// Package safepathname_oracle is the CLSI-side acceptance gate for the
// shared ollitex otc safe_pathname module. It runs the same 80,782-row,
// Node-generated differential fuzz fixture (spfuzz2.json) through
// ollitex/go/libraries/otc and asserts byte-for-byte equality of
// Clean/CleanDebug and IsClean/IsCleanDebug against the Node reference
// (safe_pathname.js oracle).
//
// This package exists because the CLSI consumers (resourcewriter, file_map,
// etc.) take their safe_pathname semantics from otc; the fixture and this
// test live here so upstream and CLSI developers both see the acceptance
// contract and the test runs in both the ollitex and clsi modules
// (clsi go.mod: `replace ollitex => ../../`).
//
// The otc module already runs this fixture too (safe_pathname_oracle_test.go
// in ./go/libraries/otc/); it is the same test duplicated here. Both must
// stay green together.
package safepathname_oracle

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	otc "ollitex/go/libraries/otc"
)

// oracleRow mirrors the diff-fuzz row shape from the Node reference
// (input, clean() output, clean reason, isClean(), isClean reason).
type oracleRow struct {
	I   string `json:"i"`
	C   string `json:"c"`
	R   string `json:"r"`
	K   bool   `json:"k"`
	Why string `json:"why"`
}

func loadSpfuzzRows(t *testing.T) []oracleRow {
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
	return rows
}

// TestSafePathnameOracle runs every fuzz row through both the clean() and
// isClean() code paths and asserts the four outputs (cleaned pathname,
// clean reason string, isClean flag, isClean reason) all match the Node
// reference exactly. This is the acceptance gate: the fixture has 80,782 rows
// and the test must pass 80,782/80,782.
func TestSafePathnameOracle(t *testing.T) {
	rows := loadSpfuzzRows(t)

	mismatch := 0
	for idx, row := range rows {
		cleaned, reason := otc.CleanDebug(row.I)
		if cleaned != row.C {
			mismatch++
			if mismatch <= 20 {
				t.Logf("MISMATCH clean row %d: in=%q want=%q got=%q", idx, row.I, row.C, cleaned)
			}
			continue
		}
		if reason != row.R {
			mismatch++
			if mismatch <= 20 {
				t.Logf("MISMATCH reason row %d: in=%q want=%q got=%q", idx, row.I, row.R, reason)
			}
			continue
		}
		ok, why := otc.IsCleanDebug(row.I)
		if ok != row.K || why != row.Why {
			mismatch++
			if mismatch <= 20 {
				t.Logf("MISMATCH isClean row %d: in=%q want=(%v,%q) got=(%v,%q)", idx, row.I, row.K, row.Why, ok, why)
			}
		}
	}
	if mismatch > 0 {
		t.Fatalf("%d/%d mismatches", mismatch, len(rows))
	}
	fmt.Printf("PASS safe_pathname oracle (CLSI-side): %d rows\n", len(rows))
}
