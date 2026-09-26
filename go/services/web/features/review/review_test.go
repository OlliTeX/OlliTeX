package review

// review_test.go — D40 P2: hermetic tests for the review-panel REST surface
// (go/services/web/features/review). Contract = the in-git review panel
// (threads-context.tsx / ranges-context.tsx / track-changes-state-context.tsx
// + services/web/types/review-panel shapes, D40-d5 pin). Domain semantics =
// go/services/collab/review.go + WEB_GO_STATE.md decision record.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ollitex/go/services/collab"
	"ollitex/go/services/web/core"

	"github.com/reearth/ygo/persistence"
)

const testPID = "aabbccddeeff001122334455"

type fakeRole map[string]collab.Role // key: "uid|pid"

func (f fakeRole) Fn(ctx context.Context, uid, pid string) collab.Role {
	return f[uid+"|"+pid]
}

func sessDoc(uid string) map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"passport": json.RawMessage(`{"user":{"_id":"` + uid + `"}}`),
	}
}

func cxt(method, path, uid string, body string, params map[string]string) *core.Cxt {
	var sess *core.Session
	if uid != "" {
		sess = &core.Session{Doc: sessDoc(uid)}
	}
	rd := io.NopCloser(strings.NewReader(body))
	p := map[string]string{"1": testPID}
	for k, v := range params {
		p[k] = v
	}
	return &core.Cxt{Req: httptest.NewRequest(method, path, rd), Sess: sess, Params: p}
}

type served struct {
	code int
	body string
}

type eventRec struct {
	name string
	args []any
}

type recEmit struct{ events *[]eventRec }

func (r *recEmit) Fn(ctx context.Context, pid, name string, args []any) error {
	*r.events = append(*r.events, eventRec{name: name, args: args})
	return nil
}

func serve(t *testing.T, c *core.Cxt, fn func(*core.Cxt, *core.Res)) served {
	t.Helper()
	rec := httptest.NewRecorder()
	fn(c, &core.Res{W: rec})
	b, _ := io.ReadAll(rec.Result().Body)
	return served{rec.Code, string(b)}
}

type trackMap struct {
	m map[string]bool
}

func (tm *trackMap) get(ctx context.Context, pid string) (map[string]bool, error) {
	if tm == nil || tm.m == nil {
		return map[string]bool{}, nil
	}
	return tm.m, nil
}

func (tm *trackMap) set(ctx context.Context, pid string, m map[string]bool) error {
	tm.m = m
	return nil
}

