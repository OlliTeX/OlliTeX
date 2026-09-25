// handlers.go — the store-backed HTTP handlers (Node controllers in
// services/history-v1/api/controllers/{projects,project_import}.js).
//
// Error semantics follow two tiers (documented in HANDOFF):
//   - controllers WITHOUT a try/catch let any error reach the expressify
//     terminal wrapper -> 500 (Go: handleAPIError default);
//   - controllers WITH a catch map specific error classes to 404 / 400 /
//     409 / 413 / 422 (Go: explicit renders below).
//
// Routes that the Node controller streams/produces on stores we cannot
// reproduce hermetically (IncrementalResponse clone stream, zip generation,
// blob streaming endpoints) stay terminal 501 "not ported".

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"history-v1/internal/core"
	"history-v1/service/persist"
)

// --- DELETE /api/projects/:id (Node deleteProject) ---
//
// Node: Promise.all([redisBuffer.hardDeleteProject, chunkStore.deleteProjectChunks,
// blobStore.deleteBlobs]). 204 on success. No catch -> 500 on store error.
// Hermetic: the redis hard-delete is absent (no buffer to wipe).
func (a *API) deleteProject(w http.ResponseWriter, pid string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	if err := a.cs.DeleteProjectChunks(pid); err != nil {
		handleAPIError(w, err)
		return
	}
	a.bs.DeleteProject(pid)
	w.WriteHeader(http.StatusNoContent)
}

// --- POST /api/projects/:id/clone (Node cloneProject) ---
//
// Node streams progress via IncrementalResponse (@overleaf/stream-utils, not
// vendored: the wire format is unknowable) and is untested at the API layer.
// A hermetic approximation would fabricate that format, so this stays
// terminal 501 (recorded decision).
func (a *API) cloneProject(w http.ResponseWriter, r *http.Request, pid string) {
	a.notPorted(w)
}

// --- GET /api/projects/:id/changes (Node getChanges) ---
//
// since: z.coerce.number().optional() — absent or "" -> 0 (Number("") === 0);
// unparseable -> 400 (zod); negative -> 400 {error: "Version out of bounds"}.
func (a *API) getChanges(w http.ResponseWriter, r *http.Request, pid string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	q := r.URL.Query()
	since := 0
	if vals, ok := q["since"]; ok && vals[0] != "" {
		n, err := strconv.Atoi(vals[0])
		if err != nil {
			// since: z.coerce.number().optional() — non-numeric NaN -> 422.
			(&validationErr{field: "since", statusCode: http.StatusUnprocessableEntity}).render(w)
			return
		}
		since = n
	}
	if since < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("Version out of bounds: %d", since)})
		return
	}
	changes, hasMore, err := a.cs.ChangesSince(pid, since, true)
	if err != nil {
		if isVersionOutOfBounds(err) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("Version out of bounds: %d", since)})
			return
		}
		handleAPIError(w, err)
		return
	}
	out := make([]json.RawMessage, 0, len(changes))
	for _, c := range changes {
		out = append(out, c.ToRaw())
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": out, "hasMore": hasMore})
}

