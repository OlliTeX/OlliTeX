// Package sharejstext — 1:1 Go port of ShareJS text OT type
// (`app/js/sharejs/types/text.js` + its `helpers.js` bootstrap).
//
// Operations are lists of components. Each component inserts, deletes or
// comments text at a position:
//
//	{i: 'str', p: 100}      insert
//	{d: 'str', p: 100}      delete
//	{c: 'str', p: 100, t:'thread'}  comment
//
// Components in an op execute sequentially, so a component's p assumes the
// previous components already applied.
//
// Ported: name, create, apply, _append, compose, compress, normalize,
// transformPosition/transformCursor, _tc (transformComponent), invert,
// transform + transformX (bootstrap). Node's `throw new Error(msg)` points
// are surfaced as Go errors.
//
// Node String.slice clamps on out-of-range ends; Go would panic. Every
// indexed slice on an op payload is therefore routed through jsSliceNS /
// jsSliceFrom (the clamping subset) so the mismatch errors apply() relies
// on and the delete-overlap transforms stay faithful.
package sharejstext

import (
	"fmt"
	"strings"
)

// Component is one op component. Exactly one of I/D/C is set. T is present
// for comments (the thread id); Node carries `t` only on comment components.
// P is always present.
type Component struct {
	I *string
	D *string
	C *string
	P int
	T *string
}

type Op []Component

// Insert / Delete / Comment constructors (I/D/C exclusive).
func Insert(p int, s string) Component { return Component{I: &s, P: p} }
func Delete(p int, s string) Component { return Component{D: &s, P: p} }
func Comment(p int, s, t string) Component {
	c, th := s, t
	return Component{C: &c, P: p, T: &th}
}

// kind returns "i"/"d"/"c" (or "").
func (c Component) kind() string {
	switch {
	case c.I != nil:
		return "i"
	case c.D != nil:
		return "d"
	case c.C != nil:
		return "c"
	}
	return ""
}

// strInject mirrors Node `strInject` (s1.slice(0,pos) + s2 + s1.slice(pos)).
// JS slice clamps: pos beyond len yields whole-front + s2 + ""; a negative
// pos (which checkValidComponent disallows but _append can produce) clamps to
// 0 for the first half as slice(0,pos) would.
func strInject(s1 string, pos int, s2 string) string {
	n := len(s1)
	front := pos
	if front < 0 {
		front = 0
	}
	if front > n {
		front = n
	}
	tail := pos
	if tail < 0 {
		tail = 0
	}
	if tail > n {
		tail = n
	}
	return s1[:front] + s2 + s1[tail:]
}

// jsSlice mirrors Node String.prototype.slice — the clamping subset the text
// type uses. Node: s.slice(start, end) clamps a negative start to 0, an
// out-of-range end to s.length, and any start >= len returns "".
func jsSlice(s string, start, end int) string {
	n := len(s)
	if start < 0 {
		start = 0
	}
	if start > n {
		start = n
	}
	if end > n {
		end = n
	}
	if start > end {
		return ""
	}
	return s[start:end]
}

// jsTail mirrors Node s.slice(start) (open end).
func jsTail(s string, start int) string { return jsSlice(s, start, len(s)) }

func checkValidComponent(c Component) error {
	if c.kind() == "" {
		return fmt.Errorf("component needs an i, d or c field")
	}
	if c.P < 0 {
		return fmt.Errorf("position cannot be negative")
	}
	return nil
}

func checkValidOp(op []Component) error {
	for _, c := range op {
		if err := checkValidComponent(c); err != nil {
			return err
		}
	}
	return nil
}

// Type mirrors the Node type metadata used by the model registry.
const Type = "text"

// Create mirrors `text.create()` — the empty snapshot.
func Create() string { return "" }

