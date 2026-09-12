package webdavinterface

import (
	"context"
	"net/http"
	"strings"
)

// --- primitive WebDAV verbs ---------------------------------------------------

func (c *WebDAVClient) url(resourcePath string) string {
	if resourcePath == "" {
		resourcePath = "/"
	}
	if !strings.HasPrefix(resourcePath, "/") {
		resourcePath = "/" + resourcePath
	}
	return c.BaseURL + resourcePath
}

func (c *WebDAVClient) propfind(ctx context.Context, resourcePath string) (body []byte, resp *http.Response, err error) {
	u := c.url(resourcePath)
	h := http.Header{}
	h.Set("Depth", "1")
	h.Set("Content-Type", "application/xml; charset=utf-8")
	xmlBody := []byte(`<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:">
  <D:allprop/>
</D:propfind>`)
	resp, err = c.do(ctx, "PROPFIND", u, h, xmlBody)
	if err != nil {
		return nil, nil, err
	}
	b, cErr := collectStatus(resp, 207)
	if cErr != nil {
		return b, resp, cErr
	}
	return b, resp, nil
}

func (c *WebDAVClient) mkcCol(ctx context.Context, dirPath string) error {
	// Recursive MKCOL of each ancestor (1:1 with createDirectory recursive:true).
	parts := strings.Split(strings.Trim(dirPath, "/"), "/")
	abs := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		abs += "/" + p
		created, cErr := c.CreateDirectory(ctx, abs)
		if cErr != nil && webdavStatusOf(cErr) != 405 {
			return cErr
		}
		if created {
			_ = created
		}
	}
	return nil
}

func (c *WebDAVClient) put(ctx context.Context, resourcePath string, content []byte, etag string) error {
	h := http.Header{}
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Overwrite", "T")
	if etag != "" {
		h.Set("If-Match", etag)
	}
	resp, err := c.do(ctx, http.MethodPut, c.url(resourcePath), h, content)
	if err != nil {
		return err
	}
	_, cErr := collectStatus(resp, 200, 201, 204)
	return cErr
}

func stringErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func collapseSlashes(s string) string {
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return s
}

func uPath(p string) string {
	if idx := strings.LastIndex(p, "?"); idx >= 0 {
		p = p[:idx]
	}
	return p
}
