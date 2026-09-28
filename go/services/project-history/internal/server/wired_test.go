package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ollitex/go/services/project-history/internal/config"
	"ollitex/go/services/project-history/internal/errrecorder"
	"ollitex/go/services/project-history/internal/flushmanager"
	"ollitex/go/services/project-history/internal/httpcontroller"
	"ollitex/go/services/project-history/internal/redismanager"
	"ollitex/go/services/project-history/internal/retrymanager"
	"ollitex/go/services/project-history/internal/snapshotmanager"
	"ollitex/go/services/project-history/internal/summarizedupdatesmanager"
	"ollitex/go/services/project-history/internal/syncmanager"
)

// ---- minimal fakes (httpcontroller seams) -----------------------------------

type wf struct {
	resync []string
}

func (f *wf) ProcessUpdatesForProject(ctx context.Context, p string) error {
	return nil
}
func (f *wf) ProcessSingleUpdateForProject(ctx context.Context, p string) error {
	return nil
}
func (f *wf) ProcessUpdatesForProjectUsingBisect(ctx context.Context, p string, batch int) error {
	return nil
}
func (f *wf) GetRawUpdates(ctx context.Context, p string, n int) (map[string]any, error) {
	return map[string]any{"project_id": p, "chunk": map[string]any{}, "updates": []any{}}, nil
}
func (f *wf) FlushResyncUpdates(ctx context.Context, p string) error { return nil }

type webF struct{}

func (webF) GetHistoryId(ctx context.Context, p string) (string, error) { return "hid", nil }

type snapF struct{}

func (snapF) GetFileSnapshotStream(ctx context.Context, p string, v int, name string) ([]byte, error) {
	return []byte("BODY"), nil
}
func (snapF) GetRangesSnapshot(ctx context.Context, p string, v int, name string) (map[string]any, error) {
	return map[string]any{"changes": 1}, nil
}
func (snapF) GetFileMetadataSnapshot(ctx context.Context, p string, v int, name string) (map[string]any, error) {
	return map[string]any{"editable": true}, nil
}
func (snapF) GetProjectSnapshot(ctx context.Context, p string, v int) (map[string]any, error) {
	return map[string]any{"files": map[string]any{}}, nil
}
func (snapF) GetPathsAtVersion(ctx context.Context, p string, v int) (map[string]any, error) {
	return map[string]any{"paths": []any{}}, nil
}
func (snapF) GetLatestSnapshotFull(ctx context.Context, p, h string) (*snapshotmanager.Snapshot, int, error) {
	return &snapshotmanager.Snapshot{ProjectVersion: "9"}, 1, nil
}
func (snapF) GetChangesInChunkSince(ctx context.Context, p, h string, since int) (int, []map[string]any, error) {
	return 1, nil, nil
}

type syncF struct {
	starts []string
}

func (f *syncF) GetResyncState(ctx context.Context, p string) (*syncmanager.SyncState, error) {
	return &syncmanager.SyncState{}, nil
}
func (f *syncF) CloneResyncState(ctx context.Context, s, t string) error { return nil }
func (f *syncF) StartResync(ctx context.Context, p string, o map[string]any) error {
	f.starts = append(f.starts, "soft")
	return nil
}
func (f *syncF) StartHardResync(ctx context.Context, p string, o map[string]any) error {
	f.starts = append(f.starts, "hard")
	return nil
}
func (f *syncF) ClearResyncState(ctx context.Context, p string) error { return nil }

type apiF struct{ use bool }

func (f apiF) ShouldUseProjectHistory(ctx context.Context, p string) (bool, error) {
	return f.use, nil
}

type lblF struct{}

func (lblF) GetLabels(ctx context.Context, p string) ([]map[string]any, error) {
	return []map[string]any{{"id": "L"}}, nil
}
func (lblF) CreateLabel(ctx context.Context, p, u string, v int, c string, at any, ve bool) (map[string]any, error) {
	return map[string]any{"id": "new"}, nil
}
func (lblF) DeleteLabelForUser(ctx context.Context, p, u, l string) error { return nil }
func (lblF) DeleteLabel(ctx context.Context, p, l string) error           { return nil }
func (lblF) TransferLabels(ctx context.Context, a, b string) error        { return nil }
func (lblF) CloneLabels(ctx context.Context, s, t string) error           { return nil }

type retryF struct{}

func (retryF) RetryFailures(ctx context.Context, o retrymanager.Options) (*retrymanager.BatchResult, error) {
	return &retrymanager.BatchResult{Succeeded: []string{"x"}}, nil
}

type flushF struct{}

func (flushF) FlushOldOps(ctx context.Context, o flushmanager.Options) (*flushmanager.FlushResult, error) {
	return &flushmanager.FlushResult{Success: []string{"x"}}, nil
}

type erStoreF struct{}

