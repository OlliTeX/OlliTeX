package realtime

// grantedDocs — the doc ids the client may cursor-report for, granted at
// join time.
//
// F2 rationale: the pre-F2 client walked open docs and emitted joinDoc per
// doc; the F2 (Yjs) client emits no joinDoc, so if the bus kept the old
// per-doc grant table empty, every clientTracking.updatePosition would be
// "silently ignored" and cross-tab cursor presence would silently die.
// The F2 join response carries the same project model (rootFolder tree),
// so granting every doc id in it is strictly less permissive than the old
// joinDoc flow only for docs the client could not have opened anyway — a
// client can only cursor-report docs shown in its own file tree, which the
// joinProjectResponse project model (permission-scoped on the web side)
// must contain.
func grantedDocs(project map[string]any) map[string]bool {
	out := map[string]bool{}
	if project == nil {
		return out
	}
	if rf, ok := project["rootFolder"].(map[string]any); ok && rf != nil {
		collectDocIDs(rf, out)
	}
	return out
}

func collectDocIDs(node map[string]any, out map[string]bool) {
	if id, ok := node["_id"].(string); ok && id != "" {
		// folder nodes also carry an _id; only leaf "doc"/"fileRef" entries
		// matter for cursor doc access — but granting folders is harmless
		// (cursorData.doc_id references doc ids, never folder ids).
	}
	for _, key := range []string{"docs", "fileRefs"} {
		if arr, ok := node[key].([]any); ok {
			for _, e := range arr {
				if m, ok := e.(map[string]any); ok {
					if id, ok := m["_id"].(string); ok && id != "" {
						out[id] = true
					}
					collectDocIDs(m, out)
				}
			}
		}
	}
	if arr, ok := node["folders"].([]any); ok {
		for _, e := range arr {
			if m, ok := e.(map[string]any); ok {
				collectDocIDs(m, out)
			}
		}
	}
}
