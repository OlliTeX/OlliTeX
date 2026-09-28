package opmodel

import (
	"reflect"
	"testing"
)

// Mirrors vendor test/unit/tracked_change.test.js and
// test/unit/tracked_change_list.test.js (overleaf-editor-core).

func wantTC(pos, length int, typ, user, ts string) map[string]any {
	return map[string]any{
		"range":    map[string]any{"pos": pos, "length": length},
		"tracking": map[string]any{"type": typ, "userId": user, "ts": ts},
	}
}

// normalizeTCRaw coerces ToRaw's map[string]int range values into the
// map[string]any wire shape so DeepEqual compares values, not types.
func normalizeTCRaw(raw []map[string]any) []map[string]any {
	out := make([]map[string]any, len(raw))
	for i, m := range raw {
		c := map[string]any{}
		for k, v := range m {
			if rm, ok := v.(map[string]int); ok {
				c[k] = map[string]any{"pos": rm["pos"], "length": rm["length"]}
			} else {
				c[k] = v
			}
		}
		out[i] = c
	}
	return out
}

func tclFromRaw(t *testing.T, raw ...map[string]any) *TrackedChangeList {
	t.Helper()
	items := make([]any, 0, len(raw))
	for _, r := range raw {
		items = append(items, r)
	}
	l, err := TrackedChangeListFromRaw(items)
	if err != nil {
		t.Fatalf("TrackedChangeListFromRaw: %v", err)
	}
	return l
}

func assertTCLRaw(t *testing.T, l *TrackedChangeList, want ...map[string]any) {
	t.Helper()
	got := normalizeTCRaw(l.ToRaw())
	if len(got) != len(want) {
		t.Fatalf("TCL length: got %d (full: %v), want %d", len(got), got, len(want))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TCL raw:\n got %+v\nwant %+v", got, want)
	}
}

const (
	t23 = "2023-01-01T00:00:00.000Z"
	t24 = "2024-01-01T00:00:00.000Z"
)

func trackU1_23() *Tracking { return NewTracking("insert", "user1", unixTs(t23)) }
func trackU1_24() *Tracking { return NewTracking("insert", "user1", unixTs(t24)) }
func trackU2_24() *Tracking { return NewTracking("insert", "user2", unixTs(t24)) }

func unixTs(s string) int64 {
	switch s {
	case t23:
		return 1672531200000
	case t24:
		return 1704067200000
	}
	panic("unknown fixture ts " + s)
}

func TestTrackedChange_SurvivesSerialization(t *testing.T) {
	// Vendor: "should survive serialization".
	orig := TrackedChange{Range: mustRange(t, 1, 2), Tracking: *trackU1_24()}
	raw := orig.ToRaw()
	decoded, err := TrackedChangeFromRaw(raw)
	if err != nil {
		t.Fatalf("fromRaw: %v", err)
	}
	assertEqualT(t, decoded.Range.Pos, orig.Range.Pos, "pos")
	assertEqualT(t, decoded.Range.Length, orig.Range.Length, "length")
	assertEqualT(t, decoded.Tracking.UserId, orig.Tracking.UserId, "user")
	assertEqualT(t, decoded.Tracking.Type, orig.Tracking.Type, "type")
}

func TestTrackedChangeList_SurvivesSerialization(t *testing.T) {
	initial := tclFromRaw(t, wantTC(0, 10, "insert", "user1", t24))
	raw := initial.ToRaw()
	decoded, err := TrackedChangeListFromRaw(rawToAnySlice(raw))
	if err != nil {
		t.Fatalf("fromRaw: %v", err)
	}
	assertEqualT(t, decoded.Len(), initial.Len(), "length")
	if !reflect.DeepEqual(normalizeTCRaw(decoded.ToRaw()), normalizeTCRaw(initial.ToRaw())) {
		t.Fatalf("round-trip mismatch: %v vs %v", decoded.ToRaw(), initial.ToRaw())
	}
}

func rawToAnySlice(raw []map[string]any) []any {
	out := make([]any, len(raw))
	for i, r := range raw {
		out[i] = r
	}
	return out
}

