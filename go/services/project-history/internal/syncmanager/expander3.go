package syncmanager

import (
	"fmt"
	"sort"
)

// commentRangesAreInSyncHistoryOT — vendor.
func commentRangesAreInSyncHistoryOT(persisted, expected map[string]any) bool {
	pr, _ := persisted["ranges"].([]any)
	er, _ := expected["ranges"].([]any)
	if len(pr) != len(er) {
		return false
	}
	for i := range pr {
		pm, _ := pr[i].(map[string]any)
		em, _ := er[i].(map[string]any)
		if pm == nil || em == nil {
			return false
		}
		pPos, _ := intAny(pm["pos"])
		ePos, _ := intAny(em["pos"])
		pLen, _ := intAny(pm["length"])
		eLen, _ := intAny(em["length"])
		if pPos != ePos || pLen != eLen {
			return false
		}
	}
	return true
}

// commentRangesAreInSync — vendor (single-range editor comment vs expected op).
func commentRangesAreInSync(persisted, expected map[string]any) bool {
	op, _ := expected["op"].(map[string]any)
	if op == nil {
		return false
	}
	expectedPos, expectedLength := 0, 0
	if h, ok := op["hpos"]; ok {
		expectedPos, _ = intAny(h)
	} else {
		expectedPos, _ = intAny(op["p"])
	}
	if h, ok := op["hlen"]; ok {
		expectedLength, _ = intAny(h)
	} else if c, ok := op["c"]; ok {
		if s, ok2 := c.(string); ok2 {
			expectedLength = len(s)
		}
	}
	if expectedLength == 0 {
		pr, _ := persisted["ranges"].([]any)
		return len(pr) == 0
	}
	pr, _ := persisted["ranges"].([]any)
	if len(pr) != 1 {
		return false
	}
	r, _ := pr[0].(map[string]any)
	if r == nil {
		return false
	}
	pPos, _ := intAny(r["pos"])
	pLen, _ := intAny(r["length"])
	return pPos == expectedPos && pLen == expectedLength
}

