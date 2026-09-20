package conversionmanager

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clsi/commandrunner"
	"clsi/config"
	oerrors "clsi/errors"
	"clsi/lockmanager"
)

// fakeRunner is an in-process commandrunner.Runner double.
type fakeRunner struct {
	runs      int
	lastCmd   []string
	lastDir   string
	lastImage string
	lastCwd   string
	lastEnv   map[string]string
	output    *commandrunner.RunOutput
	injected  error
}

func (f *fakeRunner) Run(_ string, command []string, _ string, image string,
	_ int64, environment map[string]string, _ string, cwd string,
	callback func(err error, out *commandrunner.RunOutput)) string {
	f.runs++
	f.lastCmd = command
	f.lastImage = image
	f.lastCwd = cwd
	f.lastEnv = environment
	if f.injected != nil {
		callback(f.injected, nil)
		return "c"
	}
	out := f.output
	if out == nil {
		out = &commandrunner.RunOutput{ExitCode: 0}
	}
	callback(nil, out)
	return "c"
}

func (f *fakeRunner) Kill(_ string, _ func(error))      {}
func (f *fakeRunner) CanRunSyncTeXInOutputDir() bool    { return true }

// scriptedRunner fails a specific 1-based call index.
// - cause != nil  => transport error (callback(err, nil))
// - cause == nil  => non-zero exit  (callback(nil, {ExitCode:1, Stderr:"boom"}))
type scriptedRunner struct {
	failAt int
	cause  error
	n      int
}

func (s *scriptedRunner) Run(_ string, _ []string, _ string, _ string,
	_ int64, _ map[string]string, _ string, _ string,
	callback func(err error, out *commandrunner.RunOutput)) string {
	s.n++
	if s.failAt > 0 && s.n == s.failAt {
		if s.cause != nil {
			callback(s.cause, nil)
			return "c"
		}
		callback(nil, &commandrunner.RunOutput{ExitCode: 1, Stderr: "boom"})
		return "c"
	}
	callback(nil, &commandrunner.RunOutput{ExitCode: 0})
	return "c"
}

func (s *scriptedRunner) Kill(_ string, _ func(error))     {}
func (s *scriptedRunner) CanRunSyncTeXInOutputDir() bool   { return true }

type testManager struct {
	m    *Manager
	fake *fakeRunner
	dir  string
}

