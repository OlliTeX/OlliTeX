// Package contentcachemetrics ports services/clsi/app/js/ContentCacheMetrics.js
// (146L).
//
// Node source (verbatim from the repo):
//
//	export default { emitPdfStats }
//
//	function emitPdfStats(stats, timings, request) {
//	  if (timings['compute-pdf-caching']) {
//	    emitPdfCachingStats(stats, timings, request)
//	  } else {
//	    Metrics.summary('pdf-bandwidth', stats['pdf-size'], request.metricsOpts)
//	  }
//	}
//
//	function emitPdfCachingStats(stats, timings, request) { ... }
//
//	function getSystemLoad() { /* os.loadavg() cached 10s */ }
//
// Only `emitPdfStats` is exported. `getSystemLoad` is module-private and is
// called ONLY for the `logger.warn` on slow pdf-caching. The port injects the
// metrics seams + sys-load provider so it can be tested without pulling
// prometheus or os.
package contentcachemetrics

// MetricsOpts mirrors the prometheus request.metricsOpts object passed to
// Metrics.summary/timing/inc. In the Go port this is a placeholder that the
// server layer will fill in once the prometheus bridge lands.
type MetricsOpts = map[string]any

// SeamMetrics bundles the injectable metrics functions (matching the three
// call shapes used in emitPdfStats/emitPdfCachingStats):
//
//   - Metrics.summary(name, value, opts)  — prometheus Histogram
//   - Metrics.timing(name, value, rate, opts)  — prometheus Histogram timing
//   - Metrics.inc(name, value, opts)       — prometheus Counter
type SeamMetrics struct {
	Summary func(name string, value float64, opts MetricsOpts)
	Timing  func(name string, value float64, sampleRate float64, opts MetricsOpts)
	Inc     func(name string, value float64, opts MetricsOpts)
	LogWarn func(attrs map[string]any, msg string)
	SysLoad func() [3]float64 // os.loadavg() (cached 10s in Node)
}

// EmitPdfStats ports emitPdfStats(stats, timings, request.metricsOpts). Mutates
// nothing (all metrics are side-effect-only in Node; this port is the same).
//
// Node guard: `if (timings['compute-pdf-caching'])` — a JS truthiness check
// over a number. In Go timings is map[string]any; a missing key OR a zero
// value is falsy, mirroring Node's `0` => false path.
func EmitPdfStats(stats, timings map[string]any, s SeamMetrics, opts MetricsOpts) {
	calcVal, ok := timings["compute-pdf-caching"]
	if !ok || toFloat(calcVal) == 0 {
		// else: Metrics.summary('pdf-bandwidth', stats['pdf-size'], request.metricsOpts)
		if s.Summary != nil {
			s.Summary("pdf-bandwidth", toFloat(stats["pdf-size"]), opts)
		}
		return
	}
	emitPdfCachingStats(stats, timings, s, opts)
}

