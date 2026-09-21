package rangestracker

import (
	"regexp"
	"testing"
	"time"
)

// supplement_test.go — coverage beyond the Node oracle suite: validate,
// comment APIs, dirty state, id generation, error surfaces, and the
// comment-bookkeeping branches the mocha spec doesn't exercise.

func TestValidateInsertAndComment(t *testing.T) {
	t.Run("insert ok + wrong text + comment ok + wrong text", func(t *testing.T) {
		rt := New(
			[]Change{{ID: "c1", Op: Op{I: ptrTo("world"), P: 6}}},
			[]CommentItem{{ID: "t1", Op: Op{C: ptrTo("wor"), P: 6}, Metadata: Metadata{}}},
		)
		if err := rt.Validate("hello world"); err != nil {
			t.Fatalf("expected ok, got %v", err)
		}
		rt2 := New(
			[]Change{{ID: "c1", Op: Op{I: ptrTo("world"), P: 1}}},
			nil,
		)
		if err := rt2.Validate("hello world"); err == nil || err.Error() != "insertion does not match text in document" {
			t.Fatalf("got %v", err)
		}
		rt3 := New(nil, []CommentItem{{ID: "t1", Op: Op{C: ptrTo("xx"), P: 6}, Metadata: Metadata{}}})
		if err := rt3.Validate("hello world"); err == nil || err.Error() != "comment does not match text in document" {
			t.Fatalf("got %v", err)
		}
	})
}

func TestCommentApis(t *testing.T) {
	rt := New(nil, []CommentItem{
		{ID: "a", Op: Op{C: ptrTo("one"), P: 1}, Metadata: Metadata{}},
		{ID: "b", Op: Op{C: ptrTo("two"), P: 4}, Metadata: Metadata{}},
	})

	if got := rt.GetComment("b"); got == nil || got.Op.P != 4 {
		t.Fatalf("GetComment(b) = %v", got)
	}
	if got := rt.GetComment("nope"); got != nil {
		t.Fatalf("GetComment(nope) = %v, want nil", got)
	}

	rt.MoveCommentId("b", 9, "TWO")
	c := rt.GetComment("b")
	if c.Op.P != 9 || *c.Op.C != "TWO" {
		t.Fatalf("after move = %v", c.Op)
	}
	if !dirtyHas(rt, "comment", "moved", "b") {
		t.Fatalf("dirty missing comment/b moved: %v", rt.GetDirtyState())
	}

	rt.RemoveCommentId("a")
	if rt.GetComment("a") != nil {
		t.Fatalf("RemoveCommentId(a) left the comment")
	}
	if !dirtyHas(rt, "comment", "removed", "a") {
		t.Fatalf("dirty missing comment/a removed")
	}
	rt.RemoveCommentId("ghost") // no-op
	if len(rt.Comments) != 1 {
		t.Fatalf("ghost removal changed comments: %d", len(rt.Comments))
	}
}

func dirtyHas(rt *RangesTracker, class, action, id string) bool {
	ds := rt.GetDirtyState()
	var bucket *Dirty
	if class == "comment" {
		bucket = &ds.Comment
	} else {
		bucket = &ds.Change
	}
	switch action {
	case "moved":
		_, ok := bucket.Moved[id]
		return ok
	case "removed":
		_, ok := bucket.Removed[id]
		return ok
	case "added":
		_, ok := bucket.Added[id]
		return ok
	}
	return false
}

func TestAddCommentNewAndMove(t *testing.T) {
	rt := New(nil, nil)
	rt.SetIdSeed("aaaaaaaaaaaaaaaaaaaa")

	// New comment: id from op.t
	op := CommentOp("hello", 3, "thread-1")
	rt.AddComment(op, Metadata{"user_id": "u"})
	if len(rt.Comments) != 1 || rt.Comments[0].ID != "thread-1" {
		t.Fatalf("added = %v", rt.Comments)
	}
	if !dirtyHas(rt, "comment", "added", "thread-1") {
		t.Fatalf("dirty missing added thread-1")
	}

	// Same op again → move, not add
	rt.AddComment(CommentOp("HELLO", 7, "thread-1"), Metadata{"user_id": "u"})
	if len(rt.Comments) != 1 || rt.Comments[0].Op.P != 7 || *rt.Comments[0].Op.C != "HELLO" {
		t.Fatalf("after re-add = %v", rt.Comments)
	}

	// op without thread id → fresh NewId()
	rt.AddComment(CommentOp("x", 0, ""), Metadata{})
	last := rt.Comments[len(rt.Comments)-1]
	if last.ID != "aaaaaaaaaaaaaaaaaaaa000002" {
		t.Fatalf("fresh id = %q, want aaaaaaaaaaaaaaaaaaaa000002", last.ID)
	}
}

