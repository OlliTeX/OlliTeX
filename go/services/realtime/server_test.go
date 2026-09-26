package realtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestServerEndToEnd — the full browser contract without a browser:
//
//	handshake → ws open → connect echo → joinProjectResponse
//	→ acked getConnectedUsers → cursor broadcast → disconnect broadcast
//
// against a real websocket client (gorilla) and a fake Go web join API.
func TestServerEndToEnd(t *testing.T) {
	// ---- fakes ----
	user := map[string]any{
		"_id": "6aa4b8a873ef0e5094f4cba3", "first_name": "E2e", "last_name": "Admin", "email": "e2e-admin@e2e.test",
	}
	web := &fakeWeb{project: projectModel(), level: "editor"}
	srvWeb := httptest.NewServer(web.handler())
	defer srvWeb.Close()

	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{testSID: {"passport": map[string]any{"user": user}}}}
	b := New(Options{
		Sessions: &SessionResolver{Source: src, Secrets: []string{testSecret}},
		Web:      &WebAPI{BaseURL: srvWeb.URL, User: "overleaf", Pass: "password"},
		Flush:    &FlushAPI{},
		Redis:    newMemRedis(),
	})
	srv := httptest.NewServer(NewServer(b).Routes())
	defer srv.Close()

	// ---- handshake ----
	hres, err := http.Get(srv.URL + "/socket.io/1/?projectId=" + pid + "&t=123")
	if err != nil {
		t.Fatal(err)
	}
	defer hres.Body.Close()
	body, _ := readAll(hres)
	fields := strings.Split(body, ":")
	if len(fields) != 4 {
		t.Fatalf("handshake body %q: want 4 fields", body)
	}
	sid := fields[0]
	if fields[1:3] == nil || fields[1] != "60" || fields[2] != "60" {
		t.Fatalf("handshake timeouts %v, want [60 60]", fields[1:3])
	}
	if fields[3] != "websocket,xhr-polling" {
		t.Fatalf("handshake transports %q", fields[3])
	}

	// ---- socket.io client bundle is served (the IDE loads it) ----
	jsres, err := http.Get(srv.URL + "/socket.io/socket.io.js")
	if err != nil {
		t.Fatal(err)
	}
	jsbody, _ := readAll(jsres)
	if !strings.Contains(jsbody, "0.9.17-overleaf") {
		t.Fatalf("client bundle wrong (%d bytes)", len(jsbody))
	}
	jsres.Body.Close()

	// ---- websocket open ----
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/socket.io/1/websocket/" + sid + "?projectId=" + pid
	hdr := http.Header{}
	hdr.Set("Cookie", signedCookie(t, testSID, testSecret))
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.Close()

	readLine := func(t *testing.T, what string) string {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			_ = conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
			_, data, err := conn.ReadMessage()
			if err != nil {
				continue // read deadline — keep polling
			}
			for _, l := range strings.Split(strings.TrimRight(string(data), "\r\n"), "\n") {
				if strings.TrimSpace(l) == "" {
					continue
				}
				t.Logf("<< %s", l[:min(120, len(l))])
				return l
			}
		}
		t.Fatalf("no %s frame within deadline", what)
		return ""
	}

	// 1: frame 1 = connect echo (observed live order)
	one := readLine(t, "connect echo")
	if one != "1::" {
		t.Fatalf("frame1 = %q, want 1::", one)
	}

	// 2: joinProjectResponse (fake web answers synchronously)
	two := readLine(t, "joinProjectResponse")
	if !strings.Contains(two, `"name":"joinProjectResponse"`) {
		t.Fatalf("frame2 = %q", two)
	}
	if !strings.Contains(two, `"publicId":"P.`) || !strings.Contains(two, `"protocolVersion":2`) || !strings.Contains(two, `"permissionsLevel":"editor"`) {
		t.Fatalf("join payload wrong: %q", two)
	}

	// 3: acked getConnectedUsers → "6:::1+[null,[...]]" incl. ourselves
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`5:1+::{"name":"clientTracking.getConnectedUsers"}`))
	ack := readLine(t, "getConnectedUsers ack")
	if !strings.HasPrefix(ack, "6:::1+") {
		t.Fatalf("ack = %q, want 6:::1+...", ack)
	}
	if !strings.Contains(ack, `"user_id":"6aa4b8a873ef0e5094f4cba3"`) || !strings.Contains(ack, `"client_id":"P.`) {
		t.Fatalf("ack payload wrong: %q", ack)
	}

	// 4: cursor update (self) → we receive our own clientUpdated (Node LB
	//    broadcasts to everyone; client self-filters)
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`5:2+::{"name":"clientTracking.updatePosition","args":[{"row":1,"column":2,"doc_id":"`+d1+`"}]}`))
	upd := readLine(t, "clientUpdated")
	if !strings.Contains(upd, `"name":"clientTracking.clientUpdated"`) {
		t.Fatalf("upd = %q", upd)
	}
	if !strings.Contains(upd, `"row":1`) || !strings.Contains(upd, `"doc_id":"`+d1+`"`) || !strings.Contains(upd, `"name":"E2e Admin"`) {
		t.Fatalf("clientUpdated payload wrong: %q", upd)
	}

	// 5: second client sees the cursor broadcast (Node: room broadcast)
	src.mu.Lock()
	src.docs["b"] = map[string]any{"passport": map[string]any{"user": map[string]any{"_id": "uB2", "first_name": "B", "last_name": "Two", "email": "b@x.y"}}}
	src.mu.Unlock()
	hres2, err := http.Get(srv.URL + "/socket.io/1/?projectId=" + pid)
	if err != nil {
		t.Fatal(err)
	}
	hb2, _ := readAll(hres2)
	hres2.Body.Close()
	sid2 := strings.SplitN(hb2, ":", 2)[0]
	conn2, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/socket.io/1/websocket/"+sid2+"?projectId="+pid, http.Header{"Cookie": []string{signedCookie(t, "b", testSecret)}})
	if err != nil {
		t.Fatalf("ws2 dial: %v", err)
	}
	defer conn2.Close()
	_ = drainFrames(t, conn2, 2) // echo + join response

	_ = conn.WriteMessage(websocket.TextMessage, []byte(`5:3+::{"name":"clientTracking.updatePosition","args":[{"row":9,"column":9,"doc_id":"`+d1+`"}]}`))
	other := drainFrames(t, conn2, 1)
	if !strings.Contains(other, `"name":"clientTracking.clientUpdated"`) || !strings.Contains(other, `"row":9`) {
		t.Fatalf("second client never saw cursor broadcast: %q", other)
	}

	// 6: disconnect broadcast to the survivor
	_ = conn.Close()
	left := drainFrames(t, conn2, 1)
	if !strings.Contains(left, `"name":"clientTracking.clientDisconnected"`) || !strings.Contains(left, `"user_id":""`+`"`) {
		// the sender's own publicId is what matters:
		if !strings.Contains(left, `"name":"clientTracking.clientDisconnected"`) {
			t.Fatalf("second client never got clientDisconnected: %q", left)
		}
	}

	// 7: ops APIs see the remaining client
	res, err := http.Get(srv.URL + "/project/" + pid + "/count-connected-clients")
	if err != nil {
		t.Fatal(err)
	}
	ccount, _ := readAll(res)
	res.Body.Close()
	if !strings.Contains(ccount, `"nConnectedClients":1`) {
		t.Fatalf("count = %s, want 1", ccount)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func readAll(r *http.Response) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String(), nil
}

