// Package syncadapter implements the updatesprocessor.Sync seam (C17) over
// the C16 syncmanager — the D-phase bridge between the two.
package syncadapter

import (
	"context"

	"ollitex/go/services/project-history/internal/syncmanager"
	"ollitex/go/services/project-history/internal/updatesprocessor"
)

// Adapter satisfies the updatesprocessor.Sync interface.
type Adapter struct {
	S *syncmanager.Deps
}

func New(d *syncmanager.Deps) *Adapter {
	return &Adapter{S: d}
}

// toC17 — C16 SyncState → C17 SyncState (the seam's own struct).
func toC17(s *syncmanager.SyncState) *updatesprocessor.SyncState {
	if s == nil {
		return nil
	}
	out := &updatesprocessor.SyncState{
		Ongoing:             s.IsSyncOngoing(),
		StuckClearCount:     s.StuckClearCount,
		ResyncDocContents:   s.ResyncDocContents,
		ResyncProjectStruct: s.ResyncProjectStructure,
		Raw:                 s.ToRaw(),
	}
	if s.ResyncPendingSince != nil {
		out.ResyncPendingSince = s.ResyncPendingSince.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return out
}

// SkipUpdatesDuringSync — vendor SyncManager.skipUpdatesDuringSync.
func (a *Adapter) SkipUpdatesDuringSync(ctx context.Context, projectID string, updates []map[string]any) ([]map[string]any, *updatesprocessor.SyncState, error) {
	filtered, newState, err := a.S.SkipUpdatesDuringSync(ctx, projectID, updates)
	if err != nil {
		return nil, nil, err
	}
	return filtered, toC17(newState), nil
}

// SetResyncState — persist the C17 seam state back to C16.
func (a *Adapter) SetResyncState(ctx context.Context, projectID string, state *updatesprocessor.SyncState) error {
	if state == nil {
		return nil
	}
	if raw, ok := state.Raw.(map[string]any); ok {
		_ = raw
	}
	// the C16 bundle stores the vendor raw shape; rebuild it from the fields
	// (the seam's Ongoing flag maps to the resyncProjectStructure key when
	// the structure is syncing — vendor shouldSkipUpdate semantics).
	cs := &syncmanager.SyncState{
		ResyncProjectStructure: state.ResyncProjectStruct,
		ResyncDocContents:      state.ResyncDocContents,
		StuckClearCount:        state.StuckClearCount,
	}
	return a.S.SetResyncState(ctx, projectID, cs)
}

// ExpandSyncUpdates — vendor SyncManager.expandSyncUpdates.
func (a *Adapter) ExpandSyncUpdates(ctx context.Context, projectID, historyID string, mostRecentChunk map[string]any, updates []map[string]any) ([]map[string]any, error) {
	// the seam carries no extend lock (the vendor pass is a no-op in the
	// updates processor path).
	return a.S.ExpandSyncUpdates(ctx, projectID, historyID, mostRecentChunk, updates, func() error { return nil })
}

// GetResyncState — vendor SyncManager.getResyncState.
func (a *Adapter) GetResyncState(ctx context.Context, projectID string) (*updatesprocessor.SyncState, error) {
	state, err := a.S.GetResyncState(ctx, projectID)
	return toC17(state), err
}

// StartResyncWithoutLock — vendor SyncManager.startResyncWithoutLock.
func (a *Adapter) StartResyncWithoutLock(ctx context.Context, projectID string, opts map[string]any) error {
	return a.S.StartResyncWithoutLock(ctx, projectID, opts)
}

// StartHardResync — vendor SyncManager.startHardResync.
func (a *Adapter) StartHardResync(ctx context.Context, projectID string, opts map[string]any) error {
	return a.S.StartHardResync(ctx, projectID, opts)
}