func commentList(raw []any) []map[string]any {
	out := []map[string]any{}
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// queueUpdatesForOutOfSyncCommentsHistoryOT — vendor.
func (e *expander) queueUpdatesForOutOfSyncCommentsHistoryOT(update map[string]any, pathname string, persistedComments []any) {
	expected, _ := update["resyncDocContent"].(map[string]any)
	expectedComments := []map[string]any{}
	if expected != nil {
		if hotr, ok := expected["historyOTRanges"].(map[string]any); ok {
			if cl, ok2 := hotr["comments"].([]any); ok2 {
				expectedComments = commentList(cl)
			}
		}
	}
	expectedById := map[string]map[string]any{}
	for _, c := range expectedComments {
		if id, ok := c["id"].(string); ok {
			expectedById[id] = c
		}
	}
	persisted := commentList(persistedComments)
	for _, p := range persisted {
		id, _ := p["id"].(string)
		if _, ok := expectedById[id]; !ok {
			e.expandedUpdates = append(e.expandedUpdates, map[string]any{
				"doc": update["doc"],
				"op":  []any{map[string]any{"deleteComment": id}},
				"meta": map[string]any{
					"pathname": pathname,
					"resync":   true,
					"origin":   e.origin,
					"ts":       metaTS(update),
				},
			})
		}
	}
	for _, x := range expectedComments {
		id, _ := x["id"].(string)
		if p, ok := persistedById(persisted, id); ok {
			if commentRangesAreInSyncHistoryOT(p, x) {
				if truthy(x["resolved"]) == truthy(p["resolved"]) {
					continue
				}
				e.expandedUpdates = append(e.expandedUpdates, map[string]any{
					"doc": update["doc"],
					"op": []any{map[string]any{
						"commentId": x["id"],
						"resolved":  x["resolved"],
					}},
					"meta": map[string]any{
						"pathname": pathname,
						"resync":   true,
						"origin":   e.origin,
						"ts":       metaTS(update),
					},
				})
				continue
			}
		}
		e.expandedUpdates = append(e.expandedUpdates, map[string]any{
			"doc": update["doc"],
			"op": []any{map[string]any{
				"commentId": x["id"],
				"ranges":    x["ranges"],
				"resolved":  x["resolved"],
			}},
			"meta": map[string]any{
				"pathname": pathname,
				"resync":   true,
				"origin":   e.origin,
				"ts":       metaTS(update),
			},
		})
	}
}

func persistedById(list []map[string]any, id string) (map[string]any, bool) {
	for _, p := range list {
		if pid, ok := p["id"].(string); ok && pid == id {
			return p, true
		}
	}
	return nil, false
}

func metaTS(update map[string]any) any {
	m, _ := update["meta"].(map[string]any)
	if m == nil {
		return nil
	}
	return m["ts"]
}

// queueUpdatesForOutOfSyncComments — vendor (non-HistoryOT shape).
func (e *expander) queueUpdatesForOutOfSyncComments(update map[string]any, pathname string, persistedComments []any) {
	expected, _ := update["resyncDocContent"].(map[string]any)
	if expected == nil {
		expected = map[string]any{}
	}
	expectedContent, _ := expected["content"].(string)
	expectedComments := []map[string]any{}
	if rg, ok := expected["ranges"].(map[string]any); ok {
		if cl, ok2 := rg["comments"].([]any); ok2 {
			expectedComments = commentList(cl)
		}
	}
	resolvedIDs := map[string]bool{}
	if rcl, ok := expected["resolvedCommentIds"].([]any); ok {
		for _, id := range rcl {
			if s, ok2 := id.(string); ok2 {
				resolvedIDs[s] = true
			}
		}
	}
	expectedById := map[string]map[string]any{}
	for _, c := range expectedComments {
		if id, ok := c["id"].(string); ok {
			expectedById[id] = c
		}
	}
	persisted := commentList(persistedComments)
	for _, p := range persisted {
		id, _ := p["id"].(string)
		if _, ok := expectedById[id]; !ok {
			e.expandedUpdates = append(e.expandedUpdates, map[string]any{
				"pathname":      pathname,
				"deleteComment": id,
				"meta":          metaResync(e, update),
			})
		}
	}
	for _, x := range expectedComments {
		id, _ := x["id"].(string)
		expectedResolved := resolvedIDs[id]
		if p, ok := persistedById(persisted, id); ok {
			if commentRangesAreInSync(p, x) {
				if expectedResolved == truthy(p["resolved"]) {
					continue
				}
				e.expandedUpdates = append(e.expandedUpdates, map[string]any{
					"pathname":  pathname,
					"commentId": id,
					"resolved":  expectedResolved,
					"meta":      metaResync(e, update),
				})
				continue
			}
		}
		op := map[string]any{"resolved": expectedResolved}
		if eop, ok := x["op"].(map[string]any); ok {
			for k, v := range eop {
				op[k] = v
			}
			op["resolved"] = expectedResolved
		}
		e.expandedUpdates = append(e.expandedUpdates, map[string]any{
			"doc": update["doc"],
			"op":  []any{op},
			"meta": map[string]any{
				"resync":     true,
				"origin":     e.origin,
				"ts":         metaTS(update),
				"pathname":   pathname,
				"doc_length": len(expectedContent),
			},
		})
	}
}

// --- tracked-change transitions (vendor) -------------------------------------

type transition struct {
	stage    string // 'persisted' | 'expected'
	pos      int
	tracking Tracking
}

func trackingDirectivesEqual(a, b Tracking) bool {
	if a.Type == "none" {
		return b.Type == "none"
	}
	return a.Type == b.Type && a.UserID == b.UserID && a.TS == b.TS
}

// getTrackedChangesTransitions — vendor.
func getTrackedChangesTransitions(
	persisted []TrackedChange,
	expected []map[string]any,
	persistedHistoryOT []map[string]any,
	docLength int,
) []transition {
	var transitions []transition
	for _, ch := range persisted {
		transitions = append(transitions,
			transition{stage: "persisted", pos: ch.Range.Pos, tracking: ch.Tracking},
			transition{stage: "persisted", pos: ch.Range.End(), tracking: Tracking{Type: "none"}},
		)
	}
	for _, ch := range persistedHistoryOT {
		rg, _ := ch["range"].(map[string]any)
		pos, _ := intAny(rg["pos"])
		length, _ := intAny(rg["length"])
		t, _ := ch["tracking"].(map[string]any)
		tr := Tracking{}
		if t != nil {
			tr.Type, _ = t["type"].(string)
			tr.UserID, _ = t["userId"].(string)
			tr.TS, _ = t["ts"].(string)
		}
		transitions = append(transitions,
			transition{stage: "expected", pos: pos, tracking: tr},
			transition{stage: "expected", pos: pos + length, tracking: Tracking{Type: "none"}},
		)
	}
	for _, ch := range expected {
		op, _ := ch["op"].(map[string]any)
		if op == nil {
			continue
		}
		pos, length, text, kind := 0, 0, "", ""
		if h, ok := op["hpos"]; ok {
			pos, _ = intAny(h)
		} else {
			pos, _ = intAny(op["p"])
		}
		if i, ok := op["i"]; ok && i != nil {
			text, _ = i.(string)
			length = len(text)
			kind = "insert"
		} else if d, ok := op["d"]; ok && d != nil {
			text, _ = d.(string)
			length = len(text)
			kind = "delete"
		} else {
			continue
		}
		meta, _ := ch["metadata"].(map[string]any)
		uid := ""
		ts := ""
		if meta != nil {
			uid, _ = meta["user_id"].(string)
			ts, _ = meta["ts"].(string)
		}
		transitions = append(transitions,
			transition{stage: "expected", pos: pos, tracking: Tracking{Type: kind, UserID: uid, TS: ts}},
			transition{stage: "expected", pos: pos + length, tracking: Tracking{Type: "none"}},
		)
	}
	transitions = append(transitions, transition{stage: "expected", pos: docLength, tracking: Tracking{Type: "none"}})

	sort.SliceStable(transitions, func(i, j int) bool {
		a, b := transitions[i], transitions[j]
		if a.pos != b.pos {
			return a.pos < b.pos
		}
		// 'none' comes first so it can be overridden at the same position.
		if a.tracking.Type == "none" && b.tracking.Type != "none" {
			return true
		}
		if a.tracking.Type != "none" && b.tracking.Type == "none" {
			return false
		}
		return false
	})
	return transitions
}

// queueUpdatesForOutOfSyncTrackedChanges — vendor (the cursor/transition walk).
func (e *expander) queueUpdatesForOutOfSyncTrackedChanges(update map[string]any, pathname string, persistedChanges []TrackedChange) {
	expected, _ := update["resyncDocContent"].(map[string]any)
	if expected == nil {
		expected = map[string]any{}
	}
	expectedChanges := []map[string]any{}
	if rg, ok := expected["ranges"].(map[string]any); ok {
		if cl, ok2 := rg["changes"].([]any); ok2 {
			expectedChanges = toAnyMapList(cl)
		}
	}
	// persisted HistoryOT tracked changes (the vendor passes them through the
	// call sites: update.resyncDocContent.historyOTRanges?.trackedChanges)
	persistedHistoryOT := []map[string]any{}
	if hotr, ok := expected["historyOTRanges"].(map[string]any); ok {
		if cl, ok2 := hotr["trackedChanges"].([]any); ok2 {
			persistedHistoryOT = toAnyMapList(cl)
		}
	}
	expectedContent, _ := expected["content"].(string)

	transitions := getTrackedChangesTransitions(persistedChanges, expectedChanges, persistedHistoryOT, len(expectedContent))

	cursor := 0
	persistedTracking := Tracking{Type: "none"}
	expectedTracking := Tracking{Type: "none"}
	type retainOp struct {
		r        string
		p        int
		tracking Tracking
	}
	var ops []retainOp
	currentOp := retainOp{}
	hasCurrent := false

	for _, t := range transitions {
		if t.pos > cursor {
			if trackingDirectivesEqual(expectedTracking, persistedTracking) {
				if hasCurrent {
					ops = append(ops, currentOp)
					hasCurrent = false
				}
			} else {
				retainedText := expectedContent[cursor:clampPos(t.pos, len(expectedContent))]
				if hasCurrent && currentOp.tracking.Type != "none" && trackingDirectivesEqual(expectedTracking, currentOp.tracking) {
					currentOp.r += retainedText
				} else {
					if hasCurrent {
						ops = append(ops, currentOp)
					}
					currentOp = retainOp{r: retainedText, p: cursor, tracking: expectedTracking}
					hasCurrent = true
				}
			}
			cursor = t.pos
		}
		if t.stage == "persisted" {
			persistedTracking = t.tracking
		} else {
			expectedTracking = t.tracking
		}
	}
	if hasCurrent {
		ops = append(ops, currentOp)
	}
	if len(ops) > 0 {
		opAny := make([]any, 0, len(ops))
		for _, o := range ops {
			m := map[string]any{"r": o.r, "p": o.p}
			if o.tracking.Type != "none" {
				m["tracking"] = map[string]any{"type": o.tracking.Type, "userId": o.tracking.UserID, "ts": o.tracking.TS}
			}
			opAny = append(opAny, m)
		}
		e.expandedUpdates = append(e.expandedUpdates, map[string]any{
			"doc": update["doc"],
			"op":  opAny,
			"meta": map[string]any{
				"resync":     true,
				"origin":     e.origin,
				"ts":         metaTS(update),
				"pathname":   pathname,
				"doc_length": len(expectedContent),
			},
		})
	}
}

func clampPos(p, max int) int {
	if p > max {
		return max
	}
	return p
}

func toAnyMapList(v []any) []map[string]any {
	out := []map[string]any{}
	for _, x := range v {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

var _ = fmt.Sprintf // keep fmt imported for future use
