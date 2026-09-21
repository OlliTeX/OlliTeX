package otc

// V2DocVersions mirrors lib/v2_doc_versions.js: a map from doc id to
// {pathname, version}, tracking document versions across file moves.
type V2DocVersions struct {
	Data map[string]any // id -> {pathname: string, version: number}
}

// NewV2DocVersions builds one (Node: `new V2DocVersions(data)`; nil → {}).
func NewV2DocVersions(data map[string]any) *V2DocVersions {
	if data == nil {
		data = map[string]any{}
	}
	return &V2DocVersions{Data: data}
}

// V2DocVersionsFromRaw mirrors V2DocVersions.fromRaw (nil raw → nil).
func V2DocVersionsFromRaw(raw map[string]any) *V2DocVersions {
	if raw == nil {
		return nil
	}
	return NewV2DocVersions(raw)
}

// ToRaw returns a copy of the data, or nil (Node: `toRaw`).
func (v *V2DocVersions) ToRaw() map[string]any {
	out := make(map[string]any, len(v.Data))
	for k, val := range v.Data {
		out[k] = val
	}
	return out
}

// Clone returns a new V2DocVersions of the same data (Node: `clone`).
func (v *V2DocVersions) Clone() *V2DocVersions {
	return V2DocVersionsFromRaw(v.ToRaw())
}

// ApplyTo merges these versions into a snapshot's (Node: `applyTo`).
func (v *V2DocVersions) ApplyTo(s *Snapshot) {
	if len(v.Data) == 0 {
		return
	}
	if s.V2DocVersions == nil {
		s.V2DocVersions = v.Clone()
	} else {
		for k, val := range v.Data {
			s.V2DocVersions.Data[k] = val
		}
	}
}

// MoveFile re-homes (or drops) the doc at pathname (Node: `moveFile`).
func (v *V2DocVersions) MoveFile(pathname, newPathname string) {
	for id, val := range v.Data {
		info, ok := val.(map[string]any)
		if !ok {
			continue
		}
		if info["pathname"] != pathname {
			continue
		}
		if newPathname == "" {
			delete(v.Data, id)
		} else {
			info["pathname"] = newPathname
		}
		break
	}
}
