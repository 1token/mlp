package discovery

// S4.3 acceptance: the TV-001 Domain Document fixture is the parsing
// anchor (both keys pass §6.2 self-verification with the exact vector
// kids); the §5.1 document-level checks hard-fail; the §6.2
// entry-level failures are ignored without failing the document; the
// §5.4 hardened profile enforces size, redirect, scheme/port, and
// connect-time address rules; the Resolver honors the 24-hour ceiling
// (D-33), the unknown-kid re-fetch (§5.5), and the negative cache.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"medialet.org/mlp/store"
)

const vectorPath = "../../conformance/vectors/mlp-tv-001.json"

type tv001 struct {
	Keys struct {
		Author struct{ PublicHex, Key, KID string } `json:"author"`
		SN     struct{ PublicHex, Key, KID string } `json:"sn"`
	} `json:"keys"`
	DomainDocument map[string]any `json:"domain_document"`
}

func loadTV001(t *testing.T) *tv001 {
	t.Helper()
	raw, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatalf("read TV-001: %v", err)
	}
	// json tags don't cover snake_case public_hex; decode loosely.
	var loose map[string]json.RawMessage
	if err := json.Unmarshal(raw, &loose); err != nil {
		t.Fatalf("parse TV-001: %v", err)
	}
	var v tv001
	var keys map[string]map[string]string
	if err := json.Unmarshal(loose["keys"], &keys); err != nil {
		t.Fatalf("parse TV-001 keys: %v", err)
	}
	v.Keys.Author.PublicHex = keys["author"]["public_hex"]
	v.Keys.Author.Key = keys["author"]["key"]
	v.Keys.Author.KID = keys["author"]["kid"]
	v.Keys.SN.PublicHex = keys["sn"]["public_hex"]
	v.Keys.SN.Key = keys["sn"]["key"]
	v.Keys.SN.KID = keys["sn"]["kid"]
	if err := json.Unmarshal(loose["domain_document"], &v.DomainDocument); err != nil {
		t.Fatalf("parse TV-001 domain_document: %v", err)
	}
	return &v
}

func fixtureBytes(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return b
}

func mutated(t *testing.T, base map[string]any, f func(doc map[string]any)) []byte {
	t.Helper()
	b := fixtureBytes(t, base)
	var copy map[string]any
	if err := json.Unmarshal(b, &copy); err != nil {
		t.Fatalf("clone fixture: %v", err)
	}
	f(copy)
	return fixtureBytes(t, copy)
}

// --- Domain Document parsing (§5.2, §6.1–6.3) ------------------------

func TestParseTV001Anchor(t *testing.T) {
	v := loadTV001(t)
	doc, err := ParseDocument(fixtureBytes(t, v.DomainDocument), "origin.example", []string{"0.1"})
	if err != nil {
		t.Fatalf("TV-001 fixture must parse: %v", err)
	}
	if doc.SN != "https://mlp.origin.example/sn" || doc.Contact != "hostmaster@origin.example" {
		t.Fatalf("sn/contact mismatch: %q %q", doc.SN, doc.Contact)
	}
	if doc.Rejected != 0 || len(doc.Keys) != 2 {
		t.Fatalf("want 2 accepted keys, 0 rejected; got %d/%d", len(doc.Keys), doc.Rejected)
	}
	if doc.Keys[0].KID != v.Keys.Author.KID || doc.Keys[1].KID != v.Keys.SN.KID {
		t.Fatalf("kids do not match the vector")
	}
	pub, err := doc.Keys[0].Public()
	if err != nil {
		t.Fatalf("author key decode: %v", err)
	}
	if hex.EncodeToString(pub) != v.Keys.Author.PublicHex {
		t.Fatalf("author raw key does not match the vector")
	}
	if !doc.Keys[1].HasRole("sn") || !doc.Keys[1].HasRole("bs") || doc.Keys[1].HasRole("author") {
		t.Fatalf("sn entry roles wrong: %v", doc.Keys[1].Roles)
	}
}

