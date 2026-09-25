package limits

import "testing"

// TestGetTotalSizeOfLines mirrors LimitsTests.js `getTotalSizeOfLines`.
func TestGetTotalSizeOfLines(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  int
	}{
		{"empty", []string{}, 0},
		{"single", []string{"123"}, 4},
		{"multiple", []string{"123", "4567"}, 9},
	}
	for _, c := range cases {
		if got := GetTotalSizeOfLines(c.lines); got != c.want {
			t.Fatalf("getTotalSizeOfLines(%v) = %d, want %d", c.lines, got, c.want)
		}
	}
}

// xRepeat returns a string of n 'x' chars (Node: 'x'.repeat(n)).
func xRepeat(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

// manyLines mirrors Node `'1234567890'.repeat(n).split('0')` -> n lines of
// '123456789' (the trailing separator is consumed by the split).
func manyLines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "123456789"
	}
	return out
}

// TestDocIsTooLarge mirrors LimitsTests.js `docIsTooLarge`.
func TestDocIsTooLarge(t *testing.T) {
	cases := []struct {
		name  string
		est   int
		lines []string
		limit int
		want  bool
	}{
		{"under-estimate", 128, []string{"hello", "world"}, 1024, false},
		{"at-limit estimate", 1024, []string{"hello", "world"}, 1024, false},
		{"est-over actual-under", 2048, []string{"hello", "world"}, 1024, false},
		{"est-over actual-at-limit", 2048, []string{xRepeat(1023)}, 1024, false},
		{"actual-over-by-1", 2048, []string{xRepeat(1024)}, 1024, true},
		{"actual-over", 2048, []string{xRepeat(2000)}, 1024, true},
		{"many-lines-under", 2048, manyLines(100), 1024, false},
		{"many-lines-over", 2048, manyLines(2000), 1024, true},
	}
	for _, c := range cases {
		if got := DocIsTooLarge(c.est, c.lines, c.limit); got != c.want {
			t.Fatalf("docIsTooLarge(%d, %d lines, %d) = %v, want %v (case %s)",
				c.est, len(c.lines), c.limit, got, c.want, c.name)
		}
	}
}

func trackedChangeDelete(length int) TrackedChangeRange {
	return TrackedChangeRange{
		Range:    Range{Pos: 1, Length: length},
		Tracking: Tracking{Type: "delete", TS: "2025-06-16T14:31:44.910Z", UserID: "user-id"},
	}
}

func trackedChangeInsert(length int) TrackedChangeRange {
	return TrackedChangeRange{
		Range:    Range{Pos: 1, Length: length},
		Tracking: Tracking{Type: "insert", TS: "2025-06-16T14:31:44.910Z", UserID: "user-id"},
	}
}

// TestStringFileDataContentIsTooLarge mirrors LimitsTests.js
// `stringFileDataContentIsTooLarge`.
func TestStringFileDataContentIsTooLarge(t *testing.T) {
	raw := func(content string, tcs ...TrackedChangeRange) StringFileRawData {
		return StringFileRawData{Content: content, TrackedChanges: tcs}
	}
	cases := []struct {
		name  string
		raw   StringFileRawData
		limit int
		want  bool
	}{
		{"small", raw(""), 123, false},
		{"at-limit", raw(xRepeat(123)), 123, false},
		{"over", raw(xRepeat(124)), 123, true},
		{"over-but-removed-by-delete", raw(xRepeat(124), trackedChangeDelete(1)), 123, false},
		{"over-even-after-delete", raw(xRepeat(125), trackedChangeDelete(1)), 123, true},
		{"tracked-insert-not-counted", raw(xRepeat(124), trackedChangeInsert(1)), 123, true},
	}
	for _, c := range cases {
		if got := StringFileDataContentIsTooLarge(&c.raw, c.limit); got != c.want {
			t.Fatalf("stringFileDataContentIsTooLarge(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}
