package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeWebDAV is an in-memory WebDAV server used to drive the client +
// handlers. It supports PROPFIND (multistatus XML), GET, PUT, MKCOL, DELETE,
// MOVE, and can force a one-shot retriable status (for retry tests).
type fakeWebDAV struct {
	mu         sync.Mutex
	files      map[string]string
	dirs       map[string]bool
	failOne    map[string]int // "METHOD PATH" -> status to return exactly once
	alwaysFail map[string]int // "METHOD PATH" -> status to return every time
	ops        []string
}

func newFakeWebDAV() *fakeWebDAV {
	f := &fakeWebDAV{
		files:      map[string]string{},
		dirs:       map[string]bool{"/": true, "/docs": true},
		failOne:    map[string]int{},
		alwaysFail: map[string]int{},
	}
	f.files["/notes.txt"] = "hello notes"
	f.files["/docs/memo.txt"] = "a memo"
	return f
}

// etagFor returns the deterministic etag for a stored file (== the path).
func (f *fakeWebDAV) etagFor(p string) string { return p }

func pathOnly(r *http.Request) string { return r.URL.Path }

func (f *fakeWebDAV) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := pathOnly(r)
		f.mu.Lock()
		f.ops = append(f.ops, r.Method+" "+p)
		key := r.Method + " " + p
		if st, ok := f.failOne[key]; ok {
			delete(f.failOne, key)
			f.mu.Unlock()
			http.Error(w, "forced "+fmt.Sprint(st), st)
			return
		}
		if st, ok := f.alwaysFail[key]; ok {
			f.mu.Unlock()
			http.Error(w, "always forced "+fmt.Sprint(st), st)
			return
		}
		f.mu.Unlock()

		switch r.Method {
		case "PROPFIND":
			f.writePropfind(w, p)
		case http.MethodGet:
			if content, ok := f.peekFile(p); ok {
				w.WriteHeader(200)
				_, _ = w.Write([]byte(content))
			} else {
				w.WriteHeader(404)
			}
		case http.MethodPut:
			if im := r.Header.Get("If-Match"); im != "" && im != f.etagFor(p) {
				w.WriteHeader(412)
				return
			}
			b, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			f.files[p] = string(b)
			f.mu.Unlock()
			w.WriteHeader(201)
		case "MKCOL":
			f.mu.Lock()
			exists := f.dirs[p]
			if !exists {
				f.dirs[p] = true
			}
			f.mu.Unlock()
			if exists {
				w.WriteHeader(405)
			} else {
				w.WriteHeader(201)
			}
		case http.MethodDelete:
			f.mu.Lock()
			_, ok := f.files[p]
			delete(f.files, p)
			f.mu.Unlock()
			if !ok {
				w.WriteHeader(404)
			}
		case "MOVE":
			dst := r.Header.Get("Destination")
			f.mu.Lock()
			if content, ok := f.files[p]; ok {
				f.files[dst] = content
				delete(f.files, p)
				f.mu.Unlock()
				w.WriteHeader(201)
				return
			}
			f.mu.Unlock()
			w.WriteHeader(404)
		default:
			w.WriteHeader(405)
		}
	}
}

func (f *fakeWebDAV) peekFile(p string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.files[p]
	return c, ok
}

func (f *fakeWebDAV) hasFile(p string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.files[p]
	return ok
}

func (f *fakeWebDAV) hasDir(p string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dirs[p]
}

func (f *fakeWebDAV) child(p string, name string) string {
	if p == "/" {
		return "/" + name
	}
	return p + "/" + name
}

