package otc

// file_tree_diff_test.go — 1:1 mirror of test/unit/file_tree_diff.test.js
// (the Node oracle / acceptance spec).

import (
	"reflect"
	"testing"
	"time"
)

var ftdTS = time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)

func ftdChange(t *testing.T, ops ...Operation) *Change {
	t.Helper()
	return NewChange(ops, ftdTS, nil, nil, nil, nil, nil)
}

func ftdAddFile(t *testing.T, pathname, content string) Operation {
	t.Helper()
	f, err := FileFromString(content, nil)
	if err != nil {
		t.Fatal(err)
	}
	return OperationAddFile(pathname, f)
}

func ftdEditFile(t *testing.T, pathname string) Operation {
	t.Helper()
	return OperationEditFile(pathname, mustTextOp(t, "x"))
}

func ftdMoveFile(pathname, newPathname string) Operation {
	return OperationMoveFile(pathname, newPathname)
}

func ftdRemoveFile(pathname string) Operation { return OperationRemoveFile(pathname) }

// ftdSummary is the shape the cases are written in (Node `summary`).
type ftdSummary struct {
	Pathname                string
	Origin                  *string
	Chain                   []string
	Edited                  bool
	FirstEditedAtChainIndex *int
	HasFile                 bool
	DeletedAtChangeIndex    *int
}

func ftdSum(p string) ftdSummary {
	o := p
	return ftdSummary{Pathname: p, Origin: &o, Chain: []string{p}}
}

func (s ftdSummary) originNull() ftdSummary       { s.Origin = nil; return s }
func (s ftdSummary) origin(v string) ftdSummary   { o := v; s.Origin = &o; return s }
func (s ftdSummary) chain(c ...string) ftdSummary { s.Chain = c; return s }
func (s ftdSummary) edited(v bool) ftdSummary     { s.Edited = v; return s }
func (s ftdSummary) firstEdited(v int) ftdSummary { x := v; s.FirstEditedAtChainIndex = &x; return s }
func (s ftdSummary) hasFile(v bool) ftdSummary    { s.HasFile = v; return s }
func (s ftdSummary) deletedAt(v int) ftdSummary   { x := v; s.DeletedAtChangeIndex = &x; return s }

func ftdExpectSummary(t *testing.T, label string, got *FileTreeDiffEntry, want ftdSummary) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: entry is nil", label)
		return
	}
	if tail := got.Chain[len(got.Chain)-1]; tail != want.Pathname {
		t.Errorf("%s: chain tail %q, want %q", label, tail, want.Pathname)
	}
	if (got.Origin == nil) != (want.Origin == nil) {
		t.Errorf("%s: origin nilness %v, want %v", label, got.Origin == nil, want.Origin == nil)
	} else if got.Origin != nil && *got.Origin != *want.Origin {
		t.Errorf("%s: origin %q, want %q", label, *got.Origin, *want.Origin)
	}
	if !reflect.DeepEqual(got.Chain, want.Chain) {
		t.Errorf("%s: chain %v, want %v", label, got.Chain, want.Chain)
	}
	if got.Edited != want.Edited {
		t.Errorf("%s: edited %v, want %v", label, got.Edited, want.Edited)
	}
	if (got.FirstEditedAtChainIndex == nil) != (want.FirstEditedAtChainIndex == nil) {
		t.Errorf("%s: firstEditedAtChainIndex nilness mismatch", label)
	} else if got.FirstEditedAtChainIndex != nil && *got.FirstEditedAtChainIndex != *want.FirstEditedAtChainIndex {
		t.Errorf("%s: firstEditedAtChainIndex %d, want %d", label, *got.FirstEditedAtChainIndex, *want.FirstEditedAtChainIndex)
	}
	if (got.File != nil) != want.HasFile {
		t.Errorf("%s: hasFile %v, want %v", label, got.File != nil, want.HasFile)
	}
	if (got.DeletedAtChangeIndex == nil) != (want.DeletedAtChangeIndex == nil) {
		t.Errorf("%s: deletedAtChangeIndex nilness mismatch", label)
	} else if got.DeletedAtChangeIndex != nil && *got.DeletedAtChangeIndex != *want.DeletedAtChangeIndex {
		t.Errorf("%s: deletedAtChangeIndex %d, want %d", label, *got.DeletedAtChangeIndex, *want.DeletedAtChangeIndex)
	}
}

type ftdCase struct {
	name    string
	seeded  bool
	seeds   []string
	build   func(t *testing.T) []*Change
	entries []ftdSummary
	removed []ftdSummary
}

