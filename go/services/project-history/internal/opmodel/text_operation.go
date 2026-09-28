package opmodel

import (
	"fmt"
	"sort"
	"strings"
)

// MaxStringLength mirrors vendor static MAX_STRING_LENGTH = 3*1024^2
// (OT/history ceiling; upstream inbound edits are capped lower).
const MaxStringLength = 3 * 1024 * 1024

// TextOperation — vendor `class TextOperation extends EditOperation`: a list
// of scan ops plus base/target lengths and an optional content hash.
//
// baseLength = length of the input string the op can be applied to
// (sum of all retains + all removes). targetLength = length of the output
// (sum of all retains + all inserts). Lengths are UTF-16 code units.
type TextOperation struct {
	Ops          []ScanOp
	BaseLength   int
	TargetLength int
	// ContentHash — vendor `contentHash` (string | null); nil = absent.
	ContentHash *string
}

// NewTextOperation — vendor `new TextOperation()`.
func NewTextOperation() *TextOperation { return &TextOperation{} }

// mergeScanOps merges two scan ops by value, assuming a.CanMergeWith(b).
// (Go value-receiver MergeWith would mutate a copy, so callers must store
// the returned value back; this mirrors vendor `a.mergeWith(b)` in spirit.)
func mergeScanOps(a, b ScanOp) ScanOp {
	switch x := a.(type) {
	case RetainOp:
		y, ok := b.(RetainOp)
		if !ok {
			panic("Cannot merge with incompatible operation")
		}
		r := RetainOp{Length: x.Length + y.Length, Tracking: x.Tracking}
		if x.Tracking != nil && y.Tracking != nil {
			r.Tracking = x.Tracking.MergeWith(y.Tracking)
		}
		return r
	case InsertOp:
		y, ok := b.(InsertOp)
		if !ok {
			panic("Cannot merge with incompatible operation")
		}
		// commentIds already equal per canMergeWith; keep this one (vendor).
		i := InsertOp{Insertion: x.Insertion + y.Insertion, Tracking: x.Tracking, CommentIDs: x.CommentIDs}
		if x.Tracking != nil && y.Tracking != nil {
			i.Tracking = x.Tracking.MergeWith(y.Tracking)
		}
		return i
	case RemoveOp:
		y, ok := b.(RemoveOp)
		if !ok {
			panic("Cannot merge with incompatible operation")
		}
		return RemoveOp{Length: x.Length + y.Length}
	}
	panic("Cannot merge with incompatible operation")
}

func lastOp(ops []ScanOp) ScanOp {
	if len(ops) == 0 {
		return nil
	}
	return ops[len(ops)-1]
}

// --- builders (vendor: retain/insert/remove, mutating, return self) ---

// Retain — vendor `retain(n, {tracking})`: skip n units. n==0 is a no-op.
func (o *TextOperation) Retain(n int, tracking *Tracking) *TextOperation {
	if n == 0 {
		return o
	}
	if n < 0 {
		panic("retain expects an integer or a retain object")
	}
	newOp := RetainOp{Length: n, Tracking: tracking}
	o.BaseLength += n
	o.TargetLength += n
	if last := lastOp(o.Ops); last != nil && last.CanMergeWith(newOp) {
		o.Ops[len(o.Ops)-1] = mergeScanOps(last, newOp)
		return o
	}
	o.Ops = append(o.Ops, newOp)
	return o
}

