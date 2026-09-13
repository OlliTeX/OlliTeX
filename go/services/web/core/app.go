package core

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"ollitex/go/pbhttp"
)

// Route is one registered endpoint (P0/P1 = static paths only; path
// parameters arrive with the first feature that has them — the P2 login
// whitelist additions are static paths too).
type Route struct {
	Method  string // "GET", "POST", …
	Path    string
	Handler func(*Cxt, *Res)

	// NoLogin = anonymous access allowed (the requireGlobalLogin
	// whitelist). Default false → the global gate bounces anonymous
	// requests to /login, exactly like webRouter.all('*',
	// AuthenticationController.requireGlobalLogin) at router.mjs:215.
	NoLogin bool

	// NoSession = mounted on Node's publicApiRouter / privateApiRouter
	// (which carry NO session/csrf middleware — pinned: Node /status sets
	// no overleaf.sid cookie while /login does).
	NoSession bool
}

// Feature registers a group of routes (go/services/web/features/<x>).
type Feature struct {
	Name   string
	Routes []Route
}

// Cxt is the per-request context feature handlers receive (the Go
// equivalent of req + the useful session slice).
type Cxt struct {
	Req     *http.Request
	Sess    *Session
	SiteURL string // configured site URL (views' origin + siteUrl)
}

type fnPage func(*Cxt, *Res)

// App is the running Go web service (core P0): config, backend clients,
// session store, and the registered features.
type App struct {
	Cfg   *Config
	Redis *RedisClient
	Store *SessionStore
	// Mongo lazy client (auth/user/site_settings features). Nil until
	// SetMongo is called (cmd/web + tests wire it).
	Mongo *MongoLazy
	feats []Feature

	// View renderers (P0: general/404 + general/500; login in P0-views).
	Render404Web fnPage // web profile unknown-route view (general/404)
	Render500    fnPage // error page (general/500)
}

func (a *App) SetRender404(f fnPage) { a.Render404Web = f }
func (a *App) SetRender500(f fnPage) { a.Render500 = f }
func (a *App) SetMongo(m *MongoLazy) { a.Mongo = m }

// New wires the app (callers: cmd/web + tests).
func New(cfg *Config, rdb *RedisClient) *App {
	return &App{Cfg: cfg, Redis: rdb, Store: NewSessionStore(rdb, cfg)}
}

// RegisterFeature adds routes (order = Node registration order when it
// matters; P0 features are disjoint so order is not observable yet).
func (a *App) RegisterFeature(f Feature) { a.feats = append(a.feats, f) }

// ---- response recorder (express-session can only set the cookie before
// the first byte on the wire) ----

type recWriter struct {
	http.ResponseWriter
	written bool
}

func (w *recWriter) WriteHeader(code int) {
	if !w.written {
		w.ResponseWriter.WriteHeader(code)
		w.written = true
	}
}

func (w *recWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.written = true
	}
	return w.ResponseWriter.Write(b)
}

// Handler implements http.Handler: the observable surface of the
// Server.mjs middleware chain (X-Powered-By + default CSP + static +
// session + csrf + routes + fallbacks), pinned in the P0 gate.
func (a *App) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// express default + app-level headers (X-Powered-By from express,
		// CSP from app.use(csp) — both pinned live on /status).
		w.Header().Set("X-Powered-By", "Express")
		if a.Cfg.ExpoHostname {
			if host, err := os.Hostname(); err == nil {
				w.Header().Set("X-Served-By", host)
			}
		}
		var rw *recWriter
		if existing, ok := w.(*recWriter); ok {
			rw = existing
		} else {
			rw = &recWriter{ResponseWriter: w}
			w = rw
		}
		a.serve(w, r, rw)
	})
}

