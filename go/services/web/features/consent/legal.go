// legal — the /legal page (the cookie banner's privacy/cookie-policy link
// target; the CE build ships no upstream legal copy — SaaS-only surface —
// so this is a minimal, self-contained static page, deliberately NOT a
// parity port: there is no Node /legal in this build to port from).
//
// Contract
//
//	GET /legal        → 200 text/html  (NoLogin — public, like every page
//	GET /legal/       the banner can point at; anchors: #privacy, #cookies)
//	GET /Legal, /LEGAL (Express-style case + trailing-slash tolerance)
//
// The page carries NO scripts and NO inline styles, so it runs clean
// under the app's default CSP (CSPDefaultPolicy: default-src 'none')
// without a view nonce.
package consent

import (
	"io"
	"regexp"

	"ollitex/go/services/web/core"
)

var legalPageRe = regexp.MustCompile(`^/(?i:legal)/?$`)

const legalPageHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Legal notice — privacy &amp; cookies</title>
</head>
<body>
<main>
<h1>Legal notice</h1>
<p>This page documents the privacy-relevant behaviour of this installation
(privacy policy and cookie policy). It is deliberately small: this is a
self-hosted installation, so it describes only what this installation does.</p>

<section id="privacy">
<h2>Privacy policy</h2>
<ul>
<li>Account data: your email address and account settings are stored by this
installation and used to provide the service. Project contents are stored
under your projects and shared only with collaborators you invite.</li>
<li>Server logs: standard access and error logs are kept by the operators of
this installation, per their own policies.</li>
<li>This installation does not sell or share account data with third parties.</li>
</ul>
</section>

<section id="cookies">
<h2>Cookie policy</h2>
<p>This installation uses cookies for:</p>
<ul>
<li><strong>Essential</strong> — session management, CSRF protection, and
security. These are always on and cannot be switched off.</li>
<li><strong>Consent</strong> — the <code>oa</code> cookie records the choice
you make in the cookie banner. <code>oa=1</code> means you allow
non-essential analytics/telemetry cookies; <code>oa=0</code> means you allow
essential cookies only. The choice is stored for one year, applies to this
browser on this device, and is sent as <code>Secure</code> and
<code>SameSite=Lax</code>.</li>
</ul>
<p>If a non-essential tracker is enabled on this installation, it is loaded
only when your consent choice is <code>oa=1</code>; with <code>oa=0</code> (or
no choice made) no such tracker loads.</p>
<p><strong>Changing or withdrawing your choice:</strong> select the other
option in the cookie banner when it is shown, or clear the <code>oa</code>
cookie for this site in your browser settings — either resets the choice,
and the banner (or its effect) returns to its default state.</p>
</section>

<h2>Contact</h2>
<p>Questions about this page or this installation's data handling: contact
the operators of this installation.</p>
</main>
</body>
</html>
`

// legalPage — serve the static legal notice (no scripts/styles: the app's
// default CSP stays clean without a view nonce).
func legalPage(cxt *core.Cxt, res *core.Res) {
	w := res.W
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("ETag", core.EtagWeakBody(legalPageHTML))
	w.WriteHeader(200)
	_, _ = io.WriteString(w, legalPageHTML)
}
