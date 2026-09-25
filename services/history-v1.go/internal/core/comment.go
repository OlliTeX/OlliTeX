package core

import "encoding/json"

// Comment — apply state: id + ranges + resolved.
type Comment struct {
	ID       string
	Ranges   []Range
	Resolved bool
}

// CommentList — apply state over comments (in-memory; Node CommentList).
type CommentList []*Comment

// FromRaw ports CommentList.fromRaw: [{id, ranges:[{pos,length}], resolved?}].
func CommentListFromRaw(raws []json.RawMessage) CommentList {
	var l CommentList
	for i := range raws {
		var probe struct {
			ID        *string           `json:"id"`
			RawRanges []json.RawMessage `json:"ranges"`
			Resolved  *bool             `json:"resolved"`
		}
		_ = json.Unmarshal(raws[i], &probe)
		c := &Comment{}
		if probe.ID != nil {
			c.ID = *probe.ID
		}
		c.Ranges = make([]Range, 0, len(probe.RawRanges))
		for _, rb := range probe.RawRanges {
			if r, err := RangeFromRaw(rb); err == nil {
				c.Ranges = append(c.Ranges, r)
			}
		}
		if probe.Resolved != nil {
			c.Resolved = *probe.Resolved
		}
		l = append(l, c)
	}
	return l
}

// ApplyInsert ports Comment.applyInsert over the whole list. cursor and
// length are UTF-16 code units.
func (l CommentList) ApplyInsert(cursor, length int, commentIDs []string) CommentList {
	out := make(CommentList, 0, len(l))
	for _, c := range l {
		extend := false
		for _, id := range commentIDs {
			if id == c.ID {
				extend = true
			}
		}
		out = append(out, c.applyInsert(cursor, length, extend))
	}
	return out
}

// ApplyDelete ports Comment.applyDelete over the whole list.
func (l CommentList) ApplyDelete(dr Range) CommentList {
	out := make(CommentList, 0, len(l))
	for _, c := range l {
		out = append(out, c.applyDelete(dr))
	}
	return out
}

func (c *Comment) applyInsert(cursor, length int, extend bool) *Comment {
	ranges := make([]Range, 0, len(c.Ranges))
	extended := false
	for _, r := range c.Ranges {
		switch {
		case cursor == r.Pos+r.Length:
			if extend {
				ranges = append(ranges, r.ExtendBy(length))
				extended = true
			} else {
				ranges = append(ranges, r)
			}
		case cursor == r.Pos:
			if extend {
				ranges = append(ranges, r.ExtendBy(length))
				extended = true
			} else {
				ranges = append(ranges, r.MoveBy(length))
			}
		case r.Pos > cursor:
			ranges = append(ranges, r.MoveBy(length))
		case r.ContainsCursor(cursor):
			if extend {
				ranges = append(ranges, r.ExtendBy(length))
				extended = true
			} else {
				up, _, after := r.InsertAt(cursor, length)
				ranges = append(ranges, up, after)
			}
		default:
			ranges = append(ranges, r)
		}
	}
	if extend && !extended {
		ranges = append(ranges, Range{Pos: cursor, Length: length})
	}
	nc := &Comment{ID: c.ID, Resolved: c.Resolved}
	nc.Ranges = mergeRanges(ranges)
	return nc
}

func (c *Comment) applyDelete(dr Range) *Comment {
	ranges := make([]Range, 0, len(c.Ranges))
	for _, r := range c.Ranges {
		switch {
		case r.Overlaps(dr):
			ranges = append(ranges, r.Subtract(dr))
		case r.Pos >= dr.Pos+dr.Length:
			ranges = append(ranges, r.MoveBy(-dr.Length))
		default:
			ranges = append(ranges, r)
		}
	}
	nc := &Comment{ID: c.ID, Resolved: c.Resolved}
	nc.Ranges = mergeRanges(ranges)
	return nc
}

// Add — ports CommentList.add (upsert by ID, merge ranges).
func (l CommentList) Add(c *Comment) CommentList {
	out := make(CommentList, 0, len(l)+1)
	merged := false
	for _, e := range l {
		if e.ID == c.ID {
			merged = true
			out = append(out, &Comment{ID: c.ID, Ranges: mergeRanges(append(append([]Range{}, e.Ranges...), c.Ranges...)), Resolved: c.Resolved})
			continue
		}
		out = append(out, e)
	}
	if !merged {
		out = append(out, c)
	}
	return out
}

// AddComment ports AddCommentOperation.apply (fileData.comments.add).
func (l CommentList) AddComment(id string, rngs []Range, resolved bool) CommentList {
	c := &Comment{ID: id, Ranges: rngs, Resolved: resolved}
	return l.Add(c)
}

// DeleteComment ports DeleteCommentOperation.apply (fileData.comments.delete).
func (l CommentList) DeleteComment(id string) CommentList {
	out := make(CommentList, 0, len(l))
	for _, c := range l {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return out
}

// SetCommentState ports SetCommentStateOperation.apply: replace the matching
// comment with a new one carrying the given resolved flag.
func (l CommentList) SetCommentState(id string, resolved bool) CommentList {
	for _, c := range l {
		if c.ID == id {
			nc := &Comment{ID: c.ID, Ranges: c.Ranges, Resolved: resolved}
			out := make(CommentList, 0, len(l))
			for _, e := range l {
				if e.ID != id {
					out = append(out, e)
				}
			}
			out = append(out, nc)
			return out
		}
	}
	return l
}

// mergeRanges ports Comment.mergeRanges / TrackedChangeList._mergeRanges:
// drop empties, sort by start, merge adjacent/overlapping.
func mergeRanges(rs []Range) []Range {
	sorted := make([]Range, len(rs))
	copy(sorted, rs)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1].Pos > sorted[j].Pos; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	var out []Range
	for _, r := range sorted {
		if r.Length == 0 {
			continue
		}
		if len(out) > 0 && out[len(out)-1].CanMerge(r) {
			last := out[len(out)-1]
			end := last.Pos + last.Length
			if r.Pos+r.Length > end {
				end = r.Pos + r.Length
			}
			out[len(out)-1] = Range{Pos: last.Pos, Length: end - last.Pos}
			continue
		}
		out = append(out, r)
	}
	return out
}
