// Package views renders the Go-ported web shell pages with byte parity to
// the Node service (contract: pages_data.go, captured from the running
// stack). Static skeletons + per-request slot substitution:
//
//	\x01CSRF\x02  → session csrf token (csurf contract, P0 pin)
//	\x01NONCE\x02 → per-request base64 nonce (16 random bytes, std encoding)
//
// Env note: the skeletons embed the capture origin (e2e); the renderer
// rewrites it to the current request origin so any deployment renders the
// same page for its own host.
package views

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"strings"

	"ollitex/go/services/web/core"
)

const (
	slotCSRF    = "\x01CSRF\x02"
	slotNonce   = "\x01NONCE\x02"
	slotPath    = "\x01PATH\x02"
	slotOLUsers = "\x01OLUSERS\x02"
	slotOLUID   = "\x01OLUID\x02"
	// P2 slots (tools/webviews-capture-p2.py):
	slotP2User     = "\x01USER\x02"
	slotP2UserID   = "\x01USERID\x02"
	slotResetErr   = "\x01RESETERR\x02"
	slotEmailField = "\x01EMAIL\x02"
	slotResetToken = "\x01RSTOKEN\x02"
	slotPostURL    = "\x01POSTURL\x02"
	// P3.3 slots (tools/webviews-capture-p3c.py):
	slot33User     = "\x01USR33\x02"   // ol-user meta JSON (settings page)
	slot33HBS      = "\x01HBS33\x02"   // ,&quot;hasSamlBeta&quot;:... fragment (or empty)
	slot33HasPw    = "\x01HASPW33\x02" // " content" (true) or "" (false)
	slot33ShowAI   = "\x01AI33\x02"    // same bare-content boolean rule
	slot33SamlMeta = "\x01SAMLM33\x02" // ol-samlBeta content attribute (or none)
	slot33SsoMsg   = "\x01SSOM33\x02"
	slot33SyncOk   = "\x01SYNCO33\x02"
	slot33SyncErr  = "\x01SYNCX33\x02"
	slot33RefErr   = "\x01REFE33\x02"
	slot33CurrRow  = "\x01CURRROW33\x02"   // sessions page: current session <tr>
	slot33Rows     = "\x01OTHERROWS33\x02" // sessions page: other session <tr>s
	// P3.4 register page (/register is per-auth-state dynamic; skeleton is
	// the anonymous capture so these render empty/absent for anonymous and
	// fill from the session user when logged in):
	slotRegUsers = "\x01REGUSERS\x02" // ol-usersEmail content
	slotRegUID   = "\x01REGUID\x02"   // ol-user_id: `` or ` content="…"`
	slotRegSU    = "\x01REGSU\x02"    // navbar sessionUser fragment: `` or `,&quot;sessionUser&quot;:{&quot;email&quot;:&quot;…&quot;}`
	// P6.5 library pages (/library, /library/trashed):
	slotLibUsers = "\x01LIBUSERS\x02" // ol-userSettings content JSON (htmlAttrEsc'd at finalize)
	// P6.13 slot: ExposedSettings.canManageTemplatesMenu is PER-USER on
	// Node (ExpressLocals re-computes it per request); the captured
	// skeleton carried the capture user's `false`, so pages rendered
	// for a different-privilege user (e.g. admin 404) diverged.
	slotCanMgtTpl = "\x01CANMGTPL\x02"
	// P6.13 slot: admin navbar branch (Node renders the Admin dropdown
	// for site admins on EVERY page, including 404/403). Empty for
	// non-admins; the page skeleton otherwise matches the user nav.
	slotNavAdmin = "\x01NAVADMIN\x02"
	// origin captured from the e2e fixtures (rewritten per request).
	capturedOrigin = "http://127.0.0.1:7420"
)

