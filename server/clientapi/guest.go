package clientapi

// The guest surface (S3.6, D-151–D-155): sessionless endpoints gated
// by the capability token (+ optional PIN). One Body viewer, two
// hosts (D-151) — the guest payload carries the same render form the
// inbox serves, and the guest page re-sanitizes with the same
// TV-005-proven viewer. No accept economy: guests view and download;
// the objects are the sender's own, live in this store. Views are
// never recorded; downloads are (D-147, verbatim). The claim (D-154)
// re-dispatches the original Signed Medialet to the newly minted
// local mailbox through the real ingest path, and the link survives.

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"time"

	"medialet.org/mlp/sn"
)

const guestPINLockAt = 5 // D-155

func (s *Server) registerGuestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/guest/{token}", s.guestHandler(s.handleGuestPayload))
	mux.HandleFunc("GET /api/v1/guest/{token}/o/{urn}", s.guestHandler(s.handleGuestDownload))
	mux.HandleFunc("POST /api/v1/guest/{token}/claim", s.guestHandler(s.handleGuestClaim))
}

type guestLink struct {
	ID          int64
	DeliveryID  int64
	MedialetCA  string
	ClaimedAddr string
}

// guestHandler resolves the capability: token hash → link row,
// expiry, revocation, then the PIN gate with the D-155 failure lock.
// PIN attempts against a locked link answer 423 without evaluating
// the PIN, so the lock cannot be oracle-probed.
func (s *Server) guestHandler(fn func(w http.ResponseWriter, r *http.Request, g *guestLink) *problem) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		var g guestLink
		var pinHash, claimedAddr, revokedAt sql.NullString
		var expires string
		var failures int
		err := s.DB.QueryRowContext(r.Context(),
			`SELECT gl.id, gl.delivery_id, d.medialet_ca, gl.pin_hash, gl.expires,
			        gl.pin_failures, gl.claimed_addr, gl.revoked_at
			 FROM guest_links gl JOIN deliveries d ON d.id = gl.delivery_id
			 WHERE gl.token_hash=?`, sn.HashToken(token)).
			Scan(&g.ID, &g.DeliveryID, &g.MedialetCA, &pinHash, &expires,
				&failures, &claimedAddr, &revokedAt)
		if errors.Is(err, sql.ErrNoRows) {
			writeProblem(w, problemf(http.StatusNotFound, "malformed", "no such delivery"))
			return
		}
		if err != nil {
			writeProblem(w, problemf(http.StatusInternalServerError, "malformed", "store: %v", err))
			return
		}
		if revokedAt.Valid {
			writeProblem(w, problemf(http.StatusGone, "revoked", "this link was revoked by the sender"))
			return
		}
		if exp, perr := time.Parse(time.RFC3339, expires); perr != nil || s.now().After(exp) {
			writeProblem(w, problemf(http.StatusGone, "expired", "this link has expired"))
			return
		}
		if failures >= guestPINLockAt {
			writeProblem(w, problemf(http.StatusLocked, "locked",
				"too many PIN attempts — ask the sender for a fresh link (D-155)"))
			return
		}
		if pinHash.Valid {
			pin := r.Header.Get("X-MLP-Guest-PIN")
			if pin == "" {
				writeProblem(w, problemf(http.StatusUnauthorized, "pin-required",
					"this delivery is PIN-protected — the sender conveyed the PIN separately"))
				return
			}
			if sn.HashToken(pin) != pinHash.String {
				s.DB.ExecContext(r.Context(),
					`UPDATE guest_links SET pin_failures = pin_failures + 1 WHERE id=?`, g.ID)
				writeProblem(w, problemf(http.StatusUnauthorized, "pin-invalid", "wrong PIN"))
				return
			}
			// A correct PIN resets the counter: failures measure an
			// attack in progress, not a lifetime tally.
			if failures > 0 {
				s.DB.ExecContext(r.Context(),
					`UPDATE guest_links SET pin_failures = 0 WHERE id=?`, g.ID)
			}
		}
		g.ClaimedAddr = claimedAddr.String
		if prob := fn(w, r, &g); prob != nil {
			writeProblem(w, prob)
		}
	}
}

