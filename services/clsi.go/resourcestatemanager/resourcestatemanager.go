// Package resourcestatemanager ports services/clsi/app/js/ResourceStateManager.js.
//
// Sync-state contract (verbatim from the Node header comment):
//
//   - The sync state is an identifier which must match for an incremental
//     update to be allowed.
//   - full compile stores state + resource list in the project dir; a
//     later incremental compile must present the SAME state, else 409
//     (FilesOutOfSyncError) with the prior resource list returned.
//   - an incremental compile may only update existing files with new
//     content; the identifier changes when docs/files are moved/added/
//     deleted/renamed.
//
// On-disk format: `<basePath>/.project-sync-state` holding one resource
// path per line, terminated by a final line `stateHash:<state>`.
package resourcestatemanager

import (
	stderrors "errors"
	"fmt"
	"os"
	"strings"

	oerrors "clsi/errors"
	"clsi/logger"
	"clsi/safereader"
)

const (
	// SyncStateFile is SYNC_STATE_FILE ('.project-sync-state').
	SyncStateFile = ".project-sync-state"
	// SyncStateMaxSize is SYNC_STATE_MAX_SIZE (128KiB).
	SyncStateMaxSize = 128 * 1024
)

// Resource mirrors { path }.
type Resource struct {
	Path string
}

// SaveProjectState ports saveProjectState(state, resources, basePath).
//
// state == nil clears (unlink, swallowing ENOENT); otherwise the resource
// list + `stateHash:<state>` are written as a \n-joined file (no trailing
// newline — matches Node's `[...list, 'stateHash:'+state].join('\n')`).
func SaveProjectState(state *string, resources []Resource, basePath string) (err error) {
	stateFile := basePath + "/" + SyncStateFile
	if state == nil {
		logger.Debug(map[string]any{"state": nil, "basePath": basePath}, "clearing sync state")
		e := os.Remove(stateFile)
		if e != nil {
			// Node: if (err && err.code !== 'ENOENT') callback(err)
			var pe *os.PathError
			if stderrors.As(e, &pe) {
				if stderrors.Is(pe.Err, os.ErrNotExist) {
					return nil
				}
			}
			return fmt.Errorf("ResourceStateManager.saveProjectState: %w", e)
		}
		return nil
	}
	logger.Debug(map[string]any{"state": *state, "basePath": basePath}, "writing sync state")
	lines := make([]string, 0, len(resources)+1)
	for _, r := range resources {
		lines = append(lines, r.Path)
	}
	lines = append(lines, "stateHash:"+*state)
	if err = os.WriteFile(stateFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return fmt.Errorf("ResourceStateManager.saveProjectState: %w", err)
	}
	return nil
}

// CheckProjectStateMatches ports checkProjectStateMatches(state, basePath).
//
// Reads the state file (bounded to SyncStateMaxSize via SafeReader). Any
// read error, empty file, or mismatched state ⇒ *errors.FilesOutOfSyncError
// ("invalid state for incremental update"). On success returns the resource
// list recorded on the last full compile.
func CheckProjectStateMatches(state string, basePath string) ([]Resource, error) {
	stateFile := basePath + "/" + SyncStateFile
	result, bytesRead, err := safereader.ReadFile(stateFile, SyncStateMaxSize)
	if err != nil {
		return nil, err
	}
	if bytesRead == SyncStateMaxSize {
		logger.Error(map[string]any{"file": stateFile, "size": SyncStateMaxSize, "bytesRead": bytesRead}, "project state file truncated")
	}
	array := []string{}
	if result != "" {
		array = strings.Split(result, "\n")
	}
	adjustedLength := len(array)
	if adjustedLength < 1 {
		adjustedLength = 1
	}
	resourceList := array[:adjustedLength-1]
	oldState := ""
	if len(array) >= adjustedLength {
		oldState = array[adjustedLength-1]
	}
	newState := "stateHash:" + state
	logger.Debug(map[string]any{"state": state, "oldState": oldState, "basePath": basePath, "stateMatches": newState == oldState}, "checking sync state")
	if newState != oldState {
		return nil, &oerrors.FilesOutOfSyncError{Message: "invalid state for incremental update"}
	}
	resources := make([]Resource, 0, len(resourceList))
	for _, p := range resourceList {
		resources = append(resources, Resource{Path: p})
	}
	return resources, nil
}

// CheckResourceFiles ports checkResourceFiles(resources, allFiles, basePath).
//
// Rejects resources whose path contains a literal `..` segment (plain fmt
// error, NOT an OError — the Node test matches on plain Error), and
// resources missing from allFiles (FilesOutOfSyncError, "resource files
// missing in incremental update").
func CheckResourceFiles(resources []Resource, allFiles []string, basePath string) (err error) {
	for _, r := range resources {
		if containsRelativePath(r.Path) {
			return fmt.Errorf("relative path in resource file list")
		}
	}
	seenFiles := make(map[string]struct{}, len(allFiles))
	for _, f := range allFiles {
		seenFiles[f] = struct{}{}
	}
	var missingFiles []string
	for _, r := range resources {
		if _, ok := seenFiles[r.Path]; !ok {
			missingFiles = append(missingFiles, r.Path)
		}
	}
	if len(missingFiles) > 0 {
		logger.Err(map[string]any{"missingFiles": missingFiles, "basePath": basePath, "allFiles": allFiles, "resources": resources}, "missing input files for project")
		return &oerrors.FilesOutOfSyncError{Message: "resource files missing in incremental update"}
	}
	return nil
}

// containsRelativePort: split on '/' and check for a '..' segment (Node:
// resource.path.split('/') then indexOf('..') !== -1 — exact segment match,
// NOT substring).
func containsRelativePath(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}
