package compilemanager

import (
	"context"
	"errors"
	"testing"

	commandrunner "ollitex/go/services/clsitex/commandrunner"
	clserrors "ollitex/go/services/clsitex/errors"
	histwriter "ollitex/go/services/clsitex/historyresourcewriter"
	latexrunner "ollitex/go/services/clsitex/latexrunner"
	off "ollitex/go/services/clsitex/outputfilefinder"
	"ollitex/go/services/clsitex/resourcewriter"
)

func makeRWReq() *Request { return makeReq() }

// runLatexWith overrides the RunLatex seam: optionally seed stats, then
// deliver (err, out) through the callback (mirrors Promises.runLatex).
func runLatexWith(m *Manager, err error, seed func(stats map[string]any)) {
	m.RunLatex = func(name string, opts latexrunner.Options,
		cb func(err error, out *commandrunner.RunOutput)) {
		if seed != nil {
			seed(opts.Stats)
		}
		if err != nil {
			cb(err, nil)
			return
		}
		cb(nil, &commandrunner.RunOutput{Stdout: "compile ok", ExitCode: 0})
	}
}

func pdfFile(sz int64) off.OutputFile {
	s := sz
	return off.OutputFile{Path: "output.pdf", Type: "pdf", Size: &s, Build: "b1"}
}

// --- doCompile sync-phase failures (failSync: log + original error) -----------

func TestDoCompileSyncFailures(t *testing.T) {
	cases := []struct {
		name string
		mut  func(m *Manager, req *Request)
	}{
		{"rw-plain", func(m *Manager, req *Request) {
			m.ResourceSync = func(r *resourcewriter.Request, base string) ([]resourcewriter.Resource, error) {
				return nil, errors.New("rw boom")
			}
		}},
		{"rw-fos", func(m *Manager, req *Request) {
			m.ResourceSync = func(r *resourcewriter.Request, base string) ([]resourcewriter.Resource, error) {
				return nil, clserrors.NewFilesOutOfSyncError("fos")
			}
		}},
		{"hrw-error", func(m *Manager, req *Request) {
			req.IsCompileFromHistory = true
			m.HistorySync = func(ctx context.Context, pid, uid string, r *histwriter.Request,
				base string, ti, st map[string]any) (*histwriter.Result, error) {
				return nil, errors.New("hrw boom")
			}
		}},
		{"draft-error", func(m *Manager, req *Request) {
			req.Draft = true
			m.DraftInject = func(filename string) error { return errors.New("draft boom") }
		}},
		{"tikz-check-error", func(m *Manager, req *Request) {
			m.TikzCheck = func(c, mf string, r []resourcewriter.Resource) (bool, error) {
				return false, errors.New("tikz boom")
			}
		}},
		{"tikz-inject-error", func(m *Manager, req *Request) {
			m.TikzCheck = func(c, mf string, r []resourcewriter.Resource) (bool, error) {
				return true, nil
			}
			m.TikzInject = func(c, mf string) error { return errors.New("tikz inject boom") }
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager(t, t.TempDir())
			req := makeReq()
			tc.mut(m, req)
			if _, err := m.DoCompileWithLock(req, map[string]any{}, map[string]any{}); err == nil {
				t.Fatal("expected sync-phase error")
			}
		})
	}
}
