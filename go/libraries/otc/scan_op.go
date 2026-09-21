package otc

import (
	"fmt"
)

// applyCtx is the `LengthApplyContext` (Node scan_op): {length, inputCursor,
// inputLength(readonly)}.
type applyCtx struct {
	length      int
	inputCursor int
	inputLength int
}

// ScanOp is one primitive of a TextOperation (Node: ScanOp and subclasses).
type ScanOp interface {
	isScanOp()
	ApplyToLength(ctx *applyCtx) error
	ToJSON() any
	Equals(other ScanOp) bool
	CanMergeWith(other ScanOp) bool
	MergeWith(other ScanOp) (ScanOp, error)
	String() string
}

// === RetainOp (Node: RetainOp) ==============================================

// RetainOp advances the cursor (Node: RetainOp; length + optional tracking).
type RetainOp struct {
	Length   int
	Tracking TrackingDirective
}

func (RetainOp) isScanOp() {}

// NewRetainOp builds a RetainOp (Node: `new RetainOp(length, tracking)`).
func NewRetainOp(length int, tracking TrackingDirective) (RetainOp, error) {
	if length < 0 {
		return RetainOp{}, fmt.Errorf("length must be non-negative")
	}
	return RetainOp{Length: length, Tracking: tracking}, nil
}

// RetainOpFromJSON builds a RetainOp from a raw op (Node: `RetainOp.fromJSON`).
func RetainOpFromJSON(op any) (RetainOp, error) {
	if n, ok := op.(int); ok {
		return NewRetainOp(n, nil)
	}
	if o, ok := op.(map[string]any); ok {
		r, okR := o["r"].(int)
		if !okR {
			return RetainOp{}, fmt.Errorf("retain operation must have a number property")
		}
		if raw, present := o["tracking"]; present {
			tm, okTM := raw.(map[string]any)
			if !okTM {
				return RetainOp{}, fmt.Errorf("retain operation must have a number property")
			}
			if t, _ := tm["type"].(string); t == "none" {
				return NewRetainOp(r, ClearTrackingProps{})
			}
			tp, err := FromRawTrackingProps(tm)
			if err != nil {
				return RetainOp{}, err
			}
			return NewRetainOp(r, tp)
		}
		return NewRetainOp(r, nil)
	}
	return RetainOp{}, fmt.Errorf("retain operation must have a number property")
}

// ApplyToLength advances the cursor (Node: `applyToLength`).
func (r RetainOp) ApplyToLength(ctx *applyCtx) error {
	if ctx.inputCursor+r.Length > ctx.inputLength {
		return NewApplyError("Operation can't retain more chars than are left in the string.", r.ToJSON(), ctx.inputLength)
	}
	ctx.length += r.Length
	ctx.inputCursor += r.Length
	return nil
}

// ToJSON returns the number form or {r,tracking} (Node: `toJSON`).
func (r RetainOp) ToJSON() any {
	if r.Tracking == nil {
		return r.Length
	}
	return map[string]any{"r": r.Length, "tracking": r.Tracking.ToRaw()}
}

// Equals compares length + tracking (Node: `equals`).
func (r RetainOp) Equals(other ScanOp) bool {
	o, ok := other.(RetainOp)
	if !ok {
		return false
	}
	if r.Length != o.Length {
		return false
	}
	if r.Tracking != nil {
		return r.Tracking.Equals(o.Tracking)
	}
	return o.Tracking == nil
}

// CanMergeWith reports mergeability (Node: `canMergeWith`).
func (r RetainOp) CanMergeWith(other ScanOp) bool {
	o, ok := other.(RetainOp)
	if !ok {
		return false
	}
	if r.Tracking != nil {
		if o.Tracking == nil || !r.Tracking.CanMergeWith(o.Tracking) {
			return false
		}
	} else if o.Tracking != nil {
		return false
	}
	return true
}

// MergeWith returns the merged RetainOp (Node: `mergeWith`).
func (r RetainOp) MergeWith(other ScanOp) (ScanOp, error) {
	if !r.CanMergeWith(other) {
		return nil, fmt.Errorf("Cannot merge with incompatible operation")
	}
	o := other.(RetainOp)
	out := r
	out.Length += o.Length
	if r.Tracking != nil && o.Tracking != nil {
		merged, err := r.Tracking.MergeWith(o.Tracking)
		if err != nil {
			return nil, err
		}
		out.Tracking = merged
	}
	return out, nil
}

