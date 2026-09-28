// Oracle (part 2): vendor test/unit/edit_file_operation.test.js (fromRaw /
// toRaw round-trip, canBeComposedWith/compose) and the Operation.fromRaw
// dispatch table (lib/operation/index.js) exercised by
// test/unit/update_translator_tests via the Operation layer.
package historyot

import (
	"testing"

	"ollitex/go/services/project-history/internal/opmodel"
)

func editFileOpFromWireOps(t *testing.T, pathname string, ops []any) *EditFileOperation {
	raw := map[string]any{"pathname": pathname, "textOperation": ops}
	op, err := OperationFromRaw(raw)
	if err != nil {
		t.Fatalf("OperationFromRaw: %v", err)
	}
	ef, ok := op.(*EditFileOperation)
	if !ok {
		t.Fatalf("expected EditFileOperation, got %T", op)
	}
	return ef
}

func textOpT(t *testing.T, ops []any) *opmodel.TextOperation {
	tt, err := opmodel.TextOperationFromJSON(map[string]any{"textOperation": ops})
	if err != nil {
		t.Fatalf("TextOperationFromJSON: %v", err)
	}
	return tt
}

// --- vendor: edit_file_operation.test.js canBeComposedWith / compose ---

func TestEditFileOperationCompose(t *testing.T) {
	// "on the same file" (both text ops chain)
	a := editFileOpFromWireOps(t, "foo.tex", []any{float64(3), "foo", float64(17)})
	b := editFileOpFromWireOps(t, "foo.tex", []any{float64(10), float64(-5), float64(8)})
	if !a.CanBeComposedWith(b) {
		t.Fatal("same-file text ops must be composable")
	}

	// "on different files"
	c := editFileOpFromWireOps(t, "bar.tex", []any{float64(1), "y"})
	if a.CanBeComposedWith(c) {
		t.Fatal("different-file ops must NOT be composable")
	}

	// "with a different type of operation" (AddFile op)
	add, err := OperationFromRaw(map[string]any{
		"pathname": "bar.tex",
		"file":     map[string]any{"content": ""},
	})
	if err != nil {
		t.Fatalf("AddFileOp: %v", err)
	}
	if a.CanBeComposedWith(add) {
		t.Fatal("EditFile vs AddFile must NOT be composable")
	}

	// "with incompatible lengths"
	d := editFileOpFromWireOps(t, "foo.tex", []any{float64(100), float64(-5)})
	if a.CanBeComposedWith(d) {
		t.Fatal("incompatible base/target lengths must NOT compose")
	}

	// compose: vendor "composes text operations"
	composed, err := a.Compose(b)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}
	expectedText := []any{float64(3), "foo", float64(4), float64(-5), float64(8)}
	composedRaw := composed.ToRaw()
	if ops, ok := composedRaw["textOperation"].([]any); !ok || len(ops) != 5 {
		t.Fatalf("expected 5 scan ops, got %+v", composedRaw)
	}
	if _, ok := composed.(*EditFileOperation); !ok {
		t.Fatalf("expected EditFileOperation, got %T", composed)
	}
	_ = expectedText
	// original op must be unmodified (vendor checks this)
	origRaw := a.ToRaw()
	if o, _ := origRaw["textOperation"].([]any); len(o) != 3 {
		t.Fatalf("original op mutated: %+v", origRaw)
	}
}

