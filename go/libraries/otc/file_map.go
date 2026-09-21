package otc

import "sort"

// --- pathname error family (Node: lib/file_map.js, each extends OError) ----

// FileMapPathnameError is the Node `PathnameError` base.
type FileMapPathnameError struct{ Msg string }

func (e *FileMapPathnameError) Error() string { return e.Msg }

// NonUniquePathnameError mirrors NonUniquePathnameError.
type NonUniquePathnameError struct{ Pathnames []string }

func (e *NonUniquePathnameError) Error() string { return "pathnames are not unique" }

// BadPathnameError mirrors BadPathnameError (pathname truncated for display).
type BadPathnameError struct {
	Pathname string
	Reason   string
}

func (e *BadPathnameError) Error() string { return "invalid pathname" }

func newBadPathnameError(pathname, reason string) *BadPathnameError {
	p := pathname
	if len(p) > 10 {
		p = p[:5] + "..." + p[len(p)-5:]
	}
	return &BadPathnameError{Pathname: p, Reason: reason}
}

// PathnameConflictError mirrors PathnameConflictError.
type PathnameConflictError struct{ Pathname string }

func (e *PathnameConflictError) Error() string { return "pathname conflicts with another file" }

// FileNotFoundError mirrors FileNotFoundError.
type FileNotFoundError struct{ Pathname string }

func (e *FileNotFoundError) Error() string { return "file does not exist" }

// --- FileMap ----------------------------------------------------------------

// FileMap mirrors lib/file_map.js: a set of File with case-sensitive,
// unique, non-conflicting (file/ dir) pathnames.
type FileMap struct {
	files map[string]*File
}

// NewFileMap builds a map from raw files (Node constructor), enforcing
// uniqueness and no type conflicts.
func NewFileMap(files map[string]*File) (*FileMap, error) {
	fm := &FileMap{files: make(map[string]*File, len(files))}
	for k, v := range files {
		fm.files[k] = v
	}
	if err := checkPathnamesAreUnique(fm.files); err != nil {
		return nil, err
	}
	if err := checkPathnamesDoNotConflict(fm); err != nil {
		return nil, err
	}
	return fm, nil
}

// FileMapFromRaw mirrors FileMap.fromRaw.
func FileMapFromRaw(raw map[string]map[string]any) (*FileMap, error) {
	files := make(map[string]*File, len(raw))
	for k, v := range raw {
		f, err := FileFromRaw(v)
		if err != nil {
			return nil, err
		}
		files[k] = f
	}
	return NewFileMap(files)
}

// ToRaw mirrors FileMap.toRaw.
func (fm *FileMap) ToRaw() map[string]map[string]any {
	out := make(map[string]map[string]any, len(fm.files))
	for k, v := range fm.files {
		if v == nil {
			continue
		}
		out[k] = v.ToRaw()
	}
	return out
}

// ToStats mirrors FileMap.toStats.
func (fm *FileMap) ToStats() map[string]map[string]any {
	out := make(map[string]map[string]any, len(fm.files))
	for k, v := range fm.files {
		if v == nil {
			continue
		}
		out[k] = v.ToStats()
	}
	return out
}

// AddFile creates the file at a validated, non-conflicting pathname.
func (fm *FileMap) AddFile(pathname string, file *File) error {
	if err := checkPathname(pathname); err != nil {
		return err
	}
	if fm.WouldConflict(pathname) {
		return &PathnameConflictError{Pathname: pathname}
	}
	addFileMap(fm.files, pathname, file)
	return nil
}

// RemoveFile removes the file at pathname (Node: `removeFile`).
func (fm *FileMap) RemoveFile(pathname string) error {
	if err := checkPathname(pathname); err != nil {
		return err
	}
	if _, ok := fm.files[pathname]; !ok {
		return &FileNotFoundError{Pathname: pathname}
	}
	delete(fm.files, pathname)
	return nil
}

