// Mirrors Node test/unit/js/ShareJS/TextTransformTests.js 1:1, plus a
// randomised OP self-consistency property against the vendored rangestracker
// (comment state is what document-updater actually consumes).
package sharejstext

import (
	"fmt"
	"testing"

	"ollitex/go/libraries/rangestracker"
)

const mockThread = "mock-thread-id"

func mkI(s string, p int) Component { return Insert(p, s) }

func mkD(s string, p int) Component { return Delete(p, s) }

func mkC(s, t string, p int) Component { return Comment(p, s, t) }

func runTC(t *testing.T, c, otherC Component, side string) []Component {
	t.Helper()
	dest := []Component{}
	if err := TransformComponent(&dest, c, otherC, side); err != nil {
		t.Fatalf("TransformComponent(%s, %s, %s): %v", c, otherC, side, err)
	}
	return dest
}

func wantEqual(t *testing.T, got, want []Component, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %s, want %s", label, OpString(got), OpString(want))
	}
	for i := range want {
		if !equalComponent(got[i], want[i]) {
			t.Fatalf("%s[%d] = %s, want %s", label, i, got[i], want[i])
		}
	}
}

func equalComponent(a, b Component) bool {
	if (a.I == nil) != (b.I == nil) || (a.D == nil) != (b.D == nil) || (a.C == nil) != (b.C == nil) {
		return false
	}
	if a.I != nil && *a.I != *b.I {
		return false
	}
	if a.D != nil && *a.D != *b.D {
		return false
	}
	if a.C != nil && *a.C != *b.C {
		return false
	}
	if a.P != b.P {
		return false
	}
	if (a.T == nil) != (b.T == nil) {
		return false
	}
	if a.T != nil && *a.T != *b.T {
		return false
	}
	return true
}

func TestTransform_Insert_Insert(t *testing.T) {
	var dest []Component
	dest = runTC(t, mkI("foo", 9), mkI("bar", 3), "left")
	wantEqual(t, dest, []Component{mkI("foo", 12)}, "insert before")

	dest = runTC(t, mkI("foo", 3), mkI("bar", 9), "left")
	wantEqual(t, dest, []Component{mkI("foo", 3)}, "insert after")

	dest = runTC(t, mkI("foo", 3), mkI("bar", 3), "right")
	wantEqual(t, dest, []Component{mkI("foo", 6)}, "insert same right")

	dest = runTC(t, mkI("foo", 3), mkI("bar", 3), "left")
	wantEqual(t, dest, []Component{mkI("foo", 3)}, "insert same left")
}

func TestTransform_Insert_Delete(t *testing.T) {
	var dest []Component
	dest = runTC(t, mkI("foo", 9), mkD("bar", 3), "left")
	wantEqual(t, dest, []Component{mkI("foo", 6)}, "delete before")

	dest = runTC(t, mkI("foo", 3), mkD("bar", 9), "left")
	wantEqual(t, dest, []Component{mkI("foo", 3)}, "delete after")

	dest = runTC(t, mkI("foo", 3), mkD("bar", 3), "right")
	wantEqual(t, dest, []Component{mkI("foo", 3)}, "delete same right")

	dest = runTC(t, mkI("foo", 3), mkD("bar", 3), "left")
	wantEqual(t, dest, []Component{mkI("foo", 3)}, "delete same left")
}

func TestTransform_Delete_Insert(t *testing.T) {
	var dest []Component
	dest = runTC(t, mkD("foo", 9), mkI("bar", 3), "left")
	wantEqual(t, dest, []Component{mkD("foo", 12)}, "insert before")

	dest = runTC(t, mkD("foo", 3), mkI("bar", 9), "left")
	wantEqual(t, dest, []Component{mkD("foo", 3)}, "insert after")

	dest = runTC(t, mkD("foo", 3), mkI("bar", 3), "right")
	wantEqual(t, dest, []Component{mkD("foo", 6)}, "insert same right")

	dest = runTC(t, mkD("foo", 3), mkI("bar", 3), "left")
	wantEqual(t, dest, []Component{mkD("foo", 6)}, "insert same left")

	// delete overlapping the insert location splits into two deletes
	dest = runTC(t, mkD("foo", 3), mkI("bar", 4), "left")
	wantEqual(t, dest, []Component{mkD("f", 3), mkD("oo", 6)}, "insert inside delete")
}