// Apply mirrors `text.apply(snapshot, op)`. Comments are checked but do not
// change the snapshot; a delete/comment whose text does not match the
// snapshot yields an error.
func Apply(snapshot string, op []Component) (string, error) {
	if err := checkValidOp(op); err != nil {
		return "", err
	}
	for _, component := range op {
		if component.I != nil {
			snapshot = strInject(snapshot, component.P, *component.I)
		} else if component.D != nil {
			deleted := jsSlice(snapshot, component.P, component.P+len(*component.D))
			if *component.D != deleted {
				return "", fmt.Errorf(
					"Delete component '%s' does not match deleted text '%s'",
					*component.D, deleted,
				)
			}
			snapshot = jsSlice(snapshot, 0, component.P) + jsTail(snapshot, component.P+len(*component.D))
		} else if component.C != nil {
			comment := jsSlice(snapshot, component.P, component.P+len(*component.C))
			if *component.C != comment {
				return "", fmt.Errorf(
					"Comment component '%s' does not match commented text '%s'",
					*component.C, comment,
				)
			}
		} else {
			return "", fmt.Errorf("Unknown op type")
		}
	}
	return snapshot, nil
}

// Append mirrors `text._append(newOp, c)` — appends c to *newOp, composing
// adjacent insert/insert or delete/delete runs in place. An empty insert or
// delete text is a no-op. Comments always append as their own component.
func Append(newOp *[]Component, c Component) {
	if (c.I != nil && *c.I == "") || (c.D != nil && *c.D == "") {
		return
	}
	if len(*newOp) == 0 {
		*newOp = append(*newOp, c)
		return
	}
	last := (*newOp)[len(*newOp)-1]
	if last.I != nil && c.I != nil && last.P <= c.P && c.P <= last.P+len(*last.I) {
		composed := Component{I: ptr(strInject(*last.I, c.P-last.P, *c.I)), P: last.P}
		(*newOp)[len(*newOp)-1] = composed
		return
	}
	if last.D != nil && c.D != nil && c.P <= last.P && last.P <= c.P+len(*c.D) {
		composed := Component{D: ptr(strInject(*c.D, last.P-c.P, *c.D)), P: c.P}
		(*newOp)[len(*newOp)-1] = composed
		return
	}
	*newOp = append(*newOp, c)
}

func ptr[T any](v T) *T { return &v }

func compose(op1, op2 []Component) []Component {
	newOp := make([]Component, len(op1))
	copy(newOp, op1)
	for _, c := range op2 {
		Append(&newOp, c)
	}
	return newOp
}

// Compose mirrors `text.compose(op1, op2)`.
func Compose(op1, op2 []Component) []Component { return compose(op1, op2) }

// Compress mirrors `text.compress(op)`.
func Compress(op []Component) []Component { return compose(nil, op) }

// Normalize mirrors `text.normalize(op)` — allows a bare single component
// (modelled as a one-element Op here) when p is absent, and composes like
// compress.
func Normalize(op []Component) []Component {
	newOp := []Component{}
	for _, c := range op {
		Append(&newOp, c)
	}
	return newOp
}

// transformPos mirrors the Node `transformPosition(pos, c, insertAfter)`.
func transformPos(pos int, c Component, insertAfter bool) (int, error) {
	if c.I != nil {
		if c.P < pos || (c.P == pos && insertAfter) {
			return pos + len(*c.I), nil
		}
		return pos, nil
	}
	if c.D != nil {
		if pos <= c.P {
			return pos, nil
		} else if pos <= c.P+len(*c.D) {
			return c.P, nil
		}
		return pos - len(*c.D), nil
	}
	if c.C != nil {
		return pos, nil
	}
	return 0, fmt.Errorf("unknown op type")
}

// TransformCursor mirrors `text.transformCursor(position, op, side)`.
func TransformCursor(position int, op []Component, side string) (int, error) {
	insertAfter := side == "right"
	for _, c := range op {
		p, err := transformPos(position, c, insertAfter)
		if err != nil {
			return 0, err
		}
		position = p
	}
	return position, nil
}

