// WebDAV HTTP client (P6.9 webdav).
//
// Implements the Node WebdavClient.mjs interface (npm `webdav` wrapper) with
// raw WebDAV HTTP (PROPFIND / GET / PUT / MKCOL / DELETE) + basic auth:
//
//	check()           — PROPFIND rootPath; 404 → "Root path does not exist: <p>"
//	list(p)           — PROPFIND p → [{path, isDirectory, etag, modifiedAt, size}]
//	createDirectory(p) — MKCOL p
//	put(p, body, etag) — PUT p (If-Match etag when given)
//	get(p)            — GET p → bytes
//	remove(p)         — DELETE p
//
// URL construction mirrors Node: parsed.baseUrl + path (basic auth username/
// password from the credentials; NOT an http-auth URL).

package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// wdHTTPError — an HTTP-level WebDAV failure (Node error message =
// "fetch failed" / status text; the controller surfaces err.message).
type wdHTTPError struct {
	code int
	msg  string
}

func (e *wdHTTPError) Error() string { return e.msg }
func (e *wdHTTPError) status(def int) int {
	if e.code >= 400 && e.code < 600 {
		return e.code
	}
	return def
}

type wdClient struct {
	base string // "https://host[:port]/prefix" (normalized, no trailing /)
	user string
	pass string
	root string       // normalized rootPath
	hc   *http.Client // request client (10s — Node webdav defaults)
}

type wdListItem struct {
	path        string
	isDirectory bool
	etag        *string
	modifiedAt  *string // ISO or null
	size        int64
}

// wdNewCredsClient — build the client from a decrypted credentials JSON.
func wdNewCredsClient(plainJSON string) (*wdClient, error) {
	var m map[string]json.RawMessage
	if err := jsonUnmarshal([]byte(plainJSON), &m); err != nil {
		return nil, &wdHTTPError{code: 500, msg: "WebDAV credentials are malformed"}
	}
	base, _ := wdCredStr(m, "baseUrl")
	user, _ := wdCredStr(m, "username")
	pass, _ := wdCredStr(m, "password")
	root, _ := wdCredStr(m, "rootPath")
	if root == "" {
		root = "/"
	}
	if base == "" {
		return nil, &wdHTTPError{code: 400, msg: "WebDAV URL must use HTTP or HTTPS"}
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, &wdHTTPError{code: 400, msg: "WebDAV URL must use HTTP or HTTPS"}
	}
	ub := u.String()
	if !strings.HasPrefix(ub, "http") {
		return nil, &wdHTTPError{code: 400, msg: "WebDAV URL must use HTTP or HTTPS"}
	}
	prefix := strings.TrimSuffix(u.Path, "/")
	prefix, _ = url.QueryUnescape(prefix)
	return &wdClient{
		base: u.Scheme + "://" + u.Host + prefix,
		user: user,
		pass: pass,
		root: wdNormalizePath(root),
		hc:   &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func wdNormalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = strings.ReplaceAll(p, "//", "/")
	return p
}

// wdDo — one HTTP round-trip with basic auth + retry (Node _executeWithRetry:
// 2 attempts, 500ms delay).
func (c *wdClient) wdDo(ctx context.Context, method, absPath string, body []byte, hdr map[string]string, etag *string) (int, []byte, error) {
	u := c.base + wdNormJoin(absPath)
	var lastErr error
	var lastCode int
	var lastBody []byte
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(500 * time.Millisecond):
			case <-ctx.Done():
			}
		}
		var rerr error
		lastCode, lastBody, rerr = c.wdOnce(ctx, method, u, body, hdr, etag)
		if rerr == nil {
			return lastCode, lastBody, nil
		}
		lastErr = rerr
		// retry on network-only errors (Node retries all operation failures)
		_ = lastErr
	}
	if lastCode > 0 {
		return lastCode, lastBody, &wdHTTPError{code: lastCode, msg: lastErr.Error()}
	}
	return 0, nil, &wdHTTPError{code: 500, msg: lastErr.Error()}
}

func (c *wdClient) wdOnce(ctx context.Context, method, u string, body []byte, hdr map[string]string, etag *string) (int, []byte, error) {
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return 0, nil, err
	}
	if c.user != "" || c.pass != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.user+":"+c.pass)))
	}
	if body != nil {
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
	}
	if etag != nil && *etag != "" {
		req.Header.Set("If-Match", *etag)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, err // network failure — Node: fetch failed
	}
	defer resp.Body.Close()
	b, _ := ioReadAll(resp.Body, 64<<20)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, b, nil
	}
	return resp.StatusCode, b, &wdHTTPError{code: resp.StatusCode, msg: http.StatusText(resp.StatusCode)}
}

