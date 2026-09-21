package validtools

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestErrors(t *testing.T) {
	ze := &ZodError{Issues: []Issue{NewIssue("custom", "msg")}}
	reqErr := NewInvalidRequestError(ze)
	if reqErr.Error() != "Invalid request" {
		t.Fatalf("request Error(): %q", reqErr.Error())
	}
	if reqErr.ZodError != ze {
		t.Fatal("promoted *ZodError lost (request)")
	}
	paramsErr := NewInvalidParamsError(ze)
	if paramsErr.Error() != "Invalid request parameters" {
		t.Fatalf("params Error(): %q", paramsErr.Error())
	}
	if paramsErr.ZodError != ze {
		t.Fatal("promoted *ZodError lost (params)")
	}
	var re *InvalidRequestError
	var pe *InvalidParamsError
	if !errors.As(reqErr, &re) {
		t.Fatal("errors.As(InvalidRequestError) failed")
	}
	if !errors.As(NewInvalidParamsError(ze), &pe) {
		t.Fatal("errors.As(InvalidParamsError) failed")
	}
	if e := errors.Join(reqErr, errors.New("more")); !errors.As(e, &re) {
		t.Fatal("errors.As through errors.Join failed")
	}
	ve := NewValidationError(ze, map[string]any{})
	if ve.Error() != "msg" {
		t.Fatalf("ValidationError: %q", ve.Error())
	}
	if ve.ZodError() != ze {
		t.Fatal("ValidationError ZodError() promotion lost")
	}
}

func TestAsNumber(t *testing.T) {
	cases := []any{
		float64(7), float32(3), int(1), int8(1), int16(1), int32(1), int64(1),
		uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
		math.Inf(1), math.Inf(-1), math.NaN(), 0.0, -1.5,
	}
	for _, v := range cases {
		f, ok := asNumber(v)
		if !ok {
			t.Fatalf("asNumber(%v) not ok", v)
		}
		switch n := v.(type) {
		case float32:
			if f != float64(n) {
				t.Fatalf("float32 mismatch: %v", f)
			}
		case int:
			if f != float64(n) {
				t.Fatalf("int mismatch: %v", f)
			}
		case int64:
			if f != float64(n) {
				t.Fatalf("int64 mismatch: %v", f)
			}
		}
		if (f != f) != isNan(v) {
			t.Fatalf("nan mismatch for %v: %v", v, f)
		}
	}
	if _, ok := asNumber("x"); ok {
		t.Fatal("asNumber(string) should fail")
	}
	if _, ok := asNumber(nil); ok {
		t.Fatal("asNumber(nil) should fail")
	}
	if _, ok := asNumber(true); ok {
		t.Fatal("asNumber(bool) should fail")
	}
}

func isNan(v any) bool {
	f, ok := asNumber(v)
	return ok && f != f
}

func TestNumberIntNonnegative(t *testing.T) {
	if NumberIntNonnegative() != (NumberVal{Integer: true, NonNegative: true}) {
		t.Fatal("NumberIntNonnegative != NumberVal{...}")
	}
	if got, iss := NumberIntNonnegative().Validate(true, 0.0); len(iss) != 0 || got != 0.0 {
		t.Fatalf("zero: %v %v", got, iss)
	}
	if got, iss := NumberIntNonnegative().Validate(true, 3.0); len(iss) != 0 || got != 3.0 {
		t.Fatalf("three: %v %v", got, iss)
	}
	if got, iss := NumberIntNonnegative().Validate(true, 3.5); len(iss) != 1 || iss[0].Code != "invalid_type" {
		t.Fatalf("int fail: %v %v", got, iss)
	}
	if got, iss := NumberIntNonnegative().Validate(true, -1.0); len(iss) != 1 || iss[0].Code != "too_small" {
		t.Fatalf("neg fail: %v %v", got, iss)
	}
	if !isFloat64Integer(3.0) {
		t.Fatal("isFloat64Integer(3.0) false")
	}
	if isFloat64Integer(3.5) {
		t.Fatal("isFloat64Integer(3.5) true")
	}
}

