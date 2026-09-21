package otc

import (
	"encoding/json"
	"testing"
)

// expectInverseRestores mirrors the oracle's expectInverseToLeadToInitialState.
func expectInverseRestores(t *testing.T, file *StringFileData, op *TextOperation) {
	t.Helper()
	initial := file.ToRaw()
	inverted := op.Invert(file)
	mustOps(t, op.Apply(file))
	mustOps(t, inverted.Apply(file))
	sameRaw(t, "inverse restored", file.ToRaw(), initial)
}

// composeApply mirrors the oracle's compose(): apply both, compare to composed.
func composeApply(t *testing.T, file *StringFileData, op1, op2 *TextOperation) map[string]any {
	t.Helper()
	fcopy, err := FromRawStringFileData(file.ToRaw())
	if err != nil {
		t.Fatalf("compose: copy: %v", err)
	}
	mustOps(t, op1.Apply(file))
	mustOps(t, op2.Apply(file))
	result1 := file.ToRaw()
	composed, cerr := op1.Compose(op2)
	if cerr != nil {
		t.Fatalf("compose: %v", cerr)
	}
	mustOps(t, composed.Apply(fcopy))
	result2 := fcopy.ToRaw()
	sameRaw(t, "compose vs apply-apply", result2, result1)
	return file.ToRaw()
}

// transformApply mirrors the oracle's transform().
func transformApply(t *testing.T, file *StringFileData, a, b *TextOperation) map[string]any {
	t.Helper()
	aFile, err := FromRawStringFileData(file.ToRaw())
	if err != nil {
		t.Fatalf("transform: copy a: %v", err)
	}
	bFile, err := FromRawStringFileData(file.ToRaw())
	if err != nil {
		t.Fatalf("transform: copy b: %v", err)
	}
	aPrime, bPrime, terr := Transform(a, b)
	if terr != nil {
		t.Fatalf("transform: %v", terr)
	}
	mustOps(t, a.Apply(aFile))
	mustOps(t, bPrime.Apply(aFile))
	mustOps(t, b.Apply(bFile))
	mustOps(t, aPrime.Apply(bFile))
	sameRaw(t, "transform a-path vs b-path", bFile.ToRaw(), aFile.ToRaw())
	return aFile.ToRaw()
}

func stripTrackedChangeTimestamps(raw map[string]any) map[string]any {
	tcs, ok := raw["trackedChanges"].([]map[string]any)
	if !ok {
		return raw
	}
	out := map[string]any{}
	for k, v := range raw {
		out[k] = v
	}
	newTCS := make([]map[string]any, len(tcs))
	for i, tc := range tcs {
		ntc := map[string]any{}
		for k, v := range tc {
			if k != "tracking" {
				ntc[k] = v
			}
		}
		if tr, okTr := tc["tracking"].(map[string]any); okTr {
			ntracking := map[string]any{}
			for k, v := range tr {
				if k != "ts" {
					ntracking[k] = v
				}
			}
			ntc["tracking"] = ntracking
		}
		newTCS[i] = ntc
	}
	out["trackedChanges"] = newTCS
	return out
}

