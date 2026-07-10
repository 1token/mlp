package sn

// S4.4 acceptance: dispatching the TV-001 Signed Envelope to a
// target.example SN reproduces TV-002 verdict 1 byte-identically
// (708 canonical bytes, exact signature); the recipient-accept
// upgrade reproduces verdict 2 (923 bytes) and mints the reservation
// row; the §3.4.4 validation sequence maps every failure to its §7.3
// problem; D-74 retry idempotency re-serves the snapshot; the origin
// side verifies verdicts, enforces the §7.6 transition table
// (including invalid-transition on deny→grant), honors supersession
// order, and materializes granted Reservations.

import (
	"context"
	"crypto/ed25519"
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
	"medialet.org/mlp/core"
	"medialet.org/mlp/discovery"
	"medialet.org/mlp/store"
)

const (
	tv001Path = "../../conformance/vectors/mlp-tv-001.json"
	tv002Path = "../../conformance/vectors/mlp-tv-002.json"

	seedAuthor = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60" // RFC 8032 TEST 1
	seedSN     = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb" // RFC 8032 TEST 2
	seedTarget = "c5aa8df43f9f837bedb7442f31dcb7b166d38535076f094b85ce3a2e0b4458f7" // RFC 8032 TEST 3

	mediaURN = "urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y"
	envID    = "019f2c92-2c88-7c16-a1fe-4548abf07edd"
)

func loadJSON(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}

func canonical(t *testing.T, raw []byte) []byte {
	t.Helper()
	c, err := core.Canonicalize(raw)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	return c
}

