package core

import "encoding/json"

// TrackedChange — apply state (range + tracking props).
type TrackedChange struct {
	Range    Range
	Tracking *TrackingProps
}

// TrackedChangeList — apply state over tracked changes.
type TrackedChangeList []*TrackedChange

// FromRaw ports TrackedChangeList.fromRaw: [{range:{pos,length},tracking{...}}].
func TrackedChangeListFromRaw(raws []json.RawMessage) TrackedChangeList {
	var l TrackedChangeList
	for i := range raws {
		var probe struct {
			RawRange    json.RawMessage `json:"range"`
			TrackingRaw json.RawMessage `json:"tracking"`
		}
		_ = json.Unmarshal(raws[i], &probe)
		tc := &TrackedChange{}
		if r, err := RangeFromRaw(probe.RawRange); err == nil {
			tc.Range = r
		}
		t, err := TrackingPropsFromRaw(probe.TrackingRaw)
		if err == nil {
			tc.Tracking = t
		}
		l = append(l, tc)
	}
	return l
}

func (t *TrackedChange) canMerge(o *TrackedChange) bool {
	return t.Range.Touches(o.Range) && t.Range.CanMerge(o.Range) && t.Tracking.CanMergeWith(o.Tracking)
}

func (t *TrackedChange) merge(o *TrackedChange) *TrackedChange {
	newPos := t.Range.Pos
	if o.Range.Pos < newPos {
		newPos = o.Range.Pos
	}
	end := t.Range.Pos + t.Range.Length
	if oEnd := o.Range.Pos + o.Range.Length; oEnd > end {
		end = oEnd
	}
	return &TrackedChange{
		Range:    Range{Pos: newPos, Length: end - newPos},
		Tracking: t.Tracking.MergeWith(o.Tracking),
	}
}

// mergeRanges ports TrackedChangeList._mergeRanges: sort, drop empties... (Node
// does NOT drop empties on tracked; only comments do). Then merge
// adjacent/compatible.
func (l TrackedChangeList) mergeRanges() TrackedChangeList {
	if len(l) < 2 {
		return l
	}
	sorted := make(TrackedChangeList, len(l))
	copy(sorted, l)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1].Range.Pos > sorted[j].Range.Pos; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	out := TrackedChangeList{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		c := sorted[i]
		last := out[len(out)-1]
		if c.Range.Length == 0 && last.Range.Length == 0 {
			continue
		}
		if last.canMerge(c) {
			out[len(out)-1] = last.merge(c)
			continue
		}
		out = append(out, c)
	}
	return out
}

// applyInsert ports TrackedChangeList._applyInsert (destination doc cursor at
// insert point; tracking adds a new tracked range).
func (l TrackedChangeList) applyInsert(cursor, length int, tracking *TrackingProps) TrackedChangeList {
	out := make(TrackedChangeList, 0, len(l)+1)
	for i := range l {
		tc := l[i]
		switch {
		case tc.Range.Pos > cursor || cursor == tc.Range.Pos:
			out = append(out, &TrackedChange{Range: tc.Range.MoveBy(length), Tracking: tc.Tracking})
		case cursor == tc.Range.Pos+tc.Range.Length:
			out = append(out, tc)
		case tc.Range.ContainsCursor(cursor):
			f, _, third := tc.Range.InsertAt(cursor, length)
			if f.Length > 0 {
				out = append(out, &TrackedChange{Range: f, Tracking: tc.Tracking})
			}
			if third.Length > 0 {
				out = append(out, &TrackedChange{Range: third, Tracking: tc.Tracking})
			}
		default:
			out = append(out, tc)
		}
	}
	// Node pushes a new tracked range whenever opts.tracking is truthy (a
	// Clear is a truthy object, so a clear tracked insert is also recorded).
	if tracking != nil {
		out = append(out, &TrackedChange{Range: Range{Pos: cursor, Length: length}, Tracking: tracking})
	}
	return out
}

func (l TrackedChangeList) applyDelete(cursor, length int) TrackedChangeList {
	dr := Range{Pos: cursor, Length: length}
	out := make(TrackedChangeList, 0, len(l))
	for i := range l {
		tc := l[i]
		switch {
		case dr.Contains(tc.Range):
		case dr.Overlaps(tc.Range):
			np := tc.Range.Subtract(dr)
			if np.Length > 0 {
				out = append(out, &TrackedChange{Range: np, Tracking: tc.Tracking})
			}
		case tc.Range.Pos > cursor:
			out = append(out, &TrackedChange{Range: tc.Range.MoveBy(-length), Tracking: tc.Tracking})
		default:
			out = append(out, tc)
		}
	}
	return out
}

func (l TrackedChangeList) applyRetain(cursor, length int, tracking *TrackingProps) TrackedChangeList {
	if tracking == nil {
		return l
	}
	rq := Range{Pos: cursor, Length: length}
	isSimple := tracking.Type == "insert" || tracking.Type == "delete"
	out := make(TrackedChangeList, 0, len(l)+1)
	for i := range l {
		tc := l[i]
		switch {
		case rq.Contains(tc.Range):
		case rq.Overlaps(tc.Range):
			if tc.Range.Contains(rq) {
				left, right := tc.Range.SplitAt(cursor)
				if !left.IsEmpty() {
					out = append(out, &TrackedChange{Range: left, Tracking: tc.Tracking})
				}
				if !right.IsEmpty() && right.Length > length {
					out = append(out, &TrackedChange{Range: right.MoveBy(length).ShrinkBy(length), Tracking: tc.Tracking})
				}
			} else if rq.Pos <= tc.Range.Pos {
				_, reduced := tc.Range.SplitAt(rq.Pos + rq.Length)
				if reduced.Length > 0 {
					out = append(out, &TrackedChange{Range: reduced, Tracking: tc.Tracking})
				}
			} else {
				reduced, _ := tc.Range.SplitAt(cursor)
				if reduced.Length > 0 {
					out = append(out, &TrackedChange{Range: reduced, Tracking: tc.Tracking})
				}
			}
		default:
			out = append(out, tc)
		}
	}
	if isSimple {
		out = append(out, &TrackedChange{Range: rq, Tracking: tracking})
	}
	return out
}

// ApplyTextOperation ports TrackedChangeList.applyTextOperation (node loop over
// ops; destination-document cursor threaded through scan ops; one merge at end).
// Cursor semantics VERIFIED: RetainOp cursor += length; InsertOp cursor +=
// insertion.length; RemoveOp does NOT advance cursor.
func (l *TrackedChangeList) ApplyTextOperation(ops []*ScanOp) {
	cursor := 0
	for _, op := range ops {
		if op.r.isInsert {
			*l = l.applyInsert(cursor, len(op.r.insertion), op.r.tracking)
			cursor += len(op.r.insertion)
		} else if op.r.isRemove {
			*l = l.applyDelete(cursor, op.r.length)
		} else {
			*l = l.applyRetain(cursor, op.r.length, op.r.tracking)
			cursor += op.r.length
		}
	}
	*l = l.mergeRanges()
}

// ToRaw — [{range:{pos,length},tracking:{...}}] (Node order: range, tracking).
func (l TrackedChangeList) ToRaw() json.RawMessage {
	out := make([]json.RawMessage, 0, len(l))
	for i := range l {
		tc := l[i]
		b, _ := json.Marshal(struct {
			Range    json.RawMessage `json:"range"`
			Tracking json.RawMessage `json:"tracking"`
		}{tc.Range.ToRaw(), tc.Tracking.ToRaw()})
		out = append(out, b)
	}
	b, _ := json.Marshal(out)
	return b
}