func TestTrackedChangeList_ApplyInsert_SameAuthor_MergesEarliestTs(t *testing.T) {
	// Vendor: "should merge consecutive tracked changes and use the
	// earliest timestamp".
	l := tclFromRaw(t, wantTC(0, 3, "insert", "user1", t23))
	if err := l.ApplyInsert(3, "foo", trackU1_24()); err != nil {
		t.Fatalf("applyInsert: %v", err)
	}
	assertEqualT(t, l.Len(), 1, "merged to one")
	assertTCLRaw(t, l, wantTC(0, 6, "insert", "user1", t23))
}

func TestTrackedChangeList_ApplyInsert_SameAuthor_MiddleExtend(t *testing.T) {
	// Vendor: "should extend tracked changes when inserting in the middle".
	l := tclFromRaw(t, wantTC(0, 10, "insert", "user1", t24))
	if err := l.ApplyInsert(5, "foobar", trackU1_24()); err != nil {
		t.Fatalf("applyInsert: %v", err)
	}
	assertTCLRaw(t, l, wantTC(0, 16, "insert", "user1", t24))
}

func TestTrackedChangeList_ApplyInsert_SameAuthor_Gap(t *testing.T) {
	// Vendor: "should not extend range when there is a gap between the ranges".
	l := tclFromRaw(t, wantTC(0, 3, "insert", "user1", t23))
	if err := l.ApplyInsert(4, "foobar", trackU1_24()); err != nil {
		t.Fatalf("applyInsert: %v", err)
	}
	assertEqualT(t, l.Len(), 2, "two changes")
	assertTCLRaw(t, l,
		wantTC(0, 3, "insert", "user1", t23),
		wantTC(4, 6, "insert", "user1", t24),
	)
}

func TestTrackedChangeList_ApplyInsert_SameAuthor_SpaceBetween(t *testing.T) {
	// Vendor: "should not merge tracked changes if there is a space between
	// them" (inserted AT the last contained cursor... range 5-9, insert at
	// 4: new change at 4, range moves to 8-13; no merge: 12 != 8+... 4-6 and
	// 8-12... vendor expects [{4,3,2024],[8,5,2023}]).
	l := tclFromRaw(t, wantTC(5, 5, "insert", "user1", t23))
	if err := l.ApplyInsert(4, "foo", trackU1_24()); err != nil {
		t.Fatalf("applyInsert: %v", err)
	}
	assertEqualT(t, l.Len(), 2, "two changes, no merge")
	assertTCLRaw(t, l,
		wantTC(4, 3, "insert", "user1", t24),
		wantTC(8, 5, "insert", "user1", t23),
	)
}

func TestTrackedChangeList_ApplyInsert_DiffAuthor_NoMerge(t *testing.T) {
	// Vendor: "should not merge consecutive tracked changes".
	l := tclFromRaw(t, wantTC(0, 3, "insert", "user1", t23))
	if err := l.ApplyInsert(3, "foo", trackU2_24()); err != nil {
		t.Fatalf("applyInsert: %v", err)
	}
	assertEqualT(t, l.Len(), 2, "two changes")
	assertTCLRaw(t, l,
		wantTC(0, 3, "insert", "user1", t23),
		wantTC(3, 3, "insert", "user2", t24),
	)
}

func TestTrackedChangeList_ApplyInsert_DiffAuthor_SamePos(t *testing.T) {
	// Vendor: "should not merge tracked changes at same position".
	l := tclFromRaw(t, wantTC(0, 3, "insert", "user1", t23))
	if err := l.ApplyInsert(0, "foo", trackU2_24()); err != nil {
		t.Fatalf("applyInsert: %v", err)
	}
	assertEqualT(t, l.Len(), 2, "two changes")
	assertTCLRaw(t, l,
		wantTC(0, 3, "insert", "user2", t24),
		wantTC(3, 3, "insert", "user1", t23),
	)
}

