package updatemanager

import (
	"errors"
	"reflect"
	"testing"

	"document-updater/internal/preview"
	"document-updater/internal/rangesmanager"
	"ollitex/go/libraries/rangestracker"
)

// applyGroup mirrors the oracle applyUpdate group fixtures:
// lines ['original','lines'], version 34, appliedOps [{v:42, op:'mock-op-42'},
// {v:45, op:'mock-op-45'}], updatedLines ['after','updates'],
// historyUpdates ['history-update-1','history-update-2','history-update-3'],
// project_ops_length 123 (the oracle's queueOps fake returns ops.length).
type applyGroup struct {
	lines            []string
	updatedLines     []string
	version          int
	appliedOps       []any
	rngBefore        *rangesmanager.Ranges
	rngAfter         *rangesmanager.Ranges
	rngWereCollapsed bool
	historyUpdates   []rangesmanager.HistoryUpdate
	removed          []string
	shareJsErr       error
	docType          string
	previews         []preview.SparseChangePreview
	queueErr         error
}

func newApplyGroup() *applyGroup {
	g := &applyGroup{
		lines:        []string{"original", "lines"},
		updatedLines: []string{"after", "updates"},
		version:      34,
		appliedOps: []any{
			map[string]any{"v": 42, "op": "mock-op-42"},
			map[string]any{"v": 45, "op": "mock-op-45"},
		},
		rngWereCollapsed: false,
		historyUpdates: []rangesmanager.HistoryUpdate{
			{Doc: "history-update-1"},
			{Doc: "history-update-2"},
			{Doc: "history-update-3"},
		},
		removed: []string{},
	}
	g.rngBefore = &rangesmanager.Ranges{}
	g.rngAfter = &rangesmanager.Ranges{}
	g.docType = "sharejs-text-ot"
	return g
}

// wire the oracle applyUpdate group into a Manager: getDoc returns the
// group's doc (history ranges support flag from the test), ShareJsApply the
// group's lines/version/appliedOps, ApplyRanges the group's result.
func (g *applyGroup) manager(histRS bool) (*Manager, *seamRecorder) {
	rec := &seamRecorder{}
	m := &Manager{
		GetDoc: func(projectID, docID string) (DocInfo, error) {
			rec.getDoc = "getDoc"
			return DocInfo{
				Lines:                g.lines,
				Version:              g.version,
				Ranges:               g.rngBefore,
				Pathname:             umPathname,
				ProjectHistoryID:     us("history-id-123"),
				HistoryRangesSupport: histRS,
				Type:                 g.docType,
			}, nil
		},
		ShareJsApply: func(projectID, docID string, u *Update, lines []string, version int) ([]string, int, []any, error) {
			rec.shareJs = shareJsCall{projectID: projectID, docID: docID, u: u, lines: lines, version: version}
			if g.shareJsErr != nil {
				return nil, 0, nil, g.shareJsErr
			}
			return g.updatedLines, g.version, g.appliedOps, nil
		},
		ApplyRanges: func(projectID, docID string, rng *rangesmanager.Ranges, updates []rangesmanager.Update, newDocLines []string, historyRangesSupport bool) (rangesmanager.ApplyUpdateResult, error) {
			rec.applyRanges = applyRangesCall{projectID: projectID, docID: docID, rng: rng, updates: updates, newDocLines: newDocLines, historyRangesSupport: historyRangesSupport}
			rec.order = append(rec.order, "applyRanges")
			return rangesmanager.ApplyUpdateResult{
				NewRanges:           *g.rngAfter,
				RangesWereCollapsed: g.rngWereCollapsed,
				HistoryUpdates:      g.historyUpdates,
				RemovedChangeIDs:    g.removed,
			}, nil
		},
		QueueOps: func(projectID string, ops []string) (int, error) {
			rec.queued = ops
			rec.queuedPID = projectID
			rec.order = append(rec.order, "queueOps")
			if g.queueErr != nil {
				return -1, g.queueErr
			}
			return len(ops), nil
		},
		RecordAndFlushHistoryOps: func(projectID string, updates []rangesmanager.HistoryUpdate, projectOpsLength int) {
			rec.flushed = updates
			rec.flushedLen = projectOpsLength
			rec.order = append(rec.order, "recordAndFlush")
		},
		RecordProjectNotificationTimestamp: func(projectID string, ts, userID any) error {
			rec.ts = ts
			rec.tsUserID = userID
			rec.order = append(rec.order, "recordTS")
			return nil
		},
		UpdateDocument: func(projectID, docID string, lines []string, version int, appliedOps []any, rng *rangesmanager.Ranges, meta map[string]any) error {
			rec.updateDoc = updateDocCall{projectID: projectID, docID: docID, lines: lines, version: version, appliedOps: appliedOps, rng: rng, meta: meta}
			rec.order = append(rec.order, "updateDocument")
			return nil
		},
		Inc: func(name string) { rec.incs = append(rec.incs, name) },
		Now: func() int64 { return 555 },
		SendData: func(payload map[string]any) {
			rec.sent = payload
		},
		BuildPreviews: func(changes []preview.Change, lines []string) []preview.SparseChangePreview {
			rec.buildPreviews = true
			rec.previewChanges = changes
			rec.previewLines = lines
			return g.previews
		},
		Notify: func(projectID, docID string, authorIDs []string, userID any, previews []preview.SparseChangePreview) error {
			rec.notified = true
			rec.notifyAuthors = authorIDs
			rec.notifyUserID = userID
			rec.notifyPreviews = previews
			rec.order = append(rec.order, "notify")
			return nil
		},
		RecordSnapshot: func(projectID, docID string, previousVersion int, pathname string, lines []string, rng *rangesmanager.Ranges) error {
			rec.snapshot = snapshotCall{projectID: projectID, docID: docID, previousVersion: previousVersion, pathname: pathname, lines: lines, rng: rng}
			rec.order = append(rec.order, "recordSnapshot")
			return nil
		},
	}
	return m, rec
}

