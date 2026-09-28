package updatecompressor

import (
	"ollitex/go/services/project-history/internal/historyot"
)

// CompressUpdates — vendor `compressUpdates(updates)`: fold the (single-op)
// update list left-to-right via concatTwoUpdates.
func CompressUpdates(updates []Update) ([]Update, error) {
	if len(updates) == 0 {
		return []Update{}, nil
	}
	out := []Update{updates[0]}
	rest := updates[1:]
	for i := 0; i < len(rest); i++ {
		if len(out) == 0 {
			out = append(out, rest[i])
			continue
		}
		last := out[len(out)-1]
		merged, err := concatTwoUpdates(last, rest[i])
		if err != nil {
			return nil, err
		}
		out = out[:len(out)-1]
		out = append(out, merged...)
	}
	return out, nil
}

// CompressRawUpdates — vendor `compressRawUpdates(rawUpdates)`: the pure
// pipeline convert → compress → filterBlank → concatSameVersion. (The
// vendor `...WithMetrics`/`...WithProfile` wrappers are not ported.)
func CompressRawUpdates(rawUpdates []Update) ([]Update, error) {
	updates := ConvertToSingleOpUpdates(rawUpdates)
	updates, err := CompressUpdates(updates)
	if err != nil {
		return nil, err
	}
	updates = FilterBlankUpdates(updates)
	updates = ConcatUpdatesWithSameVersion(updates)
	return updates, nil
}

func jsLen(s string) int { return utf16Len(s) }

func opSize(op map[string]any) int {
	// vendor: (firstOp.i && firstOp.i.length) || (firstOp.d && firstOp.d.length)
	if i, ok := op["i"].(string); ok && i != "" {
		return jsLen(i)
	}
	if d, ok := op["d"].(string); ok {
		return jsLen(d)
	}
	return 0
}

func toSet(ids []any) map[string]struct{} {
	if ids == nil {
		return nil
	}
	set := map[string]struct{}{}
	for _, v := range ids {
		id, ok := v.(string)
		if !ok {
			return nil
		}
		set[id] = struct{}{}
	}
	return set
}

func commentIDsOf(op map[string]any) map[string]struct{} {
	raw, ok := op["commentIds"].([]any)
	if !ok || raw == nil {
		return nil
	}
	return toSet(raw)
}

// insertOpsInsideSameComments — vendor `insertOpsInsideSameComments(op1, op2)`.
func insertOpsInsideSameComments(op1, op2 map[string]any) bool {
	c1 := commentIDsOf(op1)
	c2 := commentIDsOf(op2)
	if c1 == nil && c2 == nil {
		return true
	}
	if c1 == nil || c2 == nil {
		return false
	}
	every := func(set, other map[string]struct{}) bool {
		for id := range set {
			if _, ok := other[id]; !ok {
				return false
			}
		}
		return true
	}
	return every(c1, c2) && every(c2, c1)
}

func tsDeltaOK(first, second Update) bool {
	f, okF := asNum(MetaOf(first)["ts"])
	s, okS := asNum(MetaOf(second)["ts"])
	if !okF || !okS {
		return false
	}
	return s-f > float64(MaxTimeBetweenUpdates)
}

func hposOffsetsEqual(firstOp, secondOp map[string]any) (bool, bool) {
	firstP, ok1 := asNum(firstOp["p"])
	secondP, ok2 := asNum(secondOp["p"])
	if !ok1 || !ok2 {
		return false, false
	}
	firstHpos, hasFH := asNum(firstOp["hpos"])
	if !hasFH {
		firstHpos = firstP
	}
	secondHpos, hasSH := asNum(secondOp["hpos"])
	if !hasSH {
		secondHpos = secondP
	}
	return (firstHpos - firstP) == (secondHpos - secondP), true
}

