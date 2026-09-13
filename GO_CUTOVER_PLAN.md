# GO_CUTOVER_PLAN — make the 9 Go microservices active (Node → Go)

**Date** 2026-09-17 · **Scope** chat, notifications, docstore, filestore,
linked-url-proxy, datamanipulator, dropboxinterface, githubinterface,
webdavinterface — all converted under `go/services/*` with green unit suites,
docstore additionally live-verified against the real mongod (30/30).

## Why this cutover is structurally safe

* **Zero data migration.** Go and Node services read/write the **same Mongo
  collections** and the **same FS bucket layout** (flat keys), so flipping a
  service over does not touch data — and flipping back never does either.
* **Zero client changes.** Same ports, same bind (`LISTEN_ADDRESS` ||
  127.0.0.1), same env var surface, same routes/status codes/error strings
  (unit-locked per service). The web app and clients see an identical
  interface.
* **Rollback is one line.** The Node entrypoint stays in the image; the runit
  `run` script picks the binary via env. `sv restart <svc>` applies it
  immediately.

## Verified facts (2026-09-17)

* All 9 Node services are runit services under `server-ce/runit/*-overleaf/`;
  **none has a `check` program** (no sv health gate to satisfy). Run scripts
  `source /etc/overleaf/env.sh`; chat/notifications/docstore/linked-url-proxy
  additionally export `LISTEN_ADDRESS=127.0.0.1`.
* **The image does not contain the Go binaries today** — `server-ce/Dockerfile`
  COPYs a fixed path list that excludes `go/`, `cmd/`, `bin/`; no golang
  image exists on this offline box → **host-build + COPY** is the only viable
  packaging path (host has Go 1.27 + warm module cache).
* Makefile already has `go-check / fmt-go / lint-go / test-go / go-build /
  go-run-*` targets; `go-build` is missing **docstore** (added after the
  targets were written). `make image` = `cd server-ce && make all`
  (docker build).
* Runtime env: the Node services run with **no `MONGO_*` vars set** — they
  rely on the default chain `MONGO_CONNECTION_STRING ||
  mongodb://MONGO_HOST||'127.0.0.1'/sharelatex`. The Go mains mirror the
  identical chain, so both reach the same mongo from the same container with
  the same env. `SHARED_SERVICE_TOKEN` and `GITHUBINTERFACE_WORKDIR_ROOT`
  (`/var/lib/overleaf/ghif`) come from `compose.yaml`.
* **Node `chat` makes no outbound HTTP at all** — its `apis.web`
  (WEB_HOST/PORT + WEB_API_USER/PASSWORD) is defined but unused by the app.
  The Go chat port (thread/message/notification fan-out, no outbound) is a
  true 1:1; there is no hidden "Ask AI proxy" gap.
* Ports: the four bridge cmds honor `<NAME>_PORT`; chat/docstore/filestore/
  notifications/linked-url-proxy have the **default baked in** (matching the
  Node defaults) but no env override → Phase A adds a `PORT` escape hatch to
  all nine (needed for shadow-side differential runs).
* e2e coverage today: editor/comment flows (bug-hunt r1-r3, editor-core,
  panes), template-gallery + file flows (implicit docstore/filestore),
  `notifications` (1 thin spec: hub toggle + 301), `sync-graceful`
  (webdav+dropbox status must-not-5xx), zotero, external-probe. **Gaps:**
  linked-URL import (no spec), real webdav/dropbox sync journeys (status only),
  chat→notification→email end-to-end (smtp sink exists in the test stack),
  github (needs github.com — not reachable; unit + manual only).

## Phase A — build & packaging (no behaviour change)

| # | Item |
|---|------|
| A1 | DONE ✅ (2026-09-12): `go-build` now builds `docstore` + `seaweed-migrate`; `image` depends on `go-build` so every image build ships current binaries. |
| A2 | `server-ce/Dockerfile`: `COPY bin/ /usr/local/bin/go-services/` + `chmod 755` (after the `libraries/ services/` COPY). Offline-safe, no toolchain in the image. |
| A3 | Runit: per-service switch inside each `<svc>-overleaf/run`: `if [ "$USE_GO_<SVC>" = "true" ]; then exec /usr/local/bin/go-services/<svc>; else exec node ...; fi` — **per-service** flags (finest rollback), same env (env.sh sourced), same `LISTEN_ADDRESS` exports, same log file (`/var/log/overleaf/<svc>.log`), same `setuser www-data`. |
| A4 | `cmd/*` mains: honor a `PORT` env override (default unchanged) on all nine — enables shadow ports for the differential harness and for running Go next to Node in the dev container. (docstore + filestore done; the rest follow when their cutover prep lands.) |
| A5 | Smoke: rebuild image, `USE_GO_*=false`, cycle — stack boots, 9 services up, no diff vs today (baseline proof the packaging is inert). |

