package sn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"medialet.org/mlp/core"
)

// Forwarding (§9.1–9.2): re-dispatching a received Medialet —
// byte-identical, Author Signature intact (D-02) — inside a fresh
// Envelope with the chain appended (§3.4.2).

// ForwardMode selects who supplies the bytes (D-24).
type ForwardMode int

const (
	// Delegated: pointers only; downstream grants are fulfilled by
	// upstream custody holders. Default for aliases, auto-forwards,
	// personal forwards.
	Delegated ForwardMode = iota
	// Custody: this domain ingested the objects and leads the source
	// list. Default for list-style redistribution.
	Custody
)

// ErrForwardLoop reports the D-51 refusal: this domain already
// appears in the chain, and the re-dispatch is automatic.
var ErrForwardLoop = errors.New("mlp/sn: automatic re-dispatch refused, own domain already in the chain (D-51)")

// hopAttestation is the §3.4.2 identifying core of a prior dispatch.
type hopAttestation struct {
	Origin     string `json:"origin"`
	EnvelopeID string `json:"envelope_id"`
	Created    string `json:"created"`
	KID        string `json:"kid"`
	Sig        string `json:"sig"`
}

func (h hopAttestation) asMap() map[string]any {
	return map[string]any{
		"origin": h.Origin, "envelope_id": h.EnvelopeID,
		"created": h.Created, "kid": h.KID, "sig": h.Sig,
	}
}

// receivedEnvelope is what forwarding and delegation need from a
// Delivery Record (D-53).
type receivedEnvelope struct {
	Origin, EnvelopeID string
	MedialetCA         string
	Created            string // envelope_created (migration 0002)
	HopKID, HopSig     string
	Hops               []hopAttestation
	Sources            []map[string]any // fulfillment_sources as received
}

