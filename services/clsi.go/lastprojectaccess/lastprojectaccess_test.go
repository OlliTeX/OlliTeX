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
