package historyot

import (
	"ollitex/go/services/project-history/internal/opmodel"
)

// EditOperationTransformer — vendor `EditOperationTransformer`
// (lib/operation/edit_operation_transformer.js).
//
// Transform takes two operations A and B that happened concurrently and
// produces A' and B' such that
// apply(apply(S, A), B') == apply(apply(S, B), A').
type EditOperationTransformer struct{}

// Transform — vendor `EditOperationTransformer.transform(a, b)`.
//
// The vendor table (in order, each symmetric in both orientations):
//
//	EditNo*                    → [a, b]
//	(Text, Text)               → TextOperation.transform
//	(Text, DeleteComment)      → [a, b] (no conflict)
//	(Text, SetCommentState)    → [a, b] (no conflict)
//	(Text, AddComment)         → [a, moved(b)]
//	(Add, Add)                 → same id: [noOp, b]; else [a, b]
//	(Add, Delete)              → same id: [noOp, b] (delete wins); else [a, b]
//	(Add, Set)                 → same id: [add(resolved=b), b]; else [a, b]
//	(Delete, Delete)           → same id: [noOp, noOp] (both ignored); else [a, b]
//	(Delete, Set)              → same id: [a, noOp] (delete wins); else [a, b]
//	(Set, Set)                 → same id + same resolved: [noOp, noOp]
//	                             same id + flip: earlier resolves ⇒ [a, noOp]
//	                             (later wins the resolved state)
func (EditOperationTransformer) Transform(a, b EditOp) (EditOp, EditOp, error) {
	if a.IsNoOp() || b.IsNoOp() {
		return a, b, nil
	}

	if ta, ok := a.(*TextOpAdapter); ok {
		if tb, ok2 := b.(*TextOpAdapter); ok2 {
			aPrime, bPrime, err := opmodel.Transform(ta.t, tb.t)
			if err != nil {
				return nil, nil, err
			}
			return &TextOpAdapter{t: aPrime}, &TextOpAdapter{t: bPrime}, nil
		}
		if add, ok2 := b.(*AddCommentOperation); ok2 {
			moved, err := add.MovedByText(ta.t)
			if err != nil {
				return nil, nil, err
			}
			return a, moved, nil
		}
		// (Text, DeleteComment) / (Text, SetCommentState): no conflict.
		return a, b, nil
	}

	if tb, ok := b.(*TextOpAdapter); ok {
		if add, ok2 := a.(*AddCommentOperation); ok2 {
			moved, err := add.MovedByText(tb.t)
			if err != nil {
				return nil, nil, err
			}
			return moved, b, nil
		}
		// (DeleteComment, Text) / (SetCommentState, Text): no conflict.
		return a, b, nil
	}

	switch aA := a.(type) {
	case *AddCommentOperation:
		switch bB := b.(type) {
		case *AddCommentOperation:
			if aA.commentID == bB.commentID {
				return NewEditNoOperation(), b, nil
			}
			return a, b, nil
		case *DeleteCommentOperation:
			if aA.commentID == bB.commentID {
				return NewEditNoOperation(), b, nil
			}
			return a, b, nil
		case *SetCommentStateOperation:
			if aA.commentID == bB.commentID {
				return &AddCommentOperation{
					commentID: aA.commentID,
					ranges:    aA.ranges,
					resolved:  bB.Resolved,
				}, b, nil
			}
			return a, b, nil
		}
	}
	switch aA := a.(type) {
	case *DeleteCommentOperation:
		switch bB := b.(type) {
		case *DeleteCommentOperation:
			if aA.commentID == bB.commentID {
				return NewEditNoOperation(), NewEditNoOperation(), nil
			}
			return a, b, nil
		case *SetCommentStateOperation:
			if aA.commentID == bB.commentID {
				return a, NewEditNoOperation(), nil
			}
			return a, b, nil
		}
	}
	if sA, ok := a.(*SetCommentStateOperation); ok {
		if sB, ok2 := b.(*SetCommentStateOperation); ok2 {
			if sA.commentID != sB.commentID {
				return a, b, nil
			}
			if sA.Resolved == sB.Resolved {
				return NewEditNoOperation(), NewEditNoOperation(), nil
			}
			shouldResolve := sA.Resolved && sB.Resolved
			if sA.Resolved == shouldResolve {
				return a, NewEditNoOperation(), nil
			}
			return NewEditNoOperation(), b, nil
		}
	}

	return nil, nil, opmodel.NewUnprocessableError(
		"Transform not implemented for "+a.ClassName()+"\u25ae"+b.ClassName(),
		nil,
	)
}

// AddCommentOperation.MovedByText — vendor (TextOperation, AddCommentOperation)
// case: build the original Comment, apply the text op to it, return the add
// op for the moved ranges.
func (o *AddCommentOperation) MovedByText(t *opmodel.TextOperation) (*AddCommentOperation, error) {
	original, err := opmodel.NewComment(o.commentID, o.ranges, o.resolved)
	if err != nil {
		return nil, err
	}
	moved := original.ApplyTextOperation(t, o.commentID)
	return &AddCommentOperation{
		commentID: moved.ID,
		ranges:    moved.Ranges,
		resolved:  moved.Resolved,
	}, nil
}

// TransformMany — vendor `transformMultiple(as, bs)`: prime each a against
// each b, in place.
func (EditOperationTransformer) TransformMany(as []EditOp, bs []EditOp) error {
	for i := 0; i < len(as); i++ {
		for j := 0; j < len(bs); j++ {
			aPrime, bPrime, err := (EditOperationTransformer{}).Transform(as[i], bs[j])
			if err != nil {
				return err
			}
			as[i] = aPrime
			bs[j] = bPrime
		}
	}
	return nil
}
