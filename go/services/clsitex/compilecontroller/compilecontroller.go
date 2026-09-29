// Package compilecontroller ports services/clsi/app/js/CompileController.js
// (491L, express handlers + error dispatch).
//
// Go port (no express, mirroring outputcontroller conventions):
//
//   - Handlers are package-level exported functions taking
//     http.ResponseWriter and returning (status, error) for the server layer
//     to record. Node's express "next(error)" (unhandled error) is returned
//     as (0, error); the status 0 means "error before a status was decided".
//
// Node handlers: compile, stopCompile, clearCache, syncFromCode, syncFromPdf,
// wordcount, wordcountWithSync, status + the module export
// timeSinceLastSuccessfulCompile (consumed by the load agent).
//
// Dispatch (compile) mirrors Node verbatim, with two Go bridges:
//
//   - Node decorates the compile error object with .terminated/.validate/
//     .timedout/.code; Go's compilemanager classifies dockerrunner typed
//     errors into *compilemanager.CompileRunError with the same flags. The
//     controller inspects those flags exactly as Node inspects error?.x.
//
//   - Node's error.code === 'EPIPE' (docker stream close on shutdown) has no
//     Go typed error (documented divergence); the controller string-checks
//     error.Error() containing "EPIPE".
//
// Node's express parseReq (zod schemas) is ported as compilecontroller
// struct params + the requestparser.Parse call for compile bodies (the
// controller does the parse, mirroring Node's RequestParser.parse + the
// params/body assignment).
package compilecontroller

import (
	"sync"

	clsicachehandler "ollitex/go/services/clsitex/clsicachehandler"

	compilemanager "ollitex/go/services/clsitex/compilemanager"
)

// --- Controller (the 8 Node handlers + timer) -----------------------------------

// Controller is the ported CompileController. Node holds module-level
// state (lastSuccessfulCompileTimestamp) and imports (CompileManager,
// Settings, CLSICacheHandler.notifyCLSICacheAboutBuild); the Go form takes
// them as fields so tests can redirect the seams.
type Controller struct {
	// Manager is the ported CompileManager (Node: CompileManager).
	Manager *compilemanager.Manager

	// Notify is Node's notifyCLSICacheAboutBuild: CLSICacheHandler.Notify.
	Notify func(opts clsicachehandler.NotifyOpts) string

	// MarkProjectAccessed mirrors ProjectPersistenceManager.
	// markProjectAsJustAccessed (LAST_ACCESS.set; no error case in Go).
	MarkProjectAccessed func(projectID string, nowMs int64)

	// ClearProject mirrors ProjectPersistenceManager.clearProject (Node:
	// the cc clearCache step; the Go PPM runs CompileManager.clearProject +
	// HistoryResourceWriter.clearCache + clearProjectFromCache
	// [urlcache + LAST_ACCESS] as a chain).
	ClearProject func(projectID, userID string) error

	// Config carries the Node Settings.apis.clsi.* + Settings.clsi.docker.*
	// + settings RequestParser reads (Settings.clsi / Settings.apis.clsi).
	Config ControllerConfig
}

// ControllerConfig mirrors the Settings reads CompileController performs
// (Node: Settings.apis.clsi.instanceType/zone/isSpotInstance/outputUrlPrefix/
// downloadHost, Settings.clsi.docker.allowedImages, settings.allowedCompile
// Groups, settings.pdfCachingMinChunkSize).
type ControllerConfig struct {
	InstanceType         string
	Zone                 string
	IsSpotInstance       bool
	OutputURLPrefix      string
	DownloadHost         string
	AllowedImages        []string
	AllowedCompileGroups []string
	PdfCachingMinChunk   interface{}
	// ClearProject mirrors ProjectPersistenceManager.clearProject
	// (the cc clearCache chain). The apps layer (importing both packages)
	// wires projectpersistence.ClearProject; nil in tests falls back to
	// the Manager-local clear.
	ClearProject func(projectID, userID string) error
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
