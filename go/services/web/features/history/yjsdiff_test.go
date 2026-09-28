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
