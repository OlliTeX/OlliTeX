package syncmanager

import (
	"context"
	"errors"
	"testing"
	"time"
)

// --- StartResyncWithoutLock branches (vendor) --------------------------------

func TestStartResyncWithoutLockOngoingSynced(t *testing.T) {
	d, fs, _ := baseDeps()
	fs.raw = map[string]any{
		"resyncProjectStructure": true,
		"resyncDocContents":      []any{},
		"stuckClearCount":        2,
		"resyncPendingSince":     testBase.Add(-time.Hour), // < 4h before Now → not stuck
	}
	fs.found = true
	err := d.StartResyncWithoutLock(context.Background(), "p", map[string]any{})
	so, ok := err.(*SyncOngoingError)
	if !ok {
		t.Fatalf("want SyncOngoingError, got %v", err)
	}
	if so.Msg != "sync ongoing" {
		t.Fatalf("want vendor message, got %q", so.Msg)
	}
	if so.Info["stuckClearCount"] != 3 {
		t.Fatalf("want stuckClearCount+1=3, got %v", so.Info)
	}
	if so.Info["projectId"] != "p" {
		t.Fatalf("want projectId info")
	}
}

func TestStartResyncWithoutLockStuckCleared(t *testing.T) {
	d, fs, _ := baseDeps()
	// d.Now() is frozen at testBase in baseDeps.
	fs.raw = map[string]any{
		"resyncProjectStructure": true,
		"resyncPendingSince":     testBase.Add(-5 * time.Hour), // > 4h before Now → stuck
		"stuckClearCount":        2,
	}
	fs.found = true
	incd := []string{}
	d.Inc = func(name string, n int, info map[string]any) { incd = append(incd, name) }
	warned := 0
	d.LogWarn = func(info map[string]any, msg string) {
		if msg == "sync stuck, clearing state and restarting" {
			warned++
		}
	}
	// continue after clearing: requestResync + setResyncState succeed
	if err := d.StartResyncWithoutLock(context.Background(), "p", map[string]any{}); err != nil {
		t.Fatalf("unexpected %v", err)
	}
	if warned != 1 {
		t.Fatalf("want stuck warn, got %d", warned)
	}
	found := false
	for _, n := range incd {
		if n == "project_history_sync_stuck_cleared" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want stuck-cleared metric, got %v", incd)
	}
	// the $inc stuckClearCount update must have been issued before requestResync
	sawInc := false
	for _, up := range fs.updates {
		if inc, ok := up["$inc"].(map[string]any); ok && inc["stuckClearCount"] == 1 {
			sawInc = true
		}
	}
	if !sawInc {
		t.Fatalf("want stuck clear recorded in state, updates: %v", fs.updates)
	}
}

func TestStartResyncWithoutLockPermanentlyStuck(t *testing.T) {
	d, fs, _ := baseDeps()
	fs.raw = map[string]any{
		"resyncProjectStructure": true,
		"stuckClearCount":        5, // == MAX_STUCK_CLEAR_ATTEMPTS → log + throw
	}
	fs.found = true
	incd := []string{}
	d.Inc = func(name string, n int, info map[string]any) { incd = append(incd, name) }
	errLogged := 0
	d.LogErr = func(info map[string]any, msg string) {
		if msg == "sync permanently stuck — exceeded auto-clear limit" {
			errLogged++
		}
	}
	err := d.StartResyncWithoutLock(context.Background(), "p", map[string]any{})
	if err == nil || err.Error() != "sync permanently stuck" {
		t.Fatalf("want permanent stuck error, got %v", err)
	}
	if errLogged != 1 {
		t.Fatalf("want error log exactly once, got %d", errLogged)
	}
	for _, n := range incd {
		if n == "project_history_sync_stuck_permanent" {
			return
		}
	}
	t.Fatalf("want permanent metric, got %v", incd)
}

