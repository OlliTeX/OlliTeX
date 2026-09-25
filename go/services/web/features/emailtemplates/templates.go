// Package emailtemplates — the /hub-managed email template registry
// (remember.md P7-post item 3: "Manage the email template under /hub admin
// settings (rebrand them templates to OlliTeX too). Text areas with save
// and reset to default").
//
// The Go web builds every outbound mail's subject/text/html inline at the
// call site (13 distinct message shapes across 11 callers). This package
// makes those shapes a NAMED registry:
//
//   - the DEFAULTS live here (owner 2026-09: rebranded to OlliTeX where
//     the Node-ported strings still said "Overleaf"; dynamic brand text
//     uses the {{app}} variable which resolves via the same
//     OVERLEAF_APP_NAME mechanism the callers already used — OlliTeX by
//     default),
//   - admin overrides live in Mongo (collection `emailtemplateoverrides`)
//     and win over the defaults field-by-field,
//   - every call site renders through Render(a, slot, vars), which
//     applies the override-or-default text then interpolates {{vars}}.
//
// Template syntax: {{var}} substitution (no conditionals). A template
// may reference only the variables listed in the slot's Vars — the
// admin API validates this at save time (400 on unknown variables), and
// a missing variable at render time interpolates to the empty string
// (documented; the vars map is built by the call site).
//
// Reset-to-default = delete the override document (or its field); the
// shipped Go defaults are the source of truth again.
package emailtemplates

import (
	"regexp"
	"sort"
	"strings"
)

// Template — one outbound mail shape (defaults; overrides are per-field).
type Template struct {
	Name    string   // registry key (stable; API path segment)
	Label   string   // human label for the /hub admin UI
	Help    string   // one-line description (UI)
	Vars    []string // the only allowed {{vars}} (canonical order)
	Subject string   // subject template (required)
	Text    string   // text/plain template (required)
	HTML    string   // text/html template; "" = the slot wraps its
	// rendered text in the documented per-slot rule (collab mails:
	// <p>escaped-text</p> — htmlFromText slots).
	HTMLLabel string // UI hint for the optional HTML field
}

