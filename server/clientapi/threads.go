package clientapi

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"medialet.org/mlp/render"
	"net/http"
	"strconv"
	"time"
)

// The S3.2 Inbox surface (D-119–D-132), S4.9 scope: the flat rolled-up
// thread list (inbox and junk views), the full thread, and the triage
// trio (read/done/flag) with D-129 undo tokens. Bundles, sections,
// hoisting, and sweep are the S4.11 triage refinement.

func (s *Server) registerThreadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/threads", s.handler(s.handleThreads))
	mux.HandleFunc("GET /api/v1/threads/{id}", s.handler(s.handleThread))
	mux.HandleFunc("POST /api/v1/threads/{id}/read", s.handler(s.triage("read")))
	mux.HandleFunc("POST /api/v1/threads/{id}/done", s.handler(s.triage("done")))
	mux.HandleFunc("POST /api/v1/threads/{id}/flag", s.handler(s.triage("flag")))
	mux.HandleFunc("POST /api/v1/undo", s.handler(s.handleUndo))
	mux.HandleFunc("POST /api/v1/threads/{id}/release", s.handler(s.handleJunkRelease))
	mux.HandleFunc("POST /api/v1/threads/{id}/block", s.handler(s.handleJunkBlock))
}

func (s *Server) handleThreads(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	junk := 0
	if r.URL.Query().Get("view") == "junk" {
		junk = 1
	}
	cursor := ""
	if v := r.URL.Query().Get("cursor"); v != "" {
		cursor = v
	}
	// Inbox lists undone threads; done is triage, never retention
	// (D-120) — the thread returns on new activity (threadFor resets
	// done). The junk view lists quarantined threads regardless.
	q := `SELECT t.id, t.done, t.flagged, t.last_activity, COALESCE(t.rollup_json, '{}'),
	             (SELECT COUNT(*) FROM messages m WHERE m.thread_id = t.id AND m.read = 0)
	      FROM threads t WHERE t.mailbox_id=? AND t.junk=?`
	args := []any{mailbox, junk}
	if junk == 0 {
		q += ` AND t.done=0`
	}
	if cursor != "" {
		q += ` AND t.last_activity < ?`
		args = append(args, cursor)
	}
	q += ` ORDER BY t.last_activity DESC LIMIT 50`
	rows, err := s.DB.QueryContext(r.Context(), q, args...)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	last := ""
	for rows.Next() {
		var id int64
		var done, flagged, unread int
		var activity, rollupJSON string
		if err := rows.Scan(&id, &done, &flagged, &activity, &rollupJSON, &unread); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		var rollup map[string]any
		json.Unmarshal([]byte(rollupJSON), &rollup)
		chips, prob := s.mediaChips(r, id)
		if prob != nil {
			return prob
		}
		out = append(out, map[string]any{
			"id": id, "done": done == 1, "flagged": flagged == 1,
			"last_activity": activity, "unread": unread,
			"rollup": rollup, "media": chips,
		})
		last = activity
	}
	if err := rows.Err(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	resp := map[string]any{"threads": out}
	if len(out) == 50 {
		resp["next_cursor"] = last
	}
	writeJSON(w, http.StatusOK, resp)
	return nil
}

// mediaChips aggregates the thread's refs by state (the D-132 media
// aggregate: counts per state, thumbnails join with derivatives).
func (s *Server) mediaChips(r *http.Request, threadID int64) (map[string]int64, *problem) {
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT rf.state, COUNT(*) FROM refs rf
		 WHERE (rf.mailbox_id, rf.medialet_ca) IN
		   (SELECT m.mailbox_id, m.medialet_ca FROM messages m WHERE m.thread_id=?)
		 GROUP BY rf.state`, threadID)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var state string
		var n int64
		if err := rows.Scan(&state, &n); err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		out[state] = n
	}
	if err := rows.Err(); err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	return out, nil
}

// handleThread returns the messages with their verbatim bodies and
// per-URN reference states; sanitization is the client's TV-005-
// proven pipeline (server-side render-form derivation arrives with
// its S4.11 consumers).
func (s *Server) handleThread(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	id, prob := s.ownThread(r, mailbox)
	if prob != nil {
		return prob
	}
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT m.id, m.medialet_ca, m.read, m.received_at, md.raw,
		        COALESCE(md.render_form,''), COALESCE(md.derived_text,''), md.render_degraded
		 FROM messages m JOIN medialets md ON md.content_address = m.medialet_ca
		 WHERE m.thread_id=? ORDER BY m.received_at, m.id`, id)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	var msgs []map[string]any
	for rows.Next() {
		var msgID int64
		var ca, receivedAt, renderForm, derivedText string
		var read, degraded int
		var raw []byte
		if err := rows.Scan(&msgID, &ca, &read, &receivedAt, &raw, &renderForm, &derivedText, &degraded); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		var sm struct {
			Medialet map[string]json.RawMessage `json:"medialet"`
		}
		entry := map[string]any{
			"id": msgID, "medialet_ca": ca, "read": read == 1, "received_at": receivedAt,
		}
		if json.Unmarshal(raw, &sm) == nil {
			for _, f := range []string{"author", "subject", "created", "body", "manifest", "displayed_to", "in_reply_to"} {
				if v, ok := sm.Medialet[f]; ok {
					entry[f] = json.RawMessage(v)
				}
			}
		}
		// The server-derived render form (D-94) replaces the verbatim
		// body in the payload; the viewer re-sanitizes it (D-31 dual
		// duty). Pre-0003 rows backfill lazily. Degraded bodies ship
		// the derived text and say so.
		if renderForm == "" && degraded == 0 {
			renderForm, derivedText, degraded = s.backfillRender(r, ca, raw)
		}
		if degraded == 1 {
			entry["body"] = map[string]any{"profile": "mlp-html/1", "content": "", "degraded": true}
			entry["derived_text"] = derivedText
		} else {
			entry["body"] = map[string]any{"profile": "mlp-html/1", "content": renderForm}
		}
		refs, prob := s.messageRefs(r, mailbox, ca)
		if prob != nil {
			return prob
		}
		entry["refs"] = refs
		msgs = append(msgs, entry)
	}
	if err := rows.Err(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "messages": msgs})
	return nil
}

