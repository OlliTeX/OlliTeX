package resourcewriter

import (
	stderrors "errors"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"clsi/logger"
	"clsi/metrics"
	"clsi/outputfilefinder"
	"clsi/resourcestatemanager"
	"clsi/urlcache"
)

// --- isExtraneousFile (Node exact) ---------------

var (
	reOutputDot   = regexp.MustCompile(`^output\.`)
	reAux         = regexp.MustCompile(`\.aux$`)
	reCacheDir    = regexp.MustCompile(`^cache/`)
	reOutputDash  = regexp.MustCompile(`^output-.*`)
	reTikzExt     = regexp.MustCompile(`\.(pdf|dpth|md5)$`)
	reMintedExt   = regexp.MustCompile(`\.(pygtex|pygstyle)$`)
	reMintedDir   = regexp.MustCompile(`(^|/)_minted(-[^/]+)?/`)
	reMarkdownExt = regexp.MustCompile(`\.md\.tex$`)
	reMarkdownDir = regexp.MustCompile(`(^|/)_markdown_[^/]+/`)
	reEpsConv     = regexp.MustCompile(`-eps-converted-to\.pdf$`)
)

// forceDeleteFiles mirrors the Node hard-coded exact-match list (output.tex
// is folded in at the end of the Node function with the same effect).
var forceDeleteFiles = map[string]bool{
	"output.tar.gz":     true,
	"output.synctex.gz": true,
	"output.pdfxref":    true,
	"output.pdf":        true,
	"output.dvi":        true,
	"output.log":        true,
	"output.xdv":        true,
	"output.stdout":     true,
	"output.stderr":     true,
	"output.tex":        true,
}

// IsExtraneousFile ports isExtraneousFile(path). Deletion-eligible = true.
// The precious branch is where the vendored minimatch port is consumed (CLSI
// default preciousFilePattern="" is inert).
func IsExtraneousFile(p string) bool {
	shouldDelete := true
	if reOutputDot.MatchString(p) || reAux.MatchString(p) || reCacheDir.MatchString(p) {
		// knitr cache
		shouldDelete = false
	}
	if reOutputDash.MatchString(p) {
		// Tikz cached figures (default case)
		shouldDelete = false
	}
	if reTikzExt.MatchString(p) {
		// Tikz cached figures (by extension)
		shouldDelete = false
	}
	if reMintedExt.MatchString(p) || reMintedDir.MatchString(p) {
		// minted files/directory
		shouldDelete = false
	}
	if reMarkdownExt.MatchString(p) || reMarkdownDir.MatchString(p) {
		// markdown files/directory
		shouldDelete = false
	}
	if reEpsConv.MatchString(p) {
		// Epstopdf generated files
		shouldDelete = false
	}
	// Keep additional precious files and directories.
	if shouldDelete {
		ensurePreciousMatcher()
		if PreciousFileMatcher != nil && PreciousFileMatcher.Match(p) {
			shouldDelete = false
		}
	}
	if forceDeleteFiles[p] {
		shouldDelete = true
	}
	return shouldDelete
}

// --- path guard (Node exact) ---------------

// ErrPathOutsideRoot (Node: `new Error('resource path is outside root
// directory')` — plain, NOT an OError: the Node test checks the message only
// and the CLSI error surface does not wrap this).
var ErrPathOutsideRoot = stderrors.New("resource path is outside root directory")

// CheckPath ports checkPath(basePath, resourcePath, cb) -> (path, err).
//
//	Node: const p = Path.normalize(Path.join(basePath, resourcePath))
//	      if (p.slice(0, basePath.length + 1) !== basePath + '/') -> error
//
// Go's path.Join replicates node path.join+normalize for the POSIX cases
// (verified: 'foo/baz/../../bar' -> 'bar'; 'foo/../foobar/baz' -> 'foobar/baz'
// — prefix-attack guard preserved).
func CheckPath(basePath, resourcePath string) (string, error) {
	joined := path.Join(basePath, resourcePath)
	if !strings.HasPrefix(joined, basePath+"/") {
		return "", ErrPathOutsideRoot
	}
	return joined, nil
}

// --- injectable seams ---------------

// DownloadFile seams the UrlCache.downloadUrlToFile call for remote
// resources (CLSI passes no conversion suffix).
var DownloadFile func(projectID, url, fallbackURL, destPath string, lastModified *time.Time) error

// WriteFile seams fs.writeFile for content-bearing resources.
var WriteFile func(filePath string, content []byte) error

