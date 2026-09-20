package swap

import (
	"fmt"
	"sync"
)

// SwapStore ports bridge/swap/store/SwapStore. Downloads are returned as a
// byte slice (Go reads whole projects into memory, matching the Java
// InputStream usage in InMemorySwapStore/S3SwapStore).
type SwapStore interface {
	Upload(projectName string, blob []byte) error
	Download(projectName string) ([]byte, error)
	Remove(projectName string) error
	// IsSafe reports whether the blob store may hold the authoritative copy
	// of swapped projects. In-memory and S3 are safe; the Noop store is not.
	IsSafe() bool
}

// InMemorySwapStore ports InMemorySwapStore.
type InMemorySwapStore struct {
	lock sync.RWMutex
	m    map[string][]byte
}

func NewInMemorySwapStore() *InMemorySwapStore {
	return &InMemorySwapStore{m: map[string][]byte{}}
}

func (s *InMemorySwapStore) Upload(projectName string, blob []byte) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.m[projectName] = blob
	return nil
}

func (s *InMemorySwapStore) Download(projectName string) ([]byte, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	blob, ok := s.m[projectName]
	if !ok {
		return nil, fmt.Errorf("cannot fetch %v", projectName)
	}
	// copy
	out := make([]byte, len(blob))
	copy(out, blob)
	return out, nil
}

func (s *InMemorySwapStore) Remove(projectName string) error {
	s.lock.Lock()
	delete(s.m, projectName)
	s.lock.Unlock()
	return nil
}

func (s *InMemorySwapStore) IsSafe() bool { return true }

// NoopSwapStore ports NoopSwapStore: safe=false so the Bridge will not be
// told swapping is on; every operation is a no-op.
type NoopSwapStore struct{}

func (NoopSwapStore) Upload(string, []byte) error { return nil }
func (NoopSwapStore) Download(string) ([]byte, error) {
	return nil, fmt.Errorf("noop swap store has no downloads (and is not safe)")
}
func (NoopSwapStore) Remove(string) error { return nil }
func (NoopSwapStore) IsSafe() bool        { return false }
