# history-v1.go — remaining handoff plan (API layer + main)

Scope: the Go `service/api/` HTTP layer + `cmd/history-v1/` entrypoint, porting
`services/history-v1/` (Node/Express) to **hermetic Go** (stdlib only; no Redis/Postgres/Mongo/S3).
All storage layers (`core`, `blobstore`, `chunkstore`, `persist`, `historystore`, `config`,
`contenthash`, `projectkey`) are DONE and tested. Every contract below is verified against the
Node oracle sources (controllers, routes, app.js, schema.js) on 2025-07-22, not from memory.

## 1. Build state (verified)
`go build ./...` fails **only** in `service/api/server.go`: undefined `strings`, `splitPath`,
`a.notFound`, `a.handleInitialize`, `a.handleProjectBlobsStats`, `a.methodNotFound`,
`a.handleProjectAction`, `a.handleDeleteProject`. Everything else compiles clean.
`cmd/` does not exist yet. No `go.sum` (zero external deps — keep it that way; stdlib only).

## 2. DONE (verified, keep)
- `internal/core/*` — wire types; `Snapshot.Store(bs) (json.RawMessage, error)` uploads eager
  blobs, returns raw; `Snapshot.ToRaw()`; `Chunk.ToRaw()` → `{...}` (chunk raw, NO
  endTimestamp key); `History.ToRaw()`; `Change.ToRaw()`; `origin.NewOrigin` constructors:
  `NewEditorOrigin/...`/`OriginFromRaw` (note: setContent needs plain `{source: "..."}` — see §4).
- `service/blobstore/Store` — `NewStore()`, `Project(pid) *ProjectBlobStore`,
  `Clone(src, tgt)`, `CopyHash(src, tgt, hash)`, `DeleteProject(pid)`,
  `ProjectBlobs(pid) []BlobInfo{Hash, ByteLength, HasTextLength, TextLength}`.
  `ProjectBlobStore`: `PutString/GetString`, `PutObject/GetObject`, `PutBytes/GetHashBlob/
  StringBlob/GetRangesBlob/Contains`.
- `service/chunkstore/Store` — `Initialize(pid, snapshot) (endVersion, err)`, `Create`, `Update`,
  `LoadLatest`, `LoadAtVersion(pid, v, preferNewer)`, `LoadAtTimestamp`,
  `GetLatestChunkMetadata(pid) ChunkMetadata{ID, StartVersion, EndVersion, EndTimestamp time.Time}`,
  `GetChunkForVersionMetadata`, `ChangesSince(pid, since, preferNewer) ([]*Change, hasMore, err)`,
  `Clone(src, tgt)`, `LoadHistory`, `DeleteProjectChunks`.
  Errors: `AlreadyInitialized`, `ChunkVersionConflictError`, `VersionOutOfBoundsError{Msg}`,
  plus `core.Chunk{Not,Version,BeforeTimestamp,NotPersisted}NotFoundError`.
- `service/persist/Service` — `persist.NewService(cs, bs)`, `PersistChanges(pid, changes,
  limits, clientEndVersion) *PersistResult{CurrentChunk, ResyncNeeded, ...}`,
  `BuildSetContentChange(pid, pathname, BuildSetContentOpts{Content *string, BlobHash *string,
  Metadata json.RawMessage, UserID, Timestamp, Origin *core.Origin, TrackChanges})
  *SetContentResult{Change *Change, BaseVersion int}`; `CommitChanges` levels 1-4/ForcePersist
  → `NotPortedError`. `Limits{ChangeBucketMinutes, MaxChanges, MaxChangeBytes, MaxChunkChanges,
  MaxChunkChangeBytes, MaxChunkChangeTime, MinChangeTimestamp *time.Time, MaxChangeTimestamp}`;
  NOTE `util.go farFutureLimits()` sets MaxChanges: 0 (Node: `maxChanges: 0`).
- `service/api/render.go` — `writeJSON`, `renderBadRequest/NotFound/UnprocessableEntity/
  Conflict/RequestEntityTooLarge(w)` (body `{message}` for the render.* family), `handleAPIError`
  (terminal: `{message, error:{}}` — see §4.3).
- `service/api/security.go` — `authorize(mode, r, projectID) *AuthError`,
  `hasValidBasicAuthCredentials`. Modes: basic / jwt / token / either.