// FindOutputFiles seams outputfilefinder (tests fake the listing).
var FindOutputFiles func(resources []outputfilefinder.Resource, directory string) (outputfilefinder.FindResult, error)

// ClearSeams resets all test seams to nil so the next call uses the
// real (Node-exact) default implementations.
func ClearSeams() {
	DownloadFile = nil
	WriteFile = nil
	FindOutputFiles = nil
}

// --- delete file ---------------

// DeleteFileIfNotDirectory ports _deleteFileIfNotDirectory(path, cb).
//
//	Node fs.stat: ENOENT -> cb() ; other stat err -> cb(err)
//	stat.isFile() -> fs.unlink (err -> log + cb(err))
//	else -> cb()
func DeleteFileIfNotDirectory(filePath string) (err error) {
	st, err := os.Stat(filePath)
	if err != nil {
		if stderrors.Is(err, os.ErrNotExist) {
			return nil
		}
		logger.Err(map[string]any{"err": err, "path": filePath},
			"error stating file in deleteFileIfNotDirectory")
		return err
	}
	if st.Mode().IsRegular() {
		if rerr := os.Remove(filePath); rerr != nil {
			logger.Err(map[string]any{"err": rerr, "path": filePath},
				"error removing file in deleteFileIfNotDirectory")
			return rerr
		}
	}
	return nil
}

// --- write one resource ---------------

// writeResourceToDisk ports _writeResourceToDisk(projectId, resource,
// basePath, cb):
//
//	checkPath -> mkdir(dirname(recursive)) ->
//	  resource.url != null:
//	    UrlCache.downloadUrlToFile(projectId, url, fallbackUrl, path, modified, cb)
//	    ERRORS ARE SWALLOWED: logger + Metrics.inc('download-failed');
//	    callback() with no error ("try and continue compiling even if http
//	    resource can not be downloaded at this time")
//	  else: fs.writeFile(path, resource.content, cb)  (errors PROPAGATE)
func writeResourceToDisk(projectID string, resource Resource, basePath string) (err error) {
	resolved, cerr := CheckPath(basePath, resource.Path)
	if cerr != nil {
		return cerr
	}
	if merr := os.MkdirAll(path.Dir(resolved), 0o755); merr != nil {
		return merr
	}
	if resource.URL != "" {
		url := resource.URL
		if resource.FallbackURL != "" {
			url = resource.FallbackURL
		}
		lastModified := resource.Modified
		download := DownloadFile
		if download == nil {
			download = func(projectID, u, fallbackURL, destPath string, lastModified *time.Time) error {
				_, derr := urlcache.DownloadUrlToFile(projectID, u, fallbackURL, destPath, lastModified, "")
				return derr
			}
		}
		if derr := download(projectID, url, resource.FallbackURL, resolved, lastModified); derr != nil {
			logger.Err(map[string]any{
				"err":         derr,
				"projectId":   projectID,
				"path":        resolved,
				"resourceUrl": resource.URL,
				"modified":    lastModified,
			}, "error downloading file for resources")
			metrics.IncDownloadFailed()
		}
		// "try and continue compiling even if http resource can not be
		// downloaded at this time" — callback() with no error.
		return nil
	}
	if WriteFile != nil {
		return WriteFile(resolved, resource.Content)
	}
	return os.WriteFile(resolved, resource.Content, 0o644)
}

// --- extraneous removal ---------------

// RemoveExtraneousFiles ports _removeExtraneousFiles(request, resources,
// basePath, cb) and returns the findOutputFiles result (AllEntries feeds the
// subsequent checkResourceFiles).
//
//	Node: findOutputFiles -> for each outputFile where isExtraneousFile(path):
//	      _deleteFileIfNotDirectory(basePath + '/' + path)  (async.series).
func RemoveExtraneousFiles(request *Request, resources []Resource, basePath string) (outputfilefinder.FindResult, error) {
	shouldSkip := metrics.ShouldSkipMetrics(request.MetricsPath)
	timer := metrics.NewTimer("unlink-output-files")
	f := FindOutputFiles
	if f == nil {
		f = func(res []outputfilefinder.Resource, dir string) (outputfilefinder.FindResult, error) {
			return outputfilefinder.FindOutputFiles(res, dir)
		}
	}
	result, ferr := f(toOFResources(resources), basePath)
	if ferr != nil {
		if !shouldSkip {
			timer.Done()
		}
		return result, ferr
	}
	for _, of := range result.OutputFiles {
		if IsExtraneousFile(of.Path) {
			if derr := DeleteFileIfNotDirectory(basePath + "/" + of.Path); derr != nil {
				if !shouldSkip {
					timer.Done()
				}
				return result, derr
			}
		}
	}
	if !shouldSkip {
		timer.Done()
	}
	return result, nil
}

