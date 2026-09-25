// Package blobstore — hermetic in-memory fake of the history service's
// BlobStore (Node: storage/lib/blob_store.js). Node instantiates
// `new BlobStore(projectId)` per project (S3/GCS + Postgres metadata
// backend); the fake partitions its maps per project so the same
// per-project instance semantics hold.
//
// Node BlobStore contract (service-side, used by persist level 0):
//
//	putString(string) -> hash          git blob hash of the UTF-8 bytes
//	getString(hash)   -> string|null   raw stored string
//	putObject(obj)    -> hash          git blob hash of the JSON bytes
//	getObject(hash)   -> object|null
//
// Node models not-found as `null`. This fake models not-found as a
// *BlobNotFoundError* so error paths (BlobNotFound / resync) are
// assertable; the not-found -> resync path is pinned in the persist
// tests by calling with a never-stored hash.
package blobstore

import (
	"encoding/json"
	"errors"
	"sync"

	ch "history-v1/internal/contenthash"
)

// utf16Length — UTF-16 code units in s (supplementary points count as 2).
// Local copy: this package must not import internal/core (import cycle via
// internal/core test files importing this package).
func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// ErrBlobNotFound — the blob is not in this store (Node: getString/getObject
// returns null).
var ErrBlobNotFound = errors.New("blobstore: blob not found")

// BlobNotFoundError — carries the offending hash (Node wraps a BlobNotFound
// error in persist).
type BlobNotFoundError struct {
	Hash string
}

func (e *BlobNotFoundError) Error() string { return "blob not found: " + e.Hash }

// ns — one project's partitions.
type ns struct {
	mu     sync.RWMutex
	blob   map[string]string // git hash -> raw string (UTF-8)
	objs   map[string][]byte // git hash -> canonical JSON bytes
	binary map[string][]byte // git hash -> arbitrary bytes
}

func (n *ns) putString(hash, content string) {
	n.mu.Lock()
	n.blob[hash] = content
	n.mu.Unlock()
}

func (n *ns) blobString(hash string) (string, error) {
	n.mu.RLock()
	v, ok := n.blob[hash]
	n.mu.RUnlock()
	if !ok {
		return "", &BlobNotFoundError{Hash: hash}
	}
	return v, nil
}

func (n *ns) putObject(hash string, canonicalJSON []byte) {
	n.mu.Lock()
	cp := make([]byte, len(canonicalJSON))
	copy(cp, canonicalJSON)
	n.objs[hash] = cp
	if _, exists := n.blob[hash]; !exists {
		n.blob[hash] = string(cp)
	}
	n.mu.Unlock()
}

