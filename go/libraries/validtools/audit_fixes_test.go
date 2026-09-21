package validtools

// audit_fixes_test.go — regression pins for the owner-audit fixes, each
// verified against the LIVE Node oracle (Node 22 + installed zod 4.1.11 +
// zod-validation-error 4.0.1) before being applied. The weak-LLM suite that
// shipped with this package passed its own tests while pinning behaviour
// that contradicted the oracle on exactly these cases.

import (
	"errors"
	"testing"
)

// errErrorValue is a plain Go error used as a foreign input value
// (Node oracle: an Error instance input renders "received error").
type errErrorValue struct{ err error }

func (e errErrorValue) Error() string { return e.err.Error() }

var foreignErr = errErrorValue{err: errors.New("foreign error value")}

// TestAuditFriendlyArrayElement — the Node oracle for
// validateSchema(z.object({list: z.array(z.number())}), {list:[1,'two']})
// throws:  "1" - Invalid input: expected number, received string
//
// The pre-fix getPathValue could not traverse []any, so a DEFINED element
// value was misreported as absent → the wrong message `"1" is required`.
func TestAuditFriendlyArrayElement(t *testing.T) {
	so := NewObject(Field{Name: "list", Schema: StringArrayVal{Item: NumberVal{}}})
	data := map[string]any{"list": []any{1.0, "two"}}
	_, iss := so.Validate(true, data)
	got := NewValidationError(&ZodError{Issues: iss}, data).Error()
	want := `"1" - Invalid input: expected number, received string`
	if got != want {
		t.Fatalf("friendly = %q, want %q", got, want)
	}
}

// TestAuditFriendlyArrayElementMissingField — element object with a MISSING
// required field: the value at list.0 IS defined (the object), but the
// inner field is absent → Node: "n" is required (the walk short-circuits
// at the missing inner key).
func TestAuditFriendlyArrayElementMissingField(t *testing.T) {
	so := NewObject(Field{Name: "list", Schema: StringArrayVal{
		Item: NewStrictObject(Field{Name: "n", Schema: NumberVal{}}),
	}})
	data := map[string]any{"list": []any{map[string]any{"m": 1.0}}}
	_, iss := so.Validate(true, data)
	got := NewValidationError(&ZodError{Issues: iss}, data).Error()
	want := `"n" is required`
	if got != want {
		t.Fatalf("friendly = %q, want %q", got, want)
	}
}

// TestAuditFriendlyArrayElementNullValue — Node's getPathValue returns the
// value (even JSON null) when the key EXISTS: `value === undefined` is the
// required-test, and null is NOT undefined → "x" - ... (not "is required").
func TestAuditFriendlyArrayElementNullValue(t *testing.T) {
	so := NewObject(Field{Name: "list", Schema: StringArrayVal{Item: NumberVal{}}})
	data := map[string]any{"list": []any{nil}}
	_, iss := so.Validate(true, data)
	got := NewValidationError(&ZodError{Issues: iss}, data).Error()
	want := `"0" - Invalid input: expected number, received null`
	if got != want {
		t.Fatalf("friendly = %q, want %q", got, want)
	}
}

// TestAuditEnumSingleValue — oracle: z.enum(['only']).parse('nope') →
// `Invalid input: expected "only"` (the pre-fix Go wording used the
// multi-value "Invalid option: expected one of ..." form for one value).
func TestAuditEnumSingleValue(t *testing.T) {
	_, iss1 := EnumVal{Values: []string{"only"}}.Validate(true, "nope")
	if got, want := Wire(iss1), `Validation error: Invalid input: expected "only"`; got != want {
		t.Fatalf("wire = %q, want %q", got, want)
	}
	// Two values keep the "one of" form (unchanged).
	_, iss2 := EnumVal{Values: []string{"a", "b"}}.Validate(true, "c")
	if got, want := Wire(iss2), `Validation error: Invalid option: expected one of "a"|"b"`; got != want {
		t.Fatalf("wire = %q, want %q", got, want)
	}
}

