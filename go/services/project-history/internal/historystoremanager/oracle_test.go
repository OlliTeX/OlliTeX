// C2 oracle — HistoryStoreManager vendor test contracts (40 vendor `it`s,
// mirrored on their observable surface: URLs, payloads, error envelopes,
// results). Cases cite HistoryStoreManagerTests.js by describe/it name.
package historystoremanager

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeClient struct {
	methods []string
	urls    []string
	queries []map[string]string
	bodies  []string
	// response program (one entry per call; last entry repeats)
	status int
	body   []byte
	tfail  error
}

func (f *fakeClient) Do(ctx context.Context, method, url string, query map[string]string, header map[string]string, body []byte) ([]byte, int, error) {
	f.methods = append(f.methods, method)
	f.urls = append(f.urls, url)
	f.queries = append(f.queries, query)
	f.bodies = append(f.bodies, string(body))
	if f.tfail != nil {
		return nil, 0, f.tfail
	}
	return f.body, f.status, nil
}

func (f *fakeClient) Head(ctx context.Context, url string, header map[string]string) (int, error) {
	f.urls = append(f.urls, "HEAD:"+url)
	return f.status, nil
}

type fakeWriter struct{ paths []string }

func (w *fakeWriter) WriteToDisk(ctx context.Context, data []byte, url, fileID string) (string, error) {
	fs := "/tmp/upload/" + fileID
	w.paths = append(w.paths, fs)
	return fs, nil
}

type fakeRangeT struct {
	data   map[string]any
	found  bool
	err    error
	called int
}

func (f *fakeRangeT) CreateRangeBlobDataFromUpdate(update map[string]any) (map[string]any, bool, error) {
	f.called++
	return f.data, f.found, f.err
}

type fakeHasher struct{ hash string }

func (f fakeHasher) GetBlobHash(path string) (string, int64, error) {
	return f.hash, 12, nil
}

type fakeFileStore struct {
	data    []byte
	err     error
	readURL string
}

func (f *fakeFileStore) FileStoreRead(ctx context.Context, url string) ([]byte, error) {
	f.readURL = url
	return f.data, f.err
}

var cctx = context.Background()

// --- 1. getMostRecentChunk: successful -------------------------------

func TestC2_MostRecentChunkPath(t *testing.T) {
	fc := &fakeClient{status: 200, body: []byte(`{"chunk":{"startVersion":3,"history":{"changes":[{"timestamp":"2025-01-01T00:00:00Z"}]}}}`)}
	d := &Deps{Client: fc, Settings: Settings{HistoryHost: "http://hist"}}
	chunk, err := d.GetMostRecentChunk(cctx, "proj-1", "hist-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.urls) != 1 || fc.urls[0] != "http://hist/projects/hist-1/latest/history" {
		t.Fatalf("url: %v", fc.urls)
	}
	if fc.methods[0] != "GET" {
		t.Fatalf("method: %v", fc.methods)
	}
	if sv, _ := toIntAny(chunk["chunk"].(map[string]any)["startVersion"]); sv != 3 {
		t.Fatalf("chunk: %#v", chunk)
	}
}

func TestC2_MostRecentChunkMockSeam(t *testing.T) {
	called := false
	d := &Deps{Client: &fakeClient{}, Mocks: Mocks{
		GetMostRecentChunk: func(p, h string) (map[string]any, error) {
			called = true
			return map[string]any{"chunk": map[string]any{"startVersion": 9}}, nil
		},
	}}
	chunk, err := d.GetMostRecentChunk(cctx, "p", "h")
	if err != nil || !called || chunk["chunk"].(map[string]any)["startVersion"] != 9 {
		t.Fatalf("mock seam: %#v %v %v", chunk, err, called)
	}
}

// --- 2. unexpected response (E2) ------------------------------------

func TestC2_ChunkUnexpectedResponse(t *testing.T) {
	for name, body := range map[string]string{
		"no chunk key":   `{}`,
		"chunk null":     `{"chunk":null}`,
		"missing startV": `{"chunk":{"history":{}}}`,
		"null body":      ``,
	} {
		t.Run(name, func(t *testing.T) {
			fc := &fakeClient{status: 200, body: []byte(body)}
			d := &Deps{Client: fc, Settings: Settings{HistoryHost: "h"}}
			_, err := d.GetMostRecentChunk(cctx, "p", "h")
			if err == nil {
				t.Fatal("want error")
			}
			if err.Error() != "unexpected response" {
				t.Fatalf("msg: %q", err.Error())
			}
		})
	}
}

