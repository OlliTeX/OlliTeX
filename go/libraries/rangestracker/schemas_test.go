package rangestracker

import "testing"

// schemas_test.go — 1:1 port of test/unit/schemas.test.js (the 13 active
// cases; the 2 it.skip "non-ObjectId id" cases are intentionally skipped in
// the oracle too — the schema is id: z.string().optional()).

var schemasMetadata = map[string]any{
	"user_id": "65a4f2c3b1e4d5a6f7089123",
	"ts":      "2026-08-14T10:47:58.000Z",
}

var (
	schemasThreadID = "65a4f2c3b1e4d5a6f7089124" // Mongo ObjectId
	schemasChangeID = "65a4f2c3b1e4d5a6f7" + "000001"
)

func parseRangesSuccess(t *testing.T, data any) {
	t.Helper()
	if _, ok := ParseRanges(data); !ok {
		t.Fatalf("expected success, got failure for %v", data)
	}
}

func parseRangesFailure(t *testing.T, data any) {
	t.Helper()
	if _, ok := ParseRanges(data); ok {
		t.Fatalf("expected failure, got success for %v", data)
	}
}

func TestSchemasRanges(t *testing.T) {
	t.Run("accepts a legacy fixedRemoveChange flag on an insert op", func(t *testing.T) {
		parseRangesSuccess(t, map[string]any{
			"changes": []any{map[string]any{
				"op":       map[string]any{"i": "foo", "p": 0, "fixedRemoveChange": true},
				"metadata": schemasMetadata,
			}},
		})
	})

	t.Run("accepts a legacy fixedRemoveChange flag on a delete op", func(t *testing.T) {
		parseRangesSuccess(t, map[string]any{
			"changes": []any{map[string]any{
				"op":       map[string]any{"d": "foo", "p": 0, "fixedRemoveChange": true},
				"metadata": schemasMetadata,
			}},
		})
	})

	t.Run("accepts a legacy orderedRejections flag on an insert op", func(t *testing.T) {
		parseRangesSuccess(t, map[string]any{
			"changes": []any{map[string]any{
				"op":       map[string]any{"i": "foo", "p": 0, "orderedRejections": true},
				"metadata": schemasMetadata,
			}},
		})
	})

	t.Run("accepts a legacy orderedRejections flag on a delete op", func(t *testing.T) {
		parseRangesSuccess(t, map[string]any{
			"changes": []any{map[string]any{
				"op":       map[string]any{"d": "foo", "p": 0, "orderedRejections": true},
				"metadata": schemasMetadata,
			}},
		})
	})

	t.Run("rejects an unrecognized key on an insert op", func(t *testing.T) {
		parseRangesFailure(t, map[string]any{
			"changes": []any{map[string]any{
				"op":       map[string]any{"i": "foo", "p": 0, "somethingElse": true},
				"metadata": schemasMetadata,
			}},
		})
	})

	t.Run("rejects a tracked change without metadata", func(t *testing.T) {
		parseRangesFailure(t, map[string]any{
			"changes": []any{map[string]any{"op": map[string]any{"i": "foo", "p": 0}}},
		})
	})

	t.Run("rejects a tracked change without a user_id", func(t *testing.T) {
		parseRangesFailure(t, map[string]any{
			"changes": []any{map[string]any{
				"op":       map[string]any{"i": "foo", "p": 0},
				"metadata": map[string]any{"ts": schemasMetadata["ts"]},
			}},
		})
	})

	t.Run("accepts a tracked change with a RangesTracker id", func(t *testing.T) {
		parseRangesSuccess(t, map[string]any{
			"changes": []any{map[string]any{
				"id":       schemasChangeID,
				"op":       map[string]any{"i": "foo", "p": 0},
				"metadata": schemasMetadata,
			}},
		})
	})

	t.Run("rejects a tracked change without a ts", func(t *testing.T) {
		parseRangesFailure(t, map[string]any{
			"changes": []any{map[string]any{
				"op":       map[string]any{"i": "foo", "p": 0},
				"metadata": map[string]any{"user_id": schemasMetadata["user_id"]},
			}},
		})
	})

	t.Run("accepts a comment op with an ObjectId thread id", func(t *testing.T) {
		parseRangesSuccess(t, map[string]any{
			"comments": []any{map[string]any{"op": map[string]any{"c": "foo", "p": 0, "t": schemasThreadID}}},
		})
	})

	t.Run("rejects a comment op with a non-ObjectId thread id", func(t *testing.T) {
		parseRangesFailure(t, map[string]any{
			"comments": []any{map[string]any{"op": map[string]any{"c": "foo", "p": 0, "t": "thread-id-1"}}},
		})
	})

	t.Run("rejects a comment op without a thread id", func(t *testing.T) {
		parseRangesFailure(t, map[string]any{
			"comments": []any{map[string]any{"op": map[string]any{"c": "foo", "p": 0}}},
		})
	})

	t.Run("accepts a comment with an ObjectId id", func(t *testing.T) {
		parseRangesSuccess(t, map[string]any{
			"comments": []any{map[string]any{
				"id": schemasThreadID,
				"op": map[string]any{"c": "foo", "p": 0, "t": schemasThreadID},
			}},
		})
	})

	// Oracle it.skip parity: tracked-change id is z.string() — any string ok;
	// comment id likewise. Both "non-ObjectId id" skip cases pinned as
	// ACCEPTED here so they stay covered if the schema ever tightens.
	t.Run("accepts a tracked change with a non-ObjectId id (oracle it.skip: z.string())", func(t *testing.T) {
		parseRangesSuccess(t, map[string]any{
			"changes": []any{map[string]any{
				"id":       "tc_1000001",
				"op":       map[string]any{"i": "foo", "p": 0},
				"metadata": schemasMetadata,
			}},
		})
	})
}
