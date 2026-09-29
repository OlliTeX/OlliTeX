package views

// i18n wave A — locale-matrix gates (docs/go-i18n-evaluation.md §3.1-2).
//
// Invariants pinned here:
//  1. English bytes: nil bundle / empty locale / "en" → the finalized
//     HTML is byte-identical (the e2e pins rely on this).
//  2. German page: a de request renders the catalog's German for every
//     wave-A string present in the launchpad page.
//  3. Catalog integrity: every wave-A key exists in BOTH locales and the
//     en values equal the Go string set (single source of truth check).

import (
	"strings"
	"testing"

	"ollitex/go/libraries/i18n"
)

func TestTranslateI18nDisabledModes(t *testing.T) {
	html := "<div>Welcome to OlliTeX</div>"
	if got := translateI18n(html, I18nPage{}); got != html {
		t.Fatalf("zero page must be byte-identical: %q", got)
	}
	if got := translateI18n(html, I18nPage{Locale: "en", T: func(l, k string, v map[string]string) (string, bool) { return "X", true }}); got != html {
		t.Fatalf("en locale must be byte-identical: %q", got)
	}
	if got := translateI18n(html, I18nPage{Locale: "de", Strings: WaveALaunchpadAll()}); got != html {
		t.Fatalf("nil TFunc must be byte-identical: %q", got)
	}
}

func TestTranslateI18nGerman(t *testing.T) {
	b, err := i18n.NewBundleFromJSONDir("../../../libraries/i18n/locales")
	if err != nil {
		t.Fatal(err)
	}
	h := "<div class=\"text-center\"><h1>Welcome to OlliTeX</h1></div><h2>Create the first Admin account</h2><button>Log in</button>"
	got := translateI18n(h, I18nPage{Locale: "de", T: b.T, Strings: WaveALaunchpadAll()})
	for _, want := range []string{"Willkommen bei OlliTeX", "Erstes Admin-Konto erstellen", ">Anmelden</button>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in: %s", want, got)
		}
	}
	for _, gone := range []string{"Welcome to OlliTeX", "Log in</button>"} {
		if strings.Contains(got, gone) {
			t.Fatalf("English string should be replaced: %q in %s", gone, got)
		}
	}
}

func TestWaveACatalogIntegrity(t *testing.T) {
	b, err := i18n.NewBundleFromJSONDir("../../../libraries/i18n/locales")
	if err != nil {
		t.Fatal(err)
	}
	// Wave A+B: every shell + launchpad string key resolves in BOTH loaded
	// locales (en = identity — the byte-pinning contract; de = present),
	// and the two waves key disjointly (no ambiguous substitution).
	waveB := WaveBShellAll()
	sets := map[string]map[string]string{"waveA": WaveALaunchpadAll(), "waveB": waveB}
	for name, set := range sets {
		for k, eng := range set {
			en, okEN := b.T("en", k, nil)
			if !okEN || en != eng {
				t.Fatalf("%s en catalog[%q]=%q(ok=%v) must equal the Go set %q", name, k, en, okEN, eng)
			}
			if _, ok := b.T("de", k, nil); !ok {
				t.Fatalf("%s de catalog missing key %q", name, k)
			}
		}
	}
	// wave B must cover the navbar (it renders on every shell page:
	// admin + manage-site resolve in both locales).
	for _, k := range []string{"view.nav.admin", "view.nav.manage-site"} {
		if _, ok := waveB[k]; !ok {
			t.Fatalf("WaveBShellAll missing navbar key %q (navbar renders on shell pages)", k)
		}
	}
}

// --- i18n wave C gates (/admin shell + /read-only/one-time-login) -----------

