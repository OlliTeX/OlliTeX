// synctex.go ports the clsi syncFromCode / syncFromPdf surface (D21 part 2,
// T16) onto the T15 fork `output.sourcemap.json` sidecar.
//
// clsi (TeX) runs `synctex view` / `synctex edit` in the container against
// output.synctex.gz (-i <compileDir>/<file> -o <compileDir>/output.pdf); the
// Typst port reads the T15 fork sidecar (written next to output.pdf during
// `typst compile`) and does matching in Go (reader: the sourcemap package,
// total).
//
// Coordinate basis (LOCKED, T16 session, verified against the vendored
// frontend services/web/frontend/js/features/pdf-preview/util/highlights.ts
// + the T15 patch + a live fork sidecar): Location {x,y,w,h} is a bounding
// box in PDF user space (points, origin at the page's bottom-left, y up).
// The clsi view record (flat synctex map per clsi.go synctexparser) uses the
// SAME basis, so it is a 1:1 rename + the 0->1 page bump:
//
//	view = {"page": loc.Page+1, "h": loc.X, "v": loc.Y,
//	         "width": loc.W, "height": loc.H}
//
// For edit (sync/pdf), the frontend click v = `pageHeight - offset.top`
// (distance from the page bottom up to the box TOP) — i.e. (h, v) is the
// clicked text element's TOP-LEFT corner in user space. The nearest box is
// matched per page by distance from (h, v) to each box's CENTER: min box of
// (h - (loc.X + loc.W/2))^2 + (v - (loc.Y + loc.H/2))^2.
//
// Its span resolves to {file, line, column} via byteOffset -> LineColAt on
// the file content (Node's synctex edit does the same resolution against
// .synctex).
package compilemanager

import (
	"os"
	"path/filepath"
	"regexp"

	clerrors "ollitex/go/services/clsitypst/errors"
	so "ollitex/go/services/clsitypst/sourcemap"
)

// SyncOpts mirrors clsi compilemanager.SyncOpts. Only BuildID drives
// behavior here (build-scoped sidecar dir); the rest are accepted for wire
// parity and ignored (the Typst sync route does not select a docker image
// per request, so ImageName is deliberately NOT validated — see lock).
type SyncOpts struct {
	ImageName            string
	EditorID             string
	BuildID              string
	CompileFromClsiCache bool
}

// SyncFromCodeResult mirrors the syncFromCode return {codePositions,
// downloadedFromCache}; the controller renames CodePositions to "pdf" on
// the wire (clsi shape).
type SyncFromCodeResult struct {
	CodePositions       []map[string]any
	DownloadedFromCache bool
}

// SyncFromPdfResult mirrors the syncFromPdf return {pdfPositions,
// downloadedFromCache}; the controller renames PdfPositions to "code".
type SyncFromPdfResult struct {
	PdfPositions        []map[string]any
	DownloadedFromCache bool
}

// --- D21: not-compiled error (added error kind, rendered 405) --------------

// NotCompiledError: the project has no compiled output.pdf, so the sync
// routes cannot serve a location. Rendered 405 (empty body) by the apps
// finish arm (arm ADDED to the LOCKED order, see apps doc).
type NotCompiledError struct{ Message string }

func (e *NotCompiledError) Error() string { return e.Message }

func newNotCompiledError(projectID string) error {
	return &NotCompiledError{Message: "project not compiled: " + projectID}
}

// IsNotCompiled reports whether err is a *NotCompiledError (the finish arm).
func IsNotCompiled(err error) bool {
	_, ok := err.(*NotCompiledError)
	return ok
}

var (
	// Node: /^[a-f0-9-]+$/
	syncEditorIDRegexp = regexp.MustCompile(`^[a-f0-9-]+$`)
	// clsi OutputCacheManager.BUILD_REGEX /^[0-9a-f]+-[0-9a-f]+$/.
	syncBuildIDRegexp = regexp.MustCompile(`^[0-9a-f]+-[0-9a-f]+$`)
)

// validateSyncOpts (clsi runSynctex parity): editorId / buildId shape check.
func validateSyncOpts(opts SyncOpts) error {
	if opts.EditorID != "" && !syncEditorIDRegexp.MatchString(opts.EditorID) {
		return &clerrors.InvalidParameter{Message: "invalid editorId"}
	}
	if opts.BuildID != "" && !syncBuildIDRegexp.MatchString(opts.BuildID) {
		return &clerrors.InvalidParameter{Message: "invalid buildId"}
	}
	return nil
}

// sidecarDir resolves the directory that holds the compiled output for
// this request: buildId set -> the generated-files build dir (the T11
// productionFind copy land, same <name>/generated-files/<build>/ layout
// ocm.SaveOutputFiles uses); buildId empty -> the compile dir (writer geo:
// the fork writes output.pdf AND the sidecar next to it there).
func (m *Manager) sidecarDir(projectID, userID, buildID string) string {
	if buildID == "" {
		return compileDirOf(m.Paths.CompilesDir, projectID, userID)
	}
	name := compileName(projectID, userID)
	return filepath.Join(m.Paths.OutputDir, name, "generated-files", buildID)
}

