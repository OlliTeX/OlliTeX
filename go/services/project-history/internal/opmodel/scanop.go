package opmodel

import (
	"encoding/json"
	"fmt"
)

// Scan operations (vendor lib/operation/scan_op.js).
//
// A TextOperation is a list of scan ops:
//   - RetainOp: advance the cursor by n UTF-16 units. Wire: number (n>0) or
//     {r: n, tracking?}.
//   - InsertOp: insert a string at the cursor. Wire: string or
//     {i: s, tracking?, commentIds?}.
//   - RemoveOp: remove the next n UTF-16 units. Wire: NEGATIVE number.
//     Internal length is always positive (vendor RemoveOp.length);
//     RemoveOp.fromJSON(op) takes op <= 0 and stores -op.
//
// Lengths are UTF-16 code units (JS str.length); insertions are Go strings
// and their length is Units(s).

// ScanOp — one scan operation. Concrete types: RetainOp, InsertOp, RemoveOp
// (value structs). Interface mirrors the vendor `ScanOp` base class API.
type ScanOp interface {
	// Kind returns "retain" | "insert" | "remove".
	Kind() string
	// Equals mirrors vendor `equals`.
	Equals(other ScanOp) bool
	// CanMergeWith mirrors vendor `canMergeWith`.
	CanMergeWith(other ScanOp) bool
	// MergeWith mirrors vendor `mergeWith` (caller ensures CanMergeWith).
	MergeWith(other ScanOp)
	// ToWire mirrors vendor `toJSON` (wire JSON value).
	ToWire() any
	// ApplyToLength mirrors vendor `applyToLength(current)`, mutating ctx.
	ApplyToLength(ctx *LengthApplyContext) error
	// String mirrors vendor `toString`.
	String() string
}

// RetainOp — vendor `class RetainOp extends ScanOp`.
type RetainOp struct {
	Length   int
	Tracking *Tracking
}

func (RetainOp) Kind() string { return "retain" }

// length must be non-negative (vendor throws).
func NewRetainOp(length int, tracking *Tracking) RetainOp {
	if length < 0 {
		panic("length must be non-negative")
	}
	return RetainOp{Length: length, Tracking: tracking}
}

// RetainOpFromJSON — vendor `RetainOp.fromJSON(op)`: a positive number, or an
// object {r: number, tracking?}. The tracking 'none' directive decodes to a
// Clear (vendor: `op.tracking.type === 'none' ? new ClearTrackingProps() :
// TrackingProps.fromRaw(op.tracking)`).
func RetainOpFromJSON(op any) (RetainOp, error) {
	if n, ok := asFloat64(op); ok {
		if n < 0 {
			return RetainOp{}, NewUnprocessableError(fmt.Sprintf("Invalid ScanOp %v", op), nil)
		}
		return RetainOp{Length: int(n)}, nil
	}
	if m, ok := op.(map[string]any); ok {
		r, ok := m["r"]
		v, isNum := asFloat64(r)
		if !ok || !isNum {
			return RetainOp{}, NewUnprocessableError("retain operation must have a number property", nil)
		}
		if v < 0 {
			return RetainOp{}, NewUnprocessableError(fmt.Sprintf("Invalid ScanOp %v", op), nil)
		}
		out := RetainOp{Length: int(v)}
		if tr, has := m["tracking"]; has && tr != nil {
			out.Tracking = DecodeDirective(tr)
		}
		return out, nil
	}
	return RetainOp{}, NewUnprocessableError(fmt.Sprintf("Invalid ScanOp %v", op), nil)
}

// RetainOpFromRawLength — direct (positive) length form used by the
// TextOperation.fromJSON retain path (retail.length after the isRetain check
// guarantees > 0) and by builders.
func RetainOpFromRawLength(n int, tracking *Tracking) RetainOp {
	return RetainOp{Length: n, Tracking: tracking}
}

func (r RetainOp) Equals(o ScanOp) bool {
	or, ok := o.(RetainOp)
	if !ok {
		return false
	}
	if r.Length != or.Length {
		return false
	}
	if r.Tracking != nil {
		return r.Tracking.Equals(or.Tracking)
	}
	return or.Tracking == nil
}

func (r RetainOp) CanMergeWith(o ScanOp) bool {
	or, ok := o.(RetainOp)
	if !ok {
		return false
	}
	if r.Tracking != nil {
		return r.Tracking.CanMergeWith(or.Tracking)
	}
	return or.Tracking == nil
}

func (r RetainOp) MergeWith(o ScanOp) {
	or, ok := o.(RetainOp)
	if !ok || !r.CanMergeWith(o) {
		panic("Cannot merge with incompatible operation")
	}
	r.Length += or.Length
	if r.Tracking != nil && or.Tracking != nil {
		r.Tracking = r.Tracking.MergeWith(or.Tracking)
	}
}

