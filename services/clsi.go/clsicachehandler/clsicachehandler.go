// Package clsicachehandler ports services/clsi/app/js/CLSICacheHandler.js
// (Go 1.24+ archive/tar, standard gzip).
//
// Parity notes (Node → Go):
//
//   - Node's module-level lastFailures map is per-Handler state here; the
//     production Handler used by the CLSI service is a singleton, matching.
//   - Node's notifyCLSICacheAboutBuild is fire-and-forget (Promise chain).
//     The Go port performs the same work synchronously and returns the
//     shard name ("" when none), as CompileController consumes it.
//   - fetch-utils' fetchStream/fetchNothing map to plain net/http calls:
//     non-2xx (including 3xx, per fetch-utils' ok = 200 <= status < 300)
//     yields RequestFailedError{Response, Body}; transport failures yield
//     RequestFailedError{Err}.
//   - Node gzip header bytes differ from Go's (MTIME/XFL/OS); both are
//     valid gzip streams and the consumer gunzips before reading.
//   - Node tar-fs pack emits exactly the listed entries' metadata (plus
//     recursively a directory entry's contents when a directory is
//     listed); the Go writer mirrors this. Long names may use PAX, which
//     the reader handles transparently.
//
// Testing seam: New() wires production behaviour; tests may override the
// Handler fields (fetch, config, clocks) directly.
package clsicachehandler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	cryptorand "crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	mathrand "math/rand"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"

	"clsi/config"
	"clsi/logger"
	"clsi/metrics"
	oerrors "clsi/errors"
	"clsi/outputcachemanager"
	"clsi/outputfilefinder"
	"clsi/resourcewriter"
)

const (
	// timeoutMs mirrors TIMEOUT.
	timeoutMs = 5_000
	// maxEntriesInOutputTar mirrors MAX_ENTRIES_IN_OUTPUT_TAR.
	maxEntriesInOutputTar = 100
	// maxBLGFiles mirrors MAX_BLG_FILES.
	maxBLGFiles = 50
	// maxEnqueueBodyBytes mirrors the 10_000_000 warn threshold in
	// enqueue.
	maxEnqueueBodyBytes = 10_000_000
)

var procStart = time.Now()

// objectIdRx mirrors OBJECT_ID_REGEX (/^[0-9a-f]{24}$/).
var objectIdRx = regexp.MustCompile(`^[0-9a-f]{24}$`)

// RequestFailedError mirrors @overleaf/fetch-utils RequestFailedError with
// the fields CLSI consumes:
//
//	response — the http.Response (for the err.response.status === 404 check)
//	body     — the body string
type RequestFailedError struct {
	Response *http.Response
	Body     string
	Err      error
}

func (e *RequestFailedError) Error() string {
	if e.Response != nil {
		urlStr := ""
		if e.Response.Request != nil {
			urlStr = e.Response.Request.URL.String()
		}
		return fmt.Sprintf("request failed: %d %s", e.Response.StatusCode, urlStr)
	}
	if e.Err != nil {
		return "request failed: " + e.Err.Error()
	}
	return "request failed"
}

func (e *RequestFailedError) Unwrap() error { return e.Err }

