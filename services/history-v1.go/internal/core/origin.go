package core

import (
	"encoding/json"
	"fmt"
)

// Origin (files origin) — ports origin/index.js + restore_*_origin.js.
// Wire (kind-specific, key presence is what dispatch cares about):
//
//	"editor":           {kind, historyClientId?}
//	"restore":          {kind, version, timestamp(ISO), historyClientId?}
//	"file-restore":     {kind, version, path, timestamp(ISO), historyClientId?}
//	"project-restore":  {kind, version, timestamp(ISO), historyClientId?}
type Origin struct {
	Kind            string
	HistoryClientId string
	Version         int
	Timestamp       string // ISO string (Node emits toISOString(); nil->1970-01-01T00:00:00.000Z)
	Path            string // "file-restore" only
}

// NewEditorOrigin — the default "editor" origin (kind only, or with client id).
func NewEditorOrigin(historyClientId string) *Origin {
	return &Origin{Kind: "editor", HistoryClientId: historyClientId}
}

// NewRestoreOrigin — {kind:'restore', version, timestamp}.
func NewRestoreOrigin(version int, timestamp, historyClientId string) *Origin {
	return &Origin{Kind: "restore", Version: version, Timestamp: timestamp, HistoryClientId: historyClientId}
}

// NewRestoreFileOrigin — {kind:'file-restore', version, path, timestamp}.
func NewRestoreFileOrigin(version int, path, timestamp, historyClientId string) *Origin {
	return &Origin{Kind: "file-restore", Version: version, Path: path, Timestamp: timestamp, HistoryClientId: historyClientId}
}

// NewRestoreProjectOrigin — {kind:'project-restore', version, timestamp}.
func NewRestoreProjectOrigin(version int, timestamp, historyClientId string) *Origin {
	return &Origin{Kind: "project-restore", Version: version, Timestamp: timestamp, HistoryClientId: historyClientId}
}

// OriginFromRaw ports Origin.fromRaw (dispatch by kind; returns nil for empty).
func OriginFromRaw(raw json.RawMessage) *Origin {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var probe struct {
		Kind            string `json:"kind"`
		Version         int    `json:"version"`
		Path            string `json:"path"`
		Timestamp       string `json:"timestamp"`
		HistoryClientId string `json:"historyClientId"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	switch probe.Kind {
	case "restore":
		return &Origin{Kind: "restore", Version: probe.Version, Timestamp: probe.Timestamp, HistoryClientId: probe.HistoryClientId}
	case "file-restore":
		return &Origin{Kind: "file-restore", Version: probe.Version, Path: probe.Path, Timestamp: probe.Timestamp, HistoryClientId: probe.HistoryClientId}
	case "project-restore":
		return &Origin{Kind: "project-restore", Version: probe.Version, Timestamp: probe.Timestamp, HistoryClientId: probe.HistoryClientId}
	default:
		return &Origin{Kind: probe.Kind, HistoryClientId: probe.HistoryClientId}
	}
}

// DropHistoryClientId (Node Origin.dropHistoryClientId) — clears the id.
func (o *Origin) DropHistoryClientId() *Origin {
	if o == nil {
		return nil
	}
	cp := *o
	cp.HistoryClientId = ""
	return &cp
}

// ToRaw ports Origin.toRaw per kind.
func (o *Origin) ToRaw() json.RawMessage {
	out := map[string]any{"kind": o.Kind}
	if o.HistoryClientId != "" {
		out["historyClientId"] = o.HistoryClientId
	}
	switch o.Kind {
	case "restore":
		out["version"] = o.Version
		out["timestamp"] = o.Timestamp
	case "file-restore":
		out["version"] = o.Version
		out["path"] = o.Path
		out["timestamp"] = o.Timestamp
	case "project-restore":
		out["version"] = o.Version
		out["timestamp"] = o.Timestamp
	}
	b, _ := json.Marshal(out)
	return b
}

// ConflictingEndVersion — ports chunk.ConflictingEndVersion (OError).
type ConflictingEndVersion struct {
	ClientEndVersion int
	LatestEndVersion int
}

func (e *ConflictingEndVersion) Error() string {
	return fmt.Sprintf("client sent updates with end_version %d but latest chunk has end_version %d", e.ClientEndVersion, e.LatestEndVersion)
}
