// Package sharejsjson is a 1:1 Go port of the vendored ShareJS JSON type
// (app/js/sharejs/types/json.js) plus the bootstrap (app/js/sharejs/types/
// helpers.js).
//
// Oracle battery: /tmp/js_oracle_json.js (46+ rows) + the transformComponent
// branch probes (P-*, B-*, C-* rows) captured from the vendored source on
// 2026-07-08. Every branch of transformComponent was verified line-by-line
// against the vendored source and the oracle rows pin the behaviour.
//
// Values: snapshots and op payloads are raw JSON values (map[string]any,
// []any, float64, string, bool, nil). Path segments (Component.P) are int
// (list offsets; character offsets in strings) or string (object keys).
//
// Documented deviations from the Node oracle:
//   - JS TypeErrors thrown while navigating through a missing value inside
//     Apply (e.g. apply(1, [{p:[0]}])) surface in Go as the cleaner
//     "Referenced element not a number" / "Referenced element not a string"
//     errors. Pure arithmetic input (the model path) is unaffected.
//   - JSON.stringify of an undefined element is "undefined" in JS; Go uses
//     "null" for absent values. Oracle rows exercise concrete values only.
//   - Object/array payload identity (JS === reference equality on li/ld) is
//     approximated with value equality.
package sharejsjson

import (
	"encoding/json"
	"fmt"

	sharejstypes "document-updater/internal/sharejstypes"
)

const Name = "json"

// Component is one op component. Nil pointer means the field is absent (JS
// undefined). Path segments may be int or string.
type Component struct {
	P  []any
	NA *float64
	SI *string
	SD *string
	LI *any
	LD *any
	LM *int
	OI *any
	OD *any
}

func cloneVal(v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[k] = cloneVal(val)
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, val := range t {
			s[i] = cloneVal(val)
		}
		return s
	default:
		return v
	}
}

func cloneComponent(c Component) Component {
	out := Component{P: make([]any, len(c.P))}
	for i, seg := range c.P {
		out.P[i] = cloneVal(seg)
	}
	if c.NA != nil {
		n := *c.NA
		out.NA = &n
	}
	if c.SI != nil {
		s := *c.SI
		out.SI = &s
	}
	if c.SD != nil {
		s := *c.SD
		out.SD = &s
	}
	if c.LI != nil {
		v := cloneVal(*c.LI)
		out.LI = &v
	}
	if c.LD != nil {
		v := cloneVal(*c.LD)
		out.LD = &v
	}
	if c.LM != nil {
		l := *c.LM
		out.LM = &l
	}
	if c.OI != nil {
		v := cloneVal(*c.OI)
		out.OI = &v
	}
	if c.OD != nil {
		v := cloneVal(*c.OD)
		out.OD = &v
	}
	return out
}

func segEq(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	if as, ok := a.(string); ok {
		bs, ok := b.(string)
		return ok && as == bs
	}
	if an, ok := a.(int); ok {
		bn, ok := b.(int)
		return ok && an == bn
	}
	return a == b
}

// segLe/segLt mirror JS <= and < on path segments. JS compares strings
// lexicographically and numbers numerically; mixed types are never exercised
// by the oracle (JS coercion is not ported).
func segLe(a, b any) bool {
	if as, ok := a.(string); ok {
		bs, ok := b.(string)
		return ok && as <= bs
	}
	an, ok := a.(int)
	bn, ok2 := b.(int)
	return ok && ok2 && an <= bn
}

func segLt(a, b any) bool {
	if as, ok := a.(string); ok {
		bs, ok := b.(string)
		return ok && as < bs
	}
	an, ok := a.(int)
	bn, ok2 := b.(int)
	return ok && ok2 && an < bn
}

func segAt(p []any, i int) any {
	if i < 0 || i >= len(p) {
		return nil
	}
	return p[i]
}

func segEqAt(p []any, i int, v any) bool { return segEq(segAt(p, i), v) }

func segEq2(p1, p2 []any, i int) bool { return segEq(segAt(p1, i), segAt(p2, i)) }

