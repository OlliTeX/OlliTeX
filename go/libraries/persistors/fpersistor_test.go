package persistors

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ollitex/go/libraries/oerror"
)

// Scenario port of test/unit/FSPersistorTests.js: two settings scenarios
// (default / useSubdirectories) × the same method assertions. File
// contents mirror the Node fixtures.

const (
	fsInfoContents  = "This information is critical"
	fsOtherContents = "Some other content"
)

func setupFsScenario(t *testing.T, useSubdirectories bool) (tmpDir, location string, persistor *FSPersistor) {
	t.Helper()
	tmpDir = t.TempDir()
	location = filepath.Join(tmpDir, "bucket")

	// Node fixture tree:
	//  uploads/info.txt, uploads/other.txt
	//  not-a-dir   (regular file blocking the path)
	//  directory/subdirectory/ (empty dir)
	if err := os.MkdirAll(filepath.Join(tmpDir, "uploads"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTo := func(p, contents string) {
		if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeTo(filepath.Join(tmpDir, "uploads", "info.txt"), fsInfoContents)
	writeTo(filepath.Join(tmpDir, "uploads", "other.txt"), fsOtherContents)
	writeTo(filepath.Join(tmpDir, "not-a-dir"), "This regular file is meant to prevent using this path as a directory")
	if err := os.MkdirAll(filepath.Join(tmpDir, "directory", "subdirectory"), 0o755); err != nil {
		t.Fatal(err)
	}

	p, err := NewFSPersistor(FSSettings{UseSubdirectories: useSubdirectories})
	if err != nil {
		t.Fatal(err)
	}
	return tmpDir, location, p
}

func TestFSPersistorStorageClassRejected(t *testing.T) {
	_, err := NewFSPersistor(FSSettings{StorageClass: map[string]string{"b": "STANDARD"}})
	if !isNotImplErr(err) {
		t.Fatalf("want NotImplementedError, got %T (%v)", err, err)
	}
	if err == nil || !strings.Contains(err.Error(), "FS backend does not support storage classes") {
		t.Fatalf("message: %v", err)
	}
}

func isNotImplErr(err error) bool {
	_, ok := asPersistorError(err).(*NotImplementedError)
	return ok
}

func TestFSPersistorSendFile(t *testing.T) {
	for _, subdirs := range []bool{false, true} {
		tmpDir, location, p := setupFsScenario(t, subdirs)
		source := filepath.Join(tmpDir, "uploads", "info.txt")

		err := p.SendFile(location, "animals/wombat.tex", source)
		if err != nil {
			t.Fatalf("sendFile (subdirs=%v): %v", subdirs, err)
		}
		target := p.getFsPath(location, "animals/wombat.tex")
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read target (subdirs=%v): %v", subdirs, err)
		}
		if string(got) != fsInfoContents {
			t.Errorf("contents (subdirs=%v): %q", subdirs, got)
		}
		// no temp dirs left behind
		remaining := listTmpDirs(location)
		if len(remaining) != 0 {
			t.Errorf("temp dirs remain (subdirs=%v): %v", subdirs, remaining)
		}
	}
}

func listTmpDirs(location string) []string {
	entries, _ := os.ReadDir(location)
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "tmp-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestFSPersistorSendFileToBlockedLocation(t *testing.T) {
	tmpDir, _, p := setupFsScenario(t, false)
	notADir := filepath.Join(tmpDir, "not-a-dir")
	source := filepath.Join(tmpDir, "uploads", "info.txt")

	err := p.SendFile(notADir, "animals/wombat.tex", source)
	if err == nil {
		t.Fatal("want an error writing under a file path")
	}
	if !isNotImplErr(err) {
		// Node: a WriteError via wrapError (mkdir on a file → ENOTDIR)
		if _, isWrite := asPersistorError(err).(*WriteError); !isWrite {
			t.Fatalf("want WriteError, got %T (%v)", err, err)
		}
	}
}

func TestFSPersistorSendStreamWritesAndCleansTemp(t *testing.T) {
	tmpDir, location, p := setupFsScenario(t, false)

	err := p.SendStream(location, "animals/giraffe.tex", bytes.NewBufferString(fsOtherContents), Opts{})
	if err != nil {
		t.Fatalf("sendStream: %v", err)
	}
	target := p.getFsPath(location, "animals/giraffe.tex")
	got, _ := os.ReadFile(target)
	if string(got) != fsOtherContents {
		t.Fatalf("contents: %q", got)
	}
	if remaining := listTmpDirs(location); len(remaining) != 0 {
		t.Fatalf("temp dirs remain: %v", remaining)
	}
	_ = tmpDir
}

func TestFSPersistorSendStreamSourceMd5Match(t *testing.T) {
	tmpDir, location, p := setupFsScenario(t, false)
	want := md5HexOfString(fsOtherContents)
	defer func() { _ = tmpDir }()

	err := p.SendStream(location, "p/tato.tex", bytes.NewBufferString(fsOtherContents), Opts{SourceMD5: want})
	if err != nil {
		t.Fatalf("md5 match should succeed: %v", err)
	}
}

func TestFSPersistorSendStreamSourceMd5Mismatch(t *testing.T) {
	_, location, p := setupFsScenario(t, false)

	err := p.SendStream(location, "p/tato.tex", bytes.NewBufferString(fsOtherContents), Opts{SourceMD5: "badbad"})
	if err == nil {
		t.Fatal("md5 mismatch must fail")
	}
	// Node: WriteError('failed to write stream') wrapping WriteError('md5 hash mismatch')
	if !strings.Contains(err.Error(), "failed to write stream") {
		t.Fatalf("outer message: %v", err)
	}
	if full := oerror.GetFullInfo(err); full["cause"] == nil {
		t.Fatalf("cause chain missing: %v", full)
	}
	// the target must NOT have been written
	if _, serr := os.Stat(p.getFsPath(location, "p/tato.tex")); !os.IsNotExist(serr) {
		t.Fatal("target file was written despite md5 mismatch")
	}
	if remaining := listTmpDirs(location); len(remaining) != 0 {
		t.Fatalf("temp dirs remain after mismatch: %v", remaining)
	}
}

func TestFSPersistorSendStreamIfNoneMatchRefused(t *testing.T) {
	_, _, p := setupFsScenario(t, false)
	err := p.SendStream("loc", "k", bytes.NewBufferString("x"), Opts{IfNoneMatch: "*"})
	if !isNotImplErr(err) {
		t.Fatalf("want NotImplementedError, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "Overwrite protection required by caller") {
		t.Fatalf("message: %v", err)
	}
}

func TestFSPersistorGetObjectStream(t *testing.T) {
	tmpDir, location, p := setupFsScenario(t, false)
	_ = tmpDir
	if err := p.SendFile(location, "animals/wombat.tex", mustTempFile(t, fsInfoContents)); err != nil {
		t.Fatal(err)
	}

	stream, err := p.GetObjectStream(location, "animals/wombat.tex", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(stream)
	stream.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != fsInfoContents {
		t.Fatalf("contents: %q", got)
	}
}

func TestFSPersistorGetObjectStreamRange(t *testing.T) {
	_, location, p := setupFsScenario(t, false)
	if err := p.SendFile(location, "animals/wombat.tex", mustTempFile(t, fsInfoContents)); err != nil {
		t.Fatal(err)
	}
	start, end := int64(5), int64(16)
	stream, err := p.GetObjectStream(location, "animals/wombat.tex", Opts{Start: &start, End: &end})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(stream)
	stream.Close()
	// end is INCLUSIVE: slice(5, 17)
	want := fsInfoContents[5:17]
	if string(got) != want {
		t.Fatalf("range: %q, want %q", got, want)
	}
}

func TestFSPersistorGetObjectStreamNotFound(t *testing.T) {
	_, _, p := setupFsScenario(t, false)
	_, err := p.GetObjectStream("loc", "does-not-exist", Opts{})
	if !isNotFoundError(err) {
		t.Fatalf("want NotFoundError, got %T (%v)", err, err)
	}
}

func TestFSPersistorGetObjectStreamAutoGunzipRefused(t *testing.T) {
	_, _, p := setupFsScenario(t, false)
	_, err := p.GetObjectStream("loc", "k", Opts{AutoGunzip: true})
	if !isNotImplErr(err) {
		t.Fatalf("want NotImplementedError, got %T", err)
	}
	if !strings.Contains(err.Error(), "autoGunzip is not supported by FS backend") {
		t.Fatalf("message: %v", err)
	}
}

func TestFSPersistorGetObjectSize(t *testing.T) {
	_, location, p := setupFsScenario(t, false)
	if err := p.SendFile(location, "animals/wombat.tex", mustTempFile(t, fsInfoContents)); err != nil {
		t.Fatal(err)
	}
	size, err := p.GetObjectSize(location, "animals/wombat.tex", Opts{})
	if err != nil || size != int64(len(fsInfoContents)) {
		t.Fatalf("size = %d (%v)", size, err)
	}
	if _, err := p.GetObjectSize(location, "does-not-exist", Opts{}); !isNotFoundError(err) {
		t.Fatalf("want NotFoundError, got %v", err)
	}
}

func TestFSPersistorGetObjectMd5Hash(t *testing.T) {
	_, location, p := setupFsScenario(t, false)
	src := mustTempFile(t, fsInfoContents)
	if err := p.SendFile(location, "animals/wombat.tex", src); err != nil {
		t.Fatal(err)
	}
	got, err := p.GetObjectMd5Hash(location, "animals/wombat.tex", Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if got != md5HexOfString(fsInfoContents) {
		t.Fatalf("md5 = %q", got)
	}
	// Node: direct ReadError for md5 (not the wrapError/NotFound mapping)
	_, err = p.GetObjectMd5Hash(location, "does-not-exist", Opts{})
	if _, isRead := asPersistorError(err).(*ReadError); !isRead {
		t.Fatalf("want ReadError, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "unable to get md5 hash from file") {
		t.Fatalf("message: %v", err)
	}
}

func TestFSPersistorCopyObject(t *testing.T) {
	_, location, p := setupFsScenario(t, false)
	src := mustTempFile(t, fsInfoContents)
	if err := p.SendFile(location, "animals/wombat.tex", src); err != nil {
		t.Fatal(err)
	}
	if err := p.CopyObject(location, "animals/wombat.tex", "vegetables/potato.tex", Opts{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p.getFsPath(location, "vegetables/potato.tex"))
	if err != nil || string(got) != fsInfoContents {
		t.Fatalf("copied: %q (%v)", got, err)
	}
}

func TestFSPersistorDeleteObject(t *testing.T) {
	_, location, p := setupFsScenario(t, false)
	src := mustTempFile(t, fsInfoContents)
	if err := p.SendFile(location, "animals/wombat.tex", src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.getFsPath(location, "animals/wombat.tex")); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteObject(location, "animals/wombat.tex"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.getFsPath(location, "animals/wombat.tex")); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
	// deleting a missing file is a no-op (S3 parity)
	if err := p.DeleteObject(location, "does/not/exist"); err != nil {
		t.Fatalf("delete-missing must be a no-op: %v", err)
	}
}

func TestFSPersistorDeleteDirectory(t *testing.T) {
	tmpDir, location, p := setupFsScenario(t, false)
	_ = tmpDir
	src := mustTempFile(t, fsInfoContents)
	if err := p.SendFile(location, "animals/wombat.tex", src); err != nil {
		t.Fatal(err)
	}
	if err := p.SendFile(location, "animals/giraffe.tex", src); err != nil {
		t.Fatal(err)
	}
	// a file OUTSIDE the "animals" set
	if err := p.SendFile(location, "other/thing.txt", src); err != nil {
		t.Fatal(err)
	}

	if err := p.DeleteDirectory(location, "animals", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.getFsPath(location, "animals/wombat.tex")); !os.IsNotExist(err) {
		t.Fatal("animals/wombat.tex should be gone")
	}
	if _, err := os.Stat(p.getFsPath(location, "other/thing.txt")); err != nil {
		t.Fatal("other/thing.txt must remain")
	}
	// deleting a missing directory is a no-op
	if err := p.DeleteDirectory(location, "does-not-exist", ""); err != nil {
		t.Fatalf("delete-missing-dir: %v", err)
	}
}

func TestFSPersistorDeleteDirectorySubdirectories(t *testing.T) {
	tmpDir, _, p := setupFsScenario(t, true)
	location := filepath.Join(tmpDir, "bucket")
	src := mustTempFile(t, fsInfoContents)
	if err := p.SendFile(location, "animals/wombat.tex", src); err != nil {
		t.Fatal(err)
	}
	if err := p.SendFile(location, "other/thing.txt", src); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteDirectory(location, "animals", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(location, "animals", "wombat.tex")); !os.IsNotExist(err) {
		t.Fatal("subdir tree should be gone")
	}
	if _, err := os.Stat(filepath.Join(location, "other", "thing.txt")); err != nil {
		t.Fatal("other tree must remain")
	}
}

func TestFSPersistorCheckIfObjectExists(t *testing.T) {
	_, location, p := setupFsScenario(t, false)
	src := mustTempFile(t, fsInfoContents)
	if err := p.SendFile(location, "animals/wombat.tex", src); err != nil {
		t.Fatal(err)
	}
	ok, err := p.CheckIfObjectExists(location, "animals/wombat.tex", Opts{})
	if err != nil || !ok {
		t.Fatalf("exists = %v (%v)", ok, err)
	}
	ok, err = p.CheckIfObjectExists(location, "does-not-exist", Opts{})
	if err != nil || ok {
		t.Fatalf("exists = %v (%v)", ok, err)
	}
}

func TestFSPersistorDirectorySize(t *testing.T) {
	_, location, p := setupFsScenario(t, false)
	src := mustTempFile(t, fsInfoContents)
	if err := p.SendFile(location, "animals/wombat.tex", src); err != nil {
		t.Fatal(err)
	}
	if err := p.SendFile(location, "animals/giraffe.tex", src); err != nil {
		t.Fatal(err)
	}
	size, err := p.DirectorySize(location, "animals", "")
	if err != nil {
		t.Fatal(err)
	}
	if size != 2*int64(len(fsInfoContents)) {
		t.Fatalf("size = %d, want %d", size, 2*len(fsInfoContents))
	}
	// missing directory → 0 (Node: returns 0 on non-existing dirs)
	zero, err := p.DirectorySize(location, "does-not-exist", "")
	if err != nil {
		// Node: the list returns [] for a missing dir → sum 0 (no throw)
		t.Fatalf("size on missing dir: %v", err)
	}
	if zero != 0 {
		t.Fatalf("zero = %d", zero)
	}
}

func TestFSPersistorListDirectoryKeys(t *testing.T) {
	_, location, p := setupFsScenario(t, false)
	src := mustTempFile(t, fsInfoContents)
	for _, key := range []string{"animals/wombat.tex", "animals/giraffe.tex"} {
		if err := p.SendFile(location, key, src); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := p.ListDirectoryKeys(location, "animals")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("keys = %v", keys)
	}
	// Node: the FULL paths are returned
	for _, k := range keys {
		if !strings.HasPrefix(k, location) {
			t.Fatalf("expected full path, got %q", k)
		}
	}
	empty, err := p.ListDirectoryKeys(location, "does-not-exist")
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing dir: %v, %v", empty, err)
	}
}

func TestFSPersistorListDirectoryStats(t *testing.T) {
	_, location, p := setupFsScenario(t, false)
	src := mustTempFile(t, fsInfoContents)
	if err := p.SendFile(location, "animals/wombat.tex", src); err != nil {
		t.Fatal(err)
	}
	stats, err := p.ListDirectoryStats(location, "animals")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("stats = %v", stats)
	}
	if stats[0].Size != int64(len(fsInfoContents)) {
		t.Fatalf("size = %d", stats[0].Size)
	}
}

func TestFSPersistorDeleteDirectoryTokenIgnored(t *testing.T) {
	_, location, p := setupFsScenario(t, false)
	src := mustTempFile(t, fsInfoContents)
	if err := p.SendFile(location, "animals/wombat.tex", src); err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteDirectory(location, "animals", "someToken"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.getFsPath(location, "animals/wombat.tex")); !os.IsNotExist(err) {
		t.Fatal("should be gone")
	}
}

func TestFSPersistorGetRedirectURLNull(t *testing.T) {
	_, _, p := setupFsScenario(t, false)
	url, err := p.GetRedirectURL("loc", "k")
	if err != nil || url != "" {
		t.Fatalf("redirect = %q (%v), want empty/nil", url, err)
	}
}

// helpers

func md5HexOfString(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func mustTempFile(t *testing.T, contents string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "source-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(contents); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}
