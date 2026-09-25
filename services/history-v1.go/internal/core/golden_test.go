package core

import (
	"encoding/json"
	"testing"
)

// parseEq reports whether a and b are parse-equivalent JSON: identical object
// key sets and values (key order irrelevant) and identical array order/values.
func parseEq(t *testing.T, a, b []byte) bool {
	va, errA := normalizeJSON(a)
	vb, errB := normalizeJSON(b)
	if errA != nil || errB != nil {
		t.Fatalf("parseEq: %v / %v", errA, errB)
	}
	return jsonEq(va, vb)
}

func normalizeJSON(b []byte) (any, error) {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func jsonEq(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, va := range av {
			if !jsonEq(va, bv[k]) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEq(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		// JSON numbers decode to float64; bools to bool, strings to string.
		switch av := a.(type) {
		case float64:
			bf, ok := b.(float64)
			return ok && av == bf
		case bool:
			bb, ok := b.(bool)
			return ok && av == bb
		case string:
			bs, ok := b.(string)
			return ok && av == bs
		default:
			return a == b
		}
	}
}

// Oracle: Node StringFileData + TextOperation.apply (overleaf-editor-core).
//
//	file = File(StringFileData("abcde", [],
//	  [{range:{0,2}, tracking insert u1 2020-01-01T00:00:00.000Z},
//	   {range:{3,2}, tracking delete u2 2020-02-01T00:00:00.000Z}]))
//	op = TextOperation {textOperation: [2, {r:1,tracking:{type:clear}}, -1, 1]}
//
//	Node result (golden /tmp/tcx_node.json, oracle):
//	  content: "abce"
//	  tracked:  [{0,2 insert u1}, {3,1 delete u2}]
//	  comments: (omitted when empty)
func TestStringFileEditTrackedClear(t *testing.T) {
	f := &File{
		Kind:     "string",
		Content:  "abcde",
		Comments: CommentList{},
		TrackedChanges: TrackedChangeList{
			{
				Range:    Range{Pos: 0, Length: 2},
				Tracking: &TrackingProps{Type: "insert", UserID: "u1", TSISO: "2020-01-01T00:00:00.000Z"},
			},
			{
				Range:    Range{Pos: 3, Length: 2},
				Tracking: &TrackingProps{Type: "delete", UserID: "u2", TSISO: "2020-02-01T00:00:00.000Z"},
			},
		},
	}
	op, err := TextOpFromRaw([]byte(`{"textOperation":[2,{"r":1,"tracking":{"type":"none"}},-1,1]}`))
	if err != nil {
		t.Fatalf("TextOpFromRaw: %v", err)
	}
	if err := f.Edit(op); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if f.Content != "abce" {
		t.Fatalf("content = %q, want %q (oracle)", f.Content, "abce")
	}
	got := f.ToRaw()
	expected := `{"content":"abce","trackedChanges":[{"range":{"pos":0,"length":2},"tracking":{"type":"insert","userId":"u1","ts":"2020-01-01T00:00:00.000Z"}},{"range":{"pos":3,"length":1},"tracking":{"type":"delete","userId":"u2","ts":"2020-02-01T00:00:00.000Z"}}]}`
	if !parseEq(t, got, []byte(expected)) {
		t.Fatalf("wire = %s, want %s (parse-eq oracle)", got, expected)
	}
}

// TestTrackedChangeListOracle — oracle for StringFileData edit +
// TrackedChangeList.applyTextOperation, locked against Node
// (overleaf-editor-core) and embedded as parse-eq wire fixtures (no /tmp at
// test time).
//
// Node cursor model (locked): dest cursor; insert+retain advance, remove does
// NOT advance; one merge per op plus a final merge at end of applyTextOperation.
// Each case: file = File(StringFileData(content, [], tc-inits)); apply op.
func TestTrackedChangeListOracle(t *testing.T) {
	ins := func(u, ts string) *TrackingProps {
		return &TrackingProps{Type: "insert", UserID: u, TSISO: ts}
	}
	del := func(u, ts string) *TrackingProps {
		return &TrackingProps{Type: "delete", UserID: u, TSISO: ts}
	}
	mkTC := func(pos, length int, trk *TrackingProps) *TrackedChange {
		return &TrackedChange{Range: Range{Pos: pos, Length: length}, Tracking: trk}
	}
	cases := []struct {
		name        string
		content     string
		tc          TrackedChangeList
		op          string
		wantContent string
		wantTracked string // parse-eq wire
	}{
		{
			"plain_retain_no_tracking",
			"abcde",
			TrackedChangeList{mkTC(0, 2, ins("u", "2020-01-01T00:00:00.000Z")), mkTC(3, 2, ins("u", "2020-01-01T00:00:00.000Z"))},
			`{"textOperation":[5]}`,
			"abcde",
			`[{"range":{"pos":0,"length":2},"tracking":{"type":"insert","userId":"u","ts":"2020-01-01T00:00:00.000Z"}},{"range":{"pos":3,"length":2},"tracking":{"type":"insert","userId":"u","ts":"2020-01-01T00:00:00.000Z"}}]`,
		},
		{
			"plain_remove_middle",
			"abcdef",
			TrackedChangeList{mkTC(0, 2, ins("u0", "2020-01-01T00:00:00.000Z")), mkTC(3, 3, ins("u1", "2020-01-01T00:00:00.000Z"))},
			`{"textOperation":[2,-3,1]}`,
			"abf",
			`[{"range":{"pos":0,"length":2},"tracking":{"type":"insert","userId":"u0","ts":"2020-01-01T00:00:00.000Z"}},{"range":{"pos":2,"length":1},"tracking":{"type":"insert","userId":"u1","ts":"2020-01-01T00:00:00.000Z"}}]`,
		},
		{
			"insert_splits_and_no_untracked_merge",
			"abcde",
			TrackedChangeList{mkTC(0, 2, ins("u", "2020-01-01T00:00:00.000Z")), mkTC(3, 2, ins("u", "2020-01-01T00:00:00.000Z"))},
			`{"textOperation":[1,{"i":"Z"},4]}`,
			"aZbcde",
			`[{"range":{"pos":0,"length":1},"tracking":{"type":"insert","userId":"u","ts":"2020-01-01T00:00:00.000Z"}},{"range":{"pos":2,"length":1},"tracking":{"type":"insert","userId":"u","ts":"2020-01-01T00:00:00.000Z"}},{"range":{"pos":4,"length":2},"tracking":{"type":"insert","userId":"u","ts":"2020-01-01T00:00:00.000Z"}}]`,
		},
		{
			"delete_tracked_middle_moves_after",
			"abcdef",
			TrackedChangeList{mkTC(0, 2, ins("u", "2020-01-01T00:00:00.000Z")), mkTC(3, 3, del("u", "2020-01-01T00:00:00.000Z"))},
			`{"textOperation":[2,-3,1]}`,
			"abf",
			`[{"range":{"pos":0,"length":2},"tracking":{"type":"insert","userId":"u","ts":"2020-01-01T00:00:00.000Z"}},{"range":{"pos":2,"length":1},"tracking":{"type":"delete","userId":"u","ts":"2020-01-01T00:00:00.000Z"}}]`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &File{Kind: "string", Content: c.content, Comments: CommentList{}, TrackedChanges: c.tc}
			op, err := TextOpFromRaw([]byte(c.op))
			if err != nil {
				t.Fatalf("TextOpFromRaw: %v", err)
			}
			if err := f.Edit(op); err != nil {
				t.Fatalf("Edit: %v", err)
			}
			if f.Content != c.wantContent {
				t.Fatalf("content = %q, want %q", f.Content, c.wantContent)
			}
			if !parseEq(t, f.TrackedChanges.ToRaw(), []byte(c.wantTracked)) {
				t.Fatalf("tracked = %s, want %s", f.TrackedChanges.ToRaw(), c.wantTracked)
			}
		})
	}
}