// defaultFetchNothing mirrors fetchNothing(url, {method, body, headers,
// timeout}): POST the body and discard the response.
func defaultFetchNothing(url string, body []byte) error {
	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return &RequestFailedError{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return &RequestFailedError{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return &RequestFailedError{Response: resp, Body: string(b)}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// defaultFetchStream mirrors fetchStream(url, {signal}): GET, return the
// response body reader.
func defaultFetchStream(u string) (io.ReadCloser, error) {
	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}
	resp, err := client.Get(u)
	if err != nil {
		return nil, &RequestFailedError{Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &RequestFailedError{Response: resp, Body: string(b)}
	}
	return resp.Body, nil
}

// Handler is the Go face of the Node CLSICacheHandler module.
type Handler struct {
	// Config returns the live CLSI config (defaults to config.Get).
	Config func() *config.Config

	// fetch seams (defaults wired by New).
	fetchNothing func(url string, body []byte) error
	fetchStream  func(url string) (io.ReadCloser, error)

	// nowMs mirrors Date.now(); perfMs mirrors performance.now();
	// randF mirrors Math.random().
	nowMs  func() int64
	perfMs func() float64
	randF  func() float64

	mu           sync.Mutex
	lastFailures map[string]float64
}

// New returns a Handler wired to the production config and HTTP client.
func New() *Handler {
	h := &Handler{Config: config.Get}
	h.wire()
	return h
}

// wire fills in nil seams with production defaults.
func (h *Handler) wire() {
	if h.Config == nil {
		h.Config = config.Get
	}
	if h.fetchNothing == nil {
		h.fetchNothing = defaultFetchNothing
	}
	if h.fetchStream == nil {
		h.fetchStream = defaultFetchStream
	}
	if h.nowMs == nil {
		h.nowMs = func() int64 { return time.Now().UnixMilli() }
	}
	if h.perfMs == nil {
		h.perfMs = func() float64 {
			return float64(time.Since(procStart).Nanoseconds()) / 1e6
		}
	}
	if h.randF == nil {
		h.randF = mathrand.Float64
	}
	if h.lastFailures == nil {
		h.lastFailures = map[string]float64{}
	}
}

var (
	defaultOnce sync.Once
	defaultH    *Handler
)

// Standard returns the shared production Handler.
func Standard() *Handler {
	defaultOnce.Do(func() { defaultH = New() })
	defaultH.wire()
	return defaultH
}

// NotifyOpts mirrors the opts of notifyCLSICacheAboutBuild.
type NotifyOpts struct {
	ProjectID string
	UserID    string
	BuildID   string
	EditorID  string
	// OutputFiles is the (rich) output file list.
	OutputFiles []outputfilefinder.OutputFile
	// CompileGroup mirrors opts.compileGroup.
	CompileGroup string
	Stats        map[string]any
	Timings      map[string]any
	Options      map[string]any
	// MetricsPath mirrors metricsOpts.path (user compiles have no metrics
	// path recorded here).
	MetricsPath string
}

// enqueueFile mirrors the lean file objects sent to the clsi-cache
// enqueue endpoint: {path} plus {size, contentId, ranges} for
// output.pdf.
type enqueueFile struct {
	Path      string                          `json:"path"`
	Size      *int64                          `json:"size,omitempty"`
	ContentID *string                         `json:"contentId,omitempty"`
	Ranges    []outputfilefinder.ContentRange `json:"ranges,omitempty"`
}

// enqueueBody mirrors the JSON POSTed to /enqueue.
type enqueueBody struct {
	ProjectID     string         `json:"projectId"`
	UserID        string         `json:"userId,omitempty"`
	BuildID       string         `json:"buildId"`
	EditorID      string         `json:"editorId,omitempty"`
	Files         []enqueueFile  `json:"files"`
	DownloadHost  string         `json:"downloadHost,omitempty"`
	ClsisServerID string         `json:"clsiServerId,omitempty"`
	CompileGroup  string         `json:"compileGroup,omitempty"`
	Stats         map[string]any `json:"stats,omitempty"`
	Timings       map[string]any `json:"timings,omitempty"`
	Options       map[string]any `json:"options,omitempty"`
}

// Notify ports notifyCLSICacheAboutBuild(opts) and returns the shard name
// ("" when no shard is available).
//
// Node does the enqueue/tarball/history work through Promise chains that
// resolve after the HTTP response is sent; the Go port does it
// synchronously. Callers (CompileController) only consume the returned
// shard name.
func Notify(opts NotifyOpts) string {
	shard := Standard().notify(opts)
	if shard == nil {
		return ""
	}
	return shard.Shard
}

// DownloadLatestCompileCache ports downloadLatestCompileCache.
func DownloadLatestCompileCache(projectID, userID, compileDir string) (bool, error) {
	return Standard().downloadLatestCompileCache(projectID, userID, compileDir)
}

// DownloadOutputDotSynctexFromCompileCache ports
// downloadOutputDotSynctexFromCompileCache.
func DownloadOutputDotSynctexFromCompileCache(projectID, userID, editorID, buildID, outputDir string) (bool, error) {
	return Standard().downloadOutputDotSynctexFromCompileCache(projectID, userID, editorID, buildID, outputDir)
}

// DownloadHistorySnapshot ports downloadHistorySnapshot.
func DownloadHistorySnapshot(projectID, userID, cacheDir string) (bool, error) {
	return Standard().downloadHistorySnapshot(projectID, userID, cacheDir)
}

// getAvailableShard ports getAvailableShard(projectId).
func (h *Handler) getAvailableShard(projectID string) *config.ClsiCacheShard {
	c := h.Config()
	all := c.APIs.OutputCache.Shards
	var shards []config.ClsiCacheShard
	// Node: shards.slice(0, currentShards) with currentShards NaN (env unset)
	// yields an empty list; Go's 0 default maps to the same. cs > len clamps
	// to the full list (JS slice semantics).
	if cs := c.APIs.OutputCache.CurrentShards; cs > 0 {
		end := cs
		if end > len(all) {
			end = len(all)
		}
		shards = all[:end]
	}

	b, hexErr := hex.DecodeString(projectID)
	var counter uint32
	if hexErr == nil && len(b) >= 12 {
		counter = binary.BigEndian.Uint32(b[8:12])
	}

	now := float64(h.nowMs())
	from := c.APIs.OutputCache.ReshardFrom
	if from != nil {
		until := c.APIs.OutputCache.ReshardUntil
		if until != nil {
			fromMs := float64(from.UnixNano()) / 1e6
			untilMs := float64(until.UnixNano()) / 1e6
			if now > fromMs {
				lhs := float64(counter%100) / 100
				rhs := (untilMs - now) / (untilMs - fromMs)
				if lhs > rhs {
					ds := c.APIs.OutputCache.DesiredShards
					shards = nil
					if ds > 0 {
						end := ds
						if end > len(all) {
							end = len(all)
						}
						shards = all[:end]
					}
				}
			}
		}
	}

	i := 0
	for len(shards) > 0 {
		seed := crc32.ChecksumIEEE([]byte(projectID + "-" + strconv.Itoa(i)))
		idx := int(seed % uint32(len(shards)))
		i++
		cand := shards[idx]
		if !h.isCircuitBreakerTripped(cand.URL) {
			return &cand
		}
		shards = append(shards[:idx], shards[idx+1:]...)
	}
	return nil
}

// isCircuitBreakerTripped ports isCircuitBreakerTripped(url).
func (h *Handler) isCircuitBreakerTripped(url string) bool {
	h.mu.Lock()
	last, ok := h.lastFailures[url]
	h.mu.Unlock()
	if !ok || last == 0 {
		return false
	}
	// Circuit breaker that avoids retries for 5-20s.
	retryDelay := float64(timeoutMs) * (1 + 3*h.randF())
	return h.perfMs()-last < retryDelay
}

func (h *Handler) tripCircuitBreaker(url string) {
	h.mu.Lock()
	h.lastFailures[url] = h.perfMs()
	h.mu.Unlock()
}

func (h *Handler) closeCircuitBreaker(url string) {
	h.mu.Lock()
	delete(h.lastFailures, url)
	h.mu.Unlock()
}

// getOutputDir ports getOutputDir.
func (h *Handler) getOutputDir(projectID, userID, buildID string) string {
	c := h.Config()
	project := projectID
	if userID != "" {
		project = projectID + "-" + userID
	}
	return filepath.Join(c.Path.OutputDir, project, outputcachemanager.CacheSubdir, buildID)
}

// notify ports notifyCLSICacheAboutBuild (synchronously; see package docs).
func (h *Handler) notify(o NotifyOpts) *config.ClsiCacheShard {
	c := h.Config()
	if !c.APIs.OutputCache.Enabled {
		return nil
	}
	if !objectIdRx.MatchString(o.ProjectID) {
		return nil
	}
	shard := h.getAvailableShard(o.ProjectID)
	if shard == nil {
		return nil
	}

	isUserCompile := o.MetricsPath == ""
	if !(isUserCompile && o.CompileGroup == "standard") {
		// PDF preview, skipped only for standard-group user compiles.
		var preview []enqueueFile
		for _, f := range o.OutputFiles {
			if f.Path == "output.pdf" || f.Path == "output.log" || f.Path == "output.synctex.gz" {
				preview = append(preview, h.lean(f))
			}
		}
		var blg []outputfilefinder.OutputFile
		for _, f := range o.OutputFiles {
			if hasDotBLG(f.Path) {
				blg = append(blg, f)
			}
		}
		if len(blg) > maxBLGFiles {
			blg = blg[:maxBLGFiles]
		}
		for _, f := range blg {
			preview = append(preview, enqueueFile{Path: f.Path})
		}
		h.enqueue(shard, o, preview)
	}

	// Compile cache.
	if err := h.buildTarball(o.ProjectID, o.UserID, o.BuildID, o.OutputFiles); err != nil {
		if !isENOENT(err) {
			logger.Warn(
				map[string]any{
					"err":       err,
					"projectId": o.ProjectID,
					"userId":    o.UserID,
					"buildId":   o.BuildID,
					"shard":     shard.Shard,
				},
				"build output.tar.gz for clsi-cache failed",
			)
		}
	} else {
		h.enqueue(shard, o, []enqueueFile{{Path: "output.tar.gz"}})
	}

	// History snapshot.
	if err := h.copyHistorySnapshot(o.ProjectID, o.UserID, o.BuildID); err != nil {
		if !isENOENT(err) {
			logger.Warn(
				map[string]any{
					"err":       err,
					"projectId": o.ProjectID,
					"userId":    o.UserID,
					"buildId":   o.BuildID,
					"shard":     shard.Shard,
				},
				"copy history-resync.json.gz for clsi-cache failed",
			)
		}
	} else {
		h.enqueue(shard, o, []enqueueFile{{Path: "history-resync.json.gz"}})
	}

	return shard
}

// lean ports the `_.pick(f, ...)` mapping of outputFiles in notify.
func (h *Handler) lean(f outputfilefinder.OutputFile) enqueueFile {
	lean := enqueueFile{Path: f.Path}
	if f.Path == "output.pdf" {
		lean.Size = f.Size
		lean.ContentID = f.ContentID
		lean.Ranges = f.Ranges
	}
	return lean
}

// hasDotBLG mirrors `f.path.endsWith('.blg')`.
func hasDotBLG(p string) bool {
	return len(p) >= 4 && p[len(p)-4:] == ".blg"
}

// buildTarball ports buildTarball (promise form); the timer done runs in
// the finally block (Node: `timer.done()` after the try/catch).
func (h *Handler) buildTarball(projectID, userID, buildID string, files []outputfilefinder.OutputFile) error {
	timer := metrics.NewTimer("clsi_cache_build")
	outputDir := h.getOutputDir(projectID, userID, buildID)

	filtered := make([]string, 0, len(files))
	for _, f := range files {
		if !resourcewriter.IsExtraneousFile(f.Path) {
			filtered = append(filtered, f.Path)
		}
	}
	if len(filtered) > maxEntriesInOutputTar {
		// Node uses Metrics.inc here, which counts a single unit. The throw is
		// before the try/finally, so the timer does NOT finish here.
		metrics.Count("clsi_cache_build_too_many_entries", 1)
		return oerrors.NewOError("too many output files for output.tar.gz", map[string]any{
			"nFiles": len(filtered),
		})
	}
	metrics.Count("clsi_cache_build_files", len(filtered))

	dst := filepath.Join(outputDir, "output.tar.gz")
	if err := writeTarGzip(dst, outputDir, filtered); err != nil {
		// Node: unlink the partial file on error, then rethrow.
		_ = os.Remove(dst)
		timer.Done(map[string]any{"status": "error"})
		return err
	}
	timer.Done(map[string]any{"status": "success"})
	return nil
}

// copyHistorySnapshot ports copyHistorySnapshot.
func (h *Handler) copyHistorySnapshot(projectID, userID, buildID string) error {
	c := h.Config()
	project := projectID
	if userID != "" {
		project = projectID + "-" + userID
	}
	src := filepath.Join(c.Path.ClsiCacheDir, project, "history.json.gz")
	dst := filepath.Join(h.getOutputDir(projectID, userID, buildID), "history-resync.json.gz")
	data, rerr := os.ReadFile(src)
	if rerr != nil {
		return rerr
	}
	srcInfo, serr := os.Stat(src)
	if serr != nil {
		return serr
	}
	if merr := os.MkdirAll(filepath.Dir(dst), 0o755); merr != nil {
		return merr
	}
	// Node fs.promises.cp preserves the source mode.
	return os.WriteFile(dst, data, srcInfo.Mode().Perm())
}

// downloadOutputDotSynctexFromCompileCache ports the Node function.
func (h *Handler) downloadOutputDotSynctexFromCompileCache(projectID, userID, editorID, buildID, outputDir string) (bool, error) {
	requestPath := "/project/" + projectID + "/"
	if userID != "" {
		requestPath += "user/" + userID + "/"
	}
	requestPath += "build/" + editorID + "-" + buildID + "/search/output/output.synctex.gz"
	return h.downloadSingleFile(projectID, requestPath, outputDir, "synctex")
}

// downloadHistorySnapshot ports the Node function.
func (h *Handler) downloadHistorySnapshot(projectID, userID, cacheDir string) (bool, error) {
	requestPath := "/project/" + projectID + "/"
	if userID != "" {
		requestPath += "user/" + userID + "/"
	}
	requestPath += "latest/output/history-resync.json.gz"
	return h.downloadSingleFile(projectID, requestPath, cacheDir, "snapshot")
}

// statusOfRequestFailed extracts the response status from a
// RequestFailedError (0 when absent).
func statusOfRequestFailed(err error) int {
	var rfe *RequestFailedError
	if errors.As(err, &rfe) && rfe.Response != nil {
		return rfe.Response.StatusCode
	}
	return 0
}

// downloadSingleFile ports downloadSingleFile.
func (h *Handler) downloadSingleFile(projectID, requestPath, outputDir, label string) (bool, error) {
	c := h.Config()
	if !c.APIs.OutputCache.Enabled {
		return false, nil
	}
	if !objectIdRx.MatchString(projectID) {
		return false, nil
	}
	shard := h.getAvailableShard(projectID)
	if shard == nil {
		return false, nil
	}

	u, perr := url.Parse(shard.URL)
	if perr != nil {
		return false, oerrors.NewOError("invalid clsi-cache shard url", map[string]any{
			"url":    shard.URL,
			"shard":  shard.Shard,
			"method": label,
		})
	}
	u.Path = requestPath
	u.RawPath = ""

	timer := metrics.NewTimer("clsi_cache_download")
	stream, ferr := h.fetchStream(u.String())
	if ferr != nil {
		if statusOfRequestFailed(ferr) == http.StatusNotFound {
			h.closeCircuitBreaker(shard.URL)
			timer.Done(map[string]any{"method": label, "status": "not-found"})
			return false, nil
		}
		h.tripCircuitBreaker(shard.URL)
		timer.Done(map[string]any{"method": label, "status": "error"})
		return false, oerrors.Tag(ferr, "download failed", map[string]any{"shard": shard.Shard})
	}

	if merr := os.MkdirAll(outputDir, 0o755); merr != nil {
		stream.Close()
		return false, merr // Node: not wrapped, propagates raw.
	}
	name := path.Base(requestPath)
	dst := filepath.Join(outputDir, name)
	tmp := dst + uuidV4()
	f, oerr := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if oerr != nil {
		if isENOENT(oerr) {
			stream.Close()
			return false, nil
		}
		h.tripCircuitBreaker(shard.URL)
		stream.Close()
		timer.Done(map[string]any{"method": label, "status": "error"})
		return false, oerrors.Tag(oerr, "stream failed", map[string]any{"shard": shard.Shard})
	}
	if _, cerr := copyMetered(f, stream); cerr != nil {
		f.Close()
		stream.Close()
		if isENOENT(cerr) {
			return false, nil
		}
		h.tripCircuitBreaker(shard.URL)
		_ = os.Remove(tmp)
		timer.Done(map[string]any{"method": label, "status": "error"})
		return false, oerrors.Tag(cerr, "stream failed", map[string]any{"shard": shard.Shard})
	}
	if rerr := os.Rename(tmp, dst); rerr != nil {
		if isENOENT(rerr) {
			stream.Close()
			return false, nil
		}
		stream.Close()
		h.tripCircuitBreaker(shard.URL)
		_ = os.Remove(tmp)
		timer.Done(map[string]any{"method": label, "status": "error"})
		return false, oerrors.Tag(rerr, "stream failed", map[string]any{"shard": shard.Shard})
	}
	f.Close()
	stream.Close()
	h.closeCircuitBreaker(shard.URL)
	timer.Done(map[string]any{"method": label, "status": "success"})
	return true, nil
}

// copyMetered copies src to dst, counting bytes into Metrics.count
// (mirrors MeteredStream's per-chunk count).
func copyMetered(dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32 * 1024)
	total := int64(0)
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			metrics.Count("clsi_cache_egress", n)
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			total += int64(n)
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return total, rerr
		}
	}
	return total, nil
}

