package otc

import (
	"context"

	oerror "ollitex/go/libraries/oerror"
)

// History mirrors lib/history.js: a Snapshot and a sequence of Changes that can
// be applied to produce a new snapshot.
type History struct {
	Snapshot *Snapshot
	Changes  []*Change
}

// NewHistory mirrors `new History(snapshot, changes)` (changes defaults to []).
func NewHistory(snapshot *Snapshot, changes []*Change) *History {
	if changes == nil {
		changes = []*Change{}
	}
	return &History{Snapshot: snapshot, Changes: changes}
}

// HistoryFromRaw mirrors `History.fromRaw`.
func HistoryFromRaw(raw map[string]any) (*History, error) {
	snapRaw, ok := raw["snapshot"].(map[string]any)
	if !ok {
		return nil, oerror.New("bad raw.snapshot", nil)
	}
	snap, err := SnapshotFromRaw(snapRaw)
	if err != nil {
		return nil, err
	}
	var changes []*Change
	if rawChanges, present := raw["changes"]; present {
		list, ok := rawChanges.([]any)
		if !ok {
			return nil, oerror.New("bad raw.changes", nil)
		}
		for _, rc := range list {
			rm, ok := rc.(map[string]any)
			if !ok {
				return nil, oerror.New("bad raw.changes", nil)
			}
			c, err := ChangeFromRaw(rm)
			if err != nil {
				return nil, err
			}
			changes = append(changes, c)
		}
	}
	return NewHistory(snap, changes), nil
}

// ToRaw (Node: toRaw).
func (h *History) ToRaw() map[string]any {
	changes := make([]any, 0, len(h.Changes))
	for _, c := range h.Changes {
		changes = append(changes, c.ToRaw())
	}
	return map[string]any{
		"snapshot": h.Snapshot.ToRaw(),
		"changes":  changes,
	}
}

// GetSnapshot (Node: getSnapshot).
func (h *History) GetSnapshot() *Snapshot { return h.Snapshot }

// GetChanges (Node: getChanges).
func (h *History) GetChanges() []*Change { return h.Changes }

// CountChanges (Node: countChanges).
func (h *History) CountChanges() int { return len(h.Changes) }

// PushChanges (Node: pushChanges).
func (h *History) PushChanges(changes []*Change) {
	h.Changes = append(h.Changes, changes...)
}

// FindBlobHashes (Node: findBlobHashes) — the snapshot's hashes plus each
// change's hashes.
func (h *History) FindBlobHashes(hashes map[string]bool) {
	h.Snapshot.FindBlobHashes(hashes)
	for _, c := range h.Changes {
		c.FindBlobHashes(hashes)
	}
}

// LoadFiles (Node: loadFiles).
//
// Node runs `snapshot.loadFiles` and the per-change loads in parallel
// (Promise.all). Go runs them sequentially — the result set is identical and the
// BlobStore seam need not be goroutine-safe here.
func (h *History) LoadFiles(ctx context.Context, kind string, bs BlobStore) error {
	_ = h.Snapshot.LoadFiles(ctx, kind, bs)
	for _, c := range h.Changes {
		if err := c.LoadFiles(ctx, kind, bs); err != nil {
			return err
		}
	}
	return nil
}

// Store (Node: store).
//
//	Node:
//	  const [rawSnapshot, rawChanges] = await Promise.all([
//	    this.snapshot.store(blobStore, concurrency),
//	    pMap(this.changes, storeChange, { concurrency: concurrency || 1 }),
//	  ])
//
// Go maps this to a sequential store (faithful results). The `concurrency`
// parameter is kept for 1:1 API parity but applied sequentially — documented
// Go-ism (Node's p-map only parallelises I/O; it does not change the output).
func (h *History) Store(ctx context.Context, bs BlobStore, concurrency int) (map[string]any, error) {
	rawSnapshot, err := h.Snapshot.Store(ctx, bs)
	if err != nil {
		return nil, err
	}
	rawChanges := make([]any, 0, len(h.Changes))
	for _, c := range h.Changes {
		rc, err := c.Store(bs)
		if err != nil {
			return nil, err
		}
		rawChanges = append(rawChanges, rc)
	}
	return map[string]any{
		"snapshot": rawSnapshot,
		"changes":  rawChanges,
	}, nil
}
