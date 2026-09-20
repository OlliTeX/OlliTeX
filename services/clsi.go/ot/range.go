package ot

import "fmt"

// Range is a position and length in a file's text, measured in UTF-16 code
// units.
//
// The Pos field is a *int mirroring the JS Range: raw ranges (and derived
// ranges) can carry an undefined pos, modelled here as a nil pointer. With an
// undefined pos every JS comparison involving pos is false (undefined < 2 is
// false, undefined === 0 is false, etc.) and arithmetic on it yields
// undefined (undefined + 2 is NaN). The Go semantics below mirror that:
// any comparison with a nil Pos is false, and range arithmetic that consumes
// pos keeps it nil.
//
// Production CLSI raw data always carries {pos, length}; nil-Pos arises only
// from degenerate input (dataset cases 23/26 pass {position: ..} where the
// code reads {pos: ..}), and the faithful outcome is preserved verbatim.
type Range struct {
	Pos    *int // nil = JS undefined
	Length int
}

// NewRange builds a Range from concrete numbers, mirroring the JS
// constructor: pos < 0 || length < 0 throws OError("Invalid range", {pos,
// length}).
func NewRange(pos, length int) (*Range, error) {
	if pos < 0 || length < 0 {
		return nil, newOError("OError", "Invalid range", map[string]any{"pos": pos, "length": length})
	}
	p := pos
	return &Range{Pos: &p, Length: length}, nil
}

// RangeFromRaw builds a Range from a raw {pos, length}, mirroring
// Range.fromRaw: new Range(raw.pos, raw.length).
//
//   - absent pos          -> nil Pos (JS undefined)
//   - absent/null length  -> 0 (JS undefined is not < 0, so no error; the
//     dataset only contains absent keys in practice)
func RangeFromRaw(pos *int, length *int) (*Range, error) {
	lengthVal := 0
	if length != nil {
		lengthVal = *length
	}
	if pos != nil && (*pos < 0 || lengthVal < 0) {
		return nil, newOError("OError", "Invalid range", map[string]any{"pos": *pos, "length": lengthVal})
	}
	return &Range{Pos: pos, Length: lengthVal}, nil
}

// Start mirrors get start().
func (r *Range) Start() *int { return r.Pos }

// End mirrors get end(): a nil pos yields nil (undefined + length is
// undefined).
func (r *Range) End() *int {
	if r.Pos == nil {
		return nil
	}
	e := *r.Pos + r.Length
	return &e
}

// Equals mirrors equals(). undefined === anything is false.
func (r *Range) Equals(other *Range) bool {
	if r.Pos == nil || other.Pos == nil {
		return false
	}
	return *r.Pos == *other.Pos && r.Length == other.Length
}

// StartsAfter mirrors startsAfter(): this.start >= range.end.
func (r *Range) StartsAfter(other *Range) bool {
	if r.Pos == nil || other.Pos == nil {
		return false
	}
	return *r.Pos >= *other.Pos+other.Length
}

// StartIsAfter mirrors startIsAfter(pos): this.start > pos.
func (r *Range) StartIsAfter(pos *int) bool {
	if r.Pos == nil || pos == nil {
		return false
	}
	return *r.Pos > *pos
}

// IsEmpty mirrors isEmpty().
func (r *Range) IsEmpty() bool { return r.Length == 0 }

// Contains mirrors contains(): this.start <= range.start && this.end >= range.end.
func (r *Range) Contains(other *Range) bool {
	if r.Pos == nil || other.Pos == nil {
		return false
	}
	return *r.Pos <= *other.Pos && *r.Pos+r.Length >= *other.Pos+other.Length
}

// ContainsCursor mirrors containsCursor(): this.start <= cursor && this.end >= cursor.
func (r *Range) ContainsCursor(cursor *int) bool {
	if r.Pos == nil || cursor == nil {
		return false
	}
	return *r.Pos <= *cursor && *r.Pos+r.Length >= *cursor
}

// Overlaps mirrors overlaps(): at least one character in common.
func (r *Range) Overlaps(other *Range) bool {
	if r.Pos == nil || other.Pos == nil {
		return false
	}
	return *r.Pos < *other.Pos+other.Length && *r.Pos+r.Length > *other.Pos
}

// OverlapsStart mirrors overlapsStart().
func (r *Range) OverlapsStart(other *Range) bool {
	if r.Pos == nil || other.Pos == nil {
		return false
	}
	return *r.Pos <= *other.Pos && *r.Pos+r.Length > *other.Pos
}

// OverlapsEnd mirrors overlapsEnd().
func (r *Range) OverlapsEnd(other *Range) bool {
	if r.Pos == nil || other.Pos == nil {
		return false
	}
	return *r.Pos < *other.Pos+other.Length && *r.Pos+r.Length >= *other.Pos
}

// Touches mirrors touches().
func (r *Range) Touches(other *Range) bool {
	if r.Pos == nil || other.Pos == nil {
		return false
	}
	return *r.Pos+r.Length == *other.Pos || *r.Pos == *other.Pos+other.Length
}

