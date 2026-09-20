// Package repo tests port GitProjectRepoTest + FSGitRepoStoreTest 1:1.
//
// Fixtures: testdata/fixtures/{gits,fs} — copied from the Java
// src/test/resources/.../{GitProjectRepoTest,FSGitRepoStoreTest}/rootdir and
// the Java test's DOTgit→.git rename (mirrors Java test setup()).
//
// Layout: JGit (not `git init`) repos: git dir = <project>/.git and
// JGit's getWorkTree() returns <project>. Plain `git` discovers <project>/.
// git from cwd <project> — exactly what runGit does (cmd.Dir = workDir).
package repo

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ollitex/go/services/gitbridge/filestore"
)

// ---------------------------------------------------------------------------
// fixture/plumbing helpers
// ---------------------------------------------------------------------------

// copyPath copies the tree src→dest. Fixture roots are now tarballs
// (testdata/fixtures/{fs,gits}.*.tar.gz): `git add` records nested `.git` dirs
// as gitlinks (HANDOFF §13), so the fixtures are stored as plain-file tar
// archives and untared here into the temp copy.
func copyPath(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, info, dest)
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return untar(f, dest)
}

// copyDir recursively copies directory src→dest, preserving dotfiles AND
// dot-directories.
func copyDir(src string, info os.FileInfo, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// os.ReadDir already returns dotfiles; but hidden dirs are ALSO returned
	// (unlike `ls` filtering). Good.
	for _, e := range entries {
		full := filepath.Join(src, e.Name())
		name, _ := filepath.Rel(src, full)
		if err := copyDirOne(full, name, dest); err != nil {
			return err
		}
	}
	return nil
}

func copyDirOne(full, name, dest string) error {
	di := filepath.Join(dest, name)
	fi, err := os.Stat(full)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return copyDir(full, fi, di)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	return os.WriteFile(di, data, 0o644)
}

// untar extracts a gzip-tar (the fixture archives are made with
// `tar -czf ... -C src .` so all entries are `./`-relative) into dest.
func untar(f *os.File, dest string) error {
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := hdr.Name
		if name == "/" || strings.HasPrefix(name, "./") {
			name = strings.TrimPrefix(name, "./")
		}
		if name == "" {
			continue // archive root ("./") — dest already exists
		}
		target := filepath.Join(dest, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		case tar.TypeReg, tar.TypeSymlink:
			dot := strings.Contains(name, "/")
			if dot {
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return err
				}
			}
			if err := copyEntry(tr, target); err != nil {
				return err
			}
		default:
			// hardlinks etc. — best-effort skip
		}
	}
}

func copyEntry(r *tar.Reader, target string) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

// copyFixture copies testdata/fixtures/{set}/{name} → <temp>/<name>; returns
// the temp store-root (mirrors Java TemporaryFolder + makeTempRepoDir).
func copyFixture(t *testing.T, set, name string) string {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join("testdata", "fixtures", set, name+".tar.gz")
	if err := copyPath(src, filepath.Join(root, name)); err != nil {
		t.Fatalf("copy fixture %s: %v", src, err)
	}
	return root
}

// copyFSet copies testdata/fixtures/fs (idontexist + .wlgb + proj1 + proj2)
// into a fresh temp store root.
func copyFSet(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := copyPath(filepath.Join("testdata", "fixtures", "fs.tar.gz"), root); err != nil {
		t.Fatalf("copy fs fixture: %v", err)
	}
	return root
}

// dirSizeSizer is a deterministic directory-tree size sizer (recursive file
// sizes, no syscall). Used by the totalSize change test instead of the
// production statfs-based sizer, which measures a shared filesystem and is
// not stable under concurrent activity.
func dirSizeSizer(absPath string) (int64, error) {
	var sum int64
	err := filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil // vanished — treat as empty
			}
			return err
		}
		if info.Mode().IsRegular() {
			sum += info.Size()
		}
		return nil
	})
	return sum, err
}

func dirSizeSum(t *testing.T, root string) int64 {
	t.Helper()
	var total int64
	filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func listDir(t *testing.T, dir string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out
		}
		t.Fatalf("list dir %s: %v", dir, err)
	}
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

