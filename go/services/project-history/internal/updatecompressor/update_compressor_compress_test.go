package updatecompressor

import "testing"

const bigString = "a" // expanded via strings.Repeat in size-limit tests

func mustCompress(t *testing.T, updates []Update) []Update {
	t.Helper()
	got, err := CompressUpdates(updates)
	if err != nil {
		t.Fatalf("compress error: %v", err)
	}
	return got
}

// --- vendor: describe('compress') / describe('insert - insert') ---

func TestCompress_InsertAppend(t *testing.T) {
	// vendor "should append one insert to the other"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "append", got, []Update{
		upd(43, map[string]any{"p": 3, "i": "foobar"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
}

func TestCompress_InsertInside(t *testing.T) {
	// vendor "should insert one insert inside the other"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 5, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "inside", got, []Update{
		upd(43, map[string]any{"p": 3, "i": "fobaro"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
}

func TestCompress_InsertSeparated(t *testing.T) {
	// vendor "should not append separated inserts"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 9, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "separated", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 9, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
}

func TestCompress_InsertTooBig(t *testing.T) {
	// vendor "should not append inserts that are too big (second op)"
	big := makeString(2 * 1024 * 1024)
	medium := makeString(1024 * 1024)
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": big}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "big-second", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": big}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	// first too big
	got = mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": big}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 3 + len(big), "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "big-first", got, []Update{
		upd(42, map[string]any{"p": 3, "i": big}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 3 + len(big), "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	// both too big
	got = mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": medium}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 3 + len(medium), "i": medium}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "big-both", got, []Update{
		upd(42, map[string]any{"p": 3, "i": medium}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 3 + len(medium), "i": medium}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
}

func makeString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func TestCompress_InsertTooFarApart(t *testing.T) {
	// vendor "should not append inserts that are a long time appart"
	ts3 := fTS1 + 120000
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": ts3, "user_id": fUser1}),
	})
	assertSame(t, "far", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": ts3, "user_id": fUser1}),
	})
}

func TestCompress_InsertAcrossStructureOp(t *testing.T) {
	// vendor "should not append inserts separated by project structure ops"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		{"pathname": "/old.tex", "new_pathname": "/new.tex", "meta": map[string]any{"ts": fTS1, "user_id": fUser1}, "v": float64(43)},
		upd(44, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "structure", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		{"pathname": "/old.tex", "new_pathname": "/new.tex", "meta": map[string]any{"ts": fTS1, "user_id": fUser1}, "v": float64(43)},
		upd(44, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
}

func TestCompress_InsertDiffDoc(t *testing.T) {
	// vendor "should not append ops from different doc ids"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"doc": "doc-one"}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}, map[string]any{"doc": "doc-two"}),
	})
	assertSame(t, "doc", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"doc": "doc-one"}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}, map[string]any{"doc": "doc-two"}),
	})
}

func TestCompress_InsertDiffPathname(t *testing.T) {
	// vendor "should not append ops from different doc pathnames"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"pathname": "doc-one"}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}, map[string]any{"pathname": "doc-two"}),
	})
	assertSame(t, "pathname", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"pathname": "doc-one"}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}, map[string]any{"pathname": "doc-two"}),
	})
}

func TestCompress_InsertTCMismatch(t *testing.T) {
	// vendor "should not merge updates that track changes and updates that don't"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"pathname": "main.tex"}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1, "tc": "tracking-id"}, map[string]any{"pathname": "main.tex"}),
	})
	assertSame(t, "tc-mismatch", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"pathname": "main.tex"}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1, "tc": "tracking-id"}, map[string]any{"pathname": "main.tex"}),
	})
}

func TestCompress_InsertUndoNotMerged(t *testing.T) {
	// vendor "should not merge undos with regular ops"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"pathname": "main.tex"}),
		upd(43, map[string]any{"p": 6, "i": "bar", "u": true}, map[string]any{"ts": fTS2, "user_id": fUser1}, map[string]any{"pathname": "main.tex"}),
	})
	assertSame(t, "undo", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"pathname": "main.tex"}),
		upd(43, map[string]any{"p": 6, "i": "bar", "u": true}, map[string]any{"ts": fTS2, "user_id": fUser1}, map[string]any{"pathname": "main.tex"}),
	})
}