func TestPathSegString(t *testing.T) {
	if got := StrSeg("body").String(); got != "body" {
		t.Fatalf("StrSeg String(): %q", got)
	}
	if got := IndexSeg(7).String(); got != "7" {
		t.Fatalf("IndexSeg String(): %q", got)
	}
}

func TestNewValidationErrorRoot(t *testing.T) {
	ze := &ZodError{Issues: []Issue{NewIssue("invalid_type", "Invalid input: expected string, received number")}}
	ve := NewValidationError(ze, 7)
	if ve.Error() != "Invalid input: expected string, received number" {
		t.Fatalf("root scalar: %q", ve.Error())
	}
	ve2 := NewValidationError(ze, nil)
	if ve2.Error() != "Invalid input: expected string, received number" {
		t.Fatalf("nil data: %q", ve2.Error())
	}
	if ve2.ZodError() != ze {
		t.Fatal("ZodError promotion lost")
	}
}

func TestWireDeepPath(t *testing.T) {
	ze := &ZodError{Issues: []Issue{
		{Path: []PathSeg{StrSeg("body"), IndexSeg(0)}, Code: "custom", Message: "bad oid"},
	}}
	if got := Wire(ze.Issues); got != `Validation error: Bad oid at "body[0]"` {
		t.Fatalf("deep path: %q", got)
	}
}

func TestNewUnrecognizedKeysIssue(t *testing.T) {
	i := NewUnrecognizedKeysIssue([]string{"b", "a"})
	if got := Wire([]Issue{i}); got != `Validation error: Unrecognized keys: "b", "a"` {
		t.Fatalf("unrec keys: %q", got)
	}
}

func TestIssueAtPrepending(t *testing.T) {
	i := NewIssue("custom", "bad", IndexSeg(0))
	i2 := i.At(StrSeg("body"))
	if len(i2.Path) != 2 {
		t.Fatalf("At path length: %d", len(i2.Path))
	}
	if !i2.Path[1].IsIndex {
		t.Fatalf("At index flag: %#v", i2.Path)
	}
	_ = i2
}

func TestNewValidationErrorEmptyPath(t *testing.T) {
	ze := &ZodError{Issues: []Issue{NewIssue("invalid_format", "Invalid ISO datetime")}}
	ve := NewValidationError(ze, nil)
	if ve.Error() != "Invalid ISO datetime" {
		t.Fatalf("empty path: %q", ve.Error())
	}
}

func TestNewValidationErrorRequired(t *testing.T) {
	ze := &ZodError{Issues: []Issue{
		{Path: []PathSeg{StrSeg("name")}, Code: "invalid_type", Message: "Invalid input: expected string, received undefined"},
	}}
	ve := NewValidationError(ze, map[string]any{})
	if ve.Error() != `"name" is required` {
		t.Fatalf("required: %q", ve.Error())
	}
}

func TestNewValidationErrorNonRequired(t *testing.T) {
	ze := &ZodError{Issues: []Issue{
		{Path: []PathSeg{StrSeg("name")}, Code: "invalid_type", Message: "Invalid input: expected number, received string"},
	}}
	ve := NewValidationError(ze, map[string]any{"name": "x"})
	if ve.Error() != `"name" - Invalid input: expected number, received string` {
		t.Fatalf("non-required: %q", ve.Error())
	}
}

func TestNewValidationErrorMulti(t *testing.T) {
	ze := &ZodError{Issues: []Issue{
		{Code: "custom", Message: "first"},
		{Path: []PathSeg{StrSeg("k")}, Code: "custom", Message: "second"},
	}}
	ve := NewValidationError(ze, map[string]any{"k": "v"})
	if got := ve.Error(); got != "first; \"k\" - second" {
		t.Fatalf("multi: %q", got)
	}
}

func TestNewValidationErrorDeepIndexPath(t *testing.T) {
	ze := &ZodError{Issues: []Issue{
		{Path: []PathSeg{IndexSeg(0)}, Code: "custom", Message: "invalid Mongo ObjectId"},
	}}
	ve := NewValidationError(ze, map[string]any{"ids": []any{"nope"}})
	if got := ve.Error(); got != `"0" - invalid Mongo ObjectId` {
		t.Fatalf("deep index: %q", got)
	}
}