func TestTransform_Delete_Delete(t *testing.T) {
	var dest []Component
	dest = runTC(t, mkD("foo", 9), mkD("bar", 3), "left")
	wantEqual(t, dest, []Component{mkD("foo", 6)}, "delete before")

	dest = runTC(t, mkD("foo", 3), mkD("bar", 9), "left")
	wantEqual(t, dest, []Component{mkD("foo", 3)}, "delete after")

	// deleting the same content cancels out
	dest = runTC(t, mkD("foo", 3), mkD("foo", 3), "right")
	wantEqual(t, dest, []Component{}, "delete same content")

	dest = runTC(t, mkD("foobar", 3), mkD("abcfoo", 0), "right")
	wantEqual(t, dest, []Component{mkD("bar", 0)}, "delete overlap before")

	dest = runTC(t, mkD("abcfoo", 3), mkD("foobar", 6), "left")
	wantEqual(t, dest, []Component{mkD("abc", 3)}, "delete overlap after")

	dest = runTC(t, mkD("abcfoo123", 3), mkD("foo", 6), "left")
	wantEqual(t, dest, []Component{mkD("abc123", 3)}, "delete overlap whole")

	// fully contained delete cancels out
	dest = runTC(t, mkD("foo", 6), mkD("abcfoo123", 3), "left")
	wantEqual(t, dest, []Component{}, "delete inside whole")
}

func TestTransform_Comment_Insert(t *testing.T) {
	var dest []Component
	dest = runTC(t, mkC("foo", mockThread, 9), mkI("bar", 3), "left")
	wantEqual(t, dest, []Component{mkC("foo", mockThread, 12)}, "insert before")

	dest = runTC(t, mkC("foo", mockThread, 3), mkI("bar", 9), "left")
	wantEqual(t, dest, []Component{mkC("foo", mockThread, 3)}, "insert after")

	// RangesTracker doesn't inject inserts into comments on edges, so neither should we
	dest = runTC(t, mkC("foo", mockThread, 3), mkI("bar", 3), "left")
	wantEqual(t, dest, []Component{mkC("foo", mockThread, 6)}, "insert left edge")

	dest = runTC(t, mkC("foo", mockThread, 3), mkI("bar", 6), "left")
	wantEqual(t, dest, []Component{mkC("foo", mockThread, 3)}, "insert right edge")

	// insert in the middle extends the comment text
	dest = runTC(t, mkC("foo", mockThread, 3), mkI("bar", 5), "left")
	wantEqual(t, dest, []Component{mkC("fobaro", mockThread, 3)}, "insert middle")
}

func TestTransform_Comment_Delete(t *testing.T) {
	var dest []Component
	dest = runTC(t, mkC("foo", mockThread, 9), mkD("bar", 3), "left")
	wantEqual(t, dest, []Component{mkC("foo", mockThread, 6)}, "delete before")

	// (Node oracle uses an insert here — mirror verbatim.)
	dest = runTC(t, mkC("foo", mockThread, 3), mkI("bar", 9), "left")
	wantEqual(t, dest, []Component{mkC("foo", mockThread, 3)}, "insert after")

	dest = runTC(t, mkC("foobar", mockThread, 6), mkD("123foo", 3), "left")
	wantEqual(t, dest, []Component{mkC("bar", mockThread, 3)}, "delete overlap before")

	dest = runTC(t, mkC("foobar", mockThread, 6), mkD("bar123", 9), "left")
	wantEqual(t, dest, []Component{mkC("foo", mockThread, 6)}, "delete overlap after")

	dest = runTC(t, mkC("foo123bar", mockThread, 6), mkD("123", 9), "left")
	wantEqual(t, dest, []Component{mkC("foobar", mockThread, 6)}, "delete overlap middle")

	// delete overlapping the whole comment crops it to empty
	dest = runTC(t, mkC("foo", mockThread, 6), mkD("123foo456", 3), "left")
	wantEqual(t, dest, []Component{mkC("", mockThread, 3)}, "delete whole comment")
}