// --- POST /api/projects/:id/changes + legacy_changes (Node importChanges) ---
//
// end_version: z.coerce.number() (required); return_snapshot:
// z.enum(['hashed','none']).optional() (default 'none'). Body: array of raw
// changes (mustFromRaw failures -> 422). persist limits force persistence
// (Node getPersistLimits: maxChanges 0, far-future bucket bounds); rollout is
// level 0 / no forced buffer in the hermetic port.
func (a *API) importChanges(w http.ResponseWriter, r *http.Request, pid string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	q := r.URL.Query()
	endVersion, verr := queryCoerceNumber(q, "end_version")
	if verr != nil {
		verr.render(w)
		return
	}
	returnSnapshot := q.Get("return_snapshot")
	if returnSnapshot == "" {
		returnSnapshot = "none"
	}
	if returnSnapshot != "none" && returnSnapshot != "hashed" {
		// Node z.enum(['none','hashed']) (query tier) -> 422.
		(&validationErr{field: "return_snapshot", statusCode: http.StatusUnprocessableEntity}).render(w)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var rawChanges []json.RawMessage
	if len(raw) > 0 && !json.Valid(raw) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "body must be an array of raw changes", "statusCode": 400})
		return
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &rawChanges); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "body must be an array of raw changes", "statusCode": 400})
			return
		}
	}
	changes := make([]*core.Change, 0, len(rawChanges))
	for _, rc := range rawChanges {
		c, err := core.ChangeMustFromRaw(rc)
		if err != nil {
			renderUnprocessableEntity(w)
			return
		}
		changes = append(changes, c)
	}
	result, err := a.persist.CommitChanges(pid, changes, farFutureLimits(), endVersion,
		persist.CommitOptions{HistoryBufferLevel: 0, ForcePersistBuffer: false})
	if err != nil {
		var cee *core.ConflictingEndVersion
		if isUnprocessable(err) || errors.As(err, &cee) {
			renderUnprocessableEntity(w)
			return
		}
		if isChunkNotFound(err) {
			renderNotFound(w)
			return
		}
		handleAPIError(w, err)
		return
	}
	if result == nil {
		// Node: persistChanges returns null when no changes were persisted
		// (empty set); the controller then dereferences `result.resyncNeeded`
		// (TypeError) → expressify terminal 500. Mirror that here.
		handleAPIError(w, fmt.Errorf("importChanges: no changes to persist"))
		return
	}
	if returnSnapshot == "none" {
		writeJSON(w, http.StatusCreated, map[string]any{"resyncNeeded": result.ResyncNeeded})
		return
	}
	// Node buildResultSnapshot(result && result.currentChunk): the persisted
	// chunk, falling back to loadLatest when it is absent.
	chunk := result.CurrentChunk
	if chunk == nil {
		chunk, err = a.cs.LoadLatest(pid)
		if err != nil {
			handleAPIError(w, err)
			return
		}
	}
	snapshot := chunk.GetSnapshot()
	if err := snapshot.ApplyAll(chunk.GetChanges()); err != nil {
		handleAPIError(w, err)
		return
	}
	// Node stores into HashCheckBlobStore (reads blobs, computes hashes,
	// persists nothing). The hermetic fake store deduplicates identical
	// content, so the plain project store is observationally equivalent.
	rawSnapshot, err := snapshot.Store(a.bs.Project(pid))
	if err != nil {
		handleAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rawSnapshot)
}

// --- POST /api/projects/:id/import + legacy_import (Node importSnapshot) ---
//
// Snapshot.fromRaw(body) failure -> 422; AlreadyInitialized -> 409;
// 200 {projectId} on success.
func (a *API) importSnapshot(w http.ResponseWriter, r *http.Request, pid string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	snapshot, err := core.SnapshotMustFromRaw(raw)
	if err != nil {
		renderUnprocessableEntity(w)
		return
	}
	if _, err := a.cs.Initialize(pid, snapshot); err != nil {
		if isConflict(err) {
			renderConflict(w)
			return
		}
		handleAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projectId": pid})
}

// --- POST /api/projects (Node initializeProject) ---
//
// Body {projectId?} — both optional. Absent or null projectId -> a fresh
// id is generated (hermetic stand-in for Node postgresBackend.generateProjectId).
// Invalid JSON or a non-string projectId -> 404 (schema validation, params). Re-initialize
// (id present with endVersion >= 0) -> 409 conflict.
func (a *API) initializeProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectID *string `json:"projectId"`
	}
	raw, _ := io.ReadAll(r.Body)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			(&validationErr{field: "project_id", statusCode: 404, isParamsErr: true}).render(w)
			return
		}
	}
	pid := ""
	if body.ProjectID != nil {
		pid = *body.ProjectID
	}
	if pid == "" {
		pid = a.generateID()
	}
	// cs.Initialize(pid, nil) — Node calls chunkStore.initializeProject(projectId)
	// with no snapshot; Go Initialize builds the empty snapshot internally.
	if _, err := a.cs.Initialize(pid, nil); err != nil {
		if isConflict(err) {
			renderConflict(w)
			return
		}
		handleAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projectId": pid})
}

