package realtime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Session cookie pipeline — pinned 1:1 to the Go web contract (the same
// helpers the collab service uses; see go/services/collab/mongo.go):
//
//	cookie value = percent-encode("s:" + sign(sid, secret))
//	sign(val)     = val + "." + base64Std(HMAC-SHA256(secret, val)), trailing "=" trimmed
//
// Decoding: pctDecode → TrimPrefix "s:" → unsign against the secret chain.
// A failure at any step is "no session": the bus answers with
// connectionRejected {message:"invalid session"} + disconnect, exactly the
// Node real-time SessionSockets error path.

func signCookie(val, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(val))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return val + "." + strings.TrimRight(sig, "=")
}

func unsignCookie(val string, secrets []string) string {
	i := strings.LastIndex(val, ".")
	if i < 0 {
		return ""
	}
	candidate := val[:i]
	for _, s := range secrets {
		if s == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(signCookie(candidate, s)), []byte(val)) == 1 {
			return candidate
		}
	}
	return ""
}

// pctDecode mirrors the web's decodeCookieValue; invalid sequences fall
// through unchanged (the web does the same via try/catch).
func pctDecode(v string) string {
	if !strings.Contains(v, "%") {
		return v
	}
	if u, err := url.PathUnescape(v); err == nil {
		return u
	}
	return v
}

// sessionSid — cookie value → raw sid ("" = reject/miss).
func sessionSid(v string, secrets []string) string {
	v = pctDecode(v)
	v = strings.TrimPrefix(v, "s:")
	return unsignCookie(v, secrets)
}

// defaultCookieName — the web's COOKIE_NAME default (collab pins the same).
const defaultCookieName = "overleaf.sid"

// SessionSource — the shared session store (the same keys the Go web writes:
// "sess:<sid>" → JSON session doc, written through the session cookie chain).
type SessionSource interface {
	// Session returns the JSON session doc for sid, or nil if missing.
	Session(ctx context.Context, sid string) (map[string]any, error)
}

// SessionResolver resolves the browser's session cookie to (sid, sessionDoc)
// so the bus can answer the Node real-time's identity questions:
//
//	user            = session.passport.user  || session.user
//	anonToken[pid] = session.anonTokenAccess[pid]
type SessionResolver struct {
	Source     SessionSource
	CookieName string
	Secrets    []string
}

// Sid extracts + authenticates the session id from the request cookie.
// ok=false → no cookie, bad signature, or unknown chain (≙ "invalid session").
func (s *SessionResolver) Sid(r *http.Request) (string, bool) {
	name := s.CookieName
	if name == "" {
		name = defaultCookieName
	}
	c, err := r.Cookie(name)
	if err != nil || c.Value == "" {
		return "", false
	}
	sid := sessionSid(c.Value, s.Secrets)
	if sid == "" {
		return "", false
	}
	return sid, true
}

// sessionUser — identity + identity metadata for the bus context.
type sessionUser struct {
	ID        string // "" = anonymous
	FirstName string
	LastName  string
	Email     string
}

func (s *SessionResolver) User(ctx context.Context, r *http.Request) (*sessionUser, bool) {
	sid, ok := s.Sid(r)
	if !ok || s.Source == nil {
		return nil, false
	}
	doc, err := s.Source.Session(ctx, sid)
	if err != nil || doc == nil {
		return nil, false
	}
	u, ok := sessionUserFromDoc(doc)
	if !ok {
		return nil, false
	}
	return u, true
}

// AnonToken returns the anonymous access token the session holds for pid
// (session.anonTokenAccess[pid], set when a shared link was opened).
func (s *SessionResolver) AnonToken(ctx context.Context, r *http.Request, pid string) (string, bool) {
	sid, ok := s.Sid(r)
	if !ok || s.Source == nil || pid == "" {
		return "", false
	}
	doc, err := s.Source.Session(ctx, sid)
	if err != nil || doc == nil {
		return "", false
	}
	if t, ok := anonTokenFor(doc, pid); ok {
		return t, true
	}
	return "", false
}

func sessionUserFromDoc(doc map[string]any) (*sessionUser, bool) {
	var u map[string]any
	if p, ok := doc["passport"].(map[string]any); ok {
		u, _ = p["user"].(map[string]any)
	}
	if u == nil {
		u, _ = doc["user"].(map[string]any)
	}
	if u == nil {
		return nil, false
	}
	id, _ := u["_id"].(string)
	if id == "" {
		return nil, false
	}
	out := &sessionUser{ID: id}
	out.FirstName, _ = u["first_name"].(string)
	out.LastName, _ = u["last_name"].(string)
	out.Email, _ = u["email"].(string)
	return out, true
}

func anonTokenFor(doc map[string]any, pid string) (string, bool) {
	ata, ok := doc["anonTokenAccess"].(map[string]any)
	if !ok {
		return "", false
	}
	t, _ := ata[pid].(string)
	if t == "" {
		return "", false
	}
	return t, true
}

var objectIdRe = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

func isObjectID(s string) bool { return objectIdRe.MatchString(s) }