func (n *ns) getObject(hash string) ([]byte, error) {
	n.mu.RLock()
	v, ok := n.objs[hash]
	n.mu.RUnlock()
	if !ok {
		return nil, &BlobNotFoundError{Hash: hash}
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (n *ns) putBytes(hash string, content []byte) {
	n.mu.Lock()
	cp := make([]byte, len(content))
	copy(cp, content)
	n.binary[hash] = cp
	if _, exists := n.blob[hash]; !exists {
		n.blob[hash] = string(cp)
	}
	n.mu.Unlock()
}

// Store — the shared backend (one per test/service instance).
type Store struct {
	mu sync.Mutex
	ns map[string]*ns
}

// NewStore — empty hermetic backend.
func NewStore() *Store {
	return &Store{ns: map[string]*ns{}}
}

// Project — a ProjectBlobStore (core.BlobStoreI) bound to one project id,
// mirroring Node's `new BlobStore(projectId)`.
func (s *Store) Project(projectID string) *ProjectBlobStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.ns[projectID]
	if !ok {
		n = &ns{
			blob:   map[string]string{},
			objs:   map[string][]byte{},
			binary: map[string][]byte{},
		}
		s.ns[projectID] = n
	}
	return &ProjectBlobStore{ProjectID: projectID, ns: n}
}

// NewFakeBlobStore — a single-namespace flat fake backed by a fresh Store.
// It satisfies core.BlobStoreI plus the plain putString/getString/putBytes
// surface used directly by core tests. (Multi-project paths use
// NewStore().Project(id).) This package does NOT import internal/core to
// avoid an import cycle (core's tests import this package); interface
// satisfaction is checked at the BlobStoreI call sites instead.
func NewFakeBlobStore() *ProjectBlobStore {
	return NewStore().Project("")
}

// Clone — Node BlobStore.clone(sourceProjectId, targetId): copy every blob
// from the source project into the target.
func (s *Store) Clone(sourceID, targetID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, tgt := s.ns[sourceID], s.ns[targetID]
	if src == nil || tgt == nil {
		return
	}
	src.mu.RLock()
	tgt.mu.Lock()
	for hash, v := range src.blob {
		if _, exists := tgt.blob[hash]; !exists {
			tgt.blob[hash] = v
		}
	}
	for hash, v := range src.objs {
		if _, exists := tgt.objs[hash]; !exists {
			tgt.objs[hash] = v
		}
	}
	for hash, v := range src.binary {
		if _, exists := tgt.binary[hash]; !exists {
			tgt.binary[hash] = v
		}
	}
	tgt.mu.Unlock()
	src.mu.RUnlock()
}

// ProjectBlobStore — a core.BlobStoreI bound to one project.
type ProjectBlobStore struct {
	ProjectID string
	ns        *ns
}

// PutString (BlobStoreI) — put a string blob.
func (s *ProjectBlobStore) PutString(content string) (string, error) {
	hash := ch.BlobHash(content)
	s.ns.putString(hash, content)
	return hash, nil
}

// GetString — raw stored string for hash; *BlobNotFoundError when absent.
func (s *ProjectBlobStore) GetString(hash string) (string, error) {
	return s.ns.blobString(hash)
}

// PutObject (BlobStoreI) — put an object blob. content must be the
// canonical JSON bytes (Node: JSON.stringify(obj) — Go callers marshal the
// struct themselves to preserve key order).
func (s *ProjectBlobStore) PutObject(canonicalJSON []byte) (string, error) {
	hash := ch.BlobHashForBytes(canonicalJSON)
	s.ns.putObject(hash, canonicalJSON)
	return hash, nil
}

// GetObject — object bytes for hash; *BlobNotFoundError when absent.
func (s *ProjectBlobStore) GetObject(hash string) ([]byte, error) {
	return s.ns.getObject(hash)
}

// PutBytes — put arbitrary bytes (binary blob, Node putBytes).
func (s *ProjectBlobStore) PutBytes(content []byte) (string, error) {
	hash := ch.BlobHashForBytes(content)
	s.ns.putBytes(hash, content)
	return hash, nil
}

// GetHashBlob (BlobStoreI) — content of the stored blob at `hash`. For the
// empty-string hash (EMPTY_HASH) the content is "" (still a valid stored
// value when present); *BlobNotFoundError when the hash is not a known
// string/blob.
func (s *ProjectBlobStore) GetHashBlob(hash string) (string, error) {
	return s.ns.blobString(hash)
}

// StringBlob (BlobStoreI) — blob metadata (Node Blob.getByteLength()/
// getStringLength()). byteLength is always known when found; stringLength
// is the UTF-16 length for string/object blobs and -1 for binary blobs
// (Node: stringLength undefined). EMPTY_HASH is special-cased like Node's
// EMPTY_BLOB{0,0} (answered without a backend lookup). found=false means
// the hash is not in this project's store (Node: getBlob -> null).
func (s *ProjectBlobStore) StringBlob(hash string) (byteLength, stringLength int, found bool) {
	if hash == ch.EMPTY_HASH {
		return 0, 0, true
	}
	if bin, ok := s.ns.binary[hash]; ok {
		return len(bin), -1, true
	}
	if v, ok := s.ns.blob[hash]; ok {
		return len(v), utf16Length(v), true
	}
	return 0, 0, false
}

// GetRangesBlob (BlobStoreI) — the JSON ranges blob of a lazy string file.
func (s *ProjectBlobStore) GetRangesBlob(hash string) (json.RawMessage, error) {
	v, err := s.GetObject(hash)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// Contains — bookkeeping: is this hash known as a string/object/blob.
func (s *ProjectBlobStore) Contains(hash string) bool {
	_, err := s.ns.blobString(hash)
	return err == nil
}

// --- API-layer surface (Node blob_store/index.js copyBlob + deleteBlobs,
// used by the HTTP blob/clone/delete controllers) ---

// BlobInfo — metadata for one stored blob (Node copyBlob(sourceBlob,
// targetProjectId) reads sourceBlob.getByteLength(), and the blob-stats
// endpoint reads isString() + byteLength over every project blob).
type BlobInfo struct {
	Hash       string
	ByteLength int
	IsString   bool
}

// ProjectBlobs — enumerate every blob of a project (string/object and
// binary partitions, deduplicated). Mirrors Node getProjectBlobsBatch for
// one project: the batch version is Postgres-dependent (its projectBlobs
// query), so the hermetic port enumerates the in-memory store instead.
// Insertion order is not guaranteed (Go map iteration), which matters only
// for order-sensitive consumers — nothing is.
func (s *Store) ProjectBlobs(projectID string) []BlobInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.ns[projectID]
	if !ok {
		return nil
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	seen := map[string]BlobInfo{}
	// Binary blobs are written to BOTH the binary and blob partitions
	// partition (putBytes mirrors), so classify from the binary partition
	// first, mirroring StringBlob's "binary wins" rule. A blob only in the blob partition is a
	// text blob (putString) and is IsString:true.
	for hash, v := range n.binary {
		seen[hash] = BlobInfo{Hash: hash, ByteLength: len(v), IsString: false}
	}
	for hash, v := range n.blob {
		if _, exists := seen[hash]; !exists {
			seen[hash] = BlobInfo{Hash: hash, ByteLength: len(v), IsString: true}
		}
	}
	out := make([]BlobInfo, 0, len(seen))
	for _, info := range seen {
		out = append(out, info)
	}
	return out
}

// CopyHash — Node copyBlob(sourceBlob, targetProjectId): copy the blob
// addressed by hash into the target project. No-op when either project has
// no namespace (Node returns a Blob object either way; callers only check
// for the blob's presence afterwards) and when the target already holds the
// hash (Node putString is a no-op when the key exists; existing keys are
// never overwritten).
func (s *Store) CopyHash(sourceID, targetID, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	src, tgt := s.ns[sourceID], s.ns[targetID]
	if src == nil || tgt == nil {
		return
	}
	src.mu.RLock()
	tgt.mu.Lock()
	defer func() {
		tgt.mu.Unlock()
		src.mu.RUnlock()
	}()
	if v, ok := src.blob[hash]; ok {
		if _, exists := tgt.blob[hash]; !exists {
			tgt.blob[hash] = v
		}
	}
	if v, ok := src.binary[hash]; ok {
		if _, exists := tgt.binary[hash]; !exists {
			tgt.binary[hash] = v
		}
	}
}

// DeleteProject — Node blobStore.deleteBlobs(): drop every blob of the
// project (string, object, and binary partitions). An unknown project is a
// no-op, matching Node's `return this.blobPromise` for a missing namespace.
func (s *Store) DeleteProject(projectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ns, projectID)
}