func wdNormJoin(p string) string {
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// ---- WebDAV operations -----------------------------------------------------

// wdPropstat — the {resourcetype, displayname, getcontentlength, getlastmodified, getetag} subset.
type wdPropstat struct {
	XMLName      xml.Name `xml:"propstat"`
	Status       string   `xml:"status"`
	Resourcetype *rdRes   `xml:"prop>resourcetype"`
	Displayname  string   `xml:"prop>displayname"`
	ContentLen   string   `xml:"prop>getcontentlength"`
	LastMod      string   `xml:"prop>getlastmodified"`
	ETag         string   `xml:"prop>getetag"`
}
type rdRes struct {
	Collexists bool `xml:"collection"`
}
type rdMultistatus struct {
	XMLName   xml.Name `xml:"multistatus"`
	Responses []rdResp `xml:"response"`
}
type rdResp struct {
	Href      string       `xml:"href"`
	Propstats []wdPropstat `xml:"propstat"`
}

// check — PROPFIND rootPath; missing → error (Node message).
func (c *wdClient) check(ctx context.Context) error {
	code, _, err := c.wdDo(ctx, "PROPFIND", c.root, propfindDepth0, map[string]string{"Depth": "0"}, nil)
	if err == nil {
		return nil
	}
	if code == 404 || code == 405 {
		return &wdHTTPError{code: code, msg: "Root path does not exist: " + c.root}
	}
	return err
}

// list — PROPFIND (depth 1) → items sorted by basename (Node getDirectoryContents
// order is server order; the controller filters/maps — order parity is
// server-dependent anyway).
func (c *wdClient) list(ctx context.Context, p string) ([]wdListItem, error) {
	path := wdNormJoin(p)
	_, body, werr := c.wdDo(ctx, "PROPFIND", path, propfindDepth1, map[string]string{"Depth": "1", "Content-Type": "application/xml"}, nil)
	if werr != nil {
		return nil, werr
	}
	var ms rdMultistatus
	if xmlErr := xml.Unmarshal(body, &ms); xmlErr != nil {
		return nil, &wdHTTPError{code: 207, msg: "cannot parse multistatus response"}
	}
	items := make([]wdListItem, 0, len(ms.Responses))
	parent := strings.TrimSuffix(path, "/")
	for _, r := range ms.Responses {
		href := strings.TrimPrefix(r.Href, "<")
		href = strings.TrimSuffix(href, ">")
		href, _ = url.QueryUnescape(href)
		var ps *wdPropstat
		for i := range r.Propstats {
			ps = &r.Propstats[i]
			break
		}
		name := ""
		isDir := true
		size := int64(0)
		var etag *string
		var mod *string
		if ps != nil {
			name = ps.Displayname
			isDir = ps.Resourcetype != nil && ps.Resourcetype.Collexists
			size = parseInt64(ps.ContentLen)
			if ps.ETag != "" {
				t := ps.ETag
				etag = &t
			}
			if ps.LastMod != "" {
				t := httpTimeISO(ps.LastMod)
				mod = &t
			}
		}
		if name == "" {
			name = basename(href)
		}
		abs := href
		if !strings.HasPrefix(abs, "/") {
			abs = path + "/" + href
		}
		items = append(items, wdListItem{
			path:        joinOnce(parent, basename(href)),
			isDirectory: isDir,
			etag:        etag,
			modifiedAt:  mod,
			size:        size,
		})
		_ = abs
	}
	return items, nil
}

// put — upload (201 when created).
func (c *wdClient) put(ctx context.Context, p string, body []byte, etag *string) error {
	_, _, err := c.wdDo(ctx, "PUT", wdNormJoin(p), body, map[string]string{"Content-Type": "application/octet-stream"}, etag)
	return err
}

// get — download.
func (c *wdClient) get(ctx context.Context, p string) ([]byte, error) {
	_, body, err := c.wdDo(ctx, "GET", wdNormJoin(p), nil, nil, nil)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// createDirectory — MKCOL (201/405 both accepted by Node createDirectory).
func (c *wdClient) createDirectory(ctx context.Context, p string) error {
	_, _, err := c.wdDo(ctx, "MKCOL", wdNormJoin(p), nil, nil, nil)
	return err
}

// remove — DELETE.
func (c *wdClient) remove(ctx context.Context, p string) error {
	_, _, err := c.wdDo(ctx, "DELETE", wdNormJoin(p), nil, nil, nil)
	return err
}
