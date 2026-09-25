// Package errorsx — 1:1 port of `app/js/Errors.js` (overleaf/document-updater).
//
// Node: `class XError extends OError {}` — an OError subclass with no extra
// fields, instantiated with no arguments (message unset in Node). Go: a
// struct embedding `ollitex`'s `oerror.OError`, with `WithName` set so the
// rendered `Error()` string ("XError: ...") matches Node's `err.message`/
// `err.name` pair.
package errorsx

import oerror "ollitex/go/libraries/oerror"

// NotFoundError — the resource was not found.
type NotFoundError struct{ *oerror.OError }

// OpRangeNotAvailableError — an op-range is not available.
type OpRangeNotAvailableError struct{ *oerror.OError }

// ProjectStateChangedError — the project state changed mid-operation.
type ProjectStateChangedError struct{ *oerror.OError }

// DeleteMismatchError — delete metadata mismatch.
type DeleteMismatchError struct{ *oerror.OError }

// FileTooLargeError — a file exceeds the size limit.
type FileTooLargeError struct{ *oerror.OError }

// OTTypeMismatchError — OT type mismatch (info: {got, want}).
type OTTypeMismatchError struct{ *oerror.OError }

// DocumentValidationError — the doc returned by web/API failed validation.
type DocumentValidationError struct{ *oerror.OError }

// WebApiServerError — the web/API service returned a server error.
type WebApiServerError struct{ *oerror.OError }

func sub(name, msg string, info map[string]any) *oerror.OError {
	return oerror.New(msg, info).WithName(name)
}

func NotFound() *NotFoundError { return &NotFoundError{sub("NotFoundError", "", nil)} }

// NotFoundMsg mirrors `new Errors.NotFoundError(msg)`. Vendor ShareJsDB
// constructs a NotFoundError with a message when the doc key does not
// match.
func NotFoundMsg(msg string) *NotFoundError {
	return &NotFoundError{sub("NotFoundError", msg, nil)}
}

func OpRangeNotAvailable() *OpRangeNotAvailableError {
	return &OpRangeNotAvailableError{sub("OpRangeNotAvailableError", "", nil)}
}

func ProjectStateChanged() *ProjectStateChangedError {
	return &ProjectStateChangedError{sub("ProjectStateChangedError", "", nil)}
}

func DeleteMismatch() *DeleteMismatchError {

	return &DeleteMismatchError{sub("DeleteMismatchError", "", nil)}
}

// DeleteMismatchMsg mirrors `new Errors.DeleteMismatchError(msg)`. ShareJs
// UpdateManager maps a vendored "Delete component ..." text mismatch error
// to this ("Delete component does not match").
func DeleteMismatchMsg(msg string) *DeleteMismatchError {
	return &DeleteMismatchError{sub("DeleteMismatchError", msg, nil)}
}

func FileTooLarge() *FileTooLargeError { return &FileTooLargeError{sub("FileTooLargeError", "", nil)} }

// FileTooLargeMsg mirrors `Errors.FileTooLarge(msg)` as surfaced by
// ShareJsUpdateManager: the vendored public "max doc size" error is
// delivered as a plain error string "Update takes doc over max doc size", but
// the service layer surfaces it typed. Constructor kept for callers that
// want the typed value with a message.
func FileTooLargeMsg(msg string) *FileTooLargeError {
	return &FileTooLargeError{sub("FileTooLargeError", msg, nil)}
}

// OTTypeMismatch mirrors Node `new OTTypeMismatchError(got, want)`:
// message "ot type mismatch", info {got, want}.
func OTTypeMismatch(got, want string) *OTTypeMismatchError {
	return &OTTypeMismatchError{sub("OTTypeMismatchError", "ot type mismatch", map[string]any{"got": got, "want": want})}
}

func DocumentValidation() *DocumentValidationError {
	return &DocumentValidationError{sub("DocumentValidationError", "", nil)}
}

// WebApiServer mirrors `new WebApiServerError(msg)` — plain OError subclass.
func WebApiServer(msg string) *WebApiServerError {
	return &WebApiServerError{sub("WebApiServerError", msg, nil)}
}

// NotFoundE mirrors `new Errors.NotFoundError(msg, info)`.
func NotFoundE(msg string, info map[string]any) *NotFoundError {
	return &NotFoundError{sub("NotFoundError", msg, info)}
}

// OpRangeNotAvailableE mirrors
// `new Errors.OpRangeNotAvailableError(msg, info)`.
func OpRangeNotAvailableE(msg string, info map[string]any) *OpRangeNotAvailableError {
	return &OpRangeNotAvailableError{sub("OpRangeNotAvailableError", msg, info)}
}
