package authpages

import (
	"log"
	"strings"

	"ollitex/go/services/web/core"
)

// POST /logout — Node UserController.logout:
// passport.logout + session.destroy (DEL doc) → 302 (body.redirect || "/login").
// Pinned: 302 Location /login, "Found. Redirecting to /login", old cookie dead.
func postLogout(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		var body struct {
			Redirect string `json:"redirect"`
		}
		_ = decodeBody(cxt, &body)
		if cxt.Sess != nil {
			if cxt.Sess.IsLoggedIn() {
				// Node doLogout: untrackSession(user, sessionId) (SREM +
				// PEXPIRE) around session.destroy.
				sid := cxt.Sess.SessID
				uid := cxt.Sess.UserIDHex()
				// audit L5: a Destroy failure otherwise leaves the session doc
				// (and tracking entry) alive after "logout" — surface it.
				if derr := a.Store.Destroy(sid); derr != nil {
					log.Printf("logout: session destroy %s failed: %v", sid, derr)
				}
				if uid != "" {
					a.UntrackSession(uid, sid)
				}
			} else {
				if derr := a.Store.Destroy(cxt.Sess.SessID); derr != nil {
					log.Printf("logout: anonymous session destroy %s failed: %v", cxt.Sess.SessID, derr)
				}
			}
		}
		target := strings.TrimSpace(body.Redirect)
		// audit C2 (open-redirect): only root-relative paths are honored;
		// anything else (absolute URL, protocol-relative //evil, backslash
		// tricks) falls back to /login. Node's `res.redirect(body.redirect ||
		// '/login')` had no validation — Go hardens this (Go-first security).
		if !validLogoutRedirect(target) {
			target = "/login"
		}
		res.Redirect(cxt.Req, 302, target)
	}
}

// validLogoutRedirect — audit C2: a post-logout Location must be a
// same-origin, root-relative path (`/...`). Rejects: empty (caller handles
// default), any scheme (`http:`/`javascript:`), protocol-relative (`//x`),
// and single-leading-slash violations.
func validLogoutRedirect(t string) bool {
	if t == "" || t[0] != '/' {
		return false
	}
	if len(t) > 1 && t[1] == '/' { // protocol-relative → browser treats as host
		return false
	}
	// a scheme like `foo/...` is impossible here (must start with /), but
	// guard against injection tricks: backslash (browsers normalize /\x → //
	// x, reopening protocol-relative) and control/whitespace characters.
	for i := 0; i < len(t); i++ {
		if t[i] == '\\' || t[i] < 0x20 || t[i] == 0x7f {
			return false
		}
	}
	return true
}

// ---- POST /login (password) ----
//
// Success (acceptsJson): 200 {"redir":<postLoginRedirect||"/project">}
// Success (form):          302 Location:<same>
// In both: Set-Cookie = REGENERATED sid (doc: cookie, justLoggedIn:true,
// passport.user lightUser, analyticsId, csrfSecret), old doc DELed.
// Pinned failures:
//   unknown user / bad password → 401 {"message":{"type":"error",
//     "key":"invalid-password-retry-or-reset"}} (+ rolling cookie;
//     lastFailedLogin written when user exists)
//   malformed email             → 401 {"message":{"message":"This SSO login
//     option is not enabled.","type":"error","key":"invalid-password-retry-
//     or-reset"}}
//   loginEpoch mismatch         → 429 {"message":{}}
//   suspended (password ok)     → /account-suspended redirect, no login

// debugAuth — temporary diagnostics (WEB_GO_DEBUG_AUTH=1).
