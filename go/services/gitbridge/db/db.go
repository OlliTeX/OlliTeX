// Package db ports bridge/db: the DBStore API, ProjectState, the no-op
// implementation, and the SQLite implementation (SqliteDBStore) with the
// query/update SQL taken verbatim from the Java classes.
//
// Timestamp compatibility (verified empirically against org.xerial
// sqlite-jdbc 3.41.2.2):
//
//   - `INSERT ... DATETIME('now')`   -> stored as TEXT
//   - `setTimestamp` / UpdateSwap    -> stored as INTEGER epoch millis
//
// Both encodings live in `last_accessed`. State and ordering queries only
// compare NULL vs non-NULL or run MIN(last_accessed) — both engine-computed
// and identical for both drivers, regardless of encoding.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ---------------------------------------------------------------------------
// ProjectState — ports bridge/db/ProjectState.
// ---------------------------------------------------------------------------

type ProjectState int

const (
	ProjectStateNotPresent ProjectState = iota
	ProjectStatePresent
	ProjectStateSwapped
)

func (s ProjectState) String() string {
	switch s {
	case ProjectStateNotPresent:
		return "NOT_PRESENT"
	case ProjectStatePresent:
		return "PRESENT"
	case ProjectStateSwapped:
		return "SWAPPED"
	default:
		return "?"
	}
}

// ---------------------------------------------------------------------------
// DBStore — ports bridge/db/DBStore.
//
// Java returns null for "absent"; Go returns (value, ok) pairs. Java's
// setLastAccessedTime takes a nullable Timestamp; Go takes *int64 epoch
// millis (nil = SQL NULL: "swapped").
// ---------------------------------------------------------------------------

type DBStore interface {
	// Close closes the underlying connection. Ports java Closeable.close.
	GetNumProjects() int
	GetProjectNames() []string
	SetLatestVersionForProject(project string, versionID int)
	GetLatestVersionForProject(project string) int
	AddURLIndexForProject(projectName, url, path string)
	// DeleteFilesForProject mirrors DeleteFilesForProjectSQLUpdate (an empty
	// list becomes `IN ()`, a no-op on modern SQLite).
	DeleteFilesForProject(project string, files ...string)
	GetPathForURLInProject(projectName, url string) (path string, ok bool)
	GetOldestUnswappedProject() (projectName string, ok bool)
	Swap(projectName, compressionMethod string)
	Restore(projectName string)
	GetSwapCompression(projectName string) (compression string, ok bool)
	GetNumUnswappedProjects() int
	GetProjectState(projectName string) ProjectState
	// SetLastAccessedTime: t nil means SQL NULL (the project is swapped).
	SetLastAccessedTime(projectName string, t *int64)
	DeleteProject(projectName string)
	Close() error
}

// ---------------------------------------------------------------------------
// NoopDbStore — ports noop/NoopDbStore.
// ---------------------------------------------------------------------------

type NoopDbStore struct{}

func (NoopDbStore) GetNumProjects() int                          { return 0 }
func (NoopDbStore) GetProjectNames() []string                    { return nil }
func (NoopDbStore) SetLatestVersionForProject(string, int)       {}
func (NoopDbStore) GetLatestVersionForProject(string) int        { return 0 }
func (NoopDbStore) AddURLIndexForProject(string, string, string) {}
func (NoopDbStore) DeleteFilesForProject(string, ...string)      {}
func (NoopDbStore) GetPathForURLInProject(string, string) (string, bool) {
	return "", false
}
func (NoopDbStore) GetOldestUnswappedProject() (string, bool) { return "", false }
func (NoopDbStore) Swap(string, string)                       {}
func (NoopDbStore) Restore(string)                            {}
func (NoopDbStore) GetSwapCompression(string) (string, bool)  { return "", false }
func (NoopDbStore) GetNumUnswappedProjects() int              { return 0 }
func (NoopDbStore) GetProjectState(string) ProjectState {
	return ProjectStateNotPresent
}
func (NoopDbStore) SetLastAccessedTime(string, *int64) {}
func (NoopDbStore) DeleteProject(string)               {}
func (NoopDbStore) Close()                             {}

// ---------------------------------------------------------------------------
// SqliteDBStore — ports bridge/db/sqlite/SqliteDBStore.
// ---------------------------------------------------------------------------