// newSN builds an SN over a fresh migrated store with a resolver
// whose fetcher cannot reach anything: every resolution must be
// served by the seeded discovery cache.
func newSN(t *testing.T, domain string, clock *time.Time) *SN {
	t.Helper()
	db, err := store.Open("sqlite3", "file:"+t.TempDir()+"/mlp.db?_fk=1")
	if err != nil {
		t.Fatalf("open+migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	r := &discovery.Resolver{
		DB:        db,
		Fetcher:   discovery.NewFetcher(), // production profile; test domains are unfetchable
		Supported: []string{"0.1"},
		Now:       func() time.Time { return *clock },
	}
	return &SN{
		DB:       db,
		Resolver: r,
		Domain:   domain,
		Now:      func() time.Time { return *clock },
	}
}

func seedDomainCache(t *testing.T, s *SN, domain string, doc []byte, now time.Time) {
	t.Helper()
	parsed, err := discovery.ParseDocument(doc, domain, []string{"0.1"})
	if err != nil {
		t.Fatalf("seed document invalid: %v", err)
	}
	mustExec(t, s, `INSERT OR REPLACE INTO domain_docs (domain, doc, fetched_at, expires_at) VALUES (?,?,?,?)`,
		domain, string(doc), now.Format(time.RFC3339), now.Add(23*time.Hour).Format(time.RFC3339))
	for _, k := range parsed.Keys {
		roles, _ := json.Marshal(k.Roles)
		mustExec(t, s, `INSERT OR REPLACE INTO domain_keys (domain, kid, key, roles) VALUES (?,?,?,?)`,
			domain, k.KID, k.Key, string(roles))
	}
}

func mustExec(t *testing.T, s *SN, q string, args ...any) {
	t.Helper()
	if _, err := s.DB.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func seedOwnKey(t *testing.T, s *SN, seedHex string, roles string) string {
	t.Helper()
	seed, _ := hex.DecodeString(seedHex)
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	kid := core.KID(pub)
	mustExec(t, s, `INSERT INTO own_keys (kid, seed, roles) VALUES (?,?,?)`, kid, seed, roles)
	return kid
}

// newTargetSN assembles the TV-002 issuing SN: target.example, novak's
// mailbox, the TEST 3 sn key, origin.example's Domain Document seeded
// in the discovery cache, and the deterministic TV-002 hooks.
func newTargetSN(t *testing.T, clock *time.Time) *SN {
	t.Helper()
	tv1 := loadJSON(t, tv001Path)
	s := newSN(t, "target.example", clock)
	s.IngestBase = "https://bs.target.example/ingest/"
	mustExec(t, s, `INSERT INTO stores (id, name) VALUES (1, 'default')`)
	mustExec(t, s, `INSERT INTO mailboxes (id, local_part, created) VALUES (1, 'novak', '2026-01-01T00:00:00Z')`)
	seedOwnKey(t, s, seedTarget, `["sn","bs"]`)
	seedDomainCache(t, s, "origin.example", tv1["domain_document"], *clock)
	return s
}

func dispatchEnvelope(t *testing.T) []byte {
	t.Helper()
	return loadJSON(t, tv001Path)["signed_envelope"]
}

// --- TV-002 byte-exact conformance -----------------------------------

func TestDispatchReproducesTV002Verdict1(t *testing.T) {
	tv2 := loadJSON(t, tv002Path)
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := newTargetSN(t, &clock)
	s.NewVerdictID = func(time.Time) string { return "019f2c92-3070-7d18-adda-f5b677a35e4a" }

	got, prob := s.ProcessDispatch(context.Background(), dispatchEnvelope(t))
	if prob != nil {
		t.Fatalf("dispatch: %v", prob)
	}
	want := canonical(t, tv2["signed_verdict_1"])
	if string(got) != string(want) {
		t.Fatalf("verdict 1 not byte-identical to TV-002:\n got %s\nwant %s", got, want)
	}
	if len(got) != 708 {
		t.Fatalf("verdict 1 canonical size %d, want 708", len(got))
	}
	// The Delivery Record carries both verification results (D-32).
	var aRes, hRes string
	if err := s.DB.QueryRow(`SELECT author_sig_result, hop_sig_result FROM envelopes_in WHERE envelope_id=?`, envID).
		Scan(&aRes, &hRes); err != nil || aRes != "ok" || hRes != "ok" {
		t.Fatalf("delivery record: %v %q %q", err, aRes, hRes)
	}
}

func TestRecipientAcceptReproducesTV002Verdict2(t *testing.T) {
	tv2 := loadJSON(t, tv002Path)
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := newTargetSN(t, &clock)
	s.NewVerdictID = func(time.Time) string { return "019f2c92-3070-7d18-adda-f5b677a35e4a" }
	if _, prob := s.ProcessDispatch(context.Background(), dispatchEnvelope(t)); prob != nil {
		t.Fatalf("dispatch: %v", prob)
	}

	// The recipient's accept action at 12:30, with the vector's
	// deterministic reservation secrets.
	clock = time.Date(2026, 7, 4, 12, 30, 0, 0, time.UTC)
	s.NewVerdictID = func(time.Time) string { return "019f2d1b-6d40-7dae-a190-9b835c6df3f6" }
	s.NewReservationSecret = func() (string, string) {
		return "Yam_b2ATZeRdhaofjfPPEasHKNDlGBAB", "24c372e9a5a3c559"
	}
	got, err := s.RecipientAccept(context.Background(), "origin.example", envID)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	want := canonical(t, tv2["signed_verdict_2"])
	if string(got) != string(want) {
		t.Fatalf("verdict 2 not byte-identical to TV-002:\n got %s\nwant %s", got, want)
	}
	if len(got) != 923 {
		t.Fatalf("verdict 2 canonical size %d, want 923", len(got))
	}
	// The minted reservation is stored hash-only (D-192), pending.
	var state string
	if err := s.DB.QueryRow(`SELECT state FROM reservations_in WHERE token_hash=?`,
		tokenHash("Yam_b2ATZeRdhaofjfPPEasHKNDlGBAB")).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("reservation row: %v %q", err, state)
	}
}

func TestVerifyTV002Verdicts(t *testing.T) {
	tv2 := loadJSON(t, tv002Path)
	clock := time.Date(2026, 7, 4, 12, 30, 1, 0, time.UTC)
	s := newSN(t, "origin.example", &clock)
	seedDomainCache(t, s, "target.example", tv2["target_domain_document"], clock)

	for _, key := range []string{"signed_verdict_1", "signed_verdict_2"} {
		if _, prob := s.ParseVerdict(context.Background(), tv2[key], clock); prob != nil {
			t.Fatalf("%s must verify: %v", key, prob)
		}
	}
	tampered := strings.Replace(string(tv2["signed_verdict_1"]), `"accepted"`, `"rejected"`, 1)
	if _, prob := s.ParseVerdict(context.Background(), []byte(tampered), clock); prob == nil || prob.Code != "signature-invalid" {
		t.Fatalf("tampered verdict must fail signature-invalid: %v", prob)
	}
}

// --- §3.4.4 validation sequence and §7.3 problem mapping --------------

func mutateEnvelope(t *testing.T, f func(env, medialet map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(dispatchEnvelope(t), &doc); err != nil {
		t.Fatal(err)
	}
	env := doc["envelope"].(map[string]any)
	med := env["medialet"].(map[string]any)["medialet"].(map[string]any)
	f(env, med)
	b, _ := json.Marshal(doc)
	return b
}

func TestValidationFailureMatrix(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	cases := []struct {
		name   string
		body   func(t *testing.T) []byte
		status int
		code   string
	}{
		{"oversized", func(t *testing.T) []byte {
			return append(dispatchEnvelope(t), strings.Repeat(" ", MaxEnvelopeBytes)...)
		}, 413, "envelope-too-large"},
		{"not JSON", func(t *testing.T) []byte { return []byte("{") }, 400, "malformed"},
		{"version", func(t *testing.T) []byte {
			return mutateEnvelope(t, func(e, m map[string]any) { e["mlp"] = "0.9" })
		}, 400, "version-unsupported"},
		{"non-local recipient", func(t *testing.T) []byte {
			return mutateEnvelope(t, func(e, m map[string]any) { e["envelope_to"] = []any{"novak@other.example"} })
		}, 400, "malformed"},
		{"mixed domains", func(t *testing.T) []byte {
			return mutateEnvelope(t, func(e, m map[string]any) {
				e["envelope_to"] = []any{"novak@target.example", "x@other.example"}
			})
		}, 400, "malformed"},
		{"recipient cap", func(t *testing.T) []byte {
			return mutateEnvelope(t, func(e, m map[string]any) {
				to := make([]any, 129)
				for i := range to {
					to[i] = fmt.Sprintf("u%d@target.example", i)
				}
				e["envelope_to"] = to
			})
		}, 400, "malformed"},
		{"skew", func(t *testing.T) []byte {
			return mutateEnvelope(t, func(e, m map[string]any) { e["created"] = "2026-07-01T10:00:05Z" })
		}, 400, "timestamp-skew"},
		{"hop sig tamper", func(t *testing.T) []byte {
			body := string(dispatchEnvelope(t))
			return []byte(strings.Replace(body, "TiQzJ3TUxh0", "TiQzJ3TUxh1", 1))
		}, 401, "signature-invalid"},
		{"author sig tamper", func(t *testing.T) []byte {
			body := string(dispatchEnvelope(t))
			return []byte(strings.Replace(body, "kJ5A09wU5Tc", "kJ5A09wU5Td", 2))
		}, 401, "signature-invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTargetSN(t, &clock)
			_, prob := s.ProcessDispatch(context.Background(), tc.body(t))
			if prob == nil || prob.Status != tc.status || prob.Code != tc.code {
				t.Fatalf("want %d %s, got %v", tc.status, tc.code, prob)
			}
		})
	}
}

func TestDiscoveryFailureIs502(t *testing.T) {
	// An origin whose Domain Document is not cached and cannot be
	// fetched: verified nothing → unsigned 502, never a verdict.
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := newTargetSN(t, &clock)
	body := signedEnvelopeFrom(t, "unknown.example", "e-unknown-1", clock)
	_, prob := s.ProcessDispatch(context.Background(), body)
	if prob == nil || prob.Status != 502 || prob.Code != "discovery-failed" {
		t.Fatalf("want 502 discovery-failed, got %v", prob)
	}
}

// signedEnvelopeFrom builds a schema-valid signed dispatch from an
// arbitrary origin using the TV-001 test keys.
func signedEnvelopeFrom(t *testing.T, origin, envelopeID string, now time.Time) []byte {
	t.Helper()
	authorSeed, _ := hex.DecodeString(seedAuthor)
	snSeed, _ := hex.DecodeString(seedSN)
	authorPriv := ed25519.NewKeyFromSeed(authorSeed)
	snPriv := ed25519.NewKeyFromSeed(snSeed)
	created := now.Format(time.RFC3339)

	medialet := map[string]any{
		"mlp": "0.1", "id": envelopeID + "-m", "author": "petra@" + origin,
		"created": created,
		"body":    map[string]any{"profile": "mlp-html/1", "content": "<p>hi</p>"},
	}
	aSig, _, err := core.SignDoc(authorPriv, "author/1", core.KID(authorPriv.Public().(ed25519.PublicKey)), created, medialet)
	if err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"mlp": "0.1", "envelope_id": envelopeID, "created": created,
		"origin": origin, "envelope_to": []any{"novak@target.example"},
		"medialet": map[string]any{"medialet": medialet, "signature": aSig},
	}
	hSig, _, err := core.SignDoc(snPriv, "hop/1", core.KID(snPriv.Public().(ed25519.PublicKey)), created, envelope)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := core.CanonicalizeValue(map[string]any{"envelope": envelope, "signature": hSig})
	if err != nil {
		t.Fatal(err)
	}
	return canon
}

