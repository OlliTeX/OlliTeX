// Package outputcachemanager ports services/clsi/app/js/OutputCacheManager.js
// 1:1 (drop-in).
//
// Porting notes:
//
//   - Node module-singleton state (OLDEST_BUILD_DIR, PENDING_PROJECT_ACTIONS)
//     is per-Manager here so tests can isolate; production runs one Manager
//     for the whole process (the app serves as the singleton holder).
//   - queueDirOperation's promise chain (.then(fn, fn)) maps to a per-dir
//     goroutine pump draining a slice queue: jobs run strictly sequentially
//     per dir, one job's error does not cancel the next, and .finally()'s map
//     delete maps to the pump deleting the pump entry when the queue drains.
//   - cleanupDirectory inside saveOutputFilesInBuildDir's success path is
//     fire-and-forget in Node (un-awaited promise + .catch) — here it is
//     EnqueueDirOperation: a goroutine-driven job on the SAME dir queue that
//     serializes without deadlocking the pump.
//   - Node `fs.copyFile` ENOENT propagates as a hard error: the copy fails
//     and saveOutputFilesInBuildDir rm's its cacheDir before propagating.
//   - expireOutputFiles dir-name parse mirrors parseInt(name.split('-')[0],16)
//     JS semantics: leading-hex-only, all-garbage → NaN (Go float64 NaN whose
//     comparisons are always false, matching JS).
//   - Metrics seam: Node `Metrics.inc('pdf-caching-status', 1, {...})` fires
//     for EVERY saveStreams outcome (including error statuses); the Go
//     counter is label-less until the server port wires prometheus labels.
package outputcachemanager

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	mathrand "math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clsi/contentcachemanager"
	clserrors "clsi/errors"
	"clsi/logger"
	metrics "clsi/metrics"
	off "clsi/outputfilefinder"
	"clsi/outputfileoptimiser"
)

// Mirrors the static OutputCacheManager constants.
const (
	// ContentSubdir mirrors CONTENT_SUBDIR.
	ContentSubdir = "content"
	// CacheSubdir mirrors CACHE_SUBDIR.
	CacheSubdir = "generated-files"
	// ArchiveSubdir mirrors ARCHIVE_SUBDIR.
	ArchiveSubdir = "archived-logs"
	// CacheLimit mirrors CACHE_LIMIT (maximum cache directories kept).
	CacheLimit = 2
	// CacheAge mirrors CACHE_AGE: up to 90 minutes, in ms.
	CacheAge = 90 * 60 * 1000
)

// bulkCleanupSchedulePadding mirrors the `+ 60 * 1000` in scheduleBulkCleanup.
const bulkCleanupSchedulePadding = 60 * 1000

// BuildIdRegExp mirrors BUILD_REGEX/CONTENT_REGEX: /^[0-9a-f]+-[0-9a-f]+$/.
var BuildIdRegExp = regexp.MustCompile(`^[0-9a-f]+-[0-9a-f]+$`)

// perUserRegexp mirrors the per-user compileDir basename check
// /^[0-9a-f]{24}-[0-9a-f]{24}$/.
var perUserRegexp = regexp.MustCompile(`^[0-9a-f]{24}-[0-9a-f]{24}$`)

// fileHiddenRegexp mirrors _fileIsHidden: /^\.|\/\./.
var fileHiddenRegexp = regexp.MustCompile(`^\.|/\.`)

// straceRegexp mirrors the /^strace/ basename prefix check.
var straceRegexp = regexp.MustCompile(`^strace`)

// queueJob is one entry on a per-dir operation queue (Node promise chain
// link). val/err are written by the pump; the waiter copies them after the
// done channel closes, so no lock is needed on read.
type queueJob struct {
	call func() (any, error)
	val  any
	err  error
	done chan struct{}
}

// pumpState holds the slice queue for one dir; a single pump goroutine per
// dir drains it sequentially (Node: PENDING_PROJECT_ACTIONS chain).
type pumpState struct {
	jobs []*queueJob
}

// SaveRequest mirrors the folded {request, stats, timings} arguments:
// request = {buildId, enablePdfCaching, pdfCachingMinChunkSize, ...}.
// CompileTime is reserved for the CompileManager (the package reads
// compileTime from timings["compile"], mirroring Node exactly).
type SaveRequest struct {
	BuildID                string
	EnablePdfCaching       bool
	PdfCachingMinChunkSize int64
	MetricsOpts            map[string]any
}

