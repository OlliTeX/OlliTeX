package diffcodec

import "testing"

func TestDiffAsShareJsOp(t *testing.T) {
	cases := []struct {
		name   string
		before []string
		after  []string
		want   []Op
	}{
		{
			"insert", []string{"hello world"}, []string{"hello beautiful world"},
			[]Op{{I: strPtr("beautiful "), P: 6}},
		},
		{
			"shift-later-inserts",
			[]string{"the boy played with the ball"},
			[]string{"the tall boy played with the red ball"},
			[]Op{{I: strPtr("tall "), P: 4}, {I: strPtr("red "), P: 29}},
		},
		{
			"delete", []string{"hello beautiful world"}, []string{"hello world"},
			[]Op{{D: strPtr("beautiful "), P: 6}},
		},
		{
			"shift-later-deletes",
			[]string{"the tall boy played with the red ball"},
			[]string{"the boy played with the ball"},
			[]Op{{D: strPtr("tall "), P: 4}, {D: strPtr("red "), P: 24}},
		},
		{
			"noop", []string{"hello"}, []string{"hello"},
			[]Op{},
		},
		{
			"multiline-spanning-diff", []string{"one", "two"}, []string{"one X", "two"},
			[]Op{{I: strPtr(" X"), P: 3}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := DiffAsShareJsOp(c.before, c.after)
			if err != nil {
				t.Fatalf("err = %v, want none", err)
			}
			if !opsEqual(got, c.want) {
				t.Fatalf("got %#v, want %#v", got, c.want)
			}
		})
	}
}

func opsEqual(got, want []Op) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		g, w := got[i], want[i]
		if (g.I == nil) != (w.I == nil) {
			return false
		}
		if (g.D == nil) != (w.D == nil) {
			return false
		}
		if g.I != nil && *g.I != *w.I {
			return false
		}
		if g.D != nil && *g.D != *w.D {
			return false
		}
		if g.P != w.P {
			return false
		}
	}
	return true
}

func TestConstants(t *testing.T) {
	if ADDED != 1 || REMOVED != -1 || UNCHANGED != 0 {
		t.Fatalf("constants drifted: ADDED=%d REMOVED=%d UNCHANGED=%d", ADDED, REMOVED, UNCHANGED)
	}
}

func strPtr(s string) *string { return &s }