func TestIdGeneration(t *testing.T) {
	seed := GenerateIdSeed()
	if !regexp.MustCompile(`^[0-9a-f]{18}$`).MatchString(seed) {
		t.Fatalf("seed %q not 18 lowercase hex chars", seed)
	}
	id := GenerateId()
	if len(id) != 24 || id[18:] != "000001" {
		t.Fatalf("GenerateId() = %q", id)
	}

	rt := New(nil, nil)
	rt.SetIdSeed("abcdefabcdefabcdef")
	if rt.GetIdSeed() != "abcdefabcdefabcdef" {
		t.Fatalf("seed = %q", rt.GetIdSeed())
	}
	first := rt.NewId()
	if first != "abcdefabcdefabcdef000001" {
		t.Fatalf("first id = %q", first)
	}
	if rt.NewId() != "abcdefabcdefabcdef000002" {
		t.Fatalf("second id wrong")
	}
	// increment > 0xffffff: Node '000000'.substr(0, 6 - len) yields '' — the
	// increment itself is NOT truncated (7 hex chars survive)
	rt.SetIdSeed("s")
	rt.idIncrement = 0xFFFFFF
	last := rt.NewId()
	if last != "s1000000" {
		t.Fatalf("last id = %q, want s1000000", last)
	}
}

func TestApplyOpUnknownType(t *testing.T) {
	rt := New(nil, nil)
	if err := rt.ApplyOp(&Op{P: 0}, nil); err != ErrUnknownOp {
		t.Fatalf("got %v, want ErrUnknownOp", err)
	}
}

func TestApplyOpDefaultsTs(t *testing.T) {
	meta := Metadata{"user_id": "u"}
	rt := New(nil, nil)
	before := time.Now()
	if err := rt.ApplyOp(InsertOp("x", 0, nil), meta); err != nil {
		t.Fatal(err)
	}
	ts, ok := meta["ts"].(time.Time)
	if !ok || ts.Before(before) {
		t.Fatalf("ts = %v, want now-ish", meta["ts"])
	}
}

func TestApplyOpsSharedMetadata(t *testing.T) {
	rt := New(nil, nil)
	meta := Metadata{"user_id": "u"}
	ops := []*Op{InsertOp("a", 0, nil), InsertOp("b", 1, nil)}
	if err := rt.ApplyOps(ops, meta); err != nil {
		t.Fatal(err)
	}
	if _, ok := meta["ts"].(time.Time); !ok {
		t.Fatalf("shared metadata ts missing: %v", meta)
	}
}

func TestInsertIntoComments(t *testing.T) {
	rt := New(nil, []CommentItem{{ID: "t", Op: Op{C: ptrTo("abcdef"), P: 2}, Metadata: Metadata{}}})

	// Insert before comment → shift
	rt.ApplyInsertToComments(InsertOp(">>", 1, nil))
	if c := rt.GetComment("t"); c.Op.P != 4 {
		t.Fatalf("p = %d, want 4", c.Op.P)
	}

	// Insert inside comment → extends content at the RELATIVE offset
	// (comment spans document 4..10; inserting at document 5 lands at offset 1)
	rt.ApplyInsertToComments(InsertOp("XY", 5, nil))
	c := rt.GetComment("t")
	if *c.Op.C != "aXYbcdef" || c.Op.P != 4 {
		t.Fatalf("content = %q p=%d, want aXYbcdef p=4", *c.Op.C, c.Op.P)
	}
}

