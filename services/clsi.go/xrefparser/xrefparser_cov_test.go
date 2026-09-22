package xrefparser

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestIsNoXrefError(t *testing.T) {
	if IsNoXrefError(fmt.Errorf("other")) {
		t.Fatal("plain error is not a no-xref error")
	}
	// a real parse failure: missing file -> os.PathError -> wrapped no-xref
	if _, err := ParseXrefTable("/nonexistent/nope.pdfxref", 0); err == nil {
		t.Fatal("expected parse error")
	} else if !IsNoXrefError(err) {
		t.Fatalf("wrapped parse error %v is not a no-xref error", err)
	}
	// a seed-mismatch noXrefError round-trips through IsNoXrefError
	if !IsNoXrefError(noXrefError{msg: "no xref table"}) {
		t.Fatal("noXrefError not detected by IsNoXrefError")
	}
}

func TestWrapXrefError(t *testing.T) {
	// os.PathError -> "xref file error <cause>"
	wrapped := wrapXrefError(&os.PathError{Op: "open", Path: "/nope", Err: os.ErrNotExist})
	if !IsNoXrefError(wrapped) || !strings.Contains(wrapped.Error(), "xref file error") {
		t.Fatalf("wrap = %v", wrapped)
	}
	// generic error -> "xref file parse error"
	if got := wrapXrefError(fmt.Errorf("weird")); got.Error() != "xref file parse error" {
		t.Fatalf("wrap = %v", got)
	}
	// noXrefError passes through unchanged
	if got := wrapXrefError(noXrefError{msg: "seed mismatch"}); got.Error() != "seed mismatch" {
		t.Fatalf("passthrough = %v", got)
	}
}

func TestWrapXrefErrorNil(t *testing.T) {
	// nil is neither noXrefError nor os.PathError -> parse-error wrap
	if got := wrapXrefError(nil); got.Error() != "xref file parse error" {
		t.Fatalf("nil wrap = %v", got)
	}
}

func TestSplitLinesEdgeCases(t *testing.T) {
	if got := splitLines([]byte{}); len(got) != 0 {
		t.Fatalf("empty split = %v", got)
	}
	// trailing newline: two lines (one empty)
	if got := splitLines([]byte("a\n\n")); len(got) != 2 || string(got[0]) != "a" || string(got[1]) != "" {
		t.Fatalf("split = %v", got)
	}
	// no trailing newline: last partial line kept
	got := splitLines([]byte("a\nb"))
	if len(got) != 2 || string(got[0]) != "a" || string(got[1]) != "b" {
		t.Fatalf("no-newline split = %v", got)
	}
}
