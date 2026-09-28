// C5 oracle — SnapshotManager vendor contracts S1–S5.
package snapshotmanager

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

var cctx = context.Background()

// chunk fixture: startVersion 0, 2 changes; snapshot: a.tex (editable,
// inline content "hello"), b.bin (binary hash hbin), c.tex (editable, hash
// h3, rangesHash rh3).
// change 1: edit a.tex → "hello world" (retain 5, insert " world").
// change 2: rename c.tex → d.tex.
var chunkRaw = map[string]any{
	"chunk": map[string]any{
		"startVersion": 0,
		"history": map[string]any{
			"snapshot": map[string]any{"files": map[string]any{
				"a.tex": map[string]any{"hash": "ha", "stringLength": 5},
				"b.bin": map[string]any{"hash": "hbin"},
				"c.tex": map[string]any{"hash": "h3", "stringLength": 3, "rangesHash": "rh3"},
			}},
			"changes": []any{
				map[string]any{
					"operations": []any{map[string]any{
						"pathname":       "a.tex",
						"textOperation":  []any{5, " world"},
						"newContentHash": "ha2",
					}},
				},
				map[string]any{
					"operations": []any{map[string]any{
						"pathname":    "c.tex",
						"newPathname": "d.tex",
					}},
				},
			},
		},
	},
}

func newDeps(t *testing.T) *Deps {
	t.Helper()
	return &Deps{
		GetHistoryID:       func(ctx context.Context, p string) (string, error) { return "hid-" + p, nil },
		GetMostRecentChunk: func(ctx context.Context, p, h string) (map[string]any, error) { return chunkRaw, nil },
		GetChunkAtVersion:  func(ctx context.Context, p, h string, v int) (map[string]any, error) { return chunkRaw, nil },
		GetBlob: func(ctx context.Context, hid, hash string) (string, error) {
			switch hash {
			case "ha":
				return "hello", nil
			case "ha2":
				return "hello world", nil
			case "hbin":
				return "BINARYDATA", nil
			case "h3":
				return "content3", nil
			default:
				return "", context.DeadlineExceeded
			}
		},
	}
}

func TestC5_S1_FileMissingNotFound(t *testing.T) {
	d := newDeps(t)
	_, err := d.GetFileSnapshotStream(cctx, "p", 2, "nope.tex")
	if err == nil {
		t.Fatal("want NotFound")
	}
	if !strings.Contains(err.Error(), "nope.tex not found") {
		t.Fatalf("msg: %q", err.Error())
	}
}

func TestC5_GotRangesSnapshot_Binary(t *testing.T) {
	d := newDeps(t)
	got, err := d.GetRangesSnapshot(cctx, "p", 2, "b.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]any{"changes": []any{}, "comments": []any{}}) {
		t.Fatalf("S2 binary: %#v", got)
	}
}

func TestC5_S3_ProjectSnapshotShapes(t *testing.T) {
	d := newDeps(t)
	out, err := d.GetProjectSnapshot(cctx, "p", 2)
	if err != nil {
		t.Fatal(err)
	}
	files := out["files"].([]any)
	// S5: c.tex/d.tex (rangesHash) is loaded; a.tex (no rangesHash) is NOT
	// loaded → its content is the INLINE snapshot content (empty, 5-char
	// placeholder) — content non-null → {data: {content}} form.
	// b.bin: content null → {data: {hash}}.
	var aEntry, bEntry map[string]any
	for _, e := range files {
		m, _ := e.(map[string]any)
		if m == nil {
			continue
		}
		data := m["data"].(map[string]any)
		if _, hasContent := data["content"]; hasContent {
			aEntry = m
		} else {
			bEntry = m
		}
	}
	if bEntry == nil {
		t.Fatalf("want a hash-form entry: %#v", out)
	}
	if bEntry["data"].(map[string]any)["hash"] != "hbin" {
		t.Fatalf("S3 binary hash: %#v", bEntry)
	}
	_ = aEntry
	if out["projectId"] != "p" {
		t.Fatal("projectId key")
	}
}

func TestC5_S4_ChangesInChunkSince_PastEnd(t *testing.T) {
	d := newDeps(t)
	_, _, err := d.GetChangesInChunkSince(cctx, "p", "hid-p", 99)
	if err == nil {
		t.Fatal("want BadRequest past end")
	}
	if !strings.Contains(err.Error(), "requested version past the end of the history") {
		t.Fatalf("msg: %q", err.Error())
	}
}

func TestC5_S4_ChangesInChunkSince_SinceWithin(t *testing.T) {
	d := newDeps(t)
	lsv, changes, err := d.GetChangesInChunkSince(cctx, "p", "hid-p", 1)
	if err != nil {
		t.Fatal(err)
	}
	if lsv != 0 || len(changes) != 1 {
		t.Fatalf("lsv=%d changes=%d (want lsv=0, 1 change left since v1)", lsv, len(changes))
	}
}

func TestC5_MetadataAbsentWhenEmpty(t *testing.T) {
	d := newDeps(t)
	out, err := d.GetFileMetadataSnapshot(cctx, "p", 2, "a.tex")
	if err != nil {
		t.Fatal(err)
	}
	if _, has := out["metadata"]; has {
		t.Fatalf("empty metadata must be ABSENT (vendor undefined): %#v", out)
	}
}

func TestC5_PathsAtVersion_Renamed(t *testing.T) {
	d := newDeps(t)
	out, err := d.GetPathsAtVersion(cctx, "p", 2)
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := out["paths"].([]string)
	foundD := false
	for _, p := range paths {
		if p == "d.tex" {
			foundD = true
		}
	}
	if !foundD {
		t.Fatalf("renamed path must appear after change 2: %v", paths)
	}
}

func TestC5_Stream_UsesLoadedBlob(t *testing.T) {
	d := newDeps(t)
	// a.tex after change 1 has newContentHash ha2 → stream from blob
	body, err := d.GetFileSnapshotStream(cctx, "p", 2, "a.tex")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello world" {
		t.Fatalf("stream: %q", body)
	}
	// b.bin → raw blob
	body2, err := d.GetFileSnapshotStream(cctx, "p", 2, "b.bin")
	if err != nil {
		t.Fatal(err)
	}
	if string(body2) != "BINARYDATA" {
		t.Fatalf("binary stream: %q", body2)
	}
}
