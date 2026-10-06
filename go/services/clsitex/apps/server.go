// Package apps ports the Express app + server wiring from
// services/clsi/app.js (route table, error middleware, load agent,
// lifespan guard + smoke loop, health endpoints).
//
// Handler contract (mirrors the (int, error) seam, see the cc/cv package
// docs):
//
//	code == 0, err != nil  => Node next(error): respond renders the final
//	                          status (404/400/503/500) — the app-level error
//	                          middleware (error.go), applied only when
//	                          nothing has been written yet.
//	code != 0, err == nil  => handler already wrote its own response.
//	code != 0, err != nil  => handler wrote a body AND Node's next(error)
//	                          fired on a write error: the body is on its way
//	                          anyway; the error is logged, not re-rendered.
//
// Documented Go divergences:
//   - Metrics.injectMetricsRoute / Metrics.http.monitor / open_sockets /
//     memory / leaked_sockets monitors are not ported (observability only,
//     no consumer in this module).
//   - express.json({limit}) is ported as a body cap (413 res.sendStatus
//     body too large / 400 on parse).
//   - Node's per-connection express timeout (app.js TIMEOUT) is a socket
//     timeout; Go sets the equivalent on the cmd/server layer (Server +
//     conn.ReadTimeout) instead of per-request.
package apps

import (
	"net/http"

	cc "ollitex/go/services/clsitex/compilecontroller"
	"ollitex/go/services/clsitex/config"
	cv "ollitex/go/services/clsitex/conversioncontroller"
	fum "ollitex/go/services/clsitex/fileuploadmiddleware"
	"ollitex/go/services/clsitex/projectpersistence"
	"ollitex/go/services/clsitex/smoketest"
)

// App is the Go analogue of the express app (app.js default export).
type App struct {
	Config *config.Config

	// CC / CV are the compile and conversion controllers (Node: the
	// module imports). The apps layer owns route registration for them.
	CC *cc.Controller
	CV *cv.Controller

	// FUM is the multipart upload middleware (Node: FileUploadMiddleware).
	FUM *fum.Middleware

	// SmokeTest is the live smoke runner (Node: the SmokeTests.js import,
	// constructed + SetCompileURL'd by the cmd layer).
	SmokeTest *smoketest.SmokeTest

	// ProcessTooOld flips true when the lifespan guard fires
	// (Node: Settings.processTooOld).
	ProcessTooOld *bool

	// --- seams (production defaults wired by New; tests override) -----
	diskLow      func() bool // projectpersistence.IsAnyDiskLow
	diskCritical func() bool // projectpersistence.IsAnyDiskCriticalLow
}

// New builds the App with the production seams (Node: the module imports
// ProjectPersistenceManager / CompileController / Metrics).
func New(cfg *config.Config, CC *cc.Controller, CV *cv.Controller,
	FUM *fum.Middleware, processTooOld *bool, SmokeTest *smoketest.SmokeTest) *App {
	return &App{
		Config:        cfg,
		CC:            CC,
		CV:            CV,
		FUM:           FUM,
		SmokeTest:     SmokeTest,
		ProcessTooOld: processTooOld,
		diskLow:       projectpersistence.IsAnyDiskLow,
		diskCritical:  projectpersistence.IsAnyDiskCriticalLow,
	}
}

// Router assembles the route table (app.js:70-170 verbatim; Go 1.22 mux
// patterns: GET/POST are separate registrations per route).
//
// Node parity notes:
//   - express returns 404 for unmatched routes AND 405 for a wrong method
//     on a known path; Go's mux returns 405 for the method on a known
//     pattern and 404 otherwise, so no shim is needed.
//   - The output.zip mounts come BEFORE any output/* wildcard on purpose
//     (Node comment); Go 1.22 mux patterns are static, so ordering is
//     irrelevant.
func (a *App) Router() http.Handler {
	mux := http.NewServeMux()

	// Compile (project-scoped).
	mux.HandleFunc("POST /project/{project_id}/compile", a.compile)
	mux.HandleFunc("POST /project/{project_id}/compile/stop", a.stopCompile)
	mux.HandleFunc("DELETE /project/{project_id}", a.clearCache)
	mux.HandleFunc("GET /project/{project_id}/sync/code", a.syncCode)
	mux.HandleFunc("GET /project/{project_id}/sync/pdf", a.syncPdf)
	mux.HandleFunc("GET /project/{project_id}/wordcount", a.wordcount)
	mux.HandleFunc("POST /project/{project_id}/wordcount", a.wordcountPost)
	mux.HandleFunc("GET /project/{project_id}/status", a.status)
	mux.HandleFunc("POST /project/{project_id}/status", a.status)

	// Compile (per-user).
	mux.HandleFunc("POST /project/{project_id}/user/{user_id}/compile", a.compile)
	mux.HandleFunc("POST /project/{project_id}/user/{user_id}/compile/stop", a.stopCompile)
	mux.HandleFunc("DELETE /project/{project_id}/user/{user_id}", a.clearCache)
	mux.HandleFunc("GET /project/{project_id}/user/{user_id}/sync/code", a.syncCode)
	mux.HandleFunc("GET /project/{project_id}/user/{user_id}/sync/pdf", a.syncPdf)
	mux.HandleFunc("GET /project/{project_id}/user/{user_id}/wordcount", a.wordcount)
	mux.HandleFunc("POST /project/{project_id}/user/{user_id}/wordcount", a.wordcountPost)

	// Output zip (Node: "needs to be before output/*" — Go mux ranks the
	// most specific pattern by specificity, so no ordering constraint).
	mux.HandleFunc("GET /project/{project_id}/build/{build_id}/output/output.zip",
		a.outputZipNoUser)
	mux.HandleFunc("GET /project/{project_id}/user/{user_id}/build/{build_id}/output/output.zip",
		a.outputZipUser)

	// Output files (Node clsi: the shared-cls file-serving surface the editor
	// PDF/error panes fetch — output.pdf / output.log / output.blg /
	// output.synctex.gz / ...). Go mux ranks the literal /output/output.zip
	// patterns above over the {filepath...} wildcard, so the zip mounts keep
	// precedence. The initial port dropped these two routes and every editor
	// output fetch 404'd (live evidence 2026-10-06 19:36-19:43).
	mux.HandleFunc("GET /project/{project_id}/build/{build_id}/output/{filepath...}",
		a.buildFile)
	mux.HandleFunc("GET /project/{project_id}/user/{user_id}/build/{build_id}/output/{filepath...}",
		a.buildFile)

	// Convert (multipart upload via FUM; Node: FileUploadMiddleware).
	a.convertRoutes(mux)

	// Health + misc.
	mux.HandleFunc("GET /status", a.statusPing)
	mux.HandleFunc("GET /health_check", a.healthCheck)
	mux.HandleFunc("GET /smoke_test_force", a.smokeTestForce)

	return mux
}
