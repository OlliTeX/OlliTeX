package history

// d5dd23dd S2 — Yjs-native diff surfaces: dmp parts shape, version-range
// semantics ([from, to)-exclusive), meta authorship, filetree wire,
// fallback boundaries.

import (
	"context"
	"testing"
	"time"

	"ollitex/go/services/collab"
)

func TestYjsDocDiffInsert(t *testing.T) {
	st, room := realRoom(t, 2) // v1 = "one\n", v2 = "one\ntwo\n"
	meta := map[string]any{"users": []any{"uid9"}, "start_ts": 5, "end_ts": 5}
	body, err := yjsDocDiff(context.Background(), st, room, 2, 3, func(ctx context.Context, v int) map[string]any { return meta })
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	parts := body["diff"].([]map[string]any)
	// Node range [2,3): state after v1 ("one\n") -> state after v2 ("one\ntwo\n")
	var foundI, foundU bool
	for _, p := range parts {
		if s, ok := p["i"].(string); ok && s == "two\n" {
			foundI = true
			if m, ok := p["meta"].(map[string]any); !ok {
				t.Fatalf("insert part missing meta: %#v", p)
			} else if len(m["users"].([]any)) != 1 {
				t.Fatalf("meta = %#v", m)
			}
		}
		if s, ok := p["u"].(string); ok && s == "one\n" {
			foundU = true
		}
	}
	if !foundI || !foundU {
		t.Fatalf("parts = %#v (want u:\"one\\n\" + i:\"two\\n\")", parts)
	}
}

