package otc

// EditOperationBuilder mirrors EditOperationBuilder (fromJSON / isValid),
// dispatching a raw edit op to its concrete type by key presence (Node order).

// FromJSONEditOperation mirrors EditOperationBuilder.fromJSON.
func FromJSONEditOperation(raw map[string]any) (EditOperation, error) {
	if isTextOperationRaw(raw) {
		op, err := FromJSONTextOperation(raw)
		if err != nil {
			return nil, err
		}
		return NewTextEdit(op), nil
	}
	if isRawAddComment(raw) {
		id, _ := raw["commentId"].(string)
		ranges := asRawRanges(raw["ranges"])
		resolved, _ := raw["resolved"].(bool)
		return NewAddCommentOp(id, ranges, resolved)
	}
	if isRawDeleteComment(raw) {
		id, _ := raw["deleteComment"].(string)
		return &DeleteCommentOp{CommentID: id}, nil
	}
	if isRawSetCommentState(raw) {
		id, _ := raw["commentId"].(string)
		resolved, _ := raw["resolved"].(bool)
		return &SetCommentStateOp{CommentID: id, Resolved: resolved}, nil
	}
	if isRawEditNoOp(raw) {
		return &EditNoOp{}, nil
	}
	return nil, gop("Unsupported operation in EditOperationBuilder.fromJSON")
}

// IsValidEditOperationRaw mirrors EditOperationBuilder.isValid.
func IsValidEditOperationRaw(raw map[string]any) bool {
	return isTextOperationRaw(raw) ||
		isRawAddComment(raw) ||
		isRawDeleteComment(raw) ||
		isRawSetCommentState(raw) ||
		isRawEditNoOp(raw)
}

func isTextOperationRaw(raw map[string]any) bool {
	_, ok := raw["textOperation"]
	return ok
}

func isRawAddComment(raw map[string]any) bool {
	if _, ok := raw["commentId"]; !ok {
		return false
	}
	r, ok := raw["ranges"]
	if !ok {
		return false
	}
	_, ok = r.([]any)
	return ok
}

func isRawDeleteComment(raw map[string]any) bool {
	_, ok := raw["deleteComment"]
	return ok
}

func isRawSetCommentState(raw map[string]any) bool {
	if _, ok := raw["commentId"]; !ok {
		return false
	}
	r, ok := raw["resolved"]
	if !ok {
		return false
	}
	_, ok = r.(bool)
	return ok
}

func isRawEditNoOp(raw map[string]any) bool {
	_, ok := raw["noOp"]
	return ok
}
