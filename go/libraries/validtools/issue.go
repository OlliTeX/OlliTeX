package validtools

import (
	"fmt"
	"strings"
)

// PathSeg is one Zod issue path segment: a property name or, for arrays, a
// decimal index. Wire rendering differs by kind (`at index 0` vs `at "0"`)
// so the segment kind must be known, not sniffed.
type PathSeg struct {
	Value   string
	IsIndex bool
}

// StrSeg returns a path segment for property `s`.
func StrSeg(s string) PathSeg { return PathSeg{Value: s} }

// IndexSeg returns a path segment for array index `i`.
func IndexSeg(i int) PathSeg { return PathSeg{Value: fmt.Sprintf("%d", i), IsIndex: true} }

// String renders the segment value as JS would (decimal for indices).
func (s PathSeg) String() string { return s.Value }

// Issue is one Zod issue.
//
//   - Code: invalid_type, invalid_format, custom, invalid_value, invalid_union,
//     unrecognized_keys, too_small, too_big (the wire renders Message, not Code).
//   - InvalidUnion: the per-union-arm issue groups. Each group holds the
//     issues that arm emitted (a group of one "received X" issue for type
//     failures); the wire renderer dedupes the rendered member texts.
//   - Keys: unrecognized_keys' unknown keys (insertion order). Path holds the
//     object's own path (empty for a root-level object).
type Issue struct {
	Path         []PathSeg
	Code         string
	Message      string
	InvalidUnion [][]Issue
	Keys         []string
}

// ZodError is a failed parse: the issues, in schema evaluation order.
type ZodError struct {
	Issues []Issue
}

var _ error = (*ZodError)(nil)

// Error renders `fromError(err).toString()`.
func (e *ZodError) Error() string { return Wire(e.Issues) }

// NewZodError wraps the issues of a failed validation.
func NewZodError(issues []Issue) *ZodError { return &ZodError{Issues: issues} }

// NewIssue builds an issue with a path of kinded segments.
func NewIssue(code, message string, path ...PathSeg) Issue {
	return Issue{Path: path, Code: code, Message: message}
}

// At returns iss with each segment prepended to its path (caller composition).
func (i Issue) At(prefix ...PathSeg) Issue {
	p := make([]PathSeg, 0, len(prefix)+len(i.Path))
	p = append(p, prefix...)
	p = append(p, i.Path...)
	i.Path = p
	return i
}

// NewUnionIssue builds an invalid_union issue: groups are the per-arm issues,
// path is the union's own path (empty for a root-level union).
func NewUnionIssue(groups [][]Issue, path ...PathSeg) Issue {
	iss := NewIssue("invalid_union", "Invalid input", path...)
	iss.InvalidUnion = groups
	return iss
}

// NewUnrecognizedKeysIssue builds the strict-object extra-keys issue.
// Mirrors zod: singular `Unrecognized key: "x"` / plural `Unrecognized keys:
// "x", "y"` (keys JSON-quoted), Path = the object's own path.
func NewUnrecognizedKeysIssue(keys []string, path ...PathSeg) Issue {
	msg := "Unrecognized keys: "
	if len(keys) == 1 {
		msg = `Unrecognized key: "` + keys[0] + `"`
	} else {
		quoted := make([]string, len(keys))
		for i, k := range keys {
			quoted[i] = `"` + k + `"`
		}
		msg = "Unrecognized keys: " + strings.Join(quoted, ", ")
	}
	iss := NewIssue("unrecognized_keys", msg, path...)
	iss.Keys = append([]string(nil), keys...)
	return iss
}