func TestYjsDocDiffFromZero(t *testing.T) {
	// [0, 3): state before = "" -> state after v2; the whole text is insert.
	st, room := realRoom(t, 2)
	body, err := yjsDocDiff(context.Background(), st, room, 0, 3, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	parts := body["diff"].([]map[string]any)
	var all string
	for _, p := range parts {
		if s, ok := p["u"].(string); ok {
			all += s
		}
		if s, ok := p["i"].(string); ok {
			all += s
		}
	}
	if all != "one\ntwo\n" {
		t.Fatalf("reconstructed text = %q, want one\\ntwo\\n", all)
	}
}

func TestYjsDocDiffUnchanged(t *testing.T) {
	st, room := realRoom(t, 2)
	// [2,2) — zero-width range: before == after == state after v1 ->
	// a single u part with that text.
	body, err := yjsDocDiff(context.Background(), st, room, 2, 2, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	parts := body["diff"].([]map[string]any)
	if len(parts) != 1 {
		t.Fatalf("parts = %#v, want 1 u-part", parts)
	}
	if u, _ := parts[0]["u"].(string); u != "one\n" {
		t.Fatalf("u = %q, want one\n", u)
	}
}

func TestYjsFiletreeDiffShapes(t *testing.T) {
	edit := yjsFiletreeDiff("mainbasic.tex", true)
	d := edit["diff"].([]map[string]any)
	if d[0]["pathname"] != "mainbasic.tex" || d[0]["operation"] != "edited" {
		t.Fatalf("edited = %#v", d)
	}
	if _, has := d[0]["editable"]; has {
		t.Fatalf("FileEdited has no editable field: %#v", d)
	}
	unch := yjsFiletreeDiff("mainbasic.tex", false)
	d2 := unch["diff"].([]map[string]any)
	if d2[0]["editable"] != true || d2[0]["operation"] != nil {
		t.Fatalf("unchanged = %#v", d2)
	}
}

func TestVlogMetaFor(t *testing.T) {
	log := collab.NewMemVersionLog()
	if err := log.Upsert(context.Background(), collab.VersionMeta{
		Room: "r1", V: 3, UID: "6aa4b8b573ef0e5094", Origin: "file-restore", At: time.Unix(1700000000, 0),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	ctx := context.Background()
	m := vlogMetaFor(ctx, log, "r1", "mainbasic.tex", 3)
	if m == nil {
		t.Fatalf("missing meta")
	}
	if len(m["users"].([]any)) != 1 || m["end_ts"] != int64(1700000000000) {
		t.Fatalf("meta = %#v", m)
	}
	o := m["origin"].(map[string]any)
	if o["kind"] != "file-restore" || o["path"] != "mainbasic.tex" {
		t.Fatalf("origin = %#v", o)
	}
	// unattributed version -> nil (caller leaves meta absent)
	if got := vlogMetaFor(ctx, log, "r1", "mainbasic.tex", 9); got != nil {
		t.Fatalf("want nil, got %#v", got)
	}
	// nil vlog -> nil
	if got := vlogMetaFor(ctx, nil, "r1", "mainbasic.tex", 3); got != nil {
		t.Fatalf("want nil (nil vlog), got %#v", got)
	}
}

func TestRoomCoversFallbacks(t *testing.T) {
	ctx := context.Background()
	emptySt, emptyRoom := realRoom(t, 0)
	if ok, _ := roomCovers(ctx, emptySt, emptyRoom, 0, 1); ok {
		t.Fatalf("empty room must not cover any range")
	}
	st, room := realRoom(t, 3)
	if ok, _ := roomCovers(ctx, st, room, 1, 3); !ok {
		t.Fatalf("range [1,3) must be covered by a 3-version room")
	}
	if ok, _ := roomCovers(ctx, st, room, 2, 99); ok {
		t.Fatalf("range beyond the room must fall back")
	}
}

func TestVDropWhenInvalid(t *testing.T) {
	// missing from/to: Node z.coerce.number() on "" -> NaN -> 400, so the
	// Yjs branch must reject (the handler falls through to the Node 400).
	if _, _, ok := vRange(func(string) string { return "" }); ok {
		t.Fatalf("missing from/to must reject")
	}
	q := map[string]string{}
	get := func(k string) string { return q[k] }
	q["from"] = "abc"
	if _, _, ok := vRange(get); ok {
		t.Fatalf("from=abc must reject")
	}
	q["from"] = "5"
	q["to"] = "2"
	if _, _, ok := vRange(get); ok {
		t.Fatalf("inverted range must reject")
	}
}

// ---------- S3b: tree-op replay shapes (Node FileTreeDiffGenerator oracle) ----------

func ftFeed(items ...mergedFeedItem) mergedFeed { return mergedFeed(items) }

func TestFiletreeDiffS3Added(t *testing.T) {
	feed := ftFeed(
		mergedFeedItem{UnifiedV: 1, Source: 0, Meta: map[string]any{"start_ts": 1, "end_ts": 1, "users": []any{}}},
		mergedFeedItem{UnifiedV: 2, Source: 1, Kind: YopAdd, Pathname: "img/pic.png"},
		mergedFeedItem{UnifiedV: 3, Source: 1, Kind: YopAdd, Pathname: "notes/todo.md"},
	)
	out := yjsFiletreeDiffS3([]string{"main.tex"}, feed, 1, 4, nil)
	diffs := out["diff"].([]map[string]any)
	if len(diffs) != 3 {
		t.Fatalf("want 3 entries: %+v", diffs)
	}
	// initial first (main.tex, unchanged), then ops in unified order
	if diffs[0]["pathname"] != "main.tex" {
		t.Fatalf("entry0: %+v", diffs[0])
	}
	if _, has := diffs[0]["operation"]; has || diffs[0]["editable"] != true {
		t.Fatalf("main.tex unchanged shape: %+v", diffs[0])
	}
	if diffs[1]["operation"] != "added" || diffs[1]["pathname"] != "img/pic.png" || diffs[1]["editable"] != false {
		t.Fatalf("png added shape: %+v", diffs[1])
	}
	if diffs[2]["operation"] != "added" || diffs[2]["pathname"] != "notes/todo.md" || diffs[2]["editable"] != true {
		t.Fatalf("md added shape: %+v", diffs[2])
	}
}

func TestFiletreeDiffS3Removed(t *testing.T) {
	feed := ftFeed(
		mergedFeedItem{UnifiedV: 1, Source: 0, Meta: map[string]any{}},
		mergedFeedItem{UnifiedV: 2, Source: 1, Kind: YopRemove, Pathname: "old/draft.tex"},
	)
	out := yjsFiletreeDiffS3([]string{"main.tex", "old/draft.tex"}, feed, 1, 3, nil)
	diffs := out["diff"].([]map[string]any)
	byName := map[string]map[string]any{}
	for _, d := range diffs {
		byName[d["pathname"].(string)] = d
	}
	rem := byName["old/draft.tex"]
	if rem["operation"] != "removed" || rem["deletedAtV"] != 2 || rem["editable"] != true {
		t.Fatalf("removed shape: %+v", rem)
	}
	if ed, has := byName["main.tex"]["operation"]; has || ed != nil {
		t.Fatalf("main.tex should stay unchanged: %+v", byName["main.tex"])
	}
}

func TestFiletreeDiffS3Renamed(t *testing.T) {
	feed := ftFeed(mergedFeedItem{UnifiedV: 2, Source: 1, Kind: YopRename, Pathname: "a.md", NewPath: "b.md"})
	out := yjsFiletreeDiffS3([]string{"a.md", "main.tex"}, feed, 1, 3, nil)
	diffs := out["diff"].([]map[string]any)
	byName := map[string]map[string]any{}
	for _, d := range diffs {
		byName[d["pathname"].(string)] = d
	}
	rn := byName["a.md"]
	if rn["operation"] != "renamed" || rn["newPathname"] != "b.md" || rn["editable"] != true {
		t.Fatalf("renamed shape: %+v", rn)
	}
	if _, has := byName["b.md"]; has {
		t.Fatalf("renamed entry keys off the OLD pathname: %+v", diffs)
	}
}

func TestFiletreeDiffS3RootEdited(t *testing.T) {
	// no ops, text changed → legacy single-doc shape stays byte-identical
	feed := ftFeed(mergedFeedItem{UnifiedV: 1, Source: 0, Meta: map[string]any{}})
	out := yjsFiletreeDiffS3([]string{"main.tex"}, feed, 1, 2, map[string]bool{"main.tex": true})
	diffs := out["diff"].([]map[string]any)
	if len(diffs) != 1 || diffs[0]["operation"] != "edited" || diffs[0]["pathname"] != "main.tex" {
		t.Fatalf("edited shape: %+v", diffs)
	}
	if _, has := diffs[0]["editable"]; has {
		t.Fatalf("edited must NOT carry editable (vendor: edit implies editable): %+v", diffs[0])
	}
}

func TestYjsEditable(t *testing.T) {
	for _, c := range []struct {
		p string
		b bool
	}{{"main.tex", true}, {"notes/IMG.PNG", false}, {"a.pdf", false}, {"code.py", true}, {"noext", true}, {"x.zip", false}} {
		if got := yjsEditable(c.p); got != c.b {
			t.Fatalf("yjsEditable(%q)=%v want %v", c.p, got, c.b)
		}
	}
}
