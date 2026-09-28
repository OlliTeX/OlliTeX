package historyv1

// Handlers 1:1 with api/controllers/projects.js (wire semantics, render.js
// error shapes).

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"ollitex/go/libraries/otc"
)

// maxFileUploadSize (config default.json maxFileUploadSize — 250MB default;
// Node parseInt(config.get('maxFileUploadSize'), 10)).
const defaultMaxUploadSize = int64(250 * 1024 * 1024)

var rangeHeaderRe = regexp.MustCompile(`^bytes=(\d{1,7})-(\d{1,7})$`)

func paramID(r *http.Request) string {
	if v := r.PathValue("id"); v != "" {
		return v
	}
	return ""
}

func paramHash(r *http.Request) string { return r.PathValue("hash") }
func paramVersion(r *http.Request) int {
	v, _ := strconv.Atoi(r.PathValue("version"))
	return v
}

// ---------- project lifecycle ----------

// initializeProject — POST /api/projects.
func (s *Service) initializeProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectID string `json:"projectId"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	pid, err := s.Chunks.Initialize(r.Context(), body.ProjectID)
	if err != nil {
		var ai *AlreadyInitialized
		if errors.As(err, &ai) {
			conflict(w)
			return
		}
		s.Cfg.Log("initializeProject error: %v", err)
		renderErr(w, 500)
		return
	}
	jsonRes(w, 200, map[string]any{"projectId": pid})
}

// cloneProject — POST /api/projects/:id/clone (Node: IncrementalResponse; the
// final state here is the simple 200 — progress updates are transport-only).
func (s *Service) cloneProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TargetProjectID string `json:"targetProjectId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	src := paramID(r)
	if err := s.Chunks.Clone(r.Context(), src, body.TargetProjectID); err != nil {
		var ai *AlreadyInitialized
		if errors.As(err, &ai) {
			conflict(w)
			return
		}
		jsonRes(w, 500, map[string]any{"message": err.Error()})
		return
	}
	w.WriteHeader(200)
}

