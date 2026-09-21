package otc

import (
	"context"
	"time"
)

// EditMissingFileError mirrors Snapshot.EditMissingFileError.
type EditMissingFileError struct{ Msg string }

func (e *EditMissingFileError) Error() string { return e.Msg }

// fileLoadConcurrency mirrors FILE_LOAD_CONCURRENCY (not used for async in Go,
// kept for parity of the Store/LoadFiles surface).
const fileLoadConcurrency = 50

var _ = fileLoadConcurrency

// Snapshot mirrors lib/snapshot.js: the state of a Project at a version.
type Snapshot struct {
	FileMap        *FileMap
	ProjectVersion *string
	V2DocVersions  *V2DocVersions
	Timestamp      *time.Time
}

// NewSnapshot mirrors the Snapshot constructor (nil fileMap → empty).
func NewSnapshot(fileMap *FileMap, projectVersion *string, v2 *V2DocVersions, ts *time.Time) *Snapshot {
	if fileMap == nil {
		fm, _ := NewFileMap(map[string]*File{})
		fileMap = fm
	}
	if ts == nil {
		var zero time.Time
		ts = &zero
	}
	return &Snapshot{
		FileMap:        fileMap,
		ProjectVersion: projectVersion,
		V2DocVersions:  v2,
		Timestamp:      ts,
	}
}

// SnapshotFromRaw mirrors Snapshot.fromRaw.
func SnapshotFromRaw(raw map[string]any) (*Snapshot, error) {
	files, _ := raw["files"].(map[string]map[string]any)
	if !rawHas(raw, "files") {
		return nil, gop("bad raw.files")
	}
	fm, err := FileMapFromRaw(files)
	if err != nil {
		return nil, err
	}
	var pv *string
	if s, ok := raw["projectVersion"].(string); ok && s != "" {
		pv = &s
	}
	v2 := V2DocVersionsFromRaw(nil)
	if m, ok := raw["v2DocVersions"].(map[string]any); ok && m != nil {
		v2 = V2DocVersionsFromRaw(m)
	}
	var ts *time.Time
	if s, ok := raw["timestamp"].(string); ok && s != "" {
		t, err := parseRawTime(s)
		if err != nil {
			return nil, err
		}
		ts = &t
	}
	return NewSnapshot(fm, pv, v2, ts), nil
}

// ToRaw mirrors Snapshot.toRaw.
func (s *Snapshot) ToRaw() map[string]any {
	raw := map[string]any{"files": s.FileMap.ToRaw()}
	if s.ProjectVersion != nil {
		raw["projectVersion"] = *s.ProjectVersion
	}
	if s.V2DocVersions != nil {
		raw["v2DocVersions"] = s.V2DocVersions.ToRaw()
	}
	if s.Timestamp != nil && !s.Timestamp.IsZero() {
		raw["timestamp"] = toISOString(*s.Timestamp)
	}
	return raw
}

func (s *Snapshot) GetProjectVersion() *string { return s.ProjectVersion }
func (s *Snapshot) SetProjectVersion(s2 *string) {
	if s2 != nil && *s2 != "" {
		if !projectVersionRx.MatchString(*s2) {
			panic(newTypeError("Snapshot: bad projectVersion " + *s2))
		}
	}
	s.ProjectVersion = s2
}
func (s *Snapshot) GetV2DocVersions() *V2DocVersions     { return s.V2DocVersions }
func (s *Snapshot) SetV2DocVersions(v *V2DocVersions)    { s.V2DocVersions = v }
func (s *Snapshot) UpdateV2DocVersions(v *V2DocVersions) { v.ApplyTo(s) }
func (s *Snapshot) GetTimestamp() *time.Time             { return s.Timestamp }
func (s *Snapshot) SetTimestamp(ts *time.Time)           { s.Timestamp = ts }
func (s *Snapshot) GetFileMap() *FileMap                 { return s.FileMap }

