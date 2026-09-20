package swap

import "testing"

// Port of S3SwapStoreTest. The Java test has all bodies commented out (it
// needs live AWS credentials) and is effectively a construction-only
// construction/IsSafe smoke test. This Go test keeps that parity: construct
// and assert IsSafe, no network.
func TestS3SwapStoreConstructionIsSafe(t *testing.T) {
	store, err := NewS3SwapStore("ak", "sk", "the-bucket", "us-east-1", "")
	if err != nil {
		t.Fatalf("NewS3SwapStore: %v", err)
	}
	if !store.IsSafe() {
		t.Errorf("S3SwapStore (S3 region) must report IsSafe()=true")
	}
	if store.bucketName != "the-bucket" {
		t.Errorf("bucket = %q, want the-bucket", store.bucketName)
	}
}

func TestS3SwapStoreWithEndpointIsSafe(t *testing.T) {
	store, err := NewS3SwapStore("ak", "sk", "the-bucket", "", "http://localhost:9000")
	if err != nil {
		t.Fatalf("NewS3SwapStore(endpoint): %v", err)
	}
	if !store.IsSafe() {
		t.Errorf("S3SwapStore (endpoint) must report IsSafe()=true")
	}
	if store.bucketName != "the-bucket" {
		t.Errorf("bucket = %q, want the-bucket", store.bucketName)
	}
}

func TestNewS3SwapStoreEmptyBucketFails(t *testing.T) {
	_, err := NewS3SwapStore("ak", "sk", "", "us-east-1", "")
	if err == nil {
		t.Errorf("NewS3SwapStore with empty bucket should error")
	}
}
