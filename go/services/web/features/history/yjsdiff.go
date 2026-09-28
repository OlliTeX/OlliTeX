package history

// d5dd23dd S2 — Yjs-native diff surfaces (D41-b1 approved: "diff = Y.Text
// snapshots at two versions + Go diff").
//
// WIRE (Node PH, verified in the 1:1 Go port): res.json({diff}):
//
//	doc diff (GET /project/:p/doc/:d/diff?from=&to=):
//	  {diff: [ {u: text} | {i|d: text, meta?}, ... ]}
//	filetree diff (GET /project/:p/filetree/diff?from=&to=):
//	  {diff: [ FileDiff, ... ]}   (FileDiff = {pathname, operation} |
//	  {pathname, editable})
//
// Frontend contract (highlights-from-diff-response.ts): every i/d part MUST
// carry meta = {users, start_ts, end_ts, origin?} — it throws "No meta
// found" otherwise and labels highlights with meta.users[0] + meta.end_ts.
//
// VERSION SEMANTIC (pinned in the PH port): the range covers versions
// [from, to) — to is EXCLUSIVE. State before = after version from-1 (""
// when from < 1); state after = after version to-1. The composed update row
// {fromV, toV} maps directly (row.toV = last covered version + 1).
//
// dmp pipeline = Node oracle byte-identical (go/libraries/dmp, promoted
// from the PH 1:1 port; oracle UpdateCompressor L542-543):
//
//	d := dmp.New(); d.DiffTimeout = 0.1
//	diffs := d.DiffMain(before, after); d.CleanupSemantic(&diffs)
//
// AUTHORSHIP: on the Yjs plane authorship is per-VERSION (the version-log
// row), not per-chunk (OT plane). meta = the row of the range's last
// version (to-1): users + ts; origin = the S1.2 origin object shape.
//
// FALLBACK: room v0 (unseeded) or a range extending beyond the room
// (OT-era versions) → legacy V2 pass-through (byte-identical for OT
// projects).

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"ollitex/go/libraries/dmp"
	"ollitex/go/services/collab"

	"github.com/reearth/ygo/persistence"
)

// dmpTimeoutNode — vendor UpdateCompressor L27-28: dmp.Diff_Timeout = 0.1.
const dmpTimeoutNode = 0.1

