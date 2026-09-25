// Package historyconversions — 1:1 Go port of `app/js/HistoryConversions.js`.
//
// Translates between the editor's view of a doc (lines + editor-position
// ranges, i.e. ShareJS-style ranges maintained by the RangesTracker) and the
// history-OT raw form (content including tracked-delete bytes, with tracked
// changes and comments in history positions).
package historyconversions

import (
	"strings"

	"ollitex/go/libraries/otc"
	"ollitex/go/libraries/rangestracker"

	"document-updater/internal/utils"
)

// Ranges mirrors Node `Ranges` — editor-side view of a single doc.
type Ranges struct {
	Changes  []utils.TrackedChange
	Comments []Comment
}

// Comment mirrors Node `Comment` — the editor-side view of a comment range.
// Op has {p, c, t}; id is the ShareJS op id; metadata is preserved.
type Comment struct {
	ID       string
	Op       CommentOp
	Metadata *utils.Metadata
}

// CommentOp mirrors Node comment op {p, c, t} (plus optional hpos/hlen if it
// has been enriched by ToHistoryRanges).
type CommentOp struct {
	P    int
	C    string
	T    string
	Hpos *int
	Hlen *int
}

// HistoryRanges mirrors Node `HistoryRanges` (output of ToHistoryRanges).
type HistoryRanges struct {
	Changes  []HistoryTrackedChange
	Comments []HistoryComment
}

// HistoryTrackedChange mirrors Node `HistoryTrackedChange`.
type HistoryTrackedChange struct {
	ID       *string
	Op       HistoryOp
	Metadata *utils.Metadata
}

// HistoryComment mirrors Node `HistoryComment`.
type HistoryComment struct {
	ID       string
	Op       HistoryOp
	Metadata *utils.Metadata
}

// HistoryOp mirrors Node op after ToHistoryRanges enrichment: same editor-side
// fields plus optional hpos/hlen (history-side position/length).
type HistoryOp struct {
	P    int
	I    *string
	D    *string
	C    string
	T    string
	Hpos *int
	Hlen *int
}

// FromHistoryOTResult mirrors Node `fromHistoryOT(raw)` return shape:
// editor-filtered lines + editor-position ranges (no range metadata).
type FromHistoryOTResult struct {
	Lines    []string
	Changes  []EditorTrackedChange
	Comments []EditorComment
}

// EditorTrackedChange mirrors Node `{id, op: {p, i|d}, metadata: {user_id, ts}}`.
type EditorTrackedChange struct {
	ID     string
	P      int
	Insert *string
	Delete *string
	UserID string
	TS     string
}

// EditorComment mirrors Node comment range `{id, op: {p, c, t, resolved}}`.
type EditorComment struct {
	ID       string
	P        int
	C        string
	T        string
	Resolved bool
}

// commentsIterator mirrors the Node class: yields comments strictly before a
// position (with an open-ended default), in order.
type commentsIterator struct {
	comments   []Comment
	currentIdx int
}

// nextComments returns the comments with op.p < beforePos, moving the cursor
// forward and stopping at the first one that does not qualify.
func (it *commentsIterator) nextComments(beforePos int) []Comment {
	out := []Comment{}
	for it.currentIdx < len(it.comments) {
		c := it.comments[it.currentIdx]
		if c.Op.P < beforePos {
			out = append(out, c)
			it.currentIdx++
		} else {
			return out
		}
	}
	return out
}

func toHistoryChange(c utils.TrackedChange, offset int) HistoryTrackedChange {
	out := HistoryTrackedChange{ID: c.ID, Metadata: c.Metadata}
	out.Op.P = c.Op.P
	if c.Op.I != nil {
		out.Op.I = c.Op.I
	}
	if c.Op.D != nil {
		out.Op.D = c.Op.D
	}
	if offset > 0 {
		hpos := c.Op.P + offset
		out.Op.Hpos = &hpos
	}
	return out
}

func toHistoryComment(c Comment, offset int) HistoryComment {
	out := HistoryComment{ID: c.ID, Metadata: c.Metadata}
	out.Op.P = c.Op.P
	out.Op.C = c.Op.C
	out.Op.T = c.Op.T
	if offset > 0 {
		hpos := c.Op.P + offset
		out.Op.Hpos = &hpos
	}
	return out
}

// ToHistoryRanges mirrors Node `toHistoryRanges` — editor-position ranges
// become history-position ranges; comments are shifted by cumulative
// tracked-delete length and their length grows for every tracked delete they
// overlap.
func ToHistoryRanges(r Ranges) HistoryRanges {
	// Node: `ranges.comments ?? []` then `.slice()` — copy.
	comments := make([]Comment, len(r.Comments))
	copy(comments, r.Comments)

	// Changes are assumed sorted; comments are NOT.
	sortByP(comments)

	it := &commentsIterator{comments: comments}
	offset := 0
	var pending []HistoryComment
	historyChanges := make([]HistoryTrackedChange, 0, len(r.Changes))
	historyComments := []HistoryComment{}

	for _, change := range r.Changes {
		historyChanges = append(historyChanges, toHistoryChange(change, offset))

		if !utils.IsDelete(change.Op) {
			continue
		}

		for _, c := range it.nextComments(change.Op.P) {
			pending = append(pending, toHistoryComment(c, offset))
		}

		newPending := []HistoryComment{}
		for _, hc := range pending {
			commentEnd := hc.Op.P + len(hc.Op.C)
			if commentEnd <= change.Op.P {
				historyComments = append(historyComments, hc)
			} else {
				newPending = append(newPending, hc)
			}
		}
		pending = newPending

		deleteLen := 0
		if change.Op.D != nil {
			deleteLen = len(*change.Op.D)
		}
		for i := range pending {
			base := len(pending[i].Op.C)
			if pending[i].Op.Hlen != nil {
				base = *pending[i].Op.Hlen
			}
			hlen := base + deleteLen
			pending[i].Op.Hlen = &hlen
		}
		if change.Op.D != nil {
			offset += len(*change.Op.D)
		}
	}
	historyComments = append(historyComments, pending...)

	// Save any comments that came after the last tracked change.
	for _, c := range it.nextComments(int(^uint(0) >> 1)) {
		historyComments = append(historyComments, toHistoryComment(c, offset))
	}

	return HistoryRanges{Changes: historyChanges, Comments: historyComments}
}

