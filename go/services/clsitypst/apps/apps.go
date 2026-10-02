// Package apps ports the clsi_typst Express app (services/clsi_typst/app.js)
// as the Go server layer: the route table, the COMPILE_TYPEST_ENABLED
// feature gate (501), the app-level error middleware, /status,
// /health_check, /smoke_test_force, the build-scoped output.zip routes
// (frozen clsi/outputcontroller), and the load-balancer agent
// (see loadagent.go).
//
// Handler contract (mirrors the compilecontroller (int, error) seam,
// clsi.go convention):
//
//	code == 0, err != nil  => Node next(error): render the error
//	                          middleware response (finish).
//	code != 0, err == nil  => the handler already wrote its own response.
//	code != 0, err != nil  => the handler wrote a body and then failed
//	                          (a write error): the body is on its way
//	                          anyway; log only (documented divergence:
//	                          Node re-fires next(error) on the doomed
//	                          socket).
//
// Error middleware (LOCKED, clsi.go HANDOFF §3.2 — Node arm ordering +
// LOCKED body text):
//
//	NotFoundError        -> 404 (empty, Node res.sendStatus(404))
//	InvalidParameter     -> 400 `Bad request: (<message>)`
//	EPIPE (message-tagged, no Go typed EPIPE) -> 503 (empty)
//	else                 -> 500 `CLSI server error (internal error: <message>)`
//
// The feature gate (D14) wraps ALL main-mux routes INCLUDING /status and
// /health_check (Node app.js: middleware before routing). It does NOT
// apply to the load agent (a separate listener, D13).
//
// Documented divergences vs Node app.js:
//   - Timeout 630s: Node req/res.setTimeout is a socket layer concern;
//     Go sets the Server layer timeout in T11 (cmd), not per-request.
//   - X-Powered-By stripping: Node sets the header then strips it; Go
//     never sends it.
//   - clsiCacheShard: D15 envelope, always omitted (Node typst: undefined).
package apps

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	clserrors "ollitex/go/services/clsitypst/errors"
	clsl "ollitex/go/services/clsitypst/logger"
	otc "ollitex/go/services/clsitypst/outputcontroller"

	compilecontroller "ollitex/go/services/clsitypst/compilecontroller"
	compilemanager "ollitex/go/services/clsitypst/compilemanager"
	cltypstcfg "ollitex/go/services/clsitypst/config"
	projectpersistence "ollitex/go/services/clsitypst/projectpersistence"
	smoketest "ollitex/go/services/clsitypst/smoketest"
)

// App is the Go equivalent of the Express app (app.js default export).
type App struct {
	// Config is the typst config (feature gate flag, CompileSizeLimit,
	// Path.OutputDir, SmokeTest).
	Config *cltypstcfg.Config

	// CompileCtrl is the compile controller (Node: the CompileController
	// module import).
	CompileCtrl *compilecontroller.Controller

	// SmokeTest runs the live compile smoke (D12 lean-Y, gated on
	// Config.SmokeTest). Nil when smoke is off in deployment.
	SmokeTest *smoketest.SmokeTest

	// ProcessTooOld (D10 parity lean-Y, decision: FUNC SEAM, NOT *bool)
	// default `func() bool { return false }` (OK arm) so New() has no timer;
	// T11 wires the real PROCESS_LIFESPAN_LIMIT_MS timer/jitter func here
	// (reference apps error.go startLifespanGuard is DESIGN-ONLY mirror
	// source, different checkout).
	ProcessTooOld func() bool

	// --- seams (production defaults wired in New; tests override) -----
	diskLow      func() bool // projectpersistence.IsAnyDiskLow
	diskCritical func() bool // projectpersistence.IsAnyDiskCriticalLow
}

// New builds the App with the production seams (Node: the module imports of
// CompileController / ProjectPersistenceManager / SmokeTests — the last
// two live at the cmd layer, passed here).
func New(cfg *cltypstcfg.Config, cc *compilecontroller.Controller,
	smoke *smoketest.SmokeTest) *App {
	return &App{
		Config:        cfg,
		CompileCtrl:   cc,
		SmokeTest:     smoke,
		ProcessTooOld: func() bool { return false },
		diskLow:       projectpersistence.IsAnyDiskLow,
		diskCritical:  projectpersistence.IsAnyDiskCriticalLow,
	}
}