func TestInvertDeterministic(t *testing.T) {
	cases := []struct {
		name string
		file *StringFileData
		op   func() *TextOperation
	}{
		{"re-inserts removed range and comment",
			newFileData(t, "foo bar baz",
				[]map[string]any{{"id": "comment1", "ranges": []any{map[string]any{"pos": 4, "length": 3}}}},
				[]map[string]any{{"range": map[string]any{"pos": 4, "length": 3},
					"tracking": map[string]any{"ts": "2024-01-01T00:00:00.000Z", "type": "insert", "userId": "user1"}}}),
			func() *TextOperation {
				o := NewTextOperation()
				mustOps(t, o.Retain(4, RetainBuilderOpts{}))
				mustOps(t, o.Remove(4))
				mustOps(t, o.Retain(3, RetainBuilderOpts{}))
				return o
			}},
		{"deletes inserted range and comment",
			newFileData(t, "foo baz",
				[]map[string]any{{"id": "comment1", "ranges": []any{}, "resolved": false}}, nil),
			func() *TextOperation {
				o := NewTextOperation()
				mustOps(t, o.Retain(4, RetainBuilderOpts{}))
				mustOps(t, o.Insert("bar", InsertBuilderOpts{CommentIds: []string{"comment1"},
					Tracking: NewTrackingProps("insert", "user1", ts(t, "2024-01-01T00:00:00.000Z"))}))
				mustOps(t, o.Insert(" ", InsertBuilderOpts{}))
				mustOps(t, o.Retain(3, RetainBuilderOpts{}))
				return o
			}},
		{"removes a tracked delete",
			newFileData(t, "foo bar baz", nil, nil),
			func() *TextOperation {
				o := NewTextOperation()
				mustOps(t, o.Retain(4, RetainBuilderOpts{}))
				mustOps(t, o.Retain(4, RetainBuilderOpts{
					Tracking: NewTrackingProps("delete", "user1", ts(t, "2023-01-01T00:00:00.000Z"))}))
				mustOps(t, o.Retain(3, RetainBuilderOpts{}))
				return o
			}},
		{"restores comments that were removed",
			newFileData(t, "foo bar baz",
				[]map[string]any{{"id": "comment1", "ranges": []any{map[string]any{"pos": 4, "length": 3}}, "resolved": false}}, nil),
			func() *TextOperation {
				o := NewTextOperation()
				mustOps(t, o.Retain(4, RetainBuilderOpts{}))
				mustOps(t, o.Remove(4))
				mustOps(t, o.Retain(3, RetainBuilderOpts{}))
				return o
			}},
		{"re-inserting removed part of comment restores original comment range",
			newFileData(t, "foo bar baz",
				[]map[string]any{{"id": "comment1", "ranges": []any{map[string]any{"pos": 0, "length": 11}}, "resolved": false}}, nil),
			func() *TextOperation {
				o := NewTextOperation()
				mustOps(t, o.Retain(4, RetainBuilderOpts{}))
				mustOps(t, o.Remove(4))
				mustOps(t, o.Retain(3, RetainBuilderOpts{}))
				return o
			}},
		{"re-inserting removed part of tracked change restores tracked change range",
			newFileData(t, "foo bar baz", nil,
				[]map[string]any{{"range": map[string]any{"pos": 0, "length": 11},
					"tracking": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "type": "delete", "userId": "user1"}}}),
			func() *TextOperation {
				o := NewTextOperation()
				mustOps(t, o.Retain(4, RetainBuilderOpts{}))
				mustOps(t, o.Remove(4))
				mustOps(t, o.Retain(3, RetainBuilderOpts{}))
				return o
			}},
	}
	for _, c := range cases {
		file := c.file
		op := c.op()
		expectInverseRestores(t, file, op)
	}

	// undoing a tracked delete restores the tracked changes
	now := ts(t, "2026-07-10T00:00:00.000Z")
	file := newFileData(t, "the quick brown fox jumps over the lazy dog", nil,
		[]map[string]any{
			{"range": map[string]any{"pos": 5, "length": 5}, "tracking": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "type": "insert", "userId": "user1"}},
			{"range": map[string]any{"pos": 12, "length": 3}, "tracking": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "type": "delete", "userId": "user1"}},
			{"range": map[string]any{"pos": 18, "length": 5}, "tracking": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "type": "insert", "userId": "user1"}},
		})
	o := NewTextOperation()
	mustOps(t, o.Retain(7, RetainBuilderOpts{}))
	mustOps(t, o.Retain(13, RetainBuilderOpts{Tracking: NewTrackingProps("delete", "user1", now)}))
	mustOps(t, o.Retain(23, RetainBuilderOpts{}))
	expectInverseRestores(t, file, o)
}

