package sso

import (
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func rawDoc(t *testing.T, v any) bson.Raw {
	t.Helper()
	b, err := bson.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bson.Raw(b)
}

// fedgap-6g: ol-auth-config lists ENABLED providers only, ordered, and each
// button is typed to its own (type,id) — no cross-provider leakage.
func TestBuildLoginSlot(t *testing.T) {
	providers := []bson.Raw{
		rawDoc(t, OIDCProvider{ID: "oidc-a", Enabled: false, Order: 1, Type: "oidc"}),
		rawDoc(t, OIDCProvider{ID: "oidc-b", Enabled: true, Order: 2, Name: "B Provider", Type: "oidc"}),
		rawDoc(t, SAMLProvider{ID: "saml-c", Enabled: true, Order: 1, Name: "C IdP", Type: "saml"}),
		rawDoc(t, SAMLProvider{ID: "saml-d", Enabled: false, Order: 3, Type: "saml"}),
	}
	buttons := buildLoginSlot(providers, nil, nil)
	if len(buttons) != 2 {
		t.Fatalf("got %d buttons; want 2 (enabled-only)", len(buttons))
	}
	// ordered by Order: saml-c(1) then oidc-b(2)
	if buttons[0].Label != "C IdP" || buttons[0].Href != "/saml/login/saml-c" {
		t.Errorf("buttons[0] = %+v; want {C IdP, /saml/login/saml-c}", buttons[0])
	}
	if buttons[1].Label != "B Provider" || buttons[1].Href != "/oidc/login/oidc-b" {
		t.Errorf("buttons[1] = %+v; want {B Provider, /oidc/login/oidc-b}", buttons[1])
	}

	// no cross-provider leakage: a saml id must not appear in an oidc href and
	// vice versa; disabled ids must be absent entirely.
	for _, b := range buttons {
		if strings.Contains(b.Href, "oidc-a") || strings.Contains(b.Href, "saml-d") {
			t.Errorf("button %+v leaks a disabled provider id", b)
		}
		// the id in the href is of the same type as the href prefix.
		if strings.HasPrefix(b.Href, "/oidc/") && !strings.Contains(b.Href, "/oidc/") && strings.Contains(b.Href, "saml-") {
			t.Errorf("oidc button leaks a saml id: %q", b.Href)
		}
		if strings.HasPrefix(b.Href, "/saml/") && strings.Contains(b.Href, "oidc-") {
			t.Errorf("saml button leaks an oidc id: %q", b.Href)
		}
	}
}

func TestBuildLoginSlot_EnvProvider(t *testing.T) {
	// env mode: no DB providers, one env SAML provider → a single /saml/login (no id).
	buttons := buildLoginSlot(nil, &SAMLProvider{Name: "Corp SSO", ID: "1"}, nil)
	if len(buttons) != 1 {
		t.Fatalf("got %d buttons; want 1 (env saml)", len(buttons))
	}
	if buttons[0].Label != "Corp SSO" || !strings.HasPrefix(buttons[0].Href, "/saml/login") {
		t.Errorf("env button = %+v; want Corp SSO at /saml/login…", buttons[0])
	}
}

func TestBuildLoginSlot_Empty(t *testing.T) {
	if buttons := buildLoginSlot(nil, nil, nil); len(buttons) != 0 {
		t.Errorf("empty config → got %d buttons; want 0", len(buttons))
	}
}