type shareJsCall struct {
	projectID string
	docID     string
	u         *Update
	lines     []string
	version   int
}

type applyRangesCall struct {
	projectID            string
	docID                string
	rng                  *rangesmanager.Ranges
	updates              []rangesmanager.Update
	newDocLines          []string
	historyRangesSupport bool
}

type updateDocCall struct {
	projectID  string
	docID      string
	lines      []string
	version    int
	appliedOps []any
	rng        *rangesmanager.Ranges
	meta       map[string]any
}

// seamRecorder captures the oracle's `calledWith` pins and order
// (`calledAfter`/`calledBefore`) assertions.
type seamRecorder struct {
	getDoc         string
	shareJs        shareJsCall
	applyRanges    applyRangesCall
	updateDoc      updateDocCall
	queued         []string
	queuedPID      string
	flushed        []rangesmanager.HistoryUpdate
	flushedLen     int
	ts, tsUserID   any
	incs           []string
	order          []string
	sent           map[string]any
	buildPreviews  bool
	previewChanges []preview.Change
	previewLines   []string
	notified       bool
	notifyAuthors  []string
	notifyUserID   any
	notifyPreviews []preview.SparseChangePreview
	snapshot       snapshotCall
}

type snapshotCall struct {
	projectID       string
	docID           string
	previousVersion int
	pathname        string
	lines           []string
	rng             *rangesmanager.Ranges
}

// --- oracle 'should apply the updates via ShareJS' --------------------------

func TestApplyUpdateShareJsArgs(t *testing.T) {
	g := newApplyGroup()
	m, rec := g.manager(false)
	u := umUpdate()

	if err := m.ApplyUpdate(umProjectID, umDocID, u); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	if !reflect.DeepEqual(rec.shareJs.lines, g.lines) || rec.shareJs.version != g.version {
		t.Fatalf("ShareJsApply lines/version = %v/%d, want %v/%d", rec.shareJs.lines, rec.shareJs.version, g.lines, g.version)
	}
	if rec.shareJs.projectID != umProjectID || rec.shareJs.docID != umDocID {
		t.Fatalf("ShareJsApply project/doc = %s/%s, want %s/%s", rec.shareJs.projectID, rec.shareJs.docID, umProjectID, umDocID)
	}
}