// --- 3. getMostRecentVersion: success --------------------------------

func TestC2_MostRecentVersionSuccess(t *testing.T) {
	chunk := `{"chunk":{"startVersion":10,"history":{"snapshot":{"projectVersion":"11","v2DocVersions":{"d1":{"v":"5"}}},"changes":[
		{"timestamp":"2025-01-01T00:00:00Z"},
		{"projectVersion":"13","timestamp":"2025-01-02T00:00:00Z"},
		{"v2DocVersions":{"d1":{"v":"7"},"d2":{"v":"1"}},"timestamp":"2025-01-01T12:00:00Z"},
		{"timestamp":"2025-01-03T00:00:00Z"}]}}}`
	d := &Deps{Client: &fakeClient{status: 200, body: []byte(chunk)}, Settings: Settings{HistoryHost: "h"}}
	version, psdv, lastChange, chunkOut, err := d.GetMostRecentVersion(cctx, "p", "h")
	if err != nil {
		t.Fatal(err)
	}
	if version != 14 {
		t.Fatalf("version = %d, want 14", version)
	}
	if psdv["project"] != "13" {
		t.Fatalf("project: %#v", psdv)
	}
	docs := psdv["docs"].(map[string]any)
	if docs["d1"].(map[string]any)["v"] != "7" || docs["d2"].(map[string]any)["v"] != "1" {
		t.Fatalf("docs: %#v", docs)
	}
	if lastChange == nil || lastChange["timestamp"] != "2025-01-03T00:00:00Z" {
		t.Fatalf("lastChange: %#v", lastChange)
	}
	if chunkOut == nil {
		t.Fatal("chunk missing")
	}
}

// --- 4. out-of-order scans -------------------------------------------

func TestC2_OutOfOrderDocVersions(t *testing.T) {
	chunk := `{"chunk":{"startVersion":0,"history":{"snapshot":{"v2DocVersions":{"d1":{"v":"5"}}},"changes":[
		{"v2DocVersions":{"d1":{"v":"7"}}},
		{"v2DocVersions":{"d1":{"v":"3"}}}]}}}`
	d := &Deps{Client: &fakeClient{status: 200, body: []byte(chunk)}, Settings: Settings{HistoryHost: "h"}}
	version, psdv, _, _, err := d.GetMostRecentVersion(cctx, "p", "h")
	if err == nil || err.Error() != "doc version out of order" {
		t.Fatalf("err (want 'doc version out of order'): %v", err)
	}
	if version != 2 {
		t.Fatalf("version %d (still returned)", version)
	}
	docs := psdv["docs"].(map[string]any)
	if docs["d1"].(map[string]any)["v"] != "7" {
		t.Fatalf("d1 should stay at last good (7): %#v", docs)
	}
}

func TestC2_OutOfOrderProjectVersions(t *testing.T) {
	chunk := `{"chunk":{"startVersion":0,"history":{"snapshot":{"projectVersion":"9"},"changes":[
		{"projectVersion":"12"},
		{"projectVersion":"5"},
		{"projectVersion":"13"}]}}}`
	d := &Deps{Client: &fakeClient{status: 200, body: []byte(chunk)}, Settings: Settings{HistoryHost: "h"}}
	_, psdv, _, _, err := d.GetMostRecentVersion(cctx, "p", "h")
	if err == nil || err.Error() != "project structure version out of order" {
		t.Fatalf("err: %v", err)
	}
	oe, ok := err.(oError)
	if !ok {
		t.Fatalf("want oError: %T", err)
	}
	info := oe.Info()
	if info["projectVersionInSnapshot"] != "9" || info["projectVersionInChange"] != "5" || info["changeIdx"] != 1 {
		t.Fatalf("info: %#v", info)
	}
	if psdv["project"] != "13" {
		t.Fatalf("project: %#v (last non-out-of-order still tracked)", psdv)
	}
}

