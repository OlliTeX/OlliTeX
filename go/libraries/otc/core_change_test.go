package otc

import (
	"testing"
	"time"
)

func textOpOf(t *testing.T, retain int, insert string) *TextOperation {
	t.Helper()
	op := NewTextOperation()
	if err := op.Retain(retain, RetainBuilderOpts{}); err != nil {
		t.Fatal(err)
	}
	if insert != "" {
		if err := op.Insert(insert, InsertBuilderOpts{}); err != nil {
			t.Fatal(err)
		}
	}
	return op
}

func changeFromOps(t *testing.T, ops []Operation, ts time.Time, origin OriginIface, v2 []string) *Change {
	t.Helper()
	return NewChange(ops, ts, nil, origin, v2, nil, nil)
}

func TestChangeFindBlobHashes(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339Nano, "2015-03-05T12:03:53.035Z")
	c, err := ChangeFromRaw(map[string]any{
		"operations": []any{},
		"timestamp":  "2015-03-05T12:03:53.035Z",
		"authors":    []any{nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = ts
	hashes := map[string]bool{}
	c.FindBlobHashes(hashes)
	if len(hashes) != 0 {
		t.Fatalf("expected 0, got %v", hashes)
	}

	aFile, _ := FileFromString("a", nil)
	c.PushOperation(OperationAddFile("a.txt", aFile))
	c.FindBlobHashes(hashes)
	if len(hashes) != 0 {
		t.Fatalf("expected 0 (content has no hash), got %v", hashes)
	}

	bFile, _ := FileFromHash(FileEmptyHash, nil, nil)
	c.PushOperation(OperationAddFile("b.txt", bFile))
	c.FindBlobHashes(hashes)
	if len(hashes) != 1 || !hashes[FileEmptyHash] {
		t.Fatalf("expected empty hash, got %v", hashes)
	}

	// ranges
	fHash := "a5675307b61ec2517330622a6e649b4ca1ee5612"
	rHash := "380de212d09bf8498065833dbf242aaf11184316"
	cFile, _ := FileFromHash(fHash, strPtr(rHash), nil)
	c.PushOperation(OperationAddFile("c.txt", cFile))
	c.FindBlobHashes(hashes)
	if !hashes[fHash] || !hashes[rHash] || len(hashes) != 3 {
		t.Fatalf("expected 3 hashes, got %v", hashes)
	}
}

func TestChangeOriginTypes(t *testing.T) {
	c, err := ChangeFromRaw(map[string]any{
		"operations": []any{},
		"timestamp":  "2015-03-05T12:03:53.035Z",
		"authors":    []any{nil},
		"origin":     map[string]any{"kind": "file-restore", "version": int64(1), "path": "path", "timestamp": "2015-03-05T12:03:53.035Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.GetOrigin().(*RestoreFileOrigin); !ok {
		t.Fatalf("origin type = %T", c.GetOrigin())
	}
}

func TestChangeApplyToSetsTimestamp(t *testing.T) {
	s := NewSnapshot(nil, nil, nil, nil)
	if err := s.AddFile("main.tex", fileForTest(t, "", nil)); err != nil {
		t.Fatal(err)
	}
	file, _ := FileFromString("", nil)
	op, _ := NewAddFileOperation("main.tex", file)
	now := time.Now().UTC()
	c := NewChange([]Operation{op}, now, []any{nil}, nil, nil, nil, nil)
	if err := c.ApplyTo(s, false); err != nil {
		t.Fatal(err)
	}
	if s.GetTimestamp() == nil || !s.GetTimestamp().Equal(now) {
		t.Fatalf("timestamp not set: %v", s.GetTimestamp())
	}
}

func TestChangeCanBeComposedWith(t *testing.T) {
	a := NewChange([]Operation{OperationAddFile("f", nil)}, nowTime(), nil, nil, nil, nil, nil)
	b := NewChange([]Operation{OperationEditFile("f", NewTextEdit(NewTextOperation()))}, nowTime(), nil, nil, nil, nil, nil)
	if a.CanBeComposedWith(b) {
		t.Fatal("add vs edit should not be composable")
	}
	// more than 1 op
	a2 := NewChange([]Operation{OperationAddFile("f", nil), OperationAddFile("g", nil)}, nowTime(), nil, nil, nil, nil, nil)
	if a2.CanBeComposedWith(a) {
		t.Fatal("multi-op should not be composable")
	}
}

func TestRebaseNoConflict(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339Nano, "2025-01-02T03:04:05.678Z")
	addFile := func(pathname string) Operation {
		f, _ := FileFromString("content", nil)
		return OperationAddFile(pathname, f)
	}
	ours := []*Change{changeFromOps(t, []Operation{addFile("ours.tex")}, ts, nil, nil)}
	theirs := []*Change{changeFromOps(t, []Operation{addFile("theirs.tex")}, ts, nil, nil)}
	rebased := RebaseChanges(ours, theirs)
	if len(rebased) != 1 || len(rebased[0].Operations) != 1 {
		t.Fatalf("rebased = %d", len(rebased))
	}
	if af, ok := rebased[0].Operations[0].(*AddFileOperation); !ok || af.Pathname != "ours.tex" {
		t.Fatalf("pathname = %T", rebased[0].Operations[0])
	}
}

func TestRebaseDropsConflictingAdd(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339Nano, "2025-01-02T03:04:05.678Z")
	ours := []*Change{
		NewChange([]Operation{addFileOp("main.tex")}, ts, nil, nil, nil, nil, nil),
	}
	theirs := []*Change{
		NewChange([]Operation{addFileOp("main.tex")}, ts, nil, nil, nil, nil, nil),
	}
	rebased := RebaseChanges(ours, theirs)
	if len(rebased) != 0 {
		t.Fatalf("expected empty, got %d", len(rebased))
	}
}

func TestRebasePrunesNoOpKeepsSurvivor(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339Nano, "2025-01-02T03:04:05.678Z")
	ours := []*Change{
		NewChange([]Operation{addFileOp("main.tex"), addFileOp("other.tex")}, ts, nil, nil, nil, nil, nil),
	}
	theirs := []*Change{
		NewChange([]Operation{addFileOp("main.tex")}, ts, nil, nil, nil, nil, nil),
	}
	rebased := RebaseChanges(ours, theirs)
	if len(rebased) != 1 {
		t.Fatalf("expected 1 change, got %d", len(rebased))
	}
	ops := rebased[0].Operations
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}
	af, ok := ops[0].(*AddFileOperation)
	if !ok || af.Pathname != "other.tex" {
		t.Fatalf("op = %T", ops[0])
	}
}