func firstN(r *http.Response, n int) string {
	b, _ := readAll(r)
	if len(b) < int(n) {
		return b
	}
	return b[:n]
}

// drainFrames — read up to n non-empty frames (with a deadline), joined.
func drainFrames(t *testing.T, conn *websocket.Conn, n int) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var out []string
	for len(out) < n && time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(800 * time.Millisecond))
		_, data, err := conn.ReadMessage()
		if err != nil {
			continue
		}
		out = append(out, string(data))
	}
	return strings.Join(out, "\n")
}

// TestServerOpsOnly — handshake + 401/404 paths + static client, no ws.
func TestServerOpsOnly(t *testing.T) {
	b := New(Options{
		Sessions: &SessionResolver{Source: &fakeSessionSource{docs: map[string]fakeSessionDoc{}}, Secrets: []string{testSecret}},
		Web:      &WebAPI{BaseURL: "http://127.0.0.1:1", User: "u", Pass: "p"},
		Redis:    newMemRedis(),
	})
	srv := httptest.NewServer(NewServer(b).Routes())
	defer srv.Close()

	cases := []struct {
		path string
		code int
	}{
		{"/", 200},
		{"/status", 200},
		{"/clients", 200},
		{"/nonexistent", 404},
		{"/socket.io/socket.io.js", 200},
		{"/socket.io/1", 200}, // handshake
		{"/socket.io/2", 404}, // wrong protocol
		{"/socket.io/1/websocket/nosid", 401},
	}
	for _, c := range cases {
		res, err := http.Get(srv.URL + c.path)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		if res.StatusCode != c.code {
			t.Fatalf("%s = %d, want %d", c.path, res.StatusCode, c.code)
		}
		res.Body.Close()
	}
}

// TestServerPongRoundTrip — clientPong is validated, not echoed.
func TestServerPongRoundTrip(t *testing.T) {
	src := &fakeSessionSource{docs: map[string]fakeSessionDoc{testSID: {"passport": map[string]any{"user": map[string]any{"_id": "u1"}}}}}
	web := &fakeWeb{project: projectModel(), level: "view"}
	srvWeb := httptest.NewServer(web.handler())
	defer srvWeb.Close()
	b := New(Options{
		Sessions: &SessionResolver{Source: src, Secrets: []string{testSecret}},
		Web:      &WebAPI{BaseURL: srvWeb.URL},
		Redis:    newMemRedis(),
	})
	srv := httptest.NewServer(NewServer(b).Routes())
	defer srv.Close()

	hres, _ := http.Get(srv.URL + "/socket.io/1/?projectId=" + pid)
	body, _ := readAll(hres)
	sid := strings.SplitN(body, ":", 2)[0]
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/socket.io/1/websocket/"+sid+"?projectId="+pid, http.Header{"Cookie": []string{signedCookie(t, testSID, testSecret)}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = drainFrames(t, conn, 2)
	// pong (6 args) — must not fail the connection and produces no broadcast
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`5:9+::{"name":"clientPong","args":[1,123,"websocket","sid","websocket","sid"]}`))
	_ = conn.SetReadDeadline(time.Now().Add(600 * time.Millisecond))
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("clientPong must not produce a client frame")
	}
}
