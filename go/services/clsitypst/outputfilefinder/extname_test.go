package outputfilefinder

import "testing"

func TestNodeExtname(t *testing.T) {
	cases := map[string]string{
		"output.pdf":       ".pdf",
		"file.tex":         ".tex",
		"":                 "",
		"a.tar.gz":         ".gz",
		"file.":            ".",
		".hidden":          "",
		"..":               "",
		".tarball":         "",
		"noext":            "",
		"a.b.c.tarball":    ".tarball",
		"weird..txt":       ".txt",
		"x.y.z":            ".z",
		"/abs/file.doc":    ".doc",
		"rel/dir/file.rtf": ".rtf",
		"dir/.hidden":      "",
		"no-dot":           "",
	}
	for in, want := range cases {
		if got := nodeExtname(in); got != want {
			t.Errorf("nodeExtname(%q) = %q, want %q", in, got, want)
		}
	}
}