func setup(t *testing.T) (persistence.VersionedPersistence, *Handlers, *trackMap) {
	t.Helper()
	st := persistence.NewMemoryPersistence()
	if _, err := collab.SeedTextContent(context.Background(), st, testPID, "hello world this is a document"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	roles := fakeRole{
		"owner|" + testPID:    collab.ReadWrite,
		"user2|" + testPID:    collab.ReadWrite,
		"viewer|" + testPID:   collab.ReadOnly,
		"stranger|" + testPID: collab.Deny,
	}
	tm := &trackMap{m: map[string]bool{}}
	h := &Handlers{
		Store:   st,
		RoleFor: roles.Fn,
		UserFor: func(ctx context.Context, uid string) map[string]any {
			switch uid {
			case "owner":
				return map[string]any{"name": "Owner One", "email": "owner@e.test"}
			case "user2":
				return map[string]any{"name": "User Two", "email": "user2@e.test"}
			default:
				return map[string]any{"name": "User " + uid}
			}
		},
		TrackStateFor: tm.get,
		TrackStateSet: tm.set,
		Now:           func() int64 { return 1700000000000 },
	}
	return st, h, tm
}

// ---------- route table ----------

func TestRoutesRegistered(t *testing.T) {
	f := Feature(nil)
	if f.Name != "review" {
		t.Fatalf("name = %q", f.Name)
	}
	want := []string{
		"GET|/project/[a-fA-F0-9]{24}/threads",
		"POST|/project/[a-fA-F0-9]{24}/doc/.+/thread/.+/(resolve|reopen)",
		"DELETE|/project/[a-fA-F0-9]{24}/doc/.+/thread/.+",
		"POST|/project/[a-fA-F0-9]{24}/thread/.+/messages",
		"POST|/project/[a-fA-F0-9]{24}/thread/.+/messages/.+/edit",
		"DELETE|/project/[a-fA-F0-9]{24}/thread/.+/messages/.+",
		"DELETE|/project/[a-fA-F0-9]{24}/thread/.+/own-messages/.+",
		"POST|/project/[a-fA-F0-9]{24}/doc/.+/changes",
		"GET|/project/[a-fA-F0-9]{24}/doc/.+/changes",
		"POST|/project/[a-fA-F0-9]{24}/doc/.+/changes/accept",
		"POST|/project/[a-fA-F0-9]{24}/track_changes",
		"GET|/project/[a-fA-F0-9]{24}/ranges",
		"GET|/project/[a-fA-F0-9]{24}/changes/users",
	}
	if len(f.Routes) != len(want) {
		t.Fatalf("routes = %d, want %d", len(f.Routes), len(want))
	}
	seen := map[string]bool{}
	for _, r := range f.Routes {
		key := r.Method + "|" + r.Pattern.String()
		if _, dup := seen[key]; dup {
			t.Fatalf("duplicate route %s", key)
		}
		seen[key] = true
		if r.Pattern == nil {
			t.Fatalf("route %s without pattern", key)
		}
	}
}

// ---------- thread lifecycle (panel paths) ----------

// TestThreadFirstMessageCreatesThread — panel addComment: POST the first
// message to a client-generated thread id; the thread appears in the GET
// record keyed by that id with the pinned message shape.
func TestThreadFirstMessageCreatesThread(t *testing.T) {
	_, h, _ := setup(t)
	tid := "thr_0123456789abc"

	c := cxt(http.MethodPost, "/project/"+testPID+"/thread/"+tid+"/messages", "owner",
		`{"content":"why here?","doc":"main.tex","ranges":[{"start":1,"end":5}]}`, map[string]string{"2": tid})
	s := serve(t, c, h.messageAdd)
	if s.code != 201 {
		t.Fatalf("create code=%d body=%s", s.code, s.body)
	}
	var rec threadRecord
	if err := json.Unmarshal([]byte(s.body), &rec); err != nil {
		t.Fatalf("record not json: %v", err)
	}
	if rec.Doc != "main.tex" {
		t.Fatalf("doc = %q", rec.Doc)
	}
	if len(rec.Messages) != 1 || rec.Messages[0].Content != "why here?" {
		t.Fatalf("messages = %+v", rec.Messages)
	}
	m := rec.Messages[0]
	if m.UserID != "owner" || !m.User.IsSelf || m.User.Name != "Owner One" {
		t.Fatalf("user shape = %+v", m.User)
	}
	if !strings.HasSuffix(m.Timestamp, ".000Z") {
		t.Fatalf("timestamp %q not ISO-ms", m.Timestamp)
	}

	// ranges (P1 plain ranges, d1) are persisted on the message record
	var withRanges threadRecord
	_ = json.Unmarshal([]byte(s.body), &withRanges)
	if len(withRanges.Messages[0].Ranges) != 1 {
		t.Fatalf("ranges not persisted: %+v", withRanges.Messages[0])
	}
	rng, _ := withRanges.Messages[0].Ranges[0]["start"].(float64)
	if rng != 1 {
		t.Fatalf("range start = %v", withRanges.Messages[0].Ranges[0])
	}

	// GET threads → Record<threadId, Thread>
	c = cxt(http.MethodGet, "/project/"+testPID+"/threads", "owner", "", nil)
	s = serve(t, c, h.threadsList)
	if s.code != 200 {
		t.Fatalf("list code=%d body=%s", s.code, s.body)
	}
	var recs map[string]threadRecord
	if err := json.Unmarshal([]byte(s.body), &recs); err != nil {
		t.Fatalf("list not record: %v", err)
	}
	got, ok := recs[tid]
	if !ok {
		t.Fatalf("thread %s missing from %s", tid, s.body)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "why here?" {
		t.Fatalf("listed record = %+v", got)
	}
	if got.Resolved {
		t.Fatalf("fresh thread resolved: %+v", got)
	}
}

// TestThreadReplySecondMessage — a reply does not duplicate the thread.
func TestThreadReplySecondMessage(t *testing.T) {
	st, h, _ := setup(t)
	tid := "thr_reply12345678"
	c := cxt(http.MethodPost, "/project/"+testPID+"/thread/"+tid+"/messages", "owner",
		`{"content":"first","doc":"main.tex"}`, map[string]string{"2": tid})
	if s := serve(t, c, h.messageAdd); s.code != 201 {
		t.Fatalf("first: %d %s", s.code, s.body)
	}
	c = cxt(http.MethodPost, "/project/"+testPID+"/thread/"+tid+"/messages", "user2",
		`{"content":"second"}`, map[string]string{"2": tid})
	s := serve(t, c, h.messageAdd)
	if s.code != 201 {
		t.Fatalf("reply: %d %s", s.code, s.body)
	}
	var rec threadRecord
	_ = json.Unmarshal([]byte(s.body), &rec)
	if len(rec.Messages) != 2 || rec.Messages[1].User.ID != "user2" || rec.Messages[1].User.Name != "User Two" {
		t.Fatalf("reply record = %+v", rec)
	}
	ths, _ := collab.ListThreads(context.Background(), st, testPID)
	if len(ths) != 1 || ths[0].ID != tid {
		t.Fatalf("threads = %+v", ths)
	}
}

// TestResolveReopenEnvelope — pinned resolved fields (flat, per
// comment-thread.ts) + the actor persisted server-side (D40 decision).
func TestResolveReopenEnvelope(t *testing.T) {
	_, h, _ := setup(t)
	tid := "thr_resolve12345"
	c := cxt(http.MethodPost, "/project/"+testPID+"/thread/"+tid+"/messages", "owner",
		`{"content":"c","doc":"main.tex"}`, map[string]string{"2": tid})
	if s := serve(t, c, h.messageAdd); s.code != 201 {
		t.Fatalf("seed thread: %d %s", s.code, s.body)
	}
	s := serve(t, cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/thread/"+tid+"/resolve", "owner", "",
		map[string]string{"2": "main.tex", "3": tid, "4": "resolve"}), h.threadResolve)
	if s.code != 200 {
		t.Fatalf("resolve: %d %s", s.code, s.body)
	}
	var rec threadRecord
	_ = json.Unmarshal([]byte(s.body), &rec)
	if !rec.Resolved || rec.ResolvedByUID != "owner" {
		t.Fatalf("resolve record = %+v", rec)
	}
	if rec.ResolvedAt == "" || !strings.Contains(rec.ResolvedAt, "T") || !strings.HasSuffix(rec.ResolvedAt, "Z") {
		t.Fatalf("resolved_at = %q (want ISO-8601)", rec.ResolvedAt)
	}
	if rec.ResolvedByUser.Name != "Owner One" || rec.ResolvedByUser.Email != "owner@e.test" {
		t.Fatalf("resolved_by_user = %+v", rec.ResolvedByUser)
	}

	// reopen by another writer → resolved omitted, actor cleared
	s = serve(t, cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/thread/"+tid+"/reopen", "user2", "",
		map[string]string{"2": "main.tex", "3": tid, "4": "reopen"}), h.threadResolve)
	if s.code != 200 {
		t.Fatalf("reopen: %d %s", s.code, s.body)
	}
	var rec2 threadRecord
	_ = json.Unmarshal([]byte(s.body), &rec2)
	if rec2.Resolved {
		t.Fatalf("reopen record = %+v", rec2)
	}
}

