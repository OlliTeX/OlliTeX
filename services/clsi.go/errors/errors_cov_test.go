package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestWithCauseChaining(t *testing.T) {
	inner := fmt.Errorf("inner")
	e := NewOError("base", map[string]any{"k": "v"}).WithCause(inner)
	if e.Cause != inner || e.Message != "base" {
		t.Fatalf("chained oerror = %+v", e)
	}
	if errors.Unwrap(e) != inner {
		t.Fatalf("unwrap = %v", errors.Unwrap(e))
	}
}

func TestNewOErrorVariants(t *testing.T) {
	e := NewOError("plain")
	if e.Message != "plain" || e.Cause != nil || e.Info != nil {
		t.Fatalf("plain oerror = %+v", e)
	}
	info := map[string]any{"a": 1}
	e2 := NewOError("with-info", info)
	if e2.Info["a"] != 1 {
		t.Fatalf("info = %v", e2.Info)
	}
}

func TestTag(t *testing.T) {
	cause := fmt.Errorf("cause")
	tg := Tag(cause, "tagged")
	if tg.Message != "tagged" || tg.Cause != cause {
		t.Fatalf("tag = %+v", tg)
	}
	tgInfo := Tag(cause, "tagged2", map[string]any{"x": 2})
	if tgInfo.Info["x"] != 2 {
		t.Fatalf("tag info = %v", tgInfo.Info)
	}
	if nilCause := Tag(nil, "bare"); nilCause.Cause != nil || nilCause.Message != "bare" {
		t.Fatalf("nil-cause tag = %+v", nilCause)
	}
}

func TestNewConversionErrorT(t *testing.T) {
	c := NewConversionErrorT("conv failed", "png2pdf", "stderr-out", 4)
	if c.Message != "conv failed" || c.Type != "png2pdf" ||
		c.Stderr != "stderr-out" || c.ExitCode != 4 {
		t.Fatalf("conv error = %+v", c)
	}
	if c.Info["type"] != "png2pdf" || c.Info["exitCode"] != 4 {
		t.Fatalf("conv info = %v", c.Info)
	}
}

func TestOErrorUnwrap(t *testing.T) {
	// a chained OError unwraps to its cause (mirrors JS cause-chain walk).
	cause := fmt.Errorf("deep")
	e := NewOError("outer").WithCause(cause)
	if !errors.Is(e, cause) {
		t.Fatalf("errors.Is should find cause via Unwrap, got %v", e)
	}
}
