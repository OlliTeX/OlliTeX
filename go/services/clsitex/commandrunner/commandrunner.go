// Package commandrunner ports services/clsi/app/js/CommandRunner.js (19L).
//
// Node parity notes:
//
//   - CommandRunner.js is a MANDATORY guard: sandboxed (Docker) compiles are
//     required in OlliTeX. If Settings.clsi.dockerRunner !== true the Node
//     module logs and `process.exit(1)`. In Go New() returns an error in that
//     case (same loud-failure semantics; the app init refuses to start).
//   - The guard then re-exports DockerRunner (the .mjs module) AS the runner.
//     Runner here is the DockerRunner interface, supplied by dockerclient.
//     There is no local (in-container) runner: it was removed from Node and is
//     never ported.
package commandrunner

import (
	"fmt"
)

// RunOutput mirrors the promisified output object {stdout, stderr, exitCode}
// plus the error flag fields DockerRunner sets (timedout / terminated / code).
type RunOutput struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	Terminated bool // exit 137 (kill -9)
	Exited     bool // exit 1 (chktex)
	TimedOut   bool // container timed out
}

// Runner mirrors the surface of the Node CommandRunner default export
// actually consumed by CompileManager / LatexRunner / Png2Pdf /
// ConversionManager (CommandRunner.run / .kill / .promises.run /
// canRunSyncTeXInOutputDir).
type Runner interface {
	// Run mirrors DockerRunner.run (the non-promisified, sync-return form).
	// It returns the container NAME (Go convenience mirroring the Node dual
	// "return name + callback" shape). output, when the run is awaited, is
	// delivered via callback (error, output).
	Run(projectID string, command []string, directory string, image string,
		timeout int64, environment map[string]string, compileGroup string, cwd string,
		callback func(err error, out *RunOutput)) string
	// Kill mirrors DockerRunner.kill.
	Kill(containerID string, callback func(err error))
	// CanRunSyncTeXInOutputDir mirrors DockerRunner.canRunSyncTeXInOutputDir.
	CanRunSyncTeXInOutputDir() bool
}

// New ports CommandRunner.js: the guard + selection of the mandatory Docker
// runner. dockerRunner is the settings.clsi.dockerRunner flag (always true in
// this repo). runner is the DockerRunner implementation (from dockerclient).
// When the flag is not set, New returns an error mirroring the Node
// `process.exit(1)` (the caller must refuse to start). When runner is nil,
// New returns an error mirroring "DockerRunner.mjs required to sandbox compiles".
func New(dockerRunner bool, runner Runner, loggerf func(msg string, attrs map[string]any)) (Runner, error) {
	if !dockerRunner {
		if loggerf != nil {
			loggerf("clsi requires sandboxed compiles (clsi.dockerRunner=true). "+
				"This is enforced for OlliTeX; refusing to start with a local runner.", map[string]any{})
		}
		return nil, fmt.Errorf("commandrunner: sandboxed compiles required (clsi.dockerRunner must be true)")
	}
	if runner == nil {
		return nil, fmt.Errorf("commandrunner: DockerRunner.mjs (docker runner) is required to sandbox compiles")
	}
	if loggerf != nil {
		loggerf("commandrunner selected (mandatory: sandboxed)", map[string]any{"commandRunnerPath": "./DockerRunner.mjs"})
	}
	return runner, nil
}
