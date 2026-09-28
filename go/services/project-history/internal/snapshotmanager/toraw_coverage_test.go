package snapshotmanager

import (
	"context"
	"testing"
)

func TestLatestSnapshotFullRoundTrip(t *testing.T) {
	chunk := map[string]any{
		"startVersion": 1.0,
		"history": map[string]any{
			"snapshot": map[string]any{"projectVersion": "7", "files": map[string]any{
				"f.tex":   map[string]any{"hash": "h1", "stringLength": 2.0, "metadata": map[string]any{"k": "v"}},
				"img.png": map[string]any{"hash": "abc", "stringLength": nil},
			}},
			"changes": []any{
				map[string]any{"operations": []any{
					map[string]any{"pathname": "f.tex", "textOperation": []any{0, "xyz"}, "newContentHash": "h2"},
				}},
			},
		},
	}
	d := &Deps{GetMostRecentChunk: func(ctx context.Context, p, h string) (map[string]any, error) {
		return chunk, nil
	}}
	snap, version, err := d.GetLatestSnapshotFull(context.Background(), "p", "h")
	if err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("want version 2 (start 1 + 1 change), got %d", version)
	}
	if snap.ProjectVersion != "7" {
		t.Fatalf("want projectVersion 7, got %q", snap.ProjectVersion)
	}
	raw := snap.ToRaw()
	files, _ := raw["files"].(map[string]any)
	if files == nil {
		t.Fatalf("want files map, got %v", raw)
	}
	f, _ := files["f.tex"].(map[string]any)
	if f == nil || f["content"] != "xyz" {
		t.Fatalf("want f.tex content xyz after apply, got %v", f)
	}
	if f["metadata"].(map[string]any)["k"] != "v" {
		t.Fatalf("want metadata clone, got %v", f)
	}
	g, _ := files["img.png"].(map[string]any)
	if g == nil || g["hash"] != "abc" {
		t.Fatalf("want hash file raw, got %v", g)
	}
	if _, ok := g["content"]; ok {
		t.Fatalf("binary file must not expose content: %v", g)
	}
}

func TestToRawOptionalFields(t *testing.T) {
	content := "abc"
	f := &File{
		Pathname:       "b.tex",
		Editable:       true,
		Content:        &content,
		Comments:       []any{map[string]any{"id": 1}},
		TrackedChanges: []any{map[string]any{"id": 2}},
	}
	raw := f.toRaw()
	if raw["content"] != "abc" || raw["comments"] == nil || raw["trackedChanges"] == nil {
		t.Fatalf("want content/comments/trackedChanges, got %v", raw)
	}
	f2 := &File{Pathname: "big.bin", ByteLength: 12, RangesHash: "rr"}
	raw2 := f2.toRaw()
	if raw2["byteLength"] != 12 || raw2["rangesHash"] != "rr" {
		t.Fatalf("want byteLength/rangesHash, got %v", raw2)
	}
	// hash + byteLength coexist (vendor BinaryFileData.toRaw)
	blob := "payload"
	f3 := &File{Pathname: "c.bin", Hash: "h9", ByteLength: 7, Content: &blob, Editable: true}
	raw3 := f3.toRaw()
	if raw3["hash"] != "h9" || raw3["content"] != "payload" {
		t.Fatalf("want hash+content, got %v", raw3)
	}
}

func TestGetLatestSnapshotFullBadRequest(t *testing.T) {
	d := &Deps{GetMostRecentChunk: func(ctx context.Context, p, h string) (map[string]any, error) {
		return nil, nil
	}}
	if _, _, err := d.GetLatestSnapshotFull(context.Background(), "p", "h"); err == nil {
		t.Fatal("want error for nil chunk")
	}
}

func TestRemovePathAndErrors(t *testing.T) {
	chunk := map[string]any{
		"startVersion": 1.0,
		"history": map[string]any{
			"snapshot": map[string]any{"projectVersion": "7", "files": map[string]any{
				"a.tex": map[string]any{"hash": "h1", "stringLength": 1.0},
				"b.tex": map[string]any{"hash": "h2", "stringLength": 1.0},
			}},
			"changes": []any{
				map[string]any{"operations": []any{
					map[string]any{"pathname": "a.tex", "newPathname": ""},
				}},
			},
		},
	}
	d := &Deps{}
	snap, _, _ := d.buildSnapshot(chunk, -1)
	if snap == nil {
		t.Fatal("nil snapshot")
	}
	if _, ok := snap.files["a.tex"]; ok {
		t.Fatalf("a.tex should be removed by the delete op, got files %v", snap.files)
	}
	if !sliceContains(snap.ordered, "b.tex") {
		t.Fatalf("b.tex missing from order: %v", snap.ordered)
	}
	if ph := notFound("p", 3, "nope.tex"); ph == nil {
		t.Fatal("want notFound error")
	}
	if bd := pherrBadRequest("undefined chunk"); bd == nil {
		t.Fatal("want badRequest error")
	}
}

func sliceContains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
