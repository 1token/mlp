package sn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"medialet.org/mlp/render"
	"net/http"
	"strings"
	"time"
)

// S4.9: materializing the mailbox view at ingest — the per-mailbox
// messages/threads instances (S3.2, D-110/D-119) and the §10.3
// reference rows (D-87) the Inbox and Media surfaces read. This is
// receiver-local derivation; the Signed Medialet stays verbatim
// (D-28/D-94).
//
// Threading (D-110): a message joins the thread of the message its
// in_reply_to names, inheriting that thread's root_ca; otherwise it
// roots a new thread at its own content address. A reply whose
// parent this mailbox never saw roots its own thread — the tree root
// is unknowable without the parent, and a wrong guess is worse than
// a short thread.
//
// Quarantined recipients materialize with threads.junk=1 (the D-165
// quarantine surface reads them); accepted recipients land in the
// inbox. Rejected recipients materialize nothing.

// materialize runs after persistDispatch: one message instance per
// (mailbox, medialet) — re-deliveries of the same Medialet dedup on
// the UNIQUE constraint — plus offered refs per Manifest entry and
// the thread rollup.
func (s *SN) materialize(ctx context.Context, pe *ParsedEnvelope, targets []recipientTarget, granted map[string]bool, now time.Time) *Problem {
	if len(targets) == 0 {
		return nil
	}
	nowS := now.Format(time.RFC3339)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer tx.Rollback()

	var envRow int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM envelopes_in WHERE origin=? AND envelope_id=?`,
		pe.Origin, pe.EnvelopeID).Scan(&envRow); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}

	for _, t := range targets {
		threadID, prob := s.threadFor(ctx, tx, t.mailboxID, pe, t.verdict == "quarantined", nowS)
		if prob != nil {
			return prob
		}
		tag := ""
		if local, _, err := ParseAddress(t.addr); err == nil {
			if plus := strings.IndexByte(local, '+'); plus >= 0 {
				tag = local[plus+1:]
			}
		}
		res, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO messages (mailbox_id, medialet_ca, envelope_in, delivered_to, tag, thread_id, read, received_at)
			 VALUES (?,?,?,?,?,?,0,?)`,
			t.mailboxID, pe.ContentAddress, envRow, t.addr, nullable(tag), threadID, nowS)
		if err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // re-delivery of a known Medialet: one instance (D-110)
		}
		for _, me := range pe.Manifest {
			// MEP-001: the stored offer window is the EFFECTIVE
			// deadline — the latest of the Manifest available_until
			// and every covering source's declared until. §10.3's
			// expiry consumes exactly this value.
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO refs (mailbox_id, urn, medialet_ca, direction, state, name, size, type, available_until, preview_of, updated_at)
				 VALUES (?,?,?,'in','offered',?,?,?,?,?,?)`,
				t.mailboxID, me.URN, pe.ContentAddress,
				nullable(me.Name), me.Size, me.Type,
				effectiveDeadline(me, pe.Sources), nullable(me.PreviewOf), nowS); err != nil {
				return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
			}
			// D-139: an auto-granted entry is already accepted by
			// policy — the reference steps to expected at once
			// (arrival completes it to available with no user
			// action) and carries the EPHEMERAL class: policy
			// admitted it, so GC may take it first (§10.5's class
			// latitude). A deliberate accept or pin outranks the
			// class exactly as the state machine already says.
			if granted[me.URN] {
				if _, err := tx.ExecContext(ctx,
					`UPDATE refs SET state='expected', ephemeral=1, updated_at=? WHERE mailbox_id=? AND urn=? AND medialet_ca=? AND state='offered'`,
					nowS, t.mailboxID, me.URN, pe.ContentAddress); err != nil {
					return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
				}
			}
		}
		if prob := updateRollup(ctx, tx, threadID, pe, nowS); prob != nil {
			return prob
		}
	}
	if err := tx.Commit(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	return nil
}

// threadFor joins the in_reply_to parent's thread or roots a new one.
func (s *SN) threadFor(ctx context.Context, tx *sql.Tx, mailboxID int64, pe *ParsedEnvelope, junk bool, nowS string) (int64, *Problem) {
	if pe.InReplyTo != "" {
		var threadID int64
		err := tx.QueryRowContext(ctx,
			`SELECT thread_id FROM messages WHERE mailbox_id=? AND medialet_ca=?`,
			mailboxID, pe.InReplyTo).Scan(&threadID)
		switch {
		case err == nil:
			if _, err := tx.ExecContext(ctx,
				`UPDATE threads SET last_activity=?, done=0 WHERE id=?`, nowS, threadID); err != nil {
				return 0, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
			}
			return threadID, nil
		case !errors.Is(err, sql.ErrNoRows):
			return 0, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
	}
	junkFlag := 0
	if junk {
		junkFlag = 1
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO threads (mailbox_id, root_ca, done, flagged, junk, last_activity)
		 VALUES (?,?,0,0,?,?)`,
		mailboxID, pe.ContentAddress, junkFlag, nowS)
	if err != nil {
		return 0, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	return id, nil
}

// updateRollup precomputes the D-132 list payload from Medialet
// fields alone: subject, author, media count, latest activity. The
// derived-text snippet joins when the server-side render-form
// derivation lands (its first consumers — D-165 junk payloads and
// the D-21 classifier — arrive with S4.11).
func updateRollup(ctx context.Context, tx *sql.Tx, threadID int64, pe *ParsedEnvelope, nowS string) *Problem {
	entry := map[string]any{
		"subject":     pe.Subject,
		"last_author": pe.Author,
		"media_count": len(pe.Manifest),
		"updated":     nowS,
	}
	if pe.Derived != nil {
		entry["snippet"] = render.Snippet(pe.Derived.DerivedText, 120)
	}
	rollup, _ := json.Marshal(entry)
	if _, err := tx.ExecContext(ctx,
		`UPDATE threads SET rollup_json=?, last_activity=? WHERE id=?`,
		string(rollup), nowS, threadID); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	return nil
}

// manifestURNs projects the Manifest's addresses.
func manifestURNs(entries []ManifestEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.URN
	}
	return out
}

func boolTo01(b bool) int {
	if b {
		return 1
	}
	return 0
}

// effectiveDeadline is the MEP-001 rule: the latest of the Manifest
// available_until and the `until` of every listed source covering the
// URN. RFC 3339 UTC strings compare lexicographically.
func effectiveDeadline(me ManifestEntry, sources []SourceEntry) string {
	deadline := me.AvailableUntil
	for _, src := range sources {
		if src.Until != "" && src.Covers(me.URN) && src.Until > deadline {
			deadline = src.Until
		}
	}
	return deadline
}

// ExpireOffers fires the §10.3 offered→unavailable(expired-remote)
// transition for every offer whose effective deadline has passed —
// the sweep the Client API runs lazily on list reads.
func (s *SN) ExpireOffers(ctx context.Context, now time.Time) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE refs SET state='unavailable', cause='expired-remote', updated_at=?
		 WHERE state='offered' AND available_until < ?`,
		now.Format(time.RFC3339), now.Format(time.RFC3339))
	return err
}