// TestThreadGates — read role can list but not write; deny → 404 (existence
// not leaked); anonymous → 404.
func TestThreadRoleGates(t *testing.T) {
	_, h, _ := setup(t)
	c := cxt(http.MethodPost, "/project/"+testPID+"/thread/thr_gate123456789/messages", "viewer",
		`{"content":"x"}`, map[string]string{"2": "thr_gate123456789"})
	if s := serve(t, c, h.messageAdd); s.code != http.StatusNotFound {
		t.Fatalf("read-only add: %d %s", s.code, s.body)
	}
	c = cxt(http.MethodPost, "/project/"+testPID+"/thread/thr_gate123456789/messages", "stranger",
		`{"content":"x"}`, map[string]string{"2": "thr_gate123456789"})
	if s := serve(t, c, h.messageAdd); s.code != http.StatusNotFound {
		t.Fatalf("deny add: %d %s", s.code, s.body)
	}
	c = cxt(http.MethodPost, "/project/"+testPID+"/thread/thr_gate123456789/messages", "",
		`{"content":"x"}`, map[string]string{"2": "thr_gate123456789"})
	if s := serve(t, c, h.messageAdd); s.code != http.StatusNotFound {
		t.Fatalf("anon add: %d %s", s.code, s.body)
	}
	// read-only list still works
	c = cxt(http.MethodGet, "/project/"+testPID+"/threads", "viewer", "", nil)
	if s := serve(t, c, h.threadsList); s.code != http.StatusOK {
		t.Fatalf("read-only list: %d %s", s.code, s.body)
	}
}

// TestMessageEdit — pinned edit route; unknown message → 404.
func TestMessageEdit(t *testing.T) {
	st, h, _ := setup(t)
	tid := "thr_edit123456789"
	c := cxt(http.MethodPost, "/project/"+testPID+"/thread/"+tid+"/messages", "owner",
		`{"id":"msg_123456789","content":"before","doc":"main.tex"}`, map[string]string{"2": tid})
	if s := serve(t, c, h.messageAdd); s.code != 201 {
		t.Fatalf("seed: %d %s", s.code, s.body)
	}
	c = cxt(http.MethodPost, "/project/"+testPID+"/thread/"+tid+"/messages/msg_123456789/edit", "owner",
		`{"content":"after"}`, map[string]string{"2": tid, "3": "msg_123456789"})
	s := serve(t, c, h.messageEdit)
	if s.code != 200 || !strings.Contains(s.body, `"after"`) {
		t.Fatalf("edit: %d %s", s.code, s.body)
	}
	msgs, _ := collab.MessagesOfThread(context.Background(), st, testPID, tid)
	if len(msgs) != 1 || msgs[0].Text != "after" {
		t.Fatalf("domain msg = %+v", msgs)
	}
	c = cxt(http.MethodPost, "/project/"+testPID+"/thread/"+tid+"/messages/msg_nope/edit", "owner",
		`{"content":"x"}`, map[string]string{"2": tid, "3": "msg_nope"})
	if s := serve(t, c, h.messageEdit); s.code != http.StatusNotFound {
		t.Fatalf("edit missing: %d %s", s.code, s.body)
	}
}