func newTestManager(t *testing.T, out *commandrunner.RunOutput) *testManager {
	t.Helper()
	dir := t.TempDir()
	fake := &fakeRunner{output: out}
	m := &Manager{
		Runner:          fake,
		CompilesDir:     filepath.Join(dir, "compiles"),
		TimeoutMs:       1000,
		PandocImage:     "pandoc:3.9",
		PdftocairoImage: "pdftocairo:24.02",
		logDebug:        func(string, map[string]any) {},
	}
	m.acquire = func(key string) (*lockmanager.Lock, error) {
		// minimal, config-free lock (mirrors lockmanager.Acquire's shape)
		return &lockmanager.Lock{Key: key, ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	m.uuid = func() string { return "12345678-1234-4123-8123-1234567890ab" }
	return &testManager{m: m, fake: fake, dir: dir}
}

func writeInput(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	return p
}

func TestNewConfigWiring(t *testing.T) {
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	cfg := config.ForTest()
	if cfg == nil {
		t.Fatal("ForTest nil")
	}
	fm := &fakeRunner{}
	m := New(fm, cfg, func(string, map[string]any) {})
	if m == nil || m.Runner != fm {
		t.Fatal("New wiring wrong")
	}
	if m.CompilesDir == "" || m.TimeoutMs == 0 {
		t.Fatalf("New fields not set: %+v", m)
	}
	if m.uuid() == "" {
		t.Fatal("uuid empty")
	}
	// nil logDebug defaults (covers the fallback in New)
	New(fm, cfg, nil)
	// production acquire seam is wired
	if m.acquire == nil {
		t.Fatal("acquire seam expected non-nil")
	}
	fm.output = &commandrunner.RunOutput{ExitCode: 9}
	out, err := m.runPromise("pid", []string{"p"}, "dir", "img", 5, map[string]string{"K": "v"}, "grp", "cwd")
	if err != nil || out.ExitCode != 9 {
		t.Fatalf("runPromise: %v %+v", err, out)
	}
	// transport-error path
	fm.injected = stderrors.New("transport down")
	_, err = m.runPromise("pid", []string{"p"}, "dir", "img", 5, nil, "grp", "cwd")
	if err == nil || err.Error() != "transport down" {
		t.Fatalf("runPromise err path: %v", err)
	}
	fm.injected = nil
}

func TestUuidV4Format(t *testing.T) {
	s := uuidV4()
	if len(s) != 36 {
		t.Fatalf("uuid length: %q", s)
	}
	if s[14] != '4' {
		t.Fatalf("uuid version digit: %q", s)
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		t.Fatalf("uuid dashes: %q", s)
	}
}

func TestUnsupportedConversion(t *testing.T) {
	tm := newTestManager(t, nil)
	if _, err := tm.m.convertToLaTeX("c1", "d", "in.docx", "pdf"); err == nil {
		t.Fatal("expected unsupported error")
	}
	if _, err := tm.m.convertLaTeXToDocumentInDir("c", "d", "main.tex", "pdf"); err == nil {
		t.Fatal("expected unsupported error (2)")
	}
}

func TestConvertToLaTeXSuccess(t *testing.T) {
	tm := newTestManager(t, nil)
	src := writeInput(t, "in.docx", "hi")
	out, err := tm.m.ConvertToLaTeXWithLock("c1", src, "docx")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := filepath.Join(tm.m.CompilesDir, "c1", "12345678-1234-4123-8123-1234567890ab.zip")
	if out != want {
		t.Fatalf("path: %q want %q", out, want)
	}
	// Node unlinks the copied input after a successful conversion.
	if _, err := os.Stat(filepath.Join(tm.m.CompilesDir, "c1", "input.docx")); err == nil {
		t.Fatal("input.docx should be removed after success")
	}
	if tm.fake.lastImage != "pandoc:3.9" {
		t.Fatalf("pandoc image: %q", tm.fake.lastImage)
	}
	if tm.fake.runs != 2 {
		t.Fatalf("expected 2 runner calls (pandoc+zip) got %d", tm.fake.runs)
	}
}

func TestConvertToLaTeXPandocExit(t *testing.T) {
	tm := newTestManager(t, &commandrunner.RunOutput{ExitCode: 1, Stderr: "boom"})
	src := writeInput(t, "in.docx", "hi")
	_, err := tm.m.ConvertToLaTeXWithLock("c1", src, "docx")
	ce, ok := err.(*oerrors.ConversionError)
	if !ok {
		t.Fatalf("expected ConversionError: %T %v", err, err)
	}
	if ce.ExitCode != 1 || !ce.UserFacing {
		t.Fatalf("exit code / user-facing mismatch: %+v", ce)
	}
	if _, err := os.Stat(filepath.Join(tm.m.CompilesDir, "c1")); err == nil {
		t.Fatal("conversion dir not removed on error")
	}
}

func TestConvertToLaTeXZipTransportError(t *testing.T) {
	// 1st runner call (pandoc) is a transport error => opErr wrapped in
	// OError("pandoc conversion failed").
	tm := newTestManager(t, nil)
	tm.m.Runner = &scriptedRunner{failAt: 1, cause: stderrors.New("docker down")}
	src := writeInput(t, "in.docx", "hi")
	_, err := tm.m.ConvertToLaTeXWithLock("c1", src, "docx")
	if err == nil {
		t.Fatal("expected error")
	}
	oe, ok := err.(*oerrors.OError)
	if !ok || oe.Message != "pandoc conversion failed" {
		t.Fatalf("expected wrapped OError, got %T %v", err, err)
	}
	if oe.Cause == nil || oe.Cause.Error() != "docker down" {
		t.Fatalf("cause: %v", oe.Cause)
	}
}

func TestConvertLaTeXNoCompress(t *testing.T) {
	tm := newTestManager(t, nil)
	out, err := tm.m.convertLaTeXToDocumentInDir("c", tm.dir, "main.tex", "docx")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.HasSuffix(out, "12345678-1234-4123-8123-1234567890ab.docx") {
		t.Fatalf("out: %q", out)
	}
	got := strings.Join(tm.fake.lastCmd, " ")
	if !strings.Contains(got, "--number-sections") || !strings.Contains(got, "--resource-path=") {
		t.Fatalf("cmd: %s", got)
	}
	if tm.fake.lastCwd != "" {
		t.Fatalf("no-compress cwd should be empty: %q", tm.fake.lastCwd)
	}
}

func TestConvertLaTeXCompressSuccess(t *testing.T) {
	tm := newTestManager(t, nil)
	out, err := tm.m.convertLaTeXToDocumentInDir("c", tm.dir, "main.tex", "markdown")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.HasSuffix(out, "12345678-1234-4123-8123-1234567890ab.zip") {
		t.Fatalf("out: %q", out)
	}
	got := strings.Join(tm.fake.lastCmd, " ")
	if !strings.Contains(got, "../12345678-1234-4123-8123-1234567890ab.zip") {
		t.Fatalf("zip cmd: %s", got)
	}
	if tm.fake.lastCwd != "12345678-1234-4123-8123-1234567890ab" {
		t.Fatalf("cwd mismatch: %q", tm.fake.lastCwd)
	}
}

func TestConvertLaTeXCompressPandocError(t *testing.T) {
	tm := newTestManager(t, &commandrunner.RunOutput{ExitCode: 4, Stderr: "html-err"})
	compileDir := tm.dir
	_, err := tm.m.convertLaTeXToDocumentInDir("c", compileDir, "main.tex", "html")
	if err == nil || !strings.Contains(err.Error(), "pandoc latex-to-document conversion failed") {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestConvertLaTeXCompressZipError(t *testing.T) {
	// pandoc ok, zip non-zero EXIT => "zip compression of export failed".
	// (compress path returns errors raw, no top catch.)
	tm := newTestManager(t, nil)
	tm.m.Runner = &scriptedRunner{failAt: 2, cause: nil}
	compileDir := tm.dir
	_, err := tm.m.convertLaTeXToDocumentInDir("c", compileDir, "main.tex", "markdown")
	if err == nil {
		t.Fatal("expected zip error")
	}
	oe, ok := err.(*oerrors.OError)
	if !ok || oe.Message != "zip compression of export failed" {
		t.Fatalf("wrong error: %T %v", err, err)
	}
}

func TestConvertPDFToJPEGSuccess(t *testing.T) {
	tm := newTestManager(t, nil)
	convId := "pdf1"
	cdir := filepath.Join(tm.m.CompilesDir, convId)
	os.MkdirAll(cdir, 0o755)
	os.WriteFile(filepath.Join(cdir, "output.jpg"), []byte("j"), 0o644) // fake leaves it for Lstat
	src := writeInput(t, "in.pdf", "pdfdata")
	_, err := tm.m.ConvertPDFToJPEGWithLock(convId, src, "preview")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cdir, "input.pdf")); err == nil {
		t.Fatal("input.pdf should be removed after success")
	}
	if tm.fake.lastImage != "pdftocairo:24.02" {
		t.Fatalf("pdftocairo image: %q", tm.fake.lastImage)
	}
	got := strings.Join(tm.fake.lastCmd, " ")
	for _, frag := range []string{"pdftocairo", "-jpeg", "quality=90", "-scale-to-x"} {
		if !strings.Contains(got, frag) {
			t.Fatalf("missing %q: %s", frag, got)
		}
	}
}

func TestConvertPDFToJPEGNonRegular(t *testing.T) {
	tm := newTestManager(t, nil)
	convId := "pdf2"
	cdir := filepath.Join(tm.m.CompilesDir, convId)
	os.MkdirAll(cdir, 0o755)
	os.MkdirAll(cdir+"/output.jpg", 0o755) // a directory, not a regular file
	src := writeInput(t, "in.pdf", "pdfdata")
	_, err := tm.m.ConvertPDFToJPEGWithLock(convId, src, "thumbnail")
	if err == nil {
		t.Fatal("expected non-regular error")
	}
	if _, err := os.Stat(cdir); err == nil {
		t.Fatal("dir not removed on error")
	}
}

func TestConvertPDFToJPEGUnknownMode(t *testing.T) {
	tm := newTestManager(t, nil)
	if _, err := tm.m.convertPDFToJPEG("c", "d", "in.pdf", "bogus"); err == nil {
		t.Fatal("expected error")
	}
}

func TestConvertPDFToJPEGLstatError(t *testing.T) {
	// Lstat error => wrapped OError("pdf-to-jpeg conversion failed").
	tm := newTestManager(t, nil)
	convId := "pdf3"
	cdir := filepath.Join(tm.m.CompilesDir, convId)
	os.MkdirAll(cdir, 0o755)
	src := writeInput(t, "in.pdf", "pdf")
	_, err := tm.m.convertPDFToJPEG(convId, cdir, src, "preview")
	oe, ok := err.(*oerrors.OError)
	if !ok {
		t.Fatalf("expected *OError, got %T", err)
	}
	if oe.Cause == nil {
		t.Fatal("expected lstat cause")
	}
	if !strings.Contains(oe.Message, "pdf-to-jpeg conversion failed") {
		t.Fatalf("outer msg: %q", oe.Message)
	}
}

func TestCopyFileMissing(t *testing.T) {
	// raw ReadFile error propagates from copyFile (no wrapping).
	if err := copyFile("/nonexistent/xyz", t.TempDir()); err == nil {
		t.Fatal("expected ReadFile error")
	}
}

func TestConvertLaTeXWithLockSuccess(t *testing.T) {
	tm := newTestManager(t, nil)
	out, err := tm.m.ConvertLaTeXToDocumentInDirWithLock("c", tm.dir, "main.tex", "docx")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.Contains(out, "12345678-1234-4123-8123-1234567890ab.docx") || !strings.HasSuffix(out, ".docx") {
		t.Fatalf("out: %q", out)
	}
}

func TestWithLockAcquireError(t *testing.T) {
	tm := newTestManager(t, nil)
	tm.m.acquire = func(string) (*lockmanager.Lock, error) {
		return nil, stderrors.New("acquire failed")
	}
	if _, err := tm.m.ConvertToLaTeXWithLock("c", "in.docx", "docx"); err == nil {
		t.Fatal("expected acquire error")
	}
	if _, err := tm.m.ConvertLaTeXToDocumentInDirWithLock("c", "d", "main.tex", "md"); err == nil {
		t.Fatal("expected acquire error (latex)")
	}
	if _, err := tm.m.ConvertPDFToJPEGWithLock("c", "in.pdf", "preview"); err == nil {
		t.Fatal("expected acquire error (pdf)")
	}
}
