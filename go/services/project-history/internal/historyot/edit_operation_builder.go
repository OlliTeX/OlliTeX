package historyot

import (
	"ollitex/go/services/project-history/internal/opmodel"
)

// EditOperationBuilder — vendor `EditOperationBuilder`
// (lib/operation/edit_operation_builder.js).
//
// Vendor dispatch:
//
//	isTextOperation:           'textOperation' in raw
//	isRawAddCommentOperation:  'commentId' && Array.isArray(ranges)
//	isRawDeleteCommentOperation: 'deleteComment' in raw
//	isRawSetCommentStateOperation: 'commentId' && typeof resolved === 'boolean'
//	isRawEditNoOperation:      'noOp' in raw
//
// Order matters (vendor checks in this sequence); the Go port keeps it.
type EditOperationBuilder struct{}

// Builder — package-level instance, mirroring vendor's single
// `EditOperationBuilder` export.
var Builder = EditOperationBuilder{}

// BuildFromRaw — vendor `EditOperationBuilder.fromJSON(raw)`.
func (EditOperationBuilder) BuildFromRaw(raw map[string]any) (EditOp, error) {
	if isRawTextOperation(raw) {
		t, err := opmodel.TextOperationFromJSON(raw)
		if err != nil {
			return nil, err
		}
		return &TextOpAdapter{t: t}, nil
	}
	if isRawAddCommentOperation(raw) {
		return AddCommentOperationFromRaw(raw)
	}
	if isRawDeleteCommentOperation(raw) {
		return DeleteCommentOperationFromRaw(raw)
	}
	if isRawSetCommentStateOperation(raw) {
		return SetCommentStateOperationFromRaw(raw)
	}
	if isRawEditNoOperation(raw) {
		return NewEditNoOperation(), nil
	}
	return nil, opmodel.NewUnprocessableError("Unsupported operation in EditOperationBuilder.fromJSON", nil)
}

// IsValid — vendor `EditOperationBuilder.isValid(raw)`: the discriminator
// UpdateCompressor uses to decide whether an update op is a historyOT edit.
func (EditOperationBuilder) IsValid(raw any) bool {
	m, ok := raw.(map[string]any)
	if !ok || m == nil {
		return false
	}
	return isRawTextOperation(m) ||
		isRawAddCommentOperation(m) ||
		isRawDeleteCommentOperation(m) ||
		isRawSetCommentStateOperation(m) ||
		isRawEditNoOperation(m)
}

func isRawTextOperation(raw map[string]any) bool {
	_, has := raw["textOperation"]
	return has
}

// isRawAddCommentOperation — vendor: 'commentId' && Array.isArray(ranges).
// Array.isArray(raw.ranges): the key must be present with a non-nil slice.
func isRawAddCommentOperation(raw map[string]any) bool {
	_, hasID := raw["commentId"]
	r, hasRanges := raw["ranges"]
	if !hasID || !hasRanges || r == nil {
		return false
	}
	switch r.(type) {
	case []any:
		return true
	default:
		return false
	}
}

func isRawDeleteCommentOperation(raw map[string]any) bool {
	_, has := raw["deleteComment"]
	return has
}

// isRawSetCommentStateOperation — vendor: 'commentId' && typeof resolved ===
// 'boolean' (strict).
func isRawSetCommentStateOperation(raw map[string]any) bool {
	_, hasID := raw["commentId"]
	r, has := raw["resolved"]
	if !hasID || !has {
		return false
	}
	_, ok := r.(bool)
	return ok
}

func isRawEditNoOperation(raw map[string]any) bool {
	_, has := raw["noOp"]
	return has
}
