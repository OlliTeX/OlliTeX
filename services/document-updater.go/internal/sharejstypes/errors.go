package sharejstypes

import "fmt"

// Error values mirror the vendored types' thrown Errors. Each type's
// message text is preserved verbatim for test parity.

// ErrInvalidOp is the vendored "bad op" throw.
type ErrInvalidOp struct{ Msg string }

func (e *ErrInvalidOp) Error() string { return e.Msg }

// InvalidOp mirrors `throw new Error(...)` from a type's op handling.
func InvalidOp(msg string) *ErrInvalidOp { return &ErrInvalidOp{Msg: msg} }

// InvalidOpf formats the message.
func InvalidOpf(format string, a ...any) *ErrInvalidOp {
	return &ErrInvalidOp{Msg: fmt.Sprintf(format, a...)}
}

// ErrInvalidSnapshot is the vendored "bad snapshot" throw.
type ErrInvalidSnapshot struct{ Msg string }

func (e *ErrInvalidSnapshot) Error() string { return e.Msg }

// InvalidSnapshot mirrors `throw new Error(...)` from a type's apply.
func InvalidSnapshot(msg string) *ErrInvalidSnapshot { return &ErrInvalidSnapshot{Msg: msg} }

// ErrTransformError is a generic transform-time error (e.g. "transformed op
// is invalid").
type ErrTransformError struct{ Msg string }

func (e *ErrTransformError) Error() string { return e.Msg }
