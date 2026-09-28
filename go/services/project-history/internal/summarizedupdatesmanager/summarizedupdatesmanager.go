// Package summarizedupdatesmanager is the 1:1 port of
// services/project-history/app/js/SummarizedUpdatesManager.js (354 L).
//
// Faithful semantics:
//
//	S0 MAX_CHUNK_REQUESTS=5; TIME_BETWEEN_DISTINCT_UPDATES=5 min (ms).
//	S1 options.min_count defaults to 25; nextVersionToRequest =
//	   options.before ?? null.
//	S2 pipeline: processUpdates → getLabels (labelsByVersion: map
//	   version→[labels…]) → getHistoryId → shouldUseProjectHistory:
//	   FALSE → callback(null, []) (NO error); TRUE → the chunk loop.
//	S3 loop: while (chunksRequested < 5) && (next==null || next > 0) &&
//	   summarized.length < min_count:
//	   version==null → mostRecentChunk, else → chunkAtVersion;
//	   convertToSummarizedUpdates(chunk); updateSet.reverse();
//	   discard u.v >= options.before (when before is set);
//	   _summarizeUpdates into the accumulator; nextVersionToRequest =
//	   chunk.chunk.startVersion.
//	S4 final: callback(null, summarizedUpdates,
//	   nextVersionToRequest > 0 ? nextVersionToRequest : undefined).
//	S5 _summarizeUpdates(updates, labels, acc, toV):
//	   toV initialized at the FIRST update seen (toV = v+1); empty updates
//	   (no project_ops and no pathnames) advance toV only; projectOps get
//	   `atV = update.v`; merge into the tail update when allowed, else push
//	   {fromV: v, toV: <current>, meta {users, start_ts, end_ts, origin?},
//	   labels: labels[v+1] ?? [], pathnames Set, project_ops CLONED}.
//	   after each update toV = v.
//	S6 _shouldMergeUpdate: labels non-empty → split; origin mismatch
//	   (both present: kind or path differ → split; kind ∈ {file-restore,
//	   project-restore}: timestamp differ → split; exactly one has origin →
//	   split); tail.update end_ts - update.start_ts >= 5 min → split;
//	   text-vs-file op mix → split EXCEPT when update.origin.kind ∈
//	   {history-resync, history-migration}.
//	S7 _mergeUpdate (MUTATES the tail): users = union uniq-by
//	   (null | user.id | user); fromV = min; toV = max(tail.toV, v+1);
//	   start_ts = min; end_ts = max; project_ops appended (op.add → remove
//	   its pathname from pathnames); pathnames added to the set.
package summarizedupdatesmanager

import (
	"context"
)

const (
	MaxChunkRequests = 5
	// 5 minutes in ms (vendor: 5 * 60 * 1000)
	timeBetweenDistinctUpdates = 300000
)

// Deps — the vendor imports as seams.
type Deps struct {
	// ProcessUpdates — UpdatesProcessor.
	ProcessUpdates func(ctx context.Context, projectID string) error
	// GetLabels — C9 LabelsManager (formatted shape incl. `version`).
	GetLabels func(ctx context.Context, projectID string) ([]map[string]any, error)
	// GetHistoryId — C10 WebApiManager.
	GetHistoryId func(ctx context.Context, projectID string) (string, error)
	// ShouldUseProjectHistory — C11 (HA1).
	ShouldUseProjectHistory func(ctx context.Context, projectID string) (bool, error)
	// MostRecentChunk / ChunkAtVersion — C2 HistoryStoreManager.
	MostRecentChunk func(ctx context.Context, projectID, historyID string) (map[string]any, error)
	ChunkAtVersion  func(ctx context.Context, projectID, historyID string, version int) (map[string]any, error)
	// ConvertToSummarizedUpdates — B10 ChunkTranslator.
	ConvertToSummarizedUpdates func(chunk map[string]any) ([]map[string]any, error)
	// NewSet — the pathnames Set (Go: []string bag with membership; the
	// wire shape is an array — the D-phase serializes it).
}

// Options — vendor options {min_count, before}.
type Options struct {
	MinCount int
	Before   *int // nil = absent
}