// TestOwnMessageRule — own-messages route enforces authorship (403 on
// foreign authors); the plain messages route stays open at write role.
func TestOwnMessageRule(t *testing.T) {
	_, h, _ := setup(t)
	tid := "thr_own1234567890"
	c := cxt(http.MethodPost, "/project/"+testPID+"/thread/"+tid+"/messages", "owner",
		`{"id":"msg_own123","content":"mine","doc":"main.tex"}`, map[string]string{"2": tid})
	if s := serve(t, c, h.messageAdd); s.code != 201 {
		t.Fatalf("seed: %d %s", s.code, s.body)
	}
	c = cxt(http.MethodDelete, "/project/"+testPID+"/thread/"+tid+"/own-messages/msg_own123", "user2", "",
		map[string]string{"2": tid, "3": "msg_own123"})
	if s := serve(t, c, h.ownMessageDelete); s.code != http.StatusForbidden {
		t.Fatalf("foreign own-delete: %d %s", s.code, s.body)
	}
	c = cxt(http.MethodDelete, "/project/"+testPID+"/thread/"+tid+"/messages/msg_own123", "user2", "",
		map[string]string{"2": tid, "3": "msg_own123"})
	if s := serve(t, c, h.messageDelete); s.code != http.StatusOK {
		t.Fatalf("write-role plain delete: %d %s", s.code, s.body)
	}
}

// TestDeleteThread — cascades messages; gone from the list.
func TestDeleteThread(t *testing.T) {
	st, h, _ := setup(t)
	tid := "thr_del1234567890"
	c := cxt(http.MethodPost, "/project/"+testPID+"/thread/"+tid+"/messages", "owner",
		`{"content":"x","doc":"main.tex"}`, map[string]string{"2": tid})
	if s := serve(t, c, h.messageAdd); s.code != 201 {
		t.Fatalf("seed: %d %s", s.code, s.body)
	}
	c = cxt(http.MethodDelete, "/project/"+testPID+"/doc/main.tex/thread/"+tid, "owner", "",
		map[string]string{"2": "main.tex", "3": tid})
	if s := serve(t, c, h.threadDelete); s.code != http.StatusOK {
		t.Fatalf("delete: %d %s", s.code, s.body)
	}
	msgs, _ := collab.MessagesOfThread(context.Background(), st, testPID, tid)
	if len(msgs) != 0 {
		t.Fatalf("messages survived: %d", len(msgs))
	}
	if s := serve(t, cxt(http.MethodGet, "/project/"+testPID+"/threads", "owner", "", nil), h.threadsList); strings.Contains(s.body, tid) {
		t.Fatalf("thread still listed: %s", s.body)
	}
}

// ---------- track changes ----------

// TestTrackChangesOnFor — panel body {on_for, on_for_guests} → explicit map
// (Node parity: project.track_changes) + echo + merge semantics.
func TestTrackChangesOnFor(t *testing.T) {
	_, h, tm := setup(t)
	c := cxt(http.MethodPost, "/project/"+testPID+"/track_changes", "owner",
		`{"on_for":{"owner":true},"on_for_guests":true}`, nil)
	s := serve(t, c, h.trackChanges)
	if s.code != 200 {
		t.Fatalf("track: %d %s", s.code, s.body)
	}
	var out struct {
		ProjectID    string          `json:"project_id"`
		TrackChanges map[string]bool `json:"track_changes"`
	}
	if err := json.Unmarshal([]byte(s.body), &out); err != nil {
		t.Fatalf("body: %v", err)
	}
	if out.ProjectID != testPID || !out.TrackChanges["owner"] || !out.TrackChanges["__guests__"] {
		t.Fatalf("out = %+v", out)
	}
	if got, _ := tm.get(context.Background(), testPID); !got["owner"] || !got["__guests__"] {
		t.Fatalf("persisted map = %v", got)
	}
	// merge: toggle user2 on, owner stays on
	c = cxt(http.MethodPost, "/project/"+testPID+"/track_changes", "owner",
		`{"on_for":{"user2":true}}`, nil)
	s = serve(t, c, h.trackChanges)
	if s.code != 200 {
		t.Fatalf("track2: %d %s", s.code, s.body)
	}
	if got, _ := tm.get(context.Background(), testPID); !got["owner"] || !got["user2"] || !got["__guests__"] {
		t.Fatalf("merged map = %v", got)
	}
	// read role is enough (panel saves as member)
	c = cxt(http.MethodPost, "/project/"+testPID+"/track_changes", "viewer",
		`{"on_for":{"viewer":true}}`, nil)
	if s := serve(t, c, h.trackChanges); s.code != 200 {
		t.Fatalf("viewer track: %d %s", s.code, s.body)
	}
}

