package sn

import (
	"encoding/json"
	"fmt"
	"medialet.org/mlp/render"
	"net/http"
	"time"
	"unicode/utf8"

	"medialet.org/mlp/core"
)

// MaxEnvelopeBytes is the Signed Envelope cap, measured on the
// transmitted bytes (§3.4.4, D-20/D-52).
const MaxEnvelopeBytes = 262144

// Problem is an unsigned RFC 9457 problem response (§7.2): the reply
// for material the SN could not verify or accept — a signing key is
// never bound to statements about unverified material (D-69).
type Problem struct {
	Status int
	Code   string // registry token (§7.8); wire type is urn:mlp:err:<code>
	Detail string
}

func (p *Problem) Error() string { return fmt.Sprintf("%d %s: %s", p.Status, p.Code, p.Detail) }

func problemf(status int, code, format string, a ...any) *Problem {
	return &Problem{Status: status, Code: code, Detail: fmt.Sprintf(format, a...)}
}

func malformed(format string, a ...any) *Problem {
	return problemf(http.StatusBadRequest, "malformed", format, a...)
}

// ManifestEntry is a validated §3.2.2 entry.
type ManifestEntry struct {
	URN            string `json:"urn"`
	Size           int64  `json:"size"`
	Type           string `json:"type"`
	Name           string `json:"name,omitempty"`
	AvailableUntil string `json:"available_until"`
	PreviewOf      string `json:"preview_of,omitempty"` // MEP-002; "" when absent or ignored-as-violating
}

// SourceEntry is one parsed fulfillment_sources entry (MEP-001).
type SourceEntry struct {
	Domain string
	URNs   []string // empty = all Manifest URNs
	Until  string   // "" = no declared window
}

// Covers reports whether the entry's URN scope includes urn.
func (e SourceEntry) Covers(urn string) bool {
	if len(e.URNs) == 0 {
		return true
	}
	for _, u := range e.URNs {
		if u == urn {
			return true
		}
	}
	return false
}

// ParsedEnvelope is a Signed Envelope that passed §3.4.4 items 1–5
// (structure, version, locality, structural caps, timestamp skew).
// Signature verification (item 7) and the replay check (item 6) are
// the SN's job — they need discovery and the database.
type ParsedEnvelope struct {
	Raw []byte

	Envelope map[string]any // hop-signature payload
	HopSig   map[string]any
	Medialet map[string]any // author-signature payload
	AuthSig  map[string]any

	EnvelopeID string
	Origin     string
	Created    string
	EnvelopeTo []string

	Author         string
	AuthorDomain   string
	MedialetID     string
	Subject        string
	InReplyTo      string
	MedialetTime   string // medialet.created
	BodyContent    string
	Derived        *render.Result // §11 derivation (D-94), set at ingest/send
	Sources        []SourceEntry  // parsed fulfillment_sources (MEP-001)
	Manifest       []ManifestEntry
	HopsJSON       string // verbatim-equivalent JSON of hops, "" when absent
	ForwardedBy    string
	FulfillSrcJSON string

	// ContentAddress is the §3.3.3 content address over the JCS form
	// of the complete Signed Medialet; CanonicalMedialet is that form.
	ContentAddress    string
	CanonicalMedialet []byte
}

// idGrammar checks the Medialet-ID / envelope_id grammar (§3.2.1):
// 1–64 characters of [A-Za-z0-9_-].
func idGrammar(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return false
	}
	return true
}

func rfc3339utc(s string) bool {
	t, err := time.Parse(time.RFC3339, s)
	return err == nil && t.Location() == time.UTC
}

func sigShape(v any) (map[string]any, bool) {
	sig, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	if _, ok := sig["mlp_sig"].(string); !ok {
		return nil, false
	}
	p, ok := sig["protected"].(map[string]any)
	if !ok {
		return nil, false
	}
	if _, ok := p["kid"].(string); !ok {
		return nil, false
	}
	if _, ok := p["alg"].(string); !ok {
		return nil, false
	}
	if c, ok := p["created"].(string); !ok || !rfc3339utc(c) {
		return nil, false
	}
	if _, ok := sig["value"].(string); !ok {
		return nil, false
	}
	return sig, true
}

func cp(s string) int { return utf8.RuneCountInString(s) }

