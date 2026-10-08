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
	"regexp"
	"strconv"
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
	slot33Waka     = "\x01WAKA33\x02"  // wakaTimeEnabled true/false (settings ExposedSettings)
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
	// U10.3r slots (one_time_login.go): GET /read-only/one-time-login —
	// anonymous skeleton; these fill from the session user when a
	// logged-in visitor lands on the page (Node oracle, 2026-09-23).
	slotOTLUser = "\x01OTLUSER\x02" // ol-usersEmail content ("" anon)
	slotOTLUID  = "\x01OTLUID\x02"  // ol-user_id: `` or ` content="HEX"`
	slotOTLNav  = "\x01OTLNAV\x02"  // navbar fragment (anon vs logged-in)
	// P6.20 launchpad slots (pages_data_p620.go):
	slotLPUID   = "\x01LPUID\x02" // ol-user_id: `` or ` content="HEX"` (REGUID semantics)
	slotLPAdmin = "\x01LPADM\x02" // ol-adminUserExists bare-content boolean (` content`/``)
	// origin captured from the e2e fixtures (rewritten per request).
	capturedOrigin = "http://127.0.0.1:7420"
)

// Asset resolution (hash-drift-proof static bundle references, 2026-09-26):
// in-image webpack builds produce content-hashed asset names that DIFFER
// between builds (e.g. the runtime chunk hash changes on every rebuild),
// so baked templates never pin hashed URLs for manifest-listed assets
// (runtime.js, pages/…/….js, …/….css, marketing.js, tracking.js,
// bootstrap.js). They carry ASSET slots instead:
//
//	interpreted strings: \x01ASSET:<key>\x02  (real 0x01/0x02 bytes)
//	raw strings:         __ASSET:<key>__
//
// both resolved at serve time from core.AssetManifest (loaded in core.New
// from <PublicDir>/manifest.json; tests set it directly).
//
// SHARED slots (the other webpack half of the static set): page entries
// REQUIRE their generation's shared chunks preloaded (the e.O footer waits
// for them — the runtime does NOT self-load static chunks), which is why
// they were in the original page HTML. They are not manifest-listed under
// stable keys (chunk-id files), so the slot carries the ENTRY key and the
// serve layer composes the tags from the in-image entry file + directory
// listing (core.SharedTags — always the image's own consistent generation):
//
//	\x01SHARED_JS:<entryKey>\x02  → <script> tags (nonce placeholder inside)
//	\x01SHARED_CSS:<entryKey>\x02 → <link> tags
func resolveAssetSlots(s string) string {
	resolve := func(markerBeg, markerEnd string) string {
		out := s
		from := 0
		for {
			i := strings.Index(out[from:], markerBeg)
			if i < 0 {
				break
			}
			i += from
			end := strings.Index(out[i:], markerEnd)
			if end < 0 {
				break
			}
			end += i
			key := out[i+len(markerBeg) : end]
			if v := core.AssetFor(key); v != "" {
				out = out[:i] + v + out[end+len(markerEnd):]
				from = i + len(v)
			} else {
				from = end + len(markerEnd) // keep the token visible; skip past it
			}
		}
		return out
	}
	resolveShared := func(markerBeg, markerEnd string) string {
		out := s
		for {
			i := strings.Index(out, markerBeg)
			if i < 0 {
				break
			}
			end := strings.Index(out[i:], markerEnd)
			if end < 0 {
				break
			}
			end += i
			key := out[i+len(markerBeg) : end] // JS:<entryKey> | CSS:<entryKey>
			var v string
			if strings.HasPrefix(key, "JS:") {
				v = core.SharedTags(strings.TrimPrefix(key, "JS:"), "js")
			} else if strings.HasPrefix(key, "CSS:") {
				v = core.SharedTags(strings.TrimPrefix(key, "CSS:"), "css")
			}
			out = out[:i] + v + out[end+len(markerEnd):]
		}
		return out
	}
	s = resolve("\x01ASSET:", "\x02")
	s = resolve("__ASSET:", "__")
	s = resolveShared("\x01SHARED:", "\x02")
	s = resolveShared("__SHARED:", "__")
	return s
}

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
	// U9: navbar showSignUpLink — Node hasFeature('registration-page') =
	// boolFromEnv(OVERLEAF_ENABLE_REGISTRATION_PAGE) ?? !(sso-saml||sso-ldap||
	// sso-oidc site_settings enabled) — stack-wide, computed per request.
	// Rendered as a JSON boolean true/false in the ol-navbar metas.
	ShowSignUpLink bool
	// NavSiteAdmin — layout-react navbar admin flags (canDisplayAdminMenu
	// + canDisplayProjectUrlLookup collapse to it in this stack; see
	// core.NavSiteAdmin). Replaces the baked `false` literals in the
	// page-data navbar renders.
	NavSiteAdmin bool
	// fedgap-2 (SSO login slot): the /login ol-auth-config meta content
	// (HTML-escaped JSON `{&quot;sso&quot;:[...],&quot;ldapEnabled&quot;:false}`),
	// computed per request by the sso feature's hook. Empty = the baked
	// anonymous default (byte-identical: sso:[] + ldapEnabled:false).
	AuthConfig string
	// P2 dynamic slots (empty strings render the anonymous/absent shape):
	ResetErr   string // passwordReset meta: "" | "password_reset_token_expired"
	EmailField string // setPassword form email input
	ResetToken string // setPassword hidden token input
	PostURL    string // token page postUrl meta, e.g. /<token>/grant
	// P3.3 dynamic slots (userpages feature):
	UserMetaJSON                 string // settings page ol-user meta JSON (Node serializeUser order)
	SamlBeta                     string // session samlBeta ("" → ExposedSettings key + meta absent)
	WakaEnabled                  bool   // wakaTimeEnabled in settings ExposedSettings (Wakepi/V)
	HasPassword                  bool   // ol-hasPassword bare-content boolean meta
	ShowAiFeatures               bool   // ol-showAiFeatures bare-content boolean meta
	SsoErrorMessage              string // settings page pop-flag metas
	ProjectSyncSuccessMessage    string
	ProjectSyncErrorMessage      string
	ReferenceLinkingErrorMessage string
	SessionsCurrentRow           string   // sessions page current <tr> (IP + moment date)
	SessionsOtherRows            string   // other sessions <tr>s (may be empty)
	LibUsersJSON                 string   // P6.5: ol-userSettings JSON (editorpages.BuildUserSettings)
	CanManageTemplateMenu        bool     // P6.13: ExposedSettings.canManageTemplatesMenu (per-user)
	NavAdmin                     string   // P6.13: admin navbar fragment (site admins only)
	LaunchpadAdminExists         bool     // P6.20: ol-adminUserExists bare-content boolean
	I18n                         I18nPage // i18n wave A: locale pass (zero = English bytes)
}

