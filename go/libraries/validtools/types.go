package validtools

import "time"

// jsTypeName maps a present Go value to the JS type name zod reports for it
// (the "received X" suffix of type issues).
//
// Absence is the CALLER's convention, not the value's: a missing field is
// passed as present=false and renders "received undefined"; an explicit JSON
// null passed as a nil map value renders "received null" (see isNull).
func jsTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case time.Time:
		return "date"
	case error:
		// Node getTypeName: `error instanceof Error` → "error" (a Go error
		// value is the same kind of object — e.g. a foreign err carried into
		// a schema must render `received error`, not `received object`).
		return "error"
	case bool:
		return "boolean"
	case string:
		return "string"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float64, float32:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		// Any other Go value is validated as a JS object (services decode
		// JSON into map[string]any / scalars; struct inputs are objects).
		return "object"
	}
}

// isNull reports whether a PRESENT map value is JSON null (nil any) — the
// "received null" case. Absence is a separate flag everywhere in this package.
func isNull(v any) bool { return v == nil }

// AbsentVal is the "absent after normalisation" marker: the value a schema
// returns (with no issues) to mean "the key normalised to undefined" (Node:
// the datetime `.transform` collapsing null/absent to undefined, or
// `z.nullish().transform(v => v ?? undefined)`). The strict-object framework
// drops such keys from its output map, mirroring Node (an undefined value
// leaves no key after JSON serialisation). A plain nil, by contrast, means
// "present, JSON null" (e.g. a nullable date kept as null by the transform).
type absentVal struct{}

// Absent renders as absent in the framework; exported for tests.
func Absent() any { return absentVal{} }
