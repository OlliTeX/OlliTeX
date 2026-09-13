package docstore

// docstore_test.go — HTTP contract tests locking the exact Node behaviour
// (bodies, content types, issue strings) for every route, plus the archive
// round-trip through the real FS persistor.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Backend: "fs",
		Bucket:  t.TempDir(),
	}
}

func newTestServer(t *testing.T, cfg Config, store Store) *httptest.Server {
	t.Helper()
	cfg.Defaults()
	srv := NewServer(cfg, store, NewFSArchiver())
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	return ts
}

func do(t *testing.T, method, url string, body any) (*http.Response, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		if b, isBytes := body.([]byte); isBytes {
			rd = bytes.NewReader(b)
		} else if s, isStr := body.(string); isStr {
			rd = strings.NewReader(s)
		} else {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			rd = bytes.NewReader(b)
		}
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func get(t *testing.T, url string) (*http.Response, string) { return do(t, "GET", url, nil) }
func post(t *testing.T, url string, body any) (*http.Response, string) {
	return do(t, "POST", url, body)
}

const (
	p1  = "0000000000000000000000a1"
	p2  = "0000000000000000000000a2"
	d1  = "0000000000000000000000b1"
	d2  = "0000000000000000000000b2"
	u1  = "0000000000000000000000c1" // tracked-change user
	tid = "0000000000000000000000d1" // comment thread id
)

func TestStatusAndFallback(t *testing.T) {
	ts := newTestServer(t, testConfig(t), NewMemStore())

	resp, body := get(t, ts.URL+"/status")
	if resp.StatusCode != 200 || body != "docstore is alive" {
		t.Fatalf("status: %d %q", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("status content-type = %q", ct)
	}

	// HEAD mirrors GET (no body)
	resp, _ = do(t, "HEAD", ts.URL+"/status", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("head status: %d", resp.StatusCode)
	}

	// Unknown path → Express fallback
	resp, body = get(t, ts.URL+"/nope")
	if resp.StatusCode != 404 || !strings.Contains(body, "Cannot GET /nope") {
		t.Fatalf("fallback path: %d %q", resp.StatusCode, body)
	}
	if h := resp.Header.Get("Content-Security-Policy"); h != "default-src 'none'" {
		t.Fatalf("fallback CSP = %q", h)
	}
	if h := resp.Header.Get("X-Content-Type-Options"); h != "nosniff" {
		t.Fatalf("fallback XCTO = %q", h)
	}

	// Wrong method on a known path → Cannot PUT
	resp, body = do(t, "PUT", ts.URL+"/project/"+p1+"/doc/"+d1, "{}")
	if resp.StatusCode != 404 || !strings.Contains(body, "Cannot PUT /project/"+p1+"/doc/"+d1) {
		t.Fatalf("fallback method: %d %q", resp.StatusCode, body)
	}
}

func TestValidationShapes(t *testing.T) {
	ts := newTestServer(t, testConfig(t), NewMemStore())

	// Bad project id → 404 (params issue) with the locked issue text.
	resp, body := get(t, ts.URL+"/project/not-an-oid/doc")
	if resp.StatusCode != 404 {
		t.Fatalf("bad project: status %d", resp.StatusCode)
	}
	var v struct {
		Error      string `json:"error"`
		StatusCode int    `json:"statusCode"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("bad project body: %q", body)
	}
	want := `Validation error: Invalid Mongo ObjectId at "params.project_id"`
	if v.Error != want || v.StatusCode != 404 {
		t.Fatalf("bad project: got %q (want %q)", v.Error, want)
	}

	// Bad doc id → 404 with the doc_id issue.
	resp, body = get(t, ts.URL+"/project/"+p1+"/doc/xy")
	v = struct {
		Error      string `json:"error"`
		StatusCode int    `json:"statusCode"`
	}{}
	_ = json.Unmarshal([]byte(body), &v)
	if resp.StatusCode != 404 || !strings.Contains(body, `Invalid Mongo ObjectId at`) || !strings.Contains(body, `params.doc_id`) {
		t.Fatalf("bad doc: %d %q", resp.StatusCode, body)
	}

	// Bad query → 400 (no params issue); the path is query.useSecondary.
	resp, body = get(t, ts.URL+"/project/"+p1+"/has-ranges?useSecondary=bogus")
	if resp.StatusCode != 400 || !strings.Contains(body, `Invalid option: expected one of`) || !strings.Contains(body, `query.useSecondary`) {
		t.Fatalf("bad query: %d %q", resp.StatusCode, body)
	}

	// Unrecognized query key
	resp, body = get(t, ts.URL+"/project/"+p1+"/has-ranges?foo=1")
	if resp.StatusCode != 400 || !strings.Contains(body, `Unrecognized key:`) || !strings.Contains(body, `foo`) || !strings.Contains(body, `at`) {
		t.Fatalf("unknown query: %d %q", resp.StatusCode, body)
	}

	// updateDoc: missing body fields → all three issues reported.
	resp, body = post(t, ts.URL+"/project/"+p1+"/doc/"+d1, map[string]any{})
	if resp.StatusCode != 400 {
		t.Fatalf("update empty: %d", resp.StatusCode)
	}
	if !strings.Contains(body, `Invalid input: expected array, received undefined at \"body.lines\"`) {
		t.Fatalf("update empty lines issue: %q", body)
	}
	if !strings.Contains(body, `Invalid input: expected number, received undefined at \"body.version\"`) {
		t.Fatalf("update empty version issue: %q", body)
	}
	if !strings.Contains(body, `Invalid input: expected object, received undefined at \"body.ranges\"`) {
		t.Fatalf("update empty ranges issue: %q", body)
	}

	// unrecognized body key
	resp, body = post(t, ts.URL+"/project/"+p1+"/doc/"+d1, map[string]any{"lines": []any{"a"}, "version": 1, "ranges": map[string]any{}, "extra": 1})
	if resp.StatusCode != 400 || !strings.Contains(body, `Unrecognized key: \"extra\" at \"body\"`) {
		t.Fatalf("update extra key: %d %q", resp.StatusCode, body)
	}
}

func TestLifecycle(t *testing.T) {
	store := NewMemStore()
	ts := newTestServer(t, testConfig(t), store)
	u := ts.URL + "/project/" + p1 + "/doc"

	// empty list
	resp, body := get(t, u)
	if resp.StatusCode != 200 || body != "[]" {
		t.Fatalf("empty /doc: %d %q", resp.StatusCode, body)
	}

	// create
	resp, body = post(t, u+"/"+d1, map[string]any{"lines": []any{"hello", "world"}, "version": 3, "ranges": map[string]any{}})
	if resp.StatusCode != 200 || body != `{"modified":true,"rev":1}` {
		t.Fatalf("create: %d %q", resp.StatusCode, body)
	}

	// read back (doc view: lines, rev, version — no ranges/deleted keys)
	resp, body = get(t, u+"/"+d1)
	if resp.StatusCode != 200 {
		t.Fatalf("get: %d %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"lines":["hello","world"]`) || !strings.Contains(body, `"rev":1`) || !strings.Contains(body, `"version":3`) {
		t.Fatalf("get view: %q", body)
	}
	// deleted absent (undefined → null → omitted)
	if strings.Contains(body, `"deleted"`) {
		t.Fatalf("get view leaked deleted: %q", body)
	}

	// /doc/<id>/raw joins lines with \n
	resp, body = get(t, u+"/"+d1+"/raw")
	if resp.StatusCode != 200 || body != "hello\nworld" {
		t.Fatalf("raw: %d %q", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("raw ct: %q", ct)
	}

	// update (version advance)
	resp, body = post(t, u+"/"+d1, map[string]any{"lines": []any{"new"}, "version": 4, "ranges": map[string]any{}})
	if resp.StatusCode != 200 || body != `{"modified":true,"rev":2}` {
		t.Fatalf("update: %d %q", resp.StatusCode, body)
	}

	// version decrease → 409 "Conflict" (status text)
	resp, body = post(t, u+"/"+d1, map[string]any{"lines": []any{"x"}, "version": 1, "ranges": map[string]any{}})
	if resp.StatusCode != 409 || body != "Conflict" {
		t.Fatalf("downgrade: %d %q", resp.StatusCode, body)
	}

	// /doc-versions
	resp, body = get(t, ts.URL+"/project/"+p1+"/doc-versions")
	if resp.StatusCode != 200 || !strings.Contains(body, `"version":4`) {
		t.Fatalf("doc-versions: %d %q", resp.StatusCode, body)
	}

	// patch (soft delete)
	resp, body = do(t, "PATCH", u+"/"+d1, map[string]any{
		"deleted":   true,
		"deletedAt": "2026-09-17T12:34:56.789Z",
		"name":      "main",
	})
	if resp.StatusCode != 204 || body != "" {
		t.Fatalf("patch: %d %q", resp.StatusCode, body)
	}

	// deleted doc hidden from /doc and /doc (include_deleted=false → 404)
	resp, body = get(t, u)
	if resp.StatusCode != 200 || body != "[]" {
		t.Fatalf("doc after delete: %d %q", resp.StatusCode, body)
	}
	resp, body = get(t, u+"/"+d1)
	if resp.StatusCode != 404 || body != "Not Found" {
		t.Fatalf("get deleted: %d %q", resp.StatusCode, body)
	}

	// include_deleted=true
	resp, body = get(t, u+"/"+d1+"?include_deleted=true")
	if resp.StatusCode != 200 || !strings.Contains(body, `"deleted":true`) {
		t.Fatalf("get deleted inc: %d %q", resp.StatusCode, body)
	}

	// /doc-deleted lists {_id, name, deletedAt}
	resp, body = get(t, ts.URL+"/project/"+p1+"/doc-deleted")
	if resp.StatusCode != 200 || !strings.Contains(body, `"name":"main"`) || !strings.Contains(body, `"deletedAt":"2026-09-17T12:34:56.789Z"`) {
		t.Fatalf("doc-deleted: %d %q", resp.StatusCode, body)
	}

	// DELETE is the 500 stub
	resp, body = do(t, "DELETE", u+"/"+d1, nil)
	if resp.StatusCode != 500 || !strings.Contains(body, "DELETE-ing a doc is DEPRECATED") {
		t.Fatalf("delete stub: %d %q", resp.StatusCode, body)
	}
}

func TestRangesAndComments(t *testing.T) {
	store := NewMemStore()
	ts := newTestServer(t, testConfig(t), store)
	u := ts.URL + "/project/" + p1 + "/doc"
	ranges := map[string]any{
		"comments": []any{
			map[string]any{
				"op":       map[string]any{"c": "hi", "p": 0, "t": tid},
				"metadata": map[string]any{"user_id": u1},
			},
			map[string]any{
				"op":       map[string]any{"c": "there", "p": 1, "t": tid}, // duplicate thread id
				"metadata": map[string]any{"user_id": u1},
			},
		},
		"changes": []any{
			map[string]any{
				"op":       map[string]any{"i": "ins", "p": 0},
				"metadata": map[string]any{"user_id": u1, "ts": "2026-09-17T00:00:00.000Z"},
			},
			map[string]any{
				"op":       map[string]any{"i": "anon", "p": 2},
				"metadata": map[string]any{"user_id": "anonymous-user", "ts": "2026-09-17T00:00:02.000Z"}, // excluded user
			},
			map[string]any{
				"op":       map[string]any{"d": "del", "p": 3},
				"metadata": map[string]any{"user_id": u1, "ts": "2026-09-17T00:00:01.000Z"},
			},
		},
	}
	resp, body := post(t, u+"/"+d1, map[string]any{"lines": []any{"a"}, "version": 1, "ranges": ranges})
	if resp.StatusCode != 200 || body != `{"modified":true,"rev":1}` {
		t.Fatalf("create ranges: %d %q", resp.StatusCode, body)
	}

	// /comment-thread-ids: {_id: [threadId]}, deduped
	resp, body = get(t, ts.URL+"/project/"+p1+"/comment-thread-ids")
	if resp.StatusCode != 200 || !strings.Contains(body, `["0000000000000000000000d1"]`) {
		t.Fatalf("comment-thread-ids: %d %q", resp.StatusCode, body)
	}
	ids := strings.Count(body, "0000000000000000000000d1")
	if ids != 1 {
		t.Fatalf("comment-thread-ids dedupe: %q", body)
	}

	// /tracked-changes-user-ids: anonymous excluded, user once
	resp, body = get(t, ts.URL+"/project/"+p1+"/tracked-changes-user-ids")
	if resp.StatusCode != 200 || !strings.Contains(body, u1) || strings.Contains(body, "anonymous-user") {
		t.Fatalf("tracked users: %d %q", resp.StatusCode, body)
	}
	if n := strings.Count(body, u1); n != 1 {
		t.Fatalf("tracked users dedupe: %q", body)
	}

	// /has-ranges
	resp, body = get(t, ts.URL+"/project/"+p1+"/has-ranges")
	if resp.StatusCode != 200 || body != `{"projectHasRanges":true}` {
		t.Fatalf("has-ranges: %d %q", resp.StatusCode, body)
	}

	// /ranges view carries the ranges with the converted ids
	resp, body = get(t, ts.URL+"/project/"+p1+"/ranges")
	if resp.StatusCode != 200 || !strings.Contains(body, tid) {
		t.Fatalf("ranges: %d %q", resp.StatusCode, body)
	}
}

func TestArchiveRoundTrip(t *testing.T) {
	cfg := testConfig(t)
	store := NewMemStore()
	ts := newTestServer(t, cfg, store)
	u := ts.URL + "/project/" + p1
	durl := u + "/doc" + "/" + d1

	resp, body := post(t, durl, map[string]any{"lines": []any{"a", "b"}, "version": 7, "ranges": map[string]any{}})
	if resp.StatusCode != 200 {
		t.Fatalf("create: %d %q", resp.StatusCode, body)
	}

	// archive single doc
	resp, body = post(t, durl+"/archive", nil)
	if resp.StatusCode != 204 || body != "" {
		t.Fatalf("archive doc: %d %q", resp.StatusCode, body)
	}

	// FS file exists flat: <bucket>/<pid>_<did>
	archFile := filepath.Join(cfg.Bucket, p1+"_"+d1)
	st, err := os.Stat(archFile)
	if err != nil {
		t.Fatalf("archive file missing: %v (%s)", err, archFile)
	}
	data, _ := os.ReadFile(archFile)
	if !strings.Contains(string(data), `"schema_v":1`) || !strings.Contains(string(data), `"lines":["a","b"]`) {
		t.Fatalf("archive payload: %q", data)
	}
	_ = st

	// peek reports archived
	resp, body = get(t, durl+"/peek")
	if resp.StatusCode == 200 {
		if ct := resp.Header.Get("x-doc-status"); ct != "archived" {
			t.Fatalf("peek status = %q", ct)
		}
	} else {
		t.Fatalf("peek: %d %q", resp.StatusCode, body)
	}

	// /doc triggers unarchive and re-materializes lines
	resp, body = get(t, durl)
	if resp.StatusCode != 200 || !strings.Contains(body, `"lines":["a","b"]`) {
		t.Fatalf("get after archive: %d %q", resp.StatusCode, body)
	}

	// archive all (no-op when already archived)
	resp, body = post(t, u+"/archive", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("archive all: %d %q", resp.StatusCode, body)
	}

	// unarchive all
	resp, body = post(t, u+"/unarchive", nil)
	if resp.StatusCode != 200 || body != "OK" {
		t.Fatalf("unarchive all: %d %q", resp.StatusCode, body)
	}

	// peek back to active
	resp, body = get(t, durl+"/peek")
	if resp.StatusCode != 200 || resp.Header.Get("x-doc-status") != "active" {
		t.Fatalf("peek active: %d %q %q", resp.StatusCode, body, resp.Header.Get("x-doc-status"))
	}

	// destroy wipes docs + archive files
	resp, body = post(t, u+"/destroy", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("destroy: %d %q", resp.StatusCode, body)
	}
	if _, err := os.Stat(archFile); !os.IsNotExist(err) {
		// destroy deletes <bucket>/<pid>_* files
		t.Fatalf("archive file still present after destroy")
	}
	resp, body = get(t, ts.URL+"/project/"+p1+"/doc")
	if resp.StatusCode != 200 || body != "[]" {
		t.Fatalf("doc after destroy: %d %q", resp.StatusCode, body)
	}
	resp, body = get(t, ts.URL+"/project/"+p1+"/doc-deleted")
	if resp.StatusCode != 200 || body != "[]" {
		t.Fatalf("deleted after destroy: %d %q", resp.StatusCode, body)
	}
}

func TestDocLimit413(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxDocLength = 10 // force overflow
	ts := newTestServer(t, cfg, NewMemStore())
	resp, body := post(t, ts.URL+"/project/"+p1+"/doc/"+d1,
		map[string]any{"lines": []any{"abcdefghijklmnop"}, "version": 1, "ranges": map[string]any{}})
	if resp.StatusCode != 413 || body != "document body too large" {
		t.Fatalf("413: %d %q", resp.StatusCode, body)
	}
}

func TestBodyMalformedAndLimits(t *testing.T) {
	cfg := testConfig(t)
	ts := newTestServer(t, cfg, NewMemStore())
	u := ts.URL + "/project/" + p1 + "/doc"

	// malformed JSON → 500 generic
	resp, body := do(t, "POST", u+"/"+d1, bytesInvalidJSON)
	if resp.StatusCode != 500 || body != "Oops, something went wrong" {
		t.Fatalf("bad json: %d %q", resp.StatusCode, body)
	}

	// non-JSON content type → validation "undefined" issues
	req, _ := http.NewRequest("POST", u+"/"+d1, strings.NewReader("lines=abc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp2, err2 := http.DefaultClient.Do(req)
	if err2 != nil {
		t.Fatal(err2)
	}
	b2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != 400 || !strings.Contains(string(b2), `Invalid input: expected object, received undefined at`) {
		t.Fatalf("non-json: %d %q", resp2.StatusCode, b2)
	}

	// top-level array → 400 "expected object"
	resp, body = do(t, "POST", u+"/"+d1, []any{1, 2})
	if resp.StatusCode != 400 || !strings.Contains(body, `Invalid input: expected object, received array at \"body\"`) {
		t.Fatalf("array body: %d %q", resp.StatusCode, body)
	}
}

const bytesInvalidJSON = `{"lines": ["a",]`

func TestPatchValidation(t *testing.T) {
	ts := newTestServer(t, testConfig(t), NewMemStore())
	u := ts.URL + "/project/" + p1 + "/doc" + "/" + d1

	// deleted must be literal true
	resp, body := do(t, "PATCH", u, map[string]any{"deleted": "yes", "name": "x", "deletedAt": "2026-01-01T00:00:00.000Z"})
	_ = resp
	_ = body
	resp, body = do(t, "PATCH", u, map[string]any{"deleted": "yes", "name": "x", "deletedAt": "2026-01-01T00:00:00.000Z"})
	if resp.StatusCode != 400 || !strings.Contains(body, `Invalid input: expected true at`) {
		t.Fatalf("deleted issue: %d %q", resp.StatusCode, body)
	}

	// bad date
	resp, body = do(t, "PATCH", u, map[string]any{"deleted": true, "name": "x", "deletedAt": "not-a-date"})
	if resp.StatusCode != 400 || !strings.Contains(body, `Invalid input: expected date, received Date at`) {
		t.Fatalf("date issue: %d %q", resp.StatusCode, body)
	}

	// missing name
	resp, body = do(t, "PATCH", u, map[string]any{"deleted": true, "deletedAt": "2026-01-01T00:00:00.000Z"})
	if resp.StatusCode != 400 || !strings.Contains(body, `Invalid input: expected string, received undefined at`) {
		t.Fatalf("name issue: %d %q", resp.StatusCode, body)
	}
}

func TestIsDocDeletedAndPeek404(t *testing.T) {
	ts := newTestServer(t, testConfig(t), NewMemStore())
	u := ts.URL + "/project/" + p1 + "/doc/" + d1

	// doc not found → 404 "Not Found" (status text)
	resp, body := do(t, "GET", u+"/deleted", nil)
	if resp.StatusCode != 404 || body != "Not Found" {
		t.Fatalf("isdeleted 404: %d %q", resp.StatusCode, body)
	}

	resp, body = do(t, "GET", u+"/peek", nil)
	if resp.StatusCode != 404 || body != "Not Found" {
		t.Fatalf("peek 404: %d %q", resp.StatusCode, body)
	}

	resp, body = do(t, "GET", u+"/raw", nil)
	if resp.StatusCode != 404 || body != "Not Found" {
		t.Fatalf("raw 404: %d %q", resp.StatusCode, body)
	}
}

func TestHealthCheckUnsetProject(t *testing.T) {
	cfg := testConfig(t)
	// HEALTH_CHECK_PROJECT_ID unset → ObjectId throws → 500.
	ts := newTestServer(t, cfg, NewMemStore())
	resp, body := get(t, ts.URL+"/health_check")
	if resp.StatusCode != 500 || body != "Internal Server Error" {
		t.Fatalf("health unset: %d %q", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("health unset ct: %q", ct)
	}
}

func TestHealthCheckRoundTrip(t *testing.T) {
	cfg := testConfig(t)
	cfg.HealthCheckProjectID = p2
	store := NewMemStore()
	ts := newTestServer(t, cfg, store)
	resp, body := get(t, ts.URL+"/health_check")
	if resp.StatusCode != 200 || body != "OK" {
		t.Fatalf("health: %d %q", resp.StatusCode, body)
	}
	// the smoke doc must be cleaned up (finally)
	ctx := context.Background()
	docs, _ := store.ProjectDocs(ctx, p2, ProjectDocOpts{IncludeDeleted: true})
	if len(docs) != 0 {
		t.Fatalf("smoke doc not cleaned: %d docs", len(docs))
	}
}

// rev/version semantics (Node 1:1): identical rewrite is a 200 no-op
// (modified:false, rev unchanged); a rev-guarded upsert against the wrong rev
// fails with ErrDocRevValue (the 409 path).
func TestRevSemantics(t *testing.T) {
	store := NewMemStore()
	ts := newTestServer(t, testConfig(t), store)
	u := ts.URL + "/project/" + p1 + "/doc" + "/" + d1

	resp, body := post(t, u, map[string]any{"lines": []any{"a"}, "version": 1, "ranges": map[string]any{}})
	if resp.StatusCode != 200 || body != `{"modified":true,"rev":1}` {
		t.Fatalf("seed: %d %q", resp.StatusCode, body)
	}

	// advance
	resp, body = post(t, u, map[string]any{"lines": []any{"x"}, "version": 2, "ranges": map[string]any{}})
	if resp.StatusCode != 200 || body != `{"modified":true,"rev":2}` {
		t.Fatalf("update: %d %q", resp.StatusCode, body)
	}

	// identical write → 200 modified:false, rev unchanged
	resp, body = post(t, u, map[string]any{"lines": []any{"x"}, "version": 2, "ranges": map[string]any{}})
	if resp.StatusCode != 200 || body != `{"modified":false,"rev":2}` {
		t.Fatalf("noop write: %d %q", resp.StatusCode, body)
	}

	// store: rev-guarded upsert with the wrong previousRev → ErrDocRevValue
	if err := store.UpsertDoc(context.Background(), p1, d1, 99, WriteUpdates{Lines: int64Slice([]string{"z"})}); err != ErrDocRevValue {
		t.Fatalf("upsert wrong rev: got %v (want ErrDocRevValue)", err)
	}
	// ...and with the right one → success, rev+1
	if err := store.UpsertDoc(context.Background(), p1, d1, 2, WriteUpdates{Lines: int64Slice([]string{"z"})}); err != nil {
		t.Fatalf("upsert right rev: %v", err)
	}

	// restore with wrong rev → ErrDocRevValue
	if err := store.RestoreArchivedDoc(context.Background(), p1, d1, []string{"a"}, nil, 7); err != ErrDocRevValue {
		t.Fatalf("restore wrong rev: got %v (want ErrDocRevValue)", err)
	}
}

func int64Slice(s []string) *[]string { return &s }

// normalizeTree must convert driver primitive.D/A (and nested ObjectIDs,
// dates) into the neutral tree the handlers and JSON writer expect — this
// is what makes a mongo-backed doc indistinguishable from the memstore one.
func TestNormalizeTreeDriverShapes(t *testing.T) {
	v := primitive.D{
		{Key: "comments", Value: primitive.A{
			primitive.D{{Key: "op", Value: primitive.D{{Key: "t", Value: newTestObjectID()}}}},
		}},
	}
	n := normalizeTree(v)
	m, ok := n.(map[string]any)
	if !ok {
		t.Fatalf("normalize top: %T", n)
	}
	arr, ok := m["comments"].([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("comments: %T %#v", m["comments"], m["comments"])
	}
	op, ok := arr[0].(map[string]any)["op"].(map[string]any)
	if !ok {
		t.Fatalf("op: %#v", arr[0])
	}
	if got, ok := op["t"].(objectIDHex); !ok || got != objectIDHex(newTestObjectID().Hex()) {
		t.Fatalf("t: %#v", op["t"])
	}
}

func newTestObjectID() primitive.ObjectID {
	b, _ := hex.DecodeString("65bc1e66bf88ccab32196bb6")
	return primitive.ObjectID(b)
}
