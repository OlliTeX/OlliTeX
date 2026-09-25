// Package persist — Go port of
//
//	services/history-v1/storage/lib/persist_changes.js
//
// and
//
//	services/history-v1/storage/lib/commit_changes.js  (Node oracle).
//
// The Node oracle is a small pipeline sitting between the chunkstore (the
// authoritative, mongo-backed chunk index) and the blob store. Go keeps the
// same shape but swaps the async redis/mongo plumbing for the synchronous,
// fake-backed Go ports (service/historystore, service/blobstore,
// service/chunkstore):
//
//	persistChanges(projectId, allChanges, limits, clientEndVersion):
//	  1. Filter "old" changes (timestamp < limits.minChangeTimestamp) and
//	     gate: any change older than limits.maxChangeTimestamp, more old
//	     changes than limits.maxChanges, or more old-change bytes than
//	     limits.maxChangeBytes -> persist; otherwise return nil (no-op).
//	  2. Load the latest chunk, verify its end version matches
//	     clientEndVersion, lazy-load its files, and apply its own changes on
//	     top of a cloned snapshot.
//	  3. Extend the latest chunk if possible (fillChunk + chunkstore.Update),
//	     then spin off new chunks as needed (fillChunk + chunkstore.Create).
//	  4. Return { numberOfChangesPersisted, originalEndVersion,
//	     currentChunk, resyncNeeded }.
//
// fillChunk — the heart of the port, mirrors the Node closure exactly:
//   - while changes remain and the chunk is under maxChunkChanges and (when
//     non-empty) the byte cap holds: take the first change, apply each of its
//     operations to the live snapshot (strict, so any error aborts), validate
//     each op's contentHash against the blob store (flip resyncNeeded on
//     mismatch), run the snapshot metadata tail (projectVersion /
//     v2DocVersions / timestamp), push the change onto the chunk, and
//     advance; a Timer warns when the fill exceeds maxChunkChangeTime ms.
//
// commitChanges (Node commit_changes.js) is a persist-or-resync dispatcher
// with five "levels" (0..4) driven by the redis persist buffer. Levels 1-4
// depend on the Redis backend, which is not ported; level 0 == persistChanges
// is the only portable path and is what the HTTP import/restore/
// set-content controllers actually use. It lives in commit.go.
package persist

import (
	"fmt"
	"log"
	"time"

	"history-v1/internal/assert"
	"history-v1/internal/contenthash"
	"history-v1/internal/core"
	"history-v1/service/blobstore"
	"history-v1/service/chunkstore"
)

// Limits — the persist gates. Mirrors Node's persist_limits object:
//
//	changeBucketMinutes, maxChanges, maxChangeBytes, maxChunkChanges,
//	maxChunkChangeBytes, maxChunkChangeTime   (numbers, 0 => default)
//	minChangeTimestamp, maxChangeTimestamp   (Date | undefined)
//
// Node `_.defaults` defaults (see storage/lib/persist_changes.js),
// applied only when the field is unset (0):
//
//	changeBucketMinutes 60, maxChanges 2500, maxChangeBytes 5 * 1024 * 1024,
//	maxChunkChanges 2000, maxChunkChangeBytes 5 * 1024 * 1024,
//	maxChunkChangeTime 5000 (ms).
//
// The two timestamps are pointers: nil == Node `undefined`, where the
// comparison `ts < undefined` is false — i.e. no changes qualify and nothing
// is persisted. Callers that want to force a persist pass a far-future date
// (see the HTTP import/restore controllers: farFuture = now + 7 days).
type Limits struct {
	ChangeBucketMinutes int
	MaxChanges          int
	MaxChangeBytes      int
	MaxChunkChanges     int
	MaxChunkChangeBytes int
	MaxChunkChangeTime  int
	MinChangeTimestamp  *time.Time
	MaxChangeTimestamp  *time.Time
}

// maxChanges — Node: limits.maxChanges || 2500.
func (l Limits) maxChanges() int {
	if l.MaxChanges <= 0 {
		return 2500
	}
	return l.MaxChanges
}

