// Package bao implements the application/mlp-bao encoding (core
// draft-03 §8.9, Annex D; MEP-003, D-271): BLAKE3 verified streaming
// over MLP's existing urn:mlet: identifiers, profiled to 16 KiB chunk
// groups.
//
// The package embeds a minimal reference BLAKE3 core rather than
// taking a dependency: hash libraries expose no interior chaining
// values, and the reference compression is ~100 lines whose
// correctness TV-008 judges byte-exactly (the same posture as the
// vector generator's embedded Python core, D-277). Throughput is
// prototype-grade; a production deployment swaps the core, not the
// tree.
package bao

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Group is the Annex D.1 geometry: 16 KiB, 16 BLAKE3 chunks. Frozen
// spec-wide (D-273), never negotiated.
const Group = 16384

const chunkSize = 1024

// EncodedSize is the Annex D.3 formula: header + one 64-byte parent
// node per group beyond the first + the content itself.
func EncodedSize(contentLen int64) int64 {
	return 8 + 64*(numGroups(contentLen)-1) + contentLen
}

func numGroups(contentLen int64) int64 {
	if contentLen == 0 {
		return 1 // the empty object is one empty group (D.1)
	}
	return (contentLen + Group - 1) / Group
}

// --- the reference BLAKE3 core ---------------------------------------

var iv = [8]uint32{0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
	0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19}

var msgPerm = [16]int{2, 6, 3, 10, 7, 0, 4, 13, 1, 11, 12, 5, 9, 14, 15, 8}

const (
	flagChunkStart = 1
	flagChunkEnd   = 2
	flagParent     = 4
	flagRoot       = 8
)

type cv = [8]uint32

func g(s *[16]uint32, a, b, c, d int, mx, my uint32) {
	s[a] += s[b] + mx
	s[d] = rotr(s[d]^s[a], 16)
	s[c] += s[d]
	s[b] = rotr(s[b]^s[c], 12)
	s[a] += s[b] + my
	s[d] = rotr(s[d]^s[a], 8)
	s[c] += s[d]
	s[b] = rotr(s[b]^s[c], 7)
}

func rotr(x uint32, n uint) uint32 { return x>>n | x<<(32-n) }

func compress(h cv, block [16]uint32, counter uint64, blockLen uint32, flags uint32) cv {
	s := [16]uint32{h[0], h[1], h[2], h[3], h[4], h[5], h[6], h[7],
		iv[0], iv[1], iv[2], iv[3],
		uint32(counter), uint32(counter >> 32), blockLen, flags}
	m := block
	for r := 0; r < 7; r++ {
		g(&s, 0, 4, 8, 12, m[0], m[1])
		g(&s, 1, 5, 9, 13, m[2], m[3])
		g(&s, 2, 6, 10, 14, m[4], m[5])
		g(&s, 3, 7, 11, 15, m[6], m[7])
		g(&s, 0, 5, 10, 15, m[8], m[9])
		g(&s, 1, 6, 11, 12, m[10], m[11])
		g(&s, 2, 7, 8, 13, m[12], m[13])
		g(&s, 3, 4, 9, 14, m[14], m[15])
		if r < 6 {
			var p [16]uint32
			for i, j := range msgPerm {
				p[i] = m[j]
			}
			m = p
		}
	}
	var out cv
	for i := 0; i < 8; i++ {
		out[i] = s[i] ^ s[i+8]
	}
	return out
}

func blockWords(b []byte) [16]uint32 {
	var padded [64]byte
	copy(padded[:], b)
	var w [16]uint32
	for i := range w {
		w[i] = binary.LittleEndian.Uint32(padded[i*4:])
	}
	return w
}

