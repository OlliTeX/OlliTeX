# OlliTeX — State & Plan (single source of truth)

> **This is the current, living state+plan doc.** `WEB_GO_PLAN.md` is **frozen**
> (historical record only — do NOT append to it; it grew to ~210KB). Everything
> current lives here. Update this file as the durable state doc, not as a log.
>
> Last updated: 2026-09-26 (F1 flip — live browser E2E unblocked; D31–D37). Branch `main`. Repo root `module ollitex`, Go 1.27,
> yarn 4.18.0 (PnP). Live e2e stack = `ol-e2e-overleaf-1` (nginx :7420).

---

## 0. One-paragraph where-we-are

The Node→Go 1:1 port is complete for the **web** surface (Go `bin/web` on :4000
canonical, Go `bin/web-api` on :3000) and the P6/P7 cutover + build-system
rearchitecture (ubuntu:26.04) + P7-post items (1 SQLite config-DB, 2 GDPR
consent, 3 /hub email templates, 4 go-i18n evaluation) all landed. **S4 FLIP
F1 (IDE hard cut OT→Yjs client side) is now COMPLETE and live-verified**
(A1–A5 browser battery green on the fresh image, 2026-09-26 — see the F1
entry in §2 and D36/D37 above). Remaining flip work: F2 OT sweep + F3 e2e-
suite promotion + D28a bus-to-Go. Also in flight: turning the two remaining
Node microservices (**history-v1**, **document-updater**) into real Go
services (API/logic already ported+green in-repo; **storage/service
layers are the gap**) and the follow-on dependency-refresh arc (D27)..

---

## 1. Decision ledger (owner, 2026-09-25)