// --- oracle 'should update the ranges' ---------------------------------------

func TestApplyUpdateRangesArgs(t *testing.T) {
	g := newApplyGroup()
	m, rec := g.manager(false)

	if err := m.ApplyUpdate(umProjectID, umDocID, umUpdate()); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	if rec.applyRanges.projectID != umProjectID || rec.applyRanges.docID != umDocID {
		t.Fatalf("ApplyRanges project/doc = %s/%s", rec.applyRanges.projectID, rec.applyRanges.docID)
	}
	if !reflect.DeepEqual(rec.applyRanges.newDocLines, g.updatedLines) {
		t.Fatalf("ApplyRanges newDocLines = %v, want %v", rec.applyRanges.newDocLines, g.updatedLines)
	}
	// The raw appliedOps are bridged to typed updates, one per applied op
	// (the oracle pins the RAW appliedOps end to end; the Go bridge
	// degrades the mock 'op' string to a 0-op typed Update).
	if want := len(g.appliedOps); len(rec.applyRanges.updates) != want {
		t.Fatalf("ApplyRanges updates = %d, want %d", len(rec.applyRanges.updates), want)
	}
	for i, u := range rec.applyRanges.updates {
		wantV := 0
		if od, ok := g.appliedOps[i].(map[string]any); ok {
			wantV = int(od["v"].(int))
		}
		if u.Doc != umDocID || u.V == nil || *u.V != wantV {
			t.Fatalf("ApplyRanges update %d doc/v = %s/%v", i, u.Doc, u.V)
		}
	}
}

// --- oracle 'should save the document' ---------------------------------------

func TestApplyUpdateDocumentArgs(t *testing.T) {
	g := newApplyGroup()
	m, rec := g.manager(false)
	u := umUpdate()

	if err := m.ApplyUpdate(umProjectID, umDocID, u); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	if !reflect.DeepEqual(rec.updateDoc.lines, g.updatedLines) || rec.updateDoc.version != g.version {
		t.Fatalf("UpdateDocument lines/version = %v/%d", rec.updateDoc.lines, rec.updateDoc.version)
	}
	// RAW wire appliedOps verbatim (the oracle pins the raw passthrough).
	if !reflect.DeepEqual(rec.updateDoc.appliedOps, g.appliedOps) {
		t.Fatalf("UpdateDocument appliedOps = %v, want raw %v", rec.updateDoc.appliedOps, g.appliedOps)
	}
	if !reflect.DeepEqual(*rec.updateDoc.rng, *g.rngAfter) {
		t.Fatalf("UpdateDocument ranges = %v, want %v", *rec.updateDoc.rng, *g.rngAfter)
	}
	if rec.updateDoc.meta["user_id"] != "last-author-fake-id" {
		t.Fatalf("UpdateDocument meta = %v", rec.updateDoc.meta)
	}
}

// --- oracle 'should push the applied ops into the history queue' ------------

func TestApplyUpdateHistoryQueue(t *testing.T) {
	g := newApplyGroup()
	m, rec := g.manager(false)

	if err := m.ApplyUpdate(umProjectID, umDocID, umUpdate()); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	// Metrics.inc('history-queue') when historyUpdates is non-empty.
	if len(rec.incs) != 1 || rec.incs[0] != "history-queue" {
		t.Fatalf("incs = %v, want [history-queue]", rec.incs)
	}
	// queueOps(project_id, ...historyUpdates.map(JSON.stringify)) — 3 ops.
	if rec.queuedPID != umProjectID || len(rec.queued) != 3 {
		t.Fatalf("queued = %q, %d", rec.queuedPID, len(rec.queued))
	}
	// recordAndFlushHistoryOps(project_id, historyUpdates, historyUpdates.length).
	if len(rec.flushed) != 3 || rec.flushedLen != 3 {
		t.Fatalf("flushed = %d/%d, want 3/3", len(rec.flushed), rec.flushedLen)
	}
}

// --- oracle 'should record the project notification timestamp' ---------------