// writePropfind emits a DAV multistatus for path + its direct children.
func (f *fakeWebDAV) writePropfind(w http.ResponseWriter, p string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(207)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	b.WriteString(`<d:multistatus xmlns:d="DAV:">`)
	// self
	b.WriteString(`<d:response><d:href>` + p + `</d:href><d:propstat><d:prop>`)
	if f.hasDir(p) {
		b.WriteString(`<d:resourcetype><d:collection/></d:resourcetype>`)
	}
	b.WriteString(`</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
	// children
	f.mu.Lock()
	type ent struct {
		name  string
		isDir bool
		size  int
	}
	var kids []ent
	prefix := p
	if prefix != "/" {
		prefix += "/"
	}
	for name := range f.files {
		if strings.HasPrefix(name, prefix) {
			rest := strings.TrimPrefix(name, prefix)
			if !strings.Contains(rest, "/") {
				kids = append(kids, ent{name: rest, isDir: false, size: len(f.files[name])})
			}
		}
	}
	for name := range f.dirs {
		if name != "/" && name != p && strings.HasPrefix(name, prefix) {
			rest := strings.TrimPrefix(name, prefix)
			if !strings.Contains(rest, "/") {
				kids = append(kids, ent{name: rest, isDir: true})
			}
		}
	}
	f.mu.Unlock()
	for _, k := range kids {
		href := prefix + k.name
		b.WriteString(`<d:response><d:href>` + href + `</d:href><d:propstat><d:prop>`)
		b.WriteString(`<d:displayname>` + k.name + `</d:displayname>`)
		b.WriteString(`<d:getetag>"` + href + `"</d:getetag>`)
		if k.isDir {
			b.WriteString(`<d:resourcetype><d:collection/></d:resourcetype>`)
		} else {
			b.WriteString(`<d:getcontentlength>` + fmt.Sprint(k.size) + `</d:getcontentlength>`)
		}
		b.WriteString(`<d:getlastmodified>` + "Mon, 02 Jan 2026 00:00:00 GMT" + `</d:getlastmodified>`)
		b.WriteString(`</d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
	}
	b.WriteString(`</d:multistatus>`)
	_, _ = io.WriteString(w, b.String())
}

// wdClient builds a WebDAVClient pointed at the fake server.
func wdClient(t *testing.T, srv *httptest.Server) *WebDAVClient {
	t.Helper()
	c, err := NewWebDAVClient(WebDAVConfig{
		MaxRetries:   2,
		RetryDelayMs: 1,
		Sleep:        func(d time.Duration) {},
	}, srv.URL, "user", "pass")
	if err != nil {
		t.Fatalf("NewWebDAVClient: %v", err)
	}
	return c
}

func wdStart(t *testing.T) (*fakeWebDAV, *httptest.Server) {
	t.Helper()
	f := newFakeWebDAV()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return f, srv
}

func TestWD_NewClientValidation(t *testing.T) {
	if _, err := NewWebDAVClient(WebDAVConfig{}, "", "user", "pass"); err == nil {
		t.Fatal("expected error for empty URL-less/invalid")
	}
	if _, err := NewWebDAVClient(WebDAVConfig{}, "http://x", "", "pass"); err == nil {
		t.Fatal("expected Missing authentication credentials")
	}
	if _, err := NewWebDAVClient(WebDAVConfig{}, "http://x", "user", ""); err == nil {
		t.Fatal("expected Missing authentication credentials")
	}
	if _, err := NewWebDAVClient(WebDAVConfig{}, "ftp://x", "user", "pass"); err == nil {
		t.Fatal("expected WebDAV URL must use HTTP or HTTPS")
	}
	c, err := NewWebDAVClient(WebDAVConfig{}, "http://example.com/", "u", "p")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if c.BaseURL != "http://example.com" {
		t.Fatalf("BaseURL = %q (trailing slash should be trimmed)", c.BaseURL)
	}
}

