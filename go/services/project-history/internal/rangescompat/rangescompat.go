// Package rangescompat — the snapshotmanager Ranges seam:
// the editor-core `getDocUpdaterCompatibleRanges(file)` util port.
//
// Vendor source: libraries/overleaf-editor-core/lib/doc_updater_compatible_ranges.js
// (pinned from overleaf 6.1.0). This module is stdlib-only, so the util is
// modelled locally over the snapshotmanager File's RAW wire fields
// (Comments / TrackedChanges arrive as the editor-core raw arrays).
//
// Wire contracts (pinned from the vendor):
//
//	changes[i] = {
//	  op:       { p: <pos after delete-shift>, i|d: <raw slice> },
//	  metadata: { ts: <ISO string from raw>, user_id: <raw userId> },
//	}
//
//	comments[i] = {
//	  op: { p: <pos after delete-shift/truncation>, c: <surviving content>,
//	        t: <comment id>, resolved: <bool> },
//	  id: <comment id>,          // ABSENT for detached (zero-range) comments
//	}
//
// Quirks preserved (Q1–Q4 below) — the vendor's own header carries the
// worked example used by the tests:
//
//	the quic[k {b]rown [fox] jum[ps} ove]r the lazy dog => "rown  jum" at 8
//
// Q1: positions are relative to the content WITH the tracked deletions
//
//	removed (offset accumulation in changes; skip/overwrite in comments).
//
// Q2: a comment overlapping a tracked deletion is truncated by exactly that
//	overlap (vendor: "move the position left and truncate the overlap").
//
// Q3: multi-range comments are joined: start = ranges[0].start,
//	end = ranges[last].end (vendor indexes raw arrays).
//
// Q4: detached comments (zero ranges) carry NO top-level `id` key.

package rangescompat

import (
	"errors"
	"sort"
)

// Range — the raw `{pos, length}` wire shape (editor-core Range.fromRaw).
type Range struct {
	Pos    int
	Length int
}

func (r Range) Start() int { return r.Pos }
func (r Range) End() int   { return r.Pos + r.Length }

// TrackedChangeRaw — raw `{range: {pos, length}, tracking: {type, userId, ts}}`.
type TrackedChangeRaw struct {
	Range  Range
	Type   string // "insert" | "delete" | "none"
	UserID string
	Ts     string // ISO string (raw wire)
}

// CommentRaw — raw `{id, ranges: [{pos, length}], resolved}`.
type CommentRaw struct {
	ID       string
	Ranges   []Range
	Resolved bool
}

// GetDocUpdaterCompatibleRanges — vendor getDocUpdaterCompatibleRanges(file).
//
// content nil → the vendor throws 'Unable to read file contents' (E1).
// editable false → empty arrays (binary file).
func GetDocUpdaterCompatibleRanges(
	content *string,
	editable bool,
	rawComments, rawTrackedChanges []any,
) (changes []map[string]any, comments []map[string]any, err error) {
	if !editable {
		return []map[string]any{}, []map[string]any{}, nil
	}
	if content == nil {
		return nil, nil, errors.New("Unable to read file contents")
	}
	// (raw arrays arrive as []map[string]any from the JSON decode — coerce.)
	comments_ := make([]*CommentRaw, 0, len(rawComments))
	for _, rc := range rawComments {
		m, ok := rc.(map[string]any)
		if !ok {
			continue
		}
		c := &CommentRaw{ID: strOf(m["id"]), Resolved: boolOf(m["resolved"])}
		if raw, ok := m["ranges"].([]any); ok {
			for _, rr := range raw {
				rm, ok := rr.(map[string]any)
				if !ok {
					continue
				}
				c.Ranges = append(c.Ranges, Range{Pos: intOf(rm["pos"]), Length: intOf(rm["length"])})
			}
		}
		comments_ = append(comments_, c)
	}
	changes_ := make([]*TrackedChangeRaw, 0, len(rawTrackedChanges))
	for _, rc := range rawTrackedChanges {
		m, ok := rc.(map[string]any)
		if !ok {
			continue
		}
		tc := &TrackedChangeRaw{}
		if rm, ok := m["range"].(map[string]any); ok {
			tc.Range = Range{Pos: intOf(rm["pos"]), Length: intOf(rm["length"])}
		}
		if tr, ok := m["tracking"].(map[string]any); ok {
			tc.Type = strOf(tr["type"])
			tc.UserID = strOf(tr["userId"])
			tc.Ts = tsOf(tr["ts"])
		}
		changes_ = append(changes_, tc)
	}
	// asSorted() — stable sort by range start (vendor Array.prototype.sort is
	// stable since ES2019; Go sort.SliceStable mirrors that).
	sort.SliceStable(changes_, func(i, j int) bool { return changes_[i].Range.Start() < changes_[j].Range.Start() })

	out := make([]map[string]any, 0, len(changes_))
	deleted := make([]*TrackedChangeRaw, 0)
	offset := 0
	for _, tc := range changes_ {
		isDel := tc.Type == "delete"
		slice := contentSlice(*content, tc.Range.Start(), tc.Range.End())
		op := map[string]any{"p": tc.Range.Start() - offset}
		if isDel {
			op["d"] = slice
		} else {
			op["i"] = slice
		}
		out = append(out, map[string]any{
			"op": op,
			"metadata": map[string]any{
				"ts":      tc.Ts,
				"user_id": tc.UserID,
			},
		})
		if isDel {
			offset += tc.Range.Length
			deleted = append(deleted, tc)
		}
	}

	outc := make([]map[string]any, 0, len(comments_))
	for _, c := range comments_ {
		if len(c.Ranges) == 0 {
			// Q4: detached → op only, NO id key.
			outc = append(outc, map[string]any{
				"op": map[string]any{"p": 0, "c": "", "t": c.ID, "resolved": c.Resolved},
			})
			continue
		}
		start := c.Ranges[0].Start()
		end := c.Ranges[len(c.Ranges)-1].End()
		di := 0
		position := start
		// skip deletions that END before the comment starts
		for di < len(deleted) && deleted[di].Range.End() <= start {
			position -= deleted[di].Range.Length
			di++
		}
		// Q2: overlap → shift left past the overlap and truncate
		if di < len(deleted) && deleted[di].Range.Start() < start {
			position -= start - deleted[di].Range.Start()
		}
		var cbuf []byte
		cursor := start
		for cursor < end {
			if di >= len(deleted) || deleted[di].Range.Start() >= end {
				cbuf = append(cbuf, contentSlice(*content, cursor, end)...)
				break
			}
			if deleted[di].Range.Start() > cursor {
				cbuf = append(cbuf, contentSlice(*content, cursor, deleted[di].Range.Start())...)
			}
			if deleted[di].Range.End() <= end {
				cursor = deleted[di].Range.End()
				di++
			} else {
				break
			}
		}
		outc = append(outc, map[string]any{
			"op": map[string]any{"p": position, "c": string(cbuf), "t": c.ID, "resolved": c.Resolved},
			"id": c.ID,
		})
	}
	return out, outc, nil
}

func contentSlice(s string, a, b int) string {
	if a < 0 {
		a = 0
	}
	if b > len(s) {
		b = len(s)
	}
	if a >= b {
		return ""
	}
	return s[a:b]
}

func strOf(v any) string {
	s, _ := v.(string)
	return s
}

func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

func intOf(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func tsOf(v any) string {
	// the raw wire carries either an ISO string or epoch ms — mirror the
	// vendor which calls `ts.toISOString()` on the hydrated Date.
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
