package outputfileoptimiser

import (
	"os"
	"path"
	"testing"
)

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := path.Join(dir, name)
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func testPDF(t *testing.T, marker string) string {
	t.Helper()
	dir := t.TempDir()
	p := path.Join(dir, "output.pdf")
	writeFile(t, dir, "output.pdf", []byte("%PDF-1.4\n"+marker+"\n%%EOF"))
	return p
}

func markerFile(t *testing.T) string {
	dir := t.TempDir()
	return writeFile(t, dir, "output.pdf", []byte("%PDF not linearised"))
}

func noWarn() func(map[string]any, ...any) {
	return func(map[string]any, ...any) {}
}

func TestCheckIfPDFIsOptimisedMarker(t *testing.T) {
	ok, err := CheckIfPDFIsOptimised(testPDF(t, "/Linearized 1"))
	if err != nil {
		t.Fatalf("CheckIfPDFIsOptimised = %v", err)
	}
	if !ok {
		t.Error("file with '/Linearized 1' marker must be optimised")
	}
}

func TestCheckIfPDFIsOptimisedNoMarker(t *testing.T) {
	ok, err := CheckIfPDFIsOptimised(markerFile(t))
	if err != nil {
		t.Fatalf("CheckIfPDFIsOptimised = %v", err)
	}
	if ok {
		t.Error("file without marker must NOT be optimised")
	}
}

func TestCheckIfPDFIsOptimisedMissing(t *testing.T) {
	_, err := CheckIfPDFIsOptimised("/no/such/file.pdf")
	if err == nil {
		t.Fatal("expected open error for missing file")
	}
}

// makeBin writes a fake qpdf into bin and prepends it to PATH (restored later).
func makeBin(t *testing.T) {
	t.Helper()
	bin, err := os.MkdirTemp("", "clsi-fake-bin-*")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(bin) })
	fake := path.Join(bin, "qpdf")
	// Last arg is dstOpt; create it as an empty file = "qpdf wrote dst.opt".
	script := "#!/usr/bin/sh\nfor a in \"$@\"; do last=\"$a\"; done\n: > \"$last\"\n"
	if err := os.WriteFile(fake, []byte(script), 0755); err != nil {
		t.Fatalf("write fake qpdf: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", oldPath) })
	if err := os.Setenv("PATH", bin+":"+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
}

func TestOptimiseFileNonPDFAbstracts(t *testing.T) {
	if err := OptimiseFile("/tmp/nope.txt", "/tmp/x.pdf", noWarn()); err != nil {
		t.Errorf("no-op path returned error: %v", err)
	}
}

func TestOptimiseFileMissingSrcPropagates(t *testing.T) {
	err := OptimiseFile("/definitely/missing/output.pdf", "/tmp/d.pdf", noWarn())
	if err == nil {
		t.Error("expected the open error to propagate (Node: callback(err))")
	}
}

func TestOptimiseFileAlreadyOptimisedNoQpdf(t *testing.T) {
	p := testPDF(t, "/Linearized 1")
	called := false
	old := RunQPDF
	RunQPDF = func(src, dstOpt string) error { called = true; return nil }
	defer func() { RunQPDF = old }()
	if err := OptimiseFile(p, p+".dst", noWarn()); err != nil {
		t.Fatalf("OptimiseFile = %v", err)
	}
	if called {
		t.Error("qpdf must not run when the file is already linearised")
	}
}

func TestOptimiseFileQpdfErrorSwallowed(t *testing.T) {
	p := markerFile(t)
	old := RunQPDF
	RunQPDF = func(src, dstOpt string) error { return os.ErrNotExist }
	defer func() { RunQPDF = old }()
	dst := p + ".dst"
	if err := OptimiseFile(p, dst, noWarn()); err != nil {
		t.Fatalf("qpdf error must be swallowed (Node: callback(null)): %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("dst %q must not exist after qpdf failure", dst)
	}
}

func TestOptimiseFileQpdfSuccessRenames(t *testing.T) {
	src := markerFile(t)
	dst := path.Clean(src + ".dst")
	old := RunQPDF
	RunQPDF = func(src, dstOpt string) error {
		return os.WriteFile(dstOpt, []byte("optimised-bytes"), 0644)
	}
	defer func() { RunQPDF = old }()
	if err := OptimiseFile(src, dst, noWarn()); err != nil {
		t.Fatalf("OptimiseFile = %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("dst not produced: %v", err)
	}
	if string(data[:]) != "optimised-bytes" {
		t.Errorf("dst content = %q, want the qpdf-written bytes", data)
	}
}

func TestOptimiseFileRenameFailSwallow(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "output.pdf", []byte("x")) // ends in /output.pdf
	dst := path.Join(dir, "out.pdf")
	old := RunQPDF
	RunQPDF = func(src, dstOpt string) error { return nil }
	defer func() { RunQPDF = old }()
	warned := 0
	if err := OptimiseFile(src, dst, func(map[string]any, ...any) { warned++ }); err != nil {
		t.Fatalf("rename failure must be swallowed: %v", err)
	}
	if warned == 0 {
		t.Error("expected a warning for the failed rename")
	}
}

// TestOptimiseFileDefaultRunQPDF covers defaultImpl (real exec) end-to-end
// with a fake qpdf on PATH: exit 0 -> dstOpt created -> os.Rename(dst, ...).
func TestOptimiseFileDefaultRunQPDF(t *testing.T) {
	makeBin(t)
	old := RunQPDF
	RunQPDF = defaultImpl
	defer func() { RunQPDF = old }()

	dir := t.TempDir()
	src := writeFile(t, dir, "output.pdf", []byte("not optimised"))
	dst := path.Join(dir, "out.pdf")
	if err := OptimiseFile(src, dst, noWarn()); err != nil {
		t.Fatalf("OptimiseFile = %v (PATH=%s)", err, os.Getenv("PATH"))
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("dst missing after defaultImpl: %v", err)
	}
}

func TestDefaultImplMissingBinary(t *testing.T) {
	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)
	if err := os.Setenv("PATH", ""); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	if err := defaultImpl("/x", "y"); err == nil {
		t.Error("defaultImpl with empty PATH = nil, want error (qpdf missing)")
	}
}