// GetSummarizedProjectUpdates — vendor getSummarizedProjectUpdates (S1–S4).
func (d *Deps) GetSummarizedProjectUpdates(ctx context.Context, projectID string, options Options) ([]map[string]any, int, error) {
	if options.MinCount == 0 {
		options.MinCount = 25
	}
	before := options.Before
	var nextVersionToRequest *int
	if before != nil {
		nextVersionToRequest = before
	}

	if err := d.processAndLabels(ctx, projectID); err != nil {
		return nil, 0, err
	}
	// (S2: labels are computed; the merge consults them per-version)
	historyID, err := d.GetHistoryId(ctx, projectID)
	if err != nil {
		return nil, 0, err
	}
	use, err := d.ShouldUseProjectHistory(ctx, projectID)
	if err != nil {
		return nil, 0, err
	}
	if !use {
		return []map[string]any{}, 0, nil
	}

	labelsByVersion, err := d.labelsByVersion(ctx, projectID)
	if err != nil {
		return nil, 0, err
	}

	var summarized []map[string]any
	var toV *int
	requested := 0
	for {
		if !(requested < MaxChunkRequests &&
			(nextVersionToRequest == nil || *nextVersionToRequest > 0) &&
			len(summarized) < options.MinCount) {
			break
		}
		chunk, updateSet, err := d.getProjectUpdates(ctx, projectID, historyID, nextVersionToRequest)
		if err != nil {
			return nil, 0, err
		}
		// vendor: updateSet.reverse()
		for i, j := 0, len(updateSet)-1; i < j; i, j = i+1, j-1 {
			updateSet[i], updateSet[j] = updateSet[j], updateSet[i]
		}
		if before != nil {
			kept := updateSet[:0]
			for _, u := range updateSet {
				if v, _ := toInt(u["v"]); v < *before {
					kept = append(kept, u)
				}
			}
			updateSet = kept
		}
		summarized, toV = summarizeUpdates(updateSet, labelsByVersion, summarized, toV)
		sv := toIntOf(chunk)
		nextVersionToRequest = &sv
		requested++
	}
	if nextVersionToRequest != nil && *nextVersionToRequest > 0 {
		return summarized, *nextVersionToRequest, nil
	}
	return summarized, 0, nil
}

func (d *Deps) processAndLabels(ctx context.Context, projectID string) error {
	if d.ProcessUpdates != nil {
		return d.ProcessUpdates(ctx, projectID)
	}
	if d.GetLabels != nil {
		_, e := d.GetLabels(ctx, projectID)
		return e
	}
	return nil
}

