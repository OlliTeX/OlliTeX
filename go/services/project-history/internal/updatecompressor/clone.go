package updatecompressor

// Update — raw JSON update map (vendor `Update`):
// {op: ... | [ops], meta: {...}, v, doc, pathname, ...}.
type Update = map[string]any

// CloneUpdate — vendor `Object.assign({}, update)`.
func CloneUpdate(u Update) Update {
	out := make(Update, len(u)+1)
	for k, v := range u {
		out[k] = v
	}
	return out
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// MetaOf — returns the update's meta submap (a reference; callers may
// modify it). The top-level fields of the update itself are not cloned —
// matching vendor, which only clones meta when producing a new update.
func MetaOf(u Update) map[string]any {
	m, _ := u["meta"].(map[string]any)
	return m
}

// CloneWithOp — vendor `cloneWithOp(update, op)`: shallow-clone the update
// and its meta, overwrite op.
func CloneWithOp(u Update, op any) Update {
	out := CloneUpdate(u)
	out["meta"] = cloneMap(MetaOf(u))
	out["op"] = op
	return out
}

// MergeUpdatesWithOp — vendor `mergeUpdatesWithOp(firstUpdate, secondUpdate, op)`:
// doc_length and ts from firstUpdate, v and doc_hash from secondUpdate.
func MergeUpdatesWithOp(first, second Update, op any) Update {
	out := CloneWithOp(first, op)
	if second["v"] != nil {
		out["v"] = second["v"]
	}
	if dh, ok := MetaOf(second)["doc_hash"]; ok && dh != nil {
		MetaOf(out)["doc_hash"] = dh
	} else {
		delete(MetaOf(out), "doc_hash")
	}
	return out
}
