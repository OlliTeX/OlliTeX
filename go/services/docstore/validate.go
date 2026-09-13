package docstore

// validate.go — the zod-4 validation layer (libraries/validation-tools +
// @overleaf/ranges-tracker/schemas.js), with the exact issue texts locked by
// live probes of the running Node service. REQ_VALIDATION_MODE=enforce-log
// (production) ⇒ every schema failure throws: any issue rooted at `params`
// ⇒ 404, else 400; all issues in schema order joined by "; ".
//
// Validator success semantics: true == no issue appended (so optional fields
// report true when absent).

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hex24 = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// ---- issue text builders (locked strings) ------------------------------------

func typeIssue(expect, got, path string) string {
	return "Invalid input: expected " + expect + ", received " + got + ` at "` + path + `"`
}

func objectIDIssue(path string) string {
	return `Invalid Mongo ObjectId at "` + path + `"`
}

func stringboolIssue(path string) string {
	return `Invalid option: expected one of "true"|"1"|"yes"|"on"|"y"|"enabled"|"false"|"0"|"no"|"off"|"n"|"disabled" at "` + path + `"`
}

func intMin0Issue(path string) string {
	return `Too small: expected number to be >=0 at "` + path + `"`
}

func literalTrueIssue(path string) string {
	return `Invalid input: expected true at "` + path + `"`
}

func coerceDateIssue(path string) string {
	// z.coerce.date() with undefined or unparseable input — the observed
	// (quirky) text reports the expected type name as the received type.
	return `Invalid input: expected date, received Date at "` + path + `"`
}

func unrecognizedIssue(objPath string, keys []string) string {
	if len(keys) == 1 {
		return `Unrecognized key: "` + keys[0] + `" at "` + objPath + `"`
	}
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = `"` + k + `"`
	}
	return `Unrecognized keys: ` + strings.Join(quoted, ", ") + ` at "` + objPath + `"`
}

type issue struct {
	root string
	text string
}

func hasParamsIssue(iss []issue) bool {
	for _, e := range iss {
		if e.root == "params" {
			return true
		}
	}
	return false
}

// ---- JSON type names -----------------------------------------------------------

