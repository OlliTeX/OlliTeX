package chat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	pid     = "5f8a2c1e9d4b3c6a7e8f9a0b" // valid 24-hex project id
	tid1    = "658a1b2c3d4e5f60718293a4"
	userA   = "618a2c1e9d4b3c6a7e8f9a01"
	userB   = "618a2c1e9d4b3c6a7e8f9a02"
	userC   = "618a2c1e9d4b3c6a7e8f9a03"
	fixedID = "5f8a00000000000000000001" // valid 24-hex id absent from the store
)

type chatTest struct {
	srv *Server
	ts  *httptest.Server
	m   *memStore
}

func newChatTest(t *testing.T) *chatTest {
	t.Helper()
	m := newMemStore()
	srv := NewServer(m, Config{})
	srv.nowMs = func() int64 { return 1000 }
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	return &chatTest{srv: srv, ts: ts, m: m}
}

func (ct *chatTest) do(t *testing.T, method, path string, body any, ctHdr string) (int, string, http.Header) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		if s, ok := body.(string); ok {
			rdr = strings.NewReader(s)
		} else {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			rdr = bytes.NewReader(b)
		}
	}
	req, err := http.NewRequest(method, ct.ts.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if ctHdr == "json" {
		req.Header.Set("Content-Type", "application/json")
	} else if ctHdr != "" {
		req.Header.Set("Content-Type", ctHdr)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(data), resp.Header
}

func asMap(t *testing.T, body string) map[string]any {
	t.Helper()
	m := map[string]any{}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("body is not a JSON object: %s", body)
	}
	return m
}

func asArr(t *testing.T, body string) []any {
	t.Helper()
	var out []any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("body is not a JSON array: %s", body)
	}
	return out
}

// errOf returns the decoded `error` field (no JSON escaping) for assertions
// against the exact Node message text.
func errOf(t *testing.T, body string) string {
	t.Helper()
	v, ok := asMap(t, body)["error"].(string)
	if !ok {
		t.Fatalf("no error field in %s", body)
	}
	return v
}

// ---- contract: status, 404s, validation (Node-observed) ------------------------

func TestStatus(t *testing.T) {
	ct := newChatTest(t)
	code, body, hdr := ct.do(t, "GET", "/status", nil, "")
	if code != 200 || body != "chat is alive" {
		t.Fatalf("status = %d %q; want 200 'chat is alive'", code, body)
	}
	if !strings.Contains(hdr.Get("Content-Type"), "text/html") {
		t.Fatalf("Content-Type = %q; want text/html (Node res.send)", hdr.Get("Content-Type"))
	}
}

func TestUnmatchedRouteIsJSONNotFound(t *testing.T) {
	for _, method := range []string{"GET", "POST", "DELETE"} {
		ct := newChatTest(t)
		code, body, _ := ct.do(t, method, "/nope", nil, "")
		if code != 404 || body != `{"message":"Not found"}` {
			t.Fatalf("%s /nope = %d %q; want 404 {\"message\":\"Not found\"}", method, code, body)
		}
	}
}

func TestPathParamErrorsAre404(t *testing.T) {
	ct := newChatTest(t)
	code, body, _ := ct.do(t, "GET", "/project/zz.objectId/messages", nil, "")
	if code != 404 {
		t.Fatalf("code = %d; want 404", code)
	}
	m := asMap(t, body)
	if m["statusCode"] != float64(404) || !strings.Contains(m["error"].(string), `invalid Mongo ObjectId at "params.projectId"`) {
		t.Fatalf("body = %s", body)
	}

	// invalid threadId on thread send (valid body) → 404 with threadId issue.
	code, body, _ = ct.do(t, "POST", "/project/"+pid+"/thread/zz.objectId/messages",
		map[string]any{"user_id": userA, "content": "hi"}, "json")
	if code != 404 || !strings.Contains(errOf(t, body), `invalid Mongo ObjectId at "params.threadId"`) {
		t.Fatalf("send w/ bad threadId = %d %s", code, body)
	}

	// deleteUserMessage param order (schema extend order): messageId BEFORE userId.
	code, body, _ = ct.do(t, "DELETE", fmt.Sprintf("/project/%s/thread/%s/user/zz.users/messages/zz.message", pid, tid1), nil, "")
	if code != 404 {
		t.Fatalf("code = %d; want 404", code)
	}
	errStr := asMap(t, body)["error"].(string)
	iMid := strings.Index(errStr, `params.messageId`)
	iUser := strings.Index(errStr, `params.userId`)
	if iMid < 0 || iUser < 0 || iMid > iUser {
		t.Fatalf("param issue order wrong: %s", errStr)
	}
}