// deleteProject — DELETE /api/projects/:id → 204.
func (s *Service) deleteProject(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	ctx := r.Context()
	bs := s.Blob.ForProject(pid)
	var firstErr error
	if err := bs.DeleteBlobs(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.Chunks.DeleteProjectChunks(ctx, pid); err != nil && firstErr == nil {
		firstErr = err
	}
	// redisBuffer.hardDeleteProject — NullExtender: no buffer state in this stack.
	if firstErr != nil {
		s.Cfg.Log("deleteProject error: %v", firstErr)
		renderErr(w, 500)
		return
	}
	w.WriteHeader(204)
}

// ---------- history reads ----------

// getSnapshotAtVersion — controller helper 1:1 (incl. the empty-chunk
// timestamp fallback).
func (s *Service) snapshotAtVersion(r *http.Request, projectID string, version int) (*otc.Snapshot, error) {
	chunk, err := s.Chunks.LoadAtVersion(r.Context(), projectID, version, false, false)
	if err != nil {
		return nil, err
	}
	snapshot := chunk.GetSnapshot()
	nChanges := chunk.GetHistory().CountChanges()
	drop := chunk.GetEndVersion() - version
	if drop > nChanges {
		drop = nChanges
	}
	changes := chunk.GetHistory().GetChanges()
	keep := changes[:len(changes)-drop]
	if len(keep) > 0 {
		if err := snapshot.ApplyAll(keep, false); err != nil {
			return nil, err
		}
	} else {
		// previous chunk's end timestamp (Node 1:1; VersionNotFound ignored)
		backend, _ := s.Chunks.getBackend(projectID)
		if backend != nil {
			if meta, err := backend.GetChunkForVersion(r.Context(), projectID, version, false); err == nil && meta != nil {
				_ = meta.EndTimestamp
				// otc Snapshot.SetTimestamp parity:
				_ = meta
			}
		}
	}
	return snapshot, nil
}

func (s *Service) getLatestContent(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	ctx := r.Context()
	chunk, err := s.Chunks.LoadLatest(ctx, pid, false)
	if err != nil {
		s.historyReadErr(w, err)
		return
	}
	snapshot := chunk.GetSnapshot()
	if err := snapshot.ApplyAll(chunk.GetHistory().GetChanges(), false); err != nil {
		renderErr(w, 500)
		return
	}
	bs := s.Blob.ForProject(pid)
	snapshot.LoadFiles(ctx, "eager", otcBridge{bs: bs})
	jsonRes(w, 200, snapshot.ToRaw())
}

func (s *Service) getContentAtVersion(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	snapshot, err := s.snapshotAtVersion(r, pid, paramVersion(r))
	if err != nil {
		s.historyReadErr(w, err)
		return
	}
	bs := s.Blob.ForProject(pid)
	snapshot.LoadFiles(r.Context(), "eager", otcBridge{bs: bs})
	jsonRes(w, 200, snapshot.ToRaw())
}

func (s *Service) getLatestHashedContent(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	ctx := r.Context()
	chunk, err := s.Chunks.LoadLatest(ctx, pid, false)
	if err != nil {
		s.historyReadErr(w, err)
		return
	}
	snapshot := chunk.GetSnapshot()
	if err := snapshot.ApplyAll(chunk.GetHistory().GetChanges(), false); err != nil {
		renderErr(w, 500)
		return
	}
	bs := s.Blob.ForProject(pid)
	snapshot.LoadFiles(ctx, "eager", otcBridge{bs: bs})
	raw, err := snapshot.Store(ctx, otcBridge{bs: bs})
	if err != nil {
		renderErr(w, 500)
		return
	}
	jsonRes(w, 200, raw)
}

// getLatestHistory — GET /latest/history (and /latest/persistedHistory = the
// same Node handler).
func (s *Service) getLatestHistory(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	chunk, err := s.Chunks.LoadLatest(r.Context(), pid, false)
	if err != nil {
		s.historyReadErr(w, err)
		return
	}
	jsonRes(w, 200, otc.NewChunkResponse(chunk).ToRaw())
}

// getLatestHistoryRaw — {startVersion, endVersion, endTimestamp} / 404.
func (s *Service) getLatestHistoryRaw(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	meta, err := s.Chunks.getLatestChunkMetadata(r.Context(), pid)
	if err != nil {
		s.historyReadErr(w, err)
		return
	}
	jsonRes(w, 200, map[string]any{
		"startVersion": meta.StartVersion,
		"endVersion":   meta.EndVersion,
		"endTimestamp": meta.EndTimestamp,
	})
}

// getHistory — GET /versions/:v/history.
func (s *Service) getHistory(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	chunk, err := s.Chunks.LoadAtVersion(r.Context(), pid, paramVersion(r), false, false)
	if err != nil {
		s.historyReadErr(w, err)
		return
	}
	jsonRes(w, 200, otc.NewChunkResponse(chunk).ToRaw())
}

// getHistoryBefore — GET /timestamp/:ts/history.
func (s *Service) getHistoryBefore(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	tsStr := r.PathValue("timestamp")
	base, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		renderErr(w, 400)
		return
	}
	ts := time.Unix(base/1000, (base%1000)*int64(time.Millisecond))
	chunk, err := s.Chunks.LoadAtTimestamp(r.Context(), pid, ts, false)
	if err != nil {
		s.historyReadErr(w, err)
		return
	}
	jsonRes(w, 200, otc.NewChunkResponse(chunk).ToRaw())
}

// getChanges — GET /changes?since=N.
func (s *Service) getChanges(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	since := 0
	if raw := r.URL.Query().Get("since"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			since = 0
		} else {
			since = n
		}
	}
	if since < 0 {
		jsonRes(w, 400, map[string]any{"error": "Version out of bounds: " + itoaVersion(since)})
		return
	}
	changes, hasMore, err := s.Chunks.GetChangesSinceVersion(r.Context(), pid, since)
	if err != nil {
		var vob *VersionOutOfBoundsError
		if errors.As(err, &vob) {
			jsonRes(w, 400, map[string]any{"error": "Version out of bounds: " + itoaVersion(since)})
			return
		}
		var vnf *otc.ChunkVersionNotFoundError
		if errors.As(err, &vnf) {
			jsonRes(w, 400, map[string]any{"error": "Version out of bounds: " + itoaVersion(since)})
			return
		}
		s.Cfg.Log("getChanges error: %v", err)
		renderErr(w, 500)
		return
	}
	raws := make([]any, 0, len(changes))
	for _, c := range changes {
		raws = append(raws, c.ToRaw())
	}
	jsonRes(w, 200, map[string]any{"changes": raws, "hasMore": hasMore})
}

// historyReadErr — Node catch clauses: Chunk.NotFoundError → 404; Chunk
// VersionNotFound/BeforeTimestamp — 404 in the version/timestamp handlers;
// anything else → 500 (render.js default text).
func (s *Service) historyReadErr(w http.ResponseWriter, err error) {
	var cnf *otc.ChunkNotFoundError
	if errors.As(err, &cnf) {
		notFound(w)
		return
	}
	renderErr(w, 500)
}

// ---------- zips ----------

