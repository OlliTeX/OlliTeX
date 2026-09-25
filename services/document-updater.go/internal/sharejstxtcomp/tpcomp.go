package sharejstxtcomp

// Port of the vendored ShareJS text-composable type
// (app/js/sharejs/types/text-composable.js, ~400 LOC). "An alternate
// composable implementation for text ... closer to the implementation used
// by google wave."
//
//   Snapshots are plain strings. Ops are lists of components iterating over
//   the whole document:
//     N      skip N characters
//     {i:s}  insert string s
//     {d:s}  delete string s (matching s in the document)
//
// Wire values carried by this package: int (skip) and map[string]any with
// an "i" or a "d" string field. The vendored debug functions p/i are no-ops,
// so its error strings show a bare "undefined" and are pinned verbatim by
// the oracle golden table. The vendored transform/compose are STRICT (the
// bootstrap from types/helpers.js is never applied here and there is no
// length-1 fast path).

import (
	"errors"

	"document-updater/internal/sharejstypes"
)

// Type is the registry name (vendored name).
const Type = "text-composable"

func init() { sharejstypes.Register(Type, TPCompType{}) }

// --- component field access (mirror the vendored c.i / c.d reads) ----------

// iField returns the component's "i" string and whether the "i" key is
// present (JS: component.i != null && typeof string).
func iField(v any) (string, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", false
	}
	s, ok := m["i"].(string)
	return s, ok
}

func dField(v any) (string, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return "", false
	}
	s, ok := m["d"].(string)
	return s, ok
}

func isSkip(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case float64: // wire JSON numbers
		return int(t), true
	}
	return 0, false
}

func componentLength(c any) int {
	if n, ok := isSkip(c); ok {
		return n
	}
	if s, ok := iField(c); ok {
		return len(s)
	}
	if s, ok := dField(c); ok {
		return len(s)
	}
	return 0
}

// --- checkOp ---------------------------------------------------------------

// CheckOp mirrors the vendored checkOp (not exported by the vendored
// module but invoked by apply/transform/compose). Vendored branches:
//
//	object  -> i or d must carry a non-empty string, else
//	           "Invalid op component: undefined" (vendored i() is a no-op
//	           stub, hence the bare "undefined")
//	!number -> "Op components must be objects or numbers"
//	            (this runs before the positivity check)
//	number  -> must be > 0 and not preceded by another skip
func CheckOp(op []any) error {
	if op == nil {
		return errors.New("Op must be an array of components")
	}
	var last any
	for _, c := range op {
		if n, ok := isSkip(c); ok {
			if n <= 0 {
				return errors.New("Skip components must be a positive number")
			}
			if _, okN := isSkip(last); okN {
				return errors.New("Adjacent skip components should be added")
			}
		} else if m, ok := c.(map[string]any); ok {
			_ = m
			if !componentValid(c) {
				return errors.New("Invalid op component: undefined")
			}
		} else if c == nil {
			// JS: typeof null === 'object' -> object branch.
			return errors.New("Invalid op component: undefined")
		} else {
			// strings, booleans, etc.
			return errors.New("Op components must be objects or numbers")
		}
		last = c
	}
	return nil
}

func componentValid(c any) bool {
	if s, ok := iField(c); ok && s != "" {
		return true
	}
	if s, ok := dField(c); ok && s != "" {
		return true
	}
	return false
}

// --- _makeAppend -----------------------------------------------------------

// makeAppend ports the vendored _makeAppend(op) factory. The returned
// closure mutates the backing slice (Go: *[]any).
func makeAppend(op *[]any) func(any) {
	return func(component any) {
		if n, ok := isSkip(component); ok && n == 0 {
			return
		}
		if s, ok := iField(component); ok && s == "" {
			return
		}
		if s, ok := dField(component); ok && s == "" {
			return
		}
		if len(*op) == 0 {
			*op = append(*op, component)
			return
		}
		lastIdx := len(*op) - 1
		last := (*op)[lastIdx]
		ln, lIsNum := isSkip(last)
		if n, cIsNum := isSkip(component); cIsNum && lIsNum {
			(*op)[lastIdx] = ln + n
			return
		}
		if ci, okI := iField(component); okI {
			if li, okL := iField((*op)[lastIdx]); okL {
				(*op)[lastIdx] = map[string]any{"i": li + ci}
				return
			}
		}
		if cd, okD := dField(component); okD {
			if ld, okL := dField((*op)[lastIdx]); okL {
				(*op)[lastIdx] = map[string]any{"d": ld + cd}
				return
			}
		}
		*op = append(*op, component)
	}
}

