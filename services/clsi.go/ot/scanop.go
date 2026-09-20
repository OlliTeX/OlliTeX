package ot

import (
	"errors"
	"fmt"
)

// --- Raw scan-op dispatch (port of scan_op.js isRetain/isInsert/isRemove) ---

func isRawRetain(v interface{}) bool {
	if n, ok := v.(float64); ok {
		return n > 0
	}
	if m, ok := v.(map[string]interface{}); ok {
		if r, ok := m["r"].(float64); ok && r > 0 {
			return true
		}
	}
	return false
}

func isRawInsert(v interface{}) bool {
	if _, ok := v.(string); ok {
		return true
	}
	if m, ok := v.(map[string]interface{}); ok {
		if _, ok := m["i"].(string); ok {
			return true
		}
	}
	return false
}

func isRawRemove(v interface{}) bool {
	if n, ok := v.(float64); ok {
		return n < 0
	}
	return false
}

// ScanOp is one atomic scan operation: retain, insert or remove.
type ScanOp interface {
	toJSONRaw() interface{}
	// kind is "retain", "insert" or "remove", mirroring the JS instanceof
	// checks in the apply path.
	kind() string
	// Equals mirrors equals: both operands must be the concrete type.
	Equals(other ScanOp) bool
	// CanMergeWith mirrors canMergeWith on the base ScanOp (always false);
	// concrete types override this on their structs.
	CanMergeWith(other ScanOp) bool
	// MergeWith mirrors mergeWith; mutates the receiver in place (JS is
	// the same: mergeWith mutates `this`).
	MergeWith(other ScanOp)
	// ApplyToLength mirrors applyToLength: adds the op's effect to the
	// running length/position context and errors on overrun.
	ApplyToLength(ctx *LengthApplyCtx) error
}

// LengthApplyCtx mirrors the JS LengthApplyContext {length, inputCursor,
// inputLength}. Go uses value receivers + pointer for the mutable fields.
type LengthApplyCtx struct {
	Length      int
	InputCursor int
	InputLength int
}

// --- RetainOp ---

type RetainOp struct {
	Length   int
	Tracking *TrackingProps // nil = no tracking directive present
}

func RetainOpFromJSON(v interface{}) (ScanOp, error) {
	if n, ok := v.(float64); ok {
		length := int(n)
		if length < 0 {
			return nil, fmt.Errorf("length must be non-negative")
		}
		return &RetainOp{Length: length}, nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, NewUnprocessableError("Invalid ScanOp " + jsonString(v))
	}
	r, ok := m["r"].(float64)
	if !ok {
		return nil, errors.New("retain operation must have a number property")
	}
	length := int(r)
	if length < 0 {
		return nil, errors.New("length must be non-negative")
	}
	op := &RetainOp{Length: length}
	if tr, ok := m["tracking"].(map[string]interface{}); ok {
		// Mirrors: op.tracking && (op.tracking.type === 'none' ? new
		// ClearTrackingProps() : TrackingProps.fromRaw(op.tracking))
		// For apply purposes the presence is what matters; CLSI raw never
		// carries tracking so this is a faithfulness fallback.
		tp, _ := trackingFromRaw(tr)
		op.Tracking = tp
	}
	return op, nil
}

func (o *RetainOp) kind() string { return "retain" }

// RetainOp.Equals mirrors RetainOp.equals.
func (o *RetainOp) Equals(other ScanOp) bool {
	oth, ok := other.(*RetainOp)
	if !ok {
		return false
	}
	if o.Length != oth.Length {
		return false
	}
	if o.Tracking != nil {
		return o.Tracking.Equals(oth.Tracking)
	}
	return oth.Tracking == nil
}

// RetainOp.CanMergeWith mirrors RetainOp.canMergeWith.
func (o *RetainOp) CanMergeWith(other ScanOp) bool {
	ot, ok := other.(*RetainOp)
	if !ok {
		return false
	}
	if o.Tracking != nil {
		if ot.Tracking == nil {
			return false
		}
		return o.Tracking.CanMergeWith(ot.Tracking)
	}
	return ot.Tracking == nil
}

// RetainOp.MergeWith mutates the receiver (JS is the same).
func (o *RetainOp) MergeWith(other ScanOp) {
	ot, ok := other.(*RetainOp)
	if !ok || !o.CanMergeWith(other) {
		panic("Cannot merge with incompatible operation")
	}
	o.Length += ot.Length
	if o.Tracking != nil && ot.Tracking != nil {
		o.Tracking, _ = o.Tracking.MergeWith(ot.Tracking)
	}
}

// RetainOp.ApplyToLength mirrors RetainOp.applyToLength.
func (o *RetainOp) ApplyToLength(ctx *LengthApplyCtx) error {
	if ctx.InputCursor+o.Length > ctx.InputLength {
		return NewApplyError(
			"Operation can't retain more chars than are left in the string.",
			o.toJSONRaw(),
			ctx.InputLength,
		)
	}
	ctx.Length += o.Length
	ctx.InputCursor += o.Length
	return nil
}

func (o *RetainOp) toJSONRaw() interface{} {
	if o.Tracking == nil {
		return int(o.Length)
	}
	return map[string]interface{}{
		"r":        int(o.Length),
		"tracking": o.Tracking.ToRaw(),
	}
}

// --- InsertOp ---

type InsertOp struct {
	Insertion  string
	Tracking   *TrackingProps
	CommentIDs []string
}

