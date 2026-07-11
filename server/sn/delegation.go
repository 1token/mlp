package sn

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"medialet.org/mlp/core"
)

// DelegationBudget is the default per-(envelope, urn) acceptance
// budget (D-23/D-83). Accepted requests consume it; expired-unused
// reservations MAY refund it (the refund sweep is not yet wired).
const DelegationBudget = 10

// FulfillOutcome is one per-URN entry of the §9.4 response.
type FulfillOutcome struct {
	URN    string `json:"urn"`
	Status string `json:"status"` // will-push / refused
	Reason string `json:"reason,omitempty"`
}

// --- Requester side (§9.3–9.4) -----------------------------------------

// BuildDelegationRequest assembles and signs the §9.4 document for
// source, exercising the credential for the received (recvOrigin,
// recvEnvelopeID) and minting Reservations for our own BS — the
// ingesting party always mints (D-82). The minted reservations_in
// rows are pending immediately; entries the source refuses simply
// expire unused (quarantine GC covers them).
func (s *SN) BuildDelegationRequest(ctx context.Context, source, recvOrigin, recvEnvelopeID string, urns []string) ([]byte, error) {
	now := s.now()
	re, err := s.loadReceived(ctx, recvOrigin, recvEnvelopeID)
	if err != nil {
		return nil, err
	}
	root, err := credentialFor(re, source)
	if err != nil {
		return nil, err
	}
	sizes, err := s.manifestSizes(ctx, re.MedialetCA)
	if err != nil {
		return nil, err
	}

	media := make([]any, 0, len(urns))
	nowS := now.Format(time.RFC3339)
	for _, urn := range urns {
		size, ok := sizes[urn]
		if !ok {
			return nil, fmt.Errorf("mlp/sn: urn %s not in the Medialet's Manifest", urn)
		}
		token, suffix := s.reservationSecret()
		expires := now.Add(ReservationTTL).Format(time.RFC3339)
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO reservations_in (token_hash, urn, max_size, pusher_domain, expires, state, store_id, created)
			 VALUES (?,?,?,?,?,'pending',1,?)`,
			tokenHash(token), urn, size, source, expires, nowS); err != nil {
			return nil, err
		}
		media = append(media, map[string]any{
			"urn": urn,
			"reservation": map[string]any{
				"urn": urn, "max_size": size,
				"target_url": s.IngestBase + suffix,
				"token":      token, "expires": expires,
			},
		})
	}

	payload := map[string]any{
		"mlp":         "0.1",
		"request_id":  s.requestID(now),
		"created":     nowS,
		"requester":   s.Domain,
		"root":        root.asMap(),
		"medialet_ca": re.MedialetCA,
		"media":       media,
	}
	kid, priv, err := s.signingKey(ctx, now)
	if err != nil {
		return nil, err
	}
	sig, _, err := core.SignDoc(priv, "delegation/1", kid, nowS, payload)
	if err != nil {
		return nil, err
	}
	return core.CanonicalizeValue(map[string]any{"payload": payload, "signature": sig})
}

// credentialFor selects the §9.3 step-2 credential: the attestation
// whose origin equals source — from the chain, or constructed from
// the received Envelope itself when source is its origin.
func credentialFor(re *receivedEnvelope, source string) (hopAttestation, error) {
	if source == re.Origin {
		return re.ownAttestation(), nil
	}
	for _, h := range re.Hops {
		if h.Origin == source {
			return h, nil
		}
	}
	return hopAttestation{}, fmt.Errorf("mlp/sn: no attestation for %s in the chain (§9.3)", source)
}

func (s *SN) manifestSizes(ctx context.Context, medialetCA string) (map[string]int64, error) {
	var raw []byte
	if err := s.DB.QueryRowContext(ctx,
		`SELECT raw FROM medialets WHERE content_address=?`, medialetCA).Scan(&raw); err != nil {
		return nil, err
	}
	return manifestSizesFromRaw(raw)
}

func manifestSizesFromRaw(raw []byte) (map[string]int64, error) {
	v, err := core.ParseDialect(raw)
	if err != nil {
		return nil, err
	}
	sm, _ := v.(map[string]any)
	m, _ := sm["medialet"].(map[string]any)
	man, _ := m["manifest"].([]any)
	sizes := map[string]int64{}
	for _, x := range man {
		e, _ := x.(map[string]any)
		urn, _ := e["urn"].(string)
		if num, ok := e["size"].(json.Number); ok && urn != "" {
			if n, err := num.Int64(); err == nil {
				sizes[urn] = n
			}
		}
	}
	return sizes, nil
}

func manifestAvailability(raw []byte) (map[string]string, error) {
	v, err := core.ParseDialect(raw)
	if err != nil {
		return nil, err
	}
	sm, _ := v.(map[string]any)
	m, _ := sm["medialet"].(map[string]any)
	man, _ := m["manifest"].([]any)
	out := map[string]string{}
	for _, x := range man {
		e, _ := x.(map[string]any)
		urn, _ := e["urn"].(string)
		until, _ := e["available_until"].(string)
		if urn != "" {
			out[urn] = until
		}
	}
	return out, nil
}

func (s *SN) requestID(t time.Time) string {
	if s.NewRequestID != nil {
		return s.NewRequestID(t)
	}
	return randomUUIDv7(t)
}

// ErrUnavailable is the §9.3 graceful terminal state: every candidate
// refused or failed — the client renders "request a resend" (D-23).
var ErrUnavailable = errors.New("mlp/sn: no fulfillment source will push — request a resend (§9.3)")

// RequestFulfillment runs the §9.3 requester flow for deferred URNs
// of a received forwarded Envelope: candidates in fulfillment_sources
// order (default the received origin), non-chain-members discarded,
// each POSTed a signed delegation request until every URN is promised
// or candidates are exhausted. It returns the per-URN outcomes from
// the first source that accepted anything.
func (s *SN) RequestFulfillment(ctx context.Context, recvOrigin, recvEnvelopeID string, urns []string) ([]FulfillOutcome, error) {
	re, err := s.loadReceived(ctx, recvOrigin, recvEnvelopeID)
	if err != nil {
		return nil, err
	}
	var candidates []string
	seen := map[string]bool{}
	for _, src := range re.Sources {
		d, _ := src["domain"].(string)
		if d == "" || seen[d] || !re.chainMember(d) {
			continue // non-chain-members discarded (§9.2/§9.3)
		}
		seen[d] = true
		candidates = append(candidates, d)
	}
	if len(candidates) == 0 {
		candidates = []string{re.Origin}
	}

	for _, source := range candidates {
		doc, err := s.BuildDelegationRequest(ctx, source, recvOrigin, recvEnvelopeID, urns)
		if err != nil {
			continue
		}
		outcomes, err := s.postFulfill(ctx, source, doc)
		if err != nil {
			continue // refusal or timeout: next candidate (§9.3 step 4)
		}
		anyPush := false
		for _, o := range outcomes {
			if o.Status == "will-push" {
				anyPush = true
			}
		}
		if anyPush {
			return outcomes, nil
		}
	}
	return nil, ErrUnavailable
}

// postFulfill discovers source and POSTs the request to its /fulfill
// endpoint. The FulfillClient and FulfillEndpoint hooks exist for
// tests; production discovers the sn URL authoritatively (§5).
func (s *SN) postFulfill(ctx context.Context, source string, doc []byte) ([]FulfillOutcome, error) {
	endpoint := s.FulfillEndpoint
	if endpoint == nil {
		endpoint = func(ctx context.Context, domain string) (string, error) {
			d, err := s.Resolver.Resolve(ctx, domain)
			if err != nil {
				return "", err
			}
			return d.SN + "/fulfill", nil
		}
	}
	target, err := endpoint(ctx, source)
	if err != nil {
		return nil, err
	}
	client := s.FulfillClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(doc))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", ctDelegation)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mlp/sn: %s answered %d", source, resp.StatusCode)
	}
	var body struct {
		Media []FulfillOutcome `json:"media"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, err
	}
	return body.Media, nil
}

