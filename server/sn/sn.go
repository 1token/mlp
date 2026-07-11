package sn

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/zeebo/blake3"
	"medialet.org/mlp/core"
	"medialet.org/mlp/discovery"
)

// IdempotencyWindow bounds the D-74 retry-idempotency behavior
// (RECOMMENDED 24 h).
const IdempotencyWindow = 24 * time.Hour

// ReservationTTL is the RECOMMENDED reservation lifetime (§7.5,
// D-18): issue-time + 72 h.
const ReservationTTL = 72 * time.Hour

// SN is one domain's Signaling Node. Hook fields default to
// production behavior and exist for deterministic conformance tests.
type SN struct {
	DB         *sql.DB
	Resolver   *discovery.Resolver
	Domain     string // the domain this SN serves (§3.4.4 item 3 locality)
	IngestBase string // BS ingestion URL prefix for minted reservations (§7.5)

	Now          func() time.Time
	NewVerdictID func(t time.Time) string // default: random UUIDv7
	// FulfillClient/FulfillEndpoint override the §9.3 POST transport
	// and endpoint discovery (tests); production resolves via §5.
	FulfillClient   *http.Client
	FulfillEndpoint func(ctx context.Context, domain string) (string, error)
	// NewEnvelopeID mints envelope identifiers for forwards (§3.4.2).
	NewEnvelopeID func(t time.Time) string
	// NewRequestID mints delegation request identifiers (§9.4).
	NewRequestID func(t time.Time) string
	// NewMedialetID mints Medialet identifiers for the composer.
	NewMedialetID func(t time.Time) string
	// DispatchEndpoint overrides §5 discovery of a target's /dispatch.
	DispatchEndpoint func(ctx context.Context, domain string) (string, error)
	// NewReservationSecret returns a capability token (1–512 chars)
	// and a URL suffix appended to IngestBase.
	NewReservationSecret func() (token, urlSuffix string)
}

func (s *SN) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *SN) verdictID(t time.Time) string {
	if s.NewVerdictID != nil {
		return s.NewVerdictID(t)
	}
	return randomUUIDv7(t)
}

func (s *SN) reservationSecret() (string, string) {
	if s.NewReservationSecret != nil {
		return s.NewReservationSecret()
	}
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is not a recoverable condition
	}
	var p [8]byte
	if _, err := rand.Read(p[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), hex.EncodeToString(p[:])
}

// randomUUIDv7 builds an RFC 9562 UUIDv7 at t with random tail.
func randomUUIDv7(t time.Time) string {
	var b [16]byte
	ms := uint64(t.UnixMilli())
	b[0], b[1], b[2] = byte(ms>>40), byte(ms>>32), byte(ms>>24)
	b[3], b[4], b[5] = byte(ms>>16), byte(ms>>8), byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		panic(err)
	}
	b[6] = 0x70 | (b[6] & 0x0F)
	b[8] = 0x80 | (b[8] & 0x3F)
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func tokenHash(token string) string {
	sum := blake3.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ProcessDispatch handles POST {sn}/dispatch (§7.3): the §3.4.4
// validation sequence, the §7.7 acceptance policy, and a signed
// verdict for verified Envelopes — including verified rejections.
// Unverified material gets an unsigned Problem (D-69).
func (s *SN) ProcessDispatch(ctx context.Context, raw []byte) ([]byte, *Problem) {
	now := s.now()

	// Items 1–5.
	pe, prob := ParseEnvelope(raw, now, s.Domain)
	if prob != nil {
		return nil, prob
	}

	// Item 6: replay, refined by D-74 retry idempotency.
	var priorCA, receivedAt string
	err := s.DB.QueryRowContext(ctx,
		`SELECT medialet_ca, received_at FROM envelopes_in WHERE origin=? AND envelope_id=?`,
		pe.Origin, pe.EnvelopeID).Scan(&priorCA, &receivedAt)
	switch {
	case err == nil:
		rec, perr := time.Parse(time.RFC3339, receivedAt)
		if perr == nil && priorCA == pe.ContentAddress && now.Sub(rec) <= IdempotencyWindow {
			return s.currentVerdictSnapshot(ctx, pe.Origin, pe.EnvelopeID)
		}
		return nil, problemf(http.StatusConflict, "replay",
			"(%s, %s) already dispatched (§3.4.4 item 6, D-74)", pe.Origin, pe.EnvelopeID)
	case errors.Is(err, sql.ErrNoRows):
		// fresh dispatch
	default:
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}

	// Item 7: Hop Signature against origin's sn keys; Author
	// Signature against the author domain's author keys (D-32).
	hopKid, _ := pe.HopSig["protected"].(map[string]any)["kid"].(string)
	pub, prob := s.verificationKey(ctx, pe.Origin, hopKid, "sn", now)
	if prob != nil {
		return nil, prob
	}
	if err := core.VerifyDoc(pub, "hop/1", pe.Envelope, pe.HopSig); err != nil {
		return nil, problemf(http.StatusUnauthorized, "signature-invalid", "hop/1: %v", err)
	}
	authKid, _ := pe.AuthSig["protected"].(map[string]any)["kid"].(string)
	apub, prob := s.verificationKey(ctx, pe.AuthorDomain, authKid, "author", now)
	if prob != nil {
		return nil, prob
	}
	if err := core.VerifyDoc(apub, "author/1", pe.Medialet, pe.AuthSig); err != nil {
		return nil, problemf(http.StatusUnauthorized, "signature-invalid", "author/1: %v", err)
	}

	// Acceptance policy (§7.7) — only now (§3.4.4: recipient
	// existence and verdicts are policy outcomes).
	recipients, targets, anyT1, anyAccepted, anyQuarantined, prob := s.evaluateRecipients(ctx, pe)
	if prob != nil {
		return nil, prob
	}
	media, reservations, prob := s.mediaOutcomes(ctx, pe, now, anyT1, anyAccepted, anyQuarantined)
	if prob != nil {
		return nil, prob
	}

	payload := BuildVerdictPayload(s.Domain, pe.Origin, pe.EnvelopeID,
		s.verdictID(now), now.Format(time.RFC3339), recipients, media)
	_, canon, err := s.signVerdict(ctx, payload, now)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "sign: %v", err)
	}

	if prob := s.persistDispatch(ctx, pe, payload, canon, reservations, now); prob != nil {
		return nil, prob
	}
	if prob := s.materialize(ctx, pe, targets, now); prob != nil {
		return nil, prob
	}
	return canon, nil
}

