// Package urlcache ports services/clsi/app/js/UrlCache.js.
//
// Node parity:
//
//   - getProjectDir(projectId) = <Settings.path.clsiCacheDir>/<projectId>
//   - getCachePath(projectId, url, lastModified):
//     mtime = (lastModified && lastModified.getTime()) || 0
//     key = new URL(url).pathname.replace(/\//g, '-') + '-' + mtime
//     return Path.join(getProjectDir(projectId), key)
//     Node's URL.pathname always starts with '/', so key starts with '-'.
//   - downloadUrlToFile: cachePath (or + '.opt' when conversionSuffix is set)
//     is copyFile'd into destPath (and the fallback URL path is tried too);
//     on a miss the original is downloaded to `<cachePath>.<conversionSuffix>`
//     and either the handle {conversionPath, cachePath, destPath} (suffix) is
//     returned or it is copied onto destPath (no suffix).
//   - download dedupes concurrent downloads of the same target path
//     (PENDING_DOWNLOADS map); UrlFetcher writes atomically.
//   - commitConversion: rename(conversionPath, cachePath), copyFile(cachePath,
//     destPath).
//   - isConversionCached: presence of the '.opt' entry means a conversion has
//     already been attempted for this content.
//   - clearProject: fs.rm(projectDir, {force: true, recursive: true}).
package urlcache

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"clsi/config"
	"clsi/logger"
	"clsi/urlfetcher"
)

var pendingMu sync.Mutex

type pendingEntry struct {
	done chan struct{}
	err  error
}

// pendingDownloads dedupes concurrent downloads of a targetPath (Node
// PENDING_DOWNLOADS) while a fetch is in flight.
var pendingDownloads = map[string]*pendingEntry{}

// FetchURL is the injectable download (UrlFetcher.pipeUrlToFileWithRetry).
// Defaults to the real urlfetcher.PipeUrlToFileWithRetry; tests substitute.
var FetchURL func(urlStr, fallbackURL, filePath string) (err error) = urlfetcher.PipeUrlToFileWithRetry

// GetProjectCacheDir ports getProjectDir(projectId).
func GetProjectCacheDir(projectID string) string {
	return config.Get().Path.ClsiCacheDir + "/" + projectID
}

// CreateProjectDir ports createProjectDir(projectId): mkdir <projectDir>
// recursive.
func CreateProjectDir(projectID string) error {
	return os.MkdirAll(GetProjectCacheDir(projectID), 0755)
}

// CachePath ports getCachePath(projectId, url, lastModified).
// Exported for tests + other packages (e.g. tikzmanager/png2pdf) which
// build the same cache keys.
func CachePath(projectID, urlStr string, lastModified *time.Time) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}
	mtime := int64(0)
	if lastModified != nil {
		mtime = lastModified.UnixMilli()
	}
	// Node: mtime = (lastModified && lastModified.getTime()) || 0
	key := strings.ReplaceAll(u.Path, "/", "-") + "-" + strconv.FormatInt(mtime, 10)
	return GetProjectCacheDir(projectID) + "/" + key, nil
}

// ConversionHandle is the { conversionPath, cachePath, destPath } triple Node
// returns for fresh downloads with a conversion suffix (cache-miss case).
type ConversionHandle struct {
	ConversionPath string
	CachePath      string
	DestPath       string
}

