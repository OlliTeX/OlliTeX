// Package i18n — the Go-side catalog seam (docs/go-i18n-evaluation.md §4).
//
// Scope discipline (owner decision 2026-09-28, ADOPT):
//   - Frontend locales (18 catalogs under locales/) stay on the frontend
//     pipeline — untouched here.
//   - This package serves the Go-rendered surfaces: the e-mail slots first
//     (German canary), the ~440 view strings later (§3.1 step 2), API error
//     strings NEVER (§3.2 — byte-pinned contract surface).
//   - Default locale "en" means byte-identical behavior when no bundle is
//     wired (core.App.I18n == nil) — e2e stacks keep their pinned bytes.
package i18n

import (
	"strconv"
	"strings"

	ni18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

// TFunc — the seam signature the renderers take (nil = English-only mode).
type TFunc func(locale, key string, vars map[string]string) (string, bool)

// Bundle — a resolved catalog with documented fallbacks:
//
//   - unknown locale → falls back to Default (en);
//   - unknown key    → the key ITSELF is returned with ok=false (safe
//     fallback, zero data loss — the caller can then use its own default);
//   - nil Bundle     → T returns ("", false) — pure English-bytes mode.
type Bundle struct {
	Default string
	Locales map[string]bool
	b       *ni18n.Bundle
}

// LocaleOf — the locale for a rendering decision: user.language first,
// then Accept-Language (first list item, q-values ignored — the CE
// decision surface is the language itself); normalized to the primary
// subtag ("de-AT" → "de"); empty → "" (the caller renders English
// bytes, unchanged behavior).
func LocaleOf(userLang, acceptLang string) string {
	if l := normalizeLocale(userLang); l != "" {
		return l
	}
	al := acceptLang
	if i := strings.Index(al, ","); i > 0 {
		al = al[:i]
	}
	if i := strings.Index(al, ";"); i > 0 {
		al = al[:i]
	}
	return normalizeLocale(al)
}

// normalizeLocale — primary subtag, lowercased, trimmed ("de-AT" → "de").
func normalizeLocale(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, "-_"); i > 0 {
		s = s[:i]
	}
	return s
}

// T — resolve key for locale with vars. ok=false means "fell back"
// (unknown locale, unknown key, or nil receiver). The returned string is
// the resolved text, or the key itself when the key is unknown.
func (r *Bundle) T(locale, key string, vars map[string]string) (string, bool) {
	if r == nil || key == "" {
		return "", false
	}
	locs := []string{strings.ToLower(strings.TrimSpace(locale)), r.Default}
	var data map[string]interface{}
	if len(vars) > 0 {
		data = make(map[string]interface{}, len(vars))
		for k, v := range vars {
			data[k] = v
		}
	}
	cfg := &ni18n.LocalizeConfig{MessageID: key, TemplateData: data}
	// CLDR plural selection: nicksyder core takes the count from an explicit
	// PluralCount (not TemplateData). vars["n"] (numeric) → PluralCount;
	// non-numeric or absent → "other" form (documented).
	if nv, okn := vars["n"]; okn {
		if i, perr := strconv.ParseInt(nv, 10, 64); perr == nil {
			cfg.PluralCount = i // nicksnyder plural operand: integer or string only
		}
	}
	s, err := ni18n.NewLocalizer(r.b, locs...).Localize(cfg)
	if err != nil {
		return key, false
	}
	return s, true
}
