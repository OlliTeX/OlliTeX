# configstore — SQLite-backed configuration store (P7-post)

The key/value store for the **SQLite config DB** (P7‑post backlog, item 1:
*"SQLite config DB: env params → SQLite, /hub admin + CLI backup for `go run`"*).

This is the **first slice** of that item: the store itself (CRUD + dump/restore
+ persistence). It layers over the env‑based Go config
(`go/services/web/core/config.go`), so a value stored here overrides the
env/default for the same key. The **/hub admin endpoints** (GET/PUT the managed
keys) and the **`go run ./go/cmd/configdb backup|restore` CLI** are later slices
that use this store.

## Why SQLite
- The `mattn/go-sqlite3` driver is already in `go.mod` and proven in this build
  by `go/services/gitbridge/db` (same driver, same cgo toolchain).
- Single file, no server, trivial to back up (`Dump`) / restore
  (`Restore`) — which is exactly the owner's "CLI backup" requirement.

## API (surface used by the later slices)
| Method | Purpose |
| --- | --- |
| `New(dbFile)` | open/create the DB (parents 0o755), create the `config` table, WAL, single connection |
| `Get(key) (string, error)` | value, or `ErrMissing` |
| `Has(key) bool` | presence check |
| `Set(key, value, source)` | upsert; `source` records provenance (e.g. `hub`, `restore`) |
| `Delete(key)` | remove (idempotent) |
| `Keys() ([]string, error)` | sorted keys |
| `All() (map[string]string, error)` | full map (non‑nil when empty) |
| `Close()` | close the connection |
| `Dump(dest)` | JSON backup to `dest` (backs the `backup` CLI) |
| `Restore(src)` | load a JSON backup into the store (backs the `restore` CLI) |

## Schema
```sql
CREATE TABLE IF NOT EXISTS config (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL   -- epoch millis
);
```

## Build / test
```
go build ./go/libraries/configstore/
go vet   ./go/libraries/configstore/
go test  ./go/libraries/configstore/
```

## Later slices (P7-post item 1)
1. **Curated key registry** — which config keys are admin‑editable at runtime
   (the site/feature‑toggle subset of the `Config` struct; infra keys like
   `MONGO_URI`/ports stay env‑only). Sensitive keys (API keys, passwords) are
   excluded or masked.
2. **/hub admin endpoints** — `GET /api/hub/config`, `PUT /api/hub/config`
   (site‑admin + CSRF, mirroring the existing `/api/hub-theme` pattern).
3. **`go run ./go/cmd/configdb` CLI** — `backup [dest]`, `restore <src>`,
   `export`, `list` — using `Dump`/`Restore`.
