package projecthistoryredis

import (
	"errors"
	"testing"

	"document-updater/internal/historyconversions"
	"document-updater/internal/limits"
	"document-updater/internal/utils"
	"ollitex/go/libraries/oerror"
)

// --- fake redis surface (mirrors the oracle sinon rclient/multi stubs) ---

type rpushCap struct {
	key    string
	values []string
}

type setNxCap struct {
	key   string
	value any
}

// fakeTx mirrors the oracle this.multi stub (rpush/setnx recording + a
// per-Multi exec capture).
type fakeTx struct {
	c       *fakeClient
	rpushes []rpushCap
	setnxes []setNxCap
	execd   bool
}

func (tx *fakeTx) RPush(key string, values ...string) Tx {
	tx.rpushes = append(tx.rpushes, rpushCap{key: key, values: values})
	return tx
}

func (tx *fakeTx) SetNx(key string, value any) Tx {
	tx.setnxes = append(tx.setnxes, setNxCap{key: key, value: value})
	return tx
}

func (tx *fakeTx) Exec() []any {
	tx.execd = true
	return nil
}

// fakeClient mirrors the vendored rclient: multi() hands out recording Tx.
type fakeClient struct{ txList []*fakeTx }

func (c *fakeClient) Multi() Tx {
	tx := &fakeTx{c: c}
	c.txList = append(c.txList, tx)
	return tx
}

func (c *fakeClient) lastTx() *fakeTx { return c.txList[len(c.txList)-1] }

// oracle clock pin (timekeeper.freeze analog).
const (
	projectID      = "project-id-123"
	projectHistory = "history-id-123"
	userID         = "user-id-123"
)

func ptr[T any](v T) *T { return &v }

func b(v bool) *bool { return &v }

// oracleNow / oracleNowISO are the fixed wall instant (ISO rendered by
// tsISO over ms since epoch).
const (
	oracleNow    = int64(1700000000000)
	oracleNowISO = "2023-11-14T22:13:20.000Z"
)

// --- queueOps (direct redis path) ------------------------------------------

// TestQueueOpsPins pins the oracle: rpush on ProjectHistory:Ops:<project>
// with both raw ops, setnx on ProjectHistory:FirstOpTimestamp:<project>
// with Date.now() (raw ms int), Exec, and per-op
// metrics.summary(redis.projectHistoryOps, op.length, {status:'push'}).
func TestQueueOpsPins(t *testing.T) {
	fake := &fakeClient{}
	m := New()
	m.C = fake
	m.Now = func() int64 { return oracleNow }
	type summaryCap struct {
		metric string
		n      int
		status any
	}
	summaryCaps := []summaryCap{}
	m.Summary = func(metric string, n int, labels map[string]any) {
		summaryCaps = append(summaryCaps, summaryCap{metric: metric, n: n, status: labels["status"]})
	}

	ops := []string{"mock-op-1", "mock-op-2"}
	if err := m.QueueOps(projectID, ops[0], ops[1]); err != nil {
		t.Fatalf("queueOps: %v", err)
	}
	if len(fake.txList) != 1 {
		t.Fatalf("multi count: got %d", len(fake.txList))
	}
	tx := fake.lastTx()
	if len(tx.rpushes) != 1 || tx.rpushes[0].key != "ProjectHistory:Ops:"+projectID {
		t.Fatalf("rpush: got %#v", tx.rpushes)
	}
	if len(tx.rpushes[0].values) != 2 || tx.rpushes[0].values[0] != "mock-op-1" || tx.rpushes[0].values[1] != "mock-op-2" {
		t.Fatalf("rpush values: got %#v", tx.rpushes[0].values)
	}
	if len(tx.setnxes) != 1 || tx.setnxes[0].key != "ProjectHistory:FirstOpTimestamp:"+projectID {
		t.Fatalf("setnx: got %#v", tx.setnxes)
	}
	// Oracle pins Date.now() (raw ms) — not the ISO string.
	if tx.setnxes[0].value != oracleNow {
		t.Fatalf("setnx value: got %#v want raw ms %d", tx.setnxes[0].value, oracleNow)
	}
	if !tx.execd {
		t.Fatal("exec not called")
	}
	if len(summaryCaps) != 2 {
		t.Fatalf("summary calls: got %d", len(summaryCaps))
	}
	for i, s := range summaryCaps {
		if s.metric != "redis.projectHistoryOps" || s.n != len(ops[i]) || s.status != "push" {
			t.Fatalf("summary[%d]: got %#v", i, s)
		}
	}
}

