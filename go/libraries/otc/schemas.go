package otc

// schemas.go — 1:1 port of libraries/overleaf-editor-core/lib/schemas.js (zod /
// `z`+`zz` from @overleaf/validation-tools) onto the Go 1:1 of that library,
// `ollitex/go/libraries/validtools` (LIB-04). Same composition shape, same
// exports (Node `raw*` -> Go `Raw*`), same accept/reject semantics so the Node
// `schemas.test.js` oracle (rawTextOperation / rawRetainOp / rawLinkedFileData /
// rawFileMetadata `safeParse`) is mirrored green.
//
// Accept a value: `RawX.Validate(true, data)` returns zero Issues. The Node
// `schema.safeParse(x).success` is `len(issues) == 0`.
//
// validtools already provides: StringVal / NumberVal / BoolVal / EnumVal /
// StrictObject (+ Field Optional/Nullish) / UnionVal / ArrayVal and the `zz.*`
// leaf validators (ObjectID / BuildID / CompileGroup / CLSIServerID /
// RouteSegment). The handful of zod shorthands validtools does not expose as a
// Val are defined locally below (int min/max, z.literal, z.record, z.nullable,
// string().regex, uuid, iso.datetime, and the named-kinds refine).

import (
	"regexp"
	"strconv"

	vt "ollitex/go/libraries/validtools"
)

// --- json helpers -----------------------------------------------------------

// isJSONNull reports whether v is an explicit null (JSON null decodes to a nil
// any; the oracle passes Go `nil`).
func isJSONNull(v any) bool { return v == nil }

// toNumber coerces a JSON number to float64 (int/float widths).
func toNumber(v any) (float64, bool) {
	switch t := v.(type) {
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
	case float32:
		return float64(t), true
	case float64:
		return t, true
	default:
		return 0, false
	}
}

func typeIssue(what, got string) []vt.Issue {
	return []vt.Issue{vt.NewIssue("invalid_type", "Invalid input: expected "+what+", received "+got)}
}

// --- local Val shorthands ---------------------------------------------------

// isoDatetime — z.iso.datetime() (strict; `Z` always, numeric offset allowed).
type isoDatetimeVal struct{ dt vt.DTSchema }

func (s isoDatetimeVal) Validate(present bool, v any) (any, []vt.Issue) {
	_, iss := s.dt.Validate(present, v)
	if len(iss) > 0 {
		return nil, iss
	}
	return v, nil
}

func isoDatetime() vt.Val {
	return isoDatetimeVal{dt: vt.Datetime(vt.DatetimeOpts{Offset: true})}
}

// intVal — z.number().int() with optional min / max bounds.
type intVal struct {
	hasMin, hasMax bool
	min, max       float64
}

func (n intVal) Validate(present bool, v any) (any, []vt.Issue) {
	if !present {
		return nil, typeIssue("number", "undefined")
	}
	if isJSONNull(v) {
		return nil, typeIssue("number", "null")
	}
	f, ok := toNumber(v)
	if !ok {
		return nil, typeIssue("int", "not-a-number")
	}
	if f != float64(int64(f)) {
		return nil, typeIssue("int", "number")
	}
	if n.hasMin && f < n.min {
		return nil, []vt.Issue{vt.NewIssue("too_small", "Too small: expected number to be >="+strconv.FormatFloat(n.min, 'f', -1, 64))}
	}
	if n.hasMax && f > n.max {
		return nil, []vt.Issue{vt.NewIssue("too_big", "Too big: expected number to be <="+strconv.FormatFloat(n.max, 'f', -1, 64))}
	}
	return f, nil
}

func intPlain() vt.Val          { return vt.NumberVal{Integer: true} }
func intMin(min float64) vt.Val { return intVal{hasMin: true, min: min} }
func intMax(max float64) vt.Val { return intVal{hasMax: true, max: max} }

// literalVal — z.literal(target).
type literalVal struct{ target any }

func (l literalVal) Validate(present bool, v any) (any, []vt.Issue) {
	if !present {
		return nil, typeIssue("literal", "undefined")
	}
	if v == l.target {
		return v, nil
	}
	return nil, []vt.Issue{vt.NewIssue("invalid_literal", "Invalid literal value, expected "+literalLabel(l.target))}
}

