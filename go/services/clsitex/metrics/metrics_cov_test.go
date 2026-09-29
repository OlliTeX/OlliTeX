package metrics

import (
	"testing"
	"time"
)

func TestCountAndNamedCounter(t *testing.T) {
	Count("cov-probe", 3)
	c := NamedCounter("cov-probe")
	if c == nil {
		t.Fatal("named counter nil")
	}
	if got := c.Get(); got < 3 {
		t.Fatalf("counter = %d want >= 3", got)
	}
	// second Count reuses the same backing counter
	Count("cov-probe", 2)
	if got := NamedCounter("cov-probe").Get(); got < 5 {
		t.Fatalf("counter = %d want >= 5", got)
	}
	// a name never counted is absent (NamedCounter returns nil)
	if c := NamedCounter("cov-never-probe"); c != nil {
		t.Fatalf("fresh counter = %v want nil", c)
	}
}

func TestTimerDoneMS(t *testing.T) {
	t0 := NewTimer("probe-ms")
	time.Sleep(5 * time.Millisecond)
	if elapsed := t0.DoneMS(); elapsed < 0 {
		t.Fatalf("doneMS = %d want >= 0", elapsed)
	}
	if !t0.Finished() {
		t.Fatal("DoneMS should mark the timer finished")
	}
}
