// Package historyresourcewriter ports services/clsi/app/js/HistoryResourceWriter.js
// (869 L) 1:1: the compile-from-cache snapshot cache (history.json.gz), the
// clsi-cache resync download, the depth-first extraneous-entry sweep, the
// png2pdf slow-list gating, and the sync-to-compile-dir write loop.
//
// Design: services/clsi.go/HANDOFF.md §10. Injectable seams are
// package-level function vars (mirroring the Node vi.doMock targets);
// production values are wired below and tests override them with fakes.
package historyresourcewriter

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"clsi/clsicachehandler"
	"clsi/config"
	"clsi/errors"
	"clsi/logger"
	"clsi/png2pdf"
	"clsi/resourcewriter"
	"clsi/tikzmanager"
	"clsi/urlcache"

	"ollitex/go/libraries/fetchutils"
	otc "ollitex/go/libraries/otc"
)

// --- seams (HANDOFF §10.1; production defaults wired here) -------------------

var (
	DownloadUrlToFile       = urlcache.DownloadUrlToFile
	IsConversionCached      = urlcache.IsConversionCached
	CommitConversion        = urlcache.CommitConversion
	CreateProjectDir        = urlcache.CreateProjectDir
	GetProjectCacheDir      = urlcache.GetProjectCacheDir
	Png2PdfEnabled          = png2pdf.IsEnabled
	PngConvert              = defaultPngConvert
	DownloadHistorySnapshot = clsicachehandler.DownloadHistorySnapshot
	IsExtraneousFile        = resourcewriter.IsExtraneousFile
	WriteOutputFileIfNeeded = tikzmanager.WriteOutputFileIfNeeded
	FetchStringFunc         = fetchutils.FetchString
)

// PngConvertSeam is the HRW-side bridge to png2pdf (stats/timings are the
// loose Record<string, number> maps Node passes through).
type PngConvertFunc func(projectID, cacheProjectDir string, relativePaths []string,
	stats, timings map[string]any,
) error

// ResetSeams restores production wiring (tests defer this).
func ResetSeams() {
	DownloadUrlToFile = urlcache.DownloadUrlToFile
	IsConversionCached = urlcache.IsConversionCached
	CommitConversion = urlcache.CommitConversion
	CreateProjectDir = urlcache.CreateProjectDir
	GetProjectCacheDir = urlcache.GetProjectCacheDir
	Png2PdfEnabled = png2pdf.IsEnabled
	PngConvert = defaultPngConvert
	DownloadHistorySnapshot = clsicachehandler.DownloadHistorySnapshot
	IsExtraneousFile = resourcewriter.IsExtraneousFile
	WriteOutputFileIfNeeded = tikzmanager.WriteOutputFileIfNeeded
	FetchStringFunc = fetchutils.FetchString
}

// defaultPngConvert bridges HRW's map stats/timings onto png2pdf.Stats/Timings.
var defaultPngConvert = func(projectID, cacheProjectDir string, relativePaths []string,
	stats, timings map[string]any,
) error {
	st := png2pdf.Stats{}
	tm := png2pdf.Timings{}
	err := png2pdf.ConvertPngFilesInCacheDir(projectID, cacheProjectDir, relativePaths, &st, &tm)
	// Node: timings.png2pdf is recorded even on error (the timer done() runs
	// before the throw); stats.png2pdf only on success.
	timings["png2pdf"] = tm.Png2pdf
	if err == nil {
		stats["png2pdf"] = st.Png2pdf
	}
	return err
}

// --- Request / Result (HANDOFF §10.2) ----------------------------------------

// Request is the HRW-relevant slice of the compile request (compilemanager
// fills it from the parsed CLSI request).
type Request struct {
	BaseHistoryVersion  int
	RawSnapshot         map[string]any
	GlobalBlobs         []string
	RawChangeOperations [][]map[string]any
	PopulateClsiCache   bool
	Png2pdf             bool
	HistoryID           string
	FilestoreBlobPrefix string
	ClSIPerfVariant     string
	Draft               bool
	RootResourcePath    string
	CompileGroup        string
	MetricsPath         string // for metrics.ShouldSkipMetrics
}

// Resource is one entry of the resource list (Node: { path: string }).
type Resource struct{ Path string }

// Result mirrors Node's { baseHistoryVersion, resourceList } return.
type Result struct {
	BaseHistoryVersion int
	ResourceList       []Resource
}

// --- slow-PNG list / snapshot paths (Node: snapshotPath + save/loadSlowPngList)

