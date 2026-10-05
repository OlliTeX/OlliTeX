package sso

import (
	"testing"
)

// buildSP MUST expose the provider entryPoint as the IdP SSO binding
// location for both bindings, and the request builders must not panic
// (the 2026-10-06 positive-leg E2E found a nil-IDPMetadata panic → live 500:
// GET /saml/login/:id 500 "invalid memory address"). Regression guard.
func TestBuildSP_ExposesSsoBindingLocation(t *testing.T) {
	p := &SAMLProvider{
		ID:         "sso-saml-e2e",
		Type:       "saml",
		Enabled:    true,
		Issuer:     "https://saml.example.com/entityid",
		EntryPoint: "http://172.20.0.1:4100/api/saml/sso",
	}
	sp := buildSP(p, nil, "http://127.0.0.1:7420")
	if sp.IDPMetadata == nil {
		t.Fatal("buildSP: IDPMetadata is nil (crewjam would nil-pointer in Make*AuthenticationRequest)")
	}
	want := "http://172.20.0.1:4100/api/saml/sso"
	if got := sp.GetSSOBindingLocation("urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"); got != want {
		t.Fatalf("redirect binding = %q, want %q", got, want)
	}
	if got := sp.GetSSOBindingLocation("urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"); got != want {
		t.Fatalf("post binding = %q, want %q", got, want)
	}
	// The builder that actually panicked must now succeed.
	if _, err := sp.MakeRedirectAuthenticationRequest("relay"); err != nil {
		t.Fatalf("MakeRedirectAuthenticationRequest: %v", err)
	}
	if _, err := sp.MakePostAuthenticationRequest("relay"); err != nil {
		t.Fatalf("MakePostAuthenticationRequest: %v", err)
	}
}

// A provider WITHOUT an entryPoint must still build a non-nil SP (no panic on
// construction) — the 400 path handles the missing entryPoint upstream.
func TestBuildSP_NoEntryPoint_StillConstructs(t *testing.T) {
	p := &SAMLProvider{
		ID:      "noep",
		Type:    "saml",
		Enabled: true,
		Issuer:  "https://idp.example/",
	}
	sp := buildSP(p, nil, "http://127.0.0.1:7420")
	if sp == nil {
		t.Fatal("buildSP returned nil")
	}
	// No entryPoint ⇒ no binding location (empty), but NO panic.
	if got := sp.GetSSOBindingLocation("urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect"); got != "" {
		t.Fatalf("expected empty binding location, got %q", got)
	}
}
