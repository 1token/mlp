package bao

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zeebo/blake3"
)

// tv008 is the vector's shape (conformance/vectors/mlp-tv-008.json).
type tv008 struct {
	ChunkGroupBytes int64 `json:"chunk_group_bytes"`
	Content         struct {
		Length    int64  `json:"length"`
		URN       string `json:"urn"`
		Blake3Hex string `json:"blake3_hex"`
	} `json:"content"`
	GroupCVsHex []string `json:"group_cvs_hex"`
	Combined    struct {
		EncodedLength int64  `json:"encoded_length"`
		Blake3Hex     string `json:"blake3_hex"`
	} `json:"combined"`
	Slice struct {
		Offset        int64  `json:"offset"`
		Length        int64  `json:"length"`
		EncodedLength int64  `json:"encoded_length"`
		Blake3Hex     string `json:"blake3_hex"`
	} `json:"slice"`
	CorruptedSlice struct {
		Xor struct {
			Offset int64 `json:"offset"`
			Mask   byte  `json:"mask"`
		} `json:"xor"`
		Expect struct {
			FailingGroup int64 `json:"failing_group"`
		} `json:"expect"`
	} `json:"corrupted_slice"`
}

func loadTV008(t *testing.T) (tv008, []byte) {
	t.Helper()
	raw, err := os.ReadFile("../../conformance/vectors/mlp-tv-008.json")
	if err != nil {
		t.Fatal(err)
	}
	var v tv008
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	content := make([]byte, v.Content.Length)
	for i := range content {
		content[i] = byte((i*7 + 3) & 0xFF) // the vector's rule
	}
	return v, content
}

// TestTV008Encode — the encoder reproduces the vector byte-exactly:
// full 16 KiB groups except the final (M073), the pinned combined
// digest, and a root equal to the URN's digest (M074) — checked here
// against an INDEPENDENT BLAKE3 (zeebo), so the embedded reference
// core cannot agree with itself by accident.
func TestTV008Encode(t *testing.T) {
	v, content := loadTV008(t)
	if v.ChunkGroupBytes != Group {
		t.Fatalf("vector geometry %d, package %d", v.ChunkGroupBytes, Group)
	}
	root := Root(content)
	if hex.EncodeToString(root[:]) != v.Content.Blake3Hex {
		t.Fatalf("reference core root diverges from the vector")
	}
	if got := blake3.Sum256(content); got != root {
		t.Fatalf("reference core diverges from independent BLAKE3 (M074)")
	}
	var enc bytes.Buffer
	if err := Encode(&enc, content); err != nil {
		t.Fatal(err)
	}
	if int64(enc.Len()) != v.Combined.EncodedLength || EncodedSize(v.Content.Length) != v.Combined.EncodedLength {
		t.Fatalf("encoded length %d, vector %d", enc.Len(), v.Combined.EncodedLength)
	}
	if got := blake3.Sum256(enc.Bytes()); hex.EncodeToString(got[:]) != v.Combined.Blake3Hex {
		t.Fatalf("combined encoding does not match the vector's pinned digest")
	}
}

// TestTV008Decode — the decoder verifies the vector's combined form
// end to end, releases exactly the original content through its
// Groups sink, and never releases a byte of an unverified group
// (M075: the sink only ever sees post-verification data by
// construction, asserted via the corrupted stream below).
func TestTV008Decode(t *testing.T) {
	v, content := loadTV008(t)
	var enc bytes.Buffer
	if err := Encode(&enc, content); err != nil {
		t.Fatal(err)
	}
	root := Root(content)
	var out bytes.Buffer
	d := NewDecoder(root, v.Content.Length)
	d.Groups = func(g []byte) error { out.Write(g); return nil }
	// Feed in awkward 1000-byte pieces: PATCH boundaries are arbitrary.
	for b := enc.Bytes(); len(b) > 0; {
		n := min(1000, len(b))
		if _, err := d.Write(b[:n]); err != nil {
			t.Fatal(err)
		}
		b = b[n:]
	}
	if !d.Complete() || d.VerifiedGroups != int64(len(v.GroupCVsHex)) {
		t.Fatalf("decode incomplete: %d groups", d.VerifiedGroups)
	}
	if !bytes.Equal(out.Bytes(), content) {
		t.Fatal("decoded content diverges")
	}
}

// TestTV008CorruptedStream — flip one byte inside a group of the
// combined form: the decoder MUST fail at that group with ErrVerify
// (M055's failing input at the package level) and MUST NOT have
// released any byte of it (M075).
func TestTV008CorruptedStream(t *testing.T) {
	v, content := loadTV008(t)
	var enc bytes.Buffer
	Encode(&enc, content)
	bad := enc.Bytes()
	// Corrupt inside group 2: header 8 + root node 64 + left node 64
	// + groups 0,1 (32768) + right node 64 + 100 into group 2.
	pos := 8 + 64 + 64 + 2*Group + 64 + 100
	bad[pos] ^= 0x01
	root := Root(content)
	released := int64(0)
	d := NewDecoder(root, v.Content.Length)
	d.Groups = func(g []byte) error { released += int64(len(g)); return nil }
	_, err := d.Write(bad)
	if !errors.Is(err, ErrVerify) || !strings.Contains(err.Error(), "group 2") {
		t.Fatalf("want ErrVerify at group 2, got %v", err)
	}
	if released != 2*Group {
		t.Fatalf("released %d bytes; only groups 0 and 1 may be out (M075)", released)
	}
}

// TestDecoderCloneCheckpoint — Clone is the hasher-checkpoint twin:
// a clone taken mid-stream resumes exactly, and the original's
// further consumption never disturbs it.
func TestDecoderCloneCheckpoint(t *testing.T) {
	_, content := loadTV008(t)
	var enc bytes.Buffer
	Encode(&enc, content)
	root := Root(content)
	d := NewDecoder(root, int64(len(content)))
	b := enc.Bytes()
	cut := len(b) / 3
	if _, err := d.Write(b[:cut]); err != nil {
		t.Fatal(err)
	}
	cp := d.Clone()
	if _, err := d.Write(b[cut:]); err != nil { // original runs ahead
		t.Fatal(err)
	}
	if _, err := cp.Write(b[cut:]); err != nil { // the checkpoint resumes
		t.Fatal(err)
	}
	if !cp.Complete() || !d.Complete() {
		t.Fatal("both must complete")
	}
}

// TestEdges — the empty object (8-byte encoding, one empty group)
// and a sub-group object (no parent nodes at all).
func TestEdges(t *testing.T) {
	for _, content := range [][]byte{{}, []byte("tiny")} {
		var enc bytes.Buffer
		if err := Encode(&enc, content); err != nil {
			t.Fatal(err)
		}
		if int64(enc.Len()) != EncodedSize(int64(len(content))) {
			t.Fatalf("len(%d): encoded %d", len(content), enc.Len())
		}
		d := NewDecoder(Root(content), int64(len(content)))
		if _, err := d.Write(enc.Bytes()); err != nil {
			t.Fatal(err)
		}
		if !d.Complete() {
			t.Fatalf("len(%d): incomplete", len(content))
		}
	}
	// A lying header is an immediate verification failure.
	var enc bytes.Buffer
	Encode(&enc, []byte("tiny"))
	d := NewDecoder(Root([]byte("tiny")), 5)
	if _, err := d.Write(enc.Bytes()); !errors.Is(err, ErrVerify) {
		t.Fatalf("lying header must fail: %v", err)
	}
}