// guestManifest parses the stored medialet's manifest.
func (s *Server) guestManifest(r *http.Request, ca string) (subject, author, created, renderForm, derivedText string, degraded bool, files []map[string]any, prob *problem) {
	var raw []byte
	var rf, dt sql.NullString
	var deg int
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT raw, render_form, derived_text, render_degraded FROM medialets WHERE content_address=?`, ca).
		Scan(&raw, &rf, &dt, &deg); err != nil {
		return "", "", "", "", "", false, nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	var sm struct {
		Medialet struct {
			Subject  string `json:"subject"`
			Author   string `json:"author"`
			Created  string `json:"created"`
			Manifest []struct {
				URN       string `json:"urn"`
				Name      string `json:"name"`
				Size      int64  `json:"size"`
				Type      string `json:"type"`
				PreviewOf string `json:"preview_of"`
			} `json:"manifest"`
		} `json:"medialet"`
	}
	if err := json.Unmarshal(raw, &sm); err != nil {
		return "", "", "", "", "", false, nil, problemf(http.StatusInternalServerError, "malformed", "%v", err)
	}
	for _, m := range sm.Medialet.Manifest {
		f := map[string]any{"urn": m.URN, "name": m.Name, "size": m.Size, "type": m.Type}
		if m.PreviewOf != "" {
			f["preview_of"] = m.PreviewOf
		}
		files = append(files, f)
	}
	if rf.String == "" && deg == 0 {
		rfs, dts, degi := s.backfillRender(r, ca, raw)
		rf.String, dt.String, deg = rfs, dts, degi
	}
	return sm.Medialet.Subject, sm.Medialet.Author, sm.Medialet.Created,
		rf.String, dt.String, deg == 1, files, nil
}

// handleGuestPayload is the delivery page's data. Never recorded:
// opens are not the sender's business (D-147).
func (s *Server) handleGuestPayload(w http.ResponseWriter, r *http.Request, g *guestLink) *problem {
	subject, author, created, renderForm, derivedText, degraded, files, prob := s.guestManifest(r, g.MedialetCA)
	if prob != nil {
		return prob
	}
	body := map[string]any{"profile": "mlp-html/1", "content": renderForm}
	if degraded {
		body["content"] = ""
		body["degraded"] = true
	}
	out := map[string]any{
		"subject": subject, "author": author, "created": created,
		"body": body, "files": files, "domain": s.SN.Domain,
	}
	if degraded {
		out["derived_text"] = derivedText
	}
	if g.ClaimedAddr != "" {
		out["claimed_as"] = g.ClaimedAddr // the link survives (D-154)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// handleGuestDownload serves one manifest object and records the
// fact (D-147: downloads shown).
func (s *Server) handleGuestDownload(w http.ResponseWriter, r *http.Request, g *guestLink) *problem {
	urn := r.PathValue("urn")
	_, _, _, _, _, _, files, prob := s.guestManifest(r, g.MedialetCA)
	if prob != nil {
		return prob
	}
	var typ string
	inManifest := false
	for _, f := range files {
		if f["urn"] == urn {
			inManifest = true
			typ, _ = f["type"].(string)
			break
		}
	}
	if !inManifest {
		return problemf(http.StatusNotFound, "malformed", "not part of this delivery")
	}
	var state string
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT state FROM objects WHERE urn=?`, urn).Scan(&state); err != nil || state != "live" {
		return problemf(http.StatusNotFound, "not-available", "object not held")
	}
	f, ferr := os.Open(s.BS.ObjectPath(urn))
	if ferr != nil {
		return problemf(http.StatusNotFound, "not-available", "object bytes missing")
	}
	defer f.Close()
	nowS := s.now().Format(time.RFC3339)
	s.DB.ExecContext(r.Context(),
		`INSERT INTO guest_downloads (link_id, urn, at) VALUES (?,?,?)`, g.ID, urn, nowS)
	s.DB.ExecContext(r.Context(),
		`INSERT INTO timeline_events (delivery_id, at, kind, data_json) VALUES (?,?,?,?)`,
		g.DeliveryID, nowS, "guest.download", `{"urn":"`+urn+`"}`)
	if typ == "" {
		typ = "application/octet-stream"
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Content-Disposition", "attachment")
	w.WriteHeader(http.StatusOK)
	copyFile(w, f)
	return nil
}

var localPartRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// handleGuestClaim is the D-154 ceremony: possession of link + PIN
// is the identity proof; the mailbox is minted, the delivery
// re-dispatched through the real ingest, a session issued (the
// passkey registration follows under it), and the link marked —
// never revoked.
func (s *Server) handleGuestClaim(w http.ResponseWriter, r *http.Request, g *guestLink) *problem {
	if g.ClaimedAddr != "" {
		return problemf(http.StatusConflict, "claimed",
			"this delivery was already claimed as %s (one claim per link, D-155)", g.ClaimedAddr)
	}
	var body struct {
		LocalPart string `json:"local_part"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	if !localPartRe.MatchString(body.LocalPart) {
		return problemf(http.StatusBadRequest, "malformed", "local part must match %s", localPartRe)
	}
	nowS := s.now().Format(time.RFC3339)
	res, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO mailboxes (local_part, created) VALUES (?,?)`, body.LocalPart, nowS)
	if err != nil {
		return problemf(http.StatusConflict, "taken", "that address is taken")
	}
	mailboxID, _ := res.LastInsertId()
	addr := body.LocalPart + "@" + s.SN.Domain

	envID, snProb := s.SN.Redispatch(r.Context(), g.MedialetCA, addr)
	if snProb != nil {
		return problemf(snProb.Status, snProb.Code, "%s", snProb.Detail)
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE guest_links SET claimed_addr=?, claimed_at=? WHERE id=?`,
		addr, nowS, g.ID); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	s.DB.ExecContext(r.Context(),
		`INSERT INTO timeline_events (delivery_id, at, kind, data_json) VALUES (?,?,?,?)`,
		g.DeliveryID, nowS, "guest.claimed", `{"addr":"`+addr+`","envelope_id":"`+envID+`"}`)

	token, mintProb := s.mintSession(r, mailboxID)
	if mintProb != nil {
		return mintProb
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"address": addr, "mailbox_id": mailboxID, "envelope_id": envID,
	})
	return nil
}

// mintSession issues a fresh session token for a mailbox.
func (s *Server) mintSession(r *http.Request, mailboxID int64) (string, *problem) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", problemf(http.StatusInternalServerError, "malformed", "entropy: %v", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	nowS := s.now().Format(time.RFC3339)
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO sessions (session_hash, mailbox_id, created, last_seen, user_agent) VALUES (?,?,?,?,?)`,
		hashToken(token), mailboxID, nowS, nowS, r.UserAgent()); err != nil {
		return "", problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	return token, nil
}
