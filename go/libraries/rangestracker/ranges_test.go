package rangestracker

import (
	"testing"
	"time"
)

// ranges_test.go — 1:1 port of test/unit/ranges-tracker-test.js (mocha/chai
// suite is the oracle; expectations byte-pinned).

func metaUser(id string) Metadata { return Metadata{"user_id": id} }

func changesOps(changes []*Change) []Op {
	ops := make([]Op, len(changes))
	for i, c := range changes {
		ops[i] = c.Op
	}
	return ops
}

func wantOps(t *testing.T, rt *RangesTracker, want []Op) { //nolint:unparam // test helper
	t.Helper()
	got := changesOps(rt.Changes)
	if len(got) != len(want) {
		t.Fatalf("ops len = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if deref(got[i].I) != deref(want[i].I) || deref(got[i].D) != deref(want[i].D) || got[i].P != want[i].P {
			t.Fatalf("op[%d] = {I:%q D:%q P:%d}, want {I:%q D:%q P:%d}",
				i, deref(got[i].I), deref(got[i].D), got[i].P, deref(want[i].I), deref(want[i].D), want[i].P)
		}
	}
}

func wantFullChanges(t *testing.T, rt *RangesTracker, want []Change) { //nolint:unparam // test helper
	t.Helper()
	got := make([]Change, 0, len(rt.Changes))
	for _, c := range rt.Changes {
		got = append(got, *c)
	}
	if len(got) != len(want) {
		t.Fatalf("changes len = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i].ID != want[i].ID || got[i].Op.P != want[i].Op.P {
			t.Fatalf("change[%d] = {ID:%q P:%d}, want {ID:%q P:%d}", i, got[i].ID, got[i].Op.P, want[i].ID, want[i].Op.P)
		}
		if got[i].Op.I != nil != (want[i].Op.I != nil) || got[i].Op.D != nil != (want[i].Op.D != nil) {
			t.Fatalf("change[%d] kind mismatch: %v vs %v", i, got[i].Op, want[i].Op)
		}
		if got[i].Op.I != nil && *got[i].Op.I != *want[i].Op.I {
			t.Fatalf("change[%d] I = %q, want %q", i, *got[i].Op.I, *want[i].Op.I)
		}
		if got[i].Op.D != nil && *got[i].Op.D != *want[i].Op.D {
			t.Fatalf("change[%d] D = %q, want %q", i, *got[i].Op.D, *want[i].Op.D)
		}
	}
}

func b(v bool) *bool { return &v }

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func mustApply(t *testing.T, rt *RangesTracker, op *Op, metadata Metadata) {
	t.Helper()
	if err := rt.ApplyOp(op, metadata); err != nil {
		t.Fatalf("ApplyOp(%v) error: %v", op, err)
	}
}

// --- with duplicate change ids --------------------------------------------