// chunkCV hashes one <=1024-byte chunk at its global counter.
func chunkCV(data []byte, counter uint64, root bool) cv {
	h := iv
	n := (len(data) + 63) / 64
	if n == 0 {
		n = 1
	}
	for i := 0; i < n; i++ {
		blk := data[i*64 : min(len(data), (i+1)*64)]
		flags := uint32(0)
		if i == 0 {
			flags |= flagChunkStart
		}
		if i == n-1 {
			flags |= flagChunkEnd
			if root {
				flags |= flagRoot
			}
		}
		h = compress(h, blockWords(blk), counter, uint32(len(blk)), flags)
	}
	return h
}

func parentCV(left, right cv, root bool) cv {
	var block [16]uint32
	copy(block[:8], left[:])
	copy(block[8:], right[:])
	flags := uint32(flagParent)
	if root {
		flags |= flagRoot
	}
	return compress(iv, block, 0, 64, flags)
}

// subtreeCV is the CV over data whose first chunk sits at the given
// global chunk counter; standard shape (left = largest power of two
// of chunks strictly below the total).
func subtreeCV(data []byte, firstChunk uint64, root bool) cv {
	nChunks := (int64(len(data)) + chunkSize - 1) / chunkSize
	if nChunks <= 1 {
		return chunkCV(data, firstChunk, root)
	}
	left := int64(1)
	for left*2 < nChunks {
		left *= 2
	}
	split := left * chunkSize
	l := subtreeCV(data[:split], firstChunk, false)
	r := subtreeCV(data[split:], firstChunk+uint64(left), false)
	return parentCV(l, r, root)
}

func cvBytes(c cv) [32]byte {
	var out [32]byte
	for i, w := range c {
		binary.LittleEndian.PutUint32(out[i*4:], w)
	}
	return out
}

func cvFrom(b []byte) cv {
	var c cv
	for i := range c {
		c[i] = binary.LittleEndian.Uint32(b[i*4:])
	}
	return c
}

// Root computes the root-finalized BLAKE3 digest of content — usable
// as an independent URN check in tests.
func Root(content []byte) [32]byte {
	return cvBytes(subtreeCV(content, 0, true))
}

// --- encoder ---------------------------------------------------------

