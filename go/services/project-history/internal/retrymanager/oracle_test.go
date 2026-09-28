// C8 oracle — RetryManager vendor contracts R1–R9.
package retrymanager

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

var cctx = context.Background()

func intPtr(v int) *int { return &v }

func validID(i int) string {
	base := "0123456789abcdef"
	s := ""
	x := i + 11
	for l := 24; l > 0; l-- {
		s = string(base[x%16]) + s
		x = x/16 + 3
	}
	return s
}

func TestC8_SelectorsMatrix(t *testing.T) {
	f := func(err string, attempts *int, resync *int) *Failure {
		return &Failure{ProjectID: "p", Error: err, Attempts: attempts, ResyncAttempts: resync}
	}
	one, two, four, zero := 1, 2, 4, 0

	cases := []struct {
		name       string
		f          *Failure
		soft, hard bool
	}{
		{"temporary-not-repeated → soft", f(TEMPORARY_FAILURES[0], nil, nil), true, false},
		{"temporary-repeated(4) → NOT soft, IS hard (repeated)", f(TEMPORARY_FAILURES[1], &four, nil), false, true},
		{"first-not-hard → soft", f("Error: x", &one, nil), true, false},
		{"hard-list → hard", f(HARD_FAILURES[0], &one, nil), false, true},
		{"hard+stuck(resync2) → not hard", f(HARD_FAILURES[1], &one, &two), false, false},
		{"repeated(4) → hard", f("other", &four, nil), false, true},
		{"attempts=2 → not first, not repeated", f("other", &two, &zero), false, false},
		{"ongoing sync always hard", f("Error: sync ongoing ...", nil, &two), false, true},
		{"ongoing (substring) → soft (first, not hard-list) AND hard (ongoing)", f("sync ongoing for project", &one, &two), true, true},
		{"attempts nil → neither first nor repeated", f("plain error", nil, nil), false, false},
		{"hard-list, resync nil → hard (not stuck)", f(HARD_FAILURES[2], nil, nil), false, true},
		{"first + hard-list → NOT soft, IS hard", f(HARD_FAILURES[3], &one, nil), false, true},
	}
	for _, c := range cases {
		if got := SoftErrorSelector(c.f); got != c.soft {
			t.Errorf("%s: soft=%v want %v", c.name, got, c.soft)
		}
		if got := HardErrorSelector(c.f); got != c.hard {
			t.Errorf("%s: hard=%v want %v", c.name, got, c.hard)
		}
	}
}

func TestC8_OngoingSyncFailurePredicate(t *testing.T) {
	if IsOngoingSyncFailure(&Failure{Error: "boom"}) {
		t.Fatal("no substring → false")
	}
	if !IsOngoingSyncFailure(&Failure{Error: "Error: sync ongoing"}) {
		t.Fatal("substring → true")
	}
	if IsOngoingSyncFailure(&Failure{}) {
		t.Fatal("empty error → false (vendor ?? false)")
	}
}