func TestBuildFileTreeDiff(t *testing.T) {
	cases := []ftdCase{
		{
			name:   "reports the files that the window leaves untouched",
			seeded: true, seeds: []string{"a.tex", "b.tex"},
			build:   func(t *testing.T) []*Change { return nil },
			entries: []ftdSummary{ftdSum("a.tex"), ftdSum("b.tex")},
		},
		{
			name:   "collapses a chain of moves into a single entry",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{
					ftdChange(t, ftdMoveFile("a.tex", "b.tex")),
					ftdChange(t, ftdMoveFile("b.tex", "c.tex")),
				}
			},
			entries: []ftdSummary{ftdSum("c.tex").origin("a.tex").chain("a.tex", "b.tex", "c.tex")},
		},
		{
			name:   "records the pathnames of a file that is moved and then removed",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{
					ftdChange(t, ftdMoveFile("a.tex", "b.tex")),
					ftdChange(t, ftdRemoveFile("b.tex")),
				}
			},
			entries: []ftdSummary{ftdSum("b.tex").origin("a.tex").chain("a.tex", "b.tex").deletedAt(1)},
			removed: []ftdSummary{ftdSum("b.tex").origin("a.tex").chain("a.tex", "b.tex").deletedAt(1)},
		},
		{
			name:   "reports a file that is added and then moved at its new pathname",
			seeded: true, seeds: []string{},
			build: func(t *testing.T) []*Change {
				return []*Change{
					ftdChange(t, ftdAddFile(t, "a.tex", "content")),
					ftdChange(t, ftdMoveFile("a.tex", "b.tex")),
				}
			},
			entries: []ftdSummary{ftdSum("b.tex").originNull().chain("a.tex", "b.tex").hasFile(true)},
		},
		{
			name:   "reports a file that is added and then removed as removed",
			seeded: true, seeds: []string{},
			build: func(t *testing.T) []*Change {
				return []*Change{
					ftdChange(t, ftdAddFile(t, "a.tex", "content")),
					ftdChange(t, ftdRemoveFile("a.tex")),
				}
			},
			entries: []ftdSummary{ftdSum("a.tex").originNull().hasFile(true).deletedAt(1)},
			removed: []ftdSummary{ftdSum("a.tex").originNull().hasFile(true).deletedAt(1)},
		},
		{
			name:   "reports a file that is removed and then added again as added",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{
					ftdChange(t, ftdRemoveFile("a.tex")),
					ftdChange(t, ftdAddFile(t, "a.tex", "content")),
				}
			},
			entries: []ftdSummary{ftdSum("a.tex").originNull().hasFile(true)},
			removed: []ftdSummary{ftdSum("a.tex").deletedAt(0)},
		},
		{
			name:   "reports a file that another file is moved onto as removed",
			seeded: true, seeds: []string{"a.tex", "b.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{ftdChange(t, ftdMoveFile("a.tex", "b.tex"))}
			},
			entries: []ftdSummary{ftdSum("b.tex").origin("a.tex").chain("a.tex", "b.tex")},
			removed: []ftdSummary{ftdSum("b.tex").deletedAt(0)},
		},
		{
			name:   "reports a file that another file is added over as removed",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{ftdChange(t, ftdAddFile(t, "a.tex", "content"))}
			},
			entries: []ftdSummary{ftdSum("a.tex").originNull().hasFile(true)},
			removed: []ftdSummary{ftdSum("a.tex").deletedAt(0)},
		},
		{
			name:   "reports a file that is moved onto a pathname removed earlier",
			seeded: true, seeds: []string{"a.tex", "b.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{
					ftdChange(t, ftdRemoveFile("a.tex")),
					ftdChange(t, ftdMoveFile("b.tex", "a.tex")),
				}
			},
			entries: []ftdSummary{ftdSum("a.tex").origin("b.tex").chain("b.tex", "a.tex")},
			removed: []ftdSummary{ftdSum("a.tex").deletedAt(0)},
		},
		{
			name:   "records an edit of a file that is moved afterwards",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{
					ftdChange(t, ftdEditFile(t, "a.tex")),
					ftdChange(t, ftdMoveFile("a.tex", "b.tex")),
				}
			},
			entries: []ftdSummary{ftdSum("b.tex").origin("a.tex").chain("a.tex", "b.tex").edited(true).firstEdited(0)},
		},
		{
			name:   "records an edit of a file that was moved before",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{
					ftdChange(t, ftdMoveFile("a.tex", "b.tex")),
					ftdChange(t, ftdEditFile(t, "b.tex")),
				}
			},
			entries: []ftdSummary{ftdSum("b.tex").origin("a.tex").chain("a.tex", "b.tex").edited(true).firstEdited(1)},
		},
		{
			name:   "records the first of several edits",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{
					ftdChange(t, ftdEditFile(t, "a.tex")),
					ftdChange(t, ftdMoveFile("a.tex", "b.tex")),
					ftdChange(t, ftdEditFile(t, "b.tex")),
				}
			},
			entries: []ftdSummary{ftdSum("b.tex").origin("a.tex").chain("a.tex", "b.tex").edited(true).firstEdited(0)},
		},
		{
			name:   "creates an entry for an edit of a pathname it has not seen",
			seeded: true, seeds: []string{},
			build: func(t *testing.T) []*Change {
				return []*Change{ftdChange(t, ftdEditFile(t, "a.tex"))}
			},
			entries: []ftdSummary{ftdSum("a.tex").edited(true).firstEdited(0)},
		},
		{
			name:   "ignores an edit of a removed file",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{
					ftdChange(t, ftdRemoveFile("a.tex")),
					ftdChange(t, ftdEditFile(t, "a.tex")),
				}
			},
			entries: []ftdSummary{ftdSum("a.tex").deletedAt(0)},
			removed: []ftdSummary{ftdSum("a.tex").deletedAt(0)},
		},
		{
			name:   "resolves a swap of two files by their pathnames",
			seeded: true, seeds: []string{"a.tex", "b.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{
					ftdChange(t, ftdMoveFile("a.tex", "tmp.tex")),
					ftdChange(t, ftdMoveFile("b.tex", "a.tex")),
					ftdChange(t, ftdMoveFile("tmp.tex", "b.tex")),
				}
			},
			entries: []ftdSummary{
				ftdSum("a.tex").origin("b.tex").chain("b.tex", "a.tex"),
				ftdSum("b.tex").origin("a.tex").chain("a.tex", "tmp.tex", "b.tex"),
			},
		},
		{
			name:   "skips a move of a pathname that holds no file",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{ftdChange(t, ftdMoveFile("missing.tex", "b.tex"))}
			},
			entries: []ftdSummary{ftdSum("a.tex")},
		},
		{
			name:   "skips a removal of a pathname that holds no file",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{ftdChange(t, ftdRemoveFile("missing.tex"))}
			},
			entries: []ftdSummary{ftdSum("a.tex")},
		},
		{
			name:   "skips operations without a pathname",
			seeded: true, seeds: []string{},
			build: func(t *testing.T) []*Change {
				return []*Change{ftdChange(t,
					ftdEditFile(t, ""),
					ftdMoveFile("", "a.tex"),
					ftdRemoveFile(""),
					ftdAddFile(t, "", "content"),
				)}
			},
		},
		{
			name:   "ignores operations that leave the file tree unchanged",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{ftdChange(t,
					OperationSetFileMetadata("a.tex", map[string]any{"main": true}),
					ftdMoveFile("a.tex", "a.tex"),
					OperationNoOp,
				)}
			},
			entries: []ftdSummary{ftdSum("a.tex")},
		},
		{
			name:   "folds the operations of a single change in order",
			seeded: true, seeds: []string{"a.tex"},
			build: func(t *testing.T) []*Change {
				return []*Change{ftdChange(t,
					ftdMoveFile("a.tex", "b.tex"),
					ftdAddFile(t, "a.tex", "content"),
					ftdEditFile(t, "b.tex"),
				)}
			},
			entries: []ftdSummary{
				ftdSum("b.tex").origin("a.tex").chain("a.tex", "b.tex").edited(true).firstEdited(1),
				ftdSum("a.tex").originNull().hasFile(true),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changes := tc.build(t)
			opts := FileTreeDiffOptions{}
			if tc.seeded {
				opts.InitialPathnames = tc.seeds
			}
			result := BuildFileTreeDiff(changes, opts)

			gotEntries := result.OrderedEntries()
			if len(gotEntries) != len(tc.entries) {
				t.Fatalf("entries count = %d, want %d", len(gotEntries), len(tc.entries))
			}
			for i, want := range tc.entries {
				ftdExpectSummary(t, "entries", gotEntries[i], want)
			}

			if len(result.Removed) != len(tc.removed) {
				t.Fatalf("removed count = %d, want %d", len(result.Removed), len(tc.removed))
			}
			for i, want := range tc.removed {
				ftdExpectSummary(t, "removed", result.Removed[i], want)
			}
		})
	}
}

// onMoveCollision is called before a move replaces the file at its target.
func TestBuildFileTreeDiff_MoveCollision(t *testing.T) {
	called := 0
	collided := ""
	opts := FileTreeDiffOptions{
		InitialPathnames: []string{"a.tex", "b.tex"},
		OnMoveCollision: func(entry *FileTreeDiffEntry, op *MoveFileOperation) {
			called++
			collided = entry.Chain[len(entry.Chain)-1]
		},
	}
	changes := []*Change{ftdChange(t, ftdMoveFile("a.tex", "b.tex"))}
	_ = BuildFileTreeDiff(changes, opts)
	if called != 1 {
		t.Errorf("onMoveCollision called %d times, want 1", called)
	}
	if collided != "b.tex" {
		t.Errorf("collided = %q, want b.tex", collided)
	}
}
