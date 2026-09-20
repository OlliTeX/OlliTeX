// Package urlfetcher ports services/clsi/app/js/UrlFetcher.js.
//
// Node parity notes:
//
//   - pipeUrlToFileWithRetry(url, fallbackURL, filePath): 3 attempts, last
//     error returned on failure; a 404 on the primary URL falls back to
//     fallbackURL (RequestFailedError with response.status 404).
//   - If Settings.filestoreDomainOveride is set and the URL's host is not
//     apis.clsiPerf.host, the URL is rewritten to `<override>${pathname}${search}`.
//   - Atomic write: write to `filePath~`, then rename onto filePath. On
//     failure: unlink `filePath~` (ignore errors) and rethrow.
//   - inferSource: 'clsi-perf' if the URL includes apis.clsiPerf.host, else
//     'user-files' if it includes '/project/' and '/file/', else the bucket
//     name from the /bucket/<name>/key/ regex, else 'unknown'.
//
// The HTTP client is hand-rolled (zero/low-dep) and injectable so tests can
// serve from httptest or fake the reader entirely.
package urlfetcher

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"clsi/config"
	"clsi/logger"
)

const (
	// fetchTimeout mirrors AbortSignal.timeout(60 * 1000).
	fetchTimeout = 60 * time.Second
	// maxAttempts mirrors `remainingAttempts = 3`.
	maxAttempts = 3
)

// bucketRe mirrors the Node BUCKET_REGEX (/\/bucket\/([^/]+)\/key\//).
var bucketRe = regexp.MustCompile(`/bucket/([^/]+)/key/`)

// RequestFailedError mirrors @overleaf/fetch-utils RequestFailedError —
// a failed fetch, with the response (when present) carrying the status.
type RequestFailedError struct {
	Response *http.Response
	Err      error
}

func (e *RequestFailedError) Error() string {
	if e.Response != nil {
		return fmt.Sprintf("request failed: %d", e.Response.StatusCode)
	}
	if e.Err != nil {
		return fmt.Sprintf("request failed: %v", e.Err)
	}
	return "request failed"
}

// PipeUrlToFileWithRetry ports pipeUrlToFileWithRetry(url, fallbackURL, filePath).
func PipeUrlToFileWithRetry(urlStr, fallbackURL, filePath string) (err error) {
	var lastErr error
	for remaining := maxAttempts; remaining > 0; remaining-- {
		lastErr = pipeUrlToFile(urlStr, fallbackURL, filePath)
		if lastErr == nil {
			return nil
		}
		logger.Warn(map[string]any{"err": lastErr.Error(), "url": urlStr, "filePath": filePath, "remainingAttempts": remaining}, "error downloading url")
	}
	return lastErr
}

// pipeUrlToFile ports pipeUrlToFile(url, fallbackURL, filePath).
func pipeUrlToFile(urlStr, fallbackURL, filePath string) error {
	rewritten, ok := rewriteThroughOverride(urlStr)
	if ok {
		urlStr = rewritten
	}
	rewFallback := fallbackURL
	if fallbackURL != "" {
		if r, ok := rewriteThroughOverride(fallbackURL); ok {
			rewFallback = r
		}
	}

	reader, ferr := fetchReader(urlStr)
	if ferr != nil {
		if fallbackURL != "" {
			if rferr, ok := ferr.(*RequestFailedError); ok && rferr.Response != nil &&
				rferr.Response.StatusCode == 404 {
				reader, ferr = fetchReader(rewFallback)
				if ferr != nil {
					return ferr
				}
				urlStr = fallbackURL
			} else {
				return ferr
			}
		} else {
			return ferr
		}
	}
	defer reader.Close()

	atomicPath := filePath + "~"
	if cerr := copyToAtomic(reader, atomicPath); cerr != nil {
		return cerr
	}
	if rerr := os.Rename(atomicPath, filePath); rerr != nil {
		_ = os.Remove(atomicPath)
		return rerr
	}
	return nil
}

// copyToAtomic streams reader into filePath+".tmp" then renames it.
//
// This mirrors UrlFetcher's atomicWrite: it uses Go's os.Create on the atomic
// path. The trailing "~" is per Node: `filePath + '~'`.
func copyToAtomic(reader io.Reader, atomicPath string) (err error) {
	out, oerr := os.Create(atomicPath)
	if oerr != nil {
		return oerr
	}
	defer func() {
		out.Close()
		if err != nil {
			_ = os.Remove(atomicPath)
		}
	}()
	if _, werr := io.Copy(out, reader); werr != nil {
		return werr
	}
	return nil
}

// rewriteThroughOverride mirrors:
//
//	if (Settings.filestoreDomainOveride && u.host !== Settings.apis.clsiPerf.host)
//		url = `${Settings.filestoreDomainOveride}${u.pathname}${u.search}`
func rewriteThroughOverride(u string) (string, bool) {
	override := config.Get().FileStoreDomainOverride
	if override == "" {
		return u, false
	}
	p, err := url.Parse(u)
	if err != nil {
		return u, false
	}
	if p.Host == config.Get().APIs.Perf.Host {
		return u, false
	}
	search := ""
	if p.RawQuery != "" {
		search = "?" + p.RawQuery
	}
	return override + p.Path + search, true
}

// fetchReader is the injectable fetch: returns a reader of the body + the
// *http.Response (so 404s can be detected). Tests substitute this.
var fetchReader func(u string) (io.ReadCloser, error) = defaultFetchReader

// fetchURL performs the raw HTTP GET (60s timeout). Injected in tests.
var fetchURL func(u string) (*http.Response, error) = func(u string) (*http.Response, error) {
	client := &http.Client{Timeout: fetchTimeout}
	return client.Get(u)
}

// defaultFetchReader is the production fetchReader implementation: fetch,
// map errors, and return the body reader (a >=400 status is a 404-able
// failure via RequestFailedError).
func defaultFetchReader(u string) (io.ReadCloser, error) {
	resp, err := fetchURL(u)
	if err != nil {
		return nil, &RequestFailedError{Err: err}
	}
	if resp.StatusCode >= 400 {
		return nil, &RequestFailedError{Response: resp}
	}
	return resp.Body, nil
}

// InferSource ports inferSource(url).
func InferSource(urlStr string) string {
	perfHost := config.Get().APIs.Perf.Host
	if strings.Contains(urlStr, perfHost) {
		return "clsi-perf"
	} else if strings.Contains(urlStr, "/project/") && strings.Contains(urlStr, "/file/") {
		return "user-files"
	} else if strings.Contains(urlStr, "/key/") {
		m := bucketRe.FindStringSubmatch(urlStr)
		if m != nil {
			return m[1]
		}
	}
	return "unknown"
}
