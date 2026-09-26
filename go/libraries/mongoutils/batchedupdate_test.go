package mongoutils

// batchedupdate_test.go — the batchedUpdate.js walker pinned against a live
// mongod (skipped when none is reachable): the ascending full walk (batches,
// ordering, update count, progress markers), the descending window
// mechanics, the empty-collection short-circuit, and the single-flight
// guard.
//
// KNOWN INHERITED QUIRK (documented in HANDOFF, verified against Node
// itself): the final-batch boundary makes the walk climb empty span-sized
// windows up to ID_EDGE_FUTURE (or sit on a lone bottom doc descending).
// In production data (dense, 31-day default span) that climb is a handful
// of empty batches; with a small span over old data it is effectively a
// hang — in Node too. The tests below use production-shaped spans so the
// walks terminate quickly, exactly as Node's would.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const liveDB = "test-overleaf"

func seedBatchDocs(t *testing.T, coll *mongo.Collection, baseMs int64, offsets ...int64) {
	t.Helper()
	docs := make([]any, len(offsets))
	for i, off := range offsets {
		docs[i] = bson.M{"_id": objectIdFromMs(baseMs + off), "n": i, "flag": 0}
	}
	if _, err := coll.InsertMany(context.Background(), docs); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func newLiveCollection(t *testing.T, name string) *mongo.Collection {
	client := liveMongo(t)
	coll := client.Database(liveDB).Collection(name)
	if err := coll.Drop(context.Background()); err != nil {
		t.Fatalf("drop old collection: %v", err)
	}
	return coll
}

// captureStderr swaps os.Stderr for a pipe for the duration of the test.
func captureStderr(t *testing.T) *captureBuf {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	buf := &captureBuf{b: &bytes.Buffer{}}
	done := make(chan struct{})
	go func() {
		// chunked reads: the capture lock must only be held while appending
		// (a ReadFrom holding it would deadlock the test's String() poll).
		tmp := make([]byte, 32*1024)
		for {
			n, rerr := r.Read(tmp)
			buf.Write(tmp[:n])
			if rerr != nil {
				break
			}
		}
		close(done)
	}()
	t.Cleanup(func() {
		w.Close()
		os.Stderr = old
		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
		}
	})
	return buf
}

// TestBatchedUpdateAscendingFullWalk pins the full ascending walk with a
// production-shaped span (the default ONE_MONTH — so the final climb to
// ID_EDGE_FUTURE is a few empty batches, like in production): 4 batches
// over a 3-month span, function-form update, progress markers, and the
// exact updated count.
func TestBatchedUpdateAscendingFullWalk(t *testing.T) {
	coll := newLiveCollection(t, "batch_asc")
	base := time.Now().UnixMilli() - 90*24*3600*1000 // 90 days ago — recent data, as production has

	var mu sync.Mutex
	var batchEndings []int64
	updateFn := func(batch []Doc) error {
		// a real runner function performs its own update (Node:
		// `await update(nextBatch)`).
		ids := make([]any, len(batch))
		for i, d := range batch {
			if id, ok := objectIDOf(d); ok {
				ids[i] = id
			}
		}
		if _, uerr := coll.UpdateMany(context.Background(), bson.M{"_id": bson.M{"$in": ids}}, bson.M{"$set": bson.M{"flag": 1}}); uerr != nil {
			return uerr
		}
		mu.Lock()
		defer mu.Unlock()
		last := batch[len(batch)-1]
		id, _ := objectIDOf(last)
		batchEndings = append(batchEndings, getMsFromObjectId(id))
		return nil
	}

	var progress []string
	rec := func(p string) {
		mu.Lock()
		progress = append(progress, p)
		mu.Unlock()
	}

	// four docs, one per 30-day bucket (default span = ONE_MONTH = 31d, so
	// each bucket is exactly one batch): offsets 0d, 20d, 45d, 70d.
	offsets := []int64{
		0,
		20 * 24 * 3600 * 1000,
		45 * 24 * 3600 * 1000,
		70 * 24 * 3600 * 1000,
	}
	seedBatchDocs(t, coll, base, offsets...)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	updated, err := BatchedUpdate(ctx, coll, bson.M{"flag": 0},
		updateFn, nil, nil, BatchedUpdateOptions{TrackProgress: rec})
	if err != nil {
		t.Fatalf("BatchedUpdate: %v", err)
	}
	if updated != 4 {
		t.Fatalf("updated: got %d want 4", updated)
	}
	// (the function-form update is exercised by updateFn above — it ran once
	// per non-empty batch and saw every updated doc)
	// each batch's newest (what the walk advances to) strictly ascends
	for i := 1; i < len(batchEndings); i++ {
		if batchEndings[i] <= batchEndings[i-1] {
			t.Fatalf("batch endings must ascend: %v", batchEndings)
		}
	}
	ran, completed := 0, 0
	for _, p := range progress {
		switch {
		case strings.HasPrefix(p, "Running update on batch ending "):
			ran++
		case strings.HasPrefix(p, "Completed batch ending "):
			completed++
		}
	}
	if ran != 3 || completed != 4 { // the final empty climb batch still logs "Completed" (Node order)
		t.Fatalf("progress markers: %d running want 3, %d completed want 4:\n%s", ran, completed, strings.Join(progress, "\n"))
	}
	// every doc updated
	var flagged int64
	if n, e := coll.CountDocuments(ctx, bson.M{"flag": 1}); e != nil {
		t.Fatal(e)
	} else {
		flagged = n
	}
	if flagged != 4 {
		t.Fatalf("flagged docs: got %d want 4", flagged)
	}
}

