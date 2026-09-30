// Coverage for ProjectState.String, the NoopDbStore surface, and the
// SqliteDBStore error branches. The error branches (the `panic(fmt.Errorf...)`
// in each method) are reached by calling a method after Close(): the closed
// handle makes Query/Exec return an error, which is panicked and recovered.
// This mirrors SqliteDBStoreTest (happy paths) plus the error paths the Java
// tests exercise via DBInitException / a closed statement.
package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// ProjectState.String
// ---------------------------------------------------------------------------

func TestProjectStateString(t *testing.T) {
	if got := ProjectStateNotPresent.String(); got != "NOT_PRESENT" {
		t.Fatalf("NOT_PRESENT string = %q", got)
	}
	if got := ProjectStatePresent.String(); got != "PRESENT" {
		t.Fatalf("PRESENT string = %q", got)
	}
	if got := ProjectStateSwapped.String(); got != "SWAPPED" {
		t.Fatalf("SWAPPED string = %q", got)
	}
	// unknown state -> "?" (the Java default case).
	if got := ProjectState(99).String(); got != "?" {
		t.Fatalf("unknown state string = %q, want ? ", got)
	}
}

// ---------------------------------------------------------------------------
// NoopDbStore — full surface (ports noop/NoopDbStore).
// ---------------------------------------------------------------------------

func TestNoopDbStoreSurface(t *testing.T) {
	s := NoopDbStore{}
	if got := s.GetNumProjects(); got != 0 {
		t.Fatalf("Noop GetNumProjects = %d, want 0", got)
	}
	if got := s.GetProjectNames(); got != nil {
		t.Fatalf("Noop GetProjectNames = %v, want nil", got)
	}
	if got, ok := s.GetPathForURLInProject("p", "u"); ok || got != "" {
		t.Fatalf("Noop GetPathForURLInProject = %v %v, want \"\" false", got, ok)
	}
	if got, ok := s.GetOldestUnswappedProject(); ok || got != "" {
		t.Fatalf("Noop GetOldestUnswappedProject = %v %v, want \"\" false", got, ok)
	}
	if got, ok := s.GetSwapCompression("p"); ok || got != "" {
		t.Fatalf("Noop GetSwapCompression = %v %v, want \"\" false", got, ok)
	}
	if got := s.GetNumUnswappedProjects(); got != 0 {
		t.Fatalf("Noop GetNumUnswappedProjects = %d, want 0", got)
	}
	if got := s.GetProjectState("p"); got != ProjectStateNotPresent {
		t.Fatalf("Noop GetProjectState = %v, want NOT_PRESENT", got)
	}
	// no-op methods do not panic.
	s.SetLatestVersionForProject("p", 1)
	s.AddURLIndexForProject("p", "u", "f")
	s.DeleteFilesForProject("p", "f")
	s.Swap("p", "bzip2")
	s.Restore("p")
	s.SetLastAccessedTime("p", nil)
	s.DeleteProject("p")
	s.Close()
	_ = s.GetLatestVersionForProject("p")
}

// ---------------------------------------------------------------------------
// NewSqliteDBStore error branches
// ---------------------------------------------------------------------------

func TestNewSqliteDBStoreNotADirectory(t *testing.T) {
	base := t.TempDir()
	// Make the "parent" path a regular file: the dbFile's Dir() is that file,
	// so Stat finds it and it is not a directory => "not a directory" error.
	blocker := filepath.Join(base, "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	dbFile := filepath.Join(blocker, "nested.db")
	if _, err := NewSqliteDBStore(dbFile, 0); err == nil {
		t.Fatalf("NewSqliteDBStore must fail when the parent is a file")
	}
}

func TestNewSqliteDBStoreCannotCreateParent(t *testing.T) {
	base := t.TempDir()
	// Make the intermediate path a regular file: the parent does not exist
	// (Stat error) and MkdirAll cannot create it because a component is a
	// file => "unable to create" error.
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}
	dbFile := filepath.Join(blocker, "nested", "nested.db")
	if _, err := NewSqliteDBStore(dbFile, 0); err == nil {
		t.Fatalf("NewSqliteDBStore must fail when it cannot create the parent dir")
	}
}