func TestApplyUpdateRecordTS(t *testing.T) {
	g := newApplyGroup()
	m, rec := g.manager(false)

	if err := m.ApplyUpdate(umProjectID, umDocID, umUpdate()); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	// sinon.match.number: no ts in meta -> the injected Now() (555).
	if ts, ok := rec.ts.(int64); !ok || ts != 555 {
		t.Fatalf("ts = %v, want the Now() fallback (555)", rec.ts)
	}
	if rec.tsUserID != "last-author-fake-id" {
		t.Fatalf("ts userID = %v, want the update meta user_id", rec.tsUserID)
	}
}

func TestApplyUpdateTSInMeta(t *testing.T) {
	g := newApplyGroup()
	m, rec := g.manager(false)
	u := umUpdate()
	u.Meta["ts"] = 1234567890

	if err := m.ApplyUpdate(umProjectID, umDocID, u); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	if rec.ts != 1234567890 {
		t.Fatalf("ts = %v, want the meta ts 1234567890", rec.ts)
	}
}

func TestApplyUpdateNoHistoryUpdates(t *testing.T) {
	g := newApplyGroup()
	g.historyUpdates = nil
	m, rec := g.manager(false)

	if err := m.ApplyUpdate(umProjectID, umDocID, umUpdate()); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	// No ts recorded, no queueing, no flush, no history-queue metric.
	for _, step := range rec.order {
		switch step {
		case "queueOps", "recordTS", "recordAndFlush":
		default:
		}
		if step == "queueOps" || step == "recordTS" || step == "recordAndFlush" {
			t.Fatalf("order %v must not include a history-queue step", rec.order)
		}
	}
	if len(rec.incs) != 0 {
		t.Fatalf("incs = %v, want none", rec.incs)
	}
}

// --- oracle 'with UTF-16 surrogate pairs in the update' -----------------------

func TestApplyUpdateSurrogatePairsReplaced(t *testing.T) {
	// The oracle's input is '\uD835\uDC00' — two 16-bit surrogate units
	// (the JS 16-bit view of U+1D400). The vendor's sanitize replaces EACH
	// surrogate unit with U+FFFD, so op.i becomes '\uFFFD\uFFFD' in place
	// before ShareJsUpdateManager.applyUpdate runs.
	oraclePair := string([]rune{0x1D400}) // utf16 units D835 DC00
	g := newApplyGroup()
	m, rec := g.manager(false)
	u := &Update{Doc: umDocID, Op: []map[string]any{{"p": 42, "i": oraclePair}}}

	if err := m.ApplyUpdate(umProjectID, umDocID, u); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	want := "\uFFFD\uFFFD" // the vendor 'surrogate pairs removed' pin
	if got, ok := rec.shareJs.u.Op[0]["i"].(string); !ok || got != want {
		t.Fatalf("surrogate i = %q, want %q (each 16-bit unit replaced)", got, want)
	}
	// The oracle mutates the update in place (it then pins `update.op[0].i`).
	if got, ok := u.Op[0]["i"].(string); !ok || got != want {
		t.Fatalf("update mutated to %q, want %q", got, want)
	}
}

func TestSanitizeUpdatesLeaveTextAlone(t *testing.T) {
	m := &Manager{}
	u := &Update{Doc: umDocID, Op: []map[string]any{{"p": 0, "i": "hello ☺"}}}
	m.SanitizeUpdate(u)
	if got := u.Op[0]["i"].(string); got != "hello ☺" {
		t.Fatalf("sanitize mangled text: %q", got)
	}
}

// --- oracle 'with an error' -----------------------------------------------------

func TestApplyUpdateErrorBroadcast(t *testing.T) {
	g := newApplyGroup()
	g.shareJsErr = errors.New("something went wrong")
	m, rec := g.manager(false)

	err := m.ApplyUpdate(umProjectID, umDocID, umUpdate())
	if err == nil || err.Error() != "something went wrong" {
		t.Fatalf("ApplyUpdate err = %v, want the ShareJs error", err)
	}
	// RealTimeRedisManager.sendData({project_id, doc_id, error}) + rethrow.
	if rec.sent["project_id"] != umProjectID || rec.sent["doc_id"] != umDocID {
		t.Fatalf("sendData payload = %v", rec.sent)
	}
	if msg, ok := rec.sent["error"].(string); !ok || msg != "something went wrong" {
		t.Fatalf("sendData error = %v, want the bare message", rec.sent["error"])
	}
	// Nothing runs after the failing step.
	for _, step := range rec.order {
		if step != "" {
			t.Fatalf("order %v: no pipeline step may follow the ShareJs error", rec.order)
		}
	}
}

