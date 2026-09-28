package syncmanager

import (
	"fmt"
	"sort"
)

// --- Range (vendor overleaf-editor-core Range: {pos, length}) ----------------

// Range — vendor Range class (pos + length).
type Range struct {
	Pos    int
	Length int
}

func (r Range) End() int                 { return r.Pos + r.Length }
func (r Range) Start() int               { return r.Pos }
func (r Range) StartIsAfter(p int) bool  { return r.Pos > p }
func (r Range) StartsAfter(o Range) bool { return r.Pos >= o.End() }
func (r Range) IsEmpty() bool            { return r.Length == 0 }

// ContainsCursor — vendor: start <= cursor && end >= cursor.
func (r Range) ContainsCursor(cursor int) bool { return r.Pos <= cursor && r.End() >= cursor }

// Contains — vendor: full containment of other.
func (r Range) Contains(o Range) bool { return o.Pos >= r.Pos && o.End() <= r.End() }

// Overlaps — vendor: at least one character in common.
func (r Range) Overlaps(o Range) bool { return r.Pos < o.End() && r.End() > o.Pos }

// Touches — vendor: end === other.start || start === other.end.
func (r Range) Touches(o Range) bool { return r.End() == o.Pos || r.Pos == o.End() }

func (r Range) MoveBy(n int) Range { return Range{Pos: r.Pos + n, Length: r.Length} }
func (r Range) ExtendBy(n int) Range {
	return Range{Pos: r.Pos, Length: r.Length + n}
}

// InsertAt — vendor (throws if cursor not contained: the vendor callers only
// invoke it in the containsCursor branch).
func (r Range) InsertAt(cursor, length int) (Range, Range, Range) {
	upTo := Range{Pos: r.Pos, Length: cursor - r.Pos}
	inserted := Range{Pos: cursor, Length: length}
	after := Range{Pos: cursor + length, Length: r.Length - upTo.Length}
	return upTo, inserted, after
}

// Subtract — vendor range.subtract (overlaps branch as per vendor).
func (r Range) Subtract(o Range) Range {
	if o.Overlaps(r) {
		if o.Pos < r.Pos {
			intersected := o.End() - r.Pos
			return Range{Pos: o.Pos, Length: r.Length - intersected}
		}
		intersected := r.End() - o.Pos
		return Range{Pos: r.Pos, Length: r.Length - intersected}
	}
	return Range{Pos: r.Pos, Length: r.Length}
}

func (r Range) ToRaw() map[string]int { return map[string]int{"pos": r.Pos, "length": r.Length} }

// --- Comment (vendor editor-core Comment: id, ranges, resolved) --------------

type Comment struct {
	ID       string
	Ranges   []Range
	Resolved bool
}

// NewComment — vendor constructor (mergeRanges: sort, skip empties, overlap →
// 'Ranges cannot overlap', adjacent merged).
func NewComment(id string, ranges []Range, resolved bool) (*Comment, error) {
	merged, err := mergeRanges(ranges)
	if err != nil {
		return nil, err
	}
	return &Comment{ID: id, Ranges: merged, Resolved: resolved}, nil
}

func mergeRanges(ranges []Range) ([]Range, error) {
	sorted := make([]Range, len(ranges))
	copy(sorted, ranges)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Pos < sorted[j].Pos })
	merged := []Range{}
	for _, r := range sorted {
		if r.IsEmpty() {
			continue
		}
		last := len(merged) - 1
		if last >= 0 {
			if merged[last].Overlaps(r) {
				return nil, fmt.Errorf("Ranges cannot overlap")
			}
			if merged[last].Touches(r) {
				merged[last] = mergeRange(merged[last], r)
				continue
			}
		}
		merged = append(merged, r)
	}
	return merged, nil
}

func mergeRange(a, b Range) Range {
	pos := a.Pos
	if b.Pos < pos {
		pos = b.Pos
	}
	end := a.End()
	if b.End() > end {
		end = b.End()
	}
	return Range{Pos: pos, Length: end - pos}
}

// ApplyInsert — vendor Comment.applyInsert(cursor, length, extendComment).
func (c *Comment) ApplyInsert(cursor, length int, extendComment bool) *Comment {
	existingExtended := false
	newRanges := []Range{}
	for _, r := range c.Ranges {
		switch {
		case cursor == r.End():
			if extendComment {
				newRanges = append(newRanges, r.ExtendBy(length))
				existingExtended = true
			} else {
				newRanges = append(newRanges, r)
			}
		case cursor == r.Pos:
			if extendComment {
				newRanges = append(newRanges, r.ExtendBy(length))
				existingExtended = true
			} else {
				newRanges = append(newRanges, r.MoveBy(length))
			}
		case r.StartIsAfter(cursor):
			newRanges = append(newRanges, r.MoveBy(length))
		case r.ContainsCursor(cursor):
			if extendComment {
				newRanges = append(newRanges, r.ExtendBy(length))
				existingExtended = true
			} else {
				up, _, after := r.InsertAt(cursor, length)
				if up.Length > 0 {
					newRanges = append(newRanges, up)
				}
				newRanges = append(newRanges, after)
			}
		default:
			newRanges = append(newRanges, r)
		}
	}
	if extendComment && !existingExtended {
		newRanges = append(newRanges, Range{Pos: cursor, Length: length})
	}
	nc, _ := NewComment(c.ID, newRanges, c.Resolved)
	return nc
}

// ApplyDelete — vendor Comment.applyDelete (overlaps → subtract; startsAfter →
// move; else keep).
func (c *Comment) ApplyDelete(deletedRange Range) *Comment {
	newRanges := []Range{}
	for _, r := range c.Ranges {
		if r.Overlaps(deletedRange) {
			newRanges = append(newRanges, r.Subtract(deletedRange))
		} else if r.StartsAfter(deletedRange) {
			newRanges = append(newRanges, r.MoveBy(-deletedRange.Length))
		} else {
			newRanges = append(newRanges, r)
		}
	}
	nc, _ := NewComment(c.ID, newRanges, c.Resolved)
	return nc
}

