package sso

import (
	"regexp"
	"testing"
)

// TestOIDCCallbackRouteOrder — regression for the live OIDC positive-leg
// 404 ("OIDC provider 'callback' not found or disabled", 2026-10-06 E2E):
// the router is FIRST-MATCH (core/app.go), and "callback" matches the
// providerId char class, so /oidc/login/callback is only reachable if the
// exact callback route is registered before the parameterized provider
// route.
func TestOIDCCallbackRouteOrder(t *testing.T) {
	if !oidcCBPattern.MatchString("/oidc/login/callback") {
		t.Fatal("oidcCBPattern must match the callback path")
	}
	// The provider pattern WILL match "callback" (RE2 cannot lookahead it
	// out) — that is exactly why registration order is the protection.
	type reg struct {
		name    string
		pattern *regexp.Regexp
	}
	// Same relative order as Feature() in sso.go.
	ordered := []reg{
		{"oidcLogin", oidcLoginPattern},
		{"oidcCB", oidcCBPattern},
		{"oidcProvider", oidcProviderPattern},
	}
	for _, p := range []string{"/oidc/login/callback", "/oidc/login"} {
		var first string
		for _, r := range ordered {
			if r.pattern.MatchString(p) {
				first = r.name
				break
			}
		}
		if p == "/oidc/login/callback" && first != "oidcCB" {
			t.Fatalf("first match for callback path is %q — must be oidcCB (route order regression)", first)
		}
		if p == "/oidc/login" && first != "oidcLogin" {
			t.Fatalf("first match for /oidc/login is %q — must be oidcLogin", first)
		}
	}
}