// --- oracle 'when ranges get collapsed' ----------------------------------------

func TestApplyUpdateCollapsedSnapshot(t *testing.T) {
	g := newApplyGroup()
	g.rngWereCollapsed = true
	m, rec := g.manager(false)

	if err := m.ApplyUpdate(umProjectID, umDocID, umUpdate()); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	// Metrics.inc('doc-snapshot').
	incs := map[string]bool{}
	for _, n := range rec.incs {
		incs[n] = true
	}
	if !incs["doc-snapshot"] {
		t.Fatalf("incs = %v, want doc-snapshot", rec.incs)
	}
	// recordSnapshot(project, doc, PREVIOUS version, pathname, PRE lines, PRE ranges).
	if rec.snapshot.projectID != umProjectID || rec.snapshot.docID != umDocID {
		t.Fatalf("snapshot project/doc = %s/%s", rec.snapshot.projectID, rec.snapshot.docID)
	}
	if rec.snapshot.previousVersion != g.version {
		t.Fatalf("snapshot version = %d, want previous %d", rec.snapshot.previousVersion, g.version)
	}
	if rec.snapshot.pathname != umPathname {
		t.Fatalf("snapshot pathname = %s", rec.snapshot.pathname)
	}
	if !reflect.DeepEqual(rec.snapshot.lines, g.lines) {
		t.Fatalf("snapshot lines = %v, want pre-update lines", rec.snapshot.lines)
	}
	if rec.snapshot.rng != g.rngBefore {
		t.Fatalf("snapshot ranges must be the PRE-update ranges")
	}
}

// --- oracle 'when history ranges are supported' --------------------------------

func TestApplyUpdateHistoryRangesSupport(t *testing.T) {
	g := newApplyGroup()
	g.rngWereCollapsed = false
	m, rec := g.manager(true)

	if err := m.ApplyUpdate(umProjectID, umDocID, umUpdate()); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	// The HRS path still queues the serialized updates and flushes with the
	// project ops length.
	if rec.queuedPID != umProjectID || len(rec.queued) != 3 {
		t.Fatalf("queued = %q, %d", rec.queuedPID, len(rec.queued))
	}
	if !rec.applyRanges.historyRangesSupport {
		t.Fatal("the HRS flag must reach ApplyRanges")
	}
	if len(rec.flushed) != 3 || rec.flushedLen != 3 {
		t.Fatalf("flushed = %d/%d, want 3/3", len(rec.flushed), rec.flushedLen)
	}
}

// --- oracle 'when tracked changes are rejected' --------------------------------

