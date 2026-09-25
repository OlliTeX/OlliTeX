// HistoryStore ports storage/lib/history_store.js:
//
//	getKey(projectId, chunkId)       = path.join(format(projectId), pad(chunkId))
//	storeRaw(projectId, chunkId, raw) = persistor.set(key, gzip(rawJSON))
//	loadRaw(projectId, chunkId)      = gunzip(persistor.get(key));
//	                                   missing -> chunk NotPersistedError
//	cloneChunk(pid, newPid, id)      = persistor.clone(oldKey -> newKey)
//	deleteChunks(projectId, chunkIds) = persistor.delete(key0, key1, ...)
//
// Keys use `projectKey.format(projectId)` (the "a/b/c" object-persistor
// prefix) and `projectKey.pad(chunkId)` (zero pad to 9). On POSIX the
// history_store key is `format(pid) + "/" + pad(cid)` (path.Join of two
// non-absolute parts).
package historystore

import (
	"bytes"
	"path"

	"history-v1/internal/core"
	"history-v1/internal/projectkey"
)

// HistoryStore is the byte-agnostic key-addressed raw-history store over a
// bucket on the fake persistor.
type HistoryStore struct {
	p      *FakePersister
	bucket string
}

// New: `new FakeHistoryStore(bucket, persistor, concurrency)`. Node's
// concurrency is irrelevant for the in-memory persistor and is dropped.
func New(p *FakePersister, bucket string) *HistoryStore {
	return &HistoryStore{p: p, bucket: bucket}
}

// Key: `getKey(projectId, chunkId)`.
func (s *HistoryStore) Key(projectID, chunkID string) string {
	return path.Join(projectkey.Format(projectID), projectkey.Pad(chunkID))
}

// StoreRaw: `storeRaw(projectId, chunkId, raw)`. Node gzips the *stringified*
// JSON raw; Go gzips the JSON bytes (same shape; `JSON.parse` on load is
// what matters).
func (s *HistoryStore) StoreRaw(projectID, chunkID string, raw []byte) error {
	gz, err := gzipCompress(raw)
	if err != nil {
		return err
	}
	return s.p.PutObject(s.bucket, s.Key(projectID, chunkID), gz)
}

// LoadRaw: `loadRaw(projectId, chunkId)`:
//
//	if (!(await persistor.contains(bucket, key)))
//		throw new NotPersistedError(projectId, chunkId)
//	raw = persistor.get(bucket, key)
//	return gunzip(raw)
func (s *HistoryStore) LoadRaw(projectID, chunkID string) ([]byte, error) {
	_, err := s.p.GetObject(s.bucket, s.Key(projectID, chunkID))
	if err != nil {
		if _, notFound := err.(*ErrObjectNotFound); notFound {
			return nil, &core.ChunkNotPersistedError{ProjectID: projectID}
		}
		return nil, err
	}
	data, _ := s.p.GetObject(s.bucket, s.Key(projectID, chunkID))
	out, err := gzipDecompress(data)
	if err != nil {
		if len(bytes.TrimSpace(data)) == 0 {
			// Node gunzip of an empty buffer -> empty Buffer.
			return []byte{}, nil
		}
		return nil, err
	}
	if len(out) == 0 {
		return []byte{}, nil
	}
	return out, nil
}

// Contains: persistor.contains(bucket, key).
func (s *HistoryStore) Contains(projectID, chunkID string) bool {
	_, err := s.p.GetObject(s.bucket, s.Key(projectID, chunkID))
	if err == nil {
		return true
	}
	_, notFound := err.(*ErrObjectNotFound)
	return !notFound
}

// CloneChunk: `cloneChunk(projectId, newProjectId, chunkId)`:
//
//	oldKey = getKey(projectId, chunkId)
//	newKey = getKey(newProjectId, chunkId)
//	persistor.clone(oldKey, newKey)
func (s *HistoryStore) CloneChunk(projectID, chunkID, newProjectID, newChunkID string) error {
	data, err := s.p.GetObject(s.bucket, s.Key(projectID, chunkID))
	if err != nil {
		return err
	}
	return s.p.PutObject(s.bucket, s.Key(newProjectID, newChunkID), data)
}

// DeleteChunks: `deleteChunks`: persistor.delete(key0, key1, ...).
func (s *HistoryStore) DeleteChunks(projectID string, chunkIDs []string) error {
	keys := make([]string, 0, len(chunkIDs))
	for _, cid := range chunkIDs {
		keys = append(keys, s.Key(projectID, cid))
	}
	return s.p.DeleteObject(s.bucket, keys...)
}
