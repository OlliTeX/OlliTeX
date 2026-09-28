// Oracle: vendor test/unit/js/OperationsCompressor/OperationsCompressorTests.js
// (5 it blocks).
package operationscompressor

import (
	"reflect"
	"testing"

	"ollitex/go/services/project-history/internal/historyot"
)

// edit — vendor helper: `Operation.editFile(pathname, TextOperation.fromJSON)`.
func edit(t *testing.T, raw map[string]any) historyot.Operation {
	op, err := historyot.OperationFromRaw(raw)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	return op
}

func rawOps(ops []historyot.Operation) []map[string]any {
	out := make([]map[string]any, len(ops))
	for i, op := range ops {
		out[i] = normalizeNumbers(op.ToRaw()).(map[string]any)
	}
	return out
}

// normalizeNumbers — wire numbers come from opmodel as Go int and from JSON
// decode as float64; comparisons normalize so DeepEqual is well-defined.
func normalizeNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalizeNumbers(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalizeNumbers(val)
		}
		return out
	case int:
		return float64(t)
	}
	return v
}

func assertSameOps(t *testing.T, name string, got []historyot.Operation, want ...map[string]any) {
	t.Helper()
	gotRaw := rawOps(got)
	if len(gotRaw) != len(want) {
		t.Fatalf("%s: got %d ops, want %d: %+v", name, len(gotRaw), len(want), gotRaw)
	}
	for i, w := range want {
		if !reflect.DeepEqual(gotRaw[i], normalizeNumbers(w)) {
			t.Errorf("%s: op[%d] = %v, want %v", name, i, gotRaw[i], w)
		}
	}
}

// vendor: "collapses edit operations"
func TestCompressOperationsCollapsesEdit(t *testing.T) {
	compressed := CompressOperations([]historyot.Operation{
		edit(t, map[string]any{"pathname": "main.tex", "textOperation": []any{float64(3), "foo", float64(17)}}),
		edit(t, map[string]any{"pathname": "main.tex", "textOperation": []any{float64(10), float64(-5), float64(8)}}),
	})
	assertSameOps(t, "collapse", compressed,
		map[string]any{"pathname": "main.tex", "textOperation": []any{float64(3), "foo", float64(4), float64(-5), float64(8)}},
	)
}

// vendor: "only collapses consecutive composable edit operations"
func TestCompressOperationsConsecutiveOnly(t *testing.T) {
	compressed := CompressOperations([]historyot.Operation{
		edit(t, map[string]any{"pathname": "main.tex", "textOperation": []any{float64(3), "foo", float64(17)}}),
		edit(t, map[string]any{"pathname": "main.tex", "textOperation": []any{float64(10), float64(-5), float64(8)}}),
		edit(t, map[string]any{"pathname": "not-main.tex", "textOperation": []any{float64(3), "foo", float64(17)}}),
		edit(t, map[string]any{"pathname": "not-main.tex", "textOperation": []any{float64(10), float64(-5), float64(8)}}),
	})
	assertSameOps(t, "consecutive", compressed,
		map[string]any{"pathname": "main.tex", "textOperation": []any{float64(3), "foo", float64(4), float64(-5), float64(8)}},
		map[string]any{"pathname": "not-main.tex", "textOperation": []any{float64(3), "foo", float64(4), float64(-5), float64(8)}},
	)
}

// vendor: "don't collapses text operations around non-composable operations"
func TestCompressOperationsAroundNonComposable(t *testing.T) {
	compressed := CompressOperations([]historyot.Operation{
		edit(t, map[string]any{"pathname": "main.tex", "textOperation": []any{float64(3), "foo", float64(17)}}),
		edit(t, map[string]any{"pathname": "main.tex", "newPathname": "new-main.tex"}),
		edit(t, map[string]any{"pathname": "new-main.tex", "textOperation": []any{float64(10), float64(-5), float64(8)}}),
		edit(t, map[string]any{"pathname": "new-main.tex", "textOperation": []any{float64(6), "bar", float64(12)}}),
	})
	if len(compressed) != 3 {
		t.Fatalf("got %d ops, want 3: %+v", len(compressed), rawOps(compressed))
	}
	assertSameOps(t, "around", compressed[:2],
		map[string]any{"pathname": "main.tex", "textOperation": []any{float64(3), "foo", float64(17)}},
		map[string]any{"pathname": "main.tex", "newPathname": "new-main.tex"},
	)
	// vendor asserts `compressedOperations[1].newPathname === 'new-main.tex'`
	if np, _ := compressed[1].ToRaw()["newPathname"].(string); np != "new-main.tex" {
		t.Fatalf("move pathname: %v", compressed[1].ToRaw())
	}
	// Last op: the two edits are composed
	lastRaw := normalizeNumbers(compressed[2].ToRaw())
	wantLast := normalizeNumbers(map[string]any{"pathname": "new-main.tex", "textOperation": []any{float64(6), "bar", float64(4), float64(-5), float64(8)}})
	if !reflect.DeepEqual(lastRaw, wantLast) {
		t.Fatalf("last op = %v, want composed", lastRaw)
	}
}

// vendor: "handle empty operations"
func TestCompressOperationsEmpty(t *testing.T) {
	got := CompressOperations([]historyot.Operation{})
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

// vendor: "handle single operations"
func TestCompressOperationsSingle(t *testing.T) {
	compressed := CompressOperations([]historyot.Operation{
		edit(t, map[string]any{"pathname": "main.tex", "textOperation": []any{float64(3), "foo", float64(17)}}),
	})
	if len(compressed) != 1 {
		t.Fatalf("expected 1 op, got %v", rawOps(compressed))
	}
	last := normalizeNumbers(compressed[0].ToRaw())
	if !reflect.DeepEqual(last, normalizeNumbers(map[string]any{"pathname": "main.tex", "textOperation": []any{float64(3), "foo", float64(17)}})) {
		t.Fatalf("op = %v, want unchanged", last)
	}
}