// ToWire — vendor RetainOp.toJSON: number when no tracking, else {r, tracking}.
func (r RetainOp) ToWire() any {
	if r.Tracking == nil {
		return r.Length
	}
	return map[string]any{
		"r":        r.Length,
		"tracking": r.Tracking.ToRaw(),
	}
}

// ApplyToLength — vendor RetainOp.applyToLength: advances length AND
// inputCursor; throws ApplyError if it would exceed the input.
func (r RetainOp) ApplyToLength(ctx *LengthApplyContext) error {
	if ctx.InputCursor+r.Length > ctx.InputLength {
		return NewApplyError("Operation can't retain more chars than are left in the string.", r.ToWire(), ctx.InputLength)
	}
	ctx.Length += r.Length
	ctx.InputCursor += r.Length
	return nil
}

func (r RetainOp) String() string { return fmt.Sprintf("retain %d", r.Length) }

// InsertOp — vendor `class InsertOp extends ScanOp`.
type InsertOp struct {
	Insertion  string
	Tracking   *Tracking
	CommentIDs []string
}

func (InsertOp) Kind() string { return "insert" }

// NewInsertOp — vendor `new InsertOp(insertion, tracking, commentIds)`:
// insertion must be a string without non-BMP characters.
func NewInsertOp(insertion string, tracking *Tracking, commentIDs []string) (InsertOp, error) {
	if containsNonBmpChars(insertion) {
		return InsertOp{}, NewInvalidInsertionError(insertion, nil)
	}
	return InsertOp{Insertion: insertion, Tracking: tracking, CommentIDs: commentIDs}, nil
}

// InsertOpFromJSON — vendor `InsertOp.fromJSON(op)`: a string, or an object
// with an 'i' string property (plus tracking/commentIds). Note (vendor
// quirk): the insert path decodes tracking via TrackingProps.fromRaw only —
// it does NOT special-case type:'none' (that special case is retain-only).
func InsertOpFromJSON(op any) (InsertOp, error) {
	if s, ok := op.(string); ok {
		if containsNonBmpChars(s) {
			return InsertOp{}, NewInvalidInsertionError(s, nil)
		}
		return InsertOp{Insertion: s}, nil
	}
	if m, ok := op.(map[string]any); ok {
		i, ok := m["i"].(string)
		if !ok {
			return InsertOp{}, NewInvalidInsertionError("insert operation must have a string property", nil)
		}
		out := InsertOp{Insertion: i}
		if containsNonBmpChars(i) {
			return InsertOp{}, NewInvalidInsertionError(i, nil)
		}
		if tr, has := m["tracking"]; has && tr != nil {
			out.Tracking = DecodeDirective(tr)
		}
		if c, has := m["commentIds"]; has && c != nil {
			for _, id := range asSlice(c) {
				if s, ok := id.(string); ok {
					out.CommentIDs = append(out.CommentIDs, s)
				}
			}
		}
		return out, nil
	}
	return InsertOp{}, NewUnprocessableError(fmt.Sprintf("Invalid ScanOp %v", op), nil)
}

func (i InsertOp) Equals(o ScanOp) bool {
	oi, ok := o.(InsertOp)
	if !ok {
		return false
	}
	if i.Insertion != oi.Insertion {
		return false
	}
	if i.Tracking != nil {
		if !i.Tracking.Equals(oi.Tracking) {
			return false
		}
	} else if oi.Tracking != nil {
		return false
	}
	if i.CommentIDs != nil {
		// Vendor: this.commentIds.length === other?.length && every
		// commentIds.every(id => other.commentIds?.includes(id))
		return len(i.CommentIDs) == len(oi.CommentIDs) &&
			idsCover(i.CommentIDs, oi.CommentIDs)
	}
	return oi.CommentIDs == nil
}

func (i InsertOp) CanMergeWith(o ScanOp) bool {
	oi, ok := o.(InsertOp)
	if !ok {
		return false
	}
	if i.Tracking != nil {
		if oi.Tracking == nil || !i.Tracking.CanMergeWith(oi.Tracking) {
			return false
		}
	} else if oi.Tracking != nil {
		return false
	}
	if i.CommentIDs != nil {
		return len(i.CommentIDs) == len(oi.CommentIDs) &&
			idsCover(i.CommentIDs, oi.CommentIDs)
	}
	return oi.CommentIDs == nil
}

func (i InsertOp) MergeWith(o ScanOp) {
	oi, ok := o.(InsertOp)
	if !ok || !i.CanMergeWith(o) {
		panic("Cannot merge with incompatible operation")
	}
	i.Insertion += oi.Insertion
	if i.Tracking != nil && oi.Tracking != nil {
		i.Tracking = i.Tracking.MergeWith(oi.Tracking)
	}
	// commentIds already equal per canMergeWith
}

// ToWire — vendor InsertOp.toJSON: plain string when no tracking/commentIds.
func (i InsertOp) ToWire() any {
	if i.Tracking == nil && i.CommentIDs == nil {
		return i.Insertion
	}
	out := map[string]any{"i": i.Insertion}
	if i.Tracking != nil {
		out["tracking"] = i.Tracking.ToRaw()
	}
	if i.CommentIDs != nil {
		out["commentIds"] = i.CommentIDs
	}
	return out
}

