package filestore

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fseUUID24() string {
	return "123456789012345678901234"
}

func TestESEProjectKeyFormat(t *testing.T) {
	if got := fseProjectKeyFormat("12345"); got != "543/210/000" {
		t.Fatalf("format(12345) = %q (want 543/210/000)", got)
	}
	if got := fseProjectKeyFormat(""); got != "000/000/000" {
		t.Fatalf("format(empty) = %q (want 000/000/000)", got)
	}
}

func newFST(t *testing.T) (*FSTHandlers, string) {
	t.Helper()
	tmp := t.TempDir()
	tpl := filepath.Join(tmp, "template_files")
	pb := filepath.Join(tmp, "project-blobs")
	gb := filepath.Join(tmp, "global-blobs")
	for _, d := range []string{tpl, pb, gb, filepath.Join(tmp, "uploads")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := NewFSTHandlers(FSTConfig{
		TemplateFiles: tpl,
		ProjectBlobs:  pb,
		GlobalBlobs:   gb,
		UploadFolder:  filepath.Join(tmp, "uploads"),
	})
	return h, tmp
}

func writeAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFSEStoreRoundTrip(t *testing.T) {
	h, _ := newFST(t)
	loc := filepath.Join(t.TempDir(), "b")
	_ = loc
	_ = h
	s := &fseStore{}
	root := t.TempDir()
	loc = root
	// flattened key a/b/c -> a_b_c
	if err := s.sendStream(loc, "a/b/c.txt", bytes.NewBufferString("hello"), false, ""); err != nil {
		t.Fatalf("sendStream: %v", err)
	}
	if !s.exists(loc, "a/b/c.txt", false) {
		t.Fatal("expected flattened file to exist")
	}
	f, err := s.open(loc, "a/b/c.txt", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "hello" {
		t.Fatalf("content = %q", b)
	}
	// missing -> 404
	if _, err := s.open(loc, "nope.txt", false); err == nil || fseErrCode(err) != 404 {
		t.Fatalf("missing should be 404, got %v", err)
	}
	// md5
	if m, err := s.objectMd5(loc, "a/b/c.txt", false); err != nil || m == "" {
		t.Fatalf("md5: %v %q", err, m)
	}
	// size
	if sz, err := s.objectSize(loc, "a/b/c.txt", false); err != nil || sz != 5 {
		t.Fatalf("size = %d (%v)", sz, err)
	}
	// delete missing is a no-op
	if err := s.deleteObject(loc, "never.txt", false); err != nil {
		t.Fatalf("delete missing should no-op: %v", err)
	}
	// delete present
	if err := s.deleteObject(loc, "a/b/c.txt", false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if s.exists(loc, "a/b/c.txt", false) {
		t.Fatal("file should be deleted")
	}
}

func TestFSEStoreSubdirs(t *testing.T) {
	s := &fseStore{}
	root := t.TempDir()
	if err := s.sendStream(root, "x/y/z.bin", bytes.NewBufferString("data"), true, ""); err != nil {
		t.Fatalf("sendStream: %v", err)
	}
	if !s.exists(root, "x/y/z.bin", true) {
		t.Fatal("subdir file should exist")
	}
}

func TestFSEStoreMd5Mismatch(t *testing.T) {
	s := &fseStore{}
	root := t.TempDir()
	if err := s.sendStream(root, "f.txt", bytes.NewBufferString("abc"), false, deadbeefMD5); err == nil {
		t.Fatal("md5 mismatch should fail")
	}
}

const deadbeefMD5 = "deadbeefdeadbeefdeadbeefdeadbeef"

func TestESEWriter(t *testing.T) {
	w := &fseWriter{uploadFolder: t.TempDir()}
	p, err := w.writeStream(bytes.NewBufferString("payload"), "a/b/c")
	if err != nil {
		t.Fatalf("writeStream: %v", err)
	}
	if !strings.HasSuffix(p, filepath.Join("a-b-c")) {
		t.Fatalf("expected path ending a-b-c, got %q", p)
	}
	if err := w.deleteFile(p); err != nil {
		t.Fatalf("deleteFile: %v", err)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("file should be removed")
	}
}

func TestFSEConverterDisabled(t *testing.T) {
	c := &fseConverter{converter: "imagemagick", enable: false}
	if _, err := c.convert("/tmp/x", "png"); err == nil || fseErrCode(err) != 500 {
		t.Fatalf("disabled conversion should error, got %v", err)
	}
}

func TestFSEConverterInvalidFormat(t *testing.T) {
	c := &fseConverter{converter: "imagemagick", enable: true}
	if _, err := c.convert("/tmp/x", "gif"); err == nil {
		t.Fatal("invalid format should error")
	}
}

func TestFSEHandlerInsertInvalidKey(t *testing.T) {
	h, _ := newFST(t)
	if err := h.Handler.insertFile("loc", "badkey", bytes.NewBufferString("x"), false); err == nil {
		t.Fatal("invalid key should error")
	}
	// valid template key: <objectId>/v/<int>/<fmt>
	tkey := fseUUID24() + "/v/1/png"
	loc := filepath.Join(t.TempDir(), "t")
	if err := h.Handler.insertFile(loc, tkey, bytes.NewBufferString("pngdata"), false); err != nil {
		t.Fatalf("insert valid: %v", err)
	}
}

func TestFSEHandlerDelete(t *testing.T) {
	h, _ := newFST(t)
	loc := t.TempDir()
	tkey := fseUUID24() + "/v/1/png"
	if err := h.Handler.insertFile(loc, tkey, bytes.NewBufferString("x"), false); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := h.Handler.deleteFile(loc, tkey, false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// invalid delete key
	if err := h.Handler.deleteFile(loc, "not-a-key", false); err == nil {
		t.Fatal("invalid delete key should error")
	}
}

func TestFSEParseRange(t *testing.T) {
	if r := fseParseRange(100, "bytes=0-4"); r == nil || r.start != 0 || r.end != 4 {
		t.Fatalf("range 0-4 = %+v", r)
	}
	if r := fseParseRange(100, "bytes=10-"); r == nil || r.start != 10 || r.end != 99 {
		t.Fatalf("range 10- = %+v", r)
	}
	if r := fseParseRange(100, "bytes=-5"); r == nil || r.start != 95 || r.end != 99 {
		t.Fatalf("range -5 = %+v", r)
	}
	if r := fseParseRange(100, "bytes=500-999"); r != nil {
		t.Fatal("start >= size should be nil")
	}
	if r := fseParseRange(100, "items=0-4"); r != nil {
		t.Fatal("non-bytes should be nil")
	}
}

// --- HTTP ------------------------------------------------------------------

func TestFSTHealthAndStatus(t *testing.T) {
	h, _ := newFST(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/health_check")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("health = %d", resp.StatusCode)
	}
	resp2, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != 200 || string(b) != "filestore is up" {
		t.Fatalf("status = %d %q", resp2.StatusCode, b)
	}
}

func TestFSTTemplateCRUD(t *testing.T) {
	h, _ := newFST(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	tid := fseUUID24()
	base := srv.URL + "/template/" + tid + "/v/1/png"

	// initial GET -> 404
	resp, _ := http.Get(base)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("initial get = %d (want 404)", resp.StatusCode)
	}

	// POST insert -> 200
	resp, err := http.Post(base, "application/octet-stream", strings.NewReader("PNGDATA"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("insert = %d (want 200)", resp.StatusCode)
	}

	// GET -> 200 with body
	resp, _ = http.Get(base)
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "PNGDATA" {
		t.Fatalf("get = %d %q", resp.StatusCode, b)
	}

	// HEAD -> 200 + Content-Length 7
	req, _ := http.NewRequest(http.MethodHead, base, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Length") != "7" {
		t.Fatalf("head = %d cl=%q (want 200/7)", resp.StatusCode, resp.Header.Get("Content-Length"))
	}

	// invalid template id -> 400
	if resp, err := http.Get(srv.URL + "/template/badid/v/1/png"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("bad id = %d (want 400)", resp.StatusCode)
		}
	}

	// DELETE -> 204
	req, _ = http.NewRequest(http.MethodDelete, base, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("delete = %d (want 204)", resp.StatusCode)
	}
}

func TestFSTGlobalBlob(t *testing.T) {
	h, tmp := newFST(t)
	hash := "abcdef1234567890abcdef"
	// key = hash[0:2]/hash[2:4]/hash[4:] with subdirs
	writeAt(t, filepath.Join(h.Cfg.GlobalBlobs, "ab", "cd", "ef1234567890abcdef"), "BLOB")
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/history/global/hash/" + hash)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "BLOB" {
		t.Fatalf("global blob = %d %q", resp.StatusCode, b)
	}
	// missing hash -> 404
	if resp, err := http.Get(srv.URL + "/history/global/hash/ffffffffffffffffffffffff"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("missing blob = %d (want 404)", resp.StatusCode)
		}
	}
	_ = tmp
}

func TestFSTProjectBlob(t *testing.T) {
	h, _ := newFST(t)
	hash := "abcdef1234567890"
	// key = projectKeyFormat(12345)/hash[0:2]/hash[2:]
	key := fseProjectKeyFormat("12345") + "/ab/" + hash[2:]
	writeAt(t, filepath.Join(h.Cfg.ProjectBlobs, strings.ReplaceAll(key, "_", "")), "PB")
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/history/project/12345/hash/" + hash)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "PB" {
		t.Fatalf("project blob = %d %q (key=%q)", resp.StatusCode, b, key)
	}
}

func TestFSTBucketRoute(t *testing.T) {
	h, tmp := newFST(t)
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Chdir(tmp)
	defer os.Chdir(prev)
	// CWD-relative: bucket "b1", key "f.txt" -> b1/f.txt
	writeAt(t, filepath.Join("b1", "f.txt"), "BUCK")
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/bucket/b1/key/f.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "BUCK" {
		t.Fatalf("bucket route = %d %q", resp.StatusCode, b)
	}
	// bad bucket -> 400
	if resp, err := http.Get(srv.URL + "/bucket/BADBUCKET/key/f.txt"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("bad bucket = %d (want 400)", resp.StatusCode)
		}
	}
}

func TestFSTRangeRequest(t *testing.T) {
	h, tmp := newFST(t)
	prev, _ := os.Getwd()
	_ = os.Chdir(tmp)
	defer os.Chdir(prev)
	writeAt(t, filepath.Join("b1", "data.txt"), "0123456789")
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/bucket/b1/key/data.txt", nil)
	req.Header.Set("Range", "bytes=2-5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "2345" {
		t.Fatalf("range = %d %q (want 200/2345)", resp.StatusCode, b)
	}
}

func TestFSTCacheWarm(t *testing.T) {
	h, tmp := newFST(t)
	prev, _ := os.Getwd()
	_ = os.Chdir(tmp)
	defer os.Chdir(prev)
	writeAt(t, filepath.Join("b1", "cw.txt"), "SOME-CONTENT-123")
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/bucket/b1/key/cw.txt?cacheWarm=true")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	// cacheWarm: 200 but body not streamed
	if resp.StatusCode != 200 {
		t.Fatalf("cacheWarm = %d (want 200)", resp.StatusCode)
	}
	_ = b
}
