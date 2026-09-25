# HANDOFF — history-v1 Go port (as of 2026-09-25)

> **STATUS (2026-09-21): FULL GATE GREEN** — all 9 test packages pass
> (`go build` / `go vet` / `gofmt -l` / `go test -count=1 -race` clean).
> The API layer is **complete minus the zip/clone/blob-streaming surface**:
> initializeProject now ported (POST /api/projects → 200 {projectId},
> 409 on re-init) alongside the 20 store-backed handlers (`service/api/
> handlers.go`), per-route manual dispatch with Express-404 parity
> (`server.go`), per-route auth, and HTTP integration tests. Entry point
> `cmd/history-v1/main.go` verified by live smoke test. Upstream
> `origin/main` merged at `8ee34b3` (otc Phase C slices 2-6: History/Chunk/
> ChunkResponse + schemas + root_doc + minimatch/go-diff) — the deferred
> engine type-swap's "wait for upstream History/Chunk" blocker is now
> cleared (see "Deferred: engine type swap"). **Acceptance-test parity run
> started** (mirrored hermetically over `(*api).ServeHTTP`; see "Acceptance
> mirror — mirrored"): 422 Zod-tier parity fixes (return_snapshot,
> end_version, since, XOR/trackChanges), and the portable it() cases:
> auth (9), auth docs, import, setContent reject + 413 + 404 +
> create/edit + origin/v2Authors, getLatestZip 404-unknown, git-bridge
> origin import.

## Live home
`/home/davrot/history_v1/OlliTeX_hist/services/history-v1.go/`
(canonical path; also at `/tmp/live_dir.txt` when live). Do **not** type the
path by hand — `cat /tmp/live_dir.txt`.

Repo `/home/davrot/history_v1/OlliTeX_hist`, branch `go_history_v1`,
HEAD `8ee34b3` (merge origin/main: upstream Phase C slices 2-6);
preceding: `c824955` (initializeProject ported), `15aa75f` (HANDOFF),
`8f3bb04` (cmd/history-v1 main), `c6b668c` (HANDOFF), `c55c91c` (empty-changes
500), `394af81` (integration tests), `0132da6` (handlers + dispatch auth),
`24a1af4` (NewChange authors), `5f652aa` (HANDOFF). Module `history-v1`,
`go 1.27.0`. Deps: `require ollitex v0.0.0` + `replace ollitex => ../../`
(repo root is `module ollitex`) + `github.com/sergi/go-diff v1.4.0 //
indirect` (transitive via upstream otc DMP port) with a `go.sum`. Keep it
that way.

Node oracle at: `/home/davrot/history_v1/OlliTeX_hist/services/history-v1/`
OEC oracle/lib: `/home/davrot/history_v1/OlliTeX_hist/libraries/overleaf-editor-core/lib/`

