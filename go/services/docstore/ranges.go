package docstore

// ranges.go — RangeManager (jsonRangesToMongo / fixCommentIds /
// shouldUpdateRanges) plus the archive JSON (de)serialization
// (DocArchiveManager), 1:1 with the Node behaviour.
//
// The neutral tree uses objectIDHex for ObjectIDs and jsDate for BSON dates;
// both convert from strings the way RangeManager._safeObjectId / new Date
// do.

import (
	"encoding/json"
	"strconv"
	"strings"
)

// safeObjectID = RangeManager._safeObjectId 1:1: 24-hex strings become
// ObjectIDs, everything else passes through untouched.
func safeObjectID(v any) any {
	if s, isStr := v.(string); isStr {
		if hex24.MatchString(s) {
			return newObjectIDHex(s)
		}
	}
	return v
}

// updateCommentMetadata = jsonRangesToMongo.updateMetadata 1:1.
func updateMetaNode(meta map[string]any) {
	if ts, has := meta["ts"]; has {
		if s, isStr := ts.(string); isStr {
			if t, ok := parseJSDate(s); ok {
				meta["ts"] = newJSDate(t)
			} else {
				// Node: new Date(invalid) → Invalid Date → BSON null
				meta["ts"] = nil
			}
		}
	}
	if uid, has := meta["user_id"]; has {
		meta["user_id"] = safeObjectID(uid)
	}
}

// jsonRangesToMongo 1:1: converts the validated ranges tree in place (change
// ids, metadata ts/user_id; comment ids from op.t, op.t ← id, `resolved`
// stripped).
func jsonRangesToMongo(ranges map[string]any) map[string]any {
	if ranges == nil {
		return nil
	}
	if changes, isArr := ranges["changes"].([]any); isArr {
		for i := range changes {
			ch, isCh := changes[i].(map[string]any)
			if !isCh {
				continue
			}
			ch["id"] = safeObjectID(getRaw(ch, "id"))
			if md, isMd := ch["metadata"].(map[string]any); isMd {
				updateMetaNode(md)
			}
		}
	}
	if comments, isArr := ranges["comments"].([]any); isArr {
		for i := range comments {
			c, isC := comments[i].(map[string]any)
			if !isC {
				continue
			}
			var tRaw any
			if op, isOp := c["op"].(map[string]any); isOp {
				tRaw = getRaw(op, "t")
			}
			id := safeObjectID(orElse(tRaw, getRaw(c, "id")))
			c["id"] = id
			if op, isOp := c["op"].(map[string]any); isOp {
				op["t"] = id
				delete(op, "resolved")
			}
			if md, isMd := c["metadata"].(map[string]any); isMd {
				updateMetaNode(md)
			}
		}
	}
	return ranges
}

func getRaw(m map[string]any, key string) any {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	return v
}

func orElse(a, b any) any {
	// JS `a || b` semantics
	switch t := a.(type) {
	case nil:
		return b
	case string:
		if t != "" {
			return a
		}
	case bool:
		if t {
			return a
		}
	case float64:
		if t != 0 {
			return a
		}
	default:
		return a
	}
	return b
}

// fixCommentIDs = RangeManager.fixCommentIds 1:1 (prefer op.t when truthy).
func fixCommentIDs(d *Doc) {
	if d == nil || d.Ranges == nil {
		return
	}
	rng, isMap := d.Ranges.(map[string]any)
	if !isMap {
		return
	}
	comments, isArr := rng["comments"].([]any)
	if !isArr {
		return
	}
	for i := range comments {
		c, isC := comments[i].(map[string]any)
		if !isC {
			continue
		}
		op, isOp := c["op"].(map[string]any)
		if !isOp {
			continue
		}
		if t := truthy(op["t"]); t {
			c["id"] = op["t"]
		}
	}
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	}
	return true // ObjectID / other values are truthy in JS
}

// validRangesField validates the body/params ranges object and hands back
// the decoded tree (used by updateDoc).
func validRangesField(o *objectInput, key, path, root string, iss *[]issue, out *map[string]any) bool {
	if !o.has(key) {
		*iss = append(*iss, issue{root, typeIssue("object", "undefined", path)})
		return false
	}
	v := o.fields[key]
	m, isMap := v.(map[string]any)
	if !isMap || m == nil {
		got := "undefined"
		if v != nil {
			got = jsonReceived(v)
		}
		*iss = append(*iss, issue{root, typeIssue("object", got, path)})
		return false
	}
	if !validateRanges(root, m, path, iss) {
		return false
	}
	*out = m
	return true
}

// archiveDocJSON = JSON.stringify({lines, ranges, rev, schema_v: 1}) 1:1 —
// Node object-literal order (lines, ranges, rev, schema_v), omitting
// undefined ranges/rev (JSON.stringify drops undefined properties).
func archiveDocJSON(d *Doc) ([]byte, error) {
	var out string
	out += "{\"lines\":"
	b, ok := linesJSON(d.Lines)
	if !ok {
		return nil, ErrNoLines
	}
	out += b
	if d.Ranges != nil {
		out += `,"ranges":`
		var sb strings.Builder
		writeJSONNode(&sb, d.Ranges)
		out += sb.String()
	}
	if d.Rev != nil {
		out += `,"rev":` + itoa64(*d.Rev)
	}
	out += `,"schema_v":1}`
	return []byte(out), nil
}

func linesJSON(lines *[]string) (string, bool) {
	if lines == nil {
		return "", false
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, l := range *lines {
		if i > 0 {
			sb.WriteByte(',')
		}
		writeJSONString(&sb, l)
	}
	sb.WriteByte(']')
	return sb.String(), true
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}

// deserializeArchivedDoc = _deserializeArchivedDoc 1:1.
func deserializeArchivedDoc(data []byte) (ArchivedDoc, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return ArchivedDoc{}, ErrArchiveFmt
	}
	doc := ArchivedDoc{}
	switch t := v.(type) {
	case map[string]any:
		if t["schema_v"] == float64(1) {
			lines, isArr := t["lines"].([]any)
			if !isArr {
				return ArchivedDoc{}, ErrArchiveFmt
			}
			strs := make([]string, 0, len(lines))
			for _, e := range lines {
				s, isStr := e.(string)
				if !isStr {
					return ArchivedDoc{}, ErrArchiveFmt
				}
				strs = append(strs, s)
			}
			doc.Lines = strs
			if rng, ok := t["ranges"].(map[string]any); ok {
				doc.Ranges = jsonRangesToMongo(rng)
			}
		} else if _, isArr := t["lines"].([]any); isArr {
			return ArchivedDoc{}, ErrArchiveFmt // schema_v!==1 → not the v1 shape
		} else {
			return ArchivedDoc{}, ErrArchiveFmt
		}
	case []any:
		strs := make([]string, 0, len(t))
		for _, e := range t {
			s, isStr := e.(string)
			if !isStr {
				return ArchivedDoc{}, ErrArchiveFmt
			}
			strs = append(strs, s)
		}
		doc.Lines = strs
	default:
		return ArchivedDoc{}, ErrArchiveFmt
	}
	// Note: the legacy array shape carries no rev (Node only reads doc.rev).
	if obj, isObj := any(v).(map[string]any); isObj {
		if rv, has := obj["rev"]; has && rv != nil {
			if n, isNum := rv.(float64); isNum {
				rev := int64(n)
				doc.Rev = &rev
			}
		}
	}
	return doc, nil
}
