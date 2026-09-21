package oerror

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// --- Node suite parity (libraries/o-error/test/unit/o-error*.test.js) ---
//
// Every `it()` in both Node files is re-expressed here as a Go case,
// asserting the EXACT pinned strings. The Go-side adaptations (no V8 stacks,
// no monkey-patchable plain-Error fields, "OError: " render for wrapped plain
// errors) are pinned as named divergences in go/libraries/HANDOFF.md and
// exercised here, not hidden.

func TestInfoObject(t *testing.T) {
	e1 := New("foo", map[string]any{"foo": 1})
	if !mapEqual(e1.Info, map[string]any{"foo": 1}) {
		t.Fatalf("info %v", e1.Info)
	}
	e2 := Of("foo").WithInfo(map[string]any{"foo": 2})
	if !mapEqual(e2.Info, map[string]any{"foo": 2}) {
		t.Fatalf("WithInfo %v", e2.Info)
	}
	if Of("foo").Info != nil {
		t.Fatalf("info must be nil when omitted (Node: falsy check)")
	}
	if Of("foo").WithInfo(nil).Info != nil {
		t.Fatalf("WithInfo(nil) must be a no-op")
	}
}

func TestCause(t *testing.T) {
	e1 := New("foo", map[string]any{"foo": 1}, errors.New("cause 1"))
	if got, want := e1.Cause.(error).Error(), "cause 1"; got != want {
		t.Fatalf("cause %q want %q", got, want)
	}
	e2 := Of("foo").WithCause(errors.New("cause 2"))
	if got := e2.Cause.(error).Error(); got != "cause 2" {
		t.Fatalf("WithCause %q", got)
	}
	// Node: "accepts non-Error causes".
	if e3 := New("foo", nil, "not-an-error"); e3.Cause != "not-an-error" {
		t.Fatalf("cause %v", e3.Cause)
	}
	if e4 := Of("foo").WithCause("not-an-error"); e4.Cause != "not-an-error" {
		t.Fatalf("WithCause any %v", e4.Cause)
	}
}

func TestCustomErrorTypesWithCauses(t *testing.T) {
	// Node "handles a custom error type with a cause".
	inner := errors.New("internal error")
	e := Of("failed to foo").WithName("CustomError1").WithCause(inner)
	if got, want := e.Error(), "CustomError1: failed to foo"; got != want {
		t.Fatalf("Error() %q want %q", got, want)
	}
	if got, want := GetFullStack(e), "CustomError1: failed to foo\ncaused by:\n    internal error"; got != want {
		t.Fatalf("stack\n got=%q\nwant=%q", got, want)
	}
	if !mapEmpty(GetFullInfo(e)) {
		t.Fatalf("info %+v", GetFullInfo(e))
	}

	// Node "handles a custom error type with nested causes".
	level := Of("failed to bar!").WithName("CustomError2").WithCause(inner)
	e3 := Of("failed to foo").WithName("CustomError1").WithCause(level)
	want := "CustomError1: failed to foo\ncaused by:\n    CustomError2: failed to bar!\n    caused by:\n        internal error"
	if got := GetFullStack(e3); got != want {
		t.Fatalf("nested stack\n got=%q\nwant=%q", got, want)
	}
	if !mapEmpty(GetFullInfo(e3)) {
		t.Fatalf("nested info %+v", GetFullInfo(e3))
	}
}

func TestTagAccumulates(t *testing.T) {
	// Node "tags errors thrown from an async function" (structure-level):
	// Error: foo error / TaggedError: failed to bar / TaggedError: failed to baz
	// (Go divergence: the wrapped plain error renders "OError: foo error",
	// not "Error: foo error" — see HANDOFF.md).
	e1 := Tag(errors.New("foo error"), "failed to bar", map[string]any{"bar": "baz"})
	e2 := Tag(e1, "failed to baz", map[string]any{"baz": "bat"})
	want := []string{"OError: foo error", "TaggedError: failed to bar", "TaggedError: failed to baz"}
	if got := splitLines(GetFullStack(e2)); !equalStrings(got, want) {
		t.Fatalf("tag stack\n got=%v\nwant=%v", got, want)
	}
	if got := GetFullInfo(e2); !mapEqual(got, map[string]any{"bar": "baz", "baz": "bat"}) {
		t.Fatalf("tag info %v", got)
	}
}