// AdminNavFragment: the exact Node admin-nav <li> (captured from the Node
// 404 page render for a site admin; identical across pages).
const AdminNavFragment = `<li class="dropdown subdued" role="none"><button class="dropdown-toggle" aria-haspopup="true" aria-expanded="false" data-bs-toggle="dropdown" role="menuitem" event-tracking="menu-expand" event-tracking-mb="true" event-tracking-trigger="click" event-segmentation="{&quot;item&quot;:&quot;admin&quot;,&quot;location&quot;:&quot;top-menu&quot;}">Admin</button><ul class="dropdown-menu dropdown-menu-end" role="menu"><li role="none"><a class="dropdown-item" role="menuitem" href="/admin">Manage Site</a></li><li role="none"><a class="dropdown-item" role="menuitem" href="/admin/user">Manage Users</a></li><li role="none"><a class="dropdown-item" role="menuitem" href="/admin/project">Project/Object Lookup</a></li><li role="none"><a class="dropdown-item" role="menuitem" href="/admin/llm/settings">LLM Settings</a></li></ul></li>`

var nonceEnc = base64.StdEncoding

// Nonce = 16 random bytes, std base64 (22 chars) — same shape as Node's
// per-request CSP nonce (value is random; gates normalise it).
func NewNonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return nonceEnc.EncodeToString(b[:])
}

type PageData struct {
	CSRFToken string // session csrf token ("" → anonymous session must not appear here)
	Nonce     string
	Origin    string // "http://host" of the current request
	// CSP — Node sets a PER-LAYOUT policy (pinned live 2026-09-13):
	// React app pages (login/register) allow nonce-script + strict-dynamic;
	// plain views (logout/restricted/404) carry the restrictive policy.
	CSP        string
	AdminEmail string // general/500 contact (Settings.adminEmail, P3.1)
	// 404-only dynamic slots (the skeleton was captured logged-in):
	Path      string // request path (alternate link)
	UserEmail string // session user email (Node: ol-usersEmail + navbar pill)
	UserID    string // session user id (ol-user_id)
	// P2 dynamic slots (empty strings render the anonymous/absent shape):
	ResetErr   string // passwordReset meta: "" | "password_reset_token_expired"
	EmailField string // setPassword form email input
	ResetToken string // setPassword hidden token input
	PostURL    string // token page postUrl meta, e.g. /<token>/grant
	// P3.3 dynamic slots (userpages feature):
	UserMetaJSON                 string // settings page ol-user meta JSON (Node serializeUser order)
	SamlBeta                     string // session samlBeta ("" → ExposedSettings key + meta absent)
	HasPassword                  bool   // ol-hasPassword bare-content boolean meta
	ShowAiFeatures               bool   // ol-showAiFeatures bare-content boolean meta
	SsoErrorMessage              string // settings page pop-flag metas
	ProjectSyncSuccessMessage    string
	ProjectSyncErrorMessage      string
	ReferenceLinkingErrorMessage string
	SessionsCurrentRow           string // sessions page current <tr> (IP + moment date)
	SessionsOtherRows            string // other sessions <tr>s (may be empty)
	LibUsersJSON                 string // P6.5: ol-userSettings JSON (editorpages.BuildUserSettings)
	CanManageTemplateMenu        bool   // P6.13: ExposedSettings.canManageTemplatesMenu (per-user)
	NavAdmin                     string // P6.13: admin navbar fragment (site admins only)
}

