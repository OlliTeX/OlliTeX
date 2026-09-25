package persist

import (
	"errors"
	"testing"

	"history-v1/internal/core"
)

// TestSetContentRebaseAgainstConcurrentChange — Node oracle acceptance test
// test/acceptance/js/api/set_content.test.js "returns a change that can be
// rebased on a concurrent change":
//
//  1. setContent main.tex = "one\ntwo\nthree\n", commit            -> v1
//  2. setContent main.tex = "one\ntwo\nthree\nfour\n" (build only) -> base 1
//  3. concurrent change inserts "zero\n" at the top, commit        -> v2
//  4. the built change no longer applies: commit at base 1 -> 422
//  5. rebase the built change against the concurrent change (via
//     core.RebaseChanges), commit at base 2                      -> 201
//  6. final content is "zero\none\ntwo\nthree\nfour\n"
//
// Step 5 is the acceptance test of the RebaseChanges primitive: the same
// path the API layer uses to recover a stale setContent change.
func TestSetContentRebaseAgainstConcurrentChange(t *testing.T) {
	svc, cs, _ := newHarness(t)
	pid := "aaaa00010000000000000001"
	initProject(t, cs, pid)
	limit := farFutureLimits()
	ts := scTS

	// 1. initial doc committed at v1.
	seed, err := svc.BuildSetContentChange(pid, "main.tex", BuildSetContentOpts{
		Content: strPtr("one\ntwo\nthree\n"), Timestamp: ts,
	})
	if err != nil {
		t.Fatalf("setContent (initial): %v", err)
	}
	commitSetContent(t, svc, pid, seed)
	if _, v := loadEager(t, svc, pid, "main.tex"); v != 1 {
		t.Fatalf("after step 1: version = %d, want 1", v)
	}

	// 2. build (without committing) the change that appends "four\n".
	built, err := svc.BuildSetContentChange(pid, "main.tex", BuildSetContentOpts{
		Content: strPtr("one\ntwo\nthree\nfour\n"), Timestamp: ts,
	})
	if err != nil {
		t.Fatalf("setContent (append four): %v", err)
	}
	if built.BaseVersion != 1 {
		t.Fatalf("built.BaseVersion = %d, want 1", built.BaseVersion)
	}
	if built.Change == nil || len(built.Change.Operations) != 1 {
		t.Fatalf("built change has %d ops, want 1 editFile", len(built.Change.Operations))
	}

	// 3. a concurrent change inserts "zero\n" at the top of the file.
	concurrent := core.NewChange(
		[]*core.Operation{
			core.EditFile("main.tex", core.NewTextEditOp(
				core.NewTextOp(
					core.NewInsert("zero\n", nil, nil),
					core.NewRetain(core.UTF16Length("one\ntwo\nthree\n"), nil),
				),
			)),
		},
		ts, nil, nil, nil, "", nil,
	)
	commitSetContent(t, svc, pid, &SetContentResult{Change: concurrent, BaseVersion: 1})
	if _, v := loadEager(t, svc, pid, "main.tex"); v != 2 {
		t.Fatalf("after concurrency: version = %d, want 2", v)
	}

	// 4. the built change no longer applies at its old base version.
	_, err = svc.CommitChanges(pid, []*core.Change{built.Change}, limit, 1, CommitOptions{HistoryBufferLevel: 0})
	var ceve *core.ConflictingEndVersion
	if !errors.As(err, &ceve) {
		t.Fatalf("stale commit: err = %v, want ConflictingEndVersion", err)
	}

	// 5. rebase the built change against the concurrent change and commit.
	theirs := []*core.Change{concurrent}
	rebased := core.RebaseChanges([]*core.Change{built.Change}, theirs)
	if len(rebased) != 1 {
		t.Fatalf("rebase dropped the change; want it to survive")
	}
	_, err = svc.CommitChanges(pid, rebased, limit, 2, CommitOptions{HistoryBufferLevel: 0})
	if err != nil {
		t.Fatalf("commit rebased change: %v", err)
	}

	// 6. final content is the merge of all three writes.
	file, v := loadEager(t, svc, pid, "main.tex")
	if v != 3 {
		t.Fatalf("final version = %d, want 3", v)
	}
	content, err := file.GetContent(false)
	if err != nil {
		t.Fatalf("GetContent: %v", err)
	}
	if content != "zero\none\ntwo\nthree\nfour\n" {
		t.Fatalf("final content = %q, want merged result", content)
	}
}