func TestTagSingletonBehavior(t *testing.T) {
	// Node "should handle a singleton error": every tag call mutates the same
	// object. Go: *OError pointers are identity-checked.
	ep := Tag(Tag(Tag(Tag(errors.New("singleton error"), "in helper", nil), "in helper", nil), "in helper", nil), "in endpoint", nil)
	if got, want := tagMessages(ep.(*OError)), []string{"in helper", "in helper", "in helper", "in endpoint"}; !equalStrings(got, want) {
		t.Fatalf("singleton tags %v want %v", got, want)
	}
	// Pointer identity through the chain (Node singleton):
	oe, _ := ep.(*OError)
	again := Tag(oe, "again", nil)
	if again != any(oe) {
		t.Fatalf("Tag must return the same *OError")
	}
}

func TestTagCapDefault(t *testing.T) {
	// 100 default cap: overflow at tag 101 with sentinel in slot 1.
	e := Tag(errors.New("x"), "t1", nil)
	oe, _ := e.(*OError)
	for i := 2; i <= 100; i++ {
		e = Tag(e, fmt.Sprintf("t%d", i), nil)
	}
	oe, _ = e.(*OError)
	if !oe.Tags[0].IsDropped() && oe.Tags[1].IsDropped() {
		t.Fatalf("sentinel must be in slot 1: %v", tagMessages(oe))
	}
	if len(oe.Tags) != MaxTags {
		t.Fatalf("tag count %d want %d", len(oe.Tags), MaxTags)
	}
}

func TestTagCapThree(t *testing.T) {
	defer func() { MaxTags = 100 }()
	MaxTags = 3
	// Node "should not tag more than that": [t1 DROP t4 t5].
	e := Tag(errors.New("test error"), "test message 1", nil)
	for i := 2; i <= 5; i++ {
		e = Tag(e, fmt.Sprintf("test message %d", i), nil)
	}
	oe, _ := e.(*OError)
	want := []string{"test message 1", "... dropped tags", "test message 4", "test message 5"}
	if got := tagMessages(oe); !equalStrings(got, want) {
		t.Fatalf("cap-3 tags\n got=%v\nwant=%v", got, want)
	}
	// Node "should handle deep recursion": [L0 DROP L9 L10].
	d := Tag(errors.New("deep error"), "at level 0", nil)
	for i := 1; i <= 10; i++ {
		d = Tag(d, fmt.Sprintf("at level %d", i), nil)
	}
	do, _ := d.(*OError)
	wantD := []string{"at level 0", "... dropped tags", "at level 9", "at level 10"}
	if got := tagMessages(do); !equalStrings(got, wantD) {
		t.Fatalf("deep cap tags\n got=%v\nwant=%v", got, wantD)
	}
	// Rendered plain-error line (Go divergence pinned):
	if !strings.Contains(do.Error(), "deep error") {
		t.Fatalf("wrap lost message: %q", do.Error())
	}
}

func TestPlainErrorWrapAndReTag(t *testing.T) {
	// Go stand-in: a wrapped plain error carries the tag chain on a *OError.
	e := Tag(errors.New("foo"), "tagged", map[string]any{"k": 1})
	oe, ok := e.(*OError)
	if !ok {
		t.Fatalf("wrap lost")
	}
	if got, want := oe.Error(), "OError: foo"; got != want {
		t.Fatalf("render %q want %q (divergence pinned in HANDOFF.md)", got, want)
	}
	if got, want := GetFullStack(e), "OError: foo\nTaggedError: tagged"; got != want {
		t.Fatalf("stack %q want %q", got, want)
	}
	if got := GetFullInfo(e); !mapEqual(got, map[string]any{"k": 1}) {
		t.Fatalf("info %v", got)
	}
	// Re-tag on the same wrapped object (Node singleton).
	e2 := Tag(e, "again", map[string]any{"k2": 2})
	if e2 != any(oe) {
		t.Fatalf("identity lost")
	}
	if got := GetFullInfo(e2); !mapEqual(got, map[string]any{"k": 1, "k2": 2}) {
		t.Fatalf("re-tag info %v", got)
	}
}

