package otc

import (
	"fmt"
)

// --- text wrapper ----------------------------------------------------------

// textEdit adapts Phase A's *TextOperation to the EditOperation interface
// (Node: TextOperation extends EditOperation).
type textEdit struct{ op *TextOperation }

// NewTextEdit wraps a Phase A TextOperation as an EditOperation.
func NewTextEdit(op *TextOperation) EditOperation { return &textEdit{op: op} }

func (t *textEdit) ToJSON() map[string]any { return t.op.ToJSON() }
func (t *textEdit) Apply(file *StringFileData) error {
	if file.Comments == nil {
		file.Comments = NewCommentList(nil)
	}
	return t.op.Apply(file)
}

func (t *textEdit) ApplyToLength(length int) (int, error) { return t.op.ApplyToLength(length) }
func (t *textEdit) Invert(prev *StringFileData) (EditOperation, error) {
	return &textEdit{op: t.op.Invert(prev)}, nil
}

func (t *textEdit) CanBeComposedWith(other EditOperation) bool {
	o, ok := asTextOp(other)
	if !ok {
		return false
	}
	return t.op.CanBeComposedWith(o)
}

func (t *textEdit) CanBeComposedWithForUndo(other EditOperation) bool {
	o, ok := asTextOp(other)
	if !ok {
		return false
	}
	return t.op.CanBeComposedWithForUndo(o)
}

func (t *textEdit) Compose(other EditOperation) (EditOperation, error) {
	o, ok := asTextOp(other)
	if !ok {
		return nil, gop("Cannot compose TextOperation with " + opTypeName(other))
	}
	res, err := t.op.Compose(o)
	if err != nil {
		return nil, err
	}
	return &textEdit{op: res}, nil
}

// --- AddCommentOperation ---------------------------------------------------

// AddCommentOp mirrors AddCommentOperation.
type AddCommentOp struct {
	CommentID string
	Ranges    []Range
	Resolved  bool
}

// NewAddCommentOp builds one; Node throws on any empty range.
func NewAddCommentOp(commentID string, ranges []Range, resolved bool) (*AddCommentOp, error) {
	for _, r := range ranges {
		if r.IsEmpty() {
			return nil, gop("AddCommentOperation can't be built with empty ranges")
		}
	}
	return &AddCommentOp{CommentID: commentID, Ranges: ranges, Resolved: resolved}, nil
}

func (o *AddCommentOp) ToJSON() map[string]any {
	raw := map[string]any{"commentId": o.CommentID, "ranges": rangesToRaw(o.Ranges)}
	if o.Resolved {
		raw["resolved"] = true
	}
	return raw
}

func (o *AddCommentOp) Apply(file *StringFileData) error {
	if file.Comments == nil {
		file.Comments = NewCommentList(nil)
	}
	file.Comments.Add(NewComment(o.CommentID, o.Ranges, o.Resolved))
	return nil
}

func (o *AddCommentOp) ApplyToLength(n int) (int, error) { return n, nil }

func (o *AddCommentOp) Invert(prev *StringFileData) (EditOperation, error) {
	c := prev.Comments.GetComment(o.CommentID)
	if c == nil {
		return &DeleteCommentOp{CommentID: o.CommentID}, nil
	}
	return NewAddCommentOp(c.ID, c.Ranges, c.Resolved)
}

func (o *AddCommentOp) CanBeComposedWith(other EditOperation) bool {
	switch x := other.(type) {
	case *AddCommentOp:
		return o.CommentID == x.CommentID
	case *DeleteCommentOp:
		return o.CommentID == x.CommentID
	case *SetCommentStateOp:
		return o.CommentID == x.CommentID
	}
	return false
}

func (o *AddCommentOp) CanBeComposedWithForUndo(EditOperation) bool { return false }

func (o *AddCommentOp) Compose(other EditOperation) (EditOperation, error) {
	switch x := other.(type) {
	case *DeleteCommentOp:
		if x.CommentID == o.CommentID {
			return x, nil
		}
	case *AddCommentOp:
		if x.CommentID == o.CommentID {
			return x, nil
		}
	case *SetCommentStateOp:
		if x.CommentID == o.CommentID {
			return NewAddCommentOp(o.CommentID, o.Ranges, x.Resolved)
		}
	}
	return nil, gop(fmt.Sprintf("Trying to compose AddCommentOperation with %s.", opTypeName(other)))
}

