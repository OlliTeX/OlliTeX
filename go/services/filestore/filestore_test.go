package filestore

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

// --- fake S3 gateway (path-style; satisfies the s3x.Client wire surface) ---

type fakeS3 struct {
	mu      sync.Mutex
	buckets map[string]map[string][]byte
}

func newFakeS3() *fakeS3 { return &fakeS3{buckets: map[string]map[string][]byte{}} }

func (f *fakeS3) bucket(name string) (map[string][]byte, bool) {
	b, ok := f.buckets[name]
	return b, ok
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seg := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	bucket := ""
	if len(seg) > 0 && seg[0] != "" {
		bucket = seg[0]
	}
	keySegs := seg[1:]
	key := strings.Join(keySegs, "/")
	nosuchBucket := func() {
		w.Header().Set("Content-Type", "application/xml")
		http.Error(w, `<Error><Code>NoSuchBucket</Code></Error>`, http.StatusNotFound)
	}
	nosuchKey := func() {
		w.Header().Set("Content-Type", "application/xml")
		http.Error(w, `<Error><Code>NoSuchKey</Code></Error>`, http.StatusNotFound)
	}
	b, hasBucket := f.bucket(bucket)
	switch {
	case r.Method == http.MethodPut && key == "":
		if _, ok := f.buckets[bucket]; !ok {
			f.buckets[bucket] = map[string][]byte{}
		}
		w.WriteHeader(http.StatusOK)
	case r.URL.Query().Get("prefix") != "" || (r.Method == http.MethodGet && key == ""):
		if !hasBucket {
			nosuchBucket()
			return
		}
		prefix := r.URL.Query().Get("prefix")
		keys := make([]string, 0, len(b))
		for k := range b {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		var sb strings.Builder
		sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
		sb.WriteString(`<ListBucketResult><IsTruncated>false</IsTruncated>`)
		for _, k := range keys {
			etag := md5.Sum(b[k])
			fmt.Fprintf(&sb, `<Contents><Key>%s</Key><Size>%d</Size><ETag>%q</ETag><LastModified>2026-01-01T00:00:00.000Z</LastModified></Contents>`, k, len(b[k]), hex.EncodeToString(etag[:]))
		}
		sb.WriteString(`</ListBucketResult>`)
		w.Header().Set("Content-Type", "application/xml")
		io.Copy(w, strings.NewReader(sb.String()))
	case r.Method == http.MethodPut && len(keySegs) > 0:
		if !hasBucket {
			// SeaweedFS-parity: S3 gateway auto-creates the bucket on first
			// object PUT (the e2e gateway behaves this way).
			f.buckets[bucket] = map[string][]byte{}
			b = f.buckets[bucket]
			hasBucket = true
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Content-MD5"); got != "" {
			// SeaweedFS-parity: enforce the digest (BadDigest on mismatch).
			raw, derr := base64.StdEncoding.DecodeString(got)
			sum := md5.Sum(body)
			if derr != nil || !bytes.Equal(raw, sum[:]) {
				w.Header().Set("Content-Type", "application/xml")
				http.Error(w, `<Error><Code>BadDigest</Code></Error>`, http.StatusBadRequest)
				return
			}
		}
		b[key] = body
		etag := md5.Sum(body)
		w.Header().Set("ETag", `"`+hex.EncodeToString(etag[:])+`"`)
		w.WriteHeader(http.StatusOK)
	case len(keySegs) == 0:
		if !hasBucket {
			nosuchBucket()
			return
		}
		w.WriteHeader(http.StatusOK) // HEAD/GET bucket exists
	default:
		if !hasBucket {
			nosuchBucket()
			return
		}
		obj, ok := b[key]
		if !ok {
			nosuchKey()
			return
		}
		etag := md5.Sum(obj)
		w.Header().Set("ETag", `"`+hex.EncodeToString(etag[:])+`"`)
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", fmt.Sprint(len(obj)))
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			delete(b, key)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Content-Length", fmt.Sprint(len(obj)))
			w.WriteHeader(http.StatusOK)
			w.Write(obj)
		}
	}
}

func TestG2BackendRetired(t *testing.T) {
	// G2 (STOR-1): fs backend is gone; '' and 'fs' must fail fast with a clear error.
	for _, backend := range []string{"", "fs"} {
		_, err := NewFSTHandlers(FSTConfig{Backend: backend})
		if err == nil {
			t.Fatalf("BACKEND=%q: expected retired-backend error", backend)
		}
		if !strings.Contains(err.Error(), "retired") {
			t.Fatalf("BACKEND=%q: error should mention retirement, got %v", backend, err)
		}
	}
	// s3 with an unreachable gateway must fail at startup (not 5xx first request).
	if _, err := NewFSTHandlers(FSTConfig{Backend: "s3", S3Endpoint: "http://127.0.0.1:1"}); err == nil {
		t.Fatal("s3 backend with dead endpoint should fail fast")
	}
}

