// Tests for the persist pipeline, mirroring the Node oracle
// test/acceptance/js/storage/persist_changes.test.js.
//
// The hermetic stand-in for the Node fixtures: a shared in-memory
// blobstore + FakePersister-backed chunkstore, and a single initialized
// project (Node fixtures.docs.uninitializedProject).
package persist

import (
	"errors"
	"testing"
	"time"

	"history-v1/internal/contenthash"
	"history-v1/internal/core"
	"history-v1/service/blobstore"
	"history-v1/service/chunkstore"
	"history-v1/service/historystore"
)

// newHarness wires persist over the same blob store namespace chunkstore
// uses, like the Node storage index (chunkStore + blobStore share the
// project blob namespace).
func newHarness(t *testing.T) (*Service, *chunkstore.Store, *blobstore.Store) {
	t.Helper()
	persistor := historystore.NewFakePersister()
	hs := historystore.New(persistor, "raw_history_bucket")
	bs := blobstore.NewStore()
	cs := chunkstore.New(hs, func(p string) core.BlobStoreI { return bs.Project(p) })
	return NewService(cs, bs), cs, bs
}

// farFuture — Node: new Date() + 7 days, passed as min/maxChangeTimestamp so
// every change qualifies as "too old" and persisting is forced.
func farFuture() (min, max *time.Time) {
	ff := time.Now().Add(7 * 24 * time.Hour)
	return &ff, &ff
}

// fixedChange builds a Node-style Change(operations, new Date(), []).
func fixedChange(ts time.Time, ops ...*core.Operation) *core.Change {
	return core.NewChange(ops, ts, nil, nil, nil, "", nil)
}

func stringFile(content string) *core.File {
	return &core.File{Kind: "string", Content: content}
}

// initProject mirrors `await chunkStore.initializeProject(projectId)`.
func initProject(t *testing.T, cs *chunkstore.Store, pid string) {
	t.Helper()
	if _, err := cs.Initialize(pid, nil); err != nil {
		t.Fatalf("Initialize(%s): %v", pid, err)
	}
}

// Case 1 (Node 'persists changes'): one AddFile into a fresh [0,0] chunk.
func TestPersistChangesPersistsChanges(t *testing.T) {
	svc, cs, _ := newHarness(t)
	min, max := farFuture()
	pid := "aaaa00000000000000000001"
	initProject(t, cs, pid)

	ts := time.Now()
	change := fixedChange(ts, core.AddFile("test.tex", stringFile("")))

	result, err := svc.PersistChanges(pid, []*core.Change{change}, Limits{
		MinChangeTimestamp: min, MaxChangeTimestamp: max,
	}, 0)
	if err != nil {
		t.Fatalf("PersistChanges: %v", err)
	}
	if result.NumberOfChangesPersisted != 1 || result.OriginalEndVersion != 0 || result.ResyncNeeded {
		t.Fatalf("result = %+v, want {1, 0, _, false}", result)
	}
	if result.CurrentChunk.GetStartVersion() != 0 || result.CurrentChunk.GetEndVersion() != 1 {
		t.Fatalf("currentChunk = [%d,%d], want [0,1]",
			result.CurrentChunk.GetStartVersion(), result.CurrentChunk.GetEndVersion())
	}
	// The extended chunk keeps its start-of-chunk (empty initialized) snapshot
	// and the change is appended to its change list (Node oracle result:
	// currentChunk = new Chunk(new History(new Snapshot(), changes), 0)).
	if got := result.CurrentChunk.GetSnapshot(); got.CountFiles() != 0 || got.GetFile("test.tex") != nil {
		t.Fatalf("current snapshot files = %+v, want empty", got)
	}
	if n := len(result.CurrentChunk.GetChanges()); n != 1 {
		t.Fatalf("current chunk changes = %d, want 1", n)
	}
	if got := result.CurrentChunk.GetChanges()[0].Operations[0].Pathname; got != "test.tex" {
		t.Fatalf("change op path = %q, want test.tex", got)
	}

	// Node: chunkStore.loadLatest(projectId).
	latest, err := cs.LoadLatest(pid)
	if err != nil {
		t.Fatalf("LoadLatest: %v", err)
	}
	if latest.GetStartVersion() != 0 || latest.GetEndVersion() != 1 {
		t.Fatalf("latest chunk = [%d,%d], want [0,1]", latest.GetStartVersion(), latest.GetEndVersion())
	}
	if n := len(latest.GetChanges()); n != 1 {
		t.Fatalf("changes = %d, want 1", n)
	}
}

