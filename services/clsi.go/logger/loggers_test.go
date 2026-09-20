package logger

import (
	"log/slog"
	"testing"
)

// Port of services/clsi/test/unit/js/LoggerSerializers tests (the Node
// service test suite encodes the contract).
func TestClsiRequestSummary(t *testing.T) {
	req := map[string]interface{}{
		"syncState":              "huge-content-should-not-appear",
		"resources":              map[string]interface{}{"path": "main.tex"},
		"compiler":               "xelatex",
		"compileFromClsiCache":   true,
		"populateClsiCache":      false,
		"enablePdfCaching":       true,
		"timeout":                1,
		"imageName":              "/my/images/pdflatex",
		"draft":                  true,
		"pdfCachingMinChunkSize": 100,
		"check":                  "draft",
		"flags":                  []interface{}{"--draft"},
		"compileGroup":           "grp",
		"syncType":               "latex-draft",
		"token":                  "not-a-part-of-surface",
	}
	sum := SerializeClsiRequest(req)

	for _, k := range []string{
		"compiler", "compileFromClsiCache", "populateClsiCache",
		"enablePdfCaching", "pdfCachingMinChunkSize", "timeout",
		"draft", "check", "flags", "compileGroup", "syncType",
	} {
		if _, ok := sum[k]; !ok {
			t.Errorf("summary missing key %q", k)
		}
	}
	if got, ok := sum["imageName"]; !ok || got.(string) != "pdflatex" {
		t.Errorf("imageName = %v, want base-name \"pdflatex\"", sum["imageName"])
	}
	if _, ok := sum["syncState"]; ok {
		t.Error("summary must not contain syncState")
	}
	if _, ok := sum["resources"]; ok {
		t.Error("summary must not contain resources")
	}
	if _, ok := sum["token"]; ok {
		t.Error("summary must not contain token")
	}
	if len(sum) != 12 {
		t.Errorf("len(summary) = %d, want 12", len(sum))
	}
}

func TestClsiRequestOmitsAbsentAndFalsy(t *testing.T) {
	req := map[string]interface{}{
		"compiler":  "pdflatex",
		"imageName": "",
	}
	sum := SerializeClsiRequest(req)
	if _, ok := sum["imageName"]; ok {
		t.Error("falsy imageName must be omitted")
	}
	if got := sum["compiler"]; got != "pdflatex" {
		t.Errorf("compiler = %v", got)
	}
	if len(sum) != 1 {
		t.Errorf("len(sum) = %d, want 1", len(sum))
	}
}

func TestClsiRequestEmpty(t *testing.T) {
	if sum := SerializeClsiRequest(map[string]interface{}{}); len(sum) != 0 {
		t.Errorf("empty request => summary = %v, want empty", sum)
	}
}

func TestLevelsDoNotPanic(t *testing.T) {
	if Log == nil {
		t.Fatal("Log is nil after init")
	}
	// Exercise each leveled function (they log to stderr via JSON handler);
	// assert they run without panic.
	Debug(map[string]any{"k": "v"}, "debug msg")
	Info(map[string]any{"k": "v"}, "info msg")
	Warn(map[string]any{"k": "v"}, "warn msg")
	Error(map[string]any{"k": "v"}, "error msg")
	Err(map[string]any{"k": "v"}, "err msg")
}

func TestLogLevelInit(t *testing.T) {
	// The package-level Log must be a non-nil *slog.Logger after init().
	if Log == nil {
		t.Fatal("init() did not set Log")
	}
	// Handler is JSON-based: emit via the level funcs above covers the
	// call sites; here we only confirm the logger is wired.
	var _ = Log.Info
}

func TestResolveLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"", slog.LevelInfo},
		{"debug", slog.Level(-4)},
		{"trace", slog.Level(-4)},
		{"30", slog.LevelInfo},
		{"100", slog.LevelInfo},
		{"20", slog.LevelDebug},
		{"5", slog.LevelDebug},
		{"garbage", slog.LevelInfo},
	}
	for _, c := range cases {
		if got := resolveLevel(c.in); got != c.want {
			t.Errorf("resolveLevel(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestNewLogger(t *testing.T) {
	l := NewLogger(slog.LevelWarn)
	if l == nil {
		t.Fatal("NewLogger returned nil")
	}
}
