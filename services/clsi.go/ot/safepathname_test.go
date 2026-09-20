package ot

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

type oracleRow struct {
	I   string `json:"i"`
	C   string `json:"c"`
	R   string `json:"r"`
	K   bool   `json:"k"`
	Why string `json:"why"`
}

func TestSafePathnameOracle(t *testing.T) {
	data, err := os.ReadFile("testdata/spfuzz2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var rows []oracleRow
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no oracle rows")
	}

	mismatch := 0
	for idx, row := range rows {
		got := Clean(row.I)
		if got != row.C {
			mismatch++
			if mismatch <= 20 {
				t.Logf("MISMATCH clean row %d: in=%q want=%q got=%q", idx, row.I, row.C, got)
			}
			continue
		}
		gcd := CleanDebug(row.I)
		if gcd.Reason != row.R {
			mismatch++
			if mismatch <= 20 {
				t.Logf("MISMATCH reason row %d: in=%q want=%q got=%q", idx, row.I, row.R, gcd.Reason)
			}
			continue
		}
		gok, gwhy := IsClean(row.I)
		if gok != row.K || gwhy != row.Why {
			mismatch++
			if mismatch <= 20 {
				t.Logf("MISMATCH isClean row %d: in=%q want=(%v,%q) got=(%v,%q)", idx, row.I, row.K, row.Why, gok, gwhy)
			}
		}
	}
	if mismatch > 0 {
		t.Fatalf("%d/%d mismatches", mismatch, len(rows))
	}
	fmt.Printf("PASS safe_pathname oracle: %d rows\n", len(rows))
}
