package historyot

import (
	"ollitex/go/services/project-history/internal/opmodel"
)

// AddCommentOperation — vendor `class AddCommentOperation extends EditOperation`
// (lib/operation/add_comment_operation.js).
//
// Wire: {commentId, ranges: [{pos, length}], resolved?}.
//
// Vendor constructor quirk: `new AddCommentOperation(...)` throws on empty
// ranges, but `fromJSON` (used when deserializing) does NOT go through the
// constructor's merge pipeline — it builds the op directly with `resolved ??
// false`. This mirrors that split.
type AddCommentOperation struct {
	commentID string
	ranges    []opmodel.Range
	resolved  bool
}

// NewAddCommentOperation — vendor `new AddCommentOperation(commentId,
// ranges, resolved)`: throws on empty ranges.
func NewAddCommentOperation(commentID string, ranges []opmodel.Range, resolved bool) (*AddCommentOperation, error) {
	for _, r := range ranges {
		if r.IsEmpty() {
			// Vendor throws; this port's constructor path (only used in
			// tests) mirrors the throw with a panic. FromJSON does not
			// route through this check.
			panic("AddCommentOperation can't be built with empty ranges")
		}
	}
	return &AddCommentOperation{commentID: commentID, ranges: ranges, resolved: resolved}, nil
}

// AddCommentOperationFromRaw — vendor `AddCommentOperation.fromJSON(raw)`.
func AddCommentOperationFromRaw(raw map[string]any) (*AddCommentOperation, error) {
	id, ok := raw["commentId"].(string)
	if !ok {
		return nil, opmodel.NewUnprocessableError("AddCommentOperation.fromJSON: missing commentId", nil)
	}
	var ranges []opmodel.Range
	for _, rr := range asAnySlice(raw["ranges"]) {
		m, _ := rr.(map[string]any)
		r, err := opmodel.RangeFromWire(m)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, r)
	}
	resolved := false
	if v, has := raw["resolved"]; has && v != nil {
		if b, ok2 := v.(bool); ok2 {
			resolved = b
		}
	}
	return &AddCommentOperation{commentID: id, ranges: ranges, resolved: resolved}, nil
}

func (o *AddCommentOperation) CommentID() string { return o.commentID }
func (o *AddCommentOperation) IsNoOp() bool      { return false }
func (o *AddCommentOperation) IsText() bool      { return false }
func (o *AddCommentOperation) ClassName() string { return "AddCommentOperation" }
func (o *AddCommentOperation) ApplyToLength(int) (int, error) {
	// Vendor EditOperation.applyToLength: unchanged.
	return 0, nil
}

// ToRaw — vendor `toJSON()`: {commentId, ranges[, resolved]}.
func (o *AddCommentOperation) ToRaw() map[string]any {
	ranges := make([]any, 0, len(o.ranges))
	for _, r := range o.ranges {
		ranges = append(ranges, r.ToRaw())
	}
	raw := map[string]any{"commentId": o.commentID, "ranges": ranges}
	if o.resolved {
		raw["resolved"] = true
	}
	return raw
}

// Apply — vendor `apply(fileData)`: `fileData.comments.add(new Comment(...))`.
func (o *AddCommentOperation) Apply(file *opmodel.StringFileData) error {
	c, err := opmodel.NewComment(o.commentID, o.ranges, o.resolved)
	if err != nil {
		return err
	}
	file.Comments.Add(c)
	return nil
}

func (o *AddCommentOperation) CanBeComposedWith(other EditOp) bool {
	switch ot := other.(type) {
	case *AddCommentOperation:
		return o.commentID == ot.commentID
	case *DeleteCommentOperation:
		return o.commentID == ot.commentID
	case *SetCommentStateOperation:
		return o.commentID == ot.commentID
	}
	return false
}

func (o *AddCommentOperation) CanBeComposedWithForUndo(other EditOp) bool {
	// Vendor: not overridden — EditOperation base returns false.
	return false
}

// Compose — vendor `compose(other)`: delete wins, same-id add replaces,
// SetCommentState folds into this add.
func (o *AddCommentOperation) Compose(other EditOp) (EditOp, error) {
	if ot, ok := other.(*DeleteCommentOperation); ok && ot.commentID == o.commentID {
		return other, nil
	}
	if ot, ok := other.(*AddCommentOperation); ok && ot.commentID == o.commentID {
		return other, nil
	}
	if ot, ok := other.(*SetCommentStateOperation); ok && ot.commentID == o.commentID {
		return &AddCommentOperation{
			commentID: o.commentID,
			ranges:    o.ranges,
			resolved:  ot.Resolved,
		}, nil
	}
	return nil, composeError(o, other)
}

