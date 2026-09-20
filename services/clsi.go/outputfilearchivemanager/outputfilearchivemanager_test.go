package outputfilearchivemanager

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// fakeFind returns a fixed file list or error.
func fakeFind(files []OutputFile, err error) FindFunc {
	return func(args []string, dir string) ([]OutputFile, error) { return files, err }
}

func readZip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	out := make(map[string][]byte)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			rc.Close()
			t.Fatalf("read %s: %v", f.Name, err)
		}
		rc.Close()
		out[f.Name] = buf.Bytes()
	}
	return out
}

func TestGetContentDirNoUser(t *testing.T) {
	got := GetContentDir("/output/dir", "project-1", "")
	if want := "/output/dir/project-1/"; got != want {
		t.Errorf("GetContentDir = %q, want %q", got, want)
	}
}

func TestGetContentDirWithUser(t *testing.T) {
	got := GetContentDir("/output/dir", "project-1", "user-1")
	if want := "/output/dir/project-1-user-1/"; got != want {
		t.Errorf("GetContentDir = %q, want %q", got, want)
	}
}

func TestPathForBuild(t *testing.T) {
	if got := PathForBuild("build-id", "file_1"); got != "build-id/file_1" {
		t.Errorf("PathForBuild = %q, want build-id/file_1", got)
	}
	// Node passes "." (dot) through: build + "/." (not cleaned).
	if got := PathForBuild("build-id", "."); got != "build-id/." {
		t.Errorf("PathForBuild(build, \".\") = %q, want build-id/.", got)
	}
}

func TestFilterArchiveable(t *testing.T) {
	in := []OutputFile{
		{Path: "output.pdf"},
		{Path: "output.tar.gz"},
		{Path: "history-resync.json.gz"},
		{Path: "output.fls"},
		{Path: "output.fdb_latexmk"},
		{Path: "fig.png"},
		{Path: "a/b/c.pdf"},
	}
	got := filterArchiveable(in)
	if len(got) != 2 {
		t.Fatalf("filterArchiveable: %d files, want 2 (fig.png, a/b/c.pdf): %+v", len(got), got)
	}
	byName := map[string]bool{}
	for _, f := range got {
		byName[f.Path] = true
	}
	if !byName["fig.png"] || !byName["a/b/c.pdf"] {
		t.Errorf("filter lost expected files: %+v", byName)
	}
	if byName["output.pdf"] || byName["output.tar.gz"] || byName["history-resync.json.gz"] ||
		byName["output.fls"] || byName["output.fdb_latexmk"] {
		t.Errorf("filter kept an excluded file: %+v", byName)
	}
}

func TestFilterArchiveableEmpty(t *testing.T) {
	if got := filterArchiveable(nil); len(got) != 0 {
		t.Errorf("filterArchiveable(nil) len = %d, want 0", len(got))
	}
}

