package redismanager

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"document-updater/internal/errorsx"
)

const (
	linesWire  = `["one","two","three","これは"]`
	oracleHash = "8b69713c1e40897e9220cec1d5183ea1a7396672"
)

func contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	return strings.Contains(s, sub)
}

func TestJSONStringWireAndHash(t *testing.T) {
	wire := JSONString([]string{"one", "two", "three", "これは"})
	if wire != linesWire {
		t.Fatalf("wire: got %q want %q", wire, linesWire)
	}
	sum := sha1.Sum([]byte(wire))
	if got := hex.EncodeToString(sum[:]); got != oracleHash {
		t.Fatalf("hash: got %s want %s", got, oracleHash)
	}
	if v := JSONString(map[string]any{"a": "<b&c>"}); v != `{"a":"<b&c>"}` {
		t.Fatalf("no-escape wire: got %q", v)
	}
}

func TestSerializeRangesPins(t *testing.T) {
	v, err := SerializeRanges(Ranges{})
	if err != nil || v != nil {
		t.Fatalf("empty ranges: got %v %v want nil nil", v, err)
	}
	v, _ = SerializeRanges(Ranges{"comments": "mock", "entries": "mock"})
	if v == nil || v.(string) != `{"comments":"mock","entries":"mock"}` {
		t.Fatalf("ranges wire: got %v", v)
	}
	blob := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		blob = append(blob, strings.Repeat("x", 1<<20))
	}
	if _, err := SerializeRanges(Ranges{"blob": blob}); err == nil ||
		err.Error() != "ranges are too large" {
		t.Fatalf("big ranges: got %v want 'ranges are too large'", err)
	}
	if got, derr := DeserializeRanges(""); derr != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty deserialize: %v %v", got, derr)
	}
}

func oracleMGet() []any {
	return []any{
		linesWire,                              // doclines
		42,                                     // version
		oracleHash,                             // hash
		"project-id-123",                       // ProjectId
		`{"comments":"mock","entries":"mock"}`, // ranges
		"/a/b/c.tex",                           // pathname
		"123",                                  // projectHistoryId
		12345,                                  // unflushedTime
		"12345",                                // lastUpdatedAt
		"last-author",                          // lastUpdatedBy
	}
}

func TestGetDocAllDetails(t *testing.T) {
	c := NewFakeClient()
	c.MGetReply = oracleMGet()
	c.SIsMemberReply = map[string]int{"HistoryRangesSupport": 0}
	c.SMembersReply = map[string][]string{"ResolvedCommentIds:doc1": {"comment-1"}}
	m := New(c)

	doc, err := m.GetDoc("project-id-123", "doc1")
	if err != nil {
		t.Fatalf("getDoc: %v", err)
	}
	if len(doc.Lines) != 4 || doc.Lines[3] != "これは" {
		t.Fatalf("lines: got %v", doc.Lines)
	}
	if doc.Version != 42 {
		t.Fatalf("version: got %d want 42", doc.Version)
	}
	if doc.Pathname == nil || *doc.Pathname != "/a/b/c.tex" {
		t.Fatalf("pathname: got %v", doc.Pathname)
	}
	if doc.ProjectHistoryID == nil || *doc.ProjectHistoryID != "123" {
		t.Fatalf("projectHistoryId: got %v", doc.ProjectHistoryID)
	}
	if len(doc.ResolvedCommentIDs) != 1 || doc.ResolvedCommentIDs[0] != "comment-1" {
		t.Fatalf("resolvedCommentIds: got %v", doc.ResolvedCommentIDs)
	}
	if doc.HistoryRangesSupport {
		t.Fatal("historyRangesSupport should be false (sismember 0)")
	}
	joined := strings.Join(c.calls, "\n")
	for _, k := range []string{
		"doclines:doc1", "DocVersion:doc1", "DocHash:doc1", "ProjectId:doc1",
		"Ranges:doc1", "Pathname:doc1", "ProjectHistoryId:doc1",
		"UnflushedTime:doc1", "lastUpdatedAt:doc1", "lastUpdatedBy:doc1",
	} {
		if !contains(joined, k) {
			t.Fatalf("mget missing key: %s — calls: %v", k, c.calls)
		}
	}
}

