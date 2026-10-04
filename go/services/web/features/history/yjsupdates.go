// yjsupdates.go — d5dd23dd S1.2 (D41 hybrid b1): Yjs-native /updates.
//
// The editor's history panel reads `/project/:pid/updates`. In the Yjs era
// the version store of record is the collab room (ygo versioned store,
// room = project id) — the OT summarized-update plane (go PH :3054) is
// empty for Yjs-edited projects (verified live 2026-09-28: fresh project
// → /updates empty AND /collab/doc empty until the editor seeds the room).
//
// This file composes the Node-parity answer from the Yjs plane:
//
//   - versions:  the room's version log (st.ListVersions, ascending);
//   - author:    the collab version-log side record (S1.1: versionlog.go);
//   - pathnames: the room's single seeded text file (root doc name); the
//     multi-file tree-ops stream lands in S3 (design doc) — until then
//     project_ops stays empty;
//   - labels:    the label plane stays on the V2 surface until S2/S5
//     unification — rows carry empty labels (the u101 fixture pins an
//     unlabeled project; the labels GET/POST DELETE routes are untouched);
//   - merge:     1:1 port of the Node _summarizeUpdates/_shouldMergeUpdate/
//     _mergeUpdate core (ph port: go/services/project-history/internal/
//     summarizedupdatesmanager) so the Yjs rows and the OT rows shape and
//     group identically.
//
// Fallback: an empty room (no Yjs versions) falls through to the legacy V2
// proxy — byte-identical to today for OT-era projects.
package history

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"ollitex/go/services/collab"

	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// yjsTimeBetweenDistinctUpdates — vendor TIME_BETWEEN_DISTINCT_UPDATES
// (5 minutes, ms; split threshold in shouldMerge).
const yjsTimeBetweenUpdates = 300000

// ---------- S5–S7 core (1:1 port of the vendor/PH merge machinery) ----------

func yjsMeta(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := map[string]any{"users": m["users"], "start_ts": m["start_ts"], "end_ts": m["end_ts"]}
	if o, ok := m["origin"].(map[string]any); ok && o != nil {
		out["origin"] = o
	}
	return out
}

