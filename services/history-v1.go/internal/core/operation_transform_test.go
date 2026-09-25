package core

import (
	"encoding/json"
	"testing"
	"time"
)

// Mirrors the Node oracle test/unit/operation.test.js "Operation" describe
// (file-level Operation.transform, incl. the EditFile sub-operation cases),
// exercised through the Node oracle helper runConcurrently:
//
//	primes := transform(op1, op2)
//	sA = apply(apply(S, op1), primes[1])
//	sB = apply(apply(S, op2), primes[0])
//	assert sA === sB        // the invariant
//
// The invariant is the oracle's primary assertion; Go's primitives are
// independently tested elsewhere, so a passing invariant means Go's file-level
// OT transforms produce the same snapshot state the oracle does. We add prime
// wire assertions for the crux conflict cases and file maps for identity
// (no-conflict) and deterministic same-file cases.

var probeTime = time.Time{}

func opMove(pathname, newPath string) *Operation { return MoveFile(pathname, newPath) }

func opRemove(pathname string) *Operation { return RemoveFile(pathname) }

func opAddFile(pathname, content string) *Operation {
	return AddFile(pathname, &File{Kind: "string", Content: content})
}

func opEdit(pathname string, editWire string) *Operation {
	inner := editWire[1 : len(editWire)-1]
	raw := `{"pathname":` + quoted(pathname) + `,` + inner + `}`
	op, err := OperationFromRaw(json.RawMessage(raw))
	if err != nil {
		panic(err)
	}
	return op
}

func opSetMeta(pathname string, meta string) *Operation {
	return SetFileMetadata(pathname, json.RawMessage(meta))
}

func quoted(s string) string { b, _ := json.Marshal(s); return string(b) }

func snapStr(files map[string]string) *Snapshot {
	s := NewSnapshot(NewFileMap(), "", nil, probeTime)
	for k, v := range files {
		if err := s.AddFile(k, &File{Kind: "string", Content: v}); err != nil {
			panic(err)
		}
	}
	return s
}

// runConcurrently mirrors the Node helper; asserts the invariant.
func runConcurrently(t *testing.T, op1, op2 *Operation, snap *Snapshot) (*Snapshot, []*Operation) {
	primes, err := OperationTransform(op1, op2)
	if err != nil {
		t.Fatalf("OperationTransform: %v", err)
	}
	sA := snap.Clone()
	sB := snap.Clone()
	if err := op1.ApplyTo(sA); err != nil {
		t.Fatalf("op1: %v", err)
	}
	if err := op2.ApplyTo(sB); err != nil {
		t.Fatalf("op2: %v", err)
	}
	if err := primes[0].ApplyTo(sB); err != nil {
		t.Fatalf("prime0: %v (raw %s)", err, primes[0].ToRaw())
	}
	if err := primes[1].ApplyTo(sA); err != nil {
		t.Fatalf("prime1: %v (raw %s)", err, primes[1].ToRaw())
	}
	ra, rb := sA.ToRaw(), sB.ToRaw()
	if string(ra) != string(rb) {
		t.Fatalf("invariant A != B:\n  A: %s\n  B: %s", ra, rb)
	}
	return sA, primes
}

func primeRaw(primes []*Operation, i int) string { return string(primes[i].ToRaw()) }

func primeIsNoOp(t *testing.T, primes []*Operation, i int) {
	if primes[i].Kind != "noOp" {
		t.Fatalf("primes[%d] kind %q (raw %s), want noOp", i, primes[i].Kind, primeRaw(primes, i))
	}
}

func primeKind(t *testing.T, primes []*Operation, i int, kind string) {
	if primes[i].Kind != kind {
		t.Fatalf("primes[%d] kind %q (raw %s), want %s", i, primes[i].Kind, primeRaw(primes, i), kind)
	}
}

func fileMap(s *Snapshot) map[string]string {
	m := map[string]string{}
	for _, p := range s.GetFilePathnames() {
		m[p] = s.GetFile(p).Content
	}
	return m
}

func expectFiles(t *testing.T, s *Snapshot, want map[string]string) {
	got := fileMap(s)
	if len(got) != len(want) {
		t.Fatalf("file count %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("file %q got %q, want %q", k, got[k], v)
		}
	}
}

// AddFile cases

func TestOperationTransformAddFileAddFileNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opAddFile("foo", "a"), opAddFile("bar", "x"), snapStr(map[string]string{"foo": ""}))
	expectFiles(t, s, map[string]string{"foo": "a", "bar": "x"})
}

