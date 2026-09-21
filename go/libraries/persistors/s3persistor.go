package persistors

import (
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// S3Settings mirrors the S3Persistor settings object
// (S3PersistorSettings typedef in S3Persistor.js).
type S3Settings struct {
	StorageClass        map[string]string
	Key                 string
	Secret              string
	Region              string
	Endpoint            string
	PathStyle           bool
	MaxRetries          int
	HTTPOptions         map[string]any
	Ca                  string
	SignedUrlExpiryInMs int64
	PartSize            int64
	BucketCreds         map[string]BucketCreds
}

// BucketCreds mirrors `{auth_key, auth_secret}`.
type BucketCreds struct {
	AuthKey    string
	AuthSecret string
}

// S3ClientFactory produces a client per bucket. Node: `_getClientForBucket`
// constructs `new S3Client(_buildClientOptions(...))`; the factory is the
// seam (tests: a fake client; runtime: an adapter over the repo's AWS SDK v2
// or go/s3x gateway).
type S3ClientFactory func(bucket string) (S3Client, error)

// S3Persistor is the 1:1 port of S3Persistor.js.
type S3Persistor struct {
	settings S3Settings
	clients  map[string]S3Client
	factory  S3ClientFactory
}

// NewS3Persistor mirrors `new S3Persistor(settings)` (settings may be nil →
// {}). factory may be nil: clients then error on first use ("no S3 client
// factory configured") — Go-ism, Node always constructs an SDK client.
func NewS3Persistor(settings S3Settings, factory S3ClientFactory) *S3Persistor {
	if settings.StorageClass == nil {
		settings.StorageClass = map[string]string{}
	}
	return &S3Persistor{
		settings: settings,
		clients:  map[string]S3Client{},
		factory:  factory,
	}
}

var _ Persistor = (*S3Persistor)(nil)

// getClientForBucket mirrors `_getClientForBucket` (per-bucket cache).
func (s *S3Persistor) getClientForBucket(bucket string) (S3Client, error) {
	if c, ok := s.clients[bucket]; ok {
		return c, nil
	}
	if s.factory == nil {
		return nil, fmt.Errorf("no S3 client factory configured for bucket %q", bucket)
	}
	c, err := s.factory(bucket)
	if err != nil {
		return nil, err
	}
	s.clients[bucket] = c
	return c, nil
}

// ---------------------------------------------------------------------------
// sendStream / sendFile

// SendFile mirrors sendFile: read the file and send it as a stream.
func (s *S3Persistor) SendFile(bucket, key, fsPath string) error {
	f, err := os.Open(fsPath)
	if err != nil {
		return wrapError(err, "failed to copy the specified file",
			map[string]any{"bucketName": bucket, "key": key, "source": fsPath}, classWrite)
	}
	defer f.Close()
	return s.SendStream(bucket, key, f, Opts{})
}

// SendStream mirrors sendStream:
//
//	uploadOptions = {Bucket, Key, Body: observer}
//	+ storageClass/bucket, contentType/Encoding/Length, IfNoneMatch:'*',
//	  ssecOptions.put()
//	sourceMd5 is REFUSED (S3 has its own integrity protection).
//	upload (partSize). Errors → WriteError 'upload to S3 failed'.
func (s *S3Persistor) SendStream(bucket, key string, source io.Reader, opts Opts) error {
	if opts.SourceMD5 != "" {
		// fail straight away to prevent the client from wasting CPU/IO
		// precomputing the hash (Node throws before the Upload is built).
		err := fmt.Errorf("sourceMd5 option is not supported, S3 provides its own integrity protection mechanism")
		return wrapError(err, "upload to S3 failed", map[string]any{
			"bucketName":  bucket,
			"key":         key,
			"ifNoneMatch": opts.IfNoneMatch,
		}, classWrite)
	}

	observer := NewObserver("s3.egress", bucket, "") // egress from us to S3
	var reader io.Reader = source
	if opts.SourceMD5 == "" {
		reader = io.TeeReader(source, observer)
	}

	params := PutObjectParams{
		Bucket: bucket,
		Key:    key,
		Body:   reader,
	}
	if sc, ok := s.settings.StorageClass[bucket]; ok && sc != "" {
		params.StorageClass = sc
	}
	if opts.ContentType != "" {
		params.ContentType = opts.ContentType
	}
	if opts.ContentEncoding != "" {
		params.ContentEncoding = opts.ContentEncoding
	}
	if opts.ContentLength > 0 {
		params.ContentLength = opts.ContentLength
	}
	if opts.IfNoneMatch == "*" {
		params.IfNoneMatch = "*"
	}
	if opts.SSEC != nil {
		params.PutSSEC = opts.SSEC.Put()
	}

	client, err := s.getClientForBucket(bucket)
	if err != nil {
		observer.Finish(err)
		return wrapError(err, "upload to S3 failed", map[string]any{
			"bucketName":  bucket,
			"key":         key,
			"ifNoneMatch": opts.IfNoneMatch,
		}, classWrite)
	}
	err = client.PutObject(context.Background(), params)
	observer.Finish(err)
	if err != nil {
		return wrapError(err, "upload to S3 failed", map[string]any{
			"bucketName":  bucket,
			"key":         key,
			"ifNoneMatch": opts.IfNoneMatch,
		}, classWrite)
	}
	return nil
}

// ---------------------------------------------------------------------------
// getObjectStream

// GetObjectStream mirrors getObjectStream: GetObject (+ Range, SSEC-get),
// then pipeline(stream, observer, [gunzip], pass). Go returns a ReadCloser
// that tees through the observer and (when contentEncoding == 'gzip' &&
// opts.autoGunzip) a zlib reader. Closing the returned stream aborts the
// underlying request (Node: abortController.abort()).
func (s *S3Persistor) GetObjectStream(bucket, key string, opts Opts) (io.ReadCloser, error) {
	params := GetObjectParams{Bucket: bucket, Key: key}
	if opts.Start != nil && opts.End != nil {
		params.Range = fmt.Sprintf("bytes=%d-%d", *opts.Start, *opts.End)
	}
	if opts.SSEC != nil {
		params.GetSSEC = opts.SSEC.Get()
	}

	client, err := s.getClientForBucket(bucket)
	if err != nil {
		return nil, wrapError(err, "error reading file from S3",
			map[string]any{"bucketName": bucket, "key": key, "opts": opts}, classRead)
	}
	res, err := client.GetObject(context.Background(), params)
	if err != nil {
		return nil, wrapError(err, "error reading file from S3",
			map[string]any{"bucketName": bucket, "key": key, "opts": opts}, classRead)
	}

	observer := NewObserver("s3.ingress", bucket, "") // ingress from S3 to us
	r := io.TeeReader(res.Body, observer)
	var reader io.Reader = r
	if res.ContentEncoding == "gzip" && opts.AutoGunzip {
		zr, err := gzip.NewReader(r)
		if err != nil {
			res.Body.Close()
			observer.Finish(err)
			return nil, wrapError(err, "error reading file from S3",
				map[string]any{"bucketName": bucket, "key": key, "opts": opts}, classRead)
		}
		reader = zr
	}

	return &s3Stream{
		body:     res.Body,
		r:        reader,
		observer: observer,
	}, nil
}

// s3Stream — the Node "PassThrough with a minimal interface": buffered until
// the caller reads; errors from the source surface on Read; Close aborts the
// underlying request (Node: abortController.abort on stream error/end).
type s3Stream struct {
	body     io.ReadCloser
	r        io.Reader
	observer *Observer
}

func (s *s3Stream) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && err != io.EOF {
		s.observer.Finish(err)
		s.body.Close() // Node: abortController.abort()
	}
	return n, err
}

