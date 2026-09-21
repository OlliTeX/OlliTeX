package validtools

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestWireGoldens pins the Go port byte-for-byte against the Node oracle in
// testdata/goldens.json (captured live from installed zod 4.1.11 +
// zod-validation-error 4.0.1 — see HANDOFF.md for the capture script).
//
// Label → Go construction is a hand-written oracle mapping (the Node
// goldens.mjs label set, unchanged). Rendering contracts:
//   - leaf / strict goldens: Wire(issues), or "OK <name>" where <name> is
//     the JS type name of the parsed value (time.Time → "object": z.iso
//     transforms Dates, which JSON.stringifies as objects);
//     dtNullish undefined-passthrough: "OK data=<json>".
//   - http goldens: "<code> <body>"; next(err) ⇒ `undefined {"next":true}`.
//   - validateSchema goldens: NewValidationError(...).Error().
func TestWireGoldens(t *testing.T) {
	var g map[string]string
	raw, err := os.ReadFile(filepath.Join("testdata", "goldens.json"))
	if err != nil {
		t.Fatalf("read goldens: %v", err)
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("goldens: %v", err)
	}
	if len(g) == 0 {
		t.Fatal("empty goldens file")
	}
	for label, want := range g {
		label, want := label, want
		t.Run(label, func(t *testing.T) {
			if got := goldenWire(label); got != want {
				t.Fatalf("wire mismatch\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

func goldenWire(label string) string {
	if strings.HasPrefix(label, "http ") {
		return httpGolden(label)
	}
	if strings.HasPrefix(label, "validateSchema") {
		ze, data := validateSchemaGolden(label)
		return NewValidationError(ze, data).Error()
	}
	val, iss := leafCase(label)
	if len(iss) > 0 {
		return Wire(iss)
	}
	if label == "dtNullish undefined-passthrough" {
		b, _ := json.Marshal(val)
		return "OK data=" + string(b)
	}
	return "OK " + okName(val)
}

// okName renders the Node harness' `typeof r.data` for the OK prefix.
func okName(v any) string {
	if _, ok := v.(time.Time); ok {
		return "object" // z.iso.datetime transforms to Date ⇒ "object"
	}
	if v == nil {
		return "null"
	}
	return jsTypeName(v)
}

// leafCase resolves (value, issues) for every non-http/non-validateSchema
// golden label.
func leafCase(label string) (any, []Issue) {
	switch label {
	case "strict missing":
		so := NewStrictObject(
			Field{Name: "project_id", Schema: ObjectID()},
			Field{Name: "name", Schema: StringVal{}},
		)
		return so.Validate(true, map[string]any{"project_id": "507f1f77bcf86cd799439011"})
	case "strict bad oid":
		so := NewStrictObject(
			Field{Name: "project_id", Schema: ObjectID()},
			Field{Name: "name", Schema: StringVal{}},
		)
		return so.Validate(true, map[string]any{"name": "x", "project_id": "aa"})
	case "strict unk":
		so := NewStrictObject(
			Field{Name: "project_id", Schema: ObjectID()},
			Field{Name: "name", Schema: StringVal{}},
		)
		return so.Validate(true, map[string]any{
			"project_id": "507f1f77bcf86cd799439011", "name": "x", "extra": 1.0,
		})
	case "deep missing":
		so := NewStrictObject(Field{Name: "body", Schema: NewStrictObject(Field{Name: "x", Schema: StringVal{}})})
		return so.Validate(true, map[string]any{"body": map[string]any{}})
	case "deep bad oid":
		so := NewStrictObject(Field{Name: "body", Schema: NewStrictObject(Field{Name: "id", Schema: ObjectID()})})
		return so.Validate(true, map[string]any{"body": map[string]any{"id": "nope"}})
	case "dt absent(strict)":
		so := NewStrictObject(
			Field{Name: "a", Schema: StringVal{}},
			Field{Name: "dt", Schema: DTAdapterVal{Datetime(DatetimeOpts{})}},
		)
		return so.Validate(true, map[string]any{"a": "x"})
	case "dt null(strict)":
		so := NewStrictObject(
			Field{Name: "a", Schema: StringVal{}},
			Field{Name: "dt", Schema: DTAdapterVal{Datetime(DatetimeOpts{})}},
		)
		return so.Validate(true, map[string]any{"a": "x", "dt": nil})
	case "dt off:true absent":
		so := NewStrictObject(
			Field{Name: "a", Schema: StringVal{}},
			Field{Name: "dt", Schema: DTAdapterVal{Datetime(DatetimeOpts{Offset: true})}},
		)
		return so.Validate(true, map[string]any{"a": "x"})
	case "dt absent nullable":
		so := NewStrictObject(
			Field{Name: "a", Schema: StringVal{}},
			Field{Name: "dt", Schema: DTAdapterVal{DatetimeNullable(DatetimeOpts{})}},
		)
		return so.Validate(true, map[string]any{"a": "x"})
	case "dt absent nullish":
		so := NewStrictObject(
			Field{Name: "a", Schema: StringVal{}},
			Field{Name: "dt", Schema: DTAdapterVal{DatetimeNullish(DatetimeOpts{})}},
		)
		return so.Validate(true, map[string]any{"a": "x"})
	case "dtNullable null":
		so := NewStrictObject(
			Field{Name: "a", Schema: StringVal{}},
			Field{Name: "dt", Schema: DTAdapterVal{DatetimeNullable(DatetimeOpts{})}},
		)
		return so.Validate(true, map[string]any{"a": "x", "dt": nil})
	case "dtNullish undefined-passthrough":
		so := NewStrictObject(
			Field{Name: "a", Schema: StringVal{}},
			Field{Name: "dt", Schema: DTAdapterVal{DatetimeNullish(DatetimeOpts{})}},
		)
		// The Node golden's input has `dt` ABSENT (undefined) — not an explicit
		// null — and nullish collapses an absent key to undefined (dropped).
		// (An explicit null under nullish is a DIFFERENT case: nullish has
		// allowNull, so it is preserved as null — pinned in TestDatetimeNullishExplicitNull.)
		return so.Validate(true, map[string]any{"a": "x"})
	}
	v, in := leafArgs(label)
	return v.Validate(true, in)
}

// leafArgs is the (schema, input) table for the flat zz-leaf goldens.
func leafArgs(label string) (Val, any) {
	switch label {
	case "objectId bad":
		return ObjectID(), "aa"
	case "objectId ok":
		return ObjectID(), "507f1f77bcf86cd799439011"
	case "buildId ok":
		return BuildID(), "19d6c341530-878fff6cdab7fb0c"
	case "buildId bad":
		return BuildID(), "aa"
	case "editorBuildId ok":
		return EditorBuildID(), "03b1d773-6203-4669-b365-6a0aa5625878-19d6c341530-878fff6cdab7fb0c"
	case "editorBuildId bad":
		return EditorBuildID(), "19d6c341530-878fff6cdab7fb0c"
	case "hex empty":
		return Hex(), ""
	case "hex bad":
		return Hex(), "g"
	case "submissionId bad":
		return SubmissionID(), "a/b"
	case "submissionId ok":
		return SubmissionID(), "a_b-9"
	case "compileGroup custom":
		return CompileGroup(), "nope"
	case "compileGroup default":
		return EnumVal{Values: []string{"alpha", "gvisor", "standard", "priority"}}, "nope"
	case "int nonneg 1.5":
		return NumberVal{Integer: true, NonNegative: true}, 1.5
	case "int nonneg -1":
		return NumberVal{Integer: true, NonNegative: true}, -1.0
	case "int nonneg str":
		return NumberVal{Integer: true, NonNegative: true}, "x"
	case "dt ok":
		return DTAdapterVal{Datetime(DatetimeOpts{})}, "2024-01-01T12:00:00Z"
	case "dt ok offset:true +05:30":
		return DTAdapterVal{Datetime(DatetimeOpts{Offset: true})}, "2024-01-01T12:00:00+05:30"
	case "dt offset:true no offset":
		return DTAdapterVal{Datetime(DatetimeOpts{Offset: true})}, "2024-01-01T12:00:00Z"
	case "dt offset:false with offset":
		return DTAdapterVal{Datetime(DatetimeOpts{Offset: false})}, "2024-01-01T12:00:00+05:30"
	case "dt fraction":
		return DTAdapterVal{Datetime(DatetimeOpts{})}, "2024-01-01T12:00:00.123Z"
	case "dt no seconds":
		return DTAdapterVal{Datetime(DatetimeOpts{})}, "2024-01-01T12:00Z"
	case "dt month 13":
		return DTAdapterVal{Datetime(DatetimeOpts{})}, "2024-13-01T12:00:00Z"
	case "dt leap 2024-02-29 ok":
		return DTAdapterVal{Datetime(DatetimeOpts{})}, "2024-02-29T12:00:00Z"
	case "dt leap 2023-02-29 bad":
		return DTAdapterVal{Datetime(DatetimeOpts{})}, "2023-02-29T12:00:00Z"
	case "dt lowercase z":
		return DTAdapterVal{Datetime(DatetimeOpts{})}, "2024-01-01T12:00:00z"
	case "clsiServerId ok":
		return CLSIServerID(), "compile-1"
	case "clsiServerId bad":
		return CLSIServerID(), "Compile_1"
	case "compileBackendClass bad":
		return CompileBackendClass(), "a_b"
	case "projectHistoryId ok oid":
		return ProjectHistoryID(), "507f1f77bcf86cd799439011"
	case "projectHistoryId ok int":
		return ProjectHistoryID(), "12345"
	case "projectHistoryId lead zero":
		return ProjectHistoryID(), "0123"
	case "projectHistoryId traversal":
		return ProjectHistoryID(), "a/../../etc"
	case "chunkId bad":
		return ChunkID(), "a-b"
	case "splitTestName short":
		return SplitTestName(), "ab"
	case "splitTestName badcase":
		return SplitTestName(), "Not_Valid"
	case "variantName ok":
		return VariantName(), "variant-1"
	case "eventName min+regex both(empty)":
		return EventName(), ""
	case "eventName long":
		return EventName(), repeatN("a", 241)
	case "eventName ok 240":
		return EventName(), repeatN("a", 240)
	case "filepath empty":
		return Filepath(), ""
	case "filepath absolute":
		return Filepath(), "/output.pdf"
	case "filepath traversal":
		return Filepath(), "../output.pdf"
	case "filepath ok":
		return Filepath(), "foo/output.pdf"
	case "routeSegment empty":
		return RouteSegment(), ""
	case "routeSegment sep":
		return RouteSegment(), "foo/bar"
	case "routeSegment query":
		return RouteSegment(), "foo?bar=1"
	case "routeSegment frag":
		return RouteSegment(), "foo#bar"
	case "routeSegment dotdot":
		return RouteSegment(), ".."
	case "routeSegment dot":
		return RouteSegment(), "."
	case "safePath empty":
		return SafePath(), ""
	case "safePath ok root":
		return SafePath(), "/output.pdf"
	case "safePath folder":
		return SafePath(), "foo/"
	case "safePath traversal":
		return SafePath(), "../output.pdf"
	case "safePath lone dot":
		return SafePath(), "."
	case "safePath lead ws":
		return SafePath(), " foobar.tex"
	case "safePath null byte":
		return SafePath(), "\x00foo"
	case "safePath c1":
		return SafePath(), "foo\u0090.tex"
	case "safePath astrisk":
		return SafePath(), "foo*.tex"
	case "safePath backslash":
		return SafePath(), `foo\bar.tex`
	case "safePath proto nested ok":
		return SafePath(), "__proto__/output.pdf"
	case "safePath constructor":
		return SafePath(), "constructor"
	case "safePath hasown":
		return SafePath(), "hasOwnProperty"
	case "safePath toString nested ok":
		return SafePath(), "foo/toString"
	}
	panic("unmapped golden: " + label)
}

func repeatN(s string, n int) string {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

// httpGolden runs CreateHandleValidationError through a recorder for the
// three HTTP goldens (Node harness rendered `<code> <body>`; next ran
// renders `undefined {"next":true}`).
func httpGolden(label string) string {
	idIssue := func() []Issue {
		so := NewStrictObject(Field{Name: "id", Schema: ObjectID()})
		_, iss := so.Validate(true, map[string]any{"id": "aa"})
		return iss
	}
	nextCalled := false
	next := func(err error) { nextCalled = true }
	rec := httptest.NewRecorder()
	switch label {
	case "http InvalidRequest":
		HandleValidationError(NewInvalidRequestError(&ZodError{Issues: idIssue()}), rec, next)
	case "http InvalidParams":
		HandleValidationError(NewInvalidParamsError(&ZodError{Issues: idIssue()}), rec, next)
	case "http other":
		HandleValidationError(errors.New("boom"), rec, next)
	}
	if nextCalled {
		return `undefined {"next":true}`
	}
	return strconv.Itoa(rec.Code) + " " + rec.Body.String()
}

// validateSchemaGolden builds the (*ZodError, raw data) pair for the
// validateSchema goldens.
func validateSchemaGolden(label string) (*ZodError, any) {
	switch label {
	case "validateSchema missing(req)":
		so := NewStrictObject(Field{Name: "body", Schema: NewStrictObject(Field{Name: "name", Schema: StringVal{}})})
		_, iss := so.Validate(true, map[string]any{})
		return &ZodError{Issues: iss}, map[string]any{}
	case "validateSchema absent field":
		so := NewStrictObject(
			Field{Name: "name", Schema: StringVal{}},
			Field{Name: "age", Schema: NumberVal{NonNegative: true}},
		)
		_, iss := so.Validate(true, map[string]any{})
		return &ZodError{Issues: iss}, map[string]any{}
	case "validateSchema present bad":
		so := NewStrictObject(
			Field{Name: "name", Schema: StringVal{}},
			Field{Name: "age", Schema: NumberVal{NonNegative: true}},
		)
		_, iss := so.Validate(true, map[string]any{"name": "x", "age": -1.0})
		return &ZodError{Issues: iss}, map[string]any{"name": "x", "age": -1.0}
	case "validateSchema root":
		_, iss := StringVal{}.Validate(true, 7)
		return &ZodError{Issues: iss}, 7
	case "validateSchema union msg":
		so := NewStrictObject(Field{Name: "dt", Schema: DTAdapterVal{Datetime(DatetimeOpts{})}})
		_, iss := so.Validate(true, map[string]any{})
		return &ZodError{Issues: iss}, map[string]any{}
	case "validateSchema empty path?":
		_, iss := DTAdapterVal{Datetime(DatetimeOpts{})}.Validate(false, nil)
		return &ZodError{Issues: iss}, nil
	case "validateSchema deep path":
		so := NewStrictObject(Field{Name: "body", Schema: NewStrictObject(
			Field{Name: "ids", Schema: StringArrayVal{Item: ObjectID()}},
		)})
		data := map[string]any{"body": map[string]any{"ids": []any{"nope"}}}
		_, iss := so.Validate(true, data)
		return &ZodError{Issues: iss}, data
	}
	panic("unmapped validateSchema golden: " + label)
}
