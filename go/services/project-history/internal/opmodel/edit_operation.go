package opmodel

// EditOperation ports the vendor abstract class `EditOperation`
// (lib/operation/edit_operation.js), the base class of file-edit
// operations. In vendor it is a JS class with abstract methods
// (apply / applyToLength / invert / canBeComposedWith /
// canBeComposedWithForUndo / compose / toJSON); the Go mirror is the
// interface over that same surface, implemented by *TextOperation.
//
// Lengths are UTF-16 code units (JS str.length).
type EditOperation interface {
	// Apply applies the operation to the file (mutates the file).
	Apply(file *StringFileData) error
	// ApplyToLength returns the length of the string that results from
	// applying the operation to a string of `length` units.
	ApplyToLength(length int) (int, error)
	// Invert computes the inverse (undo) operation relative to the
	// previous state of the file.
	Invert(previousState *StringFileData) EditOperation
	// CanBeComposedWith — can `other` be applied directly after this op?
	CanBeComposedWith(other EditOperation) bool
	// CanBeComposedWithForUndo — vendor canBeComposedWithForUndo:
	// consecutive inserts at the boundary, or deletes at the same
	// position, are grouped for undo.
	CanBeComposedWithForUndo(other EditOperation) bool
	// Compose merges two consecutive operations into one:
	// apply(apply(S, A), B) == apply(S, compose(A, B)).
	Compose(other EditOperation) (EditOperation, error)
	// ToRaw mirrors vendor toJSON: {textOperation, contentHash?}.
	ToRaw() map[string]any
}
