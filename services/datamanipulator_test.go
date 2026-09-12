package services

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- pure helpers -----------------------------------------------------------

func TestDMSyncExcluded(t *testing.T) {
	excluded := []string{".git", "sub/.git/config", ".hidden", "a.b.toc", "main.aux", "build.log", "out.fls", "x.synctex.gz", "", "a.idx", "b.vrb"}
	for _, name := range excluded {
		if !dmSyncExcluded(name) {
			t.Fatalf("expected EXCLUDED: %q", name)
		}
	}
	kept := []string{"main.tex", "sub/file.txt", "docs/readme.md", "image.png", "notes.synctex.txt"}
	for _, name := range kept {
		if dmSyncExcluded(name) {
			t.Fatalf("expected KEPT: %q", name)
		}
	}
}

func TestDMExtOf(t *testing.T) {
	cases := map[string]string{
		"a/b/c.pdf":    "pdf",
		"noext":        "noext",
		".hidden":      "hidden",
		"file.tar.gz":  "gz",
		"sub/file.exe": "exe",
		"d/f.7z":       "7z",
		"trailing.":    "",
	}
	for in, want := range cases {
		if got := dmExtOf(in); got != want {
			t.Fatalf("dmExtOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDMDetectFileType(t *testing.T) {
	if typ, _ := dmDetectFileType("x.pdf", []byte("anything")); typ != DMBinary {
		t.Fatal(".pdf should be binary")
	}
	if typ, enc := dmDetectFileType("notes", nil); typ != DMText || enc != "utf8" {
		t.Fatal("empty should be text/utf8")
	}
	// >5% null bytes
	binary := make([]byte, 100)
	for i := 0; i < 20; i++ {
		binary[i] = 0
	}
	if typ, _ := dmDetectFileType("data", binary); typ != DMBinary {
		t.Fatal(">5% null bytes should be binary")
	}
	if typ, enc := dmDetectFileType("doc", []byte("hello world")); typ != DMText || enc != "utf8" {
		t.Fatal("ascii should be text/utf8")
	}
	// invalid utf-8, no nulls -> latin1
	latin := []byte{0xff, 0xfe, 0xfd, 'a', 'b', 'c'}
	if typ, enc := dmDetectFileType("legacy", latin); typ != DMText || enc != "latin1" {
		t.Fatalf("invalid-utf8 no-null should be latin1, got %s/%s", typ, enc)
	}
}

func TestDMChecksum(t *testing.T) {
	if got := dmChecksum(nil); got != "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty checksum = %q", got)
	}
	got := dmChecksum([]byte("abc"))
	want := "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Fatalf("chksum = %q want %q", got, want)
	}
}

func TestDMResolveProjectPath(t *testing.T) {
	if _, err := dmResolveProjectPath("/proj", "../outside"); err == nil {
		t.Fatal("traversal should be rejected")
	}
	if got, err := dmResolveProjectPath("/proj", "sub/file.txt"); err != nil || got == "" {
		t.Fatalf("valid path should resolve: %q %v", got, err)
	}
}

func TestDMResolveConflictByMtime(t *testing.T) {
	local := map[string]interface{}{"mtime": "2026-01-02T00:00:00.000Z"}
	remote := map[string]interface{}{"mtime": "2026-01-01T00:00:00.000Z"}
	if got := dmResolveConflictByMtime(local, remote); got != "left" {
		t.Fatalf("local newer -> left, got %q", got)
	}
	if got := dmResolveConflictByMtime(remote, local); got != "right" {
		t.Fatalf("remote newer -> right, got %q", got)
	}
	if got := dmResolveConflictByMtime(local, map[string]interface{}{"mtime": "2026-01-02T00:00:00.000Z"}); got != "needs_review" {
		t.Fatalf("equal -> needs_review, got %q", got)
	}
	if got := dmResolveConflictByMtime(map[string]interface{}{}, map[string]interface{}{}); got != "needs_review" {
		t.Fatalf("missing -> needs_review, got %q", got)
	}
}

