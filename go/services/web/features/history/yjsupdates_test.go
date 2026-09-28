package history

// d5dd23dd S1.2 — Yjs-native /updates composition: vendor merge semantics
// (1:1 vs the PH port) + Yjs feed assembly + envelope discipline.

import (
	"context"
	"strings"
	"testing"

	"ollitex/go/services/collab"

	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const fixPID = "6abaab4caa405ee17acffd08"

// findFixture — projects doc with rootDoc_id → docs doc with a name.
func findFixture(ctx context.Context, coll string, id primitive.ObjectID) (bson.D, error) {
	switch coll {
	case "projects":
		return bson.D{
			{Key: "_id", Value: id},
			{Key: "rootDoc_id", Value: primitive.ObjectID{0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9, 0xa, 0xb, 0xc}},
		}, nil
	case "docs":
		return bson.D{{Key: "_id", Value: id}, {Key: "name", Value: "mainbasic.tex"}}, nil
	}
	return bson.D{}, nil
}

func TestRootDocPathname(t *testing.T) {
	if got := rootDocPathname(context.Background(), findFixture, fixPID); got != "mainbasic.tex" {
		t.Fatalf("pathname = %q, want mainbasic.tex", got)
	}
	if got := rootDocPathname(context.Background(), nil, fixPID); got != "" {
		t.Fatalf("nil seek = %q, want empty", got)
	}
	if got := rootDocPathname(context.Background(), func(ctx context.Context, coll string, id primitive.ObjectID) (bson.D, error) {
		return bson.D{}, nil
	}, fixPID); got != "" {
		t.Fatalf("missing project = %q, want empty", got)
	}
}

// realRoom — n content versions in a throwaway room (seed + n-1 edits),
// the same pattern as the collabhistory hermetic tests.
func realRoom(t *testing.T, n int) (persistence.VersionedPersistence, string) {
	t.Helper()
	st, err := persistence.NewFilePersistence(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	room := fixPID
	if n == 0 {
		return st, room
	}
	text := "one\n"
	if _, err := collab.SeedTextContent(context.Background(), st, room, text); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := 1; i < n; i++ {
		next := text + "two\n"
		if _, err := collab.ClientEdit(context.Background(), st, room, text, next); err != nil {
			t.Fatalf("edit: %v", err)
		}
		text = next
	}
	return st, room
}

// ---------- the vendor S5–S7 core, fed from the Yjs plane ----------

func TestSummarizeYjsSingleVersion(t *testing.T) {
	ts := int64(1700000000000)
	updates := []map[string]any{
		{"v": 1, "meta": map[string]any{"users": []any{"uid1"}, "start_ts": ts, "end_ts": ts}, "pathnames": []string{"mainbasic.tex"}, "project_ops": []map[string]any{}},
	}
	rows := summarizeYjs(updates, nil, nil)
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	r := rows[0]
	if r["fromV"] != 1 || r["toV"] != 2 {
		t.Fatalf("fromV/toV = %v/%v, want 1/2", r["fromV"], r["toV"])
	}
	m := r["meta"].(map[string]any)
	if m["start_ts"] != ts || m["end_ts"] != ts || len(m["users"].([]any)) != 1 {
		t.Fatalf("meta = %#v", m)
	}
	if len(r["labels"].([]map[string]any)) != 0 || len(r["project_ops"].([]map[string]any)) != 0 {
		t.Fatalf("labels/ops should be empty: %#v", r)
	}
}

func TestSummarizeYjsMergeAndSplit(t *testing.T) {
	ts := int64(1700000000000)
	u := func(v int, d int64, users []any, origin map[string]any) map[string]any {
		m := map[string]any{"users": users, "start_ts": ts + d, "end_ts": ts + d}
		if origin != nil {
			m["origin"] = origin
		}
		return map[string]any{"v": v, "meta": m, "pathnames": []string{"mainbasic.tex"}, "project_ops": []map[string]any{}}
	}
	updates := []map[string]any{ // vendor direction: NEWEST-FIRST feed
		u(4, 370000, []any{"a"}, map[string]any{"kind": "file-restore", "path": "mainbasic.tex", "timestamp": ts}),
		u(3, 360000, []any{"a"}, nil),
		u(2, 60000, []any{"b"}, nil),
		u(1, 0, []any{"a"}, nil),
	}
	rows := summarizeYjs(updates, nil, nil)
	// vendor order is NEWEST-FIRST — exact expected shape for these inputs:
	// v4 origin -> own row; v3 split by origin mismatch; v2 split by the
	// 5-min gap (tail.end_ts 360000 - v2.start_ts 60000); v1 merges into v2.
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (%#v)", len(rows), rows)
	}
	rNewest := rows[0]
	if rNewest["fromV"] != 4 || rNewest["toV"] != 5 {
		t.Fatalf("newest row = %v/%v, want 4/5", rNewest["fromV"], rNewest["toV"])
	}
	if o, ok := rNewest["meta"].(map[string]any)["origin"].(map[string]any); !ok || o["kind"] != "file-restore" {
		t.Fatalf("newest row must carry the file-restore origin: %#v", rNewest)
	}
	rMid := rows[1]
	if rMid["fromV"] != 3 || rMid["toV"] != 4 {
		t.Fatalf("mid row = %v/%v, want 3/4", rMid["fromV"], rMid["toV"])
	}
	rOldest := rows[2]
	if rOldest["fromV"] != 1 || rOldest["toV"] != 3 {
		t.Fatalf("oldest row = %v/%v, want 1/3 (v1 merged into v2)", rOldest["fromV"], rOldest["toV"])
	}
	if got := rOldest["meta"].(map[string]any)["users"].([]any); len(got) != 2 {
		t.Fatalf("oldest users = %#v, want union of 2", got)
	}
}

func TestSummarizeYjsEmptyUpdatesInvisible(t *testing.T) {
	// vendor S5: empty updates (no project_ops AND no pathnames) only
	// advance the version state — they never create a row.
	updates := []map[string]any{
		{"v": 1, "meta": map[string]any{"users": []any{}, "start_ts": 1, "end_ts": 1}, "pathnames": []string{}, "project_ops": []map[string]any{}},
		{"v": 2, "meta": map[string]any{"users": []any{"a"}, "start_ts": 2, "end_ts": 2}, "pathnames": []string{"mainbasic.tex"}, "project_ops": []map[string]any{}},
	}
	rows := summarizeYjs(updates, nil, nil)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0]["fromV"] != 2 {
		t.Fatalf("fromV = %v, want 2 (the empty v=1 advances only toV)", rows[0]["fromV"])
	}
}

func TestOriginObjectShapes(t *testing.T) {
	if _, ok := originObject("", "p", 1); ok {
		t.Fatalf("empty origin must be absent")
	}
	fr, ok := originObject("file-restore", "mainbasic.tex", 42)
	if !ok {
		t.Fatalf("file-restore missing")
	}
	if fr["kind"] != "file-restore" || fr["path"] != "mainbasic.tex" || fr["timestamp"] != int64(42) {
		t.Fatalf("file-restore = %#v", fr)
	}
	pr, ok := originObject("project-restore", "mainbasic.tex", 7)
	if !ok || pr["timestamp"] != int64(7) {
		t.Fatalf("project-restore = %#v ok=%v", pr, ok)
	}
	up, ok := originObject("upload", "x", 9)
	if !ok || up["kind"] != "upload" {
		t.Fatalf("upload = %#v", up)
	}
}

func TestBeforeCursor(t *testing.T) {
	st, room := realRoom(t, 3)
	before := 3
	body, ok, err := composeYjsUpdates(context.Background(), st, collab.NewMemVersionLog(), room, "mainbasic.tex", &before)
	if err != nil || !ok {
		t.Fatalf("compose = %v err=%v", ok, err)
	}
	if !strings.Contains(string(body), `"fromV":1`) {
		t.Fatalf("before=3 must keep v=1: %s", body)
	}
	if strings.Contains(string(body), `"fromV":3`) {
		t.Fatalf("before=3 must drop v=3: %s", body)
	}
}

func TestComposeAttributedRestore(t *testing.T) {
	st, room := realRoom(t, 2)
	log := collab.NewMemVersionLog()
	wrapped := &collab.ActorLog{Inner: st, Log: log}
	// a restore (v3) attributed to the session user with origin
	ctx := collab.WithActor(context.Background(), "6aa4b8b573ef0e5094")
	ctx = collab.WithOrigin(ctx, "file-restore")
	if _, err := collab.ClientEdit(ctx, wrapped, room, "one\ntwo\n", "one\ntwo\nthree\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	body, ok, err := composeYjsUpdates(context.Background(), wrapped, log, room, "mainbasic.tex", nil)
	if err != nil || !ok {
		t.Fatalf("compose = %v err=%v", ok, err)
	}
	b := string(body)
	// envelope discipline (legacy V2 key order)
	if !strings.HasPrefix(b, `{"nextBeforeTimestamp":0,"updates":[`) {
		t.Fatalf("envelope = %s", b[:min(80, len(b))])
	}
	if !strings.Contains(b, `"users":["6aa4b8b573ef0e5094"]`) {
		t.Fatalf("attributed row missing: %s", b)
	}
	if !strings.Contains(b, `"kind":"file-restore"`) || !strings.Contains(b, `"path":"mainbasic.tex"`) {
		t.Fatalf("origin object wrong: %s", b)
	}
}

// TestComposeEmptyRoomFallsBack — an empty room reports ok=false so the
// handler keeps the legacy V2 path (byte-identical fallback).
func TestComposeEmptyRoomFallsBack(t *testing.T) {
	st, room := realRoom(t, 0)
	body, ok, err := composeYjsUpdates(context.Background(), st, nil, room, "mainbasic.tex", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if ok {
		t.Fatalf("empty room must report ok=false (legacy fallback)")
	}
	_ = body
}

func TestComposeUnattributedUsersEmpty(t *testing.T) {
	st, room := realRoom(t, 2)
	body, ok, err := composeYjsUpdates(context.Background(), st, collab.NewMemVersionLog(), room, "mainbasic.tex", nil)
	if err != nil || !ok {
		t.Fatalf("compose = %v err=%v", ok, err)
	}
	if !strings.Contains(string(body), `"users":[]`) {
		t.Fatalf("unattributed versions must carry users:[] — %s", body)
	}
}