func (p PageData) finalize(html string) string {
	out := resolveAssetSlots(html)
	// Audit H2 e2e-email scrub — moved HERE (from the terminal
	// sanitizeCaptureArtifacts pass) so it strips the BAKED capture
	// artifacts before the slot pass inserts the live session email.
	// The terminal blanket was erasing the legitimate session email on
	// every render — e.g. the restricted-403 account pill for a user
	// whose address is one of the captured artifacts (Node parity breaks:
	// Node renders the session email into these same slots).
	for _, em := range e2eEmails {
		if strings.Contains(out, em) {
			out = strings.ReplaceAll(out, em, "")
		}
	}
	out = strings.ReplaceAll(out, slotCSRF, p.CSRFToken)
	out = strings.ReplaceAll(out, slotNonce, p.Nonce)
	// tag-style nonce tokens (`__NONCE__`, editor/admin family) resolve
	// here too — asset-surgery rewrites emit that style in Page-family
	// shells as well; both must hit the per-request nonce or every
	// nonce-script tag violates the CSP.
	out = strings.ReplaceAll(out, "__NONCE__", p.Nonce)
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
	// 2026-10-10 (Wakepi/V): the settings-page WakatimeCard gates on
	// wakaTimeEnabled in ol-ExposedSettings — always render the key
	// (true/false) so the card shows; the Go gate stays authoritative.
	if p.WakaEnabled {
		out = strings.ReplaceAll(out, slot33Waka, "true")
	} else {
		out = strings.ReplaceAll(out, slot33Waka, "false")
	}
	out = strings.ReplaceAll(out, slotNavAdmin, p.NavAdmin)
	// U10.3r one_time_login slots: OTLNAV injects the auth-branching navbar
	// fragment FIRST (the logged-in fragment carries the form's csrf slot —
	// Node renders the SESSION token there, not the anonymous one), then
	// OTLUSER/OTLUID fill email/uid everywhere (skeleton + injected nav).
	if p.UserID == "" {
		out = strings.ReplaceAll(out, slotOTLUID, "")
		out = strings.ReplaceAll(out, slotOTLNav, otlNavAnon)
	} else {
		out = strings.ReplaceAll(out, slotOTLUID, ` content="`+htmlAttrEsc(p.UserID)+`"`)
		out = strings.ReplaceAll(out, slotOTLNav, otlNavIn)
		// the injected fragment carries a csrf slot (Node: value=csrfToken —
		// the SAME token as ol-csrfToken, session-bound when logged in):
		// the initial pass ran before injection, so resolve it now.
		out = strings.ReplaceAll(out, slotCSRF, p.CSRFToken)
	}
	out = strings.ReplaceAll(out, slotOTLUser, p.UserEmail)
	// P6.20 launchpad slots:
	if p.UserID == "" {
		out = strings.ReplaceAll(out, slotLPUID, "")
	} else {
		out = strings.ReplaceAll(out, slotLPUID, ` content="`+htmlAttrEsc(p.UserID)+`"`)
	}
	out = strings.ReplaceAll(out, slotLPAdmin, boolAttr(p.LaunchpadAdminExists))
	// P3.4 register page (anon skeleton; fills on a logged-in session):
	out = strings.ReplaceAll(out, slotRegUsers, htmlAttrEsc(p.UserEmail))
	// U9: /login anon skeleton (captured signed-out) — Node fills these from
	// the SESSION user when a logged-in visitor lands on /login (live-pinned
	// 2026-09-22); anonymous keeps the anonymous shape: content="" and the
	// valueless ol-user_id meta.
	out = strings.ReplaceAll(out, "\x01LOGINUSER\x02", htmlAttrEsc(p.UserEmail))
	if p.UserID == "" {
		out = strings.ReplaceAll(out, "\x01LOGINUID\x02", "")
	} else {
		out = strings.ReplaceAll(out, "\x01LOGINUID\x02", ` content="`+htmlAttrEsc(p.UserID)+`"`)
	}
	// fedgap-2 (SSO login slot): the /login ol-auth-config meta — the sso
	// feature's hook builds the per-request JSON (enabled providers);
	// empty renders the anonymous default (byte-identical to the baked
	// shape: sso:[] + ldapEnabled:false).
	authCfg := p.AuthConfig
	if authCfg == "" {
		authCfg = `{&quot;sso&quot;:[],&quot;ldapEnabled&quot;:false}`
	}
	out = strings.ReplaceAll(out, "\x01SSOCONFIG\x02", authCfg)
	// U9: navbar showSignUpLink (JSON boolean in the ol-navbar metas).
	if p.ShowSignUpLink {
		out = strings.ReplaceAll(out, "\x01SUPLINK\x02", "true")
	} else {
		out = strings.ReplaceAll(out, "\x01SUPLINK\x02", "false")
	}
	// U9: navbar admin flags (layout-react.pug — baked false in the page
	// data; Node flips both for a site-admin session when
	// ADMIN_PRIVILEGE_AVAILABLE=true). The dynamic editor/hub navbars are
	// separate (navbarJSON / hubNavbar) and never carry this literal.
	if p.NavSiteAdmin {
		out = strings.ReplaceAll(out, "canDisplayAdminMenu\u0026quot;:false", "canDisplayAdminMenu\u0026quot;:true")
		out = strings.ReplaceAll(out, "canDisplayProjectUrlLookup\u0026quot;:false", "canDisplayProjectUrlLookup\u0026quot;:true")
	}
	// U9: login navbar sessionUser (layout-react.pug:
	// sessionUser ? {email} : undefined — key ABSENT when anonymous).
	if p.UserEmail != "" {
		out = strings.ReplaceAll(out, "\x01LOGINITEMS\x02", `,&quot;sessionUser&quot;:{&quot;email&quot;:&quot;`+htmlAttrEsc(p.UserEmail)+`&quot;},`)
	} else {
		out = strings.ReplaceAll(out, "\x01LOGINITEMS\x02", ",")
	}
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
	out = sanitizeCaptureArtifacts(out, p.CSRFToken, orig)
	return out
}