// labelsByVersion — vendor: `{ [label.version]: [...] }` (S5 lookup key).
func (d *Deps) labelsByVersion(ctx context.Context, projectID string) (map[int][]map[string]any, error) {
	out := map[int][]map[string]any{}
	if d.GetLabels == nil {
		return out, nil
	}
	labels, err := d.GetLabels(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, label := range labels {
		v, _ := toInt(label["version"])
		out[v] = append(out[v], label)
	}
	return out, nil
}

// getProjectUpdates — vendor _getProjectUpdates.
func (d *Deps) getProjectUpdates(ctx context.Context, projectID, historyID string, version *int) (map[string]any, []map[string]any, error) {
	var chunk map[string]any
	var err error
	if version != nil {
		chunk, err = d.ChunkAtVersion(ctx, projectID, historyID, *version)
	} else {
		chunk, err = d.MostRecentChunk(ctx, projectID, historyID)
	}
	if err != nil {
		return nil, nil, err
	}
	updates, err := d.ConvertToSummarizedUpdates(chunk)
	if err != nil {
		return nil, nil, err
	}
	return chunk, updates, nil
}

// --- summarize core (S5–S7) ---------------------------------------------------

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func intAny(v any) int {
	n, _ := toInt(v)
	return n
}

func toIntOf(chunk map[string]any) int {
	c, _ := chunk["chunk"].(map[string]any)
	if c == nil {
		c = chunk
	}
	return intAny(c["startVersion"])
}

type bag []string // the pathname Set (wire: array)

func (b *bag) add(p string) {
	for _, e := range *b {
		if e == p {
			return
		}
	}
	*b = append(*b, p)
}

func (b *bag) remove(p string) {
	out := (*b)[:0]
	for _, e := range *b {
		if e != p {
			out = append(out, e)
		}
	}
	*b = out
}

// summarizeUpdates — vendor _summarizeUpdates (S5).
func summarizeUpdates(updates []map[string]any, labels map[int][]map[string]any, existing []map[string]any, toV *int) ([]map[string]any, *int) {
	summarized := existing
	if summarized == nil {
		summarized = []map[string]any{}
	}
	for _, update := range updates {
		if toV == nil {
			v := intAny(update["v"])
			toVVal := v + 1
			toV = &toVVal
		}
		v := intAny(update["v"])
		pathnames := strList(update["pathnames"])
		projectOps := mapList(update["project_ops"])
		// S5: empty updates only advance the version state
		if len(projectOps) == 0 && len(pathnames) == 0 {
			toVal := v
			toV = &toVal
			continue
		}
		for i := range projectOps {
			projectOps[i]["atV"] = v
		}
		var tail map[string]any
		if len(summarized) > 0 {
			tail = summarized[len(summarized)-1]
		}
		labelsForVersion := labels[v+1]
		if len(labelsForVersion) == 0 {
			labelsForVersion = []map[string]any{}
		}
		if tail != nil && shouldMergeUpdate(update, tail, labelsForVersion) {
			mergeUpdate(update, &tail)
			summarized[len(summarized)-1] = tail
		} else {
			newUpdate := map[string]any{
				"fromV":       v,
				"toV":         *toV,
				"meta":        metaCopy(update),
				"labels":      labelsForVersion,
				"pathnames":   append([]string{}, pathnames...),
				"project_ops": append([]map[string]any{}, projectOps...),
			}
			if m, ok := update["meta"].(map[string]any); ok {
				if origin, ok2 := m["origin"]; ok2 && origin != nil {
					nm := newUpdate["meta"].(map[string]any)
					nm["origin"] = origin
				}
			}
			summarized = append(summarized, newUpdate)
		}
		toVal := v
		toV = &toVal
	}
	return summarized, toV
}

func strList(v any) []string {
	out := []string{}
	switch l := v.(type) {
	case []any:
		for _, e := range l {
			if s2, ok := e.(string); ok {
				out = append(out, s2)
			}
		}
	case []string:
		out = append(out, l...)
	}
	return out
}

func mapList(v any) []map[string]any {
	out := []map[string]any{}
	switch l := v.(type) {
	case []any:
		for _, e := range l {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
	case []map[string]any:
		out = append(out, l...)
	}
	return out
}

func metaCopy(update map[string]any) map[string]any {
	m, _ := update["meta"].(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	out := map[string]any{
		"users":    m["users"],
		"start_ts": m["start_ts"],
		"end_ts":   m["end_ts"],
	}
	return out
}

// shouldMergeUpdate — vendor _shouldMergeUpdate (S6).
func shouldMergeUpdate(update, summarizedUpdate map[string]any, labels []map[string]any) bool {
	if summarizedUpdate == nil {
		return false
	}
	// labels → split
	if len(labels) > 0 {
		return false
	}
	um, _ := update["meta"].(map[string]any)
	sm, _ := summarizedUpdate["meta"].(map[string]any)
	uOrigin, uHasOrigin := originOf(um)
	sOrigin, sHasOrigin := originOf(sm)

	if uHasOrigin {
		if !sHasOrigin {
			return false
		}
		if uOrigin["kind"] != sOrigin["kind"] {
			return false
		}
		if uOrigin["path"] != sOrigin["path"] {
			return false
		}
		if (uOrigin["kind"] == "file-restore" || uOrigin["kind"] == "project-restore") &&
			uOrigin["timestamp"] != sOrigin["timestamp"] {
			return false
		}
	} else if sHasOrigin {
		return false
	}
	// time gap (the tail was seen earlier in time: tail.end_ts - update.start_ts)
	endTS := floatAny(sm["end_ts"])
	startTS := floatAny(um["start_ts"])
	if endTS-startTS >= float64(timeBetweenDistinctUpdates) {
		return false
	}
	// text/file op mix
	updatePathnames := strList(update["pathnames"])
	updateOps := mapList(update["project_ops"])
	summaryPathnames := strList(summarizedUpdate["pathnames"])
	summaryOps := mapList(summarizedUpdate["project_ops"])
	isHistoryResync := uHasOrigin && (uOrigin["kind"] == "history-resync" || uOrigin["kind"] == "history-migration")
	if !isHistoryResync &&
		((len(updatePathnames) > 0 && len(summaryOps) > 0) ||
			(len(updateOps) > 0 && len(summaryPathnames) > 0)) {
		return false
	}
	return true
}

func originOf(m map[string]any) (map[string]any, bool) {
	if m == nil {
		return nil, false
	}
	o, ok := m["origin"].(map[string]any)
	if !ok || o == nil {
		return nil, false
	}
	return o, true
}

func floatAny(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

// mergeUpdate — vendor _mergeUpdate (S7; MUTATES the summarized update).
func mergeUpdate(update map[string]any, summarizedUpdate *map[string]any) {
	sm := (*summarizedUpdate)["meta"].(map[string]any)
	um, _ := update["meta"].(map[string]any)
	// users: union uniq-by (null | user.id | user)
	union := userUnion(userList(sm["users"]), userList(um["users"]))
	sm["users"] = union

	(*summarizedUpdate)["fromV"] = minInt(intAny((*summarizedUpdate)["fromV"]), intAny(update["v"]))
	(*summarizedUpdate)["toV"] = maxInt(intAny((*summarizedUpdate)["toV"]), intAny(update["v"])+1)
	sm["start_ts"] = minNum(sm["start_ts"], um["start_ts"])
	sm["end_ts"] = maxNum(sm["end_ts"], um["end_ts"])

	// project ops
	ops := mapList((*summarizedUpdate)["project_ops"])
	uOps := mapList(update["project_ops"])
	ops = append(ops, uOps...)
	paths := strList((*summarizedUpdate)["pathnames"])
	for _, op := range uOps {
		if addPath, ok := op["add"].(map[string]any); ok {
			pn, _ := addPath["pathname"].(string)
			out := paths[:0]
			for _, p := range paths {
				if p != pn {
					out = append(out, p)
				}
			}
			paths = out
		}
	}
	(*summarizedUpdate)["project_ops"] = ops
	has := func(p string) bool {
		for _, e := range paths {
			if e == p {
				return true
			}
		}
		return false
	}
	for _, p := range strList(update["pathnames"]) {
		if !has(p) {
			paths = append(paths, p)
		}
	}
	(*summarizedUpdate)["pathnames"] = paths
}

func userList(v any) []any {
	l, _ := v.([]any)
	if l == nil {
		return []any{}
	}
	return l
}

// userUnion — _.uniqBy(_.union(a, b), user => user == null ? null :
// (user.id == null ? user : user.id)).
func userUnion(a, b []any) []any {
	all := append(append([]any{}, a...), b...)
	seen := map[any]bool{}
	out := []any{}
	for _, u := range all {
		key := userKey(u)
		k, ok := key.(string)
		if !ok {
			k = ""
			if u == nil {
				continue // null users are filtered out by uniqBy's null key
			}
		}
		if u == nil {
			continue
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, u)
	}
	if len(out) == 0 {
		return []any{}
	}
	return out
}

func userKey(u any) any {
	if u == nil {
		return nil
	}
	m, ok := u.(map[string]any)
	if !ok {
		return u
	}
	id, has := m["id"]
	if !has || id == nil {
		return u
	}
	return id
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minNum(a, b any) any {
	fa, fb := floatAny(a), floatAny(b)
	if fa <= fb {
		return a
	}
	return b
}

func maxNum(a, b any) any {
	fa, fb := floatAny(a), floatAny(b)
	if fa >= fb {
		return a
	}
	return b
}
