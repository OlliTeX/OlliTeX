package opmodel

// Comment ports vendor lib/comment.js (Comment class).
//
// A comment is an id + a list of non-overlapping Range objects (+ resolved
// flag). applyInsert/applyDelete return a NEW Comment (vendor constructs a
// fresh object each call; Go mirrors with value copy).

// Comment — vendor `class Comment`.
type Comment struct {
	ID       string
	Ranges   []Range
	Resolved bool
}

// NewComment — vendor constructor: `this.ranges = this.mergeRanges(ranges)`.
// Sorts the ranges, drops empty ones, merges touching/overlapping adjacent
// ranges and errors per vendor: "Ranges cannot overlap".
func NewComment(id string, ranges []Range, resolved bool) (*Comment, error) {
	merged, err := mergeCommentRanges(ranges)
	if err != nil {
		return nil, err
	}
	return &Comment{ID: id, Ranges: merged, Resolved: resolved}, nil
}

// normalizeRanges — re-applies the vendor constructor's range pipeline
// (sort, drop empty ranges, merge touching/overlapping) after
// applyInsert/applyDelete. Vendor rebuilds the comment via
// `new Comment(this.id, newRanges, this.resolved)`, which runs
// mergeRanges (throws 'Ranges cannot overlap') even on the apply paths;
// the apply paths cannot produce overlap, so the panic is unreachable but
// mirrored 1:1.
func normalizeRanges(ranges []Range) []Range {
	merged, err := mergeCommentRanges(ranges)
	if err != nil {
		panic(err)
	}
	return merged
}

// NewCommentUnsafe — the internal constructor used by applyInsert/applyDelete
// where the new ranges are derived from already-merged, valid ranges.
func NewCommentUnsafe(id string, ranges []Range, resolved bool) *Comment {
	return &Comment{ID: id, Ranges: ranges, Resolved: resolved}
}

// mergeCommentRanges — vendor `mergeRanges`: sort by start, skip empty, error on
// overlap with the last merged range, merge where canMerge.
func mergeCommentRanges(ranges []Range) ([]Range, error) {
	sorted := append([]Range(nil), ranges...)
	// Insertion sort — vendor [...ranges].sort((a,b) => a.start - b.sort,
	// which the Go stdlib sort.Slice mirrors.
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].Start() < sorted[j-1].Start(); j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	merged := []Range{}
	for _, rng := range sorted {
		if rng.IsEmpty() {
			continue // vendor's 2nd `isEmpty` check follows this one (dead; unreachable)
		}
		if len(merged) > 0 {
			// vendor: `const lastMerged = mergedRanges[mergedRanges.length - 1];
			// if (lastMerged?.overlaps(range))` — the `?.` guards the empty-list case
			last := merged[len(merged)-1]
			if last.Overlaps(rng) {
				return nil, NewRangeError("Ranges cannot overlap", nil)
			}
			if last.CanMerge(rng) {
				m, err := last.Merge(rng)
				if err != nil {
					return nil, err
				}
				merged[len(merged)-1] = m
				continue
			}
		}
		merged = append(merged, rng)
	}
	return merged, nil
}