func TestFullInfo(t *testing.T) {
	// Node: getFullInfo(null) → {}.
	if got := GetFullInfo(nil); !mapEmpty(got) {
		t.Fatalf("nil → %v", got)
	}
	// Go divergence: plain Go errors expose no .info, so a tag-only error's
	// full info IS the tag info.
	etag := Tag(errors.New("foo"), "bar", map[string]any{"userId": 123})
	if got := GetFullInfo(etag); !mapEqual(got, map[string]any{"userId": 123}) {
		t.Fatalf("tag info %v", got)
	}
	// Node: "merges info from an error and its tags".
	e := Of("foo").WithInfo(map[string]any{"projectId": 456})
	e.Tag("failed to foo", map[string]any{"userId": 123})
	if got := GetFullInfo(e); !mapEqual(got, map[string]any{"projectId": 456, "userId": 123}) {
		t.Fatalf("merge info/tag %v", got)
	}
	// Node: "merges info from a cause".
	cause := Of("bar").WithInfo(map[string]any{"userId": 123})
	outer := Of("foo").WithInfo(nil)
	outer.Cause = cause
	if got := GetFullInfo(outer); !mapEqual(got, map[string]any{"userId": 123}) {
		t.Fatalf("cause info %v", got)
	}
	// Node: "merges info from a nested cause".
	innerMost := Of("baz").WithInfo(map[string]any{"foo": 42})
	mid := Of("bar").WithCause(innerMost)
	depth := Of("foo").WithCause(mid).WithInfo(map[string]any{"userId": 123})
	if got := GetFullInfo(depth); !mapEqual(got, map[string]any{"userId": 123, "foo": 42}) {
		t.Fatalf("nested cause %v", got)
	}
	// Node: "merges info from cause with duplicate keys" (outer wins).
	ce := Of("bar").WithInfo(map[string]any{"userId": 1})
	dup := Of("foo").WithCause(ce).WithInfo(map[string]any{"userId": 42, "foo": 1337})
	if got := GetFullInfo(dup); !mapEqual(got, map[string]any{"userId": 42, "foo": 1337}) {
		t.Fatalf("dup keys %v", got)
	}
	// Node: "merges info from tags with duplicate keys" (later tag wins).
	eA := Tag(errors.New("foo"), "bar", map[string]any{"userId": 123})
	eB := Tag(eA, "bat", map[string]any{"userId": 456})
	if got := GetFullInfo(eB); !mapEqual(got, map[string]any{"userId": 456}) {
		t.Fatalf("tag dup %v", got)
	}
	// Node: `error.info = 'test'` (string) → {}; Go: WithInfo(nil) no-op.
	if got := GetFullInfo(Of("x").WithInfo(nil)); !mapEmpty(got) {
		t.Fatalf("noobj info %v", got)
	}
}

func TestFullStack(t *testing.T) {
	if got := GetFullStack(nil); got != "" {
		t.Fatalf("nil stack %q", got)
	}
	// Node: "works on a normal error" — its V8 stack has "Error: foo" +
	// frames; Go pins: plain error renders Error() text.
	if got, want := GetFullStack(errors.New("foo")), "foo"; got != want {
		t.Fatalf("plain %q want %q (Go plain error has no name prefix — divergence)", got, want)
	}
	// Node "works on an error with a cause".
	c := Of("bar")
	e := Of("foo").WithCause(c)
	if got, want := GetFullStack(e), "OError: foo\ncaused by:\n    OError: bar"; got != want {
		t.Fatalf("cause stack\n got=%q\nwant=%q", got, want)
	}
	// Node "works on both tags and causes".
	foo := Tag(errors.New("foo"), "failed to foo", map[string]any{"foo": 1})
	bar := Of("failed to bar").WithCause(foo)
	bat := Tag(bar, "failed to bat", map[string]any{"bat": 1})
	want := "OError: failed to bar\nTaggedError: failed to bat\ncaused by:\n    OError: foo\n    TaggedError: failed to foo"
	if got := GetFullStack(bat); got != want {
		t.Fatalf("tags+cause stack\n got=%q\nwant=%q", got, want)
	}
	// Info from wrapped cause picked up for BOTH the outer and the inner.
	if got := GetFullInfo(bat); !mapEqual(got, map[string]any{"bat": 1, "foo": 1}) {
		t.Fatalf("outer info %v", got)
	}
	// Node: `getFullInfo(error.cause)` — the inner cause, not the tagged wrapper.
	if got := GetFullInfo(foo); !mapEqual(got, map[string]any{"foo": 1}) {
		t.Fatalf("inner info %v", got)
	}
}

