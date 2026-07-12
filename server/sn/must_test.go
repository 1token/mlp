package sn

// D-104 gap closures (S4.14): failing-input tests for MUSTs the
// audit (conformance/MUST-AUDIT.md) found uncovered. Every case here
// is an input that VIOLATES a requirement and must be refused or
// neutralized — a MUST without a failing-input test is a MUST on the
// honor system (§14.3).

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"medialet.org/mlp/core"
)

// TestJSONConventionRefusals: §2.3 rule 1 (numbers MUST be
// integers) and §3.1 (null MUST NOT appear) on known members —
// structural refusals, independent of signatures.
func TestJSONConventionRefusals(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	cases := []struct {
		name   string
		mutate func(env, med map[string]any)
	}{
		{"float manifest size (§2.3 rule 1)", func(env, med map[string]any) {
			med["manifest"].([]any)[0].(map[string]any)["size"] = 36.5
		}},
		{"null subject (§3.1)", func(env, med map[string]any) {
			med["subject"] = nil
		}},
		{"null envelope_to entry (§3.1/§3.4.1)", func(env, med map[string]any) {
			env["envelope_to"] = []any{nil}
		}},
		{"epoch timestamp (§2.3 rule 3)", func(env, med map[string]any) {
			med["created"] = 1751623205
		}},
		{"duplicate manifest urn (§3.2.2)", func(env, med map[string]any) {
			man := med["manifest"].([]any)
			med["manifest"] = append(man, man[0])
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTargetSN(t, &clock)
			_, prob := s.ProcessDispatch(context.Background(), mutateEnvelope(t, tc.mutate))
			if prob == nil || prob.Status != 400 || prob.Code != "malformed" {
				t.Fatalf("violating input must refuse 400 malformed: %+v", prob)
			}
		})
	}
}

// TestUnknownMemberTolerance: §2.3 rule 5 / §3.1 — the POSITIVE
// case: a member from the future is ignored, not fatal. (D-43's
// forward-compatibility rule; this is what let draft-01 receivers
// survive MEP-001/002 envelopes.)
func TestUnknownMemberTolerance(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := newTargetSN(t, &clock)
	raw := mutateEnvelope(t, func(env, med map[string]any) {
		env["x_future_extension"] = "tolerated"
	})
	// The mutation breaks the hop signature — the refusal must be
	// signature-invalid (proving validation got PAST the unknown
	// member), never a structural complaint about the member.
	_, prob := s.ProcessDispatch(context.Background(), raw)
	if prob == nil || prob.Code != "signature-invalid" {
		t.Fatalf("an unknown member must be ignored structurally (D-43 rule 5): %+v", prob)
	}
}

// TestAddressGrammarRefusals: §4.1 — the grammar's MUSTs as
// failing inputs against the real parser.
func TestAddressGrammarRefusals(t *testing.T) {
	bad := []string{
		"novak@localhost",       // rule 4: at least one dot
		"novak@",                // empty domain
		"@target.example",       // empty local
		"no vak@target.example", // whitespace
		"novak@tar_get.example", // underscore in domain (LDH)
		"novak@-bad.example",    // leading hyphen in a label
		"novak@bad-.example",    // trailing hyphen in a label
		"novak",                 // no @
		"a@b@c.example",         // two @
	}
	for _, addr := range bad {
		if _, _, err := ParseAddress(addr); err == nil {
			t.Fatalf("%q must be refused by the §4.1 grammar", addr)
		}
	}
	good := []string{
		"novak@target.example",
		"o_malley@target.example",  // §4.1: local atoms admit _
		"alice+a+b@target.example", // D-55: first + splits; tag keeps +
	}
	for _, addr := range good {
		if _, _, err := ParseAddress(addr); err != nil {
			t.Fatalf("%q must parse under §4.1: %v", addr, err)
		}
	}
}

// TestNonChainSourceNeverContacted: §9.2 — listed fulfillment
// sources whose domain is not a chain member MUST be ignored. The
// forwarded envelope is properly hop-signed (the interloper is the
// FORWARDER's lie, not a wire tamper), so only the candidate filter
// stands between the interloper and our delegation request.
func TestNonChainSourceNeverContacted(t *testing.T) {
	tv4 := loadJSON(t, tv004Path)
	parsed, err := core.ParseDialect(tv4["signed_forwarded_envelope"])
	if err != nil {
		t.Fatal(err)
	}
	env := parsed.(map[string]any)["envelope"].(map[string]any)
	env["fulfillment_sources"] = []any{
		map[string]any{"domain": "interloper.example"}, // not origin, not a hop
		map[string]any{"domain": "origin.example"},
	}
	seed, _ := hex.DecodeString(seedTarget)
	priv := ed25519.NewKeyFromSeed(seed)
	kid := core.KID(priv.Public().(ed25519.PublicKey))
	created := env["created"].(string)
	hopSig, _, err := core.SignDoc(priv, "hop/1", kid, created, env)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := core.CanonicalizeValue(map[string]any{"envelope": env, "signature": hopSig})
	if err != nil {
		t.Fatal(err)
	}

	clock := time.Date(2026, 7, 4, 10, 0, 8, 0, time.UTC)
	s := finalSN(t, &clock)
	if _, prob := s.ProcessDispatch(context.Background(), canon); prob != nil {
		t.Fatalf("ingest: %v", prob)
	}
	asked := []string{}
	s.FulfillEndpoint = func(_ context.Context, domain string) (string, error) {
		asked = append(asked, domain)
		return "", context.DeadlineExceeded // every candidate dark: we only observe the order
	}
	envID := env["envelope_id"].(string)
	s.RequestFulfillment(context.Background(), "target.example", envID, []string{mediaURN})
	for _, d := range asked {
		if d == "interloper.example" {
			t.Fatalf("a non-chain-member source was contacted (§9.2): %v", asked)
		}
	}
	if len(asked) == 0 || !strings.Contains(strings.Join(asked, " "), "origin.example") {
		t.Fatalf("the legitimate chain member must still be asked: %v", asked)
	}
}
