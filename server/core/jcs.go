// Package core implements the MLP cryptographic and serialization
// primitives: RFC 8785 (JCS) canonicalization restricted to the MLP
// JSON dialect (D-43), the multiformats identifier family (D-25, D-61,
// D-62), and the document-signature construction of spec §6.4 (D-44,
// D-64).
//
// Conformance anchor: every function here is exercised against test
// vector TV-001 (conformance/vectors/mlp-tv-001.json); the test suite
// recomputes every derived value in the vector and requires byte
// equality.
package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// ErrDialect reports a document that is valid JSON but violates the
// MLP dialect (D-43): non-integer numbers, or integers outside the
// IEEE-754 exact range |n| <= 2^53-1. Dialect violations are errors,
// never coercions.
var ErrDialect = errors.New("mlp/core: JSON outside the D-43 dialect")

const maxSafeInt = int64(1)<<53 - 1 // D-43 rule 1

// Canonicalize parses a JSON document and returns its RFC 8785
// canonical form under the MLP dialect.
func Canonicalize(doc []byte) ([]byte, error) {
	v, err := ParseDialect(doc)
	if err != nil {
		return nil, err
	}
	return CanonicalizeValue(v)
}

// ParseDialect decodes JSON preserving number tokens (json.Number) so
// dialect validation can inspect them; rejects trailing data.
func ParseDialect(doc []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(doc)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("mlp/core: parse: %w", err)
	}
	if dec.More() {
		return nil, errors.New("mlp/core: trailing data after JSON document")
	}
	return v, nil
}

// CanonicalizeValue canonicalizes an already-parsed JSON value tree
// (map[string]any, []any, string, json.Number, bool, nil; native Go
// integer types are accepted for construction convenience).
func CanonicalizeValue(v any) ([]byte, error) {
	var b strings.Builder
	if err := writeCanonical(&b, v); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

func writeCanonical(b *strings.Builder, v any) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		writeJCSString(b, t)
	case json.Number:
		return writeJCSNumber(b, string(t))
	case int:
		return writeJCSNumber(b, strconv.FormatInt(int64(t), 10))
	case int64:
		return writeJCSNumber(b, strconv.FormatInt(t, 10))
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		// RFC 8785: property names sort by UTF-16 code units.
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJCSString(b, k)
			b.WriteByte(':')
			if err := writeCanonical(b, t[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("mlp/core: unsupported value type %T", v)
	}
	return nil
}

// writeJCSString serializes a string per RFC 8785 §3.2.2.2: minimal
// escaping — `"` and `\`, the two-character forms \b \t \n \f \r for
// their control characters, \u00xx (lowercase hex) for the remaining
// controls, and literal UTF-8 for everything else.
func writeJCSString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// writeJCSNumber enforces the D-43 dialect: the token must be an
// integer literal within the IEEE-754 exact range. RFC 8785's hard
// number-serialization cases are thereby absent by construction —
// the dialect rule is the mitigation the spec records (§12.8).
func writeJCSNumber(b *strings.Builder, tok string) error {
	if !integerToken(tok) {
		return fmt.Errorf("%w: non-integer number %q", ErrDialect, tok)
	}
	n, err := strconv.ParseInt(tok, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %q out of int64 range", ErrDialect, tok)
	}
	if n > maxSafeInt || n < -maxSafeInt {
		return fmt.Errorf("%w: %d outside |n| <= 2^53-1", ErrDialect, n)
	}
	if n == 0 { // ES canonical form of -0 is "0"
		b.WriteByte('0')
		return nil
	}
	b.WriteString(strconv.FormatInt(n, 10))
	return nil
}

func integerToken(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '-' {
		i++
	}
	if i >= len(s) {
		return false
	}
	if s[i] == '0' {
		return i == len(s)-1 // "0" or "-0" only
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func utf16Less(a, b string) bool {
	ua := utf16.Encode([]rune(a))
	ub := utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}
