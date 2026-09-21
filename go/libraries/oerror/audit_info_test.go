package oerror

// audit_info_test.go — owner-audit regression pins for the GetFullInfo
// cause-chain surface, oracle-verified on Node (live o-error/index.cjs):
//
//	const inner = new Error('plain'); inner.info = {from:'plain-cause'}
//	const err = new OError('outer', {k:1}, inner)
//	OError.getFullInfo(err) // => {"from":"plain-cause","k":1}
//
// i.e. Node merges `.info` off ANY object cause, not only OError instances.
// Go's opt-in equivalent is InfoProvider.

import "testing"

type foreignCause struct{ info map[string]any }

func (f foreignCause) Error() string              { return "foreign cause" }
func (f foreignCause) OErrorInfo() map[string]any { return f.info }

// TestAuditInfoProviderCause — the Node-oracle case above, Go form.
func TestAuditInfoProviderCause(t *testing.T) {
	inner := foreignCause{info: map[string]any{"from": "plain-cause"}}
	e := New("outer", map[string]any{"k": 1.0}, inner)
	got := GetFullInfo(e)
	if len(got) != 2 || got["from"] != "plain-cause" || got["k"] != 1.0 {
		t.Fatalf("GetFullInfo = %v, want {from:plain-cause k:1}", got)
	}
}

// TestAuditInfoProviderPlain — a PROVENIENT plain error implements no info
// surface → contributes nothing (Go's documented boundary; the Node
// equivalent would be a plain Error without an `.info` property).
func TestAuditInfoProviderPlain(t *testing.T) {
	e := New("outer", map[string]any{"k": 1.0}, new(foreignNoInfo))
	got := GetFullInfo(e)
	if len(got) != 1 || got["k"] != 1.0 {
		t.Fatalf("GetFullInfo = %v, want only {k:1}", got)
	}
}

type foreignNoInfo struct{}

func (foreignNoInfo) Error() string { return "no info" }

// TestAuditInfoProviderOuterStillOwnInfo — the outer OError's own info and
// tags still merge after the cause's (outermost wins) — order unchanged.
func TestAuditInfoProviderOuterStillOwnInfo(t *testing.T) {
	inner := foreignCause{info: map[string]any{"k": "inner"}}
	e := New("outer", map[string]any{"k": "outer", "own": true}, inner).Tag("t", map[string]any{"t": true})
	got := GetFullInfo(e)
	if got["k"] != "outer" || got["own"] != true || got["t"] != true {
		t.Fatalf("GetFullInfo = %v", got)
	}
}