// currentVerdictSnapshot re-serves the latest outbound verdict for an
// already-verdicted dispatch (D-74).
func (s *SN) currentVerdictSnapshot(ctx context.Context, origin, envelopeID string) ([]byte, *Problem) {
	var doc []byte
	err := s.DB.QueryRowContext(ctx,
		`SELECT doc FROM verdicts WHERE direction='out' AND envelope_origin=? AND envelope_id=?
		 ORDER BY created DESC, verdict_id DESC LIMIT 1`, origin, envelopeID).Scan(&doc)
	if err != nil {
		return nil, problemf(http.StatusConflict, "replay", "no verdict snapshot for retry: %v", err)
	}
	return doc, nil
}

type tier int

const (
	tierCorrespondent tier = 1
	tierStranger      tier = 2
	tierSuspect       tier = 3
)

// evaluateRecipients applies the §7.7 defaults per envelope_to entry.
// Trust matching compares mailbox keys (§4.2, D-55) against the
// author (the sender identity the Medialet attests).
// recipientTarget pairs an outcome with its resolved local mailbox
// (0 when the recipient is unknown) for the S4.9 materialization.
type recipientTarget struct {
	addr      string
	mailboxID int64
	verdict   string
}

func (s *SN) evaluateRecipients(ctx context.Context, pe *ParsedEnvelope) (out []RecipientOutcome, targets []recipientTarget, anyT1, anyAccepted, anyQuarantined bool, prob *Problem) {
	senderKey := MailboxKey(pe.Author)
	for _, addr := range pe.EnvelopeTo {
		key := MailboxKey(addr)
		local := key[:len(key)-len(s.Domain)-1] // key is local@s.Domain by item 3

		var mailboxID int64
		err := s.DB.QueryRowContext(ctx,
			`SELECT id FROM mailboxes WHERE local_part=?`, local).Scan(&mailboxID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			out = append(out, RecipientOutcome{Addr: addr, Verdict: "rejected", Reason: "unknown-recipient"})
			continue
		case err != nil:
			return nil, nil, false, false, false, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}

		t := tierStranger
		var override sql.NullString
		var firstOutbound sql.NullString
		err = s.DB.QueryRowContext(ctx,
			`SELECT tier_override, first_outbound_at FROM correspondents WHERE mailbox_id=? AND addr=?`,
			mailboxID, senderKey).Scan(&override, &firstOutbound)
		switch {
		case err == nil:
			switch {
			case override.String == "block":
				t = tierSuspect
			case override.String == "allow" || firstOutbound.Valid:
				t = tierCorrespondent
			}
		case errors.Is(err, sql.ErrNoRows):
			// no relationship recorded
		default:
			return nil, nil, false, false, false, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		if t == tierStranger && pe.AuthorDomain == s.Domain {
			t = tierCorrespondent // same domain (§7.7 Tier 1)
		}

		switch t {
		case tierCorrespondent:
			anyT1, anyAccepted = true, true
			out = append(out, RecipientOutcome{Addr: addr, Verdict: "accepted"})
			targets = append(targets, recipientTarget{addr: addr, mailboxID: mailboxID, verdict: "accepted"})
		case tierStranger:
			anyAccepted = true
			out = append(out, RecipientOutcome{Addr: addr, Verdict: "accepted"})
			targets = append(targets, recipientTarget{addr: addr, mailboxID: mailboxID, verdict: "accepted"})
		case tierSuspect:
			anyQuarantined = true
			out = append(out, RecipientOutcome{Addr: addr, Verdict: "quarantined", Reason: "policy"})
			targets = append(targets, recipientTarget{addr: addr, mailboxID: mailboxID, verdict: "quarantined"})
		}
	}
	return out, targets, anyT1, anyAccepted, anyQuarantined, nil
}

// mediaOutcomes computes the §7.4 per-URN verdicts as the union need
// across accepted recipients (D-70), minting Reservations for grants.
// A fully rejected Envelope omits media entirely.
func (s *SN) mediaOutcomes(ctx context.Context, pe *ParsedEnvelope, now time.Time, anyT1, anyAccepted, anyQuarantined bool) ([]MediaOutcome, []*Reservation, *Problem) {
	if len(pe.Manifest) == 0 || (!anyAccepted && !anyQuarantined) {
		return nil, nil, nil
	}
	var media []MediaOutcome
	var minted []*Reservation
	for _, me := range pe.Manifest {
		switch {
		case anyT1:
			var live int
			err := s.DB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM objects WHERE urn=? AND state='live'`, me.URN).Scan(&live)
			if err != nil {
				return nil, nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
			}
			if live > 0 {
				// Tier 1: possession disclosed (§7.7; §7.5 masking
				// applies only to non-correspondents).
				media = append(media, MediaOutcome{URN: me.URN, Verdict: "have"})
				continue
			}
			// Quota headroom is stubbed to "available" until object
			// accounting lands with the BS in S4.5.
			token, suffix := s.reservationSecret()
			r := &Reservation{
				URN:       me.URN,
				MaxSize:   me.Size,
				TargetURL: s.IngestBase + suffix,
				Token:     token,
				Expires:   now.Add(ReservationTTL).Format(time.RFC3339),
			}
			media = append(media, MediaOutcome{URN: me.URN, Verdict: "grant", Reservation: r})
			minted = append(minted, r)
		case anyAccepted:
			// Tier 2 default (§7.7): defer pending recipient
			// acceptance; possession, if any, is not disclosed.
			media = append(media, MediaOutcome{URN: me.URN, Verdict: "defer", Reason: "pending-acceptance"})
		default:
			// Quarantined-only (§7.7 Tier 3: "defer or deny").
			media = append(media, MediaOutcome{URN: me.URN, Verdict: "defer", Reason: "policy"})
		}
	}
	return media, minted, nil
}

// persistDispatch atomically records the accepted dispatch: the
// Signed Medialet (verbatim canonical form, D-28/D-46), the Delivery
// Record (D-53), the outbound verdict snapshot with its per-URN rows
// (D-71/D-149), and minted reservations (token hash only, D-192).
func (s *SN) persistDispatch(ctx context.Context, pe *ParsedEnvelope, payload map[string]any, canon []byte, reservations []*Reservation, now time.Time) *Problem {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer tx.Rollback()
	nowS := now.Format(time.RFC3339)
	mCreated, _ := pe.Medialet["created"].(string)

	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO medialets (content_address, author, medialet_id, created, raw)
		 VALUES (?,?,?,?,?)`,
		pe.ContentAddress, pe.Author, pe.MedialetID, mCreated, pe.CanonicalMedialet); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	var have int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM medialets WHERE content_address=?`, pe.ContentAddress).Scan(&have); err != nil || have == 0 {
		// The (author, id) pair exists under different content — the
		// D-46 dedup scope refuses divergent reuse.
		return problemf(http.StatusConflict, "replay",
			"(author, id) already bound to different content (D-46)")
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO envelopes_in (origin, envelope_id, medialet_ca, received_at,
		   forwarded_by, hops_json, fulfillment_sources_json,
		   author_sig_result, author_sig_kid, author_verified_at,
		   hop_sig_result, hop_sig_kid, hop_verified_at,
		   envelope_created, hop_sig_value)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		pe.Origin, pe.EnvelopeID, pe.ContentAddress, nowS,
		nullable(pe.ForwardedBy), nullable(pe.HopsJSON), nullable(pe.FulfillSrcJSON),
		"ok", kidOf(pe.AuthSig), nowS,
		"ok", kidOf(pe.HopSig), nowS,
		pe.Created, sigValue(pe.HopSig)); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}

	if prob := storeVerdict(ctx, tx, "out", payload, canon); prob != nil {
		return prob
	}

	for _, r := range reservations {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO reservations_in (token_hash, urn, max_size, pusher_domain, expires, state, store_id, created)
			 VALUES (?,?,?,?,?,'pending',1,?)`,
			tokenHash(r.Token), r.URN, r.MaxSize, pe.Origin, r.Expires, nowS); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	return nil
}

// RecipientAccept completes the Tier-2 flow (§7.7): the recipient's
// accept action upgrades every deferred URN of (origin, envelopeID)
// to grant with a fresh Reservation, issued as a new idempotent
// snapshot (§7.6) to be POSTed to the origin's /verdict endpoint.
func (s *SN) RecipientAccept(ctx context.Context, origin, envelopeID string) ([]byte, error) {
	now := s.now()
	prior, err := s.latestOutVerdict(ctx, origin, envelopeID)
	if err != nil {
		return nil, err
	}
	recipients := prior.Recipients
	var media []MediaOutcome
	var minted []*Reservation
	for _, m := range prior.Media {
		if m.Verdict != "defer" {
			media = append(media, m)
			continue
		}
		var size int64
		size, err = s.manifestSize(ctx, origin, envelopeID, m.URN)
		if err != nil {
			return nil, err
		}
		token, suffix := s.reservationSecret()
		r := &Reservation{
			URN:       m.URN,
			MaxSize:   size,
			TargetURL: s.IngestBase + suffix,
			Token:     token,
			Expires:   now.Add(ReservationTTL).Format(time.RFC3339),
		}
		media = append(media, MediaOutcome{URN: m.URN, Verdict: "grant", Reservation: r})
		minted = append(minted, r)
	}

	payload := BuildVerdictPayload(s.Domain, origin, envelopeID,
		s.verdictID(now), now.Format(time.RFC3339), recipients, media)
	_, canon, err := s.signVerdict(ctx, payload, now)
	if err != nil {
		return nil, err
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if prob := storeVerdict(ctx, tx, "out", payload, canon); prob != nil {
		return nil, errors.New(prob.Detail)
	}
	nowS := now.Format(time.RFC3339)
	for _, r := range minted {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO reservations_in (token_hash, urn, max_size, pusher_domain, expires, state, store_id, created)
			 VALUES (?,?,?,?,?,'pending',1,?)`,
			tokenHash(r.Token), r.URN, r.MaxSize, origin, r.Expires, nowS); err != nil {
			return nil, err
		}
	}
	return canon, tx.Commit()
}

// manifestSize recovers a URN's Manifest-declared size from the
// stored Signed Medialet of (origin, envelopeID).
func (s *SN) manifestSize(ctx context.Context, origin, envelopeID, urn string) (int64, error) {
	var raw []byte
	if err := s.DB.QueryRowContext(ctx,
		`SELECT m.raw FROM envelopes_in e JOIN medialets m ON m.content_address = e.medialet_ca
		 WHERE e.origin=? AND e.envelope_id=?`, origin, envelopeID).Scan(&raw); err != nil {
		return 0, err
	}
	v, err := core.ParseDialect(raw)
	if err != nil {
		return 0, err
	}
	sm, _ := v.(map[string]any)
	m, _ := sm["medialet"].(map[string]any)
	man, _ := m["manifest"].([]any)
	for _, x := range man {
		e, _ := x.(map[string]any)
		if u, _ := e["urn"].(string); u == urn {
			if num, ok := e["size"].(json.Number); ok {
				return num.Int64()
			}
		}
	}
	return 0, fmt.Errorf("mlp/sn: urn %s not in the stored manifest", urn)
}

// latestOutVerdict loads and re-parses the newest outbound snapshot
// for (origin, envelopeID).
func (s *SN) latestOutVerdict(ctx context.Context, origin, envelopeID string) (*ParsedVerdict, error) {
	var doc []byte
	if err := s.DB.QueryRowContext(ctx,
		`SELECT doc FROM verdicts WHERE direction='out' AND envelope_origin=? AND envelope_id=?
		 ORDER BY created DESC, verdict_id DESC LIMIT 1`, origin, envelopeID).Scan(&doc); err != nil {
		return nil, err
	}
	// Own documents: schema re-parse without signature verification.
	v, err := core.ParseDialect(doc)
	if err != nil {
		return nil, err
	}
	top, _ := v.(map[string]any)
	payload, _ := top["payload"].(map[string]any)
	return parseVerdictPayload(payload)
}

func kidOf(sig map[string]any) string {
	p, _ := sig["protected"].(map[string]any)
	kid, _ := p["kid"].(string)
	return kid
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// storeVerdict inserts a verdict snapshot and its per-URN rows.
// UNIQUE (direction, issuer, verdict_id) makes re-insertion of the
// same snapshot a no-op (idempotency).
func storeVerdict(ctx context.Context, tx *sql.Tx, direction string, payload map[string]any, doc []byte) *Problem {
	pv, err := parseVerdictPayload(payload)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "verdict payload: %v", err)
	}
	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO verdicts (direction, verdict_id, created, issuer, envelope_origin, envelope_id, message, doc)
		 VALUES (?,?,?,?,?,?,?,?)`,
		direction, pv.VerdictID, pv.Created, pv.Issuer, pv.EnvOrigin, pv.EnvelopeID, pv.Message, doc)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil // duplicate snapshot: idempotent
	}
	var row int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM verdicts WHERE direction=? AND issuer=? AND verdict_id=?`,
		direction, pv.Issuer, pv.VerdictID).Scan(&row); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	for _, m := range pv.Media {
		var resJSON any
		if m.Reservation != nil {
			b, _ := json.Marshal(map[string]any{
				"urn": m.Reservation.URN, "max_size": m.Reservation.MaxSize,
				"target_url": m.Reservation.TargetURL, "token": m.Reservation.Token,
				"expires": m.Reservation.Expires,
			})
			resJSON = string(b)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO verdict_media (verdict_row, urn, verdict, reason, reservation_json)
			 VALUES (?,?,?,?,?)`,
			row, m.URN, m.Verdict, nullable(m.Reason), resJSON); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
	}
	return nil
}