func TestGetDocHashMismatchLogs(t *testing.T) {
	c := NewFakeClient()
	c.MGetReply = oracleMGet()
	c.SIsMemberReply = map[string]int{"HistoryRangesSupport": 0}
	logged := 0
	m := New(c)
	m.LogHashRead = func(p, d, dp, computed, stored string) { logged++ }
	if _, err := m.GetDoc("project-id-123", "doc1"); err != nil {
		t.Fatalf("getDoc: %v", err)
	}
	c.MGetReply = oracleMGet()
	c.MGetReply[2] = "INVALID-HASH-VALUE"
	if _, err := m.GetDoc("project-id-123", "doc1"); err != nil {
		t.Fatalf("getDoc corrupted: %v", err)
	}
	if logged != 1 {
		t.Fatalf("expected exactly one hash-mismatch log, got %d", logged)
	}
}

func TestGetDocSlowTimeout(t *testing.T) {
	c := NewFakeClient()
	c.MGetReply = oracleMGet()
	m := New(c)
	m.Now = advancing(0, 8000)
	if _, err := m.GetDoc("p1", "d1"); err == nil {
		t.Fatal("expected the 'redis getDoc exceeded timeout' error")
	}
}

func TestGetDocInvalidProject(t *testing.T) {
	c := NewFakeClient()
	c.MGetReply = oracleMGet()
	m := New(c)
	_, err := m.GetDoc("wrong-project", "doc1")
	if err == nil {
		t.Fatal("expected a NotFound error")
	}
	var nf *errorsx.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("expected *NotFoundError, got %T %v", err, err)
	}
}

func TestGetDocHRSFlagSet(t *testing.T) {
	c := NewFakeClient()
	c.MGetReply = oracleMGet()
	c.SIsMemberReply = map[string]int{"HistoryRangesSupport": 1}
	c.SMembersReply = map[string][]string{"ResolvedCommentIds:doc1": {"c1"}}
	m := New(c)
	doc, err := m.GetDoc("project-id-123", "doc1")
	if err != nil {
		t.Fatalf("getDoc: %v", err)
	}
	if !doc.HistoryRangesSupport {
		t.Fatal("historyRangesSupport should be true (sismember 1)")
	}
}

func TestGetDocVersion(t *testing.T) {
	c := NewFakeClient()
	c.MGetReply = []any{"42"}
	m := New(c)
	v, err := m.GetDocVersion("doc1")
	if err != nil || v != 42 {
		t.Fatalf("getDocVersion: got %d %v want 42", v, err)
	}
	m.DocVersionSeam = func(docID string) (int, error) { return 7, nil }
	if v, err = m.GetDocVersion("doc1"); err != nil || v != 7 {
		t.Fatalf("seam getDocVersion: got %d %v want 7", v, err)
	}
	m = New(c)
	c.MGetReply = nil
	if v, err = m.GetDocVersion("doc1"); err != nil || v != 0 {
		t.Fatalf("missing version: got %d %v want 0", v, err)
	}
}

