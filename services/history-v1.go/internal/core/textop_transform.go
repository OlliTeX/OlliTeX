package core

// This file ports the op-merge + builder + transform logic from the Node
// oracle:
//
//   libraries/overleaf-editor-core/lib/operation/scan_op.js   (canMergeWith, mergeWith)
//   libraries/overleaf-editor-core/lib/operation/text_operation.js (retain/insert/remove builders, static transform)
//   libraries/overleaf-editor-core/lib/comment.js   (Comment.applyTextOperation)
//
// The Go TextOp has no targetLength field: its equivalent is computed by
// BaseLength(). Compose (the other static TextOperation method) is
// intentionally NOT ported — it is only exercised by the level 1-4 commit
// undo paths that stay NotPorted. The synthetic-remainder pattern (a
// partial op replacing the loop cursor without advancing the slice index)
// mirrors the oracle exactly.

// sameCommentIDs reports whether two insert ops' commentIDs arrays match
// (same length, same IDs in order). Mirrors InsertOp.canMergeWith/equals.
// sameCommentIDs (Node InsertOp.canMergeWith commentIds check: length
// equality plus every id present on the other side).
func sameCommentIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, id := range a {
		seen := false
		for _, other := range b {
			if id == other {
				seen = true
				break
			}
		}
		if !seen {
			return false
		}
	}
	return true
}

// canMergeWith reports whether this op can be the first of a merge pair with
// other (the second). Port of ScanOp{Retain,Insert,Remove}.canMergeWith.
//
//	Retain+Retain: tracking-compatible (nil merges with nil only, live with
//	equal live).
//	Insert+Insert: tracking-compatible AND commentIDs-compatible (nil merges
//	with nil only; live must match both ways).
//	Remove+Remove: always. Kinds never mix.
func (o *ScanOp) canMergeWith(other *ScanOp) bool {
	if o.kindName() != other.kindName() {
		return false
	}
	switch o.kindName() {
	case "retain":
		if o.r.tracking != nil {
			return other.r.tracking != nil && o.r.tracking.CanMergeWith(other.r.tracking)
		}
		return other.r.tracking == nil
	case "insert":
		if o.r.tracking != nil {
			return other.r.tracking != nil && o.r.tracking.CanMergeWith(other.r.tracking)
		}
		if other.r.tracking != nil {
			return false
		}
		if o.r.commentIDs != nil {
			return sameCommentIDs(o.r.commentIDs, other.r.commentIDs)
		}
		return other.r.commentIDs == nil
	default: // remove
		return true
	}
}

// mergeWith mutates this op in place (via the shared *ScanOp pointer). Port
// of ScanOp{Retain,Insert,Remove}.mergeWith; caller must have checked
// canMergeWith (the builders do).
func (o *ScanOp) mergeWith(other *ScanOp) {
	switch o.kindName() {
	case "retain":
		o.r.length += other.r.length
		if o.r.tracking != nil && other.r.tracking != nil {
			o.r.tracking = o.r.tracking.MergeWith(other.r.tracking)
		}
	case "insert":
		o.r.insertion += other.r.insertion
		if o.r.tracking != nil && other.r.tracking != nil {
			o.r.tracking = o.r.tracking.MergeWith(other.r.tracking)
		}
		// commentIDs: "we already have the same" — canMergeWith guarantees.
	default: // remove
		o.r.length += other.r.length
	}
}

// kindName returns the scan op kind ("retain" | "insert" | "remove").
func (o *ScanOp) kindName() string {
	switch {
	case o.r.isRetain:
		return "retain"
	case o.r.isInsert:
		return "insert"
	default:
		return "remove"
	}
}

// buildRetain ports TextOperation.prototype.retain (builder). n is the UTF-16
// length to retain. Merges with the last op when compatible, else appends.
func (o *TextOp) buildRetain(n int, tracking *TrackingProps) {
	if n <= 0 {
		return
	}
	newOp := NewRetain(n, tracking)
	last := o.lastOp()
	if last != nil && last.canMergeWith(newOp) {
		last.mergeWith(newOp)
	} else {
		o.Ops = append(o.Ops, newOp)
	}
}

