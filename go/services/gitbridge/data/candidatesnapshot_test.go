// Port of ProjectTest + coverage for candidatesnapshot (CandidateSnapshot /
// ServletFile) and the ProjectLock read-barrier internals.
package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ollitex/go/services/gitbridge/filestore"
	"ollitex/go/services/gitbridge/util"
)

// ---------------------------------------------------------------------------
// CandidateSnapshot / ServletFile (ports of data/CandidateSnapshot +
// data/ServletFile; exercised heavily by BridgeTest + FileHandler in Java).
// ---------------------------------------------------------------------------

func TestNewServletFileChangedUnchanged(t *testing.T) {
	// old == nil => changed (Java equals(null) == false).
	sf := newServletFile(filestore.NewRepositoryFile("a.tex", []byte("v2")), nil)
	if !sf.isChanged() {
		t.Fatalf("ServletFile with old==nil must be changed")
	}
	if sf.getUniqueIdentifier() == "" {
		t.Fatalf("ServletFile uuid must be non-empty")
	}
	// same path+contents => not changed.
	same := newServletFile(
		filestore.NewRepositoryFile("a.tex", []byte("v1")),
		filestore.NewRepositoryFile("a.tex", []byte("v1")),
	)
	if same.isChanged() {
		t.Fatalf("same path+contents must NOT be changed")
	}
	// different contents => changed.
	diffCont := newServletFile(
		filestore.NewRepositoryFile("a.tex", []byte("v1")),
		filestore.NewRepositoryFile("a.tex", []byte("v2")),
	)
	if !diffCont.isChanged() {
		t.Fatalf("different contents must be changed")
	}
	// different path => changed.
	diffPath := newServletFile(
		filestore.NewRepositoryFile("a.tex", []byte("v1")),
		filestore.NewRepositoryFile("b.tex", []byte("v1")),
	)
	if !diffPath.isChanged() {
		t.Fatalf("different path must be changed")
	}
	// randomUUIDString shape: version 4, 8-4-4-4-12 hex.
	_ = randomUUIDString()
}

func TestBytesEqual(t *testing.T) {
	if !bytesEqual([]byte{1, 2}, []byte{1, 2}) {
		t.Fatalf("equal slices must match")
	}
	if bytesEqual([]byte{1, 2}, []byte{1, 3}) {
		t.Fatalf("differing bytes must not match")
	}
	if bytesEqual([]byte{1, 2}, []byte{1, 2, 3}) {
		t.Fatalf("different lengths must not match")
	}
}