// sanitizeCaptureArtifacts — audit H2 (problems_30092026, 2026-09-30):
// the view skeletons were captured from e2e runs with a LIVE e2e session,
// so stale literals leaked through finalize regardless of slot coverage.
// This terminal pass (single choke point for every Page-family render) removes them:
//
//  1. literal `_csrf` form values (dead e2e tokens — would 403 a real
//     user's POST and leak capture tokens) → the CURRENT request's csrf token
//  2. any `@e2e.test` emails (e2e sessionUser remnants) → "" (guest shape;
//     the hydrated UI re-renders from the live session)
//  3. baked e2e project ids (info leak in alternate links / meta / JSON) → ""
//  4. captured origin when the request Origin is empty → root-relative
//     (otherwise a bare host render keeps the 127.0.0.1:7420 leak)
//
// Compiled once (package scope; RE2 — no lazy quantifiers).
var (
	// `name="_csrf"` (raw HTML attr or JSON-escaped `\"` attr) … `value="TOKEN"`
	csrfLiteralRe = regexp.MustCompile(`((?:name=\\?"_csrf\\?"[^>]{0,80}?value=)\\?")([A-Za-z0-9_-]{6,60})(\\?")`)
	// exact capture-artifact emails (surgical: a real user may own any other
	// @e2e.test address; these three are the e2e-run session remnants baked
	// into the captured skeletons)
	// csrfMetaRe (Part B 1.1): <meta name="ol-csrfToken" content="TOKEN">.
	csrfMetaRe = regexp.MustCompile(`(<meta name=\\?"ol-csrfToken\\?" content=\\?")([A-Za-z0-9_-]{6,60})(\\?")`)
	// authE2e patterns (Part B 1.1): ol-auth-config entry objects whose
	// domain ends in "e2e.test" (the capture artifact). The shipped baked
	// constants carry HTML-escaped JSON (&quot;), so the quote atom covers
	// both raw and escaped shapes; stripE2EDomainEntries does the comma
	// bookkeeping (not-first / first / sole entry).
	authE2eEntryRe = regexp.MustCompile(e2eDomainEntryPattern())
	e2eEmails      = []string{"e2e-user@e2e.test", "admin@e2e.test", "someone@e2e.test"}
	e2eProjectIDs  = []string{"6aa4b8c973ef0e5094f4cc02", "1234567890abcdefgh"}
)