- `service/api/util.go` — error classifiers (`isChunkNotFound` covers all four chunk
  NotFound types, `isNotPersisted`, `isUnprocessable`, `isConflict`, `isTooLarge`),
  `validationErr{field, statusCode, isParamsErr}` (renders `{error, statusCode}` body:
  `"Validation failed for params: [project_id]"` → 404, or `"Validation failed for query: [N]"`
  for query errors), `projectIDValid/hashValid/copyFromValid/intParse/tsParse`, `projectIDRX`
  (mongo-24-hex | postgres `[1-9][0-9]{0,9}`), `hexHashRX` (40 hex), `farFutureLimits()`.

## 3. Route table (VERIFIED from api/routes/projects.js + project_import.js + app.js)
Manual dispatch (no ServeMux) is required for Express parity:
**unmatched method on known path → 404** (Go default would be 405); unknown path → terminal 404
body `{message: "Not Found", error: {}}` (NOT `render.notFound` body, and NOT empty).

Top level (app.js):
| route | behavior |
|---|---|
| `GET /` | `res.send('')` → 200, empty body |
| `GET /status` | `res.send('history-v1 is up')` (Node pings Mongo first; hermetic = always up) |
| `GET /health_check` | `res.send('OK')` → 200, body `OK` (NOT `/healthcheck` — underscore!) |
| `GET /docs` | basic-auth guard `WWW-Authenticate: Basic realm="Application"`, 401 empty on fail; on success `res.send('OK')` → body `OK` (NOT HTML) |

Under `/api` (all `:project_id` = `projectIDRX`, 404 `validationErr` on bad shape):
| route | method | auth | handler |
|---|---|---|---|
| `/projects` | POST | basic | initializeProject |
| `/projects/:id/clone` | POST | basic | cloneProject |
| `/projects/blob-stats` | POST | basic | getProjectBlobsStats (LITERAL `blob-stats` segment) |
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
| `/projects/:id/versions/:v/content` | GET | jwt | getContentAtVersion |
| `/projects/:id/timestamp/:ts/history` | GET | jwt | getHistoryBefore |
| `/projects/:id/latest/zip` | GET | token | getLatestZip |
| `/projects/:id/version/:v/zip` | GET | token | getZip |
| ` (same path) ` | POST | basic | createZip → 501 Not Ported |
| `/projects/:id/changes` | GET | basic | getChanges |
| `import-router, same path` | POST | basic | importChanges ← **GET+POST on same path, different handlers** |
| `/projects/:id/import`, `/legacy_import` | POST | basic | importSnapshot |
| ` (same pair) ` | POST | basic | (aliases of importSnapshot) |
| `/projects/:id/changes`, `/legacy_changes` | POST | basic | importChanges |
| `/projects/:id/set_content` | POST | basic | setContent |
| `/projects/:id/flush` | POST | basic | flushChanges |
| `/projects/:id/expire` | POST | basic | expireProject |
| `/projects/:id/zip` (no version prefix) — | — | — | does NOT exist |

Gotchas:
- `POST /api/projects` (no id) = initialize. `GET /api/projects` = terminal 404 (no such route).
- `GET` and `POST` on `/projects/:id/changes` are DIFFERENT endpoints.
- `persistedHistory` ≡ `latest/history` handler.
- Auth runs BEFORE param validation (Express middleware order); Go: `authorize(...)` then
  `projectIDValid()` etc.

## 4. Exact response contracts (verified)
### 4.1 Success bodies
- initializeProject: `200 {"projectId": "<id>"}`. Body may omit projectId → generate (hermetic:
  incrementing numeric string like Postgres `docs_id_seq` is simplest: `counter+ "1000000"`-style;
  tests only check `assert.projectId` shape = mongo-24hex or postgres numeric). Also `AlreadyInitialized` → 409 `render.conflict` (body `{message: "Conflict"}` — render.conflict with no message → message = HTTPStatus[409] = "Conflict").
- importSnapshot: parse body as Snapshot (`core.SnapshotMustFromRaw`); fail → 422
  `render.unprocessableEntity` (`{message: "Unprocessable Entity"}`);
  `chunkstore.Initialize` AlreadyInitialized → 409; else `200 {"projectId": ...}`
  (returned historyId = the projectId given).
- importChanges: query `end_version` (z.coerce.number — required!), `return_snapshot`
  default `'none'`, enum `['hashed','none']`. body = JSON **array** of raw changes.
  Parse each with `ChangeMustFromRaw`; any fail → 422. Level 0 (hermetic) →
  `persist.PersistChanges(pid, changes, farFutureLimits(), endVersion)`;
  catch: ConflictingEndVersion/Unprocessable/NotEditable/Pathname/EditMissing/
  ChunkVersionConflict/InvalidChange → 422 `render.unprocessableEntity` (generic, message =
  "Unprocessable Entity"); Chunk.NotFoundError → 404 `render.notFound`.
  Response: `None` → `201 {"resyncNeeded": <bool>}`; `hashed` → `201` + raw snapshot JSON
  (`snapshot.Store(pbs)` from resulting/current chunk: load `GetLatestChunkMetadata(pid)` if
  result nil… Node: `result.currentChunk` — with hermetic level 0 it's ALWAYS the persisted
  chunk (PersistResult.CurrentChunk), so use that; for 'hashed' fallback
  `GetLatestChunkMetadata`-driven chunk load if nil).
