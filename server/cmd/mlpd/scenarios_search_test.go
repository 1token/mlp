// TestScenarioSearchFindsTheShoot — the S4.19 story (D-261). Petra
// delivers a shoot with a PDF contact sheet; Milan finds the message
// by a word that exists nowhere in the envelope — only inside the
// delivered PDF. Run it alone:
//
//	go test ./cmd/mlpd/ -run TestScenarioSearchFindsTheShoot -v
//
// The protocol facts this story proves:
//   - documents are just a media type: the PDF travels as ordinary
//     §8 heavy media; nothing on the wire knows or cares that a text
//     extractor exists on the receiving node
//   - the index is node-local derived data: Milan's node extracts at
//     the OnVerified moment (custody verified, §8.4), and search is
//     a client-API surface only — no query ever crosses the wire
//   - the sender's copy self-heals: Petra's upload verified before
//     her send created its promise rows, so her node indexed nothing
//     at that moment; her first search sweeps the gap (SyncObjects)
//     and finds her own sent shoot the same way
//   - diacritics fold at both ends: 'nahlady' finds 'náhľady'
package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"net/http"
	"testing"
)

// scenarioPDF builds a minimal FlateDecode PDF showing the text.
func scenarioPDF(text string) []byte {
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

type searchHit struct {
	MessageID int64  `json:"message_id"`
	Subject   string `json:"subject"`
	Matches   []struct {
		Via     string `json:"via"`
		Name    string `json:"name"`
		Snippet string `json:"snippet"`
	} `json:"matches"`
}

func searchFor(t *testing.T, c *client, q string) []searchHit {
	t.Helper()
	var out struct {
		Results []searchHit `json:"results"`
	}
	if code := c.json(http.MethodGet, "/api/v1/search?q="+q, "", &out); code != 200 {
		t.Fatalf("search %q: %d", q, code)
	}
	return out.Results
}

func TestScenarioSearchFindsTheShoot(t *testing.T) {
	w := newWorld(t, map[string]string{"photo.demo": "petra", "client.demo": "milan"})
	petra := w.login("petra@photo.demo")
	milan := w.login("milan@client.demo")

	pdf := scenarioPDF("Lisbon rooftop wedding - golden hour set")
	urn := upload(t, petra, pdf, false)
	w.send(petra, draftSpec{
		Subject:    "Svadba — náhľady",
		Body:       "<p>Prvé zábery zo strechy.</p>",
		Recipients: []string{"milan@client.demo"},
		Manifest: []map[string]any{{
			"urn": urn, "size": len(pdf), "type": "application/pdf",
			"name": "contact-sheet.pdf", "available_until": "2026-12-01T00:00:00Z",
		}},
	}, true)

	// The PDF is small enough for D-139 auto-grant — no deferred
	// accept; the sender's pusher just delivers the bytes.
	w.pushAll()
	if !objectLive(w.node("client.demo"), urn) {
		t.Fatal("the bytes must be in Milan's custody before his node can index them")
	}

	// The word "lisbon" exists only inside the PDF: Milan's node
	// extracted it at the OnVerified moment.
	hits := searchFor(t, milan, "lisbon")
	if len(hits) != 1 || len(hits[0].Matches) != 1 {
		t.Fatalf("media-text search: %+v", hits)
	}
	if m := hits[0].Matches[0]; m.Via != "media" || m.Name != "contact-sheet.pdf" {
		t.Fatalf("the hit must come through the media door: %+v", m)
	}

	// Subject search with folded diacritics: 'nahlady' finds 'náhľady'.
	hits = searchFor(t, milan, "nahlady")
	if len(hits) != 1 || hits[0].Subject != "Svadba — náhľady" {
		t.Fatalf("diacritics-folded subject search: %+v", hits)
	}

	// Petra's own sent copy: her node indexed nothing at upload time
	// (no refs existed yet) — her first search self-heals the gap.
	hits = searchFor(t, petra, "lisbon")
	if len(hits) != 1 || len(hits[0].Matches) != 1 || hits[0].Matches[0].Via != "media" {
		t.Fatalf("sender-side self-heal: %+v", hits)
	}

	// And a miss is an honest empty page, not an error.
	if hits = searchFor(t, milan, "bratislava"); len(hits) != 0 {
		t.Fatalf("miss must be empty: %+v", hits)
	}
}
