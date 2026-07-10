// Package sn implements the MLP Signaling Node's negotiation surface
// (spec §7): the /dispatch endpoint with the §3.4.4 validation
// sequence, verdict/1 document generation and verification (§7.4,
// §6.4), reservations (§7.5), verdict updates with the §7.6
// transition table, and the §7.7 default acceptance policy.
//
// Conformance anchors: TV-001 (the dispatched Signed Envelope) and
// TV-002 (both negotiation verdicts, reproduced byte-identically).
package sn

import (
	"fmt"
	"strings"
)

// ParseAddress validates s against the §4.1 grammar in routing form
// (§4.2: lowercase local, lowercase A-label domain — the only form
// legal on the wire) and returns (local, domain).
func ParseAddress(s string) (local, domain string, err error) {
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return "", "", fmt.Errorf("mlp/sn: address %q lacks local@domain shape (§4.1)", s)
	}
	local, domain = s[:at], s[at+1:]
	if len(local) > 64 {
		return "", "", fmt.Errorf("mlp/sn: local part exceeds 64 characters (§4.1)")
	}
	base := local
	if plus := strings.IndexByte(local, '+'); plus >= 0 {
		base = local[:plus]
		tag := local[plus+1:]
		if tag == "" || !validRun(tag, "+.") {
			return "", "", fmt.Errorf("mlp/sn: malformed subaddress tag (§4.1)")
		}
	}
	if base == "" {
		return "", "", fmt.Errorf("mlp/sn: empty local base (§4.1)")
	}
	for _, atom := range strings.Split(base, ".") {
		if atom == "" || !validRun(atom, "") {
			return "", "", fmt.Errorf("mlp/sn: malformed local base %q (§4.1)", base)
		}
	}
	if err := validDomain(domain); err != nil {
		return "", "", err
	}
	if s != strings.ToLower(s) {
		return "", "", fmt.Errorf("mlp/sn: address %q is not in routing form (§4.2)", s)
	}
	return local, domain, nil
}

// MailboxKey reduces a routing-form address to the delivery identity
// (§4.2, D-55): the tag is removed; routing on the wire retains it.
func MailboxKey(addr string) string {
	at := strings.IndexByte(addr, '@')
	if at < 0 {
		return addr
	}
	local := addr[:at]
	if plus := strings.IndexByte(local, '+'); plus >= 0 {
		local = local[:plus]
	}
	return local + addr[at:]
}

// validRun reports whether s consists of [a-z0-9-_] plus any bytes in
// extra. Uppercase is excluded: routing form is lowercase (§4.2).
func validRun(s, extra string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' ||
			strings.IndexByte(extra, c) >= 0 {
			continue
		}
		return false
	}
	return true
}

// validDomain checks the §4.1 domain constraints for a routing-form
// (A-label, lowercase) domain: at least one dot, labels 1–63 octets,
// whole name <= 253 octets.
func validDomain(d string) error {
	if len(d) > 253 || !strings.Contains(d, ".") {
		return fmt.Errorf("mlp/sn: domain %q violates §4.1 constraint 4", d)
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || !validRun(label, "") {
			return fmt.Errorf("mlp/sn: domain %q has a malformed label (§4.1)", d)
		}
	}
	return nil
}
