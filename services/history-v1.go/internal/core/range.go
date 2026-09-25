// Range arithmetic — mirror of overleaf-editor-core/lib/range.js (source
// verified). Wire: {"pos":<int>,"length":<int>} for fresh values; legacy
// positional [pos,length] arrays are accepted on read.
package core

import "encoding/json"

// Range is a [pos, pos+length) text range.
type Range struct {
	Pos    int
	Length int
}

func (r Range) Start() int    { return r.Pos }
func (r Range) End() int      { return r.Pos + r.Length }
func (r Range) IsEmpty() bool { return r.Length == 0 }

// ToRaw ports Range.toRaw: {"pos","length"} (fresh value, insertion order).
func (r Range) ToRaw() json.RawMessage {
	b, _ := json.Marshal(struct {
		Pos    int `json:"pos"`
		Length int `json:"length"`
	}{r.Pos, r.Length})
	return b
}

// RangeFromRaw ports Range.fromRaw (object or legacy positional array).
func RangeFromRaw(b json.RawMessage) (Range, error) {
	if len(b) > 2 && b[0] == '[' {
		var arr [2]int
		if err := json.Unmarshal(b, &arr); err != nil {
			return Range{}, &BadRawError{Msg: "bad range: " + string(b)}
		}
		return Range{arr[0], arr[1]}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return Range{}, &BadRawError{Msg: "bad range: " + string(b)}
	}
	var r Range
	if d, ok := m["pos"].(float64); ok {
		r.Pos = int(d)
	}
	if l, ok := m["length"].(float64); ok {
		r.Length = int(l)
	}
	return r, nil
}

// Overlaps — other.start < this.end && this.start < other.end.
func (r Range) Overlaps(o Range) bool { return r.Pos < o.End() && r.End() > o.Pos }

// Contains reports whether r fully contains o.
func (r Range) Contains(o Range) bool { return r.Pos <= o.Pos && r.End() >= o.End() }

// Subtract ports Range.subtract.
func (r Range) Subtract(o Range) Range {
	switch {
	case r.Contains(o):
		return r.ShrinkBy(o.Length)
	case o.Contains(r):
		return Range{r.Pos, 0}
	case r.Overlaps(o):
		if o.Pos < r.Pos {
			return Range{o.Pos, r.Length - (o.End() - r.Pos)}
		}
		return Range{r.Pos, r.Length - (r.End() - o.Pos)}
	default:
		return r
	}
}

// MoveBy returns a new range shifted by delta.
func (r Range) MoveBy(delta int) Range { return Range{r.Pos + delta, r.Length} }

// InsertAt ports Range.insertAt: [beforeCursor, inserted, afterCursor].
func (r Range) InsertAt(cursor, length int) (Range, Range, Range) {
	lenUpToCursor := cursor - r.Pos
	return Range{r.Pos, lenUpToCursor}, Range{cursor, length}, Range{cursor + length, r.Length - lenUpToCursor}
}

// SplitAt ports Range.splitAt: [beforeCursor, afterCursor].
func (r Range) SplitAt(cursor int) (Range, Range) {
	lenUp := cursor - r.Pos
	return Range{r.Pos, lenUp}, Range{cursor, r.Length - lenUp}
}

func (r Range) StartIsAfter(pos int) bool { return r.Pos > pos }
func (r Range) StartsAfter(o Range) bool  { return r.Pos >= o.End() }
func (r Range) ContainsCursor(c int) bool { return r.Pos <= c && r.End() >= c }
func (r Range) Touches(o Range) bool      { return r.End() == o.Pos || r.Pos == o.End() }
func (r Range) CanMerge(o Range) bool     { return r.Overlaps(o) || r.Touches(o) }
func (r Range) ExtendBy(n int) Range      { return Range{r.Pos, r.Length + n} }
func (r Range) ShrinkBy(n int) Range      { return Range{r.Pos, r.Length - n} }
