// Package latexrunner ports services/clsi/app/js/LatexRunner.js (224L).
//
// Node parity:
//
//   - CompileManager calls `LatexRunner.promises.runLatex(compileName, opts)`
//     with {directory, mainFile, compiler, timeout, image, flags,
//     environment, compileGroup, stopOnFirstError, stats, timings}.
//     compileName is the project id (Node's project_id[-user_id] string).
//   - Module-level ProcessTable keyed by `${projectId}`.
//   - killLatex: `CommandRunner.kill(ProcessTable[id], cb)`; missing =>
//     logger.warn("no such project to kill") then cb(null).
//   - runLatex callback: if (stats.latexmk) addLatexMkMetrics(output,
//     stats, timings); runs = output?.stdout?.match(/^Run number \d+ of
//     .*latex/gm) || output?.stderr fallback || 0; failed = output?.
//     stdout?.match(/^Latexmk: Errors/m) != null ? 1 : 0; stats.latexmk-
//     errors/latex-runs/latex-runs-with-errors/`latex-runs-${runs}`/`
//     latex-runs-with-errors-${runs}`; timings from /usr/bin/time when
//     stderr contains 'Command being timed:'; _writeLogOutput then
//     callback(error, output).
//   - _buildLatexCommand: strace prefix + settings.latexmkCommandPrefix +
//     basic flags + (-halt-on-error|-f) + flags + compiler flag +
//     mainFile.replace(/\.(Rtex|md|Rmd|Rnw)$/,'.tex') path.join.
//   - _writeLogOutput: writes non-empty <dir>/output.stdout and .stderr
//     via unlink + write-wx (errors swallowed + logged).
package latexrunner

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"clsi/commandrunner"
	"clsi/latexmetrics"
)

// ProcessTable (module-level, keyed by `${projectId}`).
var (
	processMu    sync.Mutex
	processTable = map[string]string{}
)

var (
	// TIME_V regexes mirror Node (no ^ anchor, /m flag is a no-op without
	// anchors, and `.` is the any-char class). Go RE2 leftmost-first
	// semantics match JS for these (greedy .* + backtrack).
	cpuPercentRE = regexp.MustCompile(`Percent of CPU this job got: (\d+)`)
	cpuTimeRE    = regexp.MustCompile(`User time.*: (\d+.\d+)`)
	sysTimeRE    = regexp.MustCompile(`System time.*: (\d+.\d+)`)
)

// TIME_V_METRICS mirrors the Node table (name + line-anchored regex).
var TIME_V_METRICS = []timev{
	{"cpu-percent", cpuPercentRE},
	{"cpu-time", cpuTimeRE},
	{"sys-time", sysTimeRE},
}

// compilerFlags mirrors the Node COMPILER_FLAGS table.
var compilerFlags = map[string]string{
	"latex":    "-pdfdvi",
	"lualatex": "-lualatex",
	"pdflatex": "-pdf",
	"xelatex":  "-xelatex",
}

var (
	// runNumberRE mirrors /^Run number \d+ of .*latex/m (per-line).
	runNumberRE = regexp.MustCompile(`^Run number \d+ of .*latex`)
	// latexmkErrRE mirrors /^Latexmk: Errors/m (per-line).
	latexmkErrRE = regexp.MustCompile(`^Latexmk: Errors`)
	// extRE mirrors /\.(Rtex|md|Rmd|Rnw)$/.
	extRE = regexp.MustCompile(`\.(Rtex|md|Rmd|Rnw)$`)
)

type timev struct {
	name string
	re   *regexp.Regexp
}

// Options mirrors the options object passed to Node runLatex.
type Options struct {
	Directory        string
	MainFile         string
	Compiler         string // default "pdflatex"
	Timeout          int64  // default 60000 (ms)
	Image            string
	Flags            []string
	Environment      map[string]string
	CompileGroup     string
	StopOnFirstError bool
	Stats            map[string]any
	Timings          map[string]any
	// StraceSettings / LatexmkCommandPrefixSettings are the settings.clsi.*
	// fields (undefined => "" / empty).
	StraceSettings       string
	LatexmkCommandPrefix []string
}

