package persistors

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GCSSettings mirrors the GcsPersistor settings object.
type GCSSettings struct {
	StorageClass        any // presence → NotImplementedError
	Endpoint            GCSBackendEndpoint
	RetryOptions        map[string]any
	SignedUrlExpiryInMs int64
	UnsignedUrls        bool
	DeletedBucketSuffix string
	UnlockBeforeDelete  bool
	DeleteConcurrency   int
}

// GCSBackendEndpoint — settings.endpoint {projectId, apiEndpoint}.
type GCSBackendEndpoint struct {
	ProjectID   string
	APIEndpoint string
}

// GcsPersistor is the 1:1 port of GcsPersistor.js.
type GcsPersistor struct {
	settings GCSSettings
	storage  GCSStorage
}

// NewGcsPersistor mirrors `new GcsPersistor(settings)`:
//
//	storageClass present → NotImplementedError
//	retryOptions.idempotencyStrategy validated against the SDK name table
//	    (unknown → 'Unrecognised value for retryOptions.idempotencyStrategy')
//	  → Logger.info(`Setting retryOptions.idempotencyStrategy to <name> (<value>)`)
//	clientOptions built (projectId/apiEndpoint from endpoint, retryOptions).
func NewGcsPersistor(settings GCSSettings, factory GCSStorageFactory) (*GcsPersistor, error) {
	if settings.StorageClass != nil {
		return nil, NewNotImplementedError("Use default bucket class for GCS instead of settings.storageClass", nil)
	}

	storageOptions := map[string]any{}
	if settings.Endpoint.ProjectID != "" || settings.Endpoint.APIEndpoint != "" {
		storageOptions["projectId"] = settings.Endpoint.ProjectID
		storageOptions["apiEndpoint"] = settings.Endpoint.APIEndpoint
	}
	retryOptions := map[string]any{}
	if settings.RetryOptions != nil {
		for k, v := range settings.RetryOptions {
			retryOptions[k] = v
		}
	}
	if strategy, ok := retryOptions["idempotencyStrategy"]; ok && strategy != nil {
		value, known := idempotencyStrategyValue(strategy)
		if !known {
			return nil, fmt.Errorf("Unrecognised value for retryOptions.idempotencyStrategy")
		}
		Logger.Info(nil, fmt.Sprintf("Setting retryOptions.idempotencyStrategy to %v (%d)", strategy, value))
		retryOptions["idempotencyStrategy"] = value
	}
	storageOptions["retryOptions"] = retryOptions

	if factory == nil {
		return nil, fmt.Errorf("no GCS storage factory configured")
	}
	storage, err := factory(storageOptions)
	if err != nil {
		return nil, err
	}
	return &GcsPersistor{settings: settings, storage: storage}, nil
}

// idempotencyStrategyValue — the @google-cloud/storage IdempotencyStrategy
// name→value table.
func idempotencyStrategyValue(name any) (int, bool) {
	switch n := name.(type) {
	case string:
		table := map[string]int{
			"NO_STRATEGY":                    0,
			"RETRY_IF_NOT_STARTED":           1,
			"RETRY_IF_STREAMING_NOT_STARTED": 2,
			"ALWAYS_RETRY":                   3,
		}
		v, ok := table[n]
		return v, ok
	default:
		if i, ok := n.(int); ok {
			return i, true
		}
		return 0, false
	}
}

var _ Persistor = (*GcsPersistor)(nil)

// SendFile mirrors sendFile.
func (g *GcsPersistor) SendFile(bucket, key, fsPath string) error {
	f, err := os.Open(fsPath)
	if err != nil {
		return wrapError(err, "upload to GCS failed",
			map[string]any{"bucketName": bucket, "key": key}, classWrite)
	}
	defer f.Close()
	return g.SendStream(bucket, key, f, Opts{})
}

