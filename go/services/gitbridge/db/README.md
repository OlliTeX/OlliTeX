# `gitbridge/db`

The DB store — ports `bridge/db`: the `DBStore` API, `ProjectState`, the no-op
implementation, and the **SQLite** implementation (`SqliteDBStore`) with the
query/update SQL taken verbatim from the Java classes. File-based (a per-bridge
SQLite file), not Mongo.

Timestamp compatibility note (verified against `org.xerial sqlite-jdbc
3.41.2.2`): `INSERT ... DATETIME('now')` stores TEXT, while `setTimestamp` /
`UpdateSwap` store INTEGER epoch millis — both encodings coexist in
`last_accessed`; the state/ordering queries only compare NULL vs non-NULL or
run engine-computed `MIN(last_accessed)`, identical for both drivers.

Part of the Go Git Bridge port (see `../README.md`); unit-tested
(`db_test.go`, `db_coverage_test.go`).