func newInsertOp(insertion string, tracking *TrackingProps, commentIDs []string) (*InsertOp, error) {
	if containsNonBmpChars(insertion) {
		return nil, NewInvalidInsertionError(insertion)
	}
	return &InsertOp{Insertion: insertion, Tracking: tracking, CommentIDs: commentIDs}, nil
}

func insertOpFromJSON(v interface{}) (ScanOp, error) {
	if s, ok := v.(string); ok {
		return newInsertOp(s, nil, nil)
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, NewUnprocessableError("Invalid ScanOp " + jsonString(v))
	}
	i, ok := m["i"].(string)
	if !ok {
		return nil, NewInvalidInsertionError("")
	}
	op, err := newInsertOp(i, nil, nil)
	if err != nil {
		return nil, err
	}
	if tr, ok := m["tracking"].(map[string]interface{}); ok {
		TP, _ := trackingFromRaw(tr)
		op.Tracking = TP
	}
	if cids, ok := m["commentIds"].([]interface{}); ok {
		for _, c := range cids {
			if s, ok := c.(string); ok {
				op.CommentIDs = append(op.CommentIDs, s)
			}
		}
	}
	return op, nil
}

func (o *InsertOp) kind() string { return "insert" }

// InsertOp.Equals mirrors InsertOp.equals.
func (o *InsertOp) Equals(other ScanOp) bool {
	ot, ok := other.(*InsertOp)
	if !ok {
		return false
	}
	if o.Insertion != ot.Insertion {
		return false
	}
	if o.Tracking != nil {
		return o.Tracking.Equals(ot.Tracking)
	} else if ot.Tracking != nil {
		return false
	}
	return equalStringSlices(o.CommentIDs, ot.CommentIDs)
}

// InsertOp.CanMergeWith mirrors InsertOp.canMergeWith.
func (o *InsertOp) CanMergeWith(other ScanOp) bool {
	ot, ok := other.(*InsertOp)
	if !ok {
		return false
	}
	if o.Tracking != nil {
		if ot.Tracking == nil {
			return false
		}
		if !o.Tracking.CanMergeWith(ot.Tracking) {
			return false
		}
	} else if ot.Tracking != nil {
		return false
	}
	if o.CommentIDs != nil {
		return equalStringSlices(o.CommentIDs, ot.CommentIDs)
	}
	return ot.CommentIDs == nil
}

// InsertOp.MergeWith mutates the receiver (JS is the same).
func (o *InsertOp) MergeWith(other ScanOp) {
	ot, ok := other.(*InsertOp)
	if !ok || !o.CanMergeWith(other) {
		panic("Cannot merge with incompatible operation")
	}
	o.Insertion += ot.Insertion
	if o.Tracking != nil && ot.Tracking != nil {
		o.Tracking, _ = o.Tracking.MergeWith(ot.Tracking)
	}
	// commentIDs are already equal per canMergeWith
}

// InsertOp.ApplyToLength mirrors InsertOp.applyToLength. Uses UTF-16 units
// for `insertion.length` so surrogate pairs count as 2.
func (o *InsertOp) ApplyToLength(ctx *LengthApplyCtx) error {
	ctx.Length += utf16Len(o.Insertion)
	return nil
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (o *InsertOp) toJSONRaw() interface{} {
	if o.Tracking == nil && o.CommentIDs == nil {
		return o.Insertion
	}
	out := map[string]interface{}{"i": o.Insertion}
	if o.Tracking != nil {
		out["tracking"] = o.Tracking.ToRaw()
	}
	if o.CommentIDs != nil {
		strs := make([]string, len(o.CommentIDs))
		copy(strs, o.CommentIDs)
		out["commentIds"] = strs
	}
	return out
}

// --- RemoveOp ---

type RemoveOp struct {
	Length int
}

func removeOpFromJSON(v interface{}) (ScanOp, error) {
	n, ok := v.(float64)
	if !ok || n > 0 {
		return nil, errors.New("delete operation must be a negative number")
	}
	return &RemoveOp{Length: int(-n)}, nil
}

func (o *RemoveOp) kind() string { return "remove" }

// RemoveOp.Equals mirrors RemoveOp.equals.
func (o *RemoveOp) Equals(other ScanOp) bool {
	ot, ok := other.(*RemoveOp)
	if !ok {
		return false
	}
	return o.Length == ot.Length
}

// RemoveOp.CanMergeWith mirrors RemoveOp.canMergeWith.
func (o *RemoveOp) CanMergeWith(other ScanOp) bool {
	_, ok := other.(*RemoveOp)
	return ok
}

// RemoveOp.MergeWith mutates the receiver (JS is the same).
func (o *RemoveOp) MergeWith(other ScanOp) {
	ot, ok := other.(*RemoveOp)
	if !ok {
		panic("Cannot merge with incompatible operation")
	}
	o.Length += ot.Length
}

// RemoveOp.ApplyToLength mirrors RemoveOp.applyToLength.
func (o *RemoveOp) ApplyToLength(ctx *LengthApplyCtx) error {
	ctx.InputCursor += o.Length
	return nil
}

func (o *RemoveOp) toJSONRaw() interface{} {
	return int(-o.Length)
}

// ScanOpFromJSON dispatches a raw scan op to retain/insert/remove.
// Mirrors ScanOp.fromJSON.
func ScanOpFromJSON(v interface{}) (ScanOp, error) {
	switch {
	case isRawRetain(v):
		return RetainOpFromJSON(v)
	case isRawInsert(v):
		return insertOpFromJSON(v)
	case isRawRemove(v):
		return removeOpFromJSON(v)
	default:
		return nil, NewUnprocessableError("Invalid ScanOp " + jsonString(v))
	}
}
