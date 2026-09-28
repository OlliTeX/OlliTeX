package snapshotmanager

import "context"

// ToRaw — vendor editor-core Snapshot.toRaw (the shape the HTTP
// `getLatestSnapshot` handler sends):
//
//	{ files: {path: rawFile}, projectVersion?, v2DocVersions?, timestamp? }
//
// with File.toRaw = data.toRaw() + metadata (only when non-empty — the
// storeRawMetadata `_.isEmpty` gate). The C5 File models the two raw data
// shapes: text → {content}, hash → {hash, byteLength?, rangesHash?}; the
// optional comments/trackedChanges keys are included only when present.
func (s *Snapshot) ToRaw() map[string]any {
	files := map[string]any{}
	for _, f := range s.orderedFiles() {
		files[f.Pathname] = f.toRaw()
	}
	raw := map[string]any{"files": files}
	if s.ProjectVersion != "" {
		raw["projectVersion"] = s.ProjectVersion
	}
	if s.V2DocVersions != nil {
		raw["v2DocVersions"] = s.V2DocVersions
	}
	if s.Timestamp != "" {
		raw["timestamp"] = s.Timestamp
	}
	return raw
}

func (f *File) toRaw() map[string]any {
	raw := map[string]any{}
	if f.Hash != "" {
		raw["hash"] = f.Hash
	}
	if f.ByteLength != 0 {
		raw["byteLength"] = f.ByteLength
	}
	if f.RangesHash != "" {
		raw["rangesHash"] = f.RangesHash
	}
	if f.Content != nil {
		raw["content"] = *f.Content
	}
	if len(f.Comments) > 0 {
		raw["comments"] = f.Comments
	}
	if len(f.TrackedChanges) > 0 {
		raw["trackedChanges"] = f.TrackedChanges
	}
	if len(f.Metadata) > 0 {
		clone := map[string]any{}
		for k, v := range f.Metadata {
			clone[k] = v
		}
		raw["metadata"] = clone
	}
	return raw
}

// GetLatestSnapshotFull — the vendor getLatestSnapshot shape
// ({snapshot, version}); the earlier GetLatestSnapshot kept the (int,error)
// contract used by existing tests.
func (d *Deps) GetLatestSnapshotFull(ctx context.Context, projectID, historyID string) (*Snapshot, int, error) {
	data, err := d.GetMostRecentChunk(ctx, projectID, historyID)
	if err != nil {
		return nil, 0, err
	}
	chunk := dataChunk(data)
	if data == nil || chunk == nil {
		return nil, 0, pherrBadRequest("undefined chunk")
	}
	snap, _, endVersion := d.buildSnapshot(chunk, -1)
	return snap, endVersion, nil
}
