// Package historyot ports the vendor overleaf-editor-core operation layer
// (lib/operation) to the extent the project-history service needs:
//
//   - the EditOperation base surface (lib/operation/edit_operation.js)
//   - TextOperation (reuses internal/opmodel, adapted to EditOp)
//   - comment edit operations (Add/Delete/SetCommentState/EditNo)
//   - EditOperationBuilder (lib/operation/edit_operation_builder.js)
//
// File-level operations (Add/Move/EditFileOperation, Change, Origin) land in
// later B items alongside their consumers. All lengths are UTF-16 code
// units (vendor str.length); opmodel.Units mirrors that.
package historyot

import (
	"ollitex/go/services/project-history/internal/opmodel"
)

const baseLengthMessage = "The base length of the second operation has to be the target length of the first operation"

// EditOp — vendor abstract class `EditOperation`
// (lib/operation/edit_operation.js), the interface over its abstract
// surface. All lengths are UTF-16 code units.
type EditOp interface {
	// Apply — vendor `apply(fileData)`: mutate the file in place.
	Apply(file *opmodel.StringFileData) error
	// ApplyToLength — vendor `applyToLength(length)`.
	ApplyToLength(length int) (int, error)
	// Invert — vendor `invert(previousState)`: the undo operation relative
	// to the state before this op was applied.
	Invert(previousState *opmodel.StringFileData) EditOp
	// CanBeComposedWith — vendor `canBeComposedWith(other)`.
	CanBeComposedWith(other EditOp) bool
	// CanBeComposedWithForUndo — vendor `canBeComposedWithForUndo(other)`.
	CanBeComposedWithForUndo(other EditOp) bool
	// Compose — vendor `compose(other)`: merge two consecutive operations
	// so that apply(apply(S, A), B) == apply(S, compose(A, B)). The
	// opmodel error replaces vendor's throw.
	Compose(other EditOp) (EditOp, error)
	// ToRaw — vendor `toJSON()` (the raw edit-op wire shape).
	ToRaw() map[string]any
	// IsNoOp — vendor `isNoOp()`.
	IsNoOp() bool
	// IsText — Go replacement for `x instanceof TextOperation` on paths
	// where the vendor dispatches on the concrete type.
	IsText() bool
	// ClassName — Go replacement for `other?.constructor?.name`, used in
	// vendor's compose error strings.
	ClassName() string
}

// TextOpAdapter adapts *opmodel.TextOperation to the historyot EditOp
// surface. Vendor code holds the raw TextOperation and dispatches via
// `instanceof`; the adapter is the Go replacement for the port.
type TextOpAdapter struct {
	t *opmodel.TextOperation
}

// NewTextOpAdapter wraps an opmodel.TextOperation as an EditOp.
func NewTextOpAdapter(t *opmodel.TextOperation) EditOp {
	return &TextOpAdapter{t: t}
}

func (a *TextOpAdapter) IsNoOp() bool      { return a.t.IsNoop() }
func (a *TextOpAdapter) IsText() bool      { return true }
func (a *TextOpAdapter) ClassName() string { return "TextOperation" }
func (a *TextOpAdapter) ToRaw() map[string]any {
	return a.t.ToRaw()
}
func (a *TextOpAdapter) Apply(file *opmodel.StringFileData) error {
	return a.t.Apply(file)
}
func (a *TextOpAdapter) ApplyToLength(length int) (int, error) {
	return a.t.ApplyToLength(length)
}

// Compose — vendor `TextOperation.compose`: throws when `other` is not a
// TextOperation (vendor sees `other.baseLength === undefined` and throws the
// base-length error) or when the lengths do not chain.
func (a *TextOpAdapter) Compose(other EditOp) (EditOp, error) {
	otherAdapter, ok := other.(*TextOpAdapter)
	if !ok {
		return nil, composeBaseLengthError(other)
	}
	composed, err := a.t.Compose(otherAdapter.t)
	if err != nil {
		return nil, err
	}
	composedT, ok := composed.(*opmodel.TextOperation)
	if !ok {
		return nil, opmodel.NewUnprocessableError(baseLengthMessage, nil)
	}
	return &TextOpAdapter{t: composedT}, nil
}

func (a *TextOpAdapter) CanBeComposedWith(other EditOp) bool {
	if !other.IsText() {
		return false
	}
	otherAdapter, ok := other.(*TextOpAdapter)
	if !ok {
		return false
	}
	return a.t.CanBeComposedWith(otherAdapter.t)
}

func (a *TextOpAdapter) CanBeComposedWithForUndo(other EditOp) bool {
	if !other.IsText() {
		return false
	}
	otherAdapter, ok := other.(*TextOpAdapter)
	if !ok {
		return false
	}
	return a.t.CanBeComposedWithForUndo(otherAdapter.t)
}

func (a *TextOpAdapter) Invert(previousState *opmodel.StringFileData) EditOp {
	// Vendor TextOperation.invert always returns a new TextOperation.
	inverted := a.t.Invert(previousState)
	inner, ok := inverted.(*opmodel.TextOperation)
	if !ok {
		panic("historyot: TextOperation.Invert returned non-TextOperation")
	}
	return &TextOpAdapter{t: inner}
}

// composeBaseLengthError — vendor message for composing a TextOperation with
// a non-TextOperation (other.baseLength undefined ≠ this.targetLength).
func composeBaseLengthError(other EditOp) *opmodel.UnprocessableError {
	return opmodel.NewUnprocessableError(baseLengthMessage, nil)
}