// snapshotPaths mirrors snapshotPath(cacheKey).
type snapshotPaths struct {
	dir         string
	path        string
	resyncPath  string
	slowPngPath string
}

// SnapshotPathNames mirrors Node snapshotPath (exported: tests and the
// compile wiring need the dir for the clsi-cache download target).
func SnapshotPathNames(cacheKey string) snapshotPaths {
	dir := filepath.Join(config.Get().Path.ClsiCacheDir, cacheKey)
	return snapshotPaths{
		dir:         dir,
		path:        filepath.Join(dir, "history.json.gz"),
		resyncPath:  filepath.Join(dir, "history-resync.json.gz"),
		slowPngPath: filepath.Join(dir, "png2pdf-slow.json"),
	}
}

// SaveSlowPngList mirrors saveSlowPngList: persist the slow-PNG list learned
// from the compile that just ran, so the next sync can convert only those.
// Best-effort at the caller; errors here mean the next sync falls back to the
// previous list.
func SaveSlowPngList(cacheKey string, slowPngs []string) error {
	sp := SnapshotPathNames(cacheKey)
	if err := os.MkdirAll(sp.dir, 0o755); err != nil {
		return err
	}
	if slowPngs == nil {
		slowPngs = []string{}
	}
	data, err := json.Marshal(slowPngs)
	if err != nil {
		return err
	}
	tmp := sp.slowPngPath + "~"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, sp.slowPngPath)
}

func loadSlowPngList(cacheKey string) []string {
	sp := SnapshotPathNames(cacheKey)
	blob, err := os.ReadFile(sp.slowPngPath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn(map[string]any{"err": err.Error(), "cacheKey": cacheKey},
				"compile from cache: cannot read slow-png list")
		}
		return []string{}
	}
	var list []string
	if err := json.Unmarshal(blob, &list); err != nil || list == nil {
		logger.Warn(map[string]any{"err": "not an array", "cacheKey": cacheKey},
			"compile from cache: cannot read slow-png list")
		return []string{}
	}
	return list
}

// ClearCache mirrors clearCache: best-effort remove of the snapshot dir
// (warns, never fails; ENOENT silent).
func ClearCache(projectID, userID, cacheKey string) {
	if err := os.RemoveAll(SnapshotPathNames(cacheKey).dir); err != nil {
		logger.Warn(map[string]any{
			"err":       err.Error(),
			"projectId": projectID,
			"userId":    userID,
			"cacheKey":  cacheKey,
		}, "compile from cache: failed to clear history cache")
	}
}

// --- snapshot load/save (Node: loadSnapshot / loadSnapshotFromFile /
// loadSnapshotFromClsiCache / saveSnapshot / deleteResyncSnapshot)

type snapshotLoad struct {
	RawSnapshot      map[string]any
	GlobalBlobs      []string
	LocalBaseVersion int
	FullSync         bool
	Dirty            []string
	// LastPng2pdf is the png2pdf mode recorded by the last sync (nil only on
	// the remote fallback, where Node's variable is `undefined`).
	LastPng2pdf *bool
}

func loadSnapshot(projectID, userID, cacheKey string, remoteBaseVersion int,
	populateClsiCache bool,
) (snapshotLoad, error) {
	sp := SnapshotPathNames(cacheKey)
	maxLocalBaseVersion := -1
	candidates := []string{sp.path, sp.resyncPath}
	for i, candidate := range candidates {
		fullSync := i == 1
		loaded, err := loadSnapshotFromFile(candidate, remoteBaseVersion, fullSync)
		if err == nil {
			return loaded, nil
		}
		if me, ok := err.(*errors.MissingUpdatesError); ok {
			if v, ok := me.Info["baseHistoryVersion"].(int); ok {
				maxLocalBaseVersion = maxInt(maxLocalBaseVersion, v)
			}
		} else if !os.IsNotExist(err) {
			logger.Warn(map[string]any{"err": err.Error(), "projectId": projectID, "userId": userID, "cacheKey": cacheKey},
				"compile from cache: cannot read history from disk")
		}
	}
	if populateClsiCache {
		loaded, err := loadSnapshotFromClsiCache(projectID, userID, cacheKey, remoteBaseVersion)
		if err == nil {
			return loaded, nil
		}
		if me, ok := err.(*errors.MissingUpdatesError); ok {
			if v, ok := me.Info["baseHistoryVersion"].(int); ok {
				maxLocalBaseVersion = maxInt(maxLocalBaseVersion, v)
			}
		} else if !os.IsNotExist(err) {
			logger.Warn(map[string]any{"err": err.Error(), "projectId": projectID, "userId": userID, "cacheKey": cacheKey},
				"compile from cache: cannot download from clsi-cache")
		}
	}
	return snapshotLoad{}, errors.NewMissingUpdatesError("needs more updates",
		map[string]any{"baseHistoryVersion": maxLocalBaseVersion})
}