// Handler builds the main HTTP handler: feature gate (D14, wraps every
// route incl. /status + /health_check) then the 17-route mux.
func (a *App) Handler() http.Handler {
	return a.featureGate(a.routeMux())
}

// featureGate ports the Node feature gate middleware
// (app.js: app.use before the routes):
// COMPILE_TYPEST_ENABLED=false -> 501 { error: 'typst compilation not enabled' }.
func (a *App) featureGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Config.CompileTypstEnabled {
			a.finishJSON(w, http.StatusNotImplemented,
				map[string]any{"error": "typst compilation not enabled"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// routeMux is the route table (app.js L71-124 1:1, NO sync/convert per D3;
// Go 1.22 mux 2-arg handlers + r.PathValue params).
func (a *App) routeMux() http.Handler {
	mux := http.NewServeMux()

	// Compile (project-scoped).
	mux.HandleFunc("POST /project/{project_id}/compile", a.compile)
	mux.HandleFunc("POST /project/{project_id}/compile/stop", a.stopCompile)
	mux.HandleFunc("DELETE /project/{project_id}", a.clearCache)
	mux.HandleFunc("GET /project/{project_id}/wordcount", a.wordcount)
	mux.HandleFunc("POST /project/{project_id}/wordcount", a.wordcount)
	mux.HandleFunc("GET /project/{project_id}/status", a.status)
	mux.HandleFunc("POST /project/{project_id}/status", a.status)

	// Compile (per-user containers).
	mux.HandleFunc("POST /project/{project_id}/user/{user_id}/compile", a.compile)
	mux.HandleFunc("POST /project/{project_id}/user/{user_id}/compile/stop", a.stopCompile)
	mux.HandleFunc("DELETE /project/{project_id}/user/{user_id}", a.clearCache)
	mux.HandleFunc("GET /project/{project_id}/user/{user_id}/wordcount", a.wordcount)
	mux.HandleFunc("POST /project/{project_id}/user/{user_id}/wordcount", a.wordcount)

	// output.zip (Node: "needs to be before output/*" — Go mux patterns are
	// static, ordering irrelevant).
	mux.HandleFunc("GET /project/{project_id}/build/{build_id}/output/output.zip", a.outputZip)
	mux.HandleFunc("GET /project/{project_id}/user/{user_id}/build/{build_id}/output/output.zip", a.outputZip)

	// D21 sync routes (T16): the clsi synctex surface (clsi parity, the forked
	// sidecar stands in for output.synctex.gz).
	mux.HandleFunc("GET /project/{project_id}/sync/code", a.syncCode)
	mux.HandleFunc("GET /project/{project_id}/sync/pdf", a.syncPdf)
	mux.HandleFunc("GET /project/{project_id}/user/{user_id}/sync/code", a.syncCode)
	mux.HandleFunc("GET /project/{project_id}/user/{user_id}/sync/pdf", a.syncPdf)

	// Live status.
	mux.HandleFunc("GET /status", a.aliveStatus)
	mux.HandleFunc("GET /health_check", a.healthCheck)
	mux.HandleFunc("GET /smoke_test_force", a.smokeTestForce)

	return mux
}

// --- Routes ------------------------------------------------------------

// compile is POST .../compile (both variants). Node: express.json({limit}).
func (a *App) compile(w http.ResponseWriter, r *http.Request) {
	body, ok := a.readJSON(w, r)
	if !ok {
		return
	}
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CompileCtrl.Compile(w, a.projectUser(r), body)
	})
}

// stopCompile is POST .../compile/stop (both variants).
func (a *App) stopCompile(w http.ResponseWriter, r *http.Request) {
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CompileCtrl.StopCompile(w, a.projectUser(r))
	})
}

// clearCache is DELETE /project/{project_id}( /user/{user_id}).
func (a *App) clearCache(w http.ResponseWriter, r *http.Request) {
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CompileCtrl.ClearCache(w, a.projectUser(r))
	})
}