// SaveResult mirrors the callback(null, {outputFiles, buildId}) shape.
type SaveResult struct {
	BuildID string
	Files   []off.OutputFile
}

// Manager mirrors OutputCacheManager: static fields + Settings reads +
// injectable seams (Node module imports / test doubles).
type Manager struct {
	// Settings mirrors (Node reads Settings.* at call time):
	// Settings.enablePdfCaching, Settings.enablePdfCachingDark,
	// Settings.clsi.archive_logs, Settings.clsi.strace,
	// Settings.clsi.optimiseInDocker.
	PdfCachingEnabled bool
	PdfCachingDark    bool
	ArchiveLogs       bool
	Strace            bool
	OptimiseInDocker  bool

	// Now mirrors Date.now (ms since epoch).
	Now func() int64
	// RandHex mirrors crypto.randomBytes(8) → hex string.
	RandHex func() (string, error)
	// UpdateContent mirrors ContentCacheManager.update (default: the Go
	// port of updateSameEventLoop).
	UpdateContent func(a contentcachemanager.UpdateArgs) (*contentcachemanager.UpdateResult, error)
	// OptimiseFile mirrors OutputFileOptimiser.optimiseFile(src, dst, cb).
	OptimiseFile func(src, dst string) error
	// ScheduleAfter mirrors setTimeout (the recursive bulk-cleanup loop).
	ScheduleAfter func(delayMs int64, fn func())
	// MetricsInc mirrors Metrics.inc('pdf-caching-status', 1, {status, ...}).
	MetricsInc func(status string, opts map[string]any)
	// Log mirrors logger.{debug,warn,error,fatal}: (level, msg, obj).
	Log func(level, msg string, obj map[string]any)

	// OldestBuildDir mirrors the OLDEST_BUILD_DIR Map (dir → timestamp ms).
	oldestMu sync.Mutex
	oldest   map[string]float64
	// Pumps mirrors PENDING_PROJECT_ACTIONS (dir → queue).
	pumpMu sync.Mutex
	pumps  map[string]*pumpState
}

// New returns a Manager with production defaults (Node module wiring).
func New() *Manager {
	return &Manager{
		PdfCachingEnabled: false,
		PdfCachingDark:    false,
		ArchiveLogs:       false,
		Strace:            false,
		OptimiseInDocker:  true,
		Now:               func() int64 { return time.Now().UnixMilli() },
		RandHex:           defaultRandHex,
		UpdateContent:     contentcachemanager.Update,
		OptimiseFile:      func(src, dst string) error { return outputfileoptimiser.OptimiseFile(src, dst, nil) },
		ScheduleAfter:     defaultScheduleAfter,
		MetricsInc:        defaultMetricsInc,
		Log:               defaultLog,
		oldest:            map[string]float64{},
		pumps:             map[string]*pumpState{},
	}
}

func defaultRandHex() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func defaultMetricsInc(status string, opts map[string]any) {
	// Node: Metrics.inc('pdf-caching-status', 1, {status, ...opts}).
	// Label wiring (prometheus) lands with the server port.
	_ = status
	_ = opts
	metrics.PdfCachingStatus.Inc()
}

func defaultLog(level, msg string, obj map[string]any) {
	switch level {
	case "debug":
		logger.Debug(obj, msg)
	case "info":
		logger.Info(obj, msg)
	case "warn":
		logger.Warn(obj, msg)
	default:
		logger.Error(obj, msg)
	}
}

func defaultScheduleAfter(delayMs int64, fn func()) {
	go func() {
		time.Sleep(time.Duration(delayMs) * time.Millisecond)
		fn()
	}()
}

// pump drains the job queue for dir sequentially.
func (m *Manager) pump(dir string) {
	for {
		m.pumpMu.Lock()
		st, ok := m.pumps[dir]
		if !ok || len(st.jobs) == 0 {
			if ok {
				delete(m.pumps, dir)
			}
			m.pumpMu.Unlock()
			return
		}
		job := st.jobs[0]
		st.jobs = st.jobs[1:]
		m.pumpMu.Unlock()
		// Node: pending.then(fn, fn) — the previous job's error never
		// cancels this one; each job's error is independent.
		job.val, job.err = job.call()
		close(job.done)
	}
}