// ---------------------------------------------------------------------------
// Error branches: Part B B6 rewired the closed-handle (DB-error) class from
// panic to a logged safe-default — a panic in the gitbridge swap-job
// goroutine is a process-kill on a transient hiccup. Every method must now
// return its zero value without panicking.
// ---------------------------------------------------------------------------

func assertNoPanicsNow(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s: panicked on DB error (Part B B6: must degrade safely): %v", name, r)
		}
	}()
	fn()
}

func TestSqliteDBStoreSafeAfterClose(t *testing.T) {
	s, err := openTempStore(t)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// A non-empty store so every query returns a row before the close.
	s.SetLatestVersionForProject("p", 1)
	s.AddURLIndexForProject("p", "u", "f")
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	assertNoPanicsNow(t, "GetNumProjects", func() {
		if got := s.GetNumProjects(); got != 0 {
			t.Fatalf("GetNumProjects: expected safe 0, got %d", got)
		}
	})
	assertNoPanicsNow(t, "GetProjectNames", func() {
		if got := s.GetProjectNames(); got != nil {
			t.Fatalf("GetProjectNames: expected safe nil, got %v", got)
		}
	})
	assertNoPanicsNow(t, "SetLatestVersionForProject", func() { s.SetLatestVersionForProject("p", 1) })
	assertNoPanicsNow(t, "GetLatestVersionForProject", func() {
		if got := s.GetLatestVersionForProject("p"); got != 0 {
			t.Fatalf("GetLatestVersionForProject: expected safe 0, got %d", got)
		}
	})
	assertNoPanicsNow(t, "AddURLIndexForProject", func() { s.AddURLIndexForProject("p", "u", "f") })
	assertNoPanicsNow(t, "DeleteFilesForProject", func() { s.DeleteFilesForProject("p", "f") })
	assertNoPanicsNow(t, "GetPathForURLInProject", func() {
		if p, ok := s.GetPathForURLInProject("p", "u"); p != "" || ok {
			t.Fatalf("GetPathForURLInProject: expected safe empty, got (%q, %v)", p, ok)
		}
	})
	assertNoPanicsNow(t, "GetOldestUnswappedProject", func() {
		if n, ok := s.GetOldestUnswappedProject(); n != "" || ok {
			t.Fatalf("GetOldestUnswappedProject: expected safe empty, got (%q, %v)", n, ok)
		}
	})
	assertNoPanicsNow(t, "Swap", func() { s.Swap("p", "bzip2") })
	assertNoPanicsNow(t, "Restore", func() { s.Restore("p") })
	assertNoPanicsNow(t, "GetSwapCompression", func() {
		if c, ok := s.GetSwapCompression("p"); c != "" || ok {
			t.Fatalf("GetSwapCompression: expected safe empty, got (%q, %v)", c, ok)
		}
	})
	assertNoPanicsNow(t, "GetNumUnswappedProjects", func() {
		if got := s.GetNumUnswappedProjects(); got != 0 {
			t.Fatalf("GetNumUnswappedProjects: expected safe 0, got %d", got)
		}
	})
	assertNoPanicsNow(t, "GetProjectState", func() {
		if got := s.GetProjectState("p"); got != ProjectStateNotPresent {
			t.Fatalf("GetProjectState: expected safe NOT_PRESENT, got %v", got)
		}
	})
	assertNoPanicsNow(t, "SetLastAccessedTime", func() { s.SetLastAccessedTime("p", ptr(1)) })
	assertNoPanicsNow(t, "DeleteProject", func() { s.DeleteProject("p") })
	assertNoPanicsNow(t, "PostbackPut", func() { s.PostbackPut("p", "k", "s", 1, "b") })
	assertNoPanicsNow(t, "PostbackGet", func() {
		if _, _, _, _, found := s.PostbackGet("p"); found {
			t.Fatalf("PostbackGet: expected safe found=false")
		}
	})
	assertNoPanicsNow(t, "PostbackDelete", func() { s.PostbackDelete("p") })
}

