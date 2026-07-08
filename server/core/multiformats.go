package core

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	"github.com/zeebo/blake3"
)

// Multibase base32-lower without padding — the only base MLP permits
// (D-25, §14.2 multiformats profile).
var b32l = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

var (
	mhPrefixBlake3 = []byte{0x1E, 0x20} // multihash: blake3, 32-byte digest
	mcEd25519Pub   = []byte{0xED, 0x01} // multicodec: ed25519-pub
)

const urnPrefix = "urn:mlet:b"

// MultihashBlake3 returns the multihash (0x1e 0x20 || digest) of data
// under BLAKE3-256, the mandatory-to-implement hash (D-25).
func MultihashBlake3(data []byte) []byte {
	h := blake3.Sum256(data)
	out := make([]byte, 0, 34)
	out = append(out, mhPrefixBlake3...)
	return append(out, h[:]...)
}

// URNMlet computes the content address of data (spec §3.3.3 / D-25):
// urn:mlet:<multibase(base32-lower, multihash(blake3-256, data))>.
func URNMlet(data []byte) string {
	return urnPrefix + b32l.EncodeToString(MultihashBlake3(data))
}

// ParseURNMlet validates a urn:mlet: string and returns the 32-byte
// BLAKE3 digest it carries.
func ParseURNMlet(urn string) ([]byte, error) {
	if !strings.HasPrefix(urn, urnPrefix) {
		return nil, fmt.Errorf("mlp/core: not a urn:mlet multibase-b URN: %q", urn)
	}
	raw, err := b32l.DecodeString(urn[len(urnPrefix):])
	if err != nil {
		return nil, fmt.Errorf("mlp/core: URN base32: %w", err)
	}
	if len(raw) != 34 || !bytes.Equal(raw[:2], mhPrefixBlake3) {
		return nil, errors.New("mlp/core: URN multihash is not blake3-256")
	}
	return raw[2:], nil
}

// VerifyContent checks data against its claimed content address.
func VerifyContent(urn string, data []byte) error {
	want, err := ParseURNMlet(urn)
	if err != nil {
		return err
	}
	got := blake3.Sum256(data)
	if !bytes.Equal(want, got[:]) {
		return fmt.Errorf("mlp/core: content does not match %s (hash-mismatch)", urn)
	}
	return nil
}

// KeyField encodes a public key for the Domain Document `key` member
// (D-61): multibase base32-lower of the multicodec-prefixed raw key.
func KeyField(pub ed25519.PublicKey) string {
	return "b" + b32l.EncodeToString(append(append([]byte{}, mcEd25519Pub...), pub...))
}

// KID derives the key identifier (D-62): the BLAKE3-256 multihash of
// the multicodec-prefixed key bytes, multibase base32-lower. Hashing
// the prefixed form binds the algorithm into the fingerprint.
func KID(pub ed25519.PublicKey) string {
	mc := append(append([]byte{}, mcEd25519Pub...), pub...)
	return "b" + b32l.EncodeToString(MultihashBlake3(mc))
}

// ParseKeyField decodes and validates a Domain Document `key` member,
// cross-checking the multicodec prefix against the ed25519 algorithm
// (the D-61 mandatory check).
func ParseKeyField(field string) (ed25519.PublicKey, error) {
	if len(field) < 1 || field[0] != 'b' {
		return nil, errors.New("mlp/core: key field is not multibase base32-lower")
	}
	raw, err := b32l.DecodeString(field[1:])
	if err != nil {
		return nil, fmt.Errorf("mlp/core: key field base32: %w", err)
	}
	if len(raw) != 2+ed25519.PublicKeySize || !bytes.Equal(raw[:2], mcEd25519Pub) {
		return nil, errors.New("mlp/core: key multicodec is not ed25519-pub (alg cross-check)")
	}
	return ed25519.PublicKey(raw[2:]), nil
}

// VerifyKID performs the D-62 self-verification: recompute the kid
// from the key material and require equality. Entries failing this
// check MUST be ignored on key-set load (§6.2).
func VerifyKID(kid, keyField string) error {
	pub, err := ParseKeyField(keyField)
	if err != nil {
		return err
	}
	if KID(pub) != kid {
		return errors.New("mlp/core: kid self-verification failed")
	}
	return nil
}
