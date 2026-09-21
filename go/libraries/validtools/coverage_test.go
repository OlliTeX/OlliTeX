package validtools

import (
	"testing"
	"time"
)

// Coverage-gaps test: the branch complement to the 85 oracle goldens -- the
// absent/null/wrong-type arms of every leaf, the StrictObject typed/ordered/
// nullish arms, the datetime Local + time.Time arms, the jsTypeName special
// values, and the friendly getPathValue early exits. Messages pinned to the
// zod 4.1.11 oracle (probed: absent -> "received undefined", null ->
// "received null", wrong type -> the primitive's typing issue; refine leaves
// share their primitive's typing layer).

func TestCovLeafAbsentNullWrong(t *testing.T) {
	leaves := []struct {
		name string
		v    Val
	}{
		{"objectId", ObjectID()},
		{"hex", Hex()},
		{"buildId", BuildID()},
		{"editorBuildId", EditorBuildID()},
		{"clsiServerId", CLSIServerID()},
		{"submissionId", SubmissionID()},
		{"filePath", Filepath()},
		{"safePath", SafePath()},
		{"routeSegment", RouteSegment()},
		{"projectHistoryId", ProjectHistoryID()},
		{"chunkId", ChunkID()},
		{"splitTestName", SplitTestName()},
		{"variantName", VariantName()},
		{"eventName", EventName()},
	}
	for _, l := range leaves {
		if _, iss := l.v.Validate(false, nil); len(iss) != 1 || iss[0].Message != "Invalid input: expected string, received undefined" {
			t.Fatalf("%s absent: %v", l.name, iss)
		}
		if _, iss := l.v.Validate(true, nil); len(iss) != 1 || iss[0].Message != "Invalid input: expected string, received null" {
			t.Fatalf("%s null: %v", l.name, iss)
		}
		if _, iss := l.v.Validate(true, 7.0); len(iss) != 1 || iss[0].Message != "Invalid input: expected string, received number" {
			t.Fatalf("%s wrong: %v", l.name, iss)
		}
	}
	// The two OK arms the goldens skip.
	if gotv, iss := ChunkID().Validate(true, "507f1f77bcf86cd799439011"); len(iss) != 0 || gotv != "507f1f77bcf86cd799439011" {
		t.Fatalf("chunk ok: %v %v", gotv, iss)
	}
	if gotv, iss := RouteSegment().Validate(true, "foo"); len(iss) != 0 || gotv != "foo" {
		t.Fatalf("route ok: %v %v", gotv, iss)
	}
}