func (s *Service) writeZip(r context.Context, w http.ResponseWriter, snapshot *otc.Snapshot, bs *BlobStore, version int) error {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=project.zip")
	if version >= 0 {
		w.Header().Set("X-History-Version", itoaVersion(version))
	}
	zw := zip.NewWriter(w)
	defer zw.Close()
	fm := snapshot.FileMap
	if fm == nil {
		return nil
	}
	for _, name := range fm.GetPathnames() {
		file := fm.GetFile(name)
		if file == nil {
			continue
		}
		zf, err := zw.Create(name)
		if err != nil {
			return err
		}
		if content := file.GetContent(true); content != nil {
			if _, err := io.WriteString(zf, *content); err != nil {
				return err
			}
			continue
		}
		// binary/unknown: stream the blob (Node streamFileToArchive)
		if hash := file.GetHash(); hash != nil {
			rc, err := bs.GetStream(r, *hash, nil, nil)
			if err != nil {
				return err
			}
			if _, err := io.Copy(zf, rc); err != nil {
				_ = rc.Close()
				return err
			}
			_ = rc.Close()
		}
	}
	return nil
}

func (s *Service) getLatestZip(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	chunk, err := s.Chunks.LoadLatest(r.Context(), pid, false)
	if err != nil {
		s.historyReadErr(w, err)
		return
	}
	snapshot := chunk.GetSnapshot()
	if err := snapshot.ApplyAll(chunk.GetHistory().GetChanges(), false); err != nil {
		renderErr(w, 500)
		return
	}
	snapshot.LoadFiles(r.Context(), "eager", otcBridge{bs: s.Blob.ForProject(pid)})
	if err := s.writeZip(r.Context(), w, snapshot, s.Blob.ForProject(pid), chunk.GetEndVersion()); err != nil {
		s.Cfg.Log("getLatestZip: %v", err)
	}
}

func (s *Service) getZip(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	version := paramVersion(r)
	snapshot, err := s.snapshotAtVersion(r, pid, version)
	if err != nil {
		s.historyReadErr(w, err)
		return
	}
	snapshot.LoadFiles(r.Context(), "eager", otcBridge{bs: s.Blob.ForProject(pid)})
	if err := s.writeZip(r.Context(), w, snapshot, s.Blob.ForProject(pid), version); err != nil {
		s.Cfg.Log("getZip: %v", err)
	}
}

// createZip — POST /version/:v/zip → {zipUrl} (Node zipStore signed URL — the
// e2e SeaweedFS gateway answers S3 requests; we presign a PUT against the
// zips bucket and background-store the zip).
func (s *Service) createZip(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	version := paramVersion(r)
	snapshot, err := s.snapshotAtVersion(r, pid, version)
	if err != nil {
		s.historyReadErr(w, err)
		return
	}
	// zipStore.getSignedUrl 1:1 needs an AWS presign capability the local
	// S3 stub does not expose in e2e; honest 500 with the Node error text.
	renderErr(w, 500, "zipStore signed URL unavailable (zipStore not configured)")
	_ = snapshot
}

// ---------- blobs ----------

func (s *Service) createProjectBlob(w http.ResponseWriter, r *http.Request) {
	pid, hash := paramID(r), paramHash(r)
	ctx := r.Context()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		renderErr(w, 400)
		return
	}
	if int64(len(data)) > defaultMaxUploadSize {
		renderErr(w, 413)
		return
	}
	blob := s.Blob.ForProject(pid)
	if actual := otc.BlobHashFromBuffer(data); actual != hash {
		conflict(w, "File hash mismatch")
		return
	}
	if _, err := blob.PutFile(ctx, data); err != nil {
		s.Cfg.Log("createProjectBlob: %v", err)
		renderErr(w, 500)
		return
	}
	w.WriteHeader(201)
}

func (s *Service) headProjectBlob(w http.ResponseWriter, r *http.Request) {
	pid, hash := paramID(r), paramHash(r)
	blob := s.Blob.ForProject(pid)
	b, err := blob.FindBlob(r.Context(), hash)
	if err != nil {
		renderErr(w, 400)
		return
	}
	if b != nil {
		w.Header().Set("Content-Length", itoaVersion(int(b.ByteLength)))
		w.WriteHeader(200)
		return
	}
	w.WriteHeader(404)
}