## Current build gate (verified 2026-09-25)
- `gofmt -l .` — clean.
- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go test -count=1 ./...` — **all 9 test packages `ok`**
  (internal/{assert,config,contenthash,core,projectkey},
  service/{api,blobstore,chunkstore,persist}; historystore/otcbridge have no
  tests).
- `go test -count=1 -race ./...` — all ok.

## Package layout (Go side)
- `internal/core/` — engine: File/FileData kinds (string, lazy, hash,
  binary), Operations (addFile/removeFile/moveFile/editFile/setFileMetadata/
  noOp), Snapshot, Change, History, Chunk, toRaw/FromRaw wire, Load
  (eager/lazy/hollow/raw), comments/trackedChanges, buildSetContentOperations,
  diffAsTextOperation (minimal UTF-16 prefix/suffix diff), UTF-16 helpers,
  errors, ScanOp/TextOp/EditOp, origin, v2dv.
- `internal/` — assert, config, contenthash (git-blob hash), projectkey.
- `service/blobstore/` — hermetic in-memory `Store` (Node `new
  BlobStore(projectId)` per project).
- `service/chunkstore/` — `Store` over `Historystore` + `ProjectBlobStore`
  (initialize, create, update, load at version/timestamp, changes-since,
  clone, delete).
- `service/historystore/` — `HistoryStore` + `FakePersister`.
- `service/persist/` — `Service{cs,bs}`: `persist.go` (Limits,
  PersistChanges, fillChunk, validateContentHash), `commit.go` (CommitChanges
  level-0 dispatch + NotPortedError for levels 1-4 — Redis-buffer dependent),
  `setcontent.go` (BuildSetContentChange).
- `service/api/` — HTTP layer, **complete** (see "Done" + "Remaining scope").
- `cmd/history-v1/` — binary entry (written 2026-09-25): `config.FromEnv()`,
  hermetic in-memory stores, `api.New`, 12MB JSON soft-cap, 300s read/idle
  timeouts, port 3100 (env `PORT`).

## Done (verified by gate)
Storage + engine layers are **complete and gated**:
- Engine: raw/wired kinds, snapshot/change/chunk/history, toRaw/FromRaw wire,
  eager/lazy/hollow/raw loads, FileMap, comments, tracking, origin, v2dv.
- `buildSetContentOperations` + `BuildSetContentChange` + persist pipeline
  (PersistChanges level-0 CommitChanges), content-hash validation,
  `ConflictingEndVersion` error.
- **Binary blob fidelity** (fixed 3 failing setcontent test cases).
- **OT transform (rebase)** — DONE, all oracle parity tests pass:
  - `internal/core/textop_transform.go` + `textop_transform_test.go` (3 tests).
  - `internal/core/editop_transform.go` + `editop_transform_test.go` (1 test).
  - `internal/core/operation_transform.go` + `operation_transform_test.go`
    (37 tests — full Node transform golden matrix).
  - `internal/core/rebase.go` + `rebase_test.go` (10 tests).
  - `service/persist/rebase_persist_test.go` (1 test — setContent rebase
    acceptance mirror).
- Test inventory: `service/persist` 5 + 16 + 1 = 22 funcs;
  `internal/core` 2 + 3 + 9 + 4 + 3 + 2 + 6 + 37 + 10 + 3 + 16 (rebase) —
  all PASS.
- **API layer (2026-09-25) — the last scope of the port, DONE:**
  - `service/api/server.go` — manual method+path dispatch (Express 404
    parity: unmatched method on known path → 404 NOT 405; unknown path →
    terminal 404 body `{message:"Not Found", error:{}}`).
  - `service/api/handlers.go` — 20 handlers: initializeProject, deleteProject,
    cloneProject (501), getChanges, importChanges, importSnapshot, setContent,
    flush, expire, getBlobStats, getLatestContent, getLatestHashedContent,
    getLatestHistory, getLatestPersistedHistory, getLatestHistoryRaw,
    getLatestZip (501), getHistory, getVersionContent, getZip (501),
    createZip (501), getHistoryBefore. Plus project-blob GET/HEAD/PUT (get/​head/
    put/copy).
  - Per-route dispatch/auth parity per Node routes:
    `latest/content` jwt, `latest/hashed_content` **basic** (was wrongly jwt),
    `latest/history|persistedHistory` jwt, `latest/zip` **token**,
    `versions/:v/{history,content}` jwt, `version/:v/zip` GET **token** + POST
    basic, `timestamp/:ts/history` jwt.
  - `handlers_test.go` — end-to-end over httptest: import/legacy_changes,
    latest content/hashed/history/raw, versions, changes (0/50/-1),
    setContent (ok/no-timestamp/xor/unknown-hash/no-cred), lifecycle
    (import/import-dup/flush/expire/clone/blob-stats/delete), Express 404
    catch-all.
  - Empty-changes import → 500 (Node `TypeError` on `result.resyncNeeded` —
    Go mirrors with an explicit `result == nil` guard, since Go persists nil
    for empty input rather than crashing).
  - Zip endpoints (getLatestZip/getZip/createZip) and cloneProject remain
    **501 NotPorted** (Node streaming S3/zip + IncrementalResponse not
    portable hermetically; document deviation).
- **go/libraries reuse seam (commit `cd6ee50`)** — the service no longer
  hand-maintains the pieces upstream `otc` has already ported:
  - `internal/contenthash` delegates to `otc.EmptyHash`,
    `otc.HexHashRxString`, `otc.BlobHashFromString`, `otc.BlobHashFromBuffer`
    (git-blob sha1, 40-hex validation, empty-hash constant). `ContentHash`
    (plain sha1) stays local — upstream does not port it.
  - `internal/core/safe_pathname` is now a facade over
    `otc.Clean`/`otc.IsClean`/`otc.IsCleanDebug`.
  - `service/otcbridge` pins the `otc` symbol surface via the module
    `require`/`replace` in `go.mod`.
  - **Upstream `origin/main@1827c57` merged at `8ee34b3`** (otc Phase B3/B4
    + Phase C slices 1-6): upstream now ports the `Operation` family
    (`NoOperation`/`AddFileOperation`/`MoveFileOperation`/`EditFileOperation`/
    `SetFileMetadataOperation`), `EditOperation` family + BlobStore seam,
    `File`/`FileData`/`FileMap` backbone, `Change`+`Snapshot`+`Origin`
    (+Restore variants)+`V2DocVersions` (Phase C1, `map[string]any`-based
    wire), **`History`/`Chunk`/`ChunkResponse` + `ChangeNote`/`ChangeRequest`/
    `ot_migration_stages`** (Phase C5), `root_doc` (C3), change identity +
    doc-updater ranges + file-tree diff (C2), DMP `diff_as_text_operation` +
    `build_set_content_operations` via go-diff (C4), `schemas.js` onto
    validtools (C6). Plus `go/minimatch` (module ollitex) and the
    `sergi/go-diff` dep (why this module now carries go-diff //indirect).
  Still local (wire format is `json.RawMessage`-based while upstream otc is
    `map[string]any`-based): `History`/`Chunk` seam-swap onto upstream,
    plain sha1 `ContentHash`, and the in-package `utf16Length` (import cycle;
    upstream exposes no exported UTF-16 length helper). Future seam swap for
    the engine (`Operation`/`File`/`Change`/`Snapshot`/`Origin`/`RebaseChanges`
    + now-upstream `History`/`Chunk`) is possible
    but requires aligning my wire layer (`json.RawMessage` vs upstream
    `map[string]any`) — a larger change than the leaf seams.

## Acceptance-t test parity run (started 2026-09-21, this session)

The Node acceptance harness (`test/acceptance/js/api/`) boots a live server
against Mongo/Postgres/Redis/GCS/minio and cannot be re-targeted at the
hermetic Go binary, so the portable contracts (status codes, wire shape,
422 Zod-tier semantics, origin/v2Authors echo) are mirrored hermetically in
`service/api/handlers_test.go` over `(*api).ServeHTTP`. The Node
empty-blob `File.EMPTY_FILE_HASH` PUT step is omitted where it would be
needed (Go has no blob PUT endpoint — 501); empty-hash import is portable
because Go lazily resolves blob references.

Mirrored + gate-pinned:
- **auth.test.js** (9 it) — `TestMirrorAuth`: docs no-cred/wrong-cred 401
  (WWW-Authenticate `Basic realm="Application"`), docs OK, import 401
  (Basic shape), JWT project-mismatch 403, no-JWT-for-import 401, uses-JWT,
  basic-in-place-of-JWT, old/new-key accept/reject matrix (signWith helper
  in `dispatch_test.go`).
- **set_content.test.js** — reject matrix (no-timestamp, XOR both, neither,
  trackChanges-without-userId, unknown blobHash + 413 ContentTooLarge + 404
  uninitialized) + `TestMirrorSetContentCreateEdit` (create -> POST
  /changes 201, edit -> textOperation, origin {kind:'test-source'} +
  v2Authors ['abcdef...01'] echo).
- **project_import.test.js** (3 it reduc to one contract) +
  **project_updates.test.js** git-bridge origin — `TestMirrorProjectImport`
  (empty snapshot import -> 200, add-file -> 201 {resyncNeeded:false}) and
  `TestMirrorGitBridgeOrigin` (origin {kind:'git-bridge'} preserved +
  echoed via GET latest/history).
- **422 Zod-tier parity** — missing end_version, invalid return_snapshot,
  invalid since, XOR/neither/trackChanges reject — all in
  `TestImportLegacyChanges` / `TestSetContent` / `TestGetChanges`.
- **projects.test.js** — getLatestZip unknown-project 404 (LoadLatest
  NotFound before NotPorted), flush 200 (uninit heuristic), delete 204,
  blob-stats 422 (bad hash).

NOT ported (501 bucket — document deviation):
- **project_blobs.test.js** (16 it) — blob PUT/GET/HEAD/copy backend (all
  501).
- **project_flush.test.js** happy end-to-end (Redis persistBuffer; Go
  persistChanges level-0 is a no-op and importChanges guards the nil
  result as 500 — the flushed `persistedVersion == 1` phase is
  unreachable hermetically).
- **project_expiry.test.js** — depends on Redis expireProject (Go: 200
  no-op, state not observable).
- **project_hashed_content.test.js** happy — requires a populated blob
  store (PUT); only the 401/404 tiers are portable.
- **project_updates.test.js** v2-ids, binary-op, reject-unchanged, batch
  (blob backend + Node fixture).
- **end_to_end.test.js** — composite create->edit->zip (zip 501).
- **set_content.test.js** rebased-concurrent, trackedChanges happy,
  binary-from-blob (blob backend).
- **rollout.test.js** (7 it) — config-level rollout gates (not an API
  surface; Node `getPersistLimits` + `@overleaf/features` flags).
- **zip + clone** (getLatestZip/createZip/cloneProject) — 501 (Node
  streaming NDJSON/S3; document deviation).

## Remaining scope (next session)
1. (optional) Port the 501s: `getLatestZip`/`getZip` (Node
   `project_archive.js` streamZip — `archive/zip` content parity) and
   `cloneProject` (Node IncrementalResponse NDJSON). No acceptance test
   covers zip/clone, so 501 is a documented deviation until then.

### Files to add/rewrite (the 8 steps — executed 2026-09-25)
all 8 done (6 = `cmd/history-v1/main.go`) and pinned by the gate.

### Already-done scaffolding in `service/api/` (keep)
- `render.go` — `writeJSON` (SetEscapeHTML false = Node res.json no-escape),
  `renderBadRequest/NotFound/UnprocessableEntity/Conflict/RequestEntityTooLarge`
  (body `{message: <HTTPStatus name>}`), `handleAPIError` (terminal
  `{message, error:{}}`), `statusError`/`authStatusError` carriers,
  `authErrToStatus`.
- `security.go` — `authorize(mode, r, projectID) *AuthError`,
  `hasValidBasicAuthCredentials` (tsscmp constant-time), `verifyHS256`
  (pure-Go JWT, no deps). Modes: basic / jwt / token / either.
- `util.go` — error classifiers (`isChunkNotFound` covers all four chunk
  NotFound types; `isNotPersisted`, `isVersionOutOfBounds`, `isUnprocessable`,
  `isConflict`, `isTooLarge`), `validationErr` (renders `{error, statusCode}`:
  `"Validation failed for params: [N]"`→404 | `"...query: [N]"`→422),
  `projectIDValid/hashValid/copyFromValid/intParse/tsParse`, `projectIDRX`
  (mongo-24-hex | postgres `[1-9][0-9]{0,9}`), `hexHashRX` (40 hex),
  `farFutureLimits()`.

### Route table (VERIFIED from api/routes/{projects,project_import}.js + app.js)
Manual dispatch is required for Express parity.

Top level (`app.js`, outside `/api`):
| route | behavior |
|---|---|
| `GET /` | `res.send('')` → 200, empty body |
| `GET /status` | `res.send('history-v1 is up')` (Node pings Mongo first; hermetic = always up) |
| `GET /health_check` | `res.send('OK')` → 200, body `OK` (**underscore**, NOT `/healthcheck`) |
| `GET /docs` | basic-auth guard `WWW-Authenticate: Basic realm="Application"`, 401 empty on fail; success `res.send('OK')` → body `OK` (**NOT** HTML) |

Under `/api` (all `:project_id` = `projectIDRX`, 404 `validationErr` on bad shape):
| route | method | auth | handler |
|---|---|---|---|
| `/projects` | POST | basic | initializeProject |
| `/projects/:id/clone` | POST | basic | cloneProject |
| `/projects/blob-stats` | POST | basic | getProjectBlobsStats (**literal** `blob-stats` segment) |
| `/projects/:id/blob-stats` | POST | basic | getBlobStats |
| `/projects/:id` | DELETE | basic | deleteProject |
| `/projects/:id/blobs/:hash` | GET | jwt-or-token | getProjectBlob |
| ` (same path) ` | HEAD | (auto HEAD) | headProjectBlob (200 Content-Length / 404 empty) |
| ` (same path) ` | PUT | jwt | createProjectBlob |
| ` (same path) ` | POST | jwt | copyProjectBlob |
| `/projects/:id/latest/content` | GET | jwt | getLatestContent |
| `/projects/:id/latest/hashed_content` | GET | basic | getLatestHashedContent |
| `/projects/:id/latest/history` | GET | jwt | getLatestHistory |
| `/projects/:id/latest/history/raw` | GET | jwt | getLatestHistoryRaw |
| `/projects/:id/latest/persistedHistory` | GET | jwt | getLatestHistory (SAME handler) |
| `/projects/:id/versions/:v/history` | GET | jwt | getHistory |
| `/projects/:id/versions/:v/content` | GET | jwt | getVersionContent |
| `/projects/:id/timestamp/:ts/history` | GET | jwt | getHistoryBefore |
| `/projects/:id/latest/zip` | GET | token | getLatestZip |
| `/projects/:id/versions/:v/zip` | GET | token | getZip |
| ` (same path) ` | POST | basic | createZip → 501 Not Ported |
| `/projects/:id/changes` | GET | basic | getChanges |
| `/projects/:id/changes` | POST | basic | importChanges (**GET+POST same path, diff handlers**) |
| `/projects/:id/import` | POST | basic | importSnapshot |
| `/projects/:id/legacy_import` | POST | basic | importSnapshot (alias) |
| `/projects/:id/legacy_changes` | POST | basic | importChanges (alias) |
| `/projects/:id/set_content` | POST | basic | setContent |
| `/projects/:id/flush` | POST | basic | flushChanges |
| `/projects/:id/expire` | POST | basic | expireProject |
| `/projects/:id/zip` (no version prefix) | — | — | does **NOT** exist |

Gotchas:
- `POST /api/projects` (no id) = initialize. `GET /api/projects` = terminal 404 (no such route).
- `GET` and `POST` on `/projects/:id/changes` are DIFFERENT endpoints.
- `persistedHistory` ≡ `latest/history` handler.
- Auth runs BEFORE param validation (Express middleware order); Go: `authorize(...)` then
  `projectIDValid()` etc.
- `loadLatest`/`loadAtVersion`/`getChangesSinceVersion` all go through `chunkStore`; the
  hermetic `Store` has no Redis buffer so level-0 semantics apply everywhere.

### Exact response contracts (verified against Node oracle 2026-09-22)
Success/error bodies per handler:
- **initializeProject** (ported 2026-09-21, `c824955`): `200 {"projectId": "<id>"}`. Body may omit projectId →
  generate (hermetic: incrementing numeric string, e.g. `"1000001"`,
  `"1000002"`; tests only check `assert.projectId` shape). `AlreadyInitialized`
  → 409 `render.conflict` (body `{message:"Conflict"}`).
- **importSnapshot** (`/import`, `/legacy_import`): parse body as Snapshot
  (`core.SnapshotMustFromRaw`); fail → 422 `render.unprocessableEntity`;
  `Initialize` AlreadyInitialized → 409; else `200 {"projectId": ...}`
  (returned historyId = the projectId given).
- **importChanges** (`/changes`, `/legacy_changes` POST): query `end_version`
  (z.coerce.number — required!), `return_snapshot` default `'none'`
  (enum `['hashed','none']`). body = JSON **array** of raw changes. Parse each
  with `ChangeMustFromRaw`; any fail → 422. Level-0 →
  `persist.PersistChanges(pid, changes, farFutureLimits(), endVersion)`; catch:
  ConflictingEndVersion/Unprocessable/NotEditable/Pathname/EditMissing/
  ChunkVersionConflict/InvalidChange → 422 `render.unprocessableEntity`
  (message "Unprocessable Entity"); Chunk.NotFoundError → 404
  `render.notFound`. Response: `none` → `201 {"resyncNeeded": <bool>}`;
  `hashed` → `201` + raw snapshot JSON (use `PersistResult.CurrentChunk`;
  hermetic level-0 is ALWAYS the persisted chunk).
- **setContent**: body `{pathname, source?, userId?, timestamp (ISO),
  metadata?, content? XOR blobHash?, trackChanges?}` exactly-one-of enforced
  (persist.SetContentXorError → 422). timestamp missing → 422 explicitly.
  Build via `persist.BuildSetContentChange`
  (origin: `&core.Origin{Kind: source}` — the Go `Origin` struct has no
  `source` field; `Origin{Kind: ...}` is close enough hermetically, `source`
  string maps to the Go `Kind`). Errors: ContentTooLarge → 413
  `render.requestEntityTooLarge`; NotFoundError → 404 `render.notFound`;
  BlobNotFound/Unprocessable/NotEditable/Pathname/EditMissing/InvalidChange/
  trackedChanges-out-of-sync → 422. Success: `200 {"baseVersion": N,
  "change": raw-or-null}` (Node `change ? change.toRaw() : null` — 200 even
  when change is null).
- **getLatestContent** (`/latest/content`, JWT): `loadLatest` +
  `applyAll(chunk.getChanges())` + `loadFiles('eager', pbs)`, `200
  snapshot.ToRaw()`. **NO NotFound catch** → uncaught NotFound → terminal
  `500 {"message":"no chunks for project X","error":{}}`. Mirror: Go handler
  must NOT catch isChunkNotFound here (let it reach `handleAPIError` → no
  statusCode → 500).
- **getVersionContent** (`/versions/:v/content`, JWT):
  `getSnapshotAtVersion(pid, v)` (loadAtVersion + dropRight changes + prior-
  chunk timestamp fallback, errors swallowed for first-chunk case),
  `loadFiles('eager')`, `200 snapshot.ToRaw()`. **NO NotFound catch** → 500
  terminal on missing project/version.
- **getLatestHashedContent** (`/latest/hashed_content`, basic): same shape as
  getLatestContent but with `HashCheckBlobStore` → **hermetic: plain pbs**
  (no observable diff in-memory). `snapshot.Store(pbs)` raw = same shape as
  ToRaw. NO catch → 500 terminal on missing.
- **getLatestHistory / persistedHistory** (JWT): `{ "chunk": <chunk.ToRaw()> }`
  200, or 404 `render.notFound` (body `{message:"Not Found"}` — message present,
  NO `error` key).
- **getLatestHistoryRaw** (JWT): `200
  {"startVersion": N, "endVersion": N, "endTimestamp": "<ISO>"}` via
  `GetLatestChunkMetadata`; 404 `render.notFound` on NotFound. endTimestamp =
  Go `t.UTC().Format("2006-01-02T15:04:05.000Z")`
  (mirrors Node `Date().toISOString()`).
- **getHistory** (`/versions/:v/history`, JWT): `LoadAtVersion(pid, v,
  preferNewer=false)`, `{chunk: ...}`, 404 catch.
- **getHistoryBefore** (`/timestamp/:ts/history`, JWT): `LoadAtTimestamp`,
  `{chunk: ...}`, 404 catch.
- **getChanges** (`/changes` GET, basic): query `since` optional (default 0,
  z coerce int). since<0 → `400 {"error": "Version out of bounds: <since>"}`
  (exact body, NO `error:{}` key). `ChangesSince(pid, since, preferNewer=TRUE)`.
  VersionNotFoundError → same 400 body with since value. Success:
  `200 {"changes": [raw...], "hasMore": bool}`. Build body manually:
  `{"error": fmt.Sprintf("Version out of bounds: %d", since)}` (safer than
  trusting error Msg string).
- **deleteProject** (DELETE, basic): `204` empty (`cs.DeleteProjectChunks(pid)`
  + `bs.DeleteProject(pid)`; Node 204 even if project never existed).
- **getProjectBlob** (`/blobs/:hash` GET, jwt-or-token): hash param 40-hex
  (validationErr 404 on bad shape). Missing blob → `404 EMPTY`
  (`.end()` — no JSON body, NO Content-Type). Else `Content-Type:
  application/octet-stream` + body bytes. Range header
  `/^bytes=(\d{1,7})-(\d{1,7})$/`: invalid (start>end or
  start>=blobLength) → `416` headers `Content-Range: bytes */<L>`,
  `Content-Length: 0`, no body; valid → `206` +
  `Content-Range: bytes S-A/<L>` + `Content-Length: A-S+1` + partial body
  (blob slice).
- **HEAD blob**: `200` + `Content-Length` only, no body; missing → 404 empty.
- **createProjectBlob** (PUT, jwt): read body (12MB cap). Node checks
  maxFileUploadSize on stream; Go: `http.MaxBytesReader` per request +
  `len > cfg.MaxFileUploadSize` → 413 `render.requestEntityTooLarge`. Compute
  git-blob hash (`contenthash`: `sha1("blob " + len + "\0" + bytes)` = git
  addressing). Mismatch with `:hash` → 409 `render.conflict(res,'File hash
  mismatch')` → body `{message:"File hash mismatch"}` (need
  `renderConflictMsg`). Match → `PutBytes` → `201` empty.