func TestIsJSMultiTrim(t *testing.T) {
	if isJSMultiTrim("") {
		t.Fatal("isJSMultiTrim('') true")
	}
	if !isJSMultiTrim(" x") {
		t.Fatal("isJSMultiTrim(' x') false")
	}
	if !isJSMultiTrim("x ") {
		t.Fatal("isJSMultiTrim('x ') false")
	}
	if isJSMultiTrim("x") {
		t.Fatal("isJSMultiTrim('x') true")
	}
	if !isJSMultiTrim(" tab") {
		t.Fatal("isJSMultiTrim(' tab') false (tab)")
	}
	if !isJSMultiTrim("x\n") {
		t.Fatal("isJSMultiTrim('x\\n') false (newline)")
	}
}

func TestHasDotDotSegment(t *testing.T) {
	if !hasDotDotSegment("..") {
		t.Fatal("hasDotDotSegment('..') false")
	}
	if hasDotDotSegment("a/b") {
		t.Fatal("hasDotDotSegment('a/b') true")
	}
	if !hasDotDotSegment("a/../b") {
		t.Fatal("hasDotDotSegment('a/../b') false")
	}
}

func TestCompileGroupAndBackendClass(t *testing.T) {
	cbl := CompileBackendClass()
	if got, iss := cbl.Validate(true, "alpha"); len(iss) != 0 || got != "alpha" {
		t.Fatalf("cbc ok: %v %v", got, iss)
	}
	if got, iss := cbl.Validate(true, "a_b"); len(iss) != 1 || iss[0].Message != "invalid compileBackendClass" {
		t.Fatalf("cbc bad: %v %v", got, iss)
	}
	cgCustom := CompileGroup()
	if got, iss := cgCustom.Validate(true, "nope"); len(iss) != 1 || iss[0].Message != "invalid compileGroup" {
		t.Fatalf("cg custom: %v %v", got, iss)
	}
	if got, iss := cbl.Validate(true, 1.0); len(iss) != 1 || iss[0].Code != "invalid_type" {
		t.Fatalf("cbc type: %v %v", got, iss)
	}
}

func TestNumberValBoolAndZero(t *testing.T) {
	if got, iss := (NumberVal{}).Validate(true, true); len(iss) != 1 || iss[0].Code != "invalid_type" {
		t.Fatalf("bool: %v %v", got, iss)
	}
	if got, iss := (NumberVal{}).Validate(true, float64(0)); len(iss) != 0 || got != 0.0 {
		t.Fatalf("zero: %v %v", got, iss)
	}
	got, iss := (NumberVal{NonNegative: true}).Validate(true, -0.5)
	if len(iss) != 1 || !strings.Contains(iss[0].Message, "number to be >=0") {
		t.Fatalf("neg: %v %v", got, iss)
	}
}

func TestEnumValModes(t *testing.T) {
	ev := EnumVal{Values: []string{"a", "b"}}
	if got, iss := ev.Validate(true, "a"); len(iss) != 0 || got != "a" {
		t.Fatalf("member: %v %v", got, iss)
	}
	if got, iss := ev.Validate(true, "c"); len(iss) != 1 || iss[0].Code != "invalid_value" ||
		iss[0].Message != `Invalid option: expected one of "a"|"b"` {
		t.Fatalf("default msg: %v %v", got, iss)
	}
	if got, iss := ev.Validate(true, nil); len(iss) != 1 {
		t.Fatalf("null: %v %v", got, iss)
	}
	if got, iss := ev.Validate(true, 1.0); len(iss) != 1 {
		t.Fatalf("number: %v %v", got, iss)
	}
	if got, iss := (EnumVal{Values: []string{"a"}, Message: "custom"}).Validate(true, "z"); len(iss) != 1 || iss[0].Message != "custom" {
		t.Fatalf("custom: %v %v", got, iss)
	}
}
