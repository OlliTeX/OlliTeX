package core

import (
	"encoding/json"
	"fmt"
)

// MaxStringLength — mirror of TextOperation.MAX_STRING_LENGTH (3 MiB chars).
const MaxStringLength = 3 * 1024 * 1024

// --- Error taxonomy (mirrors OEC). HTTP status mapping lives in the API layer,
// keyed on these types (persist-level-0 consumers: InvalidChangeError from the
// service wraps these). ---

// EditMissingFileError — recoverable (ignored in non-strict apply).
type EditMissingFileError struct{ Pathname string }

func (e *EditMissingFileError) Error() string {
	return "can't find file for editing: " + e.Pathname
}

// FileNotFoundError — recoverable (ignored in non-strict apply).
type FileNotFoundError struct{ Pathname string }

func (e *FileNotFoundError) Error() string {
	return "FileNotFoundError: " + e.Pathname
}

// ApplyError — text op vs string mismatch.
type ApplyError struct {
	Msg     string
	Op      json.RawMessage
	Content string
}

func (e *ApplyError) Error() string { return e.Msg + " " + string(e.Op) }

// TooLongError.
type TooLongError struct{ NewLength int }

func (e *TooLongError) Error() string {
	return fmt.Sprintf("TooLongError: %d", e.NewLength)
}

// UnprocessableError.
type UnprocessableError struct{ Msg string }

func (e *UnprocessableError) Error() string { return "UnprocessableError: " + e.Msg }

// BadRawError.
type BadRawError struct{ Msg string }

func (e *BadRawError) Error() string { return "BadRawError: " + e.Msg }

// NotEditableError.
type NotEditableError struct{}

func (e *NotEditableError) Error() string { return "File is not editable" }
