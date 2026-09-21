package notifprefs

// coverage_test.go — behaviour-pins for the JS-coercion helpers (jsTruthy /
// jsNumber / jsNumberString) across Go's full numeric widths. These are the
// 1:1 Go stand-ins for JS's single `number` type: the semantics (0 vs non-zero
// falsiness, NaN/Inf invalid, trimmed decimal/hex strings) are documented in
// notifprefs.go and oracle-covered via the exported normalizers; the width
// variants (int8..uint64, float32) are pinned here so the Go numeric range is
// handled identically to JS. No invariant here is forced beyond the documented
// JS coercion.

import (
	"math"
	"testing"
)

func TestJsTruthy(t *testing.T) {
	// JS falsy: 0 (any width), "", false, NaN, null.
	falsy := []any{
		nil, false, "", 0,
		int8(0), int16(0), int32(0), int64(0),
		uint(0), uint8(0), uint16(0), uint32(0), uint64(0),
		float32(0), float64(0),
		math.NaN(), float32(math.Float32frombits(0x7fc00000)),
	}
	for _, v := range falsy {
		if jsTruthy(v) {
			t.Errorf("jsTruthy(%#v) = true; want false", v)
		}
	}
	// Everything else is truthy (incl. negative and non-zero floats).
	truthy := []any{
		true, "x", 1,
		int8(1), int16(1), int32(1), int64(1),
		uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
		float32(0.1), float64(-1),
		struct{ Name string }{"an object"},
	}
	for _, v := range truthy {
		if !jsTruthy(v) {
			t.Errorf("jsTruthy(%#v) = false; want true", v)
		}
	}
}

func TestJsNumber(t *testing.T) {
	// Valid conversions across widths + bool.
	valid := []struct {
		v    any
		want float64
	}{
		{int(5), 5}, {int8(3), 3}, {int16(4), 4}, {int32(5), 5}, {int64(6), 6},
		{uint(7), 7}, {uint8(8), 8}, {uint16(9), 9}, {uint32(10), 10}, {uint64(11), 11},
		{float32(2.5), 2.5}, {float64(3.75), 3.75},
		{true, 1}, {false, 0},
	}
	for _, c := range valid {
		got, ok := jsNumber(c.v)
		if !ok || got != c.want {
			t.Errorf("jsNumber(%#v) = (%v, %v); want (%v, true)", c.v, got, ok, c.want)
		}
	}
	// NaN / Inf are JS-number-invalid -> ok=false.
	for _, v := range []any{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, ok := jsNumber(v); ok {
			t.Errorf("jsNumber(%#v) should be invalid (NaN/Inf)", v)
		}
	}
	// An unrecognized kind -> invalid.
	if _, ok := jsNumber(struct{ X int }{1}); ok {
		t.Error("jsNumber(object) should be invalid")
	}
}

func TestJsNumberString(t *testing.T) {
	cases := []struct {
		s     string
		want  float64
		valid bool
	}{
		{"12", 12, true},
		{"  42  ", 42, true}, // trimmed (JS: Number("  42  ") === 42)
		{"-7.5", -7.5, true},
		{"", 0, true},                    // JS: Number("") === 0
		{"0x1F", 31, true},               // JS: Number("0x1F") === 31
		{"+0x10", 16, true},              // JS: Number("+0x10") === 16
		{"0x", 0, false},                 // JS: Number("0x") === NaN
		{"abc", 0, false},                // NaN
		{"ff", 0, false},                 // not a decimal or a 0x-hex literal
		{"0x1111111111111111", 0, false}, // >2^53: documented equivalent-to-invalid
	}
	for _, c := range cases {
		got, ok := jsNumberString(c.s)
		if ok != c.valid {
			t.Errorf("jsNumberString(%q) ok = %v; want %v", c.s, ok, c.valid)
		} else if c.valid && got != c.want {
			t.Errorf("jsNumberString(%q) = %v; want %v", c.s, got, c.want)
		}
	}
}
