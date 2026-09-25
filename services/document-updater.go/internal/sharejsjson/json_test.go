// Golden tests: 96 rows captured live from the vendored Node oracle on
// 2026-07-08 (generator: /tmp/gen_golden2.js, run against the vendored
// app/js/sharejs/types/json.js). The expected values are the oracle's
// verbatim JSON output.

package sharejsjson

import (
	"encoding/json"
	"testing"
)

func decodeComponent(m map[string]any) Component {
	c := Component{}
	if p, ok := m["p"].([]any); ok {
		c.P = make([]any, len(p))
		for i, s := range p {
			if f, ok := s.(float64); ok {
				c.P[i] = int(f)
			} else if s != nil {
				c.P[i] = s
			}
		}
	}
	if v, ok := m["na"]; ok {
		f := toFloat(v)
		c.NA = &f
	}
	if v, ok := m["si"]; ok {
		s := v.(string)
		c.SI = &s
	}
	if v, ok := m["sd"]; ok {
		s := v.(string)
		c.SD = &s
	}
	if v, ok := m["li"]; ok {
		x := v
		c.LI = &x
	}
	if v, ok := m["ld"]; ok {
		x := toVal(v)
		c.LD = &x
	}
	if v, ok := m["lm"]; ok {
		n := int(toFloat(v))
		c.LM = &n
	}
	if v, ok := m["oi"]; ok {
		x := toVal(v)
		c.OI = &x
	}
	if v, ok := m["od"]; ok {
		x := toVal(v)
		c.OD = &x
	}
	return c
}

func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	default:
		return 0
	}
}

func toVal(v any) any { return v }

func decodeComponents(v any) []Component {
	arr, _ := v.([]any)
	out := make([]Component, len(arr))
	for i, e := range arr {
		m, _ := e.(map[string]any)
		out[i] = decodeComponent(m)
	}
	return out
}

func componentToAny(c Component) map[string]any {
	m := map[string]any{"p": c.P}
	if c.NA != nil {
		m["na"] = *c.NA
	}
	if c.SI != nil {
		m["si"] = *c.SI
	}
	if c.SD != nil {
		m["sd"] = *c.SD
	}
	if c.LI != nil {
		m["li"] = *c.LI
	}
	if c.LD != nil {
		m["ld"] = *c.LD
	}
	if c.LM != nil {
		m["lm"] = *c.LM
	}
	if c.OI != nil {
		m["oi"] = *c.OI
	}
	if c.OD != nil {
		m["od"] = *c.OD
	}
	return m
}

func opToAny(op []Component) []any {
	out := make([]any, 0, len(op))
	for _, c := range op {
		out = append(out, componentToAny(c))
	}
	return out
}

func normalizeAny(v any) any {
	// Normalize numeric representations for DeepEqual: float64 <-> int.
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeAny(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = normalizeAny(t[i])
		}
		return t
	case float64:
		if f := float64(int(t)); f == t {
			return float64(int(t)) // keep float64; comparison side also float64
		}
		return t
	default:
		return v
	}
}

