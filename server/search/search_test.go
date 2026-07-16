package search

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"medialet.org/mlp/store"
)

// --- fixture builders -------------------------------------------------

func zipDoc(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func docxBytes(t *testing.T, paragraphs ...string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, p := range paragraphs {
		fmt.Fprintf(&b, `<w:p><w:r><w:t>%s</w:t></w:r></w:p>`, p)
	}
	b.WriteString(`</w:body></w:document>`)
	return zipDoc(t, map[string]string{"word/document.xml": b.String()})
}

func xlsxBytes(t *testing.T, shared ...string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for _, s := range shared {
		fmt.Fprintf(&b, `<si><t>%s</t></si>`, s)
	}
	b.WriteString(`</sst>`)
	return zipDoc(t, map[string]string{
		"xl/sharedStrings.xml":     b.String(),
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c t="inlineStr"><is><t>inline cell text</t></is></c></row></sheetData></worksheet>`,
	})
}

// pdfBytes builds a one-page PDF whose content stream (FlateDecode
// when deflate=true) shows the given text with Tj and TJ.
func pdfBytes(t *testing.T, deflate bool, text string) []byte {
	t.Helper()
	content := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj T* [(and) -250 (a TJ array)] TJ ET", text)
	body := []byte(content)
	filter := ""
	if deflate {
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		zw.Write(body)
		zw.Close()
		body = z.Bytes()
		filter = "/Filter /FlateDecode "
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "%%PDF-1.4\n1 0 obj << %s/Length %d >>\nstream\n", filter, len(body))
	b.Write(body)
	b.WriteString("\nendstream\nendobj\ntrailer << >>\n%%EOF")
	return b.Bytes()
}

// --- extractor tests --------------------------------------------------

func TestExtractors(t *testing.T) {
	cases := []struct {
		name  string
		e     Extractor
		typ   string
		data  []byte
		wants []string
	}{
		{"docx", docxExtractor{}, typeDocx,
			docxBytes(t, "Zmluva o diele", "Svadba Žilina — kontrakt"),
			[]string{"Zmluva o diele", "Svadba Žilina"}},
		{"xlsx", xlsxExtractor{}, typeXlsx,
			xlsxBytes(t, "shot list", "golden hour set"),
			[]string{"shot list", "golden hour set", "inline cell text"}},
		{"pdf-flate", pdfExtractor{}, "application/pdf",
			pdfBytes(t, true, "Lisbon rooftop wedding"),
			[]string{"Lisbon rooftop wedding", "and a TJ array"}},
		{"pdf-raw", pdfExtractor{}, "application/pdf",
			pdfBytes(t, false, "uncompressed stream text"),
			[]string{"uncompressed stream text"}},
		{"text", textExtractor{}, "text/plain",
			[]byte("plain\x00 body\n\n\n\nwith runs"),
			[]string{"plain body", "with runs"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !c.e.Claims(c.typ, "") {
				t.Fatalf("%s does not claim %s", c.e.Name(), c.typ)
			}
			got, err := c.e.Extract(c.data)
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range c.wants {
				if !strings.Contains(got, w) {
					t.Fatalf("extracted %q lacks %q", got, w)
				}
			}
		})
	}
}

func TestExtractorClaimsByName(t *testing.T) {
	if !(docxExtractor{}).Claims("application/octet-stream", "contract.DOCX") {
		t.Fatal("docx should claim by suffix")
	}
	if (pdfExtractor{}).Claims("image/jpeg", "photo.jpg") {
		t.Fatal("pdf must not claim a jpeg")
	}
}

func TestPDFPrintabilityFilterDropsGarbage(t *testing.T) {
	// A hex string of CID codes (no ToUnicode): mostly non-printable
	// runes after Latin-1 mapping — must be filtered, not indexed.
	content := "BT <0102030405060708> Tj ET"
	var b bytes.Buffer
	fmt.Fprintf(&b, "%%PDF-1.4\n1 0 obj << /Length %d >>\nstream\n%s\nendstream\nendobj", len(content), content)
	got, err := (pdfExtractor{}).Extract(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "" {
		t.Fatalf("garbage survived the filter: %q", got)
	}
}

// --- indexer round-trip ------------------------------------------------

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestIndexerRoundTrip(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO mailboxes (id, local_part, created) VALUES (1,'petra','2026-07-16T00:00:00Z')`)
	exec(`INSERT INTO stores (id, name) VALUES (1,'default')`)
	exec(`INSERT INTO objects (urn, size, state, store_id, created_at) VALUES ('urn:mlet:pdf1', 9, 'live', 1, '2026-07-16T00:00:00Z')`)
	exec(`INSERT INTO medialets (content_address, author, medialet_id, created, raw, derived_text)
	      VALUES ('ca1','petra@p.example','m1','2026-07-16T00:00:00Z',
	              '{"medialet":{"subject":"Svadba Žilina","manifest":[{"name":"contact-sheet.pdf"}]}}',
	              'fotky zo svadby')`)
	exec(`INSERT INTO refs (mailbox_id, urn, medialet_ca, direction, state, name, size, type, available_until, updated_at)
	      VALUES (1,'urn:mlet:pdf1','ca1','in','available','contact-sheet.pdf',9,'application/pdf','2027-01-01T00:00:00Z','2026-07-16T00:00:00Z')`)

	pdf := pdfBytes(t, true, "Lisbon rooftop wedding")
	ix := &Indexer{DB: db, Open: func(urn string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(pdf)), nil
	}}

	if err := ix.SyncMedialets(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ix.IndexObject(ctx, "urn:mlet:pdf1"); err != nil {
		t.Fatal(err)
	}

	q := func(match string) (kinds []string) {
		rows, err := db.Query(`SELECT kind FROM search_fts WHERE search_fts MATCH ?`, match)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			rows.Scan(&k)
			kinds = append(kinds, k)
		}
		return
	}
	if got := q(MatchExpr("zilina")); len(got) != 1 || got[0] != "medialet" {
		t.Fatalf("diacritics-folded subject match: %v", got) // 'zilina' finds 'Žilina'
	}
	if got := q(MatchExpr("lisbon")); len(got) != 1 || got[0] != "object" {
		t.Fatalf("extracted PDF text match: %v", got)
	}
	if got := q(MatchExpr("contact*")); len(got) != 2 {
		t.Fatalf("filename prefix should match both rows: %v", got)
	}

	// Idempotent re-index and full rebuild both converge.
	if err := ix.IndexObject(ctx, "urn:mlet:pdf1"); err != nil {
		t.Fatal(err)
	}
	if got := q(MatchExpr("lisbon")); len(got) != 1 {
		t.Fatalf("re-index duplicated rows: %v", got)
	}
	if err := ix.Reindex(ctx); err != nil {
		t.Fatal(err)
	}
	if got := q(MatchExpr("lisbon")); len(got) != 1 {
		t.Fatalf("after Reindex: %v", got)
	}
	var extractor string
	if err := db.QueryRow(`SELECT extractor FROM object_text WHERE urn='urn:mlet:pdf1'`).Scan(&extractor); err != nil || extractor != "pdf" {
		t.Fatalf("object_text cache: %q %v", extractor, err)
	}
}

func TestMatchExpr(t *testing.T) {
	for in, want := range map[string]string{
		"Svadba Žilina":      "svadba žilina",
		`"quoted" OR (bomb)`: "quoted or bomb", // operators neutralized
		"contact*":           "contact*",
		"  --  ":             "",
		"NEAR near":          "near near",
	} {
		if got := MatchExpr(in); got != want {
			t.Fatalf("MatchExpr(%q) = %q, want %q", in, got, want)
		}
	}
}
