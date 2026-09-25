package core

// TextOp transform tests — mirror of the Node oracle TextOperation.transform.
// Expected prime JSON (toJSON) is taken verbatim from oracle probe runs
// (probe.js / probe4.js) against the overleaf-editor-core library.

import (
	"encoding/json"
	"errors"
	"testing"
)

func jsonRawScanOps(t *testing.T, wires ...string) []*ScanOp {
	t.Helper()
	ops := make([]*ScanOp, len(wires))
	for i, w := range wires {
		op, err := ScanOpFromRaw(json.RawMessage(w))
		if err != nil {
			t.Fatalf("ScanOpFromRaw(%s): %v", w, err)
		}
		ops[i] = op
	}
	return ops
}

// transformTextCase runs (a, b).TextOpTransform and asserts the primes' raw
// (wire) shapes.
func transformTextCase(t *testing.T, name string, aWires, bWires []string, aPrime, bPrime string) {
	t.Helper()
	a := &TextOp{Ops: jsonRawScanOps(t, aWires...)}
	b := &TextOp{Ops: jsonRawScanOps(t, bWires...)}
	ap, bp, err := TextOpTransform(a, b)
	if err != nil {
		t.Fatalf("%s: TextOpTransform: %v", name, err)
	}
	if got := string(ap.ToRaw()); got != aPrime {
		t.Errorf("%s: a' = %s, want %s", name, got, aPrime)
	}
	if got := string(bp.ToRaw()); got != bPrime {
		t.Errorf("%s: b' = %s, want %s", name, got, bPrime)
	}
}

func TestTextOpTransformOracle(t *testing.T) {
	t.Parallel()

	// (A/B) a = R4, I"bar", I" ", R3 ; b = R7, I" qux"
	transformTextCase(t, "merge-adjacent-inserts",
		[]string{`4`, `"bar"`, `" "`, `3`},
		[]string{`7`, `" qux"`},
		`{"textOperation":[4,"bar ",7]}`,
		`{"textOperation":[11," qux"]}`,
	)

	// (C/D) a = I"zero\n", R14 ; b = R14, I"four\n"
	transformTextCase(t, "insert-then-retain",
		[]string{`"zero\n"`, `14`},
		[]string{`14`, `"four\n"`},
		`{"textOperation":["zero\n",19]}`,
		`{"textOperation":[19,"four\n"]}`,
	)

	// (F) a = R4, D4, R3 ; b = R7, I"qux "(c1), R4
	transformTextCase(t, "remove-vs-retain-insert",
		[]string{`4`, `-4`, `3`},
		[]string{`7`, `{"i":"qux ","commentIds":["c1"]}`, `4`},
		`{"textOperation":[4,-3,4,-1,3]}`,
		`{"textOperation":[4,{"i":"qux ","commentIds":["c1"]},3]}`,
	)

	// (G) a = R4, I"qux "(c1), R7 ; b = R4, I"corge "(c1), R7
	transformTextCase(t, "inserts-with-comment-ids",
		[]string{`4`, `{"i":"qux ","commentIds":["c1"]}`, `7`},
		[]string{`4`, `{"i":"corge ","commentIds":["c1"]}`, `7`},
		`{"textOperation":[4,{"i":"qux ","commentIds":["c1"]},13]}`,
		`{"textOperation":[8,{"i":"corge ","commentIds":["c1"]},7]}`,
	)

	// (K/L) a = I"abc", I"def" ; b = I"ghi", I"jkl"
	transformTextCase(t, "inserts-at-start",
		[]string{`"abc"`, `"def"`},
		[]string{`"ghi"`, `"jkl"`},
		`{"textOperation":["abcdef",6]}`,
		`{"textOperation":[6,"ghijkl"]}`,
	)
}

func TestTextOpTransformBuilderMerges(t *testing.T) {
	t.Parallel()

	// retain then remove then retain => [1, -2, 3]
	o := NewTextOp(NewRetain(1, nil))
	o.buildRemove(2)
	o.buildRetain(3, nil)
	if got := string(o.ToRaw()); got != `{"textOperation":[1,-2,3]}` {
		t.Errorf("builder1 = %s, want {\"textOperation\":[1,-2,3]}", got)
	}

	// insert then insert then remove => ["aabb", -1]
	t2 := NewTextOp(NewInsert("aa", nil, nil))
	t2.buildInsert("bb", nil, nil)
	t2.buildRemove(1)
	if got := string(t2.ToRaw()); got != `{"textOperation":["aabb",-1]}` {
		t.Errorf("builder2 = %s, want {\"textOperation\":[\"aabb\",-1]}", got)
	}

	// remove then remove => -5
	t3 := NewTextOp(NewRemove(2))
	t3.buildRemove(3)
	if got := string(t3.ToRaw()); got != `{"textOperation":[-5]}` {
		t.Errorf("builder3 = %s, want {\"textOperation\":[-5]}", got)
	}

	// insert after remove is swapped so insert comes first
	t4 := NewTextOp(NewRemove(2))
	t4.buildInsert("x", nil, nil)
	if got := string(t4.ToRaw()); got != `{"textOperation":["x",-2]}` {
		t.Errorf("builder4 = %s, want {\"textOperation\":[\"x\",-2]}", got)
	}
}

func TestTextOpTransformBaseLengthMismatch(t *testing.T) {
	t.Parallel()
	// a = I"abc" (base 0) ; b = R5, I"|" (base 5)
	a := &TextOp{Ops: jsonRawScanOps(t, `"abc"`)}
	b := &TextOp{Ops: jsonRawScanOps(t, `5`, `"|"`)}
	_, _, err := TextOpTransform(a, b)
	var ue *UnprocessableError
	if !errors.As(err, &ue) {
		t.Errorf("base length mismatch: got %v, want *UnprocessableError", err)
	}
}
