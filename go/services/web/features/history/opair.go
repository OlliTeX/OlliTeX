package history

import (
	"bytes"
	"encoding/json"
)

// opair is an order-preserving JSON object (Node objects keep insertion
// order through res.json; Go maps do not). set keeps insertion order; new
// keys are appended (Node: data.x = y appends the key at the end for
// previously absent keys).
type opair struct {
	kvs [][2]json.RawMessage // key/value pairs (key = JSON string token)
}

func (o *opair) UnmarshalJSON(b []byte) error {
	o.kvs = o.kvs[:0]
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errNotObject
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		var kv json.RawMessage
		if err := dec.Decode(&kv); err != nil {
			return err
		}
		k, _ := json.Marshal(kt) // quoted key string token
		o.kvs = append(o.kvs, [2]json.RawMessage{k, kv})
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	return nil
}

var (
	errNotObject = errStr("not an object")
	errNotArray  = errStr("not an array")
)

type errStr string

func (e errStr) Error() string { return string(e) }

// get returns the raw value for key k (nil when absent).
func (o *opair) get(k string) json.RawMessage {
	for _, kv := range o.kvs {
		var ks string
		if json.Unmarshal(kv[0], &ks) == nil && ks == k {
			return kv[1]
		}
	}
	return nil
}

// set replaces or appends key k with JSON value v.
func (o *opair) set(k string, v json.RawMessage) {
	for i, kv := range o.kvs {
		var ks string
		if json.Unmarshal(kv[0], &ks) == nil && ks == k {
			o.kvs[i][1] = v
			return
		}
	}
	kb, _ := json.Marshal(k)
	o.kvs = append(o.kvs, [2]json.RawMessage{kb, v})
}

func (o opair) MarshalJSON() ([]byte, error) {
	if len(o.kvs) == 0 {
		return []byte("{}"), nil
	}
	var out []byte
	out = append(out, '{')
	for i, kv := range o.kvs {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, kv[0]...)
		out = append(out, ':')
		out = append(out, kv[1]...)
	}
	out = append(out, '}')
	return out, nil
}