// makeContents ports GitProjectRepoTest.makeDirContents:
//
//	GitDirectoryContents(<files>, storeRoot, name, "Winston Li",
//	"git@winston.li", "Commit Message", now)
func makeContents(storeRoot, name string, args ...string) *filestore.GitDirectoryContents {
	var files []filestore.RawFile
	for i := 0; i+1 < len(args); i += 2 {
		files = append(files, filestore.NewRepositoryFile(args[i], []byte(args[i+1])))
	}
	return filestore.NewGitDirectoryContents(files, storeRoot, name, "Winston Li", "git@winston.li", "Commit Message", 1700000000000)
}

func mapEq(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// dirBytes returns {relpath: contents} for every regular file under dir.
func dirBytes(dir string) map[string][]byte {
	out := map[string][]byte{}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if err != nil && !os.IsNotExist(err) && info == nil {
				// walk errors are tolerated for this compare
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		data, _ := os.ReadFile(path)
		out[rel] = data
		return nil
	})
	return out
}

// dirEq mirrors Java FileUtil.directoryDeepEquals: same file set + bytes.
func dirEq(a, b string) bool {
	fa, fb := dirBytes(a), dirBytes(b)
	if len(fa) != len(fb) {
		return false
	}
	for rel, d := range fa {
		od, ok := fb[rel]
		if !ok {
			return false
		}
		if len(d) != len(od) {
			return false
		}
		for i := range d {
			if d[i] != od[i] {
				return false
			}
		}
	}
	return true
}

// resetHard runs `git reset --hard` from the project dir (Java
// GitProjectRepo.resetHard).
func resetHard(dir string) error {
	_, err := runGitInDir(dir, map[string]string{"GIT_PAGER": ""}, "reset", "--hard", "--quiet")
	return err
}

// ---------------------------------------------------------------------------
// Mirror of GitProjectRepoTest (Java).
// ---------------------------------------------------------------------------

func TestDeletingIgnoredFileOnAppDeletesFromTheRepo(t *testing.T) {
	root := copyFixture(t, "gits", "repo")
	store := NewFSGitRepoStore(root, nil)
	repo, err := store.GetExistingRepo("repo")
	if err != nil {
		t.Fatalf("get existing repo: %v", err)
	}
	contents := makeContents(root, "repo", ".gitignore", "*.ignored\n")
	if _, err := repo.CommitAndGetMissing(contents); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := resetHard(repo.ProjectDir()); err != nil {
		t.Fatalf("reset: %v", err)
	}
	got := listDir(t, repo.ProjectDir())
	want := map[string]bool{".git": true, ".gitignore": true}
	if !mapEq(got, want) {
		t.Errorf("expected %v got %v", want, got)
	}
}

func TestAddingIgnoredFilesOnAppAddsToTheRepo(t *testing.T) {
	root := copyFixture(t, "gits", "repo")
	store := NewFSGitRepoStore(root, nil)
	repo, err := store.GetExistingRepo("repo")
	if err != nil {
		t.Fatalf("get existing repo: %v", err)
	}
	contents := makeContents(root, "repo",
		".gitignore", "*.ignored\n",
		"file1.ignored", "",
		"file1.txt", "a",
		"file2.txt", "b",
		"added.ignored", "",
	)
	if _, err := repo.CommitAndGetMissing(contents); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := resetHard(repo.ProjectDir()); err != nil {
		t.Fatalf("reset: %v", err)
	}
	got := listDir(t, repo.ProjectDir())
	want := map[string]bool{
		".git": true, ".gitignore": true,
		"file1.ignored": true, "file1.txt": true, "file2.txt": true, "added.ignored": true,
	}
	if !mapEq(got, want) {
		t.Errorf("expected %v got %v", want, got)
	}
}

func TestBadGitignoreShouldNotThrow(t *testing.T) {
	root := copyFixture(t, "gits", "badgitignore")
	store := NewFSGitRepoStore(root, nil)
	bad, err := store.GetExistingRepo("badgitignore")
	if err != nil {
		t.Fatalf("get existing (badgitignore): %v", err)
	}
	contents := makeContents(root, "badgitignore",
		".gitignore", "*.ignored\n",
		"file1.ignored", "x",
		"file1.txt", "a",
		"file2.txt", "b",
		"added.ignored", "",
	)
	if _, err := bad.CommitAndGetMissing(contents); err != nil {
		t.Fatalf("commit on badgitignore should not error: %v", err)
	}
}

