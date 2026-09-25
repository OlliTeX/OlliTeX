package core

import (
	"encoding/json"
	"fmt"
)

// History — ports history.js. A History is a Snapshot and a sequence of
// Changes that can be applied to produce a new snapshot.
//
//	Wire (Node History.toRaw, object):
//	  {
//	    snapshot: <Snapshot.toRaw>,
//	    changes:  [ <Change.toRaw>... ],
//	  }
type History struct {
	Snapshot *Snapshot
	Changes  []*Change
}

// NewHistory (Node History constructor).
func NewHistory(snapshot *Snapshot, changes []*Change) *History {
	if changes == nil {
		changes = []*Change{}
	}
	return &History{Snapshot: snapshot, Changes: changes}
}

// HistoryFromRaw (Node History.fromRaw).
func HistoryFromRaw(raw json.RawMessage) *History {
	var probe struct {
		Snapshot json.RawMessage   `json:"snapshot"`
		Changes  []json.RawMessage `json:"changes"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		panic(&BadRawError{Msg: "bad history raw: " + err.Error()})
	}
	snap := SnapshotFromRaw(probe.Snapshot)
	changes := make([]*Change, 0, len(probe.Changes))
	for _, cRaw := range probe.Changes {
		changes = append(changes, ChangeFromRaw(cRaw))
	}
	return NewHistory(snap, changes)
}

// ToRaw (Node History.toRaw).
func (h *History) ToRaw() json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"snapshot": json.RawMessage(h.Snapshot.ToRaw()),
		"changes":  h.changesRaw(),
	})
	return b
}

func (h *History) changesRaw() []json.RawMessage {
	out := make([]json.RawMessage, 0, len(h.Changes))
	for _, c := range h.Changes {
		out = append(out, c.ToRaw())
	}
	return out
}

// GetSnapshot (Node History.getSnapshot).
func (h *History) GetSnapshot() *Snapshot { return h.Snapshot }

// GetChanges (Node History.getChanges).
func (h *History) GetChanges() []*Change { return h.Changes }

// CountChanges (Node History.countChanges).
func (h *History) CountChanges() int { return len(h.Changes) }

// PushChanges (Node History.pushChanges).
func (h *History) PushChanges(changes []*Change) {
	h.Changes = append(h.Changes, changes...)
}

// FindBlobHashes (Node History.findBlobHashes): snapshot + each change.
func (h *History) FindBlobHashes(blobHashes *map[string]struct{}) {
	h.Snapshot.FindBlobHashes(blobHashes)
	for _, c := range h.Changes {
		c.FindBlobHashes(blobHashes)
	}
}

// LoadFiles (Node History.loadFiles): snapshot + each change, per-kind file
// load.
func (h *History) LoadFiles(kind string, bs BlobStoreI) error {
	if err := h.Snapshot.LoadFiles(kind, bs); err != nil {
		return err
	}
	for _, c := range h.Changes {
		if err := c.LoadFiles(kind, bs); err != nil {
			return err
		}
	}
	return nil
}

// Store (Node History.store): raw snapshot + one raw change per change, each
// stored into the blob store (Node: Promise.all of snapshot.store +
// pMap(changes, change.store)).
func (h *History) Store(bs BlobStoreI) (json.RawMessage, error) {
	rawSnapshot, err := h.Snapshot.Store(bs)
	if err != nil {
		return nil, err
	}
	rawChanges := make([]json.RawMessage, 0, len(h.Changes))
	for _, c := range h.Changes {
		cr, err := c.Store(bs)
		if err != nil {
			return nil, err
		}
		rawChanges = append(rawChanges, cr)
	}
	if rawChanges == nil {
		rawChanges = []json.RawMessage{}
	}
	out, err := json.Marshal(map[string]any{
		"snapshot": rawSnapshot,
		"changes":  rawChanges,
	})
	if err != nil {
		return nil, fmt.Errorf("history.store: %w", err)
	}
	return out, nil
}