// ApplyInsert — vendor `applyInsert(cursor, length, extendComment)`: shifts,
// extends or splits every range against an insert at `cursor` of `length`
// units. Returns a new Comment.
func (c Comment) ApplyInsert(cursor, length int, extendComment bool) Comment {
	existingRangeExtended := false
	newRanges := []Range{}
	for _, commentRange := range c.Ranges {
		switch {
		case cursor == commentRange.End():
			// insert right after the comment
			if extendComment {
				newRanges = append(newRanges, commentRange.ExtendBy(length))
				existingRangeExtended = true
			} else {
				newRanges = append(newRanges, commentRange)
			}
		case cursor == commentRange.Start():
			// insert at the start of the comment
			if extendComment {
				newRanges = append(newRanges, commentRange.ExtendBy(length))
				existingRangeExtended = true
			} else {
				newRanges = append(newRanges, commentRange.MoveBy(length))
			}
		case commentRange.StartIsAfter(cursor):
			// insert before the comment
			newRanges = append(newRanges, commentRange.MoveBy(length))
		case commentRange.ContainsCursor(cursor):
			// insert is inside the comment
			if extendComment {
				newRanges = append(newRanges, commentRange.ExtendBy(length))
				existingRangeExtended = true
			} else {
				up, _, after, err := commentRange.InsertAt(cursor, length)
				if err != nil {
					panic(err) // unreachable: containsCursor checked
				}
				newRanges = append(newRanges, Range{Pos: commentRange.Pos, Length: up.Length}, after)
			}
		default:
			// insert is after the comment
			newRanges = append(newRanges, commentRange)
		}
	}
	// if the insert is not inside any range, add a new range
	if extendComment && !existingRangeExtended {
		newRanges = append(newRanges, Range{Pos: cursor, Length: length})
	}
	// Vendor rebuilds via `new Comment(...)` (see normalizeRanges).
	return *NewCommentUnsafe(c.ID, normalizeRanges(newRanges), c.Resolved)
}

// ApplyDelete — vendor `applyDelete(deletedRange)`: subtracts the deleted
// range from each comment range, shifting ranges after the deletion.
func (c Comment) ApplyDelete(deletedRange Range) Comment {
	newRanges := []Range{}
	for _, commentRange := range c.Ranges {
		switch {
		case commentRange.Overlaps(deletedRange):
			newRanges = append(newRanges, commentRange.Subtract(deletedRange))
		case commentRange.StartsAfter(deletedRange):
			newRanges = append(newRanges, commentRange.MoveBy(-deletedRange.Length))
		default:
			newRanges = append(newRanges, commentRange)
		}
	}
	// Vendor rebuilds via `new Comment(...)` (see normalizeRanges).
	return *NewCommentUnsafe(c.ID, normalizeRanges(newRanges), c.Resolved)
}

// ApplyTextOperation — vendor `applyTextOperation(operation, commentId)`:
// replays the op list against this comment's ranges. The cursor tracks the
// OUTPUT string (inserts advance it, removes do not).
func (c Comment) ApplyTextOperation(op *TextOperation, commentID string) Comment {
	// method value, re-bound each step (vendor: `let comment = this`)
	comment := c
	cursor := 0
	for _, o := range op.Ops {
		switch s := o.(type) {
		case RetainOp:
			cursor += s.Length
		case InsertOp:
			comment = comment.ApplyInsert(cursor, Units(s.Insertion), coversID(s.CommentIDs, commentID))
			cursor += Units(s.Insertion)
		case RemoveOp:
			comment = comment.ApplyDelete(Range{Pos: cursor, Length: s.Length})
		}
	}
	return comment
}

// coversID — vendor `op.commentIds?.includes(commentId)`.
func coversID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// IsEmpty — vendor `isEmpty()`: no ranges.
func (c Comment) IsEmpty() bool { return len(c.Ranges) == 0 }

// ToRaw — vendor `toRaw()`: {id, ranges, resolved?}.
func (c Comment) ToRaw() map[string]any {
	ranges := make([]any, 0, len(c.Ranges))
	for _, r := range c.Ranges {
		ranges = append(ranges, r.ToRaw())
	}
	raw := map[string]any{"id": c.ID, "ranges": ranges}
	if c.Resolved {
		raw["resolved"] = true
	}
	return raw
}

// CommentFromRaw — vendor `Comment.fromRaw(rawComment)`.
func CommentFromRaw(raw map[string]any) (*Comment, error) {
	id, _ := raw["id"].(string)
	ranges := []Range{}
	if rawRanges, ok := raw["ranges"].([]any); ok {
		for _, rr := range rawRanges {
			m, ok := rr.(map[string]any)
			if !ok {
				return nil, NewRangeError("Invalid comment range", nil)
			}
			rng, err := RangeFromWire(m)
			if err != nil {
				return nil, err
			}
			ranges = append(ranges, rng)
		}
	}
	resolved, _ := raw["resolved"].(bool)
	c, err := NewComment(id, ranges, resolved)
	if err != nil {
		return nil, err
	}
	return c, nil
}
