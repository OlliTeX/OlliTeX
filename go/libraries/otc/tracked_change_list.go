package otc

import (
	"fmt"
	"sort"
)

// TrackedChangeList mirrors file_data/tracked_change_list.js.
type TrackedChangeList struct {
	changes []TrackedChange
}

// NewTrackedChangeList builds a list (Node: constructor).
func NewTrackedChangeList(changes []TrackedChange) *TrackedChangeList {
	return &TrackedChangeList{changes: changes}
}

// FromRawTrackedChangeList builds a list from raw (Node: `TrackedChangeList.fromRaw`).
func FromRawTrackedChangeList(raw []map[string]any) (*TrackedChangeList, error) {
	out := make([]TrackedChange, 0, len(raw))
	for _, r := range raw {
		tc, err := FromRawTrackedChange(r)
		if err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return &TrackedChangeList{changes: out}, nil
}

// FromRawTrackedChangeListAny builds a list from a raw slice typed []any
// (raw decoded from JSON is []any).
func FromRawTrackedChangeListAny(raw any) (*TrackedChangeList, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid tracked change list")
	}
	items := make([]map[string]any, 0, len(arr))
	for _, v := range arr {
		m, okM := v.(map[string]any)
		if !okM {
			return nil, fmt.Errorf("invalid tracked change")
		}
		items = append(items, m)
	}
	return FromRawTrackedChangeList(items)
}

// Changes returns a copy of the underlying slice (inspection helper).
func (l *TrackedChangeList) Changes() []TrackedChange {
	out := make([]TrackedChange, len(l.changes))
	copy(out, l.changes)
	return out
}

// Len returns the number of tracked changes (Node: `get length()`).
func (l *TrackedChangeList) Len() int { return len(l.changes) }

// ToRaw serialises the list (Node: `toRaw`).
func (l *TrackedChangeList) ToRaw() []map[string]any {
	out := make([]map[string]any, 0, len(l.changes))
	for _, c := range l.changes {
		out = append(out, c.ToRaw())
	}
	return out
}

// AsSorted returns the changes sorted by range start (Node: `asSorted`).
func (l *TrackedChangeList) AsSorted() []TrackedChange {
	sorted := make([]TrackedChange, len(l.changes))
	copy(sorted, l.changes)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Range.Start() < sorted[j].Range.Start() })
	return sorted
}

// InRange returns changes fully inside range (Node: `inRange`).
func (l *TrackedChangeList) InRange(r Range) []TrackedChange {
	out := []TrackedChange{}
	for _, c := range l.changes {
		if r.Contains(c.Range) {
			out = append(out, c)
		}
	}
	return out
}

// IntersectRange returns the intersected changes (Node: `intersectRange`).
func (l *TrackedChangeList) IntersectRange(r Range) []TrackedChange {
	out := []TrackedChange{}
	for _, c := range l.changes {
		if x := c.IntersectRange(r); x != nil {
			out = append(out, *x)
		}
	}
	return out
}

// PropsAtRange returns the tracking props covering range (Node: `propsAtRange`).
func (l *TrackedChangeList) PropsAtRange(r Range) TrackingProps {
	for _, c := range l.changes {
		if c.Range.Contains(r) {
			if tp, ok := asTrackingProps(c.Tracking); ok {
				return tp
			}
		}
	}
	return TrackingProps{}
}

// RemoveInRange drops changes fully inside range (Node: `removeInRange`).
func (l *TrackedChangeList) RemoveInRange(r Range) {
	out := []TrackedChange{}
	for _, c := range l.changes {
		if !r.Contains(c.Range) {
			out = append(out, c)
		}
	}
	l.changes = out
}

// Add appends a change and merges (Node: `add`).
func (l *TrackedChangeList) Add(tc TrackedChange) error {
	l.changes = append(l.changes, tc)
	return l.mergeRanges()
}

// mergeRanges collapses adjacent compatible ranges (Node: `_mergeRanges`).
func (l *TrackedChangeList) mergeRanges() error {
	if len(l.changes) < 2 {
		return nil
	}
	sorted := make([]TrackedChange, len(l.changes))
	copy(sorted, l.changes)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Range.Start() < sorted[j].Range.Start() })
	merged := []TrackedChange{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		last := merged[len(merged)-1]
		current := sorted[i]
		if last.Range.Overlaps(current.Range) {
			return fmt.Errorf("Ranges cannot overlap")
		}
		if current.Range.IsEmpty() {
			return fmt.Errorf("Tracked changes range cannot be empty")
		}
		if last.CanMerge(current) {
			combined, err := last.Merge(current)
			if err != nil {
				return err
			}
			merged[len(merged)-1] = combined
		} else {
			merged = append(merged, current)
		}
	}
	l.changes = merged
	return nil
}

// ApplyInsert applies an insert (Node: `applyInsert` + `_applyInsert`).
func (l *TrackedChangeList) ApplyInsert(cursor int, inserted string, opts InsertOpts) error {
	if err := l.applyInsertNoMerge(cursor, inserted, opts.tracking); err != nil {
		return err
	}
	return l.mergeRanges()
}

// InsertOpts carries the optional tracking directive for an insert.
type InsertOpts struct{ tracking TrackingDirective }

