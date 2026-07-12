package clientapi

// Passkey ceremonies over the webauthn package (D-161: passkey-first;
// the password_fallback table remains exactly that). Registration
// binds a credential to the session's mailbox — the claim flow lands
// here immediately after minting its session. Login begins with an
// address, answers the allow-list, and verifies the assertion
// against the stored COSE key. Challenges are single-use, short TTL.

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"medialet.org/mlp/sn"
	"medialet.org/mlp/webauthn"
)

const challengeTTL = 5 * time.Minute

func (s *Server) registerWebAuthnRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/webauthn/register/begin", s.handler(s.handleRegisterBegin))
	mux.HandleFunc("POST /api/v1/webauthn/register/finish", s.handler(s.handleRegisterFinish))
	mux.HandleFunc("POST /api/v1/webauthn/login/begin", s.public(s.handleLoginBegin))
	mux.HandleFunc("POST /api/v1/webauthn/login/finish", s.public(s.handleLoginFinish))
}

// public adapts a sessionless endpoint with the CSRF header check.
func (s *Server) public(fn func(w http.ResponseWriter, r *http.Request) *problem) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-MLP-Client") == "" {
			writeProblem(w, problemf(http.StatusForbidden, "csrf-required",
				"mutations require the X-MLP-Client header (D-170)"))
			return
		}
		if prob := fn(w, r); prob != nil {
			writeProblem(w, prob)
		}
	}
}

func (s *Server) newChallenge(r *http.Request, purpose string, mailbox any) (string, *problem) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", problemf(http.StatusInternalServerError, "malformed", "entropy: %v", err)
	}
	ch := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO webauthn_challenges (challenge, purpose, mailbox_id, created, expires_at)
		 VALUES (?,?,?,?,?)`,
		ch, purpose, mailbox, now.Format(time.RFC3339),
		now.Add(challengeTTL).Format(time.RFC3339)); err != nil {
		return "", problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	return ch, nil
}

// consumeChallenge deletes and returns the row — single-use by
// construction.
func (s *Server) consumeChallenge(r *http.Request, ch, purpose string) (mailbox sql.NullInt64, prob *problem) {
	var expires string
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT mailbox_id, expires_at FROM webauthn_challenges WHERE challenge=? AND purpose=?`,
		ch, purpose).Scan(&mailbox, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return mailbox, problemf(http.StatusUnauthorized, "challenge-invalid", "unknown or reused challenge")
	}
	if err != nil {
		return mailbox, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	s.DB.ExecContext(r.Context(), `DELETE FROM webauthn_challenges WHERE challenge=?`, ch)
	if exp, perr := time.Parse(time.RFC3339, expires); perr != nil || s.now().After(exp) {
		return mailbox, problemf(http.StatusUnauthorized, "challenge-expired", "challenge expired")
	}
	return mailbox, nil
}

func (s *Server) rpID() string { return s.SN.Domain }

// Origin is the browser origin permitted in clientDataJSON; empty
// disables the check (tests, plain-HTTP prototypes set it).
func (s *Server) webauthnOrigin() string {
	if s.WebAuthnOrigin != "" {
		return s.WebAuthnOrigin
	}
	return "https://" + s.SN.Domain
}

func (s *Server) handleRegisterBegin(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	ch, prob := s.newChallenge(r, "register", mailbox)
	if prob != nil {
		return prob
	}
	var local string
	s.DB.QueryRowContext(r.Context(), `SELECT local_part FROM mailboxes WHERE id=?`, mailbox).Scan(&local)
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge": ch,
		"rp":        map[string]any{"id": s.rpID(), "name": "Medialet — " + s.SN.Domain},
		"user": map[string]any{
			"id":   base64.RawURLEncoding.EncodeToString([]byte(local + "@" + s.SN.Domain)),
			"name": local + "@" + s.SN.Domain, "displayName": local,
		},
		"pubKeyCredParams": []map[string]any{
			{"type": "public-key", "alg": webauthn.AlgEd25519},
			{"type": "public-key", "alg": webauthn.AlgES256},
		},
		"attestation": "none",
	})
	return nil
}