// TestAuditObjectNonStrict — oracle: z.object({a: z.string()}).parse({a:'x',
// zzz:1}) → OK, keys=a (unknown keys silently DROPPED, no issue). The
// pre-fix package only offered StrictObject, which 4xx's here.
func TestAuditObjectNonStrict(t *testing.T) {
	val, iss := NewObject(Field{Name: "a", Schema: StringVal{}}).
		Validate(true, map[string]any{"a": "x", "zzz": 1.0})
	if len(iss) != 0 {
		t.Fatalf("non-strict must emit no issues, got %v", iss)
	}
	out := val.(map[string]any)
	if len(out) != 1 || out["a"] != "x" {
		t.Fatalf("unknown key must be dropped: %v", out)
	}
	// ...while StrictObject still flags it (unchanged).
	_, siss := NewStrictObject(Field{Name: "a", Schema: StringVal{}}).
		Validate(true, map[string]any{"a": "x", "zzz": 1.0})
	if len(siss) != 1 || siss[0].Code != "unrecognized_keys" {
		t.Fatalf("strict must emit the unrecognized_keys issue, got %v", siss)
	}
}

// TestAuditUtf16Length — zod's min/max compare JS string lengths: UTF-16
// CODE UNITS. "😀" is 4 bytes in Go (old code: passes min 3) but 2 units
// (Node: fails min 3). Oracle-verified unit counts.
func TestAuditUtf16Length(t *testing.T) {
	if _, iss := (StringVal{Min: 3}).Validate(true, "😀"); len(iss) != 1 {
		t.Fatalf(`"😀" is 2 UTF-16 units: min 3 must fail, got %v`, iss)
	}
	if _, iss := (StringVal{Min: 3}).Validate(true, "😀😀"); len(iss) != 0 {
		t.Fatalf(`"😀😀" is 4 UTF-16 units: min 3 must pass, got %v`, iss)
	}
	// ASCII unchanged.
	if _, iss := (StringVal{Min: 3}).Validate(true, "ab"); len(iss) != 1 {
		t.Fatalf(`"ab" min 3 must fail, got %v`, iss)
	}
	if _, iss := (StringVal{Min: 3}).Validate(true, "abcd"); len(iss) != 0 {
		t.Fatalf(`"abcd" min 3 must pass, got %v`, iss)
	}
	// EventName: ASCII behaviour unchanged.
	if _, iss := EventName().Validate(true, "a"); len(iss) != 0 {
		t.Fatalf(`"a" eventName must pass, got %v`, iss)
	}
}

// TestAuditSafePathNonASCIIWhitespace — V8's `\s` (probed on Node 22 with
// the installed zodHelpers.js) for BAD_SEGMENT_RX = /^\.$|^\s|\s$/:
// REJECTS leading/trailing NBSP, IDEOGRAPHIC SPACE, LS, PS, HAIR SPACE;
// ACCEPTS mid-segment whitespace and U+180E / U+200B (not `\s` in this V8;
// nor BAD_CHAR_RX members). The old byte-level Go check missed the whole
// non-ASCII set → silent accepts where Node rejects (a safe boundary).
func TestAuditSafePathNonASCIIWhitespace(t *testing.T) {
	for _, s := range []string{
		"\u00a0foo.tex",   // NBSP lead
		"foo\u3000/b.tex", // IDEOGRAPHIC SPACE trail (segment "foo\u3000")
		"\u3000a/b.tex",   // IDEOGRAPHIC SPACE lead
		"a.b\u2028",       // LS trail
		"\u2029end/x.tex", // PS lead
		"x\u200a",         // HAIR SPACE trail
	} {
		if _, iss := SafePath().Validate(true, s); len(iss) == 0 {
			t.Fatalf("%q must fail (Node BAD_SEGMENT_RX), got pass", s)
		}
	}
	for _, s := range []string{
		"\u180efoo.tex", // U+180E is not \s in this V8
		"\u200bfoo.tex", // U+200B is not \s
		"a\u2028c.tex",  // LS mid-segment (only edges are tested)
		"fo\u00a0o.tex", // NBSP mid-segment
		"lead\u2000b",   // EN SPACE mid-segment
	} {
		if _, iss := SafePath().Validate(true, s); len(iss) != 0 {
			t.Fatalf("%q is accepted by Node; Go must match, got %v", s, iss)
		}
	}
}

// TestAuditReceivedErrorType — Node getTypeName: an Error instance renders
// as "received error" (not "received object").
func TestAuditReceivedErrorType(t *testing.T) {
	_, iss := StringVal{}.Validate(true, foreignErr)
	if got, want := Wire(iss), "Validation error: Invalid input: expected string, received error"; got != want {
		t.Fatalf("wire = %q, want %q", got, want)
	}
}