// Subtract mirrors subtract().
func (r *Range) Subtract(other *Range) (*Range, error) {
	if r.Contains(other) {
		return r.ShrinkBy(other.Length)
	}
	if other.Contains(r) {
		return &Range{Pos: r.Pos, Length: 0}, nil
	}
	if r.Overlaps(other) {
		if *other.Pos < *r.Pos {
			intersectedLength := *other.Pos + other.Length - *r.Pos
			start := *other.Pos
			return &Range{Pos: &start, Length: r.Length - intersectedLength}, nil
		}
		intersectedLength := *r.Pos + r.Length - *other.Pos
		return &Range{Pos: r.Pos, Length: r.Length - intersectedLength}, nil
	}
	return &Range{Pos: r.Pos, Length: r.Length}, nil
}

// CanMerge mirrors canMerge().
func (r *Range) CanMerge(other *Range) bool {
	return r.Overlaps(other) || r.Touches(other)
}

// Merge mirrors merge(): throws "Ranges cannot be merged" if not mergeable.
func (r *Range) Merge(other *Range) (*Range, error) {
	if !r.CanMerge(other) {
		return nil, fmt.Errorf("Ranges cannot be merged")
	}
	newPos := *r.Pos
	if *other.Pos < newPos {
		newPos = *other.Pos
	}
	newEnd := *r.Pos + r.Length
	if otherEnd := *other.Pos + other.Length; otherEnd > newEnd {
		newEnd = otherEnd
	}
	return &Range{Pos: &newPos, Length: newEnd - newPos}, nil
}

// MoveBy mirrors moveBy(). A nil pos stays nil (undefined + length is
// undefined).
func (r *Range) MoveBy(length int) *Range {
	if r.Pos == nil {
		return &Range{Pos: nil, Length: r.Length}
	}
	p := *r.Pos + length
	return &Range{Pos: &p, Length: r.Length}
}

// ExtendBy mirrors extendBy(). A nil pos stays nil.
func (r *Range) ExtendBy(extensionLength int) *Range {
	return &Range{Pos: r.Pos, Length: r.Length + extensionLength}
}

// ShrinkBy mirrors shrinkBy().
func (r *Range) ShrinkBy(shrinkLength int) (*Range, error) {
	newLength := r.Length - shrinkLength
	if newLength < 0 {
		return nil, fmt.Errorf("Cannot shrink range by more than its length")
	}
	return &Range{Pos: r.Pos, Length: newLength}, nil
}

// InsertAt mirrors insertAt(): splits a range on the cursor with the given
// insertion length. Returns (rangeUpToCursor, insertedRange,
// rangeAfterCursor):
//
//	rangeUpToCursor  = Range(pos, cursor - pos)
//	insertedRange     = Range(cursor, length)
//	rangeAfterCursor = Range(cursor + length, range.length - (cursor - pos))
func (r *Range) InsertAt(cursor *int, length int) (a, b, c *Range, err error) {
	if !r.ContainsCursor(cursor) {
		return nil, nil, nil, fmt.Errorf("The cursor must be contained in the range")
	}
	a = &Range{Pos: r.Pos, Length: *cursor - *r.Pos}
	b = &Range{Pos: cursor, Length: length}
	afterPos := *cursor + length
	c = &Range{Pos: &afterPos, Length: r.Length - (*cursor - *r.Pos)}
	return a, b, c, nil
}

// SplitAt mirrors splitAt(): splits a range at the cursor into two ranges.
// Returns (rangeUpToCursor, rangeAfterCursor).
func (r *Range) SplitAt(cursor *int) (a, b *Range, err error) {
	if !r.ContainsCursor(cursor) {
		return nil, nil, fmt.Errorf("The cursor must be contained in the range")
	}
	a = &Range{Pos: r.Pos, Length: *cursor - *r.Pos}
	afterPos := *cursor
	b = &Range{Pos: &afterPos, Length: *r.Pos + r.Length - *cursor}
	return a, b, nil
}

// Intersect mirrors intersect(): the intersection, or nil when empty.
func (r *Range) Intersect(other *Range) *Range {
	if r.Contains(other) {
		return other
	}
	if other.Contains(r) {
		return r
	}
	if other.OverlapsStart(r) {
		length := *other.Pos + other.Length - *r.Pos
		return &Range{Pos: r.Pos, Length: length}
	}
	if other.OverlapsEnd(r) {
		length := *r.Pos + r.Length - *other.Pos
		return &Range{Pos: other.Pos, Length: length}
	}
	return nil
}

// ToRaw mirrors toRaw(). A nil Pos is omitted (JS undefined is absent in
// JSON), matching dataset cases 23/26 where the first range loses its pos.
func (r *Range) ToRaw() map[string]any {
	if r.Pos == nil {
		return map[string]any{"length": r.Length}
	}
	return map[string]any{"pos": *r.Pos, "length": r.Length}
}
