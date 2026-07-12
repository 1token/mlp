package webauthn

// The two ceremonies (D-161), verified from primitives: registration
// parses the attestation object (fmt "none" only — attestation-chain
// trust is a post-1.0 concern; what matters here is binding the
// credential the browser minted) and assertion verifies the
// authenticator's signature over authData || SHA-256(clientDataJSON)
// with the stored COSE key. ES256 (P-256) and Ed25519 cover the
// authenticators that exist.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

const (
	AlgES256   = -7
	AlgEd25519 = -8
)

const (
	flagUP = 0x01 // user present
	flagUV = 0x04 // user verified
	flagAT = 0x40 // attested credential data included
)

// AuthData is the parsed authenticator data.
type AuthData struct {
	RPIDHash     []byte
	Flags        byte
	SignCount    uint32
	CredentialID []byte // registration only
	COSEKey      []byte // registration only: the raw CBOR key
	Alg          int64  // registration only
}

func (a *AuthData) UserPresent() bool { return a.Flags&flagUP != 0 }

// ParseAuthData parses the fixed layout; withAttested demands the
// attested-credential-data block (registration).
func ParseAuthData(raw []byte, withAttested bool) (*AuthData, error) {
	if len(raw) < 37 {
		return nil, errors.New("webauthn: authData too short")
	}
	a := &AuthData{
		RPIDHash:  raw[0:32],
		Flags:     raw[32],
		SignCount: binary.BigEndian.Uint32(raw[33:37]),
	}
	rest := raw[37:]
	if a.Flags&flagAT != 0 {
		if len(rest) < 18 {
			return nil, errors.New("webauthn: attested data truncated")
		}
		idLen := int(binary.BigEndian.Uint16(rest[16:18]))
		if len(rest) < 18+idLen {
			return nil, errors.New("webauthn: credential id truncated")
		}
		a.CredentialID = rest[18 : 18+idLen]
		keyRaw := rest[18+idLen:]
		key, n, err := decodeCBOR(keyRaw)
		if err != nil {
			return nil, fmt.Errorf("webauthn: COSE key: %w", err)
		}
		if n != len(keyRaw) {
			return nil, errors.New("webauthn: trailing bytes after COSE key")
		}
		a.COSEKey = keyRaw
		alg, err := coseAlg(key)
		if err != nil {
			return nil, err
		}
		a.Alg = alg
	} else if withAttested {
		return nil, errors.New("webauthn: attested credential data required")
	}
	return a, nil
}

func coseAlg(key any) (int64, error) {
	m, ok := key.(map[any]any)
	if !ok {
		return 0, errors.New("webauthn: COSE key is not a map")
	}
	alg, ok := m[int64(3)].(int64)
	if !ok {
		return 0, errors.New("webauthn: COSE key lacks alg")
	}
	if alg != AlgES256 && alg != AlgEd25519 {
		return 0, fmt.Errorf("webauthn: unsupported alg %d", alg)
	}
	return alg, nil
}

// clientData is the browser's signed context.
type clientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

func parseClientData(raw []byte, wantType, wantChallenge, wantOrigin string) error {
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return fmt.Errorf("webauthn: clientDataJSON: %w", err)
	}
	if cd.Type != wantType {
		return fmt.Errorf("webauthn: clientData type %q, want %q", cd.Type, wantType)
	}
	if cd.Challenge != wantChallenge {
		return errors.New("webauthn: challenge mismatch")
	}
	if wantOrigin != "" && cd.Origin != wantOrigin {
		return fmt.Errorf("webauthn: origin %q not permitted", cd.Origin)
	}
	return nil
}

// Registration is a verified new credential.
type Registration struct {
	CredentialID string // base64url
	COSEKey      []byte
	Alg          int64
	SignCount    uint32
}

