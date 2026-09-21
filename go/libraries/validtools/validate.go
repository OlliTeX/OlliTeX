package validtools

import (
	"strconv"
	"strings"
)

// validate.go — validateSchema (validation-tools/validateSchema.js).
//
// Node:
//
//	function validateSchema(schema, data) {
//	  try { return schema.parse(data) }
//	  catch (err) {
//	    if (isZodErrorLike(err)) {
//	      const errorMessages = err.issues.map(issue => {
//	        const value = getPathValue(data, issue.path)
//	        if (issue.path.length === 0) return issue.message
//	        const fieldName = String(issue.path[issue.path.length - 1])
//	        if (isRequiredError(issue, value)) return `"${fieldName}" is required`
//	        return `"${fieldName}" - ` + issue.message
//	      })
//	      throw new Error(errorMessages.join('; '))
//	    }
//	    throw err
//	  }
//	}
//
// i.e. the friendly message is flat (no title-case, no "Validation error: "
// prefix): one entry per top-level issue, joined with "; ".

// ValidationError is the thrown error of ValidateSchema: Error() text is the
// Node-faithful friendly message; ZodError() retains the issue list for the
// B2 wire path (additive — Node's plain Error loses it, but Go callers
// need it to build `{"error": Wire(...)}`).
type ValidationError struct {
	msg string
	ze  *ZodError
}

// Error returns the flat user-facing message (the Node thrown text).
func (e *ValidationError) Error() string { return e.msg }

// ZodError returns the underlying failed parse (additive surface).
func (e *ValidationError) ZodError() *ZodError { return e.ze }

// NewValidationError builds the friendly error for a failed parse of `data`.
// data is the RAW input (any Go value: map, scalar, nil — the golden
// `root` case validates a scalar at the root, and `empty path?` passes
// null). Used only for the friendly message (getPathValue walk).
func NewValidationError(ze *ZodError, data any) *ValidationError {
	return &ValidationError{
		msg: friendlyMessage(ze, data),
		ze:  ze,
	}
}

// friendlyMessage mirrors the `issues.map(...).join('; ')` exactly.
func friendlyMessage(ze *ZodError, data any) string {
	parts := make([]string, 0, len(ze.Issues))
	for _, issue := range ze.Issues {
		if len(issue.Path) == 0 {
			parts = append(parts, issue.Message)
			continue
		}
		fieldName := pathString(lastPathSeg(issue.Path))
		value, defined := getPathValue(data, issue.Path)
		if isRequiredError(issue, defined) {
			parts = append(parts, `"`+fieldName+`" is required`)
			continue
		}
		_ = value
		parts = append(parts, `"`+fieldName+`" - `+issue.Message)
	}
	return strings.Join(parts, "; ")
}

// getLastPathSeg is exported only for the message builder.
func lastPathSeg(p []PathSeg) PathSeg { return p[len(p)-1] }

// pathString renders a path segment the way zod/JS does (String(key)):
// indices are decimal, keys verbatim.
func pathString(s PathSeg) string { return s.Value }

// getPathValue mirrors the JS `getPathValue(data, path)`: follow the key
// chain; null / missing object short-circuit to `undefined` (Go: ok=false).
// Both map keys (string segments) and array indices (numeric segments) are
// traversed, exactly like the JS `current[key]` (a number indexes a JS
// array; the value found IS defined even when it is JSON null — only a
// missing key or a null container stops the walk).
func getPathValue(data any, path []PathSeg) (any, bool) {
	current := data
	for i, key := range path {
		if current == nil {
			return nil, false
		}
		v, exists := indexValue(current, key)
		if !exists {
			return nil, false
		}
		if i == len(path)-1 {
			return v, true
		}
		current = v
	}
	return current, true
}

// indexValue is one JS `current[key]` step for a container that is a plain
// object or an array. A map value present as nil (JSON null) is DEFINED;
// an array index out of range or a non-index segment on an array is not.
func indexValue(container any, key PathSeg) (any, bool) {
	switch c := container.(type) {
	case map[string]any:
		v, ok := c[key.Value]
		return v, ok
	case []any:
		if !key.IsIndex {
			return nil, false
		}
		i, err := strconv.Atoi(key.Value)
		if err != nil || i < 0 || i >= len(c) {
			return nil, false
		}
		return c[i], true
	default:
		return nil, false
	}
}

// isRequiredError mirrors `value === undefined && (code is invalid_type |
// invalid_union)`.
func isRequiredError(issue Issue, defined bool) bool {
	if defined {
		return false
	}
	return issue.Code == "invalid_type" || issue.Code == "invalid_union"
}
