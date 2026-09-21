// reengine_test.go - oracle-locked closed-dialect engine.
//
// testdata/testoracle.tsv: 35400 rows (src \t flags \t file \t 0/1) are the
// V8 RegExp.test ground truth for every compiled source minimatch 10.2.6
// emits (472 distinct sources) x every test path (75). Any dialect drift
// fails this test.
package minimatch

import (
	"os"
	"testing"
)

func TestReEngineOracle(t *testing.T) {
	data, err := os.ReadFile("testdata/testoracle.tsv")
	if err != nil {
		t.Skip("oracle not on disk: " + err.Error())
	}
	rows := splitTSV(data)
	if len(rows) == 0 {
		t.Fatal("empty oracle")
	}
	bad := 0
	for _, f := range rows {
		if len(f) != 4 {
			t.Fatalf("malformed oracle row: %q", f)
		}
		src, flags, file, res := f[0], f[1], f[2], f[3]
		_ = flags // matching is flag-independent for byte-ASCII corpus
		prog, err := CompileRe(src)
		if err != nil {
			t.Fatalf("unexpected dialect error on oracle src %q: %v", src, err)
		}
		got, err := prog.Test(file)
		if err != nil {
			t.Fatalf("eval error: %v (src %q)", err, src)
		}
		if want := res == "1"; got != want {
			bad++
			if bad <= 5 {
				t.Errorf("src=%q file=%q got=%v want=%v", src, file, got, want)
			}
		}
	}
	if bad > 5 {
		t.Errorf("total mismatches: %d of %d rows", bad, len(rows))
	}
}

// Hand-probed V8 byte semantics (from session oracle probes).
func TestReEngineEdgeCases(t *testing.T) {
	cases := []struct {
		src  string
		in   string
		want bool
	}{
		{"^(?:a|$)$", "a", true},  // empty group as alternative
		{"^(?:a|)*a$", "a", true}, // lazy star of empty-alt satisfies +
		{"^(?:a|)*a$", "b", false},
		{"^(?:)$x$", "x", false}, // empty group doesn't create 'x'
		{"^(?:)+x$", "x", true},  // empty group under + (min 1, zero-cost)
		{"^(?:ab)+?$", "ab", true},
		{"^(?:|a)+b$", "b", true},
		{"^(?:|a)+b$", "ab", true},
		{"^(?:(?:b|)a)*?c$", "ac", true},
		{"^(?:(?:b|)a)*?c$", "babc", false},
		{"^(?:)^$", "", true},
		{"^(?:)$x$", "x", false},
		{"^[a-c]+$", "abc", true},
		{"^[a-c]+$", "abd", false},
		{"^[^a-c]+$", "d", true},
		{"^[^a-c]+$", "b", false},
		{"^[a-b]*$", "abc", false},
		{"^[a-b]*$", "", true},
		{"^a{1,2}$", "a", true},
		{"^a{1,2}$", "aa", true},
		{"^a{1,2}$", "aaa", false},
		{"^(?!^x$).+$", "y", true},
		{"^(?!^x$).+$", "x", false},
		{"^(?!^x$).{1,2}$", "xy", true},
		{"^(?:^|/)..?(?:$|/)$", "..", true},
		{"^(?:^|/)..?(?:$|/)$", "a/../b", false},
		{"^(?!.).+$", "x", false}, // dot-guard blocks all single chars
		{"^(?!.).*$", "", true},
		{"^a.$", "a.", true},
		{"^\\*$", "*", true},
		{"^\\*$", "**", false},
		{"^\\t*$", "\t*", false},
		{"^\\\\$", "\\", true},
		{"^[\\-\\-]$", "-", true}, // escaped-dash range (oracle span)
	}
	for _, c := range cases {
		prog, err := CompileRe(c.src)
		if err != nil {
			t.Fatalf("compile %q: %v", c.src, err)
		}
		got, err := prog.Test(c.in)
		if err != nil {
			t.Fatalf("eval %q: %v", c.src, err)
		}
		if got != c.want {
			t.Errorf("%q vs %q: got %v want %v", c.src, c.in, got, c.want)
		}
	}
}

// Dialect drift must be LOUD (compile error), never silent.
func TestReEngineDialectErrors(t *testing.T) {
	for _, badsrc := range []string{
		"^(\\w)$",  // W outside dialect
		"^(\\p)$",  // p without braces
		"^[x",      // unterminated class
		"^(?:^|/",  // unterminated group
		"^a{2,1}$", // lo > hi
		"^a{200}$", // bound out of dialect
		"^)",       // stray )
	} {
		if _, err := CompileRe(badsrc); err == nil {
			t.Errorf("expected dialect error for %q", badsrc)
		}
	}
}

func splitTSV(b []byte) [][]string {
	var rows [][]string
	for i := 0; i < len(b); {
		j := 0
		for j < len(b)-i && b[i+j] != 10 {
			j++
		}
		line := b[i : i+j]
		i += j + 1
		var f []string
		for k := 0; k < len(line); {
			m := 0
			for m < len(line)-k && line[k+m] != 9 {
				m++
			}
			f = append(f, string(line[k:k+m]))
			k += m + 1
		}
		rows = append(rows, f)
	}
	return rows
}