func TestCompress_TrackedDeleteRejectionNotMerged(t *testing.T) {
	// vendor "should not merge tracked delete rejections"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"pathname": "main.tex"}),
		upd(43, map[string]any{"p": 6, "i": "bar", "trackedDeleteRejection": true}, map[string]any{"ts": fTS2, "user_id": fUser1}, map[string]any{"pathname": "main.tex"}),
	})
	assertSame(t, "tdr", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"pathname": "main.tex"}),
		upd(43, map[string]any{"p": 6, "i": "bar", "trackedDeleteRejection": true}, map[string]any{"ts": fTS2, "user_id": fUser1}, map[string]any{"pathname": "main.tex"}),
	})
}

func TestCompress_InsertHposPreserved(t *testing.T) {
	// vendor "should preserve history metadata"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar", "hpos": 16}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "hpos", got, []Update{
		upd(43, map[string]any{"p": 3, "i": "foobar", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
}

func TestCompress_InsertDiffHposOffset(t *testing.T) {
	// vendor "should not merge inserts with different hpos offsets (multi-component update)"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar", "hpos": 18}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "hpos-offset", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar", "hpos": 18}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
}

func TestCompress_InsertDiffUser(t *testing.T) {
	// vendor "should not merge updates from different users"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar", "hpos": 16}, map[string]any{"ts": fTS2, "user_id": fOtherUser}),
	})
	assertSame(t, "user", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar", "hpos": 16}, map[string]any{"ts": fTS2, "user_id": fOtherUser}),
	})
}

func TestCompress_InsertDiffComments(t *testing.T) {
	// vendor "should not merge inserts inside different comments"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo", "hpos": 13, "commentIds": []any{"comment-id-1"}}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar", "hpos": 16}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "comments", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo", "hpos": 13, "commentIds": []any{"comment-id-1"}}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar", "hpos": 16}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
}

