// Package registrationpage ports the CE registration-page module
// (services/web/modules/registration-page) for the Node→Go web cutover.
//
// Routes (module RegistrationPageRouter.mjs):
//
//	GET  /register  → ensureRegistrationEnabled → registrationPage
//	POST /register  → ensureRegistrationEnabled → rateLimit(5/60) → registerNewUser
//
// Contract pins (live Node e2e, 2026-09-14, P3.4):
//
//	GET (enabled)          → 200 text/html (React shell)
//	GET (disabled)         → 302 → disabledRedirectUrl (or /login)
//	POST anon no-csrf      → core csrf 403 "Forbidden"
//	POST disabled          → 403 text/plain "Registration is disabled on this site"
//	POST logged-in         → 302 → "/"
//	POST name bad          → 400 {"message":"Too long name."}
//	POST invalid email     → 400 {"message":"Invalid email address."}
//	POST disallowed domain → 403 {"message":"Registration is not available for this email domain."}
//	POST email registered  → 409 {"message":{"key":"account_with_this_email_exists"}}
//	POST happy             → 200 {"message":"Registration successful. Please check your email to activate your account."}
//	                         (+ user, 7-day 'password' token, "Activate your OlliTeX Account" mail)
//	POST 6th in 60s        → 429 "Rate limit reached, please try again later"

package registrationpage

import (
	"encoding/json"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
	"regexp"
)

const (
	msgSuccess  = "Registration successful. Please check your email to activate your account."
	msgTooLong  = "Too long name."
	msgBadEmail = "Invalid email address."
	msgDomain   = "Registration is not available for this email domain."
	msgMailFail = "Failed to send the registration email. Please check the email address."
	msgDisabled = "Registration is disabled on this site"
	msg409      = `{"message":{"key":"account_with_this_email_exists"}}`
	oneWeekSec  = 7 * 24 * 60 * 60
)

var emailRe = regexp.MustCompile(`^([^<>()[\]\\.,;:\s@"]+(\.[^<>()[\]\\.,;:\s@"]+)*)@((\[[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\])|(([a-zA-Z\-0-9]+\.)+[a-zA-Z]{2,}))$`)

// Feature wires the registration routes (owns both /register routes).

// Feature wires the registration routes (owns both /register routes).
func Feature(a *core.App) core.Feature {
	mail := core.NewMail()
	tok := core.NewOneTimeTokens(a.Mongo)
	lim := core.NewRateLimiter(a.Redis, "postRegister", 5, 60)
	return core.Feature{
		Name: "registrationpage",
		Routes: []core.Route{
			{Method: "GET", Path: "/register", NoLogin: true, Handler: getPage(a)},
			{Method: "POST", Path: "/register", NoLogin: true, Handler: postHandler(a, mail, tok, lim)},
		},
	}
}

// ---- GET /register ----

// ---- GET /register ----

func getPage(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		sig := readSignup(a, cxt.Req.Context())
		if !sig.Enabled {
			target := sig.DisabledRedirectURL
			if target == "" {
				target = "/login"
			}
			res.Redirect(cxt.Req, 302, target)
			return
		}
		views.RegisterPage(res.W, pageData(cxt))
	}
}

func pageData(cxt *core.Cxt) views.PageData {
	d := views.PageData{Nonce: views.NewNonce(), Origin: cxt.SiteURL}
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
	}
	if email, uid := sessionUser(cxt); uid != "" {
		d.UserEmail = email
		d.UserID = uid
	}
	d.CSP = "script-src 'nonce-" + d.Nonce + "' 'unsafe-inline' 'strict-dynamic' https: 'report-sample'; object-src 'none'; base-uri 'none'"
	return d
}

func sessionUser(cxt *core.Cxt) (email, uid string) {
	if cxt.Sess == nil || !cxt.Sess.IsLoggedIn() {
		return "", ""
	}
	uid = cxt.Sess.UserIDHex()
	if raw, ok := cxt.Sess.GetRaw("passport"); ok {
		var p struct {
			User struct {
				Email string `json:"email"`
			} `json:"user"`
		}
		if json.Unmarshal(raw, &p) == nil {
			email = p.User.Email
		}
	}
	return email, uid
}

// ---- POST /register ----

// ---- POST /register ----

func postHandler(a *core.App, mail *core.Mail, tok *core.OneTimeTokens, lim *core.RateLimiter) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		ctx := cxt.Req.Context()
		sig := readSignup(a, ctx)
		if !sig.Enabled {
			res.PlainText(403, msgDisabled)
			return
		}
		// rate limit (Node: clientId = getUserId(req) || req.ip — a logged-in
		// session is bucketed by user id, anonymous by IP; both consume before
		// the controller's logged-in guard returns 302).
		clientID := core.ClientIP(cxt.Req)
		if cxt.Sess != nil && cxt.Sess.IsLoggedIn() {
			clientID = cxt.Sess.UserIDHex()
		}
		if !lim.Consume(clientID) {
			core.Send429(res, "Rate limit reached, please try again later")
			return
		}
		if cxt.Sess != nil && cxt.Sess.IsLoggedIn() {
			res.Redirect(cxt.Req, 302, "/")
			return
		}
		raw := decodeToMap(cxt)
		fn, okFn := raw["first_name"].(string)
		ln, okLn := raw["last_name"].(string)
		if !okFn || !okLn || len(fn) > 100 || len(ln) > 100 {
			res.JSON(400, []byte(`{"message":"Too long name."}`))
			return
		}
		emailRaw, _ := raw["email"].(string)
		parsed := parseEmail(emailRaw)
		if parsed == "" {
			res.JSON(400, []byte(`{"message":"Invalid email address."}`))
			return
		}
		if !domainAllowed(sig.AllowedEmailDomains, parsed) {
			res.JSON(403, []byte(`{"message":"`+msgDomain+`"}`))
			return
		}
		switch doRegister(a, mail, tok, cxt, parsed, fn, ln) {
		case "success":
			res.JSON(200, []byte(`{"message":"`+msgSuccess+`"}`))
		case "taken":
			res.JSON(409, []byte(msg409))
		case "mailfail":
			res.JSON(422, []byte(`{"message":"`+msgMailFail+`"}`))
		default:
			res.PlainText(500, "Internal Server Error")
		}
	}
}

// doRegister — creates the user (or reuses a holding account), hashes the
// random password, mints the 7-day token and sends the activation mail.