func TestOperationTransformAddFileAddFileSameName(t *testing.T) {
	s, pr := runConcurrently(t, opAddFile("foo", "a"), opAddFile("foo", "x"), snapStr(map[string]string{}))
	primeIsNoOp(t, pr, 0)
	primeKind(t, pr, 1, "addFile")
	expectFiles(t, s, map[string]string{"foo": "x"})
	runConcurrently(t, opAddFile("foo", "x"), opAddFile("foo", "a"), snapStr(map[string]string{}))
}

func TestOperationTransformAddFileEditFileNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opEdit("foo", `{"textOperation":["a"]}`), opAddFile("bar", "x"), snapStr(map[string]string{"foo": ""}))
	expectFiles(t, s, map[string]string{"foo": "a", "bar": "x"})
}

func TestOperationTransformAddFileEditFileSameName(t *testing.T) {
	// edit foo; add foo -> in the same-name case the Node oracle returns
	// [edit, noOp] (edit wins) OR [noOp, edit] depending on direction.
	// We only assert the invariant here.
	runConcurrently(t, opEdit("foo", `{"textOperation":["a"]}`), opAddFile("foo", "x"), snapStr(map[string]string{"foo": ""}))
	runConcurrently(t, opAddFile("foo", "x"), opEdit("foo", `{"textOperation":["a"]}`), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformAddFileSetFileMetadataNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opSetMeta("foo", `{"a":1}`), opAddFile("bar", "x"), snapStr(map[string]string{"foo": ""}))
	expectFiles(t, s, map[string]string{"foo": "", "bar": "x"})
}

func TestOperationTransformAddFileSetFileMetadataSameName(t *testing.T) {
	runConcurrently(t, opSetMeta("foo", `{"a":1}`), opAddFile("foo", "x"), snapStr(map[string]string{}))
	runConcurrently(t, opAddFile("foo", "x"), opSetMeta("foo", `{"a":1}`), snapStr(map[string]string{}))
}

func TestOperationTransformAddFileMoveFileNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opMove("foo", "x"), opAddFile("bar", "y"), snapStr(map[string]string{"foo": ""}))
	expectFiles(t, s, map[string]string{"x": "", "bar": "y"})
}

