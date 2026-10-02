package outputfilefinder

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func makeTree(t *testing.T, dir string) {
	mk := func(rel string) {
		full := filepath.Join(dir, rel)
		_ = os.Remove(full)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		if err := os.WriteFile(full, []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	mk(filepath.Join("resource", "path.tex"))
	mk("output.pdf")
	mk(filepath.Join("extra", "file.tex"))
	if err := os.Symlink("../foo", filepath.Join(dir, "sneaky-file")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
}

func TestFindOutputFilesShape(t *testing.T) {
	dir := t.TempDir()
	makeTree(t, dir)

	res, err := FindOutputFiles([]Resource{{Path: "resource/path.tex"}}, dir)
	if err != nil {
		t.Fatalf("find: %v", err)
	}

	// Membership of OutputFiles (Node test asserts deep.members — a set).
	gotPaths := map[string]string{}
	for _, f := range res.OutputFiles {
		gotPaths[f.Path] = f.Type
	}
	if len(gotPaths) != 2 {
		t.Fatalf("want exactly 2 output files, got %#v", gotPaths)
	}
	if gotPaths["output.pdf"] != "pdf" {
		t.Errorf("output.pdf type = %q, want pdf", gotPaths["output.pdf"])
	}
	if gotPaths["extra/file.tex"] != "tex" {
		t.Errorf("extra/file.tex type = %q, want tex", gotPaths["extra/file.tex"])
	}

	// allEntries set membership: files + dirs + symlink.
	set := map[string]bool{}
	for _, e := range res.AllEntries {
		set[e] = true
	}
	for _, want := range []string{"extra/file.tex", "extra/", "output.pdf", "resource/path.tex", "resource/", "sneaky-file"} {
		if !set[want] {
			t.Errorf("allEntries missing %q (go: %#v)", want, res.AllEntries)
		}
	}
	// Resource paths must NOT be in OutputFiles but MUST be in allEntries.
	if gotPaths["resource/path.tex"] != "" {
		t.Error("resource/path.tex must not be an output file")
	}
	if !set["resource/path.tex"] {
		t.Error("allEntries must contain resource/path.tex")
	}
}

func TestFindOutputFilesSortedOrderNote(t *testing.T) {
	dir := t.TempDir()
	makeTree(t, dir)
	res, err := FindOutputFiles(nil, dir)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	// Determinism check: sort and compare to the sorted Node expectation.
	entries := append([]string{}, res.AllEntries...)
	sort.Strings(entries)
	want := []string{"extra/", "extra/file.tex", "output.pdf", "resource/", "resource/path.tex", "sneaky-file"}
	if len(entries) != len(want) {
		t.Fatalf("want %d entries, got %d: %#v", len(want), len(entries), res.AllEntries)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("sorted allEntries[%d] = %q, want %q\n(all: %#v)", i, entries[i], want[i], res.AllEntries)
		}
	}
}

func TestFindOutputFilesMissingDir(t *testing.T) {
	if _, err := FindOutputFiles(nil, "/tmp/__definitely_missing_dir__"); err == nil {
		t.Error("missing directory must return an error")
	}
}