func (a *App) serve(w http.ResponseWriter, r *http.Request, rw *recWriter) {
	// rendered views override the CSP with the per-view nonce policy at
	// render time; everything else carries the default policy (both
	// profiles — pinned: :3000/status has the same header).
	if a.Cfg.Profile == "web" {
		w.Header().Set("Content-Security-Policy", CSPDefaultPolicy)
	} else {
		w.Header().Set("Content-Security-Policy", CSPDefaultPolicy)
	}

	res := &Res{W: w}
	cxt := &Cxt{Req: r, SiteURL: a.Cfg.SiteURL}

	// static (web profile; nginx usually answers first, but the app must
	// be identical when it sees the request — serveStaticWrapper).
	if a.Cfg.Profile == "web" {
		if a.serveStatic(w, r) {
			return
		}
	}

	// session (web profile only — the api profile mounts NO session
	// middleware; pinned: :3000/status sets no overleaf.sid cookie).
	// Routes with NoSession ride Node's publicApiRouter private
	// (no session/csrf at all).
	sessionless := a.routeNoSession(r)
	if a.Cfg.Profile == "web" && !sessionless {
		sess, err := a.Store.StartAnonymous(r)
		if err != nil {
			a.serve500(cxt, res, err)
			return
		}
		cxt.Sess = sess

		if mustCsrf(r.Method) {
			token := csrfTokenFrom(r)
			if token == "" || !VerifyCsrfToken(sess.CsrfSecret(), token) {
				// Node: the 403 goes out WITH the freshly issued session
				// cookie (secret was allocated during the verify attempt)
				// — headers still open, so save+cookie first.
				a.sessionBeforeHandler(cxt, w, rw)
				res.SendStatus(403)
				return
			}
		}
	}

	// express-session semantics (pinned): the session cookie is attached
	// at writeHead — BEFORE any handler body bytes — and the doc is saved
	// at res.end. Touch (rolling) + persist + cookie first (headers still
	// open), then run the handler; a session mutated DURING the handler
	// (login — P2) gets a second persist + cookie pass afterwards only if
	// headers are still open (redirect-login responses qualify).
	a.sessionBeforeHandler(cxt, w, rw)

	// route dispatch
	for _, f := range a.feats {
		for i := range f.Routes {
			rt := &f.Routes[i]
			if rt.Method != r.Method && r.Method != "HEAD" {
				continue
			}
			if rt.Path == r.URL.Path {
				if rt.NoSession {
					rt.Handler(cxt, res)
					return
				}
				if a.Cfg.Profile == "web" && !rt.NoLogin && !cxt.Sess.IsLoggedIn() {
					a.globalLoginBounce(cxt, res, r)
					a.maybeSaveSession(cxt, w, rw)
					return
				}
				rt.Handler(cxt, res)
				a.maybeSaveSession(cxt, w, rw)
				return
			}
		}
	}

	// fallback
	if a.Cfg.Profile == "web" {
		if !cxt.Sess.IsLoggedIn() {
			a.globalLoginBounce(cxt, res, r)
			a.maybeSaveSession(cxt, w, rw)
			return
		}
		// Node: webRouter.get('*', notFound) — the 404 VIEW is the GET/HEAD
		// catch-all; other methods fall through to the Express 404 page
		// (pinned: anonymous/any-method tail). The view needs the session
		// csrf token, hence it only renders here (post-gate).
		switch r.Method {
		case "GET", "HEAD":
			if a.Render404Web != nil {
				a.Render404Web(cxt, res)
			} else {
				res.SendStatus(404)
			}
		default:
			pbhttp.ExpressNotFound(rw, r)
		}
	} else {
		// api profile tail: express default 404 page (pinned:
		// :3000/zzz-nope → 404 text/html "<pre>Cannot GET /zzz-nope</pre>")
		pbhttp.ExpressNotFound(rw, r)
	}
	a.maybeSaveSession(cxt, w, rw)
}

func (a *App) serve500(cxt *Cxt, res *Res, err error) {
	log.Printf("webgo: 500 %s %s: %v", cxt.Req.Method, cxt.Req.URL.Path, err)
	if a.Render500 != nil {
		a.Render500(cxt, res)
		return
	}
	res.SendStatus(500)
}

// globalLoginBounce implements router.mjs:215 requireGlobalLogin for a
// request that reached a login-required route without a session:
//
//	acceptsJson (XHR/API) → 401 + "WWW-Authenticate: OverleafLogin", empty
//	  text/plain body (pinned anon XHR /restricted → 401, no body)
//	otherwise → 302 /login with the session cookie, and the requested
//	  path stashed as session.postLoginRedirect (Node
//	  setRedirectInSession) so POST /login can send the user back.
func (a *App) globalLoginBounce(cxt *Cxt, res *Res, r *http.Request) {
	if cxt.Sess != nil {
		safe := r.URL.Path
		if r.URL.RawQuery != "" {
			safe = safe + "?" + r.URL.RawQuery
		}
		if !isStaticRedirectPath(safe) {
			cxt.Sess.Set("postLoginRedirect", safe)
		}
	}
	if a.acceptsJSON(r) {
		// pinned: 401 + WWW-Authenticate: OverleafLogin, body "Unauthorized"
		res.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
		res.W.Header().Set("WWW-Authenticate", "OverleafLogin")
		res.W.WriteHeader(401)
		_, _ = res.W.Write([]byte("Unauthorized"))
		return
	}
	res.Redirect(r, 302, loginRedirectTarget(r))
}

// isStaticRedirectPath mirrors the static-asset guard in
// setRedirectInSession (never stash asset paths).
func isStaticRedirectPath(v string) bool {
	for _, p := range []string{"/socket.io/", "/js/", "/stylesheets/", "/img/"} {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	end := []string{".png", ".jpeg", ".svg"}
	lp := strings.ToLower(v)
	for _, e := range end {
		if strings.HasSuffix(lp, e) {
			return true
		}
	}
	return false
}

// AcceptsJSON is the exported gate/feature helper (see acceptsJSON).
func AcceptsJSON(r *http.Request) bool { return new(App).acceptsJSON(r) }

// acceptsJSON mirrors RequestContentTypeDetection.acceptsJson
// (req.accepts(['html','json']) === 'json'): html wins on conflict, so a
// plain browser ("*/*" or "text/html") is NOT json.
func (a *App) acceptsJSON(r *http.Request) bool {
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		return true
	}
	accept := strings.ToLower(r.Header.Get("Accept"))
	hasHTML := strings.Contains(accept, "text/html") || strings.Contains(accept, "application/xhtml")
	hasJSON := strings.Contains(accept, "application/json") || strings.Contains(accept, "application/*")
	return hasJSON && !hasHTML
}