// loadSidecar (plan error arms, ordered):
//  1. output.pdf missing in the resolved dir -> NotCompiledError (405)
//  2. output.sourcemap.json missing there    -> NotFoundError "sourcemap not
//     available" (404, the LOCKED plan text)
//
// (The writer always emits both after a successful compile; "pdf present,
// sidecar missing" is a corrupted / pre-fork output -> the 404 arm.)
func (m *Manager) loadSidecar(dir, projectID string) (*so.Sidecar, error) {
	fi, err := os.Stat(filepath.Join(dir, "output.pdf"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, newNotCompiledError(projectID)
		}
		return nil, &clerrors.OError{Message: err.Error()}
	}
	if !fi.Mode().IsRegular() {
		return nil, newNotCompiledError(projectID)
	}
	data, err := os.ReadFile(filepath.Join(dir, "output.sourcemap.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, newNotFoundError("sourcemap not available")
		}
		return nil, &clerrors.OError{Message: err.Error()}
	}
	return so.Parse(data)
}

// --- syncFromCode -------------------------------------------------------------

// SyncFromCode ports syncFromCode: (file, line, column) -> the nearest
// source-map location for that byte offset -> view records {page, h, v,
// width, height}. No matched location -> 200 with an empty record array
// (the clsi synctex binary returns an empty parse list the same way).
// downloadedFromCache: always false (the Typst sync routes do not bootstrap
// from the clsi cache — the v1 sidecar is local).
func (m *Manager) SyncFromCode(projectID, userID, filename string,
	line, column int, opts SyncOpts) (SyncFromCodeResult, error) {
	if err := validateSyncOpts(opts); err != nil {
		return SyncFromCodeResult{}, err
	}
	if filename == "" {
		return SyncFromCodeResult{}, &clerrors.InvalidParameter{
			Message: "file parameter is required",
		}
	}
	sm, err := m.loadSidecar(m.sidecarDir(projectID, userID, opts.BuildID), projectID)
	if err != nil {
		return SyncFromCodeResult{}, err
	}
	// Source content lives in the COMPILE dir (where Node's synctex -i reads
	// it: <compileDir>/<filename>), not the build dir.
	content := m.readFileSync(projectID, userID, filename)
	loc := so.NearestByOffset(sm.Locations, filename, so.OffsetAt(content, line, column))
	if loc == nil {
		return SyncFromCodeResult{
			CodePositions:       []map[string]any{},
			DownloadedFromCache: false,
		}, nil
	}
	return SyncFromCodeResult{
		CodePositions:       []map[string]any{viewRecord(loc)},
		DownloadedFromCache: false,
	}, nil
}

// --- syncFromPdf ----------------------------------------------------------------

// SyncFromPdf ports syncFromPdf: (page, h, v) click point -> the nearest
// location on that page (top-left distance, user-space basis; see package
// doc) -> edit record {file, line, column}. page is 1-based on the wire.
// No location on the page -> 200 with the "code" array empty.
func (m *Manager) SyncFromPdf(projectID, userID string,
	page, h, v int, opts SyncOpts) (SyncFromPdfResult, error) {
	if err := validateSyncOpts(opts); err != nil {
		return SyncFromPdfResult{}, err
	}
	sm, err := m.loadSidecar(m.sidecarDir(projectID, userID, opts.BuildID), projectID)
	if err != nil {
		return SyncFromPdfResult{}, err
	}
	loc := so.NearestByPoint(sm.Locations, page-1, h, v)
	if loc == nil {
		return SyncFromPdfResult{PdfPositions: []map[string]any{}}, nil
	}
	content := m.readFileSync(projectID, userID, loc.Span.File)
	line, column := so.LineColAt(content, loc.Span.ByteOffset)
	return SyncFromPdfResult{
		PdfPositions: []map[string]any{
			{"file": loc.Span.File, "line": line, "column": column},
		},
		DownloadedFromCache: false,
	}, nil
}

// --- source content reads -----------------------------------------------------------

// readFileSync: best-effort read of <compileDir>/<filename>; "" when the
// file is absent (offset math saturates instead of erroring — the location's
// file may have been edited away after the compile, and an empty reader is
// total per the D17 reader-layer contract). The reader is total: it never
// needs a typed "file missing" error to be total (see the 405/404 arms
// above: those are about output.pdf / the sidecar).
func (m *Manager) readFileSync(projectID, userID, filename string) string {
	data, err := os.ReadFile(filepath.Join(
		compileDirOf(m.Paths.CompilesDir, projectID, userID), filename))
	if err != nil {
		return ""
	}
	return string(data)
}

// viewRecord (D21): sidecar box -> clsi view record (flat map, the synctex
// "Output:" field set per clsi.go synctexparser.ParseViewOutput: Page->page,
// h->h, v->v, W->width, H->height).
func viewRecord(loc *so.Location) map[string]any {
	return map[string]any{
		"page":   loc.Page + 1,
		"h":      loc.X,
		"v":      loc.Y,
		"width":  loc.W,
		"height": loc.H,
	}
}
