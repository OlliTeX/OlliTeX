package otc

import (
	"time"

	oerror "ollitex/go/libraries/oerror"
)

// ChangeRequest mirrors lib/change_request.js: a list of Operations that the
// server can apply as a Change. When `untransformable` is set, the server will
// not transform it if out of date (e.g. it must set metadata on exactly one
// file, which is unsafe to transform against concurrent metadata edits).
type ChangeRequest struct {
	Authors         []any
	BaseVersion     int
	Operations      []Operation
	Untransformable bool
}

// NewChangeRequest mirrors `new ChangeRequest(baseVersion, operations,
// untransformable, authors)`:
//
//	Node:
//	  assert.integer(baseVersion, 'bad baseVersion')
//	  assert.array.of.object(operations, 'bad operations')
//	  assert.maybe.boolean(untransformable, 'ChangeRequest: bad untransformable')
//	  authors = authors || []
//	  AuthorList.assertV1(authors, 'bad authors')
//
// `authors` defaults to an empty list; AssertAuthorsV1 enforces the
// "all same type" invariant (Node: AuthorList.assertV1).
func NewChangeRequest(baseVersion int, operations []Operation, untransformable bool, authors []any) *ChangeRequest {
	if authors == nil {
		authors = []any{}
	}
	AssertAuthorsV1(authors, "bad authors")
	if operations == nil {
		operations = []Operation{}
	}
	return &ChangeRequest{
		Authors:         authors,
		BaseVersion:     baseVersion,
		Operations:      operations,
		Untransformable: untransformable,
	}
}

// ToRaw (Node: toRaw).
func (r *ChangeRequest) ToRaw() map[string]any {
	ops := make([]any, 0, len(r.Operations))
	for _, op := range r.Operations {
		ops = append(ops, op.ToRaw())
	}
	return map[string]any{
		"baseVersion":     r.BaseVersion,
		"operations":      ops,
		"untransformable": r.Untransformable,
		"authors":         r.Authors,
	}
}

// ChangeRequestFromRaw mirrors `ChangeRequest.fromRaw`.
func ChangeRequestFromRaw(raw map[string]any) (*ChangeRequest, error) {
	opsRaw, ok := raw["operations"].([]any)
	if !ok {
		return nil, oerror.New("bad raw.operations", nil)
	}
	ops := make([]Operation, 0, len(opsRaw))
	for _, ro := range opsRaw {
		rm, ok := ro.(map[string]any)
		if !ok {
			return nil, oerror.New("bad raw.operations", nil)
		}
		op, err := OperationFromRaw(rm)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	bv, ok := raw["baseVersion"].(int)
	if !ok {
		return nil, oerror.New("bad baseVersion", nil)
	}
	authors, _ := raw["authors"].([]any)
	untransformable, _ := raw["untransformable"].(bool)
	return NewChangeRequest(bv, ops, untransformable, authors), nil
}

// GetBaseVersion (Node: getBaseVersion).
func (r *ChangeRequest) GetBaseVersion() int { return r.BaseVersion }

// IsUntransformable (Node: isUntransformable).
func (r *ChangeRequest) IsUntransformable() bool { return r.Untransformable }

// MakeChange (Node: makeChange) — `new Change(operations, timestamp, authors)`.
func (r *ChangeRequest) MakeChange(timestamp time.Time) *Change {
	return NewChange(r.Operations, timestamp, r.Authors, nil, nil, nil, nil)
}