func TestRebaseMoveChain(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339Nano, "2025-01-02T03:04:05.678Z")
	ours := []*Change{
		NewChange([]Operation{
			OperationEditFile("a.tex", NewTextEdit(textOpOf(t, 0, "hello"))),
		}, ts, nil, nil, nil, nil, nil),
	}
	theirs := []*Change{
		NewChange([]Operation{OperationMoveFile("a.tex", "b.tex")}, ts, nil, nil, nil, nil, nil),
		NewChange([]Operation{OperationMoveFile("b.tex", "c.tex")}, ts, nil, nil, nil, nil, nil),
	}
	rebased := RebaseChanges(ours, theirs)
	if len(rebased) != 1 {
		t.Fatalf("expected 1 change, got %d", len(rebased))
	}
	ef, ok := rebased[0].Operations[0].(*EditFileOperation)
	if !ok || ef.Pathname != "c.tex" {
		t.Fatalf("pathname = %T (%v)", rebased[0].Operations[0], rebased[0].Operations[0])
	}
}

func TestRebaseManyOursAgainstOne(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339Nano, "2025-01-02T03:04:05.678Z")
	ours := []*Change{
		NewChange([]Operation{OperationEditFile("a.tex", NewTextEdit(textOpOf(t, 0, "one")))}, ts, nil, nil, nil, nil, nil),
		NewChange([]Operation{OperationEditFile("a.tex", NewTextEdit(textOpOf(t, 3, "two")))}, ts, nil, nil, nil, nil, nil),
	}
	theirs := []*Change{
		NewChange([]Operation{OperationMoveFile("a.tex", "renamed.tex")}, ts, nil, nil, nil, nil, nil),
	}
	rebased := RebaseChanges(ours, theirs)
	if len(rebased) != 2 {
		t.Fatalf("expected 2, got %d", len(rebased))
	}
	for i, c := range rebased {
		ef, ok := c.Operations[0].(*EditFileOperation)
		if !ok || ef.Pathname != "renamed.tex" {
			t.Fatalf("change %d pathname = %T", i, c.Operations[0])
		}
	}
}

func TestRebasePreservesIdentityFields(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339Nano, "2025-01-02T03:04:05.678Z")
	authorId := "65b9d7fb2a1b2c3d4e5f6a7b"
	origin, _ := NewOriginWithID(EditorOriginKind, nil)
	ours := []*Change{
		NewChange([]Operation{addFileOp("main.tex"), addFileOp("survivor.tex")}, ts, nil, origin, []string{authorId}, nil, nil),
	}
	theirs := []*Change{
		NewChange([]Operation{addFileOp("main.tex")}, ts, nil, nil, nil, nil, nil),
	}
	rebased := RebaseChanges(ours, theirs)
	if len(rebased) != 1 {
		t.Fatalf("expected 1, got %d", len(rebased))
	}
	raw := rebased[0].ToRaw()
	sameRaw(t, "origin", raw["origin"], map[string]any{"kind": EditorOriginKind})
	sameRaw(t, "v2Authors", raw["v2Authors"], []string{authorId})
	if v, _ := raw["timestamp"].(string); v != "2025-01-02T03:04:05.678Z" {
		t.Fatalf("timestamp = %v", raw["timestamp"])
	}
}

func TestRebaseEdgeCases(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339Nano, "2025-01-02T03:04:05.678Z")
	// theirs empty
	ours := []*Change{NewChange([]Operation{addFileOp("main.tex")}, ts, nil, nil, nil, nil, nil)}
	if got := RebaseChanges(ours, []*Change{}); len(got) != 1 {
		t.Fatalf("theirs empty -> kept ours, got %d", len(got))
	}
	// ours empty
	if got := RebaseChanges([]*Change{}, []*Change{NewChange([]Operation{addFileOp("main.tex")}, ts, nil, nil, nil, nil, nil)}); len(got) != 0 {
		t.Fatalf("ours empty -> empty, got %d", len(got))
	}
}

func addFileOp(pathname string) Operation {
	f, _ := FileFromString("content", nil)
	return OperationAddFile(pathname, f)
}
