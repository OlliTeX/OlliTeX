# OlliTeX — State & Plan (single source of truth)

> **This is the current, living state+plan doc.** `WEB_GO_PLAN.md` is **frozen**
> (historical record only — do NOT append to it; it grew to ~210KB). Everything
> current lives here. Update this file as the durable state doc, not as a log.
>
> Last updated: 2026-09-25 (ARC-9 S1 done). Branch `main`. Repo root `module ollitex`, Go 1.27,
> yarn 4.18.0 (PnP). Live e2e stack = `ol-e2e-overleaf-1` (nginx :7420).

---

## 0. One-paragraph where-we-are

The Node→Go 1:1 port is complete for the **web** surface (Go `bin/web` on :4000
canonical, Go `bin/web-api` on :3000) and the P6/P7 cutover + build-system
rearchitecture (ubuntu:26.04) + P7-post items (1 SQLite config-DB, 2 GDPR
consent, 3 /hub email templates, 4 go-i18n evaluation) all landed. Now in flight:
turning the two remaining Node microservices (**history-v1**, **document-updater**)
into real Go services (API/logic already ported+green in-repo; **storage/service
layers are the gap**), refactoring **SQLite config-DB into the single source of
truth** for site-wide settings (decoupling/removing the old docker env params),
and a wave of **removals** (retire Node web, junk pages, Java git-bridge).

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
| D14 | **git-bridge**: confirmed already Go (`gitbridge-go:latest`). **DELETE `services/git-bridge`** (Java) + clean the Java `build-git-bridge` Makefile target | APPROVED |
| D15 | **Procedural**: stop appending to `WEB_GO_PLAN.md`; maintain this file as the state doc | APPROVED |
| D16 | **`services/clsi`, `clsi_typst`, `project-history`, `real-time`** are being converted to Go **by support LLMs** (parallel; "ready soon") — do NOT re-port; integrate + audit when they land | AWARE (no action) |
| D17 | **Notifications email-dispatch cron → Go** (kills last `modules/notifications/app` runtime dependency) | APPROVED — **DONE** (ARC-8 + ARC-8a severance; zero Node runtime hooks left in services/web) |
| D18 | **Toolkit placement**: `tools/toolkit` → repo-root **`toolkit/`** (visible root position), all references updated | APPROVED — **DONE this commit** |
| D19 | **Collaboration pivot — Option B (Yjs/Ygo, HARD CUT)**: the document model becomes Yjs; server = **ygo** (`github.com/reearth/ygo`, Re:Earth's pure-Go CRDT stack — Hocuspocus-compatible WS server, Redis-Streams cluster relay, versioned persistence + snapshots, awareness; MIT; v1.50.0 pinned). The 2014 substrate (real-time Node + socket.io fork + ShareJS + OT client/editor-core) is **retired junk after the flip — no OT legacy reader** (owner hard-cut call 2026-09-25). Support-LLM real-time port: irrelevant (their own copy). Existing OT history is **not portable** — new projects seed Y.Text from current file content; OT history drops. | APPROVED — **IN PROGRESS (ARC-9)** |
| D16* | `clsi`/`clsi_typst`/`project-history` still support-LLM territory; **`real-time` is SUPERSEDED** by D19 (pivot, not port) | SUPERSEDED (real-time) |

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
  - **Leftover to clean:** `server-ce/Makefile:90 build-git-bridge` builds the
    **Java** image from `services/git-bridge/Dockerfile` and is in the `build`
    aggregate (`:130`) — must be removed/rewired to the Go image path.
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

**S1 — DONE** — `go/services/collab` service embedding the ygo WS server (13 tests green under -race; `make go-test-collab`; Dockerfile gobuilder list; ygo@v1.50.0 pinned in go.mod/go.sum, additions-only diff):
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
