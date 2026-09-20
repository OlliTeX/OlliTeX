package resourcestatemanager

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	oerrors "clsi/errors"
)

func mkRes(paths ...string) []Resource {
	out := []Resource{}
	for _, p := range paths {
		out = append(out, Resource{Path: p})
	}
	return out
}

func TestSaveProjectState_WritesResourceList(t *testing.T) {
	dir := t.TempDir()
	res := mkRes("resource-1-mock", "resource-2-mock", "resource-3-mock")
	state := "1234567890"
	if err := SaveProjectState(&state, res, dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, SyncStateFile))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	want := "resource-1-mock\nresource-2-mock\nresource-3-mock\nstateHash:1234567890"
	if string(got) != want {
		t.Errorf("state file = %q, want %q", got, want)
	}
}

func TestSaveProjectState_ClearsWhenNil(t *testing.T) {
	dir := t.TempDir()
	state := "abc"
	if err := SaveProjectState(&state, []Resource{{Path: "a"}}, dir); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	// nil state -> unlink (no error)
	if err := SaveProjectState(nil, []Resource{}, dir); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, SyncStateFile)); !os.IsNotExist(err) {
		t.Fatalf("state file should be unlinked, got stat err=%v", err)
	}
	// clearing a non-existent file swallows ENOENT
	if err := SaveProjectState(nil, []Resource{}, dir); err != nil {
		t.Fatalf("clear ENOENT should be swallowed, got %v", err)
	}
}

func TestCheckProjectStateMatches_Match(t *testing.T) {
	dir := t.TempDir()
	res := mkRes("resource-1-mock", "resource-2-mock", "resource-3-mock")
	state := "1234567890"
	if err := SaveProjectState(&state, res, dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := CheckProjectStateMatches(state, dir)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	want := mkRes("resource-1-mock", "resource-2-mock", "resource-3-mock")
	if len(got) != len(want) {
		t.Fatalf("resources = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].Path != want[i].Path {
			t.Errorf("resources[%d] = %q, want %q", i, got[i].Path, want[i].Path)
		}
	}
}

func TestCheckProjectStateMatches_MissingFile(t *testing.T) {
	dir := t.TempDir()
	// no state file present -> SafeReader ENOENT -> FilesOutOfSync
	_, err := CheckProjectStateMatches("1234567890", dir)
	if err == nil {
		t.Fatalf("want error for missing state file")
	}
	var oerr *oerrors.FilesOutOfSyncError
	if !errors.As(err, &oerr) {
		t.Fatalf("want FilesOutOfSyncError, got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "invalid state for incremental update") {
		t.Errorf("message = %q, want contain 'invalid state for incremental update'", err.Error())
	}
}

func TestCheckProjectStateMatches_Mismatch(t *testing.T) {
	dir := t.TempDir()
	state := "1234567890"
	if err := SaveProjectState(&state, mkRes("a"), dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	_, err := CheckProjectStateMatches("not-the-original-state", dir)
	if err == nil {
		t.Fatalf("want mismatch error")
	}
	if !strings.Contains(err.Error(), "invalid state for incremental update") {
		t.Errorf("message = %q, want 'invalid state for incremental update'", err.Error())
	}
}

func TestCheckResourceFiles_AllPresent(t *testing.T) {
	err := CheckResourceFiles(mkRes("a", "b", "c"), []string{"a", "b", "c"}, "/base")
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
}

func TestCheckResourceFiles_MissingFile(t *testing.T) {
	err := CheckResourceFiles(mkRes("a", "b", "c"), []string{"a", "b"}, "/base")
	if err == nil {
		t.Fatalf("want error for missing file")
	}
	if !strings.Contains(err.Error(), "resource files missing in incremental update") {
		t.Errorf("message = %q, want 'resource files missing in incremental update'", err.Error())
	}
}

func TestCheckResourceFiles_RelativePath(t *testing.T) {
	res := mkRes("../foo/bar.tex", "b")
	err := CheckResourceFiles(res, []string{"../foo/bar.tex", "b"}, "/base")
	if err == nil {
		t.Fatalf("want relative path error")
	}
	if !strings.Contains(err.Error(), "relative path in resource file list") {
		t.Errorf("message = %q, want 'relative path in resource file list'", err.Error())
	}
	// must be a plain error, not FilesOutOfSync (Node: matches Error, not the OError)
	if _, ok := err.(*oerrors.FilesOutOfSyncError); ok {
		t.Errorf("relative path must be plain Error, got FilesOutOfSyncError")
	}
}