// Case 2 (Node 'persists changes in three chunks'): maxChunkChanges = 1,
// three AddFiles land in chunks [0,1], [1,2], [2,3]; the latest holds only
// c.tex and its snapshot is a{tex,b.tex} of ”.
func TestPersistChangesThreeChunks(t *testing.T) {
	svc, cs, _ := newHarness(t)
	min, max := farFuture()
	pid := "bbbb00000000000000000001"
	initProject(t, cs, pid)

	ts := time.Now()
	changes := []*core.Change{
		fixedChange(ts, core.AddFile("a.tex", stringFile(""))),
		fixedChange(ts, core.AddFile("b.tex", stringFile(""))),
		fixedChange(ts, core.AddFile("c.tex", stringFile(""))),
	}

	result, err := svc.PersistChanges(pid, changes, Limits{
		MaxChunkChanges:    1,
		MinChangeTimestamp: min, MaxChangeTimestamp: max,
	}, 0)
	if err != nil {
		t.Fatalf("PersistChanges: %v", err)
	}
	if result.NumberOfChangesPersisted != 3 || result.OriginalEndVersion != 0 || result.ResyncNeeded {
		t.Fatalf("result = %+v, want {3, 0, _, false}", result)
	}
	chunk := result.CurrentChunk
	if chunk.GetStartVersion() != 2 || chunk.GetEndVersion() != 3 {
		t.Fatalf("currentChunk = [%d,%d], want [2,3]", chunk.GetStartVersion(), chunk.GetEndVersion())
	}
	if n := len(chunk.GetChanges()); n != 1 {
		t.Fatalf("current chunk changes = %d, want 1", n)
	}
	snap := chunk.GetSnapshot()
	if snap.CountFiles() != 2 || snap.GetFile("a.tex") == nil || snap.GetFile("b.tex") == nil || snap.GetFile("c.tex") != nil {
		t.Fatalf("snapshot files = %v, want {a.tex,b.tex}", snap.GetFilePathnames())
	}
	if op := chunk.GetChanges()[0].Operations[0]; op.Pathname != "c.tex" {
		t.Fatalf("change op = %q, want c.tex", op.Pathname)
	}

	latest, err := cs.LoadLatest(pid)
	if err != nil {
		t.Fatalf("LoadLatest: %v", err)
	}
	if latest.GetStartVersion() != 2 || latest.GetEndVersion() != 3 {
		t.Fatalf("latest chunk = [%d,%d], want [2,3]", latest.GetStartVersion(), latest.GetEndVersion())
	}
	if n := len(latest.GetChanges()); n != 1 {
		t.Fatalf("changes = %d, want 1", n)
	}
}

// Case 3 (Node 'persists the snapshot at the start of the chunk'):
// maxChunkChanges = 2; both changes extend the initial [0,0] chunk to [0,2].
func TestPersistChangesSnapshotAtStart(t *testing.T) {
	svc, cs, _ := newHarness(t)
	min, max := farFuture()
	pid := "cccc00000000000000000001"
	initProject(t, cs, pid)

	ts := time.Now()
	changes := []*core.Change{
		fixedChange(ts, core.AddFile("a.tex", stringFile(""))),
		fixedChange(ts, core.AddFile("b.tex", stringFile(""))),
	}

	result, err := svc.PersistChanges(pid, changes, Limits{
		MaxChunkChanges:    2,
		MinChangeTimestamp: min, MaxChangeTimestamp: max,
	}, 0)
	if err != nil {
		t.Fatalf("PersistChanges: %v", err)
	}
	if result.NumberOfChangesPersisted != 2 || result.OriginalEndVersion != 0 || result.ResyncNeeded {
		t.Fatalf("result = %+v, want {2, 0, _, false}", result)
	}
	if chunk := result.CurrentChunk; chunk.GetStartVersion() != 0 || chunk.GetEndVersion() != 2 {
		t.Fatalf("currentChunk = [%d,%d], want [0,2]", chunk.GetStartVersion(), chunk.GetEndVersion())
	}
	if n := len(chunkGetChanges(t, cs, pid)); n != 2 {
		t.Fatalf("latest changes = %d, want 2", n)
	}
}