// ToHistoryOT mirrors Node `toHistoryOT`: build the raw StringFileOT doc
// (content with tracked-delete bytes re-inserted, plus raw comments and tracked
// changes in history positions).
func ToHistoryOT(lines []string, r Ranges, resolvedCommentIDs []string) map[string]any {
	historyRanges := ToHistoryRanges(r)

	raw := map[string]any{
		"content": utils.AddTrackedDeletesToContent(strings.Join(lines, "\n"), r.Changes),
	}

	resolved := map[string]bool{}
	for _, id := range resolvedCommentIDs {
		resolved[id] = true
	}

	var commentsOut []map[string]any
	for _, hc := range historyRanges.Comments {
		length := 0
		if hc.Op.Hlen != nil {
			length = *hc.Op.Hlen
		} else {
			length = len(hc.Op.C)
		}
		var rawComment map[string]any
		if length > 0 {
			pos := 0
			if hc.Op.Hpos != nil {
				pos = *hc.Op.Hpos
			} else {
				pos = hc.Op.P
			}
			rawComment = map[string]any{
				"id":     hc.Op.T,
				"ranges": []any{map[string]any{"pos": pos, "length": length}},
			}
		} else {
			rawComment = map[string]any{
				"id":     hc.Op.T,
				"ranges": map[string]any{},
			}
		}
		if resolved[hc.Op.T] {
			rawComment["resolved"] = true
		}
		commentsOut = append(commentsOut, rawComment)
	}
	if len(commentsOut) > 0 {
		raw["comments"] = commentsOut
	}

	var trackedChangesOut []map[string]any
	for _, hc := range historyRanges.Changes {
		pos := hc.Op.P
		if hc.Op.Hpos != nil {
			pos = *hc.Op.Hpos
		}
		length := 0
		trackingType := "insert"
		if hc.Op.D != nil {
			length = len(*hc.Op.D)
			trackingType = "delete"
		} else {
			size, _ := stringSize(hc.Op.I)
			length = size
		}
		userID := ""
		ts := ""
		if hc.Metadata != nil {
			userID = hc.Metadata.UserID
			ts = hc.Metadata.TS
		}
		trackedChangesOut = append(trackedChangesOut, map[string]any{
			"range":    map[string]any{"pos": pos, "length": length},
			"tracking": map[string]any{"type": trackingType, "userId": userID, "ts": ts},
		})
	}
	if len(trackedChangesOut) > 0 {
		raw["trackedChanges"] = trackedChangesOut
	}

	return raw
}

// FromHistoryOT mirrors Node `fromHistoryOT`: parse a raw (StringFileData)
// history-OT doc, translate it to editor-position ranges, and expose
// editor-filtered lines.
func FromHistoryOT(raw map[string]any) (FromHistoryOTResult, error) {
	fd, err := otc.FromRawStringFileData(raw)
	if err != nil {
		return FromHistoryOTResult{}, err
	}
	file := otc.NewFile(fd, map[string]any{})
	du, err := otc.GetDocUpdaterCompatibleRanges(file)
	if err != nil {
		return FromHistoryOTResult{}, err
	}

	out := FromHistoryOTResult{Lines: fd.GetLines()}

	// Node: `changes.map(change => ({...change, id: RangesTracker.generateId()}))`.
	out.Changes = make([]EditorTrackedChange, 0, len(du.Changes))
	for _, c := range du.Changes {
		out.Changes = append(out.Changes, EditorTrackedChange{
			ID:     rangestracker.GenerateId(),
			P:      c.Op.P,
			Insert: c.Op.Insert,
			Delete: c.Op.Delete,
			UserID: c.Metadata.UserID,
			TS:     c.Metadata.TS,
		})
	}
	for _, c := range du.Comments {
		out.Comments = append(out.Comments, EditorComment{
			ID:       c.ID,
			P:        c.Op.P,
			C:        c.Op.C,
			T:        c.Op.T,
			Resolved: c.Op.Resolved,
		})
	}
	return out, nil
}

// stringSize splits on *string to make intent explicit (and to satisfy the
// linter in the rare nil case).
func stringSize(s *string) (size int, ok bool) {
	if s == nil {
		return 0, false
	}
	return len(*s), true
}

func sortByP(comments []Comment) {
	for i := 1; i < len(comments); i++ {
		for j := i; j > 0 && comments[j].Op.P < comments[j-1].Op.P; j-- {
			comments[j], comments[j-1] = comments[j-1], comments[j]
		}
	}
}
