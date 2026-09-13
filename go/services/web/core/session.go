package core

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Session is the request-scoped session state (the express-session object).
//
// The persisted document shape (pinned from live redis on the ol-e2e
// stack, 2026-09-16) for an anonymous session:
//
//	{
//	  "cookie": {"originalMaxAge": 432000000,
//	               "expires": "2026-09-18T...Z",
//	               "secure": false, "httpOnly": true, "path": "/", "sameSite": "lax"},
//	  "csrfSecret": "<24 base64url>",
//	  "inbound": {"referrer": {...}, "utm": null},
//	  "validationToken": "v1:<last 4 chars of sid>"
//	}
//
// A logged-in session adds (passport):
//
//	"passport": {"user": { <user doc as serialized by the web app> }}
//
// plus "justLoggedIn": true on the login response.
//
// Go must preserve unknown fields on read-modify-write (both stacks share
// the store): the session is carried as a generic JSON map.
type Session struct {
	// SessID is the raw session id (WITHOUT the 's:' cookie prefix). The
	// redis key is "sess:" + SessID.
	SessID string

	// Doc is the full session document (raw JSON map). Known fields are
	// accessed via the helpers below.
	Doc map[string]json.RawMessage `json:"-"`

	// Cookie holds the parsed session cookie settings (persisted under
	// "cookie").
	Cookie *SessionCookie `json:"-"`

	changed    bool
	expires    time.Time
	newSession bool
}

// SessionCookie is the express-session cookie subdocument.
type SessionCookie struct {
	OriginalMaxAge int64  `json:"originalMaxAge"`
	Expires        string `json:"expires"` // ISO8601 (RFC3339)
	Secure         bool   `json:"secure"`
	HTTPOnly       bool   `json:"httpOnly"`
	Path           string `json:"path"`
	SameSite       string `json:"sameSite"`
}

// SessionStore is the connect-redis-6.1.3-compatible session store.
type SessionStore struct {
	rdb    *RedisClient
	cfg    *Config
	newSID func() string // injectable for tests
}

func NewSessionStore(rdb *RedisClient, cfg *Config) *SessionStore {
	return &SessionStore{rdb: rdb, cfg: cfg, newSID: NewSessionID}
}

// NewSessionID is the express-session-style raw id: 24 random bytes → 32
// base64url chars (observed live: "overleaf.sid=s:<32ch>.<43ch sig>").
func NewSessionID() string { return newRandomBase64Url(24) }

const sessionKeyPrefix = "sess:" // connect-redis 6.1.3 default prefix

// ---------- persistence (CustomSessionStore parity) ----------

func (st *SessionStore) docKey(sid string) string { return sessionKeyPrefix + sid }

// validationToken parity: CustomSessionStore.computeValidationToken(sid)
// = 'v1:' + sid.slice(-4).
func validationToken(sid string) string {
	if len(sid) < 4 {
		return "v1:" + sid
	}
	return "v1:" + sid[len(sid)-4:]
}

// Get loads the session for sid (nil, nil if absent or the validation
// token is out of sync — Node rejects those too).
func (st *SessionStore) Get(sid string) (*Session, error) {
	raw, ok, err := st.rdb.GET(st.docKey(sid))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err // Node: parse error → cb(err)
	}
	if tok, hasTok := doc["validationToken"]; !hasTok {
		return nil, nil // CustomSessionStore rejects missing token
	} else {
		var s string
		if json.Unmarshal(tok, &s) != nil || s != validationToken(sid) {
			return nil, nil // "session token validation failed" → cb(err,null)
		}
	}
	return st.fromDoc(sid, doc), nil
}