type SqliteDBStore struct {
	db *sql.DB
}

// NewSqliteDBStore ports SqliteDBStore(File, int). Java throws
// DBInitException; Go returns an error.
func NewSqliteDBStore(dbFile string, heapLimitBytes int) (*SqliteDBStore, error) {
	parent := filepath.Dir(dbFile)
	if info, err := os.Stat(parent); err == nil && !info.IsDir() {
		return nil, fmt.Errorf("db: %s is not a directory", parent)
	}
	if info, _ := os.Stat(parent); info == nil {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return nil, fmt.Errorf(
				"db: %s directory didn't exist, and unable to create. Check your permissions.", parent)
		}
	}
	conn, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		return nil, fmt.Errorf("db: unable to connect to DB: %w", err)
	}
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("db: unable to connect to DB: %w", err)
	}
	s := &SqliteDBStore{db: conn}
	if err := s.createTables(heapLimitBytes); err != nil {
		return nil, err
	}
	return s, nil
}

// Close closes the underlying connection.
func (s *SqliteDBStore) Close() error { return s.db.Close() }

func (s *SqliteDBStore) exec(query string, args ...any) error {
	if _, err := s.db.Exec(query, args...); err != nil {
		return err
	}
	return nil
}

// createTables ports SqliteDBStore.createTables: best-effort migrations
// (Java swallows each migration's SQLException), the table/index creation,
// and the four schema-presence assertions (Preconditions.checkState).
func (s *SqliteDBStore) createTables(heapLimitBytes int) error {
	// Best-effort migrations.
	_, _ = s.db.Exec(fmt.Sprintf("PRAGMA soft_heap_limit=%d;", heapLimitBytes))
	_, _ = s.db.Exec("ALTER TABLE `projects`\nADD COLUMN `last_accessed` DATETIME NULL DEFAULT 0")
	_, _ = s.db.Exec("ALTER TABLE `projects`\nADD COLUMN `swap_time` DATETIME NULL;\n")
	_, _ = s.db.Exec("ALTER TABLE `projects`\nADD COLUMN `restore_time` DATETIME NULL;\n")
	_, _ = s.db.Exec("ALTER TABLE `projects`\nADD COLUMN `swap_compression` VARCHAR NULL;\n")

	if err := s.exec(
		"CREATE TABLE IF NOT EXISTS `projects` (\n" +
			"    `name` VARCHAR NOT NULL DEFAULT '',\n" +
			"    `version_id` INT NOT NULL DEFAULT 0,\n" +
			"    `last_accessed` DATETIME NULL DEFAULT 0,\n" +
			"    `swap_time` DATETIME NULL,\n" +
			"    `restore_time` DATETIME NULL,\n" +
			"    `swap_compression` VARCHAR NULL,\n" +
			"    PRIMARY KEY (`name`)\n" +
			")",
	); err != nil {
		return fmt.Errorf("db: create projects table: %w", err)
	}
	if err := s.exec(
		"CREATE INDEX IF NOT EXISTS `projects_index_last_accessed`\n" +
			"    ON `projects`(`last_accessed`)",
	); err != nil {
		return fmt.Errorf("db: create projects index: %w", err)
	}
	if err := s.exec(
		"CREATE TABLE IF NOT EXISTS `url_index_store` (\n" +
			"  `project_name` varchar(10) NOT NULL DEFAULT '',\n" +
			"  `url` text NOT NULL,\n" +
			"  `path` text NOT NULL,\n" +
			"  PRIMARY KEY (`project_name`,`url`),\n" +
			"  CONSTRAINT `url_index_store_ibfk_1` " +
			"FOREIGN KEY (`project_name`) " +
			"REFERENCES `projects` (`name`) " +
			"ON DELETE CASCADE " +
			"ON UPDATE CASCADE\n" +
			");\n",
	); err != nil {
		return fmt.Errorf("db: create url_index_store: %w", err)
	}
	if err := s.exec(
		"CREATE UNIQUE INDEX IF NOT EXISTS `project_path_index` " +
			"ON `url_index_store`(`project_name`, `path`);\n",
	); err != nil {
		return fmt.Errorf("db: create project_path_index: %w", err)
	}
	// postback_store — cross-process promise state (Option 3a: the push runs
	// in a proc-receive hook process, the postback POST lands in the serve
	// process; the shared sqlite file is the only state both see). Not in
	// the Java schema (Java's promise is in-JVM memory); the observable
	// client/server contract (push completes on the postback, 200/409 cells
	// of the postback endpoint) is unchanged.
	if err := s.exec(
		"CREATE TABLE IF NOT EXISTS `postback_store` (\n" +
			"  `project` VARCHAR NOT NULL,\n" +
			"  `key` text NOT NULL,\n" +
			"  `status` VARCHAR NOT NULL,\n" +
			"  `version_id` INT NOT NULL DEFAULT 0,\n" +
			"  `body` text NOT NULL DEFAULT '',\n" +
			"  PRIMARY KEY (`project`)\n" +
		")",
	); err != nil {
		return fmt.Errorf("db: create postback_store: %w", err)
	}

	for _, col := range []string{"last_accessed", "swap_time", "restore_time", "swap_compression"} {
		if !s.columnExists("projects", col) {
			return fmt.Errorf("db: %s missing after migration (schema change failed)", col)
		}
	}
	return nil
}

