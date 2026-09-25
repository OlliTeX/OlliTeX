package core

import (
	"encoding/json"
	"errors"
	"time"
)

// Snapshot — ports snapshot.js. Wire (Node Snapshot.toRaw, object):
//
//	{
//	  files:             <FileMap.toRaw>,  // a JSON object keyed by pathname
//	  projectVersion?:   <string>,          // when present
//	  v2DocVersions?:    <V2DocVersions.toRaw> (null when empty),  // optional
//	  timestamp?:        <ISO string>       // optional
//	}
type Snapshot struct {
	Files          *FileMap
	ProjectVersion string
	V2DocVersions  *V2DocVersions
	Timestamp      time.Time
}

// NewSnapshot (Node Snapshot constructor: (fileMap, projectVersion, v2DocVersions, timestamp)).
func NewSnapshot(files *FileMap, projectVersion string, dv *V2DocVersions, timestamp time.Time) *Snapshot {
	return &Snapshot{Files: files, ProjectVersion: projectVersion, V2DocVersions: dv, Timestamp: timestamp}
}

// SnapshotFromRaw (Node Snapshot.fromRaw). raw falsy -> nil.
//
//	assert.object(raw.files, 'bad raw.files')
//	assert.string(raw.projectVersion)
//	v2DocVersions: raw.v2DocVersions ? V2DocVersions.fromRaw : null
//	timestamp: raw.timestamp ? iso : undefined
func SnapshotFromRaw(raw json.RawMessage) *Snapshot {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	s, err := snapshotMustFromRaw(raw)
	if err != nil {
		panic(err)
	}
	return s
}

// SnapshotMustFromRaw (Node Snapshot.fromRaw, exported for the API layer).
// raw falsy -> (nil, nil) (Node `if (!raw) return null`). Invalid raw ->
// error (Node assert failures surface as errors the API renders as 422).
func SnapshotMustFromRaw(raw json.RawMessage) (*Snapshot, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	return snapshotMustFromRaw(raw)
}

