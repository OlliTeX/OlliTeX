package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- test harness ---------------------------------------------------------

type fakeSessionDoc = map[string]any

type fakeSessionSource struct {
	mu   sync.Mutex
	docs map[string]fakeSessionDoc
}

func (f *fakeSessionSource) Session(_ context.Context, sid string) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.docs[sid]
	if !ok {
		return nil, nil
	}
	return d, nil
}

type capture struct {
	mu    sync.Mutex
	frags []string
}

func (c *capture) deliver(pkt string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frags = append(c.frags, pkt)
	return true
}

func (c *capture) all() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.frags, "\n")
}

// fakeWeb — the Go web private join endpoint, pinned to joinview.go.
type fakeWeb struct {
	mu       sync.Mutex
	project  map[string]any
	level    string
	status   int // 0 → 200
	calls    []string
	restrict bool // isRestrictedUser
}

func (f *fakeWeb) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, fmt.Sprintf("%s %s", r.Method, r.URL.Path))
		f.mu.Unlock()
		var body struct {
			UserID    string `json:"userId"`
			AnonToken string `json:"anonymousAccessToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.UserID == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`Invalid input: expected string, received undefined at "body.userId"`))
			return
		}
		switch {
		case r.URL.Path == "/missing/join":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Not Found"))
		case f.status == http.StatusForbidden:
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("Forbidden"))
		default:
			w.Header().Set("Content-Type", "application/json")
			f.mu.Lock()
			project, level, restrict := f.project, f.level, f.restrict
			f.mu.Unlock()
			out := map[string]any{
				"project":          project,
				"privilegeLevel":   level,
				"isRestrictedUser": restrict,
				"isTokenMember":    false,
				"isInvitedMember":  false,
			}
			b, _ := json.Marshal(out)
			w.Write(b)
		}
	})
	return mux
}

const testSecret = "test-secret-0123456789"
const testSID = "abc123def456abc123def4"

func signedCookie(t *testing.T, sid, secret string) string {
	t.Helper()
	return "overleaf.sid=" + "s:" + signCookie(sid, secret) // (no pct-encoding needed for this value)
}

// projectModel — a minimal joinProjectResponse project (rootFolder with one
// doc + one nested folder/doc), matching joinProjectModelView shape.
func projectModel() map[string]any {
	return map[string]any{
		"_id":              pid,
		"name":             "test",
		"rootDoc_id":       d1,
		"publicAccesLevel": "private",
		"rootFolder": map[string]any{
			"_id":  "6ab73507941509df73b96a40",
			"name": "rootFolder",
			"folders": []any{
				map[string]any{
					"_id":      "6ab73507941509df73b96a41",
					"name":     "sub",
					"docs":     []any{map[string]any{"_id": d2}},
					"fileRefs": []any{},
					"folders":  []any{},
				},
			},
			"docs":     []any{map[string]any{"_id": d1}},
			"fileRefs": []any{},
		},
	}
}

const (
	pid = "6ab73507941509df73b96a34"
	d1  = "6ab73507941509df73b96a35"
	d2  = "6ab73507941509df73b96a36"
)

func newTestBus(t *testing.T, web *fakeWeb, src *fakeSessionSource) *Bus {
	t.Helper()
	srv := httptest.NewServer(web.handler())
	t.Cleanup(srv.Close)
	return New(Options{
		Sessions: &SessionResolver{Source: src, Secrets: []string{testSecret}},
		Web:      &WebAPI{BaseURL: srv.URL, User: "overleaf", Pass: "password"},
		Flush:    &FlushAPI{},
		Redis:    newMemRedis(),
	})
}

func cookieRequest(t *testing.T, cookie string, query string) *http.Request {
	t.Helper()
	r, _ := http.NewRequest("GET", "http://test.local/socket.io/1/websocket/x"+query, nil)
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	return r
}

func joinQuery(pid string, extra string) string { return "?projectId=" + pid + extra }

// --- joins -----------------------------------------------------------------

func TestJoinSuccess(t *testing.T) {
	web := &fakeWeb{project: projectModel(), level: "owner"}
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{testSID: {
		"passport": map[string]any{"user": map[string]any{
			"_id": "6aa4b8a873ef0e5094f4cba3", "first_name": "E2e", "last_name": "Admin", "email": "e2e-admin@e2e.test",
		}},
	}}}
	b := newTestBus(t, web, src)
	cap := &capture{}
	cl := b.Connect(cookieRequest(t, signedCookie(t, testSID, testSecret), joinQuery(pid, "")),
		testSID, "websocket", "127.0.0.1", "ua", map[string]string{"projectId": pid}, cap.deliver)

	got := cap.all()
	if !strings.Contains(got, `"name":"joinProjectResponse"`) {
		t.Fatalf("no joinProjectResponse delivered: %q", got)
	}
	if !strings.Contains(got, `"publicId":"P.`) ||
		!strings.Contains(got, `"permissionsLevel":"owner"`) ||
		!strings.Contains(got, `"protocolVersion":2`) {
		t.Fatalf("joinProjectResponse payload wrong: %q", got)
	}
	// doc grants: d1 + d2 (nested)
	if !cl.docAccess[d1] || !cl.docAccess[d2] {
		t.Fatalf("doc grants wrong: %v", cl.docAccess)
	}
	if cl.JoinProject != pid || cl.Privilege != "owner" {
		t.Fatalf("client state wrong: %+v", cl)
	}
}