- **copyProjectBlob** (POST, jwt, query `copyFrom` + optional `sizeLimit`
  numeric): source missing → 404 `render.notFound`; `sizeLimit>0 &&
  byteLen>sizeLimit` → `413 {"size": N}` (exact shape — `size` key, NOT
  message; Node does `.status(413).json({size: byteLength})`); target already
  has blob → `204` empty; else copy → `201` empty.
- **getBlobStats** (`/blob-stats` per-project, basic): body
  `{"blobHashes":["<40hex>",...]}` (assert each — 400 `render.badRequest` on
  invalid). Stats ONLY over those hashes (not whole project). Each hash →
  `StringBlob(h)`: found? → text (byteLength, stringLength != -1) else binary.
  `200
  {"projectId": id, "textBlobBytes": N, "binaryBlobBytes": N,
  "totalBytes": N, "nTextBlobs": N, "nBinaryBlobs": N}`. Missing blob for a
  hash silently skipped (Node `filter(Boolean)`).
- **getProjectBlobsStats** (`/blob-stats` literal segment, basic): body
  `{"projectIds":[...]}` (each projectHistoryId). Response = JSON **array** of
  stat objects using ALL blobs of each project (`Store.ProjectBlobs(pid)`),
  text/binary by `BlobInfo.IsString` flag (Go: `IsString bool`, Node:
  `stringLength != null`). `200 [...]`.
