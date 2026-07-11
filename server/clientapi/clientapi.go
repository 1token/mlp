// Package clientapi implements the intra-domain client surface of
// the Stage 3 companion draft (D-170/D-171): the /api/v1 conventions
// — D-43 dialect JSON, problem+json with client-side codes, HttpOnly
// session cookie with the X-MLP-Client CSRF posture, Idempotency-Key
// journaling — the SSE live feed with Last-Event-ID resume (D-132),
// and the endpoints whose server machinery is green as of S4.6:
// accept (defer→grant / delegation trigger, D-141), deliveries and
// the D-149 timeline, have-check, quota, settings, sessions.
//
// The interface is deployment freedom in the core spec (D-79/D-86);
// nothing here is required for federation conformance.
package clientapi

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zeebo/blake3"
	"medialet.org/mlp/bs"
	"medialet.org/mlp/sn"
)

// Server serves /api/v1 for one domain's mailboxes, wrapping the SN
// (and through it the store and resolver).
type Server struct {
	DB  *sql.DB
	SN  *sn.SN
	BS  *bs.BS // the intra-domain upload door (D-135, one code path)
	Hub *Hub
	Now func() time.Time

	// PostVerdict delivers an upgrade snapshot to the origin's
	// /verdict endpoint (§7.6). The default discovers the sn URL
	// (§5); tests override.
	PostVerdict func(ctx context.Context, origin string, doc []byte) error
	// PasswordIterations tunes the PBKDF2 cost; 0 means the
	// production default. Tests lower it.
	PasswordIterations int
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// problem mirrors the federation surface's RFC 9457 posture with the
// client-side code additions the draft names (auth-required,
// csrf-required, quota-exceeded, draft-conflict).
type problem struct {
	Status int
	Code   string
	Detail string
}

func problemf(status int, code, format string, a ...any) *problem {
	return &problem{Status: status, Code: code, Detail: fmt.Sprintf(format, a...)}
}

func writeProblem(w http.ResponseWriter, p *problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	json.NewEncoder(w).Encode(map[string]any{
		"type": "urn:mlp:err:" + p.Code, "title": p.Code,
		"status": p.Status, "detail": p.Detail,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

const sessionCookie = "mlp_session"

func hashToken(token string) string {
	sum := blake3.Sum256([]byte(token)) // at-rest hashing per D-192
	return hex.EncodeToString(sum[:])
}

// session resolves the cookie to a mailbox id, touching last_seen.
func (s *Server) session(r *http.Request) (mailboxID int64, sessionHash string, prob *problem) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return 0, "", problemf(http.StatusUnauthorized, "auth-required", "sign in to continue")
	}
	h := hashToken(c.Value)
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT mailbox_id FROM sessions WHERE session_hash=?`, h).Scan(&mailboxID); err != nil {
		return 0, "", problemf(http.StatusUnauthorized, "auth-required", "session expired or revoked")
	}
	s.DB.ExecContext(r.Context(), `UPDATE sessions SET last_seen=? WHERE session_hash=?`,
		s.now().Format(time.RFC3339), h)
	return mailboxID, h, nil
}

// handler adapts an authenticated endpoint: session first, then the
// CSRF header on mutations, then Idempotency-Key journaling (D-169).
func (s *Server) handler(fn func(w http.ResponseWriter, r *http.Request, mailbox int64) *problem) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mailbox, _, prob := s.session(r)
		if prob != nil {
			writeProblem(w, prob)
			return
		}
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead
		if mutation && r.Header.Get("X-MLP-Client") == "" {
			writeProblem(w, problemf(http.StatusForbidden, "csrf-required",
				"mutations require the X-MLP-Client header (D-170)"))
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if mutation && key != "" {
			var stored string
			err := s.DB.QueryRowContext(r.Context(),
				`SELECT response_json FROM idempotency WHERE mailbox_id=? AND key=?`, mailbox, key).Scan(&stored)
			if err == nil {
				var rec journaledResponse
				if json.Unmarshal([]byte(stored), &rec) == nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(rec.Status)
					io.WriteString(w, rec.Body)
					return
				}
			}
			rec := &recorder{ResponseWriter: w, status: http.StatusOK}
			if prob := fn(rec, r, mailbox); prob != nil {
				writeProblem(w, prob) // failures are not journaled: retries may succeed
				return
			}
			b, _ := json.Marshal(journaledResponse{Status: rec.status, Body: rec.body.String()})
			s.DB.ExecContext(r.Context(),
				`INSERT OR IGNORE INTO idempotency (mailbox_id, key, response_json, created) VALUES (?,?,?,?)`,
				mailbox, key, string(b), s.now().Format(time.RFC3339))
			return
		}
		if prob := fn(w, r, mailbox); prob != nil {
			writeProblem(w, prob)
		}
	}
}

type journaledResponse struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// recorder tees the response for the idempotency journal.
type recorder struct {
	http.ResponseWriter
	status int
	body   strings.Builder
	wrote  bool
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.wrote = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

// Handler builds the /api/v1 mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/password", s.handlePasswordLogin)
	mux.HandleFunc("GET /api/v1/sessions", s.handler(s.handleSessionsList))
	mux.HandleFunc("DELETE /api/v1/sessions", s.handler(s.handleSessionsRevokeAll))
	mux.HandleFunc("DELETE /api/v1/sessions/{hash}", s.handler(s.handleSessionRevoke))
	mux.HandleFunc("GET /api/v1/events", s.handleEvents)
	mux.HandleFunc("POST /api/v1/o/{urn}/accept", s.handler(s.handleAccept))
	mux.HandleFunc("GET /api/v1/objects/have", s.handler(s.handleHave))
	mux.HandleFunc("GET /api/v1/deliveries", s.handler(s.handleDeliveries))
	mux.HandleFunc("GET /api/v1/deliveries/{id}", s.handler(s.handleDelivery))
	mux.HandleFunc("GET /api/v1/deliveries/{id}/timeline", s.handler(s.handleTimeline))
	mux.HandleFunc("GET /api/v1/quota", s.handler(s.handleQuota))
	mux.HandleFunc("GET /api/v1/settings", s.handler(s.handleSettingsGet))
	mux.HandleFunc("PATCH /api/v1/settings", s.handler(s.handleSettingsPatch))
	s.registerThreadRoutes(mux)
	s.registerComposeRoutes(mux)
	return mux
}
