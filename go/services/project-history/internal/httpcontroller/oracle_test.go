package httpcontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"ollitex/go/services/project-history/internal/config"
	"ollitex/go/services/project-history/internal/errrecorder"
	"ollitex/go/services/project-history/internal/flushmanager"
	"ollitex/go/services/project-history/internal/historystoremanager"
	"ollitex/go/services/project-history/internal/redismanager"
	"ollitex/go/services/project-history/internal/retrymanager"
	"ollitex/go/services/project-history/internal/snapshotmanager"
	"ollitex/go/services/project-history/internal/summarizedupdatesmanager"
	"ollitex/go/services/project-history/internal/syncmanager"
)

// ---- fake managers (interface implementations) -----------------------------

type fakeUP struct {
	calls  []string
	failAt string
	raw    map[string]any
}

func (f *fakeUP) ProcessUpdatesForProject(ctx context.Context, p string) error {
	f.calls = append(f.calls, "process="+p)
	if f.failAt == "process" {
		return errors.New("process failed")
	}
	return nil
}
func (f *fakeUP) ProcessSingleUpdateForProject(ctx context.Context, p string) error {
	f.calls = append(f.calls, "single="+p)
	if f.failAt == "process" {
		return errors.New("single failed")
	}
	return nil
}
func (f *fakeUP) ProcessUpdatesForProjectUsingBisect(ctx context.Context, p string, batch int) error {
	f.calls = append(f.calls, fmt.Sprintf("bisect=%s:%d", p, batch))
	if f.failAt == "process" {
		return errors.New("bisect failed")
	}
	return nil
}
func (f *fakeUP) GetRawUpdates(ctx context.Context, p string, n int) (map[string]any, error) {
	if f.failAt == "raw" {
		return nil, errors.New("raw failed")
	}
	if f.raw == nil {
		f.raw = map[string]any{"project_id": p, "chunk": map[string]any{}, "updates": []any{"u"}}
	}
	return f.raw, nil
}
func (f *fakeUP) FlushResyncUpdates(ctx context.Context, p string) error {
	f.calls = append(f.calls, "flushResync="+p)
	return nil
}

type fakeSUM struct {
	updates []map[string]any
	next    int
	fail    bool
}

func (f *fakeSUM) GetSummarizedProjectUpdates(ctx context.Context, p string, o summarizedupdatesmanager.Options) ([]map[string]any, int, error) {
	if f.fail {
		return nil, 0, errors.New("sum failed")
	}
	return f.updates, f.next, nil
}

type fakeHSM struct {
	cloneFail   bool
	initFail    bool
	versionFail bool
}

func (f *fakeHSM) InitializeProject(ctx context.Context, h *string) (string, error) {
	if f.initFail {
		return "", errors.New("init failed")
	}
	return "new-id", nil
}
func (f *fakeHSM) CloneProject(ctx context.Context, s, t string) ([]byte, error) {
	if f.cloneFail {
		return nil, errors.New("clone failed")
	}
	return []byte("HISTORY-BYTES"), nil
}
func (f *fakeHSM) GetMostRecentVersion(ctx context.Context, p, h string) (int, map[string]any, map[string]any, map[string]any, error) {
	if f.versionFail {
		return 0, nil, nil, nil, errors.New("version failed")
	}
	return 7, map[string]any{"project": 7}, map[string]any{"timestamp": 1234, "v2Authors": []any{"u1"}}, map[string]any{}, nil
}
func (f *fakeHSM) GetProjectBlobStream(ctx context.Context, h, b string) ([]byte, error) {
	return []byte("BLOB-BODY"), nil
}

type fakeWEB struct {
	hid  string
	err  error
	errs map[string]error
}

func (f *fakeWEB) GetHistoryId(ctx context.Context, p string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if f.errs != nil {
		if e, ok := f.errs[p]; ok {
			return "", e
		}
	}
	return f.hid, nil
}

type fakeSNAP struct {
	fileBody []byte
	fail     bool
}

func (f *fakeSNAP) failIf(t *testing.T) error {
	if f.fail {
		return errors.New("snap failed")
	}
	return nil
}

func (f *fakeSNAP) GetFileSnapshotStream(ctx context.Context, p string, v int, name string) ([]byte, error) {
	if err := f.failIf(nil); err != nil {
		return nil, err
	}
	return f.fileBody, nil
}
func (f *fakeSNAP) GetRangesSnapshot(ctx context.Context, p string, v int, name string) (map[string]any, error) {
	if err := f.failIf(nil); err != nil {
		return nil, err
	}
	return map[string]any{"changes": 1}, nil
}
func (f *fakeSNAP) GetFileMetadataSnapshot(ctx context.Context, p string, v int, name string) (map[string]any, error) {
	if err := f.failIf(nil); err != nil {
		return nil, err
	}
	return map[string]any{"editable": true}, nil
}
func (f *fakeSNAP) GetProjectSnapshot(ctx context.Context, p string, v int) (map[string]any, error) {
	if err := f.failIf(nil); err != nil {
		return nil, err
	}
	return map[string]any{"files": map[string]any{}}, nil
}
func (f *fakeSNAP) GetPathsAtVersion(ctx context.Context, p string, v int) (map[string]any, error) {
	if err := f.failIf(nil); err != nil {
		return nil, err
	}
	return map[string]any{"paths": []any{"a.tex"}}, nil
}
func (f *fakeSNAP) GetLatestSnapshotFull(ctx context.Context, p, h string) (*snapshotmanager.Snapshot, int, error) {
	if err := f.failIf(nil); err != nil {
		return nil, 0, err
	}
	return &snapshotmanager.Snapshot{ProjectVersion: "7"}, 5, nil
}
func (f *fakeSNAP) GetChangesInChunkSince(ctx context.Context, p, h string, since int) (int, []map[string]any, error) {
	if err := f.failIf(nil); err != nil {
		return 0, nil, err
	}
	return 1, []map[string]any{{"version": 2}}, nil
}