// wordcount is GET and POST .../wordcount (both variants) — the node typst
// unifies them (unlike clsi tex WordcountWithSync, D2). file/image are the
// query params (Node: zz.filepath().default('main.typ')).
func (a *App) wordcount(w http.ResponseWriter, r *http.Request) {
	a.handle(w, r, func(w2 http.ResponseWriter) (int, error) {
		file, image := r.URL.Query().Get("file"), r.URL.Query().Get("image")
		var body map[string]any
		if r.Method == http.MethodPost {
			b, ok := a.readJSON(w2, r)
			if !ok {
				return 0, nil
			}
			body = b
		}
		return a.CompileCtrl.Wordcount(w2, a.projectUser(r), file, image, body)
	})
}

// status is GET/POST /project/{project_id}/status (Node controller status).
func (a *App) status(w http.ResponseWriter, r *http.Request) {
	a.handle(w, r, func(w2 http.ResponseWriter) (int, error) {
		return a.CompileCtrl.Status(w2)
	})
}

// aliveStatus is GET /status (Node: res.send('clsi_typst is alive\n')).
func (a *App) aliveStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := io.WriteString(w, "clsi_typst is alive\n"); err != nil {
		clsl.Debug(map[string]any{"path": r.URL.Path}, "status write error: "+err.Error())
	}
}

// healthCheck is GET /health_check (LOCKED order):
//  1. ProcessTooOld (D10 parity) -> 500 { "processTooOld": true }
//  2. PPM disk-critical          -> 500 { "diskCritical": true }
//  3. smoke enabled (D12)        -> SmokeTest.SendLastResult (200 "OK" / 500 <err>)
//  4. else                       -> 200 { "ok": true }
func (a *App) healthCheck(w http.ResponseWriter, r *http.Request) {
	if a.ProcessTooOld() {
		a.finishJSON(w, http.StatusInternalServerError, map[string]any{"processTooOld": true})
		return
	}
	if a.diskCritical() {
		a.finishJSON(w, http.StatusInternalServerError, map[string]any{"diskCritical": true})
		return
	}
	if a.Config.SmokeTest && a.SmokeTest != nil {
		a.SmokeTest.SendLastResult(w)
		return
	}
	a.finishJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// smokeTestForce is GET /smoke_test_force (D12 lean-Y, NO Node-typst
// analogue — the clsi.go shape). Node: sendNewResult(res).catch(next) —
// the method itself renders 200 'OK' or 500 <message>. Smoke nil (not
// configured) -> (0, OError) -> finish 500 LOCKED text (decision recorded
// at write time).
func (a *App) smokeTestForce(w http.ResponseWriter, r *http.Request) {
	if a.SmokeTest == nil {
		a.finish(w, r, 0, clserrors.NewOError("smoke test not enabled"))
		return
	}
	a.SmokeTest.SendNewResult(w)
}

// syncCode is GET .../sync/code (both project + per-user mounts).
func (a *App) syncCode(w http.ResponseWriter, r *http.Request) {
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CompileCtrl.SyncFromCode(w, a.projectUser(r), parseSyncQuery(r.URL.Query()))
	})
}

// syncPdf is GET .../sync/pdf (both mounts).
func (a *App) syncPdf(w http.ResponseWriter, r *http.Request) {
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CompileCtrl.SyncFromPdf(w, a.projectUser(r), parseSyncQuery(r.URL.Query()))
	})
}

// outputZip is GET .../output/output.zip (both variants), wired to the
// FROZEN clsi/outputcontroller (D22: reuse, not adapt):
// (200, nil)   -> the frozen func already wrote headers + zip: no-op.
// (404, nil)   -> render 404 (nothing written yet).
// (500, err)   -> finish (LOCKED else-arm 500 text).
// Default ofa.Find=nil -> (500, "Find not initialised") until cmd (T11)
// wires the production walk (the §3.4 reader/writer fork lever).
func (a *App) outputZip(w http.ResponseWriter, r *http.Request) {
	req := otc.Request{
		ProjectID: r.PathValue("project_id"),
		UserID:    r.PathValue("user_id"),
		BuildID:   r.PathValue("build_id"),
	}
	a.handle(w, r, func(_ http.ResponseWriter) (int, error) {
		code, err := otc.CreateOutputZip(w, a.Config.Path.OutputDir, req)
		if err != nil {
			// (500, err): the frozen func wrote nothing on the error arm;
			// normalise to (0, err) so finish renders the LOCKED body.
			return 0, err
		}
		if code != http.StatusOK {
			// (404, nil): content dir / output files missing.
			w.WriteHeader(http.StatusNotFound)
		}
		return code, nil
	})
}

