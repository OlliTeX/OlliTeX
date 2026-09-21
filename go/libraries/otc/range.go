package otc

import (
	"fmt"
)

// Range is a [start, end) character interval (Node: range.js). Node throws an
// OError on invalid construction and on several invariant violations; the Go
// port mirrors that with panics (the oracle only asserts "throws").
type Range struct {
	Pos    int
	Length int
}

// NewRange builds a Range (Node: `new Range(pos, length)`). Panics on
// pos < 0 or length < 0 (Node: throws an OError).
func NewRange(pos, length int) Range {
	if pos < 0 || length < 0 {
		panic(fmt.Sprintf("Invalid range (pos=%d length=%d)", pos, length))
	}
	return Range{Pos: pos, Length: length}
}

// Start returns the range start (Node: `get start()`).
func (r Range) Start() int { return r.Pos }

// End returns the range start+length (Node: `get end()`).
func (r Range) End() int { return r.Pos + r.Length }

// Equals reports identity of start+length (Node: `equals`).
func (r Range) Equals(other Range) bool {
	return r.Pos == other.Pos && r.Length == other.Length
}

// StartsAfter reports r.start >= other.end (Node: `startsAfter`).
func (r Range) StartsAfter(other Range) bool { return r.Start() >= other.End() }

// StartIsAfter reports r.start > pos (Node: `startIsAfter`).
func (r Range) StartIsAfter(pos int) bool { return r.Start() > pos }

// IsEmpty reports length == 0 (Node: `isEmpty`).
func (r Range) IsEmpty() bool { return r.Length == 0 }

// Contains reports the range contains another (Node: `contains`).
func (r Range) Contains(other Range) bool {
	return r.Start() <= other.Start() && r.End() >= other.End()
}

// ContainsCursor reports the range contains a cursor (Node: `containsCursor`).
func (r Range) ContainsCursor(cursor int) bool {
	return r.Start() <= cursor && r.End() >= cursor
}

// Overlaps reports the ranges share at least one character (Node: `overlaps`).
func (r Range) Overlaps(other Range) bool {
	return r.Start() < other.End() && r.End() > other.Start()
}

// OverlapsStart reports r overlaps the start of other (Node: `overlapsStart`).
func (r Range) OverlapsStart(other Range) bool {
	return r.Start() <= other.Start() && r.End() > other.Start()
}

// OverlapsEnd reports r overlaps the end of other (Node: `overlapsEnd`).
func (r Range) OverlapsEnd(other Range) bool {
	return r.Start() < other.End() && r.End() >= other.End()
}

// Touches reports the ranges meet at an endpoint (Node: `touches`).
func (r Range) Touches(other Range) bool {
	return r.End() == other.Start() || r.Start() == other.End()
}

// Subtract returns r with other removed (Node: `subtract`).
func (r Range) Subtract(other Range) Range {
	if r.Contains(other) {
		return r.ShrinkBy(other.Length)
	}
	if other.Contains(r) {
		return NewRange(r.Pos, 0)
	}
	if other.Overlaps(r) {
		if other.Start() < r.Start() {
			intersected := other.End() - r.Start()
			return NewRange(other.Pos, r.Length-intersected)
		}
		intersected := r.End() - other.Start()
		return NewRange(r.Pos, r.Length-intersected)
	}
	return NewRange(r.Pos, r.Length)
}

// CanMerge reports r and other are adjacent or overlapping (Node: `canMerge`).
func (r Range) CanMerge(other Range) bool {
	return r.Overlaps(other) || r.Touches(other)
}

// Merge returns the union of two mergeable ranges (Node: `merge`).
func (r Range) Merge(other Range) Range {
	if !r.CanMerge(other) {
		panic("Ranges cannot be merged")
	}
	newPos := min(r.Pos, other.Pos)
	newEnd := max(r.End(), other.End())
	return NewRange(newPos, newEnd-newPos)
}

// MoveBy shifts the range (Node: `moveBy`).
func (r Range) MoveBy(length int) Range { return NewRange(r.Pos+length, r.Length) }

// ExtendBy grows the range's length (Node: `extendBy`).
func (r Range) ExtendBy(extensionLength int) Range {
	return NewRange(r.Pos, r.Length+extensionLength)
}

// ShrinkBy shortens the range (Node: `shrinkBy`).
func (r Range) ShrinkBy(shrinkLength int) Range {
	newLength := r.Length - shrinkLength
	if newLength < 0 {
		panic("Cannot shrink range by more than its length")
	}
	return NewRange(r.Pos, newLength)
}

// InsertAt splits the range at cursor and inserts length there, returning
// [before, inserted, after] (Node: `insertAt`).
func (r Range) InsertAt(cursor, length int) [3]Range {
	if !r.ContainsCursor(cursor) {
		panic("The cursor must be contained in the range")
	}
	upToCursor := NewRange(r.Pos, cursor-r.Pos)
	inserted := NewRange(cursor, length)
	after := NewRange(cursor+length, r.Length-upToCursor.Length)
	return [3]Range{upToCursor, inserted, after}
}

// ToRaw serialises {pos, length} (Node: `toRaw`).
func (r Range) ToRaw() map[string]int {
	return map[string]int{"pos": r.Pos, "length": r.Length}
}

// FromRaw builds a Range from a raw {pos, length} (Node: `static fromRaw`).
func FromRawRange(raw map[string]int) Range {
	return NewRange(raw["pos"], raw["length"])
}

// SplitAt divides the range at cursor into [before, after] (Node: `splitAt`).
func (r Range) SplitAt(cursor int) [2]Range {
	if !r.ContainsCursor(cursor) {
		panic("The cursor must be contained in the range")
	}
	upToCursor := NewRange(r.Pos, cursor-r.Pos)
	after := NewRange(cursor, r.Length-upToCursor.Length)
	return [2]Range{upToCursor, after}
}

// Intersect returns the intersection or nil (Node: `intersect`).
func (r Range) Intersect(other Range) *Range {
	if r.Contains(other) {
		c := other
		return &c
	}
	if other.Contains(r) {
		c := r
		return &c
	}
	if other.OverlapsStart(r) {
		c := NewRange(r.Pos, other.End()-r.Start())
		return &c
	}
	if other.OverlapsEnd(r) {
		c := NewRange(other.Pos, r.End()-other.Start())
		return &c
	}
	return nil
}

// IsRange reports whether v is a Range (helper).
func IsRange(v any) (Range, bool) {
	if p, ok := v.(Range); ok {
		return p, true
	}
	if p, ok := v.(*Range); ok {
		return *p, true
	}
	return Range{}, false
}
