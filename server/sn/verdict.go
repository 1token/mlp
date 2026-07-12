package sn

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"medialet.org/mlp/core"
	"medialet.org/mlp/discovery"
)

// RecipientOutcome is one §7.4 per-recipient entry (D-70).
type RecipientOutcome struct {
	Addr    string
	Verdict string // accepted / rejected / quarantined
	Reason  string // optional
}

// Reservation is the §7.5 signed, scoped invitation to push (D-18).
type Reservation struct {
	URN       string
	MaxSize   int64
	TargetURL string
	Token     string
	Expires   string
}

// MediaOutcome is one §7.4 per-URN entry — singular per Envelope, at
// domain level (D-70).
type MediaOutcome struct {
	URN         string
	Verdict     string // grant / have / defer / deny
	Reason      string // optional
	Reservation *Reservation
}

// summaryMessage derives the §7.4 summary from per-recipient
// outcomes: accepted if any accepted; else quarantined if any
// quarantined; else rejected.
func summaryMessage(recipients []RecipientOutcome) string {
	quarantined := false
	for _, r := range recipients {
		switch r.Verdict {
		case "accepted":
			return "accepted"
		case "quarantined":
			quarantined = true
		}
	}
	if quarantined {
		return "quarantined"
	}
	return "rejected"
}

// BuildVerdictPayload assembles the §7.4 payload. Empty optional
// members are omitted; media == nil omits the member (permitted for
// fully rejected Envelopes).
func BuildVerdictPayload(issuer, envOrigin, envID, verdictID, created string,
	recipients []RecipientOutcome, media []MediaOutcome) map[string]any {
	recs := make([]any, 0, len(recipients))
	for _, r := range recipients {
		e := map[string]any{"addr": r.Addr, "verdict": r.Verdict}
		if r.Reason != "" {
			e["reason"] = r.Reason
		}
		recs = append(recs, e)
	}
	p := map[string]any{
		"mlp":             "0.1",
		"verdict_id":      verdictID,
		"created":         created,
		"issuer":          issuer,
		"envelope_origin": envOrigin,
		"envelope_id":     envID,
		"message":         summaryMessage(recipients),
		"recipients":      recs,
	}
	if media != nil {
		ms := make([]any, 0, len(media))
		for _, m := range media {
			e := map[string]any{"urn": m.URN, "verdict": m.Verdict}
			if m.Reason != "" {
				e["reason"] = m.Reason
			}
			if m.Reservation != nil {
				e["reservation"] = map[string]any{
					"urn":        m.Reservation.URN,
					"max_size":   m.Reservation.MaxSize,
					"target_url": m.Reservation.TargetURL,
					"token":      m.Reservation.Token,
					"expires":    m.Reservation.Expires,
				}
			}
			ms = append(ms, e)
		}
		p["media"] = ms
	}
	return p
}

// signVerdict signs payload under verdict/1 (§6.4) with an sn-role
// key of this domain from own_keys, valid at now, returning the wire
// object and its canonical bytes.
func (s *SN) signVerdict(ctx context.Context, payload map[string]any, now time.Time) (map[string]any, []byte, error) {
	kid, priv, err := s.signingKey(ctx, now)
	if err != nil {
		return nil, nil, err
	}
	created, _ := payload["created"].(string)
	sig, _, err := core.SignDoc(priv, "verdict/1", kid, created, payload)
	if err != nil {
		return nil, nil, err
	}
	doc := map[string]any{"payload": payload, "signature": sig}
	canon, err := core.CanonicalizeValue(doc)
	if err != nil {
		return nil, nil, err
	}
	return doc, canon, nil
}

// signingKey loads an sn-role signing key valid at now from own_keys.
func (s *SN) signingKey(ctx context.Context, now time.Time) (kid string, priv ed25519.PrivateKey, err error) {
	return s.keyWithRole(ctx, "sn", now)
}