// downloadLatestCompileCache ports downloadLatestCompileCache.
func (h *Handler) downloadLatestCompileCache(projectID, userID, compileDir string) (bool, error) {
	c := h.Config()
	if !c.APIs.OutputCache.Enabled {
		return false, nil
	}
	if !objectIdRx.MatchString(projectID) {
		return false, nil
	}
	shard := h.getAvailableShard(projectID)
	if shard == nil {
		return false, nil
	}

	timer := metrics.NewTimer("clsi_cache_download")
	requestURL := shard.URL + "/project/" + projectID + "/"
	if userID != "" {
		requestURL += "user/" + userID + "/"
	}
	requestURL += "latest/output/output.tar.gz"

	stream, ferr := h.fetchStream(requestURL)
	if ferr != nil {
		if statusOfRequestFailed(ferr) == http.StatusNotFound {
			h.closeCircuitBreaker(shard.URL)
			timer.Done(map[string]any{"method": "tar", "status": "not-found"})
			return false, nil
		}
		h.tripCircuitBreaker(shard.URL)
		timer.Done(map[string]any{"method": "tar", "status": "error"})
		return false, oerrors.Tag(ferr, "download failed", map[string]any{"shard": shard.Shard})
	}

	n, abort, err := extractCompileCache(stream, compileDir, projectID, userID)
	stream.Close()
	if err != nil {
		if isENOENT(err) {
			return false, nil
		}
		h.tripCircuitBreaker(shard.URL)
		timer.Done(map[string]any{"method": "tar", "status": "error"})
		return false, oerrors.Tag(err, "stream failed", map[string]any{"shard": shard.Shard})
	}
	h.closeCircuitBreaker(shard.URL)
	metrics.Count("clsi_cache_download_entries", n)
	timer.Done(map[string]any{"method": "tar", "status": "success"})
	return !abort, nil
}