func TestTrackedChangeList_ApplyInsert_DiffAuthor_MiddleSplit(t *testing.T) {
	// Vendor: "should insert tracked changes in the middle of a tracked range".
	l := tclFromRaw(t, wantTC(0, 10, "insert", "user1", t23))
	if err := l.ApplyInsert(5, "foobar", trackU2_24()); err != nil {
		t.Fatalf("applyInsert: %v", err)
	}
	assertEqualT(t, l.Len(), 3, "three changes")
	assertTCLRaw(t, l,
		wantTC(0, 5, "insert", "user1", t23),
		wantTC(5, 6, "insert", "user2", t24),
		wantTC(11, 5, "insert", "user1", t23),
	)
}

func TestTrackedChangeList_ApplyInsert_DiffAuthor_AtEnd(t *testing.T) {
	// Vendor: "should insert tracked changes at the end of a tracked range".
	l := tclFromRaw(t, wantTC(0, 5, "insert", "user1", t23))
	if err := l.ApplyInsert(5, "foobar", trackU2_24()); err != nil {
		t.Fatalf("applyInsert: %v", err)
	}
	assertEqualT(t, l.Len(), 2, "two changes")
	assertTCLRaw(t, l,
		wantTC(0, 5, "insert", "user1", t23),
		wantTC(5, 6, "insert", "user2", t24),
	)
}

func TestTrackedChangeList_ApplyInsert_DiffAuthor_SplitLastCursor(t *testing.T) {
	// Vendor: "should split a track range when inserting at last contained
	// cursor".
	l := tclFromRaw(t, wantTC(0, 5, "insert", "user1", t23))
	if err := l.ApplyInsert(4, "foobar", trackU2_24()); err != nil {
		t.Fatalf("applyInsert: %v", err)
	}
	assertEqualT(t, l.Len(), 3, "three changes")
	assertTCLRaw(t, l,
		wantTC(0, 4, "insert", "user1", t23),
		wantTC(4, 6, "insert", "user2", t24),
		wantTC(10, 1, "insert", "user1", t23),
	)
}

func TestTrackedChangeList_ApplyInsert_DiffAuthor_JustBefore(t *testing.T) {
	// Vendor: "should insert a new range if inserted just before the first
	// cursor of a tracked range".
	l := tclFromRaw(t, wantTC(5, 5, "insert", "user1", t23))
	if err := l.ApplyInsert(5, "foobar", trackU2_24()); err != nil {
		t.Fatalf("applyInsert: %v", err)
	}
	assertEqualT(t, l.Len(), 2, "two changes")
	assertTCLRaw(t, l,
		wantTC(5, 6, "insert", "user2", t24),
		wantTC(11, 5, "insert", "user1", t23),
	)
}

func TestTrackedChangeList_ApplyDelete_Shrinks(t *testing.T) {
	// Vendor: "should shrink tracked changes".
	l := tclFromRaw(t, wantTC(0, 10, "insert", "user1", t23))
	if err := l.ApplyDelete(5, 2); err != nil {
		t.Fatalf("applyDelete: %v", err)
	}
	assertTCLRaw(t, l, wantTC(0, 8, "insert", "user1", t23))
}

func TestTrackedChangeList_ApplyDelete_FullRemoval(t *testing.T) {
	// Vendor: "should delete tracked changes when the whole range is deleted".
	l := tclFromRaw(t, wantTC(0, 10, "insert", "user1", t23))
	if err := l.ApplyDelete(0, 10); err != nil {
		t.Fatalf("applyDelete: %v", err)
	}
	assertEqualT(t, l.Len(), 0, "empty")

	// ...and when MORE than the whole range is deleted.
	l2 := tclFromRaw(t, wantTC(5, 10, "insert", "user1", t23))
	if err := l2.ApplyDelete(0, 25); err != nil {
		t.Fatalf("applyDelete: %v", err)
	}
	assertEqualT(t, l2.Len(), 0, "empty (over-delete)")
}

func TestTrackedChangeList_ApplyDelete_OverlapStart(t *testing.T) {
	// Vendor: "should shrink the tracked change from start with overlap".
	l := tclFromRaw(t, wantTC(0, 10, "insert", "user1", t23))
	if err := l.ApplyDelete(1, 9); err != nil {
		t.Fatalf("applyDelete: %v", err)
	}
	assertTCLRaw(t, l, wantTC(0, 1, "insert", "user1", t23))
}

