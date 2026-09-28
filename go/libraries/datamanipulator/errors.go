// Package datamanipulator is the Go 1:1 rewrite of the Node datamanipulator
// service (services/datamanipulator/app/src/*). It was split from the single
// top-level services file into logical files mirroring the Node module layout
// (errors.mjs, fileUtils.mjs, fileOperations.mjs, treeCompare.mjs, sync.mjs,
// server.mjs).
package datamanipulator

// --- errors (1:1 with errors.mjs) -------------------------------------------

type DMFileNotFoundError struct{ Path string }

func (e *DMFileNotFoundError) Error() string { return "File not found: " + e.Path }

type DMDirectoryNotFoundError struct{ Path string }

func (e *DMDirectoryNotFoundError) Error() string { return "Directory not found: " + e.Path }

type DMPermissionError struct {
	Action string
	Path   string
}

func (e *DMPermissionError) Error() string { return "Permission denied: " + e.Action + " " + e.Path }

type DMConflictError struct {
	Path    string
	Message string
}

func (e *DMConflictError) Error() string {
	if e.Message == "" {
		e.Message = "File conflict detected"
	}
	return e.Message + ": " + e.Path
}