// extractCompileCache gunzips and extracts the compile-cache tarball into
// compileDir, applying the Node `ignore` hook semantics: count entries
// (files + folders) and abort (skip writes) above
// MAX_ENTRIES_IN_OUTPUT_TAR or on an unexpected entry type.
func extractCompileCache(stream io.Reader, compileDir, projectID, userID string) (int, bool, error) {
	metered := &meteredReader{inner: stream}

	gz, gerr := gzip.NewReader(metered)
	if gerr != nil {
		return 0, true, gerr
	}
	tr := tar.NewReader(gz)
	n := 0
	abort := false
	for {
		hdr, rerr := tr.Next()
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return n, abort, rerr
		}
		if !abort {
			n++
			ttype := tarEntryType(hdr)
			if n > maxEntriesInOutputTar {
				abort = true
				logger.Warn(
					map[string]any{"projectId": projectID, "userId": userID, "compileDir": compileDir},
					"too many entries in tar-ball from clsi-cache",
				)
			} else if ttype != "file" && ttype != "directory" {
				abort = true
				logger.Warn(
					map[string]any{
						"projectId":  projectID,
						"userId":     userID,
						"compileDir": compileDir,
						"entryType":  ttype,
					},
					"unexpected entry in tar-ball from clsi-cache",
				)
			}
		}
		if abort {
			continue // `ignore` returned true: skip writing this entry.
		}
		if werr := writeTarEntry(compileDir, tr, hdr); werr != nil {
			return n, abort, werr
		}
	}
	return n, abort, nil
}

