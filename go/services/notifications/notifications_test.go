package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// --- test helpers -----------------------------------------------------------

const (
	uid1 = "5f8a2c1e9d4b3c6a7e8f9a0b" // 24-hex valid ObjectId
	uid2 = "5f8a2c1e9d4b3c6a7e8f9a0c"
)

func newTestServer() (*Server, *memStore) {
	store := newMemStore()
	srv := NewServer(store, Config{Host: "127.0.0.1", Port: 3042})
	return srv, store
}

type resp struct {
	code     int
	body     string
	jsonBody map[string]any
	arrBody  []map[string]any
}

func doReq(t *testing.T, s *Server, method, path string, body any) resp {
	t.Helper()
	var reader strings.Reader
	if body != nil {
		b, merr := json.Marshal(body)
		if merr != nil {
			t.Fatal(merr)
		}
		reader = *strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:3042"+path, &reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	s.Router().ServeHTTP(rr, req)
	out := resp{code: rr.Code, body: rr.Body.String()}

	trimmed := strings.TrimSpace(out.body)
	if strings.HasPrefix(trimmed, "{") {
		if uerr := json.Unmarshal([]byte(out.body), &out.jsonBody); uerr != nil {
			t.Fatalf("%s %s → non-object, non-array JSON %q: %v", method, path, out.body, uerr)
		}
	} else if strings.HasPrefix(trimmed, "[") {
		if uerr := json.Unmarshal([]byte(out.body), &out.arrBody); uerr != nil {
			t.Fatalf("%s %s → bad array JSON %q: %v", method, path, out.body, uerr)
		}
	}
	return out
}

func addOne(t *testing.T, s *Server, uid, key, templateKey string) {
	t.Helper()
	r := doReq(t, s, http.MethodPost, "/user/"+uid, map[string]any{
		"key":         key,
		"messageOpts": map[string]any{"n": 1},
		"templateKey": templateKey,
	})
	if r.code != http.StatusOK {
		t.Fatalf("addNotification %q → %d %q (want 200 OK)", key, r.code, r.body)
	}
}

// --- status / 404 / 413 contract -------------------------------------------

func TestStatus(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodGet, "/status", nil)
	if r.code != http.StatusOK || r.body != "notifications is up" {
		t.Fatalf("GET /status = %d %q (want 200 'notifications is up')", r.code, r.body)
	}
}

func TestCatchAll_GET_NotFound(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodGet, "/nonexistent", nil)
	if r.code != http.StatusNotFound || r.body != "Not Found" {
		t.Fatalf("GET /nonexistent = %d %q (want 404 'Not Found')", r.code, r.body)
	}
}

func TestCatchAll_NonGET_Cannot(t *testing.T) {
	s, _ := newTestServer()
	cases := []struct{ method, path, want string }{
		{http.MethodPost, "/nope", "Cannot POST /nope"},
		{http.MethodPut, "/status", "Cannot PUT /status"},
		{http.MethodDelete, "/x/y", "Cannot DELETE /x/y"},
	}
	for _, c := range cases {
		t.Run(c.method, func(t *testing.T) {
			r := doReq(t, s, c.method, c.path, nil)
			if r.code != http.StatusNotFound {
				t.Fatalf("%s %s → %d (want 404)", c.method, c.path, r.code)
			}
			if r.body != c.want {
				t.Fatalf("%s %s body = %q (want %q)", c.method, c.path, r.body, c.want)
			}
		})
	}
}

