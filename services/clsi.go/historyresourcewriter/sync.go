package historyresourcewriter

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"time"

	"clsi/config"
	"clsi/draftmodemanager"
	"clsi/errors"
	"clsi/logger"
	"clsi/metrics"
	"clsi/tikzmanager"
	"clsi/urlcache"

	"ollitex/go/libraries/otc"
)

// SyncResourcesToDisk mirrors Node syncResourcesToDisk 1:1: load the cached
// snapshot (or fall back to the remote one), apply the pending changes,
// sweep extraneous disk entries, load files eagerly, and write every changed
// resource into the compileDir (downloads go through the URL cache; slow PNGs
// through the png2pdf conversion path).
//
// timings/stats mirror Node's Record<string, number> maps; the compilemanager
// owns them and passes them into the compile request.
func SyncResourcesToDisk(ctx context.Context, projectID, userID string,
	request *Request, compileDir string, timings, stats map[string]any,
) (*Result, error) {
	// - logged in user: <project-id>-<user-id>
	// - anonymous user: <project-id>
	// - conversion job: <uuid>
	cacheKey := filepath.Base(compileDir)
	remoteBaseVersion := request.BaseHistoryVersion

	loaded, loadErr := loadSnapshot(projectID, userID, cacheKey, remoteBaseVersion,
		request.PopulateClsiCache)
	var (
		rawSnapshot      map[string]any
		globalBlobs      []string
		localBaseVersion int
		source           string
		dirty            []string
		fullSync         bool
		lastPng2pdf      *bool
	)
	localKnown := true // the loaded snapshot recorded a numeric localBaseVersion
	if loadErr == nil {
		rawSnapshot = loaded.RawSnapshot
		globalBlobs = loaded.GlobalBlobs
		localBaseVersion = loaded.LocalBaseVersion
		fullSync = loaded.FullSync
		dirty = loaded.Dirty
		lastPng2pdf = loaded.LastPng2pdf
		source = "local"
		if fullSync {
			source = "clsi-cache"
		}
		logger.Debug(map[string]any{
			"projectId":         projectID,
			"userId":            userID,
			"cacheKey":          cacheKey,
			"localBaseVersion":  localBaseVersion,
			"remoteBaseVersion": remoteBaseVersion,
		}, "compile from cache: using existing snapshot")
	} else {
		if request.RawSnapshot == nil {
			return nil, loadErr
		}
		// Node: log when the local miss was not a plain MissingUpdatesError
		// (a corrupt cached snapshot).
		if !errors.IsMissingUpdates(loadErr) {
			logger.Warn(map[string]any{"err": loadErr.Error(), "projectId": projectID, "userId": userID, "cacheKey": cacheKey},
				"compile from cache: bad local history state during full resync")
		}
		logger.Debug(map[string]any{"projectId": projectID, "userId": userID, "cacheKey": cacheKey},
			"compile from cache: using incoming snapshot")
		source = "remote"
		localBaseVersion = remoteBaseVersion
		rawSnapshot = request.RawSnapshot
		globalBlobs = []string{}
		dirty = []string{}
		fullSync = true
		lastPng2pdf = nil
	}

	globalBlobs = dedupeGlobalBlobs(globalBlobs, request.GlobalBlobs)

	snapshot, err := otc.SnapshotFromRaw(rawSnapshot)
	if err != nil {
		return nil, errors.NewOError("invalid snapshot",
			map[string]any{"err": err.Error()})
	}

	// Node: `request.rawChangeOperations.slice(localBaseVersion - remoteBaseVersion)`.
	// JS slice: a negative start counts from the end; NaN (an undefined, i.e.
	// non-numeric localBaseVersion) coerces to 0 (the whole window); an
	// out-of-range start yields the empty list. After load, a numeric local is
	// always >= remote (load throws MissingUpdates otherwise); a missing
	// numeric local (Node: undefined) maps to start 0.
	rawChangeOperations := request.RawChangeOperations
	changeStart := 0
	if localKnown {
		changeStart = localBaseVersion - remoteBaseVersion
	}
	if changeStart < 0 {
		changeStart += len(rawChangeOperations) // JS: negative start counts from the end
	}
	if changeStart < 0 {
		changeStart = 0
	}
	if changeStart > len(rawChangeOperations) {
		changeStart = len(rawChangeOperations)
	}
	changes, err := changesFromRawChangeOperations(rawChangeOperations[changeStart:])
	if err != nil {
		return nil, errors.NewOError("invalid change operation", map[string]any{"err": err.Error()})
	}
	applyAllStart := time.Now()
	// Node: snapshot.applyAll(changes) — no strict opts passed, so recoverable
	// errors (EditMissingFileError/FileNotFoundError on historical bad data)
	// are ignored, mirroring the Node default.
	if err := snapshot.ApplyAll(changes, false); err != nil {
		return nil, errors.NewOError("applyAll", map[string]any{"err": err.Error()})
	}
	applyAllMs := int64(time.Since(applyAllStart).Milliseconds())
	timings["snapshotApplyAll"] = applyAllMs
	if !metrics.ShouldSkipMetrics(request.MetricsPath) {
		metrics.SnapshotApplyAllDurationSeconds.Observe(
			request.CompileGroup, source,
			float64(applyAllMs)/(1_000))
	}

	entries := &entryList{isDir: map[string]bool{}}
	if err := discoverExistingEntries(compileDir, ".", entries); err != nil {
		return nil, err
	}
	if err := removeExtraneousEntries(compileDir,
		func(p string) bool { return snapshot.GetFile(p) != nil }, entries); err != nil {
		return nil, err
	}

	pngModeChanged := lastPng2pdf == nil || *lastPng2pdf != request.Png2pdf

	blobStore := newHRWBlobStore(request.HistoryID, request.FilestoreBlobPrefix,
		request.ClSIPerfVariant, globalBlobs)

	png2pdfActive := request.Png2pdf && Png2PdfEnabled()

	// Decide which PNGs to convert. A PNG is converted when a previous compile
	// flagged it as "slow" and it is large enough to be worth converting;
	// keeping that decision here means the sync loop below just checks
	// membership. Generated for every project for analytics, filtered to only
	// request.Png2pdf && Png2PdfEnabled() once rollout completes.
	slowPngs := loadSlowPngList(cacheKey)
	shouldConvert := map[string]struct{}{}
	for _, p := range snapshot.GetFilePathnames() {
		if !isPng(p) || !inStrList(slowPngs, p) {
			continue
		}
		// Avoid doing unnecessary work converting small PNGs.
		fileSize := int64(0)
		file := snapshot.GetFile(p)
		if file != nil {
			if bl := file.GetByteLength(); bl != nil {
				fileSize = *bl
			}
		}
		if fileSize < int64(config.Get().Png2pdfMinFileSizeBytes) {
			metrics.IncPng2pdfSkippedSmall()
			continue
		}
		shouldConvert[p] = struct{}{}
	}

	// for analytics to determine if a project could have converted PNG's,
	// even if they aren't in the rollout
	if len(shouldConvert) > 0 {
		stats["optimisable-png-count"] = len(shouldConvert)
		stats["projectHasUnconvertedPngs"] = 1
	}

	// only actually convert if png2pdf was enabled and user compile is eligible
	if !png2pdfActive {
		shouldConvert = map[string]struct{}{}
	}

	// On a png2pdf mode switch, also re-serve PNGs that a previous compile
	// already optimised. Once converted, a PNG is no longer flagged slow (it
	// is included as a PDF), so it drops off the slow-list; without this it
	// would revert to the original when toggling png2pdf off and back on. The
	// optimised variant is served from the <cachePath>.opt cache, so this is a
	// cheap local cache stat.
	if png2pdfActive && pngModeChanged {
		for _, p := range snapshot.GetFilePathnames() {
			if !isPng(p) {
				continue
			}
			if _, done := shouldConvert[p]; done {
				continue
			}
			if !reservableFromOptCache(projectID, p, snapshot, blobStore) {
				continue
			}
			shouldConvert[p] = struct{}{}
		}
	}

	changedPaths := []string{}
	if fullSync {
		changedPaths = changedPathsFromSnapshot(snapshot)
		logger.Debug(map[string]any{"projectId": projectID, "userId": userID, "cacheKey": cacheKey},
			"compile from cache: full sync")
	} else {
		dedupe := map[string]struct{}{}
		for _, d := range dirty {
			dedupe[d] = struct{}{}
		}
		if request.Draft {
			dedupe[request.RootResourcePath] = struct{}{}
		}
		if pngModeChanged {
			// When the png2pdf mode changed since the last sync, the on-disk
			// images are in the wrong variant (optimized vs original).
			// Re-sync them so they are converted (served from the <cachePath>.opt
			// cache) or restored to the original png.
			for _, p := range snapshot.GetFilePathnames() {
				if isPng(p) {
					dedupe[p] = struct{}{}
				}
			}
		}
		for _, change := range changes {
			for i := range change.Operations {
				switch op := change.Operations[i].(type) {
				case *otc.AddFileOperation:
					dedupe[op.GetPathname()] = struct{}{}
				case *otc.MoveFileOperation:
					dedupe[op.GetPathname()] = struct{}{}
					if !op.IsRemoveFile() {
						dedupe[op.GetNewPathname()] = struct{}{}
					}
				case *otc.EditFileOperation:
					dedupe[op.GetPathname()] = struct{}{}
				}
			}
		}
		// Restore deleted files
		for _, p := range snapshot.GetFilePathnames() {
			if !entries.has(p) {
				dedupe[p] = struct{}{}
			}
		}
		// Include PNGs known to be slow for png2pdf conversion. The presence of
		// an optimised (.opt) cache entry means the conversion was already
		// attempted (success or failure), so we skip those and never retry.
		// The .opt cache is keyed by content hash, so a new PNG at the same
		// path has no entry and is attempted.
		for p := range shouldConvert {
			if _, in := dedupe[p]; in {
				continue
			}
			file := snapshot.GetFile(p)
			if file == nil {
				continue
			}
			hash := file.GetHash()
			if hash == nil {
				continue
			}
			u := blobStore.getBlobURL(*hash)
			attempted, _ := IsConversionCached(projectID, u, epochZero())
			if !attempted {
				dedupe[p] = struct{}{}
			}
		}
		changedPaths = sortedKeys(dedupe)
		logger.Debug(map[string]any{"projectId": projectID, "userId": userID, "cacheKey": cacheKey, "changedPaths": changedPaths},
			"compile from cache: incremental sync")
	}

	loadEagerStart := time.Now()
	// otc LoadFiles swallows per-file errors (Node: fileMap.mapAsync with
	// file.load's catch-through — a missing blob leaves the file hollow).
	_ = snapshot.LoadFiles(ctx, "eager", blobStore)
	loadEagerMs := int64(time.Since(loadEagerStart).Milliseconds())
	timings["snapshotLoadEager"] = loadEagerMs
	if !metrics.ShouldSkipMetrics(request.MetricsPath) {
		metrics.SnapshotLoadEagerDurationSeconds.Observe(
			request.CompileGroup, source,
			float64(loadEagerMs)/(1_000))
	}

	for _, p := range changedPaths {
		if snapshot.GetFile(p) == nil {
			continue // deleted, handled by removeExtraneousEntries
		}
		if err := ensureHasParentFolder(compileDir, p, entries); err != nil {
			return nil, err
		}
	}

	wasDirty := len(dirty) > 0
	dirty = []string{}
	createCacheFolderOnce := false
	var pngFilesToConvert []urlcache.ConversionHandle
	var firstWriteErr error
	for _, p := range changedPaths {
		file := snapshot.GetFile(p)
		if file == nil {
			continue // deleted, handled by removeExtraneousEntries
		}
		if content := file.GetContent(true); content != nil {
			if p == request.RootResourcePath {
				if request.Draft {
					content = new(draftmodemanager.PREFIX + *content)
					dirty = append(dirty, p)
				}
				if err := WriteOutputFileIfNeeded(compileDir,
					snapshot.GetFile(tikzmanager.OutputTex) != nil, *content); err != nil {
					// Node: every rejection in the settled per-path task is
					// rethrown as OError.tag(err, 'write failed', {path}).
					firstWriteErr = errors.Tag(err, "write failed", map[string]any{"path": p})
					break
				}
			}
			if err := writeString(filepath.Join(compileDir, p), *content); err != nil {
				firstWriteErr = errors.Tag(err, "write failed", map[string]any{"path": p})
				break
			}
		} else {
			hash := file.GetHash()
			if hash == nil {
				firstWriteErr = errors.Tag(
					errors.NewOError("unexpected file without content and hash",
						map[string]any{"path": p}),
					"write failed", map[string]any{"path": p})
				break
			}
			if !createCacheFolderOnce {
				createCacheFolderOnce = true
				// Node: createProjectDir rejection rides the same settled
				// 'write failed', {path} tag as everything else.
				if err := CreateProjectDir(projectID); err != nil {
					firstWriteErr = errors.Tag(err, "write failed", map[string]any{"path": p})
					break
				}
			}
			u := blobStore.getBlobURL(*hash)
			destPath := filepath.Join(compileDir, p)
			const fallbackURL = ""      // no fallback
			lastModified := epochZero() // content is static
			// Node: one try/catch around BOTH download branches — a failed
			// download (conversion or not) logs + metrics.inc('download-failed')
			// and the loop continues (the settled result is fulfilled).
			var dlErr error
			// PNGs selected for conversion go through a batch conversion
			// process first (see shouldConvert above).
			if _, toConvert := shouldConvert[p]; toConvert {
				handle, dlErr := DownloadUrlToFile(projectID, u, fallbackURL, destPath,
					lastModified, cacheKey)
				if dlErr == nil && handle != nil {
					pngFilesToConvert = append(pngFilesToConvert, *handle)
				}
			} else {
				_, dlErr = DownloadUrlToFile(projectID, u, fallbackURL, destPath,
					lastModified, "")
			}
			if dlErr != nil {
				logger.Err(map[string]any{"err": dlErr.Error(), "projectId": projectID, "path": p, "resourceUrl": u},
					"error downloading file for resources")
				metrics.IncDownloadFailed()
			}
		}
	}
	if firstWriteErr != nil {
		return nil, firstWriteErr
	}

	if len(pngFilesToConvert) > 0 {
		cacheDir := GetProjectCacheDir(projectID)
		relativePaths := make([]string, len(pngFilesToConvert))
		for i, f := range pngFilesToConvert {
			rel, _ := filepath.Rel(cacheDir, f.ConversionPath)
			relativePaths[i] = rel
		}
		if err := PngConvert(projectID, cacheDir, relativePaths, stats, timings); err != nil {
			logger.Warn(map[string]any{"err": err.Error(), "projectId": projectID, "userId": userID, "count": len(pngFilesToConvert)},
				"png2pdf conversion failed, using original png(s)")
		}
		for _, f := range pngFilesToConvert {
			if err := CommitConversion(f.ConversionPath, f.CachePath, f.DestPath); err != nil {
				logger.Err(map[string]any{"err": err.Error(), "projectId": projectID, "userId": userID, "path": f.DestPath},
					"error copying file for resources")
				metrics.IncDownloadFailed()
			}
		}
	}

	baseHistoryVersion := localBaseVersion + len(changes)
	if fullSync || len(changes) > 0 || wasDirty || len(dirty) > 0 || pngModeChanged {
		if err := saveSnapshot(cacheKey, snapshot.ToRaw(), baseHistoryVersion,
			globalBlobs, dirty, request.Png2pdf); err != nil {
			return nil, err
		}
	}
	if fullSync {
		deleteResyncSnapshot(projectID, userID, cacheKey)
	}
	return &Result{
		BaseHistoryVersion: baseHistoryVersion,
		ResourceList:       resourceListFromSnapshot(snapshot),
	}, nil
}

func dedupeGlobalBlobs(global, incoming []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, s := range global {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	for _, s := range incoming {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func changedPathsFromSnapshot(snapshot *otc.Snapshot) []string {
	return snapshot.GetFilePathnames()
}

func resourceListFromSnapshot(snapshot *otc.Snapshot) []Resource {
	out := []Resource{}
	for _, p := range snapshot.GetFilePathnames() {
		out = append(out, Resource{Path: p})
	}
	return out
}

func sortedKeys(in map[string]struct{}) []string {
	out := make([]string, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func epochZero() *time.Time {
	t := time.Unix(0, 0)
	return &t
}

func inStrList(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// reservableFromOptCache mirrors the Node mode-switch re-serve check:
// hash + .opt cache stat.
func reservableFromOptCache(projectID string, p string, snapshot *otc.Snapshot,
	blobStore *hrwBlobStore,
) bool {
	file := snapshot.GetFile(p)
	if file == nil {
		return false
	}
	hash := file.GetHash()
	if hash == nil {
		return false
	}
	u := blobStore.getBlobURL(*hash)
	cached, err := IsConversionCached(projectID, u, epochZero())
	return cached && err == nil
}

func writeString(file string, content string) error {
	return os.WriteFile(file, []byte(content), 0o644)
}
