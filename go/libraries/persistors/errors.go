package persistors

import "ollitex/go/libraries/oerror"

// Each persistor error embeds *oerror.OError; oerror.GetFullInfo only
// descends *OError values directly, so the wrappers re-expose the FULL
// merged info (their own + cause chain + tags) through the InfoProvider
// seam — exactly Node's `OError.getFullInfo(err)` on a subclass instance.
func fullInfoOf(e *oerror.OError) map[string]any {
	if e == nil {
		return nil
	}
	return oerror.GetFullInfo(e)
}

// Error hierarchy — a 1:1 port of libraries/object-persistor/src/Errors.js.
// Each Node class is `class X extends OError {}`, i.e. a subclass with no
// own behaviour; the constructor is OError's (message, info?, cause?).
//
// Go: each type embeds *oerror.OError (the OError base) and gains a named
// rendering ("NotFoundError: <message>") via WithName — the V8 error-name
// rendering. Constructors mirror `new X(message, info?, cause?)`.

type NotFoundError struct{ *oerror.OError }
type WriteError struct{ *oerror.OError }
type ReadError struct{ *oerror.OError }
type SettingsError struct{ *oerror.OError }
type NotImplementedError struct{ *oerror.OError }
type AlreadyWrittenError struct{ *oerror.OError }
type NoKEKMatchedError struct{ *oerror.OError }

func (e *NotFoundError) OErrorInfo() map[string]any       { return fullInfoOf(e.OError) }
func (e *WriteError) OErrorInfo() map[string]any          { return fullInfoOf(e.OError) }
func (e *ReadError) OErrorInfo() map[string]any           { return fullInfoOf(e.OError) }
func (e *SettingsError) OErrorInfo() map[string]any       { return fullInfoOf(e.OError) }
func (e *NotImplementedError) OErrorInfo() map[string]any { return fullInfoOf(e.OError) }
func (e *AlreadyWrittenError) OErrorInfo() map[string]any { return fullInfoOf(e.OError) }
func (e *NoKEKMatchedError) OErrorInfo() map[string]any   { return fullInfoOf(e.OError) }

func makeErr(t string, message string, info map[string]any, cause error) *oerror.OError {
	return oerror.New(message, info, cause).WithName(t)
}

func NewNotFoundError(message string, info map[string]any, cause ...error) *NotFoundError {
	var c error
	if len(cause) > 0 {
		c = cause[0]
	}
	return &NotFoundError{makeErr("NotFoundError", message, info, c)}
}

func NewWriteError(message string, info map[string]any, cause ...error) *WriteError {
	var c error
	if len(cause) > 0 {
		c = cause[0]
	}
	return &WriteError{makeErr("WriteError", message, info, c)}
}

func NewReadError(message string, info map[string]any, cause ...error) *ReadError {
	var c error
	if len(cause) > 0 {
		c = cause[0]
	}
	return &ReadError{makeErr("ReadError", message, info, c)}
}

func NewSettingsError(message string, info map[string]any, cause ...error) *SettingsError {
	var c error
	if len(cause) > 0 {
		c = cause[0]
	}
	return &SettingsError{makeErr("SettingsError", message, info, c)}
}

func NewNotImplementedError(message string, info map[string]any, cause ...error) *NotImplementedError {
	var c error
	if len(cause) > 0 {
		c = cause[0]
	}
	return &NotImplementedError{makeErr("NotImplementedError", message, info, c)}
}

func NewAlreadyWrittenError(message string, info map[string]any, cause ...error) *AlreadyWrittenError {
	var c error
	if len(cause) > 0 {
		c = cause[0]
	}
	return &AlreadyWrittenError{makeErr("AlreadyWrittenError", message, info, c)}
}

func NewNoKEKMatchedError(message string, info map[string]any, cause ...error) *NoKEKMatchedError {
	var c error
	if len(cause) > 0 {
		c = cause[0]
	}
	return &NoKEKMatchedError{makeErr("NoKEKMatchedError", message, info, c)}
}

// Node's `instanceof` checks across the persistors (e.g. `err instanceof
// NotFoundError`). Go: errors.Is through Unwrap + a type switch helper.
func asPersistorError(e error) any {
	for e != nil {
		switch t := e.(type) {
		case *NotFoundError, *WriteError, *ReadError, *SettingsError,
			*NotImplementedError, *AlreadyWrittenError, *NoKEKMatchedError:
			return t
		case interface{ Unwrap() error }:
			e = t.Unwrap()
		default:
			return nil
		}
	}
	return nil
}

func isNotFoundError(e error) bool { _, ok := asPersistorError(e).(*NotFoundError); return ok }
func isAlreadyWrittenError(e error) bool {
	_, ok := asPersistorError(e).(*AlreadyWrittenError)
	return ok
}
func isReadError(e error) bool { _, ok := asPersistorError(e).(*ReadError); return ok }
