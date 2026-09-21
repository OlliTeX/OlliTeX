package validtools

import "ollitex/go/libraries/oerror"

// errors.go — the Node error classes from Errors.js, Go-idiomatic typed
// wrappers over a *ZodError.
//
// Node:
//   class InvalidRequestError extends Error {
//     constructor(zodError) { super('Invalid request', {}, zodError); this.zodError = zodError }
//   }
//   class InvalidParamsError extends Error {
//     constructor(zodError) { super('Invalid request parameters', {}, zodError); this.zodError = zodError }
//   }
//
// Go: named struct types carrying the *ZodError, so the B2 surface
// (HandleValidationError) can `errors.As` them to route status. oerror is
// re-exported so callers can carry richer context in Info while keeping the
// wire text identical (the wire only reads .ZodError).

// InvalidRequestError marks a parse failure routed through
// HandleValidationError to its configured status (default 400).
type InvalidRequestError struct {
	*ZodError
	// OError wraps this for caller info/context (Node's `super(..., {},
	// {}/*cause*/)`) when the Go middleware wants oerror semantics.
	*oerror.OError
}

// InvalidParamsError marks a params/URL-validation failure routed to 404.
type InvalidParamsError struct {
	*ZodError
}

// NewInvalidRequestError mirrors `new InvalidRequestError(zodError)`.
func NewInvalidRequestError(ze *ZodError) *InvalidRequestError {
	return &InvalidRequestError{ZodError: ze, OError: oerror.New("Invalid request", nil, ze)}
}

// NewInvalidParamsError mirrors `new InvalidParamsError(zodError)`.
func NewInvalidParamsError(ze *ZodError) *InvalidParamsError {
	return &InvalidParamsError{ZodError: ze}
}

// Error renders the Node `super('Invalid request')` message.
func (e *InvalidRequestError) Error() string { return "Invalid request" }

// Error renders the Node `super('Invalid request parameters')` message.
func (e *InvalidParamsError) Error() string { return "Invalid request parameters" }
