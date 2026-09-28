// Package snapshotmanager is the 1:1 port of
// services/project-history/app/js/SnapshotManager.js (310 L) — the snapshot
// queries over history-v1 chunks (git-bridge + history API surface).
//
// Self-contained modelling (B10 precedent; no cross-module otc import):
// the vendor `Core.Chunk.fromRaw → getSnapshot → applyAll` chain is
// implemented over the raw chunk maps, and the `File` surface is the
// minimal state {content?, hash, rangesHash?, metadata?, editable}.
//
// Faithful semantics:
//
//	S1 getFile*/getRanges*/getMetadata*: missing file →
//	   NotFoundError "<pathname> not found" {projectId, version, pathname}.
//	S2 getRangesSnapshot: non-editable file → {changes: [], comments: []}.
//	S3 getProjectSnapshot: content null → {data:{hash}}, else {data:{content}}
//	   (tracked-deletes filtered content).
//	S4 getChangesInChunkSince: sinceVersion > endVersion →
//	   BadRequestError 'requested version past the end of the history';
//	   sinceVersion < latestStartVersion → fetch that version's chunk.
//	S5 _loadFilesLimit: load only editable files with a rangesHash (a hash
//	   without ranges is dereferenced by the git bridge, not loaded).
package snapshotmanager

import (
	"context"
	"time"

	pherr "ollitex/go/services/project-history/internal/errors"
)

// Deps — the vendor module-level imports as seams.
type Deps struct {
	// GetHistoryID — WebApiManager.getHistoryId.
	GetHistoryID func(ctx context.Context, projectID string) (string, error)
	// GetMostRecentChunk / GetChunkAtVersion — C2 HistoryStoreManager.
	GetMostRecentChunk func(ctx context.Context, projectID, historyID string) (map[string]any, error)
	GetChunkAtVersion  func(ctx context.Context, projectID, historyID string, version int) (map[string]any, error)
	// GetBlob — the history blob store fetch (C2 BlobStore.FetchString).
	GetBlob func(ctx context.Context, historyID, hash string) (string, error)
	// Ranges — the Core.getDocUpdaterCompatibleRanges seam (wired D-phase
	// to the editor-core util port; tests supply fakes). Returns
	// (changes, comments) for the loaded file's ranges blob.
	Ranges func(ctx context.Context, historyID string, f *File) ([]any, []any)
	// MaxRequests — vendor MAX_REQUESTS (=4).
	MaxRequests int
}

func (d *Deps) withDefaults() *Deps {
	if d == nil {
		d = &Deps{}
	}
	if d.MaxRequests <= 0 {
		d.MaxRequests = 4
	}
	return d
}

// File — the per-pathname snapshot state (the vendor Core.File surface used
// by this module).
type File struct {
	Pathname   string
	Hash       string
	RangesHash string
	Metadata   map[string]any
	// ByteLength — vendor BinaryFileData.byteLength (0 when unknown).
	ByteLength int
	// Comments/TrackedChanges — raw arrays when present (editor-core
	// StringFileData raw shape); empty → key omitted from ToRaw.
	Comments       []any
	TrackedChanges []any
	// Content is nil for binary files or until loaded from the blob.
	Content  *string
	Editable bool
	loaded   bool
	loadErr  error
}

// Snapshot — files by pathname with stable path order (vendor FileMap).
type Snapshot struct {
	files   map[string]*File
	ordered []string
	// ProjectVersion/V2DocVersions — vendor snapshot.projectVersion /
	// v2DocVersions (raw model; set where the chunk carries them).
	ProjectVersion string
	V2DocVersions  map[string]any
	// Timestamp — vendor snapshot.timestamp (ISO string when present).
	Timestamp string
}

// GetFile — vendor snapshot.getFile (nil when absent).
func (s *Snapshot) GetFile(pathname string) *File { return s.files[pathname] }

// GetFilePathnames — vendor snapshot.getFilePathnames.
func (s *Snapshot) GetFilePathnames() []string {
	out := make([]string, 0, len(s.ordered))
	return append(out, s.ordered...)
}

func (s *Snapshot) orderedFiles() []*File {
	out := make([]*File, 0, len(s.ordered))
	for _, p := range s.ordered {
		out = append(out, s.files[p])
	}
	return out
}

// --- value helpers ---------------------------------------------------------

func intAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func stringAny(v any) string {
	s, _ := v.(string)
	return s
}

