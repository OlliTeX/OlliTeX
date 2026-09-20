// Package giterrors — snapshot-side (postback + snapshot-API) exceptions.
// Ports:
//
//	snapshot/push/exception/*  (SnapshotPostException, OutOfDate, InvalidProject,
//	  InvalidFiles, UnexpectedError, InternalError, PostbackTimeout,
//	  InvalidPostbackKey, UnexpectedPostback)
//	snapshot/base/*            (MissingRepositoryException + its message builders,
//	  ForbiddenException)
//	snapshot/exception/*       (FailedConnectionException)
//	snapshot/getdoc/exception/* (getdoc InvalidProjectException)
package giterrors

import (
	"encoding/json"
	"fmt"
)

// ---------------------------------------------------------------------------
// Snapshot-post exceptions (snapshot/push/exception/*)
// ---------------------------------------------------------------------------

// OutOfDateException — ports push/exception/OutOfDateException.
type OutOfDateException struct{}

func (e *OutOfDateException) Error() string { return "out of date" }
func (e *OutOfDateException) Description() []string {
	return []string{"out of date (shouldn't print this)"}
}

// InvalidProjectException (push/postback) — ports push/exception/InvalidProjectException.
// Built from postback body `{"errors":[...]}`.
type InvalidProjectException struct{ Errors []string }

func (e *InvalidProjectException) Error() string         { return "invalid project" }
func (e *InvalidProjectException) Description() []string { return e.Errors }

func buildInvalidProjectException(body []byte) *InvalidProjectException {
	var s struct {
		Errors []string `json:"errors"`
	}
	_ = json.Unmarshal(body, &s)
	return &InvalidProjectException{Errors: s.Errors}
}

// NewInvalidProjectException builds from a JSON postback body (exported
// seam: the server package's SnapshotPostExceptionBuilder mirrors Java's
// `new InvalidProjectException(jsonElement)`).
func NewInvalidProjectException(body []byte) *InvalidProjectException {
	return buildInvalidProjectException(body)
}

type errorEntry struct {
	File      string `json:"file"`
	CleanFile string `json:"cleanFile"`
	State     string `json:"state"`
}

// InvalidFilesException — ports push/exception/InvalidFilesException.
type InvalidFilesException struct{ Errors []errorEntry }

func (e *InvalidFilesException) Error() string { return "invalid files" }
func (e *InvalidFilesException) Description() []string {
	lines := []string{
		fmt.Sprintf("You have %d invalid files in your %s project:", len(e.Errors), serviceName()),
	}
	for _, er := range e.Errors {
		lines = append(lines, er.describe())
	}
	return lines
}

func (er errorEntry) describe() string {
	var paren string
	if er.CleanFile != "" {
		paren = "rename to: " + er.CleanFile
	} else if er.State == "disallowed" {
		paren = "invalid file extension"
	} else {
		paren = "error"
	}
	return er.File + " (" + paren + ")"
}

func buildInvalidFilesException(body []byte) *InvalidFilesException {
	var s struct {
		Errors []errorEntry `json:"errors"`
	}
	_ = json.Unmarshal(body, &s)
	return &InvalidFilesException{Errors: s.Errors}
}

// NewInvalidFilesException builds from a JSON postback body (exported seam:
// the postback package calls buildInvalidFilesException internally; tests
// bind stub Pushers directly). Shape mirrors the Java exception's
// `errors` body: [{file, cleanFile, state}...].
func NewInvalidFilesException(body []byte) *InvalidFilesException {
	return buildInvalidFilesException(body)
}

// UnexpectedErrorException — ports push/exception/UnexpectedErrorException (Severe).
type UnexpectedErrorException struct{}

func (e *UnexpectedErrorException) Error() string { return serviceName() + " error" }
func (e *UnexpectedErrorException) Description() []string {
	return []string{
		"There was an internal error with the " + serviceName() + " server.",
		"Please contact " + serviceName() + ".",
	}
}

// InternalErrorException — ports push/exception/InternalErrorException (Severe).
type InternalErrorException struct{}

func (e *InternalErrorException) Error() string { return "internal error" }
func (e *InternalErrorException) Description() []string {
	return []string{
		"There was an internal error with the Git server.",
		"Please contact " + serviceName() + ".",
	}
}

// PostbackTimeoutException — ports push/exception/PostbackTimeoutException (Severe).
type PostbackTimeoutException struct{ TimeoutSeconds int }

func (e *PostbackTimeoutException) Error() string {
	return fmt.Sprintf("Request timed out (after %d seconds)", e.TimeoutSeconds)
}
func (e *PostbackTimeoutException) Description() []string {
	return []string{
		"The " + serviceName() + " server is currently unavailable.",
		"Please try again later.",
	}
}

// InvalidPostbackKeyException — ports push/exception/InvalidPostbackKeyException.
type InvalidPostbackKeyException struct{}

func (InvalidPostbackKeyException) Error() string { return "invalid postback key" }

