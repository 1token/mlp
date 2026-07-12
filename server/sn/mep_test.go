package sn

// MEP-001 and MEP-002 acceptance, anchored on TV-006 and TV-007:
// Forward(Custody, until) reproduces the TV-006 envelope
// byte-identically and records the dispatch; ingesting TV-006 at
// final.example stores the EFFECTIVE deadline (the declared until,
// not the passed Manifest window) and the offered reference survives
// the Manifest date, expiring only past the declaration; the source
// side honors exactly the until it hop-signed (§9.5: past the
// Manifest window → will-push; past its own declaration → the
// graceful resend state); TV-007's preview_of validation outcomes
// reproduce table-driven (violating members ignored, entries stand).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

const tv006Path = "../../conformance/vectors/mlp-tv-006.json"
const tv007Path = "../../conformance/vectors/mlp-tv-007.json"

func tv006EnvelopeID(t *testing.T) string {
	t.Helper()
	tv6 := loadJSON(t, tv006Path)
	var env struct {
		Envelope struct {
			EnvelopeID string `json:"envelope_id"`
		} `json:"envelope"`
	}
	if err := json.Unmarshal(tv6["signed_custody_envelope"], &env); err != nil {
		t.Fatal(err)
	}
	return env.Envelope.EnvelopeID
}

// custodyTargetSN: target.example with TV-001 ingested, the object
// live in its store, clock advanced to the TV-006 forward moment.
func custodyTargetSN(t *testing.T, clock *time.Time) *SN {
	t.Helper()
	*clock = time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := forwardTargetSN(t, clock)
	// The §9.5 exchange: final.example will sign fulfillment requests
	// at us, so its domain document must be resolvable here.
	tv4 := loadJSON(t, tv004Path)
	seedDomainCache(t, s, "final.example", tv4["final_domain_document"], *clock)
	// newTargetSN provisioned store 1 (TV-001 ingest reserves into it).
	mustExec(t, s, `INSERT INTO objects (urn, size, state, store_id, created_at, verified_at)
		VALUES (?, 36, 'live', 1, '2026-07-04T12:35:00Z', '2026-07-04T12:35:00Z')`, mediaURN)
	*clock = time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	return s
}

func TestForwardCustodyUntilReproducesTV006(t *testing.T) {
	tv6 := loadJSON(t, tv006Path)
	var clock time.Time
	s := custodyTargetSN(t, &clock)
	s.NewEnvelopeID = func(time.Time) string { return tv006EnvelopeID(t) }

	got, err := s.Forward(context.Background(), "origin.example", envID,
		[]string{"carol@final.example"}, "novak@target.example", Custody, false,
		"2026-09-01T00:00:00Z")
	if err != nil {
		t.Fatalf("custody forward: %v", err)
	}
	want := canonical(t, tv6["signed_custody_envelope"])
	if string(got) != string(want) {
		t.Fatalf("custody envelope not byte-identical to TV-006:\n got %s\nwant %s", got, want)
	}
	// The dispatch record is the §9.5 own-record: what we honor later
	// is exactly what we hop-signed here.
	var canon []byte
	if err := s.DB.QueryRow(`SELECT envelope_canonical FROM dispatches WHERE envelope_id=?`,
		tv006EnvelopeID(t)).Scan(&canon); err != nil || string(canon) != string(want) {
		t.Fatalf("dispatch record: %v", err)
	}
}

func TestTV006IngestStoresEffectiveDeadline(t *testing.T) {
	tv6 := loadJSON(t, tv006Path)
	exp := map[string]string{}
	json.Unmarshal(tv6["expectations"], &exp)

	clock := time.Date(2026, 7, 12, 9, 0, 5, 0, time.UTC)
	s := finalSN(t, &clock)
	verdict, prob := s.ProcessDispatch(context.Background(), tv6["signed_custody_envelope"])
	if prob != nil {
		t.Fatalf("TV-006 ingest: %v", prob)
	}
	pv, _ := parseOwn(t, verdict)
	if pv.Message != "accepted" {
		t.Fatalf("verdict: %q", pv.Message)
	}
	var until, state string
	if err := s.DB.QueryRow(`SELECT available_until, state FROM refs WHERE urn=?`, mediaURN).
		Scan(&until, &state); err != nil {
		t.Fatal(err)
	}
	if until != exp["effective_deadline"] || state != "offered" {
		t.Fatalf("effective deadline (MEP-001): until=%q state=%q, want %q offered",
			until, state, exp["effective_deadline"])
	}
	if until == exp["manifest_available_until"] {
		t.Fatal("the stored deadline must be the declared until, not the Manifest date")
	}

	// The offer survives the Manifest window (the whole point)…
	survives, _ := time.Parse(time.RFC3339, exp["offered_ref_survives_at"])
	if err := s.ExpireOffers(context.Background(), survives); err != nil {
		t.Fatal(err)
	}
	s.DB.QueryRow(`SELECT state FROM refs WHERE urn=?`, mediaURN).Scan(&state)
	if state != "offered" {
		t.Fatalf("offer must survive the Manifest window under the declared until: %q", state)
	}
	// …and expires past the declaration, through the §10.3 trigger.
	expires, _ := time.Parse(time.RFC3339, exp["offered_ref_expires_at"])
	if err := s.ExpireOffers(context.Background(), expires); err != nil {
		t.Fatal(err)
	}
	var cause string
	s.DB.QueryRow(`SELECT state, COALESCE(cause,'') FROM refs WHERE urn=?`, mediaURN).Scan(&state, &cause)
	if state != "unavailable" || cause != "expired-remote" {
		t.Fatalf("past the declaration: %q %q", state, cause)
	}
}

