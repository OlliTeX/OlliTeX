# collab — Yjs/Ygo collaboration service (ARC-9, D19)

The WebSocket relay + document persistence for the Yjs editor. Replaces the
node `real-time` service (socket.io 0.9 fork + ShareJS + OT) per the owner's
**hard-cut** pivot decision (D19): the document model is now Yjs; there is **no
OT legacy reader** — old OT history is dropped, new Y documents seed from
current file content.

## What it is

A thin OlliTeX layer around [ygo](https://github.com/reearth/ygo)
(`github.com/reearth/ygo@v1.50.0`, MIT) — a production CRDT stack with a
Hocuspocus-compatible y-protocols WebSocket server:

- **relay**: ygo `provider/websocket` server — y-protocol sync (SyncStep1/2 +
  Update), message relay, room lifecycle, graceful drain
- **auth**: OlliTeX session (shared Redis session store, `COOKIE_NAME` cookie
  `overleaf.sid` → `sess:<sid>` → `passport.user._id`) + user not frozen +
  project role gate:
  - `owner_ref` / `collaborator_refs` → **read-write** peer
  - `readOnly_refs` → **read-only peer** (ygo `ConnectionConfig.ReadOnly`:
    receives broadcasts, inbound writes dropped)
  - else → 401 at the WebSocket handshake (fail-closed on store errors)
- **persistence**: per-room versioned binary update log + snapshots (the new
  "history" model). **Production = `MongoStore`** (`mongostore.go`, the ygo
  `VersionedPersistence` interface over the `"ydoc"` collection — D19: Mongo
  is the system of record); `FilePersistence` remains the dev/test fallback
  (`DataDir`). Room isolation: one project's log never leaks into another.
- **rooms**: path `/collab/{projectId}` — room name = project id.

## Files

| file | purpose |
|---|---|
| `collab.go` | service core: `New()` (ygo server + `Authorize` hook), `AuthSource`, `SessionAuth` (session→uid→frozen check, project roles), fail-closed policy |
| `mongo.go` | production `Mongo` impl (users/projects lookup) + `RedisSessionDoc` (cookie→session store, EXACT web wire contract: pct-decode → `s:` → HMAC-SHA256 un-sign, D26) + `NewMongoFrom` (single-client wiring) |
| `mongostore.go` | `MongoStore` — ygo `VersionedPersistence` over Mongo (update log + snapshots + compaction; `RunConformance` green) |
| `roomdoc.go` | shared room-doc domain ops (`TEXT_TYPE="content"`, seed, TextAt, RestoreToVersion, ClientEdit) |
| `seedsource.go` | seed source (D26): projects `rootDoc_id` → docstore lines → text |
| `collab_test.go` | auth-gate 401 matrix, SessionAuth fingerprints, cookie wire-contract pin, CRDT convergence ×2, persistence round-trip / room isolation |
| `relay_diag_test.go` | hermetic 2-peer live-contract pin (seed → relay → persistence) |
| `../cmd/collab/main.go` | entrypoint: env config, single Mongo client (auth+seed+store), `/healthz`, room mux, graceful shutdown |

## Run

`make go-run-collab` (dev) or `./bin/collab` (after `make go-build`).

Env: `COLLAB_LISTEN` (:3450), `COLLAB_KEEP_VERSIONS` (0 = keep-all history; N = retain most-recent N) and `COLLAB_COMPACT_EVERY` (0 = compact on room unload; N = also every N flushes) — both settable in the /hub admin too (config-DB value wins over env; binds on service start — D23), `MONGO_CONNECTION_STRING`/`OVERLEAF_MONGO_URL`,
`OLLITEX_DB_NAME`, `OVERLEAF_REDIS_HOST/PORT/PASS`, `COLLAB_DATA_DIR`
(/data/collab-docs), `COLLAB_ALLOWED_ORIGINS`, `COLLAB_MAX_CONNECTIONS`,
`COLLAB_MAX_PEERS_PER_ROOM`, `COOKIE_NAME`, `COLLAB_LOG_LEVEL` (debug = ygo frame/persistence decisions to the service log; D22 ops surface). `COLLAB_DATA_DIR` is the DEV/TEST fallback only — production persists to Mongo. The SEED SOURCE (S4 contract) is wired in `cmd/collab`: a room adopts the project's CURRENT main-file content read from the docstore — the SAME document the web editor renders — env `WEB_DOCSTORE_URL` (default `http://127.0.0.1:3016`), optional `V1_HISTORY_USER`/`V1_HISTORY_PASSWORD` (basic-auth parity) — see `seedsource.go` (projects doc → `rootDoc_id` → `GET /project/{pid}/doc/{did}` → `join(lines,"\n")`; no rootDoc / 404 = empty seed; any other failure = fail-closed).

## Gates

`make go-test-collab` — build + vet + gofmt + `go test -race` (auth gate + cookie wire contract, MongoStore conformance, seed-source suite: docstore oracle + auth parity + fail-closed matrix + full-WS wiring, 2-peer relay+persistence pin, retention-knob wiring).

## Slice status (ARC-9)

- **S1**: service core + auth (cookie wire contract, D26) + gates — **DONE**
- **S2**: Mongo `VersionedPersistence` (`MongoStore`, `RunConformance` green)
  + retention knobs (`KeepVersions`/`CompactEvery`) — **DONE**
- **S2b/S3-server**: history/restore endpoints on Go web
  (`features/collabhistory`) + seed hook — **DONE**
- **S3-client**: Yjs client engine + CM6 bridge (D24) in
  `frontend/js/features/ide-react/collab` — **DONE** (14-test suite)
- **S4 prep**: runit + nginx `/collab` + image re-bake (build #2, healthy) —
  **DONE**; **S4 LIVE AUDIT (D26)**: three production bugs caught + fixed,
  full live E2E green (seed/relay/persist/restore) — **DONE**
- **S4 flip (REMAINING)**: IDE hard cut OT→Yjs (wire `createEngine`,
  comments/track-changes disabled per D25), retire OT substrate → `junk/`,
  new Yjs e2e in the repo suite (convergence / offline reload / history
  restore)