func TestJoinForbidden(t *testing.T) {
	web := &fakeWeb{status: http.StatusForbidden}
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{testSID: {
		"passport": map[string]any{"user": map[string]any{"_id": "6aa4b8a873ef0e5094f4cba3"}},
	}}}
	b := newTestBus(t, web, src)
	cap := &capture{}
	b.Connect(cookieRequest(t, signedCookie(t, testSID, testSecret), joinQuery(pid, "")),
		testSID, "websocket", "127.0.0.1", "ua", map[string]string{"projectId": pid}, cap.deliver)
	if got := cap.all(); !strings.Contains(got, `"message":"not authorized"`) {
		t.Fatalf("want 'not authorized' rejection, got: %q", got)
	}
}

func TestJoinNotFound(t *testing.T) {
	// web returns 404 "Not Found" for ghost projects
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{testSID: {
		"passport": map[string]any{"user": map[string]any{"_id": "6aa4b8a873ef0e5094f4cba3"}},
	}}}
	b := New(Options{
		Sessions: &SessionResolver{Source: src, Secrets: []string{testSecret}},
		Web:      &WebAPI{},
		Flush:    &FlushAPI{},
		Redis:    newMemRedis(),
	})
	neg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not Found"))
	}))
	defer neg.Close()
	b.Web.BaseURL = neg.URL
	cap := &capture{}
	b.Connect(cookieRequest(t, signedCookie(t, testSID, testSecret), joinQuery(pid, "")),
		testSID, "websocket", "127.0.0.1", "ua", map[string]string{"projectId": pid}, cap.deliver)
	if got := cap.all(); !strings.Contains(got, `"message":"project not found"`) {
		t.Fatalf("want 'project not found', got: %q", got)
	}
}

func TestJoinInvalidSession(t *testing.T) {
	web := &fakeWeb{project: projectModel(), level: "owner"}
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{}} // no session
	b := newTestBus(t, web, src)
	cap := &capture{}
	b.Connect(cookieRequest(t, signedCookie(t, "unknown-sid", testSecret), joinQuery(pid, "")),
		testSID, "websocket", "127.0.0.1", "ua", map[string]string{"projectId": pid}, cap.deliver)
	if got := cap.all(); !strings.Contains(got, `"message":"invalid session"`) {
		t.Fatalf("want 'invalid session', got: %q", got)
	}
}

func TestJoinBadSecret(t *testing.T) {
	web := &fakeWeb{project: projectModel(), level: "owner"}
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{testSID: {
		"passport": map[string]any{"user": map[string]any{"_id": "u1"}},
	}}}
	b := newTestBus(t, web, src)
	cap := &capture{}
	b.Connect(cookieRequest(t, signedCookie(t, testSID, "WRONG-SECRET"), joinQuery(pid, "")),
		testSID, "websocket", "127.0.0.1", "ua", map[string]string{"projectId": pid}, cap.deliver)
	if got := cap.all(); !strings.Contains(got, `"message":"invalid session"`) {
		t.Fatalf("want 'invalid session' for bad signature, got: %q", got)
	}
}

func TestJoinMissingProjectID(t *testing.T) {
	web := &fakeWeb{project: projectModel(), level: "owner"}
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{testSID: {
		"passport": map[string]any{"user": map[string]any{"_id": "u1"}},
	}}}
	b := newTestBus(t, web, src)
	q := map[string]string{} // no projectId
	r, _ := http.NewRequest("GET", "http://test.local/socket.io/1/websocket/x", nil)
	r.Header.Set("Cookie", signedCookie(t, testSID, testSecret))
	cap := &capture{}
	b.Connect(r, testSID, "websocket", "127.0.0.1", "ua", q, cap.deliver)
	if got := cap.all(); !strings.Contains(got, `"message":"missing/bad ?projectId=... query flag on handshake"`) {
		t.Fatalf("want projectId rejection, got: %q", got)
	}
}