// ---------- changes ----------

func TestChangesCreateAccept(t *testing.T) {
	st, h, _ := setup(t)
	c := cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/changes", "owner",
		`{"content":"inserted text","start":6,"end":6}`, map[string]string{"2": "main.tex"})
	s := serve(t, c, h.changesCreate)
	if s.code != 201 {
		t.Fatalf("create: %d %s", s.code, s.body)
	}
	var ch map[string]any
	_ = json.Unmarshal([]byte(s.body), &ch)
	if ch["kind"] != "insert" || ch["state"] != "pending" || ch["content"] != "inserted text" {
		t.Fatalf("change = %v", ch)
	}
	id, _ := ch["change_id"].(string)
	if id == "" {
		t.Fatalf("no change_id: %s", s.body)
	}

	// delete kind inferred from empty content
	c = cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/changes", "owner",
		`{"start":0,"end":5}`, map[string]string{"2": "main.tex"})
	s = serve(t, c, h.changesCreate)
	if s.code != 201 {
		t.Fatalf("create del: %d %s", s.code, s.body)
	}
	var ch2 map[string]any
	_ = json.Unmarshal([]byte(s.body), &ch2)
	if ch2["kind"] != "delete" {
		t.Fatalf("change2 = %v", ch2)
	}

	// accept (bulk, panel body {change_ids})
	c = cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/changes/accept", "owner",
		`{"change_ids":["`+id+`","nope_1234567890"]}`, map[string]string{"2": "main.tex"})
	s = serve(t, c, h.changesAccept)
	if s.code != 200 {
		t.Fatalf("accept: %d %s", s.code, s.body)
	}
	var acc map[string]any
	_ = json.Unmarshal([]byte(s.body), &acc)
	if acc["accepted"] != float64(1) {
		t.Fatalf("accepted = %v", acc)
	}
	chs, _ := collab.ListChanges(context.Background(), st, testPID)
	if len(chs) != 2 {
		t.Fatalf("changes = %+v", chs)
	}
	for _, x := range chs {
		if x.ID == id && x.State != "accepted" {
			t.Fatalf("state = %q", x.State)
		}
	}
}

func TestChangesGates(t *testing.T) {
	_, h, _ := setup(t)
	c := cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/changes", "viewer",
		`{"content":"x","start":0,"end":1}`, map[string]string{"2": "main.tex"})
	if s := serve(t, c, h.changesCreate); s.code != http.StatusNotFound {
		t.Fatalf("viewer create: %d %s", s.code, s.body)
	}
	c = cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/changes/accept", "viewer",
		`{"change_ids":["a"]}`, map[string]string{"2": "main.tex"})
	if s := serve(t, c, h.changesAccept); s.code != http.StatusNotFound {
		t.Fatalf("viewer accept: %d %s", s.code, s.body)
	}
	c = cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/changes", "owner",
		`{bad json`, map[string]string{"2": "main.tex"})
	if s := serve(t, c, h.changesCreate); s.code != http.StatusBadRequest {
		t.Fatalf("bad body: %d %s", s.code, s.body)
	}
}

// TestChangesList — d10 read path: GET the room's tracked-change records
// (creation order, wire superset incl. state + author + timestamp_ms).
func TestChangesList(t *testing.T) {
	_, h, _ := setup(t)
	c := cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/changes", "owner",
		`{"content":"inserted text","start":6,"end":6}`, map[string]string{"2": "main.tex"})
	s := serve(t, c, h.changesCreate)
	if s.code != 201 {
		t.Fatalf("create: %d %s", s.code, s.body)
	}
	var ch map[string]any
	_ = json.Unmarshal([]byte(s.body), &ch)
	id, _ := ch["change_id"].(string)

	c = cxt(http.MethodGet, "/project/"+testPID+"/doc/main.tex/changes", "owner",
		"", map[string]string{"2": "main.tex"})
	s = serve(t, c, h.changesList)
	if s.code != 200 {
		t.Fatalf("list: %d %s", s.code, s.body)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(s.body), &list); err != nil {
		t.Fatalf("list json: %v (%s)", err, s.body)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d: %s", len(list), s.body)
	}
	if list[0]["id"] != id || list[0]["kind"] != "insert" || list[0]["state"] != "pending" {
		t.Fatalf("entry = %v", list[0])
	}
	if list[0]["start"].(float64) != 6 || list[0]["timestamp_ms"].(float64) <= 0 {
		t.Fatalf("entry fields = %v", list[0])
	}

	// role gate: read role (ReadOnly) passes; below-role (Deny) → 404
	c = cxt(http.MethodGet, "/project/"+testPID+"/doc/main.tex/changes", "viewer",
		"", map[string]string{"2": "main.tex"})
	if s := serve(t, c, h.changesList); s.code != 200 {
		t.Fatalf("viewer list: %d %s", s.code, s.body)
	}
	c = cxt(http.MethodGet, "/project/"+testPID+"/doc/main.tex/changes", "stranger",
		"", map[string]string{"2": "main.tex"})
	if s := serve(t, c, h.changesList); s.code != http.StatusNotFound {
		t.Fatalf("stranger list: %d %s", s.code, s.body)
	}
}