// Insert — vendor `insert(insertValue, {tracking, commentIds})`: insert a BMP
// string at the cursor. Panics on non-BMP insertion (vendor throws); "" is a
// no-op. Enforces the vendor invariant that an insert always precedes an
// adjacent remove.
func (o *TextOperation) Insert(insertion string, tracking *Tracking, commentIDs []string) *TextOperation {
	if containsNonBmpChars(insertion) {
		panic(NewInvalidInsertionError(insertion, nil))
	}
	if insertion == "" {
		return o
	}
	newOp := InsertOp{Insertion: insertion, Tracking: tracking, CommentIDs: commentIDs}
	o.TargetLength += Units(insertion)
	if len(o.Ops) == 0 {
		o.Ops = append(o.Ops, newOp)
		return o
	}
	last := o.Ops[len(o.Ops)-1]
	if last.CanMergeWith(newOp) {
		o.Ops[len(o.Ops)-1] = mergeScanOps(last, newOp)
		return o
	}
	if isRemoveOp(last) {
		// Enforce insert-before-removed-range ordering (vendor quirk).
		if len(o.Ops) >= 2 {
			secondLast := o.Ops[len(o.Ops)-2]
			if secondLast.CanMergeWith(newOp) {
				o.Ops[len(o.Ops)-2] = mergeScanOps(secondLast, newOp)
				return o
			}
		}
		// [..., secondLast, remove] -> [..., secondLast, newOp, remove]
		o.Ops = append(o.Ops, last)
		o.Ops[len(o.Ops)-2] = newOp
		return o
	}
	o.Ops = append(o.Ops, newOp)
	return o
}

// Remove — vendor `remove(n)`: remove the next n units. n==0 is a no-op; n is
// taken as a length (vendor negates positive n and consumes a RemoveOp with
// the positive internal length). REMOVES INCREASE BaseLength (vendor quirk:
// `baseLength -= n` with the wire-negative n).
func (o *TextOperation) Remove(n int) *TextOperation {
	if n == 0 {
		return o
	}
	if n > 0 {
		n = -n
	}
	length := -n
	newOp := RemoveOp{Length: length}
	o.BaseLength += length
	if last := lastOp(o.Ops); last != nil && last.CanMergeWith(newOp) {
		o.Ops[len(o.Ops)-1] = mergeScanOps(last, newOp)
		return o
	}
	o.Ops = append(o.Ops, newOp)
	return o
}

// --- predicates / equality / rendering ---

// IsNoop — vendor `isNoop()`: empty, or a single untracked retain.
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

