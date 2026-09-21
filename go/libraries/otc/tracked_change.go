package otc

import "fmt"

// TrackedChange is a tracked change over a range with a tracking directive
// (Node: file_data/tracked_change.js). The directive is normally a TrackingProps
// (insert/delete) but can be a ClearTrackingProps when a directive clears.
type TrackedChange struct {
	Range    Range
	Tracking TrackingDirective
}

// NewTrackedChange builds a TrackedChange (Node: `new TrackedChange(range, tracking)`).
func NewTrackedChange(r Range, tracking TrackingDirective) TrackedChange {
	return TrackedChange{Range: r, Tracking: tracking}
}

// FromRawTrackedChange builds a TrackedChange from raw (Node: `TrackedChange.fromRaw`).
func FromRawTrackedChange(raw map[string]any) (TrackedChange, error) {
	rangeRaw := asRawMap(raw["range"])
	pos, _ := numberToInt(rangeRaw["pos"])
	length, _ := numberToInt(rangeRaw["length"])
	trackingRaw := asRawMap(raw["tracking"])
	tp, err := FromRawTrackingProps(trackingRaw)
	if err != nil {
		return TrackedChange{}, err
	}
	return NewTrackedChange(NewRange(pos, length), tp), nil
}

func numberToInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// ToRaw serialises {range, tracking} (Node: `toRaw`).
func (c TrackedChange) ToRaw() map[string]any {
	return map[string]any{"range": c.Range.ToRaw(), "tracking": c.Tracking.ToRaw()}
}

// AsTrackingProps returns the tracking directive as a TrackingProps, if it is one.
func (c TrackedChange) AsTrackingProps() (TrackingProps, bool) {
	return asTrackingProps(c.Tracking)
}

// CanMerge reports mergeability (Node: `canMerge`).
func (c TrackedChange) CanMerge(other TrackedChange) bool {
	return c.Tracking.CanMergeWith(other.Tracking) &&
		c.Range.Touches(other.Range) &&
		c.Range.CanMerge(other.Range)
}

// Merge returns the merged tracked change (Node: `merge`).
func (c TrackedChange) Merge(other TrackedChange) (TrackedChange, error) {
	if !c.CanMerge(other) {
		return TrackedChange{}, fmt.Errorf("Cannot merge tracked changes")
	}
	mergedTracking, err := c.Tracking.MergeWith(other.Tracking)
	if err != nil {
		return TrackedChange{}, err
	}
	return NewTrackedChange(c.Range.Merge(other.Range), mergedTracking), nil
}

// IntersectRange returns the portion of c inside range, or nil (Node: `intersectRange`).
func (c TrackedChange) IntersectRange(r Range) *TrackedChange {
	intersection := c.Range.Intersect(r)
	if intersection == nil {
		return nil
	}
	tc := NewTrackedChange(*intersection, c.Tracking)
	return &tc
}
