package util

import "testing"

func TestSplitURIPath(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"/", []string{""}},
		{"", []string{""}},
		{"/project", []string{"", "project"}},
		{"/project/", []string{"", "project"}},
		{"//", []string{""}},
		{"/a/b/c", []string{"", "a", "b", "c"}},
		{"/0123.git/info/refs", []string{"", "0123.git", "info", "refs"}},
	}
	for _, c := range cases {
		got := SplitURIPath(c.in)
		if len(got) != len(c.want) {
			t.Errorf("SplitURIPath(%q) = %v (len %d), want len %d", c.in, got, len(got), len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("SplitURIPath(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}
