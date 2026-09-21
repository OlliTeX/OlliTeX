package otc

import (
	"fmt"
	"sort"
)

// MaxStringLength is the longest file we'll attempt to edit
// (Node: TextOperation.MAX_STRING_LENGTH = 3 * 1024^2).
const MaxStringLength = 3 * 1024 * 1024

// RetainBuilderOpts carries the optional tracking directive for a retain.
type RetainBuilderOpts struct{ Tracking TrackingDirective }

// InsertBuilderOpts carries optional tracking + comment ids for an insert.
type InsertBuilderOpts struct {
	Tracking   TrackingDirective
	CommentIds []string
}

// TextOperation is a text OT operation (Node: operation/text_operation.js).
type TextOperation struct {
	Ops          []ScanOp
	BaseLength   int
	TargetLength int
	ContentHash  *string
}

// NewTextOperation creates an empty text operation (Node: constructor).
func NewTextOperation() *TextOperation {
	return &TextOperation{}
}

// Equals reports structural equality with another operation
// (Node: `equals`).
func (o *TextOperation) Equals(other *TextOperation) bool {
	if o.BaseLength != other.BaseLength {
		return false
	}
	if o.TargetLength != other.TargetLength {
		return false
	}
	if len(o.Ops) != len(other.Ops) {
		return false
	}
	for i := range o.Ops {
		if !o.Ops[i].Equals(other.Ops[i]) {
			return false
		}
	}
	return true
}

// Retain skips over n characters (Node: `retain`).
func (o *TextOperation) Retain(n int, opts RetainBuilderOpts) error {
	if n == 0 {
		return nil
	}
	newOp := RetainOp{Length: n, Tracking: opts.Tracking}
	if newOp.Length == 0 {
		return nil
	}
	o.BaseLength += newOp.Length
	o.TargetLength += newOp.Length
	if lastIdx := len(o.Ops) - 1; lastIdx >= 0 {
		if last := o.Ops[lastIdx]; last.CanMergeWith(newOp) {
			merged, err := last.MergeWith(newOp)
			if err != nil {
				return err
			}
			o.Ops[lastIdx] = merged
			return nil
		}
	}
	o.Ops = append(o.Ops, newOp)
	return nil
}

// Insert inserts a string at the current position (Node: `insert`).
func (o *TextOperation) Insert(insertion string, opts InsertBuilderOpts) error {
	newOp, err := NewInsertOp(insertion, opts.Tracking, opts.CommentIds)
	if err != nil {
		return err
	}
	if newOp.Insertion == "" {
		return nil
	}
	o.TargetLength += len(newOp.Insertion)
	if lastIdx := len(o.Ops) - 1; lastIdx >= 0 {
		if last := o.Ops[lastIdx]; last.CanMergeWith(newOp) {
			merged, err := last.MergeWith(newOp)
			if err != nil {
				return err
			}
			o.Ops[lastIdx] = merged
			return nil
		} else if _, isRemove := last.(RemoveOp); isRemove {
			// enforce that the insert op comes before the remove op
			if len(o.Ops) >= 2 {
				secondIdx := len(o.Ops) - 2
				if second := o.Ops[secondIdx]; second.CanMergeWith(newOp) {
					merged, err := second.MergeWith(newOp)
					if err != nil {
						return err
					}
					o.Ops[secondIdx] = merged
					return nil
				}
			}
			n := len(o.Ops)
			o.Ops = append(o.Ops, o.Ops[n-1])
			o.Ops[n-1] = newOp
			return nil
		}
	}
	o.Ops = append(o.Ops, newOp)
	return nil
}

// Remove removes n characters at the current position (Node: `remove`).
func (o *TextOperation) Remove(n int) error {
	if n == 0 {
		return nil
	}
	if n < 0 {
		n = -n
	}
	newOp, err := NewRemoveOp(n)
	if err != nil {
		return err
	}
	o.BaseLength += newOp.Length
	if lastIdx := len(o.Ops) - 1; lastIdx >= 0 {
		if last := o.Ops[lastIdx]; last.CanMergeWith(newOp) {
			merged, err := last.MergeWith(newOp)
			if err != nil {
				return err
			}
			o.Ops[lastIdx] = merged
			return nil
		}
	}
	o.Ops = append(o.Ops, newOp)
	return nil
}