// buildInsert ports TextOperation.prototype.insert (builder). insertion is a
// Go (UTF-8) string.
func (o *TextOp) buildInsert(insertion string, tracking *TrackingProps, commentIDs []string) {
	if insertion == "" {
		return
	}
	newOp := NewInsert(insertion, tracking, commentIDs)
	last := o.lastOp()
	if last != nil && last.canMergeWith(newOp) {
		last.mergeWith(newOp)
	} else if last != nil && last.r.isRemove {
		// It doesn't matter when an operation is applied whether the operation
		// is remove-then-insert or insert-then-remove. Enforce insert first.
		var secondToLast *ScanOp
		if len(o.Ops) > 1 {
			secondToLast = o.Ops[len(o.Ops)-1-1]
		}
		if secondToLast != nil && secondToLast.canMergeWith(newOp) {
			secondToLast.mergeWith(newOp)
		} else {
			// Swap the new op in front of the remove op.
			o.Ops = append(o.Ops, last)
			o.Ops[len(o.Ops)-2] = newOp
		}
	} else {
		o.Ops = append(o.Ops, newOp)
	}
}

// buildRemove ports TextOperation.prototype.remove (builder). n is the
// positive UTF-16 length to remove.
func (o *TextOp) buildRemove(n int) {
	if n <= 0 {
		return
	}
	newOp := NewRemove(n)
	last := o.lastOp()
	if last != nil && last.canMergeWith(newOp) {
		last.mergeWith(newOp)
	} else {
		o.Ops = append(o.Ops, newOp)
	}
}

// lastOp is the builder's "lastOperation" (the op at the end of the op list).
func (o *TextOp) lastOp() *ScanOp {
	if len(o.Ops) == 0 {
		return nil
	}
	return o.Ops[len(o.Ops)-1]
}