// LatexRunner is the module default export (isRunning/runLatex/killLatex).
type LatexRunner struct {
	runner   Runner
	logDebug func(map[string]any, string)
	logWarn  func(map[string]any, string)
	logError func(map[string]any, string)
}

// Runner is the CommandRunner surface consumed by LatexRunner.
type Runner = commandrunner.Runner

// New wires the mandatory docker runner. logger seams nil => no-op.
func New(runner Runner, logDebug, logWarn, logError func(map[string]any, string)) *LatexRunner {
	if logDebug == nil {
		logDebug = func(map[string]any, string) {}
	}
	if logWarn == nil {
		logWarn = func(map[string]any, string) {}
	}
	if logError == nil {
		logError = func(map[string]any, string) {}
	}
	return &LatexRunner{runner: runner, logDebug: logDebug, logWarn: logWarn, logError: logError}
}

// IsRunning mirrors isRunning(projectId): ProcessTable[id] != null.
func (r *LatexRunner) IsRunning(projectID string) bool {
	processMu.Lock()
	defer processMu.Unlock()
	_, ok := processTable[projectID]
	return ok
}

// KillLatex mirrors killLatex(projectId, callback).
func (r *LatexRunner) KillLatex(projectID string, cb func(err error)) {
	processMu.Lock()
	name, ok := processTable[projectID]
	processMu.Unlock()
	r.logDebug(map[string]any{"id": projectID}, "killing running compile")
	if !ok {
		r.logWarn(map[string]any{"id": projectID}, "no such project to kill")
		cb(nil)
		return
	}
	r.runner.Kill(name, cb)
}

// RunLatex mirrors the callback form of Node runLatex.
func (r *LatexRunner) RunLatex(projectID string, opts Options, cb func(err error, out *commandrunner.RunOutput)) {
	compiler := opts.Compiler
	if compiler == "" {
		compiler = "pdflatex"
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 60000
	}
	if opts.Stats == nil {
		opts.Stats = map[string]any{}
	}
	if opts.Timings == nil {
		opts.Timings = map[string]any{}
	}
	r.logDebug(map[string]any{
		"directory":        opts.Directory,
		"compiler":         compiler,
		"timeout":          timeout,
		"mainFile":         opts.MainFile,
		"environment":      opts.Environment,
		"flags":            opts.Flags,
		"compileGroup":     opts.CompileGroup,
		"stopOnFirstError": opts.StopOnFirstError,
	}, "starting compile")

	command, err := buildLatexCommand(opts.MainFile, compiler, opts)
	if err != nil {
		cb(err, nil)
		return
	}
	id := projectID
	// Node: `ProcessTable[id] = CommandRunner.run(...)`. run() may dispatch an
	// error callback synchronously (Node: `image not allowed`) where the
	// assigned value is `undefined` and isRunning() is false for that id.
	// In Go: skip the table entry when the callback already fired.
	fired := false
	rname := r.runner.Run(projectID, command, opts.Directory, opts.Image,
		timeout, opts.Environment, opts.CompileGroup, "", func(err error, out *commandrunner.RunOutput) {
			fired = true
			processMu.Lock()
			delete(processTable, id)
			processMu.Unlock()
			// Node: `if (error) return callback(error)` — fires before any
			// metrics/timing/log work.
			if err != nil {
				cb(err, nil)
				return
			}
			// Node: `if (stats.latexmk)` — the marker exists iff the caller
			// ran `enableLatexMkMetrics(stats)` (CompileManager always does).
			if _, ok := opts.Stats["latexmk"].(map[string]any); ok {
				latexmetrics.AddLatexMkMetrics(out.Stdout, out.Stderr, opts.Stats, opts.Timings)
			}

			runs := countRunNumber(out.Stdout)
			if runs == 0 {
				runs = countRunNumber(out.Stderr)
			}
			failed := 0
			if hasLatexmkErrors(out.Stdout) {
				failed = 1
			}
			opts.Stats["latexmk-errors"] = failed
			opts.Stats["latex-runs"] = runs
			// Node: `stats['latex-runs-with-errors'] = failed ? runs : 0` (the
			// run count, not just the failure flag).
			we := 0
			if failed == 1 {
				we = runs
			}
			opts.Stats["latex-runs-with-errors"] = we
			opts.Stats["latex-runs-"+strconv.Itoa(runs)] = 1
			if failed == 1 {
				opts.Stats["latex-runs-with-errors-"+strconv.Itoa(runs)] = 1
			} else {
				opts.Stats["latex-runs-with-errors-"+strconv.Itoa(runs)] = 0
			}

			stderr := out.Stderr
			if strings.Contains(stderr, "Command being timed:") {
				for _, tv := range TIME_V_METRICS {
					m := tv.re.FindStringSubmatch(stderr)
					if len(m) >= 2 {
						v, _ := strconv.ParseFloat(m[1], 64)
						opts.Timings[tv.name] = v
					}
				}
			}

			r.writeLogOutput(projectID, opts.Directory, out, func() {
				cb(nil, out)
			})
		})
	if !fired {
		// Run has not fired the callback synchronously: mirror the Node
		// `ProcessTable[id] = <name>` assignment.
		processMu.Lock()
		processTable[id] = rname
		processMu.Unlock()
	}
}

