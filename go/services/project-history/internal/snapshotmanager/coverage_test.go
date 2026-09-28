package snapshotmanager

import (
	"context"
	"testing"
)

func TestC5_AddFileOp(t *testing.T) {
	chunk := map[string]any{
		"startVersion": 0,
		"history": map[string]any{
			"snapshot": map[string]any{"files": map[string]any{}},
			"changes": []any{
				map[string]any{"operations": []any{
					map[string]any{"pathname": "new.tex", "file": map[string]any{
						"hash": "hn", "stringLength": 7, "metadata": map[string]any{"owner": "u1"},
					}},
				}},
			},
		},
	}
	d := &Deps{GetHistoryID: func(ctx context.Context, p string) (string, error) { return "h", nil }}
	snap, sv, ev := d.buildSnapshot(chunk, -1)
	if sv != 0 || ev != 1 {
		t.Fatalf("versions: %d %d", sv, ev)
	}
	f := snap.GetFile("new.tex")
	if f == nil || f.Hash != "hn" || f.Metadata["owner"] != "u1" || !f.Editable {
		t.Fatalf("%#v", f)
	}
}

func TestC5_RenameToAbsentSource(t *testing.T) {
	_ = (&Deps{}).withDefaults()
	snap := &Snapshot{files: map[string]*File{}}
	snap.applyOp(map[string]any{"pathname": "gone.tex", "newPathname": "target.tex"})
	if snap.GetFile("target.tex") == nil {
		t.Fatal("rename of absent source creates an empty file state")
	}
	snap.applyOp(map[string]any{"pathname": "gone.tex", "newPathname": ""})
	if len(snap.ordered) == 0 {
		t.Fatal("delete of absent source is a no-op")
	}
}

func TestC5_TextOpInsertFirst(t *testing.T) {
	_ = (&Deps{}).withDefaults()
	snap := &Snapshot{files: map[string]*File{"a": {Pathname: "a", Editable: true, Content: nil}}}
	snap.applyOp(map[string]any{"pathname": "a", "textOperation": []any{"start ", 0}})
	f := snap.GetFile("a")
	if f.Content == nil || *f.Content != "start " {
		t.Fatalf("content: %q", toStr(f.Content))
	}
}

func toStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestC5_LoadFileErrorPropagates(t *testing.T) {
	d := &Deps{
		GetHistoryID: func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetBlob:      func(ctx context.Context, hid, hash string) (string, error) { return "", context.Canceled },
	}
	snap := &Snapshot{files: map[string]*File{"a": {Pathname: "a", Hash: "h1", Editable: true}}}
	if err := d.loadFile(cctx, "h", snap.GetFile("a")); err == nil {
		t.Fatal("want blob error")
	}
	// second call returns the cached error (loaded flag)
	if err := d.loadFile(cctx, "h", snap.GetFile("a")); err == nil {
		t.Fatal("want cached blob error")
	}
	err := d.loadFile(cctx, "h", &File{Pathname: "b", Editable: false})
	if err != nil {
		t.Fatalf("non-editable load: %v", err)
	}
}

func TestC5_LatestErrors(t *testing.T) {
	d := &Deps{
		GetMostRecentChunk: func(ctx context.Context, p, h string) (map[string]any, error) { return nil, nil },
	}
	if _, err := d.GetLatestSnapshot(cctx, "p", "h"); err == nil {
		t.Fatal("want undefined chunk error")
	}
	if _, err := d.GetLatestSnapshotFiles(cctx, "p", "h"); err == nil {
		t.Fatal("want undefined chunk error")
	}
	if _, _, err := d.GetChangesInChunkSince(cctx, "p", "h", 1); err == nil {
		t.Fatal("want undefined chunk error")
	}
}

func TestC5_SinceBeforeLatestStartFallsBack(t *testing.T) {
	d := newDeps(t)
	oldest := map[string]any{
		"chunk": map[string]any{
			"startVersion": 0,
			"history": map[string]any{
				"snapshot": map[string]any{"files": map[string]any{"a": map[string]any{"hash": "ha"}}},
				"changes":  []any{},
			},
		},
	}
	d.GetChunkAtVersion = func(ctx context.Context, p, h string, v int) (map[string]any, error) { return oldest, nil }
	lsv, changes, err := d.GetChangesInChunkSince(cctx, "p", "hid-p", -1)
	if err != nil {
		t.Fatal(err)
	}
	if lsv != 0 || len(changes) != 0 {
		t.Fatalf("lsv=%d n=%d", lsv, len(changes))
	}
}

