// Package contentcachemanager ports services/clsi/app/js/ContentCacheManager.js.
//
// Node parity:
//
//   - update() early-returns {contentRanges:[], newContentRanges:[],
//     reclaimedSpace:0} (no other keys) when pdfSize < minChunkSize.
//   - updateSameEventLoop is the production path (the Node worker-pool path
//     is V8-specific and unavailable in Go); in the default Node config the
//     worker pool is off, so this matches production behavior.
//   - Soft deadline timeout: a breach inside the read/hash/write loop stops
//     the loop and yields the partial ranges plus TimedOutErr. A breach
//     before the loop propagates as a hard error.
//   - parseXrefTable errors surface as clserrors.NoXrefTableError wrapping
//     the xrefparser error.
//   - HashFileTracker persists <contentDir>/.state.v0.json as
//     {"hashAge":[[hash,age],...],"hashSize":[[hash,size],...]} preserving
//     insertion order (Node Map iteration order).
//   - getDeadlineChecker: timeout = min(max(compileTime/4, 1000),
//     pdfCachingMaxProcessingTime).
//   - tracker.flush() runs on success AND failure (Node finally-block
//     semantics) — partial tracker state is persisted so the next cycle can
//     use already-written ranges.
package contentcachemanager

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"clsi/xrefparser"

	clserrors "clsi/errors"
)

// MaxProcessingTimeMS mirrors Settings.pdfCachingMaxProcessingTime
// (default 10 * 1000 ms); tests may override.
var MaxProcessingTimeMS = int64(10 * 1000)

// Now mirrors Date.now for the deadline checker; tests may override.
var Now = func() time.Time { return time.Now() }

