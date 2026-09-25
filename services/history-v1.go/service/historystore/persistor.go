// Package historystore ports the key-addressed object persistor behind
// history-v1's raw-history store (`storage/lib/history_store.js`:
// `#persistor.get/set/clone/delete` + gzip).
//
// The persistor is BYTE-AGNOSTIC (the S3/persistor layer holds gzip bytes
// verbatim); the gzip itself lives on the store (matching Node, which
// gzips `rawHistory` before `sendStream` and gunzips after `getObject`).
package historystore

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"sync"
)

// ErrObjectNotFound is the persistor's "no such key" error, matching the
// persistor contract (Node `#persistor.get`: throws ObjectNotFoundError-ish
// when the key is absent).
type ErrObjectNotFound struct {
	B string
	K string
}

func (e *ErrObjectNotFound) Error() string {
	return "object not found: " + e.B + "/" + e.K
}

// FakePersister is an in-memory persistor: `map[bucket]map[key][]byte`.
// It models the Node S3/persistor contract (byte-agnostic send/get/clone/
// delete) WITHOUT an S3 dependency (hermetic).
type FakePersister struct {
	mu sync.RWMutex
	b  map[string]map[string][]byte
}

// NewFakePersister: `FakePersister{b: {}}` (Node `#persistor = null` until
// init; here it is created eagerly for testability).
func NewFakePersister() *FakePersister {
	return &FakePersister{b: map[string]map[string][]byte{}}
}

func (p *FakePersister) bucket(b string) map[string][]byte {
	if m, ok := p.b[b]; ok {
		return m
	}
	m := map[string][]byte{}
	p.b[b] = m
	return m
}

// PutObject: persistor.set(bucket, key, bytes) (byte-agnostic, gzip or not).
func (p *FakePersister) PutObject(b, key string, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	p.bucket(b)[key] = cp
	return nil
}

// GetObject: persistor.get(bucket, key) -> bytes; ErrObjectNotFound when absent.
func (p *FakePersister) GetObject(b, key string) ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	m, ok := p.b[b]
	if !ok {
		return nil, &ErrObjectNotFound{B: b, K: key}
	}
	data, ok := m[key]
	if !ok {
		return nil, &ErrObjectNotFound{B: b, K: key}
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	return cp, nil
}

// CopyObject: persistor.clone (bucket, key) -> (targetBucket, targetKey).
func (p *FakePersister) CopyObject(src, dst string, key string) error {
	data, err := p.GetObject(src, key)
	if err != nil {
		return err
	}
	// copy the bytes into the destination bucket.
	if err := p.PutObject(dst, key, data); err != nil {
		return err
	}
	return nil
}

// DeleteObject: persistor.delete(bucket, keys...).
func (p *FakePersister) DeleteObject(b string, keys ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range keys {
		delete(p.bucket(b), k)
	}
	return nil
}

// gzipCompress: gzip(bytes) -> gzip bytes. Node gzip writes a 12-byte header
// (no timestamp, no OS byte); Go writes its own format — byte equality is
// NOT required (the gunzip side is what matters), so a plain `compress/gzip`
// stream is fine.
func gzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("gzip: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	return buf.Bytes(), nil
}

// gzipDecompress: gunzip(gzippedBytes) -> bytes.
func gzipDecompress(data []byte) ([]byte, error) {
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		// Node gunzip of non-gzip yields an empty Buffer (or throws). Treat as empty.
		return nil, fmt.Errorf("gzip decompress: not gzip (magic % x)", data[:min(2, len(data))])
	}
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	return out, nil
}

// min: returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
