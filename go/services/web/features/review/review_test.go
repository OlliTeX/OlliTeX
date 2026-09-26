package review

// review_test.go — D40 P2: hermetic tests for the V1 threads / track-changes
// REST surface (go/services/web/features/review). Contract = the in-git
// review panel (services/web/types/review-panel shapes + the panel's calls);
// domain semantics = the D40 decision record (WEB_GO_STATE.md).

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

func serve(t *testing.T, h *Handlers, c *core.Cxt, fn func(*core.Cxt, *core.Res)) served {
	t.Helper()
	rec := httptest.NewRecorder()
	fn(c, &core.Res{W: rec})
	b, _ := io.ReadAll(rec.Result().Body)
	return served{rec.Code, string(b)}
}

func setup(t *testing.T) (persistence.VersionedPersistence, *Handlers) {
	t.Helper()
	st := persistence.NewMemoryPersistence()
	seedRoom(t, st)
	roles := fakeRole{
		"owner|" + testPID:    collab.ReadWrite,
		"user2|" + testPID:    collab.ReadWrite,
		"viewer|" + testPID:   collab.ReadOnly,
		"stranger|" + testPID: collab.Deny,
	}
	track := true
	h := &Handlers{
		Store:   st,
		RoleFor: roles.Fn,
		UserFor: func(ctx context.Context, uid string) map[string]any {
			if uid == "owner" {
				return map[string]any{"name": "Owner One", "email": "owner@e.test"}
			}
			return map[string]any{"name": "User Two", "email": "user@e.test"}
		},
		TrackFor:    func(ctx context.Context, uid string) (bool, error) { return track, nil },
		SetTrackFor: func(ctx context.Context, uid string, enabled bool) error { track = enabled; return nil },
		Now:         func() int64 { return 1700000000000 },
	}
	return st, h
}

func seedRoom(t *testing.T, st persistence.VersionedPersistence) {
	t.Helper()
	if _, err := collab.SeedTextContent(context.Background(), st, testPID, "hello world this is a document"); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestRoutesRegistered(t *testing.T) {
	f := Feature(nil)
	if f.Name != "review" {
		t.Fatalf("name = %q", f.Name)
	}
	if len(f.Routes) != 10 {
		t.Fatalf("routes = %d, want 10", len(f.Routes))
	}
}

func TestThreadCreateListEnvelope(t *testing.T) {
	_, h := setup(t)
	c := cxt(http.MethodPost, "/project/"+testPID+"/threads", "owner",
		`{"content":"why here?","doc":"main.tex","ranges":[{"start":1,"end":5}]}`, nil)
	s := serve(t, h, c, h.threadCreate)
	if s.code != 201 {
		t.Fatalf("create code=%d body=%s", s.code, s.body)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(s.body), &created); err != nil {
		t.Fatalf("create body not json: %v", err)
	}
	if created["doc"] != "main.tex" || created["state"] != "opened" {
		t.Fatalf("envelope: %v", created)
	}
	au, _ := created["author"].(map[string]any)
	if au["id"] != "owner" || au["isSelf"] != true || au["email"] != "owner@e.test" {
		t.Fatalf("author shape: %v", au)
	}
	// created = millisecond ISO of the fixed clock.
	if created["created"] != "2023-11-14T22:13:20.000Z" {
		t.Fatalf("created = %v (want fixed ISO)", created["created"])
	}
	msgs, _ := created["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages: %v", created["messages"])
	}
	m0, _ := msgs[0].(map[string]any)
	if m0["content"] != "why here?" || m0["user_id"] != "owner" {
		t.Fatalf("message: %v", m0)
	}
	u0, _ := m0["user"].(map[string]any)
	for _, k := range []any{"avatar_text", "email", "hue", "id", "isSelf", "name"} {
		if _, ok := u0[k.(string)]; !ok {
			t.Fatalf("user shape missing %v: %v", k, u0)
		}
	}
	if created["resolved"] != nil {
		t.Fatalf("unresolved thread must omit resolved: %v", created["resolved"])
	}

	// GET lists it (in the envelope the panel consumes).
	c2 := cxt(http.MethodGet, "/project/"+testPID+"/threads", "owner", "", nil)
	s2 := serve(t, h, c2, h.threadsList)
	if s2.code != 200 {
		t.Fatalf("list code=%d", s2.code)
	}
	var list struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.Unmarshal([]byte(s2.body), &list); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(list.Threads) != 1 || list.Threads[0]["id"] != created["id"] {
		t.Fatalf("list: %s", s2.body)
	}
}

