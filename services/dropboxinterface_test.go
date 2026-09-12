package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeDropbox mimics the Dropbox API v2 endpoints used by the client.
type fakeDropbox struct {
	mu         sync.Mutex
	goodToken  string
	srv        *httptest.Server
	uploads    map[string]string // arg -> body
	listCursor int               // remaining pages for list_folder
}

func newFakeDropbox() *fakeDropbox {
	return &fakeDropbox{
		goodToken: "sl.goodtoken",
		uploads:   map[string]string{},
	}
}

func (f *fakeDropbox) authorized(r *http.Request) bool {
	return r.Header.Get("Authorization") == "Bearer "+f.goodToken
}

func (f *fakeDropbox) jsonError(w http.ResponseWriter, code int, errObj string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":` + errObj + `}`))
}

// bodyPath unwraps a simple JSON {"path":"..."} (or from/to) for fake routing.
func bodyPath(r *http.Request) string {
	b, _ := io.ReadAll(r.Body)
	var x struct {
		Path string `json:"path"`
		From string `json:"from_path"`
		To   string `json:"to_path"`
	}
	_ = json.Unmarshal(b, &x)
	return x.Path + x.From + x.To
}

func (f *fakeDropbox) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		switch p {
		case "users/get_current_account":
			if !f.authorized(r) {
				f.jsonError(w, 401, `{"access_token":"invalid_access_token"}`)
				return
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"account_id":"acc-123"}`))
		case "files/list_folder":
			if !f.authorized(r) {
				f.jsonError(w, 401, `{"access_token":"invalid_access_token"}`)
				return
			}
			f.mu.Lock()
			remaining := f.listCursor
			f.listCursor--
			f.mu.Unlock()
			if remaining > 0 {
				w.WriteHeader(200)
				_, _ = w.Write([]byte(`{"entries":[{"name":"a.txt","path_display":"/sub/a.txt",".tag":"file","size":10,"rev":"r1","id":"id-a"}],"cursor":"c1","has_more":true}`))
			} else {
				w.WriteHeader(200)
				_, _ = w.Write([]byte(`{"entries":[
					{"name":"docs","path_display":"/docs",".tag":"folder","id":"id-d"},
					{"name":"notes.md","path_display":"/notes.md",".tag":"file","size":22,"rev":"r2","id":"id-n","hash":"h1","content_hash":"ch1","server_modified":"2026-01-02T03:04:05Z"}
				],"cursor":null,"has_more":false}`))
			}
		case "files/download":
			arg := r.Header.Get("Dropbox-API-Arg")
			if strings.Contains(arg, "missing") {
				f.jsonError(w, 404, `{"path":{".tag":"not_found","summary":"path_not_found"}}`)
				return
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte("dropbox file bytes"))
		case "files/upload":
			if !f.authorized(r) {
				f.jsonError(w, 401, `{"access_token":"invalid_access_token"}`)
				return
			}
			body, _ := io.ReadAll(r.Body)
			arg := r.Header.Get("Dropbox-API-Arg")
			if strings.Contains(arg, "conflict") {
				// Dropbox returns HTTP 409 for upload conflicts.
				f.jsonError(w, 409, `{"conflict":{"summary":"upload_...same_file..."}}`)
				return
			}
			f.mu.Lock()
			f.uploads[arg] = string(body)
			f.mu.Unlock()
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"rev":"rev777","id":"id-up"}`))
		case "files/delete_v2":
			if strings.Contains(bodyPath(r), "missing") {
				f.jsonError(w, 404, `{"path":{".tag":"not_found"}}`)
				return
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{}`))
		case "files/create_folder_v2":
			if strings.Contains(bodyPath(r), "exists") {
				f.jsonError(w, 409, `{"folder":{"summary":"...same folder..."}, "error_summary":"folder_exists"}`)
				return
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"metadata":{"path_display":"/newdir"}, "name":"newdir"}`))
		case "files/move_v2":
			if strings.Contains(bodyPath(r), "conflict") {
				f.jsonError(w, 409, `{"conflict":{"summary":"different_file"}}`)
				return
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"metadata":{"path_display":"/moved"}}`))
		case "files/get_metadata":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"path_display":"/m","name":"m.",".tag":"file","size":5,"rev":"m-rev","id":"id-m","server_modified":"2026-01-01T00:00:00Z"}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":{"path":{".tag":"not_found"}}}`))
		}
	}
}

