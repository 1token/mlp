package search

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"strings"
	"unicode"
	"unicode/utf16"
)

// pdfExtractor is a deliberately minimal, prototype-grade PDF text
// extractor: it scans for stream objects, inflates FlateDecode
// streams (the overwhelmingly common filter), and walks content
// streams for the text-showing operators Tj, ', ", and TJ, decoding
// literal and hex strings.
//
// Documented limits (a production deployment swaps in a real parser
// behind the Extractor interface): no encryption, no LZW/DCT/other
// filters, and — most importantly — no ToUnicode CMaps, so text set
// in subset-embedded fonts with custom encodings comes out garbled;
// a printability filter drops such runs rather than indexing noise.
type pdfExtractor struct{}

func (pdfExtractor) Name() string { return "pdf" }

func (pdfExtractor) Claims(mediaType, name string) bool {
	return mediaType == "application/pdf" || strings.HasSuffix(strings.ToLower(name), ".pdf")
}

func (pdfExtractor) Extract(data []byte) (string, error) {
	var b strings.Builder
	for _, raw := range pdfStreams(data) {
		content := raw
		if inflated, ok := inflate(raw); ok {
			content = inflated
		}
		if !bytes.Contains(content, []byte("BT")) {
			continue // not a text-bearing content stream
		}
		extractContentText(content, &b)
		if b.Len() > MaxText {
			break
		}
	}
	return clean(b.String()), nil
}

// pdfStreams returns the byte regions between stream/endstream pairs.
func pdfStreams(data []byte) [][]byte {
	var out [][]byte
	rest := data
	for {
		i := bytes.Index(rest, []byte("stream"))
		if i < 0 {
			return out
		}
		body := rest[i+len("stream"):]
		// The keyword is followed by CRLF or LF (§7.3.8 of ISO 32000).
		if len(body) > 0 && body[0] == '\r' {
			body = body[1:]
		}
		if len(body) > 0 && body[0] == '\n' {
			body = body[1:]
		}
		j := bytes.Index(body, []byte("endstream"))
		if j < 0 {
			return out
		}
		out = append(out, body[:j])
		rest = body[j+len("endstream"):]
	}
}

func inflate(raw []byte) ([]byte, bool) {
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, MaxInput))
	if err != nil || len(out) == 0 {
		return nil, false
	}
	return out, true
}

// extractContentText walks one content stream. It is a scanner, not a
// full parser: strings are captured as they appear; when a
// text-showing operator follows, the pending strings are emitted.
func extractContentText(cs []byte, b *strings.Builder) {
	var pending []string
	emit := func() {
		joined := strings.Join(pending, "")
		pending = pending[:0]
		if printable(joined) {
			b.WriteString(joined)
			b.WriteByte(' ')
		}
	}
	i := 0
	for i < len(cs) {
		switch c := cs[i]; {
		case c == '(':
			s, next := pdfLiteral(cs, i)
			pending = append(pending, s)
			i = next
		case c == '<' && i+1 < len(cs) && cs[i+1] != '<':
			s, next := pdfHex(cs, i)
			pending = append(pending, s)
			i = next
		case c == '%': // comment to end of line
			for i < len(cs) && cs[i] != '\n' {
				i++
			}
		case (c == '-' || c >= '0' && c <= '9') && len(pending) > 0:
			// A number between TJ array strings adjusts spacing; a
			// large negative adjustment is the word-gap convention.
			v, next := pdfNumber(cs, i)
			if v <= -180 {
				pending = append(pending, " ")
			}
			i = next
		default:
			if op, next, ok := pdfOperator(cs, i); ok {
				switch op {
				case "Tj", "'", "\"", "TJ":
					emit()
				case "Td", "TD", "T*", "ET":
					pending = pending[:0]
					b.WriteByte('\n')
				}
				i = next
			} else {
				i++
			}
		}
	}
}