func TestDMIsValidProjectId(t *testing.T) {
	if !dmIsValidProjectId("0123456789ab") || !dmIsValidProjectId("ABCDEF012345") {
		t.Fatal("12-hex should be valid")
	}
	if dmIsValidProjectId("0123456789a") || dmIsValidProjectId("../12") || dmIsValidProjectId("") || dmIsValidProjectId("0123456789ZZ") {
		t.Fatal("invalid ids should be rejected")
	}
}

func TestDMCompareTrees(t *testing.T) {
	entry := func(p, sum string, size int) map[string]interface{} {
		e := map[string]interface{}{"relative_path": p, "name": p, "type": "file", "size": size}
		if sum != "" {
			e["checksum"] = sum
		}
		return e
	}
	left := DMTreeResult{Entries: []DMTreeEntry{
		entry("a.txt", "sha256:aaa", 10),
		entry("same.txt", "sha256:sam", 5),
		entry("diff.txt", "sha256:L", 5),
		entry("nosum.txt", "", 3),
	}}
	right := DMTreeResult{Entries: []DMTreeEntry{
		entry("a.txt", "sha256:BBB", 10),
		entry("same.txt", "sha256:sam", 5),
		entry("diff.txt", "sha256:R", 5),
		entry("nosum.txt", "", 3),
		entry("r.txt", "sha256:rr", 2),
	}}
	res := dmCompareTrees(left, right)
	cnt := func(k string) int { n, _ := res[k].([]interface{}); return len(n) }
	if cnt("conflicts") != 2 {
		t.Fatalf("conflicts = %d (want 2)", cnt("conflicts"))
	}
	if cnt("onlyInLeft") != 0 {
		t.Fatalf("onlyInLeft = %d (want 0)", cnt("onlyInLeft"))
	}
	if cnt("onlyInRight") != 1 {
		t.Fatalf("onlyInRight = %d (want 1)", cnt("onlyInRight"))
	}
	if cnt("identical") != 1 {
		t.Fatalf("identical = %d (want 1)", cnt("identical"))
	}
	if cnt("unknown") != 1 {
		t.Fatalf("unknown = %d (want 1: nosum.txt equal size)", cnt("unknown"))
	}
}

// --- file operations --------------------------------------------------------

