package smoketest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"ollitex/go/services/clsitex/config"
)

func newTestSmoke(t *testing.T, url string) *SmokeTest {
	st := &SmokeTest{timeout: 2 * time.Second, lastError: NewErrPending}
	st.SetCompileURL(url)
	return st
}

// newTestConfig builds a minimal *config.Config for the exported-ctor
// tests (no env, no sandbox).
func newTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Internal.Compile.Host = "127.0.0.1"
	cfg.Internal.Compile.Port = 3013
	return cfg
}

func validEnvelopeResponse(t *testing.T) string {
	return `{"status":"success","buildId":"b1","outputFiles":[{"path":"output.pdf","type":"pdf"},{"path":"output.log","type":"log"}]}`
}

func TestRunSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/project/smoketest-12345/compile" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("unexpected content type: %s", ct)
		}
		// validate the request body is the smoke compile
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("body: %v", err)
		}
		compile, ok := body["compile"].(map[string]any)
		if !ok {
			t.Fatalf("body has no compile key: %v", body)
		}
		resources, _ := compile["resources"].([]any)
		if len(resources) != 1 {
			t.Fatalf("expected 1 resource, got %d", len(resources))
		}
		first := resources[0].(map[string]any)
		if first["path"] != "main.tex" {
			t.Errorf("resource path = %v", first["path"])
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(validEnvelopeResponse(t)))
	}))
	defer srv.Close()
	st := newTestSmoke(t, srv.URL+"/project/smoketest-12345/compile")
	if err := st.Run(context.Background()); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
}

func TestRunNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	st := newTestSmoke(t, srv.URL)
	err := st.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestRunNoPdf(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","buildId":"b1","outputFiles":[{"path":"output.log","type":"log"}]}`))
	}))
	defer srv.Close()
	st := newTestSmoke(t, srv.URL)
	err := st.Run(context.Background())
	if err == nil || err.Error() != "no pdf returned" {
		t.Fatalf("error = %v", err)
	}
}

func TestRunNoLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","buildId":"b1","outputFiles":[{"path":"output.pdf","type":"pdf"}]}`))
	}))
	defer srv.Close()
	st := newTestSmoke(t, srv.URL)
	err := st.Run(context.Background())
	if err == nil || err.Error() != "no log returned" {
		t.Fatalf("error = %v", err)
	}
}

func TestRunIncompletePayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","buildId":"b1","outputFiles":[]}`))
	}))
	defer srv.Close()
	st := newTestSmoke(t, srv.URL)
	err := st.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "response payload incomplete") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	st := newTestSmoke(t, srv.URL)
	err := st.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "response payload incomplete") {
		t.Fatalf("error = %v", err)
	}
}

func TestLastErrorPending(t *testing.T) {
	st := newTestSmoke(t, "http://127.0.0.1:0")
	if st.LastError() == nil {
		t.Fatal("expected pending error")
	}
	if st.LastError().Error() != "SmokeTestsPending" {
		t.Fatalf("pending = %v", st.LastError())
	}
	if st.LastRunSuccessful() {
		t.Fatal("expected LastRunSuccessful false")
	}
}

func TestSendLastResult(t *testing.T) {
	rec := httptest.NewRecorder()
	st := newTestSmoke(t, "http://127.0.0.1:0")
	st.SendLastResult(rec)
	if rec.Code != 500 {
		t.Fatalf("code = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("content type = %s", ct)
	}
	if rec.Body.String() != "SmokeTestsPending" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestSendNewResultFailureAndSuccess(t *testing.T) {
	// failure: no server
	st := newTestSmoke(t, "http://127.0.0.1:1")
	rec := httptest.NewRecorder()
	st.SendNewResult(rec)
	if rec.Code != 500 || st.LastRunSuccessful() {
		t.Fatalf("expected failure, got code=%d", rec.Code)
	}
	if !strings.Contains(st.LastError().Error(), "smoke test: POST") {
		t.Fatalf("lastError = %v", st.LastError())
	}

	// success
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(validEnvelopeResponse(t)))
	}))
	defer srv.Close()
	st.SetCompileURL(srv.URL)
	rec2 := httptest.NewRecorder()
	st.SendNewResult(rec2)
	if rec2.Code != 200 || !st.LastRunSuccessful() {
		t.Fatalf("expected success, code=%d", rec2.Code)
	}
	if rec2.Body.String() != "OK" {
		t.Fatalf("body = %q", rec2.Body.String())
	}
	// and the cached last result now says OK
	rec3 := httptest.NewRecorder()
	st.SendLastResult(rec3)
	if rec3.Code != 200 || rec3.Body.String() != "OK" {
		t.Fatalf("SendLastResult after success = %d %q", rec3.Code, rec3.Body.String())
	}
}

func TestTriggerRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(validEnvelopeResponse(t)))
	}))
	defer srv.Close()
	st := newTestSmoke(t, srv.URL)
	var wg sync.WaitGroup
	wg.Add(1)
	st.TriggerRun(func(err error) {
		defer wg.Done()
		if err != nil {
			t.Fatalf("callback error: %v", err)
		}
	})
	wg.Wait()
	if !st.LastRunSuccessful() {
		t.Fatal("expected last run successful after TriggerRun")
	}
}

func TestTriggerRunFailure(t *testing.T) {
	st := newTestSmoke(t, "http://127.0.0.1:1")
	var wg sync.WaitGroup
	wg.Add(1)
	st.TriggerRun(func(err error) {
		defer wg.Done()
		if err == nil {
			t.Fatal("expected error")
		}
	})
	wg.Wait()
	if st.LastRunSuccessful() {
		t.Fatal("expected last run failed")
	}
}

func TestDefaultURLFor(t *testing.T) {
	url := DefaultURLFor("127.0.0.1", 3013)
	if !strings.HasPrefix(url, "http://127.0.0.1:3013/project/smoketest-") {
		t.Fatalf("url = %s", url)
	}
}

func TestNew(t *testing.T) {
	cfg := newTestConfig()
	st := New(cfg)
	if !strings.Contains(st.compileURL, "/project/smoketest-") {
		t.Fatalf("url = %s", st.compileURL)
	}
	if st.LastRunSuccessful() || st.LastError() == nil {
		t.Fatal("New() must start with the pending error")
	}
}

func TestSnippet(t *testing.T) {
	if got := snippet([]byte("abc")); got != "abc" {
		t.Fatalf("snippet = %q", got)
	}
	long := strings.Repeat("x", 300)
	if got := snippet([]byte(long)); len(got) != 200 {
		t.Fatalf("snippet len = %d", len(got))
	}
}
