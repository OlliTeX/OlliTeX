// Whitebox coverage tests for package repo, on top of the mirrored
// GitProjectRepoTest / FSGitRepoStoreTest cases in project_test.go.
//
// Targets the remaining branches: explicit-commit accessors
// (NewProjectAtCommit / UseJGitRepoAt), walkTree (unborn / committed / bad
// override / size limit), GC + purge error branches, remove* helpers, and
// the tar compression seams (size limits, unknown streams, forged headers).
package repo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ollitex/go/services/gitbridge/filestore"
	"ollitex/go/services/gitbridge/giterrors"
)

func isRoot() bool { return os.Geteuid() == 0 }

// initProjectRepo creates <root>/<proj> as a fresh repo with one commit
// (a.txt and sub/b.txt). Returns (store, project, headSha).
func initProjectRepo(t *testing.T, root, proj string) (*FSGitRepoStore, *Project, string) {
	t.Helper()
	store := NewFSGitRepoStore(root, nil)
	if _, err := store.InitRepo(proj); err != nil {
		t.Fatalf("init: %v", err)
	}
	p := store.NewProject(proj)
	env := map[string]string{
		"GIT_AUTHOR_NAME":     "W",
		"GIT_AUTHOR_EMAIL":    "w@w",
		"GIT_COMMITTER_NAME":  "W",
		"GIT_COMMITTER_EMAIL": "w@w",
		"GIT_AUTHOR_DATE":     "@1700000000 +0000",
		"GIT_COMMITTER_DATE":  "@1700000000 +0000",
	}
	if err := os.MkdirAll(filepath.Join(root, proj, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, proj, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, proj, "sub", "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	if _, err := p.GitRaw(nil, "add", "--", "."); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := p.GitRaw(env, "commit", "--quiet", "-m", "first"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	sha, err := p.GitRaw(nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	sha = strings.TrimSpace(sha)
	return store, p, sha
}

// commitNew commits files {path: body} at "W <w@w>" 2023-11-14 and returns
// the HEAD sha.
func commitNew(t *testing.T, p *Project, files map[string]string) string {
	t.Helper()
	env := map[string]string{
		"GIT_AUTHOR_NAME":     "W",
		"GIT_AUTHOR_EMAIL":    "w@w",
		"GIT_COMMITTER_NAME":  "W",
		"GIT_COMMITTER_EMAIL": "w@w",
		"GIT_AUTHOR_DATE":     "@1700000000 +0000",
		"GIT_COMMITTER_DATE":  "@1700000000 +0000",
	}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Join(p.workDir, filepath.Dir(path)), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(p.workDir, path), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if _, err := p.GitRaw(nil, "add", "--", "."); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := p.GitRaw(env, "commit", "--quiet", "-m", "v2"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	sha, err := p.GitRaw(nil, "rev-parse", "HEAD")
	_ = err
	return strings.TrimSpace(sha)
}

// ---------------------------------------------------------------------------
// Project accessors + walkTree (GetDirectory) paths.
// ---------------------------------------------------------------------------

func TestProjectAccessorsAndHeadDirectory(t *testing.T) {
	root := t.TempDir()
	store, p, _ := initProjectRepo(t, root, "demo")
	if p.GetProjectName() != "demo" {
		t.Fatalf("GetProjectName = %q, want demo", p.GetProjectName())
	}
	if store.GetRepoStorePath() != root || store.GetRootDirectory() != root {
		t.Fatalf("store path accessors wrong")
	}
	dir, err := p.GetDirectory()
	if err != nil {
		t.Fatalf("GetDirectory: %v", err)
	}
	if len(dir) != 2 {
		t.Fatalf("GetDirectory = %d files, want 2", len(dir))
	}
	if got := dir["a.txt"].Size(); got != int64(len("alpha")) {
		t.Fatalf("a.txt size %d want %d", got, len("alpha"))
	}
}

func TestGetDirectoryUnbornIsEmpty(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	if _, err := store.InitRepo("empty"); err != nil {
		t.Fatalf("init: %v", err)
	}
	dir, err := store.NewProject("empty").GetDirectory()
	if err != nil {
		t.Fatalf("GetDirectory (unborn): %v", err)
	}
	if len(dir) != 0 {
		t.Fatalf("unborn GetDirectory not empty: %v", dir)
	}
}

func TestGetDirectoryBadCommitOverrideIsInvalidRepo(t *testing.T) {
	root := t.TempDir()
	store, _, _ := initProjectRepo(t, root, "demo")
	at := store.NewProjectAtCommit("demo", "not-a-sha")
	if at.GetProjectName() != "demo" {
		t.Fatalf("at-commit project name wrong: %q", at.GetProjectName())
	}
	_, err := at.GetDirectory()
	if err == nil {
		t.Fatalf("GetDirectory with bogus commit override must error")
	}
	if _, ok := err.(*giterrors.InvalidGitRepository); !ok {
		t.Fatalf("expected *InvalidGitRepository, got %v", err)
	}
}

func TestUseJGitRepoAtMissingObjectsIsInvalid(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	if _, err := store.UseJGitRepoAt("absent", "abc"); err == nil {
		t.Fatalf("UseJGitRepoAt on a missing project must error")
	}
}

func TestUseJGitRepoAtWalksExplicitCommit(t *testing.T) {
	root := t.TempDir()
	store, _, sha := initProjectRepo(t, root, "demo")
	at, err := store.UseJGitRepoAt("demo", sha)
	if err != nil {
		t.Fatalf("UseJGitRepoAt: %v", err)
	}
	dir, err := at.GetDirectory()
	if err != nil {
		t.Fatalf("at-commit GetDirectory: %v", err)
	}
	if got := dir["sub/b.txt"].Size(); got != int64(len("beta")) {
		t.Fatalf("sub/b.txt size %d want %d", got, len("beta"))
	}
	// NewProjectAtCommit (top-level, store-agnostic) also wires the commit.
	np := NewProjectAtCommit(store, "demo", "not-a-sha")
	if np.commitID != "not-a-sha" {
		t.Fatalf("NewProjectAtCommit commitID = %q", np.commitID)
	}
}

func TestGetDirectorySizeLimitExceeded(t *testing.T) {
	root := t.TempDir()
	limit := int64(16)
	store := NewFSGitRepoStoreWithSizer(root, limit, nil)
	p, err := store.InitRepo("big")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	commitNew(t, p, map[string]string{"huge.txt": string(bytes.Repeat([]byte("x"), 32))})
	_, err = p.GetDirectory()
	if err == nil {
		t.Fatalf("GetDirectory must exceed the 16-byte size limit")
	}
	if errAsAny := err; errAsAny == nil {
		t.Fatalf("expected err")
	}
	if _, ok := err.(*giterrors.SizeLimitExceededException); !ok {
		t.Fatalf("expected *SizeLimitExceededException, got %v", err)
	}
}

func TestGetFullBranchUninitialisedIsError(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	if _, err := store.NewProject("norun").GetFullBranch(); err == nil {
		t.Fatalf("GetFullBranch on an uninitialised project must error")
	}
}

func TestUseExistingRepositoryMissingObjects(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	if err := store.NewProject("ninit").UseExistingRepository(); err == nil {
		t.Fatalf("UseExistingRepository on an uninitialised project must error")
	}
}

func TestInitRepoWorkDirIsFile(t *testing.T) {
	root := t.TempDir()
	fileproj := filepath.Join(root, "fileproj")
	if err := os.WriteFile(fileproj, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	store := NewFSGitRepoStore(root, nil)
	if _, err := store.InitRepo("fileproj"); err == nil {
		t.Fatalf("InitRepo must fail when the project path is a file")
	}
}

func TestCommitNewSecondCommit(t *testing.T) {
	root := t.TempDir()
	_, p, _ := initProjectRepo(t, root, "demo")
	commitNew(t, p, map[string]string{"third.txt": "gamma"})
	// walk: a.txt, sub/b.txt, third.txt
	dir, err := p.GetDirectory()
	if err != nil {
		t.Fatalf("GetDirectory: %v", err)
	}
	if len(dir) != 3 {
		t.Fatalf("expected 3 files, got %d", len(dir))
	}
}

func TestLsTreePathsBadCommit(t *testing.T) {
	root := t.TempDir()
	_, p, _ := initProjectRepo(t, root, "demo")
	if _, err := lsTreePaths(p.gitDir(), "bogus-sha"); err == nil {
		t.Fatalf("lsTreePaths must fail for a bogus commit")
	}
}

func TestRemoveInDirectoryErrorsSilently(t *testing.T) {
	if isRoot() {
		t.Skip("cannot simulate permission errors as root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	removeInDirectoryApartFrom(dir, ".git") // must return silently
}

func TestRemoveRecursiveDeletesContents(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "x", "y"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "f"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "x", "f2"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "x", "y", "f3"), []byte("3"), 0o644); err != nil {
		t.Fatal(err)
	}
	removeRecursive(base)
	if _, err := os.Stat(filepath.Join(base, "f")); err == nil {
		t.Fatalf("f must be deleted by removeRecursive")
	}
	// error path: ReadDir on a missing dir returns silently.
	removeRecursive(filepath.Join(t.TempDir(), "ghost"))
}

func TestDeleteIncomingUnreadableDir(t *testing.T) {
	if isRoot() {
		t.Skip("cannot simulate permission errors as root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	if err := deleteIncoming(dir); err == nil {
		t.Fatalf("deleteIncoming must fail on an unreadable directory")
	}
}

// ---------------------------------------------------------------------------
// store-level error + happy branches.
// ---------------------------------------------------------------------------

func TestStoreSwapInvalidNames(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	// Leading-dot / empty names are INVALID per util.IsValidProjectName.
	if _, err := store.Bzip2Project(".hidden"); err == nil {
		t.Fatalf("Bzip2Project must reject leading-dot names")
	}
	if _, err := store.GzipProject(""); err == nil {
		t.Fatalf("GzipProject must reject empty name")
	}
	if err := store.Unbzip2Project("..", nil); err == nil {
		t.Fatalf("Unbzip2Project must reject invalid names")
	}
	if err := store.UngzipProject("..", nil); err == nil {
		t.Fatalf("UngzipProject must reject invalid names")
	}
}

func TestStoreUntargetCompressMissingGIt(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	// a valid project name but no <proj>/.git directory: compressDir stat
	// fails -> sizeLimitError (NOT the invalid-name error).
	if _, err := store.Bzip2Project("nogit"); err == nil {
		t.Fatalf("Bzip2Project must fail when the .git directory is missing")
	}
	if _, err := store.GzipProject("nogit"); err == nil {
		t.Fatalf("GzipProject must fail when the .git directory is missing")
	}
	// Unbzip2Project when the project path is a regular FILE: MkdirAll
	// cannot create a directory over it -> wrapped error (covers the
	// `target directory already exists` branch).
	if err := os.WriteFile(filepath.Join(root, "asfile"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := store.Unbzip2Project("asfile", []byte("junk")); err == nil {
		t.Fatalf("Unbzip2Project must fail when the project dir exists as a file")
	}
}

func TestGcProjectAndRemove(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	// invalid names (leading dot / empty) hit the IsValidProjectName error.
	if err := store.GcProject(".x"); err == nil {
		t.Fatalf("GcProject must reject invalid names")
	}
	if err := store.Remove(""); err == nil {
		t.Fatalf("Remove must reject empty name")
	}
	// Remove on a project that exists as a regular FILE: DeleteDirectory
	// errors on a file (not a directory) -> returns the error.
	if err := os.WriteFile(filepath.Join(root, "asfile"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := store.Remove("asfile"); err == nil {
		t.Fatalf("Remove must fail when the project path is a file")
	}

	// happy: GcProject on a real (init'd) repo.
	if _, err := store.InitRepo("real"); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := store.GcProject("real"); err != nil {
		t.Fatalf("GcProject on a real repo: %v", err)
	}

	// store.Remove on a real project dir (dir exists, DeleteDirectory happy).
	if _, err := store.InitRepo("toremove"); err != nil {
		t.Fatalf("init2: %v", err)
	}
	if err := store.Remove("toremove"); err != nil {
		t.Fatalf("Remove on existing project: %v", err)
	}
}

func TestNewFSGitRepoStoreExplicitMax(t *testing.T) {
	root := t.TempDir()
	max := int64(424242)
	s := NewFSGitRepoStore(root, &max)
	if s.MaxFileSize() != max {
		t.Fatalf("explicit MaxFileSize = %d, want %d", s.MaxFileSize(), max)
	}
	// nil -> default.
	s2 := NewFSGitRepoStore(t.TempDir(), nil)
	if s2.MaxFileSize() != DefaultMaxFileSize {
		t.Fatalf("default MaxFileSize = %d, want %d", s2.MaxFileSize(), DefaultMaxFileSize)
	}
}

func TestPurgeRootMissingAndUnreadable(t *testing.T) {
	if isRoot() {
		t.Skip("chmod tests only meaningful as non-root")
	}
	// missing root: ReadDir ENOENT -> nil.
	ghost := filepath.Join(t.TempDir(), "gone", "sub")
	store := NewFSGitRepoStore(ghost, nil)
	if err := store.PurgeNonexistentProjects(nil); err != nil {
		t.Fatalf("PurgeNonexistentProjects on a missing root must return nil, got %v", err)
	}

	// unreadable root: ReadDir error (non-NotExist) -> return err.
	unreadable := t.TempDir()
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(unreadable, 0o755)
	store2 := NewFSGitRepoStore(unreadable, nil)
	if err := store2.PurgeNonexistentProjects(nil); err == nil {
		t.Fatalf("PurgeNonexistentProjects must fail on an unreadable root")
	}

	// a regular FILE in the store root: DeleteDirectory on a file errors
	// -> Purge returns that error.
	root3 := t.TempDir()
	fileInRoot := filepath.Join(root3, "afile")
	if err := os.WriteFile(fileInRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store3 := NewFSGitRepoStore(root3, nil)
	if err := store3.PurgeNonexistentProjects(nil); err == nil {
		t.Fatalf("PurgeNonexistentProjects must fail when it hits a regular file")
	}
}

func TestDeleteIncomingMissingDirIsNil(t *testing.T) {
	// deleteIncoming on a missing directory: ReadDir ENOENT -> nil.
	if err := deleteIncoming(filepath.Join(t.TempDir(), "no-dir")); err != nil {
		t.Fatalf("deleteIncoming on a missing dir must return nil, got %v", err)
	}
}

func TestStoreNewProjectAtCommit(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	// store.NewProjectAtCommit wires the commit override (exported surface).
	at := store.NewProjectAtCommit("whatever", "sha-123")
	if at.GetProjectName() != "whatever" || at.commitID != "sha-123" {
		t.Fatalf("NewProjectAtCommit wrong: %q %q", at.GetProjectName(), at.commitID)
	}
}

func TestDiskUsedSizerMissingPath(t *testing.T) {
	if _, err := diskUsedSizer(filepath.Join(t.TempDir(), "ghost")); err == nil {
		t.Fatalf("diskUsedSizer must fail for a missing path")
	}
}

// ---------------------------------------------------------------------------
// tar.go seams.
// ---------------------------------------------------------------------------

func TestCompressDirUnknownKind(t *testing.T) {
	if _, err := compressDir(t.TempDir(), ".", "zip"); err == nil {
		t.Fatalf("compressDir must reject unknown stream kinds")
	}
}

func TestCompressDirMissingDirIsSizeLimit(t *testing.T) {
	_, err := compressDir(filepath.Join(t.TempDir(), "ghost"), t.TempDir(), "gzip")
	if err == nil {
		t.Fatalf("compressDir must fail for a missing directory")
	}
	if _, ok := err.(*sizeLimitError); !ok {
		t.Fatalf("expected *sizeLimitError, got %v", err)
	}
}

func TestAddTarFileTooLarge(t *testing.T) {
	base := t.TempDir()
	// Create the SPARSE file: open with O_TRUNC and seek past 2 GiB so we
	// don't actually allocate that much disk (a 2 GiB hole-backed file).
	full := filepath.Join(base, "huge")
	f, err := os.Create(full)
	if err != nil {
		t.Fatalf("create sparse: %v", err)
	}
	if _, err := f.Seek((1<<31)+1024, io.SeekStart); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if _, err := f.Write([]byte{0}); err != nil {
		t.Fatalf("write sparse byte: %v", err)
	}
	f.Close()
	if _, ok := statSize(full); !ok {
		t.Fatalf("stat sparse")
	}
	// compressDir on a FILE (not dir) stat's it, hits addTarFile's size
	// check (2 GiB).
	if _, err := compressDir(full, base, "gzip"); err == nil {
		t.Fatalf("compressDir must fail when the file exceeds 2 GiB")
	}
}

func statSize(path string) (int64, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return st.Size(), true
}

func TestRelToBaseFallback(t *testing.T) {
	// relToBase uses filepath.Rel (base, abs) then ToSlash. /b is not under
	// /a (no error, just a dot-dot path).
	if got := relToBase("/b", "/a"); got != "../b" {
		t.Fatalf("relToBase(/b,/a) = %q, want ../b", got)
	}
	// A prefix that IS a parent yields the plain relative name.
	base := t.TempDir()
	full := filepath.Join(base, "sub", "f.txt")
	if got := relToBase(full, base); got != filepath.ToSlash(filepath.Join("sub", "f.txt")) {
		t.Fatalf("relToBase = %q", got)
	}
}

func TestSizeLimitErrorString(t *testing.T) {
	e := &sizeLimitError{path: "p", size: 99}
	if s := e.Error(); s != "file too big (99 B): p" {
		t.Fatalf("Error() = %q", s)
	}
}

func TestUntarStreamBadGzip(t *testing.T) {
	if err := untarStream([]byte("not-a-gzip"), t.TempDir(), "gzip"); err == nil {
		t.Fatalf("untarStream must fail for non-gzip data")
	}
}

func TestUntarStreamBadBzip2(t *testing.T) {
	if err := untarStream([]byte("not-a-bzip2"), t.TempDir(), "bzip2"); err == nil {
		t.Fatalf("untarStream must fail for non-bzip2 data")
	}
}

func TestUntarStreamRejectsOversizedEntry(t *testing.T) {
	// raw tar with a file entry claiming a 4 GiB size: reader sees
	// hdr.Size > 1<<31-1 and must reject before opening the dest file.
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	if err := tw.WriteHeader(&tar.Header{Name: "big", Mode: 0o644, Size: 1 << 32}); err != nil {
		t.Fatalf("write forged header: %v", err)
	}
	// Do NOT Close: the writer would reject the unwritten 4 GiB body.
	// We just need the header block (512 bytes) which tar.NewReader accepts.
	if err := untarStream(raw.Bytes(), t.TempDir(), "raw"); err == nil {
		t.Fatalf("untarStream must reject entries larger than 2 GiB")
	}
}

func TestUntarStreamStripsLeadingSlash(t *testing.T) {
	// raw tar with "/leading" file + a "." skip entry; verify the slash is
	// stripped and "." is skipped.
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	content := []byte("hello")
	if err := tw.WriteHeader(&tar.Header{Name: "/leading", Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatalf("hdr: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	target := t.TempDir()
	if err := untarStream(raw.Bytes(), target, "raw"); err != nil {
		t.Fatalf("untarStream: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "leading")); err != nil || string(got) != "hello" {
		t.Fatalf("leading not extracted: %v %q", err, got)
	}
}

func TestUntarStreamDirectoryEntry(t *testing.T) {
	// directory entries: the TypeDir branch MkdirAlls the sub-path.
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	if err := tw.WriteHeader(&tar.Header{Name: "subdir/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatalf("dir hdr: %v", err)
	}
	content := []byte("body")
	if err := tw.WriteHeader(&tar.Header{Name: "subdir/f", Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatalf("file hdr: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	target := t.TempDir()
	if err := untarStream(raw.Bytes(), target, "raw"); err != nil {
		t.Fatalf("untarStream: %v", err)
	}
	if st, err := os.Stat(filepath.Join(target, "subdir")); err != nil || !st.IsDir() {
		t.Fatalf("subdir not created: %v", err)
	}
}

// gzipRoundTrip helper (avoids re-implementing gzip; exercised via the same
// writer type the gzip branch of untarStream decodes).
func gzipCompress(t *testing.T, data []byte) []byte {
	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, 9)
	if err != nil {
		t.Fatalf("gzip writer: %v", err)
	}
	if _, err := gw.Write(data); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func TestUntarStreamGzipDirectoryEntry(t *testing.T) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	if err := tw.WriteHeader(&tar.Header{Name: "subdir/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatalf("dir hdr: %v", err)
	}
	content := []byte("body")
	if err := tw.WriteHeader(&tar.Header{Name: "subdir/f", Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatalf("file hdr: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	gz := gzipCompress(t, raw.Bytes())
	target := t.TempDir()
	if err := untarStream(gz, target, "gzip"); err != nil {
		t.Fatalf("untarStream gzip: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "subdir", "f")); err != nil || string(got) != "body" {
		t.Fatalf("subdir/f not extracted: %v %q", err, got)
	}
}

// ---------------------------------------------------------------------------
// computeMissing via CommitAndGetMissing (missing paths are returned).
// ---------------------------------------------------------------------------

func TestCommitAndGetMissingReportsDeletedFiles(t *testing.T) {
	root := t.TempDir()
	_, p, _ := initProjectRepo(t, root, "demo")
	// commit contents that omit "sub/b.txt": it must be reported as missing.
	files := []filestore.RawFile{filestore.NewRepositoryFile("a.txt", []byte("alpha2"))}
	contentsObj := filestore.NewGitDirectoryContents(files, root, "demo",
		"Winston Li", "w@w", "update", 1700000000000)
	if missing, err := p.CommitAndGetMissing(contentsObj); err != nil ||
		len(missing) != 1 || missing[0] != "sub/b.txt" {
		if err != nil {
			t.Fatalf("CommitAndGetMissing: %v (missing=%v)", err, missing)
		}
		t.Fatalf("missing = %v, want [sub/b.txt]", missing)
	}
}

// CommitAndGetMissing on a committed repo where the work tree is dirty
// (resetHard + git rm + add + commit path).
func TestCommitAndMissingOnRepoHeadReset(t *testing.T) {
	root := t.TempDir()
	_, p, _ := initProjectRepo(t, root, "demo")
	// dirty work tree: add an untracked file; CommitAndGetMissing must
	// still succeed (reset-hard first).
	if err := os.MkdirAll(filepath.Join(p.ProjectDir(), "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.ProjectDir(), "untracked.txt"), []byte("u"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}
	files := []filestore.RawFile{
		filestore.NewRepositoryFile("a.txt", []byte("alpha")),
		filestore.NewRepositoryFile("untracked.txt", []byte("u2")),
	}
	contentsObj := filestore.NewGitDirectoryContents(files, root, "demo",
		"Winston Li", "w@w", "commit", 1700000000000)
	if missing, err := p.CommitAndGetMissing(contentsObj); err != nil {
		t.Fatalf("commit on dirty tree: %v", err)
	} else if len(missing) != 1 || missing[0] != "sub/b.txt" {
		t.Fatalf("missing = %v, want [sub/b.txt]", missing)
	}
}

// RunGC on a repo with a commit (the real `git gc` happy path).
func TestRunGCHappyPath(t *testing.T) {
	root := t.TempDir()
	_, p, _ := initProjectRepo(t, root, "demo")
	if err := p.RunGC(); err != nil {
		t.Fatalf("RunGC: %v", err)
	}
}

func TestStoreGetExistingRepoMissingObjects(t *testing.T) {
	store := NewFSGitRepoStore(t.TempDir(), nil)
	if _, err := store.GetExistingRepo("absent"); err == nil {
		t.Fatalf("GetExistingRepo must error for a missing project")
	}
}

func TestStoreGcProjectMissingObjectsIsError(t *testing.T) {
	store := NewFSGitRepoStore(t.TempDir(), nil)
	if err := store.GcProject("absent"); err == nil {
		t.Fatalf("GcProject must error for a missing project")
	}
}

func TestPurgeRootAfterRemoveIsNil(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	os.RemoveAll(root) // ReadDir now returns IsNotExist -> nil
	if err := store.PurgeNonexistentProjects(nil); err != nil {
		t.Fatalf("PurgeNonexistentProjects after IsNotExist must be nil, got %v", err)
	}
}

func TestRunGitWithStdin(t *testing.T) {
	// non-empty stdin exercises the `cmd.Stdin = strings.NewReader(stdin)`
	// branch (git version ignores stdin).
	if _, err := runGit(t.TempDir(), nil, "dummy-stdin", "version"); err != nil {
		t.Fatalf("runGit: %v", err)
	}
	// `git hash-object --stdin` consumes stdin -> covers the pipe for real.
	data, err := runGit(t.TempDir(), nil, "hello", "hash-object", "--stdin")
	if err != nil {
		t.Fatalf("hash-object: %v", err)
	}
	// b6fc4c6 = sha1("hello") without trailing newline (stdin has no \n).
	if !strings.HasPrefix(data, "b6fc4c620b67") {
		t.Fatalf("hash-object --stdin mismatch: %q", data)
	}
}

func TestWalkTreeEmptyCommitCoversEmptyLine(t *testing.T) {
	// commit --allow-empty: ls-tree emits nothing; the `line == ""` branch
	// in walkTree still runs.
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	if _, err := store.InitRepo("e"); err != nil {
		t.Fatalf("init: %v", err)
	}
	p := store.NewProject("e")
	env := map[string]string{
		"GIT_AUTHOR_NAME":     "W",
		"GIT_AUTHOR_EMAIL":    "w@w",
		"GIT_COMMITTER_NAME":  "W",
		"GIT_COMMITTER_EMAIL": "w@w",
		"GIT_AUTHOR_DATE":     "@1700000000 +0000",
		"GIT_COMMITTER_DATE":  "@1700000000 +0000",
	}
	if _, err := p.GitRaw(env, "commit", "--allow-empty", "--quiet", "-m", "empty"); err != nil {
		t.Fatalf("empty commit: %v", err)
	}
	if dir, err := p.GetDirectory(); err != nil || len(dir) != 0 {
		t.Fatalf("GetDirectory (empty commit) = %d files (err %v), want 0 nil", len(dir), err)
	}
}

func TestRemoveRecursiveUnreadableDir(t *testing.T) {
	if isRoot() {
		t.Skip("chmod only meaningful as non-root")
	}
	base := t.TempDir()
	if err := os.Chmod(base, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(base, 0o755)
	// ReadDir err -> early return (no panic).
	removeRecursive(base)
}

func TestUngzipBadCompression(t *testing.T) {
	// UngzipProject happy-name + corrupt stream: the gzip reader errors.
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	if err := store.UngzipProject("junk", []byte("notgzip")); err == nil {
		t.Fatalf("UngzipProject must fail for non-gzip data")
	}
}

func TestStoreTotalSizeCustomErrorSizer(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStoreWithSizer(root, int64(42), func(string) (int64, error) {
		return 0, errors.New("no size")
	})
	if _, err := store.TotalSize(); err == nil {
		t.Fatalf("TotalSize with error sizer must propagate error")
	}
}

func TestIsProjectPresentFalseOnAbsent(t *testing.T) {
	store := NewFSGitRepoStore(t.TempDir(), nil)
	if store.IsProjectPresent("noper") {
		t.Fatalf("IsProjectPresent must be false for an absent project")
	}
}

// TestCommitAndGetMissingSubdirExercisesRemoveRecursive covers:
//   - the `if e.IsDir() { removeRecursive(full); continue }` branch in
//     removeInDirectoryApartFrom (workdir has a subdirectory at cleanup time)
//   - the `if line == "" { continue }` branch in lsTreePaths (when HEAD has
//     an empty tree: `ls-tree -r --name-only` emits nothing, so Split +
//     TrimSpace yields a single empty line)
func TestCommitAndGetMissingSubdirExercisesRemoveRecursive(t *testing.T) {
	root := t.TempDir()
	store, p, _ := initProjectRepo(t, root, "demo")
	_ = store
	// Build an empty commit on a second project, so lsTreePaths sees
	// `out=""` (no entries).
	root2 := t.TempDir()
	store2 := NewFSGitRepoStore(root2, nil)
	if _, err := store2.InitRepo("emptyhead"); err != nil {
		t.Fatalf("init emptyhead: %v", err)
	}
	p2 := store2.NewProject("emptyhead")
	env := map[string]string{
		"GIT_AUTHOR_NAME":     "W",
		"GIT_AUTHOR_EMAIL":    "w@w",
		"GIT_COMMITTER_NAME":  "W",
		"GIT_COMMITTER_EMAIL": "w@w",
		"GIT_AUTHOR_DATE":     "@1700000000 +0000",
		"GIT_COMMITTER_DATE":  "@1700000000 +0000",
	}
	if _, err := p2.GitRaw(env, "commit", "--allow-empty", "--quiet", "-m", "empty"); err != nil {
		t.Fatalf("empty commit: %v", err)
	}
	// Commit on p2 via CommitAndGetMissing: lsTreePaths hits empty output
	// -> `line == ""` branch covers 271-272.
	emptyFiles := []filestore.RawFile{}
	ce := filestore.NewGitDirectoryContents(emptyFiles, root2, "emptyhead",
		"W", "w@w", "m", 1700000000000)
	if _, err := p2.CommitAndGetMissing(ce); err != nil {
		t.Fatalf("CommitAndGetMissing on empty tree: %v", err)
	}

	// Now write a subdirectory into the workdir of `demo` project, so
	// removeInDirectoryApartFrom hits the IsDir branch (321-323).
	// We re-create the .git state by committing first (so the work-tree is
	// ephemeral + cleared) then using a fresh contents that writes a subdir.
	sub := filepath.Join(p.ProjectDir(), "sub2")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub2: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "x.txt"), []byte("1"), 0o644); err != nil {
		t.Fatalf("write x.txt: %v", err)
	}
	subFiles := []filestore.RawFile{
		filestore.NewRepositoryFile("sub2/x.txt", []byte("1")),
		filestore.NewRepositoryFile("a.txt", []byte("alpha")),
	}
	c := filestore.NewGitDirectoryContents(subFiles, root, "demo",
		"W", "w@w", "v", 1700000000000)
	if missing, err := p.CommitAndGetMissing(c); err != nil {
		t.Fatalf("CommitAndGetMissing with subdir: %v (missing=%v)", err, missing)
	}
	// After a successful commit + clear, `sub2` must be gone from the
	// workdir (removeRecursive cleaned it).
	if _, err := os.Stat(filepath.Join(p.ProjectDir(), "sub2", "x.txt")); err == nil {
		t.Fatalf("sub2/x.txt must be removed after CommitAndGetMissing's cleanup")
	}
}

// TestDeleteIncomingUnreadableSubdir covers:
//   - `deleteIncoming` recurses into subdirs (347, 358-360) — if a subdir
//     is unreadable, the recursive call returns a non-IsNotExist error which
//     propagates to the `if err := deleteIncoming(full); err != nil { return err }`
//     branch 358-360.
//     Also `os.Remove` on an unreadable incoming_*.pack file propagates the
//     non-IsNotExist error (364-366).
func TestDeleteIncomingUnreadableSubdir(t *testing.T) {
	if isRoot() {
		t.Skip("chmod only meaningful as non-root")
	}
	// Create .git with an incoming_*.pack file and a subdir that is chmod 0.
	root := t.TempDir()
	_, p, _ := initProjectRepo(t, root, "demo")
	gd := filepath.Join(p.ProjectDir(), ".git")
	sub := filepath.Join(gd, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// An incoming pack file that lives in the subdir; chmod the subdir 0
	// makes ReadDir fail on it.
	if err := os.WriteFile(filepath.Join(sub, "incoming_deadbeef.pack"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(sub, 0o755)
	// Now `deleteIncoming` on `.git` recurses into `subdir`, ReadDir
	// fails with EACCES (non-IsNotExist) -> error propagates.
	if err := p.DeleteIncomingPacks(); err == nil {
		t.Fatalf("DeleteIncomingPacks must propagate permission errors from subdirs")
	}
}

// TestRunGCPermissionFailure covers RunGC's error branch (`git gc` failing).
// Chmod the workdir's .git to be unreadable so `git gc` fails.
func TestRunGCPermissionFailure(t *testing.T) {
	if isRoot() {
		t.Skip("chmod only meaningful as non-root")
	}
	root := t.TempDir()
	_, p, _ := initProjectRepo(t, root, "demo")
	// Make `git gc` fail by making the .git dir unreadable.
	if err := os.Chmod(filepath.Join(p.ProjectDir(), ".git"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(p.ProjectDir(), ".git"), 0o755)
	if err := p.RunGC(); err == nil {
		t.Fatalf("RunGC must fail when .git is unreadable")
	}
}

// TestComputeMissingUnbornCoversReturnNil: a project with an unborn HEAD
// (only `git init`, no commits) hits the `if committed == "" { return nil }`
// branch in computeMissing (via CommitAndGetMissing on a freshly inited project).
func TestComputeMissingUnbornCoversReturnNil(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	p, err := store.InitRepo("unborn")
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	// contents with one file; unborn => computeMissing returns nil, no
	// `git rm` is executed.
	contentsObj := filestore.NewGitDirectoryContents(
		[]filestore.RawFile{filestore.NewRepositoryFile("one.txt", []byte("1"))},
		root, "unborn", "W", "w@w", "first", 1700000000000)
	if missing, err := p.CommitAndGetMissing(contentsObj); err != nil {
		t.Fatalf("CommitAndGetMissing (unborn): %v", err)
	} else if len(missing) != 0 {
		t.Fatalf("unborn CommitAndGetMissing must report no missing files, got %v", missing)
	}
}

func TestLsTreePathsEmptyTreeCoversEmptyLine(t *testing.T) {
	root := t.TempDir()
	_, p, _ := initProjectRepo(t, root, "demo")
	// store a *locally present* empty tree in this repo, then ls-tree on it:
	// output is empty -> the `line == ""` continue branch runs.
	emptyTree, err := runGit(p.ProjectDir(), nil, "", "mktree")
	if err != nil {
		t.Fatalf("empty mktree: %v", err)
	}
	emptyTree = strings.TrimSpace(emptyTree)
	paths, err := lsTreePaths(p.gitDir(), emptyTree)
	if err != nil {
		t.Fatalf("lsTreePaths(emptyTree): %v", err)
	}
	if len(paths) != 0 {
		t.Fatalf("lsTreePaths(emptyTree) = %v, want empty", paths)
	}
}

func TestWalkTreeMissingObjectIsInvalidRepository(t *testing.T) {
	root := t.TempDir()
	_, p, _ := initProjectRepo(t, root, "demo")
	gd := filepath.Join(p.ProjectDir(), ".git")
	// Build a tree with a dangling blob OID (000...1): `git mktree
	// --missing` accepts and stores the tree object locally without
	// validating the child. ls-tree lists the entry; walkTree then
	// reaches `cat-file -e <oid>` which fails for the missing object
	// -> InvalidGitRepository.
	tree, err := runGit(gd, nil,
		"000000 blob 0000000000000000000000000000000000000001\tmissing.txt\n",
		"mktree", "--missing")
	if err != nil {
		t.Skipf("mktree --missing unavailable: %v", err)
	}
	tree = strings.TrimSpace(tree)
	// Walk the dangling tree via the at-commit Project override: this
	// makes walkTree run against the fake tree and exercise the
	// `cat-file -e` failing branch (project.go:241-243).
	if _, err := NewProjectAtCommit(NewFSGitRepoStore(root, nil), "demo", tree).GetDirectory(); err == nil {
		t.Fatalf("GetDirectory over a dangling-object tree must error")
	}
	_ = gd
}

func TestUngzipProjectUnreadableTarget(t *testing.T) {
	if isRoot() {
		t.Skip("chmod only meaningful as non-root")
	}
	root := t.TempDir()
	// UngzipProject: target path exists as a FILE -> MkdirAll fails ->
	// propagates (covers UngzipProject's MkdirAll branch too).
	if err := os.WriteFile(filepath.Join(root, "asfile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewFSGitRepoStore(root, nil)
	if err := store.UngzipProject("asfile", gzipCompress(t, make([]byte, 4))); err == nil {
		t.Fatalf("UngzipProject must fail when the target is a file")
	}
}

func TestUntarStreamSkipsDotEntry(t *testing.T) {
	// raw tar with a first entry named "." (skipped: `name == "."`) then
	// a real file, to cover the explicit `.` continue branch.
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	if err := tw.WriteHeader(&tar.Header{Name: ".", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatalf("hdr: %v", err)
	}
	content := []byte("real")
	if err := tw.WriteHeader(&tar.Header{Name: "f", Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatalf("file hdr: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	target := t.TempDir()
	if err := untarStream(raw.Bytes(), target, "raw"); err != nil {
		t.Fatalf("untarStream: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "f")); err != nil || string(got) != "real" {
		t.Fatalf("f not extracted: %v %q", err, got)
	}
}