func (m *Manager) enqueue(dir string, job *queueJob) {
	m.pumpMu.Lock()
	defer m.pumpMu.Unlock()
	st, ok := m.pumps[dir]
	if !ok {
		st = &pumpState{}
		m.pumps[dir] = st
	}
	st.jobs = append(st.jobs, job)
	// The pump goroutine only starts here (it deleted the map entry when
	// it drained), so a length 0→1 transition launches it.
	if len(st.jobs) == 1 {
		go m.pump(dir)
	}
}

// QueueDirOperation mirrors queueDirOperation (await the next op on dir).
func (m *Manager) QueueDirOperation[T any](dir string, fn func() (T, error)) (T, error) {
	job := &queueJob{call: func() (any, error) { return fn() }, done: make(chan struct{})}
	m.enqueue(dir, job)
	<-job.done
	if job.err != nil {
		var zero T
		return zero, job.err
	}
	res, _ := job.val.(T)
	return res, nil
}

// EnqueueDirOperation is the fire-and-forget twin of QueueDirOperation
// (Node: promise started but never awaited).
func (m *Manager) EnqueueDirOperation(dir string, fn func() error) {
	m.enqueue(dir, &queueJob{
		call: func() (any, error) { return nil, fn() },
		done: make(chan struct{}),
	})
}

// Path mirrors OutputCacheManager.path: given buildId return
// CACHE_SUBDIR/buildId/file, or the bare file for invalid build ids.
func (m *Manager) Path(buildId, file string) string {
	if BuildIdRegExp.MatchString(buildId) {
		return filepath.Join(CacheSubdir, buildId, file)
	}
	return file
}

// GenerateBuildId mirrors generateBuildId: `${Date.now().toString(16)}-${hex8}`.
func (m *Manager) GenerateBuildId() (string, error) {
	random, err := m.RandHex()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x-%s", m.Now(), random), nil
}

// Init mirrors init(): doInit().catch(logger.fatal) — Go surfaces errors;
// the app port logs the fatal-equivalent.
func (m *Manager) Init(outputDir string) error {
	if err := m.fillCache(outputDir); err != nil {
		return err
	}
	oldest, err := m.RunBulkCleanup()
	if err != nil {
		return err
	}
	m.scheduleBulkCleanup(oldest)
	return nil
}

// fillCache mirrors fillCache: register every entry of outputDir for
// cleanup "in the next hour" (Node does NOT stat — plain fs.opendir, files
// included; Go ReadDir is likewise unfiltered per the 1:1 decision).
func (m *Manager) fillCache(outputDir string) error {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return err
	}
	now := float64(m.Now())
	for _, e := range entries {
		// Node: Date.now() - Math.random() * CACHE_AGE
		m.setOldest(filepath.Join(outputDir, e.Name()),
			now-mathrand.Float64()*float64(CacheAge))
	}
	return nil
}

// setOldest inserts into the OLDEST_BUILD_DIR mirror.
func (m *Manager) setOldest(dir string, ts float64) {
	m.oldestMu.Lock()
	m.oldest[dir] = ts
	m.oldestMu.Unlock()
}

func (m *Manager) hasOldest(dir string) bool {
	m.oldestMu.Lock()
	defer m.oldestMu.Unlock()
	_, ok := m.oldest[dir]
	return ok
}

// RunBulkCleanup mirrors runBulkCleanup: clean up dirs whose registered
// timestamp is past the age threshold; return the oldest kept timestamp.
// Map iteration order in Node is insertion-ordered; Go sort gives a
// deterministic order (min() semantics are order-independent anyway).
func (m *Manager) RunBulkCleanup() (float64, error) {
	now := float64(m.Now())
	threshold := now - float64(CacheAge)
	oldestTimestamp := now
	type entryPair struct {
		dir string
		ts  float64
	}
	var pairs []entryPair
	m.oldestMu.Lock()
	for dir, ts := range m.oldest {
		pairs = append(pairs, entryPair{dir, ts})
	}
	m.oldestMu.Unlock()
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].dir < pairs[j].dir })
	for _, p := range pairs {
		if p.ts < threshold {
			limit := CacheLimit
			if err := m.CleanupDirectory(p.dir, newExpireOptions("", &limit)); err != nil {
				return 0, err
			}
		} else if p.ts < oldestTimestamp {
			oldestTimestamp = p.ts
		}
	}
	return oldestTimestamp, nil
}