func (s *Server) messageRefs(r *http.Request, mailbox int64, ca string) (map[string]map[string]any, *problem) {
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT urn, state, COALESCE(cause,''), COALESCE(name,''), size, type, available_until
		 FROM refs WHERE mailbox_id=? AND medialet_ca=?`, mailbox, ca)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	out := map[string]map[string]any{}
	for rows.Next() {
		var urn, state, cause, name, typ, until string
		var size int64
		if err := rows.Scan(&urn, &state, &cause, &name, &size, &typ, &until); err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		entry := map[string]any{
			"state": state, "name": name, "size": size, "type": typ, "available_until": until,
		}
		if cause != "" {
			entry["cause"] = cause
		}
		out[urn] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	return out, nil
}

// triage builds the read/done/flag mutation with a D-129 undo token
// (TTL 30 s) journaling the transactional inverse.
func (s *Server) triage(op string) func(http.ResponseWriter, *http.Request, int64) *problem {
	return func(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
		id, prob := s.ownThread(r, mailbox)
		if prob != nil {
			return prob
		}
		var body struct {
			Value *bool `json:"value"` // default true
		}
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
		value := body.Value == nil || *body.Value

		inverse := map[string]any{"op": op, "thread": id}
		switch op {
		case "read":
			// Inverse: the exact set of messages this call flips.
			rows, err := s.DB.QueryContext(r.Context(),
				`SELECT id FROM messages WHERE thread_id=? AND read=?`, id, boolInt(!value))
			if err != nil {
				return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
			}
			var flipped []int64
			for rows.Next() {
				var mid int64
				rows.Scan(&mid)
				flipped = append(flipped, mid)
			}
			rows.Close()
			inverse["messages"], inverse["value"] = flipped, !value
			if _, err := s.DB.ExecContext(r.Context(),
				`UPDATE messages SET read=? WHERE thread_id=?`, boolInt(value), id); err != nil {
				return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
			}
		case "done", "flag":
			col := map[string]string{"done": "done", "flag": "flagged"}[op]
			var old int
			if err := s.DB.QueryRowContext(r.Context(),
				`SELECT `+col+` FROM threads WHERE id=?`, id).Scan(&old); err != nil {
				return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
			}
			inverse["value"] = old == 1
			if _, err := s.DB.ExecContext(r.Context(),
				`UPDATE threads SET `+col+`=? WHERE id=?`, boolInt(value), id); err != nil {
				return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
			}
		}

		token, prob := s.mintUndo(r, mailbox, inverse)
		if prob != nil {
			return prob
		}
		s.Hub.Emit(r.Context(), mailbox, "thread.changed", map[string]any{"id": id})
		writeJSON(w, http.StatusOK, map[string]any{"undo_token": token})
		return nil
	}
}

func (s *Server) mintUndo(r *http.Request, mailbox int64, inverse map[string]any) (string, *problem) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", problemf(http.StatusInternalServerError, "malformed", "entropy: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	b, _ := json.Marshal(inverse)
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO undo_journal (token, mailbox_id, inverse_json, expires) VALUES (?,?,?,?)`,
		token, mailbox, string(b), s.now().Add(30*time.Second).Format(time.RFC3339)); err != nil {
		return "", problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	return token, nil
}

