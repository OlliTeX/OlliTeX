package compilemanager

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// outputfile_test.go pins the shared-cls file-serving surface
// (ServeBuildFile) that serves the editor output-URL fetches
// (GET .../build/{bid}/output/{file}, apps/server.go). The production
// catch that motivated this port (2026-10-06): the route was missing and
// the editor's output.pdf fetch 404'd while the file sat on the shared
// volume under <OutputDir>/<pid>-<uid>/generated-files/<bid>/.

func outputfileTestMgr(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	m := &Manager{Paths: Paths{
		CompilesDir: filepath.Join(root, "compiles"),
		OutputDir:   filepath.Join(root, "output"),
	}}
	return m, root
}

func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("BODY-"+name), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestServeBuildFile_Found(t *testing.T) {
	m, root := outputfileTestMgr(t)
	buildDir := filepath.Join(root, "output", "p1-u1", "generated-files", "b-1")
	writeFile(t, buildDir, "output.pdf")

	r := httptest.NewRequest(http.MethodGet, "/project/p1/user/u1/build/b-1/output/output.pdf", nil)
	w := httptest.NewRecorder()
	code, err := m.ServeBuildFile(w, r, "p1", "u1", "b-1", "output.pdf")
	if err != nil {
		t.Fatalf("ServeBuildFile: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("Content-Type = %q, want application/pdf", ct)
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != "BODY-output.pdf" {
		t.Fatalf("body = %q", body)
	}
}

func TestServeBuildFile_NoUserMount(t *testing.T) {
	m, root := outputfileTestMgr(t)
	buildDir := filepath.Join(root, "output", "p1", "generated-files", "b-2")
	writeFile(t, buildDir, "output.log")

	w := httptest.NewRecorder()
	code, err := m.ServeBuildFile(w, httptest.NewRequest(http.MethodGet, "/x", nil), "p1", "", "b-2", "output.log")
	if err != nil || code != http.StatusOK {
		t.Fatalf("serve: code=%d err=%v", code, err)
	}
}

func TestServeBuildFile_Errors(t *testing.T) {
	m, _ := outputfileTestMgr(t)

	cases := []struct {
		name    string
		proj, us string
		build   string
		fn      string
	}{
		{"empty-file", "p1", "u1", "b-1", ""},
		{"dot", "p1", "u1", "b-1", "."},
		{"dotdot", "p1", "u1", "b-1", ".."},
		{"traversal", "p1", "u1", "b-1", "../escape.pdf"},
		{"missing", "p1", "u1", "b-1", "output.pdf"},
		{"missing-build", "p1", "u1", "nope", "output.pdf"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			code, err := m.ServeBuildFile(w, httptest.NewRequest(http.MethodGet, "/x", nil), c.proj, c.us, c.build, c.fn)
			if code != 0 || err == nil {
				t.Fatalf("code=%d err=%v, want (0, NotFound)", code, err)
			}
			if w.Body.Len() != 0 {
				t.Fatal("body must not be written on the error arms")
			}
		})
	}
}

func TestServeBuildFile_DirectoryRejected(t *testing.T) {
	m, root := outputfileTestMgr(t)
	buildDir := filepath.Join(root, "output", "p1-u1", "generated-files", "b-1")
	if err := os.MkdirAll(buildDir, 0o777); err != nil {
		t.Fatal(err)
	}
	writeFile(t, buildDir, "output.pdf")

	// The build dir itself (or any subdirectory) is not a regular file.
	w := httptest.NewRecorder()
	if code, err := m.ServeBuildFile(w, httptest.NewRequest(http.MethodGet, "/x", nil), "p1", "u1", "b-1", ""); err == nil {
		t.Fatalf("code=%d, want error", code)
	}
}