- **cloneProject** (`/clone`, basic): body `{"targetProjectId": ...}`. Node
  uses IncrementalResponse streaming (NDJSON); no acceptance test covers
  clone → simple `200` on success is acceptable hermetic (document
  deviation). `cs.Clone(src, tgt)` + `bs.Clone(src, tgt)`. Errors → terminal
  500.
- **getLatestZip** (`/latest/zip` GET, token): `loadLatest` (404 catch),
  `res.setHeader('X-History-Version', chunk.getEndVersion())` on ALL, then
  `Content-Type: application/octet-stream`, `Content-Disposition:
  attachment; filename=project.zip`, body = ZIP of eager file contents
  (Node project_archive streamZip: file path entries, binary raw, text full
  string; empty files included). Go: `archive/zip`, Deflate (or
  Store — pick Deflate; content parity, bytes differ but tests check
  contents).
- **getZip** (`/versions/:v/zip` GET, token): `getSnapshotAtVersion` semantics
  (preferNewer default FALSE for version-specified), 404 catch, same zip
  (NO `X-History-Version` header — only getLatestZip sets it).
- **createZip** (POST `/versions/:v/zip`, basic): 501 — Node S3/GCS signed URL
  not hermetic. Respond `501
  {"message":"Not ported: ...","error":{}}` (or 500 terminal). No acceptance
  test → document deviation.
