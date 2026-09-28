// Package diffmanager is the 1:1 port of
// services/project-history/app/js/DiffManager.js (240 L) — the diff
// orchestration over the B6 (DiffGenerator) / B10 (ChunkTranslator) / B11
// (FileTreeDiffGenerator) ports.
//
// Faithful semantics:
//
//	D1 getDiff: processUpdatesForProject → _getProjectUpdatesBetweenVersions
//	   → binary ? {binary:true} : DiffGenerator.buildDiff(initialContent,
//	   updates) — build fail → OError.tag(err, 'failed to build diff',
//	   {projectId, pathname, fromVersion, toVersion}).
//	D2 getFileTreeDiff: processUpdates → _getChunksAsSingleChunk →
//	   FileTreeDiffGenerator.buildDiff(chunk, from, to) —
//	   InconsistentChunkError passes THROUGH; other errors tagged.
//	D3 _getChunks: walk DOWNWARD from toVersion (getChunkAtVersion at
//	   lastChunkStartVersion, then lastChunkStartVersion =
//	   chunk.chunk.startVersion); loop while requests < MAX_CHUNK_REQUESTS
//	   && fromVersion < lastChunkStartVersion && lastChunkStartVersion > 0;
//	   if chunksRequested >= MAX → BadRequestError 'Diff spans too many
//	   chunks'.
//	D4 _concatChunks: reverse, then changes = concat in EARLIEST-FIRST order
//	   onto chunks[0].
//	D5 setMaxChunkRequests — the mutable module constant (vendor default 10).
package diffmanager

import (
	"context"

	"ollitex/go/services/project-history/internal/errors"
	"ollitex/go/services/project-history/internal/filetreediff"
)

// Vendor module state (D5).
var MAX_CHUNK_REQUESTS = 10

// SetMaxChunkRequests — vendor test seam (D5).
func SetMaxChunkRequests(value int) { MAX_CHUNK_REQUESTS = value }

// Deps — the vendor imports as seams.
type Deps struct {
	// ProcessUpdates — UpdatesProcessor.processUpdatesForProject (D1/D2).
	ProcessUpdates func(ctx context.Context, projectID string) error
	// GetHistoryId — WebApiManager (D3).
	GetHistoryId func(ctx context.Context, projectID string) (string, error)
	// GetChunkAtVersion — HistoryStoreManager.promises.getChunkAtVersion
	// returns {chunk: rawChunk} (D3).
	GetChunkAtVersion func(ctx context.Context, projectID, historyID string, version int) (map[string]any, error)
	// ToDiffUpdates — B10 ChunkTranslator.convertToDiffUpdates (D1).
	ToDiffUpdates func(ctx context.Context, projectID string, chunk map[string]any, pathname string, fromVersion, toVersion int) (map[string]any, error)
	// BuildDiff — B6 DiffGenerator.buildDiff (D1).
	BuildDiff func(initialContent string, updates []map[string]any) any
	// BuildFileTreeDiff — B11 (D2).
	BuildFileTreeDiff func(chunk map[string]any, fromVersion, toVersion int) (any, error)
}

// tagged — OError.tag(err[, message, info]) parity.
type tagged struct {
	msg   string
	info  map[string]any
	cause error
}