// ParseEnvelope runs §3.4.4 items 1–5 against raw for an SN serving
// localDomain. Structural-cap and locality violations map to
// `malformed` (they are schema-grade defects of the received
// document; §7.3 enumerates no closer code).
func ParseEnvelope(raw []byte, now time.Time, localDomain string) (*ParsedEnvelope, *Problem) {
	// Item 1: size cap on transmitted bytes; well-formed JSON under
	// the §2.3 dialect; schema of §§3.2–3.4.
	if len(raw) > MaxEnvelopeBytes {
		return nil, problemf(http.StatusRequestEntityTooLarge, "envelope-too-large",
			"%d bytes exceeds the %d-byte cap (§3.4.4)", len(raw), MaxEnvelopeBytes)
	}
	v, err := core.ParseDialect(raw)
	if err != nil {
		return nil, malformed("%v", err)
	}
	top, ok := v.(map[string]any)
	if !ok {
		return nil, malformed("top level is not an object")
	}
	pe := &ParsedEnvelope{Raw: raw}
	if pe.Envelope, ok = top["envelope"].(map[string]any); !ok {
		return nil, malformed("missing envelope member (§3.4.3)")
	}
	if pe.HopSig, ok = sigShape(top["signature"]); !ok {
		return nil, malformed("malformed hop signature object (§3.4.3)")
	}
	env := pe.Envelope

	// Item 2: supported version. The member must exist (schema);
	// a present-but-different version is version-unsupported.
	mlp, ok := env["mlp"].(string)
	if !ok {
		return nil, malformed("missing envelope mlp member (§3.4.1)")
	}
	if mlp != "0.1" {
		return nil, problemf(http.StatusBadRequest, "version-unsupported", "envelope mlp %q (§3.4.4)", mlp)
	}

	if pe.EnvelopeID, ok = env["envelope_id"].(string); !ok || !idGrammar(pe.EnvelopeID) {
		return nil, malformed("malformed envelope_id (§3.4.1)")
	}
	if pe.Created, ok = env["created"].(string); !ok || !rfc3339utc(pe.Created) {
		return nil, malformed("malformed envelope created (§3.4.1)")
	}
	if pe.Origin, ok = env["origin"].(string); !ok || validDomain(pe.Origin) != nil || pe.Origin != lower(pe.Origin) {
		return nil, malformed("malformed envelope origin (§3.4.1)")
	}

	// envelope_to: non-empty, bare routing-form addresses, single
	// shared domain (§3.4.1); cap 128 (item 4); locality (item 3).
	toRaw, ok := env["envelope_to"].([]any)
	if !ok || len(toRaw) == 0 {
		return nil, malformed("envelope_to missing or empty (§3.4.1)")
	}
	if len(toRaw) > 128 {
		return nil, malformed("envelope_to exceeds 128 entries (§3.4.4 cap)")
	}
	for _, x := range toRaw {
		addr, ok := x.(string)
		if !ok {
			return nil, malformed("envelope_to entry is not a string (§3.4.1)")
		}
		_, dom, err := ParseAddress(addr)
		if err != nil {
			return nil, malformed("envelope_to entry: %v", err)
		}
		if dom != localDomain {
			return nil, malformed("envelope_to %q is not at the served domain %s (§3.4.4 item 3)", addr, localDomain)
		}
		pe.EnvelopeTo = append(pe.EnvelopeTo, addr)
	}

	if fb, present := env["forwarded_by"]; present {
		s, ok := fb.(string)
		if !ok {
			return nil, malformed("forwarded_by is not a string (§3.4.1)")
		}
		if _, _, err := ParseAddress(s); err != nil {
			return nil, malformed("forwarded_by: %v", err)
		}
		pe.ForwardedBy = s
	}
	if fs, present := env["fulfillment_sources"]; present {
		list, ok := fs.([]any)
		if !ok {
			return nil, malformed("fulfillment_sources is not an array (§3.4.1)")
		}
		for _, x := range list {
			src, ok := x.(map[string]any)
			if !ok {
				return nil, malformed("fulfillment_sources entry is not an object (§3.4.1)")
			}
			d, ok := src["domain"].(string)
			if !ok || validDomain(d) != nil {
				return nil, malformed("fulfillment_sources entry lacks a valid domain (§3.4.1)")
			}
			entry := SourceEntry{Domain: d}
			if u, present := src["urns"]; present {
				urns, ok := u.([]any)
				if !ok {
					return nil, malformed("fulfillment_sources urns is not an array (§3.4.1)")
				}
				for _, uu := range urns {
					us, ok := uu.(string)
					if !ok {
						return nil, malformed("fulfillment_sources urn is not a string (§3.4.1)")
					}
					entry.URNs = append(entry.URNs, us)
				}
			}
			// MEP-001: the declaring source's own offer window —
			// a known member, validated strictly when present.
			if u, present := src["until"]; present {
				us, ok := u.(string)
				if !ok || !rfc3339utc(us) {
					return nil, malformed("fulfillment_sources until is not RFC 3339 UTC (§3.4.1, MEP-001)")
				}
				entry.Until = us
			}
			pe.Sources = append(pe.Sources, entry)
		}
		b, _ := json.Marshal(fs)
		pe.FulfillSrcJSON = string(b)
	}

	// hops: structural validation only (§3.4.2 — provenance data);
	// cap 32 (item 4).
	if h, present := env["hops"]; present {
		hops, ok := h.([]any)
		if !ok {
			return nil, malformed("hops is not an array (§3.4.1)")
		}
		if len(hops) > 32 {
			return nil, malformed("hops exceeds 32 entries (§3.4.4 cap)")
		}
		for _, x := range hops {
			hop, ok := x.(map[string]any)
			if !ok {
				return nil, malformed("hop attestation is not an object (§3.4.2)")
			}
			for _, m := range []string{"origin", "envelope_id", "created", "kid", "sig"} {
				if _, ok := hop[m].(string); !ok {
					return nil, malformed("hop attestation missing %s (§3.4.2)", m)
				}
			}
		}
		b, _ := json.Marshal(h)
		pe.HopsJSON = string(b)
	}

	// The embedded Signed Medialet (§3.3.1, §3.2).
	sm, ok := env["medialet"].(map[string]any)
	if !ok {
		return nil, malformed("missing medialet member (§3.4.1)")
	}
	if pe.Medialet, ok = sm["medialet"].(map[string]any); !ok {
		return nil, malformed("missing inner medialet (§3.3.1)")
	}
	if pe.AuthSig, ok = sigShape(sm["signature"]); !ok {
		return nil, malformed("malformed author signature object (§3.3.1)")
	}
	if prob := pe.validateMedialet(); prob != nil {
		return nil, prob
	}

	// Item 5: timestamp skew ±48 h on envelope.created (D-20).
	created, _ := time.Parse(time.RFC3339, pe.Created)
	if d := now.Sub(created); d > 48*time.Hour || d < -48*time.Hour {
		return nil, problemf(http.StatusBadRequest, "timestamp-skew",
			"envelope created %s outside ±48 h of %s (§3.4.4)", pe.Created, now.UTC().Format(time.RFC3339))
	}

	// §3.3.3: the content address over the JCS form of the complete
	// Signed Medialet.
	canon, err := core.CanonicalizeValue(sm)
	if err != nil {
		return nil, malformed("medialet does not canonicalize: %v", err)
	}
	pe.CanonicalMedialet = canon
	pe.ContentAddress = core.URNMlet(canon)
	return pe, nil
}