// _makeAppend is exported for parity with the vendored module (the vendor
// comment says it is used by a randomOpGenerator that does not exist in
// this service; the oracle table does not exercise it beyond makeAppend).
func _makeAppend(op []any) func(any) { return makeAppend(&op) }

// --- makeTake --------------------------------------------------------------

type taker struct {
	idx    int
	offset int
	op     []any
}

func makeTake(op []any) *taker {
	return &taker{op: op, idx: 0, offset: 0}
}

func (t *taker) Peek() any {
	if t.idx >= len(t.op) {
		return nil
	}
	return t.op[t.idx]
}

// take ports the vendored take(n, indivisibleField). n < 0 marks "take the
// next component" (JS: n == null). indivisableField is "" (none), "i" or "d".
func (t *taker) take(n int, indivisableField string) any {
	if t.idx >= len(t.op) {
		return nil
	}
	c := t.op[t.idx]
	if n0, ok := isSkip(c); ok {
		if n < 0 || n0-t.offset <= n {
			c = n0 - t.offset
			t.idx++
			t.offset = 0
			return c
		}
		t.offset += n
		return n
	}
	is, iok := iField(c)
	ds, dok := dField(c)
	field := "d"
	if iok && is != "" {
		field = "i"
	}
	val := is
	if dok && field == "d" {
		val = ds
	}
	if n < 0 || len(val)-t.offset <= n || field == indivisableField {
		val = val[t.offset:]
		t.idx++
		t.offset = 0
	} else {
		val = val[t.offset : t.offset+n]
		t.offset += n
	}
	return map[string]any{field: val}
}

// --- apply -----------------------------------------------------------------

// Create returns the empty snapshot (JS: "").
func Create() string { return "" }

// CheckOpInput wraps []any in the vendored checkOp error contract.
func CheckOpInput(op []any) error { return CheckOp(op) }

// Apply applies the op to the snapshot string and returns the new string.
func Apply(snapshot any, op []any) (string, error) {
	str, ok := snapshot.(string)
	if !ok {
		return "", errors.New("Snapshot should be a string")
	}
	if err := CheckOp(op); err != nil {
		return "", err
	}
	var newDoc []string
	rem := str
	for _, component := range op {
		if n, ok := isSkip(component); ok {
			if n > len(rem) {
				return "", errors.New("The op is too long for this document")
			}
			newDoc = append(newDoc, rem[:n])
			rem = rem[n:]
		} else if s, ok := iField(component); ok {
			newDoc = append(newDoc, s)
		} else {
			d, _ := dField(component)
			if d != rem[:len(d)] {
				return "", errors.New("The deleted text '" + d + "' doesn't match the next characters in the document '" + rem[:len(d)] + "'")
			}
			rem = rem[len(d):]
		}
	}
	if rem != "" {
		return "", errors.New("The applied op doesn't traverse the entire document")
	}
	out := ""
	for _, s := range newDoc {
		out += s
	}
	return out, nil
}

// --- normalize -------------------------------------------------------------

// Normalize concatenates adjacent same-kind components and drops empty
// ones. NOTE: the vendored normalize does NOT run checkOp (the vendored
// line was deleted in the decaffeinate conversion).
func Normalize(op []any) []any {
	newOp := []any{}
	append := makeAppend(&newOp)
	for _, component := range op {
		append(component)
	}
	return newOp
}

// --- transform -------------------------------------------------------------

var (
	errTraversesMore = errors.New("The op traverses more elements than the document has")
	errDeleteNoMatch = errors.New("The deleted text doesn't match the inserted text")
)

