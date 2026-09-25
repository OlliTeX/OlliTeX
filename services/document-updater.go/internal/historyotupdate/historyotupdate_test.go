package historyotupdate

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"document-updater/internal/errorsx"

	otc "ollitex/go/libraries/otc"
)

// --- raw wire op builders (vendored-go wire format; see probe) ------------------
//
// In the vendored Go otc lib, a raw text op is a bare primitive (positive int
// = retain, string = insert, negative int = remove), wrapped in
// {"textOperation": [...]}. A raw addComment op is
// {"commentId": id, "ranges": []any{{"pos":..,"length":..}}}. A raw no-op is
// {"noOp": true}.

func rawTextOp(ops ...any) map[string]any {
	return map[string]any{"textOperation": ops}
}

func rawAddComment(id string, pos, length int) map[string]any {
	ranges := []any{map[string]any{"pos": pos, "length": length}}
	return map[string]any{"commentId": id, "ranges": ranges}
}

func rawNoop() map[string]any { return map[string]any{"noOp": true} }

// --- shared seam recorder ------------------------------------------------------

type recs struct {
	persisted      map[string]any
	persistVers    int
	persistApplied []map[string]any
	persistMeta    map[string]any
	queued         []string
	flushedLen     int
	notifyTs       any
	notifyUser     any
	sent           []map[string]any
}

func (r *recs) wire(doc any, version int, pathname, docType string, found bool) *Manager {
	m := &Manager{}
	m.GetDoc = func(projectID, docID string) (any, int, string, string, bool, error) {
		return doc, version, pathname, docType, found, nil
	}
	m.UpdateDocument = func(projectID, docID string, docLines map[string]any, version int, appliedOps []map[string]any, ranges map[string]any, meta map[string]any) error {
		r.persisted = docLines
		r.persistVers = version
		r.persistApplied = appliedOps
		r.persistMeta = meta
		return nil
	}
	m.RecordProjectNotificationTimestamp = func(projectID string, timestamp, userID any) error {
		r.notifyTs = timestamp
		r.notifyUser = userID
		return nil
	}
	m.QueueOps = func(projectID string, ops []string) (int, error) {
		r.queued = append(r.queued, ops...)
		return len(r.queued), nil
	}
	m.RecordAndFlushHistoryOps = func(projectID string, updates []map[string]any, projectOpsLength int) {
		r.flushedLen = projectOpsLength
	}
	m.SendData = func(payload map[string]any) { r.sent = append(r.sent, payload) }
	m.Now = func() int64 { return 12345 }
	return m
}

func (r *recs) content() string {
	c, _ := r.persisted["content"].(string)
	return c
}

// The vendored otc serialises a persisted comment as
// {"id": id, "ranges": [{pos,长度}, ...]} with ranges []map[string]int.
func commentRangesOf(t *testing.T, raw map[string]any, id string) []any {
	t.Helper()
	comments, ok := raw["comments"].([]map[string]any)
	if !ok {
		t.Fatalf("persisted raw missing comments: %v", raw)
	}
	for _, c := range comments {
		if cid, ok := c["id"].(string); ok && cid == id {
			rng, _ := c["ranges"].([]map[string]int)
			out := make([]any, len(rng))
			for i, m := range rng {
				out[i] = m
			}
			return out
		}
	}
	t.Fatalf("comment %q not found in persisted raw: %v", id, raw)
	return nil
}

// ================================ oracle tests ===================================

