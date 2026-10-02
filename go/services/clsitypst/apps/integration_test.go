// integration_test.go — the T12 strict cross-chain layer: the real
// constructors (compilecontroller.New + apps.New) + the real App.Handler,
// driven over httptest, with only the compilemanager typst seams faked (the
// controller/manager/App/controller-factory chain is real code).
//
// This complements the existing suite: the success arm through App.Handler
// (TestCompileRouteSuccess) uses struct-literal Controller fixtures; here
// the ENVELOPE is produced by the real wire translators + the real Manager
// dispatch, and (unlike the controller-level arm) the failure surface is
// exercised end-to-end through the app route + finish middleware.
package apps

import (
	"encoding/json"
	"net/http"
	"testing"

	"ollitex/go/services/clsitypst/commandrunner"
	"ollitex/go/services/clsitypst/compilecontroller"
	"ollitex/go/services/clsitypst/compilemanager"
	cltypstcfg "ollitex/go/services/clsitypst/config"
	"ollitex/go/services/clsitypst/dockerrunner"
	"ollitex/go/services/clsitypst/typstrunner"
)

// chainApp builds the REAL production chain (fake seams only inside the
// Manager): newTestManager (fixture) -> compilecontroller.New -> apps.New.
// The returned Manager lets individual tests retune a typst seam and
// re-drive the same App.
func chainApp(t *testing.T) (*App, *compilemanager.Manager) {
	t.Helper()
	tmp := t.TempDir()
	m := newTestManager(t, tmp)
	cfg := &cltypstcfg.Config{}
	cfg.CompileTypstEnabled = true
	cfg.CompileSizeLimit = "7mb"
	cfg.Path.OutputDir = tmp + "/output"
	cfg.APIs.Clsi.InstanceType = "test-instance"
	cfg.APIs.Clsi.Zone = "zone-a"
	cfg.APIs.Clsi.IsSpotInstance = true
	cfg.APIs.Clsi.OutputURLPrefix = "http://outputs.test"
	cfg.APIs.Clsi.DownloadHost = "http://download.test"
	cfg.ClSI.Docker.Image = "pandoc/typst:latest-alpine"
	cfg.AllowedCompileGroups = []string{"standard"}
	cc := compilecontroller.New(m, cfg)
	a := New(cfg, cc, nil)
	return a, m
}

type chainEnvelope struct {
	Compile struct {
		Status      string `json:"status"`
		BuildID     string `json:"buildId"`
		OutputFiles []struct {
			URL  string `json:"url"`
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"outputFiles"`
	} `json:"compile"`
}

func chainCompile(t *testing.T, a *App) *chainEnvelope {
	t.Helper()
	rec := doPost(t, a.Handler(), "/project/pchain/compile", `{"compile":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("chain compile: %d %s", rec.Code, rec.Body.String())
	}
	var env chainEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("chain envelope: %v (body %s)", err, rec.Body.String())
	}
	return &env
}

// TestChainCompileSuccess — real constructors, real App route: the success
// envelope must come out as produced by the REAL wire translator (buildId
// from the Manager seam, outputFiles with urls) and the app must hand the
// 200 body through unchanged.
func TestChainCompileSuccess(t *testing.T) {
	a, _ := chainApp(t)
	env := chainCompile(t, a)
	if env.Compile.Status != "success" {
		t.Fatalf("chain success: status %q", env.Compile.Status)
	}
	if env.Compile.BuildID != "b1" {
		t.Fatalf("chain buildId: %q", env.Compile.BuildID)
	}
	if len(env.Compile.OutputFiles) == 0 {
		t.Fatalf("chain: no outputFiles")
	}
	f := env.Compile.OutputFiles[0]
	if f.Path != "output.pdf" || f.Type != "pdf" {
		t.Fatalf("chain outputFiles[0]: %s/%s", f.Path, f.Type)
	}
	// url built by the REAL wireFile: %v/project/<pid>[...]/build/<build>/output/<path>.
	if want := "http://download.test/project/pchain/build/b1/output/output.pdf"; f.URL != want {
		t.Fatalf("chain url: %q, want %q", f.URL, want)
	}
}

// TestChainCompileTerminated — the D16/error arm through the FULL chain:
// RunTypst delivers a dockerrunner *TerminatedError (exit 137 = kill -9,
// stop-compile) -> the controller's REAL dispatch -> the app's REAL
// finish middleware -> 200 with status "terminated" (NOT a 5xx: Node
// error.terminated -> 200 failure envelope).
func TestChainCompileTerminated(t *testing.T) {
	a, m := chainApp(t)
	m.RunTypst = func(compileName string, opts typstrunner.Options,
		cb func(err error, out *commandrunner.RunOutput)) {
		cb(&dockerrunner.TerminatedError{}, nil)
	}
	rec := doPost(t, a.Handler(), "/project/pchain/compile", `{"compile":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminated chain: %d %s", rec.Code, rec.Body.String())
	}
	var env chainEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("terminated envelope: %v", err)
	}
	if env.Compile.Status != "terminated" {
		t.Fatalf("terminated chain: status %q (body %s)", env.Compile.Status, rec.Body.String())
	}
	if env.Compile.BuildID != "b1" {
		t.Fatalf("terminated chain: no buildId on the error tail (saveOutputFiles ran: %q)", env.Compile.BuildID)
	}
}

// TestChainCompileGenericError — a run error that is NOT terminated/timed
// out reaches the app's finish else-arm: a 500 with status "error" + the
// run error message (contrasts the 200 "terminated"/"failure" arms: those
// two are the Node error.terminated/error.timedout flags that Node renders
// as 200 failure envelopes; a generic CompileRunError is the finish
// else-arm 500). Exercises the app route + finish + real dispatch.
func TestChainCompileGenericError(t *testing.T) {
	a, m := chainApp(t)
	m.RunTypst = func(compileName string, opts typstrunner.Options,
		cb func(err error, out *commandrunner.RunOutput)) {
		cb(&fakeRunError{}, nil)
	}
	rec := doPost(t, a.Handler(), "/project/pchain/compile", `{"compile":{}}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("generic-error chain: %d (want 500) %s", rec.Code, rec.Body.String())
	}
	var env chainEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		// The else-arm still renders the compile envelope (status "error") +
		// text; the 500 body is the envelope JSON, not the finish string.
		t.Fatalf("envelope: %v (body %s)", err, rec.Body.String())
	}
	if env.Compile.Status != "error" {
		t.Fatalf("generic-error chain: status %q (body %s)", env.Compile.Status, rec.Body.String())
	}
	// saveOutputFiles ran on the error tail: buildId is present.
	if env.Compile.BuildID != "b1" {
		t.Fatalf("generic-error chain: no buildId (body %s)", rec.Body.String())
	}
}

type fakeRunError struct{}

func (fakeRunError) Error() string { return "typst run exploded" }