func TestC2_OutOfOrderBoth_ProjectErrorWins(t *testing.T) {
	chunk := `{"chunk":{"startVersion":0,"history":{"snapshot":{"projectVersion":"9","v2DocVersions":{"d1":{"v":"5"}}},"changes":[
		{"projectVersion":"5","v2DocVersions":{"d1":{"v":"3"}}}]}}}`
	d := &Deps{Client: &fakeClient{status: 200, body: []byte(chunk)}, Settings: Settings{HistoryHost: "h"}}
	_, _, _, _, err := d.GetMostRecentVersion(cctx, "p", "h")
	if err == nil || err.Error() != "project structure version out of order" {
		t.Fatalf("want project error first (err1 || err2): %v", err)
	}
}

// --- 5. _requestHistoryService error envelopes (E1) -------------------

func TestC2_ErrorEnvelope409Body(t *testing.T) {
	fc := &fakeClient{status: 409, body: []byte(`{"error":"conflict"}`)}
	d := &Deps{Client: fc, Settings: Settings{HistoryHost: "h"}}
	_, err := d.GetProjectBlob(cctx, "h1", "b1")
	if err == nil {
		t.Fatal("want error")
	}
	if err.Error() != "history store a non-success status code: 409" {
		t.Fatalf("msg: %q", err.Error())
	}
	oe, ok := asOError(err)
	if !ok {
		t.Fatalf("not an oError: %T", err)
	}
	info := oe.Info()
	if info["statusCode"] != 409 {
		t.Fatalf("statusCode: %#v", info)
	}
	if info["body"] != `{"error":"conflict"}` {
		t.Fatalf("body (409 keeps body): %#v", info["body"])
	}
	if info["method"] != "GET" {
		t.Fatalf("method: %#v", info["method"])
	}
}

func TestC2_ErrorEnvelope500NoBody(t *testing.T) {
	fc := &fakeClient{status: 500, body: []byte(`{"oops":true}`)}
	d := &Deps{Client: fc, Settings: Settings{HistoryHost: "h"}}
	_, err := d.GetProjectBlob(cctx, "h1", "b1")
	oe, ok := asOError(err)
	if !ok {
		t.Fatalf("not an oError: %T", err)
	}
	if oe.Info()["body"] != nil {
		t.Fatalf("500 must NOT carry body: %#v", oe.Info()["body"])
	}
	if err.Error() != "history store a non-success status code: 500" {
		t.Fatalf("msg: %q", err.Error())
	}
}

// --- 6. getMostRecentVersionRaw ---------------------------------------

func TestC2_VersionRawReadOnly(t *testing.T) {
	fc := &fakeClient{status: 200, body: []byte(`{"startVersion":1,"endVersion":9,"endTimestamp":"2025-06-01T10:00:00.000Z"}`)}
	d := &Deps{Client: fc, Settings: Settings{HistoryHost: "h"}}
	rv, err := d.GetMostRecentVersionRaw(cctx, "p", "h", true)
	if err != nil {
		t.Fatal(err)
	}
	if rv.StartVersion != 1 || rv.EndVersion != 9 {
		t.Fatalf("raw: %#v", rv)
	}
	if fc.queries[0]["readOnly"] != "true" {
		t.Fatalf("readOnly query: %#v", fc.queries)
	}
}

// --- 7. sendChanges ----------------------------------------------------

func TestC2_SendChangesResyncNeeded(t *testing.T) {
	fc := &fakeClient{status: 200, body: []byte(`{"resyncNeeded":true}`)}
	d := &Deps{Client: fc, Settings: Settings{HistoryHost: "http://h"}}
	resync, err := d.SendChanges(cctx, "p", "h", []any{map[string]any{"op": "x"}}, 7)
	if err != nil || !resync {
		t.Fatalf("resync=%v err=%v", resync, err)
	}
	if fc.methods[0] != "POST" || fc.urls[0] != "http://h/projects/h/legacy_changes" { // host http://h

		t.Fatalf("post: %v %v", fc.methods, fc.urls)
	}
	if fc.queries[0]["end_version"] != "7" {
		t.Fatalf("qs: %#v", fc.queries)
	}
}