func TestArrayBodyIsObjectReceivedArray(t *testing.T) {
	routes := []struct {
		method string
		path   string
	}{{"POST", "/project/" + pid + "/messages"},
		{"POST", "/project/" + pid + "/messages/" + fixedID + "/edit"},
		{"POST", "/project/" + pid + "/thread/" + tid1 + "/resolve"},
		{"POST", "/project/" + pid + "/generate-thread-data"},
		{"POST", "/project/" + pid + "/duplicate-comment-threads"},
		{"POST", "/project/" + pid + "/clone-comment-threads"}}
	for _, rt := range routes {
		ct := newChatTest(t)
		code, body, hdr := ct.do(t, rt.method, rt.path, "[1,2]", "json")
		want := `Validation error: Invalid input: expected object, received array at "body"`
		if code != 400 || errOf(t, body) != want || asMap(t, body)["statusCode"] != float64(400) {
			t.Fatalf("%s %s = %d %q; want 400 %q", rt.method, rt.path, code, body, want)
		}
		if !strings.HasPrefix(hdr.Get("Content-Type"), "application/json") {
			t.Fatalf("Content-Type = %q", hdr.Get("Content-Type"))
		}
	}
}

func TestBodyErrorExactness(t *testing.T) {
	ct := newChatTest(t)
	// empty JSON body → user_id undefined + content invalid (Node-observed).
	code, body, _ := ct.do(t, "POST", "/project/"+pid+"/messages", map[string]any{}, "json")
	m := asMap(t, body)
	errStr := m["error"].(string)
	want := `Validation error: Invalid input: expected string, received undefined at "body.user_id"; No content provided at "body.content"`
	if code != 400 || m["statusCode"] != float64(400) || errStr != want {
		t.Fatalf("got %d %s (want %d %q)", code, body, 400, want)
	}

	// unknown key (strict) → unknown-key issue (Node-observed).
	code, body, _ = ct.do(t, "POST", "/project/"+pid+"/messages",
		map[string]any{"user_id": userA, "content": "hi", "forceCreate": true}, "json")
	if code != 400 || !strings.Contains(errOf(t, body), `Unrecognized key: "forceCreate" at "body"`) {
		t.Fatalf("unknown key = %d %s", code, body)
	}

	// threads must be an array (schema error text).
	code, body, _ = ct.do(t, "POST", "/project/"+pid+"/generate-thread-data",
		map[string]any{"threads": "zz"}, "json")
	if code != 400 || !strings.Contains(errOf(t, body), `Invalid input: expected array, received string at "body.threads"`) {
		t.Fatalf("threads type = %d %s", code, body)
	}

	// query validation → 400 (z.coerce.number(): "abc" → NaN)
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/messages?limit=abc", nil, "")
	if code != 400 || !strings.Contains(errOf(t, body), `Invalid input: expected number, received NaN at "query.limit"`) {
		t.Fatalf("query limit = %d %s", code, body)
	}
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/messages?before=abc", nil, "")
	if code != 400 || !strings.Contains(errOf(t, body), `Invalid input: expected number, received NaN at "query.before"`) {
		t.Fatalf("query before = %d %s", code, body)
	}
}

// ---- message + thread lifecycle --------------------------------------------------