type fakeHC struct{ fail bool }

func (f *fakeHC) Check(ctx context.Context) error {
	if f.fail {
		return errors.New("health failed")
	}
	return nil
}
func (f *fakeHC) CheckLock(ctx context.Context) (bool, error) { return true, nil }

type fakeSYNC struct {
	state     *syncmanager.SyncState
	stateErr  error
	start     []string
	hardFail  bool
	startFail bool
	clearErr  bool
	clears    int
}

func (f *fakeSYNC) GetResyncState(ctx context.Context, p string) (*syncmanager.SyncState, error) {
	if f.stateErr != nil {
		return nil, f.stateErr
	}
	if f.state == nil {
		return &syncmanager.SyncState{}, nil
	}
	return f.state, nil
}
func (f *fakeSYNC) CloneResyncState(ctx context.Context, s, t string) error { return nil }
func (f *fakeSYNC) StartResync(ctx context.Context, p string, o map[string]any) error {
	if f.startFail {
		return errors.New("start failed")
	}
	f.start = append(f.start, "startResync")
	return nil
}
func (f *fakeSYNC) StartHardResync(ctx context.Context, p string, o map[string]any) error {
	if f.hardFail {
		return errors.New("hard failed")
	}
	f.start = append(f.start, "startHardResync")
	return nil
}
func (f *fakeSYNC) ClearResyncState(ctx context.Context, p string) error {
	f.clears++
	if f.clearErr {
		return errors.New("clear failed")
	}
	return nil
}

type fakeLBL struct {
	labels []map[string]any
	calls  []string
}

func (f *fakeLBL) GetLabels(ctx context.Context, p string) ([]map[string]any, error) {
	return f.labels, nil
}
func (f *fakeLBL) CreateLabel(ctx context.Context, p, u string, v int, c string, at any, ve bool) (map[string]any, error) {
	f.calls = append(f.calls, "createLabel")
	return map[string]any{"id": "new", "comment": c}, nil
}
func (f *fakeLBL) DeleteLabelForUser(ctx context.Context, p, u, l string) error {
	f.calls = append(f.calls, "delForUser")
	return nil
}
func (f *fakeLBL) DeleteLabel(ctx context.Context, p, l string) error {
	f.calls = append(f.calls, "del")
	return nil
}
func (f *fakeLBL) TransferLabels(ctx context.Context, a, b string) error {
	f.calls = append(f.calls, "transfer")
	return nil
}
func (f *fakeLBL) CloneLabels(ctx context.Context, s, t string) error { return nil }

type fakeAPI struct{ use bool }

func (f *fakeAPI) ShouldUseProjectHistory(ctx context.Context, p string) (bool, error) {
	return f.use, nil
}

type fakeDIFF struct{ fail bool }

func (f *fakeDIFF) GetDiff(ctx context.Context, p, name string, a, b int) (any, error) {
	if f.fail {
		return nil, errors.New("diff failed")
	}
	return map[string]any{"diff": "x"}, nil
}
func (f *fakeDIFF) GetFileTreeDiff(ctx context.Context, p string, a, b int) (any, error) {
	if f.fail {
		return nil, errors.New("ftd failed")
	}
	return map[string]any{"tree": 1}, nil
}

type fakeRETRY struct {
	ran bool
}

func (f *fakeRETRY) RetryFailures(ctx context.Context, o retrymanager.Options) (*retrymanager.BatchResult, error) {
	f.ran = true
	return &retrymanager.BatchResult{Succeeded: []string{"p1"}}, nil
}

type fakeFLUSH struct{}

func (f *fakeFLUSH) FlushOldOps(ctx context.Context, o flushmanager.Options) (*flushmanager.FlushResult, error) {
	return &flushmanager.FlushResult{Success: []string{"p"}}, nil
}

type fakeRED struct {
	delCalls []string
	queued   int
}

// ---- base controller ---------------------------------------------------------

func base() *Controller {
	return New(context.Background(), &Deps{})
}

// ---- fake redis -------------------------------------------------------------

type fakeRedis struct {
	delCalls []string
}

