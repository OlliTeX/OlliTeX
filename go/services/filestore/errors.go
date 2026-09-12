// Package filestore is the Go 1:1 rewrite of the Node filestore service
// (services/filestore/app/*). Split from the single top-level services file
// into logical files mirroring the Node module layout (FSPersistor.js,
// LocalFileWriter.js, SafeExec.js, ImageOptimiser.js, FileConverter.js,
// FileHandler.js, ProjectKey.js, server.mjs). It has no SHARED_SERVICE_TOKEN
// auth (1:1 with Node; the web app talks to it in-container without a token).
package filestore

import (
	"errors"
)

// --- errors (1:1 with object-persistor Errors + filestore Errors) -----------

type fseError struct {
	msg  string
	code int
}

func (e *fseError) Error() string { return e.msg }

func fseNotFound() *fseError {
	return &fseError{"not found", 404}
}
func fseRead(detail string) *fseError {
	return &fseError{"read error: " + detail, 500}
}
func fseWrite(detail string) *fseError {
	return &fseError{"write error: " + detail, 500}
}
func fseNotImplemented(detail string) *fseError {
	return &fseError{"not implemented: " + detail, 500}
}
func fseConversion(detail string) *fseError {
	return &fseError{"conversion error: " + detail, 500}
}
func fseConvDisabled() *fseError {
	return &fseError{"image conversions are disabled", 500}
}
func fseInvalidParams(detail string) *fseError {
	return &fseError{"invalid parameters: " + detail, 500}
}
func fseFailedCommand(detail string) *fseError {
	return &fseError{"command failed with error exit code: " + detail, 500}
}

func fseErrCode(err error) int {
	var fe *fseError
	if errors.As(err, &fe) {
		return fe.code
	}
	return 0
}