func TestDuplicateChangeIds(t *testing.T) {
	base := []Change{
		{ID: "id1", Op: Op{I: ptrTo("hello"), P: 1}},
		{ID: "id2", Op: Op{I: ptrTo("world"), P: 10}},
		{ID: "id3", Op: Op{I: ptrTo("!!!"), P: 20}},
		{ID: "id1", Op: Op{D: ptrTo("duplicate"), P: 30}},
	}
	rt := New(base, nil)

	t.Run("GetChanges returns all changes with the given ids", func(t *testing.T) {
		got := rt.GetChanges([]string{"id1", "id2"})
		want := []Change{base[0], base[1], base[3]}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i].ID != want[i].ID || got[i].Op.P != want[i].Op.P || deref(got[i].Op.I) != deref(want[i].Op.I) || deref(got[i].Op.D) != deref(want[i].Op.D) {
				t.Fatalf("got[%d] = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("RemoveChangeIds removes all changes with the given ids", func(t *testing.T) {
		rt.RemoveChangeIds([]string{"id1", "id2"})
		want := []Change{base[2]}
		wantFullChanges(t, rt, want)
	})
}

// --- with duplicate tracked insert ids --------------------------------------

func TestDuplicateTrackedInsertIds(t *testing.T) {
	base := []Change{
		{ID: "id1", Op: Op{I: ptrTo("one"), P: 10}},
		{ID: "id1", Op: Op{I: ptrTo("two"), P: 20}},
		{ID: "id1", Op: Op{D: ptrTo("three"), P: 30}},
	}
	rt := New(base, nil)

	t.Run("deleting one tracked insert doesn't delete the others", func(t *testing.T) {
		mustApply(t, rt, DeleteOp("two", 20, nil), metaUser("u"))
		want := []Change{base[0], base[2]}
		wantFullChanges(t, rt, want)
	})
}

// --- with duplicate tracked delete ids ---------------------------------------

func dupDeleteBase() ([]Change, *RangesTracker) {
	base := []Change{
		{ID: "id1", Op: Op{D: ptrTo("one"), P: 10}},
		{ID: "id1", Op: Op{D: ptrTo("two"), P: 20}},
		{ID: "id1", Op: Op{D: ptrTo("three"), P: 30}},
	}
	return base, New(base, nil)
}

func TestDuplicateTrackedDeleteIds(t *testing.T) {
	_, rt := dupDeleteBase()
	rt.TrackChanges = true

	t.Run("deleting over tracked deletes in tracked changes mode removes the covered", func(t *testing.T) {
		_, fresh := dupDeleteBase()
		fresh.TrackChanges = true
		mustApply(t, fresh, DeleteOp("567890123456789012345", 15, nil), metaUser("u"))
		wantOps(t, fresh, []Op{
			{D: ptrTo("one"), P: 10},
			{D: ptrTo("56789two0123456789three012345"), P: 15},
		})
	})

	t.Run("a tracked delete between two tracked deletes joins them", func(t *testing.T) {
		_, fresh := dupDeleteBase()
		fresh.TrackChanges = true
		mustApply(t, fresh, DeleteOp("0123456789", 20, nil), metaUser("u"))
		wantOps(t, fresh, []Op{
			{D: ptrTo("one"), P: 10},
			{D: ptrTo("two0123456789three"), P: 20},
		})
	})

	t.Run("rejecting one tracked delete doesn't reject the others", func(t *testing.T) {
		_, fresh := dupDeleteBase()
		fresh.TrackChanges = true
		mustApply(t, fresh, InsertOp("two", 20, b(true)), metaUser("u"))
		wantOps(t, fresh, []Op{
			{D: ptrTo("one"), P: 10},
			{D: ptrTo("three"), P: 33},
		})
	})

	t.Run("rejecting all tracked deletes doesn't introduce tracked inserts", func(t *testing.T) {
		_, fresh := dupDeleteBase()
		fresh.TrackChanges = true
		mustApply(t, fresh, InsertOp("one", 10, b(true)), metaUser("u"))
		mustApply(t, fresh, InsertOp("two", 23, b(true)), metaUser("u"))
		mustApply(t, fresh, InsertOp("three", 36, b(true)), metaUser("u"))
		if len(fresh.Changes) != 0 {
			t.Fatalf("changes = %v, want empty", fresh.Changes)
		}
	})
}

// --- multiple tracked deletes at the same position ----------------------------

func samePosDeletes() []Change {
	return []Change{
		{ID: "id1", Op: Op{D: ptrTo("before"), P: 33}},
		{ID: "id2", Op: Op{D: ptrTo("right before"), P: 50}},
		{ID: "id3", Op: Op{D: ptrTo("this one"), P: 50}},
		{ID: "id4", Op: Op{D: ptrTo("right after"), P: 50}},
		{ID: "id5", Op: Op{D: ptrTo("long after"), P: 75}},
	}
}

func TestSamePositionTrackedDeletes(t *testing.T) {
	t.Run("preserves the text order when rejecting changes", func(t *testing.T) {
		rt := New(samePosDeletes(), nil)
		mustApply(t, rt, InsertOp("this one", 50, b(true)), metaUser("user-id"))
		want := []Change{
			{ID: "id1", Op: Op{D: ptrTo("before"), P: 33}},
			{ID: "id2", Op: Op{D: ptrTo("right before"), P: 50}},
			{ID: "id4", Op: Op{D: ptrTo("right after"), P: 58}},
			{ID: "id5", Op: Op{D: ptrTo("long after"), P: 83}},
		}
		wantFullChanges(t, rt, want)
	})

	t.Run("moves all tracked deletes after the insert if orderedRejections doesn't reject", func(t *testing.T) {
		rt := New(samePosDeletes(), nil)
		op := InsertOp("some other text", 50, b(true))
		op.OrderedRejections = b(true)
		mustApply(t, rt, op, metaUser("user-id"))
		want := []Change{
			{ID: "id1", Op: Op{D: ptrTo("before"), P: 33}},
			{ID: "id2", Op: Op{D: ptrTo("right before"), P: 65}},
			{ID: "id3", Op: Op{D: ptrTo("this one"), P: 65}},
			{ID: "id4", Op: Op{D: ptrTo("right after"), P: 65}},
			{ID: "id5", Op: Op{D: ptrTo("long after"), P: 90}},
		}
		wantFullChanges(t, rt, want)
	})
}

func TestSamePositionSameContentDeletes(t *testing.T) {
	base := []Change{
		{ID: "id1", Op: Op{D: ptrTo("cat"), P: 10}},
		{ID: "id2", Op: Op{D: ptrTo("giraffe"), P: 10}},
		{ID: "id3", Op: Op{D: ptrTo("cat"), P: 10}},
		{ID: "id4", Op: Op{D: ptrTo("giraffe"), P: 10}},
	}
	rt := New(base, nil)
	mustApply(t, rt, InsertOp("giraffe", 10, b(true)), metaUser("user-id"))
	wantFullChanges(t, rt, []Change{
		{ID: "id1", Op: Op{D: ptrTo("cat"), P: 10}},
		{ID: "id3", Op: Op{D: ptrTo("cat"), P: 17}},
		{ID: "id4", Op: Op{D: ptrTo("giraffe"), P: 17}},
	})
}

// --- tracked insert at same position as tracked delete -------------------------

func TestInsertAtDeletePlusInsertPosition(t *testing.T) {
	base := []Change{
		{ID: "id1", Op: Op{D: ptrTo("before"), P: 5}, Metadata: metaUser("user-id")},
		{ID: "id2", Op: Op{D: ptrTo("delete"), P: 10}, Metadata: metaUser("user-id")},
		{ID: "id3", Op: Op{I: ptrTo("insert"), P: 10}, Metadata: metaUser("user-id")},
	}
	rt := New(base, nil)
	rt.TrackChanges = true
	mustApply(t, rt, InsertOp("incoming", 10, nil), metaUser("user-id"))
	wantOps(t, rt, []Op{
		{D: ptrTo("before"), P: 5},
		{I: ptrTo("incoming"), P: 10},
		{D: ptrTo("delete"), P: 18},
		{I: ptrTo("insert"), P: 18},
	})
}

// --- merging tracked inserts from the same user --------------------------------

func tsAsTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case string:
		d, err := time.Parse(time.RFC3339Nano, t)
		if err != nil {
			return time.Time{}, false
		}
		return d, true
	}
	return time.Time{}, false
}

