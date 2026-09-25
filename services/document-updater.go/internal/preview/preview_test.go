package preview

import "testing"

func strp(s string) *string { return &s }

func strRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func eqStr(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func joinStr(s []string, sep string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}

func TestPreview_emptyInputsNoPreviews(t *testing.T) {
	if len(BuildSparseChangePreviews([]Change{}, []string{"a"})) != 0 {
		t.Fatal("changes=[] should yield 0 previews")
	}
	if len(BuildSparseChangePreviews(nil, []string{"a"})) != 0 {
		t.Fatal("changes=nil should yield 0 previews")
	}
	if len(BuildSparseChangePreviews([]Change{{Op: Op{I: strp("x"), P: 0}}}, nil)) != 0 {
		t.Fatal("lines=nil should yield 0 previews")
	}
}

func TestPreview_latexSectionPath(t *testing.T) {
	lines := []string{"\\section{Intro}", "Some intro text.", "\\subsection{Details}", "target line here"}
	pos := len(lines[0]) + 1 + len(lines[1]) + 1 + len(lines[2]) + 5
	got := BuildSparseChangePreviews([]Change{{Op: Op{I: strp("x"), P: pos}}}, lines)
	if len(got) != 1 {
		t.Fatalf("want 1 preview, got %d", len(got))
	}
	if !eqStr(got[0].SectionPath, []string{"Intro", "Details"}) {
		t.Fatalf("sectionPath=%v want [Intro Details]", got[0].SectionPath)
	}
}

func TestPreview_emptySectionPath(t *testing.T) {
	got := BuildSparseChangePreviews([]Change{{Op: Op{I: strp("x"), P: 5}}}, []string{"plain text", "more text"})
	if len(got) != 1 || len(got[0].SectionPath) != 0 {
		t.Fatalf("sectionPath=%v want empty", got[0].SectionPath)
	}
}

func TestPreview_startLineOneBased(t *testing.T) {
	lines := []string{"first", "second", "third"}
	got := BuildSparseChangePreviews([]Change{{Op: Op{I: strp("x"), P: 5 + 1 + 6 + 1 + 1}}}, lines)
	if len(got) != 1 || got[0].StartLine != 3 {
		t.Fatalf("startLine=%d want 3", got[0].StartLine)
	}
}

func TestPreview_newlineAttribute(t *testing.T) {
	got := BuildSparseChangePreviews([]Change{{Op: Op{I: strp("x"), P: 2}}}, []string{"ab", "cd"})
	if len(got) != 1 || got[0].StartLine != 1 {
		t.Fatalf("startLine=%d want 1", got[0].StartLine)
	}
}

func TestPreview_clampToEnd(t *testing.T) {
	got := BuildSparseChangePreviews([]Change{{Op: Op{I: strp("x"), P: 500}}}, []string{"ab", "cd"})
	if len(got) != 1 || got[0].StartLine != 2 {
		t.Fatalf("startLine=%d want 2", got[0].StartLine)
	}
}

func TestPreview_projectOpsAndUserIDs(t *testing.T) {
	got := BuildSparseChangePreviews([]Change{
		{ID: strp("c1"), Op: Op{I: strp("hi"), P: 0}, Metadata: &Metadata{UserID: "u1"}},
		{ID: strp("c2"), Op: Op{D: strp("x"), P: 5}, Metadata: &Metadata{UserID: "u2"}},
	}, []string{"abcdefghij"})
	if len(got) != 1 || len(got[0].Changes) != 2 {
		t.Fatalf("want 1 preview with 2 changes")
	}
	c0, c1 := got[0].Changes[0], got[0].Changes[1]
	if c0.I == nil || *c0.I != "hi" || c0.D != nil || c0.P != 0 {
		t.Fatalf("c0=%#v", c0)
	}
	if c1.I != nil || c1.D == nil || *c1.D != "x" || c1.P != 5 {
		t.Fatalf("c1=%#v", c1)
	}
	if !eqStr(got[0].UserIDs, []string{"u1", "u2"}) {
		t.Fatalf("userIDs=%v want [u1 u2]", got[0].UserIDs)
	}
}

func TestPreview_zeroWidthDelete(t *testing.T) {
	line := "Overleaf is and features are listed below."
	got := BuildSparseChangePreviews([]Change{{Op: Op{D: strp("a great tool "), P: len("Overleaf is ")}}}, []string{line})
	if len(got) != 1 || got[0].Slice != line || got[0].SliceStart != 0 {
		t.Fatalf("delete window slice=%q start=%d want full line at 0", got[0].Slice, got[0].SliceStart)
	}
}

func TestPreview_nearbyOnePreview(t *testing.T) {
	got := BuildSparseChangePreviews([]Change{
		{Op: Op{I: strp("a"), P: 0}, Metadata: &Metadata{UserID: "u1"}},
		{Op: Op{I: strp("b"), P: 20}, Metadata: &Metadata{UserID: "u2"}},
	}, []string{"one two three four five six seven eight nine ten"})
	if len(got) != 1 || len(got[0].Changes) != 2 {
		t.Fatalf("want 1 preview 2 changes, got %d", len(got))
	}
	if !eqStr(got[0].UserIDs, []string{"u1", "u2"}) {
		t.Fatalf("userIDs=%v want [u1 u2]", got[0].UserIDs)
	}
}

func TestPreview_distantSeparatePreviews(t *testing.T) {
	filler := strRepeat("x", 5000)
	top := "\\section{Top}"
	bottom := "\\section{Bottom}"
	lines := []string{top, filler, bottom, filler}
	l1 := len(top) + 1
	l3 := l1 + len(filler) + 1 + len(bottom) + 1
	got := BuildSparseChangePreviews([]Change{
		{Op: Op{I: strp("a"), P: l1 + 3}, Metadata: &Metadata{UserID: "u1"}},
		{Op: Op{I: strp("b"), P: l3 + 3}, Metadata: &Metadata{UserID: "u2"}},
	}, lines)
	if len(got) != 2 {
		t.Fatalf("want 2 previews, got %d", len(got))
	}
	if !eqStr(got[0].SectionPath, []string{"Top"}) || got[0].StartLine != 2 {
		t.Fatalf("top sectionPath=%v start=%d", got[0].SectionPath, got[0].StartLine)
	}
	if !eqStr(got[0].UserIDs, []string{"u1"}) {
		t.Fatalf("top userIDs=%v", got[0].UserIDs)
	}
	if !eqStr(got[1].SectionPath, []string{"Bottom"}) || got[1].StartLine != 4 {
		t.Fatalf("bot sectionPath=%v start=%d", got[1].SectionPath, got[1].StartLine)
	}
	if !eqStr(got[1].UserIDs, []string{"u2"}) {
		t.Fatalf("bot userIDs=%v", got[1].UserIDs)
	}
}

func TestPreview_lineBoundaryKeepsNewline(t *testing.T) {
	lines := []string{"first line", "second line", "third line"}
	secondStart := len(lines[0]) + 1
	nl := string([]byte{10})
	want := lines[0] + nl + lines[1] + nl + lines[2]
	got := BuildSparseChangePreviews([]Change{{Op: Op{I: strp("x"), P: secondStart}}}, lines)
	if len(got) != 1 || got[0].SliceStart != 0 || got[0].Slice != want {
		t.Fatalf("boundary sliceStart=%d slice=%q", got[0].SliceStart, got[0].Slice)
	}
}

func TestPreview_sliceBounded(t *testing.T) {
	filler := strRepeat("x", 5000)
	lines := []string{filler, filler, filler}
	lastStart := (len(filler) + 1) * 2
	got := BuildSparseChangePreviews([]Change{
		{Op: Op{I: strp("a"), P: 0}},
		{Op: Op{I: strp("b"), P: lastStart + 10}},
	}, lines)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	for i, p := range got {
		if len(p.Slice) >= 1200 {
			t.Fatalf("preview %d slice len %d >= 1200", i, len(p.Slice))
		}
	}
}

func TestPreview_orderByPosition(t *testing.T) {
	got := BuildSparseChangePreviews([]Change{
		{Op: Op{I: strp("late"), P: 250}},
		{Op: Op{I: strp("early"), P: 10}},
	}, []string{strRepeat("a", 100), strRepeat("b", 100), strRepeat("c", 100)})
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if got[0].Changes[0].I == nil || *got[0].Changes[0].I != "early" {
		t.Fatal("first should be early")
	}
	if got[1].Changes[0].I == nil || *got[1].Changes[0].I != "late" {
		t.Fatal("second should be late")
	}
}