// vendor: below the log threshold (stuckClearCount < max) no error log, but
// still throws.
func TestStartResyncWithoutLockPermanentlyStuckNoLog(t *testing.T) {
	d, fs, _ := baseDeps()
	fs.raw = map[string]any{
		"resyncProjectStructure": true,
		"stuckClearCount":        6, // > max: throws again, no log (only at === max)
	}
	fs.found = true
	// NOTE: vendor logs only when stuckClearCount === MAX (5). At 6 the state
	// should already be cleared by the previous cycle; the port still throws.
	errLogged := 0
	d.LogErr = func(info map[string]any, msg string) {
		if msg == "sync permanently stuck — exceeded auto-clear limit" {
			errLogged++
		}
	}
	err := d.StartResyncWithoutLock(context.Background(), "p", map[string]any{})
	if err == nil || err.Error() != "sync permanently stuck" {
		t.Fatalf("want error, got %v", err)
	}
	if errLogged != 0 {
		t.Fatalf("want no log at count>max, got %d", errLogged)
	}
}

func TestStartResyncWithoutLockNonOngoing(t *testing.T) {
	d, fs, _ := baseDeps()
	fs.raw = map[string]any{}
	fs.found = true
	webOpts := map[string]any{}
	d.RequestResync = func(ctx context.Context, pid string, opts map[string]any) error {
		webOpts = opts
		return nil
	}
	err := d.StartResyncWithoutLock(context.Background(), "p", map[string]any{
		"historyRangesMigration":     true,
		"resyncProjectStructureOnly": true,
		"hard":                       true,
		"recoverCorruptedFiles":      false,
		"origin":                     map[string]any{"kind": "custom"},
		"ignoredKey":                 "x", // must NOT leak into webOpts
	})
	if err != nil {
		t.Fatalf("unexpected %v", err)
	}
	if webOpts["historyRangesMigration"] != true || webOpts["resyncProjectStructureOnly"] != true {
		t.Fatalf("want both web opts, got %v", webOpts)
	}
	if len(webOpts) != 2 {
		t.Fatalf("web opts must be truthy-only, got %v", webOpts)
	}
	if len(fs.updates) != 1 {
		t.Fatalf("want setResyncState")
	}
	setv, _ := fs.updates[0]["$set"].(map[string]any)
	if setv["hardResync"] != true {
		t.Fatalf("want hardResync true")
	}
	origin, _ := setv["origin"].(map[string]any)
	if origin["kind"] != "custom" {
		t.Fatalf("want custom origin, got %v", origin)
	}
}

// vendor: default origin is {kind: history-resync}.
func TestStartResyncWithoutLockDefaultOrigin(t *testing.T) {
	d, fs, _ := baseDeps()
	fs.raw = map[string]any{}
	fs.found = true
	d.StartResyncWithoutLock(context.Background(), "p", nil)
	setv, _ := fs.updates[0]["$set"].(map[string]any)
	origin, _ := setv["origin"].(map[string]any)
	if origin["kind"] != "history-resync" {
		t.Fatalf("want default origin, got %v", origin)
	}
}

// --- SkipUpdatesDuringSync (vendor) ------------------------------------------

func TestSkipUpdatesDuringSyncNotOngoing(t *testing.T) {
	d, fs, _ := baseDeps()
	fs.raw = map[string]any{}
	fs.found = true
	updates := []map[string]any{{"version": 1}, {"doc": "d", "op": []any{}}}
	out, state, err := d.SkipUpdatesDuringSync(context.Background(), "p", updates)
	if err != nil {
		t.Fatal(err)
	}
	if state != nil {
		t.Fatalf("want null syncState when unchanged, got %v", state)
	}
	if len(out) != 2 {
		t.Fatalf("want all updates kept, got %d", len(out))
	}
}

func TestSkipUpdatesDuringSyncFiltering(t *testing.T) {
	d, fs, _ := baseDeps()
	fs.raw = map[string]any{
		"resyncProjectStructure": true,
		"resyncDocContents":      []any{},
	}
	fs.found = true
	skipped := 0
	d.Inc = func(name string, n int, info map[string]any) {
		if name == "project_history_sync_update_skipped" {
			skipped++
		}
	}
	updates := []map[string]any{
		{"version": 2}, // skipped (structure syncing)
		{"resyncProjectStructure": map[string]any{"docs": []any{map[string]any{"path": "a.tex"}}}}, // sync update: kept, flips state
		{"doc": "d", "op": []any{}, "meta": map[string]any{"pathname": "a.tex", "doc_length": 1}},  // kept? after first update, structure sync stopped, doc a.tex syncing → skipped
		{"doc": "d", "op": []any{}, "meta": map[string]any{"pathname": "b.tex", "doc_length": 1}},  // b.tex not syncing → kept
	}
	out, state, err := d.SkipUpdatesDuringSync(context.Background(), "p", updates)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatalf("want syncState returned when ongoing")
	}
	// vendor: the v2 update is skipped (structure was syncing at that point);
	// the structure-sync update is kept and flips the state to doc a.tex;
	// the a.tex text update is skipped; the b.tex text update is kept.
	if len(out) != 2 {
		t.Fatalf("want 2 kept, got %d: %v", len(out), out)
	}
	if _, ok := out[0]["resyncProjectStructure"]; !ok {
		t.Fatalf("first kept must be the structure sync update, got %v", out[0])
	}
	if skipped != 2 {
		t.Fatalf("want 2 skipped, got %d", skipped)
	}
}