// --- Policy outcomes (§7.7 defaults) ----------------------------------

func TestUnknownRecipientAndCompleteness(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := newTargetSN(t, &clock)
	body := mutateEnvelope(t, func(e, m map[string]any) {
		e["envelope_to"] = []any{"novak@target.example", "ghost@target.example"}
	})
	body = resign(t, body)
	got, prob := s.ProcessDispatch(context.Background(), body)
	if prob != nil {
		t.Fatalf("dispatch: %v", prob)
	}
	pv, prob := parseOwn(t, got)
	if prob != nil {
		t.Fatal(prob)
	}
	if pv.Message != "accepted" || len(pv.Recipients) != 2 {
		t.Fatalf("summary/completeness: %q %d", pv.Message, len(pv.Recipients))
	}
	if pv.Recipients[0].Verdict != "accepted" ||
		pv.Recipients[1].Verdict != "rejected" || pv.Recipients[1].Reason != "unknown-recipient" {
		t.Fatalf("per-recipient outcomes wrong: %+v", pv.Recipients)
	}
	if len(pv.Media) != 1 || pv.Media[0].Verdict != "defer" || pv.Media[0].Reason != "pending-acceptance" {
		t.Fatalf("Tier-2 media default wrong: %+v", pv.Media)
	}
}

