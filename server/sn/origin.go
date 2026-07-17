package sn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

// The origin side of negotiation: recording the synchronous dispatch
// reply, and processing later verdict *updates* POSTed to
// {sn}/verdict (§7.6) — idempotent snapshots superseding by `created`
// order, with per-URN transitions confined to the §7.6 table.

// RecordDispatchResponse verifies and records the signed verdict
// received as the synchronous /dispatch reply for one of our own
// dispatches. It establishes the per-URN baseline the §7.6 transition
// table is evaluated against.
func (s *SN) RecordDispatchResponse(ctx context.Context, raw []byte) *Problem {
	now := s.now()
	pv, prob := s.ParseVerdict(ctx, raw, now)
	if prob != nil {
		return prob
	}
	if prob := s.matchDispatch(ctx, pv); prob != nil {
		return prob
	}
	return s.applySnapshot(ctx, pv, false)
}

// ProcessVerdictUpdate handles POST {sn}/verdict (§7.6). Valid
// updates answer 204 (a nil Problem).
func (s *SN) ProcessVerdictUpdate(ctx context.Context, raw []byte) *Problem {
	now := s.now()
	pv, prob := s.ParseVerdict(ctx, raw, now)
	if prob != nil {
		return prob
	}
	if prob := s.matchDispatch(ctx, pv); prob != nil {
		return prob
	}
	return s.applySnapshot(ctx, pv, true)
}

// matchDispatch enforces §7.6 addressing: envelope_origin is this
// domain and (envelope_id) matches an outstanding dispatch whose
// target is the verdict's issuer — else 404 unknown-envelope.
func (s *SN) matchDispatch(ctx context.Context, pv *ParsedVerdict) *Problem {
	if pv.EnvOrigin != s.Domain {
		return problemf(http.StatusNotFound, "unknown-envelope",
			"envelope_origin %q is not this domain (§7.6)", pv.EnvOrigin)
	}
	var target string
	err := s.DB.QueryRowContext(ctx,
		`SELECT target_domain FROM dispatches WHERE envelope_id=?`, pv.EnvelopeID).Scan(&target)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return problemf(http.StatusNotFound, "unknown-envelope",
			"no outstanding dispatch %s (§7.6)", pv.EnvelopeID)
	case err != nil:
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if target != pv.Issuer {
		return problemf(http.StatusNotFound, "unknown-envelope",
			"dispatch %s targets %s, not issuer %s", pv.EnvelopeID, target, pv.Issuer)
	}
	return nil
}