func sanitizeCaptureArtifacts(in, csrfToken, origin string) string {
	if csrfLiteralRe.MatchString(in) {
		// ${n} braces are mandatory: $1 + a token char run is parsed as ONE
		// (named) group reference and silently expands to "".
		in = csrfLiteralRe.ReplaceAllString(in, "${1}"+csrfToken+"${3}")
	}
	// NB: the e2eEmails scrub moved to finalize's front (pre-slot) so it
	// no longer erases the legitimate session email re-inserted by slots.
	for _, id := range e2eProjectIDs {
		if strings.Contains(in, id) {
			in = strings.ReplaceAll(in, id, "")
		}
	}
	// captured origin → the current request origin (empty → root-relative;
	// never serve a 127.0.0.1:7420 literal to a real user).
	if strings.Contains(in, capturedOrigin) {
		in = strings.ReplaceAll(in, capturedOrigin, origin)
	}
	// Part B 1.1 (sessions page): the <meta name="ol-csrfToken"> can be a
	// BAKED literal (not the runtime slot) — React pages read their CSRF
	// token from this meta (getMeta), so a stale literal = every XHR on
	// that page 403s. Rewrite it to the current session token exactly like
	// the _csrf inputs above. (Runtime slots contain control-char markers
	// and never match the token-shape class.)
	if csrfMetaRe.MatchString(in) {
		in = csrfMetaRe.ReplaceAllString(in, "${1}"+csrfToken+"${3}")
	}
	// Part B 1.1 (register page): the baked ol-auth-config lists the
	// capture's registration-domain restriction ("e2e.test") — real users
	// must not see e2e.test as a live domain restriction. Drop the e2e.test
	// entries (leaves a valid empty domains array; a pathological real
	// domain ending in ".e2e.test" is the only collateral).
	in = stripE2EDomainEntries(in)
	return in
}

// e2eDomainEntryPattern — one ol-auth-config entry object (raw JSON or
// HTML-escaped &quot; quotes).
func e2eDomainEntryPattern() string {
	q := `(?:"|&quot;)`
	// quotes wrap KEYS and STRING VALUES only; the booleans are bare.
	return `\{` + q + `domain` + q + `:` + q + `[0-9A-Za-z*._-]*e2e\.test` + q +
		`,` + q + `exact` + q + `:` + `(?:true|false)` +
		`,` + q + `subdomains` + q + `:` + `(?:true|false)` + `\}`
}