// summarizeYjs — vendor _summarizeUpdates (ascending `updates`; each
// {v, meta{users,start_ts,end_ts,origin?}, pathnames, project_ops}).
func summarizeYjs(updates []map[string]any, labels map[int][]map[string]any, existing []map[string]any) []map[string]any {
	summarized := existing
	if summarized == nil {
		summarized = []map[string]any{}
	}
	toV := -1 // vendor: toV initialized at (first v)+1 before the first row
	have := false
	for _, update := range updates {
		v := yjsInt(update["v"])
		if !have {
			toV = v + 1
			have = true
		}
		pathnames := yjsStrList(update["pathnames"])
		projectOps := yjsMapList(update["project_ops"])
		// S5: empty updates only advance the version state
		if len(projectOps) == 0 && len(pathnames) == 0 {
			toV = v
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
		if labelsForVersion == nil {
			labelsForVersion = []map[string]any{}
		}
		if tail != nil && yjsShouldMerge(update, tail, labelsForVersion) {
			yjsMerge(update, &tail)
			summarized[len(summarized)-1] = tail
		} else {
			meta := yjsMeta(mapOf(update["meta"]))
			if o, ok := mapOf(update["meta"])["origin"]; ok {
				if om, ok2 := o.(map[string]any); ok2 && om != nil {
					meta["origin"] = om
				}
			}
			summarized = append(summarized, map[string]any{
				"fromV":       v,
				"toV":         toV,
				"meta":        meta,
				"labels":      labelsForVersion,
				"pathnames":   append([]string{}, pathnames...),
				"project_ops": append([]map[string]any{}, projectOps...),
			})
		}
		toV = v
	}
	return summarized
}

func yjsShouldMerge(update, summarizedUpdate map[string]any, labels []map[string]any) bool {
	if summarizedUpdate == nil {
		return false
	}
	if len(labels) > 0 {
		return false
	}
	um := mapOf(update["meta"])
	sm := mapOf(summarizedUpdate["meta"])
	uOrigin, uHas := yjsOrigin(um)
	sOrigin, sHas := yjsOrigin(sm)
	if uHas {
		if !sHas {
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
	} else if sHas {
		return false
	}
	if yjsFloat(sm["end_ts"])-yjsFloat(um["start_ts"]) >= yjsTimeBetweenUpdates {
		return false
	}
	up, uops := yjsStrList(update["pathnames"]), yjsMapList(update["project_ops"])
	sp, sops := yjsStrList(summarizedUpdate["pathnames"]), yjsMapList(summarizedUpdate["project_ops"])
	isResync := uHas && (uOrigin["kind"] == "history-resync" || uOrigin["kind"] == "history-migration")
	if !isResync && ((len(up) > 0 && len(sops) > 0) || (len(uops) > 0 && len(sp) > 0)) {
		return false
	}
	return true
}

func yjsMerge(update map[string]any, summarizedUpdate *map[string]any) {
	sm := (*summarizedUpdate)["meta"].(map[string]any)
	um := mapOf(update["meta"])
	sm["users"] = yjsUserUnion(yjsList(sm["users"]), yjsList(um["users"]))
	(*summarizedUpdate)["fromV"] = yjsMin(yjsInt((*summarizedUpdate)["fromV"]), yjsInt(update["v"]))
	(*summarizedUpdate)["toV"] = yjsMax(yjsInt((*summarizedUpdate)["toV"]), yjsInt(update["v"])+1)
	sm["start_ts"] = yjsMinNum(sm["start_ts"], um["start_ts"])
	sm["end_ts"] = yjsMaxNum(sm["end_ts"], um["end_ts"])
	ops := yjsMapList((*summarizedUpdate)["project_ops"])
	ops = append(ops, yjsMapList(update["project_ops"])...)
	(*summarizedUpdate)["project_ops"] = ops
	paths := yjsStrList((*summarizedUpdate)["pathnames"])
	for _, p := range yjsStrList(update["pathnames"]) {
		dup := false
		for _, e := range paths {
			if e == p {
				dup = true
				break
			}
		}
		if !dup {
			paths = append(paths, p)
		}
	}
	(*summarizedUpdate)["pathnames"] = paths
}

func yjsOrigin(m map[string]any) (map[string]any, bool) {
	if m == nil {
		return nil, false
	}
	o, ok := m["origin"].(map[string]any)
	return o, ok && o != nil
}

// User union — the Yjs feed is single-actor-per-version, so this is the
// set-union of two small lists (null-safe, order-preserving).
func yjsUserUnion(a, b []any) []any {
	seen := map[string]bool{}
	k := func(u any) string {
		if u == nil {
			return "null"
		}
		if m, ok := u.(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				return "id:" + id
			}
		}
		if s, ok := u.(string); ok {
			return "id:" + s
		}
		return fmt.Sprintf("%v", u)
	}
	out := []any{}
	for _, u := range append(append([]any{}, a...), b...) {
		if seen[k(u)] {
			continue
		}
		seen[k(u)] = true
		out = append(out, u)
	}
	return out
}

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func yjsStrList(v any) []string {
	out := []string{}
	switch l := v.(type) {
	case []any:
		for _, e := range l {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = append(out, l...)
	}
	return out
}

func yjsMapList(v any) []map[string]any {
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

func yjsList(v any) []any {
	l, _ := v.([]any)
	if l == nil {
		return []any{}
	}
	return l
}

func yjsInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func yjsFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	}
	return 0
}

func yjsMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func yjsMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func yjsMinNum(a, b any) any {
	if yjsFloat(a) < yjsFloat(b) {
		return a
	}
	return b
}

func yjsMaxNum(a, b any) any {
	if yjsFloat(a) > yjsFloat(b) {
		return a
	}
	return b
}

// ---------- Yjs feed assembly ----------

// originObject — maps the S1.1 flat origin tag to the Node origin object
// shape (frontend shared.ts): file-restore {kind,path,timestamp},
// project-restore {kind,timestamp}, plain kinds {kind}.
func originObject(origin, path string, ts int64) (map[string]any, bool) {
	switch origin {
	case "":
		return nil, false
	case "file-restore":
		return map[string]any{"kind": "file-restore", "path": path, "timestamp": ts}, true
	case "project-restore":
		return map[string]any{"kind": "project-restore", "timestamp": ts}, true
	default:
		return map[string]any{"kind": origin}, true
	}
}

// composeYjsUpdates — the Node-parity /updates body from the Yjs plane.
// ok=false when the room has no versions (caller falls back to the legacy
// V2 path). before: optional version cursor (vendor `before`; excluded).
// S3a: superseded by composeMerged (which also merges the tree-op log);
// kept as the ylog==nil fast path (and the S1.2 test oracle).
func composeYjsUpdates(ctx context.Context, st persistence.VersionedPersistence, vlog collab.Log, room, rootPath string, before *int) (body []byte, ok bool, err error) {
	return composeMerged(ctx, st, vlog, nil, room, rootPath, before)
}

// composeMerged — the S3a unified /updates body: the room's text versions
// and the tree-op log (yops) merged into ONE sequence (ts order; ties:
// room before yops), a unified 1..N index assigned, then fed NEWEST-FIRST
// to the vendor summarizer (which carries project_ops, stamps atV, and
// keeps the S1.2 row/meta/origin rules intact — text and file rows never
// merge, vendor S6).
func composeMerged(ctx context.Context, st persistence.VersionedPersistence, vlog collab.Log, ylog YopLog, room, rootPath string, before *int) (body []byte, ok bool, err error) {
	lvs, err := st.ListVersions(ctx, room)
	if err != nil {
		return nil, false, err
	}
	var yops []YopMeta
	if ylog != nil {
		if yops, err = ylog.List(ctx, room); err != nil {
			return nil, false, err
		}
	}
	if len(lvs) == 0 && len(yops) == 0 {
		return nil, false, nil // nothing in the Yjs plane → legacy fallback
	}
	vmetas := map[uint64]collab.VersionMeta{}
	if vlog != nil && len(lvs) > 0 {
		var maxV uint64
		for _, lv := range lvs {
			if uint64(lv.Version) > maxV {
				maxV = uint64(lv.Version)
			}
		}
		if metas, rerr := vlog.Range(ctx, room, 0, maxV); rerr == nil {
			for _, m := range metas {
				vmetas[m.V] = m
			}
		}
	}
	feed := buildUnifiedFeed(lvs, vmetas, yops, rootPath)
	updates := []map[string]any{}
	for _, item := range feed {
		v := item.UnifiedV
		if before != nil && v >= *before {
			continue
		}
		opswire := []map[string]any{}
		opswire = append(opswire, item.Ops...)
		updates = append(updates, map[string]any{
			"v":           v,
			"meta":        item.Meta,
			"pathnames":   item.Path,
			"project_ops": opswire,
		})
	}
	// vendor direction: NEWEST-FIRST feed (S1.2 rule)
	for i, j := 0, len(updates)-1; i < j; i, j = i+1, j-1 {
		updates[i], updates[j] = updates[j], updates[i]
	}
	rows := summarizeYjs(updates, map[int][]map[string]any{}, []map[string]any{})
	out := map[string]any{"nextBeforeTimestamp": 0, "updates": rows}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// rootDocPathname — the room's seeded text file path: the project doc's
// rootFolder entity tree (rootFolder[].docs[].{name,_id}, plus nested
// folders) walked for the entity with _id == rootDoc_id. "" when
// unavailable (rows then carry empty pathnames — vendor: invisible, an
// honest state). NOTE: in this stack the mongo `docs` collection is the OT
// content doc (lines/rev) and has no name — the FILE NAME lives in the
// project doc's rootFolder tree (verified live: rootFolder[0].docs[0]).
func rootDocPathname(ctx context.Context, findOne func(ctx context.Context, coll string, id bson.ObjectID) (bson.D, error), pid string) string {
	if findOne == nil {
		return ""
	}
	po, err := bson.ObjectIDFromHex(strings.ToLower(pid))
	if err != nil {
		return ""
	}
	proj, err := findOne(ctx, "projects", po)
	if err != nil || len(proj) == 0 {
		return ""
	}
	var rootDoc string // hex form — compared against tree entity ids
	var rootFolder any
	for _, e := range proj {
		switch e.Key {
		case "rootDoc_id":
			switch v := e.Value.(type) {
			case bson.ObjectID:
				rootDoc = v.Hex()
			case string:
				rootDoc = v
			}
		case "rootFolder":
			rootFolder = e.Value
		}
	}
	if rootDoc == "" || rootFolder == nil {
		return ""
	}
	want := strings.ToLower(rootDoc)
	var walk func(v any) string
	walk = func(v any) string {
		ent, ok := v.(bson.D)
		if !ok {
			if dm, dok := v.(bson.M); dok {
				ent = dmToD(dm)
			} else {
				return ""
			}
		}
		// docs: direct children
		if docs := entArrAny(ent, "docs"); docs != nil {
			for _, de := range docs {
				m, ok := de.(bson.D)
				if !ok {
					continue
				}
				if oid, dok := entField(m, "_id"); dok && strings.ToLower(oid) == want {
					if s, ok := entField(m, "name"); ok {
						return strings.TrimPrefix(s, "/")
					}
				}
			}
		}
		// folders: nested entity roots
		if folders := entArrAny(ent, "folders"); folders != nil {
			for _, f := range folders {
				if r := walk(f); r != "" {
					return r
				}
			}
		}
		return ""
	}
	switch rf := rootFolder.(type) {
	case []any:
		for _, e := range rf {
			if r := walk(e); r != "" {
				return r
			}
		}
	case bson.A:
		for _, e := range rf {
			if r := walk(e); r != "" {
				return r
			}
		}
	case []bson.D:
		for _, e := range rf {
			if r := walk(bson.D(e)); r != "" {
				return r
			}
		}
	case bson.D:
		if r := walk(rf); r != "" {
			return r
		}
	}
	return ""
}

func dmToD(m bson.M) bson.D {
	out := make(bson.D, 0, len(m))
	for k, v := range m {
		out = append(out, bson.E{Key: k, Value: v})
	}
	return out
}

func entField(d bson.D, key string) (string, bool) {
	for _, e := range d {
		if e.Key == key {
			if s, ok := e.Value.(string); ok {
				return s, true
			}
			if o, ok := e.Value.(bson.ObjectID); ok {
				return o.Hex(), true
			}
		}
	}
	return "", false
}

func entArrAny(d bson.D, key string) []any {
	for _, e := range d {
		if e.Key == key {
			switch v := e.Value.(type) {
			case []any:
				return v
			case bson.A:
				return []any(v)
			case []bson.D:
				out := make([]any, len(v))
				for i, x := range v {
					out[i] = x
				}
				return out
			}
		}
	}
	return nil
}

// MongoDocSeeker — adapts a lazy *mongo.Database to the findOne seam so
// the composition layer stays testable without a live MongoDB driver.
func MongoDocSeeker(db *mongo.Database) func(ctx context.Context, coll string, id bson.ObjectID) (bson.D, error) {
	return func(ctx context.Context, coll string, id bson.ObjectID) (bson.D, error) {
		var out bson.D
		err := db.Collection(coll).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&out)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return bson.D{}, nil
			}
			return nil, err
		}
		return out, nil
	}
}

