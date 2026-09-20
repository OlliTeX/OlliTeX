// Package resource ports bridge/resource/* (ResourceCache + UrlResourceCache)
// and the NingHttpClient facade used to fetch attachment URLs.
//
// UrlResourceCache mirrors Java 1:1:
//
//	cache key  = url with "token=[^&]*" replaced by "token=REMOVED"
//	known path = dbStore.getPathForURLInProject(project, cacheKey)
//	   null  -> fetch the URL, record fetchedUrls[url] = contents, path = newPath
//	   set   -> reuse fetchedUrls[url] if present, else read from the current
//	           commit's fileTable[path] (fetch if the entry is missing)
//	fetch      = GET the URL; the Content-Length header (first value) is checked
//	           against maxFileSize BEFORE the body is read; the final body length
//	           is also re-checked; then dbStore.addURLIndexForProject.
package resource

import (
	"io"
	"net/http"
	"regexp"
	"strconv"

	"ollitex/go/services/gitbridge/filestore"
	"ollitex/go/services/gitbridge/giterrors"
)

// ResourceCache — narrow interface over the URL fetcher (the Java
// mock seam `NingHttpClientFacade`).
//
//	headerCheck is invoked with the first Content-Length header value (""
//	when absent) before the body is read. Returning an error (e.g. a
//	SizeLimitExceededException) aborts the fetch; the returned error is the
//	fetch's error.
//
// The returned contents are the whole response body. A non-2xx/3xx status
// or transport failure yields a *giterrors.FailedConnectionException.
type ByteGetter func(url string, headerCheck func(contentLength string) error) (contents []byte, err error)

// DefaultByteGetter performs a real HTTP GET (Java: NingHttpClient.get).
func DefaultByteGetter(client *http.Client) ByteGetter {
	return func(url string, headerCheck func(contentLength string) error) (contents []byte, err error) {
		resp, e := client.Get(url)
		if e != nil {
			return nil, &giterrors.FailedConnectionException{}
		}
		defer resp.Body.Close()
		// Java (Ning): statusCode >= 400 -> "got status N fetching url".
		if resp.StatusCode >= 400 {
			return nil, &giterrors.FailedConnectionException{}
		}
		// Java: handler applied on the HttpHeaders; a thrown
		// SizeLimitExceededException aborts the fetch and propagates.
		if headerCheck != nil {
			if e := headerCheck(resp.Header.Get("Content-Length")); e != nil {
				return nil, e
			}
		}
		body, e := io.ReadAll(resp.Body)
		if e != nil {
			return nil, &giterrors.FailedConnectionException{}
		}
		return body, nil
	}
}

// ---------------------------------------------------------------------------
// ResourceCache (port of bridge/resource/ResourceCache)
// ---------------------------------------------------------------------------

// ResourceCache is the lookup contract for resolving an attachment URL to
// file contents (Java: the interface of the same name).
//
//	maxFileSize nil == Java Optional.empty (no limit).
type ResourceCache interface {
	Get(projectName, url, newPath string, fileTable map[string]filestore.RawFile, fetchedUrls map[string][]byte, maxFileSize *int64) (filestore.RawFile, error)
}

// httpDB is the narrow DB seam (Java: the DBStore mock). *db.SqliteDBStore
// satisfies it.
type httpDB interface {
	GetPathForURLInProject(projectName, url string) (string, bool)
	AddURLIndexForProject(projectName, url, path string)
}

// UrlResourceCache ports bridge/resource/UrlResourceCache.
type UrlResourceCache struct {
	dbStore httpDB
	byteGet ByteGetter
}

// NewUrlResourceCache builds the cache with the default HTTP byte getter
// (Java: `new UrlResourceCache(dbStore)` -> a real AsyncHttpClient under it).
func NewUrlResourceCache(dbStore httpDB) *UrlResourceCache {
	return &UrlResourceCache{dbStore: dbStore, byteGet: DefaultByteGetter(&http.Client{})}
}

// NewUrlResourceCacheWithGetter injects the byte getter (the Java test seam:
// `new UrlResourceCache(db, http)` where http is a mocked facade).
func NewUrlResourceCacheWithGetter(dbStore httpDB, byteGet ByteGetter) *UrlResourceCache {
	return &UrlResourceCache{dbStore: dbStore, byteGet: byteGet}
}

// Get ports UrlResourceCache.get 1:1.
func (c *UrlResourceCache) Get(projectName, url, newPath string, fileTable map[string]filestore.RawFile, fetchedUrls map[string][]byte, maxFileSize *int64) (filestore.RawFile, error) {
	path, known := c.dbStore.GetPathForURLInProject(projectName, getCacheKeyFromUrl(url))
	var contents []byte
	if !known {
		// path == null (fresh URL): fetch and remember.
		fetched, err := c.fetch(projectName, url, newPath, maxFileSize)
		if err != nil {
			return nil, err
		}
		// NOTE: plain assignment — Java has no := shadowing; the old
		// `contents, err := c.fetch(...)` declared a block-scoped shadow and
		// the final NewRepositoryFile call saw the nil outer variable
		// (silent 0-byte attachment file).
		contents = fetched
		fetchedUrls[url] = contents
	} else {
		if knownContents, ok := fetchedUrls[url]; ok {
			contents = knownContents
		} else {
			if rawFile, ok := fileTable[path]; ok {
				// Java: read it straight from the current commit's file table.
				contents = rawFile.GetContents()
			} else {
				// Java Log.warn path: not a git object yet, fetch instead.
				fetched, err := c.fetch(projectName, url, path, maxFileSize)
				if err != nil {
					return nil, err
				}
				contents = fetched
			}
		}
	}
	return filestore.NewRepositoryFile(newPath, contents), nil
}

// Java: fetch(...) — Content-Length pre-check, body length post-check,
// URL index update.
func (c *UrlResourceCache) fetch(projectName, url, path string, maxFileSize *int64) ([]byte, error) {
	headerCheck := func(contentLength string) error {
		if maxFileSize == nil {
			return nil // Java: !maxFileSize.isPresent()
		}
		if contentLength == "" {
			return nil // Java: contentLengths.isEmpty()
		}
		cl, err := strconv.ParseInt(contentLength, 10, 64)
		if err != nil {
			// Java: Long.parseLong throws NumberFormatException (unhandled).
			// Treat a broken header as "no length".
			return nil
		}
		if cl > *maxFileSize {
			return &giterrors.SizeLimitExceededException{Path: path, ActualSize: cl, MaxSize: *maxFileSize}
		}
		return nil
	}
	contents, err := c.byteGet(url, headerCheck)
	if err != nil {
		if _, ok := err.(*giterrors.SizeLimitExceededException); ok {
			return nil, err // Java: rethrow the SizeLimit cause
		}
		// Java: any other ExecutionException -> FailedConnectionException.
		return nil, &giterrors.FailedConnectionException{}
	}
	if maxFileSize != nil && int64(len(contents)) > *maxFileSize {
		return nil, &giterrors.SizeLimitExceededException{Path: path, ActualSize: int64(len(contents)), MaxSize: *maxFileSize}
	}
	c.dbStore.AddURLIndexForProject(projectName, getCacheKeyFromUrl(url), path)
	return contents, nil
}

// getCacheKeyFromUrl ports UrlResourceCache.getCacheKeyFromUrl.
var cacheKeyTokenRe = regexp.MustCompile(`token=[^&]*`)

func getCacheKeyFromUrl(url string) string {
	return cacheKeyTokenRe.ReplaceAllString(url, "token=REMOVED")
}