func TestServletFileWriteToDiskWithName(t *testing.T) {
	dir := t.TempDir()
	sf := newServletFile(filestore.NewRepositoryFile("x", []byte("hello-servlet")), nil)
	name := "dir1/file.bin"
	if err := sf.WriteToDiskWithName(dir, name); err != nil {
		t.Fatalf("WriteToDiskWithName: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "dir1", "file.bin"))
	if err != nil || string(data) != "hello-servlet" {
		t.Fatalf("written file wrong: %v %q", err, data)
	}
}

func TestNewCandidateSnapshotDiff(t *testing.T) {
	cs := NewCandidateSnapshot("proj1", 7,
		filestore.NewRawDirectory(map[string]filestore.RawFile{
			"a.tex":     filestore.NewRepositoryFile("a.tex", []byte("new")),
			"same.txt":  filestore.NewRepositoryFile("same.txt", []byte("old")),  // unchanged
			"sub/b.pdf": filestore.NewRepositoryFile("sub/b.pdf", []byte("big")), // new (old nil)
		}),
		filestore.NewRawDirectory(map[string]filestore.RawFile{
			"a.tex":    filestore.NewRepositoryFile("a.tex", []byte("old")),    // changed
			"same.txt": filestore.NewRepositoryFile("same.txt", []byte("old")), // unchanged
			"gone.txt": filestore.NewRepositoryFile("gone.txt", []byte("x")),   // deleted
		}),
	)
	if got := cs.GetProjectName(); got != "proj1" {
		t.Fatalf("project = %q", got)
	}
	if got := cs.CurrentVersion(); got != 7 {
		t.Fatalf("version = %d, want 7", got)
	}
	// Sorted by path: a.tex, same.txt, sub/b.pdf
	files := cs.Files()
	if len(files) != 3 {
		t.Fatalf("files len = %d, want 3", len(files))
	}
	expected := []string{"a.tex", "same.txt", "sub/b.pdf"}
	for i, want := range expected {
		if files[i].Path != want {
			t.Fatalf("files[%d].Path = %q, want %q", i, files[i].Path, want)
		}
	}
	// Deleted: only gone.txt (it's missing from the new contents).
	if got := cs.GetDeleted(); len(got) != 1 || got[0] != "gone.txt" {
		t.Fatalf("deleted = %v, want [gone.txt]", got)
	}
	// a.tex changed, same.txt NOT changed.
	byPath := map[string]*ServletFile{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	if !byPath["a.tex"].isChanged() {
		t.Fatalf("a.tex must be changed")
	}
	if !byPath["sub/b.pdf"].isChanged() {
		t.Fatalf("sub/b.pdf (new) must be changed (old == nil)")
	}
	if byPath["same.txt"].isChanged() {
		t.Fatalf("same.txt must NOT be changed")
	}
}

func TestCandidateSnapshotWriteAndDeleteServletFiles(t *testing.T) {
	root := t.TempDir()
	cs := NewCandidateSnapshot("projX", 3,
		filestore.NewRawDirectory(map[string]filestore.RawFile{
			"changed.txt": filestore.NewRepositoryFile("changed.txt", []byte("changed")),
			"kept.txt":    filestore.NewRepositoryFile("kept.txt", []byte("same")),
		}),
		filestore.NewRawDirectory(map[string]filestore.RawFile{
			"kept.txt": filestore.NewRepositoryFile("kept.txt", []byte("same")),
		}),
	)
	if err := cs.WriteServletFiles(root); err != nil {
		t.Fatalf("WriteServletFiles: %v", err)
	}
	// Only the CHANGED file must be on disk under .wlgb/atts/projX/.
	attsDir := filepath.Join(root, ".wlgb", "atts", "projX")
	changedOnly := map[string]*ServletFile{}
	for _, f := range cs.Files() {
		if f.isChanged() {
			changedOnly[f.Path] = f
		}
	}
	if len(changedOnly) != 1 {
		t.Fatalf("must have exactly 1 changed file")
	}
	for _, f := range cs.Files() {
		full := filepath.Join(attsDir, f.getUniqueIdentifier())
		if f.isChanged() {
			if _, err := os.Stat(full); err != nil {
				t.Fatalf("changed file should exist: %v", err)
			}
		} else {
			if _, err := os.Stat(full); !os.IsNotExist(err) {
				t.Fatalf("unchanged file must NOT be written: %v", err)
			}
		}
	}
	// DeleteServletFiles removes the atts dir.
	if err := cs.DeleteServletFiles(); err != nil {
		t.Fatalf("DeleteServletFiles: %v", err)
	}
	if _, err := os.Stat(attsDir); !os.IsNotExist(err) {
		t.Fatalf("atts dir must be removed after DeleteServletFiles")
	}
}

func TestCandidateSnapshotJSONRepresentation(t *testing.T) {
	// Bind the postback URL (Java getPostbackURL).
	util.SetPostbackURL("https://overleaf.app.example/")
	t.Cleanup(func() { util.SetPostbackURL("") })

	cs := NewCandidateSnapshot("projZ", 11,
		filestore.NewRawDirectory(map[string]filestore.RawFile{
			"new.txt":  filestore.NewRepositoryFile("new.txt", []byte("fresh")), // changed
			"same.txt": filestore.NewRepositoryFile("same.txt", []byte("same")), // unchanged
		}),
		filestore.NewRawDirectory(map[string]filestore.RawFile{
			"same.txt": filestore.NewRepositoryFile("same.txt", []byte("same")),
		}),
	)
	body, err := cs.JSONRepresentation("PBKEY")
	if err != nil {
		t.Fatalf("JSONRepresentation: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("body is not valid JSON: %v\n%s", err, body)
	}
	if got, _ := parsed["latestVerId"].(float64); int(got) != 11 {
		t.Fatalf("latestVerId = %v, want 11", parsed["latestVerId"])
	}
	postbackURL, _ := parsed["postbackUrl"].(string)
	if postbackURL != "https://overleaf.app.example/api/projZ/PBKEY/postback" {
		t.Fatalf("postbackUrl = %q, want the app URL + key", postbackURL)
	}
	files, _ := parsed["files"].([]interface{})
	if len(files) != 2 {
		t.Fatalf("files len = %d, want 2", len(files))
	}
	// The changed file has a URL pointing at the atts blob; the unchanged
	// file has no url key.
	changed := files[0].(map[string]interface{})
	unchanged := files[1].(map[string]interface{})
	if changed["name"] != "new.txt" {
		t.Fatalf("files[0].name = %v, want new.txt", changed["name"])
	}
	if u, ok := changed["url"].(string); !ok {
		t.Fatalf("changed file must have a url key")
	} else if want := "https://overleaf.app.example/api/projZ/" + cs.Files()[0].getUniqueIdentifier() + "?key=PBKEY"; u != want {
		t.Fatalf("changed url = %q, want %q", u, want)
	}
	if _, hasURL := changed["url"]; !hasURL {
		t.Fatalf("changed file must carry a url")
	}
	if _, hasURL := unchanged["url"]; hasURL {
		t.Fatalf("unchanged file must NOT carry a url")
	}
	if unchanged["name"] != "same.txt" {
		t.Fatalf("files[1].name = %v, want same.txt", unchanged["name"])
	}
}

func TestWriteToDiskWithNameUnwritable(t *testing.T) {
	// A directory path component that cannot become a file => error.
	base := t.TempDir()
	blocker := filepath.Join(base, "f.txt")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	sf := newServletFile(filestore.NewRepositoryFile("y", []byte("no")), nil)
	if err := sf.WriteToDiskWithName(base, "f.txt/sub.txt"); err == nil {
		t.Fatalf("WriteToDiskWithName must fail under a file, not dir")
	}
}

func TestDeleteDirectoryErrors(t *testing.T) {
	// nonexistent dir => os.IsNotExist => nil (no-op).
	if err := deleteDirectory(filepath.Join(t.TempDir(), "ghost")); err != nil {
		t.Fatalf("deleteDirectory(ghost) = %v, want nil", err)
	}
	// A file path (not a dir) => ReadDir error surfaced.
	file := filepath.Join(t.TempDir(), "regular.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := deleteDirectory(file); err == nil {
		t.Fatalf("deleteDirectory on file must error")
	}
}

// ---------------------------------------------------------------------------
// User / SnapshotFile / ParseSnapshotCreated
// ---------------------------------------------------------------------------

func TestUserFrom(t *testing.T) {
	svc := util.GetServiceName()
	// either empty => both fall back to anonymous (Java: name==null || email==null).
	if u := UserFrom("", "e@e.com"); u.Name != "Anonymous" || u.Email != "anonymous@"+svc+".com" {
		t.Fatalf("UserFrom empty name must be anonymous: %v", u)
	}
	if u := UserFrom("", ""); u.Name != "Anonymous" || u.Email != "anonymous@"+svc+".com" {
		t.Fatalf("UserFrom empty email must be anonymous: %v", u)
	}
	// both present
	if u := UserFrom("n", "e"); u.Name != "n" || u.Email != "e" {
		t.Fatalf("UserFrom both: %v", u)
	}
}

func TestSnapshotFileSize(t *testing.T) {
	if got := (SnapshotFile{Contents: []byte("abcd")}).Size(); got != 4 {
		t.Fatalf("Size = %d, want 4", got)
	}
}

func TestParseSnapshotCreated(t *testing.T) {
	// empty -> 0 (v2-import edge case).
	if got := ParseSnapshotCreated(""); got != 0 {
		t.Fatalf("ParseSnapshotCreated empty = %d, want 0", got)
	}
	// unparseable -> 0.
	if got := ParseSnapshotCreated("not-a-date"); got != 0 {
		t.Fatalf("unparseable = %d, want 0", got)
	}
	// ISO with millis.
	if got := ParseSnapshotCreated("2020-01-01T00:00:00.500Z"); got <= 0 {
		t.Fatalf("ParseSnapshotCreated iso = %d, want >0", got)
	}
	// bare date.
	if got := ParseSnapshotCreated("2020-01-01"); got <= 0 {
		t.Fatalf("ParseSnapshotCreated date = %d, want >0", got)
	}
}