// meteredReader wraps stream, counting Read bytes into the
// clsi_cache_egress metric (mirrors MeteredStream).
type meteredReader struct {
	inner  io.Reader
	egress int64
}

func (m *meteredReader) Read(p []byte) (int, error) {
	n, err := m.inner.Read(p)
	m.egress += int64(n)
	if n > 0 {
		metrics.Count("clsi_cache_egress", n)
	}
	return n, err
}

// writeTarEntry writes one extracted entry into compileDir (file,
// directory or symlink, mirroring the tar-fs extract shapes CLSI
// produces).
func writeTarEntry(compileDir string, tr *tar.Reader, hdr *tar.Header) error {
	name := filepath.Join(compileDir, hdr.Name)
	switch hdr.Typeflag {
	case tar.TypeDir:
		if merr := os.MkdirAll(name, 0o755); merr != nil {
			return merr
		}
		if mtime := hdr.ModTime; !mtime.IsZero() {
			_ = os.Chtimes(name, mtime, mtime)
		}
		return nil
	case tar.TypeSymlink:
		if merr := os.MkdirAll(filepath.Dir(name), 0o755); merr != nil {
			return merr
		}
		if rerr := os.Remove(name); rerr != nil && !os.IsNotExist(rerr) {
			return rerr
		}
		return os.Symlink(hdr.Linkname, name)
	case tar.TypeReg, tar.TypeRegA:
		if merr := os.MkdirAll(filepath.Dir(name), 0o755); merr != nil {
			return merr
		}
		f, oerr := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if oerr != nil {
			return oerr
		}
		if _, cerr := io.Copy(f, tr); cerr != nil {
			f.Close()
			return cerr
		}
		if cerr := f.Close(); cerr != nil {
			return cerr
		}
		if mtime := hdr.ModTime; !mtime.IsZero() {
			_ = os.Chtimes(name, mtime, mtime)
		}
		return nil
	default:
		// Not expected after the abort hook; skip defensively.
		return nil
	}
}

