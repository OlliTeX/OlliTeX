package validtools

import (
	"math"
	"sort"
	"strconv"
)

// Val is a value schema: Validate(present, v) → (normalised value, issues),
// where present=false means "key absent" and v==nil means "present, null"
// (the two forms Node's `optional().transform(v => v ?? undefined)`
// normalises into one "absent" value).
type Val interface {
	Validate(present bool, v any) (any, []Issue)
}

// Field is one declared object (strict-object) field.
type Field struct {
	Name     string
	Optional bool // z...().optional(): absent (undefined) allowed; null still fails
	Nullish  bool // zz.nullabsorption: absent AND null allowed, both → absent
	Schema   Val
}

// StrictObject ports zodHelpers strictObject() + z.strictObject: per-field
// issues in declaration order, then the unrecognized_keys issue (probe11/12:
// that issue comes AFTER the per-field issues). A missing required field
// runs the schema against "undefined" (so z.strictObject + a union schema
// emit the union "received undefined" issues — pinned: the `dt absent`
// goldens). Keys are listed sorted for determinism (Node preserves
// insertion order from JSON.parse; Go maps are unordered — documented
// deviation, sorted-alphabetical is the wire order).
type StrictObject struct {
	Fields []Field
}

// NewStrictObject builds the framework from a declaration-order field list.
func NewStrictObject(fields ...Field) *StrictObject {
	return &StrictObject{Fields: fields}
}

// Object ports zod's NON-strict `z.object(...)`: declared fields are
// validated exactly as in StrictObject, but unrecognized keys are silently
// DROPPED from the output and emit NO issue (oracle: `z.object({a: z.string()})`
// .parse({a:'x', zzz:1}) → {a:'x'}), which is the common outer-envelope form
// in the services' route schemas. StrictObject is reserved for zz.uploadedFile
// and the `params`/route-segment shapes where the Node code uses
// `z.strictObject` (unknown key → 404-class issue).
type Object struct {
	StrictObject
}

// NewObject builds a non-strict object from a declaration-order field list.
func NewObject(fields ...Field) *Object {
	return &Object{StrictObject{Fields: fields}}
}

// Validate implements Val: strict-object validation minus the
// unrecognized-keys issue (dropped, as in z.object).
func (o *Object) Validate(present bool, v any) (any, []Issue) {
	val, iss := o.StrictObject.Validate(present, v)
	kept := make([]Issue, 0, len(iss))
	for _, is := range iss {
		if is.Code != "unrecognized_keys" {
			kept = append(kept, is)
		}
	}
	return val, kept
}

// missingField — removed (was dead; strict-object absence is handled inline
// via the present=false branch of each field schema).

// OrderedObject is an incoming decoded object that preserves key insertion
// order (the Go JSON layer can provide this via a small custom decoder —
// see services; plain encoding/json decodes to an unordered map and the
// framework falls back to sorted order in that case).
type OrderedObject struct {
	M        map[string]any
	KeyOrder []string
}

// Validate implements Val.
func (o *StrictObject) Validate(present bool, v any) (any, []Issue) {
	if !present || isNull(v) {
		what := "undefined"
		if present {
			what = "null"
		}
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected object, received "+what)}
	}
	var m map[string]any
	var order []string
	if ov, ok := v.(*OrderedObject); ok {
		m, order = ov.M, ov.KeyOrder
	} else if am, ok := v.(map[string]any); ok {
		m = am
	} else {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected object, received "+jsTypeName(v))}
	}
	issues := []Issue{}
	out := map[string]any{}
	for _, f := range o.Fields {
		fv, has := m[f.Name]
		if !has {
			if f.Optional || f.Nullish {
				continue
			}
			if val, iss := f.Schema.Validate(false, fv); len(iss) > 0 {
				for n := range iss {
					issues = append(issues, iss[n].At(StrSeg(f.Name)))
				}
			} else if _, isAbsent := val.(absentVal); !isAbsent {
				out[f.Name] = val
			}
			continue
		}
		if f.Nullish && isNull(fv) {
			continue // normalised to undefined → no key
		}
		val, iss := f.Schema.Validate(true, fv)
		if len(iss) > 0 {
			for n := range iss {
				issues = append(issues, iss[n].At(StrSeg(f.Name)))
			}
			continue
		}
		if _, isAbsent := val.(absentVal); isAbsent {
			continue // normalised to undefined → no key (Node drops it)
		}
		out[f.Name] = val
	}
	// unrecognized_keys, AFTER the per-field issues (probe11/12).
	unknown := []string{}
	declared := map[string]bool{}
	for _, f := range o.Fields {
		declared[f.Name] = true
	}
	if order != nil {
		for _, k := range order {
			if !declared[k] {
				unknown = append(unknown, k)
			}
		}
	} else {
		for k := range m {
			if !declared[k] {
				unknown = append(unknown, k)
			}
		}
		sort.Strings(unknown)
	}
	if len(unknown) > 0 {
		issues = append(issues, NewUnrecognizedKeysIssue(unknown))
	}
	return out, issues
}