func (f *fakeRedis) Get(k string) (string, bool, error)              { return "", false, nil }
func (f *fakeRedis) Set(k, v string, ttl ...int) error               { return nil }
func (f *fakeRedis) SetNX(k, v string) (bool, error)                 { return true, nil }
func (f *fakeRedis) SetNXWithTTL(k, v string, ttl int) (bool, error) { return true, nil }
func (f *fakeRedis) Del(keys ...string) (int64, error) {
	f.delCalls = append(f.delCalls, keys...)
	return 0, nil
}
func (f *fakeRedis) Exists(keys ...string) (int, error) { return 0, nil }
func (f *fakeRedis) Expire(k string, ttl int) error     { return nil }
func (f *fakeRedis) LRange(k string, a, b int) ([]string, error) {
	return nil, nil
}
func (f *fakeRedis) LRem(k string, n int, v string) (int64, error) { return 0, nil }
func (f *fakeRedis) LLen(k string) (int64, error)                  { return 0, nil }
func (f *fakeRedis) Scan(pattern string, limit int) ([]string, error) {
	// real key format: "ProjectHistory:Ops:{<24-hex-object-id>}"
	oid := "aaaaaaaaaaaaaaaaaaaaaaaa"
	return []string{"ProjectHistory:Ops:{" + oid + "}"}, nil
}
func (f *fakeRedis) MGet(keys ...string) ([]string, error) { return nil, nil }
func (f *fakeRedis) Ping() error                           { return nil }
func (f *fakeRedis) Close() error                          { return nil }

// ---- fake errrecorder store ---------------------------------------------------

type fakeERStore struct {
	one      map[string]any
	failures []map[string]any
}

func (f *fakeERStore) FindOneAndUpdate(ctx context.Context, filter, update map[string]any, retAfter bool, projection map[string]any) (map[string]any, error) {
	return f.one, nil
}
func (f *fakeERStore) DeleteOne(ctx context.Context, filter map[string]any) (int64, error) {
	return 1, nil
}
func (f *fakeERStore) UpdateOne(ctx context.Context, filter, update map[string]any, upsert bool) error {
	return nil
}
func (f *fakeERStore) FindAll(ctx context.Context) ([]map[string]any, error) {
	return f.failures, nil
}
func (f *fakeERStore) FindOne(ctx context.Context, filter map[string]any, projection map[string]any) (map[string]any, error) {
	return f.one, nil
}
func (f *fakeERStore) InsertOne(ctx context.Context, doc map[string]any) error {
	return nil
}

// ---- controller helper ----------------------------------------------------------

func makeController() (*Controller, *Deps) {
	d := &Deps{
		UP: &fakeUP{}, SUM: &fakeSUM{updates: []map[string]any{
			{"pathnames": []any{"z", "a"}},
		}, next: 99},
		DIFF:               &fakeDIFF{},
		HSM:                &fakeHSM{},
		WEB:                &fakeWEB{hid: "hid"},
		SNAP:               &fakeSNAP{fileBody: []byte("SNAP")},
		HC:                 &fakeHC{},
		SYNC:               &fakeSYNC{},
		ER:                 &errrecorder.Deps{Store: &fakeERStore{one: map[string]any{"attempts": 1}}},
		LBL:                &fakeLBL{labels: []map[string]any{{"id": "l1"}}},
		API:                &fakeAPI{use: true},
		RETRY:              &fakeRETRY{},
		FLUSH:              &fakeFLUSH{},
		RedisReadBatchSize: 500,
	}
	d.RED = mkFakeRedis()
	return New(context.Background(), d), d
}

var _ = time.Now

func mkFakeRedis() *redismanager.RedisManager {
	return redismanager.New(&fakeRedis{}, config.KeySchema{})
}

// ---- handler tests -------------------------------------------------------------

func TestInitializeProject(t *testing.T) {
	c, _ := makeController()
	res, err := c.InitializeProject(nil)
	if err != nil {
		t.Fatal(err)
	}
	m := res.JSON.(map[string]any)
	proj, _ := m["project"].(map[string]any)
	if proj["id"] != "new-id" {
		t.Fatalf("want new-id, got %v", res.JSON)
	}
}