func TestC2_SendChangesDefaultFalse(t *testing.T) {
	fc := &fakeClient{status: 200, body: []byte(`{}`)}
	d := &Deps{Client: fc, Settings: Settings{HistoryHost: "h"}}
	resync, err := d.SendChanges(cctx, "p", "h", []any{}, 1)
	if err != nil || resync {
		t.Fatalf("resync=%v (want default false) err=%v", resync, err)
	}
}

func TestC2_SendChangesFailureEnvelope(t *testing.T) {
	fc := &fakeClient{status: 422, body: []byte(`bad`)}
	d := &Deps{Client: fc, Settings: Settings{HistoryHost: "h"}}
	_, err := d.SendChanges(cctx, "p1", "h1", []any{}, 3)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "failed to send changes to v1") {
		t.Fatalf("tag msg: %q", err.Error())
	}
	oe, ok := asOError(err)
	if !ok {
		t.Fatalf("not an oError: %T", err)
	}
	info := oe.Info()
	if info["projectId"] != "p1" || info["historyId"] != "h1" || info["endVersion"] != 3 {
		t.Fatalf("info: %#v", info)
	}
	if info["statusCode"] != 422 || info["errorBody"] != "bad" {
		t.Fatalf("status/body props: %#v", info)
	}
}

// --- 8. createBlobForUpdate --------------------------------------------

func stdBlobDeps(c Client) *Deps {
	return &Deps{
		Client:     c,
		FileWriter: &fakeWriter{},
		Hasher:     fakeHasher{hash: "hash-1"},
		Settings:   Settings{HistoryHost: "http://h", FilestoreURL: "http://fs", FilestoreEnabled: true},
	}
}

