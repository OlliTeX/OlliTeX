// Package metrics ports the observability surface used by the CLSI Go port.
//
// Node parity notes:
//
// Node uses @overleaf/metrics + prom. Each counter/gauge below maps 1:1 to
// the metric name incremented or set by its Node source file. The prom
// histograms (durations) are modelled here as plain float64 histograms over
// second-value buckets, because their exact prometheus wiring is out of
// scope for the port until the server route lands (which owns exposure).
package metrics

import (
	"sync"
	"time"
)

// Counter is a monotonically increasing counter (prom style).
type Counter struct {
	Name  string
	Value int64
	Mu    sync.Mutex
}

// Gauge is a point-in-time value (prom style).
type Gauge struct {
	Name  string
	Value float64
	Mu    sync.Mutex
}

var (
	ConcurrentCompileRequests        = &Gauge{Name: "concurrent_compile_requests"}
	CompileLockExpired               = &Counter{Name: "compile_lock_expired"}
	CompileLockReleasedTwice         = &Counter{Name: "compile_lock_released_twice"}
	CompileLockExpiredBeforeRelease  = &Counter{Name: "compile_lock_expired_before_release"}
	ExceededCompilerConcurrencyLimit = &Counter{Name: "exceeded-compilier-concurrency-limit"}
	PdfCachingStatus                 = &Counter{Name: "pdf-caching-status"}
)

// GaugeConcurrentCompileRequests mirrors
// Metrics.gauge('concurrent_compile_requests', LOCKS.size) in LockManager.js.
func GaugeConcurrentCompileRequests(size int) {
	ConcurrentCompileRequests.Set(float64(size))
}

// IncCompileLockExpiredBeforeRelease mirrors
// Metrics.inc('compile_lock_expired_before_release') in Lock.release().
func IncCompileLockExpiredBeforeRelease() { CompileLockExpiredBeforeRelease.Inc() }

// IncExceededCompilerConcurrencyLimit mirrors
// Metrics.inc('exceeded-compilier-concurrency-limit') (sic — upstream typo,
// verbatim) in checkConcurrencyLimit().
func IncExceededCompilerConcurrencyLimit() { ExceededCompilerConcurrencyLimit.Inc() }

// IncCompileLockReleasedTwice mirrors the logger.error 'Lock was released
// twice' path in Lock.release() (Node logs; we expose as a counter too).
func IncCompileLockReleasedTwice() { CompileLockReleasedTwice.Inc() }

// IncCompileLockExpired mirrors LockManager.acquire's expired-lock path
// (Node emits logger.warn {key}, 'Compile lock expired'; counted here).
func IncCompileLockExpired() { CompileLockExpired.Inc() }

// ShouldSkipMetrics mirrors Metrics.shouldSkipMetrics(request):
// returns true for the clsi-perf / health-check / clsi-cache-template paths.
func ShouldSkipMetrics(metricsPath string) bool {
	return metricsPath == "clsi-perf" || metricsPath == "health-check" || metricsPath == "clsi-cache-template"
}

// Set / Inc implement the tiny surface metrics needs without pulling in a
// prometheus dependency.
func (g *Gauge) Set(v float64) {
	g.Mu.Lock()
	g.Value = v
	g.Mu.Unlock()
}

// Get returns the current gauge value (used by tests and by the server
// health endpoint).
func (g *Gauge) Get() float64 {
	g.Mu.Lock()
	defer g.Mu.Unlock()
	return g.Value
}

// Inc increments a counter by 1.
func (c *Counter) Inc() { c.Add(1) }

// Add increments a counter by n.
func (c *Counter) Add(n int64) {
	c.Mu.Lock()
	c.Value += n
	c.Mu.Unlock()
}

// Get returns the current counter value (used by tests and the server).
func (c *Counter) Get() int64 {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	return c.Value
}

// Timer mirrors new Metrics.Timer(name, sampleRate, labels) from the vendorized
// @overleaf/metrics (a prom summary timer). Only its observable contract is
// needed: Timer(name, sampleRate, labels) + Done(labels) (a no-op when the
// request is exempt from metrics; the port keeps no prometheus plumbing).
type Timer struct {
	Name     string
	start    time.Time
	finished bool
}

var (
	downloadFailedMu sync.Mutex
	downloadFailed   int64
)

// NewTimer mirrors new Metrics.Timer(name, sampleRate, labels).
func NewTimer(name string) *Timer {
	return &Timer{Name: name, start: time.Now()}
}

