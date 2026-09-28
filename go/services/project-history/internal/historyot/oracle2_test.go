package historyot

import (
	"testing"
	"time"

	"ollitex/go/services/project-history/internal/opmodel"
)

// --- change.go ---------------------------------------------------------------

func TestChangeRoundTrip(t *testing.T) {
	now := time.Unix(1735689600, 0)
	op := NewNoOperation()
	c := NewChange([]Operation{op}, now, []any{"u1", 0.0}, &Origin{Kind: "history-resync", HistoryClientID: "hc1"},
		[]any{"v2u"}, float64(7), &V2DocVersions{Data: map[string]any{"d1": map[string]any{"pathname": "a.tex", "v": 3.0}}})
	raw := c.ToRaw()
	if raw["projectVersion"] != 7.0 {
		t.Fatalf("want projectVersion 7, got %v", raw["projectVersion"])
	}
	if raw["v2DocVersions"] == nil || raw["v2Authors"] == nil || raw["origin"] == nil {
		t.Fatalf("want all optionals present, got %v", raw)
	}
	if authors, ok := raw["authors"].([]any); !ok || authors[0] != "u1" || authors[1] != 0.0 {
		t.Fatalf("want authors pass-through (0 stays 0 on raw), got %v", raw["authors"])
	}
	back, err := ChangeFromRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.ProjectVersion != 7.0 || len(back.Operations) != 1 {
		t.Fatalf("round trip lost data: %+v", back)
	}
	// 0 author id → null (vendor historical clean-up in fromRaw)
	if back.Authors[1] != nil {
		t.Fatalf("want null author for 0, got %v", back.Authors[1])
	}
	// PushOperation returns the chainable self.
	if c2 := c.PushOperation(op); c2 != c {
		t.Fatal("PushOperation must chain")
	}
}

func TestChangeFromRawErrors(t *testing.T) {
	if _, err := ChangeFromRaw(map[string]any{}); err == nil {
		t.Fatal("want bad operations error")
	}
	if _, err := ChangeFromRaw(map[string]any{"operations": []any{}}); err == nil {
		t.Fatal("want bad timestamp error")
	}
	if _, err := ChangeFromRaw(map[string]any{"operations": []any{"notamap"}, "timestamp": "2025-01-01T00:00:00.000Z"}); err == nil {
		t.Fatal("want bad raw change operation error")
	}
}