func TestDomainBindingHardFails(t *testing.T) {
	v := loadTV001(t)
	if _, err := ParseDocument(fixtureBytes(t, v.DomainDocument), "evil.example", []string{"0.1"}); err == nil {
		t.Fatal("domain binding mismatch must be a hard failure (D-57)")
	}
}

func TestVersionIntersectionHardFails(t *testing.T) {
	v := loadTV001(t)
	if _, err := ParseDocument(fixtureBytes(t, v.DomainDocument), "origin.example", []string{"0.2"}); err == nil {
		t.Fatal("empty version intersection must be a hard failure (§5.1 step 3)")
	}
}

func TestMissingRequiredMemberHardFails(t *testing.T) {
	v := loadTV001(t)
	for _, member := range []string{"domain", "mlp", "sn", "keys"} {
		raw := mutated(t, v.DomainDocument, func(d map[string]any) { delete(d, member) })
		if _, err := ParseDocument(raw, "origin.example", []string{"0.1"}); err == nil {
			t.Fatalf("missing %q must be a hard failure", member)
		}
	}
}

func TestKidSelfVerificationIgnoresEntry(t *testing.T) {
	v := loadTV001(t)
	raw := mutated(t, v.DomainDocument, func(d map[string]any) {
		entry := d["keys"].([]any)[0].(map[string]any)
		kid := entry["kid"].(string)
		// flip the final character within the base32 alphabet
		last := byte('a')
		if kid[len(kid)-1] == 'a' {
			last = 'b'
		}
		entry["kid"] = kid[:len(kid)-1] + string(last)
	})
	doc, err := ParseDocument(raw, "origin.example", []string{"0.1"})
	if err != nil {
		t.Fatalf("entry failure must not fail the document (§6.2): %v", err)
	}
	if len(doc.Keys) != 1 || doc.Rejected != 1 || doc.Keys[0].KID != v.Keys.SN.KID {
		t.Fatalf("want the tampered entry ignored and the sn entry kept; got %d/%d", len(doc.Keys), doc.Rejected)
	}
}

func TestAlgMismatchIgnoresEntry(t *testing.T) {
	v := loadTV001(t)
	raw := mutated(t, v.DomainDocument, func(d map[string]any) {
		d["keys"].([]any)[1].(map[string]any)["alg"] = "secp256k1"
	})
	doc, err := ParseDocument(raw, "origin.example", []string{"0.1"})
	if err != nil || len(doc.Keys) != 1 || doc.Rejected != 1 {
		t.Fatalf("unknown alg must ignore only that entry: %v %d/%d", err, len(doc.Keys), doc.Rejected)
	}
}

func TestDuplicateKidIgnored(t *testing.T) {
	v := loadTV001(t)
	raw := mutated(t, v.DomainDocument, func(d map[string]any) {
		keys := d["keys"].([]any)
		d["keys"] = append(keys, keys[0])
	})
	doc, err := ParseDocument(raw, "origin.example", []string{"0.1"})
	if err != nil || len(doc.Keys) != 2 || doc.Rejected != 1 {
		t.Fatalf("duplicate kid must be ignored: %v %d/%d", err, len(doc.Keys), doc.Rejected)
	}
}

func TestKeyEntryCapHardFails(t *testing.T) {
	v := loadTV001(t)
	raw := mutated(t, v.DomainDocument, func(d map[string]any) {
		keys := d["keys"].([]any)
		for len(keys) <= MaxKeyEntries {
			keys = append(keys, keys[0])
		}
		d["keys"] = keys
	})
	if _, err := ParseDocument(raw, "origin.example", []string{"0.1"}); err == nil {
		t.Fatalf("more than %d entries must be a hard failure (§5.2)", MaxKeyEntries)
	}
}

func TestUnknownMembersIgnored(t *testing.T) {
	v := loadTV001(t)
	raw := mutated(t, v.DomainDocument, func(d map[string]any) {
		d["x_future"] = "ignored"
		d["keys"].([]any)[0].(map[string]any)["x_hint"] = "ignored"
	})
	doc, err := ParseDocument(raw, "origin.example", []string{"0.1"})
	if err != nil || len(doc.Keys) != 2 {
		t.Fatalf("unknown members must be ignored (§2.3 rule 5): %v", err)
	}
}

