package core

import (
	"testing"
)

// EditOp transform tests — mirror of the Node oracle
// test/unit/edit_operation.test.js (EditOperationTransformer cases). The
// prime wire JSON below is exactly what the Node oracle emits (probed via
// edit_operation_transformer.js with toJSON assertions).

const (
	editNoOpJSON = `{"noOp":true}`
)

func expectEditTransform(t *testing.T, name string, a, b *EditOp, wantA, wantB string) {
	t.Helper()
	ap, bp, err := EditOpTransform(a, b)
	if err != nil {
		t.Fatalf("%s: transform: %v", name, err)
	}
	if got := string(ap.ToRaw()); got != wantA {
		t.Errorf("%s: a' = %s, want %s", name, got, wantA)
	}
	if got := string(bp.ToRaw()); got != wantB {
		t.Errorf("%s: b' = %s, want %s", name, got, wantB)
	}
}

func TestEditOpTransform(t *testing.T) {
	t.Parallel()

	// two AddCommentOperations, same commentId => a' noOp, b' kept
	expectEditTransform(t, "add/add same",
		NewAddCommentEditOp("comm1", []Range{{0, 1}}, false),
		NewAddCommentEditOp("comm1", []Range{{2, 3}}, false),
		editNoOpJSON,
		`{"commentId":"comm1","ranges":[{"pos":2,"length":3}]}`,
	)

	// two AddCommentOperations, different commentId => unchanged
	expectEditTransform(t, "add/add diff",
		NewAddCommentEditOp("comm1", []Range{{0, 1}}, false),
		NewAddCommentEditOp("comm2", []Range{{2, 3}}, false),
		`{"commentId":"comm1","ranges":[{"pos":0,"length":1}]}`,
		`{"commentId":"comm2","ranges":[{"pos":2,"length":3}]}`,
	)

	// two DeleteCommentOperations, same id => both noOp
	expectEditTransform(t, "del/del same",
		NewDeleteCommentEditOp("comm1"),
		NewDeleteCommentEditOp("comm1"),
		editNoOpJSON, editNoOpJSON,
	)

	// two DeleteCommentOperations, different id => unchanged
	expectEditTransform(t, "del/del diff",
		NewDeleteCommentEditOp("comm1"),
		NewDeleteCommentEditOp("comm2"),
		`{"deleteComment":"comm1"}`,
		`{"deleteComment":"comm2"}`,
	)

	// AddComment + DeleteComment same id => delete wins (a' noOp)
	expectEditTransform(t, "add/del same",
		NewAddCommentEditOp("comm1", []Range{{0, 1}}, false),
		NewDeleteCommentEditOp("comm1"),
		editNoOpJSON, `{"deleteComment":"comm1"}`,
	)

	// DeleteComment + AddComment same id => delete wins (b' noOp)
	expectEditTransform(t, "del/add same",
		NewDeleteCommentEditOp("comm1"),
		NewAddCommentEditOp("comm1", []Range{{0, 1}}, false),
		`{"deleteComment":"comm1"}`, editNoOpJSON,
	)

	// TextOp (retain 9, insert ' world') x AddComment(10,3) => comment moves to 16
	expectEditTransform(t, "text/add same",
		NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `9`, `" world"`)}),
		NewAddCommentEditOp("comm1", []Range{{10, 3}}, false),
		`{"textOperation":[9," world"]}`,
		`{"commentId":"comm1","ranges":[{"pos":16,"length":3}]}`,
	)

	// AddComment x TextOp => b' is text, a' moved comment
	expectEditTransform(t, "add/text same",
		NewAddCommentEditOp("comm1", []Range{{10, 3}}, false),
		NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `9`, `" world"`)}),
		`{"commentId":"comm1","ranges":[{"pos":16,"length":3}]}`,
		`{"textOperation":[9," world"]}`,
	)

	// AddComment + TextOp that detaches the comment (remove 13 covers 5,5)
	expectEditTransform(t, "text/add detached",
		NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `-13`)}),
		NewAddCommentEditOp("comm1", []Range{{5, 5}}, false),
		`{"textOperation":[-13]}`,
		`{"commentId":"comm1","ranges":[]}`,
	)

	// AddComment + deletion TextOp: keep partial overlap
	expectEditTransform(t, "text/add partial-delete",
		NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `8`, `-4`)}),
		NewAddCommentEditOp("comm1", []Range{{10, 3}}, false),
		`{"textOperation":[8,-4]}`,
		`{"commentId":"comm1","ranges":[{"pos":8,"length":1}]}`,
	)

	// AddComment + complex TextOp (insert + retain + remove)
	expectEditTransform(t, "text/add complex",
		NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `"foo "`, `8`, `-4`)}),
		NewAddCommentEditOp("comm1", []Range{{10, 3}}, false),
		`{"textOperation":["foo ",8,-4]}`,
		`{"commentId":"comm1","ranges":[{"pos":12,"length":1}]}`,
	)

	// Text x DeleteComment => no conflict
	expectEditTransform(t, "text/del",
		NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `9`, `" world"`)}),
		NewDeleteCommentEditOp("comm1"),
		`{"textOperation":[9," world"]}`,
		`{"deleteComment":"comm1"}`,
	)

	// Text x SetCommentState => no conflict
	expectEditTransform(t, "text/set",
		NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `9`, `" world"`)}),
		NewSetCommentStateEditOp("comm1", true),
		`{"textOperation":[9," world"]}`,
		`{"resolved":true,"commentId":"comm1"}`,
	)

	// SetCommentState x AddComment same id
	expectEditTransform(t, "set/add same",
		NewAddCommentEditOp("comm1", []Range{{0, 1}}, false),
		NewSetCommentStateEditOp("comm1", true),
		`{"commentId":"comm1","ranges":[{"pos":0,"length":1}],"resolved":true}`,
		`{"resolved":true,"commentId":"comm1"}`,
	)

	// SetCommentState x DeleteComment same id => delete wins
	expectEditTransform(t, "set/del same",
		NewDeleteCommentEditOp("comm1"),
		NewSetCommentStateEditOp("comm1", true),
		`{"deleteComment":"comm1"}`, editNoOpJSON,
	)

	// SetCommentState x SetCommentState same id, different resolved => earlier wins
	expectEditTransform(t, "set/set diff",
		NewSetCommentStateEditOp("comm1", false),
		NewSetCommentStateEditOp("comm1", true),
		`{"resolved":false,"commentId":"comm1"}`,
		editNoOpJSON,
	)

	// SetCommentState x SetCommentState same id same state => both noOp
	expectEditTransform(t, "set/set same-state",
		NewSetCommentStateEditOp("comm1", true),
		NewSetCommentStateEditOp("comm1", true),
		editNoOpJSON, editNoOpJSON,
	)

	// SetCommentState x SetCommentState diff id => unchanged
	expectEditTransform(t, "set/set diff-id",
		NewSetCommentStateEditOp("comm1", false),
		NewSetCommentStateEditOp("comm2", true),
		`{"resolved":false,"commentId":"comm1"}`,
		`{"resolved":true,"commentId":"comm2"}`,
	)

	// Text x Text => both text, transformed (insert 'foo' / insert 'bar' at
	// base 0). Probed via the oracle: a' = ["foo", 3], b' = [3, "bar"].
	expectEditTransform(t, "text/text",
		NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `"foo"`)}),
		NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `"bar"`)}),
		`{"textOperation":["foo",3]}`,
		`{"textOperation":[3,"bar"]}`,
	)

	// noOp x other => unchanged
	ap2, bp2, err := EditOpTransform(
		NewEditNoOp(),
		NewTextEditOp(&TextOp{Ops: jsonRawScanOps(t, `"foo"`)}),
	)
	if err != nil {
		t.Fatalf("noop/text: %v", err)
	}
	if got := string(ap2.ToRaw()); got != editNoOpJSON {
		t.Errorf("noop/text a' = %s, want noOp", got)
	}
	if got := string(bp2.ToRaw()); got != `{"textOperation":["foo"]}` {
		t.Errorf("noop/text b' = %s, want text op", got)
	}
}