// keyWithRole loads a valid own key carrying the role (D-13: the
// domain holds and applies author keys too).
func (s *SN) keyWithRole(ctx context.Context, role string, now time.Time) (kid string, priv ed25519.PrivateKey, err error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT kid, seed, roles, not_before, not_after FROM own_keys`)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, roles string
		var seed []byte
		var nb, na sql.NullString
		if err := rows.Scan(&k, &seed, &roles, &nb, &na); err != nil {
			return "", nil, err
		}
		var rr []string
		if json.Unmarshal([]byte(roles), &rr) != nil || !contains(rr, role) {
			continue
		}
		e := discovery.KeyEntry{NotBefore: nb.String, NotAfter: na.String}
		if !e.ValidAt(now) || len(seed) != ed25519.SeedSize {
			continue
		}
		return k, ed25519.NewKeyFromSeed(seed), rows.Err()
	}
	return "", nil, errors.New("mlp/sn: no valid sn-role signing key in own_keys")
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// ParsedVerdict is a §7.4 verdict document that passed schema
// validation and verdict/1 signature verification.
type ParsedVerdict struct {
	Raw        []byte
	Payload    map[string]any
	VerdictID  string
	Created    string
	Issuer     string
	EnvOrigin  string
	EnvelopeID string
	Message    string
	Recipients []RecipientOutcome
	Media      []MediaOutcome
	HasMedia   bool
}

// ParseVerdict validates raw as a signed verdict document (§7.4) and
// verifies its verdict/1 signature against the issuer's sn keys
// (§6.4), with role, validity-window, and kid self-verification
// per §§6.2–6.3 evaluated at now (D-32 ingest semantics).
func (s *SN) ParseVerdict(ctx context.Context, raw []byte, now time.Time) (*ParsedVerdict, *Problem) {
	v, err := core.ParseDialect(raw)
	if err != nil {
		return nil, malformed("%v", err)
	}
	top, ok := v.(map[string]any)
	if !ok {
		return nil, malformed("top level is not an object")
	}
	pv := &ParsedVerdict{Raw: raw}
	if pv.Payload, ok = top["payload"].(map[string]any); !ok {
		return nil, malformed("missing verdict payload (§7.4)")
	}
	sig, ok := sigShape(top["signature"])
	if !ok {
		return nil, malformed("malformed verdict signature object (§6.4)")
	}
	p := pv.Payload
	if mlp, ok := p["mlp"].(string); !ok || mlp != "0.1" {
		return nil, malformed("verdict mlp missing or unsupported (§7.4)")
	}
	if pv.VerdictID, ok = p["verdict_id"].(string); !ok || !idGrammar(pv.VerdictID) {
		return nil, malformed("malformed verdict_id (§7.4)")
	}
	if pv.Created, ok = p["created"].(string); !ok || !rfc3339utc(pv.Created) {
		return nil, malformed("malformed verdict created (§7.4)")
	}
	if pv.Issuer, ok = p["issuer"].(string); !ok || validDomain(pv.Issuer) != nil {
		return nil, malformed("malformed verdict issuer (§7.4)")
	}
	if pv.EnvOrigin, ok = p["envelope_origin"].(string); !ok || validDomain(pv.EnvOrigin) != nil {
		return nil, malformed("malformed envelope_origin (§7.4)")
	}
	if pv.EnvelopeID, ok = p["envelope_id"].(string); !ok || !idGrammar(pv.EnvelopeID) {
		return nil, malformed("malformed envelope_id (§7.4)")
	}
	if pv.Message, ok = p["message"].(string); !ok ||
		(pv.Message != "accepted" && pv.Message != "rejected" && pv.Message != "quarantined") {
		return nil, malformed("malformed verdict message (§7.4)")
	}
	if r, present := p["reason"]; present {
		if _, ok := r.(string); !ok {
			return nil, malformed("verdict reason is not a string (§7.4)")
		}
	}

	recs, ok := p["recipients"].([]any)
	if !ok || len(recs) == 0 {
		return nil, malformed("recipients missing or empty (§7.4)")
	}
	for _, x := range recs {
		e, ok := x.(map[string]any)
		if !ok {
			return nil, malformed("recipient outcome is not an object (§7.4)")
		}
		var ro RecipientOutcome
		if ro.Addr, ok = e["addr"].(string); !ok {
			return nil, malformed("recipient outcome lacks addr (§7.4)")
		}
		if ro.Verdict, ok = e["verdict"].(string); !ok ||
			(ro.Verdict != "accepted" && ro.Verdict != "rejected" && ro.Verdict != "quarantined") {
			return nil, malformed("recipient verdict invalid (§7.4)")
		}
		ro.Reason, _ = e["reason"].(string)
		pv.Recipients = append(pv.Recipients, ro)
	}

	if mRaw, present := p["media"]; present {
		pv.HasMedia = true
		list, ok := mRaw.([]any)
		if !ok {
			return nil, malformed("media is not an array (§7.4)")
		}
		seen := map[string]bool{}
		for _, x := range list {
			e, ok := x.(map[string]any)
			if !ok {
				return nil, malformed("media outcome is not an object (§7.4)")
			}
			var mo MediaOutcome
			if mo.URN, ok = e["urn"].(string); !ok || seen[mo.URN] {
				return nil, malformed("media urn missing or repeated (§7.4)")
			}
			seen[mo.URN] = true
			if mo.Verdict, ok = e["verdict"].(string); !ok ||
				(mo.Verdict != "grant" && mo.Verdict != "have" && mo.Verdict != "defer" && mo.Verdict != "deny") {
				return nil, malformed("media verdict invalid (§7.4)")
			}
			mo.Reason, _ = e["reason"].(string)
			res, present := e["reservation"]
			if mo.Verdict == "grant" && !present {
				return nil, malformed("grant without reservation (§7.4)")
			}
			if present {
				r, prob := parseReservation(res, mo.URN)
				if prob != nil {
					return nil, prob
				}
				mo.Reservation = r
			}
			pv.Media = append(pv.Media, mo)
		}
	}

	// verdict/1 verification against the issuer's sn keys (§6.4).
	kid, _ := sig["protected"].(map[string]any)["kid"].(string)
	pub, prob := s.verificationKey(ctx, pv.Issuer, kid, "sn", now)
	if prob != nil {
		return nil, prob
	}
	if err := core.VerifyDoc(pub, "verdict/1", pv.Payload, sig); err != nil {
		return nil, problemf(http.StatusUnauthorized, "signature-invalid", "verdict/1: %v", err)
	}
	return pv, nil
}

// parseReservation validates a §7.5 Reservation carried in a media
// entry for urn.
func parseReservation(v any, urn string) (*Reservation, *Problem) {
	o, ok := v.(map[string]any)
	if !ok {
		return nil, malformed("reservation is not an object (§7.5)")
	}
	var r Reservation
	if r.URN, ok = o["urn"].(string); !ok || r.URN != urn {
		return nil, malformed("reservation urn must equal the invited object (§7.5)")
	}
	num, ok := o["max_size"].(json.Number)
	if !ok {
		return nil, malformed("reservation max_size is not an integer (§7.5)")
	}
	sz, err := num.Int64()
	if err != nil || sz < 0 {
		return nil, malformed("reservation max_size invalid (§7.5)")
	}
	r.MaxSize = sz
	if r.TargetURL, ok = o["target_url"].(string); !ok {
		return nil, malformed("reservation lacks target_url (§7.5)")
	}
	if u, err := url.Parse(r.TargetURL); err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, malformed("reservation target_url is not https (§7.5)")
	}
	if r.Token, ok = o["token"].(string); !ok || len(r.Token) < 1 || len(r.Token) > 512 {
		return nil, malformed("reservation token outside 1–512 characters (§7.5)")
	}
	if r.Expires, ok = o["expires"].(string); !ok || !rfc3339utc(r.Expires) {
		return nil, malformed("reservation expires malformed (§7.5)")
	}
	return &r, nil
}

// verificationKey resolves (domain, kid) for role at time `at` — the
// §6.4 verification chain: discovery with §5.5 unknown-kid re-fetch,
// §6.2 self-verification (done on key-set load), §6.3 role and
// validity-window enforcement. Unknown kid and role/window failures
// are verification failures (401); resolution failures are
// discovery-failed (502).
func (s *SN) verificationKey(ctx context.Context, domain, kid, role string, at time.Time) (ed25519.PublicKey, *Problem) {
	// Self-domain: a domain knows its own keys — verification
	// consults own_keys directly (the §6.3 resolution machinery
	// exists for REMOTE attribution). This is what lets Redispatch
	// (D-154) ride the real ingest path.
	if domain == s.Domain {
		var seed []byte
		var roles string
		var nb, na sql.NullString
		if err := s.DB.QueryRowContext(ctx,
			`SELECT seed, roles, not_before, not_after FROM own_keys WHERE kid=?`, kid).
			Scan(&seed, &roles, &nb, &na); err != nil {
			return nil, problemf(http.StatusUnauthorized, "signature-invalid",
				"kid %s is not one of this domain's own keys", kid)
		}
		var rr []string
		if json.Unmarshal([]byte(roles), &rr) != nil || !contains(rr, role) {
			return nil, problemf(http.StatusUnauthorized, "signature-invalid",
				"own key %s lacks role %q (§6.3)", kid, role)
		}
		e := discovery.KeyEntry{NotBefore: nb.String, NotAfter: na.String}
		if !e.ValidAt(at) || len(seed) != ed25519.SeedSize {
			return nil, problemf(http.StatusUnauthorized, "signature-invalid",
				"own key %s outside its validity window (§6.3)", kid)
		}
		return ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), nil
	}
	entry, err := s.Resolver.ResolveKID(ctx, domain, kid)
	if err != nil {
		if errors.Is(err, discovery.ErrUnknownKID) {
			return nil, problemf(http.StatusUnauthorized, "signature-invalid", "%v", err)
		}
		return nil, problemf(http.StatusBadGateway, "discovery-failed", "%s: %v", domain, err)
	}
	if !entry.HasRole(role) {
		return nil, problemf(http.StatusUnauthorized, "signature-invalid",
			"key %s of %s lacks role %q (§6.3)", kid, domain, role)
	}
	if !entry.ValidAt(at) {
		return nil, problemf(http.StatusUnauthorized, "signature-invalid",
			"key %s of %s outside its validity window (§6.3)", kid, domain)
	}
	pub, err := entry.Public()
	if err != nil {
		return nil, problemf(http.StatusUnauthorized, "signature-invalid", "%v", err)
	}
	return pub, nil
}
