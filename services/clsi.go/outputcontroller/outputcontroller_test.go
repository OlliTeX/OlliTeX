package outputcontroller_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clsi/config"
	"clsi/outputcontroller"
	"clsi/outputfilearchivemanager"
	"clsi/outputfilefinder"
)

// TestMain sets the sandbox env required by config.ForTest and wires the
// production OutputFileFinder walk into the archive manager (mirroring
// Node's module-level import).
func TestMain(m *testing.M) {
	os.Setenv("SANDBOXED_COMPILES_HOST_DIR_COMPILES", "/tmp/c")
	os.Setenv("SANDBOXED_COMPILES_HOST_DIR_CACHE", "/tmp/nc")
	os.Setenv("SANDBOXED_COMPILES_HOST_DIR_OUTPUT", "/tmp/o")
	// Production wiring: archive manager probes via the real OutputFileFinder.
	outputfilearchivemanager.AssignFind(func(args []string, dir string) ([]outputfilearchivemanager.OutputFile, error) {
		res, err := outputfilefinder.FindOutputFiles(nil, dir)
		if err != nil {
			// Node: _getAllOutputFiles maps ENOENT/ENOTDIR/EACCES -> NotFound.
			if pe, ok := err.(*os.PathError); ok {
				switch pe.Err.Error() {
				case "file not found", "not a directory":
					return nil, outputfilearchivemanager.NotFoundError("Output files not found")
				}
			}
			return nil, err
		}
		out := make([]outputfilearchivemanager.OutputFile, 0, len(res.OutputFiles))
		for _, f := range res.OutputFiles {
			out = append(out, outputfilearchivemanager.OutputFile{Path: f.Path})
		}
		return out, nil
	})
	config.ForTest()
	os.Exit(m.Run())
}

func seedOutputDir(t *testing.T, pid, build string) (string, string) {
	outputDir := t.TempDir()
	contentDir := outputfilearchivemanager.GetContentDir(outputDir, pid, "")
	if err := os.MkdirAll(filepath.Join(contentDir, build), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return outputDir, contentDir + build + "/"
}

func TestCreateOutputZipHappy(t *testing.T) {
	outputDir, buildDir := seedOutputDir(t, "p1", "b1")
	if err := os.WriteFile(filepath.Join(buildDir, "body.pdf"), []byte("PDFDATA"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec := httptest.NewRecorder()
	status, err := outputcontroller.CreateOutputZip(rec, outputDir, outputcontroller.Request{ProjectID: "p1", BuildID: "b1"})
	if err != nil {
		t.Fatalf("CreateOutputZip happy: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	h := rec.Result()
	if ct := h.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := h.Header.Get("Content-Disposition"); cd != `attachment; filename="output.zip"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if x := h.Header.Get("X-Content-Type-Options"); x != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", x)
	}
	if cl := h.Header.Get("Content-Length"); cl == "" {
		t.Error("missing Content-Length")
	}
	if len(rec.Body.Bytes()) == 0 {
		t.Error("empty body on happy zip")
	}
	// Body must be a valid zip (PK magic).
	body := rec.Body.Bytes()
	if len(body) < 4 || !strings.HasPrefix(string(body[0:4]), "PK\x03\x04") {
		t.Errorf("body is not a zip: first bytes = %v", body[0:4])
	}
}

func TestCreateOutputZipServerError(t *testing.T) {
	outputDir, _ := seedOutputDir(t, "p1", "b1")
	// Force a non-NotFound error from Find: the archive manager's
	// OpenErrorToNotFound only maps NotFoundError to 404; anything else
	// propagates and CreateOutputZip returns (500, err).
	real := outputfilearchivemanager.Find
	outputfilearchivemanager.Find = func(args []string, dir string) ([]outputfilearchivemanager.OutputFile, error) {
		return nil, errors.New("unexpected io failure (not NotFound)")
	}
	defer func() { outputfilearchivemanager.Find = real }()

	rec := httptest.NewRecorder()
	status, err := outputcontroller.CreateOutputZip(rec, outputDir, outputcontroller.Request{ProjectID: "p1", BuildID: "b1"})
	if err == nil {
		t.Fatal("want non-NotFound error propagated")
	}
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
}

type brokenWriter struct {
	http.ResponseWriter
}

func (b brokenWriter) Write(p []byte) (int, error) { return 0, errors.New("broken write") }

func TestCreateOutputZipWriteError(t *testing.T) {
	outputDir, buildDir := seedOutputDir(t, "p1", "b1")
	if err := os.WriteFile(filepath.Join(buildDir, "body.pdf"), []byte("PDFDATA"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Wrap a real recorder in one whose Write returns an error so the
	// CreateOutputZip's res.Write error branch (line 79-80) executes.
	base := httptest.NewRecorder()
	var res http.ResponseWriter = brokenWriter{base}
	status, err := outputcontroller.CreateOutputZip(res, outputDir, outputcontroller.Request{ProjectID: "p1", BuildID: "b1"})
	if err == nil {
		t.Fatal("want write error propagated")
	}
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
}

func TestCreateOutputZipMissingBuildIs404(t *testing.T) {
	outputDir, _ := seedOutputDir(t, "p1", "b1")
	// A build that has no content dir: FindOutputFiles' walkFolder returns
	// *os.PathError (ENOENT) -> my adapter maps to NotFoundError -> 404.
	rec := httptest.NewRecorder()
	status, err := outputcontroller.CreateOutputZip(rec, outputDir, outputcontroller.Request{ProjectID: "p1", BuildID: "does-not-exist"})
	if err != nil {
		t.Fatalf("CreateOutputZip missing build should not return error: %v", err)
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("404 must not write a body (Node emits a JSON error separately)")
	}
}
