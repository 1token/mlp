package bs

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"syscall"
	"time"

	"medialet.org/mlp/discovery"
)

// DefaultChunk is the RECOMMENDED PATCH ceiling (§8.4, D-76).
const DefaultChunk = 256 << 20

// Typed pusher outcomes the negotiation layer acts on.
var (
	// ErrRenegotiate: the Reservation is expired, unknown, or
	// consumed — go back to §7.6 (grant → grant refresh).
	ErrRenegotiate = errors.New("mlp/bs: reservation requires renegotiation (§8.7 step 1)")
	// ErrHashMismatch: the source bytes do not match the Manifest's
	// URN — a content problem, reported loudly (D-27).
	ErrHashMismatch = errors.New("mlp/bs: object failed URN verification at the receiver (§8.5)")
)

// Pusher drives the §8.7 loop for reservations_out rows.
type Pusher struct {
	DB     *sql.DB
	Domain string // our domain; signing key = bs-role entry in own_keys
	Now    func() time.Time
	// Client defaults to the D-72 hardened profile: https only, the
	// §5.4 address filter pinned at dial time, redirects refused
	// outright (§8.2 rule 6).
	Client *http.Client
	Chunk  int64
	// MaxAttempts bounds the outer HEAD/PATCH loop.
	MaxAttempts int
}

func (p *Pusher) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func (p *Pusher) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	p.Client = HardenedClient()
	return p.Client
}

// HardenedClient builds the D-72 pusher client: the discovery address
// filter runs in the dialer Control hook on the literal address at
// connect time; every redirect is refused. No overall timeout — PATCH
// bodies are legitimately long-lived; cancellation is the context's.
func HardenedClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return fmt.Errorf("mlp/bs: dialing non-IP address %q", host)
			}
			if discovery.ForbiddenAddr(ip) {
				return fmt.Errorf("mlp/bs: forbidden address %s (D-72)", ip)
			}
			return nil
		},
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("mlp/bs: redirects refused (§8.2 rule 6)")
		},
	}
}

// Push transfers src (whose declared exact size the reservation
// carries) under reservations_out row id, maintaining the row's
// state/offset. It returns nil once the receiver answers
// MLP-Object-State: verified.
func (p *Pusher) Push(ctx context.Context, id int64, src io.ReadSeeker) error {
	var targetURL, token, expires string
	var size int64
	if err := p.DB.QueryRowContext(ctx,
		`SELECT target_url, token, max_size, expires FROM reservations_out WHERE id=?`, id).
		Scan(&targetURL, &token, &size, &expires); err != nil {
		return fmt.Errorf("mlp/bs: reservation row: %w", err)
	}
	u, err := url.Parse(targetURL)
	if err != nil || u.Scheme != "https" {
		if p.Client == nil { // the hardened default is https-only (D-72)
			p.setState(ctx, id, "failed", 0)
			return fmt.Errorf("mlp/bs: target_url is not https (D-72)")
		}
	}
	if exp, err := time.Parse(time.RFC3339, expires); err == nil && p.now().After(exp) {
		p.setState(ctx, id, "expired", 0)
		return fmt.Errorf("%w: expired locally at %s", ErrRenegotiate, expires)
	}
	kid, priv, err := p.signingKey(ctx)
	if err != nil {
		return err
	}
	p.setState(ctx, id, "pushing", -1)

	attempts := p.MaxAttempts
	if attempts <= 0 {
		attempts = 8
	}
	chunk := p.Chunk
	if chunk <= 0 {
		chunk = DefaultChunk
	}

	for attempt := 0; attempt < attempts; attempt++ {
		// Step 1–2: HEAD for the durable checkpoint.
		offset, prob, err := p.head(ctx, targetURL, token, kid, priv)
		if err != nil {
			continue // transport fault: retry the loop
		}
		if prob != nil {
			return p.classify(ctx, id, prob)
		}
		p.setState(ctx, id, "pushing", offset)

		// Step 3: seek and PATCH the remainder.
		for offset < size {
			n := chunk
			if size-offset < n {
				n = size - offset
			}
			if _, err := src.Seek(offset, io.SeekStart); err != nil {
				return fmt.Errorf("mlp/bs: source seek: %w", err)
			}
			body := make([]byte, n)
			if _, err := io.ReadFull(src, body); err != nil {
				return fmt.Errorf("mlp/bs: source read: %w", err)
			}
			newOffset, verified, prob, err := p.patch(ctx, targetURL, token, kid, priv, offset, body)
			if err != nil {
				break // lost reply or transport death → re-HEAD (§8.7)
			}
			if prob != nil {
				switch prob.Code {
				case "digest-mismatch", "offset-mismatch":
					// digest-mismatch: retry the same offset (§8.5);
					// offset-mismatch: realign. Both via HEAD, bounded
					// by the outer attempt budget.
					prob = nil
				default:
					return p.classify(ctx, id, prob)
				}
				break
			}
			offset = newOffset
			p.setState(ctx, id, "pushing", offset)
			if verified {
				p.setState(ctx, id, "done", offset)
				return nil // step 4: MLP-Object-State: verified
			}
		}
	}
	p.setState(ctx, id, "failed", -1)
	return fmt.Errorf("mlp/bs: push not completed within %d attempts", attempts)
}