func (p PageData) finalize(html string) string {
	out := strings.ReplaceAll(html, slotCSRF, p.CSRFToken)
	out = strings.ReplaceAll(out, slotNonce, p.Nonce)
	if p.Path != "" {
		out = strings.ReplaceAll(out, slotPath, p.Path)
	} else {
		out = strings.ReplaceAll(out, slotPath, "/")
	}
	out = strings.ReplaceAll(out, slotOLUsers, p.UserEmail)
	out = strings.ReplaceAll(out, slotOLUID, p.UserID)
	out = strings.ReplaceAll(out, slotP2User, p.UserEmail)
	out = strings.ReplaceAll(out, slotP2UserID, p.UserID)
	out = strings.ReplaceAll(out, slotResetErr, p.ResetErr)
	out = strings.ReplaceAll(out, slotEmailField, p.EmailField)
	out = strings.ReplaceAll(out, slotResetToken, p.ResetToken)
	out = strings.ReplaceAll(out, slotPostURL, p.PostURL)
	// P3.3 slots (value-only where set; bare-content boolean metas render
	// a SPACE + `content` when true, nothing when false):
	out = strings.ReplaceAll(out, slot33User, htmlAttrEsc(p.UserMetaJSON))
	if p.SamlBeta != "" {
		frag := `&quot;hasSamlBeta&quot;:&quot;` + htmlAttrEsc(p.SamlBeta) + `&quot;` + string(',')
		out = strings.ReplaceAll(out, slot33HBS, frag)
		out = strings.ReplaceAll(out, slot33SamlMeta, ` content="`+htmlAttrEsc(p.SamlBeta)+`"`)
	} else {
		out = strings.ReplaceAll(out, slot33HBS, "")
		out = strings.ReplaceAll(out, slot33SamlMeta, "")
	}
	out = strings.ReplaceAll(out, slot33HasPw, boolAttr(p.HasPassword))
	out = strings.ReplaceAll(out, slot33ShowAI, boolAttr(p.ShowAiFeatures))
	out = strings.ReplaceAll(out, slot33SsoMsg, metaContentAttr(p.SsoErrorMessage))
	out = strings.ReplaceAll(out, slot33SyncOk, metaContentAttr(p.ProjectSyncSuccessMessage))
	out = strings.ReplaceAll(out, slot33SyncErr, metaContentAttr(p.ProjectSyncErrorMessage))
	out = strings.ReplaceAll(out, slot33RefErr, metaContentAttr(p.ReferenceLinkingErrorMessage))
	out = strings.ReplaceAll(out, slot33CurrRow, p.SessionsCurrentRow)
	out = strings.ReplaceAll(out, slot33Rows, p.SessionsOtherRows)
	out = strings.ReplaceAll(out, slotLibUsers, htmlAttrEsc(p.LibUsersJSON))
	if p.CanManageTemplateMenu {
		out = strings.ReplaceAll(out, slotCanMgtTpl, "true")
	} else {
		out = strings.ReplaceAll(out, slotCanMgtTpl, "false")
	}
	out = strings.ReplaceAll(out, slotNavAdmin, p.NavAdmin)
	// P3.4 register page (anon skeleton; fills on a logged-in session):
	out = strings.ReplaceAll(out, slotRegUsers, htmlAttrEsc(p.UserEmail))
	if p.UserID == "" {
		out = strings.ReplaceAll(out, slotRegUID, "")
	} else {
		out = strings.ReplaceAll(out, slotRegUID, ` content="`+htmlAttrEsc(p.UserID)+`"`)
	}
	if p.UserEmail == "" {
		out = strings.ReplaceAll(out, slotRegSU, "")
	} else {
		out = strings.ReplaceAll(out, slotRegSU, `,&quot;sessionUser&quot;:{&quot;email&quot;:&quot;`+htmlAttrEsc(p.UserEmail)+`&quot;}`)
	}
	orig := originOf(p.Origin)
	if orig != "" {
		out = strings.ReplaceAll(out, capturedOrigin, orig)
	}
	return out
}

// boolAttr — pug `content=bool` renders the ATTRIBUTE PRESENT (bare, empty
// value) when true and ABSENT when false (pinned on ol-hasPassword et al.).
func boolAttr(v bool) string {
	if v {
		return " content"
	}
	return ""
}

// metaContentAttr — string metas: ` content="ESC"` when truthy, nothing
// when falsy (pinned: <meta name="ol-samlBeta"> vs content="CAP-SAMLBETA").
func metaContentAttr(v string) string {
	if v == "" {
		return ""
	}
	return ` content="` + htmlAttrEsc(v) + `"`
}

// htmlAttrEsc — pug attribute escaping: & < > " (pug uses the HTML escape
// set for attribute values).
func htmlAttrEsc(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return r.Replace(s)
}

