package core

import (
	"encoding/json"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WriteOrderedValue emits v as JSON, PRESERVING the key order of primitive.D /
// primitive.A (BSON document order).
//
// Why: Go's encoding/json over a map is keyed unordered, but Node's
// json.stringify / res.json emits object keys in document order. The mongo
// driver decodes nested objects to primitive.D (ordered), so we emit those in
// order to match the Node wire byte-for-byte (pinned on the api profile
// GET /project/:id/details "features" object, whose 11 keys must appear in the
// exact order Node stores them).
//
// Scalars use encoding/json for correct escaping; numbers/bools are emitted
// directly (Go's int32/int64/float64/bool map to the same JSON text).
func WriteOrderedValue(sb *strings.Builder, v any) {
	switch x := v.(type) {
	case string:
		b, _ := json.Marshal(x)
		sb.Write(b)
	case primitive.ObjectID:
		sb.WriteString(`"` + x.Hex() + `"`)
	case int32:
		sb.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		sb.WriteString(strconv.FormatInt(x, 10))
	case float64:
		b, _ := json.Marshal(x)
		sb.Write(b)
	case bool:
		if x {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case primitive.A:
		sb.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			WriteOrderedValue(sb, e)
		}
		sb.WriteByte(']')
	case primitive.D:
		sb.WriteByte('{')
		for i, e := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			kb, _ := json.Marshal(e.Key)
			sb.Write(kb)
			sb.WriteByte(':')
			WriteOrderedValue(sb, e.Value)
		}
		sb.WriteByte('}')
	case nil:
		sb.WriteString("null")
	default:
		b, _ := json.Marshal(v)
		if b == nil {
			sb.WriteString("null")
		} else {
			sb.Write(b)
		}
	}
}

// OrderedD renders a primitive.D (ordered BSON object) as a JSON object string
// in stored key order.
func OrderedD(d primitive.D) []byte {
	var sb strings.Builder
	WriteOrderedValue(&sb, d)
	return []byte(sb.String())
}
