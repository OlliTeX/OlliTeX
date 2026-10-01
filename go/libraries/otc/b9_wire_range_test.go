package otc

import (
	"testing"
)

// audit B9 regression: crafted raw range coordinates (pos < 0 / length < 0)
// must produce a proper ERROR on the wire path — never a process-level
// panic. Node oracle: `new Range(-1, 0)` throws an OError → the fromRaw
// chain rejects → the caller returns an error envelope.

func TestB9WireNewRangePanicDeterministic_addCommentOp(t *testing.T) {
	raw := map[string]any{
		"commentId": "c1",
		"ranges":    []any{map[string]any{"pos": -1, "length": 0}},
		"resolved":  false,
	}
	op, err := OperationFromRaw(raw)
	if err == nil {
		t.Fatalf("want error for pos=-1 range, got op=%+v", op)
	}
}

func TestB9WireNewRangePanicDeterministic_negativeLength(t *testing.T) {
	raw := map[string]any{
		"commentId": "c1",
		"ranges":    []any{map[string]any{"pos": 3, "length": -2}},
		"resolved":  false,
	}
	op, err := OperationFromRaw(raw)
	if err == nil {
		t.Fatalf("want error for length=-2 range, got op=%+v", op)
	}
}

func TestB9WireNewRangePanicDeterministic_fromRawComment(t *testing.T) {
	raw := map[string]any{
		"id": "c1",
		"ranges": []any{
			map[string]any{"pos": 0, "length": 5},
			map[string]any{"pos": -9, "length": 1},
		},
	}
	c, err := FromRawComment(raw)
	if err == nil {
		t.Fatalf("want error for malformed comment range, got %+v", c)
	}
}

func TestB9WireNewRangePanicDeterministic_fromRawChangeOperations(t *testing.T) {
	raw := [][]map[string]any{
		{
			{
				"pathname":  "main.tex",
				"commentId": "c1",
				"ranges":    []any{map[string]any{"pos": -1, "length": 1}},
				"resolved":  false,
			},
		},
	}
	// wire shape in historyresourcewriter.go changesFromRawChangeOperations:
	for _, oRaw := range raw[0] {
		op, err := OperationFromRaw(oRaw)
		if err == nil {
			t.Fatalf("want error for crafted rawChangeOperations, got op=%+v", op)
		}
	}
}

func TestRawRangesValidStillRoundTrips(t *testing.T) {
	raw := []any{
		map[string]any{"pos": 0, "length": 10},
		map[string]any{"pos": 10, "length": 5},
	}
	out, err := asRawRangesErr(raw)
	if err != nil {
		t.Fatalf("valid ranges must not error: %v", err)
	}
	if len(out) != 2 || out[0].Start() != 0 || out[1].End() != 15 {
		t.Fatalf("unexpected ranges %+v", out)
	}
}
