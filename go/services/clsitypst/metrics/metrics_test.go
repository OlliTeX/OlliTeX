// Tests the metrics package's counter and gauge behavior.
package metrics

import (
	"sync"
	"testing"
)

func TestCounterIncAndAdd(t *testing.T) {
	var c *Counter
	// Use the package's exported counters via their names to stay decoupled
	// from global state, but we can only touch the exported *Counter values.
	// Reset the shared counter to a known baseline.
	c = &Counter{}
	c.Inc()
	c.Inc()
	c.Add(5)
	if got := c.Get(); got != 7 {
		t.Errorf("Get = %d, want 7", got)
	}
}

func TestGaugeSetAndGet(t *testing.T) {
	g := &Gauge{}
	g.Set(42)
	if got := g.Get(); got != 42 {
		t.Errorf("Get = %v, want 42", got)
	}
	g.Set(0)
	if got := g.Get(); got != 0 {
		t.Errorf("after Set(0) = %v, want 0", got)
	}
}

func TestIncHelpersDelegate(t *testing.T) {
	// Each exports the same counter it names; exercise them once so the
	// 1-to-1 mapping is covered (values are restored after the test).
	defer func() {
		CompileLockExpired.Value = 0
		CompileLockReleasedTwice.Value = 0
		CompileLockExpiredBeforeRelease.Value = 0
		ExceededCompilerConcurrencyLimit.Value = 0
	}()
	IncCompileLockExpired()
	IncCompileLockReleasedTwice()
	IncCompileLockExpiredBeforeRelease()
	IncExceededCompilerConcurrencyLimit()
	if got := CompileLockExpired.Get(); got != 1 {
		t.Errorf("CompileLockExpired = %d, want 1", got)
	}
	if got := CompileLockReleasedTwice.Get(); got != 1 {
		t.Errorf("CompileLockReleasedTwice = %d, want 1", got)
	}
	if got := CompileLockExpiredBeforeRelease.Get(); got != 1 {
		t.Errorf("CompileLockExpiredBeforeRelease = %d, want 1", got)
	}
	if got := ExceededCompilerConcurrencyLimit.Get(); got != 1 {
		t.Errorf("ExceededCompilerConcurrencyLimit = %d, want 1", got)
	}
}
func TestShouldSkipMetricsPaths(t *testing.T) {
	for _, p := range []string{"clsi-perf", "health-check", "clsi-cache-template"} {
		if !ShouldSkipMetrics(p) {
			t.Errorf("ShouldSkipMetrics(%q) = false, want true", p)
		}
	}
	if ShouldSkipMetrics("/compile") {
		t.Error("ShouldSkipMetrics('/compile') = true, want false")
	}
}

func TestConcurrencyGauge(t *testing.T) {
	// Reset by saving and restoring the value.
	prev := ConcurrentCompileRequests.Get()
	defer GaugeConcurrentCompileRequests(int(prev))
	GaugeConcurrentCompileRequests(5)
	if got := ConcurrentCompileRequests.Get(); got != 5 {
		t.Errorf("after GaugeConcurrentCompileRequests(5) = %v, want 5", got)
	}
}

func TestConcurrencyGaugeIsConcurrentSafe(t *testing.T) {
	prev := ConcurrentCompileRequests.Get()
	defer GaugeConcurrentCompileRequests(int(prev))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				GaugeConcurrentCompileRequests(j)
				ConcurrentCompileRequests.Get()
			}
		}()
	}
	wg.Wait()
}

// --- timer + download-failed counter -----------------------------------------

type metricsPath struct{ path string }

func (m *metricsPath) MetricsPath() string { return m.path }

func TestTimerLifecycle(t *testing.T) {
	tn := NewTimer("compile-time")
	if tn.Name != "compile-time" {
		t.Errorf("NewTimer name = %q, want compile-time", tn.Name)
	}
	if tn.Finished() {
		t.Error("Finished() = true before Done")
	}
	tn.Done(map[string]interface{}{"user": "u1"})
	if !tn.Finished() {
		t.Error("Finished() = false after Done")
	}
}

func TestIncDownloadFailed(t *testing.T) {
	defer func() { DownloadFailed.Value -= 1 }()
	IncDownloadFailed()
	if DownloadFailed.Get() < 1 {
		t.Fatalf("IncDownloadFailed: counter not advanced, Get = %d", DownloadFailed.Get())
	}
}

func TestShouldSkipTimerDelegates(t *testing.T) {
	if ShouldSkipTimer(nil) {
		t.Error("ShouldSkipTimer(nil) = true, want false")
	}
	if !ShouldSkipTimer(&metricsPath{"clsi-perf"}) {
		t.Error("ShouldSkipTimer('/perf') = false, want true")
	}
	if ShouldSkipTimer(&metricsPath{"compile"}) {
		t.Error("ShouldSkipTimer('/compile') = true, want false")
	}
}