func TestGolden(t *testing.T) {
	for _, r := range goldenRows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			var input any
			if err := json.Unmarshal([]byte(r.input), &input); err != nil {
				t.Fatalf("bad input: %v", err)
			}
			var got any
			var gotErr string
			switch r.kind {
			case "create":
				got = nil
			case "apply":
				arr := input.([]any)
				snap := toAnyValue(arr[0])
				comp := decodeComponent(compMapInput(arr[1]))
				res, err := Apply(snap, []Component{comp})
				if err != nil {
					gotErr = err.Error()
				} else {
					got = res
				}
			case "invert":
				got = opToAny(Invert(decodeComponents(input)))
			case "normalize":
				got = opToAny(Normalize(decodeComponents(input)))
			case "compose":
				arr := input.([]any)
				// The generator stored ops as JSON-encoded strings.
				op1 := decodeOpString(arr[0].(string))
				op2 := decodeOpString(arr[1].(string))
				got = opToAny(Compose(op1, op2))
			case "transform":
				arr := input.([]any)
				c := decodeComponent(compMapInput(arr[0]))
				o := decodeComponent(compMapInput(arr[1]))
				side := arr[2].(string)
				res, err := Transform([]Component{c}, []Component{o}, side)
				if err != nil {
					gotErr = err.Error()
				} else {
					got = opToAny(res)
				}
			case "transform-err":
				arr := input.([]any)
				// Shape: [component, []|component, side] — arr[1] is the whole
				// (possibly empty) other op, or a single component.
				op1 := []Component{decodeComponent(compMapInput(arr[0]))}
				var op2 []Component
				if a, ok := arr[1].([]any); ok {
					op2 = decodeComponents(a)
				} else {
					op2 = []Component{decodeComponent(compMapInput(arr[1]))}
				}
				side := arr[2].(string)
				res, err := Transform(op1, op2, side)
				if err != nil {
					gotErr = err.Error()
				} else {
					got = opToAny(res)
				}
			case "transformX":
				arr := input.([]any)
				l, r, err := TransformX(decodeComponents(toArray(arr[0])), decodeComponents(toArray(arr[1])))
				if err != nil {
					gotErr = err.Error()
				} else {
					got = []any{opToAny(l), opToAny(r)}
				}
			case "tfa":
				arr := input.([]any)
				c := decodeComponent(compMapInput(arr[0]))
				other := decodeComponent(compMapInput(arr[1]))
				side := arr[2].(string)
				var dest []Component
				if err := TransformComponent(&dest, c, other, side); err != nil {
					gotErr = err.Error()
				} else {
					got = opToAny(dest)
				}
			default:
				t.Fatalf("unknown kind %s", r.kind)
			}

			if gotErr != "" {
				wantErr := r.expected[len("ERR:"):]
				if gotErr != wantErr {
					t.Fatalf("error = %q, want %q", gotErr, wantErr)
				}
				return
			}
			if r.expected == "nil" {
				if got != nil {
					t.Fatalf("got %v, want nil", got)
				}
				return
			}
			var want any
			if err := json.Unmarshal([]byte(r.expected), &want); err != nil {
				t.Fatalf("bad expected: %v", err)
			}
			if !deepEqAny(got, normalizeAny(want)) {
				gb, _ := json.Marshal(got)
				t.Fatalf("got %s, want %s", gb, r.expected)
			}
		})
	}
}

func compMapInput(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func toArray(v any) []any {
	a, _ := v.([]any)
	return a
}

// toAnyValue keeps apply-input values in the same Go shape as decode:
// numbers float64, strings, maps, arrays, nil.
func toAnyValue(v any) any {
	if b, err := json.Marshal(v); err == nil {
		var x any
		_ = json.Unmarshal(b, &x)
		return x
	}
	return nil
}

func deepEqAny(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			if !deepEqAny(v, y[k]) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !deepEqAny(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	// Numbers: compare as float64.
	af, aok := toNumber(a)
	bf, bok := toNumber(b)
	if aok && bok {
		return af == bf
	}
	return asString(a) == asString(b)
}

func toNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case bool:
		return 0, false
	default:
		if v == nil {
			return 0, false
		}
		// map/slice: not numeric.
		if fm, ok := v.(map[string]any); ok {
			_ = fm
			return 0, false
		}
		if sl, ok := v.([]any); ok {
			_ = sl
			return 0, false
		}
		return 0, false
	}
}

func asString(v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	case string:
		return t
	case map[string]any:
		b, _ := json.Marshal(v)
		return string(b)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func decodeOpString(v string) []Component {
	var raw []any
	if err := json.Unmarshal([]byte(v), &raw); err != nil {
		return nil
	}
	var out []Component
	for _, e := range raw {
		out = append(out, decodeComponent(compMapAny(e)))
	}
	return out
}

func compMapAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
