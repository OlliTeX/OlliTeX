// Package smoketest ports services/clsi/test/smoke/js/SmokeTests.js.
//
// Node parity notes:
//
//   - The smoke test self-POSTs a LaTeX compile to this service's own
//     compile endpoint (Node: http://<host>:<port>/project/smoketest-<pid>/compile).
//   - Node validates body.compile.outputFiles, a vestige of the wire shape
//     before compilecontroller's top-level envelope (buildCompileBody). The
//     Go endpoint renders a TOP-LEVEL {status, buildId, outputFiles, ...}
//     envelope, so Run() validates the top-level keys and looks for a
//     "pdf" and a "log" entry in outputFiles (see wire.go). Same intent,
//     corrected Go wire path (documented divergence).
//   - Node's fetchJson had no explicit timeout; Go uses an injectable
//     timeout (default DefaultTimeout = 5 min) because a cold LaTeX compile
//     with TikZ/minted shell-escapes can be slow.
package smoketest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"ollitex/go/services/clsitex/config"
)

// DefaultTimeout is the default per-compile budget (see package doc).
const DefaultTimeout = 5 * time.Minute

// mainTex is the verbatim LaTeX document from Node's SmokeTests.js (the JS
// template literal de-escaped at port time: \ -> \ and the trailing
// line-continuation backslashes dropped, exactly as V8 does).
const mainTex = `% Membrane-like surface
% Author: Yotam Avital
\\documentclass{article}
\\usepackage{tikz}
\\usetikzlibrary{calc,fadings,decorations.pathreplacing}
\\usepackage{ifplatform} % test shell escape, conditionals to test which platform is being used
\\usepackage{minted}  % to test shell commands
\\usepackage{bashful} % to test shell commands
\\begin{document}
\\begin{tikzpicture}
  \\def\\nuPi{3.1459265}
  \\foreach \\i in {5,4,...,2}{% This one doesn't matter
    \\foreach \\j in {3,2,...,0}{% This will crate a membrane
                               % with the front lipids visible
      % top layer
      \\pgfmathsetmacro{\\dx}{rand*0.1}% A random variance in the x coordinate
      \\pgfmathsetmacro{\\dy}{rand*0.1}% A random variance in the y coordinate,
                                     % gives a hight fill to the lipid
      \\pgfmathsetmacro{\\rot}{rand*0.1}% A random variance in the
                                      % molecule orientation
      \\shade[ball color=red] ({\\i+\\dx+\\rot},{0.5*\\j+\\dy+0.4*sin(\\i*\\nuPi*10)}) circle(0.45);
      \\shade[ball color=gray] (\\i+\\dx,{0.5*\\j+\\dy+0.4*sin(\\i*\\nuPi*10)-0.9}) circle(0.45);
      \\shade[ball color=gray] (\\i+\\dx-\\rot,{0.5*\\j+\\dy+0.4*sin(\\i*\\nuPi*10)-1.8}) circle(0.45);
      % bottom layer
      \\pgfmathsetmacro{\\dx}{rand*0.1}
      \\pgfmathsetmacro{\\dy}{rand*0.1}
      \\pgfmathsetmacro{\\rot}{rand*0.1}
      \\shade[ball color=gray] (\\i+\\dx+\\rot,{0.5*\\j+\\dy+0.4*sin(\\i*\\nuPi*10)-2.8}) circle(0.45);
      \\shade[ball color=gray] (\\i+\\dx,{0.5*\\j+\\dy+0.4*sin(\\i*\\nuPi*10)-3.7}) circle(0.45);
      \\shade[ball color=red] (\\i+\\dx-\\rot,{0.5*\\j+\\dy+0.4*sin(\\i*\\nuPi*10)-4.6}) circle(0.45);
    }
  }
\\end{tikzpicture}

% Test minted (shell commands)
\\begin{minted}{python}
x = 1 + 2
\\end{minted}

% Test bashful (shell commands)
\\bash[stdout,stderr]
date
\\END

% Test system
\\immediate\\write18{/bin/date > date.txt}
\\input date.txt

% Test popen
\\input{"|date"}

\\end{document}`

// DefaultURLFor builds the Node-equivalent URL:
// http://<host>:<port>/project/smoketest-<pid>/compile.
func DefaultURLFor(host string, port int) string {
	return fmt.Sprintf("http://%s:%d/project/smoketest-%d/compile",
		host, port, os.Getpid())
}

