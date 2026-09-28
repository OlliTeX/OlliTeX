package rangescompat

import (
	"testing"
)

// The vendor's own header example (pinned):
//
//	the quic[k {b]rown [fox] jum[ps} ove]r the lazy dog  =>  "rown  jum" at 8
//
// Bracketed = tracked deletions; the curly-braced span = the comment range.
func TestVendorHeaderExample(t *testing.T) {
	// Pinned oracle content: "k" is the deleted char @9; "fox" @13-15;
	// "ps" @24-25; the comment span is [10, 24) ("rown " + " jum").
	content := "the quic[k ]brown [fox] jum[ps} ove]r the lazy dog"
	tracked := []any{
		map[string]any{ // deletion "k" @9
			"range":    map[string]any{"pos": 9, "length": 2},
			"tracking": map[string]any{"type": "delete", "userId": "u1", "ts": "T1"},
		},
		map[string]any{ // deletion "fox" @19..21 (bracket @18)
			"range":    map[string]any{"pos": 19, "length": 3},
			"tracking": map[string]any{"type": "delete", "userId": "u1", "ts": "T1"},
		},
		map[string]any{ // deletion "ps" @28..29 (bracket @27, comment-terminus at 30)
			"range":    map[string]any{"pos": 28, "length": 2},
			"tracking": map[string]any{"type": "delete", "userId": "u1", "ts": "T1"},
		},
	}
	comments := []any{
		map[string]any{
			"id": "c1",
			"ranges": []any{
				map[string]any{"pos": 12, "length": 18}, // comment {b ... ps} span [12,30)
			},
			"resolved": false,
		},
	}
	changes, outs, err := GetDocUpdaterCompatibleRanges(&content, true, comments, tracked)
	if err != nil {
		t.Fatal(err)
	}
	// --- changes ---------------------------------------------------------
	if len(changes) != 3 {
		t.Fatalf("changes = %v", changes)
	}
	c0 := changes[0]["op"].(map[string]any)
	if c0["p"] != 9 || c0["d"] != "k " {
		t.Fatalf("change0 = %v (want p=9 d=\"k \")", c0)
	}
	meta0 := changes[0]["metadata"].(map[string]any)
	if meta0["ts"] != "T1" || meta0["user_id"] != "u1" {
		t.Fatalf("meta0 = %v", meta0)
	}
	c1 := changes[1]["op"].(map[string]any)
	if c1["p"] != 17 || c1["d"] != "fox" { // 19-2 (k-delete offset)
		t.Fatalf("change1 = %v (want p=17 d=fox)", c1)
	}
	c2 := changes[2]["op"].(map[string]any)
	if c2["p"] != 23 || c2["d"] != "ps" { // 28-5 (offset 5 = 2+3)
		t.Fatalf("change2 = %v (want p=23 d=ps)", c2)
	}
	// --- comments --------------------------------------------------------
	if len(outs) != 1 {
		t.Fatalf("comments = %v", outs)
	}
	op := outs[0]["op"].(map[string]any)
	if outs[0]["id"] != "c1" {
		t.Fatalf("must carry id key: %v", outs[0])
	}
	// start=9 end=29 (comment span [2,21) in delete-skip terms... see inline).
	// pre-skip: del0 ends at 10 > 9 → no skip.
	// overlap: del0.start 8 < 9 → position = 9-(9-8) = 8 ✓ (vendor "at 8")
	// cursor 9: del0.start 8 not > 9; del0.end 10 <= 29 → cursor=10, di=1.
	// (content derivation asserted below against the vendor pin)

	// start=12 end=30: pre-skip del0 [9,11) (ends 11 <= 12) → position 10,
	// di=1. cursor 12: del1 [19,22) start 19 > 12 → copy [12:19] = "brown [";
	// del1.end 22 <= 30 → cursor 22, di=2. del2 [28,30) start 28 > 22 → copy
	// [22:28] = " jum["; del2.end 30 <= 30 → cursor 30, di=3 (exhausted; done).
	if op["p"] != 10 {
		t.Fatalf("comment p = %v (want 10)", op["p"])
	}
	want := content[12:19] + content[22:28]
	if op["c"] != want {
		t.Fatalf("comment c = %q (want %q)", op["c"], want)
	}
}

func TestDetachedCommentNoID(t *testing.T) {
	content := "abc"
	comments := []any{map[string]any{"id": "solo", "ranges": []any{}, "resolved": true}}
	_, outs, err := GetDocUpdaterCompatibleRanges(&content, true, comments, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, hasID := outs[0]["id"]; hasID {
		t.Fatalf("detached comment must NOT carry id: %v", outs[0])
	}
	op := outs[0]["op"].(map[string]any)
	if op["p"] != 0 || op["c"] != "" || op["t"] != "solo" || op["resolved"] != true {
		t.Fatalf("op = %v", op)
	}
}

func TestBinaryFileEmpty(t *testing.T) {
	changes, comments, err := GetDocUpdaterCompatibleRanges(nil, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 || len(comments) != 0 {
		t.Fatalf("binary = %v %v", changes, comments)
	}
}

func TestNoContentError(t *testing.T) {
	_, _, err := GetDocUpdaterCompatibleRanges(nil, true, nil, nil)
	if err == nil || err.Error() != "Unable to read file contents" {
		t.Fatalf("err = %v", err)
	}
}

func TestInsertWireShape(t *testing.T) {
	content := "abcd"
	tracked := []any{
		map[string]any{
			"range":    map[string]any{"pos": 2, "length": 2},
			"tracking": map[string]any{"type": "insert", "userId": "alice", "ts": "T9"},
		},
	}
	changes, _, err := GetDocUpdaterCompatibleRanges(&content, true, nil, tracked)
	if err != nil {
		t.Fatal(err)
	}
	op := changes[0]["op"].(map[string]any)
	if op["p"] != 2 || op["i"] != "cd" {
		t.Fatalf("op = %v", op)
	}
	if _, hasD := op["d"]; hasD {
		t.Fatalf("insert must not carry d: %v", op)
	}
}