func listAny(v any) []any {
	lst, _ := v.([]any)
	return lst
}

// --- chunk modeling --------------------------------------------------------

func dataChunk(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	if c, ok := data["chunk"].(map[string]any); ok {
		return c
	}
	return data
}

// rawFile — vendor snapshot file shape {hash, stringLength?, rangesHash?,
// metadata?} → File state.
func rawFile(pathname string, f map[string]any) *File {
	file := &File{
		Pathname:   pathname,
		Hash:       stringAny(f["hash"]),
		RangesHash: stringAny(f["rangesHash"]),
	}
	// vendor: files with a null stringLength are binary (non-editable).
	if sl, has := f["stringLength"]; has && sl != nil {
		file.Editable = true
		c := ""
		file.Content = &c
	}
	if meta, ok := f["metadata"].(map[string]any); ok {
		file.Metadata = meta
	}
	return file
}

// buildSnapshot — vendor `Core.Chunk.fromRaw(chunk).getSnapshot()` +
// `applyAll(changes)` (only the first n = version - startVersion changes,
// as _getSnapshotAtVersion slices them; the caller passes the slice bound).
func (d *Deps) buildSnapshot(chunk map[string]any, applyUpTo int) (*Snapshot, int, int) {
	if chunk == nil {
		return nil, 0, 0
	}
	startVersion := 0
	if v, ok := intAny(chunk["startVersion"]); ok {
		startVersion = v
	}
	history, _ := chunk["history"].(map[string]any)
	var changes []map[string]any
	if history != nil {
		for _, e := range listAny(history["changes"]) {
			if m, ok := e.(map[string]any); ok {
				changes = append(changes, m)
			}
		}
	}
	endVersion := startVersion + len(changes)

	snap := &Snapshot{files: map[string]*File{}}
	if history != nil {
		// vendor wire shape: chunk.history = <rawHistory> with
		// { snapshot: <rawSnapshotDocument>, changes: [...] } where
		// rawSnapshotDocument = { files: {path: <rawFile>}, projectVersion?,
		// v2DocVersions?, timestamp? } (editor-core Snapshot.fromRaw; the B10
		// ChunkTranslator produces exactly this document).
		if snapDoc, ok := history["snapshot"].(map[string]any); ok {
			filesRaw, _ := snapDoc["files"].(map[string]any)
			for path := range filesRaw {
				f, _ := filesRaw[path].(map[string]any)
				if f == nil {
					continue
				}
				file := rawFile(path, f)
				snap.files[path] = file
				snap.ordered = append(snap.ordered, path)
			}
			if v := stringAny(snapDoc["projectVersion"]); v != "" {
				snap.ProjectVersion = v
			}
			if vd, ok := snapDoc["v2DocVersions"].(map[string]any); ok {
				snap.V2DocVersions = vd
			}
			if ts, ok := snapDoc["timestamp"]; ok && ts != nil {
				if t, isT := ts.(time.Time); isT {
					snap.Timestamp = t.UTC().Format("2006-01-02T15:04:05.000Z")
				} else if str, isS := ts.(string); isS {
					snap.Timestamp = str
				}
			}
		}
	}
	// applyAll(changes) — the vendor applies each change's operations in
	// order; `applyUpTo` bounds the number of changes (the -1 case: all).
	limit := len(changes)
	if applyUpTo >= 0 && applyUpTo < limit {
		limit = applyUpTo
	}
	for i := 0; i < limit; i++ {
		for _, opAny := range listAny(changes[i]["operations"]) {
			op, _ := opAny.(map[string]any)
			if op == nil {
				continue
			}
			snap.applyOp(op)
		}
	}
	return snap, startVersion, endVersion
}

