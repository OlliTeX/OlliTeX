package otc

import "fmt"

// The Go package exposes ToRaw() as idiomatic typed values ([]map[string]any,
// []map[string]int, map[string]any), but the raw form decoded from JSON (or an
// external producer) uses []any. These helpers tolerate both so that
// FromRawStringFileData(file.ToRaw()) round-trips exactly like Node's
// `StringFileData.fromRaw(file.toRaw())`.

// toAnySlice normalises a raw array to []any, accepting []any and common typed
// slices.
func toAnySlice(v any) []any {
	switch a := v.(type) {
	case []any:
		return a
	case []map[string]any:
		out := make([]any, 0, len(a))
		for _, e := range a {
			out = append(out, e)
		}
		return out
	case []map[string]int:
		out := make([]any, 0, len(a))
		for _, e := range a {
			out = append(out, e)
		}
		return out
	case []Range:
		out := make([]any, 0, len(a))
		for _, e := range a {
			out = append(out, e.ToRaw())
		}
		return out
	case []string:
		out := make([]any, 0, len(a))
		for _, e := range a {
			out = append(out, e)
		}
		return out
	}
	return []any{}
}

// asRawMap normalises a raw object to map[string]any, accepting map[string]any
// or map[string]int.
func asRawMap(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case map[string]int:
		out := map[string]any{}
		for k, val := range m {
			out[k] = val
		}
		return out
	}
	return map[string]any{}
}

// asRawObjects extracts a list of raw object maps from a raw array.
func asRawObjects(v any) []map[string]any {
	out := []map[string]any{}
	for _, e := range toAnySlice(v) {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// asRawRanges extracts a list of Ranges from a raw array (elements may be
// map[string]any or map[string]int) — legacy lenient wrapper (invalid
// elements are skipped); wire paths must use asRawRangesErr so malformed
// data becomes a proper error instead of a NewRange panic (audit B9).
func asRawRanges(v any) []Range {
	out, _ := asRawRangesErr(v)
	return out
}

// asRawRangesErr — the decode-path variant (audit B9): a raw range with
// pos < 0 or length < 0 is a protocol error, returned (Node: `new Range`
// throws the matching OError), not a panic.
func asRawRangesErr(v any) ([]Range, error) {
	out := []Range{}
	for _, e := range toAnySlice(v) {
		var pos, length int
		switch m := e.(type) {
		case map[string]any:
			pos, _ = numberToInt(m["pos"])
			length, _ = numberToInt(m["length"])
		case map[string]int:
			pos, length = m["pos"], m["length"]
		default:
			continue
		}
		r, ok := NewRangeSafe(pos, length)
		if !ok {
			return nil, gop(fmt.Sprintf("invalid range (pos=%d length=%d)", pos, length))
		}
		out = append(out, r)
	}
	return out, nil
}
