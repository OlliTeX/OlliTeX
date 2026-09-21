package otc

import (
	"fmt"
)

// typeError is Go's narrowest faithful equivalent of the JS `TypeError` that
// check-types assertions raise. It is a distinct typed error so tests can
// assert the specific class (mirroring Node's `.to.throw(TypeError)`).
type typeError struct{ msg string }

func (e *typeError) Error() string { return e.msg }

// newTypeError constructs a TypeError equivalent.
func newTypeError(msg string) error { return &typeError{msg: msg} }

func typeErrorf(format string, a ...any) error {
	return &typeError{msg: fmt.Sprintf(format, a...)}
}
