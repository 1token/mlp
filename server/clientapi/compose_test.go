package clientapi

// S4.10 acceptance — the composer closes the circle: composing
// Petra's draft through the real pipeline reproduces the TV-001
// Signed Medialet AND Signed Envelope byte-identically, dispatches
// to a live target SN, and records the byte-exact TV-002 verdict 1
// that comes back — the origin-side send path meeting the S4.4
// target machinery over real HTTP. Plus: the intra-domain upload
// door over the shared bs core (hash-first, have-check, resume,
// digest refusal), the D-135 possession gate, per-domain fan-out
// with one delivery, and the drafts lifecycle.

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"medialet.org/mlp/core"
	"medialet.org/mlp/discovery"
	"medialet.org/mlp/sn"
)

const seedAuthorKey = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60" // RFC 8032 TEST 1

// originAPI builds petra@origin.example's server with both signing
// roles (author = TEST 1, sn/bs = TEST 2 — exactly the TV-001 keys)
// and the object in possession.
func originAPI(t *testing.T, clock *time.Time) *Server {
	t.Helper()
	tv2 := loadJSON(t, tv002Path)
	s := newAPI(t, "origin.example", "petra", seedOriginSN, clock,
		map[string]json.RawMessage{"target.example": tv2["target_domain_document"]})
	aSeed, _ := hex.DecodeString(seedAuthorKey)
	aKID := core.KID(ed25519.NewKeyFromSeed(aSeed).Public().(ed25519.PublicKey))
	mustExec(t, s.DB, `INSERT INTO own_keys (kid, seed, roles) VALUES (?,?,?)`, aKID, aSeed, `["author"]`)
	mustExec(t, s.DB, `INSERT INTO objects (urn, size, state, store_id, created_at, verified_at)
		VALUES (?, 36, 'live', 1, '2026-07-04T09:00:00Z', '2026-07-04T09:00:00Z')`, mediaURN)
	return s
}

// tv001Draft is the draft whose send must reproduce the vector.
func tv001Draft(t *testing.T) string {
	t.Helper()
	tv1 := loadJSON(t, tv001Path)
	var env struct {
		Envelope struct {
			Medialet struct {
				Medialet struct {
					Subject     string            `json:"subject"`
					Body        map[string]string `json:"body"`
					DisplayedTo []sn.DisplayedTo  `json:"displayed_to"`
					Manifest    []struct {
						URN            string `json:"urn"`
						Size           int64  `json:"size"`
						Type           string `json:"type"`
						Name           string `json:"name"`
						AvailableUntil string `json:"available_until"`
					} `json:"manifest"`
				} `json:"medialet"`
			} `json:"medialet"`
		} `json:"envelope"`
	}
	if err := json.Unmarshal(tv1["signed_envelope"], &env); err != nil {
		t.Fatal(err)
	}
	m := env.Envelope.Medialet.Medialet
	draft := sn.DraftContent{
		Subject:     m.Subject,
		BodyContent: m.Body["content"],
		DisplayedTo: m.DisplayedTo,
		Recipients:  []string{"novak@target.example"},
		JobTag:      "novak wedding",
	}
	for _, me := range m.Manifest {
		draft.Manifest = append(draft.Manifest, sn.ManifestEntry{
			URN: me.URN, Size: me.Size, Type: me.Type, Name: me.Name, AvailableUntil: me.AvailableUntil,
		})
	}
	b, _ := json.Marshal(draft)
	return string(b)
}