// VerifyRegistration checks a create() result: clientDataJSON
// (type/challenge/origin), the attestation object (fmt "none"), and
// authData (rpIdHash, UP, attested data).
func VerifyRegistration(clientDataJSON, attestationObject []byte, challenge, origin, rpID string) (*Registration, error) {
	if err := parseClientData(clientDataJSON, "webauthn.create", challenge, origin); err != nil {
		return nil, err
	}
	obj, err := decodeCBORExact(attestationObject)
	if err != nil {
		return nil, err
	}
	m, ok := obj.(map[any]any)
	if !ok {
		return nil, errors.New("webauthn: attestation object is not a map")
	}
	fmtName, _ := m["fmt"].(string)
	if fmtName != "none" {
		return nil, fmt.Errorf("webauthn: attestation fmt %q — only \"none\" is accepted (D-161 scope)", fmtName)
	}
	authRaw, ok := m["authData"].([]byte)
	if !ok {
		return nil, errors.New("webauthn: attestation object lacks authData")
	}
	ad, err := ParseAuthData(authRaw, true)
	if err != nil {
		return nil, err
	}
	rpHash := sha256.Sum256([]byte(rpID))
	if !bytes.Equal(ad.RPIDHash, rpHash[:]) {
		return nil, errors.New("webauthn: rpIdHash mismatch")
	}
	if !ad.UserPresent() {
		return nil, errors.New("webauthn: user-present flag required")
	}
	if len(ad.CredentialID) == 0 || len(ad.CredentialID) > 1023 {
		return nil, errors.New("webauthn: credential id length")
	}
	return &Registration{
		CredentialID: base64.RawURLEncoding.EncodeToString(ad.CredentialID),
		COSEKey:      ad.COSEKey,
		Alg:          ad.Alg,
		SignCount:    ad.SignCount,
	}, nil
}

// Assertion is a verified login.
type Assertion struct{ SignCount uint32 }

// VerifyAssertion checks a get() result against the stored COSE key:
// clientDataJSON (type/challenge/origin), rpIdHash, UP, and the
// signature over authData || SHA-256(clientDataJSON).
func VerifyAssertion(clientDataJSON, authDataRaw, signature, coseKey []byte, challenge, origin, rpID string) (*Assertion, error) {
	if err := parseClientData(clientDataJSON, "webauthn.get", challenge, origin); err != nil {
		return nil, err
	}
	ad, err := ParseAuthData(authDataRaw, false)
	if err != nil {
		return nil, err
	}
	rpHash := sha256.Sum256([]byte(rpID))
	if !bytes.Equal(ad.RPIDHash, rpHash[:]) {
		return nil, errors.New("webauthn: rpIdHash mismatch")
	}
	if !ad.UserPresent() {
		return nil, errors.New("webauthn: user-present flag required")
	}
	cdHash := sha256.Sum256(clientDataJSON)
	signed := append(append([]byte{}, authDataRaw...), cdHash[:]...)
	key, err := decodeCBORExact(coseKey)
	if err != nil {
		return nil, err
	}
	m := key.(map[any]any)
	alg, err := coseAlg(key)
	if err != nil {
		return nil, err
	}
	switch alg {
	case AlgES256:
		x, _ := m[int64(-2)].([]byte)
		y, _ := m[int64(-3)].([]byte)
		if len(x) != 32 || len(y) != 32 {
			return nil, errors.New("webauthn: malformed P-256 coordinates")
		}
		pub := &ecdsa.PublicKey{Curve: elliptic.P256(),
			X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		digest := sha256.Sum256(signed)
		if !ecdsa.VerifyASN1(pub, digest[:], signature) {
			return nil, errors.New("webauthn: ES256 signature invalid")
		}
	case AlgEd25519:
		x, _ := m[int64(-2)].([]byte)
		if len(x) != ed25519.PublicKeySize {
			return nil, errors.New("webauthn: malformed Ed25519 key")
		}
		if !ed25519.Verify(ed25519.PublicKey(x), signed, signature) {
			return nil, errors.New("webauthn: Ed25519 signature invalid")
		}
	}
	return &Assertion{SignCount: ad.SignCount}, nil
}
