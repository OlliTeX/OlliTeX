package db

import (
	"os"
	"testing"
	"time"
)

// 1:1 port of src/test/.../bridge/db/sqlite/SqliteDBStoreTest.java
// (plus DeleteFilesForProjectSQLUpdateTest).

func newTestStore(t *testing.T) *SqliteDBStore {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "dbStore.db")
	if err != nil {
		t.Fatalf("temp db: %v", err)
	}
	f.Close()
	s, err := NewSqliteDBStore(f.Name(), 0)
	if err != nil {
		t.Fatalf("new SqliteDBStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func millis(t time.Time) int64 { return t.UnixMilli() }

func TestGetNumProjects(t *testing.T) {
	dbStore := newTestStore(t)
	if n := dbStore.GetNumProjects(); n != 0 {
		t.Fatalf("getNumProjects = %d, want 0", n)
	}
	dbStore.SetLatestVersionForProject("asdf", 1)
	if n := dbStore.GetNumProjects(); n != 1 {
		t.Fatalf("getNumProjects = %d, want 1", n)
	}
	dbStore.SetLatestVersionForProject("asdf1", 2)
	if n := dbStore.GetNumProjects(); n != 2 {
		t.Fatalf("getNumProjects = %d, want 2", n)
	}
	dbStore.SetLatestVersionForProject("asdf1", 3)
	if n := dbStore.GetNumProjects(); n != 2 {
		t.Fatalf("getNumProjects = %d, want 2 (replace)", n)
	}
}

func TestSwapTableStartsOutEmpty(t *testing.T) {
	dbStore := newTestStore(t)
	if _, ok := dbStore.GetOldestUnswappedProject(); ok {
		t.Fatalf("oldestUnswapped present, want nil")
	}
}

func TestGetOldestUnswappedProject(t *testing.T) {
	dbStore := newTestStore(t)
	now := time.Now()
	dbStore.SetLatestVersionForProject("older", 3)
	dbStore.SetLastAccessedTime("older", ptr(millis(now.Add(-5*time.Second))))
	dbStore.SetLatestVersionForProject("asdf", 1)
	dbStore.SetLastAccessedTime("asdf", ptr(millis(now.Add(-1*time.Second))))
	got, ok := dbStore.GetOldestUnswappedProject()
	if !ok || got != "older" {
		t.Fatalf("oldestUnswapped = %v, want older", got)
	}
	dbStore.SetLastAccessedTime("older", ptr(millis(now)))
	got, _ = dbStore.GetOldestUnswappedProject()
	if got != "asdf" {
		t.Fatalf("oldestUnswapped = %v, want asdf", got)
	}
}

func TestSwapAndRestore(t *testing.T) {
	dbStore := newTestStore(t)
	projectName, compression := "something", "bzip2"
	dbStore.SetLatestVersionForProject(projectName, 42)
	dbStore.Swap(projectName, compression)
	_, ok := dbStore.GetOldestUnswappedProject()
	if ok {
		t.Fatalf("expected no unswapped project after swap")
	}
	if got, _ := dbStore.GetSwapCompression(projectName); got != compression {
		t.Fatalf("swap compression = %v, want %v", got, compression)
	}
	// and restore
	dbStore.Restore(projectName)
	if got, ok := dbStore.GetSwapCompression(projectName); ok {
		t.Fatalf("swap compression after restore = %v, want nil", got)
	}
}

func TestNoOldestProjectIfAllEvicted(t *testing.T) {
	dbStore := newTestStore(t)
	dbStore.SetLatestVersionForProject("older", 3)
	dbStore.Swap("older", "bzip2")
	if _, ok := dbStore.GetOldestUnswappedProject(); ok {
		t.Fatalf("expected nil oldest project (all evicted)")
	}
}

func TestNullLastAccessedTimesDoNotCount(t *testing.T) {
	dbStore := newTestStore(t)
	now := time.Now()
	dbStore.SetLatestVersionForProject("older", 2)
	dbStore.SetLastAccessedTime("older", ptr(millis(now.Add(-5*time.Second))))
	dbStore.SetLatestVersionForProject("newer", 3)
	dbStore.SetLastAccessedTime("newer", ptr(millis(now)))
	if got, _ := dbStore.GetOldestUnswappedProject(); got != "older" {
		t.Fatalf("oldestUnswapped = %v, want older", got)
	}
	dbStore.Swap("older", "bzip2")
	if got, _ := dbStore.GetOldestUnswappedProject(); got != "newer" {
		t.Fatalf("oldestUnswapped after swap = %v, want newer", got)
	}
}

func TestMissingProjectLastAccessedTimeCanBeSet(t *testing.T) {
	dbStore := newTestStore(t)
	dbStore.SetLatestVersionForProject("asdf", 1)
	dbStore.SetLastAccessedTime("asdf", ptr(millis(time.Now())))
	if got, _ := dbStore.GetOldestUnswappedProject(); got != "asdf" {
		t.Fatalf("oldestUnswapped = %v, want asdf", got)
	}
}

func TestGetNumUnswappedProjects(t *testing.T) {
	dbStore := newTestStore(t)
	dbStore.SetLatestVersionForProject("asdf", 1)
	dbStore.SetLastAccessedTime("asdf", ptr(millis(time.Now())))
	if n := dbStore.GetNumUnswappedProjects(); n != 1 {
		t.Fatalf("numUnswapped = %d, want 1", n)
	}
	dbStore.Swap("asdf", "bzip2")
	if n := dbStore.GetNumUnswappedProjects(); n != 0 {
		t.Fatalf("numUnswapped = %d, want 0", n)
	}
}

func TestProjectState(t *testing.T) {
	dbStore := newTestStore(t)

	// projectStateIsNotPresentIfNotInDBAtAll
	if got := dbStore.GetProjectState("asdf"); got != ProjectStateNotPresent {
		t.Fatalf("state for missing project = %v, want NOT_PRESENT", got)
	}
	// projectStateIsPresentIfProjectHasLastAccessed
	dbStore.SetLatestVersionForProject("asdf", 1)
	dbStore.SetLastAccessedTime("asdf", ptr(millis(time.Now())))
	if got := dbStore.GetProjectState("asdf"); got != ProjectStatePresent {
		t.Fatalf("state with lastAccessed = %v, want PRESENT", got)
	}
	// projectStateIsSwappedIfLastAccessedIsNull
	dbStore.Swap("asdf", "bzip2")
	if got := dbStore.GetProjectState("asdf"); got != ProjectStateSwapped {
		t.Fatalf("state after swap = %v, want SWAPPED", got)
	}
}

func TestDeleteProject(t *testing.T) {
	dbStore := newTestStore(t)
	dbStore.SetLatestVersionForProject("project1", 1)
	dbStore.SetLatestVersionForProject("project2", 1)
	if got := dbStore.GetProjectState("project1"); got != ProjectStatePresent {
		t.Fatalf("state project1 = %v, want PRESENT", got)
	}
	if got := dbStore.GetProjectState("project2"); got != ProjectStatePresent {
		t.Fatalf("state project2 = %v, want PRESENT", got)
	}
	dbStore.DeleteProject("project1")
	if got := dbStore.GetProjectState("project1"); got != ProjectStateNotPresent {
		t.Fatalf("state project1 after delete = %v, want NOT_PRESENT", got)
	}
	if got := dbStore.GetProjectState("project2"); got != ProjectStatePresent {
		t.Fatalf("state project2 = %v, want PRESENT", got)
	}
}

// DeleteFilesForProjectSQLUpdateTest: asserts the exact Java SQL string.
func TestDeleteFilesForProjectSQL(t *testing.T) {
	q, _ := DeleteFilesForProjectSQL("projname", "path1", "path2")
	want := "DELETE FROM `url_index_store` WHERE `project_name` = ? AND path IN (?, ?);\n"
	if q != want {
		t.Fatalf("SQL = %q, want %q", q, want)
	}
	empty, _ := DeleteFilesForProjectSQL("projname")
	wantEmpty := "DELETE FROM `url_index_store` WHERE `project_name` = ? AND path IN ();\n"
	if empty != wantEmpty {
		t.Fatalf("empty SQL = %q, want %q", empty, wantEmpty)
	}
}

func TestURLIndexRoundTrip(t *testing.T) {
	dbStore := newTestStore(t)
	dbStore.SetLatestVersionForProject("p", 1)
	dbStore.AddURLIndexForProject("p", "https://x/1", "f.tex")
	if got, ok := dbStore.GetPathForURLInProject("p", "https://x/1"); !ok || got != "f.tex" {
		t.Fatalf("pathForUrl = %v, want f.tex", got)
	}
	if got, _ := dbStore.GetPathForURLInProject("p", "nope"); got != "" {
		t.Fatalf("pathForUrl for missing = %v, want empty", got)
	}
	dbStore.DeleteFilesForProject("p", "f.tex")
	if got, ok := dbStore.GetPathForURLInProject("p", "https://x/1"); ok {
		t.Fatalf("pathForUrl after delete = %v, want nil", got)
	}
}

func ptr(i int64) *int64 { return &i }