func TestGlobalSendGetEditDelete(t *testing.T) {
	ct := newChatTest(t)
	// send (creates the GLOBAL room)
	code, body, _ := ct.do(t, "POST", "/project/"+pid+"/messages",
		map[string]any{"user_id": userA, "content": "hello"}, "json")
	if code != 201 {
		t.Fatalf("send = %d %s", code, body)
	}
	m := asMap(t, body)
	if m["room_id"] != pid || m["user_id"] != userA || m["content"] != "hello" || m["timestamp"] != float64(1000) {
		t.Fatalf("send body = %s", body)
	}
	if _, has := m["thread_id"]; has {
		t.Fatalf("send body must not include thread_id: %s", body)
	}
	if len(ct.m.rooms) != 1 || ct.m.rooms[0].ThreadID != nil {
		t.Fatalf("rooms state = %+v; want one GLOBAL room", ct.m.rooms)
	}

	// get messages: room_id stripped from array items
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/messages", nil, "")
	items := asArr(t, body)
	if code != 200 || len(items) != 1 {
		t.Fatalf("get = %d %s", code, body)
	}
	item := asMap(t, toJSON(t, items[0]))
	if _, has := item["room_id"]; has {
		t.Fatalf("item must not include room_id: %v", item)
	}
	if item["user_id"] != userA || item["content"] != "hello" {
		t.Fatalf("item = %v", item)
	}

	// get single (existing)
	msgID, _ := item["id"].(string)
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/messages/"+msgID, nil, "")
	if code != 200 || asMap(t, body)["id"] != msgID {
		t.Fatalf("get single = %d %s", code, body)
	}

	// get single (missing but valid id) → 404 text Not Found
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/messages/"+fixedID, nil, "")
	if code != 404 || body != "Not Found" {
		t.Fatalf("get missing = %d %q; want 404 'Not Found'", code, body)
	}

	// edit (matching user) → 204
	code, body, _ = ct.do(t, "POST", "/project/"+pid+"/messages/"+msgID+"/edit",
		map[string]any{"content": "hi", "userId": userA}, "json")
	if code != 204 || body != "" {
		t.Fatalf("edit = %d %q", code, body)
	}
	msg := ct.m.messages[0]
	if msg.Content != "hi" || msg.EditedAt == nil || *msg.EditedAt != 1000 {
		t.Fatalf("edited message = %+v", msg)
	}

	// edit (different valid user) → 404 Not Found (modifiedCount 0)
	code, body, _ = ct.do(t, "POST", "/project/"+pid+"/messages/"+msgID+"/edit",
		map[string]any{"content": "x", "userId": userC}, "json")
	if code != 404 || body != "Not Found" {
		t.Fatalf("edit no-match = %d %q", code, body)
	}

	// delete missing (valid ids, no message) → 204 (Node: no existence check)
	code, body, _ = ct.do(t, "DELETE", "/project/"+pid+"/messages/"+fixedID, nil, "")
	if code != 204 || body != "" {
		t.Fatalf("delete missing = %d %q; want 204 empty", code, body)
	}

	// delete existing → 204 + gone
	code, _, _ = ct.do(t, "DELETE", "/project/"+pid+"/messages/"+msgID, nil, "")
	if code != 204 || len(ct.m.messages) != 0 {
		t.Fatalf("delete = %d, messages=%d", code, len(ct.m.messages))
	}
}

