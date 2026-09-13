# WEB_GO_PLAN — 1:1 drop-in Go replacement of the `services/web` backend

Status: **PLAN** (not started). Companion to `GO_CUTOVER_PLAN.md` (Phase D complete:
the nine microservices are Go-only as of `8090d454fb`).

---

## 0. Purpose, scope, non-goals

Replace the Node/Express `services/web` backend (the Overleaf web app core) with a
Go implementation, **feature-by-feature, 1:1 drop-in**, so that at every moment:

* the same ports, env contract, cookies, JSON shapes, error shapes, and view DOM are
  served;
* each feature can be flipped Node → Go independently, tested, and rolled back
  independently (nginx path-prefix flip + env, no redeploy needed);
* no feature is "half-replaced" in a state the web surface can see.

Target layout (matches the established `cmd/<svc>` + `go/services/<svc>` convention):

```
cmd/web/                    # binary → bin/web → /usr/local/bin/go-services/web
go/services/web/
  core/                     # P0 foundation: server, config, session, csrf,
                            #   validation, rates, views, static, errors, context
  features/
    <feature>/…             # one package per web Feature (50, §4)
  modules/
    <module>/…              # one package per web module (41, §4.5)
```

**In scope:** everything `services/web` serves — both processes (§1), Features,
modules, views, static, queues owned by web.
**Out of scope (separate effort, stays Node for now):** `clsi`, `clsi_typst`,
`real-time`, `document-updater`, `history-v1`, `project-history` — Go web talks to
them over the same HTTP/socket APIs it uses today.
**Explicitly preserved:** the webpack frontend bundle (served as-is by Go), the
locale files (`services/web/locales/*`), template project files, and the
`server-ce` config surface (`/etc/overleaf/env.d`, `settings.js` keys).

---

## 1. Current-snapshot inventory (measured on the tree, 2026-09-16)

### 1.1 Process topology (what "drop-in" must reproduce)

The SAME app source runs as **two runit services** (env-driven profiles):

| service            | run script               | listener          | profile                  | log                    |
|--------------------|--------------------------|-------------------|--------------------------|------------------------|
| `web-overleaf`     | `runit/web-overleaf/run` | `127.0.0.1:4000`  | `ENABLED_SERVICES=web`   | `/var/log/overleaf/web.log`   |
| `web-api-overleaf` | `runit/web-api-overleaf/run` | `0.0.0.0:3000` | `ENABLED_SERVICES=api`, web-api auth (`WEB_API_USER`/`WEB_API_PASSWORD`) | `/var/log/overleaf/web-api.log` |

Both source `/etc/overleaf/env.sh` (+`env.d/`). **Go web is one binary, two
profiles**, selected by the same env — `bin/web` served from
`/usr/local/bin/go-services/web` with identical env, log, user (`www-data`),
ports.

### 1.2 Scale (measured)

* **50 Features** in `app/src/Features/`, ≈ 150k LOC of feature code + infra
  (the `Metadata` feature is 71.8k "lines" but is 3 small files + a 1.7 MB
  package-mapping data payload — logic size is small).
* **41 modules** in `modules/`, ≈ 95k+ app/frontend LOC for the top 20
  (ollitex-hub 19.9k, admin-tools 15k, llm 14.4k, bib-editor 10.2k,
  github-sync 6.2k, template-gallery 5.3k, webdav 4.5k, …).
* **163 core route registrations** in `app/src/router.mjs` (78 GET, 70 POST,
  13 DELETE, 1 PUT, 1 PATCH, 1 ALL-wildcard) **plus** module-registered routes
  — total surface roughly 250–300 routes; exact per-feature attribution is an
  M0 deliverable (§6.2).
* **77 pug views** (server-rendered pages) + 44 mongoose models + 46
  `infrastructure/` files (middleware/clients: Validation, Modules, RateLimiter,
  RedisWrapper, LockManager, Queues, Views, Translate, ServeStatic, SessionManager,
  Csrf, Sanitize, Response, AsyncLocalStorage, Metrics, GracefulShutdown, …).
* **Bull queues** (redis-backed), CE workers: `scheduled-jobs`,
  `emails-onboarding`, `post-registration-analytics`, `deferred-emails` —
  **queue ownership flips with the owning feature; two implementations must never
  consume the same queue** (§7 R5).