func segInc(p []any, i int) {
	n, ok := segIntSafe(p, i)
	if ok {
		p[i] = n + 1
	} else if i >= 0 && i < len(p) {
		// JS `'a'++` -> NaN -> JSON null (oracle-preserving artifact).
		p[i] = nil
	}
}

func segDec(p []any, i int) {
	n, ok := segIntSafe(p, i)
	if ok {
		p[i] = n - 1
	} else if i >= 0 && i < len(p) {
		p[i] = nil
	}
}

func segInt(p []any, i int) (int, bool) {
	n, ok := p[i].(int)
	return n, ok
}

// CommonPath mirrors json.commonPath (oracle L269-287). ok=false stands in
// for JS `undefined`; the -1 returned when p2 is empty is a real value
// (ok=true) and flows into the transform chain exactly like the JS value.
func CommonPath(p1, p2 []any) (int, bool) {
	a := make([]any, 0, len(p1)+1)
	a = append(a, "data")
	a = append(a, p1...)
	a = a[:len(a)-1]
	b := make([]any, 0, len(p2)+1)
	b = append(b, "data")
	b = append(b, p2...)
	b = b[:len(b)-1]
	if len(b) == 0 {
		return -1, true
	}
	i := 0
	for i < len(a) && i < len(b) && segEq(a[i], b[i]) {
		i++
		if i == len(b) {
			return i - 1, true
		}
	}
	return 0, false
}

func PathMatches(p1, p2 []any, ignoreLast bool) bool {
	if len(p1) != len(p2) {
		return false
	}
	for i := range p1 {
		if !segEq(p1[i], p2[i]) {
			if ignoreLast && i == len(p1)-1 {
				continue
			}
			return false
		}
	}
	return true
}

// Append mirrors json.append (oracle L197-233): clones c and folds it into
// dest applying the vendored merge rules.
func Append(dest *[]Component, c Component) {
	c = cloneComponent(c)
	if len(*dest) == 0 {
		*dest = append(*dest, c)
		return
	}
	last := (*dest)[len(*dest)-1]
	if !PathMatches(c.P, last.P, false) {
		*dest = append(*dest, c)
		return
	}
	if last.NA != nil && c.NA != nil {
		na := *last.NA + *c.NA
		*dest = append((*dest)[:len(*dest)-1], Component{P: last.P, NA: &na})
		return
	}
	if last.LI != nil && c.LI == nil && c.LD != nil && *c.LD == *last.LI {
		if last.LD != nil {
			last.LI = nil
			*dest = append((*dest)[:len(*dest)-1], last)
		} else {
			*dest = (*dest)[:len(*dest)-1]
		}
		return
	}
	if last.OD != nil && last.OI == nil && c.OI != nil && c.OD == nil {
		last.OI = c.OI
		*dest = append((*dest)[:len(*dest)-1], last)
		return
	}
	if c.LM != nil && len(c.P) > 0 && c.P[len(c.P)-1] == *c.LM {
		return
	}
	*dest = append(*dest, c)
}

func Compose(op1, op2 []Component) []Component {
	CheckValidOp(op1)
	CheckValidOp(op2)
	// Vendored: newOp = clone(op1); for c of op2: append(newOp, c).
	newOp := make([]Component, 0, len(op1))
	for i := range op1 {
		newOp = append(newOp, cloneComponent(op1[i]))
	}
	for i := range op2 {
		Append(&newOp, op2[i])
	}
	return newOp
}

func Normalize(op []Component) []Component {
	newOp := []Component{}
	for i := range op {
		c := op[i]
		if c.P == nil {
			c.P = []any{}
		}
		Append(&newOp, c)
	}
	return newOp
}

func CheckValidOp(_ []Component) {}

// --- apply (oracle L82-175) --------------------------------------------------

func isNumberVal(v any) bool {
	switch v.(type) {
	case float64, int, int64:
		return true
	}
	return false
}

func numFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}

