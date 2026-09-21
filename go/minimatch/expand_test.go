package minimatch

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func hexToStr(s string) (string, bool) {
	if len(s) == 0 {
		return "", true
	}
	if len(s)%2 != 0 {
		return "", false
	}
	out := make([]byte, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		b, err := strconv.ParseInt(s[i:i+2], 16, 8)
		if err != nil {
			return "", false
		}
		out = append(out, byte(b))
	}
	return string(out), true
}

func TestExpandOracle(t *testing.T) {
	data, err := os.ReadFile("testdata/expand.tsv")
	if err != nil {
		t.Skip("no oracle file")
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			continue
		}
		input, wantJoin := fields[0], fields[1]
		var want []string
		if wantJoin != "" {
			for _, h := range strings.Split(wantJoin, "|") {
				s, ok := hexToStr(h)
				if !ok {
					t.Fatalf("bad hex %q", h)
				}
				want = append(want, s)
			}
		}
		got, err := Expand(input, Options{})
		if err != nil {
			t.Fatalf("%q: %v", input, err)
		}
		if len(got) != len(want) {
			t.Errorf("%q: got %v want %v", input, got, want)
			continue
		}
		for k := range got {
			if got[k] != want[k] {
				t.Errorf("%q: got %v want %v", input, got, want)
			}
		}
	}
}

func TestExpandSanity(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"{a,b}", []string{"a", "b"}},
		{"{0..3}", []string{"0", "1", "2", "3"}},
		{"plain", []string{"plain"}},
		{"", []string{}},
	}
	for _, c := range cases {
		got, err := Expand(c.in, Options{})
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v want %v", c.in, got, c.want)
		}
	}
}
