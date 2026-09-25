// Package sharejstptwo is a Go port of the vendored ShareJS text-tp2 type
// (app/js/sharejs/types/text-tp2.js, ~500 LOC), oracle-verified against a
// 77-row golden table generated live from the vendored source.
//
// A document is {charLength, totalLength, positionCache, data} where data is
// a flat array of strings and tombstone counts, e.g. ["Hello ", 5, "world"].
// Ops are lists of components: a bare positive number skips N chars,
// {"i": "str"} inserts, {"i": N} inserts N tombstones, {"d": N} deletes N
// chars. Raw wire forms: op = []any of int | map[string]any.
//
// Fidelity notes (oracle-pinned):
//   - checkOp rejects: non-array ops, adjacent skips, zero/degenerate
//     components, and components without .i or .d (vendored message text).
//   - transform is STRICT: un-covered op tails become "Remaining fragments
//     in the op: ..." errors (ints print as numbers, objects as
//     "[object Object]") — the vendored transformer is NOT lenient like
//     its text.js sibling.
//   - transform has NO empty early exit and NO length-1 fast path (the
//     bootstrapTransform is never applied to text-tp2: the module exports
//     the plain type object, so [] vs [] returns []).
//   - compose(nil, op2) returns op2 untouched; compose error message is
//     "Remaining fragments in op1:" (transformer says "... in the op:").
//   - prune = transformer(op, other, goForwards=false, side=undefined).
//   - positionCache is a dead field in the vendored code (never read); kept
//     for JSON parity with the oracle.
package sharejstptwo

import (
	"errors"
	"fmt"

	"document-updater/internal/sharejstypes"
)

// Type is the vendored type name.
const Type = "text-tp2"

// OpError is a vendored checkOp / traversal error (message = vendored text).
type OpError struct{ Msg string }

func (e OpError) Error() string { return e.Msg }

// --- component field helpers (wire form: int | map[string]any) --------------