func lookupVal(elem any, key any) (any, bool) {
	switch t := elem.(type) {
	case map[string]any:
		k, ok := key.(string)
		if !ok {
			k = fmt.Sprint(key)
		}
		v, ok := t[k]
		return v, ok
	case []any:
		n, ok := key.(int)
		if !ok || n < 0 || n >= len(t) {
			return nil, false
		}
		return t[n], true
	default:
		// elem is a primitive (string, number, null): JS property access on a
		// primitive is undefined (Go deviates from JS TypeErrors here).
		return nil, false
	}
}

func assignVal(elem any, key any, val any) {
	switch t := elem.(type) {
	case map[string]any:
		k, ok := key.(string)
		if !ok {
			k = fmt.Sprint(key)
		}
		t[k] = val
	case []any:
		n, ok := key.(int)
		if ok && n >= 0 && n < len(t) {
			t[n] = val
		}
	}
}

func deleteVal(elem any, key any) {
	t, ok := elem.(map[string]any)
	if !ok {
		return
	}
	k, okStr := key.(string)
	if !okStr {
		k = fmt.Sprint(key)
	}
	delete(t, k)
}

func jsSliceStr(s string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if start > len(s) {
		start = len(s)
	}
	if end > len(s) {
		end = len(s)
	}
	if end < start {
		end = start
	}
	return s[start:end]
}