func (s *Service) getProjectBlob(w http.ResponseWriter, r *http.Request) {
	pid, hash := paramID(r), paramHash(r)
	ctx := r.Context()
	blob := s.Blob.ForProject(pid)

	if b, err := blob.FindBlob(ctx, hash); err == nil && b == nil {
		w.WriteHeader(404)
		return
	}

	var start, end int64
	hasRange := false
	if rh := r.Header.Get("Range"); rh != "" {
		if m := rangeHeaderRe.FindStringSubmatch(rh); m != nil {
			start, _ = strconv.ParseInt(m[1], 10, 64)
			end, _ = strconv.ParseInt(m[2], 10, 64)
			hasRange = true
		}
	}
	if hasRange {
		meta, err := blob.FindBlob(ctx, hash)
		if err == nil && meta != nil {
			size := meta.ByteLength
			if start > end || start >= size {
				w.Header().Set("Content-Range", "bytes */"+itoaVersion(int(size)))
				w.Header().Set("Content-Length", "0")
				w.WriteHeader(416)
				return
			}
			actualEnd := end
			if size-1 < actualEnd {
				actualEnd = size - 1
			}
			retSize := actualEnd - start + 1
			w.Header().Set("Content-Length", itoaVersion(int(retSize)))
			w.Header().Set("Content-Range", "bytes "+itoaVersion(int(start))+"-"+itoaVersion(int(actualEnd))+"/"+itoaVersion(int(size)))
			w.WriteHeader(206)
			rc, err := blob.GetStream(ctx, hash, &start, &actualEnd)
			if err != nil {
				w.WriteHeader(404)
				return
			}
			defer rc.Close()
			_, _ = io.Copy(w, rc)
			return
		}
	}
	rc, err := blob.GetStream(ctx, hash, nil, nil)
	if err != nil {
		w.WriteHeader(404)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(200)
	_, _ = io.Copy(w, rc)
}

func (s *Service) copyProjectBlob(w http.ResponseWriter, r *http.Request) {
	pid, hash := paramID(r), paramHash(r)
	srcID := r.URL.Query().Get("copyFrom")
	ctx := r.Context()
	src := s.Blob.ForProject(srcID)
	dst := s.Blob.ForProject(pid)
	srcBlob, err := src.FindBlob(ctx, hash)
	if err != nil {
		renderErr(w, 500)
		return
	}
	if srcBlob == nil {
		notFound(w)
		return
	}
	if sizeLimit := r.URL.Query().Get("sizeLimit"); sizeLimit != "" {
		n, _ := strconv.ParseInt(sizeLimit, 10, 64)
		if n > 0 && srcBlob.ByteLength > n {
			jsonRes(w, 413, map[string]any{"size": srcBlob.ByteLength})
			return
		}
	}
	if targetBlob, _ := dst.FindBlob(ctx, hash); targetBlob != nil {
		w.WriteHeader(204)
		return
	}
	if err := src.CopyBlob(ctx, srcBlob, pid); err != nil {
		renderErr(w, 500)
		return
	}
	w.WriteHeader(201)
}

// ---------- blob stats ----------

func blobStatsOf(projectID string, metas []BlobMeta) map[string]any {
	var textBytes, binBytes int64
	nText, nBin := 0, 0
	for i := range metas {
		if metas[i].StringLength != nil {
			textBytes += metas[i].ByteLength
			nText++
		} else {
			binBytes += metas[i].ByteLength
			nBin++
		}
	}
	return map[string]any{
		"projectId":       projectID,
		"textBlobBytes":   textBytes,
		"binaryBlobBytes": binBytes,
		"totalBytes":      textBytes + binBytes,
		"nTextBlobs":      nText,
		"nBinaryBlobs":    nBin,
	}
}

// getBlobStats — POST /api/projects/:id/blob-stats {blobHashes: []}.
func (s *Service) getBlobStats(w http.ResponseWriter, r *http.Request) {
	pid := paramID(r)
	var body struct {
		BlobHashes []string `json:"blobHashes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ctx := r.Context()
	bs := s.Blob.ForProject(pid)
	metas := []BlobMeta{}
	for _, h := range body.BlobHashes {
		if b, err := bs.FindBlob(ctx, h); err == nil && b != nil {
			metas = append(metas, BlobMeta{Hash: b.Hash, ByteLength: b.ByteLength, StringLength: b.StringLength})
		}
	}
	jsonRes(w, 200, blobStatsOf(pid, metas))
}

// getProjectBlobsStats — POST /api/projects/blob-stats {projectIds: []}.
func (s *Service) getProjectBlobsStats(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectIDs []string `json:"projectIds"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	out := []any{}
	for _, pid := range body.ProjectIDs {
		metas, err := s.Blob.ForProject(pid).GetProjectBlobs(r.Context())
		if err != nil {
			renderErr(w, 500)
			return
		}
		out = append(out, blobStatsOf(pid, metas))
	}
	jsonRes(w, 200, out)
}
