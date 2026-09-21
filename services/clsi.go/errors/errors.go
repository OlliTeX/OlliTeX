// Package errors mirrors services/clsi/app/js/Errors.js: the Errors map
// exposed via OError, plus ConversionError with USER_FACING_ERRORS.
//
// Wire shape: each error has a JS-side `name` and `message`; the Go types
// implement the same set so the error middleware can type-switch on them.
package errors

// userFacingExitCodes mirrors the JS ConversionError.USER_FACING_ERRORS set.
var userFacingExitCodes = map[int]bool{
	1:  true, // IO error
	23: true, // Unsupported extension
	24: true, // Citeproc error
	25: true, // Other bibliography error
	44: true, // Malformed XML error
	63: true, // Generic error (e.g. malformed docx container)
	64: true, // Parse error
	91: true, // Macro loop
	92: true, // UTF8 decoding error
	94: true, // Unsupported char set
	95: true, // Input not text
	97: true, // Missing data file
	98: true, // Missing metadata file
	99: true, // Missing file
}

// NotFoundError (Errors.NotFoundError — Error + custom name, not OError).
type NotFoundError struct{ Message string }

func (e *NotFoundError) Error() string { return e.Message }
func NewNotFoundError(message string) error {
	return &NotFoundError{Message: message}
}

// FilesOutOfSyncError (Errors.FilesOutOfSyncError — Error + custom name).
type FilesOutOfSyncError struct{ Message string }

func (e *FilesOutOfSyncError) Error() string { return e.Message }
func NewFilesOutOfSyncError(message string) error {
	return &FilesOutOfSyncError{Message: message}
}

// AlreadyCompilingError (Errors.AlreadyCompilingError — Error + custom name).
type AlreadyCompilingError struct{ Message string }

func (e *AlreadyCompilingError) Error() string { return e.Message }
func NewAlreadyCompilingError(message string) error {
	return &AlreadyCompilingError{Message: message}
}

// OError mirrors @overleaf/o-error: Error + optional info + cause.
type OError struct {
	Message string
	Info    map[string]any
	Cause   error
}

func (e *OError) Error() string { return e.Message }
func (e *OError) Unwrap() error { return e.Cause }
func (e *OError) WithInfo(info map[string]any) *OError {
	e.Info = info
	return e
}

// WithCause mirrors OError.prototype.withCause(err) — keeps Message, sets
// the wrapped cause, returns self for chaining.
func (e *OError) WithCause(cause error) *OError {
	e.Cause = cause
	return e
}

// Named OError subtypes (JS: export class X extends OError {}).

type QueueLimitReachedError struct {
	Message string
	Info    map[string]any
}

func (e *QueueLimitReachedError) Error() string { return e.Message }

func NewQueueLimitReachedError(e error) *QueueLimitReachedError {
	q := &QueueLimitReachedError{Info: map[string]any{}}
	if e != nil {
		q.Message = e.Error()
	}
	return q
}

type TimedOutError struct {
	Message string
	Info    map[string]any
}

func (e *TimedOutError) Error() string { return e.Message }

func NewTimedOutError(e error) *TimedOutError {
	t := &TimedOutError{Message: "the compile took too long"}
	if e != nil {
		t.Message = e.Error()
	}
	return t
}

type NoXrefTableError struct {
	Message string
	Info    map[string]any
}

func (e *NoXrefTableError) Error() string { return e.Message }

func NewNoXrefTableError(e error) *NoXrefTableError {
	if e == nil {
		return &NoXrefTableError{Message: "xref table not found"}
	}
	return &NoXrefTableError{Message: e.Error()}
}

type TooManyCompileRequestsError struct {
	Message string
	Info    map[string]any
}

func (e *TooManyCompileRequestsError) Error() string { return e.Message }

func NewTooManyCompileRequestsError(msg string) *TooManyCompileRequestsError {
	return &TooManyCompileRequestsError{Message: msg}
}

type InvalidParameter struct {
	Message string
	Info    map[string]any
}

func (e *InvalidParameter) Error() string { return e.Message }

type MissingUpdatesError struct {
	Message string
	Info    map[string]any
}

func (e *MissingUpdatesError) Error() string { return e.Message }

// ConversionError (Errors.ConversionError): super(message, { exitCode, type }).
type ConversionError struct {
	Message    string
	Info       map[string]any // exitCode, type
	Cause      error
	Stderr     string
	ExitCode   int
	Type       string
	UserFacing bool
}

func (e *ConversionError) Error() string { return e.Message }
func (e *ConversionError) Unwrap() error { return e.Cause }

// NewConversionError builds the error from the JS factory args:
// NewConversionError(message, stderr, exitCode) — the JS constructor
// (message, { type, stderr, exitCode }).
func NewConversionError(message, stderr string, exitCode int) *ConversionError {
	return &ConversionError{
		Message:    message,
		Info:       map[string]any{"exitCode": exitCode},
		Stderr:     stderr,
		ExitCode:   exitCode,
		UserFacing: userFacingExitCodes[exitCode],
	}
}

// NewConversionErrorT mirrors `new ConversionError(message, { type, exitCode,
// stderr })` with an explicit `type` in the info (used by ConversionManager).
func NewConversionErrorT(message, conversionType, stderr string, exitCode int) *ConversionError {
	return &ConversionError{
		Message:    message,
		Info:       map[string]any{"exitCode": exitCode, "type": conversionType},
		Stderr:     stderr,
		ExitCode:   exitCode,
		Type:       conversionType,
		UserFacing: userFacingExitCodes[exitCode],
	}
}

// IsX helpers for the error middleware (mirrors JS `instanceof` checks).

func IsNotFoundError(err error) bool {
	for err != nil {
		if _, ok := err.(*NotFoundError); ok {
			return true
		}
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}

func IsInvalidParameter(err error) bool {
	for err != nil {
		if _, ok := err.(*InvalidParameter); ok {
			return true
		}
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}

// IsAlreadyCompiling reports Whether err (or its cause chain) is the
// AlreadyCompilingError the LockManager throws on concurrent compiles.
func IsAlreadyCompiling(err error) bool {
	for err != nil {
		if _, ok := err.(*AlreadyCompilingError); ok {
			return true
		}
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
		} else {
			return false
		}
	}
	return false
}

// NewOError builds a bare OError with an optional info map (the public face
// for ported `new OError(msg, info)`). Used for the png2pdf / docker exit-
// error constructors that have no named CLSI subclass.
func NewOError(message string, info ...map[string]any) *OError {
	var e OError
	e.Message = message
	if len(info) > 0 {
		e.Info = info[0]
	}
	return &e
}

// Tag mirrors OError.tag(err, message, info): wraps err so the caller sees the
// tag message first (OError is the public face) while preserving the original
// error via Unwrap. A nil err yields a bare OError.
func Tag(err error, message string, info ...map[string]any) *OError {
	e := &OError{Message: message, Cause: err}
	if len(info) > 0 {
		e.Info = info[0]
	}
	return e
}
