package webdavinterface

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// WebDAVClient performs the WebDAV protocol operations. It is the 1:1 Go port
// of services/webdavinterface/app/src/WebDAVClient.mjs.
type WebDAVClient struct {
	Cfg      WebDAVConfig
	BaseURL  string
	Username string
	Password string
}

// NewWebDAVClient validates auth + URL (1:1 with the node constructor).
func NewWebDAVClient(cfg WebDAVConfig, baseURL, username, password string) (*WebDAVClient, error) {
	if username == "" || password == "" {
		return nil, errors.New("Missing authentication credentials")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("WebDAV URL must use HTTP or HTTPS")
	}
	cfg.withDefaults()
	base := strings.TrimSuffix(u.Scheme+"://"+u.Host+u.Path, "/")
	return &WebDAVClient{Cfg: cfg, BaseURL: base, Username: username, Password: password}, nil
}

// do performs one upstream request with Basic auth.
func (c *WebDAVClient) do(ctx context.Context, method, u string, header http.Header, body []byte) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	if header != nil {
		for k, vs := range header {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}
	}
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	return c.Cfg.HTTPClient.Do(req)
}

// executeWithRetry runs op with exponential backoff on 423/502/503/504 (1:1).
func (c *WebDAVClient) executeWithRetry(ctx context.Context, op func() error) error {
	var last error
	for attempt := 0; attempt <= c.Cfg.MaxRetries; attempt++ {
		err := op()
		if err == nil {
			return nil
		}
		last = err
		st := webdavStatusOf(err)
		if !isRetryable(st) {
			return err
		}
		delay := time.Duration(c.Cfg.RetryDelayMs*int(1<<uint(attempt))) * time.Millisecond
		if c.Cfg.Sleep != nil {
			c.Cfg.Sleep(delay)
		} else {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return last
}

// closeErr closes a response body and classifies a non-2xx status as a
// *WebDAVError carrying the provider status.
func collectStatus(resp *http.Response, allow ...int) (body []byte, err error) {
	ok := false
	for _, a := range allow {
		if resp.StatusCode == a {
			ok = true
			break
		}
	}
	defer resp.Body.Close()
	b, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return nil, rerr
	}
	if !ok {
		return b, webdavStatus(resp.StatusCode, "WebDAV request failed: "+resp.Status)
	}
	return b, nil
}

// Check lists the root (1:1 with node check()).
func (c *WebDAVClient) Check(ctx context.Context) error {
	return c.executeWithRetry(ctx, func() error {
		_, _, err := c.propfind(ctx, "/")
		return err
	})
}

// List returns the normalized directory entries for path (1:1).
func (c *WebDAVClient) List(ctx context.Context, resourcePath string) ([]WebDAVEntry, error) {
	var entries []WebDAVEntry
	if err := c.executeWithRetry(ctx, func() error {
		raw, _, perr := c.propfind(ctx, resourcePath)
		if perr != nil {
			return perr
		}
		entries = buildEntries(resourcePath, parseMultistatus(raw))
		return nil
	}); err != nil {
		return nil, err
	}
	return entries, nil
}

// buildEntries maps parsed multistatus entries to normalized paths (1:1 with
// node list()).
func buildEntries(resourcePath string, entries []WebDAVEntry) []WebDAVEntry {
	parent := strings.TrimSuffix(resourcePath, "/")
	if parent == "" {
		parent = "/"
	}
	for i := range entries {
		base := entries[i].Href
		if base == "" {
			base = path.Base(uPath(resourcePath))
		}
		// Node: path = filename || `${parent}/${basename}` collapsed.
		full := parent + "/" + base
		if !strings.HasPrefix(full, "/") {
			full = "/" + full
		}
		entries[i].Path = collapseSlashes(full)
	}
	return entries
}

// Download returns the base64 file content (1:1).
func (c *WebDAVClient) Download(ctx context.Context, resourcePath string) (string, error) {
	var out string
	if err := c.executeWithRetry(ctx, func() error {
		b, derr := c.downloadRaw(ctx, resourcePath)
		if derr != nil {
			return derr
		}
		out = base64.StdEncoding.EncodeToString(b)
		return nil
	}); err != nil {
		return "", err
	}
	return out, nil
}

func (c *WebDAVClient) downloadRaw(ctx context.Context, resourcePath string) ([]byte, error) {
	u := c.url(resourcePath)
	resp, err := c.do(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return nil, rerr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return b, webdavStatus(resp.StatusCode, "WebDAV download failed")
	}
	return b, nil
}

// Upload creates the parent dir (recursive) then PUTs content (1:1, incl.
// If-Match etag, 404-retry mkcol+put, retriable retries).
func (c *WebDAVClient) Upload(ctx context.Context, resourcePath, contentBase64 string, etag string) error {
	content, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return err
	}
	parent := "/"
	if idx := strings.LastIndex(resourcePath, "/"); idx > 0 {
		parent = resourcePath[:idx]
	}
	return c.executeWithRetry(ctx, func() error {
		_ = c.mkcCol(ctx, parent)
		perr := c.put(ctx, resourcePath, content, etag)
		if st := webdavStatusOf(perr); st == 404 {
			_ = c.mkcCol(ctx, parent)
			perr = c.put(ctx, resourcePath, content, etag)
		}
		if st := webdavStatusOf(perr); st == 412 || reConflict.MatchString(stringErr(perr)) {
			return webdavStatus(412, "Upload conflict: ETag mismatch")
		}
		return perr
	})
}

// Delete removes a file; 404 -> success with notFound (1:1).
func (c *WebDAVClient) Delete(ctx context.Context, resourcePath string) (notFound bool, err error) {
	var nf bool
	if e := c.executeWithRetry(ctx, func() error {
		resp, rerr := c.do(ctx, http.MethodDelete, c.url(resourcePath), nil, nil)
		if rerr != nil {
			return rerr
		}
		b, cErr := collectStatus(resp, 200, 204)
		_ = b
		if cErr != nil {
			if webdavStatusOf(cErr) == 404 {
				nf = true
				return nil
			}
			return cErr
		}
		return nil
	}); e != nil {
		return false, e
	}
	return nf, nil
}

// Move moves src -> dst with overwrite (1:1).
func (c *WebDAVClient) Move(ctx context.Context, source, destination string) error {
	return c.executeWithRetry(ctx, func() error {
		h := http.Header{}
		h.Set("Destination", c.url(destination))
		resp, err := c.do(ctx, "MOVE", c.url(source), h, nil)
		if err != nil {
			return err
		}
		_, cErr := collectStatus(resp, 200, 201, 204)
		return cErr
	})
}

// CreateDirectory MKCOLs a path; 405/ALREADY_EXISTS -> created=false (1:1).
func (c *WebDAVClient) CreateDirectory(ctx context.Context, resourcePath string) (created bool, err error) {
	var createdFlag bool
	if e := c.executeWithRetry(ctx, func() error {
		resp, rerr := c.do(ctx, "MKCOL", c.url(resourcePath), nil, nil)
		if rerr != nil {
			return rerr
		}
		resp.Body.Close()
		if resp.StatusCode == 405 {
			createdFlag = false
			return nil
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return webdavStatus(resp.StatusCode, "WebDAV MKCOL failed")
		}
		createdFlag = true
		return nil
	}); e != nil {
		return false, e
	}
	return createdFlag, nil
}