// SendStream mirrors sendStream:
//
//	observer (gcs.egress, hash='md5' when no sourceMd5)
//	writeOptions {resumable:false}
//	  + sourceMd5 → validation 'md5' + metadata.md5Hash = hexToBase64(sourceMd5)
//	  + contentType / contentEncoding metadata
//	fileOptions: ifNoneMatch '*' → generation 0
//	pipeline(readStream, observer, uploadStream)
//	if !sourceMd5 → verifyMd5(this, bucket, key, observer.getHash())
//	errors → WriteError 'upload to GCS failed'
func (g *GcsPersistor) SendStream(bucket, key string, source io.Reader, opts Opts) error {
	observeHash := ""
	if opts.SourceMD5 == "" {
		observeHash = "md5"
	}
	observer := NewObserver("gcs.egress", bucket, observeHash) // egress from us to GCS

	writeOptions := GCSWriteOptions{Resumable: false}
	sourceMD5 := opts.SourceMD5
	if sourceMD5 != "" {
		writeOptions.Validation = "md5"
		writeOptions.Metadata = map[string]string{
			"md5Hash": hexToBase64(sourceMD5),
		}
	}
	if opts.ContentType != "" {
		if writeOptions.Metadata == nil {
			writeOptions.Metadata = map[string]string{}
		}
		writeOptions.Metadata["contentType"] = opts.ContentType
	}
	if opts.ContentEncoding != "" {
		if writeOptions.Metadata == nil {
			writeOptions.Metadata = map[string]string{}
		}
		writeOptions.Metadata["contentEncoding"] = opts.ContentEncoding
	}

	var file GCSFile
	if opts.IfNoneMatch == "*" {
		file = g.storage.Bucket(bucket).File(key, 0) // generation: 0
	} else {
		file = g.storage.Bucket(bucket).File(key)
	}
	wc, err := file.CreateWriteStream(writeOptions)
	if err != nil {
		observer.Finish(err)
		return wrapError(err, "upload to GCS failed", map[string]any{
			"bucketName":  bucket,
			"key":         key,
			"ifNoneMatch": opts.IfNoneMatch,
		}, classWrite)
	}
	if _, err := io.Copy(wc, io.TeeReader(source, observer)); err != nil {
		wc.Close()
		observer.Finish(err)
		return wrapError(err, "upload to GCS failed", map[string]any{
			"bucketName":  bucket,
			"key":         key,
			"ifNoneMatch": opts.IfNoneMatch,
		}, classWrite)
	}
	if err := wc.Close(); err != nil {
		observer.Finish(err)
		return wrapError(err, "upload to GCS failed", map[string]any{
			"bucketName":  bucket,
			"key":         key,
			"ifNoneMatch": opts.IfNoneMatch,
		}, classWrite)
	}
	observer.Finish(nil)

	if sourceMD5 == "" {
		// compare the computed hash with Google's (couldn't be told
		// beforehand) — throws on mismatch
		sourceMD5 = observer.GetHash()
		if err := verifyMd5(g, bucket, key, sourceMD5); err != nil {
			return wrapError(err, "upload to GCS failed", map[string]any{
				"bucketName":  bucket,
				"key":         key,
				"ifNoneMatch": opts.IfNoneMatch,
			}, classWrite)
		}
	}
	return nil
}

// GetObjectStream mirrors getObjectStream (response-status switch → wrap;
// observer; autoGunzip when content-encoding is gzip).
func (g *GcsPersistor) GetObjectStream(bucket, key string, opts Opts) (io.ReadCloser, error) {
	observer := NewObserver("gcs.ingress", bucket, "") // ingress from GCS to us

	readOpts := GCSReadOptions{Decompress: false}
	if opts.Start != nil {
		readOpts.Start = opts.Start
	}
	if opts.End != nil {
		readOpts.End = opts.End
	}

	file := g.storage.Bucket(bucket).File(key)
	stream, err := file.CreateReadStream(readOpts)
	if err != nil {
		return nil, gcsStreamError(err, bucket, key, opts)
	}

	var contentEncoding string
	switch stream.Status {
	case 200, 206: // full / partial response
		contentEncoding = stream.ContentEncoding
	case 404:
		stream.Body.Close()
		return nil, gcsStreamError(NewNotFoundError("", nil), bucket, key, opts)
	default:
		stream.Body.Close()
		return nil, gcsStreamError(fmt.Errorf("non success status: %d", stream.Status), bucket, key, opts)
	}

	r := io.TeeReader(stream.Body, observer)
	var reader io.Reader = r
	if contentEncoding == "gzip" && opts.AutoGunzip {
		zr, zerr := gzip.NewReader(r)
		if zerr != nil {
			stream.Body.Close()
			observer.Finish(zerr)
			return nil, gcsStreamError(zerr, bucket, key, opts)
		}
		reader = zr
	}
	return &gcsStream{
		body:     stream.Body,
		r:        reader,
		observer: observer,
	}, nil
}

func gcsStreamError(cause error, bucket, key string, opts Opts) error {
	return wrapError(cause, "error reading file from GCS", map[string]any{
		"bucketName": bucket,
		"key":        key,
		"opts":       opts,
	}, classRead)
}

// gcsStream — errors from the source surface on Read (Node: pipeline catch).
type gcsStream struct {
	body     io.ReadCloser
	r        io.Reader
	observer *Observer
}

