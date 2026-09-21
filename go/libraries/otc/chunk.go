package otc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	oerror "ollitex/go/libraries/oerror"
)

// ConflictingEndVersion (Node: chunk.js `class ConflictingEndVersion`).
type ConflictingEndVersion struct {
	ClientEndVersion int
	LatestEndVersion int
}

func NewChunkConflictingEndVersion(clientEndVersion, latestEndVersion int) error {
	return &ConflictingEndVersion{
		ClientEndVersion: clientEndVersion,
		LatestEndVersion: latestEndVersion,
	}
}

func (e *ConflictingEndVersion) Error() string {
	return "client sent updates with end_version " + strconv.Itoa(e.ClientEndVersion) +
		" but latest chunk has end_version " + strconv.Itoa(e.LatestEndVersion)
}

// ChunkNotFoundError (Node: `class NotFoundError`). Message can be overridden by
// subclasses; "" falls back to the Node default.
type ChunkNotFoundError struct {
	ProjectID string
	Message   string
}

func (e *ChunkNotFoundError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "no chunks for project " + e.ProjectID
}

// ChunkVersionNotFoundError (Node: `class VersionNotFoundError`).
type ChunkVersionNotFoundError struct {
	ProjectID string
	Version   string
}

func NewChunkVersionNotFoundError(projectID, version string) error {
	return &ChunkVersionNotFoundError{ProjectID: projectID, Version: version}
}

func (e *ChunkVersionNotFoundError) Error() string {
	return "chunk for " + e.ProjectID + " v " + e.Version + " not found"
}

// ChunkBeforeTimestampNotFoundError (Node: `class BeforeTimestampNotFoundError`).
type ChunkBeforeTimestampNotFoundError struct {
	ProjectID string
	Timestamp any
}

func NewChunkBeforeTimestampNotFoundError(projectID string, timestamp any) error {
	return &ChunkBeforeTimestampNotFoundError{ProjectID: projectID, Timestamp: timestamp}
}

func (e *ChunkBeforeTimestampNotFoundError) Error() string {
	return "chunk for " + e.ProjectID + " timestamp " + fmt.Sprint(e.Timestamp) + " not found"
}

// ChunkNotPersistedError (Node: `class NotPersistedError`).
type ChunkNotPersistedError struct {
	ProjectID string
}

func NewChunkNotPersistedError(projectID string) error {
	return &ChunkNotPersistedError{ProjectID: projectID}
}

func (e *ChunkNotPersistedError) Error() string {
	return "chunk for " + e.ProjectID + " not persisted yet"
}

// Chunk mirrors lib/chunk.js: a History that is part of a project's overall
// history, with a startVersion that places it in context.
type Chunk struct {
	History      *History
	StartVersion int
}

// NewChunk mirrors `new Chunk(history, startVersion)`.
func NewChunk(history *History, startVersion int) *Chunk {
	return &Chunk{History: history, StartVersion: startVersion}
}

// ChunkFromRaw mirrors `Chunk.fromRaw`.
func ChunkFromRaw(raw map[string]any) (*Chunk, error) {
	hRaw, ok := raw["history"].(map[string]any)
	if !ok {
		return nil, oerror.New("bad raw.history", nil)
	}
	h, err := HistoryFromRaw(hRaw)
	if err != nil {
		return nil, err
	}
	sv, ok := raw["startVersion"].(int)
	if !ok {
		return nil, oerror.New("bad startVersion", nil)
	}
	return NewChunk(h, sv), nil
}

// ToRaw (Node: toRaw).
func (c *Chunk) ToRaw() map[string]any {
	return map[string]any{
		"history":      c.History.ToRaw(),
		"startVersion": c.StartVersion,
	}
}

// GetHistory (Node: getHistory).
func (c *Chunk) GetHistory() *History { return c.History }

// GetSnapshot (Node: getSnapshot).
func (c *Chunk) GetSnapshot() *Snapshot { return c.History.GetSnapshot() }

// GetChanges (Node: getChanges).
func (c *Chunk) GetChanges() []*Change { return c.History.GetChanges() }

// PushChanges (Node: pushChanges).
func (c *Chunk) PushChanges(changes []*Change) { c.History.PushChanges(changes) }

// GetEndVersion (Node: getEndVersion) — startVersion + number of changes.
func (c *Chunk) GetEndVersion() int { return c.StartVersion + c.History.CountChanges() }

// GetEndTimestamp (Node: getEndTimestamp) — nil (Node: null) when there are no
// changes, else the last change's timestamp.
func (c *Chunk) GetEndTimestamp() *time.Time {
	if c.History.CountChanges() == 0 {
		return nil
	}
	cs := c.History.GetChanges()
	ts := cs[len(cs)-1].GetTimestamp()
	return &ts
}

// GetStartVersion (Node: getStartVersion).
func (c *Chunk) GetStartVersion() int { return c.StartVersion }

// LoadFiles (Node: loadFiles) — delegates to the history.
func (c *Chunk) LoadFiles(ctx context.Context, kind string, bs BlobStore) error {
	return c.History.LoadFiles(ctx, kind, bs)
}
