package wakatime

// Client contract (real httptest WakaTime upstream) + handler routes with
// injected gate/creds seams.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ollitex/go/services/web/core"
)

// --- client against a fake wakatime.com -----------------------------------

type fakeCall struct {
	method string
	path   string
	raw    string
	auth   string
	ua     string
}

func startFakeWaka(t *testing.T, status int, body string, calls *[]fakeCall) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if calls != nil {
			*calls = append(*calls, fakeCall{r.Method, r.URL.Path, string(b), r.Header.Get("Authorization"), r.Header.Get("User-Agent")})
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestClientVerifyAndHeartbeat(t *testing.T) {
	var calls []fakeCall
	srv := startFakeWaka(t, 201, `{"ok":1}`, &calls)
	defer srv.Close()
	c := newWakaClient()
	cr := wakaCreds{APIURL: srv.URL, APIKey: "SECRETKEY"}
	if err := c.verifyCredentials(context.Background(), cr); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := c.sendHeartbeat(context.Background(), cr, map[string]any{"entity": "main.tex"}); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	want := 2
	if len(calls) != want {
		t.Fatalf("calls: got %d, want %d", len(calls), want)
	}
	if calls[0].path != "/users/current" || calls[0].method != "GET" {
		t.Fatalf("verify call: %+v", calls[0])
	}
	wantAuth := "Basic U0VDUkVUS0VZ" // base64("SECRETKEY")
	if calls[0].auth != wantAuth {
		t.Errorf("auth header: got %q, want %q", calls[0].auth, wantAuth)
	}
	if calls[1].path != "/users/current/heartbeats" || calls[1].method != "POST" {
		t.Fatalf("heartbeat call: %+v", calls[1])
	}
	var hb map[string]any
	if json.Unmarshal([]byte(calls[1].raw), &hb) != nil || hb["entity"] != "main.tex" {
		t.Errorf("heartbeat body: %s", calls[1].raw)
	}
	for _, wantSub := range []string{"wakatime/", "ollitex/"} {
		if !containsAny(calls[1].ua, wantSub) {
			t.Errorf("user-agent %q missing %q", calls[1].ua, wantSub)
		}
	}
}

func TestClientBulkAndSummary(t *testing.T) {
	var calls []fakeCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if len(calls) == 0 && r.URL.Path == "/users/current/summaries" {
		}
		calls = append(calls, fakeCall{r.Method, r.URL.Path + r.URL.RawQuery, string(b), "", ""})
		switch r.URL.Path {
		case "/users/current/heartbeats.bulk":
			w.WriteHeader(201)
		case "/users/current/summaries":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"data":[{"projects":[{"name":"P1","total_seconds":111},{"name":"OTHER","total_seconds":9999}]},{"projects":[{"name":"P1","total_seconds":222}]}]}`))
		default:
			w.WriteHeader(200)
		}
	}))
	defer srv.Close()
	c := newWakaClient()
	cr := wakaCreds{APIURL: srv.URL, APIKey: "K"}
	if err := c.sendHeartbeatsBulk(context.Background(), cr, []map[string]any{{"a": 1}, {"b": 2}}); err != nil {
		t.Fatalf("bulk: %v", err)
	}
	total, err := c.projectSummary(context.Background(), cr, "P1", 7)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if total != 333 {
		t.Errorf("summary total: got %d, want 333", total)
	}
}

func TestClientErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		want   *ErrWaka
	}{
		{201, nil},
		{401, errForbidden},
		{403, errForbidden},
		{404, errNotFound},
		{429, errTooMany},
		{500, nil},
	}
	for _, tc := range cases {
		if tc.want != nil {
			if got := mapStatus(tc.status); got != tc.want {
				t.Errorf("mapStatus(%d) = %v, want %v", tc.status, got, tc.want)
			}
			continue
		}
		// 201 → success path exercised via a client call
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
		}))
		c := newWakaClient()
		err := c.verifyCredentials(context.Background(), wakaCreds{APIURL: srv.URL, APIKey: "K"})
		if tc.status == 201 && err != nil {
			t.Errorf("201 verify: %v", err)
		}
		if tc.status == 500 && (err == nil || err.(*ErrWaka).Status != http.StatusInternalServerError) {
			t.Errorf("500 verify: %v", err)
		}
		srv.Close()
	}
}

// --- handlers (fakes for gate/creds) ---------------------------------------

func handlerTest(t *testing.T, f func(s *svc)) http.Handler {
	t.Helper()
	t.Setenv("WAKATIME_INTEGRATION_ENABLED", "true")
	a := &core.App{}
	s := newSvc(a)
	seedCreds(t, s)
	if f != nil {
		f(s)
	}
	return routerFor(s)
}

func routerFor(s *svc) http.Handler {
	routes := s.Routes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res := &core.Res{W: w}
		cxt := &core.Cxt{
			Req:    r,
			A:      &core.App{},
			Sess:   sessionForTest(testUID),
			Params: map[string]string{},
		}
		for _, rt := range routes {
			if rt.Method != r.Method {
				continue
			}
			if !rt.Pattern.MatchString(r.URL.Path) {
				continue
			}
			m := rt.Pattern.FindStringSubmatch(r.URL.Path)
			names := rt.Pattern.SubexpNames()
			for i := 1; i < len(m) && i < len(names); i++ {
				cxt.Params[names[i]] = m[i]
			}
			rt.Handler(cxt, res)
			return
		}
		w.WriteHeader(404)
	})
}

func sessionForTest(uid string) *core.Session {
	sessJSON := `{"passport":{"user":{"email":"wakatime@test.local","_id":"` + uid + `"}}}`
	s := &core.Session{
		SessID: "testsess-" + uid,
		Doc:    map[string]json.RawMessage{"passport": json.RawMessage(`{"user":{"email":"wakatime@test.local","_id":"` + uid + `"}}`)},
	}
	_ = sessJSON
	return s
}

func do(t *testing.T, h http.Handler, method, path, body string) {
	t.Helper()
	var rdr io.Reader = strings.NewReader(body)
	req := httptest.NewRequest(method, path, rdr)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	want := 200
	if strings.HasSuffix(path, "heartbeat") && !strings.HasSuffix(path, "heartbeats/bulk") {
		want = 202
	}
	if strings.HasSuffix(path, "heartbeats/bulk") {
		want = 202
	}
	if method == "DELETE" {
		want = 200
	}
	t.Logf("%s %s → %d (%s)", method, path, rr.Code, rr.Body.String())
	if rr.Code != want {
		t.Errorf("%s %s: got %d body %q, want %d", method, path, rr.Code, rr.Body.String(), want)
	}
}

func seedCreds(t *testing.T, s *svc) {
	t.Helper()
	key := "K"
	apiURL := "https://wakatime.test/api/v1"
	s.verify = func(ctx context.Context, cr wakaCreds) error {
		if cr.APIKey == key || cr.APIKey == "NEWKEY" {
			return nil
		}
		return errForbidden
	}
	s.storeCreds = func(ctx context.Context, uid, u, k string) error { return nil }
	s.loadCreds = func(ctx context.Context, uid string) (wakaCreds, bool, error) {
		if uid == "64aaaaaa4a5b4c5d4e4f4a5b" {
			return wakaCreds{APIURL: apiURL, APIKey: key}, true, nil
		}
		return wakaCreds{}, false, nil
	}
	s.delCreds = func(ctx context.Context, uid string) error { return nil }
	const pid = "65d43a5b4a5b4c5d4e4f4a5b"
	s.gateOverride = func(cxt *core.Cxt) gateResult {
		if cxt.Params["1"] == pid {
			return gateResult{uid: "64aaaaaa4a5b4c5d4e4f4a5b", projectName: "WakaProj", found: true}
		}
		return gateResult{}
	}
	s.wakaOne = func(ctx context.Context, cr wakaCreds, hb map[string]any) error {
		if hb["project"] != "WakaProj" {
			return errUpstream
		}
		// audit 035: the relay must hand the upstream a canonical WakaTime
		// payload (Wakapi v2.18 rejects the raw frontend shape).
		if hb["category"] != "Development" || hb["language"] == "" {
			return errBadAPI
		}
		if _, has := hb["time"]; !has {
			return errBadAPI
		}
		return nil
	}
	s.wakaBulk = func(ctx context.Context, cr wakaCreds, hbs []map[string]any) error { return nil }
	s.wakaSummary = func(ctx context.Context, cr wakaCreds, project string, days int) (int64, error) {
		return 42, nil
	}
}

func TestStatusRoute(t *testing.T) {
	h := handlerTest(t, nil)
	do(t, h, "GET", "/user/wakatime/status", `{"connected":true,"apiUrl":"https://wakatime.test/api/v1"}`)
}

const (
	testUID    = "64aaaaaa4a5b4c5d4e4f4a5b"
	testPID    = "65d43a5b4a5b4c5d4e4f4a5b"
	testNoBody = ``
)

func TestLinkAndUnlink(t *testing.T) {
	h := handlerTest(t, nil)
	do(t, h, "PUT", "/user/wakatime", `{"apiUrl":"https://wakatime.test/api/v1","apiKey":"NEWKEY"}`)
	do(t, h, "DELETE", "/user/wakatime", ``)
}

func TestHeartbeatRoute(t *testing.T) {
	h := handlerTest(t, nil)
	do(t, h, "POST", "/project/"+testPID+"/wakatime/heartbeat", `{"entity":"main.tex","time":123}`)
}

func TestHeartbeatBulkRoute(t *testing.T) {
	h := handlerTest(t, nil)
	do(t, h, "POST", "/project/"+testPID+"/wakatime/heartbeats/bulk", `[{"entity":"a.tex"},{"entity":"b.tex"}]`)
}

func TestSummaryRoute(t *testing.T) {
	h := handlerTest(t, nil)
	do(t, h, "GET", "/project/"+testPID+"/wakatime/summary", `{"connected":true,"totalSeconds":42,"rangeDays":7}`)
}

func TestBulkLimit(t *testing.T) {
	h := handlerTest(t, nil)
	// 51 heartbeats → 400 "too many heartbeats"
	var hb []string
	for i := 0; i < maxBulkHeartbeats+1; i++ {
		hb = append(hb, `{"entity":"f.tex"}`)
	}
	body := "[" + strings.Join(hb, ",") + "]"
	req := httptest.NewRequest("POST", "/project/"+testPID+"/wakatime/heartbeats/bulk",
		strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("bulk limit: got %d, want 400 (%s)", rr.Code, rr.Body.String())
	}
}

func TestNotLinkedHeartbeat(t *testing.T) {
	h := handlerTest(t, func(s *svc) {
		s.loadCreds = func(ctx context.Context, uid string) (wakaCreds, bool, error) {
			return wakaCreds{}, false, nil
		}
	})
	req := httptest.NewRequest("POST", "/project/"+testPID+"/wakatime/heartbeat",
		strings.NewReader(`{"entity":"f.tex"}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("not-linked: got %d, want 400 (%s)", rr.Code, rr.Body.String())
	}
}

func TestResolveEnabledDefaultOff(t *testing.T) {
	// OlliTeX owner directive: off-by-default (reference repo default ON).
	t.Setenv("WAKATIME_INTEGRATION_ENABLED", "")
	if ResolveEnabled(context.Background(), &core.App{}) {
		t.Error("default must be OFF (opt-in)")
	}
	t.Setenv("WAKATIME_INTEGRATION_ENABLED", "true")
	if !ResolveEnabled(context.Background(), &core.App{}) {
		t.Error("env true must enable")
	}
}

// --- helpers ----------------------------------------------------------------

func containsAny(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
