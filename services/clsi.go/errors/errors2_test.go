package errors

import (
	"fmt"
	"testing"
)

func TestQueueLimitReachedError(t *testing.T) {
	if NewQueueLimitReachedError(nil).Message != "" {
		t.Error("nil-cause QueueLimitReachedError must have empty message")
	}
	e := fmt.Errorf("upstream")
	q := NewQueueLimitReachedError(e)
	if q.Message != e.Error() {
		t.Errorf("message = %q", q.Message)
	}
	if len(q.Info) != 0 {
		t.Errorf("Info = %v, want empty map", q.Info)
	}
}

func TestTimedOutError(t *testing.T) {
	e := fmt.Errorf("timeout after 200s")
	tm := NewTimedOutError(e)
	if tm.Message != e.Error() {
		t.Errorf("message = %q", tm.Message)
	}
	// The Node TimedOutError has no cause and no extra info surface:
	if tm.Info != nil {
		t.Errorf("Info = %v, want nil", tm.Info)
	}
}

func TestNoXrefTableError(t *testing.T) {
	n := NewNoXrefTableError(fmt.Errorf("no xref table found on disk"))
	if n.Message != "no xref table found on disk" {
		t.Errorf("message = %q", n.Message)
	}
}

func TestTooManyCompileRequestsError(t *testing.T) {
	e := NewTooManyCompileRequestsError("too many concurrent compile requests")
	if e.Message != "too many concurrent compile requests" {
		t.Errorf("message = %q", e.Message)
	}
	if e.Info != nil {
		t.Errorf("Info = %v, want nil map", e.Info)
	}
}
