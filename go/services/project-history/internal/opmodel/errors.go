// Package opmodel — opmodel (scan ops, ranges, text operations).
//
// Port of the vendor overleaf-editor-core operation layer
// (lib/operation/{scan_op,text_operation,range,edit_operation,no_operation}
// + lib/range.js + lib/file_data/{tracking,comment,tracked_change}), 1:1
// vendored, stdlib only. Oracle: vendor unit tests
// test/unit/{scan_op,range,text_operation,edit_operation,operation}.test.js.
//
// Lengths are counted in UTF-16 code units (JS `str.length` semantics; a
// supplementary code point = 2 units), mirroring the engine throughout.
package opmodel

// Op errors mirror editor-core/lib/errors.js:
//
//	class UnprocessableError extends OError {}
//	class ApplyError extends UnprocessableError { constructor(message, operation, operand) }
//	class InvalidInsertionError extends UnprocessableError { constructor(str, operation) }
//	class TooLongError extends UnprocessableError { constructor(operation, resultLength) }
//
// OError carries (message, info). The subclass extra properties (operation,
// operand, str, resultLength) are mirrored as promoted struct fields so
// callers can type-assert and inspect them, matching the JS class shape.

// UnprocessableError mirrors `UnprocessableError extends OError` (message + info map).
type UnprocessableError struct {
	Message string
	Info    map[string]any
}

func (e *UnprocessableError) Error() string { return e.Message }

func NewUnprocessableError(message string, info map[string]any) *UnprocessableError {
	return &UnprocessableError{Message: message, Info: info}
}

// ApplyError mirrors editor-core ApplyError (message, operation, operand).
type ApplyError struct {
	UnprocessableError
	Operation any
	Operand   any
}

func (e *ApplyError) Error() string { return e.UnprocessableError.Message }

func NewApplyError(message string, operation, operand any) *ApplyError {
	return &ApplyError{
		UnprocessableError: UnprocessableError{Message: message},
		Operation:          operation,
		Operand:            operand,
	}
}

// InvalidInsertionError mirrors editor-core InvalidInsertionError.
//
// Vendor quirk (mirrored): the OError message is ALWAYS
// "inserted text contains non BMP characters" regardless of the argument;
// the constructor argument lands in the `str` property, and `operation` is
// extra. Call sites pass e.g. `insertion must be a string` or the offending
// insertion text.
type InvalidInsertionError struct {
	UnprocessableError
	Str       string
	Operation any
}

func (e *InvalidInsertionError) Error() string { return e.UnprocessableError.Message }

func NewInvalidInsertionError(str string, operation any) *InvalidInsertionError {
	return &InvalidInsertionError{
		UnprocessableError: UnprocessableError{
			Message: "inserted text contains non BMP characters",
		},
		Str:       str,
		Operation: operation,
	}
}

// TooLongError mirrors editor-core TooLongError (info: {resultLength}).
type TooLongError struct {
	UnprocessableError
	Operation any
	ResultLen int
}

func (e *TooLongError) Error() string { return e.UnprocessableError.Message }

func NewTooLongError(operation any, resultLength int) *TooLongError {
	return &TooLongError{
		UnprocessableError: UnprocessableError{
			Message: "resulting string would be too long",
			Info:    map[string]any{"resultLength": resultLength},
		},
		Operation: operation,
		ResultLen: resultLength,
	}
}

// RangeError mirrors Range's `new OError('Invalid range', {pos, length})`
// (lib/range.js constructor) and the method-level throws (shrinkBy/insertAt/
// splitAt: "Cannot shrink range by more than its length",
// "The cursor must be contained in the range").
type RangeError struct {
	Message string
	Info    map[string]any
}

func (e *RangeError) Error() string { return e.Message }

func NewRangeError(message string, info map[string]any) *RangeError {
	return &RangeError{Message: message, Info: info}
}