// --- Source side (§9.5) -------------------------------------------------

// ProcessFulfill validates a delegation request in the §9.5 order and
// answers the unsigned §9.4 response (D-83). Accepted entries consume
// budget and enqueue reservations_out for our BS pusher.
func (s *SN) ProcessFulfill(ctx context.Context, raw []byte) ([]byte, *Problem) {
	now := s.now()

	// Step 1: schema, version, skew, signature.
	pd, prob := s.parseDelegation(ctx, raw, now)
	if prob != nil {
		return nil, prob
	}

	// Dedup on (requester, request_id): replays answer the prior
	// response and consume no budget (§9.4).
	if prior, ok, prob := s.priorFulfillResponse(ctx, pd); prob != nil {
		return nil, prob
	} else if ok {
		return prior, nil
	}

	// Step 2: the credential against our own dispatch records.
	var storedSig, storedKID, storedCreated, medialetCA string
	err := s.DB.QueryRowContext(ctx,
		`SELECT hop_sig_value, hop_kid, created, medialet_ca FROM dispatches WHERE envelope_id=?`,
		pd.Root.EnvelopeID).Scan(&storedSig, &storedKID, &storedCreated, &medialetCA)
	switch {
	case pd.Root.Origin != s.Domain, errors.Is(err, sql.ErrNoRows):
		return nil, problemf(http.StatusNotFound, "unknown-envelope",
			"no such dispatch in this domain's records (§9.5 step 2)")
	case err != nil:
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if storedSig != pd.Root.Sig || storedKID != pd.Root.KID || storedCreated != pd.Root.Created {
		// We stored what we signed (D-51); inequality means the
		// presented attestation is not ours.
		return nil, problemf(http.StatusNotFound, "unknown-envelope",
			"attestation does not match the recorded dispatch (§9.5 step 2)")
	}

	// Step 3: content binding.
	if medialetCA != pd.MedialetCA {
		return nil, problemf(http.StatusConflict, "medialet-mismatch",
			"attestation spliced onto foreign content (§9.5 step 3, D-83)")
	}

	// Step 4 prerequisites: the Manifest of the dispatched Medialet.
	var smRaw []byte
	if err := s.DB.QueryRowContext(ctx,
		`SELECT raw FROM medialets WHERE content_address=?`, medialetCA).Scan(&smRaw); err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	sizes, err := manifestSizesFromRaw(smRaw)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "stored medialet: %v", err)
	}
	availability, err := manifestAvailability(smRaw)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "stored medialet: %v", err)
	}

	outcomes := make([]FulfillOutcome, 0, len(pd.Media))
	var accepted []delegatedGrant
	for _, m := range pd.Media {
		size, inManifest := sizes[m.URN]
		if !inManifest {
			outcomes = append(outcomes, FulfillOutcome{URN: m.URN, Status: "refused", Reason: "not-available"})
			continue
		}
		// The Reservation's max_size must equal the Manifest size —
		// a malformed request member is a request-level failure.
		if m.Reservation.MaxSize != size {
			return nil, problemf(http.StatusBadRequest, "malformed",
				"reservation max_size %d differs from the Manifest size %d (§9.5 step 4)", m.Reservation.MaxSize, size)
		}
		if until, err := time.Parse(time.RFC3339, availability[m.URN]); err != nil || now.After(until) {
			outcomes = append(outcomes, FulfillOutcome{URN: m.URN, Status: "refused", Reason: "not-available"})
			continue
		}
		var live int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM objects WHERE urn=? AND state='live'`, m.URN).Scan(&live); err != nil || live == 0 {
			outcomes = append(outcomes, FulfillOutcome{URN: m.URN, Status: "refused", Reason: "not-available"})
			continue
		}
		var used int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM delegations WHERE envelope_id=? AND urn=? AND status='accepted'`,
			pd.Root.EnvelopeID, m.URN).Scan(&used); err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		if used >= DelegationBudget {
			outcomes = append(outcomes, FulfillOutcome{URN: m.URN, Status: "refused", Reason: "delegation-budget"})
			continue
		}
		outcomes = append(outcomes, FulfillOutcome{URN: m.URN, Status: "will-push"})
		accepted = append(accepted, delegatedGrant{urn: m.URN, res: m.Reservation})
	}

	// Persist outcomes (budget at acceptance, D-83) and enqueue the
	// pushes for our BS (§9.5: an intra-domain hand-off; the pusher
	// applies D-72 to the requester-supplied target_url).
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer tx.Rollback()
	nowS := now.Format(time.RFC3339)
	for _, o := range outcomes {
		status := "refused"
		if o.Status == "will-push" {
			status = "accepted"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO delegations (request_id, requester, envelope_id, urn, status, reason, created)
			 VALUES (?,?,?,?,?,?,?)`,
			pd.RequestID, pd.Requester, pd.Root.EnvelopeID, o.URN, status, nullable(o.Reason), nowS); err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
	}
	for _, g := range accepted {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO reservations_out (urn, target_url, token, max_size, expires, envelope_id, state)
			 VALUES (?,?,?,?,?,?,'pending')`,
			g.urn, g.res.TargetURL, g.res.Token, g.res.MaxSize, g.res.Expires, pd.Root.EnvelopeID); err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	return marshalFulfillResponse(outcomes)
}

