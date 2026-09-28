package updatecompressor

import "testing"

// --- vendor: describe('convertToSingleOpUpdates') ---

func TestConvertToSingleOpUpdates_SplitGrouped(t *testing.T) {
	// vendor "should split grouped updates into individual updates"
	got := ConvertToSingleOpUpdates([]Update{
		upd(42, []any{map[string]any{"p": 0, "i": "Foo"}, map[string]any{"p": 6, "i": "bar"}}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, []any{map[string]any{"p": 10, "i": "baz"}}, map[string]any{"ts": fTS2, "user_id": fOtherUser}),
	})
	want := []Update{
		upd(42, map[string]any{"p": 0, "i": "Foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(42, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(43, map[string]any{"p": 10, "i": "baz"}, map[string]any{"ts": fTS2, "user_id": fOtherUser}),
	}
	assertSame(t, "split", got, want)
}

func TestConvertToSingleOpUpdates_EmptyOpList(t *testing.T) {
	// vendor "should return no-op updates when the op list is empty"
	got := ConvertToSingleOpUpdates([]Update{
		upd(42, []any{}, map[string]any{"ts": fTS1, "user_id": fUser1}),
	})
	assertSame(t, "empty", got, []Update{})
}

func TestConvertToSingleOpUpdates_CommentOps(t *testing.T) {
	// vendor "should not ignore comment ops"
	got := ConvertToSingleOpUpdates([]Update{
		upd(42, []any{
			map[string]any{"p": 0, "i": "Foo"},
			map[string]any{"p": 9, "c": "baz"},
			map[string]any{"p": 6, "i": "bar"},
		}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 10}),
	})
	want := []Update{
		upd(42, map[string]any{"p": 0, "i": "Foo"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 10}),
		upd(42, map[string]any{"p": 9, "c": "baz"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 13}),
		upd(42, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 13}),
	}
	assertSame(t, "comment", got, want)
}

func TestConvertToSingleOpUpdates_RetainOps(t *testing.T) {
	// vendor "should not ignore retain ops with tracking data"
	got := ConvertToSingleOpUpdates([]Update{
		upd(42, []any{
			map[string]any{"p": 0, "i": "Foo"},
			map[string]any{"p": 9, "r": "baz", "tracking": map[string]any{"type": "none"}},
			map[string]any{"p": 6, "i": "bar"},
		}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 10}),
	})
	want := []Update{
		upd(42, map[string]any{"p": 0, "i": "Foo"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 10}),
		upd(42, map[string]any{"p": 9, "r": "baz", "tracking": map[string]any{"type": "none"}}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 13}),
		upd(42, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 13}),
	}
	assertSame(t, "retain", got, want)
}

func TestConvertToSingleOpUpdates_DocLengthInsert(t *testing.T) {
	// vendor "should update doc_length when splitting after an insert"
	got := ConvertToSingleOpUpdates([]Update{
		upd(42, []any{
			map[string]any{"p": 0, "i": "foo"},
			map[string]any{"p": 6, "d": "bar"},
		}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 20}),
	})
	want := []Update{
		upd(42, map[string]any{"p": 0, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 20}),
		upd(42, map[string]any{"p": 6, "d": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 23}),
	}
	assertSame(t, "insert", got, want)
}

func TestConvertToSingleOpUpdates_DocLengthDelete(t *testing.T) {
	// vendor "should update doc_length when splitting after a delete"
	got := ConvertToSingleOpUpdates([]Update{
		upd(42, []any{
			map[string]any{"p": 0, "d": "foo"},
			map[string]any{"p": 6, "i": "bar"},
		}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 20}),
	})
	want := []Update{
		upd(42, map[string]any{"p": 0, "d": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 20}),
		upd(42, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_length": 17}),
	}
	assertSame(t, "delete", got, want)
}

