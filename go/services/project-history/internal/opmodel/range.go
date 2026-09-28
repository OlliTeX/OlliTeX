package opmodel

// Range ports editor-core lib/range.js (`Range`): a [pos, pos+length) range
// in UTF-16 code units. Vendor throws on invalid args; the Go mirror returns
// a *RangeError (info: {pos, length} for the constructor, nil for the
// method-level throws).

// Range — [Pos, Pos+Length).
type Range struct {
	Pos    int
	Length int
}

// NewRange ports the `Range` constructor (OError 'Invalid range').
func NewRange(pos, length int) (Range, error) {
	if pos < 0 || length < 0 {
		return Range{}, NewRangeError("Invalid range", map[string]any{"pos": pos, "length": length})
	}
	return Range{Pos: pos, Length: length}, nil
}

// Start — vendor getter `start`.
func (r Range) Start() int { return r.Pos }

// End — vendor getter `end`.
func (r Range) End() int { return r.Pos + r.Length }

// Equals — vendor `equals`.
func (r Range) Equals(o Range) bool {
	return r.Pos == o.Pos && r.Length == o.Length
}

// StartsAfter — vendor `startsAfter`.
func (r Range) StartsAfter(o Range) bool { return r.Start() >= o.End() }

// StartIsAfter — vendor `startIsAfter`.
func (r Range) StartIsAfter(pos int) bool { return r.Start() > pos }

// IsEmpty — vendor `isEmpty`.
func (r Range) IsEmpty() bool { return r.Length == 0 }

// Contains — vendor `contains`.
func (r Range) Contains(o Range) bool {
	return r.Start() <= o.Start() && r.End() >= o.End()
}

// ContainsCursor — vendor `containsCursor`.
func (r Range) ContainsCursor(cursor int) bool {
	return r.Start() <= cursor && r.End() >= cursor
}

// Overlaps — vendor `overlaps`.
func (r Range) Overlaps(o Range) bool {
	return r.Start() < o.End() && r.End() > o.Start()
}

// OverlapsStart — vendor `overlapsStart`.
func (r Range) OverlapsStart(o Range) bool {
	return r.Start() <= o.Start() && r.End() > o.Start()
}

// OverlapsEnd — vendor `overlapsEnd`.
func (r Range) OverlapsEnd(o Range) bool {
	return r.Start() < o.End() && r.End() >= o.End()
}

// Touches — vendor `touches`.
func (r Range) Touches(o Range) bool {
	return r.End() == o.Start() || r.Start() == o.End()
}

// Subtract — vendor `subtract`. The overlap branch returns the range
// re-rooted at `other.Pos` (vendor quirk: `new Range(range.pos, ...)`),
// mirrored 1:1. No error paths: contains ⇒ length-safe.
func (r Range) Subtract(o Range) Range {
	switch {
	case r.Contains(o):
		return Range{Pos: r.Pos, Length: r.Length - o.Length}
	case o.Contains(r):
		return Range{Pos: r.Pos, Length: 0}
	case o.Overlaps(r):
		if o.Start() < r.Start() {
			return Range{Pos: o.Pos, Length: r.Length - (o.End() - r.Start())}
		}
		return Range{Pos: r.Pos, Length: r.Length - (r.End() - o.Start())}
	default:
		return Range{Pos: r.Pos, Length: r.Length}
	}
}

// CanMerge — vendor `canMerge`.
func (r Range) CanMerge(o Range) bool { return r.Overlaps(o) || r.Touches(o) }

// Merge — vendor `merge` (throws 'Ranges cannot be merged').
func (r Range) Merge(o Range) (Range, error) {
	if !r.CanMerge(o) {
		return Range{}, NewRangeError("Ranges cannot be merged", nil)
	}
	newPos := r.Pos
	if o.Pos < newPos {
		newPos = o.Pos
	}
	newEnd := r.End()
	if o.End() > newEnd {
		newEnd = o.End()
	}
	return Range{Pos: newPos, Length: newEnd - newPos}, nil
}

