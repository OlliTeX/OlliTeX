# configdb — CLI for the SQLite config DB (P7-post item 1)

The `go run` operator CLI for the overleaf **SQLite config DB** (remember.md:
*"a CLI tool as backup"*). It reads/writes the same `go/libraries/configstore`
that the /hub admin endpoints use, so you can manage the runtime config without
touching the web service.

## Build / run
```
make go-build                    # builds ./bin/configdb (added to the go-build target)
go run ./cmd/configdb help       # run without building a bin
```

## Commands
| Command | Effect |
| --- | --- |
| `configdb list` | list configured keys |
| `configdb get KEY` | print the value of `KEY` |
| `configdb set KEY VALUE [SOURCE]` | upsert `KEY` (`SOURCE` default `cli:configdb`) |
| `configdb delete KEY` | remove `KEY` (idempotent) |
| `configdb export` | print the whole store as JSON |
| `configdb backup [DEST]` | write a JSON backup (default `./configdb-backup-<ts>.json`) |
| `configdb restore SRC` | load a JSON backup into the store |
| `configdb help` | this help |

## DB path
Resolved in order: `$CONFIG_DB_PATH` → `$OVERLEAF_HOME/configdb/configdb.sqlite3`
→ `./configdb/configdb.sqlite3`.

## Examples
```
go run ./cmd/configdb set SiteTitle "My OlliTeX"
go run ./cmd/configdb get SiteTitle
go run ./cmd/configdb backup /var/backups/ol-config.json
go run ./cmd/configdb restore /var/backups/ol-config.json
```

## Exit status
`0` on success; `1` on any error (a one-line `configdb: <reason>` on stderr).

## Tests
```
go test ./cmd/configdb/
```
