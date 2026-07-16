// Package search is the node-local search layer (S4.19, D-261):
// pluggable text extraction from held media, an FTS4 index over
// medialet text and extracted media text, and the query machinery
// behind GET /api/v1/search. Documents are just a media type — a PDF
// is heavy media the way a RAW file is — so extraction is a registry
// of pluggable Extractors, and everything here is derived data in the
// §11.6 family: rebuildable, never authoritative, never on the wire.
package search

import (
	"strings"
	"unicode/utf8"
)

// Caps keep the index bounded and extraction cheap enough to run
// synchronously at the OnVerified moment in the prototype. Inputs
// larger than MaxInput are extracted from their first MaxInput bytes;
// extracted text is truncated at MaxText bytes on a rune boundary.
const (
	MaxInput = 32 << 20  // bytes of media read per object
	MaxText  = 512 << 10 // bytes of extracted text kept per object
)

// Extractor turns one media format into plain text. Claims decides on
// the declared Manifest type and name (the receiver never sniffs
// content to choose a parser — the declared type is the contract, and
// a mismatch simply yields an extraction error, never a fallback that
// could be steered). Extract receives at most MaxInput bytes.
type Extractor interface {
	Name() string
	Claims(mediaType, name string) bool
	Extract(data []byte) (string, error)
}

// Builtin is the default registry, in claim order. All built-ins are
// stdlib-only by design (D-pending: no extraction dependencies in the
// prototype; production deployments swap in real parsers behind the
// same interface).
func Builtin() []Extractor {
	return []Extractor{docxExtractor{}, xlsxExtractor{}, pdfExtractor{}, textExtractor{}}
}

// textExtractor passes plain-text families through.
type textExtractor struct{}

func (textExtractor) Name() string { return "text" }

func (textExtractor) Claims(mediaType, name string) bool {
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	for _, suf := range []string{".txt", ".md", ".csv"} {
		if strings.HasSuffix(strings.ToLower(name), suf) {
			return true
		}
	}
	return false
}

func (textExtractor) Extract(data []byte) (string, error) {
	return clean(string(data)), nil
}

// clean enforces valid UTF-8, strips NULs, collapses runs of blank
// lines, and applies the MaxText cap on a rune boundary.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	s = strings.ReplaceAll(s, "\x00", "")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, strings.TrimRight(l, " \t\r"))
	}
	s = strings.TrimSpace(strings.Join(out, "\n"))
	if len(s) > MaxText {
		s = s[:MaxText]
		for len(s) > 0 && !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}
