package federation

import "testing"

// TestBridgePatternMatchesConsentDeny pins the wire URL contract
// (session-7, 2026-06-11 live E2E): the bridge pattern must match the
// ACTUAL consent/deny URLs /interact/<uid>/{consent,deny} (single slash).
// The original (?://...) double-slash literal never matched them, so
// POST consent/deny fell through to the CSRF 403 gate and the live
// consent dance silently broke while the handler-level hermetic suite
// stayed green (it drives opBridgeConsent directly, never the route).
func TestBridgePatternMatchesConsentDeny(t *testing.T) {
	cases := map[string]struct {
		match bool
		uid   string
		act   string
	}{
		"/federation/oidc/interact/abc123":           {true, "abc123", ""},
		"/federation/oidc/interact/abc123/consent":   {true, "abc123", "consent"},
		"/federation/oidc/interact/abc123/deny":      {true, "abc123", "deny"},
		"/federation/oidc/interact/abc123/other":     {false, "", ""},
		"/federation/oidc/interact/":                 {false, "", ""},
		"/federation/oidc/interact/abc123//consent":  {false, "", ""},
		"/federation/oidc/interact/abc123/consent2":  {false, "", ""},
	}
	for url, want := range cases {
		m := opInteractBridgePattern.MatchString(url)
		if m != want.match {
			t.Fatalf("pattern(%q) = %v, want %v", url, m, want.match)
		}
		if !m {
			continue
		}
		sub := opInteractBridgePattern.FindStringSubmatch(url)
		names := opInteractBridgePattern.SubexpNames()
		got := map[string]string{}
		for i, s := range sub {
			if i > 0 {
				got[names[i]] = s
			}
		}
		if got["uid"] != want.uid || got["act"] != want.act {
			t.Fatalf("pattern(%q) captures = %v, want uid=%q act=%q", url, got, want.uid, want.act)
		}
	}
}