func (erStoreF) FindOneAndUpdate(ctx context.Context, filter, update map[string]any, retAfter bool, projection map[string]any) (map[string]any, error) {
	return nil, nil
}
func (erStoreF) DeleteOne(ctx context.Context, filter map[string]any) (int64, error) {
	return 0, nil
}
func (erStoreF) UpdateOne(ctx context.Context, filter, update map[string]any, upsert bool) error {
	return nil
}
func (erStoreF) FindAll(ctx context.Context) ([]map[string]any, error) { return nil, nil }
func (erStoreF) FindOne(ctx context.Context, filter map[string]any, projection map[string]any) (map[string]any, error) {
	return map[string]any{"attempts": 1}, nil
}
func (erStoreF) InsertOne(ctx context.Context, doc map[string]any) error { return nil }

// ---- tests -------------------------------------------------------------------

type sumF struct{}

func (sumF) GetSummarizedProjectUpdates(ctx context.Context, p string, o summarizedupdatesmanager.Options) ([]map[string]any, int, error) {
	return []map[string]any{}, 0, nil
}

type diffF struct{}

func (diffF) GetDiff(ctx context.Context, p, name string, a, b int) (any, error) {
	return map[string]any{"d": 1}, nil
}
func (diffF) GetFileTreeDiff(ctx context.Context, p string, a, b int) (any, error) {
	return map[string]any{"t": 1}, nil
}

type hsmF struct{}

func (hsmF) InitializeProject(ctx context.Context, h *string) (string, error) {
	return "id", nil
}
func (hsmF) CloneProject(ctx context.Context, s, t string) ([]byte, error) {
	return []byte("C"), nil
}
func (hsmF) GetMostRecentVersion(ctx context.Context, p, h string) (int, map[string]any, map[string]any, map[string]any, error) {
	return 1, nil, nil, nil, nil
}
func (hsmF) GetProjectBlobStream(ctx context.Context, h, b string) ([]byte, error) {
	return []byte("B"), nil
}

type hcF struct{}

func (hcF) Check(ctx context.Context) error             { return nil }
func (hcF) CheckLock(ctx context.Context) (bool, error) { return true, nil }

func wiredEnv(t *testing.T) (http.Handler, *httpcontroller.Deps) {
	t.Helper()
	d := &httpcontroller.Deps{
		UP:                 &wf{},
		SUM:                &sumF{},
		DIFF:               &diffF{},
		HSM:                &hsmF{},
		WEB:                webF{},
		SNAP:               snapF{},
		HC:                 &hcF{},
		SYNC:               &syncF{},
		ER:                 &errrecorder.Deps{Store: erStoreF{}},
		LBL:                lblF{},
		API:                apiF{use: true},
		RETRY:              retryF{},
		FLUSH:              flushF{},
		RedisReadBatchSize: 500,
	}
	fakeRD := &fr{}
	d.RED = redismanager.New(fakeRD, config.KeySchema{})
	c := httpcontroller.New(context.Background(), d)
	return (&Server{}).NewWiredServer(&Deps{C: c}), nil
}

func TestWiredStatusAnd404(t *testing.T) {
	h, _ := wiredEnv(t)
	r := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/status", nil)
	h.ServeHTTP(r, req)
	if r.Code != 200 || strings.TrimSpace(r.Body.String()) != "project-history is up" {
		t.Fatalf("want status text, got %d %q", r.Code, r.Body.String())
	}
	r = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/nope", nil)
	h.ServeHTTP(r, req)
	if r.Code != 404 {
		t.Fatalf("want 404, got %d", r.Code)
	}
}

func TestWiredFlush204(t *testing.T) {
	h, _ := wiredEnv(t)
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/project/P/flush", nil)
	h.ServeHTTP(r, req)
	if r.Code != 204 {
		t.Fatalf("want 204, got %d", r.Code)
	}
}

func TestWiredLabelsJSON(t *testing.T) {
	h, _ := wiredEnv(t)
	r := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/project/P/labels", nil)
	h.ServeHTTP(r, req)
	if r.Code != 200 {
		t.Fatalf("want 200, got %d", r.Code)
	}
	var labels []map[string]any
	if err := json.NewDecoder(r.Body).Decode(&labels); err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || labels[0]["id"] != "L" {
		t.Fatalf("want label L, got %v", labels)
	}
}

func TestWiredResyncSoft(t *testing.T) {
	h, _ := wiredEnv(t)
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/project/P/resync", strings.NewReader("{}"))
	h.ServeHTTP(r, req)
	if r.Code != 204 {
		t.Fatalf("want 204, got %d", r.Code)
	}
}