func (pe *ParsedEnvelope) validateMedialet() *Problem {
	m := pe.Medialet
	mlp, ok := m["mlp"].(string)
	if !ok {
		return malformed("missing medialet mlp (§3.2.1)")
	}
	if mlp != "0.1" {
		return problemf(http.StatusBadRequest, "version-unsupported", "medialet mlp %q", mlp)
	}
	if pe.MedialetID, ok = m["id"].(string); !ok || !idGrammar(pe.MedialetID) {
		return malformed("malformed medialet id (§3.2.1)")
	}
	if pe.Author, ok = m["author"].(string); !ok {
		return malformed("missing medialet author (§3.2.1)")
	}
	if _, dom, err := ParseAddress(pe.Author); err != nil {
		return malformed("author: %v", err)
	} else {
		pe.AuthorDomain = dom
	}
	if s, present := m["subject"]; present {
		str, ok := s.(string)
		if !ok || cp(str) < 1 || cp(str) > 256 {
			return malformed("subject outside 1–256 code points (§3.2.1)")
		}
		pe.Subject = str
	}
	if c, ok := m["created"].(string); !ok || !rfc3339utc(c) {
		return malformed("malformed medialet created (§3.2.1)")
	} else {
		pe.MedialetTime = c
	}
	if irt, present := m["in_reply_to"]; present {
		s, ok := irt.(string)
		if !ok {
			return malformed("in_reply_to is not a string (§3.2.1)")
		}
		if _, err := core.ParseURNMlet(s); err != nil {
			return malformed("in_reply_to is not a content address (D-49): %v", err)
		}
		pe.InReplyTo = s
	}
	for _, field := range []string{"displayed_to", "displayed_cc"} {
		if r, present := m[field]; present {
			list, ok := r.([]any)
			if !ok {
				return malformed("%s is not an array (§3.2.1)", field)
			}
			for _, x := range list {
				rec, ok := x.(map[string]any)
				if !ok {
					return malformed("%s entry is not a Recipient object (§3.2.1)", field)
				}
				addr, ok := rec["addr"].(string)
				if !ok {
					return malformed("%s Recipient lacks addr (§3.2.1)", field)
				}
				if _, _, err := ParseAddress(addr); err != nil {
					return malformed("%s Recipient addr: %v", field, err)
				}
				if n, present := rec["name"]; present {
					s, ok := n.(string)
					if !ok || cp(s) < 1 || cp(s) > 128 {
						return malformed("Recipient name outside 1–128 code points (§3.2.1)")
					}
				}
			}
		}
	}
	body, ok := m["body"].(map[string]any)
	if !ok {
		return malformed("missing body (§3.2.1)")
	}
	if p, ok := body["profile"].(string); !ok || p != "mlp-html/1" {
		return malformed("body profile is not mlp-html/1 (§3.2.1)")
	}
	if c, ok := body["content"].(string); !ok {
		return malformed("body content is not a string (§3.2.1)")
	} else {
		pe.BodyContent = c
	}

	if prob := validateManifest(pe, m); prob != nil {
		return prob
	}
	return nil
}