// MoveBy — vendor `moveBy`.
func (r Range) MoveBy(length int) Range { return Range{Pos: r.Pos + length, Length: r.Length} }

// ExtendBy — vendor `extendBy`.
func (r Range) ExtendBy(length int) Range { return Range{Pos: r.Pos, Length: r.Length + length} }

// ShrinkBy — vendor `shrinkBy` (throws 'Cannot shrink range by more than
// its length').
func (r Range) ShrinkBy(length int) (Range, error) {
	newLength := r.Length - length
	if newLength < 0 {
		return Range{}, NewRangeError("Cannot shrink range by more than its length", nil)
	}
	return Range{Pos: r.Pos, Length: newLength}, nil
}

// InsertAt — vendor `insertAt`: split the range at the cursor and insert a
// range of `length` there. Throws 'The cursor must be contained in the
// range'.
func (r Range) InsertAt(cursor, length int) (Range, Range, Range, error) {
	if !r.ContainsCursor(cursor) {
		return Range{}, Range{}, Range{}, NewRangeError("The cursor must be contained in the range", nil)
	}
	up := Range{Pos: r.Pos, Length: cursor - r.Pos}
	ins := Range{Pos: cursor, Length: length}
	after := Range{Pos: cursor + length, Length: r.Length - up.Length}
	return up, ins, after, nil
}

// ToRaw — vendor `toRaw`: {pos, length}.
func (r Range) ToRaw() map[string]int {
	return map[string]int{"pos": r.Pos, "length": r.Length}
}

// FromRawRange — vendor `Range.fromRaw` (constructor-validates).
func FromRawRange(raw map[string]int) (Range, error) {
	return NewRange(raw["pos"], raw["length"])
}

// SplitAt — vendor `splitAt`: [before, after]. Throws 'The cursor must be
// contained in the range'.
func (r Range) SplitAt(cursor int) (Range, Range, error) {
	if !r.ContainsCursor(cursor) {
		return Range{}, Range{}, NewRangeError("The cursor must be contained in the range", nil)
	}
	up := Range{Pos: r.Pos, Length: cursor - r.Pos}
	after := Range{Pos: cursor, Length: r.Length - up.Length}
	return up, after, nil
}

// Intersect — vendor `intersect`: the overlap, or nil if empty.
func (r Range) Intersect(o Range) *Range {
	var out Range
	if r.Contains(o) {
		out = o
	} else if o.Contains(r) {
		out = r
	} else if o.OverlapsStart(r) {
		out = Range{Pos: r.Pos, Length: o.End() - r.Start()}
	} else if o.OverlapsEnd(r) {
		out = Range{Pos: o.Pos, Length: r.End() - o.Start()}
	} else {
		return nil
	}
	return &out
}

// RangeFromWire — decode a JSON-decoded {pos, length} range object
// (numbers decoded as float64) into a Range. Used by the fromRaw paths
// where the wire values are JSON numbers, not Go ints.
func RangeFromWire(m map[string]any) (Range, error) {
	pos, ok := asFloat64(m["pos"])
	length, ok2 := asFloat64(m["length"])
	if !ok || !ok2 {
		return Range{}, NewRangeError("Invalid range", nil)
	}
	return NewRange(int(pos), int(length))
}

// rangeFromAny — accept either a JSON-decoded {pos, length} (map[string]any)
// or the in-memory ToRaw shape (map[string]int) as produced by
// Range.ToRaw / TrackedChange.ToRaw. Mirrors vendor fromRaw round-tripping
// toRaw output.
func rangeFromAny(v any) (Range, error) {
	switch m := v.(type) {
	case map[string]int:
		r, err := NewRange(m["pos"], m["length"])
		if err != nil {
			return Range{}, err
		}
		return r, nil
	case map[string]any:
		return RangeFromWire(m)
	default:
		return Range{}, NewRangeError("Invalid range", nil)
	}
}