func (s *s3Stream) Close() error {
	s.observer.Finish(nil)
	return s.body.Close()
}

// ---------------------------------------------------------------------------
// getRedirectUrl

// GetRedirectURL mirrors getRedirectUrl: the presigned GetObject URL with
// the signedUrlExpiryInMs setting.
func (s *S3Persistor) GetRedirectURL(bucket, key string) (string, error) {
	client, err := s.getClientForBucket(bucket)
	if err != nil {
		return "", wrapError(err, "error generating signed url for S3 file",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	expires := time.Duration(s.settings.SignedUrlExpiryInMs/1000) * time.Second
	url, err := client.PresignGetObject(context.Background(), bucket, key, expires)
	if err != nil {
		return "", wrapError(err, "error generating signed url for S3 file",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	return url, nil
}

// ---------------------------------------------------------------------------
// deleteDirectory / listDirectory

// DeleteDirectory mirrors deleteDirectory (recursive via continuation
// token; DeleteObjects with Quiet).
func (s *S3Persistor) DeleteDirectory(bucket, key, continuationToken string) error {
	contents, response, err := s.listDirectory(bucket, key, continuationToken)
	if err != nil {
		return err
	}
	objects := make([]S3Object, 0, len(contents))
	for _, item := range contents {
		objects = append(objects, S3Object{Key: item.Key})
	}
	if len(objects) > 0 {
		client, err := s.getClientForBucket(bucket)
		if err != nil {
			return wrapError(err, "failed to delete objects in S3",
				map[string]any{"bucketName": bucket, "key": key}, classWrite)
		}
		if err := client.DeleteObjects(context.Background(), DeleteObjectsParams{
			Bucket:  bucket,
			Objects: objects,
			Quiet:   true,
		}); err != nil {
			return wrapError(err, "failed to delete objects in S3",
				map[string]any{"bucketName": bucket, "key": key}, classWrite)
		}
	}
	if response.IsTruncated {
		return s.DeleteDirectory(bucket, key, response.NextContinuationToken)
	}
	return nil
}

// listDirectory mirrors #listDirectory → {contents, response}.
func (s *S3Persistor) listDirectory(bucket, key, continuationToken string) ([]S3Object, ListObjectsV2Result, error) {
	params := ListObjectsV2Params{Bucket: bucket, Prefix: key}
	if continuationToken != "" {
		params.ContinuationToken = continuationToken
	}
	client, err := s.getClientForBucket(bucket)
	if err != nil {
		return nil, ListObjectsV2Result{}, wrapError(err, "failed to list objects in S3",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	res, err := client.ListObjectsV2(context.Background(), params)
	if err != nil {
		return nil, ListObjectsV2Result{}, wrapError(err, "failed to list objects in S3",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	return res.Contents, *res, nil
}

// ---------------------------------------------------------------------------
// head / size / storageClass / md5

// headObject mirrors #headObject (message: 'error getting size of s3 object'
// — the Node code uses this message for ALL head failures, not just size).
func (s *S3Persistor) headObject(bucket, key string, opts Opts) (*HeadObjectResult, error) {
	client, err := s.getClientForBucket(bucket)
	if err != nil {
		return nil, wrapError(err, "error getting size of s3 object",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	var getSSEC map[string]any
	if opts.SSEC != nil {
		getSSEC = opts.SSEC.Get()
	}
	res, err := client.HeadObject(context.Background(), bucket, key, getSSEC)
	if err != nil {
		return nil, wrapError(err, "error getting size of s3 object",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	return res, nil
}

// GetObjectSize mirrors getObjectSize.
func (s *S3Persistor) GetObjectSize(bucket, key string, opts Opts) (int64, error) {
	res, err := s.headObject(bucket, key, opts)
	if err != nil {
		return 0, err
	}
	if res.ContentLength != nil {
		return *res.ContentLength, nil
	}
	return 0, nil
}

// GetObjectStorageClass mirrors getObjectStorageClass.
func (s *S3Persistor) GetObjectStorageClass(bucket, key string, opts Opts) (string, error) {
	res, err := s.headObject(bucket, key, opts)
	if err != nil {
		return "", err
	}
	return res.StorageClass, nil
}

// GetObjectMd5Hash mirrors getObjectMd5Hash:
//
//	eTag = response.ETag?.replace(/[ "]/g, '')
//	if (eTag matches /^[a-f0-9]{32}$/) → eTag
//	else Metrics.inc('s3.md5Download') + md5 of the downloaded object.
func (s *S3Persistor) GetObjectMd5Hash(bucket, key string, opts Opts) (string, error) {
	if !opts.EtagIsNotMD5 {
		res, err := s.headObject(bucket, key, opts)
		if err != nil {
			return "", wrapError(err, "error getting hash of s3 object",
				map[string]any{"bucketName": bucket, "key": key}, classRead)
		}
		if md5hash := md5FromResponse(res); md5hash != nil {
			return *md5hash, nil
		}
	}
	Metrics.Inc("s3.md5Download", 1, nil)
	stream, err := s.GetObjectStream(bucket, key, opts)
	if err != nil {
		return "", wrapError(err, "error getting hash of s3 object",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	h, err := calculateStreamMd5(stream)
	stream.Close()
	if err != nil {
		return "", wrapError(err, "error getting hash of s3 object",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	return h, nil
}

var md5FromRe = regexp.MustCompile(`^[a-f0-9]{32}$`)

// md5FromResponse mirrors static _md5FromResponse.
func md5FromResponse(res *HeadObjectResult) *string {
	md5h := strings.NewReplacer(`"`, "", ` `, "").Replace(res.ETag)
	if !md5FromRe.MatchString(md5h) {
		return nil
	}
	return &md5h
}

// ---------------------------------------------------------------------------
// deleteObject / copyObject / checkIfObjectExists

// DeleteObject mirrors deleteObject (S3 has no NotFoundError on delete —
// the Go seam mirrors that).
func (s *S3Persistor) DeleteObject(bucket, key string) error {
	client, err := s.getClientForBucket(bucket)
	if err != nil {
		return wrapError(err, "failed to delete file in S3",
			map[string]any{"bucketName": bucket, "key": key}, classWrite)
	}
	if err := client.DeleteObject(context.Background(), bucket, key); err != nil {
		return wrapError(err, "failed to delete file in S3",
			map[string]any{"bucketName": bucket, "key": key}, classWrite)
	}
	return nil
}

// CopyObject mirrors copyObject.
func (s *S3Persistor) CopyObject(bucket, sourceKey, destKey string, opts Opts) error {
	params := CopyObjectParams{
		Bucket:     bucket,
		Key:        destKey,
		CopySource: fmt.Sprintf("/%s/%s", bucket, sourceKey),
	}
	if opts.SSECSrc != nil {
		params.CopySSEC = opts.SSECSrc.Copy()
	}
	if opts.SSEC != nil {
		params.PutSSEC = opts.SSEC.Put()
	}
	client, err := s.getClientForBucket(bucket)
	if err != nil {
		return wrapError(err, "failed to copy file in S3", infoOfParams(params), classWrite)
	}
	if err := client.CopyObject(context.Background(), params); err != nil {
		return wrapError(err, "failed to copy file in S3", infoOfParams(params), classWrite)
	}
	return nil
}

func infoOfParams(p CopyObjectParams) map[string]any {
	m := map[string]any{
		"Bucket":     p.Bucket,
		"Key":        p.Key,
		"CopySource": p.CopySource,
	}
	for k, v := range p.CopySSEC {
		m[k] = v
	}
	for k, v := range p.PutSSEC {
		m[k] = v
	}
	return m
}

// CheckIfObjectExists mirrors checkIfObjectExists (true / false on
// NotFoundError / wrapped otherwise).
func (s *S3Persistor) CheckIfObjectExists(bucket, key string, opts Opts) (bool, error) {
	_, err := s.GetObjectSize(bucket, key, opts)
	if err == nil {
		return true, nil
	}
	if isNotFoundError(err) {
		return false, nil
	}
	return false, wrapError(err, "error checking whether S3 object exists",
		map[string]any{"bucketName": bucket, "key": key}, classRead)
}

// ---------------------------------------------------------------------------
// directorySize / listDirectoryKeys / listDirectoryStats

// DirectorySize mirrors directorySize (recursive).
func (s *S3Persistor) DirectorySize(bucket, key, continuationToken string) (int64, error) {
	contents, response, err := s.listDirectory(bucket, key, continuationToken)
	if err != nil {
		return 0, err
	}
	var size int64
	for _, item := range contents {
		size += item.Size
	}
	if response.IsTruncated {
		more, err := s.DirectorySize(bucket, key, response.NextContinuationToken)
		if err != nil {
			return 0, err
		}
		return size + more, nil
	}
	return size, nil
}

// ListDirectoryKeys mirrors listDirectoryKeys (recursive).
func (s *S3Persistor) ListDirectoryKeys(bucket, prefix string) ([]string, error) {
	keys := []string{}
	token := ""
	for {
		contents, response, err := s.listDirectory(bucket, prefix, token)
		if err != nil {
			return nil, err
		}
		for _, item := range contents {
			keys = append(keys, item.Key)
		}
		if !response.IsTruncated {
			return keys, nil
		}
		token = response.NextContinuationToken
	}
}

// ListDirectoryStats mirrors listDirectoryStats (recursive; size ?? -1).
func (s *S3Persistor) ListDirectoryStats(bucket, prefix string) ([]DirStat, error) {
	stats := []DirStat{}
	token := ""
	for {
		contents, response, err := s.listDirectory(bucket, prefix, token)
		if err != nil {
			return nil, err
		}
		for _, item := range contents {
			stats = append(stats, DirStat{Key: item.Key, Size: item.Size})
		}
		if !response.IsTruncated {
			return stats, nil
		}
		token = response.NextContinuationToken
	}
}

// ---------------------------------------------------------------------------
// client options — the pure half of _buildClientOptions, test-pinned in the
// Node suite via the constructed S3Client's options (credentials, region,
// endpoint/ssl, pathStyle, maxAttempts, httpOptions/ca).

// buildClientOptions mirrors _buildClientOptions, returning the resolved
// config map (the observable surface Node's tests assert on).
func (s *S3Persistor) buildClientOptions(bucketCreds *BucketCreds, base map[string]any) map[string]any {
	options := base
	if options == nil {
		options = map[string]any{}
	}

	if bucketCreds != nil {
		options["credentials"] = map[string]string{
			"accessKeyId":     bucketCreds.AuthKey,
			"secretAccessKey": bucketCreds.AuthSecret,
		}
	} else if s.settings.Key != "" {
		options["credentials"] = map[string]string{
			"accessKeyId":     s.settings.Key,
			"secretAccessKey": s.settings.Secret,
		}
	}
	// else: default credentials provider chain (env → SSP → ini → IAM) —
	// represented by the ABSENCE of the key (Node leaves options unset).

	region := s.settings.Region
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}
	options["region"] = region

	sslEnabled := false
	if s.settings.Endpoint != "" {
		options["endpoint"] = s.settings.Endpoint
		sslEnabled = strings.HasPrefix(s.settings.Endpoint, "https:")
	}
	options["sslEnabled"] = sslEnabled

	if s.settings.PathStyle {
		options["forcePathStyle"] = true
	}
	if s.settings.MaxRetries > 0 {
		options["maxAttempts"] = s.settings.MaxRetries + 1
	}

	requestHandler := s.buildRequestHandler(sslEnabled)
	if requestHandler != nil {
		options["requestHandler"] = requestHandler
	}
	return options
}

// buildRequestHandler mirrors _buildNodeHttpHandler (+ the ca/https.Agent
// branch): returns nil when there is nothing to configure.
func (s *S3Persistor) buildRequestHandler(sslEnabled bool) map[string]any {
	params := map[string]any{}
	if s.settings.HTTPOptions != nil {
		for k, v := range s.settings.HTTPOptions {
			params[k] = v
		}
	}

	if sslEnabled && s.settings.Ca != "" {
		agent := map[string]any{"rejectUnauthorized": true, "ca": s.settings.Ca}
		params["httpAgent"] = agent
		params["httpsAgent"] = agent
	}

	// backwards compatibility: v2 httpOptions.timeout → connectionTimeout
	if timeout, ok := params["timeout"]; ok {
		params["connectionTimeout"] = timeout
		delete(params, "timeout")
	}

	if len(params) == 0 {
		return nil
	}
	params["handler"] = "NodeHttpHandler"
	return params
}

// ---------------------------------------------------------------------------
// S3Md5Middleware — the DeleteObjects MD5 fallback (S3Md5Middleware.js).
// Ported as pure logic over the params map (Node: an SDK middleware that
// strips x-amz-*(sdk-)?checksum-* headers and adds Content-MD5).

// applyDeleteObjectsMd5Fallback mirrors the md5Middleware behaviour for the
// DeleteObjects request body: drop default checksum headers, add Content-MD5
// (base64) when a body is present.
func applyDeleteObjectsMd5Fallback(headers map[string]string, body []byte) map[string]string {
	out := map[string]string{}
	for k, v := range headers {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-checksum-") || strings.HasPrefix(lk, "x-amz-sdk-checksum-") {
			continue
		}
		out[k] = v
	}
	if body != nil {
		sum := md5.Sum(body)
		out["Content-MD5"] = base64.StdEncoding.EncodeToString(sum[:])
	}
	return out
}

// CreateBucket — test-only (Node `._createBucket`).
func (s *S3Persistor) CreateBucket(bucket string) error {
	client, err := s.getClientForBucket(bucket)
	if err != nil {
		return err
	}
	return client.CreateBucket(context.Background(), bucket)
}
