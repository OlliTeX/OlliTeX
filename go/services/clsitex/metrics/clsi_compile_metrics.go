package metrics

import "sync"

// Compile-time buckets (Node Metrics.js COMPILE_TIME_BUCKETS).
const compileTimeBuckets = "0.5,1,1.5,2,3,4,5,6,8,10,15,20,25,30,45,60,75,90,120,150,180,210,240"

// CompilesTotal mirrors ClsiMetrics.compilesTotal (prom.Counter).
var CompilesTotal = NewLabeledCounter(
	"clsi_compiles_total",
	"status", "engine", "compile", "group", "image",
	"draft", "stop_on_first_error", "passes", "type", "png2pdf",
)

// CompileDurationSeconds mirrors ClsiMetrics.compileDurationSeconds
// (prom.Histogram, the latexmkrc invocation duration).
var CompileDurationSeconds = NewLabeledHistogram(
	"clsi_compile_duration_seconds",
	"status", "engine", "compile", "group", "passes",
)

// E2ECompileDurationSeconds mirrors ClsiMetrics.e2eCompileDurationSeconds
// (prom.Histogram, the entire compile request: sync, latexmk, output).
var E2ECompileDurationSeconds = NewLabeledHistogram(
	"clsi_e2e_compile_duration_seconds",
	"compile", "group", "compileFromHistory",
)

// E2ECompileDurationClsiPerfSeconds mirrors
// ClsiMetrics.e2eCompileDurationClsiPerfSeconds (prom.Gauge).
var E2ECompileDurationClsiPerfSeconds = NewLabeledGauge(
	"clsi_e2e_compile_duration_clsi_perf_seconds",
	"compile", "variant",
)

// SyncResourcesDurationSeconds mirrors ClsiMetrics.syncResourcesDurationSeconds
// (prom.Histogram).
var SyncResourcesDurationSeconds = NewLabeledHistogram(
	"clsi_sync_resources_duration_seconds",
	"type", "compile", "group",
)

// ProcessOutputFilesDurationSeconds mirrors
// ClsiMetrics.processOutputFilesDurationSeconds (prom.Histogram).
var ProcessOutputFilesDurationSeconds = NewLabeledHistogram(
	"clsi_process_output_files_duration_seconds",
	"compile", "group",
)

// LatexmkRuleDurationSeconds mirrors ClsiMetrics.latexmkRuleDurationSeconds
// (prom.Histogram).
var LatexmkRuleDurationSeconds = NewLabeledHistogram(
	"clsi_latexmk_rule_duration_seconds",
	"group", "rule",
)

// ImageProcessingDurationSeconds mirrors
// ClsiMetrics.imageProcessingDurationSeconds (prom.Histogram).
var ImageProcessingDurationSeconds = NewLabeledHistogram(
	"clsi_image_processing_duration_seconds",
	"group", "type",
)

// --- Generic named bridges (prometheus exposure arrives with the server).
// These back ContentCacheMetrics.SeamMetrics (pdf-bandwidth, compute-
// pdf-caching, ...) and any other Metrics.summary/timing/inc name that does
// not map to a fixed labeled metric above.

type genericPoint struct {
	count int64
	sum   float64
}

var (
	genericMu   sync.Mutex
	genericHist = map[string]*genericPoint{}
)

// GenericObserve records a summary/timing-style observation of `value`
// under `name` (backing Metrics.summary / Metrics.timing in the port).
func GenericObserve(name string, value float64) {
	genericMu.Lock()
	defer genericMu.Unlock()
	p := genericHist[name]
	if p == nil {
		p = &genericPoint{}
		genericHist[name] = p
	}
	p.count++
	p.sum += value
}

// GenericInc records a Metrics.inc (count) under `name` by n.
func GenericInc(name string, n int) { Count(name, n) }

// GenericObserved returns (count, sum) recorded for `name`.
func GenericObserved(name string) (int64, float64) {
	genericMu.Lock()
	defer genericMu.Unlock()
	p := genericHist[name]
	if p == nil {
		return 0, 0
	}
	return p.count, p.sum
}

// GenericReset is a test helper to clear the generic records between tests.
func GenericReset() {
	genericMu.Lock()
	defer genericMu.Unlock()
	genericHist = map[string]*genericPoint{}
}
