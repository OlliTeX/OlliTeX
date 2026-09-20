package ot

// Port of comment.js.

import "sort"

// Comment is a single user comment with ranges in a single file.
type Comment struct {
	id       string
	ranges   []*Range
	resolved bool
}

// NewComment mirrors the Comment constructor (id, ranges, resolved).
// Ranges are merged as in the JS constructor.
func NewComment(id string, ranges []*Range, resolved bool) (*Comment, error) {
	merged, err := mergeRanges(ranges)
	if err != nil {
		return nil, err
	}
	return &Comment{id: id, ranges: merged, resolved: resolved}, nil
}

// ID returns the comment id.
func (c *Comment) ID() string { return c.id }

// Ranges returns the (read-only) ranges.
func (c *Comment) Ranges() []*Range { return c.ranges }

// Resolved reports the resolved flag.
func (c *Comment) Resolved() bool { return c.resolved }

// IsEmpty mirrors isEmpty().
func (c *Comment) IsEmpty() bool { return len(c.ranges) == 0 }

// ApplyInsert mirrors applyInsert(cursor, length, extendComment).
// cursor is *int mirroring the JS range.pos (undefined = nil).
func (c *Comment) ApplyInsert(cursor *int, length int, extendComment bool) *Comment {
	existingRangeExtended := false
	newRanges := make([]*Range, 0, len(c.ranges)+1)

	for _, cr := range c.ranges {
		end := cr.End()
		if cursor == nil && end == nil {
			// cursor === commentRange.end where both are undefined: JS
			// `undefined === undefined` is true -> "insert right after".
			if extendComment {
				newRanges = append(newRanges, cr.ExtendBy(length))
				existingRangeExtended = true
			} else {
				newRanges = append(newRanges, cr)
			}
		} else if cursor != nil && end != nil && *cursor == *end {
			// insert right after the comment
			if extendComment {
				newRanges = append(newRanges, cr.ExtendBy(length))
				existingRangeExtended = true
			} else {
				newRanges = append(newRanges, cr)
			}
		} else if cr.Pos != nil && cursor != nil && *cursor == *cr.Pos {
			// insert at the start of the comment
			if extendComment {
				newRanges = append(newRanges, cr.ExtendBy(length))
				existingRangeExtended = true
			} else {
				newRanges = append(newRanges, cr.MoveBy(length))
			}
		} else if cr.StartIsAfter(cursor) {
			// insert before the comment
			newRanges = append(newRanges, cr.MoveBy(length))
		} else if cr.ContainsCursor(cursor) {
			// insert is inside the comment
			if extendComment {
				newRanges = append(newRanges, cr.ExtendBy(length))
				existingRangeExtended = true
			} else {
				rangeUpToCursor, _, rangeAfterCursor, _ := cr.InsertAt(cursor, length)
				// use current commentRange for the part before the cursor
				newRanges = append(newRanges, &Range{Pos: cr.Pos, Length: rangeUpToCursor.Length})
				// add the part after the cursor as a new range
				newRanges = append(newRanges, rangeAfterCursor)
			}
		} else {
			// insert is after the comment
			newRanges = append(newRanges, cr)
		}
	}

	if extendComment && !existingRangeExtended {
		var pos *int
		if cursor != nil {
			v := *cursor
			pos = &v
		}
		newRanges = append(newRanges, &Range{Pos: pos, Length: length})
	}

	merged, err := mergeRanges(newRanges)
	if err != nil {
		// Faithful: the JS constructor throws; the oracle never exercises a
		// failing merge from applyInsert, so panic.
		panic(err)
	}
	return &Comment{id: c.id, ranges: merged, resolved: c.resolved}
}

// ApplyDelete mirrors applyDelete(deletedRange).
func (c *Comment) ApplyDelete(deletedRange *Range) *Comment {
	newRanges := make([]*Range, 0, len(c.ranges))
	for _, cr := range c.ranges {
		var pushed *Range
		if cr.Overlaps(deletedRange) {
			sub, err := cr.Subtract(deletedRange)
			if err != nil {
				panic(err)
			}
			pushed = sub
		} else if cr.StartsAfter(deletedRange) {
			pushed = cr.MoveBy(-deletedRange.Length)
		} else {
			pushed = cr
		}
		newRanges = append(newRanges, pushed)
	}
	merged, err := mergeRanges(newRanges)
	if err != nil {
		panic(err)
	}
	return &Comment{id: c.id, ranges: merged, resolved: c.resolved}
}

// ToRaw mirrors toRaw(). The resolved flag is omitted when false.
func (c *Comment) ToRaw() map[string]any {
	out := make([]any, len(c.ranges))
	for i, r := range c.ranges {
		out[i] = r.ToRaw()
	}
	raw := map[string]any{
		"id":     c.id,
		"ranges": out,
	}
	if c.resolved {
		raw["resolved"] = true
	}
	return raw
}

// CommentFromRaw mirrors Comment.fromRaw.
func CommentFromRaw(id string, rawRanges []map[string]any, resolved bool) (*Comment, error) {
	ranges := make([]*Range, 0, len(rawRanges))
	for _, rr := range rawRanges {
		var pos *int
		if p, ok := rr["pos"].(int); ok {
			pos = &p
		}
		length := 0
		if l, ok := rr["length"].(int); ok {
			length = l
		}
		r, err := RangeFromRaw(pos, &length)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, r)
	}
	return NewComment(id, ranges, resolved)
}

// mergeRanges mirrors Comment.mergeRanges.
//
// V8 sort: the comparator `a.start - b.start` yields NaN when either start is
// undefined; V8 treats the items as equal and V8's TimSort (stable) preserves
// the input order. Go: sort.SliceStable preserves input order for "equal"
// elements, matching V8 on real data (no nil-Pos in raw comment ranges).
func mergeRanges(ranges []*Range) ([]*Range, error) {
	sorted := make([]*Range, len(ranges))
	copy(sorted, ranges)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i].Pos, sorted[j].Pos
		if a == nil || b == nil {
			return false // "equal" (NaN comparator) => keep input order
		}
		return *a < *b
	})

	mergedRanges := make([]*Range, 0, len(sorted))
	for _, rv := range sorted {
		if rv.IsEmpty() {
			return nil, newOError("OError", "Comment range cannot be empty", nil)
		}
		var lastMerged *Range
		if len(mergedRanges) > 0 {
			lastMerged = mergedRanges[len(mergedRanges)-1]
			if lastMerged.Overlaps(rv) {
				return nil, newOError("OError", "Ranges cannot overlap", nil)
			}
		}
		if lastMerged != nil && lastMerged.CanMerge(rv) {
			merged, err := lastMerged.Merge(rv)
			if err != nil {
				return nil, err
			}
			mergedRanges[len(mergedRanges)-1] = merged
		} else {
			mergedRanges = append(mergedRanges, rv)
		}
	}
	return mergedRanges, nil
}
