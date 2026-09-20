package tikzmanager

import (
	"os"
	"path/filepath"
	"testing"

	"clsi/resourcewriter"
	"clsi/safereader"
)

func TestCheckMainFileResourcesArg(t *testing.T) {
	// resources already contains output.tex -> needsMainFile=false without
	// reading the file (Node logs and returns callback(null,false)).
	needs, err := CheckMainFile("/whatever", "main.tex", []resourcewriter.Resource{
		{Path: "output.tex"},
	})
	_ = safereader.ReadFile // silence unused warning if we drop the import
	if err != nil {
		t.Fatalf("CheckMainFile with output.tex in resources: %v", err)
	}
	if needs {
		t.Fatalf("needsMainFile = true, want false when output.tex already in resources")
	}
}

func TestCheckMainFileDetectsTikz(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "main.tex"),
		[]byte(`\tikzexternalize
\documentclass{article}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	needs, err := CheckMainFile(base, "main.tex", nil)
	if err != nil {
		t.Fatalf("CheckMainFile: %v", err)
	}
	if !needs {
		t.Fatalf("needsMainFile = false, want true (tikzexternalize present)")
	}
}

func TestCheckMainFileReadsTruncatedSafe(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "main.tex"), []byte("no marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	needs, err := CheckMainFile(base, "main.tex", nil)
	if err != nil {
		t.Fatalf("CheckMainFile: %v", err)
	}
	if needs {
		t.Fatalf("needsMainFile = true for plain main.tex")
	}
}

func TestCheckMainFileNoMainFile(t *testing.T) {
	// main.tex missing -> CheckPath returns nil path? No: CheckPath errors
	// when the path escapes base; a *missing* but in-base path still returns
	// a path (safereader then swallows ENOENT -> empty -> false).
	base := t.TempDir()
	needs, err := CheckMainFile(base, "main.tex", nil)
	if err != nil {
		t.Fatalf("CheckMainFile missing main.tex: %v", err)
	}
	if needs {
		t.Fatalf("needsMainFile = true for missing main.tex")
	}
}

func TestUsesTikzExternalize(t *testing.T) {
	for c, want := range map[string]bool{
		"\\tikzexternalize":         true,
		"\\usepackage{pstool}":      true,
		"\\usepackage{amsmath}":     false,
		"no externalize, no pstool": false,
		"":                          false,
	} {
		if got := UsesTikzExternalize(c); got != want {
			t.Errorf("UsesTikzExternalize(%q) = %v, want %v", c, got, want)
		}
	}
}

func TestWriteOutputFileIfNeeded(t *testing.T) {
	base := t.TempDir()
	// no output.tex marker, no tikz content -> no write, no error.
	if err := WriteOutputFileIfNeeded(base, false, "\\documentclass{article}"); err != nil {
		t.Fatalf("plain content (no tikz): %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, OutputTex)); err == nil {
		t.Fatalf("output.tex written for non-tikz content")
	}
	// hasOutputTex marker -> no write even for tikz content.
	if err := WriteOutputFileIfNeeded(base, true, "\\tikzexternalize"); err != nil {
		t.Fatalf("hasOutputTex=true: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, OutputTex)); err == nil {
		t.Fatalf("output.tex written even though snapshot already has output.tex")
	}
	// no marker + tikz content -> writes the content.
	if err := WriteOutputFileIfNeeded(base, false, "\\tikzexternalize"); err != nil {
		t.Fatalf("tikz content: %v", err)
	}
	data, rerr := os.ReadFile(filepath.Join(base, OutputTex))
	if rerr != nil || string(data) != "\\tikzexternalize" {
		t.Fatalf("output.tex = %q err=%v", data, rerr)
	}
	_ = resourcewriter.Resource{} // keep the import (checkpath type-alias)
}

func TestInjectOutputFileExclusiveCreate(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "main.tex"), []byte("main-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Output.tex already present -> O_EXCL error (Node fs.writeFile flag:'wx').
	if err := os.WriteFile(filepath.Join(base, "output.tex"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InjectOutputFile(base, "main.tex"); err == nil {
		t.Fatalf("InjectOutputFile: want O_EXCL error, got nil (output.tex pre-exists)")
	}
	if _, err := os.ReadFile(filepath.Join(base, "output.tex")); err != nil {
		t.Fatalf("existing output.tex corrupted, want unchanged")
	}
	// After removing output.tex it copies main.tex.
	if err := os.Remove(filepath.Join(base, "output.tex")); err != nil {
		t.Fatal(err)
	}
	if err := InjectOutputFile(base, "main.tex"); err != nil {
		t.Fatalf("inject after remove: %v", err)
	}
	data, rerr := os.ReadFile(filepath.Join(base, "output.tex"))
	if rerr != nil || string(data) != "main-content" {
		t.Fatalf("output.tex = %q err=%v, want 'main-content'", data, rerr)
	}
}

// --- error branches ----------------------------------------------------------

func TestCheckMainFileEscapeError(t *testing.T) {
	base := t.TempDir()
	// mainFile escaping basePath -> CheckPath error.
	needs, err := CheckMainFile(base, "../../outside.tex", nil)
	_ = needs
	if err == nil {
		t.Fatal("expected CheckPath escape error")
	}
	if needs {
		t.Fatalf("needs = true on error")
	}
}

func TestCheckMainFileReadError(t *testing.T) {
	base := t.TempDir()
	// mainFile that exists as a directory -> ReadFile fails.
	sub := filepath.Join(base, "x")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := CheckMainFile(base, "x", nil) // mainFile is a FILE-named dir? No: CheckPath returns path. ReadFile of a dir fails
	if err == nil {
		t.Fatal("CheckMainFile reading a directory: expected error")
	}
}