func TestTransform_Comment_Nop(t *testing.T) {
	var dest []Component
	dest = runTC(t, mkI("foo", 6), mkC("bar", mockThread, 3), "left")
	wantEqual(t, dest, []Component{mkI("foo", 6)}, "insert vs comment")

	dest = runTC(t, mkD("foo", 6), mkC("bar", mockThread, 3), "left")
	wantEqual(t, dest, []Component{mkD("foo", 6)}, "delete vs comment")

	dest = runTC(t, mkC("foo", mockThread, 6), mkC("bar", mockThread, 3), "left")
	wantEqual(t, dest, []Component{mkC("foo", mockThread, 6)}, "comment vs comment")
}

func TestApply(t *testing.T) {
	got, err := Apply("foo", []Component{Insert(2, "bar")})
	if err != nil || got != "fobaro" {
		t.Fatalf("apply insert = %q (err %v), want fobaro", got, err)
	}
	got, err = Apply("foo123bar", []Component{Delete(3, "123")})
	if err != nil || got != "foobar" {
		t.Fatalf("apply delete = %q (err %v), want foobar", got, err)
	}
	got, err = Apply("foo123bar", []Component{Comment(3, "123", mockThread)})
	if err != nil || got != "foo123bar" {
		t.Fatalf("apply comment = %q (err %v), want foo123bar", got, err)
	}
	if _, err = Apply("foo123bar", []Component{Delete(3, "456")}); err == nil {
		t.Fatalf("apply mismatched delete: want error")
	}
	if _, err = Apply("foo123bar", []Component{Comment(3, "456", mockThread)}); err == nil {
		t.Fatalf("apply mismatched comment: want error")
	}
}

func TestInvert(t *testing.T) {
	out := Invert([]Component{
		Insert(2, "bar"),
		Delete(5, "foo"),
	})
	wantEqual(t, out, []Component{Insert(5, "foo"), Delete(2, "bar")}, "invert")
}

func TestNormalize(t *testing.T) {
	// adjacent inserts compose during normalize (no reordering happened)
	out := Normalize([]Component{
		Insert(0, "a"),
		Insert(1, "b"),
	})
	wantEqual(t, out, []Component{Insert(0, "ab")}, "normalize")
	out = Normalize([]Component{
		Insert(1, "b"),
		Insert(0, "a"),
	})
	wantEqual(t, out, []Component{Insert(1, "b"), Insert(0, "a")}, "normalize no reorder")
}

func TestCompress(t *testing.T) {
	out := Compress([]Component{
		Insert(0, "a"),
		Insert(1, "b"),
	})
	wantEqual(t, out, []Component{Insert(0, "ab")}, "compress")
}

func TestCompose(t *testing.T) {
	out := Compose(
		[]Component{Insert(0, "a"), Delete(1, "b")},
		[]Component{Insert(0, "c"), Delete(4, "x")},
	)
	wantEqual(t, out, []Component{Insert(0, "a"), Delete(1, "b"), Insert(0, "c"), Delete(4, "x")}, "compose")
}

func TestTransformCursor(t *testing.T) {
	pos, err := TransformCursor(5, []Component{Delete(2, "abc")}, "left")
	if err != nil || pos != 2 {
		t.Fatalf("cursor in delete = %d (err %v), want 2", pos, err)
	}
	pos, err = TransformCursor(5, []Component{Insert(2, "abc")}, "left")
	if err != nil || pos != 8 {
		t.Fatalf("cursor after insert left = %d (err %v), want 8", pos, err)
	}
	pos, err = TransformCursor(5, []Component{Insert(5, "abc")}, "left")
	if err != nil || pos != 5 {
		t.Fatalf("cursor same-position insert left = %d (err %v), want 5", pos, err)
	}
	pos, err = TransformCursor(5, []Component{Insert(5, "abc")}, "right")
	if err != nil || pos != 8 {
		t.Fatalf("cursor same-position insert right = %d (err %v), want 8", pos, err)
	}
}

