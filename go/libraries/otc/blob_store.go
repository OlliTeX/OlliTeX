package otc

import (
	"context"
	"encoding/json"
	"fmt"
)

// BlobStore is the blob-storage seam (Node: BlobStoreBase and its test fakes).
// Node's async methods become (value, error) returns. A missing blob is a
// nil *Blob (not an error), matching `getBlob -> null`.
type BlobStore interface {
	GetBlob(ctx context.Context, hash string) (*Blob, error)
	GetString(ctx context.Context, hash string) (string, error)
	GetObject(ctx context.Context, hash string) (map[string]any, error)
	PutString(ctx context.Context, content string) (*Blob, error)
	PutObject(ctx context.Context, obj map[string]any) (*Blob, error)
}

// BaseBlobStore supplies the BlobStoreBase defaults (getBlob -> nil,
// getString -> ” for the empty hash else fetchString, getObject -> JSON.parse
// or {}) over a concrete fetch/put implementation.
type BaseBlobStore struct {
	FetchString func(ctx context.Context, hash string) (string, error)
	PutStringFn func(ctx context.Context, content string) (*Blob, error)
	PutObjectFn func(ctx context.Context, obj map[string]any) (*Blob, error)
}

// GetBlob mirrors BlobStoreBase.getBlob (default: return null).
func (b *BaseBlobStore) GetBlob(ctx context.Context, hash string) (*Blob, error) { return nil, nil }

// GetString mirrors BlobStoreBase.getString (” for empty hash, else fetch).
func (b *BaseBlobStore) GetString(ctx context.Context, hash string) (string, error) {
	if hash == "" {
		return "", nil
	}
	if b.FetchString == nil {
		return "", fmt.Errorf("BlobStoreBase.fetchString not implemented")
	}
	return b.FetchString(ctx, hash)
}

// GetObject mirrors BlobStoreBase.getObject (JSON.parse, or {} when empty).
func (b *BaseBlobStore) GetObject(ctx context.Context, hash string) (map[string]any, error) {
	s, err := b.GetString(ctx, hash)
	if err != nil {
		return nil, err
	}
	if s == "" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// PutString delegates to the concrete implementation (Node abstract stub).
func (b *BaseBlobStore) PutString(ctx context.Context, content string) (*Blob, error) {
	if b.PutStringFn == nil {
		return nil, fmt.Errorf("BlobStoreBase.putString not implemented")
	}
	return b.PutStringFn(ctx, content)
}

// PutObject delegates to the concrete implementation (Node abstract stub).
func (b *BaseBlobStore) PutObject(ctx context.Context, obj map[string]any) (*Blob, error) {
	if b.PutObjectFn == nil {
		return nil, fmt.Errorf("BlobStoreBase.putObject not implemented")
	}
	return b.PutObjectFn(ctx, obj)
}