// ---- csrf plumbing ----

func mustCsrf(method string) bool {
	switch method {
	case "GET", "HEAD", "OPTIONS":
		return false
	}
	return true
}

// csrfTokenFrom mirrors csurf defaultValue (Overleaf-patched source,
// .yarn/patches/csurf-npm-1.11.0-c1b9cbb35b.patch):
//
//	body._csrf || query._csrf || csrf-token || xsrf-token ||
//	x-csrf-token || x-xsrf-token
func csrfTokenFrom(r *http.Request) string {
	if v := r.FormValue("_csrf"); v != "" {
		return v
	}
	if v := r.URL.Query().Get("_csrf"); v != "" {
		return v
	}
	for _, h := range []string{"csrf-token", "xsrf-token", "x-csrf-token", "x-xsrf-token"} {
		if v := r.Header.Get(h); v != "" {
			return v
		}
	}
	return ""
}

// sessionBeforeHandler mirrors express-session's writeHead-time cookie
// attachment, run while headers are still open.
//
// Node's observable rules (pinned): a session is saved + cookie-d only
// when it was MODIFIED this request (saveUninitialized:false; rolling
// touch updates expires on save). A brand-new session that nothing
// touched (e.g. a plain page view) is NOT persisted and NO cookie is
// issued — Node only sets the overleaf.sid cookie from the moment the
// session becomes real (login, csrf use, …).
func (a *App) sessionBeforeHandler(cxt *Cxt, w http.ResponseWriter, rw *recWriter) {
	sess := cxt.Sess
	if sess == nil || sess.SessID == "" || a.Cfg.Profile != "web" {
		return
	}
	if a.Cfg.RollingSession {
		sess.touch()
	}
	if !sess.changed {
		return // unmodified → express-session skips save+cookie
	}
	if err := a.Store.persist(sess); err != nil {
		log.Printf("webgo: session persist: %v", err)
	}
	if !rw.written {
		sess.writeSessionCookie(w, a.Cfg)
	}
}

// maybeSaveSession handles sessions mutated DURING the handler (login /
// P2 flows): persist immediately; the cookie is settable only before the
// first byte (a redirect login response still qualifies).
func (a *App) maybeSaveSession(cxt *Cxt, w http.ResponseWriter, rw *recWriter) {
	sess := cxt.Sess
	if sess == nil || sess.SessID == "" || !sess.changed {
		return
	}
	if err := a.Store.persist(sess); err != nil {
		log.Printf("webgo: session persist: %v", err)
	}
	if !rw.written {
		sess.writeSessionCookie(w, a.Cfg)
	}
}

// CommitSess persists sess and issues its Set-Cookie NOW — before the
// handler writes any body — which is required for session
// REGENERATION (login): the NEW sid must ride the very response that
// completes the login (P2 pin: sid regenerated, old doc destroyed).
func (a *App) CommitSess(sess *Session, w http.ResponseWriter) {
	if sess == nil || sess.SessID == "" {
		return
	}
	if err := a.Store.persist(sess); err != nil {
		log.Printf("webgo: session persist: %v", err)
	}
	sess.writeSessionCookie(w, a.Cfg)
}

// routeNoSession reports whether the requested path is a NoSession route
// (publicApiRouter/privateApiRouter parity).
func (a *App) routeNoSession(r *http.Request) bool {
	for _, f := range a.feats {
		for i := range f.Routes {
			rt := &f.Routes[i]
			if rt.Path == r.URL.Path && rt.Method == r.Method {
				return rt.NoSession
			}
		}
	}
	return false
}

// ---- static serving (serveStaticWrapper parity, minimal) ----

func (a *App) serveStatic(w http.ResponseWriter, r *http.Request) bool {
	if a.Cfg.PublicDir == "" {
		return false
	}
	p := filepath.Join(a.Cfg.PublicDir, filepath.Clean("/"+r.URL.Path))
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	if a.Cfg.CacheStaticAssets {
		w.Header().Set("Cache-Control", "public, max-age=31536000")
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
	return true
}

// loginRedirectTarget preserves the query string (express redirect +
// getQueryString parity: '/login' + original '?a=b').
// loginRedirectTarget — Node's gate always 302s to clean "/login" (the
// pre-login target is stashed in session.postLoginRedirect, not the URL).
func loginRedirectTarget(r *http.Request) string {
	return "/login"
}
