package historyv1

// s3xAdapter — persistors.S3Client over the go/s3x gateway client.
//
// The persistor logic (go/libraries/persistors) is backend-agnostic; this
// adapter gives it a real gateway. Request semantics mirror the Node
// @overleaf/object-persistor S3 client calls (put/get with range/head/
// delete/copy/list/create-bucket). Presign is not implementable over the
// basic-auth gateway (same honest-500 position as createZip).

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"ollitex/go/libraries/persistors"
	"ollitex/go/s3x"
)

type s3xAdapter struct {
	c *s3x.Client
	// ifNoneMatch — send If-None-Match on conditional puts. Node parity has
	// it ON; the SeaweedFS S3 emulation 500s on it (owner 2026-10-07 live
	// evidence: initializeProject -> 500 InternalError), so the deployment
	// can opt out via OVERLEAF_HISTORY_S3_IF_NONE_MATCH=false.
	ifNoneMatch bool
}

// NewS3xAdapter exposes the gateway adapter for main wiring.
func NewS3xAdapter(endpoint, key, secret string, ifNoneMatch bool) persistors.S3Client {
	return &s3xAdapter{c: s3x.New(endpoint, key, secret), ifNoneMatch: ifNoneMatch}
}

// toS3Error normalizes s3x classified errors to the persistor duck-typed
// S3Error (code + status) the wrapError/isNotFound logic inspects.
func toS3Error(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, s3x.ErrNoSuchBucket) {
		return &persistors.S3Error{ErrorCode: "NoSuchBucket", Status: 404, Message: err.Error(), Err: err}
	}
	if errors.Is(err, s3x.ErrNoSuchKey) {
		return &persistors.S3Error{ErrorCode: "NoSuchKey", Status: 404, Message: err.Error(), Err: err}
	}
	if m := httpRE.FindStringSubmatch(err.Error()); m != nil {
		code := m[1]
		status, _ := atoi(m[1])
		ec := code
		if code == "404" {
			ec = "NoSuchKey"
		}
		return &persistors.S3Error{ErrorCode: ec, Status: status, Message: err.Error(), Err: err}
	}
	return err
}

var httpRE = regexp.MustCompile(`s3x: HTTP (\d+)`)