// runLatexAsync is the promisified wrapper (Node: promisify(runLatex)).
// It returns a channel that receives the terminal error.
func (r *LatexRunner) RunLatexAsync(projectID string, opts Options) <-chan error {
	done := make(chan error, 1)
	r.RunLatex(projectID, opts, func(err error, _ *commandrunner.RunOutput) {
		done <- err
	})
	return done
}

// buildLatexCommand mirrors _buildLatexCommand(mainFile, opts = {}).
func buildLatexCommand(mainFile, compiler string, opts Options) ([]string, error) {
	command := []string{}
	if opts.StraceSettings != "" {
		command = append(command, "strace", "-o", "strace", "-ff")
	}
	if len(opts.LatexmkCommandPrefix) > 0 {
		command = append(command, opts.LatexmkCommandPrefix...)
	}
	command = append(command,
		"latexmk",
		"-cd",
		"-jobname=output",
		"-auxdir=$COMPILE_DIR",
		"-outdir=$COMPILE_DIR",
		"-synctex=1",
		"-interaction=batchmode",
		"-time",
	)
	if opts.StopOnFirstError {
		command = append(command, "-halt-on-error")
	} else {
		command = append(command, "-f")
	}
	if len(opts.Flags) > 0 {
		command = append(command, opts.Flags...)
	}
	flag, ok := compilerFlags[compiler]
	if !ok {
		return nil, errors.New("unknown compiler: " + compiler)
	}
	command = append(command, flag)
	command = append(command, filepath.Join("$COMPILE_DIR", extRE.ReplaceAllString(mainFile, ".tex")))
	return command, nil
}

// writeLogOutput mirrors _writeLogOutput.
// errors are swallowed + logged (Node "don't fail on error").
func (r *LatexRunner) writeLogOutput(projectID, dir string, out *commandrunner.RunOutput, done func()) {
	if out == nil {
		done()
		return
	}
	writeOne := func(name string, content string) {
		if content == "" {
			return
		}
		dst := filepath.Join(dir, name)
		os.Remove(dst)
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			r.logError(map[string]any{"err": err.Error(), "projectId": projectID,
				"file": name}, "error writing log file")
			return
		}
		_, _ = f.Write([]byte(content))
		f.Close()
	}
	writeOne("output.stdout", out.Stdout)
	writeOne("output.stderr", out.Stderr)
	done()
}

// countRunNumber mirrors Node's multiline match count.
func countRunNumber(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if runNumberRE.MatchString(line) {
			n++
		}
	}
	return n
}

func hasLatexmkErrors(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		if latexmkErrRE.MatchString(line) {
			return true
		}
	}
	return false
}
