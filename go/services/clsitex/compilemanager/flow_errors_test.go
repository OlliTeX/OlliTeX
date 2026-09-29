package compilemanager

import (
	"errors"
	"testing"

	commandrunner "ollitex/go/services/clsitex/commandrunner"
	dockerrunner "ollitex/go/services/clsitex/dockerrunner"
	latexrunner "ollitex/go/services/clsitex/latexrunner"
)

// runLatexErr overrides the RunLatex seam to deliver a single error through
// the callback (the compile phase's failure paths).
func runLatexErr(m *Manager, err error) {
	m.RunLatex = func(name string, opts latexrunner.Options,
		cb func(err error, out *commandrunner.RunOutput)) {
		cb(err, nil)
	}
	_ = err
}

// --- runCompile error classification (classifyRunError + remaps) ---------------

func TestRunCompileErrorMatrix(t *testing.T) {
	cases := []struct {
		name   string
		runErr error
		check  string
		want   func(e *CompileRunError) bool
	}{
		{"terminated", &dockerrunner.TerminatedError{}, "", func(e *CompileRunError) bool {
			return e.Terminated
		}},
		{"timedout", &dockerrunner.TimedOutError{}, "", func(e *CompileRunError) bool {
			return e.TimedOut
		}},
		{"exited-code-17", &dockerrunner.ExitedError{Code: 17}, "", func(e *CompileRunError) bool {
			return e.Code != nil && *e.Code == "17"
		}},
		{"epipe-code", errors.New("EPIPE read closed"), "", func(e *CompileRunError) bool {
			// Node: docker EPIPE — the controller string-matches 'EPIPE'.
			// Go classify only sets flags; verify plain propagation:
			return e.Message == "EPIPE read closed"
		}},
		{"validate-no-code", errors.New("chktex clean"), "validate", func(e *CompileRunError) bool {
			return e.Message == "validation" && e.Validate != nil && *e.Validate == "pass"
		}},
		{"validate-with-exit-1", &dockerrunner.ExitedError{Code: 1}, "validate", func(e *CompileRunError) bool {
			return e.Message == "validation" && e.Validate != nil && *e.Validate == "fail"
		}},
		{"error-check-exited", &dockerrunner.ExitedError{Code: 1}, "error", func(e *CompileRunError) bool {
			return e.Message == "compilation" && e.Validate != nil && *e.Validate == "fail"
		}},
		{"error-check-plain", errors.New("boom"), "error", func(e *CompileRunError) bool {
			return e.Message == "boom" && e.Validate == nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager(t, t.TempDir())
			req := makeReq()
			if tc.check != "" {
				ck := tc.check
				req.Check = &ck
			}
			runLatexErr(m, tc.runErr)
			_, derr := m.DoCompileWithLock(req, map[string]any{}, map[string]any{})
			var ce *CompileRunError
			if !errors.As(derr, &ce) {
				t.Fatalf("expected *CompileRunError, got: %v", derr)
			}
			if ce.OutputFiles == nil || ce.BuildID == nil {
				t.Fatalf("error payload not attached: %+v", ce)
			}
			if !tc.want(ce) {
				t.Fatalf("classification mismatch: %+v (err=%v)", ce, ce.Error())
			}
		})
	}
}

// --- validate check on success (synthesized pass error) ------------------------

func TestRunCompileValidatePass(t *testing.T) {
	m := newTestManager(t, t.TempDir())
	req := makeReq()
	ck := "validate"
	req.Check = &ck
	m.RunLatex = func(name string, opts latexrunner.Options,
		cb func(err error, out *commandrunner.RunOutput)) {
		cb(nil, &commandrunner.RunOutput{Stdout: "ok"})
	}
	_, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{})
	var ce *CompileRunError
	if !errors.As(err, &ce) {
		t.Fatalf("expected validation error, got: %v", err)
	}
	if ce.Message != "validation" || ce.Validate == nil || *ce.Validate != "pass" {
		t.Fatalf("validate pass mismatch: %+v", ce)
	}
}
