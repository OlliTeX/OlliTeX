package otc

import "encoding/json"

// EditOperation is the Node `EditOperation` abstract (edit_operation.js):
// a single edit that can be applied to file data / a length, inverted, and
// composed with other edits. Concretes: the text wrapper (Phase A
// *TextOperation), AddCommentOperation, DeleteCommentOperation,
// SetCommentStateOperation, and the no-op.
//
// Node methods that `throw` return an `error` here (Phase A convention for the
// OT error family); invariant violations are returned as typed errors rather
// than panicking (see HANDOFF §Decisions).
type EditOperation interface {
	ToJSON() map[string]any
	Apply(file *StringFileData) error
	ApplyToLength(length int) (int, error)
	Invert(previous *StringFileData) (EditOperation, error)
	CanBeComposedWith(other EditOperation) bool
	CanBeComposedWithForUndo(other EditOperation) bool
	Compose(other EditOperation) (EditOperation, error)
}

// mustJSON renders an EditOperation's toJSON as a compact JSON string (for
// error messages that interpolate the op, e.g. InvalidConversionError).
func mustJSON(op EditOperation) string {
	b, err := json.Marshal(op.ToJSON())
	if err != nil {
		return "{}"
	}
	return string(b)
}

// opTypeName maps an EditOperation to its Node class name, used to build the
// "Trying to compose X with Y" messages the oracles pin.
func opTypeName(op EditOperation) string {
	switch op.(type) {
	case *textEdit:
		return "TextOperation"
	case *AddCommentOp:
		return "AddCommentOperation"
	case *DeleteCommentOp:
		return "DeleteCommentOperation"
	case *SetCommentStateOp:
		return "SetCommentStateOperation"
	case *EditNoOp:
		return "NoOperation"
	default:
		return "EditOperation"
	}
}

// asTextOp unwraps the text-backed EditOperation to its Phase A *TextOperation.
func asTextOp(op EditOperation) (*TextOperation, bool) {
	te, ok := op.(*textEdit)
	if !ok {
		return nil, false
	}
	return te.op, true
}