func tsDate(s string) time.Time {
	d, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestMergingTrackedInsertsSameUser(t *testing.T) {
	newer := tsDate("2024-06-01T00:00:00.000Z")
	older := tsDate("2024-01-01T00:00:00.000Z")

	t.Run("keeps earlier ts when existing insert is older", func(t *testing.T) {
		rt := New([]Change{
			{ID: "id1", Op: Op{I: ptrTo("foo"), P: 10},
				Metadata: Metadata{"user_id": "user-1", "ts": "2024-01-01T00:00:00.000Z"}},
		}, nil)
		rt.TrackChanges = true
		mustApply(t, rt, InsertOp("bar", 13, nil), Metadata{"user_id": "user-1", "ts": newer})
		if len(rt.Changes) != 1 {
			t.Fatalf("len = %d, want 1", len(rt.Changes))
		}
		c := rt.Changes[0]
		if c.Op.P != 10 || *c.Op.I != "foobar" {
			t.Fatalf("op = {I:%q P:%d}, want {I:foobar P:10}", *c.Op.I, c.Op.P)
		}
		if c.Metadata["user_id"] != "user-1" {
			t.Fatalf("user_id = %v", c.Metadata["user_id"])
		}
		got, ok := tsAsTime(c.Metadata["ts"])
		if !ok || !got.Equal(older) {
			t.Fatalf("ts = %v, want %v", got, older)
		}
	})

	t.Run("keeps earlier ts when incoming insert is older", func(t *testing.T) {
		rt := New([]Change{
			{ID: "id1", Op: Op{I: ptrTo("foo"), P: 10},
				Metadata: Metadata{"user_id": "user-1", "ts": "2024-06-01T00:00:00.000Z"}},
		}, nil)
		rt.TrackChanges = true
		mustApply(t, rt, InsertOp("bar", 13, nil), Metadata{"user_id": "user-1", "ts": older})
		c := rt.Changes[0]
		if *c.Op.I != "foobar" || c.Op.P != 10 {
			t.Fatalf("op = {I:%q P:%d}", *c.Op.I, c.Op.P)
		}
		got, ok := tsAsTime(c.Metadata["ts"])
		if !ok || !got.Equal(older) {
			t.Fatalf("ts = %v, want %v", got, older)
		}
	})

	t.Run("uses defined ts when existing insert has null ts", func(t *testing.T) {
		rt := New([]Change{
			{ID: "id1", Op: Op{I: ptrTo("foo"), P: 10},
				Metadata: Metadata{"user_id": "user-1", "ts": nil}},
		}, nil)
		rt.TrackChanges = true
		mustApply(t, rt, InsertOp("bar", 13, nil), Metadata{"user_id": "user-1", "ts": older})
		c := rt.Changes[0]
		got, ok := tsAsTime(c.Metadata["ts"])
		if !ok || !got.Equal(older) {
			t.Fatalf("ts = %v, want %v", c.Metadata["ts"], older)
		}
	})

	t.Run("keeps earlier ts when re-merging inserts after a deletion", func(t *testing.T) {
		rt := New([]Change{
			{ID: "id1", Op: Op{I: ptrTo("aaa"), P: 10},
				Metadata: Metadata{"user_id": "user-1", "ts": "2024-03-01T00:00:00.000Z"}},
			{ID: "id2", Op: Op{I: ptrTo("bb"), P: 13},
				Metadata: Metadata{"user_id": "user-2", "ts": "2024-02-01T00:00:00.000Z"}},
			{ID: "id3", Op: Op{I: ptrTo("ccc"), P: 15},
				Metadata: Metadata{"user_id": "user-1", "ts": "2024-01-01T00:00:00.000Z"}},
		}, nil)
		mustApply(t, rt, DeleteOp("bb", 13, nil), metaUser("user-1"))
		if len(rt.Changes) != 1 {
			t.Fatalf("len = %d, want 1 (got %v)", len(rt.Changes), changesOps(rt.Changes))
		}
		c := rt.Changes[0]
		if *c.Op.I != "aaaccc" || c.Op.P != 10 {
			t.Fatalf("op = {I:%q P:%d}, want {I:aaaccc P:10}", *c.Op.I, c.Op.P)
		}
		if c.Metadata["user_id"] != "user-1" {
			t.Fatalf("user_id = %v", c.Metadata["user_id"])
		}
		got, ok := tsAsTime(c.Metadata["ts"])
		if !ok || !got.Equal(older) {
			t.Fatalf("ts = %v, want %v", c.Metadata["ts"], older)
		}
	})
}
