package collab

// review_test.go — D40 P1 foundations: hermetic tests for the room-doc
// comment + tracked-change domain ops (go/services/collab/review.go).
//
// Oracle: the D40 decision record (WEB_GO_STATE.md) — server-authoritative,
// idempotent-on-id lifecycle, CRDT order-independence, and the exact
// RejectChange mutation semantics. No Node oracle exists for Y.Doc-native
// reviews (the OT contract dies with the migration, D41) — the contract the
// tests pin is D40 itself.

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
)

func ctx0() context.Context { return context.Background() }

func newMemStore(t *testing.T) persistence.VersionedPersistence {
	t.Helper()
	return persistence.NewMemoryPersistence()
}

// seedRoom — the room holds a version-1 content doc (first Y-join state).
func seedRoom(t *testing.T, store persistence.VersionedPersistence, room, text string) {
	t.Helper()
	v, err := SeedTextContent(ctx0(), store, room, text)
	if err != nil || v != 1 {
		t.Fatalf("seed: v=%d err=%v (want v1)", v, err)
	}
}

var idHex24 = regexp.MustCompile(`^[0-9a-f]{24}$`)

func TestNewReviewID(t *testing.T) {
	base := time.Unix(1700000000, 0)
	a := NewReviewID(base)
	b := NewReviewID(base.Add(time.Nanosecond))
	if !idHex24.MatchString(a) || !idHex24.MatchString(b) {
		t.Fatalf("ids must be hex24: %q %q", a, b)
	}
	if a == b {
		t.Fatalf("ids must differ: %q", a)
	}
}

func TestCommentLifecycle(t *testing.T) {
	ctx := ctx0()
	store := newMemStore(t)
	seedRoom(t, store, "room", "hello world")

	const id = "aaaaaaaaaaaaaaaaaaaaaaaa"
	c := Comment{
		ID:      id,
		File:    "main.tex",
		Text:    "check this",
		State:   "opened",
		Author:  map[string]any{"user_id": "u1", "email": "u1@e.test"},
		Ranges:  []map[string]any{{"start": 0, "end": 3}},
		Created: 111,
		Edited:  111,
	}
	got, applied, v, err := AddComment(ctx, store, "room", c)
	if err != nil || !applied || v != 2 {
		t.Fatalf("add: applied=%v v=%d err=%v", applied, v, err)
	}
	if got.ID != id {
		t.Fatalf("add returned id %q", got.ID)
	}

	// Idempotent re-add: no mutation, no new version, equal record.
	dup, applied2, v2, err := AddComment(ctx, store, "room", c)
	if err != nil || applied2 || v2 != 2 {
		t.Fatalf("dup add: applied=%v v=%d err=%v (want false@v2)", applied2, v2, err)
	}
	if dup.Text != "check this" || dup.Author["email"] != "u1@e.test" {
		t.Fatalf("dup record mismatch: %+v", dup)
	}

	// Round-trip shape through persistence.
	list, err := ListComments(ctx, store, "room")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: n=%d err=%v", len(list), err)
	}
	g := list[0]
	if g.ID != id || g.File != "main.tex" || g.State != "opened" ||
		g.Author["email"] != "u1@e.test" || g.Created != 111 {
		t.Fatalf("round-trip mismatch: %+v", g)
	}
	if len(g.Ranges) != 1 || g.Ranges[0]["start"] != float64(0) || g.Ranges[0]["end"] != float64(3) {
		t.Fatalf("ranges round-trip mismatch: %+v", g.Ranges)
	}

	// Edit text (applied once, idempotent on equal text).
	if _, applied, _, err := EditCommentText(ctx, store, "room", id, "check this!"); err != nil || !applied {
		t.Fatalf("edit: applied=%v err=%v", applied, err)
	}
	before, _ := store.Load(ctx, "room")
	if _, applied, _, _ := EditCommentText(ctx, store, "room", id, "check this!"); applied {
		t.Fatalf("second edit must be a no-op")
	}
	after, _ := store.Load(ctx, "room")
	if after.Version != before.Version {
		t.Fatalf("no-op edit changed version: %d → %d", before.Version, after.Version)
	}

	// Reply (panel reply map stored verbatim).
	if _, applied, _, err := AddCommentReply(ctx, store, "room", id, map[string]any{"user_id": "u2", "text": "ok"}); err != nil || !applied {
		t.Fatalf("reply: applied=%v err=%v", applied, err)
	}
	list, _ = ListComments(ctx, store, "room")
	if len(list[0].Replies) != 1 || list[0].Replies[0]["user_id"] != "u2" {
		t.Fatalf("replies mismatch: %+v", list[0].Replies)
	}

	// State transition (idempotent on target state).
	if _, applied, _, err := SetCommentState(ctx, store, "room", id, "resolved"); err != nil || !applied {
		t.Fatalf("resolve: applied=%v err=%v", applied, err)
	}
	if _, applied, _, _ := SetCommentState(ctx, store, "room", id, "resolved"); applied {
		t.Fatalf("second resolve must be a no-op")
	}

	// Unknown ids.
	if _, _, _, err := SetCommentState(ctx, store, "room", "ffffffffffffffffffffffff", "resolved"); err != ErrCommentNotFound {
		t.Fatalf("want ErrCommentNotFound, got %v", err)
	}

	// Delete (idempotent), then empty list.
	if applied, _, err := DeleteComment(ctx, store, "room", id); err != nil || !applied {
		t.Fatalf("delete: applied=%v err=%v", applied, err)
	}
	if applied, _, _ := DeleteComment(ctx, store, "room", id); applied {
		t.Fatalf("second delete must be a no-op")
	}
	if list, _ := ListComments(ctx, store, "room"); len(list) != 0 {
		t.Fatalf("list after delete: %+v", list)
	}
}