func chunkGetChanges(t *testing.T, cs *chunkstore.Store, pid string) []*core.Change {
	t.Helper()
	latest, err := cs.LoadLatest(pid)
	if err != nil {
		t.Fatalf("LoadLatest: %v", err)
	}
	return latest.GetChanges()
}

// Case 4 (Node "errors if the version doesn't match the latest chunk").
func TestPersistChangesConflictingEndVersion(t *testing.T) {
	svc, cs, _ := newHarness(t)
	min, max := farFuture()
	pid := "dddd00000000000000000001"
	initProject(t, cs, pid)

	changes := []*core.Change{
		fixedChange(time.Now(), core.AddFile("a.tex", stringFile(""))),
		fixedChange(time.Now(), core.AddFile("b.tex", stringFile(""))),
	}

	_, err := svc.PersistChanges(pid, changes, Limits{
		MinChangeTimestamp: min, MaxChangeTimestamp: max,
	}, 1)
	if err == nil {
		t.Fatal("PersistChanges: wanted error, got nil")
	}
	want := "client sent updates with end_version 1 but latest chunk has end_version 0"
	var ceve *core.ConflictingEndVersion
	if !errors.As(err, &ceve) || ceve.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
	// The failed persist must not have written anything.
	if md, merr := cs.GetLatestChunkMetadata(pid); merr != nil || md.EndVersion != 0 {
		t.Fatalf("after conflict: metadata = %+v %v, want endVersion 0", md, merr)
	}
}

// Case 5 + 6 (Node 'content hash validation'): an AddFile + EditFile pair
// whose edit op carries a contentHash. A matching hash leaves resyncNeeded
// false; a wrong hash flips it true (and is then cleared before storing).
func TestPersistChangesContentHashValidation(t *testing.T) {
	cases := []struct {
		name   string
		hash   string
		resync bool
		pid    string
	}{
		{"valid hash", contenthash.ContentHash("hello world"), false, "eeee00000000000000000001"},
		{"bad hash", contenthash.ContentHash("bad hash"), true, "ffff00000000000000000001"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, cs, _ := newHarness(t)
			min, max := farFuture()
			initProject(t, cs, c.pid)

			// new TextOperation().insert('hello ').retain(5), contentHash set.
			textOp := core.NewTextOp(core.NewInsert("hello ", nil, nil), core.NewRetain(5, nil))
			textOp.ContentHash = c.hash
			change := fixedChange(time.Now(),
				core.AddFile("a.tex", stringFile("world")),
				core.EditFile("a.tex", core.NewTextEditOp(textOp)),
			)

			result, err := svc.PersistChanges(c.pid, []*core.Change{change}, Limits{
				MinChangeTimestamp: min, MaxChangeTimestamp: max,
			}, 0)
			if err != nil {
				t.Fatalf("PersistChanges: %v", err)
			}
			if result.NumberOfChangesPersisted != 1 {
				t.Fatalf("persisted = %d, want 1", result.NumberOfChangesPersisted)
			}
			if result.ResyncNeeded != c.resync {
				t.Fatalf("resyncNeeded = %v, want %v", result.ResyncNeeded, c.resync)
			}
			// The hash must be stripped from the stored op (Node:
			// validateContentHash always does `operation.contentHash =
			// undefined` — actually `te.contentHash = null`).
			latest, err := cs.LoadLatest(c.pid)
			if err != nil {
				t.Fatalf("LoadLatest: %v", err)
			}
			stored := latest.GetChanges()[0].Operations[1]
			if te := stored.EditOp.TextOpForEdit(); te == nil || te.ContentHash != "" {
				t.Fatalf("stored textOp contentHash = %+v, want cleared", te)
			}
		})
	}
}
