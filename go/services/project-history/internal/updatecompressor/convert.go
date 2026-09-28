package updatecompressor

// ConvertToSingleOpUpdates — vendor `convertToSingleOpUpdates(updates)`.
//
// Updates come from the doc updater as `{op: [op1, op2], meta, v}`; this
// splits them into one-op-per-update, carrying doc_length forward and the
// doc_hash onto the last split update only.
func ConvertToSingleOpUpdates(updates []Update) []Update {
	out := []Update{}
	for _, update := range updates {
		if _, hasOp := update["op"]; !hasOp {
			// Not a text op, likely a project structure op
			out = append(out, update)
			continue
		}
		ops, _ := update["op"].([]any)
		m := MetaOf(update)
		docLength := m["history_doc_length"]
		if docLength == nil {
			docLength = m["doc_length"]
		}
		// Temporary vendor fix for document-updater sending -1 for
		// empty documents.
		if isNegOne(docLength) {
			docLength = float64(0)
		}
		docHash := m["doc_hash"]
		for _, rawOp := range ops {
			opMap, _ := rawOp.(map[string]any)
			split := CloneWithOp(update, opMap)
			// Only the last update will keep the doc_hash property
			delete(MetaOf(split), "doc_hash")
			if docLength != nil {
				n, _ := asNum(docLength)
				MetaOf(split)["doc_length"] = n
				if next, err := adjustLengthByOp(int(n), opMap, jsTruthy(m["tc"])); err == nil {
					docLength = float64(next)
				}
				delete(MetaOf(split), "history_doc_length")
			}
			out = append(out, split)
		}
		if docHash != nil && len(out) > 0 {
			MetaOf(out[len(out)-1])["doc_hash"] = docHash
		}
	}
	return out
}

func isNegOne(v any) bool {
	n, ok := asNum(v)
	return ok && n == -1
}

// FilterBlankUpdates — vendor `filterBlankUpdates(updates)`.
func FilterBlankUpdates(updates []Update) []Update {
	out := make([]Update, 0, len(updates))
	for _, update := range updates {
		op, ok := update["op"].(map[string]any)
		drop := false
		if ok {
			if i, hasI := op["i"]; hasI && i == "" {
				drop = true
			} else if d, hasD := op["d"]; hasD && d == "" {
				drop = true
			}
		}
		if !drop {
			out = append(out, update)
		}
	}
	return out
}

// ConcatUpdatesWithSameVersion — vendor `concatUpdatesWithSameVersion`.
func ConcatUpdatesWithSameVersion(updates []Update) []Update {
	out := []Update{}
	for _, update := range updates {
		if update["op"] == nil {
			// Project structure op
			out = append(out, update)
			continue
		}
		u := CloneWithOp(update, []any{update["op"]})
		if n := len(out); n > 0 {
			last := out[n-1]
			lastOps, ok := last["op"].([]any)
			if ok &&
				last["v"] == u["v"] &&
				last["doc"] == u["doc"] &&
				last["pathname"] == u["pathname"] &&
				isHistoryOT(lastOps[0]) == isHistoryOT(u["op"].([]any)[0]) {
				merged := make([]any, 0, len(lastOps)+1)
				merged = append(merged, lastOps...)
				merged = append(merged, u["op"].([]any)...)
				last["op"] = merged
				lastMeta := MetaOf(last)
				if h := MetaOf(u)["doc_hash"]; h != nil {
					lastMeta["doc_hash"] = h
				} else {
					delete(lastMeta, "doc_hash")
				}
				continue
			}
		}
		out = append(out, u)
	}
	return out
}