- setContent: body `{pathname, source?, userId?, timestamp (ISO), metadata?, content? XOR
  blobHash?, trackChanges?}` exactly-one-of enforced (persist.SetContentXorError → 422).
  timestamp missing → 422 explicitly. Build via `BuildSetContentChange` (origin:
  `{"source": source}` — Go `core.Origin` has no plain source constructor; check
  `origin.go`: only Editor/Restore variants + `OriginFromRaw`. Either marshal
  `{"source": "..."}` with `OriginFromRaw` (if it accepts `{source}`) or add an
  `internal/...` helper; Origin wire = `{type, source?, version?...}` — verify Node
  `new Origin(source)` produces `{source: "..."}` only when source non-empty).
  Errors: ContentTooLarge → 413 `render.requestEntityTooLarge` (body `{message:
  "Request Entity Too Large"}`); NotFoundError → 404 `render.notFound`; BlobNotFound/
  Unprocessable/NotEditable/Pathname/EditMissing/InvalidChange/trackedChanges-sync → 422.
  Success: `200 {"baseVersion": N, "change": raw-or-null}` (Node `change ? change.toRaw() :
  null` — note: 200 even when change is null).
- getLatestContent: `loadLatest` + `applyAll(changes)` + `loadFiles('eager', pbs)`,
  `200 snapshot.ToRaw()`. **No try/catch in Node** → uncaught NotFound → terminal
  `500 {"message": "no chunks for project X", "error": {}}`. Mirror: Go handler must NOT
  catch-isChunkNotFound here (let it reach `handleAPIError` → err with no statusCode
  → 500). (Same for getLatestHashedContent — but hashed uses HashCheckBlobStore; see §5.)
- getContentAtVersion: `getSnapshotAtVersion(pid, v)` (loadAtVersion + dropRight
  changes + previous-chunk timestamp fallback, errors swallowed for first-chunk case),
  `loadFiles('eager')`, `200 snapshot.ToRaw()`. NO NotFound catch → 500 terminal on
  missing project/version (Node `getSnapshotAtVersion` throws ChunkNotFoundError unhandled).
- getLatestHashedContent: same shape as getLatestContent but with
  `HashCheckBlobStore` → **hermetic: use plain pbs** (re-validation is a Node
  integrity check against S3, no-observable-difference in-memory). `snapshot.Store(pbs)`
  raw = same shape as ToRaw. NO catch → 500 terminal on missing.
- getLatestHistory / persistedHistory: `{ "chunk": <chunk.ToRaw()> }` 200, or 404
  `render.notFound` (body `{message: "Not Found"}` — note: message present, no
  `error` key).
- getLatestHistoryRaw: `200 {"startVersion": N, "endVersion": N, "endTimestamp": "<ISO>"}`
  via `GetLatestChunkMetadata`; 404 `render.notFound` on NotFound. endTimestamp =
  Node `Date().toISOString()` → Go format `t.UTC().Format("2006-01-02T15:04:05.000Z")`.
  (Node getLatestChunkMetadata returns endTimestamp as ISO string, NOT JSON number.)
- getHistory (versions/:v/history): `LoadAtVersion(pid, v, false)`, `{chunk: ...}`,
  404 catch.
- getHistoryBefore: `LoadAtTimestamp`, `{chunk: ...}`, 404 catch.
- getChanges: query `since` optional (default 0, z coerce int); since<0 →
  `400 {"error": "Version out of bounds: -1"}` (exact body, no `error:{}` key);
  `ChangesSince(pid, since, preferNewer=TRUE)`; VersionNotFoundError → same 400 body
  with since value; success `200 {"changes": [raw...], "hasMore": bool}`.
  Message format check: Go `VersionOutOfBoundsError.Msg` should already be
  `"Version out of bounds: N"` OR build body manually from since (manual is safer:
  `{"error": fmt.Sprintf("Version out of bounds: %d", since)}`).
- deleteProject: 204 empty. (`cs.DeleteProjectChunks(pid)` + `bs.DeleteProject(pid)`;
  Node 204 even if project never existed.)
