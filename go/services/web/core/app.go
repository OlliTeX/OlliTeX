package core

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

	// APIOnly = mounted on Node's privateApiRouter / publicApiRouter but NOT
	// on webRouter (e.g. GET /project/:id/details, POST /user/:id/project/new,
	// POST /tpds/folder-update). Served on the api profile; the web profile
	// SKIPS it (falls through to the web 404 tail — Node's web stack 404s these
	// since they are not wired on webRouter). This is the "api-only" cell of the
	// web/both/api-only matrix;
	// NoSession (doc-trio, /status, health_check) = served on BOTH (dual).
	APIOnly bool

	// Pattern — Express-style regex route (P2a: /:token token access +
	// consent routes). First/named capture group = route param (Cxt.Params
	// ["token" / "1"]).
	Pattern *regexp.Regexp
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
	A       *App              // the running app (DB ladder access for page data)
	SiteURL string            // configured site URL (views' origin + siteUrl)
	Params  map[string]string // route params (P2a pattern routes)
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
		// x-powered-by: Node sends it ONLY on the express res.send() paths
		// that stay enabled — pinned live 2026-09-14 (P3.2): present on
		// /status 200,csrf 403 (res.sendStatus) and the body-parser 400;
		// ABSENT on page renders, res.json() API responses, redirects, the
		// 401 login gate and 404s. Added per-route (status.go) and at the
		// csrf-403 + BareWrite sites, NOT globally.
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
	cxt := &Cxt{Req: r, A: a, SiteURL: a.Cfg.SiteURL}

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

		// express.json (body-parser) parity — Node Server.mjs registers
		// express.json BEFORE the csrf middleware. Strict mode (default)
		// accepts ONLY object/array JSON roots; a scalar root
		// (number/string/boolean/null/unparseable) → 400 with exactly
		// body "{}" + application/json + weak ETag — and, pinned P3.3,
		// that happens BEFORE the csrf 403 even for anonymous requests.
		// Array roots pass the parser (→ csrf 403 / handler 400).
		if ct := strings.ToLower(r.Header.Get("Content-Type")); strings.Contains(ct, "json") &&
			(r.Method == http.MethodPost || r.Method == http.MethodPut ||
				r.Method == http.MethodPatch || r.Method == http.MethodDelete) {
			if r.ContentLength > 0 || r.ContentLength < 0 {
				raw, rerr := io.ReadAll(http.MaxBytesReader(w, r.Body, 12*1024*1024))
				if rerr != nil {
					// P6.17: Node bodyParser.json({limit: max_json_request_size})
					// — 12 MiB default (services/web settings.defaults). A body
					// over the limit → entity.too.large → express error → 413,
					// content-negotiated exactly like the 400 bad-JSON (JSON
					// accept -> "{}"; else the 705-byte page), BEFORE the csrf
					// 403. (Pinned live P6.17: 12 MB+1 body → Node 413 {}.)
					badBody413(a, r, res)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(raw))
				r.ContentLength = int64(len(raw))
				trimmed := bytes.TrimSpace(raw)
				if len(trimmed) > 0 && trimmed[0] != '{' && trimmed[0] != '[' {
					// Scalar-root / unparseable body (P3.3, refined P6.4a):
					// Node 400 with a CONTENT-NEGOTIATED body — JSON accept
					// -> "{}"; html accept (or no Accept header, "*/*") ->
					// the 705B error page (pinned: POST notjson with no
					// Accept -> 400 page; accept: application/json -> 400 {};
					// scalar 123/"str"/true follow the same negotiation).
					badBody400(a, r, res)
					return
				}
				if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
					// P6.14: express.json PARSES the whole root — an
					// object/array-shaped body that is NOT valid JSON
					// ({bad / [1,) also 400s BEFORE the csrf 403 (pinned
					// live on the Node leg: POST {bad without csrf -> 400 {},
					// not 403). Valid object/array roots pass here.
					var probe any
					if perr := json.Unmarshal(trimmed, &probe); perr != nil {
						badBody400(a, r, res)
						return
					}
				}
			}
		}

		if mustCsrf(r.Method) {
			token := csrfTokenFrom(r)
			if token == "" || !VerifyCsrfToken(sess.CsrfSecret(), token) {
				// Node: the 403 goes out WITH the freshly issued session
				// cookie (secret was allocated during the verify attempt)
				// — headers still open, so save+cookie first.
				//
				// Order note (Server.mjs): csrf middleware (line ~233) is
				// registered BEFORE helmet (line ~322), so the csrf 403
				// response carries NONE of the web-baseline headers (pinned
				// P0: no nosniff; P3.1: no helmet set on the 403). Node sends
				// it via res.sendStatus → X-Powered-By: Express present (pinned
				// P3.2 on DELETE /status 403).
				a.sessionBeforeHandler(cxt, w, rw)
				res.W.Header().Set("X-Powered-By", "Express")
				res.SendStatus(403)
				return
			}
		}

		// Server.mjs:322-360 — helmet + conditional no-cache at request
		// time for everything AFTER the csrf check (gate bounces, routes,
		// 404 views, rendered 500 — pinned P3.1 on all of them; the csrf
		// 403 above is excluded by the ordering). Not applied to the
		// NoSession publicApi responses (e.g. /status — no baseline there).
		a.setWebBaseline(w, r, sess.IsLoggedIn())
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
			if rt.Path == r.URL.Path || (rt.Pattern != nil && rt.Pattern.MatchString(r.URL.Path)) {
				// api profile (ENABLED_SERVICES=api, Node's :3000): serve ONLY
				// the privateApiRouter + publicApiRouter routes. In Go those are
				// exactly the NoSession routes (no session/csrf middleware — Node's
				// api profile mounts neither webRouter nor its session stack).
				// Web-router routes (NoSession=false) are 404 here, exactly like
				// Node :3000, which does not mount webRouter:
				//   pinned: /project, /members, /entities, / → 404 + XPB on :3000
				//           (but 200/302 on :4000); only /status,/health_check*,
				//           doc-trio, snapshots, ... (NoSession) are 200/401/404.
				// The web profile (:4000) is untouched — it serves all routes.
				if a.Cfg.Profile == "api" && !rt.NoSession && !rt.APIOnly {
					continue // web-router route: skip → api 404 tail, like Node :3000
				}
				if a.Cfg.Profile == "web" && rt.APIOnly {
					continue // api-only route: Node's web stack does not mount it —
					// skip → web 404 tail (unchanged, verified) instead of serving.
				}
				if rt.NoSession {
					if rt.Pattern != nil {
						if m := rt.Pattern.FindStringSubmatch(r.URL.Path); m != nil {
							cxt.Params = map[string]string{}
							names := rt.Pattern.SubexpNames()
							for i := 1; i < len(m) && i < len(names); i++ {
								k := names[i]
								if k == "" {
									k = strconv.Itoa(i)
								}
								cxt.Params[k] = m[i]
							}
						}
					}
					rt.Handler(cxt, res)
					return
				}
				if a.Cfg.Profile == "web" && !a.Cfg.AllowPublicAccess && !rt.NoLogin && !a.webAuthed(cxt, r) {
					a.globalLoginBounce(cxt, res, r)
					a.maybeSaveSession(cxt, w, rw)
					return
				}
				if rt.Pattern != nil {
					if m := rt.Pattern.FindStringSubmatch(r.URL.Path); m != nil {
						cxt.Params = map[string]string{}
						names := rt.Pattern.SubexpNames()
						for i := 1; i < len(m) && i < len(names); i++ {
							k := names[i]
							if k == "" {
								k = strconv.Itoa(i)
							}
							cxt.Params[k] = m[i]
						}
					}
				}
				rt.Handler(cxt, res)
				a.maybeSaveSession(cxt, w, rw)
				return
			}
		}
	}

	// fallback
	if a.Cfg.Profile == "web" {
		// Node requireGlobalLogin: the NoLogin/NoSession whitelist applies to
		// EVERY method — anonymous OPTIONS /status → 200 Allow, anonymous
		// OPTIONS /zzz-nope → 302 (pinned P6.18).
		gated := !a.Cfg.AllowPublicAccess && !a.webAuthed(cxt, r)
		if gated && a.pathHasNoLogin(r.URL.Path) {
			gated = false
		}
		if gated {
			a.globalLoginBounce(cxt, res, r)
			a.maybeSaveSession(cxt, w, rw)
			return
		}
		// express Router auto-OPTIONS (pinned P6.18): once past the gate,
		// an OPTIONS request with no method route gets 200 +
		// Allow:"GET,HEAD[,<route methods>]" (registration order, GET,HEAD
		// ALWAYS first — even on POST-only paths like /api/format-tex →
		// "GET,HEAD,POST"; unknown paths → bare "GET,HEAD") + the SAME
		// string as body + text/html + weak sha1 etag + NO X-Powered-By.
		if r.Method == "OPTIONS" {
			a.serveOptionsAuto(res, r)
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

// pathHasNoLogin reports whether at least one route registered on the
// exact path carries the NoLogin/NoSession marker (Node's
// requireGlobalLogin whitelist — method-independent, pinned P6.18).
func (a *App) pathHasNoLogin(p string) bool {
	for _, f := range a.feats {
		for i := range f.Routes {
			if f.Routes[i].Path == p && (f.Routes[i].NoLogin || f.Routes[i].NoSession) {
				return true
			}
		}
	}
	return false
}

// serveOptionsAuto — express Router auto-OPTIONS response (pinned P6.18):
//
//	200, Allow: "GET,HEAD" + the path's non-GET/HEAD route methods in
//	registration order (e.g. /api/format-tex → "GET,HEAD,POST"; unknown
//	path → "GET,HEAD"), body = the same string, text/html; charset=utf-8,
//	weak sha1 etag (the `etag` package: sha1 digest base64), NO
//	X-Powered-By (res.send-style path, not res.sendStatus).
func (a *App) serveOptionsAuto(res *Res, r *http.Request) {
	ms := []string{"GET", "HEAD"}
	seen := map[string]bool{"GET": true, "HEAD": true}
	for _, f := range a.feats {
		for i := range f.Routes {
			if f.Routes[i].Path != r.URL.Path {
				continue
			}
			m := f.Routes[i].Method
			if !seen[m] {
				seen[m] = true
				ms = append(ms, m)
			}
		}
	}
	allow := strings.Join(ms, ",")
	sum := sha1.Sum([]byte(allow))
	// express `etag` package: W/"<hex-len>-<sha1 base64 NO padding>" —
	// pinned P6.18 (13 → W/"d-…", 8 → W/"8-…", no trailing '=').
	etagBody := base64.StdEncoding.EncodeToString(sum[:])
	for strings.HasSuffix(etagBody, "=") {
		etagBody = strings.TrimSuffix(etagBody, "=")
	}
	// Node attaches X-Powered-By: Express on the auto-OPTIONS response for
	// NoSession (publicApi-style) paths (/status, /health_check/*) but not
	// on sessionful ones (/login, /api/format-tex) — pinned P6.18.
	public := false
	for _, f := range a.feats {
		for i := range f.Routes {
			if f.Routes[i].Path == r.URL.Path && f.Routes[i].NoSession {
				public = true
			}
		}
	}
	h := res.W.Header()
	if public {
		h.Set("X-Powered-By", "Express")
	}
	h.Set("Allow", allow)
	h.Set("ETag", fmt.Sprintf(`W/"%x-%s"`, len(allow), etagBody))
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(allow)))
	res.W.WriteHeader(200)
	_, _ = res.W.Write([]byte(allow))
}