func TestBodyOverflow_Is500(t *testing.T) {
	// Node's notifications handleApiError forces 500 (not 413) on body overflow.
	s, _ := newTestServer()
	big := strings.Repeat("a", 101*1024) // > 100kb
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:3042/user/"+uid1, strings.NewReader(`{"key":"`+big+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("overflow = %d (want 500)", rr.Code)
	}
	if strings.TrimSpace(rr.Body.String()) != "Internal Server Error" {
		t.Fatalf("overflow body = %q (want 'Internal Server Error')", rr.Body)
	}
}

// --- addNotification --------------------------------------------------------

func TestAddNotification_New_then_Get(t *testing.T) {
	s, store := newTestServer()
	addOne(t, s, uid1, "proj.created", "tmpl")
	// stored doc has templateKey → unread
	list := doReq(t, s, http.MethodGet, "/user/"+uid1, nil)
	if list.code != http.StatusOK || len(list.arrBody) != 1 {
		t.Fatalf("GET list = %d %q (want 200 with 1 doc)", list.code, list.body)
	}
	d := list.arrBody[0]
	if d["_id"] == nil || d["user_id"] == nil || d["templateKey"] != "tmpl" || d["key"] != "proj.created" {
		t.Fatalf("doc shape wrong: %v", d)
	}
	// serialization: user_id must be the hex string back
	if d["user_id"] != uid1 {
		t.Fatalf("user_id = %v (want %s)", d["user_id"], uid1)
	}
	// store holds exactly one doc
	if got := len(store.docs); got != 1 {
		t.Fatalf("store docs = %d (want 1)", got)
	}
}

func TestAddNotification_SkipExistingWithoutForce(t *testing.T) {
	s, _ := newTestServer()
	addOne(t, s, uid1, "k", "t1")
	// second add, same key, different templateKey, no force → must NOT overwrite
	r := doReq(t, s, http.MethodPost, "/user/"+uid1, map[string]any{"key": "k", "templateKey": "t2"})
	if r.code != http.StatusOK {
		t.Fatalf("2nd add → %d (want 200)", r.code)
	}
	list := doReq(t, s, http.MethodGet, "/user/"+uid1, nil)
	if len(list.arrBody) != 1 || list.arrBody[0]["templateKey"] != "t1" {
		t.Fatalf("expected original templateKey t1 preserved; got %v", list.arrBody)
	}
}

func TestAddNotification_ForceCreateOverwrites(t *testing.T) {
	s, _ := newTestServer()
	addOne(t, s, uid1, "k", "t1")
	r := doReq(t, s, http.MethodPost, "/user/"+uid1, map[string]any{"key": "k", "templateKey": "t2", "forceCreate": true})
	if r.code != http.StatusOK {
		t.Fatalf("forceCreate add → %d (want 200)", r.code)
	}
	list := doReq(t, s, http.MethodGet, "/user/"+uid1, nil)
	if len(list.arrBody) != 1 || list.arrBody[0]["templateKey"] != "t2" {
		t.Fatalf("expected templateKey overwritten to t2; got %v", list.arrBody)
	}
}

func TestAddNotification_InvalidUserID_404(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodPost, "/user/notanobjectid", map[string]any{"key": "k", "templateKey": "t"})
	if r.code != http.StatusNotFound {
		t.Fatalf("invalid user_id → %d (want 404)", r.code)
	}
	if r.jsonBody["statusCode"] != float64(http.StatusNotFound) {
		t.Fatalf("statusCode field = %v (want 404)", r.jsonBody["statusCode"])
	}
	if r.jsonBody["error"] == nil || r.jsonBody["error"] == "" {
		t.Fatalf("error field missing")
	}
}

func TestAddNotification_MissingTemplateKey_400(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodPost, "/user/"+uid1, map[string]any{"key": "k"})
	if r.code != http.StatusBadRequest {
		t.Fatalf("missing templateKey → %d (want 400)", r.code)
	}
	if r.jsonBody["statusCode"] != float64(http.StatusBadRequest) {
		t.Fatalf("statusCode field = %v (want 400)", r.jsonBody["statusCode"])
	}
}

func TestAddNotification_MissingKey_400(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodPost, "/user/"+uid1, map[string]any{"templateKey": "t"})
	if r.code != http.StatusBadRequest {
		t.Fatalf("missing key → %d (want 400)", r.code)
	}
}

func TestAddNotification_InvalidExpires_500(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodPost, "/user/"+uid1, map[string]any{"key": "k", "templateKey": "t", "expires": "not-a-date"})
	if r.code != http.StatusInternalServerError {
		t.Fatalf("invalid expires → %d (want 500)", r.code)
	}
}

func TestAddNotification_ValidExpires_Stored(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodPost, "/user/"+uid1, map[string]any{
		"key": "k", "templateKey": "t", "expires": "2031-06-01T00:00:00.000Z",
	})
	if r.code != http.StatusOK {
		t.Fatalf("valid expires add → %d %q (want 200)", r.code, r.body)
	}
	list := doReq(t, s, http.MethodGet, "/user/"+uid1, nil)
	if len(list.arrBody) != 1 {
		t.Fatalf("list = %v", list.arrBody)
	}
	exp, _ := list.arrBody[0]["expires"].(string)
	if exp != "2031-06-01T00:00:00.000Z" {
		t.Fatalf("expires = %q (want 2031-06-01T00:00:00.000Z)", exp)
	}
}

// --- getUserNotifications ---------------------------------------------------

func TestGetUserNotifications_Empty(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodGet, "/user/"+uid1, nil)
	if r.code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(r.body), "[") {
		t.Fatalf("GET empty = %d %q (want 200 [])", r.code, r.body)
	}
}

func TestGetUserNotifications_InvalidUserID_404(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodGet, "/user/zzz", nil)
	if r.code != http.StatusNotFound {
		t.Fatalf("invalid user_id → %d (want 404)", r.code)
	}
}

func TestGetUserNotifications_ReadableExcludedAfterMark(t *testing.T) {
	s, _ := newTestServer()
	addOne(t, s, uid1, "k", "t")
	// mark read via key → should disappear from the unread list
	r := doReq(t, s, http.MethodDelete, "/user/"+uid1, map[string]any{"key": "k"})
	if r.code != http.StatusOK {
		t.Fatalf("mark-read → %d (want 200)", r.code)
	}
	list := doReq(t, s, http.MethodGet, "/user/"+uid1, nil)
	if len(list.arrBody) != 0 {
		t.Fatalf("after mark-read list should be empty, got %v", list.arrBody)
	}
}

// --- removeNotificationId ---------------------------------------------------

func TestRemoveNotificationId(t *testing.T) {
	s, _ := newTestServer()
	addOne(t, s, uid1, "k", "t")
	list := doReq(t, s, http.MethodGet, "/user/"+uid1, nil)
	nid, _ := list.arrBody[0]["_id"].(string)
	if nid == "" {
		t.Fatalf("no _id in list")
	}
	r := doReq(t, s, http.MethodDelete, "/user/"+uid1+"/notification/"+nid, nil)
	if r.code != http.StatusOK {
		t.Fatalf("remove by id → %d (want 200)", r.code)
	}
	after := doReq(t, s, http.MethodGet, "/user/"+uid1, nil)
	if len(after.arrBody) != 0 {
		t.Fatalf("after remove-by-id list should be empty, got %v", after.arrBody)
	}
}

func TestRemoveNotificationId_InvalidId_404(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodDelete, "/user/"+uid1+"/notification/badid", nil)
	if r.code != http.StatusNotFound {
		t.Fatalf("invalid notification_id → %d (want 404)", r.code)
	}
}

func TestRemoveNotificationId_InvalidUserID_404(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodDelete, "/user/badid/notification/"+uid1, nil)
	if r.code != http.StatusNotFound {
		t.Fatalf("invalid user_id → %d (want 404)", r.code)
	}
}

// --- removeNotificationKey --------------------------------------------------

func TestRemoveNotificationKey_MissingKey_400(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodDelete, "/user/"+uid1, map[string]any{})
	if r.code != http.StatusBadRequest {
		t.Fatalf("missing key → %d (want 400)", r.code)
	}
}

// --- removeByKeyOnly / count / bulk ----------------------------------------

func TestRemoveByKeyOnly(t *testing.T) {
	s, _ := newTestServer()
	addOne(t, s, uid1, "shared-key", "t")
	r := doReq(t, s, http.MethodDelete, "/key/shared-key", nil)
	if r.code != http.StatusOK {
		t.Fatalf("remove by key → %d (want 200)", r.code)
	}
	// both unread gone now
	list := doReq(t, s, http.MethodGet, "/user/"+uid1, nil)
	if len(list.arrBody) != 0 {
		t.Fatalf("after remove-by-key list should be empty, got %v", list.arrBody)
	}
}

func TestCountByKeyOnly(t *testing.T) {
	s, _ := newTestServer()
	addOne(t, s, uid1, "ck", "t")
	addOne(t, s, uid2, "ck", "t") // different user, same key, both unread
	r := doReq(t, s, http.MethodGet, "/key/ck/count", nil)
	if r.code != http.StatusOK {
		t.Fatalf("count → %d (want 200)", r.code)
	}
	if r.jsonBody["count"] != float64(2) {
		t.Fatalf("count = %v (want 2)", r.jsonBody["count"])
	}
	// zero when none
	r2 := doReq(t, s, http.MethodGet, "/key/unknown/count", nil)
	if r2.jsonBody["count"] != float64(0) {
		t.Fatalf("unknown count = %v (want 0)", r2.jsonBody["count"])
	}
}

func TestBulkDelete(t *testing.T) {
	s, _ := newTestServer()
	addOne(t, s, uid1, "bulk", "t")
	addOne(t, s, uid2, "bulk", "t") // both unread, same key
	// mark uid2's as read → only uid1's remains unread
	l2 := doReq(t, s, http.MethodGet, "/user/"+uid2, nil)
	nid2, _ := l2.arrBody[0]["_id"].(string)
	_ = doReq(t, s, http.MethodDelete, "/user/"+uid2+"/notification/"+nid2, nil)

	bulk := doReq(t, s, http.MethodDelete, "/key/bulk/bulk", nil)
	if bulk.code != http.StatusOK {
		t.Fatalf("bulk → %d (want 200)", bulk.code)
	}
	if bulk.jsonBody["count"] != float64(1) {
		t.Fatalf("bulk count = %v (want 1: only uid1's unread doc)", bulk.jsonBody["count"])
	}
	// the read (uid2) doc must remain, just with templateKey removed
	l2after := doReq(t, s, http.MethodGet, "/user/"+uid2, nil)
	if len(l2after.arrBody) != 0 {
		t.Fatalf("uid2 read doc should not appear in unread list, got %v", l2after.arrBody)
	}
}

// --- serialisation ----------------------------------------------------------

func TestSerialize_MessageOptsNested(t *testing.T) {
	s, _ := newTestServer()
	r := doReq(t, s, http.MethodPost, "/user/"+uid1, map[string]any{
		"key":         "k",
		"templateKey": "t",
		"messageOpts": map[string]any{"a": 1, "b": "x", "c": []any{1, 2}},
	})
	if r.code != http.StatusOK {
		t.Fatalf("add → %d %q", r.code, r.body)
	}
	list := doReq(t, s, http.MethodGet, "/user/"+uid1, nil)
	d := list.arrBody[0]
	opts, ok := d["messageOpts"].(map[string]any)
	if !ok {
		t.Fatalf("messageOpts not an object: %v", d)
	}
	if opts["a"] != float64(1) || opts["b"] != "x" {
		t.Fatalf("messageOpts = %v", opts)
	}
}

// --- memStore sanity (direct) ----------------------------------------------

func TestMemStore_CountAndDeleteSemantics(t *testing.T) {
	ctx := context.Background()
	m := newMemStore()
	uid := mustObjectID(uid1)
	_ = m.Upsert(ctx, mkM("user_id", uid, "key", "k"), mkM("user_id", uid, "key", "k", "templateKey", "t", "messageOpts", "m"))
	if n, _ := m.CountByUserKey(ctx, uid, "k"); n != 1 {
		t.Fatalf("CountByUserKey = %d (want 1)", n)
	}
	if n, _ := m.CountByKeyOnly(ctx, "k"); n != 1 {
		t.Fatalf("CountByKeyOnly = %d (want 1)", n)
	}
	_ = m.UnsetByUserKey(ctx, uid, "k")
	if n, _ := m.CountByKeyOnly(ctx, "k"); n != 0 {
		t.Fatalf("after unset CountByKeyOnly = %d (want 0)", n)
	}
	if n, _ := m.DeleteManyByKeyOnly(ctx, "k"); n != 0 {
		t.Fatalf("after unset DeleteMany = %d (want 0)", n)
	}
}

// --- tiny test helpers ------------------------------------------------------

func mkM(kvs ...any) primitive.M {
	out := primitive.M{}
	for i := 0; i+1 < len(kvs); i += 2 {
		out[kvs[i].(string)] = kvs[i+1]
	}
	return out
}
