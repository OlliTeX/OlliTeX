package minimatch

import "testing"

func mmOpts(dot bool) *Options { return &Options{Dot: dot, Platform: "posix"} }

func TestSmokeMM(t *testing.T) {
	cases := []struct {
		pat, f    string
		dot, want bool
	}{
		{"a", "a", false, true},
		{"a", "b", false, false},
		{"*", "a", false, true},
		{"*", ".a", false, false},
		{"*", ".a", true, true},
		{"**", "a/b/c", false, true},
		{"**", "a/.b/c", false, false},
		{"b/**", "b/a/c", false, true},
		{"b/**", "b", false, false},
		{"b/**/c", "b/a/c", false, true},
		{"b/**/c", "b/c", false, true},
		{"a/**/c**", "a/b/cxx", false, true},
		{"a/**/c**", "a/b/d/cxx", false, true},
		{"a{,/**}/*.md", "a/b/c.md", false, true},
		{"a{,/**}/*.md", "a/b/c", false, false},
		{"{a,bb/**}/{c,dd}/{e,ff}/g/{h,ii}.txt", "a/c/e/g/h.txt", false, true},
		{"foo/*.txt", "foo/bar.txt", false, true},
		{"foo/**", "foo", false, false},
		{"*.js", "foo.js", false, true},
		{"[a-c]*", "ax", false, true},
		{"[a-c]*", "bx", false, true},
		{"[a-c]*", "zx", false, false},
		{"\\*", "a", false, false},
		{"\\*", "*", false, true},
		{"\\{x\\{y,\\{z\\}\\}\\}", "{x{y,{z}}}", false, true},
		{"\\}", "}", false, true},
		{"\\[\\]", "[]", false, true},
		// negate
		{"!a", "a", false, false},
		{"!a", "b", false, true},
		{"!!a", "a", false, true},
		{"!foo/*.md", "foo/a.md", false, false},
		// CLSI patterns
		{"keepdir/**", "keepdir/a/b.txt", false, true},
		{"keepdir/**", "keepdir/.hidden/x", true, true},
		{"keepdir/**", "keepdir/.hidden/x", false, false},
		{".other/keepdir/**", ".other/keepdir/deep/x.y", false, true},
		{"{keepdir/**,.other/keepdir/**}", "keepdir/a/b.txt", false, true},
	}
	for _, tc := range cases {
		m, err := New(tc.pat, mmOpts(tc.dot))
		if err != nil {
			t.Fatalf("New(%q): %v", tc.pat, err)
		}
		got := m.Match(tc.f)
		if got != tc.want {
			t.Errorf("Match(%q,%q) dot=%v: got %v want %v", tc.f, tc.pat, tc.dot, got, tc.want)
		}
	}
}