func TestComposeSendReproducesTV001(t *testing.T) {
	tv1, tv2 := loadJSON(t, tv001Path), loadJSON(t, tv002Path)

	// The live target: the S4.4 machinery with the TV-002 hooks.
	targetClock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	target := newAPI(t, "target.example", "novak", seedT3, &targetClock,
		map[string]json.RawMessage{"origin.example": tv1["domain_document"]})
	target.SN.NewVerdictID = func(time.Time) string { return "019f2c92-3070-7d18-adda-f5b677a35e4a" }
	federation := httptest.NewServer(sn.Handler(target.SN))
	defer federation.Close()

	// The origin: the first now() call stamps the Medialet
	// (10:00:00Z); every later call is dispatch time (10:00:05Z) —
	// exactly the vector's two timestamps.
	t0 := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	t5 := time.Date(2026, 7, 4, 10, 0, 5, 0, time.UTC)
	first := true
	clock := t0
	s := originAPI(t, &clock)
	s.SN.Now = func() time.Time {
		if first {
			first = false
			return t0
		}
		return t5
	}
	s.SN.NewMedialetID = func(time.Time) string { return "019f2c92-1900-7b0f-8f1e-30c7d7d77f8c" }
	s.SN.NewEnvelopeID = func(time.Time) string { return envID }
	s.SN.DispatchEndpoint = func(ctx context.Context, domain string) (string, error) {
		return federation.URL + "/dispatch", nil
	}
	s.SN.FulfillClient = federation.Client()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "petra@origin.example")

	resp := do(http.MethodPost, "/api/v1/drafts", tv001Draft(t), nil)
	var created struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.ID == "" {
		t.Fatal("draft id missing")
	}

	resp = do(http.MethodPost, "/api/v1/drafts/"+created.ID+"/send", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("send: %d", resp.StatusCode)
	}
	var result sn.SendResult
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	if len(result.Targets) != 1 || result.Targets[0].Message != "accepted" ||
		result.Targets[0].EnvelopeID != envID {
		t.Fatalf("send result: %+v", result)
	}

	// The Signed Medialet is TV-001's, byte-identical; so is its CA.
	wantSM := canonical(t, extractRaw(t, tv1, "signed_envelope", "envelope", "medialet"))
	var gotSM []byte
	s.DB.QueryRow(`SELECT raw FROM medialets WHERE content_address=?`, result.MedialetCA).Scan(&gotSM)
	if result.MedialetCA != core.URNMlet(wantSM) || string(gotSM) != string(wantSM) {
		t.Fatalf("Signed Medialet not byte-identical to TV-001")
	}
	// The dispatched Signed Envelope is TV-001's, byte-identical.
	var gotEnv []byte
	s.DB.QueryRow(`SELECT envelope_canonical FROM dispatches WHERE envelope_id=?`, envID).Scan(&gotEnv)
	if string(gotEnv) != string(canonical(t, tv1["signed_envelope"])) {
		t.Fatalf("Signed Envelope not byte-identical to TV-001:\n got %s", gotEnv)
	}
	// The synchronous verdict recorded at the origin is TV-002
	// verdict 1, byte-identical — round-tripped over real HTTP.
	var gotVerdict []byte
	s.DB.QueryRow(`SELECT doc FROM verdicts WHERE direction='in' AND envelope_id=?`, envID).Scan(&gotVerdict)
	if string(gotVerdict) != string(canonical(t, tv2["signed_verdict_1"])) {
		t.Fatalf("recorded verdict is not TV-002 verdict 1")
	}

	// Origin materialization: delivery linked, timeline dispatched +
	// verdict.received, promised ref, the sender's own read copy.
	var deliveryID int64
	s.DB.QueryRow(`SELECT delivery_id FROM dispatches WHERE envelope_id=?`, envID).Scan(&deliveryID)
	if deliveryID != result.DeliveryID || deliveryID == 0 {
		t.Fatalf("dispatch not linked to the delivery: %d vs %d", deliveryID, result.DeliveryID)
	}
	var kinds []string
	rows, _ := s.DB.Query(`SELECT kind FROM timeline_events WHERE delivery_id=? ORDER BY at, id`, deliveryID)
	for rows.Next() {
		var k string
		rows.Scan(&k)
		kinds = append(kinds, k)
	}
	rows.Close()
	if len(kinds) != 2 || kinds[0] != "dispatched" || kinds[1] != "verdict.received" {
		t.Fatalf("timeline: %v", kinds)
	}
	var refState string
	s.DB.QueryRow(`SELECT state FROM refs WHERE mailbox_id=1 AND urn=? AND direction='out'`, mediaURN).Scan(&refState)
	if refState != "promised" {
		t.Fatalf("outbound ref: %q", refState)
	}
	var read int
	var envIn *int64
	if err := s.DB.QueryRow(`SELECT read, envelope_in FROM messages WHERE mailbox_id=1`).Scan(&read, &envIn); err != nil || read != 1 || envIn != nil {
		t.Fatalf("sender copy: %v read=%d envelope_in=%v", err, read, envIn)
	}
	// The draft is gone; the target materialized novak's inbox.
	var drafts, novakMsgs int
	s.DB.QueryRow(`SELECT COUNT(*) FROM drafts`).Scan(&drafts)
	target.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE mailbox_id=1`).Scan(&novakMsgs)
	if drafts != 0 || novakMsgs != 1 {
		t.Fatalf("post-send state: drafts=%d target messages=%d", drafts, novakMsgs)
	}
}

func TestUploadDoorAndPossessionGate(t *testing.T) {
	clock := time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC)
	tv2 := loadJSON(t, tv002Path)
	s := newAPI(t, "origin.example", "petra", seedOriginSN, &clock,
		map[string]json.RawMessage{"target.example": tv2["target_domain_document"]})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "petra@origin.example")

	object := []byte("MLP test vector 001: media object A\n")

	// Hash-first declare: absent → an upload lane.
	resp := do(http.MethodPost, "/api/v1/uploads",
		fmt.Sprintf(`{"urn":%q,"size":36}`, mediaURN), nil)
	var lane struct {
		Have   bool   `json:"have"`
		Upload string `json:"upload"`
	}
	json.NewDecoder(resp.Body).Decode(&lane)
	resp.Body.Close()
	if lane.Have || lane.Upload == "" {
		t.Fatalf("declare: %+v", lane)
	}

	// Chunk 1 through the session-authed door.
	resp = do(http.MethodPatch, lane.Upload, string(object[:20]),
		map[string]string{"Upload-Offset": "0"})
	if resp.StatusCode != 204 || resp.Header.Get("Upload-Offset") != "20" {
		t.Fatalf("chunk 1: %d %q", resp.StatusCode, resp.Header.Get("Upload-Offset"))
	}
	resp.Body.Close()
	// A corrupted client digest is refused; the checkpoint stands.
	bad := sha256.Sum256([]byte("junk"))
	resp = do(http.MethodPatch, lane.Upload, string(object[20:]),
		map[string]string{"Upload-Offset": "20",
			"Content-Digest": "sha-256=:" + base64.StdEncoding.EncodeToString(bad[:]) + ":"})
	if resp.StatusCode != 422 {
		t.Fatalf("corrupt digest must be 422: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// Resume and finish.
	resp = do("HEAD", lane.Upload, "", nil)
	if resp.Header.Get("Upload-Offset") != "20" {
		t.Fatalf("resume offset: %q", resp.Header.Get("Upload-Offset"))
	}
	resp.Body.Close()
	resp = do(http.MethodPatch, lane.Upload, string(object[20:]),
		map[string]string{"Upload-Offset": "20"})
	if resp.StatusCode != 204 || resp.Header.Get("MLP-Object-State") != "verified" {
		t.Fatalf("completion: %d %+v", resp.StatusCode, resp.Header)
	}
	resp.Body.Close()

	// Declare again: attach by reference, zero upload (D-135).
	resp = do(http.MethodPost, "/api/v1/uploads",
		fmt.Sprintf(`{"urn":%q,"size":36}`, mediaURN), nil)
	json.NewDecoder(resp.Body).Decode(&lane)
	resp.Body.Close()
	if !lane.Have {
		t.Fatal("second declare must answer have:true")
	}

	// The possession gate: a manifest urn nobody holds blocks send.
	foreign := "urn:mlet:bdyqhpdu37yof4e5ka5bbamzdh7rl2w3wyocv6hfza67mwkvw6ge7j5y"
	draft, _ := json.Marshal(sn.DraftContent{
		BodyContent: "<p>x</p>", Recipients: []string{"novak@target.example"},
		Manifest: []sn.ManifestEntry{{URN: foreign, Size: 1, Type: "text/plain", AvailableUntil: "2026-07-11T10:00:00Z"}},
	})
	resp = do(http.MethodPost, "/api/v1/drafts", string(draft), nil)
	var created struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	resp = do(http.MethodPost, "/api/v1/drafts/"+created.ID+"/send", "", nil)
	if resp.StatusCode != 409 {
		t.Fatalf("send must gate on possession (D-135): %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestMultiDomainFanOut(t *testing.T) {
	tv1, tv4 := loadJSON(t, tv001Path), loadJSON(t, tv004Path)

	targetClock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	target := newAPI(t, "target.example", "novak", seedT3, &targetClock,
		map[string]json.RawMessage{"origin.example": tv1["domain_document"]})
	finalClock := targetClock
	final := newAPI(t, "final.example", "carol", seedFinal, &finalClock,
		map[string]json.RawMessage{"origin.example": tv1["domain_document"]})
	tsTarget := httptest.NewServer(sn.Handler(target.SN))
	defer tsTarget.Close()
	tsFinal := httptest.NewServer(sn.Handler(final.SN))
	defer tsFinal.Close()

	clock := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	s := originAPI(t, &clock)
	seedDomainDoc(t, s, "final.example", tv4["final_domain_document"], clock)
	s.SN.DispatchEndpoint = func(ctx context.Context, domain string) (string, error) {
		if domain == "target.example" {
			return tsTarget.URL + "/dispatch", nil
		}
		return tsFinal.URL + "/dispatch", nil
	}

	draft, _ := json.Marshal(sn.DraftContent{
		Subject: "fan-out", BodyContent: "<p>hello both</p>",
		Recipients: []string{"novak@target.example", "carol@final.example"},
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "petra@origin.example")
	resp := do(http.MethodPost, "/api/v1/drafts", string(draft), nil)
	var created struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	resp = do(http.MethodPost, "/api/v1/drafts/"+created.ID+"/send", "", nil)
	var result sn.SendResult
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	if len(result.Targets) != 2 ||
		result.Targets[0].Message != "accepted" || result.Targets[1].Message != "accepted" {
		t.Fatalf("fan-out: %+v", result)
	}
	// One delivery, two linked dispatches, distinct envelopes.
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM dispatches WHERE delivery_id=?`, result.DeliveryID).Scan(&n)
	if n != 2 || result.Targets[0].EnvelopeID == result.Targets[1].EnvelopeID {
		t.Fatalf("fan-out dispatches: %d", n)
	}
	// Both recipients materialized.
	var a, b int
	target.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&a)
	final.DB.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&b)
	if a != 1 || b != 1 {
		t.Fatalf("recipient materialization: %d %d", a, b)
	}
}

