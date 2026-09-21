package rediswrapper

// errors_test.go — the Errors.js hierarchy: names, defaults, causes.

import (
	"errors"
	"testing"

	"ollitex/go/libraries/oerror"
)

func assertOError(t *testing.T, err error, wantType, wantMessage string) *oerror.OError {
	t.Helper()
	var oe *oerror.OError
	if !errors.As(err, &oe) {
		t.Fatalf("%v: not an OError", err)
	}
	if oe.Type != wantType {
		t.Fatalf("type: got %q want %q", oe.Type, wantType)
	}
	if oe.Message != wantMessage {
		t.Fatalf("message: got %q want %q", oe.Message, wantMessage)
	}
	return oe
}

func TestErrorHierarchyNames(t *testing.T) {
	t.Parallel()
	info := map[string]any{"stage": "probe"}

	cases := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{"RedisError", NewRedisError("redis says no", nil, nil), "redis says no"},
		{"RedisHealthCheckFailed", NewRedisHealthCheckFailed("health check failed", nil, nil), "health check failed"},
		{"RedisHealthCheckTimedOut", NewRedisHealthCheckTimedOut(info, nil), "timeout"},
		{"RedisHealthCheckWriteError", NewRedisHealthCheckWriteError("write failed", info, nil), "write failed"},
		{"RedisHealthCheckVerifyError", NewRedisHealthCheckVerifyError("read failed", info, nil), "read failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assertOError(t, c.err, c.name, c.wantMsg)
		})
	}
}

func TestErrorCauseChaining(t *testing.T) {
	t.Parallel()
	cause := errors.New("connection reset by peer")
	err := NewRedisHealthCheckWriteError("write errored", map[string]any{"stage": "write"}, cause)
	oe := assertOError(t, err, "RedisHealthCheckWriteError", "write errored")
	gotCause, ok := oe.Cause.(error)
	if !ok || gotCause != cause {
		t.Fatalf("cause chain: got %v", oe.Cause)
	}
	// cause must be reachable through standard errors.Unwrap / errors.Is
	if !errors.Is(err, cause) {
		t.Fatal("cause must be reachable via errors.Is (OError.Unwrap)")
	}
}

func TestErrorInfoPreserved(t *testing.T) {
	t.Parallel()
	info := map[string]any{"stage": "verify", "deleteAck": int64(0)}

	oe := assertOError(t, NewRedisHealthCheckVerifyError("delete failed", info, nil),
		"RedisHealthCheckVerifyError", "delete failed")
	if !reflectEqual(oe.Info, info) {
		t.Fatalf("info not preserved: %v", oe.Info)
	}

	oe2 := assertOError(t, NewRedisHealthCheckVerifyError("delete failed", nil, nil),
		"RedisHealthCheckVerifyError", "delete failed")
	if oe2.Info != nil {
		t.Fatalf("Info should be nil when none passed, got %v", oe2.Info)
	}
}
