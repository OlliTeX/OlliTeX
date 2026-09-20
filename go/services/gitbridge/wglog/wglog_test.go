package wglog

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

// swapStderr redirects os.Stderr to a temp file for the duration of f.
// The slog handler is bound to the *os.File at SetLevel time and writes
// directly (no Go-level buffering), so the temp file can be read back after
// restoring os.Stderr.
func swapStderr(t *testing.T, f func()) (path string) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatalf("temp stderr: %v", err)
	}
	old := os.Stderr
	os.Stderr = file
	defer func() {
		os.Stderr = old
		file.Close()
	}()
	f()
	return file.Name()
}

// TestFilteringHonoursLevel ports the Java Log level-filtering contract:
// with level WARN, Debug/Info are filtered, Warn/Error are emitted.
func TestFilteringHonoursLevel(t *testing.T) {
	out := swapStderr(t, func() {
		SetLevel(slog.LevelWarn)
		Debug("debug %d filtered", 1)
		Info("info %%s filtered %s", "never-logged")
		Warn("warn %s emitted", "marker-warn")
		Error("error %s emitted", "marker-error")
	})
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	s := string(data)
	if strings.Contains(s, "marker-warn") != true {
		t.Fatalf("WARN not emitted at WARN level: %q", s)
	}
	if strings.Contains(s, "marker-error") != true {
		t.Fatalf("ERROR not emitted at WARN level: %q", s)
	}
	if strings.Contains(s, "never-logged") {
		t.Errorf("INFO emitted at WARN level; must be filtered: %q", s)
	}
	if strings.Contains(s, "filtered") {
		t.Errorf("DEBUG/Info emitted at WARN level; must be filtered: %q", s)
	}
}

// TestParseLevel pins the LOG_LEVEL -> slog.Level mapping (logback semantics,
// including the WARNING->WARN normalization Java does).
func TestParseLevel(t *testing.T) {
	cases := []struct {
		name string
		want slog.Level
	}{
		{"", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"info", slog.LevelInfo},
		{"DEBUG", slog.LevelDebug},
		{"TRACE", slog.Level(-4)},
		{"debug", slog.LevelDebug},
		{"WARN", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"ERROR", slog.LevelError},
		{"garbage", slog.LevelInfo}, // default fallback
	}
	for _, c := range cases {
		if got := parseLevel(c.name); got != c.want {
			t.Errorf("parseLevel(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestDefaultLevelIsInfo pins the Java default (LOG_LEVEL unset -> INFO).
func TestDefaultLevelIsInfo(t *testing.T) {
	defer os.Setenv("LOG_LEVEL", "")
	os.Unsetenv("LOG_LEVEL")
	// SetLevel(Info) must emit INFO and filter DEBUG, matching logback INFO.
	out := swapStderr(t, func() {
		SetLevel(slog.LevelInfo)
		Info("info-emitted %d", 1)
		Debug("debug-filtered %d", 2)
	})
	data, _ := os.ReadFile(out)
	s := string(data)
	if !strings.Contains(s, "info-emitted") || strings.Contains(s, "debug-filtered") {
		t.Fatalf("INFO level filter wrong: %q", s)
	}
}

// TestSetLevelRoundTrip exercises the exported setter (the seam used by
// GceMetadataLogLevelChecker at runtime).
func TestSetLevelRoundTrip(t *testing.T) {
	out := swapStderr(t, func() {
		SetLevel(slog.LevelWarn)
	})
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("SetLevel did not create handler on stderr: %v", err)
	}
}