// scheduleBulkCleanup mirrors the recursive scheduleBulkCleanup(delay+60s).
func (m *Manager) scheduleBulkCleanup(oldestTimestamp float64) {
	delay := int64(math.Max(float64(CacheAge)+oldestTimestamp-float64(m.Now()), 0) +
		float64(bulkCleanupSchedulePadding))
	m.ScheduleAfter(delay, func() {
		oldest, err := m.RunBulkCleanup()
		if err != nil {
			m.Log("fatal", "low level error running bulk cleanup",
				map[string]any{"err": err.Error()})
			return
		}
		m.scheduleBulkCleanup(oldest)
	})
}

// CleanupDirectory mirrors cleanupDirectory: queue the op on dir's queue;
// expire errors are LOGGED and swallowed (the queued fn always succeeds,
// exactly like the Node catch).
func (m *Manager) CleanupDirectory(dir string, options *ExpireOptions) error {
	_, err := m.QueueDirOperation(dir, func() (struct{}, error) {
		if err := m.ExpireOutputFiles(dir, options); err != nil {
			m.Log("error", "cleanup of output directory failed", map[string]any{
				"dir": dir, "err": err.Error(),
			})
		}
		return struct{}{}, nil
	})
	return err
}

// ExpireOptions mirrors the {keep, limit} options of expireOutputFiles.
// Limit nil = absent (Node: options?.limit != null).
type ExpireOptions struct {
	keep  string
	limit *int
}

func newExpireOptions(keep string, limit *int) *ExpireOptions {
	return &ExpireOptions{keep: keep, limit: limit}
}

func (o *ExpireOptions) hasLimit() (int, bool) {
	if o == nil || o.limit == nil {
		return 0, false
	}
	return *o.limit, true
}

// ExpireOutputFiles mirrors expireOutputFiles (async version).
func (m *Manager) ExpireOutputFiles(outputDir string, options *ExpireOptions) error {
	cacheRoot := filepath.Join(outputDir, CacheSubdir)
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// cache directory is empty (or missing): remove the whole dir
			if cErr := m.cleanupAll(outputDir); cErr != nil {
				return cErr
			}
			return nil
		}
		m.Log("error", "error clearing cache", map[string]any{
			"err": err.Error(), "projectId": cacheRoot,
		})
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	// Node: results.sort().reverse() — ReadDir is already ascending, so
	// reverse to oldest-name-first... no: reverse yields DESCENDING (newest
	// build id first), exactly matching Node after the second sort.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	currentTime := float64(m.Now())
	var oldestDirTimeToKeep float64
	keepDir := ""
	var limit *int
	hasLimit := false
	if options != nil {
		keepDir = options.keep
		limit = options.limit
		hasLimit = limit != nil
	}

	var toRemove []string
	for i, dir := range names {
		if keepDir == dir {
			// this build dir is the one we just created: keep it
			oldestDirTimeToKeep = currentTime
			continue
		}
		if hasLimit && i > *limit {
			toRemove = append(toRemove, dir)
			continue
		}
		if i > CacheLimit {
			toRemove = append(toRemove, dir)
			continue
		}
		// build time comes from the DDDD part of DDDD-RRRR (hex).
		dirTime := nodeParseInt16(firstDashPart(dir))
		age := currentTime - dirTime
		if age > float64(CacheAge) {
			toRemove = append(toRemove, dir)
			continue
		}
		oldestDirTimeToKeep = dirTime
	}
	if len(toRemove) == len(names) {
		// no builds left after cleanup: drop everything
		if cErr := m.cleanupAll(outputDir); cErr != nil {
			return cErr
		}
		return nil
	}
	for _, dir := range toRemove {
		if rErr := os.RemoveAll(filepath.Join(cacheRoot, dir)); rErr != nil {
			m.Log("error", "cache remove error", map[string]any{
				"err": rErr.Error(), "dir": dir,
			})
			return rErr
		}
		m.Log("debug", "removed expired cache dir", map[string]any{
			"cache": cacheRoot, "dir": dir,
		})
	}
	m.setOldest(outputDir, oldestDirTimeToKeep)
	return nil
}

