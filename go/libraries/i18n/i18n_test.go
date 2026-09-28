package i18n

import (
	"testing"
)

// Locale-matrix gate (docs/go-i18n-evaluation.md §4 test strategy):
// en/de × every canary key referenced by the seams, plus the fallback
// chain and the ICU plural pin. Oracle-pinned bytes.

const dir = "./locales"

var mustBundle = func() *Bundle {
	b, err := NewBundleFromJSONDir(dir)
	if err != nil {
		panic(err)
	}
	return b
}()

func TestT_EN_Identity(t *testing.T) {
	got, ok := mustBundle.T("en", "email.sessions-cleared.subject", nil)
	if !ok {
		t.Fatalf("en identity lookup failed (ok=false)")
	}
	want := "{{app}} security note: active sessions cleared"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestT_DE_Canary(t *testing.T) {
	got, ok := mustBundle.T("de", "email.sessions-cleared.subject", nil)
	if !ok {
		t.Fatalf("de canary lookup failed (ok=false)")
	}
	want := "{{app}} Sicherheitshinweis: aktive Sitzungen wurden entfernt"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestT_UnknownLocale_FallsBackToEN(t *testing.T) {
	got, ok := mustBundle.T("xx-XX", "email.sessions-cleared.subject", nil)
	if !ok {
		t.Fatalf("unknown locale must fall back to en (ok=false, but with en text)")
	}
	want := "{{app}} security note: active sessions cleared"
	if got != want {
		t.Fatalf("fallback mismatch: %q", got)
	}
}

func TestT_UnknownLocaleSubtag_FallsBack(t *testing.T) {
	got, _ := mustBundle.T("de-AT", "email.sessions-cleared.subject", nil)
	want := "{{app}} Sicherheitshinweis: aktive Sitzungen wurden entfernt"
	if got != want {
		t.Fatalf("de-AT should resolve de: %q", got)
	}
}

func TestT_UnknownKey_ReturnsKey(t *testing.T) {
	got, ok := mustBundle.T("de", "email.no-such-slot.subject", nil)
	if ok {
		t.Fatalf("unknown key must be ok=false")
	}
	if got != "email.no-such-slot.subject" {
		t.Fatalf("unknown key must return the key itself, got %q", got)
	}
}

func TestT_NilBundle(t *testing.T) {
	var r *Bundle
	got, ok := r.T("de", "email.sessions-cleared.subject", nil)
	if ok || got != "" {
		t.Fatalf("nil bundle must be (%q, false)", got)
	}
}

// CLDR plural-form pin (nicksnyder core = CLDR forms, not ICU):
func TestT_PLURAL_FormSelection_PIN(t *testing.T) {
	cases := []struct {
		loc  string
		n    string
		want string
	}{
		{"en", "0", "recent days"}, // en CLDR has no 'zero' category → other
		{"en", "1", "yesterday"},
		{"en", "5", "recent days"},
		{"de", "0", "vor Kurzem"},
		{"de", "1", "gestern"},
		{"de", "5", "vor Kurzem"},
		{"de-DE", "1", "gestern"},
		{"fr", "1", "yesterday"}, // fr not in catalog → en fallback
	}
	for _, c := range cases {
		got, ok := mustBundle.T(c.loc, "rel.time-days", map[string]string{"n": c.n})
		if !ok {
			t.Fatalf("plural resolve failed for %s n=%s (ok=false)", c.loc, c.n)
		}
		if got != c.want {
			t.Fatalf("plural[%s n=%s]\n got  %q\n want %q", c.loc, c.n, got, c.want)
		}
	}
}

func TestLocaleOf_PIN(t *testing.T) {
	cases := []struct {
		user, accept, want string
	}{
		{"de-DE", "", "de"},
		{"", "fr,de;q=0.9", "fr"},
		{"", "de-AT;q=1.0", "de"},
		{"", "", ""},
		{"  de ", "fr", "de"},
	}
	for _, c := range cases {
		if got := LocaleOf(c.user, c.accept); got != c.want {
			t.Fatalf("LocaleOf(%q,%q) = %q, want %q", c.user, c.accept, got, c.want)
		}
	}
}