func TestDeleteCommentsBranches(t *testing.T) {
	t.Run("delete fully before shifts", func(t *testing.T) {
		rt := New(nil, []CommentItem{{ID: "t", Op: Op{C: ptrTo("xyz"), P: 5}, Metadata: Metadata{}}})
		if err := rt.ApplyDeleteToComments(DeleteOp("ab", 1, nil)); err != nil {
			t.Fatal(err)
		}
		if c := rt.GetComment("t"); c.Op.P != 3 {
			t.Fatalf("p = %d, want 3", c.Op.P)
		}
	})

	t.Run("delete fully after is a no-op", func(t *testing.T) {
		rt := New(nil, []CommentItem{{ID: "t", Op: Op{C: ptrTo("xyz"), P: 2}, Metadata: Metadata{}}})
		if err := rt.ApplyDeleteToComments(DeleteOp("kl", 8, nil)); err != nil {
			t.Fatal(err)
		}
		c := rt.GetComment("t")
		if c.Op.P != 2 || *c.Op.C != "xyz" {
			t.Fatalf("unexpected change: %v %q", c.Op.P, *c.Op.C)
		}
	})

	t.Run("overlap keeps before+after, drops deleted middle", func(t *testing.T) {
		// comment "hello" at 0 (len 5); delete "ell" at 1 (doc positions 1..4)
		rt := New(nil, []CommentItem{{ID: "t", Op: Op{C: ptrTo("hello"), P: 0}, Metadata: Metadata{}}})
		if err := rt.ApplyDeleteToComments(DeleteOp("ell", 1, nil)); err != nil {
			t.Fatal(err)
		}
		c := rt.GetComment("t")
		if c.Op.P != 0 || *c.Op.C != "ho" {
			t.Fatalf("got p=%d c=%q, want p=0 c=ho", c.Op.P, *c.Op.C)
		}
	})

	t.Run("mismatch throws", func(t *testing.T) {
		rt := New(nil, []CommentItem{{ID: "t", Op: Op{C: ptrTo("hello"), P: 0}, Metadata: Metadata{}}})
		err := rt.ApplyDeleteToComments(DeleteOp("zzz", 1, nil))
		if err != ErrDeletedCommentMismatch {
			t.Fatalf("got %v, want ErrDeletedCommentMismatch", err)
		}
	})
}

func TestDeleteShiftsAndMergesNonTracking(t *testing.T) {
	t.Run("non-tracking: delete before tracked delete shifts it back", func(t *testing.T) {
		rt := New([]Change{{ID: "d", Op: Op{D: ptrTo("ghost"), P: 6}}}, nil)
		mustApply(t, rt, DeleteOp("abc", 1, nil), metaUser("u"))
		c := rt.Changes[0]
		if c.Op.P != 3 || *c.Op.D != "ghost" {
			t.Fatalf("got p=%d d=%q, want p=3 d=ghost", c.Op.P, *c.Op.D)
		}
	})

	t.Run("non-tracking: overlapping tracked delete snaps to op start", func(t *testing.T) {
		rt := New([]Change{{ID: "d", Op: Op{D: ptrTo("abcdef"), P: 4}}}, nil)
		mustApply(t, rt, DeleteOp("XYZ", 2, nil), metaUser("u"))
		c := rt.Changes[0]
		if c.Op.P != 2 || *c.Op.D != "abcdef" {
			t.Fatalf("got p=%d d=%q, want p=2 d=abcdef", c.Op.P, *c.Op.D)
		}
	})
}

func TestTrackingDeleteAbsorbsTrackedDelete(t *testing.T) {
	rt := New([]Change{{ID: "d", Op: Op{D: ptrTo("XYZ"), P: 4}, Metadata: metaUser("u1")}}, nil)
	rt.TrackChanges = true
	mustApply(t, rt, DeleteOp("ab", 2, nil), metaUser("u2"))
	if len(rt.Changes) != 1 {
		t.Fatalf("len = %d", len(rt.Changes))
	}
	c := rt.Changes[0]
	if c.Op.P != 2 || *c.Op.D != "abXYZ" {
		t.Fatalf("got p=%d d=%q, want p=2 d=abXYZ", c.Op.P, *c.Op.D)
	}
}

func TestSplitAnotherUsersInsert(t *testing.T) {
	rt := New([]Change{{ID: "other", Op: Op{I: ptrTo("abcdef"), P: 10}, Metadata: metaUser("u2")}}, nil)
	mustApply(t, rt, InsertOp("X", 12, nil), metaUser("u1"))
	ops := changesOps(rt.Changes)
	if len(ops) != 2 {
		t.Fatalf("len = %d (%v), want 2", len(ops), ops)
	}
	byID := map[string]Op{}
	for i, c := range rt.Changes {
		byID[c.ID] = ops[i]
	}
	before, ok := byID["other"]
	if !ok || deref(before.I) != "ab" || before.P != 10 {
		t.Fatalf("before = %v, want I=ab P=10", before)
	}
	afterSeen := false
	for id, o := range byID {
		if id == "other" {
			continue
		}
		afterSeen = true
		if deref(o.I) != "cdef" || o.P != 13 {
			t.Fatalf("after = {I:%q P:%d}, want {I:cdef P:13}", deref(o.I), o.P)
		}
	}
	if !afterSeen {
		t.Fatalf("no 'after' change found: %v", byID)
	}
}