// applySnapshot stores an inbound snapshot and, when it supersedes
// (latest `created` wins, ties by verdict_id — §7.6), enforces the
// transition table against the current per-URN state and materializes
// granted Reservations into reservations_out.
//
// Transition semantics (the table plus snapshot logic): entries whose
// state is unchanged are no-ops — repeating `have`/`deny` does not
// "alter" a terminal state, and idempotent snapshots necessarily
// restate unchanged entries. State *changes* must be in the table:
// defer→grant, defer→deny, grant→deny; grant→grant is an explicit
// refresh carrying a fresh Reservation. Everything else is
// invalid-transition and the whole update is discarded.
func (s *SN) applySnapshot(ctx context.Context, pv *ParsedVerdict, isUpdate bool) *Problem {
	// Idempotency: an already-stored verdict_id is acknowledged
	// without reprocessing.
	var seen int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM verdicts WHERE direction='in' AND issuer=? AND verdict_id=?`,
		pv.Issuer, pv.VerdictID).Scan(&seen); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if seen > 0 {
		return nil
	}

	current, latestCreated, latestID, prob := s.currentMediaState(ctx, pv.Issuer, pv.EnvelopeID)
	if prob != nil {
		return prob
	}
	supersedes := latestCreated == "" ||
		pv.Created > latestCreated ||
		(pv.Created == latestCreated && pv.VerdictID > latestID)

	var grants []MediaOutcome
	if supersedes && current != nil {
		for _, m := range pv.Media {
			old, known := current[m.URN]
			if !known || old == m.Verdict {
				// New URN state, or an unchanged restatement.
				if m.Verdict == "grant" && (!known || old != "grant") {
					grants = append(grants, m)
				}
				continue
			}
			legal := (old == "defer" && (m.Verdict == "grant" || m.Verdict == "deny")) ||
				(old == "grant" && m.Verdict == "deny")
			if !legal {
				return problemf(http.StatusConflict, "invalid-transition",
					"%s: %s → %s is outside the §7.6 table", m.URN, old, m.Verdict)
			}
			if m.Verdict == "grant" {
				grants = append(grants, m)
			}
		}
		// grant→grant refresh: an unchanged grant with a Reservation
		// present is a fresh invitation (§7.6).
		for _, m := range pv.Media {
			if old, known := current[m.URN]; known && old == "grant" && m.Verdict == "grant" && m.Reservation != nil {
				grants = appendUnique(grants, m)
			}
		}
	} else if supersedes {
		// No baseline yet (update arrived before the response was
		// recorded): the snapshot is the baseline.
		for _, m := range pv.Media {
			if m.Verdict == "grant" {
				grants = append(grants, m)
			}
		}
	}
	// A stale snapshot (supersedes == false) is stored for the
	// history (D-149) but alters no state and mints nothing.

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer tx.Rollback()
	if prob := storeVerdict(ctx, tx, "in", pv.Payload, pv.Raw); prob != nil {
		return prob
	}
	if supersedes {
		// The push queue carries the RECEIVING domain's identity so
		// the pusher can consult its §5.2 capability advertisement.
		var targetDomain string
		tx.QueryRowContext(ctx,
			`SELECT target_domain FROM dispatches WHERE envelope_id=?`, pv.EnvelopeID).Scan(&targetDomain)
		for _, g := range grants {
			r := g.Reservation
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO reservations_out (urn, target_url, token, max_size, expires, envelope_id, state, target_domain)
				 VALUES (?,?,?,?,?,?,'pending',?)`,
				r.URN, r.TargetURL, r.Token, r.MaxSize, r.Expires, pv.EnvelopeID, targetDomain); err != nil {
				return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	s.recordTimeline(ctx, pv, isUpdate)
	return nil
}

// recordTimeline appends the D-149 protocol fact when the dispatch
// is linked to a delivery (best-effort; the snapshot itself is
// already durable in verdicts).
func (s *SN) recordTimeline(ctx context.Context, pv *ParsedVerdict, isUpdate bool) {
	var deliveryID int64
	if err := s.DB.QueryRowContext(ctx,
		`SELECT delivery_id FROM dispatches WHERE envelope_id=? AND delivery_id IS NOT NULL`,
		pv.EnvelopeID).Scan(&deliveryID); err != nil {
		return
	}
	kind := "verdict.received"
	if isUpdate {
		kind = "verdict.updated"
	}
	data, _ := json.Marshal(map[string]any{
		"issuer": pv.Issuer, "verdict_id": pv.VerdictID, "message": pv.Message,
	})
	s.DB.ExecContext(ctx,
		`INSERT INTO timeline_events (delivery_id, at, kind, data_json) VALUES (?,?,?,?)`,
		deliveryID, pv.Created, kind, string(data))
}

func appendUnique(list []MediaOutcome, m MediaOutcome) []MediaOutcome {
	for _, x := range list {
		if x.URN == m.URN {
			return list
		}
	}
	return append(list, m)
}

// currentMediaState reads the per-URN state established by the latest
// stored snapshot for (issuer, envelopeID). A nil map with empty
// created means no baseline exists yet.
func (s *SN) currentMediaState(ctx context.Context, issuer, envelopeID string) (map[string]string, string, string, *Problem) {
	var row int64
	var created, verdictID string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, created, verdict_id FROM verdicts
		 WHERE direction='in' AND issuer=? AND envelope_id=?
		 ORDER BY created DESC, verdict_id DESC LIMIT 1`, issuer, envelopeID).Scan(&row, &created, &verdictID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, "", "", nil
	case err != nil:
		return nil, "", "", problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT urn, verdict FROM verdict_media WHERE verdict_row=?`, row)
	if err != nil {
		return nil, "", "", problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	state := map[string]string{}
	for rows.Next() {
		var urn, v string
		if err := rows.Scan(&urn, &v); err != nil {
			return nil, "", "", problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		state[urn] = v
	}
	if err := rows.Err(); err != nil {
		return nil, "", "", problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	return state, created, verdictID, nil
}
