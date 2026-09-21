package otc

import (
	"encoding/json"
	"fmt"
	"math"
)

// HistoryFileTreeStage is HISTORY_FILE_TREE_STAGE (Node: ot_migration_stages.js).
// The migration stage at or above which `history` holds a project's file tree
// (once a project crosses the gate there is no way back: web's mongo
// rootFolder/docs/fileRefs are no longer authoritative).
const HistoryFileTreeStage = 11

// historyStageTypeError mirrors the Node `TypeError` thrown by
// historyIsSourceOfTruth when the stage is neither absent nor a number.
type historyStageTypeError struct{ stage any }

func (e *historyStageTypeError) Error() string {
	raw, err := json.Marshal(e.stage)
	if err != nil {
		raw = []byte(fmt.Sprintf("%v", e.stage))
	}
	return "otMigrationStage is not a number: " + string(raw)
}

// toFiniteNumber reports whether v holds a representable, finite JSON number and
// returns its float64 value. Mirrors Node's `typeof v === 'number' &&
// Number.isFinite(v)` (Go has no `typeof`; we accept every integer/float width
// plus json.Number, and reject anything else, as well as Inf/NaN).
func toFiniteNumber(v any) (float64, bool) {
	f, isNum := v.(float64)
	if !isNum {
		switch t := v.(type) {
		case int:
			f = float64(t)
		case int8:
			f = float64(t)
		case int16:
			f = float64(t)
		case int32:
			f = float64(t)
		case int64:
			f = float64(t)
		case uint:
			f = float64(t)
		case uint8:
			f = float64(t)
		case uint16:
			f = float64(t)
		case uint32:
			f = float64(t)
		case uint64:
			f = float64(t)
		case float32:
			f = float64(t)
		case json.Number:
			num, err := t.Float64()
			if err != nil {
				return 0, false
			}
			f = num
		default:
			return 0, false
		}
	}
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

// HistoryIsSourceOfTruth mirrors historyIsSourceOfTruth(otMigrationStage):
//
//	Node:
//	  if (otMigrationStage == null) return false
//	  if (typeof otMigrationStage !== 'number' || !Number.isFinite(otMigrationStage))
//	    throw new TypeError(`otMigrationStage is not a number: ${JSON.stringify(...)}`)
//	  return otMigrationStage >= HISTORY_FILE_TREE_STAGE
//
// Go mapping (documented Go-ism):
//   - Node `null`/`undefined` -> Go `nil` -> returns (false, nil).
//   - A non-number / non-finite value -> Go cannot throw a TypeError, so this
//     returns the equivalent error (carrying the same message) as the second value.
//   - A finite number -> (stage >= HistoryFileTreeStage, nil).
func HistoryIsSourceOfTruth(otMigrationStage any) (bool, error) {
	if otMigrationStage == nil {
		return false, nil
	}
	n, ok := toFiniteNumber(otMigrationStage)
	if !ok {
		return false, &historyStageTypeError{stage: otMigrationStage}
	}
	return n >= HistoryFileTreeStage, nil
}
