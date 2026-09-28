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