// concatTwoUpdates — vendor `_concatTwoUpdates(firstUpdate, secondUpdate)`.
func concatTwoUpdates(firstUpdate, secondUpdate Update) ([]Update, error) {
	firstOp, ok1 := firstUpdate["op"].(map[string]any)
	secondOp, ok2 := secondUpdate["op"].(map[string]any)
	both := ok1 && ok2
	if !both {
		// Project structure ops (op == null)
		return []Update{firstUpdate, secondUpdate}, nil
	}

	firstIsHistoryOT := isHistoryOT(firstOp)
	secondIsHistoryOT := isHistoryOT(secondOp)
	if firstIsHistoryOT != secondIsHistoryOT {
		return []Update{firstUpdate, secondUpdate}, nil
	}
	if firstUpdate["doc"] != secondUpdate["doc"] ||
		firstUpdate["pathname"] != secondUpdate["pathname"] {
		return []Update{firstUpdate, secondUpdate}, nil
	}
	firstMeta, secondMeta := MetaOf(firstUpdate), MetaOf(secondUpdate)
	if jsTruthy(firstMeta["resync"]) || jsTruthy(secondMeta["resync"]) {
		return []Update{firstUpdate, secondUpdate}, nil
	}
	if firstMeta["user_id"] != secondMeta["user_id"] {
		return []Update{firstUpdate, secondUpdate}, nil
	}
	firstExt := firstMeta["type"] == "external"
	secondExt := secondMeta["type"] == "external"
	if (firstExt && !secondExt) || (!firstExt && secondExt) ||
		(firstExt && secondExt && firstMeta["source"] != secondMeta["source"]) {
		return []Update{firstUpdate, secondUpdate}, nil
	}
	if tsDeltaOK(firstUpdate, secondUpdate) {
		return []Update{firstUpdate, secondUpdate}, nil
	}
	if (firstMeta["tc"] == nil && secondMeta["tc"] != nil) ||
		(firstMeta["tc"] != nil && secondMeta["tc"] == nil) {
		return []Update{firstUpdate, secondUpdate}, nil
	}
	if jsTruthy(firstOp["u"]) != jsTruthy(secondOp["u"]) {
		return []Update{firstUpdate, secondUpdate}, nil
	}
	if firstIsHistoryOT && secondIsHistoryOT {
		op1, err := historyot.Builder.BuildFromRaw(firstOp)
		if err != nil {
			return nil, err
		}
		op2, err := historyot.Builder.BuildFromRaw(secondOp)
		if err != nil {
			return nil, err
		}
		if op1.CanBeComposedWith(op2) {
			composed, err := op1.Compose(op2)
			if err != nil {
				return nil, err
			}
			return []Update{MergeUpdatesWithOp(firstUpdate, secondUpdate, composed.ToRaw())}, nil
		}
		return []Update{firstUpdate, secondUpdate}, nil
	}
	if jsTruthy(firstOp["trackedDeleteRejection"]) || jsTruthy(secondOp["trackedDeleteRejection"]) {
		return []Update{firstUpdate, secondUpdate}, nil
	}
	if firstOp["trackedChanges"] != nil || secondOp["trackedChanges"] != nil {
		return []Update{firstUpdate, secondUpdate}, nil
	}

	firstP, okFP := asNum(firstOp["p"])
	secondP, okSP := asNum(secondOp["p"])
	if !okFP || !okSP {
		return []Update{firstUpdate, secondUpdate}, nil
	}
	firstSize := opSize(firstOp)
	secondSize := opSize(secondOp)
	firstOpInsideSecondOp := secondP <= firstP && firstP <= secondP+float64(secondSize)
	secondOpInsideFirstOp := firstP <= secondP && secondP <= firstP+float64(firstSize)
	combinedLengthUnderLimit := firstSize+secondSize < MaxUpdateSize
	if eq, ok := hposOffsetsEqual(firstOp, secondOp); !ok || !eq {
		return []Update{firstUpdate, secondUpdate}, nil
	}

	firstI, hasFirstI := firstOp["i"].(string)
	secondI, hasSecondI := secondOp["i"].(string)
	firstD, hasFirstD := firstOp["d"].(string)
	secondD, hasSecondD := secondOp["d"].(string)

	// Two inserts
	if hasFirstI && hasSecondI && secondOpInsideFirstOp &&
		combinedLengthUnderLimit && insertOpsInsideSameComments(firstOp, secondOp) {
		newOp := CloneUpdate(firstOp)
		newOp["i"] = strInject(firstI, int(secondP-firstP), secondI)
		return []Update{MergeUpdatesWithOp(firstUpdate, secondUpdate, newOp)}, nil
	}

	// Two deletes
	if hasFirstD && hasSecondD && firstOpInsideSecondOp &&
		combinedLengthUnderLimit &&
		firstMeta["tc"] == nil && secondMeta["tc"] == nil {
		newOp := CloneUpdate(secondOp)
		newOp["d"] = strInject(secondD, int(firstP-secondP), firstD)
		return []Update{MergeUpdatesWithOp(firstUpdate, secondUpdate, newOp)}, nil
	}

	// An insert and then a delete
	if hasFirstI && hasSecondD && secondOpInsideFirstOp &&
		firstMeta["tc"] == nil && secondMeta["tc"] == nil {
		offset := int(secondP - firstP)
		insertedText := strUnitSlice(firstI, offset, secondSize)
		if insertedText == secondD {
			insert := strRemove(firstI, offset, secondSize)
			if insert == "" {
				return []Update{}, nil
			}
			newOp := CloneUpdate(firstOp)
			newOp["i"] = insert
			return []Update{MergeUpdatesWithOp(firstUpdate, secondUpdate, newOp)}, nil
		}
		// The delete extends outside the insert.
		return []Update{firstUpdate, secondUpdate}, nil
	}

	// A delete then an insert at the same place, likely a copy-paste.
	if hasFirstD && hasSecondI && firstP == secondP &&
		firstMeta["tc"] == nil && secondMeta["tc"] == nil {
		return diffDeleteInsert(firstUpdate, secondUpdate, firstOp, secondOp)
	}

	return []Update{firstUpdate, secondUpdate}, nil
}

