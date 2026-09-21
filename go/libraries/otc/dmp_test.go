package otc

// dmp_test.go pins that the Go diff-match-patch we depend on
// (github.com/sergi/go-diff) is drop-in-compatible with the exact
// diff-match-patch@1.0.5 overleaf pins in Node. The expectations are the byte-for-byte
// outputs the Node library produces (Diff_Timeout=0.1, diff_main(a,b), then
// diff_cleanupSemantic) for the inputs the otc oracles actually feed DMP, plus a few
// general line-mode cases. If a future version of the Go dependency diverges from
// those diffs, this test fails — so the port stays 1:1 with the Node oracle.

import (
	"testing"
	"time"

	diffmatchpatch "github.com/sergi/go-diff/diffmatchpatch"
)

type dmpExp struct {
	op   int
	text string
}

// dmpGroundTruth mirrors the Node diff-match-patch@1.0.5 outputs captured for the
// oracle inputs (raw == cleanupSemantic for these, so one expected set).
var dmpGroundTruth = []struct {
	name   string
	before string
	after  string
	diff   []dmpExp
}{
	{"noop", "hello world", "hello world", []dmpExp{{0, "hello world"}}},
	{"insert", "hello world", "hello brave world", []dmpExp{{0, "hello "}, {1, "brave "}, {0, "world"}}},
	{"remove", "hello cruel world", "hello world", []dmpExp{{0, "hello "}, {-1, "cruel "}, {0, "world"}}},
	{
		"multiline", "one\ntwo\nthree", "one\ntwo and a half\nthree",
		[]dmpExp{{0, "one\ntwo"}, {1, " and a half"}, {0, "\nthree"}},
	},
	{
		"multiline-tail", "A\nB\nC", "A\nB2\nC2",
		[]dmpExp{{0, "A\nB"}, {1, "2"}, {0, "\nC"}, {1, "2"}},
	},
	{
		"sentence", "Hello world.\nFoo bar.", "Hello world.\nBaz bar.",
		[]dmpExp{{0, "Hello world.\n"}, {-1, "Foo"}, {1, "Baz"}, {0, " bar."}},
	},
	{
		"block-replace", "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n",
		"line1\nline2\nMIDDLE\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n",
		[]dmpExp{
			{0, "line1\nline2\n"}, {-1, "line3"}, {1, "MIDDLE"},
			{0, "\nline4\nline5\nline6\nline7\nline8\nline9\nline10\n"},
		},
	},
}

// TestDMPDepMatchesNode105 is the interop gate for Option A: it asserts the Go
// diff-match-patch dependency produces exactly the Node 1.0.5 diffs the otc oracle
// relies on, for both the raw diff and after diff_cleanupSemantic.
func TestDMPDepMatchesNode105(t *testing.T) {
	// Mirror Node: dmp.Diff_Timeout = 0.1
	dmp := diffmatchpatch.New()
	dmp.DiffTimeout = 100 * time.Millisecond

	for _, tc := range dmpGroundTruth {
		raw := dmp.DiffMain(tc.before, tc.after, true)
		for _, got := range []struct {
			name  string
			diffs []diffmatchpatch.Diff
		}{
			{"raw", raw},
			{"cleanupSemantic", dmp.DiffCleanupSemantic(dmp.DiffMain(tc.before, tc.after, true))},
		} {
			if len(got.diffs) != len(tc.diff) {
				t.Errorf("%s (%s): %d diffs, want %d\n got  %+v\n want %+v", tc.name, got.name, len(got.diffs), len(tc.diff), got.diffs, tc.diff)
				continue
			}
			for i := range tc.diff {
				if int(got.diffs[i].Type) != tc.diff[i].op || got.diffs[i].Text != tc.diff[i].text {
					t.Errorf("%s (%s): diff[%d] = {%d %q}, want {%d %q}",
						tc.name, got.name, i, got.diffs[i].Type, got.diffs[i].Text, tc.diff[i].op, tc.diff[i].text)
				}
			}
		}
	}
}
