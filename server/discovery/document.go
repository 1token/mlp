// Package discovery implements MLP discovery (spec §5): the Domain
// Document (§5.2), the hardened fetch profile (§5.4, D-11/D-59), and
// caching with the 24-hour ceiling (§5.5, D-33).
//
// Conformance anchor: the Domain Document fixture embedded in TV-001
// (conformance/vectors/mlp-tv-001.json, member `domain_document`).
package discovery

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/url"
	"time"

	"medialet.org/mlp/core"
)

const (
	// MaxDocumentBytes is the Domain Document size cap (D-57),
	// aligned with the hardened-profile response cap (§5.4 rule 2).
	MaxDocumentBytes = 65536

	// MaxKeyEntries caps the key set (§5.2).
	MaxKeyEntries = 64
)

// ErrDocument reports a document-level validation failure: the domain
// is undiscoverable for this attempt (§5.1 step 3).
var ErrDocument = errors.New("mlp/discovery: invalid Domain Document")

// KeyEntry is one member of a Domain Document key set (§5.2) that
// survived kid self-verification (§6.2).
type KeyEntry struct {
	KID       string
	Alg       string
	Key       string
	Roles     []string
	NotBefore string // RFC 3339 UTC or "" (valid from the beginning of time, §6.3.3)
	NotAfter  string // RFC 3339 UTC or "" (no scheduled expiry, §6.3.3)
}

// Public decodes the entry's key material (§6.1), cross-checking the
// multicodec prefix against ed25519 (D-61).
func (e *KeyEntry) Public() (ed25519.PublicKey, error) {
	return core.ParseKeyField(e.Key)
}

// HasRole reports whether the entry's roles include role (§6.3.1).
func (e *KeyEntry) HasRole(role string) bool {
	for _, r := range e.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// ValidAt evaluates the entry's validity window at time t (§6.3.3).
// Absent bounds are open; window fields are validated at parse time.
func (e *KeyEntry) ValidAt(t time.Time) bool {
	if e.NotBefore != "" {
		nb, err := time.Parse(time.RFC3339, e.NotBefore)
		if err != nil || t.Before(nb) {
			return false
		}
	}
	if e.NotAfter != "" {
		na, err := time.Parse(time.RFC3339, e.NotAfter)
		if err != nil || t.After(na) {
			return false
		}
	}
	return true
}

// Document is a validated Domain Document (§5.2).
type Document struct {
	Domain  string
	MLP     []string
	SN      string
	Contact string
	// Capabilities are the §5.2 optional-capability tokens (MEP-003,
	// draft-03). Unknown tokens MUST be ignored by consumers — the
	// slice carries whatever the document advertised; membership
	// checks pick out what a consumer understands.
	Capabilities []string
	Keys         []KeyEntry // entries that passed self-verification, in published order
	// Rejected counts key entries ignored under the §6.2 rule (kid
	// self-verification failure, alg/multicodec mismatch, malformed
	// entry, duplicate kid). The remainder of the document is still
	// processed.
	Rejected int
}

// VerificationKey returns the public key for kid, enforcing the §6.3
// semantics S4.4+ verification requires: the kid must be present, its
// roles must include role (rule 1), and its validity window must
// contain at (rule 3, evaluated at ingest per D-32).
func (d *Document) VerificationKey(kid, role string, at time.Time) (ed25519.PublicKey, error) {
	for i := range d.Keys {
		e := &d.Keys[i]
		if e.KID != kid {
			continue
		}
		if !e.HasRole(role) {
			return nil, fmt.Errorf("mlp/discovery: key %s of %s lacks role %q (role mismatch is verification failure, §6.3)", kid, d.Domain, role)
		}
		if !e.ValidAt(at) {
			return nil, fmt.Errorf("mlp/discovery: key %s of %s outside its validity window at %s (§6.3)", kid, d.Domain, at.UTC().Format(time.RFC3339))
		}
		return e.Public()
	}
	return nil, fmt.Errorf("mlp/discovery: unknown kid %s for %s", kid, d.Domain)
}

// ParseDocument validates raw as the Domain Document for
// queriedDomain (§5.1 step 3): parsed under the D-43 JSON dialect,
// required members present and well-typed, `domain` binding equal to
// the queried domain (D-57), `mlp` intersecting supported, `sn` an
// https URL, at most 64 key entries. Key entries are individually
// kid-self-verified (§6.2, D-62) with the alg/multicodec cross-check
// (§6.1, D-61); failing entries are ignored, the rest of the document
// is processed. Unknown members are ignored (§2.3 rule 5).
func ParseDocument(raw []byte, queriedDomain string, supported []string) (*Document, error) {
	if len(raw) > MaxDocumentBytes {
		return nil, fmt.Errorf("%w: %d bytes exceeds the %d-byte cap (D-57)", ErrDocument, len(raw), MaxDocumentBytes)
	}
	v, err := core.ParseDialect(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDocument, err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: top level is not an object", ErrDocument)
	}

	doc := &Document{}
	if doc.Domain, ok = obj["domain"].(string); !ok {
		return nil, fmt.Errorf("%w: missing or non-string `domain`", ErrDocument)
	}
	if doc.Domain != queriedDomain {
		return nil, fmt.Errorf("%w: `domain` %q does not equal queried domain %q (binding check, D-57)", ErrDocument, doc.Domain, queriedDomain)
	}

	mlpRaw, ok := obj["mlp"].([]any)
	if !ok {
		return nil, fmt.Errorf("%w: missing or non-array `mlp`", ErrDocument)
	}
	for _, x := range mlpRaw {
		s, ok := x.(string)
		if !ok {
			return nil, fmt.Errorf("%w: non-string entry in `mlp`", ErrDocument)
		}
		doc.MLP = append(doc.MLP, s)
	}
	if !intersects(doc.MLP, supported) {
		return nil, fmt.Errorf("%w: versions %v do not intersect supported %v (§5.1 step 3)", ErrDocument, doc.MLP, supported)
	}

	if doc.SN, ok = obj["sn"].(string); !ok {
		return nil, fmt.Errorf("%w: missing or non-string `sn`", ErrDocument)
	}
	if u, err := url.Parse(doc.SN); err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("%w: `sn` is not an https URL", ErrDocument)
	}

	doc.Contact, _ = obj["contact"].(string)          // OPTIONAL (D-56)
	if raw, present := obj["capabilities"]; present { // OPTIONAL (MEP-003)
		arr, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("capabilities must be an array of strings (§5.2)")
		}
		for _, v := range arr {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("capabilities must be an array of strings (§5.2)")
			}
			doc.Capabilities = append(doc.Capabilities, s)
		}
	}

	keysRaw, ok := obj["keys"].([]any)
	if !ok {
		return nil, fmt.Errorf("%w: missing or non-array `keys`", ErrDocument)
	}
	if len(keysRaw) > MaxKeyEntries {
		return nil, fmt.Errorf("%w: %d key entries exceeds the cap of %d (§5.2)", ErrDocument, len(keysRaw), MaxKeyEntries)
	}
	seen := map[string]bool{}
	for _, x := range keysRaw {
		e, ok := parseKeyEntry(x)
		if !ok || seen[e.KID] {
			doc.Rejected++ // §6.2: ignore the entry, process the rest
			continue
		}
		seen[e.KID] = true
		doc.Keys = append(doc.Keys, e)
	}
	return doc, nil
}

