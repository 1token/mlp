package clientapi

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"medialet.org/mlp/core"
	"medialet.org/mlp/sn"
)

// The composer's API half (S3.3, D-133–D-138): drafts autosave, the
// intra-domain upload door (the D-135 pipeline over the same bs
// transactional core — one code path, D-79), and send.

func (s *Server) registerComposeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/drafts", s.handler(s.handleDraftCreate))
	mux.HandleFunc("GET /api/v1/drafts", s.handler(s.handleDraftList))
	mux.HandleFunc("GET /api/v1/drafts/{id}", s.handler(s.handleDraftGet))
	mux.HandleFunc("PATCH /api/v1/drafts/{id}", s.handler(s.handleDraftPatch))
	mux.HandleFunc("DELETE /api/v1/drafts/{id}", s.handler(s.handleDraftDelete))
	mux.HandleFunc("POST /api/v1/drafts/{id}/send", s.handler(s.handleDraftSend))
	mux.HandleFunc("POST /api/v1/uploads", s.handler(s.handleUploadCreate))
	mux.HandleFunc("HEAD /api/v1/uploads/{token}", s.handler(s.handleUploadHead))
	mux.HandleFunc("PATCH /api/v1/uploads/{token}", s.handler(s.handleUploadPatch))
}

// --- Drafts (D-138: a draft is unsigned medialet JSON + upload state) --

func (s *Server) handleDraftCreate(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	doc, prob := readDraftDoc(w, r)
	if prob != nil {
		return prob
	}
	raw := make([]byte, 16)
	rand.Read(raw)
	id := hex.EncodeToString(raw)
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO drafts (id, mailbox_id, doc_json, updated) VALUES (?,?,?,?)`,
		id, mailbox, doc, s.now().Format(time.RFC3339)); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	return nil
}

func readDraftDoc(w http.ResponseWriter, r *http.Request) (string, *problem) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		return "", problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	var d sn.DraftContent
	if err := json.Unmarshal(body, &d); err != nil {
		return "", problemf(http.StatusBadRequest, "malformed", "draft: %v", err)
	}
	return string(body), nil
}

func (s *Server) handleDraftList(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, doc_json, updated FROM drafts WHERE mailbox_id=? ORDER BY updated DESC`, mailbox)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, doc, updated string
		if err := rows.Scan(&id, &doc, &updated); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		out = append(out, map[string]any{"id": id, "doc": json.RawMessage(doc), "updated": updated})
	}
	if err := rows.Err(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": out})
	return nil
}

func (s *Server) ownDraft(r *http.Request, mailbox int64) (string, *problem) {
	id := r.PathValue("id")
	var owner int64
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT mailbox_id FROM drafts WHERE id=?`, id).Scan(&owner); err != nil || owner != mailbox {
		return "", problemf(http.StatusNotFound, "malformed", "no such draft")
	}
	return id, nil
}

func (s *Server) handleDraftGet(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	id, prob := s.ownDraft(r, mailbox)
	if prob != nil {
		return prob
	}
	var doc, updated string
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT doc_json, updated FROM drafts WHERE id=?`, id).Scan(&doc, &updated); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "doc": json.RawMessage(doc), "updated": updated})
	return nil
}