func TestTrackedChangeList_ApplyDelete_OverlapEnd(t *testing.T) {
	// Vendor: "should shrink the tracked change from end with overlap".
	l := tclFromRaw(t, wantTC(0, 10, "insert", "user1", t23))
	if err := l.ApplyDelete(0, 9); err != nil {
		t.Fatalf("applyDelete: %v", err)
	}
	assertTCLRaw(t, l, wantTC(0, 1, "insert", "user1", t23))
}

func TestTrackedChangeList_ApplyRetain_TracksRange(t *testing.T) {
	// Vendor: "should add tracking information to an untracked range".
	l := tclFromRaw(t)
	if err := l.ApplyRetain(0, 10, trackU1_24()); err != nil {
		t.Fatalf("applyRetain: %v", err)
	}
	assertTCLRaw(t, l, wantTC(0, 10, "insert", "user1", t24))
}

func TestTrackedChangeList_ApplyRetain_Shrink(t *testing.T) {
	// Vendor: "should shrink a tracked range to make room for retained
	// operation".
	l := tclFromRaw(t, wantTC(3, 7, "insert", "user1", t23))
	if err := l.ApplyRetain(0, 5, trackU2_24()); err != nil {
		t.Fatalf("applyRetain: %v", err)
	}
	assertEqualT(t, l.Len(), 2, "two changes")
	assertTCLRaw(t, l,
		wantTC(0, 5, "insert", "user2", t24),
		wantTC(5, 5, "insert", "user1", t23),
	)
}

func TestTrackedChangeList_ApplyRetain_ClearDropsChange(t *testing.T) {
	// Vendor: "should delete a tracked change which is being resolved"
	// (both user1 and user2 get the same behaviour) and "... being rejected"
	// (delete-type change).
	l := tclFromRaw(t, wantTC(0, 10, "insert", "user1", t23))
	if err := l.ApplyRetain(0, 10, Clear()); err != nil {
		t.Fatalf("applyRetain: %v", err)
	}
	assertEqualT(t, l.Len(), 0, "cleared")

	l2 := tclFromRaw(t, map[string]any{
		"range":    map[string]any{"pos": 0, "length": 10},
		"tracking": map[string]any{"type": "delete", "userId": "user1", "ts": t23},
	})
	if err := l2.ApplyRetain(0, 10, Clear()); err != nil {
		t.Fatalf("applyRetain: %v", err)
	}
	assertEqualT(t, l2.Len(), 0, "rejected cleared")
}

func TestTrackedChangeList_ApplyRetain_AppendOtherUser(t *testing.T) {
	// Vendor: "should append a new tracked change when retaining a range
	// from another user with tracking info".
	l := tclFromRaw(t, map[string]any{
		"range":    map[string]any{"pos": 4, "length": 4},
		"tracking": map[string]any{"type": "delete", "userId": "user1", "ts": t23},
	})
	user2del := NewTracking("delete", "user2", unixTs(t24))
	if err := l.ApplyRetain(8, 1, user2del); err != nil {
		t.Fatalf("applyRetain: %v", err)
	}
	assertEqualT(t, l.Len(), 2, "two changes")
	assertTCLRaw(t, l,
		map[string]any{
			"range":    map[string]any{"pos": 4, "length": 4},
			"tracking": map[string]any{"type": "delete", "userId": "user1", "ts": t23},
		},
		map[string]any{
			"range":    map[string]any{"pos": 8, "length": 1},
			"tracking": map[string]any{"type": "delete", "userId": "user2", "ts": t24},
		},
	)
}

func TestTrackedChangeList_ApplyRetain_NoTrackingLeavesAlone(t *testing.T) {
	// Vendor: "should leave ignore a retain operation with no tracking info".
	l := tclFromRaw(t, wantTC(3, 7, "insert", "user1", t23))
	if err := l.ApplyRetain(0, 5, nil); err != nil {
		t.Fatalf("applyRetain: %v", err)
	}
	assertEqualT(t, l.Len(), 1, "untouched")
	assertTCLRaw(t, l, wantTC(3, 7, "insert", "user1", t23))
}
