package rediswrapper

import (
	"ollitex/go/libraries/oerror"
)

// Error hierarchy mirroring libraries/redis-wrapper/Errors.js:
//
//	class RedisError extends OError {}
//	class RedisHealthCheckFailed extends RedisError {}
//	class RedisHealthCheckTimedOut extends RedisHealthCheckFailed {}
//	class RedisHealthCheckWriteError extends RedisHealthCheckFailed {}
//	class RedisHealthCheckVerifyError extends RedisHealthCheckFailed {}
//
// In Go the hierarchy is expressed through the OError `Type` (name) each
// constructor sets (Node `instance.name`), not through embedding — Node
// consumers (and tests) identify these by name/message, not instanceof.

// newRedisError builds an OError with name + message + optional info/cause
// (Node: `super(message, context, cause)` → OError signature).
func newRedisError(name, message string, info map[string]any, cause error) *oerror.OError {
	e := oerror.New(message, info).WithName(name)
	if cause != nil {
		e = e.WithCause(cause)
	}
	return e
}

// RedisError mirrors `class RedisError extends OError`.
func NewRedisError(message string, info map[string]any, cause error) *oerror.OError {
	return newRedisError("RedisError", message, info, cause)
}

// RedisHealthCheckFailed mirrors `class RedisHealthCheckFailed extends
// RedisError`.
func NewRedisHealthCheckFailed(message string, info map[string]any, cause error) *oerror.OError {
	return newRedisError("RedisHealthCheckFailed", message, info, cause)
}

// RedisHealthCheckTimedOut mirrors `class RedisHealthCheckTimedOut extends
// RedisHealthCheckFailed` — raised when the healthCheck does not complete
// within the heartbeat timeout.
func NewRedisHealthCheckTimedOut(info map[string]any, cause error) *oerror.OError {
	return newRedisError("RedisHealthCheckTimedOut", "timeout", info, cause)
}

// RedisHealthCheckWriteError mirrors `class RedisHealthCheckWriteError
// extends RedisHealthCheckFailed` — the healthCheck key write failed or
// errored.
func NewRedisHealthCheckWriteError(message string, info map[string]any, cause error) *oerror.OError {
	return newRedisError("RedisHealthCheckWriteError", message, info, cause)
}

// RedisHealthCheckVerifyError mirrors `class RedisHealthCheckVerifyError
// extends RedisHealthCheckFailed` — the healthCheck read back the wrong
// value, or delete/read/delete errored.
func NewRedisHealthCheckVerifyError(message string, info map[string]any, cause error) *oerror.OError {
	return newRedisError("RedisHealthCheckVerifyError", message, info, cause)
}
