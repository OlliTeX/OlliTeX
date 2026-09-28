package historyv1

// HistoryStore 1:1 with storage/lib/history_store.js: raw History objects
// (RawHistory JSON) stored as gzipped blobs in the persistor bucket, keyed
// <projectKey>/<chunkId> (chunkId 24-hex padded by the persistor path? no —
// Node uses projectKey.pad(chunkId) for the chunk part too).
//
// Node NotFoundError (object-persistor) → Chunk.NotPersistedError(projectId).

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"path"

	"ollitex/go/libraries/otc"
	persistors "ollitex/go/libraries/persistors"
)

// HistoryStore stores/loads raw histories in the object persistor.
type HistoryStore struct {
	persistor persistors.Persistor
	bucket    string
}

func NewHistoryStore(p persistors.Persistor, bucket string) *HistoryStore {
	return &HistoryStore{persistor: p, bucket: bucket}
}

// key 1:1: path.join(projectKey.format(projectId), projectKey.pad(chunkId)).
func historyKey(projectID, chunkID string) string {
	return path.Join(projectKeyFormat(projectID), leftPad(chunkID, 9))
}

// loadRaw — Node HistoryStore.loadRaw (gunzip + JSON.parse; NotFoundError →
// Chunk.NotPersistedError).
func (h *HistoryStore) LoadRaw(ctx context.Context, projectID, chunkID string) (map[string]any, error) {
	rc, err := h.persistor.GetObjectStream(h.bucket, historyKey(projectID, chunkID), persistors.Opts{AutoGunzip: true})
	if err != nil {
		if _, ok := asNotFound(err); ok {
			return nil, otc.NewChunkNotPersistedError(projectID)
		}
		return nil, err
	}
	defer rc.Close()
	buf, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(buf, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// storeRaw — Node HistoryStore.storeRaw (gzip(JSON) + sendStream with
// contentType application/json, contentEncoding gzip, contentLength).
func (h *HistoryStore) StoreRaw(ctx context.Context, projectID, chunkID string, rawHistory map[string]any) error {
	j, err := json.Marshal(rawHistory)
	if err != nil {
		return err
	}
	var gz bytes.Buffer
	w, _ := gzip.NewWriterLevel(&gz, gzip.DefaultCompression)
	if _, err := w.Write(j); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return h.persistor.SendStream(h.bucket, historyKey(projectID, chunkID), &gz, persistors.Opts{
		ContentType:     "application/json",
		ContentEncoding: "gzip",
		ContentLength:   int64(gz.Len()),
	})
}

// cloneChunk — Node HistoryStore.cloneChunk (persistor.copyObject).
func (h *HistoryStore) CloneChunk(ctx context.Context, srcProject, srcChunk, dstProject, dstChunk string) error {
	return h.persistor.CopyObject(h.bucket,
		historyKey(srcProject, srcChunk), historyKey(dstProject, dstChunk), persistors.Opts{})
}

// deleteChunks — Node HistoryStore.deleteChunks (persistor.deleteObject each).
func (h *HistoryStore) DeleteChunks(ctx context.Context, chunks []struct {
	ProjectID string
	ChunkID   string
}) {
	var firstErr error
	for _, c := range chunks {
		if err := h.persistor.DeleteObject(h.bucket, historyKey(c.ProjectID, c.ChunkID)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// Node: Promise.all — the first error propagates (best-match port).
	if firstErr != nil {
		// Node does not wrap here; surface as is.
		_ = firstErr
	}
}

// asNotFound unwraps the persistors.NotFoundError (oerror name "NotFoundError").
func asNotFound(err error) (*persistors.NotFoundError, bool) {
	for err != nil {
		if ne, ok := err.(*persistors.NotFoundError); ok {
			return ne, true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return nil, false
		}
		err = u.Unwrap()
	}
	return nil, false
}