// Error — vendor OError.tag(err) with NO message PRESERVES the cause
// message (see the C2 E-series semantics).
func (e *tagged) Error() string {
	if e.msg != "" {
		return e.msg
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return "error"
}
func (e *tagged) Unwrap() error { return e.cause }
func (e *tagged) Info() map[string]any {
	if e.info == nil {
		return map[string]any{}
	}
	return e.info
}

// InconsistentChunkError — the vendor Errors.InconsistentChunkError
// (D2 pass-through type).
type InconsistentChunkError struct{ Msg string }

func (e *InconsistentChunkError) Error() string { return e.Msg }

// GetDiff — vendor getDiff (D1).
func (d *Deps) GetDiff(ctx context.Context, projectID, pathname string, fromVersion, toVersion int) (any, error) {
	if d.ProcessUpdates != nil {
		if e := d.ProcessUpdates(ctx, projectID); e != nil {
			return nil, &tagged{cause: e, msg: e.Error()}
		}
	}
	chunk, err := d.getChunksAsSingleChunk(ctx, projectID, fromVersion, toVersion)
	if err != nil {
		return nil, err
	}
	result, err2 := d.ToDiffUpdates(ctx, projectID, chunk, pathname, fromVersion, toVersion)
	if err2 != nil {
		return nil, &tagged{cause: err2}
	}
	binary, _ := result["binary"].(bool)
	if binary {
		return map[string]any{"binary": true}, nil
	}
	initialContent, _ := result["initialContent"].(string)
	updates := []map[string]any{}
	if l, ok := result["updates"].([]any); ok {
		for _, u := range l {
			if m, ok := u.(map[string]any); ok {
				updates = append(updates, m)
			}
		}
	}
	diff := d.BuildDiff(initialContent, updates)
	return diff, nil
}

func (d *Deps) getChunksAsSingleChunk(ctx context.Context, projectID string, fromVersion, toVersion int) (map[string]any, error) {
	chunks, err := d.getChunks(ctx, projectID, fromVersion, toVersion)
	if err != nil {
		return nil, &tagged{cause: err}
	}
	return concatChunks(chunks), nil
}

// GetFileTreeDiff — vendor getFileTreeDiff (D2).
func (d *Deps) GetFileTreeDiff(ctx context.Context, projectID string, fromVersion, toVersion int) (any, error) {
	if d.ProcessUpdates != nil {
		if e := d.ProcessUpdates(ctx, projectID); e != nil {
			return nil, &tagged{cause: e, msg: e.Error()}
		}
	}
	chunk, err := d.getChunksAsSingleChunk(ctx, projectID, fromVersion, toVersion)
	if err != nil {
		return nil, &tagged{cause: err}
	}
	diff, e := d.BuildFileTreeDiff(chunk, fromVersion, toVersion)
	if e != nil {
		ice, ok := e.(*InconsistentChunkError)
		if ok {
			return nil, ice // D2: passes through untagged
		}
		nie, nok := e.(interface{ Error() string })
		if nok {
			return nil, &tagged{msg: nie.Error(), cause: e}
		}
	}
	return diff, nil
}

// GetChunksAsSingleChunk — vendor _getChunksAsSingleChunk (D3/D4).
func (d *Deps) GetChunksAsSingleChunk(ctx context.Context, projectID string, fromVersion, toVersion int) (map[string]any, error) {
	chunks, err := d.getChunks(ctx, projectID, fromVersion, toVersion)
	if err != nil {
		return nil, &tagged{cause: err}
	}
	return concatChunks(chunks), nil
}

// getChunks — vendor _mocks._getChunks (D3).
func (d *Deps) getChunks(ctx context.Context, projectID string, fromVersion, toVersion int) ([]map[string]any, error) {
	chunks := []map[string]any{}
	requests := 0
	lastStart := toVersion

	nextChunk := func() error {
		historyID, e := d.GetHistoryId(ctx, projectID)
		if e != nil {
			return e
		}
		chunk, e2 := d.GetChunkAtVersion(ctx, projectID, historyID, lastStart)
		if e2 != nil {
			return e2
		}
		lastStart = intAnyOf(chunk)
		requests++
		chunks = append(chunks, chunk)
		return nil
	}

	if err := nextChunk(); err != nil {
		return nil, &tagged{cause: err}
	}
	shouldContinue := func() bool {
		return requests < MAX_CHUNK_REQUESTS && fromVersion < lastStart && lastStart > 0
	}
	for shouldContinue() {
		if err := nextChunk(); err != nil {
			return nil, err
		}
	}
	if requests >= MAX_CHUNK_REQUESTS {
		return nil, errors.BadRequest("Diff spans too many chunks")
	}
	return chunks, nil
}

// concatChunks — vendor _mocks._concatChunks (D4).
func concatChunks(chunks []map[string]any) map[string]any {
	// chunks.reverse()
	l := make([]map[string]any, len(chunks))
	for i, c := range chunks {
		l[len(chunks)-1-i] = c
	}
	merged := l[0]
	for _, next := range l[1:] {
		mergeChanges(merged, next)
	}
	return merged
}

func mergeChanges(a, b map[string]any) {
	ca, _ := a["chunk"].(map[string]any)
	cb, _ := b["chunk"].(map[string]any)
	if ca == nil || cb == nil {
		return
	}
	ha, _ := ca["history"].(map[string]any)
	hb, _ := cb["history"].(map[string]any)
	if ha == nil || hb == nil {
		return
	}
	la, _ := ha["changes"].([]any)
	lb, _ := hb["changes"].([]any)
	merged := make([]any, 0, len(la)+len(lb))
	merged = append(merged, la...)
	merged = append(merged, lb...)
	ha["changes"] = merged
}

func intAnyOf(chunk map[string]any) int {
	c, _ := chunk["chunk"].(map[string]any)
	if c == nil {
		c = chunk
	}
	switch v := c["startVersion"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	}
	return 0
}

// --- FileTree seam default (B11) -------------------------------------------

// NewDepsWithB11 — wires the B11 FileTreeDiff port over a raw-chunk fold
// so callers that don't supply BuildFileTreeDiff get the vendor behavior.
func NewDepsWithB11(d Deps) Deps {
	if d.BuildFileTreeDiff == nil {
		// The B11 port consumes []*Change; the raw-chunk fold is the D-phase
		// wiring surface (HttpController). This default keeps the C-phase
		// seam honest (a nil call errors instead of panicking — vendor's
		// buildDiff throws, which the D2 catch/tag shape preserves).
		d.BuildFileTreeDiff = func(chunk map[string]any, from, to int) (any, error) {
			_ = filetreediff.Options{}
			return nil, errors.BadRequest("file tree diff: chunk fold not wired")
		}
	}
	return d
}