func TestApplyUpdateRejectedNotify(t *testing.T) {
	g := newApplyGroup()
	g.removed = []string{"change-1", "change-2"}
	g.rngBefore = &rangesmanager.Ranges{
		Changes: []rangestracker.Change{
			{ID: "change-1", Metadata: rangestracker.Metadata{"user_id": "author-1"}},
			{ID: "change-2", Metadata: rangestracker.Metadata{"user_id": "author-2"}},
			{ID: "change-untouched", Metadata: rangestracker.Metadata{"user_id": "author-3"}},
		},
	}
	g.previews = []preview.SparseChangePreview{{SectionPath: []string{"Intro"}, Slice: "x"}}
	m, rec := g.manager(false)

	if err := m.ApplyUpdate(umProjectID, umDocID, umUpdate()); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	// buildSparseChangePreviews({changes, lines}) with the rejected changes
	// (id + metadata.user_id), in the pre-update range order.
	wantIDs := []string{"change-1", "change-2"}
	wantAuthors := []string{"author-1", "author-2"}
	for i, c := range rec.previewChanges {
		if !reflect.DeepEqual(*c.ID, wantIDs[i]) {
			t.Fatalf("preview change %d id = %v", i, c.ID)
		}
		if c.Metadata == nil || c.Metadata.UserID != wantAuthors[i] {
			t.Fatalf("preview change %d metadata = %v", i, c.Metadata)
		}
	}
	if !reflect.DeepEqual(rec.previewLines, g.lines) {
		t.Fatalf("preview lines = %v, want the pre-update lines", rec.previewLines)
	}
	// notify(project, doc, [author-1, author-2], update.meta.user_id, previews).
	if !rec.notified {
		t.Fatal("Notify must be called for rejected tracked changes")
	}
	if want := []string{"author-1", "author-2"}; !reflect.DeepEqual(rec.notifyAuthors, want) {
		t.Fatalf("notify authors = %v, want %v", rec.notifyAuthors, want)
	}
	if rec.notifyUserID != "last-author-fake-id" {
		t.Fatalf("notify userID = %v, want the update meta user_id", rec.notifyUserID)
	}
	if !reflect.DeepEqual(rec.notifyPreviews, g.previews) {
		t.Fatalf("notify previews = %v, want the builder previews", rec.notifyPreviews)
	}
}

func TestApplyUpdateNoRejectedNoNotify(t *testing.T) {
	g := newApplyGroup()
	m, rec := g.manager(false)

	if err := m.ApplyUpdate(umProjectID, umDocID, umUpdate()); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}
	if rec.notified {
		t.Fatal("Notify must not be called when nothing is rejected")
	}
}

// --- FromOpData (production raw -> typed bridge) ------------------------------

func TestFromOpDataBridgesWireOps(t *testing.T) {
	applied := []any{
		map[string]any{
			"v":    42,
			"meta": map[string]any{"tc": "tracking-info"},
			"op": []any{
				map[string]any{"i": "foo", "p": 4},
				map[string]any{"d": "bar", "p": 9},
			},
		},
		map[string]any{"v": 45, "op": "mock-op-45"},
	}
	got := FromOpData(umDocID, applied)
	if len(got) != 2 {
		t.Fatalf("FromOpData len = %d, want 2", len(got))
	}
	u0 := got[0]
	if u0.Doc != umDocID || u0.V == nil || *u0.V != 42 {
		t.Fatalf("bridged update 0 = %+v", u0)
	}
	if len(u0.Op) != 2 {
		t.Fatalf("bridged update 0 ops = %d, want 2", len(u0.Op))
	}
	if u0.Op[0].I == nil || *u0.Op[0].I != "foo" || u0.Op[0].P != 4 {
		t.Fatalf("bridged op 0 = %+v", u0.Op[0])
	}
	if u0.Op[1].D == nil || *u0.Op[1].D != "bar" || u0.Op[1].P != 9 {
		t.Fatalf("bridged op 1 = %+v", u0.Op[1])
	}
	if u0.Meta["tc"] != "tracking-info" {
		t.Fatalf("bridged meta = %v", u0.Meta)
	}
	// The mock 'op' string is not a wire component list: it degrades to a
	// 0-op typed update (the documented mock-degradation of the raw bridge).
	g1 := got[1]
	if g1.Doc != umDocID || g1.V == nil || *g1.V != 45 || len(g1.Op) != 0 {
		t.Fatalf("bridged update 1 = %+v", g1)
	}
	if got := FromOpData(umDocID, nil); got != nil {
		t.Fatalf("FromOpData(nil) = %v, want nil", got)
	}
}

// --- _adjustHistoryUpdatesMetadata (direct exported method) -----------------

