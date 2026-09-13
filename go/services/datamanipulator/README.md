# `datamanipulator` (Go) — the file-ops / sync engine helpers (token-protected)

> 1:1 Go drop-in for **`services/datamanipulator`** (Node). The internal
> service the sync bridges (Dropbox / GitHub / WebDAV) and the web app call to
> move data in and out of a project's files, authenticated with the shared
> service token.

## What it does

The engine behind "sync to/from an external source":

| Route | Purpose |
| --- | --- |
| `/files` | read the project's current file list |
| `/file` | write/replace a single file (with atomicity + conflict checks) |
| `/tree` | read the project tree |
| `/compare` | diff a remote file-set against the project (what-changed / conflicts) |
| `/sync/full` | apply a full remote→local sync (create/update/delete) |
| `/pull` / `/push` | the two directions the bridges drive |
| `/health` | liveness |

It works against the project files root on the shared volume
(`DATAMANIPULATOR_PROJECTS_ROOT`) and the `filestore` blobs underneath; the
tree-compare and conflict semantics (`treecompare.go`, `sync.go`) are the part
the three external bridges depend on, so they are reproduced 1:1.

## 1:1 mapping (mirrors the Node module layout)

| Go file | Node original |
| --- | --- |
| `server.go` | route table + token gate |
| `fileops.go` | the file read/write ops (`fileOperations.mjs`) |
| `fileutils.go` | path/size helpers (`fileUtils.mjs`) |
| `treecompare.go` | remote-vs-project diff (`treeCompare.mjs`) |
| `sync.go` | the sync application logic (`sync.mjs`) |
| `errors.go` | error → status mapping (`errors.mjs`) |
| `*_test.go` | the locked contract |

## Interface

- **Port** — `DATAMANIPULATOR_PORT` (no default; set by the CE compose)
- **Env** — `DATAMANIPULATOR_PORT`, `DATAMANIPULATOR_PROJECTS_ROOT`,
  `SHARED_SERVICE_TOKEN`
- **FS** — the project-files volume + `filestore` blobs (same paths as the
  Node service)

## Build / test / run

```sh
go build ./go/services/datamanipulator
go test  ./go/services/datamanipulator
go run   ./cmd/datamanipulator
```

## Status

Converted, tests green. Cutover in the CE image awaits the owner call.