func (a *App) serve500(cxt *Cxt, res *Res, err error) {
	log.Printf("webgo: 500 %s %s: %v", cxt.Req.Method, cxt.Req.URL.Path, err)
	if a.Render500 != nil {
		a.Render500(cxt, res)
		return
	}
	res.SendStatus(500)
}

// webAuthed — the auth decision of Node's requireGlobalLogin for the web
// profile (mirror the Node ordering in AuthenticationController.
// requireGlobalLogin). When an Authorization header is PRESENT the
// basic-credential decision is authoritative (valid → authenticated, invalid →
// unauthenticated — the session is ignored, exactly like Node). When absent
// the session decides. This is what lets a *valid* private-API basic cred
// authenticate a web-profile request (→ dispatched to the route / web 404
// view) instead of being bounced to 401, matching Node; an invalid cred still
// 401s.
func (a *App) webAuthed(cxt *Cxt, r *http.Request) bool {
	if r.Header.Get("Authorization") != "" {
		return a.basicAuthValid(r)
	}
	return cxt.Sess != nil && cxt.Sess.IsLoggedIn()
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
	if a.acceptsJSON(r) || r.Header.Get("Authorization") != "" {
		// pinned: 401 + WWW-Authenticate: OverleafLogin, body "Unauthorized",
		// text/plain; charset=utf-8 + weak ETag + Content-Length (express
		// res.sendStatus — pinned P3.2); NO x-powered-by (Node: absent).
		res.W.Header().Set("WWW-Authenticate", "OverleafLogin")
		res.SendStatus(401)
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
	// Regeneration: Node's login response carries EXACTLY ONE Set-Cookie
	// (the new sid). The pre-handler pass may have queued the old sid's
	// cookie into the (still unsent) headers — revoke it, then issue the
	// new one (P3.3 pin: two cookies leaked, first = destroyed old sid).
	w.Header().Del("Set-Cookie")
	sess.writeSessionCookie(w, a.Cfg)
}

// routeNoSession reports whether the requested path is a NoSession route
// (publicApiRouter/privateApiRouter parity).
func (a *App) routeNoSession(r *http.Request) bool {
	web := a.Cfg.Profile == "web"
	for _, f := range a.feats {
		for i := range f.Routes {
			rt := &f.Routes[i]
			// APIOnly routes are ABSENT from Node's web profile (Route.APIOnly):
			// they must not mark the path "sessionless" there — doing so skips
			// session init, leaves cxt.Sess nil, and the web fallback /
			// login-gate derefs it → nil-pointer panic (empty reply; pinned
			// 2026-09-23: web /project/:id/details + /user/:id/personal_info
			// crashed before this). On the api profile they ARE mounted
			// (NoSession, basic-auth) and count as before.
			if web && rt.APIOnly {
				continue
			}
			if (rt.Path == r.URL.Path && rt.Method == r.Method) ||
				(rt.Pattern != nil && rt.Pattern.MatchString(r.URL.Path) && rt.Method == r.Method) {
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
