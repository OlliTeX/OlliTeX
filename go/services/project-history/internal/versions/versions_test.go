package versions

import "testing"

// Mirrors test/unit/js/Versions/VersionTest.js (live compare table) with
// additional non-numeric-segment cases probed against live Node
// (app/js/Versions.js) 2026-09-19 (parseInt/NaN semantics).

func TestCompare(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want int
	}{
		{"greater major", "2.1", "1.1", 1},
		{"lesser major", "1.1", "2.1", -1},
		{"equal bare major", "2", "2", 0},
		{"greater minor", "2.3", "2.1", 1},
		{"lesser minor", "2.1", "2.3", -1},
		{"non lexical minor", "2.10", "2.9", 1},
		{"non lexical minor rev", "2.9", "2.10", -1},
		{"major+minor vs major", "2.1", "1", 1},
		{"major vs major+minor", "1", "2.1", -1},
		{"minor vs zero", "2.3", "2.0", 1},
		{"zero vs minor", "2.0", "2.3", -1},
		// parseInt/NaN edge semantics (Node oracle 2026-09-19)
		{"empty vs '1.1'", "", "1.1", -1},
		{"bare garbage", "abc", "1", 0},
		{"garbage equal", "abc", "abc", 0},
		{"garbage vs other garbage", "abc", "def", 0},
		{"negatives", "-1", "0", -1},
		{"negative equal", "-1", "-1", 0},
		{"trailing garbage", "1-2", "1", 0},
		{"empty vs triple", "", "1.1.1", -1},
		{"one vs empty-then-compare", "1", "", 0},
		{"decimal garbage", ".1", ".1", 0},
		{"trailing dot", "1.", "1", 1},
		{"alpha major", ".", "1.0", 0},
		{"alpha major compare", "a.b", "c.d", 0},
		{"trailing alpha minor", "5.x", "5", 1},
		{"leading zero", "01", "1", 0},
		{"1.0 vs 1", "1.0", "1", 1},
		{"1 vs 1.0", "1", "1.0", -1},
		{"2 vs 2.0", "2", "2.0", -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d (node oracle case %q)", c.a, c.b, got, c.want, c.name)
		}
	}
}

func TestPredicates(t *testing.T) {
	cases := []struct {
		name             string
		a, b             string
		gt, gte, lt, lte bool
	}{
		{"greater", "2.1", "1.1", true, true, false, false},
		{"lesser", "1.1", "2.1", false, false, true, true},
		{"equal", "2", "2", false, true, false, true},
	}
	for _, c := range cases {
		if got := GT(c.a, c.b); got != c.gt {
			t.Errorf("%s: GT = %v want %v", c.name, got, c.gt)
		}
		if got := GTE(c.a, c.b); got != c.gte {
			t.Errorf("%s: GTE = %v want %v", c.name, got, c.gte)
		}
		if got := LT(c.a, c.b); got != c.lt {
			t.Errorf("%s: LT = %v want %v", c.name, got, c.lt)
		}
		if got := LTE(c.a, c.b); got != c.lte {
			t.Errorf("%s: LTE = %v want %v", c.name, got, c.lte)
		}
	}
}