func TestInvertRandomised(t *testing.T) {
	tr := newTestRand(42)
	for i := 0; i < 500; i++ {
		str := tr.randString(50, true)
		ids, rawComments := tr.randComments(6)
		o := tr.randOperation(str, ids)
		original := newFileData(t, str, rawComments, nil)
		p := o.Invert(original)
		if o.BaseLength != p.TargetLength {
			t.Fatalf("trial %d: o.baseLength=%d != p.targetLength=%d (op=%v)", i, o.BaseLength, p.TargetLength, o.Ops)
		}
		if o.TargetLength != p.BaseLength {
			t.Fatalf("trial %d: o.targetLength=%d != p.baseLength=%d (op=%v)", i, o.TargetLength, p.BaseLength, o.Ops)
		}
		file := newFileData(t, str, rawComments, nil)
		if err := o.Apply(file); err != nil {
			t.Fatalf("trial %d: apply o: %v", i, err)
		}
		if err := p.Apply(file); err != nil {
			t.Fatalf("trial %d: apply p: %v", i, err)
		}
		sameRaw(t, "invert round-trip", file.ToRaw(), original.ToRaw())
	}
}

func TestApplyRandomised(t *testing.T) {
	tr := newTestRand(7)
	for i := 0; i < 500; i++ {
		str := tr.randString(50, true)
		ids, rawComments := tr.randComments(6)
		o := tr.randOperation(str, ids)
		if len(str) != o.BaseLength {
			t.Fatalf("trial %d: len(str)=%d != baseLength=%d", i, len(str), o.BaseLength)
		}
		file := newFileData(t, str, rawComments, nil)
		if err := o.Apply(file); err != nil {
			t.Fatalf("trial %d: apply: %v", i, err)
		}
		result := file.Content
		if len(result) != o.TargetLength {
			t.Fatalf("trial %d: result len=%d != targetLength=%d", i, len(result), o.TargetLength)
		}
	}
}

func TestJSONRoundTripRandomised(t *testing.T) {
	tr := newTestRand(11)
	for i := 0; i < 500; i++ {
		doc := tr.randString(50, true)
		ids, _ := tr.randComments(2)
		op := tr.randOperation(doc, ids)
		rt, err := FromJSONTextOperation(op.ToJSON())
		if err != nil {
			t.Fatalf("trial %d: fromJSON: %v", i, err)
		}
		if !op.Equals(rt) {
			t.Fatalf("trial %d: op not equal to round-trip", i)
		}
	}
}

func TestComposeRejectsDifferentBase(t *testing.T) {
	a := NewTextOperation()
	mustOps(t, a.Retain(4, RetainBuilderOpts{}))
	b := NewTextOperation()
	mustOps(t, b.Retain(7, RetainBuilderOpts{}))
	_, err := a.Compose(b)
	if err == nil {
		t.Fatal("compose with mismatched lengths should error")
	}
	assertErrType[*UnprocessableError](t, "compose", err)
}