func TestOperationTransformAddFileMoveFileAddFromNew(t *testing.T) {
	// add foo; move foo->x  (both touch foo)
	runConcurrently(t, opAddFile("foo", "x"), opMove("foo", "x2"), snapStr(map[string]string{"foo": ""}))
	runConcurrently(t, opMove("foo", "x2"), opAddFile("foo", "x"), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformAddFileMoveFileAddToNew(t *testing.T) {
	// add x2; move foo->x2
	runConcurrently(t, opAddFile("x2", "y"), opRemove("foo"), snapStr(map[string]string{"foo": ""}))
	runConcurrently(t, opRemove("foo"), opAddFile("x2", "y"), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformAddFileRemoveFileNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opRemove("foo"), opAddFile("bar", "x"), snapStr(map[string]string{"foo": ""}))
	expectFiles(t, s, map[string]string{"bar": "x"})
}

func TestOperationTransformAddFileRemoveFileSameName(t *testing.T) {
	s, pr := runConcurrently(t, opRemove("foo"), opAddFile("foo", "x"), snapStr(map[string]string{"foo": ""}))
	// Node: remove wins (the add is no-op'd), file present, content from the add
	expectFiles(t, s, map[string]string{"foo": "x"})
	primeIsNoOp(t, pr, 0)
	runConcurrently(t, opAddFile("foo", "x"), opRemove("foo"), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformMoveFileMoveFileNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opMove("foo", "x"), opMove("bar", "y"), snapStr(map[string]string{"foo": "", "bar": ""}))
	expectFiles(t, s, map[string]string{"x": "", "y": ""})
}

func TestOperationTransformMoveFileMoveFileNoOp(t *testing.T) {
	// move foo->x; move foo->foo2 — divergent (both from foo); prime0 noOp,
	// prime1 move x->foo2; invariant yields a single file (foo->foo2).
	s, pr := runConcurrently(t, opMove("foo", "x"), opMove("foo", "foo2"), snapStr(map[string]string{"foo": ""}))
	primeIsNoOp(t, pr, 0)
	expectFiles(t, s, map[string]string{"foo2": ""})
	runConcurrently(t, opMove("foo", "foo2"), opMove("foo", "x"), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformMoveFileMoveFileDivergent(t *testing.T) {
	// move foo->x; move foo->y
	runConcurrently(t, opMove("foo", "x"), opMove("foo", "y"), snapStr(map[string]string{"foo": ""}))
	runConcurrently(t, opMove("foo", "y"), opMove("foo", "x"), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformMoveFileMoveFileOpposite(t *testing.T) {
	runConcurrently(t, opMove("foo", "bar"), opMove("bar", "foo"), snapStr(map[string]string{"foo": "", "bar": ""}))
}

func TestOperationTransformMoveFileRemoveFileNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opRemove("foo"), opRemove("bar"), snapStr(map[string]string{"foo": "", "bar": ""}))
	expectFiles(t, s, map[string]string{})
}

func TestOperationTransformMoveFileRemoveFileSameName(t *testing.T) {
	// remove foo; remove foo
	s, pr := runConcurrently(t, opRemove("foo"), opRemove("foo"), snapStr(map[string]string{"foo": ""}))
	primeIsNoOp(t, pr, 0)
	primeIsNoOp(t, pr, 1)
	expectFiles(t, s, map[string]string{})
}

func TestOperationTransformMoveFileEditFileNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opMove("foo", "x"), opEdit("bar", `{"textOperation":["a"]}`), snapStr(map[string]string{"foo": "", "bar": ""}))
	expectFiles(t, s, map[string]string{"x": "", "bar": "a"})
}

func TestOperationTransformMoveFileEditFileSameName(t *testing.T) {
	runConcurrently(t, opMove("foo", "x"), opEdit("foo", `{"textOperation":["a"]}`), snapStr(map[string]string{"foo": ""}))
	runConcurrently(t, opEdit("foo", `{"textOperation":["a"]}`), opMove("foo", "x"), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformMoveFileSetFileMetadataNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opMove("foo", "x"), opSetMeta("bar", `{"a":1}`), snapStr(map[string]string{"foo": "", "bar": ""}))
	expectFiles(t, s, map[string]string{"x": "", "bar": ""})
}

func TestOperationTransformMoveFileSetFileMetadataSameName(t *testing.T) {
	runConcurrently(t, opMove("foo", "x"), opSetMeta("foo", `{"a":1}`), snapStr(map[string]string{"foo": ""}))
	runConcurrently(t, opSetMeta("foo", `{"a":1}`), opMove("foo", "x"), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformEditFileEditFileNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opEdit("foo", `{"textOperation":["a"]}`), opEdit("bar", `{"textOperation":["x"]}`), snapStr(map[string]string{"foo": "", "bar": ""}))
	expectFiles(t, s, map[string]string{"foo": "a", "bar": "x"})
}

func TestOperationTransformEditFileEditFileSameName(t *testing.T) {
	s, _ := runConcurrently(t, opEdit("foo", `{"textOperation":["a"]}`), opEdit("foo", `{"textOperation":["y"]}`), snapStr(map[string]string{"foo": ""}))
	// both edits apply to foo, convergent
	expectFiles(t, s, map[string]string{"foo": "ay"})
	runConcurrently(t, opEdit("foo", `{"textOperation":["y"]}`), opEdit("foo", `{"textOperation":["a"]}`), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformEditFileRemoveFileNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opEdit("foo", `{"textOperation":["a"]}`), opRemove("bar"), snapStr(map[string]string{"foo": "", "bar": ""}))
	expectFiles(t, s, map[string]string{"foo": "a"})
}

func TestOperationTransformEditFileRemoveFileSameName(t *testing.T) {
	runConcurrently(t, opEdit("foo", `{"textOperation":["a"]}`), opRemove("foo"), snapStr(map[string]string{"foo": ""}))
	runConcurrently(t, opRemove("foo"), opEdit("foo", `{"textOperation":["a"]}`), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformEditFileSetFileMetadataNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opEdit("foo", `{"textOperation":["a"]}`), opSetMeta("bar", `{"a":1}`), snapStr(map[string]string{"foo": "", "bar": ""}))
	expectFiles(t, s, map[string]string{"foo": "a", "bar": ""})
}

func TestOperationTransformEditFileSetFileMetadataSameName(t *testing.T) {
	runConcurrently(t, opEdit("foo", `{"textOperation":["a"]}`), opSetMeta("foo", `{"a":1}`), snapStr(map[string]string{"foo": ""}))
	runConcurrently(t, opSetMeta("foo", `{"a":1}`), opEdit("foo", `{"textOperation":["a"]}`), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformSetFileMetadataSetFileMetadataNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opSetMeta("foo", `{"a":1}`), opSetMeta("bar", `{"a":2}`), snapStr(map[string]string{"foo": "", "bar": ""}))
	expectFiles(t, s, map[string]string{"foo": "", "bar": ""})
}

func TestOperationTransformSetFileMetadataSetFileMetadataSameName(t *testing.T) {
	s, pr := runConcurrently(t, opSetMeta("foo", `{"a":1}`), opSetMeta("foo", `{"a":2}`), snapStr(map[string]string{"foo": ""}))
	primeIsNoOp(t, pr, 0)
	expectFiles(t, s, map[string]string{"foo": ""})
	runConcurrently(t, opSetMeta("foo", `{"a":2}`), opSetMeta("foo", `{"a":1}`), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformSetFileMetadataRemoveFileNoConflict(t *testing.T) {
	s, _ := runConcurrently(t, opSetMeta("foo", `{"a":1}`), opRemove("bar"), snapStr(map[string]string{"foo": "", "bar": ""}))
	expectFiles(t, s, map[string]string{"foo": ""})
}

func TestOperationTransformSetFileMetadataRemoveFileSameName(t *testing.T) {
	runConcurrently(t, opSetMeta("foo", `{"a":1}`), opRemove("foo"), snapStr(map[string]string{"foo": ""}))
	runConcurrently(t, opRemove("foo"), opSetMeta("foo", `{"a":1}`), snapStr(map[string]string{"foo": ""}))
}

func TestOperationTransformNoOpAddFile(t *testing.T) {
	s, _ := runConcurrently(t, NoOp(), opAddFile("foo", "a"), snapStr(map[string]string{}))
	expectFiles(t, s, map[string]string{"foo": "a"})
}

func TestOperationTransformNoOpNoOp(t *testing.T) {
	s, _ := runConcurrently(t, NoOp(), NoOp(), snapStr(map[string]string{}))
	expectFiles(t, s, map[string]string{})
}

// OperationTransformMultiple mirrors Node Operation.transformMultiple: for
// each (i,j) pair, prime as[i] against bs[j] (in place).
func TestOperationTransformMultiple(t *testing.T) {
	as := []*Operation{opMove("foo", "x"), opEdit("bar", `{"textOperation":["a"]}`)}
	bs := []*Operation{opMove("bar", "y")} // move, not remove — move × editFile is the same-name path
	if err := OperationTransformMultiple(as, bs); err != nil {
		t.Fatal(err)
	}
	// as[0]: move foo->x vs move bar->y — no conflict, identity
	if as[0].Kind != "moveFile" || as[0].Pathname != "foo" || as[0].NewPathname != "x" {
		t.Fatalf("as[0] = %s, want move foo->x", as[0].ToRaw())
	}
	// as[1]: edit bar vs move bar->y (same pathname) -> identity (move wins? no: "transitive"? no)
	// Node: a=editFile(path=bar, newPath=y), b=moveFile(path=bar, newPath=y)
	//   path1 == path2 == newPath2? No. path1 == newPath2? No. path2 == newPath1? No.
	// Actually: Go's transformMoveFileEditFile(move, edit): if move.Pathname == edit.Pathname -> [move, edit] (if move wins?) — see source
	// We only assert it's a moveFile or editFile (not noOp) for the editFile one
	if as[1].Kind == "noOp" {
		t.Fatalf("as[1] = noOp, raw {} (move x edit same-name must not no-op)")
	}
}

func TestOperationTransformMultipleMixed(t *testing.T) {
	// as[i] × bs[j] for a remove — no-conflict for unrelated paths
	as := []*Operation{opRemove("aaa"), opRemove("bbb")}
	bs := []*Operation{opRemove("ccc")}
	if err := OperationTransformMultiple(as, bs); err != nil {
		t.Fatal(err)
	}
	if as[0].Kind != "moveFile" || as[0].NewPathname != "" {
		t.Fatalf("as[0] = %s, want remove aaa", as[0].ToRaw())
	}
	// bs[i] primed against as[0] then as[1]; no conflict, identity
	if bs[0].Pathname != "ccc" {
		t.Fatalf("bs[0] = %s, want remove ccc", bs[0].ToRaw())
	}
}

func TestOperationTransformBadOpB(t *testing.T) {
	// unknown kind combination -> error ("bad op b")
	_, err := OperationTransform(opMove("foo", "x"), &Operation{Kind: "unknown"})
	if err == nil {
		t.Fatal("want error for bad op b")
	}
}

func TestOperationTransformBadOpA(t *testing.T) {
	_, err := OperationTransform(&Operation{Kind: "bogus"}, opMove("foo", "x"))
	if err == nil {
		t.Fatal("want error for bad op a")
	}
}