// validateManifest applies §3.2.2/§3.2.3 to the medialet's manifest,
// including the MEP-002 preview_of constraints (violating members
// ignored, entries standing). Exposed within the package so the
// TV-007 conformance test anchors on the real validator.
func validateManifest(pe *ParsedEnvelope, m map[string]any) *Problem {
	if man, present := m["manifest"]; present {
		list, ok := man.([]any)
		if !ok {
			return malformed("manifest is not an array (§3.2.1)")
		}
		if len(list) > 256 {
			return malformed("manifest exceeds 256 entries (§3.4.4 cap)")
		}
		seen := map[string]bool{}
		for _, x := range list {
			e, ok := x.(map[string]any)
			if !ok {
				return malformed("manifest entry is not an object (§3.2.2)")
			}
			var me ManifestEntry
			if me.URN, ok = e["urn"].(string); !ok {
				return malformed("manifest entry lacks urn (§3.2.2)")
			}
			if _, err := core.ParseURNMlet(me.URN); err != nil {
				return malformed("manifest urn: %v", err)
			}
			if seen[me.URN] {
				return malformed("manifest urn %s not distinct (§3.2.2)", me.URN)
			}
			seen[me.URN] = true
			num, ok := e["size"].(json.Number)
			if !ok {
				return malformed("manifest size is not an integer (§3.2.2)")
			}
			sz, err := num.Int64()
			if err != nil || sz < 0 {
				return malformed("manifest size is not a non-negative integer (§3.2.2)")
			}
			me.Size = sz
			if me.Type, ok = e["type"].(string); !ok {
				return malformed("manifest entry lacks type (§3.2.2)")
			}
			if n, present := e["name"]; present {
				s, ok := n.(string)
				if !ok || cp(s) < 1 || cp(s) > 255 {
					return malformed("manifest name outside 1–255 code points (§3.2.2)")
				}
				me.Name = s
			}
			if me.AvailableUntil, ok = e["available_until"].(string); !ok || !rfc3339utc(me.AvailableUntil) {
				return malformed("manifest available_until malformed (§3.2.2)")
			}
			if seg, present := e["segments"]; present {
				segs, ok := seg.([]any)
				if !ok {
					return malformed("segments is not an array (§3.2.2)")
				}
				for _, s := range segs {
					if _, ok := s.(string); !ok {
						return malformed("segment digest is not a string (§3.2.2)")
					}
				}
			}
			if pv, present := e["preview_of"]; present {
				// MEP-002: type-checked here; the relational
				// constraints run after the loop, and violations
				// IGNORE the member (the entry stands).
				pvs, ok := pv.(string)
				if !ok {
					return malformed("preview_of is not a string (§3.2.2, MEP-002)")
				}
				me.PreviewOf = pvs
			}
			pe.Manifest = append(pe.Manifest, me)
		}
		// MEP-002 relational constraints, second pass: a violating
		// preview_of (dangling target, chain, self-reference) is
		// ignored — stripped from the parsed entry, never fatal.
		// Constraints read the DECLARED members (a pre-strip
		// snapshot), so outcomes are order-independent.
		declared := make(map[string]int, len(pe.Manifest))
		original := make([]string, len(pe.Manifest))
		for i, me := range pe.Manifest {
			declared[me.URN] = i
			original[i] = me.PreviewOf
		}
		for i := range pe.Manifest {
			pv := original[i]
			if pv == "" {
				continue
			}
			target, present := declared[pv]
			if !present || pv == pe.Manifest[i].URN || original[target] != "" {
				pe.Manifest[i].PreviewOf = "" // ignored (§3.2.2, MEP-002)
			}
		}
	}
	return nil
}

func lower(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}