func TestC5_RangesSeamAndMetadata(t *testing.T) {
	d := newDeps(t)
	d.Ranges = func(ctx context.Context, hid string, f *File) ([]any, []any) {
		return []any{map[string]any{"text": "x"}}, []any{map[string]any{"text": "y"}}
	}
	got, err := d.GetRangesSnapshot(cctx, "p", 2, "d.tex") // c.tex was renamed by change 2
	if err != nil {
		t.Fatal(err)
	}
	if len(got["changes"].([]any)) != 1 || len(got["comments"].([]any)) != 1 {
		t.Fatalf("%#v", got)
	}
	// metadata present when non-empty
	chunk := map[string]any{
		"chunk": map[string]any{
			"startVersion": 0,
			"history": map[string]any{
				"snapshot": map[string]any{"files": map[string]any{"m.tex": map[string]any{"hash": "hm", "stringLength": 1, "metadata": map[string]any{"k": "v"}}}},
				"changes":  []any{},
			},
		},
	}
	d.GetChunkAtVersion = func(ctx context.Context, p, h string, v int) (map[string]any, error) { return chunk, nil }
	metaOut, err := d.GetFileMetadataSnapshot(cctx, "p", 1, "m.tex")
	if err != nil {
		t.Fatal(err)
	}
	if metaOut["metadata"].(map[string]any)["k"] != "v" {
		t.Fatalf("%#v", metaOut)
	}
}

func TestC5_NilChunkAndErroringStore(t *testing.T) {
	d := &Deps{
		GetHistoryID:       func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetChunkAtVersion:  func(ctx context.Context, p, h string, v int) (map[string]any, error) { return nil, nil },
		GetMostRecentChunk: func(ctx context.Context, p, h string) (map[string]any, error) { return nil, context.Canceled },
	}
	if err := d.loadFile(cctx, "h", &File{Editable: true, Hash: "x"}); err == nil {
		t.Fatal("want err")
	}
	// snapshotAtVersion: a nil chunk → 'undefined chunk' error (vendor)
	if _, _, _, err := d.snapshotAtVersion(cctx, "p", 1); err == nil {
		t.Fatal("want undefined-chunk error")
	}
	// erroring store propagates to GetLatestSnapshot
	if _, err := d.GetLatestSnapshot(cctx, "p", "h"); err == nil {
		t.Fatal("want store error")
	}
	if _, err := d.GetLatestSnapshotFiles(cctx, "p", "h"); err == nil {
		t.Fatal("want store error")
	}
	if _, _, err := d.GetChangesInChunkSince(cctx, "p", "h", 1); err == nil {
		t.Fatal("want store error")
	}
	// GetHistoryID error paths
	d2 := &Deps{GetHistoryID: func(ctx context.Context, p string) (string, error) { return "", context.Canceled }}
	if _, err := d2.GetFileSnapshotStream(cctx, "p", 1, "a"); err == nil {
		t.Fatal("want hid error")
	}
	if _, err := d2.GetRangesSnapshot(cctx, "p", 1, "a"); err == nil {
		t.Fatal("want hid error")
	}
	if _, err := d2.GetProjectSnapshot(cctx, "p", 1); err == nil {
		t.Fatal("want hid error")
	}
	d3 := &Deps{}
	if _, err := d3.GetFileSnapshotStream(cctx, "p", 1, "a"); err == nil {
		t.Fatal("want store-missing path")
	}
	if _, err := d3.GetRangesSnapshot(cctx, "p", 1, "a"); err == nil {
		t.Fatal("want store-missing path")
	}
	if _, err := d3.GetPathsAtVersion(cctx, "p", 1); err == nil {
		t.Fatal("want store-missing path")
	}
	if _, err := d3.GetFileMetadataSnapshot(cctx, "p", 1, "a"); err == nil {
		// file missing → notFound
		t.Fatal("want not found")
	}
	if _, err := d3.GetProjectSnapshot(cctx, "p", 1); err == nil {
		// un-wired store seam → the vendor awaits REJECT → error propagates
		t.Fatal("want store-seam error propagation")
	}
}