func TestC2_BlobFileUpdateFilestore(t *testing.T) {
	fs := &fakeFileStore{data: []byte("file-bytes")}
	fc := &headDoClient{head: 404, do: 201}
	d := stdBlobDeps(fc)
	d.FileStore = fs
	out, err := d.CreateBlobForUpdate(cctx, "abcabcabcabcabcabcabcabc", "h1", map[string]any{
		"file":   true,
		"url":    "http://other/project/abcabcabcabcabcabcabcabc/file/defdefdefdefdefdefdefdef",
		"hash":   "web-hash",
		"ranges": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["file"] != "hash-1" {
		t.Fatalf("out: %#v", out)
	}
	if fs.readURL != "http://fs/project/abcabcabcabcabcabcabcabc/file/defdefdefdefdefdefdefdef" {
		t.Fatalf("filestore URL rewrite: %q", fs.readURL)
	}
	// last Do = PUT blob
	last := len(fc.urls) - 1
	if fc.methods[last] != "PUT" || !strings.Contains(fc.urls[last], "/blobs/hash-1") {
		t.Fatalf("put: %v %v", fc.methods[last], fc.urls[last])
	}
}

func TestC2_BlobFilestoreDisabled(t *testing.T) {
	fc := &fakeClient{status: 201}
	fs := &fakeFileStore{}
	d := stdBlobDeps(fc)
	d.Settings.FilestoreEnabled = false
	d.FileStore = fs
	_, err := d.CreateBlobForUpdate(cctx, "abcabcabcabcabcabcabcabc", "h1", map[string]any{
		"file": true,
		"url":  "http://other/project/abcabcabcabcabcabcabcabc/file/defdefdefdefdefdefdefdef",
	})
	if err == nil || err.Error() != "blocking filestore read" {
		t.Fatalf("err: %v", err)
	}
	if fs.readURL != "" {
		t.Fatal("must not read from filestore when disabled")
	}
}

func TestC2_BlobInvalidLocation(t *testing.T) {
	d := stdBlobDeps(&fakeClient{status: 200})
	fs := &fakeFileStore{}
	d.FileStore = fs
	_, err := d.CreateBlobForUpdate(cctx, "abcabcabcabcabcabcabcabc", "h1", map[string]any{
		"file": true,
		"url":  "http://other/project/abcdefabcdefabcdefabcdef/file/defdefdefdefdefdefdefdef",
	})
	if err == nil || err.Error() != "invalid project for blob creation" {
		t.Fatalf("err: %v", err)
	}
	// a malformed path gives the other message
	d2 := stdBlobDeps(&fakeClient{status: 200})
	_, err2 := d2.CreateBlobForUpdate(cctx, "abcabcabcabcabcabcabcabc", "h1", map[string]any{
		"file": true,
		"url":  "http://other/somewhere-else/x",
	})
	if err2 == nil || err2.Error() != "invalid file for blob creation" {
		t.Fatalf("err2: %v", err2)
	}
}

func TestC2_BlobCreatedFlagNoURL(t *testing.T) {
	d := stdBlobDeps(&fakeClient{status: 200})
	_, err := d.CreateBlobForUpdate(cctx, "p", "h1", map[string]any{
		"file":        true,
		"createdBlob": true,
	})
	if err == nil || err.Error() != "no filestore URL provided and blob was not created" {
		t.Fatalf("err: %v", err)
	}
}

func TestC2_Blob404StoredEmpty(t *testing.T) {
	fc := &fakeClient{status: 201}
	fs := &fakeFileStore{err: &FileStoreStatus{Status: 404}}
	d := stdBlobDeps(fc)
	d.FileStore = fs
	// HEAD must report 404 as "not exists" — the seam returns 201 only for Do;
	// set Head status:
	fc.status = 404
	// Do calls after the HEAD: the PUT must still succeed — model: Head uses
	// the client status at call time; the fake returns the same status for
	// both. To keep this test honest, give the client separate statuses:
	type splitClient struct {
		head int
		do   int
	}
	_ = splitClient{}
	d.Client = &headDoClient{head: 404, do: 201}
	out, err := d.CreateBlobForUpdate(cctx, "abcabcabcabcabcabcabcabc", "h1", map[string]any{
		"file": true,
		"url":  "http://other/project/abcabcabcabcabcabcabcabc/file/defdefdefdefdefdefdefdef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["file"] != "hash-1" {
		t.Fatalf("out: %#v", out)
	}
}

type headDoClient struct {
	head, do int
	methods  []string
	urls     []string
	queries  []map[string]string
	bodies   []string
	body     []byte
}

func (h *headDoClient) Do(ctx context.Context, method, url string, query map[string]string, header map[string]string, body []byte) ([]byte, int, error) {
	h.methods = append(h.methods, method)
	h.urls = append(h.urls, url)
	h.queries = append(h.queries, query)
	h.bodies = append(h.bodies, string(body))
	if method == "HEAD" {
		return nil, h.head, nil
	}
	return h.body, h.do, nil
}
func (h *headDoClient) Head(ctx context.Context, url string, header map[string]string) (int, error) {
	return h.head, nil
}

func TestC2_BlobFilestoreReadError(t *testing.T) {
	d := stdBlobDeps(&fakeClient{status: 201})
	fs := &fakeFileStore{err: &FileStoreStatus{Status: 500}}
	d.Client = &headDoClient{head: 404, do: 201}
	d.FileStore = fs
	_, err := d.CreateBlobForUpdate(cctx, "abcabcabcabcabcabcabcabc", "h1", map[string]any{
		"file": true,
		"url":  "http://other/project/abcabcabcabcabcabcabcabc/file/defdefdefdefdefdefdefdef",
	})
	if err == nil || !strings.Contains(err.Error(), "error from filestore") {
		t.Fatalf("err: %v", err)
	}
}

func TestC2_BlobDocUpdateWithRanges(t *testing.T) {
	fr := &fakeRangeT{data: map[string]any{"trackedChanges": []any{}, "comments": []any{}}, found: true}
	d := stdBlobDeps(&fakeClient{status: 201})
	d.Range = fr
	out, err := d.CreateBlobForUpdate(cctx, "p", "h1", map[string]any{
		"doc":      "d1",
		"docLines": "hello doc lines",
		"ranges":   map[string]any{"changes": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["file"] != "hash-1" || out["ranges"] != "hash-1" {
		t.Fatalf("out: %#v", out)
	}
	if fr.called != 1 {
		t.Fatal("range translator must be called exactly once")
	}
}

func TestC2_BlobDocUpdateNoRanges(t *testing.T) {
	fr := &fakeRangeT{found: false}
	d := stdBlobDeps(&fakeClient{status: 201})
	d.Range = fr
	out, err := d.CreateBlobForUpdate(cctx, "p", "h1", map[string]any{
		"doc":      "d1",
		"docLines": "hello",
		"ranges":   map[string]any{"changes": []any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["file"] != "hash-1" {
		t.Fatalf("out: %#v", out)
	}
	if _, has := out["ranges"]; has {
		t.Fatal("no ranges key expected")
	}
}

func TestC2_BlobInvalidUpdate(t *testing.T) {
	d := stdBlobDeps(&fakeClient{status: 200})
	_, err := d.CreateBlobForUpdate(cctx, "p", "h1", map[string]any{"foo": "bar"})
	if err == nil || err.Error() != "invalid update for blob creation" {
		t.Fatalf("err: %v", err)
	}
}

// --- 9. initialize / delete / clone ------------------------------------

func TestC2_InitializeProjectPayload(t *testing.T) {
	fc := &fakeClient{status: 200, body: []byte(`{"projectId":"new-id"}`)}
	d := &Deps{Client: fc, Settings: Settings{HistoryHost: "h"}}
	id, err := d.InitializeProject(cctx, nil)
	if err != nil || id != "new-id" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if fc.methods[0] != "POST" || fc.bodies[0] != "true" {
		t.Fatalf("payload (nil historyId → true): %v %q", fc.methods, fc.bodies)
	}
	fc2 := &fakeClient{status: 200, body: []byte(`{"projectId":"a"}`)}
	d2 := &Deps{Client: fc2, Settings: Settings{HistoryHost: "h"}}
	hid := "hist-9"
	if _, err := d2.InitializeProject(cctx, &hid); err != nil {
		t.Fatal(err)
	}
	if fc2.bodies[0] != `{"projectId":"hist-9"}` {
		t.Fatalf("payload (historyId): %q", fc2.bodies[0])
	}
}

func TestC2_InitializeProjectMissingID(t *testing.T) {
	fc := &fakeClient{status: 200, body: []byte(`{}`)}
	d := &Deps{Client: fc, Settings: Settings{HistoryHost: "h"}}
	_, err := d.InitializeProject(cctx, nil)
	if err == nil || err.Error() != "history store did not return a project id" {
		t.Fatalf("err: %v", err)
	}
	var oe oError
	if !errors.As(err, &oe) {
		t.Fatalf("not an oError: %T", err)
	}
	if oe.Code() != "" {
		t.Fatalf("code should be '' (id undefined → empty): %q", oe.Code())
	}
}

func TestC2_DeleteAndClone(t *testing.T) {
	fc := &fakeClient{status: 200, body: []byte(`cloned-bytes`)}
	d := &Deps{Client: fc, Settings: Settings{HistoryHost: "http://h"}}
	if err := d.DeleteProject(cctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if fc.methods[0] != "DELETE" || fc.urls[0] != "http://h/projects/p1" {
		t.Fatalf("delete: %v %v", fc.methods, fc.urls)
	}
	out, err := d.CloneProject(cctx, "src", "dst")
	if err != nil || string(out) != "cloned-bytes" {
		t.Fatalf("clone: %q %v", out, err)
	}
	if fc.methods[1] != "POST" || fc.urls[1] != "http://h/projects/src/clone" {
		t.Fatalf("clone url: %v %v", fc.methods[1:], fc.urls[1:])
	}
	if fc.bodies[1] != `{"targetProjectId":"dst"}` {
		t.Fatalf("clone body: %q", fc.bodies[1])
	}
}

func TestC2_BlobStoreFetch(t *testing.T) {
	fc := &fakeClient{status: 200, body: []byte("blob-bytes")}
	d := &Deps{Client: fc, Settings: Settings{HistoryHost: "http://h"}}
	s := d.GetBlobStore("proj")
	got, err := s.FetchString(cctx, "b1")
	if err != nil || got != "blob-bytes" {
		t.Fatalf("fetch: %q %v", got, err)
	}
	if fc.urls[0] != "http://h/projects/proj/blobs/b1" {
		t.Fatalf("url: %v", fc.urls)
	}
}