func TestCompress_InsertCommentIdsPropagated(t *testing.T) {
	// vendor "should propagate the commentIds property"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo", "hpos": 13, "commentIds": []any{"comment-id-1"}}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar", "hpos": 16, "commentIds": []any{"comment-id-1"}}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "comment-ids", got, []Update{
		upd(43, map[string]any{"p": 3, "i": "foobar", "hpos": 13, "commentIds": []any{"comment-id-1"}}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
}

// --- vendor: delete - delete ---

func TestCompress_DeleteAppend(t *testing.T) {
	// vendor "should append one delete to the other"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 3, "d": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "append", got, []Update{
		upd(43, map[string]any{"p": 3, "d": "foobar"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
}

func TestCompress_DeleteInside(t *testing.T) {
	// vendor "should insert one delete inside the other"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 1, "d": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "inside", got, []Update{
		upd(43, map[string]any{"p": 1, "d": "bafoor"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
}

func TestCompress_DeleteSeparated(t *testing.T) {
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 9, "d": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "separated", got, []Update{
		upd(42, map[string]any{"p": 3, "d": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 9, "d": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
}

func TestCompress_DeleteTrackedChangesNoMerge(t *testing.T) {
	// vendor "should not merge deletes over tracked changes"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 3, "d": "bar", "trackedChanges": []any{map[string]any{"type": "delete", "pos": 2, "length": 10}}}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "tracked", got, []Update{
		upd(42, map[string]any{"p": 3, "d": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 3, "d": "bar", "trackedChanges": []any{map[string]any{"type": "delete", "pos": 2, "length": 10}}}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
}

func TestCompress_DeleteHposCarried(t *testing.T) {
	// extra (not a vendor case): a delete merge carries the second op's
	// hpos through, mirroring the vendor `{...secondOp, d: strInject(...)}`.
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "foo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 3, "d": "bar", "hpos": 13}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "hpos", got, []Update{
		upd(43, map[string]any{"p": 3, "d": "foobar", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
}

func TestCompress_DeleteDiffHposOffset(t *testing.T) {
	// vendor "should not merge deletes with different hpos offsets (multi-component update)"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "foo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 3, "d": "bar", "hpos": 17}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "offset", got, []Update{
		upd(42, map[string]any{"p": 3, "d": "foo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		(upd(43, map[string]any{"p": 3, "d": "bar", "hpos": 17}, map[string]any{"ts": fTS2, "user_id": fUser1})),
	})
}

func TestCompress_DeleteTrackedNoMerge(t *testing.T) {
	// vendor "should not merge when the deletes are tracked"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1, "tc": "tracking-id"}),
		upd(43, map[string]any{"p": 3, "d": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1, "tc": "tracking-id"}),
	})
	assertSame(t, "tc", got, []Update{
		upd(42, map[string]any{"p": 3, "d": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1, "tc": "tracking-id"}),
		upd(43, map[string]any{"p": 3, "d": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1, "tc": "tracking-id"}),
	})
}

// --- vendor: insert - delete ---

func TestCompress_InsertThenDeleteTrims(t *testing.T) {
	// vendor "should undo a previous insert"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 5, "d": "o"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "trim", got, []Update{
		upd(43, map[string]any{"p": 3, "i": "fo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
}

func TestCompress_InsertThenDeleteMiddle(t *testing.T) {
	// vendor "should remove part of an insert from the middle"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "fobaro"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 5, "d": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "middle", got, []Update{
		upd(43, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
}

func TestCompress_InsertThenDeleteCancels(t *testing.T) {
	// vendor "should cancel out two opposite updates"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 3, "d": "foo"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "cancel", got, []Update{})
}

func TestCompress_InsertThenDeleteSeparated(t *testing.T) {
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 9, "d": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "separated", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 9, "d": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
}

func TestCompress_InsertThenDeleteBeyondEnd(t *testing.T) {
	// vendor "should not combine updates with overlap beyond the end"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foobar"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "d": "bardle"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "beyond", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foobar"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "d": "bardle"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
}

func TestCompress_InsertThenDeleteHposPreserved(t *testing.T) {
	// vendor "should preserver history metadata"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 5, "d": "o", "hpos": 15}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "hpos", got, []Update{
		upd(43, map[string]any{"p": 3, "i": "fo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
}

func TestCompress_InsertThenDeleteDiffHposOffset(t *testing.T) {
	// vendor "should not merge insert and delete with different hpos offsets (multi-component update)"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 5, "d": "o", "hpos": 17}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "offset", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		(upd(43, map[string]any{"p": 5, "d": "o", "hpos": 17}, map[string]any{"ts": fTS2, "user_id": fUser1})),
	})
}

// --- vendor: delete - insert ---

func TestCompress_DeleteThenInsertDiffs(t *testing.T) {
	// vendor "should do a diff of the content"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "one two three four five six seven eight"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100}),
		upd(43, map[string]any{"p": 3, "i": "one 2 three four five six seven eight"}, map[string]any{"ts": fTS2, "user_id": fUser1, "doc_length": 100}),
	})
	assertSame(t, "diff", got, []Update{
		upd(43, map[string]any{"p": 7, "d": "two"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100}),
		upd(43, map[string]any{"p": 7, "i": "2"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 97}),
	})
}

func TestCompress_DeleteSameInsertNoop(t *testing.T) {
	// vendor "should return a no-op if the delete and insert are the same"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "one two three four five six seven eight"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 3, "i": "one two three four five six seven eight"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "noop", got, []Update{})
}

func TestCompress_DeleteThenInsertHposComments(t *testing.T) {
	// vendor "should preserve history metadata" (delete-insert)
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "one two three four five six seven eight", "hpos": 13}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100}),
		upd(43, map[string]any{"p": 3, "i": "one 2 three four five six seven eight", "hpos": 13, "commentIds": []any{"comment-1"}}, map[string]any{"ts": fTS2, "user_id": fUser1, "doc_length": 100}),
	})
	assertSame(t, "hpos-comments", got, []Update{
		upd(43, map[string]any{"p": 7, "d": "two", "hpos": 17}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100}),
		upd(43, map[string]any{"p": 7, "i": "2", "hpos": 17, "commentIds": []any{"comment-1"}}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 97}),
	})
}

func TestCompress_DeleteThenInsertTrackedNoMerge(t *testing.T) {
	// vendor "should not merge when tracking changes"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "one two three four five six seven eight"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100, "tc": "tracking-id"}),
		upd(43, map[string]any{"p": 3, "i": "one 2 three four five six seven eight"}, map[string]any{"ts": fTS2, "user_id": fUser1, "doc_length": 100, "tc": "tracking-id"}),
	})
	assertSame(t, "tracked", got, []Update{
		upd(42, map[string]any{"p": 3, "d": "one two three four five six seven eight"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100, "tc": "tracking-id"}),
		upd(43, map[string]any{"p": 3, "i": "one 2 three four five six seven eight"}, map[string]any{"ts": fTS2, "user_id": fUser1, "doc_length": 100, "tc": "tracking-id"}),
	})
}

// --- vendor: a long chain of ops ---

func TestCompress_SplitsAfter60Seconds(t *testing.T) {
	// vendor "should always split after 60 seconds"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS1 + 20000, "user_id": fUser1}),
		upd(44, map[string]any{"p": 9, "i": "baz"}, map[string]any{"ts": fTS1 + 40000, "user_id": fUser1}),
		upd(45, map[string]any{"p": 12, "i": "qux"}, map[string]any{"ts": fTS1 + 80000, "user_id": fUser1}),
	})
	assertSame(t, "chain", got, []Update{
		upd(44, map[string]any{"p": 3, "i": "foobarbaz"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(45, map[string]any{"p": 12, "i": "qux"}, map[string]any{"ts": fTS1 + 80000, "user_id": fUser1}),
	})
}

// --- vendor: external updates ---

func TestCompress_ExternalSources(t *testing.T) {
	// vendor "should be split from editor updates and from other sources"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1, "source": "some-editor-id"}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1, "source": "some-other-editor-id"}),
		upd(44, map[string]any{"p": 9, "i": "baz"}, map[string]any{"ts": fTS1, "user_id": fUser1, "type": "external", "source": "dropbox"}),
		upd(45, map[string]any{"p": 12, "i": "qux"}, map[string]any{"ts": fTS1, "user_id": fUser1, "type": "external", "source": "dropbox"}),
		upd(46, map[string]any{"p": 15, "i": "quux"}, map[string]any{"ts": fTS1, "user_id": fUser1, "type": "external", "source": "upload"}),
	})
	assertSame(t, "external", got, []Update{
		upd(43, map[string]any{"p": 3, "i": "foobar"}, map[string]any{"ts": fTS1, "user_id": fUser1, "source": "some-editor-id"}),
		upd(45, map[string]any{"p": 9, "i": "bazqux"}, map[string]any{"ts": fTS1, "user_id": fUser1, "type": "external", "source": "dropbox"}),
		upd(46, map[string]any{"p": 15, "i": "quux"}, map[string]any{"ts": fTS1, "user_id": fUser1, "type": "external", "source": "upload"}),
	})
}

