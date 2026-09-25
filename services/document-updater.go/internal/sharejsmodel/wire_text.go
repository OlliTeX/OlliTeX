package sharejsmodel

import (
	"errors"

	sjtext "document-updater/internal/sharejstext"
)

// ErrNilOp is the oracle-pinned error for a nil/undefined wire op reaching
// the type (V8: "undefined is not iterable (cannot read property
// Symbol(Symbol.iterator))", thrown by the vendored checkValidOp over an
// undefined op).
var ErrNilOp = errors.New("undefined is not iterable (cannot read property Symbol(Symbol.iterator))")

// TextWireType is the wire-native "text" type face consumed by the model
// (wire snapshots = string, wire ops = []any component maps). Its wire<->
// typed conversion delegates op-level behavior to the oracle-verified
// sharejstext port.
type TextWireType struct{}

func (TextWireType) Name() string { return sjtext.Type }

func (TextWireType) Create() any { return "" }

// componentFromWire converts a wire component map {p, i|d|c, t?} to a typed
// Component. A non-map wire value yields a zero component (the vendored
// Array.from(undefined) throw is modelled upstream in the adapter, not here).
func componentFromWire(c any) sjtext.Component {
	var comp sjtext.Component
	m, _ := c.(map[string]any)
	if m == nil {
		return comp
	}
	if p, ok := m["p"].(int); ok {
		comp.P = p
	}
	if i, ok := m["i"].(string); ok {
		comp.I = &i
	}
	if d, ok := m["d"].(string); ok {
		comp.D = &d
	}
	if c2, ok := m["c"].(string); ok {
		comp.C = &c2
	}
	if t, ok := m["t"].(string); ok {
		comp.T = &t
	}
	return comp
}

func componentsFromWire(op []any) []sjtext.Component {
	out := make([]sjtext.Component, 0, len(op))
	for _, c := range op {
		out = append(out, componentFromWire(c))
	}
	return out
}

func componentToWire(c sjtext.Component) any {
	out := map[string]any{"p": c.P}
	if c.I != nil {
		out["i"] = *c.I
	}
	if c.D != nil {
		out["d"] = *c.D
	}
	if c.C != nil {
		out["c"] = *c.C
	}
	if c.T != nil {
		out["t"] = *c.T
	}
	return out
}

func (TextWireType) Apply(snapshot any, op any) (any, error) {
	s, ok := snapshot.(string)
	if !ok {
		return nil, errors.New("Snapshot should be a string")
	}
	if op == nil {
		return nil, ErrNilOp
	}
	wire, ok := op.([]any)
	if !ok {
		return nil, ErrNilOp
	}
	return sjtext.Apply(s, componentsFromWire(wire))
}

func (TextWireType) Transform(op1 any, op2 any, side string) (any, error) {
	if op1 == nil || op2 == nil {
		return nil, ErrNilOp
	}
	o1, ok1 := op1.([]any)
	o2, ok2 := op2.([]any)
	if !ok1 || !ok2 {
		return nil, ErrNilOp
	}
	c1 := componentsFromWire(o1)
	c2 := componentsFromWire(o2)
	nt, err := sjtext.Transform(c1, c2, side)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(nt))
	for _, c := range nt {
		out = append(out, componentToWire(c))
	}
	return out, nil
}