// applyOp — one edit operation (vendor EditOperation family, the shapes
// that affect the snapshot state).
func (s *Snapshot) applyOp(op map[string]any) {
	pathname := stringAny(op["pathname"])
	if pathname == "" {
		return
	}

	// rename / move / remove: `newPathname` present ("" = remove)
	if v, has := op["newPathname"]; has {
		dst := stringAny(v)
		src, exists := s.files[pathname]
		if !exists {
			if dst == "" {
				return
			}
			src = &File{Pathname: pathname}
			s.files[pathname] = src
			s.ordered = append(s.ordered, pathname)
		}
		if dst == "" {
			s.removePath(pathname)
			return
		}
		if oldIdx, hasIdx := indexOf(s.ordered, pathname); hasIdx {
			if dstIdx, hasDst := indexOf(s.ordered, dst); !hasDst {
				s.ordered[oldIdx] = dst
			} else {
				s.ordered = append(s.ordered[:dstIdx], s.ordered[dstIdx+1:]...)
				if oldIdx > dstIdx {
					s.ordered[oldIdx-1] = dst
				} else {
					s.ordered[oldIdx] = dst
				}
			}
		} else {
			s.ordered = append(s.ordered, dst)
		}
		src.Pathname = dst
		s.files[dst] = src
		delete(s.files, pathname)
		return
	}

	// add file
	if v, has := op["file"]; has {
		f, _ := v.(map[string]any)
		if f == nil {
			f = map[string]any{}
		}
		file := rawFile(pathname, f)
		if _, existed := s.files[pathname]; !existed {
			s.ordered = append(s.ordered, pathname)
		}
		s.files[pathname] = file
		return
	}

	// edit content (text operation)
	if v, has := op["textOperation"]; has && v != nil {
		file, exists := s.files[pathname]
		if !exists {
			return
		}
		content := ""
		if file.Content != nil {
			content = *file.Content
		}
		newContent := applyTextOp(content, v)
		c := newContent
		file.Content = &c
		file.Editable = true
		if h := stringAny(op["newContentHash"]); h != "" {
			file.Hash = h
		}
	}
}

func (s *Snapshot) removePath(pathname string) {
	delete(s.files, pathname)
	if i, ok := indexOf(s.ordered, pathname); ok {
		s.ordered = append(s.ordered[:i], s.ordered[i+1:]...)
	}
}

func indexOf(list []string, v string) (int, bool) {
	for i, e := range list {
		if e == v {
			return i, true
		}
	}
	return -1, false
}