func emitPdfCachingStats(stats, timings map[string]any, s SeamMetrics, opts MetricsOpts) {
	// Node: `if (!stats['pdf-size']) return`
	if toFloat(stats["pdf-size"]) == 0 {
		return
	}

	// Node: if (stats['pdf-caching-timed-out']) Metrics.inc('pdf-caching-timed-out', 1, opts)
	if v, ok := stats["pdf-caching-timed-out"]; ok && toTrue(v) {
		if s.Inc != nil {
			s.Inc("pdf-caching-timed-out", 1, opts)
		}
	}

	// Node: if (timings['pdf-caching-overhead-delete-stale-hashes'] !== undefined)
	//       Metrics.summary('pdf-caching-overhead-delete-stale-hashes', ...)
	if v, ok := timings["pdf-caching-overhead-delete-stale-hashes"]; ok && v != nil {
		if s.Summary != nil {
			s.Summary("pdf-caching-overhead-delete-stale-hashes", toFloat(v), opts)
		}
	}

	// Node: Metrics.timing('compute-pdf-caching', timings['compute-pdf-caching'], 1, opts)
	if s.Timing != nil {
		s.Timing("compute-pdf-caching", toFloat(timings["compute-pdf-caching"]), 1, opts)
	}

	// fraction = compileE2E / (compileE2E − computePdfCaching) or 1
	compileE2E := toFloat(timings["compileE2E"])
	pdfCaching := toFloat(timings["compute-pdf-caching"])
	denom := compileE2E - pdfCaching
	fraction := 1.0
	if denom != 0 {
		fraction = compileE2E / denom
	}

	// Node: if (fraction > 1.5 && compileE2E > 10 * 1000) logger.warn(...)
	if fraction > 1.5 && compileE2E > 10000.0 {
		load := [3]float64{}
		if s.SysLoad != nil {
			load = s.SysLoad()
		}
		if s.LogWarn != nil {
			s.LogWarn(map[string]any{
				"stats":   stats,
				"timings": timings,
				"load":    load,
			}, "slow pdf caching")
		}
	}

	// Node: Metrics.summary('overhead-compute-pdf-ranges', fraction * 100 - 100, opts)
	if s.Summary != nil {
		s.Summary("overhead-compute-pdf-ranges", fraction*100-100, opts)
	}

	// Node: Metrics.timing('compute-pdf-caching-relative-to-pdf-size', ...)
	pdfSize := toFloat(stats["pdf-size"])
	if s.Timing != nil && pdfSize != 0 {
		s.Timing("compute-pdf-caching-relative-to-pdf-size",
			pdfCaching/(pdfSize/oneMB), 1, opts)
	}

	rangesSize := toFloat(stats["pdf-caching-total-ranges-size"])
	nRanges := toFloat(stats["pdf-caching-n-ranges"])
	nNewRanges := toFloat(stats["pdf-caching-n-new-ranges"])
	newRangesSize := toFloat(stats["pdf-caching-new-ranges-size"])

	// Node: `if (stats['pdf-caching-total-ranges-size'])` wraps only the
	// relative-to-total-ranges-size, relative-to-ranges-count, and
	// new-pdf-ranges-relative-to-total-ranges calls inside it.
	if rangesSize != 0 {
		// Node: Metrics.timing('compute-pdf-caching-relative-to-total-ranges-size', ...)
		if s.Timing != nil && nRanges != 0 {
			s.Timing("compute-pdf-caching-relative-to-total-ranges-size",
				pdfCaching/(rangesSize/oneMB), 1, opts)
		}
		// Node: Metrics.timing('compute-pdf-caching-relative-to-ranges-count', ...)
		if s.Timing != nil && nRanges != 0 {
			s.Timing("compute-pdf-caching-relative-to-ranges-count",
				pdfCaching/nRanges, 1, opts)
		}
		// Node: Metrics.summary('new-pdf-ranges-relative-to-total-ranges', ...)
		if s.Summary != nil && nRanges != 0 {
			s.Summary("new-pdf-ranges-relative-to-total-ranges",
				(nNewRanges/nRanges)*100, opts)
		}
	}

	// Node: Metrics.summary('cacheable-ranges-to-pdf-size', rangesSize / pdfSize * 100)
	if s.Summary != nil && pdfSize != 0 {
		s.Summary("cacheable-ranges-to-pdf-size", (rangesSize/pdfSize)*100, opts)
	}

	sizeWhenDownloadedInFull := pdfSize - rangesSize + newRangesSize

	// Node: Metrics.summary('pdf-bandwidth-savings', 100 − sizeWhen.../pdfSize*100)
	if s.Summary != nil && pdfSize != 0 {
		s.Summary("pdf-bandwidth-savings", 100-((sizeWhenDownloadedInFull/pdfSize)*100), opts)
	}

	// Node: Metrics.summary('pdf-bandwidth', sizeWhenDownloadedInFull)
	if s.Summary != nil {
		s.Summary("pdf-bandwidth", sizeWhenDownloadedInFull, opts)
	}

	// Node: Metrics.summary('pdf-ranges-disk-size',
	//   stats['pdf-caching-new-ranges-size' minus stats['pdf-caching-reclaimed-space'])
	reclaimed := toFloat(stats["pdf-caching-reclaimed-space"])
	if s.Summary != nil {
		s.Summary("pdf-ranges-disk-size", newRangesSize-reclaimed, opts)
	}
}

const oneMB = 1024.0 * 1024.0

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	}
	return 0
}

func toTrue(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	}
	return true // any non-nil is truthy in JS terms
}