// originOf normalizes "http://host" / "https://host" (strips paths).
func originOf(u string) string {
	scheme := "http"
	rest := u
	switch {
	case len(u) > 8 && u[:8] == "https://":
		scheme, rest = "https", u[8:]
	case len(u) > 7 && u[:7] == "http://":
		rest = u[7:]
	default:
		return u
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' {
			return scheme + "://" + rest[:i]
		}
	}
	return scheme + "://" + rest
}

// Page renders a shell page (HTML, no ETag — Node sets none on views).
const cspRestrictive = "base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'"

// cspReact — Node's React-layout CSP (pinned live on /login + /register).
func cspReact(nonce string) string {
	return "script-src 'nonce-" + nonce + "' 'unsafe-inline' 'strict-dynamic' https: 'report-sample'; object-src 'none'; base-uri 'none'"
}

func Page(w http.ResponseWriter, d PageData, skeleton string) {
	csp := d.CSP
	if csp == "" {
		csp = cspRestrictive
	}
	html := d.finalize(skeleton)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", csp)
	// HttpPermissionsPolicy — rendered views only (pinned P3.1: the 500 view
	// carries it, JSON routes do not). Set after the CSP so renderers can
	// override either cleanly.
	w.Header().Set("Permissions-Policy", core.PinnedPermissionsPolicy)
	// Node express etag on rendered views (pinned on /login + P3.3 pages):
	// weak W/\"<hexlen>-<sha1-27>\".
	w.Header().Set("ETag", core.EtagWeakBody(html))
	w.WriteHeader(200)
	_, _ = io.WriteString(w, html)
}

// StatusPage for 404/500-style views (Node: res.status(404).render(...)).
func StatusPage(w http.ResponseWriter, d PageData, status int, skeleton string) {
	csp := d.CSP
	if csp == "" {
		csp = cspRestrictive
	}
	html := d.finalize(skeleton)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("Permissions-Policy", core.PinnedPermissionsPolicy)
	w.Header().Set("ETag", core.EtagWeakBody(html))
	w.WriteHeader(status)
	_, _ = io.WriteString(w, html)
}

// Restricted403 — the CE access-denied render (403 + restricted view),
// e.g. ensureUserCanReadProject on token-member-only projects (P2).
func Restricted403(w http.ResponseWriter, d PageData) {
	StatusPage(w, d, 403, restrictedHTML)
}

// restrictedDefaultTitleHTML — the SAME restricted page rendered by Node's
// ErrorController.forbidden (Errors.ForbiddenError — e.g. the P6.14
// notifications project-prefs non-member 403): res.render('user/restricted')
// with NO title local → the layout default title (appName only), unlike the
// AuthorizationMiddleware.restricted ({ title: 'restricted' }) variant that
// Restricted403 above mirrors. Pinned live 2026-09-18 (A/B battery).
var restrictedDefaultTitleHTML = strings.NewReplacer(
	`<title translate="no">Restricted - OlliTeX, Online LaTeX Editor</title>`,
	`<title translate="no">OlliTeX, Online LaTeX Editor</title>`,
	`<meta name="twitter:title" content="Restricted">`,
	`<meta name="twitter:title" content="OlliTeX, Online LaTeX Editor">`,
	`<meta name="og:title" content="Restricted">`,
	`<meta name="og:title" content="OlliTeX, Online LaTeX Editor">`,
).Replace(restrictedHTML)

// Restricted403AppTitle — the ErrorController.forbidden family (403 +
// restricted view, layout-default title — P6.14 notifications).
func Restricted403AppTitle(w http.ResponseWriter, d PageData) {
	StatusPage(w, d, 403, restrictedDefaultTitleHTML)
}

// LoginPage / RegisterPage / LogoutConfirmation / Restricted / NotFound.
func LoginPage(w http.ResponseWriter, d PageData) { d.CSP = cspReact(d.Nonce); Page(w, d, loginHTML) }

