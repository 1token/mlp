package bs

// S4.5 acceptance, anchored on TV-003: the RFC 9421 base for every
// vector request reproduces byte-identically and the vector
// signatures verify against the pusher kid; the full transcript
// (PATCH 0–19, HEAD, PATCH 20–35) replays header-for-header through
// the handler with the vector's responses; the D-77 invariant holds
// under digest-mismatch rollback, hash-mismatch reset, offset
// realignment, freshness rejection, consumed/expired tokens, and
// process-restart re-derivation; the §8.7 pusher loop survives a
// lost 204 re-sending nothing that already landed.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"medialet.org/mlp/core"
	"medialet.org/mlp/discovery"
	"medialet.org/mlp/store"
)

const (
	tv001Path = "../../conformance/vectors/mlp-tv-001.json"
	tv003Path = "../../conformance/vectors/mlp-tv-003.json"
	seedSNBS  = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb" // RFC 8032 TEST 2
	seedAuth  = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60" // RFC 8032 TEST 1
)

type tvRequest struct {
	Step          int               `json:"step"`
	Request       string            `json:"request"`
	BodyUTF8      string            `json:"body_utf8"`
	Headers       map[string]string `json:"headers"`
	SignatureBase string            `json:"signature_base"`
	Response      map[string]any    `json:"response"`
}

type tv003 struct {
	Reservation struct {
		TargetURL string `json:"target_url"`
		Token     string `json:"token"`
		MaxSize   int64  `json:"max_size"`
		URN       string `json:"urn"`
		Expires   string `json:"expires"`
	} `json:"reservation"`
	Object struct {
		BytesUTF8 string `json:"bytes_utf8"`
		Size      int64  `json:"size"`
		URN       string `json:"urn"`
	} `json:"object"`
	PusherKID string      `json:"pusher_kid"`
	Requests  []tvRequest `json:"requests"`
}

func loadTV003(t *testing.T) *tv003 {
	t.Helper()
	raw, err := os.ReadFile(tv003Path)
	if err != nil {
		t.Fatal(err)
	}
	var v tv003
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return &v
}

// clock for the transcript: within the 300 s window of every vector
// created (1783168260–70) and well before the reservation expiry.
func vectorClock() time.Time { return time.Unix(1783168265, 0).UTC() }