func TestDepthIndentation(t *testing.T) {
	// 4 spaces per cause level (Node indent × nesting depth).
	c3 := Of("c3")
	c2 := Of("c2").WithCause(c3)
	c1 := Of("c1").WithCause(c2)
	want := "OError: c1\ncaused by:\n    OError: c2\n    caused by:\n        OError: c3"
	if got := GetFullStack(c1); got != want {
		t.Fatalf("indent\n got=%q\nwant=%q", got, want)
	}
}

func TestNonErrorCause(t *testing.T) {
	e := New("f", nil, "not-an-error")
	if got, want := GetFullStack(e), "OError: f\ncaused by:\n    (no stack)"; got != want {
		t.Fatalf("string-cause stack %q want %q", got, want)
	}
}

func TestTagNameVariants(t *testing.T) {
	if got, want := Of("plain").Error(), "OError: plain"; got != want {
		t.Fatalf("default %q want %q", got, want)
	}
	if got, want := Of("plain").WithName("SubError").Error(), "SubError: plain"; got != want {
		t.Fatalf("named %q want %q", got, want)
	}
	// Named render through Tag's wrap (Go divergence: wrap is "OError: "
	// prefixed; WithName overrides that).
	oe := Tag(errors.New("foo"), "x", nil).(*OError)
	oe.WithName("MyErr")
	if got, want := oe.Error(), "MyErr: foo"; got != want {
		t.Fatalf("re-named wrap %q want %q (documented)", got, want)
	}
}

func TestTagEmptyMessage(t *testing.T) {
	e := Of("x")
	e.Tag("", nil)
	if got, want := e.Tags[0].Error(), "TaggedError"; got != want {
		t.Fatalf("empty-tag render %q want %q", got, want)
	}
}

func TestNilTagAndNilWithCause(t *testing.T) {
	if got := Tag(nil, "m", nil); got != nil {
		t.Fatalf("Tag(nil) must be nil")
	}
	// Node: `withCause(undefined)` leaves cause unset.
	e := New("x", map[string]any{"a": 1}, errors.New("original cause"))
	e.WithCause(nil)
	if got, want := e.Cause.(error).Error(), "original cause"; got != want {
		t.Fatalf("nil-cause overwrite %q want %q", got, want)
	}
	// Fresh error with nil cause through the variadic New:
	if Of("x").Cause != nil {
		t.Fatalf("New without cause must have nil cause")
	}
}

func TestUnwrapInterop(t *testing.T) {
	target := errors.New("target")
	e := Of("outer").WithCause(target)
	if !errors.Is(e, target) {
		t.Fatalf("errors.Is must follow Unwrap to the error cause")
	}
	var wrapped *OError
	if !errors.As(e, &wrapped) {
		t.Fatalf("errors.As must find *OError")
	}
	if wrapped.Message != "outer" {
		t.Fatalf("As target %+v", wrapped)
	}
	// Non-error cause: Unwrap() must return nil (safe for errors.Is walk).
	e2 := New("m", nil, "string-cause")
	var wrapped2 *OError
	if e2.Unwrap() != nil {
		t.Fatalf("non-error cause must not Unwrap")
	}
	if !errors.As(e2, &wrapped2) {
		t.Fatalf("As on non-error-cause")
	}
}

// --- helpers ---

func tagMessages(e *OError) []string {
	if e == nil || e.Tags == nil {
		return nil
	}
	out := make([]string, len(e.Tags))
	for i, tg := range e.Tags {
		out[i] = tg.Message
	}
	return out
}

func splitLines(s string) []string { return strings.Split(s, "\n") }

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mapEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || v != w {
			return false
		}
	}
	return true
}

func mapEmpty(m map[string]any) bool { return len(m) == 0 }
