package authpages

import (
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
				_ = a.Store.Destroy(sid)
				if uid != "" {
					a.UntrackSession(uid, sid)
				}
			} else {
				_ = a.Store.Destroy(cxt.Sess.SessID)
			}
		}
		target := body.Redirect
		if target == "" {
			target = "/login"
		}
		res.Redirect(cxt.Req, 302, target)
	}
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