func literalLabel(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	if b, ok := v.(bool); ok {
		return strconv.FormatBool(b)
	}
	return "literal"
}

func literal(target any) vt.Val { return literalVal{target: target} }

// recordVal — z.record(z.string(), item): a map[string]any whose values all pass item.
type recordVal struct{ item vt.Val }

func (r recordVal) Validate(present bool, v any) (any, []vt.Issue) {
	if !present {
		return nil, typeIssue("record", "undefined")
	}
	if isJSONNull(v) {
		return nil, typeIssue("record", "null")
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, typeIssue("record", "object")
	}
	var issues []vt.Issue
	for _, val := range m {
		if _, iss := r.item.Validate(true, val); len(iss) > 0 {
			issues = append(issues, iss...)
		}
	}
	if len(issues) > 0 {
		return nil, issues
	}
	return m, nil
}

func record(item vt.Val) vt.Val { return recordVal{item: item} }

// nullableVal — z.X().nullable(): an explicit null is accepted, else delegates.
type nullableVal struct{ inner vt.Val }

func (n nullableVal) Validate(present bool, v any) (any, []vt.Issue) {
	if present && isJSONNull(v) {
		return nil, nil
	}
	return n.inner.Validate(present, v)
}

func nullable(inner vt.Val) vt.Val { return nullableVal{inner: inner} }

// regexVal — z.string().regex(rx, {message}).
type regexVal struct {
	re  *regexp.Regexp
	msg string
}

func (s regexVal) Validate(present bool, v any) (any, []vt.Issue) {
	base, iss := vt.StringVal{}.Validate(present, v)
	if len(iss) > 0 {
		return base, iss
	}
	str, _ := v.(string)
	if !s.re.MatchString(str) {
		return nil, []vt.Issue{vt.NewIssue("invalid_format", s.msg)}
	}
	return str, nil
}

func strRegex(re *regexp.Regexp, msg string) vt.Val { return regexVal{re: re, msg: msg} }

// uuidVal — z.uuid().
type uuidVal struct{}