// parseV — a non-negative integer query param ("" -> 0, Node z.coerce).
func parseV(s string) (int, bool) {
	if s == "" {
		return 0, true
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// vRange — from/to from the query (frontend: from=updateRange.fromV,
// to=updateRange.toV). ok=false when malformed or inverted.
func vRange(get func(string) string) (from, to int, ok bool) {
	f, okF := parseV(get("from"))
	t, okT := parseV(get("to"))
	if !okF || !okT || t < 1 || f > t {
		return 0, 0, false
	}
	return f, t, true
}

// textAt — the room's Y.Text content after version v ("" for v<1).
func textAt(ctx context.Context, st persistence.VersionedPersistence, room string, v int) (string, error) {
	if v < 1 {
		return "", nil
	}
	return collab.TextAt(ctx, st, room, persistence.Version(v))
}

// yjsDocDiff — the {diff: [...]} body for a room version range [from, to).
// metaAt(v) supplies the authored row for version v (nil when unattributed).
func yjsDocDiff(ctx context.Context, st persistence.VersionedPersistence, room string, from, to int, metaAt func(ctx context.Context, v int) map[string]any) (map[string]any, error) {
	before, err := textAt(ctx, st, room, from-1)
	if err != nil {
		return nil, err
	}
	after, err := textAt(ctx, st, room, to-1)
	if err != nil {
		return nil, err
	}
	d := dmp.New()
	d.DiffTimeout = dmpTimeoutNode
	diffs := d.DiffMain(before, after)
	d.CleanupSemantic(&diffs)

	var meta map[string]any
	if metaAt != nil {
		meta = metaAt(ctx, to-1) // the range's last version carries the label
	}
	parts := make([]map[string]any, 0, len(diffs))
	for _, df := range diffs {
		switch df.Op {
		case dmp.Equal:
			parts = append(parts, map[string]any{"u": df.Text})
		case dmp.Insert:
			p := map[string]any{"i": df.Text}
			if meta != nil {
				p["meta"] = meta
			}
			parts = append(parts, p)
		case dmp.Delete:
			p := map[string]any{"d": df.Text}
			if meta != nil {
				p["meta"] = meta
			}
			parts = append(parts, p)
		}
	}
	return map[string]any{"diff": parts}, nil
}

// yjsFiletreeDiff — the {diff: [FileDiff]} wire over a single-file (root
// doc) room: edited when the text changed, unchanged otherwise. Tree
// operations (added/removed/renamed) arrive with the S3 ops stream.
func yjsFiletreeDiff(rootDoc string, changed bool) map[string]any {
	if changed {
		return map[string]any{"diff": []map[string]any{{"pathname": rootDoc, "operation": "edited"}}}
	}
	return map[string]any{"diff": []map[string]any{{"pathname": rootDoc, "editable": true}}}
}

// roomCovers — true when the whole [from, to) range is covered by room
// versions (room non-empty, to-1 <= max, from <= max+1).
func roomCovers(ctx context.Context, st persistence.VersionedPersistence, room string, from, to int) (bool, error) {
	okMax, err := st.Load(ctx, room)
	if err != nil {
		return false, err
	}
	if okMax.Version == 0 {
		return false, nil
	}
	return to-1 <= int(okMax.Version) && from <= int(okMax.Version)+1, nil
}

// vlogMetaFor — the authored row wire meta for room version v (nil when the
// version log has no entry — unattributed), same shape as S1.2 rows.
func vlogMetaFor(ctx context.Context, vlog collab.Log, room, rootPath string, v int) map[string]any {
	if vlog == nil || v < 1 {
		return nil
	}
	m, ok, err := vlog.Get(ctx, room, uint64(v))
	if err != nil || !ok {
		return nil
	}
	users := []any{}
	if m.UID != "" {
		users = []any{m.UID}
	}
	ats := m.At.UnixMilli()
	out := map[string]any{"users": users, "start_ts": ats, "end_ts": ats}
	if o, has := originObject(m.Origin, rootPath, ats); has {
		out["origin"] = o
	}
	return out
}

// ---------- S3b: full tree-op replay (Node FileTreeDiffGenerator shapes) ----------

// yjsFiletreeDiffS3 — the {diff: [FileDiff]} wire for [from,to) over the
// UNIFIED stream (room text versions + tree ops), Node-oracle shapes:
//
//	added     {pathname, operation:"added", editable}
//	removed   {pathname, operation:"removed", editable, deletedAtV:<unified v>}
//	renamed   {pathname:<old>, newPathname, operation:"renamed", editable}
//	edited    {pathname, operation:"edited"}            (no editable — vendor: edit implies editable)
//	unchanged {pathname, editable}
//
// Entry order: the initial pathnames (sorted), then ops in unified order —
// matching the generator's "initial tree, then appended ops" entry order.
func yjsFiletreeDiffS3(initial []string, feed mergedFeed, from, to int, rootEdited bool) map[string]any {
	type acc struct {
		entry map[string]any
		pos   int // order rank: initial = sorted idx; ops = 1000+v
	}
	byPath := map[string]*acc{}
	order := []string{}
	for i, p := range initial {
		if p == "" {
			continue
		}
		a := &acc{entry: map[string]any{"pathname": p, "editable": yjsEditable(p)}, pos: i}
		byPath[p] = a
		order = append(order, p)
	}
	for _, it := range feed {
		if it.Source != 1 {
			continue
		}
		v := it.UnifiedV
		if v < from || v >= to {
			continue
		}
		switch it.Kind {
		case YopAdd:
			a := &acc{entry: map[string]any{"pathname": it.Pathname, "operation": "added", "editable": yjsEditable(it.Pathname)}, pos: 1000 + v}
			byPath[it.Pathname] = a
			if _, ok := orderMap(order, it.Pathname); !ok {
				order = append(order, it.Pathname)
			}
		case YopRemove:
			if a, ok := byPath[it.Pathname]; ok {
				a.entry = map[string]any{"pathname": it.Pathname, "operation": "removed", "editable": yjsEditable(it.Pathname), "deletedAtV": v}
				a.pos = 1000 + v
			}
		case YopRename:
			if a, ok := byPath[it.Pathname]; ok {
				a.entry = map[string]any{"pathname": it.Pathname, "newPathname": it.NewPath, "operation": "renamed", "editable": yjsEditable(it.Pathname)}
				a.pos = 1000 + v
				if _, ok := orderMap(order, it.Pathname); !ok {
					order = append(order, it.Pathname)
				}
			}
		}
	}
	// the root doc's text edit (only if not already represented by a
	// stronger op on it)
	if rootEdited {
		// locate the edited doc = the room's seeded text file; the entry
		// wins only when it has no operation yet (unchanged) — vendor
		// precedence: removed > added > renamed > edited > unchanged
		for _, p := range initial {
			if p == "" {
				continue
			}
			if a, ok := byPath[p]; ok && a.entry["operation"] == nil {
				a.entry = map[string]any{"pathname": p, "operation": "edited"}
			}
			break // single-doc room text (S3 scope)
		}
	}
	diffs := []map[string]any{}
	seen := map[string]bool{}
	rank := map[string]int{}
	for _, a := range byPath {
		rank[a.entry["pathname"].(string)] = a.pos
	}
	sort.SliceStable(order, func(i, j int) bool {
		return rank[order[i]] < rank[order[j]]
	})
	for _, p := range order {
		if seen[p] {
			continue
		}
		seen[p] = true
		diffs = append(diffs, byPath[p].entry)
	}
	return map[string]any{"diff": diffs}
}

func orderMap(s []string, want string) (string, bool) {
	for _, x := range s {
		if x == want {
			return x, true
		}
	}
	return "", false
}

// yjsEditable — the file-editability signal (Node File.isEditable()): text
// file types are editable; known binary types are not. Unknown → editable
// (overleaf defaults to string content).
func yjsEditable(pathname string) bool {
	name := strings.ToLower(pathname)
	ext := ""
	if i := strings.LastIndex(name, "."); i >= 0 {
		ext = name[i+1:]
	}
	switch ext {
	case "pdf", "png", "jpg", "jpeg", "gif", "bmp", "tiff", "eps", "ps", "ai",
		"psd", "mp3", "mp4", "wav", "zip", "gz", "tgz", "tar", "7z", "rar",
		"doc", "docx", "xls", "xlsx", "ppt", "pptx", "epub", "exe", "ttf",
		"otf", "woff", "woff2", "eot", "ico":
		return false
	}
	return true
}