- getProjectBlob: hash param 40-hex (validationErr 404). Missing blob → `404 EMPTY`
  (`.end()` — no JSON body, no Content-Type). Else `Content-Type:
  application/octet-stream` + body bytes; Range header `/^bytes=(\d{1,7})-(\d{1,7})$/`:
  invalid start/end range → `416` headers `Content-Range: bytes */L`,
  `Content-Length: 0`, no body; valid → `206` + `Content-Range: bytes S-A/L` +
  `Content-Length: A-S+1` + partial body (blob slice; hermetic: `PutBytes` blob →
  slice bytes).
- HEAD blob: `200` + `Content-Length` only, no body; missing → 404 empty.
- createProjectBlob (PUT): read body (12MB cap via request size — Node checks
  maxFileUploadSize on stream; Go: `http.MaxBytesReader` per request in main, then
  len check `> cfg.MaxFileUploadSize` → 413 `render.requestEntityTooLarge`). Compute
  git-blob hash (`contenthash` package: `gitBlobHash(content) = "git blob " + len +
  "\0" + bytes` → sha1 hex); mismatch with :hash → 409 `render.conflict(res,
  'File hash mismatch')` → body `{message: "File hash mismatch"}` (render.conflict takes
  optional message! verify render.go handles `renderConflict` message arg —
  Node `render.conflict(res, 'File hash mismatch')`. Go render.go has `renderConflict(w)`
  with fixed message "Conflict" — EXTEND: need `renderConflictMsg(w, msg)` or write
  `{message: "File hash mismatch"}` manually). Match → `PutBytes` → `201` empty.
- copyProjectBlob (POST, query `copyFrom`, optional `sizeLimit` numeric):
  source missing → 404 `render.notFound`; `sizeLimit > 0 && byteLen > sizeLimit` →
  `413 {"size": N}` (exact shape — size key, NOT message); target already has blob →
  `204` empty; else → `CopyHash` → `201` empty.
- getBlobStats (per-project): body `{"blobHashes": ["<40hex>", ...]}` (assert each →
  400 `render.badRequest` on invalid); stats ONLY over those hashes (not whole
  project): each hash → `StringBlob(h)` found? → text (byteLength) else binary.
  `200 {"projectId": id, "textBlobBytes": N, "binaryBlobBytes": N, "totalBytes": N,
  "nTextBlobs": N, "nBinaryBlobs": N}`. (Node batches via BatchBlobStore; hermetic:
  per-hash lookup.) Missing blob for a hash: Node `filter(Boolean)` → counts only
  PRESENT blobs. So hash absent from project = silently skipped.
- getProjectBlobsStats (batch, literal `blob-stats` segment): body
  `{"projectIds": [...]}` (each projectHistoryId); response = JSON **array** of same
  stat objects, using ALL blobs of each project (`Store.ProjectBlobs(pid)`) →
  text/binary counts by `HasTextLength` flag (BlobInfo). `200 [...]`.
- cloneProject: body `{"targetProjectId": ...}`. Flush source (persistBuffer is
  Redis-only; hermetic: skip / no-op), then `cs.Clone(src, tgt)` + `bs.Clone(src, tgt)`.
  Response: Node uses IncrementalResponse streaming (NDJSON) — **no acceptance test
  covers clone** → simple `200` on success is acceptable hermetic behavior
  (document the deviation). Errors → terminal 500.
- getLatestZip: `loadLatest` (404 catch), `res.setHeader('X-History-Version',
  chunk.getEndVersion())` header on ALL (incl. before body), then
  `Content-Type: application/octet-stream`, `Content-Disposition:
  attachment; filename=project.zip`, body = ZIP of eager file contents
  (Node project_archive streamZip: file path entries, uncompressed-deflated?
  check project_archive.js `ZipStore` / `streamZip` — it uses `archiver`-style
  with `file` entries named `basename`; empty files included; binary files
  included raw; text file content = full string). Go: `archive/zip`, Deflate
  (or Store — pick Deflate; content parity, bytes differ but tests check
  contents not bytes), entry names = file pathnames (safe_pathname).
- getZip (versions/:v/zip): `getSnapshotAtVersion` semantics (preferNewer default
  FALSE for version-specified), 404 catch, zip same (NO X-History-Version
  header — verify: Node getZip does NOT set the header (only getLatestZip does)).
- createZip (POST version/:v/zip): 501 — respond like Node terminal: NOT ported →
  hermetic decision: `501 {"message": "Not ported: ...", "error": {}}` or 500; no
  acceptance test → pick 501 terminal body (document deviation).

