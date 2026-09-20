// Port of the filestore/data-layer contracts exercised by GitProjectRepoTest
// and the FileUtil/Tar tests. There is no dedicated Java FileStore test class;
// the assertions mirror the production behaviour those tests rely on.
package filestore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryFileAccessors(t *testing.T) {
	f := NewRepositoryFile("src/notes.txt", []byte("hello"))
	if got := f.GetPath(); got != "src/notes.txt" {
		t.Fatalf("path = %q, want src/notes.txt", got)
	}
	if got := string(f.GetContents()); got != "hello" {
		t.Fatalf("contents = %q", got)
	}
}

func TestSize(t *testing.T) {
	if got := NewRepositoryFile("a.bin", []byte{1, 2, 3, 4}).Size(); got != int64(4) {
		t.Fatalf("Size = %d, want 4", got)
	}
}

func TestEqual(t *testing.T) {
	a := &repoFile{path: "a.txt", contents: []byte("x")}
	b := &repoFile{path: "a.txt", contents: []byte("x")}
	if !a.Equal(b) {
		t.Fatalf("identical repoFiles must be Equal")
	}
	c := &repoFile{path: "a.txt", contents: []byte("y")}
	if a.Equal(c) {
		t.Fatalf("different-contents must not be Equal")
	}
	d := &repoFile{path: "b.txt", contents: []byte("x")}
	if a.Equal(d) {
		t.Fatalf("different-path must not be Equal")
	}
	if a.Equal("not a rf") {
		t.Fatalf("mismatched type must not be Equal")
	}
}

func TestWriteFileToDisk(t *testing.T) {
	dir := t.TempDir()
	f := NewRepositoryFile("nested/dir/f.txt", []byte("bytes"))
	if err := WriteFileToDisk(dir, f); err != nil {
		t.Fatalf("WriteFileToDisk: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "nested", "dir", "f.txt"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != "bytes" {
		t.Fatalf("written contents = %q, want bytes", data)
	}
}

func TestWriteFileToDiskUnwritable(t *testing.T) {
	// A directory path component that cannot become a file => error.
	base := t.TempDir()
	blocker := filepath.Join(base, "f.txt")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	f := NewRepositoryFile("f.txt/sub.txt", []byte("no"))
	if err := WriteFileToDisk(base, f); err == nil {
		t.Fatalf("WriteFileToDisk must fail under a file, not a dir")
	}
}

func TestRawDirectory(t *testing.T) {
	rd := NewRawDirectory(map[string]RawFile{})
	if len(rd.FileTable) != 0 {
		t.Fatalf("filetable len = %d, want 0", len(rd.FileTable))
	}
	// nil map falls back to an empty table
	empty := NewRawDirectory(nil)
	if len(empty.FileTable) != 0 {
		t.Fatalf("nil map filetable len = %d, want 0", len(empty.FileTable))
	}
}

func TestNewGitDirectoryContentsAndWrite(t *testing.T) {
	root := t.TempDir()
	srcName := "src/notes.txt"
	mainTex := NewRepositoryFile("main.tex", []byte("contents"))
	subA := NewRepositoryFile("sub"+"a.txt", []byte("a"))
	_ = srcName
	g := NewGitDirectoryContents(
		[]RawFile{mainTex, subA},
		root, "proj1", "User", "u@e.com", "msg", 1234,
	)
	if got := g.GetDirectory(); got != filepath.Join(root, "proj1") {
		t.Fatalf("git dir = %q, want the joined project path", got)
	}
	if err := g.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	for _, rel := range []string{"main.tex", "sub" + "a.txt"} {
		if _, err := os.Stat(filepath.Join(g.GetDirectory(), rel)); err != nil {
			t.Fatalf("expected written file %q: %v", rel, err)
		}
	}
}

func TestWriteClearsExistingFilesKeepsDotGit(t *testing.T) {
	root := t.TempDir()
	g := NewGitDirectoryContents(
		[]RawFile{NewRepositoryFile("keep.txt", []byte("keep"))},
		root, "proj", "u", "u@e.com", "m", 0,
	)
	dir := g.GetDirectory()
	if err := os.MkdirAll(filepath.Join(dir, "stale", "deep"), 0o755); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stale", "deep", "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.txt"), []byte("top"), 0o644); err != nil {
		t.Fatalf("seed top: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("seed .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0o644); err != nil {
		t.Fatalf("seed HEAD: %v", err)
	}

	if err := g.Write(); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale")); !os.IsNotExist(err) {
		t.Errorf("stale dir must be removed before rewrite")
	}
	if _, err := os.Stat(filepath.Join(dir, "top.txt")); !os.IsNotExist(err) {
		t.Errorf("top.txt must be removed before rewrite")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "HEAD")); err != nil {
		t.Errorf(".git/HEAD must be preserved, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Errorf("keep.txt not written: %v", err)
	}
}

func TestErrorTypes(t *testing.T) {
	fileTooLarge := NewRepositoryFileTooLarge(&TooLargeFile{Path: "big.pdf", Limit: 1048576})
	if got := fileTooLarge.Error(); got != "File big.pdf exceeds the 1048576 byte limit" {
		t.Fatalf("RepositoryFileTooLarge.Error = %q", got)
	}
	filesTooLarge := NewFilesTooLarge([]*TooLargeFile{
		{Path: "big.pdf", Limit: 1048576},
		{Path: "huge.bin", Limit: 2097152},
	})
	if got, want := filesTooLarge.Error(), "File big.pdf exceeds the 1048576 byte limit, File huge.bin exceeds the 2097152 byte limit"; got != want {
		t.Fatalf("FilesTooLarge.Error = %q, want %q", got, want)
	}
	sizeLimit := NewSizeLimitExceededException(2000)
	if got := sizeLimit.Error(); got == "" {
		t.Fatalf("SizeLimitExceededException message empty")
	}
}

func TestWriteFailsUnreadableDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory permission checks; test requires non-root")
	}
	root := t.TempDir()
	g := NewGitDirectoryContents(
		[]RawFile{NewRepositoryFile("f.txt", []byte("x"))},
		root, "proj", "u", "u@e.com", "m", 0,
	)
	dir := g.GetDirectory()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(dir, 0o755) }()
	if err := g.Write(); err == nil {
		t.Fatalf("Write must fail on an unreadable project dir (non-root)")
	}
}

func TestDeleteInDirectoryApartFromMissingDir(t *testing.T) {
	// Fresh root that does not contain the project dir => os.ReadDir yields
	// IsNotExist => no-op success.
	root := t.TempDir()
	g := NewGitDirectoryContents(nil, root, "ghost", "u", "u@e.com", "m", 0)
	if err := g.Write(); err != nil {
		t.Fatalf("Write into a nonexistent dir should be a no-op success, got %v", err)
	}
}

func TestDeleteRecursiveErrors(t *testing.T) {
	// nonexistent dir => ReadDir IsNotExist => nil (no-op).
	if err := deleteRecursive(filepath.Join(t.TempDir(), "ghost")); err != nil {
		t.Fatalf("deleteRecursive on missing dir must be nil, got %v", err)
	}
	// unreadable dir => ReadDir error surfaced (non-root).
	dir := t.TempDir()
	good := filepath.Join(dir, "sub")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A file that is also a dir name is impossible; use chmod to force error.
	// Make a path that is a regular file, not a dir:
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed f: %v", err)
	}
	if err := deleteRecursive(file); err == nil {
		t.Fatalf("deleteRecursive on a file must error (ReadDir on non-dir)")
	}
}