* Downstream clients (unchanged contracts, stay Node): clsi / clsi_typst
  (compile), real-time / document-updater / history-v1 / project-history
  (sync+history), and the nine **Go** microservices (chat, docstore, filestore,
  notifications, datamanipulator, webdav, dropbox, github, linked-url) —
  **Go web's handler packages become thin clients of services that are already
  Go**: no protocol re-invention for ~ten handler families.

---

## 2. What "drop-in" means here (runtime contract checklist)

Every flip must keep ALL of these true (this is the gate contract template):

1. **Listeners/ports**: web profile on `127.0.0.1:4000`, api profile on
   `0.0.0.0:3000` — same env, same log paths, same `www-data` user.
2. **Sessions**: shared Mongo session store — Node-written sessions must be
   readable by Go and vice versa (same collection, same document shape, same
   cookie name/format, same `express-session` cookie signature if HMAC is used;
   M1 proves bidirectional interop with a live A/B cookie).
3. **CSRF**: same token generation/storage/verification contract (Node's
   `Csrf.mjs` as spec).
4. **Errors**: same status codes + body shapes — `{"message": ...}` for
   validation (zod/validation-tools shapes), the `Error: ...` prefixes used by
   handlers, and the **Express default 404 HTML page** byte-exact for unknown
   paths (already pinned in `go/pbhttp.ExpressNotFound`, Phase C).
5. **JSON APIs**: 1:1 field-for-field with Node (same key order not required,
   same *set* + types + values required; pin via fixtures).
6. **HTML pages** (77 views): DOM-level parity (structure, classes, ids,
   data-attributes, meta) + asset URLs identical; not byte-exact (allow
   whitespace/attribute-order variance; assert on parsed DOM + served asset set).
7. **Static**: Go serves the SAME webpack output under `services/web/public` —
   no rebuild, no path changes.
8. **I18n**: same `locales/*.json` files parsed by Go web; same `?lang=`/
   cookie-driven selection behavior.
9. **Queues**: per-queue exclusive ownership (env/flag flips the worker with the
   feature).
10. **Audit/metrics**: project/user audit log writes and `@overleaf/metrics`
    emitters preserved for the flipped surface (same metric names + labels).
11. **Web-API auth profile**: `WEB_API_USER`/`WEB_API_PASSWORD` gate on the api
    listener, same as Node.
12. **Graceful shutdown**: runit `sv down` → in-flight drains (mirror
    `GracefulShutdown.mjs` semantics).

---

## 3. Architecture of the migration (how features flip independently)

```
          user / browser
                 │
        ┌────────┴────────┐
        │   nginx (public)│   compose_cep/nginx.conf
        └────────┬────────┘
   path-prefix routing table:
   /Project/… /user/… /admin/… /compile/… /chat/… …
        │                        │
   FLIPPED PREFIX →        NOT YET FLIPPED →
   go web (127.0.0.1:4000)   node web (127.0.0.1:4001 shadow)
        │                        │
        └────────┬───────────────┘
                 ▼
   SHARED: Mongo (sharelatex) · Redis (sessions/rates/locks/queues)
           S3 · /var/lib/overleaf/data/* · downstream Node services
           (clsi, real-time, document-updater, history) · Go microservices
```

Mechanics:

* **M0 builds the route→feature table** (auto-extracted from Node sources into
  `go/services/web/contract/routes.csv` + per-feature `contract.json`): the
  flip unit is a *contiguous path-prefix set per feature*, reviewed once.
* **nginx owns the flip**: one line per feature prefix. `sv`/env are not
  involved in steady-state flips → **rollback = remove line + `nginx -s reload`**
  (< 1 s, no state migration).
* **Shadow port**: Node web keeps serving on its normal port; Go web listens on
  `127.0.0.1:4001` (or `WEB_GO_PORT`), both attached to the same MONGO + REDIS.
  Battery tests hit either directly; the e2e journeys hit through nginx.
* **Never-midflight rule**: anything a single user flow crosses (login → open
  editor → compile) must be flipped only when every leg's owner is in the same
  stack (P0→P1→P2 order in §4 guarantees this).
