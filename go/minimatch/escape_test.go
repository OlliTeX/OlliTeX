package minimatch

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// testdata/escape.tsv (generated from minimatch 10.2.6 in node v24) — 10 cols per row:
//
//	in \t esc0 \t escMag \t escWpne \t escWpneMag \t un0 \t un0p \t un2 \t un3 \t un3p
//
//	esc*  = escape(in, variant)            cols 1..4
//	un0   = unescape(in, {})               magical defaults true
//	un0p  = unescape(in, magicalBraces:false)
//	un2   = unescape(in, wpne:true)
//	un3   = unescape(in, wpne:true, magicalBraces:false)
//	un3p  = unescape(in, wpne:true)  (magicalBraces default true)
func TestEscapeMatchesNodeOracle(t *testing.T) {
	var rows [][]string
	sc := loadEscapeTable(t)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		c := splitTab9(line, "\t")
		if len(c) != 10 {
			t.Fatalf("bad escape.tsv row (want 10 cols): %v", c)
		}
		ins, e0, e1, e2, e3 := c[0], c[1], c[2], c[3], c[4]
		got := []string{
			Escape(ins, nil),
			Escape(ins, &Options{MagicalBraces: true}),
			Escape(ins, &Options{WindowsPathsNoEscape: true}),
			Escape(ins, &Options{WindowsPathsNoEscape: true, MagicalBraces: true}),
		}
		want := []string{e0, e1, e2, e3}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("escape variant %d(%q): got %q want %q", i, ins, got[i], want[i])
			}
		}
		_ = rows
	}
}

func loadEscapeTable(t *testing.T) *bufio.Scanner {
	f, err := os.Open("testdata/escape.tsv")
	if err != nil {
		t.Skip("no testdata/escape.tsv (generated via genoracle.mjs); skipping oracle")
	}
	t.Cleanup(func() { f.Close() })
	return bufio.NewScanner(f)
}

func splitTab9(s, _ string) []string {
	var out []string
	cur := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' {
			out = append(out, s[cur:i])
			cur = i + 1
		}
	}
	out = append(out, s[cur:])
	return out
}

func TestEscapeAssertValidPattern(t *testing.T) {
	if err := assertValidPattern("a"); err != nil {
		t.Fatalf("short pattern should pass: %v", err)
	}
	if err := assertValidPattern(strings.Repeat("a", 1024*65)); err == nil {
		t.Fatal("pattern longer than 64KiB should be rejected")
	}
}
