package clientapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"medialet.org/mlp/core"
)

// handleAccept is POST /o/{urn}/accept (S3.4/D-141): the recipient's
// accept action on deferred Media. Direct receipt → the local
// defer→grant upgrade (§7.6), with the snapshot POSTed to the
// origin's /verdict; forwarded receipt → the §9.3 delegation flow.
//
// S4.7 posture: the acting mailbox is not yet checked against the
// envelope's recipients (the messages materialization that would
// authorize this lands with the Inbox substage); single-user
// prototype semantics until then.
func (s *Server) handleAccept(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	urn := r.PathValue("urn")
	if _, err := core.ParseURNMlet(urn); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	// The most recent outbound snapshot holding this URN at defer.
	var origin, envelopeID string
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT v.envelope_origin, v.envelope_id
		 FROM verdicts v JOIN verdict_media vm ON vm.verdict_row = v.id
		 WHERE v.direction='out' AND vm.urn=? AND vm.verdict='defer'
		 ORDER BY v.created DESC, v.verdict_id DESC LIMIT 1`, urn).Scan(&origin, &envelopeID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return problemf(http.StatusNotFound, "malformed", "no deferred delivery holds %s", urn)
	case err != nil:
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}

	var hops sql.NullString
	var medialetCA string
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT hops_json, medialet_ca FROM envelopes_in WHERE origin=? AND envelope_id=?`,
		origin, envelopeID).Scan(&hops, &medialetCA); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	// The acting mailbox must actually hold this delivery (S4.9,
	// closing the D-213 single-user note): a messages row is the
	// materialized proof of recipiency.
	var held int
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM messages WHERE mailbox_id=? AND medialet_ca=?`,
		mailbox, medialetCA).Scan(&held); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if held == 0 {
		return problemf(http.StatusNotFound, "malformed", "no delivery of %s to this mailbox", urn)
	}
	// Accepting flips the refs state machine: offered -> expected
	// (§10.3; the trigger enforces legality).
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE refs SET state='expected', updated_at=? WHERE mailbox_id=? AND urn=? AND medialet_ca=? AND state='offered'`,
		s.now().Format(time.RFC3339), mailbox, urn, medialetCA); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}

	if hops.Valid && hops.String != "" && hops.String != "null" {
		// Forwarded: the enveloping domain holds nothing (§9.3).
		outcomes, err := s.SN.RequestFulfillment(r.Context(), origin, envelopeID, []string{urn})
		if err != nil {
			return problemf(http.StatusBadGateway, "not-available", "%v", err)
		}
		s.Hub.Emit(r.Context(), mailbox, "media.accepted",
			map[string]any{"urn": urn, "mode": "delegated"})
		writeJSON(w, http.StatusOK, map[string]any{"mode": "delegated", "outcomes": outcomes})
		return nil
	}

	// Direct: issue the upgrade snapshot and deliver it (§7.6).
	doc, err := s.SN.RecipientAccept(r.Context(), origin, envelopeID)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "upgrade: %v", err)
	}
	if err := s.postVerdict(r.Context(), origin, doc); err != nil {
		return problemf(http.StatusBadGateway, "discovery-failed", "verdict delivery: %v", err)
	}
	s.Hub.Emit(r.Context(), mailbox, "media.accepted",
		map[string]any{"urn": urn, "mode": "upgraded"})
	writeJSON(w, http.StatusOK, map[string]any{"mode": "upgraded"})
	return nil
}

func (s *Server) postVerdict(ctx context.Context, origin string, doc []byte) error {
	if s.PostVerdict != nil {
		return s.PostVerdict(ctx, origin, doc)
	}
	d, err := s.SN.Resolver.Resolve(ctx, origin)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.SN+"/verdict", bytes.NewReader(doc))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/mlp-verdict+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("origin answered %d", resp.StatusCode)
	}
	return nil
}

// handleHave is GET /objects/have?urn= — the compose-time
// attach-by-reference check (S3.3).
func (s *Server) handleHave(w http.ResponseWriter, r *http.Request, _ int64) *problem {
	urn := r.URL.Query().Get("urn")
	if _, err := core.ParseURNMlet(urn); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	var state string
	err := s.DB.QueryRowContext(r.Context(), `SELECT state FROM objects WHERE urn=?`, urn).Scan(&state)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeJSON(w, http.StatusOK, map[string]any{"have": false})
	case err != nil:
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"have": state == "live", "state": state})
	}
	return nil
}