* **Queue workers** are started by the process whose feature owns them (env
  `WEB_GO_QUEUES=...`); the Node side stops its worker when the feature flips.
  The two stacks (Node web / Go web) are separate processes, so "whoever is up
  with the feature on" is enforced by the same flip record, not luck.

---

## 4. The feature list (the replacement inventory) and phase order

Sizes are measured LOC (app code). Phasing principle: **leaves first** (small,
few cross-refs), then **auth core**, then **user/admin**, then **project/editor
core**, then **modules**, and finally Node-retirement. Each item below is a
candidate flip unit; M0 refines prefix sets where two features share a path.

### P0 — Go web foundation (`go/services/web/core/`) — *no user-facing feature yet*

| component | Node source of truth | notes |
|---|---|---|
| server/http kernel + two process profiles | `Server.mjs`, `app.mjs` env logic | one binary, `ENABLED_SERVICES`-driven |
| config/settings reader | `@overleaf/settings` + `settings.js`/`env.d` | same env names, same defaults |
| context plumbing | `app.locals` (the Node features' global glue) | **explicit per-feature constructors** — the biggest structural change (§7 R7) |
| session store (Mongo) + cookie | `SessionManager.mjs`, `express-session` config | bidirectional interop test = M1 acceptance |
| CSRF | `infrastructure/Csrf.mjs` | same token contract |
| validation | `validation-tools` (zod) usage sites (45 imports) | port the *schemas in use* per feature, not the whole lib |
| rate limiting | `RateLimiter.mjs` (Redis) | **same redis key namespaces** |
| responses/errors | `Response.mjs`, `Errors/` feature, Express 404 | reuse `go/pbhttp` (AuthGate, ExpressNotFound, body helpers) |
| views | 77 pug templates → Go `html/template` + shared components | DOM-parity harness (M1 tooling) |
| static serving | `ServeStatic.mjs` + webpack output | serve `public/` as-is |
| i18n | `Translate.mjs` + `locales/` | same JSON files |
| request logging/metrics | `@overleaf/metrics`, `LoggerSerializers` | same metric names/labels for flipped routes |

**P0 gate**: login page + one static page + `/api` health parity, *session
interop A/B test* (Node login → Go reads session; Go login → Node reads), all
P0 unit tests green, e2e smoke through nginx with **zero** feature traffic on Go.

### P1 — leaf features (small blast radius; each = a quick win + more harness muscle)

| feature | LOC | why safe/notes |
|---|---|---|
| HealthCheck | 83 | trivial |
| SystemMessages | 154 | read-only + admin write; small |
| StaticPages (home/about/privacy/terms) | 161 | pure views; view-parity harness's first real target |
| Tutorial | 182 | |
| SamlLog | 101 | log-only |
| Survey | 99 | |
| BetaProgram | 107 | |
| BrandVariations | 90 | |
| InactiveData | 202 | |
| SocketDiagnostics | 11 | |
| Metadata (package-mapping API + 1.7 MB data) | ~3 files + data | data payload: Go reads the same file |
| TokenGenerator | 98 | |
| OnboardingDataCollection | 77 | **queue** (post-registration-analytics) — first queue-flip exercise |
| SplitTests | 2,392 | assignment endpoints; medium |
| Spelling | 217 | thin client to LanguageTool sidecar |
| References | 43 | thin (bib references proxy) |
| Publishers | 38 | thin list |
| GlobalMetrics | 38 | |
| V1 (legacy compat) | 189 | deprecated surface — parity matters, volume low |
| Contacts | 142 | CRUD small |
| Downloads | 480 | zip assembly — reuse Go zip tooling; pin bytes vs Node |
| Exports | 460 | client of project-history service |
| Tags | 415 | |

### P2 — auth core (*every later phase depends on this*)

| item | LOC | notes |
|---|---|---|
| Session/Authentication core | `Authentication` 1,373 | login/logout/verify/email-verify |
| Captcha | 263 | recaptcha client contract |
| PasswordReset | 478 | token emails (SMTP) |
| Security | 264 | audit of auth |
| TokenAccess | 1,085 | token endpoints (`/:token` route family) |
| Authorization | 1,499 | role checks — used by admin routes |
| modules/authentication/saml + oidc | (module) | SAML/OIDC handshakes — pin redirects |

**P2 gate**: full login/register/logout/reset journeys e2e; Node-created
session usable on Go-served page and vice versa under the flipped prefixes;
admin authz matrix battery.

### P3 — user & admin surfaces

User (4,814: profile, user pages, audit, deleter, updater, saml-identity),
ServerAdmin (254) + admin-tools module (15k, after its API owners flip),
SiteSettings (1,915 — the **EnvHydrator** boot-time settings hydration must be
reproduced: stored settings win over env; this is boot-critical, gate hard),
Institutions (994), Email (2,134 — SMTP via nodemailer-equivalent), Analytics
(1,461 — queue flip), InactiveProjectController + related (inside User/Project;
split in M0), modules: registration-page (1,539), user-activate (2,045),
page-shells (561) + instance-stats (2,409).

### P4 — project core (the heavy centre; flip in listed sub-order)

1. **Docstore** (436) + **FileStore** (422) + **Documents** (304) +
   **LinkedFiles** (1,537) + **Uploads** (1,637) — *thin clients of the Go
   docstore/filestore services already in production*: highest value/effort
   ratio in the whole plan.
2. **Chat** (517) + **Notifications** (576, web-side proxy of the Go chat +
   notifications services).
3. **Project** (7,947: list/CRUD/duplicate/delete/options/audit log writer) —
   split into M0-verified sub-features (list, crud, duplicate, delete, audit).
4. **Collaborators** (3,791).
5. **History** (2,556 — clients of history-v1/project-history services).
6. **ThirdPartyDataStore** (1,189), **Templates** (293) + template-gallery
   module (5.3k), **Launchpad** (1,774).

### P5 — editor & compile (highest coordination cost, deliberately last among core)

Editor (1,418: `/Editor/:id` open + OT plumbing to real-time),
DocumentUpdater (600 — client of the document-updater service),
Compile (4,380 — ClsiManager ↔ clsi service: compile triggers, logs, output
files; the compile *engine* remains clsi/Node; we replace the **control
plane**).
**P5 gate**: full open → edit (shared session with a Node-open second client) →
compile → PDF render journey, plus disconnect/storm soak.

### P6 — modules (41 total; each a self-contained flip, roughly in this order)

ollitex-hub (19.9k — the workspace/admin surfaces; mostly proxies of already
-flipped P3/P4 endpoints + its own HubController), admin-tools (15k — lands
after P3), llm (14.4k — external-provider client + rate limits + BYO-key crypto),
bib-editor (10.2k), github-sync (6.2k — client of the **Go** githubinterface),
webdav (4.5k — client of the **Go** webdavinterface), dropbox (2.7k — client of
the Go dropboxinterface), zotero (2.5k), mendeley (1.3k), orcid-picker (1.1k),
typst (1.3k), python-runner, languagetool (2k), notifications module (1.7k —
preferences over the Go notifications service), diagram, latex-editor,
webdav, github-sync, ce-ui, page-shells, server-ce-scripts,
registration-page, saml/oidc (with P2), toast-image, …
(Exact set = M0 output from `modules/*/index.mjs` + `SaaSModule`/`CEUI`
registries.)

### P7 — Node retirement

With all prefixes flipped and ≥ 24 h soak each: point both runit services at
`bin/web` (exactly what Phase D did for the nine services), delete
`services/web` (keep `public/` bundle, `locales/`, template project files,
`test/` → re-homed), prune `package.json`/`services.js`/compose like Phase D,
rebuild image, cycle both live stacks, full e2e suite green, commit record.

---

## 5. Per-feature gate method (proven in Phase C — reuse as-is)

For each flip unit (P1…P6 items):

1. **Contract pin (Node as baseline)**: from the route table, extract the
   feature's routes; record request→response fixtures (status, headers subset,
   body shape, error shapes) by hitting Node web with a replay/fixture set
   (`tests/e2e/specs/webgo/<feature>.test.e2e.ts`, same shape as
   `service-linked-url.test.e2e.ts` — offline-safe, loopback mocks for
   external APIs: SMTP sink, LanguageTool, SAML IdP mock, clsi stub).
   Node leg **must be green first** (the contract is Node).
