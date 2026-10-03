# configstore — shared configuration store (Postgres‑primary, SQLite offline fallback)

The key/value store for the OlliTeX **shared config DB** (P7‑post backlog,
item 1: *"config DB: env params → DB, /hub admin + CLI backup"*). It layers
over the env‑based Go config (`go/services/web/core/config.go`), so a value
stored here overrides the env/default for the same key. The **/hub admin
endpoints** (GET/PUT the managed keys), the **`cmd/configdb` CLI**, and
**`toolkit/bin/config`** all manage this one logical store.

## Backends (one contract, `Store`)
| Backend | When | How |
| --- | --- | --- |
| **Postgres (PRIMARY)** | A DSN is in env: `CONFIG_DB_DSN` > `DATABASE_URL` > `HISTORY_CONNECTION_STRING` | table `configdb` (key/value/source/updated_at) in the stack's central DB — the same PG plane `historyv1`'s chunk/blob stores use (pgx v5). `NewPG(dsn)` |
| **SQLite (offline emergency fallback)** | No DSN in env | file chain `$CONFIG_DB_PATH → $OVERLEAF_HOME/configdb/… → ./configdb/configdb.sqlite3` (WAL, single conn). `New(dbFile)` |

Entry points: `Dial()` (env‑based selection), `DialFile(path)`, `DialPG(dsn)`,
plus `New`/`NewPG` for the explicit forms. `Store.Describe()` names the live
backend (credentials redacted) for operator output (`configdb doctor`, etc.).

**SQLite → Postgres migration:** `configdb backup file.json` (offline SQLite)
then `configdb restore file.json` with the DSN in env — `Restore` is
backend‑agnostic by design.

## Why Postgres primary / SQLite fallback
- **Postgres** is the stack's existing central store (owner‑pinned
  `postgres:18-alpine`; `historyv1` already persists chunk/blob metadata and
  blobs there). The config DB therefore rides the **same plane and DSN** —
  one shared, backed‑up, horizontally‑reachable store — which is what the
  horizontal‑scaling requirement needs (a single source of truth from every
  web instance, not a per‑instance local file).
- **SQLite** is retained as the **offline/emergency** path (air‑gapped bootstrap,
  `go run` with no DSN, `mattn/go‑sqlite3` already proven by `gitbridge/db`).
  Its single‑file `Dump`/`Restore` is the trivial backup/restore story.

## API (surface used by the later slices)
| Method | Purpose |
| --- | --- |
| `Dial()` | env‑based selection: Postgres (DSN in env) else SQLite file chain |
| `DialFile(path)` / `DialPG(dsn)` | explicit SQLite / Postgres open |
| `New(dbFile)` | open/create the SQLite DB (parents 0o755), `config` table, WAL, single connection |
| `NewPG(dsn)` | connect the Postgres DB, ensure the `configdb` table |
| `Get(key) (string, error)` | value, or `ErrMissing` |
| `Has(key) bool` | presence check |
| `Set(key, value, source)` | upsert; `source` records provenance (e.g. `hub`, `restore`) |
| `Delete(key)` | remove (idempotent) |
| `Keys() ([]string, error)` | sorted keys |
| `All() (map[string]string, error)` | full map (non‑nil when empty) |
| `Close()` | close the connection |
| `Describe()` | backend name with credentials redacted (operator output) |
| `Dump(dest)` | JSON backup to `dest` (backs the `backup` CLI; identical across backends) |
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