func TestCommentAddGeneratedID(t *testing.T) {
	ctx := ctx0()
	store := newMemStore(t)
	seedRoom(t, store, "room", "hello")
	got, applied, _, err := AddComment(ctx, store, "room", Comment{File: "main.tex", Text: "t", State: "opened"})
	if err != nil || !applied {
		t.Fatalf("add: applied=%v err=%v", applied, err)
	}
	if !idHex24.MatchString(got.ID) {
		t.Fatalf("generated id not hex24: %q", got.ID)
	}
}

func TestChangeInsertReject(t *testing.T) {
	ctx := ctx0()
	store := newMemStore(t)
	// Doc AFTER the user's pending insert "HELLO" at the start.
	seedRoom(t, store, "room", "HELLOhello")

	const id = "bbbbbbbbbbbbbbbbbbbbbbbb"
	ch := TrackedChange{ID: id, Kind: ChangeKindInsert, File: "main.tex", Start: 0, End: 5, Content: "HELLO", Created: 1}
	if _, applied, _, err := AddChange(ctx, store, "room", ch); err != nil || !applied {
		t.Fatalf("add change: applied=%v err=%v", applied, err)
	}

	// Reject → the inserted text is removed (server-side, exactly once).
	if _, applied, _, err := RejectChange(ctx, store, "room", id); err != nil || !applied {
		t.Fatalf("reject: applied=%v err=%v", applied, err)
	}
	text, _, err := HeadText(ctx, store, "room")
	if err != nil || text != "hello" {
		t.Fatalf("text after reject = %q (want %q)", text, "hello")
	}

	// Racing reject: NO second mutation.
	if _, applied, _, _ := RejectChange(ctx, store, "room", id); applied {
		t.Fatalf("second reject must be a no-op")
	}
	if text, _, _ := HeadText(ctx, store, "room"); text != "hello" {
		t.Fatalf("double-reject mutated text: %q", text)
	}

	// Recorded state.
	list, _ := ListChanges(ctx, store, "room")
	if len(list) != 1 || list[0].State != ChangeStateRejected {
		t.Fatalf("change list: %+v", list)
	}
}

