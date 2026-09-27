package collab

// legacybackfill_test.go — D40 P4 pins (hermetic: fake docstore + fake TC
// chat; the room goes through the real OnLoadDocument hook so the full
// seed → backfill → read chain is covered).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
)

const (
	p4Text      = "hello world this is a document"
	p4Room      = "4142434445464748494a4b4c" // 24-hex room (seedHex24 gate)
	p4RootDocID = "aaaa00000000000000000000"
)

// ---- pure materialization ---------------------------------------------------

func legacyP4Corpus() LegacyData {
	return LegacyData{
		Threads: []LegacyThread{
			{
				ID:         "th1",
				Resolved:   true,
				ResolvedAt: 2000,
				Messages: []LegacyMessage{
					{ID: "m1", Content: "first note", Timestamp: 1000, UserID: "6userA"},
					{ID: "m2", Content: "reply", Timestamp: 1500, UserID: "6userB", EditedAt: &[]int64{1600}[0]},
				},
			},
			{
				ID: "th2",
				Messages: []LegacyMessage{
					{ID: "m3", Content: "open thought", Timestamp: 500, UserID: "6userA"},
				},
			},
		},
		Pointers: []LegacyPointer{
			{ID: "pt1", ThreadID: "th1", Start: 6}, // zero-length range over "world"
			{ID: "pt2", ThreadID: "th2", Start: 0},
			{ID: "pt3", ThreadID: "thGONE", Start: 9}, // dangling pointer (no thread)
		},
		Changes: []LegacyChangeOp{
			{ID: "c1", InsText: "XYZ", Start: 0, State: "pending"},
			{ID: "c2", DelText: "abc", Start: 5, State: "accepted"},
			{ID: "empty"}, // no text — must not materialize
		},
	}
}

func TestMaterializeLegacy_P4(t *testing.T) {
	threads, comments, changes := MaterializeLegacy(legacyP4Corpus(), "main.tex")

	if len(threads) != 2 {
		t.Fatalf("threads = %d, want 2", len(threads))
	}
	byID := map[string]Thread{}
	for _, th := range threads {
		byID[th.ID] = th
	}
	th1 := byID["th1"]
	if th1.State != ThreadStateResolved || th1.Resolved != 2000 {
		t.Fatalf("th1 resolved mapping: %+v", th1)
	}
	if th1.Created != 1000 {
		t.Fatalf("th1.Created = %d, want first-message ts 1000", th1.Created)
	}
	if th1.Author == nil || th1.Author["user_id"] != "6userA" {
		t.Fatalf("th1.Author = %#v (want first message user 6userA)", th1.Author)
	}
	if byID["th2"].State != ThreadStateOpened {
		t.Fatalf("th2.State = %q, want opened", byID["th2"].State)
	}

	if len(comments) != 3 {
		t.Fatalf("comments = %d, want 3", len(comments))
	}
	// ranges attach to the thread's messages via the legacy POINTERS (op.t).
	cmM1 := comments[0]
	if cmM1.ThreadID != "th1" || cmM1.Text != "first note" || cmM1.Created != 1000 {
		t.Fatalf("m1 mapping: %+v", cmM1)
	}
	if len(cmM1.Ranges) != 1 || cmM1.Ranges[0]["start"] != 6 || cmM1.Ranges[0]["end"] != 6 {
		t.Fatalf("m1 ranges (zero-length point over 6): %#v", cmM1.Ranges)
	}
	if comments[1].Text != "reply" || comments[1].Edited != 1600 {
		t.Fatalf("m2 mapping: %+v", comments[1])
	}
	if byID["th2"].ID == "" && len(comments[2].Ranges) != 1 {
		t.Fatalf("m3 must carry the th2 pointer range")
	}

	if len(changes) != 2 {
		t.Fatalf("changes = %d, want 2 (the empty op drops)", len(changes))
	}
	if changes[0].Kind != ChangeKindInsert || changes[0].Start != 0 || changes[0].End != 3 ||
		changes[0].Content != "XYZ" || changes[0].State != ChangeStatePending {
		t.Fatalf("c1 mapping: %+v", changes[0])
	}
	if changes[1].Kind != ChangeKindDelete || changes[1].Start != 5 || changes[1].End != 8 ||
		changes[1].Content != "abc" || changes[1].State != ChangeStateAccepted {
		t.Fatalf("c2 mapping: %+v", changes[1])
	}
}