// --- presence --------------------------------------------------------------

func TestPresenceGetAndDisconnect(t *testing.T) {
	web := &fakeWeb{project: projectModel(), level: "owner"}
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{
		"a": {"passport": map[string]any{"user": map[string]any{"_id": "uA", "first_name": "A", "last_name": "One", "email": "a@x.y"}}},
		"b": {"passport": map[string]any{"user": map[string]any{"_id": "uB", "first_name": "B", "last_name": "Two", "email": "b@x.y"}}},
	}}
	b := newTestBus(t, web, src)

	mk := func(sid string) *Client {
		r, _ := http.NewRequest("GET", "http://t/x", nil)
		r.Header.Set("Cookie", signedCookie(t, sid, testSecret))
		return b.Connect(r, sid, "websocket", "127.0.0.1", "ua", map[string]string{"projectId": pid}, func(string) bool { return true })
	}
	// give both users distinct public identities via different cookies; the
	// bus keys presence by publicId — two sessions, one room.
	a := mk("a")
	bc := mk("b")

	time.Sleep(50 * time.Millisecond)
	// B asks for connected users → must include A (and itself).
	// Drive B's getConnectedUsers with an ack, then verify the ack frame.
	bcap := &capture{}
	{
		// re-bind B's write sink to capture (connect already delivered join)
		bc.mu.Lock()
		bc.writeFn = bcap.deliver
		bc.mu.Unlock()
	}
	b.Dispatch(bc, Packet{Type: TypeEvent, ID: "7", Ack: true, Name: EvClientTrackingGetUsers})
	deadline := time.Now().Add(3 * time.Second)
	var ack string
	for time.Now().Before(deadline) {
		if strings.Contains(bcap.all(), "6:::7+") {
			ack = bcap.all()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ack == "" {
		t.Fatalf("no ack for getConnectedUsers (got: %q)", bcap.all())
	}
	if !strings.Contains(ack, `"user_id":"uA"`) || !strings.Contains(ack, `"user_id":"uB"`) {
		t.Fatalf("getConnectedUsers did not list both users: %q", ack)
	}
	if !regexp.MustCompile(`"connected":true`).MatchString(ack) {
		t.Fatalf("getConnectedUsers shape wrong: %q", ack)
	}
	_ = a

	// A disconnects → B gets clientTracking.clientDisconnected with A's publicId
	b.mu.Lock()
	pubA := a.PublicID
	b.mu.Unlock()
	bcap2 := &capture{}
	bc.mu.Lock()
	bc.writeFn = bcap2.deliver
	bc.mu.Unlock()
	b.Close(a.ID)
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(bcap2.all(), EvClientTrackingDisc) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := bcap2.all(); !strings.Contains(got, "\""+EvClientTrackingDisc+"\"") || !strings.Contains(got, pubA) {
		t.Fatalf("no clientTracking.clientDisconnected with publicId %s: %q", pubA, got)
	}
}

func TestUpdatePositionBroadcasts(t *testing.T) {
	web := &fakeWeb{project: projectModel(), level: "owner"}
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{
		"a": {"passport": map[string]any{"user": map[string]any{"_id": "uA", "first_name": "A", "last_name": "", "email": "a@x.y"}}},
		"b": {"passport": map[string]any{"user": map[string]any{"_id": "uB", "first_name": "B", "last_name": "", "email": "b@x.y"}}},
	}}
	b := newTestBus(t, web, src)
	drop := func(string) bool { return true }
	a := b.Connect(cookieRequest(t, signedCookie(t, "a", testSecret), joinQuery(pid, "")), "a", "websocket", "x", "ua", map[string]string{"projectId": pid}, drop)
	bc := b.Connect(cookieRequest(t, signedCookie(t, "b", testSecret), joinQuery(pid, "")), "b", "websocket", "x", "ua", map[string]string{"projectId": pid}, drop)

	bcap := &capture{}
	bc.mu.Lock()
	bc.writeFn = bcap.deliver
	bc.mu.Unlock()

	// A sends cursor at d1 (granted) → B must receive clientTracking.clientUpdated
	b.Dispatch(a, Packet{Type: TypeEvent, Name: EvClientTrackingUpdate, Args: []json.RawMessage{
		[]byte(`{"row":3,"column":7,"doc_id":"` + d1 + `"}`),
	}})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !strings.Contains(bcap.all(), EvClientTrackingUpdated) {
		time.Sleep(20 * time.Millisecond)
	}
	got := bcap.all()
	if !strings.Contains(got, EvClientTrackingUpdated) {
		t.Fatalf("B never got clientTracking.clientUpdated: %q", got)
	}
	if !strings.Contains(got, `"row":3`) || !strings.Contains(got, `"column":7`) ||
		!strings.Contains(got, `"doc_id":"`+d1+`"`) || !strings.Contains(got, `"user_id":"uA"`) {
		t.Fatalf("clientUpdated payload wrong: %q", got)
	}

	// A sends cursor at an UNGRAUNTED doc → no broadcast (silent reject)
	bcap.Reset()
	b.Dispatch(a, Packet{Type: TypeEvent, Name: EvClientTrackingUpdate, Args: []json.RawMessage{
		[]byte(`{"row":3,"column":7,"doc_id":"6ab73507941509df73b96a99"}`),
	}})
	time.Sleep(150 * time.Millisecond)
	if strings.Contains(bcap.all(), EvClientTrackingUpdated) {
		t.Fatalf("ungranted doc must not broadcast: %q", bcap.all())
	}
}

