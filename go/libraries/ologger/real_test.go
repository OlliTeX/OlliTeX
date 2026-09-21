package ologger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRealLogger_EmitsJSONLine(t *testing.T) {
	var buf bytes.Buffer
	b := NewJSONBunyan(&buf)
	l := b.CreateLogger(&LoggerConfig{
		Name:        "testlog",
		Serializers: map[string]Serializer{"err": ErrSerializer},
		Streams:     []StreamConfig{{Level: "debug"}},
	}).(*jsonLogger)

	l.Debug(map[string]any{"note": "hi"}, "hello")
	out := strings.TrimRight(buf.String(), "\n")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("expected a JSON line, got %q: %v", out, err)
	}
	if decoded["msg"] != "hello" {
		t.Fatalf("expected msg hello, got %v", decoded["msg"])
	}
	if decoded["name"] != "testlog" {
		t.Fatalf("expected name testlog, got %v", decoded["name"])
	}
	if decoded["note"] != "hi" {
		t.Fatalf("expected attribute note, got %v", decoded)
	}
}

func TestRealLogger_AppliesSerializer(t *testing.T) {
	var buf bytes.Buffer
	b := NewJSONBunyan(&buf)
	l := b.CreateLogger(&LoggerConfig{
		Name:        "s",
		Serializers: map[string]Serializer{"res": ResSerializer},
		Streams:     []StreamConfig{{Level: "debug"}},
	}).(*jsonLogger)
	l.Info(map[string]any{"res": map[string]any{"hidden": true}}, "x")
	if strings.Contains(buf.String(), `"hidden"`) {
		t.Fatalf("expected the res serializer to strip the payload, got %q", buf.String())
	}
}

func TestRealLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	b := NewJSONBunyan(&buf)
	l := b.CreateLogger(&LoggerConfig{
		Name:    "f",
		Streams: []StreamConfig{{Level: "info"}},
	}).(*jsonLogger)
	// debug (10) < info (30) -> suppressed; info (30) -> emitted.
	l.Debug(map[string]any{}, "suppressed")
	if buf.Len() != 0 {
		t.Fatalf("expected debug suppressed at info level, got %q", buf.String())
	}
	l.Info(map[string]any{}, "kept")
	if !strings.Contains(buf.String(), "kept") {
		t.Fatalf("expected info emitted at info level, got %q", buf.String())
	}
}

func TestRealLogger_AllLevelsAndName(t *testing.T) {
	var buf bytes.Buffer
	b := NewJSONBunyan(&buf)
	l := b.CreateLogger(&LoggerConfig{Name: "n", Streams: []StreamConfig{{Level: "trace"}}}).(*jsonLogger)
	l.Debug(map[string]any{}, "d")
	l.Info(map[string]any{}, "i")
	l.Warn(map[string]any{}, "w")
	l.Error(map[string]any{}, "e")
	l.Fatal(map[string]any{}, "f")
	if l.Name() != "n" {
		t.Fatalf("expected name n, got %q", l.Name())
	}
	if n := strings.Count(buf.String(), "\n"); n != 5 {
		t.Fatalf("expected 5 lines, got %d (%q)", n, buf.String())
	}
}

func TestRealLogger_MsgStringVariants(t *testing.T) {
	if msgString(nil) != "" {
		t.Fatalf("expected nil message to be empty")
	}
	if msgString("hi") != "hi" {
		t.Fatalf("expected string passthrough")
	}
	if msgString(42) != "42" {
		t.Fatalf("expected int rendered, got %q", msgString(42))
	}
}

func TestNewJSONBunyan_DefaultAndRingBuffer(t *testing.T) {
	b := NewJSONBunyan(nil) // nil -> stdout default (no write)
	rb := b.RingBuffer(12)
	if rb.Limit != 12 {
		t.Fatalf("expected ring buffer limit 12, got %d", rb.Limit)
	}
}

func TestWithStreams_Overrides(t *testing.T) {
	h := newHarness(t, map[string]string{})
	streams := []StreamConfig{{Level: "trace"}, {Level: "debug"}}
	h.mgr.Initialize("test", WithStreams(streams))
	cfg, _ := h.bunyan.firstConfig()
	if len(cfg.Streams) != 2 || cfg.Streams[0].Level != "trace" || cfg.Streams[1].Level != "debug" {
		t.Fatalf("expected the WithStreams override, got %+v", cfg.Streams)
	}
}

func TestSetupLogLevelChecker_UnrecognisedSource(t *testing.T) {
	h := newHarness(t, map[string]string{"NODE_ENV": "production", "LOG_LEVEL_SOURCE": "bogus"})
	h.mgr.Initialize("test")
	if h.mgr.logLevelChecker != nil {
		t.Fatalf("expected no checker for an unrecognised source")
	}
}

func TestRemoveWarningHandler_NilNoop(t *testing.T) {
	h := newHarness(t, map[string]string{})
	h.mgr.Initialize("test")
	// No handler registered yet: remove is a no-op.
	h.mgr.RemoveWarningHandler()
	if h.warnUnregistered != 0 {
		t.Fatalf("expected no unregistration when no handler is installed")
	}
}

func TestParseRingBufferSize(t *testing.T) {
	cases := map[string]int{
		"":       0,
		"0":      0,
		"20":     20,
		"abc":    0,
		"12x9":   12,
		"-5":     -5,
		"0007":   7,
		"100000": 100000,
	}
	for in, want := range cases {
		if got := parseRingBufferSize(in); got != want {
			t.Fatalf("parseRingBufferSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestNameFromLevel_All(t *testing.T) {
	cases := map[int]string{
		10: "trace", 20: "debug", 30: "info", 40: "warn", 50: "error", 60: "fatal",
	}
	for lvl, name := range cases {
		if got := nameFromLevel(lvl); got != name {
			t.Fatalf("nameFromLevel(%d) = %q, want %q", lvl, got, name)
		}
	}
	if nameFromLevel(999) != "" {
		t.Fatalf("expected empty for unknown level")
	}
}

func TestLevelFromName_All(t *testing.T) {
	cases := map[string]int{
		"trace": 10, "debug": 20, "info": 30, "warn": 40, "error": 50, "fatal": 60,
	}
	for name, lvl := range cases {
		if got := levelFromName(name); got != lvl {
			t.Fatalf("levelFromName(%q) = %d, want %d", name, got, lvl)
		}
	}
	if levelFromName("nope") != LevelInfo {
		t.Fatalf("expected fallback to info for unknown name")
	}
}

func TestToFloat_AndToInt(t *testing.T) {
	if f, ok := toFloat(1.5); !ok || f != 1.5 {
		t.Fatalf("toFloat(1.5) = %v,%v", f, ok)
	}
	if f, _ := toFloat("2.5"); f != 2.5 {
		t.Fatalf("toFloat(\"2.5\") = %v", f)
	}
	if f, _ := toFloat(3); f != 3 {
		t.Fatalf("toFloat(3) = %v", f)
	}
	if _, ok := toFloat("nope"); ok {
		t.Fatalf("toFloat(\"nope\") should not parse")
	}
	if toInt("123") != 123 {
		t.Fatalf("toInt(\"123\") = %d", toInt("123"))
	}
	if toInt(7) != 7 {
		t.Fatalf("toInt(7) = %d", toInt(7))
	}
	if toInt(9.9) != 9 {
		t.Fatalf("toInt(9.9) = %d", toInt(9.9))
	}
	if toInt("x") != 0 {
		t.Fatalf("toInt(\"x\") = %d", toInt("x"))
	}
}