- **flushChanges** (`/flush`, basic): hermetic persistBuffer = no Redis =
  no-op success → `200 EMPTY` (`res.status(200).end()`). Node catches
  Chunk.NotFoundError → 404 render, but persistBuffer only throws for missing
  project if Redis… hermetic: treat unknown project as `200` (nothing to
  persist). Acceptance test `project_flush.test.js` uses an EXISTING project
  + Redis-only queueChanges → simple hermetic `200 END` always.
- **expireProject** (`/expire`, basic): `200` empty always (Node redis expire;
  hermetic no-op).

### Terminal 404 + error handler (app.js `setupErrorHandling`)
- Catchall (unknown route): `new Error('Not Found')` err.status=404 → terminal
  handler body: `{"message": "Not Found", "error": {}}` (prod env; development
  would put err in error key — tests run prod → `{}`).
- Terminal handler for thrown errors with NO statusCode/status → 500 +
  `{"message": err.message, "error": {}}`.
- `handleAPIError(w, err)` in Go must produce exactly `{message, error:{}}`
  (already implemented in render.go — verify it emits `error: {}` key always).
- 401 auth: route-level → terminal handler renders body `{message:
  "", error: {}}` + `WWW-Authenticate` header. `/docs` special: `res.end()`
  NO body, just the `WWW-Authenticate: Basic` header. Tests only check status
  + header, NOT body.