2. **Go implementation** under `go/services/web/features/<x>/` (+tests,
   httptest + memstore, same house style as the nine services).
3. **Go leg green against the identical fixtures** on the shadow port — same
   battery, zero drift (status/body/headers).
4. **Flip at nginx** (prefix set), run the **user journeys** (e2e browser
   specs on the live stack), 24 h soak with zero new panic/fatal/warn in
   `/var/log/overleaf/web*.log`, then **next feature**.
5. **Rollback** at any step 3–5: nginx line out + reload; queue flag back;
   (state needs no migration — both implementations read/write the same Mongo
   with the same schemas; the Node-side implementation of the feature still
   exists until P7).

---

## 6. M0 deliverables (start here, ~the first working unit)

1. `go/services/web/contract/routes.csv` — auto-extracted route→feature
   attribution for all ~300 routes (method, path, feature, auth requirement,
   queue owned, downstream services touched) — the flip table.
2. `go/services/web/core/` skeleton: http kernel, config, session, csrf,
   validation (schemas-in-use), rates, views harness (DOM-parity checker),
   static, errors (`pbhttp` reuse), context (per-feature constructors).
3. Shadow-port runner (`cmd/web` with `WEB_GO_PROFILE=web|api`), runit
   `web-go-overleaf/run` (new service, `sv`-managed, **off by default**).
