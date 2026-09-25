package core

import (
	"encoding/json"
	"fmt"
	"sort"
)

// FileMap errors (ports FileMap). recoverable in iterativelyApplyTo is
// FileNotFoundError (the recoverable set is Snapshot.EditMissingFileError and
// FileMap.FileNotFoundError).
type PathnameError struct {
	Msg      string
	Pathname string
}

func (e *PathnameError) Error() string { return e.Msg }

// NonUniquePathnameError — Node FileMap.NonUniquePathnameError.
func NonUniquePathnameError(pathnames []string) *PathnameError {
	b, _ := json.Marshal(pathnames)
	return &PathnameError{Msg: fmt.Sprintf("pathnames are not unique: %s", string(b))}
}

// FileMap (ports FileMap). files: pathname -> *File. Wire:
// toRaw() -> {pathname: <file toRaw>} (a JSON object keyed by pathname).
type FileMap struct {
	Files map[string]*File
}

// FileMapFromRaw (Node FileMap.fromRaw: bare-object map of File.fromRaw).
func FileMapFromRaw(raw json.RawMessage) (*FileMap, error) {
	var entries map[string]json.RawMessage
	if len(raw) == 0 || string(raw) == "null" {
		entries = map[string]json.RawMessage{}
	} else if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, &BadRawError{Msg: "bad raw.files: " + err.Error()}
	}
	fm := &FileMap{Files: make(map[string]*File, len(entries))}
	for pathname, fileRaw := range entries {
		f, err := FileFromRaw(fileRaw)
		if err != nil {
			return nil, err
		}
		fm.Files[pathname] = f
	}
	// Node FileMap constructor: checkPathnamesAreUnique + checkPathnamesDoNotConflict.
	if !pathnamesAreUnique(fm.Files) {
		k := make([]string, 0, len(fm.Files))
		for p := range fm.Files {
			k = append(k, p)
		}
		return nil, NonUniquePathnameError(k)
	}
	if cp := checkPathnamesDoNotConflict(fm.Files); cp != nil {
		return nil, cp
	}
	return fm, nil
}

// NewFileMap — empty file map (Node `new FileMap({})`).
func NewFileMap() *FileMap { return &FileMap{Files: map[string]*File{}} }

// ToRaw (Node FileMap.toRaw: _.mapValues(this.files, fileToRaw)).
func (m *FileMap) ToRaw() json.RawMessage {
	m2 := make(map[string]json.RawMessage, len(m.Files))
	for k, f := range m.Files {
		m2[k] = f.ToRaw()
	}
	b, _ := json.Marshal(m2)
	return b
}

// CountFiles (Node FileMap.countFiles).
func (m *FileMap) CountFiles() int { return len(m.Files) }

// GetFile (Node FileMap.getFile).
func (m *FileMap) GetFile(pathname string) *File { return m.Files[pathname] }

// GetPathnames (Node FileMap.getPathnames).
func (m *FileMap) GetPathnames() []string {
	out := make([]string, 0, len(m.Files))
	for k := range m.Files {
		out = append(out, k)
	}
	return out
}

// AddFile (Node FileMap.addFile).
func (m *FileMap) AddFile(pathname string, file *File) error {
	if len(pathname) == 0 {
		return &PathnameError{Msg: "bad pathname", Pathname: truncatePathnameForError(pathname)}
	}
	if ok, _ := IsCleanDebug(pathname); !ok {
		return &PathnameError{Msg: "invalid pathname", Pathname: truncatePathnameForError(pathname)}
	}
	if m.WouldConflict(pathname, "") {
		return &PathnameError{Msg: "pathname conflicts with another file", Pathname: truncatePathnameForError(pathname)}
	}
	delete(m.Files, pathname) // Node addFile: overwrite existing.
	m.Files[pathname] = file
	return nil
}

// RemoveFile (Node FileMap.removeFile).
func (m *FileMap) RemoveFile(pathname string) error {
	if ok, _ := IsCleanDebug(pathname); !ok {
		return &PathnameError{Msg: "invalid pathname", Pathname: truncatePathnameForError(pathname)}
	}
	if _, ok := m.Files[pathname]; !ok {
		return &FileNotFoundError{Pathname: pathname}
	}
	delete(m.Files, pathname)
	return nil
}