// SettingsPage — GET /user/settings (React layout: nonce CSP, pinned P3.3).
func SettingsPage(w http.ResponseWriter, d PageData) {
	d.CSP = cspReact(d.Nonce)
	Page(w, d, settingsHTML)
}

// LibraryView — GET /library (trash=false) + /library/trashed (true):
// React shell (bib-editor entrypoint), pinned P6.5 (57-pin oracle
// /tmp/p65_node.json). The two bodies differ only in ol-libraryView +
// the navbar/alternate URL.
func LibraryView(w http.ResponseWriter, d PageData, trash bool) {
	d.CSP = cspReact(d.Nonce)
	if trash {
		Page(w, d, libraryTrashHTML)
		return
	}
	Page(w, d, libraryHTML)
}

// SessionsPage — GET /user/sessions (layout-website-redesign — the same
// nonce-script CSP per the live capture).
func SessionsPage(w http.ResponseWriter, d PageData) {
	d.CSP = cspReact(d.Nonce)
	Page(w, d, sessionsHTML)
}
func RegisterPage(w http.ResponseWriter, d PageData) {
	d.CSP = cspReact(d.Nonce)
	Page(w, d, registerHTML)
}
func LogoutPage(w http.ResponseWriter, d PageData)     { Page(w, d, logoutHTML) }
func RestrictedPage(w http.ResponseWriter, d PageData) { Page(w, d, restrictedHTML) }
func NotFoundPage(w http.ResponseWriter, d PageData)   { StatusPage(w, d, 404, notFoundHTML) }

// Error500Page — general/500 (pinned live P3.1: nonce-policy CSP like the
// React layouts, PP header, deterministic 681-byte body, ETag W/"2a9-..").
const error500HTML = `<!DOCTYPE html><html lang="en"><head><title>Something went wrong</title><link rel="icon" href="/favicon.ico"><link rel="stylesheet" href="/stylesheets/main-style-78c09375767178f0ab97.css"></head><body class="full-height"><main class="content content-alt full-height" id="main-content"><div class="container full-height"><div class="error-container full-height"><div class="error-details"><p class="error-status">Something went wrong, sorry.</p>If the problem persists, please contact us at
<a href="mailto:__ADMINEMAIL__" target="_blank">__ADMINEMAIL__</a>.<p class="error-actions"><a class="error-btn" href="/">Home</a></p></div></div></div></main></body></html>`

func Error500Page(w http.ResponseWriter, d PageData) {
	if d.AdminEmail == "" {
		// services/web settings: adminEmail = env OVERLEAF_ADMIN_EMAIL with the
		// CE default fallback (views/general/500.pug / settings.js).
		if v := os.Getenv("OVERLEAF_ADMIN_EMAIL"); v != "" {
			d.AdminEmail = v
		} else {
			d.AdminEmail = "placeholder@example.com"
		}
	}
	d.CSP = cspReact(d.Nonce)
	body := strings.ReplaceAll(error500HTML, "__ADMINEMAIL__", d.AdminEmail)
	csp := d.CSP
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("Permissions-Policy", core.PinnedPermissionsPolicy)
	w.Header().Set("ETag", core.EtagWeakBody(body))
	w.WriteHeader(500)
	_, _ = io.WriteString(w, body)
}

// P2 (website-redesign layout — React shell, same CSP family as login).
func PasswordResetPage(w http.ResponseWriter, d PageData) {
	d.CSP = cspReact(d.Nonce)
	Page(w, d, passwordResetHTML)
}
func SetPasswordPage(w http.ResponseWriter, d PageData) {
	d.CSP = cspReact(d.Nonce)
	Page(w, d, setPasswordHTML)
}
func TokenAccessPage(w http.ResponseWriter, d PageData) {
	d.CSP = cspReact(d.Nonce)
	Page(w, d, tokenAccessLegacyHTML)
}
func SharingUpdatesPage(w http.ResponseWriter, d PageData) {
	d.CSP = cspReact(d.Nonce)
	Page(w, d, sharingUpdatesHTML)
}