// --- ExpandSyncUpdates (vendor) ----------------------------------------------

func testExpanderDeps(t *testing.T) *Deps {
	d, _, _ := baseDeps()
	d.GetLatestSnapshotFilesForChunk = func(ctx context.Context, historyID string, chunk map[string]any) (map[string]*File, error) {
		return map[string]*File{
			"a.tex":   NewStringFile("a.tex", "persisted a"),
			"pic.png": {Pathname: "pic.png", Editable: false, DataHash: "oldhash", Metadata: map[string]any{"provider": "linked", "k": "v"}},
		}, nil
	}
	d.GetHistoryID = func(ctx context.Context, pid string) (string, error) { return "hid", nil }
	d.LoadFileContent = func(ctx context.Context, pid string, f *File) error {
		if f.Pathname == "a.tex" {
			f.Content = "persisted a"
		}
		return nil
	}
	d.GetBlobHashFromString = func(s string) string { return "h:" + s }
	d.DiffAsShareJsOps = func(a, b string) []any {
		// a minimal faithful-ish diff: delete a, insert b
		return []any{
			map[string]any{"d": a},
			map[string]any{"i": b, "p": 0},
		}
	}
	return d
}

func TestExpandNoSyncUpdatesFastPath(t *testing.T) {
	d := testExpanderDeps(t)
	updates := []map[string]any{{"version": 1}}
	out, err := d.ExpandSyncUpdates(context.Background(), "p", "hid", map[string]any{}, updates, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0]["version"] != 1 {
		t.Fatalf("want passthrough, got %v", out)
	}
}

func TestExpandInvalidFile(t *testing.T) {
	d := testExpanderDeps(t)
	d.GetLatestSnapshotFilesForChunk = func(ctx context.Context, historyID string, chunk map[string]any) (map[string]*File, error) {
		return map[string]*File{"x.tex": nil}, nil
	}
	_, err := d.ExpandSyncUpdates(context.Background(), "p", "hid", map[string]any{},
		[]map[string]any{{"resyncProjectStructure": map[string]any{}}}, nil)
	if err == nil || err.Error() != "file is missing isEditable method" {
		t.Fatalf("want invalid file error, got %v", err)
	}
}

func TestExpandStructureResync(t *testing.T) {
	d := testExpanderDeps(t)
	incd := map[string]int{}
	d.Inc = func(name string, n int, info map[string]any) {
		if st, ok := info["status"].(string); ok {
			incd[st] += n
		}
	}
	updates := []map[string]any{{
		"resyncProjectStructure": map[string]any{
			"docs": []any{map[string]any{"path": "a.tex", "doc": "docA"}},
			"files": []any{
				// pic.png persisted hash differs → out of sync binary
				map[string]any{"path": "pic.png", "file": "blobNew", "_hash": "newhash", "metadata": map[string]any{"provider": "linked", "k": "v"}},
				// brand-new missing file
				map[string]any{"path": "new.bin", "file": "blobX"},
			},
		},
		"meta": map[string]any{"ts": "ts1"},
	}}
	out, err := d.ExpandSyncUpdates(context.Background(), "p", "hid", map[string]any{}, updates, nil)
	if err != nil {
		t.Fatalf("unexpected %v", err)
	}
	// expected ops:
	//  remove unexpected: none (a.tex editable kept; pic.png non-editable matched)
	//  remove unexpected nonbinary: a.tex is expected → no remove
	//  add missing binary: new.bin
	//  add missing nonbinary: none (a.tex persisted editable)
	//  binary out of sync: remove pic.png + add pic.png
	//  metadata: pic.png metadata equal? persisted {provider,k} vs expected {provider,k} → equal → no op
	var removes, adds int
	for _, op := range out {
		if _, ok := op["new_pathname"]; ok {
			removes++
		} else {
			adds++
		}
	}
	if removes != 1 {
		t.Fatalf("want 1 remove (pic.png), got %d: %v", removes, out)
	}
	if adds != 2 {
		t.Fatalf("want 2 adds (new.bin, pic.png), got %d: %v", adds, out)
	}
	if incd["add missing file"] != 1 {
		t.Fatalf("want 1 add-missing metric (new.bin), got %v", incd)
	}
	if incd["update binary file contents"] != 1 {
		t.Fatalf("want 1 binary-update metric, got %v", incd)
	}
}