func TestTier1GrantAndHave(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := newTargetSN(t, &clock)
	// Prior outbound correspondence: novak → petra (mailbox keys, D-55).
	mustExec(t, s, `INSERT INTO correspondents (mailbox_id, addr, first_outbound_at) VALUES (1, 'petra@origin.example', '2026-06-01T00:00:00Z')`)

	got, prob := s.ProcessDispatch(context.Background(), dispatchEnvelope(t))
	if prob != nil {
		t.Fatalf("dispatch: %v", prob)
	}
	pv, _ := parseOwn(t, got)
	if pv.Media[0].Verdict != "grant" || pv.Media[0].Reservation == nil {
		t.Fatalf("Tier-1 must grant with a reservation: %+v", pv.Media[0])
	}
	r := pv.Media[0].Reservation
	if r.URN != mediaURN || r.MaxSize != 36 || !strings.HasPrefix(r.TargetURL, "https://bs.target.example/ingest/") {
		t.Fatalf("reservation shape wrong: %+v", r)
	}
	var n int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM reservations_in WHERE token_hash=?`, tokenHash(r.Token)).Scan(&n); err != nil || n != 1 {
		t.Fatalf("token must be stored as hash only: %d %v", n, err)
	}

	// Possession disclosed at Tier 1 (D-29 masking is for strangers).
	s2 := newTargetSN(t, &clock)
	mustExec(t, s2, `INSERT INTO correspondents (mailbox_id, addr, first_outbound_at) VALUES (1, 'petra@origin.example', '2026-06-01T00:00:00Z')`)
	mustExec(t, s2, `INSERT INTO objects (urn, size, state, store_id, created_at, verified_at) VALUES (?,36,'live',1,'2026-06-01T00:00:00Z','2026-06-01T00:00:00Z')`, mediaURN)
	got2, prob := s2.ProcessDispatch(context.Background(), dispatchEnvelope(t))
	if prob != nil {
		t.Fatal(prob)
	}
	pv2, _ := parseOwn(t, got2)
	if pv2.Media[0].Verdict != "have" {
		t.Fatalf("Tier-1 possession must be disclosed as have: %+v", pv2.Media[0])
	}
}

func TestBlockedSenderQuarantined(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := newTargetSN(t, &clock)
	mustExec(t, s, `INSERT INTO correspondents (mailbox_id, addr, tier_override) VALUES (1, 'petra@origin.example', 'block')`)
	got, prob := s.ProcessDispatch(context.Background(), dispatchEnvelope(t))
	if prob != nil {
		t.Fatal(prob)
	}
	pv, _ := parseOwn(t, got)
	if pv.Message != "quarantined" || pv.Recipients[0].Verdict != "quarantined" {
		t.Fatalf("blocked sender must quarantine: %q %+v", pv.Message, pv.Recipients)
	}
}

func parseOwn(t *testing.T, doc []byte) (*ParsedVerdict, *Problem) {
	t.Helper()
	v, err := core.ParseDialect(doc)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := v.(map[string]any)["payload"].(map[string]any)
	pv, err := parseVerdictPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	return pv, nil
}

// resign rebuilds both signatures over a mutated envelope with the
// TV-001 test keys (mutations that must survive to policy).
func resign(t *testing.T, body []byte) []byte {
	t.Helper()
	v, err := core.ParseDialect(body)
	if err != nil {
		t.Fatal(err)
	}
	top := v.(map[string]any)
	env := top["envelope"].(map[string]any)
	med := env["medialet"].(map[string]any)["medialet"].(map[string]any)

	authorSeed, _ := hex.DecodeString(seedAuthor)
	snSeed, _ := hex.DecodeString(seedSN)
	authorPriv := ed25519.NewKeyFromSeed(authorSeed)
	snPriv := ed25519.NewKeyFromSeed(snSeed)
	aSig, _, err := core.SignDoc(authorPriv, "author/1", core.KID(authorPriv.Public().(ed25519.PublicKey)), med["created"].(string), med)
	if err != nil {
		t.Fatal(err)
	}
	env["medialet"].(map[string]any)["signature"] = aSig
	hSig, _, err := core.SignDoc(snPriv, "hop/1", core.KID(snPriv.Public().(ed25519.PublicKey)), env["created"].(string), env)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := core.CanonicalizeValue(map[string]any{"envelope": env, "signature": hSig})
	if err != nil {
		t.Fatal(err)
	}
	return canon
}

// --- D-74 retry idempotency -------------------------------------------

func TestRetryIdempotencyAndReplay(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := newTargetSN(t, &clock)
	s.NewVerdictID = func(time.Time) string { return "019f2c92-3070-7d18-adda-f5b677a35e4a" }

	first, prob := s.ProcessDispatch(context.Background(), dispatchEnvelope(t))
	if prob != nil {
		t.Fatal(prob)
	}
	// Same (origin, envelope_id), same content: the current snapshot.
	again, prob := s.ProcessDispatch(context.Background(), dispatchEnvelope(t))
	if prob != nil || string(again) != string(first) {
		t.Fatalf("retry must re-serve the snapshot: %v", prob)
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM envelopes_in`).Scan(&n)
	if n != 1 {
		t.Fatalf("retry must not create a second delivery record: %d", n)
	}
	// Same identifiers, different content: the D-20 replay attack.
	tampered := resign(t, mutateEnvelope(t, func(e, m map[string]any) { m["subject"] = "swapped" }))
	if _, prob := s.ProcessDispatch(context.Background(), tampered); prob == nil || prob.Code != "replay" || prob.Status != 409 {
		t.Fatalf("content substitution must be replay: %v", prob)
	}
	// Beyond the idempotency window, even identical retries are replay.
	clock = clock.Add(IdempotencyWindow + time.Hour)
	if _, prob := s.ProcessDispatch(context.Background(), dispatchEnvelope(t)); prob == nil || prob.Code != "replay" {
		t.Fatalf("post-window retry must be replay: %v", prob)
	}
}

