package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The hardened fetch profile (§5.4, D-11/D-59). Discovery fetches are
// the protocol's only server-initiated outbound requests outside
// granted reservations (D-34), so every rule is enforced here:
//
//  1. GET, https, port 443
//  2. response size cap 65,536 bytes, aborted beyond it
//  3. at most 3 redirects, each https, each re-subjected to every rule
//  4. IP safety with the resolved address pinned at connection time
//  5. connect <= 5 s, total <= 10 s
//  6. no credentials, no cookies, no request bodies
const (
	connectTimeout = 5 * time.Second
	totalTimeout   = 10 * time.Second
	maxRedirects   = 3
)

var errTooLarge = fmt.Errorf("mlp/discovery: response exceeds the %d-byte cap (§5.4 rule 2)", MaxDocumentBytes)

// forbiddenPrefixes lists IANA special-purpose ranges beyond what
// netip classifies directly (§5.4 rule 4).
var forbiddenPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),   // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation TEST-NET-1
	netip.MustParsePrefix("198.51.100.0/24"), // documentation TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation TEST-NET-3
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved (incl. broadcast)
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64 well-known prefix
}

// addrForbidden implements §5.4 rule 4: refuse addresses in the IANA
// special-purpose registries. IPv4-mapped IPv6 addresses are unmapped
// first so `::ffff:127.0.0.1` is judged as `127.0.0.1`.
func addrForbidden(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	for _, p := range forbiddenPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// urlHardened enforces §5.4 rule 1 on a request URL — including every
// redirect target (rule 3). requirePort443 is relaxed only in tests.
func urlHardened(u *url.URL, requirePort443 bool) error {
	if u.Scheme != "https" {
		return fmt.Errorf("mlp/discovery: scheme %q refused, https only (§5.4 rule 1)", u.Scheme)
	}
	if requirePort443 {
		if p := u.Port(); p != "" && p != "443" {
			return fmt.Errorf("mlp/discovery: port %s refused, 443 only (§5.4 rule 1)", p)
		}
	}
	return nil
}

// Fetcher performs Domain Document fetches under the hardened
// profile. The zero value is not usable; construct with NewFetcher.
type Fetcher struct {
	client *http.Client
	// endpoint maps a domain to the fetch URL; production is the
	// well-known location (§5.1 step 2). Overridden only in tests.
	endpoint func(domain string) string
	// checkAddr is consulted at connection time on the literal
	// address being dialed; production is addrForbidden.
	checkAddr func(netip.Addr) error
	// requirePort443 is true in production (§5.4 rule 1).
	requirePort443 bool
	// allowInsecure permits plain http; NewDemoFetcher only.
	allowInsecure bool
}

// NewFetcher returns a production Fetcher enforcing the full §5.4
// profile.
func NewFetcher() *Fetcher {
	f := &Fetcher{
		endpoint: func(domain string) string {
			return "https://" + domain + "/.well-known/medialet.json"
		},
		checkAddr: func(ip netip.Addr) error {
			if addrForbidden(ip) {
				return fmt.Errorf("mlp/discovery: forbidden address %s (§5.4 rule 4)", ip)
			}
			return nil
		},
		requirePort443: true,
	}
	f.client = f.newClient(nil)
	return f
}

// newClient builds the hardened http.Client. The Control hook runs on
// the literal resolved address immediately before the connect(2) call
// — the check and the use are the same address, which is the §5.4
// rule 4 pinning requirement (no DNS-rebinding window between them).
// Each redirect hop dials afresh and passes through the same hook.
func (f *Fetcher) newClient(base http.RoundTripper) *http.Client {
	dialer := &net.Dialer{
		Timeout: connectTimeout,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return fmt.Errorf("mlp/discovery: dialing non-IP address %q", host)
			}
			return f.checkAddr(ip)
		},
	}
	transport, _ := base.(*http.Transport)
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil // discovery never traverses a proxy
	return &http.Client{
		Transport: transport,
		Timeout:   totalTimeout, // §5.4 rule 5
		Jar:       nil,          // §5.4 rule 6: no cookies
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("mlp/discovery: more than %d redirects (§5.4 rule 3)", maxRedirects)
			}
			return urlHardened(req.URL, f.requirePort443)
		},
	}
}

// FetchDomainDocument GETs the Domain Document for domain under the
// hardened profile, returning the raw body (<= MaxDocumentBytes) and
// the response headers (for §5.5 freshness).
func (f *Fetcher) FetchDomainDocument(ctx context.Context, domain string) ([]byte, http.Header, error) {
	target := f.endpoint(domain)
	u, err := url.Parse(target)
	if err != nil {
		return nil, nil, fmt.Errorf("mlp/discovery: %w", err)
	}
	if err := urlHardened(u, f.requirePort443); err != nil && !(f.allowInsecure && u.Scheme == "http") {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil) // rule 6: no body
	if err != nil {
		return nil, nil, fmt.Errorf("mlp/discovery: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("mlp/discovery: fetch %s: %w", domain, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("mlp/discovery: fetch %s: status %d", domain, resp.StatusCode)
	}
	// Rule 2: read at most cap+1 bytes; anything past the cap aborts
	// the transfer (Close on the partially read body tears down the
	// connection rather than draining it).
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxDocumentBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("mlp/discovery: fetch %s: %w", domain, err)
	}
	if len(body) > MaxDocumentBytes {
		return nil, nil, errTooLarge
	}
	return body, resp.Header, nil
}

// freshness derives a cache TTL from response headers under standard
// HTTP semantics (§5.5): Cache-Control max-age when present;
// no-store/no-cache yield zero (the response is used but not reused).
// The 24-hour ceiling (D-33) is applied by the Resolver regardless.
func freshness(h http.Header, fallback time.Duration) time.Duration {
	cc := strings.ToLower(h.Get("Cache-Control"))
	if cc == "" {
		return fallback
	}
	for _, d := range strings.Split(cc, ",") {
		d = strings.TrimSpace(d)
		if d == "no-store" || d == "no-cache" {
			return 0
		}
		if v, ok := strings.CutPrefix(d, "max-age="); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 {
				return 0
			}
			return time.Duration(n) * time.Second
		}
	}
	return fallback
}

var errNegativeCached = errors.New("mlp/discovery: domain in negative cache (§5.5)")

// ForbiddenAddr reports whether ip falls in the §5.4 rule-4 forbidden
// set. Exported for the D-72 pusher connection-safety reuse (§7.5,
// §8.2): transfer clients apply the same address filter at dial time.
func ForbiddenAddr(ip netip.Addr) bool { return addrForbidden(ip) }

// NewDemoFetcher returns a Fetcher for LOCAL DEMONSTRATIONS ONLY: it
// resolves the given domains to explicit base URLs (typically
// http://127.0.0.1:<port>), permits loopback dialing, plain HTTP,
// and non-443 ports — every one of which the production §5.4
// profile forbids. Domains outside the map fail closed. Nothing in
// production composition calls this; the demo binary does, loudly.
func NewDemoFetcher(peers map[string]string) *Fetcher {
	f := NewFetcher()
	f.endpoint = func(domain string) string {
		base, ok := peers[domain]
		if !ok {
			return "https://" + domain + "/.well-known/medialet.json"
		}
		return base + "/.well-known/medialet.json"
	}
	f.checkAddr = func(netip.Addr) error { return nil }
	f.requirePort443 = false
	f.allowInsecure = true
	f.client = f.newClient(http.DefaultTransport)
	return f
}