func TestThreadLifecycleAndGrouping(t *testing.T) {
	ct := newChatTest(t)
	// send thread message (room auto-created with thread_id)
	code, body, _ := ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/messages",
		map[string]any{"user_id": userA, "content": "one"}, "json")
	if code != 201 {
		t.Fatalf("send = %d %s", code, body)
	}
	ct.srv.nowMs = func() int64 { return 2000 }
	// a second message from another user
	code2, b2, _ := ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/messages",
		map[string]any{"user_id": userB, "content": "two"}, "json")
	if code2 != 201 {
		t.Fatalf("second send = %d %s", code2, b2)
	}

	// /threads lists the thread; messages sorted ascending
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/threads", nil, "")
	if code != 200 {
		t.Fatalf("threads = %d %s", code, body)
	}
	threads := asMap(t, body)
	threadRaw, ok := threads[tid1]
	if !ok {
		t.Fatalf("threads missing %s: %s", tid1, body)
	}
	tm := asMap(t, toJSON(t, threadRaw))
	msgs := asArr(t, toJSON(t, tm["messages"]))
	if len(msgs) != 2 {
		t.Fatalf("msgs = %s", body)
	}
	first := asMap(t, toJSON(t, msgs[0]))
	second := asMap(t, toJSON(t, msgs[1]))
	if first["content"] != "one" || second["content"] != "two" {
		t.Fatalf("order = %s", body)
	}

	// resolve
	code, body, _ = ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/resolve",
		map[string]any{"user_id": userA}, "json")
	if code != 204 {
		t.Fatalf("resolve = %d %s", code, body)
	}
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/threads", nil, "")
	threads = asMap(t, body)
	tm = asMap(t, toJSON(t, threads[tid1]))
	if tm["resolved"] != true || tm["resolved_by_user_id"] != userA {
		t.Fatalf("resolved state = %s", body)
	}
	if !strings.HasSuffix(tm["resolved_at"].(string), "Z") {
		t.Fatalf("resolved_at = %v", tm["resolved_at"])
	}

	// resolved-thread-ids
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/resolved-thread-ids", nil, "")
	if code != 200 || !strings.Contains(body, tid1) {
		t.Fatalf("resolved ids = %d %s", code, body)
	}

	// getThread → messages + resolved object
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/thread/"+tid1, nil, "")
	if code != 200 || !strings.Contains(body, `"resolved":true`) {
		t.Fatalf("thread = %d %s", code, body)
	}

	// reopen → resolved gone
	code, _, _ = ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/reopen", nil, "")
	if code != 204 {
		t.Fatalf("reopen = %d", code)
	}
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/resolved-thread-ids", nil, "")
	if code != 200 || strings.Contains(body, tid1) {
		t.Fatalf("resolved ids after reopen = %d %s", code, body)
	}

	// deleteThread → 204 + thread gone (subsequent getThread → 404)
	code, _, _ = ct.do(t, "DELETE", "/project/"+pid+"/thread/"+tid1, nil, "")
	if code != 204 {
		t.Fatalf("deleteThread = %d", code)
	}
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/thread/"+tid1, nil, "")
	if code != 404 || body != "Not Found" {
		t.Fatalf("getThread after delete = %d %q", code, body)
	}
}

func TestResolveNonExistentRoomStill204(t *testing.T) {
	ct := newChatTest(t)
	c1, b1, _ := ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/resolve",
		map[string]any{"user_id": userA}, "json")
	if c1 != 204 || b1 != "" {
		t.Fatalf("resolve missing = %d %q; want 204 (Node updateOne no-op)", c1, b1)
	}
	c2, b2, _ := ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/reopen", nil, "")
	if c2 != 204 || b2 != "" {
		t.Fatalf("reopen missing = %d %q; want 204", c2, b2)
	}
}

func TestDestroyProject(t *testing.T) {
	ct := newChatTest(t)
	ct.do(t, "POST", "/project/"+pid+"/messages", map[string]any{"user_id": userA, "content": "g"}, "json")
	ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/messages", map[string]any{"user_id": userA, "content": "t"}, "json")
	if len(ct.m.rooms) != 2 || len(ct.m.messages) != 2 {
		t.Fatalf("setup state rooms=%d msgs=%d", len(ct.m.rooms), len(ct.m.messages))
	}
	code, body, _ := ct.do(t, "DELETE", "/project/"+pid, nil, "")
	if code != 204 || body != "" {
		t.Fatalf("destroy = %d %q", code, body)
	}
	if len(ct.m.rooms) != 0 || len(ct.m.messages) != 0 {
		t.Fatalf("after destroy rooms=%d msgs=%d", len(ct.m.rooms), len(ct.m.messages))
	}
}

