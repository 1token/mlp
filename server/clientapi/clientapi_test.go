package clientapi

// S4.7 acceptance: the D-170 conventions bite (auth-required without
// a session, csrf-required without the header, Idempotency-Key
// replays without re-execution); PBKDF2 matches the RFC 7914 §11
// vectors; sessions mint/list/revoke; the SSE feed delivers typed
// events live and replays exactly from Last-Event-ID; the accept
// endpoint drives the real machinery — a direct TV-001 delivery
// yields the byte-exact TV-002 upgrade snapshot POSTed to the origin,
// and a forwarded TV-004 delivery triggers the §9.3 delegation flow;
// deliveries/timeline read the D-149 facts the sn layer now records.

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"medialet.org/mlp/bs"
	"medialet.org/mlp/core"
	"medialet.org/mlp/discovery"
	"medialet.org/mlp/sn"
	"medialet.org/mlp/store"
)

const (
	tv001Path = "../../conformance/vectors/mlp-tv-001.json"
	tv002Path = "../../conformance/vectors/mlp-tv-002.json"
	tv004Path = "../../conformance/vectors/mlp-tv-004.json"
	seedT3    = "c5aa8df43f9f837bedb7442f31dcb7b166d38535076f094b85ce3a2e0b4458f7"
	seedFinal = "f5e5767cf153319517630f226876b86c8160cc583bc013744c6bf255f5cc0ee5"
	mediaURN  = "urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y"
	envID     = "019f2c92-2c88-7c16-a1fe-4548abf07edd"
	fwdEnvID  = "019f2c92-3458-7ba2-9bec-0190697bca43"
)

func loadJSON(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func canonical(t *testing.T, raw []byte) []byte {
	t.Helper()
	c, err := core.Canonicalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// newAPI assembles a Server over a fresh store for `domain`, with the
// named mailbox, a password, an sn key from seedHex, and the given
// domain documents cached.
func newAPI(t *testing.T, domain, localPart, seedHex string, clock *time.Time, cache map[string]json.RawMessage) *Server {
	t.Helper()
	db, err := store.Open("sqlite3", "file:"+t.TempDir()+"/mlp.db?_fk=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, `INSERT INTO stores (id, name) VALUES (1, 'default')`)
	mustExec(t, db, `INSERT INTO mailboxes (id, local_part, created) VALUES (1, ?, '2026-01-01T00:00:00Z')`, localPart)
	hash, err := HashPassword("correct horse", 1000)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `INSERT INTO password_fallback (mailbox_id, hash) VALUES (1, ?)`, hash)
	seed, _ := hex.DecodeString(seedHex)
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	mustExec(t, db, `INSERT INTO own_keys (kid, seed, roles) VALUES (?,?,?)`, core.KID(pub), seed, `["sn","bs"]`)
	for d, doc := range cache {
		parsed, err := discovery.ParseDocument(doc, d, []string{"0.1"})
		if err != nil {
			t.Fatalf("cache %s: %v", d, err)
		}
		mustExec(t, db, `INSERT INTO domain_docs (domain, doc, fetched_at, expires_at) VALUES (?,?,?,?)`,
			d, string(doc), clock.Format(time.RFC3339), clock.Add(23*time.Hour).Format(time.RFC3339))
		for _, k := range parsed.Keys {
			roles, _ := json.Marshal(k.Roles)
			mustExec(t, db, `INSERT INTO domain_keys (domain, kid, key, roles) VALUES (?,?,?,?)`,
				d, k.KID, k.Key, string(roles))
		}
	}
	node := &sn.SN{
		DB: db,
		Resolver: &discovery.Resolver{
			DB: db, Fetcher: discovery.NewFetcher(), Supported: []string{"0.1"},
			Now: func() time.Time { return *clock },
		},
		Domain:     domain,
		IngestBase: "https://bs." + domain + "/ingest/",
		Now:        func() time.Time { return *clock },
	}
	hub := NewHub(db)
	hub.Now = func() time.Time { return *clock }
	blob := &bs.BS{DB: db, Root: t.TempDir(), Now: func() time.Time { return *clock }}
	return &Server{DB: db, SN: node, BS: blob, Hub: hub, Now: func() time.Time { return *clock }, PasswordIterations: 1000}
}

// login mints a session and returns an authenticated request helper.
func login(t *testing.T, ts *httptest.Server, address string) func(method, path, body string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/password",
		strings.NewReader(`{"address":"`+address+`","password":"correct horse"}`))
	req.Header.Set("X-MLP-Client", "test")
	resp, err := ts.Client().Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("login: %v %v", err, resp.Status)
	}
	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	resp.Body.Close()
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	return func(method, path, body string, hdr map[string]string) *http.Response {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		req.AddCookie(cookie)
		req.Header.Set("X-MLP-Client", "test")
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}
}

