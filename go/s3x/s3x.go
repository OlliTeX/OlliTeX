// Package s3x is a minimal, dependency-free S3 (path-style) client aimed at
// SeaweedFS's S3 gateway and similar S3-compatible object stores.
//
// It implements exactly the operations the Overleaf persistors need:
// PutObject / HeadObject / GetObject / DeleteObject / ListObjects (paged) /
// CreateBucket — anonymous credentials by default (SeaweedFS S3 accepts
// unsigned requests unless auth is enabled). If AWS_ACCESS_KEY_ID /
// AWS_SECRET_ACCESS_KEY are set they are sent as basic-auth headers, which
// both SeaweedFS (when auth is enabled) and most S3-compatible gateways
// accept; full AWS SigV4 is deliberately out of scope (no real-AWS targets
// in this stack).
package s3x

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors mapped from the gateway response, 1:1 with how the callers
// (filestore/docstore) distinguish "missing" from "transport/permission".
var (
	ErrNoSuchBucket = errors.New("s3x: no such bucket")
	ErrNoSuchKey    = errors.New("s3x: no such key")
)

// Client is an S3 gateway client. Endpoint must be absolute (e.g.
// "http://127.0.0.1:8333").
type Client struct {
	Endpoint string
	Key      string
	Secret   string
	HTTP     *http.Client
}

func New(endpoint, key, secret string) *Client {
	return &Client{
		Endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"),
		Key:      key,
		Secret:   secret,
		HTTP:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) urlv(bucket, key string, query url.Values) string {
	u := c.Endpoint + "/" + url.PathEscape(bucket)
	if key != "" {
		u += "/" + url.PathEscape(key)
	}
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

func (c *Client) newReq(method, u string, body io.Reader, extraHeader http.Header) (*http.Request, error) {
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}
	if c.Key != "" {
		req.SetBasicAuth(c.Key, c.Secret)
	}
	for k, vs := range extraHeader {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	return req, nil
}

// mapErr converts an HTTP error status into the sentinel errors the
// persistor layer understands (404 → bucket/key not found; anything else →
// a generic error carrying the gateway's status + body, like the Node
// persistor surfaces).
func mapErr(status int, body []byte) error {
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) > 300 {
		trimmed = trimmed[:300]
	}
	if status == 404 {
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "nosuchbucket") || strings.Contains(lower, "bucket") {
			return fmt.Errorf("%w: %s", ErrNoSuchBucket, trimmed)
		}
		return fmt.Errorf("%w: %s", ErrNoSuchKey, trimmed)
	}
	return fmt.Errorf("s3x: HTTP %d: %s", status, trimmed)
}

// PutObject uploads r to bucket/key. etag (usually the hex md5) is returned;
// sourceMD5, when non-empty, is sent as Content-MD5 so the gateway rejects
// corrupt uploads the way the Node FSPersistor md5 check does.
func (c *Client) PutObject(bucket, key string, r io.Reader, mimeSubtype string, sourceMD5 string) (string, error) {
	hdr := http.Header{}
	if mimeSubtype != "" {
		if ct := mime.TypeByExtension(strings.TrimLeft(mimeSubtype, ".")); ct != "" {
			hdr.Set("Content-Type", ct)
		} else {
			hdr.Set("Content-Type", "application/octet-stream")
		}
	} else {
		hdr.Set("Content-Type", "application/octet-stream")
	}
	if sourceMD5 != "" {
		// Internal md5 convention is hex (FSPersistor parity); the S3 wire
		// format for Content-MD5 is base64 (AWS spec — SeaweedFS enforces
		// it with BadDigest). Translate at the boundary.
		raw, err := hex.DecodeString(sourceMD5)
		if len(raw) != 16 || err != nil {
			return "", fmt.Errorf("s3x: bad source md5 %q", sourceMD5)
		}
		hdr.Set("Content-MD5", base64.StdEncoding.EncodeToString(raw))
	}
	req, err := c.newReq(http.MethodPut, c.urlv(bucket, key, nil), r, hdr)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", mapErr(resp.StatusCode, body)
	}
	return strings.Trim(resp.Header.Get("ETag"), `"`), nil
}

// HeadObject returns size/etag for bucket/key.
func (c *Client) HeadObject(bucket, key string) (int64, string, error) {
	req, err := c.newReq(http.MethodHead, c.urlv(bucket, key, nil), nil, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		k := key
		if k == "" {
			return 0, "", ErrNoSuchBucket
		}
		return 0, "", ErrNoSuchKey
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, "", mapErr(resp.StatusCode, body)
	}
	size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	return size, strings.Trim(resp.Header.Get("ETag"), `"`), nil
}

// GetResult carries the object body plus the metadata the persistor layer
// reports to clients.
type GetResult struct {
	Body        io.ReadCloser
	Size        int64
	ETag        string
	ContentType string
	LastMod     time.Time
}

