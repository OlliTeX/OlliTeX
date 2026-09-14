package core

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------- fake RESP server (in-memory) ----------

type fakeRedis struct {
	mu   sync.Mutex
	conn net.Conn
	m    map[string]string // key -> value
	ttl  map[string]int
	ops  []string // command log (for NX/XX assertions)
}

func startFakeRedis(t *testing.T) (*fakeRedis, string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fr := &fakeRedis{m: map[string]string{}, ttl: map[string]int{}}
	go fr.serve(l)
	t.Cleanup(func() { l.Close() })
	return fr, l.Addr().String()
}

func (fr *fakeRedis) serve(l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go fr.handle(c)
	}
}

func (fr *fakeRedis) handle(c net.Conn) {
	defer c.Close()
	fr.mu.Lock()
	fr.conn = c
	fr.mu.Unlock()
	br := bufio.NewReader(c)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "*") {
			c.Write([]byte("-bad\r\n"))
			continue
		}
		n := atoi(line[1:])
		args := make([]string, 0, n)
		for i := 0; i < n; i++ {
			h, err := br.ReadString('\n')
			if err != nil {
				return
			}
			h = strings.TrimSpace(h)
			if !strings.HasPrefix(h, "$") {
				c.Write([]byte("-bad\r\n"))
				return
			}
			size := atoi(h[1:])
			buf := make([]byte, size+2)
			io.ReadFull(br, buf)
			args = append(args, string(buf[:size]))
		}
		fr.mu.Lock()
		fr.ops = append(fr.ops, strings.Join(args, " "))
		fr.mu.Unlock()
		c.Write(fr.exec(args))
	}
}