func (c *capture) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frags = nil
}

// --- drain -----------------------------------------------------------------

func TestDrainRejectsNewAndAsksExisting(t *testing.T) {
	web := &fakeWeb{project: projectModel(), level: "owner"}
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{
		"a": {"passport": map[string]any{"user": map[string]any{"_id": "uA"}}},
	}}
	b := newTestBus(t, web, src)
	drop := func(string) bool { return true }
	a := b.Connect(cookieRequest(t, signedCookie(t, "a", testSecret), joinQuery(pid, "")), "a", "websocket", "x", "ua", map[string]string{"projectId": pid}, drop)

	b.StartDrain(10)
	defer b.StopDrain()

	// existing client gets reconnectGracefully within ~200ms
	icap := &capture{}
	a.mu.Lock()
	a.writeFn = icap.deliver
	a.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(icap.all(), EvReconnectGracefully) {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(icap.all(), EvReconnectGracefully) {
		t.Fatalf("existing client never got reconnectGracefully: %q", icap.all())
	}

	// new connection while draining → connectionRejected 'retry'
	ncap := &capture{}
	b.Connect(cookieRequest(t, signedCookie(t, "a", testSecret), joinQuery(pid, "")),
		"new", "websocket", "x", "ua", map[string]string{"projectId": pid}, ncap.deliver)
	if !strings.Contains(ncap.all(), `"message":"retry"`) {
		t.Fatalf("draining connect must be rejected 'retry': %q", ncap.all())
	}
}

// --- HTTP ops --------------------------------------------------------------

func TestOpsAPIs(t *testing.T) {
	web := &fakeWeb{project: projectModel(), level: "owner"}
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{
		"a": {"passport": map[string]any{"user": map[string]any{"_id": "uA", "first_name": "A", "last_name": "", "email": "a@x.y"}}},
	}}
	b := newTestBus(t, web, src)
	srv := httptest.NewServer(NewServer(b).Routes())
	defer srv.Close()

	drop := func(string) bool { return true }
	a := b.Connect(cookieRequest(t, signedCookie(t, "a", testSecret), joinQuery(pid, "")), "a", "websocket", "x", "ua", map[string]string{"projectId": pid}, drop)
	_ = a

	get := func(path string) (int, string) {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		buf := make([]byte, 4096)
		n, _ := res.Body.Read(buf)
		return res.StatusCode, string(buf[:n])
	}

	code, _ := get("/status")
	if code != 200 {
		t.Fatalf("/status code = %d", code)
	}
	code, body := get("/clients")
	if code != 200 || !strings.Contains(body, `"client_id":"a"`) || !strings.Contains(body, `"project_id":"`+pid+`"`) {
		t.Fatalf("/clients = %d %s", code, body)
	}
	code, _ = get("/clients/" + "a")
	if code != 200 {
		t.Fatalf("/clients/a = %d", code)
	}
	code, _ = get("/clients/unknown")
	if code != 404 {
		t.Fatalf("/clients/unknown = %d, want 404", code)
	}
	code, body = get("/project/" + pid + "/count-connected-clients")
	if code != 200 || !strings.Contains(body, `"nConnectedClients":1`) {
		t.Fatalf("/count = %d %s", code, body)
	}

	// message relay: connected A receives the room event
	acap := &capture{}
	a.mu.Lock()
	a.writeFn = acap.deliver
	a.mu.Unlock()
	res, err := http.Post(srv.URL+"/project/"+pid+"/message/updateProjectMetadata", "application/json", strings.NewReader(`[{"name":"new name"}]`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !strings.Contains(acap.all(), "updateProjectMetadata") {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(acap.all(), `"name":"updateProjectMetadata"`) || !strings.Contains(acap.all(), `"name":"new name"`) {
		t.Fatalf("message relay not delivered: %q", acap.all())
	}
}
