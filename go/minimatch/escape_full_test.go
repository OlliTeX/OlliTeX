// escape_full_test.go - 157-row oracle for Escape (4 flag combos) and
// Unescape (4 flag combos). Base64-encoded fields.
// Column layout: s escD escW escMB escWMB unD un2 un3 un4, where
//
//	unD  = Node unescape(escD, {})
//	un2  = Node unescape(escD, {magicalBraces:false})
//	un3  = Node unescape(escD, {wpne:true})
//	un4  = Node unescape(escD, {wpne:true, magicalBraces:false})
package minimatch

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestEscapeOracle(t *testing.T) {
	data, err := os.ReadFile("testdata/esc_oracle.tsv")
	if err != nil {
		t.Skip("oracle missing: " + err.Error())
	}
	lines := strings.Split(string(data), "\n")
	n := 0
	for _, ln := range lines[1:] {
		if ln == "" {
			continue
		}
		c := strings.Split(ln, "\t")
		if len(c) != 9 {
			t.Fatalf("bad row cols=%d: %q", len(c), c)
		}
		s, _ := base64.StdEncoding.DecodeString(c[0])
		combos := []struct {
			name string
			opts *Options
		}{
			// upstream escape() default is magicalBraces=FALSE (differs
			// from minimatch()'s magicalBraces default of true).
			{"escD", &Options{}},
			{"escW", &Options{WindowsPathsNoEscape: true}},
			{"escMB", &Options{MagicalBraces: true}},
			{"escWMB", &Options{WindowsPathsNoEscape: true, MagicalBraces: true}},
		}
		outs := [4]string{c[1], c[2], c[3], c[4]}
		for i, cb := range combos {
			want, _ := base64.StdEncoding.DecodeString(outs[i])
			if got := Escape(string(s), cb.opts); got != string(want) {
				t.Errorf("%s %x: got %q want %q", cb.name, s, got, want)
			}
		}
		n++
	}
	t.Logf("escape oracle: %d rows x 4 combos", n)
}

func TestUnescapeOracle(t *testing.T) {
	data, err := os.ReadFile("testdata/esc_oracle.tsv")
	if err != nil {
		t.Skip("oracle missing: " + err.Error())
	}
	lines := strings.Split(string(data), "\n")
	n := 0
	for _, ln := range lines[1:] {
		if ln == "" {
			continue
		}
		c := strings.Split(ln, "\t")
		in, _ := base64.StdEncoding.DecodeString(c[1]) // escD
		type row struct {
			name string
			opts *Options
		}
		combos := []row{
			{"unD", &Options{}},
			{"un2", &Options{MagicalBraces: false}},
			{"un3", &Options{WindowsPathsNoEscape: true}},
			{"un4", &Options{WindowsPathsNoEscape: true, MagicalBraces: false}},
		}
		outs := [4]string{c[5], c[6], c[7], c[8]}
		for i, cb := range combos {
			want, _ := base64.StdEncoding.DecodeString(outs[i])
			if got, wantS := Unescape(string(in), cb.opts), string(want); got != wantS {
				t.Errorf("%s in=%q: got %q want %q", cb.name, in, got, wantS)
			}
		}
		n++
	}
	t.Logf("unescape oracle: %d rows x 4 combos", n)
}
