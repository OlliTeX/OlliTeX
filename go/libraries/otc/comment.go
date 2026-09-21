package otc

// Comment mirrors comment.js: an id + a set of ranges (+ resolved).
type Comment struct {
	ID       string
	Ranges   []Range
	Resolved bool
}

// NewComment builds a Comment, merging ranges (Node: `new Comment(id, ranges, resolved)`).
func NewComment(id string, ranges []Range, resolved bool) *Comment {
	return NewCommentWithID(id, mergeCommentRanges(ranges), resolved)
}

// NewCommentWithID builds a Comment with already-merged ranges.
func NewCommentWithID(id string, ranges []Range, resolved bool) *Comment {
	return &Comment{ID: id, Ranges: ranges, Resolved: resolved}
}

// Len returns the number of ranges (Node: `get length()`).
func (c *Comment) Len() int { return len(c.Ranges) }

// ApplyInsert applies an insert (Node: `applyInsert`).
func (c *Comment) ApplyInsert(cursor, length int, extendComment bool) *Comment {
	existingRangeExtended := false
	newRanges := []Range{}
	for _, cr := range c.Ranges {
		switch {
		case cursor == cr.End():
			if extendComment {
				newRanges = append(newRanges, cr.ExtendBy(length))
				existingRangeExtended = true
			} else {
				newRanges = append(newRanges, cr)
			}
		case cursor == cr.Start():
			if extendComment {
				newRanges = append(newRanges, cr.ExtendBy(length))
				existingRangeExtended = true
			} else {
				newRanges = append(newRanges, cr.MoveBy(length))
			}
		case cr.StartIsAfter(cursor):
			newRanges = append(newRanges, cr.MoveBy(length))
		case cr.ContainsCursor(cursor):
			if extendComment {
				newRanges = append(newRanges, cr.ExtendBy(length))
				existingRangeExtended = true
			} else {
				ranges := cr.InsertAt(cursor, length)
				newRanges = append(newRanges, NewRange(cr.Pos, ranges[0].Length))
				newRanges = append(newRanges, ranges[2])
			}
		default:
			newRanges = append(newRanges, cr)
		}
	}
	if extendComment && !existingRangeExtended {
		newRanges = append(newRanges, NewRange(cursor, length))
	}
	return NewComment(c.ID, newRanges, c.Resolved)
}

// ApplyDelete applies a delete (Node: `applyDelete`).
func (c *Comment) ApplyDelete(deleted Range) *Comment {
	newRanges := []Range{}
	for _, cr := range c.Ranges {
		if cr.Overlaps(deleted) {
			newRanges = append(newRanges, cr.Subtract(deleted))
		} else if cr.StartsAfter(deleted) {
			newRanges = append(newRanges, cr.MoveBy(-deleted.Length))
		} else {
			newRanges = append(newRanges, cr)
		}
	}
	return NewComment(c.ID, newRanges, c.Resolved)
}

// ApplyTextOperation applies a whole operation (Node: `applyTextOperation`).
func (c *Comment) ApplyTextOperation(op *TextOperation, commentID string) *Comment {
	comment := c
	cursor := 0
	for _, s := range op.Ops {
		switch t := s.(type) {
		case RetainOp:
			cursor += t.Length
		case InsertOp:
			extend := false
			for _, cid := range t.CommentIds {
				if cid == commentID {
					extend = true
				}
			}
			comment = comment.ApplyInsert(cursor, len(t.Insertion), extend)
			cursor += len(t.Insertion)
		case RemoveOp:
			comment = comment.ApplyDelete(NewRange(cursor, t.Length))
		}
	}
	return comment
}

// IsEmpty reports whether the comment has no ranges (Node: `isEmpty`).
func (c *Comment) IsEmpty() bool { return len(c.Ranges) == 0 }

// ToRaw serialises {id, ranges, resolved?} (Node: `toRaw`).
func (c *Comment) ToRaw() map[string]any {
	raw := map[string]any{
		"id":     c.ID,
		"ranges": commentRangesRaw(c.Ranges),
	}
	if c.Resolved {
		raw["resolved"] = true
	}
	return raw
}

func commentRangesRaw(ranges []Range) []map[string]int {
	out := make([]map[string]int, len(ranges))
	for i, r := range ranges {
		out[i] = r.ToRaw()
	}
	return out
}

// mergeRanges collapses overlapping/adjacent ranges (Node: `mergeRanges`).
func mergeCommentRanges(ranges []Range) []Range {
	merged := []Range{}
	sorted := make([]Range, len(ranges))
	copy(sorted, ranges)
	sortRanges(sorted)
	for _, range_ := range sorted {
		if range_.IsEmpty() {
			continue
		}
		var last *Range
		if n := len(merged); n > 0 {
			last = &merged[n-1]
		}
		if last != nil && last.Overlaps(range_) {
			panic("Ranges cannot overlap")
		}
		if range_.IsEmpty() {
			panic("Comment range cannot be empty")
		}
		if last != nil && last.CanMerge(range_) {
			merged[len(merged)-1] = last.Merge(range_)
		} else {
			merged = append(merged, range_)
		}
	}
	return merged
}

func sortRanges(r []Range) {
	for i := 1; i < len(r); i++ {
		for j := i; j > 0 && r[j].Start() < r[j-1].Start(); j-- {
			r[j], r[j-1] = r[j-1], r[j]
		}
	}
}

// FromRawComment builds a Comment from raw (Node: `Comment.fromRaw`).
func FromRawComment(raw map[string]any) (*Comment, error) {
	id, _ := raw["id"].(string)
	var ranges []Range
	if v, ok := raw["ranges"]; ok && v != nil {
		ranges = asRawRanges(v)
	}
	resolved, _ := raw["resolved"].(bool)
	return NewComment(id, ranges, resolved), nil
}