// Equals — vendor `equals(other)`.
func (o *TextOperation) Equals(other *TextOperation) bool {
	if o.BaseLength != other.BaseLength || o.TargetLength != other.TargetLength {
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

// String — vendor `toString`: "retain 2, insert 'lorem', remove 5, ...".
func (o *TextOperation) String() string {
	parts := make([]string, len(o.Ops))
	for i, s := range o.Ops {
		parts[i] = s.String()
	}
	return strings.Join(parts, ", ")
}

// ToRaw — vendor `toJSON()`: {textOperation: [...], contentHash?}.
func (o *TextOperation) ToRaw() map[string]any {
	ops := make([]any, 0, len(o.Ops))
	for _, s := range o.Ops {
		ops = append(ops, s.ToWire())
	}
	raw := map[string]any{"textOperation": ops}
	if o.ContentHash != nil {
		raw["contentHash"] = *o.ContentHash
	}
	return raw
}

// --- fromJSON ---

// TextOperationFromJSON — vendor `TextOperation.fromJSON({textOperation,
// contentHash})`.
func TextOperationFromJSON(raw map[string]any) (*TextOperation, error) {
	ops, ok := raw["textOperation"].([]any)
	if !ok {
		return nil, NewUnprocessableError("TextOperation.fromJSON: missing textOperation", nil)
	}
	o := NewTextOperation()
	for _, rop := range ops {
		if IsRetainWire(rop) {
			ret, err := RetainOpFromJSON(rop)
			if err != nil {
				return nil, err
			}
			o.Retain(ret.Length, ret.Tracking)
		} else if IsInsertWire(rop) {
			ins, err := InsertOpFromJSON(rop)
			if err != nil {
				return nil, err
			}
			o.Insert(ins.Insertion, ins.Tracking, ins.CommentIDs)
		} else if IsRemoveWire(rop) {
			rm, err := RemoveOpFromJSON(rop)
			if err != nil {
				return nil, err
			}
			o.Remove(rm.Length)
		} else {
			return nil, NewUnprocessableError("unknown operation: "+wireString(rop), nil)
		}
	}
	if ch, has := raw["contentHash"]; has && ch != nil {
		if s, ok := ch.(string); ok {
			o.ContentHash = &s
		}
	}
	return o, nil
}

// --- apply / applyToLength ---

// Apply — vendor `apply(file)`: scan the ops over the file content, update
// comments live, replay tracked changes, replace file.content.
func (o *TextOperation) Apply(file *StringFileData) error {
	str := file.GetContent()
	if Units(str) != o.BaseLength {
		return NewApplyError("The operation's base length must be equal to the string's length.", o, str)
	}
	inputCursor := 0
	var result strings.Builder
	for _, op := range o.Ops {
		switch s := op.(type) {
		case RetainOp:
			if inputCursor+s.Length > Units(str) {
				return NewApplyError("Operation can't retain more chars than are left in the string.", s.ToWire(), str)
			}
			result.WriteString(unitSlice(str, inputCursor, s.Length))
			inputCursor += s.Length
		case InsertOp:
			file.Comments.ApplyInsert(Range{Pos: Units(result.String()), Length: Units(s.Insertion)}, s.CommentIDs)
			result.WriteString(s.Insertion)
		case RemoveOp:
			file.Comments.ApplyDelete(Range{Pos: Units(result.String()), Length: s.Length})
			inputCursor += s.Length
		default:
			return NewUnprocessableError("Unknown ScanOp type during apply", nil)
		}
	}
	if inputCursor != Units(str) {
		return NewApplyError("The operation didn't operate on the whole string.", o, str)
	}
	if resultLen := Units(result.String()); resultLen > MaxStringLength {
		return NewTooLongError(o, resultLen)
	}
	if err := file.TrackedChanges.ApplyTextOperation(o.Ops); err != nil {
		return err
	}
	file.Content = result.String()
	return nil
}

// ApplyToLength — vendor `applyToLength(length)`.
func (o *TextOperation) ApplyToLength(length int) (int, error) {
	if length != o.BaseLength {
		return 0, NewApplyError("The operation's base length must be equal to the string's length.", o, length)
	}
	ctx := LengthApplyContext{InputLength: length}
	for _, op := range o.Ops {
		if err := op.ApplyToLength(&ctx); err != nil {
			return 0, err
		}
	}
	if ctx.InputCursor != length {
		return 0, NewApplyError("The operation didn't operate on the whole string.", o, length)
	}
	if ctx.Length > MaxStringLength {
		return 0, NewTooLongError(o, ctx.Length)
	}
	return ctx.Length, nil
}

// Invert — vendor `invert(previousState)`: the inverse (undo) operation
// relative to the previous file state. Remove segments are re-inserted with
// their overlapping comment/tracked-change state restored.
func (o *TextOperation) Invert(previousState *StringFileData) EditOperation {
	str := previousState.GetContent()
	inverse := NewTextOperation()
	strIndex := 0
	for _, op := range o.Ops {
		switch s := op.(type) {
		case RetainOp:
			if s.Tracking != nil {
				target := s.Length + strIndex
				previousChanges := previousState.TrackedChanges.IntersectRange(
					Range{Pos: strIndex, Length: s.Length})
				for _, change := range previousChanges {
					if strIndex < change.Range.Start() {
						inverse.Retain(change.Range.Start()-strIndex, Clear())
						strIndex = change.Range.Start()
					}
					inverse.Retain(change.Range.Length, &change.Tracking)
					strIndex += change.Range.Length
				}
				if strIndex < target {
					inverse.Retain(target-strIndex, Clear())
					strIndex = target
				}
			} else {
				inverse.Retain(s.Length, nil)
				strIndex += s.Length
			}
		case InsertOp:
			inverse.Remove(Units(s.Insertion))
		case RemoveOp:
			segments := calculateTrackingCommentSegments(
				strIndex, s.Length, previousState.Comments, previousState.TrackedChanges)
			for _, segment := range segments {
				inverse.Insert(unitSlice(str, strIndex, segment.Length), segment.Tracking, segment.CommentIDs)
				strIndex += segment.Length
			}
		default:
			panic("unknown scanop during inversion")
		}
	}
	return inverse
}

// --- compose ---

func composeShouldntHappen(op1, op2 ScanOp) *UnprocessableError {
	return NewUnprocessableError("This shouldn't happen: op1: "+wireString(op1.ToWire())+", op2: "+wireString(op2.ToWire()), nil)
}

// Compose — vendor `compose(operation2)`: merge two consecutive operations
// into one such that apply(apply(S, A), B) == apply(S, compose(A, B)).
func (o *TextOperation) Compose(other EditOperation) (EditOperation, error) {
	op2o, ok := other.(*TextOperation)
	if !ok {
		return nil, NewUnprocessableError("Trying to compose TextOperation with "+fmt.Sprintf("%T", other), nil)
	}
	if o.TargetLength != op2o.BaseLength {
		return nil, NewUnprocessableError("The base length of the second operation has to be the target length of the first operation", nil)
	}
	operation := NewTextOperation()
	ops1, ops2 := o.Ops, op2o.Ops
	i1, i2 := 0, 0
	op1 := nextOp(ops1, &i1)
	op2 := nextOp(ops2, &i2)
	for {
		if op1 == nil && op2 == nil {
			break
		}
		if r, ok := op1.(RemoveOp); ok {
			operation.Remove(r.Length)
			op1 = nextOp(ops1, &i1)
			continue
		}
		if in, ok := op2.(InsertOp); ok {
			operation.Insert(in.Insertion, in.Tracking, in.CommentIDs)
			op2 = nextOp(ops2, &i2)
			continue
		}
		if op1 == nil {
			return nil, NewUnprocessableError("Cannot compose operations: first operation is too short.", nil)
		}
		if op2 == nil {
			return nil, NewUnprocessableError("Cannot compose operations: first operation is too long.", nil)
		}
		switch s1 := op1.(type) {
		case RetainOp:
			switch s2 := op2.(type) {
			case RetainOp:
				// If both have tracking, use the latter (op2) one.
				tracking := s1.Tracking
				if s2.Tracking != nil {
					tracking = s2.Tracking
				}
				if s1.Length > s2.Length {
					operation.Retain(s2.Length, tracking)
					op1 = RetainOp{Length: s1.Length - s2.Length, Tracking: s1.Tracking}
					op2 = nextOp(ops2, &i2)
				} else if s1.Length == s2.Length {
					operation.Retain(s1.Length, tracking)
					op1 = nextOp(ops1, &i1)
					op2 = nextOp(ops2, &i2)
				} else {
					operation.Retain(s1.Length, tracking)
					op2 = RetainOp{Length: s2.Length - s1.Length, Tracking: s2.Tracking}
					op1 = nextOp(ops1, &i1)
				}
			case RemoveOp:
				if s1.Length > s2.Length {
					operation.Remove(s2.Length)
					op1 = RetainOp{Length: s1.Length - s2.Length, Tracking: s1.Tracking}
					op2 = nextOp(ops2, &i2)
				} else if s1.Length == s2.Length {
					operation.Remove(s2.Length)
					op1 = nextOp(ops1, &i1)
					op2 = nextOp(ops2, &i2)
				} else {
					operation.Remove(s1.Length)
					op2 = RemoveOp{Length: s2.Length - s1.Length}
					op1 = nextOp(ops1, &i1)
				}
			default:
				return nil, composeShouldntHappen(op1, op2)
			}
		case InsertOp:
			switch s2 := op2.(type) {
			case RemoveOp:
				il := Units(s1.Insertion)
				if il > s2.Length {
					_, rest := unitCut(s1.Insertion, s2.Length)
					op1 = InsertOp{Insertion: rest, Tracking: s1.Tracking, CommentIDs: s1.CommentIDs}
					op2 = nextOp(ops2, &i2)
				} else if il == s2.Length {
					op1 = nextOp(ops1, &i1)
					op2 = nextOp(ops2, &i2)
				} else {
					op1 = nextOp(ops1, &i1)
					op2 = RemoveOp{Length: s2.Length - il}
				}
			case RetainOp:
				var tracking *Tracking
				if s2.Tracking != nil {
					// Prefer the second operation's tracking unless it's a
					// clear (which cancels the first op's tracking).
					if s2.Tracking.Type != "none" {
						tracking = s2.Tracking
					}
				} else {
					tracking = s1.Tracking
				}
				il := Units(s1.Insertion)
				if il > s2.Length {
					head, rest := unitCut(s1.Insertion, s2.Length)
					operation.Insert(head, tracking, s1.CommentIDs)
					op1 = InsertOp{Insertion: rest, Tracking: s1.Tracking, CommentIDs: s1.CommentIDs}
					op2 = nextOp(ops2, &i2)
				} else if il == s2.Length {
					operation.Insert(s1.Insertion, tracking, s1.CommentIDs)
					op1 = nextOp(ops1, &i1)
					op2 = nextOp(ops2, &i2)
				} else {
					operation.Insert(s1.Insertion, tracking, s1.CommentIDs)
					op1 = nextOp(ops1, &i1)
					op2 = RetainOp{Length: s2.Length - il, Tracking: s2.Tracking}
				}
			default:
				return nil, composeShouldntHappen(op1, op2)
			}
		default:
			return nil, composeShouldntHappen(op1, op2)
		}
	}
	return operation, nil
}

// --- undo / composition predicates ---

func getStartIndex(operation *TextOperation) int {
	if len(operation.Ops) > 0 {
		if r, ok := operation.Ops[0].(RetainOp); ok {
			return r.Length
		}
	}
	return 0
}

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

// CanBeComposedWith — vendor `canBeComposedWith`: targets chain.
func (o *TextOperation) CanBeComposedWith(other EditOperation) bool {
	otherOp, ok := other.(*TextOperation)
	if !ok {
		return false
	}
	return o.TargetLength == otherOp.BaseLength
}

// CanBeComposedWithForUndo — vendor `canBeComposedWithForUndo`: consecutive
// inserts at the boundary, or deletes at the same position.
func (o *TextOperation) CanBeComposedWithForUndo(other EditOperation) bool {
	otherOp, ok := other.(*TextOperation)
	if !ok {
		return false
	}
	if o.IsNoop() || otherOp.IsNoop() {
		return true
	}
	simpleA := getSimpleOp(o)
	simpleB := getSimpleOp(otherOp)
	if simpleA == nil || simpleB == nil {
		return false
	}
	startA := getStartIndex(o)
	startB := getStartIndex(otherOp)
	if ia, ok := simpleA.(InsertOp); ok {
		if _, ok := simpleB.(InsertOp); ok {
			return startA+Units(ia.Insertion) == startB
		}
	}
	if _, ok := simpleA.(RemoveOp); ok {
		if rb, ok := simpleB.(RemoveOp); ok {
			return startB+rb.Length == startA || startA == startB
		}
	}
	return false
}

// --- transform ---

func nextOp(ops []ScanOp, i *int) ScanOp {
	if *i < len(ops) {
		op := ops[*i]
		*i++
		return op
	}
	return nil
}

// Transform — vendor static `TextOperation.transform(operation1, operation2)`:
// given two concurrent ops A and B over the same base, produce A' and B' such
// that apply(apply(S, A), B') == apply(apply(S, B), A'). Insert is preferred
// from op1; retain/retain uses op1's tracking.
func Transform(operation1, operation2 *TextOperation) (*TextOperation, *TextOperation, error) {
	if operation1.BaseLength != operation2.BaseLength {
		return nil, nil, NewUnprocessableError("Both operations have to have the same base length", nil)
	}
	op1prime := NewTextOperation()
	op2prime := NewTextOperation()
	ops1, ops2 := operation1.Ops, operation2.Ops
	i1, i2 := 0, 0
	op1 := nextOp(ops1, &i1)
	op2 := nextOp(ops2, &i2)
	for {
		if op1 == nil && op2 == nil {
			break
		}
		if in, ok := op1.(InsertOp); ok {
			op1prime.Insert(in.Insertion, in.Tracking, in.CommentIDs)
			op2prime.Retain(Units(in.Insertion), nil)
			op1 = nextOp(ops1, &i1)
			continue
		}
		if in, ok := op2.(InsertOp); ok {
			op1prime.Retain(Units(in.Insertion), nil)
			op2prime.Insert(in.Insertion, in.Tracking, in.CommentIDs)
			op2 = nextOp(ops2, &i2)
			continue
		}
		if op1 == nil {
			return nil, nil, NewUnprocessableError("Cannot compose operations: first operation is too short.", nil)
		}
		if op2 == nil {
			return nil, nil, NewUnprocessableError("Cannot compose operations: first operation is too long.", nil)
		}
		switch s1 := op1.(type) {
		case RetainOp:
			switch s2 := op2.(type) {
			case RetainOp:
				// retain/retain — op1's tracking wins.
				t1, t2 := s1.Tracking, s2.Tracking
				if s1.Tracking != nil {
					t2 = nil
				}
				var minl int
				if s1.Length > s2.Length {
					minl = s2.Length
					op1 = RetainOp{Length: s1.Length - s2.Length, Tracking: s1.Tracking}
					op2 = nextOp(ops2, &i2)
				} else if s1.Length == s2.Length {
					minl = s2.Length
					op1 = nextOp(ops1, &i1)
					op2 = nextOp(ops2, &i2)
				} else {
					minl = s1.Length
					op2 = RetainOp{Length: s2.Length - s1.Length, Tracking: s2.Tracking}
					op1 = nextOp(ops1, &i1)
				}
				op1prime.Retain(minl, t1)
				op2prime.Retain(minl, t2)
			case RemoveOp:
				// retain/remove
				var minl int
				if s1.Length > s2.Length {
					minl = s2.Length
					op1 = RetainOp{Length: s1.Length - s2.Length, Tracking: s1.Tracking}
					op2 = nextOp(ops2, &i2)
				} else if s1.Length == s2.Length {
					minl = s1.Length
					op1 = nextOp(ops1, &i1)
					op2 = nextOp(ops2, &i2)
				} else {
					minl = s1.Length
					op2 = RemoveOp{Length: s2.Length - s1.Length}
					op1 = nextOp(ops1, &i1)
				}
				op2prime.Remove(minl)
			default:
				return nil, nil, NewUnprocessableError("The two operations aren't compatible", nil)
			}
		case RemoveOp:
			switch s2 := op2.(type) {
			case RemoveOp:
				// remove/remove — skip, carry the longer removal.
				if s1.Length > s2.Length {
					op1 = RemoveOp{Length: s1.Length - s2.Length}
					op2 = nextOp(ops2, &i2)
				} else if s1.Length == s2.Length {
					op1 = nextOp(ops1, &i1)
					op2 = nextOp(ops2, &i2)
				} else {
					op1 = nextOp(ops1, &i1)
					op2 = RemoveOp{Length: s2.Length - s1.Length}
				}
			case RetainOp:
				// remove/retain
				var minl int
				if s1.Length > s2.Length {
					minl = s2.Length
					op1 = RemoveOp{Length: s1.Length - s2.Length}
					op2 = nextOp(ops2, &i2)
				} else if s1.Length == s2.Length {
					minl = s2.Length
					op1 = nextOp(ops1, &i1)
					op2 = nextOp(ops2, &i2)
				} else {
					minl = s1.Length
					op2 = RetainOp{Length: s2.Length - s1.Length, Tracking: s2.Tracking}
					op1 = nextOp(ops1, &i1)
				}
				op1prime.Remove(minl)
			default:
				return nil, nil, NewUnprocessableError("The two operations aren't compatible", nil)
			}
		default:
			return nil, nil, NewUnprocessableError("The two operations aren't compatible", nil)
		}
	}
	return op1prime, op2prime, nil
}

func isRemoveOp(op ScanOp) bool {
	_, ok := op.(RemoveOp)
	return ok
}

// invertSegment — a segment of calculateTrackingCommentSegments (vendor
// `{length, commentIds?, tracking}`).
type invertSegment struct {
	Length     int
	CommentIDs []string // nil when none (vendor: undefined)
	Tracking   *Tracking
}

// calculateTrackingCommentSegments — vendor calculateTrackingCommentSegments:
// the segments of [cursor, cursor+length) cut by every comment range and
// tracked-change range boundary; each carries the comments covering it and
// the tracking props over it.
func calculateTrackingCommentSegments(cursor, length int, commentsList *CommentList, trackedChangeList *TrackedChangeList) []invertSegment {
	breaks := map[int]struct{}{}
	opEnd := cursor + length
	addBreak := func(b int) {
		if b < cursor || b > opEnd {
			return
		}
		breaks[b] = struct{}{}
	}
	for _, c := range commentsList.Array() {
		for _, rng := range c.Ranges {
			addBreak(rng.End())
			addBreak(rng.Start())
		}
	}
	for _, tc := range trackedChangeList.AsSorted() {
		addBreak(tc.Range.Start())
		addBreak(tc.Range.End())
	}
	addBreak(cursor)
	addBreak(opEnd)
	sorted := make([]int, 0, len(breaks))
	for b := range breaks {
		sorted = append(sorted, b)
	}
	sort.Ints(sorted)
	segments := []invertSegment{}
	for i := 1; i < len(sorted); i++ {
		start, end := sorted[i-1], sorted[i]
		currentRange := Range{Pos: start, Length: end - start}
		ids := commentsList.IDsCoveringRange(currentRange)
		if len(ids) == 0 {
			ids = nil
		}
		segments = append(segments, invertSegment{
			Length:     end - start,
			CommentIDs: ids,
			Tracking:   trackedChangeList.PropsAtRange(currentRange),
		})
	}
	return segments
}

// unitSlice — s from UTF-16 unit `start` for `length` units (vendor
// str.slice(inputCursor, inputCursor + length)).
func unitSlice(s string, start, length int) string {
	units := utf16Decode(s)
	if start >= len(units) {
		return ""
	}
	end := start + length
	if end > len(units) {
		end = len(units)
	}
	return utf16Encode(units[start:end])
}

// UnitSlice — vendored unit-space s.slice(start, end) (JS slice semantics:
// start > len => "", end clamped, start > end => ""). B10 chunktranslator's
// TextUpdateBuilder needs it over raw content strings on UTF-16 code units.
func UnitSlice(s string, start, end int) string {
	units := utf16Decode(s)
	if start >= len(units) {
		return ""
	}
	if end > len(units) {
		end = len(units)
	}
	if start > end {
		return ""
	}
	return utf16Encode(units[start:end])
}

// unitCut — split s at UTF-16 unit position n: (prefix, suffix) (vendor
// insertion.slice(0, n) / insertion.slice(n) on code units).
func unitCut(s string, n int) (string, string) {
	units := utf16Decode(s)
	if n <= 0 {
		return "", s
	}
	if n >= len(units) {
		return s, ""
	}
	return utf16Encode(units[:n]), utf16Encode(units[n:])
}
