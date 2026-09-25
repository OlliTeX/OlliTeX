// Package consent — GDPR cookie consent core (remember.md P7-post item 2;
// owner spec 2026-09-19: "Go HTTP handlers and middleware to handle
// setting, parsing, and validating secure, … consent cookies
// (SameSite=Lax/Strict, Secure)" + "server-side logic in Go to
// conditionally render/inject tracking scripts … based on the user's
// cookie consent state").
//
// Contract
//
//	POST /cookie-consent  (NoLogin; CSRF-protected like every POST)
//	  body: {"consent":"all"} | {"consent":"essential"}
//	  → 200 {"ok":true,"consent":"all"|"essential"}
//	      + Set-Cookie: oa=1|0; Path=/; Max-Age=31536000; Secure; SameSite=Lax
//	  → 400 {"ok":false,"error":"invalid_consent"}  (anything else)
//
//	GET /cookie-consent   (NoLogin)
//	  → 200 {"consent":"all"}              (cookie oa=1)
//	  → 200 {"consent":"essential"}        (cookie oa=0)
//	  → 200 {"consent":null}               (absent or unparseable value)
//
// Cookie contract (compat — do not rename)
//
// The consent cookie is the ONE the first-party trackers gate on:
// frontend/js/infrastructure/tracking-loader.ts reads `oa=1` before loading
// any tracker, and frontend/js/features/cookie-banner/utils.ts
// (setConsent / hasMadeCookieChoice) writes/reads `oa=1|0`. `oa` = the
// Upstream "Overleaf Analytics" flag; `1` = analytics+marketing allowed,
// `0` = essential only.
//
// Deliberately NOT HttpOnly: the consent gate itself is first-party JS that
// reads the cookie — HttpOnly would break the very control it protects, and
// reading a self-set preference cookie gains an attacker nothing. The
// hardening lives in the SET path instead: a CSRF-protected
// server-validated endpoint (value allowlisted, canonical attributes),
// `Secure` always sent (effective on HTTPS; browsers drop it over plain
// http, where the frontend's local write keeps the UX working),
// `SameSite=Lax`, `Path=/`, one-year lifetime (parity with the legacy JS
// writer).
//
// # Server-side tracking-injection seam
//
// ConsentAllowsAnalytics(r) is the predicate view-rendering code must
// consult before including any external tracker block (the current stack
// ships zero external telemetry — owner audit 2026-09 pinned the GA/loader
// removal — so today it documents and freezes the rule; if a gaToken /
// mixpanel token is ever configured, the renderer omits the tracker block
// unless this returns true).
package consent

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"ollitex/go/services/web/core"
)

// Cookie/contract constants (compat — see package doc).
const (
	// CookieName — the consent cookie (compat contract; do not rename).
	CookieName = "oa"
	// ValueAll — analytics + marketing allowed.
	ValueAll = "1"
	// ValueEssential — essential only.
	ValueEssential = "0"
	// CookieMaxAge — one year, parity with the legacy JS writer
	// (60 * 60 * 24 * 365).
	CookieMaxAge = 365 * 24 * time.Hour
)

// wire bodies (the stable contract — explicit key order, no implicit
// Go map sorting):
//
//	200  {"ok":true,"consent":"all"|"essential"}
//	400  {"ok":false,"error":"invalid_consent"}
const (
	bodyOKAll = `{"ok":true,"consent":"all"}`
	bodyOKEss = `{"ok":true,"consent":"essential"}`
	bodyBad   = `{"ok":false,"error":"invalid_consent"}`
)

// consentValues — the allowlist (JSON "all"|"essential" → cookie value).
var consentValues = map[string]string{
	"all":       ValueAll,
	"essential": ValueEssential,
}

// Feature registers the consent endpoints.
//
// NoLogin: consent is per-browser (the banner appears on anonymous
// marketing pages); the endpoints must work without an account. The
// anonymous session still carries the CSRF secret (core-pinned), so the
// POST stays CSRF-protected exactly like every other POST.
func Feature(a *core.App) core.Feature {
	_ = a
	return core.Feature{
		Name: "consent",
		Routes: []core.Route{
			{Method: "GET", Path: "/cookie-consent", NoLogin: true, Handler: hGetConsent},
			{Method: "POST", Path: "/cookie-consent", NoLogin: true, Handler: hSetConsent},
		},
	}
}

// ParseConsentValue — validates a cookie value: only "1" → "all",
// "0" → "essential"; anything else → "" (absent/invalid). Exported so
// the renderers and tests share one parser (the "parsing and validating"
// half of the owner spec).
func ParseConsentValue(v string) string {
	switch v {
	case ValueAll:
		return "all"
	case ValueEssential:
		return "essential"
	}
	return ""
}

// ConsentAllowsAnalytics — the server-side tracking-injection predicate:
// true IFF the request carries a valid analytics consent (oa=1).
func ConsentAllowsAnalytics(r *http.Request) bool {
	c, err := r.Cookie(CookieName)
	return err == nil && c.Value == ValueAll
}

// consentCookie — the canonical Set-Cookie for a consent choice.
func consentCookie(v string) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    v,
		Path:     "/",
		MaxAge:   int(CookieMaxAge.Seconds()),
		Secure:   true,
		HttpOnly: false, // contract: first-party JS reads it (package doc)
		SameSite: http.SameSiteLaxMode,
	}
}

// hSetConsent — POST /cookie-consent.
//
// The core body-parser (express.json parity, core/app.go) already rejected
// non-object JSON roots with 400 before this handler runs; the strict
// decode here guards the remaining shapes (missing key, unknown value,
// wrong type) with the stable 400 contract.
func hSetConsent(cxt *core.Cxt, res *core.Res) {
	raw, err := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	if err != nil {
		res.JSON(400, []byte(bodyBad))
		return
	}
	var in struct {
		Consent string `json:"consent"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		res.JSON(400, []byte(bodyBad))
		return
	}
	cv, ok := consentValues[in.Consent]
	if !ok {
		res.JSON(400, []byte(bodyBad))
		return
	}
	http.SetCookie(res.W, consentCookie(cv))
	body := bodyOKEss
	if cv == ValueAll {
		body = bodyOKAll
	}
	res.JSON(200, []byte(body))
}

// hGetConsent — GET /cookie-consent (parse + report the current choice).
func hGetConsent(cxt *core.Cxt, res *core.Res) {
	_ = cxt
	val := "null"
	if c, err := cxt.Req.Cookie(CookieName); err == nil {
		if s := ParseConsentValue(c.Value); s != "" {
			val = `"` + s + `"`
		}
	}
	res.JSON(200, []byte(`{"consent":`+val+`}`))
}