// MoveFile (Node FileMap.moveFile). newPathname == "" removes.
func (m *FileMap) MoveFile(pathname, newPathname string) error {
	if pathname == newPathname {
		return nil
	}
	if newPathname == "" {
		return m.RemoveFile(pathname)
	}
	if ok, _ := IsCleanDebug(pathname); !ok {
		return &PathnameError{Msg: "invalid pathname", Pathname: truncatePathnameForError(pathname)}
	}
	if ok, _ := IsCleanDebug(newPathname); !ok {
		return &PathnameError{Msg: "invalid pathname", Pathname: truncatePathnameForError(newPathname)}
	}
	if m.WouldConflict(newPathname, pathname) {
		return &PathnameError{Msg: "pathname conflicts with another file", Pathname: truncatePathnameForError(newPathname)}
	}
	if _, ok := m.Files[pathname]; !ok {
		return &FileNotFoundError{Pathname: pathname}
	}
	file := m.Files[pathname]
	delete(m.Files, pathname)
	m.Files[newPathname] = file
	return nil
}

// WouldConflict (Node FileMap.wouldConflict).
func (m *FileMap) WouldConflict(pathname, ignoredPathname string) bool {
	dirname := pathname + "/"
	for _, existing := range m.GetPathnames() {
		if stringsHasPrefix(existing, dirname) && existing != ignoredPathname {
			return true
		}
		if stringsHasPrefix(pathname, existing) && len(pathname) > len(existing) &&
			pathname[len(existing)] == '/' && existing != ignoredPathname {
			return true
		}
	}
	return false
}

func stringsHasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func pathnamesEqual(p, q string) bool { return p == q }

func pathnamesAreUnique(files map[string]*File) bool {
	seen := map[string]struct{}{}
	for k := range files {
		if _, ok := seen[k]; ok {
			return false
		}
		seen[k] = struct{}{}
	}
	return true
}

// checkPathnamesDoNotConflict (Node checkPathnamesDoNotConflict).
func checkPathnamesDoNotConflict(files map[string]*File) *PathnameError {
	pathnames := make([]string, 0, len(files))
	for k := range files {
		pathnames = append(pathnames, k)
	}
	dirnames := make([]string, 0, len(pathnames))
	for _, p := range pathnames {
		dirnames = append(dirnames, p+"/")
	}
	sort.Strings(dirnames)
	for i := 0; i < len(dirnames)-1; i++ {
		if stringsHasPrefix(dirnames[i+1], dirnames[i]) {
			conflictPathname := dirnames[i+1][:len(dirnames[i+1])-1]
			if !pathnamesEqual(conflictPathname, "") {
				return &PathnameError{Msg: "pathname conflicts with another file", Pathname: conflictPathname}
			}
		}
	}
	return nil
}

func truncatePathnameForError(pathname string) string {
	if len(pathname) > 10 {
		return pathname[:5] + "..." + pathname[len(pathname)-5:]
	}
	return pathname
}

// FindBlobHashes (Node FileMap.findBlobHashes via snapshot).
// RemoveFile — port of Node FileMap.removeFile.

// LoadFiles (Node Snapshot.loadFiles → mapAsync load): per-file file.Load(kind, blobStore),
// the resulting files replace the originals. Iterating in sorted order
// (Node iterates FileMap's insertion order; Go's map is in random order, sorting makes it deterministic).
// Errors propagate (Node's mapAsync throws on the first error).
func (m *FileMap) LoadFiles(kind string, bs BlobStoreI) error {
	if m.Files == nil {
		m.Files = make(map[string]*File)
	}
	names := make([]string, 0, len(m.Files))
	for p := range m.Files {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		lf, err := m.Files[p].Load(kind, bs)
		if err != nil {
			return err
		}
		m.Files[p] = lf
	}
	return nil
}

func (m *FileMap) FindBlobHashes(blobHashes *map[string]struct{}) {
	if blobHashes == nil {
		return
	}
	for _, f := range m.Files {
		if h := f.GetHash(); h != "" {
			(*blobHashes)[h] = struct{}{}
		}
		if rh := f.GetRangesHash(); rh != "" {
			(*blobHashes)[rh] = struct{}{}
		}
	}
}