// cleanupAll mirrors the cleanupAll closure: rm -rf outputDir and drop the
// OLDEST_BUILD_DIR entry.
func (m *Manager) cleanupAll(outputDir string) error {
	if err := os.RemoveAll(outputDir); err != nil {
		return err
	}
	m.oldestMu.Lock()
	delete(m.oldest, outputDir)
	m.oldestMu.Unlock()
	return nil
}

// firstDashPart mirrors dir.split('-')[0].
func firstDashPart(dir string) string {
	i := strings.Index(dir, "-")
	if i < 0 {
		return ""
	}
	return dir[:i]
}

// nodeParseInt16 mirrors parseInt(str, 16) JS semantics: skip leading
// (JS) whitespace, optional sign, then the longest hex-digit prefix.
// Returns NaN (Go float64) when no leading hex digit is found — the
// caller's age comparison is then always false, matching JS.
func nodeParseInt16(s string) float64 {
	i := 0
	for i < len(s) && isJSWhitespace(s[i]) {
		i++
	}
	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	j := i
	for j < len(s) && isHexByte(s[j]) {
		j++
	}
	if j == i {
		return math.NaN()
	}
	v, err := strconv.ParseInt(s[start:j], 16, 64)
	if err != nil {
		return math.NaN()
	}
	return float64(v)
}

func isJSWhitespace(b byte) bool {
	// ES5 single-byte whitespace (Go scanning: multi-byte unicode spaces
	// in directory names are not a concern here).
	switch b {
	case ' ', 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x1c, 0x1d, 0x1e, 0x1f, 0x7f, 0xa0:
		return true
	}
	return false
}