### Hermetic decisions already made (locked)
1. HashCheckBlobStore = plain pbs (re-validation observable-only against
   external S3; in-memory store is authoritative).
2. Redis persist buffer = no-op; level 0 only.
3. Zip GET = `archive/zip` stdlib; entry = every file in snapshot (text +
   binary); Deflate (or Store — pick Deflate; content parity, bytes differ
   but tests check contents not bytes).
4. `GET /` → 200 empty body (Node `res.send('')`).
5. POST zip → 501 Not Ported terminal body.
6. generateProjectId → incrementing numeric string (Node postgres sequence),
   NOT mongo-24hex (tests accept either).
7. Timestamps in JSON bodies (endTimestamp etc.) →
   `UTC().Format("2006-01-02T15:04:05.000Z")` to match `Date.toISOString()`.

### main.go assembly (`cmd/history-v1/main.go`) — written at `cmd/history-v1/main.go`
1. `cfg := config.FromEnv()` (returns struct with `BasicHttpAuthPassword`,
   `BasicHttpAuthOldPassword`, `JWTAuthKey`, `JWTAuthOldKey`,
   `MaxFileUploadSize` (int64, 52428800), `Port` (int, default 3100),
   `HTTPRequestTimeout` (int64, 300000)).
2. `fp := historystore.NewFakePersister()`;
   `hs := historystore.New(fp, "main")`.