// tarEntryType maps an archive/tar header to the tar-stream `type` string
// so warn logs and the abort hook match Node.
func tarEntryType(hdr *tar.Header) string {
	switch hdr.Typeflag {
	case tar.TypeReg, tar.TypeRegA:
		return "file"
	case '1':
		return "link"
	case '2':
		return "symlink"
	case '3':
		return "character-device"
	case '4':
		return "block-device"
	case '5':
		return "directory"
	case '6':
		return "fifo"
	case '7':
		return "contiguous-file"
	case 'P':
		return "pax-header"
	case 'g':
		return "pax-global-header"
	default:
		return "unknown"
	}
}

// enqueue ports the internal enqueue(files) callback.
func (h *Handler) enqueue(shard *config.ClsiCacheShard, o NotifyOpts, files []enqueueFile) {
	c := h.Config()

	if files == nil {
		files = []enqueueFile{}
	}
	body := enqueueBody{
		ProjectID:     o.ProjectID,
		UserID:        o.UserID,
		BuildID:       o.BuildID,
		EditorID:      o.EditorID,
		Files:         files,
		DownloadHost:  c.APIs.Compile.DownloadHost,
		ClsisServerID: c.APIs.Compile.ServerID,
		CompileGroup:  o.CompileGroup,
		Stats:         o.Stats,
		Timings:       o.Timings,
		Options:       o.Options,
	}
	raw, merr := json.Marshal(&body)
	if merr != nil {
		return
	}
	if len(raw) > maxEnqueueBodyBytes {
		outputPDFSize := 0
		nPDFCachingRanges := 0
		for _, f := range files {
			if f.Path == "output.pdf" {
				sz, _ := json.Marshal(f)
				outputPDFSize = len(sz)
				nPDFCachingRanges = len(f.Ranges)
			}
		}
		logger.Warn(
			map[string]any{
				"projectId":         o.ProjectID,
				"userId":            o.UserID,
				"bodySize":          len(raw),
				"nFiles":            len(files),
				"outputPDFSize":     outputPDFSize,
				"nPDFCachingRanges": nPDFCachingRanges,
			},
			"large clsi-cache request",
		)
	}
	metrics.Count("clsi_cache_enqueue_files", len(files))
	ferr := h.fetchNothing(shard.URL+"/enqueue", raw)
	if ferr != nil {
		h.tripCircuitBreaker(shard.URL)
		logger.Warn(
			map[string]any{
				"err":       ferr,
				"projectId": o.ProjectID,
				"userId":    o.UserID,
				"buildId":   o.BuildID,
				"shard":     shard.Shard,
			},
			"enqueue for clsi-cache failed",
		)
	} else {
		h.closeCircuitBreaker(shard.URL)
	}
}

