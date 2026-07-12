package webauthn

// Synthetic-authenticator tests: the test plays the platform
// authenticator, minting real P-256 and Ed25519 keys, encoding
// authData and COSE by hand through the package's own strict codec,
// and signing exactly what a FIDO device signs. Registration and
// assertion verify end to end; tampered signatures, wrong
// challenges, foreign rpIDs, and non-"none" attestations refuse.

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func clientDataJSON(t *testing.T, typ, challenge string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{
		"type": typ, "challenge": challenge, "origin": "https://target.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func authData(rpID string, flags byte, signCount uint32, credID []byte, coseKey []byte) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, h[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, signCount)
	if credID != nil {
		out = append(out, make([]byte, 16)...) // AAGUID zero
		out = binary.BigEndian.AppendUint16(out, uint16(len(credID)))
		out = append(out, credID...)
		out = append(out, coseKey...)
	}
	return out
}

func es256Key(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cose := EncodeCBOR(map[any]any{
		int64(1): int64(2), int64(3): int64(AlgES256), int64(-1): int64(1),
		int64(-2): priv.X.FillBytes(make([]byte, 32)),
		int64(-3): priv.Y.FillBytes(make([]byte, 32)),
	})
	return priv, cose
}

func ed25519Key(t *testing.T) (ed25519.PrivateKey, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cose := EncodeCBOR(map[any]any{
		int64(1): int64(1), int64(3): int64(AlgEd25519), int64(-1): int64(6),
		int64(-2): []byte(pub),
	})
	return priv, cose
}

func TestRegistrationAndAssertionES256(t *testing.T) {
	priv, cose := es256Key(t)
	credID := []byte("cred-es256-0001")
	challenge := b64([]byte("register-challenge-1"))

	att := EncodeCBOR(map[any]any{
		"fmt": "none", "attStmt": map[any]any{},
		"authData": authData("target.example", flagUP|flagUV|flagAT, 0, credID, cose),
	})
	reg, err := VerifyRegistration(clientDataJSON(t, "webauthn.create", challenge), att,
		challenge, "https://target.example", "target.example")
	if err != nil {
		t.Fatalf("registration: %v", err)
	}
	if reg.CredentialID != b64(credID) || reg.Alg != AlgES256 {
		t.Fatalf("registration parse: %+v", reg)
	}

	// Assertion.
	loginChallenge := b64([]byte("login-challenge-1"))
	cd := clientDataJSON(t, "webauthn.get", loginChallenge)
	ad := authData("target.example", flagUP, 7, nil, nil)
	cdHash := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	as, err := VerifyAssertion(cd, ad, sig, reg.COSEKey,
		loginChallenge, "https://target.example", "target.example")
	if err != nil {
		t.Fatalf("assertion: %v", err)
	}
	if as.SignCount != 7 {
		t.Fatalf("sign count: %d", as.SignCount)
	}

	// Tampering refuses.
	sig[8] ^= 0x40
	if _, err := VerifyAssertion(cd, ad, sig, reg.COSEKey,
		loginChallenge, "https://target.example", "target.example"); err == nil {
		t.Fatal("tampered signature must refuse")
	}
	sig[8] ^= 0x40
	if _, err := VerifyAssertion(cd, ad, sig, reg.COSEKey,
		b64([]byte("other")), "https://target.example", "target.example"); err == nil {
		t.Fatal("wrong challenge must refuse")
	}
	if _, err := VerifyAssertion(cd, ad, sig, reg.COSEKey,
		loginChallenge, "https://target.example", "evil.example"); err == nil {
		t.Fatal("foreign rpID must refuse")
	}
}

func TestRegistrationAndAssertionEd25519(t *testing.T) {
	priv, cose := ed25519Key(t)
	credID := []byte("cred-ed25519-01")
	challenge := b64([]byte("register-challenge-2"))
	att := EncodeCBOR(map[any]any{
		"fmt": "none", "attStmt": map[any]any{},
		"authData": authData("target.example", flagUP|flagAT, 3, credID, cose),
	})
	reg, err := VerifyRegistration(clientDataJSON(t, "webauthn.create", challenge), att,
		challenge, "https://target.example", "target.example")
	if err != nil {
		t.Fatalf("registration: %v", err)
	}
	loginChallenge := b64([]byte("login-challenge-2"))
	cd := clientDataJSON(t, "webauthn.get", loginChallenge)
	ad := authData("target.example", flagUP, 4, nil, nil)
	cdHash := sha256.Sum256(cd)
	sig := ed25519.Sign(priv, append(append([]byte{}, ad...), cdHash[:]...))
	if _, err := VerifyAssertion(cd, ad, sig, reg.COSEKey,
		loginChallenge, "https://target.example", "target.example"); err != nil {
		t.Fatalf("assertion: %v", err)
	}
}

func TestRegistrationRefusals(t *testing.T) {
	_, cose := es256Key(t)
	credID := []byte("cred")
	challenge := b64([]byte("c"))
	cd := clientDataJSON(t, "webauthn.create", challenge)

	packed := EncodeCBOR(map[any]any{
		"fmt": "packed", "attStmt": map[any]any{},
		"authData": authData("target.example", flagUP|flagAT, 0, credID, cose),
	})
	if _, err := VerifyRegistration(cd, packed, challenge, "https://target.example", "target.example"); err == nil {
		t.Fatal("non-none attestation must refuse")
	}
	noUP := EncodeCBOR(map[any]any{
		"fmt": "none", "attStmt": map[any]any{},
		"authData": authData("target.example", flagAT, 0, credID, cose),
	})
	if _, err := VerifyRegistration(cd, noUP, challenge, "https://target.example", "target.example"); err == nil {
		t.Fatal("absent user-present must refuse")
	}
	trailing := append(EncodeCBOR(map[any]any{
		"fmt": "none", "attStmt": map[any]any{},
		"authData": authData("target.example", flagUP|flagAT, 0, credID, cose),
	}), 0x00)
	if _, err := VerifyRegistration(cd, trailing, challenge, "https://target.example", "target.example"); err == nil {
		t.Fatal("trailing bytes must refuse")
	}
}