func TestMaterializeLegacy_StateDefaultsAndDanglingPointer(t *testing.T) {
	d := LegacyData{
		Changes: []LegacyChangeOp{{ID: "x", InsText: "q", Start: 1, State: "weird-legacy"}},
		Threads: []LegacyThread{{ID: "t", Messages: []LegacyMessage{{ID: "m", Content: "c", Timestamp: 1, UserID: "u"}}}},
	}
	_, _, ch := MaterializeLegacy(LegacyData{Changes: d.Changes}, "main.tex")
	if len(ch) != 1 || ch[0].State != ChangeStatePending {
		t.Fatalf("unknown legacy state must default pending: %+v", ch)
	}
	threads2, comments2, _ := MaterializeLegacy(d, "main.tex")
	if len(threads2) != 1 || len(comments2) != 1 {
		t.Fatalf("thread + message materialize: %d %d", len(threads2), len(comments2))
	}
	if comments2[0].Author == nil || comments2[0].Author["user_id"] != "u" {
		t.Fatalf("author passthrough: %#v", comments2[0].Author)
	}
}

// ---- source over fake services ----------------------------------------------

const p4DocstoreJSON = `{"_id":"aa","lines":["hello world this is a document"],"ranges":{
  "comments":[{"id":"pt1","op":{"t":"th1","p":6},"state":"open","metadata":{"user_id":"6userA","ts":1000}}],
  "changes":[{"id":"c1","op":{"i":"XYZ","p":0},"state":"pending","metadata":{"user_id":"6userB","ts":900}}]
}}`

const p4ChatJSON = `{"th1":{"messages":[{"id":"m1","content":"first note","timestamp":1000,"user_id":"6userA"}],"resolved":true,"resolved_at":"2000","resolved_by_user_id":"6userB"}}`

func fakeProjectLegacySource(t *testing.T, docstoreBody string) (*LegacySource, *httptest.Server) {
	t.Helper()
	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/project/"+p4Room+"/doc/"+p4RootDocID {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(docstoreBody))
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(ds.Close)
	seed := NewSeedSource(
		&seedFakeProjects{doc: seedProjectDoc(p4RootDocID)}, // 24-hex string shape
		ds.URL, "", "", &http.Client{Timeout: 2 * time.Second},
	)
	return NewLegacySource(seed), ds
}

func TestLegacySourceLoad_P4(t *testing.T) {
	src, _ := fakeProjectLegacySource(t, p4DocstoreJSON)
	ch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/project/"+p4Room+"/threads" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(p4ChatJSON))
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(ch.Close)
	src.ChatBase = ch.URL
	src.HTTP = ch.Client()

	data, err := src.Load(context.Background(), p4Room)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(data.Skipped) != 0 {
		t.Fatalf("Skipped = %v (want full corpus)", data.Skipped)
	}
	if len(data.Pointers) != 1 || data.Pointers[0].ThreadID != "th1" || data.Pointers[0].Start != 6 {
		t.Fatalf("pointers: %+v", data.Pointers)
	}
	if len(data.Changes) != 1 || data.Changes[0].InsText != "XYZ" {
		t.Fatalf("changes: %+v", data.Changes)
	}
	if len(data.Threads) != 1 || !data.Threads[0].Resolved || data.Threads[0].ResolvedAt != 2000 {
		t.Fatalf("threads: %+v", data.Threads)
	}
	if data.Threads[0].ResolvedBy != "6userB" {
		t.Fatalf("resolved_by passthrough = %q, want 6userB", data.Threads[0].ResolvedBy)
	}
	// content unwrap (JSON string → text)
	if data.Threads[0].Messages[0].Content != "first note" {
		t.Fatalf("content unwrap: %q", data.Threads[0].Messages[0].Content)
	}
}

func TestLegacySourceLoad_ChatDownSkipsThreadsOnly(t *testing.T) {
	src, _ := fakeProjectLegacySource(t, p4DocstoreJSON)
	src.ChatBase = "http://127.0.0.1:1" // unreachable → skipped, recorded
	data, err := src.Load(context.Background(), p4Room)
	if err != nil {
		t.Fatalf("Load must stay best-effort: %v", err)
	}
	if len(data.Threads) != 0 {
		t.Fatalf("threads must be absent on chat failure: %+v", data.Threads)
	}
	if len(data.Pointers) != 1 || len(data.Changes) != 1 {
		t.Fatalf("docstore side must survive: %+v", data)
	}
	found := false
	for _, s := range data.Skipped {
		if len(s) > 0 && s[0] == 't' { // "threads: ..."
			found = true
		}
	}
	if !found {
		t.Fatalf("skipped reasons must record the threads side: %v", data.Skipped)
	}
}

// ---- full seed → backfill chain (the real hook) -------------------------------

