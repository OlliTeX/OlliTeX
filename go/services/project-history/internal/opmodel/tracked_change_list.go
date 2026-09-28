package opmodel

import (
	"sort"
)

// TrackedChangeList ports vendor lib/file_data/tracked_change_list.js.
//
// An ordered list of non-overlapping tracked changes. ApplyInsert/
// ApplyDelete/ApplyRetain rewrite the ranges as the file is edited;
// mergeRanges collapses adjacent compatible changes once per operation.
type TrackedChangeList struct {
	changes []TrackedChange
}

// TrackedChangeListFromRaw — vendor `TrackedChangeList.fromRaw(raw)`.
func TrackedChangeListFromRaw(raw []any) (*TrackedChangeList, error) {
	l := &TrackedChangeList{}
	for _, rc := range raw {
		m, ok := rc.(map[string]any)
		if !ok {
			return nil, NewUnprocessableError("Invalid tracked change raw data", nil)
		}
		tc, err := TrackedChangeFromRaw(m)
		if err != nil {
			return nil, err
		}
		l.changes = append(l.changes, tc)
	}
	return l, nil
}

// NewTrackedChangeList — vendor direct construction (internal).
func NewTrackedChangeList(changes []TrackedChange) *TrackedChangeList {
	return &TrackedChangeList{changes: changes}
}

// Len — vendor `get length()`.
func (l *TrackedChangeList) Len() int {
	if l == nil {
		return 0
	}
	return len(l.changes)
}

// AsSorted — vendor `asSorted()`: the changes sorted by range start
// (a copy; internal order is preserved).
func (l *TrackedChangeList) AsSorted() []TrackedChange {
	out := make([]TrackedChange, l.Len())
	for i := range l.changes {
		out[i] = l.changes[i]
	}
	sort.Slice(out, func(a, b int) bool {
		return out[a].Range.Start() < out[b].Range.Start()
	})
	return out
}

// InRange — vendor `inRange(range)`: changes fully contained in range.
func (l *TrackedChangeList) InRange(rng Range) []TrackedChange {
	out := []TrackedChange{}
	for i := range l.changes {
		if rng.Contains(l.changes[i].Range) {
			out = append(out, l.changes[i])
		}
	}
	return out
}

// IntersectRange — vendor `intersectRange(range)`: changes clipped to range.
func (l *TrackedChangeList) IntersectRange(rng Range) []TrackedChange {
	out := []TrackedChange{}
	for i := range l.changes {
		if inter := l.changes[i].IntersectRange(rng); inter != nil {
			out = append(out, *inter)
		}
	}
	return out
}

// PropsAtRange — vendor `propsAtRange(range)`: the tracking props of the
// first change fully containing range, or nil when none.
func (l *TrackedChangeList) PropsAtRange(rng Range) *Tracking {
	for i := range l.changes {
		if l.changes[i].Range.Contains(rng) {
			t := l.changes[i].Tracking
			return &t
		}
	}
	return nil
}

// RemoveInRange — vendor `removeInRange(range)`: drop changes fully inside.
func (l *TrackedChangeList) RemoveInRange(rng Range) {
	out := []TrackedChange{}
	for i := range l.changes {
		if !rng.Contains(l.changes[i].Range) {
			out = append(out, l.changes[i])
		}
	}
	l.changes = out
}

// Add — vendor `add(trackedChange)`: append + merge ranges.
func (l *TrackedChangeList) Add(tc TrackedChange) {
	l.changes = append(l.changes, tc)
	l.mergeRanges()
}

// ToRaw — vendor `toRaw()`.
func (l *TrackedChangeList) ToRaw() []map[string]any {
	out := make([]map[string]any, 0, l.Len())
	for i := range l.changes {
		out = append(out, l.changes[i].ToRaw())
	}
	return out
}

// mergeRanges — vendor `_mergeRanges()`: sort by start, then collapse
// adjacent compatible changes (errors per vendor).
func (l *TrackedChangeList) mergeRanges() {
	if len(l.changes) < 2 {
		return
	}
	// ranges are non-overlapping so we sort based on their first indices.
	sort.Slice(l.changes, func(a, b int) bool {
		return l.changes[a].Range.Start() < l.changes[b].Range.Start()
	})
	newChanges := []TrackedChange{l.changes[0]}
	for i := 1; i < len(l.changes); i++ {
		last := newChanges[len(newChanges)-1]
		current := l.changes[i]
		if last.Range.Overlaps(current.Range) {
			panic("Ranges cannot overlap")
		}
		if current.Range.IsEmpty() {
			panic("Tracked changes range cannot be empty")
		}
		if last.CanMerge(current) {
			m, err := last.Merge(current)
			if err != nil {
				panic(err)
			}
			newChanges[len(newChanges)-1] = m
		} else {
			newChanges = append(newChanges, current)
		}
	}
	l.changes = newChanges
}

// ApplyInsert — vendor `applyInsert(cursor, insertedText, {tracking})`.
func (l *TrackedChangeList) ApplyInsert(cursor int, insertedText string, tracking *Tracking) error {
	l.applyInsert(cursor, insertedText, tracking)
	l.mergeRanges()
	return nil
}

