// metrics_cov_histogram_test.go — coverage for the histogram surface and the
// png2pdf-skipped-small counter that HistoryResourceWriter observes.
package metrics

import (
	"testing"
)

func TestHistogramObserveObservedCountGet(t *testing.T) {
	h := &Histogram{Name: "clsi_cov_probe"}
	// fresh histogram: zero observations, zero bucket values.
	if n := h.ObservedCount("standard", "local"); n != 0 {
		t.Fatalf("fresh bucket = %d, want 0", n)
	}
	if n, sum := h.Get(); n != 0 || sum != 0 {
		t.Fatalf("fresh Get = (%d, %v), want (0, 0)", n, sum)
	}

	// Observe under distinct (group, source) labels.
	h.Observe("standard", "local", 0.5)
	h.Observe("standard", "local", 0.25)
	h.Observe("clsi-perf", "remote", 1.0)

	if n, sum := h.Get(); n != 3 || sum != 1.75 {
		t.Fatalf("Get = (%d, %v), want (3, 1.75)", n, sum)
	}
	if n := h.ObservedCount("standard", "local"); n != 2 {
		t.Fatalf("bucket standard/local = %d, want 2", n)
	}
	if n := h.ObservedCount("clsi-perf", "remote"); n != 1 {
		t.Fatalf("bucket clsi-perf/remote = %d, want 1", n)
	}
	// a never-observed (group, source) pair is zero.
	if n := h.ObservedCount("nope", "nope"); n != 0 {
		t.Fatalf("unknown bucket = %d, want 0", n)
	}

	// The production wiring used by HRW: the named histogram globals get
	// observed exactly like a sync would.
	SnapshotApplyAllDurationSeconds.Observe("standard", "local", 0.1)
	if n := SnapshotApplyAllDurationSeconds.ObservedCount("standard", "local"); n < 1 {
		t.Fatalf("SnapshotApplyAll bucket = %d, want >= 1", n)
	}
	SnapshotLoadEagerDurationSeconds.Observe("clsi-cache", "clsi-cache", 0.2)
	if n, sum := SnapshotLoadEagerDurationSeconds.Get(); n < 1 || sum < 0.2 {
		t.Fatalf("SnapshotLoadEager Get = (%d, %v), want >= 1 and >= 0.2", n, sum)
	}
}

func TestIncPng2pdfSkippedSmall(t *testing.T) {
	before := Png2pdfSkippedSmall.Get()
	IncPng2pdfSkippedSmall()
	IncPng2pdfSkippedSmall()
	if got := Png2pdfSkippedSmall.Get(); got != before+2 {
		t.Fatalf("skipped-small = %d, want %d", got, before+2)
	}
	// The named counter identity matches the prom name.
	if Png2pdfSkippedSmall.Name != "png2pdf-skipped-small" {
		t.Fatalf("name = %q, want png2pdf-skipped-small", Png2pdfSkippedSmall.Name)
	}
}