func TestTrackedDeletesLength(t *testing.T) {
	rt := New([]Change{
		{ID: "a", Op: Op{D: ptrTo("ab"), P: 0}},
		{ID: "b", Op: Op{I: ptrTo("xyz"), P: 2}},
		{ID: "c", Op: Op{D: ptrTo("w"), P: 5}},
	}, nil)
	if got := rt.GetTrackedDeletesLength(); got != 3 {
		t.Fatalf("got %d, want 3", got)
	}
}

func TestDirtyResetAndLiveRefs(t *testing.T) {
	rt := New([]Change{{ID: "x", Op: Op{I: ptrTo("aa"), P: 1}}}, nil)
	rt.TrackChanges = true
	rt.SetIdSeed("beefbeefbeefbeefbe")
	mustApply(t, rt, InsertOp("bb", 0, nil), metaUser("u"))
	if !dirtyHas(rt, "change", "added", "beefbeefbeefbeefbe000001") {
		t.Fatalf("dirty = %v", rt.GetDirtyState())
	}
	if !dirtyHas(rt, "change", "moved", "x") {
		t.Fatalf("dirty = %v", rt.GetDirtyState())
	}
	ref := rt.GetDirtyState().Change.Added["beefbeefbeefbeefbe000001"]
	if ref == nil || ref.Change == nil {
		t.Fatalf("ref = %v", ref)
	}
	rt.ResetDirtyState()
	if dirtyHas(rt, "change", "added", "beefbeefbeefbeefbe000001") {
		t.Fatalf("reset didn't clear")
	}
}

func TestDirtyRefsAreLive(t *testing.T) {
	// Node hands out the SAME object reference: later ops must be visible
	// through the captured reference (document-updater flushes dirty state).
	rt := New(nil, nil)
	rt.TrackChanges = true
	rt.SetIdSeed("cafebeefcafebeef")
	mustApply(t, rt, InsertOp("abc", 0, nil), metaUser("u"))
	ref := rt.GetDirtyState().Change.Added["cafebeefcafebeef000001"]
	if ref == nil || ref.Change == nil {
		t.Fatalf("ref = %v", ref)
	}
	// Merge an adjacent same-user insert — the captured ref must see it.
	mustApply(t, rt, InsertOp("d", 3, nil), metaUser("u"))
	if *ref.Change.Op.I != "abcd" {
		t.Fatalf("live ref I = %q, want abcd", *ref.Change.Op.I)
	}
}

func TestPickTimestampEdges(t *testing.T) {
	old1 := tsDate("2024-01-01T00:00:00Z")
	new1 := tsDate("2024-06-01T00:00:00Z")

	// old ts null → new wins
	if got := pickTimestamp(Metadata{"ts": nil}, Metadata{"ts": new1}); got != new1 {
		t.Fatalf("got %v", got)
	}
	// missing old → new wins
	if got := pickTimestamp(Metadata{}, Metadata{"ts": new1}); got != new1 {
		t.Fatalf("got %v", got)
	}
	// new null → old wins
	if got := pickTimestamp(Metadata{"ts": old1}, Metadata{"ts": nil}); got != old1 {
		t.Fatalf("got %v", got)
	}
	// older wins
	if got := pickTimestamp(Metadata{"ts": old1}, Metadata{"ts": new1}); got != old1 {
		t.Fatalf("got %v", got)
	}
	// newer stays
	if got := pickTimestamp(Metadata{"ts": new1}, Metadata{"ts": old1}); got != old1 {
		t.Fatalf("got %v", got)
	}
	// tie → new (Node: strict <)
	if got := pickTimestamp(Metadata{"ts": old1}, Metadata{"ts": old1}); got != old1 {
		t.Fatalf("got %v", got)
	}
	// unparseable → new (Node: Invalid Date comparison false)
	if got := pickTimestamp(Metadata{"ts": "not-a-date"}, Metadata{"ts": new1}); got != new1 {
		t.Fatalf("got %v", got)
	}
}

func TestGetChangesPreservesOrder(t *testing.T) {
	base := []Change{
		{ID: "z", Op: Op{I: ptrTo("1"), P: 30}},
		{ID: "a", Op: Op{I: ptrTo("2"), P: 10}},
		{ID: "b", Op: Op{I: ptrTo("3"), P: 20}},
	}
	rt := New(base, nil)
	got := rt.GetChanges([]string{"b", "a", "b"})
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("got %v", got)
	}
}