| # | Decision | Status |
|---|---|---|
| D1 | P7 **step 4: RETIRE the Node web** to junk/; adapt/remove dependent e2e tests | APPROVED |
| D2 | P7 **step 5b junk pages**: trash items 1–11 (see §5) + §5 extras; keep JSON APIs | APPROVED |
| D3 | **`/status`**: compose_cep healthcheck hits `GET/HEAD /` (NOT `/status`/`/health`) → **remove `/status`** | APPROVED (verified) |
| D4 | **history-v1 + document-updater**: build on the **real** stack backends — **Mongo + S3/TPDS (SeaweedFS) + Redis** (not hermetic in-memory) | APPROVED (A) |
| D5 | **Postgres**: owner asked "should we add it, what's the benefit?" → **recommendation: NO, keep Mongo** (see §8 Q1). *pending final confirm* | RECOMMENDED |
| D6 | **Config-DB = single source of truth** for site-wide settings (retire the Mongo `site_settings` duality); **de-couple + remove the old docker env params** that move into SQLite; **surface the unsurfaced site-level params** in /hub; field-level **AES-256-GCM** encryption keyed by boot env | APPROVED (B) |
| D7 | **go-i18n**: ADOPT `nicksnyder/go-i18n` "as much as possible" (incl. its pre-translated DB). First slice = `go/libraries/i18n` + one e-mail slot German canary | APPROVED |
| D8 | **Registry rename** `sharelatex/*`→`ollitex/*` push **AND** `/overleaf`→`/ollitex` route rename — one green slice | APPROVED |
| D9 | **Image re-bake + stack cycle + registry/external push** — authorized "when ready" (owner-gated) | APPROVED |
| D10 | **P7 step 7**: `frontend/**` README tour | APPROVED |
| D11 | **`server-ce` → `build-images`** rename + update `compose_cep/overleafserver/compose.yaml` | APPROVED |
| D12 | **SeaweedFS services → toolkit** + update image names | APPROVED |
| D13 | **Storybook**: expose as much web UI as possible | APPROVED |
| D14 | **git-bridge**: confirmed already Go (`gitbridge-go:latest`). **DELETE `services/git-bridge`** (Java) + clean the Java `build-git-bridge` Makefile target | **DONE 2026-09-26**: 564 tracked files deleted; Makefile Java target + GIT_BRIDGE vars removed; develop/ + server-ce/test/ compose re-pointed to the Go image (ollitex/git-bridge:latest, `git_bridge serve /conf/runtime.json` + runtime.json mounts; Java rollback commented). All compose configs validate. |
| D15 | **Procedural**: stop appending to `WEB_GO_PLAN.md`; maintain this file as the state doc | APPROVED |
| D16 | **`services/clsi`, `clsi_typst`, `project-history`, `real-time`** are being converted to Go **by support LLMs** (parallel; "ready soon") — do NOT re-port; integrate + audit when they land | AWARE (no action) |
| D17 | **Notifications email-dispatch cron → Go** (kills last `modules/notifications/app` runtime dependency) | APPROVED — **DONE** (ARC-8 + ARC-8a severance; zero Node runtime hooks left in services/web) |
| D18 | **Toolkit placement**: `tools/toolkit` → repo-root **`toolkit/`** (visible root position), all references updated | APPROVED — **DONE this commit** |
| D19 | **Collaboration pivot — Option B (Yjs/Ygo, HARD CUT)**: the document model becomes Yjs; server = **ygo** (`github.com/reearth/ygo`, Re:Earth's pure-Go CRDT stack — Hocuspocus-compatible WS server, Redis-Streams cluster relay, versioned persistence + snapshots, awareness; MIT; v1.50.0 pinned). The 2014 substrate (real-time Node + socket.io fork + ShareJS + OT client/editor-core) is **retired junk after the flip — no OT legacy reader** (owner hard-cut call 2026-09-25). Support-LLM real-time port: irrelevant (their own copy). Existing OT history is **not portable** — new projects seed Y.Text from current file content; OT history drops. | APPROVED — **IN PROGRESS (ARC-9)** |
| D16* | `clsi`/`clsi_typst`/`project-history` still support-LLM territory; **`real-time` is SUPERSEDED** by D19 (pivot, not port) | SUPERSEDED (real-time) |
| D20 | **Collab client stack**: browser `yjs` (stable v13 line) + `y-websocket` + `y-indexeddb` + `y-undo`; CodeMirror-6 bridge is OUR code (full-replace sync loop; y-cursor is CM5-only → awareness-rendered cursors via a CM6 decoration plugin). **yhub = REST DESIGN SPEC ONLY (history/changeset/restore shape), never a runtime** (AGPL/beta + Postgres + Redis Streams = D5/D19 conflict). `k_yrs_go`/`electric` investigated and **rejected** (wrong substrate). Client code MIT-compatible with the AGPL product. | CONFIRMED (owner 2026-09-25) |
| D21 | **Notifications**: email stack stays **`wneessen/go-mail`** (owner-approved P3; stronger client than `go-pkgz/notify`'s SMTP — adopting notify as a mailer REJECTED; its multi-channel shape is only a design reference). Non-email delivery (webhook/Slack/in-app presence) = separate arc under owner-delegated discretion (2026-09-25 "make the priority decisions yourself"); the Yjs awareness stack already provides the real-time presence half. | RECORDED |
| D22 | **Observability modernization (owner note 2026-09-25)**: rework `features/instancestats` (Mongo time-series + email-threshold alerts) onto the **Prometheus + Grafana** ecosystem: Go services expose Prometheus-format `/metrics` (client_golang; the existing `go/libraries/ometrics` registry becomes a bridge/collector), node_exporter (host) + cAdvisor (containers) scrapes, Prometheus sidecar (runit/compose, OFF-BY-DEFAULT to keep clean-by-default images), Grafana embedded in /hub (kiosk iframe, Mantine `GrafanaPanel`), admin email alerts re-implemented as Prometheus rule alerts → webhook → the existing email pipeline. Loki/Alloy (logs) = optional phase. Single-host stack (NO k8s) → static scrape configs, no ServiceMonitor/K8s auth bits. Phased: A instrument → B Prometheus → C Grafana/hub → D alerts (product behavior kept) → E optional Loki → legacy series retirement. **Sized after the D19 flip (S4) so it measures the live Go stack.** | RECORDED — PLANNED (owner note for later) |
| D23 | **Config-DB consumer contract (this commit)**: new shared library `go/libraries/configres` — ONE file-path contract (`$CONFIG_DB_PATH → $OVERLEAF_HOME/configdb/configdb.sqlite3 → ./configdb/configdb.sqlite3`, the same file /hub + `cmd/configdb` + toolkit manage) and ONE precedence chain **config-DB → legacy env → static default** for every service consumer. `Open()` reads only and never creates the file (pre-config-DB deployments stay bit-identical — additive-by-default). Precedence rule: first USABLE value wins; unparseable/empty fall through (a /hub typo can't break a service boot); explicit zero IS a value (pinned: `COLLAB_KEEP_VERSIONS=0` = keep-all). First consumer: `cmd/collab` resolves `COLLAB_KEEP_VERSIONS`/`COLLAB_COMPACT_EVERY` through it (registered in the `configschema` registry + `defaults.jsonc` lockstep → visible in /hub admin + operator CLI; int type-checked on PUT). Binding = service start (like every boot parameter); registry descriptions state it. This is the D6 "single source of truth" consumer side for non-web services — extensible to any future service knob without re-deriving the contract. | DONE (goal item 2) |
| D24 | **Yjs client sync design (S3 client engine)**: the D20 goal's "full-replacement sync" is EXPLICITLY REFINED (decision, not drift) — tested against yjs 13.6.32 in the engine's test suite, full-replace in BOTH directions merges CONCURRENT whole-document edits to a DUPLICATED document (X+X — the failure D19 forbids for seeding). D24: **LOCAL direction = granular delta ops** (host hands the editor's change spans in old-space coordinates — exactly `CodeMirror.iterChanges`; replay right-to-left, delete-then-insert per span, ONE transaction = one undo entry) + **REMOTE direction = full mirror** (re-derive the host from the Y.Text; zero index math remote-side). Safety model: Yjs fires type observers SYNCHRONOUSLY within the applying task (probe-verified); CM6 dispatches run in their own tasks ⇒ invariant `after every task host === Y.Text` ⇒ spans are valid Y coordinates ⇒ peers converge; re-entrancy guards close the in-task loop. Accepted trade-off (documented + test-pinned): two concurrent whole-document rewrites interleave both texts (any text CRDT); convergence + no-loss are invariants. **Undo split:** CM6 `history()` owns user-facing undo (remote mirrors dispatch `userEvent:'input.remote'`); the engine's y-undo is the programmatic seam tracking LOCAL-origin only (yjs default `trackedOrigins={null}` makes explicit tracking mandatory — discovered + pinned by test). **Browser gate:** provider attachment gates on the browser `window` (modern Node exposes a native WebSocket — gating on the substrate would misfire), so the engine is headless importable/testable. Engine = `frontend/js/features/ide-react/collab` (ydoc/sync/providers/codemirror/engine + 14-test Node suite in the frontend workspace); S4 wiring stays mechanical (README snippet). | DONE (S3 client engine core) |
| D25 | **S4 flip scope + ARC-1/ARC-2 shrink verdict (2026-09-25)**: after live verification of the Yjs stack, the flip retires (→ `junk/`): `services/real-time` (Node/socket.io :3026 OT sync); the client OT transport (`ide-react/connection/*` socket.io managers, `editor/share-js-doc.ts`, `share-js-history-ot-type.ts`, the OT path of `editor/document-container.ts`, `editor/offline-doc-backup.ts` (replaced by `y-indexeddb`), `editor-watchdog-manager.ts` (replaced by the engine reconnect), the vendored `sharejs.js`, the OT half of `source-editor/extensions/realtime.ts`); and `go/services/web/features/history` (the OT-history REST proxy — replaced by `features/collabhistory`). **`history-v1` STAYS for now**: it remains the blob/content store (the web file-proxy + project create/clone/upload all pin `{v1_history}/…/blobs/…` — the same path the web file-proxy + create/clone/upload read); its OT-history API loses all consumers at the flip, and a follow-up arc (**ARC-1b**) migrates blob ownership to Go (reusing `go/libraries/persistors`) before `history-v1` itself retires. **`document-updater`**: the OT core (updatemanager / HistoryManager OT path, OT historyconversions, OT diffcodec) retires with the substrate. **COMMENTS/TRACK-CHANGES** are OT-document-native (`HistoryOTShareDoc` snapshot/ranges/accept-reject) and do NOT survive the OT retirement. Disposition (safe default, per the autonomy directive): at the flip the editor runs the Yjs text engine and the comments/track-changes UI is DISABLED with an honest "unavailable on the Yjs engine" placeholder (NOT silently deleted — code stays in git; the feature is re-attachable); a Y.Doc-native port (Y.Map/Y.Array in the same room doc) or a permanent drop is an **OWNER PRODUCT DECISION** flagged here — no silent product loss, no silent scope crawl. | DECIDED (flip scope) — comments/track-changes: owner decision pending |
| D26 | **S4 LIVE AUDIT (2026-09-26) — three production bugs caught & fixed by live verification; server-side contract now fully verified end-to-end.** (1) **Session gate** (`mongo.go`): the production `RedisSessionDoc` compared the RAW cookie value — but the wire value is `percent-encode("s:" + sign(sid, secret))` (cookie-signature HMAC-SHA256). The web decodes (%3A→:), strips `s:`, and UN-signs against `OVERLEAF_SESSION_SECRET‖CRYPTO_RANDOM (+*UPCOMING/*FALLBACK)` before the `sess:<sid>` lookup (core/session.go). The collab gate now mirrors that exact pipeline (`sessionSid` + `signCookie`/`unsignCookie`, timing-safe) — previously EVERY real browser session 401'd at the gate. (2) **Seed source** (`seedsource.go`): reworked from the guessed v1-history blob path to the EDITOR's actual oracle — projects doc `rootDoc_id` → docstore `GET {WEB_DOCSTORE_URL}/project/{pid}/doc/{did}` → `join(lines,"\n")` (live-verified: a project created via POST /project/new has rootFolder EMPTY and its content in the docstore revision-0 doc; env default `http://127.0.0.1:3016`). No rootDoc / 404 = empty seed; 5xx/transport = fail-closed. (3) **Store wiring** (`cmd/collab/main.go`): the service NEVER passed `Options.Store` → it fell back to FilePersistence on a root-owned dir www-data cannot write → every room load 500'd ("room unavailable"). Now ONE Mongo client backs auth + seed + `MongoStore` (single connection; `NewMongoFrom`). Ops surface added: `Options.Logger` (slog; `COLLAB_LOG_LEVEL=debug`) + lifecycle observers (`first-peer`/`last-peer`/`unload-doc` logs) — the first D22 real-time signals. **EVIDENCE (live e2e, ol-e2e stack via nginx `/collab`):** login → project create → WS `/collab/<pid>` seeded with the rendered template (222 B) → 2nd peer converges identical → peer-A live edit RELAYED to peer-B → REST `GET /collab/history` versions [2,1] → `GET /history/1` = seed → `POST /history/1/restore` → v3 → doc head = seed → fresh WS peer converges post-restore → anon 401. Hermetic pin added: `TestRelay_TwoPeersEditAndPersist` (2 peers, relay + persistence, `go test -race` green). NOTE for the S4 client: ygo wire facts (client need NOT answer its own step-1; server sends step1+step2+awareness up front; awareness frames MUST be consumed by clients; Text handles grabbed OUTSIDE `Transact` — doc locks are not reentrant; `ApplySyncMessage` takes the FULL sync message) — y-websocket clients handle all of this natively, the probe pitfalls were hand-frame artifacts. | DONE (verified live; commit `…`) |
| D27 | **Dependency‑modernization verdict (owner note 2026-09-26)**: the support‑LLM hints are directionally sound GO‑dependency hygiene, so ADOPT the genuinely good items as ONE self‑contained "dependency‑refresh" arc scheduled AFTER the S4 IDE flip (green‑slice invariant — not interleaved with the live cut): (1) `mongo-driver v1.17.10→v2.x` — the real "biggest win," but it touches all 146 import sites and the collab service was LIVE‑verified on v1.17.10 this session; keep it the dedicated D22b sub‑arc (atomic bump + full e2e re‑verify + re‑bake). (2) `dsnet/compress bzip2 → klauspost/compress bzip2` in `go/services/gitbridge/repo/{store,tar}.go` — the one genuine maintenance‑risk catch (dsnet is a frozen 2023 pre‑1.0 pseudo‑version on a LIVE bzip2 code path; the drop‑in replacement is already in the graph as an indirect dep and mirrors the stdlib API) — a focused 2‑file + gitbridge round‑trip‑test‑green slice. (3) same‑major low‑risk bumps folded into the refresh: `go-redis v9.18→v9.22`, `aws s3 v1.113.1→v1.113.4`, `x/crypto v0.54→v0.57`. No action on "add" items already covered/optional: `prometheus client` = the decided D22 arc; `x/time/rate`, `x/net`, `go-cmp`, `testcontainers`, `loki` are already present via third parties or not needed under the 1:1 parity policy. **REJECTED for now** (violate the 1:1 drop‑in / oracle‑pinned invariants if done mid‑endgame, and churn codebases that are being retired or are already migrated): `webpack/Babel/ts-loader→Vite/turbopack` (the build pipeline is oracle‑pinned to `make all` + baked views + PnP + the canvas/pandoc layer contract — a toolchain swap is a large standalone project, not a fix); `express→hono/fastify` (the Go web port is the end state; the Node side stays frozen per parity — do not churning a retiring codebase); `moment→dayjs/date‑fns` and `lodash→es‑toolkit` (legitimate long‑term, but 100+ file churn with parity‑of‑semantics risk — defer to a post‑endgame frontend arc, only if at all); `jQuery/backbone removal` (largely already done in the React/Mantine migration — residue folds into the frontend consolidation arc). | DECIDED (adopt‑good / reject‑bad, with rationale) |
| D28 | **S4 bus re-scope (2026-09-26, client-flip recon)**: mapping the client socket consumers before the flip surfaced that `services/real-time` is NOT just the OT text sync — it is ALSO the app EVENT BUS (socket.io endpoint for the browser): join/leave project rooms + joinProjectResponse, online-user presence, file-tree pushes (reciveNewFile/reciveNewDoc/removeEntity/reciveEntityRename/reciveEntityMove), project-wide settings (spellCheckLanguageUpdated, referenceFormatUpdated, imageNameUpdated, grammarPickyUpdated, compilerUpdated), references (references:keys:updated), chat relay, socket diagnostics. Architecture: the Go web server is the PUBLISHER (e.g. entEmitEvent to Redis channel editor-events); the Node real-time service SUBSCRIBES to that channel and relays over socket.io to browsers. **Scope call**: the S4 hard cut retires the OT TEXT SYNC (ShareJS/ops/ack/updatemanager/document-updater OT core — the D19/D25 substance) but the event bus (presence, file-tree, settings, references — none of it OT) STAYS on the real-time relay for now (it is socket.io plumbing, not the OT substrate — D25 refined to real-time-OT-core-retires, bus-stays-until-a-Go-socket.io-endpoint-exists). The full bus-to-Go move (engine.io/socket.io endpoint in the Go web server over gorilla ws, reusing the collab service ws machinery) is a dedicated follow-up arc (D28a) — owner endgame direction unchanged (zero Node on the web path), just sequenced: (F1) text hard cut to Yjs [this slice], (F2) client OT file sweep + Go OT-history proxy retirement, (D28a) bus to Go + real-time retired, (F3) e2e + parity + commit. | DECIDED (bus re-scope, sequencing) |
| D29 | **A2 RE-BAKE RECIPE (2026-09-26)**: baked view rebake = per-page asset/CSS/script blocks regenerated against the canonical `public/manifest.json` (A2: 74 assets, 0 misses); the A2 re-bake commit is the reference recipe for any future per-image asset rebake. | DONE |
| D30 | **OPS (2026-09-26)**: never assume `ollitex/ollitex:main` is healthy right after a rebuild without asset verification (a stale baked asset ref broke psintern login); restore/re-verify the served image before declaring a flip shipped. | DECIDED |
| D31 | **GENERATION INTEGRITY (2026-09-26)**: webpack persistent caches (docker `--mount=type=cache`) mix local+image module state — a replay can restore an OLD-generation entry chunk whose `e.O(0,[ids])` runtime chunk ids do not exist in the freshly-built runtime → the entry chunk never executes → SILENT dead boot (login AND editor), with every asset serving 200. Fix in `server-ce/Dockerfile`: `rm -rf` the stale copied webpack output (`public/{js,minjs,stylesheets,manifest.json}`) before compiling, and mount webpack/babel caches as **tmpfs** (a plain `rm` on the mount point fails "Device or resource busy"). Every image is now exactly ONE self-consistent webpack generation. | DONE (verified: login+hub+IDE boot on fresh image) |
| D32 | **SHARED CHUNKS = MANDATORY PRELOADS, serve-time resolved (2026-09-26)**: numeric webpack shared chunks (e.g. 1772/7189/101) are ABSENT from `manifest.json` stable keys and the entry `e.O(0,[ids])` footer REQUIRES them preloaded (the runtime will not self-load static chunks). Resolve at SERVE time via `core.SharedTags(entryKey, kind)`: read the in-image entry file's `e.O` id list, resolve each id from the `public/js`+`public/stylesheets` dir listing (`^<id>-[a-f0-9]{10,}.js?$`), emit nonce-bearing `<script>`/`<link>` tags. Templates carry `\x01SHARED:JS:<entry>\x02` / `\x01SHARED:CSS:<entry>\x02` (raw-string variants `__SHARED:*__`), expanded by `resolveAssetSlots` before nonce substitution. Fully hash-independent + generation-safe. Pin: `go/services/web/views/assetfixture_test.go` (+ `testdata/assetfix/`). | DONE |
| D33 | **y-websocket URL composition (2026-09-26)**: y-websocket 3.x builds the WS URL as `serverUrl + '/' + roomname`; a room name WITH a leading slash double-slashes (`//collab/…`) and is mis-routed. The room must be relative: `collab/<projectId>` (`frontend/js/features/ide-react/collab/providers.ts`; pin tests updated). | DONE |
| D34 | **Manifest unmarshal (2026-09-26)**: `public/manifest.json` carries NON-STRING values (the `entrypoints` object) — `json.Unmarshal` into `map[string]string` silently rejects the WHOLE map → empty manifest → `\x01ASSET:*\x02` tokens render literally → CSP `ERR_UNKNOWN_URL_SCHEME` on boot. Must unmarshal to `map[string]json.RawMessage` and keep string values only (`core.AssetManifest`, `go/services/web/core/app.go`). | DONE |
| D35 | **ygo v1.50.0 auth/origin semantics (2026-09-26)**: `Authorize`/`AuthFunc` false → **401 "unauthorized"** at the WS handshake; `AllowedOrigins` empty (= same-origin fallback: Origin host must equal the request Host); clients omitting Origin are always allowed (non-browser). Cross-origin without an explicit `COLLAB_ALLOWED_ORIGINS` allow-list → 403 (gorilla `Upgrader.CheckOrigin` rejection, body "Forbidden"). Pin: `go/services/collab/origin_check_test.go`. | DONE |
| D36 | **nginx /collab Host header (2026-09-26) — the F1 browser-WS 403 root cause**: nginx's `$host` variable STRIPS the request port, so the collab service saw Host=`127.0.0.1` while the browser sent `Origin: http://127.0.0.1:7420` → ygo's same-origin check (D35) rejected EVERY browser WS handshake (403 "Forbidden"), while cookie-less/no-Origin probes looked normal. Fix in `server-ce/nginx/overleaf.conf.template` `/collab` block: `proxy_set_header Host $http_host` (raw Host header WITH port). Verified live: browser WS → 101 + y-protocols SYNC_STEP1/STEP2 + seeded content frame; `TestOriginGateMatrix` pins the matrix. | DONE (verified live) |
| D37 | **y-websocket 3.x provider API (2026-09-26) — the F1 "could not reach the collaboration service" root cause**: the 3.x `WebsocketProvider` exposes live state via EVENTS only — `status` → `{status:'connecting'|'connected'|'disconnected'}` (emitted on socket open) and `synced` → boolean — there is NO `.status` property (the 2.x-shaped read my `document-container.ts` join-wait used always saw `undefined` → 5 s deadline → fail-closed error, even though the frames were arriving and applying). `waitFirstSync` now subscribes to both events, with raw `ws.ws.readyState === 1` as a fallback signal and `text.length>0 || synced || settle-time` as the settled condition. Gates: tsc 586 baseline, vitest 14/14. | DONE (gates green) |

---

## 2. Committed & green (this arc)

- Go web hard-cutover, git-bridge Go, Dockerfile clean, shared-stack web→Go,
  build-system ubuntu:26.04 rearch + A2 re-bake (all prior, committed).
- **P7-post item 2 (GDPR consent)**: `6e8ca411f5` / `76c481361c` / `52f9f25ce8`
  — Go `/cookie-consent` GET/POST + `/legal` + frontend `setConsent` wiring +
  http/Secure cookie fix. 8/8 e2e.
- **P7-post item 3 (/hub email templates)**:
  - `a24ae3f9b2` registry + admin API (13 slots, `{{var}}` renderer, Mongo
    `emailtemplateoverrides` store, `GET/PUT/DELETE /api/hub/email-templates`).
  - `530ac20200` all 11 mail call-sites rewired through `RenderFor`; 13-slot
    byte-parity pins; two real Mongo store bugs fixed (UpdateOne+$set+upsert;
    flat-doc decode). Live mail-loop proven (override → reset).
  - `fe16899d75` /hub UI section (Site settings→General→"Email templates"),
    per-slot Save + Reset-to-default, `tests/e2e/specs/email-templates.test.e2e.ts`
    4/4.
- **P7-post item 4 (go-i18n eval)**: `dc01edab2c` — `docs/go-i18n-evaluation.md`
  (recommendation was defer; **superseded by D7: adopt nicksnyder/go-i18n**).
- **Two microservice modules integrated + audited**: `7a64433001` —
  - `services/history-v1.go/` (module `history-v1`, 153 test fns, gate green
    `make go-test-history-v1`): faithful API layer over a **fake in-memory
    storage**; clone/zip/blob-PUT = 501; flush/expire no-op.
  - `services/document-updater.go/` (module `document-updater`, 215 test fns,
    gate green `make go-test-document-updater`): internal logic core (ShareJS OT
    types + sharejs model + both update managers + rangesmanager + redis facade);
    **no HTTP controller, no main**.
  - `GO_SERVICES_INTEGRATION.md` = the audit record (verified-vs-claimed + flip
    gates). Both Makefile gate targets added.
- **P7 step-4 (module Node backends retired)** — this turn:
  - 203 `services/web/modules/**` Node-backend files deleted: 21 module `app/`
    trees, 26 `index.*` boot stubs (incl. `sandboxed-compiles` whole module),
    21 dead node-app test files, 3 non-workspace `package.json`.
    **(2026-09-25 update)** the dispatch cron is now the Go `cronmail`
    binary (ARC-8); `modules/notifications/app` + `scripts/process_notifications.mjs`
    are superseded — junk-sweep candidates once the rebuilt image is live.
    **Kept alive (verified live):** `notifications` app+tests (cron chain via
    `server-ce/cron/notification-email-dispatch.sh` →
    `scripts/process_notifications.mjs`), `server-ce-scripts` (grunt/ops
    tooling), `authentication/{ldap,saml,oidc}` (D1 scope), ALL module
    frontends (webpack/CI/settings entries), live module `types/` dirs
    (admin-tools, git-bridge, template-gallery — imported **relatively** by
    live frontends; my first import-recon was blind to relative paths and
    nearly deleted them — caught by tsc A/B), `template-gallery/
    app/src/CleanHtml.mjs` (single live leaf import).
  - Node web defanged (shadow runtime, D1-retires): `moduleImportSequence: []`
    (settings.defaults.js), `getHubTheme`/`readAdminSettings` local stubs
    (ProjectController.mjs / ExpressLocals.mjs).
  - `types/backend/express/request.d.ts`: added `user?: User` to the Request
    augmentation (documented middleware property, never typed anywhere;
    surfaced 3× in PermissionsController once baseline parse quirks went away
    — real fix, backend tsc 96→94, **0 new**).
  - Gates (Node 24.21.0, owner-installed nvm v24): **frontend tsc 586=586
    identical set**; backend tsc 94 < 96, 0 new; CI-exact vitest A/B =
    **identical failing set** (17 pre-existing local-env `@`-alias failures,
    present at clean baseline too — not a regression; host Node 22 vs 24 both
    fail identically); `go build` exit 0; webpack inputs net-unchanged
    (frontends untouched, restorations byte-identical to HEAD).
  - **Open (owner note):** the 17 vitest `@/…` alias failures exist at clean
    baseline on this host — pre-dates this arc; CI may differ.
- **Owner decisions (2026-09-25, this turn):**
  - **`services/clsi`, `services/clsi_typst`, `services/project-history`,
    `services/real-time` are under Go conversion by SUPPORT LLMS** (in
    parallel; "ready soon"). Do NOT re-port them; do NOT block ARC work on
    them; when they land, integrate+audit them the same way as
    history-v1/document-updater (GO_SERVICES_INTEGRATION pattern).
  - **Notifications email-dispatch cron: port to Go** (kills the last
    `modules/notifications/app` runtime dependency; unlocks junk-ifying the
    remainder of `services/web`). — **DONE this commit** (ARC-8).
  - **Toolkit move: `tools/toolkit` → repo-root `toolkit/`** (visible root
    position) — update all references (REPO_ROOT in `bin/config`, docs,
    compose comments, Dockerfile-base comment). — **DONE this commit**
    (17 refs updated; host-route + container-route both verified).

---

## 3. Verified facts (evidence, 2026-09-25)

- **compose_cep overleafserver healthcheck** = `curl -fsI http://localhost`
  → a `HEAD /`. It does **not** use `/status` or `/health`. ⇒ `/status` is safe
  to remove (D3).
- **git-bridge running impl = Go**: `compose_cep/git-bridge/compose.yaml`
  `image: gitbridge-go:latest`, `command: ["git_bridge","serve","/conf/runtime.json"]`
  (rollback line references the old Java `compose.yaml.java.orig`). ⇒
  `services/git-bridge` (Java, `pom.xml`) is legacy → delete (D14).
  - **Leftover to clean (D14)**: `server-ce/Makefile:90 build-git-bridge` built
    the **Java** image from `services/git-bridge/Dockerfile` (SUPERSEDED —
    D14 executed 2026-09-26: tree deleted, Makefile target + vars removed,
    develop/ + server-ce/test/ re-pointed to the Go bridge).
- **`/hub` settings are dual-sourced today:**
  - **Mongo `site_settings` collection** (backing nearly all site sections;
    seeded from env via `go/services/web/features/sitesettings/seeds.go`) —
    /hub-editable.
  - **SQLite config-DB** (`go/libraries/configstore` + `core/configdb_override.go`
    + `/api/hub/config` + `cmd/configdb`) — **only 3 curated keys**
    (`AppName`, `SiteURL`, `CacheStaticAssets`).
  - `seeds.go` is the authoritative **env→/hub-seeded** map (email creds, zotero/
    mendeley/github/webdav/dropbox OAuth secrets, sandboxed docker images,
    git-bridge host/port, typst/pandoc, misc limits, etc.) — the exact set D6
    must migrate into the SQLite single source + surface.
- **Go `persistors.S3Persistor`** (`go/libraries/persistors/s3persistor.go`) has
  the full object API (`GetObjectStream/Size/Md5`, `PutObject`…) over the same
  S3/TPDS backend ⇒ the history-v1 blob store can build on it (D4).
- This stack has **Mongo + Redis + SeaweedFS(S3/TPDS)**; **no Postgres
  container** (grep of compose/e2e infra = 0).

---

## 4. In flight / next (working order)

### ARC-1 · history-v1 → real service (D4/D5) — START HERE
Gate per slice: `make go-test-history-v1` + live A/B vs Node oracle.

1. **Real blob store** on `persistors.S3Persistor` (S3/TPDS backend) —
   replaces the in-memory fake; un-blocks blob PUT/GET/HEAD/copy. Wire
   `core.BlobStoreI`.
2. **Mongo chunk store** (target = Mongo per D5, not Postgres) —
   initialize/create/update/load@version/load@timestamp/changes-since/clone/
   delete, mirroring Node `chunk_store/mongo.js`.
3. **Redis persist-buffer** → real `flush`/`expire`/`queueChanges`.
4. **Zip streaming** (`archive/zip`) for `getLatestZip`/`getZip` + `createZip`,
   and **`cloneProject`** — replacing the 501s.
5. **Live A/B**: Go-web's V1 client (`:3100`) green against the Go service +
   full e2e; then flip runit `history-v1-overleaf` to the Go binary (owner-gated
   stack cycle).

### ARC-2 · document-updater → real service (D4)
6. Remaining managers over the already-green core (DocumentManager,
   ProjectManager, PersistenceManager, HistoryManager, DispatchManager,
   SnapshotManager, DeleteQueueManager, ProjectLockManager, WebApiManager).
7. Mongo + Redis + queue wiring (real Redis client behind the existing
   `redismanager` facade; Mongo adapters; the Bull/queue loop).
8. **HTTP controller** (Node `HttpController.js` ~880 LOC) + **`main`**.
9. **Live A/B**: real-time → Go-updater editor-save e2e + track-changes/ranges;
   then flip runit `document-updater-overleaf` (owner-gated).

### ARC-3 · Config-DB single source of truth (D6)
**Slice A ✅ (this doc's commit):** encryption + registry + CLI + toolkit all
green.

- **(done) AES-256-GCM field encryption** in `configstore` (`crypto.go`): key =
  `CONFIG_DB_ENCRYPTION_KEY` (boot env, hex-64/base64-44, never stored in the
  DB); values written under a key are stored as `enc:v1:<b64(nonce||ct||tag)>`;
  legacy plaintext rows still read (pass-through), re-set encrypts; tamper &
  wrong-key = hard errors. 7 new tests.
- **(done) Full registry** = single source for the key space:
  `go/libraries/configschema` — **149 params** across 11 groups (core/boot/
  services/email/integrations/compilation/limits/security/test, …), each typed
  (string/bool/int) + `[secret]` flag + default + description. Uniqueness/
  shape tests gate it.
- **(done) CLI extended** (`cmd/configdb`): `list --all` (registry table),
  `get --reveal` (registered secrets **masked** by default), `set` type-checked
  against the registry, `import-env FILE` (bootstrap from a KEY=VALUE file;
  unknown keys skipped), `init` (generates + prints the encryption key when
  absent; seeds from the process env without clobbering), `doctor`
  (key/db/read-back health). 5 new tests; old contract tests untouched
  (green).
- **(done) Toolkit emergency path** (`toolkit/bin/config`): drives the
  same CLI **with no web service / /hub required**; routes to the running
  OlliTeX container (`OLLITEX_CONTAINER` or image auto-detect) or falls back
  to the host `go run ./cmd/configdb`; end-to-end verified (init → import-env
  → masked get → doctor, clean rc's). NOTE: a re-bake (ARC-6/D9) is required
  before the container route has the new binary.
- **(done) Defaults JSONC — initial setup seed** (owner request, 2026-09-25):
  `go/libraries/configschema/defaults.jsonc` (embedded via go:embed; the
  operator's commented view of all 149 keys — `// comment` + `/* */`
  + trailing commas parse; string content is comment-safe) is the single seed
  source. `configdb import-defaults [FILE]` / `defaults` + `init` now do
  key → env seed → defaults seed in one shot; **never clobbers** an existing
  DB value; typed defaults (bool/int/string) verified against the registry
  by test. Toolkit `bin/config` documents it.
- Remaining (slices B–D):
  10. **Web wiring**: `core.LoadConfig` + `/api/hub/config` honor the registry
      (DB value > env > default) group by group (email first), so the web
      process actually reads the migrated keys from SQLite.
12. **Retire the Mongo `site_settings` duality** — single SQLite source; /hub
    sections read/write the SQLite store.
13. **Surface the unsurfaced site-level params** as new /hub admin field cards
    (ADMIN_EMAIL, NAV_HIDE_POWERED_BY, ROBOTS_NOINDEX, OVERLEAF_HISTORY_RESTORE,
    ENABLE_PDF_CACHING, ENABLE_PYTHON_RUNNER, ALLOWED_ORIGINS, cookie session
    length, …).
14. **Publish the "boot env" list** (the minimal env needed to boot /hub so the
    rest is editable in /hub) — see §8 Q2.

### ARC-8 · Notifications email-dispatch cron → Go (D17) — **DONE (this commit)**
The node dispatch chain (`server-ce/cron/notification-email-dispatch.sh` →
`scripts/process_notifications.mjs` → `modules/notifications/app/src/
ProcessNotifications.mjs`) is replaced by a byte-exact Go service:

- **`go/services/cronmail`** — package:
  - **Templates** (`render.go` + `oraclebase_go.go`): the two live
    `emailNotifications` types — `projectNotification` (chat comments) +
    `trackedChangesNotification` (legacy scheduler) — rendered from
    **oracle-pinned base bytes** (generated from the real Node
    `EmailBuilder` output; `tmp-cronmail-oracle.mjs`) with exact-value
    splice of the dynamic slots (title, schema.org action JSON, CTA URL,
    message, logo). `& < > \u2028 \u2029` script-safety per
    `StringHelper.stringifyJsonForScript`; `escape` = lodash-_.escape order
    (& first); `cleanEntityText` = oracle-pinned sanitize of the
    already-escaped template domain.
  - **Protocol 1:1** (`process.go` + `store.go`): node claim filter
    (due + {never-processed / retryable / legacy-retryable(`type` key) /
    stale-in-progress(>1h); not `dead`) with atomic `processing` claim;
    `attempts++` on failure; dead-letter at `OVERLEAF_NOTIFICATIONS_MAX_ATTEMPTS`
    (default 3); exponential backoff `2^att * (OVERLEAF_NOTIFICATION_SILENCE_PERIOD_MS
    || 2h)`; dry-run (`OVERLEAF_NOTIFICATIONS_DRY_RUN=true`) claims, renders,
    does NOT send, releases the claim at end of run (node `_releaseForDryRun`);
    `PROCESS_NOTIFICATIONS_BATCH_SIZE` (100) cap; `to` resolved from
    `toUserId`/`fromUserId` via `users` (node
    `EmailNotificationUtils.getRecipicentIdOrUserEmail`); send via the shared
    `core.Mail` seam (SMTP settings identical to the node EmailSender).
- **`cmd/cronmail`** — one cron-friendly pass, exit 0/1, stats JSON to stderr.
- **Gate** — `make go-test-cronmail` (gofmt/vet/build + `-race` tests):
  golden **byte-parity test** (6 oracle fixtures, subject+html+text exact),
  escape/clean/jsonForScript pins, and 10 loop-semantics tests (happy, dry-run
  release, retry→dead at 3, missing/unknown emailType, legacy `type`,
  recipient resolution + missing-recipient, batch cap, stale reclaim). All
  green.
- **Wiring**: `server-ce/Dockerfile` gobuilder list += `cronmail`;
  `server-ce/cron/notification-email-dispatch.sh` now execs
  `/usr/local/bin/go-services/cronmail` (env + crontab entry unchanged).
- **Effect**: the last **live** dependency on `modules/notifications/app` is
  gone; that tree + `scripts/process_notifications.mjs` + the queue-consumer
  half of `document-updater` (Bull `ProjectNotificationQueueConsumer`) become
  ARC-4 junk-sweep candidates (queue path is dead: no live Node web consumer
  ever ran it in this stack — the producers are chat Go + the legacy
  scheduler writing straight to `emailNotifications`).

### ARC-9 · Yjs/Ygo collaboration pivot (D19, HARD CUT) — **START HERE**
Replaces the OT collaboration substrate (real-time Node + socket.io 0.9 fork +
ShareJS + overleaf-editor-core client + OT history) with the Yjs document
model: browser `yjs` + `y-websocket` + `y-indexeddb` + CodeMirror binding;
server = **ygo** (`github.com/reearth/ygo@v1.50.0`, MIT, Hocuspocus-compatible
WS server with auth hooks / rate limits, `VersionedPersistence` conformance
suite, file/sqlite/memory backends, Redis pub-sub + Streams cluster relay,
awareness, snapshots/compaction, `cmd/ygo-server` reference).

**S1 — DONE** — `go/services/collab` service embedding the ygo WS server (15 tests green under -race; `make go-test-collab`; Dockerfile gobuilder list; ygo@v1.50.0 pinned in go.mod/go.sum, additions-only diff):

**LIBS CONSIDERED (owner 2026-09-25, both aligned to the plan):**
- `coder/websocket` — excellent Go WS lib (ISC, context-aware, zero-alloc), but our only production WS path is ygo's provider (gorilla-internal); adopting it means rewriting S1 and contradicts D19/D20. **Rejected for this plan** (revisit only if the ygo server itself is dropped).
- `go.mongodb.org/mongo-driver/v2` (v2.9.1) — technically adoptable (needs Go 1.25+/Mongo 4.4+, both ok) but a repo-wide major bump against byte-pinned oracle-parity code. **Deferred** to a dedicated migration arc AFTER the S4 flip is stable; never mixed into a feature slice.
- `Authorize` hook = OlliTeX session (cookie → Redis session store, shared
  `core` primitives) + project access (owner/collab = read-write,
  readOnly = **read-only peer** via ygo `ConnectionConfig`);
- room = projectId, path `/collab/{projectId}`;
- persistence = ygo `FilePersistence` (versioned update log + snapshots +
  `MaterializeAt` restore) rooted under the data dir; Mongo adapter = S2;
- `cmd/collab` + `make go-test-collab` (auth gate, convergence, persistence
  round-trip, awareness) + Dockerfile build list;
- S2: Mongo `VersionedPersistence` adapter + restore/history endpoints on Go
  web; S3: client editor page (yjs stack, hard cut of OT editor);
  S4: flip — real-time Node + OT substrate → junk, runit + nginx + image
  + new Yjs e2e (convergence / offline reload / history restore).
**S2 — DONE** — `MongoStore` (`mongostore.go`): `VersionedPersistence` over
Mongo (one room doc: versioned update log `upds[]` + named snapshots
`snaps{}`; every mutation is a single-doc atomic write). Two server facts
pinned by live testing (Mongo 8.3): (a) **`$push` is NOT a valid
update-pipeline stage** — appends use `$concatArrays` (aggregation
expression) with `$ifNull($head,0)+1` computed via `$let`; (b) a
$set-with-$filter MUST be a pipeline (array-form) update — a plain update
stores the `$filter` doc as a literal value and corrupts the log.
**`persistence.RunConformance` GREEN** against live Mongo (all subtests:
append/list ordering, GetUpdate, MaterializeAt, PruneAfter target=2/0,
Compact, snapshot round-trip, Delete; crash-injection subtest skipped by
contract — single-doc writes need no crash choreography) +
`LegacyAdapter` bridge round-trip (the exact WS-server path). Test-quirk
pinned: `RunConformance`'s `factory()` must hand back a FRESH store per
subtest (MemoryPersistence semantics) — the Mongo test clears rooms per
factory call. `Service` now takes `Options.Store` (dev default unchanged =
FilePersistence; S4 wires Mongo in production).
  Remaining S2 slice: restore/history endpoints on Go web (next).
**RETENTION KNOBS (added this turn)** — `Service.KeepVersions` (0 = keep-all
default; N = retain most-recent N updates, oldest folded into one record) +
`Service.CompactEvery` (0 = compact-on-unload only = ygo default; N = also
after every N flushes). Wired to ygo's `CompactableAdapter` (server auto-calls
it; `LegacyAdapter` forwards to `MongoStore.Compact` with the `KeepVersions`
policy). Env: `COLLAB_KEEP_VERSIONS`, `COLLAB_COMPACT_EVERY` (default 0/0 =
unchanged behavior). New hermetic test `TestKeepVersionsWiring` pins the
adapter→store `keep` pass-through. This is the bound that keeps the
``single-doc log under Mongo's 16 MB cap for long-lived rooms (set
`COLLAB_KEEP_VERSIONS` > 0 + rely on snapshots for older history if a room
outgrows the cap). — **DONE**

**RETENTION KNOBS → /hub admin (goal item 2) — DONE (this commit)**
(D23): the two collab retention knobs are now first-class config-DB keys —
`COLLAB_KEEP_VERSIONS` + `COLLAB_COMPACT_EVERY` in the `configschema`
registry (`services` group, int, default 0) with `defaults.jsonc` lockstep,
managed by /hub admin (`GET /api/hub/config` lists them; `PUT` type-checks
ints) and the operator CLI; the `cmd/collab` service resolves them through
the new `configres` contract (DB → env → default; bind-on-start; additive
when the DB is absent). Full precedence matrix tested (incl. the zero-value
and junk-value fall-through seams); configschema + configres + hub + collab
+ collabhistory suites green (28 web pkgs ok).

**S3 — server slice DONE (this commit)** — the Yjs history surface lands on
Go web, decoupled from the OT engine:
- **`roomdoc.go`** (`go/services/collab`): the shared ROOM-DOC domain ops on
  any `VersionedPersistence` — `SeedTextContent` (room's v1 from initial
  file content; no-op if versions exist), `TextAt` (materialize version v's
  visible text), `HeadText` (current head text+version), `RestoreToVersion`
  (CRDT-correct restore: load head, delete-all+insert content(v) in ONE txn,
  append the delta as a NEW version — no time-travel, converges for all live
  peers; no new version when content already equals v), `ClientEdit` (test
  helper that emulates one browser edit end-to-end). `TextType = "content"`
  is the single shared Y.Text name the client + both server surfaces agree
  on. Hermetic (FilePersistence) + live-Mongo suites GREEN.
- **Seed hook** (`Service.SeedFn` → ygo `OnLoadDocument`): the SERVER is the
  single source of the first content — peers join EMPTY and receive the seed
  via initial sync; `OnLoadDocument` fires once per room, seeds only
  truly-empty rooms, persists as v1, fails room load on seed error
  (fail-closed). `TestSeedOverWS` pins the full y-protocol path (gorilla
  dial + step1 exchange) and that exactly ONE seed version is created for
  two peers.
- **`features/collabhistory`** (Go web REST, yhub-shape as SPEC): `
  GET /project/:pid/collab/history` (versions newest-first; ≥read),
  `GET /project/:pid/collab/history/:v` (version content; ≥read),
  `POST /project/:pid/collab/history/:v/restore` (write),
  `GET /project/:pid/collab/doc` (head; ≥read). Role-gated via the shared
  `collab` policy (owner/collab = RW, readOnly = RO, else 404 no-leak). The
  CRDT mechanics live in the collab package — this package is HTTP + policy
  ONLY, so the history surface and the WS surface cannot drift apart on doc
  semantics. 28 web feature packages still green.
- Registered in `cmd/web`.

**S3 client engine core — DONE (this commit, D24)** — the Yjs client engine
`frontend/js/features/ide-react/collab/` lands as a complete, tested unit:
`newYContent()` (Y.Doc + `content` Y.Text + LOCAL-origin programmatic
y-undo), the D24 `YTextSync` bridge (granular local spans → Y.Text /
full-mirror remote → host; probe-verified synchronous-observer model →
convergence invariant), `attachProviders` (y-websocket → `/collab/:pid`
same-origin cookie auth; y-indexeddb v9 offline; browser-gated so the
engine is headless-testable), `syncExtension` (the CM6 updateListener
half), `createEngine()` facade. **14-test Node suite GREEN in the frontend
workspace** (seed path, granular convergence, concurrent disjoint +
whole-doc, no-op idempotency, delete-to-empty, undo seam, endpoint/URL
contract). Wire contract pinned on BOTH languages (`roomdoc_test.go`
`TestTextTypeContract` ↔ `test/text-type.test.ts`). Gated: tsc **586
baseline held** (zero new), prettier clean, Go `collab` green, zero
`services/web` net-diff / no regressions. The engine is intentionally NOT
yet wired into the live editor (that hard cut is the S4 flip, below).

**S4 prep (a) — collab service goes live in the image (this commit)**: the
Go collab service gets its deployment artifacts. Runit service
`server-ce/runit/collab-overleaf/run` (binary-gated exit for
non-Go-lineage images, same pattern as `web-go-overleaf`; `COLLAB_LISTEN=
127.0.0.1:3450`; Mongo/Redis/session env from the container env dump;
`COLLAB_ALLOWED_ORIGINS` intentionally UNSET → ygo's default same-origin
check exactly matches the same-origin nginx pass). Nginx vhost
(`overleaf.conf.template`): `location /collab` → 127.0.0.1:3450 with WS
Upgrade headers (modeled on the `/socket.io` block), placed before the
stylesheets group; longest-prefix match wins over `location /`. No compose
change needed (service runs in-container under runit like all go-services).
Inert until the next image re-bake + cycle (live WS/seed/REST verification
is the next step, on the `ol-e2e` stack per the P7-constraint that the
canonical dev verification uses the isolated `ol-e2e-mongo-1`).

**S4 prep (b) — seed source LIVE + the dead-include build fix (this commit)**:
the room's initial content now has a REAL source (`go/services/collab/
seedsource.go`, wired in `cmd/collab` as `Options.SeedFn`): a room adopts
the project's CURRENT main-file content through the EXACT blob path the web
file proxy pins (historically-v1 hash store: `projects` doc → `rootFolder`
tree walk `fileRef`'s → `{WEB_V1_HISTORY_URL}/projects/{hid}/blobs/{hash}`,
basic-auth `staging:` — the same contract, same env names as the web). Main-
file selection is deterministic and test-pinned: `main.tex` (the template
contract) → first `.tex` → empty. Failure semantics: no project doc →
`ErrSeedProject`; no `.tex` / 404 blob → honest empty seed (a state, not a
failure); 5xx/transport → **fail-closed** (never seed a room from a failed
read). Both `overleaf.history.id` shapes (string + ObjectID) are pinned.
Suite: `TestSeedSource*` (hermetic httptest) + `TestSeedSourceWiredIntoService`
(production path — a real WS peer receives the seeded blob text over initial
sync, exactly one persisted version). This is the D19/seed design closed:
**the server is the single source of initial room content**, clients join
empty; existing projects backfill for free because their current main.tex
lives in the same blob store (no one-time migration job needed).

**BUILD FIX (this commit)**: the image re-bake exposed a DORMANT landmine
laid by P7 step-4 (`c6101120f9` retired the admin-tools Node backend, incl.
`app/views/active-projects.pug`) — `admin/index.pug:70` still `include`d it,
and `genScript compile` runs `precompile-pug` in the background with
`wait $pid` → the missing include made the image build exit 1 (the prior
builds only passed because the layer was cached — any context change
re-ran it). The dead "Active Projects" tab (header + pane) is removed from
`admin/index.pug` — consistent with the step-4 retirement (the live surface
for admin is the Go serveradmin//hub admin; the hub already has
`active-projects-section.tsx`). `yarn precompile-pug` verified GREEN
(37 templates compiled).

**S4 LIVE AUDIT (D26) — server-side contract verified end-to-end (this commit)**:
a live-probe pass against the real ol-e2e stack (nginx `/collab` → collab →
Mongo + docstore + redis sessions) caught **three production bugs** the
hermetic suite could not (all fixed, all test-pinned):
1. **Session gate** — `mongo.go` compared the RAW cookie value; the wire
   value is `percent-encode("s:" + sign(sid, secret))`. The gate now runs
   the web's exact pipeline (pct-decode → strip `s:` → verify HMAC-SHA256
   against the `OVERLEAF_SESSION_SECRET‖CRYPTO_RANDOM` chain) before the
   `sess:<sid>` lookup. Before: every real browser session 401'd (`collab.go`
   `TestSessionSidDecode`).
2. **Seed source** — rewritten to the editor's actual oracle (D26):
   `rootDoc_id` → docstore `GET /project/{pid}/doc/{did}` → `join(lines)`.
   (The earlier blob-path design was a wrong model: created projects carry
   `rootFolder` EMPTY and their content in the docstore revision-0 doc.)
   `seedsource_test.go` suite rewritten accordingly.
3. **Store wiring** — `cmd/collab` never passed `Options.Store`, so the
   service silently ran FilePersistence on a root-only dir → every room
   load 500'd. Now one Mongo client backs auth + seed + `MongoStore`.
Plus D22-first ops surface: `Options.Logger` (`COLLAB_LOG_LEVEL=debug`) +
lifecycle observers. **Live evidence (all green):** WS seed = rendered
template (222 B) → 2nd-peer convergence → live edit RELAYED → history
[2,1] → `restore` → v3 = seed → fresh peer converges → anon 401. Hermetic
relay pin: `TestRelay_TwoPeersEditAndPersist` (`go test -race` green).
**Next: the S4 IDE flip** (client hard cut) — the server half of the D19
pivot is now DONE AND VERIFIED.

**S4 FLIP (F1) — IDE hard cut OT->Yjs, client side (this commit)**: the editor text
synchronization is off the OT substrate and onto the D24 engine, live against the Go
collab service. Changes:
- `editor/document-container.ts` REWRITTEN as the Yjs adapter (same public API:
  join/leave, getSnapshot, pollSavedStatus, setTrackChangesUserId, submitOp, op
  getters, events, chaosMonkey). join = engine start + bounded wait for the first
  sync (fail-closed, same contract as the old joinDoc error); leave = engine
  destroy. Offline = y-indexeddb (replaces the OT offline backup). The OT review
  surfaces (shareDoc/historyOTShareDoc/getTrackingChanges/track-changes user
  id) are honestly no-op/thrower (D25).
- `source-editor/extensions/realtime.ts` REWRITTEN as the Yjs bridge
  (`syncExtension` = D24 local direction; container sink = D24 remote
  direction). `EditorFacade`/`trackChangesAnnotation`/`ChangeDescription` kept
  inert for OT-era imports (D25).
- `codemirror-editor.tsx`: the review-panel mount (ReviewPanelRoot,
  TabsHeaderPortal, FloatingMenu, TooltipMenu) replaced by the honest D25
  placeholder `YjsEngineReviewNote` (comments + tracked changes unavailable on
  the Yjs engine — NOT deleted; review-panel code stays in git, re-attachable
  after a Y.Doc-native port; owner decision).
- `review-mode-switcher.tsx` + `toolbar/review-mode-options.tsx`: the
  "Reviewing" (tracked changes) entry DISABLED with the honest description.
- OT offline-recovery unit test (document-container-recovery.test.ts) DELETED with
  the OT offline path (y-indexeddb replaces it — the contract retired, D25).
- `editor-manager-context.tsx`: teardown line no longer touches the OT shareDoc
  (engine destroy covers it); `history-ot.ts`: inert cast for the (never-active
  under Yjs) shareDocState init.
GATED: tsc **586 baseline held** (zero new — diffed vs stashed baseline),
frontend engine vitest **14/14**, webpack production **compiled successfully**.

**F1 STATUS: COMPLETE — live browser E2E GREEN (2026-09-26, image
`ollitex/ollitex:main` = 7a64d71cbc99 @ commit `bf045526fb`).** Battery
`tests/e2e/tmp-f1-e2e.mjs <pid>` on `ol-e2e-overleaf-1`: A1 editor boots with
seeded (v1) content ✓; A2 D25 review placeholder visible ✓; A3 local typing
committed (history v1→…v25) ✓; A4 external-peer push RELAYED and MIRRORED into
the open live editor (text grew, marker present) ✓; A5 history chain (26
versions, newest first, content per-version readable) ✓; zero page/console
errors. Two production root causes found + fixed during bring-up: **D36**
(nginx /collab now forwards `Host $http_host` — the port-kept form ygo's
same-origin check requires; browser WS was 403-ing) and **D37** (the
`document-container.ts` join-wait read a non-existent 2.x `.status` property;
y-websocket 3.x exposes state via `status`/`synced` EVENTS — rewired). TEST-
HARNESS bug (not production): the A4 push probe sent its update in the OUTER
'auth' (tag-2) envelope — `y-protocols` requires the update INSIDE the sync
(tag-0) envelope as sub-message 2, and relative to the SERVER's state vector
(`encodeStateAsUpdate(doc, serverSv)` — against its own sv it is empty);
`/tmp/wsprj/push.cjs` fixed and re-verified. Ops follow-through: psintern
(`compose_cep` `overleafserver`) re-pointed to the same fresh image and
re-verified (login page boots, form renders, zero JS errors — the earlier
stale-asset break is healed); orphan container `crazy_einstein` (standalone
idle mongo:6.0, no compose/data refs) STOPPED (kept, not deleted).

**REMAINING S4-FLIP WORK (F3 + D28a, per the D28 sequence):** ~~F2~~ **F2 ✅ COMPLETE (2026-09-26)** — client OT sweep: deleted `share-js-doc.ts`, `share-js-history-ot-type.ts`, `offline-doc-backup.ts`, `editor/types/document.ts`, `editor-watchdog-manager.ts`, vendored `sharejs.js` + 5 OT unit-test files; callers re-pointed (event types → `DocumentContainer`, watchdog plumbing removed, OT offline-recovery path retired → fail-closed OutOfSyncModal, D25). KEPT (verified live-consumed): `features/history` REST (history-UI backend over `history-v1` — NOT the retired OT proxy) and the socket.io EVENT BUS stack (D28; D28a will move the bus to Go). Gates: tsc 586 = baseline (delta 0), full vitest failure set byte-identical pre/post (86 pre-existing env failures), collab 14/14, realtime 3/3. NEXT — D28a —
app EVENT BUS from the Node `real-time` socket.io relay to a Go socket.io-
compatible endpoint (the bus itself STAYS — presence/file-tree/settings —
only the OT text sync died, D25/D28); F3 — the Yjs e2e above promoted into
the repo suite (parity + restore + convergence) and the final flip commit
(with D25's comments/track-changes owner decision recorded).

**S3 remaining (NOW the S4-flip step)**: (a) awareness/presence cursors
(the item-1 presence layer over `y-protocols/awareness` — needs the editor
integration + browser/e2e to do responsibly); (b) the hard cut — the IDE's
OT `ShareJsDoc`/`ConnectionManager`/`EditorFacade` transport replaced by
`createEngine()` (the mechanical wiring snippet is in the package README),
track-changes/comments `HistoryOTShareDoc` port-vs-junk verdict, retiring
`frontend/js/vendor/libs/sharejs.js` + the `real-time` service; (c) new Yjs
e2e (convergence / offline-reload / history-restore) on the live stack.
The client `TextType = "content"` already matches the server constant
(pinned on both sides). Then S4 flip.

- **Supersedes** the remaining "make OT services real" work (ARC-1/ARC-2 OT
  persistence machinery) and the D16 support-LLM real-time port: with a CRDT
  the transform/meshing layer no longer exists — history **is** the update
  log ygo already versions.

### ARC-8a · Notifications Node tree severed (D17 tail) — **DONE (this commit)**
The cron swap (ARC-8) left one live consumer on the Node tree: the frontend
hook's type import. That consumer is now cut, the rest of the tree deleted:

- `services/web/types/api/notifications.d.ts` (self-contained, 2 schemas,
  unchanged content) → **`frontend/types/api/notifications.d.ts`** — seeds
  the Phase-2 `frontend/types` home; the hook
  (`frontend/js/features/ide-settings/hooks/use-project-notification-preferences.ts`)
  now imports `../../../../types/api/notifications` (stays inside the
  frontend workspace — no cross-tree path).
- **Deleted** `services/web/modules/notifications/**` (17 files: app tree,
  5 unit tests, index stub, README, 1 untracked pug artifact) +
  `services/web/scripts/process_notifications.mjs` (superseded by cronmail).
- **Producer audit (safety evidence):** the only LIVE `emailNotifications`
  producer is the Go chat service (`go/services/chat` `UpsertEmailNotification`,
  emailType `projectNotification`) — contract verified against cronmail's
  claim/send; the tracked-changes producer chain (
  `ScheduleProjectChangeNotifications.mjs` +
  `ProjectNotificationQueueConsumer.mjs`) had **no live callers** at HEAD
  (zero call-sites repo-wide; the Bull consumer was never registered because
  Node-web boot is defanged) — functionally dead before this slice. The Go
  renderer still supports `trackedChangesNotification` for any future
  producer.
- Comments updated to post-severance truth: `settings.defaults.js`
  (moduleImportSequence live-set), `types/api/README.md`,
  `web-go-p614-flip` origin note.
- **Gates:** frontend tsc **586=586 set-identical** (stash A/B: +0/−0);
  backend tsc **94→39** (+0 added; −55 all inside the deleted tree);
  CI-exact vitest set **identical** (17F/1P/3t, pre-existing alias failures
  unchanged); webpack **0 module-not-found**, 23 errors all the pre-existing
  mini-css-extract class (local-invocation quirk), none reference touched
  files; `go build ./go/... ./cmd/...` OK.

**Net:** services/web now has **zero** Node runtime hooks — every
`modules/*` app tree is either retired junk (D1 sweep, Phase 5) or one of
two kept-alive exceptions (server-ce-scripts ops, authentication P2 family).

### ARC-4 · Removals (D1/D2/D3/D14)
15. **Junk-page removal** (§5) + e2e adaptations.
16. **`/status` removal** (D3) + e2e adaptations.
17. **Retire Node web** to junk/ (D1) + e2e adaptations.
18. **Delete `services/git-bridge`** (Java) + fix `build-git-bridge` Makefile
    target (D14).

### ARC-5 · Build / naming / docs
19. `server-ce` → `build-images` rename + `compose_cep/overleafserver/compose.yaml`
    (D11).
20. SeaweedFS → toolkit + image names (D12).
21. `frontend/**` README tour (D10).
22. Storybook exposure of web UI (D13).

### ARC-6 · Branding / publishing (owner-gated)
23. `/overleaf`→`/ollitex` route rename + `sharelatex/*`→`ollitex/*` registry
    push (D8).
24. **Image re-bake + stack cycle + push** (D9) — bake: Go consent, Go
    emailtemplates, new hub bundle, ARC-1/2 Go services, ARC-3 config-DB, all
    removals. Do this AFTER arcs 1–5 land.

### ARC-7 · i18n (D7)
25. `go/libraries/i18n` on `nicksnyder/go-i18n` (bundle from `locales/*.json` /
    its pre-translated DB), `T(locale,key,vars)` seam, e-mail German canary first,
    then Go-rendered shell strings. (Revises `docs/go-i18n-evaluation.md`.)

---

## 5. Junk page removal list (D2/D3)

**TRASH (page + its handler/route; keep any JSON API of the same path):**
| item | route(s) |
|---|---|
| 1 | `/university` |
| 2 | `/learn` `/blog` `/latex` `/contact` |
| 3 | `/launchpad/register_ldap_admin` `/launchpad/register_saml_admin` |
| 4 | `/templates/manage` (keep the bundle-import API) |
| 5 | `/project/new/dropbox` `/project/new/webdav` |
| 6 | `/user/notification-preferences` `/notifications/preferences` (redirect shells) |
| 7 | `/user/llm-settings` (redirect shell) |
| 8 | `/tag` |
| 9 | `/piv` |
| 10 | `/secret` `/some-post` |
| 11 | `/dev/csrf` (drop in prod image; keep for dev if wanted) |
| 13 | `/user/settings` **page** (keep its JSON API; /hub `mysettings.*` supersedes) |
| D3 | `/status` (healthcheck uses `/`) |

**KEEP:** `/user/llm-usage` (backs /hub `mysettings.llm.usage`), all other
`/user/*` JSON APIs + OAuth callbacks, `/login /logout /register /restricted
/read-only/one-time-login`, `/launchpad` (register), `/librar*`, `/orcid-picker/*`,
`/project*` + `/hub*`, `/editor/:id`, `/cookie-consent`, `/legal`.

> Each removal = one green slice: delete route + handler, drop/redirect any
> inbound e2e that pins it, re-run the affected specs + `go build ./go/...` + the
> frontend gate if the page bundled JS.

---

## 6. Green-slice invariant & gates (unchanged)

- **NEVER `git add -A`.** Stage exact paths; no probe/scratch files in the repo.
- Per unit: `gofmt` + `go vet` + `go build` + `go test` (+ `tsc`/`webpack` for
  frontend, + the relevant e2e spec) **green**, then one commit.
- **tsc baseline = 586 pre-existing errors** (hub etc.) — real gate = "no NEW
  errors" + webpack + browser. Host mocha `test:frontend` is host-broken
  (pre-existing `bootstrap.js` `Unexpected identifier 'as'`) — not a signal.
- **Playwright e2e**: run from `tests/e2e/` (own npm `node_modules`, NOT yarn PnP).
  CSRF header `X-CSRF-TOKEN` from `meta[name="ol-csrfToken"]`; login `#email`/
  `#password`; e2e admin `e2e-admin@e2e.test` / `Ol-Fixture-9x7K`.
- **FLIP-era specs copy host `bin/web`** → run `make go-build` before parity/full.
- Baked views = compile-time Go HTML constants pinning hashed chunk names;
  live hot-patch = serve new content under old names (additive, `.bak-patch`).
  Durable fix = the owner-gated image bake (ARC-6).

---

## 7. Live stack / deploy facts (unchanged)

- Post-cutover: `web-overleaf` = Go :4000; `web-api-overleaf` = Go :3000;
  nginx → `web-overleaf`; Real-time = Node; mail sink = `smtpsink` (:1025 / API
  :8025); Mongo `ol-e2e-mongo-1` (treat oplog inspection with care); git-bridge =
  Go `gitbridge-go:latest`.
- Deploy loop: `make go-build` → `docker cp bin/web
  ol-e2e-overleaf-1:/usr/local/bin/go-services/web` → `docker exec
  ol-e2e-overleaf-1 sv restart /etc/service/web-overleaf` (md5-verify the
  binary).
- Consent + emailtemplates are live on the stack via the same docker-cp pattern;
  the hub bundle is hot-patched in-container; all of it bakes durably in ARC-6.

---

## 8. Open questions / owner input

**Q1 (D5) — Postgres in the stack?** *Recommendation: NO, keep Mongo.*
`history-v1`'s Node code supports both a Mongo and a Postgres chunk backend; the
live stack already runs **Mongo** (required by the whole app) and has **no
Postgres container**. Adding Postgres = one more long-running DB service with no
benefit to us, and the Go port targets Mongo (already present, already the data
store). **Decision requested: confirm "no Postgres, Mongo backend".**

**Q2 (D6) — "boot env" list.** To boot /hub so everything else is /hub-editable,
the minimal env that must remain (all of it either a credential/infra/boot
param, intentionally NOT in SQLite):
- **Credentials (encrypted at the caller, never in the DB):** `OVERLEAF_SESSION_SECRET` / `CRYPTO_RANDOM` (+ `SESSION_SECRET_UPCOMING/FALLBACK`), `CONFIG_DB_ENCRYPTION_KEY` (the config-DB key), `WEB_API_USER/PASSWORD`, `STAGING_PASSWORD`, `OT_JWT_AUTH_KEY`/`OT_JWT_AUTH_OLD_KEY`.
- **Boot/infra:** `ENABLED_SERVICES`, `NODE_ENV`, `OVERLEAF_HOME`, `CONFIG_DB_PATH`, `DOCKER_SOCKET_PATH` (sandboxed), `OVERLEAF_MONGO_URL` / `MONGO_CONNECTION_STRING`, service endpoints (`DOCSTORE_HOST`, `FILESTORE_HOST`, `DOCUMENT_UPDATER_HOST`, `WEB_*_URL`, `REALTIME_URL/USER/PASS`, `CLSI_LB_HOST`, `WEB_CHAT_URL`, `PROJECT_HISTORY_HOST/URL`, `V1_HISTORY_URL/USER/PASS`, `GIT_BRIDGE_HOST/PORT`, `WEB_GO_PUBLIC_DIR`), `COOKIE_DOMAIN`, `ALLOWED_ORIGINS`, `DOCKER_SOCKET_PATH`, `ADDITIONAL_TEXT_EXTENSIONS` (if kept env).
> Everything else (site-wide settings + the `seeds.go` env map) moves to the
> encrypted SQLite via /hub in ARC-3, and its legacy docker env params are
> removed (D6). **Decision requested: confirm this split + the
> `CONFIG_DB_ENCRYPTION_KEY` name.**

**Q3 — ARC ordering.** Proposed: ARC-1 → ARC-2 (the two services, D4) in parallel
with ARC-3 (config-DB). ARC-4/5/7 anytime. ARC-6 last. **Decision requested:
confirm order or prioritize.**

---

## 9. Reference map (where things live)

- Gate defs: `Makefile` (`go-test-history-v1`, `go-test-document-updater`,
  `go-build`, `test-go`).
- Audit: `GO_SERVICES_INTEGRATION.md`; i18n eval: `docs/go-i18n-evaluation.md`.
- Config: `go/libraries/configstore/`, `go/services/web/core/configdb_override.go`,
  `go/services/web/features/hub/config.go`, `cmd/configdb/`,
  `go/services/web/features/sitesettings/` (Mongo + `seeds.go`).
- Services: `services/history-v1.go/`, `services/document-updater.go/`,
  `go/services/gitbridge/` (Go), `services/git-bridge/` (Java, to delete).
- Frontend: `frontend/` + `services/web/modules/ollitex-hub/`; tests
  `tests/e2e/specs/`.
- Deploy/build: `server-ce/` (→ `build-images`, D11),
  `/data_1/docker/compose_cep/`.

---

## 10. D28a — real-time event bus in Go (2026-09-26, LIVE)

The Node `services/real-time` socket.io-0.9 relay is replaced by a Go service
(`go/services/realtime/` + `cmd/realtime`, port 3026, 1:1 wire contract —
live-captured strings pinned in `protocol_test.go`). The browser socket.io
0.9.17-overleaf-6 client is unchanged. OT doc transport (`joinDoc` /
`leaveDoc` / `applyOtUpdate`) is deliberately NOT ported — after the F2 hard
cut, real-time is purely the collaboration-event bus (presence, cursors,
join/leave, drain, ops); text sync lives in the Yjs/Ygo collab engine (port
3450). Doc-granting moved to join time (`grantdocs.go`) since F2 clients
no longer emit `joinDoc`.

**Flip (this slice):**
- `server-ce/runit/real-time-overleaf/run` → `go-services/realtime` (Node
  kept only as an in-image fallback if the binary is absent)
- `server-ce/Dockerfile` builds `go-services/realtime` alongside the other
  go-services
- `cmd/realtime/main.go`: `WEB_API_PORT||WEB_PORT||3000` — the join/flush
  private API lives on the Go web **api profile** (:3000), exactly like the
  Node bus default. (A first build defaulted to the :4000 web profile, which
  correctly 403s CSRF-blocks POST /join — caught by E2E, fixed.)
- Gate: `make go-test-realtime` (build+vet+gofmt+test -race; wire pins,
  join flows, presence, drain, ops, full websocket client e2e) — GREEN.

**Live verification (2026-09-26, final image `fb0e7dc814cf`, e2e stack):**
- D28a bus battery — B1 IDE boots (bus join) · B2 second tab boots ·
  B3 `/clients` shows both publicIds · B4 `/count-connected-clients`=2 ·
  B5 close tab → count drops to 1 — ALL PASS.
- F1 regression battery — A1 render · A2 D25 placeholder · A3 typing →
  history v2 · A4 external peer push mirrored · A5 history chain ≥3 —
  ALL PASS (the bus flip changed only the transport underneath).

**D28a COMPLETE (2026-09-26).** Node `services/real-time` hard-cut and
deleted (commit `ee26f0a0b2`, 18,288 lines), workspace dropped, dev
compose repointed at the Go binary. Gates green: `make go-test-realtime`,
tsc services/web = 586 (delta 0), vitest full suite byte-identical
(86 failed / 4851 passed — pre-existing baseline).

**S4 FLIP COMPLETE (D19)** — F1 client engine (A1–A5) + F2 client OT
hard-cut (`ccf85fc12a`) + D28/D28a event bus in Go (this section) +
**F3 promotion**: both batteries are now permanent suite specs
(`tests/e2e/specs/collab-yjs.test.e2e.ts`, 2-tab CRDT convergence incl.
a REAL second peer; `tests/e2e/specs/realtime-bus.test.e2e.ts`, presence
lifecycle) — 2 passed in 56s on the live stack.

## 11. D27 dependency-refresh (2026-09-26, post-S4 — unblocked)

Executed the verifiable half of D27:
- **Bumps applied + gates green**: `go-redis v9.18→v9.22`, `aws s3
  v1.113.1→v1.113.4`, `x/crypto v0.54→v0.57` (+ transitives x/sys, x/text,
  x/sync), `go mod tidy` clean, full module build + per-package gates green.
- **bzip2 swap: DEFERRED with evidence** — the D27 note assumed
  `klauspost/compress/bzip2` exists; it does NOT (verified:
  `go get github.com/klauspost/compress@v1.16.7 → module does not contain
  package bzip2`; v1.20.1 the same). No vetted pure-Go bzip2 drop-in was
  resolvable through the proxy (`nwaples/bzip2` 404s at both v1 and v2
  paths). `dsnet/compress/bzip2` (gitbridge swap-archive codec) stays —
  working, pure Go, single call-sites; revisit if it misbehaves.
- **Test hardening (real defects fixed along the way)**:
  - `go/libraries/persistors/migration_test.go` — DATA RACE FIXED: the
    fake persistor's call-record slices were written by the background
    copy-on-miss goroutine while tests polled them; mutex with proper
    Lock/Unlock discipline (first attempt locked-without-unlock → deadlock
    caught by the test, corrected) + locked accessor reads in the test.
  - `go/libraries/mongoutils/batchedupdate_test.go` — DATA RACE FIXED:
    the stderr-capture `bytes.Buffer` was shared between the pipe-reader
    goroutine and the test poll (bytes.Buffer is not concurrent-safe);
    now a `captureBuf` with chunked lock-scoped appends, and the fixed
    50ms settle sleep replaced by a 2s settle poll.
  - `ologger`/`ometrics` tick tests — fixed sleeps replaced; `stubLogger`
    race fixed (mutex); two REAL races fixed: (a) `ometrics.registry`:
    series mutations applied OUTSIDE the registry lock while `Get()` read
    them under it → now applied under the lock; (b) Go 1.27 PLATFORM
    FINDING (minimal repro in /tmp/twk): `ticker.Stop()` no longer wakes a
    `range ticker.C` receiver — `EventLoopMonitor` switched to a
    select-on-stop-channel exit + `loopWait WaitGroup` so tests/shutdown
    deterministically drain the tick goroutine before touching the globals
    it reads. ometrics/ologger/persistors/mongoutils now pass
    `-race -count=2/3` standalone.
  - **PRE-EXISTING, NOT TOUCHED (verified pre-existing with pre-bump
    go.mod):** `rediswrapper` locker/health tests still flake under
    `-race` count=2 (timing asserts + fake-driver latency) — candidate for
    a dedicated test-hardening slice; not a D27 regression.

**D38 (this session)** — P7 goal item 5 ("frontend consolidated under
/frontend/modules"): the 28-module frontend trees have NEVER been moved
(services/web/modules is the live single tree, oracle-pinned under
`make all` + PnP + baked views). Moving 2000+ files is pure churn with
parity risk and zero behavior gain; per D27's own "don't churn mid-endgame"
verdict, **DEFERRED** (post-endgame, only if the owner wants the shape
change). All other terminal-state bullets hold (see §10).

**D39 (2026-09-26, LIVE BUG — owner report "login CSS missing / locking")** —
ROOT CAUSE: the 2026-09-26 06:56 cold-webpack rebuild moved Mantine's
compiled CSS-in-JS rules OUT of `main-style-*.css` INTO a shared chunk CSS
(`9663-a7a4b91ea9fb30ea5876.css`). The baked login view (pages_data.go
loginHTML) had no `SHARED:CSS:` token, so that chunk CSS was never linked:
100% of Mantine class rules unapplied in-browser (UA-default rendering =
the "missing CSS"/"locking" perception). Audit of ALL baked views: login
was the only missing one (register/password-reset HAVE the token; ide/detached
carry per-entry CSS already linked; token-access/sharing-updates/
user-settings/project-invite* entries have no CSS in the manifest). FIX:
`\x01SHARED:CSS:pages/auth/login.js\x02` added to loginHTML (parity with
registerHTML) — the per-generation resolver links the chunk CSS from the
in-image entry file (D31 machinery). Gates green (views/core -race).
DEPLOY: image re-bake + overleafserver cycle.

**D39a (2026-09-26, the REAL root cause — supersedes D39's "capture-cap" theory):**
`core.scanSharedDirs` matched shared-chunk files with `sharedFileRe =
^(\\d+)-[a-f0-9]{10,}\\.js?$` — a pattern that can ONLY match `.js` files, so the CSS
map was ALWAYS empty and every `SHARED:CSS` token silently emitted zero links (only
`sharedMissing` bookkeeping). It went unnoticed because every shared-CSS test fixture
was written for the JS path and the Node-era parity captures predate webpack's
per-chunk CSS — until webpack split Mantine's rules into chunk CSS (the 09-26 assets),
at which point login/register (and every token page) lost all shared styles. FIX:
extension-split scan (`sharedFileJSRe` .js-only → sharedDirJS; `sharedFileCSSRe`
.css-only → sharedDirCSS) + regression pin `TestSharedCSSResolution` (fails on the old
code) + register parity capture re-pinned WITH the now-emitted 9663 shared CSS link at
its token position (oracle re-bake; the link is the fix, the capture predated it).
Gates: full `go/services/web/...` build+vet+`-race` green.

**D39b (2026-09-26, the REAL token) + CLOSEOUT** — loginHTML's `SHARED:CSS` token was
registered in `sharedCSSTokens` but NEVER actually inserted into the login HTML (an early Python
edit only added the map entry) → re-inserted after the manifest link (`7fc16d7cdb`), parity with
registerHTML verified. **C CLOSED (2026-09-26)**: also removed the dead `real-time` entry from
`server-ce/services.js` (it broke every `build-community` bake); canonical bake `b17465cbda0d`
(lineage `45f0867be2` = D39a+D39b+D40 P1+threads) → `overleafserver` cycled → LIVE VERIFIED:
`/login` 200, Login button `rgb(9,136,66)` = **#098842 brand green** (radius 8px),
`/stylesheets/9663-a7a4b91ea9fb30ea5876.css` loaded, `/status` 200, new review API routes
registered (anon → 302 to login). Owner confirmation = ticket close.

**D40 (2026-09-26, owner directive 10 — HIGHEST PRIORITY) — Y.Doc-native model for
comments + tracked changes** — supersedes the D25 "placeholder" decision.
- ONE Y.Doc per room (existing `persistence.VersionedPersistence` store) gains first-class
  top-level types next to `content` (Y.Text): `comments` (Y.Array of record maps: id, file,
  ranges, text, author, created, edited, replies, state) + `trackedChanges` (Y.Array: id,
  type insert|delete, ranges, content, author, state pending|accepted|rejected).
- **d1 (taken as default, owner may veto):** ranges = plain {start,end} in P1 (Node-contract
  compatible); sticky anchors via RelativePosition (ygo) are the P3 upgrade.
- **d2 (taken as default):** SERVER-AUTHORITATIVE lifecycle — web REST (Go web, same seam as
  collabhistory) proposes; collab-package domain ops apply exactly once; idempotent on record
  id (already-applied accept/reject = no-op). Deterministic e2e oracle; matches D19.
- d3: comment emails via existing cronmail/emailtemplates seam (assumed yes).
- d4: P1 text files only (assumed yes).
- **Contract to serve = the V1 threads/track-changes API the in-git review panel calls**
  (`frontend/js/features/review-panel/`, e.g. `POST /project/:pid/doc/:docId/changes/accept
  {change_ids}`) NOT the legacy OT-comment shape — the panel is the ship client.
- Phases: **P1** doc types + domain ops + hermetic tests (go/services/collab, roomdoc.go seam) —
  **GREEN (a03d7385ce)**: review.go + review_test.go, 10 tests incl. order-independence and
  double-reject idempotency; **threads Y.Array + lifecycle GREEN (45f0867be2)** — incl. fixing the
  YMap-accessor-in-Transact deadlock (read values before mutating).
  **P2** V1 contract routes on Go web — **GREEN (c850e88f45)**: `go/services/web/features/review`
  registered in cmd/web (panel's full call set; wire shapes pinned from
  `services/web/types/review-panel`; resolve/reopen persist `resolved_by_*` actor;
  8 hermetic suites incl. role gating + own-message 403/200 + cascade).
  **P2 RE-ALIGNED (ea4179379b / 0464f1a10f / 214f3a8e55)** — re-audited the panel
  top-to-bottom and fixed three contract mismatches + wired the live-sync seam:
  - **d5 (contract evidence, recorded):** the Node CE fork NEVER served these
    routes (`services/web` has only the internal `changes/reject` →
    document-updater, NO threads/track-changes endpoints) AND the Yjs engine
    (F1/D25) throws for `historyOTShareDoc` (document-container.ts) — the
    panel's OT comment paths were DEAD in this fork, so **the REST surface IS
    the shipping contract**. Pinned shapes that differ from first build:
    `GET threads` = `Record<threadId, Thread>` (panel `type Threads`), flat
    `resolved_at`/`resolved_by_user_id`/`resolved_by_user`; thread creation
    = FIRST message POST for a client-generated threadId (body
    `{content, id?, doc?, ranges?}` — panel addComment) — no
    `POST /project/:pid/threads` needed; `track_changes` body =
    `{on_for?, on_for_guests?}` → explicit map on `project.track_changes`
    (Node parity: ProjectEditorHandler → editor trackChangesState); NEW
    server-assisted `POST .../doc/:doc/changes` (editor-side creation
    pending — the gap D40 remark A asks for).
  - **d7 (live-sync, owner-visible):** the panel syncs local state via room
    socket events; in Go the equivalent seam = realtime bus SendRoomMessage
    (`POST /project/:pid/message/:name`, pinned 'Node
    HttpApiController.sendMessage → LB emitToRoom'). Review handlers relay all
    8 pinned events (new-comment, edit-message, delete-message,
    resolve-thread, reopen-thread, delete-thread, accept-changes,
    toggle-track-changes) with listener-pinned payloads; `Handlers.Emit`
    seam + `config.RealtimeURL` (REALTIME_HOST||127.0.0.1 :3026);
    best-effort (emit failure logs, never 5xx — Node parity).
  - **d8 (2026-09-26, e2e root cause #1 — ROUTE ORDER)**: the legacy P6.12
    `features/trackchanges` (Node-module shadow proxying chat :3010 / DU :3003 —
    the dead OT pipeline) registers the SAME 11 panel routes and was registered
    FIRST (main.go) → first-match dispatch let it swallow every D40 route; its
    pinned-downstream-failure path renders the HTML 500 page (the exact symptom:
    500 + `general/500` body + NO Go log line; GET /threads "passing" was the
    legacy reader, not review). **Decision**: `review.Feature(app)` now registers
    BEFORE `trackchanges.Feature(app)` — review owns all 11 overlapping routes
    (the D40/d5 panel contract is the shipping contract); legacy keeps only the
    non-overlapping `/ranges` + `/changes/users` fallbacks. Evidence of the
    pre-existing flip-gate drift (NOT a D40 regression): `web-go-p614-flip`
    (notifications, zero D40 surface) fails the same "Go :4010 never came up"
    harness leg — the whole flip-gate family probes the pre-P7 shadow port +
    Node-web baseline legs (retired by D1/P7), so it is stale machinery, not a
    live contract. The legacy flip specs for track-changes (web-go-p612-flip)
    are superseded both mechanically (:4010) and contractually (D40 d5/d8):
    retiring/rewriting the 53-spec flip family is a separate housekeeping arc,
    not part of the D40 green slice.
  - **d9 (2026-09-26, e2e root cause #2 — primitive.D)**: mongo-driver decodes
    an `any`-typed struct field holding a BSON document as **`primitive.D`**
    (not `map[string]any`) — `prodTrack`'s read switch missed it → the
    `track_changes` merge silently dropped the stored map on every write after
    the first (live mongo showed `{merge_probe:true}` only after the first call
    stored `{e2e_probe_user,__guests__}`). Fixed: switch now handles
    `primitive.D` / `primitive.M` / `map[string]any` / `bool`. (Same driver
    quirk applies to every `any` struct-field decode in this tree — audit
    others on suspicion.)
  - **P2 GREEN (2026-09-26)**: `review-panel.test.e2e.ts` (R1 boot record-GET,
    R2 first-message thread create 201, R3 reply, R4 resolve/reopen w/ actor,
    R5 change create + bulk-accept idempotency, R6 on_for/on_for_guests
    persist+MERGE, R7 own-message authorship) PASSED live (43s); `collab-yjs`
    PASSED (no D25 placeholder + record GET). Go green-slice: gofmt/vet/build
    clean; `go test -race` green across `go/services/web/...` + `cmd/web/...`.
    Diagnostic hardening kept: `internalErr` logs `review: 500: <err>` on
    stderr (500s must be diagnosable live).
  - **d10 (2026-09-26, D40-surface read path)**: `GET /project/:pid/doc/:doc/changes`
    — the D40 surface read path for tracked changes. The Node world served the
    change list from the OT snapshot (dead in this fork), so the REST surface
    gains the list endpoint (d8: REST is the shipping contract). Room-scoped
    (P1 single content doc), creation order, role read (below → 404), wire
    record {id, kind, file, start, end, content, author, created,
    timestamp_ms, state}. E2E R5b asserts created change present w/ state
    transition. (Editor-side capture + panel Changes-tab re-wire build on
    this route — next D40 slice.)
  - **d11 (2026-09-26, editor-side tracked-change capture — d5 pending piece
    implemented)**: when this session's track-changes is ON (the panel's
    'toggle-track-changes' intent → editor-manager-context →
    `DocumentContainer.setTrackChangesUserId` → `track_changes_as` — the
    D25 DISABLED pin is superseded), every LOCAL CM6 edit span is captured
    as server-authoritative change records on the d5 create surface
    (POST /project/:pid/doc/:doc/changes): pure insert → zero-width
    {content,start,end:start}; pure delete → {start,end}; replace → BOTH,
    delete first (a record is single-kinded; d2 idempotent apply). Pure
    span→body contract in `ide-react/collab/capture.ts` (`spanBodies`,
    5 unit tests in the pinned CollabYjs vitest set); host listener
    `trackedChangesCapture` in `source-editor/extensions/realtime.ts`
    (gated on track_changes_as; skips remote mirrors via
    Transaction.userEvent 'input.remote'; best-effort fetch — failure
    never blocks typing). Gates: tsc 586=586 (0 new); CollabYjs vitest
    19/19; services/web vitest failure set IDENTICAL with/without the
    slice (stash A/B — 0 regressions; the 123 FAIL lines are the pinned
    pre-existing local-env alias failures).
  - **P2 REMAINING** = panel Changes-tab re-wire to the d10 read path (the
    panel currently reads changes from the dead OT historyOT snapshot —
    the d5/d8 superseded source; slice after live capture verification).
  **P3** relative-position anchoring + concurrent-lifecycle races. **P4** legacy OT
  comments/tracked-changes backfill at first Y-join.
- **P2 contract (PINNED from the in-git panel — do not invent; `frontend/js/features/review-panel/`):**
  - `GET   /project/:pid/threads` → Record<threadId, Thread> (d5 pin)
  - `POST  /project/:pid/thread/:threadId/messages` (FIRST message creates the
    thread — d5 pin; body {content, id?, doc?, ranges?})
  - `POST  /project/:pid/thread/:threadId/messages/:commentId/edit` (edit message)
  - `DELETE /project/:pid/thread/:threadId/messages/:commentId` (any owner)
  - `DELETE /project/:pid/thread/:threadId/own-messages/:commentId` (own-message rule)
  - `POST  /project/:pid/doc/:docId/thread/:threadId/resolve | /reopen`
  - `DELETE /project/:pid/doc/:docId/thread/:threadId`
  - `POST  /project/:pid/doc/:docId/changes` `{content?,start,end?,kind?,change_id?}` (NEW d5 —
    server-assisted create; non-empty content → insert)
  - `POST  /project/:pid/doc/:docId/changes/accept` `{change_ids: [...]}`
  - `POST  /project/:pid/track_changes` `{on_for?, on_for_guests?}` (d5 pin;
  - NUANCE: the panel rejects changes CLIENT-side (source-editor
    `changes/reject-changes` does the text edit) then state-syncs — in Yjs the content edit is a
    plain Y.Text mutation (already CRDT-convergent) + a server state transition; the P1
    server-applied RejectChange mutation stays as the deterministic path for server-driven/batch
    flows. P2 maps panel flow → (client text edit + state op) exactly as the panel does.

**D41 (2026-09-26, owner item 11) — history-v1 / document-updater → Yjs verdicts**
- **history-v1: YES (hybrid b1).** Yjs-native history = the Y.Doc update stream; versions/restore
  already live (collabhistory REST + ygo versioned store + D23 retention). **diff** = Y.Text
  snapshots at two versions + go-diff (in-tree via otc). Multi-file hybrid: text in Y.Doc
  versions; file-tree ops (add/rename/delete/binary) stay in the Go web API with their own
  version log; the history contract composes both streams into the Node-parity answer.
  Full file-tree-in-Y.Doc (+ ygo blobs) = optional stretch, NOT the critical path.
  The OT engine behind it is transitional; retires after the last OT doc migrates (D40 P4).
- **document-updater: RETIRE (as OT applier) + salvage non-OT duties.** Apply/rebase-into-
  docstore is exactly what ygo persistence replaces — nothing to convert. Non-OT duties
  (limits ≈ web limiter.go; notifications ≈ cronmail; preview job = audit at execution) salvage
  as small Go worker/cron slices. **Main-arc re-scope:** terminal state = Yjs-native history
  (reusing history-v1.go's engine-agnostic HTTP/dispatch/security layer) +
  document-updater retirement + otc junked post-migration — NOT flipping the OT-engine ports live.

**D42 (2026-09-26) — project-history B-track handoff policy** (parallel Go port,
`/data_1/image_mining/the_diff/project-history.go`, module `project-history`; live checkout
davrot-machine, branch golang-ph; ledger = its HANDOFF.md; B7/B8b done, next B9/B10/B12)
- **B9 oracle blocker RESOLVED:** the two disputed vendor expectations ("insertions at the start
  and end" → `[20]`; "non-linear offset order" → `[3,'bar',12,'foo',5]`) verified as GROUND
  TRUTH by running the vendor's own 27-case suite in the overleaf monorepo (byte-identical
  sources, real OEC): `yarn workspace @overleaf/project-history exec mocha --loader=esmock
  --exit test/unit/js/UpdateTranslator/UpdateTranslatorTests.js` → **27 passing**; the mismatch
  is Go-side (OperationsBuilder cursor/docLength bookkeeping / OperationsCompressor sibling
  composition). Recorded in project-history.go/HANDOFF.md §"B9 ORACLE GROUND-TRUTH".
- **Policy (owner question answered):** port writing STAYS with the support LLM for now; owner-
  side picks up Go B9/B10/B12 after (a) D39 bake+cycle+verify is green and (b) D40 P1 lands
  (both now done) — or immediately on owner instruction. No state lost: vendor 27-case table = the Go oracle.
- **B9 DONE (2026-09-26, owner-side takeover):** `internal/updatetranslator` (translate.go/
  builder.go/translate_test.go) — 1:1 port green against the 27-case vendor oracle (both
  previously-disputed cases included: `[20]`, `[3,'bar',12,'foo',5]`), coverage 86.6%,
  `go test -race ./internal/...` all green. 3 oracle-surfaced port fixes recorded in
  project-history.go/HANDOFF.md (builder retain-string length, v2Authors always-array,
  historyot origin wire via ToWire + toWireOrigin nil-guard).
- **B12 DONE (2026-09-26, owner-side):** `internal/types` — 1:1 wire model of vendor
  mongo-types.ts (ErrorRecord|SyncStartRecord discriminator; ProjectHistoryFailure with
  optional requestCount; exact mongo keys incl. snake_case `project_id`); 80.2% ≥80 gate.
- **LockManager coverage DONE (owner-side):** 65.7% → **94.0%** (race-safe suites incl.
  GetLock-timeout, (*Lock).Extend branches, HealthCheck free/busy/err, mismatch +
  redis-error paths).
- **B-track state:** B9/B12/LockManager green (all 16 test pkgs -race clean); **NEXT = B10
  ChunkTranslator** (647L + 3.1kL test oracle; needs WebApiManager/HistoryStoreManager
  seams — a dedicated session), then C-phase managers per the HANDOFF worklist.

**OUTSTANDING (owner decision):**
- psintern (compose_cep) still runs the pre-D28a image with the Node bus
  (Node `real-time` no longer exists in the tree — the old image keeps
  running its baked copy). Applying the new image there = a hard cutover
  on the external host → needs owner sign-off.
- MAIN ARC (history-v1 / document-updater live flip): Go ports are
  hermetic-gate green (`services/history-v1.go/`,
  `services/document-updater.go/`, HANDOFFs in-tree; live homes under
  /home/davrot/history_v1). Remaining: real persistors + zip/clone
  streaming (501-bucket), document-updater Phase 9 (managers/HTTP over
  live Redis/Mongo), ARC-1b blob migration, then the runit flips.