func TestExpandStructurePartialAbort(t *testing.T) {
	d := testExpanderDeps(t)
	deleted := 0
	d.DeleteAppliedDocUpdate = func(ctx context.Context, pid string, update map[string]any) error {
		deleted++
		return nil
	}
	updates := []map[string]any{{
		"resyncProjectStructure": map[string]any{
			// z.tex is NOT in the persisted snapshot → an add op is queued for
			// a doc path → the partial-resync abort must fire (vendor).
			"docs": []any{map[string]any{"path": "z.tex", "doc": "docZ"}},
		},
		"resyncProjectStructureOnly": true,
		"meta":                       map[string]any{"ts": "ts1"}, // ensure meta.ts exists for the add op
	}}
	_, err := d.ExpandSyncUpdates(context.Background(), "p", "hid", map[string]any{}, updates, nil)
	if err == nil {
		t.Fatalf("want abort error")
	}
	if err.Error() != "aborting partial resync: touched doc" {
		t.Fatalf("want vendor abort message, got %v", err)
	}
	if deleted != 1 {
		t.Fatalf("want deleteAppliedDocUpdate called, got %d", deleted)
	}
}

func TestExpandPassthrough(t *testing.T) {
	d := testExpanderDeps(t)
	update := map[string]any{"version": 4}
	out, err := d.ExpandSyncUpdates(context.Background(), "p", "hid", map[string]any{},
		[]map[string]any{{
			"resyncProjectStructure": map[string]any{
				"docs":  []any{map[string]any{"path": "a.tex"}},
				"files": []any{},
			},
			"meta": map[string]any{"ts": "t"},
		}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = update
	if len(out) == 0 {
		t.Fatalf("want some expansions")
	}
}

func TestExpandDocContentHashMatch(t *testing.T) {
	d := testExpanderDeps(t)
	// snapshot file hash equals expected content hash → skip diff
	d.GetLatestSnapshotFilesForChunk = func(ctx context.Context, historyID string, chunk map[string]any) (map[string]*File, error) {
		f := NewStringFile("a.tex", "expected content")
		f.Hash = "h:expected content"
		return map[string]*File{"a.tex": f}, nil
	}
	updates := []map[string]any{{
		"doc":              "docA",
		"path":             "a.tex",
		"resyncDocContent": map[string]any{"content": "expected content"},
		"meta":             map[string]any{"ts": "ts1"},
	}}
	out, err := d.ExpandSyncUpdates(context.Background(), "p", "hid", map[string]any{}, updates, nil)
	if err != nil {
		t.Fatal(err)
	}
	// no content diff ops; comment/tracking lists empty → no updates except none
	if len(out) != 0 {
		t.Fatalf("want no queued updates on hash match, got %v", out)
	}
}

func TestExpandDocContentDiff(t *testing.T) {
	d := testExpanderDeps(t)
	updates := []map[string]any{{
		"doc":              "docA",
		"path":             "a.tex",
		"resyncDocContent": map[string]any{"content": "expected b"},
		"meta":             map[string]any{"ts": "ts1"},
	}}
	out, err := d.ExpandSyncUpdates(context.Background(), "p", "hid", map[string]any{}, updates, nil)
	if err != nil {
		t.Fatal(err)
	}
	var withOp bool
	for _, op := range out {
		if o, ok := op["op"].([]any); ok && len(o) == 2 {
			withOp = true
			meta := op["meta"].(map[string]any)
			if meta["pathname"] != "a.tex" || meta["doc_length"] != len("persisted a") {
				t.Fatalf("want meta with pathname+doc_length, got %v", meta)
			}
		}
	}
	if !withOp {
		t.Fatalf("want the diff op update, got %v", out)
	}
}

func TestExpandDocContentUnrecognised(t *testing.T) {
	d := testExpanderDeps(t)
	updates := []map[string]any{{
		"doc":              "docA",
		"path":             "missing.tex",
		"resyncDocContent": map[string]any{"content": "x"},
		"meta":             map[string]any{"ts": "t"},
	}}
	_, err := d.ExpandSyncUpdates(context.Background(), "p", "hid", map[string]any{}, updates, nil)
	if err == nil || err.Error() != "unrecognised file: not in snapshot" {
		t.Fatalf("want unrecognised error, got %v", err)
	}
}

func TestExpandDocContentRecoverCorrupted(t *testing.T) {
	d := testExpanderDeps(t)
	d.LoadFileContent = func(ctx context.Context, pid string, f *File) error {
		f.Content = ""
		return &UnprocessableError{Msg: "op apply failed"}
	}
	// recoverCorruptedFiles comes from the sync state:
	fsRaw := map[string]any{
		"resyncProjectStructure": false,
		"resyncDocContents":      []any{"a.tex"},
		"recoverCorruptedFiles":  true,
	}
	d.FindOneState = func(ctx context.Context, pid string) (map[string]any, bool, error) {
		return fsRaw, true, nil
	}
	updates := []map[string]any{{
		"doc":              "docA",
		"path":             "a.tex",
		"resyncDocContent": map[string]any{"content": "fresh content"},
		"meta":             map[string]any{"ts": "ts1"},
	}}
	out, err := d.ExpandSyncUpdates(context.Background(), "p", "hid", map[string]any{}, updates, nil)
	if err != nil {
		t.Fatalf("unexpected %v", err)
	}
	var found, hasRemove bool
	for _, op := range out {
		if op["docLines"] == "fresh content" {
			found = true
		}
		if np, ok := op["new_pathname"]; ok && np == "" {
			hasRemove = true
		}
	}
	if !found || !hasRemove {
		t.Fatalf("want remove+re-add pair, got %v", out)
	}
}

func TestExpandDocContentTooLong(t *testing.T) {
	d := testExpanderDeps(t)
	d.LoadFileContent = func(ctx context.Context, pid string, f *File) error {
		return &UnprocessableError{Msg: "corrupt"}
	}
	fsRaw := map[string]any{"resyncDocContents": []any{"a.tex"}, "recoverCorruptedFiles": true}
	d.FindOneState = func(ctx context.Context, pid string) (map[string]any, bool, error) {
		return fsRaw, true, nil
	}
	long := make([]byte, maxStringLength+1)
	for i := range long {
		long[i] = 'a'
	}
	updates := []map[string]any{{
		"doc":              "docA",
		"path":             "a.tex",
		"resyncDocContent": map[string]any{"content": string(long)},
		"meta":             map[string]any{"ts": "t"},
	}}
	_, err := d.ExpandSyncUpdates(context.Background(), "p", "hid", map[string]any{}, updates, nil)
	tl, ok := err.(*TooLongError)
	if !ok {
		t.Fatalf("want TooLongError, got %v", err)
	}
	if tl.Info["maxLength"] != maxStringLength {
		t.Fatalf("want maxLength info, got %v", tl.Info)
	}
}

func TestExpandDocContentTransientRethrown(t *testing.T) {
	d := testExpanderDeps(t)
	transient := errors.New("redis timeout")
	d.LoadFileContent = func(ctx context.Context, pid string, f *File) error {
		return transient
	}
	fsRaw := map[string]any{"resyncDocContents": []any{"a.tex"}, "recoverCorruptedFiles": true}
	d.FindOneState = func(ctx context.Context, pid string) (map[string]any, bool, error) {
		return fsRaw, true, nil
	}
	updates := []map[string]any{{
		"doc":              "docA",
		"path":             "a.tex",
		"resyncDocContent": map[string]any{"content": "x"},
		"meta":             map[string]any{"ts": "t"},
	}}
	_, err := d.ExpandSyncUpdates(context.Background(), "p", "hid", map[string]any{}, updates, nil)
	if err != transient {
		t.Fatalf("want the transient error rethrown, got %v", err)
	}
}
