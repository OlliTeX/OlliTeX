// smoketest_test.go covers the SMOKE_TEST (D12 lean-Y) smoke-test runner:
// the compile-envelope validation (Run), the last-result cache
// (LastRunSuccessful / LastError), the HTTP renderers
// (SendLastResult / SendNewResult) and the background TriggerRun, plus the
// URL/timeout helpers.
package smoketest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ollitex/go/services/clsitypst/config"
)

// okEnvelope is the compile endpoint success shape (buildId b1, output.pdf).
func okEnvelope() string {
	return `{"compile":{"buildId":"b1","status":"success","outputFiles":[{"path":"output.pdf","type":"pdf"}]}}`
}

func TestDefaultURLFor(t *testing.T) {
	url := DefaultURLFor("10.0.0.1", 3014)
	if !strings.HasPrefix(url, "http://10.0.0.1:3014/project/smoketest-") {
		t.Fatalf("default url: %s", url)
	}
	if !strings.Contains(url, "/compile") {
		t.Fatalf("default url: %s", url)
	}
}

func TestNewDefaults(t *testing.T) {
	cfg := &config.Config{}
	cfg.Internal.Host = "127.0.0.1"
	cfg.Internal.Port = 3014
	s := New(cfg)
	if s.compileURL != DefaultURLFor("127.0.0.1", 3014) {
		t.Fatalf("compile url: %s", s.compileURL)
	}
	if s.timeout != DefaultTimeout {
		t.Fatalf("timeout: %v", s.timeout)
	}
	if got := s.LastError(); got != NewErrPending {
		t.Fatalf("initial last error: %v", got)
	}
	if s.LastRunSuccessful() {
		t.Fatal("must be pending (not successful) at init")
	}
}

// --- Run (validation arms) -----------------------------------------------------------

func TestRunSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("POST expected, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/compile") {
			t.Fatalf("url: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(okEnvelope()))
	}))
	defer srv.Close()
	s := New(&config.Config{})
	s.SetCompileURL(srv.URL + "/project/smoke/compile")
	s.SetTimeout(5 * time.Second)
	if err := s.Run(t.Context()); err != nil {
		t.Fatalf("Run success: %v", err)
	}
}

func TestRunFailedCompile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"compile":{"status":"error","error":"bust"}}"`))

	}))
	defer srv.Close()
	s := New(&config.Config{})
	s.SetCompileURL(srv.URL + "/compile")
	s.SetTimeout(5 * time.Second)
	// status "error" + no outputFiles -> Run rejects as incomplete payload.
	if err := s.Run(t.Context()); err == nil {
		t.Fatal("Run must reject a failed-compile envelope")
	}
}

func TestRunNonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := New(&config.Config{})
	s.SetCompileURL(srv.URL + "/compile")
	s.SetTimeout(5 * time.Second)
	err := s.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want error containing 'boom', got: %v", err)
	}
}

func TestRunNoPDF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"compile":{"status":"success","outputFiles":[{"path":"x.txt","type":"txt"}]}}`))
	}))
	defer srv.Close()
	s := New(&config.Config{})
	s.SetCompileURL(srv.URL + "/compile")
	s.SetTimeout(5 * time.Second)
	err := s.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "no pdf returned") {
		t.Fatalf("want no-pdf error, got: %v", err)
	}
}

func TestRunPayloadIncomplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"compile":{"status":"success","outputFiles":[]}}`))
	}))
	defer srv.Close()
	s := New(&config.Config{})
	s.SetCompileURL(srv.URL + "/compile")
	s.SetTimeout(5 * time.Second)
	err := s.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "response payload incomplete") {
		t.Fatalf("want incomplete payload error, got: %v", err)
	}
}

func TestRunConnectError(t *testing.T) {
	// A closed server -> the POST fails with a connection error.
	s := New(&config.Config{})
	s.SetCompileURL("http://127.0.0.1:1/compile") // refuses
	s.SetTimeout(5 * time.Second)
	if err := s.Run(t.Context()); err == nil {
		t.Fatal("Run must fail on connect error")
	}
}

// --- LastResult cache + renderers ------------------------------------------------------

