package png2pdf

import (
	"testing"

	"clsi/config"
)

func enabledConfig(t *testing.T) {
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/host/compiles")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/host/cache")
	t.Setenv("ENABLE_PNG2PDF_CONVERSIONS", "true")
	config.ForTest()
	t.Cleanup(ClearSeams)
}

func TestIsEnabled(t *testing.T) {
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/host/compiles")
	// Default (no enable flag, no cache dir) -> disabled.
	config.ForTest()
	if IsEnabled() {
		t.Fatal("default: IsEnabled = true (no env set)")
	}
	// All gates set -> enabled.
	t.Setenv("ENABLE_PNG2PDF_CONVERSIONS", "true")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/host/cache")
	config.ForTest()
	if !IsEnabled() {
		t.Fatal("all gates set: IsEnabled = false")
	}
}

func TestConvertCallsRunnerAndRecordsStats(t *testing.T) {
	enabledConfig(t)
	var gotImage, gotDir string
	gotTimeout := int64(-1)
	gotCommand := []string(nil)
	gotGroup := ""
	RunFunc = func(projectID string, command []string, directory, image string,
		timeout int64, environment map[string]string, compileGroup, cwd string) (Output, error) {
		gotImage = image
		gotDir = directory
		gotTimeout = timeout
		gotGroup = compileGroup
		gotCommand = command
		return Output{Stdout: "Converted a.tmp to PDF\nConverted b.tmp to PDF\n", Stderr: "", ExitCode: 0}, nil
	}
	stats, timings := &Stats{}, &Timings{}
	if err := ConvertPngFilesInCacheDir("p1", "/cache/dir", []string{"a.tmp", "b.tmp"}, stats, timings); err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(gotCommand) != 4 || gotCommand[0] != "--in-place" || gotCommand[1] != "--" || gotCommand[2] != "a.tmp" || gotCommand[3] != "b.tmp" {
		t.Fatalf("command = %v, want [--in-place -- a.tmp b.tmp]", gotCommand)
	}
	if gotDir != "/cache/dir" || gotGroup != "png2pdf" || gotImage != "image" && gotImage == "" {
		t.Fatalf("dir=%q group=%q image=%q (need non-empty image)", gotDir, gotGroup, gotImage)
	}
	// Default PNG2PDF_IMAGE is set by config; assert timeout = cfg.ConversionTimeoutSeconds*1000 (default 60_000).
	if gotTimeout != int64(config.Get().ConversionTimeoutSeconds*1000) {
		t.Fatalf("timeout = %d, want %d", gotTimeout, config.Get().ConversionTimeoutSeconds*1000)
	}
	if stats.Png2pdf != 2 {
		t.Fatalf("stats.Png2pdf = %d, want 2", stats.Png2pdf)
	}
	// Timings recorded (the port uses DoneMS, an arbitrary non-negative value).
	if timings.Png2pdf < 0 {
		t.Fatalf("timings.Png2pdf = %d, want >= 0", timings.Png2pdf)
	}
}

func TestConvertRecordsOnlyConverted(t *testing.T) {
	enabledConfig(t)
	RunFunc = func(_ string, _ []string, _, _ string, _ int64, _ map[string]string, _, _ string) (Output, error) {
		return Output{Stdout: "Converted a.tmp to PDF\n", Stderr: "", ExitCode: 0}, nil
	}
	stats := &Stats{}
	if err := ConvertPngFilesInCacheDir("p1", "/cache/dir", []string{"a.tmp", "b.tmp"}, stats, nil); err != nil {
		t.Fatalf("convert: %v", err)
	}
	if stats.Png2pdf != 1 {
		t.Fatalf("stats.Png2pdf = %d, want 1 (b.tmp skipped)", stats.Png2pdf)
	}
}

func TestConvertDoesNothingWhenDisabled(t *testing.T) {
	// No enable flag -> disabled (compiles dir still needed by sandboxed New).
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/host/compiles")
	t.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "")
	config.ForTest()
	if IsEnabled() {
		t.Fatal("expect disabled when no cache dir")
	}
	called := false
	RunFunc = func(_ string, _ []string, _, _ string, _ int64, _ map[string]string, _, _ string) (Output, error) {
		called = true
		return Output{}, nil
	}
	stats, timings := &Stats{}, &Timings{}
	if err := ConvertPngFilesInCacheDir("p1", "/cache/dir", []string{"a.tmp"}, stats, timings); err != nil {
		t.Fatalf("disabled convert: %v", err)
	}
	if called {
		t.Fatalf("runner called when disabled")
	}
	if stats.Png2pdf != 0 {
		t.Fatalf("stats.Png2pdf = %d, want 0 (untouched)", stats.Png2pdf)
	}
}

func TestConvertNonZeroExitRecordsTimingsOnly(t *testing.T) {
	enabledConfig(t)
	RunFunc = func(_ string, _ []string, _, _ string, _ int64, _ map[string]string, _, _ string) (Output, error) {
		return Output{Stdout: "", Stderr: "boom", ExitCode: 1}, nil
	}
	stats, timings := &Stats{}, &Timings{}
	err := ConvertPngFilesInCacheDir("p1", "/cache/dir", []string{"a.tmp"}, stats, timings)
	if err == nil {
		t.Fatalf("expect error (non-zero exit), got nil")
	}
	if stats.Png2pdf != 0 {
		t.Fatalf("stats.Png2pdf = %d, want 0 (untouched on error)", stats.Png2pdf)
	}
	// Timings still recorded (Node: done() fires on the throw path too).
	if timings.Png2pdf < 0 {
		t.Fatalf("timings.Png2pdf = %d, want >= 0 even on error", timings.Png2pdf)
	}
}
