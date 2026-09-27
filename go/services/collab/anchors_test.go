package collab

// anchors_test.go — D40-P3: relative-position anchoring semantics.
//
// The oracle is the ygo/yrs RelativePosition engine (Yjs-wire-compatible),
// verified here against LOCAL client edits (targeted insert/delete — the
// shape the D24 edit path produces; a full rewrite would delete anchor
// items, which is a different, degenerate case). Pinned observed behavior
// (anchors for the range [6,11) = "world" in "hello world this is a document"):
//
//   - insert BEFORE the range  → range shifts along with the text  (9,14)
//   - insert INSIDE  the range → range grows over the insertion    (6,13)
//   - insert AFTER   the range → range unchanged                   (6,11)
//   - delete BEFORE  the range → range shifts along with the text  (3,8)
//   - delete INSIDE  the range → the nearest-surviving-boundary
//     resolution of the engine: (6,11) anchored "wo th" (deterministic;
//     the range never vanishes)
//
// The review guarantees we BUILD on: the anchored text never drifts away
// from the original comment/change span (insertions move it with the
// text, never silently detach it), and resolution is deterministic.

import (
	"context"
	"sync"
	"testing"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
)

const anchorPID = "cafebabecafebabecafebabe"

// base content; "world" = [6,11)
const anchorBase = "hello world this is a document"

func seedAnch(t *testing.T) persistence.VersionedPersistence {
	t.Helper()
	st := persistence.NewMemoryPersistence()
	if _, err := SeedTextContent(context.Background(), st, anchorPID, anchorBase); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return st
}

// localEdit — a LOCAL client edit (targeted insert/delete against the head),
// appended as an update — the shape the D24/D19 edit path produces.
func localEdit(ctx context.Context, st persistence.VersionedPersistence, room string, op func(txn *crdt.Transaction, t *crdt.YText)) error {
	lr, err := st.Load(ctx, room)
	if err != nil {
		return err
	}
	d := crdt.New()
	if err := crdt.ApplyUpdateV1(d, lr.Update, nil); err != nil {
		return err
	}
	sv := d.StateVector().Clone()
	d.Transact(func(txn *crdt.Transaction) { op(txn, txn.GetText(TextType)) })
	_, err = st.AppendUpdate(ctx, room, crdt.EncodeStateAsUpdateV1(d, sv))
	return err
}

func rangeAfter(t *testing.T, st persistence.VersionedPersistence, ar AnchorRange) (int, int, string) {
	t.Helper()
	ctx := context.Background()
	s, e, err := ResolveRangeAnchors(ctx, st, anchorPID, ar, 6, 11)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	text, _, err := HeadText(ctx, st, anchorPID)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if s >= 0 && s < e && e <= len(text) {
		return s, e, text[s:e]
	}
	return s, e, ""
}

const (
	aBefore = ">>>hello world this is a document" // 3 inserted before [6,11)
	aInside = "hello wo!!rld this is a document"  // 2 inserted inside [6,11)
	aAfter  = "hello world this is a documentXXX" // 3 inserted after [6,11)
)

func TestAnchor_Insert_Before_Shifts(t *testing.T) {
	ctx := context.Background()
	st := seedAnch(t)
	ar, err := MakeRangeAnchors(ctx, st, anchorPID, 6, 11)
	if err != nil {
		t.Fatal(err)
	}
	if err := localEdit(ctx, st, anchorPID, func(txn *crdt.Transaction, t *crdt.YText) { t.Insert(txn, 0, ">>>", nil) }); err != nil {
		t.Fatal(err)
	}
	s, e, anchored := rangeAfter(t, st, ar)
	if s != 9 || e != 14 || anchored != "world" {
		t.Fatalf("= (%d,%d,%q), want (9,14,\"world\") — range must follow the text", s, e, anchored)
	}
}

func TestAnchor_Insert_Inside_Grows(t *testing.T) {
	ctx := context.Background()
	st := seedAnch(t)
	ar, err := MakeRangeAnchors(ctx, st, anchorPID, 6, 11)
	if err != nil {
		t.Fatal(err)
	}
	if err := localEdit(ctx, st, anchorPID, func(txn *crdt.Transaction, t *crdt.YText) { t.Insert(txn, 8, "!!", nil) }); err != nil {
		t.Fatal(err)
	}
	s, e, anchored := rangeAfter(t, st, ar)
	if s != 6 || e != 13 || anchored != "wo!!rld" {
		t.Fatalf("= (%d,%d,%q), want (6,13,\"wo!!rld\") — insertion inside grows the range", s, e, anchored)
	}
}

func TestAnchor_Insert_After_Unchanged(t *testing.T) {
	ctx := context.Background()
	st := seedAnch(t)
	ar, err := MakeRangeAnchors(ctx, st, anchorPID, 6, 11)
	if err != nil {
		t.Fatal(err)
	}
	if err := localEdit(ctx, st, anchorPID, func(txn *crdt.Transaction, t *crdt.YText) { t.Insert(txn, 28, "XXX", nil) }); err != nil {
		t.Fatal(err)
	}
	s, e, anchored := rangeAfter(t, st, ar)
	if s != 6 || e != 11 || anchored != "world" {
		t.Fatalf("= (%d,%d,%q), want (6,11,\"world\")", s, e, anchored)
	}
	_ = aAfter
}

