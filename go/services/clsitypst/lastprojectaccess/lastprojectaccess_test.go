package lastprojectaccess

import (
	"sync"
	"testing"
)

func TestGetAbsentDefaultsZero(t *testing.T) {
	if got := GetLastProjectAccessTime("ghost-project"); got != 0 {
		t.Errorf("Get(absent) = %d, want 0", got)
	}
}

func TestSetThenGet(t *testing.T) {
	SetLastProjectAccessTime("proj-1", 123456789)
	if got := GetLastProjectAccessTime("proj-1"); got != 123456789 {
		t.Errorf("Get(proj-1) = %d, want 123456789", got)
	}
}

func TestSetOverwrite(t *testing.T) {
	SetLastProjectAccessTime("proj-2", 1)
	SetLastProjectAccessTime("proj-2", 2)
	if got := GetLastProjectAccessTime("proj-2"); got != 2 {
		t.Errorf("Get(proj-2) = %d, want 2 (last write)", got)
	}
}

// TestConcurrentReadWrite verifies the mutex protects the last-access map.
func TestConcurrentReadWrite(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			SetLastProjectAccessTime("p", int64(i))
		}(i)
		go func() {
			defer wg.Done()
			GetLastProjectAccessTime("p")
		}()
	}
	wg.Wait()
}

func TestDelete(t *testing.T) {
	SetLastProjectAccessTime("del-1", 5)
	Delete("del-1")
	if got := GetLastProjectAccessTime("del-1"); got != 0 {
		t.Errorf("after Delete, got %d, want 0", got)
	}
	// Deletion of a non-existent key must be a no-op.
	Delete("ghost-del")
}

func TestSetIfNewerOnlyAdvances(t *testing.T) {
	SetLastProjectAccessTime("sn-1", 100)
	if got := GetLastProjectAccessTime("sn-1"); got != 100 {
		t.Fatalf("initial ts = %d, want 100", got)
	}
	// Older timestamp is rejected (ts > current is the gate; not >=).
	SetIfNewer("sn-1", 90)
	if got := GetLastProjectAccessTime("sn-1"); got != 100 {
		t.Errorf("SetIfNewer(90) must not regress to 100, got=%d", got)
	}
	// Equal timestamp: ts > current is false, so unchanged (documented).
	SetIfNewer("sn-1", 100)
	// Newer timestamp wins.
	SetIfNewer("sn-1", 200)
	if got := GetLastProjectAccessTime("sn-1"); got != 200 {
		t.Errorf("SetIfNewer(200) wins, got=%d", got)
	}
	// Absent key: 0 is not > 0, so SetIfNewer("a", 0) stays absent.
	SetIfNewer("sn-2", 0)
	if got := GetLastProjectAccessTime("sn-2"); got != 0 {
		t.Errorf("SetIfNewer(0) absent, got=%d", got)
	}
	// Absent key with positive ts.
	SetIfNewer("sn-3", 7)
	if got := GetLastProjectAccessTime("sn-3"); got != 7 {
		t.Errorf("SetIfNewer(7) absent, got=%d", got)
	}
}

func TestEachExpired(t *testing.T) {
	// Isolate: clear the keys this test owns (the map is a package global
	// shared by every test; assertions are scoped to the owned keys).
	for _, pid := range []string{"ex-1", "ex-2", "ex-3"} {
		Delete(pid)
	}
	SetLastProjectAccessTime("ex-1", 100)
	SetLastProjectAccessTime("ex-2", 500)
	SetLastProjectAccessTime("ex-3", 200)

	chk := func(before int64, want1, want2, want3 bool) {
		t.Helper()
		got := EachExpired(before)
		for _, pid := range []string{"ex-1", "ex-2", "ex-3"} {
			want := (pid == "ex-1" && want1) || (pid == "ex-2" && want2) || (pid == "ex-3" && want3)
			exp := false
			for _, s := range got {
				if s == pid {
					exp = true
					break
				}
			}
			if exp != want {
				t.Fatalf("beforeMs=%d: %s expired=%v, want %v (got=%v)", before, pid, exp, want, got)
			}
		}
	}
	// 100 < 300 (ex-1) -> expired; 500 >= 300 (ex-2) -> live; 200 < 300 (ex-3) -> expired.
	chk(300, true, false, true)
	// 500 == beforeMs (ex-2) -> NOT expired (strict <, Node parity).
	chk(500, true, false, true)
}
