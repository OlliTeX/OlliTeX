package persistors

import (
	"context"
	"io"
	"strconv"
	"time"
)

// S3Client is the narrow AWS-S3 slice object-persistor's S3Persistor uses.
// Node injects an SDK client (`new S3Client(...)`); the oracle pins the
// per-COMMAND behaviour (send(command, payload) → response/error). The Go
// seam is one method per command Node sends:
//
//	GetObjectCommand / PutObjectCommand / HeadObjectCommand /
//	DeleteObjectCommand / DeleteObjectsCommand / ListObjectsV2Command /
//	CopyObjectCommand / CreateBucketCommand (+ presigned GetObject URL).
//
// Adapters: tests use a fake; a runtime adapter over the repo's existing
// AWS SDK v2 (go.mod already requires github.com/aws/aws-sdk-go-v2/service/s3)
// or the go/s3x gateway client implements this seam — the persistor logic
// is identical either way (1:1 with the Node code path).

type PutObjectParams struct {
	Bucket          string
	Key             string
	Body            io.Reader
	StorageClass    string
	ContentType     string
	ContentEncoding string
	ContentLength   int64
	IfNoneMatch     string
	// SSEC put parameters (SSECustomerKey/MD5/Algorithm), when set.
	PutSSEC map[string]any
}

type GetObjectParams struct {
	Bucket  string
	Key     string
	Range   string // "bytes=start-end"
	GetSSEC map[string]any
}

type GetObjectResult struct {
	Body            io.ReadCloser
	ContentEncoding string
}

type HeadObjectResult struct {
	ContentLength *int64
	ETag          string
	StorageClass  string
}

type S3Object struct {
	Key  string
	Size int64
}

type ListObjectsV2Params struct {
	Bucket            string
	Prefix            string
	ContinuationToken string
}

type ListObjectsV2Result struct {
	Contents              []S3Object
	IsTruncated           bool
	NextContinuationToken string
}

type DeleteObjectsParams struct {
	Bucket  string
	Objects []S3Object // each {Key}
	Quiet   bool       // Node: Delete: { Objects, Quiet: true }
}
type CopyObjectParams struct {
	Bucket     string
	Key        string
	CopySource string // "/bucket/sourceKey"
	CopySSEC   map[string]any
	PutSSEC    map[string]any
}

// S3Client — one method per AWS command the persistor sends.
type S3Client interface {
	PutObject(ctx context.Context, p PutObjectParams) error
	GetObject(ctx context.Context, p GetObjectParams) (*GetObjectResult, error)
	HeadObject(ctx context.Context, bucket, key string, getSSEC map[string]any) (*HeadObjectResult, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	DeleteObjects(ctx context.Context, p DeleteObjectsParams) error
	ListObjectsV2(ctx context.Context, p ListObjectsV2Params) (*ListObjectsV2Result, error)
	CopyObject(ctx context.Context, p CopyObjectParams) error
	CreateBucket(ctx context.Context, bucket string) error
	// PresignGetObject mirrors `getSignedUrl(client, GetObjectCommand, {expiresIn})`.
	PresignGetObject(ctx context.Context, bucket, key string, expiry time.Duration) (string, error)
}

// S3Error is the Go stand-in for the AWS-SDK-style error duck typing the
// Node code leans on (error.code / error.Code / error.response.statusCode).
// Adapters wrap their SDK errors into this (or implement codedErr/statusErr
// directly).
type S3Error struct {
	ErrorCode string // 'NoSuchKey', 'AccessDenied', 'PreconditionFailed', '404', ...
	Status    int    // error.response.statusCode (404/412), 0 when absent
	Message   string
	Err       error
}

func (e *S3Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "s3 error"
}

// Code / StatusCode implement the codedErr / statusErr seams (Node:
// error.code / error.response.statusCode).
func (e *S3Error) Code() string { return e.ErrorCode }
func (e *S3Error) StatusCode() int {
	if e.Status != 0 {
		return e.Status
	}
	if n, err := strconv.Atoi(e.ErrorCode); err == nil && n > 0 {
		return n
	}
	return 0
}
func (e *S3Error) Unwrap() error { return e.Err }
