// Package passwordreset ports the CE password-reset flow (P2):
//
//	GET  /user/password/reset          (page; ?error=token_expired variant)
//	POST /user/password/reset          (rate limited; email the reset link)
//	GET  /user/password/set            (token or session; page)
//	POST /user/password/set            (validate + set + kill sessions)
//	POST /user/reconfirm               (same handler as POST reset, CE)
//
// Node contract pins (live + source, 2026-09-14):
//
//   - rate limiter 'password_reset_rate_limit' points:6 duration:60s,
//     key `rate-limit:password_reset_rate_limit:<ip>` (ipOnly), shared by
//     all five routes (router.mjs:43 family); 429 = plain body
//     'Rate limit reached, please try again later' (no content-type).
//   - 400 invalid email: {"message":"Must be an email address."}
//   - 404 unknown email: {"message":"That email address is not registered,
//     sorry."}
//   - 404 secondary email: {"message":"That email is registered as a
//     secondary email. Please enter the primary email for your account."}
//   - 200 sent: {"message":"You have been sent an email to complete your
//     password reset."}
//   - POST set: {"message":{"key":"invalid-password"}} | validation
//     {"message":{"type":"error","key":"password-…","text":"…"}} |
//     {"message":{"key":"token-expired"}} (404) |
//     {"message":{"key":"password-must-be-different"}} | 200 'OK'
//     (res.sendStatus(200): text/html 'OK' body).
//   - tokens collection: {use:'password', token:64hex,
//     data:{user_id,email}, createdAt, expiresAt(+1h), peekCount?, usedAt?}
//   - user update: users.updateOne({_id}, {$set:{hashedPassword},
//     $unset:{password:true}}); bcrypt rounds 12 (BCRYPT_ROUNDS env).
//   - removeSessionsFromRedis: SCAN keyspace, drop every session doc whose
//     passport.user (or user) _id matches; THEN the 200 goes out.