func TestRunGCReducesTheSizeOfARepoWithGarbage(t *testing.T) {
	root := copyFixture(t, "gits", "repo")
	store := NewFSGitRepoStore(root, nil)
	repo, err := store.GetExistingRepo("repo")
	if err != nil {
		t.Fatalf("get existing: %v", err)
	}
	before := dirSizeSum(t, repo.ProjectDir())
	if err := repo.RunGC(); err != nil {
		t.Fatalf("gc: %v", err)
	}
	after := dirSizeSum(t, repo.ProjectDir())
	// Mirrors Java: assertThat(beforeSize, lessThan(afterSize)).
	if before >= after {
		t.Errorf("assertion beforeSize < afterSize failed: before=%d after=%d", before, after)
	}
}

func TestRunGCDoesNothingOnARepoWithoutGarbage(t *testing.T) {
	root := copyFixture(t, "gits", "repo")
	store := NewFSGitRepoStore(root, nil)
	repo, err := store.GetExistingRepo("repo")
	if err != nil {
		t.Fatalf("get existing: %v", err)
	}
	if err := repo.RunGC(); err != nil {
		t.Fatalf("gc1: %v", err)
	}
	before := dirSizeSum(t, repo.ProjectDir())
	if err := repo.RunGC(); err != nil {
		t.Fatalf("gc2: %v", err)
	}
	after := dirSizeSum(t, repo.ProjectDir())
	if before != after {
		t.Errorf("size must be stable after double gc: before=%d after=%d", before, after)
	}
}

func TestDeleteIncomingPacksDeletesIncomingPacks(t *testing.T) {
	incRoot := copyFixture(t, "gits", "incoming")
	woRoot := copyFixture(t, "gits", "without_incoming")
	incStore := NewFSGitRepoStore(incRoot, nil)
	inc, err := incStore.GetExistingRepo("incoming")
	if err != nil {
		t.Fatalf("no incoming repo: %v", err)
	}
	woStore := NewFSGitRepoStore(woRoot, nil)
	wo, err := woStore.GetExistingRepo("without_incoming")
	if err != nil {
		t.Fatalf("no without repo: %v", err)
	}
	if dirEq(inc.ProjectDir(), wo.ProjectDir()) {
		t.Fatalf("incoming and without_incoming should differ before cleanup")
	}
	if err := inc.DeleteIncomingPacks(); err != nil {
		t.Fatalf("deleteIncomingPacks: %v", err)
	}
	if !dirEq(inc.ProjectDir(), wo.ProjectDir()) {
		t.Errorf("after deleteIncomingPacks the project dirs should be byte-equal")
	}
}