func TestComposedMultiOpAppliesEveryOp(t *testing.T) {
	// Doc after a cut: " world" with comment "c1" detached (empty ranges).
	doc, _ := otc.NewStringFileData("hello world", nil, nil)
	doc.Comments.Add(otc.NewComment("c1", []otc.Range{{Pos: 0, Length: 5}}, false))
	cut := otc.NewTextOperation()
	_ = cut.Remove(5)
	_ = cut.Retain(6, otc.RetainBuilderOpts{})
	if err := doc.Edit(otc.NewTextEdit(cut)); err != nil {
		t.Fatalf("setup cut: %v", err)
	}
	if !doc.GetComments().GetComment("c1").IsEmpty() {
		t.Fatalf("setup: c1 should be empty after the cut")
	}

	r := &recs{}
	m := r.wire(doc.ToRaw(), 1, "/a/b/c.tex", "history-ot", true)

	// Paste "hello" at the end, then re-home the comment onto it.
	paste := rawTextOp(6, "hello")
	readd := rawAddComment("c1", 6, 5)
	update := &Update{Doc: "document-id-123", Op: []map[string]any{paste, readd}, V: 1, Meta: map[string]any{}}

	if err := m.ApplyUpdate("project-id-123", "document-id-123", update); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	if got := r.content(); got != " worldhello" {
		t.Fatalf("content: %q (want %q)", got, " worldhello")
	}
	c1 := commentRangesOf(t, r.persisted, "c1")
	if len(c1) != 1 {
		t.Fatalf("c1 ranges: %v", c1)
	}
	m0, _ := c1[0].(map[string]int)
	if m0["pos"] != 6 || m0["length"] != 5 {
		t.Fatalf("c1 range: %v (want pos=6 length=5)", m0)
	}
	if r.persistVers != 2 {
		t.Fatalf("persisted version: %d (want 2)", r.persistVers)
	}
	if len(r.persistApplied) != 1 {
		t.Fatalf("appliedOps: %v", r.persistApplied)
	}
	if r.notifyTs != int64(12345) {
		t.Fatalf("notify ts: %v", r.notifyTs)
	}
	if len(r.sent) != 1 {
		t.Fatalf("sent: %v", r.sent)
	}
	op, _ := r.sent[0]["op"].(map[string]any)
	if op == nil || op["v"] != 1 {
		t.Fatalf("sent op v: %v", r.sent[0])
	}
}

func TestRebasesAgainstConcurrentUpdate(t *testing.T) {
	// Server is at v2: a previous op inserted "ZZ" at the end.
	serverDoc, _ := otc.NewStringFileData("hello worldZZ", nil, nil)
	r := &recs{}
	m := r.wire(serverDoc.ToRaw(), 2, "/a/b/c.tex", "history-ot", true)

	previous := rawTextOp(11, "ZZ")
	m.GetPreviousDocOps = func(docID string, start, end int) ([]map[string]any, error) {
		if docID != "document-id-123" || start != 1 || end != 2 {
			t.Fatalf("getPreviousDocOps args: %s %d %d", docID, start, end)
		}
		return []map[string]any{{"doc": "document-id-123", "op": []any{previous}, "v": 1, "meta": map[string]any{}}}, nil
	}

	// Our update (based on v1) inserts "X" at the start and comments it.
	insert := rawTextOp("X", 11)
	addComment := rawAddComment("c1", 0, 1)
	update := &Update{Doc: "document-id-123", Op: []map[string]any{insert, addComment}, V: 1, Meta: map[string]any{}}

	if err := m.ApplyUpdate("project-id-123", "document-id-123", update); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	if got := r.content(); got != "Xhello worldZZ" {
		t.Fatalf("content: %q", got)
	}
	c1 := commentRangesOf(t, r.persisted, "c1")
	if len(c1) != 1 {
		t.Fatalf("c1 ranges: %v", c1)
	}
	m0, _ := c1[0].(map[string]int)
	if m0["pos"] != 0 || m0["length"] != 1 {
		t.Fatalf("c1 range: %v (want pos=0 length=1)", m0)
	}
	// The update op array is replaced with the rebased raw ops (2 ops remain).
	if len(update.Op) != 2 {
		t.Fatalf("rebased op count: %d", len(update.Op))
	}
}

func TestRebasesToNoopOnIdenticalConcurrent(t *testing.T) {
	// Server at v2: a concurrent client already added the same comment.
	serverDoc, _ := otc.NewStringFileData("hello world", nil, nil)
	serverDoc.Comments.Add(otc.NewComment("c1", []otc.Range{{Pos: 0, Length: 5}}, false))
	r := &recs{}
	m := r.wire(serverDoc.ToRaw(), 2, "/a/b/c.tex", "history-ot", true)

	concurrent := rawAddComment("c1", 0, 5)
	m.GetPreviousDocOps = func(docID string, start, end int) ([]map[string]any, error) {
		return []map[string]any{{"doc": "document-id-123", "op": []any{concurrent}, "v": 1, "meta": map[string]any{}}}, nil
	}

	addComment := rawAddComment("c1", 0, 5)
	update := &Update{Doc: "document-id-123", Op: []map[string]any{addComment}, V: 1, Meta: map[string]any{}}
	if err := m.ApplyUpdate("project-id-123", "document-id-123", update); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	// The rebased op is now a no-op.
	if len(update.Op) != 1 {
		t.Fatalf("rebased op count: %d (want 1)", len(update.Op))
	}
	if _, ok := update.Op[0]["noOp"]; !ok {
		t.Fatalf("rebased op must be noOp: %v", update.Op[0])
	}
	if got := r.content(); got != "hello world" {
		t.Fatalf("content: %q", got)
	}
	c1 := commentRangesOf(t, r.persisted, "c1")
	if len(c1) != 1 {
		t.Fatalf("c1 ranges: %v", c1)
	}
	m0, _ := c1[0].(map[string]int)
	if m0["pos"] != 0 || m0["length"] != 5 {
		t.Fatalf("c1 range: %v", m0)
	}
}