// loadReceived reads the Delivery Record for (origin, envelopeID).
func (s *SN) loadReceived(ctx context.Context, origin, envelopeID string) (*receivedEnvelope, error) {
	re := &receivedEnvelope{Origin: origin, EnvelopeID: envelopeID}
	var created, hopSig sql.NullString
	var hopsJSON, srcJSON sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT medialet_ca, envelope_created, hop_sig_kid, hop_sig_value, hops_json, fulfillment_sources_json
		 FROM envelopes_in WHERE origin=? AND envelope_id=?`, origin, envelopeID).
		Scan(&re.MedialetCA, &created, &re.HopKID, &hopSig, &hopsJSON, &srcJSON)
	if err != nil {
		return nil, fmt.Errorf("mlp/sn: delivery record (%s, %s): %w", origin, envelopeID, err)
	}
	if !created.Valid || !hopSig.Valid {
		return nil, fmt.Errorf("mlp/sn: delivery record (%s, %s) predates migration 0002 and cannot seed attestations (D-209)", origin, envelopeID)
	}
	re.Created, re.HopSig = created.String, hopSig.String
	if hopsJSON.Valid && hopsJSON.String != "" {
		if err := json.Unmarshal([]byte(hopsJSON.String), &re.Hops); err != nil {
			return nil, fmt.Errorf("mlp/sn: stored hops: %w", err)
		}
	}
	if srcJSON.Valid && srcJSON.String != "" {
		if err := json.Unmarshal([]byte(srcJSON.String), &re.Sources); err != nil {
			return nil, fmt.Errorf("mlp/sn: stored fulfillment_sources: %w", err)
		}
	}
	return re, nil
}

// ownAttestation forms the Hop Attestation of the received Envelope
// itself — exactly what a forwarder appends (§3.4.2) and what a
// requester presents when the source is that Envelope's origin
// (§9.3 step 2).
func (re *receivedEnvelope) ownAttestation() hopAttestation {
	return hopAttestation{
		Origin: re.Origin, EnvelopeID: re.EnvelopeID,
		Created: re.Created, KID: re.HopKID, Sig: re.HopSig,
	}
}

// rootOrigin is the chain's oldest origin: hops[0] when the chain
// exists, else the received Envelope's own origin.
func (re *receivedEnvelope) rootOrigin() string {
	if len(re.Hops) > 0 {
		return re.Hops[0].Origin
	}
	return re.Origin
}

// chainMember reports whether domain dispatched somewhere in the
// chain (§9.2 rule 2): the received Envelope's origin or any
// attestation's origin.
func (re *receivedEnvelope) chainMember(domain string) bool {
	if domain == re.Origin {
		return true
	}
	for _, h := range re.Hops {
		if h.Origin == domain {
			return true
		}
	}
	return false
}

// Forward re-dispatches the received (origin, envelopeID) to
// envelopeTo per §3.4.2/§9.2 and records the dispatch (the §9.5
// credential store). automatic engages D-51 loop prevention;
// deliberate user forwards pass automatic=false. forwardedBy may be
// "" for forwarder privacy (D-50). It returns the canonical Signed
// Envelope for dispatching to the target's SN.
// until is the MEP-001 custody-window declaration: non-empty on a
// Custody forward, it becomes this domain's own `until` in its
// fulfillment_sources entry — the forwarder's separately-attributed
// promise, never touching the author's Manifest. Ignored for
// Delegated forwards (a delegator makes no custody promise).
func (s *SN) Forward(ctx context.Context, origin, envelopeID string, envelopeTo []string, forwardedBy string, mode ForwardMode, automatic bool, until string) ([]byte, error) {
	now := s.now()
	re, err := s.loadReceived(ctx, origin, envelopeID)
	if err != nil {
		return nil, err
	}
	if automatic && re.chainMember(s.Domain) {
		return nil, ErrForwardLoop
	}
	if len(re.Hops) >= 32 {
		return nil, fmt.Errorf("mlp/sn: chain at the 32-attestation cap, cannot append (§3.4.4)")
	}
	if len(envelopeTo) == 0 || len(envelopeTo) > 128 {
		return nil, fmt.Errorf("mlp/sn: envelope_to must have 1–128 entries (§3.4.1)")
	}
	targetDomain := ""
	for _, addr := range envelopeTo {
		_, dom, err := ParseAddress(addr)
		if err != nil {
			return nil, err
		}
		if targetDomain == "" {
			targetDomain = dom
		} else if dom != targetDomain {
			return nil, fmt.Errorf("mlp/sn: envelope_to entries must share a single domain (§3.4.1)")
		}
	}

	var raw []byte
	if err := s.DB.QueryRowContext(ctx,
		`SELECT raw FROM medialets WHERE content_address=?`, re.MedialetCA).Scan(&raw); err != nil {
		return nil, fmt.Errorf("mlp/sn: stored medialet %s: %w", re.MedialetCA, err)
	}
	smv, err := core.ParseDialect(raw)
	if err != nil {
		return nil, err
	}

	// The chain: everything received, plus the received Envelope's
	// own attestation — none omitted, reordered, or altered (D-84).
	hops := make([]any, 0, len(re.Hops)+1)
	for _, h := range re.Hops {
		hops = append(hops, h.asMap())
	}
	hops = append(hops, re.ownAttestation().asMap())

	// fulfillment_sources per mode (§9.2 rule 2).
	sources := s.forwardSources(re, mode, until)

	to := make([]any, len(envelopeTo))
	for i, a := range envelopeTo {
		to[i] = a
	}
	envelope := map[string]any{
		"mlp":                 "0.1",
		"envelope_id":         s.envelopeID(now),
		"created":             now.Format(time.RFC3339),
		"origin":              s.Domain,
		"envelope_to":         to,
		"fulfillment_sources": sources,
		"hops":                hops,
		"medialet":            smv,
	}
	if forwardedBy != "" {
		envelope["forwarded_by"] = forwardedBy
	}

	kid, priv, err := s.signingKey(ctx, now)
	if err != nil {
		return nil, err
	}
	sig, _, err := core.SignDoc(priv, "hop/1", kid, envelope["created"].(string), envelope)
	if err != nil {
		return nil, err
	}
	canon, err := core.CanonicalizeValue(map[string]any{"envelope": envelope, "signature": sig})
	if err != nil {
		return nil, err
	}

	// Record the dispatch: we are now a chain member other domains
	// may present attestations to (§9.5 step 2).
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO dispatches (envelope_id, target_domain, medialet_ca, created, envelope_canonical, hop_sig_value, hop_kid)
		 VALUES (?,?,?,?,?,?,?)`,
		envelope["envelope_id"], targetDomain, re.MedialetCA, envelope["created"], canon,
		sig["value"], kid); err != nil {
		return nil, err
	}
	for _, a := range envelopeTo {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO dispatch_recipients (envelope_id, addr) VALUES (?,?)`,
			envelope["envelope_id"], a); err != nil {
			return nil, err
		}
	}
	return canon, tx.Commit()
}

// forwardSources builds the §9.2 rule-2 list. Delegated: the received
// list carried through, with the root origin guaranteed present (the
// minimum a delegating forwarder must list). Custody: this domain
// first (nearest hop), received sources as fallback.
func (s *SN) forwardSources(re *receivedEnvelope, mode ForwardMode, until string) []any {
	var out []any
	seen := map[string]bool{}
	add := func(entry map[string]any) {
		d, _ := entry["domain"].(string)
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		out = append(out, entry)
	}
	if mode == Custody {
		self := map[string]any{"domain": s.Domain}
		if until != "" {
			self["until"] = until // MEP-001: our own offer window
		}
		add(self)
	}
	for _, src := range re.Sources {
		add(src)
	}
	add(map[string]any{"domain": re.rootOrigin()})
	return out
}

// envelopeID mints a fresh envelope identifier (UUIDv7 RECOMMENDED,
// §3.4.1); the hook keeps conformance tests deterministic.
func (s *SN) envelopeID(t time.Time) string {
	if s.NewEnvelopeID != nil {
		return s.NewEnvelopeID(t)
	}
	return randomUUIDv7(t)
}
