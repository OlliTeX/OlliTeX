package emailtemplates

import (
	"testing"

	i18nlib "ollitex/go/libraries/i18n"
	"ollitex/go/services/web/core"
)

// i18n canary gate (docs/go-i18n-evaluation.md §4): the sessions-cleared
// e-mail slot through RenderForLocale. Oracle-pinned bytes.

const canaryDir = "../../../../libraries/i18n/locales"

var canaryBundle = func() *i18nlib.Bundle {
	b, err := i18nlib.NewBundleFromJSONDir(canaryDir)
	if err != nil {
		panic(err)
	}
	return b
}()

var canaryVars = map[string]string{
	"app":      "Ollitex",
	"datetime": "Monday 26 January 2026",
	"email":    "user@example.test",
	"guideUrl": "https://example.test/guide",
}

func deApp() *core.App { return &core.App{I18n: canaryBundle} }

func TestRenderForLocale_CANARY_DE(t *testing.T) {
	rr, err := RenderForLocale(deApp(), "sessions-cleared", canaryVars, "de-DE", "")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	wantSubj := "Ollitex Sicherheitshinweis: aktive Sitzungen wurden entfernt"
	if rr.Subject != wantSubj {
		t.Fatalf("subject\n got  %q\n want %q", rr.Subject, wantSubj)
	}
	wantTextHead := "Hallo,\n\nAktive Sitzungen entfernt\n\nMonday 26 January 2026\n\nAuf Ihrem Konto user@example.test wurden die aktiven Sitzungen entfernt."
	if rr.Text[:len(wantTextHead)] != wantTextHead {
		t.Fatalf("text head\n got  %q", rr.Text[:min(len(rr.Text), len(wantTextHead))])
	}
	// interpolation applied to the translated bytes
	if !contains(rr.Text, "Kurzanleitung: https://example.test/guide") {
		t.Fatalf("interpolation missing in de text: %q", rr.Text)
	}
}

func TestRenderForLocale_EN_Identity_PIN(t *testing.T) {
	// en locale → EXACTLY the RenderFor English bytes (canary must not
	// perturb the default pipeline even with a bundle wired).
	rr1, err1 := RenderForLocale(deApp(), "sessions-cleared", canaryVars, "en", "")
	rr0, err0 := RenderFor(deApp(), "sessions-cleared", canaryVars)
	if err1 != nil || err0 != nil {
		t.Fatalf("errors: %v %v", err1, err0)
	}
	if rr1 != rr0 {
		t.Fatalf("en identity broken:\n got  %+v\n want %+v", rr1, rr0)
	}
}

func TestRenderForLocale_NilBundle_KeepsEnglish(t *testing.T) {
	a := &core.App{} // I18n nil (default production mode)
	rr1, err1 := RenderForLocale(a, "sessions-cleared", canaryVars, "de-DE", "de-DE,de;q=0.9")
	rr0, err0 := RenderFor(a, "sessions-cleared", canaryVars)
	if err1 != nil || err0 != nil {
		t.Fatalf("errors: %v %v", err1, err0)
	}
	if rr1 != rr0 {
		t.Fatalf("nil bundle must keep English bytes:\n got  %+v\n want %+v", rr1, rr0)
	}
}

func TestRenderForLocale_OverrideBeatsCatalog(t *testing.T) {
	// admin override is instance intent — wins over the catalog per-field.
	rr, err := renderLocalized(Override{Subject: "OVERRIDE SUBJECT"}, "sessions-cleared", canaryVars, "de", canaryBundle.T)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if rr.Subject != "OVERRIDE SUBJECT" {
		t.Fatalf("override must win: %q", rr.Subject)
	}
	// text not overridden → catalog de
	if !contains(rr.Text, "Auf Ihrem Konto user@example.test") {
		t.Fatalf("non-overridden field must come from catalog: %q", rr.Text[:120])
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
