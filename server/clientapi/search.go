package clientapi

import (
	"net/http"
	"strconv"

	"medialet.org/mlp/search"
)

// handleSearch is GET /api/v1/search?q=&limit=&offset= (S4.19,
// D-261): full-text search over this mailbox's messages — subjects,
// derived body text, media names, and text extracted from held media
// (documents are just a media type). The index is node-local derived
// data; search never crosses the wire, and results are scoped to the
// authenticated mailbox through the messages/refs joins.
//
// Response: {"query","match","results":[{"message_id","thread_id",
// "medialet_ca","received_at","subject","matches":[{"via":"message"|
// "media","name","snippet"}]}]} — newest first (mailbox search is
// chronological, like the inbox), snippets mark hits with [ ].
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, mailbox int64) *problem {
	q := r.URL.Query().Get("q")
	if q == "" || len(q) > 256 {
		return problemf(http.StatusBadRequest, "malformed", "q required, at most 256 bytes")
	}
	limit, offset := 20, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return problemf(http.StatusBadRequest, "malformed", "limit outside 1–100")
		}
		limit = n
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return problemf(http.StatusBadRequest, "malformed", "offset must be >= 0")
		}
		offset = n
	}
	match := search.MatchExpr(q)
	empty := map[string]any{"query": q, "match": match, "results": []any{}}
	if match == "" {
		writeJSON(w, http.StatusOK, empty)
		return nil
	}

	ix := s.Search
	if ix == nil {
		ix = &search.Indexer{DB: s.DB} // medialet text still searchable
	}
	if err := ix.SyncMedialets(r.Context()); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "index: %v", err)
	}
	if err := ix.SyncObjects(r.Context()); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "index: %v", err)
	}

	// Two doors into the same messages table: medialet hits join by
	// content address; object hits walk refs (mailbox, urn, medialet)
	// back to the message that delivered them. UNION dedups; the
	// prototype fetches a bounded window and groups in Go (D-pending).
	const rawCap = 400
	rows, err := s.DB.QueryContext(r.Context(), `
		WITH hits AS (
		  SELECT kind, key, snippet(search_fts,'[',']','…',-1,10) AS snip
		  FROM search_fts WHERE search_fts MATCH ?1
		)
		SELECT m.id, m.thread_id, m.medialet_ca, m.received_at, h.snip, 'message', ''
		  FROM hits h JOIN messages m ON h.kind='medialet' AND m.medialet_ca=h.key
		 WHERE m.mailbox_id=?2
		UNION
		SELECT m.id, m.thread_id, m.medialet_ca, m.received_at, h.snip, 'media', COALESCE(r.name,'')
		  FROM hits h
		  JOIN refs r ON h.kind='object' AND r.urn=h.key AND r.mailbox_id=?2
		  JOIN messages m ON m.medialet_ca=r.medialet_ca AND m.mailbox_id=?2
		ORDER BY 4 DESC, 1 DESC LIMIT ?3`, match, mailbox, rawCap)
	if err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "search: %v", err)
	}
	defer rows.Close()

	type hit struct {
		Via     string `json:"via"`
		Name    string `json:"name,omitempty"`
		Snippet string `json:"snippet"`
	}
	type result struct {
		MessageID  int64  `json:"message_id"`
		ThreadID   int64  `json:"thread_id"`
		MedialetCA string `json:"medialet_ca"`
		ReceivedAt string `json:"received_at"`
		Subject    string `json:"subject"`
		Matches    []hit  `json:"matches"`
	}
	var order []int64
	grouped := map[int64]*result{}
	for rows.Next() {
		var res result
		var h hit
		if err := rows.Scan(&res.MessageID, &res.ThreadID, &res.MedialetCA,
			&res.ReceivedAt, &h.Snippet, &h.Via, &h.Name); err != nil {
			return problemf(http.StatusInternalServerError, "malformed", "search: %v", err)
		}
		g, ok := grouped[res.MessageID]
		if !ok {
			g = &res
			grouped[res.MessageID] = g
			order = append(order, res.MessageID)
		}
		g.Matches = append(g.Matches, h)
	}
	if err := rows.Err(); err != nil {
		return problemf(http.StatusInternalServerError, "malformed", "search: %v", err)
	}

	if offset >= len(order) {
		writeJSON(w, http.StatusOK, empty)
		return nil
	}
	page := order[offset:min(offset+limit, len(order))]
	results := make([]*result, 0, len(page))
	for _, id := range page {
		results = append(results, grouped[id])
	}

	// Subjects come from the index's medialet rows (title column) —
	// one lookup for the page, not a raw-JSON parse per row.
	for _, res := range results {
		s.DB.QueryRowContext(r.Context(),
			`SELECT title FROM search_fts WHERE kind='medialet' AND key=?`,
			res.MedialetCA).Scan(&res.Subject)
	}
	writeJSON(w, http.StatusOK, map[string]any{"query": q, "match": match, "results": results})
	return nil
}