// ---- duplicate / clone -------------------------------------------------------------

func TestDuplicateCommentThreads(t *testing.T) {
	ct := newChatTest(t)
	ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/messages",
		map[string]any{"user_id": userA, "content": "orig"}, "json")

	// valid duplicate → new duplicateId (a new ObjectId hex)
	code, body, _ := ct.do(t, "POST", "/project/"+pid+"/duplicate-comment-threads",
		map[string]any{"threads": []string{tid1}}, "json")
	if code != 200 || !strings.Contains(body, `"newThreads"`) || !strings.Contains(body, `"duplicateId"`) {
		t.Fatalf("duplicate = %d %s", code, body)
	}
	// duplicate adds a room with a NEW thread_id; the source stays (Node
	// duplicateThread keeps the original room)
	foundDup := false
	for _, r := range ct.m.rooms {
		if r.ThreadID != nil && r.ThreadID.Hex() != tid1 {
			foundDup = true
		}
	}
	if len(ct.m.rooms) != 2 || !foundDup {
		t.Fatalf("duplicate must add a room with a new thread_id; rooms=%+v", ct.m.rooms)
	}

	// missing thread id (valid hex, not present) → {error:'not found'}
	ct2 := newChatTest(t)
	code, body, _ = ct2.do(t, "POST", "/project/"+pid+"/duplicate-comment-threads",
		map[string]any{"threads": []string{fixedID}}, "json")
	if code != 200 || !strings.Contains(body, `"error":"not found"`) {
		t.Fatalf("duplicate missing = %d %s", code, body)
	}
}

func TestCloneCommentThreads(t *testing.T) {
	ct := newChatTest(t)
	// source: one thread with two messages (first then edited via the API)
	ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/messages", map[string]any{"user_id": userA, "content": "m1"}, "json")
	ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/messages", map[string]any{"user_id": userB, "content": "m2"}, "json")
	room := ct.m.rooms[0]
	msgID := ct.m.messages[0].ID
	ct.srv.nowMs = func() int64 { return 500 }
	editPath := "/project/" + pid + "/thread/" + room.ThreadID.Hex() + "/messages/" + msgID.Hex() + "/edit"
	code, body, _ := ct.do(t, "POST", editPath, map[string]any{"content": "m1-edited", "userId": userA}, "json")
	if code != 204 {
		t.Fatalf("edit = %d %s", code, body)
	}

	// clone into the (empty) target project — any valid objectId works as target
	targetID, _ := primitive.ObjectIDFromHex(userC)
	code, body, _ = ct.do(t, "POST", "/project/"+pid+"/clone-comment-threads",
		map[string]any{"targetProjectId": userC}, "json")
	if code != 204 || body != "" {
		t.Fatalf("clone = %d %q", code, body)
	}
	var tRooms []*Room
	for _, r := range ct.m.rooms {
		if r.ProjectID == targetID {
			tRooms = append(tRooms, r)
		}
	}
	tRoomIDs := map[string]bool{}
	for _, r := range tRooms {
		tRoomIDs[r.ID.Hex()] = true
	}
	var tMsgs int
	var first *Message
	for _, msg := range ct.m.messages {
		if !tRoomIDs[msg.RoomID.Hex()] {
			continue
		}
		tMsgs++
		if msg.Content == "m1-edited" {
			first = msg
		}
	}
	if len(tRooms) != 1 {
		t.Fatalf("target rooms = %d; want 1", len(tRooms))
	}
	if tMsgs != 2 {
		t.Fatalf("target messages = %d; want 2", tMsgs)
	}
	// cloned first message: content preserved, edited_at dropped (Node deletes it)
	if first == nil || first.EditedAt != nil {
		t.Fatalf("cloned first message must drop edited_at: %+v", first)
	}
	// source untouched (Node clone does not delete)
	pidObj, _ := primitive.ObjectIDFromHex(pid)
	var sRooms int
	for _, r := range ct.m.rooms {
		if r.ProjectID == pidObj {
			sRooms++
		}
	}
	if sRooms != 1 {
		t.Fatalf("source rooms altered: %d", sRooms)
	}
	if len(ct.m.messages) != 4 { // 2 source + 2 cloned
		t.Fatalf("total messages = %d; want 4", len(ct.m.messages))
	}
}