// TestRelayEvents — the panel's local state syncs via room events; the
// handlers must relay the pinned event + payload for every mutation
// (listener signatures pinned from threads-context.tsx /
// ranges-context.tsx / track-changes-state-context.tsx).
func TestRelayEvents(t *testing.T) {
	var evs []eventRec
	h := &Handlers{
		Store:   persistence.NewMemoryPersistence(),
		RoleFor: fakeRole{"owner|" + testPID: collab.ReadWrite}.Fn,
		TrackStateFor: func(ctx context.Context, pid string) (map[string]bool, error) {
			return map[string]bool{}, nil
		},
		TrackStateSet: func(ctx context.Context, pid string, m map[string]bool) error { return nil },
		UserFor: func(ctx context.Context, uid string) map[string]any {
			return map[string]any{"name": "Owner One", "email": "owner@e.test"}
		},
		Emit: (&recEmit{events: &evs}).Fn,
		Now:  func() int64 { return 1700000000000 },
	}
	tid := "thr_evt1234567890"

	serve(t, cxt(http.MethodPost, "/project/"+testPID+"/thread/"+tid+"/messages", "owner",
		`{"content":"hi"}`, map[string]string{"2": tid}), h.messageAdd)
	serve(t, cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/thread/"+tid+"/resolve", "owner", "",
		map[string]string{"2": "main.tex", "3": tid, "4": "resolve"}), h.threadResolve)
	serve(t, cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/thread/"+tid+"/reopen", "owner", "",
		map[string]string{"2": "main.tex", "3": tid, "4": "reopen"}), h.threadResolve)
	srv := serve(t, cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/changes", "owner",
		`{"content":"x","start":1,"end":1}`, map[string]string{"2": "main.tex"}), h.changesCreate)
	var ch map[string]any
	_ = json.Unmarshal([]byte(srv.body), &ch)
	servedID, _ := ch["change_id"].(string)
	serve(t, cxt(http.MethodPost, "/project/"+testPID+"/doc/main.tex/changes/accept", "owner",
		`{"change_ids":["`+servedID+`"]}`, map[string]string{"2": "main.tex"}), h.changesAccept)
	serve(t, cxt(http.MethodPost, "/project/"+testPID+"/track_changes", "owner",
		`{"on_for":{"owner":true}}`, nil), h.trackChanges)
	serve(t, cxt(http.MethodDelete, "/project/"+testPID+"/doc/main.tex/thread/"+tid, "owner", "",
		map[string]string{"2": "main.tex", "3": tid}), h.threadDelete)

	names := []string{}
	for _, e := range evs {
		names = append(names, e.name)
	}
	want := []string{"new-comment", "resolve-thread", "reopen-thread", "accept-changes", "toggle-track-changes", "delete-thread"}
	if len(evs) != len(want) {
		t.Fatalf("events = %v", names)
	}
	for i, w := range want {
		if evs[i].name != w {
			t.Fatalf("event[%d] = %v, want %s (all=%v)", i, evs[i].name, w, names)
		}
	}
	// payload pins
	nc := evs[0].args
	if nc[0] != tid {
		t.Fatalf("new-comment args = %v", nc)
	}
	cm, _ := nc[1].(map[string]any)
	if cm["content"] != "hi" || cm["timestamp"] != int64(1700000000000) {
		t.Fatalf("new-comment comment = %v", cm)
	}
	if evs[1].args[0] != tid {
		t.Fatalf("resolve-thread args = %v", evs[1].args)
	}
	ru, _ := evs[1].args[1].(map[string]any)
	if ru["id"] != "owner" || ru["first_name"] != "Owner" || ru["email"] != "owner@e.test" {
		t.Fatalf("resolve-thread user = %v", ru)
	}
	if evs[3].args[0] != "main.tex" {
		t.Fatalf("accept-changes doc = %v", evs[3].args)
	}
	if evs[4].args[0].(map[string]bool)["owner"] != true {
		t.Fatalf("toggle-track-changes state = %v", evs[4].args)
	}
	if evs[5].args[0] != tid {
		t.Fatalf("delete-thread args = %v", evs[5].args)
	}
}