// columnExists ports the PRAGMA table_info(`projects`) existence checks.
func (s *SqliteDBStore) columnExists(table, column string) bool {
	rows, err := s.db.Query("PRAGMA table_info(`" + table + "`)")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// DBStore implementation (1:1 ports of the Java query/update classes).
// ---------------------------------------------------------------------------

// GetNumProjects — GetNumProjects.
func (s *SqliteDBStore) GetNumProjects() int {
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*)\n    FROM `projects`").Scan(&n); err != nil {
		panic(fmt.Errorf("db: getNumProjects: %w", err))
	}
	return n
}

// GetProjectNames — GetProjectNamesSQLQuery.
func (s *SqliteDBStore) GetProjectNames() []string {
	rows, err := s.db.Query("SELECT `name` FROM `projects`")
	if err != nil {
		panic(fmt.Errorf("db: getProjectNames: %w", err))
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			panic(fmt.Errorf("db: getProjectNames scan: %w", err))
		}
		names = append(names, name)
	}
	return names
}

// SetLatestVersionForProject — SetProjectSQLUpdate
// (`last_accessed` = SQL DATETIME('now'), TEXT now in both drivers).
func (s *SqliteDBStore) SetLatestVersionForProject(projectName string, versionID int) {
	if err := s.exec(
		"INSERT OR REPLACE INTO `projects`(`name`, `version_id`, `last_accessed`) VALUES (?, ?, DATETIME('now'));\n",
		projectName, versionID,
	); err != nil {
		panic(fmt.Errorf("db: setLatestVersionForProject: %w", err))
	}
}

// GetLatestVersionForProject — GetLatestVersionForProjectSQLQuery (default 0).
func (s *SqliteDBStore) GetLatestVersionForProject(projectName string) int {
	rows, err := s.db.Query(
		"SELECT `version_id` FROM `projects` WHERE `name` = ?", projectName)
	if err != nil {
		panic(fmt.Errorf("db: getLatestVersionForProject: %w", err))
	}
	defer rows.Close()
	versionID := 0
	for rows.Next() {
		if err := rows.Scan(&versionID); err != nil {
			panic(fmt.Errorf("db: getLatestVersionForProject scan: %w", err))
		}
	}
	return versionID
}

// AddURLIndexForProject — AddURLIndexSQLUpdate.
func (s *SqliteDBStore) AddURLIndexForProject(projectName, url, path string) {
	if err := s.exec(
		"INSERT OR REPLACE INTO `url_index_store`(`project_name`, `url`, `path`) VALUES (?, ?, ?)\n",
		projectName, url, path,
	); err != nil {
		panic(fmt.Errorf("db: addURLIndexForProject: %w", err))
	}
}

// DeleteFilesForProject — DeleteFilesForProjectSQLUpdate. SQL is built like
// Java; empty list => `IN ()`, a no-op on modern SQLite (verified).
func (s *SqliteDBStore) DeleteFilesForProject(projectName string, files ...string) {
	q, args := DeleteFilesForProjectSQL(projectName, files...)
	if _, err := s.db.Exec(q, args...); err != nil {
		panic(fmt.Errorf("db: deleteFilesForProject: %w", err))
	}
}