func TestMessageReplyEditDelete(t *testing.T) {
	_, h := setup(t)
	// create thread (owner) + a reply from user2.
	th := serve(t, h, cxt(http.MethodPost, "/p", "owner", `{"content":"q?","doc":"main.tex"}`, nil), h.threadCreate)
	threadID, _ := extractID(t, th.body)
	reply := serve(t, h, cxt(http.MethodPost, "/p", "user2", `{"content":"a!"}`, map[string]string{
		"2": threadID,
	}), h.messageAdd)
	if reply.code != 201 {
		t.Fatalf("reply code=%d body=%s", reply.code, reply.body)
	}
	cid, _ := extractID(t, reply.body)

	// edit by its author — content updates (200), unknown ids → 404.
	edit := serve(t, h, cxt(http.MethodPost, "/p", "user2", `{"content":"a? (edited)"}`, map[string]string{
		"2": threadID, "3": cid,
	}), h.messageEdit)
	if edit.code != 200 {
		t.Fatalf("edit code=%d body=%s", edit.code, edit.body)
	}
	var edited map[string]any
	json.Unmarshal([]byte(edit.body), &edited)
	if edited["content"] != "a? (edited)" {
		t.Fatalf("edited: %v", edited)
	}
	badEdit := serve(t, h, cxt(http.MethodPost, "/p", "user2", `{"content":"x"}`, map[string]string{
		"2": threadID, "3": "0000000000000",
	}), h.messageEdit)
	if badEdit.code != 404 {
		t.Fatalf("edit unknown = %d (want 404)", badEdit.code)
	}

	// own-message rule: the non-author is forbidden; the author succeeds.
	foreign := serve(t, h, cxt(http.MethodDelete, "/p", "owner", "", map[string]string{
		"2": threadID, "3": cid,
	}), h.ownMessageDelete)
	if foreign.code != 403 {
		t.Fatalf("own-delete foreign = %d (want 403)", foreign.code)
	}
	own := serve(t, h, cxt(http.MethodDelete, "/p", "user2", "", map[string]string{
		"2": threadID, "3": cid,
	}), h.ownMessageDelete)
	if own.code != 200 {
		t.Fatalf("own-delete own = %d (want 200)", own.code)
	}
}

func TestThreadResolveReopen(t *testing.T) {
	_, h := setup(t)
	th := serve(t, h, cxt(http.MethodPost, "/p", "owner", `{"content":"q?","doc":"main.tex"}`, nil), h.threadCreate)
	threadID, _ := extractID(t, th.body)

	resol := serve(t, h, cxt(http.MethodPost, "/p", "user2", "", map[string]string{
		"2": "main.tex", "3": threadID, "4": "resolve",
	}), h.threadResolve)
	if resol.code != 200 {
		t.Fatalf("resolve code=%d body=%s", resol.code, resol.body)
	}
	var rt map[string]any
	json.Unmarshal([]byte(resol.body), &rt)
	if rt["state"] != "resolved" || rt["resolved"] != true {
		t.Fatalf("resolved envelope: %v", rt)
	}
	info, _ := rt["resolvedInfo"].(map[string]any)
	if info == nil || info["resolved_by_user_id"] != "user2" {
		t.Fatalf("resolvedBy: %v", rt)
	}
	// idempotent resolve.
	if r2 := serve(t, h, cxt(http.MethodPost, "/p", "owner", "", map[string]string{
		"2": "main.tex", "3": threadID, "4": "resolve",
	}), h.threadResolve); r2.code != 200 {
		t.Fatalf("idempotent resolve = %d", r2.code)
	}

	reopen := serve(t, h, cxt(http.MethodPost, "/p", "owner", "", map[string]string{
		"2": "main.tex", "3": threadID, "4": "reopen",
	}), h.threadResolve)
	if reopen.code != 200 {
		t.Fatalf("reopen code=%d", reopen.code)
	}
	var ro map[string]any
	json.Unmarshal([]byte(reopen.body), &ro)
	if ro["state"] != "opened" || ro["resolvedInfo"] != nil {
		t.Fatalf("reopened envelope: %v", ro)
	}

	// unknown thread → 404 (existence not leaked).
	if r3 := serve(t, h, cxt(http.MethodPost, "/p", "owner", "", map[string]string{
		"2": "main.tex", "3": "0000000000000", "4": "resolve",
	}), h.threadResolve); r3.code != 404 {
		t.Fatalf("resolve unknown = %d (want 404)", r3.code)
	}
}

