package errors

import (
	"errors"
	"fmt"
	"testing"
)

// These tests mirror services/clsi/app/js/Errors.js and the Go IsX helpers
// for the error middleware. They exercise the exported constructors and the
// error-chain (Unwrap) walk that mirrors the JS `instanceof` checks.

func TestNotFound(t *testing.T) {
	err := NewNotFoundError("build not found")
	if got := err.Error(); got != "build not found" {
		t.Fatalf("Error() = %q, want %q", got, "build not found")
	}
	if !IsNotFoundError(err) {
		t.Fatal("IsNotFoundError(NewNotFoundError) = false, want true")
	}
	// Through the Unwrap chain: errors.Wrap-style wrapping.
	wrapped := fmt.Errorf("outer: %w", err)
	if !IsNotFoundError(wrapped) {
		t.Fatal("IsNotFoundError(wrapped) = false, want true (Unwrap chain)")
	}
	// A plain error is not NotFound.
	if IsNotFoundError(errors.New("other")) {
		t.Fatal("IsNotFoundError(plain error) = true, want false")
	}
}

func TestFilesOutOfSync(t *testing.T) {
	err := NewFilesOutOfSyncError("resource changed during compile")
	if got := err.Error(); got != "resource changed during compile" {
		t.Fatalf("Error() = %q, want %q", got, "resource changed during compile")
	}
	// Identifying via a direct type assertion (the middleware may switch on it).
	if _, ok := err.(*FilesOutOfSyncError); !ok {
		t.Fatal("NewFilesOutOfSyncError did not return *FilesOutOfSyncError")
	}
}

func TestAlreadyCompiling(t *testing.T) {
	err := NewAlreadyCompilingError("another compile has started")
	if got := err.Error(); got != "another compile has started" {
		t.Fatalf("Error() = %q, want %q", got, "another compile has started")
	}
	if !IsAlreadyCompiling(err) {
		t.Fatal("IsAlreadyCompiling(NewAlreadyCompilingError) want true")
	}
	wrapped := fmt.Errorf("outer: %w", err)
	if !IsAlreadyCompiling(wrapped) {
		t.Fatal("IsAlreadyCompiling(wrapped) = false, want true (Unwrap chain)")
	}
	if IsAlreadyCompiling(errors.New("other")) {
		t.Fatal("IsAlreadyCompiling(plain error) = true, want false")
	}
}

func TestOErrorCauseChain(t *testing.T) {
	cause := fmt.Errorf("root cause")
	e := &OError{Message: "outer", Info: map[string]any{"k": "v"}, Cause: cause}
	if e.Error() != "outer" {
		t.Fatalf("OError.Error() = %q, want outer", e.Error())
	}
	if e.Unwrap() != cause {
		t.Fatal("OError.Unwrap() did not return the cause")
	}
	info := e.WithInfo(map[string]any{"other": int64(1)})
	if info.Info["other"] != int64(1) {
		t.Fatalf("WithInfo returned unexpected Info: %v", info.Info)
	}
}

// TestConversionError checks the USER_FACING_ERRORS set byte-for-byte.
func TestConversionErrorUserFacing(t *testing.T) {
	cases := map[int]bool{
		0:   false,
		1:   true, // IO error
		2:   false,
		23:  true, // Unsupported extension
		24:  true, // Citeproc error
		25:  true, // Other bibliography error
		44:  true, // Malformed XML
		63:  true, // Generic
		64:  true, // Parse error
		91:  true, // Macro loop
		92:  true, // UTF8 decoding
		94:  true, // Unsupported char set
		95:  true, // Input not text
		97:  true, // Missing data file
		98:  true, // Missing metadata file
		99:  true, // Missing file
		100: false,
	}
	for code, want := range cases {
		e := NewConversionError("boom", "stderr line", code)
		if e.UserFacing != want {
			t.Fatalf("exitCode %d UserFacing = %v, want %v", code, e.UserFacing, want)
		}
		if e.Stderr != "stderr line" {
			t.Fatalf("exitCode %d Stderr = %q, want %q", code, e.Stderr, "stderr line")
		}
		if e.ExitCode != code {
			t.Fatalf("exitCode %d ExitCode = %d", code, e.ExitCode)
		}
		if e.Error() != "boom" {
			t.Fatalf("exitCode %d Error() = %q, want %q", code, e.Error(), "boom")
		}
		var ce *ConversionError
		if !errors.As(error(e), &ce) {
			t.Fatalf("exitCode %d errors.As did not match *ConversionError", code)
		}
	}
}

func TestInvalidParameter(t *testing.T) {
	var err error = &InvalidParameter{Message: "bad param"}
	if !IsInvalidParameter(err) {
		t.Fatal("IsInvalidParameter(*InvalidParameter) want true")
	}
	if IsInvalidParameter(errors.New("not it")) {
		t.Fatal("IsInvalidParameter(plain error) = true, want false")
	}
	// Wrapped in an OError.
	wrapped := &OError{Message: "wrap", Cause: err}
	if !IsInvalidParameter(wrapped) {
		t.Fatal("IsInvalidParameter(wrapped OError) = false, want true (Unwrap chain)")
	}
}

func TestIsXFalseBranches(t *testing.T) {
	plain := NewNotFoundError("plain")
	if IsAlreadyCompiling(plain) {
		t.Fatal("IsAlreadyCompiling(NotFoundError) = true, want false")
	}
	if IsInvalidParameter(plain) {
		t.Fatal("IsInvalidParameter(NotFoundError) = true, want false")
	}
}

// TestOErrorSubtypes exercises each exported OError subtype's Error()
// (mirroring the JS `class X extends OError {}` shadings).
func TestOErrorSubtypes(t *testing.T) {
	messages := map[string]error{
		"QueueLimitReached":      &QueueLimitReachedError{Message: "queue full"},
		"TimedOut":               &TimedOutError{Message: "timeout"},
		"NoXrefTable":            &NoXrefTableError{Message: "no xref"},
		"TooManyCompileRequests": &TooManyCompileRequestsError{Message: "too many"},
		"MissingUpdates":         &MissingUpdatesError{Message: "missing"},
	}
	for name, e := range messages {
		if got := e.Error(); got == "" {
			t.Fatalf("%s Error() = empty", name)
		}
	}
}

func TestConversionErrorUnwrap(t *testing.T) {
	cause := fmt.Errorf("inner")
	e := &ConversionError{Message: "boom", Cause: cause}
	if e.Unwrap() != cause {
		t.Fatal("ConversionError.Unwrap() did not return cause")
	}
}

// TestIsXUnwrapToNil covers the final `return false` reached when the
// Unwrap chain terminates at nil without a matching type (e.g. an OError
// whose Cause is nil).
func TestIsXUnwrapToNil(t *testing.T) {
	// &OError{}: not a NotFound, has Unwrap returning nil -> loop ends -> false.
	oe := &OError{Message: "x"}
	if IsNotFoundError(oe) {
		t.Fatal("IsNotFoundError(&OError{}) = true, want false (Unwrap to nil)")
	}
	if IsInvalidParameter(oe) {
		t.Fatal("IsInvalidParameter(&OError{}) = true, want false (Unwrap to nil)")
	}
	if IsAlreadyCompiling(oe) {
		t.Fatal("IsAlreadyCompiling(&OError{}) = true, want false (Unwrap to nil)")
	}
}

func TestInvalidParameterErrorString(t *testing.T) {
	e := &InvalidParameter{Message: "nope", Info: map[string]any{"k": 1}}
	if e.Error() != "nope" {
		t.Fatalf("Error() = %q, want nope", e.Error())
	}
}
