package core

import "go.mongodb.org/mongo-driver/v2/bson"

// LooseDoc — deep-normalize a Mongo document decoded through mongo driver v2:
// the driver returns nested documents as bson.D and arrays as bson.A inside a
// top-level map[string]any target. Callers that read the nesting with plain
// `.(map[string]any)` / `.(bson.D)` type assertions therefore see the nested
// doc as a DIFFERENT type and silently fall back to defaults.
//
// Audit 001 (theme lost across hub<->editor, "falls back to system"):
// editorpages.buildUserSettings read ace.* through such assertions, so
// ace.overallTheme was never visible and the page always emitted the default
// "system" — the user's stored Dark/Light choice (persisted fine by
// POST /user/settings) never reached the page. The 014-016 cluster
// (llmsettings asMap) and the 003 admin cluster (positional _id, string
// trashed refs) are the same family of driver-shape pitfalls.
//
// Returns nil for non-object roots (callers keep their existing nil checks).
func LooseDoc(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = looseVal(val)
		}
		return out
	case bson.D:
		out := make(map[string]any, len(t))
		for _, e := range t {
			out[e.Key] = looseVal(e.Value)
		}
		return out
	}
	return nil
}

func looseVal(v any) any {
	switch t := v.(type) {
	case bson.D:
		return LooseDoc(t)
	case bson.A:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = looseVal(e)
		}
		return out
	default:
		return v
	}
}