// applyInsert — the private (no-merge) version.
func (l *TrackedChangeList) applyInsert(cursor int, insertedText string, tracking *Tracking) {
	length := Units(insertedText)
	newChanges := []TrackedChange{}
	for i := range l.changes {
		tc := l.changes[i]
		if tc.Range.StartIsAfter(cursor) || cursor == tc.Range.Start() {
			// insertion before or at the start: move the change.
			newChanges = append(newChanges, TrackedChange{
				Range:    tc.Range.MoveBy(length),
				Tracking: tc.Tracking,
			})
		} else if cursor == tc.Range.End() {
			// insertion at the end: don't move.
			newChanges = append(newChanges, tc)
		} else if tc.Range.ContainsCursor(cursor) {
			// insertion inside: split (the middle is added by the op's
			// own tracking, not here).
			first, _, after, err := tc.Range.InsertAt(cursor, length)
			if err != nil {
				panic(err) // unreachable: containsCursor checked
			}
			if !first.IsEmpty() {
				newChanges = append(newChanges, TrackedChange{Range: first, Tracking: tc.Tracking})
			}
			if !after.IsEmpty() {
				newChanges = append(newChanges, TrackedChange{Range: after, Tracking: tc.Tracking})
			}
		} else {
			newChanges = append(newChanges, tc)
		}
	}
	if tracking != nil {
		// this is a new tracked change
		newChanges = append(newChanges, TrackedChange{
			Range:    Range{Pos: cursor, Length: length},
			Tracking: *tracking,
		})
	}
	l.changes = newChanges
}

// ApplyDelete — vendor `applyDelete(cursor, length)`.
func (l *TrackedChangeList) ApplyDelete(cursor, length int) error {
	l.applyDelete(cursor, length)
	l.mergeRanges()
	return nil
}

// applyDelete — the private (no-merge) version.
func (l *TrackedChangeList) applyDelete(cursor, length int) {
	newChanges := []TrackedChange{}
	deletedRange := Range{Pos: cursor, Length: length}
	for i := range l.changes {
		tc := l.changes[i]
		if deletedRange.Contains(tc.Range) {
			// fully contained: removed.
			continue
		} else if deletedRange.Overlaps(tc.Range) {
			newRange := tc.Range.Subtract(deletedRange)
			if !newRange.IsEmpty() {
				newChanges = append(newChanges, TrackedChange{Range: newRange, Tracking: tc.Tracking})
			}
		} else if tc.Range.StartIsAfter(cursor) {
			newChanges = append(newChanges, TrackedChange{
				Range:    tc.Range.MoveBy(-length),
				Tracking: tc.Tracking,
			})
		} else {
			newChanges = append(newChanges, tc)
		}
	}
	l.changes = newChanges
}

// ApplyRetain — vendor `applyRetain(cursor, length, {tracking})`.
func (l *TrackedChangeList) ApplyRetain(cursor, length int, tracking *Tracking) error {
	l.applyRetain(cursor, length, tracking)
	l.mergeRanges()
	return nil
}

// applyRetain — the private (no-merge) version.
func (l *TrackedChangeList) applyRetain(cursor, length int, tracking *Tracking) {
	// if there's no tracking info, leave everything as-is
	if tracking == nil {
		return
	}
	newChanges := []TrackedChange{}
	retainedRange := Range{Pos: cursor, Length: length}
	for i := range l.changes {
		tc := l.changes[i]
		if retainedRange.Contains(tc.Range) {
			// fully contained: removed.
		} else if retainedRange.Overlaps(tc.Range) {
			if tc.Range.Contains(retainedRange) {
				left, right, err := tc.Range.SplitAt(cursor)
				if err != nil {
					panic(err) // unreachable: overlaps checked
				}
				if !left.IsEmpty() {
					newChanges = append(newChanges, TrackedChange{Range: left, Tracking: tc.Tracking})
				}
				if !right.IsEmpty() && right.Length > length {
					moved := right.MoveBy(length)
					shrunk, err := moved.ShrinkBy(length)
					if err == nil && !shrunk.IsEmpty() {
						newChanges = append(newChanges, TrackedChange{Range: shrunk, Tracking: tc.Tracking})
					}
				}
			} else if retainedRange.Start() <= tc.Range.Start() {
				// overlaps to the left
				_, reduced, err := tc.Range.SplitAt(retainedRange.End())
				if err != nil {
					panic(err) // unreachable: overlaps checked
				}
				if !reduced.IsEmpty() {
					newChanges = append(newChanges, TrackedChange{Range: reduced, Tracking: tc.Tracking})
				}
			} else {
				// overlaps to the right
				reduced, _, err := tc.Range.SplitAt(cursor)
				if err != nil {
					panic(err) // unreachable: overlaps checked
				}
				if !reduced.IsEmpty() {
					newChanges = append(newChanges, TrackedChange{Range: reduced, Tracking: tc.Tracking})
				}
			}
		} else {
			// keep the range
			newChanges = append(newChanges, tc)
		}
	}
	// a 'none' (clear) directive does not create a tracked change; vendor
	// checks `opts.tracking instanceof TrackingProps`.
	if tracking.Type != "none" {
		newChanges = append(newChanges, TrackedChange{
			Range:    retainedRange,
			Tracking: *tracking,
		})
	}
	l.changes = newChanges
}

// ApplyTextOperation — vendor `applyTextOperation(operation)`: replay the op
// list (the cursor tracks the destination document: inserts and removes
// move it, retains advance it). Ranges are merged once at the end.
func (l *TrackedChangeList) ApplyTextOperation(ops []ScanOp) error {
	cursor := 0
	for _, op := range ops {
		switch s := op.(type) {
		case RetainOp:
			l.applyRetain(cursor, s.Length, s.Tracking)
			cursor += s.Length
		case InsertOp:
			l.applyInsert(cursor, s.Insertion, s.Tracking)
			cursor += Units(s.Insertion)
		case RemoveOp:
			l.applyDelete(cursor, s.Length)
			// cursor does NOT advance (vendor: it tracks the destination document)
		}
	}
	l.mergeRanges()
	return nil
}