func dbxStart(t *testing.T) *fakeDropbox {
	t.Helper()
	f := newFakeDropbox()
	f.srv = httptest.NewServer(f.handler())
	t.Cleanup(f.srv.Close)
	return f
}

func dbxClientFor(t *testing.T, f *fakeDropbox, token string) *DropboxClient {
	t.Helper()
	c, err := NewDropboxClient(DropboxConfig{APIBase: f.srv.URL, ContentBase: f.srv.URL}, token)
	if err != nil {
		t.Fatalf("NewDropboxClient: %v", err)
	}
	return c
}

func TestDBX_CheckOK(t *testing.T) {
	f := dbxStart(t)
	c := dbxClientFor(t, f, f.goodToken)
	acc, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if acc != "acc-123" {
		t.Fatalf("account = %q", acc)
	}
}

func TestDBX_CheckAuthFailure(t *testing.T) {
	f := dbxStart(t)
	c := dbxClientFor(t, f, "sl.bad")
	_, err := c.Check(context.Background())
	if dropboxStatusOf(err) != 401 {
		t.Fatalf("expected 401, got %v (statusOf=%d)", err, dropboxStatusOf(err))
	}
}

func TestDBX_List(t *testing.T) {
	f := dbxStart(t)
	c := dbxClientFor(t, f, f.goodToken)
	res, err := c.List(context.Background(), "/", false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	entries, ok := res["entries"].([]DropboxEntry)
	if !ok || len(entries) < 2 {
		t.Fatalf("bad entries: %#v", res)
	}
	var notes *DropboxEntry
	for i := range entries {
		if entries[i].Name == "notes.md" {
			notes = &entries[i]
		}
	}
	if notes == nil {
		t.Fatalf("notes.md not found: %+v", entries)
	}
	if notes.RelativePath != "notes.md" {
		t.Fatalf("relative_path = %q", notes.RelativePath)
	}
	if notes.Size != 22 || notes.Type != "file" || !notes.Binary {
		t.Fatalf("entry fields wrong: %+v", notes)
	}
	if notes.Hash == nil || *notes.Hash != "h1" {
		t.Fatalf("hash = %v", notes.Hash)
	}
	if notes.ContentHash == nil || *notes.ContentHash != "ch1" {
		t.Fatalf("content_hash = %v", notes.ContentHash)
	}
	if notes.Rev == nil || *notes.Rev != "r2" {
		t.Fatalf("rev = %v", notes.Rev)
	}
	if notes.Mtime == "" {
		t.Fatal("mtime empty")
	}
}

func TestDBX_ListPagination(t *testing.T) {
	f := dbxStart(t)
	f.listCursor = 1 // first call returns has_more, second returns the rest
	c := dbxClientFor(t, f, f.goodToken)
	res, err := c.List(context.Background(), "/sub", false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	entries, _ := res["entries"].([]DropboxEntry)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries across pages (a.txt + docs + notes.md), got %d", len(entries))
	}
}

func TestDBX_Download(t *testing.T) {
	f := dbxStart(t)
	c := dbxClientFor(t, f, f.goodToken)
	b64, notFound, err := c.Download(context.Background(), "/docs/x.bin")
	if err != nil || notFound {
		t.Fatalf("Download: %v notFound=%v", err, notFound)
	}
	dec, _ := base64.StdEncoding.DecodeString(b64)
	if string(dec) != "dropbox file bytes" {
		t.Fatalf("content = %q", dec)
	}
	_, notFound, _ = c.Download(context.Background(), "/missing")
	if !notFound {
		t.Fatal("expected notFound")
	}
}

func TestDBX_UploadOK(t *testing.T) {
	f := dbxStart(t)
	c := dbxClientFor(t, f, f.goodToken)
	res, err := c.Upload(context.Background(), "/docs/new.bin", base64.StdEncoding.EncodeToString([]byte("payload")), "overwrite", "")
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if res["revision"] != "rev777" || res["dropbox_id"] != "id-up" {
		t.Fatalf("upload result = %+v", res)
	}
}

func TestDBX_UploadConflict(t *testing.T) {
	f := dbxStart(t)
	c := dbxClientFor(t, f, f.goodToken)
	_, err := c.Upload(context.Background(), "/conflict", base64.StdEncoding.EncodeToString([]byte("x")), "update", "rev0")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if dropboxStatusOf(err) != 409 {
		t.Fatalf("expected 409, got %d (%v)", dropboxStatusOf(err), err)
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected 'conflict' in message, got %q", err.Error())
	}
}

func TestDBX_Delete(t *testing.T) {
	f := dbxStart(t)
	c := dbxClientFor(t, f, f.goodToken)
	nf, err := c.Delete(context.Background(), "/docs/gone.txt")
	if err != nil || nf {
		t.Fatalf("Delete: nf=%v err=%v", nf, err)
	}
	nf, _ = c.Delete(context.Background(), "/missing")
	if !nf {
		t.Fatal("expected notFound for missing")
	}
}

func TestDBX_CreateDirectory(t *testing.T) {
	f := dbxStart(t)
	c := dbxClientFor(t, f, f.goodToken)
	created, err := c.CreateDirectory(context.Background(), "/newdir")
	if err != nil || !created {
		t.Fatalf("CreateDirectory: created=%v err=%v", created, err)
	}
	created, _ = c.CreateDirectory(context.Background(), "/exists")
	if created {
		t.Fatal("expected created=false for existing")
	}
}

func TestDBX_Move(t *testing.T) {
	f := dbxStart(t)
	c := dbxClientFor(t, f, f.goodToken)
	res, err := c.Move(context.Background(), "/src", "/dst")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if res["new_path"] != "/moved" {
		t.Fatalf("new_path = %v", res["new_path"])
	}
}

// --- helpers ----------------------------------------------------------------

func TestDBX_ValidateToken(t *testing.T) {
	if err := dbxValidateToken(""); err == nil {
		t.Fatal("expected error for empty")
	}
	if err := dbxValidateToken("sl.abc"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestDBX_IsTokenFormat(t *testing.T) {
	if !dbxIsTokenFormat("sl.abc") || !dbxIsTokenFormat("dp.xyz") {
		t.Fatal("should be valid format")
	}
	if dbxIsTokenFormat("abc") {
		t.Fatal("should be invalid format")
	}
}

func TestDBX_SanitizeTokenForLogging(t *testing.T) {
	if got := dbxSanitizeTokenForLogging(""); got != "[none]" {
		t.Fatalf("got %q", got)
	}
	if got := dbxSanitizeTokenForLogging("short"); got != "[hidden]" {
		t.Fatalf("got %q", got)
	}
	if got := dbxSanitizeTokenForLogging("sl.1234567890abcd"); !strings.Contains(got, "...") {
		t.Fatalf("got %q", got)
	}
}

func TestDBX_SafeProviderError(t *testing.T) {
	in := "failed access_token=secret123 with sl.abc token"
	got := safeProviderError(in, "sl.abc")
	if strings.Contains(got, "secret123") {
		t.Fatalf("token value leaked: %q", got)
	}
	if !strings.Contains(got, "access_token=<redacted>") {
		t.Fatalf("expected redaction: %q", got)
	}
}

func TestDBX_MapDropboxError(t *testing.T) {
	cases := []struct {
		status int
		d      *dropboxErrorDetails
		wantC  int
		wantM  string
	}{
		{401, nil, 401, "Invalid or expired access token"},
		{200, &dropboxErrorDetails{AccessToken: strPtr("invalid_access_token")}, 401, "Invalid or expired access token"},
		{429, nil, 429, "Rate limit exceeded. Please wait and try again."},
		{403, nil, 403, "Permission denied"},
		{404, nil, 404, "File or folder not found"},
		{200, &dropboxErrorDetails{Path: &dropboxPathError{Tag: "not_found"}}, 404, "File or folder not found"},
		{409, &dropboxErrorDetails{Conflict: &struct{ Summary string }{Summary: "same_file"}}, 409, "File conflict detected"},
		{500, nil, 503, "Dropbox service temporarily unavailable"},
	}
	for _, tc := range cases {
		c, m, _ := mapDropboxError(tc.status, tc.d)
		if c != tc.wantC || m != tc.wantM {
			t.Fatalf("mapDropboxError(%d,%+v) = (%d,%q), want (%d,%q)", tc.status, tc.d, c, m, tc.wantC, tc.wantM)
		}
	}
}

func strPtr(s string) *string { return &s }

// --- handler wiring (service token + missing fields) ------------------------

func TestDBX_HandlerHealthAndAuth(t *testing.T) {
	f := dbxStart(t)
	h := &DropboxHandlers{Cfg: DropboxConfig{ServiceToken: "svc", APIBase: f.srv.URL, ContentBase: f.srv.URL}}
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()

	code := dbxHTTP(srv.URL+"/health", nil, nil)
	if code != 200 {
		t.Fatalf("/health got %d", code)
	}
	code = dbxHTTP(srv.URL+"/check", map[string]interface{}{"access_token": f.goodToken}, nil)
	if code != 401 {
		t.Fatalf("/check no service token got %d", code)
	}
	code = dbxHTTP(srv.URL+"/check", map[string]interface{}{"access_token": f.goodToken}, map[string]string{"X-Service-Token": "svc"})
	if code != 200 {
		t.Fatalf("/check with token got %d", code)
	}
}

func TestDBX_HandlerListMissingToken(t *testing.T) {
	f := dbxStart(t)
	h := &DropboxHandlers{Cfg: DropboxConfig{APIBase: f.srv.URL, ContentBase: f.srv.URL}}
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	code := dbxHTTP(srv.URL+"/list", map[string]interface{}{"path": "/"}, nil)
	if code != 400 {
		t.Fatalf("expected 400, got %d", code)
	}
}

func TestDBX_HandlerFileGet(t *testing.T) {
	f := dbxStart(t)
	h := &DropboxHandlers{Cfg: DropboxConfig{APIBase: f.srv.URL, ContentBase: f.srv.URL}}
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	code := dbxDo("GET", srv.URL+"/file?path=/docs/x.bin", nil, map[string]string{"X-Access-Token": f.goodToken})
	if code != 200 {
		t.Fatalf("file GET got %d", code)
	}
}

func TestDBX_HandlerFileDeleteNotFound(t *testing.T) {
	f := dbxStart(t)
	h := &DropboxHandlers{Cfg: DropboxConfig{APIBase: f.srv.URL, ContentBase: f.srv.URL}}
	srv := httptest.NewServer(h.Mux())
	defer srv.Close()
	code := dbxDo("DELETE", srv.URL+"/file?path=/missing", nil, map[string]string{"X-Access-Token": f.goodToken})
	if code != 200 {
		t.Fatalf("file DELETE missing got %d (expected 200 notFound)", code)
	}
}

// --- http helpers -----------------------------------------------------------

func dbxHTTP(url string, body map[string]interface{}, header map[string]string) int {
	return dbxDo("POST", url, body, header)
}

func dbxDo(method, url string, body map[string]interface{}, header map[string]string) int {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, url, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}
