// Package typstrunner ports services/clsi_typst/app/js/TypstRunner.js (97L).
//
// Node parity:
//
//   - Module-level ProcessTable keyed by `${projectId}` (the container id
//     value returned by CommandRunner.run — Go: the container name).
//   - buildTypstCompileCommand: `sh -c 'echo "typst $(typst --version ...)"
//     > "$3"; typst compile "$1" "$2" >> "$3" 2>&1; exit 0'` — the `exit 0`
//     is DELIBERATE (D16): typst reports error: lines into output.log, so a
//     compile with errors still exits 0 and clsi does not treat it as a
//     failure (success gate = output.pdf size>0, D16).
//   - runTypst: runs the command through the (clsi generic) Runner, records
//     ProcessTable[id], on completion deletes it, counts `^error:` lines into
//     stats['typst-errors'], sets stats['typst-compile-runs']=1, then
//     callback(error, output).
//   - isRunning/killTypst: ProcessTable lookups; missing => warn + cb(null).
//
// D22: this package (clsi_typst/typstrunner) is local; it consumes the clsi
// generic Runner/RunOutput seam (clsi/commandrunner) — NO clsi.go change.
// The copied dockerrunner (clsi_typst/dockerrunner) provides the docker
// implementation that satisfies commandrunner.Runner (clsi.go invariant:
// commandrunner is a thin guard over the docker runner).
package typstrunner

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"ollitex/go/services/clsitypst/commandrunner"
)

// ProcessTable (module-level, keyed by `${projectId}`).
var (
	processMu    sync.Mutex
	processTable = map[string]string{}
)

// errLineRE mirrors Node's /^error: .*/gm (per-line).
var errLineRE = regexp.MustCompile(`^error: .*`)

// Options mirrors the options object passed to Node runTypst.
type Options struct {
	Directory    string
	MainFile     string
	Timeout      int64 // default 60000 (ms)
	Image        string
	Environment  map[string]string
	CompileGroup string
	Stats        map[string]any
	Timings      map[string]any
}

// TypstRunner is the module default export (isRunning/runTypst/killTypst).
type TypstRunner struct {
	runner commandrunner.Runner
	// logDebug/logWarn seams (Node logger.debug / logger.warn).
	logDebug func(map[string]any, string)
	logWarn  func(map[string]any, string)
}

// New wires the mandatory docker runner. Logger seams nil => no-op (tests).
func New(runner commandrunner.Runner, logDebug, logWarn func(map[string]any, string)) *TypstRunner {
	if logDebug == nil {
		logDebug = func(map[string]any, string) {}
	}
	if logWarn == nil {
		logWarn = func(map[string]any, string) {}
	}
	return &TypstRunner{runner: runner, logDebug: logDebug, logWarn: logWarn}
}

// IsRunning mirrors isRunning(projectId): ProcessTable[id] != null.
func (r *TypstRunner) IsRunning(projectID string) bool {
	processMu.Lock()
	defer processMu.Unlock()
	_, ok := processTable[projectID]
	return ok
}

// KillTypst mirrors killTypst(projectId, callback).
func (r *TypstRunner) KillTypst(projectID string, cb func(err error)) {
	processMu.Lock()
	name, ok := processTable[projectID]
	processMu.Unlock()
	r.logDebug(map[string]any{"id": projectID}, "killing running typst compile")
	if !ok {
		r.logWarn(map[string]any{"id": projectID}, "no such project to kill")
		cb(nil)
		return
	}
	r.runner.Kill(name, cb)
}

// buildTypstCompileCommand mirrors buildTypstCompileCommand(mainFile).
//
//	sh -c 'echo "typst $(typst --version 2>/dev/null || echo unknown)" > "$3";
//	       typst compile "$1" "$2" >> "$3" 2>&1; exit 0' -- \
//	       $COMPILE_DIR/<mainFile> output.pdf output.log
func buildTypstCompileCommand(mainFile string) []string {
	return []string{
		"sh",
		"-c",
		"echo \"typst $(typst --version 2>/dev/null || echo unknown)\" > \"$3\"; " +
			"typst compile \"$1\" \"$2\" >> \"$3\" 2>&1; exit 0",
		"--",
		filepath.Join("$COMPILE_DIR", mainFile),
		"output.pdf",
		"output.log",
	}
}

// RunTypst mirrors the callback form of Node runTypst.
func (r *TypstRunner) RunTypst(projectID string, opts Options, cb func(err error, out *commandrunner.RunOutput)) {
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
		"directory":    opts.Directory,
		"timeout":      timeout,
		"mainFile":     opts.MainFile,
		"environment":  opts.Environment,
		"compileGroup": opts.CompileGroup,
	}, "starting typst compile")

	command := buildTypstCompileCommand(opts.MainFile)
	id := projectID

	// Node: `ProcessTable[id] = CommandRunner.run(...)`. run() may dispatch
	// the error callback synchronously (copied dockerrunner: "image not
	// allowed" path), in which case the table entry must NOT stick.
	fired := false
	rname := r.runner.Run(projectID, command, opts.Directory, opts.Image,
		timeout, opts.Environment, opts.CompileGroup, "", func(err error, out *commandrunner.RunOutput) {
			fired = true
			processMu.Lock()
			delete(processTable, id)
			processMu.Unlock()
			if err != nil {
				cb(err, nil)
				return
			}
			// Informational stats (clsi_compile_metrics parity).
			errLines := countErrorLines(out.Stdout)
			opts.Stats["typst-errors"] = errLines
			opts.Stats["typst-compile-runs"] = 1
			cb(nil, out)
		})
	if !fired {
		processMu.Lock()
		processTable[id] = rname
		processMu.Unlock()
	}
}

// RunTypstAsync is the promisified wrapper (Node: promisify(runTypst)).
func (r *TypstRunner) RunTypstAsync(projectID string, opts Options) <-chan error {
	done := make(chan error, 1)
	r.RunTypst(projectID, opts, func(err error, _ *commandrunner.RunOutput) {
		done <- err
	})
	return done
}

func countErrorLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if errLineRE.MatchString(line) {
			n++
		}
	}
	return n
}