func TestGenerateThreadData(t *testing.T) {
	ct := newChatTest(t)
	ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/messages",
		map[string]any{"user_id": userA, "content": "orig"}, "json")
	code, body, _ := ct.do(t, "POST", "/project/"+pid+"/generate-thread-data",
		map[string]any{"threads": []string{tid1}}, "json")
	if code != 200 || !strings.Contains(body, `"messages"`) {
		t.Fatalf("generate = %d %s", code, body)
	}
}

// ---- validation helpers -------------------------------------------------------------

func TestEditBodyValidation(t *testing.T) {
	ct := newChatTest(t)
	ct.do(t, "POST", "/project/"+pid+"/messages", map[string]any{"user_id": userA, "content": "x"}, "json")
	msgID := ct.m.messages[0].ID.Hex()
	edit := "/project/" + pid + "/messages/" + msgID + "/edit"

	// content required (custom error 'No content provided')
	code, body, _ := ct.do(t, "POST", edit, map[string]any{"userId": userA}, "json")
	if code != 400 || !strings.Contains(errOf(t, body), `No content provided at "body.content"`) {
		t.Fatalf("content missing = %d %s", code, body)
	}
	// userId invalid (present) → refine failure 'invalid Mongo ObjectId'
	code, body, _ = ct.do(t, "POST", edit, map[string]any{"content": "y", "userId": "zz"}, "json")
	if code != 400 || !strings.Contains(errOf(t, body), `invalid Mongo ObjectId at "body.userId"`) {
		t.Fatalf("userId invalid = %d %s", code, body)
	}
	// unknown key (strictObject)
	code, body, _ = ct.do(t, "POST", edit, map[string]any{"content": "y", "nope": 1}, "json")
	if code != 400 || !strings.Contains(errOf(t, body), `Unrecognized key: "nope" at "body"`) {
		t.Fatalf("unknown key = %d %s", code, body)
	}
}

func TestMessageLengthBoundaries(t *testing.T) {
	ct := newChatTest(t)
	ok := strings.Repeat("a", 10240)
	code, body, _ := ct.do(t, "POST", "/project/"+pid+"/messages",
		map[string]any{"user_id": userA, "content": ok}, "json")
	if code != 201 {
		t.Fatalf("10240 chars send = %d %s", code, body)
	}
	tooLong := strings.Repeat("a", 10241)
	code, body, _ = ct.do(t, "POST", "/project/"+pid+"/messages",
		map[string]any{"user_id": userA, "content": tooLong}, "json")
	if code != 400 || !strings.Contains(errOf(t, body), `Content too long (> 10240 bytes) at "body.content"`) {
		t.Fatalf("10241 send = %d %s", code, body)
	}
	// empty content
	code, body, _ = ct.do(t, "POST", "/project/"+pid+"/messages",
		map[string]any{"user_id": userA, "content": ""}, "json")
	if code != 400 {
		t.Fatalf("empty content = %d %s", code, body)
	}
}

// ---- body size limit (Node-observed overflow) ---------------------------------------

func TestBodyOverflowIs500JSON(t *testing.T) {
	ct := newChatTest(t)
	payload := strings.Repeat("a", 120*1024)
	reqBody := `{"user_id":"` + userA + `","content":"` + payload + `"}`
	code, body, hdr := ct.do(t, "POST", "/project/"+pid+"/messages", reqBody, "json")
	want := `{"message":"Internal error: request entity too large"}`
	if code != 500 || body != want {
		t.Fatalf("overflow = %d %q; want 500 %s", code, body, want)
	}
	if !strings.HasPrefix(hdr.Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type = %q", hdr.Get("Content-Type"))
	}
}

