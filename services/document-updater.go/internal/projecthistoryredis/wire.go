package projecthistoryredis

// wire.go — ordered JSON wire for the vendored project-history queue.
//
// The vendored ProjectHistoryRedisManager builds each project-update
// object literal with a fixed key insertion order (Node `JSON.stringify`
// preserves it) and pushes it onto the Redis `ProjectHistory:Ops:<
// project>` list. Go's encoding/json sorts map keys, so byte-pinning the
// vendored wire uses the ordered-pair encoder below for every object the
// module itself constructs. Free-form metadata objects the vendor passes
// through ({ts, user_id, resolved}) are Go maps: sorted key order is
// oracle-compatible for the fixture metadata keys.
//
// Key orders (as vendored, and as the oracle fixtures pin them):
//
//	projectUpdate (rename):  pathname, new_pathname, meta, version,
//	                        projectHistoryId, <entityType>
//	projectUpdate (add):     pathname, docLines?, url?, meta, version,
//	                        hash?, metadata?, projectHistoryId,
//	                        createdBlob, ranges?, <entityType>
//	projectUpdate (resync):  resyncDocContent, projectHistoryId, path,
//	                        doc, meta
//	projectUpdate (struct):  resyncProjectStructure:{docs, files},
//	                        projectHistoryId, meta[,
//	                        resyncProjectStructureOnly]
//	resyncDocContent:        version[, ranges, resolvedCommentIds] |
//	                        [historyOTRanges]
//
// meta:                     user_id?, ts[, origin|source, type?]
// historyOTRanges:          comments?, trackedChanges? (verbatim raw items)
// raw comment:              id?, ranges?:[{pos, length}]
// raw trackedChange:        range:{pos, length}, tracking:{type[, userId?,
//	                        ts?]}
// history ranges (HRS):     changes?:[{op, metadata?}], comments?[{op,
//	                        metadata?}] — op: {p, i?|d?|c?, t?, hpos?,
//	                        hlen?}; metadata: {ts?, user_id?, resolved?}
// ranges (HRS) container:   {comments?, changes?} (comments first).

import (
	"bytes"
	"encoding/json"
)

// pair is one insertion-ordered key/value of an object literal.
type pair struct {
	key string
	val any
}

// obj builds an ordered object from alternating key/value args.
func obj(items ...any) []pair {
	pairs := make([]pair, 0, len(items)/2)
	for i := 0; i < len(items); i += 2 {
		pairs = append(pairs, pair{key: items[i].(string), val: items[i+1]})
	}
	return pairs
}

// encodeOrdered mirrors `JSON.stringify` on an insertion-ordered object:
// keys in the order given, values via jsonWire.
func encodeOrdered(pairs []pair) (string, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(p.key)
		if err != nil {
			return "", err
		}
		b.Write(k)
		b.WriteByte(':')
		v, err := jsonWire(p.val)
		if err != nil {
			return "", err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.String(), nil
}

// jsonWire mirrors `JSON.stringify` on a wire value:
//   - []pair      -> ordered object
//   - []any slice -> bare array
//   - string      -> JSON string (Go escaping matches JSON for the wire
//     charset)
//   - other      -> encoding/json (ints, bools, canonical structs)
func jsonWire(v any) ([]byte, error) {
	switch t := v.(type) {
	case nil:
		return []byte("null"), nil
	case []pair:
		s, err := encodeOrdered(t)
		if err != nil {
			return nil, err
		}
		return []byte(s), nil
	case []any:
		b := []byte{'['}
		for i, e := range t {
			if i > 0 {
				b = append(b, ',')
			}
			ev, err := jsonWire(e)
			if err != nil {
				return nil, err
			}
			b = append(b, ev...)
		}
		return append(b, ']'), nil
	case string:
		s, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		return s, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
}
