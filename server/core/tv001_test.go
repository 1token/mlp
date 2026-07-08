package core

// TV-001 conformance: this test recomputes every derived value in
// conformance/vectors/mlp-tv-001.json from first principles — key
// encodings and kids from the RFC 8032 seeds, the media URN, both
// JCS signing inputs byte-for-byte, both Ed25519 signatures (RFC 8032
// signing is deterministic), the content address of the Signed
// Medialet, the canonical envelope size, and even the deterministic
// UUIDv7 identifiers — and requires equality with the committed
// vector. Green here is the D-190 acceptance gate for this package.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/zeebo/blake3"
)

func loadVector(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../../conformance/vectors/mlp-tv-001.json")
	if err != nil {
		t.Fatalf("vector: %v", err)
	}
	v, err := ParseDialect(raw)
	if err != nil {
		t.Fatalf("vector parse: %v", err)
	}
	return v.(map[string]any)
}

func at(v any, path ...string) any {
	for _, k := range path {
		v = v.(map[string]any)[k]
	}
	return v
}
func str(v any, path ...string) string { return at(v, path...).(string) }

func keyFromSeed(t *testing.T, seedHex string) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return priv, priv.Public().(ed25519.PublicKey)
}

// uuid7 mirrors the vector generators' deterministic UUIDv7
// construction (48-bit ms timestamp, version/variant bits, BLAKE3 of
// the label filling the random fields).
func uuid7(ts time.Time, label string) string {
	ms := ts.UnixMilli()
	r := blake3.Sum256([]byte(label))
	var b [16]byte
	b[0], b[1], b[2] = byte(ms>>40), byte(ms>>32), byte(ms>>24)
	b[3], b[4], b[5] = byte(ms>>16), byte(ms>>8), byte(ms)
	b[6] = 0x70 | (r[0] & 0x0F)
	b[7] = r[1]
	b[8] = 0x80 | (r[2] & 0x3F)
	copy(b[9:], r[3:10])
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func TestTV001(t *testing.T) {
	v := loadVector(t)

	// --- keys: encodings and kids from the RFC 8032 seeds (D-61/D-62)
	privA, pubA := keyFromSeed(t, str(v, "keys", "author", "seed_hex"))
	privS, pubS := keyFromSeed(t, str(v, "keys", "sn", "seed_hex"))
	if hex.EncodeToString(pubA) != str(v, "keys", "author", "public_hex") {
		t.Fatal("author public key mismatch")
	}
	if KeyField(pubA) != str(v, "keys", "author", "key") {
		t.Fatal("author key field (D-61) mismatch")
	}
	if KID(pubA) != str(v, "keys", "author", "kid") {
		t.Fatal("author kid (D-62) mismatch")
	}
	if KeyField(pubS) != str(v, "keys", "sn", "key") || KID(pubS) != str(v, "keys", "sn", "kid") {
		t.Fatal("sn key/kid mismatch")
	}
	if err := VerifyKID(str(v, "keys", "author", "kid"), str(v, "keys", "author", "key")); err != nil {
		t.Fatalf("kid self-verification: %v", err)
	}

	// --- media object: content address (D-25)
	media := []byte(str(v, "media", "bytes_utf8"))
	if URNMlet(media) != str(v, "media", "urn") {
		t.Fatal("media URN mismatch")
	}
	if err := VerifyContent(str(v, "media", "urn"), media); err != nil {
		t.Fatalf("content verify: %v", err)
	}

	// --- author signature: input bytes, deterministic value, verify
	medialet := at(v, "signed_medialet", "medialet")
	sigA := at(v, "signed_medialet", "signature").(map[string]any)
	protA := sigA["protected"].(map[string]any)
	gotSigA, inputA, err := SignDoc(privA, LabelAuthor,
		protA["kid"].(string), protA["created"].(string), medialet)
	if err != nil {
		t.Fatalf("SignDoc author: %v", err)
	}
	if string(inputA) != str(v, "author_sig_input_jcs") {
		t.Fatal("author signing input (JCS) mismatch")
	}
	if gotSigA["value"] != sigA["value"] {
		t.Fatal("author signature value mismatch")
	}
	if err := VerifyDoc(pubA, LabelAuthor, medialet, sigA); err != nil {
		t.Fatalf("author verify: %v", err)
	}
	// context-match rule (D-64): the same signature under the wrong label fails
	if err := VerifyDoc(pubA, LabelHop, medialet, sigA); err == nil {
		t.Fatal("label context mismatch not detected")
	}

	// --- content address of the Signed Medialet (§3.3.3)
	smCanon, err := CanonicalizeValue(at(v, "signed_medialet"))
	if err != nil {
		t.Fatal(err)
	}
	if URNMlet(smCanon) != str(v, "signed_medialet_content_address") {
		t.Fatal("signed-medialet content address mismatch")
	}

	// --- hop signature over the envelope
	envelope := at(v, "signed_envelope", "envelope")
	sigH := at(v, "signed_envelope", "signature").(map[string]any)
	protH := sigH["protected"].(map[string]any)
	gotSigH, _, err := SignDoc(privS, LabelHop,
		protH["kid"].(string), protH["created"].(string), envelope)
	if err != nil {
		t.Fatalf("SignDoc hop: %v", err)
	}
	if gotSigH["value"] != sigH["value"] {
		t.Fatal("hop signature value mismatch")
	}
	if err := VerifyDoc(pubS, LabelHop, envelope, sigH); err != nil {
		t.Fatalf("hop verify: %v", err)
	}

	// --- canonical envelope size (D-20 accounting basis)
	seCanon, err := CanonicalizeValue(at(v, "signed_envelope"))
	if err != nil {
		t.Fatal(err)
	}
	wantSize, _ := at(v, "signed_envelope_canonical_size").(json.Number).Int64()
	if int64(len(seCanon)) != wantSize {
		t.Fatalf("envelope canonical size: got %d want %d", len(seCanon), wantSize)
	}

	// --- deterministic UUIDv7 identifiers
	t0 := time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 7, 4, 10, 0, 5, 0, time.UTC)
	if uuid7(t0, "TV-001 medialet id") != str(v, "signed_medialet", "medialet", "id") {
		t.Fatal("medialet id (uuid7) mismatch")
	}
	if uuid7(t1, "TV-001 envelope id") != str(v, "signed_envelope", "envelope", "envelope_id") {
		t.Fatal("envelope id (uuid7) mismatch")
	}

	// --- domain document fixture keys agree with derivations
	for _, k := range at(v, "domain_document", "keys").([]any) {
		entry := k.(map[string]any)
		if err := VerifyKID(entry["kid"].(string), entry["key"].(string)); err != nil {
			t.Fatalf("domain document kid: %v", err)
		}
	}
}

func TestDialectRejections(t *testing.T) {
	if _, err := Canonicalize([]byte(`{"a": 1.5}`)); err == nil {
		t.Fatal("float accepted")
	}
	if _, err := Canonicalize([]byte(`{"a": 9007199254740992}`)); err == nil {
		t.Fatal("2^53 accepted")
	}
	if _, err := Canonicalize([]byte(`{"a": 9007199254740991}`)); err != nil {
		t.Fatalf("2^53-1 rejected: %v", err)
	}
	out, err := Canonicalize([]byte("{\"b\":\"Nov\u00e1k \\u0007\",\"a\":[true,null,-0]}"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":[true,null,0],\"b\":\"Nov\u00e1k \\u0007\"}"
	if string(out) != want {
		t.Fatalf("canonical form: got %s want %s", out, want)
	}
}