func TestWiredFileSnapshotBody(t *testing.T) {
	h, _ := wiredEnv(t)
	r := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/project/P/version/2/a.tex", nil)
	h.ServeHTTP(r, req)
	if r.Code != 200 {
		t.Fatalf("want 200, got %d", r.Code)
	}
	b, _ := io.ReadAll(r.Result().Body)
	if string(b) != "BODY" {
		t.Fatalf("want BODY, got %q", b)
	}
}

type fr struct{}

func (fr) Get(k string) (string, bool, error)              { return "", false, nil }
func (fr) Set(k, v string, ttl ...int) error               { return nil }
func (fr) SetNX(k, v string) (bool, error)                 { return true, nil }
func (fr) SetNXWithTTL(k, v string, ttl int) (bool, error) { return true, nil }
func (fr) Del(keys ...string) (int64, error)               { return 0, nil }
func (fr) Exists(keys ...string) (int, error)              { return 0, nil }
func (fr) Expire(k string, ttl int) error                  { return nil }
func (fr) LRange(k string, a, b int) ([]string, error)     { return nil, nil }
func (fr) LRem(k string, n int, v string) (int64, error)   { return 0, nil }
func (fr) LLen(k string) (int64, error)                    { return 0, nil }
func (fr) Scan(pattern string, limit int) ([]string, error) {
	return nil, nil
}
func (fr) MGet(keys ...string) ([]string, error) { return nil, nil }
func (fr) Ping() error                           { return nil }
func (fr) Close() error                          { return nil }

var _ = time.Now

// ---- route battery (every remaining Router.js path) -----------------------------

func doReq(t *testing.T, h http.Handler, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, reader)
	h.ServeHTTP(r, req)
	return r
}

func TestWiredRouteBattery(t *testing.T) {
	h, _ := wiredEnv(t)

	cases := []struct {
		method string
		target string
		body   string
		want   int
	}{
		{"POST", "/project", `{"historyId":"h9"}`, 200},            // initialize
		{"DELETE", "/project/P", "", 204},                          // delete (fake redis ok)
		{"GET", "/project/P/snapshot", "", 200},                    // latest snapshot
		{"GET", "/project/P/diff?pathname=a&from=1&to=2", "", 200}, // diff
		{"GET", "/project/P/diff?pathname=a", "", 400},             // missing ints → 400 (badRequest)
		{"GET", "/project/P/filetree/diff?from=1&to=2", "", 200},   // filetree diff
		{"GET", "/project/P/updates", "", 200},                     // updates
		{"GET", "/project/P/changes-in-chunk?since=1", "", 200},    // changes-in-chunk
		{"GET", "/project/P/version", "", 200},                     // latest version
		{"POST", "/project/P/flush?debug=true", "", 204},           // flush debug
		{"POST", "/project/P/flush?bisect=true", "", 204},          // flush bisect
		{"GET", "/project/P/resync-pending", "", 200},              // resync pending
		{"GET", "/project/P/debug-info", "", 200},                  // debug info
		{"POST", "/project/P/resync?force=true&recoverCorruptedFiles=true", `{"origin":{"kind":"k"},"historyRangesMigration":"forwards"}`, 204},
		{"GET", "/project/P/dump?count=2", "", 200},                       // dump
		{"POST", "/project/P/labels", `{"version":1,"comment":"c"}`, 200}, // create label
		{"DELETE", "/project/P/user/U/labels/L", "", 204},                 // delete label for user
		{"DELETE", "/project/P/labels/L", "", 204},                        // delete label
		{"POST", "/user/A/labels/transfer/B", "", 204},                    // transfer
		{"GET", "/project/P/version/3/a.tex", "", 200},                    // file snapshot
		{"GET", "/project/P/version/3", "", 200},                          // project snapshot
		{"GET", "/project/P/ranges/version/3/a.tex", "", 200},             // ranges
		{"GET", "/project/P/metadata/version/3/a.tex", "", 200},           // metadata
		{"GET", "/project/P/paths/version/3", "", 200},                    // paths
		{"POST", "/project/P/force?clear=true", "", 200},                  // force debug
		{"GET", "/project/H/blob/HASH", "", 200},                          // blob
		{"POST", "/project/P/clone", `{"targetProjectId":"D"}`, 200},      // clone
		{"GET", "/status/failures", "", 200},                              // failures
		{"GET", "/status/failures-full", "", 200},                         // failures full
		{"GET", "/status/queue", "", 200},                                 // queue
		{"POST", "/retry/failures?failureType=soft", "", 200},             // retry
		{"POST", "/retry/failures?callbackUrl=http://cb", "", 200},        // retry background
		{"POST", "/flush/old?maxAge=60&limit=5", "", 200},                 // flush old
		{"GET", "/health_check", "", 200},                                 // health
		{"GET", "/check_lock", "", 200},                                   // lock
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			rec := doReq(t, h, tc.method, tc.target, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("want %d, got %d (body %q)", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}
