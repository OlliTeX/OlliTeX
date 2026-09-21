package otc

import (
	"fmt"

	oerror "ollitex/go/libraries/oerror"
)

// NotEditableError mirrors File.NotEditableError ("File is not editable").
type NotEditableError struct{}

func (e *NotEditableError) Error() string { return "File is not editable" }

// InvalidConversionError mirrors EditOperation.InvalidConversionError, thrown
// when a lazy FileData must be materialised with an incompatible edit op.
type InvalidConversionError struct{ Msg string }

func (e *InvalidConversionError) Error() string { return e.Msg }

func newInvalidConversionError(pathname string, op EditOperation) *InvalidConversionError {
	return &InvalidConversionError{Msg: fmt.Sprintf("Cannot lazyify %s (edit op = %s)", pathname, mustJSON(op))}
}

// genericOpError is the Go stand-in for the many plain `new Error(msg)` throws
// in the operation/file-data layer (message-pinned, not a dedicated class in
// Node).
type genericOpError struct{ Msg string }

func (e *genericOpError) Error() string { return e.Msg }

func gop(msg string) error { return &genericOpError{Msg: msg} }

// tagErr attaches oerror metadata to err in place, preserving the concrete OT
// error type (Node's OError.tag tags in place, so the error still instancesApply
// ApplyError/TooLongError and so the oracle can assert the type). Non-OT errors
// fall back to oerror.Tag (wrap).
func tagErr(err error, info map[string]any) error {
	if err == nil {
		return nil
	}
	if s, ok := err.(interface{ SetTagInfo(map[string]any) }); ok {
		s.SetTagInfo(info)
		return err
	}
	return oerror.Tag(err, "", info)
}