func loadSnapshotFromClsiCache(projectID, userID, cacheKey string, remoteBaseVersion int) (snapshotLoad, error) {
	sp := SnapshotPathNames(cacheKey)
	if err := os.MkdirAll(sp.dir, 0o755); err != nil {
		return snapshotLoad{}, err
	}
	ok, err := DownloadHistorySnapshot(projectID, userID, sp.dir)
	if err != nil {
		return snapshotLoad{}, err
	}
	if !ok {
		return snapshotLoad{}, errors.NewMissingUpdatesError("needs full sync",
			map[string]any{"baseHistoryVersion": -1})
	}
	logger.Debug(map[string]any{"projectId": projectID, "userId": userID},
		"compile from cache: restored history from clsi-cache")
	return loadSnapshotFromFile(sp.resyncPath, remoteBaseVersion, true)
}

type rawSnapshotFile struct {
	RawSnapshot      map[string]any `json:"rawSnapshot"`
	GlobalBlobs      []string       `json:"globalBlobs"`
	LocalBaseVersion *int           `json:"localBaseVersion"`
	Dirty            []string       `json:"dirty"`
	Png2pdf          bool           `json:"png2pdf"`
}

func loadSnapshotFromFile(file string, remoteBaseVersion int, fullSync bool) (snapshotLoad, error) {
	blob, err := os.ReadFile(file)
	if err != nil {
		return snapshotLoad{}, err
	}
	blob, err = gunzipBytes(blob)
	if err != nil {
		return snapshotLoad{}, err
	}
	var raw rawSnapshotFile
	if err := json.Unmarshal(blob, &raw); err != nil {
		return snapshotLoad{}, err
	}
	localBaseVersion := 0
	if raw.LocalBaseVersion != nil {
		localBaseVersion = *raw.LocalBaseVersion
		// Node: `localBaseVersion < remoteBaseVersion` — a missing key is
		// undefined and the comparison is false.
		if localBaseVersion < remoteBaseVersion {
			return snapshotLoad{}, errors.NewMissingUpdatesError("missing updates",
				map[string]any{"baseHistoryVersion": localBaseVersion})
		}
	}
	dirty := raw.Dirty
	if dirty == nil {
		dirty = []string{}
	}
	globalBlobs := raw.GlobalBlobs
	if globalBlobs == nil {
		globalBlobs = []string{}
	}
	png2pdf := raw.Png2pdf
	return snapshotLoad{
		RawSnapshot:      raw.RawSnapshot,
		GlobalBlobs:      globalBlobs,
		LocalBaseVersion: localBaseVersion,
		FullSync:         fullSync,
		Dirty:            dirty,
		LastPng2pdf:      &png2pdf,
	}, nil
}

// rawSnapshotData is the on-disk shape (field order mirrors Node).
type rawSnapshotData struct {
	GlobalBlobs      []string       `json:"globalBlobs"`
	LocalBaseVersion int            `json:"localBaseVersion"`
	RawSnapshot      map[string]any `json:"rawSnapshot"`
	Dirty            []string       `json:"dirty"`
	Png2pdf          bool           `json:"png2pdf"`
}

