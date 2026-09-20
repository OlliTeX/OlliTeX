// Package resourcewriter ports CLSI app/js/ResourceWriter.js (398L, callback
// style).
//
// It owns on-disk writes of project resources (local content or remote URLs)
// on both the full compile path (Compile/CompileRequestHandler via
// ResourceWriter.promises.syncResourcesToDisk) and the clsi-cache path — the
// latter consuming IsExtraneousFile directly (CLSICacheHandler.js:283 keeps
// non-extraneous output files to enqueue to clsi-cache shards) and the
// ResourceWriter.checkPath path guard (TikzManager.js:67,98).
//
// Node parity notes:
//
//   - Remote resources: Node resolves `url = resource.fallbackURL ??
//     resource.url` and then calls UrlCache.downloadUrlToFile(projectId, url,
//     resource.fallbackURL, path, resource.modified, cb) — WITHOUT a
//     conversion suffix in CLSI (its 6th param defaults to ”). Remote
//     download errors are LOGGED + counted (Metrics.inc 'download-failed')
//     and SWALLOWED (no error on the callback) so a missing remote file does
//     not abort the compile. Go mirrors: a DownloadFile seam defaulting to
//     urlcache.DownloadUrlToFile(..., "") + logger.Err +
//     metrics.IncDownloadFailed, and no error return from the URL branch.
//   - Content resources: fs.writeFile(resource.path, resource.content, 0644).
//     Local write errors DO propagate (no Node-style catch).
//   - checkPath: path.normalize(join(basePath, resourcePath)) then a
//     basePath+separator prefix test — a traversal outside the root yields
//     a plain `Error('resource path is outside root directory')`.
//   - extraneous-file cleanup after findOutputFiles: Node iterates the
//     findOutputFiles result (NOT a separate configured list) and deletes
//     each file whose IsExtraneousFile(relPath) is true.
//   - The precious-file glob `settings.preciousFilePattern` is compiled with
//     `dot: true` and is where the vendored minimatch port (ollitex/go
//     /minimatch) is consumed: the precious branch of IsExtraneousFile. CLSI
//     ships preciousFilePattern = ” (matches nothing; empty pattern is
//     valid, matching only the empty string — oracle-verified), so the
//     matcher is inert unless configured.
//
// The Node test suite (test/unit/js/ResourceWriter.test.js) is the contract
// the Go tests mirror: full/incremental sync shapes, per-path delete
// decisions in _removeExtraneousFiles, swallow-on-download-error, and
// checkPath traversal guard (three cases).
package resourcewriter

import (
	"time"

	mm "ollitex/go/minimatch"

	"clsi/config"
	"clsi/logger"
)

// Request mirrors the (project_id, user_id, syncType, syncState, resources,
// metricsOpts{path}) request object CLSI hands to syncResourcesToDisk.
type Request struct {
	ProjectID string
	UserID    string
	SyncType  string // '' | 'incremental' (Node: syncType)
	SyncState string // Node: syncState (undefined on full compile)
	Resources []Resource
	// MetricsPath — metricsOpts.path; shouldSkipMetrics keys off this.
	MetricsPath string
}

// Resource mirrors a request resource: {path, url?, fallbackURL?, content?,
// modified?} (zod: path string required; url/fallbackURL/content optional).
type Resource struct {
	Path        string
	URL         string
	FallbackURL string
	Content     []byte
	Modified    *time.Time
}

// --- precious-file matcher (minimatch, {dot: true}) --------------------------

// PreciousFileMatcher is `const preciousFileMatcher = new Minimatch(
// settings.preciousFilePattern, {dot:true})` — built from
// config.PreciousFilePattern (default ” => matches only the empty string).
// CLSI main / tests establish it via InitPreciousFileMatcher; the lazy
// ensurePreciousMatcher derives it from config on first IsExtraneousFile
// use IF and only if it has not been established yet.
var (
	preciousReady       bool
	PreciousFileMatcher *mm.Minimatch
)

// InitPreciousFileMatcher (re)builds the dot:true glob matcher for the given
// pattern AND marks the package initialized so ensurePreciousMatcher no
// longer derives from config. This is the only place
// settings.preciousFilePattern feeds into the minimatch port. CLSI default
// pattern ” is valid (matches nothing except ""); a malformed pattern
// leaves the matcher nil (no-op precious branch).
func InitPreciousFileMatcher(pattern string) {
	match, err := mm.New(pattern, &mm.Options{Dot: true})
	if err != nil {
		logger.Error(map[string]any{"pattern": pattern, "err": err},
			"resourcewriter: failed to build precious file matcher; using none")
		PreciousFileMatcher = nil
	} else {
		PreciousFileMatcher = match
	}
	preciousReady = true
}

// ensurePreciousMatcher derives the matcher from live config exactly when
// the package has not been initialized (CLSI main init / first production
// use). InitPreciousFileMatcher (tests, CLSI startup) short-circuits it.
func ensurePreciousMatcher() {
	if preciousReady {
		return
	}
	InitPreciousFileMatcher(config.Get().PreciousFilePattern)
}

// shouldSkipMetrics mirrors ClsiMetrics.shouldSkipMetrics(request).
