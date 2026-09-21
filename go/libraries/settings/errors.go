package settings

import "errors"

// ErrNilDefaults is returned when Merge receives a nil `defaults` map. In Node,
// `merge(overrides, undefined)` writes through `undefined[key] = v` and throws
// a TypeError before any observable result; the Go port surfaces an explicit
// sentinel instead of panicking.
var ErrNilDefaults = errors.New("settings.merge: nil defaults (Node: cannot assign properties to undefined)")

// ErrNilOverrides is returned when Merge receives a nil `overrides` map. Node
// `merge(undefined, defaults)` reaches `Object.entries(undefined)` and throws
// "Cannot convert undefined or null to object" immediately, returning nothing.
var ErrNilOverrides = errors.New("settings.merge: cannot convert undefined or null to object (overrides is nil)")

// ErrNullOverride is returned when a `null` (JS null) override value is folded.
// In Node, the value is `typeof 'object'` (not Array) so it recurses into
// `Object.entries(null)`, which throws "Cannot convert undefined or null to
// object". The Go port reports it once, at the offending key.
var ErrNullOverride = errors.New("cannot convert undefined or null to object")

// NullOverrideError is the *typed* form of ErrNullOverride that carries the
// dotted config-key path where a `null` override value was hit. Node has no
// path in its message (the raw TypeError); the path is a Go convenience that
// preserves the location context without changing the root-cause text.
type NullOverrideError struct {
	Path string
}

func (e *NullOverrideError) Error() string {
	return ErrNullOverride.Error()
}

func (e *NullOverrideError) Unwrap() error { return ErrNullOverride }

// PathOf returns the dotted key path recorded on a NullOverrideError, or "" if
// err is not one of those.
func PathOf(err error) string {
	var e *NullOverrideError
	if errors.As(err, &e) {
		return e.Path
	}
	return ""
}
