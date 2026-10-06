package sso

import (
	"strings"
	"testing"
)

// A3 (owner 2026-10-06): the SP-metadata admin page contracts — configured
// shows the entityID + escaped XML + download/copy hooks; unconfigured is
// the honest empty state. (Route registration + the adminGate wire are
// covered by the feature suite + live E2E.)
func TestSamlMetaAdminHTMLShapes(t *testing.T) {
	cfg := samlMetaAdminHTML("https://sp.example.com/ol", `<md:EntityDescriptor entityID="https://sp.example.com/ol">&lt;evil&gt;</md:EntityDescriptor>`)
	for _, want := range []string{
		`SAML SP metadata — https://sp.example.com/ol`,
		`/saml/meta`,
		`id="saml-meta-xml"`,
		`id="saml-meta-copy"`,
		`nonce="__NONCE__"`, // replaced with a real nonce in the handler
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("configured page missing %q", want)
		}
	}
	// hostile XML must stay inert (escaped twice: html once, <pre> text)
	if strings.Contains(cfg, `<evil>`) {
		t.Fatal("unescaped hostile XML in the page")
	}
	u := samlMetaUnconfiguredHTML
	for _, want := range []string{`SAML SP metadata`, `not configured on this instance yet`, `site settings` } {
		if !strings.Contains(u, want) {
			t.Fatalf("unconfigured page missing %q", want)
		}
	}
}

func TestAdminSAMLMetaPatternWire(t *testing.T) {
	// exact + Admin case + trailing slash (U9-style variant discipline).
	for _, p := range []string{"/admin/saml/metadata", "/Admin/saml/metadata"} {
		if !adminSAMLMetaPattern.MatchString(p) {
			t.Fatalf("pattern must match %q", p)
		}
	}
	for _, p := range []string{"/saml/meta", "/admin/saml/metadata/x", "/admin/saml", "/admin/saml/metadata/"} {
		if adminSAMLMetaPattern.MatchString(p) {
			t.Fatalf("pattern must NOT match %q", p)
		}
	}
}
