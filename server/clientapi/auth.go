package clientapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"medialet.org/mlp/sn"
)

// Password fallback (S3.8/D-161). The primary ceremony is WebAuthn,
// which lands with the identity substage (S4.11); the fallback is
// implemented now because the API backbone needs a real session
// mint. Hashing is PBKDF2-HMAC-SHA256 (RFC 8018) from the stdlib —
// no dependency — verified against the RFC 7914 §11 test vector;
// production would prefer argon2id once a dependency is acceptable.
const defaultPBKDF2Iterations = 210_000 // OWASP guidance for SHA-256

// pbkdf2SHA256 implements RFC 8018 §5.2 with HMAC-SHA256 as the PRF.
func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hLen := prf.Size()
	blocks := (keyLen + hLen - 1) / hLen
	dk := make([]byte, 0, blocks*hLen)
	u := make([]byte, hLen)
	for block := 1; block <= blocks; block++ {
		prf.Reset()
		prf.Write(salt)
		var idx [4]byte
		binary.BigEndian.PutUint32(idx[:], uint32(block))
		prf.Write(idx[:])
		u = prf.Sum(u[:0])
		t := make([]byte, hLen)
		copy(t, u)
		for i := 2; i <= iterations; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range t {
				t[j] ^= u[j]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

// HashPassword produces the stored form:
// pbkdf2-sha256$<iterations>$<salt b64url>$<key b64url>.
func HashPassword(password string, iterations int) (string, error) {
	if iterations <= 0 {
		iterations = defaultPBKDF2Iterations
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := pbkdf2SHA256([]byte(password), salt, iterations, 32)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iterations,
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(key)), nil
}

func verifyPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iterations, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// handlePasswordLogin mints a session on a correct fallback password.
// It is the one unauthenticated mutation, so the CSRF header is
// still required by hand.
func (s *Server) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-MLP-Client") == "" {
		writeProblem(w, problemf(http.StatusForbidden, "csrf-required",
			"mutations require the X-MLP-Client header (D-170)"))
		return
	}
	var body struct {
		Address  string `json:"address"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeProblem(w, problemf(http.StatusBadRequest, "malformed", "%v", err))
		return
	}
	local, _, err := sn.ParseAddress(sn.MailboxKey(strings.ToLower(strings.TrimSpace(body.Address))))
	if err != nil {
		writeProblem(w, problemf(http.StatusUnauthorized, "auth-required", "unknown address or wrong password"))
		return
	}
	var mailboxID int64
	var stored string
	err = s.DB.QueryRowContext(r.Context(),
		`SELECT m.id, p.hash FROM mailboxes m JOIN password_fallback p ON p.mailbox_id = m.id
		 WHERE m.local_part=?`, local).Scan(&mailboxID, &stored)
	// Deliberately indistinguishable outcomes: unknown mailbox and
	// wrong password answer identically.
	if err != nil || !verifyPassword(stored, body.Password) {
		writeProblem(w, problemf(http.StatusUnauthorized, "auth-required", "unknown address or wrong password"))
		return
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		writeProblem(w, problemf(http.StatusInternalServerError, "malformed", "entropy: %v", err))
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	nowS := s.now().Format(time.RFC3339)
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO sessions (session_hash, mailbox_id, created, last_seen, user_agent) VALUES (?,?,?,?,?)`,
		hashToken(token), mailboxID, nowS, nowS, r.UserAgent()); err != nil {
		writeProblem(w, problemf(http.StatusInternalServerError, "malformed", "store: %v", err))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil,
	})
	writeJSON(w, http.StatusOK, map[string]any{"mailbox_id": mailboxID})
}

func (s *Server) handleSessionsList(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	_, current, _ := s.session(r)
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT session_hash, created, last_seen, user_agent FROM sessions WHERE mailbox_id=? ORDER BY created`, mailbox)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var h, created, lastSeen string
		var ua sql.NullString
		if err := rows.Scan(&h, &created, &lastSeen, &ua); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		out = append(out, map[string]any{
			"id": h[:12], "created": created, "last_seen": lastSeen,
			"user_agent": ua.String, "current": h == current,
		})
	}
	if err := rows.Err(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
	return nil
}

// handleSessionRevoke revokes one session by its 12-character id
// prefix (the list's `id`).
func (s *Server) handleSessionRevoke(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	prefix := r.PathValue("hash")
	if len(prefix) < 8 {
		return problemf(http.StatusBadRequest, "malformed", "session id too short")
	}
	res, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM sessions WHERE mailbox_id=? AND session_hash LIKE ? || '%'`, mailbox, prefix)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return problemf(http.StatusNotFound, "malformed", "no such session")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleSessionsRevokeAll is sign-out-everywhere, including the
// caller's own session.
func (s *Server) handleSessionsRevokeAll(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	if _, err := s.DB.ExecContext(r.Context(),
		`DELETE FROM sessions WHERE mailbox_id=?`, mailbox); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
	return nil
}
