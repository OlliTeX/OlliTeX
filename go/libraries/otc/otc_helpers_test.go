package otc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// testRand mirrors test/unit/support/random.js.
type testRand struct{ rng *rand.Rand }

func newTestRand(seed int64) *testRand { return &testRand{rng: rand.New(rand.NewSource(seed))} }

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// randInt (Node: `randomInt`).
func (tr *testRand) randInt(n int) int {
	if n <= 0 {
		return 0
	}
	return tr.rng.Intn(n)
}

// randString (Node: `randomString`).
func (tr *testRand) randString(n int, newLine bool) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		if newLine && tr.rng.Float64() < 0.15 {
			sb.WriteByte('\n')
		} else {
			sb.WriteByte(byte(rune('a') + rune(tr.randInt(26))))
		}
	}
	return sb.String()
}

// randElement (Node: `randomElement`).
func (tr *testRand) randElement(s []string) string { return s[tr.randInt(len(s))] }

// randSubset (Node: `randomSubset`).
func (tr *testRand) randSubset(arr []string) []string {
	n := tr.randInt(len(arr))
	subset := []string{}
	indices := make([]int, len(arr))
	for i := range indices {
		indices[i] = i
	}
	for i := 0; i < n; i++ {
		index := tr.randInt(len(indices))
		subset = append(subset, arr[indices[index]])
		indices = append(indices[:index], indices[index+1:]...)
	}
	return subset
}

// randComments (Node: `randomComments`). Returns ids + raw comments.
func (tr *testRand) randComments(number int) ([]string, []map[string]any) {
	ids := []string{}
	comments := []map[string]any{}
	seen := map[string]bool{}
	for len(comments) < number {
		id := tr.randString(10, false)
		if !seen[id] {
			comments = append(comments, map[string]any{"id": id, "ranges": []any{}, "resolved": false})
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids, comments
}

var fixedTimestamps = []string{
	"2024-01-01T00:00:00.000Z",
	"2023-01-01T00:00:00.000Z",
	"2022-01-01T00:00:00.000Z",
}

// randOperation mirrors test/unit/support/random_text_operation.js.
func (tr *testRand) randOperation(str string, commentIDs []string) *TextOperation {
	o := NewTextOperation()
	for {
		left := len(str) - o.BaseLength
		if left == 0 {
			break
		}
		r := tr.rng.Float64()
		l := 1 + tr.randInt(min2(left-1, 20))
		var tracked TrackingDirective
		if tr.rng.Float64() < 0.1 {
			tt := tr.randElement([]string{"insert", "delete"})
			user := tr.randElement([]string{"user1", "user2", "user3"})
			tsStr := tr.randElement(fixedTimestamps)
			ts, _ := time.Parse(time.RFC3339Nano, tsStr)
			tracked = TrackingProps{Type: tt, UserID: user, TS: ts}
		}
		switch {
		case r < 0.2:
			var cids []string
			if len(commentIDs) > 0 && tr.rng.Float64() < 0.3 {
				cids = tr.randSubset(commentIDs)
			}
			_ = o.Insert(tr.randString(l, true), InsertBuilderOpts{Tracking: tracked, CommentIds: cids})
		case r < 0.4:
			_ = o.Remove(l)
		case r < 0.5:
			_ = o.Retain(l, RetainBuilderOpts{Tracking: ClearTrackingProps{}})
		default:
			_ = o.Retain(l, RetainBuilderOpts{Tracking: tracked})
		}
	}
	if tr.rng.Float64() < 0.3 {
		_ = o.Insert("1"+tr.randString(10, true), InsertBuilderOpts{})
	}
	return o
}

// sameRaw compares two raw values for deep equality via JSON
// (map key order independent; slice order preserved).
func sameRaw(t *testing.T, label string, actual, expected any) {
	t.Helper()
	ja, errA := json.Marshal(actual)
	if errA != nil {
		t.Fatalf("%s: marshal actual: %v", label, errA)
	}
	jb, errB := json.Marshal(expected)
	if errB != nil {
		t.Fatalf("%s: marshal expected: %v", label, errB)
	}
	if !bytes.Equal(ja, jb) {
		t.Fatalf("%s raw mismatch:\nactual   %s\nexpected %s", label, ja, jb)
	}
}

// isErrType reports whether err is (or wraps) a *T.
func isErrType[T any](err error) bool {
	for err != nil {
		if _, ok := err.(T); ok {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// expectErr panics-free test helper: asserts err is of type T.
func assertErrType[T any](t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error, got nil", label)
	}
	if !isErrType[T](err) {
		t.Fatalf("%s: expected a %T error, got %T (%v)", label, err, err, err)
	}
}

// assertPanics asserts fn panics (mirroring the Node `expect(...).to.throw()`
// oracle). The recovered value is reported on failure.
func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()
		fn()
	}()
	if !didPanic {
		t.Fatalf("expected a panic, but fn did not panic")
	}
}

// assertPanicsWithValue asserts fn panics and the recovered value has the
// given Error() text (for typed errors like *typeError).
func assertPanicsWithValue(t *testing.T, want string, fn func()) {
	t.Helper()
	didPanic := false
	var msg string
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
				if e, ok := r.(error); ok {
					msg = e.Error()
				} else {
					msg = fmt.Sprintf("%v", r)
				}
			}
		}()
		fn()
	}()
	if !didPanic {
		t.Fatalf("expected a panic, but fn did not panic")
	}
	if msg != want {
		t.Fatalf("expected panic message %q, got %q", want, msg)
	}
}
