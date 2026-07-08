package core

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
)

// Document-signature labels founded in §6.4 (D-64). The registry is
// administered per §14; these constants mirror it.
const (
	LabelAuthor     = "author/1"
	LabelHop        = "hop/1"
	LabelVerdict    = "verdict/1"
	LabelDelegation = "delegation/1"
)

var b64url = base64.RawURLEncoding // unpadded, per D-49

// SignDoc computes the §6.4 construction (D-44): pure Ed25519 (RFC
// 8032, no ph, no ctx — domain separation is the in-band label, D-63)
// over JCS({"mlp_sig": label, "protected": P, "payload": payload}).
// It returns the signature object for the wire (mlp_sig, protected,
// value) and the exact signing input for auditing/tests.
func SignDoc(priv ed25519.PrivateKey, label, kid, created string, payload any) (sig map[string]any, input []byte, err error) {
	protected := map[string]any{"kid": kid, "alg": "ed25519", "created": created}
	input, err = CanonicalizeValue(map[string]any{
		"mlp_sig":   label,
		"protected": protected,
		"payload":   payload,
	})
	if err != nil {
		return nil, nil, err
	}
	sig = map[string]any{
		"mlp_sig":   label,
		"protected": protected,
		"value":     b64url.EncodeToString(ed25519.Sign(priv, input)),
	}
	return sig, input, nil
}

// VerifyDoc verifies a §6.4 signature object against the label the
// consuming context demands (the D-64 context-match rule: an author/1
// signature presented where hop/1 is expected fails regardless of
// cryptographic validity), reconstructing the signing input by JCS
// over the parsed members (canonicalize-then-verify, D-44).
func VerifyDoc(pub ed25519.PublicKey, expectLabel string, payload any, sig map[string]any) error {
	label, _ := sig["mlp_sig"].(string)
	if label != expectLabel {
		return fmt.Errorf("mlp/core: signature label %q where %q required (context mismatch)", label, expectLabel)
	}
	protected, _ := sig["protected"].(map[string]any)
	if protected == nil {
		return errors.New("mlp/core: signature missing protected block")
	}
	if alg, _ := protected["alg"].(string); alg != "ed25519" {
		return fmt.Errorf("mlp/core: unsupported alg %q", protected["alg"])
	}
	value, _ := sig["value"].(string)
	raw, err := b64url.DecodeString(value)
	if err != nil || len(raw) != ed25519.SignatureSize {
		return errors.New("mlp/core: malformed signature value")
	}
	input, err := CanonicalizeValue(map[string]any{
		"mlp_sig":   label,
		"protected": protected,
		"payload":   payload,
	})
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, input, raw) {
		return errors.New("mlp/core: signature-invalid")
	}
	return nil
}
