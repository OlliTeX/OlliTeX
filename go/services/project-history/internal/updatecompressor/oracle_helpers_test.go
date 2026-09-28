// Oracle tests mirroring vendor
// test/unit/js/UpdateCompressor/UpdateCompressorTests.js.
//
// Numbers: fixtures use int values (JSON-decoded in production they are
// float64; both marshal identically here, and the port writes plain
// floats/ints so a JSON round-trip byte comparison is type-agnostic).
package updatecompressor

import (
	"encoding/json"
	"fmt"
	"testing"
)

// --- shared fixtures / helpers ---

const (
	fTS1       = float64(1000000)
	fTS2       = float64(1001000)
	fUser1     = "user-id-1"
	fOtherUser = "user-id-2"
)

func opMap(p int, fields map[string]any) map[string]any {
	m := map[string]any{"p": p}
	for k, v := range fields {
		m[k] = v
	}
	return m
}

func upd(v int, op any, meta map[string]any, extra ...map[string]any) map[string]any {
	u := map[string]any{"op": op, "meta": meta, "v": v}
	for _, e := range extra {
		for k, val := range e {
			u[k] = val
		}
	}
	return u
}

func assertSame(t *testing.T, name string, got, want []map[string]any) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if fmt.Sprintf("%s", g) != fmt.Sprintf("%s", w) {
		t.Fatalf("%s mismatch:\n got: %s\nwant: %s", name, g, w)
	}
}
