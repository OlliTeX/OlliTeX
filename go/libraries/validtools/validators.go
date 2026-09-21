package validtools

import (
	"regexp"
	"strings"
)

// validators.go — the zz.* leaf constructors from validation-tools/
// zodHelpers.js. Every message byte-pinned against the 85-entry goldens.json
// oracle capture (Node: fromError(zodError).toString() on installed
// zod 4.1.11). Shared primitives (StringVal/NumberVal/EnumVal/...Val) live
// in object.go; this file is the public zz API.
//
// zod 4 chain rules (verified probes 9-12):
//   - .regex().min().max() string chains: ALL failing refinements, decl order
//   - z.number().int().nonnegative(): int-check first, short-circuits
//   - z.enum(..., {message}): single invalid_value, custom or default wording
//   - z.strictObject(...): per-field issues in decl order, then ONE
//     unrecognized_keys issue for the surplus keys

var (
	objectIDRx       = regexp.MustCompile(`^[0-9a-f]{24}$`)
	buildIDRx        = regexp.MustCompile(`^[0-9a-f]+-[0-9a-f]+$`)
	editorBuildIDRx  = regexp.MustCompile(`^[a-f0-9-]{36}-[0-9a-f]+-[0-9a-f]+$`)
	clsicodesRx      = regexp.MustCompile(`^[a-z0-9-]+$`)
	submissionIDRx   = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	kebabCaseRx      = regexp.MustCompile(`^[a-z0-9-]+$`)
	eventNameRx      = regexp.MustCompile(`^[a-zA-Z0-9-_]+$`)
	projectHistoryRx = regexp.MustCompile(`^([0-9a-f]{24}|[1-9][0-9]{0,9})$`)
	routeSegSepRx    = regexp.MustCompile(`[/\\?#]`)
)

// ObjectID — zz.objectId(): z.string().refine(ObjectId.isValid, {message:
// 'invalid Mongo ObjectId'}). Wire: `Invalid Mongo ObjectId`.
func ObjectID() Val {
	return stringRefine{refine: func(s string) []Issue {
		if !objectIDRx.MatchString(s) {
			return []Issue{NewIssue("custom", "invalid Mongo ObjectId")}
		}
		return nil
	}}
}

// Hex — zz.hex(): z.string().regex(/^[0-9a-f]*$/) — zod default
// invalid_format message "Invalid string: must match pattern <rx>".
func Hex() Val {
	return stringRefine{refine: func(s string) []Issue {
		if !hexRx.MatchString(s) {
			return []Issue{NewIssue("invalid_format", "Invalid string: must match pattern /"+hexRx.String()+"/")}
		}
		return nil
	}}
}

var hexRx = regexp.MustCompile(`^[0-9a-f]*$`)

// BuildID — zz.buildId(): regex buildIDRx, custom 'invalid buildId'.
func BuildID() Val {
	return stringRefine{refine: func(s string) []Issue {
		if !buildIDRx.MatchString(s) {
			return []Issue{NewIssue("invalid_format", "invalid buildId")}
		}
		return nil
	}}
}

// EditorBuildID — zz.editorBuildId(): custom 'invalid editorId-buildId'.
func EditorBuildID() Val {
	return stringRefine{refine: func(s string) []Issue {
		if !editorBuildIDRx.MatchString(s) {
			return []Issue{NewIssue("invalid_format", "invalid editorId-buildId")}
		}
		return nil
	}}
}

// CLSIServerID — zz.clsiServerId(): custom 'invalid clsiServerId'.
func CLSIServerID() Val {
	return stringRefine{refine: func(s string) []Issue {
		if !clsicodesRx.MatchString(s) {
			return []Issue{NewIssue("invalid_format", "invalid clsiServerId")}
		}
		return nil
	}}
}

// CompileBackendClass — custom 'invalid compileBackendClass'.
func CompileBackendClass() Val {
	return stringRefine{refine: func(s string) []Issue {
		if !clsicodesRx.MatchString(s) {
			return []Issue{NewIssue("invalid_format", "invalid compileBackendClass")}
		}
		return nil
	}}
}

// SubmissionID — zz.submissionId(): z.string().regex(/^[a-zA-Z0-9_-]+$/)
// default message "Invalid string: must match pattern <rx>".
func SubmissionID() Val {
	return stringRefine{refine: func(s string) []Issue {
		if !submissionIDRx.MatchString(s) {
			return []Issue{NewIssue("invalid_format", "Invalid string: must match pattern /"+submissionIDRx.String()+"/")}
		}
		return nil
	}}
}

// CompileGroup — zz.compileGroup(): z.enum(['alpha','gvisor','standard',
// 'priority'], {message: 'Invalid compileGroup'}).
func CompileGroup() Val {
	return EnumVal{
		Values:  []string{"alpha", "gvisor", "standard", "priority"},
		Message: "invalid compileGroup",
	}
}

// StringRefine helper: z.string() + a refine callback returning the refine
// issues (all of them; zod has no per-refine early-exit — each refine
// appends its own issue). Declared in object.go next to the primitives.
type stringRefine struct{ refine func(s string) []Issue }