// String renders "retain N" (Node: `toString`).
func (r RetainOp) String() string { return fmt.Sprintf("retain %d", r.Length) }

// === InsertOp (Node: InsertOp) ==============================================

// InsertOp inserts a string (Node: InsertOp; insertion + tracking + commentIds).
type InsertOp struct {
	Insertion  string
	Tracking   TrackingDirective
	CommentIds []string
}

func (InsertOp) isScanOp() {}

// NewInsertOp builds an InsertOp (Node: `new InsertOp(insertion, tracking, commentIds)`).
// Returns InvalidInsertionError for non-BMP characters.
func NewInsertOp(insertion string, tracking TrackingDirective, commentIds []string) (InsertOp, error) {
	if ContainsNonBmpChars(insertion) {
		return InsertOp{}, NewInvalidInsertionError(insertion, nil)
	}
	return InsertOp{Insertion: insertion, Tracking: tracking, CommentIds: commentIds}, nil
}

// InsertOpFromJSON builds an InsertOp from a raw op (Node: `InsertOp.fromJSON`).
func InsertOpFromJSON(op any) (InsertOp, error) {
	if s, ok := op.(string); ok {
		return NewInsertOp(s, nil, nil)
	}
	if o, ok := op.(map[string]any); ok {
		i, okI := o["i"].(string)
		if !okI {
			return InsertOp{}, fmt.Errorf("insert operation must have a string property")
		}
		var tracking TrackingDirective
		var commentIds []string
		if raw, present := o["tracking"]; present {
			if tm, okTM := raw.(map[string]any); okTM {
				tp, err := FromRawTrackingProps(tm)
				if err != nil {
					return InsertOp{}, err
				}
				tracking = tp
			}
		}
		if raw, present := o["commentIds"]; present {
			if arr, okA := raw.([]any); okA {
				for _, v := range arr {
					if s, okS := v.(string); okS {
						commentIds = append(commentIds, s)
					}
				}
			}
		}
		return NewInsertOp(i, tracking, commentIds)
	}
	return InsertOp{}, fmt.Errorf("insert operation must have a string property")
}

// ApplyToLength grows the output length (Node: `applyToLength`).
func (i InsertOp) ApplyToLength(ctx *applyCtx) error {
	ctx.length += len(i.Insertion)
	return nil
}

// ToJSON returns the string form or {i,tracking,commentIds} (Node: `toJSON`).
func (i InsertOp) ToJSON() any {
	if i.Tracking == nil && len(i.CommentIds) == 0 {
		return i.Insertion
	}
	out := map[string]any{"i": i.Insertion}
	if i.Tracking != nil {
		out["tracking"] = i.Tracking.ToRaw()
	}
	if len(i.CommentIds) > 0 {
		arr := make([]any, len(i.CommentIds))
		for k, v := range i.CommentIds {
			arr[k] = v
		}
		out["commentIds"] = arr
	}
	return out
}

// Equals compares insertion + tracking + commentIds (Node: `equals`).
func (i InsertOp) Equals(other ScanOp) bool {
	o, ok := other.(InsertOp)
	if !ok {
		return false
	}
	if i.Insertion != o.Insertion {
		return false
	}
	if i.Tracking != nil {
		if !i.Tracking.Equals(o.Tracking) {
			return false
		}
	} else if o.Tracking != nil {
		return false
	}
	if len(i.CommentIds) > 0 {
		return commentIdsEqual(i.CommentIds, o.CommentIds)
	}
	return len(o.CommentIds) == 0
}

// CanMergeWith reports mergeability (Node: `canMergeWith`).
func (i InsertOp) CanMergeWith(other ScanOp) bool {
	o, ok := other.(InsertOp)
	if !ok {
		return false
	}
	if i.Tracking != nil {
		if o.Tracking == nil || !i.Tracking.CanMergeWith(o.Tracking) {
			return false
		}
	} else if o.Tracking != nil {
		return false
	}
	if len(i.CommentIds) > 0 {
		return commentIdsEqual(i.CommentIds, o.CommentIds)
	}
	return len(o.CommentIds) == 0
}