func TestCovPrimitives(t *testing.T) {
	// BoolVal (0%): absent/null/ok/wrong.
	if _, iss := (BoolVal{}).Validate(false, nil); iss[0].Message != "Invalid input: expected boolean, received undefined" {
		t.Fatalf("bool absent: %v", iss)
	}
	if _, iss := (BoolVal{}).Validate(true, nil); iss[0].Message != "Invalid input: expected boolean, received null" {
		t.Fatalf("bool null: %v", iss)
	}
	if v, iss := (BoolVal{}).Validate(true, true); v != true || len(iss) != 0 {
		t.Fatalf("bool ok: %v %v", v, iss)
	}
	if _, iss := (BoolVal{}).Validate(true, 7.0); iss[0].Message != "Invalid input: expected boolean, received number" {
		t.Fatalf("bool wrong: %v", iss)
	}
	// StringVal: absent/null/wrong/min/max.
	if _, iss := (StringVal{}).Validate(false, nil); iss[0].Message != "Invalid input: expected string, received undefined" {
		t.Fatalf("string absent: %v", iss)
	}
	if _, iss := (StringVal{}).Validate(true, nil); iss[0].Message != "Invalid input: expected string, received null" {
		t.Fatalf("string null: %v", iss)
	}
	if _, iss := (StringVal{}).Validate(true, 7.0); iss[0].Message != "Invalid input: expected string, received number" {
		t.Fatalf("string wrong: %v", iss)
	}
	if _, iss := (StringVal{Min: 3}).Validate(true, "ab"); iss[0].Message != "Too small: expected string to have >=3 characters" {
		t.Fatalf("string min: %v", iss)
	}
	if _, iss := (StringVal{Max: 2}).Validate(true, "abc"); iss[0].Message != "Too big: expected string to have <=2 characters" {
		t.Fatalf("string max: %v", iss)
	}
	// NumberVal: absent/null/wrong/type-conversions/int/neg.
	if _, iss := (NumberVal{}).Validate(false, nil); iss[0].Message != "Invalid input: expected number, received undefined" {
		t.Fatalf("num absent: %v", iss)
	}
	if _, iss := (NumberVal{}).Validate(true, nil); iss[0].Message != "Invalid input: expected number, received null" {
		t.Fatalf("num null: %v", iss)
	}
	if _, iss := (NumberVal{}).Validate(true, "s"); iss[0].Message != "Invalid input: expected number, received string" {
		t.Fatalf("num wrong: %v", iss)
	}
	if v, iss := (NumberVal{}).Validate(true, int(5)); v != 5.0 || len(iss) != 0 {
		t.Fatalf("num int: %v %v", v, iss)
	}
	if v, iss := (NumberVal{}).Validate(true, uint8(3)); v != 3.0 || len(iss) != 0 {
		t.Fatalf("num uint: %v %v", v, iss)
	}
	if _, iss := (NumberVal{Integer: true}).Validate(true, 1.5); iss[0].Message != "Invalid input: expected int, received number" {
		t.Fatalf("num int-fract: %v", iss)
	}
	if _, iss := (NumberVal{NonNegative: true}).Validate(true, -1.0); iss[0].Message != "Too small: expected number to be >=0" {
		t.Fatalf("num neg: %v", iss)
	}
	if _, iss := (NumberIntNonnegative()).Validate(true, "s"); iss[0].Message != "Invalid input: expected number, received string" {
		t.Fatalf("nn wrong: %v", iss)
	}
	// StringArrayVal (thin): absent/null/wrong/default-item/custom-item + elem path.
	if _, iss := (StringArrayVal{}).Validate(false, nil); iss[0].Message != "Invalid input: expected array, received undefined" {
		t.Fatalf("arr absent: %v", iss)
	}
	if _, iss := (StringArrayVal{}).Validate(true, nil); iss[0].Message != "Invalid input: expected array, received null" {
		t.Fatalf("arr null: %v", iss)
	}
	if _, iss := (StringArrayVal{}).Validate(true, "s"); iss[0].Message != "Invalid input: expected array, received string" {
		t.Fatalf("arr wrong: %v", iss)
	}
	if _, iss := (StringArrayVal{}).Validate(true, []any{"a", 7.0}); len(iss) != 1 || iss[0].Message != "Invalid input: expected string, received number" || iss[0].Path[0].Value != "1" {
		t.Fatalf("arr elem: %v", iss)
	}
	if _, iss := (StringArrayVal{Item: ChunkID()}).Validate(true, []any{"507f1f77bcf86cd799439011"}); len(iss) != 0 {
		t.Fatalf("arr custom ok: %v", iss)
	}
	if _, iss := (StringArrayVal{Item: ChunkID()}).Validate(true, []any{"a-b"}); len(iss) != 1 || iss[0].Message != "invalid chunk id" {
		t.Fatalf("arr custom: %v", iss)
	}
}