// ToRaw — vendor: {id, ranges:[{pos,length}], resolved? (only when true)}.
func (c *Comment) ToRaw() map[string]any {
	raw := map[string]any{
		"id":     c.ID,
		"ranges": toRawRanges(c.Ranges),
	}
	if c.Resolved {
		raw["resolved"] = true
	}
	return raw
}

func toRawRanges(rs []Range) []any {
	out := make([]any, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.ToRaw())
	}
	return out
}

// --- TrackedChange (vendor: range + tracking) --------------------------------

type Tracking struct {
	Type   string
	UserID string
	TS     string
}

type TrackedChange struct {
	Range    Range
	Tracking Tracking
}

// ApplyInsert — vendor TrackedChangeList.applyInsert (no opts.tracking in the
// C16 call path; the list-level merge follows with _mergeRanges — the C16
// consumer only reads the sorted list, so we keep list order stable).
func applyTrackedInsert(changes []TrackedChange, cursor int, insertedText string) []TrackedChange {
	out := []TrackedChange{}
	for _, tc := range changes {
		switch {
		case tc.Range.StartIsAfter(cursor) || cursor == tc.Range.Pos:
			out = append(out, TrackedChange{Range: tc.Range.MoveBy(len(insertedText)), Tracking: tc.Tracking})
		case cursor == tc.Range.End():
			out = append(out, tc)
		case tc.Range.ContainsCursor(cursor):
			first, _, third := tc.Range.InsertAt(cursor, len(insertedText))
			if !first.IsEmpty() {
				out = append(out, TrackedChange{Range: first, Tracking: tc.Tracking})
			}
			if !third.IsEmpty() {
				out = append(out, TrackedChange{Range: third, Tracking: tc.Tracking})
			}
		default:
			out = append(out, tc)
		}
	}
	return out
}

// ApplyDelete — vendor TrackedChangeList.applyDelete (contains → drop;
// overlaps → subtract-if-nonempty; after-cursor → move; else keep).
func applyTrackedDelete(changes []TrackedChange, cursor, length int) []TrackedChange {
	deleted := Range{Pos: cursor, Length: length}
	out := []TrackedChange{}
	for _, tc := range changes {
		if deleted.Contains(tc.Range) {
			continue
		}
		if deleted.Overlaps(tc.Range) {
			nr := tc.Range.Subtract(deleted)
			if !nr.IsEmpty() {
				out = append(out, TrackedChange{Range: nr, Tracking: tc.Tracking})
			}
			continue
		}
		if tc.Range.StartIsAfter(cursor) {
			out = append(out, TrackedChange{Range: tc.Range.MoveBy(-length), Tracking: tc.Tracking})
			continue
		}
		out = append(out, tc)
	}
	return out
}

// --- File (vendor Core.File surface used by the expander) --------------------

// File — the snapshot file the expander operates over. Content is the loaded
// persisted content; Comments/TrackedChanges persist with it.
type File struct {
	Pathname       string
	Editable       bool
	Hash           string // vendor file.getHash() ("" = null)
	DataHash       string // vendor file.data.hash
	Metadata       map[string]any
	Content        string
	Comments       []*Comment
	TrackedChanges []TrackedChange
}

// NewStringFile — vendor File.fromString (editable text file).
func NewStringFile(pathname, content string) *File {
	return &File{
		Pathname: pathname,
		Editable: true,
		Content:  content,
		Comments: []*Comment{},
	}
}

// GetCommentsApplyInsert — vendor file.getComments().applyInsert(new Range(p, len)).
func (f *File) CommentsApplyInsert(cursor, length int) {
	out := []*Comment{}
	for _, c := range f.Comments {
		out = append(out, c.ApplyInsert(cursor, length, false))
	}
	f.Comments = out
}

// TrackedChangesApplyInsert — vendor file.getTrackedChanges().applyInsert(p, text).
func (f *File) TrackedChangesApplyInsert(cursor int, text string) {
	f.TrackedChanges = applyTrackedInsert(f.TrackedChanges, cursor, text)
}

// CommentsApplyDelete — vendor file.getComments().applyDelete(new Range(p, len)).
func (f *File) CommentsApplyDelete(cursor, length int) {
	out := []*Comment{}
	for _, c := range f.Comments {
		out = append(out, c.ApplyDelete(Range{Pos: cursor, Length: length}))
	}
	f.Comments = out
}

// TrackedChangesApplyDelete — vendor file.getTrackedChanges().applyDelete(p, len).
func (f *File) TrackedChangesApplyDelete(cursor, length int) {
	f.TrackedChanges = applyTrackedDelete(f.TrackedChanges, cursor, length)
}

// CommentsToRaw — vendor file.getComments().toRaw().
func (f *File) CommentsToRaw() []any {
	out := make([]any, 0, len(f.Comments))
	for _, c := range f.Comments {
		out = append(out, c.ToRaw())
	}
	return out
}

// TrackedChangesAsSorted — vendor asSorted (list order = insertion order; the
// vendor list is kept sorted by its merge — our port keeps insertion order,
// which matches the vendor after merges).
func (f *File) TrackedChangesAsSorted() []TrackedChange {
	out := make([]TrackedChange, len(f.TrackedChanges))
	copy(out, f.TrackedChanges)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Range.Pos < out[j].Range.Pos || (out[i].Range.Pos == out[j].Range.Pos && out[i].Range.End() < out[j].Range.End())
	})
	return out
}
