// Package views — /admin shell page (U9) builder.
package views

import (
	"io"
	"net/http"
	"strings"

	"ollitex/go/services/web/core"
)

// AdminShellParams — the dynamic inputs of the /admin page. Everything else
// is pinned byte-exact in adminShellTemplate (Node oracle, U9 2026-09-22).
type AdminShellParams struct {
	Nonce          string
	CSRF           string
	Email          string
	UID            string
	OverallTheme   string // user.ace.overallTheme ?? '' (Node: ?? '' — empty default!)
	Origin         string // site URL
	CurrentURL     string // requested path (path-aware alternate link)
	Exposed        string // ol-ExposedSettings JSON (editorpages.ExposedSettingsJSON)
	LLMEnabled     bool
	SystemMessages []string // message.content strings, DB (natural) order
}

// llmTabHeaderON / OFF — Node: if (llmEnabled) +bookmarkable-tabset-header(
// 'llm-configuration', 'LLM Configuration') — the header is ABSENT when off.
const llmTabHeaderON = `<li role="presentation"><a class="nav-link" href="#llm-configuration" aria-controls="llm-configuration" role="tab" data-toggle="tab" data-ol-bookmarkable-tab="data-ol-bookmarkable-tab">LLM Configuration</a></li>`
const llmTabHeaderOFF = ``

// llmPaneON / OFF — admin/index.pug: if (llmEnabled) → p.text-muted + iframe;
// else → p.text-muted "LLM is disabled on this deployment (set
// LLM_ENABLED=true to enable)."
const llmPaneON = `<p class="text-muted">Site-wide LLM backend, model allowlist, and compliance review settings.</p><iframe src="/admin/llm/settings" title="LLM Configuration" style="width: 100%; height: calc(100vh - 280px); min-height: 480px; border: 0;"></iframe>`
const llmPaneOFF = `<p class="text-muted">LLM is disabled on this deployment (set LLM_ENABLED=true to enable).</p>`

// pugEscape — Pug `#{...}` HTML escaping (Pinned: & < > " ' →
// &amp; &lt; &gt; &quot; &#39;) — order matters (& first).
func pugEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// sysMessagesHTML — Node: each message in systemMessages →
// ul.system-messages > li.system-message.row-spaced #{message.content}
// (one <ul> per message, BEFORE the <hr> that follows the loop).
func sysMessagesHTML(msgs []string) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(`<ul class="system-messages"><li class="system-message row-spaced">`)
		b.WriteString(pugEscape(m))
		b.WriteString(`</li></ul>`)
	}
	return b.String()
}

// AdminShell renders the /admin page BODY (U9) — used by AdminShellPage and
// the unit tests.
func AdminShell(p AdminShellParams) string {
	t := adminShellTemplate
	hdr, pane := llmTabHeaderOFF, llmPaneOFF
	if p.LLMEnabled {
		hdr, pane = llmTabHeaderON, llmPaneON
	}
	// Deterministic token replacements (no placeholder overlaps).
	rep := func(s, old, new string) string { return strings.ReplaceAll(s, old, new) }
	t = rep(t, `__SYSMSGS__`, sysMessagesHTML(p.SystemMessages))
	t = rep(t, "__LLMPANE__", pane)
	t = rep(t, "__LLMHDR__", hdr)
	t = rep(t, `name="ol-csrfToken" content="__CSRF__"`, `name="ol-csrfToken" content="`+p.CSRF+`"`)
	t = rep(t, `value="__CSRF__"`, `value="`+p.CSRF+`"`)
	t = rep(t, `nonce="__NONCE__"`, `nonce="`+p.Nonce+`"`)
	t = rep(t, "__EMAIL__", p.Email)
	t = rep(t, `name="ol-user_id" content="__UID__"`, `name="ol-user_id" content="`+p.UID+`"`)
	t = rep(t, `name="ol-adminOverallTheme" content="__OVERALLTHEME__"`, `name="ol-adminOverallTheme" content="`+p.OverallTheme+`"`)
	t = rep(t, `href="__ORIGIN____CURRENTURL__"`, `href="`+p.Origin+p.CurrentURL+`"`)
	// ol-ExposedSettings — Pug HTML-escapes meta attribute values: the JSON's
	// double quotes are rendered &quot; (Node oracle 2026-09-22; the hub
	// template carries the same baked escaping).
	exposed := strings.NewReplacer("&", "&amp;", "\"", "&quot;").Replace(p.Exposed)
	return rep(t, `name="ol-ExposedSettings" data-type="json" content="__EXPOSED__"`, `name="ol-ExposedSettings" data-type="json" content="`+exposed+`"`)
}

// AdminShellPage writes the /admin response — Node wire (pinned live 2026-09-22
// on the e2e stack): helmet baseline (core App already sends it) + the
// React-layout CSP with the render nonce + Permissions-Policy + weak ETag +
// text/html 200 (same head set as views.Page — the existing gated pages).
func AdminShellPage(w http.ResponseWriter, p AdminShellParams) {
	html := AdminShell(p)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", cspReact(p.Nonce))
	w.Header().Set("Permissions-Policy", core.PinnedPermissionsPolicy)
	w.Header().Set("ETag", core.EtagWeakBody(html))
	w.WriteHeader(200)
	_, _ = io.WriteString(w, html)
}