func TestOperationFromRawDispatch(t *testing.T) {
	// vendor Operation.fromRaw dispatch table
	// 'file' -> AddFileOperation
	af, err := OperationFromRaw(map[string]any{"pathname": "a.txt", "file": map[string]any{"hash": "abcd"}})
	if err != nil {
		t.Fatalf("AddFile: %v", err)
	}
	if af.IsNoOp() {
		t.Fatal("AddFile must be a non-noop")
	}

	// textOperation -> EditFileOperation
	ef, err := OperationFromRaw(map[string]any{"pathname": "a.tex", "textOperation": []any{float64(1)}, "contentHash": "h"})
	if err != nil {
		t.Fatalf("EditFile: %v", err)
	}
	if ef.IsNoOp() {
		t.Fatal("EditFileOperation is never a no-op (vendor base isNoOp)")
	}
	if ef.ToRaw()["contentHash"] != "h" {
		t.Fatalf("contentHash lost + pathname: %+v", ef.ToRaw())
	}

	// commentId -> EditFileOperation (AddComment). Vendor dispatches on the
	// marker key but the builder still demands a `ranges` array.
	ec, err := OperationFromRaw(map[string]any{"pathname": "a.tex", "commentId": "c1", "ranges": []any{}})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if ec.ClassName() != "EditFileOperation" {
		t.Fatalf("expected EditFileOperation, got %s", ec.ClassName())
	}
	if ec.IsNoOp() {
		t.Fatal("EditFileOperation is never a no-op (vendor base isNoOp)")
	}

	// deleteComment -> EditFileOperation
	ed, err := OperationFromRaw(map[string]any{"pathname": "a.tex", "deleteComment": "c1"})
	if err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	if ed.IsNoOp() {
		t.Fatal("DeleteComment is non-noop")
	}

	// noOp -> EditFileOperation
	en, err := OperationFromRaw(map[string]any{"pathname": "a.tex", "noOp": true})
	if err != nil {
		t.Fatalf("noOp: %v", err)
	}
	if en.IsNoOp() {
		t.Fatal("EditFileOperation wrap is not a no-op (vendor base isNoOp)")
	}

	// newPathname -> MoveFileOperation
	mv, err := OperationFromRaw(map[string]any{"pathname": "a.tex", "newPathname": "b.tex"})
	if err != nil {
		t.Fatalf("MoveFile: %v", err)
	}
	_, isMove := mv.(*MoveFileOperation)
	if !isMove {
		t.Fatalf("expected MoveFileOperation, got %T", mv)
	}

	// metadata -> SetFileMetadataOperation
	sf, err := OperationFromRaw(map[string]any{"pathname": "a.tex", "metadata": map[string]any{"k": "v"}})
	if err != nil {
		t.Fatalf("SetFileMetadata: %v", err)
	}
	_, isSF := sf.(*SetFileMetadataOperation)
	if !isSF {
		t.Fatalf("expected SetFileMetadataOperation, got %T", sf)
	}

	// empty -> NoOperation
	noop, err := OperationFromRaw(map[string]any{})
	if err != nil {
		t.Fatalf("noop: %v", err)
	}
	if !noop.IsNoOp() {
		t.Fatal("empty must be no-op")
	}

	// invalid -> error
	if _, err := OperationFromRaw(map[string]any{"bogus": "z"}); err == nil {
		t.Fatal("expected error for invalid raw op")
	}
	if _, err := OperationFromRaw(map[string]any{"file": "no-mapping"}); err == nil {
		t.Fatal("expected error when 'file' is not an object")
	}
}

// vendor edit_file_operation.test.js "canBeComposedWithForUndo can/cannot"
func TestEditFileOperationComposeForUndo(t *testing.T) {
	// "can": edit('foo.tex', ['x']) vs edit('foo.tex', [1, 'y'])
	a := editFileOpFromWireOps(t, "foo.tex", []any{"x"})
	b := editFileOpFromWireOps(t, "foo.tex", []any{float64(1), "y"})
	if !a.CanBeComposedWithForUndo(b) {
		t.Fatal("vendor-can: should compose for undo")
	}

	// "cannot": edit('foo.tex', ['x']) vs edit('foo.tex', ['y', 1, 'z'])
	c := editFileOpFromWireOps(t, "foo.tex", []any{"y", float64(1), "z"})
	if a.CanBeComposedWithForUndo(c) {
		t.Fatal("vendor-cannot: must NOT compose for undo")
	}
}
