package opmodel

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// randState — a shared deterministic RNG (seeded once) so the fuzz loops
// mirror the vendor's randomized tests while remaining reproducible in CI.
var randState = rand.New(rand.NewSource(0xE5E5)) // #nosec G404 -- deterministic test

func fuzzer() *rand.Rand { return randState }

// fuzzString — vendor random.string(n): lowercase 'a'..'z' plus occasional
// newlines (15%). BMP only (vendor random.string is BMP-only).
func fuzzString(n int) string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		if fuzzer().Float64() < 0.15 {
			out = append(out, "\n")
		} else {
			out = append(out, string(rune(fuzzer().Int()%26+97)))
		}
	}
	return strings.Join(out, "")
}

// fuzzContent — the 50-char file content for the apply/invert fuzz. BMP only
// (a-z + newlines) exactly like vendor random.string(50). (Supplementary
// content is NOT generated: the Invert remove→insert path, like the vendor,
// rejects non-BMP insertions.)
func fuzzContent() string { return fuzzString(50) }

// fuzzComments — 6 comments with random ids and (in Go, for coverage) a
// random initial range each. Vendor comments start empty; widening here keeps
// the op fuzz meaningful and still exercises applyInsert/applyDelete paths.
// fuzzComments — mirror vendor random.comments(6): unique ids of 10 random
// letters, initially EMPTY ranges, not resolved.
func fuzzComments() (ids []string, comments []*Comment) {
	used := map[string]bool{}
	for len(comments) < 6 {
		id := fuzzString(10)
		if used[id] {
			continue
		}
		used[id] = true
		c, err := NewComment(id, nil, false)
		if err != nil {
			continue
		}
		ids = append(ids, id)
		comments = append(comments, c)
	}
	return ids, comments
}

// newFuzzFile — a StringFileData with content and a comment list.
func newFuzzFile(content string, comments []*Comment) *StringFileData {
	cl := &CommentList{comments: map[string]*Comment{}}
	for _, c := range comments {
		cl.Add(c)
	}
	return &StringFileData{
		Content:        content,
		Comments:       cl,
		TrackedChanges: &TrackedChangeList{},
	}
}

// randomOp — mirrors vendor random_text_operation(str, commentIds): a valid
// operation over a string of length Units(str) (baseLength == len(str)), with
// a trailing insert (vendor 30%) when allowTailInsert.
func randomOp(contentStr string, commentIDs []string, allowTailInsert bool) *TextOperation {
	op := NewTextOperation()
	for {
		left := Units(contentStr) - op.BaseLength
		if left == 0 {
			break
		}
		r := fuzzer().Float64()
		var l int
		if left <= 1 {
			l = 1
		} else {
			l = 1 + fuzzer().Int()%min(left-1, 20)
		}
		var tracking *Tracking
		if fuzzer().Float64() < 0.1 {
			ttype := "insert"
			if fuzzer().Float64() < 0.5 {
				ttype = "delete"
			}
			user := "user1"
			if fuzzer().Float64() < 0.5 {
				user = "user2"
			}
			var ts int64
			switch fuzzer().Int() % 3 {
			case 0:
				ts = 1672531200000 // 2024-01-01T00:00:00.000Z
			case 1:
				ts = 1672502400000 // 2023-01-01T00:00:00.000Z
			default:
				ts = 1672416000000 // 2022-01-01T00:00:00.000Z
			}
			tracking = NewTracking(ttype, user, ts)
		}
		var cids []string
		if len(commentIDs) > 0 && fuzzer().Float64() < 0.3 {
			for _, id := range commentIDs {
				if fuzzer().Float64() < 0.5 {
					cids = append(cids, id)
				}
			}
		}
		switch {
		case r < 0.2:
			op = op.Insert(fuzzString(l), tracking, cids)
		case r < 0.4:
			op.Remove(l)
		case r < 0.5:
			op.Retain(l, Clear())
		default:
			op.Retain(l, tracking)
		}
	}
	if allowTailInsert && fuzzer().Float64() < 0.3 {
		op.Insert(string('a'+rune(fuzzer().Int()%26))+fuzzString(1+fuzzer().Int()%10), nil, nil)
	}
	return op
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func assertEqualT[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func assertBoolT(t *testing.T, got, want bool, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func sameMap(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func assertMapT(t *testing.T, got, want map[string]int, msg string) {
	t.Helper()
	if !sameMap(got, want) {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

// assertWireT compares wire values (JSON-ish: numbers, strings, maps, slices).
// fmt's %v rendering sorts map keys, so map equality is order-insensitive.
func assertWireT(t *testing.T, got, want any, msg string) {
	t.Helper()
	if fmtSprintfAny(got) != fmtSprintfAny(want) {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func fmtSprintfAny(v any) string { return fmt.Sprintf("%v", v) }
