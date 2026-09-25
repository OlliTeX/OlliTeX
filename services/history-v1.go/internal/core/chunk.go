package core

import (
	"encoding/json"
	"fmt"
	"time"
)

// --- Chunk error taxonomy (ports chunk.js OError subclasses) ---
//
// Node OError message + info objects, locked against the Node oracle:
//
//	NotFoundError               "no chunks for project {p}"
//	VersionNotFoundError        "chunk for {p} v {n} not found"
//	BeforeTimestampNotFoundError "chunk for {p} timestamp {ts} not found"
//	NotPersistedError           "chunk for {p} not persisted yet"
//
// (ConflictingEndVersion already lives in origin.go.)

// ChunkNotFoundError — base Node Chunk.NotFoundError.
type ChunkNotFoundError struct{ ProjectID string }

func (e *ChunkNotFoundError) Error() string {
	return fmt.Sprintf("no chunks for project %s", e.ProjectID)
}

// ChunkVersionNotFoundError — Node Chunk.VersionNotFoundError.
type ChunkVersionNotFoundError struct {
	ProjectID string
	Version   int
}

func (e *ChunkVersionNotFoundError) Error() string {
	return fmt.Sprintf("chunk for %s v %d not found", e.ProjectID, e.Version)
}

// ChunkBeforeTimestampNotFoundError — Node Chunk.BeforeTimestampNotFoundError.
type ChunkBeforeTimestampNotFoundError struct {
	ProjectID string
	Timestamp string
}

func (e *ChunkBeforeTimestampNotFoundError) Error() string {
	return fmt.Sprintf("chunk for %s timestamp %s not found", e.ProjectID, e.Timestamp)
}

// ChunkNotPersistedError — Node Chunk.NotPersistedError.
type ChunkNotPersistedError struct{ ProjectID string }

func (e *ChunkNotPersistedError) Error() string {
	return fmt.Sprintf("chunk for %s not persisted yet", e.ProjectID)
}

// --- Chunk (ports chunk.js) ---

// Chunk — a History that is part of a project's overall history. It has a
// start and an end version that place its History in context.
//
//	Wire (Node Chunk.toRaw, object):
//	  {
//	    history:       <History.toRaw>,
//	    startVersion:  <int>,
//	  }
type Chunk struct {
	History      *History
	StartVersion int
}

// NewChunk (Node Chunk constructor).
func NewChunk(history *History, startVersion int) *Chunk {
	return &Chunk{History: history, StartVersion: startVersion}
}

// ChunkFromRaw (Node Chunk.fromRaw).
func ChunkFromRaw(raw json.RawMessage) *Chunk {
	var probe struct {
		History      json.RawMessage `json:"history"`
		StartVersion int             `json:"startVersion"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		panic(&BadRawError{Msg: "bad chunk raw: " + err.Error()})
	}
	return NewChunk(HistoryFromRaw(probe.History), probe.StartVersion)
}

// ToRaw (Node Chunk.toRaw).
func (c *Chunk) ToRaw() json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"history":      json.RawMessage(c.History.ToRaw()),
		"startVersion": c.StartVersion,
	})
	return b
}

// GetHistory (Node Chunk.getHistory).
func (c *Chunk) GetHistory() *History { return c.History }

// GetSnapshot (Node Chunk.getSnapshot).
func (c *Chunk) GetSnapshot() *Snapshot { return c.History.GetSnapshot() }

// GetChanges (Node Chunk.getChanges).
func (c *Chunk) GetChanges() []*Change { return c.History.GetChanges() }

// PushChanges (Node Chunk.pushChanges).
func (c *Chunk) PushChanges(changes []*Change) { c.History.PushChanges(changes) }

// GetEndVersion (Node Chunk.getEndVersion) — version after applying all
// changes in this chunk.
func (c *Chunk) GetEndVersion() int {
	return c.StartVersion + c.History.CountChanges()
}

// GetEndTimestamp (Node Chunk.getEndTimestamp) — timestamp of the last
// change; the zero time models the Node `null` return when there are no
// changes.
func (c *Chunk) GetEndTimestamp() time.Time {
	if c.History.CountChanges() == 0 {
		return time.Time{}
	}
	return c.History.GetChanges()[len(c.History.GetChanges())-1].Timestamp
}

// GetStartVersion (Node Chunk.getStartVersion).
func (c *Chunk) GetStartVersion() int { return c.StartVersion }

// LoadFiles (Node Chunk.loadFiles): delegate to the history.
func (c *Chunk) LoadFiles(kind string, bs BlobStoreI) error {
	return c.History.LoadFiles(kind, bs)
}