func TestConvertToSingleOpUpdates_TrackedChangesLength(t *testing.T) {
	// vendor "should take tracked changes into account when calculating the doc length"
	meta := map[string]any{"ts": fTS1, "user_id": fUser1, "tc": "tracked-change-id"}
	got := ConvertToSingleOpUpdates([]Update{
		upd(42, []any{
			map[string]any{"p": 6, "i": "orange"},
			map[string]any{"p": 22, "d": "apple"},
			map[string]any{"p": 12, "i": "melon", "u": true},
			map[string]any{"p": 18, "i": "banana", "u": true, "trackedDeleteRejection": true},
			map[string]any{"p": 8, "d": "pineapple", "trackedChanges": []any{map[string]any{"type": "insert", "offset": 0, "length": 9}}},
			map[string]any{"p": 11, "i": "fruit salad"},
		}, map[string]any{"ts": fTS1, "user_id": fUser1, "tc": "tracked-change-id", "doc_length": 20, "history_doc_length": 30}),
	})
	want := []Update{
		upd(42, map[string]any{"p": 6, "i": "orange"}, map[string]any{"ts": fTS1, "user_id": fUser1, "tc": "tracked-change-id", "doc_length": 30}),
		upd(42, map[string]any{"p": 22, "d": "apple"}, map[string]any{"ts": fTS1, "user_id": fUser1, "tc": "tracked-change-id", "doc_length": 36}),
		upd(42, map[string]any{"p": 12, "i": "melon", "u": true}, map[string]any{"ts": fTS1, "user_id": fUser1, "tc": "tracked-change-id", "doc_length": 36}),
		upd(42, map[string]any{"p": 18, "i": "banana", "u": true, "trackedDeleteRejection": true}, map[string]any{"ts": fTS1, "user_id": fUser1, "tc": "tracked-change-id", "doc_length": 41}),
		upd(42, map[string]any{"p": 8, "d": "pineapple", "trackedChanges": []any{map[string]any{"type": "insert", "offset": 0, "length": 9}}}, map[string]any{"ts": fTS1, "user_id": fUser1, "tc": "tracked-change-id", "doc_length": 41}),
		upd(42, map[string]any{"p": 11, "i": "fruit salad"}, map[string]any{"ts": fTS1, "user_id": fUser1, "tc": "tracked-change-id", "doc_length": 32}),
	}
	_ = meta
	assertSame(t, "tracked", got, want)
}