// Encode writes the Annex D.3 combined form of content to w.
func Encode(w io.Writer, content []byte) error {
	var hdr [8]byte
	binary.LittleEndian.PutUint64(hdr[:], uint64(len(content)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	return encodeTree(w, content, 0)
}

func encodeTree(w io.Writer, data []byte, firstChunk uint64) error {
	if int64(len(data)) <= Group {
		_, err := w.Write(data)
		return err
	}
	nGroups := numGroups(int64(len(data)))
	left := int64(1)
	for left*2 < nGroups {
		left *= 2
	}
	split := left * Group
	l := cvBytes(subtreeCV(data[:split], firstChunk, false))
	r := cvBytes(subtreeCV(data[split:], firstChunk+uint64(left*Group/chunkSize), false))
	if _, err := w.Write(append(l[:], r[:]...)); err != nil {
		return err
	}
	if err := encodeTree(w, data[:split], firstChunk); err != nil {
		return err
	}
	return encodeTree(w, data[split:], firstChunk+uint64(left*Group/chunkSize))
}

// --- incremental verifying decoder -----------------------------------

// ErrVerify is the Annex D verification failure — §8.9 maps it to
// 422 bao-verify-failed with reset-to-zero (the source-wrong taxon).
var ErrVerify = errors.New("bao-verify-failed")

type span struct {
	expect  cv
	loGroup int64
	hiGroup int64 // exclusive
	root    bool
}

// Decoder consumes the combined form incrementally, verifying every
// parent node and chunk group the moment it is complete (Annex D.3:
// nothing unverified is ever released). Its state is small and
// re-derivable from any verified prefix, mirroring the §8.4 hasher
// checkpoint discipline.
type Decoder struct {
	root       cv
	contentLen int64 // declared by the Reservation; the header must agree
	gotHeader  bool
	stack      []span
	buf        []byte
	consumed   int64
	// Groups, when non-nil, receives each verified group's bytes in
	// order — the finalize-time decode-to-file sink.
	Groups func(data []byte) error
	// VerifiedGroups counts groups accepted so far.
	VerifiedGroups int64
}

// NewDecoder verifies against root (the URN digest) for an object of
// the declared content length.
func NewDecoder(root [32]byte, contentLen int64) *Decoder {
	d := &Decoder{root: cvFrom(root[:]), contentLen: contentLen}
	d.stack = []span{{expect: d.root, loGroup: 0, hiGroup: numGroups(contentLen), root: true}}
	return d
}

// Clone snapshots the decoder — the checkpoint twin of hasher.Clone.
func (d *Decoder) Clone() *Decoder {
	c := *d
	c.stack = append([]span(nil), d.stack...)
	c.buf = append([]byte(nil), d.buf...)
	c.Groups = nil // sinks do not survive cloning
	return &c
}

// Complete reports whether the whole encoded stream has verified.
func (d *Decoder) Complete() bool {
	return d.gotHeader && len(d.stack) == 0 && len(d.buf) == 0
}

func (d *Decoder) groupSize(g int64) int64 {
	if d.contentLen == 0 {
		return 0
	}
	last := numGroups(d.contentLen) - 1
	if g < last {
		return Group
	}
	return d.contentLen - last*Group
}

// Write feeds encoded bytes. On a verification failure it returns
// ErrVerify (wrapped with the failing position); transport-level
// callers translate that to 422 bao-verify-failed.
func (d *Decoder) Write(p []byte) (int, error) {
	total := len(p)
	for {
		if !d.gotHeader {
			if len(p) == 0 {
				break
			}
			take := min(8-len(d.buf), len(p))
			d.buf = append(d.buf, p[:take]...)
			p = p[take:]
			if len(d.buf) < 8 {
				continue
			}
			declared := int64(binary.LittleEndian.Uint64(d.buf))
			if declared != d.contentLen {
				return total - len(p), fmt.Errorf("%w: header declares %d, reservation %d", ErrVerify, declared, d.contentLen)
			}
			d.gotHeader, d.buf = true, d.buf[:0]
			continue
		}
		if len(d.stack) == 0 {
			if len(p) == 0 {
				break
			}
			return total - len(p), fmt.Errorf("%w: bytes beyond the encoded stream", ErrVerify)
		}
		top := d.stack[len(d.stack)-1]
		var need int64
		if top.hiGroup-top.loGroup == 1 {
			need = d.groupSize(top.loGroup)
		} else {
			need = 64
		}
		take := min64(need-int64(len(d.buf)), int64(len(p)))
		d.buf = append(d.buf, p[:take]...)
		p = p[take:]
		if int64(len(d.buf)) < need {
			break // need more input for the current item
		}
		item := d.buf
		d.buf = nil
		d.stack = d.stack[:len(d.stack)-1]
		if top.hiGroup-top.loGroup == 1 {
			got := subtreeCV(item, uint64(top.loGroup*Group/chunkSize), top.root)
			if got != top.expect {
				return total - len(p), fmt.Errorf("%w: group %d", ErrVerify, top.loGroup)
			}
			d.VerifiedGroups++
			if d.Groups != nil {
				if err := d.Groups(item); err != nil {
					return total - len(p), err
				}
			}
		} else {
			l, r := cvFrom(item[:32]), cvFrom(item[32:])
			if parentCV(l, r, top.root) != top.expect {
				return total - len(p), fmt.Errorf("%w: parent covering groups %d-%d", ErrVerify, top.loGroup, top.hiGroup-1)
			}
			left := int64(1)
			for left*2 < top.hiGroup-top.loGroup {
				left *= 2
			}
			// Right pushed first so the left pops first (pre-order).
			d.stack = append(d.stack,
				span{expect: r, loGroup: top.loGroup + left, hiGroup: top.hiGroup},
				span{expect: l, loGroup: top.loGroup, hiGroup: top.loGroup + left})
		}
	}
	d.consumed += int64(total)
	return total, nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