// adjustUpdates mirrors the oracle fixture (4 updates / 2 delete changes in
// the pre-update ranges); the oracle's deep equal pins the exact
// {projectHistoryId, v, op, meta} shapes after the adjust run.
func adjustFixtures() []rangesmanager.HistoryUpdate {
	return []rangesmanager.HistoryUpdate{
		{
			V: iv(42),
			Op: []rangesmanager.HistoryOp{
				{I: us("bing"), P: 12, TrackedDeleteRejection: true},
				{I: us("foo"), P: 4},
				{I: us("bar"), P: 6},
			},
		},
		{
			V: iv(45),
			Op: []rangesmanager.HistoryOp{
				{D: us("qux"), P: 4},
				{I: us("bazbaz"), P: 14},
				{D: us("bong"), P: 28, TrackedChanges: []rangesmanager.HistoryDeleteTrackedChange{
					{Type: "insert", Offset: 0, Length: 4},
				}},
			},
			Meta: map[string]any{"tc": "tracking-info"},
		},
		{V: iv(47), Op: []rangesmanager.HistoryOp{{D: us("so"), P: 0}}},
		{V: iv(49), Op: []rangesmanager.HistoryOp{{I: us("penguin"), P: 18}}},
	}
}

func TestAdjustWithoutHistoryRangesSupport(t *testing.T) {
	m := &Manager{}
	updates := adjustFixtures()
	rng := &rangesmanager.Ranges{Changes: []rangestracker.Change{
		{Op: rangestracker.Op{D: us("bingbong"), P: 12}},
		{Op: rangestracker.Op{I: us("test"), P: 5}},
	}}
	m.AdjustHistoryUpdatesMetadata(updates, umPathname, us("history-id-123"),
		[]string{"some", "test", "data"}, rng, []string{"after", "updates"}, false)

	// {v: 42, 45, 47, 49} preserved (the oracle deep-comps each update).
	if updates[0].V == nil || *updates[0].V != 42 || updates[3].V == nil || *updates[3].V != 49 {
		t.Fatalf("versions mutated: %v %v", updates[0].V, updates[3].V)
	}
	if updates[0].ProjectHistoryID == nil || *updates[0].ProjectHistoryID != "history-id-123" {
		t.Fatalf("projectHistoryId = %v", updates[0].ProjectHistoryID)
	}
	lengths := []int{14, 24, 23, 21}
	for i, u := range updates {
		if u.Meta["pathname"] != umPathname {
			t.Fatalf("update %d meta.pathname = %v", i, u.Meta["pathname"])
		}
		if got := u.Meta["doc_length"]; got != lengths[i] {
			t.Fatalf("update %d doc_length = %v, want %d", i, got, lengths[i])
		}
		if _, ok := u.Meta["history_doc_length"]; ok {
			t.Fatalf("update %d has history_doc_length, want none", i)
		}
	}
	if _, ok := updates[0].Meta["doc_hash"]; ok {
		t.Fatal("doc_hash must not be set without history ranges support")
	}
	// !historyRangesSupport drops tracked-change metadata (vendor deletes
	// meta.tc so project-history can't process tracked changes).
	if _, ok := updates[1].Meta["tc"]; ok {
		t.Fatal("meta.tc must be deleted without history ranges support")
	}
}

func TestAdjustWithHistoryRangesSupport(t *testing.T) {
	m := &Manager{}
	updates := adjustFixtures()
	rng := &rangesmanager.Ranges{Changes: []rangestracker.Change{
		{Op: rangestracker.Op{D: us("bingbong"), P: 12}},
		{Op: rangestracker.Op{I: us("test"), P: 5}},
	}}
	m.AdjustHistoryUpdatesMetadata(updates, umPathname, us("history-id-123"),
		[]string{"some", "test", "data"}, rng, []string{"after", "updates"}, true)

	// doc_length: 14 + 'bing''foo''bar' = 24, - 'qux' + 'bazbaz' - 'bong' = 23,
	// - 'so' = 21 (vendor 14/24/23/21).
	// history_doc_length: seeded 14 + len('bingbong') = 22;
	//  + 'foo'+'bar' (the trackedDeleteRejection insert keeps its chars) = 28;
	//  - 'bong' (trackedChanges insert length 4) + 'bazbaz' = 30; - 'so' = 28.
	wants := []struct{ doc, hist int }{{14, 22}, {24, 28}, {23, 30}, {21, 28}}
	for i, u := range updates {
		if got := u.Meta["doc_length"]; got != wants[i].doc {
			t.Fatalf("update %d doc_length = %v, want %d", i, got, wants[i].doc)
		}
		if got := u.Meta["history_doc_length"]; got != wants[i].hist {
			t.Fatalf("update %d history_doc_length = %v, want %d", i, got, wants[i].hist)
		}
	}
	// The tracked-delete meta survives (history ranges support enabled), but
	// the final doc hash is only stamped on the last update.
	if updates[1].Meta["tc"] != "tracking-info" {
		t.Fatalf("meta.tc = %v, want 'tracking-info' kept", updates[1].Meta["tc"])
	}
	wantHash := "b59765029d070677a5fe52530286dccbe1359b2d" // sha1("after\nupdates")
	if updates[3].Meta["doc_hash"] != wantHash {
		t.Fatalf("last doc_hash = %v, want %s", updates[3].Meta["doc_hash"], wantHash)
	}
	if _, ok := updates[0].Meta["doc_hash"]; ok {
		t.Fatal("doc_hash must be on the last update only")
	}
}