func TestTranslateI18nGermanAdmin(t *testing.T) {
	b, err := i18n.NewBundleFromJSONDir("../../../libraries/i18n/locales")
	if err != nil {
		t.Fatal(err)
	}
	p := AdminShellParams{
		Nonce:          "N",
		CSRF:           "C",
		Email:          "x@e.test",
		UID:            "u1",
		OverallTheme:   "",
		Origin:         "http://h",
		CurrentURL:     "/admin",
		Exposed:        `{}`,
		LLMEnabled:     true,
		SystemMessages: []string{"hello"},
	}
	en := AdminShell(p)
	// en-mode invariance (the byte-pinned oracle behaviour)
	if got := translateI18n(en, I18nPage{}); got != en {
		t.Fatalf("zero-page admin must be byte-identical")
	}
	if got := translateI18n(en, I18nPage{Locale: "en", T: b.T, Strings: WaveBShellAll()}); got != en {
		t.Fatalf("en locale admin must be byte-identical")
	}
	de := translateI18n(en, I18nPage{Locale: "de", T: b.T, Strings: WaveBShellAll()})
	for _, want := range []string{
		"Admin-Panel",
		"Systemnachrichten",
		"Aktive Projekte",
		"Offene Sockets",
		"Editor öffnen/schließen",
		"Alle Nachrichten löschen",
		"Alle Benutzer trennen",
		"LLM-Konfiguration",
		"Site-weite LLM-Backend-Einstellungen",
		"Nachricht",
	} {
		if !strings.Contains(de, want) {
			t.Errorf("de admin missing %q", want)
		}
	}
	// untouched: technical markup survives translation (hrefs/srcs, data-attrs)
	for _, keep := range []string{"/admin/llm/settings", "data-ol-bookmarkable-tab", "#llm-configuration"} {
		if !strings.Contains(de, keep) {
			t.Errorf("de admin lost token %q", keep)
		}
	}
	// the LLM-OFF variant: the env-var token must survive into German
	p.OverallTheme = ""
	p.LLMEnabled = false
	enOff := AdminShell(p)
	deOff := translateI18n(enOff, I18nPage{Locale: "de", T: b.T, Strings: WaveBShellAll()})
	if !strings.Contains(deOff, "LLM ist in dieser Installation deaktiviert") {
		t.Errorf("de admin-off pane missing: %s", deOff)
	}
	if !strings.Contains(deOff, "LLM_ENABLED=true") {
		t.Errorf("de admin-off pane lost the env token: %s", deOff)
	}
}

func TestTranslateI18nGermanOneTime(t *testing.T) {
	b, err := i18n.NewBundleFromJSONDir("../../../libraries/i18n/locales")
	if err != nil {
		t.Fatal(err)
	}
	h := "<h1>We're back!</h1></div><p>Overleaf is now running normally.</p><p>Please\n<a href=\"/login\">log in</a>\nto continue working on your projects.</p>"
	en := h
	if got := translateI18n(en, I18nPage{}); got != en {
		t.Fatal("zero-page onetime must be byte-identical")
	}
	de := translateI18n(h, I18nPage{Locale: "de", T: b.T, Strings: WaveBShellAll()})
	if !strings.Contains(de, "Wir sind zurück!") {
		t.Errorf("de onetime missing h1: %s", de)
	}
	if !strings.Contains(de, "Overleaf funktioniert jetzt wieder normal.") {
		t.Errorf("de onetime missing p1: %s", de)
	}
	if !strings.Contains(de, `<a href="/login">anmelden</a>`) {
		t.Errorf("de onetime link must keep the href: %s", de)
	}
	if strings.Contains(de, "to continue working") {
		t.Errorf("de onetime left the English tail: %s", de)
	}
}

func TestWaveCCatalogIntegrity(t *testing.T) {
	for _, k := range []string{
		"view.admin.h1", "view.admin.tab-messages", "view.admin.tab-projects",
		"view.admin.tab-sockets", "view.admin.tab-editor", "view.admin.projects-help",
		"view.admin.message-label", "view.admin.clear-messages", "view.admin.disconnect",
		"view.admin.editor-note", "view.admin.llm-tab", "view.admin.llm-on", "view.admin.llm-off",
		"view.onetime.h1", "view.onetime.p1", "view.onetime.p2a", "view.onetime.login", "view.onetime.p2b",
	} {
		if _, ok := WaveBShellAll()[k]; !ok {
			t.Errorf("shell union missing %s", k)
		}
	}
	b, err := i18n.NewBundleFromJSONDir("../../../libraries/i18n/locales")
	if err != nil {
		t.Fatal(err)
	}
	// en catalog values must equal the Go set (single source of truth)
	for k, v := range WaveCAdminStrings {
		if got, ok := b.T("en", k, nil); !ok || got != v {
			t.Errorf("en catalog drift %s: %q vs %q", k, got, v)
		}
	}
	for k, v := range WaveCOneTimeStrings {
		if got, ok := b.T("en", k, nil); !ok || got != v {
			t.Errorf("en catalog drift %s: %q vs %q", k, got, v)
		}
	}
	// de must resolve every key (no MessageNotFoundErr on the wave-C surface)
	for _, set := range []map[string]string{WaveCAdminStrings, WaveCOneTimeStrings} {
		for k := range set {
			if _, ok := b.T("de", k, nil); !ok {
				t.Errorf("de catalog missing %s", k)
			}
		}
	}
}
