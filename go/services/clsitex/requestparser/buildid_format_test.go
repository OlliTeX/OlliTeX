package requestparser

import "testing"

// The CLSI buildId contract (Node OutputCacheManager.generateBuildId):
// `${Date.now().hex}-${crypto.randomBytes(8).hex}` — EXACTLY ONE dash.
// A UUIDv4 (three internal dashes) is REJECTED by the strictObject schema —
// pinned by the live audit 013 conversion 500 ("buildId attribute does not
// match regex /^[0-9a-f]+-[0-9a-f]+$/").
func TestBuildIdFormatPinned(t *testing.T) {
	valid := "2026ab12-0123456789abcdef"
	if !BuildRegex.MatchString(valid) {
		t.Fatal(`expected 'dateHex-randomHex16' to match buildId regex`)
	}
	uuid := "e7b01b52-70eb-4134-9f8e-cab800ef27d3"
	if BuildRegex.MatchString(uuid) {
		t.Fatal("a UUIDv4 must NOT match the buildId regex (one-dash contract)")
	}
}