// --- Helpers -----------------------------------------------------------

// projectUser extracts the (project_id, user_id) pair from the path —
// user_id is "" on the project-scoped mounts (no user segment).
func (a *App) projectUser(r *http.Request) compilecontroller.ProjectUser {
	return compilecontroller.ProjectUser{
		ProjectID: r.PathValue("project_id"),
		UserID:    r.PathValue("user_id"),
	}
}

// readJSON ports express.json({ limit: Settings.compileSizeLimit }):
//
//	over-limit body  -> 413 (Node express ByteLimitError path, rendered
//	                        directly; ok=false).
//	invalid / JSON-not-an-object body -> (0, InvalidParameter) via finish
//	                        -> 400 `Bad request: (<message>)` (ok=false).
//
// An empty body forwards an empty map: the controller's RequestParser
// rejects the missing 'compile' attribute (Node parity — the forwarded
// parse error surfaces through finish).
func (a *App) readJSON(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	limit := compileSizeBytes(a.Config.CompileSizeLimit)
	var data []byte
	var err error
	if limit > 0 {
		// limit+1: detect oversize without buffering the whole body.
		data, err = io.ReadAll(io.LimitReader(r.Body, limit+1))
		if err == nil && int64(len(data)) > limit {
			// Node express.json 413: res.status(413).
			clsl.Warn(map[string]any{"path": r.URL.Path, "size": len(data), "limit": limit},
				"compile request body over limit")
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return nil, false
		}
	} else {
		// Limit 0 (misconfigured / empty token, Node parseInt-fail) => FAIL
		// CLOSED: any non-empty body is rejected with 413 (a misconfigured
		// limit must not silently accept unbounded bodies). Deploy always
		// sets the "7mb" default — this arm is the misconfiguration guard.
		data, err = io.ReadAll(r.Body)
		if err == nil && int64(len(data)) > 0 {
			clsl.Warn(map[string]any{"path": r.URL.Path, "size": len(data)},
				"compile request body has no configured limit")
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return nil, false
		}
	}
	if err != nil {
		a.finish(w, r, 0, &clserrors.InvalidParameter{Message: fmt.Sprintf("read body: %v", err)})
		return nil, false
	}
	if len(data) == 0 {
		// Empty body -> empty map (the controller parse rejects it).
		return map[string]any{}, true
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		a.finish(w, r, 0, &clserrors.InvalidParameter{
			Message: fmt.Sprintf("invalid compile request body: %v", err)})
		return nil, false
	}
	return body, true
}

// parseSyncQuery (clsi.go apps convention) ports the synctex query object:
// file/line/column (sync/code), page/h/v (sync/pdf) + the accepted-but-ignored
// imageName/editorId/buildId/compileFromClsiCache routing fields. Numeric
// query fields go through coerceInt (""->0; clsi coerceInt mirrors).
func parseSyncQuery(v url.Values) compilecontroller.SyncQuery {
	var sq compilecontroller.SyncQuery
	sq.File = v.Get("file")
	sq.Line = coerceInt(v.Get("line"))
	sq.Column = coerceInt(v.Get("column"))
	sq.Page = coerceInt(v.Get("page"))
	sq.H = coerceInt(v.Get("h"))
	sq.V = coerceInt(v.Get("v"))
	sq.ImageName = v.Get("imageName")
	sq.EditorID = v.Get("editorId")
	sq.BuildID = v.Get("buildId")
	sq.CompileFromClsiCache = v.Get("compileFromClsiCache") == "true"
	return sq
}

// coerceInt ports the clsi.go apps coerceInt: strconv.Atoi ("" -> 0).
func coerceInt(raw string) int {
	n, _ := strconv.Atoi(raw)
	return n
}