func TestExecErrorBranch(t *testing.T) {
	s, _ := openTempStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := s.exec("SELECT 1"); err == nil {
		t.Fatalf("exec on closed DB must return an error")
	}
}

// exec on a live DB returns nil (the happy branch), already exercised by the
// happy-path tests, so no extra coverage is required here.

func openTempStore(t *testing.T) (*SqliteDBStore, error) {
	f, err := os.CreateTemp(t.TempDir(), "dbStore.db")
	if err != nil {
		return nil, err
	}
	f.Close()
	s, err := NewSqliteDBStore(f.Name(), 0)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, nil
}

// ---------------------------------------------------------------------------
// createTables' inner error branches (via a raw handle that we close before
// invoking createTables directly, so every s.exec fails and surfaces the
// `if err != nil` paths).
// ---------------------------------------------------------------------------

func TestCreateTablesPanicsAfterClose(t *testing.T) {
	s, _ := openTempStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := s.createTables(0); err == nil {
		t.Fatalf("createTables on a closed handle must return an error")
	}
}

// createTables' "create projects table" error branch: a VIEW named
// `projects` collides with CREATE TABLE IF NOT EXISTS (SQLite: "table or
// view already exists"), so createTables returns the wrapped error.
func TestCreateTablesPreexistingViewFails(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "view.sqlite")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	f.Close()
	db, err := sql.Open("sqlite3", f.Name())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE VIEW `projects` AS SELECT 1 AS name"); err != nil {
		t.Fatalf("seed view: %v", err)
	}
	store := &SqliteDBStore{db: db}
	if err := store.createTables(0); err == nil {
		t.Fatalf("createTables must fail when a colliding view exists")
	}
}

// createTables' "create project_path_index" error branch: a pre-existing
// url_index_store table lacking the indexed columns makes
// CREATE UNIQUE INDEX fail.
func TestCreateTablesIndexError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "badindex.sqlite")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	f.Close()
	db, err := sql.Open("sqlite3", f.Name())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE `url_index_store` (x INTEGER)"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	store := &SqliteDBStore{db: db}
	if err := store.createTables(0); err == nil {
		t.Fatalf("createTables must fail when the index cannot be created")
	}
}

// columnExists branches: true (existing column), false (missing column but
// table present), false (no table -> Query error -> return false).
func TestColumnExistsBranches(t *testing.T) {
	s, _ := openTempStore(t)
	// true: a real column on a real table.
	if !s.columnExists("projects", "name") {
		t.Fatalf("columnExists(name) = false, want true")
	}
	// false (no such column, no error):
	if s.columnExists("projects", "no_such_column") {
		t.Fatalf("columnExists(no_such) = true, want false")
	}
	// false (no such table -> Query error -> early return false):
	if s.columnExists("no_such_table", "x") {
		t.Fatalf("columnExists(bad table) = true, want false")
	}
}

func TestGetProjectNamesWithRows(t *testing.T) {
	s, _ := openTempStore(t)
	s.SetLatestVersionForProject("a", 1)
	s.SetLatestVersionForProject("b", 2)
	s.SetLatestVersionForProject("c", 3)
	if got := s.GetProjectNames(); len(got) != 3 {
		t.Fatalf("GetProjectNames len = %d, want 3", len(got))
	}
	// absent project: version stays 0.
	if got := s.GetLatestVersionForProject("ghost"); got != 0 {
		t.Fatalf("absent version = %d, want 0", got)
	}
	// present projects: the inner Scan loop body runs for each row.
	for i, n := range []string{"a", "b", "c"} {
		if got := s.GetLatestVersionForProject(n); got != i+1 {
			t.Fatalf("GetLatestVersionForProject(%s) = %d, want %d", n, got, i+1)
		}
	}
}