func TestComposeDeterministic(t *testing.T) {
	// composes two operations with comments
	got := composeApply(t,
		newFileData(t, "foo baz",
			[]map[string]any{{"id": "comment1", "ranges": []any{}, "resolved": false}}, nil),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(4, RetainBuilderOpts{}))
			mustOps(t, o.Insert("bar", InsertBuilderOpts{CommentIds: []string{"comment1"},
				Tracking: NewTrackingProps("insert", "user1", ts(t, "2024-01-01T00:00:00.000Z"))}))
			mustOps(t, o.Insert(" ", InsertBuilderOpts{}))
			mustOps(t, o.Retain(3, RetainBuilderOpts{}))
			return o
		}(),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(4, RetainBuilderOpts{}))
			mustOps(t, o.Remove(4))
			mustOps(t, o.Retain(3, RetainBuilderOpts{}))
			return o
		}(),
	)
	sameRaw(t, "comments compose", got, map[string]any{
		"content":  "foo baz",
		"comments": []map[string]any{{"id": "comment1", "ranges": []map[string]any{}}},
	})

	// prioritizes tracked changes info from the latter operation
	trackedRaw := []map[string]any{{
		"range":    map[string]any{"pos": 4, "length": 4},
		"tracking": map[string]any{"ts": "2024-01-01T00:00:00.000Z", "type": "delete", "userId": "user2"},
	}}
	got2 := composeApply(t,
		newFileData(t, "foo bar baz", nil, nil),
		trackedOp(t, "2023-01-01T00:00:00.000Z", "user1"),
		trackedOp(t, "2024-01-01T00:00:00.000Z", "user2"),
	)
	sameRaw(t, "latter op wins", got2, map[string]any{
		"content":        "foo bar baz",
		"trackedChanges": trackedRaw,
	})

	// does not remove tracked change if not overriden
	got3 := composeApply(t,
		newFileData(t, "foo bar baz", nil, nil),
		trackedOp(t, "2023-01-01T00:00:00.000Z", "user1"),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(11, RetainBuilderOpts{}))
			return o
		}(),
	)
	sameRaw(t, "not overriden", got3, map[string]any{
		"content": "foo bar baz",
		"trackedChanges": []map[string]any{{
			"range":    map[string]any{"pos": 4, "length": 4},
			"tracking": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "type": "delete", "userId": "user1"},
		}},
	})

	// adds comment ranges from both operations
	got4 := composeApply(t,
		newFileData(t, "foo bar baz",
			[]map[string]any{
				{"id": "comment1", "ranges": []any{map[string]any{"pos": 4, "length": 3}}, "resolved": false},
				{"id": "comment2", "ranges": []any{map[string]any{"pos": 8, "length": 3}}, "resolved": false},
			}, nil),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(5, RetainBuilderOpts{}))
			mustOps(t, o.Insert("aa", InsertBuilderOpts{CommentIds: []string{"comment1"}}))
			mustOps(t, o.Retain(6, RetainBuilderOpts{}))
			return o
		}(),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(11, RetainBuilderOpts{}))
			mustOps(t, o.Insert("bb", InsertBuilderOpts{CommentIds: []string{"comment2"}}))
			mustOps(t, o.Retain(2, RetainBuilderOpts{}))
			return o
		}(),
	)
	sameRaw(t, "comment ranges both ops", got4, map[string]any{
		"content": "foo baaar bbbaz",
		"comments": []map[string]any{
			{"id": "comment1", "ranges": []map[string]any{{"pos": 4, "length": 5}}},
			{"id": "comment2", "ranges": []map[string]any{{"pos": 10, "length": 5}}},
		},
	})

	// removes the tracking range from a tracked delete if operation 2 resolves it
	got5 := composeApply(t,
		newFileData(t, "foo bar baz", nil, nil),
		trackedOp(t, "2023-01-01T00:00:00.000Z", "user1"),
		clearOp(t),
	)
	sameRaw(t, "op2 clears delete tracking", got5, map[string]any{"content": "foo bar baz"})

	// removes the tracking from an insert if operation 2 resolves it
	got6 := composeApply(t,
		newFileData(t, "foo bar baz", nil, nil),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(4, RetainBuilderOpts{}))
			mustOps(t, o.Insert("quux ", InsertBuilderOpts{
				Tracking: NewTrackingProps("insert", "user1", ts(t, "2023-01-01T00:00:00.000Z"))}))
			mustOps(t, o.Retain(7, RetainBuilderOpts{}))
			return o
		}(),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(6, RetainBuilderOpts{}))
			mustOps(t, o.Retain(5, RetainBuilderOpts{Tracking: ClearTrackingProps{}}))
			mustOps(t, o.Retain(5, RetainBuilderOpts{}))
			return o
		}(),
	)
	sameRaw(t, "op2 clears insert tracking", got6, map[string]any{
		"content": "foo quux bar baz",
		"trackedChanges": []map[string]any{{
			"range":    map[string]any{"pos": 4, "length": 2},
			"tracking": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "type": "insert", "userId": "user1"},
		}},
	})
}

func trackedOp(t *testing.T, tsString, user string) *TextOperation {
	o := NewTextOperation()
	mustOps(t, o.Retain(4, RetainBuilderOpts{}))
	mustOps(t, o.Retain(4, RetainBuilderOpts{Tracking: NewTrackingProps("delete", user, ts(t, tsString))}))
	mustOps(t, o.Retain(3, RetainBuilderOpts{}))
	return o
}

func clearOp(t *testing.T) *TextOperation {
	o := NewTextOperation()
	mustOps(t, o.Retain(4, RetainBuilderOpts{}))
	mustOps(t, o.Retain(4, RetainBuilderOpts{Tracking: ClearTrackingProps{}}))
	mustOps(t, o.Retain(3, RetainBuilderOpts{}))
	return o
}