func isHexByte(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// SaveOutputFiles mirrors saveOutputFiles:
// getBuildId → register outputDir in OLDEST_BUILD_DIR → (queued)
// saveOutputFilesInBuildDir → collectOutputPdfSize → gate →
// saveStreamsInContentDir → Metrics.inc → callback(null, {outputFiles,
// buildId}). saveStreamsInContentDir's error is logged and swallowed —
// only build-id generation, copy errors and the pdf-size stat error fail
// the compile.
func (m *Manager) SaveOutputFiles(
	req SaveRequest,
	files []off.OutputFile,
	compileDir string,
	outputDir string,
	stats map[string]float64,
	timings map[string]float64,
) (*SaveResult, error) {
	buildId, err := m.getBuildId(req.BuildID)
	if err != nil {
		return nil, err
	}
	if !m.hasOldest(outputDir) {
		m.setOldest(outputDir, float64(m.Now()))
	}
	outFiles, err := m.QueueDirOperation(outputDir, func() ([]off.OutputFile, error) {
		return m.saveOutputFilesInBuildDir(files, compileDir, outputDir, buildId)
	})
	if err != nil {
		return nil, err
	}
	if sErr := m.collectOutputPdfSize(outFiles, outputDir, stats); sErr != nil {
		return nil, sErr
	}
	enablePdfCaching := req.EnablePdfCaching
	dark := m.PdfCachingDark && !enablePdfCaching
	if !m.PdfCachingEnabled || (!enablePdfCaching && !dark) {
		return &SaveResult{BuildID: buildId, Files: outFiles}, nil
	}
	status, sErr := m.saveStreamsInContentDir(req, outFiles, stats, timings, dark,
		compileDir, outputDir)
	// Node Metrics.inc fires for EVERY (err, status) callback from
	// saveStreamsInContentDir — including swallowed error statuses.
	m.MetricsInc(status, req.MetricsOpts)
	if sErr != nil {
		m.Log("warn", "pdf caching failed", map[string]any{
			"err": sErr.Error(), "outputDir": outputDir,
		})
	}
	return &SaveResult{BuildID: buildId, Files: outFiles}, nil
}

func (m *Manager) getBuildId(requestBuildId string) (string, error) {
	if requestBuildId != "" {
		return requestBuildId, nil
	}
	return m.GenerateBuildId()
}

// saveOutputFilesInBuildDir mirrors saveOutputFilesInBuildDir:
// (fire-and-forget) archiveLogs → mkdir cacheDir → copy each output file →
// on success: return copied list + (fire-and-forget) cleanupDirectory;
// on copy error: rm newly-created cacheDir(s) and return error.
func (m *Manager) saveOutputFilesInBuildDir(
	files []off.OutputFile,
	compileDir string,
	outputDir string,
	buildId string,
) ([]off.OutputFile, error) {
	cacheDir := filepath.Join(outputDir, CacheSubdir, buildId)
	perUser := perUserRegexp.MatchString(filepath.Base(compileDir))

	// Archive logs in background: Node fires this promise and does NOT
	// await it, so mirror with a goroutine.
	if m.ArchiveLogs || m.Strace {
		go func() {
			if err := m.archiveLogs(files, compileDir, outputDir, buildId); err != nil {
				m.Log("warn", "erroring archiving log files", map[string]any{
					"err": err.Error(),
				})
			}
		}()
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		m.Log("error", "error creating cache directory", map[string]any{
			"err": err.Error(), "directory": cacheDir,
		})
		return nil, err
	}
	created := map[string]struct{}{cacheDir: {}}
	results := make([]off.OutputFile, 0, len(files))
	for i, file := range files {
		// dot files are not served by the static server, so skip them
		if fileHidden(file.Path) {
			m.Log("debug", "ignoring dotfile in output", map[string]any{
				"compileDir": compileDir, "path": file.Path,
			})
			continue
		}
		src := filepath.Join(compileDir, file.Path)
		dst := filepath.Join(cacheDir, file.Path)
		shouldCopy, err := m.checkIfShouldCopy(src)
		if err != nil {
			m.rmCreated(created)
			return nil, err
		}
		if !shouldCopy {
			continue
		}
		if err := m.copyFile(src, dst, created); err != nil {
			m.rmCreated(created)
			return nil, err
		}
		files[i].Build = buildId
		results = append(results, files[i])
	}
	// On success: let file expiry run in the background. Node enqueues this
	// cleanupDirectory on the SAME per-outputDir queue and never awaits it
	// (the promise's .catch(() => {})) — EnqueueDirOperation mirrors that,
	// serialising without dead-locking the pump.
	m.EnqueueDirOperation(outputDir, func() error {
		var limit *int
		if perUser {
			l := 1
			limit = &l
		}
		return m.CleanupDirectory(outputDir, newExpireOptions(buildId, limit))
	})
	return results, nil
}

// rmCreated mirrors `fs.rm(cacheDir, {force: true, recursive: true})` after
// a copy failure: remove only what this call created.
func (m *Manager) rmCreated(created map[string]struct{}) {
	for dir := range created {
		if err := os.RemoveAll(dir); err != nil {
			m.Log("error", "error removing cache dir after failure", map[string]any{
				"err": err.Error(), "dir": dir,
			})
		}
	}
}

// collectOutputPdfSize mirrors collectOutputPdfSize: stat output.pdf (by
// path) and set file.Size + stats['pdf-size']. A stat error propagates
// (the compile fails), matching the Node callback(err, ...).
func (m *Manager) collectOutputPdfSize(
	files []off.OutputFile,
	outputDir string,
	stats map[string]float64,
) error {
	for i, file := range files {
		if file.Path != "output.pdf" {
			continue
		}
		p := filepath.Join(outputDir, m.Path(file.Build, file.Path))
		st, err := os.Stat(p)
		if err != nil {
			return err
		}
		size := st.Size()
		files[i].Size = &size
		stats["pdf-size"] = float64(size)
		break
	}
	return nil
}

// saveStreamsInContentDir mirrors saveStreamsInContentDir. Returns
// (status, err): status is ALWAYS non-empty (for the metrics label) and
// err is set only for hard failures (file/ensure content dir errors and
// "failed").
func (m *Manager) saveStreamsInContentDir(
	req SaveRequest,
	files []off.OutputFile,
	stats map[string]float64,
	timings map[string]float64,
	dark bool,
	compileDir string,
	outputDir string,
) (string, error) {
	contentRoot := filepath.Join(outputDir, ContentSubdir)
	contentDir, err := m.ensureContentDir(contentRoot)
	if err != nil {
		return "content-dir-unavailable", err
	}
	pdfIdx := -1
	for i, f := range files {
		if f.Path == "output.pdf" {
			pdfIdx = i
			break
		}
	}
	if pdfIdx < 0 {
		return "missing-pdf", nil
	}
	filePdfSize := files[pdfIdx].Size
	filePdfSizeValue := int64(0)
	if filePdfSize != nil {
		filePdfSizeValue = *filePdfSize
	}
	compileTime := float64(0.0)
	if timings != nil {
		compileTime = timings["compile"]
	}
	// Node: new Metrics.Timer + update(...) — Go seams: Now for the timer,
	// UpdateContent for the CCM call.
	timerStart := m.Now()
	res, uErr := m.UpdateContent(contentcachemanager.UpdateArgs{
		ContentDir:             contentDir,
		FilePath:               filepath.Join(outputDir, m.Path(files[pdfIdx].Build, files[pdfIdx].Path)),
		PdfSize:                filePdfSizeValue,
		PdfCachingMinChunkSize: req.PdfCachingMinChunkSize,
		CompileTime:            compileTime,
	})
	if uErr != nil {
		var noXref *clserrors.NoXrefTableError
		if errors.As(uErr, &noXref) {
			// Node callback(null, err.message): success with the message as
			// the status string.
			return noXref.Message, nil
		}
		var queueLimit *clserrors.QueueLimitReachedError
		if errors.As(uErr, &queueLimit) {
			m.Log("warn", "pdf caching queue limit reached", map[string]any{
				"err": uErr, "outputDir": outputDir,
			})
			stats["pdf-caching-queue-limit-reached"] = 1
			return "queue-limit", nil
		}
		var timedOut *clserrors.TimedOutError
		if errors.As(uErr, &timedOut) {
			m.Log("warn", "pdf caching timed out", map[string]any{
				"err": uErr, "outputDir": outputDir, "stats": stats, "timings": timings,
			})
			stats["pdf-caching-timed-out"] = 1
			return "timed-out", nil
		}
		return "failed", uErr
	}
	// Success (possibly soft-timed-out).
	status := "success"
	if res.TimedOutErr != nil {
		m.Log("warn", "pdf caching timed out - soft failure", map[string]any{
			"err": res.TimedOutErr, "outputDir": outputDir,
		})
		stats["pdf-caching-timed-out"] = 1
		status = "timed-out-soft-failure"
	}
	if !dark {
		contentId := filepath.Base(contentDir)
		files[pdfIdx].ContentID = &contentId
		ranges := make([]off.ContentRange, 0, len(res.ContentRanges))
		for _, r := range res.ContentRanges {
			ranges = append(ranges, off.ContentRange{
				ObjectID: r.ObjectID, Start: r.Start, End: r.End, Hash: r.Hash,
			})
		}
		files[pdfIdx].Ranges = ranges
		// CCM never sets startXRefTable (port decision); keep the pointer
		// as-is so the wire shape matches.
	}
	if timings != nil {
		// Node: timer.done() returns ms elapsed.
		timings["compute-pdf-caching"] = float64(m.Now() - timerStart)
	}
	totalSize := int64(0)
	for _, r := range res.ContentRanges {
		totalSize += r.End - r.Start
	}
	newTotalSize := int64(0)
	for _, r := range res.NewContentRanges {
		newTotalSize += r.End - r.Start
	}
	stats["pdf-caching-n-ranges"] = float64(len(res.ContentRanges))
	stats["pdf-caching-total-ranges-size"] = float64(totalSize)
	stats["pdf-caching-n-new-ranges"] = float64(len(res.NewContentRanges))
	stats["pdf-caching-new-ranges-size"] = float64(newTotalSize)
	stats["pdf-caching-reclaimed-space"] = float64(res.ReclaimedSpace)
	if res.OverheadDeleteStaleHashes != nil && timings != nil {
		timings["pdf-caching-overhead-delete-stale-hashes"] = float64(*res.OverheadDeleteStaleHashes)
	}
	return status, nil
}

// ensureContentDir mirrors ensureContentDir: mkdir → readdir (sorted) →
// first BUILD_REGEX match, else generate a fresh content dir.
func (m *Manager) ensureContentDir(contentRoot string) (string, error) {
	if err := os.MkdirAll(contentRoot, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(contentRoot)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		dirs = append(dirs, e.Name())
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		if BuildIdRegExp.MatchString(d) {
			return filepath.Join(contentRoot, d), nil
		}
	}
	contentId, err := m.GenerateBuildId()
	if err != nil {
		return "", err
	}
	contentDir := filepath.Join(contentRoot, contentId)
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		return "", err
	}
	return contentDir, nil
}