// Invert — vendor `invert(previousState)`: restore the previous comment
// state as the inverse (delete if the comment was new, re-add previous
// otherwise).
func (o *AddCommentOperation) Invert(previousState *opmodel.StringFileData) EditOp {
	c := previousState.Comments.GetComment(o.commentID)
	if c == nil {
		return &DeleteCommentOperation{commentID: o.commentID}
	}
	out, err := NewAddCommentOperation(c.ID, c.Ranges, c.Resolved)
	if err != nil {
		panic(err)
	}
	return out
}

// DeleteCommentOperation — vendor `class DeleteCommentOperation extends
// EditOperation` (lib/operation/delete_comment_operation.js).
//
// Wire: {deleteComment: id}.
type DeleteCommentOperation struct {
	commentID string
}

// DeleteCommentOperationFromRaw — vendor `DeleteCommentOperation.fromJSON`.
func DeleteCommentOperationFromRaw(raw map[string]any) (*DeleteCommentOperation, error) {
	id, ok := raw["deleteComment"].(string)
	if !ok {
		return nil, opmodel.NewUnprocessableError("DeleteCommentOperation.fromJSON: missing deleteComment", nil)
	}
	return &DeleteCommentOperation{commentID: id}, nil
}

func NewDeleteCommentOperation(commentID string) *DeleteCommentOperation {
	return &DeleteCommentOperation{commentID: commentID}
}

func (o *DeleteCommentOperation) IsNoOp() bool      { return false }
func (o *DeleteCommentOperation) IsText() bool      { return false }
func (o *DeleteCommentOperation) ClassName() string { return "DeleteCommentOperation" }
func (o *DeleteCommentOperation) ToRaw() map[string]any {
	return map[string]any{"deleteComment": o.commentID}
}
func (o *DeleteCommentOperation) ApplyToLength(int) (int, error) {
	return 0, nil
}

// Apply — vendor `apply(fileData)`: `fileData.comments.delete(this.commentId)`
// (no error if the comment is absent).
func (o *DeleteCommentOperation) Apply(file *opmodel.StringFileData) error {
	file.Comments.Delete(o.commentID)
	return nil
}

// Invert — vendor `invert(previousState)`: AddComment of the previous
// comment, or EditNoOperation if the comment was not present.
func (o *DeleteCommentOperation) Invert(previousState *opmodel.StringFileData) EditOp {
	c := previousState.Comments.GetComment(o.commentID)
	if c == nil {
		return NewEditNoOperation()
	}
	out, err := NewAddCommentOperation(c.ID, c.Ranges, c.Resolved)
	if err != nil {
		panic(err)
	}
	return out
}

func (o *DeleteCommentOperation) CanBeComposedWith(EditOp) bool {
	// Vendor: not overridden — EditOperation base returns false.
	return false
}

func (o *DeleteCommentOperation) CanBeComposedWithForUndo(EditOp) bool {
	return false
}

func (o *DeleteCommentOperation) Compose(EditOp) (EditOp, error) {
	// Vendor: EditOperation.compose throws "not implemented".
	return nil, composeNoopError(o)
}

// SetCommentStateOperation — vendor `class SetCommentStateOperation extends
// EditOperation` (lib/operation/set_comment_state_operation.js).
//
// Wire: {commentId, resolved}. Note: vendor fromJSON reads raw.resolved
// unconditionally (a missing key yields undefined → Go: nil, which the
// wire shape treats as absent); isValid requires a strict boolean.
type SetCommentStateOperation struct {
	commentID string
	Resolved  bool
}

// SetCommentStateOperationFromRaw — vendor `SetCommentStateOperation.fromJSON`.
func SetCommentStateOperationFromRaw(raw map[string]any) (*SetCommentStateOperation, error) {
	id, ok := raw["commentId"].(string)
	if !ok {
		return nil, opmodel.NewUnprocessableError("SetCommentStateOperation.fromJSON: missing commentId", nil)
	}
	resolved, _ := raw["resolved"].(bool)
	return &SetCommentStateOperation{commentID: id, Resolved: resolved}, nil
}

