package clientapi

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"medialet.org/mlp/search"
)

// searchPDF builds a minimal FlateDecode PDF showing the given text.
func searchPDF(text string) []byte {
	content := fmt.Sprintf("BT /F1 12 Tf (%s) Tj ET", text)
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write([]byte(content))
	zw.Close()
	var b bytes.Buffer
	fmt.Fprintf(&b, "%%PDF-1.4\n1 0 obj << /Filter /FlateDecode /Length %d >>\nstream\n", z.Len())
	b.Write(z.Bytes())
	b.WriteString("\nendstream\nendobj\ntrailer << >>\n%%EOF")
	return b.Bytes()
}

func TestSearchEndpoint(t *testing.T) {
	clock := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	s := newAPI(t, "p.example", "petra", seedT3, &clock, nil)

	// Two mailboxes on one node: scoping must hold (D-04 in spirit —
	// the index is shared derived data, results are per-mailbox).
	mustExec(t, s.DB, `INSERT INTO mailboxes (id, local_part, created) VALUES (2,'milan','2026-01-01T00:00:00Z')`)

	// A delivered message for petra: subject with diacritics, body
	// derived text, and a PDF contact sheet held live.
	raw := `{"medialet":{"subject":"Svadba Žilina","manifest":[{"name":"contact-sheet.pdf"}]}}`
	mustExec(t, s.DB, `INSERT INTO medialets (content_address, author, medialet_id, created, raw, derived_text)
	  VALUES ('ca-shoot','petra@p.example','m1','2026-07-15T00:00:00Z',?, 'golden hour set from the rooftop')`, raw)
	mustExec(t, s.DB, `INSERT INTO threads (id, mailbox_id, root_ca, last_activity) VALUES (10,1,'ca-shoot','2026-07-15T00:00:00Z')`)
	mustExec(t, s.DB, `INSERT INTO messages (id, mailbox_id, medialet_ca, thread_id, received_at) VALUES (100,1,'ca-shoot',10,'2026-07-15T09:00:00Z')`)
	mustExec(t, s.DB, `INSERT INTO objects (urn, size, state, store_id, created_at) VALUES ('urn:mlet:sheet',9,'live',1,'2026-07-15T00:00:00Z')`)
	mustExec(t, s.DB, `INSERT INTO refs (mailbox_id, urn, medialet_ca, direction, state, name, size, type, available_until, updated_at)
	  VALUES (1,'urn:mlet:sheet','ca-shoot','in','available','contact-sheet.pdf',9,'application/pdf','2027-01-01T00:00:00Z','2026-07-15T00:00:00Z')`)

	// Milan's own message on the same node matches the same words —
	// it must never surface in petra's results.
	mustExec(t, s.DB, `INSERT INTO medialets (content_address, author, medialet_id, created, raw, derived_text)
	  VALUES ('ca-other','x@q.example','m2','2026-07-15T00:00:00Z','{"medialet":{"subject":"Svadba tajná"}}','rooftop secrets')`)
	mustExec(t, s.DB, `INSERT INTO threads (id, mailbox_id, root_ca, last_activity) VALUES (11,2,'ca-other','2026-07-15T00:00:00Z')`)
	mustExec(t, s.DB, `INSERT INTO messages (id, mailbox_id, medialet_ca, thread_id, received_at) VALUES (101,2,'ca-other',11,'2026-07-15T09:30:00Z')`)

	// Wire an indexer whose Open serves the PDF bytes, and index the
	// object the way OnVerified would.
	s.Search = newTestIndexer(s, map[string][]byte{"urn:mlet:sheet": searchPDF("Lisbon rooftop wedding")})
	if err := s.Search.IndexObject(context.Background(), "urn:mlet:sheet"); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "petra@p.example")

	get := func(query string) map[string]any {
		t.Helper()
		resp := do("GET", "/api/v1/search?"+query, "", nil)
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("search %s: %d %s", query, resp.StatusCode, body)
		}
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		return out
	}
	results := func(out map[string]any) []map[string]any {
		var rs []map[string]any
		for _, r := range out["results"].([]any) {
			rs = append(rs, r.(map[string]any))
		}
		return rs
	}

	// Subject hit with diacritics folded: 'zilina' finds 'Žilina'.
	rs := results(get("q=zilina"))
	if len(rs) != 1 || rs[0]["message_id"].(float64) != 100 || rs[0]["subject"] != "Svadba Žilina" {
		t.Fatalf("subject search: %+v", rs)
	}

	// Media-text hit: the word exists only inside the PDF.
	rs = results(get("q=lisbon"))
	if len(rs) != 1 {
		t.Fatalf("media search: %+v", rs)
	}
	m := rs[0]["matches"].([]any)[0].(map[string]any)
	if m["via"] != "media" || m["name"] != "contact-sheet.pdf" {
		t.Fatalf("media match shape: %+v", m)
	}

	// One message, two doors: 'rooftop' is in the body AND the PDF —
	// grouped as one result with both matches; milan's message with
	// the same word must not leak into petra's view.
	rs = results(get("q=rooftop"))
	if len(rs) != 1 || len(rs[0]["matches"].([]any)) != 2 {
		t.Fatalf("grouping/scoping: %+v", rs)
	}

	// Filename search finds the message; prefix operator survives.
	if rs = results(get("q=contact*")); len(rs) != 1 {
		t.Fatalf("filename prefix: %+v", rs)
	}

	// Hostile input is sanitized, never a MATCH syntax error.
	if out := get(`q=%22unbalanced%20OR%20NEAR(`); out["results"] == nil {
		t.Fatalf("sanitizer: %+v", out)
	}

	// Paging: offset beyond the result set is an empty page.
	if rs = results(get("q=rooftop&offset=5")); len(rs) != 0 {
		t.Fatalf("offset paging: %+v", rs)
	}

	// Bad parameters are problems, not panics.
	for _, q := range []string{"q=", "q=x&limit=0", "q=x&limit=101", "q=x&offset=-1"} {
		if resp := do("GET", "/api/v1/search?"+q, "", nil); resp.StatusCode != 400 {
			t.Fatalf("%s: want 400, got %d", q, resp.StatusCode)
		}
	}
}

// newTestIndexer wires an Indexer whose Open serves from a byte map.
func newTestIndexer(s *Server, blobs map[string][]byte) *search.Indexer {
	return &search.Indexer{DB: s.DB, Open: func(urn string) (io.ReadCloser, error) {
		b, ok := blobs[urn]
		if !ok {
			return nil, fmt.Errorf("no blob %s", urn)
		}
		return io.NopCloser(bytes.NewReader(b)), nil
	}}
}