## Phase A2 — SeaweedFS object storage (default local storage) — DONE ✅ (code + live-verified 2026-09-12)

Owner decision: **SeaweedFS is the default local storage** (fs→s3), external
taxonomy at `/data_1/docker/compose_cep/seaweedfs-{master,volume,filer,s3}`,
started **first** (`up_seaweedfs-all.sh`), before overleafserver.

**Stack status (fixed this session):** root cause was a DNS-name bug —
containers are named `seaweedfs-*` but cross-refs used `master:9333` /
`filer:8888`. Fix: `overleaf-network.aliases` (`master`, `volume`, `filer`,
`s3`) added to all four compose.yaml. Volume now registers with master
(`added volume server 0: volume:8080`); all four Up. Also dropped the host
`8080:8080` volume mapping (host port occupied; volume only needs the
internal address). Ops doc: `compose_cep/SEAWEEDFS.md`.

**Go side (new this session, all unit + live green):**

| Piece | What it is |
|-------|-----------|
| `go/s3x` | zero-dep path-style S3 client (SeaweedFS gateway): Put/Head/Get/Delete/List (paged) / CreateBucket; anonymous by default; hex-md5 → base64 `Content-MD5` boundary translation (SeaweedFS enforces the AWS wire format with `BadDigest`); sentinels `ErrNoSuchKey`/`ErrNoSuchBucket` |
| filestore `s3xStore` | implements the same `Store` interface as `fseStore`; keys stored **verbatim** (`<project>/<path>`); `BACKEND=s3` + `OVERLEAF_FILESTORE_S3_ENDPOINT/KEY/SECRET` + the CE bucket names; buckets auto-created idempotently at startup |
| docstore `s3Archiver` | `Send/Get/DeleteDirectory` on slash keys `projectId/docId` — the *exact* key shape Node's `DocArchiveManager` writes for the s3 persistor, so fs→s3 and Node↔Go interop are unambiguous; `DeleteDirectory` = prefix sweep (Node `S3Persistor.deleteDirectory` parity) |
| `cmd/seaweed-migrate` | **fs ↔ seaweed conversion tool**: `health`, `list`, `to-seaweed` (flat→S3 keys; exact for `<pid>_<did>`, heuristic-splits otherwise and reports the count; `--keys` TSV for lossless filestore keys, `--dry-run`, Content-MD5 + ETag verification), `from-seaweed` (authoritative S3 keys → flat, `--verify` md5) |

**Live verification matrix (against the real stack, all OK, cleanup clean):**

* `seaweed-migrate health` → gateway 2xx
* fs file → `to-seaweed` (md5-verified PUT) → gateway GET `DATA_OK`
* → `from-seaweed --verify` → `ROUNDTRIP_OK` (byte-identical)
* Go filestore with `BACKEND=s3`: bucket auto-create, `/status` 200, file
  GET/HEAD via the gateway `DATA_OK`, missing key 404, non-GET on the
  GET-only bucket route → 404 (Node/Express parity, not Go-mux 405)
* gateway S3 protocol: bucket create/list/object PUT+GET/DELETE 200/204;
  object→nonexistent-bucket → `NoSuchBucket` (bucket must pre-exist)

**Flip (when cutover runs):** env on the overleafserver container
(`server-ce/config/env.sh`):

```sh
OVERLEAF_FILESTORE_BACKEND=s3            # filestore → SeaweedFS
OVERLEAF_FILESTORE_S3_ENDPOINT=http://seaweedfs-s3:8333
OVERLEAF_FILESTORE_TEMPLATE_FILES_BUCKET_NAME=filestore-template
OVERLEAF_HISTORY_PROJECT_BLOBS_BUCKET=filestore-blobs
OVERLEAF_HISTORY_BLOBS_BUCKET=filestore-global-blobs
BACKEND=s3                               # docstore archives → SeaweedFS
BUCKET_NAME=docstore-archive
AWS_S3_ENDPOINT=http://seaweedfs-s3:8333
```