// seedDomainDoc caches one more domain document on an existing server.
func seedDomainDoc(t *testing.T, s *Server, domain string, doc json.RawMessage, clock time.Time) {
	t.Helper()
	parsed, err := discovery.ParseDocument(doc, domain, []string{"0.1"})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, s.DB, `INSERT INTO domain_docs (domain, doc, fetched_at, expires_at) VALUES (?,?,?,?)`,
		domain, string(doc), clock.Format(time.RFC3339), clock.Add(23*time.Hour).Format(time.RFC3339))
	for _, k := range parsed.Keys {
		roles, _ := json.Marshal(k.Roles)
		mustExec(t, s.DB, `INSERT INTO domain_keys (domain, kid, key, roles) VALUES (?,?,?,?)`,
			domain, k.KID, k.Key, string(roles))
	}
}

func TestDraftLifecycle(t *testing.T) {
	clock := time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC)
	tv2 := loadJSON(t, tv002Path)
	s := newAPI(t, "origin.example", "petra", seedOriginSN, &clock,
		map[string]json.RawMessage{"target.example": tv2["target_domain_document"]})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "petra@origin.example")

	resp := do(http.MethodPost, "/api/v1/drafts", `{"subject":"wip","recipients":[]}`, nil)
	var created struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()

	resp = do(http.MethodPatch, "/api/v1/drafts/"+created.ID,
		`{"subject":"wip 2","recipients":["novak@target.example"],"body_content":"<p>hi</p>"}`, nil)
	if resp.StatusCode != 204 {
		t.Fatalf("autosave: %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = do(http.MethodGet, "/api/v1/drafts", "", nil)
	var list struct {
		Drafts []struct {
			ID  string          `json:"id"`
			Doc json.RawMessage `json:"doc"`
		} `json:"drafts"`
	}
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Drafts) != 1 || !strings.Contains(string(list.Drafts[0].Doc), "wip 2") {
		t.Fatalf("drafts list: %+v", list)
	}
	resp = do(http.MethodDelete, "/api/v1/drafts/"+created.ID, "", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = do(http.MethodGet, "/api/v1/drafts/"+created.ID, "", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("deleted draft must be 404: %d", resp.StatusCode)
	}
	resp.Body.Close()
}