func TestC8_RetryFailuresSoft(t *testing.T) {
	var mu sync.Mutex
	seen := []string{}
	one, four := 1, 4
	recs := []Failure{
		{ProjectID: validID(1), Error: "plain", Attempts: &one},
		{ProjectID: validID(2), Error: TEMPORARY_FAILURES[4]},
		{ProjectID: validID(3), Error: HARD_FAILURES[0], Attempts: &one},
		{ProjectID: validID(4), Error: "x", Attempts: &four},
	}
	d := &Deps{
		GetFailedProjects: func(ctx context.Context) ([]Failure, error) { return recs, nil },
		ProcessUpdatesForProject: func(ctx context.Context, p string) error {
			mu.Lock()
			seen = append(seen, p)
			mu.Unlock()
			if p == validID(2) {
				return context.Canceled
			}
			return nil
		},
	}
	res, err := d.RetryFailures(cctx, Options{FailureType: "soft", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Succeeded) != 1 || res.Succeeded[0] != validID(1) {
		t.Fatalf("succeeded: %v", res.Succeeded)
	}
	if len(res.Failed) != 1 || res.Failed[0] != validID(2) {
		t.Fatalf("failed: %v", res.Failed)
	}
	if len(seen) != 2 {
		t.Fatalf("handler calls: %v", seen)
	}
}

func TestC8_RetryFailuresHard_UsesResync(t *testing.T) {
	one, two, four := 1, 2, 4
	recs := []Failure{
		{ProjectID: validID(1), Error: HARD_FAILURES[0], Attempts: &one, ResyncAttempts: intPtr(1)},
		{ProjectID: validID(2), Error: "sync ongoing x", Attempts: &one},
		{ProjectID: validID(3), Error: "plain", Attempts: &four},
		{ProjectID: validID(4), Error: HARD_FAILURES[1], ResyncAttempts: &two},
	}
	var mu sync.Mutex
	softCalls, hardCalls := []string{}, []string{}
	d := &Deps{
		GetFailedProjects: func(ctx context.Context) ([]Failure, error) { return recs, nil },
		StartResync: func(ctx context.Context, p string) error {
			mu.Lock()
			softCalls = append(softCalls, p)
			mu.Unlock()
			return nil
		},
		StartHardResync: func(ctx context.Context, p string) error {
			mu.Lock()
			hardCalls = append(hardCalls, p)
			mu.Unlock()
			return nil
		},
		GetHistoryID:            func(ctx context.Context, p string) (string, error) { return "hid-" + p, nil },
		GetFailureRecord:        func(ctx context.Context, p string) (*Failure, error) { return nil, nil },
		CountUnprocessedUpdates: func(ctx context.Context, p string) (int, error) { return 0, nil },
	}
	res, err := d.RetryFailures(cctx, Options{FailureType: "hard", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Succeeded) != 3 {
		t.Fatalf("succeeded: %v", res.Succeeded)
	}
	// rec 3 (resyncAttempts=1 >= MAX_SOFT_RESYNC_ATTEMPTS, not ongoing) → hard resync
	if len(hardCalls) != 1 || hardCalls[0] != validID(1) {
		t.Fatalf("hardCalls: %v", hardCalls)
	}
	if len(softCalls) != 2 {
		t.Fatalf("softCalls: %v", softCalls)
	}
}

func TestC8_UnknownFailureTypeNoop(t *testing.T) {
	d := &Deps{}
	res, err := d.RetryFailures(cctx, Options{FailureType: "weird"})
	if err != nil || res != nil {
		t.Fatalf("vendor: unmatched failureType → undefined → (%v, %v)", res, err)
	}
}

func TestC8_ResyncBadIDClearsAndReturns(t *testing.T) {
	cleared := 0
	d := &Deps{ClearError: func(ctx context.Context, p string) error { cleared++; return nil }}
	if err := d.resyncProject(cctx, "not-a-valid-id", false); err != nil {
		t.Fatalf("vendor: bad id → clearError + RETURN (no throw): %v", err)
	}
	if cleared != 1 {
		t.Fatalf("clearError calls: %d", cleared)
	}
}

func TestC8_ResyncNoHistoryID(t *testing.T) {
	d := &Deps{GetHistoryID: func(ctx context.Context, p string) (string, error) { return "", nil }}
	err := d.resyncProject(cctx, validID(9), false)
	if err == nil {
		t.Fatal("want wrap error")
	}
	re, ok := err.(*retryError)
	if !ok || re.msg != "failed to resync project" || re.cause == nil {
		t.Fatalf("wrap shape: %#v", err)
	}
	if re.cause.Error() != "no history id" {
		t.Fatalf("cause: %q", re.cause.Error())
	}
	if re.Info()["projectId"] != validID(9) || re.Info()["hard"] != false {
		t.Fatalf("info: %#v", re.Info())
	}
}

func TestC8_QueueNotEmptyAfter30(t *testing.T) {
	slews := 0
	d := &Deps{
		CountUnprocessedUpdates: func(ctx context.Context, p string) (int, error) { return 5, nil },
		Sleep:                   func(d2 time.Duration) { slews++ },
		GetHistoryID:            func(ctx context.Context, p string) (string, error) { return "h", nil },
		StartResync:             func(ctx context.Context, p string) error { return nil },
	}
	err := d.resyncProject(cctx, validID(1), false)
	if err == nil {
		t.Fatal("want wrap error")
	}
	cre, ok := err.(*retryError)
	if !ok || cre.cause.Error() != "queue not empty" {
		t.Fatalf("cause: %v", cre.cause)
	}
	if slews != 30 {
		t.Fatalf("slews: %d (want 30)", slews)
	}
}

func TestC8_RecordStillExists(t *testing.T) {
	one := 1
	d := &Deps{
		CountUnprocessedUpdates: func(ctx context.Context, p string) (int, error) { return 0, nil },
		GetFailureRecord: func(ctx context.Context, p string) (*Failure, error) {
			return &Failure{ProjectID: p, Attempts: &one}, nil
		},
		GetHistoryID:    func(ctx context.Context, p string) (string, error) { return "h", nil },
		StartResync:     func(ctx context.Context, p string) error { return nil },
		StartHardResync: func(ctx context.Context, p string) error { return nil },
		Sleep:           func(d2 time.Duration) {},
	}
	err := d.resyncProject(cctx, validID(2), true)
	if err == nil {
		t.Fatal("want wrap")
	}
	cre, ok := err.(*retryError)
	if !ok || cre.cause.Error() != "failure record still exists" {
		t.Fatalf("cause: %v", cre.cause)
	}
	if cre.Info()["hard"] != true {
		t.Fatalf("info hard: %#v", cre.Info())
	}
}

func TestC8_TimeoutStopEarly(t *testing.T) {
	recs := []Failure{}
	for i := 0; i < 5; i++ {
		recs = append(recs, Failure{ProjectID: validID(i), Error: HARD_FAILURES[0]})
	}
	ms := int64(0)
	calls := 0
	d := &Deps{
		GetFailedProjects: func(ctx context.Context) ([]Failure, error) { return recs, nil },
		Now:               func() time.Time { return time.UnixMilli(ms) },
		StartResync: func(ctx context.Context, p string) error {
			calls++
			ms += 200
			return nil
		},
		GetHistoryID:            func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetFailureRecord:        func(ctx context.Context, p string) (*Failure, error) { return nil, nil },
		CountUnprocessedUpdates: func(ctx context.Context, p string) (int, error) { return 0, nil },
	}
	res, _ := d.RetryFailures(cctx, Options{FailureType: "hard", Timeout: 500, Limit: 10})
	// vendor: break when elapsed > timeout → 3 attempts (200/400/600; 600>500 stops the 4th)
	if len(res.Succeeded) != 3 || calls != 3 {
		t.Fatalf("succeeded=%d calls=%d (want early break)", len(res.Succeeded), calls)
	}
}

func TestC8_LimitTruncates(t *testing.T) {
	one := 1
	recs := []Failure{}
	for i := 0; i < 6; i++ {
		recs = append(recs, Failure{ProjectID: validID(i), Error: HARD_FAILURES[0], Attempts: &one, ResyncAttempts: intPtr(1)})
	}
	d := &Deps{
		GetFailedProjects:       func(ctx context.Context) ([]Failure, error) { return recs, nil },
		StartHardResync:         func(ctx context.Context, p string) error { return nil },
		GetHistoryID:            func(ctx context.Context, p string) (string, error) { return "h", nil },
		GetFailureRecord:        func(ctx context.Context, p string) (*Failure, error) { return nil, nil },
		CountUnprocessedUpdates: func(ctx context.Context, p string) (int, error) { return 0, nil },
	}
	res, _ := d.RetryFailures(cctx, Options{FailureType: "hard", Limit: 3})
	if len(res.Succeeded) != 3 {
		t.Fatalf("limit: %d", len(res.Succeeded))
	}
}

func TestC8_ErrorsStrings(t *testing.T) {
	// R1/R2 exact strings
	if TEMPORARY_FAILURES[0] != "Error: ENOSPC: no space left on device, write" {
		t.Fatal("R1 exact string")
	}
	if HARD_FAILURES[0] != "Error: history store a non-success status code: 422" {
		t.Fatal("R2 exact string")
	}
	if MaxResyncAttempts != 2 || MaxSoftResyncAttempts != 1 || SyncOngoingErrMessage != "sync ongoing" {
		t.Fatal("R3 constants")
	}
}

var _ = strings.HasPrefix // (kept for clarity of intent)
