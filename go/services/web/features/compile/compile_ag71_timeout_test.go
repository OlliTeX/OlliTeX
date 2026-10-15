package compile

import (
	"testing"
	"time"
)

// ag-71 (2026-10-15 live): the cold FIRST typst compile (fresh package cache,
// cold fs) measured ~270 s against the former 240 s dispatch deadline → the
// deadline expired ~seconds before the job finished → invisible 500 + the
// client's "compiling" spinner never cleared. Guardrail: the dispatch budget
// keeps substantial headroom over the observed cold run and the 180 s job
// timeout, so realistic cold compiles land inside the dispatch window.
func TestAg71ClsiDispatchTimeoutBudget(t *testing.T) {
	if clsiDispatchTimeout < 600*time.Second {
		t.Fatalf("clsiDispatchTimeout = %v: below the 600 s guardrail (observed live cold typst run ~270 s; the 240 s deadline failed)", clsiDispatchTimeout)
	}
	if clsiDispatchTimeout <= defCompileTimeout*time.Second {
		t.Fatalf("clsiDispatchTimeout (%v) must exceed the default compile job timeout (%v), else the job could never finish inside its dispatch window", clsiDispatchTimeout, defCompileTimeout*time.Second)
	}
}
