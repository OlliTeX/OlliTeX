package core

// Ports the Node oracle lib/operation/edit_operation_transformer.js.

// NewEditNoOp (Node EditNoOperation).
func NewEditNoOp() *EditOp { return &EditOp{kind: "noOp"} }

// NewAddCommentEditOp (Node AddCommentOperation: commentId, ranges, resolved).
func NewAddCommentEditOp(id string, ranges []Range, resolved bool) *EditOp {
	return &EditOp{kind: "addComment", commentID: id, ranges: ranges, resolved: resolved}
}

// NewDeleteCommentEditOp (Node DeleteCommentOperation).
func NewDeleteCommentEditOp(id string) *EditOp {
	return &EditOp{kind: "deleteComment", commentID: id}
}

// NewSetCommentStateEditOp (Node SetCommentStateOperation).
func NewSetCommentStateEditOp(id string, resolved bool) *EditOp {
	return &EditOp{kind: "setCommentState", commentID: id, resolved: resolved}
}

// editOpKindName mirrors Node's constructor.name for the unreachable error
// message (only reached in the "not implemented" case, kept for parity).
func editOpKindName(e *EditOp) string {
	switch e.kind {
	case "text":
		return "TextOperation"
	case "addComment":
		return "AddCommentOperation"
	case "deleteComment":
		return "DeleteCommentOperation"
	case "setCommentState":
		return "SetCommentStateOperation"
	default:
		return "EditNoOperation"
	}
}

// EditOpTransform ports EditOperationTransformer.transform: transforms two
// edit operations against each other.
//
// The ordered transformer list below matches Node's `transformers` array
// (first match wins). Symmetric pairs (where the Node transformer takes an
// ordered (a,b) but is called via createTransformer on either argument order)
// are handled by trying (a,b) and, failing that, (b,a) reversed — the same
// transpose the oracle uses via createTransformer.
func EditOpTransform(a, b *EditOp) (*EditOp, *EditOp, error) {
	if a.kind == "noOp" || b.kind == "noOp" {
		return a, b, nil
	}

	// The ordered list of transformers, indexed by ordered kind pair.
	// Each entry transforms (a,b) in that exact argument order.
	transformers := map[string]func(a, b *EditOp) (*EditOp, *EditOp, error){
		"text_text": func(a, b *EditOp) (*EditOp, *EditOp, error) {
			ta, tb := a.textOp, b.textOp
			if ta == nil || tb == nil {
				return nil, nil, &BadRawError{Msg: "text edit op missing text op"}
			}
			taPrime, tbPrime, err := TextOpTransform(ta, tb)
			if err != nil {
				return nil, nil, err
			}
			return NewTextEditOp(taPrime), NewTextEditOp(tbPrime), nil
		},
		"text_deleteComment": func(a, b *EditOp) (*EditOp, *EditOp, error) {
			return a, b, nil // noConflict
		},
		"text_setCommentState": func(a, b *EditOp) (*EditOp, *EditOp, error) {
			return a, b, nil // noConflict
		},
		"text_addComment": func(a, b *EditOp) (*EditOp, *EditOp, error) {
			ta := a.textOp
			if ta == nil {
				return nil, nil, &BadRawError{Msg: "text edit op missing text op"}
			}
			// apply the text operation to the comment
			originalComment := &Comment{ID: b.commentID, Ranges: b.ranges, Resolved: b.resolved}
			movedComment := originalComment.ApplyTextOperation(ta, b.commentID)
			return a, NewAddCommentEditOp(movedComment.ID, movedComment.Ranges, movedComment.Resolved), nil
		},
		"addComment_addComment": func(a, b *EditOp) (*EditOp, *EditOp, error) {
			if a.commentID == b.commentID {
				return NewEditNoOp(), b, nil
			}
			return a, b, nil
		},
		"addComment_deleteComment": func(a, b *EditOp) (*EditOp, *EditOp, error) {
			if a.commentID == b.commentID {
				// delete wins
				return NewEditNoOp(), b, nil
			}
			return a, b, nil
		},
		"addComment_setCommentState": func(a, b *EditOp) (*EditOp, *EditOp, error) {
			if a.commentID == b.commentID {
				return NewAddCommentEditOp(a.commentID, a.ranges, b.resolved), b, nil
			}
			return a, b, nil
		},
		"deleteComment_deleteComment": func(a, b *EditOp) (*EditOp, *EditOp, error) {
			if a.commentID == b.commentID {
				// if both operations delete the same comment, we can ignore both
				return NewEditNoOp(), NewEditNoOp(), nil
			}
			return a, b, nil
		},
		"deleteComment_setCommentState": func(a, b *EditOp) (*EditOp, *EditOp, error) {
			if a.commentID == b.commentID {
				// delete wins
				return a, NewEditNoOp(), nil
			}
			return a, b, nil
		},
		"setCommentState_setCommentState": func(a, b *EditOp) (*EditOp, *EditOp, error) {
			if a.commentID != b.commentID {
				return a, b, nil
			}
			if a.resolved == b.resolved {
				return NewEditNoOp(), NewEditNoOp(), nil
			}
			shouldResolve := a.resolved && b.resolved
			if a.resolved == shouldResolve {
				return a, NewEditNoOp(), nil
			}
			return NewEditNoOp(), b, nil
		},
	}

	// Ordered kind-pair match list — the transformer list order from Node
	// matters: earlier transformers win over later ones (e.g. noOp check is
	// already handled above; text_deleteComment precedes others, etc.).
	pairOrder := []string{
		"text_text",
		"text_deleteComment",
		"text_setCommentState",
		"text_addComment",
		"addComment_addComment",
		"addComment_deleteComment",
		"addComment_setCommentState",
		"deleteComment_deleteComment",
		"deleteComment_setCommentState",
		"setCommentState_setCommentState",
	}
	for _, pair := range pairOrder {
		keyA := a.kind + "_" + b.kind
		keyB := b.kind + "_" + a.kind
		switch {
		case keyA == pair:
			if f, ok := transformers[pair]; ok {
				return f(a, b)
			}
		case keyB == pair:
			if f, ok := transformers[pair]; ok {
				bPrime, aPrime, err := f(b, a)
				if err != nil {
					return nil, nil, err
				}
				return aPrime, bPrime, nil
			}
		}
	}

	return nil, nil, &BadRawError{
		Msg: "Transform not implemented for " + editOpKindName(a) + "\uffe6" + editOpKindName(b),
	}
}