// ApplyToLength — vendor InsertOp.applyToLength: length += insertion length
// (UTF-16 units); inputCursor is NOT advanced (insertions are output).
func (i InsertOp) ApplyToLength(ctx *LengthApplyContext) error {
	ctx.Length += Units(i.Insertion)
	return nil
}

func (i InsertOp) String() string { return fmt.Sprintf("insert '%s'", i.Insertion) }

// RemoveOp — vendor `class RemoveOp extends ScanOp`. Internal length is
// positive; the wire form is negative.
type RemoveOp struct {
	Length int
}

func (RemoveOp) Kind() string { return "remove" }

// length must be non-negative (vendor throws).
func NewRemoveOp(length int) RemoveOp {
	if length < 0 {
		panic("length must be non-negative")
	}
	return RemoveOp{Length: length}
}

// RemoveOpFromJSON — vendor `RemoveOp.fromJSON(op)`: a number <= 0 (wire);
// returns RemoveOp(-op).
func RemoveOpFromJSON(op any) (RemoveOp, error) {
	n, ok := asFloat64(op)
	if !ok || n > 0 {
		return RemoveOp{}, NewUnprocessableError("delete operation must be a negative number", nil)
	}
	return RemoveOp{Length: int(-n)}, nil
}

func (r RemoveOp) Equals(o ScanOp) bool {
	or, ok := o.(RemoveOp)
	if !ok {
		return false
	}
	return r.Length == or.Length
}

func (r RemoveOp) CanMergeWith(o ScanOp) bool {
	_, ok := o.(RemoveOp)
	return ok
}

func (r RemoveOp) MergeWith(o ScanOp) {
	ro, ok := o.(RemoveOp)
	if !ok || !r.CanMergeWith(o) {
		panic("Cannot merge with incompatible operation")
	}
	r.Length += ro.Length
}

// ToWire — vendor RemoveOp.toJSON: NEGATIVE number.
func (r RemoveOp) ToWire() any { return -r.Length }

// ApplyToLength — vendor RemoveOp.applyToLength: inputCursor += length
// (removal consumes input); length is NOT changed.
func (r RemoveOp) ApplyToLength(ctx *LengthApplyContext) error {
	ctx.InputCursor += r.Length
	return nil
}

func (r RemoveOp) String() string { return fmt.Sprintf("remove %d", r.Length) }

// ScanOpFromJSON — vendor `ScanOp.fromJSON(raw)` dispatch (isRetain / isInsert
// / isRemove), else UnprocessableError("Invalid ScanOp …").
func ScanOpFromJSON(raw any) (ScanOp, error) {
	if IsRetainWire(raw) {
		r, err := RetainOpFromJSON(raw)
		return r, err
	}
	if IsInsertWire(raw) {
		i, err := InsertOpFromJSON(raw)
		return i, err
	}
	if IsRemoveWire(raw) {
		r, err := RemoveOpFromJSON(raw)
		return r, err
	}
	return nil, NewUnprocessableError("Invalid ScanOp "+wireString(raw), nil)
}

// IsRetainWire — vendor `isRetain`: number>0 or {r: number>0}.
func IsRetainWire(op any) bool {
	if n, ok := asFloat64(op); ok {
		return n > 0
	}
	if m, ok := op.(map[string]any); ok {
		if r, has := m["r"]; has {
			if v, ok2 := asFloat64(r); ok2 {
				return v > 0
			}
		}
	}
	return false
}

// IsInsertWire — vendor `isInsert`: string or {i: string}.
func IsInsertWire(op any) bool {
	if _, ok := op.(string); ok {
		return true
	}
	if m, ok := op.(map[string]any); ok {
		_, has := m["i"].(string)
		return has
	}
	return false
}

// IsRemoveWire — vendor `isRemove`: number<0.
func IsRemoveWire(op any) bool {
	n, ok := asFloat64(op)
	return ok && n < 0
}

// LengthApplyContext — vendor `LengthApplyContext`
// ({length, inputCursor, inputLength: readonly}): threaded through
// TextOperation.applyToLength.
type LengthApplyContext struct {
	Length      int
	InputCursor int
	InputLength int
}

// --- tiny wire-decode helpers (Go JSON unmarshals numbers as float64) ---

func asFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// wireString — stable rendering for error messages (vendor: JSON.stringify).
func wireString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func idsCover(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		found := false
		for _, y := range b {
			if y == x {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// containsNonBmpChars — vendor util.containsNonBmpChars: a leading high
// surrogate, i.e. any UTF-16 unit in D800–DBFF. In Go: any rune > 0xFFFF.
func containsNonBmpChars(s string) bool {
	for _, r := range s {
		if r > 0xFFFF {
			return true
		}
	}
	return false
}