func isMapC(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func compI(c any) (any, bool) {
	m, ok := c.(map[string]any)
	if !ok {
		return nil, false
	}
	v, ok := m["i"]
	return v, ok
}

func compD(c any) (any, bool) {
	m, ok := c.(map[string]any)
	if !ok {
		return nil, false
	}
	v, ok := m["d"]
	return v, ok
}

func intVal(v any) int {
	if n, ok := v.(int); ok {
		return n
	}
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

// componentLen mirrors the vendored componentLength (skip value, i-string
// length, else i/d +ive).
func componentLen(c any) int {
	if n, ok := c.(int); ok {
		return n
	}
	m := c.(map[string]any)
	if s, ok := m["i"].(string); ok {
		return len(s)
	}
	if v, ok := m["i"]; ok {
		return intVal(v)
	}
	return intVal(m["d"])
}

// --- Doc model ----------------------------------------------------------------

// Doc is the text-tp2 document snapshot (positions are derived; the vendored
// positionCache is dead, kept for JSON parity).
type Doc struct {
	CharLength    int   `json:"charLength"`
	TotalLength   int   `json:"totalLength"`
	PositionCache []any `json:"positionCache"`
	Data          []any `json:"data"`
}

// Create mirrors type.create().
func Create() Doc {
	return Doc{CharLength: 0, TotalLength: 0, PositionCache: []any{}, Data: []any{}}
}

// Serialize mirrors type.serialize(doc) -> doc.data.
func Serialize(d *Doc) ([]any, error) {
	if d == nil || d.Data == nil {
		return nil, errors.New("invalid doc snapshot")
	}
	return d.Data, nil
}

// Deserialize mirrors type.deserialize(data).
func Deserialize(data []any) (Doc, error) {
	doc := Create()
	doc.Data = data
	for _, c := range data {
		switch v := c.(type) {
		case string:
			doc.CharLength += len(v)
			doc.TotalLength += len(v)
		case int:
			doc.TotalLength += v
		}
	}
	return doc, nil
}

// --- checkOp -------------------------------------------------------------------

// CheckOpInput mirrors the vendored checkOp over the wire op ([]any).
// CheckOpRaw mirrors the vendored entry guard (op must be an array).
func CheckOpRaw(op any) error {
	if a, ok := op.([]any); ok {
		return CheckOpInput(a)
	}
	return OpError{Msg: "Op must be an array of components"}
}
func CheckOpInput(op []any) error {
	lastWasNumber := false
	for _, c := range op {
		if n, ok := c.(int); ok {
			if n <= 0 {
				return OpError{Msg: "Skip components must be a positive number"}
			}
			if lastWasNumber {
				return OpError{Msg: "Adjacent skip components should be combined"}
			}
		} else if m, ok := c.(map[string]any); ok {
			if v, ok := m["i"]; ok {
				switch s := v.(type) {
				case string:
					if s == "" {
						return OpError{Msg: "Inserts must insert a string or a +ive number"}
					}
				case int:
					if s <= 0 {
						return OpError{Msg: "Inserts must insert a string or a +ive number"}
					}
				default:
					return OpError{Msg: "Inserts must insert a string or a +ive number"}
				}
			} else if v, ok := m["d"]; ok {
				if d, ok := v.(int); !ok || d <= 0 {
					return OpError{Msg: "Deletes must be a +ive number"}
				}
			} else {
				return OpError{Msg: "Operation component must define .i or .d"}
			}
		} else {
			return OpError{Msg: "Op components must be objects or numbers"}
		}
		lastWasNumber = !isMapC(c)
	}
	return nil
}

// --- takeDoc / appendDoc --------------------------------------------------------

func partLen(v any) int {
	switch t := v.(type) {
	case string:
		return len(t)
	case int:
		return t
	}
	return 0
}

// TakeDoc mirrors type._takeDoc (pos updated in place). maxLength nil = no
// max; tombsIndivisible mirrors the vendored flag.
func TakeDoc(d *Doc, pos *Pos, maxLength *int, tombsIndivisible bool) (any, error) {
	if pos.Index >= len(d.Data) {
		return nil, OpError{Msg: "Operation goes past the end of the document"}
	}
	part := d.Data[pos.Index]
	var result any
	switch p := part.(type) {
	case int:
		avail := p - pos.Offset
		if avail < 0 {
			avail = 0
		}
		if maxLength == nil || tombsIndivisible {
			result = avail
		} else {
			result = minInt(avail, *maxLength)
		}
	default:
		s := p.(string)
		if maxLength == nil {
			result = s[pos.Offset:]
		} else {
			j := pos.Offset + *maxLength
			if j > len(s) {
				j = len(s)
			}
			result = s[pos.Offset:j]
		}
	}
	resultLen := partLen(result)
	used := partLen(part) - pos.Offset
	if used > resultLen {
		pos.Offset += resultLen
	} else {
		pos.Index++
		pos.Offset = 0
	}
	return result, nil
}

// Pos is the mutable take position (mirrors the vendored position object).
type Pos struct{ Index, Offset int }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// AppendDoc mirrors type._appendDoc (doc mutated; p is string or int).
func AppendDoc(doc *Doc, p any) {
	if p == 0 || p == "" {
		return
	}
	if s, ok := p.(string); ok {
		doc.CharLength += len(s)
		doc.TotalLength += len(s)
	} else {
		doc.TotalLength += p.(int)
	}
	if len(doc.Data) == 0 {
		doc.Data = append(doc.Data, p)
		return
	}
	last := doc.Data[len(doc.Data)-1]
	if sameGoType(last, p) {
		if s, ok := p.(string); ok {
			doc.Data[len(doc.Data)-1] = last.(string) + s
		} else {
			doc.Data[len(doc.Data)-1] = last.(int) + p.(int)
		}
		return
	}
	doc.Data = append(doc.Data, p)
}

func sameGoType(a, b any) bool {
	_, as := a.(string)
	_, bs := b.(string)
	if as != bs {
		return false
	}
	if as {
		return true
	}
	_, ai := a.(int)
	_, bi := b.(int)
	return ai && bi
}

// --- apply ----------------------------------------------------------------------

// Apply mirrors type.apply (original doc unmodified). rawOp is the vendored
// op (any: slice for valid ops; a non-slice trips the vendored checkOp
// "Op must be an array of components"). doc == nil models the vendored
// "field absent" check -> "Snapshot is invalid".
func Apply(doc *Doc, op any) (Doc, error) {
	if doc == nil || doc.Data == nil {
		return Doc{}, OpError{Msg: "Snapshot is invalid"}
	}
	opSlice, ok := op.([]any)
	if !ok {
		return Doc{}, OpError{Msg: "Op must be an array of components"}
	}
	if err := CheckOpInput(opSlice); err != nil {
		return Doc{}, err
	}
	op = opSlice
	newDoc := Create()
	pos := Pos{0, 0}
	for _, component := range opSlice {
		if n, ok := component.(int); ok {
			rem := n
			for rem > 0 {
				part, err := TakeDoc(doc, &pos, &rem, false)
				if err != nil {
					return Doc{}, err
				}
				AppendDoc(&newDoc, part)
				rem -= partLen(part)
			}
		} else if m, ok := component.(map[string]any); ok {
			if v, ok := m["i"]; ok {
				AppendDoc(&newDoc, numGo(v))
			} else if v, ok := m["d"]; ok {
				rem := v.(int)
				for rem > 0 {
					part, err := TakeDoc(doc, &pos, &rem, false)
					if err != nil {
						return Doc{}, err
					}
					rem -= partLen(part)
				}
				AppendDoc(&newDoc, v)
			}
		}
	}
	return newDoc, nil
}

func numGo(v any) any {
	if n, ok := v.(float64); ok && n == float64(int(n)) {
		return int(n)
	}
	return v
}

// --- op-level _append -------------------------------------------------------------

func isZeroComponent(c any) bool {
	if n, ok := c.(int); ok {
		return n == 0
	}
	m, ok := c.(map[string]any)
	if !ok {
		return false
	}
	if s, ok := m["i"]; ok {
		if s2, ok := s.(string); ok {
			return s2 == ""
		}
		if n, ok := s.(int); ok {
			return n == 0
		}
	}
	if n, ok := m["d"]; ok {
		if n2, ok := n.(int); ok {
			return n2 == 0
		}
	}
	return false
}

// AppendOp mirrors type._append (vendored op-level merge rules: skip+skip,
// same-kind i, any d; zero-valued components are no-ops).
func AppendOp(op *[]any, component any) {
	if isZeroComponent(component) {
		return
	}
	if len(*op) == 0 {
		*op = append(*op, component)
		return
	}
	last := (*op)[len(*op)-1]
	if cn, ok := component.(int); ok {
		if ln, ok := last.(int); ok {
			(*op)[len(*op)-1] = ln + cn
			return
		}
	} else if m, ok := component.(map[string]any); ok {
		ml, ok := last.(map[string]any)
		if ok {
			if ci, cok := m["i"]; cok {
				if li, lok := ml["i"]; lok && li != nil && sameGoType(li, ci) {
					ml["i"] = mergeI(li, ci)
					return
				}
			}
			if cd, dok := m["d"]; dok {
				if ld, lok := ml["d"]; lok && ld != nil {
					ml["d"] = intVal(ld) + intVal(cd)
					return
				}
			}
		}
	}
	*op = append(*op, component)
}

func mergeI(a, b any) any {
	if s, ok := a.(string); ok {
		return s + b.(string)
	}
	return a.(int) + b.(int)
}

// Normalize mirrors type.normalize (re-emit through the _append merge rules).
func Normalize(op []any) []any {
	newOp := []any{}
	for _, c := range op {
		AppendOp(&newOp, c)
	}
	return newOp
}

// --- makeTake -----------------------------------------------------------------

// OpTaker mirrors the vendored makeTake closure: stateful take of the op,
// returning fresh component values (source op unmodified).
type OpTaker struct {
	op     []any
	index  int
	offset int
}

func newOpTaker(op []any) *OpTaker { return &OpTaker{op: op} }

// PeekHasI mirrors __guard__(peek(), x => x.i) !== undefined: the next
// element is a map with a defined i key (string or tombstone count both
// count — the guard tests key presence, not type).
func (t *OpTaker) PeekHasI() bool {
	if t.index >= len(t.op) {
		return false
	}
	e := t.op[t.index]
	m, ok := e.(map[string]any)
	if !ok {
		return false
	}
	_, ok = m["i"]
	return ok
}

// hasIKey mirrors `e.i !== undefined` on a component map.
func (t OpTaker) valueOf(e any) int {
	if n, ok := e.(int); ok {
		return n
	}
	m := e.(map[string]any)
	if v, ok := m["i"]; ok {
		return intVal(v)
	}
	return intVal(m["d"])
}

// numeric takes the value-kind element (bare skip, {i:N}, or {d:N}).
func (t *OpTaker) takeNumeric(maxLength *int, insertsIndivisible bool) any {
	e := t.op[t.index]
	cur := t.valueOf(e)
	indiv := insertsIndivisible && isMapI(e)
	if maxLength == nil || cur-t.offset <= *maxLength || indiv {
		c := cur - t.offset
		t.index++
		t.offset = 0
		return t.finish(e, c)
	}
	t.offset += *maxLength
	return t.finish(e, *maxLength)
}

// finish mirrors `e.i !== undefined ? {i: c} : e.d !== undefined ? {d: c} : c`.
func (t *OpTaker) finish(e any, c int) any {
	if n, ok := e.(int); ok {
		_ = n
		return c
	}
	m := e.(map[string]any)
	if hasIKey(m) {
		return map[string]any{"i": c}
	}
	return map[string]any{"d": c}
}

func isMapI(e any) bool {
	m, ok := e.(map[string]any)
	if !ok {
		return false
	}
	_, ok = m["i"]
	return ok
}

// Take mirrors the vendored take(maxlength, insertsIndivisible).
// maxLength nil = no max. ok=false mirrors the vendored null return (op
// fully consumed).
func (t *OpTaker) Take(maxLength *int, insertsIndivisible bool) (any, bool) {
	if t.index >= len(t.op) {
		return nil, false
	}
	e := t.op[t.index]
	if n, ok := e.(int); ok {
		_ = n
		return t.takeNumeric(maxLength, insertsIndivisible), true
	}
	m := e.(map[string]any)
	numerical := false
	if v, ok := m["i"]; ok {
		if isIntVal(v) {
			numerical = true
		}
	}
	if hasDKey(m) {
		numerical = true
	}
	if numerical {
		return t.takeNumeric(maxLength, insertsIndivisible), true
	}
	// String insert.
	str := m["i"].(string)
	if maxLength == nil || len(str)-t.offset <= *maxLength || insertsIndivisible {
		out := map[string]any{"i": str[t.offset:]}
		t.index++
		t.offset = 0
		return out, true
	}
	end := minInt(t.offset+*maxLength, len(str))
	out := map[string]any{"i": str[t.offset:end]}
	t.offset += *maxLength
	return out, true
}

func hasIKey(m map[string]any) bool {
	_, ok := m["i"]
	return ok
}

// --- transformer / transform / prune ----------------------------------------------

// Transform mirrors type.transform (goForwards=true; side 'left'/'right').
func Transform(op, otherOp []any, side string) ([]any, error) {
	if side != "left" && side != "right" {
		return nil, fmt.Errorf("side (%v) should be 'left' or 'right'", side)
	}
	return transformer(op, otherOp, true, side)
}

// Prune mirrors type.prune (goForwards=false, side undefined).
func Prune(op, otherOp []any) ([]any, error) {
	return transformer(op, otherOp, false, "")
}

func fragmentsMsg(tag string, chunk any) string {
	if n, ok := chunk.(int); ok {
		return fmt.Sprintf("%s: %d", tag, n)
	}
	return tag + ": [object Object]"
}

func transformer(op, otherOp []any, goForwards bool, side string) ([]any, error) {
	if err := CheckOpInput(op); err != nil {
		return nil, err
	}
	if err := CheckOpInput(otherOp); err != nil {
		return nil, err
	}
	newOp := []any{}
	tk := newOpTaker(op)
	for _, component := range otherOp {
		length := componentLen(component)
		if v, ok := compI(component); ok && v != nil {
			// Insert (string or tombstone count).
			if goForwards {
				if side == "left" {
					for tk.PeekHasI() {
						c, _ := tk.Take(nil, false)
						AppendOp(&newOp, c)
					}
				}
				AppendOp(&newOp, length)
			} else {
				rem := length
				for rem > 0 {
					chunk, ok := tk.Take(&rem, true)
					if !ok {
						return nil, OpError{Msg: "The transformed op is invalid"}
					}
					if m, ok := chunk.(map[string]any); ok {
						if hasDKey(m) {
							return nil, OpError{Msg: "The transformed op deletes locally inserted characters - it cannot be purged of the insert."}
						}
					}
					if n, ok := chunk.(int); ok {
						rem -= n
					} else {
						AppendOp(&newOp, chunk)
					}
				}
			}
		} else {
			// Skip or delete.
			rem := length
			for rem > 0 {
				chunk, ok := tk.Take(&rem, true)
				if !ok {
					return nil, OpError{Msg: "The op traverses more elements than the document has"}
				}
				AppendOp(&newOp, chunk)
				if isIntOrNoI(chunk) {
					rem -= componentLen(chunk)
				}
			}
		}
	}
	for {
		component, ok := tk.Take(nil, false)
		if !ok {
			break
		}
		if m, ok := component.(map[string]any); ok {
			if hasIKey(m) {
				AppendOp(&newOp, component)
				continue
			}
		}
		return nil, OpError{Msg: fragmentsMsg("Remaining fragments in the op", component)}
	}
	return newOp, nil
}

func hasDKey(m map[string]any) bool {
	_, ok := m["d"]
	return ok
}

// isIntOrNoI mirrors the vendored `!chunk.i`: skip chunks and {d:-} chunks
// (and nothing else reachable from Take have i) decrement the length.
func isIntOrNoI(chunk any) bool {
	m, ok := chunk.(map[string]any)
	return !ok || !hasIKey(m)
}

// --- compose -----------------------------------------------------------------------

// Compose mirrors type.compose. op1 == nil models the vendored
// `op1 === null || op1 === undefined` early return (op2 returned as-is).
func Compose(op1, op2 []any) ([]any, error) {
	if op1 == nil {
		return op2, nil
	}
	if err := CheckOpInput(op1); err != nil {
		return nil, err
	}
	if err := CheckOpInput(op2); err != nil {
		return nil, err
	}
	result := []any{}
	tk := newOpTaker(op1)
	for _, component := range op2 {
		if n, ok := component.(int); ok {
			rem := n
			for rem > 0 {
				chunk, ok := tk.Take(&rem, false)
				if !ok {
					return nil, OpError{Msg: "The op traverses more elements than the document has"}
				}
				AppendOp(&result, chunk)
				rem -= componentLen(chunk)
			}
		} else if m, ok := component.(map[string]any); ok {
			if v, ok := m["i"]; ok {
				AppendOp(&result, map[string]any{"i": v})
			} else if v, ok := m["d"]; ok {
				rem := v.(int)
				for rem > 0 {
					chunk, ok := tk.Take(&rem, false)
					if !ok {
						return nil, OpError{Msg: "The op traverses more elements than the document has"}
					}
					cl := componentLen(chunk)
					if mm, ok := chunk.(map[string]any); ok && hasIKey(mm) {
						AppendOp(&result, map[string]any{"i": cl})
					} else {
						AppendOp(&result, map[string]any{"d": cl})
					}
					rem -= cl
				}
			}
		}
	}
	for {
		component, ok := tk.Take(nil, false)
		if !ok {
			break
		}
		if m, ok := component.(map[string]any); ok {
			if hasIKey(m) {
				AppendOp(&result, component)
				continue
			}
		}
		return nil, OpError{Msg: fragmentsMsg("Remaining fragments in op1", component)}
	}
	return result, nil
}

func isIntVal(v any) bool {
	_, ok := v.(int)
	return ok
}

// --- registry adapter -----------------------------------------------------------

// TP2Type adapts the vendored text-tp2 type to the sharejstypes registry
// (chunk 5 model consumes types only through that interface).
type TP2Type struct{}

func init() { sharejstypes.Register(Type, TP2Type{}) }

func (TP2Type) Name() string { return Type }

func (TP2Type) Create() any { return Create() }

func (TP2Type) Apply(snapshot any, op any) (any, error) {
	s, ok := snapshot.(Doc)
	if !ok {
		return nil, fmt.Errorf("text-tp2: snapshot must be Doc")
	}
	return Apply(&s, op)
}

func (TP2Type) Transform(op any, otherOp any, side string) (any, error) {
	a, ok1 := op.([]any)
	b, ok2 := otherOp.([]any)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("text-tp2: ops must be []any")
	}
	return Transform(a, b, side)
}
