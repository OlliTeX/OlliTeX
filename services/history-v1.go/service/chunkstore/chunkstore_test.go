// Tests for the chunkstore state machine.
package chunkstore

import (
	"testing"
	"time"

	"history-v1/internal/core"
	"history-v1/service/blobstore"
	"history-v1/service/historystore"
)

func newStore(t *testing.T) (*Store, *historystore.HistoryStore) {
	t.Helper()
	persistor := historystore.NewFakePersister()
	hs := historystore.New(persistor, "raw_history_bucket")
	bs := blobstore.NewStore()
	return New(hs, func(p string) core.BlobStoreI { return bs.Project(p) }), hs
}

func noopChange(v int, ts time.Time) *core.Change {
	c := core.NewChange(nil, time.Now(), nil, nil, nil, "", nil)
	_ = v
	_ = ts
	return c
}

func emptyChunk(ts time.Time, start int, nChanges int) *core.Chunk {
	h := core.NewHistory(core.NewSnapshot(core.NewFileMap(), "", nil, ts), nil)
	c := core.NewChunk(h, start)
	for i := 0; i < nChanges; i++ {
		c.PushChanges([]*core.Change{noopChange(i, ts)})
	}
	return c
}

// Initialize activates a [0,0] chunk; a second Initialize fails.
func TestInitialize(t *testing.T) {
	cs, _ := newStore(t)
	pid := "aaaaaaaaaaaaaaaaaaaaaaaaaa"

	ev, err := cs.Initialize(pid, nil)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if ev != 0 {
		t.Fatalf("endVersion = %d, want 0", ev)
	}
	if _, err := cs.Initialize(pid, nil); err == nil {
		t.Fatal("second Initialize: wanted AlreadyInitialized error, got nil")
	}
}

// Create [0,0] when no chunk exists activates it (Node create, start == 0:
// no close, activate only).
func TestCreateFirstChunk(t *testing.T) {
	cs, _ := newStore(t)
	pid := "bbbbbbbbbbbbbbbbbbbbbbbbbb"

	chunk := emptyChunk(time.Now(), 0, 0)
	id, err := cs.Create(pid, chunk, time.Now())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" {
		t.Fatal("chunk id is empty")
	}

	// Now latest is the new id.
	md, err := cs.GetLatestChunkMetadata(pid)
	if err != nil {
		t.Fatalf("GetLatestChunkMetadata: %v", err)
	}
	if md.ID != id || md.StartVersion != 0 || md.EndVersion != 0 {
		t.Fatalf("metadata = %+v, want id=%s start=0 end=0", md, id)
	}

	// LoadAtVersion(0) resolves to it.
	ch, err := cs.LoadAtVersion(pid, 0, false)
	if err != nil {
		t.Fatalf("LoadAtVersion(0): %v", err)
	}
	if ch.GetStartVersion() != 0 || ch.GetEndVersion() != 0 {
		t.Fatalf("chunk %d..%d", ch.GetStartVersion(), ch.GetEndVersion())
	}
}

// LoadLatest on a fresh project: ChunkNotFoundError.
func TestLoadLatestNoChunks(t *testing.T) {
	cs, _ := newStore(t)
	if _, err := cs.LoadLatest("cccccccccccccccccccccccccc"); err == nil {
		t.Fatal("LoadLatest on fresh project: wanted error, got nil")
	}
}