4. Session interop A/B harness (the single most important test in the whole
   project).
5. nginx flip-table stub + documented flip/rollback SOP (one page, in
   `WEB_GO_PLAN.md` §3 — keep it live-updated).
6. **Go green, zero traffic, e2e smoke unchanged** → P0 complete.

---

## 7. Risk register (web-specific)

| # | risk | mitigation |
|---|---|---|
| R1 | **session interop is the linchpin** — any drift breaks every user on every flip | dedicated A/B harness in M0; cookie/token formats pinned by fixture; P2 gate is the interop suite |
| R2 | **`app.locals` global glue** — Node features read shared singletons implicitly; Go must reproduce the same read/write order | core context carries the *same data* with documented per-feature constructors (§2.11 pattern); M0 attributes every `app.locals` read to a feature |
| R3 | **SiteSettings EnvHydrator** (boot-time stored-wins env mutation) leaks into boot order | Go core hydrates before any handler init; fixture-compare the hydrated env between Node and Go |
| R4 | **audit log dual writers** (Project/User AuditLog handlers) if a flip leaves two writers for one action | each audit write is owned by exactly one stack: the flip record assigns; e2e asserts single entries |
| R5 | **queue double-consumption** (bull) across Node/Go | queue ownership flag flips with the feature; never both at once; drained before flip |
| R6 | **pug vs Go template drift** (77 views, mixins, i18n conditionals) | DOM-parity harness (parsed-DOM compare + asset URL set); views are flip *with* their feature |
| R7 | **compile control plane** split-brain during P5 with clsi still Node | clsi API contract already HTTP; pin compile trigger/log/output fixtures; engine untouched |
| R8 | **module coupling**: modules import from core Features (llm→User, hub→everything) | module flips come after their dependency features (P4 before P6) |
| R9 | **env/cookie drift between the two stacks** after env.d edits | same `env-diff` pattern as Phase C (`tools/service-parity`) adapted: env + cookie + config fixture snapshot per cycle |
| R10 | **webpack bundle served by Go** — path/mime/cache-header drift | asset URL set parity asserted in the DOM harness; same `public/` dir |

---

## 8. Honest framing

This is the **largest** replacement in the program (≈ 250k+ LOC of Node app +
95k+ modules vs. the nine services combined at ≈ 30k LOC) and it must move in
small, gated flips. The nine-services cutover already paid for the hard parts:
the gate method (§5), the shared HTTP helpers (`pbhttp`), the mongoh URI chain,
the S3 helpers, the image-bake ritual, and — most of P4's target surface —
services that are **already Go**. Estimated order-of-magnitude if run the same
way: P0 in the first working sprint; P1/P2 a few flip-units each across the
next sprints; P3–P6 long-tail (modules dominate the count but are small and
independent); P7 only after 100% prefix coverage + soak.

Nothing in this plan blocks the nine-service steady state; it starts at M0 and
touches the user only via the nginx flip table — which is exactly the property
that made Phase C safe.
