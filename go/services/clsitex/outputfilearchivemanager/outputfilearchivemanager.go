// Package outputfilearchivemanager ports services/clsi/app/js/OutputFileArchiveManager.js.
//
// Node parity:
//
//   - getContentDir(projectId, userId): when userId != "" the subDir is
//     "<projectId>-<userId>", else "<projectId>"; result is
//     "<outputDir>/<subDir>/" (trailing slash, Node template string).
//   - _getAllOutputFiles calls OutputFileFinder.promises.findOutputFiles([],
//     contentDir + OutputCacheManager.path(build, ".")) and filters the
//     list by:
//     path !== 'output.pdf' && path !== 'output.tar.gz' &&
//     path !== 'history-resync.json.gz' && !ignoreFiles.includes(path)
//     Error mapping: error.code in {ENOENT, ENOTDIR, EACCES}
//     -> NotFoundError('Output files not found'); other errors propagate.
//   - archiveFilesForBuild:
//     for each found file, open(contentDir + path(build, file.path));
//     a failed open -> warn + push into missingFiles (skipped);
//     otherwise append the stream under name = file.path.
//     missing non-empty -> append a 'missing_files.txt' text entry
//     (paths joined by newline).
//     finally archive.finalize().
//
// Port notes:
//
//   - Node's archiver (a ZIP stream) is reproduced with Go archive/zip:
//     entry NAMES are identical (the test contract asserts on entry names
//     and order, not on zip byte-stream equality, which cannot match
//     across implementations).
//   - OutputFileFinder is injectable via Find; production wiring assigns
//     the real walk at startup.
//   - OutputCacheManager.path is reproduced by PathForBuild = build+"/"+file.
package outputfilearchivemanager

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ignoreFiles — NOTE: updating this list requires a corresponding change in
// services/web/frontend/js/features/pdf-preview/util/file-list.ts
var ignoreFiles = []string{"output.fls", "output.fdb_latexmk"}

const (
	outputPDFName     = "output.pdf"
	outputTarName     = "output.tar.gz"
	historyResyncName = "history-resync.json.gz"
	missingFileName   = "missing_files.txt"
)

// OutputFile mirrors OutputFileFinder's record ({ path }).
type OutputFile struct {
	Path string
}

// FindFunc mirrors OutputFileFinder.promises.findOutputFiles(args, dir).
type FindFunc func(args []string, dir string) ([]OutputFile, error)

// AssignFind installs the production find implementation (default nil).
func AssignFind(f FindFunc) { Find = f }

// Find is the injectable OutputFileFinder.promises.findOutputFiles.
// Production init assigns AssignFind(realWalk). Tests override it freely.
var Find FindFunc

// NotFoundError mirrors Errors.NotFoundError.
type NotFoundError string

func (e NotFoundError) Error() string { return string(e) }

// GetContentDir ports getContentDir(projectId, userId).
func GetContentDir(outputDir, projectID, userID string) string {
	var subDir string
	if userID != "" {
		subDir = projectID + "-" + userID
	} else {
		subDir = projectID
	}
	return outputDir + "/" + subDir + "/"
}

// filterArchiveable mirrors the outputFiles.filter(...) predicate.
func filterArchiveable(files []OutputFile) []OutputFile {
	out := make([]OutputFile, 0, len(files))
	for _, f := range files {
		if f.Path != outputPDFName && f.Path != outputTarName && f.Path != historyResyncName && !stringContains(ignoreFiles, f.Path) {
			out = append(out, f)
		}
	}
	return out
}

func stringContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// PathForBuild mirrors OutputCacheManager.path(buildId, file) for the
// archive call sites: build + "/" + file. ("build/." is left as-is —
// Node passes "build/." straight into a path and the OS normalises it.)
func PathForBuild(build, file string) string {
	return build + "/" + file
}

// OpenErrorToNotFound ports _getAllOutputFiles's catch:
//
//	code === 'ENOENT' | 'ENOTDIR' | 'EACCES' -> NotFoundError('Output files not found')
//
// Go mapping: ENOENT -> os.ErrNotExist; EACCES -> os.ErrPermission;
// ENOTDIR -> *fs.PathError with ENOTDIR errno (via *fs.PathError.Err which
// is a *Error wrapping the raw errno).
func OpenErrorToNotFound(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return NotFoundError("Output files not found")
	}
	// ENOTDIR: on Linux this surfaces as *fs.PathError whose inner errno
	// error renders as "not a directory"; detect via the rendered message
	// (no exported sentinel in the Go stdlib for ENOTDIR).
	var pe *fs.PathError
	if errors.As(err, &pe) && isNotDirError(pe) {
		return NotFoundError("Output files not found")
	}
	return err
}

func isNotDirError(pe *fs.PathError) bool {
	return pe.Err != nil && strings.Contains(pe.Err.Error(), "not a directory")
}

// ArchiveTo writes a ZIP into zw, one entry per file (name = relPath,
// content = read from open(contentDir/relPath)); a failed open is collected
// into a "missing_files.txt" text entry appended when any file went missing.
func ArchiveTo(zw *zip.Writer, contentDir string, files []OutputFile) (err error) {
	var missing []string
	for _, f := range files {
		fh, err := os.Open(filepath.Join(contentDir, f.Path))
		if err != nil {
			missing = append(missing, f.Path)
			continue
		}
		defer fh.Close()
		w, cerr := zw.Create(f.Path)
		if cerr != nil {
			return cerr
		}
		if _, err := io.Copy(w, fh); err != nil {
			return err
		}
	}
	if len(missing) > 0 {
		w, cerr := zw.Create(missingFileName)
		if cerr != nil {
			return cerr
		}
		if _, err := io.WriteString(w, strings.Join(missing, "\n")); err != nil {
			return err
		}
	}
	return nil
}

// ArchiveFilesForBuild ports archiveFilesForBuild(projectId, userId, build).
// Returns the written ZIP via the out writer (Node: returns the archiver
// stream; the consumer writes it to disk).
func ArchiveFilesForBuild(outputDir, projectID, userID, build string, out io.Writer) error {
	if Find == nil {
		return fmt.Errorf("outputfilearchivemanager: Find not initialised")
	}
	contentDir := GetContentDir(outputDir, projectID, userID)
	files, err := findAllOutputFiles(contentDir, build)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	if cerr := ArchiveTo(zw, contentDir, files); cerr != nil {
		return cerr
	}
	return zw.Close()
}

func findAllOutputFiles(contentDir, build string) (files []OutputFile, err error) {
	dir := contentDir + PathForBuild(build, ".")
	f, err := Find(nil, dir)
	if err != nil {
		return nil, OpenErrorToNotFound(err)
	}
	files = filterArchiveable(f)
	return
}