// diffDeleteInsert handles the delete-then-insert-at-same-place case by
// diffing the old and new text into sharejs ops.
func diffDeleteInsert(firstUpdate, secondUpdate Update, firstOp, secondOp map[string]any) ([]Update, error) {
	firstD, _ := firstOp["d"].(string)
	secondI, _ := secondOp["i"].(string)
	firstP, _ := asNum(firstOp["p"])
	hoffset, hasHpos := asNum(firstOp["hpos"])

	diffOps := DiffAsShareJsOps(firstD, secondI)
	off := firstP
	diffUpdates := []Update{}
	for _, op := range diffOps {
		newOp := CloneUpdate(op)
		pos, ok := newOp["p"].(float64)
		if !ok {
			continue
		}
		newOp["p"] = pos + off
		if hasHpos {
			newOp["hpos"] = pos + hoffset
		}
		if jsTruthy(firstOp["u"]) && jsTruthy(secondOp["u"]) {
			newOp["u"] = true
		}
		if _, hasI := newOp["i"]; hasI && secondOp["commentIds"] != nil {
			newOp["commentIds"] = secondOp["commentIds"]
		}
		update := MergeUpdatesWithOp(firstUpdate, secondUpdate, newOp)
		// Set the doc hash only on the last update
		delete(MetaOf(update), "doc_hash")
		diffUpdates = append(diffUpdates, update)
	}
	if docHash := MetaOf(secondUpdate)["doc_hash"]; docHash != nil && len(diffUpdates) > 0 {
		MetaOf(diffUpdates[len(diffUpdates)-1])["doc_hash"] = docHash
	}

	// Recalculate the doc lengths (lost during the diff).
	firstMeta := MetaOf(firstUpdate)
	docLength := firstMeta["history_doc_length"]
	if docLength == nil {
		docLength = firstMeta["doc_length"]
	}
	for i := range diffUpdates {
		update := diffUpdates[i]
		if n, ok := asNum(docLength); ok {
			MetaOf(update)["doc_length"] = n
			na, _ := asNum(MetaOf(update)["doc_length"])
			if advanced, err := adjustLengthByOp(int(na), update["op"].(map[string]any), jsTruthy(MetaOf(update)["tc"])); err == nil {
				docLength = float64(advanced)
			}
		}
		delete(MetaOf(update), "history_doc_length")
	}
	return diffUpdates, nil
}

// DiffAsShareJsOps — vendor `diffAsShareJsOps(before, after)`: diff a
// delete's old text against an insert's new text, returning sharejs ops
// (i/d) with positions relative to the start of the deleted range.
func DiffAsShareJsOps(before, after string) []map[string]any {
	diffs := diffDmp.DiffMain(before, after)
	diffDmp.CleanupSemantic(&diffs)
	ops := []map[string]any{}
	position := 0
	const ADDED, REMOVED, UNCHANGED = 1, -1, 0
	for _, diff := range diffs {
		if diff.Op == ADDED {
			ops = append(ops, map[string]any{"i": diff.Text, "p": float64(position)})
			position += jsLen(diff.Text)
		} else if diff.Op == REMOVED {
			ops = append(ops, map[string]any{"d": diff.Text, "p": float64(position)})
		} else if diff.Op == UNCHANGED {
			position += jsLen(diff.Text)
		} else {
			panic("UpdateCompressor: unknown diff type")
		}
	}
	return ops
}