// parseVerdictPayload extracts a ParsedVerdict from an
// already-trusted payload value (own snapshots; schema errors are
// internal bugs, not wire conditions).
func parseVerdictPayload(p map[string]any) (*ParsedVerdict, error) {
	if p == nil {
		return nil, errors.New("mlp/sn: nil verdict payload")
	}
	pv := &ParsedVerdict{Payload: p}
	pv.VerdictID, _ = p["verdict_id"].(string)
	pv.Created, _ = p["created"].(string)
	pv.Issuer, _ = p["issuer"].(string)
	pv.EnvOrigin, _ = p["envelope_origin"].(string)
	pv.EnvelopeID, _ = p["envelope_id"].(string)
	pv.Message, _ = p["message"].(string)
	if recs, ok := p["recipients"].([]any); ok {
		for _, x := range recs {
			e, _ := x.(map[string]any)
			ro := RecipientOutcome{}
			ro.Addr, _ = e["addr"].(string)
			ro.Verdict, _ = e["verdict"].(string)
			ro.Reason, _ = e["reason"].(string)
			pv.Recipients = append(pv.Recipients, ro)
		}
	}
	if list, ok := p["media"].([]any); ok {
		pv.HasMedia = true
		for _, x := range list {
			e, _ := x.(map[string]any)
			mo := MediaOutcome{}
			mo.URN, _ = e["urn"].(string)
			mo.Verdict, _ = e["verdict"].(string)
			mo.Reason, _ = e["reason"].(string)
			if r, ok := e["reservation"].(map[string]any); ok {
				res := &Reservation{}
				res.URN, _ = r["urn"].(string)
				if num, ok := r["max_size"].(json.Number); ok {
					res.MaxSize, _ = num.Int64()
				}
				res.TargetURL, _ = r["target_url"].(string)
				res.Token, _ = r["token"].(string)
				res.Expires, _ = r["expires"].(string)
				mo.Reservation = res
			}
			pv.Media = append(pv.Media, mo)
		}
	}
	return pv, nil
}

func sigValue(sig map[string]any) string {
	v, _ := sig["value"].(string)
	return v
}
