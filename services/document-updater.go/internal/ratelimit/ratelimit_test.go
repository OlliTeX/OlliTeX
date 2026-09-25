// Mirrors Node test/unit/js/RateLimitManager/RateLimitManager.js 1:1.
//
// Node drives task completion via setTimeout (the task defers its callback).
// The Go tests mirror the observable transitions: ActiveWorkerCount, the
// current-limit gauge, the up/down limit adjustments, and the error flow.
package ratelimit

import (
	"errors"
	"math"
	"testing"
)

type gaugeRecorder struct {
	entries []gaugeEntry
}

type gaugeEntry struct {
	name  string
	value int
}

func (g *gaugeRecorder) Gauge(name string, value int) {
	g.entries = append(g.entries, gaugeEntry{name, value})
}

func (g *gaugeRecorder) last() gaugeEntry {
	return g.entries[len(g.entries)-1]
}

func TestNew_Defaults(t *testing.T) {
	if rd := NewDefault(); rd.CurrentWorkerLimit != 10 || rd.BaseWorkerCount != 10 {
		t.Fatalf("NewDefault: limit/base = %v/%d, want 10/10", rd.CurrentWorkerLimit, rd.BaseWorkerCount)
	}
	if n := New(3); n.CurrentWorkerLimit != 3 || n.BaseWorkerCount != 3 {
		t.Fatalf("New(3): limit/base = %v/%d, want 3/3", n.CurrentWorkerLimit, n.BaseWorkerCount)
	}
}

// Node "for a single task": limit 1, one background task, active count is 1
// and the callback already fired.
func TestSingleTask_Background(t *testing.T) {
	rl := New(1)
	rec := &gaugeRecorder{}
	rl.Metrics = rec
	var taskRan, callbackRan int
	rl.Run(func(done func(error)) {
		taskRan++
		// no done — the task stays "active"
	}, func(error) {
		callbackRan++
	})
	if taskRan != 1 {
		t.Fatalf("task ran %d times, want 1", taskRan)
	}
	if callbackRan != 1 {
		t.Fatalf("callback ran %d times, want 1", callbackRan)
	}
	if rl.ActiveWorkerCount != 1 {
		t.Errorf("activeWorkerCount = %d, want 1 (in the background)", rl.ActiveWorkerCount)
	}
	if got := rec.last(); got.name != "processingUpdates" || got.value != 1 {
		t.Errorf("last gauge = %+v, want {processingUpdates 1}", got)
	}
	if len(rec.entries) != 1 {
		t.Errorf("gauge called %d times, want 1", len(rec.entries))
	}
}

// A synchronous (limit-reached) successful task raises the limit and emits
// the currentLimit gauge.
func TestSynchronousSuccess_RaisesLimit(t *testing.T) {
	rl := New(1)
	rec := &gaugeRecorder{}
	rl.Metrics = rec
	// Occupy the single slot so the next run is synchronous.
	rl.Run(func(done func(error)) { /* active forever */ }, func(error) {})
	if rl.ActiveWorkerCount != 1 {
		t.Fatalf("active count = %d, want 1 (prime slot)", rl.ActiveWorkerCount)
	}
	var err error
	rl.Run(func(done func(error)) { done(nil) }, func(e error) { err = e })
	if err != nil {
		t.Fatalf("callback error: %v", err)
	}
	if rl.CurrentWorkerLimit != 1.1 {
		t.Errorf("limit = %v, want 1.1 (up-adjusted)", rl.CurrentWorkerLimit)
	}
	if g := rec.last(); g.name != "currentLimit" || g.value != int(math.Ceil(1.1)) {
		t.Errorf("last gauge = %+v, want {currentLimit 2}", g)
	}
}

// A synchronous task that fails propagates the error to the callback and
// does NOT raise the limit.
func TestSynchronousFailure_NoRaise(t *testing.T) {
	rl := New(1)
	rec := &gaugeRecorder{}
	rl.Metrics = rec
	boom := errors.New("boom")
	rl.Run(func(done func(error)) { /* stays active: prime the slot */ }, func(error) {})
	if rl.ActiveWorkerCount != 1 {
		t.Fatalf("active count = %d, want 1 (primed)", rl.ActiveWorkerCount)
	}
	var gotErr error
	rl.Run(func(done func(error)) { done(boom) }, func(e error) { gotErr = e })
	if !errors.Is(gotErr, boom) {
		t.Fatalf("callback error = %v, want boom", gotErr)
	}
	if rl.CurrentWorkerLimit != 1 {
		t.Errorf("limit = %v, want 1 (unchanged after failure)", rl.CurrentWorkerLimit)
	}
	if rl.ActiveWorkerCount != 1 {
		t.Errorf("active = %d, want 1 (first task still active)", rl.ActiveWorkerCount)
	}
	_ = rec
}

// Node "multiple tasks": with a high limit, an extra background run lowers an
// above-base limit (max(base, limit*0.9)).
func TestBelowLimit_LowersExcessiveLimit(t *testing.T) {
	rl := New(3)
	rec := &gaugeRecorder{}
	rl.Metrics = rec
	rl.CurrentWorkerLimit = 4 // e.g. after successive up-adjustments
	var callbackRan int
	rl.Run(func(done func(error)) { /* stays active */ }, func(error) { callbackRan++ })
	if callbackRan != 1 {
		t.Fatalf("callback ran %d times, want 1", callbackRan)
	}
	if rl.CurrentWorkerLimit != 3.6 {
		t.Fatalf("limit = %v, want 3.6 (4*0.9)", rl.CurrentWorkerLimit)
	}
	if g := rec.last(); g.name != "currentLimit" || g.value != 4 {
		t.Errorf("last gauge = %+v, want {currentLimit 4}", g)
	}
}

// The limit never drops below the base: max(base, limit*0.9).
func TestLimitFloorsAtBase(t *testing.T) {
	rl := New(3)
	rec := &gaugeRecorder{}
	rl.Metrics = rec
	rl.CurrentWorkerLimit = 3.2 // 3.2*0.9 = 2.88 < base
	rl.Run(func(done func(error)) { /* stays active */ }, func(error) {})
	if rl.CurrentWorkerLimit != float64(rl.BaseWorkerCount) {
		t.Fatalf("limit = %v, want base %d", rl.CurrentWorkerLimit, rl.BaseWorkerCount)
	}
	if g := rec.last(); g.name != "currentLimit" || g.value != 3 {
		t.Errorf("last gauge = %+v, want {currentLimit 3}", g)
	}
}