func TestChangeDeleteReject(t *testing.T) {
	ctx := ctx0()
	store := newMemStore(t)
	// Doc AFTER the user's pending delete of "hello" [0,5).
	seedRoom(t, store, "room", " world")

	const id = "cccccccccccccccccccccccc"
	ch := TrackedChange{ID: id, Kind: ChangeKindDelete, File: "main.tex", Start: 0, End: 5, Content: "hello", Created: 1}
	if _, applied, _, err := AddChange(ctx, store, "room", ch); err != nil || !applied {
		t.Fatalf("add change: applied=%v err=%v", applied, err)
	}
	if _, applied, _, err := RejectChange(ctx, store, "room", id); err != nil || !applied {
		t.Fatalf("reject: applied=%v err=%v", applied, err)
	}
	text, _, _ := HeadText(ctx, store, "room")
	if text != "hello world" {
		t.Fatalf("text after delete-reject = %q (want %q)", text, "hello world")
	}
}

func TestChangeAccept(t *testing.T) {
	ctx := ctx0()
	store := newMemStore(t)
	seedRoom(t, store, "room", "keep")

	const id = "dddddddddddddddddddddddd"
	ch := TrackedChange{ID: id, Kind: ChangeKindInsert, Start: 4, End: 8, Content: " keep"}
	if _, applied, _, err := AddChange(ctx, store, "room", ch); err != nil || !applied {
		t.Fatalf("add: %v", err)
	}
	got, applied, _, err := AcceptChange(ctx, store, "room", id)
	if err != nil || !applied || got.State != ChangeStateAccepted {
		t.Fatalf("accept: state=%q applied=%v err=%v", got.State, applied, err)
	}
	// Accept stands: NO content mutation; idempotent.
	if text, _, _ := HeadText(ctx, store, "room"); text != "keep" {
		t.Fatalf("accept mutated text: %q", text)
	}
	if _, applied, _, _ := AcceptChange(ctx, store, "room", id); applied {
		t.Fatalf("second accept must be a no-op")
	}
}

func TestChangeRejectDriftStaysPending(t *testing.T) {
	ctx := ctx0()
	store := newMemStore(t)
	seedRoom(t, store, "room", "abc")

	ch := TrackedChange{ID: "eeeeeeeeeeeeeeeeeeeeeeee", Kind: ChangeKindInsert, Start: 0, End: 10, Content: "xxxxxxxxxx"}
	if _, applied, _, err := AddChange(ctx, store, "room", ch); err != nil || !applied {
		t.Fatalf("add: %v", err)
	}
	if _, applied, _, err := RejectChange(ctx, store, "room", "eeeeeeeeeeeeeeeeeeeeeeee"); err != ErrChangeRange || applied {
		t.Fatalf("want ErrChangeRange (applied=false), got applied=%v err=%v", applied, err)
	}
	// Text untouched, state still pending (caller re-resolves).
	if text, _, _ := HeadText(ctx, store, "room"); text != "abc" {
		t.Fatalf("drift reject mutated text: %q", text)
	}
	list, _ := ListChanges(ctx, store, "room")
	if len(list) != 1 || list[0].State != ChangeStatePending {
		t.Fatalf("drift: state = %+v (want pending)", list)
	}
}

func TestChangeKindRejected(t *testing.T) {
	ctx := ctx0()
	store := newMemStore(t)
	seedRoom(t, store, "room", "x")
	if _, _, _, err := AddChange(ctx, store, "room", TrackedChange{ID: "111111111111111111111111", Kind: "move"}); err != ErrChangeKind {
		t.Fatalf("want ErrChangeKind, got %v", err)
	}
}

// TestTwoClientConvergence — the D40 core guarantee: a comment op (client A)
// and a text edit (client B, via the real roomdoc seam) converge to the SAME
// final doc regardless of delivery order.
func TestTwoClientConvergence(t *testing.T) {
	ctx := ctx0()

	deltaComment := captureDeltaComment(t, ctx, "hello", Comment{
		ID: "f1f1f1f1f1f1f1f1f1f1f1f1", File: "main.tex", Text: "note", State: "opened",
	})
	deltaEdit := captureDeltaEdit(t, ctx, "hello", "hello big")

	var orderAB, orderBA persistence.VersionedPersistence
	orderAB = applyBoth(t, ctx, "hello", deltaComment, deltaEdit)
	orderBA = applyBoth(t, ctx, "hello", deltaEdit, deltaComment)

	for name, s := range map[string]persistence.VersionedPersistence{"AB": orderAB, "BA": orderBA} {
		text, _, err := HeadText(ctx, s, "room")
		if err != nil || text != "hello big" {
			t.Fatalf("%s: text=%q err=%v (want %q)", name, text, err, "hello big")
		}
		list, err := ListComments(ctx, s, "room")
		if err != nil || len(list) != 1 || list[0].ID != "f1f1f1f1f1f1f1f1f1f1f1f1" || list[0].Text != "note" {
			t.Fatalf("%s: comments=%+v err=%v", name, list, err)
		}
	}
}

