package otc

import "time"

// EditorOriginKind is the origin kind stamped on a change submitted over the
// editor (Node: `EDITOR_ORIGIN_KIND`).
const EditorOriginKind = "editor"

// RestoreOriginKinds (Node: the static KIND constants).
const (
	RestoreOriginKind        = "restore"
	RestoreFileOriginKind    = "file-restore"
	RestoreProjectOriginKind = "project-restore"
)

// OriginIface is the polymorphic origin (Node: `Origin` and its subclasses).
type OriginIface interface {
	GetKind() string
	GetHistoryClientId() *string
	DropHistoryClientId()
	ToRaw() map[string]any
}

// Origin is a simple tag origin (Node: `class Origin`).
type Origin struct {
	Kind            string
	HistoryClientId *string
}

// NewOrigin builds one (Node: `new Origin(kind, historyClientId)`).
func NewOrigin(kind string, historyClientId string) (*Origin, error) {
	if kind == "" {
		return nil, gop("Origin: bad kind")
	}
	if historyClientId == "" {
		// Node: assert.maybe.nonEmptyString — empty string is invalid (not just undefined).
		return nil, gop("Origin: bad historyClientId")
	}
	return &Origin{Kind: kind, HistoryClientId: &historyClientId}, nil
}

func NewOriginWithID(kind string, id *string) (*Origin, error) {
	if kind == "" {
		return nil, gop("Origin: bad kind")
	}
	return &Origin{Kind: kind, HistoryClientId: id}, nil
}

func (o *Origin) GetKind() string             { return o.Kind }
func (o *Origin) GetHistoryClientId() *string { return o.HistoryClientId }
func (o *Origin) DropHistoryClientId()        { o.HistoryClientId = nil }
func (o *Origin) ToRaw() map[string]any {
	raw := map[string]any{"kind": o.Kind}
	if o.HistoryClientId != nil {
		raw["historyClientId"] = *o.HistoryClientId
	}
	return raw
}

// OriginFromRaw mirrors `Origin.fromRaw` (nil → nil; dispatch by kind).
func OriginFromRaw(raw map[string]any) (OriginIface, error) {
	if raw == nil {
		return nil, nil
	}
	kind, _ := raw["kind"].(string)
	switch {
	case kind == RestoreOriginKind && rawHas(raw, "version"):
		return RestoreOriginFromRaw(raw)
	case kind == RestoreFileOriginKind && rawHas(raw, "path"):
		return RestoreFileOriginFromRaw(raw)
	case kind == RestoreProjectOriginKind && rawHas(raw, "version"):
		return RestoreProjectOriginFromRaw(raw)
	default:
		return NewOriginWithID(kind, strOrPtrRaw(raw["historyClientId"]))
	}
}

func strOrPtrRaw(v any) *string {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return &s
}

func toISOString(ts time.Time) string {
	return ts.UTC().Format("2006-01-02T15:04:05.000Z")
}

// --- RestoreOrigin ---------------------------------------------------------

// RestoreOrigin records restoring a previous version (Node: `RestoreOrigin`).
type RestoreOrigin struct {
	*Origin
	Version   int64
	Timestamp time.Time
}

// NewRestoreOrigin builds one.
func NewRestoreOrigin(version int64, ts time.Time, historyClientId string) (*RestoreOrigin, error) {
	base, err := NewOrigin(RestoreOriginKind, historyClientId)
	if err != nil {
		return nil, err
	}
	return &RestoreOrigin{Origin: base, Version: version, Timestamp: ts}, nil
}

func RestoreOriginFromRaw(raw map[string]any) (*RestoreOrigin, error) {
	version, _ := raw["version"].(int64)
	ts, err := parseRawTime(raw["timestamp"])
	if err != nil {
		return nil, err
	}
	base, err := NewOriginWithID(RestoreOriginKind, strOrPtrRaw(raw["historyClientId"]))
	if err != nil {
		return nil, err
	}
	return &RestoreOrigin{Origin: base, Version: version, Timestamp: ts}, nil
}