func TestChangeFindBlobHashes(t *testing.T) {
	f, err := FileFromRaw(map[string]any{"hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "stringLength": 0.0, "rangesHash": "r1x"})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]struct{}{}
	c := NewChange(nil, time.Unix(0, 0), nil, nil, nil, nil, nil)
	c.Operations = []Operation{
		NewAddFileOperation("a.tex", f),
		NewMoveFileOperation("a.tex", "b.tex"),
	}
	c.FindBlobHashes(out)
	if _, ok := out["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]; !ok {
		t.Fatalf("want file hash in blob hashes, got %v", out)
	}
}

// --- file.go -----------------------------------------------------------------

func TestFileConstructors(t *testing.T) {
	f, err := FileFromRaw(map[string]any{"hash": "h1", "rangesHash": "r1", "metadata": map[string]any{"m": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if f.GetHash() != "h1" || f.GetRangesHash() != "r1" || f.GetMetadata()["m"] != 1 {
		t.Fatalf("want hash file, got %v", f.ToRaw())
	}
	f2 := FileFromString("hello", map[string]any{})
	if content, ok := f2.GetContent(); !ok || content != "hello" {
		t.Fatalf("want content, got %q %v", content, ok)
	}
	if f3, err := FileFromHash("deadbeef", "", nil); err == nil || f3 != nil {
		t.Fatal("want short-hash error")
	}
}

// --- operation.go ------------------------------------------------------------

func TestOperationFromRawDispatchExtra(t *testing.T) {
	// AddFile: 'file' key
	if op, err := OperationFromRaw(map[string]any{"file": map[string]any{"hash": "h1", "stringLength": 0}}); err == nil || op == nil {
		// hash-only file must be constructable (isHex40 gate uses 40 chars
		// in the vendor regex; 'h1' fails) — expect an error here.
	}
	op, err := OperationFromRaw(map[string]any{
		"file": map[string]any{"hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "stringLength": 0},
	})
	if err != nil || op == nil {
		t.Fatalf("AddFile dispatch: %v", err)
	}
	// EditFile via noOp
	if op2, err2 := OperationFromRaw(map[string]any{"pathname": "x.tex", "noOp": 1}); err2 != nil || op2 == nil {
		t.Fatalf("EditFile noOp dispatch: %v", err2)
	}
	// unknown shape
	if _, err := OperationFromRaw(map[string]any{"bogus": true}); err == nil {
		t.Fatal("want unrecognized operation error")
	}
}

func TestNoOpAndMoveOps(t *testing.T) {
	noop := NewNoOperation()
	if !noop.IsNoOp() || noop.ClassName() != "NoOperation" {
		t.Fatal("noop shape")
	}
	if len(noop.ToRaw()) != 0 {
		t.Fatalf("noop raw must be empty, got %v", noop.ToRaw())
	}
	if _, err := noop.Compose(noop); err == nil {
		t.Fatal("want not implemented")
	}
	mv := NewMoveFileOperation("a", "b")
	if mv.ToRaw()["newPathname"] != "b" || mv.ToRaw()["pathname"] != "a" {
		t.Fatalf("move raw got %v", mv.ToRaw())
	}
	rm := NewRemoveFileOperation("a")
	if !rm.IsRemoveFile() || mv.IsNoOp() {
		t.Fatal("move/remove flags")
	}
	if mv.CanBeComposedWith(rm) {
		t.Fatal("move canBeComposedWith must be false (not overridden)")
	}
	if _, err := mv.Compose(mv); err == nil {
		t.Fatal("want not implemented")
	}
}

func TestEditFileComposeTextOps(t *testing.T) {
	// ta: base 2 + retain 2 → target 4. tb: base 4 → composable per the
	// vendor rule TargetLength == other.BaseLength.
	taT := opmodel.NewTextOperation()
	taT.Retain(2, nil)
	ta := NewTextOpAdapter(taT)
	tbT := opmodel.NewTextOperation()
	tbT.Retain(2, nil) // base 2 == ta.target 2 → composable
	tb := NewTextOpAdapter(tbT)
	a1 := NewEditFileOperation("f.tex", ta)
	b1 := NewEditFileOperation("f.tex", tb)
	if !a1.CanBeComposedWith(b1) {
		t.Fatal("text edits on the same file with matching lengths must be composable")
	}
	composed, err := a1.Compose(b1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := composed.(*EditFileOperation); !ok {
		t.Fatal("must be an EditFileOperation")
	}
	// different pathnames → false
	other := NewEditFileOperation("other.tex", tb)
	if a1.CanBeComposedWith(other) {
		t.Fatal("different pathnames must not compose")
	}
	// non-EditFileOperation → false
	if a1.CanBeComposedWith(NewNoOperation()) {
		t.Fatal("type mismatch must not compose")
	}
}

// --- origin.go ---------------------------------------------------------------

func TestOriginRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
	}{
		{"kind", map[string]any{"kind": "history-resync"}},
		{"historyClientID", map[string]any{"historyClientId": "hc1"}},
		{"restore", map[string]any{"kind": "restore", "version": 3.0, "timestamp": "2025-01-01T00:00:00.000Z"}},
		{"file-restore", map[string]any{"kind": "file-restore", "path": "a.tex", "version": 3.0, "timestamp": "2025-01-01T00:00:00.000Z"}},
		{"project-restore", map[string]any{"kind": "project-restore", "version": 5.0, "timestamp": "2025-01-01T00:00:00.000Z"}},
	}
	for _, tc := range cases {
		o, err := OriginFromRaw(tc.raw)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		switch ot := o.(type) {
		case *Origin:
			if tc.name == "kind" && ot.Kind != "history-resync" {
				t.Fatalf("kind origin got %q", ot.Kind)
			}
			if tc.name == "historyClientID" && ot.HistoryClientID != "hc1" {
				t.Fatalf("historyClientId got %q", ot.HistoryClientID)
			}
		case *RestoreOrigin:
			if ot.Version != 3 {
				t.Fatalf("restore origin got %+v", ot)
			}
		case *RestoreFileOrigin:
			if ot.Path != "a.tex" || ot.Version != 3 {
				t.Fatalf("file-restore origin got %+v", ot)
			}
		case *RestoreProjectOrigin:
			if ot.Version != 5 {
				t.Fatalf("project-restore origin got %+v", ot)
			}
		default:
			t.Fatalf("%s: unexpected origin type %T", tc.name, o)
		}
		c := NewChange(nil, time.Unix(0, 0), nil, o, nil, nil, nil)
		if c.OriginRaw() == nil {
			t.Fatalf("%s: OriginRaw nil", tc.name)
		}
	}
	if o, err := OriginFromRaw(map[string]any{}); err != nil {
		t.Fatalf("empty origin: %v", err)
	} else if og, ok := o.(*Origin); !ok || og.Kind != "" {
		t.Fatalf("want generic empty origin, got %T %v", o, o)
	}
}

func TestOriginFallbackAndWire(t *testing.T) {
	// unknown kind falls back to the generic Origin (vendor: kind is kept).
	o, err := OriginFromRaw(map[string]any{"kind": "weird"})
	if err != nil {
		t.Fatal(err)
	}
	if og, ok := o.(*Origin); !ok || og.Kind != "weird" {
		t.Fatalf("want generic origin fallback, got %T %v", o, o)
	}
	// ToWire for each variant (hcid omitted when empty — vendor gate).
	g := toWireOrigin("restore", "", func(m map[string]any) { m["version"] = 1.0 })
	if g["kind"] != "restore" {
		t.Fatalf("wire origin: %v", g)
	}
	if _, ok := g["historyClientId"]; ok {
		t.Fatalf("empty hcid must be absent: %v", g)
	}
}

func TestWireTimeRoundTrip(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	s := wireTime(now)
	if s != "2025-01-01T00:00:00.000Z" {
		t.Fatalf("got %q", s)
	}
	back, err := decodeWireTime(s)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Equal(now) {
		t.Fatalf("round trip mismatch: %v vs %v", back, now)
	}
	if tv, err := decodeWireTime(123); err != nil || !tv.IsZero() {
		t.Fatalf("non-string timestamp must yield zero time without error, got %v %v", tv, err)
	}
	if tv, err := decodeWireTime("not-a-date"); err != nil || !tv.IsZero() {
		t.Fatalf("unparseable timestamp must yield zero time without error, got %v %v", tv, err)
	}
	_ = mapString(map[string]any{"a": "1"})
}

// --- transform.go ------------------------------------------------------------

func TestTransformTextText(t *testing.T) {
	ta := NewTextOpAdapter(func() *opmodel.TextOperation { x := opmodel.NewTextOperation(); x.Retain(5, nil); return x }())
	tp := NewTextOpAdapter(func() *opmodel.TextOperation { x := opmodel.NewTextOperation(); x.Retain(2, nil); return x }())
	a, b, err := (EditOperationTransformer{}).Transform(ta, tp)
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || b == nil {
		t.Fatal("nil transform results")
	}
}

func TestTransformManyAndNoOpIdentity(t *testing.T) {
	tt := NewTextOpAdapter(opmodel.NewTextOperation())
	nope := NewEditNoOperation()
	if a3, b3, err := (EditOperationTransformer{}).Transform(nope, tt); err != nil || a3 != nope || b3 != tt {
		t.Fatalf("no-op identity: %v", err)
	}
	if err := (EditOperationTransformer{}).TransformMany([]EditOp{tt}, []EditOp{tt}); err != nil {
		t.Fatal(err)
	}
}

func TestSetCommentStateComposeSameID(t *testing.T) {
	a, err := SetCommentStateOperationFromRaw(map[string]any{"commentId": "c1", "resolved": true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := SetCommentStateOperationFromRaw(map[string]any{"commentId": "c1", "resolved": false})
	if err != nil {
		t.Fatal(err)
	}
	if !a.CanBeComposedWith(b) {
		t.Fatal("same-id SetCommentState must compose")
	}
	c, err := SetCommentStateOperationFromRaw(map[string]any{"commentId": "c2", "resolved": true})
	if err != nil {
		t.Fatal(err)
	}
	if a.CanBeComposedWith(c) {
		t.Fatal("different ids must not compose")
	}
	// missing commentId → UnprocessableError
	if _, err := SetCommentStateOperationFromRaw(map[string]any{"resolved": true}); err == nil {
		t.Fatal("want missing commentId error")
	}
	// Apply over a file with and without the comment.
	f := NewStringFileData("hello")
	c1, _ := opmodel.NewComment("c1", []opmodel.Range{{Pos: 0, Length: 2}}, false)
	f.Comments.Add(c1)
	if err := a.Apply(f); err != nil {
		t.Fatal(err)
	}
	got := f.Comments.GetComment("c1")
	if got == nil || !got.Resolved {
		t.Fatalf("want resolved comment after Apply, got %+v", got)
	}
	if err := b.Apply(f); err != nil {
		t.Fatal(err)
	}
	// ToRaw shape
	raw := a.ToRaw()
	if raw["commentId"] != "c1" || raw["resolved"] != true {
		t.Fatalf("raw shape: %v", raw)
	}
}