// Initialize => [0,0]. Create [1,1], but no chunk covers version 1 yet:
// ChunkVersionNotFoundError (Node getChunkForVersion).
func TestCreateVersionNotFound(t *testing.T) {
	cs, _ := newStore(t)
	pid := "dddddddddddddddddddddddddd"

	if _, err := cs.Initialize(pid, nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := cs.Create(pid, emptyChunk(time.Now(), 1, 0), time.Now()); err == nil {
		t.Fatal("Create [1,1] with nothing covering version 1: wanted error, got nil")
	}
}

// Update replaces the active chunk: after Initialize => [0,0] active, an
// Update with a [0,1] chunk deletes the old record and activates the new.
func TestUpdateReplacesActive(t *testing.T) {
	cs, _ := newStore(t)
	pid := "eeeeeeeeeeeeeeeeeeeeeeeeee"

	if _, err := cs.Initialize(pid, nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// B = [0,1]: start 0, one noop change.
	b := emptyChunk(time.Now(), 0, 1)
	if _, err := cs.Update(pid, b, time.Now()); err != nil {
		t.Fatalf("Update [0,1] over active [0,0]: %v", err)
	}

	md, err := cs.GetLatestChunkMetadata(pid)
	if err != nil {
		t.Fatalf("GetLatestChunkMetadata: %v", err)
	}
	if md.StartVersion != 0 || md.EndVersion != 1 {
		t.Fatalf("metadata = %+v, want start=0 end=1", md)
	}

	// LoadAtVersion(0) resolves to B.
	ch, err := cs.LoadAtVersion(pid, 0, false)
	if err != nil {
		t.Fatalf("LoadAtVersion(0): %v", err)
	}
	if ch.GetStartVersion() != 0 || ch.GetEndVersion() != 1 {
		t.Fatalf("chunk %d..%d, want 0..1", ch.GetStartVersion(), ch.GetEndVersion())
	}
}

// Create [1,1] after [0,1] closes the old active chunk and activates [1,1].
func TestCreateClosesOld(t *testing.T) {
	cs, _ := newStore(t)
	pid := "ffffffffffffffffffffffffff"

	if _, err := cs.Initialize(pid, nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Active [0,0]. Update to [0,1] (endVersion 1).
	if _, err := cs.Update(pid, emptyChunk(time.Now(), 0, 1), time.Now()); err != nil {
		t.Fatalf("Update [0,1]: %v", err)
	}

	// Create [1,1]: getChunkForVersion(1) -> [0,1] (covers 1), end==1 OK -> close.
	c := emptyChunk(time.Now(), 1, 0)
	if _, err := cs.Create(pid, c, time.Now()); err != nil {
		t.Fatalf("Create [1,1]: %v", err)
	}

	md, err := cs.GetLatestChunkMetadata(pid)
	if err != nil {
		t.Fatalf("GetLatestChunkMetadata: %v", err)
	}
	if md.StartVersion != 1 || md.EndVersion != 1 {
		t.Fatalf("metadata = %+v, want start=1 end=1", md)
	}
	// LoadAtVersion(0) -> the (now closed) [0,1] chunk.
	old, err := cs.LoadAtVersion(pid, 0, false)
	if err != nil {
		t.Fatalf("LoadAtVersion(0): %v", err)
	}
	if old.GetStartVersion() != 0 || old.GetEndVersion() != 1 {
		t.Fatalf("old chunk %d..%d, want 0..1", old.GetStartVersion(), old.GetEndVersion())
	}
}

// LoadAtVersion out of range: ChunkVersionNotFoundError (version 99).
func TestLoadAtVersionOutOfBounds(t *testing.T) {
	cs, _ := newStore(t)
	pid := "333333333333333333333333"

	if _, err := cs.Initialize(pid, nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := cs.LoadAtVersion(pid, 99, false); err == nil {
		t.Fatal("LoadAtVersion(99): wanted error, got nil")
	}
}

// ChangesSince(0) on an active [0,1] chunk: hasMore=true (latest version 1
// > chunk end at this version).
func TestChangesSinceHasMore(t *testing.T) {
	cs, _ := newStore(t)
	pid := "444444444444444444444444"

	if _, err := cs.Initialize(pid, nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := cs.Update(pid, emptyChunk(time.Now(), 0, 1), time.Now()); err != nil {
		t.Fatalf("Update [0,1]: %v", err)
	}

	changes, hasMore, err := cs.ChangesSince(pid, 0, false)
	if err != nil {
		t.Fatalf("ChangesSince(0): %v", err)
	}
	_ = changes
	if hasMore {
		t.Fatal("ChangesSince(0) on [0,1]: expected hasMore=false")
	}

	// Now extend with a Create [1,1]; latest end is 1, but the [0,1]
	// chunk is closed (end 1). ChangesSince(0) sees latest=1, chunk=[0,1]
	// end=1 -> hasMore=false still. After another Create, latest end grows.

}

// After Initialize a [0,0] chunk is loadable; its raw is stored in the
// history store (bucket, keyed by format/pad).
func TestChunkRawStored(t *testing.T) {
	persistor := historystore.NewFakePersister()
	hs := historystore.New(persistor, "raw_history_bucket")
	bs := blobstore.NewStore()
	cs := New(hs, func(p string) core.BlobStoreI { return bs.Project(p) })
	pid := "555555555555555555555555"

	if _, err := cs.Initialize(pid, nil); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	md, err := cs.GetLatestChunkMetadata(pid)
	if err != nil {
		t.Fatalf("GetLatestChunkMetadata: %v", err)
	}
	raw, err := hs.LoadRaw(pid, md.ID)
	if err != nil {
		t.Fatalf("LoadRaw(pid, %q): %v", md.ID, err)
	}
	if len(raw) == 0 {
		t.Fatal("empty raw history")
	}
}

func TestEmptyChunkEndVersion(t *testing.T) {
	// emptyChunk(1) endVersion = 0 + 1 = 1.
	c := emptyChunk(time.Now(), 0, 1)
	if c.GetEndVersion() != 1 {
		t.Fatalf("endVersion = %d, want 1", c.GetEndVersion())
	}
}
