package filetreediff

// Oracle tests mirror vendor `test/unit/file_tree_diff.test.js` (456 L)
// case-for-case: the vendor `testCases` table, "keeps the file of the most
// recent add", the unseeded-mode cases, and the onMoveCollision cases.

import "testing"

// opspec is one vendor-fixture operation, "kind[:pathname[:arg]]":
//
//	"add:<pn>:<file>"  "edit:<pn>"  "mv:<pn>:<newPn>"  "rm:<pn>"  "other"
func opFromSpec(s string) Op {
	kind, rest, ok := cut3(s, ':')
	if !ok {
		return &OtherOp{}
	}
	switch kind {
	case "add":
		pn, f, _ := cut3(rest, ':')
		return &AddOp{Pathname: pn, File: f}
	case "edit":
		return &EditOp{Pathname: rest}
	case "mv":
		pn, pn2, _ := cut3(rest, ':')
		return &MoveOp{Pathname: pn, NewPathname: pn2}
	case "rm":
		return &MoveOp{Pathname: rest, NewPathname: ""}
	default:
		return &OtherOp{}
	}
}

// cut3 splits s on the first byte d: ok=false when there is no separator
// (rest is empty and the whole s is the first field).
func cut3(s string, d byte) (a, b string, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == d {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// fold drives BuildFileTreeDiff from vendor-style fixture strings. A nil
// init is vendor's unseeded mode (initialPathnames omitted).
func fold(init []string, changes ...[]string) (*Result, error) {
	var ip *[]string
	if init != nil {
		ip = &init
	}
	cs := make([]*Change, 0, len(changes))
	for _, ch := range changes {
		ops := make([]Op, 0, len(ch))
		for _, s := range ch {
			ops = append(ops, opFromSpec(s))
		}
		cs = append(cs, ChangeOf(ops...))
	}
	return BuildFileTreeDiff(cs, &Options{InitialPathnames: ip})
}

func chainStr(e *Entry) string {
	out := ""
	for i, c := range e.Chain {
		if i > 0 {
			out += " "
		}
		out += c
	}
	return out
}

// sum is a fixture row: (chain, origin, edited, firstEdited, hasFile,
// deletedAt) — "" = nil.
type sum struct {
	chain   string
	origin  string
	edited  bool
	first   string
	hasFile bool
	deleted string
}

func assertEntry(t *testing.T, where string, e *Entry, w sum) {
	t.Helper()
	if got := chainStr(e); got != w.chain {
		t.Fatalf("%s: chain got %q want %q", where, got, w.chain)
	}
	orig := ""
	if e.Origin != nil {
		orig = *e.Origin
	}
	if orig != w.origin {
		t.Fatalf("%s: origin got %q want %q", where, orig, w.origin)
	}
	if e.Edited != w.edited {
		t.Fatalf("%s: edited got %v want %v", where, e.Edited, w.edited)
	}
	gf := ""
	if e.FirstEditedAtChainIndex != nil {
		gf = digit(*e.FirstEditedAtChainIndex)
	}
	if gf != w.first {
		t.Fatalf("%s: first got %q want %q", where, gf, w.first)
	}
	gd := ""
	if e.DeletedAtChangeIndex != nil {
		gd = digit(*e.DeletedAtChangeIndex)
	}
	if gd != w.deleted {
		t.Fatalf("%s: deleted got %q want %q", where, gd, w.deleted)
	}
	if (e.File != nil) != w.hasFile {
		t.Fatalf("%s: hasFile got %v want %v", where, e.File != nil, w.hasFile)
	}
}

func digit(n int) string { return string(byte('0' + byte(n))) }

func TestVendorOracleTable(t *testing.T) {
	type tc struct {
		name    string
		init    []string
		changes [][]string
		want    []sum
		wantRem []sum
	}
	cases := []tc{
		{
			name:    "reports the files that the window leaves untouched",
			init:    []string{"a.tex", "b.tex"},
			changes: nil,
			want: []sum{
				{"a.tex", "a.tex", false, "", false, ""},
				{"b.tex", "b.tex", false, "", false, ""},
			},
		},
		{
			name: "collapses a chain of moves into a single entry",
			init: []string{"a.tex"},
			changes: [][]string{
				{"mv:a.tex:b.tex"},
				{"mv:b.tex:c.tex"},
			},
			want: []sum{
				{"a.tex b.tex c.tex", "a.tex", false, "", false, ""},
			},
		},
		{
			name: "records the pathnames of a file that is moved and then removed",
			init: []string{"a.tex"},
			changes: [][]string{
				{"mv:a.tex:b.tex"},
				{"rm:b.tex"},
			},
			want: []sum{
				{"a.tex b.tex", "a.tex", false, "", false, "1"},
			},
			wantRem: []sum{
				{"a.tex b.tex", "a.tex", false, "", false, "1"},
			},
		},
		{
			name:    "reports a file that is added and then moved at its new pathname",
			init:    []string{},
			changes: [][]string{{"add:a.tex:content"}, {"mv:a.tex:b.tex"}},
			want: []sum{
				{"a.tex b.tex", "", false, "", true, ""},
			},
		},
		{
			name:    "reports a file that is added and then removed as removed",
			init:    []string{},
			changes: [][]string{{"add:a.tex:content"}, {"rm:a.tex"}},
			want: []sum{
				{"a.tex", "", false, "", true, "1"},
			},
			wantRem: []sum{
				{"a.tex", "", false, "", true, "1"},
			},
		},
		{
			name:    "reports a file that is removed and then added again as added",
			init:    []string{"a.tex"},
			changes: [][]string{{"rm:a.tex"}, {"add:a.tex:content"}},
			want: []sum{
				{"a.tex", "", false, "", true, ""},
			},
			wantRem: []sum{
				{"a.tex", "a.tex", false, "", false, "0"},
			},
		},
		{
			name:    "reports a file that another file is moved onto as removed",
			init:    []string{"a.tex", "b.tex"},
			changes: [][]string{{"mv:a.tex:b.tex"}},
			want: []sum{
				{"a.tex b.tex", "a.tex", false, "", false, ""},
			},
			wantRem: []sum{
				{"b.tex", "b.tex", false, "", false, "0"},
			},
		},
		{
			name:    "reports a file that another file is added over as removed",
			init:    []string{"a.tex"},
			changes: [][]string{{"add:a.tex:content"}},
			want: []sum{
				{"a.tex", "", false, "", true, ""},
			},
			wantRem: []sum{
				{"a.tex", "a.tex", false, "", false, "0"},
			},
		},
		{
			name:    "reports a file that is moved onto a pathname removed earlier in the window",
			init:    []string{"a.tex", "b.tex"},
			changes: [][]string{{"rm:a.tex"}, {"mv:b.tex:a.tex"}},
			want: []sum{
				{"b.tex a.tex", "b.tex", false, "", false, ""},
			},
			wantRem: []sum{
				{"a.tex", "a.tex", false, "", false, "0"},
			},
		},
		{
			name:    "records an edit of a file that is moved afterwards",
			init:    []string{"a.tex"},
			changes: [][]string{{"edit:a.tex"}, {"mv:a.tex:b.tex"}},
			want: []sum{
				{"a.tex b.tex", "a.tex", true, "0", false, ""},
			},
		},
		{
			name:    "records an edit of a file that was moved before",
			init:    []string{"a.tex"},
			changes: [][]string{{"mv:a.tex:b.tex"}, {"edit:b.tex"}},
			want: []sum{
				{"a.tex b.tex", "a.tex", true, "1", false, ""},
			},
		},
		{
			name:    "records the first of several edits",
			init:    []string{"a.tex"},
			changes: [][]string{{"edit:a.tex"}, {"mv:a.tex:b.tex"}, {"edit:b.tex"}},
			want: []sum{
				{"a.tex b.tex", "a.tex", true, "0", false, ""},
			},
		},
		{
			name:    "creates an entry for an edit of a pathname it has not seen",
			init:    []string{},
			changes: [][]string{{"edit:a.tex"}},
			want: []sum{
				{"a.tex", "a.tex", true, "0", false, ""},
			},
		},
		{
			name:    "ignores an edit of a removed file",
			init:    []string{"a.tex"},
			changes: [][]string{{"rm:a.tex"}, {"edit:a.tex"}},
			want: []sum{
				{"a.tex", "a.tex", false, "", false, "0"},
			},
			wantRem: []sum{
				{"a.tex", "a.tex", false, "", false, "0"},
			},
		},
		{
			name: "resolves a swap of two files by their pathnames",
			init: []string{"a.tex", "b.tex"},
			changes: [][]string{
				{"mv:a.tex:tmp.tex"},
				{"mv:b.tex:a.tex"},
				{"mv:tmp.tex:b.tex"},
			},
			want: []sum{
				{"b.tex a.tex", "b.tex", false, "", false, ""},
				{"a.tex tmp.tex b.tex", "a.tex", false, "", false, ""},
			},
		},
		{
			name:    "skips a move of a pathname that holds no file",
			init:    []string{"a.tex"},
			changes: [][]string{{"mv:missing.tex:b.tex"}},
			want: []sum{
				{"a.tex", "a.tex", false, "", false, ""},
			},
		},
		{
			name:    "skips a removal of a pathname that holds no file",
			init:    []string{"a.tex"},
			changes: [][]string{{"rm:missing.tex"}},
			want: []sum{
				{"a.tex", "a.tex", false, "", false, ""},
			},
		},
		{
			name:    "skips operations without a pathname",
			init:    []string{},
			changes: [][]string{{"edit:", "mv::a.tex", "rm:", "add::x"}},
			want:    []sum{},
		},
		{
			name:    "ignores operations that leave the file tree unchanged",
			init:    []string{"a.tex"},
			changes: [][]string{{"other", "mv:a.tex:a.tex", "other"}},
			want: []sum{
				{"a.tex", "a.tex", false, "", false, ""},
			},
		},
		{
			name:    "folds the operations of a single change in order",
			init:    []string{"a.tex"},
			changes: [][]string{{"mv:a.tex:b.tex", "add:a.tex:content", "edit:b.tex"}},
			want: []sum{
				{"a.tex b.tex", "a.tex", true, "1", false, ""},
				{"a.tex", "", false, "", true, ""},
			},
		},
		{
			name:    "reports the index of the change that removed a file",
			init:    []string{"a.tex", "b.tex"},
			changes: [][]string{{"edit:a.tex"}, {"rm:b.tex"}, {"rm:a.tex"}},
			want: []sum{
				{"a.tex", "a.tex", true, "0", false, "2"},
				{"b.tex", "b.tex", false, "", false, "1"},
			},
			wantRem: []sum{
				{"b.tex", "b.tex", false, "", false, "1"},
				{"a.tex", "a.tex", true, "0", false, "2"},
			},
		},
	}
	for i, c := range cases {
		res, cerr := fold(c.init, c.changes...)
		if cerr != nil {
			t.Fatalf("case %d (%s): fold err: %v", i, c.name, cerr)
		}
		got := res.Entries()
		if len(got) != len(c.want) {
			t.Fatalf("case %s: got %d entries want %d", c.name, len(got), len(c.want))
		}
		for j, e := range got {
			assertEntry(t, c.name, e, c.want[j])
		}
		if len(res.Removed()) != len(c.wantRem) {
			t.Fatalf("case %s: got %d removed want %d",
				c.name, len(res.Removed()), len(c.wantRem))
		}
		for j, e := range res.Removed() {
			assertEntry(t, c.name+" (removed)", e, c.wantRem[j])
		}
	}
}

func TestVendorKeepsFileOfMostRecentAdd(t *testing.T) {
	finalFile := "final"
	res, cerr := BuildFileTreeDiff(
		[]*Change{
			ChangeOf(&AddOp{Pathname: "a.tex", File: "first"}),
			ChangeOf(&AddOp{Pathname: "a.tex", File: finalFile}),
			ChangeOf(&MoveOp{Pathname: "a.tex", NewPathname: "b.tex"}),
		},
		&Options{InitialPathnames: &[]string{}},
	)
	if cerr != nil {
		t.Fatalf("fold err: %v", cerr)
	}
	if got := res.Get("b.tex"); got == nil || got.File != finalFile {
		t.Fatalf("want most recent add's file at b.tex, got %v", got)
	}
}

func TestVendorUnseededMoveAssumesFile(t *testing.T) {
	// vendor "assumes that a moved file existed before the window".
	res, cerr := fold(nil,
		[]string{"mv:a.tex:b.tex"},
		[]string{"mv:b.tex:c.tex"},
	)
	if cerr != nil {
		t.Fatalf("fold err: %v", cerr)
	}
	if got := res.Entries(); len(got) != 1 || chainStr(got[0]) != "a.tex b.tex c.tex" {
		t.Fatalf("unseeded move: got %d entries", len(res.Entries()))
	}
	if got := res.Get("c.tex"); got.Origin == nil || *got.Origin != "a.tex" {
		t.Fatalf("unseeded origin: got %v", got.Origin)
	}
	if len(res.Removed()) != 0 {
		t.Fatalf("want no removed, got %d", len(res.Removed()))
	}
}

func TestVendorUnseededRemoveAssumesFile(t *testing.T) {
	// vendor "assumes that a removed file existed before the window".
	res, cerr := fold(nil, []string{"rm:a.tex"})
	if cerr != nil {
		t.Fatalf("fold err: %v", cerr)
	}
	if got := res.Entries(); len(got) != 1 || got[0].DeletedAtChangeIndex == nil ||
		*got[0].DeletedAtChangeIndex != 0 {
		t.Fatalf("unseeded remove: got %d entries", len(got))
	}
	if len(res.Removed()) != 1 {
		t.Fatalf("want 1 removed, got %d", len(res.Removed()))
	}
}

func TestVendorUnseededMoveHasNoRemoved(t *testing.T) {
	// vendor "has no file to report as removed at the target of a move".
	res, cerr := fold(nil, []string{"mv:a.tex:b.tex"})
	if cerr != nil {
		t.Fatalf("fold err: %v", cerr)
	}
	if len(res.Removed()) != 0 {
		t.Fatalf("want no removed (unseeded), got %d", len(res.Removed()))
	}
	if res.Get("b.tex") == nil {
		t.Fatalf("want b.tex live")
	}
}

func TestVendorCollisionCalledWithTargetEntry(t *testing.T) {
	// vendor: onMoveCollision is called with the file at the target before it
	// is replaced.
	var targets []*Entry
	res, cerr := BuildFileTreeDiff(
		[]*Change{ChangeOf(&MoveOp{Pathname: "a.tex", NewPathname: "b.tex"})},
		&Options{
			InitialPathnames: &[]string{"a.tex", "b.tex"},
			OnMoveCollision:  func(target *Entry, op *MoveOp) error { targets = append(targets, target); return nil },
		},
	)
	if cerr != nil {
		t.Fatalf("fold err: %v", cerr)
	}
	if len(targets) != 1 || chainStr(targets[0]) != "b.tex" {
		t.Fatalf("collision: %d calls, target chain %q", len(targets), "")
	}
	if got := res.Entries(); len(got) != 1 || (got[0].Origin == nil || *got[0].Origin != "a.tex") {
		t.Fatalf("after collision-fold: %d entries", len(res.Entries()))
	}
}

func TestVendorCollisionNotCalledOnVacantTarget(t *testing.T) {
	var called bool
	_, cerr := BuildFileTreeDiff(
		[]*Change{ChangeOf(&MoveOp{Pathname: "a.tex", NewPathname: "b.tex"})},
		&Options{
			InitialPathnames: &[]string{"a.tex"},
			OnMoveCollision:  func(*Entry, *MoveOp) error { called = true; return nil },
		},
	)
	if cerr != nil {
		t.Fatalf("fold err: %v", cerr)
	}
	if called {
		t.Fatalf("collision called for vacant target")
	}
}

func TestVendorCollisionNotCalledOnRemovedTarget(t *testing.T) {
	var called bool
	_, cerr := BuildFileTreeDiff(
		[]*Change{
			ChangeOf(&MoveOp{Pathname: "b.tex", NewPathname: ""}),
			ChangeOf(&MoveOp{Pathname: "a.tex", NewPathname: "b.tex"}),
		},
		&Options{
			InitialPathnames: &[]string{"a.tex", "b.tex"},
			OnMoveCollision:  func(*Entry, *MoveOp) error { called = true; return nil },
		},
	)
	if cerr != nil {
		t.Fatalf("fold err: %v", cerr)
	}
	if called {
		t.Fatalf("collision called for removed target")
	}
}

func TestVendorCollisionCanAbortFold(t *testing.T) {
	// vendor: the callback can throw before the move is folded in. Go: the
	// non-nil return aborts the fold.
	_, cerr := BuildFileTreeDiff(
		[]*Change{ChangeOf(&MoveOp{Pathname: "a.tex", NewPathname: "b.tex"})},
		&Options{
			InitialPathnames: &[]string{"a.tex", "b.tex"},
			OnMoveCollision:  func(*Entry, *MoveOp) error { return collisionErr },
		},
	)
	if cerr == nil || cerr.Error() != "collision" {
		t.Fatalf("want collision error, got %v", cerr)
	}
}

type collisionError struct{}

func (collisionError) Error() string { return "collision" }

var collisionErr = collisionError{}
