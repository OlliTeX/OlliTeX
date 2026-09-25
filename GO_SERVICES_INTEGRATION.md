# Go service integrations — status & gap audit (2026-09-25)

Two independently prepared Go ports were integrated into this repo and audited
against the Node oracles this repo pins (`services/history-v1/`,
`services/document-updater/`). Both modules are **green in this repo**
(`make go-test-history-v1` / `make go-test-document-updater`), but
**neither is yet a live-service replacement** — the gaps below are the flip
gates. Audit performed 2026-09-25 (this branch); per-module detail lives in
their `HANDOFF.md` (treat those claims as verified only where re-checked here).

## Integrated (green in this repo)

### `services/history-v1.go` (module `history-v1`) — 153 test fns, gate green
- **API layer: real and faithful.** Route table re-verified line-by-line
  against `services/history-v1/api/routes/projects.js` — including the trap
  the port got right that its own HANDOFF table misstates: Node zip is
  singular `/projects/:id/version/:v/zip` while history/content are plural
  `/versions/:v/{history,content}` (the dispatch code `server.go` matches the
  Node oracle exactly). Per-route auth modes verified (basic / jwt / token /
  mixed, e.g. `latest/hashed_content` = basic, `latest/zip` = token,
  `versions/:v/zip` GET token + POST basic).
- Error contracts mirror the Node terminal handler (`{"message", "error":{}}`),
  zod-tier 422s, the empty-changes-import Node-`TypeError`→500 quirk, and the
  Express 404 (404-not-405) semantics.
- **Live hermetic smoke (host, this audit):** boot, `GET /` (200 empty),
  `GET /status`, `GET /health_check` ("OK"), `POST /api/projects`
  (basic `staging:<STAGING_PASSWORD>` → 200 `{"projectId":"1"}`),
  `DELETE /api/projects/:id` (204), per-route 401 matrix — all behave per
  contract.

**Gaps (flip blockers):**
1. **Storage is an in-memory fake** (`blobstore`/`historystore` = hermetic
   structs; chunkstore over them). Live restart ⇒ all data gone. The Node
   service persists chunks (Mongo in this stack — **no Postgres exists in
   e2e/compose_cep**) + blobs via `object-persistor` (S3/TPDS) + Redis
   persist-buffer.
2. **Redis persist-buffer / queueChanges / flush / expire = no-ops** (flush
   returns 200 without persisting; expire is a no-op).
3. **501 bucket:** `cloneProject`, `getLatestZip`, `getZip` (GET zip),
   `createZip` (POST zip) — the zip/clone/streaming surface. Our Go web
   already calls `…/version/:v/zip` (Go-web pinned) and the duplication flow
   calls clone — those will 501 against this port.
4. **blob PUT (createProjectBlob)** — 501. The Go web's example-project
   creation uploads blobs into history-v1 (`create.go crUploadBlob`) — would
   501.
5. Minor parity note: the port implements an explicit `HEAD` blob route
   (200 + Content-Length); this fork's live Node behavior (pinned by our Go
   web `fileproxy.go`) is HEAD → 404 empty. Match-or-justify at flip time.

### `services/document-updater.go` (module `document-updater`) — 215 test fns, gate green
- The **internal logic core** of the Node service (ShareJS text/json/text-tp2/
  text-composable OT types, ShareJS model, ShareJsDB facade,
  ShareJsUpdateManager, HistoryOTUpdateManager, UpdateManager pipeline,
  RangesManager, preview, history-conversions, RedisManager facade,
  ProjectHistoryRedisManager, RateLimit, limits, diff codec) — ported with
  large oracle golden tables (96-row json, 50-row tp2, 12-scenario model, 8
  redis-manager pin groups, 31-test UpdateManager suite), all gate-green here.

**Gaps (flip blockers — this is a library set, NOT a service yet):**
- **No HTTP server, no `main`** (`cmd/` empty in the source tree; git skips
  empty dirs).
- Missing managers the Node service needs: DocumentManager (833 LOC),
  ProjectManager, PersistenceManager, HistoryManager, DispatchManager,
  SnapshotManager, DeleteQueueManager, ProjectLockManager, WebApiManager.
- Missing persistence: Mongo adapters and the Bull queue loop; the real
  Redis/HTTP wiring (the committed `redismanager` is the facade over an
  injectable client — no real client is wired).

## Verified-true vs claimed (the "check carefully" pass)
- ✅ Both full gates (build/vet/gofmt/test -race) reproduced in this repo.
- ✅ Route table vs Node (spot-audited; the HANDOFF's own route table has ONE
  misstatement — `versions/:v/zip` — but the CODE is correct).
- ✅ Live binary boots and serves the top-level + auth contracts.
- ✅ `staging` username + `STAGING_PASSWORD` basic auth, JWT `Bearer` with
  `project_id` claim (tests pin old/new key matrix).
- ⚠️ HANDOFF_API.md/HANDOFF.md "live home" paths (`/home/davrot/...`) are the
  authoring machine, not ours — stale, documented.
- ⚠️ document-updater HANDOFF "Phase 9 = remaining managers / controller"
  confirmed by the source tree (no cmd, no http package).

## Flip gates (definition of "done" per service)
history-v1: Mongo chunk store + real blob store (object-persistor/S3, reuse
the existing Go `persistors` library) + Redis persist-buffer/flush/expire +
zip streaming (`archive/zip`) + clone + blob PUT → then a live A/B spec
(Go-web V1 client at `:3100` green against the Go service, full e2e).
document-updater: DocumentManager + remaining managers + Mongo/Bull + HTTP
controller + main → then live editor-save A/B (real-time → Go updater) +
ranges/track-changes e2e.

Both flips stay owner-gated (stack cycle). Until then the live stack keeps
running the Node services — the Go modules are integration-ready building
blocks, verified green, but not wired in.