// ---- pagination semantics -----------------------------------------------------------

func TestPaginationSemantics(t *testing.T) {
	ct := newChatTest(t)
	for i := 1; i <= 3; i++ {
		ts := int64(100 * i)
		ct.srv.nowMs = func() int64 { return ts }
		ct.do(t, "POST", "/project/"+pid+"/messages", map[string]any{"user_id": userA, "content": fmt.Sprintf("m%d", i-1)}, "json")
	}
	// m0@100, m1@200, m2@300. before=300 → strictly older → m0,m1.
	code, body, _ := ct.do(t, "GET", "/project/"+pid+"/messages?before=300", nil, "")
	if code != 200 || len(asArr(t, body)) != 2 {
		t.Fatalf("before=300 → %d %s (want m0,m1)", code, body)
	}
	// before=0 (falsy in Node) → no filter → all three
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/messages?before=0", nil, "")
	if code != 200 || len(asArr(t, body)) != 3 {
		t.Fatalf("before=0 → %d %s (want all 3)", code, body)
	}
	code, body, _ = ct.do(t, "GET", "/project/"+pid+"/messages?limit=1", nil, "")
	items := asArr(t, body)
	if code != 200 || len(items) != 1 {
		t.Fatalf("limit=1 → %d %s", code, body)
	}
	if asMap(t, toJSON(t, items[0]))["content"] != "m2" { // newest first (sort desc)
		t.Fatalf("limit=1 newest = %s", body)
	}
}

// ---- notification fan-out (Node: NotificationsManager 1:1) --------------------------

func TestNotificationFanoutCommentAndReply(t *testing.T) {
	ct := newChatTest(t)
	owner, _ := primitive.ObjectIDFromHex(userA)
	userBID, _ := primitive.ObjectIDFromHex(userB)
	userCID, _ := primitive.ObjectIDFromHex(userC)
	nameA := "Alice"
	emailA := "alice@example.org"
	pidObj, _ := primitive.ObjectIDFromHex(pid)
	ct.m.projects[pid] = &ProjectRefs{
		Owner:    &owner,
		Collab:   []primitive.ObjectID{userBID, userCID},
		AccessRO: []primitive.ObjectID{owner}, // duplicate on purpose: dedupe expected
		Name:     &nameA,
	}
	ct.m.users[userA] = &UserNames{FirstName: &nameA, Email: &emailA}
	ct.m.users[userC] = &UserNames{Email: &emailA} // no names → email fallback
	// userB is muted at project level (project_id: null)
	ct.m.prefs = append(ct.m.prefs, map[string]any{
		"user_id": userBID, "muteAllNotifications": true, "project_id": nil,
	})
	// userC disables replies-to-participants
	ct.m.prefs = append(ct.m.prefs, map[string]any{
		"user_id": userCID, "project_id": pidObj, "repliesOnParticipatingThread": false,
	})

	// COMMENT (first message, by owner userA = sender). Node excludes the
	// sender from recipients: userA (sender), userB (muted), userC invited
	// with commentOnInvitedProject default-true → exactly userC.
	code, body, _ := ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/messages",
		map[string]any{"user_id": userA, "content": "first"}, "json")
	if code != 201 {
		t.Fatalf("send = %d %s", code, body)
	}
	if len(ct.m.notifications) != 1 {
		t.Fatalf("comment notifications = %d; want 1 (invited userC only); got %+v", len(ct.m.notifications), ct.m.notifications)
	}
	n := ct.m.notifications[0]
	uid, _ := n["user_id"].(primitive.ObjectID)
	if uid != userCID {
		t.Fatalf("comment recipient = %s; want userC", uid.Hex())
	}
	if n["templateKey"] != "notification_comment_on_project" {
		t.Fatalf("templateKey = %v", n["templateKey"])
	}
	key := n["key"].(string)
	opts := n["messageOpts"].(map[string]any)
	if !strings.HasPrefix(key, "project-comment-"+pid+"-"+tid1) {
		t.Fatalf("key = %q", key)
	}
	if opts["projectName"] != nameA || opts["userName"] != "Alice" || opts["projectId"] != pid || opts["threadId"] != tid1 {
		t.Fatalf("messageOpts = %v", opts)
	}
	// email notifications: one per recipient; isComment=true
	if len(ct.m.emailNotifs) != 1 {
		t.Fatalf("emailNotifs = %d; want 1", len(ct.m.emailNotifs))
	}
	if ct.m.emailNotifs[0]["opts"].(map[string]any)["isComment"] != true {
		t.Fatalf("email opts = %v", ct.m.emailNotifs[0]["opts"])
	}

	// REPLY (second message, by userC). Recipients: owner userA = thread
	// author → repliesOnAuthoredThread default true; userB muted; userC is
	// the sender (excluded) and would be off via its participating pref.
	before := len(ct.m.notifications)
	code, body, _ = ct.do(t, "POST", "/project/"+pid+"/thread/"+tid1+"/messages",
		map[string]any{"user_id": userC, "content": "second"}, "json")
	if code != 201 {
		t.Fatalf("reply send = %d %s", code, body)
	}
	added := ct.m.notifications[before:]
	if len(added) != 1 {
		t.Fatalf("reply notifications = %d; want 1; got %+v", len(added), added)
	}
	rid, _ := added[0]["user_id"].(primitive.ObjectID)
	if rid != owner {
		t.Fatalf("reply recipient = %s; want owner userA", rid.Hex())
	}
	if added[0]["templateKey"] != "notification_reply_on_project" {
		t.Fatalf("reply templateKey = %v", added[0]["templateKey"])
	}
	if !strings.HasPrefix(added[0]["key"].(string), "project-reply-"+pid+"-"+tid1) {
		t.Fatalf("reply key = %v", added[0]["key"])
	}
	if added[0]["messageOpts"].(map[string]any)["userName"] != emailA { // sender userC has no names → email fallback
		t.Fatalf("reply userName = %v", added[0]["messageOpts"].(map[string]any)["userName"])
	}
}