package passwordreset

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var rw = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@([A-Za-z0-9-]+\.)+[A-Za-z]{2,}$`)

func Feature(a *core.App) core.Feature {
	lim := core.NewRateLimiter(a.Redis, "password_reset_rate_limit", 6, 60)
	mail := core.NewMail()
	tok := core.NewOneTimeTokens(a.Mongo)

	limit := func(fn func(cxt *core.Cxt, res *core.Res)) func(*core.Cxt, *core.Res) {
		return func(cxt *core.Cxt, res *core.Res) {
			if lim != nil && !lim.Consume(core.ClientIP(cxt.Req)) {
				core.Send429(res, "Rate limit reached, please try again later")
				return
			}
			fn(cxt, res)
		}
	}

	jmsg := func(res *core.Res, code int, msg string) {
		res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
		res.W.WriteHeader(code)
		_, _ = fmt.Fprintf(res.W, `{"message":%q}`, msg)
	}

	pageData := func(cxt *core.Cxt) views.PageData {
		d := views.PageData{Nonce: views.NewNonce()}
		origin := cxt.SiteURL
		if origin == "" {
			origin = "http://" + cxt.Req.Host
		}
		d.Origin = origin
		if cxt.Sess != nil {
			d.CSRFToken = cxt.Sess.CsrfToken()
			d.UserEmail, d.UserID = core.PageUserSlots(cxt.Sess)
		}
		return d
	}

	requestReset := func(cxt *core.Cxt, res *core.Res) {
		var in struct {
			Email string `json:"email"`
		}
		body, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		if err := json.Unmarshal(body, &in); err != nil || in.Email == "" {
			jmsg(res, 400, "Must be an email address.")
			return
		}
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if !rw.MatchString(email) {
			jmsg(res, 400, "Must be an email address.")
			return
		}
		status, err := generateAndEmailResetToken(cxt, a, mail, tok, email)
		if err != nil {
			res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
			res.W.WriteHeader(500)
			return
		}
		switch status {
		case "primary":
			jmsg(res, 200, "You have been sent an email to complete your password reset.")
		case "secondary":
			jmsg(res, 404, "That email is registered as a secondary email. Please enter the primary email for your account.")
		default:
			jmsg(res, 404, "That email address is not registered, sorry.")
		}
	}

	return core.Feature{
		Name: "passwordreset",
		Routes: []core.Route{
			{Method: "GET", Path: "/user/password/reset", NoLogin: true, Handler: func(cxt *core.Cxt, res *core.Res) {
				d := pageData(cxt)
				if cxt.Req.URL.Query().Get("error") == "token_expired" {
					d.ResetErr = "password_reset_token_expired"
				}
				views.PasswordResetPage(res.W, d)
			}},
			{Method: "POST", Path: "/user/password/reset", NoLogin: true, Handler: limit(requestReset)},
			{Method: "GET", Path: "/user/password/set", NoLogin: true, Handler: func(cxt *core.Cxt, res *core.Res) {
				q := cxt.Req.URL.Query()
				emailRaw := q.Get("email")
				token := q.Get("passwordResetToken")
				email := parseEmail(emailRaw)
				if stringValid(emailRaw) && token != "" {
					ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
					defer cancel()
					_, remaining, perr := tok.Peek(ctx, "password", token)
					if perr != nil || remaining <= 0 {
						res.Redirect(cxt.Req, 302, "/user/password/reset?error=token_expired")
						return
					}
					if cxt.Sess != nil {
						cxt.Sess.Set("resetToken", token)
					}
					loc := "/user/password/set"
					if email != "" {
						loc += "?email=" + url.QueryEscape(email)
					}
					res.Redirect(cxt.Req, 302, loc)
					return
				}
				if stringValid(emailRaw) && token == "" {
					var sessionToken string
					if cxt.Sess != nil {
						if raw, ok := cxt.Sess.GetRaw("resetToken"); ok {
							_ = json.Unmarshal(raw, &sessionToken)
						}
					}
					if sessionToken == "" {
						res.Redirect(cxt.Req, 302, "/user/password/reset")
						return
					}
					d := pageData(cxt)
					d.EmailField = email
					d.ResetToken = sessionToken
					if cxt.Sess != nil {
						cxt.Sess.Del("resetToken")
					}
					views.SetPasswordPage(res.W, d)
					return
				}
				// email missing → Node parseReq 400 (battery-pinned fallback
				// shape; the CE validation handler replies with the zod
				// issue list — adjusted if the battery says otherwise).
				res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
				res.W.WriteHeader(400)
				_, _ = io.WriteString(res.W, `{"error":"email must be a string"}`)
			}},
			{Method: "POST", Path: "/user/password/set", NoLogin: true, Handler: limit(setNewPassword(a, mail, tok))},
			{Method: "POST", Path: "/user/reconfirm", Handler: limit(requestReset)},
		},
	}
}

// ---------- handler internals ----------

// ---------- handler internals ----------

func parseEmail(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if !rw.MatchString(s) {
		return ""
	}
	return s
}

func stringValid(s string) bool { return s != "" }

func appName() string {
	if v := os.Getenv("OVERLEAF_APP_NAME"); v != "" {
		return v
	}
	return "OlliTeX"
}

// setNewPassword is the POST /user/password/set handler constructor.

func respondJSON(res *core.Res, code int, body string) {
	res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
	res.W.WriteHeader(code)
	_, _ = io.WriteString(res.W, body)
}

// removeUserSessions scans the redis keyspace and dels session docs whose
// passport.user._id or user._id matches (Node UserSessionsManager parity).

func sanitizePw(p string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', '\t':
			return -1
		}
		return r
	}, p)
}

func bcryptRounds() int {
	if v := os.Getenv("BCRYPT_ROUNDS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n >= 4 {
			return n
		}
	}
	return 12
}

func bcryptHash(pw string, rounds int) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), rounds)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// validatePassword mirrors AuthenticationManager.validatePassword (CE
// order): too_short → too_long → invalid_character → contains_email.
// Returns (0,"") when valid; code 1..4 selects the response key.

// validatePassword mirrors AuthenticationManager.validatePassword (CE
// order): too_short → too_long → invalid_character → contains_email.
// Returns (0,"") when valid; code 1..4 selects the response key.
func validatePassword(password, email string) (code int, text string) {
	const (
		minLen int = 8
		maxLen int = 72
	)
	ln := len(password)
	switch {
	case ln < minLen:
		return 1, "Password too short, minimum 8."
	case ln > maxLen:
		return 2, "Password too long, maximum 72."
	}
	allowed := func(r rune) bool {
		if r == '£' || r == '€' {
			return true // listed in the CE symbol set
		}
		if r < 0x80 {
			b := byte(r)
			if (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') {
				return true
			}
			return strings.IndexByte("@#$%^&*()-_=+[]{};:<>/?!.,", b) >= 0
		}
		return false // other code points fail Node's indexOf allow-list
	}
	for _, r := range password {
		if !allowed(r) {
			return 3, "Password contains an invalid character."
		}
	}
	if email != "" {
		startOfEmail := email
		if i := strings.IndexByte(email, '@'); i >= 0 {
			startOfEmail = email[:i]
		}
		if strings.Contains(password, email) ||
			(startOfEmail != "" && strings.Contains(password, startOfEmail)) ||
			strings.Contains(email, password) {
			return 4, "Password cannot contain parts of email address."
		}
	}
	return 0, ""
}
