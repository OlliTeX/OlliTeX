package ometrics

import "testing"

// httpLogger records calls driven by RequestLogger (Node: distinct debug/info/warn).
type httpLogger struct {
	calls []httpLogCall
}

type httpLogCall struct {
	level string
	info  map[string]any
	msg   string
	args  []any
}

func (h *httpLogger) Log(level string, info map[string]any, msg string, args ...any) {
	h.calls = append(h.calls, httpLogCall{level: level, info: info, msg: msg, args: args})
}

func newHttpFixture() (*Request, *Responder, *httpLogger) {
	req := &Request{
		Method:  "POST",
		URL:     "/project/1234/cleanup",
		Headers: map[string]string{"content-length": "123"},
		Route:   &RoutePath{Path: "/project/:id/cleanup"},
	}
	res := &Responder{
		Status: nil,
		End:    func(...any) {},
	}
	logger := &httpLogger{}
	return req, res, logger
}

func runMonitor(t *testing.T, rec *stubRecorder, level string,
	mutate func(rl *RequestLogger, req *Request, res *Responder)) *httpLogger {
	t.Helper()
	req, res, logger := newHttpFixture()

	oldRec := recorder
	oldNow := NowMS
	rec.reset()
	recorder = rec
	NowMS = func() int64 { return 0 }
	defer func() {
		recorder = oldRec
		NowMS = oldNow
	}()

	middleware := HTTPMonitor(logger, level)
	nextCalled := false
	middleware(req, res, func() { nextCalled = true })
	if !nextCalled {
		t.Fatal("next() was not called")
	}

	if mutate != nil {
		mutate(req.Logger, req, res)
	}

	// tick 500ms, then end
	NowMS = func() int64 { return 500 }
	res.End("data")
	return logger
}

func assertHttpMetrics(t *testing.T, rec *stubRecorder) {
	t.Helper()
	if len(rec.timings) != 1 {
		t.Fatalf("timings count = %d, want 1", len(rec.timings))
	}
	tc := rec.timings[0]
	if tc.key != "http_request" {
		t.Fatalf("timing key = %q, want http_request", tc.key)
	}
	if tc.value != 500 {
		t.Fatalf("timing value = %v, want 500", tc.value)
	}
	assertHttpLabels(t, tc.labels)

	if len(rec.sums) != 1 {
		t.Fatalf("summaries count = %d, want 1", len(rec.sums))
	}
	sc := rec.sums[0]
	if sc.key != "http_request_size_bytes" {
		t.Fatalf("summary key = %q, want http_request_size_bytes", sc.key)
	}
	if sc.value != 123 {
		t.Fatalf("summary value = %v, want 123", sc.value)
	}
	assertHttpLabels(t, sc.labels)
}

func assertHttpLabels(t *testing.T, labels map[string]any) {
	t.Helper()
	if labels["method"] != "POST" {
		t.Fatalf("labels.method = %v, want POST", labels["method"])
	}
	if labels["path"] != "project_id_cleanup" {
		t.Fatalf("labels.path = %v, want project_id_cleanup", labels["path"])
	}
	if labels["status_code"] != nil {
		t.Fatalf("labels.status_code = %v, want nil", labels["status_code"])
	}
}

func TestHTTPMonitor(t *testing.T) {
	rec := &stubRecorder{}

	t.Run("logs the request at the DEBUG level", func(t *testing.T) {
		logger := runMonitor(t, rec, "", nil)
		assertHttpMetrics(t, rec)
		if len(logger.calls) != 1 {
			t.Fatalf("logger calls = %d, want 1", len(logger.calls))
		}
		call := logger.calls[0]
		if call.level != "debug" {
			t.Fatalf("level = %q, want debug", call.level)
		}
		assertLogInfo(t, call, false)
	})

	t.Run("calls the original res.end()", func(t *testing.T) {
		rec := &stubRecorder{}
		req, _, _ := newHttpFixture()
		var origArgs []any
		var origCalled bool
		res := &Responder{Status: nil, End: func(args ...any) { origCalled = true; origArgs = args }}

		oldRec := recorder
		recorder = rec
		defer func() { recorder = oldRec }()
		middleware := HTTPMonitor(&httpLogger{}, "")
		middleware(req, res, func() {})
		res.End("data")
		if !origCalled {
			t.Fatal("original res.end was not called")
		}
		if len(origArgs) != 1 || origArgs[0] != "data" {
			t.Fatalf("origArgs = %v, want [data]", origArgs)
		}
	})

	t.Run("when logging is disabled", func(t *testing.T) {
		rec := &stubRecorder{}
		logger := runMonitor(t, rec, "", func(rl *RequestLogger, _ *Request, _ *Responder) {
			rl.Disable()
		})
		assertHttpMetrics(t, rec)
		if len(logger.calls) != 0 {
			t.Fatalf("logger calls = %d, want 0", len(logger.calls))
		}
	})

	t.Run("with custom log fields", func(t *testing.T) {
		rec := &stubRecorder{}
		logger := runMonitor(t, rec, "", func(rl *RequestLogger, _ *Request, _ *Responder) {
			rl.AddFields(map[string]any{"a": 1, "b": 2})
		})
		if len(logger.calls) != 1 {
			t.Fatalf("logger calls = %d, want 1", len(logger.calls))
		}
		assertLogInfo(t, logger.calls[0], true)
	})

	t.Run("when setting the log level", func(t *testing.T) {
		rec := &stubRecorder{}
		logger := runMonitor(t, rec, "", func(rl *RequestLogger, _ *Request, _ *Responder) {
			rl.SetLevel("warn")
		})
		if len(logger.calls) != 1 {
			t.Fatalf("logger calls = %d, want 1", len(logger.calls))
		}
		if logger.calls[0].level != "warn" {
			t.Fatalf("level = %q, want warn", logger.calls[0].level)
		}
	})

	t.Run("with a different default log level", func(t *testing.T) {
		rec := &stubRecorder{}
		logger := runMonitor(t, rec, "info", nil)
		if len(logger.calls) != 1 {
			t.Fatalf("logger calls = %d, want 1", len(logger.calls))
		}
		if logger.calls[0].level != "info" {
			t.Fatalf("level = %q, want info", logger.calls[0].level)
		}
	})
}

func assertLogInfo(t *testing.T, call httpLogCall, custom bool) {
	t.Helper()
	if call.msg != "%s %s" {
		t.Fatalf("msg = %q, want %%s %%s", call.msg)
	}
	if len(call.args) != 2 || call.args[0] != "POST" || call.args[1] != "/project/1234/cleanup" {
		t.Fatalf("args = %v, want [POST /project/1234/cleanup]", call.args)
	}
	info := call.info
	if info["responseTimeMs"] != float64(500) {
		t.Fatalf("responseTimeMs = %v, want 500", info["responseTimeMs"])
	}
	if info["req"] == nil || info["res"] == nil {
		t.Fatal("info must contain req and res")
	}
	if custom {
		if info["a"] != 1 || info["b"] != 2 {
			t.Fatalf("custom fields = a:%v b:%v, want 1/2", info["a"], info["b"])
		}
	}
}