// compileSizeBytes ports Node `bytes.parse` for the settings.compileSizeLimit
// tokens (default '7mb'): b/kb/k/mb/m/gb/g (frozen reference convention).
// A misconfigured / empty token yields 0 = no cap (Node: parseInt fail).
func compileSizeBytes(s string) int64 {
	s = strings.TrimSpace(strings.ToLower(s))
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "kb"):
		s = strings.TrimSuffix(s, "kb")
		mult = 1000
	case strings.HasSuffix(s, "mb"):
		s = strings.TrimSuffix(s, "mb")
		mult = 1e6
	case strings.HasSuffix(s, "gb"):
		s = strings.TrimSuffix(s, "gb")
		mult = 1e9
	case strings.HasSuffix(s, "k"):
		s = strings.TrimSuffix(s, "k")
		mult = 1024
	case strings.HasSuffix(s, "m"):
		s = strings.TrimSuffix(s, "m")
		mult = 1e6
	case strings.HasSuffix(s, "g"):
		s = strings.TrimSuffix(s, "g")
		mult = 1e9
	case strings.HasSuffix(s, "b"):
		s = strings.TrimSuffix(s, "b")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n * mult
}

// handle wraps the (int, error) handler contract: (0, err) -> the error
// middleware (finish); (code, nil) -> already rendered.
func (a *App) handle(w http.ResponseWriter, r *http.Request, fn func(http.ResponseWriter) (int, error)) {
	code, err := fn(w)
	if err == nil {
		_ = code
		return
	}
	if code != 0 {
		// Partial write: the body is on its way. Node's error middleware
		// fires again on the doomed socket; Go cannot re-render — log
		// only (documented divergence, see package doc).
		clsl.Debug(map[string]any{"err": err.Error(), "code": code, "path": r.URL.Path},
			"finish: partial write, render skipped")
		return
	}
	a.finish(w, r, 0, err)
}

// finish is the app-level error middleware (Node app.js error chain +
// LOCKED body text): arm order NotFound -> InvalidParameter -> EPIPE ->
// else, with the LOCKED 400/500 body strings. EPIPE is message-tagged
// (the Go dockerrunner has no typed EPIPE error — the message carries
// the code, compilecontroller isEPipe convention).
func (a *App) finish(w http.ResponseWriter, r *http.Request, code int, err error) {
	if err == nil {
		_ = code
		return
	}
	if code != 0 {
		clsl.Debug(map[string]any{"err": err.Error(), "code": code, "path": r.URL.Path},
			"finish: code already written, render skipped")
		return
	}
	switch {
	case clserrors.IsNotFoundError(err):
		clsl.Debug(map[string]any{"err": err.Error(), "path": r.URL.Path}, "not found error")
		w.WriteHeader(http.StatusNotFound)
	case compilemanager.IsNotCompiled(err):
		// D21: no compiled output -> 405 (empty body; added arm, after the
		// LOCKED NotFound arm so the LOCKED order is preserved).
		clsl.Debug(map[string]any{"err": err.Error(), "path": r.URL.Path}, "not compiled error")
		w.WriteHeader(http.StatusMethodNotAllowed)
	case clserrors.IsInvalidParameter(err):
		// LOCKED: `Bad request: (<message>)`
		clsl.Debug(map[string]any{"err": err.Error(), "path": r.URL.Path}, "invalid parameter error")
		a.plainText(w, http.StatusBadRequest, fmt.Sprintf("Bad request: (%s)", err.Error()))
	case isEPipe(err):
		// Node: error.code === 'EPIPE' -> res.sendStatus(503) (the
		// inspect container returned EPIPE on shutdown).
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusServiceUnavailable)
	default:
		// LOCKED: `CLSI server error (internal error: <message>)`
		clsl.Error(map[string]any{"err": err.Error(), "path": r.URL.Path}, "server error")
		a.plainText(w, http.StatusInternalServerError,
			fmt.Sprintf("CLSI server error (internal error: %s)", err.Error()))
	}
}

// plainText renders a LOCKED text body (Node res.send on a text error).
func (a *App) plainText(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	if _, err := io.WriteString(w, body); err != nil {
		clsl.Error(map[string]any{"err": err.Error()}, "plainText write error")
	}
}

// finishJSON renders a JSON body (gate / health_check / 501).
func (a *App) finishJSON(w http.ResponseWriter, code int, body map[string]any) {
	data, err := json.Marshal(body)
	if err != nil {
		clsl.Error(map[string]any{"err": err.Error()}, "finishJSON marshal error")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if _, err := w.Write(data); err != nil {
		clsl.Error(map[string]any{"err": err.Error()}, "finishJSON write error")
	}
}

// isEPipe mirrors `error?.code === 'EPIPE'` (the Go surface tags EPIPE in
// the message — dockerrunner has no typed error for it).
func isEPipe(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "EPIPE")
}