// ContentRange describes one uncompressed PDF stream object,
// serialized as {objectId, start, end, hash}.
type ContentRange struct {
	ObjectID string `json:"objectId"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Hash     string `json:"hash"`
}

// UpdateArgs mirrors the argument object passed to update().
type UpdateArgs struct {
	ContentDir             string
	FilePath               string
	PdfSize                int64
	PdfCachingMinChunkSize int64
	CompileTime            float64
}

// UpdateResult mirrors the result object returned by update().
// OverheadDeleteStaleHashes and StartXRefTable are pointers so the
// absent-key wire shape matches Node's "undefined" fields (absent = nil).
// TimedOutErr is non-nil for a soft deadline timeout.
type UpdateResult struct {
	ContentRanges             []ContentRange
	NewContentRanges          []ContentRange
	ReclaimedSpace            int64
	OverheadDeleteStaleHashes *int64
	TimedOutErr               error
	StartXRefTable            *int64
}

// deadlineChecker mirrors getDeadlineChecker.
type deadlineChecker struct {
	timeout         float64
	deadline        time.Time
	lastStage       string
	lastStageAt     time.Time
	completedStages int
}

func getDeadlineChecker(compileTime float64, now func() time.Time) *deadlineChecker {
	timeout := math.Min(math.Max(compileTime/4, 1000),
		float64(MaxProcessingTimeMS))
	n := now()
	return &deadlineChecker{
		timeout:         timeout,
		deadline:        n.Add(time.Duration(timeout * float64(time.Millisecond))),
		lastStage:       "start",
		lastStageAt:     n,
		completedStages: 0,
	}
}

// breached mirrors the JS `throw new TimedOutError(...)`: returns non-nil
// when the deadline is exceeded, otherwise updates stage bookkeeping and
// returns nil.
func (d *deadlineChecker) breached(stage string) *clserrors.TimedOutError {
	now := Now()
	if now.After(d.deadline) {
		return &clserrors.TimedOutError{
			Message: stage, // OError carries the stage name as message
			Info: map[string]any{
				"timeout":         d.timeout,
				"completedStages": d.completedStages,
				"lastStage":       d.lastStage,
				"diffToLastStage": now.Sub(d.lastStageAt).Milliseconds(),
			},
		}
	}
	d.completedStages++
	d.lastStage = stage
	d.lastStageAt = now
	return nil
}

// hashFileTracker mirrors HashFileTracker.
//
// ageOrder / sizeOrder preserve JS Map insertion order that flows into
// .state.v0.json serialization.
type hashFileTracker struct {
	contentDir string
	hashAge    map[string]int
	hashSize   map[string]int64
	ageOrder   []string
	sizeOrder  []string
}

func statePath(contentDir string) string {
	return filepath.Join(contentDir, ".state.v0.json")
}

func (t *hashFileTracker) has(hash string) bool {
	_, ok := t.hashAge[hash]
	return ok
}

// track mirrors HashFileTracker.track.
func (t *hashFileTracker) track(hash string, size int64) {
	if _, seen := t.hashSize[hash]; !seen {
		t.hashSize[hash] = size
		t.sizeOrder = append(t.sizeOrder, hash)
	}
	if _, seen := t.hashAge[hash]; !seen {
		t.ageOrder = append(t.ageOrder, hash)
	}
	t.hashAge[hash] = 0
}

// updateAge increments every existing hash's age by one.
func (t *hashFileTracker) updateAge() {
	for _, h := range t.ageOrder {
		t.hashAge[h]++
	}
}

// findStale returns the hashes whose age exceeds maxAge.
func (t *hashFileTracker) findStale(maxAge int) []string {
	var stale []string
	for _, h := range t.ageOrder {
		if t.hashAge[h] > maxAge {
			stale = append(stale, h)
		}
	}
	return stale
}

// flush writes .state.v0.json atomically (write to "<state>~", rename).
func (t *hashFileTracker) flush() error {
	blob, err := json.Marshal(struct {
		HashAge  [][]any `json:"hashAge"`
		HashSize [][]any `json:"hashSize"`
	}{
		HashAge:  t.ageEntries(),
		HashSize: t.sizeEntries(),
	})
	if err != nil {
		return err
	}
	atomicWrite := statePath(t.contentDir) + "~"
	if err := os.WriteFile(atomicWrite, blob, 0o644); err != nil {
		_ = os.Remove(atomicWrite)
		return err
	}
	if err := os.Rename(atomicWrite, statePath(t.contentDir)); err != nil {
		_ = os.Remove(atomicWrite)
		return err
	}
	return nil
}

func (t *hashFileTracker) ageEntries() [][]any {
	out := make([][]any, 0, len(t.ageOrder))
	for _, h := range t.ageOrder {
		out = append(out, []any{h, t.hashAge[h]})
	}
	return out
}

func (t *hashFileTracker) sizeEntries() [][]any {
	out := make([][]any, 0, len(t.sizeOrder))
	for _, h := range t.sizeOrder {
		out = append(out, []any{h, t.hashSize[h]})
	}
	return out
}

// deleteStaleHashes removes hash files whose age exceeds n generations;
// ENOENT on unlink is ignored (previous cleanup cycle may have been
// interrupted). Returns [reclaimedSpace, overheadMS].
func (t *hashFileTracker) deleteStaleHashes(n int) (int64, int64, error) {
	t0 := Now()
	reclaimedSpace := int64(0)
	hashes := t.findStale(n)
	if len(hashes) == 0 {
		return reclaimedSpace, int64(Now().Sub(t0) / time.Millisecond), nil
	}
	for _, hash := range hashes {
		if err := os.Remove(filepath.Join(t.contentDir, hash)); err != nil &&
			!os.IsNotExist(err) {
			return 0, 0, fmt.Errorf("delete stale hash: %w", err)
		}
		size := t.hashSize[hash]
		delete(t.hashAge, hash)
		delete(t.hashSize, hash)
		t.ageOrder = removeString(t.ageOrder, hash)
		t.sizeOrder = removeString(t.sizeOrder, hash)
		reclaimedSpace += size
	}
	return reclaimedSpace, int64(Now().Sub(t0) / time.Millisecond), nil
}

func removeString(s []string, target string) []string {
	out := make([]string, 0, len(s))
	for _, v := range s {
		if v != target {
			out = append(out, v)
		}
	}
	return out
}

// trackerFrom mirrors HashFileTracker.from. A missing or corrupt state file
// yields a fresh (empty) tracker.
func trackerFrom(contentDir string) *hashFileTracker {
	t := &hashFileTracker{
		contentDir: contentDir,
		hashAge:    map[string]int{},
		hashSize:   map[string]int64{},
	}
	blob, err := os.ReadFile(statePath(contentDir))
	if err != nil {
		return t
	}
	var state struct {
		HashAge  [][]any `json:"hashAge"`
		HashSize [][]any `json:"hashSize"`
	}
	if err := json.Unmarshal(blob, &state); err != nil {
		return t
	}
	for _, e := range state.HashAge {
		if len(e) >= 2 {
			if h, ok := e[0].(string); ok {
				if n, ok := asInt64(e[1]); ok {
					if _, seen := t.hashAge[h]; !seen {
						t.ageOrder = append(t.ageOrder, h)
					}
					t.hashAge[h] = int(n)
				}
			}
		}
	}
	for _, e := range state.HashSize {
		if len(e) >= 2 {
			if h, ok := e[0].(string); ok {
				if n, ok := asInt64(e[1]); ok {
					if _, seen := t.hashSize[h]; !seen {
						t.sizeOrder = append(t.sizeOrder, h)
					}
					t.hashSize[h] = n
				}
			}
		}
	}
	return t
}

func asInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), true
	}
	return 0, false
}

// pdfStreamHash mirrors pdfStreamHash (sha256 hex of the stream bytes).
func pdfStreamHash(buffer []byte) string {
	sum := sha256.Sum256(buffer)
	return hex.EncodeToString(sum[:])
}

// writePdfStream mirrors writePdfStream (write to "<file>~", rename).
func writePdfStream(dir, hash string, buffer []byte) error {
	filename := filepath.Join(dir, hash)
	atomicWrite := filename + "~"
	if err := os.WriteFile(atomicWrite, buffer, 0o644); err != nil {
		_ = os.Remove(atomicWrite)
		return err
	}
	if err := os.Rename(atomicWrite, filename); err != nil {
		_ = os.Remove(atomicWrite)
		return err
	}
	return nil
}

// xrefObject is a parsed xref entry with its (post-fill) end offset.
type xrefObject struct {
	offset    int
	endOffset int
}

// Update mirrors the exported update() entry point.
func Update(a UpdateArgs) (res *UpdateResult, err error) {
	if a.PdfSize < a.PdfCachingMinChunkSize {
		return &UpdateResult{
			ContentRanges:    []ContentRange{},
			NewContentRanges: []ContentRange{},
			ReclaimedSpace:   0,
		}, nil
	}
	return updateSameEventLoop(a)
}

// updateSameEventLoop mirrors the Node updateSameEventLoop.
func updateSameEventLoop(a UpdateArgs) (res *UpdateResult, err error) {
	checkDeadline := getDeadlineChecker(a.CompileTime, Now)

	tracker := trackerFrom(a.ContentDir)
	tracker.updateAge()
	if te := checkDeadline.breached("after init HashFileTracker"); te != nil {
		return nil, te
	}

	reclaimedSpace, overheadDeleteStaleHashes, derr :=
		tracker.deleteStaleHashes(5)
	if derr != nil {
		return nil, derr
	}
	if te := checkDeadline.breached("after delete stale hashes"); te != nil {
		return nil, te
	}

	entries, parseErr := xrefparser.ParseXrefTable(a.FilePath, a.PdfSize)
	if parseErr != nil {
		// Node: NoXrefTableError propagates up from parseXrefTable.
		return nil, clserrors.NewNoXrefTableError(parseErr)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Offset < entries[j].Offset
	})
	if te := checkDeadline.breached("after parsing"); te != nil {
		return nil, te
	}

	uncompressedObjects := make([]xrefObject, 0, len(entries))
	for i := range entries {
		if i+1 >= len(entries) {
			// The last object should be part of the xRef table; ignore.
			continue
		}
		if !entries[i].Uncompressed {
			continue
		}
		object := xrefObject{
			offset:    entries[i].Offset,
			endOffset: entries[i+1].Offset,
		}
		size := object.endOffset - object.offset
		if size < int(a.PdfCachingMinChunkSize) {
			continue
		}
		uncompressedObjects = append(uncompressedObjects, object)
	}
	if te := checkDeadline.breached("after finding uncompressed"); te != nil {
		return nil, te
	}

	timedOutErr := error(nil)
	contentRanges := make([]ContentRange, 0, len(uncompressedObjects))
	newContentRanges := make([]ContentRange, 0, len(uncompressedObjects))

	handle, openErr := os.Open(a.FilePath)
	if openErr != nil {
		// Node: fs.open error propagates (no finally needed; tracker is
		// fresh and nothing has been tracked yet in that path).
		return nil, openErr
	}
	defer handle.Close()
	// Node finally: flush on success AND failure so the next cycle can use
	// the (partial) ranges. We flush here unconditionally at exit.
	defer func() {
		_ = tracker.flush()
	}()

	for idx, object := range uncompressedObjects {
		size := object.endOffset - object.offset
		buf := make([]byte, size)
		n, _ := handle.ReadAt(buf, int64(object.offset))
		if int64(n) != int64(size) {
			// Node: throw new OError('could not read full chunk', {object,
			// bytesRead}); OError does super(message), so err.message is
			// exactly this string.
			return nil, &clserrors.OError{
				Message: "could not read full chunk",
				Info: map[string]any{
					"object":    map[string]any{"offset": object.offset, "endOffset": object.endOffset},
					"bytesRead": n,
				},
			}
		}
		if te := checkDeadline.breached("after read " + itoa(idx)); te != nil {
			// Soft: return the partial ranges plus the soft marker.
			timedOutErr = te
			break
		}

		idxObj := bytes.Index(buf, []byte("obj"))
		var objectIDRaw, stream []byte
		var rawLen int
		if idxObj < 0 {
			// Node indexOf returns -1: objectIdRaw = buffer.subarray(0, -1)
			// (all but the last byte); buffer = buffer.subarray(objectIdRaw.
			// byteLength) leaves the last byte as the stream. The idxObj > 100
			// check does not fire at -1.
			objectIDRaw = buf[:len(buf)-1]
			stream = buf[len(buf)-1:]
			rawLen = len(buf) - 1
		} else {
			if idxObj > 100 {
				// Node: throw new OError('objectId is too large', {object,
				// idxObj}).
				return nil, &clserrors.OError{
					Message: "objectId is too large",
					Info: map[string]any{
						"object": map[string]any{"offset": object.offset, "endOffset": object.endOffset},
						"idxObj": idxObj,
					},
				}
			}
			objectIDRaw = buf[:idxObj]
			stream = buf[idxObj:]
			rawLen = idxObj
		}

		hash := pdfStreamHash(stream)
		if te := checkDeadline.breached("after hash " + itoa(idx)); te != nil {
			timedOutErr = te
			break
		}

		theRange := ContentRange{
			ObjectID: string(objectIDRaw),
			Start:    int64(object.offset) + int64(rawLen),
			End:      int64(object.endOffset),
			Hash:     hash,
		}

		if tracker.has(hash) {
			// Optimization: skip writing of already-seen hashes.
			tracker.track(hash, theRange.End-theRange.Start)
			contentRanges = append(contentRanges, theRange)
			continue
		}

		if werr := writePdfStream(a.ContentDir, hash, stream); werr != nil {
			return nil, werr
		}
		tracker.track(hash, theRange.End-theRange.Start)
		contentRanges = append(contentRanges, theRange)
		newContentRanges = append(newContentRanges, theRange)
		if te := checkDeadline.breached("after write " + itoa(idx)); te != nil {
			timedOutErr = te
			break
		}
	}

	overheadPtr := &overheadDeleteStaleHashes
	// Node: xrefparser's startXRefTable is undefined (ParseXrefTable in this
	// deployment returns only {xRefEntries}); keep the wire key as nil to
	// mirror "undefined".
	return &UpdateResult{
		ContentRanges:             contentRanges,
		NewContentRanges:          newContentRanges,
		ReclaimedSpace:            reclaimedSpace,
		OverheadDeleteStaleHashes: overheadPtr,
		TimedOutErr:               timedOutErr,
		StartXRefTable:            nil,
	}, nil
}

func itoa(i int) string { return fmt.Sprintf("%d", i) }