func captureDeltaComment(t *testing.T, ctx context.Context, text string, c Comment) []byte {
	t.Helper()
	s := newMemStore(t)
	seedRoom(t, s, "room", text)
	if _, applied, _, err := AddComment(ctx, s, "room", c); err != nil || !applied {
		t.Fatalf("add: %v", err)
	}
	upd, _, ok, err := s.GetUpdate(ctx, "room", 2)
	if err != nil || !ok {
		t.Fatalf("getupdate: ok=%v err=%v", ok, err)
	}
	return upd
}

func captureDeltaEdit(t *testing.T, ctx context.Context, from, to string) []byte {
	t.Helper()
	s := newMemStore(t)
	seedRoom(t, s, "room", from)
	if _, err := ClientEdit(ctx, s, "room", from, to); err != nil {
		t.Fatalf("clientedit: %v", err)
	}
	upd, _, ok, err := s.GetUpdate(ctx, "room", 2)
	if err != nil || !ok {
		t.Fatalf("getupdate: ok=%v err=%v", ok, err)
	}
	return upd
}

func applyBoth(t *testing.T, ctx context.Context, text string, first, second []byte) persistence.VersionedPersistence {
	t.Helper()
	s := newMemStore(t)
	seedRoom(t, s, "room", text)
	if _, err := s.AppendUpdate(ctx, "room", first); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if _, err := s.AppendUpdate(ctx, "room", second); err != nil {
		t.Fatalf("append second: %v", err)
	}
	return s
}

// TestHistoryIncludesReviews — D41 interlock: version v1 (pre-comment) has
// no comments; the head version has the comment (history snapshots carry
// review state for free).
func TestHistoryIncludesReviews(t *testing.T) {
	ctx := ctx0()
	s := newMemStore(t)
	seedRoom(t, s, "room", "hello")
	if _, applied, _, _ := AddComment(ctx, s, "room", Comment{ID: "444444444444444444444444", Text: "n", State: "opened", File: "main.tex"}); !applied {
		t.Fatalf("add comment")
	}
	countAt := func(v persistence.Version) int {
		snap, err := s.MaterializeAt(ctx, "room", v)
		if err != nil {
			t.Fatalf("materialize @%d: %v", v, err)
		}
		d := crdt.New()
		if err := crdt.ApplyUpdateV1(d, snap, nil); err != nil {
			t.Fatalf("apply @%d: %v", v, err)
		}
		return d.GetArray(CommentsType).Len()
	}
	if n := countAt(1); n != 0 {
		t.Fatalf("v1 comments = %d (want 0)", n)
	}
	if n := countAt(2); n != 1 {
		t.Fatalf("head comments = %d (want 1)", n)
	}
}

