package latexrunner

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"clsi/commandrunner"
)

// --- buildLatexCommand -------------------------------------------------

func TestBuildLatexCommandBasics(t *testing.T) {
	cmd, err := buildLatexCommand("main.tex", "pdflatex", Options{})
	if err != nil {
		t.Fatalf("basic: %v", err)
	}
	want := []string{
		"latexmk",
		"-cd",
		"-jobname=output",
		"-auxdir=$COMPILE_DIR",
		"-outdir=$COMPILE_DIR",
		"-synctex=1",
		"-interaction=batchmode",
		"-time",
		"-f",
		"-pdf",
		"$COMPILE_DIR/main.tex",
	}
	if len(cmd) != len(want) {
		t.Fatalf("len got %d want %d: %v", len(cmd), len(want), cmd)
	}
	for i, wantS := range want {
		if cmd[i] != wantS {
			t.Fatalf("arg %d got %q want %q", i, cmd[i], wantS)
		}
	}
}

func TestBuildLatexCommandStopOnFirstError(t *testing.T) {
	cmd, err := buildLatexCommand("main.tex", "pdflatex", Options{StopOnFirstError: true})
	if err != nil {
		t.Fatalf("stopOnFirstError: %v", err)
	}
	if !sequencePresent(cmd, "-time", "-halt-on-error", "-pdf") {
		t.Fatalf("order wrong: %v", cmd)
	}
	if indexOf(cmd, "-f") >= 0 {
		t.Fatalf("-f should be absent: %v", cmd)
	}
}

func indexOf(cmd []string, s string) int {
	for i, a := range cmd {
		if a == s {
			return i
		}
	}
	return -1
}

func TestBuildLatexCommandStraceAndPrefix(t *testing.T) {
	cmd, err := buildLatexCommand("main.tex", "pdflatex", Options{
		StraceSettings:       "/usr/bin/strace",
		LatexmkCommandPrefix: []string{"time"},
	})
	if err != nil {
		t.Fatalf("strace: %v", err)
	}
	if !sequencePresent(cmd, "strace", "-o", "strace", "-ff", "time", "latexmk") {
		t.Fatalf("order wrong: %v", cmd)
	}
}

func sequencePresent(cmd []string, want ...string) bool {
	idx := 0
	for _, a := range cmd {
		if idx < len(want) && a == want[idx] {
			idx++
		}
	}
	return idx == len(want)
}