// parseKeyEntry validates a single Key Entry. Any failure — shape,
// alg, kid self-verification, malformed window — invalidates only
// this entry (§6.1, §6.2).
func parseKeyEntry(x any) (KeyEntry, bool) {
	var e KeyEntry
	obj, ok := x.(map[string]any)
	if !ok {
		return e, false
	}
	if e.KID, ok = obj["kid"].(string); !ok {
		return e, false
	}
	if e.Alg, ok = obj["alg"].(string); !ok || e.Alg != "ed25519" {
		// Unknown algorithms are ignored entries, not document
		// failures: `alg` is present for agility (D-12).
		return e, false
	}
	if e.Key, ok = obj["key"].(string); !ok {
		return e, false
	}
	// Kid self-verification (D-62) including the D-61 multicodec
	// cross-check — the MUST wired on every key-set load (§6.2).
	if err := core.VerifyKID(e.KID, e.Key); err != nil {
		return e, false
	}
	rolesRaw, ok := obj["roles"].([]any)
	if !ok || len(rolesRaw) == 0 {
		return e, false
	}
	for _, r := range rolesRaw {
		s, ok := r.(string)
		if !ok {
			return e, false
		}
		e.Roles = append(e.Roles, s)
	}
	for field, dst := range map[string]*string{"not_before": &e.NotBefore, "not_after": &e.NotAfter} {
		if raw, present := obj[field]; present {
			s, ok := raw.(string)
			if !ok {
				return e, false
			}
			if t, err := time.Parse(time.RFC3339, s); err != nil || t.Location() != time.UTC {
				return e, false // §2.3 rule 2: RFC 3339 UTC with Z
			}
			*dst = s
		}
	}
	return e, true
}

func intersects(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}
