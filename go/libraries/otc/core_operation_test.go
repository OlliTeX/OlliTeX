package otc

import (
	"context"
	"encoding/json"
	"testing"
)

// realBlobStore is an in-memory BlobStore that produces valid 40-char git
// blob hashes, so stored content can round-trip through File.fromRaw.
func realBlobStore() *BaseBlobStore {
	return &BaseBlobStore{
		PutStringFn: func(_ context.Context, content string) (*Blob, error) {
			n := int64(len(content))
			return &Blob{Hash: BlobHashFromString(content), ByteLength: n, StringLength: int64Ptr(len(content))}, nil
		},
		PutObjectFn: func(_ context.Context, obj map[string]any) (*Blob, error) {
			b, _ := json.Marshal(obj)
			return &Blob{Hash: BlobHashFromBuffer(b), ByteLength: int64(len(b))}, nil
		},
	}
}

func fileOp(t *testing.T, pathname, content string) *AddFileOperation {
	t.Helper()
	f, err := FileFromString(content, nil)
	if err != nil {
		t.Fatal(err)
	}
	op, err := NewAddFileOperation(pathname, f)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func isOpNoOp(t *testing.T, label string, op Operation) {
	t.Helper()
	if !op.IsNoOp() {
		t.Fatalf("%s: expected no-op, got %T", label, op)
	}
}
func expectType(t *testing.T, label string, op Operation, want string) {
	t.Helper()
	if op == nil {
		t.Fatalf("%s: nil op", label)
	}
	name := ""
	switch op.(type) {
	case *AddFileOperation:
		name = "AddFile"
	case *MoveFileOperation:
		name = "MoveFile"
	case *EditFileOperation:
		name = "EditFile"
	case *SetFileMetadataOperation:
		name = "SetFileMetadata"
	case *NoOperation:
		name = "NoOperation"
	}
	if name != want {
		t.Fatalf("%s: want %s got %s (%T)", label, want, name, op)
	}
}

func TestOperationTransformAddAdd(t *testing.T) {
	a := fileOp(t, "main.tex", "ours")
	b := fileOp(t, "main.tex", "theirs")
	pr := OperationTransform(a, b)
	isOpNoOp(t, "a'", pr[0])
	expectType(t, "b'", pr[1], "AddFile")

	ca := fileOp(t, "a.tex", "1")
	cb := fileOp(t, "b.tex", "2")
	pr2 := OperationTransform(ca, cb)
	expectType(t, "a2'", pr2[0], "AddFile")
	expectType(t, "b2'", pr2[1], "AddFile")
}

func TestOperationTransformAddMove(t *testing.T) {
	// add at the moved-from path: both relocate to new path
	add := fileOp(t, "old.tex", "c")
	move := OperationMoveFile("old.tex", "new.tex")
	pr := OperationTransform(add, move)
	expectType(t, "add'", pr[0], "AddFile")
	if af, ok := pr[0].(*AddFileOperation); !ok || af.Pathname != "new.tex" {
		t.Fatalf("add' = %T %v", pr[0], pr[0])
	}
	expectType(t, "move'", pr[1], "MoveFile")

	// no overlap
	add2 := fileOp(t, "other.tex", "c")
	pr2 := OperationTransform(add2, move)
	expectType(t, "add2'", pr2[0], "AddFile")
	expectType(t, "move2'", pr2[1], "MoveFile")
}

func TestOperationTransformMoveMove(t *testing.T) {
	// same move -> both no-op
	m1 := OperationMoveFile("a", "b")
	m2 := OperationMoveFile("a", "b")
	pr := OperationTransform(m1, m2)
	isOpNoOp(t, "m1'", pr[0])
	isOpNoOp(t, "m2'", pr[1])

	// divergent moves: both from a to different targets -> both dropped (conflict)
	d1 := OperationMoveFile("a", "x")
	d2 := OperationMoveFile("a", "y")
	pr2 := OperationTransform(d1, d2)
	isOpNoOp(t, "d1'", pr2[0])
	// d2' should be the "loser" move a->y
	expectType(t, "d2'", pr2[1], "MoveFile")

	// no overlap
	n1 := OperationMoveFile("p", "q")
	n2 := OperationMoveFile("r", "s")
	pr3 := OperationTransform(n1, n2)
	expectType(t, "n1'", pr3[0], "MoveFile")
	expectType(t, "n2'", pr3[1], "MoveFile")
}

func TestOperationTransformMoveEdit(t *testing.T) {
	// edit the moved-from file: edit re-homed to new path
	move := OperationMoveFile("a", "b")
	edit := OperationEditFile("a", NewTextEdit(NewTextOperation()))
	pr := OperationTransform(move, edit)
	expectType(t, "move'", pr[0], "MoveFile")
	if ef, ok := pr[1].(*EditFileOperation); !ok || ef.Pathname != "b" {
		t.Fatalf("edit' = %T %v", pr[1], pr[1])
	}

	// remove then edit same path -> edit dropped
	rm := OperationMoveFile("a", "")
	pr2 := OperationTransform(rm, edit)
	expectType(t, "remove'", pr2[0], "MoveFile")
	isOpNoOp(t, "edit'", pr2[1])
}

func TestOperationTransformEditEdit(t *testing.T) {
	e1 := OperationEditFile("f", NewTextEdit(NewTextOperation()))
	e2 := OperationEditFile("f", NewTextEdit(NewTextOperation()))
	pr := OperationTransform(e1, e2)
	expectType(t, "e1'", pr[0], "EditFile")
	expectType(t, "e2'", pr[1], "EditFile")

	// different files untouched
	e3 := OperationEditFile("x", NewTextEdit(NewTextOperation()))
	pr2 := OperationTransform(e1, e3)
	expectType(t, "e12'", pr2[0], "EditFile")
	expectType(t, "e3'", pr2[1], "EditFile")
}

func TestOperationTransformSetSet(t *testing.T) {
	s1 := OperationSetFileMetadata("f", map[string]any{"a": 1})
	s2 := OperationSetFileMetadata("f", map[string]any{"b": 2})
	pr := OperationTransform(s1, s2)
	isOpNoOp(t, "s1'", pr[0])
	expectType(t, "s2'", pr[1], "SetFileMetadata")

	s3 := OperationSetFileMetadata("g", map[string]any{"a": 1})
	pr2 := OperationTransform(s1, s3)
	expectType(t, "s1b'", pr2[0], "SetFileMetadata")
	expectType(t, "s3'", pr2[1], "SetFileMetadata")
}

func TestOperationTransformSymmetry(t *testing.T) {
	// a=move, b=add (transpose path)
	add := fileOp(t, "old.tex", "c")
	move := OperationMoveFile("old.tex", "new.tex")
	pr := OperationTransform(move, add)
	// pr[0] is move' (relocated add handled), pr[1] is add'
	expectType(t, "move'", pr[0], "MoveFile")
	if af, ok := pr[1].(*AddFileOperation); !ok || af.Pathname != "new.tex" {
		t.Fatalf("add' = %T %v", pr[1], pr[1])
	}
}

func TestOperationApplyToSnapshot(t *testing.T) {
	s := NewSnapshot(nil, nil, nil, nil)

	// AddFile
	if err := fileOp(t, "a.txt", "hello").ApplyTo(s); err != nil {
		t.Fatal(err)
	}
	if s.GetFile("a.txt") == nil || *s.GetFile("a.txt").GetContent(false) != "hello" {
		t.Fatal("add didn't apply")
	}

	// MoveFile
	if err := fileOp(t, "b.txt", "x").ApplyTo(s); err != nil {
		t.Fatal(err)
	}
	mv := OperationMoveFile("b.txt", "c.txt")
	if err := mv.ApplyTo(s); err != nil {
		t.Fatal(err)
	}
	if s.GetFile("b.txt") != nil || s.GetFile("c.txt") == nil {
		t.Fatal("move didn't apply")
	}

	// RemoveFile
	rm := OperationRemoveFile("c.txt")
	if err := rm.ApplyTo(s); err != nil {
		t.Fatal(err)
	}
	if s.GetFile("c.txt") != nil {
		t.Fatal("remove didn't apply")
	}

	// SetFileMetadata
	if err := fileOp(t, "d.txt", "y").ApplyTo(s); err != nil {
		t.Fatal(err)
	}
	set := OperationSetFileMetadata("d.txt", map[string]any{"main": true})
	if err := set.ApplyTo(s); err != nil {
		t.Fatal(err)
	}
	main, _ := s.GetFile("d.txt").GetMetadata()["main"].(bool)
	if !main {
		t.Fatal("set metadata didn't apply")
	}

	// NoOperation
	if err := OperationNoOp.ApplyTo(s); err != nil {
		t.Fatal(err)
	}
}

func TestOperationRawRoundTrip(t *testing.T) {
	// AddFile
	a := fileOp(t, "p.txt", "zz")
	raw, err := a.Store(realBlobStore())
	if err != nil {
		t.Fatal(err)
	}
	back, err := OperationFromRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	if af, ok := back.(*AddFileOperation); !ok || af.Pathname != "p.txt" {
		t.Fatalf("roundtrip add = %T", back)
	}

	// MoveFile
	m := OperationMoveFile("a", "b")
	rawM := m.ToRaw()
	backM, err := OperationFromRaw(rawM)
	if err != nil {
		t.Fatal(err)
	}
	if af, ok := backM.(*MoveFileOperation); !ok || af.NewPathname != "b" {
		t.Fatalf("roundtrip move = %T", backM)
	}

	// SetFileMetadata
	st := OperationSetFileMetadata("q", map[string]any{"k": "v"})
	backS, err := OperationFromRaw(st.ToRaw())
	if err != nil {
		t.Fatal(err)
	}
	if sf, ok := backS.(*SetFileMetadataOperation); !ok || sf.Metadata["k"] != "v" {
		t.Fatalf("roundtrip set = %T", backS)
	}

	// NoOperation
	rawN := OperationNoOp.ToRaw()
	backN, err := OperationFromRaw(rawN)
	if err != nil {
		t.Fatal(err)
	}
	if !backN.IsNoOp() {
		t.Fatal("roundtrip noop")
	}
}