func (o *RestoreOrigin) ToRaw() map[string]any {
	raw := o.Origin.ToRaw()
	raw["version"] = o.Version
	raw["timestamp"] = toISOString(o.Timestamp)
	return raw
}

func (o *RestoreOrigin) GetVersion() int64       { return o.Version }
func (o *RestoreOrigin) GetTimestamp() time.Time { return o.Timestamp }

// --- RestoreFileOrigin -----------------------------------------------------

// RestoreFileOrigin records restoring a single file (Node: `RestoreFileOrigin`).
type RestoreFileOrigin struct {
	*Origin
	Version   int64
	Path      string
	Timestamp time.Time
}

// NewRestoreFileOrigin builds one.
func NewRestoreFileOrigin(version int64, path string, ts time.Time, historyClientId string) (*RestoreFileOrigin, error) {
	if path == "" {
		return nil, gop("RestoreFileOrigin: bad path")
	}
	base, err := NewOrigin(RestoreFileOriginKind, historyClientId)
	if err != nil {
		return nil, err
	}
	return &RestoreFileOrigin{Origin: base, Version: version, Path: path, Timestamp: ts}, nil
}

func RestoreFileOriginFromRaw(raw map[string]any) (*RestoreFileOrigin, error) {
	version, _ := raw["version"].(int64)
	path, _ := raw["path"].(string)
	if path == "" {
		return nil, gop("RestoreFileOrigin: bad path")
	}
	ts, err := parseRawTime(raw["timestamp"])
	if err != nil {
		return nil, err
	}
	base, err := NewOriginWithID(RestoreFileOriginKind, strOrPtrRaw(raw["historyClientId"]))
	if err != nil {
		return nil, err
	}
	return &RestoreFileOrigin{Origin: base, Version: version, Path: path, Timestamp: ts}, nil
}

func (o *RestoreFileOrigin) ToRaw() map[string]any {
	raw := o.Origin.ToRaw()
	raw["version"] = o.Version
	raw["path"] = o.Path
	raw["timestamp"] = toISOString(o.Timestamp)
	return raw
}

func (o *RestoreFileOrigin) GetVersion() int64       { return o.Version }
func (o *RestoreFileOrigin) GetPath() string         { return o.Path }
func (o *RestoreFileOrigin) GetTimestamp() time.Time { return o.Timestamp }

// --- RestoreProjectOrigin --------------------------------------------------

// RestoreProjectOrigin records restoring a project (Node: `RestoreProjectOrigin`).
type RestoreProjectOrigin struct {
	*Origin
	Version   int64
	Timestamp time.Time
}

// NewRestoreProjectOrigin builds one.
func NewRestoreProjectOrigin(version int64, ts time.Time, historyClientId string) (*RestoreProjectOrigin, error) {
	base, err := NewOrigin(RestoreProjectOriginKind, historyClientId)
	if err != nil {
		return nil, err
	}
	return &RestoreProjectOrigin{Origin: base, Version: version, Timestamp: ts}, nil
}

func RestoreProjectOriginFromRaw(raw map[string]any) (*RestoreProjectOrigin, error) {
	version, _ := raw["version"].(int64)
	ts, err := parseRawTime(raw["timestamp"])
	if err != nil {
		return nil, err
	}
	base, err := NewOriginWithID(RestoreProjectOriginKind, strOrPtrRaw(raw["historyClientId"]))
	if err != nil {
		return nil, err
	}
	return &RestoreProjectOrigin{Origin: base, Version: version, Timestamp: ts}, nil
}

func (o *RestoreProjectOrigin) ToRaw() map[string]any {
	raw := o.Origin.ToRaw()
	raw["version"] = o.Version
	raw["timestamp"] = toISOString(o.Timestamp)
	return raw
}

func (o *RestoreProjectOrigin) GetVersion() int64       { return o.Version }
func (o *RestoreProjectOrigin) GetTimestamp() time.Time { return o.Timestamp }

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func parseRawTime(v any) (time.Time, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}, gop("Origin: bad timestamp")
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000Z", time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts.UTC(), nil
		}
	}
	return time.Time{}, gop("Origin: bad timestamp " + s)
}
