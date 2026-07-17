package sn

// S4.6 acceptance, anchored on TV-004: target.example's auto-forward
// of the TV-001 dispatch reproduces the forwarded Envelope
// byte-identically (1,669 canonical bytes; the appended attestation
// IS TV-001's hop signature verbatim); final.example ingests it and
// its delegation request to the root reproduces the vector (1,095
// bytes) minting the reservation on its own BS (D-82); the source-
// side §9.5 walk validates against origin.example's own dispatch
// records, answers the exact unsigned response, enqueues the push,
// deduplicates replays, enforces the budget, and alarms on spliced
// attestations; the requester loop discards non-chain-members and
// falls through candidates to the graceful resend state; D-51 loop
// prevention blocks automatic re-dispatch only.

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"medialet.org/mlp/core"
)

const (
	tv004Path = "../../conformance/vectors/mlp-tv-004.json"
	seedFinal = "f5e5767cf153319517630f226876b86c8160cc583bc013744c6bf255f5cc0ee5" // RFC 8032 TEST 1024
	fwdEnvID  = "019f2c92-3458-7ba2-9bec-0190697bca43"
	tv1CA     = "urn:mlet:bdyqhmtxg343efvdn34cvh4xacxbfa7keroljucjvcpvg63rtkvhmlqa"
)

// forwardTargetSN prepares target.example holding the TV-001 delivery
// (via the real S4.4 ingest path, so migration-0002 columns populate).
func forwardTargetSN(t *testing.T, clock *time.Time) *SN {
	t.Helper()
	s := newTargetSN(t, clock)
	if _, prob := s.ProcessDispatch(context.Background(), dispatchEnvelope(t)); prob != nil {
		t.Fatalf("TV-001 ingest: %v", prob)
	}
	return s
}

func TestForwardReproducesTV004Envelope(t *testing.T) {
	tv4 := loadJSON(t, tv004Path)
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := forwardTargetSN(t, &clock)
	clock = time.Date(2026, 7, 4, 10, 0, 7, 0, time.UTC)
	s.NewEnvelopeID = func(time.Time) string { return fwdEnvID }

	got, err := s.Forward(context.Background(), "origin.example", envID,
		[]string{"carol@final.example"}, "novak@target.example", "", Delegated, true, "")
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	want := canonical(t, tv4["signed_forwarded_envelope"])
	if string(got) != string(want) {
		t.Fatalf("forwarded envelope not byte-identical to TV-004:\n got %s\nwant %s", got, want)
	}
	if len(got) != 1669 {
		t.Fatalf("canonical size %d, want 1669", len(got))
	}
	// The dispatch is recorded: target.example is now a §9.5-capable
	// chain member.
	var targetDomain, ca string
	if err := s.DB.QueryRow(`SELECT target_domain, medialet_ca FROM dispatches WHERE envelope_id=?`, fwdEnvID).
		Scan(&targetDomain, &ca); err != nil || targetDomain != "final.example" || ca != tv1CA {
		t.Fatalf("dispatch record: %v %q %q", err, targetDomain, ca)
	}
}

func TestLoopPrevention(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 7, 0, time.UTC)
	s := forwardTargetSN(t, &clock)
	// Auto-forwarding back to the chain: origin.example is the
	// received Envelope's origin → refused for automatic dispatch.
	pretendOwn := *s
	pretendOwn.Domain = "origin.example"
	if _, err := pretendOwn.Forward(context.Background(), "origin.example", envID,
		[]string{"x@final.example"}, "", "", Delegated, true, ""); err == nil {
		t.Fatal("automatic re-dispatch into the chain must be refused (D-51)")
	}
	// A deliberate user forward MAY proceed regardless.
	pretendOwn.NewEnvelopeID = nil
	if _, err := pretendOwn.Forward(context.Background(), "origin.example", envID,
		[]string{"x@final.example"}, "petra@origin.example", "", Delegated, false, ""); err != nil {
		t.Fatalf("deliberate forward must proceed: %v", err)
	}
}

