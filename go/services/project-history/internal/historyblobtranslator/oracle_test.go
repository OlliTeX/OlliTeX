// C4 oracle — HistoryBlobTranslator vendor contract (createRangeBlobDataFromUpdate).
package historyblobtranslator

import (
	"testing"
)

func TestC4_NotAddFileUpdate(t *testing.T) {
	if _, _, err := CreateRangeBlobDataFromUpdate(map[string]any{"docLines": "x"}); err == nil || err.Error() != "Not an AddFileUpdate" {
		t.Fatalf("err: %v", err)
	}
	if _, _, err := CreateRangeBlobDataFromUpdate(map[string]any{"doc": "d"}); err == nil || err.Error() != "Not an AddFileUpdate" {
		t.Fatalf("err: %v", err)
	}
}

func TestC4_NoRangesUndefined(t *testing.T) {
	data, found, err := CreateRangeBlobDataFromUpdate(map[string]any{"doc": "d", "docLines": "x"})
	if err != nil || data != nil || !found {
		t.Fatalf("want undefined (found=true, data=nil): %#v %v %v", data, found, err)
	}
	// empty both lists → also undefined
	_, found2, err2 := CreateRangeBlobDataFromUpdate(map[string]any{
		"doc": "d", "docLines": "x",
		"ranges": map[string]any{"changes": []any{}, "comments": []any{}},
	})
	if err2 != nil || !found2 {
		t.Fatalf("want undefined: %v %v", found2, err2)
	}
	// ranges with only null lists → also undefined
	_, found3, err3 := CreateRangeBlobDataFromUpdate(map[string]any{
		"doc": "d", "docLines": "x",
		"ranges": map[string]any{},
	})
	if err3 != nil || !found3 {
		t.Fatalf("want undefined: %v %v", found3, err3)
	}
}

func TestC4_TrackedChangesBuilt(t *testing.T) {
	// changes: two deletes + one insert, out of order by p; vendor sorts by
	// op.p with delete-before-insert on ties.
	data, found, err := CreateRangeBlobDataFromUpdate(map[string]any{
		"doc": "d", "docLines": "xxxxx",
		"ranges": map[string]any{
			"changes": []any{
				map[string]any{
					"op":       map[string]any{"p": 6, "d": "bc"},
					"metadata": map[string]any{"user_id": "u1", "ts": 1700000001000},
				},
				map[string]any{
					"op":       map[string]any{"p": 2, "d": "a"},
					"metadata": map[string]any{"user_id": "u1", "ts": 1700000000000},
				},
				map[string]any{
					"op":       map[string]any{"p": 10, "i": "Z"},
					"metadata": map[string]any{"user_id": "u2", "ts": 1700000002000},
				},
			},
		},
	})
	if err != nil || !found || data == nil {
		t.Fatalf("data=%#v found=%v err=%v", data, found, err)
	}
	tcs, _ := data["trackedChanges"].([]map[string]any)
	if len(tcs) != 3 {
		t.Fatalf("tcs: %#v", tcs)
	}
	// vendor: tcList.add merges compatible adjacent ranges; 2..3 and 10..12
	// (delete p10 len2 + insert p10 len1 are SEPARATE lists in the vendor
	// model only via tracking type — different tracking types do NOT merge).
	// Order follows insertion (a's AsSorted sorts by start).
	seeFirst := tcs[0]
	firstRange := seeFirst["range"].(map[string]int)
	if firstRange["pos"] != 2 {
		t.Fatalf("first range: %#v", seeFirst)
	}
	firstTracking := seeFirst["tracking"].(map[string]any)
	if firstTracking["type"] != "delete" {
		t.Fatalf("first tracking: %#v", seeFirst)
	}
}