func newBS(t *testing.T, clock *time.Time) *BS {
	t.Helper()
	db, err := store.Open("sqlite3", "file:"+t.TempDir()+"/mlp.db?_fk=1")
	if err != nil {
		t.Fatalf("open+migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, `INSERT INTO stores (id, name) VALUES (1, 'default')`)
	b := &BS{
		DB: db,
		Resolver: &discovery.Resolver{
			DB: db, Fetcher: discovery.NewFetcher(), Supported: []string{"0.1"},
			Now: func() time.Time { return *clock },
		},
		Root:       t.TempDir(),
		PublicBase: "https://bs.target.example",
		Now:        func() time.Time { return *clock },
	}
	// origin.example's Domain Document (TV-001) provides the pusher's
	// bs-role key; the fetcher can reach nothing, so the cache serves.
	var tv1 map[string]json.RawMessage
	raw, _ := os.ReadFile(tv001Path)
	json.Unmarshal(raw, &tv1)
	doc, err := discovery.ParseDocument(tv1["domain_document"], "origin.example", []string{"0.1"})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO domain_docs (domain, doc, fetched_at, expires_at) VALUES (?,?,?,?)`,
		"origin.example", string(tv1["domain_document"]),
		clock.Format(time.RFC3339), clock.Add(23*time.Hour).Format(time.RFC3339))
	for _, k := range doc.Keys {
		roles, _ := json.Marshal(k.Roles)
		mustExec(t, db, `INSERT INTO domain_keys (domain, kid, key, roles) VALUES (?,?,?,?)`,
			"origin.example", k.KID, k.Key, string(roles))
	}
	return b
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func seedReservation(t *testing.T, b *BS, token, urn string, maxSize int64, expires string) {
	t.Helper()
	if _, err := b.DB.Exec(
		`INSERT INTO reservations_in (token_hash, urn, max_size, pusher_domain, expires, state, store_id, created)
		 VALUES (?,?,?,?,?,'pending',1,?)`,
		tokenHash(token), urn, maxSize, "origin.example", expires, b.now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
}

func vectorBS(t *testing.T, clock *time.Time) *BS {
	t.Helper()
	v := loadTV003(t)
	b := newBS(t, clock)
	seedReservation(t, b, v.Reservation.Token, v.Reservation.URN, v.Reservation.MaxSize, v.Reservation.Expires)
	return b
}

// --- RFC 9421 base construction, byte-exact ---------------------------

func TestSignatureBasesReproduceTV003(t *testing.T) {
	v := loadTV003(t)
	seed, _ := hex.DecodeString(seedSNBS)
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if core.KID(pub) != v.PusherKID {
		t.Fatalf("pusher kid mismatch")
	}
	for _, r := range v.Requests {
		si, err := ParseSignatureInput(r.Headers["Signature-Input"])
		if err != nil {
			t.Fatalf("step %d: %v", r.Step, err)
		}
		header := func(name string) string {
			for k, val := range r.Headers {
				if strings.EqualFold(k, name) {
					return val
				}
			}
			return ""
		}
		base, err := BuildBase(r.Request, v.Reservation.TargetURL, header, si)
		if err != nil {
			t.Fatalf("step %d: %v", r.Step, err)
		}
		if base != r.SignatureBase {
			t.Fatalf("step %d base not byte-identical:\n got %q\nwant %q", r.Step, base, r.SignatureBase)
		}
		sig, err := ParseSignature(r.Headers["Signature"])
		if err != nil {
			t.Fatalf("step %d: %v", r.Step, err)
		}
		if !ed25519.Verify(pub, []byte(base), sig) {
			t.Fatalf("step %d: vector signature does not verify", r.Step)
		}
	}
}

// --- The TV-003 transcript through the handler ------------------------

func replay(t *testing.T, ts *httptest.Server, r tvRequest, targetPath string) *http.Response {
	t.Helper()
	var body *strings.Reader = strings.NewReader(r.BodyUTF8)
	req, err := http.NewRequest(r.Request, ts.URL+targetPath, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestTranscriptWalk(t *testing.T) {
	v := loadTV003(t)
	clock := vectorClock()
	b := vectorBS(t, &clock)
	ts := httptest.NewServer(Handler(b))
	defer ts.Close()
	path := "/ingest/24c372e9a5a3c559"

	// Step 1: PATCH bytes 0–19 → 204, checkpoint 20. (The lost reply
	// is pusher-side fiction; the server state is what matters.)
	resp := replay(t, ts, v.Requests[0], path)
	if resp.StatusCode != 204 || resp.Header.Get("Upload-Offset") != "20" {
		t.Fatalf("step 1: %d %q", resp.StatusCode, resp.Header.Get("Upload-Offset"))
	}
	// Step 2: HEAD → the durable checkpoint, exactly the vector reply.
	resp = replay(t, ts, v.Requests[1], path)
	if resp.StatusCode != 200 ||
		resp.Header.Get("Upload-Offset") != "20" ||
		resp.Header.Get("Upload-Length") != "36" ||
		resp.Header.Get("Upload-Expires") != "Tue, 07 Jul 2026 12:30:00 GMT" ||
		resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("step 2 headers: %d %+v", resp.StatusCode, resp.Header)
	}
	// Step 3: PATCH bytes 20–35 → completion, verified.
	resp = replay(t, ts, v.Requests[2], path)
	if resp.StatusCode != 204 ||
		resp.Header.Get("Upload-Offset") != "36" ||
		resp.Header.Get("MLP-Object-State") != "verified" {
		t.Fatalf("step 3: %d %+v", resp.StatusCode, resp.Header)
	}
	// Object live with the exact bytes; token consumed; quarantine empty.
	data, err := os.ReadFile(b.ObjectPath(v.Object.URN))
	if err != nil || string(data) != v.Object.BytesUTF8 {
		t.Fatalf("object bytes: %v %q", err, data)
	}
	var state string
	var n int
	b.DB.QueryRow(`SELECT state FROM reservations_in WHERE token_hash=?`, tokenHash(v.Reservation.Token)).Scan(&state)
	b.DB.QueryRow(`SELECT COUNT(*) FROM objects WHERE urn=? AND state='live'`, v.Object.URN).Scan(&n)
	if state != "consumed" || n != 1 {
		t.Fatalf("finalization: reservation=%q objects=%d", state, n)
	}
	if _, err := os.Stat(b.quarantinePath(tokenHash(v.Reservation.Token))); !os.IsNotExist(err) {
		t.Fatalf("partial must leave quarantine: %v", err)
	}
	// The consumed token is single-use (D-18).
	resp = replay(t, ts, v.Requests[2], path)
	if resp.StatusCode != 410 {
		t.Fatalf("consumed token must be 410 reservation-invalid: %d", resp.StatusCode)
	}
}

// TestRestartRederivation replays the transcript but simulates a
// process restart between the PATCHes: the in-memory checkpoint is
// dropped and must re-derive from the quarantined partial (D-27).
func TestRestartRederivation(t *testing.T) {
	v := loadTV003(t)
	clock := vectorClock()
	b := vectorBS(t, &clock)
	ts := httptest.NewServer(Handler(b))
	defer ts.Close()
	path := "/ingest/24c372e9a5a3c559"

	if resp := replay(t, ts, v.Requests[0], path); resp.StatusCode != 204 {
		t.Fatalf("step 1: %d", resp.StatusCode)
	}
	b.mu.Lock()
	b.active = nil // the restart
	b.mu.Unlock()
	resp := replay(t, ts, v.Requests[2], path)
	if resp.StatusCode != 204 || resp.Header.Get("MLP-Object-State") != "verified" {
		t.Fatalf("post-restart completion: %d %+v", resp.StatusCode, resp.Header)
	}
}

// --- The D-77 invariant: rollback, reset, realignment ------------------

// signedPatch builds a fresh signed PATCH with our own signer.
func signedPatch(t *testing.T, targetURL, token string, offset int64, body []byte, digestOverride string, created int64) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, targetURL, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	digest := digestOverride
	if digest == "" {
		sum := sha256Sum(body)
		digest = "sha-256=:" + toBase64(sum) + ":"
	}
	req.Header.Set("Tus-Resumable", "1.0.0")
	req.Header.Set("Content-Type", ctOffset)
	req.Header.Set("Upload-Offset", fmt.Sprintf("%d", offset))
	req.Header.Set("Content-Digest", digest)
	req.Header.Set("MLP-Reservation", token)
	seed, _ := hex.DecodeString(seedSNBS)
	priv := ed25519.NewKeyFromSeed(seed)
	header := func(name string) string { return req.Header.Get(name) }
	sigInput, signature, err := SignRequest(priv, core.KID(priv.Public().(ed25519.PublicKey)),
		"PATCH", targetURL, header, created, true)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Signature-Input", sigInput)
	req.Header.Set("Signature", signature)
	return req
}

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

func TestDigestMismatchRollsBack(t *testing.T) {
	v := loadTV003(t)
	clock := vectorClock()
	b := vectorBS(t, &clock)
	ts := httptest.NewServer(Handler(b))
	defer ts.Close()
	target := ts.URL + "/x"
	b.PublicBase = strings.TrimSuffix(ts.URL, "") // sign over the real URL
	created := clock.Unix()
	object := []byte(v.Object.BytesUTF8)
	token := v.Reservation.Token

	// A correct first PATCH lands bytes 0–19.
	resp, err := ts.Client().Do(signedPatch(t, target, token, 0, object[:20], "", created))
	if err != nil || resp.StatusCode != 204 {
		t.Fatalf("setup PATCH: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// A corrupted second PATCH: valid signature over a wrong claimed
	// digest (the signature covers the *claimed* value, D-77).
	wrong := "sha-256=:" + toBase64(sha256Sum([]byte("not these bytes"))) + ":"
	resp, err = ts.Client().Do(signedPatch(t, target, token, 20, object[20:], wrong, created))
	if err != nil || resp.StatusCode != 422 {
		t.Fatalf("corrupted PATCH: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()

	// The checkpoint stands at 20; the partial truncated back to it.
	var offset int64
	b.DB.QueryRow(`SELECT upload_offset FROM reservations_in WHERE token_hash=?`, tokenHash(token)).Scan(&offset)
	st, err := os.Stat(b.quarantinePath(tokenHash(token)))
	if offset != 20 || err != nil || st.Size() != 20 {
		t.Fatalf("rollback: offset=%d size=%v", offset, st)
	}
	// Retry at the same offset succeeds and completes.
	resp, err = ts.Client().Do(signedPatch(t, target, token, 20, object[20:], "", created))
	if err != nil || resp.StatusCode != 204 || resp.Header.Get("MLP-Object-State") != "verified" {
		t.Fatalf("retry: %v %d", err, resp.StatusCode)
	}
	resp.Body.Close()
}

func TestOffsetMismatchAndHashReset(t *testing.T) {
	v := loadTV003(t)
	clock := vectorClock()
	b := vectorBS(t, &clock)
	ts := httptest.NewServer(Handler(b))
	defer ts.Close()
	b.PublicBase = ts.URL
	target := ts.URL + "/x"
	created := clock.Unix()
	token := v.Reservation.Token
	object := []byte(v.Object.BytesUTF8)

	// Wrong offset → 409, nothing consumed.
	resp, _ := ts.Client().Do(signedPatch(t, target, token, 5, object[5:20], "", created))
	if resp.StatusCode != 409 {
		t.Fatalf("want 409 offset-mismatch: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 36 bytes of wrong content: request digests fine, object fails
	// its URN at completion → 422 hash-mismatch, reset to zero.
	garbage := []byte(strings.Repeat("Z", 36))
	resp, _ = ts.Client().Do(signedPatch(t, target, token, 0, garbage, "", created))
	if resp.StatusCode != 422 {
		t.Fatalf("want 422 hash-mismatch: %d", resp.StatusCode)
	}
	resp.Body.Close()
	var offset int64
	b.DB.QueryRow(`SELECT upload_offset FROM reservations_in WHERE token_hash=?`, tokenHash(token)).Scan(&offset)
	if offset != 0 {
		t.Fatalf("hash-mismatch must reset to zero: %d", offset)
	}
	// The reservation survives; a clean re-push verifies (§8.5).
	resp, _ = ts.Client().Do(signedPatch(t, target, token, 0, object, "", created))
	if resp.StatusCode != 204 || resp.Header.Get("MLP-Object-State") != "verified" {
		t.Fatalf("clean re-push: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSizeOverrunIsObjectFailure(t *testing.T) {
	v := loadTV003(t)
	clock := vectorClock()
	b := vectorBS(t, &clock)
	ts := httptest.NewServer(Handler(b))
	defer ts.Close()
	b.PublicBase = ts.URL
	body := []byte(v.Object.BytesUTF8 + "EXTRA")
	resp, _ := ts.Client().Do(signedPatch(t, ts.URL+"/x", v.Reservation.Token, 0, body, "", clock.Unix()))
	if resp.StatusCode != 422 {
		t.Fatalf("overrun past the exact size must fail: %d", resp.StatusCode)
	}
	resp.Body.Close()
	var offset int64
	b.DB.QueryRow(`SELECT upload_offset FROM reservations_in WHERE token_hash=?`,
		tokenHash(v.Reservation.Token)).Scan(&offset)
	if offset != 0 {
		t.Fatalf("overrun must reset: %d", offset)
	}
}

func TestFreshnessWindowAndRoles(t *testing.T) {
	v := loadTV003(t)
	clock := vectorClock()
	b := vectorBS(t, &clock)
	ts := httptest.NewServer(Handler(b))
	defer ts.Close()
	b.PublicBase = ts.URL
	object := []byte(v.Object.BytesUTF8)

	// created 10 minutes stale → 401 (§6.6 rule 3).
	resp, _ := ts.Client().Do(signedPatch(t, ts.URL+"/x", v.Reservation.Token, 0, object[:20], "", clock.Add(-10*time.Minute).Unix()))
	if resp.StatusCode != 401 {
		t.Fatalf("stale created must be 401: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Signed with the author key (no bs role) → 401.
	req := signedPatch(t, ts.URL+"/x", v.Reservation.Token, 0, object[:20], "", clock.Unix())
	seed, _ := hex.DecodeString(seedAuth)
	priv := ed25519.NewKeyFromSeed(seed)
	header := func(name string) string { return req.Header.Get(name) }
	si, sg, err := SignRequest(priv, core.KID(priv.Public().(ed25519.PublicKey)), "PATCH", ts.URL+"/x", header, clock.Unix(), true)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Signature-Input", si)
	req.Header.Set("Signature", sg)
	resp, _ = ts.Client().Do(req)
	if resp.StatusCode != 401 {
		t.Fatalf("author-role key must be refused for transfer: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Past expiry → 410 reservation-expired.
	clock = time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	resp, _ = ts.Client().Do(signedPatch(t, ts.URL+"/x", v.Reservation.Token, 0, object[:20], "", clock.Unix()))
	if resp.StatusCode != 410 {
		t.Fatalf("expired reservation must be 410: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// --- The §8.7 pusher loop ----------------------------------------------

// lossyTransport performs the first PATCH for real but reports a
// transport failure — the lost-204 scenario of TV-003 step 1.
type lossyTransport struct {
	base    http.RoundTripper
	mu      sync.Mutex
	dropped bool
}

func (l *lossyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := l.base.RoundTrip(req)
	if err != nil || req.Method != http.MethodPatch {
		return resp, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.dropped {
		l.dropped = true
		resp.Body.Close()
		return nil, fmt.Errorf("simulated reply loss")
	}
	return resp, nil
}

func TestPusherLoopSurvivesLostReply(t *testing.T) {
	v := loadTV003(t)
	clock := vectorClock()
	b := vectorBS(t, &clock)

	var mu sync.Mutex
	var patchOffsets []string
	logging := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			mu.Lock()
			patchOffsets = append(patchOffsets, r.Header.Get("Upload-Offset"))
			mu.Unlock()
		}
		Handler(b).ServeHTTP(w, r)
	})
	ts := httptest.NewServer(logging)
	defer ts.Close()
	b.PublicBase = ts.URL

	// The origin side: own bs key and the granted reservation row.
	odb, err := store.Open("sqlite3", "file:"+t.TempDir()+"/origin.db?_fk=1")
	if err != nil {
		t.Fatal(err)
	}
	defer odb.Close()
	seed, _ := hex.DecodeString(seedSNBS)
	kid := core.KID(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	if _, err := odb.Exec(`INSERT INTO own_keys (kid, seed, roles) VALUES (?,?,?)`, kid, seed, `["sn","bs"]`); err != nil {
		t.Fatal(err)
	}
	if _, err := odb.Exec(
		`INSERT INTO reservations_out (id, urn, target_url, token, max_size, expires, envelope_id, state)
		 VALUES (1,?,?,?,?,?,?, 'pending')`,
		v.Object.URN, ts.URL+"/x", v.Reservation.Token, v.Object.Size, v.Reservation.Expires,
		"019f2c92-2c88-7c16-a1fe-4548abf07edd"); err != nil {
		t.Fatal(err)
	}

	p := &Pusher{
		DB: odb, Domain: "origin.example",
		Now:    func() time.Time { return clock },
		Client: &http.Client{Transport: &lossyTransport{base: http.DefaultTransport}},
		Chunk:  20,
	}
	if err := p.Push(context.Background(), 1, strings.NewReader(v.Object.BytesUTF8)); err != nil {
		t.Fatalf("push: %v", err)
	}
	var state string
	var confirmed int64
	odb.QueryRow(`SELECT state, offset_confirmed FROM reservations_out WHERE id=1`).Scan(&state, &confirmed)
	if state != "done" || confirmed != 36 {
		t.Fatalf("pusher record: %q %d", state, confirmed)
	}
	// Bytes 0–19 landed once despite the lost reply; only the
	// remainder was re-sent: PATCH offsets exactly [0, 20].
	mu.Lock()
	defer mu.Unlock()
	if len(patchOffsets) != 2 || patchOffsets[0] != "0" || patchOffsets[1] != "20" {
		t.Fatalf("zero redundant bytes re-transferred, want [0 20]: %v", patchOffsets)
	}
	// The receiving side finalized: object live.
	var n int
	b.DB.QueryRow(`SELECT COUNT(*) FROM objects WHERE urn=? AND state='live'`, v.Object.URN).Scan(&n)
	if n != 1 {
		t.Fatalf("object must be live after the loop: %d", n)
	}
}

func TestHardenedPusherRefusesLoopbackAndHTTP(t *testing.T) {
	// The default D-72 client: http scheme fails fast; a loopback
	// https URL is refused at dial time by the Control hook.
	db, err := store.Open("sqlite3", "file:"+t.TempDir()+"/o.db?_fk=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seed, _ := hex.DecodeString(seedSNBS)
	kid := core.KID(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	db.Exec(`INSERT INTO own_keys (kid, seed, roles) VALUES (?,?,?)`, kid, seed, `["bs"]`)
	db.Exec(`INSERT INTO reservations_out (id, urn, target_url, token, max_size, expires, envelope_id, state)
		 VALUES (1,'urn:mlet:bx','http://x.example/i','tk',1,'2027-01-01T00:00:00Z','e','pending')`)
	p := &Pusher{DB: db, Domain: "origin.example", MaxAttempts: 1}
	if err := p.Push(context.Background(), 1, strings.NewReader("A")); err == nil {
		t.Fatal("http target must be refused (D-72)")
	}
	db.Exec(`UPDATE reservations_out SET target_url='https://127.0.0.1:443/i', state='pending' WHERE id=1`)
	if err := p.Push(context.Background(), 1, strings.NewReader("A")); err == nil ||
		!strings.Contains(err.Error(), "not completed") {
		t.Fatalf("loopback https must fail at dial: %v", err)
	}
}