// pdfOperator reads an operator or other token starting at i; returns
// ok only for the operators the scanner cares about.
func pdfOperator(cs []byte, i int) (string, int, bool) {
	for _, op := range [...]string{"Tj", "TJ", "TD", "Td", "T*", "ET", "'", "\""} {
		if bytes.HasPrefix(cs[i:], []byte(op)) {
			return op, i + len(op), true
		}
	}
	return "", i, false
}

// pdfLiteral decodes a (literal) string with escapes and balanced
// parentheses, returning the text and the index after the closer.
func pdfLiteral(cs []byte, i int) (string, int) {
	var out []byte
	depth := 0
	for ; i < len(cs); i++ {
		c := cs[i]
		switch {
		case c == '\\' && i+1 < len(cs):
			i++
			switch e := cs[i]; e {
			case 'n':
				out = append(out, '\n')
			case 'r', 't', 'b', 'f':
				out = append(out, ' ')
			case '(', ')', '\\':
				out = append(out, e)
			default:
				if e >= '0' && e <= '7' { // octal, up to 3 digits
					v := int(e - '0')
					for d := 0; d < 2 && i+1 < len(cs) && cs[i+1] >= '0' && cs[i+1] <= '7'; d++ {
						i++
						v = v*8 + int(cs[i]-'0')
					}
					out = append(out, byte(v))
				}
			}
		case c == '(':
			depth++
			if depth > 1 {
				out = append(out, c)
			}
		case c == ')':
			depth--
			if depth == 0 {
				return decodeBytes(out), i + 1
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return decodeBytes(out), i
}

// pdfHex decodes a <hex> string.
func pdfHex(cs []byte, i int) (string, int) {
	j := bytes.IndexByte(cs[i:], '>')
	if j < 0 {
		return "", len(cs)
	}
	hexPart := cs[i+1 : i+j]
	var raw []byte
	var hi byte = 0xFF
	for _, c := range hexPart {
		v, ok := hexVal(c)
		if !ok {
			continue
		}
		if hi == 0xFF {
			hi = v
		} else {
			raw = append(raw, hi<<4|v)
			hi = 0xFF
		}
	}
	if hi != 0xFF { // odd count: final digit padded with 0 (§7.3.4.3)
		raw = append(raw, hi<<4)
	}
	return decodeBytes(raw), i + j + 1
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// decodeBytes maps string bytes to text: UTF-16BE when BOM-marked,
// Latin-1 otherwise (a fair stand-in for the standard encodings).
func decodeBytes(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		u := make([]uint16, 0, len(raw)/2)
		for k := 2; k+1 < len(raw); k += 2 {
			u = append(u, binary.BigEndian.Uint16(raw[k:]))
		}
		return string(utf16.Decode(u))
	}
	rs := make([]rune, len(raw))
	for k, c := range raw {
		rs[k] = rune(c)
	}
	return string(rs)
}

// printable rejects runs that are mostly non-text — the signature of
// CID-keyed subset fonts this extractor cannot decode.
func printable(s string) bool {
	if s == "" {
		return false
	}
	good, total := 0, 0
	for _, r := range s {
		total++
		if unicode.IsPrint(r) || r == '\n' {
			good++
		}
	}
	return good*10 >= total*8 // ≥80% printable
}

// pdfNumber parses a signed decimal starting at i.
func pdfNumber(cs []byte, i int) (float64, int) {
	j := i
	if cs[j] == '-' {
		j++
	}
	for j < len(cs) && (cs[j] >= '0' && cs[j] <= '9' || cs[j] == '.') {
		j++
	}
	var v float64
	neg := cs[i] == '-'
	frac, div := false, 1.0
	for k := i; k < j; k++ {
		switch {
		case cs[k] == '-':
		case cs[k] == '.':
			frac = true
		case frac:
			div *= 10
			v += float64(cs[k]-'0') / div
		default:
			v = v*10 + float64(cs[k]-'0')
		}
	}
	if neg {
		v = -v
	}
	return v, j
}