// --- POST /api/projects/:id/set_content (Node setContent) ---
//
// ContentTooLarge -> 413; NotFound -> 404; op/file/pathname/blob errors ->
// 422 (Node catch list); the xor violations (SetContentXorError) are NOT in
// the Node catch list -> terminal 500; everything else 500.
func (a *API) setContent(w http.ResponseWriter, r *http.Request, pid string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	if len(raw) > 0 && !json.Valid(raw) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad request.json", "error": map[string]any{}})
		return
	}
	var body struct {
		Pathname     string          `json:"pathname"`
		Source       *string         `json:"source"`
		UserID       *string         `json:"userId"`
		Timestamp    *string         `json:"timestamp"`
		Metadata     json.RawMessage `json:"metadata"`
		TrackChanges *bool           `json:"trackChanges"`
		Content      *string         `json:"content"`
		BlobHash     *string         `json:"blobHash"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad request.json", "error": map[string]any{}})
		return
	}
	if body.Timestamp == nil {
		renderUnprocessableEntity(w)
		return
	}
	ts, err := timeParse(*body.Timestamp)
	if err != nil {
		renderUnprocessableEntity(w)
		return
	}
	opts := persist.BuildSetContentOpts{
		Content:      body.Content,
		BlobHash:     body.BlobHash,
		Metadata:     body.Metadata,
		Timestamp:    ts,
		TrackChanges: body.TrackChanges != nil && *body.TrackChanges,
	}
	if body.UserID != nil {
		opts.UserID = *body.UserID
	}
	if body.Source != nil && *body.Source != "" {
		opts.Origin = &core.Origin{Kind: *body.Source}
	}
	result, err := a.persist.BuildSetContentChange(pid, body.Pathname, opts)
	if err != nil {
		var ct *persist.ContentTooLargeError
		if errors.As(err, &ct) {
			renderRequestEntityTooLarge(w)
			return
		}
		if isChunkNotFound(err) {
			renderNotFound(w)
			return
		}
		// XOR violation (exactly one of content/blobHash) and trackChanges-
		// without-userId: over HTTP these are rejected at the Zod tier
		// (union+strict() + .refine) -> 422 before persist raises them.
		var xor *persist.SetContentXorError
		if errors.As(err, &xor) {
			renderUnprocessableEntity(w)
			return
		}
		var ue *core.UnprocessableError
		var ne *core.NotEditableError
		var pe *core.PathnameError
		var ed *core.EditMissingFileError
		var ic *persist.InvalidChangeError
		var snb *persist.ServiceBlobNotFoundError
		if errors.As(err, &ue) || errors.As(err, &ne) || errors.As(err, &pe) ||
			errors.As(err, &ed) || errors.As(err, &ic) || errors.As(err, &snb) {
			renderUnprocessableEntity(w)
			return
		}
		handleAPIError(w, err)
		return
	}
	changeOut := any(nil)
	if result.Change != nil {
		changeOut = result.Change.ToRaw()
	}
	writeJSON(w, http.StatusOK, map[string]any{"baseVersion": result.BaseVersion, "change": changeOut})
}

// --- POST /api/projects/:id/flush (Node flushChanges) ---
//
// persistBuffer is Redis-dependent (not hermetic); the observable contract is
// "the project exists + 200". Unknown project -> Chunk.NotFoundError -> 404.
func (a *API) flush(w http.ResponseWriter, r *http.Request, pid string) {
	if _, err := a.cs.GetLatestChunkMetadata(pid); err != nil {
		if isChunkNotFound(err) {
			renderNotFound(w)
			return
		}
		handleAPIError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// --- POST /api/projects/:id/expire (Node expireProject) ---
//
// Node redisBackend.expireProject — hermetic no-op, 200.
func (a *API) expire(w http.ResponseWriter, pid string) {
	w.WriteHeader(http.StatusOK)
}

// --- POST /api/projects/:id/blob-stats (Node getBlobStats) ---
//
// body {blobHashes?}; assert.blobHash per hash -> 422 (recorded); then the
// BatchBlobStore preload + text/binary split (getStringLength() != null).
func (a *API) getBlobStats(w http.ResponseWriter, r *http.Request, pid string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body struct {
		BlobHashes []string `json:"blobHashes"`
	}
	if len(raw) > 0 {
		if !json.Valid(raw) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "bad request.json", "error": map[string]any{}})
			return
		}
		_ = json.Unmarshal(raw, &body)
	}
	for _, h := range body.BlobHashes {
		if !hexHashRX.MatchString(h) {
			renderUnprocessableEntity(w)
			return
		}
	}
	pbs := a.bs.Project(pid)
	seen := map[string]struct{}{}
	for _, h := range body.BlobHashes {
		seen[h] = struct{}{}
	}
	var textBytes, binBytes, nText, nBin int
	for h := range seen {
		byteLength, stringLength, found := pbs.StringBlob(h)
		if !found {
			continue
		}
		if stringLength >= 0 {
			nText++
			textBytes += byteLength
		} else {
			nBin++
			binBytes += byteLength
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"projectId":       pid,
		"textBlobBytes":   textBytes,
		"binaryBlobBytes": binBytes,
		"totalBytes":      textBytes + binBytes,
		"nTextBlobs":      nText,
		"nBinaryBlobs":    nBin,
	})
}

// --- GET /api/projects/:id/latest/content (Node getLatestContent) ---
//
// NO try/catch: any error (including unknown project) -> 500.
func (a *API) getLatestContent(w http.ResponseWriter, pid string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	chunk, err := a.cs.LoadLatest(pid)
	if err != nil {
		handleAPIError(w, err)
		return
	}
	snapshot := chunk.GetSnapshot()
	if err := snapshot.ApplyAll(chunk.GetChanges()); err != nil {
		handleAPIError(w, err)
		return
	}
	if err := snapshot.LoadFiles("eager", a.bs.Project(pid)); err != nil {
		handleAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot.ToRaw())
}

// --- GET /api/projects/:id/latest/hashed_content (Node getLatestHashedContent) ---
//
// NO try/catch: any error -> 500. Node stores into HashCheckBlobStore (no
// persist); the hermetic fake store deduplicates, so the observable raw is
// the same.
func (a *API) getLatestHashedContent(w http.ResponseWriter, r *http.Request, pid string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	chunk, err := a.cs.LoadLatest(pid)
	if err != nil {
		handleAPIError(w, err)
		return
	}
	snapshot := chunk.GetSnapshot()
	if err := snapshot.ApplyAll(chunk.GetChanges()); err != nil {
		handleAPIError(w, err)
		return
	}
	if err := snapshot.LoadFiles("eager", a.bs.Project(pid)); err != nil {
		handleAPIError(w, err)
		return
	}
	raw, err := snapshot.Store(a.bs.Project(pid))
	if err != nil {
		handleAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, raw)
}

// --- GET /api/projects/:id/latest/history (Node getLatestHistory) ---

func (a *API) getLatestHistory(w http.ResponseWriter, pid string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	chunk, err := a.cs.LoadLatest(pid)
	if err != nil {
		if isChunkNotFound(err) {
			renderNotFound(w)
			return
		}
		handleAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chunk": chunk.ToRaw()})
}

// --- GET /api/projects/:id/latest/persistedHistory ---
//
// Node exports getLatestPersistedHistory = getLatestHistory.
func (a *API) getLatestPersistedHistory(w http.ResponseWriter, pid string) {
	a.getLatestHistory(w, pid)
}

// --- GET /api/projects/:id/latest/history/raw (Node getLatestHistoryRaw) ---
//
// The readOnly query flag is redis/persist-only and does not change the
// hermetic result.
func (a *API) getLatestHistoryRaw(w http.ResponseWriter, pid string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	meta, err := a.cs.GetLatestChunkMetadata(pid)
	if err != nil {
		if isChunkNotFound(err) {
			renderNotFound(w)
			return
		}
		handleAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"startVersion": meta.StartVersion,
		"endVersion":   meta.EndVersion,
		"endTimestamp": meta.EndTimestamp.UTC().Format("2006-01-02T15:04:05.000Z"),
	})
}

// --- GET /api/projects/:id/latest/zip (Node getLatestZip) ---
//
// The 404-on-unknown-project observable is preserved (loadLatest first);
// the zip stream itself is not hermetically reproducible (501).
func (a *API) getLatestZip(w http.ResponseWriter, pid string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	if _, err := a.cs.LoadLatest(pid); err != nil {
		if isChunkNotFound(err) {
			renderNotFound(w)
			return
		}
		handleAPIError(w, err)
		return
	}
	a.notPorted(w)
}

// --- GET /api/projects/:id/versions/:version/history (Node getHistory) ---

func (a *API) getHistory(w http.ResponseWriter, pid, version string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	v, verr := intParse(version)
	if verr != nil {
		verr.render(w)
		return
	}
	chunk, err := a.cs.LoadAtVersion(pid, v, false)
	if err != nil {
		if isChunkNotFound(err) {
			renderNotFound(w)
			return
		}
		handleAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chunk": chunk.ToRaw()})
}

// --- GET /api/projects/:id/versions/:version/content (Node getContentAtVersion) ---
//
// getSnapshotAtVersion: loadAtVersion; dropRight(changes, endVersion-v);
// applyAll when the tail is non-empty, else setTimestamp from the previous
// chunk metadata (Chunk.VersionNotFoundError is swallowed). NO try/catch:
// any other error -> 500.
func (a *API) getVersionContent(w http.ResponseWriter, pid, version string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	ver, verr := intParse(version)
	if verr != nil {
		verr.render(w)
		return
	}
	chunk, err := a.cs.LoadAtVersion(pid, ver, false)
	if err != nil {
		handleAPIError(w, err)
		return
	}
	snapshot := chunk.GetSnapshot()
	changes := chunk.GetChanges()
	n := chunk.GetEndVersion() - ver
	var tail []*core.Change
	switch {
	// dropRight(changes, 0) keeps every change (state at endVersion).
	case n == 0:
		tail = changes
	// 0 < n < len: the first len-n changes (state inside this chunk).
	case n > 0 && n < len(changes):
		tail = changes[len(changes)-n:]
	default: // n < 0 or n >= len: dropRight leaves nothing -> else branch.
		tail = nil
	}
	if len(tail) > 0 {
		if err := snapshot.ApplyAll(tail); err != nil {
			handleAPIError(w, err)
			return
		}
	} else {
		meta, merr := a.cs.GetChunkForVersionMetadata(pid, ver, false)
		if merr != nil {
			if !isChunkNotFound(merr) {
				handleAPIError(w, merr)
				return
			}
			// Node: swallow Chunk.VersionNotFoundError — first snapshot of the
			// first chunk has no timestamp; leave the snapshot's empty one.
		} else {
			snapshot.SetTimestamp(meta.EndTimestamp)
		}
	}
	if err := snapshot.LoadFiles("eager", a.bs.Project(pid)); err != nil {
		handleAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot.ToRaw())
}

// --- GET /api/projects/:id/version/:version/zip (Node getZip) ---

func (a *API) getZip(w http.ResponseWriter, pid, version string) {
	a.notPorted(w)
}

// --- POST /api/projects/:id/version/:version/zip (Node createZip) ---

func (a *API) createZip(w http.ResponseWriter, pid, version string) {
	a.notPorted(w)
}

// --- GET /api/projects/:id/timestamp/:timestamp/history (Node getHistoryBefore) ---

func (a *API) getHistoryBefore(w http.ResponseWriter, pid, timestamp string) {
	if v := projectIDValid(pid); v != nil {
		v.render(w)
		return
	}
	ts, verr := tsParse(timestamp)
	if verr != nil {
		verr.render(w)
		return
	}
	chunk, err := a.cs.LoadAtTimestamp(pid, ts)
	if err != nil {
		if isChunkNotFound(err) {
			renderNotFound(w)
			return
		}
		handleAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"chunk": chunk.ToRaw()})
}

// --- helpers ---

// queryCoerceNumber — ports zod z.coerce.number(): absent -> invalid (400);
// "" -> 0 (Number("") === 0); non-integer -> invalid (400).
// queryCoerceNumber — Node z.coerce.number() (REQUIRED query field, e.g.
// importChanges end_version): absent or non-numeric -> 422 (request tier);
// empty string coerces to 0 (Number("") === 0); "abc" is NaN -> 422. Use the
// inline optional-since parse for z.coerce.number().optional() (getChanges).
func queryCoerceNumber(q url.Values, key string) (int, *validationErr) {
	vals, ok := q[key]
	if !ok {
		return 0, &validationErr{field: key, statusCode: http.StatusUnprocessableEntity}
	}
	s := vals[0]
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, &validationErr{field: key, statusCode: http.StatusUnprocessableEntity}
	}
	return n, nil
}

// timeParse — a body timestamp string in the wire format (or RFC3339).
// Node does new Date(timestamp); unparseable -> caller renders 422.
func timeParse(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid timestamp: %s", s)
}
