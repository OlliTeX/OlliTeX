package settings

import (
	"math"
	"testing"
)

// C3: foldLayer truthy-PRIMITIVE default slot — override map silently dropped.
// Oracle: merge({a:{b:1}}, {a:"5"}) -> {"a":"5"} (the string slot is a truthy
// primitive; JS `{"b":1}[k]` is a no-op on a string primitive). Exercises the
// foldLayer `default:` arm (distinct from the falsy-0 / map branches).
func TestCovFoldDefaultArm(t *testing.T) {
	out := mustMerge(t, map[string]any{"a": map[string]any{"b": 1.0}}, map[string]any{"a": "5"})
	if got, ok := out["a"].(string); !ok || got != "5" {
		t.Fatalf("truthy-prim slot: got %v, want string \"5\"", out["a"])
	}
	out = mustMerge(t, map[string]any{"a": map[string]any{"b": 1.0}}, map[string]any{"a": 7.0})
	if got, ok := out["a"].(float64); !ok || got != 7.0 {
		t.Fatalf("number slot: got %v, want 7", out["a"])
	}
}

// C4: isNonNegativeInt arms (empty, non-digit, digit), and the foldLayer
// primitive-replace path for the EMPTY-STRING key (not a numeric-string).
func TestCovIsNonNegativeIntArms(t *testing.T) {
	if isNonNegativeInt("") || isNonNegativeInt("a") || isNonNegativeInt("1a") {
		t.Fatal("isNonNegativeInt: empty and non-digit must be false")
	}
	if !isNonNegativeInt("0") || !isNonNegativeInt("123") {
		t.Fatal("isNonNegativeInt: digit-only must be true")
	}
	out := mustMerge(t, map[string]any{"": "x"}, map[string]any{"": 0.0})
	if got, ok := out[""].(string); !ok || got != "x" {
		t.Fatalf("empty-key primitive replace: got %v", out[""])
	}
}

// C5: jsFalsy across every numeric type the runtime can hold, so the int8 /
// int16 / int32 / int64 / uint / uint8 / uint16 / uint32 / uint64 branches
// compile AND run (the oracle goldens exercised only the float64 arm).
func TestCovJsFalsyArms(t *testing.T) {
	falsyVals := []any{
		0, int8(0), int16(0), int32(0), int64(0),
		uint(0), uint8(0), uint16(0), uint32(0), uint64(0),
		float64(0), math.NaN(),
		"", false, nil,
	}
	for _, v := range falsyVals {
		if !jsFalsy(v) {
			t.Errorf("jsFalsy(%v) (type %T) must be true (falsy)", v, v)
		}
	}
	truthyVals := []any{
		int8(1), int16(-1), int32(4), int64(5),
		uint(6), uint8(7), uint16(8), uint32(9), uint64(10),
		float64(7.0), float64(-0.5),
		"1", true,
		map[string]any{"k": 1}, []any{1},
	}
	for _, v := range truthyVals {
		if jsFalsy(v) {
			t.Errorf("jsFalsy(%v) (type %T) must be false (truthy)", v, v)
		}
	}
}