// Registry — the 13 message shapes, in the stable API/UI order.
//
// Defaults are the OlliTeX-rebranded texts (owner item 3). The `app`
// variable carries the brand (OVERLEAF_APP_NAME or "OlliTeX") exactly
// like the appName() helpers the call sites used before the move.
var Registry = []Template{
	{
		Name:    "password-reset",
		Label:   "Password reset",
		Help:    "Sent when a user requests a password reset.",
		Vars:    []string{"app", "link"},
		Subject: "Password Reset - {{app}}",
		Text:    "We got a request to reset your {{app}} password.\n\nReset password: {{link}}\n\nIf you ignore this message, your password won't be changed.\nIf you didn't request a password reset, let us know.",
		HTML: "<p>We got a request to reset your {{app}} password.</p>" +
			"<p><a href=\"{{link}}\">Reset password</a></p>" +
			"<p>If you ignore this message, your password won't be changed.<br>If you didn't request a password reset, let us know.</p>",
		HTMLLabel: "text/html part (HTML tags allowed)",
	},
	{
		Name:      "instance-stats-test",
		Label:     "Instance statistics alert (test)",
		Help:      "Sent from the site settings Instance Statistics test button.",
		Vars:      []string{"app"},
		Subject:   "[{{app}}] Instance stats alert test",
		Text:      "This is a test email from the Instance Statistics alert configuration.",
		HTML:      "<p>This is a test email from the Instance Statistics alert configuration.</p>",
		HTMLLabel: "text/html part (HTML tags allowed)",
	},
	{
		Name:    "sessions-cleared",
		Label:   "Security note: active sessions cleared",
		Help:    "Security alert when all other active sessions are cleared.",
		Vars:    []string{"app", "datetime", "email", "guideUrl"},
		Subject: "{{app}} security note: active sessions cleared",
		Text:    "Hi there,\n\nActive sessions cleared\n\n{{datetime}}\n\nactive sessions were cleared on your account {{email}}\n\nQuick guide: {{guideUrl}}\n\nThanks,\n{{app}} Team\n",
		HTML: "<html><body><h1>Active sessions cleared</h1>" +
			"<p>{{datetime}}</p>" +
			"<p>active sessions were cleared on your account {{email}}</p>" +
			"<p><a href=\"{{guideUrl}}\">quick guide</a></p>" +
			"</body></html>",
		HTMLLabel: "text/html part (HTML tags allowed)",
	},
	{
		Name:    "activate-account",
		Label:   "Activate account",
		Help:    "Sent when an account is created for a user (registration or admin-created).",
		Vars:    []string{"app", "email", "link", "adminEmail", "site"},
		Subject: "Activate your {{app}} Account",
		Text: "Hi,\n\n" +
			"Congratulations, you've just had an account created for you on {{app}}" +
			" with the email address '{{email}}'.\n\n" +
			"Click here to set your password and log in:\n\n" +
			"Set password: {{link}}\n\n" +
			"If you have any questions or problems, please contact {{adminEmail}}\n\n" +
			"Regards,\nThe {{app}} Team - {{site}}\n",
		HTML: "<p>Hi,</p>" +
			"<p>Congratulations, you've just had an account created for you on {{app}}" +
			" with the email address '{{email}}'.</p>" +
			"<p>Click here to set your password and log in:</p>" +
			"<p><a href=\"{{link}}\">Set password</a></p>" +
			"<p>If you have any questions or problems, please contact {{adminEmail}}</p>" +
			"<p>Regards,<br/>The {{app}} Team - {{site}}</p>",
		HTMLLabel: "text/html part (HTML tags allowed)",
	},
	{
		Name:    "mail-config-test",
		Label:   "E-mail configuration test",
		Help:    "Sent from the site settings E-mail test button.",
		Vars:    []string{"app", "sentAt", "via"},
		Subject: "[{{app}}] E-mail configuration test",
		Text: "This is a test e-mail sent from the {{app}} admin console " +
			"(Manage Site → E-mail).\n\nSent at: {{sentAt}}\nVia: {{via}}.",
		HTML: "<p>This is a test e-mail sent from the {{app}} admin console " +
			"(Manage Site → E-mail).</p>" +
			"<p>Sent at: {{sentAt}} via <code>{{via}}</code></p>",
		HTMLLabel: "text/html part (HTML tags allowed)",
	},
	{
		Name:    "collab-access-requested",
		Label:   "Access request (owner notified)",
		Help:    "Sent to the project owner when someone requests access.",
		Vars:    []string{"app", "first", "last", "email", "roleWord", "project"},
		Subject: "{{first}} {{last}} ({{email}}) requested {{roleWord}} access to {{project}} - {{app}}",
		Text:    "{{first}} {{last}} ({{email}}) requested {{roleWord}} access to {{project}} - {{app}}",
	},
	{
		Name:    "collab-access-declined",
		Label:   "Access request declined",
		Help:    "Sent to the requester when their access request is declined.",
		Vars:    []string{"app", "project"},
		Subject: "Your access request to {{project}} was declined - {{app}}",
		Text:    "Your access request to {{project}} was declined - {{app}}",
	},
	{
		Name:    "collab-access-granted",
		Label:   "Access request granted",
		Help:    "Sent to the requester when their access request is granted.",
		Vars:    []string{"app", "project"},
		Subject: "Your access request to {{project}} was granted - {{app}}",
		Text:    "Your access request to {{project}} was granted - {{app}}",
	},
	{
		Name:    "ownership-transfer",
		Label:   "Project ownership transfer",
		Help:    "Sent to both parties when project ownership is transferred.",
		Vars:    []string{"app"},
		Subject: "Project ownership transfer - {{app}}",
		Text:    "Project ownership transfer - {{app}}",
	},
	{
		Name:      "project-invite",
		Label:     "Project invite",
		Help:      "Sent to the invitee when a project is shared with them.",
		Vars:      []string{"app", "project", "owner", "url", "site"},
		Subject:   "\"{{project}}\" — shared by {{owner}}",
		Text:      projectInviteText,
		HTML:      projectInviteHTML,
		HTMLLabel: "text/html part (full branded HTML)",
	},
	{
		Name:    "security-note",
		Label:   "Security note (account change)",
		Help:    "Generic security alert for account-affecting actions.",
		Vars:    []string{"app", "action", "description", "adminEmail"},
		Subject: "{{app}} security note: {{action}}",
		Text: "Hi,\n\n" +
			"{{action}}.\n\n" +
			"{{description}}.\n\n" +
			"This is just a security notification — no action is required.\n" +
			"If you did not do this, please contact {{adminEmail}} immediately.\n",
		HTML: "<p>Hi,</p>" +
			"<p>{{action}}.</p>" +
			"<p>{{description}}.</p>" +
			"<p>This is just a security notification — no action is required.</p>" +
			"<p>If you did not do this, please contact {{adminEmail}} immediately.</p>",
		HTMLLabel: "text/html part (HTML tags allowed)",
	},
	{
		Name:    "git-token",
		Label:   "Git token generated",
		Help:    "Security alert when a Git authentication token is created.",
		Vars:    []string{"app", "email"},
		Subject: "{{app}} security note: new Git authentication token generated",
		Text: "A new Git authentication token has been generated for your account {{email}}. " +
			"If you did not do this, disable the token in your account settings and " +
			"change your password as soon as possible.",
	},
	{
		Name:    "test-mail",
		Label:   "Test e-mail",
		Help:    "Sent by the test-e-mail endpoints (notifications / launchpad).",
		Vars:    []string{"app", "site"},
		Subject: "A Test Email from {{app}}",
		Text:    "Hi,\n\nThis is a test Email from {{app}}\n\nOpen {{app}}: {{site}}\n\nRegards,\nThe {{app}} Team - {{site}}\n",
		HTML: "<p>Hi,</p>" +
			"<p>This is a test Email from {{app}}</p>" +
			"<p><a href=\"{{site}}\">Open {{app}}</a></p>" +
			"<p>Regards,<br/>The {{app}} Team - {{site}}</p>",
		HTMLLabel: "text/html part (HTML tags allowed)",
	},
}