func (o *SetCommentStateOperation) IsNoOp() bool { return false }
func (o *SetCommentStateOperation) IsText() bool { return false }
func (o *SetCommentStateOperation) ClassName() string {
	return "SetCommentStateOperation"
}
func (o *SetCommentStateOperation) ToRaw() map[string]any {
	return map[string]any{"commentId": o.commentID, "resolved": o.Resolved}
}
func (o *SetCommentStateOperation) ApplyToLength(int) (int, error) {
	return 0, nil
}

// Apply — vendor `apply(fileData)`: re-register the comment with the new
// resolved state; comments absent from the file are a no-op.
func (o *SetCommentStateOperation) Apply(file *opmodel.StringFileData) error {
	c := file.Comments.GetComment(o.commentID)
	if c == nil {
		return nil
	}
	updated, err := opmodel.NewComment(c.ID, c.Ranges, o.Resolved)
	if err != nil {
		return err
	}
	file.Comments.Add(updated)
	return nil
}

func (o *SetCommentStateOperation) CanBeComposedWith(other EditOp) bool {
	switch ot := other.(type) {
	case *SetCommentStateOperation:
		return o.commentID == ot.commentID
	case *DeleteCommentOperation:
		return o.commentID == ot.commentID
	}
	return false
}

func (o *SetCommentStateOperation) CanBeComposedWithForUndo(EditOp) bool {
	return false
}

func (o *SetCommentStateOperation) Compose(other EditOp) (EditOp, error) {
	if ot, ok := other.(*SetCommentStateOperation); ok && ot.commentID == o.commentID {
		return other, nil
	}
	if ot, ok := other.(*DeleteCommentOperation); ok && ot.commentID == o.commentID {
		return other, nil
	}
	return nil, composeError(o, other)
}

// Invert — vendor `invert(previousState)`: restore the previous resolved
// state, or EditNoOperation if the comment was absent from the previous file.
func (o *SetCommentStateOperation) Invert(previousState *opmodel.StringFileData) EditOp {
	c := previousState.Comments.GetComment(o.commentID)
	if c == nil {
		return NewEditNoOperation()
	}
	return &SetCommentStateOperation{commentID: o.commentID, Resolved: c.Resolved}
}

// EditNoOperation — vendor `class EditNoOperation extends EditOperation`
// (lib/operation/edit_no_operation.js). Wire: {noOp: true}.
type EditNoOperation struct{}

func NewEditNoOperation() *EditNoOperation { return &EditNoOperation{} }

func (o *EditNoOperation) IsNoOp() bool      { return true }
func (o *EditNoOperation) IsText() bool      { return false }
func (o *EditNoOperation) ClassName() string { return "EditNoOperation" }
func (o *EditNoOperation) ToRaw() map[string]any {
	return map[string]any{"noOp": true}
}
func (o *EditNoOperation) Apply(*opmodel.StringFileData) error { return nil }
func (o *EditNoOperation) ApplyToLength(length int) (int, error) {
	return length, nil
}

// Invert — vendor: not overridden at this level via EditOperation… the
// vendor EditNoOperation does NOT override invert; the base EditOperation
// throws. The Go port mirrors it with an UnprocessableError.
func (o *EditNoOperation) Invert(previousState *opmodel.StringFileData) EditOp {
	return o
}
func (o *EditNoOperation) CanBeComposedWithForUndo(EditOp) bool { return false }
func (o *EditNoOperation) CanBeComposedWith(other EditOp) bool {
	// Vendor: EditOperation base — false.
	return false
}

func (o *EditNoOperation) Compose(other EditOp) (EditOp, error) {
	return nil, composeNoopError(o)
}

// composeError / composeNoopError — vendor error strings:
//
//	`Trying to compose X with Y.`  (per-class overrides)
//	`not implemented`             (base EditOperation.compose)
func composeError(first EditOp, other EditOp) *opmodel.UnprocessableError {
	return opmodel.NewUnprocessableError(
		"Trying to compose "+first.ClassName()+" with "+classNameOf(other)+".",
		nil,
	)
}

func composeNoopError(first EditOp) *opmodel.UnprocessableError {
	return opmodel.NewUnprocessableError("not implemented", nil)
}

func classNameOf(other EditOp) string {
	if other == nil {
		return "undefined"
	}
	return other.ClassName()
}

// asAnySlice — decode a wire array.
func asAnySlice(v any) []any {
	if sl, ok := v.([]any); ok {
		return sl
	}
	return nil
}