func saveSnapshot(cacheKey string, toRaw map[string]any, localBaseVersion int,
	globalBlobs []string, dirty []string, png2pdfMode bool,
) error {
	sp := SnapshotPathNames(cacheKey)
	if err := os.MkdirAll(sp.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(rawSnapshotData{
		GlobalBlobs:      globalBlobs,
		LocalBaseVersion: localBaseVersion,
		RawSnapshot:      toRaw,
		Dirty:            dirty,
		Png2pdf:          png2pdfMode,
	})
	if err != nil {
		return err
	}
	tmp := sp.path + "~"
	// Node: writeFile(tmp, gzip(...), { flag: 'wx' }) — create, fail if exists.
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	gz, gerr := gzip.NewWriterLevel(f, 1) // cheapest level, as in Node { level: 1 }
	if gerr == nil {
		_, gerr = gz.Write(data)
	}
	if gerr == nil {
		gerr = gz.Close()
	}
	if gerr == nil {
		gerr = f.Close()
	} else {
		f.Close()
	}
	if gerr != nil {
		os.Remove(tmp)
		return gerr
	}
	return os.Rename(tmp, sp.path)
}

func deleteResyncSnapshot(projectID, userID, cacheKey string) {
	err := os.Remove(SnapshotPathNames(cacheKey).resyncPath)
	if err != nil && !os.IsNotExist(err) {
		logger.Warn(map[string]any{"err": err.Error(), "projectId": projectID, "userId": userID, "cacheKey": cacheKey},
			"compile from cache: failed to clear history-resync.json.gz")
	}
}

// --- filesystem discovery / cleanup (Node: discoverExistingEntries /
// removeExtraneousEntries / ensureHasParentFolder)

// entryList mirrors Node's Map<string, boolean> (pathname -> isDir) with
// insertion-order iteration; discovery is post-recursive so children precede
// parents.
type entryList struct {
	order []string
	isDir map[string]bool
}

func (e *entryList) set(p string, dir bool) {
	if _, ok := e.isDir[p]; !ok {
		e.order = append(e.order, p)
	}
	e.isDir[p] = dir
}

func (e *entryList) has(p string) bool {
	_, ok := e.isDir[p]
	return ok
}

func (e *entryList) isDirOf(p string) bool {
	v, ok := e.isDir[p]
	return ok && v
}

func (e *entryList) remove(p string) {
	delete(e.isDir, p)
}

func discoverExistingEntries(compileDir, subDir string, entries *entryList) error {
	dirents, err := os.ReadDir(filepath.Join(compileDir, subDir))
	if err != nil {
		return err
	}
	for _, d := range dirents {
		p := path.Join(subDir, d.Name())
		m := d.Type()
		if m.IsDir() {
			if err := discoverExistingEntries(compileDir, p, entries); err != nil {
				return err
			}
		} else if m == 0 {
			entries.set(p, false)
		} else if m&os.ModeSymlink != 0 ||
			(m&os.ModeDevice != 0 && m&os.ModeCharDevice == 0) || m&os.ModeSocket != 0 {
			// should not happen, delete right away
			logger.Warn(map[string]any{"compileDir": compileDir, "subDir": subDir, "dirent": d.Name()},
				"compile from cache: found blocked dirent")
			if err := os.Remove(filepath.Join(compileDir, p)); err != nil {
				return err
			}
		} else {
			return errors.NewOError("unexpected dir entry",
				map[string]any{"compileDir": compileDir, "subDir": subDir, "dirent": d.Name()})
		}
	}
	entries.set(subDir, true)
	return nil
}

func removeExtraneousEntries(compileDir string, getSnapshotHas func(path string) bool,
	e *entryList,
) error {
	keepFolders := map[string]struct{}{".": {}}
	for _, p := range e.order {
		if !e.has(p) {
			continue
		}
		isDir := e.isDir[p]
		shouldBeFile := getSnapshotHas(p)
		if isDir {
			if !shouldBeFile {
				// directory can stay directory
				if _, ok := keepFolders[p]; ok {
					// folder is still in use
					keepFolders[path.Dir(p)] = struct{}{}
				} else {
					// empty folder
					if err := os.Remove(filepath.Join(compileDir, p)); err != nil {
						return err
					}
					e.remove(p)
				}
				continue
			}
			// a folder turned into a file
			// before: foo/bar.txt/baz.txt      now: foo/bar.txt
			//             ^^^^^^^ folder              ^^^^^^^ file
			needle := p + "/"
			for _, child := range e.order {
				if !e.has(child) || !strings.HasPrefix(child, needle) {
					continue
				}
				if err := os.Remove(filepath.Join(compileDir, child)); err != nil {
					return err
				}
				e.remove(child)
			}
			if err := os.Remove(filepath.Join(compileDir, p)); err != nil {
				return err
			}
			e.remove(p)
			continue
		}
		if shouldBeFile || !IsExtraneousFile(p) {
			// resource or cached file
			keepFolders[path.Dir(p)] = struct{}{}
			continue
		}
		if err := os.Remove(filepath.Join(compileDir, p)); err != nil {
			return err
		}
		e.remove(p)
	}
	return nil
}

func ensureHasParentFolder(compileDir, p string, e *entryList) error {
	parent := path.Dir(p)
	if e.has(parent) {
		return nil
	}
	if err := ensureHasParentFolder(compileDir, parent, e); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(compileDir, parent), 0o755); err != nil {
		return err
	}
	e.set(parent, true)
	return nil
}