// Done mirrors timer.done(labels).
func (t *Timer) Done(labels ...map[string]interface{}) { t.finished = true }

// Finished reports whether Done has been called (test/observability aid).
func (t *Timer) Finished() bool { return t.finished }

// DownloadFailed mirrors Metrics.inc('download-failed').
var DownloadFailed *Counter

func init() {
	DownloadFailed = &Counter{Name: "download-failed"}
}

// IncDownloadFailed mirrors Metrics.inc('download-failed').
func IncDownloadFailed() { DownloadFailed.Inc() }

// Png2pdfSkippedSmall mirrors the @overleaf/metrics inc('png2pdf-skipped-small')
// call in HistoryResourceWriter (a small slow-PNG not worth converting).
var Png2pdfSkippedSmall *Counter

func init() { Png2pdfSkippedSmall = &Counter{Name: "png2pdf-skipped-small"} }

// IncPng2pdfSkippedSmall mirrors Metrics.inc('png2pdf-skipped-small').
func IncPng2pdfSkippedSmall() { Png2pdfSkippedSmall.Inc() }

// Histogram observations (Node ClsiMetrics.snapshotApplyAllDurationSeconds /
// snapshotLoadEagerDurationSeconds prom histograms). The port keeps one
// named counter per histogram (same as Count); the prometheus exposure
// arrives with the /metrics endpoint.
type Histogram struct {
	Name     string
	Observed int64
	SumSecs  float64
	Mu       sync.Mutex
	// ObservedBy tracks per-label observations: key = "group\x00source".
	// Prometheus labels; kept so the later /metrics exposure and tests can
	// assert the right (group, source) bucket was hit.
	ObservedBy map[string]int64
}

func (h *Histogram) Observe(group, source string, seconds float64) {
	h.Mu.Lock()
	h.Observed++
	h.SumSecs += seconds
	if h.ObservedBy == nil {
		h.ObservedBy = map[string]int64{}
	}
	h.ObservedBy[group+"\u0000"+source]++
	h.Mu.Unlock()
}

func (h *Histogram) ObservedCount(group, source string) int64 {
	h.Mu.Lock()
	defer h.Mu.Unlock()
	return h.ObservedBy[group+"\u0000"+source]
}

// SnapshotApplyAllDurationSeconds / SnapshotLoadEagerDurationSeconds port the
// two CLSI prom histograms observed in syncResourcesToDisk.
var (
	SnapshotApplyAllDurationSeconds  = &Histogram{Name: "clsi_snapshot_applyAll_duration_seconds"}
	SnapshotLoadEagerDurationSeconds = &Histogram{Name: "clsi_snapshot_loadEager_duration_seconds"}
)

func (h *Histogram) Get() (observed int64, sumSecs float64) {
	h.Mu.Lock()
	defer h.Mu.Unlock()
	return h.Observed, h.SumSecs
}

// ShouldSkipTimer mirrors the port of shouldSkipMetrics for timers:
// returns true if the request is a health/perf path. Mirrors the semantics
// of the node shouldSkipFlags(request) used around Metrics.Timer.
func ShouldSkipTimer(request interface{ MetricsPath() string }) bool {
	if request == nil {
		return false
	}
	return ShouldSkipMetrics(request.MetricsPath())
}

// Count mirrors Metrics.count(name, n): bump a named counter by n. For
// observability only — no Prometheus plumbing; kept as an exported seam so a
// future /metrics endpoint can read it. The port uses a single shared counter
// keyed by the prom metric name, since CLSI only observes 'png2pdf-converted'
// and 'download-failed' style scalars.
func Count(name string, n int) {
	countMu.Lock()
	c := countersByName[name]
	if c == nil {
		c = &Counter{Name: name}
		countersByName[name] = c
	}
	countMu.Unlock()
	c.Add(int64(n))
}

var (
	countMu        sync.Mutex
	countersByName = map[string]*Counter{}
)

// NamedCounter returns the *Counter backing Metrics.count(name, n) (test/inspect aid).
func NamedCounter(name string) *Counter {
	countMu.Lock()
	defer countMu.Unlock()
	return countersByName[name]
}

// Done returns the elapsed ms since the Timer was created (Node timer.done()
// returns the elapsed time and emits a prom histogram label). The caller (e.g.
// png2pdf) records it into its timings map.
func (t *Timer) DoneMS() int64 {
	t.finished = true
	return time.Since(t.start).Milliseconds()
}