func TestComposeAssociativityTimestamp(t *testing.T) {
	str := "AB"
	a, err := FromJSONTextOperation(map[string]any{"textOperation": []any{
		map[string]any{"r": 2, "tracking": map[string]any{"type": "delete", "userId": "user1", "ts": "2023-01-01T00:00:00.000Z"}},
	}})
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := FromJSONTextOperation(map[string]any{"textOperation": []any{
		map[string]any{"r": 1, "tracking": map[string]any{"type": "delete", "userId": "user1", "ts": "2022-01-01T00:00:00.000Z"}},
		1,
	}})
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	c, err := FromJSONTextOperation(map[string]any{"textOperation": []any{1, "X", 1}})
	if err != nil {
		t.Fatalf("c: %v", err)
	}
	ab, cerr := a.Compose(b)
	if cerr != nil {
		t.Fatalf("a.compose(b): %v", cerr)
	}
	ab_c, cerr := ab.Compose(c)
	if cerr != nil {
		t.Fatalf("ab.compose(c): %v", cerr)
	}
	bc, cerr := b.Compose(c)
	if cerr != nil {
		t.Fatalf("b.compose(c): %v", cerr)
	}
	a_bc, cerr := a.Compose(bc)
	if cerr != nil {
		t.Fatalf("a.compose(bc): %v", cerr)
	}
	ab_c_file := newFileData(t, str, nil, nil)
	mustOps(t, ab_c.Apply(ab_c_file))
	a_bc_file := newFileData(t, str, nil, nil)
	mustOps(t, a_bc.Apply(a_bc_file))

	raw1 := ab_c_file.ToRaw()
	raw2 := a_bc_file.ToRaw()
	if sameRawJSON(raw1, raw2) {
		t.Fatal("the two orders should differ on timestamps")
	}
	sameRaw(t, "ignoring timestamps", stripTrackedChangeTimestamps(raw1), stripTrackedChangeTimestamps(raw2))
}