func TestThreadDeleteCascade(t *testing.T) {
	_, h := setup(t)
	th := serve(t, h, cxt(http.MethodPost, "/p", "owner", `{"content":"q?","doc":"main.tex"}`, nil), h.threadCreate)
	threadID, _ := extractID(t, th.body)
	rep := serve(t, h, cxt(http.MethodPost, "/p", "user2", `{"content":"a!"}`, map[string]string{
		"2": threadID,
	}), h.messageAdd)
	_, cid := extractID(t, rep.body)
	_ = cid

	del := serve(t, h, cxt(http.MethodDelete, "/p", "owner", "", map[string]string{
		"2": "main.tex", "3": threadID,
	}), h.threadDelete)
	if del.code != 200 {
		t.Fatalf("delete code=%d", del.code)
	}
	list := serve(t, h, cxt(http.MethodGet, "/p", "owner", "", nil), h.threadsList)
	if !strings.Contains(list.body, `"threads":[]`) {
		t.Fatalf("after delete: %s", list.body)
	}
}

func TestChangesAccept(t *testing.T) {
	st, h := setup(t)
	// register a pending change directly in the domain (the editor flow
	// creates these at type-time; the accept route is what the panel calls).
	ctx := context.Background()
	if _, _, _, err := collab.AddChange(ctx, st, testPID, collab.TrackedChange{
		ID: "555555555555555555555555", Kind: collab.ChangeKindInsert, Start: 5, End: 11, Content: " world",
	}); err != nil {
		t.Fatalf("addchange: %v", err)
	}
	acc := serve(t, h, cxt(http.MethodPost, "/p", "owner",
		`{"change_ids":["555555555555555555555555"]}`, map[string]string{"2": "main.tex"}), h.changesAccept)
	if acc.code != 200 {
		t.Fatalf("accept code=%d body=%s", acc.code, acc.body)
	}
	var out struct {
		Accepted int `json:"accepted"`
	}
	if err := json.Unmarshal([]byte(acc.body), &out); err != nil || out.Accepted != 1 {
		t.Fatalf("accepted: %s", acc.body)
	}
	// idempotency: a second accept counts nothing new.
	acc2 := serve(t, h, cxt(http.MethodPost, "/p", "owner",
		`{"change_ids":["555555555555555555555555"]}`, map[string]string{"2": "main.tex"}), h.changesAccept)
	var out2 struct {
		Accepted int `json:"accepted"`
	}
	json.Unmarshal([]byte(acc2.body), &out2)
	if out2.Accepted != 0 {
		t.Fatalf("second accept: %s", acc2.body)
	}
}

func TestTrackChangesToggle(t *testing.T) {
	_, h := setup(t)
	if s := serve(t, h, cxt(http.MethodPost, "/p", "owner", `{}`, nil), h.trackChanges); s.code != 200 {
		t.Fatalf("track code=%d", s.code)
	}
	off := serve(t, h, cxt(http.MethodPost, "/p", "owner", `{"enabled":false}`, nil), h.trackChanges)
	if !strings.Contains(off.body, `"enabled":false`) {
		t.Fatalf("off: %s", off.body)
	}
	on := serve(t, h, cxt(http.MethodPost, "/p", "owner", `{"enabled":true}`, nil), h.trackChanges)
	if !strings.Contains(on.body, `"enabled":true`) {
		t.Fatalf("on: %s", on.body)
	}
}

func TestRoleGating(t *testing.T) {
	_, h := setup(t)
	// stranger (Deny): even a read leaks nothing.
	if s := serve(t, h, cxt(http.MethodGet, "/p", "stranger", "", nil), h.threadsList); s.code != 404 {
		t.Fatalf("stranger list = %d (want 404)", s.code)
	}
	// viewer (ReadOnly): reads OK, writes denied.
	if s := serve(t, h, cxt(http.MethodGet, "/p", "viewer", "", nil), h.threadsList); s.code != 200 {
		t.Fatalf("viewer list = %d (want 200)", s.code)
	}
	if s := serve(t, h, cxt(http.MethodPost, "/p", "viewer", `{"content":"x","doc":"m"}`, nil), h.threadCreate); s.code != 404 {
		t.Fatalf("viewer create = %d (want 404)", s.code)
	}
	// anonymous: 404 (CSRF 403 sits upstream at core for POST).
	if s := serve(t, h, cxt(http.MethodGet, "/p", "", "", nil), h.threadsList); s.code != 404 {
		t.Fatalf("anon list = %d (want 404)", s.code)
	}
}

func extractID(t *testing.T, body string) (string, string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("extract: %v", err)
	}
	id, _ := m["id"].(string)
	if id == "" {
		t.Fatalf("no id in %s", body)
	}
	return id, id
}
