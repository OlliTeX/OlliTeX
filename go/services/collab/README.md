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
- **persistence (S1)**: ygo `FilePersistence` — a per-room versioned binary
  update log + snapshots (the new "history" model; S2 moves it to Mongo via the
  `VersionedPersistence` adapter). Room isolation: one project's log never
  leaks into another.
- **rooms**: path `/collab/{projectId}` — room name = project id.

## Files

| file | purpose |
|---|---|
| `collab.go` | service core: `New()` (ygo server + `Authorize` hook), `AuthSource`, `SessionAuth` (session→uid→frozen check, project roles), fail-closed policy |
| `mongo.go` | production `Mongo` impl (users/projects lookup) + `RedisSessionDoc` (cookies→session store) |
| `collab_test.go` | auth-gate 401 matrix, SessionAuth fingerprints, CRDT convergence ×2, FilePersistence round-trip / room isolation |
| `../cmd/collab/main.go` | entrypoint: env config, `/healthz`, room mux, graceful shutdown |

## Run

`make go-run-collab` (dev) or `./bin/collab` (after `make go-build`).

Env: `COLLAB_LISTEN` (:3450), `COLLAB_KEEP_VERSIONS` (0 = keep-all history; N = retain most-recent N) and `COLLAB_COMPACT_EVERY` (0 = compact on room unload; N = also every N flushes) — both settable in the /hub admin too (config-DB value wins over env; binds on service start — D23), `MONGO_CONNECTION_STRING`/`OVERLEAF_MONGO_URL`,
`OLLITEX_DB_NAME`, `OVERLEAF_REDIS_HOST/PORT/PASS`, `COLLAB_DATA_DIR`
(/data/collab-docs), `COLLAB_ALLOWED_ORIGINS`, `COLLAB_MAX_CONNECTIONS`,
`COLLAB_MAX_PEERS_PER_ROOM`, `COOKIE_NAME`. The SEED SOURCE (S4 contract) is wired in `cmd/collab`: a room adopts the project's CURRENT main-file content through the web's pinned blob path — env `WEB_V1_HISTORY_URL` (default `http://127.0.0.1:3100/api`), `V1_HISTORY_USER` (default `staging`), `V1_HISTORY_PASSWORD` — see `seedsource.go` (main.tex → first .tex → empty; 404 blob = empty seed; any other failure = fail-closed).

## Gates

`make go-test-collab` — build + vet + gofmt + `go test -race` (incl. the seed-source suite: selection rule, pinned blob path + basic auth, fail-closed matrix, ObjectID hid, full WS seed e2e).

## Slice status (ARC-9)

- **S1 (this)**: service core + auth + FilePersistence + gates — **DONE**
- **S2**: Mongo `VersionedPersistence` adapter + history/restore endpoints on
  Go web
- **S3**: client editor page (yjs + y-websocket + y-indexeddb + CodeMirror
  binding), hard cut of the OT editor
- **S4**: flip — node real-time + OT substrate → junk; runit + nginx + image
  re-bake; new Yjs e2e (convergence / offline reload / history restore)