func (l stringRefine) Validate(present bool, v any) (any, []Issue) {
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received null")}
	}
	s, ok := v.(string)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received "+jsTypeName(v))}
	}
	if is := l.refine(s); len(is) > 0 {
		return nil, is
	}
	return s, nil
}

// Filepath — zz.filepath():
//
//	z.string().nonempty({message: 'path is empty'})
//	  .refine(s => !s.startsWith('/'), {message: 'path is absolute'})
//	  .refine(s => !s.split('/').includes('..'), {message: 'path traversal detected'})
//
// chain: ALL failing refinements, declaration order (probes 1-3).
func Filepath() Val {
	return filepathVal{}
}

type filepathVal struct{}

func (filepathVal) Validate(present bool, v any) (any, []Issue) {
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received null")}
	}
	s, ok := v.(string)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received "+jsTypeName(v))}
	}
	is := []Issue{}
	if s == "" {
		is = append(is, NewIssue("custom", "path is empty"))
	} else {
		if strings.HasPrefix(s, "/") {
			is = append(is, NewIssue("custom", "path is absolute"))
		}
		if hasDotDotSegment(s) {
			is = append(is, NewIssue("custom", "path traversal detected"))
		}
	}
	if len(is) > 0 {
		return nil, is
	}
	return s, nil
}

func hasDotDotSegment(s string) bool {
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// RouteSegment — single opaque URL segment (see Node doc comment). Chain:
// nonempty, no [/ \ ? #], not '.'/'..'.
func RouteSegment() Val {
	return routeSegmentVal{}
}

type routeSegmentVal struct{}

func (routeSegmentVal) Validate(present bool, v any) (any, []Issue) {
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received null")}
	}
	s, ok := v.(string)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received "+jsTypeName(v))}
	}
	is := []Issue{}
	if s == "" {
		is = append(is, NewIssue("custom", "route segment is empty"))
	} else {
		if routeSegSepRx.MatchString(s) {
			is = append(is, NewIssue("custom", "route segment contains a path, query or fragment separator"))
		}
		if s == "." || s == ".." {
			is = append(is, NewIssue("custom", "route segment is a relative path component"))
		}
	}
	if len(is) > 0 {
		return nil, is
	}
	return s, nil
}

// SafePath — zz.safePath(): nonempty, not endsWith('/'), no '..' segment,
// no '.'/ws segment, no bad char, top-level not an unsafe property name.
// Chain emits all failing refinements in declaration order (probe 1 /
// goldens: each failure has its own single-issue golden because each golden
// input fails exactly one refine).
func SafePath() Val {
	return safePathVal{}
}

type safePathVal struct{}

func (safePathVal) Validate(present bool, v any) (any, []Issue) {
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received null")}
	}
	s, ok := v.(string)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received "+jsTypeName(v))}
	}
	is := []Issue{}
	if s == "" {
		is = append(is, NewIssue("custom", "path is empty"))
	} else {
		if strings.HasSuffix(s, "/") {
			is = append(is, NewIssue("custom", "path is a folder, not a file"))
		}
		if hasDotDotSegment(s) {
			is = append(is, NewIssue("custom", "path traversal detected"))
		}
		for _, seg := range strings.Split(s, "/") {
			if seg == "." || isJSMultiTrim(seg) {
				is = append(is, NewIssue("custom", `path segment is "." or has leading/trailing whitespace`))
				break
			}
		}
		if badSafePathChar(s) {
			is = append(is, NewIssue("custom", "path contains a disallowed character"))
		}
		if unsafePropName(strings.TrimPrefix(s, "/")) {
			is = append(is, NewIssue("custom", "path is an unsafe property name"))
		}
	}
	if len(is) > 0 {
		return nil, is
	}
	return s, nil
}

// isJSMultiTrim reports whether the segment has leading/trailing whitespace
// per JS `\s` — the FULL ECMA-262 set that V8's `/^\s|\s$/` matches, probed
// against Node 22: space, tab, LF, VT, FF, CR, U+00A0, U+1680, U+2000–U+200A,
// U+2028, U+2029, U+202F, U+205F, U+3000, U+FEFF (U+180E and U+200B are NOT
// `\s` in this V8). The original byte-level check missed every non-ASCII
// whitespace (NBSP and friends) — a silent accept where Node rejects.
func isJSMultiTrim(s string) bool {
	if s == "" {
		return false
	}
	r := []rune(s)
	return isJSWhitespace(r[0]) || isJSWhitespace(r[len(r)-1])
}

// badSafePathChar mirrors BAD_CHAR_RX = /[\\*\x00-\x1f\x7f\x80-\x9f
// \uD800-\uDFFF]/ (lone surrogates can't appear in Go strings).
func badSafePathChar(s string) bool {
	for _, r := range s {
		if r == '\\' || r == '*' {
			return true
		}
		if r < 0x80 {
			if r < 0x20 || r == 0x7f {
				return true
			}
		} else if r <= 0x9f || (r >= 0xd800 && r <= 0xdfff) {
			// C1 control block + the whole UTF-16 surrogate range (never
			// valid in Go's UTF-8 string rune iteration).
			return true
		}
	}
	return false
}

