package otc

import "fmt"

// TransformEditOps mirrors EditOperationTransformer.transform: turns two edit
// operations against each other, returning [a', b'].
//
// The "not implemented" message pins Node's U+FFAE separator (\uFFAE).
func TransformEditOps(a, b EditOperation) (EditOperation, EditOperation, error) {
	if isNoOp(a) || isNoOp(b) {
		return a, b, nil
	}
	for _, c := range editTransformCases {
		if c.matchA(a) && c.matchB(b) {
			return c.fn(a, b)
		}
		if c.matchB(a) && c.matchA(b) {
			x, y, err := c.fn(b, a)
			if err != nil {
				return nil, nil, err
			}
			return y, x, nil
		}
	}
	return nil, nil, gop(fmt.Sprintf(
		"Transform not implemented for %s\uFFAE%s", opTypeName(a), opTypeName(b)))
}

func isNoOp(e EditOperation) bool { _, ok := e.(*EditNoOp); return ok }
func isText(e EditOperation) bool { _, ok := asTextOp(e); return ok }
func isAdd(e EditOperation) bool  { _, ok := e.(*AddCommentOp); return ok }
func isDelete(e EditOperation) bool {
	_, ok := e.(*DeleteCommentOp)
	return ok
}
func isSet(e EditOperation) bool {
	_, ok := e.(*SetCommentStateOp)
	return ok
}

type editTransformCase struct {
	matchA func(EditOperation) bool
	matchB func(EditOperation) bool
	fn     func(a, b EditOperation) (EditOperation, EditOperation, error)
}

var editTransformCases = []editTransformCase{
	{isText, isText, editTransformTextText},
	{isText, isDelete, editTransformNoConflict},
	{isText, isSet, editTransformNoConflict},
	{isText, isAdd, editTransformTextAdd},
	{isAdd, isAdd, func(a, b EditOperation) (EditOperation, EditOperation, error) {
		aA, bA := a.(*AddCommentOp), b.(*AddCommentOp)
		if aA.CommentID == bA.CommentID {
			return &EditNoOp{}, b, nil
		}
		return a, b, nil
	}},
	{isAdd, isDelete, func(a, b EditOperation) (EditOperation, EditOperation, error) {
		aA, bD := a.(*AddCommentOp), b.(*DeleteCommentOp)
		if aA.CommentID == bD.CommentID { // delete wins
			return &EditNoOp{}, b, nil
		}
		return a, b, nil
	}},
	{isAdd, isSet, func(a, b EditOperation) (EditOperation, EditOperation, error) {
		aA, bS := a.(*AddCommentOp), b.(*SetCommentStateOp)
		if aA.CommentID == bS.CommentID {
			na, err := NewAddCommentOp(aA.CommentID, aA.Ranges, bS.Resolved)
			if err != nil {
				return nil, nil, err
			}
			return na, b, nil
		}
		return a, b, nil
	}},
	{isDelete, isDelete, func(a, b EditOperation) (EditOperation, EditOperation, error) {
		aD, bD := a.(*DeleteCommentOp), b.(*DeleteCommentOp)
		if aD.CommentID == bD.CommentID {
			return &EditNoOp{}, &EditNoOp{}, nil
		}
		return a, b, nil
	}},
	{isDelete, isSet, func(a, b EditOperation) (EditOperation, EditOperation, error) {
		aD, bS := a.(*DeleteCommentOp), b.(*SetCommentStateOp)
		if aD.CommentID == bS.CommentID { // delete wins
			return a, &EditNoOp{}, nil
		}
		return a, b, nil
	}},
	{isSet, isSet, func(a, b EditOperation) (EditOperation, EditOperation, error) {
		aS, bS := a.(*SetCommentStateOp), b.(*SetCommentStateOp)
		if aS.CommentID != bS.CommentID {
			return a, b, nil
		}
		if aS.Resolved == bS.Resolved {
			return &EditNoOp{}, &EditNoOp{}, nil
		}
		shouldResolve := aS.Resolved && bS.Resolved
		if aS.Resolved == shouldResolve {
			return a, &EditNoOp{}, nil
		}
		return &EditNoOp{}, b, nil
	}},
}

func editTransformNoConflict(a, b EditOperation) (EditOperation, EditOperation, error) {
	return a, b, nil
}

func editTransformTextText(a, b EditOperation) (EditOperation, EditOperation, error) {
	aText, _ := asTextOp(a)
	bText, _ := asTextOp(b)
	ap, bp, err := Transform(aText, bText)
	if err != nil {
		return nil, nil, err
	}
	return &textEdit{op: ap}, &textEdit{op: bp}, nil
}

// editTransformTextAdd applies the text op to the comment being added, moving
// its ranges (Node: Comment.applyTextOperation + new AddCommentOperation).
func editTransformTextAdd(a, b EditOperation) (EditOperation, EditOperation, error) {
	aText, ok := asTextOp(a)
	if !ok {
		return nil, nil, gop("Transform not implemented for text+AddComment")
	}
	addB := b.(*AddCommentOp)
	original := NewComment(addB.CommentID, addB.Ranges, addB.Resolved)
	moved := original.ApplyTextOperation(aText, addB.CommentID)
	na, err := NewAddCommentOp(moved.ID, moved.Ranges, moved.Resolved)
	if err != nil {
		return nil, nil, err
	}
	return a, na, nil
}

// TransformEditOpsMultiple mirrors EditOperationTransformer.transformMultiple.
func TransformEditOpsMultiple(as, bs []EditOperation) {
	for i := range as {
		for j := range bs {
			ap, bp, err := TransformEditOps(as[i], bs[j])
			if err != nil {
				continue // Node throws; the multi-path is used where it holds
			}
			as[i] = ap
			bs[j] = bp
		}
	}
}