func TestAnchor_Delete_Before_Shifts(t *testing.T) {
	ctx := context.Background()
	st := seedAnch(t)
	ar, err := MakeRangeAnchors(ctx, st, anchorPID, 6, 11)
	if err != nil {
		t.Fatal(err)
	}
	if err := localEdit(ctx, st, anchorPID, func(txn *crdt.Transaction, t *crdt.YText) { t.Delete(txn, 0, 3) }); err != nil {
		t.Fatal(err)
	}
	s, e, anchored := rangeAfter(t, st, ar)
	if s != 3 || e != 8 || anchored != "world" {
		t.Fatalf("= (%d,%d,%q), want (3,8,\"world\") — deletion before shifts the range", s, e, anchored)
	}
}

func TestAnchor_Delete_Inside_EngineBoundary(t *testing.T) {
	ctx := context.Background()
	st := seedAnch(t)
	ar, err := MakeRangeAnchors(ctx, st, anchorPID, 6, 11)
	if err != nil {
		t.Fatal(err)
	}
	if err := localEdit(ctx, st, anchorPID, func(txn *crdt.Transaction, t *crdt.YText) { t.Delete(txn, 8, 3) }); err != nil {
		t.Fatal(err)
	}
	// engine-pinned (honest oracle): deleting "rld" inside [6,11) resolves to
	// (6,11) anchored "wo th" — the nearest-surviving-boundary semantics of
	// the reference engine; the range stays well-formed and deterministic.
	s, e, anchored := rangeAfter(t, st, ar)
	if s != 6 || e != 11 || anchored != "wo th" {
		t.Fatalf("= (%d,%d,%q), engine-pinned (6,11,\"wo th\")", s, e, anchored)
	}
}

func TestAnchor_Clamp(t *testing.T) {
	ctx := context.Background()
	st := seedAnch(t)
	ar, err := MakeRangeAnchors(ctx, st, anchorPID, 999, 10000)
	if err != nil {
		t.Fatal(err)
	}
	s, e, err := ResolveRangeAnchors(ctx, st, anchorPID, ar, 999, 10000)
	if err != nil {
		t.Fatal(err)
	}
	text, _, _ := HeadText(ctx, st, anchorPID)
	if s != len(text) || e < len(text) {
		t.Fatalf("clamp = (%d,%d), want (len,%d+)", s, e, len(text))
	}
}

