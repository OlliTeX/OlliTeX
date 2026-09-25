package profiler

import "testing"

// The profiler reads the wall clock through the package-level nowNano hook so
// tests can force a deterministic sequence of unix-nanos timestamps.
//
// Node oracle anchors (Profiler.js semantics):
//   - t0 is the constructor time; log(label, {sync}) pushes [label, dtMs] and
//     (optionally) adds dt to totalSyncTime; end() returns total whole-run
//     millis and warns iff total > 15s or totalSyncTime > 1s.
//   - Cutoffs are strictly-greater comparisons.
func TestProfiler_ImmediateNoWarn(t *testing.T) {
	oldWarn := Warn
	oldNano := nowNano
	var warned map[string]any
	Warn = func(args map[string]any, name string) { warned = args }
	defer func() { Warn = oldWarn; nowNano = oldNano }()

	seq := []int64{0, 0, 0} // New: (t0, t); Log: (t) — End uses stored t.
	i := 0
	nowNano = func() int64 {
		v := seq[i]
		i++
		return v
	}

	p := New("model.transform")
	if got := p.Log("transform", true); got != p {
		t.Fatal("Log must be chainable (return the same profiler)")
	}
	total := p.End()
	if total != 0 {
		t.Fatalf("total: want 0, got %d", total)
	}
	if warned != nil {
		t.Fatalf("unexpected warn: %v", warned)
	}
}

func TestProfiler_SyncCutoffWarn(t *testing.T) {
	oldWarn := Warn
	oldNano := nowNano
	var warned map[string]any
	Warn = func(args map[string]any, name string) { warned = args }
	defer func() { Warn = oldWarn; nowNano = oldNano }()

	// constructor (t0 = 0, t = 0), Log(t = 2s): dt = 2000ms sync.
	seq := []int64{0, 0, 2_000_000_000, 2_000_000_000}
	i := 0
	nowNano = func() int64 {
		v := seq[i]
		i++
		return v
	}

	p := New("model.apply")
	p.Log("transform", true) // dt = 2000ms, sync
	total := p.End()

	if total != 2000 {
		t.Fatalf("total: want 2000, got %d", total)
	}
	if warned == nil {
		t.Fatal("end must warn when sync time exceeds 1s")
	}
	if !warned["exceedsSyncCutoff"].(bool) {
		t.Fatal("exceedsSyncCutoff must be true")
	}
	if warned["exceedsCutoff"].(bool) {
		t.Fatal("exceedsCutoff must be false (2s < 15s)")
	}
}

func TestProfiler_TotalCutoffWarn(t *testing.T) {
	oldWarn := Warn
	oldNano := nowNano
	var warned map[string]any
	Warn = func(args map[string]any, name string) { warned = args }
	defer func() { Warn = oldWarn; nowNano = oldNano }()

	// constructor (0, 0), Log(t = 16s, sync flagged false).
	seq := []int64{0, 0, 16_000_000_000, 16_000_000_000}
	i := 0
	nowNano = func() int64 {
		v := seq[i]
		i++
		return v
	}

	p := New("model.transform")
	p.Log("transform", false) // dt = 16s, not sync
	total := p.End()

	if total != 16_000 {
		t.Fatalf("total: want 16000, got %d", total)
	}
	if warned == nil {
		t.Fatal("end must warn when total exceeds 15s")
	}
	if !warned["exceedsCutoff"].(bool) {
		t.Fatal("exceedsCutoff must be true")
	}
	if warned["exceedsSyncCutoff"].(bool) {
		t.Fatal("exceedsSyncCutoff must be false (no sync time)")
	}
}

// Cutoffs are strictly-greater: exactly at the cutoff does NOT warn.
func TestProfiler_CutoffIsStrict(t *testing.T) {
	oldWarn := Warn
	oldNano := nowNano
	var warned map[string]any
	Warn = func(args map[string]any, name string) { warned = args }
	defer func() { Warn = oldWarn; nowNano = oldNano }()

	// constructor (0, 0), Log(t = 1s): dt = exactly 1000ms sync.
	seq := []int64{0, 0, 1_000_000_000, 1_000_000_000}
	i := 0
	nowNano = func() int64 {
		v := seq[i]
		i++
		return v
	}

	p := New("model.transform")
	p.Log("model.transform", true)
	p.End()
	if warned != nil {
		t.Fatalf("1s exactly must not exceed the 1s cutoff: %v", warned)
	}
}