func TestMessageAddBadBody(t *testing.T) {
	_, h, _ := setup(t)
	c := cxt(http.MethodPost, "/project/"+testPID+"/thread/thr_bad123456789/messages", "owner",
		`{"content":"   "}`, map[string]string{"2": "thr_bad123456789"})
	if s := serve(t, c, h.messageAdd); s.code != http.StatusBadRequest {
		t.Fatalf("blank content: %d %s", s.code, s.body)
	}
	c = cxt(http.MethodPost, "/project/"+testPID+"/thread/thr_bad123456789/messages", "owner",
		`not json`, map[string]string{"2": "thr_bad123456789"})
	if s := serve(t, c, h.messageAdd); s.code != http.StatusBadRequest {
		t.Fatalf("bad json: %d %s", s.code, s.body)
	}
}

// TestTimestampPinned — the pinned clock renders in both message + thread
// timestamps (ISO-ms, JS Date parseable).
func TestTimestampPinned(t *testing.T) {
	_, h, _ := setup(t)
	tid := "thr_ts1234567890"
	c := cxt(http.MethodPost, "/project/"+testPID+"/thread/"+tid+"/messages", "owner",
		`{"content":"c"}`, map[string]string{"2": tid})
	s := serve(t, c, h.messageAdd)
	if s.code != 201 {
		t.Fatalf("seed: %d %s", s.code, s.body)
	}
	if !strings.Contains(s.body, "2023-11-14T22:13:20.000Z") { // 1700000000000ms, pinned clock
		t.Fatalf("timestamp not from pinned clock: %s", s.body)
	}
}

// ---------- D40-d12: the panel Changes-tab legacy endpoints ----------