func jsonReceived(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// ---- field validators (success == no issue appended) ---------------------------

func vObjectID(o *objectInput, key, path, root string, iss *[]issue) (string, bool) {
	if !o.has(key) {
		*iss = append(*iss, issue{root, typeIssue("string", "undefined", path)})
		return "", false
	}
	v := o.fields[key]
	s, isStr := v.(string)
	if !isStr {
		*iss = append(*iss, issue{root, typeIssue("string", jsonReceived(v), path)})
		return "", false
	}
	if !hex24.MatchString(s) {
		*iss = append(*iss, issue{root, objectIDIssue(path)})
		return "", false
	}
	return s, true
}

func vRequiredString(o *objectInput, key, path, root string, iss *[]issue) (string, bool) {
	if !o.has(key) {
		*iss = append(*iss, issue{root, typeIssue("string", "undefined", path)})
		return "", false
	}
	v := o.fields[key]
	s, isStr := v.(string)
	if !isStr {
		*iss = append(*iss, issue{root, typeIssue("string", jsonReceived(v), path)})
		return "", false
	}
	return s, true
}

func vOptionalString(o *objectInput, key, path, root string, iss *[]issue) (string, bool) {
	if !o.has(key) {
		return "", true
	}
	v := o.fields[key]
	s, isStr := v.(string)
	if !isStr {
		*iss = append(*iss, issue{root, typeIssue("string", jsonReceived(v), path)})
		return "", false
	}
	return s, true
}

func vOptionalBool(o *objectInput, key, path, root string, iss *[]issue) (bool, bool) {
	if !o.has(key) {
		return false, true
	}
	v := o.fields[key]
	b, isBool := v.(bool)
	if !isBool {
		*iss = append(*iss, issue{root, typeIssue("boolean", jsonReceived(v), path)})
		return false, false
	}
	return b, true
}

func vNumber(o *objectInput, key, path, root string, iss *[]issue) (float64, bool) {
	if !o.has(key) {
		*iss = append(*iss, issue{root, typeIssue("number", "undefined", path)})
		return 0, false
	}
	v := o.fields[key]
	n, isNum := v.(float64)
	if !isNum {
		*iss = append(*iss, issue{root, typeIssue("number", jsonReceived(v), path)})
		return 0, false
	}
	return n, true
}

func vIntMin0(o *objectInput, key, path, root string, iss *[]issue) (int64, bool) {
	if !o.has(key) {
		*iss = append(*iss, issue{root, typeIssue("number", "undefined", path)})
		return 0, false
	}
	v := o.fields[key]
	n, isNum := v.(float64)
	if !isNum {
		*iss = append(*iss, issue{root, typeIssue("number", jsonReceived(v), path)})
		return 0, false
	}
	if n != trunc64(n) {
		*iss = append(*iss, issue{root, typeIssue("int", "number", path)})
		return 0, false
	}
	if n < 0 {
		*iss = append(*iss, issue{root, intMin0Issue(path)})
		return 0, false
	}
	return int64(n), true
}

func vStringboolValue(s string) (bool, bool) {
	switch s {
	case "true", "1", "yes", "on", "y", "enabled":
		return true, true
	case "false", "0", "no", "off", "n", "disabled":
		return false, true
	}
	return false, false
}

func vStringbool(o *objectInput, key, path, root string, iss *[]issue) (bool, bool) {
	if !o.has(key) {
		return false, true // z.stringbool().default(false)
	}
	v := o.fields[key]
	if b, isBool := v.(bool); isBool {
		return b, true
	}
	if s, isStr := v.(string); isStr {
		if bv, ok := vStringboolValue(s); ok {
			return bv, true
		}
	}
	*iss = append(*iss, issue{root, stringboolIssue(path)})
	return false, false
}

func vLiteralTrue(o *objectInput, key, path, root string, iss *[]issue) (bool, bool) {
	if !o.has(key) {
		*iss = append(*iss, issue{root, literalTrueIssue(path)})
		return false, false
	}
	if b, isBool := o.fields[key].(bool); isBool && b {
		return true, true
	}
	*iss = append(*iss, issue{root, literalTrueIssue(path)})
	return false, false
}

func vCoerceDate(o *objectInput, key, path, root string, iss *[]issue) (time.Time, bool) {
	if !o.has(key) {
		*iss = append(*iss, issue{root, coerceDateIssue(path)})
		return time.Time{}, false
	}
	v := o.fields[key]
	if n, isNum := v.(float64); isNum && n == trunc64(n) && n >= 0 {
		return time.UnixMilli(int64(n)).UTC(), true
	}
	if s, isStr := v.(string); isStr {
		if t, ok := parseJSDate(s); ok {
			return t, true
		}
	}
	*iss = append(*iss, issue{root, coerceDateIssue(path)})
	return time.Time{}, false
}

func vArrayStrings(o *objectInput, key, path, root string, iss *[]issue) ([]string, bool) {
	if !o.has(key) {
		*iss = append(*iss, issue{root, typeIssue("array", "undefined", path)})
		return nil, false
	}
	v := o.fields[key]
	arr, isArr := v.([]any)
	if !isArr {
		*iss = append(*iss, issue{root, typeIssue("array", jsonReceived(v), path)})
		return nil, false
	}
	out := make([]string, 0, len(arr))
	okAll := true
	for i, e := range arr {
		s, isStr := e.(string)
		if !isStr {
			*iss = append(*iss, issue{root, typeIssue("string", jsonReceived(e), path+"["+strconv.Itoa(i)+"]")})
			okAll = false
			continue
		}
		out = append(out, s)
	}
	return out, okAll
}

// ---- strictObject machinery ----------------------------------------------------

// strictObject validates a decoded object against its declared keys:
// declared keys in declaration order, then ONE Unrecognized issue for the
// unknown input keys (input order; singular/plural as observed).
func strictObject(root, objPath string, o *objectInput, declared []string, validate func(key string), iss *[]issue) {
	declaredSet := make(map[string]bool, len(declared))
	for _, d := range declared {
		declaredSet[d] = true
	}
	for _, d := range declared {
		validate(d)
	}
	var unknown []string
	for _, k := range o.keys {
		if !declaredSet[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		*iss = append(*iss, issue{root, unrecognizedIssue(objPath, unknown)})
	}
}

func objectFor(root string, v any, objPath string, iss *[]issue) (*objectInput, bool) {
	m, isMap := v.(map[string]any)
	if !isMap || m == nil {
		got := "undefined"
		if v != nil {
			got = jsonReceived(v)
		}
		*iss = append(*iss, issue{root, typeIssue("object", got, objPath)})
		return nil, false
	}
	return &objectInput{keys: sortedKeys(m), fields: m}, true
}

// ---- ranges schema (locked from @overleaf/ranges-tracker/schemas.js) -----------

// validateOpBranch: insertOp {i,p,u?,fixedRemoveChange?,orderedRejections?}
// or deleteOp {d,p,u?,fixedRemoveChange?,orderedRejections?}.
func validateOpBranch(root, objPath string, o *objectInput, firstKey string, iss *[]issue) bool {
	okAll := true
	strictObject(root, objPath, o, []string{firstKey, "p", "u", "fixedRemoveChange", "orderedRejections"}, func(key string) {
		p := objPath + "." + key
		switch key {
		case "i", "d":
			if _, ok := vRequiredString(o, key, p, root, iss); !ok {
				okAll = false
			}
		case "p":
			if _, ok := vIntMin0(o, key, p, root, iss); !ok {
				okAll = false
			}
		case "u", "fixedRemoveChange", "orderedRejections":
			if _, ok := vOptionalBool(o, key, p, root, iss); !ok {
				okAll = false
			}
		}
	}, iss)
	return okAll
}

// validateOpUnion: insertOp.or(deleteOp); failure renders as
// `branch1-issues or branch2-issues` (each branch "; "-joined) — locked by
// probe. Appends exactly one union issue on failure.
func validateOpUnion(root, objPath string, v any, iss *[]issue) bool {
	o, isObj := objectFor(root, v, objPath, iss)
	if !isObj {
		return false
	}
	insI := []issue{}
	if validateOpBranch(root, objPath, o, "i", &insI) {
		return true
	}
	delI := []issue{}
	if validateOpBranch(root, objPath, o, "d", &delI) {
		return true
	}
	t1 := make([]string, len(insI))
	for i, e := range insI {
		t1[i] = e.text
	}
	t2 := make([]string, len(delI))
	for i, e := range delI {
		t2[i] = e.text
	}
	*iss = append(*iss, issue{root, strings.Join(t1, "; ") + " or " + strings.Join(t2, "; ")})
	return false
}

func validateCommentOp(root, objPath string, o *objectInput, iss *[]issue) bool {
	okAll := true
	strictObject(root, objPath, o, []string{"c", "p", "t", "u", "resolved"}, func(key string) {
		p := objPath + "." + key
		switch key {
		case "c":
			if _, ok := vRequiredString(o, key, p, root, iss); !ok {
				okAll = false
			}
		case "p":
			if _, ok := vIntMin0(o, key, p, root, iss); !ok {
				okAll = false
			}
		case "t":
			if _, ok := vObjectID(o, key, p, root, iss); !ok {
				okAll = false
			}
		case "u", "resolved":
			if _, ok := vOptionalBool(o, key, p, root, iss); !ok {
				okAll = false
			}
		}
	}, iss)
	return okAll
}

func validateComment(root, path string, v any, iss *[]issue) bool {
	o, isObj := objectFor(root, v, path, iss)
	if !isObj {
		return false
	}
	failed := false
	strictObject(root, path, o, []string{"id", "op", "metadata"}, func(key string) {
		p := path + "." + key
		switch key {
		case "id":
			if _, ok := vOptionalString(o, key, p, root, iss); !ok {
				failed = true
			}
		case "op":
			if !o.has("op") {
				*iss = append(*iss, issue{root, typeIssue("object", "undefined", p)})
				failed = true
				return
			}
			op, isOp := objectFor(root, o.fields["op"], p, iss)
			if !isOp {
				failed = true
				return
			}
			if !validateCommentOp(root, p, op, iss) {
				failed = true
			}
		case "metadata":
			if !o.has("metadata") {
				return
			}
			md, isMd := objectFor(root, o.fields["metadata"], p, iss)
			if !isMd {
				failed = true
				return
			}
			strictObject(root, p, md, []string{"user_id", "ts"}, func(k2 string) {
				if _, ok := vOptionalString(md, k2, p+"."+k2, root, iss); !ok {
					failed = true
				}
			}, iss)
		}
	}, iss)
	return !failed
}

func validateTrackedChange(root, path string, v any, iss *[]issue) bool {
	o, isObj := objectFor(root, v, path, iss)
	if !isObj {
		return false
	}
	failed := false
	strictObject(root, path, o, []string{"id", "op", "metadata"}, func(key string) {
		p := path + "." + key
		switch key {
		case "id":
			if _, ok := vOptionalString(o, key, p, root, iss); !ok {
				failed = true
			}
		case "op":
			if !o.has("op") {
				*iss = append(*iss, issue{root, typeIssue("object", "undefined", p)})
				failed = true
				return
			}
			if !validateOpUnion(root, p, o.fields["op"], iss) {
				failed = true
			}
		case "metadata":
			if !o.has("metadata") {
				*iss = append(*iss, issue{root, typeIssue("object", "undefined", p)})
				failed = true
				return
			}
			md, isMd := objectFor(root, o.fields["metadata"], p, iss)
			if !isMd {
				failed = true
				return
			}
			strictObject(root, p, md, []string{"user_id", "ts"}, func(k2 string) {
				if _, ok := vRequiredString(md, k2, p+"."+k2, root, iss); !ok {
					failed = true
				}
			}, iss)
		}
	}, iss)
	return !failed
}

// validateRanges: {comments? comment[], changes? trackedChange[]} strict.
func validateRanges(root string, v any, path string, iss *[]issue) bool {
	o, isObj := objectFor(root, v, path, iss)
	if !isObj {
		return false
	}
	failed := false
	strictObject(root, path, o, []string{"comments", "changes"}, func(key string) {
		if !o.has(key) {
			return
		}
		p := path + "." + key
		arr, isArr := o.fields[key].([]any)
		if !isArr {
			*iss = append(*iss, issue{root, typeIssue("array", jsonReceived(o.fields[key]), p)})
			failed = true
			return
		}
		for i, e := range arr {
			ok := false
			if key == "comments" {
				ok = validateComment(root, p+"["+strconv.Itoa(i)+"]", e, iss)
			} else {
				ok = validateTrackedChange(root, p+"["+strconv.Itoa(i)+"]", e, iss)
			}
			if !ok {
				failed = true
			}
		}
	}, iss)
	return !failed
}

// ---- route parameter/query validation --------------------------------------------

type routeParams struct {
	projectID string
	docID     string
}

// validateRouteParams: strict {project_id [, doc_id]} (route-derived, so only
// the ObjectId checks can fail).
func validateRouteParams(p routeParams, wantDoc bool, iss *[]issue) {
	obj := &objectInput{fields: map[string]any{"project_id": p.projectID}}
	if wantDoc {
		obj.fields["doc_id"] = p.docID
		obj.keys = []string{"project_id", "doc_id"}
	} else {
		obj.keys = []string{"project_id"}
	}
	declared := []string{"project_id"}
	if wantDoc {
		declared = append(declared, "doc_id")
	}
	strictObject("params", "params", obj, declared, func(key string) {
		_, _ = vObjectID(obj, key, "params."+key, "params", iss)
	}, iss)
}

// parseRawQuery: query string → objectInput in URL order; repeated keys keep
// the last value (Express/qs); '+' → space.
func parseRawQuery(raw string) *objectInput {
	q := &objectInput{fields: map[string]any{}}
	if raw == "" {
		return q
	}
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		k, v := part, ""
		if i := strings.IndexByte(part, '='); i >= 0 {
			k, v = part[:i], part[i+1:]
		}
		k = strings.ReplaceAll(k, "+", " ")
		v = strings.ReplaceAll(v, "+", " ")
		if uk, err := url.QueryUnescape(k); err == nil {
			k = uk
		}
		if uv, err := url.QueryUnescape(v); err == nil {
			v = uv
		}
		if _, seen := q.fields[k]; !seen {
			q.keys = append(q.keys, k)
		}
		q.fields[k] = v
	}
	return q
}

// validateQuery: strict query object with the given stringbool keys.
func validateQuery(root string, q *objectInput, declared []string, set func(key string, value bool), iss *[]issue) {
	strictObject(root, root, q, declared, func(key string) {
		v, ok := vStringbool(q, key, root+"."+key, root, iss)
		if ok {
			set(key, v)
		}
	}, iss)
}

func trunc64(f float64) float64 {
	i := int64(f)
	return float64(i)
}

// parseJSDate: the string forms Node's new Date(string) accepts for
// web-generated timestamps. Unparseable input is the zod coerce failure.
func parseJSDate(s string) (time.Time, bool) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05",
		"2006-01-02",
		"2006-01-02 15:04:05",
		"01/02/2006 15:04:05",
		"01/02/2006",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), true
		}
	}
	if regexp.MustCompile(`^\d{13}$`).MatchString(s) {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.UnixMilli(n).UTC(), true
		}
	}
	return time.Time{}, false
}
