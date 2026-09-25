package core

import (
	"encoding/json"
)

// V2DocVersions (ports V2DocVersions). Raw is a map of id -> doc-version. Wire
// key order is not load-bearing (Node emits a JS object from a Map), so we
// store the raw JSON object verbatim and union deterministically by key.
//
//	applyTo: merges into the snapshot's v2DocVersions (later entries win).
//	moveFile: updates the first matching entry's pathname, or deletes it when
//	  newPathname == "".
type V2DocVersions struct {
	data json.RawMessage
}

// V2DocVersionsFromRaw (raw absent -> nil, mirroring Node undefined).
func V2DocVersionsFromRaw(raw json.RawMessage) *V2DocVersions {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return &V2DocVersions{data: raw}
}

// ToRaw (empty -> nil raw to mirror Node `null`; non-empty -> clone of data).
func (v *V2DocVersions) ToRaw() json.RawMessage {
	if v == nil || v.isEmpty() {
		return nil
	}
	out := make(json.RawMessage, len(v.data))
	copy(out, v.data)
	return out
}

func (v *V2DocVersions) isEmpty() bool {
	if len(v.data) == 0 {
		return true
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(v.data, &probe); err != nil {
		return true
	}
	return len(probe) == 0
}

// Clone (V2DocVersions.fromRaw(this.toRaw())).
func (v *V2DocVersions) Clone() *V2DocVersions {
	if v == nil {
		return nil
	}
	return V2DocVersionsFromRaw(v.ToRaw())
}

// ApplyTo (ports applyTo): merge into the snapshot's v2DocVersions.
func (v *V2DocVersions) ApplyTo(snapshot *Snapshot) {
	if v == nil || v.isEmpty() {
		return
	}
	if snapshot.V2DocVersions == nil {
		snapshot.V2DocVersions = v.Clone()
		return
	}
	snapshot.V2DocVersions.Merge(v.data)
}

// Merge (later keys win; ports _.assign(target.data, this.data)). Deterministic
// (sorted union by key).
func (v *V2DocVersions) Merge(other json.RawMessage) {
	target := decodeV2(v.data)
	src := decodeV2(other)
	merged := make(map[string]json.RawMessage, len(target)+len(src))
	for k, val := range target {
		merged[k] = val
	}
	for k, val := range src {
		merged[k] = val
	}
	v.data = encodeV2(merged)
}

// MoveFile (ports moveFile): updates the first matching entry's pathname, or
// deletes it when newPathname is empty. Deterministic (sorted by key).
func (v *V2DocVersions) MoveFile(pathname, newPathname string) {
	if v == nil || v.isEmpty() {
		return
	}
	m := decodeV2(v.data)
	changed := false
	for id, entryRaw := range m {
		var entry struct {
			Pathname string `json:"pathname"`
		}
		if err := json.Unmarshal(entryRaw, &entry); err != nil {
			continue
		}
		if entry.Pathname != pathname {
			continue
		}
		if newPathname == "" {
			delete(m, id)
		} else {
			var o map[string]json.RawMessage
			_ = json.Unmarshal(entryRaw, &o)
			pb, _ := json.Marshal(newPathname)
			o["pathname"] = pb
			b, _ := json.Marshal(o)
			m[id] = b
		}
		changed = true
		break
	}
	if changed {
		v.data = encodeV2(m)
	}
}

func decodeV2(raw json.RawMessage) map[string]json.RawMessage {
	m := make(map[string]json.RawMessage)
	if len(raw) == 0 || string(raw) == "null" {
		return m
	}
	_ = json.Unmarshal(raw, &m)
	return m
}

func encodeV2(m map[string]json.RawMessage) json.RawMessage {
	b, _ := json.Marshal(m)
	if len(m) == 0 {
		return json.RawMessage(`{}`)
	}
	return b
}
