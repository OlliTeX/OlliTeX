package contentcachemetrics

import (
	"math"
	"testing"
)

type recMetrics struct {
	summaries map[string]float64
	timings   map[string]float64
	incs      map[string]float64
	warns     int
}

func newRecorder() *recMetrics {
	return &recMetrics{
		summaries: map[string]float64{},
		timings:   map[string]float64{},
		incs:      map[string]float64{},
	}
}

func (r *recMetrics) seam() SeamMetrics {
	return SeamMetrics{
		Summary: func(name string, val float64, opts MetricsOpts) {
			r.summaries[name] = val
		},
		Timing: func(name string, val float64, sampleRate float64, opts MetricsOpts) {
			r.timings[name] = val
		},
		Inc: func(name string, val float64, opts MetricsOpts) {
			r.incs[name] = val
		},
		LogWarn: func(attrs map[string]any, msg string) { r.warns++ },
		SysLoad: func() [3]float64 { return [3]float64{1, 2, 3} },
	}
}

func TestEmitPdfStats_NoCaching(t *testing.T) {
	rec := newRecorder()
	stats := map[string]any{"pdf-size": 1024.0}
	timings := map[string]any{}
	EmitPdfStats(stats, timings, rec.seam(), map[string]any{})
	// Only pdf-bandwidth (no caching) should be emitted from this path.
	if got := rec.summaries["pdf-bandwidth"]; got != 1024.0 {
		t.Fatalf("pdf-bandwidth = %v want 1024", got)
	}
	if len(rec.timings) != 0 {
		t.Fatalf("unexpected timings: %v", rec.timings)
	}
}

func TestEmitPdfStats_CachingFull(t *testing.T) {
	rec := newRecorder()
	stats := map[string]any{
		"pdf-size":                      2048.0,
		"pdf-caching-total-ranges-size": 1024.0,
		"pdf-caching-new-ranges-size":   512.0,
		"pdf-caching-reclaimed-space":   128.0,
		"pdf-caching-n-ranges":          10.0,
		"pdf-caching-n-new-ranges":      4.0,
		"pdf-caching-timed-out":         1.0,
	}
	timings := map[string]any{
		"compute-pdf-caching": 100.0,
		"compileE2E":          400.0,
		"pdf-caching-overhead-delete-stale-hashes": 5.0,
	}
	EmitPdfStats(stats, timings, rec.seam(), map[string]any{})

	wantSummaries := map[string]float64{
		"overhead-compute-pdf-ranges":              (400.0/(400.0-100.0))*100 - 100,
		"new-pdf-ranges-relative-to-total-ranges":  (4.0 / 10.0) * 100,
		"cacheable-ranges-to-pdf-size":             (1024.0 / 2048.0) * 100,
		"pdf-bandwidth-savings":                    100 - (float64(2048-1024+512)/2048.0)*100,
		"pdf-bandwidth":                            float64(2048 - 1024 + 512),
		"pdf-ranges-disk-size":                     512.0 - 128.0,
		"pdf-caching-overhead-delete-stale-hashes": 5.0,
	}
	for k, v := range wantSummaries {
		if got := rec.summaries[k]; math.Abs(got-v) > 1e-12 {
			t.Fatalf("%s = %v want %v", k, got, v)
		}
	}
	if rec.incs["pdf-caching-timed-out"] != 1 {
		t.Fatalf("pdf-caching-timed-out inc missing")
	}
	if rec.timings["compute-pdf-caching"] != 100.0 {
		t.Fatalf("compute-pdf-caching timing missing")
	}
	if rec.timings["compute-pdf-caching-relative-to-pdf-size"] != 100.0/(2048.0/oneMB) {
		t.Fatalf("relative-to-pdf-size wrong: %v", rec.timings["compute-pdf-caching-relative-to-pdf-size"])
	}
	if rec.timings["compute-pdf-caching-relative-to-total-ranges-size"] != 100.0/(1024.0/oneMB) {
		t.Fatalf("relative-to-total-ranges wrong: %v", rec.timings["compute-pdf-caching-relative-to-total-ranges-size"])
	}
	if rec.timings["compute-pdf-caching-relative-to-ranges-count"] != 100.0/10.0 {
		t.Fatalf("relative-to-ranges-count wrong: %v", rec.timings["compute-pdf-caching-relative-to-ranges-count"])
	}
}

// compileE2E (0) − compute-pdf-caching (100) == 0 denom => fraction 1 (no div-by-zero).
func TestEmitPdfStats_DivByZeroFraction(t *testing.T) {
	rec := newRecorder()
	// compileE2E == compute-pdf-caching => zero denom => fraction = 1 => overhead 0
	stats := map[string]any{"pdf-size": 100.0}
	timings := map[string]any{"compute-pdf-caching": 100.0, "compileE2E": 100.0}
	EmitPdfStats(stats, timings, rec.seam(), map[string]any{})
	if got := rec.summaries["overhead-compute-pdf-ranges"]; math.Abs(got) > 1e-12 {
		t.Fatalf("overhead = %v want 0 (zero denom fallback)", got)
	}
}

func TestEmitPdfStats_WarnOnSlow(t *testing.T) {
	rec := newRecorder()
	// compileE2E large, compute-pdf-caching small but ratio must trip the
	// (f>1.5 && E2E>10s) condition: e.g. E2E=20000, caching=1000 =>
	// ratio = 20000/19000 ≈ 1.05 (not >1.5). Use E2E=20s, caching=5s =>
	// ratio = 20/15 ≈ 1.33 (not >1.5). To force >1.5: E2E=20000/15000=1.33, no.
	// E2E=20s caching=6.5s => ratio = 20/13.5 ≈ 1.48 (not >1.5). E2E=11s,
	// caching=6s => 11/5 = 2.2 >1.5 AND 11s>10s ⇒ warn.
	rec.warns = 0
	timings := map[string]any{"compute-pdf-caching": 6000.0, "compileE2E": 11000.0}
	stats := map[string]any{"pdf-size": 100.0}
	EmitPdfStats(stats, timings, rec.seam(), map[string]any{})
	if rec.warns != 1 {
		t.Fatalf("warns=%d want 1", rec.warns)
	}
}

func TestEmitPdfStats_NoWarn(t *testing.T) {
	rec := newRecorder()
	rec.warns = 0
	// ratio 10000/9000 ≈ 1.11 < 1.5; no warn
	EmitPdfStats(map[string]any{"pdf-size": 100.0},
		map[string]any{"compute-pdf-caching": 1000.0, "compileE2E": 10000.0}, rec.seam(), nil)
	if rec.warns != 0 {
		t.Fatalf("warns=%d want 0", rec.warns)
	}
}