func TestCovStrictObjectArms(t *testing.T) {
	so2 := NewStrictObject(Field{Name: "name", Schema: StringVal{}})
	// absent strict object -> "received undefined"; present null -> "received null".
	if _, iss := so2.Validate(false, nil); iss[0].Message != "Invalid input: expected object, received undefined" {
		t.Fatalf("oabsent: %v", iss)
	}
	if _, iss := so2.Validate(true, nil); iss[0].Message != "Invalid input: expected object, received null" {
		t.Fatalf("onull: %v", iss)
	}
	// non-map, non-OrderedObject struct -> "received object" (jsTypeName default).
	if _, iss := so2.Validate(true, struct{}{}); len(iss) != 1 {
		t.Fatalf("objtype: %v", iss)
	}
	// ordered (KeyOrder) path: a listed unknown key surfaces unrecognized_keys.
	if _, iss := so2.Validate(true, &OrderedObject{M: map[string]any{"name": "x", "zz": 1.0}, KeyOrder: []string{"name", "zz"}}); iss[0].Code != "unrecognized_keys" {
		t.Fatalf("ordered unk: %v", iss)
	}
	// map branch: known + unknown keys.
	if _, iss := so2.Validate(true, map[string]any{"name": "x", "zz": 1.0}); len(iss) != 1 || iss[0].Code != "unrecognized_keys" {
		t.Fatalf("map unk: %v", iss)
	}
	// optional field absent -> dropped (no issue, no key).
	so3 := NewStrictObject(Field{Name: "str", Schema: StringVal{}}, Field{Name: "num", Schema: NumberVal{}, Optional: true})
	if v, iss := so3.Validate(true, map[string]any{"str": "a"}); len(iss) != 0 {
		t.Fatalf("optdrop: %v %v", v, iss)
	}
	// schema-normalised-absent (datetime nullish) -> dropped from output.
	so4 := NewStrictObject(Field{Name: "dt", Schema: DTAdapterVal{DatetimeNullish(DatetimeOpts{})}})
	if _, iss := so4.Validate(true, map[string]any{}); len(iss) != 0 {
		t.Fatalf("absent drop: %v", iss)
	}
	if v, iss := so4.Validate(true, map[string]any{"dt": nil}); len(iss) != 0 {
		t.Fatalf("nullish drop: %v %v", v, iss)
	}
}

func TestCovUploadedFileOk(t *testing.T) {
	// a full, valid file object (optional "encoding" absent).
	if _, iss := UploadedFile().Validate(true, map[string]any{
		"fieldname": "f", "originalname": "a.pdf", "mimetype": "pdf",
		"size": 0.0, "destination": "/tmp", "filename": "a.pdf", "path": "/tmp/a.pdf",
	}); len(iss) != 0 {
		t.Fatalf("upload: %v", iss)
	}
}

func TestCovTimeArms(t *testing.T) {
	// the time.Time passthrough arm (z.date()).
	in := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	out, iss := (Datetime(DatetimeOpts{})).Validate(true, in)
	if len(iss) != 0 || !out.Time.Equal(in) {
		t.Fatalf("date arm: %v %v", out, iss)
	}
	// the Local bare-datetime parse (zone == "" -> FixedZone fallback).
	if locT := parseISODatetime("2024-01-01T12:00:00"); locT.Year() != 2024 || locT.Month() != 1 {
		t.Fatalf("local parse: %v", locT)
	}
	// isJSIdStart astral arm (r >= 1<<16 -> false).
	if isJSIdStart(rune(0x10000)) {
		t.Fatalf("astral must not be id start")
	}
	// jsTypeName nil / time.Time arms.
	if gotv := jsTypeName(nil); gotv != "null" {
		t.Fatalf("name nil: %q", gotv)
	}
	if gotv := jsTypeName(time.Now()); gotv != "date" {
		t.Fatalf("name date: %q", gotv)
	}
}

func TestCovFriendlyEarlyExits(t *testing.T) {
	// data nil, non-empty path -> undefined -> "is required".
	we := NewValidationError(&ZodError{Issues: []Issue{NewIssue("invalid_type", "m", StrSeg("a"))}}, nil)
	if got, want := we.Error(), `"a" is required`; got != want {
		t.Fatalf("nil data: got %q want %q", got, want)
	}
	// empty path: the loop skips, returns (current, true).
	if v, ok := getPathValue(map[string]any{"a": 1.0}, nil); !ok || v == nil {
		t.Fatalf("empty path: %v %v", v, ok)
	}
}