func dmTestProject(t *testing.T) (string, func()) {
	root := t.TempDir()
	projID := "0123456789ab"
	dir := filepath.Join(root, projID)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite := func(rel, content string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("main.tex", "\\documentclass{article}")
	mustWrite("sub/note.txt", "hello")
	mustWrite("build.aux", "aux-transient") // should be excluded
	return dir, func() {}
}

func TestDMWalkTree(t *testing.T) {
	dir, _ := dmTestProject(t)
	tree, err := dmWalkTree(dir, "")
	if err != nil {
		t.Fatalf("walkTree: %v", err)
	}
	present := map[string]bool{}
	for _, e := range tree.Entries {
		p, _ := e["relative_path"].(string)
		present[p] = true
	}
	if !present["main.tex"] || !present["sub"] || !present["sub/note.txt"] {
		t.Fatalf("missing expected entries: %v", present)
	}
	if present["build.aux"] {
		t.Fatal("sync-excluded .aux should not be listed")
	}
	if tree.TotalFiles != 2 {
		t.Fatalf("totalFiles = %d (want 2)", tree.TotalFiles)
	}
	// directory entry depth
	for _, e := range tree.Entries {
		if e["relative_path"] == "sub" {
			if d, _ := e["depth"].(int); d != 0 {
				t.Fatalf("sub depth = %v (want 0)", e["depth"])
			}
		}
	}
}

func TestDMReadWriteDelete(t *testing.T) {
	dir, _ := dmTestProject(t)
	data, err := dmReadFile(dir, "main.tex")
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	cb, _ := data["content_base64"].(string)
	dec, _ := base64.StdEncoding.DecodeString(cb)
	if string(dec) != "\\documentclass{article}" {
		t.Fatalf("content = %q", dec)
	}
	if _, err := dmReadFile(dir, "nope.txt"); err == nil {
		t.Fatal("missing file should error")
	}
	// write nested (creates parent)
	if _, err := dmWriteFile(dir, "new/deep/file.txt", []byte("created")); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	if st, e := os.Stat(filepath.Join(dir, "new/deep/file.txt")); e != nil || st.Size() != 7 {
		t.Fatalf("nested write not on disk")
	}
	// delete file
	if err := dmDeletePath(dir, "new/deep/file.txt"); err != nil {
		t.Fatalf("delete file: %v", err)
	}
	if _, e := os.Stat(filepath.Join(dir, "new/deep/file.txt")); e == nil {
		t.Fatal("file should be deleted")
	}
	// delete a directory
	if _, err := dmWriteFile(dir, "rmdir/a.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := dmDeletePath(dir, "rmdir"); err != nil {
		t.Fatalf("delete dir: %v", err)
	}
	if _, e := os.Stat(filepath.Join(dir, "rmdir")); e == nil {
		t.Fatal("directory should be deleted")
	}
	if err := dmDeletePath(dir, "never/existed"); err == nil {
		t.Fatal("deleting missing should error (FileNotFound)")
	}
}

// --- sync -------------------------------------------------------------------

func dmRemoteFile(p, sum, b64, mtime string) map[string]interface{} {
	f := map[string]interface{}{"relative_path": p}
	if sum != "" {
		f["checksum"] = sum
	}
	if b64 != "" {
		f["content_base64"] = b64
	}
	if mtime != "" {
		f["mtime"] = mtime
	}
	return f
}

func TestDMPullFiles(t *testing.T) {
	dir, _ := dmTestProject(t)
	// main.tex exists locally. Remote has main.tex (conflict, different checksum) + brand-new.txt (new).
	remote := []map[string]interface{}{
		dmRemoteFile("main.tex", "sha256:DIFFERENT", base64.StdEncoding.EncodeToString([]byte("remote")), "2026-01-01T00:00:00.000Z"),
		dmRemoteFile("brand-new.txt", "sha256:n", base64.StdEncoding.EncodeToString([]byte("newfile")), "2026-01-01T00:00:00.000Z"),
		// local sub/note.txt is NOT in remote -> would be deleted (deletion)
	}
	res, err := dmPullFiles(dir, remote, DMPullOptions{AllowEmptyRemote: true})
	if err != nil {
		t.Fatalf("pullFiles: %v", err)
	}
	if res["downloaded"].(int) != 1 {
		t.Fatalf("downloaded = %v (want 1 new file)", res["downloaded"])
	}
	if conf, _ := res["conflicts"].([]interface{}); len(conf) != 1 {
		t.Fatalf("conflicts = %v (want 1)", res["conflicts"])
	}
	if _, has := res["skipped_deletions"]; !has {
		t.Fatalf("expected skipped_deletions (deletion of sub/note.txt + main? no): %v", res)
	}
	// brand-new.txt should now exist locally
	if st, e := os.Stat(filepath.Join(dir, "brand-new.txt")); e != nil || st.Size() != 7 {
		t.Fatalf("brand-new.txt not downloaded")
	}
}

func TestDMPullFilesConfirmedDelete(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "0123456789ab")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tex"), []byte("tex"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Remote lists only note.txt (not local) -> local main.tex is the single deletion.
	remote := []map[string]interface{}{dmRemoteFile("note.txt", "sha256:x", base64.StdEncoding.EncodeToString([]byte("hello")), "")}
	res, err := dmPullFiles(dir, remote, DMPullOptions{AllowEmptyRemote: true, ConfirmRemoteDeletions: true})
	if err != nil {
		t.Fatalf("pullFiles: %v", err)
	}
	if res["deleted"].(int) != 1 {
		t.Fatalf("deleted = %v (want 1: main.tex)", res["deleted"])
	}
	if _, e := os.Stat(filepath.Join(dir, "main.tex")); e == nil {
		t.Fatal("main.tex should have been deleted")
	}
}

func TestDMPullFilesEmptyListing(t *testing.T) {
	dir, _ := dmTestProject(t)
	if _, err := dmPullFiles(dir, nil, DMPullOptions{}); err == nil {
		t.Fatal("empty remote without allowEmptyRemote should error")
	}
	if _, err := dmPullFiles(dir, nil, DMPullOptions{AllowEmptyRemote: true}); err != nil {
		t.Fatalf("empty remote with allowEmptyRemote should be ok: %v", err)
	}
}

