# `docstore` (Go) — the text-document store, port 3016

> 1:1 Go drop-in for **`services/docstore`** (Node, port 3016): the overleaf
> text-document store (the `docs` collection) including the soft-delete and
> **archive/unarchive** round-trip through the FS persistor. See `../README.md`
> for the doctrine; this service is the newest conversion and the one with the
> widest surface (21 routes).

## What it does

Stores every project's text documents (line arrays + a `ranges` tree holding
comments and tracked changes) under shared `docs` collection. It also owns:

- **soft-delete** (`PATCH` sets `deleted`/`deletedAt`/`name`);
- **archive** — move a doc's `lines`/`ranges`/`rev` out of Mongo into an
  object bucket (FS persistor 1:1: flat `<bucket>/<projectId>_<docId>` file,
  temp-file rename, source-md5 verification) with an `archivingUntil`
  concurrency lock in Mongo, and **unarchive** — the reverse, with a
  rev-guarded restore;
- **read-through unarchive** — reading an archived doc transparently
  re-materialises it into Mongo (Node `_getDoc` recursion, 1:1);
- **range views** — comment-thread ids, tracked-change user ids, has-ranges;
- **health check** — the exact Node `HealthChecker` round-trip (POST a smoke
  doc, GET it back, always clean up; throws/500s when
  `HEALTH_CHECK_PROJECT_ID` is unset, just like Node's
  `new ObjectId(undefined)`).

## 1:1 mapping

| Go file | Node original |
| --- | --- |
| `routes.go` | `app.js` routes + `app/js/HttpController.js` handlers + `DocManager.js` flows |
| `handlers.go` / `responses.go` | `handleValidationError`, `expressSend(SendStatus/Text)`, Express fallback 404 page |
| `validate.go` | `app/js/...` + `libraries/validation-tools` (locked issue strings, 404-if-params-else-400) |
| `tree.go` | `RangeManager.js` (jsonRangesToMongo / fixCommentIds) + the neutral-tree normaliser for driver shapes |
| `mongo.go` | `MongoManager.js` (same `$literal` pipelines, `archivingUntil` lock, read prefs) |
| `archive.go` | `PersistorManager.js` + `app/js/FSPersistor` 1:1 (fs backend) |
| `s3archiver.go` | `DocArchiveManager.js` + S3Persistor 1:1 — SeaweedFS/S3 archive backend; slash keys `projectId/docId` (the exact Node S3 shape), prefix sweep delete (`BACKEND=s3`) |
| `healthcheck.go` | `app/js/HealthChecker.js` |
| `memstore.go` / `docstore_test.go` | in-memory Store for tests + the locked HTTP contract (incl. the driver-shape normalisation test) |

Validation and error surface are locked: `params` schema failure → **404**,
other → **400**, both `{"error": "Validation error: …", "statusCode": N}`;
`NotFoundError` → 404 "Not Found"; doc-modified/version-decremented → 409
"Conflict"; anything else → 500 "Oops, something went wrong" (text/html);
unknown path → Express fallback 404 with CSP + nosniff; body-limit overflow →
the Node service's exact overflow response (12 MB `updateDoc`, 100 kb
`patchDoc`).

## Interface

- **Port** 3016, `LISTEN_ADDRESS` || 127.0.0.1
- **Mongo** `sharelatex` via `go/mongoh`; the `docs` collection
- **Bucket** `BUCKET_NAME` || `AWS_BUCKET` || `bucket` (FS persistor)
- **Env** (from `services/docstore/config/settings.defaults.cjs`, see
  `cmd/docstore/main.go`): `MONGO_CONNECTION_STRING`/`MONGO_HOST`,
  `MONGO_HAS_SECONDARIES`, `BACKEND`, `BUCKET_NAME`/`AWS_BUCKET`,
  `ARCHIVE_ON_SOFT_DELETE`, `KEEP_SOFT_DELETED_DOCS_ARCHIVED`,
  `HEALTH_CHECK_PROJECT_ID`, `MAX_DELETED_DOCS`, `MAX_DOC_LENGTH`,
  `MAX_JSON_REQUEST_SIZE`, `UN_ARCHIVE_BATCH_SIZE`, `PARALLEL_ARCHIVE_JOBS`,
  `ARCHIVING_LOCK_DURATION_MS`

## Build / test / run

```sh
go build ./go/services/docstore
go test  ./go/services/docstore ./go/services/docstore -race
go run   ./cmd/docstore
```

Live-verified against the real mongod (scratch project, 30/30 CRUD + archive +
peek + destroy checks passed, scratch data cleaned).

## Status

Converted + live-verified; **cutover in the CE image awaits the owner call**.
Note: `HEALTH_CHECK_PROJECT_ID` is unset in the CE image, so `/health_check`
500s there — the Go port preserves the Node behaviour exactly; set the env
value to flip it green.