// maxChangeBytes — Node: limits.maxChangeBytes || 5 * 1024 * 1024.
func (l Limits) maxChangeBytes() int {
	if l.MaxChangeBytes <= 0 {
		return 5 * 1024 * 1024
	}
	return l.MaxChangeBytes
}

// maxChunkChanges — Node: limits.maxChunkChanges || 2000.
func (l Limits) maxChunkChanges() int {
	if l.MaxChunkChanges <= 0 {
		return 2000
	}
	return l.MaxChunkChanges
}

// maxChunkChangeBytes — Node: limits.maxChunkChangeBytes || 5 * 1024 * 1024.
func (l Limits) maxChunkChangeBytes() int {
	if l.MaxChunkChangeBytes <= 0 {
		return 5 * 1024 * 1024
	}
	return l.MaxChunkChangeBytes
}

// maxChunkChangeTime — Node: limits.maxChunkChangeTime || 5000 (ms).
func (l Limits) maxChunkChangeTime() int {
	if l.MaxChunkChangeTime <= 0 {
		return 5000
	}
	return l.MaxChunkChangeTime
}

// PersistResult — Node persistChanges return shape.
type PersistResult struct {
	NumberOfChangesPersisted int
	OriginalEndVersion       int
	CurrentChunk             *core.Chunk
	ResyncNeeded             bool
}

// Service — Node's persist_changes.js module state (a single module instance
// holds references to chunkStore + blobStore). In the Go port the controller
// (later api/) shares the same blobstore instance the chunkstore is built
// from, keeping the in-memory hermetic blob store consistent.
type Service struct {
	cs *chunkstore.Store
	bs *blobstore.Store
}

func NewService(cs *chunkstore.Store, bs *blobstore.Store) *Service {
	return &Service{cs: cs, bs: bs}
}

// InvalidChangeError — ports InvalidChangeError from storage/lib/errors.js.
type InvalidChangeError struct {
	Msg       string
	ProjectID string
	Path      string
}

func (e *InvalidChangeError) Error() string { return e.Msg }

// timer — Node Timer (process.hrtime based); time.Now()/time.Since port.
type timer struct{ start time.Time }

func (t timer) elapsed() int64 { return time.Since(t.start).Milliseconds() }

// checkElapsedTime — Node checkElapsedTime: logs "warning: slow chunk" when
// the fill took longer than limits.maxChunkChangeTime ms.
func checkElapsedTime(projectID string, limits Limits, t timer) {
	timeTaken := t.elapsed()
	if timeTaken > int64(limits.maxChunkChangeTime()) {
		log.Printf("warning: slow chunk %s %d", projectID, timeTaken)
	}
}

// countChangeBytes — Node: chunkStore.countChangeBytes(change) ==
// Buffer.byteLength(change.toRaw()). JSON-byte length in Go.
func countChangeBytes(c *core.Change) int {
	return len(c.ToRaw())
}

// totalChangeBytes — Node totalChangeBytes: 0 for empty, else sum.
func totalChangeBytes(changes []*core.Change) int {
	if len(changes) == 0 {
		return 0
	}
	n := 0
	for _, c := range changes {
		n += countChangeBytes(c)
	}
	return n
}