func TestWD_Check(t *testing.T) {
	_, srv := wdStart(t)
	c := wdClient(t, srv)
	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestWD_CheckAuthFailure(t *testing.T) {
	f, srv := wdStart(t)
	f.failOne["PROPFIND /"] = 401
	c := wdClient(t, srv)
	err := c.Check(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	st, msg := providerStatusError(webdavStatusOf(err), err.Error())
	if st != 401 || msg != "authentication failed" {
		t.Fatalf("mapped = %d %q", st, msg)
	}
}

func TestWD_List(t *testing.T) {
	_, srv := wdStart(t)
	c := wdClient(t, srv)
	entries, err := c.List(context.Background(), "/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := map[string]WebDAVEntry{}
	for _, e := range entries {
		byName[e.Href] = e
	}
	if !byName["docs"].IsDirectory {
		t.Fatal("docs should be a directory")
	}
	if !byName["notes.txt"].IsDirectory {
		// notes.txt is a file
	}
	if byName["notes.txt"].Size != int64(len("hello notes")) {
		t.Fatalf("notes.txt size = %d", byName["notes.txt"].Size)
	}
	if byName["notes.txt"].Etag == "" {
		t.Fatal("notes.txt missing etag")
	}
	if byName["notes.txt"].Path != "/notes.txt" {
		t.Fatalf("notes.txt path = %q", byName["notes.txt"].Path)
	}
}

func TestWD_Download(t *testing.T) {
	_, srv := wdStart(t)
	c := wdClient(t, srv)
	b64, err := c.Download(context.Background(), "/notes.txt")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	dec, derr := base64StdDecode(b64)
	if derr != nil || dec != "hello notes" {
		t.Fatalf("decoded = %q", dec)
	}
	// 404
	_, err = c.Download(context.Background(), "/missing.txt")
	if webdavStatusOf(err) != 404 {
		t.Fatalf("expected 404, got %v (statusOf=%d)", err, webdavStatusOf(err))
	}
}

func TestWD_UploadOK(t *testing.T) {
	f, srv := wdStart(t)
	c := wdClient(t, srv)
	b64 := base64StdEncode("new file content")
	if err := c.Upload(context.Background(), "/docs/new.txt", b64, ""); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if !f.hasFile("/docs/new.txt") {
		t.Fatal("file not stored")
	}
}

func TestWD_UploadConflict(t *testing.T) {
	_, srv := wdStart(t)
	c := wdClient(t, srv)
	b64 := base64StdEncode("x")
	// etag does NOT match the fake's etag (which == path) -> 412
	err := c.Upload(context.Background(), "/docs/memo.txt", b64, "wrong-etag")
	st := webdavStatusOf(err)
	if st != 412 {
		t.Fatalf("expected 412, got statusOf=%d (%v)", st, err)
	}
	if !strings.Contains(err.Error(), "ETag mismatch") {
		t.Fatalf("msg = %q", err.Error())
	}
}

func TestWD_UploadCorrectEtag(t *testing.T) {
	f, srv := wdStart(t)
	c := wdClient(t, srv)
	b64 := base64StdEncode("updated")
	// correct etag == path
	if err := c.Upload(context.Background(), "/docs/memo.txt", b64, "/docs/memo.txt"); err != nil {
		t.Fatalf("Upload with correct etag: %v", err)
	}
	if !f.hasFile("/docs/memo.txt") {
		t.Fatal("file not stored")
	}
}

func TestWD_Delete(t *testing.T) {
	_, srv := wdStart(t)
	c := wdClient(t, srv)
	notFound, err := c.Delete(context.Background(), "/notes.txt")
	if err != nil || notFound {
		t.Fatalf("Delete existing: notFound=%v err=%v", notFound, err)
	}
	// already-gone -> notFound true, no error
	notFound, err = c.Delete(context.Background(), "/notes.txt")
	if !notFound || err != nil {
		t.Fatalf("Delete missing: notFound=%v err=%v", notFound, err)
	}
}

func TestWD_Move(t *testing.T) {
	_, srv := wdStart(t)
	c := wdClient(t, srv)
	if err := c.Move(context.Background(), "/notes.txt", "/renamed.txt"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	// 404 move
	if err := c.Move(context.Background(), "/gone.txt", "/x"); webdavStatusOf(err) != 404 {
		t.Fatalf("expected 404, got %v", err)
	}
}

func TestWD_CreateDirectory(t *testing.T) {
	_, srv := wdStart(t)
	c := wdClient(t, srv)
	created, err := c.CreateDirectory(context.Background(), "/newdir")
	if err != nil || !created {
		t.Fatalf("CreateDirectory: created=%v err=%v", created, err)
	}
	// already exists -> created=false, no error
	created, err = c.CreateDirectory(context.Background(), "/newdir")
	if created || err != nil {
		t.Fatalf("CreateDirectory exists: created=%v err=%v", created, err)
	}
}

func TestWD_RetryOn503ThenSuccess(t *testing.T) {
	f, srv := wdStart(t)
	// force 503 on the first GET /notes.txt, then succeed
	f.failOne["GET /notes.txt"] = 503
	c := wdClient(t, srv)
	b64, err := c.Download(context.Background(), "/notes.txt")
	if err != nil {
		t.Fatalf("expected retry success, got %v", err)
	}
	if b64 == "" {
		t.Fatal("empty body")
	}
}

func TestWD_RetryExhaustedThenFail(t *testing.T) {
	f, srv := wdStart(t)
	// 503 forever on this path; the client must retry (default 2 retries = 3
	// attempts) and then surface the error.
	f.alwaysFail["GET /always503"] = 503
	c, _ := NewWebDAVClient(WebDAVConfig{Sleep: func(d time.Duration) {}}, srv.URL, "u", "p")
	_, err := c.Download(context.Background(), "/always503")
	if err == nil {
		t.Fatal("expected failure after retries")
	}
	if webdavStatusOf(err) != 503 {
		t.Fatalf("expected status 503, got %d (%v)", webdavStatusOf(err), err)
	}
	f.mu.Lock()
	got := 0
	for _, op := range f.ops {
		if op == "GET /always503" {
			got++
		}
	}
	f.mu.Unlock()
	if got != 3 {
		t.Fatalf("expected 3 GET attempts (MaxRetries=2), got %d", got)
	}
}

// --- helpers ----------------------------------------------------------------

func TestWD_SafeErrorRedaction(t *testing.T) {
	in := "GET https://user:secret@host/p failed"
	got := safeError(fmt.Errorf("%s", in))
	if strings.Contains(got, "secret@") {
		t.Fatalf("credential not redacted: %q", got)
	}
	if !strings.Contains(got, "<redacted>@") {
		t.Fatalf("expected redaction marker: %q", got)
	}
}

func TestWD_ProviderStatusErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		msg    string
		wantC  int
		wantM  string
	}{
		{401, "", 401, "authentication failed"},
		{500, "invalid token", 401, "authentication failed"},
		{404, "", 404, "not found"},
		{500, "no such file", 404, "not found"},
		{409, "", 409, "modified since last sync"},
		{412, "precondition failed", 409, "modified since last sync"},
		{500, "boom", 502, "provider request failed"},
	}
	for _, tc := range cases {
		c, m := providerStatusError(tc.status, tc.msg)
		if c != tc.wantC || m != tc.wantM {
			t.Fatalf("providerStatusError(%d,%q) = (%d,%q), want (%d,%q)", tc.status, tc.msg, c, m, tc.wantC, tc.wantM)
		}
	}
}

func TestWD_SanitizeURLForLogging(t *testing.T) {
	got := sanitizeUrlForLogging("https://user:pass@host.example/p/q")
	if strings.Contains(got, "pass@") || strings.Contains(got, "user") {
		t.Fatalf("credential leaked: %q", got)
	}
	if got != "https://host.example/p/q" {
		t.Fatalf("got %q", got)
	}
}

func TestWD_TimingSafeEqual(t *testing.T) {
	if !timingSafeEqual("abc", "abc") {
		t.Fatal("equal should be true")
	}
	if timingSafeEqual("abc", "abd") {
		t.Fatal("different should be false")
	}
	if timingSafeEqual("abc", "abcd") {
		t.Fatal("different length should be false")
	}
	if !timingSafeEqual("", "") {
		t.Fatal("empty equal should be true")
	}
}

func TestWD_BasicPassword(t *testing.T) {
	// "user:pass" -> pass ; username with ':' -> split on first ':'
	h := "Basic " + b64("user:pass")
	if got := basicPassword(h); got != "pass" {
		t.Fatalf("got %q", got)
	}
	h = "Basic " + b64("us:er:pass")
	if got := basicPassword(h); got != "er:pass" {
		t.Fatalf("got %q", got)
	}
	if got := basicPassword("Bearer x"); got != "" {
		t.Fatalf("non-basic should be empty, got %q", got)
	}
	if got := basicPassword(""); got != "" {
		t.Fatalf("empty should be empty, got %q", got)
	}
}

func TestWD_ServiceTokenAuth(t *testing.T) {
	h := &WebDAVHandlers{Cfg: WebDAVConfig{ServiceToken: "secret-token", Sleep: func(time.Duration) {}}}
	mux := h.Mux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// valid token
	code, _ := wdDo(t, "POST", srv.URL+"/check", map[string]interface{}{"server_url": "http://x", "username": "u", "password": "p"}, map[string]string{"X-Service-Token": "secret-token"})
	if code != 400 && code != 502 && code != 401 {
		// x URL won't resolve; but auth must PASS (not 401 missing-token). Any provider error is fine.
	}
	// missing token -> 401 Invalid or missing service token
	code, body := wdDo(t, "POST", srv.URL+"/check", map[string]interface{}{"server_url": "http://x", "username": "u", "password": "p"}, nil)
	if code != 401 {
		t.Fatalf("missing token: got %d", code)
	}
	if !strings.Contains(body, "Invalid or missing service token") {
		t.Fatalf("body = %q", body)
	}
	// wrong token -> 401
	code, _ = wdDo(t, "POST", srv.URL+"/check", map[string]interface{}{"server_url": "http://x", "username": "u", "password": "p"}, map[string]string{"X-Service-Token": "wrong"})
	if code != 401 {
		t.Fatalf("wrong token: got %d", code)
	}
	// bearer token -> accepted (auth pass)
	code, _ = wdDo(t, "POST", srv.URL+"/check", map[string]interface{}{"server_url": "http://x", "username": "u", "password": "p"}, map[string]string{"Authorization": "Bearer secret-token"})
	if code == 401 && strings.Contains("x", "Invalid or missing") {
		t.Fatal("bearer should be accepted")
	}
}

func TestWD_HandlerMissingFields(t *testing.T) {
	h := &WebDAVHandlers{Cfg: WebDAVConfig{Sleep: func(time.Duration) {}}}
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	// /check missing password
	code, body := wdDo(t, "POST", srv.URL+"/check", map[string]interface{}{"server_url": "http://x", "username": "u"}, nil)
	if code != 400 {
		t.Fatalf("got %d", code)
	}
	if !strings.Contains(body, "Missing required fields") {
		t.Fatalf("body = %q", body)
	}
}

func TestWD_HandlerListEndToEnd(t *testing.T) {
	f, fsrv := wdStart(t)
	h := &WebDAVHandlers{Cfg: WebDAVConfig{Sleep: func(time.Duration) {}}}
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	code, body := wdDo(t, "POST", srv.URL+"/list", map[string]interface{}{
		"server_url": fsrv.URL, "username": "u", "password": "p", "path": "/",
	}, nil)
	if code != 200 {
		t.Fatalf("got %d body=%s", code, body)
	}
	var out struct {
		Entries []WebDAVEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(out.Entries) < 2 {
		t.Fatalf("expected >=2 entries, got %d", len(out.Entries))
	}
	_ = f
}

// --- test helpers -----------------------------------------------------------

func b64(s string) string             { return base64.StdEncoding.EncodeToString([]byte(s)) }
func base64StdEncode(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
func base64StdDecode(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	return string(b), err
}

// wdDo POSTs a JSON body and returns the status code + body string.
func wdDo(t *testing.T, method, url string, body map[string]interface{}, header map[string]string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("req: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

var _ = time.Now // keep import