// RemoveStr removes a string (Node: `remove(n)` with a string argument).
func (o *TextOperation) RemoveStr(s string) error { return o.Remove(len(s)) }

// IsNoop reports whether the operation has no effect (Node: `isNoop`).
func (o *TextOperation) IsNoop() bool {
	if len(o.Ops) == 0 {
		return true
	}
	if len(o.Ops) == 1 {
		if r, ok := o.Ops[0].(RetainOp); ok && r.Tracking == nil {
			return true
		}
	}
	return false
}

// String pretty-prints the operation (Node: `toString`).
func (o *TextOperation) String() string {
	parts := make([]string, len(o.Ops))
	for i, op := range o.Ops {
		parts[i] = op.String()
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// ToJSON serialises as {textOperation, contentHash?} (Node: `toJSON`).
func (o *TextOperation) ToJSON() map[string]any {
	json_ := map[string]any{}
	arr := make([]any, len(o.Ops))
	for i, op := range o.Ops {
		arr[i] = op.ToJSON()
	}
	json_["textOperation"] = arr
	if o.ContentHash != nil {
		json_["contentHash"] = *o.ContentHash
	}
	return json_
}

// FromJSONTextOperation builds and validates an operation from raw
// (Node: `TextOperation.fromJSON`).
func FromJSONTextOperation(raw map[string]any) (*TextOperation, error) {
	o := NewTextOperation()
	opsRaw, ok := raw["textOperation"].([]any)
	if !ok {
		return nil, NewUnprocessableError("unknown operation")
	}
	for _, opRaw := range opsRaw {
		if IsRetain(opRaw) {
			retain, err := RetainOpFromJSON(opRaw)
			if err != nil {
				return nil, err
			}
			if err := o.Retain(retain.Length, RetainBuilderOpts{Tracking: retain.Tracking}); err != nil {
				return nil, err
			}
		} else if IsInsert(opRaw) {
			insert, err := InsertOpFromJSON(opRaw)
			if err != nil {
				return nil, err
			}
			if err := o.Insert(insert.Insertion, InsertBuilderOpts{Tracking: insert.Tracking, CommentIds: insert.CommentIds}); err != nil {
				return nil, err
			}
		} else if IsRemove(opRaw) {
			remove, err := RemoveOpFromJSON(opRaw)
			if err != nil {
				return nil, err
			}
			if err := o.Remove(remove.Length); err != nil {
				return nil, err
			}
		} else {
			return nil, NewUnprocessableError("unknown operation: " + fmt.Sprint(opRaw))
		}
	}
	if ch, okCh := raw["contentHash"].(string); okCh {
		o.ContentHash = &ch
	}
	return o, nil
}

// Apply applies the operation to a file, mutating it (Node: `apply`).
func (o *TextOperation) Apply(file *StringFileData) error {
	str := file.Content
	if len(str) != o.BaseLength {
		return NewApplyError("The operation's base length must be equal to the string's length.", o, str)
	}
	inputCursor := 0
	result := ""
	for _, opRaw := range o.Ops {
		switch op := opRaw.(type) {
		case RetainOp:
			if inputCursor+op.Length > len(str) {
				return NewApplyError("Operation can't retain more chars than are left in the string.", op.ToJSON(), str)
			}
			result += str[inputCursor : inputCursor+op.Length]
			inputCursor += op.Length
		case InsertOp:
			if file.Comments != nil {
				file.Comments.ApplyInsert(NewRange(len(result), len(op.Insertion)), op.CommentIds)
			}
			result += op.Insertion
		case RemoveOp:
			if file.Comments != nil {
				file.Comments.ApplyDelete(NewRange(len(result), op.Length))
			}
			inputCursor += op.Length
		default:
			return NewUnprocessableError("Unknown ScanOp type during apply")
		}
	}
	if inputCursor != len(str) {
		return NewApplyError("The operation didn't operate on the whole string.", o, str)
	}
	if len(result) > MaxStringLength {
		return NewTooLongError(o, len(result))
	}
	if file.TrackedChanges != nil {
		if err := file.TrackedChanges.ApplyTextOperation(o); err != nil {
			return err
		}
	}
	file.Content = result
	return nil
}

// ApplyToLength returns the resulting length for an input of `length`
// (Node: `applyToLength`).
func (o *TextOperation) ApplyToLength(length int) (int, error) {
	if length != o.BaseLength {
		return 0, NewApplyError("The operation's base length must be equal to the string's length.", o, length)
	}
	ctx := applyCtx{length: 0, inputCursor: 0, inputLength: length}
	for _, op := range o.Ops {
		if err := op.ApplyToLength(&ctx); err != nil {
			return 0, err
		}
	}
	if ctx.inputCursor != length {
		return 0, NewApplyError("The operation didn't operate on the whole string.", o, length)
	}
	if ctx.length > MaxStringLength {
		return 0, NewTooLongError(o, ctx.length)
	}
	return ctx.length, nil
}

// Invert returns the inverse operation (Node: `invert`).
func (o *TextOperation) Invert(previous *StringFileData) *TextOperation {
	str := previous.Content
	strIndex := 0
	inverse := NewTextOperation()
	for _, opRaw := range o.Ops {
		switch op := opRaw.(type) {
		case RetainOp:
			if op.Tracking != nil {
				target := strIndex + op.Length
				var previousChanges []TrackedChange
				if previous.TrackedChanges != nil {
					previousChanges = previous.TrackedChanges.IntersectRange(NewRange(strIndex, op.Length))
				}
				for _, change := range previousChanges {
					if strIndex < change.Range.Start() {
						_ = inverse.Retain(change.Range.Start()-strIndex, RetainBuilderOpts{Tracking: ClearTrackingProps{}})
						strIndex = change.Range.Start()
					}
					tp, _ := asTrackingProps(change.Tracking)
					_ = inverse.Retain(change.Range.Length, RetainBuilderOpts{Tracking: tp})
					strIndex += change.Range.Length
				}
				if strIndex < target {
					_ = inverse.Retain(target-strIndex, RetainBuilderOpts{Tracking: ClearTrackingProps{}})
					strIndex = target
				}
			} else {
				_ = inverse.Retain(op.Length, RetainBuilderOpts{})
				strIndex += op.Length
			}
		case InsertOp:
			_ = inverse.Remove(len(op.Insertion))
		case RemoveOp:
			var comments *CommentList
			var tracked *TrackedChangeList
			if previous.Comments != nil {
				comments = previous.Comments
			}
			if previous.TrackedChanges != nil {
				tracked = previous.TrackedChanges
			}
			segments := calculateTrackingCommentSegments(strIndex, op.Length, comments, tracked)
			for _, segment := range segments {
				s := str[strIndex : strIndex+segment.Length]
				_ = inverse.Insert(s, InsertBuilderOpts{Tracking: segment.Tracking, CommentIds: segment.CommentIds})
				strIndex += segment.Length
			}
		}
	}
	return inverse
}

// CanBeComposedWith reports whether two operations can be composed
// (Node: `canBeComposedWith`).
func (o *TextOperation) CanBeComposedWith(other *TextOperation) bool {
	return o.TargetLength == other.BaseLength
}

// CanBeComposedWithForUndo reports whether two operations are undo-composable
// (Node: `canBeComposedWithForUndo`).
func (o *TextOperation) CanBeComposedWithForUndo(other *TextOperation) bool {
	if o.IsNoop() || other.IsNoop() {
		return true
	}
	startA := getStartIndex(o)
	startB := getStartIndex(other)
	simpleA := getSimpleOp(o)
	simpleB := getSimpleOp(other)
	if simpleA == nil || simpleB == nil {
		return false
	}
	if a, okA := simpleA.(InsertOp); okA {
		if _, okB := simpleB.(InsertOp); okB {
			return startA+len(a.Insertion) == startB
		}
	}
	if _, okA := simpleA.(RemoveOp); okA {
		if b, okB := simpleB.(RemoveOp); okB {
			return startB+b.Length == startA || startA == startB
		}
	}
	return false
}

// Compose combines this operation with a second (Node: `compose`).
func (o *TextOperation) Compose(operation2 *TextOperation) (*TextOperation, error) {
	if o.TargetLength != operation2.BaseLength {
		return nil, NewUnprocessableError("The base length of the second operation has to be the target length of the first operation")
	}
	operation1 := o
	combined := NewTextOperation()
	ops1 := operation1.Ops
	ops2 := operation2.Ops
	i1, i2 := 0, 0
	op1 := takeOp(ops1, &i1)
	op2 := takeOp(ops2, &i2)
	for {
		if op1 == nil && op2 == nil {
			break
		}
		if r1, ok1 := op1.(RemoveOp); ok1 {
			_ = combined.Remove(r1.Length)
			op1 = takeOp(ops1, &i1)
			continue
		}
		if i2op, ok2 := op2.(InsertOp); ok2 {
			_ = combined.Insert(i2op.Insertion, InsertBuilderOpts{Tracking: i2op.Tracking, CommentIds: i2op.CommentIds})
			op2 = takeOp(ops2, &i2)
			continue
		}
		if op1 == nil {
			return nil, NewUnprocessableError("Cannot compose operations: first operation is too short.")
		}
		if op2 == nil {
			return nil, NewUnprocessableError("Cannot compose operations: first operation is too long.")
		}
		if r1, ok1 := op1.(RetainOp); ok1 {
			if r2, ok2 := op2.(RetainOp); ok2 {
				tracking := r2.Tracking
				if tracking == nil {
					tracking = r1.Tracking
				}
				if r1.Length > r2.Length {
					_ = combined.Retain(r2.Length, RetainBuilderOpts{Tracking: tracking})
					op1 = RetainOp{Length: r1.Length - r2.Length, Tracking: r1.Tracking}
					op2 = takeOp(ops2, &i2)
				} else if r1.Length == r2.Length {
					_ = combined.Retain(r1.Length, RetainBuilderOpts{Tracking: tracking})
					op1 = takeOp(ops1, &i1)
					op2 = takeOp(ops2, &i2)
				} else {
					_ = combined.Retain(r1.Length, RetainBuilderOpts{Tracking: tracking})
					op2 = RetainOp{Length: r2.Length - r1.Length, Tracking: r2.Tracking}
					op1 = takeOp(ops1, &i1)
				}
				continue
			}
		}
		if i1op, ok1 := op1.(InsertOp); ok1 {
			if r2, ok2 := op2.(RemoveOp); ok2 {
				if len(i1op.Insertion) > r2.Length {
					op1 = InsertOp{Insertion: i1op.Insertion[r2.Length:], Tracking: i1op.Tracking, CommentIds: i1op.CommentIds}
					op2 = takeOp(ops2, &i2)
				} else if len(i1op.Insertion) == r2.Length {
					op1 = takeOp(ops1, &i1)
					op2 = takeOp(ops2, &i2)
				} else {
					op2 = RemoveOp{Length: r2.Length - len(i1op.Insertion)}
					op1 = takeOp(ops1, &i1)
				}
				continue
			}
		}
		if i1op, ok1 := op1.(InsertOp); ok1 {
			if r2, ok2 := op2.(RetainOp); ok2 {
				opts := InsertBuilderOpts{CommentIds: i1op.CommentIds}
				if _, okTP := r2.Tracking.(TrackingProps); okTP {
					opts.Tracking = r2.Tracking
				} else if !isClearTracking(r2.Tracking) {
					opts.Tracking = i1op.Tracking
				}
				if len(i1op.Insertion) > r2.Length {
					_ = combined.Insert(i1op.Insertion[:r2.Length], opts)
					op1 = InsertOp{Insertion: i1op.Insertion[r2.Length:], Tracking: i1op.Tracking, CommentIds: i1op.CommentIds}
					op2 = takeOp(ops2, &i2)
				} else if len(i1op.Insertion) == r2.Length {
					_ = combined.Insert(i1op.Insertion, opts)
					op1 = takeOp(ops1, &i1)
					op2 = takeOp(ops2, &i2)
				} else {
					_ = combined.Insert(i1op.Insertion, opts)
					op2 = RetainOp{Length: r2.Length - len(i1op.Insertion), Tracking: r2.Tracking}
					op1 = takeOp(ops1, &i1)
				}
				continue
			}
		}
		if r1, ok1 := op1.(RetainOp); ok1 {
			if r2, ok2 := op2.(RemoveOp); ok2 {
				if r1.Length > r2.Length {
					_ = combined.Remove(r2.Length)
					op1 = RetainOp{Length: r1.Length - r2.Length, Tracking: r1.Tracking}
					op2 = takeOp(ops2, &i2)
				} else if r1.Length == r2.Length {
					_ = combined.Remove(r2.Length)
					op1 = takeOp(ops1, &i1)
					op2 = takeOp(ops2, &i2)
				} else {
					_ = combined.Remove(r1.Length)
					op2 = RemoveOp{Length: r2.Length - r1.Length}
					op1 = takeOp(ops1, &i1)
				}
				continue
			}
		}
		return nil, fmt.Errorf("This shouldn't happen: op1: %v, op2: %v", op1, op2)
	}
	return combined, nil
}

// TakeOp helper: returns ops[i] and advances i (nil at end).
func takeOp(ops []ScanOp, i *int) ScanOp {
	if *i >= len(ops) {
		return nil
	}
	op := ops[*i]
	*i++
	return op
}

// Transform takes two concurrent ops and produces A' and B' (Node: `transform`).
// This is the heart of OT.
func Transform(operation1, operation2 *TextOperation) (*TextOperation, *TextOperation, error) {
	if operation1.BaseLength != operation2.BaseLength {
		return nil, nil, NewUnprocessableError("Both operations have to have the same base length")
	}
	op1Prime := NewTextOperation()
	op2Prime := NewTextOperation()
	ops1 := operation1.Ops
	ops2 := operation2.Ops
	i1, i2 := 0, 0
	op1 := takeOp(ops1, &i1)
	op2 := takeOp(ops2, &i2)
	for {
		if op1 == nil && op2 == nil {
			break
		}
		if i1op, ok1 := op1.(InsertOp); ok1 {
			_ = op1Prime.Insert(i1op.Insertion, InsertBuilderOpts{Tracking: i1op.Tracking, CommentIds: i1op.CommentIds})
			_ = op2Prime.Retain(len(i1op.Insertion), RetainBuilderOpts{})
			op1 = takeOp(ops1, &i1)
			continue
		}
		if i2op, ok2 := op2.(InsertOp); ok2 {
			_ = op1Prime.Retain(len(i2op.Insertion), RetainBuilderOpts{})
			_ = op2Prime.Insert(i2op.Insertion, InsertBuilderOpts{Tracking: i2op.Tracking, CommentIds: i2op.CommentIds})
			op2 = takeOp(ops2, &i2)
			continue
		}
		if op1 == nil {
			return nil, nil, NewUnprocessableError("Cannot compose operations: first operation is too short.")
		}
		if op2 == nil {
			return nil, nil, NewUnprocessableError("Cannot compose operations: first operation is too long.")
		}
		if r1, ok1 := op1.(RetainOp); ok1 {
			if r2, ok2 := op2.(RetainOp); ok2 {
				var t1, t2 TrackingDirective
				if r1.Tracking != nil {
					t1 = r1.Tracking
				} else {
					t2 = r2.Tracking
				}
				var minl int
				if r1.Length > r2.Length {
					minl = r2.Length
					op1 = RetainOp{Length: r1.Length - r2.Length, Tracking: r1.Tracking}
					op2 = takeOp(ops2, &i2)
				} else if r1.Length == r2.Length {
					minl = r2.Length
					op1 = takeOp(ops1, &i1)
					op2 = takeOp(ops2, &i2)
				} else {
					minl = r1.Length
					op2 = RetainOp{Length: r2.Length - r1.Length, Tracking: r2.Tracking}
					op1 = takeOp(ops1, &i1)
				}
				_ = op1Prime.Retain(minl, RetainBuilderOpts{Tracking: t1})
				_ = op2Prime.Retain(minl, RetainBuilderOpts{Tracking: t2})
				continue
			}
		}
		if r1, ok1 := op1.(RemoveOp); ok1 {
			if r2, ok2 := op2.(RemoveOp); ok2 {
				if r1.Length > r2.Length {
					op1 = RemoveOp{Length: r1.Length - r2.Length}
					op2 = takeOp(ops2, &i2)
				} else if r1.Length == r2.Length {
					op1 = takeOp(ops1, &i1)
					op2 = takeOp(ops2, &i2)
				} else {
					op2 = RemoveOp{Length: r2.Length - r1.Length}
					op1 = takeOp(ops1, &i1)
				}
				continue
			}
		}
		if r1, ok1 := op1.(RemoveOp); ok1 {
			if r2, ok2 := op2.(RetainOp); ok2 {
				var minl int
				if r1.Length > r2.Length {
					minl = r2.Length
					op1 = RemoveOp{Length: r1.Length - r2.Length}
					op2 = takeOp(ops2, &i2)
				} else if r1.Length == r2.Length {
					minl = r2.Length
					op1 = takeOp(ops1, &i1)
					op2 = takeOp(ops2, &i2)
				} else {
					minl = r1.Length
					op2 = RetainOp{Length: r2.Length - r1.Length, Tracking: r2.Tracking}
					op1 = takeOp(ops1, &i1)
				}
				_ = op1Prime.Remove(minl)
				continue
			}
		}
		if r1, ok1 := op1.(RetainOp); ok1 {
			if r2, ok2 := op2.(RemoveOp); ok2 {
				var minl int
				if r1.Length > r2.Length {
					minl = r2.Length
					op1 = RetainOp{Length: r1.Length - r2.Length, Tracking: r1.Tracking}
					op2 = takeOp(ops2, &i2)
				} else if r1.Length == r2.Length {
					minl = r1.Length
					op1 = takeOp(ops1, &i1)
					op2 = takeOp(ops2, &i2)
				} else {
					minl = r1.Length
					op2 = RemoveOp{Length: r2.Length - r1.Length}
					op1 = takeOp(ops1, &i1)
				}
				_ = op2Prime.Remove(minl)
				continue
			}
		}
		return nil, nil, NewUnprocessableError("The two operations aren't compatible")
	}
	return op1Prime, op2Prime, nil
}

// getSimpleOp returns the single non-retain op if the op is "simple"
// (Node: `getSimpleOp`).
func getSimpleOp(operation *TextOperation) ScanOp {
	ops := operation.Ops
	switch len(ops) {
	case 1:
		return ops[0]
	case 2:
		if _, ok := ops[0].(RetainOp); ok {
			return ops[1]
		}
		if _, ok := ops[1].(RetainOp); ok {
			return ops[0]
		}
		return nil
	case 3:
		if _, ok0 := ops[0].(RetainOp); ok0 {
			if _, ok2 := ops[2].(RetainOp); ok2 {
				return ops[1]
			}
		}
	}
	return nil
}

// getStartIndex returns the leading retain length if present (Node: `getStartIndex`).
func getStartIndex(operation *TextOperation) int {
	if len(operation.Ops) > 0 {
		if r, ok := operation.Ops[0].(RetainOp); ok {
			return r.Length
		}
	}
	return 0
}

func isClearTracking(d TrackingDirective) bool {
	if d == nil {
		return false
	}
	if cp, ok := d.(ClearTrackingProps); ok {
		_ = cp
		return true
	}
	_, ok := d.(*ClearTrackingProps)
	return ok
}

// trackingCommentSegment is a segment of a delete with its own tracking +
// comment ids (Node: `calculateTrackingCommentSegments` element).
type trackingCommentSegment struct {
	Length     int
	CommentIds []string
	Tracking   TrackingDirective
}

// calculateTrackingCommentSegments builds the segments of a delete range
// (Node: `calculateTrackingCommentSegments`).
func calculateTrackingCommentSegments(cursor, length int, comments *CommentList, tracked *TrackedChangeList) []trackingCommentSegment {
	breakSet := map[int]bool{}
	opStart := cursor
	opEnd := cursor + length
	addBreak := func(b int) {
		if b >= opStart && b <= opEnd {
			breakSet[b] = true
		}
	}
	if comments != nil {
		for _, c := range comments.ToArray() {
			for _, r := range c.Ranges {
				addBreak(r.End())
				addBreak(r.Start())
			}
		}
	}
	if tracked != nil {
		for _, tc := range tracked.AsSorted() {
			addBreak(tc.Range.Start())
			addBreak(tc.Range.End())
		}
	}
	addBreak(opStart)
	addBreak(opEnd)

	breaks := make([]int, 0, len(breakSet))
	for b := range breakSet {
		breaks = append(breaks, b)
	}
	sort.Ints(breaks)

	out := []trackingCommentSegment{}
	for i := 1; i < len(breaks); i++ {
		start := breaks[i-1]
		end := breaks[i]
		if end == start {
			continue
		}
		curRange := NewRange(start, end-start)
		commentIds := []string{}
		if comments != nil {
			commentIds = comments.IDsCoveringRange(curRange)
		}
		seg := trackingCommentSegment{Length: curRange.Length}
		if len(commentIds) > 0 {
			seg.CommentIds = commentIds
		}
		if tracked != nil {
			if tp := tracked.PropsAtRange(curRange); tp.Type != "" {
				seg.Tracking = tp
			}
		}
		out = append(out, seg)
	}
	return out
}
