package swap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3SwapStore ports S3SwapStore (AWS-S3-backed blob store). Region defaults
// to us-east-1 when unset; a non-empty endpoint enables path-style addressing
// and points the client at that endpoint (matching the Java builder, which
// sets forcePathStyle(true) and endpointOverride(URI) together).
type S3SwapStore struct {
	client     *s3.Client
	bucketName string
}

// NewS3SwapStore builds the S3 client from an access key / secret pair,
// bucket name, region and (optional) endpoint URL.
func NewS3SwapStore(accessKey, secret, bucketName, region, endpoint string) (*S3SwapStore, error) {
	if bucketName == "" {
		return nil, fmt.Errorf("s3 swap store: empty bucket name")
	}
	if region == "" {
		region = "us-east-1"
	}
	opts := s3.Options{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secret, ""),
	}
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("s3 swap store: bad endpoint %q: %w", endpoint, err)
		}
		opts.BaseEndpoint = aws.String(u.String())
		opts.UsePathStyle = true
	}
	client := s3.New(opts)
	return &S3SwapStore{client: client, bucketName: bucketName}, nil
}

// Upload ports S3SwapStore.upload: put (bucket, project) = blob.
func (s *S3SwapStore) Upload(projectName string, blob []byte) error {
	_, err := s.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:        aws.String(s.bucketName),
		Key:           aws.String(projectName),
		Body:          bytes.NewReader(blob),
		ContentLength: int64PtrOf(len(blob)),
	})
	if err != nil {
		return fmt.Errorf("s3 upload %q: %w", projectName, err)
	}
	return nil
}

// Download ports S3SwapStore.openDownloadStream: get (bucket, project) and
// read it entirely (Go reads whole projects into memory, mirroring the Java
// usage where the InputStream is fully read before the swap completes).
func (s *S3SwapStore) Download(projectName string) ([]byte, error) {
	out, err := s.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(projectName),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 download %q: %w", projectName, err)
	}
	defer out.Body.Close()
	blob, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("s3 download body %q: %w", projectName, err)
	}
	return blob, nil
}

// Remove ports S3SwapStore.remove: deleteObject (idempotent for our purposes).
func (s *S3SwapStore) Remove(projectName string) error {
	_, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketName),
		Key:    aws.String(projectName),
	})
	if err != nil {
		return fmt.Errorf("s3 remove %q: %w", projectName, err)
	}
	return nil
}

func (s *S3SwapStore) IsSafe() bool { return true }

func int64PtrOf(n int) *int64 { return aws.Int64(int64(n)) }