func TestSourceHonorsOwnDeclaredUntil(t *testing.T) {
	// target.example custody-forwarded with until (the dispatch
	// record from the real Forward); final.example requests
	// fulfillment past the Manifest window.
	var targetClock time.Time
	target := custodyTargetSN(t, &targetClock)
	target.NewEnvelopeID = func(time.Time) string { return tv006EnvelopeID(t) }
	forwarded, err := target.Forward(context.Background(), "origin.example", envID,
		[]string{"carol@final.example"}, "novak@target.example", Custody, false,
		"2026-09-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	finalClock := time.Date(2026, 7, 12, 9, 0, 5, 0, time.UTC)
	final := finalSN(t, &finalClock)
	if _, prob := final.ProcessDispatch(context.Background(), forwarded); prob != nil {
		t.Fatalf("ingest at final: %v", prob)
	}

	ts := httptest.NewServer(Handler(target))
	defer ts.Close()
	final.FulfillEndpoint = func(ctx context.Context, domain string) (string, error) {
		if domain == "target.example" {
			return ts.URL + "/fulfill", nil
		}
		return "", context.DeadlineExceeded // the root origin is dark
	}
	final.FulfillClient = ts.Client()

	// 2026-08-15: past the Manifest window (07-11), inside the
	// declared until (09-01) — the custody holder must honor.
	targetClock = time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	finalClock = targetClock
	refreshDocs(t, target, final, targetClock)
	outcomes, err := final.RequestFulfillment(context.Background(),
		"target.example", tv006EnvelopeID(t), []string{mediaURN})
	if err != nil {
		t.Fatalf("fulfillment inside the declared window: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != "will-push" {
		t.Fatalf("must honor the self-declared until (§9.5): %+v", outcomes)
	}

	// 2026-09-02: past its own declaration — refused; with the root
	// dark, the requester reaches the graceful resend state. The
	// caches are fresh, so the refusal is §9.5's, not cert rot.
	targetClock = time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	finalClock = targetClock
	refreshDocs(t, target, final, targetClock)
	if _, err := final.RequestFulfillment(context.Background(),
		"target.example", tv006EnvelopeID(t), []string{mediaURN}); err == nil {
		t.Fatal("past the self-declared until the source must refuse (§9.5)")
	}
}

func TestTV007PreviewOfValidation(t *testing.T) {
	raw, err := os.ReadFile(tv007Path)
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Cases []struct {
			Name     string           `json:"name"`
			Manifest []map[string]any `json:"manifest"`
			Expected []map[string]any `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	if len(vector.Cases) != 4 {
		t.Fatalf("TV-007 corpus: %d cases", len(vector.Cases))
	}
	for _, c := range vector.Cases {
		pe := &ParsedEnvelope{}
		// Mirror the wire parse: numbers arrive as json.Number.
		medialet := map[string]any{"manifest": reparse(t, c.Manifest)}
		if prob := validateManifest(pe, medialet); prob != nil {
			t.Fatalf("%s: unexpected rejection: %v", c.Name, prob)
		}
		if len(pe.Manifest) != len(c.Expected) {
			t.Fatalf("%s: entry count %d, want %d", c.Name, len(pe.Manifest), len(c.Expected))
		}
		for i, want := range c.Expected {
			wantPreview, _ := want["preview_of"].(string)
			if pe.Manifest[i].PreviewOf != wantPreview {
				t.Fatalf("%s entry %d: preview_of %q, want %q (violating members are ignored, entries stand)",
					c.Name, i, pe.Manifest[i].PreviewOf, wantPreview)
			}
			if pe.Manifest[i].URN != want["urn"].(string) {
				t.Fatalf("%s entry %d: the entry itself must stand", c.Name, i)
			}
		}
	}
}

// refreshDocs re-seeds the cross-domain document caches at the given
// time — the months-long §9.5 window outlives any single cache TTL,
// and the test must exercise the declaration logic, not cache expiry.
func refreshDocs(t *testing.T, target, final *SN, now time.Time) {
	t.Helper()
	tv2, tv4 := loadJSON(t, tv002Path), loadJSON(t, tv004Path)
	for _, pair := range []struct {
		s      *SN
		domain string
		doc    json.RawMessage
	}{
		{target, "final.example", tv4["final_domain_document"]},
		{final, "target.example", tv2["target_domain_document"]},
	} {
		mustExec(t, pair.s, `DELETE FROM domain_docs WHERE domain=?`, pair.domain)
		mustExec(t, pair.s, `DELETE FROM domain_keys WHERE domain=?`, pair.domain)
		seedDomainCache(t, pair.s, pair.domain, pair.doc, now)
	}
}

func reparse(t *testing.T, in []map[string]any) []any {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out []any
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