// --- DeleteCommentOperation ------------------------------------------------

// DeleteCommentOp mirrors DeleteCommentOperation.
type DeleteCommentOp struct{ CommentID string }

func (o *DeleteCommentOp) ToJSON() map[string]any {
	return map[string]any{"deleteComment": o.CommentID}
}

func (o *DeleteCommentOp) Apply(file *StringFileData) error {
	if file.Comments != nil {
		file.Comments.Delete(o.CommentID)
	}
	return nil
}

func (o *DeleteCommentOp) ApplyToLength(n int) (int, error) { return n, nil }

func (o *DeleteCommentOp) Invert(prev *StringFileData) (EditOperation, error) {
	c := prev.Comments.GetComment(o.CommentID)
	if c == nil {
		return &EditNoOp{}, nil
	}
	return NewAddCommentOp(c.ID, c.Ranges, c.Resolved)
}

func (o *DeleteCommentOp) CanBeComposedWith(EditOperation) bool { return false }
func (o *DeleteCommentOp) CanBeComposedWithForUndo(EditOperation) bool {
	return false
}
func (o *DeleteCommentOp) Compose(EditOperation) (EditOperation, error) {
	return nil, gop("Abstract method not implemented")
}

// --- SetCommentStateOperation ----------------------------------------------

// SetCommentStateOp mirrors SetCommentStateOperation.
type SetCommentStateOp struct {
	CommentID string
	Resolved  bool
}

func (o *SetCommentStateOp) ToJSON() map[string]any {
	return map[string]any{"resolved": o.Resolved, "commentId": o.CommentID}
}

func (o *SetCommentStateOp) Apply(file *StringFileData) error {
	if file.Comments == nil {
		return nil
	}
	c := file.Comments.GetComment(o.CommentID)
	if c == nil {
		return nil
	}
	file.Comments.Add(NewComment(c.ID, c.Ranges, o.Resolved))
	return nil
}

func (o *SetCommentStateOp) ApplyToLength(n int) (int, error) { return n, nil }

func (o *SetCommentStateOp) Invert(prev *StringFileData) (EditOperation, error) {
	c := prev.Comments.GetComment(o.CommentID)
	if c == nil {
		return &EditNoOp{}, nil
	}
	return &SetCommentStateOp{CommentID: o.CommentID, Resolved: c.Resolved}, nil
}

func (o *SetCommentStateOp) CanBeComposedWith(other EditOperation) bool {
	switch x := other.(type) {
	case *SetCommentStateOp:
		return o.CommentID == x.CommentID
	case *DeleteCommentOp:
		return o.CommentID == x.CommentID
	}
	return false
}

func (o *SetCommentStateOp) CanBeComposedWithForUndo(EditOperation) bool {
	return false
}

func (o *SetCommentStateOp) Compose(other EditOperation) (EditOperation, error) {
	switch x := other.(type) {
	case *SetCommentStateOp:
		if x.CommentID == o.CommentID {
			return x, nil
		}
	case *DeleteCommentOp:
		if x.CommentID == o.CommentID {
			return x, nil
		}
	}
	return nil, gop(fmt.Sprintf("Trying to compose SetCommentStateOperation with %s.", opTypeName(other)))
}

// --- EditNoOperation -------------------------------------------------------

// EditNoOp mirrors EditNoOperation.
type EditNoOp struct{}

func (n *EditNoOp) ToJSON() map[string]any      { return map[string]any{"noOp": true} }
func (n *EditNoOp) Apply(*StringFileData) error { return nil }
func (n *EditNoOp) ApplyToLength(length int) (int, error) {
	return length, nil
}
func (n *EditNoOp) Invert(*StringFileData) (EditOperation, error) {
	return nil, gop("Abstract method not implemented")
}
func (n *EditNoOp) CanBeComposedWith(EditOperation) bool { return false }
func (n *EditNoOp) CanBeComposedWithForUndo(EditOperation) bool {
	return false
}
func (n *EditNoOp) Compose(EditOperation) (EditOperation, error) {
	return nil, gop("Abstract method not implemented")
}

// rangesToRaw serialises []Range to the raw form (Node: ranges.map(toRaw)).
func rangesToRaw(ranges []Range) []map[string]any {
	out := make([]map[string]any, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, map[string]any{"pos": r.Pos, "length": r.Length})
	}
	return out
}
