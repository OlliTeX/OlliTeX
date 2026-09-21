package ologger

import "testing"

func TestRealLogger_ControlMethods(t *testing.T) {
	var w nopWriter
	b := NewJSONBunyan(&w)
	l := b.CreateLogger(&LoggerConfig{Name: "c", Streams: []StreamConfig{{Level: "debug"}}}).(*jsonLogger)
	l.Level("info")
	if l.level != "info" {
		t.Fatalf("expected Level to set the minimum level")
	}
	before := len(l.streams)
	l.AddStream(StreamConfig{Level: "trace"})
	if len(l.streams) != before+1 {
		t.Fatalf("expected AddStream to append a stream")
	}
	l.Serializers()["custom"] = func(v any) any { return v }
	if _, ok := l.Serializers()["custom"]; !ok {
		t.Fatalf("expected Serializers() map to be usable")
	}
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestGetRawReqInput_NonReq(t *testing.T) {
	if got := getRawReqInput("not-a-req"); got != nil {
		t.Fatalf("expected nil for non-Req, got %#v", got)
	}
	req := Req{Params: map[string]any{"a": 1}}
	if got := getRawReqInput(req); got["a"] != 1 {
		t.Fatalf("expected params surfaced, got %#v", got)
	}
}

func TestGetRawReqInput_LockdownRawNil(t *testing.T) {
	req := Req{LockdownInstalled: true} // no RawParams
	if got := getRawReqInput(req); got != nil {
		t.Fatalf("expected nil when lockdown has no raw params, got %#v", got)
	}
}

func TestReqHeader_CaseInsensitiveAndLengths(t *testing.T) {
	req := Req{Headers: map[string]string{"Content-Length": "9", "X-Foo": "y"}}
	if req.Header("content-length") != "9" {
		t.Fatalf("expected case-insensitive header lookup")
	}
	if req.Header("x-foo") != "y" {
		t.Fatalf("expected case-insensitive header lookup 2")
	}
	if req.Header("content-l") != "" {
		t.Fatalf("expected empty for a different-length header")
	}
	if req.Header("content-L") != "" {
		t.Fatalf("expected empty for a different header")
	}
}

func TestToInt_ToFloat_MoreBranches(t *testing.T) {
	if toInt(int64(5)) != 5 {
		t.Fatalf("toInt(int64) = %d", toInt(int64(5)))
	}
	if f, ok := toFloat(int64(5)); !ok || f != 5 {
		t.Fatalf("toFloat(int64) = %v,%v", f, ok)
	}
	if toInt(nil) != 0 {
		t.Fatalf("toInt(nil) = %d", toInt(nil))
	}
	if _, ok := toFloat(nil); ok {
		t.Fatalf("toFloat(nil) should not parse")
	}
}

func TestNew_DefaultsApplied(t *testing.T) {
	opts := Options{}
	m := New(opts)
	if m.opts.Bunyan == nil {
		t.Fatalf("expected a default Bunyan factory")
	}
	if m.opts.Env == nil || m.opts.Now == nil || m.opts.Exit == nil {
		t.Fatalf("expected default Env/Now/Exit seams")
	}
	if m.opts.Checkers.File == nil || m.opts.Checkers.GCERequired == nil {
		t.Fatalf("expected default log-level checkers")
	}
	if m.opts.LevelCheckInterval == 0 {
		t.Fatalf("expected a default level-check interval")
	}
	// Deterministically non-production so the real file checker (goroutine)
	// is never started.
	t.Setenv("NODE_ENV", "test")
	if m.Initialize("default").logger == nil {
		t.Fatalf("expected a logger after initialize")
	}
	if m.ringBuffer != nil {
		t.Fatalf("expected no ring buffer when LOG_RING_BUFFER_SIZE is unset")
	}
}

func TestEqualFold(t *testing.T) {
	if !equalFold("AbC", "abc") {
		t.Fatalf("expected equalFold to match")
	}
	if equalFold("ab", "abc") {
		t.Fatalf("expected different-length strings to differ")
	}
	if equalFold("ab", "cd") {
		t.Fatalf("expected different strings to differ")
	}
}