// --- vendor: doc hash ---

func TestCompress_DocHashOnLast(t *testing.T) {
	// vendor "should keep the doc hash if it's on the last update"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1, "doc_hash": "hash1"}),
	})
	assertSame(t, "hash-keep", got, []Update{
		upd(43, map[string]any{"p": 3, "i": "foobar"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_hash": "hash1"}),
	})
}

func TestCompress_DocHashNotOnLast(t *testing.T) {
	// vendor "should not keep the doc hash if it's not on the last update"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_hash": "hash1"}),
		upd(43, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS2, "user_id": fUser1}),
	})
	assertSame(t, "hash-drop", got, []Update{
		upd(43, map[string]any{"p": 3, "i": "foobar"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
}

func TestCompress_ResyncFirst(t *testing.T) {
	// vendor "should not compress when first update is a resync"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "d": "one two three four five six seven eight"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100, "resync": true}),
		upd(43, map[string]any{"p": 3, "i": "one 2 three four five six seven eight"}, map[string]any{"ts": fTS2, "user_id": fUser1, "doc_length": 100}),
	})
	assertSame(t, "resync-first", got, []Update{
		upd(42, map[string]any{"p": 3, "d": "one two three four five six seven eight"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100, "resync": true}),
		upd(43, map[string]any{"p": 3, "i": "one 2 three four five six seven eight"}, map[string]any{"ts": fTS2, "user_id": fUser1, "doc_length": 100}),
	})
}

func TestCompress_ResyncSecond(t *testing.T) {
	// vendor "should not compress when second update is a resync"
	got := mustCompress(t, []Update{
		upd(42, map[string]any{"p": 3, "i": "one 2 three four five six seven eight"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100}),
		upd(43, map[string]any{"p": 3, "d": "one two three four five six seven eight"}, map[string]any{"ts": fTS2, "user_id": fUser1, "doc_length": 100, "resync": true}),
	})
	assertSame(t, "resync-second", got, []Update{
		upd(42, map[string]any{"p": 3, "i": "one 2 three four five six seven eight"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100}),
		upd(43, map[string]any{"p": 3, "d": "one two three four five six seven eight"}, map[string]any{"ts": fTS2, "user_id": fUser1, "doc_length": 100, "resync": true}),
	})
}

func TestCompress_DeleteInsertSpecialCase(t *testing.T) {
	// vendor "special case for delete + insert triggering diff"
	gos := mustCompress(t, []Update{
		{"op": map[string]any{"p": 3, "d": "foo"}, "meta": map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 10}, "v": float64(42)},
		{"op": map[string]any{"p": 3, "i": "bar"}, "meta": map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 10, "doc_hash": "hash1"}, "v": float64(43)},
	})
	assertSame(t, "special", gos, []Update{
		upd(43, map[string]any{"p": 3, "d": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 10}),
		upd(43, map[string]any{"p": 3, "i": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 7, "doc_hash": "hash1"}),
	})
}