func TestPBKDF2AgainstRFC7914Vectors(t *testing.T) {
	got := pbkdf2SHA256([]byte("passwd"), []byte("salt"), 1, 64)
	want := "55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc49ca9cccf179b645991664b39d77ef317c71b845b1e30bd509112041d3a19783"
	if hex.EncodeToString(got) != want {
		t.Fatalf("vector 1 mismatch: %x", got)
	}
	got = pbkdf2SHA256([]byte("Password"), []byte("NaCl"), 80000, 64)
	want = "4ddcd8f60b98be21830cee5ef22701f9641a4418d04c0414aeff08876b34ab56a1d425a1225833549adb841b51c9b3176a272bdebba1d078478f62b397f33c8d"
	if hex.EncodeToString(got) != want {
		t.Fatalf("vector 2 mismatch: %x", got)
	}
	h, err := HashPassword("s3cret", 1000)
	if err != nil || !verifyPassword(h, "s3cret") || verifyPassword(h, "wrong") {
		t.Fatalf("round trip: %v %q", err, h)
	}
}

func TestConventions(t *testing.T) {
	clock := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	s := newAPI(t, "x.example", "user", seedT3, &clock, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// No session → 401 auth-required problem+json.
	resp, _ := http.Get(ts.URL + "/api/v1/quota")
	if resp.StatusCode != 401 {
		t.Fatalf("want 401: %d", resp.StatusCode)
	}
	var p map[string]any
	json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()
	if p["type"] != "urn:mlp:err:auth-required" {
		t.Fatalf("problem type: %v", p["type"])
	}

	// Login without the CSRF header → 403 csrf-required.
	resp, _ = http.Post(ts.URL+"/api/v1/auth/password", "application/json",
		strings.NewReader(`{"address":"user@x.example","password":"correct horse"}`))
	if resp.StatusCode != 403 {
		t.Fatalf("want 403 csrf-required: %d", resp.StatusCode)
	}
	resp.Body.Close()

	do := login(t, ts, "user@x.example")

	// Wrong password is indistinguishable from an unknown address.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/password",
		strings.NewReader(`{"address":"user@x.example","password":"nope"}`))
	req.Header.Set("X-MLP-Client", "test")
	resp, _ = ts.Client().Do(req)
	bad1 := resp.StatusCode
	resp.Body.Close()
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/password",
		strings.NewReader(`{"address":"ghost@x.example","password":"nope"}`))
	req.Header.Set("X-MLP-Client", "test")
	resp, _ = ts.Client().Do(req)
	if bad1 != 401 || resp.StatusCode != 401 {
		t.Fatalf("credential failures must both be 401: %d %d", bad1, resp.StatusCode)
	}
	resp.Body.Close()

	resp = do(http.MethodPatch, "/api/v1/settings", `{"theme":"dark"}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("settings patch: %d", resp.StatusCode)
	}
	var settings map[string]any
	json.NewDecoder(resp.Body).Decode(&settings)
	resp.Body.Close()
	if settings["theme"] != "dark" {
		t.Fatalf("settings: %v", settings)
	}

	// Sessions: list shows the device; revoke-all signs out.
	resp = do(http.MethodGet, "/api/v1/sessions", "", nil)
	var sessions struct {
		Sessions []map[string]any `json:"sessions"`
	}
	json.NewDecoder(resp.Body).Decode(&sessions)
	resp.Body.Close()
	if len(sessions.Sessions) != 1 || sessions.Sessions[0]["current"] != true {
		t.Fatalf("sessions: %+v", sessions)
	}
	resp = do(http.MethodDelete, "/api/v1/sessions", "", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("revoke all: %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = do(http.MethodGet, "/api/v1/quota", "", nil)
	if resp.StatusCode != 401 {
		t.Fatalf("revoked session must be 401: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// directDeliveryAPI: target.example holding the TV-001 dispatch via
// the real ingest path — the API server for the "novak accepts"
// scenario.
func directDeliveryAPI(t *testing.T, clock *time.Time) *Server {
	t.Helper()
	tv1 := loadJSON(t, tv001Path)
	s := newAPI(t, "target.example", "novak", seedT3, clock,
		map[string]json.RawMessage{"origin.example": tv1["domain_document"]})
	s.SN.NewVerdictID = func(time.Time) string { return "019f2c92-3070-7d18-adda-f5b677a35e4a" }
	if _, prob := s.SN.ProcessDispatch(context.Background(), tv1["signed_envelope"]); prob != nil {
		t.Fatalf("TV-001 ingest: %v", prob)
	}
	return s
}

func TestAcceptDirectReproducesTV002Upgrade(t *testing.T) {
	tv2 := loadJSON(t, tv002Path)
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := directDeliveryAPI(t, &clock)

	clock = time.Date(2026, 7, 4, 12, 30, 0, 0, time.UTC)
	s.SN.NewVerdictID = func(time.Time) string { return "019f2d1b-6d40-7dae-a190-9b835c6df3f6" }
	s.SN.NewReservationSecret = func() (string, string) {
		return "Yam_b2ATZeRdhaofjfPPEasHKNDlGBAB", "24c372e9a5a3c559"
	}
	var posted []byte
	s.PostVerdict = func(ctx context.Context, origin string, doc []byte) error {
		if origin != "origin.example" {
			t.Fatalf("verdict posted to %q", origin)
		}
		posted = doc
		return nil
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "novak@target.example")

	resp := do(http.MethodPost, "/api/v1/o/"+mediaURN+"/accept", "{}",
		map[string]string{"Idempotency-Key": "accept-1"})
	if resp.StatusCode != 200 {
		t.Fatalf("accept: %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if body["mode"] != "upgraded" {
		t.Fatalf("mode: %v", body)
	}
	// The snapshot that left for the origin is TV-002 verdict 2,
	// byte-identical — the whole S4.4 machinery behind one API call.
	if string(posted) != string(canonical(t, tv2["signed_verdict_2"])) {
		t.Fatalf("posted snapshot is not TV-002 verdict 2")
	}
	// Idempotent replay: same response, no third snapshot, no fresh
	// reservation.
	resp = do(http.MethodPost, "/api/v1/o/"+mediaURN+"/accept", "{}",
		map[string]string{"Idempotency-Key": "accept-1"})
	var replay map[string]any
	json.NewDecoder(resp.Body).Decode(&replay)
	resp.Body.Close()
	if replay["mode"] != "upgraded" {
		t.Fatalf("replay: %v", replay)
	}
	var snapshots, reservations int
	s.DB.QueryRow(`SELECT COUNT(*) FROM verdicts WHERE direction='out'`).Scan(&snapshots)
	s.DB.QueryRow(`SELECT COUNT(*) FROM reservations_in`).Scan(&reservations)
	if snapshots != 2 || reservations != 1 {
		t.Fatalf("idempotency: snapshots=%d reservations=%d", snapshots, reservations)
	}
	// The journaled SSE event exists exactly once.
	var events int
	s.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE type='media.accepted'`).Scan(&events)
	if events != 1 {
		t.Fatalf("events: %d", events)
	}
}