// TestRangesList — GET /project/:pid/ranges must return the panel's
// Changes-tab wire shape (pinned from use-project-ranges.ts +
// review-panel-change.tsx / review-panel-overview-file.tsx), built from
// the room's Y.Doc (insert entries {op:{i,p}}, delete entries {op:{d,p}},
// comment pointers {op:{t,p:0}, resolved}), role-gated read.
func TestRangesList(t *testing.T) {
	_, h, _ := setup(t)
	ctx := context.Background()
	if _, _, _, err := collab.AddThread(ctx, h.Store, testPID, collab.Thread{
		ID: "thr-1", File: "main.tex", State: "opened",
		Author: map[string]any{"user_id": "owner"},
	}); err != nil {
		t.Fatalf("thread: %v", err)
	}
	if _, _, _, err := collab.AddComment(ctx, h.Store, testPID, collab.Comment{
		ThreadID: "thr-1", File: "main.tex", Text: "a question",
		Author: map[string]any{"user_id": "owner"},
	}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if _, _, _, err := collab.SetThreadStateBy(ctx, h.Store, testPID, "thr-1", "resolved", map[string]any{"user_id": "user2"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, _, _, err := collab.AddThread(ctx, h.Store, testPID, collab.Thread{
		ID: "thr-2", File: "main.tex", State: "opened",
		Author: map[string]any{"user_id": "user2"},
	}); err != nil {
		t.Fatalf("thread2: %v", err)
	}
	if _, _, _, err := collab.AddComment(ctx, h.Store, testPID, collab.Comment{
		ThreadID: "thr-2", File: "main.tex", Text: "an open question",
		Author: map[string]any{"user_id": "user2"},
	}); err != nil {
		t.Fatalf("comment2: %v", err)
	}
	if _, _, _, err := collab.AddChange(ctx, h.Store, testPID, collab.TrackedChange{
		ID: "chg-i1", Kind: "insert", File: "main.tex", Start: 6, End: 6,
		Content: "inserted text", Author: map[string]any{"user_id": "owner"},
	}); err != nil {
		t.Fatalf("addChange ins: %v", err)
	}
	if _, _, _, err := collab.AddChange(ctx, h.Store, testPID, collab.TrackedChange{
		ID: "chg-d1", Kind: "delete", File: "main.tex", Start: 2, End: 5,
		Content: "del text", Author: map[string]any{"user_id": "user2"},
	}); err != nil {
		t.Fatalf("addChange del: %v", err)
	}

	c := cxt(http.MethodGet, "/project/"+testPID+"/ranges", "owner", "", map[string]string{})
	s := serve(t, c, h.rangesList)
	if s.code != 200 {
		t.Fatalf("rangesList: %d %s", s.code, s.body)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(s.body), &arr); err != nil {
		t.Fatalf("json: %v (%s)", err, s.body)
	}
	if len(arr) != 1 || arr[0]["id"] != "main.tex" {
		t.Fatalf("entries = %s", s.body)
	}
	rng, _ := arr[0]["ranges"].(map[string]any)
	if rng == nil {
		t.Fatalf("no ranges object: %s", s.body)
	}
	changes, _ := rng["changes"].([]any)
	comments, _ := rng["comments"].([]any)
	if len(changes) != 2 {
		t.Fatalf("changes len = %d: %s", len(changes), s.body)
	}
	if len(comments) != 2 {
		t.Fatalf("comments len = %d: %s", len(comments), s.body)
	}
	// insert entry: {id, op:{i, p}, state, metadata}
	var insMap, delMap, insMsg, openMsg map[string]any
	for _, raw := range comments {
		e := raw.(map[string]any)
		op := e["op"].(map[string]any)
		switch op["t"] {
		case "thr-1":
			insMsg = e
		case "thr-2":
			openMsg = e
		}
	}
	if insMsg == nil || openMsg == nil {
		t.Fatalf("comment pointers missing: %s", s.body)
	}
	if insMsg["resolved"] != true {
		t.Fatalf("thr-1 should be resolved: %v", insMsg)
	}
	if openMsg["resolved"] != false {
		t.Fatalf("thr-2 should be open: %v", openMsg)
	}
	for _, raw := range changes {
		e := raw.(map[string]any)
		op := e["op"].(map[string]any)
		if e["id"] == "chg-i1" {
			insMap = e
			if op["i"] != "inserted text" {
				t.Fatalf("insert op.i = %v", op)
			}
			if op["p"].(float64) != 6 {
				t.Fatalf("insert op.p = %v", op)
			}
			md, _ := e["metadata"].(map[string]any)
			if md == nil || md["user_id"] != "owner" {
				t.Fatalf("insert metadata = %v", e)
			}
		}
		if e["id"] == "chg-d1" {
			delMap = e
			if op["d"] != "del text" {
				t.Fatalf("delete op.d = %v (d11b: deleted text carried)", op)
			}
			if op["p"].(float64) != 2 {
				t.Fatalf("delete op.p = %v", op)
			}
		}
	}
	if insMap == nil || delMap == nil {
		t.Fatalf("change entries missing: %s", s.body)
	}
	// role gate: read passes, below → 404
	if s := serve(t, cxt(http.MethodGet, "/project/"+testPID+"/ranges", "viewer", "", nil), h.rangesList); s.code != 200 {
		t.Fatalf("viewer ranges: %d %s", s.code, s.body)
	}
	if s := serve(t, cxt(http.MethodGet, "/project/"+testPID+"/ranges", "stranger", "", nil), h.rangesList); s.code != http.StatusNotFound {
		t.Fatalf("stranger ranges: %d %s", s.code, s.body)
	}
}

// TestChangesUsers — GET /project/:pid/changes/users returns the distinct
// authors (comments + changes) in the panel's ChangesUser shape.
func TestChangesUsers(t *testing.T) {
	_, h, _ := setup(t)
	ctx := context.Background()
	if _, _, _, err := collab.AddThread(ctx, h.Store, testPID, collab.Thread{
		ID: "thr-1", File: "main.tex", State: "opened",
		Author: map[string]any{"user_id": "owner"},
	}); err != nil {
		t.Fatalf("thread: %v", err)
	}
	if _, _, _, err := collab.AddComment(ctx, h.Store, testPID, collab.Comment{
		ThreadID: "thr-1", File: "main.tex", Text: "hi",
		Author: map[string]any{"user_id": "owner"},
	}); err != nil {
		t.Fatalf("comment: %v", err)
	}
	if _, _, _, err := collab.AddChange(ctx, h.Store, testPID, collab.TrackedChange{
		ID: "chg-1", Kind: "insert", File: "main.tex", Start: 1, End: 1,
		Content: "x", Author: map[string]any{"user_id": "user2"},
	}); err != nil {
		t.Fatalf("addChange: %v", err)
	}
	c := cxt(http.MethodGet, "/project/"+testPID+"/changes/users", "owner", "", nil)
	s := serve(t, c, h.changesUsers)
	if s.code != 200 {
		t.Fatalf("changesUsers: %d %s", s.code, s.body)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(s.body), &arr); err != nil {
		t.Fatalf("json: %v (%s)", err, s.body)
	}
	if len(arr) != 2 {
		t.Fatalf("users len = %d: %s", len(arr), s.body)
	}
	byID := map[string]map[string]any{}
	for _, u := range arr {
		byID[u["id"].(string)] = u
	}
	if byID["owner"]["email"] != "owner@e.test" {
		t.Fatalf("owner user shape = %v", byID["owner"])
	}
	if byID["user2"] == nil || byID["user2"]["email"] != "user2@e.test" {
		t.Fatalf("user2 user shape = %v", byID)
	}
	if s := serve(t, cxt(http.MethodGet, "/project/"+testPID+"/changes/users", "stranger", "", nil), h.changesUsers); s.code != http.StatusNotFound {
		t.Fatalf("stranger: %d %s", s.code, s.body)
	}
}
