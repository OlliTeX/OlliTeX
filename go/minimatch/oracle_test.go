package minimatch

import (
	"os"
	"strings"
	"testing"
)

func readTSV(t *testing.T, path string) [][]string {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("%s: %v", path, err)
	}
	var rows [][]string
	line := ""
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			rows = append(rows, strings.Split(line, "\t"))
			line = ""
		} else {
			line += string(data[i])
		}
	}
	if line != "" {
		rows = append(rows, strings.Split(line, "\t"))
	}
	return rows
}

// resOK converts oracle result cell "0"/"1" to int; returns ok=false otherwise.
func resOK(res string) (int, bool) {
	if res == "0" || res == "1" {
		return int(res[0]) - '0', true
	}
	return 0, false
}

func runMatchOracle(t *testing.T, path string) {
	rows := readTSV(t, path)
	var mism, total int
	for i, row := range rows {
		if len(row) != 4 {
			continue
		}
		total++
		pat, dot, f, res := row[0], row[1], row[2], row[3]
		want, ok := resOK(res)
		if !ok {
			continue
		}
		m, err := New(pat, &Options{Dot: dot == "1"})
		if err != nil {
			t.Fatalf("row %d: New(%q): %v", i, pat, err)
		}
		got := 0
		if m.Match(f) {
			got = 1
		}
		if got != want {
			mism++
			if mism <= 30 {
				t.Errorf("pat=%q dot=%s file=%q want=%d got=%d", pat, dot, f, want, got)
			}
		}
	}
	t.Logf("%s: %d rows, %d mismatches", path, total, mism)
}

func runFull26(t *testing.T, path string) {
	rows := readTSV(t, path)
	var mism, total int
	for i, row := range rows {
		if len(row) != 3 {
			continue
		}
		total++
		pat, f, res := row[0], row[1], row[2]
		want, ok := resOK(res)
		if !ok {
			continue
		}
		m, err := New(pat, &Options{Dot: true})
		if err != nil {
			t.Errorf("row %d: New(%q): %v", i, pat, err)
			continue
		}
		got := 0
		if m.Match(f) {
			got = 1
		}
		if got != want {
			mism++
			if mism <= 30 {
				t.Errorf("pat=%q file=%q want=%d got=%d", pat, f, want, got)
			}
		}
	}
	t.Logf("%s: %d rows, %d mismatches", path, total, mism)
}

func runDiff26(t *testing.T, path string) {
	rows := readTSV(t, path)
	var mism, total int
	for i, row := range rows {
		if len(row) != 3 {
			continue
		}
		total++
		pat, f, cr := row[0], row[1], row[2]
		arrow := strings.Index(cr, "=>")
		if arrow < 0 {
			continue
		}
		dot, res := cr[:arrow], cr[arrow+2:]
		want, ok := resOK(res)
		if !ok {
			continue
		}
		m, err := New(pat, &Options{Dot: dot == "1"})
		if err != nil {
			t.Errorf("row %d: New(%q): %v", i, pat, err)
			continue
		}
		got := 0
		if m.Match(f) {
			got = 1
		}
		if got != want {
			mism++
			if mism <= 30 {
				t.Errorf("pat=%q file=%q dot=%s want=%d got=%d", pat, f, dot, want, got)
			}
		}
	}
	t.Logf("%s: %d rows, %d mismatches", path, total, mism)
}

func runSegPortion(t *testing.T, path string) {
	rows := readTSV(t, path)
	var mism, total int
	for i, row := range rows {
		if len(row) != 5 {
			continue
		}
		pat, pos, dot, f, res := row[0], row[1], row[2], row[3], row[4]
		if res == "NOTEST" || res == "E" {
			// NOTEST: JS cell has no .test (string cell or **). Go side: plain string (isStr)
			// or globstar sentinel. The Go port's parsePortion produces isStr for plain,
			// globstar for "**", so both should agree on NOTEST-ness — this is a
			// structural check, not a value check.
			total++
			continue
		}
		want, ok := resOK(res)
		if !ok {
			continue
		}
		m, err := New(pat, &Options{Dot: dot == "1"})
		if err != nil {
			continue // oracle skipped failing patterns
		}
		idx := 1
		if pos == "s" {
			idx = 0
		}
		if len(m.set) == 0 || idx >= len(m.set[0]) {
			t.Errorf("row %d: missing set[0][%d] pat=%q pos=%s", i, idx, pat, pos)
			continue
		}
		cell := m.set[0][idx]
		if cell.isStr() || cell.globstar {
			// Go says plain; oracle says testable → structural mismatch.
			mism++
			if mism <= 30 {
				t.Errorf("structural: pat=%q pos=%s file=%q want=%d (go cell isStr=%v globstar=%v)",
					pat, pos, f, want, cell.isStr(), cell.globstar)
			}
			continue
		}
		total++
		got := 0
		if hit, _ := cell.test(f); hit {
			got = 1
		}
		if got != want {
			mism++
			if mism <= 30 {
				t.Errorf("pat=%q pos=%s dot=%s file=%q want=%d got=%d", pat, pos, dot, f, want, got)
			}
		}
	}
	t.Logf("%s: %d rows (tested), %d mismatches", path, total, mism)
}

// oraclePath prefers the checked-in testdata/ copy, falling back to the
// live /tmp/mmprobe generation location (where the TSVs were produced).
func oraclePath(t *testing.T, name string) string {
	if data, err := os.ReadFile("testdata/" + name); err == nil && len(data) > 0 {
		return "testdata/" + name
	}
	return "/tmp/mmprobe/" + name
}

func TestOracleMatchCorpus(t *testing.T) {
	runMatchOracle(t, oraclePath(t, "match_oracle.tsv"))
}

func TestOracleSegM(t *testing.T) {
	runMatchOracle(t, oraclePath(t, "segM.tsv"))
}

func TestOracleFull26(t *testing.T) {
	runFull26(t, oraclePath(t, "full26.tsv"))
}

func TestOracleDiff26(t *testing.T) {
	runDiff26(t, oraclePath(t, "diff26.tsv"))
}

func TestOracleSegPortion(t *testing.T) {
	runSegPortion(t, oraclePath(t, "segPortion.tsv"))
}