func (s *Server) handleRegisterFinish(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	var body struct {
		Challenge         string `json:"challenge"`
		ClientDataJSON    string `json:"client_data_json"`   // base64url
		AttestationObject string `json:"attestation_object"` // base64url
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&body); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	chMailbox, prob := s.consumeChallenge(r, body.Challenge, "register")
	if prob != nil {
		return prob
	}
	if !chMailbox.Valid || chMailbox.Int64 != mailbox {
		return problemf(http.StatusUnauthorized, "challenge-invalid", "challenge belongs to another registration")
	}
	cd, err1 := base64.RawURLEncoding.DecodeString(body.ClientDataJSON)
	att, err2 := base64.RawURLEncoding.DecodeString(body.AttestationObject)
	if err1 != nil || err2 != nil {
		return problemf(http.StatusBadRequest, "malformed", "base64url fields")
	}
	reg, err := webauthn.VerifyRegistration(cd, att, body.Challenge, s.webauthnOrigin(), s.rpID())
	if err != nil {
		return problemf(http.StatusUnauthorized, "registration-invalid", "%v", err)
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO webauthn_credentials (credential_id, mailbox_id, public_key, alg, sign_count, created)
		 VALUES (?,?,?,?,?,?)`,
		reg.CredentialID, mailbox, reg.COSEKey, reg.Alg, reg.SignCount,
		s.now().Format(time.RFC3339)); err != nil {
		return problemf(http.StatusConflict, "credential-exists", "that credential is already registered")
	}
	writeJSON(w, http.StatusOK, map[string]any{"credential_id": reg.CredentialID})
	return nil
}

func (s *Server) handleLoginBegin(w http.ResponseWriter, r *http.Request) *problem {
	var body struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	local, _, err := sn.ParseAddress(sn.MailboxKey(strings.ToLower(strings.TrimSpace(body.Address))))
	if err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	var mailboxID int64
	dbErr := s.DB.QueryRowContext(r.Context(),
		`SELECT id FROM mailboxes WHERE local_part=?`, local).Scan(&mailboxID)
	// Indistinguishable outcomes for unknown addresses: a challenge
	// with an empty allow-list (the assertion can never verify).
	var allow []map[string]any
	var challengeOwner any
	if dbErr == nil {
		challengeOwner = mailboxID
		rows, err := s.DB.QueryContext(r.Context(),
			`SELECT credential_id FROM webauthn_credentials WHERE mailbox_id=?`, mailboxID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					allow = append(allow, map[string]any{"type": "public-key", "id": id})
				}
			}
		}
	}
	ch, prob := s.newChallenge(r, "login", challengeOwner)
	if prob != nil {
		return prob
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge": ch, "rpId": s.rpID(), "allowCredentials": allow,
	})
	return nil
}

func (s *Server) handleLoginFinish(w http.ResponseWriter, r *http.Request) *problem {
	var body struct {
		Challenge         string `json:"challenge"`
		CredentialID      string `json:"credential_id"`
		ClientDataJSON    string `json:"client_data_json"`
		AuthenticatorData string `json:"authenticator_data"`
		Signature         string `json:"signature"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<17)).Decode(&body); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	chMailbox, prob := s.consumeChallenge(r, body.Challenge, "login")
	if prob != nil {
		return prob
	}
	var coseKey []byte
	var alg int64
	var signCount uint32
	var credMailbox int64
	err := s.DB.QueryRowContext(r.Context(),
		`SELECT mailbox_id, public_key, alg, sign_count FROM webauthn_credentials WHERE credential_id=?`,
		body.CredentialID).Scan(&credMailbox, &coseKey, &alg, &signCount)
	if err != nil || !chMailbox.Valid || credMailbox != chMailbox.Int64 {
		return problemf(http.StatusUnauthorized, "auth-required", "unknown credential")
	}
	cd, err1 := base64.RawURLEncoding.DecodeString(body.ClientDataJSON)
	ad, err2 := base64.RawURLEncoding.DecodeString(body.AuthenticatorData)
	sig, err3 := base64.RawURLEncoding.DecodeString(body.Signature)
	if err1 != nil || err2 != nil || err3 != nil {
		return problemf(http.StatusBadRequest, "malformed", "base64url fields")
	}
	as, err := webauthn.VerifyAssertion(cd, ad, sig, coseKey, body.Challenge, s.webauthnOrigin(), s.rpID())
	if err != nil {
		return problemf(http.StatusUnauthorized, "auth-required", "%v", err)
	}
	// Clone detection: a sign-count regression is logged, not fatal
	// (platform authenticators legitimately report zero).
	if as.SignCount != 0 && as.SignCount <= signCount && signCount != 0 {
		log.Printf("webauthn: sign-count regression on %s (%d -> %d)", body.CredentialID, signCount, as.SignCount)
	}
	s.DB.ExecContext(r.Context(),
		`UPDATE webauthn_credentials SET sign_count=? WHERE credential_id=?`,
		as.SignCount, body.CredentialID)
	token, prob := s.mintSession(r, credMailbox)
	if prob != nil {
		return prob
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil,
	})
	writeJSON(w, http.StatusOK, map[string]any{"mailbox_id": credMailbox})
	return nil
}