func TestSizeCapAtParse(t *testing.T) {
	pad := strings.Repeat(" ", MaxDocumentBytes+1)
	if _, err := ParseDocument([]byte(pad), "origin.example", []string{"0.1"}); err == nil {
		t.Fatal("oversized document must be a hard failure (D-57)")
	}
}

func TestVerificationKeySemantics(t *testing.T) {
	v := loadTV001(t)
	at := time.Date(2026, 7, 4, 10, 0, 5, 0, time.UTC)
	raw := mutated(t, v.DomainDocument, func(d map[string]any) {
		sn := d["keys"].([]any)[1].(map[string]any)
		sn["not_before"] = "2026-07-01T00:00:00Z"
		sn["not_after"] = "2026-07-31T00:00:00Z"
	})
	doc, err := ParseDocument(raw, "origin.example", []string{"0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.VerificationKey(v.Keys.SN.KID, "sn", at); err != nil {
		t.Fatalf("in-window, in-role lookup must succeed: %v", err)
	}
	if _, err := doc.VerificationKey(v.Keys.SN.KID, "author", at); err == nil {
		t.Fatal("role mismatch must be verification failure (§6.3 rule 1)")
	}
	if _, err := doc.VerificationKey(v.Keys.SN.KID, "sn", at.AddDate(0, 2, 0)); err == nil {
		t.Fatal("out-of-window key must be rejected (§6.3 rule 3)")
	}
	if _, err := doc.VerificationKey("bunknownkid", "sn", at); err == nil {
		t.Fatal("unknown kid must fail")
	}
}

// --- Hardened fetch profile (§5.4) -----------------------------------

func TestAddrForbidden(t *testing.T) {
	forbidden := []string{
		"127.0.0.1", "127.255.255.254", "10.1.2.3", "172.16.5.5",
		"192.168.1.1", "100.64.0.1", "169.254.169.254", "0.0.0.0",
		"224.0.0.1", "255.255.255.255", "192.0.0.8", "192.0.2.1",
		"198.51.100.7", "203.0.113.9", "198.18.0.1", "240.0.0.1",
		"::1", "::", "fe80::1", "fc00::1", "fd12::1", "ff02::1",
		"2001:db8::1", "64:ff9b::7f00:1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", // v4-mapped must unmap
	}
	for _, s := range forbidden {
		if !addrForbidden(netip.MustParseAddr(s)) {
			t.Errorf("%s must be forbidden (§5.4 rule 4)", s)
		}
	}
	allowed := []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "2606:4700::1111"}
	for _, s := range allowed {
		if addrForbidden(netip.MustParseAddr(s)) {
			t.Errorf("%s must be allowed", s)
		}
	}
}

func TestURLHardened(t *testing.T) {
	for _, bad := range []string{"http://a.example/x", "ftp://a.example/x", "https://a.example:8443/x"} {
		u, _ := url.Parse(bad)
		if err := urlHardened(u, true); err == nil {
			t.Errorf("%s must be refused (§5.4 rule 1)", bad)
		}
	}
	for _, good := range []string{"https://a.example/.well-known/medialet.json", "https://a.example:443/x"} {
		u, _ := url.Parse(good)
		if err := urlHardened(u, true); err != nil {
			t.Errorf("%s must be allowed: %v", good, err)
		}
	}
}

// testFetcher builds a Fetcher that trusts ts's certificate and skips
// the port-443 and address rules (each unit-tested above), so the
// pipeline rules — size cap, redirect budget, per-hop scheme — can be
// exercised against loopback servers.
func testFetcher(ts *httptest.Server, docURL string) *Fetcher {
	f := &Fetcher{
		endpoint:       func(string) string { return docURL },
		checkAddr:      func(netip.Addr) error { return nil },
		requirePort443: false,
	}
	f.client = f.newClient(ts.Client().Transport)
	return f
}

