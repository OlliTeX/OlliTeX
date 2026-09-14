package core

import (
	"os"
	"strconv"
)

// UserSessions — the Node UserSessionsManager parity surface
// (services/web/app/src/Features/User/UserSessionsManager.mjs), pinned:
//
//	set key   UserSessions:{<userIdHex>}
//	            (cluster hash-tag braces — the Node key is pinned EXACT:
//	            UserSessionsRedis.sessionSetKey returns `UserSessions:${user._id}`
//	            with the curly braces; bare `UserSessions:<uid>` is a
//	            DIFFERENT key and split-brains the session ledger — pinned
//	            from redis MONITOR 2026-09-14)
//	member    sess:<s32.sid>            (express-session doc key)
//	ttl       Settings.cookieSessionLength (default 5*24*3600*1000 ms)
//
// trackSession   SADD + PEXPIRE set
// untrackSession SREM + PEXPIRE set
// _checkSessions lazily srem members whose sess: doc no longer exists
//
// Node calls trackSession on every successful login (AuthenticationController)
// and untrackSession on logout (UserController.doLogout). Go P3.3 adds the
// same calls on its login/logout (features/authpages).
func UserSessionsKey(userIDHex string) string { return "UserSessions:{" + userIDHex + "}" }

func SessionDocKey(sid string) string { return "sess:" + sid }

// CookieSessionLengthMs — Node Settings.cookieSessionLength (5 days),
// env-overridable as in server-ce settings.js.
func CookieSessionLengthMs() int64 {
	if v, err := strconv.ParseInt(
		os.Getenv("COOKIE_SESSION_LENGTH"), 10, 64); err == nil && v > 0 {
		return v
	}
	return 5 * 24 * 60 * 60 * 1000
}

// TrackSession mirrors trackSession(user, sessionId): SADD the session doc
// key into the per-user set + refresh the set TTL. Never fails the request
// (Node: the login flow ignores track rejections).
func (a *App) TrackSession(userIDHex, sid string) {
	key := UserSessionsKey(userIDHex)
	_ = a.Redis.SADD(key, SessionDocKey(sid))
	_ = a.Redis.PEXPIRE(key, CookieSessionLengthMs())
}

// UntrackSession mirrors untrackSession: SREM + PEXPIRE.
func (a *App) UntrackSession(userIDHex, sid string) {
	key := UserSessionsKey(userIDHex)
	_ = a.Redis.SREM(key, SessionDocKey(sid))
	_ = a.Redis.PEXPIRE(key, CookieSessionLengthMs())
}