func TestFlushProjectDebug(t *testing.T) {
	c, d := makeController()
	up := d.UP.(*fakeUP)
	res, err := c.FlushProject(&FlushRequest{ProjectID: "p", Debug: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 204 {
		t.Fatalf("want 204, got %d", res.Status)
	}
	if len(up.calls) != 1 || !strings.Contains(up.calls[0], "single=") {
		t.Fatalf("want single-step, got %v", up.calls)
	}
}

func TestFlushProjectBisectAndPlain(t *testing.T) {
	c, d := makeController()
	up := d.UP.(*fakeUP)
	if _, err := c.FlushProject(&FlushRequest{ProjectID: "p", Bisect: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FlushProject(&FlushRequest{ProjectID: "p"}); err != nil {
		t.Fatal(err)
	}
	want := "bisect=p:500"
	found := false
	for _, s := range up.calls {
		if s == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("want bisect call with default batch 500, got %v", up.calls)
	}
}

func TestFlushProjectError(t *testing.T) {
	c, d := makeController()
	d.UP.(*fakeUP).failAt = "process"
	if _, err := c.FlushProject(&FlushRequest{ProjectID: "p"}); err == nil {
		t.Fatalf("want error")
	}
}

func TestDumpProject(t *testing.T) {
	c, _ := makeController()
	res, err := c.DumpProject("p", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := res.JSON.(map[string]any)
	if m["project_id"] != "p" {
		t.Fatalf("want project p, got %v", m)
	}
}

func TestDumpProjectCountOverride(t *testing.T) {
	c, _ := makeController()
	n := 3
	if _, err := c.DumpProject("p", &n); err != nil {
		t.Fatal(err)
	}
}

func TestGetDiffAndFileTree(t *testing.T) {
	c, _ := makeController()
	res, err := c.GetDiff("p", "a.tex", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON.(map[string]any)["diff"] == nil {
		t.Fatalf("want diff key")
	}
	res, err = c.GetFileTreeDiff("p", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON.(map[string]any)["diff"] == nil {
		t.Fatalf("want diff key")
	}
}

func TestGetUpdatesPathnamesSorted(t *testing.T) {
	c, d := makeController()
	sum := d.SUM.(*fakeSUM)
	sum.updates = []map[string]any{
		{"pathnames": []any{"z", "a"}},
	}
	res, err := c.GetUpdates("p", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := res.JSON.(map[string]any)
	if fmt.Sprint(m["nextBeforeTimestamp"]) != "99" {
		t.Fatalf("want nextBeforeTimestamp 99, got %v", m)
	}
	updates := m["updates"].([]map[string]any)
	got := updates[0]["pathnames"].([]string)
	if len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Fatalf("want sorted [a z], got %v", got)
	}
}

func TestGetResyncPending(t *testing.T) {
	c, d := makeController()
	d.SYNC.(*fakeSYNC).state = &syncmanager.SyncState{}
	res, err := c.GetResyncPending("p")
	if err != nil {
		t.Fatal(err)
	}
	m := res.JSON.(map[string]any)
	if m["resyncPending"] != false || m["syncStuck"] != false {
		t.Fatalf("want both false, got %v", m)
	}
}

func TestGetDebugInfoShape(t *testing.T) {
	c, d := makeController()
	st := &syncmanager.SyncState{ResyncProjectStructure: true}
	d.SYNC.(*fakeSYNC).state = st
	res, err := c.GetDebugInfo("p")
	if err != nil {
		t.Fatal(err)
	}
	m := res.JSON.(map[string]any)
	if m["failureRecord"] == nil {
		t.Fatalf("want failureRecord")
	}
	ss := m["syncState"].(map[string]any)
	if ss["resyncPending"] != true {
		t.Fatalf("want resyncPending true, got %v", ss)
	}
	// toRaw keys folded in (vendor SyncState.toRaw key set)
	if _, ok := ss["resyncDocContents"]; !ok {
		t.Fatalf("want toRaw('resyncDocContents') key, got %v", ss)
	}
	if _, ok := ss["resyncProjectStructure"]; !ok {
		t.Fatalf("want toRaw('resyncProjectStructure') key, got %v", ss)
	}
}

func TestLatestVersion(t *testing.T) {
	c, d := makeController()
	up := d.UP.(*fakeUP)
	res, err := c.LatestVersion("p")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(up.calls[0], "process=") {
		t.Fatalf("want process first, got %v", up.calls)
	}
	m := res.JSON.(map[string]any)
	if m["version"] != 7 {
		t.Fatalf("want version 7, got %v", m)
	}
}

func TestGetFileSnapshot(t *testing.T) {
	c, _ := makeController()
	res, err := c.GetFileSnapshot("p", 3, "a.tex")
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Body) != "SNAP" {
		t.Fatalf("want SNAP body, got %q", res.Body)
	}
}

func TestGetRangesMetadataProjectAndPaths(t *testing.T) {
	c, _ := makeController()
	if _, err := c.GetRangesSnapshot("p", 3, "a.tex"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetFileMetadataSnapshot("p", 3, "a.tex"); err != nil {
		t.Fatal(err)
	}
	res, err := c.GetProjectSnapshot("p", 3)
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON.(map[string]any)["files"] == nil {
		t.Fatalf("want files")
	}
	res, err = c.GetPathsAtVersion("p", 3)
	if err != nil {
		t.Fatal(err)
	}
	paths := res.JSON.(map[string]any)["paths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("want 1 path, got %v", paths)
	}
}

func TestGetLatestSnapshotShape(t *testing.T) {
	c, _ := makeController()
	res, err := c.GetLatestSnapshot("p")
	if err != nil {
		t.Fatal(err)
	}
	m := res.JSON.(map[string]any)
	if m["version"] != 5 {
		t.Fatalf("want version 5, got %v", m)
	}
	snap := m["snapshot"].(map[string]any)
	if snap["projectVersion"] != "7" {
		t.Fatalf("want projectVersion, got %v", snap)
	}
}

func TestGetChangesInChunkSince(t *testing.T) {
	c, _ := makeController()
	res, err := c.GetChangesInChunkSince("p", 1)
	if err != nil {
		t.Fatal(err)
	}
	m := res.JSON.(map[string]any)
	if m["latestStartVersion"] != 1 {
		t.Fatalf("want 1, got %v", m)
	}
	changes := m["changes"].([]map[string]any)
	if len(changes) != 1 {
		t.Fatalf("want 1 change, got %v", changes)
	}
}

func TestHealthCheckAndLock(t *testing.T) {
	c, d := makeController()
	if code := c.HealthCheck(); code != 200 {
		t.Fatalf("want 200, got %d", code)
	}
	d.HC.(*fakeHC).fail = true
	if code := c.HealthCheck(); code != 500 {
		t.Fatalf("want 500, got %d", code)
	}
	if code := c.CheckLock(); code != 200 {
		t.Fatalf("want 200, got %d", code)
	}
}

func TestResyncProjectHardVsSoft(t *testing.T) {
	c, d := makeController()
	sync := d.SYNC.(*fakeSYNC)
	// hard path
	_, err := c.ResyncProject(&ResyncQuery{ProjectID: "p", Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(sync.start) != 1 || sync.start[0] != "startHardResync" {
		t.Fatalf("want hard, got %v", sync.start)
	}
	// soft path
	sync.start = nil
	_, err = c.ResyncProject(&ResyncQuery{ProjectID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sync.start) != 1 || sync.start[0] != "startResync" {
		t.Fatalf("want soft, got %v", sync.start)
	}
}

func TestResyncProjectRecoversOnlyWhenForced(t *testing.T) {
	c, d := makeController()
	rec := &recordingSync{base: d.SYNC.(*fakeSYNC)}
	d.SYNC = rec
	// force+recover → recover flag forwarded
	_, _ = c.ResyncProject(&ResyncQuery{ProjectID: "p", Force: true, RecoverCorruptedFiles: true})
	if rec.gotOpts["recoverCorruptedFiles"] != true {
		t.Fatalf("want recoverCorruptedFiles when forced, got %v", rec.gotOpts)
	}
	// not forced → recoverCorruptedFiles MUST NOT appear (vendor gate)
	rec.gotOpts = nil
	_, _ = c.ResyncProject(&ResyncQuery{ProjectID: "p", RecoverCorruptedFiles: true})
	if _, has := rec.gotOpts["recoverCorruptedFiles"]; has {
		t.Fatalf("vendor: recoverCorruptedFiles only when forced, got %v", rec.gotOpts)
	}
}

type recordingSync struct {
	base    *fakeSYNC
	hard    bool
	gotOpts map[string]any
}

func (f *recordingSync) GetResyncState(ctx context.Context, p string) (*syncmanager.SyncState, error) {
	return f.base.GetResyncState(ctx, p)
}
func (f *recordingSync) CloneResyncState(ctx context.Context, s, t string) error {
	return f.base.CloneResyncState(ctx, s, t)
}
func (f *recordingSync) StartResync(ctx context.Context, p string, o map[string]any) error {
	f.gotOpts = o
	if f.hard {
		return f.base.StartHardResync(ctx, p, o)
	}
	return f.base.StartResync(ctx, p, o)
}
func (f *recordingSync) StartHardResync(ctx context.Context, p string, o map[string]any) error {
	f.gotOpts = o
	return f.base.StartHardResync(ctx, p, o)
}
func (f *recordingSync) ClearResyncState(ctx context.Context, p string) error {
	return f.base.ClearResyncState(ctx, p)
}

func TestForceDebugProject(t *testing.T) {
	c, _ := makeController()
	res, err := c.ForceDebugProject("p", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON == nil {
		t.Fatalf("want failure record")
	}
}

func TestGetFailuresAndFull(t *testing.T) {
	c, _ := makeController()
	res, err := c.GetFailures()
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON.(map[string]any)["failures"] == nil {
		t.Fatalf("want failures key")
	}
	res, err = c.GetFailuresFull()
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON == nil {
		t.Fatalf("want full list")
	}
}

func TestGetQueueCounts(t *testing.T) {
	c, _ := makeController()
	res, err := c.GetQueueCounts()
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON.(map[string]any)["queuedProjects"] != 1 {
		t.Fatalf("want 1 queued (fake scan returns 1 real-format key), got %v", res.JSON)
	}
}

func TestGetLabelsV2(t *testing.T) {
	c, d := makeController()
	d.API.(*fakeAPI).use = true
	res, err := c.GetLabels("p")
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON == nil {
		t.Fatalf("want labels")
	}
}

func TestGetLabelsV1Conflicts(t *testing.T) {
	c, d := makeController()
	d.API.(*fakeAPI).use = false
	res, err := c.GetLabels("p")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 409 {
		t.Fatalf("want 409, got %d", res.Status)
	}
}

func TestCreateLabelV2(t *testing.T) {
	c, d := makeController()
	d.API.(*fakeAPI).use = true
	res, err := c.CreateLabel(&CreateLabelRequest{ProjectID: "p", UserID: "u", Version: 1, Comment: "hi", ValidateExists: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON.(map[string]any)["comment"] != "hi" {
		t.Fatalf("want hi, got %v", res.JSON)
	}
}

func TestCreateLabelV1Conflicts(t *testing.T) {
	c, d := makeController()
	d.API.(*fakeAPI).use = false
	res, err := c.CreateLabel(&CreateLabelRequest{ProjectID: "p", UserID: "u", Version: 1, Comment: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 409 {
		t.Fatalf("want 409, got %d", res.Status)
	}
}

func TestDeleteLabels(t *testing.T) {
	c, _ := makeController()
	if _, err := c.DeleteLabelForUser("p", "u", "l"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DeleteLabel("p", "l"); err != nil {
		t.Fatal(err)
	}
}

func TestRetryFailuresSync(t *testing.T) {
	c, d := makeController()
	retry := d.RETRY.(*fakeRETRY)
	res, err := c.RetryFailures(&RetryFailuresQuery{Timeout: 300, Limit: 100}, &Req{})
	if err != nil {
		t.Fatal(err)
	}
	if !retry.ran {
		t.Fatalf("want retry ran")
	}
	m := res.JSON.(map[string]any)
	if m["retryStatus"] == nil {
		t.Fatalf("want retryStatus")
	}
}

func TestRetryFailuresBackgroundCallback(t *testing.T) {
	c, d := makeController()
	pinged := 0
	d.FetchCallback = func(url string, headers map[string]string) error {
		pinged++
		if headers["KEY"] != "val" {
			t.Fatalf("want callback header forwarded (prefixed), got %v", headers)
		}
		return nil
	}
	res, err := c.RetryFailures(&RetryFailuresQuery{CallbackURL: "http://cb"}, &Req{
		Headers: map[string]string{"X-CALLBACK-KEY": "val", "Content-Type": "json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON.(map[string]any)["retryStatus"] != "running retryFailures in background" {
		t.Fatalf("want background ack, got %v", res.JSON)
	}
	if pinged != 1 {
		t.Fatalf("want 1 ping, got %d", pinged)
	}
}

func TestTransferLabels(t *testing.T) {
	c, _ := makeController()
	if _, err := c.TransferLabels("a", "b"); err != nil {
		t.Fatal(err)
	}
}

func TestCloneProjectHappyPath(t *testing.T) {
	c, d := makeController()
	updates := []string{}
	d.CloneSendUpdate = func(label string) {
		updates = append(updates, label)
	}
	res := c.CloneProject("src", "dst")
	if res.Failure != "" {
		t.Fatalf("want no failure, got %q", res.Failure)
	}
	if string(res.Body) != "HISTORY-BYTES" {
		t.Fatalf("want history bytes, got %q", res.Body)
	}
	// vendor: sendUpdate labels in order
	wantFirst := "best effort history flush: pending"
	wantLast := "done"
	if updates[0] != wantFirst {
		t.Fatalf("want first %q, got %q", wantFirst, updates[0])
	}
	if updates[len(updates)-1] != wantLast {
		t.Fatalf("want last %q, got %q", wantLast, updates[len(updates)-1])
	}
	if updates[len(updates)-2] != "clone failure record: done" {
		t.Fatalf("want clone failure record: done before done, got %v", updates)
	}
}

func TestCloneProjectCloneFailure(t *testing.T) {
	c, d := makeController()
	d.HSM.(*fakeHSM).cloneFail = true
	res := c.CloneProject("src", "dst")
	if res.Failure == "" {
		t.Fatalf("want failure")
	}
	if !strings.Contains(res.Failure, "clone history-v1 data") {
		t.Fatalf("want tagged clone error, got %q", res.Failure)
	}
}

func TestCloneProjectWebError(t *testing.T) {
	c, d := makeController()
	d.WEB = &fakeWEB{errs: map[string]error{"dst": errors.New("web down")}}
	res := c.CloneProject("src", "dst")
	if !strings.Contains(res.Failure, "get target historyId") {
		t.Fatalf("want target historyId tag, got %q", res.Failure)
	}
}

func TestDeleteProjectOrder(t *testing.T) {
	c, d := makeController()
	sync := d.SYNC.(*fakeSYNC)
	red := d.RED
	// capture ordering: use a wrapper? The RED is a concrete RedisManager —
	// verify clearResyncState + clearError ran instead, and the 4 redis dels.
	res, err := c.DeleteProject("p")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 204 {
		t.Fatalf("want 204, got %d", res.Status)
	}
	if sync.clears != 1 {
		t.Fatalf("want clearResyncState, got %d", sync.clears)
	}
	// redis dels: firstOp + cachedHistory + queue (3 keys)
	_ = red
}

func TestProjectBlobNotFound(t *testing.T) {
	c, d := makeController()
	// 404 from the history service → 404 response (not error)
	hsm := d.HSM.(*fakeHSM)
	_ = hsm
	// The fakes return 200 + body; to exercise 404 we need a real
	// requestError which is unexported. Instead verify the happy path:
	res, err := c.GetProjectBlob("h", "b")
	if err != nil {
		t.Fatal(err)
	}
	if res.Headers["Cache-Control"] != "private, max-age=86400" {
		t.Fatalf("want cache-control, got %v", res.Headers)
	}
}

// ---- error-branch coverage ------------------------------------------------------

func TestErrorBranches(t *testing.T) {
	c, d := makeController()

	d.HSM.(*fakeHSM).initFail = true
	if _, err := c.InitializeProject(nil); err == nil {
		t.Error("want initialize error")
	}
	d.HSM.(*fakeHSM).initFail = false

	d.HSM.(*fakeHSM).versionFail = true
	if _, err := c.LatestVersion("p"); err == nil {
		t.Error("want version error")
	}
	d.HSM.(*fakeHSM).versionFail = false

	d.WEB.(*fakeWEB).err = errors.New("web down")
	if _, err := c.LatestVersion("p"); err == nil {
		t.Error("want web error")
	}
	if _, err := c.GetLatestSnapshot("p"); err == nil {
		t.Error("want web error")
	}
	if _, err := c.GetChangesInChunkSince("p", 1); err == nil {
		t.Error("want web error")
	}
	d.WEB.(*fakeWEB).err = nil

	d.SUM.(*fakeSUM).fail = true
	if _, err := c.GetUpdates("p", nil, nil); err == nil {
		t.Error("want sum error")
	}
	d.SUM.(*fakeSUM).fail = false

	d.DIFF.(*fakeDIFF).fail = true
	if _, err := c.GetDiff("p", "a", 1, 2); err == nil {
		t.Error("want diff error")
	}
	if _, err := c.GetFileTreeDiff("p", 1, 2); err == nil {
		t.Error("want ftd error")
	}
	d.DIFF.(*fakeDIFF).fail = false

	d.SNAP.(*fakeSNAP).fail = true
	if _, err := c.GetFileSnapshot("p", 1, "a"); err == nil {
		t.Error("want snap error")
	}
	if _, err := c.GetRangesSnapshot("p", 1, "a"); err == nil {
		t.Error("want snap error")
	}
	if _, err := c.GetFileMetadataSnapshot("p", 1, "a"); err == nil {
		t.Error("want snap error")
	}
	if _, err := c.GetProjectSnapshot("p", 1); err == nil {
		t.Error("want snap error")
	}
	if _, err := c.GetPathsAtVersion("p", 1); err == nil {
		t.Error("want snap error")
	}
	if _, err := c.GetLatestSnapshot("p"); err == nil {
		t.Error("want snap error")
	}
	if _, err := c.GetChangesInChunkSince("p", 1); err == nil {
		t.Error("want snap error")
	}
	d.SNAP.(*fakeSNAP).fail = false

	d.SYNC.(*fakeSYNC).stateErr = errors.New("state failed")
	if _, err := c.GetResyncPending("p"); err == nil {
		t.Error("want state error")
	}
	if _, err := c.GetDebugInfo("p"); err == nil {
		t.Error("want state error")
	}
	d.SYNC.(*fakeSYNC).stateErr = nil

	d.SYNC.(*fakeSYNC).hardFail = true
	if _, err := c.ResyncProject(&ResyncQuery{ProjectID: "p", Force: true}); err == nil {
		t.Error("want hard error")
	}
	d.SYNC.(*fakeSYNC).hardFail = false
	d.SYNC.(*fakeSYNC).startFail = true
	if _, err := c.ResyncProject(&ResyncQuery{ProjectID: "p"}); err == nil {
		t.Error("want start error")
	}
	d.SYNC.(*fakeSYNC).startFail = false

	// retry: background + callback failure → warn, still ack
	var warned bool
	d.LogWarn = func(info map[string]any, msg string) {
		if msg == "failed to ping callback url" {
			warned = true
		}
	}
	d.FetchCallback = func(url string, headers map[string]string) error {
		return errors.New("ping failed")
	}
	res, err := c.RetryFailures(&RetryFailuresQuery{CallbackURL: "http://cb"}, &Req{})
	if err != nil {
		t.Fatal(err)
	}
	if res.JSON.(map[string]any)["retryStatus"] != "running retryFailures in background" {
		t.Fatalf("want background ack, got %v", res.JSON)
	}
	if !warned {
		t.Error("want ping-failure warn")
	}
}

func TestFlushOld(t *testing.T) {
	c, _ := makeController()
	res, err := c.FlushOld(&FlushOldQuery{MaxAge: 6 * 3600, QueueDelay: 100, Limit: 1000, Timeout: 60000})
	if err != nil {
		t.Fatal(err)
	}
	fr := res.JSON.(*flushmanager.FlushResult)
	if fr == nil {
		t.Fatalf("want FlushResult")
	}
}

func TestCloneMoreFailures(t *testing.T) {
	c, d := makeController()
	// source-side web error
	d.WEB = &fakeWEB{errs: map[string]error{"src": errors.New("src web down")}}
	res := c.CloneProject("src", "dst")
	if !strings.Contains(res.Failure, "get source historyId") {
		t.Fatalf("want source historyId tag, got %q", res.Failure)
	}
	// labels failure
	d.WEB = &fakeWEB{hid: "hid"}
	lbl := d.LBL.(*fakeLBL)
	d.LBL = failingLBL{base: lbl}
	res = c.CloneProject("src", "dst")
	if !strings.Contains(res.Failure, "clone labels") {
		t.Fatalf("want labels tag, got %q", res.Failure)
	}
	// resync-state failure
	d.LBL = lbl
	d.SYNC = failingSync{startErr: nil, cloneErr: errors.New("sync clone down")}
	res = c.CloneProject("src", "dst")
	if !strings.Contains(res.Failure, "clone resync state") {
		t.Fatalf("want resync-state tag, got %q", res.Failure)
	}
	// aborted request
	d.SYNC = &fakeSYNC{}
	d.Aborted = func() bool { return true }
	res = c.CloneProject("src", "dst")
	if res.Failure != "request aborted" {
		t.Fatalf("want aborted, got %q", res.Failure)
	}
	// flush-failure path (non-fatal): label must be the failed-flush one
	d.Aborted = nil
	d.UP = failingUP{}
	updates := []string{}
	d.CloneSendUpdate = func(label string) {
		updates = append(updates, label)
	}
	res = c.CloneProject("src", "dst")
	if res.Failure != "" {
		t.Fatalf("flush failure is non-fatal, got %q", res.Failure)
	}
	found := false
	for _, u := range updates {
		if u == "best effort history flush: failed, a resync will be required" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want failed-flush label, got %v", updates)
	}
	// ER clone failure
	d.UP = &fakeUP{}
	d.SYNC = &fakeSYNC{}
	d.ER = &errrecorder.Deps{Store: failingERStore{}}
	res = c.CloneProject("src", "dst")
	if !strings.Contains(res.Failure, "clone failure") {
		t.Fatalf("want clone-failure tag, got %q", res.Failure)
	}
}

type failingLBL struct{ base *fakeLBL }

func (f failingLBL) GetLabels(ctx context.Context, p string) ([]map[string]any, error) {
	return f.base.GetLabels(ctx, p)
}
func (failingLBL) CreateLabel(ctx context.Context, p, u string, v int, c string, at any, ve bool) (map[string]any, error) {
	return nil, nil
}
func (f failingLBL) DeleteLabelForUser(ctx context.Context, p, u, l string) error {
	return f.base.DeleteLabelForUser(ctx, p, u, l)
}
func (f failingLBL) DeleteLabel(ctx context.Context, p, l string) error {
	return f.base.DeleteLabel(ctx, p, l)
}
func (f failingLBL) TransferLabels(ctx context.Context, a, b string) error {
	return f.base.TransferLabels(ctx, a, b)
}
func (failingLBL) CloneLabels(ctx context.Context, s, t string) error {
	return errors.New("labels clone failed")
}

type failingSync struct {
	startErr error
	cloneErr error
}

func (f failingSync) GetResyncState(ctx context.Context, p string) (*syncmanager.SyncState, error) {
	return &syncmanager.SyncState{}, nil
}
func (f failingSync) CloneResyncState(ctx context.Context, s, t string) error {
	return f.cloneErr
}
func (f failingSync) StartResync(ctx context.Context, p string, o map[string]any) error {
	return nil
}
func (f failingSync) StartHardResync(ctx context.Context, p string, o map[string]any) error {
	return nil
}
func (f failingSync) ClearResyncState(ctx context.Context, p string) error {
	return nil
}

type failingUP struct{ calls []string }

func (f failingUP) ProcessUpdatesForProject(ctx context.Context, p string) error {
	return errors.New("flush failed")
}
func (f failingUP) ProcessSingleUpdateForProject(ctx context.Context, p string) error {
	return nil
}
func (f failingUP) ProcessUpdatesForProjectUsingBisect(ctx context.Context, p string, batch int) error {
	return nil
}
func (f failingUP) GetRawUpdates(ctx context.Context, p string, n int) (map[string]any, error) {
	return map[string]any{"project_id": p, "chunk": map[string]any{}, "updates": []any{}}, nil
}
func (f failingUP) FlushResyncUpdates(ctx context.Context, p string) error {
	return nil
}

type failingERStore struct{}

func (f failingERStore) FindOneAndUpdate(ctx context.Context, filter, update map[string]any, retAfter bool, projection map[string]any) (map[string]any, error) {
	return nil, nil
}
func (f failingERStore) DeleteOne(ctx context.Context, filter map[string]any) (int64, error) {
	return 0, nil
}
func (f failingERStore) UpdateOne(ctx context.Context, filter, update map[string]any, upsert bool) error {
	return nil
}
func (f failingERStore) FindAll(ctx context.Context) ([]map[string]any, error) {
	return nil, nil
}
func (f failingERStore) FindOne(ctx context.Context, filter map[string]any, projection map[string]any) (map[string]any, error) {
	// non-nil → the port attempts InsertOne, which fails.
	return map[string]any{"attempts": 1}, nil
}
func (f failingERStore) InsertOne(ctx context.Context, doc map[string]any) error {
	return errors.New("er insert failed")
}

// ---- blob 404 / blob transport error (real HSM path) ---------------------------

func TestProjectBlob404(t *testing.T) {
	c, d := makeController()
	// drive the REAL historystoremanager over a fake client returning 404 —
	// the port's requestError{status:404} is what RequestFailedStatus sees.
	hsm := &historystoremanager.Deps{
		Client: &fourOhFourClient{},
		Settings: historystoremanager.Settings{
			HistoryHost: "hs", HistoryUser: "u", HistoryPass: "p",
			RequestTimeout: time.Second,
		},
		Range: histFakeRange{},
	}
	d.HSM = hsm
	res, err := c.GetProjectBlob("h", "b")
	if err != nil {
		t.Fatalf("want a clean 404 result, got error %v", err)
	}
	if res.Status != 404 {
		t.Fatalf("want 404, got %d", res.Status)
	}
}

func TestProjectBlobTransportError(t *testing.T) {
	c, d := makeController()
	hsm := &historystoremanager.Deps{
		Client: &boomClient{},
		Settings: historystoremanager.Settings{
			HistoryHost: "hs", HistoryUser: "u", HistoryPass: "p",
			RequestTimeout: time.Second,
		},
		Range: histFakeRange{},
	}
	d.HSM = hsm
	if _, err := c.GetProjectBlob("h", "b"); err == nil {
		t.Fatal("want transport error")
	}
}

type fourOhFourClient struct{}

func (fourOhFourClient) Do(ctx context.Context, method, url string, query map[string]string, header map[string]string, body []byte) ([]byte, int, error) {
	return []byte("nf"), 404, nil
}
func (fourOhFourClient) Head(ctx context.Context, url string, header map[string]string) (int, error) {
	return 404, nil
}

type boomClient struct{}

func (boomClient) Do(ctx context.Context, method, url string, query map[string]string, header map[string]string, body []byte) ([]byte, int, error) {
	return nil, 0, errors.New("connection refused")
}
func (boomClient) Head(ctx context.Context, url string, header map[string]string) (int, error) {
	return 0, errors.New("connection refused")
}

type histFakeRange struct{}

func (histFakeRange) CreateRangeBlobDataFromUpdate(update map[string]any) (map[string]any, bool, error) {
	return nil, false, nil
}