// --- Origin side: §7.6 updates ----------------------------------------

// newOriginSN assembles origin.example holding the TV-001 dispatch
// record, with target.example's keys cached.
func newOriginSN(t *testing.T, clock *time.Time) *SN {
	t.Helper()
	tv1, tv2 := loadJSON(t, tv001Path), loadJSON(t, tv002Path)
	s := newSN(t, "origin.example", clock)
	mustExec(t, s, `INSERT INTO stores (id, name) VALUES (1, 'default')`)
	seedDomainCache(t, s, "target.example", tv2["target_domain_document"], *clock)

	sm := canonical(t, extract(t, tv1, "signed_envelope", "envelope", "medialet"))
	ca := core.URNMlet(sm)
	mustExec(t, s, `INSERT INTO medialets (content_address, author, medialet_id, created, raw) VALUES (?,?,?,?,?)`,
		ca, "petra@origin.example", "019f2c92-1900-7b0f-8f1e-30c7d7d77f8c", "2026-07-04T10:00:00Z", sm)
	mustExec(t, s, `INSERT INTO dispatches (envelope_id, target_domain, medialet_ca, created, envelope_canonical, hop_sig_value, hop_kid)
		VALUES (?,?,?,?,?,?,?)`,
		envID, "target.example", ca, "2026-07-04T10:00:05Z", canonical(t, tv1["signed_envelope"]), "x", "x")
	return s
}