var uuidRx = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`,
)

func (uuidVal) Validate(present bool, v any) (any, []vt.Issue) {
	if !present {
		return nil, typeIssue("uuid", "undefined")
	}
	if isJSONNull(v) {
		return nil, typeIssue("uuid", "null")
	}
	s, ok := v.(string)
	if !ok {
		return nil, typeIssue("uuid", "string")
	}
	if !uuidRx.MatchString(s) {
		return nil, []vt.Issue{vt.NewIssue("invalid_format", "Invalid uuid")}
	}
	return s, nil
}

func uuid() vt.Val { return uuidVal{} }

// kindVal — z.string().refine(kind => !namedOriginKinds.includes(kind)).
type kindVal struct{ named map[string]bool }

func (k kindVal) Validate(present bool, v any) (any, []vt.Issue) {
	base, iss := vt.StringVal{}.Validate(present, v)
	if len(iss) > 0 {
		return base, iss
	}
	s, _ := v.(string)
	if k.named[s] {
		return nil, []vt.Issue{vt.NewIssue("custom", "origin kind has a shape of its own")}
	}
	return s, nil
}

// --- leaf building blocks ---------------------------------------------------

var (
	rawTrackingProps      = vt.NewStrictObject(fieldType("type", vt.EnumVal{Values: []string{"insert", "delete"}}), field("userId", vt.ObjectID()), field("ts", isoDatetime()))
	rawClearTrackingProps = vt.NewStrictObject(fieldType("type", literal("none")))
)

func field(name string, schema vt.Val) vt.Field { return vt.Field{Name: name, Schema: schema} }
func fieldOpt(name string, schema vt.Val) vt.Field {
	return vt.Field{Name: name, Optional: true, Schema: schema}
}
func fieldNullish(name string, schema vt.Val) vt.Field {
	return vt.Field{Name: name, Nullish: true, Schema: schema}
}
func fieldType(name string, schema vt.Val) vt.Field { return vt.Field{Name: name, Schema: schema} }

var (
	rawInsertOp = vt.NewUnion(
		vt.NewStrictObject(
			field("i", vt.StringVal{}),
			fieldOpt("commentIds", vt.ArrayVal{Item: vt.ObjectID()}),
			fieldOpt("tracking", rawTrackingProps),
		),
		vt.StringVal{},
	)

	rawRemoveOp = intMax(-1)

	rawRetainOp = vt.NewUnion(
		vt.NewStrictObject(
			field("r", intMin(1)),
			fieldOpt("commentIds", vt.ArrayVal{Item: vt.ObjectID()}),
			fieldOpt("tracking", vt.NewUnion(rawTrackingProps, rawClearTrackingProps)),
		),
		intMin(1),
	)

	rawScanOp = vt.NewUnion(rawInsertOp, rawRemoveOp, rawRetainOp)

	rawTextOperation = vt.NewStrictObject(
		field("textOperation", vt.ArrayVal{Item: rawScanOp}),
		fieldOpt("contentHash", vt.StringVal{}),
	)

	rawRange = vt.NewStrictObject(
		field("pos", intMin(0)),
		field("length", intMin(0)),
	)
)

var (
	rawAddCommentOperation = vt.NewStrictObject(
		field("commentId", vt.ObjectID()),
		field("ranges", vt.ArrayVal{Item: rawRange}),
		fieldOpt("resolved", vt.BoolVal{}),
	)

	rawSetCommentStateOperation = vt.NewStrictObject(
		field("commentId", vt.ObjectID()),
		field("resolved", vt.BoolVal{}),
	)

	rawDeleteCommentOperation = vt.NewStrictObject(
		field("deleteComment", vt.ObjectID()),
	)

	rawEditNoOperation = vt.NewStrictObject(
		field("noOp", literal(true)),
	)

	rawEditOperation = vt.NewUnion(
		rawTextOperation,
		rawAddCommentOperation,
		rawDeleteCommentOperation,
		rawSetCommentStateOperation,
		rawEditNoOperation,
	)

	rawComment = vt.NewStrictObject(
		field("id", vt.ObjectID()),
		field("ranges", vt.ArrayVal{Item: rawRange}),
		fieldOpt("resolved", vt.BoolVal{}),
	)

	rawTrackedChange = vt.NewStrictObject(
		field("range", rawRange),
		field("tracking", rawTrackingProps),
	)

	rawStringFileData = vt.NewStrictObject(
		field("content", vt.StringVal{}),
		fieldOpt("comments", vt.ArrayVal{Item: rawComment}),
		fieldOpt("trackedChanges", vt.ArrayVal{Item: rawTrackedChange}),
	)
)

// rawLinkedFileData — z.discriminatedUnion('provider', [...]). Modelled as a
// union of strict objects, each with a literal `provider` arm; the strict-object
// arm both discriminates on the literal and rejects unrecognised keys (which is
// exactly the oracle's accept/reject contract).
var rawLinkedFileData = vt.NewUnion(
	vt.NewStrictObject( // provider: 'url'
		fieldType("provider", literal("url")),
		field("url", vt.StringVal{}),
		fieldOpt("importedAt", isoDatetime()),
	),
	vt.NewStrictObject( // provider: 'project_file'
		fieldType("provider", literal("project_file")),
		fieldOpt("source_project_id", vt.ObjectID()),
		fieldOpt("v1_source_doc_id", vt.NumberVal{}),
		field("source_entity_path", vt.StringVal{}),
		fieldOpt("source_project_display_name", vt.StringVal{}),
		fieldOpt("importedAt", isoDatetime()),
	),
	vt.NewStrictObject( // provider: 'project_output_file'
		fieldType("provider", literal("project_output_file")),
		fieldOpt("source_project_id", vt.ObjectID()),
		fieldOpt("v1_source_doc_id", vt.NumberVal{}),
		field("source_output_file_path", vt.StringVal{}),
		fieldOpt("build_id", vt.BuildID()),
		fieldOpt("compileGroup", vt.CompileGroup()),
		fieldOpt("clsiServerId", vt.CLSIServerID()),
		fieldOpt("importedAt", isoDatetime()),
	),
	vt.NewStrictObject( // provider: 'mendeley'
		fieldType("provider", literal("mendeley")),
		fieldNullish("group_id", vt.RouteSegment()),
		fieldOpt("importer_id", vt.StringVal{}),
		fieldOpt("v1_importer_id", vt.NumberVal{}),
		fieldOpt("importedAt", isoDatetime()),
	),
	vt.NewStrictObject( // provider: 'zotero'
		fieldType("provider", literal("zotero")),
		fieldOpt("format", vt.EnumVal{Values: []string{"bibtex", "biblatex"}}),
		fieldNullish("group_id", vt.RouteSegment()),
		fieldNullish("zoteroGroupId", vt.RouteSegment()),
		fieldOpt("bibFormat", vt.EnumVal{Values: []string{"bibtex", "biblatex"}}),
		fieldNullish("importedByUserId", vt.ObjectID()),
		fieldNullish("importedByName", vt.StringVal{}),
		fieldOpt("importer_id", vt.StringVal{}),
		fieldOpt("v1_importer_id", vt.NumberVal{}),
		fieldOpt("importedAt", isoDatetime()),
	),
	vt.NewStrictObject( // provider: 'papers'
		fieldType("provider", literal("papers")),
		fieldNullish("group_id", vt.RouteSegment()),
		fieldOpt("importer_id", vt.StringVal{}),
		fieldOpt("v1_importer_id", vt.NumberVal{}),
		fieldOpt("importedAt", isoDatetime()),
	),
)

var rawBlobHash = strRegex(regexp.MustCompile(`^[0-9a-f]{40}$`), "invalid blob hash")

var rawFileMetadata = vt.NewUnion(
	vt.NewStrictObject(), // doc / clear metadata: {}
	vt.NewStrictObject(field("importedAt", isoDatetime())),
	rawLinkedFileData,
	vt.NewStrictObject(
		fieldOpt("main", vt.BoolVal{}),
		fieldOpt("mainBibliography", vt.BoolVal{}),
		fieldOpt("importedAt", isoDatetime()),
	),
	vt.NewStrictObject(
		field("agent", vt.StringVal{}),
		field("agentDataId", vt.NumberVal{Integer: true}),
		fieldOpt("importedAt", isoDatetime()),
	),
)

var (
	rawHashFileData = vt.NewStrictObject(
		field("hash", rawBlobHash),
		fieldOpt("rangesHash", rawBlobHash),
	)

	rawBinaryFileData = vt.NewStrictObject(
		field("hash", rawBlobHash),
		field("byteLength", intMin(0)),
	)

	rawLazyStringFileData = vt.NewStrictObject(
		field("hash", rawBlobHash),
		field("stringLength", intMin(0)),
		fieldOpt("rangesHash", rawBlobHash),
		fieldOpt("operations", vt.ArrayVal{Item: rawEditOperation}),
	)

	rawHollowBinaryFileData = vt.NewStrictObject(
		field("byteLength", intMin(0)),
	)

	rawHollowStringFileData = vt.NewStrictObject(
		field("stringLength", intMin(0)),
	)

	rawFileData = vt.NewUnion(
		rawBinaryFileData,
		rawHashFileData,
		rawHollowBinaryFileData,
		rawHollowStringFileData,
		rawLazyStringFileData,
		rawStringFileData,
	)
)

// rawFile — rawFileData arms, each extended with an optional metadata.
var rawFile = vt.NewUnion(fileArmsWithMetadata(
	rawBinaryFileData,
	rawHashFileData,
	rawHollowBinaryFileData,
	rawHollowStringFileData,
	rawLazyStringFileData,
	rawStringFileData,
))

func fileArmsWithMetadata(arms ...vt.Val) *vt.UnionVal {
	withMeta := make([]vt.Val, 0, len(arms))
	for _, a := range arms {
		so, ok := a.(*vt.StrictObject)
		if !ok {
			panic("otc.schemas: file arm must be a strict object")
		}
		fields := append(append([]vt.Field{}, so.Fields...), fieldOpt("metadata", rawFileMetadata))
		withMeta = append(withMeta, vt.NewStrictObject(fields...))
	}
	return vt.NewUnion(withMeta...)
}

var (
	rawFileMap        = record(rawFile)
	rawV2DocVersions  = record(vt.NewStrictObject(field("pathname", vt.StringVal{}), field("v", intPlain())))
	rawSnapshotSchema = vt.NewStrictObject(
		field("files", rawFileMap),
		fieldOpt("projectVersion", vt.StringVal{}),
		fieldNullish("v2DocVersions", rawV2DocVersions),
		fieldOpt("timestamp", isoDatetime()),
	)
)

// --- Origin (mirrors Origin.fromRaw: 3 restore variants + a generic kind) ---

var namedOriginKinds = map[string]bool{"restore": true, "file-restore": true, "project-restore": true}

var rawBaseOrigin = vt.NewStrictObject(
	field("kind", kindVal{named: namedOriginKinds}),
	fieldOpt("historyClientId", uuid()),
)

var rawRestoreOrigin = vt.NewStrictObject(
	fieldType("kind", literal("restore")),
	field("version", intPlain()),
	field("timestamp", isoDatetime()),
	fieldOpt("historyClientId", uuid()),
)

var rawRestoreFileOrigin = vt.NewStrictObject(
	fieldType("kind", literal("file-restore")),
	field("version", intPlain()),
	field("path", vt.StringVal{}),
	field("timestamp", isoDatetime()),
	fieldOpt("historyClientId", uuid()),
)

var rawRestoreProjectOrigin = vt.NewStrictObject(
	fieldType("kind", literal("project-restore")),
	field("version", intPlain()),
	field("timestamp", isoDatetime()),
	fieldOpt("historyClientId", uuid()),
)

var rawOrigin = vt.NewUnion(
	rawRestoreOrigin,
	rawRestoreFileOrigin,
	rawRestoreProjectOrigin,
	rawBaseOrigin,
)

// --- Operation (mirrors Operation.fromRaw) ---

var rawAddFileOperation = vt.NewStrictObject(
	field("pathname", vt.StringVal{}),
	field("file", rawFile),
)

var rawMoveFileOperation = vt.NewStrictObject(
	field("pathname", vt.StringVal{}),
	field("newPathname", vt.StringVal{}),
)

// rawEditFileOperation — rawEditOperation arms, each extended with a pathname.
var rawEditFileOperation = vt.NewUnion(editArmsWithPathname(
	rawTextOperation,
	rawAddCommentOperation,
	rawDeleteCommentOperation,
	rawSetCommentStateOperation,
	rawEditNoOperation,
))

func editArmsWithPathname(arms ...vt.Val) *vt.UnionVal {
	withPath := make([]vt.Val, 0, len(arms))
	for _, a := range arms {
		so, ok := a.(*vt.StrictObject)
		if !ok {
			panic("otc.schemas: edit arm must be a strict object")
		}
		fields := append(append([]vt.Field{}, so.Fields...), field("pathname", vt.StringVal{}))
		withPath = append(withPath, vt.NewStrictObject(fields...))
	}
	return vt.NewUnion(withPath...)
}

var rawSetFileMetadataOperation = vt.NewStrictObject(
	field("pathname", vt.StringVal{}),
	field("metadata", rawFileMetadata),
)

var rawNoOperation = vt.NewStrictObject()

var rawOperation = vt.NewUnion(
	rawAddFileOperation,
	rawEditFileOperation,
	rawMoveFileOperation,
	rawSetFileMetadataOperation,
	rawNoOperation,
)

// rawChange — mirrors Change.fromRaw's strict shape.
var rawChange = vt.NewStrictObject(
	field("operations", vt.ArrayVal{Item: rawOperation}),
	field("timestamp", isoDatetime()),
	fieldOpt("authors", vt.ArrayVal{Item: nullable(intPlain())}),
	fieldOpt("v2Authors", vt.ArrayVal{Item: nullable(vt.ObjectID())}),
	fieldOpt("origin", rawOrigin),
	fieldOpt("projectVersion", vt.StringVal{}),
	fieldOpt("v2DocVersions", rawV2DocVersions),
)
