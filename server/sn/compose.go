package sn

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"

	"fmt"
	"io"
	"medialet.org/mlp/render"
	"net/http"
	"time"

	"medialet.org/mlp/core"
)

// The composer's server half (S3.3, D-133–D-138): building the
// Signed Medialet from a draft, the silent pre-flight, the per-domain
// Envelope fan-out (§3.4.1: envelope_to shares one domain), dispatch
// with synchronous verdict recording, and the origin-side
// materialization — deliveries, dispatches(delivery_id), promised
// refs, the sender's own message copy (envelope_in NULL), timeline.
//
// Signing is the SN's act (D-13): both the author/1 and hop/1 keys
// come from own_keys. The 10-second undo hold is the client's
// (D-138); by the time Send runs, the signature moment has arrived.

// DraftContent is the unsigned medialet material a draft carries.
type DraftContent struct {
	Subject     string          `json:"subject,omitempty"`
	BodyContent string          `json:"body_content"`
	InReplyTo   string          `json:"in_reply_to,omitempty"`
	DisplayedTo []DisplayedTo   `json:"displayed_to,omitempty"`
	Recipients  []string        `json:"recipients"`
	Manifest    []ManifestEntry `json:"manifest,omitempty"`
	JobTag      string          `json:"job_tag,omitempty"`
	Guests      []string        `json:"guests,omitempty"`    // D-151: named, never in envelope_to
	GuestPIN    bool            `json:"guest_pin,omitempty"` // D-152: per-draft second-channel PINs
}

// DisplayedTo is the visible recipient list entry (D-03 honesty:
// what the Medialet displays, distinct from routing).
type DisplayedTo struct {
	Addr string `json:"addr"`
	Name string `json:"name,omitempty"`
}

// SendResult reports one send: the delivery row and the per-domain
// synchronous outcomes.
type SendResult struct {
	DeliveryID int64           `json:"delivery_id"`
	MedialetCA string          `json:"medialet_ca"`
	Targets    []TargetOutcome `json:"targets"`
	Guests     []GuestOutcome  `json:"guests,omitempty"` // D-151–D-153
}

type TargetOutcome struct {
	Domain     string `json:"domain"`
	EnvelopeID string `json:"envelope_id"`
	Message    string `json:"message"` // the synchronous verdict message
}