func TestC4_CommentsBuilt(t *testing.T) {
	data, found, err := CreateRangeBlobDataFromUpdate(map[string]any{
		"doc": "d", "docLines": "x",
		"ranges": map[string]any{
			"comments": []any{
				map[string]any{"op": map[string]any{"t": "c1", "p": 5, "c": "hello", "resolved": true}},
				map[string]any{"op": map[string]any{"t": "c1", "p": 9, "c": "!!"}}, // same id, resolved false (unset) vs true
			},
		},
	})
	// resolved mismatch → vendor throws 'Mismatching resolved status for comment'
	if err == nil {
		t.Fatalf("want mismatch error (resolved true vs unset), got data %#v found %v", data, found)
	}
	if err.Error() != "Mismatching resolved status for comment" {
		t.Fatalf("err: %q", err.Error())
	}

	// consistent resolved status → comment with ranges for non-empty c only
	data2, found2, err2 := CreateRangeBlobDataFromUpdate(map[string]any{
		"doc": "d", "docLines": "x",
		"ranges": map[string]any{
			"comments": []any{
				map[string]any{"op": map[string]any{"t": "c1", "p": 5, "c": "hello"}},
				map[string]any{"op": map[string]any{"t": "c1", "p": 9, "c": ""}}, // empty → detached (no range)
				map[string]any{"op": map[string]any{"t": "c2", "p": 1, "c": "a"}},
			},
		},
	})
	if err2 != nil || !found2 {
		t.Fatalf("err2=%v found2=%v", err2, found2)
	}
	cmtsAny := data2["comments"].([]map[string]any)
	// vendor sorts comments by op.p BEFORE building the map → c2 (p1) first,
	// c1 (p5) second; toRaw follows that insertion order.
	if len(cmtsAny) != 2 {
		t.Fatalf("comments: %#v", cmtsAny)
	}
	if cmtsAny[0]["id"] != "c2" || cmtsAny[1]["id"] != "c1" {
		t.Fatalf("order: %#v", cmtsAny)
	}
	first := cmtsAny[1]
	firstRanges := first["ranges"].([]any)
	fr0 := firstRanges[0].(map[string]int)
	if len(firstRanges) != 1 || fr0["pos"] != 5 || fr0["length"] != 5 {
		t.Fatalf("c1 ranges: %#v", first)
	}
}

func numAsInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	return -1
}

func TestC4_TiedPositionsDeleteBeforeInsert(t *testing.T) {
	// vendor: 'should insert tracked deletions before insertions' — both ops
	// at p=4; the insert carries hpos=9 so the ranges do not overlap.
	// Input order: delete first.
	data, found, err := CreateRangeBlobDataFromUpdate(map[string]any{
		"doc": "d", "docLines": "the quickrapid brown fox jumps over the lazy dog",
		"ranges": map[string]any{
			"changes": []any{
				map[string]any{
					"op":       map[string]any{"p": 4, "d": "quick"},
					"metadata": map[string]any{"ts": "2024-01-01T00:00:00.000Z", "user_id": "user-1"},
				},
				map[string]any{
					"op":       map[string]any{"p": 4, "hpos": 9, "i": "rapid"},
					"metadata": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "user_id": "user-2"},
				},
			},
		},
	})
	if err != nil || !found || data == nil {
		t.Fatalf("found=%v err=%v", found, err)
	}
	tcs, _ := data["trackedChanges"].([]map[string]any)
	if len(tcs) != 2 {
		t.Fatalf("tcs: %#v", tcs)
	}
	// delete at [4,5) user-1; insert at [9,5) user-2 (hpos used)
	d0range := tcs[0]["range"].(map[string]int)
	d0track := tcs[0]["tracking"].(map[string]any)
	if d0range["pos"] != 4 || d0range["length"] != 5 || d0track["type"] != "delete" || d0track["userId"] != "user-1" {
		t.Fatalf("first: %#v", tcs[0])
	}
	i0range := tcs[1]["range"].(map[string]int)
	i0track := tcs[1]["tracking"].(map[string]any)
	if i0range["pos"] != 9 || i0range["length"] != 5 || i0track["type"] != "insert" || i0track["userId"] != "user-2" {
		t.Fatalf("second: %#v", tcs[1])
	}
	if comments, _ := data["comments"].([]map[string]any); len(comments) != 0 {
		t.Fatalf("comments: %#v", comments)
	}

	// reverse input order: the vendor still lists the delete first (the
	// tie-break rule 'deletes before insertions' applies to the ORDER, and
	// the resulting ranges are the same non-overlapping pair).
	data2, found2, err2 := CreateRangeBlobDataFromUpdate(map[string]any{
		"doc": "d", "docLines": "the quickrapid brown fox jumps over the lazy dog",
		"ranges": map[string]any{
			"changes": []any{
				map[string]any{
					"op":       map[string]any{"p": 4, "hpos": 9, "i": "rapid"},
					"metadata": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "user_id": "user-2"},
				},
				map[string]any{
					"op":       map[string]any{"p": 4, "d": "quick"},
					"metadata": map[string]any{"ts": "2024-01-01T00:00:00.000Z", "user_id": "user-1"},
				},
			},
		},
	})
	if err2 != nil || !found2 {
		t.Fatalf("reverse: found=%v err=%v", found2, err2)
	}
	tcs2 := data2["trackedChanges"].([]map[string]any)
	if len(tcs2) != 2 || tcs2[0]["tracking"].(map[string]any)["type"] != "delete" {
		t.Fatalf("reverse order: %#v", tcs2)
	}
}
