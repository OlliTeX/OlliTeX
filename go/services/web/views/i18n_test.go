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
	set := WaveALaunchpadAll()
	for k, eng := range set {
		en, okEN := b.T("en", k, nil)
		if !okEN || en != eng {
			t.Fatalf("en catalog[%q]=%q(ok=%v) must equal the Go set %q", k, en, okEN, eng)
		}
		if _, ok := b.T("de", k, nil); !ok {
			t.Fatalf("de catalog missing key %q", k)
		}
	}
}
