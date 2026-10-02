// errors_cov_missing_test.go — coverage for the remaining errors constructors
// (NewMissingUpdatesError, IsMissingUpdates, NoXrefTable nil branch).
package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestNewMissingUpdatesErrorAndIsMissingUpdates(t *testing.T) {
	info := map[string]any{"baseHistoryVersion": -1}
	e := NewMissingUpdatesError("needs more updates", info)
	if e.Message != "needs more updates" {
		t.Fatalf("message = %q, want needs more updates", e.Message)
	}
	if e.Info["baseHistoryVersion"] != -1 {
		t.Fatalf("info = %v, want baseHistoryVersion=-1", e.Info)
	}
	if !IsMissingUpdates(e) {
		t.Fatalf("IsMissingUpdates(direct) = false, want true")
	}

	// Wrapped via fmt.Errorf %w: the walk must follow Unwrap.
	wrapped := fmt.Errorf("outer: %w", e)
	if !IsMissingUpdates(wrapped) {
		t.Fatalf("IsMissingUpdates(wrapped) = false, want true")
	}
	// Chained via *OError (its Unwalk returns the cause).
	chained := NewOError("outermost").WithCause(e)
	if !IsMissingUpdates(chained) {
		t.Fatalf("IsMissingUpdates(chained) = false, want true")
	}

	// Negatives.
	if IsMissingUpdates(fmt.Errorf("not a missing-updates error")) {
		t.Fatalf("IsMissingUpdates(plain) = true, want false")
	}
	if IsMissingUpdates(nil) {
		t.Fatalf("IsMissingUpdates(nil) = true, want false")
	}
	// A different error type must not be mistaken.
	if IsMissingUpdates(NewNoXrefTableError(nil)) {
		t.Fatalf("IsMissingUpdates(NoXrefTable) = true, want false")
	}
	// errors.New wrapped: no Unwalk, so the walk stops immediately.
	if IsMissingUpdates(errors.New("bare")) {
		t.Fatalf("IsMissingUpdates(bare) = true, want false")
	}
}

func TestNewNoXrefTableErrorNil(t *testing.T) {
	e := NewNoXrefTableError(nil)
	if e.Message != "xref table not found" {
		t.Fatalf("message = %q, want xref table not found", e.Message)
	}
	e2 := NewNoXrefTableError(fmt.Errorf("custom xref"))
	if e2.Message != "custom xref" {
		t.Fatalf("message = %q, want custom xref", e2.Message)
	}
	if e == e2 {
		t.Fatalf("expected distinct instances")
	}
}
