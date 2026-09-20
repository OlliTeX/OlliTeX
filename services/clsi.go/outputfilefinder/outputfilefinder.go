// Package outputfilefinder ports services/clsi/app/js/OutputFileFinder.js.
//
// Node parity:
//
//   - findOutputFiles(resources, directory):
//     walkFolder(directory, ”, files, allEntries):
//     dirents = fs.readdir(path.join(directory, d)...)
//     if isDirectory  -> recurse THEN push "d/name/" (children first,
//     "dir/" last — so a directory's trailing-slash
//     entry appears AFTER its children);
//     if isFile       -> files.push + allEntries.push (no trailing /);
//     else (symlink, fifo, socket, device) -> allEntries.push only.
//     for each walked path:
//     if resources contains the path -> skip
//     if path == '.project-sync-state' -> skip
//     outputFiles.push({ path, type: Path.extname(path).replace(/^\./,”) || undefined })
//
//   - Node's Dirent.isDirectory() is lstat-based: a symlink->dir is NOT a
//     directory and NOT a file — it lands in the else bucket (allEntries
//     only, no recursion). Go's os.ReadDir uses os.Lstat the same way, so
//     de.IsDir() == false and de.Type().IsRegular() == false for a symlink.
//
//   - Path.extname: Go's path.Ext diverges from Node's on leading/trailing
//     dots, so we hand-roll it (nodeExtname).
package outputfilefinder

import (
	"os"
	"path"
	"strings"
)

// Resource mirrors the { path } objects in `resources`.
type Resource struct {
	Path string
}

// OutputFile mirrors the { path, type } objects.
// OutputFile mirrors the Node outputFile object produced by find():
// {path, type} plus the fields OutputCacheManager attaches during
// save/collect (build, size, contentId, ranges, startXRefTable). Pointer
// fields carry JSON "absent" parity with Node's undefined keys.
type OutputFile struct {
	Path      string         `json:"path"`
	Type      string         `json:"type"`
	Build     string         `json:"build,omitempty"`
	Size      *int64         `json:"size,omitempty"`
	ContentID *string        `json:"contentId,omitempty"`
	Ranges    []ContentRange `json:"ranges,omitempty"`
	// StartXRefTable mirrors startXRefTable on the Node file object.
	StartXRefTable *int64 `json:"startXRefTable,omitempty"`
}

// ContentRange mirrors the Node contentRange {objectId, start, end, hash}.
type ContentRange struct {
	ObjectID string `json:"objectId"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Hash     string `json:"hash"`
}

// FindResult mirrors { outputFiles, allEntries }.
type FindResult struct {
	OutputFiles []OutputFile
	AllEntries  []string
}

// FindOutputFiles ports findOutputFiles(resources, directory).
func FindOutputFiles(resources []Resource, directory string) (result FindResult, err error) {
	files := []string{}
	allEntries := []string{}
	if err = walkFolder(directory, "", &files, &allEntries); err != nil {
		return FindResult{}, err
	}
	incoming := make(map[string]bool, len(resources))
	for _, r := range resources {
		incoming[r.Path] = true
	}
	outputFiles := []OutputFile{}
	for _, p := range files {
		if incoming[p] {
			continue
		}
		if p == ".project-sync-state" {
			continue
		}
		outputFiles = append(outputFiles, OutputFile{
			Path: p,
			Type: typeFromPath(p),
		})
	}
	return FindResult{
		OutputFiles: outputFiles,
		AllEntries:  allEntries,
	}, nil
}

// typeFromPath ports Path.extname(path).replace(/^\./, ”) || undefined.
// nodeExtname always returns "" or ".xxx"; strip the leading dot and map
// the resulting empty string to Go "" (== JS undefined).
func typeFromPath(p string) string {
	return strings.TrimPrefix(nodeExtname(p), ".")
}

// nodeExtname ports node:path.extname (lib/path.js extname) for the POSIX
// (slash) separator. Returns the extension including the dot, or "".
func nodeExtname(pathStr string) string {
	start := 0
	startDot := -1
	startPart := 0
	end := -1
	matchedSlash := true
	preDotState := 0
	for i := len(pathStr) - 1; i >= start; i-- {
		code := pathStr[i]
		if code == '/' {
			if !matchedSlash {
				startPart = i + 1
				break
			}
			continue
		}
		if end == -1 {
			matchedSlash = false
			end = i + 1
		}
		if code == '.' {
			if startDot == -1 {
				startDot = i
			} else if preDotState != 1 {
				preDotState = 1
			}
		} else if startDot != -1 {
			preDotState = -1
		}
	}
	if startDot == -1 || end == -1 ||
		preDotState == 0 ||
		(preDotState == 1 && startDot == end-1 && startDot == startPart+1) {
		return ""
	}
	return pathStr[startDot:end]
}

// walkFolder ports walkFolder(compileDir, d, files, allEntries).
//
// Closure self-reference: `var walk func(...)` so it can call itself.
func walkFolder(compileDir, d string, files, allEntries *[]string) (err error) {
	dirents, err := os.ReadDir(path.Join(compileDir, d))
	if err != nil {
		return err
	}
	for _, de := range dirents {
		p := path.Join(d, de.Name())
		if de.IsDir() {
			// Node: recurse FIRST, then push "dir/" AFTER (so children
			// appear before their parent's trailing-slash entry).
			if werr := walkFolder(compileDir, p, files, allEntries); werr != nil {
				return werr
			}
			*allEntries = append(*allEntries, p+"/")
		} else if de.Type().IsRegular() {
			*files = append(*files, p)
			*allEntries = append(*allEntries, p)
		} else {
			// symlink / fifo / socket / device: allEntries only.
			*allEntries = append(*allEntries, p)
		}
	}
	return nil
}