func (s *Snapshot) GetFilePathnames() []string    { return s.FileMap.GetPathnames() }
func (s *Snapshot) GetFile(pathname string) *File { return s.FileMap.GetFile(pathname) }

// GetPathnameWithDocFlag returns the (sorted-first) pathname carrying a
// document flag AND whose metadata is doc-only (Node: `getPathnameWithDocFlag`).
func (s *Snapshot) GetPathnameWithDocFlag(key string) *string {
	pathnames := s.GetFilePathnames()
	for _, p := range pathnames { // GetPathnames is already sorted
		file := s.GetFile(p)
		if file == nil {
			continue
		}
		meta := file.GetMetadata()
		if HasDocumentMetadataFlag(meta, key) && IsDocumentMetadata(meta) {
			rp := p
			return &rp
		}
	}
	return nil
}

func (s *Snapshot) AddFile(pathname string, file *File) error {
	return s.FileMap.AddFile(pathname, file)
}
func (s *Snapshot) MoveFile(pathname, newPathname string) error {
	if err := s.FileMap.MoveFile(pathname, newPathname); err != nil {
		return err
	}
	if s.V2DocVersions != nil {
		s.V2DocVersions.MoveFile(pathname, newPathname)
	}
	return nil
}
func (s *Snapshot) CountFiles() int { return s.FileMap.CountFiles() }

// EditFile edits an editable file, throwing EditMissingFileError if absent
// (Node: `editFile`).
func (s *Snapshot) EditFile(pathname string, op EditOperation) error {
	file := s.FileMap.GetFile(pathname)
	if file == nil {
		return &EditMissingFileError{Msg: "can't find file for editing: " + pathname}
	}
	return file.Edit(op)
}

// ApplyAll applies changes in sequence (Node: `applyAll`).
func (s *Snapshot) ApplyAll(changes []*Change, strict bool) error {
	for _, c := range changes {
		if err := c.ApplyTo(s, strict); err != nil {
			return err
		}
	}
	return nil
}

// FindBlobHashes collects blob hashes referenced by the snapshot's files
// (Node: `findBlobHashes`).
func (s *Snapshot) FindBlobHashes(hashes map[string]bool) {
	for _, p := range s.GetFilePathnames() {
		file := s.GetFile(p)
		if file == nil {
			continue
		}
		if h := file.GetHash(); h != nil {
			hashes[*h] = true
		}
		if r := file.GetRangesHash(); r != nil {
			hashes[*r] = true
		}
	}
}

// LoadFiles loads every file of the given kind (Node: `loadFiles`), returning
// the pathname→file map (Go: sequential, no concurrency).
func (s *Snapshot) LoadFiles(ctx context.Context, kind string, bs BlobStore) map[string]*File {
	out := make(map[string]*File, s.FileMap.CountFiles())
	for _, p := range s.GetFilePathnames() {
		f := s.GetFile(p)
		if f == nil {
			continue
		}
		if lf, err := f.Load(ctx, kind, bs); err == nil {
			out[p] = lf
		} else {
			out[p] = f
		}
	}
	return out
}

// Store stores each file and returns the raw snapshot (Node: `store`).
func (s *Snapshot) Store(ctx context.Context, bs BlobStore) (map[string]any, error) {
	rawFiles := make(map[string]map[string]any, s.FileMap.CountFiles())
	for _, p := range s.GetFilePathnames() {
		f := s.GetFile(p)
		if f == nil {
			continue
		}
		raw, err := f.Store(ctx, bs)
		if err != nil {
			return nil, err
		}
		rawFiles[p] = raw
	}
	raw := map[string]any{"files": rawFiles}
	if s.Timestamp != nil {
		raw["timestamp"] = s.Timestamp
	}
	return raw, nil
}

// Clone returns a deep clone (Node: `clone`).
func (s *Snapshot) Clone() (*Snapshot, error) {
	return SnapshotFromRaw(s.ToRaw())
}