func TestBackfill_OnFirstSeed_P4(t *testing.T) {
	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/project/"+p4Room+"/doc/"+p4RootDocID {
			_, _ = w.Write([]byte(p4DocstoreJSON))
			return
		}
		w.WriteHeader(404)
	}))
	defer ds.Close()
	ch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/project/"+p4Room+"/threads" {
			_, _ = w.Write([]byte(p4ChatJSON))
			return
		}
		w.WriteHeader(404)
	}))
	defer ch.Close()

	seed := NewSeedSource(&seedFakeProjects{doc: seedProjectDoc(p4RootDocID)}, ds.URL, "", "", ds.Client())
	src := NewLegacySource(seed)
	src.ChatBase = ch.URL
	src.HTTP = ch.Client()

	svc, err := New(Options{
		Auth:    &fakeAuth{sessionUID: "ownerA", projRole: map[string]Role{p4Room: ReadWrite}},
		DataDir: t.TempDir(),
		SeedFn:  seed.SeedText,
		LegacyBackfill: func(ctx context.Context, room string, store persistence.VersionedPersistence) error {
			data, lerr := src.Load(ctx, room)
			if lerr != nil {
				return lerr
			}
			_ = Backfill(ctx, store, room, data, "main.tex")
			return nil // best-effort contract — stats are logged by the real wiring
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	doc := crdt.New()
	if err := svc.Server.OnLoadDocument(ctx, p4Room, doc); err != nil {
		t.Fatalf("hook: %v", err)
	}
	// The seeded text + the backfilled corpus must both be queryable:
	if got, _, hterr := HeadText(ctx, svc.Store(), p4Room); hterr != nil || got != "hello world this is a document" {
		t.Fatalf("head text = %q (seed)", got)
	}
	threads, err := ListThreads(ctx, svc.Store(), p4Room)
	if err != nil || len(threads) != 1 {
		t.Fatalf("threads after backfill = %d (%v), want 1", len(threads), err)
	}
	if threads[0].State != ThreadStateResolved {
		t.Fatalf("thread state = %q, want resolved", threads[0].State)
	}
	if threads[0].ResolvedBy == nil || threads[0].ResolvedBy["user_id"] != "6userB" {
		t.Fatalf("thread resolved_by = %#v (want 6userB)", threads[0].ResolvedBy)
	}
	comments, err := ListComments(ctx, svc.Store(), p4Room)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments after backfill = %d (%v), want 1", len(comments), err)
	}
	if len(comments[0].Ranges) != 1 {
		t.Fatalf("comment ranges (from the legacy pointer) = %#v", comments[0].Ranges)
	}
	changes, err := ListChanges(ctx, svc.Store(), p4Room)
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes after backfill = %d (%v), want 1", len(changes), err)
	}
	if changes[0].Kind != ChangeKindInsert || changes[0].Content != "XYZ" {
		t.Fatalf("change record = %+v", changes[0])
	}
}

// TestBackfill_RoomWithoutLegacy_NoOp — the common e2e path: a room with a
// fresh (legacy-free) project must seed EXACTLY one version and no records.
func TestBackfill_RoomWithoutLegacy_NoOp(t *testing.T) {
	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/project/"+p4Room+"/doc/"+p4RootDocID {
			_, _ = w.Write([]byte(`{"_id":"aa","lines":["blank"],"ranges":{"comments":[],"changes":[]}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer ds.Close()
	seed := NewSeedSource(&seedFakeProjects{doc: seedProjectDoc(p4RootDocID)}, ds.URL, "", "", ds.Client())
	src := NewLegacySource(seed)
	src.ChatBase = "http://127.0.0.1:1" // chat down (e2e has no TC service) — corpus stays empty

	svc, err := New(Options{
		Auth:    &fakeAuth{sessionUID: "ownerA", projRole: map[string]Role{p4Room: ReadWrite}},
		DataDir: t.TempDir(),
		SeedFn:  seed.SeedText,
		LegacyBackfill: func(ctx context.Context, room string, store persistence.VersionedPersistence) error {
			data, _ := src.Load(ctx, room)
			if len(data.Threads)+len(data.Pointers)+len(data.Changes) == 0 {
				return nil
			}
			Backfill(ctx, store, room, data, "main.tex")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := svc.Server.OnLoadDocument(ctx, p4Room, crdt.New()); err != nil {
		t.Fatalf("hook must stay green on an empty legacy corpus (best-effort): %v", err)
	}
	threads, _ := ListThreads(ctx, svc.Store(), p4Room)
	comments, _ := ListComments(ctx, svc.Store(), p4Room)
	changes, _ := ListChanges(ctx, svc.Store(), p4Room)
	if len(threads) != 0 || len(comments) != 0 || len(changes) != 0 {
		t.Fatalf("no-legacy room must have zero records: %d %d %d", len(threads), len(comments), len(changes))
	}
	if got, _, hterr := HeadText(ctx, svc.Store(), p4Room); hterr != nil || got != "blank" {
		t.Fatalf("seed text = %q", got)
	}
}