// isJSWhitespace is JS `\s` membership for a single code point (probed set,
// above; U+180E deliberately excluded to match V8 — it is NOT `\s` there).
func isJSWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r',
		0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// unsafePropName mirrors BLOCKEDFILE_RX (top-level name only, per the Node
// doc: the unsafe-property check is applied to the path with a SINGLE
// leading '/' stripped, and only matches the WHOLE top segment name — the
// Node regex anchors ^...$ on the replaced string).
//
// NOTE: the Node check is UNSAFE_SEGMENT_RX.test(s.replace(/^\//, ”)) — a
// SINGLE leading '/' is stripped, then the WHOLE remaining string is tested
// against the anchored name list. Go: same.
func unsafePropName(s string) bool {
	switch s {
	case "prototype", "constructor", "toString", "toLocaleString", "valueOf",
		"hasOwnProperty", "isPrototypeOf", "propertyIsEnumerable",
		"__defineGetter__", "__lookupGetter__", "__defineSetter__", "__lookupSetter__",
		"__proto__":
		return true
	}
	return false
}

// ProjectHistoryID — zz.projectHistoryId(): regex (ObjectId | Postgres int),
// custom 'invalid project history id'.
func ProjectHistoryID() Val {
	return projectHistoryIDVal{}
}

type projectHistoryIDVal struct{}

func (projectHistoryIDVal) Validate(present bool, v any) (any, []Issue) {
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received null")}
	}
	s, ok := v.(string)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received "+jsTypeName(v))}
	}
	if !projectHistoryRx.MatchString(s) {
		return nil, []Issue{NewIssue("custom", "invalid project history id")}
	}
	return s, nil
}

// ChunkID — zz.chunkId(): same regex, custom 'invalid chunk id'.
func ChunkID() Val {
	return chunkIDVal{}
}

type chunkIDVal struct{}

func (chunkIDVal) Validate(present bool, v any) (any, []Issue) {
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received null")}
	}
	s, ok := v.(string)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received "+jsTypeName(v))}
	}
	if !projectHistoryRx.MatchString(s) {
		return nil, []Issue{NewIssue("custom", "invalid chunk id")}
	}
	return s, nil
}

// SplitTestName — chain: kebab regex (FIRST), then min 3.
func SplitTestName() Val {
	return chainCaseMinVal{"split test name must be kebab-case", "split test name must be at least 3 characters long"}
}

// VariantName — same chain, different messages.
func VariantName() Val {
	return chainCaseMinVal{"variant name must be kebab-case", "variant name must be at least 3 characters long"}
}

type chainCaseMinVal struct{ kebabMsg, minMsg string }

func (c chainCaseMinVal) Validate(present bool, v any) (any, []Issue) {
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received null")}
	}
	s, ok := v.(string)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received "+jsTypeName(v))}
	}
	is := []Issue{}
	if !kebabCaseRx.MatchString(s) {
		is = append(is, NewIssue("invalid_format", c.kebabMsg))
	}
	if jsStringLen(s) < 3 {
		is = append(is, NewIssue("custom", c.minMsg))
	}
	if len(is) > 0 {
		return nil, is
	}
	return s, nil
}

// EventName — chain: min 1, regex, max 240. Empty input fails BOTH min and
// regex (golden: "Invalid event name; Invalid event name").
func EventName() Val { return eventNameVal{} }

type eventNameVal struct{}

func (eventNameVal) Validate(present bool, v any) (any, []Issue) {
	if !present {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received undefined")}
	}
	if isNull(v) {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received null")}
	}
	s, ok := v.(string)
	if !ok {
		return nil, []Issue{NewIssue("invalid_type", "Invalid input: expected string, received "+jsTypeName(v))}
	}
	is := []Issue{}
	if jsStringLen(s) < 1 {
		is = append(is, NewIssue("custom", "invalid event name"))
	}
	if !eventNameRx.MatchString(s) {
		is = append(is, NewIssue("invalid_format", "invalid event name"))
	}
	if jsStringLen(s) > 240 {
		is = append(is, NewIssue("custom", "event name is too long"))
	}
	if len(is) > 0 {
		return nil, is
	}
	return s, nil
}

// NumberIntNonnegative — z.number().int().nonnegative() (the multer
// `size` field of zz.uploadedFile()).
func NumberIntNonnegative() Val { return NumberVal{Integer: true, NonNegative: true} }

// UploadedFile — zz.uploadedFile(): z.strictObject of the multer file
// shape (fieldname, originalname=filepath, encoding (optional), mimetype,
// size int nonneg, destination, filename, path).
func UploadedFile() Val {
	return NewStrictObject(
		Field{Name: "fieldname", Schema: StringVal{}},
		Field{Name: "originalname", Schema: Filepath()},
		Field{Name: "encoding", Schema: StringVal{}, Optional: true},
		Field{Name: "mimetype", Schema: StringVal{}},
		Field{Name: "size", Schema: NumberIntNonnegative()},
		Field{Name: "destination", Schema: StringVal{}},
		Field{Name: "filename", Schema: StringVal{}},
		Field{Name: "path", Schema: StringVal{}},
	)
}