func TestThreadsConcurrentInsertThroughOurOps(t *testing.T) {
	// Server advanced to v2 by inserting "Z" at position 6 of "hello world".
	serverDoc, _ := otc.NewStringFileData("hello Zworld", nil, nil)
	r := &recs{}
	m := r.wire(serverDoc.ToRaw(), 2, "/a/b/c.tex", "history-ot", true)

	previous := rawTextOp(6, "Z", 5)
	m.GetPreviousDocOps = func(docID string, start, end int) ([]map[string]any, error) {
		return []map[string]any{{"doc": "document-id-123", "op": []any{previous}, "v": 1, "meta": map[string]any{}}}, nil
	}

	// Our update (v1): insert "AAA" at the start, then comment "hello" (now at
	// [3, 8)). The "Z" insert must be threaded past our "AAA" (to pos 9, after
	// the comment) so it leaves the comment alone; un-threaded (pos 6) it would
	// wrongly extend the comment.
	insert := rawTextOp("AAA", 11)
	addComment := rawAddComment("c1", 3, 5)
	update := &Update{Doc: "document-id-123", Op: []map[string]any{insert, addComment}, V: 1, Meta: map[string]any{}}
	if err := m.ApplyUpdate("project-id-123", "document-id-123", update); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	if got := r.content(); got != "AAAhello Zworld" {
		t.Fatalf("content: %q (want %q)", got, "AAAhello Zworld")
	}
	c1 := commentRangesOf(t, r.persisted, "c1")
	if len(c1) != 1 {
		t.Fatalf("c1 ranges: %v", c1)
	}
	m0, _ := c1[0].(map[string]int)
	if m0["pos"] != 3 || m0["length"] != 5 {
		t.Fatalf("c1 range: %v (want pos=3 length=5)", m0)
	}
}

func TestRebasesMixedSequences(t *testing.T) {
	// Concurrent update (theirs), v1 -> v2 on "hello world": append "1",
	// append "2", then comment "12". Edits stay at the end.
	base, _ := otc.NewStringFileData("hello world", nil, nil)
	t0, t1 := otc.NewTextOperation(), otc.NewTextOperation()
	_ = t0.Retain(11, otc.RetainBuilderOpts{})
	_ = t0.Insert("1", otc.InsertBuilderOpts{})
	_ = base.Edit(otc.NewTextEdit(t0))
	_ = t1.Retain(12, otc.RetainBuilderOpts{})
	_ = t1.Insert("2", otc.InsertBuilderOpts{})
	_ = base.Edit(otc.NewTextEdit(t1))
	theirComment, _ := otc.NewAddCommentOp("sc", []otc.Range{{Pos: 11, Length: 2}}, false)
	_ = base.Edit(theirComment)

	// Hand-built raw comment ops: typed AddCommentOp.ToJSON emits ranges as
	// []map[string]any, which otc's isRawAddComment rejects ([]any required);
	// the Node oracle's .toJSON() has no typed/erased-slice distinction, so
	// the hand-built raw form is the 1:1 wire fixture.
	theirCommentRaw := map[string]any{
		"commentId": "sc", "ranges": []any{map[string]any{"pos": 11, "length": 2}},
	}
	ourCommentRaw := map[string]any{
		"commentId": "c1", "ranges": []any{map[string]any{"pos": 1, "length": 5}},
	}

	r := &recs{}
	m := r.wire(base.ToRaw(), 2, "/a/b/c.tex", "history-ot", true)
	m.GetPreviousDocOps = func(docID string, start, end int) ([]map[string]any, error) {
		return []map[string]any{{"doc": "document-id-123", "op": []any{t0.ToJSON(), t1.ToJSON(), theirCommentRaw}, "v": 1, "meta": map[string]any{}}}, nil
	}

	// Our update (ours), based on v1: prepend "A", comment "hello", prepend "B".
	o0, o2 := otc.NewTextOperation(), otc.NewTextOperation()
	_ = o0.Insert("A", otc.InsertBuilderOpts{})
	_ = o0.Retain(11, otc.RetainBuilderOpts{})
	_ = o2.Insert("B", otc.InsertBuilderOpts{})
	_ = o2.Retain(12, otc.RetainBuilderOpts{})
	update := &Update{Doc: "document-id-123", Op: []map[string]any{o0.ToJSON(), ourCommentRaw, o2.ToJSON()}, V: 1, Meta: map[string]any{}}
	if err := m.ApplyUpdate("project-id-123", "document-id-123", update); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	if got := r.content(); got != "BAhello world12" {
		t.Fatalf("content: %q (want %q)", got, "BAhello world12")
	}
	c1 := commentRangesOf(t, r.persisted, "c1")
	if len(c1) != 1 {
		t.Fatalf("c1 ranges: %v", c1)
	}
	m0, _ := c1[0].(map[string]int)
	if m0["pos"] != 2 || m0["length"] != 5 {
		t.Fatalf("c1 range: %v (want pos=2 length=5)", m0)
	}
	if got := r.content(); got[2:7] != "hello" {
		t.Fatalf("content[2:7]: %q", got[2:7])
	}
}

