package ologger

import "testing"

// stubChecker records Start/Stop for the production-level checker assertions.
type stubChecker struct {
	started bool
	stopped bool
}

func (c *stubChecker) Start()               { c.started = true }
func (c *stubChecker) Stop()                { c.stopped = true }
func (c *stubChecker) CheckLogLevel() error { return nil }

type testHarness struct {
	mgr    *LoggingManager
	bunyan *stubBunyan
	logger *stubLogger

	fileChecker *stubChecker
	gceChecker  *stubChecker

	// setLogger registrations captured from Options.
	fetchWarn     func(info map[string]any, message string)
	valWarn       func(info map[string]any, message string)
	fetchSetCalls int
	valSetCalls   int

	exits            []int
	warnRegistered   int
	warnUnregistered int
}

func newHarness(t *testing.T, env map[string]string) *testHarness {
	t.Helper()
	h := &testHarness{}
	h.logger = newStubLogger("test")
	h.bunyan = newStubBunyan(h.logger)
	h.fileChecker = &stubChecker{}
	h.gceChecker = &stubChecker{}

	h.mgr = New(Options{
		Bunyan: h.bunyan,
		Env:    func(k string) string { return env[k] },
		Now:    func() int64 { return 0 },
		Checkers: CheckerFactories{
			File:        func(Logger, string) Checker { return h.fileChecker },
			GCERequired: func(Logger, string) Checker { return h.gceChecker },
		},
		Exit:              func(code int) { h.exits = append(h.exits, code) },
		RegisterWarning:   func(func(any)) { h.warnRegistered++ },
		UnregisterWarning: func(func(any)) { h.warnUnregistered++ },
		FetchUtilsSetLogger: func(warn func(info map[string]any, message string)) {
			h.fetchWarn = warn
			h.fetchSetCalls++
		},
		ValidationToolsSetLogger: func(warn func(info map[string]any, message string)) {
			h.valWarn = warn
			h.valSetCalls++
		},
	})
	return h
}

func (h *testHarness) firstStreamsLevel() string {
	cfg, ok := h.bunyan.firstConfig()
	if !ok || len(cfg.Streams) == 0 {
		return "<none>"
	}
	return cfg.Streams[0].Level
}

func TestInitialize_NotProduction(t *testing.T) {
	h := newHarness(t, map[string]string{})
	h.mgr.Initialize("test")
	if got := h.firstStreamsLevel(); got != "debug" {
		t.Fatalf("expected default level debug, got %q", got)
	}
	if h.mgr.logLevelChecker != nil {
		t.Fatalf("expected no log level checker in non-production")
	}
}

func TestInitialize_Production(t *testing.T) {
	h := newHarness(t, map[string]string{"NODE_ENV": "production"})
	h.mgr.Initialize("test")
	if got := h.firstStreamsLevel(); got != "info" {
		t.Fatalf("expected production default level info, got %q", got)
	}
	if h.mgr.logLevelChecker != h.fileChecker {
		t.Fatalf("expected the file log level checker, got %v", h.mgr.logLevelChecker)
	}
	if !h.fileChecker.started {
		t.Fatalf("expected file checker to be started")
	}
}

func TestInitialize_LogLevelEnv(t *testing.T) {
	h := newHarness(t, map[string]string{"LOG_LEVEL": "trace"})
	h.mgr.Initialize("test")
	if got := h.firstStreamsLevel(); got != "trace" {
		t.Fatalf("expected custom level trace, got %q", got)
	}
}

func TestInitialize_GCEChecker(t *testing.T) {
	h := newHarness(t, map[string]string{"NODE_ENV": "production", "LOG_LEVEL_SOURCE": "gce_metadata"})
	h.mgr.Initialize("test")
	if h.mgr.logLevelChecker != h.gceChecker {
		t.Fatalf("expected the GCE log level checker, got %v", h.mgr.logLevelChecker)
	}
	if !h.gceChecker.started {
		t.Fatalf("expected GCE checker to be started")
	}
}

func TestInitialize_CheckerNone(t *testing.T) {
	h := newHarness(t, map[string]string{"NODE_ENV": "production", "LOG_LEVEL_SOURCE": "none"})
	h.mgr.Initialize("test")
	if h.mgr.logLevelChecker != nil {
		t.Fatalf("expected no checker with source none")
	}
}

