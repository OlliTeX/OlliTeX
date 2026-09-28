package opmodel

// TrackedChange ports vendor lib/file_data/tracked_change.js.
//
// Tracking is stored as a value so TrackedChange stays a plain struct
// (no pointer indirection); *Tracking methods are reached via &field.

// TrackedChange — vendor `class TrackedChange { range, tracking }`.
type TrackedChange struct {
	Range    Range
	Tracking Tracking
}

// TrackedChangeFromRaw — vendor `TrackedChange.fromRaw(raw)` (wire:
// {range: {pos, length}, tracking: {type, userId, ts}}).
func TrackedChangeFromRaw(raw map[string]any) (TrackedChange, error) {
	rng, err := rangeFromAny(raw["range"])
	if err != nil {
		return TrackedChange{}, NewRangeError("Invalid tracked change range", nil)
	}
	tr := DecodeDirective(raw["tracking"])
	if tr == nil {
		return TrackedChange{}, NewRangeError("Invalid tracked change tracking", nil)
	}
	return TrackedChange{Range: rng, Tracking: *tr}, nil
}

// ToRaw — vendor `toRaw()`: {range: {pos, length}, tracking: {...}}.
func (tc TrackedChange) ToRaw() map[string]any {
	return map[string]any{
		"range":    tc.Range.ToRaw(),
		"tracking": tc.Tracking.ToRaw(),
	}
}

// CanMerge — vendor `canMerge(other)`: tracking compatible AND ranges touch
// AND ranges can merge.
func (tc TrackedChange) CanMerge(other TrackedChange) bool {
	return tc.Tracking.CanMergeWith(&other.Tracking) &&
		tc.Range.Touches(other.Range) &&
		tc.Range.CanMerge(other.Range)
}

// Merge — vendor `merge(other)`: throws 'Cannot merge tracked changes'.
func (tc TrackedChange) Merge(other TrackedChange) (TrackedChange, error) {
	if !tc.CanMerge(other) {
		return TrackedChange{}, NewRangeError("Cannot merge tracked changes", nil)
	}
	r, err := tc.Range.Merge(other.Range)
	if err != nil {
		return TrackedChange{}, err
	}
	merged := tc.Tracking.MergeWith(&other.Tracking)
	return TrackedChange{Range: r, Tracking: *merged}, nil
}

// IntersectRange — vendor `intersectRange(range)`: the equivalent tracked
// change clipped to `range`, or nil if the intersection is empty.
func (tc TrackedChange) IntersectRange(r Range) *TrackedChange {
	intersection := tc.Range.Intersect(r)
	if intersection == nil {
		return nil
	}
	return &TrackedChange{Range: *intersection, Tracking: tc.Tracking}
}