func TestDupIfSourceSkipsApplyAndPersist(t *testing.T) {
	base, _ := otc.NewStringFileData("hello world", nil, nil)
	r := &recs{}
	m := r.wire(base.ToRaw(), 2, "/a/b/c.tex", "history-ot", true)
	m.GetPreviousDocOps = func(docID string, start, end int) ([]map[string]any, error) {
		previous := rawTextOp(11, "!")
		return []map[string]any{{"doc": "document-id-123", "op": []any{previous}, "v": 1, "meta": map[string]any{"source": "source-1"}}}, nil
	}

	addComment := rawAddComment("c1", 0, 5)
	update := &Update{
		Doc:         "document-id-123",
		Op:          []map[string]any{addComment},
		V:           1,
		Meta:        map[string]any{},
		DupIfSource: []string{"source-1"},
	}
	if err := m.ApplyUpdate("project-id-123", "document-id-123", update); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	if !update.Dup {
		t.Fatalf("update.Dup not set")
	}
	if r.persisted != nil {
		t.Fatalf("updateDocument must not be called on dup: %v", r.persisted)
	}
	if len(r.sent) != 1 {
		t.Fatalf("realtime broadcast must still happen: %v", r.sent)
	}
}

// ================================ guard + errors ==================================

func TestIsHistoryOTEditOperationUpdate(t *testing.T) {
	validText := rawTextOp(1, "x")
	validAdd := rawAddComment("c1", 0, 1)
	validNoop := rawNoop()
	shareJS := map[string]any{"p": 1, "t": "foo"} // raw sharejs component: invalid

	if !IsHistoryOTEditOperationUpdate(&Update{Doc: "d", Op: []map[string]any{validText}}) {
		t.Fatalf("text op must be valid")
	}
	if !IsHistoryOTEditOperationUpdate(&Update{Doc: "d", Op: []map[string]any{validText, validAdd, validNoop}}) {
		t.Fatalf("mixed ops must be valid")
	}
	if IsHistoryOTEditOperationUpdate(&Update{Doc: "d", Op: []map[string]any{shareJS}}) {
		t.Fatalf("raw sharejs component must be invalid")
	}
	if IsHistoryOTEditOperationUpdate(&Update{}) {
		t.Fatalf("empty op list must be invalid")
	}
	if IsHistoryOTEditOperationUpdate(&Update{Doc: "d", Op: []map[string]any{}}) {
		t.Fatalf("empty op list must be invalid")
	}
	if IsHistoryOTEditOperationUpdate(nil) {
		t.Fatalf("nil update must be invalid")
	}
}

func TestApplyUpdateDocumentNotFound(t *testing.T) {
	r := &recs{}
	m := r.wire(nil, 0, "", "sharejs-text-ot", false)
	err := m.ApplyUpdate("p", "d-missing", &Update{Doc: "d", Op: []map[string]any{rawTextOp(1)}, V: 1})
	var nf *errorsx.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %v", err)
	}
	if nf.Error() != "NotFoundError: document not found: d-missing" {
		t.Fatalf("rendered error: %q", nf.Error())
	}
	// The error is broadcast to realtime with the BARE message (vendor:
	// `error.message`), not the rendered "Type: message".
	if len(r.sent) != 1 {
		t.Fatalf("error broadcast: %v", r.sent)
	}
	if msg, _ := r.sent[0]["error"].(string); msg != "document not found: d-missing" {
		t.Fatalf("error payload: %v", r.sent[0])
	}
}