func TestThreadLifecycle(t *testing.T) {
	ctx := ctx0()
	store := newMemStore(t)
	seedRoom(t, store, "room", "hello world")

	th, applied, _, err := AddThread(ctx, store, "room", Thread{
		ID: "555555555555555555555555", File: "main.tex",
		Author: map[string]any{"user_id": "u1"}, Created: 10,
	})
	if err != nil || !applied || th.State != ThreadStateOpened {
		t.Fatalf("add thread: applied=%v state=%q err=%v", applied, th.State, err)
	}

	// Idempotent re-add.
	if _, applied, _, _ := AddThread(ctx, store, "room", Thread{ID: "555555555555555555555555"}); applied {
		t.Fatalf("thread re-add must be a no-op")
	}

	// Messages belong to the thread.
	msg := Comment{ID: "666666666666666666666666", ThreadID: "555555555555555555555555",
		File: "main.tex", Text: "why here?", State: "opened", Ranges: []map[string]any{{"start": 1, "end": 5}}}
	if _, applied, _, err := AddComment(ctx, store, "room", msg); err != nil || !applied {
		t.Fatalf("add message: %v", err)
	}
	if _, applied, _, err := AddCommentReply(ctx, store, "room", msg.ID, map[string]any{"text": "a reply"}); err != nil || !applied {
		t.Fatalf("reply: %v", err)
	}

	standalone := Comment{ID: "676767676767676767676767", File: "main.tex", Text: "loose"}
	if _, applied, _, _ := AddComment(ctx, store, "room", standalone); !applied {
		t.Fatalf("standalone message")
	}

	all, _ := ListComments(ctx, store, "room")
	if len(all) != 2 {
		t.Fatalf("all comments: %d", len(all))
	}
	msgs, _ := MessagesOfThread(ctx, store, "room", "555555555555555555555555")
	if len(msgs) != 1 || msgs[0].ID != msg.ID {
		t.Fatalf("thread messages: %+v", msgs)
	}

	// Resolve → reopen transitions + state validation.
	if _, applied, _, _ := SetThreadState(ctx, store, "room", "555555555555555555555555", ThreadStateResolved); !applied {
		t.Fatalf("resolve")
	}
	if _, applied, _, _ := SetThreadState(ctx, store, "room", "555555555555555555555555", ThreadStateResolved); applied {
		t.Fatalf("double resolve must be a no-op")
	}
	got, _, _, _ := SetThreadState(ctx, store, "room", "555555555555555555555555", ThreadStateOpened)
	if got.Resolved != 0 {
		t.Fatalf("reopen must clear resolved ts: %d", got.Resolved)
	}
	if _, _, _, err := SetThreadState(ctx, store, "room", "555555555555555555555555", "bogus"); err == nil {
		t.Fatalf("bogus state must be rejected")
	}
	if _, _, _, err := SetThreadState(ctx, store, "room", "999999999999999999999999", ThreadStateResolved); err != ErrThreadNotFound {
		t.Fatalf("want ErrThreadNotFound, got %v", err)
	}

	// Thread deletion cascades to its messages, keeps standalone comments.
	if applied, _, err := DeleteThread(ctx, store, "room", "555555555555555555555555"); err != nil || !applied {
		t.Fatalf("delete thread: %v", err)
	}
	if applied, _, _ := DeleteThread(ctx, store, "room", "555555555555555555555555"); applied {
		t.Fatalf("double delete must be a no-op")
	}
	rest, _ := ListComments(ctx, store, "room")
	if len(rest) != 1 || rest[0].ID != standalone.ID {
		t.Fatalf("cascade left: %+v", rest)
	}
	if threads, _ := ListThreads(ctx, store, "room"); len(threads) != 0 {
		t.Fatalf("threads left: %+v", threads)
	}
}

func TestThreadAndConcurrentTextOrderIndependence(t *testing.T) {
	ctx := ctx0()
	deltaThread := func() []byte {
		s := newMemStore(t)
		seedRoom(t, s, "room", "hello")
		if _, applied, _, _ := AddThread(ctx, s, "room", Thread{ID: "777777777777777777777777", File: "main.tex"}); !applied {
			t.Fatalf("thread add")
		}
		upd, _, ok, err := s.GetUpdate(ctx, "room", 2)
		if err != nil || !ok {
			t.Fatalf("getupdate: %v", err)
		}
		return upd
	}
	edit := captureDeltaEdit(t, ctx, "hello", "hello two")

	for _, order := range [][2][]byte{{deltaThread(), edit}, {edit, deltaThread()}} {
		s := newMemStore(t)
		seedRoom(t, s, "room", "hello")
		if _, err := s.AppendUpdate(ctx, "room", order[0]); err != nil {
			t.Fatalf("append0: %v", err)
		}
		if _, err := s.AppendUpdate(ctx, "room", order[1]); err != nil {
			t.Fatalf("append1: %v", err)
		}
		if text, _, _ := HeadText(ctx, s, "room"); text != "hello two" {
			t.Fatalf("text %q", text)
		}
		threads, _ := ListThreads(ctx, s, "room")
		if len(threads) != 1 || threads[0].ID != "777777777777777777777777" {
			t.Fatalf("threads: %+v", threads)
		}
	}
}