func TestAcceptForwardedTriggersDelegation(t *testing.T) {
	tv1, tv2, tv4 := loadJSON(t, tv001Path), loadJSON(t, tv002Path), loadJSON(t, tv004Path)
	clock := time.Date(2026, 7, 4, 10, 30, 0, 0, time.UTC)
	s := newAPI(t, "final.example", "carol", seedFinal, &clock, map[string]json.RawMessage{
		"origin.example": tv1["domain_document"],
		"target.example": tv2["target_domain_document"],
	})
	if _, prob := s.SN.ProcessDispatch(context.Background(), tv4["signed_forwarded_envelope"]); prob != nil {
		t.Fatalf("forwarded ingest: %v", prob)
	}
	// A canned source: §9 correctness is S4.6's proof; the API layer
	// needs only the transport contract.
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"media":[{"urn":%q,"status":"will-push"}]}`, mediaURN)
	}))
	defer source.Close()
	s.SN.FulfillEndpoint = func(ctx context.Context, domain string) (string, error) {
		return source.URL + "/fulfill", nil
	}
	s.SN.FulfillClient = source.Client()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "carol@final.example")
	resp := do(http.MethodPost, "/api/v1/o/"+mediaURN+"/accept", "{}", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("accept: %d", resp.StatusCode)
	}
	var body struct {
		Mode     string              `json:"mode"`
		Outcomes []sn.FulfillOutcome `json:"outcomes"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if body.Mode != "delegated" || len(body.Outcomes) != 1 || body.Outcomes[0].Status != "will-push" {
		t.Fatalf("delegated accept: %+v", body)
	}
	// The requester minted its reservation (the ingesting party
	// always mints, D-82).
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM reservations_in WHERE pusher_domain='origin.example'`).Scan(&n)
	if n != 1 {
		t.Fatalf("reservation not minted: %d", n)
	}
}

func TestSSELiveAndResume(t *testing.T) {
	clock := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	s := newAPI(t, "x.example", "user", seedT3, &clock, nil)
	s.Hub.Heartbeat = 50 * time.Millisecond
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "user@x.example")

	// Live: connect, emit, receive the typed frame with its id.
	resp := do(http.MethodGet, "/api/v1/events", "", nil)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("events: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if err := s.Hub.Emit(context.Background(), 1, "test.ping", map[string]any{"n": 1}); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(resp.Body)
	frame := readFrame(t, reader)
	resp.Body.Close()
	if !strings.Contains(frame, "id: 1") || !strings.Contains(frame, "event: test.ping") ||
		!strings.Contains(frame, `data: {"n":1}`) {
		t.Fatalf("frame: %q", frame)
	}

	// Two more events while disconnected; resume from Last-Event-ID 1
	// replays exactly 2 and 3.
	s.Hub.Emit(context.Background(), 1, "test.ping", map[string]any{"n": 2})
	s.Hub.Emit(context.Background(), 1, "test.ping", map[string]any{"n": 3})
	resp = do(http.MethodGet, "/api/v1/events", "", map[string]string{"Last-Event-ID": "1"})
	reader = bufio.NewReader(resp.Body)
	f2, f3 := readFrame(t, reader), readFrame(t, reader)
	resp.Body.Close()
	if !strings.Contains(f2, `data: {"n":2}`) || !strings.Contains(f3, `data: {"n":3}`) {
		t.Fatalf("resume frames: %q %q", f2, f3)
	}
}

// readFrame reads one SSE frame (up to the blank line), skipping
// keep-alive comments.
func readFrame(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	var b strings.Builder
	lines := make(chan string)
	errs := make(chan error, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				errs <- err
				return
			}
			lines <- line
		}
	}()
	for {
		select {
		case <-deadline:
			t.Fatalf("SSE frame timeout; got %q", b.String())
		case err := <-errs:
			t.Fatalf("SSE read: %v (got %q)", err, b.String())
		case line := <-lines:
			if strings.HasPrefix(line, ":") {
				continue // heartbeat
			}
			if line == "\n" {
				if b.Len() > 0 {
					return b.String()
				}
				continue
			}
			b.WriteString(line)
		}
	}
}

func TestDeliveriesAndTimeline(t *testing.T) {
	tv1, tv2 := loadJSON(t, tv001Path), loadJSON(t, tv002Path)
	clock := time.Date(2026, 7, 4, 10, 0, 7, 0, time.UTC)
	// origin.example as sender: petra's mailbox, a delivery linked to
	// the TV-001 dispatch, TV-002's verdicts recorded through the
	// real origin-side path (which now writes the D-149 timeline).
	s := newAPI(t, "origin.example", "petra", seedT3, &clock,
		map[string]json.RawMessage{"target.example": tv2["target_domain_document"]})
	sm := canonical(t, extractRaw(t, tv1, "signed_envelope", "envelope", "medialet"))
	ca := core.URNMlet(sm)
	mustExec(t, s.DB, `INSERT INTO medialets (content_address, author, medialet_id, created, raw) VALUES (?,?,?,?,?)`,
		ca, "petra@origin.example", "m-1", "2026-07-04T10:00:00Z", sm)
	mustExec(t, s.DB, `INSERT INTO deliveries (id, mailbox_id, medialet_ca, job_tag, created) VALUES (7, 1, ?, 'novak wedding', '2026-07-04T10:00:05Z')`, ca)
	mustExec(t, s.DB, `INSERT INTO dispatches (envelope_id, target_domain, medialet_ca, created, envelope_canonical, hop_sig_value, hop_kid, delivery_id)
		VALUES (?,?,?,?,?,?,?,7)`, envID, "target.example", ca, "2026-07-04T10:00:05Z", canonical(t, tv1["signed_envelope"]), "x", "x")

	if prob := s.SN.RecordDispatchResponse(context.Background(), tv2["signed_verdict_1"]); prob != nil {
		t.Fatalf("record response: %v", prob)
	}
	clock = time.Date(2026, 7, 4, 12, 30, 1, 0, time.UTC)
	if prob := s.SN.ProcessVerdictUpdate(context.Background(), tv2["signed_verdict_2"]); prob != nil {
		t.Fatalf("update: %v", prob)
	}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "petra@origin.example")

	resp := do(http.MethodGet, "/api/v1/deliveries", "", nil)
	var list struct {
		Deliveries []struct {
			ID      int64 `json:"id"`
			Targets []struct {
				Domain   string `json:"domain"`
				Headline string `json:"headline"`
			} `json:"targets"`
		} `json:"deliveries"`
	}
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Deliveries) != 1 || list.Deliveries[0].ID != 7 ||
		list.Deliveries[0].Targets[0].Headline != "accepted" {
		t.Fatalf("deliveries: %+v", list)
	}

	resp = do(http.MethodGet, "/api/v1/deliveries/7", "", nil)
	var detail struct {
		Targets []struct {
			Media []map[string]any `json:"media"`
		} `json:"targets"`
	}
	json.NewDecoder(resp.Body).Decode(&detail)
	resp.Body.Close()
	if len(detail.Targets) != 1 || detail.Targets[0].Media[0]["verdict"] != "grant" {
		t.Fatalf("matrix must show the upgraded grant: %+v", detail)
	}

	resp = do(http.MethodGet, "/api/v1/deliveries/7/timeline", "", nil)
	var tl struct {
		Timeline []map[string]any `json:"timeline"`
	}
	json.NewDecoder(resp.Body).Decode(&tl)
	resp.Body.Close()
	if len(tl.Timeline) != 2 ||
		tl.Timeline[0]["kind"] != "verdict.received" || tl.Timeline[1]["kind"] != "verdict.updated" {
		t.Fatalf("timeline: %+v", tl.Timeline)
	}

	// Ownership: another mailbox's delivery is 404.
	mustExec(t, s.DB, `INSERT INTO mailboxes (id, local_part, created) VALUES (2, 'other', '2026-01-01T00:00:00Z')`)
	mustExec(t, s.DB, `UPDATE deliveries SET mailbox_id=2 WHERE id=7`)
	resp = do(http.MethodGet, "/api/v1/deliveries/7/timeline", "", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("foreign delivery must be 404: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func extractRaw(t *testing.T, m map[string]json.RawMessage, path ...string) json.RawMessage {
	t.Helper()
	cur := m[path[0]]
	for _, p := range path[1:] {
		var next map[string]json.RawMessage
		if err := json.Unmarshal(cur, &next); err != nil {
			t.Fatal(err)
		}
		cur = next[p]
	}
	return cur
}

func TestHaveAndQuota(t *testing.T) {
	clock := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	s := newAPI(t, "x.example", "user", seedT3, &clock, nil)
	mustExec(t, s.DB, `INSERT INTO objects (urn, size, state, store_id, created_at, verified_at)
		VALUES (?, 36, 'live', 1, '2026-07-01T00:00:00Z', '2026-07-01T00:00:00Z')`, mediaURN)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "user@x.example")

	resp := do(http.MethodGet, "/api/v1/objects/have?urn="+mediaURN, "", nil)
	var have map[string]any
	json.NewDecoder(resp.Body).Decode(&have)
	resp.Body.Close()
	if have["have"] != true {
		t.Fatalf("have: %v", have)
	}
	resp = do(http.MethodGet, "/api/v1/quota", "", nil)
	var quota struct {
		Stores []map[string]any `json:"stores"`
	}
	json.NewDecoder(resp.Body).Decode(&quota)
	resp.Body.Close()
	if len(quota.Stores) != 1 || quota.Stores[0]["used_bytes"].(float64) != 36 {
		t.Fatalf("quota: %+v", quota)
	}
}