// DownloadUrlToFile ports downloadUrlToFile(projectId, url, fallbackURL,
// destPath, lastModified, conversionSuffix).
//
//   - cache hit (or fallback cache hit): the file is at destPath, returns (nil, nil)
//   - cache miss, conversionSuffix == "": download to cachePath, copy into
//     destPath, returns (nil, nil)
//   - cache miss, conversionSuffix != "": download to conversionPath,
//     returns the handle (the caller converts + CommitConversion)
func DownloadUrlToFile(projectID, urlStr, fallbackURL, destPath string, lastModified *time.Time, conversionSuffix string) (*ConversionHandle, error) {
	cachePath, err := CachePath(projectID, urlStr, lastModified)
	if err != nil {
		return nil, err
	}
	fullCachePath := cachePath
	if conversionSuffix != "" {
		fullCachePath += ".opt"
	}

	copied, cerr := tryCopyFile(fullCachePath, destPath)
	if !copied && fallbackURL != "" {
		fCache, ferr := CachePath(projectID, fallbackURL, lastModified)
		if ferr != nil {
			return nil, ferr
		}
		if conversionSuffix != "" {
			fCache += ".opt"
		}
		copied, cerr = tryCopyFile(fCache, destPath)
	}
	if cerr != nil {
		return nil, cerr
	}
	if copied {
		return nil, nil
	}

	conversionPath := fullCachePath + conversionSuffix
	if derr := download(urlStr, fallbackURL, conversionPath); derr != nil {
		return nil, derr
	}

	if conversionSuffix != "" {
		return &ConversionHandle{
			ConversionPath: conversionPath,
			CachePath:      fullCachePath,
			DestPath:       destPath,
		}, nil
	}

	if _, cerr := tryCopyFile(cachePath, destPath); cerr != nil {
		return nil, cerr
	}
	return nil, nil
}

// Download dedupes concurrent downloads of the same targetPath (Node:
// PENDING_DOWNLOADS), delegating to URLFetcher (injectable via FetchURL).
func download(urlStr, fallbackURL, targetPath string) error {
	f := FetchURL
	if f == nil {
		f = urlfetcher.PipeUrlToFileWithRetry
	}

	pendingMu.Lock()
	entry, ok := pendingDownloads[targetPath]
	if !ok {
		entry = &pendingEntry{done: make(chan struct{})}
		pendingDownloads[targetPath] = entry
	}
	pendingMu.Unlock()

	if ok {
		// in flight: share the first caller's outcome (Node `return pending`)
		<-entry.done
		return entry.err
	}

	defer func() {
		pendingMu.Lock()
		delete(pendingDownloads, targetPath)
		pendingMu.Unlock()
		close(entry.done)
	}()

	entry.err = f(urlStr, fallbackURL, targetPath)
	return entry.err
}

// CommitConversion ports commitConversion(conversionPath, cachePath, destPath).
func CommitConversion(conversionPath, cachePath, destPath string) error {
	if err := os.Rename(conversionPath, cachePath); err != nil {
		return err
	}
	if _, cerr := tryCopyFile(cachePath, destPath); cerr != nil {
		return cerr
	}
	return nil
}

// IsConversionCached ports isConversionCached(projectId, url, lastModified).
func IsConversionCached(projectID, urlStr string, lastModified *time.Time) (bool, error) {
	cachePath, err := CachePath(projectID, urlStr, lastModified)
	if err != nil {
		return false, err
	}
	optPath := cachePath + ".opt"
	if _, err := os.Stat(optPath); err != nil {
		if !os.IsNotExist(err) {
			logger.Warn(map[string]any{"err": err.Error(), "projectId": projectID, "url": urlStr, "cachePath": optPath}, "failure checking cache for converted file")
		}
		return false, nil
	}
	return true, nil
}

// ClearProject ports clearProject(projectId): rm(projectDir, {force, recursive: true}).
func ClearProject(projectID string) error {
	return os.RemoveAll(GetProjectCacheDir(projectID))
}

// tryCopyFile ports tryCopyFile(src, dest): (copied bool, err).
//   - src present: copy into dest (ensuring parent), return (true, nil)
//   - src absent (ENOENT): return (false, nil)
//   - other errors: return (false, err)
func tryCopyFile(src, dst string) (copied bool, err error) {
	data, rerr := os.ReadFile(src)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return false, nil
		}
		return false, rerr
	}
	dir := parentDir(dst)
	if dir != "" && dir != "." {
		if merr := os.MkdirAll(dir, 0755); merr != nil {
			return false, merr
		}
	}
	if werr := os.WriteFile(dst, data, 0644); werr != nil {
		return false, werr
	}
	return true, nil
}

// parentDir returns the directory part of a POSIX path (empty for a bare
// filename).
func parentDir(p string) string {
	last := strings.LastIndex(p, "/")
	if last <= 0 {
		return ""
	}
	return p[:last]
}
