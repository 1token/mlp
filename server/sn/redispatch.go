package sn

// Redispatch (D-154): the claim ceremony's delivery mechanism. The
// stored Signed Medialet — the author's signature untouched — rides
// a fresh envelope to the newly claimed local address, through the
// REAL dispatch path (ProcessDispatch on ourselves): materialization,
// threading, effective deadlines, classifier, everything, for free
// and by the same code that handles any other delivery. Instant-have
// follows from physics: the objects are already live in this
// domain's store, so accepts complete on the spot.

import (
	"context"
	"net/http"
	"time"

	"medialet.org/mlp/core"
)

// Redispatch delivers a stored medialet to a local address. Returns
// the new envelope id.
func (s *SN) Redispatch(ctx context.Context, medialetCA, toAddr string) (string, *Problem) {
	var raw []byte
	if err := s.DB.QueryRowContext(ctx,
		`SELECT raw FROM medialets WHERE content_address=?`, medialetCA).Scan(&raw); err != nil {
		return "", problemf(http.StatusNotFound, "malformed", "no such medialet: %v", err)
	}
	// ParseDialect preserves number tokens, so the re-embedded
	// medialet canonicalizes byte-identically (the CA must hold).
	smv, err := core.ParseDialect(raw)
	if err != nil {
		return "", problemf(http.StatusInternalServerError, "malformed", "stored medialet: %v", err)
	}
	now := s.now()
	created := now.Format(time.RFC3339)
	envelope := map[string]any{
		"mlp":         "0.1",
		"envelope_id": s.envelopeID(now),
		"created":     created,
		"origin":      s.Domain,
		"envelope_to": []any{toAddr},
		"medialet":    smv,
	}
	kid, priv, err := s.signingKey(ctx, now)
	if err != nil {
		return "", problemf(http.StatusInternalServerError, "malformed", "%v", err)
	}
	hopSig, _, err := core.SignDoc(priv, "hop/1", kid, created, envelope)
	if err != nil {
		return "", problemf(http.StatusInternalServerError, "malformed", "hop sign: %v", err)
	}
	canon, err := core.CanonicalizeValue(map[string]any{"envelope": envelope, "signature": hopSig})
	if err != nil {
		return "", problemf(http.StatusInternalServerError, "malformed", "%v", err)
	}
	if _, prob := s.ProcessDispatch(ctx, canon); prob != nil {
		return "", prob
	}
	return envelope["envelope_id"].(string), nil
}