// --- queue entity seams (oracle `promises.queueOps = stub()`) --------------

type sizeCap struct {
	size  int
	lines []string
	max   int
}

type rawCap struct {
	raw *limits.StringFileRawData
	max int
}

// seamManager mirrors the oracle sandboxed module (queueOps stubbed, Limits
// stubbed, wall frozen).
type seamManager struct {
	m        *Manager
	ops      []string
	sizeCaps []sizeCap
	rawCaps  []rawCap
	tooLarge bool
}

func newSeam(t *testing.T) *seamManager {
	t.Helper()
	sm := &seamManager{}
	sm.m = New()
	sm.m.Now = func() int64 { return oracleNow }
	sm.m.MaxDocLength = 123 // oracle sandboxed Settings override.
	sm.m.QueueOpsSeam = func(projectID string, ops ...string) error {
		sm.ops = append(sm.ops, ops...)
		return nil
	}
	sm.m.DocIsTooLarge = func(estimatedSize int, lines []string, maxDocLength int) bool {
		sm.sizeCaps = append(sm.sizeCaps, sizeCap{size: estimatedSize, lines: lines, max: maxDocLength})
		return sm.tooLarge
	}
	sm.m.StringFileDataContentIsTooLarge = func(raw *limits.StringFileRawData, maxDocLength int) bool {
		sm.rawCaps = append(sm.rawCaps, rawCap{raw: raw, max: maxDocLength})
		return sm.tooLarge
	}
	return sm
}

// --- queueRenameEntity -------------------------------------------------------