// --- helpers -------------------------------------------------------------------

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func isPng(p string) bool {
	return strings.ToLower(filepath.Ext(p)) == ".png"
}

func gunzipBytes(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// changesFromRawChangeOperations mirrors
// `raw.map(o => Change.mustFromRaw({operations: o, timestamp: "0"}))`. The Go
// otc ChangeFromRaw rejects raw timestamp '0' (not a valid ISO time), so the
// change is built synthetically: the zero time.Time mirrors mustFromRaw's
// timestamp-only purpose (it affects Change.Timestamp/ToRaw, never
// ApplyAll semantics). An empty `{}` op is a NoOperation (OperationFromRaw's
// nil-variant default); an op that fails to materialise surfaces as an error,
// so the sync fails loudly rather than corrupting the snapshot state.
func changesFromRawChangeOperations(raw [][]map[string]any) ([]*otc.Change, error) {
	changes := make([]*otc.Change, 0, len(raw))
	for i, opsRaw := range raw {
		ops := make([]otc.Operation, 0, len(opsRaw))
		for j, oRaw := range opsRaw {
			op, err := otc.OperationFromRaw(oRaw)
			if err != nil {
				return nil, errors.NewOError("invalid raw change operation",
					map[string]any{"change": i, "operation": j, "err": err.Error()})
			}
			ops = append(ops, op)
		}
		changes = append(changes, otc.NewChange(ops, time.Time{}, nil, nil, nil, nil, nil))
	}
	return changes, nil
}

// --- the BlobStore (Node: class BlobStore extends BlobStoreBase) --------------

// hrwBlobStore mirrors `class BlobStore extends BlobStoreBase`. It embeds
// otc.BaseBlobStore (so GetBlob/GetString/GetObject delegate and the
// EmptyHash short-circuit applies); FetchString carries the 3-attempt retry
// from Node.
type hrwBlobStore struct {
	*otc.BaseBlobStore
	historyID           string
	filestoreBlobPrefix string
	clsiPerfVariant     string
	globalBlobs         []string
}

func newHRWBlobStore(historyID, filestoreBlobPrefix, clsiPerfVariant string,
	globalBlobs []string,
) *hrwBlobStore {
	b := &hrwBlobStore{
		BaseBlobStore:       &otc.BaseBlobStore{},
		historyID:           historyID,
		filestoreBlobPrefix: filestoreBlobPrefix,
		clsiPerfVariant:     clsiPerfVariant,
		globalBlobs:         globalBlobs,
	}
	b.FetchString = b.fetchString
	return b
}

// getBlobURL mirrors BlobStore.getBlobURL (returns u.href).
func (b *hrwBlobStore) getBlobURL(hash string) string {
	base, _ := url.ParseRequestURI(config.Get().APIs.FileStore.URL)
	if b.filestoreBlobPrefix != "" {
		base.Path = b.filestoreBlobPrefix + "/" + hash
	} else if b.clsiPerfVariant != "" {
		base.Host = config.Get().APIs.Perf.Host
		base.Path = "/variant/" + b.clsiPerfVariant + "/hash/" + hash
	} else if contains(b.globalBlobs, hash) {
		base.Path = "/history/global/hash/" + hash
	} else {
		base.Path = "/history/project/" + b.historyID + "/hash/" + hash
	}
	return base.String()
}

// fetchString mirrors BlobStore.fetchString: 3 attempts, 3 s timeout per
// attempt, 100 ms between attempts, 404 -> Errors.NotFoundError (which otc
// BaseBlobStore wraps into BlobNotFound, mirroring Node's Blob.NotFoundError).
func (b *hrwBlobStore) fetchString(ctx context.Context, hash string) (string, error) {
	u := b.getBlobURL(hash)
	remainingAttempts := 3
	for {
		attemptCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		s, err := FetchStringFunc(attemptCtx, u)
		cancel()
		if err == nil {
			return s, nil
		}
		if rfe, ok := err.(*fetchutils.RequestFailedError); ok && rfe.Status == 404 {
			return "", errors.NewNotFoundError("")
		}
		remainingAttempts--
		if remainingAttempts <= 0 {
			return "", err
		}
		logger.Warn(map[string]any{"err": err.Error(), "url": u, "remainingAttempts": remainingAttempts},
			"compile from cache: history blob download failed")
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}