func TestTransformX_MultiOp(t *testing.T) {
	left, right, err := TransformX(
		[]Component{Insert(0, "a"), Delete(1, "b")},
		[]Component{Insert(5, "x")},
	)
	if err != nil {
		t.Fatalf("TransformX: %v", err)
	}
	wantEqual(t, left, []Component{Insert(0, "a"), Delete(1, "b")}, "left")
	wantEqual(t, right, []Component{Insert(5, "x")}, "right")
}

// --- property: applying ops and comments in different orders must agree ---

func applyTracker(rt *rangestracker.RangesTracker, ops []Component) {
	for i := range ops {
		o := &rangestracker.Op{I: ops[i].I, D: ops[i].D, C: ops[i].C, T: ops[i].T, P: ops[i].P}
		if err := rt.ApplyOp(o, rangestracker.Metadata{}); err != nil {
			panic(err)
		}
	}
}

func commentsEqual(c1, c2 []*rangestracker.CommentItem) bool {
	if len(c1) != len(c2) {
		return false
	}
	type key struct {
		offset int
		text   string
	}
	count := map[key]int{}
	for _, c := range c1 {
		count[key{c.Op.P, *c.Op.C}]++
	}
	for _, c := range c2 {
		k := key{c.Op.P, *c.Op.C}
		count[k]--
		if count[k] < 0 {
			return false
		}
	}
	for _, v := range count {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestOT_SelfConsistent(t *testing.T) {
	snapshot := "123"
	var ops []Component
	for p := 0; p <= len(snapshot); p++ {
		ops = append(ops, Insert(p, "a"), Insert(p, "bc"))
	}
	for p := 0; p < len(snapshot); p++ {
		for l := 1; l <= len(snapshot)-p; l++ {
			ops = append(ops, Delete(p, snapshot[p:p+l]))
		}
	}
	for p := 0; p < len(snapshot); p++ {
		for l := 1; l <= len(snapshot)-p; l++ {
			// Each comment op gets its OWN thread id so it always creates a
			// comment in the tracker (a repeated thread id would move the
			// existing comment instead — move semantics is order-asymmetric
			// and is exercised separately, not here).
			c, th := snapshot[p:p+l], fmt.Sprintf("t-%d-%d", p, l)
			ops = append(ops, Component{C: &c, P: p, T: &th})
		}
	}
	for _, op1 := range ops {
		for _, op2 := range ops {
			op1T, err := Transform([]Component{op1}, []Component{op2}, "left")
			if err != nil {
				t.Fatalf("transform %s: %v", op1, err)
			}
			op2T, err := Transform([]Component{op2}, []Component{op1}, "right")
			if err != nil {
				t.Fatalf("transform %s: %v", op2, err)
			}
			s1, _ := Apply(snapshot, []Component{op1})
			s12, err := Apply(s1, op2T)
			if err != nil {
				t.Fatalf("apply %s: %v", op1, err)
			}
			rt12 := rangestracker.New(nil, nil)
			applyTracker(rt12, []Component{op1})
			applyTracker(rt12, op2T)

			s2, _ := Apply(snapshot, []Component{op2})
			s21, err := Apply(s2, op1T)
			if err != nil {
				t.Fatalf("apply %s: %v", op2, err)
			}
			rt21 := rangestracker.New(nil, nil)
			applyTracker(rt21, []Component{op2})
			applyTracker(rt21, op1T)

			if s12 != s21 {
				t.Fatalf("OT inconsistent: %q vs %q (op1=%s op2=%s op1T=%s op2T=%s)",
					s12, s21, op1, op2, op1T, op2T)
			}
			if !commentsEqual(rt12.Comments, rt21.Comments) {
				t.Fatalf("OT comments inconsistent (op1=%s op2=%s op1T=%s op2T=%s) rt12=%v rt21=%v",
					op1, op2, op1T, op2T, rt12.Comments, rt21.Comments)
			}
		}
	}
}