// setupTestEnv builds a contentDir with real small files and sets Find to
// return them (the production path: real files on disk).
func setupTestEnv(t *testing.T, files map[string]string) (outputDir string) {
	t.Helper()
	outputDir = t.TempDir()
	subDir := filepath.Join(outputDir, "project-1-user-1")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		p := filepath.Join(subDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return outputDir
}

// realFind returns (files, nil) for a known directory listing.
func realFind(files []OutputFile) FindFunc {
	return func(args []string, _ string) ([]OutputFile, error) { return files, nil }
}

func TestArchiveFilesForBuildAllFilesPresent(t *testing.T) {
	outputDir := setupTestEnv(t, map[string]string{
		"build-id/file_1": "data-1",
		"build-id/file_2": "data-2",
	})
	AssignFind(realFind([]OutputFile{
		{Path: "build-id/file_1"},
		{Path: "build-id/file_2"},
	}))
	defer AssignFind(nil)

	var out bytes.Buffer
	if err := ArchiveFilesForBuild(outputDir, "project-1", "user-1", "build-id", &out); err != nil {
		t.Fatalf("ArchiveFilesForBuild: %v", err)
	}
	z := readZip(t, out.Bytes())
	if len(z) != 2 {
		t.Fatalf("zip entries = %d, want 2: %v", len(z), keysOf(z))
	}
	if string(z["build-id/file_1"]) != "data-1" || string(z["build-id/file_2"]) != "data-2" {
		t.Errorf("zip contents wrong: %v", z)
	}
	if _, hasMissing := z["missing_files.txt"]; hasMissing {
		t.Error("unexpected missing_files.txt when no file is missing")
	}
}

func TestArchiveFilesForBuildSomeFilesMissing(t *testing.T) {
	// file_1 exists, file_2 does NOT -> missing_files.txt entry.
	outputDir := setupTestEnv(t, map[string]string{
		"build-id/file_1": "data-1",
	})
	AssignFind(realFind([]OutputFile{
		{Path: "build-id/file_1"},
		{Path: "build-id/file_2"},
	}))
	defer AssignFind(nil)

	var out bytes.Buffer
	if err := ArchiveFilesForBuild(outputDir, "project-1", "user-1", "build-id", &out); err != nil {
		t.Fatalf("ArchiveFilesForBuild: %v", err)
	}
	z := readZip(t, out.Bytes())
	if !stringEq(z["missing_files.txt"], "build-id/file_2") {
		t.Errorf("missing_files.txt = %q, want \"build-id/file_2\"", z["missing_files.txt"])
	}
	if _, ok := z["build-id/file_2"]; ok {
		t.Error("file_2 should NOT be in the archive (open failed)")
	}
	if string(z["build-id/file_1"]) != "data-1" {
		t.Errorf("file_1 content wrong: %q", z["build-id/file_1"])
	}
}

func stringEq(a []byte, want string) bool {
	return string(a) == want
}

func TestArchiveFilesForBuildFindMissing(t *testing.T) {
	assignFindNotFound(t, os.ErrNotExist)
	var out bytes.Buffer
	err := ArchiveFilesForBuild("any", "p", "u", "b", &out)
	if err.(NotFoundError) != NotFoundError("Output files not found") {
		t.Fatalf("wrong NotFoundError: %v (want %q)", err, "Output files not found")
	}
}

func assignFindNotFound(t *testing.T, cause error) {
	AssignFind(func(args []string, _ string) ([]OutputFile, error) { return nil, cause })
}

func TestArchiveFilesForBuildFindNotInitialised(t *testing.T) {
	AssignFind(nil)
	defer AssignFind(nil)
	var out bytes.Buffer
	err := ArchiveFilesForBuild("any", "p", "u", "b", &out)
	if err == nil {
		t.Fatal("wanted error when Find is nil")
	}
	if got, want := err.Error(), "outputfilearchivemanager: Find not initialised"; got != want {
		t.Errorf("Find-not-initialised error = %q, want %q", got, want)
	}
}

func keysOf(m map[string][]byte) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestOpenErrorToNotFoundNil(t *testing.T) {
	if got := OpenErrorToNotFound(nil); got != nil {
		t.Fatalf("OpenErrorToNotFound(nil) = %v, want nil", got)
	}
}

func TestOpenErrorToNotFoundENOENT(t *testing.T) {
	if got := OpenErrorToNotFound(os.ErrNotExist); got != NotFoundError("Output files not found") {
		t.Fatalf("ENOENT = %v, want NotFoundError", got)
	}
}

func TestOpenErrorToNotFoundEACCES(t *testing.T) {
	if got := OpenErrorToNotFound(os.ErrPermission); got != NotFoundError("Output files not found") {
		t.Fatalf("EACCES = %v, want NotFoundError", got)
	}
}

// ENOTDIR: Go surfaces it as *fs.PathError wrapping the raw ENOTDIR errno.
// We trigger the message-based match by crafting a *fs.PathError whose
// inner error is the ENOTDIR errno error (os.PathError whose .Err text is
// "not a directory" on linux).
func TestOpenErrorToNotFoundENOTDIR(t *testing.T) {
	// Build a *fs.PathError whose inner error matches "not a directory".
	inner := errors.New("not a directory (os error 20)")
	pe := &fs.PathError{Op: "open", Path: "/dir", Err: inner}
	if got := OpenErrorToNotFound(pe); got != NotFoundError("Output files not found") {
		t.Fatalf("ENOTDIR = %v, want NotFoundError", got)
	}
}

func TestOpenErrorToNotFoundOtherPropagates(t *testing.T) {
	generic := errors.New("some other error")
	if got := OpenErrorToNotFound(generic); got != generic {
		t.Fatalf("generic error not propagated: %v", got)
	}
}

// End-to-end through archiveFilesForBuild with a Find that returns ENOENT.
func TestArchiveFilesForBuildEACCES(t *testing.T) {
	assignFindNotFound(t, os.ErrPermission)
	var out bytes.Buffer
	if err := ArchiveFilesForBuild("any", "p", "u", "b", &out); err != NotFoundError("Output files not found") {
		t.Fatalf("EACCES = %v, want NotFoundError", err)
	}
}

func TestArchiveFilesForBuildOtherErrorPropagates(t *testing.T) {
	cause := errors.New("boom")
	AssignFind(func(args []string, _ string) ([]OutputFile, error) { return nil, cause })
	defer AssignFind(nil)
	var out bytes.Buffer
	if err := ArchiveFilesForBuild("any", "p", "u", "b", &out); err != cause {
		t.Fatalf("unexpected error: %v (want %v)", err, cause)
	}
}