func atoi(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number: %s", s)
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// doBuf — run the request with the body materialized (known Content-Length,
// no chunked transfer — the S3 gateway in this stack rejects chunked puts).
func (a *s3xAdapter) doBuf(bucket, key string, method string, query url.Values, hdr http.Header, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil && len(body) > 0 {
		r = bytes.NewReader(body)
	} else if method == http.MethodPut {
		r = bytes.NewReader(nil)
	}
	resp, err := a.c.DoRaw(method, bucket, key, query, hdr, r)
	if err != nil {
		return nil, toS3Error(err)
	}
	return resp, nil
}

func (a *s3xAdapter) PutObject(_ context.Context, p persistors.PutObjectParams) error {
	var buf []byte
	if p.Body != nil {
		b, err := io.ReadAll(p.Body)
		if err != nil {
			return fmt.Errorf("s3x adapter: read body: %w", err)
		}
		buf = b
	}
	hdr := http.Header{}
	if p.ContentType != "" {
		hdr.Set("Content-Type", p.ContentType)
	} else {
		hdr.Set("Content-Type", "application/octet-stream")
	}
	if p.ContentEncoding != "" {
		hdr.Set("Content-Encoding", p.ContentEncoding)
	}
	if a.ifNoneMatch && p.IfNoneMatch != "" {
		hdr.Set("If-None-Match", p.IfNoneMatch)
	}
	resp, err := a.doBuf(p.Bucket, p.Key, http.MethodPut, nil, hdr, buf)
	if err == nil {
		resp.Body.Close()
	}
	return err
}

type s3ListEntry struct {
	Key  string `xml:"Key"`
	Size int64  `xml:"Size"`
	ETag string `xml:"ETag"`
}

type s3ListContents struct {
	XMLName        xml.Name      `xml:"ListBucketResult"`
	IsTruncated    string        `xml:"IsTruncated"`
	NextKeyMarker  string        `xml:"NextKeyMarker"`
	CommonPrefixes []string      `xml:"CommonPrefixes>Prefix"`
	Contents       []s3ListEntry `xml:"Contents"`
}

func (a *s3xAdapter) GetObject(_ context.Context, p persistors.GetObjectParams) (*persistors.GetObjectResult, error) {
	hdr := http.Header{}
	if p.Range != "" {
		hdr.Set("Range", p.Range)
	}
	resp, err := a.doBuf(p.Bucket, p.Key, http.MethodGet, nil, hdr, nil)
	if err != nil {
		return nil, err
	}
	return &persistors.GetObjectResult{
		Body:            resp.Body,
		ContentEncoding: resp.Header.Get("Content-Encoding"),
	}, nil
}

func (a *s3xAdapter) HeadObject(_ context.Context, bucket, key string, _ map[string]any) (*persistors.HeadObjectResult, error) {
	resp, err := a.doBuf(bucket, key, http.MethodHead, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var cl *int64
	if v, err := parseInt(resp.Header.Get("Content-Length")); err == nil {
		cl = &v
	}
	return &persistors.HeadObjectResult{
		ContentLength: cl,
		ETag:          strings.Trim(resp.Header.Get("ETag"), `"`),
	}, nil
}

func (a *s3xAdapter) DeleteObject(_ context.Context, bucket, key string) error {
	_, err := a.doBuf(bucket, key, http.MethodDelete, nil, nil, nil)
	return err
}

func (a *s3xAdapter) DeleteObjects(_ context.Context, p persistors.DeleteObjectsParams) error {
	// Batch delete via the S3 DeleteObjects (multipart/form-data) request.
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	fmt.Fprintf(&buf, `<Delete><Quiet>%t</Quiet>`, p.Quiet)
	for _, o := range p.Objects {
		fmt.Fprintf(&buf, `<DeleteObject><Key>%s</Key></DeleteObject>`, xmlEscape(o.Key))
	}
	buf.WriteString(`</Delete>`)
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/xml")
	if q := p.Quiet; q {
		_ = q
	}
	_, err := a.doBuf(p.Bucket, "", http.MethodPost, url.Values{"delete": {""}}, hdr, buf.Bytes())
	return err
}

func (a *s3xAdapter) ListObjectsV2(_ context.Context, p persistors.ListObjectsV2Params) (*persistors.ListObjectsV2Result, error) {
	q := url.Values{}
	if p.Prefix != "" {
		q.Set("prefix", p.Prefix)
	}
	if p.ContinuationToken != "" {
		q.Set("continuation-token", p.ContinuationToken)
	}
	q.Set("list-type", "2")
	resp, err := a.doBuf(p.Bucket, "", http.MethodGet, q, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed s3ListContents
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("s3x adapter: list parse: %w", err)
	}
	res := &persistors.ListObjectsV2Result{
		IsTruncated:           parsed.IsTruncated == "true",
		NextContinuationToken: parsed.NextKeyMarker,
	}
	for _, e := range parsed.Contents {
		res.Contents = append(res.Contents, persistors.S3Object{Key: e.Key, Size: e.Size})
	}
	return res, nil
}

func (a *s3xAdapter) CopyObject(_ context.Context, p persistors.CopyObjectParams) error {
	hdr := http.Header{}
	hdr.Set("x-amz-copy-source", p.CopySource)
	_, err := a.doBuf(p.Bucket, p.Key, http.MethodPut, nil, hdr, nil)
	return err
}

func (a *s3xAdapter) CreateBucket(_ context.Context, bucket string) error {
	resp, err := a.doBuf(bucket, "", http.MethodPut, nil, nil, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (a *s3xAdapter) PresignGetObject(_ context.Context, _, _ string, _ time.Duration) (string, error) {
	return "", errors.New("s3x adapter: presigned URLs are not available over the basic-auth gateway (zipStore presign not supported)")
}

func parseInt(s string) (int64, error) {
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not an int: %q", s)
		}
		n = n*10 + int64(r-'0')
	}
	return n, nil
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