// finalSN prepares final.example: carol's mailbox, the TEST 1024 sn
// key, and the discovery cache holding target.example (hop signer)
// and origin.example (author keys + delegation source).
func finalSN(t *testing.T, clock *time.Time) *SN {
	t.Helper()
	tv1, tv2 := loadJSON(t, tv001Path), loadJSON(t, tv002Path)
	s := newSN(t, "final.example", clock)
	s.IngestBase = "https://bs.final.example/ingest/"
	mustExec(t, s, `INSERT INTO stores (id, name) VALUES (1, 'default')`)
	mustExec(t, s, `INSERT INTO mailboxes (id, local_part, created) VALUES (1, 'carol', '2026-01-01T00:00:00Z')`)
	seedOwnKey(t, s, seedFinal, `["sn","bs"]`)
	seedDomainCache(t, s, "origin.example", tv1["domain_document"], *clock)
	seedDomainCache(t, s, "target.example", tv2["target_domain_document"], *clock)
	return s
}

// ingestForwarded runs the TV-004 forwarded Envelope through
// final.example's real dispatch path.
func ingestForwarded(t *testing.T, s *SN) {
	t.Helper()
	tv4 := loadJSON(t, tv004Path)
	verdict, prob := s.ProcessDispatch(context.Background(), tv4["signed_forwarded_envelope"])
	if prob != nil {
		t.Fatalf("forwarded ingest: %v", prob)
	}
	pv, _ := parseOwn(t, verdict)
	if pv.Message != "accepted" || len(pv.Media) != 1 || pv.Media[0].Verdict != "defer" {
		t.Fatalf("forwarded verdict: %q %+v", pv.Message, pv.Media)
	}
}

func TestForwardedIngestAndDeliveryRecord(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 8, 0, time.UTC)
	s := finalSN(t, &clock)
	ingestForwarded(t, s)
	// The Delivery Record retains the chain, the source list, and —
	// per migration 0002 — everything an attestation needs (D-53).
	var hops, sources, fwdBy, created, sig string
	if err := s.DB.QueryRow(
		`SELECT hops_json, fulfillment_sources_json, forwarded_by, envelope_created, hop_sig_value
		 FROM envelopes_in WHERE origin='target.example' AND envelope_id=?`, fwdEnvID).
		Scan(&hops, &sources, &fwdBy, &created, &sig); err != nil {
		t.Fatalf("delivery record: %v", err)
	}
	if !strings.Contains(hops, `"origin.example"`) || !strings.Contains(sources, `"origin.example"`) ||
		fwdBy != "novak@target.example" || created != "2026-07-04T10:00:07Z" || sig == "" {
		t.Fatalf("delivery record incomplete: %q %q %q %q", hops, sources, fwdBy, created)
	}
}

func TestDelegationRequestReproducesTV004(t *testing.T) {
	tv4 := loadJSON(t, tv004Path)
	clock := time.Date(2026, 7, 4, 10, 0, 8, 0, time.UTC)
	s := finalSN(t, &clock)
	ingestForwarded(t, s)

	clock = time.Date(2026, 7, 4, 11, 0, 0, 0, time.UTC)
	s.NewRequestID = func(time.Time) string { return "019f2cc9-0780-796b-b6b9-f0bcd5f10c95" }
	s.NewReservationSecret = func() (string, string) {
		return "SG0L0KEh3gj7GvsW7zAuzIrG2CkvCtMH", "7b236472a18b169b"
	}
	got, err := s.BuildDelegationRequest(context.Background(), "origin.example",
		"target.example", fwdEnvID, []string{mediaURN})
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	want := canonical(t, tv4["signed_delegation_request"])
	if string(got) != string(want) {
		t.Fatalf("delegation request not byte-identical to TV-004:\n got %s\nwant %s", got, want)
	}
	if len(got) != 1095 {
		t.Fatalf("canonical size %d, want 1095", len(got))
	}
	// The reservation is minted on OUR side — the ingesting party
	// always mints (D-82) — hash-only (D-192).
	var state, pusher string
	if err := s.DB.QueryRow(`SELECT state, pusher_domain FROM reservations_in WHERE token_hash=?`,
		tokenHash("SG0L0KEh3gj7GvsW7zAuzIrG2CkvCtMH")).Scan(&state, &pusher); err != nil ||
		state != "pending" || pusher != "origin.example" {
		t.Fatalf("minted reservation: %v %q %q", err, state, pusher)
	}
}