func jsonStringify(v any) string {
	switch t := v.(type) {
	case string:
		b, _ := json.Marshal(t)
		return string(b)
	case nil:
		return "null"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func checkList(elem any) error {
	if _, ok := elem.([]any); !ok {
		return fmt.Errorf("Referenced element not a list")
	}
	return nil
}

func checkObj(elem any) error {
	if _, ok := elem.(map[string]any); !ok {
		return fmt.Errorf("Referenced element not an object (it was %s)", jsonStringify(elem))
	}
	return nil
}

func segPos(c Component) int {
	// The op position is the (numeric) key of the value: the last path
	// segment. String/non-numeric segments map to 0 (JS coerces numbers; for
	// the invalid-list/string op cases the oracle only exercises ints).
	if n, ok := segIntSafe(c.P, len(c.P)-1); ok {
		return n
	}
	return 0
}

// Apply mirrors json.apply: each component mutates {data: clone(snapshot)}
// and container.data is returned.
func Apply(snapshot any, op []Component) (any, error) {
	CheckValidOp(op)
	container := map[string]any{"data": cloneVal(snapshot)}
	for i := range op {
		c := op[i]
		var parent any
		var parentkey any
		elem := any(container)
		var key any
		key = "data"
		for _, p := range c.P {
			parent = elem
			parentkey = key
			e, ok := lookupVal(elem, key)
			if !ok {
				elem = nil
			} else {
				elem = e
			}
			key = p
		}
		switch {
		case c.NA != nil:
			v, ok := lookupVal(elem, key)
			if !ok || !isNumberVal(v) {
				return nil, fmt.Errorf("Referenced element not a number")
			}
			assignVal(elem, key, numFloat(v)+*c.NA)
		case c.SI != nil:
			s, ok := elem.(string)
			if !ok {
				return nil, fmt.Errorf("Referenced element not a string (it was %s)", jsonStringify(elem))
			}
			pos := segPos(c)
			assignVal(parent, parentkey, jsSliceStr(s, 0, pos)+*c.SI+jsSliceStr(s, pos, len(s)))
		case c.SD != nil:
			s, ok := elem.(string)
			if !ok {
				return nil, fmt.Errorf("Referenced element not a string")
			}
			pos := segPos(c)
			if jsSliceStr(s, pos, pos+len(*c.SD)) != *c.SD {
				return nil, fmt.Errorf("Deleted string does not match")
			}
			assignVal(parent, parentkey, jsSliceStr(s, 0, pos)+jsSliceStr(s, pos+len(*c.SD), len(s)))
		case c.LI != nil && c.LD != nil:
			if err := checkList(elem); err != nil {
				return nil, err
			}
			assignVal(elem, key, cloneVal(*c.LI))
		case c.LI != nil:
			if err := checkList(elem); err != nil {
				return nil, err
			}
			l, _ := elem.([]any)
			pos := segPos(c)
			next := make([]any, 0, len(l)+1)
			next = append(next, l[:pos]...)
			next = append(next, cloneVal(*c.LI))
			next = append(next, l[pos:]...)
			assignVal(parent, parentkey, next)
		case c.LD != nil:
			if err := checkList(elem); err != nil {
				return nil, err
			}
			l, _ := elem.([]any)
			pos := segPos(c)
			if pos < 0 {
				pos = 0
			}
			if pos >= len(l) {
				// JS splice clamps: deleting past the end is a no-op.
				break
			}
			next := make([]any, 0, len(l))
			next = append(next, l[:pos]...)
			next = append(next, l[pos+1:]...)
			assignVal(parent, parentkey, next)
		case c.LM != nil:
			if err := checkList(elem); err != nil {
				return nil, err
			}
			l, _ := elem.([]any)
			pos := segPos(c)
			if pos == *c.LM {
				break // c.lm === key: noop.
			}
			if pos < 0 {
				pos = 0
			}
			var moved any
			if pos < len(l) {
				moved = l[pos]
			}
			next := make([]any, 0, len(l))
			next = append(next, l[:pos]...)
			if pos < len(l) {
				next = append(next, l[pos+1:]...)
			}
			to := *c.LM
			if to < 0 {
				to = 0
			}
			if to > len(next) {
				to = len(next)
			}
			next = append(next[:to], append([]any{moved}, next[to:]...)...)
			assignVal(parent, parentkey, next)
		case c.OI != nil:
			if err := checkObj(elem); err != nil {
				return nil, err
			}
			assignVal(elem, key, cloneVal(*c.OI))
		case c.OD != nil:
			if err := checkObj(elem); err != nil {
				return nil, err
			}
			deleteVal(elem, key)
		default:
			return nil, fmt.Errorf("invalid / missing instruction in op")
		}
	}
	return container["data"], nil
}

// --- invert (oracle L32-60) ---------------------------------------------------

func invertComponent(c Component) Component {
	out := Component{P: c.P}
	if c.SI != nil {
		out.SD = c.SI
	}
	if c.SD != nil {
		out.SI = c.SD
	}
	if c.OI != nil {
		out.OD = c.OI
	}
	if c.OD != nil {
		out.OI = c.OD
	}
	if c.LI != nil {
		out.LD = c.LI
	}
	if c.LD != nil {
		out.LI = c.LD
	}
	if c.NA != nil {
		n := -(*c.NA)
		out.NA = &n
	}
	if c.LM != nil {
		l, _ := segIntSafe(c.P, len(c.P)-1)
		out.LM = &l
		p := make([]any, 0, len(c.P))
		p = append(p, c.P[:len(c.P)-1]...)
		p = append(p, *c.LM)
		out.P = p
	}
	return out
}

func Invert(op []Component) []Component {
	out := make([]Component, 0, len(op))
	for i := len(op) - 1; i >= 0; i-- {
		out = append(out, invertComponent(op[i]))
	}
	return out
}

// JSONType adapts the json type to the sharejstypes registry (chunk 5
// model consumes types only through that interface).
type JSONType struct{}

func init() { sharejstypes.Register(Name, JSONType{}) }

func (JSONType) Name() string { return Name }

func (JSONType) Create() any { return nil }

func (JSONType) Apply(snapshot any, op any) (any, error) {
	opList, ok := op.([]Component)
	if !ok {
		return nil, fmt.Errorf("json: op must be []sharejsjson.Component")
	}
	return Apply(snapshot, opList)
}

func (JSONType) Transform(op any, otherOp any, side string) (any, error) {
	a, ok1 := op.([]Component)
	b, ok2 := otherOp.([]Component)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("json: ops must be []sharejsjson.Component")
	}
	return Transform(a, b, side)
}