// buildUnifiedFeed — S3a core: sort room versions + tree ops by ts (ties:
// room before yops), assign the unified 1-based index. Shared by the
// /updates composition and the S3b filetree range selection.
// buildUnifiedFeedMulti — the 024 Option B union feed: text versions from
// MULTIPLE rooms (the project's root room + per-doc rooms, with each row's
// pathname) merged with the root room's tree ops, sorted by timestamp and
// given a unified 1..N index. Same row shapes as buildUnifiedFeed so the
// vendor summarizer and S3 replay consume both identically.
func buildUnifiedFeedMulti(rooms []projectRoom, rootPath string, yops []YopMeta) mergedFeed {
	feed := mergedFeed{}
	for _, r := range rooms {
		path := r.Pathname
		if r.IsRoot {
			path = rootPath
		} else if path == "" {
			path = r.Room
		}
		for _, lv := range r.Lversions {
			metaInfo, _ := r.Vmetas[uint64(lv.Version)]
			ts := lv.UpdatedAt.UTC().UnixMilli()
			users := []any{}
			if metaInfo.UID != "" {
				users = []any{metaInfo.UID}
			}
			m := map[string]any{"users": users, "start_ts": ts, "end_ts": ts}
			if o, has := originObject(metaInfo.Origin, path, ts); has {
				m["origin"] = o
			}
			pathnames := []string{}
			if path != "" {
				pathnames = []string{path}
			}
			feed = append(feed, mergedFeedItem{Room: r.Room, V: int(lv.Version), Meta: m, Path: pathnames, Ts: ts, Source: 0})
		}
	}
	for _, yo := range yops {
		ts := yo.At.UTC().UnixMilli()
		users := []any{}
		if yo.UID != "" {
			users = []any{yo.UID}
		}
		m := map[string]any{"users": users, "start_ts": ts, "end_ts": ts}
		feed = append(feed, mergedFeedItem{V: int(yo.V), Meta: m, Ops: []map[string]any{projectOpsWire(yo)}, Ts: ts, Source: 1, Kind: yo.Kind, Pathname: yo.Pathname, NewPath: yo.NewPath})
	}
	sort.Slice(feed, func(i, j int) bool {
		if feed[i].Ts != feed[j].Ts {
			return feed[i].Ts < feed[j].Ts
		}
		if feed[i].Source != feed[j].Source {
			return feed[i].Source < feed[j].Source
		}
		return feed[i].V < feed[j].V
	})
	for i := range feed {
		feed[i].UnifiedV = i + 1
	}
	return feed
}