// MergeWith returns the merged InsertOp (Node: `mergeWith`).
func (i InsertOp) MergeWith(other ScanOp) (ScanOp, error) {
	if !i.CanMergeWith(other) {
		return nil, fmt.Errorf("Cannot merge with incompatible operation")
	}
	o := other.(InsertOp)
	out := i
	out.Insertion += o.Insertion
	if i.Tracking != nil && o.Tracking != nil {
		merged, err := i.Tracking.MergeWith(o.Tracking)
		if err != nil {
			return nil, err
		}
		out.Tracking = merged
	}
	// commentIds are already equal
	return out, nil
}

// String renders "insert '...'" (Node: `toString`).
func (i InsertOp) String() string { return fmt.Sprintf("insert '%s'", i.Insertion) }

func commentIdsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, y := range b {
		if !set[y] {
			return false
		}
	}
	return true
}

// === RemoveOp (Node: RemoveOp) ==============================================

// RemoveOp removes characters (Node: RemoveOp; positive length).
type RemoveOp struct {
	Length int
}

func (RemoveOp) isScanOp() {}

// NewRemoveOp builds a RemoveOp (Node: `new RemoveOp(length)`).
func NewRemoveOp(length int) (RemoveOp, error) {
	if length < 0 {
		return RemoveOp{}, fmt.Errorf("length must be non-negative")
	}
	return RemoveOp{Length: length}, nil
}

// RemoveOpFromJSON builds a RemoveOp from a raw op (Node: `RemoveOp.fromJSON`).
// The raw form is a negative number.
func RemoveOpFromJSON(op any) (RemoveOp, error) {
	n, ok := op.(int)
	if !ok || n > 0 {
		return RemoveOp{}, fmt.Errorf("delete operation must be a negative number")
	}
	return NewRemoveOp(-n)
}

// ApplyToLength advances the input cursor (Node: `applyToLength`).
func (r RemoveOp) ApplyToLength(ctx *applyCtx) error {
	ctx.inputCursor += r.Length
	return nil
}

// ToJSON returns the negative number (Node: `toJSON`).
func (r RemoveOp) ToJSON() any { return -r.Length }

// Equals compares length (Node: `equals`).
func (r RemoveOp) Equals(other ScanOp) bool {
	o, ok := other.(RemoveOp)
	return ok && r.Length == o.Length
}

// CanMergeWith reports other is a RemoveOp (Node: `canMergeWith`).
func (r RemoveOp) CanMergeWith(other ScanOp) bool {
	_, ok := other.(RemoveOp)
	return ok
}

// MergeWith returns the merged RemoveOp (Node: `mergeWith`).
func (r RemoveOp) MergeWith(other ScanOp) (ScanOp, error) {
	if !r.CanMergeWith(other) {
		return nil, fmt.Errorf("Cannot merge with incompatible operation")
	}
	out := r
	out.Length += other.(RemoveOp).Length
	return out, nil
}

// String renders "remove N" (Node: `toString`).
func (r RemoveOp) String() string { return fmt.Sprintf("remove %d", r.Length) }

// === raw-type predicates (Node: isRetain/isInsert/isRemove) ================

// IsRetain reports the raw op is a retain (Node: `isRetain`).
func IsRetain(op any) bool {
	if n, ok := op.(int); ok {
		return n > 0
	}
	if o, ok := op.(map[string]any); ok {
		if r, okR := o["r"].(int); okR {
			return r > 0
		}
	}
	return false
}

// IsInsert reports the raw op is an insert (Node: `isInsert`).
func IsInsert(op any) bool {
	if _, ok := op.(string); ok {
		return true
	}
	if o, ok := op.(map[string]any); ok {
		_, okI := o["i"].(string)
		return okI
	}
	return false
}

// IsRemove reports the raw op is a remove (Node: `isRemove`).
func IsRemove(op any) bool {
	n, ok := op.(int)
	return ok && n < 0
}

// ScanOpFromJSON builds a ScanOp from a raw op (Node: `ScanOp.fromJSON`).
func ScanOpFromJSON(raw any) (ScanOp, error) {
	switch {
	case IsRetain(raw):
		return RetainOpFromJSON(raw)
	case IsInsert(raw):
		return InsertOpFromJSON(raw)
	case IsRemove(raw):
		return RemoveOpFromJSON(raw)
	default:
		return nil, NewUnprocessableError("Invalid ScanOp " + fmt.Sprint(raw))
	}
}
