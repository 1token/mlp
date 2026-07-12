package clientapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"medialet.org/mlp/core"
)

// The Media surface (S3.7, D-156–D-160): the reference-centric
// library over the §10.3 machine — one card per URN aggregating its
// per-delivery refs, pin/unpin, owner delete (D-88: the owner may
// destroy what GC may not), and object/raw serving for the viewer.

func (s *Server) registerMediaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/media", s.handler(s.handleMediaList))
	mux.HandleFunc("POST /api/v1/o/{urn}/pin", s.handler(s.pinOp(true)))
	mux.HandleFunc("POST /api/v1/o/{urn}/unpin", s.handler(s.pinOp(false)))
	mux.HandleFunc("DELETE /api/v1/o/{urn}", s.handler(s.handleObjectDelete))
	mux.HandleFunc("GET /api/v1/o/{urn}", s.handler(s.handleObjectGet))
	mux.HandleFunc("GET /api/v1/m/{ca}", s.handler(s.handleMedialetRaw))
	mux.HandleFunc("GET /api/v1/correspondents", s.handler(s.handleCorrespondents))
	mux.HandleFunc("PUT /api/v1/correspondents/{addr}", s.handler(s.handleCorrespondentPut))
}

// handleMediaList groups the mailbox's refs by URN (D-156: one row
// per delivery, one card per object).
func (s *Server) handleMediaList(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	s.SN.ExpireOffers(r.Context(), s.now())
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT rf.urn, rf.state, COALESCE(rf.cause,''), COALESCE(rf.name,''), rf.size, rf.type,
		        rf.available_until, rf.direction, COALESCE(rf.preview_of,''),
		        COALESCE((SELECT o.state FROM objects o WHERE o.urn = rf.urn), '')
		 FROM refs rf WHERE rf.mailbox_id=? ORDER BY rf.urn, rf.updated_at`, mailbox)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	type card struct {
		URN        string         `json:"urn"`
		Name       string         `json:"name"`
		Size       int64          `json:"size"`
		Type       string         `json:"type"`
		Held       bool           `json:"held"`
		Pinned     bool           `json:"pinned"`
		PreviewOf  string         `json:"preview_of,omitempty"` // MEP-002: fold hint
		States     map[string]int `json:"states"`
		Deliveries int            `json:"deliveries"`
		Windows    []string       `json:"windows"`
	}
	byURN := map[string]*card{}
	var order []string
	for rows.Next() {
		var urn, state, cause, name, typ, until, direction, previewOf, objState string
		var size int64
		if err := rows.Scan(&urn, &state, &cause, &name, &size, &typ, &until, &direction, &previewOf, &objState); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		c, ok := byURN[urn]
		if !ok {
			c = &card{URN: urn, Name: name, Size: size, Type: typ, States: map[string]int{}}
			byURN[urn] = c
			order = append(order, urn)
		}
		if c.Name == "" {
			c.Name = name
		}
		c.States[state]++
		c.Deliveries++
		c.Held = objState == "live"
		c.Pinned = c.Pinned || state == "pinned"
		if previewOf != "" {
			c.PreviewOf = previewOf
		}
		c.Windows = append(c.Windows, until)
	}
	if err := rows.Err(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	out := make([]*card, 0, len(order))
	for _, u := range order {
		out = append(out, byURN[u])
	}
	writeJSON(w, http.StatusOK, map[string]any{"media": out})
	return nil
}

// pinOp flips available↔pinned under the §10.3 trigger (D-88: pin
// protects from GC, never from the owner).
func (s *Server) pinOp(pin bool) func(http.ResponseWriter, *http.Request, int64) *problem {
	from, to := "available", "pinned"
	if !pin {
		from, to = "pinned", "available"
	}
	return func(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
		urn := r.PathValue("urn")
		res, err := s.DB.ExecContext(r.Context(),
			`UPDATE refs SET state=?, updated_at=? WHERE mailbox_id=? AND urn=? AND state=?`,
			to, s.now().Format(time.RFC3339), mailbox, urn, from)
		if err != nil {
			return problemf(http.StatusConflict, "invalid-transition", "%v", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return problemf(http.StatusConflict, "invalid-transition",
				"no %s reference for %s (§10.3)", from, urn)
		}
		s.Hub.Emit(r.Context(), mailbox, "media.changed", map[string]any{"urn": urn})
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

// handleObjectDelete is the owner's delete (D-88): every reference
// (pinned included) goes unavailable(deleted); the object row and
// bytes leave the store.
func (s *Server) handleObjectDelete(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	urn := r.PathValue("urn")
	if _, err := core.ParseURNMlet(urn); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE refs SET state='unavailable', cause='deleted', updated_at=?
		 WHERE mailbox_id=? AND urn=? AND state IN ('available','pinned')`,
		s.now().Format(time.RFC3339), mailbox, urn); err != nil {
		return problemf(http.StatusConflict, "invalid-transition", "%v", err)
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM objects WHERE urn=?`, urn); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if err := tx.Commit(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if s.BS != nil {
		os.Remove(s.BS.ObjectPath(urn))
	}
	s.Hub.Emit(r.Context(), mailbox, "media.changed", map[string]any{"urn": urn})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleObjectGet serves live object bytes to the session (the
// viewer's resolveUrn target). Range serving is a QoI note.
func (s *Server) handleObjectGet(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	urn := r.PathValue("urn")
	if _, err := core.ParseURNMlet(urn); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	var state string
	err := s.DB.QueryRowContext(r.Context(), `SELECT state FROM objects WHERE urn=?`, urn).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) || state != "live" {
		return problemf(http.StatusNotFound, "not-available", "object not held live")
	}
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	typ := "application/octet-stream"
	s.DB.QueryRowContext(r.Context(),
		`SELECT type FROM refs WHERE mailbox_id=? AND urn=? LIMIT 1`, mailbox, urn).Scan(&typ)
	f, ferr := os.Open(s.BS.ObjectPath(urn))
	if ferr != nil {
		return problemf(http.StatusNotFound, "not-available", "object bytes missing")
	}
	defer f.Close()
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable") // content-addressed
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox") // never a document context
	w.WriteHeader(http.StatusOK)
	copyFile(w, f)
	return nil
}

// handleMedialetRaw serves the verbatim Signed Medialet (D-28) — the
// client's verification and fidelity source.
func (s *Server) handleMedialetRaw(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	ca := r.PathValue("ca")
	var held int
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM messages WHERE mailbox_id=? AND medialet_ca=?`, mailbox, ca).Scan(&held); err != nil || held == 0 {
		return problemf(http.StatusNotFound, "malformed", "no such medialet in this mailbox")
	}
	var raw []byte
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT raw FROM medialets WHERE content_address=?`, ca).Scan(&raw); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	w.Header().Set("Content-Type", "application/mlp-medialet+json")
	w.WriteHeader(http.StatusOK)
	w.Write(raw)
	return nil
}

// --- correspondents (S3.8, the trust ledger feeding §7.7) --------------

func (s *Server) handleCorrespondents(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT addr, COALESCE(tier_override,''), COALESCE(first_outbound_at,'')
		 FROM correspondents WHERE mailbox_id=? ORDER BY addr`, mailbox)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var addr, override, first string
		if err := rows.Scan(&addr, &override, &first); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		e := map[string]any{"addr": addr}
		if override != "" {
			e["tier_override"] = override
		}
		if first != "" {
			e["first_outbound_at"] = first
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"correspondents": out})
	return nil
}

func (s *Server) handleCorrespondentPut(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	addr := r.PathValue("addr")
	var body struct {
		TierOverride string `json:"tier_override"` // allow | block | ""
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	if body.TierOverride != "" && body.TierOverride != "allow" && body.TierOverride != "block" {
		return problemf(http.StatusBadRequest, "malformed", "tier_override must be allow, block, or empty")
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO correspondents (mailbox_id, addr, tier_override) VALUES (?,?,?)
		 ON CONFLICT(mailbox_id, addr) DO UPDATE SET tier_override=excluded.tier_override`,
		mailbox, addr, nullIfEmpty(body.TierOverride)); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func copyFile(w http.ResponseWriter, f *os.File) {
	buf := make([]byte, 64<<10)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