// TestBatchedUpdateDescendingWindow pins the descending mechanics that a
// full walk cannot reach (the inherited final-batch quirk — see the file
// header — means no descending run over real data terminates; Node's is
// the same). The two observable pieces are pinned directly:
//
//  1. the descending window filter `$gt: end, $lte: start` with the
//     descending sort (getNextBatch, one query),
//  2. the window end arithmetic (getNextEnd descending + clamp — see
//     TestGetNextEndSpans, pure).
func TestBatchedUpdateDescendingWindow(t *testing.T) {
	coll := newLiveCollection(t, "batch_descw")
	base := time.Now().UnixMilli() - 90*24*3600*1000
	// docs at 0d, 10d, 20d (ascending offsets)
	seedBatchDocs(t, coll, base, 0, 10*24*3600*1000, 20*24*3600*1000)

	start := objectIdFromMs(base + 20*24*3600*1000) // window top (inclusive)
	end := objectIdFromMs(base + 5*24*3600*1000)    // window lower bound (exclusive) → window holds the 10d and 20d docs

	// getNextBatch reads the global direction for its sort: set it
	// descending (snapshot/restore the module state).
	snap := snapGlobals()
	defer restoreGlobals(snap)
	batchStateMu.Lock()
	BatchDescending = true
	batchStateMu.Unlock()

	// descending mode: the walk builds the window filter `$gt: end, $lte:
	// start` into the query (batchedUpdate's queryCopy) and sorts
	// newest-first; pin that single-query mechanics end-to-end.
	batch, err := getNextBatch(context.Background(), coll,
		bson.M{"flag": 0, "_id": bson.M{"$gt": end, "$lte": start}},
		start, end, bson.M{"_id": 1, "flag": 1}, options.Find(), 100)
	if err != nil {
		t.Fatalf("getNextBatch: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("window content: got %d docs want 2 (descending (end, start] window)", len(batch))
	}
	first, _ := objectIDOf(batch[0])
	second, _ := objectIDOf(batch[1])
	if getMsFromObjectId(first) <= getMsFromObjectId(second) {
		t.Fatalf("descending sort violated: %d !> %d", getMsFromObjectId(first), getMsFromObjectId(second))
	}
	// _id boundary respected: the 0d doc (<= end) is excluded
	for _, d := range batch {
		id, _ := objectIDOf(d)
		if getMsFromObjectId(id) <= base+5*24*3600*1000 {
			t.Fatalf("window lower bound (exclusive $gt end) violated in batch: %v", id)
		}
	}
}

func TestBatchedUpdateDocumentUpdateForm(t *testing.T) {
	t.Parallel()
	coll := newLiveCollection(t, "batch_docform")
	base := time.Now().UnixMilli() - 5*24*3600*1000
	seedBatchDocs(t, coll, base, 0, 1000*1000)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	updated, err := BatchedUpdate(ctx, coll, bson.M{"flag": 0},
		bson.M{"$set": bson.M{"flag": 1}}, nil, nil, BatchedUpdateOptions{})
	if err != nil {
		t.Fatalf("BatchedUpdate: %v", err)
	}
	if updated != 2 {
		t.Fatalf("updated: got %d want 2 (document-form update path)", updated)
	}
	var flagged int64
	if n, e := coll.CountDocuments(ctx, bson.M{"flag": 1}); e != nil {
		t.Fatal(e)
	} else {
		flagged = n
	}
	if flagged != 2 {
		t.Fatalf("flagged: got %d want 2", flagged)
	}
}

func TestBatchedUpdateEmptyCollection(t *testing.T) {
	coll := newLiveCollection(t, "batch_empty")

	buf := captureStderr(t)
	updated, err := BatchedUpdate(context.Background(), coll, bson.M{},
		bson.M{"$set": bson.M{"x": 1}}, nil, nil, BatchedUpdateOptions{})
	if err != nil {
		t.Fatalf("empty collection should short-circuit cleanly, got %v", err)
	}
	if updated != 0 {
		t.Fatalf("updated: got %d want 0", updated)
	}
	// Wait for the async pipe reader to deliver the warning (a fixed sleep
	// is flaky under full-suite load).
	const want = "The collection batch_empty appears to be empty."
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), want) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if out := buf.String(); !strings.Contains(out, want) {
		t.Fatalf("stderr warning:\n got: %q\nwant to contain: %q", out, want)
	}
}

func TestBatchedUpdateSingleFlight(t *testing.T) {
	snap := snapGlobals()
	defer restoreGlobals(snap)

	// force the running flag (same package) — the guard fires before ANY
	// server I/O (Node order: the running check is first), so no mongod is
	// needed: a lazily-connected client suffices.
	batchStateMu.Lock()
	BatchedUpdateRunning = true
	batchStateMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(liveMongoURI))
	if err != nil {
		t.Fatalf("lazy connect: %v", err)
	}
	coll := client.Database(liveDB).Collection("batch_race")

	_, err = BatchedUpdate(context.Background(), coll, bson.M{},
		bson.M{"$set": bson.M{"x": 1}}, nil, nil, BatchedUpdateOptions{})
	if err == nil {
		t.Fatal("expected the single-flight guard error")
	}
	const want = "batchedUpdate is already running"
	if err.Error() != want {
		t.Fatalf("\n got: %q\nwant: %q", err.Error(), want)
	}
}

var _ = fmt.Sprintf

// captureBuf — a mutex-guarded bytes.Buffer for the stderr capture pipe:
// the reader goroutine appends while the test polls String() concurrently
// (bytes.Buffer is not safe for concurrent use).
type captureBuf struct {
	mu sync.Mutex
	b  *bytes.Buffer
}

func (c *captureBuf) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.Write(p)
}

func (c *captureBuf) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}