// writeTarGzip packs `files` (paths relative to srcDir) into a gzip'd tar
// at dst, mirroring tar-fs pack({entries}) + zlib createGzip: file-only
// entries (directories are recursed), in list order, missing listed
// entries raise ENOENT, and the dest is unlinked on error.
func writeTarGzip(dst, srcDir string, files []string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	var done bool
	defer func() {
		if !done {
			_ = f.Close()
			_ = os.Remove(dst)
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if werr := packEntries(tw, srcDir, files); werr != nil {
		return werr
	}
	if cerr := tw.Close(); cerr != nil {
		return cerr
	}
	if gerr := gz.Close(); gerr != nil {
		return gerr
	}
	done = true
	return f.Close()
}

// packEntries writes headers for each path relative to dir, in order.
// For directory entries, it recurses (matching tar-fs pack({entries})
// behaviour: a listed directory contributes its entry plus its children).
func packEntries(tw *tar.Writer, dir string, names []string) error {
	for _, name := range names {
		abs := filepath.Join(dir, name)
		st, serr := os.Stat(abs)
		if serr != nil {
			return serr // ENOENT propagates (Node: listed entries must exist).
		}
		stt, _ := st.Sys().(*syscall.Stat_t)
		uid, gid := uint32(0), uint32(0)
		if stt != nil {
			uid, gid = stt.Uid, stt.Gid
		}
		if st.IsDir() {
			hdr := &tar.Header{
				Name:     name + "/",
				Typeflag: tar.TypeDir,
				Mode:     int64(0o755),
				Uid:      int(uid),
				Gid:      int(gid),
			}
			if werr := tw.WriteHeader(hdr); werr != nil {
				return werr
			}
			entries, rerr := os.ReadDir(abs)
			if rerr != nil {
				return rerr
			}
			sub := make([]string, len(entries))
			for i, e := range entries {
				sub[i] = name + "/" + e.Name()
			}
			// Recurse against the SAME base dir; child names stay
			// relative to the original dir so fs lookups and tar entry
			// names both use the full relative path.
			if err := packEntries(tw, dir, sub); err != nil {
				return err
			}
			continue
		}
		hdr := &tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Size:     st.Size(),
			Mode:     int64(0o644),
			ModTime:  st.ModTime(),
			Uid:      int(uid),
			Gid:      int(gid),
		}
		if werr := tw.WriteHeader(hdr); werr != nil {
			return werr
		}
		data, rerr := os.ReadFile(abs)
		if rerr != nil {
			return rerr
		}
		if _, werr := tw.Write(data); werr != nil {
			return werr
		}
	}
	return nil
}

// isENOENT mirrors isENOENT(err) (err.code === 'ENOENT').
func isENOENT(err error) bool {
	return os.IsNotExist(err)
}

// uuidV4 generates a crypto-random v4 UUID (mirrors crypto.randomUUID()).
func uuidV4() string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