// DeleteFilesForProjectSQL builds the exact Java SQL (DeleteFilesForProject
// SQLUpdateTest asserts this string) and positional args.
func DeleteFilesForProjectSQL(projectName string, files ...string) (string, []any) {
	q := "DELETE FROM `url_index_store` WHERE `project_name` = ? AND path IN ("
	args := []any{projectName}
	for i, f := range files {
		q += "?"
		if i < len(files)-1 {
			q += ", "
		}
		args = append(args, f)
	}
	q += ");\n"
	return q, args
}

// GetPathForURLInProject — GetPathForURLInProjectSQLQuery.
func (s *SqliteDBStore) GetPathForURLInProject(projectName, url string) (string, bool) {
	rows, err := s.db.Query(
		"SELECT `path` FROM `url_index_store` WHERE `project_name` = ? AND `url` = ?",
		projectName, url)
	if err != nil {
		panic(fmt.Errorf("db: getPathForURLInProject: %w", err))
	}
	defer rows.Close()
	path := sql.NullString{}
	for rows.Next() {
		if err := rows.Scan(&path); err != nil {
			panic(fmt.Errorf("db: getPathForURLInProject scan: %w", err))
		}
	}
	return path.String, path.Valid
}

// PostbackPut stores/replaces the cross-process postback promise state for a
// project. status ∈ {"pending", "upToDate", "err:<code>"}; body is the raw
// postback payload (for error reconstruction on the waiter side).
func (s *SqliteDBStore) PostbackPut(project, key, status string, versionID int, body string) {
	_, err := s.db.Exec(
		"INSERT INTO `postback_store` (`project`,`key`,`status`,`version_id`,`body`) VALUES (?,?,?,?,?) " +
			"ON CONFLICT(`project`) DO UPDATE SET `key`=excluded.`key`, `status`=excluded.`status`, `version_id`=excluded.`version_id`, `body`=excluded.`body`",
		project, key, status, versionID, body)
	if err != nil {
		panic(fmt.Errorf("db: postbackPut: %w", err))
	}
}

// PostbackGet reads the postback promise state for a project (found=false when absent).
func (s *SqliteDBStore) PostbackGet(project string) (key, status string, versionID int, body string, found bool) {
	row := s.db.QueryRow("SELECT `key`,`status`,`version_id`,`body` FROM `postback_store` WHERE `project` = ?", project)
	var v int
	var b string
	if err := row.Scan(&key, &status, &v, &b); err != nil {
		if err == sql.ErrNoRows {
			return "", "", 0, "", false
		}
		panic(fmt.Errorf("db: postbackGet: %w", err))
	}
	return key, status, v, b, true
}

// PostbackDelete removes the postback promise state for a project.
func (s *SqliteDBStore) PostbackDelete(project string) {
	if _, err := s.db.Exec("DELETE FROM `postback_store` WHERE `project` = ?", project); err != nil {
		panic(fmt.Errorf("db: postbackDelete: %w", err))
	}
}

// GetOldestUnswappedProject — GetOldestProjectName, verbatim SQL (bare MIN
// without GROUP BY; SQLite returns the MIN row). Java only reads `name`;
// MIN is engine-computed and identical across drivers.
func (s *SqliteDBStore) GetOldestUnswappedProject() (string, bool) {
	rows, err := s.db.Query(
		"SELECT `name`, MIN(`last_accessed`)\n" +
			"    FROM `projects` \n" +
			"    WHERE `last_accessed` IS NOT NULL;")
	if err != nil {
		panic(fmt.Errorf("db: getOldestUnswappedProject: %w", err))
	}
	defer rows.Close()
	name := sql.NullString{}
	if rows.Next() {
		var min any // value unused (could be INTEGER epoch-s or TEXT)
		if err := rows.Scan(&name, &min); err != nil {
			panic(fmt.Errorf("db: getOldestUnswappedProject scan: %w", err))
		}
	}
	return name.String, name.Valid
}