func (s *gcsStream) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && err != io.EOF {
		s.observer.Finish(err)
	}
	return n, err
}
func (s *gcsStream) Close() error {
	s.observer.Finish(nil)
	return s.body.Close()
}

// GetRedirectURL mirrors getRedirectUrl:
//
//	unsignedUrls → `${apiEndpoint || https://storage.googleapis.com}/
//	  download/storage/v1/b/${bucket}/o/${key}?alt=media`
//	else the signed-read URL (expires = now + signedUrlExpiryInMs).
func (g *GcsPersistor) GetRedirectURL(bucket, key string) (string, error) {
	if g.settings.UnsignedUrls {
		apiEndpoint := g.settings.Endpoint.APIEndpoint
		if apiEndpoint == "" {
			apiEndpoint = "https://storage.googleapis.com"
		}
		return fmt.Sprintf("%s/download/storage/v1/b/%s/o/%s?alt=media", apiEndpoint, bucket, key), nil
	}
	file := g.storage.Bucket(bucket).File(key)
	url, err := g.signedReadURL(file)
	if err != nil {
		return "", wrapError(err, "error generating signed url for GCS file",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	return url, nil
}

// signedReadURL — seam call (Node: file.getSignedUrl({action:'read', expires})).
func (g *GcsPersistor) signedReadURL(file GCSFile) (string, error) {
	type signable interface {
		GetSignedURL(expires int64) (string, error)
	}
	if s, ok := file.(signable); ok {
		return s.GetSignedURL(nowMs() + g.settings.SignedUrlExpiryInMs)
	}
	return "", fmt.Errorf("signed URLs not supported by this GCS adapter")
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}

// GetObjectSize mirrors getObjectSize (metadata.size is a string).
func (g *GcsPersistor) GetObjectSize(bucket, key string, _ Opts) (int64, error) {
	file := g.storage.Bucket(bucket).File(key)
	md, err := file.GetMetadata()
	if err != nil {
		return 0, wrapError(err, "error getting size of GCS object",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	size, _ := strconv.ParseInt(md.Size, 10, 64)
	return size, nil
}

// GetObjectMd5Hash mirrors getObjectMd5Hash (metadata.md5Hash is base64 → hex).
func (g *GcsPersistor) GetObjectMd5Hash(bucket, key string, _ Opts) (string, error) {
	file := g.storage.Bucket(bucket).File(key)
	md, err := file.GetMetadata()
	if err != nil {
		return "", wrapError(err, "error getting hash of GCS object",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	return base64ToHex(md.MD5Hash), nil
}

// DeleteObject mirrors deleteObject (deletedBucketSuffix copy,
// unlockBeforeDelete, 404 is a no-op).
func (g *GcsPersistor) DeleteObject(bucket, key string) error {
	file := g.storage.Bucket(bucket).File(key)

	if g.settings.DeletedBucketSuffix != "" {
		suffix := g.settings.DeletedBucketSuffix
		iso := time.Now().UTC().Format("2006-01-02T15:04:05.000Z") // new Date().toISOString()
		destFile := g.storage.Bucket(bucket + suffix).File(key + "-" + iso)
		if err := file.Copy(&destFile); err != nil {
			return gcsDeleteError(err, bucket, key)
		}
	}
	if g.settings.UnlockBeforeDelete {
		falseVal := false
		if err := file.SetMetadata(map[string]any{"eventBasedHold": &falseVal}); err != nil {
			return gcsDeleteError(err, bucket, key)
		}
	}
	if err := file.Delete(); err != nil {
		return gcsDeleteError(err, bucket, key)
	}
	return nil
}

func gcsDeleteError(err error, bucket, key string) error {
	// ignore 404s: it's fine if the file doesn't exist.
	if se, ok := err.(interface{ StatusCode() int }); ok && se.StatusCode() == 404 {
		return nil
	}
	if codeOf(err) == "404" {
		return nil
	}
	return wrapError(err, "error deleting GCS object",
		map[string]any{"bucketName": bucket, "key": key}, classWrite)
}

// DeleteDirectory mirrors deleteDirectory (paginated sweep with
// deleteConcurrency; a NotFound prefix is a no-op).
func (g *GcsPersistor) DeleteDirectory(bucket, key string, _ string) error {
	prefix := ensurePrefixIsDirectory(key)
	query := GCSQuery{Prefix: prefix, AutoPaginate: false}
	for {
		files, next, err := g.storage.Bucket(bucket).GetFiles(query)
		if err != nil {
			wrapped := wrapError(err, "failed to delete directory in GCS",
				map[string]any{"bucketName": bucket, "key": key}, classWrite)
			if isNotFoundError(wrapped) {
				return nil
			}
			return wrapped
		}
		if len(files) > 0 {
			concurrency := g.settings.DeleteConcurrency
			if concurrency <= 0 {
				concurrency = 1
			}
			if err := runPool(concurrency, files, func(f GCSListedFile) error {
				return g.DeleteObject(bucket, f.Name)
			}); err != nil {
				wrapped := wrapError(err, "failed to delete directory in GCS",
					map[string]any{"bucketName": bucket, "key": key}, classWrite)
				if isNotFoundError(wrapped) {
					return nil
				}
				return wrapped
			}
		}
		if next == nil {
			return nil
		}
		query = *next
	}
}

// listDirectory mirrors #listDirectory.
func (g *GcsPersistor) listDirectory(bucket, prefix string) ([]GCSListedFile, error) {
	files, _, err := g.storage.Bucket(bucket).GetFiles(GCSQuery{Prefix: prefix})
	if err != nil {
		return nil, wrapError(err, "failed to list objects in GCS",
			map[string]any{"bucketName": bucket, "prefix": prefix}, classRead)
	}
	return files, nil
}

// DirectorySize mirrors directorySize.
func (g *GcsPersistor) DirectorySize(bucket, key string, _ string) (int64, error) {
	files, err := g.listDirectory(bucket, ensurePrefixIsDirectory(key))
	if err != nil {
		return 0, err
	}
	var size int64
	for _, file := range files {
		sz, _ := strconv.ParseInt(file.Metadata.Size, 10, 64)
		size += sz
	}
	return size, nil
}

// ListDirectoryKeys mirrors listDirectoryKeys.
func (g *GcsPersistor) ListDirectoryKeys(bucket, prefix string) ([]string, error) {
	files, err := g.listDirectory(bucket, ensurePrefixIsDirectory(prefix))
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(files))
	for _, f := range files {
		keys = append(keys, f.Name)
	}
	return keys, nil
}

// ListDirectoryStats mirrors listDirectoryStats.
func (g *GcsPersistor) ListDirectoryStats(bucket, prefix string) ([]DirStat, error) {
	files, err := g.listDirectory(bucket, ensurePrefixIsDirectory(prefix))
	if err != nil {
		return nil, err
	}
	stats := make([]DirStat, 0, len(files))
	for _, f := range files {
		sz, _ := strconv.ParseInt(f.Metadata.Size, 10, 64)
		stats = append(stats, DirStat{Key: f.Name, Size: sz})
	}
	return stats, nil
}

// CheckIfObjectExists mirrors checkIfObjectExists.
func (g *GcsPersistor) CheckIfObjectExists(bucket, key string, _ Opts) (bool, error) {
	file := g.storage.Bucket(bucket).File(key)
	ok, err := file.Exists()
	if err != nil {
		return false, wrapError(err, "error checking if file exists in GCS",
			map[string]any{"bucketName": bucket, "key": key}, classRead)
	}
	return ok, nil
}

// CopyObject mirrors copyObject (incl. the fake-gcs-server invalid-JSON
// workaround: message === 'Cannot parse response as JSON: not found\n'
// is treated as a 404).
func (g *GcsPersistor) CopyObject(bucket, sourceKey, destKey string, _ Opts) error {
	src := g.storage.Bucket(bucket).File(sourceKey)
	dest := g.storage.Bucket(bucket).File(destKey)
	if err := src.Copy(&dest); err != nil {
		// fake-gcs-server bug workaround (Node sets err.code = 404)
		if err.Error() == "Cannot parse response as JSON: not found\n" {
			err = &GCSStatusError{ErrorCode: 404, Message: err.Error()}
		}
		return wrapError(err, "failed to copy file in GCS",
			map[string]any{"bucketName": bucket, "sourceKey": sourceKey, "destKey": destKey}, classWrite)
	}
	return nil
}

// ensurePrefixIsDirectory mirrors the module helper.
func ensurePrefixIsDirectory(key string) string {
	if key == "" || strings.HasSuffix(key, "/") {
		return key
	}
	return key + "/"
}

// runPool — tiny-async-pool: run `items` through `fn` with N in flight,
// failing fast on the first error.
func runPool(n int, items []GCSListedFile, fn func(GCSListedFile) error) error {
	if n <= 0 {
		n = 1
	}
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	jobs := make(chan GCSListedFile, len(items))
	go func() {
		for _, it := range items {
			jobs <- it
		}
		close(jobs)
	}()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				err := fn(item)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	return firstErr
}