// stripE2EDomainEntries — remove every e2e.test auth-config entry with
// correct array comma bookkeeping, pass order: entry with a preceding
// comma (not-first), then a following comma (first), then bare (sole).
func stripE2EDomainEntries(in string) string {
	ep := e2eDomainEntryPattern()
	in = regexp.MustCompile(`\s*,\s*`+ep).ReplaceAllString(in, "")
	in = regexp.MustCompile(ep+`\s*,?\s*`).ReplaceAllString(in, "")
	in = regexp.MustCompile(ep).ReplaceAllString(in, "")
	return in
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

// ReactorCSP — exported accessor for the React-layout CSP (the admin
// sub-pages, e.g. the SAML SP-metadata page, embed a nonce'd inline script).
func ReactorCSP(nonce string) string { return cspReact(nonce) }

func cspReact(nonce string) string {
	return "script-src 'nonce-" + nonce + "' 'unsafe-inline' 'strict-dynamic' https: 'report-sample'; object-src 'none'; base-uri 'none'"
}

func Page(w http.ResponseWriter, d PageData, skeleton string) {
	csp := d.CSP
	if csp == "" {
		// Node: every rendered page goes through the React layout — the
		// nonce CSP (pinned U10.2 on 404/restricted/logout; explicit d.CSP
		// still wins for callers that pin a different policy).
		csp = cspReact(d.Nonce)
	}
	html := d.finalize(skeleton)
	html = translateI18n(html, d.I18n)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", csp)
	// Express res.render always sends the full body length (Node sends
	// Content-Length on rendered views — pinned U10.1: 15 KB 404/403 pages).
	w.Header().Set("Content-Length", strconv.Itoa(len(html)))
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
		// U10.2: Node's 404/restricted status pages render through the React
		// layout → nonce CSP (pinned live: 404 + restricted 403 both carry
		// `script-src 'nonce-…' 'unsafe-inline' 'strict-dynamic' …`).
		csp = cspReact(d.Nonce)
	}
	html := d.finalize(skeleton)
	html = translateI18n(html, d.I18n)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("Permissions-Policy", core.PinnedPermissionsPolicy)
	w.Header().Set("ETag", core.EtagWeakBody(html))
	w.Header().Set("Content-Length", strconv.Itoa(len(html)))
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

// LaunchpadAdminPage / LaunchpadFreshPage — the P6.20 launchpad bakes
// (pages_data_p620.go). CSP = cspReact (pinned live 2026-09-21 on the
// 200 admin page: `script-src 'nonce-…' 'unsafe-inline' 'strict-dynamic'
// https: 'report-sample'; object-src 'none'; base-uri 'none'`). The
// admin page is only rendered for a site-admin session (Node
// hasAdminAccess), so ExposedSettings.canManageTemplatesMenu = true and
// ol-adminUserExists = true there; the fresh (anonymous, no-admin) page
// renders the opposite per its capture.
func LaunchpadAdminPage(w http.ResponseWriter, d PageData) {
	d.CSP = cspReact(d.Nonce)
	d.CanManageTemplateMenu = true
	d.LaunchpadAdminExists = true
	Page(w, d, launchpadAdminHTML)
}
func LaunchpadFreshPage(w http.ResponseWriter, d PageData) {
	d.CSP = cspReact(d.Nonce)
	d.CanManageTemplateMenu = false
	d.LaunchpadAdminExists = false
	Page(w, d, launchpadFreshHTML)
}

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
const error500HTML = `<!DOCTYPE html><html lang="en"><head><title>Something went wrong</title><link rel="icon" href="/favicon.ico"><link rel="stylesheet" href="\x01ASSET:main-style.css\x02" id="main-stylesheet"></head><body class="full-height"><main class="content content-alt full-height" id="main-content"><div class="container full-height"><div class="error-container full-height"><div class="error-details"><p class="error-status">Something went wrong, sorry.</p>If the problem persists, please contact us at
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
	body = resolveAssetSlots(body)
	csp := d.CSP
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("Permissions-Policy", core.PinnedPermissionsPolicy)
	w.Header().Set("ETag", core.EtagWeakBody(body))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
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