func (p *Pusher) classify(ctx context.Context, id int64, prob *Problem) error {
	switch prob.Code {
	case "reservation-expired":
		p.setState(ctx, id, "expired", -1)
		return fmt.Errorf("%w: %s", ErrRenegotiate, prob.Detail)
	case "reservation-invalid":
		p.setState(ctx, id, "failed", -1)
		return fmt.Errorf("%w: %s", ErrRenegotiate, prob.Detail)
	case "hash-mismatch":
		p.setState(ctx, id, "failed", -1)
		return fmt.Errorf("%w: %s", ErrHashMismatch, prob.Detail)
	default:
		p.setState(ctx, id, "failed", -1)
		return prob
	}
}

func (p *Pusher) setState(ctx context.Context, id int64, state string, offset int64) {
	if offset >= 0 {
		p.DB.ExecContext(ctx, `UPDATE reservations_out SET state=?, offset_confirmed=? WHERE id=?`, state, offset, id)
		return
	}
	p.DB.ExecContext(ctx, `UPDATE reservations_out SET state=? WHERE id=?`, state, id)
}

func (p *Pusher) head(ctx context.Context, targetURL, token, kid string, priv ed25519.PrivateKey) (int64, *Problem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, targetURL, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("MLP-Reservation", token)
	if err := p.sign(req, kid, priv, false); err != nil {
		return 0, nil, err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, readProblem(resp), nil
	}
	offset, err := strconv.ParseInt(resp.Header.Get("Upload-Offset"), 10, 64)
	if err != nil {
		return 0, nil, fmt.Errorf("mlp/bs: HEAD without Upload-Offset")
	}
	return offset, nil, nil
}

func (p *Pusher) patch(ctx context.Context, targetURL, token, kid string, priv ed25519.PrivateKey, offset int64, body []byte) (int64, bool, *Problem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, targetURL, bytes.NewReader(body))
	if err != nil {
		return 0, false, nil, err
	}
	digest := sha256.Sum256(body)
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Content-Type", ctOffset)
	req.Header.Set("Upload-Offset", strconv.FormatInt(offset, 10))
	req.Header.Set("Content-Digest", "sha-256=:"+toBase64(digest[:])+":")
	req.Header.Set("MLP-Reservation", token)
	if err := p.sign(req, kid, priv, true); err != nil {
		return 0, false, nil, err
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return 0, false, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return 0, false, readProblem(resp), nil
	}
	newOffset, err := strconv.ParseInt(resp.Header.Get("Upload-Offset"), 10, 64)
	if err != nil {
		return 0, false, nil, fmt.Errorf("mlp/bs: 204 without Upload-Offset")
	}
	return newOffset, resp.Header.Get("MLP-Object-State") == "verified", nil, nil
}

func (p *Pusher) sign(req *http.Request, kid string, priv ed25519.PrivateKey, hasBody bool) error {
	header := func(name string) string { return req.Header.Get(name) }
	sigInput, signature, err := SignRequest(priv, kid, req.Method, req.URL.String(), header, p.now().Unix(), hasBody)
	if err != nil {
		return err
	}
	req.Header.Set("Signature-Input", sigInput)
	req.Header.Set("Signature", signature)
	return nil
}

// signingKey loads a bs-role key valid now from own_keys.
func (p *Pusher) signingKey(ctx context.Context) (string, ed25519.PrivateKey, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT kid, seed, roles, not_before, not_after FROM own_keys`)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	now := p.now()
	for rows.Next() {
		var kid, roles string
		var seed []byte
		var nb, na sql.NullString
		if err := rows.Scan(&kid, &seed, &roles, &nb, &na); err != nil {
			return "", nil, err
		}
		var rr []string
		if json.Unmarshal([]byte(roles), &rr) != nil || !hasString(rr, "bs") {
			continue
		}
		e := discovery.KeyEntry{NotBefore: nb.String, NotAfter: na.String}
		if !e.ValidAt(now) || len(seed) != ed25519.SeedSize {
			continue
		}
		return kid, ed25519.NewKeyFromSeed(seed), rows.Err()
	}
	return "", nil, errors.New("mlp/bs: no valid bs-role signing key in own_keys")
}

func hasString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func readProblem(resp *http.Response) *Problem {
	var body struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
	code := body.Type
	if c, ok := cutPrefix(code, "urn:mlp:err:"); ok {
		code = c
	}
	return &Problem{Status: resp.StatusCode, Code: code, Detail: body.Detail}
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}
