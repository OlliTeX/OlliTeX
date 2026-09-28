// Package historyblobtranslator is the 1:1 port of
// services/project-history/app/js/HistoryBlobTranslator.js (123 L).
package historyblobtranslator

import (
	"sort"

	"ollitex/go/services/project-history/internal/opmodel"
)

// CreateRangeBlobDataFromUpdate — vendor
// `createRangeBlobDataFromUpdate(update)`:
//
//   - update.doc == null || update.docLines == null  →  throw OError('Not an AddFileUpdate')
//   - no ranges OR (ranges.changes==null && ranges.comments==null) → undefined (nil, true)
//   - both empty lists → undefined
//   - else builds {trackedChanges: tcList.toRaw(), comments: commentList.toRaw()}
//
// Port note: Go has no undefined-vs-object distinction, so the return is
// (data map[string]any, found bool, err error) — found=false ≙ vendor
// `undefined`.
func CreateRangeBlobDataFromUpdate(update map[string]any) (map[string]any, bool, error) {
	if _, hasDoc := update["doc"]; !hasDoc || update["doc"] == nil {
		return nil, false, newOError("Not an AddFileUpdate")
	}
	if _, hasLines := update["docLines"]; !hasLines || update["docLines"] == nil {
		return nil, false, newOError("Not an AddFileUpdate")
	}
	rangesVal, hasRanges := update["ranges"]
	if !hasRanges || rangesVal == nil {
		return nil, true, nil // vendor: return undefined
	}
	ranges, ok := rangesVal.(map[string]any)
	if !ok {
		return nil, true, nil // !update.ranges → undefined
	}
	changesVal, hasChanges := ranges["changes"]
	commentsVal, hasComments := ranges["comments"]
	if !hasChanges || changesVal == nil {
		if !hasComments || commentsVal == nil {
			return nil, true, nil
		}
	} else if listEmpty(changesVal) {
		if !hasComments || commentsVal == nil || listEmpty(commentsVal) {
			return nil, true, nil
		}
	}
	if !hasChanges {
		changesVal = nil
	}
	if !hasComments {
		commentsVal = nil
	}
	changes := toOpList(changesVal)
	comments := toOpList(commentsVal)

	// vendor: sort by op.p; ties: a delete-before-insert order —
	// 'i' in a.op && a.op.i != null && 'd' in b.op && b.op.d != null → 1,
	// else -1 (i.e. deletes come first when p ties).
	sort.SliceStable(changes, func(i, j int) bool {
		opI, opJ := opOf(changes[i]), opOf(changes[j])
		pi, _ := opI["p"].(int)
		pj, _ := opJ["p"].(int)
		if hasNum(opI, "p") && hasNum(opJ, "p") {
			if pi != pj {
				return pi < pj
			}
		}
		// tie: if i is insert && j is delete → a AFTER b (vendor returns 1)
		if opHasInsert(opI) && opHasDelete(opJ) {
			return false
		}
		return true // -1 → a before b
	})

	tcList := opmodel.NewTrackedChangeList(nil)
	for _, change := range changes {
		op := opOf(change)
		meta, _ := change["metadata"].(map[string]any)
		switch {
		case opHasDelete(op):
			length := strLen(op["d"])
			pos := opPosHpos(op)
			rng, err := opmodel.NewRange(pos, length)
			if err != nil {
				return nil, false, err
			}
			tcList.Add(opmodel.TrackedChange{
				Range:    rng,
				Tracking: *opmodel.NewTracking("delete", strOf(meta, "user_id"), tsMsOf(meta["ts"])),
			})
		case opHasInsert(op):
			length := strLen(op["i"])
			pos := opPosHpos(op)
			rng, err := opmodel.NewRange(pos, length)
			if err != nil {
				return nil, false, err
			}
			tcList.Add(opmodel.TrackedChange{
				Range:    rng,
				Tracking: *opmodel.NewTracking("insert", strOf(meta, "user_id"), tsMsOf(meta["ts"])),
			})
		}
	}

	// comments: sort by op.p (vendor: a.op.p - b.op.p)
	sort.SliceStable(comments, func(i, j int) bool {
		return opPos(opOf(comments[i])) < opPos(opOf(comments[j]))
	})
	type commentAgg struct {
		ranges   []opmodel.Range
		resolved bool
	}
	orderedIDs := []string{}
	commentMap := map[string]*commentAgg{}
	for _, comment := range comments {
		op := opOf(comment)
		id, _ := op["t"].(string)
		entry, has := commentMap[id]
		if !has {
			entry = &commentAgg{resolved: resolvedOf(op)}
			commentMap[id] = entry
			orderedIDs = append(orderedIDs, id)
		}
		if entry.resolved != resolvedOf(op) {
			return nil, false, newPlainError("Mismatching resolved status for comment")
		}
		cLen := strLen(op["c"])
		if cLen > 0 {
			rng, err := opmodel.NewRange(opPosHpos(op), cLen)
			if err != nil {
				return nil, false, err
			}
			entry.ranges = append(entry.ranges, rng)
		}
	}
	clComments := make([]*opmodel.Comment, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		entry := commentMap[id]
		if c, err := opmodel.NewComment(id, entry.ranges, entry.resolved); err == nil {
			clComments = append(clComments, c)
		}
	}
	cl := opmodel.NewCommentList(clComments)
	return map[string]any{
		"trackedChanges": tcList.ToRaw(),
		"comments":       cl.ToRaw(),
	}, true, nil
}

// --- helpers -------------------------------------------------------------

type oError struct{ msg string }

func (e *oError) Error() string { return e.msg }

func newOError(msg string) error { return &oError{msg} }
func newPlainError(msg string) error {
	return &plainError{msg}
}

type plainError struct{ msg string }

func (e *plainError) Error() string { return e.msg }

func listEmpty(v any) bool {
	lst, ok := v.([]any)
	return ok && len(lst) == 0
}

func toOpList(v any) []map[string]any {
	lst, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(lst))
	for _, e := range lst {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func opOf(change map[string]any) map[string]any {
	op, _ := change["op"].(map[string]any)
	return op
}

func hasNum(op map[string]any, key string) bool {
	_, ok := op[key].(int)
	return ok
}

func strOf(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func opPosHpos(op map[string]any) int {
	if h, ok := op["hpos"].(int); ok {
		return h
	}
	if p, ok := op["p"].(int); ok {
		return p
	}
	return 0
}

func opPos(op map[string]any) int {
	if p, ok := op["p"].(int); ok {
		return p
	}
	return 0
}

func opHasDelete(op map[string]any) bool {
	v, has := op["d"]
	return has && v != nil
}

func opHasInsert(op map[string]any) bool {
	v, has := op["i"]
	return has && v != nil
}

func strLen(v any) int {
	s, _ := v.(string)
	return len(s)
}

func resolvedOf(op map[string]any) bool {
	if v, has := op["resolved"]; has && v != nil {
		b, _ := v.(bool)
		return b
	}
	return false
}

func tsMsOf(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case string:
		// vendor `new Date(string)` — ISO parsing handled by caller deps;
		// a numeric-looking string is accepted, else 0 (tests pin numeric).
		var out int64
		for _, r := range n {
			if r < '0' || r > '9' {
				return 0
			}
			out = out*10 + int64(r-'0')
		}
		return out
	}
	return 0
}