func TestBuildLatexCommandFlagsAndCompiler(t *testing.T) {
	// Custom flags distinct from every base/compiler flag so positions can
	// be asserted unambiguously.
	for compiler, flag := range map[string]string{
		"latex":    "-pdfdvi",
		"lualatex": "-lualatex",
		"pdflatex": "-pdf",
		"xelatex":  "-xelatex",
	} {
		cmd, err := buildLatexCommand("main.tex", compiler, Options{
			Flags: []string{"-foo=1", "-bar=2"},
		})
		if err != nil {
			t.Fatalf("%s: %v", compiler, err)
		}
		fIdx := indexOf(cmd, flag)
		f1 := indexOf(cmd, "-foo=1")
		f2 := indexOf(cmd, "-bar=2")
		if f1 < 0 || f2 < 0 || fIdx < 0 {
			t.Fatalf("%s: missing args: %v (f1=%d f2=%d fIdx=%d)", compiler, cmd, f1, f2, fIdx)
		}
		// Node: [... basic, (halt|f), ...opts.flags, compilerFlag, mainFilePath]
		// so custom flags must come before the compiler flag, in order.
		if !(f1 < f2 && f2 < fIdx) {
			t.Fatalf("%s: order wrong: %v", compiler, cmd)
		}
		if got := cmd[fIdx+1]; got != "$COMPILE_DIR/main.tex" {
			t.Fatalf("%s: compiler flag not last arg: %v", compiler, cmd)
		}
	}
}
func TestBuildLatexCommandUnknownCompiler(t *testing.T) {
	_, err := buildLatexCommand("main.tex", "badpdf", Options{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); got != "unknown compiler: badpdf" {
		t.Fatalf("got %q want %q", got, "unknown compiler: badpdf")
	}
}

func TestBuildLatexCommandExtensionReplacement(t *testing.T) {
	for _, ext := range []string{".Rtex", ".Rmd", ".md", ".Rnw"} {
		cmd, err := buildLatexCommand("main"+ext, "pdflatex", Options{})
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if got := cmd[len(cmd)-1]; got != "$COMPILE_DIR/main.tex" {
			t.Fatalf("%s: last arg %q", ext, got)
		}
	}
	// no replacement for other extensions
	cmd, _ := buildLatexCommand("other.log", "pdflatex", Options{})
	if got := cmd[len(cmd)-1]; got != "$COMPILE_DIR/other.log" {
		t.Fatalf("log: last %q", got)
	}
	// a plain "main.tex"
	cmd, _ = buildLatexCommand("main.tex", "pdflatex", Options{})
	if got := cmd[len(cmd)-1]; got != "$COMPILE_DIR/main.tex" {
		t.Fatalf("tex: last %q", got)
	}
}

// --- IsRunning / ProcessTable -----------------------------------------

func TestIsRunningInitialState(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	lr := New(f, nil, nil, nil)
	if lr.IsRunning("never-started") {
		t.Fatalf("should not be running")
	}
}

func TestRunLatexSetsThenClearsProcessTable(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	lr := New(f, nil, nil, nil)

	var gotErr error
	var gotOut *commandrunner.RunOutput
	lr.RunLatex("pid-1", Options{
		Directory: t.TempDir(),
		MainFile:  "main.tex",
		Stats:     map[string]any{},
		Timings:   map[string]any{},
	}, func(err error, out *commandrunner.RunOutput) {
		gotErr = err
		gotOut = out
	})
	if !lr.IsRunning("pid-1") {
		t.Fatalf("should be running while callback pending")
	}
	// drive the callback
	out := &commandrunner.RunOutput{Stdout: "", ExitCode: 0}
	f.drive("pid-1", out, nil)
	if gotErr != nil {
		t.Fatalf("unexpected err: %v", gotErr)
	}
	if gotOut != out {
		t.Fatalf("out not echoed: %v", gotOut)
	}
	if lr.IsRunning("pid-1") {
		t.Fatalf("should be cleared after callback")
	}
}

func TestRunLatexCallbackWritesStatsAndTimings(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	lr := New(f, nil, nil, nil)
	stats := map[string]any{"latexmk": map[string]any{}}
	timings := map[string]any{}
	var gotOut *commandrunner.RunOutput
	var gotErr error
	lr.RunLatex("pid-s", Options{
		Directory: t.TempDir(),
		MainFile:  "main.tex",
		Stats:     stats,
		Timings:   timings,
	}, func(err error, out *commandrunner.RunOutput) {
		gotErr = err
		gotOut = out
	})
	out := &commandrunner.RunOutput{
		Stdout: "Run number 1 of 2, /usr/bin/latex\n" +
			"Latexmk: Errors, attempting to handle them with -f.\n" +
			"Run number 2 of 2, /usr/bin/latex\n",
		Stderr: "Command being timed: /usr/bin/latexmk\n" +
			"Percent of CPU this job got: 123\n" +
			"User time (Journals, Secs.): 1.50.12\n" +
			"System time (Journals, Secs.): 0.75.04\n",
		ExitCode: 0,
	}
	f.drive("pid-s", out, nil)

	if gotErr != nil {
		t.Fatalf("err: %v", gotErr)
	}
	if gotOut != out {
		t.Fatalf("out not echoed")
	}
	if stats["latexmk-errors"] != 1 {
		t.Fatalf("latexmk-errors: got %v want 1", stats["latexmk-errors"])
	}
	if stats["latex-runs"] != 2 {
		t.Fatalf("latex-runs: got %v want 2", stats["latex-runs"])
	}
	if stats["latex-runs-with-errors"] != 2 {
		t.Fatalf("latex-runs-with-errors: got %v want 2 (=runs when failed)", stats["latex-runs-with-errors"])
	}
	if stats["latex-runs-2"] != 1 {
		t.Fatalf("latex-runs-2: got %v want 1", stats["latex-runs-2"])
	}
	if stats["latex-runs-with-errors-2"] != 1 {
		t.Fatalf("latex-runs-with-errors-2: got %v want 1", stats["latex-runs-with-errors-2"])
	}
	// timings: cpuPercentRE -> 123 => 123.0
	if got := timings["cpu-percent"]; got != 123.0 {
		t.Fatalf("cpu-percent: got %v want 123.0", got)
	}
	// cpuTimeRE: "User time.*: (\d+\.\d+)" — "User time (Journals, Secs.): 1.50.12" first match
	// captures "1.50" (because the .* is greedy and the next token group is
	// the first "d+.d+" after the colon). Actually "User time (Journals, Secs.): 1.50.12\n":
	//   "User time" then ".*" greedy matches " (Journals, Secs.): 1.50" then ": " no... let's trace:
	//   ^User time.*: (\d+.\d+)
	//   regex engine is leftmost-then-greedy at each position. ^User time matches "User time".
	//   .* greedy would eat " (Journals, Secs.): 1.50.12" but must leave ": (\d+.\d+)" to match.
	//   The regex engine backtracks: .* matches minimally enough such that ": " + (\d+.\d+) can match.
	//   Greedy: .* first matches up to the last possible "...": \d+.\d+ requires a digit after "."
	//   After "1.50.12\n" — .* matches " (Journals, Secs.): 1.50.1"? and then ": " — no ':' left.
	//   .* matches " (Journals, Secs.)": then ": " matches ": " and (\d+.\d+) captures "1.50" and
	//   the remaining ".12\n" is unmatched (MatchString only needs the anchor match).
	//   Actually FindStringSubmatch returns the first match of the WHOLE pattern, which is
	//   "User time (Journals, Secs.): 1.50" with capture "1.50".
	// So the capture is "1.50" => parseFloat("1.50") = 1.5.
	if got := timings["cpu-time"]; got != 1.5 {
		t.Fatalf("cpu-time: got %v want 1.5 (JS greedy .* stops at first ': ' after User time)", got)
	}
	// sys-time: "System time (Journals, Secs.): 0.75.04" -> captures "0.75" -> 0.75
	if got := timings["sys-time"]; got != 0.75 {
		t.Fatalf("sys-time: got %v want 0.75", got)
	}
}

func TestRunLatexNoRunsNoTimings(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	lr := New(f, nil, nil, nil)
	stats := map[string]any{"latexmk": map[string]any{}}
	timings := map[string]any{}
	var gotOut *commandrunner.RunOutput
	var gotErr error
	lr.RunLatex("pid-n", Options{
		Directory: t.TempDir(),
		MainFile:  "main.tex",
		Stats:     stats,
		Timings:   timings,
	}, func(err error, out *commandrunner.RunOutput) {
		gotOut = out
		gotErr = err
	})
	out := &commandrunner.RunOutput{Stdout: "", Stderr: "", ExitCode: 0}
	f.drive("pid-n", out, nil)

	if gotErr != nil {
		t.Fatalf("unexpected err: %v", gotErr)
	}
	if gotOut != out {
		t.Fatalf("out not echoed: %v", gotOut)
	}
	if stats["latex-runs"] != 0 {
		t.Fatalf("latex-runs want 0 got %v", stats["latex-runs"])
	}
	if stats["latex-runs-with-errors"] != 0 {
		t.Fatalf("latex-runs-with-errors want 0 got %v", stats["latex-runs-with-errors"])
	}
	// include-image-* keys ARE written unconditionally (imgTimes [] is truthy
	// in Node); cpu-* are absent (no 'Command being timed:').
	if v, ok := timings["include-image-all"].(float64); !ok || v != 0 {
		t.Fatalf("include-image-all: got %v want 0 (empty imgTimes)", timings["include-image-all"])
	}
	if v, ok := timings["include-image-optimised"].(float64); !ok || v != 0 {
		t.Fatalf("include-image-optimised: got %v want 0", timings["include-image-optimised"])
	}
	// cpu-* are absent (no 'Command being timed:' in stderr).
	if _, ok := timings["cpu-percent"]; ok {
		t.Fatalf("cpu-percent should be absent")
	}
}

func TestRunLatexMarkerNotPresent(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	lr := New(f, nil, nil, nil)
	// stats WITHOUT the "latexmk" marker: the `if (stats.latexmk)` guard is
	// falsy, so addLatexMkMetrics is skipped.
	stats := map[string]any{} // no "latexmk" key
	timings := map[string]any{}
	var gotOut *commandrunner.RunOutput
	var gotErr error
	lr.RunLatex("pid-g", Options{
		Directory: t.TempDir(),
		MainFile:  "main.tex",
		Stats:     stats,
		Timings:   timings,
	}, func(err error, out *commandrunner.RunOutput) {
		gotOut = out
		gotErr = err
	})
	out := &commandrunner.RunOutput{Stdout: "Run number 1 of 1, /usr/bin/latex\n", ExitCode: 1}
	f.drive("pid-g", out, nil)
	// marker was NOT added by the runner.
	if _, ok := stats["latexmk"]; ok {
		t.Fatalf("latexmk key should not have been created when marker absent")
	}
	// The non-mkdir stats are still written.
	if stats["latex-runs"] != 1 {
		t.Fatalf("latex-runs want 1 got %v", stats["latex-runs"])
	}
	if gotOut != out {
		t.Fatalf("out not echoed")
	}
	if gotErr != nil {
		t.Fatalf("err: %v", gotErr)
	}
}

// --- KillLatex ---------------------------------------------------------

func TestKillLatexNothingRunning(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	lr := New(f, nil, nil, nil)
	var called bool
	lr.KillLatex("no-such", func(err error) {
		called = true
		if err != nil {
			t.Fatalf("kill: unexpected error %v", err)
		}
	})
	if !called {
		t.Fatalf("callback not called")
	}
	if f.killCalls != 0 {
		t.Fatalf("kill should not be forwarded when no such project; got %d", f.killCalls)
	}
}

func TestKillLatexRunningProject(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	lr := New(f, nil, nil, nil)

	// set up a running project
	var gotErr error
	lr.RunLatex("pid-k", Options{
		Directory: t.TempDir(),
		MainFile:  "main.tex",
		Stats:     map[string]any{},
		Timings:   map[string]any{},
	}, func(err error, out *commandrunner.RunOutput) {
		gotErr = err
	})

	// now kill it
	f.killErr = errors.New("kill error")
	var killErr error
	lr.KillLatex("pid-k", func(err error) {
		killErr = err
	})
	if killErr == nil {
		t.Fatalf("expected kill error to be forwarded")
	}
	if f.killCalls != 1 {
		t.Fatalf("killCalls want 1 got %d", f.killCalls)
	}
	if f.killVal != "container-pid-k" {
		t.Fatalf("kill container: got %q want container-pid-k", f.killVal)
	}
	// table entry remains until the Run callback fires (the Run callback is
	// the one that clears it), and we have NOT driven it yet.
	if !lr.IsRunning("pid-k") {
		t.Fatalf("table entry should remain after kill (only Run callback clears)")
	}
	// drive the Run callback; then the table is cleared and gotErr is nil
	out := &commandrunner.RunOutput{Stdout: "", ExitCode: 137}
	f.killVal = ""
	f.drive("pid-k", out, nil)
	if gotErr != nil {
		t.Fatalf("run cb got error: %v", gotErr)
	}
	if lr.IsRunning("pid-k") {
		t.Fatalf("table should be cleared after run callback")
	}
}

// --- Run callback error path ------------------------------------------

func TestRunLatexCallbackErrorShortCircuits(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	lr := New(f, nil, nil, nil)
	stats := map[string]any{"latexmk": map[string]any{}}
	timings := map[string]any{}
	var gotErr error
	var gotOut *commandrunner.RunOutput
	lr.RunLatex("pid-e", Options{
		Directory: t.TempDir(),
		MainFile:  "main.tex",
		Stats:     stats,
		Timings:   timings,
	}, func(err error, out *commandrunner.RunOutput) {
		gotErr = err
		gotOut = out
	})
	errRun := errors.New("simulated docker failure")
	f.drive("pid-e", nil, errRun)
	if gotErr != errRun {
		t.Fatalf("err not echoed: %v", gotErr)
	}
	if gotOut != nil {
		t.Fatalf("out should be nil on error path, got %v", gotOut)
	}
	// stats should NOT be written on the error path
	if _, ok := stats["latex-runs"]; ok {
		t.Fatalf("stats should not be written on error path; got %v", stats)
	}
	// table cleared
	if lr.IsRunning("pid-e") {
		t.Fatalf("entry should be cleared on error path")
	}
}

func TestBuildLatexCommandErrorsShortCircuits(t *testing.T) {
	f := newInlineFakeRunner()
	resetCaptureAndTable(f)
	lr := New(f, nil, nil, nil)
	var gotErr error
	lr.RunLatex("pid-c", Options{
		Directory: t.TempDir(),
		MainFile:  "main.tex",
		Compiler:  "bogus",
		Stats:     map[string]any{},
		Timings:   map[string]any{},
	}, func(err error, out *commandrunner.RunOutput) {
		gotErr = err
	})
	if gotErr == nil {
		t.Fatalf("expected build error")
	}
	if got := gotErr.Error(); got != "unknown compiler: bogus" {
		t.Fatalf("got %q want unknown compiler: bogus", got)
	}
	if lr.IsRunning("pid-c") {
		t.Fatalf("table should NOT contain pid-c on build error")
	}
}

// --- writeLogOutput ----------------------------------------------------

func TestWriteLogOutputNilOutput(t *testing.T) {
	f := newInlineFakeRunner()
	lr := New(f, nil, nil, nil)
	done := make(chan struct{})
	lr.writeLogOutput("pid-w", t.TempDir(), nil, func() {
		close(done)
	})
	select {
	case <-done:
		// expected
	default:
		t.Fatalf("writeLogOutput(nil) should call done synchronously")
	}
}

func TestWriteLogOutputEmptySkips(t *testing.T) {
	dir := t.TempDir()
	f := newInlineFakeRunner()
	lr := New(f, nil, nil, nil)
	done := make(chan struct{})
	lr.writeLogOutput("pid-e", dir, &commandrunner.RunOutput{Stdout: "", Stderr: ""}, func() {
		close(done)
	})
	select {
	case <-done:
	default:
		t.Fatalf("writeLogOutput empty should call done synchronously")
	}
	if _, err := os.Stat(filepath.Join(dir, "output.stdout")); !os.IsNotExist(err) {
		t.Fatalf("output.stdout should not exist for empty stdout; err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "output.stderr")); !os.IsNotExist(err) {
		t.Fatalf("output.stderr should not exist for empty stderr; err=%v", err)
	}
}

func TestWriteLogOutputWritesFiles(t *testing.T) {
	dir := t.TempDir()
	f := newInlineFakeRunner()
	lr := New(f, nil, nil, nil)

	// pre-existing files should be OVERWRITTEN (unlink then write)
	preStd := filepath.Join(dir, "output.stdout")
	preErr := filepath.Join(dir, "output.stderr")
	preStdWrite(t, preStd, "old-stdout\n")
	preErrWrite(t, preErr, "old-stderr\n")

	out := &commandrunner.RunOutput{Stdout: "std-out-bytes\n", Stderr: "std-err-bytes\n"}
	done := make(chan struct{})
	lr.writeLogOutput("pid-w", dir, out, func() { close(done) })
	select {
	case <-done:
	default:
		t.Fatalf("writeLogOutput should call done synchronously")
	}
	sb, err := os.ReadFile(preStd)
	if err != nil {
		t.Fatalf("output.stdout: %v", err)
	}
	if string(sb) != "std-out-bytes\n" {
		t.Fatalf("output.stdout got %q", string(sb))
	}
	eb, err := os.ReadFile(preErr)
	if err != nil {
		t.Fatalf("output.stderr: %v", err)
	}
	if string(eb) != "std-err-bytes\n" {
		t.Fatalf("output.stderr got %q", string(eb))
	}
}

func preStdWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("prewrite: %v", err)
	}
}

func preErrWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("prewrite: %v", err)
	}
}