func toOFResources(rs []Resource) []outputfilefinder.Resource {
	out := make([]outputfilefinder.Resource, 0, len(rs))
	for _, r := range rs {
		out = append(out, outputfilefinder.Resource{Path: r.Path})
	}
	return out
}

// --- save / create ---------------

// CreateDirectory ports _createDirectory(basePath): mkdir; EEXIST -> ok.
func CreateDirectory(basePath string) (err error) {
	if merr := os.Mkdir(basePath, 0o755); merr != nil {
		if stderrors.Is(merr, os.ErrExist) {
			return nil
		}
		logger.Debug(map[string]any{"err": merr, "dir": basePath}, "error creating directory")
		return merr
	}
	return nil
}

// SaveIncrementalResourcesToDisk ports saveIncrementalResourcesToDisk
// (Node parallelLimit(writes, parallelFileDownloads); CLSI =1 => sequential).
func SaveIncrementalResourcesToDisk(projectID string, resources []Resource, basePath string) (err error) {
	if cerr := CreateDirectory(basePath); cerr != nil {
		return cerr
	}
	for _, resource := range resources {
		if werr := writeResourceToDisk(projectID, resource, basePath); werr != nil {
			return werr
		}
	}
	return nil
}

// SaveAllResourcesToDisk ports saveAllResourcesToDisk.
func SaveAllResourcesToDisk(request *Request, basePath string) (err error) {
	if cerr := CreateDirectory(basePath); cerr != nil {
		return cerr
	}
	if _, rerr := RemoveExtraneousFiles(request, request.Resources, basePath); rerr != nil {
		return rerr
	}
	for _, resource := range request.Resources {
		if werr := writeResourceToDisk(request.ProjectID, resource, basePath); werr != nil {
			return werr
		}
	}
	return nil
}

// --- top-level sync ---------------

// SyncResourcesToDisk ports syncResourcesToDisk(request, basePath, cb) and
// returns the (resourceList, err) the fulfilled callback resolves to.
func SyncResourcesToDisk(request *Request, basePath string) (resourceList []Resource, err error) {
	if request.SyncType == "incremental" {
		logger.Debug(map[string]any{"projectId": request.ProjectID, "userId": request.UserID}, "incremental sync")
		prior, cerr := resourcestatemanager.CheckProjectStateMatches(request.SyncState, basePath)
		if cerr != nil {
			return nil, cerr
		}
		result, rerr := RemoveExtraneousFiles(request, toResources(prior), basePath)
		if rerr != nil {
			return nil, rerr
		}
		if cerr := resourcestatemanager.CheckResourceFiles(prior, result.AllEntries, basePath); cerr != nil {
			return nil, cerr
		}
		if werr := SaveIncrementalResourcesToDisk(request.ProjectID, request.Resources, basePath); werr != nil {
			return nil, werr
		}
		return toResources(prior), nil
	}
	logger.Debug(map[string]any{"projectId": request.ProjectID, "userId": request.UserID}, "full sync")
	if cerr := urlcache.CreateProjectDir(request.ProjectID); cerr != nil {
		return nil, cerr
	}
	if werr := SaveAllResourcesToDisk(request, basePath); werr != nil {
		return nil, werr
	}
	var state *string
	if request.SyncState != "" {
		s := request.SyncState
		state = &s
	}
	// Node: saveProjectState(state, resources, basePath); undefined state
	// clears the file (Go: nil => unlink semantics).
	if serr := resourcestatemanager.SaveProjectState(state, toRSM(request.Resources), basePath); serr != nil {
		return nil, serr
	}
	return request.Resources, nil
}

func toResources(rs []resourcestatemanager.Resource) []Resource {
	out := make([]Resource, 0, len(rs))
	for _, r := range rs {
		out = append(out, Resource{Path: r.Path})
	}
	return out
}

func toRSM(rs []Resource) []resourcestatemanager.Resource {
	out := make([]resourcestatemanager.Resource, 0, len(rs))
	for _, r := range rs {
		out = append(out, resourcestatemanager.Resource{Path: r.Path})
	}
	return out
}