// GetObject streams bucket/key back.
func (c *Client) GetObject(bucket, key string) (*GetResult, error) {
	req, err := c.newReq(http.MethodGet, c.urlv(bucket, key, nil), nil, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, mapErr(resp.StatusCode, body)
	}
	size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	return &GetResult{
		Body:        resp.Body,
		Size:        size,
		ETag:        strings.Trim(resp.Header.Get("ETag"), `"`),
		ContentType: resp.Header.Get("Content-Type"),
		LastMod:     parseTime(resp.Header.Get("Last-Modified")),
	}, nil
}

// DeleteObject removes bucket/key. A missing key is a no-op, matching S3
// (and the Node persistor) semantics.
func (c *Client) DeleteObject(bucket, key string) error {
	req, err := c.newReq(http.MethodDelete, c.urlv(bucket, key, nil), nil, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	// S3 DELETE of a missing object is 204 — anything 4xx/5xx is a real
	// error except 404 which we treat as "already gone".
	if resp.StatusCode == 404 {
		return nil
	}
	return mapErr(resp.StatusCode, body)
}

// Object is one listed object.
type Object struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
}

type listResult struct {
	IsTruncated bool
	NextMarker  string
	Contents    []listedObj `xml:"Contents"`
}

type listedObj struct {
	Key          string
	Size         int64  `xml:"Size"`
	ETag         string `xml:"ETag"`
	LastModified string
}

// ListObjects lists all objects under bucket with the given key prefix,
// following S3 pagination (IsTruncated/NextMarker) to completion.
func (c *Client) ListObjects(bucket, prefix string) ([]Object, error) {
	var out []Object
	marker := ""
	for {
		q := url.Values{}
		q.Set("prefix", prefix)
		if marker != "" {
			q.Set("marker", marker)
		}
		req, err := c.newReq(http.MethodGet, c.urlv(bucket, "", q), nil, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if resp.StatusCode == 404 {
			return nil, fmt.Errorf("%w: %s", ErrNoSuchBucket, strings.TrimSpace(string(body)))
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, mapErr(resp.StatusCode, body)
		}
		var lr listResult
		if err := xml.Unmarshal(body, &lr); err != nil {
			return nil, fmt.Errorf("s3x: bad list XML: %w", err)
		}
		for _, o := range lr.Contents {
			out = append(out, Object{
				Key:          o.Key,
				Size:         o.Size,
				ETag:         strings.Trim(o.ETag, `"`),
				LastModified: parseTime(o.LastModified),
			})
		}
		if !lr.IsTruncated {
			return out, nil
		}
		if lr.NextMarker == "" {
			if len(lr.Contents) > 0 {
				marker = lr.Contents[len(lr.Contents)-1].Key
			} else {
				break
			}
		} else {
			marker = lr.NextMarker
		}
	}
	return out, nil
}

// CreateBucket makes bucket; an existing/owned bucket (200 or 409) is a
// success, matching "make it exist" semantics.
func (c *Client) CreateBucket(bucket string) error {
	req, err := c.newReq(http.MethodPut, c.urlv(bucket, "", nil), nil, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 || resp.StatusCode == 201 || resp.StatusCode == 202 || resp.StatusCode == 409 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return mapErr(resp.StatusCode, body)
}

// BucketExists reports whether bucket answers HEAD.
func (c *Client) BucketExists(bucket string) (bool, error) {
	req, err := c.newReq(http.MethodHead, c.urlv(bucket, "", nil), nil, nil)
	if err != nil {
		return false, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		return true, nil
	}
	if resp.StatusCode == 404 {
		return false, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return false, mapErr(resp.StatusCode, body)
}

// Ping hits the gateway root (ListAllMyBuckets) — used for the health
// probe. A live gateway answers 2xx; connection failures surface as
// *url.Error (see IsConnectionErr).
func (c *Client) Ping() (*http.Response, error) {
	req, err := c.newReq(http.MethodGet, c.Endpoint, nil, nil)
	if err != nil {
		return nil, err
	}
	return c.HTTP.Do(req)
}

func parseTime(s string) time.Time {
	if t, err := time.Parse(time.RFC1123, s); err == nil {
		return t
	}
	z := strings.ReplaceAll(s, "Z", "+00:00")
	if t, err := time.Parse(time.RFC3339, z); err == nil {
		return t
	}
	return time.Time{}
}

// IsConnectionErr reports whether err is a TCP-level "no gateway" failure
// (vs a 4xx/5xx from a live gateway) — used by startup code that must
// distinguish "seaweed not up yet" from a configured-but-broken gateway.
func IsConnectionErr(err error) bool {
	var ne *net.OpError
	if errors.As(err, &ne) {
		return true
	}
	return false
}