3. `bs := blobstore.NewStore()`; `cs := chunkstore.New(hs,
   func(p string) core.BlobStoreI { return bs.Project(p) })`.
4. `ps := persist.NewService(cs, bs)` (takes `*blobstore.Store`).
5. `api := api.New(cs, bs, cfg)` — `configView{...}` fields:
   `BasicHttpAuthPassword string`, `BasicHttpAuthOldPassword string`,
   `JWTAuthKey string`, `JWTAuthOldKey string`, `MaxFileUploadSize int64`.
6. Body limit: 12MB via `http.MaxBytesReader` middleware on POST
   `/api/projects/*` (Node express.json 12MB; overflow → 413 body
   `{"message":"request size too large","error":{}}` — terminal shape).
7. Timeouts: Node `res.setTimeout(HTTPRequestTimeout=300000)` → Go
   `http.Server{ReadTimeout, WriteTimeout, IdleTimeout: 300s}`.
8. Port: `cfg.Port` default 3100 (env `PORT`).

## Constraints / gotchas (do not regress)
- Wire/parse equivalence is **locked** to the Node oracle — golden tests.
  Do not "improve" wire shapes.
- `service/blobstore` must **not** import `internal/core` (import cycle);
  `BlobStoreI` lives in core, `blobstore.ProjectBlobStore` implements it.