var (
	slotByName = func() map[string]Template {
		m := make(map[string]Template, len(Registry))
		for _, t := range Registry {
			m[t.Name] = t
		}
		return m
	}()

	varRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)
)

// Lookup — registry lookup (ok = known slot).
func Lookup(name string) (Template, bool) {
	t, ok := slotByName[name]
	return t, ok
}

// Names — the registry slots in order (API/UI list order).
func Names() []string {
	out := make([]string, 0, len(Registry))
	for _, t := range Registry {
		out = append(out, t.Name)
	}
	return out
}

// VarsOf — canonical variable list for a slot ("" for unknown slots).
func VarsOf(name string) []string {
	if t, ok := slotByName[name]; ok {
		return t.Vars
	}
	return nil
}

// RenderResult — one rendered message part set (defaults-or-overrides).
type RenderResult struct {
	Subject string
	Text    string
	HTML    string // "" for htmlFromText slots: the caller applies its
	// documented wrapper (collab mails: <p>escaped-text</p>).
	HTMLDerived bool // true when HTML=="" carries the "caller wraps" rule
}

// Render — resolve a slot's current effective templates (override wins
// per field) and interpolate {{vars}}. vars may omit values (they render
// as empty strings; call sites pass their actual values).
//
// The brand variable `app` is NOT auto-filled: the call site passes it
// (the same OVERLEAF_APP_NAME/OlliTeX resolution as before), keeping the
// renderer pure and testable.
func Render(slots []Template, overrides map[string]Override, name string, vars map[string]string) (RenderResult, error) {
	t, ok := lookup(slots, name)
	if !ok {
		return RenderResult{}, ErrUnknownSlot
	}
	ov := overrides[name]
	fields := []string{"subject", "text", "html"}
	got := map[string]string{}
	for _, f := range fields {
		v := overrideField(ov, f)
		if v == "" {
			v = defaultField(t, f)
		}
		got[f] = v
	}
	if err := checkVars(t, got); err != nil {
		return RenderResult{}, err
	}
	sub := interpolate(got["subject"], vars)
	text := interpolate(got["text"], vars)
	html := interpolate(got["html"], vars)
	driven := t.HTML == ""
	return RenderResult{
		Subject:     sub,
		Text:        text,
		HTML:        html,
		HTMLDerived: driven,
	}, nil
}

// lookup — indirection so tests can substitute a registry.
func lookup(slots []Template, name string) (Template, bool) {
	for _, t := range slots {
		if t.Name == name {
			return t, true
		}
	}
	return Template{}, false
}

func defaultField(t Template, field string) string {
	switch field {
	case "subject":
		return t.Subject
	case "text":
		return t.Text
	case "html":
		return t.HTML
	}
	return ""
}

// checkVars — every {{var}} referenced in the effective templates must be
// one of the slot's declared Vars (admin-API save-time + render-time
// guard against a broken mail).
func checkVars(t Template, got map[string]string) error {
	allowed := map[string]bool{}
	for _, v := range t.Vars {
		allowed[v] = true
	}
	bad := map[string]bool{}
	for _, f := range []string{"subject", "text", "html"} {
		for _, m := range varRe.FindAllStringSubmatch(got[f], -1) {
			if !allowed[m[1]] {
				bad[m[1]] = true
			}
		}
	}
	if len(bad) == 0 {
		return nil
	}
	names := make([]string, 0, len(bad))
	for k := range bad {
		names = append(names, k)
	}
	sort.Strings(names)
	return &VarError{Unknown: names, Allowed: t.Vars}
}

// VarError — a template references variables the slot does not define.
type VarError struct {
	Unknown []string
	Allowed []string
}

func (e *VarError) Error() string {
	return "unknown variables " + strings.Join(e.Unknown, ", ") +
		" (allowed: " + strings.Join(e.Allowed, ", ") + ")"
}

// interpolate — replace {{var}} with vars[var] ("" when absent).
func interpolate(tpl string, vars map[string]string) string {
	if tpl == "" {
		return ""
	}
	return varRe.ReplaceAllStringFunc(tpl, func(m string) string {
		key := varRe.FindStringSubmatch(m)[1]
		return vars[key] // "" when the caller omitted it
	})
}

// CollabHTML — the documented wrapper for htmlFromText slots. Parity
// with the ported Node behavior (collab.go): exactly `&` → `&amp;`
// (and nothing else — the original replaced only '&').
func CollabHTML(text string) string {
	return "<p>" + strings.ReplaceAll(text, "&", "&amp;") + "</p>"
}

// ErrUnknownSlot — Render for a slot name absent from the registry.
var ErrUnknownSlot = errUnknownSlot{}

type errUnknownSlot struct{}

func (errUnknownSlot) Error() string { return "unknown email template slot" }
