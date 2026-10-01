// Identity anchor utilities (Node `util/Anchor.mjs`, 01 §3, 04 §1).
//
// The identity anchor is a TUPLE `(origin, localName)`:
//
//	origin    — the home instance's FQDN (host of Settings.siteUrl), no
//	            scheme, no port, e.g. overleaf.uni-bremen.de
//	localName — the home login name (the home user's email convention;
//	            may contain `@`), e.g. bla@example.com
//
// Display serialization `localName:origin` exists ONLY at human I/O
// boundaries; the wire carries two separate OIDC id_token claims and
// storage is two separate fields. The display string is split on the LAST
// colon so `origin` is unambiguous, and `:` is banned in new `localName`
// values. NOTHING stores or transmits the concatenated anchor string.
package federation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// AnchorResult is the split of a display anchor (localName, origin).
type AnchorResult struct {
	LocalName string
	Origin    string
}

// ParseAnchor splits a display anchor `localName:origin` on the LAST colon.
// Node: Anchor.parseAnchor — returns null (nil here) when the input is not
// a well-formed display string.
func ParseAnchor(str string) *AnchorResult {
	if str == "" {
		return nil
	}
	idx := lastIndexByte(str, ':')
	if idx <= 0 {
		return nil
	}
	localName := str[:idx]
	origin := str[idx+1:]
	if localName == "" || origin == "" {
		return nil
	}
	return &AnchorResult{LocalName: localName, Origin: origin}
}

// FormatAnchor renders `localName:origin` (01 §3.1). Empty when the anchor
// is incomplete. Node: Anchor.formatAnchor (returns null)
func FormatAnchor(localName, origin string) string {
	if localName == "" || origin == "" {
		return ""
	}
	return localName + ":" + origin
}

// ValidateAnchor rejects `:` in localName (01 §3.3 display-layer) and a
// non-bare-FQDN origin (the Node regex). Returns the validated anchor or an
// error with the exact Node message.
func ValidateAnchor(localName, origin string) (AnchorResult, error) {
	if localName == "" {
		return AnchorResult{}, errAnchor("federation: localName required (non-empty string)")
	}
	if origin == "" {
		return AnchorResult{}, errAnchor("federation: origin required (non-empty string)")
	}
	if lastContains(localName, ':') {
		return AnchorResult{}, errAnchor("federation: localName cannot contain \":\" (01 §3.3, display-layer)")
	}
	if !isBareFQDN(origin) {
		return AnchorResult{}, errAnchor("federation: invalid origin FQDN: " + origin)
	}
	return AnchorResult{LocalName: localName, Origin: origin}, nil
}

// errAnchor wraps a plain error with the federation prefix.
type anchorError string

func errAnchor(msg string) error { return anchorError(msg) }

func (e anchorError) Error() string { return string(e) }

// isBareFQDN mirrors the Node regex:
//
//	^[a-z][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z][a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/i
//
// (label 3–63 chars, alphanumeric + hyphen, no leading/trailing hyphen,
// starts letter, ends alnum; case-insensitive). Implemented by hand to
// avoid a regexp dependency for a single hot check.
func isBareFQDN(s string) bool {
	if s == "" {
		return false
	}
	labels := splitFQDN(s)
	if len(labels) == 0 {
		return false
	}
	for _, lbl := range labels {
		if !validLabel(lbl) {
			return false
		}
	}
	return true
}

func splitFQDN(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == '.' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(c)
		}
	}
	out = append(out, cur)
	return out
}

func validLabel(lbl string) bool {
	n := len(lbl)
	if n < 2 || n > 64 {
		return false
	}
	// first char letter (a-z, case-insensitive); last char alnum.
	if !isAlphaASCII(lbl[0]) {
		return false
	}
	last := lbl[n-1]
	if !isAlnumASCII(last) {
		return false
	}
	for i := 1; i < n-1; i++ {
		c := lbl[i]
		if !(isAlnumASCII(c) || c == '-') {
			return false
		}
	}
	return true
}

// isBareFQDN / validLabel letter/digit helpers (ASCII only; the Node regex
// is `[a-z0-9]` — a bare-FQDN is ASCII).
func isAlphaASCII(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isAlnumASCII(b byte) bool { return isAlphaASCII(b) || (b >= '0' && b <= '9') }

func lastContains(s string, b byte) bool { return lastIndexByte(s, b) >= 0 }

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// HashInviteeEmail — `ProjectInvite.federated.localNameHash` (04 §1/§2):
// a `sha256:<hex>` of the display string `<localName>:<origin>`. A stable
// per-row identifier for the invitee; NOT a claim.
func HashInviteeEmail(localName, origin string) string {
	display := localName + ":" + origin
	sum := sha256.Sum256([]byte(display))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SaltedLocalNameHash — Redis-key component (04 §6): a SECRET-SALTED HMAC
// over `<localName>:<origin>`; 32-hex (128-bit) of the HMAC. The salt is the
// site session secret so two instances do not key on shared material.
func SaltedLocalNameHash(salt, localName, origin string) string {
	data := localName + ":" + origin
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}
