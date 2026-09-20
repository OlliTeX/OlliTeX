package latexrunner

import (
	"clsi/commandrunner"
)

// fakeRunner captures Run invocations and lets tests drive the callback
// (mirroring Node's async docker callback). A nil capture means "drive
// inline" via the inline callback (the sync mode).
type inlineFakeRunner struct {
	captured map[string]func(err error, out *commandrunner.RunOutput)
	killErr  error
	killVal  string
	killCalls int
}

func newInlineFakeRunner() *inlineFakeRunner {
	return &inlineFakeRunner{captured: map[string]func(err error, out *commandrunner.RunOutput){}}
}

func (f *inlineFakeRunner) capture(projectID string, cb func(err error, out *commandrunner.RunOutput)) {
	f.captured[projectID] = cb
}

func (f *inlineFakeRunner) drive(projectID string, out *commandrunner.RunOutput, err error) {
	cb, ok := f.captured[projectID]
	if !ok {
		return
	}
	cb(err, out)
}

func (f *inlineFakeRunner) Run(projectID string, command []string, directory string, image string,
	timeout int64, environment map[string]string, compileGroup string, cwd string,
	callback func(err error, out *commandrunner.RunOutput)) string {
	f.capture(projectID, callback)
	return "container-" + projectID
}

func (f *inlineFakeRunner) Kill(containerID string, callback func(err error)) {
	f.killVal = containerID
	f.killCalls++
	callback(f.killErr)
}

func (f *inlineFakeRunner) CanRunSyncTeXInOutputDir() bool { return true }

func resetCaptureAndTable(f *inlineFakeRunner) {
	processMu.Lock()
	processTable = map[string]string{}
	processMu.Unlock()
}