func sameRawJSON(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func TestComposeRandomised(t *testing.T) {
	tr := newTestRand(13)
	for i := 0; i < 500; i++ {
		str := tr.randString(20, true)
		ids, rawComments := tr.randComments(6)
		a := tr.randOperation(str, ids)
		file := newFileData(t, str, rawComments, nil)
		if err := a.Apply(file); err != nil {
			t.Fatalf("trial %d: apply a: %v", i, err)
		}
		afterA := file.Content
		if len(afterA) != a.TargetLength {
			t.Fatalf("trial %d: afterA len=%d != a.target=%d", i, len(afterA), a.TargetLength)
		}
		b := tr.randOperation(afterA, ids)
		if err := b.Apply(file); err != nil {
			t.Fatalf("trial %d: apply b: %v", i, err)
		}
		afterB := file.ToRaw()
		if len(file.Content) != b.TargetLength {
			t.Fatalf("trial %d: afterB len=%d != b.target=%d", i, len(file.Content), b.TargetLength)
		}
		ab, cerr := a.Compose(b)
		if cerr != nil {
			t.Fatalf("trial %d: compose: %v", i, cerr)
		}
		if ab.TargetLength != b.TargetLength {
			t.Fatalf("trial %d: ab.target=%d != b.target=%d", i, ab.TargetLength, b.TargetLength)
		}
		abFile := newFileData(t, str, rawComments, nil)
		if err := ab.Apply(abFile); err != nil {
			t.Fatalf("trial %d: apply ab: %v", i, err)
		}
		sameRaw(t, "compose invariant", abFile.ToRaw(), afterB)
	}
}

func TestComposeAssociativityRandomised(t *testing.T) {
	tr := newTestRand(23)
	for i := 0; i < 300; i++ {
		str := tr.randString(20, true)
		ids, rawComments := tr.randComments(6)
		a := tr.randOperation(str, ids)
		afterA := newFileData(t, str, rawComments, nil)
		if err := a.Apply(afterA); err != nil {
			t.Fatalf("trial %d: apply a: %v", i, err)
		}
		b := tr.randOperation(afterA.Content, ids)
		afterB := newFileData(t, afterA.Content, rawComments, nil)
		if err := b.Apply(afterB); err != nil {
			t.Fatalf("trial %d: apply b: %v", i, err)
		}
		c := tr.randOperation(afterB.Content, ids)

		ab, cerr := a.Compose(b)
		if cerr != nil {
			t.Fatalf("trial %d: a.compose(b): %v", i, cerr)
		}
		ab_c, cerr := ab.Compose(c)
		if cerr != nil {
			t.Fatalf("trial %d: ab.compose(c): %v", i, cerr)
		}
		bc, cerr := b.Compose(c)
		if cerr != nil {
			t.Fatalf("trial %d: b.compose(c): %v", i, cerr)
		}
		a_bc, cerr := a.Compose(bc)
		if cerr != nil {
			t.Fatalf("trial %d: a.compose(bc): %v", i, cerr)
		}
		ab_c_file := newFileData(t, str, rawComments, nil)
		if err := ab_c.Apply(ab_c_file); err != nil {
			t.Fatalf("trial %d: apply ab_c: %v", i, err)
		}
		a_bc_file := newFileData(t, str, rawComments, nil)
		if err := a_bc.Apply(a_bc_file); err != nil {
			t.Fatalf("trial %d: apply a_bc: %v", i, err)
		}
		sameRaw(t, "associativity (no ts)", stripTrackedChangeTimestamps(ab_c_file.ToRaw()), stripTrackedChangeTimestamps(a_bc_file.ToRaw()))
	}
}

func TestTransformRejectsDifferentBase(t *testing.T) {
	a := NewTextOperation()
	mustOps(t, a.Retain(4, RetainBuilderOpts{}))
	b := NewTextOperation()
	mustOps(t, b.Retain(7, RetainBuilderOpts{}))
	_, _, err := Transform(a, b)
	if err == nil {
		t.Fatal("transform with mismatched bases should error")
	}
	assertErrType[*UnprocessableError](t, "transform", err)
}

func TestTransformDeterministic(t *testing.T) {
	// chooses lower tracked change timestamp
	ts1 := "2024-01-01T01:00:00.000Z"
	ts2 := "2024-01-01T02:00:00.000Z"
	a := NewTextOperation()
	mustOps(t, a.Retain(2, RetainBuilderOpts{Tracking: NewTrackingProps("insert", "user1", ts(t, ts1))}))
	mustOps(t, a.Retain(1, RetainBuilderOpts{}))
	mustOps(t, a.Retain(2, RetainBuilderOpts{Tracking: NewTrackingProps("insert", "user1", ts(t, ts2))}))
	b := NewTextOperation()
	mustOps(t, b.Retain(1, RetainBuilderOpts{}))
	mustOps(t, b.Remove(3))
	mustOps(t, b.Retain(1, RetainBuilderOpts{}))
	aPrime, bPrime, err := Transform(a, b)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	aComposeBPrime, cerr := a.Compose(bPrime)
	if cerr != nil {
		t.Fatalf("a.compose(b'): %v", cerr)
	}
	bComposeAPrime, cerr := b.Compose(aPrime)
	if cerr != nil {
		t.Fatalf("b.compose(a'): %v", cerr)
	}
	aBPFile := newFileData(t, "abcde", nil, nil)
	mustOps(t, aComposeBPrime.Apply(aBPFile))
	bAPFile := newFileData(t, "abcde", nil, nil)
	mustOps(t, bComposeAPrime.Apply(bAPFile))
	sameRaw(t, "lower ts transform", bAPFile.ToRaw(), aBPFile.ToRaw())
	if got := aBPFile.TrackedChanges.Len(); got != 1 {
		t.Fatalf("trackedChanges = %d, want 1", got)
	}
	sorted := aBPFile.TrackedChanges.AsSorted()
	if tp, ok := asTrackingProps(sorted[0].Tracking); !ok || isoDate(tp.TS) != ts1 {
		t.Fatalf("ts = %v, want %s", sorted[0].Tracking, ts1)
	}

	// adds a tracked change from operation 1
	got2 := transformApply(t,
		newFileData(t, "foo baz", nil, nil),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(4, RetainBuilderOpts{}))
			mustOps(t, o.Insert("bar", InsertBuilderOpts{Tracking: NewTrackingProps("insert", "user1", ts(t, "2024-01-01T00:00:00.000Z"))}))
			mustOps(t, o.Insert(" ", InsertBuilderOpts{}))
			mustOps(t, o.Retain(3, RetainBuilderOpts{}))
			return o
		}(),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(7, RetainBuilderOpts{}))
			mustOps(t, o.Insert(" qux", InsertBuilderOpts{}))
			return o
		}(),
	)
	sameRaw(t, "transform adds op1 tracked", got2, map[string]any{
		"content": "foo bar baz qux",
		"trackedChanges": []map[string]any{{
			"range":    map[string]any{"pos": 4, "length": 3},
			"tracking": map[string]any{"ts": "2024-01-01T00:00:00.000Z", "type": "insert", "userId": "user1"},
		}},
	})

	// prioritizes tracked change from the first operation
	got3 := transformApply(t,
		newFileData(t, "foo bar baz", nil, nil),
		trackedOp(t, "2023-01-01T00:00:00.000Z", "user1"),
		trackedOp(t, "2024-01-01T00:00:00.000Z", "user2"),
	)
	sameRaw(t, "transform op1 tracked", got3, map[string]any{
		"content": "foo bar baz",
		"trackedChanges": []map[string]any{{
			"range":    map[string]any{"pos": 4, "length": 4},
			"tracking": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "type": "delete", "userId": "user1"},
		}},
	})

	// splits a tracked change in two to resolve conflicts
	got4 := transformApply(t,
		newFileData(t, "foo bar baz", nil, nil),
		trackedOp(t, "2023-01-01T00:00:00.000Z", "user1"),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(4, RetainBuilderOpts{}))
			mustOps(t, o.Retain(5, RetainBuilderOpts{Tracking: NewTrackingProps("delete", "user2", ts(t, "2024-01-01T00:00:00.000Z"))}))
			mustOps(t, o.Retain(2, RetainBuilderOpts{}))
			return o
		}(),
	)
	sameRaw(t, "transform splits tracked", got4, map[string]any{
		"content": "foo bar baz",
		"trackedChanges": []map[string]any{
			{"range": map[string]any{"pos": 4, "length": 4}, "tracking": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "type": "delete", "userId": "user1"}},
			{"range": map[string]any{"pos": 8, "length": 1}, "tracking": map[string]any{"ts": "2024-01-01T00:00:00.000Z", "type": "delete", "userId": "user2"}},
		},
	})

	// inserts a tracked change from operation 2 after one from operation 1
	got5 := transformApply(t,
		newFileData(t, "aaabbbccc", nil, nil),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(3, RetainBuilderOpts{}))
			mustOps(t, o.Insert("xxx", InsertBuilderOpts{Tracking: NewTrackingProps("insert", "user1", ts(t, "2023-01-01T00:00:00.000Z"))}))
			mustOps(t, o.Retain(6, RetainBuilderOpts{}))
			return o
		}(),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(3, RetainBuilderOpts{}))
			mustOps(t, o.Insert("yyy", InsertBuilderOpts{Tracking: NewTrackingProps("insert", "user2", ts(t, "2024-01-01T00:00:00.000Z"))}))
			mustOps(t, o.Retain(6, RetainBuilderOpts{}))
			return o
		}(),
	)
	sameRaw(t, "transform insert both", got5, map[string]any{
		"content": "aaaxxxyyybbbccc",
		"trackedChanges": []map[string]any{
			{"range": map[string]any{"pos": 3, "length": 3}, "tracking": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "type": "insert", "userId": "user1"}},
			{"range": map[string]any{"pos": 6, "length": 3}, "tracking": map[string]any{"ts": "2024-01-01T00:00:00.000Z", "type": "insert", "userId": "user2"}},
		},
	})

	// preserves a comment even if it is completely removed in one operation
	got6 := transformApply(t,
		newFileData(t, "foo bar baz",
			[]map[string]any{{"id": "comment1", "ranges": []any{map[string]any{"pos": 4, "length": 3}}, "resolved": false}}, nil),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(4, RetainBuilderOpts{}))
			mustOps(t, o.Remove(4))
			mustOps(t, o.Retain(3, RetainBuilderOpts{}))
			return o
		}(),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(7, RetainBuilderOpts{}))
			mustOps(t, o.Insert("qux ", InsertBuilderOpts{CommentIds: []string{"comment1"}}))
			mustOps(t, o.Retain(4, RetainBuilderOpts{}))
			return o
		}(),
	)
	sameRaw(t, "transform preserve comment", got6, map[string]any{
		"content":  "foo qux baz",
		"comments": []map[string]any{{"id": "comment1", "ranges": []map[string]any{{"pos": 4, "length": 4}}}},
	})

	// extends a comment to both ranges if both operations add text in it
	got7 := transformApply(t,
		newFileData(t, "foo bar baz",
			[]map[string]any{{"id": "comment1", "ranges": []any{map[string]any{"pos": 4, "length": 3}}, "resolved": false}}, nil),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(4, RetainBuilderOpts{}))
			mustOps(t, o.Insert("qux ", InsertBuilderOpts{CommentIds: []string{"comment1"}}))
			mustOps(t, o.Retain(7, RetainBuilderOpts{}))
			return o
		}(),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(4, RetainBuilderOpts{}))
			mustOps(t, o.Insert("corge ", InsertBuilderOpts{CommentIds: []string{"comment1"}}))
			mustOps(t, o.Retain(7, RetainBuilderOpts{}))
			return o
		}(),
	)
	sameRaw(t, "transform extend comment", got7, map[string]any{
		"content":  "foo qux corge bar baz",
		"comments": []map[string]any{{"id": "comment1", "ranges": []map[string]any{{"pos": 4, "length": 13}}}},
	})

	// adds a tracked change from both operations at different places
	got8 := transformApply(t,
		newFileData(t, "foo bar baz", nil, nil),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(4, RetainBuilderOpts{}))
			mustOps(t, o.Insert("qux ", InsertBuilderOpts{Tracking: NewTrackingProps("insert", "user1", ts(t, "2023-01-01T00:00:00.000Z"))}))
			mustOps(t, o.Retain(7, RetainBuilderOpts{}))
			return o
		}(),
		func() *TextOperation {
			o := NewTextOperation()
			mustOps(t, o.Retain(8, RetainBuilderOpts{}))
			mustOps(t, o.Insert("corge ", InsertBuilderOpts{Tracking: NewTrackingProps("insert", "user2", ts(t, "2024-01-01T00:00:00.000Z"))}))
			mustOps(t, o.Retain(3, RetainBuilderOpts{}))
			return o
		}(),
	)
	sameRaw(t, "transform tracked both", got8, map[string]any{
		"content": "foo qux bar corge baz",
		"trackedChanges": []map[string]any{
			{"range": map[string]any{"pos": 4, "length": 4}, "tracking": map[string]any{"ts": "2023-01-01T00:00:00.000Z", "type": "insert", "userId": "user1"}},
			{"range": map[string]any{"pos": 12, "length": 6}, "tracking": map[string]any{"ts": "2024-01-01T00:00:00.000Z", "type": "insert", "userId": "user2"}},
		},
	})
}

func TestTransformRandomised(t *testing.T) {
	tr := newTestRand(31)
	for i := 0; i < 500; i++ {
		str := tr.randString(20, true)
		ids, rawComments := tr.randComments(6)
		a := tr.randOperation(str, ids)
		b := tr.randOperation(str, ids)
		aPrime, bPrime, err := Transform(a, b)
		if err != nil {
			t.Fatalf("trial %d: transform: %v", i, err)
		}
		abPrime, cerr := a.Compose(bPrime)
		if cerr != nil {
			t.Fatalf("trial %d: a.compose(b'): %v", i, cerr)
		}
		baPrime, cerr := b.Compose(aPrime)
		if cerr != nil {
			t.Fatalf("trial %d: b.compose(a'): %v", i, cerr)
		}
		abFile := newFileData(t, str, rawComments, nil)
		if err := abPrime.Apply(abFile); err != nil {
			t.Fatalf("trial %d: apply ab': %v", i, err)
		}
		baFile := newFileData(t, str, rawComments, nil)
		if err := baPrime.Apply(baFile); err != nil {
			t.Fatalf("trial %d: apply ba': %v", i, err)
		}
		sameRaw(t, "transform invariant", baFile.ToRaw(), abFile.ToRaw())
	}
}