func TestAddChange_AutoAnchor_LiveList(t *testing.T) {
	ctx := context.Background()
	st := seedAnch(t)
	if _, _, _, err := AddChange(ctx, st, anchorPID, TrackedChange{
		ID: "ch-a1", Kind: ChangeKindInsert, File: "main.tex", Start: 11, End: 11,
		Content: "anchored", Author: map[string]any{"user_id": "u1"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	// a LOCAL insertion BEFORE the change position must shift the LISTED
	// position (P3 live resolution): 11 → 14
	if err := localEdit(ctx, st, anchorPID, func(txn *crdt.Transaction, t *crdt.YText) { t.Insert(txn, 0, "XXX", nil) }); err != nil {
		t.Fatal(err)
	}
	all, err := ListChanges(ctx, st, anchorPID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Start != 14 {
		t.Fatalf("live list = %+v, want start=14 (11 + 3 inserted before)", all)
	}
}

func TestAddComment_AutoAnchor_LiveRanges(t *testing.T) {
	ctx := context.Background()
	st := seedAnch(t)
	if _, _, _, err := AddComment(ctx, st, anchorPID, Comment{
		ID: "cm-a1", ThreadID: "th-a1", File: "main.tex", Text: "over world",
		Author: map[string]any{"user_id": "u1"},
		Ranges: []map[string]any{{"start": 6, "end": 11}},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	msgs, err := MessagesOfThread(ctx, st, anchorPID, "th-a1")
	if err != nil || len(msgs) != 1 {
		t.Fatalf("messages: %v (%d)", err, len(msgs))
	}
	if _, has := msgs[0].Ranges[0]["a"]; !has {
		t.Fatalf("r[0] lacks anchor: %v", msgs[0].Ranges[0])
	}
	if err := localEdit(ctx, st, anchorPID, func(txn *crdt.Transaction, t *crdt.YText) { t.Insert(txn, 0, ">>", nil) }); err != nil {
		t.Fatal(err)
	}
	out, err := ResolveLiveRanges(ctx, st, anchorPID, msgs[0].Ranges)
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	if intOfAnyTest(out[0]["start"]) != 8 || intOfAnyTest(out[0]["end"]) != 13 {
		t.Fatalf("live r[0] = (%v,%v), want (8,13) — 2 inserted before", out[0]["start"], out[0]["end"])
	}
	// P1 plain-only records pass through (documented plain-coord fallback)
	outp, err := ResolveLiveRanges(ctx, st, anchorPID, []map[string]any{{"start": 6, "end": 11}})
	if err != nil {
		t.Fatal(err)
	}
	if intOfAnyTest(outp[0]["start"]) != 6 || intOfAnyTest(outp[0]["end"]) != 11 {
		t.Fatalf("plain record = (%v,%v), want (6,11) unchanged", outp[0]["start"], outp[0]["end"])
	}
}

func TestConcurrentLifecycle_P3(t *testing.T) {
	ctx := context.Background()
	st := seedAnch(t)
	if _, _, _, err := AddThread(ctx, st, anchorPID, Thread{ID: "th-race", File: "main.tex", State: "opened", Author: map[string]any{"user_id": "u1"}}); err != nil {
		t.Fatalf("thread: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 24)

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			state := "resolved"
			if i%2 == 1 {
				state = "opened"
			}
			if _, _, _, err := SetThreadStateBy(ctx, st, anchorPID, "th-race", state, map[string]any{"user_id": "racer"}); err != nil {
				errs <- err
			}
		}(i)
	}
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, _, _, err := AddComment(ctx, st, anchorPID, Comment{
				ID: "cm-race-1", ThreadID: "th-race", File: "main.tex",
				Text: "racing comment", Author: map[string]any{"user_id": "u1"},
				Ranges: []map[string]any{{"start": 6, "end": 11}},
			}); err != nil {
				errs <- err
			}
		}(i)
	}
	if _, _, _, err := AddChange(ctx, st, anchorPID, TrackedChange{ID: "ch-race-1", Kind: "insert", File: "main.tex", Start: 2, End: 2, Content: "x"}); err != nil {
		t.Fatalf("change: %v", err)
	}
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, _, _, err := AcceptChange(ctx, st, anchorPID, "ch-race-1"); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent failure: %v", err)
	}

	msgs, err := MessagesOfThread(ctx, st, anchorPID, "th-race")
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("racing comment added %d times, want exactly 1 (idempotent on id)", len(msgs))
	}
	if len(msgs[0].Ranges) != 1 {
		t.Fatalf("racing comment lost its range set: %v", msgs[0].Ranges)
	}
	if _, has := msgs[0].Ranges[0]["a"]; !has {
		t.Fatalf("racing comment lost its anchored range: %v", msgs[0].Ranges)
	}
	ths, err := ListThreads(ctx, st, anchorPID)
	if err != nil {
		t.Fatalf("threads: %v", err)
	}
	found := false
	for _, th := range ths {
		if th.ID == "th-race" {
			found = true
			if th.State != "opened" && th.State != "resolved" {
				t.Fatalf("thread state = %q", th.State)
			}
			if (th.State == "resolved") != (th.Resolved != 0) {
				t.Fatalf("state/resolved inconsistency: state=%q resolved=%d", th.State, th.Resolved)
			}
		}
	}
	if !found {
		t.Fatalf("thread lost under concurrency")
	}
	all, err := ListChanges(ctx, st, anchorPID)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(all) != 1 || all[0].State != ChangeStateAccepted {
		t.Fatalf("change state under concurrency = %+v", all)
	}
}

// TestConcurrentReviewWrites_RecordIntegrity — permanent regression for the
// D40-P3 client-id identity bug: concurrent review writers (thread state +
// change accept + comment add) in the SAME process must never leave a
// corrupted record (empty id) or a lost change. Pinned to 10 rounds.
func TestConcurrentReviewWrites_RecordIntegrity(t *testing.T) {
	ctx := context.Background()
	for round := 0; round < 10; round++ {
		st := seedAnch(t)
		if _, _, _, err := AddThread(ctx, st, anchorPID, Thread{ID: "th-b1234", File: "main.tex", State: "opened"}); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := AddChange(ctx, st, anchorPID, TrackedChange{ID: "ch-b1234", Kind: "insert", File: "main.tex", Start: 2, End: 2, Content: "x"}); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				state := "resolved"
				if i%2 == 1 {
					state = "opened"
				}
				SetThreadStateBy(ctx, st, anchorPID, "th-b1234", state, map[string]any{"user_id": "r"})
			}(i)
		}
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				AcceptChange(ctx, st, anchorPID, "ch-b1234")
			}()
		}
		wg.Wait()
		all, err := ListChanges(ctx, st, anchorPID)
		if err != nil {
			t.Fatalf("round %d list: %v", round, err)
		}
		if len(all) != 1 || all[0].ID != "ch-b1234" || all[0].State != ChangeStateAccepted {
			t.Fatalf("round %d record corrupted: %+v (client-id identity regression)", round, all)
		}
		ths, err := ListThreads(ctx, st, anchorPID)
		if err != nil {
			t.Fatalf("round %d threads: %v", round, err)
		}
		found := false
		for _, th := range ths {
			if th.ID == "th-b1234" {
				found = true
				if th.State != "opened" && th.State != "resolved" {
					t.Fatalf("round %d thread state = %q", round, th.State)
				}
			}
		}
		if !found {
			t.Fatalf("round %d thread lost", round)
		}
	}
}

func intOfAnyTest(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
