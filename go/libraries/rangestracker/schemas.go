package rangestracker

import "ollitex/go/libraries/validtools"

// schemas.go — 1:1 port of libraries/ranges-tracker/schemas.js (zod) onto
// validtools (the Go 1:1 of @overleaf/validation-tools).
//
// The schemas model WIRE REALITY (ranges reach docstore from several writers
// and round-trip without normalization), so legacy flags
// (fixedRemoveChange / orderedRejections) and history-restore `resolved` are
// accepted keys while everything else is rejected (strict objects).
//
// Node exports: { insertOp, deleteOp, commentOp, comment, trackedChange, ranges }.
// Go: InsertOpSchema / DeleteOpSchema / CommentOpSchema / CommentSchema /
// TrackedChangeSchema / RangesSchema + ParseRanges (the Node safeParse).

// intPosition is z.number().int().min(0).
var intPosition = validtools.NumberIntNonnegative()

// InsertOpSchema — z.strictObject({ i, p, u?, fixedRemoveChange?, orderedRejections? }).
var InsertOpSchema = validtools.NewStrictObject(
	validtools.Field{Name: "i", Schema: validtools.StringVal{}},
	validtools.Field{Name: "p", Schema: intPosition},
	validtools.Field{Name: "u", Optional: true, Schema: validtools.BoolVal{}},
	validtools.Field{Name: "fixedRemoveChange", Optional: true, Schema: validtools.BoolVal{}},
	validtools.Field{Name: "orderedRejections", Optional: true, Schema: validtools.BoolVal{}},
)

// DeleteOpSchema — same shape with d.
var DeleteOpSchema = validtools.NewStrictObject(
	validtools.Field{Name: "d", Schema: validtools.StringVal{}},
	validtools.Field{Name: "p", Schema: intPosition},
	validtools.Field{Name: "u", Optional: true, Schema: validtools.BoolVal{}},
	validtools.Field{Name: "fixedRemoveChange", Optional: true, Schema: validtools.BoolVal{}},
	validtools.Field{Name: "orderedRejections", Optional: true, Schema: validtools.BoolVal{}},
)

// CommentOpSchema — { c, p, t (objectId), u?, resolved? }.
var CommentOpSchema = validtools.NewStrictObject(
	validtools.Field{Name: "c", Schema: validtools.StringVal{}},
	validtools.Field{Name: "p", Schema: intPosition},
	validtools.Field{Name: "t", Schema: validtools.ObjectID()},
	validtools.Field{Name: "u", Optional: true, Schema: validtools.BoolVal{}},
	validtools.Field{Name: "resolved", Optional: true, Schema: validtools.BoolVal{}},
)

// commentMetadata — { user_id?, ts? }.
var CommentMetadataSchema = validtools.NewStrictObject(
	validtools.Field{Name: "user_id", Optional: true, Schema: validtools.StringVal{}},
	validtools.Field{Name: "ts", Optional: true, Schema: validtools.StringVal{}},
)

// trackedChangeMetadata — both always set (author + edit time).
var TrackedChangeMetadataSchema = validtools.NewStrictObject(
	validtools.Field{Name: "user_id", Schema: validtools.StringVal{}},
	validtools.Field{Name: "ts", Schema: validtools.StringVal{}},
)

// CommentSchema — { id?, op: commentOp, metadata? }.
var CommentSchema = validtools.NewStrictObject(
	validtools.Field{Name: "id", Optional: true, Schema: validtools.StringVal{}},
	validtools.Field{Name: "op", Schema: CommentOpSchema},
	validtools.Field{Name: "metadata", Optional: true, Schema: CommentMetadataSchema},
)

// TrackedChangeSchema — { id?, op: insertOp|deleteOp, metadata (required) }.
var TrackedChangeSchema = validtools.NewStrictObject(
	validtools.Field{Name: "id", Optional: true, Schema: validtools.StringVal{}},
	validtools.Field{
		Name:   "op",
		Schema: validtools.NewUnion(InsertOpSchema, DeleteOpSchema),
	},
	validtools.Field{Name: "metadata", Schema: TrackedChangeMetadataSchema},
)

// RangesSchema — { comments?, changes? } (Node: the exported `ranges` schema).
var RangesSchema = validtools.NewStrictObject(
	validtools.Field{Name: "comments", Optional: true, Schema: validtools.ArrayVal{Item: CommentSchema}},
	validtools.Field{Name: "changes", Optional: true, Schema: validtools.ArrayVal{Item: TrackedChangeSchema}},
)

// ParseRanges is the Node `ranges.safeParse(data)` contract: returns the
// normalised value and success (zero issues).
func ParseRanges(data any) (value any, success bool) {
	value, issues := RangesSchema.Validate(true, data)
	return value, len(issues) == 0
}
