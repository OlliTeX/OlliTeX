package docstore

import (
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Marker values inside the neutral tree (see normalizeTree):
//
// jobj is an explicitly ordered JSON object (response views keep Node's
// key insertion order; Go maps would have to sort).
type (
	objectIDHex string
	jsDate      time.Time
	jobj        []jpair
	jpair       struct {
		key string
		val any
	}
)

func newObjectIDHex(hex string) objectIDHex { return objectIDHex(hex) }
func newJSDate(t time.Time) jsDate          { return jsDate(t) }

// toJSDate renders a time like JS Date.prototype.toISOString: UTC with
// exactly three millisecond digits.
func toJSDate(t time.Time) string {
	ms := int(t.UTC().Nanosecond() / 1e6)
	return t.UTC().Format("2006-01-02T15:04:05.") + fmt.Sprintf("%03dZ", ms)
}

// normalizeTree converts driver values (primitive.ObjectID, time.Time,
// int32/64, bsoncore-ish maps) into the neutral tree used for both storage
// conversion and JSON output: map[string]any, []any, string, int64, float64,
// bool, nil, objectIDHex, jsDate.
func normalizeTree(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case primitive.ObjectID:
		return newObjectIDHex(t.Hex())
	case time.Time:
		return newJSDate(t)
	case int:
		return int64(t)
	case int32:
		return int64(t)
	case int64:
		return t
	case float32:
		return float64(t)
	case float64:
		return t
	case bool:
		return t
	case string:
		return t
	case objectIDHex:
		return t
	case jsDate:
		return t
	case []string:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = normalizeTree(e)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = normalizeTree(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = normalizeTree(e)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[fmt.Sprint(k)] = normalizeTree(e)
		}
		return out
	case primitive.D:
		out := make(map[string]any, len(t))
		for i := range len(t) {
			out[t[i].Key] = normalizeTree(t[i].Value)
		}
		return out
	case primitive.A:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = normalizeTree(e)
		}
		return out
	default:
		return fmt.Sprint(t)
	}
}

// jsonTree marshals a neutral tree the way Node's JSON.stringify does:
// objectIDHex → quoted hex string; jsDate → quoted ISO string; numbers as
// JSON numbers; object keys in sorted order for deterministic output (Node
// keeps BSON insertion order — same object, consumed by named key). HTML
// characters are NOT escaped, matching JSON.stringify (unlike Go's default).
func jsonTree(v any) ([]byte, error) {
	var sb strings.Builder
	writeJSONNode(&sb, v)
	return []byte(sb.String()), nil
}

func writeJSONNode(sb *strings.Builder, v any) {
	switch t := v.(type) {
	case nil:
		sb.WriteString("null")
	case objectIDHex:
		writeJSONString(sb, string(t))
	case jsDate:
		writeJSONString(sb, toJSDate(time.Time(t)))
	case primitive.ObjectID:
		writeJSONString(sb, t.Hex())
	case time.Time:
		writeJSONString(sb, toJSDate(t))
	case jobj:
		sb.WriteByte('{')
		for i, kv := range t {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeJSONString(sb, kv.key)
			sb.WriteByte(':')
			writeJSONNode(sb, kv.val)
		}
		sb.WriteByte('}')
	case string:
		writeJSONString(sb, t)
	case bool:
		if t {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case int:
		sb.WriteString(fmt.Sprintf("%d", t))
	case int32:
		sb.WriteString(fmt.Sprintf("%d", t))
	case int64:
		sb.WriteString(fmt.Sprintf("%d", t))
	case float64:
		writeJSONFloat(sb, t)
	case []any:
		sb.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeJSONNode(sb, e)
		}
		sb.WriteByte(']')
	case []string:
		sb.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeJSONString(sb, e)
		}
		sb.WriteByte(']')
	case map[string]any:
		writeJSONObject(sb, t)
	default:
		writeJSONString(sb, fmt.Sprint(t))
	}
}

func writeJSONObject(sb *strings.Builder, m map[string]any) {
	sb.WriteByte('{')
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// insertion-independent determinism: sorted keys (Node keeps insertion
	// order; the object is consumed by named key on the client)
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte(',')
		}
		writeJSONString(sb, k)
		sb.WriteByte(':')
		writeJSONNode(sb, m[k])
	}
	sb.WriteByte('}')
}

func writeJSONFloat(sb *strings.Builder, f float64) {
	// Go's shortest round-trip formatting matches JS Number output for the
	// magnitudes Overleaf stores (small integers, ratios).
	sb.WriteString(fmt.Sprintf("%g", f))
}

// writeJSONString escapes like JSON.stringify: quotes, backslash, and control
// characters (no HTML escaping).
func writeJSONString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		default:
			if r < 0x20 {
				sb.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}

// jsonEqual is a structural deep-equality over neutral trees (Node
// _.isEqual on the same shapes).
func jsonEqual(a, b any) bool {
	ta, tb := normalizeTree(a), normalizeTree(b)
	switch x := ta.(type) {
	case nil:
		return tb == nil
	case objectIDHex:
		if y, ok := tb.(objectIDHex); ok {
			return y == x
		}
		if y, ok := tb.(primitive.ObjectID); ok {
			if id, err := primitive.ObjectIDFromHex(string(x)); err == nil {
				return id == y
			}
			return false
		}
		return false
	case primitive.ObjectID:
		if y, ok := tb.(primitive.ObjectID); ok {
			return x == y
		}
		if y, ok := tb.(objectIDHex); ok {
			if id, err := primitive.ObjectIDFromHex(string(y)); err == nil {
				return x == id
			}
			return false
		}
		return false
	case jsDate:
		if y, ok := tb.(jsDate); ok {
			return x == y
		}
		if y, ok := tb.(time.Time); ok {
			return x == jsDate(y)
		}
		return false
	case time.Time:
		if y, ok := tb.(time.Time); ok {
			return x == y
		}
		if y, ok := tb.(jsDate); ok {
			return jsDate(x) == y
		}
		return false
	case string:
		y, ok := tb.(string)
		return ok && y == x
	case bool:
		y, ok := tb.(bool)
		return ok && y == x
	case int64:
		y, ok := tb.(int64)
		return ok && y == x
	case float64:
		y, ok := tb.(float64)
		return ok && y == x
	case []any:
		y, ok := tb.([]any)
		if !ok || len(y) != len(x) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := tb.(map[string]any)
		if !ok || len(y) != len(x) {
			return false
		}
		for k, ev := range x {
			yv, present := y[k]
			if !present || !jsonEqual(ev, yv) {
				return false
			}
		}
		return true
	default:
		// both sides fell through to the same default rendering
		return ta == tb
	}
}