func TestFetchSizeCapAborts(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", MaxDocumentBytes+4096)))
	}))
	defer ts.Close()
	f := testFetcher(ts, ts.URL+"/.well-known/medialet.json")
	if _, _, err := f.FetchDomainDocument(context.Background(), "origin.example"); err == nil {
		t.Fatal("response beyond the cap must abort (§5.4 rule 2)")
	}
}

func TestFetchRedirectBudget(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, ts.URL+r.URL.Path+"r", http.StatusFound)
	}))
	defer ts.Close()
	f := testFetcher(ts, ts.URL+"/hop")
	_, _, err := f.FetchDomainDocument(context.Background(), "origin.example")
	if err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Fatalf("a fourth redirect must be refused (§5.4 rule 3): %v", err)
	}
}

func TestFetchRedirectToHTTPRefused(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://origin.example/doc", http.StatusFound)
	}))
	defer ts.Close()
	f := testFetcher(ts, ts.URL+"/doc")
	if _, _, err := f.FetchDomainDocument(context.Background(), "origin.example"); err == nil {
		t.Fatal("redirect to http must be refused (§5.4 rule 3)")
	}
}

// TestDialTimeAddressCheckWired proves the production address filter
// runs at connection time on the literal dialed address: a fetcher
// with the production checkAddr (but relaxed port rule and test TLS
// trust) must refuse to even connect to a loopback server.
func TestDialTimeAddressCheckWired(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{}"))
	}))
	defer ts.Close()
	f := &Fetcher{
		endpoint: func(string) string { return ts.URL + "/doc" },
		checkAddr: func(ip netip.Addr) error {
			if addrForbidden(ip) {
				return fmt.Errorf("forbidden address %s", ip)
			}
			return nil
		},
		requirePort443: false,
	}
	f.client = f.newClient(ts.Client().Transport)
	_, _, err := f.FetchDomainDocument(context.Background(), "origin.example")
	if err == nil || !strings.Contains(err.Error(), "forbidden address") {
		t.Fatalf("loopback dial must be refused by the Control hook (§5.4 rule 4): %v", err)
	}
}

// --- Resolver: caching, ceiling, unknown-kid re-fetch (§5.5) ---------

type fixtureServer struct {
	ts   *httptest.Server
	hits atomic.Int64
	doc  atomic.Value // []byte
	hdr  atomic.Value // string Cache-Control, "" for none
	code atomic.Int64
}

func newFixtureServer(t *testing.T, doc []byte) *fixtureServer {
	fs := &fixtureServer{}
	fs.doc.Store(doc)
	fs.hdr.Store("")
	fs.code.Store(int64(http.StatusOK))
	fs.ts = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.hits.Add(1)
		if cc := fs.hdr.Load().(string); cc != "" {
			w.Header().Set("Cache-Control", cc)
		}
		if c := int(fs.code.Load()); c != http.StatusOK {
			w.WriteHeader(c)
			return
		}
		w.Write(fs.doc.Load().([]byte))
	}))
	t.Cleanup(fs.ts.Close)
	return fs
}