// Send runs pre-flight, signs, fans out, and materializes. It
// returns a Problem for pre-flight failures (the client renders the
// single ready/blocked state, D-138) and transport errors per target
// inside the result (a failed target does not unsend the others).
func (s *SN) Send(ctx context.Context, mailboxID int64, addr string, d *DraftContent) (*SendResult, *Problem) {
	now := s.now()

	// --- Pre-flight (D-138), silent and total -----------------------
	if len(d.Recipients)+len(d.Guests) == 0 || len(d.Recipients) > 128 {
		return nil, problemf(http.StatusBadRequest, "malformed", "1–128 recipients (§3.4.1); a guests-only send is permitted")
	}
	byDomain := map[string][]string{}
	var domainOrder []string
	for _, r := range d.Recipients {
		_, dom, err := ParseAddress(r)
		if err != nil {
			return nil, problemf(http.StatusBadRequest, "malformed", "recipient %q: %v", r, err)
		}
		if _, seen := byDomain[dom]; !seen {
			domainOrder = append(domainOrder, dom)
		}
		byDomain[dom] = append(byDomain[dom], r)
	}
	if len(d.Manifest) > 256 {
		return nil, problemf(http.StatusBadRequest, "malformed", "manifest exceeds 256 entries (§3.2.3)")
	}
	// Dispatch gates on possession (D-135/D-84): every Manifest
	// object verified live in our own store.
	for _, me := range d.Manifest {
		var live int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM objects WHERE urn=? AND state='live'`, me.URN).Scan(&live); err != nil || live == 0 {
			return nil, problemf(http.StatusConflict, "not-available",
				"%s is not verified in your store — send gates on possession (D-135)", me.URN)
		}
	}

	// --- The Signed Medialet (author/1, D-13) ------------------------
	medialet := map[string]any{
		"mlp":     "0.1",
		"id":      s.medialetID(now),
		"author":  addr,
		"created": now.Format(time.RFC3339),
		"body":    map[string]any{"profile": "mlp-html/1", "content": d.BodyContent},
	}
	if d.Subject != "" {
		medialet["subject"] = d.Subject
	}
	if d.InReplyTo != "" {
		medialet["in_reply_to"] = d.InReplyTo
	}
	if len(d.DisplayedTo) > 0 {
		disp := make([]any, len(d.DisplayedTo))
		for i, dt := range d.DisplayedTo {
			e := map[string]any{"addr": dt.Addr}
			if dt.Name != "" {
				e["name"] = dt.Name
			}
			disp[i] = e
		}
		medialet["displayed_to"] = disp
	}
	if len(d.Manifest) > 0 {
		stripInvalidPreviews(d.Manifest) // MEP-002: same rule as ingest
		man := make([]any, len(d.Manifest))
		for i, me := range d.Manifest {
			e := map[string]any{"urn": me.URN, "size": me.Size, "type": me.Type,
				"available_until": me.AvailableUntil}
			if me.Name != "" {
				e["name"] = me.Name
			}
			if me.PreviewOf != "" {
				e["preview_of"] = me.PreviewOf
			}
			man[i] = e
		}
		medialet["manifest"] = man
	}
	authorKID, authorPriv, err := s.authorKey(ctx, now)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "%v", err)
	}
	authorSig, _, err := core.SignDoc(authorPriv, "author/1", authorKID, medialet["created"].(string), medialet)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "author sign: %v", err)
	}
	smv := map[string]any{"medialet": medialet, "signature": authorSig}
	smCanon, err := core.CanonicalizeValue(smv)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "%v", err)
	}
	ca := core.URNMlet(smCanon)
	derived := render.Derive(d.BodyContent, manifestURNs(d.Manifest))

	// --- Materialize the delivery ------------------------------------
	nowS := now.Format(time.RFC3339)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO medialets (content_address, author, medialet_id, created, raw, render_form, derived_text, render_degraded)
		 VALUES (?,?,?,?,?,?,?,?)`,
		ca, addr, medialet["id"], medialet["created"], smCanon,
		nullable(derived.RenderForm), derived.DerivedText, boolTo01(derived.Degraded)); err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO deliveries (mailbox_id, medialet_ca, job_tag, created) VALUES (?,?,?,?)`,
		mailboxID, ca, nullable(d.JobTag), nowS)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	deliveryID, _ := res.LastInsertId()
	for _, me := range d.Manifest {
		// The outbound promise (§10.5): direction out ⇔ promised.
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO refs (mailbox_id, urn, medialet_ca, direction, state, name, size, type, available_until, updated_at)
			 VALUES (?,?,?,'out','promised',?,?,?,?,?)`,
			mailboxID, me.URN, ca, nullable(me.Name), me.Size, me.Type, me.AvailableUntil, nowS); err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
	}
	// First outbound contact makes a correspondent (D-162: the
	// legible tier reason; §7.5/§7.7 key Tier 1 on exactly this).
	for _, r := range d.Recipients {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO correspondents (mailbox_id, addr, first_outbound_at)
			 VALUES (?,?,?)
			 ON CONFLICT(mailbox_id, addr) DO UPDATE SET
			   first_outbound_at = COALESCE(first_outbound_at, excluded.first_outbound_at)`,
			mailboxID, r, nowS); err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
	}
	// The sender's own copy (envelope_in NULL), read, threaded per
	// D-110 — replies from recipients will join this thread.
	threadID, prob := s.threadForSent(ctx, tx, mailboxID, d.InReplyTo, ca, nowS)
	if prob != nil {
		return nil, prob
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO messages (mailbox_id, medialet_ca, envelope_in, thread_id, read, received_at)
		 VALUES (?,?,NULL,?,1,?)`, mailboxID, ca, threadID, nowS); err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	pe := &ParsedEnvelope{Subject: d.Subject, Author: addr, Manifest: d.Manifest, Derived: &derived}
	if prob := updateRollup(ctx, tx, threadID, pe, nowS); prob != nil {
		return nil, prob
	}
	if err := tx.Commit(); err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}

	// --- Per-domain fan-out (§3.4.1) ----------------------------------
	result := &SendResult{DeliveryID: deliveryID, MedialetCA: ca}
	dispatchCreated := s.now().Format(time.RFC3339)
	kid, priv, err := s.signingKey(ctx, s.now())
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "%v", err)
	}
	for _, dom := range domainOrder {
		to := make([]any, len(byDomain[dom]))
		for i, r := range byDomain[dom] {
			to[i] = r
		}
		envelope := map[string]any{
			"mlp":         "0.1",
			"envelope_id": s.envelopeID(s.now()),
			"created":     dispatchCreated,
			"origin":      s.Domain,
			"envelope_to": to,
			"medialet":    smv,
		}
		hopSig, _, err := core.SignDoc(priv, "hop/1", kid, dispatchCreated, envelope)
		if err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "hop sign: %v", err)
		}
		canon, err := core.CanonicalizeValue(map[string]any{"envelope": envelope, "signature": hopSig})
		if err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "%v", err)
		}
		envID := envelope["envelope_id"].(string)
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO dispatches (envelope_id, target_domain, medialet_ca, created, envelope_canonical, hop_sig_value, hop_kid, delivery_id)
			 VALUES (?,?,?,?,?,?,?,?)`,
			envID, dom, ca, dispatchCreated, canon, hopSig["value"], kid, deliveryID); err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		for _, r := range byDomain[dom] {
			s.DB.ExecContext(ctx, `INSERT INTO dispatch_recipients (envelope_id, addr) VALUES (?,?)`, envID, r)
		}
		s.DB.ExecContext(ctx,
			`INSERT INTO timeline_events (delivery_id, at, kind, data_json) VALUES (?,?,?,?)`,
			deliveryID, dispatchCreated, "dispatched",
			fmt.Sprintf(`{"domain":%q,"envelope_id":%q}`, dom, envID))

		outcome := TargetOutcome{Domain: dom, EnvelopeID: envID}
		verdict, err := s.postDispatch(ctx, dom, canon)
		if err != nil {
			outcome.Message = "dispatch-failed"
			s.DB.ExecContext(ctx,
				`INSERT INTO timeline_events (delivery_id, at, kind, data_json) VALUES (?,?,?,?)`,
				deliveryID, s.now().Format(time.RFC3339), "dispatch.failed",
				fmt.Sprintf(`{"domain":%q,"error":%q}`, dom, err.Error()))
		} else {
			if prob := s.RecordDispatchResponse(ctx, verdict); prob != nil {
				outcome.Message = "verdict-invalid"
			} else {
				var msg struct {
					Payload struct {
						Message string `json:"message"`
					} `json:"payload"`
				}
				json.Unmarshal(verdict, &msg)
				outcome.Message = msg.Payload.Message
			}
		}
		result.Targets = append(result.Targets, outcome)
	}
	if len(d.Guests) > 0 {
		guests, prob := s.createGuestLinks(ctx, deliveryID, d.Guests, d.GuestPIN, s.now())
		if prob != nil {
			return nil, prob
		}
		result.Guests = guests
	}
	return result, nil
}

// threadForSent mirrors threadFor for the sender's copy.
func (s *SN) threadForSent(ctx context.Context, tx *sql.Tx, mailboxID int64, inReplyTo, ca, nowS string) (int64, *Problem) {
	if inReplyTo != "" {
		var threadID int64
		err := tx.QueryRowContext(ctx,
			`SELECT thread_id FROM messages WHERE mailbox_id=? AND medialet_ca=?`,
			mailboxID, inReplyTo).Scan(&threadID)
		if err == nil {
			tx.ExecContext(ctx, `UPDATE threads SET last_activity=?, done=0 WHERE id=?`, nowS, threadID)
			return threadID, nil
		}
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO threads (mailbox_id, root_ca, done, flagged, junk, last_activity)
		 VALUES (?,?,0,0,0,?)`, mailboxID, ca, nowS)
	if err != nil {
		return 0, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// postDispatch POSTs the Signed Envelope to the target's SN,
// returning the synchronous verdict body. DispatchEndpoint and
// FulfillClient are the test seams; production discovers via §5.
func (s *SN) postDispatch(ctx context.Context, domain string, envelope []byte) ([]byte, error) {
	endpoint := s.DispatchEndpoint
	if endpoint == nil {
		endpoint = func(ctx context.Context, dom string) (string, error) {
			d, err := s.Resolver.Resolve(ctx, dom)
			if err != nil {
				return "", err
			}
			return d.SN + "/dispatch", nil
		}
	}
	target, err := endpoint(ctx, domain)
	if err != nil {
		return nil, err
	}
	client := s.FulfillClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(envelope))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", ctEnvelope)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mlp/sn: %s answered %d", domain, resp.StatusCode)
	}
	return body, nil
}

// authorKey loads an author-role signing key valid now (D-13: the
// domain signs for its authors).
func (s *SN) authorKey(ctx context.Context, now time.Time) (string, ed25519.PrivateKey, error) {
	return s.keyWithRole(ctx, "author", now)
}

// medialetID mints Medialet identifiers (UUIDv7 RECOMMENDED).
func (s *SN) medialetID(t time.Time) string {
	if s.NewMedialetID != nil {
		return s.NewMedialetID(t)
	}
	return randomUUIDv7(t)
}

// stripInvalidPreviews applies the MEP-002 constraints to a draft's
// Manifest before signing (dangling / chain / self-reference members
// are dropped) — the composer never signs what ingest would ignore.
func stripInvalidPreviews(entries []ManifestEntry) {
	declared := make(map[string]int, len(entries))
	original := make([]string, len(entries))
	for i, me := range entries {
		declared[me.URN] = i
		original[i] = me.PreviewOf
	}
	for i := range entries {
		pv := original[i]
		if pv == "" {
			continue
		}
		target, present := declared[pv]
		if !present || pv == entries[i].URN || original[target] != "" {
			entries[i].PreviewOf = ""
		}
	}
}
