// outputfile.go: GET .../build/{build_id}/output/{filename} file serving.
//
// Why this route exists (production catch 2026-10-06 "still broken" #2): the
// absorbed output URLs the compile response advertises
//
//	/project/{pid}[/{user}/{uid}]/build/{bid}/output/output.pdf|log|blg|...
//
// are fetched by the editor's PDF/error panes. Go web
// (features/compile/outputfile_web.go) proxies them to this CLSI service, but
// the Go port (2026-09-29) absorbed only the .../output/output.zip routes, so
// every editor output fetch 404'd even when the build's files existed on the
// shared data volume (nginx evidence 2026-10-06 19:36-19:43: output.pdf on
// disk, HTTP 404 to the owner). clsitypst closed the identical gap on its side
// (apps.go buildFile + compilemanager/outputfile.go, typst-t2 2026-10-05);
// this is the 1:1 tex-side port.
//
// Contract (Node express res.sendFile parity, best-effort — single-deployment
// port, same as the typst sibling):
//
//	path must stay inside the resolved build dir after clean (no traversal)
//	regular file present  -> 200 + body (Content-Type by extension;
//	                           http.ServeFile gives Range support like
//	                           express sendFile)
//	missing / traversal / empty -> NotFoundError (apps error middleware
//	                               renders the 404)
package compilemanager

import (
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	cerrors "ollitex/go/services/clsitex/errors"
)

func newNotFoundError(msg string) error { return cerrors.NewNotFoundError(msg) }

// ServeBuildFile resolves the build dir (buildID set -> the
// <name>/generated-files/<build>/ content dir SaveOutputFiles copies to, same
// layout synctex.go uses for the output-dir synctex run; empty -> the compile
// dir where the runner writes in place) and streams the requested file.
// Returns (200, nil) after the body is written, or (0, error) for the apps
// error middleware to render.
func (m *Manager) ServeBuildFile(w http.ResponseWriter, r *http.Request,
	projectID, userID, buildID, fn string) (int, error) {
	if fn == "" {
		return 0, newNotFoundError("file parameter is required")
	}
	rel := strings.ReplaceAll(fn, "\\", "/")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || rel == "." || rel == ".." {
		return 0, newNotFoundError("path not allowed")
	}
	rel = path.Clean(rel)

	dir := m.buildContentDir(projectID, userID, buildID)
	target := filepath.Join(dir, filepath.FromSlash(rel))

	// Containment: the joined path must stay inside dir (authoritative check
	// after the lexical clean).
	relAbs, err := filepath.Rel(dir, target)
	if err != nil || relAbs == ".." ||
		strings.HasPrefix(relAbs, ".."+string(filepath.Separator)) ||
		relAbs == string(filepath.Separator) {
		return 0, newNotFoundError("path not allowed")
	}

	fi, err := os.Stat(target)
	if err != nil || !fi.Mode().IsRegular() {
		return 0, newNotFoundError("output file not found")
	}

	if ct := mime.TypeByExtension(filepath.Ext(target)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeFile(w, r, target)
	return http.StatusOK, nil
}

// buildContentDir mirrors sidecarDir resolution on the typst side:
// buildID non-empty -> <OutputDir>/<name>/generated-files/<buildID>/ (the
// ocm.SaveOutputFiles copy land); empty -> the compile dir.
func (m *Manager) buildContentDir(projectID, userID, buildID string) string {
	if buildID == "" {
		return compileDirOf(m.Paths.CompilesDir, projectID, userID)
	}
	return filepath.Join(m.Paths.OutputDir, compileName(projectID, userID), "generated-files", buildID)
}
