package otc

// The OT error family (Node: errors.js; each extends OError /
// UnprocessableError). They are separate Go types so callers — and the test
// oracle — can identify them individually (`errors.As`) while still treating any
// of them as an *UnprocessableError* via the embedded base.

// UnprocessableError is the OT base error (Node: `UnprocessableError extends OError`).
type UnprocessableError struct {
	Message string
	// Info carries oerror tag metadata (set by tagErr) and is exposed through
	// InfoProvider so oerror.GetFullInfo surfaces it (Node: OError.info/tags).
	Info map[string]any
}

// Error implements error.
func (e *UnprocessableError) Error() string { return e.Message }

// OErrorInfo implements oerror.InfoProvider.
func (e *UnprocessableError) OErrorInfo() map[string]any { return e.Info }

// SetTagInfo merges oerror tag metadata in place (preserving the concrete OT
// error type).
func (e *UnprocessableError) SetTagInfo(info map[string]any) {
	if e.Info == nil {
		e.Info = make(map[string]any, len(info))
	}
	for k, v := range info {
		e.Info[k] = v
	}
}

// NewUnprocessableError builds an UnprocessableError.
func NewUnprocessableError(message string) *UnprocessableError {
	return &UnprocessableError{Message: message}
}

// ApplyError is raised when an operation cannot be applied to the given input
// (Node: `ApplyError extends UnprocessableError`).
type ApplyError struct {
	*UnprocessableError
	Operation any
	Operand   any
}

// NewApplyError builds an ApplyError.
func NewApplyError(message string, operation, operand any) *ApplyError {
	return &ApplyError{NewUnprocessableError(message), operation, operand}
}

// InvalidInsertionError is raised when an insert carries a disallowed string
// (Node: `InvalidInsertionError extends UnprocessableError`).
type InvalidInsertionError struct {
	*UnprocessableError
	Str       any
	Operation any
}

// NewInvalidInsertionError builds an InvalidInsertionError with the
// oracle-pinned message.
func NewInvalidInsertionError(str any, operation any) *InvalidInsertionError {
	return &InvalidInsertionError{
		NewUnprocessableError("inserted text contains non BMP characters"),
		str,
		operation,
	}
}

// TooLongError is raised when an operation result exceeds the string ceiling
// (Node: `TooLongError extends UnprocessableError`).
type TooLongError struct {
	*UnprocessableError
	Operation    any
	ResultLength int
}

// NewTooLongError builds a TooLongError with the oracle-pinned message.
func NewTooLongError(operation any, resultLength int) *TooLongError {
	return &TooLongError{
		NewUnprocessableError("resulting string would be too long"),
		operation,
		resultLength,
	}
}