func newFST(t *testing.T) (*FSTHandlers, *fakeS3) {
	t.Helper()
	f := newFakeS3()
	ts := httptest.NewServer(f)
	t.Cleanup(ts.Close)
	h, err := NewFSTHandlers(FSTConfig{
		Backend:       "s3",
		S3Endpoint:    ts.URL,
		TemplateFiles: "templatefiles",
		ProjectBlobs:  "projectblobs",
		GlobalBlobs:   "globalblobs",
		UploadFolder:  t.TempDir() + "/uploads",
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, f
}

func putObj(t *testing.T, h *FSTHandlers, bucket, key, data string) {
	t.Helper()
	if err := h.Store.sendStream(bucket, key, strings.NewReader(data), false, ""); err != nil {
		t.Fatalf("seed %s/%s: %v", bucket, key, err)
	}
}

func TestS3StoreRoundTrip(t *testing.T) {
	h, _ := newFST(t)
	// keys are stored verbatim (S3 semantics; no fs flattening)
	if err := h.Store.sendStream("templatefiles", "a/b/c.txt", strings.NewReader("hello"), false, ""); err != nil {
		t.Fatalf("sendStream: %v", err)
	}
	if !h.Store.exists("templatefiles", "a/b/c.txt", false) {
		t.Fatal("expected object to exist")
	}
	f, err := h.Store.open("templatefiles", "a/b/c.txt", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "hello" {
		t.Fatalf("content = %q", b)
	}
	// missing -> 404
	if _, err := h.Store.open("templatefiles", "nope.txt", false); err == nil || fseErrCode(err) != 404 {
		t.Fatalf("missing should be 404, got %v", err)
	}
	// md5 + size
	if m, err := h.Store.objectMd5("templatefiles", "a/b/c.txt", false); err != nil || m == "" {
		t.Fatalf("md5: %v %q", err, m)
	}
	if sz, err := h.Store.objectSize("templatefiles", "a/b/c.txt", false); err != nil || sz != 5 {
		t.Fatalf("size = %d (%v)", sz, err)
	}
	// delete missing is a no-op; delete present works
	if err := h.Store.deleteObject("templatefiles", "never.txt", false); err != nil {
		t.Fatalf("delete missing should no-op: %v", err)
	}
	if err := h.Store.deleteObject("templatefiles", "a/b/c.txt", false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if h.Store.exists("templatefiles", "a/b/c.txt", false) {
		t.Fatal("object should be deleted")
	}
}

func TestS3StoreMd5Mismatch(t *testing.T) {
	h, _ := newFST(t)
	if err := h.Store.sendStream("templatefiles", "f.txt", strings.NewReader("abc"), false, deadbeefMD5); err == nil {
		t.Fatal("md5 mismatch should fail (gateway BadDigest)")
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
	if err := h.Handler.insertFile("templatefiles", "badkey", bytes.NewBufferString("x"), false); err == nil {
		t.Fatal("invalid key should error")
	}
	// valid template key: <objectId>/v/<int>/<fmt>
	tkey := fseUUID24() + "/v/1/png"
	if err := h.Handler.insertFile("templatefiles", tkey, bytes.NewBufferString("pngdata"), false); err != nil {
		t.Fatalf("insert valid: %v", err)
	}
}

func TestFSEHandlerDelete(t *testing.T) {
	h, _ := newFST(t)
	tkey := fseUUID24() + "/v/1/png"
	if err := h.Handler.insertFile("templatefiles", tkey, bytes.NewBufferString("x"), false); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := h.Handler.deleteFile("templatefiles", tkey, false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// invalid delete key
	if err := h.Handler.deleteFile("templatefiles", "not-a-key", false); err == nil {
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
	h, _ := newFST(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	hash := "abcdef1234567890abcdef"
	// key = hash[0:2]/hash[2:4]/hash[4:] (verbatim in S3)
	putObj(t, h, "globalblobs", "ab/cd/ef1234567890abcdef", "BLOB")
	srvURL := srv.URL
	resp, err := http.Get(srvURL + "/history/global/hash/" + hash)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "BLOB" {
		t.Fatalf("global blob = %d %q", resp.StatusCode, b)
	}
	// missing hash -> 404
	if resp, err := http.Get(srvURL + "/history/global/hash/ffffffffffffffffffffffff"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("missing blob = %d (want 404)", resp.StatusCode)
		}
	}
}

func TestFSTProjectBlob(t *testing.T) {
	h, _ := newFST(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	hash := "abcdef1234567890"
	// key = projectKeyFormat(12345)/hash[0:2]/hash[2:]
	key := fseProjectKeyFormat("12345") + "/ab/" + hash[2:]
	putObj(t, h, "projectblobs", key, "PB")
	srvURL := srv.URL
	resp, err := http.Get(srvURL + "/history/project/12345/hash/" + hash)
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
	h, _ := newFST(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	// bucket "b1", key "f.txt" (S3: bucket name + verbatim key)
	putObj(t, h, "b1", "f.txt", "BUCK")
	srvURL := srv.URL
	resp, err := http.Get(srvURL + "/bucket/b1/key/f.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(b) != "BUCK" {
		t.Fatalf("bucket route = %d %q", resp.StatusCode, b)
	}
	// bad bucket -> 400
	if resp, err := http.Get(srvURL + "/bucket/BADBUCKET/key/f.txt"); err == nil {
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("bad bucket = %d (want 400)", resp.StatusCode)
		}
	}
}

func TestFSTRangeRequest(t *testing.T) {
	h, _ := newFST(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	putObj(t, h, "b1", "data.txt", "0123456789")
	srvURL := srv.URL
	req, _ := http.NewRequest(http.MethodGet, srvURL+"/bucket/b1/key/data.txt", nil)
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
	h, _ := newFST(t)
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	putObj(t, h, "b1", "cw.txt", "SOME-CONTENT-123")
	srvURL := srv.URL
	resp, err := http.Get(srvURL + "/bucket/b1/key/cw.txt?cacheWarm=true")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// cacheWarm: 200 but body not streamed
	if resp.StatusCode != 200 {
		t.Fatalf("cacheWarm = %d (want 200)", resp.StatusCode)
	}
}