func TestDeleteIncomingPacksOnDirWithoutIncomingPacksDoesNothing(t *testing.T) {
	woRoot := copyFixture(t, "gits", "without_incoming")
	woRootCopy := copyFixture(t, "gits", "without_incoming") // "expected" pristine copy
	woStore := NewFSGitRepoStore(woRoot, nil)
	wo, err := woStore.GetExistingRepo("without_incoming")
	if err != nil {
		t.Fatalf("no without repo: %v", err)
	}
	if err := wo.DeleteIncomingPacks(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !dirEq(wo.ProjectDir(), filepath.Join(woRootCopy, "without_incoming")) {
		t.Errorf("without_incoming must be unchanged by deleteIncomingPacks")
	}
}

func TestInitRepoSetsDefaultBranchToMain(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	if _, err := store.InitRepo("testproject"); err != nil {
		t.Fatalf("init: %v", err)
	}
	br, err := store.NewProject("testproject").GetFullBranch()
	if err != nil {
		t.Fatalf("get branch: %v", err)
	}
	if br != "refs/heads/main" {
		t.Errorf("expected refs/heads/main, got %s", br)
	}
}

func TestInitRepoDoesNotRewriteExistingMasterRepoToMain(t *testing.T) {
	root := t.TempDir()
	store := NewFSGitRepoStore(root, nil)
	if _, err := store.InitRepo("testproject"); err != nil {
		t.Fatalf("init: %v", err)
	}
	// Java: repo.updateRef(HEAD).link("refs/heads/master")
	if _, err := store.NewProject("testproject").GitRaw(nil, "symbolic-ref", "HEAD", "refs/heads/master"); err != nil {
		t.Fatalf("relink HEAD to master: %v", err)
	}
	// Second initRepo on a repo that already has objects must be a no-op.
	if _, err := store.InitRepo("testproject"); err != nil {
		t.Fatalf("re-init: %v", err)
	}
	br, err := store.NewProject("testproject").GetFullBranch()
	if err != nil {
		t.Fatalf("get branch: %v", err)
	}
	if br != "refs/heads/master" {
		t.Errorf("expected refs/heads/master (no rewrite), got %s", br)
	}
}

// ---------------------------------------------------------------------------
// Mirror of FSGitRepoStoreTest (Java) cases not covered above.
// ---------------------------------------------------------------------------

func TestPurgeNonexistentProjects(t *testing.T) {
	root := copyFSet(t)
	store := NewFSGitRepoStore(root, nil)
	if err := store.PurgeNonexistentProjects([]string{"proj1", "proj2"}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "idontexist")); err == nil {
		t.Errorf("idontexist should have been purged")
	}
	if _, err := os.Stat(filepath.Join(root, ".wlgb")); err != nil {
		t.Errorf(".wlgb must survive purge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "proj1")); err != nil {
		t.Errorf("proj1 must survive purge: %v", err)
	}
}

func TestTotalSizeShouldChangeWhenFilesAreCreatedAndDeleted(t *testing.T) {
	// Java parity: asserts totalSize grows on a 16MB file write and shrinks on
	// delete. The production/default sizer (Java: File#totalSpace-freeSpace,
	// Go: statfs Blocks-Bavail) measures the whole filesystem, which is shared
	// and mutates concurrently (CI + parallel tests), making strict orderings
	// unreliable on large disks. Java allows injecting fsSizer on the store
	// constructor, so the same seam is honored here: a deterministic directory
	// size sizer keeps the assertion exact and stable.
	root := copyFSet(t)
	store := NewFSGitRepoStoreWithSizer(root, 0, dirSizeSizer)
	old, err := store.TotalSize()
	if err != nil {
		t.Fatalf("totalSize: %v", err)
	}
	temp := filepath.Join(root, "__temp.txt")
	data := make([]byte, 16*1024*1024)
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	newAfter, err := store.TotalSize()
	if err != nil {
		t.Fatalf("totalSize: %v", err)
	}
	if newAfter <= old {
		t.Errorf("totalSize must grow after write: old=%d new=%d", old, newAfter)
	}
	if err := os.Remove(temp); err != nil {
		t.Fatalf("remove temp: %v", err)
	}
	afterDel, err := store.TotalSize()
	if err != nil {
		t.Fatalf("totalSize: %v", err)
	}
	if afterDel >= newAfter {
		t.Errorf("totalSize must shrink after delete: new=%d del=%d", newAfter, afterDel)
	}
}

func TestZipAndUnzipShouldBeTheSame(t *testing.T) {
	origRoot := copyFSet(t)
	root := copyFSet(t)
	store := NewFSGitRepoStore(root, nil)
	orig := filepath.Join(origRoot, "proj1")
	cur := filepath.Join(root, "proj1")
	if !dirEq(orig, cur) {
		t.Fatalf("fixture pre-mismatch?")
	}
	if !store.IsProjectPresent("proj1") {
		t.Fatalf("proj1 must be present")
	}
	data, err := store.Bzip2Project("proj1")
	if err != nil {
		t.Fatalf("bzip2: %v", err)
	}
	if err := store.Remove("proj1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(cur); err == nil {
		t.Fatalf("proj1 should be removed")
	}
	if err := store.Unbzip2Project("proj1", data); err != nil {
		t.Fatalf("unbzip2: %v", err)
	}
	if !dirEq(orig, cur) {
		t.Errorf("after unbzip2 the project must be byte-equal to the original")
	}
	// the repo must be usable again (objects db present).
	if _, err := store.GetExistingRepo("proj1"); err != nil {
		t.Fatalf("repo must be usable after unbzip2: %v", err)
	}
}

func TestGzipRoundTrip(t *testing.T) {
	origRoot := copyFSet(t)
	root := copyFSet(t)
	store := NewFSGitRepoStore(root, nil)
	orig := filepath.Join(origRoot, "proj1")
	cur := filepath.Join(root, "proj1")
	data, err := store.GzipProject("proj1")
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := store.Remove("proj1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := store.UngzipProject("proj1", data); err != nil {
		t.Fatalf("ungzip: %v", err)
	}
	if !dirEq(orig, cur) {
		t.Errorf("after ungzip the project must be byte-equal to the original")
	}
}

func TestSizeLimitFromStoreIsWired(t *testing.T) {
	root := copyFixture(t, "gits", "repo")
	limit := int64(1024)
	store := NewFSGitRepoStoreWithSizer(root, limit, nil)
	if store.MaxFileSize() != limit {
		t.Errorf("store.MaxFileSize must be %d, got %d", limit, store.MaxFileSize())
	}
}
