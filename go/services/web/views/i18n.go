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
	"ollitex/go/services/web/core"
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
	// LONGEST English source first: "Log in now" must be replaced before
	// its substring "Log in" (and friends) — key order as deterministic
	// tie-break.
	sort.Slice(keys, func(i, j int) bool {
		li, lj := len(p.Strings[keys[i]]), len(p.Strings[keys[j]])
		if li != lj {
			return li > lj
		}
		return keys[i] < keys[j]
	})
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

// WaveBSharedStrings — the site chrome shared by every baked page:
// cookie banner + footer attribution (the navbar set is Wave A's).
var WaveBSharedStrings = map[string]string{
	"view.cookie.text":        "We use cookies to improve your experience on our site, for analytics, and to support marketing, which may involve the sharing of data. You can find out more in our",
	"view.cookie.accept":      "Accept all cookies",
	"view.cookie.essential":   "Essential cookies only",
	"view.cookie.policy":      "cookie policy",
	"view.footer.agpl":        "(open source, AGPLv3)",
	"view.footer.cec":         "Overleaf Community Edition",
	"view.404.title":          "Not found",
	"view.404.message":        "Sorry, we can’t find the page you are looking for.",
	"view.404.home":           "Home",
	"view.restricted.message": "Restricted, sorry you don’t have permission to load this page.",
	"view.logout.cancel":      "Cancel",
	"view.logout.title":       "Log out of OlliTeX",
}

// WaveBSetPasswordStrings — the /set-password page set (p2 family).
var WaveBSetPasswordStrings = map[string]string{
	"view.setpw.heading":       "Create a new password for your account.",
	"view.setpw.label":         "New password",
	"view.setpw.rules-intro":   "To help keep your account secure, make sure your new password:",
	"view.setpw.rule-len":      "is at least 8 characters long",
	"view.setpw.rule-leak":     "is not used on any other website",
	"view.setpw.rule-email":    "does not contain or significantly match your email",
	"view.setpw.leak-note":     "This password was detected on a",
	"view.setpw.leak-list":     "public list of known compromised passwords",
	"view.setpw.invalid":       "Invalid Password..",
	"view.setpw.same-current":  "Password can’t be the same as current one.",
	"view.setpw.changed":       "Your password has been successfully changed.",
	"view.setpw.updated":       "Password updated.",
	"view.setpw.login-now":     "Log in now",
	"view.setpw.expired":       "Your password reset token has expired. Please request a new password reset email and follow the link there.",
	"view.setpw.request-again": "Request a new password reset email",
	"view.setpw.reset-heading": "Reset your password",
	"view.setpw.set-new":       "Set new password",
}

// WaveBSessionsStrings — the /user/sessions page set (p3c family).
var WaveBSessionsStrings = map[string]string{
	"view.sessions.your":          "Your Sessions",
	"view.sessions.current":       "Current Session",
	"view.sessions.other":         "Other Sessions",
	"view.sessions.none-other":    "No other sessions active",
	"view.sessions.created":       "Session Created At",
	"view.sessions.ip":            "IP Address",
	"view.sessions.clear":         "Clear sessions",
	"view.sessions.cleared":       "Sessions cleared",
	"view.sessions.back":          "Back to account settings",
	"view.sessions.back-projects": "Back to your projects",
}

// WaveBShellAll — the full wave-A+B set for the site shell pages
// (login-adjacent, 404/500/restricted, logout, set-password, sessions).
func WaveBShellAll() map[string]string {
	out := map[string]string{}
	for k, v := range WaveANavStrings {
		out[k] = v
	}
	for _, s := range []map[string]string{WaveBSharedStrings, WaveBSetPasswordStrings, WaveBSessionsStrings} {
		for k, v := range s {
			out[k] = v
		}
	}
	return out
}

// ShellI18n — the wave-A+B locale pass for a shell page (nav + cookie/
// footer + 404/500/restricted + logout + set-password + sessions). One
// line per feature builder: d.I18n = views.ShellI18n(cxt.A, cxt).
// a or a.I18n nil → zero I18nPage → EXACT English bytes (e2e pins).
func ShellI18n(a *core.App, cxt *core.Cxt) I18nPage {
	if a == nil || a.I18n == nil {
		return I18nPage{}
	}
	return I18nPage{Locale: a.PageLocale(cxt), T: a.I18n.T, Strings: WaveBShellAll()}
}