func TestGetPreviousDocOpsOffsets(t *testing.T) {
	c := NewFakeClient()
	c.LLenReply = map[string]int{"DocOps:doc1": 40}
	c.GetReply = map[string]string{"DocVersion:doc1": "70"}
	c.LRangeReply = map[string][]string{"DocOps:doc1": {
		`{"mock":"op-1"}`, `{"mock":"op-2"}`,
	}}
	m := New(c)
	ops, err := m.GetPreviousDocOps("doc1", 50, 60)
	if err != nil {
		t.Fatalf("getPrevOps: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("ops: got %d want 2", len(ops))
	}
	last := c.calls[len(c.calls)-1]
	if !contains(last, "lrange DocOps:doc1 20 30") {
		t.Fatalf("lrange offset not shifted (firstVersionInRedis=70-40=30, start=50-30, end=60-30): %s", last)
	}
}

func TestGetPreviousDocOpsEndMinusOne(t *testing.T) {
	c := NewFakeClient()
	c.LLenReply = map[string]int{"DocOps:doc1": 40}
	c.GetReply = map[string]string{"DocVersion:doc1": "70"}
	c.LRangeReply = map[string][]string{"DocOps:doc1": {
		`{"mock":"op-1"}`, `{"mock":"op-2"}`,
	}}
	m := New(c)
	if _, err := m.GetPreviousDocOps("doc1", 50, -1); err != nil {
		t.Fatalf("getPrevOps end=-1: %v", err)
	}
	last := c.calls[len(c.calls)-1]
	if !contains(last, "lrange DocOps:doc1 20 -1") {
		t.Fatalf("lrange end must stay -1: %s", last)
	}
}

func TestGetPreviousDocOpsOutOfRange(t *testing.T) {
	c := NewFakeClient()
	c.LLenReply = map[string]int{"DocOps:doc1": 40}
	c.GetReply = map[string]string{"DocVersion:doc1": "70"}
	m := New(c)
	_, err := m.GetPreviousDocOps("doc1", 20, -1)
	if err == nil {
		t.Fatal("expected OpRangeNotAvailable")
	}
	var o *errorsx.OpRangeNotAvailableError
	if !errors.As(err, &o) {
		t.Fatalf("expected *OpRangeNotAvailableError, got %T %v", err, err)
	}
}

func TestGetPreviousDocOpsSlowTimeout(t *testing.T) {
	c := NewFakeClient()
	c.LLenReply = map[string]int{"DocOps:doc1": 40}
	c.GetReply = map[string]string{"DocVersion:doc1": "70"}
	c.LRangeReply = map[string][]string{"DocOps:doc1": {`{"a":1}`}}
	m := New(c)
	m.Now = advancing(0, 9000)
	if _, err := m.GetPreviousDocOps("doc1", 50, 60); err == nil {
		t.Fatal("expected the 'redis getPreviousDocOps exceeded timeout' error")
	}
}

// --- the oracle updateDocument pins. ---

func oracleOpsJSON() []string {
	return []string{
		`{"op":[{"i":"foo","p":4}]}`,
		`{"op":[{"i":"bar","p":8}]}`,
	}
}

func TestUpdateDocumentConsistent(t *testing.T) {
	c := NewFakeClient()
	c.MGetReply = nil // MGet(DocVersion) via GetDocVersion seam below.
	ops := []any{
		map[string]any{"op": []any{map[string]any{"i": "foo", "p": 4}}},
		map[string]any{"op": []any{map[string]any{"i": "bar", "p": 8}}},
	}
	m := New(c)
	m.DocVersionSeam = func(docID string) (int, error) { return 40, nil }
	err := m.UpdateDocument(
		"proj", "doc1",
		[]string{"one", "two", "three", "これは"},
		42,
		ops,
		Ranges{"comments": "mock", "entries": "mock"},
		map[string]any{"user_id": "last-author-fake-id"},
	)
	if err != nil {
		t.Fatalf("updateDocument: %v", err)
	}
	// the 6-key mset with the version, hash, wire lines, ranges,
	// lastUpdatedAt (now seam) and lastUpdatedBy.
	if len(c.Tx.MSets) != 1 {
		t.Fatalf("expected one mset, got %d", len(c.Tx.MSets))
	}
	ms := c.Tx.MSets[0]
	if ms["DocVersion:doc1"] != 42 {
		t.Fatalf("mset version: got %v want 42", ms["DocVersion:doc1"])
	}
	if ms["DocHash:doc1"] != oracleHash {
		t.Fatalf("mset hash: got %v want %s", ms["DocHash:doc1"], oracleHash)
	}
	if ms["doclines:doc1"] != linesWire {
		t.Fatalf("mset lines: got %v want %s", ms["doclines:doc1"], linesWire)
	}
	if ms["lastUpdatedBy:doc1"] != "last-author-fake-id" {
		t.Fatalf("mset lastUpdatedBy: got %v", ms["lastUpdatedBy:doc1"])
	}
	// the rpush op list (both ops serialized).
	if len(c.Tx.RPushs) != 1 {
		t.Fatalf("rpush count: got %d want 1", len(c.Tx.RPushs))
	}
	rp := c.Tx.RPushs[0]
	if rp.key != "DocOps:doc1" || len(rp.vals) != 2 {
		t.Fatalf("rpush args: got key=%s vals=%v", rp.key, rp.vals)
	}
	for i, want := range oracleOpsJSON() {
		if rp.vals[i] != want {
			t.Fatalf("rpush op %d: got %s want %s", i, rp.vals[i], want)
		}
	}
	// the docops-list expire with DOC_OPS_TTL.
	if len(c.Tx.Expires) != 1 || c.Tx.Expires[0].key != "DocOps:doc1" ||
		c.Tx.Expires[0].secs != DocOpsTTL {
		t.Fatalf("expire: got %v want key=DocOps:doc1 secs=%d", c.Tx.Expires, DocOpsTTL)
	}
	// the ltrim to the last 100 ops.
	if len(c.Tx.LTrims) != 1 ||
		c.Tx.LTrims[0].key != "DocOps:doc1" ||
		c.Tx.LTrims[0].start != -DocOpsMaxLength || c.Tx.LTrims[0].stop != -1 {
		t.Fatalf("ltrim: got %v want key=DocOps:doc1 start=-100 stop=-1", c.Tx.LTrims)
	}
	// the unflushed-time NX set.
	if len(c.Tx.Sets) != 1 || c.Tx.Sets[0].key != "UnflushedTime:doc1" ||
		len(c.Tx.Sets[0].opts) < 1 || c.Tx.Sets[0].opts[0] != "NX" {
		t.Fatalf("unflushed NX set: got %v", c.Tx.Sets)
	}
}

func TestUpdateDocumentNoOpsNoRPush(t *testing.T) {
	c := NewFakeClient()
	m := New(c)
	m.DocVersionSeam = func(docID string) (int, error) { return 42, nil }
	err := m.UpdateDocument(
		"proj", "doc1",
		[]string{"one", "two", "three", "これは"},
		42,
		nil,
		Ranges{"comments": "mock", "entries": "mock"},
		map[string]any{"user_id": "last-author-fake-id"},
	)
	if err != nil {
		t.Fatalf("updateDocument no-ops: %v", err)
	}
	if len(c.Tx.RPushs) != 0 {
		t.Fatalf("no-ops must not rpush, got %d rpush calls", len(c.Tx.RPushs))
	}
	if len(c.Tx.Expires) != 0 {
		t.Fatalf("no-ops must not expire, got %d expire calls", len(c.Tx.Expires))
	}
	if len(c.Tx.MSets) != 1 {
		t.Fatalf("no-ops must still mset doclines, got %d msets", len(c.Tx.MSets))
	}
}

func TestUpdateDocumentInconsistentVersion(t *testing.T) {
	c := NewFakeClient()
	m := New(c)
	m.DocVersionSeam = func(docID string) (int, error) { return 40, nil }
	err := m.UpdateDocument(
		"proj", "doc1",
		[]string{"one", "two", "three", "これは"},
		42,
		[]any{map[string]any{"op": nil}},
		Ranges{"comments": "mock", "entries": "mock"},
		map[string]any{"user_id": "last-author-fake-id"},
	)
	if err == nil {
		t.Fatal("expected a version-mismatch error")
	}
	if len(c.Tx.MSets) != 0 {
		t.Fatalf("mismatch: exec not called, mset called anyway: %v", c.Tx.MSets)
	}
}

func TestUpdateDocumentEmptyRanges(t *testing.T) {
	c := NewFakeClient()
	m := New(c)
	m.DocVersionSeam = func(docID string) (int, error) { return 40, nil }
	err := m.UpdateDocument(
		"proj", "doc1",
		[]string{"one", "two", "three", "これは"},
		42,
		[]any{
			map[string]any{"op": nil},
			map[string]any{"op": nil},
		},
		Ranges{}, // empty ranges must serialize to nil in the mset.
		map[string]any{"user_id": "last-author-fake-id"},
	)
	if err != nil {
		t.Fatalf("updateDocument empty ranges: %v", err)
	}
	if len(c.Tx.MSets) != 1 {
		t.Fatalf("msets: %d", len(c.Tx.MSets))
	}
	if v := c.Tx.MSets[0]["Ranges:doc1"]; v != nil {
		t.Fatalf("empty ranges mset value: got %v want nil", v)
	}
}

func TestUpdateDocumentNoUserID(t *testing.T) {
	c := NewFakeClient()
	m := New(c)
	m.DocVersionSeam = func(docID string) (int, error) { return 40, nil }
	err := m.UpdateDocument(
		"proj", "doc1",
		[]string{"one", "two", "three", "これは"},
		42,
		[]any{map[string]any{"op": nil}, map[string]any{"op": nil}},
		Ranges{"comments": "mock", "entries": "mock"},
		map[string]any{}, // no user_id — the vendor leaves lastUpdatedBy undefined.
	)
	if err != nil {
		t.Fatalf("updateDocument no-user: %v", err)
	}
	if v := c.Tx.MSets[0]["lastUpdatedBy:doc1"]; v != nil {
		t.Fatalf("lastUpdatedBy with no user_id: got %v want nil", v)
	}
}

// --- the oracle putDocInMemory pins. ---

func TestPutDocInMemoryNonEmptyRanges(t *testing.T) {
	c := NewFakeClient()
	c.scriptExec(0, []any{0}) // block probe MULTI reply: [0] (not blocked).
	m := New(c)
	err := m.PutDocInMemory(
		"project-id-123", "doc1",
		[]string{"one", "two", "three", "これは"},
		42,
		Ranges{"comments": "mock", "entries": "mock"},
		[]string{"comment-1"},
		"/a/b/c.tex",
		"123",
		false,
	)
	if err != nil {
		t.Fatalf("putDocInMemory: %v", err)
	}
	// the block probe MULTI: exists(ProjectBlock)+sadd(DocsIn,doc) before
	// the contents write.
	probe := c.Tx
	if len(probe.Existses) != 1 || probe.Existses[0] != "ProjectBlock:project-id-123" {
		t.Fatalf("probe exists: got %v", probe.Existses)
	}
	if len(probe.SAdds) != 1 || probe.SAdds[0].key != "DocsIn:project-id-123" ||
		len(probe.SAdds[0].members) != 1 || probe.SAdds[0].members[0] != "doc1" {
		t.Fatalf("probe sadd: got %v", probe.SAdds)
	}
	// the 7-key contents mset.
	if len(probe.MSets) != 1 {
		t.Fatalf("msets: %d", len(probe.MSets))
	}
	ms := probe.MSets[0]
	if ms["doclines:doc1"] != linesWire {
		t.Fatalf("mset lines: got %v", ms["doclines:doc1"])
	}
	if ms["ProjectId:doc1"] != "project-id-123" {
		t.Fatalf("mset projectId: got %v", ms["ProjectId:doc1"])
	}
	if ms["DocVersion:doc1"] != 42 {
		t.Fatalf("mset version: got %v", ms["DocVersion:doc1"])
	}
	if ms["DocHash:doc1"] != oracleHash {
		t.Fatalf("mset hash: got %v", ms["DocHash:doc1"])
	}
	if ms["Ranges:doc1"] != `{"comments":"mock","entries":"mock"}` {
		t.Fatalf("mset ranges: got %v", ms["Ranges:doc1"])
	}
	if ms["Pathname:doc1"] != "/a/b/c.tex" {
		t.Fatalf("mset pathname: got %v", ms["Pathname:doc1"])
	}
	if ms["ProjectHistoryId:doc1"] != "123" {
		t.Fatalf("mset pHistId: got %v", ms["ProjectHistoryId:doc1"])
	}
	// HRS=false → the direct-client srem.
	if len(c.calls) == 0 || !contains(strings.Join(c.calls, "\n"), "srem HistoryRangesSupport doc1") {
		t.Fatalf("HRS=false srem missing: %v", c.calls)
	}
}

func TestPutDocInMemoryBlocked(t *testing.T) {
	c := NewFakeClient()
	c.scriptExec(0, []any{1}) // exists reply [1] → blocked.
	m := New(c)
	err := m.PutDocInMemory(
		"project-id-123", "doc1",
		[]string{"one", "two"},
		1,
		Ranges{"comments": "mock"},
		[]string{"comment-1"},
		"/a.tex",
		"1",
		false,
	)
	if err == nil {
		t.Fatal("expected the project-blocked error")
	}
	if len(c.Tx.MSets) != 0 {
		t.Fatalf("blocked: no contents mset, got %d", len(c.Tx.MSets))
	}
}

func TestPutDocInMemoryHRS(t *testing.T) {
	c := NewFakeClient()
	c.scriptExec(0, []any{0})
	m := New(c)
	err := m.PutDocInMemory(
		"project-id-123", "doc1",
		[]string{"one", "two", "three", "これは"},
		42,
		Ranges{"comments": "mock"},
		[]string{"comment-1"},
		"/a/b/c.tex",
		"123",
		true,
	)
	if err != nil {
		t.Fatalf("putDocInMemory HRS: %v", err)
	}
	// HRS=true → the direct-client sadd + the contents-multi del/sadd of
	// the resolved comments.
	if len(c.calls) == 0 || !contains(strings.Join(c.calls, "\n"), "sadd HistoryRangesSupport doc1") {
		t.Fatalf("HRS=true sadd missing: %v", c.calls)
	}
	delFound, saddRCID := false, false
	for _, d := range c.Tx.Dels {
		for _, k := range d.keys {
			if k == "ResolvedCommentIds:doc1" {
				delFound = true
			}
		}
	}
	for _, s := range c.Tx.SAdds {
		if s.key == "ResolvedCommentIds:doc1" {
			saddRCID = true
		}
	}
	if !delFound {
		t.Fatal("HRS=true: no del(ResolvedCommentIds) in the multi")
	}
	if !saddRCID {
		t.Fatal("HRS=true: no sadd(ResolvedCommentIds) in the multi")
	}
}

// --- the oracle removeDocFromMemory / clearProjectState / renameDoc /
// getDocVersion pins. ---

func TestRemoveDocFromMemory(t *testing.T) {
	c := NewFakeClient()
	c.scriptExec(0, []any{0}) // strlen reply [0] (no bytes freed).
	m := New(c)
	if err := m.RemoveDocFromMemory("project-id-123", "doc1"); err != nil {
		t.Fatalf("removeDoc: %v", err)
	}
	if len(c.Tx.StrLens) != 1 || c.Tx.StrLens[0] != "doclines:doc1" {
		t.Fatalf("strlen: got %v", c.Tx.StrLens)
	}
	// the 11-key del in one multi call.
	delFound := false
	for _, d := range c.Tx.Dels {
		found := true
		want := []string{
			"doclines:doc1", "ProjectId:doc1", "DocVersion:doc1", "DocHash:doc1",
			"Ranges:doc1", "Pathname:doc1", "ProjectHistoryId:doc1",
			"UnflushedTime:doc1", "lastUpdatedAt:doc1", "lastUpdatedBy:doc1",
			"ResolvedCommentIds:doc1",
		}
		if len(d.keys) != len(want) {
			found = false
			break
		}
		for i, k := range d.keys {
			if k != want[i] {
				found = false
			}
		}
		if found {
			delFound = true
		}
	}
	if !delFound {
		t.Fatalf("11-key del missing: %v", c.Tx.Dels)
	}
	// the docsIn-project srem + the projectState del (second MULTI).
	sremDocsIn, delProjectState := false, false
	for _, s := range c.Tx.SReMs {
		if s.key == "DocsIn:project-id-123" {
			sremDocsIn = true
		}
	}
	for _, d := range c.Tx.Dels {
		for _, k := range d.keys {
			if k == "ProjectState:project-id-123" {
				delProjectState = true
			}
		}
	}
	if !sremDocsIn {
		t.Fatal("no srem(DocsIn) in the multi")
	}
	if !delProjectState {
		t.Fatal("no del(ProjectState) in the multi")
	}
	// the HRS srem (direct client).
	if !contains(strings.Join(c.calls, "\n"), "srem HistoryRangesSupport doc1") {
		t.Fatalf("no HRS srem: %v", c.calls)
	}
}

func TestClearProjectState(t *testing.T) {
	c := NewFakeClient()
	m := New(c)
	if err := m.ClearProjectState("project-id-123"); err != nil {
		t.Fatalf("clearProjectState: %v", err)
	}
	if !contains(strings.Join(c.calls, "\n"), "del ProjectState:project-id-123") {
		t.Fatalf("no del(ProjectState): %v", c.calls)
	}
}

func TestRenameDocCached(t *testing.T) {
	c := NewFakeClient()
	m := New(c)
	m.GetDocSeam = func(projectID, docID string) (*Doc, error) {
		lines := []string{"lines"}
		return &Doc{Lines: lines, Version: 42}, nil
	}
	err := m.RenameDoc("project-id-123", "doc1", "user", map[string]any{
		"id": "doc1", "pathname": "pathname", "newPathname": "new-pathname",
	}, "123")
	if err != nil {
		t.Fatalf("renameDoc: %v", err)
	}
	if !contains(strings.Join(c.calls, "\n"), "set Pathname:doc1 new-pathname") {
		t.Fatalf("no set(Pathname): %v", c.calls)
	}
}

func TestRenameDocNotCached(t *testing.T) {
	c := NewFakeClient()
	m := New(c)
	m.GetDocSeam = func(projectID, docID string) (*Doc, error) {
		return &Doc{Lines: nil, Version: 0}, nil
	}
	if err := m.RenameDoc("project-id-123", "doc1", "user", map[string]any{
		"id": "doc1", "newPathname": "new-pathname",
	}, "123"); err != nil {
		t.Fatalf("renameDoc not-cached: %v", err)
	}
	if len(c.calls) != 0 {
		t.Fatalf("not-cached renameDoc must not redis: %v", c.calls)
	}
}

// advancing returns a wall clock that reads `first` on the first call and
// `second` thereafter — the oracle's timer.start→done jump (the
// metrics.Timer timeSpan seam).
func advancing(first, second int64) func() int64 {
	n := 0
	return func() int64 {
		if n == 0 {
			n++
			return first
		}
		return second
	}
}