// handleDeliveries is GET /deliveries — the sender's list with
// headline states (S3.5/D-145, headline simplified to the latest
// verdict message per target until the full derivation lands).
func (s *Server) handleDeliveries(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	cursor := int64(0)
	if v := r.URL.Query().Get("cursor"); v != "" {
		cursor, _ = strconv.ParseInt(v, 10, 64)
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, medialet_ca, job_tag, created FROM deliveries
		 WHERE mailbox_id=? AND id>? ORDER BY id LIMIT 50`, mailbox, cursor)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	var lastID int64
	for rows.Next() {
		var id int64
		var ca, created string
		var jobTag sql.NullString
		if err := rows.Scan(&id, &ca, &jobTag, &created); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		targets, prob := s.deliveryTargets(r.Context(), id)
		if prob != nil {
			return prob
		}
		out = append(out, map[string]any{
			"id": id, "medialet_ca": ca, "job_tag": jobTag.String,
			"created": created, "targets": targets,
		})
		lastID = id
	}
	if err := rows.Err(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	resp := map[string]any{"deliveries": out}
	if len(out) == 50 {
		resp["next_cursor"] = strconv.FormatInt(lastID, 10)
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

func (s *Server) deliveryTargets(ctx context.Context, deliveryID int64) ([]map[string]any, *problem) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT d.envelope_id, d.target_domain,
		        COALESCE((SELECT v.message FROM verdicts v
		                  WHERE v.direction='in' AND v.envelope_id=d.envelope_id AND v.issuer=d.target_domain
		                  ORDER BY v.created DESC, v.verdict_id DESC LIMIT 1), 'dispatched')
		 FROM dispatches d WHERE d.delivery_id=? ORDER BY d.created`, deliveryID)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var envID, domain, headline string
		if err := rows.Scan(&envID, &domain, &headline); err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		out = append(out, map[string]any{
			"envelope_id": envID, "domain": domain, "headline": headline,
		})
	}
	return out, nil
}

// handleDelivery is GET /deliveries/{id}: the two domain-grouped
// matrices (D-146), read from the latest inbound snapshot per target.
func (s *Server) handleDelivery(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	id, prob := s.ownDelivery(r, mailbox)
	if prob != nil {
		return prob
	}
	targets, prob := s.deliveryTargets(r.Context(), id)
	if prob != nil {
		return prob
	}
	for _, t := range targets {
		var doc []byte
		err := s.DB.QueryRowContext(r.Context(),
			`SELECT doc FROM verdicts WHERE direction='in' AND envelope_id=? AND issuer=?
			 ORDER BY created DESC, verdict_id DESC LIMIT 1`,
			t["envelope_id"], t["domain"]).Scan(&doc)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		var parsed struct {
			Payload struct {
				Recipients []map[string]any `json:"recipients"`
				Media      []map[string]any `json:"media"`
			} `json:"payload"`
		}
		if json.Unmarshal(doc, &parsed) == nil {
			t["recipients"] = parsed.Payload.Recipients
			t["media"] = parsed.Payload.Media
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "targets": targets})
	return nil
}

// handleTimeline is GET /deliveries/{id}/timeline — the D-149
// chronological protocol-fact feed.
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	id, prob := s.ownDelivery(r, mailbox)
	if prob != nil {
		return prob
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT at, kind, data_json FROM timeline_events WHERE delivery_id=? ORDER BY at, id`, id)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var at, kind, dataJSON string
		if err := rows.Scan(&at, &kind, &dataJSON); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		var data any
		json.Unmarshal([]byte(dataJSON), &data)
		out = append(out, map[string]any{"at": at, "kind": kind, "data": data})
	}
	if err := rows.Err(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"timeline": out})
	return nil
}

// ownDelivery parses {id} and checks it belongs to the mailbox.
func (s *Server) ownDelivery(r *http.Request, mailbox int64) (int64, *problem) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, problemf(http.StatusBadRequest, "malformed", "delivery id: %v", err)
	}
	var owner int64
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT mailbox_id FROM deliveries WHERE id=?`, id).Scan(&owner); err != nil || owner != mailbox {
		return 0, problemf(http.StatusNotFound, "malformed", "no such delivery")
	}
	return id, nil
}

// handleQuota is GET /quota — per-store meters (D-159, segmentation
// simplified to live totals until refs-based accounting lands).
func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request, _ int64) *problem {
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT st.id, st.name, COALESCE(SUM(CASE WHEN o.state='live' THEN o.size END), 0)
		 FROM stores st LEFT JOIN objects o ON o.store_id = st.id GROUP BY st.id ORDER BY st.id`)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, used int64
		var name string
		if err := rows.Scan(&id, &name, &used); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		out = append(out, map[string]any{"store_id": id, "name": name, "used_bytes": used})
	}
	if err := rows.Err(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"stores": out})
	return nil
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT key, value_json FROM settings WHERE mailbox_id=?`, mailbox)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		var parsed any
		if json.Unmarshal([]byte(v), &parsed) == nil {
			out[k] = parsed
		}
	}
	if err := rows.Err(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func (s *Server) handleSettingsPatch(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	var body map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	for k, v := range body {
		b, err := json.Marshal(v)
		if err != nil {
			return problemf(http.StatusBadRequest, "malformed", "%s: %v", k, err)
		}
		if _, err := s.DB.ExecContext(r.Context(),
			`INSERT INTO settings (mailbox_id, key, value_json) VALUES (?,?,?)
			 ON CONFLICT(mailbox_id, key) DO UPDATE SET value_json=excluded.value_json`,
			mailbox, k, string(b)); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
	}
	return s.handleSettingsGet(w, r, mailbox)
}
