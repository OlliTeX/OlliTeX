# `filestore` (Go) — the project & template file store, port 3009

> 1:1 Go drop-in for **`services/filestore`** (Node, port 3009), **FS
> backend only** — which is exactly what the CE image runs (the Node service's
> S3/GCS branches are not configured in CE).

## What it does

Serves and stores the big immutable files the editor needs:

- **`/bucket/<bucket>/<file...>`** — project blobs (uploads, generated history
  files) under one of the configured buckets
- **`/history/global/hash/<sha>`** — global (cross-project) content-addressed
  blobs
- **`/history/project/<hid>/hash/<sha>`** — per-project history blobs
- **`/template/<name>/<file...>`** — template files (new-project creation)
- optional **file conversions** (`ENABLE_CONVERSIONS` + a `CONVERTER` binary)
  for e.g. `.docx`/image handling, run as a subprocess — same command line and
  working dir as the Node `FileConverter`
- `/status` + `/health_check` liveness probes

All storage is **plain files on the shared volume** — the same keys and the
same on-disk layout the Node service uses, so a project created against one
reads fine against the other.

## 1:1 mapping (from the Node module layout)

| Go file | Node original |
| --- | --- |
| `server.go` | `app.js` (route table, port/host, bucket config) |
| `filehandler.go` | `FileHandler.js` (key derivation + file read/write) |
| `projectkey.go` | the `fse` project-key format (`<hid>.<timestamp>`) |
| `persistor.go` | `PersistorManager.js` / `FSPersistor` (flat vs subdirectory keys 1:1) |
| `s3store.go` | object-persistor S3 1:1 — SeaweedFS/S3 backend behind the same `Store` interface (`BACKEND=s3`, CE env `OVERLEAF_FILESTORE_*`); keys stored verbatim (`<project>/<path>`), buckets auto-created idempotently |
| `converter.go`, `exec.go` | `FileConverter.js` (subprocess invocation, timeouts) |
| `writer.go` | the streaming write path (`StreamToBuffer` equivalent) |
| `optimiser.go` | the blob optimiser (dedupe / content-addressing) |
| `errors.go` | the service's error → status mapping |
| `*_test.go` | the locked HTTP contract |

## Interface

- **Port** 3009, `LISTEN_ADDRESS` || 127.0.0.1
- **Env** (from `cmd/filestore/main.go`, same names the Node config reads):
  `TEMPLATE_FILES_BUCKET_NAME`, `OVERLEAF_HISTORY_PROJECT_BLOBS_BUCKET`,
  `OVERLEAF_HISTORY_BLOBS_BUCKET`, `ENABLE_CONVERSIONS`, `CONVERTER`
- **FS layout** — the bucket roots default under
  `/var/lib/overleaf/data/...` (see `server.go`), uploads scratch under
  `/var/lib/overleaf/tmp/uploads`

## Build / test / run

```sh
go build ./go/services/filestore
go test  ./go/services/filestore
go run   ./cmd/filestore
```

## Status

Converted (FS backend), tests green. Cutover in the CE image awaits the owner
call.