// sourceSN prepares origin.example as the delegation source: the
// TV-001 dispatch in its records, the object live, final.example's
// keys cached.
func sourceSN(t *testing.T, clock *time.Time) *SN {
	t.Helper()
	tv4 := loadJSON(t, tv004Path)
	s := newOriginSN(t, clock)
	seedOwnKey(t, s, seedSN, `["sn","bs"]`)
	seedDomainCache(t, s, "final.example", tv4["final_domain_document"], *clock)
	mustExec(t, s, `INSERT INTO objects (urn, size, state, store_id, created_at, verified_at)
		VALUES (?,36,'live',1,'2026-07-04T10:00:00Z','2026-07-04T10:00:00Z')`, mediaURN)
	// newOriginSN seeded the dispatch row with placeholder hop
	// fields; the §9.5 credential store needs the real ones.
	tv1 := loadJSON(t, tv001Path)
	var env struct {
		Signature struct {
			Protected struct {
				KID string `json:"kid"`
			} `json:"protected"`
			Value string `json:"value"`
		} `json:"signature"`
	}
	if err := json.Unmarshal(tv1["signed_envelope"], &env); err != nil {
		t.Fatal(err)
	}
	mustExec(t, s, `UPDATE dispatches SET hop_sig_value=?, hop_kid=? WHERE envelope_id=?`,
		env.Signature.Value, env.Signature.Protected.KID, envID)
	return s
}