func TestDMPushFiles(t *testing.T) {
	dir, _ := dmTestProject(t)
	// local: main.tex, sub/note.txt. remote lists only main.tex (identical checksum) ->
	// sub/note.txt is new (upload), main.tex identical (skip), remote has extra r.txt (deleted_remote).
	localMainSum := dmChecksum([]byte("\\documentclass{article}"))
	localNoteSum := dmChecksum([]byte("hello"))
	remote := []map[string]interface{}{
		dmRemoteFile("main.tex", localMainSum, "", ""),
		dmRemoteFile("r.txt", "sha256:r", "", ""),
	}
	res, err := dmPushFiles(dir, remote)
	if err != nil {
		t.Fatalf("pushFiles: %v", err)
	}
	if res["uploaded"].(int) != 1 {
		t.Fatalf("uploaded = %v (want 1: sub/note.txt)", res["uploaded"])
	}
	if res["skipped"].(int) != 1 {
		t.Fatalf("skipped = %v (want 1: main.tex identical)", res["skipped"])
	}
	if res["deleted_remote"].(int) != 1 {
		t.Fatalf("deleted_remote = %v (want 1: r.txt)", res["deleted_remote"])
	}
	_ = localNoteSum
}

func TestDMFullSync(t *testing.T) {
	dir, _ := dmTestProject(t)
	remote := []map[string]interface{}{
		dmRemoteFile("main.tex", "sha256:DIFFERENT", "", "2026-01-01T00:00:00.000Z"),
		dmRemoteFile("r.txt", "sha256:r", "", "2026-01-01T00:00:00.000Z"),
	}
	res, err := dmFullSync(dir, remote)
	if err != nil {
		t.Fatalf("fullSync: %v", err)
	}
	if res["conflicts"] == nil || res["only_local"] == nil || res["only_remote"] == nil {
		t.Fatalf("fullSync shape: %v", res)
	}
	conflicts, _ := res["conflicts"].([]interface{})
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %v (want 1)", res["conflicts"])
	}
}

// --- HTTP -------------------------------------------------------------------

func dmMux(t *testing.T, token string) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	projID := "0123456789ab"
	dir := filepath.Join(root, projID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tex"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := NewDMHandlers(DMConfig{ProjectsRoot: root, ServiceToken: token}).Mux()
	return httptest.NewServer(mux)
}

func TestDMHealth(t *testing.T) {
	srv := dmMux(t, "")
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health = %d", resp.StatusCode)
	}
}

func TestDMTreeEndpoint(t *testing.T) {
	srv := dmMux(t, "")
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/tree?project_id=0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("tree = %d (%s)", resp.StatusCode, b)
	}
}

func TestDMTreeInvalidId(t *testing.T) {
	srv := dmMux(t, "")
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/tree?project_id=../etc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("invalid id = %d (want 400)", resp.StatusCode)
	}
}

func TestDMFilePostMissingContent(t *testing.T) {
	srv := dmMux(t, "")
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/file?project_id=0123456789ab&path=x.txt", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("file post missing content = %d (want 400)", resp.StatusCode)
	}
}

func TestDMPush501(t *testing.T) {
	srv := dmMux(t, "")
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/push?project_id=0123456789ab", "application/json", strings.NewReader(`{"remote_files":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 501 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("push = %d (want 501) %s", resp.StatusCode, b)
	}
}

func TestDMAuthEnforced(t *testing.T) {
	srv := dmMux(t, "sekret")
	defer srv.Close()
	// missing token -> 401
	resp, err := http.Get(srv.URL + "/tree?project_id=0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("missing token = %d (want 401)", resp.StatusCode)
	}
	// with token -> 200
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/tree?project_id=0123456789ab", nil)
	req.Header.Set("X-Service-Token", "sekret")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Body.Close()
	if r2.StatusCode != 200 {
		t.Fatalf("with token = %d (want 200)", r2.StatusCode)
	}
}
