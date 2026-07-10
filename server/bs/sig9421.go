// Package bs implements the MLP Blob Store's cross-domain transfer
// surface (spec §8): the tus-bound upload resource with the D-77
// transactional verification pipeline, HEAD resumption, D-27
// incremental BLAKE3 checkpoints, and the §8.7 pusher loop under the
// D-72 connection-safety rules.
//
// Conformance anchor: TV-003 — the interrupted-and-resumed push of
// the TV-001 object under the TV-002 Reservation, replayed
// header-for-header against known keys.
package bs

import (
	"crypto/ed25519"
	"fmt"
	"strconv"
	"strings"
)

// The §6.6 profile (D-66): fixed covered-component sets, exactly as
// ordered; label `mlp`; params created + keyid required, alg optional
// but then ed25519.
var (
	bodyComponents = []string{"@method", "@target-uri", "content-digest", "upload-offset", "mlp-reservation"}
	headComponents = []string{"@method", "@target-uri", "mlp-reservation"}
)

// SigInput is a parsed Signature-Input header (label mlp).
type SigInput struct {
	Components []string
	Params     string // the verbatim inner-list serialization — reused byte-exactly in the base
	Created    int64
	KeyID      string
}

// ParseSignatureInput parses the profile's Signature-Input header.
func ParseSignatureInput(h string) (*SigInput, error) {
	rest, ok := strings.CutPrefix(h, "mlp=")
	if !ok {
		return nil, fmt.Errorf("mlp/bs: Signature-Input lacks the mlp label (§6.6)")
	}
	if !strings.HasPrefix(rest, "(") {
		return nil, fmt.Errorf("mlp/bs: malformed component list")
	}
	end := strings.IndexByte(rest, ')')
	if end < 0 {
		return nil, fmt.Errorf("mlp/bs: unterminated component list")
	}
	si := &SigInput{Params: rest}
	for _, c := range strings.Fields(rest[1:end]) {
		if len(c) < 2 || c[0] != '"' || c[len(c)-1] != '"' {
			return nil, fmt.Errorf("mlp/bs: unquoted component %q", c)
		}
		si.Components = append(si.Components, c[1:len(c)-1])
	}
	for _, p := range strings.Split(rest[end+1:], ";") {
		p = strings.TrimSpace(p)
		switch {
		case p == "":
		case strings.HasPrefix(p, "created="):
			n, err := strconv.ParseInt(p[len("created="):], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("mlp/bs: malformed created parameter")
			}
			si.Created = n
		case strings.HasPrefix(p, `keyid="`) && strings.HasSuffix(p, `"`):
			si.KeyID = p[len(`keyid="`) : len(p)-1]
		case p == `alg="ed25519"`:
			// permitted (§6.6 rule 3)
		case strings.HasPrefix(p, `alg="`):
			return nil, fmt.Errorf("mlp/bs: alg present but not ed25519 (§6.6)")
		default:
			return nil, fmt.Errorf("mlp/bs: unknown signature parameter %q", p)
		}
	}
	if si.Created == 0 || si.KeyID == "" {
		return nil, fmt.Errorf("mlp/bs: created and keyid are REQUIRED (§6.6)")
	}
	return si, nil
}

// ParseSignature extracts the raw signature bytes from the Signature
// header (label mlp, RFC 9421 byte-sequence).
func ParseSignature(h string) ([]byte, error) {
	v, ok := strings.CutPrefix(h, "mlp=:")
	if !ok || !strings.HasSuffix(v, ":") {
		return nil, fmt.Errorf("mlp/bs: malformed Signature header")
	}
	return stdBase64(v[:len(v)-1])
}

// componentsEqual enforces "exactly as ordered" (D-66).
func componentsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// BuildBase assembles the RFC 9421 signature base for the profile:
// one line per covered component, closing with @signature-params
// carrying the verbatim serialization from Signature-Input; no
// trailing newline.
func BuildBase(method, targetURI string, header func(string) string, si *SigInput) (string, error) {
	var b strings.Builder
	for i, c := range si.Components {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch c {
		case "@method":
			fmt.Fprintf(&b, "%q: %s", c, method)
		case "@target-uri":
			fmt.Fprintf(&b, "%q: %s", c, targetURI)
		default:
			v := header(c)
			if v == "" {
				return "", fmt.Errorf("mlp/bs: covered component %q absent from the request", c)
			}
			fmt.Fprintf(&b, "%q: %s", c, v)
		}
	}
	fmt.Fprintf(&b, "\n%q: %s", "@signature-params", si.Params)
	return b.String(), nil
}

// SignRequest produces the two signature headers for a request under
// the profile. hasBody selects the covered-component set.
func SignRequest(priv ed25519.PrivateKey, kid, method, targetURI string, header func(string) string, created int64, hasBody bool) (sigInput, signature string, err error) {
	comps := headComponents
	if hasBody {
		comps = bodyComponents
	}
	quoted := make([]string, len(comps))
	for i, c := range comps {
		quoted[i] = `"` + c + `"`
	}
	params := fmt.Sprintf(`(%s);created=%d;keyid=%q;alg="ed25519"`,
		strings.Join(quoted, " "), created, kid)
	si := &SigInput{Components: comps, Params: params, Created: created, KeyID: kid}
	base, err := BuildBase(method, targetURI, header, si)
	if err != nil {
		return "", "", err
	}
	sig := ed25519.Sign(priv, []byte(base))
	return "mlp=" + params, "mlp=:" + toBase64(sig) + ":", nil
}