// --- vendor: the full CompressRawUpdates pipeline (convert -> compress ->
// filterBlank -> concatSameVersion) ---

func TestCompressRawUpdates_Pipeline(t *testing.T) {
	// vendor `compressRawUpdates`: convert -> compress -> filterBlank ->
	// concatUpdatesWithSameVersion. A grouped multi-op update is converted
	// to single ops, compress folds the whole contiguous run into one,
	// and concat wraps it back into an op array.
	got, err := CompressRawUpdates([]Update{
		upd(42, []any{
			map[string]any{"p": 3, "i": "foo"},
			map[string]any{"p": 6, "i": "bar"},
			map[string]any{"p": 9, "i": "baz"},
		}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100}, map[string]any{"doc": "d", "pathname": "m.tex"}),
	})
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	// compress merges the three contiguous inserts into one (foo@3, bar@6,
	// baz@9 are all back-to-back), first meta carries through; concat wraps
	// the single op back into an op array.
	assertSame(t, "pipeline", got, []Update{
		{"op": []any{map[string]any{"p": 3, "i": "foobarbaz"}},
			"meta": map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 100},
			"v":    float64(42), "doc": "d", "pathname": "m.tex"},
	})
}

func TestCompressRawUpdates_PipelineCrossVersion(t *testing.T) {
	// extra: the vendor compress pipeline merges a v42/v43 pair before
	// concat, then folds the result with a later same-version update.
	got, err := CompressRawUpdates([]Update{
		upd(41, []any{map[string]any{"p": 3, "i": "foo"}}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"doc": "d", "pathname": "m.tex"}),
		upd(42, []any{map[string]any{"p": 6, "i": "bar"}}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"doc": "d", "pathname": "m.tex"}),
	})
	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}
	assertSame(t, "cross-version", got, []Update{
		{"op": []any{map[string]any{"p": 3, "i": "foobar"}},
			"meta": map[string]any{"ts": fTS1, "user_id": fUser1},
			"v":    float64(42), "doc": "d", "pathname": "m.tex"},
	})
}
