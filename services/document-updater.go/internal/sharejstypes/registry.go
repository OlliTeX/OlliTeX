// Package sharejstypes is a Go port of the vendored ShareJS type registry
// (app/js/sharejs/types/index.js) and the standalone vendored types:
// `simple`, `count`, `text` (wrapper over the oracle-verified sharejstext
// port), `json`, `text-tp2`, and `text-composable`.
//
// Node types/index.js registers a flat map name -> type object. Go: Register
// / Lookup over the same names. Each registered type satisfies the Type
// interface below; type-specific snapshot/op concrete values are defined in
// each type's own file. The model package consumes types only through this
// interface.
package sharejstypes

import (
	sjtext "document-updater/internal/sharejstext"
	"sync"
)

// Type is the Go analogue of a vendored ShareJS "type" object
// (name/create/apply/transform). Bootstrap types (text, json) also expose
// Compose/Normalize helpers on their concrete files, but the model only
// needs this face.
type Type interface {
	Name() string
	Create() any
	Apply(snapshot any, op any) (any, error)
	Transform(op1 any, op2 any, side string) (any, error)
}

var (
	mu       sync.RWMutex
	registry = map[string]Type{
		"simple": Simple{},
		"count":  Count{},
		"text":   textType{},
		// json, text-tp2, text-composable: registered further down in their
		// own files (chunks 2-4).
	}
)

// Register makes a type available by name (mirrors types/index.js register).
func Register(name string, t Type) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = t
}

// Lookup returns the registered type for name, mirroring `types[data.type]`
// (nil when the type is missing; the model treats a nil type as a "Type not
// found" error).
func Lookup(name string) Type {
	mu.RLock()
	defer mu.RUnlock()
	return registry[name]
}

// textType adapts the oracle-verified sharejstext package (vendored
// types/text.js + types/helpers.js) to the Type interface. Snapshot = string,
// op = []sharejstext.Component.
type textType struct{}

func (textType) Name() string { return sjtext.Type }

func (textType) Create() any { return sjtext.Create() }

func (textType) Apply(snapshot any, op any) (any, error) {
	s, ok := snapshot.(string)
	if !ok {
		return nil, InvalidSnapshot("text snapshot must be a string")
	}
	o, ok := op.([]sjtext.Component)
	if !ok {
		return nil, InvalidOp("text op must be []sharejstext.Component")
	}
	return sjtext.Apply(s, o)
}

func (textType) Transform(op1 any, op2 any, side string) (any, error) {
	o1, ok := op1.([]sjtext.Component)
	if !ok {
		return nil, InvalidOp("text op1 must be []sharejstext.Component")
	}
	o2, ok := op2.([]sjtext.Component)
	if !ok {
		return nil, InvalidOp("text op2 must be []sharejstext.Component")
	}
	return sjtext.Transform(o1, o2, side)
}