Buckets are auto-created idempotently by both services at startup (unit +
live verified); no manual seeding. Rollback = revert two env lines to
`fs`/unset + migrate-back with `seaweed-migrate from-seaweed` (lossless).

## Phase B — the anti-surprise test layer (the important part)

**Strategy: test-first.** Write/extend the e2e spec against the **Node-active
stack first (today's state → green = the Node contract pinned in e2e form)**,
then flip `USE_GO_<SVC>` per service and rerun the same specs: green again ⇒
the Go replacement is behaviourally indistinguishable on that journey.

| # | Item | What it catches |
|---|------|-----------------|
| B1 | **`service-docstore-filestore-journey.e2e`**: create project (empty + template), open editor, edit + compile, history/version list, upload + download + rename + delete file, duplicate project, delete project. | full docstore + filestore state paths through the real web app (unit tests use in-memory stores; this uses real Mongo + real bucket) |
| B2 | **`service-linked-url.e2e`**: import-from-URL with a static fixture HTTP serving in the test stack → file lands in the tree; 400/404 contract on bad URL (no 5xx). | the only service with **zero** e2e coverage today |
| B3 | **`service-webdav-sync.e2e`**: local WebDAV server added to the e2e stack; import-from-WebDAV + mysettings link + a sync round trip; keeps `sync-graceful` (no-5xx) as the gate. | webdavinterface + datamanipulator on a *real* endpoint (only "status not 5xx" today) |
| B4 | **`service-chat-notifications.e2e`**: comment thread on a project (add/reply/resolve) → assert notification docs created **and** the email lands in the test-stack smtp sink. | Go chat's recipient fan-out + Go notifications pipeline together end-to-end (the 12-pref logic is the riskiest Go code in the fleet) |
| B5 | **Dropbox**: e2e-stack mock (Dropbox-API-shaped, plain HTTP) + `DROPBOX_API_BASE` pointing at it → real `/check` + `/list` + import journey through the web module. (Node side stays at `sync-graceful`; the Node client can't be re-based offline.) | dropboxinterface + webdavinterface-shaped client code on a real endpoint |
| B6 | **Differential (shadow) harness** in `tools/service-parity/`: same request fixtures at Node (in-image, canonical port) vs Go (host, `PORT`+shadow) against **isolated scratch project ids**; assert `(status, content-type, body-normalised)` equality per route. | env/semantics drift unit tests can't see — Mongo-driver shape rendering, persistor key layout, read-preference, body limits |
| B7 | **Env-parity audit** `tools/service-parity/env-diff.sh`: for each service, diff env names read in `services/<svc>/config/settings.defaults.cjs` (or `server-ce/config/settings.js` overrides) vs `go/services/<svc>/*` + `cmd/<svc>/main.go`; table out, zero unexplained rows. | the classic "env name drift" surprise, checked mechanically on every change |
| B8 | Github | **not e2e-testable offline** (github.com REST + git protocol). Pinned by the unit contract suite + a 1-page manual checklist in the cutover record (clone / commit / branch-head / can-push against a real account, one pass, pre-cutover). |

## Phase C — cutover, in risk order

Flip per service (each: flip → `sv status` → package `go test` → the service's
Phase-B spec(s) green → 30 min log soak for `panic|warn` → next):

1. **linked-url-proxy** — stateless outbound proxy, smallest surface
   ✔ **CUTOVER COMPLETE (2026-09-16)** — see record below.
2. **docstore** — already live-verified 30/30, most test-covered
3. **filestore** — heavy state, heavy e2e coverage (templates/files/history)
4. **notifications** — B4 journey is the gate
5. **chat** — B4 journey is the gate
6. **datamanipulator** — internal engine, exercised via B3/B5
7. **webdavinterface** — B3 journey is the gate
8. **dropboxinterface** — B5 mock journey is the gate
9. **githubinterface** — unit + manual checklist (B8), last

### Cutover records

**#1 linked-url-proxy — COMPLETE 2026-09-16.**
* Gate: `tests/e2e/specs/parity/service-linked-url.test.e2e.ts` — a 21-case
deterministic contract battery (`tests/e2e/parity/lup/battery.js`) pinned 1:1 from
Node sources (`LinkedUrlProxyController.mjs`, `strict-url-sanitise@0.0.1`,
`als-normalize-urlpath@2.3.0`, `libraries/fetch-utils`): health, exact Express
404 pages (`Cannot <METHOD> <url>`), missing-param 400, invalid-URL 500s with
exact `Invalid url to pass to open(): <raw>` messages (ftp/data/javascript/relative/
garbage), blocked-IP 403s (loopback/private/link-local), DNS-fail 421, upstream
200 byte round-trip, upstream 404 → `Error: request failed`, 302 follow + >5-hop
421, 413 too-large, 422 refused, 30s-timeout 408, >2000-char path 400.
* Result: **Node (e2e) 21/21 == Go (e2e) 21/21 == Go (live overleafserver) 21/21**;
`go test ./go/services/linked-url-proxy/` green (exact-message + Express-page +
ipaddr.js-unicast-set assertions). Pre-fix Go battery red: 405→404, `N protocol is
not allowed` 400→`Invalid url...` 500, `404 Not Found`→`request failed`, missing
2000-char gate, `ipaddr` CGNAT/broadcast nuances fixed.
* Live state: `overleafserver` running the repo `bin/go-services/linked-url-proxy`
(flag `/etc/overleaf/env.d/ollitex-gocutover.sh`); `bin/` is what the image bakes
(`server-ce/Dockerfile` COPY bin/), so the next image rebuild ships this binary.
Rollback: remove flag file + `sv restart linked-url-proxy-overleaf`.
* Test artifacts (loopback upstream :9991 + `OVERLEAF_LINKED_URL_ALLOWED_RESOURCES`
escape hatch) are provisioned per-run by the spec and torn down on the live box
after the live leg.

**Stack-wide gates:** full e2e suite green with *all* flags on (test stack);
then prod: `make release` → owner push + `cycle_overleafserver.sh` →
`USE_GO_*=true` for all nine → 17-point prod probe → 24 h soak.

**Rollback at any point:** unset the flag(s) + `sv restart <svc>-overleaf`.
No data, no client, no migration involved; in-flight state (archive locks)
self-resolves within the lock TTL (≈60 s).

## Risk register (what we explicitly guard against)

| # | Risk | Guard |
|---|------|-------|
| R1 | BSON/EJSON rendering differences between Node driver and Go driver | B6 differential + B1 journey |
| R2 | FS persistor key layout drift (flat vs subdirs) | unit-locked (flat) + B1/B3 + spot-check existing bucket files after first Go archive round trip |
| R3 | Env-name drift between Node config and Go main | B7 audit + per-service table in this file at flip time |
| R4 | Port/bind differences at runtime | A4 `PORT` + B5 smoke with flags off, then on; `ss -ltnp` check per flip |
| R5 | `setuser www-data` / volume ownership | unchanged in run script; Go process runs as the same user |
| R6 | Body-limit / 413 / 500-contract drift | unit-locked (12 MB / 100 kb / `Oops, something went wrong`) + B1/B2 |
| R7 | `CONVERTER` (filestore) binary availability in image | verify `CONVERTER` value + tool presence in image during A5 smoke |
| R8 | Log rotation / log file drift | same file names in run scripts; `logrotate.d/overleaf` is name-based |
| R9 | SeaweedFS gateway down ⇒ archive/blob reads fail (same class as fs disk failure; no degradation path) | stack starts seaweed first (`up_seaweedfs-all.sh`); `seaweed-migrate health` in the cutover record; buckets idempotently pre-created; fs fallback remains one env flip + `from-seaweed` |
| R10 | filestore keys with `_` in path segments mis-split during fs→s3 import | `to-seaweed --keys` (TSV from the `files` collection) is the lossless path; the heuristic mode *reports* every ambiguous filename; `from-seaweed` is always authoritative |

## Deliverables & sign-off

* [ ] Phase A complete (image ships `go-services/`, flags off = byte-identical behaviour)
* [ ] **Phase A2 seaweed: done — code + live matrix recorded above** (stack fixed, `seaweed-migrate` round trip, filestore/docstore `BACKEND=s3` verified)
* [ ] Phase B specs written **and green on the Node-active stack** (baseline)
* [ ] Phase C flips 1→9 green with per-service records (env table + evidence links)
* [ ] Full e2e + 17-point prod probe + 24 h soak recorded
* [ ] Node entrypoints kept in-tree (not removed) until one more release after cutover