// --- PersistChanges — Node persistChanges(projectId, allChanges, limits,
// clientEndVersion). Returns (nil, nil) when nothing needs persisting.
//
// Ports the Node oracle faithfully:
//   - filter oldChanges (below minChangeTimestamp); nil timestamp gates mean
//     "no changes qualify" (Node `ts < undefined` is false).
//   - anyTooOld / tooManyChanges / tooManyBytes gate.
//   - loadLatestChunk (load latest + version-check + lazy-load files +
//     snapshot clone + applyAll)
//   - extendLastChunkIfPossible (fillChunk + checkElapsedTime + Update)
//   - createNewChunksAsNeeded (loop: fillChunk + checkElapsedTime + Create)
func (s *Service) PersistChanges(projectID string, allChanges []*core.Change, limits Limits, clientEndVersion int) (*PersistResult, error) {
	if a := assert.ProjectID(projectID, "persistChanges: bad projectId"); a != nil {
		return nil, a
	}
	if len(allChanges) == 0 {
		return nil, nil // Node: persistChanges with no changes -> null
	}

	// Node: oldChanges = _.filter(allChanges, c => c.getTimestamp() < limits.minChangeTimestamp)
	// With minChangeTimestamp undefined the comparison is false, so the filter
	// yields an empty list and (further below) nothing is persisted.
	oldChanges := make([]*core.Change, 0, len(allChanges))
	if limits.MinChangeTimestamp != nil {
		for _, c := range allChanges {
			if c.Timestamp.UnixMilli() < limits.MinChangeTimestamp.UnixMilli() {
				oldChanges = append(oldChanges, c)
			}
		}
	}
	// anyTooOld = _.some(oldChanges, c => c.getTimestamp() < limits.maxChangeTimestamp)
	anyTooOld := false
	if limits.MaxChangeTimestamp != nil {
		for _, c := range oldChanges {
			if c.Timestamp.UnixMilli() < limits.MaxChangeTimestamp.UnixMilli() {
				anyTooOld = true
				break
			}
		}
	}
	tooManyChanges := len(oldChanges) > limits.maxChanges()
	tooManyBytes := totalChangeBytes(oldChanges) > limits.maxChangeBytes()

	if !anyTooOld && !tooManyChanges && !tooManyBytes {
		return nil, nil // Node: return null
	}

	pbs := s.bs.Project(projectID)

	// --- loadLatestChunk ---
	latest, err := s.cs.LoadLatest(projectID)
	if err != nil {
		return nil, err
	}
	originalEndVersion := latest.GetEndVersion()
	if originalEndVersion != clientEndVersion {
		return nil, &core.ConflictingEndVersion{
			ClientEndVersion: clientEndVersion,
			LatestEndVersion: originalEndVersion,
		}
	}
	// Node loadLatest lazy-loads history files; Go LoadLatest does not, so we
	// load them onto the chunk ourselves before using the snapshot files.
	if err := latest.LoadFiles("lazy", pbs); err != nil {
		return nil, &InvalidChangeError{Msg: "load files: " + err.Error(), ProjectID: projectID}
	}
	currentSnapshot := latest.GetSnapshot().Clone()
	// Node: currentSnapshot.applyAll(currentChunk.getChanges()) — strict.
	if err := currentSnapshot.ApplyAll(latest.GetChanges()); err != nil {
		return nil, &InvalidChangeError{Msg: "applyAll: " + err.Error(), ProjectID: projectID}
	}
	earliestChangeTS := allChanges[0].Timestamp
	changesToPersist := oldChanges
	resyncNeeded := false
	currentChunk := latest

	// --- extendLastChunkIfPossible ---
	t := timer{start: time.Now()}
	pushed, err := s.fillChunk(projectID, pbs, currentSnapshot, currentChunk, &changesToPersist, limits, &resyncNeeded)
	if err != nil {
		return nil, err
	}
	if pushed {
		checkElapsedTime(projectID, limits, t)
		if _, err := s.cs.Update(projectID, currentChunk, earliestChangeTS); err != nil {
			return nil, err
		}
	}

	// --- createNewChunksAsNeeded ---
	for len(changesToPersist) > 0 {
		endVersion := currentChunk.GetEndVersion()
		newChunk := core.NewChunk(core.NewHistory(currentSnapshot.Clone(), nil), endVersion)
		t := timer{start: time.Now()}
		pushed, err := s.fillChunk(projectID, pbs, currentSnapshot, newChunk, &changesToPersist, limits, &resyncNeeded)
		if err != nil {
			return nil, err
		}
		if !pushed {
			return nil, fmt.Errorf("persist: failed to fill empty chunk")
		}
		checkElapsedTime(projectID, limits, t)
		if _, err := s.cs.Create(projectID, newChunk, earliestChangeTS); err != nil {
			return nil, err
		}
		currentChunk = newChunk
	}

	return &PersistResult{
		NumberOfChangesPersisted: len(oldChanges),
		OriginalEndVersion:       originalEndVersion,
		CurrentChunk:             currentChunk,
		ResyncNeeded:             resyncNeeded,
	}, nil
}

