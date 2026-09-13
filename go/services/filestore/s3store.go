package filestore

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"ollitex/go/s3x"
)

// --- S3Persistor (1:1 with libraries/object-persistor/src/S3Persistor.js),
// on the s3x gateway client. The gateway is whatever AWS_S3_ENDPOINT /
// OVERLEAF_FILESTORE_S3_ENDPOINT point at — in this stack the local
// SeaweedFS S3 gateway (http://127.0.0.1:8333), where bucket names are the
// "location" argument of every Store call.
//
// Key handling is deliberately 1:1 with Node: the key is stored VERBATIM
// (Node only flattens slashes in FSPersistor). Missing key → fseNotFound
// (404), delete-of-missing is a no-op, deleteDirectory sweeps the exact
// prefix (Node S3Persistor: ListObjectsV2 { Prefix: key }.

type s3xStore struct {
	c *s3x.Client
}

func newS3Store(endpoint, key, secret string) *s3xStore {
	return &s3xStore{c: s3x.New(endpoint, key, secret)}
}

// ensureBucket idempotently creates the bucket (Node S3 services pre-create
// buckets via the S3 API; SeaweedFS 404s a PUT to a missing bucket, so the
// service creates its own buckets at startup).
func (s *s3xStore) ensureBucket(bucket string) error {
	if err := s.c.CreateBucket(bucket); err != nil {
		return err
	}
	return nil
}

func (s *s3xStore) open(location, key string, _ bool) (io.ReadCloser, error) {
	gr, err := s.c.GetObject(location, key)
	if err != nil {
		if errors.Is(err, s3x.ErrNoSuchKey) || errors.Is(err, s3x.ErrNoSuchBucket) {
			return nil, fseNotFound()
		}
		return nil, fseRead("failed to open file for streaming")
	}
	return gr.Body, nil
}

func (s *s3xStore) objectSize(location, key string, _ bool) (int64, error) {
	size, _, err := s.c.HeadObject(location, key)
	if err != nil {
		if errors.Is(err, s3x.ErrNoSuchKey) || errors.Is(err, s3x.ErrNoSuchBucket) {
			return 0, fseNotFound()
		}
		return 0, fseRead("failed to stat file")
	}
	return size, nil
}

func (s *s3xStore) objectMd5(location, key string, _ bool) (string, error) {
	f, err := s.open(location, key, false)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fseRead("unable to hash file")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *s3xStore) exists(location, key string, _ bool) bool {
	_, _, err := s.c.HeadObject(location, key)
	return err == nil
}

// sendStream uploads the stream verbatim; when sourceMd5 is given it is sent
// as Content-MD5 and a corrupt upload is rejected by the gateway (the S3
// analog of fseStore's temp-file md5 check).
func (s *s3xStore) sendStream(location, key string, r io.Reader, _ bool, sourceMd5 string) error {
	if sourceMd5 == "" {
		// fseStore computes the hash only when asked to verify; without a
		// given md5 we could still checksum, but Node S3 sendStream also
		// skips it — keep the traffic profile identical.
		etag, err := s.c.PutObject(location, key, r, "", "")
		if err != nil {
			return fseWrite("failed to write stream")
		}
		_ = etag
		return nil
	}
	if _, err := s.c.PutObject(location, key, r, "", sourceMd5); err != nil {
		return fseWrite("md5 hash mismatch")
	}
	return nil
}

func (s *s3xStore) sendFile(location, key, source string, reqUseSub bool) error {
	// In S3 mode the "local source" is always a real local file (uploads are
	// staged in the upload folder regardless of backend — same as Node), so
	// this stays a local read → gateway PUT.
	in, err := os.Open(source)
	if err != nil {
		return fseWrite("failed to copy the specified file")
	}
	defer in.Close()
	return s.sendStream(location, key, in, reqUseSub, "")
}

func (s *s3xStore) copyObject(location, from, to string, _ bool) error {
	src, err := s.open(location, from, false)
	if err != nil {
		return fseRead("failed to copy file")
	}
	defer src.Close()
	if _, err := s.c.PutObject(location, to, src, "", ""); err != nil {
		return fseWrite("failed to copy file")
	}
	return nil
}

func (s *s3xStore) deleteObject(location, key string, _ bool) error {
	if err := s.c.DeleteObject(location, key); err != nil {
		// S3 DELETE of a missing key is a 204 no-op; a non-2xx here is a
		// real error (maps to fseWrite 5xx like the fs backend).
		return fseWrite("failed to delete file")
	}
	return nil
}

// deleteDirectory: Node S3Persistor.deleteDirectory = ListObjectsV2
// {Prefix: key} + DeleteObjects, following IsTruncated/ContinuationToken.
// Mirrored here as s3x.ListObjects (fully paged) + per-key delete.
func (s *s3xStore) deleteDirectory(location, key string, _ bool) error {
	objs, err := s.c.ListObjects(location, key)
	if err != nil {
		return fseRead("failed to list objects in S3")
	}
	for _, o := range objs {
		if err := s.c.DeleteObject(location, o.Key); err != nil {
			return fseWrite("failed to delete objects in S3")
		}
	}
	return nil
}

func (s *s3xStore) listFiles(location, key string, _ bool) []string {
	objs, err := s.c.ListObjects(location, key)
	if err != nil {
		return nil
	}
	var out []string
	for _, o := range objs {
		out = append(out, o.Key)
	}
	return out
}