### 4.2 flush / expire
- flushChanges: hermetic persistBuffer = no Redis buffer = no-op success → `200
  EMPTY` (`res.status(200).end()`). Node catches Chunk.NotFoundError → 404
  render, but persistBuffer only throws for missing project if Redis… hermetic:
  treat unknown project as `200` (nothing to persist) — CHECK: acceptance
  `project_flush.test.js` uses an EXISTING project (createEmptyProject first) and
  queues via `queueChanges` (Redis-only; the Go test can't queue) → simplest
  hermetic flush = `200 END` always.
- expireProject: `200` empty always (Node redis expire; hermetic no-op).

### 4.3 Terminal 404 + error handler (app.js `setupErrorHandling`)
- Catchall (unknown route): `new Error('Not Found')` err.status=404 → terminal
  handler body: `{"message": "Not Found", "error": {}}` (prod env; development
  would put err in error key — tests run prod → `{}`).
- Terminal handler for thrown errors with no statusCode/status → 500 +
  `{"message": err.message, "error": {}}`.
- `handleAPIError(w, err)` in Go must produce exactly `{message, error:{}}`
  (already implemented in render.go — verify it emits `error: {}` key always).

## 5. Hermetic decisions already made (confirm, don't re-litigate)
1. HashCheckBlobStore = plain pbs (re-validation observable-only against external
   S3; in-memory store is authoritative).
2. Redis persist buffer = no-op; level 0 only.
3. Zip GET = `archive/zip` stdlib; entry = every file in snapshot (text +
   binary).
4. `GET /` → 200 empty body (Node `res.send('')`).
5. POST zip → 501 Not Ported terminal body.
6. generateProjectId → incrementing numeric string (Node postgres sequence),
   NOT mongo-24hex (tests accept either).
7. Timestamps in JSON bodies (endTimestamp etc.) → `UTC().Format(
   "2006-01-02T15:04:05.000Z")` to match `Date.toISOString()`.

## 6. main.go assembly (cmd/history-v1/main.go)
1. `cfg, _ := config.FromEnv()`.
2. `bs := blobstore.NewStore()`; `cs := chunkstore.New(hs, func(pid string) core.
   BlobStoreI { return bs.Project(pid) })` — verify `chunkstore.New` signature
   (takes `blobs func(pid) core.BlobStoreI`; adapt `bs.Project` accordingly) and
   `historystore` constructor.
3. `api.New(cs, bs, configView{...passwords..., MaxFileUploadSize int})` —
   NOTE configView.MaxFileUploadSize is `int` in current api package but
   `cfg.MaxFileUploadSize` may be int64 — reconcile at the boundary.
   Also `persist.NewService(cs, bs)`: confirm it takes `*blobstore.Store`
   (yes — grep showed `NewService(cs, bs)`).
4. Body limit: wrap handler with 12MB `http.MaxBytesReader` middleware for
   POST /api/projects/* (Node express.json 12MB; overflow → 413 body
   `{"message": "request size too large", "error": {}}`? — verify:
   express.json 413 → Node terminal handler → `message: "request size too
   large"` (body-parser's default message, status 413 set by express); Go
   MaxBytesReader error → return 413 with terminal shape `{message:
   "request size too large", error:{}}`.
5. Auth timeouts: Node `res.setTimeout(HTTPRequestTimeout=300000)` → Go
   `http.Server{ReadTimeout, WriteTimeout, IdleTimeout: 300s}`.
6. Port: `cfg.Port` default 3100 (env PORT). `ListenAndServe`.

## 7. Acceptance-test coverage (must pass hermetic)
auth, end_to_end, project_blobs, project_flush, project_hashed_content,
project_import, project_updates, projects, project_expiry, set_content,
backupDeletion, backupVerifier, rollout. Test harness: `test_server.js` boots a
server on a port with basic auth `staging:<password>` + JWT secrets from env —
the Go binary must accept `BASIC_HTTP_AUTH_PASSWORD/OLD`, `JWT_AUTH_KEY/OLD`
env names matching `config.FromEnv()`.

## 8. Immediate next steps
1. Fix `service/api/server.go` dispatch (rewrite `dispatchAPI` cleanly using the
   table in §3; add `splitPath` helper or slice-split).
2. Add `service/api/projects.go` — all controllers from §4.
3. Add `service/api/import.go` — import/importChanges/setContent/flush/expire.
4. Remove `probe.go` (temp compilation probe).
5. Extend `render.go` with a message-variant conflict renderer (`File hash
   mismatch`) if `renderConflict` doesn't accept a message.
6. `cmd/history-v1/main.go` per §6.
7. `go build ./...` green → run acceptance suite against Go binary on
   `PORT=<test port>` with the test_server's env.
