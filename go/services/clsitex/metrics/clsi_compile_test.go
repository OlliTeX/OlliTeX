package metrics

import "testing"

func TestLabelKeySortedDeterministic(t *testing.T) {
	// Insertion order must not matter: keys are sorted into the key string.
	a := labelKey(map[string]string{"b": "2", "a": "1"})
	b := labelKey(map[string]string{"a": "1", "b": "2"})
	if a != b {
		t.Fatalf("labelKey not deterministic: %q != %q", a, b)
	}
	if a != "a=1\x00b=2" {
		t.Fatalf("unexpected key encoding: %q", a)
	}
	if got := labelKey(map[string]string{}); got != "" {
		t.Fatalf("empty values: %q, want empty", got)
	}
}

func TestLabeledCounter(t *testing.T) {
	c := NewLabeledCounter("x", "a", "b")
	c.Inc(map[string]string{"a": "1"})
	c.Add(map[string]string{"a": "1"}, 4)
	c.Add(map[string]string{"a": "2"}, 7)
	if got := c.Value(map[string]string{"a": "1"}); got != 5 {
		t.Fatalf("Value(a=1) = %d, want 5 (Inc+Add)", got)
	}
	if got := c.Value(map[string]string{"a": "2"}); got != 7 {
		t.Fatalf("Value(a=2) = %d, want 7", got)
	}
	// Distinct label values are distinct keys; missing values default 0.
	if got := c.Value(map[string]string{"a": "3"}); got != 0 {
		t.Fatalf("absent label = %d, want 0", got)
	}
}

func TestLabeledGauge(t *testing.T) {
	g := NewLabeledGauge("g", "a")
	if got := g.Value(map[string]string{"a": "x"}); got != 0 {
		t.Fatalf("absent gauge = %v, want 0", got)
	}
	g.Set(map[string]string{"a": "x"}, 1.5)
	g.Set(map[string]string{"a": "x"}, 2.5)
	if got := g.Value(map[string]string{"a": "x"}); got != 2.5 {
		t.Fatalf("gauge set last write = %v, want 2.5", got)
	}
	// Other label values unaffected.
	g.Set(map[string]string{"a": "y"}, 9)
	if got := g.Value(map[string]string{"a": "y"}); got != 9 {
		t.Fatalf("gauge y = %v, want 9", got)
	}
}

func TestLabeledHistogram(t *testing.T) {
	h := NewLabeledHistogram("h", "a")
	if c, s := h.Observed(map[string]string{"a": "x"}); c != 0 || s != 0 {
		t.Fatalf("absent histogram = (%d, %v), want (0, 0)", c, s)
	}
	h.Observe(map[string]string{"a": "x"}, 0.5)
	h.Observe(map[string]string{"a": "x"}, 1.5)
	h.Observe(map[string]string{"a": "y"}, 3.0)
	if c, s := h.Observed(map[string]string{"a": "x"}); c != 2 || s != 2.0 {
		t.Fatalf("histogram x = (%d, %v), want (2, 2.0)", c, s)
	}
	if c, s := h.Observed(map[string]string{"a": "y"}); c != 1 || s != 3.0 {
		t.Fatalf("histogram y = (%d, %v), want (1, 3.0)", c, s)
	}
}

func TestGenericObserveIncObserved(t *testing.T) {
	GenericReset()
	defer GenericReset()

	if c, s := GenericObserved("never-observed"); c != 0 || s != 0 {
		t.Fatalf("absent generic = (%d, %v), want (0, 0)", c, s)
	}
	GenericObserve("t", 1.5)
	GenericObserve("t", 2.5)
	if c, s := GenericObserved("t"); c != 2 || s != 4.0 {
		t.Fatalf("generic t = (%d, %v), want (2, 4.0)", c, s)
	}
	// GenericInc routes to the standalone metrics.Count (not the generic
	// histogram): the Count counter under the name is incremented, and the
	// generic sum is untouched.
	GenericReset()
	GenericInc("cnt", 30)
	if got := NamedCounter("cnt").Get(); got != 30 {
		t.Fatalf("Count after GenericInc = %d, want 30", got)
	}
	if c, s := GenericObserved("cnt"); c != 0 || s != 0 {
		t.Fatalf("GenericInc must not touch generic sum: got (%d, %v), want (0, 0)", c, s)
	}
	// A second generic name is independent.
	GenericObserve("u", 10)
	if c, s := GenericObserved("u"); c != 1 || s != 10 {
		t.Fatalf("generic u = (%d, %v), want (1, 10)", c, s)
	}
	GenericReset()
	if c, s := GenericObserved("u"); c != 0 || s != 0 {
		t.Fatalf("after reset = (%d, %v), want (0, 0)", c, s)
	}
}