// Transform transforms op by otherOp (side left|right). Neither operand is
// mutated (the vendored takes are fresh).
func Transform(op []any, otherOp []any, side string) ([]any, error) {
	if side != "left" && side != "right" {
		return nil, errors.New("side (" + side + " must be 'left' or 'right'")
	}
	if err := CheckOp(op); err != nil {
		return nil, err
	}
	if err := CheckOp(otherOp); err != nil {
		return nil, err
	}
	newOp := []any{}
	append := makeAppend(&newOp)
	t := makeTake(op)

	for _, component := range otherOp {
		if n0, ok := isSkip(component); ok {
			length := n0
			for length > 0 {
				chunk := t.take(length, "i")
				if chunk == nil {
					return nil, errTraversesMore
				}
				append(chunk)
				if !hasI(chunk) {
					length -= componentLength(chunk)
				}
			}
		} else if s, okI := iField(component); okI {
			_ = s
			if side == "left" {
				if oi, ok := iField(t.Peek()); ok && oi != "" {
					append(t.take(-1, ""))
				}
			}
			append(len(s))
		} else {
			d, _ := dField(component)
			length := len(d)
			for length > 0 {
				chunk := t.take(length, "i")
				if chunk == nil {
					return nil, errTraversesMore
				}
				if n, ok := isSkip(chunk); ok {
					length -= n
				} else if s, ok := iField(chunk); ok {
					append(chunk)
					_ = s
				} else if ds, ok := dField(chunk); ok {
					length -= len(ds)
				}
			}
		}
	}

	for component := t.take(-1, ""); component != nil; component = t.take(-1, "") {
		if !hasI(component) {
			return nil, errors.New("Remaining fragments in the op: undefined")
		}
		append(component)
	}
	return newOp, nil
}

func hasI(c any) bool {
	s, ok := iField(c)
	return ok && s != ""
}

func hasD(c any) bool {
	s, ok := dField(c)
	return ok && s != ""
}

// --- compose ---------------------------------------------------------------

// Compose composes op1 then op2 into a single op. Strict vendored
// semantics: op2's deletes must be satisfied by op1's skips (converted to
// deletes) or inserts (cancelling, strings must match); op1 tails must be
// deletes only.
func Compose(op1 []any, op2 []any) ([]any, error) {
	if err := CheckOp(op1); err != nil {
		return nil, err
	}
	if err := CheckOp(op2); err != nil {
		return nil, err
	}
	result := []any{}
	append := makeAppend(&result)
	t := makeTake(op1)

	for _, component := range op2 {
		if n0, ok := isSkip(component); ok {
			length := n0
			for length > 0 {
				chunk := t.take(length, "d")
				if chunk == nil {
					return nil, errTraversesMore
				}
				append(chunk)
				if !hasD(chunk) {
					length -= componentLength(chunk)
				}
			}
		} else if s, okI := iField(component); okI {
			append(map[string]any{"i": s})
		} else {
			d, _ := dField(component)
			offset := 0
			for offset < len(d) {
				chunk := t.take(len(d)-offset, "d")
				if chunk == nil {
					return nil, errTraversesMore
				}
				if n, ok := isSkip(chunk); ok {
					append(map[string]any{"d": d[offset : offset+n]})
					offset += n
				} else if s, ok := iField(chunk); ok {
					if d[offset:offset+len(s)] != s {
						return nil, errDeleteNoMatch
					}
					offset += len(s)
					// the ops cancel each other out
				} else {
					append(chunk)
					// offset does NOT advance; the vendored loop then
					// re-takes and, once op1 is exhausted, errors.
				}
			}
		}
	}

	for component := t.take(-1, ""); component != nil; component = t.take(-1, "") {
		if !hasD(component) {
			return nil, errors.New("Trailing stuff in op1 undefined")
		}
		append(component)
	}
	return result, nil
}

// --- invert ----------------------------------------------------------------

func invertComponent(c any) any {
	if n, ok := isSkip(c); ok {
		return n
	}
	if s, ok := iField(c); ok {
		return map[string]any{"d": s}
	}
	if s, ok := dField(c); ok {
		return map[string]any{"i": s}
	}
	return c
}

// Invert inverts an op (i <-> d, skips unchanged), merging adjacent.
func Invert(op []any) []any {
	result := []any{}
	append := makeAppend(&result)
	for _, component := range op {
		append(invertComponent(component))
	}
	return result
}

// --- registry adapter ------------------------------------------------------

// TPCompType adapts the vendored text-composable type to the sharejstypes
// registry (the model consumes types through that interface only).
type TPCompType struct{}

func (TPCompType) Name() string { return Type }

func (TPCompType) Create() any { return Create() }

func (TPCompType) Apply(snapshot any, op any) (any, error) {
	opList, ok := op.([]any)
	if !ok {
		return nil, errors.New("text-composable: op must be []any")
	}
	return Apply(snapshot, opList)
}

func (TPCompType) Transform(op any, otherOp any, side string) (any, error) {
	a, ok1 := op.([]any)
	b, ok2 := otherOp.([]any)
	if !ok1 || !ok2 {
		return nil, errors.New("text-composable: ops must be []any")
	}
	return Transform(a, b, side)
}