func TestAdjustEmptyDocument(t *testing.T) {
	m := &Manager{}
	updates := []rangesmanager.HistoryUpdate{
		{V: iv(42), Op: []rangesmanager.HistoryOp{{I: us("foobar"), P: 0}}},
	}
	// ranges {} — vendor destructures ranges.changes ?? []; in Go the Ranges
	// value has no Changes key.
	m.AdjustHistoryUpdatesMetadata(updates, umPathname, us("history-id-123"),
		[]string{}, (&rangesmanager.Ranges{}), []string{"foobar"}, false)

	u := updates[0]
	if u.Meta["pathname"] != umPathname {
		t.Fatalf("meta.pathname = %v", u.Meta["pathname"])
	}
	if got := u.Meta["doc_length"]; got != 0 {
		t.Fatalf("doc_length = %v, want 0", got)
	}
}

// --- New() defaults ----------------------------------------------------------
//
// The vendor constructor assigns:
//   _isHistoryOTUpdate = HistoryOTUpdateManager.isHistoryOTUpdate (the guard)
//   buildPreviews      = preview (the sparse-preview builder)
// The oracle never exercises these (sandboxed-module stubs), so they are
// pinned here instead: Inc is a no-op, Now is wall-clock, and IsHistoryOT /
// BuildPreviews project the vendor's collaborators.

func TestNewDefaults(t *testing.T) {
	m := New()

	// Inc: the vendor's Metrics.inc — an unobservable side effect; the Go
	// default is a no-op function (nil-safety is the contract).
	if m.Inc == nil {
		t.Fatal("Inc default must be a no-op func, not nil")
	}
	m.Inc("any-metric") // panics if the contract is wrong

	// Now: wall-clock milliseconds (the oracle pins recordTS numbers).
	if got := m.Now(); got <= 0 {
		t.Fatalf("Now default returned %d, want a positive timestamp", got)
	}

	// IsHistoryOT: the vendor's HistoryOTUpdateManager.isHistoryOTUpdate
	// guard: every op must be a valid editor-core op.
	if !m.IsHistoryOT(&Update{Doc: "d", Op: []map[string]any{{"textOperation": "noop", "ranges": []any{}}}}) {
		t.Fatal("a textOperation op must be a history-OT edit operation")
	}
	if !m.IsHistoryOT(&Update{Doc: "d", Op: []map[string]any{{"noOp": struct{}{}}}}) {
		t.Fatal("a noOp op must be a history-OT edit operation")
	}
	if m.IsHistoryOT(&Update{Op: []map[string]any{{"i": "foo", "p": 4}}}) {
		t.Fatal("a plain sharejs op is NOT a history-OT edit operation")
	}
	if m.IsHistoryOT(&Update{Op: nil}) {
		t.Fatal("an update without ops must not be a history-OT edit operation")
	}

	// BuildPreviews: the preview package's sparse-change builder.
	hello := "hello"
	previewChange := preview.Change{
		Op: preview.Op{I: &hello, P: 0},
	}
	if got := m.BuildPreviews([]preview.Change{previewChange}, []string{"original", "lines"}); len(got) != 1 {
		t.Fatalf("BuildPreviews returned %d previews, want 1", len(got))
	}
}