func snapshotMustFromRaw(raw json.RawMessage) (*Snapshot, error) {
	var probe struct {
		Files          json.RawMessage `json:"files"`
		ProjectVersion *string         `json:"projectVersion"`
		V2DocVersions  json.RawMessage `json:"v2DocVersions"`
		Timestamp      *string         `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, &BadRawError{Msg: "bad snapshot raw: " + err.Error()}
	}
	fm, err := FileMapFromRaw(probe.Files)
	if err != nil {
		return nil, err
	}
	s := &Snapshot{Files: fm}
	if probe.ProjectVersion != nil {
		s.ProjectVersion = *probe.ProjectVersion
	}
	if len(probe.V2DocVersions) > 0 && string(probe.V2DocVersions) != "null" {
		s.V2DocVersions = V2DocVersionsFromRaw(probe.V2DocVersions)
	}
	if probe.Timestamp != nil && *probe.Timestamp != "" {
		t, err := time.Parse(time.RFC3339, *probe.Timestamp)
		if err != nil {
			return nil, &BadRawError{Msg: "bad snapshot timestamp: " + err.Error()}
		}
		s.Timestamp = t
	}
	return s, nil
}

// ToRaw (Node Snapshot.toRaw).
func (s *Snapshot) ToRaw() json.RawMessage {
	out := map[string]any{
		"files": json.RawMessage(s.Files.ToRaw()),
	}
	if s.ProjectVersion != "" {
		out["projectVersion"] = s.ProjectVersion
	}
	if s.V2DocVersions != nil {
		out["v2DocVersions"] = json.RawMessage(s.V2DocVersions.ToRaw())
	}
	if !s.Timestamp.IsZero() {
		out["timestamp"] = s.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	b, _ := json.Marshal(out)
	return b
}

// Clone (Node Snapshot.clone: (new Snapshot).FileMap/... via toRaw/fromRaw +
// File.clone). Go: deep copy with fresh Files map.
func (s *Snapshot) Clone() *Snapshot {
	cloneFiles := &FileMap{Files: make(map[string]*File, len(s.Files.Files))}
	for k, f := range s.Files.Files {
		cloneFiles.Files[k] = f.Clone()
	}
	cloneDV := s.V2DocVersions
	if s.V2DocVersions != nil {
		cloneDV = s.V2DocVersions.Clone()
	}
	return &Snapshot{
		Files:          cloneFiles,
		ProjectVersion: s.ProjectVersion,
		V2DocVersions:  cloneDV,
		Timestamp:      s.Timestamp,
	}
}

// GetFile (Node Snapshot.getFile).
func (s *Snapshot) GetFile(pathname string) *File {
	if s.Files == nil {
		return nil
	}
	return s.Files.GetFile(pathname)
}

// GetFilePathnames (Node Snapshot.getFilePathnames).
func (s *Snapshot) GetFilePathnames() []string {
	if s.Files == nil {
		return nil
	}
	return s.Files.GetPathnames()
}

// CountFiles (Node Snapshot.countFiles).
func (s *Snapshot) CountFiles() int {
	if s.Files == nil {
		return 0
	}
	return s.Files.CountFiles()
}

// AddFile (Node Snapshot.addFile: checkFile + fileMap.addFile).
func (s *Snapshot) AddFile(pathname string, file *File) error {
	return s.Files.AddFile(pathname, file)
}

// MoveFile (Node Snapshot.moveFile: checkFile + v2DocVersions.moveFile +
// fileMap.moveFile).
func (s *Snapshot) MoveFile(pathname, newPathname string) error {
	if s.Files == nil {
		// Files absent == nothing to move; not a recovery case.
		return nil
	}
	if s.V2DocVersions != nil {
		s.V2DocVersions.MoveFile(pathname, newPathname)
	}
	return s.Files.MoveFile(pathname, newPathname)
}

// EditFile (Node Snapshot.editFile: throws EditMissingFileError when file
// missing).
func (s *Snapshot) EditFile(pathname string, editOp *EditOp) error {
	f := s.Files.GetFile(pathname)
	if f == nil {
		return &EditMissingFileError{Pathname: pathname}
	}
	return editOp.ApplyFile(f)
}

// ApplyAll (Node Snapshot.applyAll: ops apply in order; strict mode).
func (s *Snapshot) ApplyAll(changes []*Change) error {
	for _, c := range changes {
		if err := c.ApplyTo(s); err != nil {
			return err
		}
	}
	return nil
}

// LoadFiles (Node Snapshot.loadFiles: per-file load kind). For persist: kind
// "lazy" (string kind stays, hash/ranges kind -> lazy (not eager)).
func (s *Snapshot) LoadFiles(kind string, bs BlobStoreI) error {
	return s.Files.LoadFiles(kind, bs)
}

// FindBlobHashes (Node Snapshot.findBlobHashes): collect every (hash,
// rangesHash) referenced by files.
func (s *Snapshot) FindBlobHashes(hashSet *map[string]struct{}) {
	if s.Files != nil {
		s.Files.FindBlobHashes(hashSet)
	}
}

// SetProjectVersion (Node Snapshot.setProjectVersion: only called when
// present; empty string clears).
func (s *Snapshot) SetProjectVersion(version string) { s.ProjectVersion = version }

// SetTimestamp (Node Snapshot.setTimestamp).
func (s *Snapshot) SetTimestamp(t time.Time) { s.Timestamp = t }

// Store (Node Snapshot.store): store each file into the blob store and
// return {files, projectVersion?, v2DocVersions?, timestamp?}. The optional
// keys are omitted when absent (empty string / nil / zero time), mirroring
// Node where `undefined` values are dropped from the serialized object.
func (s *Snapshot) Store(bs BlobStoreI) (json.RawMessage, error) {
	filesOut := map[string]json.RawMessage{}
	for p, f := range s.Files.Files {
		w, err := f.Store(bs)
		if err != nil {
			return nil, err
		}
		filesOut[p] = w
	}
	filesB, err := json.Marshal(filesOut)
	if err != nil {
		return nil, &BadRawError{Msg: "snapshot.store: bad files: " + err.Error()}
	}
	snapRaw := map[string]json.RawMessage{"files": filesB}
	if s.ProjectVersion != "" {
		v, _ := json.Marshal(s.ProjectVersion)
		snapRaw["projectVersion"] = v
	}
	if s.V2DocVersions != nil {
		snapRaw["v2DocVersions"] = json.RawMessage(s.V2DocVersions.ToRaw())
	}
	if !s.Timestamp.IsZero() {
		vb, _ := json.Marshal(s.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z"))
		snapRaw["timestamp"] = vb
	}
	return json.Marshal(snapRaw)
}

// UpdateV2DocVersions (Node Snapshot.updateV2DocVersions).
func (s *Snapshot) UpdateV2DocVersions(other *V2DocVersions) {
	if other == nil {
		return
	}
	if s.V2DocVersions == nil {
		s.V2DocVersions = other.Clone()
		return
	}
	s.V2DocVersions.Merge(rawOf(other))
}

func rawOf(other *V2DocVersions) json.RawMessage {
	return other.ToRaw()
}

// FindBlobHashes is on *Snapshot.
// EditFile used to be File.edit (see EditOp); kept for discoverability.

var ErrNotFound = errors.New("file does not exist")