// TransformComponent mirrors `text._tc(dest, c, otherC, side)` — appends the
// transform of component c by otherC (on the given side) to *dest. The
// result may add 0, 1 or 2 components to *dest.
func TransformComponent(dest *[]Component, c, otherC Component, side string) error {
	if err := checkValidOp([]Component{c}); err != nil {
		return err
	}
	if err := checkValidOp([]Component{otherC}); err != nil {
		return err
	}

	if c.I != nil {
		p, err := transformPos(c.P, otherC, side == "right")
		if err != nil {
			return err
		}
		Append(dest, Component{I: ptr(*c.I), P: p})
		return nil
	}

	if c.D != nil {
		if otherC.I != nil {
			// delete vs insert
			s := *c.D
			if c.P < otherC.P {
				Append(dest, Component{D: ptr(jsSlice(s, 0, otherC.P-c.P)), P: c.P})
				s = jsTail(s, otherC.P-c.P)
			}
			if s != "" {
				Append(dest, Component{D: ptr(s), P: c.P + len(*otherC.I)})
			}
			return nil
		}
		if otherC.D != nil {
			// delete vs delete
			if c.P >= otherC.P+len(*otherC.D) {
				Append(dest, Component{D: ptr(*c.D), P: c.P - len(*otherC.D)})
				return nil
			}
			if c.P+len(*c.D) <= otherC.P {
				Append(dest, c)
				return nil
			}
			// They overlap somewhere.
			newC := Component{D: ptr(""), P: c.P}
			if c.P < otherC.P {
				newC.D = ptr(jsSlice(*c.D, 0, otherC.P-c.P))
			}
			if c.P+len(*c.D) > otherC.P+len(*otherC.D) {
				newC.D = ptr(jsSlice(*newC.D, 0, len(*newC.D)) +
					jsSlice(*c.D, otherC.P+len(*otherC.D)-c.P, c.P+len(*c.D)-c.P))
			}
			intersectStart := max(c.P, otherC.P)
			intersectEnd := min(c.P+len(*c.D), otherC.P+len(*otherC.D))
			cIntersect := jsSlice(*c.D, intersectStart-c.P, intersectEnd-c.P)
			otherIntersect := jsSlice(*otherC.D, intersectStart-otherC.P, intersectEnd-otherC.P)
			if cIntersect != otherIntersect {
				return fmt.Errorf(
					"Delete ops delete different text in the same region of the document",
				)
			}
			// `d: ''` is a no-op — Append skips it (empty d). Node guards
			// with `if (newC.d !== '')`.
			if *newC.D != "" {
				newP, err := transformPos(newC.P, otherC, false)
				if err != nil {
					return err
				}
				newC.P = newP
				Append(dest, newC)
			}
			return nil
		}
		if otherC.C != nil {
			Append(dest, c)
			return nil
		}
		return fmt.Errorf("unknown op type")
	}

	// c.C != nil: comment
	if otherC.I != nil {
		if c.P < otherC.P && otherC.P < c.P+len(*c.C) {
			offset := otherC.P - c.P
			newC := jsSlice(*c.C, 0, offset) + *otherC.I + jsTail(*c.C, offset)
			Append(dest, Component{C: ptr(newC), P: c.P, T: c.T})
		} else {
			p, err := transformPos(c.P, otherC, true)
			if err != nil {
				return err
			}
			Append(dest, Component{C: ptr(*c.C), P: p, T: c.T})
		}
		return nil
	}
	if otherC.D != nil {
		if c.P >= otherC.P+len(*otherC.D) {
			Append(dest, Component{C: ptr(*c.C), P: c.P - len(*otherC.D), T: c.T})
			return nil
		}
		if c.P+len(*c.C) <= otherC.P {
			Append(dest, c)
			return nil
		}
		// Delete overlaps comment.
		newC := Component{C: ptr(""), P: c.P, T: c.T}
		if c.P < otherC.P {
			newC.C = ptr(jsSlice(*c.C, 0, otherC.P-c.P))
		}
		if c.P+len(*c.C) > otherC.P+len(*otherC.D) {
			newC.C = ptr(jsSlice(*newC.C, 0, len(*newC.C)) +
				jsSlice(*c.C, otherC.P+len(*otherC.D)-c.P, c.P+len(*c.C)-c.P))
		}
		intersectStart := max(c.P, otherC.P)
		intersectEnd := min(c.P+len(*c.C), otherC.P+len(*otherC.D))
		cIntersect := jsSlice(*c.C, intersectStart-c.P, intersectEnd-c.P)
		otherIntersect := jsSlice(*otherC.D, intersectStart-otherC.P, intersectEnd-otherC.P)
		if cIntersect != otherIntersect {
			return fmt.Errorf(
				"Delete ops delete different text in the same region of the document",
			)
		}
		newP, err := transformPos(newC.P, otherC, false)
		if err != nil {
			return err
		}
		newC.P = newP
		Append(dest, newC)
		return nil
	}
	if otherC.C != nil {
		Append(dest, c)
		return nil
	}
	return fmt.Errorf("unknown op type")
}

