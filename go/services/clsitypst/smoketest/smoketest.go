// Package smoketest ports the clsi_tex smoke-test behavior (clsi.go
// smoketest) for clsi_typst.go — env-gated SMOKE_TEST (D12 lean-Y) so the
// deployed binary proves end-to-end that the Docker typst image compiles.
//
// Typst porting (documented divergence from the tex smoketest):
//
//   - The tex smoke compiles a main.tex and validates outputFiles has a "pdf"
//     AND a "log" entry. Typst has no .log analogue (the D16 success gate is
//     output.pdf size>0), so Run() validates exactly that outputFiles carries
//     an output.pdf (type "pdf"). This mirrors the Node typst baseline (which
//     ports the clsi smoke, §3.3).
//   - The wire path for the compile is the typst controller's TOP-LEVEL
//     {compile: {status, outputFiles, ...}} envelope (clsi_typst wire.go,
//     the clsi shape minus clsiCacheShard). Run() reads body.compile.*.
//   - Node's fetchJson has no explicit timeout; Go uses an injectable
//     timeout (default DefaultTimeout = 5 min) — a cold Docker pull/compile
//     can be slow on first boot.
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

	"ollitex/go/services/clsitypst/config"
)

// DefaultTimeout is the default per-compile budget (see package doc).
const DefaultTimeout = 5 * time.Minute

// mainTyp is a minimal Typst document (the D12 "hello.typ" smoke).
const mainTyp = `#let hello = "Hello from clsi_typst smoke test"

${hello}$

Compiled by the clsi_typst smoke test.
`

// NewErrPending is the initial "no run yet" error.
var NewErrPending = errors.New("SmokeTestsPending")

// SmokeTest mirrors the tex SmokeTests module: state + methods.
type SmokeTest struct {
	cfg        *config.Config
	compileURL string
	timeout    time.Duration

	mu        sync.Mutex
	lastError error
}

// DefaultURLFor builds the compile URL: http://<host>:<port>/project/
// smoketest-<pid>/compile (pid is os.Getpid, mirroring the tex DefaultURLFor).
func DefaultURLFor(host string, port int) string {
	return fmt.Sprintf("http://%s:%d/project/smoketest-%d/compile",
		host, port, os.Getpid())
}

// New builds a SmokeTest wired to cfg's host/port (compile URL derived from
// os.Getpid on the default port).
func New(cfg *config.Config) *SmokeTest {
	return &SmokeTest{
		cfg:        cfg,
		compileURL: DefaultURLFor(cfg.Internal.Host, cfg.Internal.Port),
		timeout:    DefaultTimeout,
		lastError:  NewErrPending,
	}
}

// SetCompileURL overrides the compile URL (tests + main when LISTEN_ADDRESS
// is not loopback).
func (s *SmokeTest) SetCompileURL(url string) { s.compileURL = url }

// SetTimeout overrides the per-compile timeout (tests).
func (s *SmokeTest) SetTimeout(d time.Duration) { s.timeout = d }

// compileEnvelope is the subset of the typst compile wire envelope that
// Run() validates: the TOP-LEVEL {compile: {...}} envelope (wire.go).
type compileEnvelope struct {
	Compile struct {
		Status      string `json:"status"`
		OutputFiles []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"outputFiles"`
	} `json:"compile"`
}

// Run ports Run(): POST the smoke compile and validate that outputFiles
// carries an output.pdf. Returns nil on success.
func (s *SmokeTest) Run(ctx context.Context) error {
	client := &http.Client{Timeout: s.timeout}
	body := map[string]any{
		"compile": map[string]any{
			"options":   map[string]any{"compiler": "typst", "metricsPath": "health-check"},
			"resources": []any{map[string]any{"path": "main.typ", "content": mainTyp}},
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
	var env compileEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("smoke test: response payload incomplete: %w", err)
	}
	if env.Compile.Status == "" || len(env.Compile.OutputFiles) == 0 {
		return errors.New("response payload incomplete")
	}
	pdfFound := false
	for _, f := range env.Compile.OutputFiles {
		if f.Type == "pdf" || f.Path == "output.pdf" {
			pdfFound = true
		}
	}
	if !pdfFound {
		return errors.New("no pdf returned")
	}
	return nil
}

// LastRunSuccessful: true iff the last run ended without error.
func (s *SmokeTest) LastRunSuccessful() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastError == nil
}

// LastError: the cached last error (initial value NewErrPending).
func (s *SmokeTest) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastError
}

// SendLastResult: respond with the cached last error (used by /health_check).
func (s *SmokeTest) SendLastResult(w http.ResponseWriter) {
	err := s.LastError()
	s.sendResponse(w, err)
}

// SendNewResult: run the smoke compile now and respond with its outcome
// (used by /smoke_test_force).
func (s *SmokeTest) SendNewResult(w http.ResponseWriter) {
	err := s.Run(context.Background())
	s.setLastError(err)
	s.sendResponse(w, err)
}

// TriggerRun: run in the background, update the cached last error, invoke
// cb exactly once with the outcome.
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
	if err == nil {
		s.lastError = nil
	} else {
		s.lastError = err
	}
}

// sendResponse: text/plain, 500 + error message or 200 + "OK".
func (s *SmokeTest) sendResponse(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/plain")
	if err != nil {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	w.WriteHeader(200)
	_, _ = w.Write([]byte("OK"))
}

func snippet(data []byte) string {
	if len(data) > 200 {
		return string(data[:200])
	}
	return string(data)
}