// fillChunk — Node fillChunk(chunk, changes). Returns whether any change was
// pushed onto the chunk. Ports the closure faithfully: the max-changes test
// and the byte guard run BEFORE applying (the first change is always
// accepted — the byte guard requires an existing change in the chunk), every
// operation is applied strictly to the live snapshot, each op's contentHash
// is validated against the blob store, the snapshot metadata tail is applied
// (matching Node Change.iterativelyApplyTo), and the change is recorded on
// the chunk with its byte count accumulated.
func (s *Service) fillChunk(projectID string, pbs core.BlobStoreI, currentSnapshot *core.Snapshot, chunk *core.Chunk, changes *[]*core.Change, limits Limits, resyncNeeded *bool) (bool, error) {
	totalBytes := totalChangeBytes(chunk.GetChanges())
	pushed := false
	for len(*changes) > 0 {
		// Node: if (chunk.getChanges().length >= limits.maxChunkChanges) break
		if len(chunk.GetChanges()) >= limits.maxChunkChanges() {
			break
		}
		change := (*changes)[0]
		changeBytes := countChangeBytes(change)
		// Node: byte guard (only when the chunk already has a change).
		if len(chunk.GetChanges()) > 0 && totalBytes+changeBytes > limits.maxChunkChangeBytes() {
			break
		}
		for _, op := range change.Operations {
			// Strict: any operation error aborts the persist (Node
			// iterativelyApplyTo with {strict: true}: all errors thrown).
			if err := op.ApplyTo(currentSnapshot); err != nil {
				return false, &InvalidChangeError{Msg: "change could not be applied: " + err.Error(), ProjectID: projectID}
			}
			// Node: validateContentHash(operation) — may set resyncNeeded.
			if err := s.validateContentHash(projectID, pbs, currentSnapshot, op, resyncNeeded); err != nil {
				return false, err
			}
		}
		// Node iterativelyApplyTo tail: projectVersion / v2DocVersions (merge)
		// / timestamp; applied BEFORE the change is pushed, exactly like Node.
		if change.ProjectVersion != "" {
			currentSnapshot.ProjectVersion = change.ProjectVersion
		}
		if change.V2DocVersions != nil {
			change.V2DocVersions.ApplyTo(currentSnapshot)
		}
		currentSnapshot.Timestamp = change.Timestamp

		// Node: chunk.pushChanges([change]); totalBytes += changeBytes; shift.
		chunk.PushChanges([]*core.Change{change})
		*changes = (*changes)[1:]
		totalBytes += changeBytes
		pushed = true
	}
	return pushed, nil
}

// validateContentHash — Node validateContentHash(operation). For an
// editFile op carrying a text op + contentHash, the actual content of the
// file after the edit is re-hashed and compared. A mismatch sets
// resyncNeeded; the hash is then cleared in all cases so it is never stored.
func (s *Service) validateContentHash(projectID string, pbs core.BlobStoreI, currentSnapshot *core.Snapshot, op *core.Operation, resyncNeeded *bool) error {
	if op.Kind != "editFile" {
		return nil
	}
	te := op.EditOp.TextOpForEdit()
	if te == nil || te.ContentHash == "" {
		return nil
	}
	path := op.Pathname
	file := currentSnapshot.GetFile(path)
	if file == nil {
		return &InvalidChangeError{Msg: "file not found for hash validation", ProjectID: projectID, Path: path}
	}
	loaded, err := file.Load("eager", pbs)
	if err != nil {
		return &InvalidChangeError{Msg: "load file for hash validation: " + err.Error(), ProjectID: projectID, Path: path}
	}
	// Go File.Load returns a NEW *File for lazy/hash kinds (Node File.load
	// mutates in place) — write it back so the snapshot sees the loaded file.
	if currentSnapshot.Files != nil {
		currentSnapshot.Files.Files[path] = loaded
	}
	// Node: content != null ? getContentHash(content) : null; binary/other
	// files have null content and hence a null hash (always a mismatch).
	var actualHash string
	if loaded.Kind == "string" {
		content, _ := loaded.GetContent(true)
		actualHash = contenthash.ContentHash(content)
	}
	if actualHash != te.ContentHash {
		*resyncNeeded = true
	}
	te.ContentHash = ""
	return nil
}
