package historyot

import (
	"encoding/json"
	"time"
)

// Origin wire helpers (lib/origin).
//
// Vendors serialize timestamps to wire as ISO-8601 strings via toISOString().
// The Go port keeps them as time.Time (UTC, ms) and encodes/decodes with the
// same precision. The wire string is exposed verbatim wherever the Go port
// re-emits raw values (Change.toRaw emits the Origin's wire form).

// Origin — vendor `Origin` (lib/origin/index.js): a tagged origin.
type Origin struct {
	Kind            string
	HistoryClientID string
}

// RestoreOrigin — vendor `RestoreOrigin` (lib/origin/restore_origin.js).
type RestoreOrigin struct {
	Kind            string
	HistoryClientID string
	Version         int
	Timestamp       time.Time
}

// RestoreFileOrigin — vendor `RestoreFileOrigin`
// (lib/origin/restore_file_origin.js).
type RestoreFileOrigin struct {
	Kind            string
	HistoryClientID string
	Version         int
	Path            string
	Timestamp       time.Time
}

// RestoreProjectOrigin — vendor `RestoreProjectOrigin`
// (lib/origin/restore_project_origin.js).
type RestoreProjectOrigin struct {
	Kind            string
	HistoryClientID string
	Version         int
	Timestamp       time.Time
}

// OriginFromRaw — vendor `Origin.fromRaw(raw)`: dispatch on kind.
//
// Return: the concrete origin for the wire tag.
func OriginFromRaw(raw map[string]any) (any, error) {
	kind, _ := raw["kind"].(string)
	switch kind {
	case "restore":
		if _, hasVersion := raw["version"]; hasVersion {
			v, _ := raw["version"].(float64)
			ts, err := decodeWireTime(raw["timestamp"])
			if err != nil {
				return nil, err
			}
			hcid, _ := raw["historyClientId"].(string)
			return &RestoreOrigin{Kind: "restore", HistoryClientID: hcid, Version: int(v), Timestamp: ts}, nil
		}
	case "file-restore":
		if _, hasPath := raw["path"]; hasPath {
			v, _ := raw["version"].(float64)
			p, _ := raw["path"].(string)
			ts, err := decodeWireTime(raw["timestamp"])
			if err != nil {
				return nil, err
			}
			hcid, _ := raw["historyClientId"].(string)
			return &RestoreFileOrigin{Kind: "file-restore", HistoryClientID: hcid, Version: int(v), Path: p, Timestamp: ts}, nil
		}
	case "project-restore":
		if _, hasVersion := raw["version"]; hasVersion {
			v, _ := raw["version"].(float64)
			ts, err := decodeWireTime(raw["timestamp"])
			if err != nil {
				return nil, err
			}
			hcid, _ := raw["historyClientId"].(string)
			return &RestoreProjectOrigin{Kind: "project-restore", HistoryClientID: hcid, Version: int(v), Timestamp: ts}, nil
		}
	}
	hcid, _ := raw["historyClientId"].(string)
	return &Origin{Kind: kind, HistoryClientID: hcid}, nil
}

// ToWire — vendor `origin.toRaw()`.
func toWireOrigin(kind, hcid string, extra func(m map[string]any)) map[string]any {
	m := map[string]any{"kind": kind}
	if hcid != "" {
		m["historyClientId"] = hcid
	}
	if extra != nil {
		extra(m)
	}
	return m
}

func (o *Origin) ToWire() map[string]any { return toWireOrigin(o.Kind, o.HistoryClientID, nil) }
func (o *RestoreOrigin) ToWire() map[string]any {
	return toWireOrigin("restore", o.HistoryClientID, func(m map[string]any) {
		m["version"] = o.Version
		m["timestamp"] = wireTime(o.Timestamp)
	})
}
func (o *RestoreFileOrigin) ToWire() map[string]any {
	return toWireOrigin("file-restore", o.HistoryClientID, func(m map[string]any) {
		m["version"] = o.Version
		m["path"] = o.Path
		m["timestamp"] = wireTime(o.Timestamp)
	})
}
func (o *RestoreProjectOrigin) ToWire() map[string]any {
	return toWireOrigin("project-restore", o.HistoryClientID, func(m map[string]any) {
		m["version"] = o.Version
		m["timestamp"] = wireTime(o.Timestamp)
	})
}

// wireTime — vendor toISOString(): the ISO-8601 string at millisecond
// precision (always Z).
func wireTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func decodeWireTime(v any) (time.Time, error) {
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000Z", time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, nil
}

// mapString — a minimal JSON serialization for error messages, mirroring
// vendor `JSON.stringify`. Deterministic ordering is not required (only used
// in error text).
func mapString(m map[string]any) string {
	data, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(data)
}