// MoveFile renames/removes a file (Node: `moveFile`).
func (fm *FileMap) MoveFile(pathname, newPathname string) error {
	if pathname == newPathname {
		return nil
	}
	if newPathname == "" {
		return fm.RemoveFile(pathname)
	}
	if err := checkPathname(pathname); err != nil {
		return err
	}
	if err := checkPathname(newPathname); err != nil {
		return err
	}
	if fm.WouldConflict(newPathname, pathname) {
		return &PathnameConflictError{Pathname: newPathname}
	}
	file, ok := fm.files[pathname]
	if !ok {
		return &FileNotFoundError{Pathname: pathname}
	}
	delete(fm.files, pathname)
	addFileMap(fm.files, newPathname, file)
	return nil
}

// CountFiles returns the number of files (Node: `countFiles`).
func (fm *FileMap) CountFiles() int { return len(fm.files) }

// GetFile returns the file at pathname, or nil (Node: `getFile`).
func (fm *FileMap) GetFile(pathname string) *File { return fm.files[pathname] }

// GetPathnames returns the keys (Node: `getPathnames`).
func (fm *FileMap) GetPathnames() []string {
	out := make([]string, 0, len(fm.files))
	for k := range fm.files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MapValues maps the files (Node: `map`).
func (fm *FileMap) MapValues(f func(file *File, pathname string) any) map[string]any {
	out := make(map[string]any, len(fm.files))
	for k, v := range fm.files {
		out[k] = f(v, k)
	}
	return out
}

// MapAsync maps the files with an optional concurrency bound (Node: `mapAsync`);
// the Go port applies them in path order (values are independent of order).
func (fm *FileMap) MapAsync(f func(file *File, pathname string, pathnames []string) any, concurrency int) map[string]any {
	pathnames := fm.GetPathnames()
	out := make(map[string]any, len(pathnames))
	for _, p := range pathnames {
		out[p] = f(fm.GetFile(p), p, pathnames)
	}
	return out
}

// WouldConflict reports whether pathname conflicts with an entry (Node:
// `wouldConflict`): one is a strict dir prefix of the other.
func (fm *FileMap) WouldConflict(pathname string, ignoredPathname ...string) bool {
	if err := checkPathname(pathname); err != nil {
		return false
	}
	var ignored string
	if len(ignoredPathname) > 0 {
		ignored = ignoredPathname[0]
	}
	pathnames := fm.GetPathnames()
	dirname := pathname + "/"
	for _, p := range pathnames {
		if len(p) >= len(dirname) && p[:len(dirname)] == dirname && p != ignored {
			return true
		}
		if len(pathname) > len(p) && p == pathname[:len(p)] &&
			pathname[len(p)] == '/' && p != ignored {
			return true
		}
	}
	return false
}

// --- internals --------------------------------------------------------------

func addFileMap(files map[string]*File, pathname string, file *File) {
	if _, ok := files[pathname]; ok {
		delete(files, pathname)
	}
	files[pathname] = file
}

func checkPathname(pathname string) error {
	if pathname == "" {
		return newBadPathnameError(pathname, "pathname must be non-empty")
	}
	isClean, reason := IsCleanDebug(pathname)
	if !isClean {
		return newBadPathnameError(pathname, reason)
	}
	return nil
}

func checkPathnamesAreUnique(files map[string]*File) error {
	seen := map[string]bool{}
	for k := range files {
		if seen[k] {
			return &NonUniquePathnameError{Pathnames: mapKeys(files)}
		}
		seen[k] = true
	}
	return nil
}

func checkPathnamesDoNotConflict(fm *FileMap) error {
	pathnames := fm.GetPathnames()
	for _, p := range pathnames {
		if err := checkPathname(p); err != nil {
			return err
		}
	}
	dirnames := make([]string, len(pathnames))
	for i, p := range pathnames {
		dirnames[i] = p + "/"
	}
	sort.Strings(dirnames)
	for i := 0; i+1 < len(dirnames); i++ {
		if len(dirnames[i+1]) >= len(dirnames[i]) &&
			dirnames[i+1][:len(dirnames[i])] == dirnames[i] {
			conflictPathname := dirnames[i+1][:len(dirnames[i+1])-1]
			return &PathnameConflictError{Pathname: conflictPathname}
		}
	}
	return nil
}

func mapKeys(files map[string]*File) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	return out
}