func TestQueueRenameEntityPin(t *testing.T) {
	sm := newSeam(t)
	sm.m.QueueRenameEntity(projectID, projectHistory, "file", 1234, userID, RawRenameEntity{
		Pathname: "/old", NewPathname: "/new", Version: 2,
	}, "editor")
	if len(sm.ops) != 1 {
		t.Fatalf("seam ops: got %d", len(sm.ops))
	}
	want := `{"pathname":"/old","new_pathname":"/new","meta":{"user_id":"user-id-123","ts":"` + oracleNowISO +
		`","source":"editor"},"version":2,"projectHistoryId":"history-id-123","file":1234}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("rename wire:\n got %s\nwant %s", got, want)
	}
}

func TestQueueRenameEntityOriginObject(t *testing.T) {
	sm := newSeam(t)
	sm.m.QueueRenameEntity(projectID, projectHistory, "doc", "doc-id", userID, RawRenameEntity{
		Pathname: "/old", NewPathname: "/new", Version: 1,
	}, map[string]any{"kind": "import", "metadata": "provider-x"})
	want := `{"pathname":"/old","new_pathname":"/new","meta":{"user_id":"user-id-123","ts":"` + oracleNowISO +
		`","origin":{"kind":"import","metadata":"provider-x"},"type":"external"},"version":1,"projectHistoryId":"history-id-123","doc":"doc-id"}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("origin wire:\n got %s\nwant %s", got, want)
	}
}

func TestQueueRenameEntityNonEditorSourceNoType(t *testing.T) {
	sm := newSeam(t)
	sm.m.QueueRenameEntity(projectID, projectHistory, "doc", "doc-id", userID, RawRenameEntity{
		Pathname: "/old", NewPathname: "/new", Version: 1,
	}, "import")
	want := `{"pathname":"/old","new_pathname":"/new","meta":{"user_id":"user-id-123","ts":"` + oracleNowISO +
		`","source":"import","type":"external"},"version":1,"projectHistoryId":"history-id-123","doc":"doc-id"}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("source wire:\n got %s\nwant %s", got, want)
	}
}

// --- queueAddEntity ------------------------------------------------------------

func TestQueueAddEntityDoc(t *testing.T) {
	sm := newSeam(t)
	sm.m.QueueAddEntity(projectID, projectHistory, "doc", 1234, userID, RawAddEntity{
		Pathname: "/old", DocLines: ptr("a\nb"), Version: 2,
	}, "editor")
	want := `{"pathname":"/old","docLines":"a\nb","meta":{"user_id":"user-id-123","ts":"` + oracleNowISO +
		`","source":"editor"},"version":2,"projectHistoryId":"history-id-123","createdBlob":false,"doc":1234}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("doc add wire:\n got %s\nwant %s", got, want)
	}
}

func TestQueueAddEntityDocURL(t *testing.T) {
	sm := newSeam(t)
	sm.m.QueueAddEntity(projectID, projectHistory, "doc", 1234, userID, RawAddEntity{
		Pathname: "/old", DocLines: ptr("a\nb"), URL: ptr("filestore.example.com"), Version: 2,
	}, "editor")
	want := `{"pathname":"/old","docLines":"a\nb","url":"filestore.example.com","meta":{"user_id":"user-id-123","ts":"` + oracleNowISO +
		`","source":"editor"},"version":2,"projectHistoryId":"history-id-123","createdBlob":false,"doc":1234}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("doc-url add wire:\n got %s\nwant %s", got, want)
	}
}

func TestQueueAddEntityCreatedBlobTrue(t *testing.T) {
	sm := newSeam(t)
	sm.m.QueueAddEntity(projectID, projectHistory, "doc", 1234, userID, RawAddEntity{
		Pathname: "/old", DocLines: ptr("a\nb"), Version: 2, CreatedBlob: ptr(true),
	}, "editor")
	want := `{"pathname":"/old","docLines":"a\nb","meta":{"user_id":"user-id-123","ts":"` + oracleNowISO +
		`","source":"editor"},"version":2,"projectHistoryId":"history-id-123","createdBlob":true,"doc":1234}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("createdBlob-true add wire:\n got %s\nwant %s", got, want)
	}
}

func TestQueueAddEntityFileMetadata(t *testing.T) {
	sm := newSeam(t)
	url := `http://filestore/project/` + projectID + `/file/file-id`
	sm.m.QueueAddEntity("project-id", projectHistory, "file", "file-id", userID, RawAddEntity{
		Pathname: "foo.png",
		URL:      ptr(url),
		Version:  42,
		Hash:     ptr("1337"),
		Metadata: map[string]any{
			"importedAt": "2024-07-30T09:14:45.928Z",
			"provider":   "references-provider",
		},
	}, "editor")
	want := `{"pathname":"foo.png","url":"` + url + `","meta":{"user_id":"user-id-123","ts":"` + oracleNowISO +
		`","source":"editor"},"version":42,"hash":"1337","metadata":{"importedAt":"2024-07-30T09:14:45.928Z","provider":"references-provider"},"projectHistoryId":"history-id-123","createdBlob":false,"file":"file-id"}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("file wire:\n got %s\nwant %s", got, want)
	}
}

func TestQueueAddEntityHRS(t *testing.T) {
	sm := newSeam(t)
	ranges := historyconversions.Ranges{
		Changes: []utils.TrackedChange{
			{Op: utils.Op{P: 4, I: ptr("quick")}, Metadata: &utils.Metadata{TS: "2024-01-01T00:00:00.000Z", UserID: "user-1"}},
			{Op: utils.Op{P: 9, D: ptr(" brown")}, Metadata: &utils.Metadata{TS: "2024-02-01T00:00:00.000Z", UserID: "user-1"}},
			{Op: utils.Op{P: 14, I: ptr("jumps")}, Metadata: &utils.Metadata{TS: "2024-02-01T00:00:00.000Z", UserID: "user-1"}},
		},
		Comments: []historyconversions.Comment{
			{Op: historyconversions.CommentOp{P: 29, C: "lazy", T: "comment-1"}, Metadata: &utils.Metadata{Resolved: ptr(false)}},
		},
	}
	sm.m.QueueAddEntity(projectID, projectHistory, "doc", 1234, userID, RawAddEntity{
		Pathname:             "/old",
		DocLines:             ptr("the quick fox jumps over the lazy dog"),
		Version:              2,
		HistoryRangesSupport: true,
		Ranges:               &ranges,
	}, "editor")
	// HRS: content = addTrackedDeletesToContent + toHistoryRanges
	// enrichment (hpos per committed port); comments-first container wire,
	// metadata wire keys ts/user_id (Go-sorted, oracle-compatible).
	want := `{"pathname":"/old","docLines":"the quick brown fox jumps over the lazy dog","meta":{"user_id":"user-id-123","ts":"` + oracleNowISO +
		`","source":"editor"},"version":2,"projectHistoryId":"history-id-123","createdBlob":false,"ranges":{"comments":[{"op":{"p":29,"c":"lazy","t":"comment-1","hpos":35},"metadata":{"resolved":false}}]` +
		`,"changes":[{"op":{"p":4,"i":"quick"},"metadata":{"ts":"2024-01-01T00:00:00.000Z","user_id":"user-1"}}` +
		`,{"op":{"p":9,"d":" brown"},"metadata":{"ts":"2024-02-01T00:00:00.000Z","user_id":"user-1"}}` +
		`,{"op":{"p":14,"i":"jumps","hpos":20},"metadata":{"ts":"2024-02-01T00:00:00.000Z","user_id":"user-1"}}]},"doc":1234}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("HRS add wire:\n got %s\nwant %s", got, want)
	}
}

func TestQueueAddEntityHRSDisabled(t *testing.T) {
	sm := newSeam(t)
	ranges := historyconversions.Ranges{
		Changes: []utils.TrackedChange{
			{Op: utils.Op{P: 0, I: ptr("foo")}, Metadata: &utils.Metadata{TS: "2024-01-01T00:00:00.000Z", UserID: "user-1"}},
		},
		Comments: []historyconversions.Comment{
			{Op: historyconversions.CommentOp{P: 4, C: "bar", T: "comment-1"}, Metadata: &utils.Metadata{Resolved: ptr(false)}},
		},
	}
	sm.m.QueueAddEntity(projectID, projectHistory, "doc", 1234, userID, RawAddEntity{
		Pathname: "/old", DocLines: ptr("a\nb"), Version: 2,
		HistoryRangesSupport: false, Ranges: &ranges,
	}, "editor")
	// historyRangesSupport falsy: no content transform, no ranges key.
	want := `{"pathname":"/old","docLines":"a\nb","meta":{"user_id":"user-id-123","ts":"` + oracleNowISO +
		`","source":"editor"},"version":2,"projectHistoryId":"history-id-123","createdBlob":false,"doc":1234}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("HRS-disabled add wire:\n got %s\nwant %s", got, want)
	}
}

// --- queueResyncProjectStructure -----------------------------------------------

func TestQueueResyncProjectStructure(t *testing.T) {
	sm := newSeam(t)
	// No opts: no flag key.
	sm.m.QueueResyncProjectStructure(projectID, projectHistory, []string{"d1", "d2"}, []string{"f1"}, nil)
	want := `{"resyncProjectStructure":{"docs":["d1","d2"],"files":["f1"]},"projectHistoryId":"history-id-123","meta":{"ts":"` + oracleNowISO + `"}}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("resync-structure wire:\n got %s\nwant %s", got, want)
	}

	// Falsy flag: vendor `if (opts.resyncProjectStructureOnly)` omits the
	// key entirely.
	sm.m.QueueResyncProjectStructure(projectID, projectHistory, []string{"d1"}, []string{}, &ResyncOpts{ResyncProjectStructureOnly: b(false)})
	wantFalsy := `{"resyncProjectStructure":{"docs":["d1"],"files":[]},"projectHistoryId":"history-id-123","meta":{"ts":"` + oracleNowISO + `"}}`
	if got := sm.ops[1]; got != wantFalsy {
		t.Fatalf("falsy-flag wire (key must be absent):\n got %s\nwant %s", got, wantFalsy)
	}

	// Truthy flag: key appended as the last projectUpdate key.
	sm.m.QueueResyncProjectStructure(projectID, projectHistory, []string{"d1"}, []string{"f9"}, &ResyncOpts{ResyncProjectStructureOnly: b(true)})
	wantTrue := `{"resyncProjectStructure":{"docs":["d1"],"files":["f9"]},"projectHistoryId":"history-id-123","meta":{"ts":"` + oracleNowISO + `"},"resyncProjectStructureOnly":true}`
	if got := sm.ops[2]; got != wantTrue {
		t.Fatalf("truthy-flag wire:\n got %s\nwant %s", got, wantTrue)
	}
}

// --- queueResyncDocContent -------------------------------------------------------

func TestQueueResyncDocContentGood(t *testing.T) {
	sm := newSeam(t)
	sm.m.QueueResyncDocContent(projectID, projectHistory, 1234, []string{"one", "two"}, nil, historyconversions.Ranges{}, []string{"comment-1"}, 2, "/path", false)
	want := `{"resyncDocContent":{"version":2,"content":"one\ntwo"},"projectHistoryId":"history-id-123","path":"/path","doc":1234,"meta":{"ts":"` + oracleNowISO + `"}}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("resync-good wire:\n got %s\nwant %s", got, want)
	}
	// Vendor optimised size check: docIsTooLarge(serialisedUpdateLength,
	// lines (no-tracked-delete form), max_doc_length).
	if len(sm.sizeCaps) != 1 || sm.sizeCaps[0].size != len(want) || sm.sizeCaps[0].max != 123 {
		t.Fatalf("size caps: got %+v", sm.sizeCaps)
	}
	if len(sm.sizeCaps[0].lines) != 2 || sm.sizeCaps[0].lines[0] != "one" || sm.sizeCaps[0].lines[1] != "two" {
		t.Fatalf("size cap lines: got %v", sm.sizeCaps[0].lines)
	}
	// The raw-string guard must NOT be consulted on the lines path.
	if len(sm.rawCaps) != 0 {
		t.Fatalf("raw caps: got %+v", sm.rawCaps)
	}
}

func TestQueueResyncDocContentTooLarge(t *testing.T) {
	sm := newSeam(t)
	sm.tooLarge = true
	err := sm.m.QueueResyncDocContent(projectID, projectHistory, 1234, []string{"one", "two"}, nil, historyconversions.Ranges{}, []string{"comment-1"}, 2, "/path", false)
	if len(sm.ops) != 0 {
		t.Fatalf("too-large must not queue ops, got %v", sm.ops)
	}
	var oe *oerror.OError
	if !errors.As(err, &oe) {
		t.Fatalf("want OError, got %v", err)
	}
	if oe.Message != "blocking resync doc content insert into project history queue: doc is too large" {
		t.Fatalf("OError message: got %q", oe.Message)
	}
	wantWire := `{"resyncDocContent":{"version":2,"content":"one\ntwo"},"projectHistoryId":"history-id-123","path":"/path","doc":1234,"meta":{"ts":"` + oracleNowISO + `"}}`
	if oe.Info["projectId"] != projectID || oe.Info["docId"] != 1234 || oe.Info["docSize"] != len(wantWire) {
		t.Fatalf("OError info: got %#v", oe.Info)
	}
}

func TestQueueResyncDocContentHRS(t *testing.T) {
	sm := newSeam(t)
	ranges := historyconversions.Ranges{
		Changes: []utils.TrackedChange{
			{Op: utils.Op{I: ptr("ne"), P: 1}},
			{Op: utils.Op{D: ptr("deleted"), P: 3}},
		},
	}
	sm.m.QueueResyncDocContent(projectID, projectHistory, 1234, []string{"one", "two"}, nil, ranges, []string{"comment-1"}, 2, "/path", true)
	// DIVERGENCE (HANDOFF): the vendor oracle fixture pins op key order
	// {i,p} (lodash cloneDeep preserves fixture insertion order); the Go
	// port emits the CANONICAL p-first op wire (production ShareJS shape).
	// Content = addTrackedDeletesToContent('one\ntwo', {p:3,d:'deleted'});
	// no hpos/hlen (offset 0 throughout, no comments).
	want := `{"resyncDocContent":{"version":2,"ranges":{"changes":[{"op":{"p":1,"i":"ne"}},{"op":{"p":3,"d":"deleted"}}]},"resolvedCommentIds":["comment-1"],"content":"onedeleted\ntwo"},"projectHistoryId":"history-id-123","path":"/path","doc":1234,"meta":{"ts":"` + oracleNowISO + `"}}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("HRS resync wire:\n got %s\nwant %s", got, want)
	}
	if len(sm.sizeCaps) != 1 || sm.sizeCaps[0].size != len(want) {
		t.Fatalf("HRS size caps: got %+v", sm.sizeCaps)
	}
}

func TestQueueResyncDocContentHistoryOT(t *testing.T) {
	sm := newSeam(t)
	raw := &RawLines{
		Content: "onedeleted\ntwo",
		Comments: []RawLineComment{
			{ID: "id1", Ranges: []RawRange{{Pos: 0, Length: 3}}},
		},
		TrackedChanges: []RawTrackedChange{
			{Range: RawRange{Pos: 3, Length: 7}, Tracking: RawTracking{Type: "delete", UserID: "user-id", TS: "2025-06-16T14:31:44.910Z"}},
		},
	}
	sm.m.QueueResyncDocContent(projectID, projectHistory, 1234, nil, raw, historyconversions.Ranges{}, nil, 2, "/path", true)
	// historyOTRanges: verbatim fixture pass-through (the oracle fixture is
	// both input and expected output).
	want := `{"resyncDocContent":{"version":2,"historyOTRanges":{"comments":[{"id":"id1","ranges":[{"pos":0,"length":3}]}],"trackedChanges":[{"range":{"pos":3,"length":7},"tracking":{"type":"delete","userId":"user-id","ts":"2025-06-16T14:31:44.910Z"}}]},"content":"onedeleted\ntwo"},"projectHistoryId":"history-id-123","path":"/path","doc":1234,"meta":{"ts":"` + oracleNowISO + `"}}`
	if got := sm.ops[0]; got != want {
		t.Fatalf("historyOT wire:\n got %s\nwant %s", got, want)
	}
	// stringFileDataContentIsTooLarge(raw, max) — the raw form reshaped
	// into the committed limits type.
	if len(sm.rawCaps) != 1 {
		t.Fatalf("raw caps: got %+v", sm.rawCaps)
	}
	capRaw := sm.rawCaps[0]
	if capRaw.max != 123 || capRaw.raw.Content != "onedeleted\ntwo" {
		t.Fatalf("raw caps: got %+v", capRaw)
	}
	if len(capRaw.raw.TrackedChanges) != 1 {
		t.Fatalf("raw trackedChanges: got %+v", capRaw.raw.TrackedChanges)
	}
	tr := capRaw.raw.TrackedChanges[0]
	if tr.Range.Pos != 3 || tr.Range.Length != 7 || tr.Tracking.Type != "delete" || tr.Tracking.UserID != "user-id" || tr.Tracking.TS != "2025-06-16T14:31:44.910Z" {
		t.Fatalf("raw trackedChange cap: got %+v", tr)
	}
	if len(sm.sizeCaps) != 0 {
		t.Fatalf("lines caps must be unused on the raw path: got %+v", sm.sizeCaps)
	}
}

func TestQueueResyncDocContentHistoryOTTooLarge(t *testing.T) {
	sm := newSeam(t)
	sm.tooLarge = true
	raw := &RawLines{Content: "onedeleted\ntwo"}
	err := sm.m.QueueResyncDocContent(projectID, projectHistory, 1234, nil, raw, historyconversions.Ranges{}, nil, 2, "/path", true)
	if len(sm.ops) != 0 {
		t.Fatalf("too-large must not queue ops, got %v", sm.ops)
	}
	var oe *oerror.OError
	if !errors.As(err, &oe) {
		t.Fatalf("want OError, got %v", err)
	}
	if oe.Message != "blocking resync doc content insert into project history queue: doc is too large" {
		t.Fatalf("OError message: got %q", oe.Message)
	}
	// Vendor emits historyOTRanges even when comments/trackedChanges are
	// undefined: JSON.stringify({comments: undefined, trackedChanges:
	// undefined}) == {}.
	wantWire := `{"resyncDocContent":{"version":2,"historyOTRanges":{},"content":"onedeleted\ntwo"},"projectHistoryId":"history-id-123","path":"/path","doc":1234,"meta":{"ts":"` + oracleNowISO + `"}}`
	if oe.Info["docSize"] != len(wantWire) {
		t.Fatalf("OError docSize: got %v want %d", oe.Info["docSize"], len(wantWire))
	}
}