// (err != nil, status set) from a successful ensure — Node emits
// callback(err, 'content-dir-unavailable').

// archiveLogs mirrors archiveLogs: mkdir archiveDir → each output file →
// _checkIfShouldArchive → _copyFile (on success only).
func (m *Manager) archiveLogs(
	files []off.OutputFile,
	compileDir string,
	outputDir string,
	buildId string,
) error {
	archiveDir := filepath.Join(outputDir, ArchiveSubdir, buildId)
	m.Log("debug", "archiving log files for project", map[string]any{"dir": archiveDir})
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return err
	}
	created := map[string]struct{}{archiveDir: {}}
	for _, file := range files {
		src := filepath.Join(compileDir, file.Path)
		dst := filepath.Join(archiveDir, file.Path)
		shouldArchive, err := m.checkIfShouldArchive(src)
		if err != nil {
			return err
		}
		if !shouldArchive {
			continue
		}
		if err := m.copyFile(src, dst, created); err != nil {
			return err
		}
	}
	return nil
}

// ensureParentExists mirrors _ensureParentExists: mkdir(dst.parent) and
// add parent + ancestors to dirCache until a cached ancestor is reached.
func (m *Manager) ensureParentExists(dst string, dirCache map[string]struct{}) error {
	parent := filepath.Dir(dst)
	if _, ok := dirCache[parent]; ok {
		return nil
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	for {
		dirCache[parent] = struct{}{}
		cwdParent := parent
		parent = filepath.Dir(parent)
		if parent == cwdParent {
			break
		}
		if _, ok := dirCache[parent]; ok {
			break
		}
	}
	return nil
}

// copyFile mirrors _copyFile: _ensureParentExists → copy → (skip optimise
// when Settings.clsi.optimiseInDocker). Node fs.copyFile ENOENT is a HARD
// error (the copy fails; the caller cleans up and propagates), so mirror:
// any missing-source error returns to the caller.
func (m *Manager) copyFile(src, dst string, dirCache map[string]struct{}) error {
	if err := m.ensureParentExists(dst, dirCache); err != nil {
		m.Log("warn", "creating parent directory in output cache failed", map[string]any{
			"err": err.Error(), "dst": dst,
		})
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			m.Log("warn", "file has disappeared when copying to build cache", map[string]any{
				"err": err.Error(), "file": src,
			})
		} else {
			m.Log("error", "copy error for file in cache", map[string]any{
				"err": err.Error(), "src": src, "dst": dst,
			})
		}
		return err
	}
	defer in.Close()
	if err := m.ensureParentExists(dst, dirCache); err != nil {
		m.Log("warn", "creating parent directory in output cache failed", map[string]any{
			"err": err.Error(), "dst": dst,
		})
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		m.Log("error", "copy error for file in cache", map[string]any{
			"err": err.Error(), "src": src, "dst": dst,
		})
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		m.Log("error", "copy error for file in cache", map[string]any{
			"err": err.Error(), "src": src, "dst": dst,
		})
		return err
	}
	if err = out.Close(); err != nil {
		m.Log("error", "copy error for file in cache", map[string]any{
			"err": err.Error(), "src": src, "dst": dst,
		})
		return err
	}
	if !m.OptimiseInDocker {
		// call the optimiser for the file too
		return m.OptimiseFile(src, dst)
	}
	return nil
}

// checkIfShouldCopy mirrors _checkIfShouldCopy.
func (m *Manager) checkIfShouldCopy(src string) (bool, error) {
	return !straceRegexp.MatchString(filepath.Base(src)), nil
}

// checkIfShouldArchive mirrors _checkIfShouldArchive.
func (m *Manager) checkIfShouldArchive(src string) (bool, error) {
	basename := filepath.Base(src)
	if straceRegexp.MatchString(basename) {
		return true, nil
	}
	if m.ArchiveLogs && (basename == "output.log" || basename == "output.blg") {
		return true, nil
	}
	return false, nil
}

// fileHidden mirrors _fileIsHidden.
func fileHidden(p string) bool {
	return fileHiddenRegexp.MatchString(p)
}