func TestSourceSideWalk(t *testing.T) {
	tv4 := loadJSON(t, tv004Path)
	clock := time.Date(2026, 7, 4, 11, 0, 1, 0, time.UTC)
	s := sourceSN(t, &clock)
	ctx := context.Background()
	request := canonical(t, tv4["signed_delegation_request"])

	resp, prob := s.ProcessFulfill(ctx, request)
	if prob != nil {
		t.Fatalf("fulfill: %v", prob)
	}
	want := canonical(t, tv4["fulfill_response_unsigned"])
	if string(resp) != string(want) {
		t.Fatalf("response not byte-identical:\n got %s\nwant %s", resp, want)
	}
	// Budget consumed at acceptance; the push enqueued with the
	// requester-supplied reservation.
	var status string
	var token, url string
	s.DB.QueryRow(`SELECT status FROM delegations WHERE requester='final.example' AND urn=?`, mediaURN).Scan(&status)
	if err := s.DB.QueryRow(`SELECT token, target_url FROM reservations_out WHERE envelope_id=? AND urn=?`,
		envID, mediaURN).Scan(&token, &url); err != nil ||
		status != "accepted" ||
		token != "SG0L0KEh3gj7GvsW7zAuzIrG2CkvCtMH" ||
		url != "https://bs.final.example/ingest/7b236472a18b169b" {
		t.Fatalf("enqueue: %v status=%q token=%q url=%q", err, status, token, url)
	}
	// Replay: same response, no second budget consumption (§9.4).
	resp2, prob := s.ProcessFulfill(ctx, request)
	if prob != nil || string(resp2) != string(want) {
		t.Fatalf("replay: %v", prob)
	}
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM delegations WHERE urn=?`, mediaURN).Scan(&n)
	if n != 1 {
		t.Fatalf("replay must consume no budget: %d rows", n)
	}
}

func TestSourceSideFailures(t *testing.T) {
	tv4 := loadJSON(t, tv004Path)
	clock := time.Date(2026, 7, 4, 11, 0, 1, 0, time.UTC)
	request := canonical(t, tv4["signed_delegation_request"])
	ctx := context.Background()

	mutate := func(t *testing.T, f func(p map[string]any)) []byte {
		t.Helper()
		var doc map[string]any
		if err := json.Unmarshal(request, &doc); err != nil {
			t.Fatal(err)
		}
		f(doc["payload"].(map[string]any))
		b, _ := json.Marshal(doc)
		return b
	}

	t.Run("wrong domain", func(t *testing.T) {
		s := sourceSN(t, &clock)
		s.Domain = "elsewhere.example"
		if _, prob := s.ProcessFulfill(ctx, request); prob == nil || prob.Code != "unknown-envelope" {
			t.Fatalf("root.origin mismatch must be unknown-envelope: %v", prob)
		}
	})
	t.Run("tampered attestation sig", func(t *testing.T) {
		s := sourceSN(t, &clock)
		// The attestation is inside the signed payload; flipping it
		// breaks the delegation signature first — so instead corrupt
		// the *record*: the stored sig differs from the presented one.
		mustExec(t, s, `UPDATE dispatches SET hop_sig_value='different' WHERE envelope_id=?`, envID)
		if _, prob := s.ProcessFulfill(ctx, request); prob == nil || prob.Code != "unknown-envelope" {
			t.Fatalf("sig not matching our records must be unknown-envelope: %v", prob)
		}
	})
	t.Run("spliced content", func(t *testing.T) {
		s := sourceSN(t, &clock)
		mustExec(t, s, `INSERT INTO medialets (content_address, author, medialet_id, created, raw)
			VALUES ('urn:mlet:bother', 'x@origin.example', 'other', '2026-07-04T00:00:00Z', X'7B7D')`)
		mustExec(t, s, `UPDATE dispatches SET medialet_ca='urn:mlet:bother' WHERE envelope_id=?`, envID)
		if _, prob := s.ProcessFulfill(ctx, request); prob == nil || prob.Code != "medialet-mismatch" {
			t.Fatalf("splice must alarm as medialet-mismatch: %v", prob)
		}
	})
	t.Run("signature tamper", func(t *testing.T) {
		s := sourceSN(t, &clock)
		bad := strings.Replace(string(request), "-_AzAVW-", "-_AzAVW_", 1)
		if _, prob := s.ProcessFulfill(ctx, []byte(bad)); prob == nil || prob.Code != "signature-invalid" {
			t.Fatalf("tampered delegation must be signature-invalid: %v", prob)
		}
	})
	t.Run("skew", func(t *testing.T) {
		late := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
		s := sourceSN(t, &late)
		if _, prob := s.ProcessFulfill(ctx, request); prob == nil || prob.Code != "timestamp-skew" {
			t.Fatalf("stale request must be timestamp-skew: %v", prob)
		}
	})
	t.Run("object not held", func(t *testing.T) {
		s := sourceSN(t, &clock)
		mustExec(t, s, `DELETE FROM objects WHERE urn=?`, mediaURN)
		resp, prob := s.ProcessFulfill(ctx, request)
		if prob != nil || !strings.Contains(string(resp), `"not-available"`) {
			t.Fatalf("unheld object must refuse not-available: %v %s", prob, resp)
		}
	})
	t.Run("availability window passed", func(t *testing.T) {
		// available_until is 2026-07-11T10:00:00Z; a request signed
		// fresh at 07-12 clears skew but fails availability.
		late := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
		s := sourceSN(t, &late)
		fresh := s.now().Format(time.RFC3339)
		req := resignDelegation(t, mutate(t, func(p map[string]any) { p["created"] = fresh }), fresh)
		resp, prob := s.ProcessFulfill(ctx, req)
		if prob != nil || !strings.Contains(string(resp), `"not-available"`) {
			t.Fatalf("past available_until must refuse not-available: %v %s", prob, resp)
		}
	})
	t.Run("budget exhaustion", func(t *testing.T) {
		s := sourceSN(t, &clock)
		for i := 0; i < DelegationBudget; i++ {
			mustExec(t, s, `INSERT INTO delegations (request_id, requester, envelope_id, urn, status, created)
				VALUES (?, 'someone.example', ?, ?, 'accepted', '2026-07-04T10:30:00Z')`,
				itoaTest(i), envID, mediaURN)
		}
		resp, prob := s.ProcessFulfill(ctx, request)
		if prob != nil || !strings.Contains(string(resp), `"delegation-budget"`) {
			t.Fatalf("exhausted budget must refuse delegation-budget: %v %s", prob, resp)
		}
	})
	t.Run("max_size mismatch", func(t *testing.T) {
		s := sourceSN(t, &clock)
		fresh := s.now().Format(time.RFC3339)
		req := resignDelegation(t, mutate(t, func(p map[string]any) {
			res := p["media"].([]any)[0].(map[string]any)["reservation"].(map[string]any)
			res["max_size"] = 99
		}), fresh)
		if _, prob := s.ProcessFulfill(ctx, req); prob == nil || prob.Code != "malformed" {
			t.Fatalf("max_size != Manifest size must be malformed: %v", prob)
		}
	})
}

func itoaTest(i int) string { return string([]byte{'r', byte('0' + i)}) }

// resignDelegation re-signs a mutated request with final.example's
// key so mutations survive to the step under test.
func resignDelegation(t *testing.T, raw []byte, created string) []byte {
	t.Helper()
	v, err := core.ParseDialect(raw)
	if err != nil {
		t.Fatal(err)
	}
	top := v.(map[string]any)
	payload := top["payload"].(map[string]any)
	payload["created"] = created
	seed, _ := hex.DecodeString(seedFinal)
	priv := ed25519.NewKeyFromSeed(seed)
	sig, _, err := core.SignDoc(priv, "delegation/1", core.KID(priv.Public().(ed25519.PublicKey)), created, payload)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := core.CanonicalizeValue(map[string]any{"payload": payload, "signature": sig})
	if err != nil {
		t.Fatal(err)
	}
	return canon
}

func TestRequesterFallthrough(t *testing.T) {
	clock := time.Date(2026, 7, 4, 11, 0, 0, 0, time.UTC)
	s := finalSN(t, &clock)
	ingestForwarded(t, s)

	// The real source behind httptest.
	srcClock := clock
	src := sourceSN(t, &srcClock)
	ts := httptest.NewServer(Handler(src))
	defer ts.Close()

	calls := map[string]int{}
	s.FulfillEndpoint = func(ctx context.Context, domain string) (string, error) {
		calls[domain]++
		if domain == "origin.example" {
			return ts.URL + "/fulfill", nil
		}
		return "", context.DeadlineExceeded // unreachable candidate
	}
	s.FulfillClient = ts.Client()

	outcomes, err := s.RequestFulfillment(context.Background(),
		"target.example", fwdEnvID, []string{mediaURN})
	if err != nil {
		t.Fatalf("fulfillment: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != "will-push" {
		t.Fatalf("outcomes: %+v", outcomes)
	}
	if calls["origin.example"] != 1 {
		t.Fatalf("candidate order wrong: %v", calls)
	}
	// The source enqueued the push toward final.example's BS.
	var n int
	src.DB.QueryRow(`SELECT COUNT(*) FROM reservations_out WHERE envelope_id=?`, envID).Scan(&n)
	if n != 1 {
		t.Fatalf("push not enqueued at the source: %d", n)
	}

	// Exhaust every candidate: the graceful terminal state (§9.3).
	s.FulfillEndpoint = func(ctx context.Context, domain string) (string, error) {
		return "", context.DeadlineExceeded
	}
	if _, err := s.RequestFulfillment(context.Background(),
		"target.example", fwdEnvID, []string{mediaURN}); err == nil {
		t.Fatal("exhausted candidates must surface the resend state")
	}
}

func TestFulfillHTTP(t *testing.T) {
	tv4 := loadJSON(t, tv004Path)
	clock := time.Date(2026, 7, 4, 11, 0, 1, 0, time.UTC)
	s := sourceSN(t, &clock)
	ts := httptest.NewServer(Handler(s))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/fulfill", ctDelegation,
		strings.NewReader(string(canonical(t, tv4["signed_delegation_request"]))))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("fulfill POST: %v %v", err, resp.Status)
	}
	var body struct {
		Media []FulfillOutcome `json:"media"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if len(body.Media) != 1 || body.Media[0].Status != "will-push" {
		t.Fatalf("response: %+v", body)
	}
	// Wrong content type → 415.
	resp, _ = http.Post(ts.URL+"/fulfill", "application/json", strings.NewReader("{}"))
	if resp.StatusCode != 415 {
		t.Fatalf("want 415: %d", resp.StatusCode)
	}
	resp.Body.Close()
}