func extract(t *testing.T, m map[string]json.RawMessage, path ...string) json.RawMessage {
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

// signSnapshot builds a signed §7.6 snapshot with the TEST 3 key.
func signSnapshot(t *testing.T, verdictID, created, mediaVerdict string, reservation *Reservation) []byte {
	t.Helper()
	media := []MediaOutcome{{URN: mediaURN, Verdict: mediaVerdict, Reservation: reservation}}
	payload := BuildVerdictPayload("target.example", "origin.example", envID, verdictID, created,
		[]RecipientOutcome{{Addr: "novak@target.example", Verdict: "accepted"}}, media)
	seed, _ := hex.DecodeString(seedTarget)
	priv := ed25519.NewKeyFromSeed(seed)
	sig, _, err := core.SignDoc(priv, "verdict/1", core.KID(priv.Public().(ed25519.PublicKey)), created, payload)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := core.CanonicalizeValue(map[string]any{"payload": payload, "signature": sig})
	if err != nil {
		t.Fatal(err)
	}
	return canon
}

func TestOriginTransitionWalk(t *testing.T) {
	tv2 := loadJSON(t, tv002Path)
	clock := time.Date(2026, 7, 4, 10, 0, 7, 0, time.UTC)
	s := newOriginSN(t, &clock)
	ctx := context.Background()

	// Synchronous reply recorded: baseline defer.
	if prob := s.RecordDispatchResponse(ctx, tv2["signed_verdict_1"]); prob != nil {
		t.Fatalf("record response: %v", prob)
	}
	// The upgrade snapshot: defer → grant; reservation materialized.
	clock = time.Date(2026, 7, 4, 12, 30, 1, 0, time.UTC)
	if prob := s.ProcessVerdictUpdate(ctx, tv2["signed_verdict_2"]); prob != nil {
		t.Fatalf("upgrade update: %v", prob)
	}
	var token, url string
	if err := s.DB.QueryRow(`SELECT token, target_url FROM reservations_out WHERE envelope_id=? AND urn=?`,
		envID, mediaURN).Scan(&token, &url); err != nil ||
		token != "Yam_b2ATZeRdhaofjfPPEasHKNDlGBAB" ||
		url != "https://bs.target.example/ingest/24c372e9a5a3c559" {
		t.Fatalf("reservation not materialized: %v %q %q", err, token, url)
	}
	// Re-POST of the same update: idempotent, no duplicate reservation.
	if prob := s.ProcessVerdictUpdate(ctx, tv2["signed_verdict_2"]); prob != nil {
		t.Fatalf("idempotent re-POST: %v", prob)
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM reservations_out`).Scan(&n)
	if n != 1 {
		t.Fatalf("duplicate snapshot minted again: %d rows", n)
	}
	// Revocation: grant → deny is legal.
	deny := signSnapshot(t, "019f2d1b-0000-7000-8000-000000000001", "2026-07-04T13:00:00Z", "deny", nil)
	if prob := s.ProcessVerdictUpdate(ctx, deny); prob != nil {
		t.Fatalf("grant→deny must be legal: %v", prob)
	}
	// deny is terminal: deny → grant is invalid-transition.
	regrant := signSnapshot(t, "019f2d1b-0000-7000-8000-000000000002", "2026-07-04T13:30:00Z", "grant",
		&Reservation{URN: mediaURN, MaxSize: 36, TargetURL: "https://bs.target.example/ingest/aa", Token: "tk", Expires: "2026-07-07T13:30:00Z"})
	if prob := s.ProcessVerdictUpdate(ctx, regrant); prob == nil || prob.Code != "invalid-transition" {
		t.Fatalf("deny→grant must be invalid-transition: %v", prob)
	}
	// A stale snapshot (older created) is stored but alters nothing.
	stale := signSnapshot(t, "019f2d1b-0000-7000-8000-000000000003", "2026-07-04T11:00:00Z", "defer", nil)
	if prob := s.ProcessVerdictUpdate(ctx, stale); prob != nil {
		t.Fatalf("stale snapshot must be acknowledged: %v", prob)
	}
	state, _, _, prob := s.currentMediaState(ctx, "target.example", envID)
	if prob != nil || state[mediaURN] != "deny" {
		t.Fatalf("stale snapshot must not alter state: %v %v", state, prob)
	}
}

func TestUpdateUnknownEnvelope(t *testing.T) {
	tv2 := loadJSON(t, tv002Path)
	clock := time.Date(2026, 7, 4, 12, 30, 1, 0, time.UTC)
	s := newSN(t, "origin.example", &clock)
	seedDomainCache(t, s, "target.example", tv2["target_domain_document"], clock)
	// No dispatches row at all.
	if prob := s.ProcessVerdictUpdate(context.Background(), tv2["signed_verdict_2"]); prob == nil ||
		prob.Status != 404 || prob.Code != "unknown-envelope" {
		t.Fatalf("want 404 unknown-envelope: %v", prob)
	}
}

// --- HTTP surface (§7.2/§7.3) ------------------------------------------

func TestHTTPDispatch(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := newTargetSN(t, &clock)
	ts := httptest.NewServer(Handler(s))
	defer ts.Close()

	// Wrong content type: 415 problem+json.
	resp, err := http.Post(ts.URL+"/dispatch", "application/json", strings.NewReader("{}"))
	if err != nil || resp.StatusCode != 415 {
		t.Fatalf("want 415: %v %v", err, resp.Status)
	}
	resp.Body.Close()

	// A verified dispatch: 200 with the signed verdict.
	resp, err = http.Post(ts.URL+"/dispatch", ctEnvelope, strings.NewReader(string(dispatchEnvelope(t))))
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != ctVerdict {
		t.Fatalf("want 200 %s: %v %v", ctVerdict, err, resp.Status)
	}
	resp.Body.Close()

	// Malformed body: 400 with the reason-code URI.
	resp, err = http.Post(ts.URL+"/dispatch", ctEnvelope, strings.NewReader("{"))
	if err != nil || resp.StatusCode != 400 || resp.Header.Get("Content-Type") != ctProblem {
		t.Fatalf("want 400 problem+json: %v %v", err, resp.Status)
	}
	var p map[string]any
	json.NewDecoder(resp.Body).Decode(&p)
	resp.Body.Close()
	if p["type"] != "urn:mlp:err:malformed" {
		t.Fatalf("problem type: %v", p["type"])
	}
}
