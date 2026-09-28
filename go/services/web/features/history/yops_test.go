package history

// d5dd23dd S3a — tree-op log + merged /updates composition tests.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/reearth/ygo/persistence"

	"ollitex/go/services/collab"
)

func TestYopWireShapes(t *testing.T) {
	r := projectOpsWire(YopMeta{Kind: YopRename, Pathname: "a.md", NewPath: "b.md"})
	if r["pathname"] != "a.md" || r["newPathname"] != "b.md" {
		t.Fatalf("rename wire: %+v", r)
	}
	a := projectOpsWire(YopMeta{Kind: YopAdd, Pathname: "notes/img.png"})
	file, ok := a["file"].(map[string]any)
	if !ok || file["hash"] != "" {
		t.Fatalf("add wire: %+v", a)
	}
	d := projectOpsWire(YopMeta{Kind: YopRemove, Pathname: "old.tex"})
	if d["pathname"] != "old.tex" || len(d) != 1 {
		t.Fatalf("remove wire: %+v", d)
	}
}

func TestMemYopLogAppendList(t *testing.T) {
	l := NewMemYopLog()
	v1, _ := l.Append(context.Background(), YopMeta{Room: "p1", Kind: YopAdd, Pathname: "a.md", UID: "u9"})
	v2, _ := l.Append(context.Background(), YopMeta{Room: "p1", Kind: YopRename, Pathname: "a.md", NewPath: "b.md"})
	list, err := l.List(context.Background(), "p1")
	if err != nil || len(list) != 2 || v1 != 1 || v2 != 2 || list[1].NewPath != "b.md" {
		t.Fatalf("v1=%d v2=%d list=%+v err=%v", v1, v2, list, err)
	}
	other, _ := l.List(context.Background(), "other")
	if len(other) != 0 {
		t.Fatalf("other room: %+v", other)
	}
}

// mergedFeed test — room text versions + tree ops, unified index, vendor merge
// rules (text/file rows never merge; text+text merge within 5 min).
func TestComposeMergedFeed(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	st, _ := persistence.NewFilePersistence(t.TempDir())
	room := "m1"
	if _, err := collab.SeedTextContent(context.Background(), st, room, "hello"); err != nil { // v1 (wallclock ts)
		t.Fatal(err)
	}
	metaLog := collab.NewMemVersionLog()
	if _, err := collab.ClientEdit(context.Background(), st, room, "hello", "hello x"); err != nil { // v2 (wallclock ts, ~ms later)
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	metaLog.Upsert(context.Background(), collab.VersionMeta{Room: room, V: 1, UID: "u1", At: time.UnixMilli(now)})
	metaLog.Upsert(context.Background(), collab.VersionMeta{Room: room, V: 2, UID: "u1", At: time.UnixMilli(now)})
	// tree op AFTER both text versions (the room stamps wallclock, so the op
	// ts must be >= now to stay newest in the unified stream)
	ylog := NewMemYopLog()
	ylog.Append(context.Background(), YopMeta{Room: room, Kind: YopAdd, Pathname: "img/pic.png", UID: "u2", At: time.UnixMilli(now + 60000)})

	got, ok, err := composeMerged(context.Background(), st, metaLog, ylog, room, "main.tex", nil)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	var env map[string]any
	if err := json.Unmarshal(got, &env); err != nil {
		t.Fatal(err)
	}
	rows, _ := env["updates"].([]any)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (merged text + standalone op): %s", string(got))
	}
	raw := string(got)
	// vendor: newest-first → op row (v=3) FIRST, then the merged text row (v1+v2).
	opRow := rows[0].(map[string]any)
	if opRow["fromV"].(float64) != 3 {
		t.Fatalf("op row fromV: %s", string(got))
	}
	ops := opRow["project_ops"].([]any)
	opj, _ := json.Marshal(ops[0])
	for _, want := range []string{`"pathname":"img/pic.png"`, `"file"`, `"hash"`, `"atV":3`} {
		if !strings.Contains(string(opj), want) {
			t.Fatalf("op wire: %s (want %s)", string(opj), want)
		}
	}
	textRow := rows[1].(map[string]any)
	if textRow["fromV"].(float64) != 1 || textRow["toV"].(float64) != 3 {
		t.Fatalf("text row span: %v..%v", textRow["fromV"], textRow["toV"])
	}
	if ops, has := textRow["project_ops"].([]any); has && len(ops) != 0 {
		t.Fatalf("text row must have no ops: %s", string(got))
	}
	_ = raw
}

func TestComposeMergedYopsOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	// A room with NO versions but recorded tree ops must still serve (tree-op
	// history is part of the Yjs plane) — and stay invisible when nothing
	// happened at all.
	st, _ := persistence.NewFilePersistence(t.TempDir())
	room := "y1"
	ylog := NewMemYopLog()
	base := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC).UnixMilli()
	ylog.Append(context.Background(), YopMeta{Room: room, Kind: YopRename, Pathname: "a.md", NewPath: "b.md", UID: "u7", At: time.UnixMilli(base)})
	metaLog := collab.NewMemVersionLog()

	got, ok, err := composeMerged(context.Background(), st, metaLog, ylog, room, "main.tex", nil)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	var env map[string]any
	if err := json.Unmarshal(got, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rows := env["updates"].([]any); len(rows) != 1 {
		t.Fatalf("rows: %s", string(got))
	}
	// nothing at all → legacy fallback
	ylog2 := NewMemYopLog()
	if _, ok2, _ := composeMerged(context.Background(), st, metaLog, ylog2, "empty", "main.tex", nil); ok2 {
		t.Fatal("empty everything must not serve from the Yjs plane")
	}
}

var _ = json.Marshal