type delegatedGrant struct {
	urn string
	res *Reservation
}

func marshalFulfillResponse(outcomes []FulfillOutcome) ([]byte, *Problem) {
	entries := make([]any, len(outcomes))
	for i, o := range outcomes {
		e := map[string]any{"urn": o.URN, "status": o.Status}
		if o.Reason != "" {
			e["reason"] = o.Reason
		}
		entries[i] = e
	}
	body, err := core.CanonicalizeValue(map[string]any{"media": entries})
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "response: %v", err)
	}
	return body, nil
}

// priorFulfillResponse rebuilds the response of an already-processed
// (requester, request_id) from the delegations rows.
func (s *SN) priorFulfillResponse(ctx context.Context, pd *parsedDelegation) ([]byte, bool, *Problem) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT urn, status, reason FROM delegations WHERE requester=? AND request_id=? ORDER BY rowid`,
		pd.Requester, pd.RequestID)
	if err != nil {
		return nil, false, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	var outcomes []FulfillOutcome
	for rows.Next() {
		var urn, status string
		var reason sql.NullString
		if err := rows.Scan(&urn, &status, &reason); err != nil {
			return nil, false, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		o := FulfillOutcome{URN: urn, Reason: reason.String}
		if status == "accepted" {
			o.Status = "will-push"
		} else {
			o.Status = "refused"
		}
		outcomes = append(outcomes, o)
	}
	if err := rows.Err(); err != nil {
		return nil, false, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if len(outcomes) == 0 {
		return nil, false, nil
	}
	body, prob := marshalFulfillResponse(outcomes)
	return body, prob == nil, prob
}

// parsedDelegation is a schema-valid, signature-verified §9.4 request.
type parsedDelegation struct {
	RequestID  string
	Created    string
	Requester  string
	Root       hopAttestation
	MedialetCA string
	Media      []struct {
		URN         string
		Reservation *Reservation
	}
}

// parseDelegation runs §9.5 step 1.
func (s *SN) parseDelegation(ctx context.Context, raw []byte, now time.Time) (*parsedDelegation, *Problem) {
	v, err := core.ParseDialect(raw)
	if err != nil {
		return nil, malformed("%v", err)
	}
	top, ok := v.(map[string]any)
	if !ok {
		return nil, malformed("top level is not an object")
	}
	p, ok := top["payload"].(map[string]any)
	if !ok {
		return nil, malformed("missing delegation payload (§9.4)")
	}
	sig, ok := sigShape(top["signature"])
	if !ok {
		return nil, malformed("malformed delegation signature object (§6.4)")
	}
	pd := &parsedDelegation{}
	mlp, ok := p["mlp"].(string)
	if !ok {
		return nil, malformed("missing mlp (§9.4)")
	}
	if mlp != "0.1" {
		return nil, problemf(http.StatusBadRequest, "version-unsupported", "delegation mlp %q", mlp)
	}
	if pd.RequestID, ok = p["request_id"].(string); !ok || !idGrammar(pd.RequestID) {
		return nil, malformed("malformed request_id (§9.4)")
	}
	if pd.Created, ok = p["created"].(string); !ok || !rfc3339utc(pd.Created) {
		return nil, malformed("malformed created (§9.4)")
	}
	created, _ := time.Parse(time.RFC3339, pd.Created)
	if d := now.Sub(created); d > 48*time.Hour || d < -48*time.Hour {
		return nil, problemf(http.StatusBadRequest, "timestamp-skew",
			"created %s outside ±48 h (§9.4)", pd.Created)
	}
	if pd.Requester, ok = p["requester"].(string); !ok || validDomain(pd.Requester) != nil {
		return nil, malformed("malformed requester (§9.4)")
	}
	root, ok := p["root"].(map[string]any)
	if !ok {
		return nil, malformed("missing root attestation (§9.4)")
	}
	for m, dst := range map[string]*string{
		"origin": &pd.Root.Origin, "envelope_id": &pd.Root.EnvelopeID,
		"created": &pd.Root.Created, "kid": &pd.Root.KID, "sig": &pd.Root.Sig,
	} {
		if *dst, ok = root[m].(string); !ok {
			return nil, malformed("root attestation missing %s (§3.4.2)", m)
		}
	}
	if pd.MedialetCA, ok = p["medialet_ca"].(string); !ok {
		return nil, malformed("missing medialet_ca (§9.4)")
	}
	if _, err := core.ParseURNMlet(pd.MedialetCA); err != nil {
		return nil, malformed("medialet_ca: %v", err)
	}
	mediaRaw, ok := p["media"].([]any)
	if !ok || len(mediaRaw) == 0 {
		return nil, malformed("media missing or empty (§9.4)")
	}
	seen := map[string]bool{}
	for _, x := range mediaRaw {
		e, ok := x.(map[string]any)
		if !ok {
			return nil, malformed("media entry is not an object (§9.4)")
		}
		var entry struct {
			URN         string
			Reservation *Reservation
		}
		if entry.URN, ok = e["urn"].(string); !ok || seen[entry.URN] {
			return nil, malformed("media urn missing or repeated (§9.4)")
		}
		seen[entry.URN] = true
		res, present := e["reservation"]
		if !present {
			return nil, malformed("media entry lacks its reservation (§9.4, D-82)")
		}
		r, prob := parseReservation(res, entry.URN)
		if prob != nil {
			return nil, prob
		}
		entry.Reservation = r
		pd.Media = append(pd.Media, entry)
	}

	// delegation/1 against the requester's sn keys (§9.4).
	kid, _ := sig["protected"].(map[string]any)["kid"].(string)
	pub, prob := s.verificationKey(ctx, pd.Requester, kid, "sn", now)
	if prob != nil {
		return nil, prob
	}
	if err := core.VerifyDoc(pub, "delegation/1", p, sig); err != nil {
		return nil, problemf(http.StatusUnauthorized, "signature-invalid", "delegation/1: %v", err)
	}
	return pd, nil
}