// persist writes sess using the Node NX/XX decision:
//   - new session (key did not exist): SET … NX EX
//   - in-place update (key existed):    SET … XX EX
//
// The TTL is cookie.expires - now (connect-redis _getTTL), floored at 1s.
func (st *SessionStore) persist(sess *Session) error {
	doc := sess.Doc
	if doc == nil {
		doc = map[string]json.RawMessage{}
		sess.Doc = doc
	}
	vt, _ := json.Marshal(validationToken(sess.SessID))
	doc["validationToken"] = vt
	if sess.Cookie != nil {
		cj, err := json.Marshal(sess.Cookie)
		if err != nil {
			return err
		}
		doc["cookie"] = cj
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	ttl := time.Until(sess.expires)
	if ttl < time.Second {
		ttl = time.Second
	}
	key := st.docKey(sess.SessID)
	if sess.newSession {
		sess.newSession = false // subsequent saves are XX (Node: store.create once, then store.touch)
		return st.rdb.SETNXEX(key, string(raw), ttl)
	}
	if err := st.rdb.SETXXEX(key, string(raw), ttl); err != nil {
		return err
	}
	// if the key had expired in between, Node's XX no-ops; create it
	// (express-session re-saves a resurrected session on first touch)
	return st.rdb.SETNXEX(key, string(raw), ttl)
}

func (st *SessionStore) Destroy(sid string) error { return st.rdb.DEL(st.docKey(sid)) }

// ---------- request plumbing ----------

// fromDoc builds the Session view over a parsed document.
func (st *SessionStore) fromDoc(sid string, doc map[string]json.RawMessage) *Session {
	s := &Session{SessID: sid, Doc: doc}
	if raw, ok := doc["cookie"]; ok {
		var c SessionCookie
		if json.Unmarshal(raw, &c) == nil {
			s.Cookie = &c
		}
	}
	if s.Cookie == nil {
		now := time.Now()
		c := &SessionCookie{
			OriginalMaxAge: st.cfg.CookieLengthMS,
			Expires:        now.Add(st.cfg.CookieLength).UTC().Format(time.RFC3339),
			Secure:         st.cfg.SecureCookie,
			HTTPOnly:       true,
			Path:           "/",
			SameSite:       st.cfg.SameSite,
		}
		s.Cookie = c
		s.changed = true
	}
	s.expires, _ = time.Parse(time.RFC3339, s.Cookie.Expires)
	if s.expires.IsZero() {
		s.expires = time.Now().Add(st.cfg.CookieLength)
	}
	return s
}

// StartAnonymous creates (or returns) the session for the incoming cookie.
// Mirrors express-session(resave:false, saveUninitialized:false): a
// brand-new unmodified session is NOT persisted and gets NO cookie (Node
// sets overleaf.sid only once the session is real — login, csrf use, …).
func (st *SessionStore) StartAnonymous(req *http.Request) (*Session, error) {
	if st.isDisabled(req) {
		return &Session{SessID: "", Doc: map[string]json.RawMessage{}}, nil
	}
	raw, _ := req.Cookie(st.cfg.CookieName)
	if raw == nil {
		// brand new anonymous session
		cs := st.newAnonymousSession()
		return cs, nil
	}
	sid := strings.TrimPrefix(raw.Value, "s:")
	sid = unsignCookie(sid, st.cfg.SessionSecrets)
	if sid == "" {
		// bad signature → treat as new
		cs := st.newAnonymousSession()
		return cs, nil
	}
	sess, err := st.Get(sid)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		cs := st.newAnonymousSession()
		return cs, nil
	}
	if st.cfg.RollingSession {
		sess.touch()
	}
	return sess, nil
}

func (st *SessionStore) newAnonymousSession() *Session {
	now := time.Now()
	exp := now.Add(st.cfg.CookieLength)
	s := &Session{
		SessID:    st.newSID(),
		Doc:       map[string]json.RawMessage{},
		expires:   exp,
		newSession: true,
		Cookie: &SessionCookie{
			OriginalMaxAge: st.cfg.CookieLengthMS,
			Expires:        exp.UTC().Format(time.RFC3339),
			Secure:         st.cfg.SecureCookie,
			HTTPOnly:       true,
			Path:           "/",
			SameSite:       st.cfg.SameSite,
		},
	}
	// csurf lazy contract (pinned vs Node): the secret is allocated on
	// FIRST USE (CsrfSecret()/token path), not at session birth — Node's
	// /status (publicApiRouter) and unmodified views create no doc, while
	// the first csrf-verified request DOES (secret allocated, doc saved,
	// cookie issued).
	return s
}

func (st *SessionStore) isDisabled(req *http.Request) bool {
	// bot probes skip session creation (SessionAutostartMiddleware parity)
	ua := strings.ToLower(req.UserAgent())
	for _, bot := range []string{"kube-probe", "googletest"} {
		if strings.Contains(ua, bot) {
			return true
		}
	}
	return false
}

