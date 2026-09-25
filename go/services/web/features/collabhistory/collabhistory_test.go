package collabhistory

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

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

func cxt(method, path, uid string, params map[string]string) *core.Cxt {
	var sess *core.Session
	if uid != "" {
		sess = &core.Session{Doc: sessDoc(uid)}
	}
	p := map[string]string{"1": testPID} // default project id
	for k, v := range params {
		p[k] = v
	}
	return &core.Cxt{Req: httptest.NewRequest(method, path, nil), Sess: sess, Params: p}
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

func setupStore(t *testing.T) (persistence.VersionedPersistence, *Handlers) {
	t.Helper()
	st, err := persistence.NewFilePersistence(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	roles := fakeRole{
		"owner|" + testPID:    collab.ReadWrite,
		"viewer|" + testPID:   collab.ReadOnly,
		"stranger|" + testPID: collab.Deny,
	}
	return st, &Handlers{Store: st, RoleFor: roles.Fn}
}

func TestRoutesRegistered(t *testing.T) {
	f := Feature(nil)
	if f.Name != "collabhistory" {
		t.Fatalf("name = %q", f.Name)
	}
	if len(f.Routes) != 4 {
		t.Fatalf("routes = %d, want 4", len(f.Routes))
	}
	want := []struct {
		m string
		p *regexp.Regexp
	}{
		{"GET", histPattern},
		{"GET", histVPattern},
		{"POST", restorePattern},
		{"GET", docPattern},
	}
	for i, w := range want {
		if f.Routes[i].Method != w.m || f.Routes[i].Pattern != w.p {
			t.Fatalf("route[%d] = %s %v, want %s %v", i, f.Routes[i].Method, f.Routes[i].Pattern, w.m, w.p)
		}
	}
}

func TestGates(t *testing.T) {
	_, h := setupStore(t)

	denied := []struct {
		kind string
		c    *core.Cxt
		fn   func(*core.Cxt, *core.Res)
	}{
		{"list-anon", cxt("GET", "/project/"+testPID+"/collab/history", "", nil), h.list},
		{"doc-anon", cxt("GET", "/project/"+testPID+"/collab/doc", "", nil), h.doc},
		{"restore-anon", cxt("POST", "/project/"+testPID+"/collab/history/1/restore", "", map[string]string{"2": "1"}), h.restore},
		{"list-stranger", cxt("GET", "/project/"+testPID+"/collab/history", "stranger", nil), h.list},
		{"at-stranger", cxt("GET", "/project/"+testPID+"/collab/history/1", "stranger", map[string]string{"2": "1"}), h.at},
		{"restore-viewer", cxt("POST", "/project/"+testPID+"/collab/history/1/restore", "viewer", map[string]string{"2": "1"}), h.restore},
	}
	for _, tc := range denied {
		got := serve(t, h, tc.c, tc.fn)
		if got.code != 404 {
			t.Fatalf("%s: status = %d, want 404 (body %s)", tc.kind, got.code, got.body)
		}
	}

	allowed := []struct {
		kind string
		c    *core.Cxt
		fn   func(*core.Cxt, *core.Res)
	}{
		{"list-viewer", cxt("GET", "/project/"+testPID+"/collab/history", "viewer", nil), h.list},
		{"doc-viewer", cxt("GET", "/project/"+testPID+"/collab/doc", "viewer", nil), h.doc},
	}
	for _, tc := range allowed {
		got := serve(t, h, tc.c, tc.fn)
		if got.code != 200 {
			t.Fatalf("%s: status = %d, want 200 (body %s)", tc.kind, got.code, got.body)
		}
	}
}

func TestFullHistoryFlow(t *testing.T) {
	st, h := setupStore(t)
	ctx := context.Background()

	v1 := "v1-content\n"
	v2 := v1 + "v2-line\n"
	if _, err := collab.SeedTextContent(ctx, st, testPID, v1); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if v, err := collab.ClientEdit(ctx, st, testPID, v1, v2); err != nil || v != 2 {
		t.Fatalf("edit = (%d, %v), want (2, nil)", v, err)
	}

	// list: newest first, one entry, RFC3339 timestamp
	got := serve(t, h, cxt("GET", "/project/"+testPID+"/collab/history", "owner", nil), h.list)
	if got.code != 200 {
		t.Fatalf("list: %d %s", got.code, got.body)
	}
	var l struct {
		ProjectID string `json:"project_id"`
		Versions  []struct {
			Version uint64 `json:"version"`
			At      string `json:"at"`
		} `json:"versions"`
	}
	if err := json.Unmarshal([]byte(got.body), &l); err != nil {
		t.Fatalf("parse list: %v (%s)", err, got.body)
	}
	if l.ProjectID != testPID || len(l.Versions) != 2 || l.Versions[0].Version != 2 || l.Versions[1].Version != 1 {
		t.Fatalf("list body = %s", got.body)
	}
	if _, err := time.Parse(time.RFC3339, l.Versions[0].At); err != nil {
		t.Fatalf("at not RFC3339: %q", l.Versions[0].At)
	}

	// per-version content
	got = serve(t, h, cxt("GET", "/project/"+testPID+"/collab/history/1", "owner", map[string]string{"2": "1"}), h.at)
	if got.code != 200 || !strings.Contains(got.body, `"version":1`) || !strings.Contains(got.body, "v1-content") {
		t.Fatalf("at(1) = %d %s", got.code, got.body)
	}
	got = serve(t, h, cxt("GET", "/project/"+testPID+"/collab/history/2", "owner", map[string]string{"2": "2"}), h.at)
	if got.code != 200 || !strings.Contains(got.body, "v2-line") {
		t.Fatalf("at(2) = %d %s", got.code, got.body)
	}
	// unknown version → 404
	got = serve(t, h, cxt("GET", "/project/"+testPID+"/collab/history/99", "owner", map[string]string{"2": "99"}), h.at)
	if got.code != 404 {
		t.Fatalf("at(99) = %d, want 404 (%s)", got.code, got.body)
	}

	// doc (head) = v2
	got = serve(t, h, cxt("GET", "/project/"+testPID+"/collab/doc", "owner", nil), h.doc)
	if got.code != 200 || !strings.Contains(got.body, `"version":2`) || !strings.Contains(got.body, "v2-line") {
		t.Fatalf("doc = %d %s", got.code, got.body)
	}

	// restore to v1 → new head v3 with v1 content; log intact behind it
	got = serve(t, h, cxt("POST", "/project/"+testPID+"/collab/history/1/restore", "owner", map[string]string{"2": "1"}), h.restore)
	if got.code != 200 || !strings.Contains(got.body, `"version":3`) {
		t.Fatalf("restore = %d %s", got.code, got.body)
	}
	got = serve(t, h, cxt("GET", "/project/"+testPID+"/collab/doc", "owner", nil), h.doc)
	if !strings.Contains(got.body, `"version":3`) || strings.Contains(got.body, "v2-line") {
		t.Fatalf("doc after restore = %d %s (want v3 = v1 content, no v2-line)", got.code, got.body)
	}
	// restore to the current head content → NO new version
	got = serve(t, h, cxt("POST", "/project/"+testPID+"/collab/history/3/restore", "owner", map[string]string{"2": "3"}), h.restore)
	if got.code != 200 || !strings.Contains(got.body, `"version":3`) {
		t.Fatalf("restore-to-head = %d %s (want no-op v3)", got.code, got.body)
	}
}

func TestEmptyRoomShapes(t *testing.T) {
	_, h := setupStore(t)
	// list on a never-opened room: 200 + empty list (not an error)
	got := serve(t, h, cxt("GET", "/project/"+testPID+"/collab/history", "owner", nil), h.list)
	if got.code != 200 || !strings.Contains(got.body, `"versions":[]`) {
		t.Fatalf("empty list = %d %s", got.code, got.body)
	}
	// at on empty room: 404
	got = serve(t, h, cxt("GET", "/project/"+testPID+"/collab/history/1", "owner", map[string]string{"2": "1"}), h.at)
	if got.code != 404 {
		t.Fatalf("at-empty = %d, want 404 (%s)", got.code, got.body)
	}
	// restore on empty room: 404
	got = serve(t, h, cxt("POST", "/project/"+testPID+"/collab/history/1/restore", "owner", map[string]string{"2": "1"}), h.restore)
	if got.code != 404 {
		t.Fatalf("restore-empty = %d, want 404 (%s)", got.code, got.body)
	}
	// doc on empty room: 200 {version:0, content:""}
	got = serve(t, h, cxt("GET", "/project/"+testPID+"/collab/doc", "owner", nil), h.doc)
	if got.code != 200 || !strings.Contains(got.body, `"version":0`) || !strings.Contains(got.body, `"content":""`) {
		t.Fatalf("doc-empty = %d %s", got.code, got.body)
	}
}