func TestInitialize_LoggingFormatGKE(t *testing.T) {
	h := newHarness(t, map[string]string{"LOGGING_FORMAT": "gke"})
	h.mgr.Initialize("app")
	cfg, _ := h.bunyan.firstConfig()
	if !cfg.Streams[0].GKE {
		t.Fatalf("expected the gke output stream")
	}
	if cfg.Streams[0].Level != "debug" {
		t.Fatalf("expected gke stream level debug, got %q", cfg.Streams[0].Level)
	}
}

func TestInitialize_LoggingFormatGCE(t *testing.T) {
	h := newHarness(t, map[string]string{"LOGGING_FORMAT": "gce"})
	h.mgr.Initialize("app")
	cfg, _ := h.bunyan.firstConfig()
	if !cfg.Streams[0].GCE {
		t.Fatalf("expected the gce output stream")
	}
	if cfg.Streams[0].LogName != "app" || cfg.Streams[0].ServiceContext != "app" {
		t.Fatalf("expected gce stream to carry the logger name, got %+v", cfg.Streams[0])
	}
}

func TestAddSerializer_RegistersCurrent(t *testing.T) {
	h := newHarness(t, map[string]string{})
	h.mgr.Initialize("test")
	serializer := func(v any) any { return "custom" }
	h.mgr.AddSerializer("custom", serializer)
	got := h.logger.Serializers()["custom"]
	if got == nil || got("x") != "custom" {
		t.Fatalf("expected serializer registered on the current logger")
	}
}

func TestAddSerializer_PersistsAcrossInitialize(t *testing.T) {
	h := newHarness(t, map[string]string{})
	h.mgr.AddSerializer("custom", func(v any) any { return "custom" })
	h.bunyan.resetCreateLogger()
	h.mgr.Initialize("test")
	cfg, _ := h.bunyan.firstConfig()
	got := cfg.Serializers["custom"]
	if got == nil || got("x") != "custom" {
		t.Fatalf("expected custom serializer to survive a later initialize")
	}
}

func TestAddSerializer_KeepsBaseSerializers(t *testing.T) {
	h := newHarness(t, map[string]string{})
	h.mgr.AddSerializer("custom", func(v any) any { return "custom" })
	h.bunyan.resetCreateLogger()
	h.mgr.Initialize("test")
	cfg, _ := h.bunyan.firstConfig()
	for _, k := range []string{"err", "error", "req", "res"} {
		if _, ok := cfg.Serializers[k]; !ok {
			t.Fatalf("expected base serializer %q in the createLogger config", k)
		}
	}
}

func TestInitialize_RegistersWarnWithFetchUtils(t *testing.T) {
	h := newHarness(t, map[string]string{})
	h.mgr.Initialize("test")
	if h.fetchSetCalls != 1 {
		t.Fatalf("expected fetch-utils setLogger to be called once, got %d", h.fetchSetCalls)
	}
	if h.fetchWarn == nil {
		t.Fatalf("expected a warn callback from fetch-utils setLogger")
	}
	h.fetchWarn(map[string]any{"k": "v"}, "hello")
	c, ok := callList(h.logger.warnCalls).last()
	if !ok {
		t.Fatalf("expected the registered warn to route to the manager Warn")
	}
	if !deepEqualAny(c.attributes, map[string]any{"k": "v"}) || c.message != "hello" {
		t.Fatalf("expected (info,msg) forwarded to Warn, got %#v %q", c.attributes, c.message)
	}
}

func TestInitialize_RegistersWarnWithValidationTools(t *testing.T) {
	h := newHarness(t, map[string]string{})
	h.mgr.Initialize("test")
	if h.valSetCalls != 1 || h.valWarn == nil {
		t.Fatalf("expected validation-tools setLogger to be wired")
	}
	h.valWarn(map[string]any{"a": 1}, "vt")
	c, ok := callList(h.logger.warnCalls).last()
	if !ok || c.message != "vt" {
		t.Fatalf("expected validation-tools warn to route to the manager Warn")
	}
}