// UnexpectedPostbackException — ports push/exception/UnexpectedPostbackException.
type UnexpectedPostbackException struct{}

func (UnexpectedPostbackException) Error() string { return "unexpected postback" }

// ---------------------------------------------------------------------------
// Snapshot-API errors (snapshot/base/* + snapshot/exception/*)
// ---------------------------------------------------------------------------

// MissingRepositoryException — ports base/MissingRepositoryException.
type MissingRepositoryException struct{ DescriptionLines []string }

func (e *MissingRepositoryException) Error() string         { return "no git access" }
func (e *MissingRepositoryException) Description() []string { return e.DescriptionLines }

// GenericMissingRepositoryException uses GENERIC_REASON.
func GenericMissingRepositoryException() *MissingRepositoryException {
	return &MissingRepositoryException{DescriptionLines: []string{
		"This Overleaf project currently has no git access, either because",
		"the project does not exist, or because git access is not enabled",
		"for the project.",
		"",
		"If this is unexpected, please contact us at support@overleaf.com, or",
		"see https://www.overleaf.com/learn/how-to/Git_integration for more information.",
	}}
}

// buildDeprecatedMessage — newUrl "" -> the no-URL block.
func buildDeprecatedMessage(newUrl string) []string {
	if newUrl == "" {
		return []string{
			"This project has not yet been moved into the new version of Overleaf. You will",
			"need to move it in order to continue working on it. Please visit this project",
			"online on www.overleaf.com to do this.",
			"",
			"After migrating this project to the new version of Overleaf, you will be",
			"prompted to update your git remote to the project's new identifier.",
			"",
			"If this is unexpected, please contact us at support@overleaf.com, or",
			"see https://www.overleaf.com/learn/how-to/Git_integration for more information.",
		}
	}
	return []string{
		"This project has not yet been moved into the new version of Overleaf. You will",
		"need to move it in order to continue working on it. Please visit this project",
		"online to do this:",
		"",
		"    " + newUrl,
		"",
		"After migrating this project to the new version of Overleaf, you will be",
		"prompted to update your git remote to the project's new identifier.",
		"",
		"If this is unexpected, please contact us at support@overleaf.com, or",
		"see https://www.overleaf.com/learn/how-to/Git_integration for more information.",
	}
}

// buildExportedToV2Message — remoteUrl "" -> the no-URL block.
func buildExportedToV2Message(remoteUrl string) []string {
	if remoteUrl == "" {
		return []string{
			"This Overleaf project has been moved to Overleaf v2 and cannot be used with git at this time.",
			"",
			"If this error persists, please contact us at support@overleaf.com, or",
			"see https://www.overleaf.com/learn/how-to/Git_integration for more information.",
		}
	}
	return []string{
		"This Overleaf project has been moved to Overleaf v2 and has a new identifier.",
		"Please update your remote to:",
		"",
		"    " + remoteUrl,
		"",
		"Assuming you are using the default \"origin\" remote, the following commands",
		"will change the remote for you:",
		"",
		"    git remote set-url origin " + remoteUrl,
		"",
		"If this does not work, please contact us at support@overleaf.com, or",
		"see https://www.overleaf.com/learn/how-to/Git_integration for more information.",
	}
}

// ForbiddenException — ports base/ForbiddenException.
type ForbiddenException struct{}

func (ForbiddenException) Error() string         { return "forbidden" }
func (ForbiddenException) Description() []string { return []string{"forbidden"} }

// FailedConnectionException — ports snapshot/exception/FailedConnectionException.
type FailedConnectionException struct{}

func (FailedConnectionException) Error() string {
	return serviceName() + " server not available. Please try again later."
}

// GetDocInvalidProjectException — ports getdoc/exception/InvalidProjectException
// (thrown when getdoc returns status 404; constructed empty => no description).
type GetDocInvalidProjectException struct{}

func (e *GetDocInvalidProjectException) Error() string         { return "invalid project" }
func (e *GetDocInvalidProjectException) Description() []string { return []string{} }

// ---------------------------------------------------------------------------
// Exported accessors used by the snapshot wire layer (Java: the same static
// builders referenced from the SnapshotAPIRequest result handling).
// ---------------------------------------------------------------------------

// GenReason ports MissingRepositoryException.GENERIC_REASON.
func GenReason() []string {
	return []string{
		"This Overleaf project currently has no git access, either because",
		"the project does not exist, or because git access is not enabled",
		"for the project.",
		"",
		"If this is unexpected, please contact us at support@overleaf.com, or",
		"see https://www.overleaf.com/learn/how-to/Git_integration for more information.",
	}
}

// DeprecatedMessage ports MissingRepositoryException.buildDeprecatedMessage.
func DeprecatedMessage(newUrl string) []string { return buildDeprecatedMessage(newUrl) }

// ExportV2Message ports MissingRepositoryException.buildExportedToV2Message.
func ExportV2Message(remoteUrl string) []string { return buildExportedToV2Message(remoteUrl) }