// applyTextOp — vendor TextOperation apply: retain n → advance; insert s →
// append; delete n → drop. (The port applies on runes — vendor JS strings
// are UTF-16 units, identical for BMP test corpora.)
func applyTextOp(content string, v any) string {
	runes := []rune(content)
	var out []rune
	cursor := 0
	for _, opAny := range listAny(v) {
		switch t := opAny.(type) {
		case int:
			if t >= 0 { // retain n
				end := cursor + t
				if end > len(runes) {
					end = len(runes)
				}
				if cursor <= end {
					out = append(out, runes[cursor:min2(cursor, len(runes))]...)
				}
				cursor = end
			} else { // delete n
				cursor += -t
			}
		case float64:
			if t > 0 { // retain n
				end := cursor + int(t)
				if end > len(runes) {
					end = len(runes)
				}
				if cursor < len(runes) {
					out = append(out, runes[cursor:min2(cursor, len(runes))]...)
				}
				cursor = end
			} else { // delete n
				cursor += int(-t)
			}
		case string: // insert s
			out = append(out, []rune(t)...)
		}
	}
	return string(out)
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// loadFile — vendor file.load(kind, blobStore) for a content-fetch.
func (d *Deps) loadFile(ctx context.Context, historyID string, f *File) error {
	if f.loaded {
		return f.loadErr
	}
	if !f.Editable {
		f.loaded = true
		return nil
	}
	if d.GetBlob == nil {
		f.loadErr = &wiringError{msg: "history blob store seam is not wired"}
		f.loaded = true
		return f.loadErr
	}
	blob, err := d.GetBlob(ctx, historyID, f.Hash)
	if err != nil {
		f.loadErr = err
		f.loaded = true
		return err
	}
	c := blob
	f.Content = &c
	f.loaded = true
	return nil
}

// --- exported surface -------------------------------------------------------

// GetFileSnapshotStream — vendor getFileSnapshotStream (S1).
func (d *Deps) GetFileSnapshotStream(ctx context.Context, projectID string, version int, pathname string) ([]byte, error) {
	d = d.withDefaults()
	snap, _, _, err := d.snapshotAtVersion(ctx, projectID, version)
	if err != nil {
		return nil, err
	}
	file := snap.GetFile(pathname)
	if file == nil {
		return nil, notFound(projectID, version, pathname)
	}
	historyID, err := d.historyID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if file.Editable {
		if e := d.loadFile(ctx, historyID, file); e != nil {
			return nil, e
		}
		return []byte(*file.Content), nil
	}
	// vendor: binary → return the raw blob stream
	blob, e := d.GetBlob(ctx, historyID, file.Hash)
	if e != nil {
		return nil, e
	}
	return []byte(blob), nil
}

// GetRangesSnapshot — vendor getRangesSnapshot (S2).
func (d *Deps) GetRangesSnapshot(ctx context.Context, projectID string, version int, pathname string) (map[string]any, error) {
	d = d.withDefaults()
	snap, _, _, err := d.snapshotAtVersion(ctx, projectID, version)
	if err != nil {
		return nil, err
	}
	file := snap.GetFile(pathname)
	if file == nil {
		return nil, notFound(projectID, version, pathname)
	}
	if !file.Editable {
		return map[string]any{"changes": []any{}, "comments": []any{}}, nil
	}
	historyID, err := d.historyID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if e := d.loadFile(ctx, historyID, file); e != nil {
		return nil, e
	}
	var changes, comments []any
	if d.Ranges != nil {
		changes, comments = d.Ranges(ctx, historyID, file)
	}
	if changes == nil {
		changes = []any{}
	}
	if comments == nil {
		comments = []any{}
	}
	return map[string]any{"changes": changes, "comments": comments}, nil
}

// GetFileMetadataSnapshot — vendor getFileMetadataSnapshot
// (`_.isEmpty(raw) → undefined` → the key is ABSENT from the JSON).
func (d *Deps) GetFileMetadataSnapshot(ctx context.Context, projectID string, version int, pathname string) (map[string]any, error) {
	d = d.withDefaults()
	snap, _, _, err := d.snapshotAtVersion(ctx, projectID, version)
	if err != nil {
		return nil, err
	}
	file := snap.GetFile(pathname)
	if file == nil {
		return nil, notFound(projectID, version, pathname)
	}
	out := map[string]any{}
	if file.Metadata != nil && len(file.Metadata) > 0 {
		out["metadata"] = file.Metadata
	}
	return out, nil
}

// GetProjectSnapshot — vendor getProjectSnapshot (S3, S5).
func (d *Deps) GetProjectSnapshot(ctx context.Context, projectID string, version int) (map[string]any, error) {
	d = d.withDefaults()
	snap, _, _, err := d.snapshotAtVersion(ctx, projectID, version)
	if err != nil {
		return nil, err
	}
	historyID, err := d.historyID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// S5: _loadFilesLimit — only editable files WITH a rangesHash.
	for _, file := range snap.orderedFiles() {
		if file == nil || !file.Editable {
			continue
		}
		if file.Hash != "" && file.RangesHash == "" {
			continue // git bridge dereferences the blob
		}
		if file.RangesHash == "" {
			continue
		}
		if e := d.loadFile(ctx, historyID, file); e != nil {
			return nil, e
		}
	}
	files := make([]any, 0, len(snap.ordered))
	for _, file := range snap.orderedFiles() {
		if file == nil {
			files = append(files, nil)
			continue
		}
		// S3: content === null → hash form
		if file.Content == nil {
			files = append(files, map[string]any{"data": map[string]any{"hash": file.Hash}})
			continue
		}
		files = append(files, map[string]any{"data": map[string]any{"content": *file.Content}})
	}
	return map[string]any{"projectId": projectID, "files": files}, nil
}

// GetPathsAtVersion — vendor getPathsAtVersion.
func (d *Deps) GetPathsAtVersion(ctx context.Context, projectID string, version int) (map[string]any, error) {
	d = d.withDefaults()
	snap, _, _, err := d.snapshotAtVersion(ctx, projectID, version)
	if err != nil {
		return nil, err
	}
	return map[string]any{"paths": snap.GetFilePathnames()}, nil
}

// GetLatestSnapshot — vendor getLatestSnapshot → {version, snapshot}.
func (d *Deps) GetLatestSnapshot(ctx context.Context, projectID, historyID string) (int, error) {
	d = d.withDefaults()
	data, err := d.GetMostRecentChunk(ctx, projectID, historyID)
	if err != nil {
		return 0, err
	}
	chunk := dataChunk(data)
	if chunk == nil {
		return 0, pherrBadRequest("undefined chunk")
	}
	if data == nil {
		return 0, pherrBadRequest("undefined chunk")
	}
	snap, _, endVersion := d.buildSnapshot(chunk, -1)
	_ = snap
	return endVersion, nil
}

// GetLatestSnapshotFiles — vendor getLatestSnapshotFiles: the snapshot's
// files loaded 'lazy' (content available on demand).
func (d *Deps) GetLatestSnapshotFiles(ctx context.Context, projectID, historyID string) (map[string]*File, error) {
	d = d.withDefaults()
	data, err := d.GetMostRecentChunk(ctx, projectID, historyID)
	if err != nil {
		return nil, err
	}
	chunk := dataChunk(data)
	if chunk == nil {
		return nil, pherrBadRequest("undefined chunk")
	}
	snap, _, _ := d.buildSnapshot(chunk, -1)
	return snap.files, nil
}

// GetChangesInChunkSince — vendor getChangesInChunkSince (S4).
func (d *Deps) GetChangesInChunkSince(ctx context.Context, projectID, historyID string, sinceVersion int) (int, []map[string]any, error) {
	d = d.withDefaults()
	data, err := d.GetMostRecentChunk(ctx, projectID, historyID)
	if err != nil {
		return 0, nil, err
	}
	chunk := dataChunk(data)
	if chunk == nil {
		return 0, nil, pherrBadRequest("undefined chunk")
	}
	_, startVersion, endVersion := d.buildSnapshot(chunk, -1)
	if sinceVersion > endVersion {
		return 0, nil, pherr.BadRequest("requested version past the end of the history")
	}
	if sinceVersion < startVersion {
		data2, err2 := d.GetChunkAtVersion(ctx, projectID, historyID, sinceVersion)
		if err2 != nil {
			return 0, nil, err2
		}
		chunk = dataChunk(data2)
		if chunk == nil {
			return 0, nil, pherrBadRequest("undefined chunk")
		}
		_, startVersion, _ = d.buildSnapshot(chunk, -1)
	}
	changes := []map[string]any{}
	if history, ok := chunk["history"].(map[string]any); ok {
		ops := listAny(history["changes"])
		idx := sinceVersion - startVersion
		if idx < 0 {
			idx = 0
		}
		for i := idx; i < len(ops); i++ {
			if m, ok := ops[i].(map[string]any); ok {
				changes = append(changes, m)
			}
		}
	}
	return startVersion, changes, nil
}

// --- internals --------------------------------------------------------------

func pherrBadRequest(msg string) error { return pherr.BadRequest(msg) }

// historyID — the vendor `await WebApiManager.promises.getHistoryId` — the
// seam is mandatory in production builds; a nil seam is a wiring error.
func (d *Deps) historyID(ctx context.Context, projectID string) (string, error) {
	if d.GetHistoryID == nil {
		return "", &wiringError{msg: "WebApiManager.getHistoryId seam is not wired"}
	}
	return d.GetHistoryID(ctx, projectID)
}

type wiringError struct{ msg string }

func (e *wiringError) Error() string { return e.msg }

func notFound(projectID string, version int, pathname string) error {
	return &notFoundError{projectID: projectID, version: version, pathname: pathname}
}

type notFoundError struct {
	projectID string
	version   int
	pathname  string
}

func (e *notFoundError) Error() string { return e.pathname + " not found" }
func (e *notFoundError) Info() map[string]any {
	return map[string]any{
		"projectId": e.projectID,
		"version":   e.version,
		"pathname":  e.pathname,
	}
}

// snapshotAtVersion — vendor _getSnapshotAtVersion: the snapshot with the
// changes [0, version - startVersion) applied. Store/seam failures PROPAGATE
// (vendor: the awaits reject before the file lookup).
func (d *Deps) snapshotAtVersion(ctx context.Context, projectID string, version int) (*Snapshot, int, int, error) {
	d = d.withDefaults()
	historyID, err := d.historyID(ctx, projectID)
	if err != nil {
		return nil, 0, 0, err
	}
	if d.GetChunkAtVersion == nil {
		return nil, 0, 0, &wiringError{msg: "history store seam is not wired"}
	}
	data, err := d.GetChunkAtVersion(ctx, projectID, historyID, version)
	if err != nil {
		return nil, 0, 0, err
	}
	chunk := dataChunk(data)
	if chunk == nil {
		return nil, 0, 0, pherrBadRequest("undefined chunk")
	}
	// vendor: `chunk.getChanges().slice(0, version - chunk.getStartVersion())`
	applyUpTo := -1
	if sv, ok := intAny(chunk["startVersion"]); ok {
		if av := version - sv; av >= 0 {
			applyUpTo = av
		}
	}
	snap, sv2, ev := d.buildSnapshot(chunk, applyUpTo)
	return snap, sv2, ev, nil
}