func newTestResolver(t *testing.T, fs *fixtureServer) (*Resolver, *time.Time) {
	t.Helper()
	db, err := store.Open("sqlite3", "file:"+t.TempDir()+"/mlp.db?_fk=1")
	if err != nil {
		t.Fatalf("open+migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	r := &Resolver{
		DB:        db,
		Fetcher:   testFetcher(fs.ts, fs.ts.URL+"/.well-known/medialet.json"),
		Supported: []string{"0.1"},
		Now:       func() time.Time { return now },
	}
	return r, &now
}

func TestResolverCacheCeilingAndKidRefetch(t *testing.T) {
	v := loadTV001(t)
	authorOnly := mutated(t, v.DomainDocument, func(d map[string]any) {
		d["keys"] = d["keys"].([]any)[:1]
	})
	fs := newFixtureServer(t, authorOnly)
	fs.hdr.Store("max-age=999999") // must be capped by the ceiling
	r, now := newTestResolver(t, fs)
	ctx := context.Background()

	doc, err := r.Resolve(ctx, "Origin.Example") // normalization too
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(doc.Keys) != 1 || fs.hits.Load() != 1 {
		t.Fatalf("first resolve: keys=%d hits=%d", len(doc.Keys), fs.hits.Load())
	}

	// D-33: expires_at - fetched_at is capped at 24 h despite max-age.
	var fetched, expires string
	if err := r.DB.QueryRow(`SELECT fetched_at, expires_at FROM domain_docs WHERE domain='origin.example'`).
		Scan(&fetched, &expires); err != nil {
		t.Fatalf("cache row: %v", err)
	}
	ft, _ := time.Parse(time.RFC3339, fetched)
	et, _ := time.Parse(time.RFC3339, expires)
	if et.Sub(ft) != CacheCeiling {
		t.Fatalf("ceiling not applied: ttl=%v (D-33)", et.Sub(ft))
	}

	// Fresh cache serves without a network hit.
	if _, err := r.Resolve(ctx, "origin.example"); err != nil || fs.hits.Load() != 1 {
		t.Fatalf("second resolve must be cached: err=%v hits=%d", err, fs.hits.Load())
	}

	// Known kid resolves from cache.
	if _, err := r.ResolveKID(ctx, "origin.example", v.Keys.Author.KID); err != nil || fs.hits.Load() != 1 {
		t.Fatalf("known kid must resolve from cache: err=%v hits=%d", err, fs.hits.Load())
	}

	// §5.5 MUST: an unknown kid forces a re-fetch. Rotate the served
	// document to the full two-key set; the sn kid must then be found.
	fs.doc.Store(fixtureBytes(t, v.DomainDocument))
	e, err := r.ResolveKID(ctx, "origin.example", v.Keys.SN.KID)
	if err != nil || e.Key != v.Keys.SN.Key {
		t.Fatalf("unknown kid must trigger re-fetch and resolve: %v", err)
	}
	if fs.hits.Load() != 2 {
		t.Fatalf("re-fetch expected exactly once: hits=%d", fs.hits.Load())
	}
	var n int
	if err := r.DB.QueryRow(`SELECT COUNT(*) FROM domain_keys WHERE domain='origin.example'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("domain_keys must hold the rotated set: n=%d err=%v", n, err)
	}

	// Expiry honored: past the ceiling, resolve fetches again.
	*now = now.Add(CacheCeiling + time.Hour)
	if _, err := r.Resolve(ctx, "origin.example"); err != nil || fs.hits.Load() != 3 {
		t.Fatalf("expired cache must re-fetch: err=%v hits=%d", err, fs.hits.Load())
	}

	// A kid still absent after a forced re-fetch is unknown.
	if _, err := r.ResolveKID(ctx, "origin.example", "bnosuchkid"); err == nil {
		t.Fatal("absent kid must fail after re-fetch")
	}
}

func TestResolverNegativeCache(t *testing.T) {
	fs := newFixtureServer(t, []byte("{}"))
	fs.code.Store(int64(http.StatusInternalServerError))
	r, now := newTestResolver(t, fs)
	ctx := context.Background()

	if _, err := r.Resolve(ctx, "origin.example"); err == nil {
		t.Fatal("500 must fail resolution")
	}
	if _, err := r.Resolve(ctx, "origin.example"); err == nil || fs.hits.Load() != 1 {
		t.Fatalf("failure must be negative-cached (§5.5): hits=%d", fs.hits.Load())
	}
	// After the negative TTL the domain is retried and recovers.
	fs.code.Store(int64(http.StatusOK))
	v := loadTV001(t)
	fs.doc.Store(fixtureBytes(t, v.DomainDocument))
	*now = now.Add(NegativeTTL + time.Second)
	if _, err := r.Resolve(ctx, "origin.example"); err != nil || fs.hits.Load() != 2 {
		t.Fatalf("negative cache must expire: err=%v hits=%d", err, fs.hits.Load())
	}
}

func TestResolverRejectsMalformedDomain(t *testing.T) {
	r := &Resolver{}
	for _, bad := range []string{"", "a/b", "a b", "user@host", ".example", "example."} {
		if _, err := r.Resolve(context.Background(), bad); err == nil {
			t.Errorf("domain %q must be rejected", bad)
		}
	}
}
