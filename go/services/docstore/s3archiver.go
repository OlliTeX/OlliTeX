package docstore

// s3archiver.go — the object-persistor S3 backend for docstore archives
// (Node: PersistorManager → ObjectPersistor(s3) in DocArchiveManager), on
// the s3x gateway client.
//
// Key semantics are 1:1 with Node's S3Persistor usage:
//   - Send/Get use the key VERBATIM — DocArchiveManager builds it as
//     `${projectId}/${docId}`, so the S3 key keeps its slash (only the FS
//     archiver flattens '/'→'_'),
//   - DeleteDirectory sweeps Prefix=key exactly (Node: ListObjectsV2
//     {Prefix: key} + DeleteObjects; no trailing slash — with 24-hex
//     project ids the prefix is unambiguous),
//   - missing key → error (Node persistor throws; the route maps it to the
//     generic 500 "Oops, something went wrong", same as the fs backend's
//     ENOENT path).

import (
	"bytes"
	"context"
	"errors"
	"io"

	"ollitex/go/s3x"
)

type s3Archiver struct {
	c *s3x.Client
}

// BucketInitializer is implemented by archivers that manage their own bucket
// (the S3/SeaweedFS backend creates its archive bucket at startup; the fs
// backend does not need one).
type BucketInitializer interface {
	EnsureBucket(ctx context.Context, bucket string) error
}

// NewS3Archiver builds the S3 backend. endpoint/key/secret follow the Node
// docstore s3 config: AWS_S3_ENDPOINT || default gateway, optional
// AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY (anonymous on a local SeaweedFS).
func NewS3Archiver(endpoint, key, secret string) Archiver {
	return &s3Archiver{c: s3x.New(endpoint, key, secret)}
}

// EnsureBucket makes the archive bucket idempotently (SeaweedFS answers a
// PUT on a missing bucket with 403/NoSuchBucket, unlike AWS).
func (a *s3Archiver) EnsureBucket(ctx context.Context, bucket string) error {
	return a.c.CreateBucket(bucket)
}

// Send = S3Persistor.sendStream 1:1 (PUT, optional source-md5 as
// Content-MD5 so a corrupt upload is rejected).
func (a *s3Archiver) Send(ctx context.Context, bucket, key string, data []byte, sourceMD5 string) error {
	if _, err := a.c.PutObject(bucket, key, bytes.NewReader(data), "json", sourceMD5); err != nil {
		return err
	}
	return nil
}

// Get = S3Persistor.getObjectStream + getObjectMd5Hash 1:1 (bytes + live
// md5 of the stored object — the ETag of a SeaweedFS object is exactly the
// content md5, but we hash like the fs backend keeps doing, so a gateway
// with a different ETag policy cannot desync the caller's check).
func (a *s3Archiver) Get(ctx context.Context, bucket, key string) ([]byte, string, error) {
	gr, err := a.c.GetObject(bucket, key)
	if err != nil {
		return nil, "", err
	}
	defer gr.Body.Close()
	data, err := io.ReadAll(gr.Body)
	if err != nil {
		return nil, "", err
	}
	return data, md5hex(data), nil
}

// DeleteDirectory = S3Persistor.deleteDirectory 1:1 (paged prefix list +
// delete each; empty prefix list is a no-op, missing bucket is an error
// like the fs backend's directory-missing path).
func (a *s3Archiver) DeleteDirectory(ctx context.Context, bucket, key string) error {
	objs, err := a.c.ListObjects(bucket, key)
	if err != nil {
		// A missing bucket is a hard error for the Node persistor too, but
		// the archive-all path tolerates "nothing to delete": Node's
		// deleteDirectory on an existing-but-empty bucket is a no-op; keep
		// the same observable result for the "no archived docs" sweep.
		if errors.Is(err, s3x.ErrNoSuchBucket) {
			return nil
		}
		return err
	}
	for _, o := range objs {
		if err := a.c.DeleteObject(bucket, o.Key); err != nil {
			return err
		}
	}
	return nil
}
