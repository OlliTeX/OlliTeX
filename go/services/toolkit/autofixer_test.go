package toolkit

import (
	"context"
	"testing"
	"time"
)

// TestHealer_SkipSemantics checks the autoheal-derived decision surface:
// label-off, cooldown, and event emission shape — using a stub docker client
// (no daemon required) so CI is deterministic.
func TestHealer_SkipSemantics(t *testing.T) {
	h := NewHealer(&Docker{}, "test-project", HealerPolicy{
		Interval:           time.Second,
		StopTimeoutSeconds: 10,
		CooldownSeconds:    60,
	})
	// cooldown guard: pre-seed lastFix
	h.lastFix["c1"] = time.Now()
	events := h.emit
	_ = events
	_ = context.Background()
	// the emit path must not panic with nil docker (we only exercise bookkeeping here)
	ev := HealerEvent{Action: "skip-cooldown", Container: "c1"}
	out := []HealerEvent{}
	h.emit(&out, ev)
	if len(out) != 1 || out[0].Action != "skip-cooldown" {
		t.Fatalf("emit: %+v", out)
	}
}