// projectCovers — true when the UNION feed has at least to-1 timeline
// points (the panel's from/to are unified indices under Option B).
func projectCovers(feed mergedFeed, from, to int) bool {
	return len(feed) >= to-1 && from >= 1
}

func buildUnifiedFeed(lvs []persistence.VersionMeta, vmetas map[uint64]collab.VersionMeta, yops []YopMeta, rootPath string) mergedFeed {
	feed := mergedFeed{}
	for _, lv := range lvs {
		metaInfo, _ := vmetas[uint64(lv.Version)]
		ts := lv.UpdatedAt.UTC().UnixMilli()
		users := []any{}
		if metaInfo.UID != "" {
			users = []any{metaInfo.UID}
		}
		m := map[string]any{"users": users, "start_ts": ts, "end_ts": ts}
		if o, has := originObject(metaInfo.Origin, rootPath, ts); has {
			m["origin"] = o
		}
		pathnames := []string{}
		if rootPath != "" {
			pathnames = []string{rootPath}
		}
		feed = append(feed, mergedFeedItem{V: int(lv.Version), Meta: m, Path: pathnames, Ts: ts, Source: 0})
	}
	for _, yo := range yops {
		ts := yo.At.UTC().UnixMilli()
		users := []any{}
		if yo.UID != "" {
			users = []any{yo.UID}
		}
		m := map[string]any{"users": users, "start_ts": ts, "end_ts": ts}
		feed = append(feed, mergedFeedItem{V: int(yo.V), Meta: m, Ops: []map[string]any{projectOpsWire(yo)}, Ts: ts, Source: 1, Kind: yo.Kind, Pathname: yo.Pathname, NewPath: yo.NewPath})
	}
	sort.Slice(feed, func(i, j int) bool {
		if feed[i].Ts != feed[j].Ts {
			return feed[i].Ts < feed[j].Ts
		}
		if feed[i].Source != feed[j].Source {
			return feed[i].Source < feed[j].Source
		}
		return feed[i].V < feed[j].V
	})
	for i := range feed {
		feed[i].UnifiedV = i + 1
	}
	return feed
}