// --- primitive leaf validators (z.string(), z.number() etc.) -------------

type StringVal struct {
	Min int // z.min(N)
	Max int // z.max(N)
}

func (s StringVal) Validate(present bool, v any) (any, []Issue) {
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received null")}
	}
	str, ok := v.(string)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received "+jsTypeName(v))}
	}
	is := []Issue{}
	// zod compares JS string lengths: UTF-16 CODE UNITS, not Go bytes
	// (an emoji is 4 bytes but 2 units — min/max results can differ).
	if s.Min > 0 && jsStringLen(str) < s.Min {
		// zod 4.1.11 too_small/too_big: no "but received N" suffix (probe16).
		is = append(is, NewIssue("too_small", "Too small: expected string to have >="+strconv.Itoa(s.Min)+" characters"))
	}
	if s.Max > 0 && jsStringLen(str) > s.Max {
		is = append(is, NewIssue("too_big", "Too big: expected string to have <="+strconv.Itoa(s.Max)+" characters"))
	}
	if len(is) > 0 {
		return nil, is
	}
	return str, nil
}

type NumberVal struct {
	Integer     bool
	NonNegative bool
}

func (n NumberVal) Validate(present bool, v any) (any, []Issue) {
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected number, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected number, received null")}
	}
	num, ok := asNumber(v)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected number, received "+jsTypeName(v))}
	}
	if n.Integer {
		if !isFloat64Integer(num) {
			return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected int, received number")}
		}
	}
	if n.NonNegative && num < 0 {
		return nil, []Issue{NewIssue("too_small", "Too small: expected number to be >=0")}
	}
	return num, nil
}

// isFloat64Integer reports whether the float64 has no fractional part.
func isFloat64Integer(x float64) bool { return x == math.Trunc(x) }

// jsStringLen mirrors JS `string.length`: the number of UTF-16 code units
// (1 per BMP code point, 2 per astral code point such as emoji), NOT the
// Go byte length and NOT the rune count. Used by the zod min/max string
// checks so their thresholds behave exactly as in Node.
func jsStringLen(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++ // surrogates: 2 UTF-16 units
		}
	}
	return n
}

func asNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int8:
		return float64(t), true
	case int16:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint8:
		return float64(t), true
	case uint16:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	}
	return 0, false
}

type BoolVal struct{}

func (BoolVal) Validate(present bool, v any) (any, []Issue) {
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected boolean, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected boolean, received null")}
	}
	if b, ok := v.(bool); ok {
		return b, nil
	}
	return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected boolean, received "+jsTypeName(v))}
}

// StringArrayVal ports z.array(item, ...) for zz-style arrays: element
// schemas are composed (default: z.string()); element issues carry the
// numeric path segment (wire: `at "0"`; see the `validateSchema deep path`
// golden). Multiple element issues are all kept (zod 4: no early-exit).
type StringArrayVal struct {
	Item Val // nil → StringVal{}
}

func (a StringArrayVal) Validate(present bool, v any) (any, []Issue) {
	item := a.Item
	if item == nil {
		item = StringVal{}
	}
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected array, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected array, received null")}
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected array, received "+jsTypeName(v))}
	}
	out := make([]any, len(arr))
	issues := []Issue{}
	for i, e := range arr {
		val, iss := item.Validate(true, e)
		out[i] = val
		for n := range iss {
			issues = append(issues, iss[n].At(IndexSeg(i)))
		}
	}
	return out, issues
}

// EnumVal ports z.enum(values, {message}). Wire (zod 4.1.11, probe17):
// a SINGLE invalid_value issue for ALL failure modes (absent, null, wrong
// type, non-member) — no per-type issues emitted. Custom message OR
// `Invalid option: expected one of "a"|"b"`.
type EnumVal struct {
	Values  []string
	Message string // "" → default wording
}

func (e EnumVal) Validate(present bool, v any) (any, []Issue) {
	member, ok := v.(string)
	if ok {
		for _, val := range e.Values {
			if member == val {
				return member, nil
			}
		}
	}
	if e.Message != "" {
		return nil, []Issue{NewIssue("invalid_value", e.Message)}
	}
	// Node (zod 4.1.11 oracle): a ONE-member enum renders
	// `Invalid input: expected "<value>"`; two-plus members use the
	// `Invalid option: expected one of ...` form; an empty enum keeps that
	// form with a trailing space (oracle-pinned).
	if len(e.Values) == 1 {
		return nil, []Issue{NewIssue("invalid_value", "Invalid input: expected "+strconv.Quote(e.Values[0]))}
	}
	joined := ""
	for i, val := range e.Values {
		if i > 0 {
			joined += "|"
		}
		joined += `"` + val + `"`
	}
	return nil, []Issue{NewIssue("invalid_value", "Invalid option: expected one of "+joined)}
}