func TestNoRefsNoProjectNoNotifications(t *testing.T) {
	ct := newChatTest(t)
	// project with zero refs → no fan-out, but message still 201
	code, body, _ := ct.do(t, "POST", "/project/"+pid+"/messages",
		map[string]any{"user_id": userA, "content": "x"}, "json")
	if code != 201 {
		t.Fatalf("send = %d %s", code, body)
	}
	if len(ct.m.notifications) != 0 || len(ct.m.emailNotifs) != 0 {
		t.Fatalf("unexpected notification docs: %+v", ct.m.notifications)
	}

	// project missing entirely → logged and skipped, still 201
	ct2 := newChatTest(t)
	code, body, _ = ct2.do(t, "POST", "/project/"+pid+"/messages",
		map[string]any{"user_id": userA, "content": "x"}, "json")
	if code != 201 || len(ct2.m.notifications) != 0 {
		t.Fatalf("missing project = %d %s ntf=%d", code, body, len(ct2.m.notifications))
	}
}

func TestSenderNameFallbacks(t *testing.T) {
	if got := senderName(nil); got != "Someone" {
		t.Fatalf("nil user = %q", got)
	}
	first := "Al"
	last := "ice"
	if got := senderName(&UserNames{FirstName: &first, LastName: &last}); got != "Al ice" {
		t.Fatalf("names = %q", got)
	}
	emptyFirst := ""
	email := "x@y.z"
	if got := senderName(&UserNames{FirstName: &emptyFirst, Email: &email}); got != email {
		t.Fatalf("email fallback = %q", got)
	}
	if got := senderName(&UserNames{}); got != "Someone" {
		t.Fatalf("empty user = %q", got)
	}
}

// ---- json helpers ------------------------------------------------------------------

func toJSON(t *testing.T, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