func TestConvertToSingleOpUpdates_DocHashLastOnly(t *testing.T) {
	// vendor "should set the doc hash on the last split update only"
	got := ConvertToSingleOpUpdates([]Update{
		upd(42, []any{
			map[string]any{"p": 0, "i": "foo"},
			map[string]any{"p": 6, "i": "bar"},
		}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_hash": "hash1"}),
		upd(43, []any{map[string]any{"p": 10, "i": "baz"}}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_hash": "hash2"}),
		upd(44, []any{
			map[string]any{"p": 0, "d": "foo"},
			map[string]any{"p": 20, "i": "quux"},
			map[string]any{"p": 3, "d": "bar"},
		}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_hash": "hash3"}),
	})
	want := []Update{
		upd(42, map[string]any{"p": 0, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(42, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_hash": "hash1"}),
		upd(43, map[string]any{"p": 10, "i": "baz"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_hash": "hash2"}),
		upd(44, map[string]any{"p": 0, "d": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(44, map[string]any{"p": 20, "i": "quux"}, map[string]any{"ts": fTS1, "user_id": fUser1}),
		upd(44, map[string]any{"p": 3, "d": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_hash": "hash3"}),
	}
	assertSame(t, "hash", got, want)
}

// --- vendor: describe('filterBlankUpdates') (no direct vendor cases;
// covered via the pure pipeline + direct check) ---

func TestFilterBlankUpdates_Direct(t *testing.T) {
	got := FilterBlankUpdates([]Update{
		upd(42, map[string]any{"p": 0, "i": ""}, map[string]any{"ts": fTS1}),
		upd(42, map[string]any{"p": 0, "d": ""}, map[string]any{"ts": fTS1}),
		upd(42, map[string]any{"p": 0, "i": "x"}, map[string]any{"ts": fTS1}),
		{"meta": map[string]any{"ts": fTS1}, "v": float64(42), "pathname": "a.tex"},
	})
	assertSame(t, "blank", got, []Update{
		upd(42, map[string]any{"p": 0, "i": "x"}, map[string]any{"ts": fTS1}),
		{"meta": map[string]any{"ts": fTS1}, "v": float64(42), "pathname": "a.tex"},
	})
}

// --- vendor: describe('concatUpdatesWithSameVersion') ---

func TestConcatUpdatesWithSameVersion_Concat(t *testing.T) {
	// vendor "should concat updates with the same version, doc and pathname"
	got := ConcatUpdatesWithSameVersion([]Update{
		upd(42, map[string]any{"p": 0, "i": "Foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"doc": "doc-id", "pathname": "main.tex"}),
		upd(42, map[string]any{"p": 6, "i": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"doc": "doc-id", "pathname": "main.tex"}),
		upd(43, map[string]any{"p": 10, "i": "baz"}, map[string]any{"ts": fTS2, "user_id": fOtherUser}, map[string]any{"doc": "doc-id", "pathname": "main.tex"}),
	})
	want := []Update{
		{"doc": "doc-id", "pathname": "main.tex", "v": float64(42), "meta": map[string]any{"ts": fTS1, "user_id": fUser1},
			"op": []any{map[string]any{"p": 0, "i": "Foo"}, map[string]any{"p": 6, "i": "bar"}}},
		{"doc": "doc-id", "pathname": "main.tex", "v": float64(43), "meta": map[string]any{"ts": fTS2, "user_id": fOtherUser},
			"op": []any{map[string]any{"p": 10, "i": "baz"}}},
	}
	assertSame(t, "concat", got, want)
}

func TestConcatUpdatesWithSameVersion_DiffDoc(t *testing.T) {
	// vendor "should not concat updates with different doc id"
	got := ConcatUpdatesWithSameVersion([]Update{
		upd(42, map[string]any{"p": 0, "i": "Foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"doc": "doc-id", "pathname": "main.tex"}),
		upd(42, map[string]any{"p": 0, "i": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"doc": "other", "pathname": "main.tex"}),
		upd(43, map[string]any{"p": 0, "i": "baz"}, map[string]any{"ts": fTS2, "user_id": fOtherUser}, map[string]any{"doc": "doc-id", "pathname": "main.tex"}),
	})
	// nothing merged
	assertSame(t, "doc", got, []Update{
		{"doc": "doc-id", "pathname": "main.tex", "v": float64(42), "meta": map[string]any{"ts": fTS1, "user_id": fUser1},
			"op": []any{map[string]any{"p": 0, "i": "Foo"}}},
		{"doc": "other", "pathname": "main.tex", "v": float64(42), "meta": map[string]any{"ts": fTS1, "user_id": fUser1},
			"op": []any{map[string]any{"p": 0, "i": "bar"}}},
		{"doc": "doc-id", "pathname": "main.tex", "v": float64(43), "meta": map[string]any{"ts": fTS2, "user_id": fOtherUser},
			"op": []any{map[string]any{"p": 0, "i": "baz"}}},
	})
}

func TestConcatUpdatesWithSameVersion_StructureOp(t *testing.T) {
	// vendor "should not concat text updates with project structure ops"
	got := ConcatUpdatesWithSameVersion([]Update{
		upd(42, map[string]any{"p": 0, "i": "Foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"doc": "doc-id", "pathname": "main.tex"}),
		{"pathname": "main.tex", "new_pathname": "new.tex", "meta": map[string]any{"ts": fTS1, "user_id": fUser1}, "v": float64(42)},
	})
	assertSame(t, "structure", got, []Update{
		{"doc": "doc-id", "pathname": "main.tex", "v": float64(42), "meta": map[string]any{"ts": fTS1, "user_id": fUser1},
			"op": []any{map[string]any{"p": 0, "i": "Foo"}}},
		{"pathname": "main.tex", "new_pathname": "new.tex", "meta": map[string]any{"ts": fTS1, "user_id": fUser1}, "v": float64(42)},
	})
}

func TestConcatUpdatesWithSameVersion_DocHashLastOnly(t *testing.T) {
	// vendor "should keep the doc hash only when it's on the last update"
	got := ConcatUpdatesWithSameVersion([]Update{
		upd(1, map[string]any{"p": 0, "i": "foo"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"doc": "doc-id", "pathname": "main.tex"}),
		upd(1, map[string]any{"p": 10, "i": "bar"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_hash": "hash1"}, map[string]any{"doc": "doc-id", "pathname": "main.tex"}),
		upd(2, map[string]any{"p": 20, "i": "baz"}, map[string]any{"ts": fTS1, "user_id": fUser1, "doc_hash": "hash2"}, map[string]any{"doc": "doc-id", "pathname": "main.tex"}),
		upd(2, map[string]any{"p": 30, "i": "quux"}, map[string]any{"ts": fTS1, "user_id": fUser1}, map[string]any{"doc": "doc-id", "pathname": "main.tex"}),
	})
	assertSame(t, "hash-last", got, []Update{
		{"doc": "doc-id", "pathname": "main.tex", "v": float64(1),
			"meta": map[string]any{"ts": fTS1, "user_id": fUser1, "doc_hash": "hash1"},
			"op":   []any{map[string]any{"p": 0, "i": "foo"}, map[string]any{"p": 10, "i": "bar"}}},
		{"doc": "doc-id", "pathname": "main.tex", "v": float64(2),
			"meta": map[string]any{"ts": fTS1, "user_id": fUser1},
			"op":   []any{map[string]any{"p": 20, "i": "baz"}, map[string]any{"p": 30, "i": "quux"}}},
	})
}