// touch implements express-session rolling (cookie.expires = now +
// originalMaxAge).
func (s *Session) touch() {
	if s.Cookie == nil {
		return
	}
	max := time.Duration(s.Cookie.OriginalMaxAge) * time.Millisecond
	if max <= 0 {
		max = 5 * 24 * time.Hour
	}
	s.expires = time.Now().Add(max)
	s.Cookie.Expires = s.expires.UTC().Format(time.RFC3339)
	s.changed = true
}

// Set stores an arbitrary top-level field (e.g. passport.user on login).
func (s *Session) Set(key string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	if s.Doc == nil {
		s.Doc = map[string]json.RawMessage{}
	}
	s.Doc[key] = raw
	s.changed = true
}

// GetRaw returns the raw JSON of a top-level field.
func (s *Session) GetRaw(key string) (json.RawMessage, bool) {
	if s.Doc == nil {
		return nil, false
	}
	v, ok := s.Doc[key]
	return v, ok
}

// CsrfSecret returns the per-session CSRF secret (csurf contract),
// allocating one on first read.
func (s *Session) CsrfSecret() string {
	var sec string
	if raw, ok := s.Doc["csrfSecret"]; ok {
		json.Unmarshal(raw, &sec)
	}
	if sec == "" {
		sec = NewCsrfSecret()
		raw, _ := json.Marshal(sec)
		s.Doc["csrfSecret"] = raw
		s.changed = true
	}
	return sec
}

// CsrfToken returns a fresh render token for this session (req.csrfToken()).
func (s *Session) CsrfToken() string { return CsrfToken(s.CsrfSecret()) }

// IsLoggedIn mirrors SessionManager.isUserLoggedIn:
//
//	req.session ? Boolean(req.session.user || (req.session.passport &&
//	    req.session.passport.user)) : false
//
// (passport serializes user under session.passport.user; the legacy
// session.user is honoured too.)
func (s *Session) IsLoggedIn() bool {
	if s.SessID == "" {
		return false
	}
	if raw, ok := s.Doc["user"]; ok && string(raw) != "null" {
		return true
	}
	raw, ok := s.Doc["passport"]
	if !ok {
		return false
	}
	var p struct {
		User json.RawMessage `json:"user"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return false
	}
	return string(p.User) != "" && string(p.User) != "null"
}

// LogoutSessionID is provided for feature code; destroying happens via the
// store.
func (s *Session) Key() string { return sessionKeyPrefix + s.SessID }

// Set-Cookie value produced on new/session-cycling responses
// (express-session setcookie: 's:' + sign(sid, secret)).
func (cfg *Config) SignedCookieValue(sid string) string {
	sec := cfg.SessionSecrets[0]
	if sec == "" {
		panic("no session secret configured")
	}
	return "s:" + signCookie(sid, sec)
}

// writeSessionCookie emits the overleaf.sid cookie exactly as
// express-session + the `cookie` module do (Path/HttpOnly/SameSite/
// Secure/Expires; the observed live header order is
// "Path=/; Expires=...; HttpOnly; SameSite=Lax" — Go emits attributes in
// its canonical order; browsers/Node do not depend on the order, but we
// pin the SET itself so the value+flags are identical).
func (s *Session) writeSessionCookie(w http.ResponseWriter, cfg *Config) {
	if s.SessID == "" {
		return
	}
	max := time.Duration(s.Cookie.OriginalMaxAge) * time.Millisecond
	if max <= 0 {
		return
	}
	c := &http.Cookie{
		Name:     cfg.CookieName,
		Value:    cfg.SignedCookieValue(s.SessID),
		Path:     s.Cookie.Path,
		MaxAge:   int(time.Until(s.expires).Seconds()),
		Expires:  s.expires,
		HttpOnly: true,
		Secure:   cfg.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	}
	if cfg.SameSite == "strict" {
		c.SameSite = http.SameSiteStrictMode
	} else if cfg.SameSite == "none" {
		c.SameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, c)
}

var _ = rand.Reader // (crypto/rand flows through newRandomBase64Url)

func init() {
	// sanity: keep the compiler honest about constant shapes used in
	// pinned tests
	if validationToken("abcd") != "v1:abcd" {
		panic(fmt.Sprintf("validationToken regression: %q", validationToken("abcd")))
	}
}
