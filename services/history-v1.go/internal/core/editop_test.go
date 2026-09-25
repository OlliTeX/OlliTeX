package core

import (
	"encoding/json"
	"testing"
)

func TestEditOpFromRawDispatch(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		kind string
		err  bool
	}{
		{"text", `{"textOperation":[5]}`, "text", false},
		{"addComment", `{"commentId":"c1","ranges":[{"pos":0,"length":2}]}`, "addComment", false},
		{"deleteComment", `{"deleteComment":"c1"}`, "deleteComment", false},
		{"setCommentState", `{"commentId":"c1","resolved":true}`, "setCommentState", false},
		{"noOp", `{"noOp":true}`, "noOp", false},
		{"unsupported commentId alone", `{"commentId":"c1"}`, "", true},
		{"unsupported bare", `{}`, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, err := EditOpFromRaw(json.RawMessage(c.raw))
			if c.err {
				if err == nil {
					t.Fatalf("want error, got %+v", e)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if e.kind != c.kind {
				t.Fatalf("kind = %q, want %q", e.kind, c.kind)
			}
		})
	}
}

func TestEditOpApplyComment(t *testing.T) {
	f := &File{Kind: "string", Content: "abcdef", Comments: CommentList{
		{ID: "c1", Ranges: []Range{{0, 2}}, Resolved: false},
	}}
	// delete "c1"
	e, _ := EditOpFromRaw(json.RawMessage(`{"deleteComment":"c1"}`))
	if err := e.ApplyFile(f); err != nil {
		t.Fatal(err)
	}
	if len(f.Comments) != 0 {
		t.Fatalf("comments = %d, want 0", len(f.Comments))
	}
	// noOp
	e2, _ := EditOpFromRaw(json.RawMessage(`{"noOp":true}`))
	if err := e2.ApplyFile(f); err != nil {
		t.Fatal(err)
	}
	// addComment c2
	e3, _ := EditOpFromRaw(json.RawMessage(`{"commentId":"c2","ranges":[{"pos":1,"length":3}]}`))
	if err := e3.ApplyFile(f); err != nil {
		t.Fatal(err)
	}
	if len(f.Comments) != 1 || f.Comments[0].ID != "c2" {
		t.Fatalf("comments = %+v, want c2", f.Comments)
	}
	// setCommentState resolved
	e4, _ := EditOpFromRaw(json.RawMessage(`{"commentId":"c2","resolved":true}`))
	if err := e4.ApplyFile(f); err != nil {
		t.Fatal(err)
	}
	if !f.Comments[0].Resolved {
		t.Fatalf("comment c2 not resolved")
	}
}

func TestEditOpToRawRoundTrip(t *testing.T) {
	e, _ := EditOpFromRaw(json.RawMessage(`{"commentId":"cx","ranges":[{"pos":1,"length":2},{"pos":5,"length":1}]}`))
	b, _ := json.Marshal(e.ToRaw())
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if cid, _ := m["commentId"].(string); cid != "cx" {
		t.Fatalf("commentId = %v", m["commentId"])
	}
	rb, _ := json.Marshal(m["ranges"])
	if !parseEq(t, rb, []byte(`[{"pos":1,"length":2},{"pos":5,"length":1}]`)) {
		t.Fatalf("ranges wire = %s", rb)
	}
	// noOp
	e2, _ := EditOpFromRaw(json.RawMessage(`{"noOp":true}`))
	if got := string(e2.ToRaw()); got != `{"noOp":true}` {
		t.Fatalf("noOp wire = %s", got)
	}
}