// Swap — UpdateSwap. Xerial setTimestamp stores INTEGER epoch millis;
// Go mirrors that.
func (s *SqliteDBStore) Swap(projectName, compression string) {
	now := unixMillisNow()
	if err := s.exec(
		"UPDATE `projects`\n"+
			"SET `last_accessed` = NULL,\n"+
			"    `swap_time` = ?,\n"+
			"    `restore_time` = NULL,\n"+
			"    `swap_compression` = ?\n"+
			"WHERE `name` = ?;\n",
		now, compression, projectName,
	); err != nil {
		panic(fmt.Errorf("db: swap: %w", err))
	}
}

// Restore — UpdateRestore.
func (s *SqliteDBStore) Restore(projectName string) {
	now := unixMillisNow()
	if err := s.exec(
		"UPDATE `projects`\n"+
			"SET `last_accessed` = ?,\n"+
			"    `swap_time` = NULL,\n"+
			"    `restore_time` = ?,\n"+
			"    `swap_compression` = NULL\n"+
			"WHERE `name` = ?;\n",
		now, now, projectName,
	); err != nil {
		panic(fmt.Errorf("db: restore: %w", err))
	}
}

func unixMillisNow() int64 {
	return time.Now().UnixMilli()
}

// GetSwapCompression — GetSwapCompression.
func (s *SqliteDBStore) GetSwapCompression(projectName string) (string, bool) {
	rows, err := s.db.Query(
		"SELECT `swap_compression` FROM `projects` WHERE `name` = ?", projectName)
	if err != nil {
		panic(fmt.Errorf("db: getSwapCompression: %w", err))
	}
	defer rows.Close()
	comp := sql.NullString{}
	for rows.Next() {
		if err := rows.Scan(&comp); err != nil {
			panic(fmt.Errorf("db: getSwapCompression scan: %w", err))
		}
	}
	return comp.String, comp.Valid
}

// GetNumUnswappedProjects — GetNumUnswappedProjects.
func (s *SqliteDBStore) GetNumUnswappedProjects() int {
	var n int
	if err := s.db.QueryRow(
		"SELECT COUNT(*)\n    FROM `projects`\n    WHERE `last_accessed` IS NOT NULL",
	).Scan(&n); err != nil {
		panic(fmt.Errorf("db: getNumUnswappedProjects: %w", err))
	}
	return n
}

// GetProjectState — GetProjectState (no row => NOT_PRESENT, NULL
// last_accessed => SWAPPED, else PRESENT). go-sqlite3 converts DATETIME
// columns (INTEGER epoch-s or TEXT) to time.Time; NULL is invalid, so a
// single NullTime scan mirrors Java's getTimestamp null-check.
func (s *SqliteDBStore) GetProjectState(projectName string) ProjectState {
	rows, err := s.db.Query(
		"SELECT `last_accessed`\n"+
			"    FROM `projects`\n"+
			"    WHERE `name` = ?", projectName)
	if err != nil {
		panic(fmt.Errorf("db: getProjectState: %w", err))
	}
	defer rows.Close()
	state := ProjectStateNotPresent
	for rows.Next() {
		var ts sql.NullTime
		if err := rows.Scan(&ts); err != nil {
			panic(fmt.Errorf("db: getProjectState scan: %w", err))
		}
		if ts.Valid {
			state = ProjectStatePresent
		} else {
			state = ProjectStateSwapped
		}
	}
	return state
}

// SetLastAccessedTime — SetProjectLastAccessedTime (nil => SQL NULL).
func (s *SqliteDBStore) SetLastAccessedTime(projectName string, t *int64) {
	var v any
	if t != nil {
		v = *t
	}
	if err := s.exec(
		"UPDATE `projects`\nSET `last_accessed` = ?\nWHERE `name` = ?",
		v, projectName,
	); err != nil {
		panic(fmt.Errorf("db: setLastAccessedTime: %w", err))
	}
}

// DeleteProject — DeleteAllFilesInProjectSQLUpdate + DeleteProjectSQLUpdate
// (the FK cascade makes the first delete redundant but it runs, as in Java).
func (s *SqliteDBStore) DeleteProject(projectName string) {
	if err := s.exec(
		"DELETE FROM `url_index_store` WHERE `project_name` = ?", projectName); err != nil {
		panic(fmt.Errorf("db: deleteProject: %w", err))
	}
	if err := s.exec("DELETE FROM `projects` WHERE `name` = ?", projectName); err != nil {
		panic(fmt.Errorf("db: deleteProject: %w", err))
	}
}