func invertComponent(c Component) Component {
	if c.I != nil {
		return Component{D: c.I, P: c.P}
	}
	return Component{I: c.D, P: c.P}
}

// Invert mirrors `text.invert(op)`.
func Invert(op []Component) []Component {
	out := make([]Component, 0, len(op))
	for i := len(op) - 1; i >= 0; i-- {
		out = append(out, invertComponent(op[i]))
	}
	return out
}

func transformComponentX(left, right Component, destLeft, destRight *[]Component) error {
	if err := TransformComponent(destLeft, left, right, "left"); err != nil {
		return err
	}
	return TransformComponent(destRight, right, left, "right")
}

// TransformX mirrors the Node `transformX(leftOp, rightOp)` bootstrap —
// transforms rightOp by leftOp and returns [leftOp', rightOp'].
func TransformX(leftOp, rightOp []Component) (leftR, rightR []Component, err error) {
	if err := checkValidOp(leftOp); err != nil {
		return nil, nil, err
	}
	if err := checkValidOp(rightOp); err != nil {
		return nil, nil, err
	}
	newRightOp := []Component{}
	for _, rc := range rightOp {
		rightComponent := rc
		newLeftOp := []Component{}
		consumed := false
		k := 0
		for k < len(leftOp) {
			nextC := []Component{}
			if e := transformComponentX(leftOp[k], rightComponent, &newLeftOp, &nextC); e != nil {
				return nil, nil, e
			}
			k++
			if len(nextC) == 1 {
				rightComponent = nextC[0]
			} else if len(nextC) == 0 {
				newLeftOp = append(newLeftOp, leftOp[k:]...)
				consumed = true
				break
			} else {
				l, r, e := TransformX(leftOp[k:], nextC)
				if e != nil {
					return nil, nil, e
				}
				newLeftOp = append(newLeftOp, l...)
				newRightOp = append(newRightOp, r...)
				consumed = true
				break
			}
		}
		if !consumed {
			newRightOp = append(newRightOp, rightComponent)
		}
		leftOp = newLeftOp
	}
	return leftOp, newRightOp, nil
}

// Transform mirrors `text.transform(op, otherOp, side)`.
func Transform(op, otherOp []Component, side string) ([]Component, error) {
	if side != "left" && side != "right" {
		return nil, fmt.Errorf("type must be 'left' or 'right'")
	}
	if len(otherOp) == 0 {
		return op, nil
	}
	if len(op) == 1 && len(otherOp) == 1 {
		dest := []Component{}
		if err := TransformComponent(&dest, op[0], otherOp[0], side); err != nil {
			return nil, err
		}
		return dest, nil
	}
	if side == "left" {
		left, _, err := TransformX(op, otherOp)
		if err != nil {
			return nil, err
		}
		return left, nil
	}
	_, right, err := TransformX(otherOp, op)
	if err != nil {
		return nil, err
	}
	return right, nil
}

// String is for debug/test diffs.
func (c Component) String() string {
	switch {
	case c.I != nil:
		return fmt.Sprintf("{i:%q p:%d}", *c.I, c.P)
	case c.D != nil:
		return fmt.Sprintf("{d:%q p:%d}", *c.D, c.P)
	case c.C != nil:
		if c.T != nil {
			return fmt.Sprintf("{c:%q p:%d t:%q}", *c.C, c.P, *c.T)
		}
		return fmt.Sprintf("{c:%q p:%d}", *c.C, c.P)
	}
	return fmt.Sprintf("{p:%d}", c.P)
}

func OpString(op []Component) string {
	var b strings.Builder
	for i, c := range op {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(c.String())
	}
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