func (l *TrackedChangeList) applyInsertNoMerge(cursor int, inserted string, tracking TrackingDirective) error {
	newChanges := []TrackedChange{}
	for _, c := range l.changes {
		if c.Range.StartIsAfter(cursor) {
			newChanges = append(newChanges, NewTrackedChange(c.Range.MoveBy(len(inserted)), c.Tracking))
		} else if cursor == c.Range.Start() {
			newChanges = append(newChanges, NewTrackedChange(c.Range.MoveBy(len(inserted)), c.Tracking))
		} else if cursor == c.Range.End() {
			newChanges = append(newChanges, c)
		} else if c.Range.ContainsCursor(cursor) {
			parts := c.Range.InsertAt(cursor, len(inserted))
			if first := NewTrackedChange(parts[0], c.Tracking); !first.Range.IsEmpty() {
				newChanges = append(newChanges, first)
			}
			if third := NewTrackedChange(parts[2], c.Tracking); !third.Range.IsEmpty() {
				newChanges = append(newChanges, third)
			}
		} else {
			newChanges = append(newChanges, c)
		}
	}
	if tracking != nil {
		newChanges = append(newChanges, NewTrackedChange(NewRange(cursor, len(inserted)), tracking))
	}
	l.changes = newChanges
	return nil
}

// ApplyDelete applies a delete (Node: `applyDelete` + `_applyDelete`).
func (l *TrackedChangeList) ApplyDelete(cursor, length int) error {
	if err := l.applyDeleteNoMerge(cursor, length); err != nil {
		return err
	}
	return l.mergeRanges()
}

func (l *TrackedChangeList) applyDeleteNoMerge(cursor, length int) error {
	newChanges := []TrackedChange{}
	deleted := NewRange(cursor, length)
	for _, c := range l.changes {
		if deleted.Contains(c.Range) {
			// drop
		} else if deleted.Overlaps(c.Range) {
			newRange := c.Range.Subtract(deleted)
			if !newRange.IsEmpty() {
				newChanges = append(newChanges, NewTrackedChange(newRange, c.Tracking))
			}
		} else if c.Range.StartIsAfter(cursor) {
			newChanges = append(newChanges, NewTrackedChange(c.Range.MoveBy(-length), c.Tracking))
		} else {
			newChanges = append(newChanges, c)
		}
	}
	l.changes = newChanges
	return nil
}

// ApplyRetain applies a retain-with-tracking (Node: `applyRetain` + `_applyRetain`).
func (l *TrackedChangeList) ApplyRetain(cursor, length int, opts RetainOpts) error {
	if err := l.applyRetainNoMerge(cursor, length, opts.tracking); err != nil {
		return err
	}
	return l.mergeRanges()
}

// RetainOpts carries the optional tracking directive for a retain.
type RetainOpts struct{ tracking TrackingDirective }

func (l *TrackedChangeList) applyRetainNoMerge(cursor, length int, tracking TrackingDirective) error {
	if tracking == nil {
		return nil
	}
	newChanges := []TrackedChange{}
	retained := NewRange(cursor, length)
	for _, c := range l.changes {
		if retained.Contains(c.Range) {
			// remove
		} else if retained.Overlaps(c.Range) {
			if c.Range.Contains(retained) {
				parts := c.Range.SplitAt(cursor)
				left, right := parts[0], parts[1]
				if !left.IsEmpty() {
					newChanges = append(newChanges, NewTrackedChange(left, c.Tracking))
				}
				if !right.IsEmpty() && right.Length > length {
					newChanges = append(newChanges, NewTrackedChange(right.MoveBy(length).ShrinkBy(length), c.Tracking))
				}
			} else if retained.Start() <= c.Range.Start() {
				parts := c.Range.SplitAt(retained.End())
				reduced := parts[1]
				if !reduced.IsEmpty() {
					newChanges = append(newChanges, NewTrackedChange(reduced, c.Tracking))
				}
			} else {
				parts := c.Range.SplitAt(cursor)
				reduced := parts[0]
				if !reduced.IsEmpty() {
					newChanges = append(newChanges, NewTrackedChange(reduced, c.Tracking))
				}
			}
		} else {
			newChanges = append(newChanges, c)
		}
	}
	if tp, ok := asTrackingProps(tracking); ok {
		newChanges = append(newChanges, NewTrackedChange(retained, tp))
	}
	l.changes = newChanges
	return nil
}

// ApplyTextOperation applies a whole text operation (Node: `applyTextOperation`).
func (l *TrackedChangeList) ApplyTextOperation(op *TextOperation) error {
	cursor := 0
	for _, s := range op.Ops {
		switch t := s.(type) {
		case InsertOp:
			if err := l.applyInsertNoMerge(cursor, t.Insertion, t.Tracking); err != nil {
				return err
			}
			cursor += len(t.Insertion)
		case RemoveOp:
			if err := l.applyDeleteNoMerge(cursor, t.Length); err != nil {
				return err
			}
		case RetainOp:
			if err := l.applyRetainNoMerge(cursor, t.Length, t.Tracking); err != nil {
				return err
			}
			cursor += t.Length
		}
	}
	return l.mergeRanges()
}