- Go 1.27: `compress/gzip` has **no** `gzip.Default` constant — use
  `gzip.NewWriter`. gofmt reformats indented doc comments — run
  `gofmt -w` after doc edits.
- `BlobHash = sha1("blob len\0"+utf16)`; git-blob addressing
  (`sha1("blob %d\0"+bytes)`).
- `BlobHashForBytes` for raw byte slices (binary blobs, range requests).
- Lengths are UTF-16 code units for text; byte-length for binary.
- `core.BlobStoreI` (filedata.go): `PutString`, `PutObject`, `GetHashBlob`,
  `GetRangesBlob`, `StringBlob`. Adding methods requires updating
  `blobstore.ProjectBlobStore` **and** the core test fakes
  (`store_test.go` countedBlobStore, `filedata_load_test.go`, etc.).
- Project IDs: `^[0-9a-f]{24}$` (projectkey) OR numeric (postgres).
- Hash: 40-hex `^[0-9a-f]{40}$`.
- Timestamps wire format: `2006-01-02T15:04:05.000Z` (UTC).
- `json.RawMessage` is `[]byte` — compare with `string(val)` /
  `bytes.Equal`, never type-assert.
- MetadataEqual: nil/"null"/"" → "{}".
- XOR: exactly one of content/blobHash (setContent).
- Minimal diff (prefix/suffix UTF-16) is sufficient for all oracle diff cases
  — DMP is NOT needed.
- `configView.MaxFileUploadSize` is `int` in api package but
  `cfg.MaxFileUploadSize` is `int64` — reconcile at boundary.
- `chunkstore.New` takes `blobs func(projectID string) core.BlobStoreI`
  — adapt `bs.Project` accordingly.

## Deferred: engine type swap onto upstream `otc` (separate follow-up)
NOT part of this pass (deliberate: "API dispatch first, swap after"). The
local engine uses a `json.RawMessage`-based wire
(`Change`/`Snapshot`/`History`/`Chunk`) while upstream `otc` (merged at
`8ee34b3`, Phase B3/B4 + C1-C6) ports `Operation`/`File`/`Change`/`Snapshot`/
`Origin`/`V2DocVersions` **and `History`/`Chunk`/`ChunkResponse`** on a
`map[string]any` wire. The "wait for upstream History/Chunk" blocker is
**cleared** by this merge. Swapping the engine onto otc requires:
- aligning the wire layer (`json.RawMessage` ↔ `map[string]any`) — touches
  FromRaw/ToRaw golden tests and every API handler that serializes
  chunks/snapshots/changes;
- plain-sha1 `ContentHash` and the in-package `utf16Length` have no upstream
  export (import-cycle guard) — they stay local either way.
If picked up: one focused commit, full 9-package gate + `-race` re-run, then
a parity sweep against the acceptance mirror before exposing it through the
API surface.

## Environment notes
- No local databases; blobstore/historystore are in-memory fakes.
- The OEC lib can be probed directly with node + a stubbed
  `@overleaf/o-error` (leaf-class `class extends Error`) — useful for
  ground-truthing transform behavior.
- HANDOFF is the durable state doc: update it as the plan, not as a log.
- `HANDOFF_API.md` is the detailed backup plan (superseded by this file but
  still a good reference for the contracts).