// SmokeTest mirrors the Node SmokeTests module: state + methods.
type SmokeTest struct {
	cfg        *config.Config
	compileURL string
	timeout    time.Duration

	mu        sync.Mutex
	lastError error
}

// NewErrPending is the initial "no run yet" error (Node:
// _lastError = new Error('SmokeTestsPending')).
var NewErrPending = errors.New("SmokeTestsPending")

// New builds a SmokeTest wired to cfg's host/port (compile URL derived from
// ProcessId + os.Getpid()).
func New(cfg *config.Config) *SmokeTest {
	return &SmokeTest{
		cfg:        cfg,
		compileURL: DefaultURLFor(cfg.Internal.Compile.Host, cfg.Internal.Compile.Port),
		timeout:    DefaultTimeout,
		lastError:  NewErrPending,
	}
}

// SetCompileURL overrides the compile URL (tests, and main which needs the
// actual listening host/port when LISTEN_ADDRESS is not loopback).
func (s *SmokeTest) SetCompileURL(url string) { s.compileURL = url }

// SetTimeout overrides the per-compile timeout (tests).
func (s *SmokeTest) SetTimeout(d time.Duration) { s.timeout = d }

// compileEnvelope is the subset of compilecontroller's wire envelope
// (buildCompileBody, wire.go) that Run() validates.
type compileEnvelope struct {
	Status      string `json:"status"`
	BuildID     string `json:"buildId"`
	OutputFiles []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"outputFiles"`
}

// Run ports _run(): POST the smoke compile and validate that the response
// carries a pdf and a log output file. Returns nil on success.
func (s *SmokeTest) Run(ctx context.Context) error {
	client := &http.Client{Timeout: s.timeout}
	body := map[string]any{
		"compile": map[string]any{
			"options":   map[string]any{"metricsPath": "health-check"},
			"resources": []any{map[string]any{"path": "main.tex", "content": mainTex}},
		},
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("smoke test: marshalling request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.compileURL,
		bytes.NewReader(bodyJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("smoke test: POST %s: %w", s.compileURL, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("smoke test: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("smoke test: compile endpoint returned %d: %s",
			resp.StatusCode, snippet(data))
	}
	// Node: !body || !body.compile.outputFiles -> 'response payload incomplete'.
	var env compileEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("smoke test: response payload incomplete: %w", err)
	}
	if env.Status == "" || len(env.OutputFiles) == 0 {
		return errors.New("response payload incomplete")
	}
	pdfFound, logFound := false, false
	for _, f := range env.OutputFiles {
		if f.Type == "pdf" {
			pdfFound = true
		}
		if f.Type == "log" {
			logFound = true
		}
	}
	if !pdfFound {
		return errors.New("no pdf returned")
	}
	if !logFound {
		return errors.New("no log returned")
	}
	return nil
}

// LastRunSuccessful ports lastRunSuccessful(): true iff the last run ended
// without error.
func (s *SmokeTest) LastRunSuccessful() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastError == nil
}

// LastError mirrors the Node _lastError field (initial value NewErrPending).
func (s *SmokeTest) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastError
}

// SendLastResult ports sendLastResult: respond with the cached last error
// (used by /health_check).
func (s *SmokeTest) SendLastResult(w http.ResponseWriter) {
	err := s.LastError()
	s.sendResponse(w, err)
}

// SendNewResult ports sendNewResult: run the smoke compile and respond with
// its outcome (used by /smoke_test_force).
func (s *SmokeTest) SendNewResult(w http.ResponseWriter) {
	err := s.Run(context.Background())
	s.setLastError(err)
	s.sendResponse(w, err)
}

// TriggerRun ports triggerRun(cb): run in the background, update the
// cached last error, invoke cb exactly once with the outcome.
func (s *SmokeTest) TriggerRun(cb func(error)) {
	go func() {
		err := s.Run(context.Background())
		s.setLastError(err)
		if cb != nil {
			cb(err)
		}
	}()
}

func (s *SmokeTest) setLastError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Node: .then(() => this._lastError = null) — nil on success.
	if err == nil {
		s.lastError = nil
	} else {
		s.lastError = err
	}
}

// sendResponse mirrors Node's _sendResponse: text/plain, 500 + error message
// or 200 + "OK".
func (s *SmokeTest) sendResponse(w http.ResponseWriter, err error) {
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(500)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(200)
	_, _ = w.Write([]byte("OK"))
}

func snippet(data []byte) string {
	if len(data) > 200 {
		return string(data[:200])
	}
	return string(data)
}
