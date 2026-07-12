// Package webauthn implements the D-161 passkey ceremonies without
// dependencies: registration with "none" attestation and assertion
// login, over a deliberately minimal CBOR codec that decodes exactly
// the shapes WebAuthn uses — unsigned/negative integers, byte and
// text strings, arrays, and maps with integer or text keys. Anything
// else is an error: strictness is the feature.
package webauthn

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
)

var errCBOR = errors.New("webauthn: malformed CBOR")

type cborDecoder struct {
	b   []byte
	off int
}

func (d *cborDecoder) byte() (byte, error) {
	if d.off >= len(d.b) {
		return 0, errCBOR
	}
	v := d.b[d.off]
	d.off++
	return v, nil
}

func (d *cborDecoder) take(n int) ([]byte, error) {
	if n < 0 || d.off+n > len(d.b) {
		return nil, errCBOR
	}
	v := d.b[d.off : d.off+n]
	d.off += n
	return v, nil
}

// head reads a major type and its argument (no indefinite lengths —
// WebAuthn encoders do not emit them, and we refuse them).
func (d *cborDecoder) head() (major byte, arg uint64, err error) {
	ib, err := d.byte()
	if err != nil {
		return 0, 0, err
	}
	major = ib >> 5
	ai := ib & 0x1f
	switch {
	case ai < 24:
		return major, uint64(ai), nil
	case ai == 24:
		b, err := d.byte()
		return major, uint64(b), err
	case ai == 25:
		b, err := d.take(2)
		if err != nil {
			return 0, 0, err
		}
		return major, uint64(binary.BigEndian.Uint16(b)), nil
	case ai == 26:
		b, err := d.take(4)
		if err != nil {
			return 0, 0, err
		}
		return major, uint64(binary.BigEndian.Uint32(b)), nil
	case ai == 27:
		b, err := d.take(8)
		if err != nil {
			return 0, 0, err
		}
		return major, binary.BigEndian.Uint64(b), nil
	default:
		return 0, 0, errCBOR // indefinite or reserved
	}
}

// value decodes one item. Integers come back as int64, strings as
// string, byte strings as []byte, arrays as []any, maps as
// map[any]any with int64 or string keys.
func (d *cborDecoder) value(depth int) (any, error) {
	if depth > 8 {
		return nil, errCBOR
	}
	major, arg, err := d.head()
	if err != nil {
		return nil, err
	}
	switch major {
	case 0: // unsigned
		if arg > math.MaxInt64 {
			return nil, errCBOR
		}
		return int64(arg), nil
	case 1: // negative: -1 - arg
		if arg > math.MaxInt64-1 {
			return nil, errCBOR
		}
		return -1 - int64(arg), nil
	case 2: // byte string
		b, err := d.take(int(arg))
		if err != nil {
			return nil, err
		}
		out := make([]byte, len(b))
		copy(out, b)
		return out, nil
	case 3: // text string
		b, err := d.take(int(arg))
		if err != nil {
			return nil, err
		}
		return string(b), nil
	case 4: // array
		if arg > 64 {
			return nil, errCBOR
		}
		out := make([]any, 0, arg)
		for i := uint64(0); i < arg; i++ {
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case 5: // map
		if arg > 64 {
			return nil, errCBOR
		}
		out := make(map[any]any, arg)
		for i := uint64(0); i < arg; i++ {
			k, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			switch k.(type) {
			case int64, string:
			default:
				return nil, errCBOR // only the WebAuthn key kinds
			}
			if _, dup := out[k]; dup {
				return nil, errCBOR
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			out[k] = v
		}
		return out, nil
	default:
		return nil, fmt.Errorf("webauthn: unsupported CBOR major type %d", major)
	}
}

// decodeCBOR decodes one item and reports how many bytes it consumed
// (the COSE key inside authData is length-delimited only by its own
// encoding).
func decodeCBOR(b []byte) (any, int, error) {
	d := &cborDecoder{b: b}
	v, err := d.value(0)
	if err != nil {
		return nil, 0, err
	}
	return v, d.off, nil
}

// decodeCBORExact requires the item to consume the whole input.
func decodeCBORExact(b []byte) (any, error) {
	v, n, err := decodeCBOR(b)
	if err != nil {
		return nil, err
	}
	if n != len(b) {
		return nil, errors.New("webauthn: trailing bytes after CBOR item")
	}
	return v, nil
}

// --- encoding (tests synthesize authenticators; production only
// --- decodes) ----------------------------------------------------------

func appendCBORHead(out []byte, major byte, arg uint64) []byte {
	switch {
	case arg < 24:
		return append(out, major<<5|byte(arg))
	case arg <= 0xff:
		return append(out, major<<5|24, byte(arg))
	case arg <= 0xffff:
		out = append(out, major<<5|25)
		return binary.BigEndian.AppendUint16(out, uint16(arg))
	case arg <= 0xffffffff:
		out = append(out, major<<5|26)
		return binary.BigEndian.AppendUint32(out, uint32(arg))
	default:
		out = append(out, major<<5|27)
		return binary.BigEndian.AppendUint64(out, arg)
	}
}

// EncodeCBOR encodes the subset the decoder accepts, with canonical
// (sorted, shortest-head) map keys — enough to build test vectors.
func EncodeCBOR(v any) []byte {
	return appendCBOR(nil, v)
}

func appendCBOR(out []byte, v any) []byte {
	switch x := v.(type) {
	case int64:
		if x >= 0 {
			return appendCBORHead(out, 0, uint64(x))
		}
		return appendCBORHead(out, 1, uint64(-1-x))
	case int:
		return appendCBOR(out, int64(x))
	case []byte:
		out = appendCBORHead(out, 2, uint64(len(x)))
		return append(out, x...)
	case string:
		out = appendCBORHead(out, 3, uint64(len(x)))
		return append(out, x...)
	case []any:
		out = appendCBORHead(out, 4, uint64(len(x)))
		for _, e := range x {
			out = appendCBOR(out, e)
		}
		return out
	case map[any]any:
		out = appendCBORHead(out, 5, uint64(len(x)))
		keys := make([]any, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j])
		})
		for _, k := range keys {
			out = appendCBOR(out, k)
			out = appendCBOR(out, x[k])
		}
		return out
	default:
		panic(fmt.Sprintf("webauthn: cannot encode %T", v))
	}
}
