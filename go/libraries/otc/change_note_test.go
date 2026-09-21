package otc

import (
	"testing"
	"time"
)

func TestChangeNote_Basics(t *testing.T) {
	c := NewChange(nil, time.Time{}, nil, nil, nil, nil, nil)
	n := NewChangeNote(7, c)

	if n.GetBaseVersion() != 7 {
		t.Fatalf("GetBaseVersion = %d; want 7", n.GetBaseVersion())
	}
	if n.GetResultVersion() != 8 {
		t.Fatalf("GetResultVersion = %d; want 8 (baseVersion+1)", n.GetResultVersion())
	}
	if n.GetChange() != c {
		t.Fatal("GetChange should return the same *Change")
	}

	raw := n.ToRaw()
	if bv, ok := raw["baseVersion"].(int); !ok || bv != 7 {
		t.Fatalf("toRaw baseVersion = %v; want 7", raw["baseVersion"])
	}
	if raw["change"] == nil {
		t.Fatal("toRaw should include the change")
	}

	without := n.ToRawWithoutChange()
	if bv, ok := without["baseVersion"].(int); !ok || bv != 7 {
		t.Fatalf("toRawWithoutChange baseVersion = %v; want 7", without["baseVersion"])
	}
	if len(without) != 1 {
		t.Fatalf("toRawWithoutChange should carry only baseVersion; got %v keys", len(without))
	}
	if _, has := without["change"]; has {
		t.Fatal("toRawWithoutChange must NOT include a change")
	}
}

func TestChangeNote_RoundTrip(t *testing.T) {
	c := NewChange(nil, time.Now(), []any{"alice"}, nil, nil, nil, nil)
	n := NewChangeNote(3, c)

	got, err := ChangeNoteFromRaw(n.ToRaw())
	if err != nil {
		t.Fatal(err)
	}
	if got.GetBaseVersion() != 3 {
		t.Fatalf("round-trip baseVersion = %d; want 3", got.GetBaseVersion())
	}
	if got.GetChange() == nil {
		t.Fatal("round-trip should restore the change")
	}
}

func TestChangeNote_FromRawNoChange(t *testing.T) {
	n, err := ChangeNoteFromRaw(map[string]any{"baseVersion": 4})
	if err != nil {
		t.Fatal(err)
	}
	if n.GetBaseVersion() != 4 {
		t.Fatalf("baseVersion = %d; want 4", n.GetBaseVersion())
	}
	if n.GetChange() != nil {
		t.Fatal("a note without a change should have a nil *Change")
	}
}

func TestChangeNote_FromRawBadBaseVersion(t *testing.T) {
	if _, err := ChangeNoteFromRaw(map[string]any{}); err == nil {
		t.Fatal("missing baseVersion must error")
	}
	if _, err := ChangeNoteFromRaw(map[string]any{"baseVersion": "nope"}); err == nil {
		t.Fatal("non-int baseVersion must error")
	}
}