func TestBunyanLogging_ForwardFirstArg(t *testing.T) {
	logArgs := []any{map[string]any{"foo": "bar"}, "foo", "bar"}
	h := newHarness(t, map[string]string{})
	h.mgr.Initialize("test")

	h.mgr.Debug(logArgs, nil)
	c, _ := callList(h.logger.debugCalls).last()
	if !deepEqualAny(c.attributes, logArgs) {
		t.Fatalf("debug: expected first arg forwarded verbatim, got %#v", c.attributes)
	}

	h.mgr.Info(logArgs, nil)
	c, _ = callList(h.logger.infoCalls).last()
	if !deepEqualAny(c.attributes, logArgs) {
		t.Fatalf("info: expected first arg forwarded verbatim, got %#v", c.attributes)
	}

	h.mgr.Error(logArgs, nil)
	c, _ = callList(h.logger.errorCalls).last()
	if !deepEqualAny(c.attributes, logArgs) {
		t.Fatalf("error: expected first arg forwarded verbatim, got %#v", c.attributes)
	}

	h.mgr.Warn(logArgs, nil)
	c, _ = callList(h.logger.warnCalls).last()
	if !deepEqualAny(c.attributes, logArgs) {
		t.Fatalf("warn: expected first arg forwarded verbatim, got %#v", c.attributes)
	}

	h.mgr.Fatal(logArgs, nil)
	c, _ = callList(h.logger.fatalCalls).last()
	if !deepEqualAny(c.attributes, logArgs) {
		t.Fatalf("fatal: expected first arg forwarded verbatim, got %#v", c.attributes)
	}

	h.mgr.Err(logArgs, nil)
	c, _ = callList(h.logger.errorCalls).last()
	if !deepEqualAny(c.attributes, logArgs) {
		t.Fatalf("err: expected to delegate to error, got %#v", c.attributes)
	}
}

func TestRingBuffer_Positive(t *testing.T) {
	h := newHarness(t, map[string]string{"LOG_RING_BUFFER_SIZE": "20"})
	h.mgr.Initialize("test")
	mock := []Entry{
		{"msg": "log 1"},
		{"msg": "log 2"},
		{"level": 50, "msg": "error"},
	}
	h.mgr.ringBuffer.Records = mock
	h.mgr.Error(map[string]any{}, "error")
	c, ok := callList(h.logger.errorCalls).last()
	if !ok {
		t.Fatalf("expected an error call")
	}
	attrs, ok := c.attributes.(map[string]any)
	if !ok {
		t.Fatalf("expected a map attributes, got %#v", c.attributes)
	}
	want := []Entry{{"msg": "log 1"}, {"msg": "log 2"}}
	if !deepEqualAny(attrs["logBuffer"], want) {
		t.Fatalf("expected logBuffer filtered to non-error records, got %#v", attrs["logBuffer"])
	}
}

func TestRingBuffer_Zero(t *testing.T) {
	h := newHarness(t, map[string]string{"LOG_RING_BUFFER_SIZE": "0"})
	h.mgr.Initialize("test")
	if h.mgr.ringBuffer != nil {
		t.Fatalf("expected nil ring buffer when size is 0")
	}
	h.mgr.Error(map[string]any{}, "error")
	c, _ := callList(h.logger.errorCalls).last()
	attrs, ok := c.attributes.(map[string]any)
	if !ok {
		t.Fatalf("expected map attributes")
	}
	if _, has := attrs["logBuffer"]; has {
		t.Fatalf("expected no logBuffer when ring buffer is disabled")
	}
}

func TestRemoveWarningHandler(t *testing.T) {
	h := newHarness(t, map[string]string{})
	h.mgr.Initialize("test")
	h.mgr.RegisterWarning()
	if h.warnRegistered != 1 {
		t.Fatalf("expected warning handler registered once")
	}
	h.mgr.RemoveWarningHandler()
	if h.warnUnregistered != 1 {
		t.Fatalf("expected warning handler unregistered")
	}
}

func TestWarningHandler_EmitsWarn(t *testing.T) {
	h := newHarness(t, map[string]string{})
	m := h.mgr
	m.Initialize("test")
	m.RegisterWarning()
	hdl := m.warningHandler
	if hdl == nil {
		t.Fatalf("expected a warning handler to be installed")
	}
	hdl(boomErr{})
	c, ok := callList(h.logger.warnCalls).last()
	if !ok {
		t.Fatalf("expected a warn call from the warning handler")
	}
	attrs, _ := c.attributes.(map[string]any)
	if _, ok := attrs["err"].(boomErr); !ok {
		t.Fatalf("expected {err} of type boomErr, got %#v", c.attributes)
	}
	if c.message != "Warning details" {
		t.Fatalf("expected message 'Warning details', got %q", c.message)
	}
}

type boomErr struct{}

func (boomErr) Error() string { return "boom" }

func TestExit_DelaysThenExits(t *testing.T) {
	h := newHarness(t, map[string]string{})
	h.mgr.Initialize("test")
	h.mgr.Exit(3)
	if len(h.exits) != 1 || h.exits[0] != 3 {
		t.Fatalf("expected process.exit(3), got %v", h.exits)
	}
}
