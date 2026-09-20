// Package ot error types. These mirror the @overleaf/o-error based
// constructors in the oracle. The oracle records `err.constructor.name` and
// `err.message`; for ApplyError it also records `err.operand`. The Go port
// must produce the same triple, plus the operand where relevant.
package ot

import "fmt"

// Err is a named error. Kind is the JS error class name. Msg is the error
// message, which the oracle compares byte-for-byte.
type Err struct {
	Kind string
	Msg  string
}

func (e *Err) Error() string { return e.Msg }

// KindOf returns the recorded kind for an error value.
func KindOf(err error) string {
	if e, ok := err.(*Err); ok {
		return e.Kind
	}
	if e, ok := err.(*OError); ok {
		return e.Kind
	}
	return ""
}

// --- o-error base ---

// OError is the @overleaf/o-error base class. It carries the class name as the
// error kind for oracle comparison.
type OError struct {
	Kind    string
	Message string
	Info    map[string]any
}

func (e *OError) Error() string { return e.Message }

func newOError(kind, message string, info map[string]any) *OError {
	if info == nil {
		info = map[string]any{}
	}
	return &OError{Kind: kind, Message: message, Info: info}
}

// --- errors.js (TextOperation family) ---

type UnprocessableError struct {
	*OError
}

func NewUnprocessableError(msg string) *UnprocessableError {
	return &UnprocessableError{newOError("UnprocessableError", msg, nil)}
}

// ApplyError is thrown when an operation cannot be applied to a string.
// operand is the recorded operand (string | number).
type ApplyError struct {
	*OError
	Operand      any
	ResultLength int
}

func NewApplyError(msg string, operand any, resultLength int) *ApplyError {
	return &ApplyError{newOError("ApplyError", msg, nil), operand, resultLength}
}

// InvalidInsertionError is thrown when inserted text contains non-BMP
// characters. Its message is fixed.
type InvalidInsertionError struct {
	*OError
	Str string
}

func NewInvalidInsertionError(str string) *InvalidInsertionError {
	return &InvalidInsertionError{
		newOError("InvalidInsertionError", "inserted text contains non BMP characters", nil),
		str,
	}
}

// TooLongError is thrown when the resulting string would exceed
// MaxStringLength.
type TooLongError struct {
	*OError
	ResultLength int
}

func NewTooLongError(resultLength int) *TooLongError {
	return &TooLongError{newOError("TooLongError", "resulting string would be too long", map[string]any{"resultLength": resultLength}), resultLength}
}

// --- file_map.js (PathnameError family) ---

type PathnameError struct{ *OError }

type NonUniquePathnameError struct {
	*PathnameError
	Pathnames []string
}

func NewNonUniquePathnameError(pathnames []string) *NonUniquePathnameError {
	return &NonUniquePathnameError{
		&PathnameError{newOError("NonUniquePathnameError", "pathnames are not unique", map[string]any{"pathnames": pathnames})},
		pathnames,
	}
}

func truncatePathname(pathname string) string {
	if len(pathname) > 10 {
		return pathname[:5] + "..." + pathname[len(pathname)-5:]
	}
	return pathname
}

type BadPathnameError struct {
	*PathnameError
	Pathname string
	Reason   string
}

func NewBadPathnameError(pathname, reason string) *BadPathnameError {
	return &BadPathnameError{
		&PathnameError{newOError("BadPathnameError", "invalid pathname", map[string]any{"reason": reason, "pathname": pathname})},
		truncatePathname(pathname),
		reason,
	}
}

type PathnameConflictError struct {
	*PathnameError
	Pathname string
}

func NewPathnameConflictError(pathname string) *PathnameConflictError {
	return &PathnameConflictError{
		&PathnameError{newOError("PathnameConflictError", "pathname conflicts with another file", map[string]any{"pathname": pathname})},
		pathname,
	}
}

type FileNotFoundError struct {
	*PathnameError
	Pathname string
}

func NewFileNotFoundError(pathname string) *FileNotFoundError {
	return &FileNotFoundError{
		&PathnameError{newOError("FileNotFoundError", "file does not exist", map[string]any{"pathname": pathname})},
		pathname,
	}
}

// --- file.js ---

// EditMissingFileError is a recoverable error when editing a missing file.
type EditMissingFileError struct {
	*OError
}

func NewEditMissingFileError(pathname string) *EditMissingFileError {
	return &EditMissingFileError{newOError("EditMissingFileError", fmt.Sprintf("can't find file for editing: %s", pathname), nil)}
}

// NotEditableError is thrown when editing a file that is not editable.
type NotEditableError struct {
	*OError
}

func NewNotEditableError() *NotEditableError {
	return &NotEditableError{newOError("NotEditableError", "File is not editable", nil)}
}

// --- snapshot.js ---

type SnapshotError struct{ *OError }

// --- blob_store_base.js (CLSI HRW) ---

type NotFoundError struct {
	*OError
	Hash string
}

func NewNotFoundError(hash string) *NotFoundError {
	return &NotFoundError{
		&OError{Kind: "NotFoundError", Message: "blob " + hash + " not found"},
		hash,
	}
}

// isRecoverable returns true for the two error kinds Change.applyTo treats as
// recoverable (historical bad data), per change.js:
//
//	err instanceof Snapshot.EditMissingFileError || err instanceof FileMap.FileNotFoundError
func isRecoverable(err error) bool {
	_, ok := err.(*EditMissingFileError)
	if ok {
		return true
	}
	_, ok = err.(*FileNotFoundError)
	return ok
}
