package outputfilefinder

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOrderMatchesNodeExactOrder pins allEntries to Node's deep.equal
// contract (exact order, NOT a set).
func TestOrderMatchesNodeExactOrder(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel string) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		_ = os.Remove(full)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		if err := os.WriteFile(full, []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	mk("resource/path.tex")
	mk("output.pdf")
	mk("extra/file.tex")
	// symlink (non-regular) -> allEntries only
	if err := os.Symlink("../foo", filepath.Join(dir, "sneaky-file")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	res, err := FindOutputFiles([]Resource{{Path: "resource/path.tex"}}, dir)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	want := []string{"extra/file.tex", "extra/", "output.pdf", "resource/path.tex", "resource/", "sneaky-file"}
	if len(res.AllEntries) != len(want) {
		t.Fatalf("allEntries = %v, want order %v", res.AllEntries, want)
	}
	for i := range want {
		if res.AllEntries[i] != want[i] {
			t.Errorf("allEntries[%d] = %q, want %q (full: %v)", i, res.AllEntries[i], want[i], res.AllEntries)
		}
	}
	// outputFiles order + membership (Node: deep.members — order-agnostic set
	// in the outputFiles assertion; here both entries).
	out := map[string]string{}
	for _, f := range res.OutputFiles {
		out[f.Path] = f.Type
	}
	if out["output.pdf"] != "pdf" || out["extra/file.tex"] != "tex" || len(out) != 2 {
		t.Errorf("outputFiles = %v, want output.pdf/pdf + extra/file.tex/tex", out)
	}
}
