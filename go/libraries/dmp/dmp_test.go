package dmp

import (
	"encoding/json"
	"os"
	"testing"
)

type oracleCase struct {
	Name    string  `json:"name"`
	Timeout float64 `json:"timeout"`
	Before  string  `json:"before"`
	Updated string  `json:"updated"`
}

type oracleSegment struct {
	Op    int    `json:"op"`
	Units string `json:"units"`
}

// TestGoldenMatch replays every case through (diff_main, diff_cleanupSemantic)
// and compares the result to the Node oracle (byte-UTF-16-unit exact).
func TestGoldenMatch(t *testing.T) {
	raw, err := os.ReadFile("goldens.json")
	if os.IsNotExist(err) {
		t.Skip("goldens.json not present")
	}
	if err != nil {
		t.Fatal(err)
	}

	var g struct {
		Cases []oracleCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		var list []oracleCase
		if err2 := json.Unmarshal(raw, &list); err2 != nil {
			t.Fatalf("parse goldens.json: %v / %v", err, err2)
		}
		g.Cases = list
	}

	outRaw, err := os.ReadFile("oracle_out.json")
	if os.IsNotExist(err) {
		t.Skip("oracle_out.json not present (generate it against node + diff-match-patch)")
	}
	if err != nil {
		t.Fatal(err)
	}
	var oracle map[string][]oracleSegment
	if err := json.Unmarshal(outRaw, &oracle); err != nil {
		t.Fatal("parse oracle_out.json:", err)
	}

	for _, c := range g.Cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			want, ok := oracle[c.Name]
			if !ok {
				t.Fatalf("no oracle entry for %q", c.Name)
			}

			d := New()
			d.DiffTimeout = c.Timeout
			diff := d.DiffMain(c.Before, c.Updated)
			d.CleanupSemantic(&diff)

			if len(diff) != len(want) {
				t.Fatalf("segment count: got %d want %d (diff %v)", len(diff), len(want), diff)
			}
			for i, seg := range diff {
				wantSeg := want[i]
				if seg.Op != Op(wantSeg.Op) {
					t.Errorf("seg %d op: got %d want %d (seg %q)", i, seg.Op, wantSeg.Op, seg.Text)
				}
				gotUnits := unitsHex(u16(seg.Text))
				if gotUnits != wantSeg.Units {
					t.Errorf("seg %d units: got %s want %s (seg %q)\nall: %v", i, gotUnits, wantSeg.Units, seg.Text, diff)
				}
			}
		})
	}
}

// TestRoundtripProperty: the diff must replay to both sides — the invariant
// UpdateCompressor / editor-core depend on.
func TestRoundtripProperty(t *testing.T) {
	pairs := [][2]string{
		{"", ""},
		{"", "b"},
		{"aaa bbb ccc ddd eee fff", "bbb ccc ddd aaa eee fff"},
		{"the quick brown fox", "the quick brown dog"},
		{"hello world", ""},
		{"\U0001F389b", "\U0001F389c"},
		{"abcabcabcabc", "abcabcabc"},
		{"\n \n a", "\n \n b"},
		{"aaaa\nbbbb\n\ncccc", "aaaa\nxxxx\n\ncccc"},
		{"αβγ", "αβγδ"},
	}
	for _, pr := range pairs {
		before, after := pr[0], pr[1]
		d := New()
		diff := d.DiffMain(before, after)
		d.CleanupSemantic(&diff)
		var buildBefore, buildAfter string
		for _, seg := range diff {
			if seg.Op != Insert {
				buildBefore += seg.Text
			}
			if seg.Op != Delete {
				buildAfter += seg.Text
			}
		}
		if buildBefore != before {
			t.Errorf("before mismatch: got %q want %q: %v", buildBefore, before, diff)
		}
		if buildAfter != after {
			t.Errorf("after mismatch: got %q want %q: %v", buildAfter, after, diff)
		}
	}
}

// TestU16Roundtrip verifies the UTF-16 codec is bijective, including lone
// surrogates in the middle (emoji inputs).
func TestU16Roundtrip(t *testing.T) {
	for _, s := range []string{"", "abc", "héllo", "αβγ", "a\U0001F389b", "\U0001F389"} {
		round := str(u16(s))
		if round != s {
			t.Errorf("roundtrip mismatch for %q: got %q", s, round)
		}
	}
	// A lone high surrogate must round-trip through the codec 1:1 (this is the
	// corruption that `range`-based Go strings silently mask by collapsing
	// lone surrogates to U+FFFD). Asserting round-trip only, with no byte
	// literals — the CESU-8 layout is an internal detail of str/u16.
	lone := []uint16{0xD83C}
	if dec := u16(str(lone)); len(dec) != 1 || dec[0] != 0xD83C {
		t.Fatalf("lone surrogate round-trip: got %v want [0xD83C]", dec)
	}
	// And a lone *low* surrogate is equally preserved.
	loneL := []uint16{0xDC00}
	if dec := u16(str(loneL)); len(dec) != 1 || dec[0] != 0xDC00 {
		t.Fatalf("lone low surrogate round-trip: got %v want [0xDC00]", dec)
	}
}

func unitsHex(units []uint16) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 0, len(units)*4)
	for _, u := range units {
		out = append(out, hexdigits[u>>12], hexdigits[u>>8&0xF], hexdigits[u>>4&0xF], hexdigits[u&0xF])
	}
	return string(out)
}