func atoi(s string) int {
	n := 0
	neg := false
	for i, r := range s {
		if i == 0 && r == '-' {
			neg = true
			continue
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		n = -n
	}
	return n
}

func (fr *fakeRedis) exec(args []string) []byte {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	cmd := strings.ToUpper(args[0])
	switch cmd {
	case "PING":
		return []byte("+PONG\r\n")
	case "GET":
		if v, ok := fr.m[args[1]]; ok {
			return bulk(v)
		}
		return []byte("$-1\r\n")
	case "SET":
		key, val := args[1], args[2]
		isNX := contains(args, "NX")
		isXX := contains(args, "XX")
		_, exists := fr.m[key]
		var ttlSecs int = 1
		if i := indexof(args, "EX"); i >= 0 && i+1 < len(args) {
			ttlSecs = atoi(args[i+1])
		}
		switch {
		case isNX && exists:
			return []byte("$-1\r\n") // NX failed (null reply)
		case isXX && !exists:
			return []byte("$-1\r\n")
		default:
			fr.m[key] = val
			if ttlSecs > 0 {
				fr.ttl[key] = ttlSecs
			}
			return []byte("+OK\r\n")
		}
	case "DEL":
		delete(fr.m, args[1])
		return []byte(":1\r\n")
	case "TTL":
		return []byte(":" + itoa(fr.ttl[args[1]]) + "\r\n")
	case "INCRBY":
		v := atoi(args[2])
		fr.m[args[1]] = itoa(v)
		return []byte(":" + itoa(v) + "\r\n")
	}
	return []byte("-ERR unknown command\r\n")
}

func bulk(v string) []byte {
	return []byte("$" + itoa(len(v)) + "\r\n" + v + "\r\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func contains(args []string, s string) bool {
	for _, a := range args {
		if strings.EqualFold(a, s) {
			return true
		}
	}
	return false
}

func indexof(args []string, s string) int {
	for i, a := range args {
		if strings.EqualFold(a, s) {
			return i
		}
	}
	return -1
}

// ---------- pinned crypto tests (Node values from the live stack) ----------

// testCfg returns a config with the KNOWN e2e-stack session secret.
func testCfg(t *testing.T, secret string) *Config {
	t.Helper()
	return &Config{
		Profile:           "web",
		ListenAddr:        "127.0.0.1:0",
		SessionSecrets:    []string{secret},
		CookieName:        "overleaf.sid",
		CookieLength:      5 * 24 * time.Hour,
		CookieLengthMS:    5 * 24 * 60 * 60 * 1000,
		SecureCookie:      false,
		SameSite:          "lax",
		RollingSession:    true,
		CacheStaticAssets: true,
		CookieDomain:      "",
	}
}

// cookie-signature (stack-resolved 1.2.x): std base64, padding stripped.
func nodeBase64NoPad(b []byte) string {
	return strings.TrimRight(base64.StdEncoding.EncodeToString(b), "=")
}

func nodeSign(val, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(val))
	return val + "." + nodeBase64NoPad(mac.Sum(nil))
}

func b64urlNoPad(b []byte) string { return urlB64.EncodeToString(b) }

// TestCookieSignAgainstLiveValue pins the live e2e cookie:
// overleaf.sid=s:kUhalKuiiDPyIh10iIOFKT1mGyDyzdER.lvlayHwk7wCLrCj8Dp9mVzdaR4PcV4eFCdSRhRg2NJ8
// (session secret = the e2e .env.test OVERLEAF_SESSION_SECRET at capture
// time; the RELATION is what matters — recompute with a known secret).
func TestCookieSignAgainstLiveValue(t *testing.T) {
	const secret = "3a6ac8a3knownsecretfortesting"
	val := "kUhalKuiiDPyIh10iIOFKT1mGyDyzdER"
	got := signCookie(val, secret)
	want := nodeSign(val, secret)
	if got != want {
		t.Fatalf("signCookie: got %q want %q", got, want)
	}
	if unsignCookie(got, []string{"wrong", secret}) != val {
		t.Fatalf("unsignCookie failed to recover %q", val)
	}
	if unsignCookie(got, []string{"onlyWrong"}) != "" {
		t.Fatal("unsignCookie must reject with wrong secrets")
	}
	if strings.Count(signCookie(val, secret), ".") != 1 || strings.Count(signCookie(val, secret), "-") != 0 && strings.Count(signCookie(val, secret), "_") != 0 && strings.Count(signCookie(val, secret), "+") != 0 {
		// no requirement — just exercising
	}
}

func TestCsrfTokenShapeAndVerification(t *testing.T) {
	secret := "drzwCockORpWF_0nbHrzUdQh" // observed live csrfSecret
	tok := CsrfToken(secret)
	if !VerifyCsrfToken(secret, tok) {
		t.Fatal("fresh token must verify")
	}
	if VerifyCsrfToken("different-secret-123", tok) {
		t.Fatal("different secret must not verify")
	}
	tok2 := CsrfToken(secret)
	if tok == tok2 {
		t.Fatal("expected distinct random salts")
	}
	if !VerifyCsrfToken(secret, tok2) {
		t.Fatal("any valid-salt token must verify for the same secret")
	}
}

// TestCsrfTokenMatchesNodeAlgorithm pins the csrf@3.1.0 derivation with a
// fixed salt (Node: token = salt + '-' + b64url(SHA1(salt+'-'+secret))):
func TestCsrfTokenMatchesNodeAlgorithm(t *testing.T) {
	const secret = "drzwCockORpWF_0nbHrzUdQh"
	const salt = "yJOxYeKt" // observed live salt shape (8 base62 chars)
	sha := sha1.Sum([]byte(salt + "-" + secret))
	want := salt + "-" + b64urlNoPad(sha[:])
	// build the same token via our internal pieces
	got := salt + "-" + csrfHash(salt+"-"+secret)
	if got != want {
		t.Fatalf("token derivation: got %q want %q", got, want)
	}
	if !VerifyCsrfToken(secret, want) {
		t.Fatal("Node-derived token must verify on Go")
	}
	if VerifyCsrfToken(secret, salt+"-"+b64urlNoPad(sha[:][:10])) {
		t.Fatal("truncated token must not verify")
	}
}

func TestNewCsrfSecretShape(t *testing.T) {
	s := NewCsrfSecret()
	if len(s) != 24 {
		t.Fatalf("csrfSecret must be 24 base64url chars, got %d (%q)", len(s), s)
	}
}

func TestEtagPinnedLiveValue(t *testing.T) {
	body := "web is alive (web)"
	got := EtagWeakBody(body)
	// W/"12-1XIqlzCgkZ5qAye222Y2URlsyEU" — the live /status ETag
	want := `W/"12-1XIqlzCgkZ5qAye222Y2URlsyEU"`
	if got != want {
		t.Fatalf("etag: got %q want %q", got, want)
	}
}

func TestEtagForbidden(t *testing.T) {
	got := EtagWeakBody("Forbidden")
	if !strings.HasPrefix(got, `W/"9-`) {
		t.Fatalf("Forbidden etag prefix: %q", got)
	}
}

// ---------- session store tests ----------

func TestSessionStoreRoundTrip(t *testing.T) {
	fr, addr := startFakeRedis(t)
	rc := &RedisClient{Addr: addr}
	rc.command("PING")
	cfg := testCfg(t, "test-secret")
	st := NewSessionStore(rc, cfg)

	sess := st.newAnonymousSession("")
	if sess.SessID == "" {
		t.Fatal("new session must have an id")
	}
	if len(sess.SessID) != 32 {
		t.Fatalf("sid must be 32 chars (uid-safe shape), got %d: %q", len(sess.SessID), sess.SessID)
	}
	// lazy-csrf contract: first USE allocates the secret and marks the
	// session modified (then it persists like any real session)
	_ = sess.CsrfToken()
	if !sess.changed {
		t.Fatal("first csrf use must mark the session modified")
	}
	if err := st.persist(sess); err != nil {
		t.Fatal(err)
	}
	raw, ok := fr.m["sess:"+sess.SessID]
	if !ok {
		t.Fatal("session not stored under sess:<sid>")
	}
	var doc map[string]any
	json.Unmarshal([]byte(raw), &doc)
	if doc["validationToken"] != "v1:"+sess.SessID[len(sess.SessID)-4:] {
		t.Fatalf("validationToken mismatch: %v", doc["validationToken"])
	}
	if _, hasCsrf := doc["csrfSecret"]; !hasCsrf {
		t.Fatal("session used for csrf must carry csrfSecret (csurf contract)")
	}
	cookieRaw, _ := doc["cookie"].(map[string]any)
	if cookieRaw["originalMaxAge"] != float64(5*24*60*60*1000) {
		t.Fatalf("originalMaxAge: %v", cookieRaw["originalMaxAge"])
	}
	// round trip
	got, err := st.Get(sess.SessID)
	if err != nil || got == nil {
		t.Fatalf("Get: %v / %v", got, err)
	}
	if got.CsrfSecret() != sess.CsrfSecret() {
		t.Fatal("csrfSecret must survive the round trip")
	}
}

func TestSessionStoreRejectsBadValidationToken(t *testing.T) {
	fr, addr := startFakeRedis(t)
	rc := &RedisClient{Addr: addr}
	cfg := testCfg(t, "s")
	st := NewSessionStore(rc, cfg)
	// Node CustomSessionStore rejects doc.validationToken != 'v1:'+sid[-4:]
	fr.mu.Lock()
	fr.m["sess:abcd1234"] = `{"cookie":{"originalMaxAge":1,"expires":"2026-01-01T00:00:00Z","secure":false,"httpOnly":true,"path":"/","sameSite":"lax"},"validationToken":"v1:WRNG"}`
	fr.mu.Unlock()
	sess, err := st.Get("abcd1234")
	if err != nil {
		t.Fatal(err)
	}
	if sess != nil {
		t.Fatal("out-of-sync validationToken must be rejected (Node parity)")
	}
}

// TestNXXXSemantics pins connect-redis + CustomSessionStore: new session
// SETs with NX, in-place update with XX.
func TestNXXXSemantics(t *testing.T) {
	fr, addr := startFakeRedis(t)
	rc := &RedisClient{Addr: addr}
	cfg := testCfg(t, "s")
	st := NewSessionStore(rc, cfg)
	n1 := st.newAnonymousSession("")
	if err := st.persist(n1); err != nil {
		t.Fatal(err)
	}
	foundNX := false
	for _, op := range fr.ops {
		if strings.Contains(op, "sess:") && strings.Contains(op, " NX") {
			foundNX = true
		}
	}
	if !foundNX {
		t.Fatalf("new session must use SET … NX (ops: %v)", fr.ops)
	}
	n1.Doc["extra"] = mustRawJSON("true")
	n1.changed = true
	if err := st.persist(n1); err != nil {
		t.Fatal(err)
	}
	foundXX := false
	for _, op := range fr.ops {
		if strings.Contains(op, "sess:") && strings.Contains(op, " XX") {
			foundXX = true
		}
	}
	if !foundXX {
		t.Fatalf("in-place update must use SET … XX (ops: %v)", fr.ops)
	}
}

func mustRawJSON(s string) json.RawMessage {
	return json.RawMessage(s)
}

// ---------- app-level parity tests ----------

func newTestApp(t *testing.T, profile, secret string) (*App, *fakeRedis, string) {
	t.Helper()
	fr, addr := startFakeRedis(t)
	rc := &RedisClient{Addr: addr}
	cfg := testCfg(t, secret)
	cfg.Profile = profile
	app := New(cfg, rc)
	return app, fr, addr
}

func doReq(t *testing.T, h http.Handler, method, target string, headers map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	var err error
	if body != "" {
		r, err = http.NewRequest(method, target, strings.NewReader(body))
	} else {
		r, err = http.NewRequest(method, target, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAppStatusParity(t *testing.T) {
	app, _, _ := newTestApp(t, "web", "sec")
	app.RegisterFeature(testStatusFeature())
	w := doReq(t, app.Handler(), "GET", "/status", nil, "")
	if w.Code != 200 {
		t.Fatalf("status: %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("ct: %q", ct)
	}
	if nosniff := w.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("nosniff: %q", nosniff)
	}
	if etag := w.Header().Get("ETag"); etag != `W/"12-1XIqlzCgkZ5qAye222Y2URlsyEU"` {
		t.Fatalf("etag: %q", etag)
	}
	if csp := w.Header().Get("Content-Security-Policy"); csp != CSPDefaultPolicy {
		t.Fatalf("csp: %q", csp)
	}
	if xpb := w.Header().Get("X-Powered-By"); xpb != "Express" {
		t.Fatalf("xpb: %q", xpb)
	}
	if b := w.Body.String(); b != "web is alive (web)" {
		t.Fatalf("body: %q", b)
	}
	if cl := w.Header().Get("Content-Length"); cl != "18" {
		t.Fatalf("cl: %q", cl)
	}
}

func testStatusFeature() Feature {
	return Feature{
		Name: "status",
		Routes: []Route{
			{Method: "GET", Path: "/status", NoLogin: true, Handler: func(c *Cxt, r *Res) {
				// The real status feature sets this (Node /status responses
				// carry xpb; pinned P0 + re-pinned P3.2).
				r.W.Header().Set("X-Powered-By", "Express")
				r.PlainText(200, "web is alive (web)")
			}},
		},
	}
}

func TestAppAnonymousUnknownRouteRedirectsToLogin(t *testing.T) {
	app, fr, _ := newTestApp(t, "web", "sec")
	app.RegisterFeature(Feature{
		Name: "authed",
		Routes: []Route{
			{Method: "GET", Path: "/secret", NoLogin: false, Handler: func(c *Cxt, r *Res) {
				r.PlainText(200, "secret")
			}},
		},
	})
	w := doReq(t, app.Handler(), "GET", "/zzz-nope", map[string]string{"Accept": "*/*"}, "")
	if w.Code != 302 {
		t.Fatalf("want 302 got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Fatalf("location: %q", loc)
	}
	if b := w.Body.String(); b != "Found. Redirecting to /login" {
		t.Fatalf("redirect body: %q", b)
	}
	// Node A (pinned: anonymous /project 302 carries Set-Cookie overleaf.sid
	// — the rolling middleware touches every webRouter session, so even a
	// brand-new one is persisted + cookie-d; /status alone is exempt
	// because it rides publicApiRouter without the session chain). The
	// value is node-`cookie`-module encoded (s%3A...), pinned live.
	sid := w.Header().Get("Set-Cookie")
	if !strings.HasPrefix(sid, "overleaf.sid=s%3A") {
		t.Fatalf("web-router anonymous response must issue the session cookie, got %q", sid)
	}
	// and the doc must be in the shared store
	found := false
	for k, v := range fr.m {
		if strings.Contains(k, "sess:") && strings.Contains(v, "validationToken") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("touched session must be persisted (sess:* doc in redis)")
	}
}

func TestAppCsrf403OnBadToken(t *testing.T) {
	app, _, _ := newTestApp(t, "web", "sec")
	app.RegisterFeature(Feature{
		Name: "login-ish",
		Routes: []Route{
			{Method: "POST", Path: "/some-post", NoLogin: true, Handler: func(c *Cxt, r *Res) {
				r.PlainText(200, "ok")
			}},
		},
	})
	// no token at all → 403 "Forbidden" (EBADCSRFTOKEN parity)
	w := doReq(t, app.Handler(), "POST", "/some-post", map[string]string{"Content-Type": "application/json"}, "{}")
	if w.Code != 403 {
		t.Fatalf("want 403 got %d", w.Code)
	}
	if b := w.Body.String(); b != "Forbidden" {
		t.Fatalf("403 body: %q", b)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("403 ct: %q", ct)
	}
	if w.Header().Get("X-Content-Type-Options") == "nosniff" {
		t.Fatal("sendStatus 403 must NOT carry nosniff (Response.mjs parity)")
	}
}

func TestAppCsrfGoodTokenPasses(t *testing.T) {
	app, fr, _ := newTestApp(t, "web", "sec")
	app.RegisterFeature(Feature{
		Name: "post",
		Routes: []Route{
			{Method: "POST", Path: "/some-post", NoLogin: true, Handler: func(c *Cxt, r *Res) {
				r.PlainText(200, "received:"+c.Sess.CsrfSecret()[:4])
			}},
		},
	})
	// first POST with a bad token: Node's 403 path lazily allocates the
	// secret and ISSUES the session cookie (pinned) — do the same
	w1 := doReq(t, app.Handler(), "POST", "/some-post", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, "_csrf=garbage")
	sidCookie := w1.Header().Get("Set-Cookie")
	if sidCookie == "" {
		t.Fatal("403 csrf path must issue the session cookie (secret allocated)")
	}
	rawSID := strings.TrimPrefix(strings.SplitN(sidCookie, ";", 2)[0], "overleaf.sid=")
	secret := ""
	for k, v := range fr.m {
		if strings.HasSuffix(k, rawSID) || strings.Contains(k, "sess:") {
			var d map[string]any
			if json.Unmarshal([]byte(v), &d) == nil {
				if cs, ok := d["csrfSecret"].(string); ok {
					secret = cs
				}
			}
			break
		}
	}
	if secret == "" {
		t.Fatal("no csrfSecret stored for the anonymous session")
	}
	tok := CsrfToken(secret)
	w2 := doReq(t, app.Handler(), "POST", "/some-post",
		map[string]string{"Cookie": "overleaf.sid=" + rawSID, "Content-Type": "application/x-www-form-urlencoded"}, "_csrf="+urlEscape(tok))
	if w2.Code != 200 {
		t.Fatalf("valid csrf must pass: %d %s", w2.Code, w2.Body.String())
	}
}

func TestAppAPIProfile404IsExpressPage(t *testing.T) {
	app, _, _ := newTestApp(t, "api", "sec")
	app.RegisterFeature(Feature{
		Name: "status",
		Routes: []Route{
			{Method: "GET", Path: "/status", NoLogin: true, Handler: func(c *Cxt, r *Res) {
				r.PlainText(200, "web is alive (api)")
			}},
		},
	})
	w := doReq(t, app.Handler(), "GET", "/zzz-nope", nil, "")
	if w.Code != 404 {
		t.Fatalf("want 404 got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Cannot GET /zzz-nope") || !strings.HasPrefix(body, "<!DOCTYPE html>") {
		t.Fatalf("express 404 page expected, got %q", body)
	}
	// api profile must NOT set a session cookie
	if sc := w.Header().Get("Set-Cookie"); strings.Contains(sc, "overleaf.sid") {
		t.Fatalf("api profile must not issue session cookies, got %q", sc)
	}
}

func urlEscape(s string) string {
	q := neturl.QueryEscape(s)
	return strings.ReplaceAll(q, "%2B", "+")
}

var _ bytes.Buffer