// rootFolderPaths — ALL doc pathnames in the project doc's rootFolder tree
// (the S3b "initial snapshot" for filetree/diff). Sorted, '/'-joined for
// nested folders, "" entries dropped.
func rootFolderPaths(ctx context.Context, findOne func(ctx context.Context, coll string, id bson.ObjectID) (bson.D, error), pid string) []string {
	if findOne == nil {
		return nil
	}
	po, err := bson.ObjectIDFromHex(strings.ToLower(pid))
	if err != nil {
		return nil
	}
	proj, err := findOne(ctx, "projects", po)
	if err != nil || len(proj) == 0 {
		return nil
	}
	var rootFolder any
	for _, e := range proj {
		if e.Key == "rootFolder" {
			rootFolder = e.Value
		}
	}
	if rootFolder == nil {
		return nil
	}
	var out []string
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		ent, ok := v.(bson.D)
		if !ok {
			if dm, dok := v.(bson.M); dok {
				ent = dmToD(dm)
			} else {
				return
			}
		}
		if docs := entArrAny(ent, "docs"); docs != nil {
			for _, de := range docs {
				m, dok := de.(bson.D)
				if !dok {
					continue
				}
				if name, ok := entField(m, "name"); ok && name != "" {
					out = append(out, prefix+strings.TrimPrefix(name, "/"))
				}
			}
		}
		if folders := entArrAny(ent, "folders"); folders != nil {
			// folder names (from their _name/`name` field if present)
			fname := ""
			if nm, ok := entField(ent, "name"); ok {
				fname = nm + "/"
			}
			for _, f := range folders {
				walk(fname, f)
			}
		}
	}
	switch rf := rootFolder.(type) {
	case []any:
		for _, e := range rf {
			walk("", e)
		}
	case bson.A:
		for _, e := range rf {
			walk("", e)
		}
	case []bson.D:
		for _, e := range rf {
			walk("", bson.D(e))
		}
	case bson.D:
		walk("", rf)
	}
	sort.Strings(out)
	return out
}
