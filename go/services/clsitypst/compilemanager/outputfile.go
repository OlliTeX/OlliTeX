// outputfile.go: GET .../build/{build_id}/output/{filename} file serving.
//
// Why this route exists (the Go-world gap closed 2026-10-05, M1 parity /
// typst-t2 live): in the Node clsi_typst deployment the absorbed output
// URLs that the compile response advertises
//
//	/project/{pid}[/{user}/{uid}]/build/{bid}/output/output.pdf
//
// were served by the SHARED clsi (tex) service on the common data volume
// (the clsi_typst app.js registers only output.zip). In the single
// Go deployment there is NO tex clsi: the compiled artifacts live in THIS
// service's OUTPUT dir (Ocm.SaveOutputFiles copy land:
// <name>/generated-files/<build>/), so this service must serve the files
// itself or the web proxy (Route A / getFileFromClsi shapes) 404s and the
// editor's PDF pane never loads (typst-t2 "PDF artifact not ready").
//
// Contract (Node express res.sendFile parity, best-effort — this is a
// single-deployment port, not a locked wire test):
//
//	path must stay inside the resolved build dir after clean (no traversal)
//	regular file present  -> 200 + body (Content-Type by extension;
//	                           http.ServeFile gives Range support like
//	                           express sendFile)
//	missing / traversal / empty -> NotFoundError (finish renders 404)
package compilemanager

import (
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ServeBuildFile resolves the build dir (same sidecarDir resolution the
// sync routes use — buildID set → generated-files build dir; empty → the
// compile dir where the fork writes output.pdf + sidecar) and streams the
// requested file. Returns (200, nil) after the body is written, or
// (0, error) for finish to render.
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
	if path.Clean(rel) != rel {
		// contains .. or . segments — resolve and re-check containment
		// anyway (the Rel check below is authoritative).
		rel = path.Clean(rel)
	}

	dir := m.sidecarDir(projectID, userID, buildID)
	target := filepath.Join(dir, filepath.FromSlash(rel))

	// Containment: the joined path must stay inside dir (TOCTOU-safe
	// double check after the lexical clean).
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