func TestApplyUpdateWrongType(t *testing.T) {
	r := &recs{}
	lines, _ := otc.NewStringFileData("hello", nil, nil)
	m := r.wire(lines.ToRaw(), 1, "/a.tex", "sharejs-text-ot", true)
	update := &Update{Doc: "d", Op: []map[string]any{rawTextOp("x")}, V: 1}
	err := m.ApplyUpdate("p", "d", update)
	var otm *errorsx.OTTypeMismatchError
	if !errors.As(err, &otm) {
		t.Fatalf("want OTTypeMismatchError, got %v", err)
	}
	if otm.Error() != "OTTypeMismatchError: ot type mismatch" {
		t.Fatalf("rendered error: %q", otm.Error())
	}
	if len(r.sent) != 1 {
		t.Fatalf("error broadcast missing: %v", r.sent)
	}
	if msg, _ := r.sent[0]["error"].(string); msg != "ot type mismatch" {
		t.Fatalf("error payload: %v", r.sent[0])
	}
}

func TestFromWire(t *testing.T) {
	raw := map[string]any{
		"doc":         "d1",
		"v":           7,
		"op":          []any{map[string]any{"textOperation": []any{1, "x"}}},
		"meta":        map[string]any{"source": "s1"},
		"dupIfSource": []any{"s1", "s2"},
		"dup":         true,
	}
	u := FromWire(raw)
	if u.Doc != "d1" || u.V != 7 || !u.Dup {
		t.Fatalf("u: %+v", u)
	}
	if len(u.Op) != 1 || len(u.DupIfSource) != 2 {
		t.Fatalf("u.Op: %v dupIf: %v", u.Op, u.DupIfSource)
	}
	if u.Meta["source"] != "s1" {
		t.Fatalf("meta: %v", u.Meta)
	}
}

func TestUpdateToRawShape(t *testing.T) {
	u := &Update{Doc: "d", Op: []map[string]any{rawNoop()}, V: 3, Meta: map[string]any{"source": "s"}}
	raw := u.ToRaw()
	if raw["doc"] != "d" || raw["v"] != 3 {
		t.Fatalf("raw: %v", raw)
	}
	if _, present := raw["dup"]; present {
		t.Fatalf("dup must be absent unless set: %v", raw)
	}
	u.Dup = true
	if u.ToRaw()["dup"] != true {
		t.Fatalf("dup must be present when set: %v", u.ToRaw())
	}
	// JSON round trip exercises the raw op's wire serialisation.
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(string(b), `"noOp"`) {
		t.Fatalf("op array: %s", b)
	}
}

func TestQueueSerializationIncludesPathname(t *testing.T) {
	base, _ := otc.NewStringFileData("hello", nil, nil)
	r := &recs{}
	m := r.wire(base.ToRaw(), 1, "/a/b.tex", "history-ot", true)
	update := &Update{Doc: "d", Op: []map[string]any{rawTextOp("x", 5)}, V: 1, Meta: map[string]any{}}
	if err := m.ApplyUpdate("p1", "d", update); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	if len(r.queued) != 1 {
		t.Fatalf("queued: %v", r.queued)
	}
	var q map[string]any
	if err := json.Unmarshal([]byte(r.queued[0]), &q); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	meta, _ := q["meta"].(map[string]any)
	if meta == nil || meta["pathname"] != "/a/b.tex" {
		t.Fatalf("queued(meta): %v", q)
	}
	if v, ok := q["v"].(float64); !ok || v != 1 {
		t.Fatalf("queued v: %v", q)
	}
	if q["doc"] != "d" {
		t.Fatalf("queued doc: %v", q)
	}
}

func TestNonfatalHistoryQueueError(t *testing.T) {
	base, _ := otc.NewStringFileData("hello", nil, nil)
	r := &recs{}
	m := r.wire(base.ToRaw(), 1, "/a.tex", "history-ot", true)
	errQueue := errors.New("redis down")
	m.QueueOps = func(projectID string, ops []string) (int, error) { return 0, errQueue }
	update := &Update{Doc: "d", Op: []map[string]any{rawTextOp("x", 5)}, V: 1}
	// Vendor: a queue error is caught and swallowed (history re-syncs); the
	// update is still broadcast and the ack returned.
	if err := m.ApplyUpdate("p1", "d", update); err != nil {
		t.Fatalf("queue error must be swallowed: %v", err)
	}
	if r.persisted == nil {
		t.Fatalf("document must still persist")
	}
}