func (s *Server) handleDraftPatch(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	id, prob := s.ownDraft(r, mailbox)
	if prob != nil {
		return prob
	}
	doc, prob := readDraftDoc(w, r)
	if prob != nil {
		return prob
	}
	if _, err := s.DB.ExecContext(r.Context(),
		`UPDATE drafts SET doc_json=?, updated=? WHERE id=?`,
		doc, s.now().Format(time.RFC3339), id); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) handleDraftDelete(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	id, prob := s.ownDraft(r, mailbox)
	if prob != nil {
		return prob
	}
	s.DB.ExecContext(r.Context(), `DELETE FROM drafts WHERE id=?`, id)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleDraftSend: the signature moment (D-138 — the 10 s undo hold
// already elapsed client-side). Success deletes the draft.
func (s *Server) handleDraftSend(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	id, prob := s.ownDraft(r, mailbox)
	if prob != nil {
		return prob
	}
	var doc string
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT doc_json FROM drafts WHERE id=?`, id).Scan(&doc); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	var d sn.DraftContent
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "draft: %v", err)
	}
	var local string
	if err := s.DB.QueryRowContext(r.Context(),
		`SELECT local_part FROM mailboxes WHERE id=?`, mailbox).Scan(&local); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	result, sprob := s.SN.Send(r.Context(), mailbox, local+"@"+s.SN.Domain, &d)
	if sprob != nil {
		return problemf(sprob.Status, sprob.Code, "%s", sprob.Detail)
	}
	s.DB.ExecContext(r.Context(), `DELETE FROM drafts WHERE id=?`, id)
	s.Hub.Emit(r.Context(), mailbox, "delivery.created",
		map[string]any{"delivery_id": result.DeliveryID})
	writeJSON(w, http.StatusOK, result)
	return nil
}

// --- Intra-domain uploads (D-135 over the bs core) ---------------------

// handleUploadCreate declares (urn, size) — hash-first (D-135): the
// client computes the address before bytes move — and mints an
// intra-domain reservation on our own BS. Present-and-live answers
// have:true with no upload (attach by reference).
func (s *Server) handleUploadCreate(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	var body struct {
		URN  string `json:"urn"`
		Size int64  `json:"size"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	if _, err := core.ParseURNMlet(body.URN); err != nil {
		return problemf(http.StatusBadRequest, "malformed", "%v", err)
	}
	if body.Size <= 0 {
		return problemf(http.StatusBadRequest, "malformed", "size must be positive")
	}
	var state string
	err := s.DB.QueryRowContext(r.Context(), `SELECT state FROM objects WHERE urn=?`, body.URN).Scan(&state)
	if err == nil && state == "live" {
		writeJSON(w, http.StatusOK, map[string]any{"have": true})
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	raw := make([]byte, 24)
	rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := s.DB.ExecContext(r.Context(),
		`INSERT INTO reservations_in (token_hash, urn, max_size, pusher_domain, expires, state, store_id, created)
		 VALUES (?,?,?,?,?,'pending',1,?)`,
		hashToken(token), body.URN, body.Size, s.SN.Domain,
		s.now().Add(24*time.Hour).Format(time.RFC3339), s.now().Format(time.RFC3339)); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"have": false, "upload": "/api/v1/uploads/" + token, "chunk_offset": 0,
	})
	return nil
}

func (s *Server) handleUploadHead(w http.ResponseWriter, r *http.Request, _ int64) *problem {
	offset, length, bprob := s.BS.LocalHead(r.Context(), r.PathValue("token"))
	if bprob != nil {
		return problemf(bprob.Status, bprob.Code, "%s", bprob.Detail)
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(offset, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	return nil
}

// handleUploadPatch drives the same transactional pipeline as the
// federation door (checkpointed BLAKE3, URN verification at
// completion); session auth replaces RFC 9421 (D-79 intra-domain
// freedom). The chunk digests itself as it is read — the TLS+session
// channel carries transport integrity and the URN completion check
// stays absolute; a client-supplied Content-Digest, when present,
// must match or the chunk is refused (client-side corruption still
// detectable).
func (s *Server) handleUploadPatch(w http.ResponseWriter, r *http.Request, _ int64) *problem {
	reqOffset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || reqOffset < 0 {
		return problemf(http.StatusConflict, "offset-mismatch", "malformed Upload-Offset")
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 512<<20))
	if err != nil {
		return problemf(http.StatusBadRequest, "digest-mismatch", "%v", err)
	}
	sum := sha256.Sum256(body)
	if h := r.Header.Get("Content-Digest"); h != "" {
		claimed, ok := strings.CutPrefix(strings.TrimSpace(h), "sha-256=:")
		if !ok || !strings.HasSuffix(claimed, ":") {
			return problemf(http.StatusBadRequest, "digest-mismatch", "Content-Digest must be sha-256")
		}
		want, derr := base64.StdEncoding.DecodeString(strings.TrimSuffix(claimed, ":"))
		if derr != nil || !bytesEqual(want, sum[:]) {
			return problemf(http.StatusUnprocessableEntity, "digest-mismatch", "chunk digest mismatch")
		}
	}
	offset, verified, bprob := s.BS.LocalPatch(r.Context(), r.PathValue("token"), sum[:], reqOffset, bytes.NewReader(body))
	if bprob != nil {
		return problemf(bprob.Status, bprob.Code, "%s", bprob.Detail)
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(offset, 10))
	if verified {
		w.Header().Set("MLP-Object-State", "verified")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