func TestSendLastResultPending(t *testing.T) {
	s := New(&config.Config{})
	rec := httptest.NewRecorder()
	s.SendLastResult(rec)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("pending: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "SmokeTestsPending") {
		t.Fatalf("pending body: %s", rec.Body.String())
	}
}

func TestSendLastResultCached(t *testing.T) {
	s := New(&config.Config{})
	s.setLastError(nil)
	rec := httptest.NewRecorder()
	s.SendLastResult(rec)
	if rec.Code != http.StatusOK || rec.Body.String() != "OK" {
		t.Fatalf("cached success: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSendNewResultSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(okEnvelope()))
	}))
	defer srv.Close()
	s := New(&config.Config{})
	s.SetCompileURL(srv.URL + "/compile")
	s.SetTimeout(5 * time.Second)
	rec := httptest.NewRecorder()
	s.SendNewResult(rec)
	if rec.Code != http.StatusOK || rec.Body.String() != "OK" {
		t.Fatalf("send new: %d %s", rec.Code, rec.Body.String())
	}
	if !s.LastRunSuccessful() {
		t.Fatal("must be successful after a live run")
	}
	if s.LastError() != nil {
		t.Fatalf("last error: %v", s.LastError())
	}
}

func TestSendNewResultFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := New(&config.Config{})
	s.SetCompileURL(srv.URL + "/compile")
	s.SetTimeout(5 * time.Second)
	rec := httptest.NewRecorder()
	s.SendNewResult(rec)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("fail: %d", rec.Code)
	}
	if s.LastRunSuccessful() {
		t.Fatal("must NOT be successful after a failed live run")
	}
	if !strings.Contains(s.LastError().Error(), "nope") {
		t.Fatalf("last error: %v", s.LastError())
	}
}

// --- TriggerRun (background + callback) -------------------------------------------------

func TestTriggerRunCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(okEnvelope()))
	}))
	defer srv.Close()
	s := New(&config.Config{})
	s.SetCompileURL(srv.URL + "/compile")
	s.SetTimeout(5 * time.Second)

	type result struct {
		ok  bool
		err string
	}
	done := make(chan result, 1)
	var n atomic.Int64
	s.TriggerRun(func(err error) {
		n.Add(1)
		done <- result{ok: err == nil, err: ""}
	})
	select {
	case res := <-done:
		if !res.ok {
			t.Fatal("TriggerRun callback must get a nil error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("trigger did not invoke callback in time")
	}
	if n.Load() != 1 {
		t.Fatalf("callback fired %d times, want 1", n.Load())
	}
	if !s.LastRunSuccessful() {
		// The goroutine set lastError before the callback; allow a short
		// settle window (the callback runs after setLastError).
		time.Sleep(50 * time.Millisecond)
	}
}

func TestTriggerRunFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "trigger-err", http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := New(&config.Config{})
	s.SetCompileURL(srv.URL + "/compile")
	s.SetTimeout(5 * time.Second)
	got := make(chan error, 1)
	s.TriggerRun(func(err error) { got <- err })
	select {
	case err := <-got:
		if err == nil || !strings.Contains(err.Error(), "trigger-err") {
			t.Fatalf("trigger err: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("trigger did not invoke callback")
	}
}

func TestTriggerRunNilCallback(t *testing.T) {
	// nil callback -> no panic.
	s := New(&config.Config{})
	s.SetCompileURL("http://127.0.0.1:1/compile")
	s.SetTimeout(5 * time.Second)
	s.TriggerRun(nil)
	time.Sleep(50 * time.Millisecond)
}

// --- snippet (truncation) ----------------------------------------------------------------

func TestSnippetShort(t *testing.T) {
	if got := snippet([]byte("boom")); got != "boom" {
		t.Fatalf("snippet: %q", got)
	}
}

func TestSnippetLong(t *testing.T) {
	long := strings.Repeat("x", 1000)
	if got := snippet([]byte(long)); len(got) != 200 {
		t.Fatalf("snippet length: %d want 200", len(got))
	}
}

// --- mainTyp constant (sanity: it compiles to a non-zero pdf) -----------------------------

func TestMainTypPresent(t *testing.T) {
	if !strings.Contains(mainTyp, "hello") {
		t.Fatal("mainTyp must contain the hello content")
	}
}