// handleUndo reverses one journaled triage transactionally (D-129).
func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.Token == "" {
		return problemf(http.StatusBadRequest, "malformed", "missing undo token")
	}
	var inverseJSON, expires string
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT inverse_json, expires FROM undo_journal WHERE token=? AND mailbox_id=?`,
		body.Token, mailbox).Scan(&inverseJSON, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return problemf(http.StatusNotFound, "malformed", "unknown undo token")
	}
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if exp, perr := time.Parse(time.RFC3339, expires); perr != nil || s.now().After(exp) {
		return problemf(http.StatusGone, "malformed", "undo window elapsed (D-129)")
	}
	var inverse struct {
		Op       string  `json:"op"`
		Thread   int64   `json:"thread"`
		Value    bool    `json:"value"`
		Messages []int64 `json:"messages"`
	}
	if err := json.Unmarshal([]byte(inverseJSON), &inverse); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "journal: %v", err)
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer tx.Rollback()
	switch inverse.Op {
	case "read":
		for _, mid := range inverse.Messages {
			if _, err := tx.ExecContext(r.Context(),
				`UPDATE messages SET read=? WHERE id=? AND thread_id=?`,
				boolInt(inverse.Value), mid, inverse.Thread); err != nil {
				return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
			}
		}
	case "done", "flag":
		col := map[string]string{"done": "done", "flag": "flagged"}[inverse.Op]
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE threads SET `+col+`=? WHERE id=?`, boolInt(inverse.Value), inverse.Thread); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
	default:
		return problemf(http.StatusInternalServerError, "malformed", "journal op %q", inverse.Op)
	}
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM undo_journal WHERE token=?`, body.Token); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if err := tx.Commit(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	s.Hub.Emit(r.Context(), mailbox, "thread.changed", map[string]any{"id": inverse.Thread})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleJunkRelease frees a quarantined thread to the inbox and
// remembers the trust decision (D-165: releasing is the strongest
// allow signal).
func (s *Server) handleJunkRelease(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	id, prob := s.ownThread(r, mailbox)
	if prob != nil {
		return prob
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE threads SET junk=0 WHERE id=?`, id); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if addr, ok := s.threadAuthor(r, id); ok {
		s.DB.ExecContext(r.Context(),
			`INSERT INTO correspondents (mailbox_id, addr, tier_override) VALUES (?,?,'allow')
			 ON CONFLICT(mailbox_id, addr) DO UPDATE SET tier_override='allow'`, mailbox, addr)
	}
	s.Hub.Emit(r.Context(), mailbox, "thread.changed", map[string]any{"id": id})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleJunkBlock confirms the quarantine: the sender is blocked and
// the thread marked done (it stays in junk for the record).
func (s *Server) handleJunkBlock(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	id, prob := s.ownThread(r, mailbox)
	if prob != nil {
		return prob
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE threads SET done=1 WHERE id=?`, id); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if addr, ok := s.threadAuthor(r, id); ok {
		s.DB.ExecContext(r.Context(),
			`INSERT INTO correspondents (mailbox_id, addr, tier_override) VALUES (?,?,'block')
			 ON CONFLICT(mailbox_id, addr) DO UPDATE SET tier_override='block'`, mailbox, addr)
	}
	s.Hub.Emit(r.Context(), mailbox, "thread.changed", map[string]any{"id": id})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// threadAuthor reads the latest message's author for trust actions.
func (s *Server) threadAuthor(r *http.Request, threadID int64) (string, bool) {
	var raw []byte
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT md.raw FROM messages m JOIN medialets md ON md.content_address=m.medialet_ca
		 WHERE m.thread_id=? ORDER BY m.received_at DESC, m.id DESC LIMIT 1`, threadID).Scan(&raw); err != nil {
		return "", false
	}
	var sm struct {
		Medialet struct {
			Author string `json:"author"`
		} `json:"medialet"`
	}
	if json.Unmarshal(raw, &sm) != nil || sm.Medialet.Author == "" {
		return "", false
	}
	return sm.Medialet.Author, true
}

func (s *Server) ownThread(r *http.Request, mailbox int64) (int64, *problem) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, problemf(http.StatusBadRequest, "malformed", "thread id: %v", err)
	}
	var owner int64
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT mailbox_id FROM threads WHERE id=?`, id).Scan(&owner); err != nil || owner != mailbox {
		return 0, problemf(http.StatusNotFound, "malformed", "no such thread")
	}
	return id, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// backfillRender derives §11 artifacts for rows predating migration
// 0003 (lazy, per read, then persisted).
func (s *Server) backfillRender(r *http.Request, ca string, raw []byte) (renderForm, derivedText string, degraded int) {
	var sm struct {
		Medialet struct {
			Body struct {
				Content string `json:"content"`
			} `json:"body"`
			Manifest []struct {
				URN string `json:"urn"`
			} `json:"manifest"`
		} `json:"medialet"`
	}
	if json.Unmarshal(raw, &sm) != nil {
		return "", "", 1
	}
	urns := make([]string, len(sm.Medialet.Manifest))
	for i, m := range sm.Medialet.Manifest {
		urns[i] = m.URN
	}
	res := render.Derive(sm.Medialet.Body.Content, urns)
	if res.Degraded {
		degraded = 1
	}
	s.DB.ExecContext(r.Context(),
		`UPDATE medialets SET render_form=?, derived_text=?, render_degraded=? WHERE content_address=?`,
		nullIfEmpty(res.RenderForm), res.DerivedText, degraded, ca)
	return res.RenderForm, res.DerivedText, degraded
}
