package otc

import (
	"math"
	"strings"
	"testing"
)

// TestHistoryIsSourceOfTruth mirrors the Node ot_migration_stages behavior:
// null -> false; non-number/non-finite -> TypeError; number -> >= HISTORY_FILE_TREE_STAGE.
func TestHistoryIsSourceOfTruth(t *testing.T) {
	// nil (Node null/undefined) -> false, no error.
	if ok, err := HistoryIsSourceOfTruth(nil); err != nil || ok != false {
		t.Fatalf("nil: got (%v, %v); want (false, nil)", ok, err)
	}
	// Below the gate.
	if ok, err := HistoryIsSourceOfTruth(10); err != nil || ok != false {
		t.Fatalf("10: got (%v, %v); want (false, nil)", ok, err)
	}
	// Exactly the gate.
	if ok, err := HistoryIsSourceOfTruth(11); err != nil || ok != true {
		t.Fatalf("11: got (%v, %v); want (true, nil)", ok, err)
	}
	// Above the gate.
	if ok, err := HistoryIsSourceOfTruth(42); err != nil || ok != true {
		t.Fatalf("42: got (%v, %v); want (true, nil)", ok, err)
	}
	// float exactly at the gate.
	if ok, err := HistoryIsSourceOfTruth(11.0); err != nil || ok != true {
		t.Fatalf("(float) 11.0: got (%v, %v); want (true, nil)", ok, err)
	}
	// float below the gate.
	if ok, err := HistoryIsSourceOfTruth(10.5); err != nil || ok != false {
		t.Fatalf("(float) 10.5: got (%v, %v); want (false, nil)", ok, err)
	}

	cases := []any{"11", true, map[string]any{"stage": 1}}
	for _, v := range cases {
		if ok, err := HistoryIsSourceOfTruth(v); err == nil {
			t.Fatalf("%v: expected error, got (%v, %v)", v, ok, err)
		} else if !strings.Contains(err.Error(), "otMigrationStage is not a number: ") {
			t.Fatalf("%v: wrong message: %q", v, err.Error())
		}
	}

	// Non-finite numbers -> error (Node: Number.isFinite fails).
	for _, v := range []any{math.Inf(1), math.Inf(-1), math.NaN()} {
		if _, err := HistoryIsSourceOfTruth(v); err == nil {
			t.Fatalf("%v: expected error for non-finite", v)
		}
	}

	// The gate constant is exported and matches Node.
	if HistoryFileTreeStage != 11 {
		t.Fatalf("HistoryFileTreeStage = %d; want 11", HistoryFileTreeStage)
	}
}

// TestHistoryStageTypeErrorMessage pins the exact message shape for a string input
// (Node: `otMigrationStage is not a number: ${JSON.stringify(v)}`).
func TestHistoryStageTypeErrorMessage(t *testing.T) {
	_, err := HistoryIsSourceOfTruth("11")
	want := "otMigrationStage is not a number: \"11\""
	if err == nil || err.Error() != want {
		t.Fatalf("got %v; want %q", err, want)
	}
}
