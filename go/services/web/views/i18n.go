package views

// i18n wave A — Go-rendered shell strings through the catalog seam
// (docs/go-i18n-evaluation.md §3.1 item 2, ADOPT 2026-09-28).
//
// The baked page HTML is byte-pinned to the Node oracle (EN). The locale
// pass runs AFTER slot finalization and replaces a small, explicit string
// set (English source → catalog translation). Guarantees:
//
//   - nil TFunc / empty/"en" locale / empty set → the EXACT finalized
//     bytes (e2e pins stay byte-identical);
//   - unknown key → the library returns ok=false → the English bytes
//     stay (zero data loss);
//   - the English source text is matched exactly (case-sensitive) — no
//     regexes, no partial-word surprises beyond the documented set.

import (
	"sort"
	"strings"

	i18nlib "ollitex/go/libraries/i18n"
)

// I18nPage — the per-request locale pass for a baked page.
type I18nPage struct {
	Locale  string            // resolved request locale (""/"en" = unchanged)
	T       i18nlib.TFunc     // nil = English-bytes mode
	Strings map[string]string // key → English display text (the wave set)
}

func (p I18nPage) enabled() bool {
	return p.Locale != "" && p.Locale != "en" && p.T != nil && len(p.Strings) > 0
}

// translateI18n — apply the wave set to the finalized HTML.
func translateI18n(html string, p I18nPage) string {
	if !p.enabled() {
		return html
	}
	keys := make([]string, 0, len(p.Strings))
	for k := range p.Strings {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic replacement order
	for _, k := range keys {
		eng := p.Strings[k]
		if eng == "" {
			continue
		}
		tr, ok := p.T(p.Locale, k, nil)
		if ok && tr != eng {
			html = strings.ReplaceAll(html, eng, tr)
		}
	}
	return html
}

// WaveANavStrings — the shared navbar set (appears on every baked page
// that carries the site nav): key → English source.
var WaveANavStrings = map[string]string{
	"view.nav.skip":             "Skip to content",
	"view.nav.admin":            "Admin",
	"view.nav.manage-site":      "Manage Site",
	"view.nav.manage-users":     "Manage Users",
	"view.nav.project-lookup":   "Project/Object Lookup",
	"view.nav.llm":              "LLM Settings",
	"view.nav.library":          "Library",
	"view.nav.templates":        "Templates",
	"view.nav.projects":         "Projects",
	"view.nav.account":          "Account",
	"view.nav.account-settings": "Account settings",
	"view.nav.ai-settings":      "AI Settings",
	"view.nav.log-out":          "Log Out",
	"view.nav.log-in":           "Log in",
}

// WaveALaunchpadStrings — the /launchpad page set (admin + fresh variants
// share the navbar set above; this is the page-specific part).
var WaveALaunchpadStrings = map[string]string{
	"view.launchpad.welcome":     "Welcome to OlliTeX",
	"view.launchpad.status":      "Status Checks",
	"view.launchpad.websockets":  "WebSockets",
	"view.launchpad.checking":    "Checking",
	"view.launchpad.ok":          "OK",
	"view.launchpad.retry":       "Retry",
	"view.launchpad.error":       "Error",
	"view.launchpad.other":       "Other Actions",
	"view.launchpad.test-email":  "Send a test email",
	"view.launchpad.email":       "Email",
	"view.launchpad.send":        "Send",
	"view.launchpad.sending":     "Sending…",
	"view.launchpad.to-admin":    "Go To Admin Panel",
	"view.launchpad.start":       "Start Using OlliTeX",
	"view.launchpad.first-admin": "Create the first Admin account",
	"view.launchpad.ldap-para":   "Choose an email address for the first OlliTeX admin account. This should correspond to an account in the LDAP system. You will then be asked to log in with this account.",
	"view.launchpad.local-title": "Local account",
	"view.launchpad.local-para":  "Alternatively, you can create OlliTeX local admin account.",
	"view.launchpad.password":    "Password",
	"view.launchpad.register":    "Register",
	"view.launchpad.registering": "Registering…",
}

// WaveALaunchpadAll — nav + page set for the launchpad renders.
func WaveALaunchpadAll() map[string]string {
	out := map[string]string{}
	for k, v := range WaveANavStrings {
		out[k] = v
	}
	for k, v := range WaveALaunchpadStrings {
		out[k] = v
	}
	return out
}
