// compilecontroller.go ports the clsi_typst CompileController
// (Node: app/js/CompileController.js, 265 L) to the clsi.go Go convention:
// exported handler methods taking http.ResponseWriter and returning
// (status, error); (0, error) is Node's next(error) — the express error
// middleware (T10 server layer) then maps error type -> status/body.
//
// Node compile dispatch chain (L60-176): parseReq ->
// Metrics.Timer('compile-request') -> RequestParser.parse -> project_id/
// user_id assignment -> markProjectAsJustAccessed -> doCompileWithLock ->
// error dispatch (AlreadyCompiling 423 'compile-in-progress' /
// FilesOutOfSync 409 'conflict' / MissingUpdates 409 'missing-updates' /
// EPIPE|TooMany 503 'unavailable' / terminated / timedout / else 'error'
// 500) -> success gate (output.pdf size>0 -> 'success' + timestamp) -> wire
// send res.status(code || 200).send({compile: {...}}).
//
// D9 parity note (documented): Node's success-tail branches
// (stopOnFirstError -> 'stopped-on-first-error', plain 'failure') are DEAD
// in the typst port — CompileManager.doCompile-with-error ALWAYS throws
// (its success return is never reached), so `status` holds the dispatch
// value ('error' 500 for the dispatch else-case) and is rendered verbatim.
// The D16 gate (output.pdf size>0) is live: it stamps the success
// timestamp. The Go port preserves the Node structure (gate after the else
// arm) so a future nil-error+no-output case behaves exactly like Node.
package compilecontroller

import (
	"sync"
	"time"

	"ollitex/go/services/clsitypst/lastprojectaccess"

	compilemanager "ollitex/go/services/clsitypst/compilemanager"
	cltypstcfg "ollitex/go/services/clsitypst/config"
	projectpersistence "ollitex/go/services/clsitypst/projectpersistence"
)

// Controller ports the module-level state + imports of CompileController.js.
type Controller struct {
	// Manager is the ported CompileManager (Node: CompileManager).
	Manager *compilemanager.Manager

	// MarkProjectAccessed mirrors ProjectPersistenceManager.
	// markProjectAsJustAccessed (the Go form has no error case —
	// documented divergence).
	MarkProjectAccessed func(projectID string, nowMs int64)

	// ClearProject mirrors Node's ProjectPersistenceManager.clearProject
	// (clearCache: stopCompile -> PPM.clearProject -> 204): LAST_ACCESS
	// delete + rm compileDir AND outputDir {force, recursive} (clsi seam
	// convention; the production default is projectpersistence.ClearProject
	// — the app-level singleton, T10 inits it before serving).
	ClearProject func(projectID, userID string) error

	// Config carries the Settings.apis.clsi.* + settings RequestParser
	// reads (Settings.clsi / Settings.apis.clsi).
	Config ControllerConfig
}

// ControllerConfig mirrors the Settings reads CompileController performs
// (Node: Settings.apis.clsi.instanceType/zone/isSpotInstance/outputUrlPrefix/
// downloadHost, Settings.clsi.docker.allowedImages, settings.
// allowedCompileGroups, settings.pdfCachingMinChunkSize, settings.
// compileConcurrencyLimit, settings.compileSizeLimit).
type ControllerConfig struct {
	InstanceType         string
	Zone                 string
	IsSpotInstance       bool
	OutputURLPrefix      string
	DownloadHost         string
	AllowedImages        []string
	AllowedCompileGroups []string
	// Requestparser.AllowedCompileGroupsSet mirrors Node's
	// `allowedCompileGroups` truthiness (the slice presence, not
	// emptiness). Keep both for the requestparser.Config surface.
	AllowedCompileGroupsSet bool
	PdfCachingMinChunk      interface{}
}

// New wires the production seams (Node: the module imports).
func New(m *compilemanager.Manager, cfg *cltypstcfg.Config) *Controller {
	return &Controller{
		Manager:             m,
		MarkProjectAccessed: lastprojectaccess.SetLastProjectAccessTime,
		// Node clearCache: CompileManager.stopCompile -> PPM.clearProject.
		ClearProject: projectpersistence.ClearProject,
		Config: ControllerConfig{
			InstanceType:            cfg.APIs.Clsi.InstanceType,
			Zone:                    cfg.APIs.Clsi.Zone,
			IsSpotInstance:          cfg.APIs.Clsi.IsSpotInstance,
			OutputURLPrefix:         cfg.APIs.Clsi.OutputURLPrefix,
			DownloadHost:            cfg.APIs.Clsi.DownloadHost,
			AllowedImages:           []string{cfg.ClSI.Docker.Image},
			AllowedCompileGroups:    cfg.AllowedCompileGroups,
			AllowedCompileGroupsSet: cfg.AllowedCompileGroups != nil,
			PdfCachingMinChunk:      cfg.PdfCachingMinChunkSize,
		},
	}
}

// --- lastSuccessfulCompileTimestamp (module state, load agent) --------------------

var lastSuccessfulMu sync.Mutex
var lastSuccessfulCompileTimestamp int64

// setLastSuccessfulCompile stamps the success gate (Node:
// lastSuccessfulCompileTimestamp = Date.now()). Exported only for tests
// (the exported TimeSinceLastSuccessfulCompile reads it).
func setLastSuccessfulCompile(nowMs int64) {
	lastSuccessfulMu.Lock()
	defer lastSuccessfulMu.Unlock()
	lastSuccessfulCompileTimestamp = nowMs
}

func lastSuccessful() int64 {
	lastSuccessfulMu.Lock()
	defer lastSuccessfulMu.Unlock()
	return lastSuccessfulCompileTimestamp
}

// TimeSinceLastSuccessfulCompile ports the module export
// timeSinceLastSuccessfulCompile() (ms since the last successful compile;
// consumed by the load agent).
func TimeSinceLastSuccessfulCompile() int64 {
	return time.Now().UnixMilli() - lastSuccessful()
}