// TextOpTransform ports TextOperation.transform: takes two operations A and B
// that happened concurrently and produces A' and B' such that
//
//	apply(apply(S, A), B') = apply(apply(S, B), A').
func TextOpTransform(a, b *TextOp) (*TextOp, *TextOp, error) {
	if a.BaseLength() != b.BaseLength() {
		return nil, nil, &UnprocessableError{
			Msg: "Both operations have to have the same base length",
		}
	}

	operation1prime := &TextOp{}
	operation2prime := &TextOp{}
	ops1 := a.Ops
	ops2 := b.Ops
	i1, i2 := 0, 0
	var op1, op2 *ScanOp
	if i1 < len(ops1) {
		op1 = ops1[i1]
		i1++
	}
	if i2 < len(ops2) {
		op2 = ops2[i2]
		i2++
	}
	for {
		// At every iteration of the loop, the imaginary cursor that both
		// operation1 and operation2 have that operates on the input string
		// must be at the same position in the input string.
		if op1 == nil && op2 == nil {
			break
		}

		// Next two cases: one or both ops are insert ops => insert the string
		// in the corresponding prime operation, skip it in the other. If both
		// op1 and op2 are insert ops, prefer op1.
		if op1 != nil && op1.r.isInsert {
			operation1prime.buildInsert(op1.r.insertion, op1.r.tracking, op1.r.commentIDs)
			operation2prime.buildRetain(utf16Length(op1.r.insertion), nil)
			op1 = nextOp1(ops1, &i1)
			continue
		}
		if op2 != nil && op2.r.isInsert {
			operation1prime.buildRetain(utf16Length(op2.r.insertion), nil)
			operation2prime.buildInsert(op2.r.insertion, op2.r.tracking, op2.r.commentIDs)
			op2 = nextOp1(ops2, &i2)
			continue
		}

		if op1 == nil {
			return nil, nil, &UnprocessableError{
				Msg: "Cannot compose operations: first operation is too short.",
			}
		}
		if op2 == nil {
			return nil, nil, &UnprocessableError{
				Msg: "Cannot compose operations: first operation is too long.",
			}
		}

		var minl int
		switch {
		case op1.r.isRetain && op2.r.isRetain:
			// Simple case: retain/retain. If both have tracking info, we use
			// the one from op1.
			var prime1Tracking, prime2Tracking *TrackingProps
			if op1.r.tracking != nil {
				prime1Tracking = op1.r.tracking
			} else {
				prime2Tracking = op2.r.tracking
			}
			if op1.r.length > op2.r.length {
				minl = op2.r.length
				op1 = NewRetain(op1.r.length-op2.r.length, op1.r.tracking)
				op2 = nextOp1(ops2, &i2)
			} else if op1.r.length == op2.r.length {
				minl = op2.r.length
				op1 = nextOp1(ops1, &i1)
				op2 = nextOp1(ops2, &i2)
			} else {
				minl = op1.r.length
				op2 = NewRetain(op2.r.length-op1.r.length, op2.r.tracking)
				op1 = nextOp1(ops1, &i1)
			}
			operation1prime.buildRetain(minl, prime1Tracking)
			operation2prime.buildRetain(minl, prime2Tracking)
		case op1.r.isRemove && op2.r.isRemove:
			// Both operations remove the same string at the same position. No
			// prime produced; skip over the removes, handling the case one
			// removes more than the other.
			if op1.r.length > op2.r.length {
				op1 = NewRemove(op1.r.length - op2.r.length)
				op2 = nextOp1(ops2, &i2)
			} else if op1.r.length == op2.r.length {
				op1 = nextOp1(ops1, &i1)
				op2 = nextOp1(ops2, &i2)
			} else {
				op2 = NewRemove(op2.r.length - op1.r.length)
				op1 = nextOp1(ops1, &i1)
			}
		case op1.r.isRemove && op2.r.isRetain:
			if op1.r.length > op2.r.length {
				minl = op2.r.length
				op1 = NewRemove(op1.r.length - op2.r.length)
				op2 = nextOp1(ops2, &i2)
			} else if op1.r.length == op2.r.length {
				minl = op2.r.length
				op1 = nextOp1(ops1, &i1)
				op2 = nextOp1(ops2, &i2)
			} else {
				minl = op1.r.length
				op2 = NewRetain(op2.r.length-op1.r.length, op2.r.tracking)
				op1 = nextOp1(ops1, &i1)
			}
			operation1prime.buildRemove(minl)
		case op1.r.isRetain && op2.r.isRemove:
			if op1.r.length > op2.r.length {
				minl = op2.r.length
				op1 = NewRetain(op1.r.length-op2.r.length, op1.r.tracking)
				op2 = nextOp1(ops2, &i2)
			} else if op1.r.length == op2.r.length {
				minl = op1.r.length
				op1 = nextOp1(ops1, &i1)
				op2 = nextOp1(ops2, &i2)
			} else {
				minl = op1.r.length
				op2 = NewRemove(op2.r.length - op1.r.length)
				op1 = nextOp1(ops1, &i1)
			}
			operation2prime.buildRemove(minl)
		default:
			return nil, nil, &UnprocessableError{
				Msg: "The two operations aren't compatible",
			}
		}
	}

	return operation1prime, operation2prime, nil
}

// nextOp1 ports the oracle's `ops[i++]` advance (nil when exhausted).
func nextOp1(ops []*ScanOp, i *int) *ScanOp {
	if *i < len(ops) {
		op := ops[*i]
		*i++
		return op
	}
	return nil
}

// ApplyTextOperation ports Comment.applyTextOperation: applies the text op to
// this comment as if the op were applied to the document starting at cursor
// 0, extending ranges the op flags with this comment's ID. cursor and insert
// lengths are UTF-16 code units.
func (c *Comment) ApplyTextOperation(to *TextOp, commentID string) *Comment {
	comment := c
	cursor := 0
	for _, op := range to.Ops {
		switch {
		case op.r.isRetain:
			cursor += op.r.length
		case op.r.isInsert:
			extend := false
			for _, id := range op.r.commentIDs {
				if id == commentID {
					extend = true
				}
			}
			l := utf16Length(op.r.insertion)
			comment = comment.applyInsert(cursor, l, extend)
			cursor += l
		case op.r.isRemove:
			comment = comment.applyDelete(Range{Pos: cursor, Length: op.r.length})
		}
	}
	return comment
}
