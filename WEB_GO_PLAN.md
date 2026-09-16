# WEB_GO_PLAN — 1:1 drop-in Go replacement of the `services/web` backend

Status: **IN PROGRESS** — P0+M0 ✔ (6/6), P1 ✔ (3/3), P2 ✔ (4/4), **P3.1 ✔ (3/3), P3.2 ✔ (3/3), P3.3 ✔ (3/3), P3.4 ✔ registration-page (3/3),**
all 2026-09-14); **P3.5 user-activate = OUT OF SCOPE (SaaS, not ported); P3.6 SiteSettings ✔ GATE 3/3 GREEN** — **all of P3 complete.** **P4.1 project-list ✔ 3/3; P4.2 project-entities ✔ 3/3; P4.3 project-members ✔ 3/3; P4.4 access-requests ✔ 3/3; P4.5 project-rename ✔ 3/3; P4.6 project-flag-writes ✔ 3/3; P4.7 basic project-creation (`POST /project/new`) ✔ 3/3; **P4.7b example project-creation (`template: "example"`) ✔ 3/3 — both `basic` + `example` templates done**; **P4.8 project delete/restore (`DELETE /Project/:id`, `POST /Project/:id/restore`) ✔ 3/3 — deletedProjects record + $unset-archived contract byte-pinned**; **P4.9 project clone (`POST /Project/:id/clone`) ✔ 3/3 — incl. Node's missing-name→500 quirk + per-edit `version` counter pin**; **P4.10a collaborator mutations** (`PUT /project/:id/users/:uid` set-level, `POST /project/:id/leave`, `DELETE /project/:id/users/:uid`, access-request decline/grant, `POST /project/:id/transfer-ownership`) **✔ 3/3 — setLevel $pull+$addToSet+$set tc contract, 8-mail battery, transfer flush+contacts, byte-pinned VA errors** (all 2026-09-14/15); **P4.10b invites + sharing-links + token-acceptance ✔ 3/3 x3 (10 routes, 6-mail battery, sink-token live-selection, invite shell / Invalid-404 / restricted-403 views, raw-SMTP mail byte-parity)** — **ALL OF P4 (project-entities surface + collaborators + invites) COMPLETE: 36/36 regression green** (2026-09-15); **P4.11a editor entity creation (`POST /project/:id/doc` + `/folder`) ✔ 3/3 x3 (SafePath replica, docstore call-order pin, folder-JSON/doc-text 400 split, blocked-word table) — 39/39 P4 regression green**; **P4.11b editor entity deletion (`DELETE /project/:id/{doc,file,folder}/:entity_id`) ✔ 3/3 x3 ($pull + $inc + $set + conditional $unset rootDoc_id, per-subtree-doc docstore PATCH with 404-after-write quirk, 422 root-folder guard, params-VA 404-JSON) — 42/42 full P4 regression green**; **P4.12a file proxy (`GET|HEAD /Project/:id/file/:File_id`) ✔ 3/3 x3 (history-v1→Go-filestore blob chain, no-CT/chunked 200 pin, HEAD-404 fork quirk, guest/VA/authz battery) — ALSO FIXED the baked-in-capture-user bug in `restrictedHTML` (Go 403-restricted pages now render the requesting user; a P2-page variant of the same bug is deferred to a view-audit unit)**; **P4.12b doc download (`GET|HEAD /Project/:id/doc/:Doc_id/download`) ✔ 3/3 x3 (DU fromVersion=-1 lines, CD attachment, HEAD-200 asymmetry, sendStatus-404 pin) — 45-test P4 regression green** (2026-09-15). **P4.12c private API doc trio (GET|POST doc + changes/reject) ✔ 3/3 x3 (basic-auth surface, XPB+CSP pin, `NoSession` route option, 204 ETag quirk) — 47-test P4 regression green**; **P4.13a project upload (`POST /Project/:id/upload`) ✔ 3/3 x3 (thin-client upsert engine: docstore/v1H/DU call order, file↔doc swaps, cross-type 200+422/400/403/404/500 contract, `owner_ref`/docstore-seed env pins, `entCanWrite` collabRefs + 500-mailto parity fixes) — 65-test cross-regression green**; **P4.13b new-project zip upload (`POST /project/new/upload`) ✔ 3/3 x3 (FileSystemImportManager replica: zip-skip→topLevel-dir strip, FileTypeManager byte-level parity incl. utf16le-BOM + latin1 fallback + non-BMP→file + 3MiB decode gate, rootDoc priority pin, enforce-mode VA byte-pins, blocked-name `toString` → 500 + `zip-import-failure` deleter, multer `LIMIT_UNEXPECTED_FILE` HTML page, 22-case response+mongo+DU+docstore state battery) — FULL P4 regression green (36 p4* + 23 p411–p413b + P2/P3 sweep)** (2026-09-15). **P4.13b ALSO FIXED the P4.1 list owner+collaborator dedup bug (user who is owner AND invited-collaborator listed twice by Go; Node dedups best-access — latent, exposed by p4col-gate leftover projects) and hardened p413a/p413b/p413 flip harnesses against stale-include cascade failures.** **ALL OF P4 COMPLETE (2026-09-15): project list + entities + members + access-requests + rename + flags + create(+example) + delete/restore + clone + collaborators + invites + editor entity create/delete + file proxy + doc download + private doc trio + project upload + zip upload.**
Companion to `GO_CUTOVER_PLAN.md` (Phase D complete: the nine microservices are
Go-only as of `8090d454fb`). P4.3–P7 to come.

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

### P0 — Go web foundation (`go/services/web/core/`) — **✔ COMPLETE 2026-09-13**

> Status (2026-09): core + `features/{status,healthcheck,devcsrf}` + `cmd/web`
> built; shadow service `web-go-overleaf` (127.0.0.1:4010) runit-managed, OFF
> by default (nothing routes to it until a flip is applied); flip table
> `server-ce/nginx/flips/web-p0.conf` + `web-go-flip` runit service (gated by
> `FLIP_GO_WEB_P0=1`). P0 three-leg gate GREEN (5/5, no flakes):
> `tests/e2e/specs/parity/web-go-p0-flip.test.e2e.ts` — shadow up, A-parity
> (/status byte+header exact incl. pinned ETag), **A/B session interop both
> directions with token cross-verification against the shared redis doc**, and
> flip ON/OFF routing (`/health_check/redis` 404→204 flip proof, stock
> restored on strip). Route→feature table: `go/services/web/contract/routes.csv`
> (159 routes + module routers). Contract pins learned in P0 (BINDING for all
> later phases):
>
> 1. **Sessions live in REDIS** (connect-redis 6.1.3, `sess:<sid>` keys), not Mongo.
>    Doc = {cookie{originalMaxAge 432000000,expires…}, csrfSecret,
>    validationToken `v1:`+sid[-4:], passport.user when logged in}; NX-on-create
>    / XX-on-touch SET semantics.
> 2. **Cookie = `s:<sid32>.<sig>`**, sig = std base64 (NOT url-safe) of
>    HMAC-SHA256(key=secrets[0], msg=sid) with trailing `=` stripped — the
>    stack-resolved cookie-signature; unsign = last-dot split + recompute.
> 3. **csurf**: token = 8-base62-salt + `-` + b64url(SHA1(salt+`-`+secret));
>    secret (24 b64url chars) is LAZILY allocated into the doc on first use;
>    403 = `Forbidden` text/plain, no nosniff.
> 4. **Session cookie is issued on every webRouter request** (rolling touch),
>    but NOT on publicApiRouter/privateApiRouter routes (/status, /health_check/*
>    set no cookie) → `Route.NoSession` in core.
> 5. Node persists sessions **after** the response (fire-and-forget) — interop
>    probes must wait for the redis doc.
> 6. nginx `location =` exact matches outrank the stock `location ~* ^/health_check`
>    404 block — that is the flip mechanism (see §3 update below).
>
> Original table (kept for reference):

| component | Node source of truth | notes |
|---|---|---|
| server/http kernel + two process profiles | `Server.mjs`, `app.mjs` env logic | one binary, `ENABLED_SERVICES`-driven |
| config/settings reader | `@overleaf/settings` + `settings.js`/`env.d` | same env names, same defaults |
| context plumbing | `app.locals` (the Node features' global glue) | **explicit per-feature constructors** — the biggest structural change (§7 R7) |
| session store (REDIS, connect-redis) + cookie | `CustomSessionStore.mjs`, `express-session` config | bidirectional interop test — **done in P0 gate** |
| CSRF | `infrastructure/Csrf.mjs` | same token contract |
| validation | `validation-tools` (zod) usage sites (45 imports) | port the *schemas in use* per feature, not the whole lib |
| rate limiting | `RateLimiter.mjs` (Redis) | **same redis key namespaces** |
| responses/errors | `Response.mjs`, `Errors/` feature, Express 404 | reuse `go/pbhttp` (AuthGate, ExpressNotFound, body helpers) |
| views | 77 pug templates → Go `html/template` + shared components | DOM-parity harness (M1 tooling) |
| static serving | `ServeStatic.mjs` + webpack output | serve `public/` as-is |
| i18n | `Translate.mjs` + `locales/` | same JSON files |
| request logging/metrics | `@overleaf/metrics`, `LoggerSerializers` | same metric names/labels for flipped routes |

**P0 gate (met 2026-09-13)**: session interop A/B (Node session → Go reads +
token verify; Go session → Node reads + token verify), /status + health checks
+ /dev/csrf flip ON/OFF green through public nginx, core unit tests green
(10 pinned-crypto/semantic tests), 16/16 e2e regression sample green, shadow
OFF by default = zero feature traffic on Go. (Login page parity itself lands
with P2-auth, as planned — its interop half is already pinned by leg3a/3b.)

### P1 — leaf features (small blast radius; each = a quick win + more harness muscle)

**✔ DONE 2026-09-13** — views engine (`go/services/web/views/`), `authpages`
(login/POST·logout/restricted/register), `staticpages` (`/`, marketing 301s),
`systemmessages`. Green: Go unit tests, 33/33 A/B battery direct-vs-direct,
and the **3-leg flip-gate spec** `tests/e2e/specs/parity/web-go-p1-auth-flip.test.e2e.ts`
(Node baseline / FLIP ON identical battery through nginx + A/B interop both ways +
redis doc / FLIP OFF reversal). Contract pins (all pinned live vs Node):
cookie
**Expires-only, no Max-Age** + `s%3A…` wire encoding + Node-cookie-undecode on
read; view `siteUrl`/origin from **`OVERLEAF_SITE_URL`** (settings.siteUrl, not
proxied Host); **regeneration rides the login response** (`CommitSess`);
`loginRedirectTarget` always `/login`; 401 body `Unauthorized`; anon
`/restricted` XHR=401 vs page=302; `/system/messages` anon=`[]`; marketing gated;
CSP per layout (React nonce vs restrictive); 404 view PATH/OLUSERS/OLUID slots;
`loginEpoch` filter **type-sensitive** (no int64 coercion → 429); shadow needs
`WEB_PORT=4010`; gate compares Set-Cookie **shape** with nonce/csrf/sid
normalized (nonce class must include `+ / =`); redis key for the cookie sid =
sid **minus** the `.sig` suffix.

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

**Status (2026-09-13): implemented + gating.** Scope actually gated in P2
(the rest — captcha, SAML/OIDC, admin authz — rides P3/P4 with their
features):

| unit | surface | state |
|---|---|---|
| PasswordReset | GET/POST `/user/password/reset`, GET/POST `/user/password/set`, POST `/user/reconfirm` | flip-gated (`web-p2.conf`) |
| TokenAccess | GET `/<rw-token>`, GET `/read/<ro-token>`, POST `…/grant` (×2) | flip-gated |
| Sharing-updates consent | GET `/project/:id/sharing-updates`, POST `…/join`, `…/view` | flip-gated |

Contract pins added by the P2 gate (Node = oracle, pinned live 2026-09-13):

- **Global login gate** (`router.mjs:215`, `allowPublicAccess` default off):
  every non-whitelisted route answers anon with 401
  (`WWW-Authenticate: OverleafLogin`, `text/plain` `Unauthorized`,
  CSP default policy) for `accepts-json`, else 302 `/login`
  (`Found. Redirecting to /login` body, session cookie,
  `session.postLoginRedirect` stashed). Whitelist (CE core):
  `/login`, `/login/legacy`, `/read-only/one-time-login`, `/register`,
  `/system/messages`, `/user/password/reset`, `/user/password/set`,
  `/user/activate`, launchpad. **NOT whitelisted**: `/logout`,
  `/restricted`, marketing 301s, the token grant family, reconfirm,
  sharing-updates — all gate first. Go: `Route.NoLogin` flag +
  `Cfg.AllowPublicAccess` (env `OVERLEAF_ALLOW_PUBLIC_ACCESS`).
- **429 rate-limit response**: NO content-type header, body
  `Rate limit reached, please try again later` (pinned via the P2 gate:
  Node's 429 has an empty content-type).
- Grant `checkAndGet` order: lookup → **token gate** (`tokenBased` else
  404) → anon branches → higher-privilege shortcut → proceed.
- `sendStatus(200)` success body is `OK` + newline (express `res.send`
  appends `\n`); 204 moves are body-less.
- **set POST success CT is `text/plain; charset=utf-8`** (live-pinned on
  both stacks — NOT the naive `sendStatus` html default; the gateway
  battery originally diffed exactly this).
- **set POST missing-field 400**: zod fires on a MISSING key → body
  `{"error":"Validation error: Invalid input: expected string, received
  undefined at \"body.passwordResetToken\"","statusCode":400}` (field =
  the absent required one; `password` first); PRESENT-but-empty hits the
  handler branch → `400 {"message":{"key":"invalid-password"}}`.
- `validatePassword` order: too-short → too-long → invalid-character →
  contains-email.
- Session cookie `Expires` (absolute) legitimately differs per leg — gate
  normalizes it; nonce in CSP + bodies is normalized per-response.

**P2 gate** (`tests/e2e/specs/parity/web-go-p2-flip.test.e2e.ts`,
`server-ce/nginx/flips/web-p2.conf`): 3-leg battery — (1) Node baseline,
(2) FLIP ON (Go must match byte-for-byte after nonce/csrf/expires
normalization), (3) FLIP OFF (reversal) — covering pages, the reset
matrix, the set-password validation matrix, the reset SUCCESS round-trip
(with password restore), token pages, the grant matrix, the consent
page/moves, and the 429 burst; the smtp-sink leg asserts both stacks sent
the reset mail. Budget sections rest 66s between (password_reset 6/60s
shared window); fixture token refs + password are restored in teardown.

**P2 gate result (2026-09-13): 4/4 GREEN** — leg 1 Node baseline + leg 2
FLIP-ON Go parity (0 diffs after nonce/csrf/expires normalization) + leg 3
FLIP-OFF restore + leg 4 smtp-sink (≥5 reset mails, both stacks) pass;
e2e-user password restored to original; stack left at Node-active baseline
(shadow service stopped, flip stripped).

Remaining P2 leftovers (ride later phases): captcha, SAML/OIDC handshakes,
admin authz matrix, `/user/activate` (user-activate module, P3).

### P3 — user & admin surfaces

User (4,814: profile, user pages, audit, deleter, updater, saml-identity),
ServerAdmin (254) + admin-tools module (15k, after its API owners flip),
SiteSettings (1,915 — the **EnvHydrator** boot-time settings hydration must be
reproduced: stored settings win over env; this is boot-critical, gate hard),
Institutions (994), Email (2,134 — SMTP via nodemailer-equivalent), Analytics
(1,461 — queue flip), InactiveProjectController + related (inside User/Project;
split in M0), modules: registration-page (1,539), user-activate (2,045),
page-shells (561) + instance-stats (2,409).

#### P3.1 — ServerAdmin leaf (editor-state + system messages) — **✔ COMPLETE 2026-09-14**

Go: `features/serveradmin/` (7 routes: `GET /admin/editor-state`,
`POST /admin/openEditor|closeEditor|messages|messages/clear`,
`PATCH|DELETE /admin/messages/:id`) + core helpers — `edstate.go` (tri-state
`closeEditor {}` = site closed + editor OPEN: `editorIsOpen=undefined`→true),
`headers.go` (`setWebBaseline` at request time after the csrf check),
`authorize.go` (`RequireSiteAdmin` → 302 `/restricted?from=…`), `response.go`
(`Redirect` Accept matrix: explicit `text/html`→`<p>…</p>`; `text/*`/`*/*`→
plain; no Accept→plain; other→**empty body, no Content-Type**; all 302s
`Vary: Accept`, no ETag) + `core.BareWrite` (express.json-rejection 400s carry
NONE of the web-baseline: no CSP/nosniff/Set-Cookie — pinned live).

Key pinned/learned behaviors (e2e mongo `sharelatex`):
- **Mongo collection is `systemmessages`** (mongoose auto-pluralization of
  model `SystemMessage`), NOT `system_messages` — the P1-era Go code used a
  shadow collection; fixed in P3.1 (systemmessages.go + serveradmin.go).
- **Non-object JSON roots** (`5`, `"str"`, `true`, unparseable) → express.json
  strict rejection → **400 `{}`** with no baseline headers and NO set-cookie;
  object/array roots reach zod → verbose 400 (`expected object, received
  array` for `[1]`); `null` → `received null`. Go: `readBody` kind =
  scalar/array/null/object, `BareWrite` for the bare shape.
- Anonymous `/system/messages` → always `[]` (controller short-circuit);
  logged-in → cached manager list (pubsub `refresh-system-messages` OR
  20–30s background refresh; `NOTIFY_ON_SYSTEM_MESSAGE_CHANGES=true` needed
  for the pubsub path). Go mutations `PUBLISH` the same channel so the Node
  leg's cache refreshes (cross-stack parity in both flip directions).
- Route params: Go patterns use named groups (`(?P<id>[^/]+)`) — the core
  dispatcher maps `SubexpNames()` → `cxt.Params["id"]`.
- **Shadow-binary gotcha**: the runit shadow runs the CONTAINER copy
  `/usr/local/bin/go-services/web` — every change needs
  `go build -o bin/web ./cmd/web && docker cp bin/web ol-e2e-overleaf-1:/usr/local/bin/go-services/web
  && sv restart web-go-overleaf`. Also: after `sv restart`, the previous process
  may still own :4010 briefly (graceful-shutdown overlap) — wait for the new pid
  before driving it (observed phantom 404s during the overlap window).

**P3.1 gate** (`tests/e2e/specs/parity/web-go-p3a-flip.test.e2e.ts`,
`server-ce/nginx/flips/web-p3a.conf`): 3-leg battery — (1) Node baseline, (2)
FLIP ON Go parity, (3) FLIP OFF reversal — covering authz probes (anon /
non-admin bounce + 403 csrf), the editor-state tri-state sequence, the create
validation matrix (zod issue strings verbatim incl. unknown-key ORDERING),
PATCH matrix (xhr 200 `success` vs plain 302), DELETE matrix (200 / 500 page /
302), clear + final slate; header set incl. CSP (nonce-normalized), ETag
(skipped only for fresh-`_id` list bodies), Set-Cookie (sid/expiry
normalized).

**P3.1 gate result (2026-09-14): 3/3 GREEN** — leg 1 Node baseline + leg 2
FLIP-ON Go parity (0 diffs after nonce/sid/expires normalization) + leg 3
FLIP-OFF restore; stack left at Node-active baseline (shadow armed, flip
stripped, messages cleared, editor open).

#### P3.2 — instance-stats web leaf — **✔ COMPLETE 2026-09-14**

Go: `features/instancestats/` (routes flipped:
`GET /admin/instance-stats` → 301 `/hub#/site.general.stats`; `GET …/api/series`;
`GET|PUT …/api/alert-config`; `POST …/api/send-test-alert-email`) + core:
`core.NewMail()` (env-driven `OVERLEAF_EMAIL_SMTP_HOST/PORT/SECURE` +
`OVERLEAF_EMAIL_FROM_ADDRESS`, envelope = local address of `Display <addr>`,
`Client.Data()` writer MUST close to terminate the DATA phase — otherwise the
next send fails with `250 "2.0.0 OK accepted"`), `core.SendStatus` parity, and
the **X-Powered-By scoping fix**: Node emits `X-Powered-By: Express` ONLY on
`/status` 200s, csrf-403s (`res.sendStatus`) and express.json-rejection 400s
(`BareWrite`) — it was wrongly global in Go until P3.2 (removed; re-added at
those three sites). `features/status` now owns the status xpb.

Pinned node contract (p32pin.json; probe diff 0/42 cases):
- 301 Accept matrix = the 302 matrix (status-independent rules): html→`<p>Moved
  Permanently. Redirecting to …</p>`; text/plain / text-star / starstar / none→
  plain; json|xml→**empty body, NO Content-Type**; `Vary: Accept`, no ETag.
- anonymous: json accept→**401** `text/plain` + `WWW-Authenticate: OverleafLogin`
  (via `res.SendStatus`, no xpb); else 302 `/login`.
- non-admin: 302 `/restricted?from=%2F…` (percent-encoded path).
- series: `{metric, window, points:[{day: epochMsUtcMidnight, values}]}`;
  windows day/week/month/6m/year/all (all = `INSTANCE_STATS_RETENTION_DAYS ||
  365` — Go guards `n > 0`); unknown/empty metric or window → 400
  `{"message":"Invalid metric|window"}`; **duplicate query params = 400**
  (Node `req.query.x` becomes an array → fails `typeof === 'string'`).
- alert-config GET defaults `{"alertEmails":[],"alertEmail":"",
  "diskWarningPercent":90,"ramWarningPercent":90}`; PUT validates emails first
  (split `[\s,;]+`, dedupe, EMAIL_RE) then disk then ram (numbers in [1,100];
  **non-integers legal**, stored as BSON doubles, round-tripped verbatim);
  success `{"ok":true}`; errors `{"message":"…"}`; `emails` wins over legacy
  `email`.
- test alert: invalid → `{"message":"Invalid email address: <bad>"}` (no
  recipients: no suffix); success `{"ok":true,"sentTo":[…]}`; subject
  `[Overleaf] Instance stats alert test`; one mail per recipient.

**P3.2 exposed + fixed a Go session-core divergence (P0 interop regression):**
express-session KEEPS the cookie's id when a validly-signed cookie arrives
with a **missing doc** (lazy-csrf 403 then writes `sess:<cookie-sid>`; the
spec's D2 cross-check proved it live). Go `StartAnonymous` was re-randomizing
the sid (`newAnonymousSession`); fixed to `newAnonymousSession(sid string)` —
`""` keeps fresh-id semantics (no cookie / bad signature / `Fresh()`
regeneration), cookie-id otherwise.

**P0 spec staleness (fixed in the P0 spec):** since the P2 wire-encoding
change both stacks percent-encode Set-Cookie (`s%3A…`); the P0 leg3b D2 slice
(`gSigned.slice(2,34)`) must **decodeURIComponent first**, and the cookie sent
to Node for the cross-check goes **verbatim** in wire form (double-encoding it
broke the signature and the 403 looked like a token reject).

**P3.2 gate** (`tests/e2e/specs/parity/web-go-p3b-flip.test.e2e.ts`,
`server-ce/nginx/flips/web-p3b.conf`): 3-leg battery (~37 cases) — page 301
Accept matrix (incl. the no-Accept raw-socket row fetch can't express), anon
401/302 authz, non-admin bounces, series validation + all 7 windows,
alert-config 400 matrix + float/int round-trips, csrf-403 (xpb Express),
test-alert mail battery with **per-leg SMTP-sink deltas** (3 mails/leg,
recipients asserted; multiset-diff tolerant of pre-existing probe mail).

**P3.2 gate result (2026-09-14): 3/3 GREEN** (38s) + **full regression
matrix all GREEN on the final binary**: P0 6/6 · P1 3/3 · P2 4/4 (17.7m —
rate-limiter sleeps are real) · P3.1 3/3 · P3.2 3/3. E2e left Node-active
(flip stripped, shadow armed on :4010, config defaults 90/90, messages 0,
instanceStats seed docs intact).

#### P3.3 — user settings + sessions family — **✅ GATE 3/3 GREEN (2026-09-14)**
Scope: `GET /user/settings` (React page), `POST /user/settings` (zod-strict
updates incl. ace/zotero/keybindings + email branch), `GET /user/sessions`
(plain page), `GET /user/sessions/list` (JSON), `POST /user/sessions/clear`
(201 + security mail + audit + other-session purge).

**Gate green (flip `web-p3c.conf`, spec `web-go-p3c-flip.test.e2e.ts`)**:
leg1 Node baseline battery (22+ cases) → leg2 FLIP ON byte-for-byte match →
leg3 FLIP OFF reversal; per-leg pins: exactly 1 clear-mail
(`Overleaf security note: active sessions cleared`) + 2 clear-sessions audit
entries (Node-shape `{userId:ObjectId, operation:"clear-sessions"}`) per leg.
Full matrix same day: P0 5/5 · P1 3/3 · P2 4/4 · P3.1 3/3 · P3.2 3/3
(one 429 rate-limit flake on a P3.3 leg in the matrix run re-ran green).

**Live bugs found + fixed in the P3.3 leg (all parity-breaking)**:
1. **`UserSessions:{<uid>}` key split-brain** — Node's set key is
   `UserSessions:{6aa4b8b5…}` (cluster hash-tag **with** the curly braces,
   `UserSessionsRedis.sessionSetKey`); Go had `UserSessions:<uid>` → Go and
   Node addressed **different** redis sets (Go lists never saw Node's
   sessions and vice versa, clears purged the wrong set). Pinned from redis
   MONITOR; fixed in `core.UserSessionsKey`.
2. **go-mail TLS default hard-failed plaintext sinks** — go-mail v0.8.1
   defaults to `TLSMandatory`; the e2e smtpsink is plaintext →
   `dial failed: STARTTLS mode set to "TLSMandatory"…` and the sessions-clear
   mail silently dropped. Fixed: `goma.WithTLSPolicy(TLSOpportunistic|
   TLSMandatory)` per `secure` env (nodemailer `secure:false` semantics);
   mail errors are now logged (Node: log & continue, never 500).
3. **login response leaked TWO Set-Cookies** — the pre-handler pass queued
   the old sid's cookie, then regeneration issued the new one (Node sends
   exactly one: the new sid). `core.CommitSess` now revokes the pre-queued
   Set-Cookie before issuing the regenerated one.
4. **`customKeybindings` saved as a k/v ARRAY** — Node's mongoose Map shape
   is a `{key:value}` document; the array shape broke Node's own `user.save()`
   (500 on every later save). Fixed: Go writes `map[string]string`.
5. **audit doc typing** — Go stored `userId`/`initiatorId` as hex strings +
   string timestamp; Node stores ObjectIDs + BSON date. Fixed to
   `primitive.ObjectID` + `time.Time` (audit count queries are ObjectId-
   shaped).
6. **`json.Marshal` HTML-escapes `<`** — the `<=255 characters` validation
   message rendered as `\u003c=255`; Node's JSON.stringify keeps a literal
   `<`. `validationError` now JSON-escapes without the HTML set.
7. **`GET /user/sessions/list` empty list** — Node always emits `[]`; Go's
   nil slice marshalled to `null`. All callers now get a non-nil slice.
8. **`X-Powered-By` on rendered views** — Node's `res.render` views carry NO
   xpb (only `res.send`/`res.json`/`sendStatus` do); removed from the Go
   view writers (asserted in `pages_test.go`).
9. **express.json body gate in core BEFORE csrf** — non-object/array JSON
   roots (42, string, boolean, null, unparseable) → bare 400 `{}` with none
   of the web-baseline headers (even anonymous); array roots pass the parser
   → csrf 403 / handler zod 400. Pinned: `core.Res.BareWrite(400,"{}")`.
10. **validation is multi-issue** — zod reports **all** issues joined by
    "; " in SCHEMA definition order (then unrecognized keys in body order);
    the first-error-wins draft was wrong (live-pinned against Node).
11. **keybindings have NO validation length/count limits** (65/66 valid
    entries → 200); the `slice(0,64)` + value filter (non-empty key, null
    or 1..24 char string) is SAVE-time only.

**Node oracle pinned live (report `/tmp/p33-pin-node.json`)** — key contracts:
- anon: HTML GETs 302→/login; JSON Accept → 401 "Unauthorized" (text/plain);
  POSTs without csrf → 403 "Forbidden" (csrf before the login gate).
- `POST /user/settings {}` → **200 body `OK`** (text/plain; charset=utf-8);
  unknown key → 400 `Validation error: Unrecognized key: "X" at "body[.zotero]"`
  (application/json); wrong type → 400 `expected <T>, received <U> at "body.…"`;
  non-object JSON roots: object/array/null through the parser (array/null → zod
  400), scalar → body-parser 400 **HTML** error page (705B) — same split as P3.1.
- `email` branch: own email → 200; malformed → 400 "Bad Request" (text/plain);
  full email-change flow deferred (UserEmails module family, later P3 leaf).
- sessions: list JSON `{currentSession:{ip_address,session_created},
  sessions:[…]}`; **clear → 201 "Created" (text/plain)**; cleared sibling
  session then 401; **repeat clear → 201 again (mail every time)**; audit op
  `clear-sessions` in `userAuditLogEntries`.
- **Node fault contract found live**: a single corrupt `sess:*` doc in the
  `UserSessions:{uid}` set makes ALL sessions endpoints 500 (JSON.parse in
  `getAllUserSessions`) — Go must replicate (and gate it deterministically).

**P3.3 debug detour (recorded)**: Node `POST /user/login` 403 storm was
probe-side (route is `POST /login`; csrf header is `x-csrf-token`; cookie wire
form must stay percent-escaped verbatim); a sh-quoting bug in a pin helper had
stored one unquoted session doc — cleaned (1 doc + 3 stale sets), Node
restored. No Node source changes (container copy verified byte-equal before
revert).

#### P3.4 — registration page (module `registration-page`) — **✅ GATE 3/3 GREEN (2026-09-14)**
Scope: `GET /register` (React shell, anon + logged-in) + `POST /register`
(`ensureRegistrationEnabled` → rateLimit(5/60, bucket `getUserId(req)||req.ip`)
→ create user + 7-day `password` one-time token + activation mail). Node
source: `modules/registration-page/app/src/{RegistrationPageRouter,
RegistrationPageController,UserRegistrationHandler}.mjs` +
`app/src/Features/User/UserRegistrationHandler.mjs`.

**Go implementation**:
- Routes: `go/services/web/features/registrationpage/registrationpage.go`
  now owns **both** `/register` routes (the duplicate `GET /register` was
  removed from `features/authpages/authpages.go`); wired in `cmd/web/main.go`.
- `views.RegisterPage` + anon skeleton (`go/services/web/views/pages.go`,
  `pages_data.go`, `testregister_anon.html`, `register_rendertest_test.go`) —
  byte-pinned to the live Node register page (nonce/CSRF/auth-state slots).
- `core/tokens.go` gained `OneTimeTokens.NewWithExp` (7-day `password` token;
  the existing `New` delegates with the prior 1h default).
- `features/registrationpage/userdoc_gen.go` embeds Node's 42 static user-doc
  defaults (int32 numerics); dynamic fields injected at runtime.
- **`core.Send429` made wire-exact** (shared by P2 + P3.4): Node emits the
  rate-limit 429 via `res.status(429); res.write(…); res.end()` → **chunked,
  no Content-Type, no Content-Length**; Go now forces `Transfer-Encoding:
  chunked` + empty CT (so net/http drops the auto Content-Length). P2's diff
  ignores CL/TE so it stays green; P3.4's diff asserts it.

**Node oracle pinned live (report `/tmp/p33pin.mjs` / reg pins)** — key
contracts:
- `GET /register` anon (enabled) → 200 text/html (React shell; nonce ×23,
  `ol-csrfToken`, auth-state fields); logged-in inserts `sessionUser`.
- `POST /register` no-csrf → 403 text/plain `Forbidden` (core csrf runs
  before the rate-limit; does not consume budget).
- validation (controller order): first/last non-string/`>100` → 400
  `Too long name.`; unparsable/`>254`/no-`@` email → 400 `Invalid email
  address.`; disallowed domain → 403 `Registration is not available for this
  email domain.`; existing email (holdingAccount=false) → 409
  `{"message":{"key":"account_with_this_email_exists"}}`.
- happy → 200 `Registration successful. Please check your email to activate
  your account.` **+ creates** user (random 32-byte-hex pw → bcrypt, `emails[]`,`
  signUpDate`), **token** (`use=password`, 64-hex, expires ≈ 7d), **mail**
  subject `Activate your OlliTeX Account` (CTA `/user/activate?token=…&user_id=…`).
- logged-in → 302 `/` (rate bucketed by **userId**, not IP — NOT 429).
- 6th consuming POST in 60s → 429 chunked `Rate limit reached, please try
  again later` (shared Redis `rate-limit:postRegister:*` — the gate resets
  it between legs).

**Live A/B (Node :4000 vs Go :4010) 10/10 byte-parity PASS** (status, headers
incl. CT/CSP/Location/Set-Cookie, bodies after nonce/csrf normalization,
equal mail deltas) + created-user/token sanity (bcrypt hash, `use=password`,
7-day expiry).

**P3.4 gate** (`server-ce/nginx/flips/web-p3d.conf` + `tests/e2e/specs/
parity/web-go-p3d-flip.test.e2e.ts`): leg1 Node baseline → leg2 FLIP ON
`web-p3d.conf` (Go) byte-for-byte match → leg3 FLIP OFF Node reversal; each
leg asserts the 10 HTTP cases + exactly-one activation mail (delta 1) +
user/token side effects.

**P3.4 gate result (2026-09-14): 3/3 GREEN.**

**Live bugs found + fixed in P3.4**:
1. **logged-in 429 vs 302** — Go consumed the rate limit under the IP bucket
   for a logged-in POST (→ 429); Node buckets by `getUserId(req)||req.ip`
   (→ 302). Fixed: `clientID = userId` when logged-in before `lim.Consume`.
2. **429 Content-Length leak** — Go auto-set `Content-Length`; Node's
   streamed 429 is chunked and has none. Fixed in shared `core.Send429`
   (see above); P2 unaffected (its diff ignores CL/TE).
3. **gate flip plumbing** — the CSP **header** nonce needed normalization in
   the diff (body nonce was already handled); the sed strip line needed a
   doubled backslash to survive JS→shell.

**Flake note**: running P3.3 immediately after P2 in one matrix pass can
fail a P3.3 leg — P2's deliberate 429-burst battery drains the shared login
rate-limit budget in Redis (login is `loginRateLimitEmail`-gated), so a P3.3
login misbehaves. With a recovered budget P3.3 is green; not a code defect.

#### P3.5 — user-activate (activation page + admin user creation) — **🚫 OUT OF SCOPE (SaaS, NOT ported) — 2026-09-14**
**Decision: deliberately excluded from the Go drop-in (owner instruction, 2026-09-14).**

**Why it's SaaS / not a CE leaf:** in a clean CE install `GET /user/activate` is simply
**not registered → 404** (owner-verified on the reference install:
`GET /user/activate → 404 (Not Found)`). In *this* build the route is claimed by the
`admin-tools` module (`AdminToolsRouter.mjs:18` → `UserListController.activate-
AccountPage`), which shadows the clean `modules/user-activate` handler and is part of
the SaaS / hub **admin user-management** surface. The clean `user-activate` module's
own routes are dead here: `GET /admin/register` → general-**404** page, `POST /admin/
register` → **"Cannot POST /admin/register"** (route unregistered), `GET /admin/user`
→ **301** `/hub#/site.general.users.all` (hub).

**Live Node contracts (pinned 2026-09-14, `/tmp/ua_pin_node.json`):**
- `GET /user/activate` (admin-tools handler):
  - missing `user_id` or `token` → **404** (general/404, 13932B)
  - nested `user_id` (`?user_id[x]=y`) → **403** (user/restricted, 14070B)
  - `user_id` a string, **no such user** → **404**
  - `user_id` a string, **valid user** (any loginCount) → **500** — **a real Node
    bug**: `admin-tools/…/UserListController.mjs:170` renders via
    `Path.resolve(__dirname, …)` but **`__dirname` is never defined** (ESM) →
    `ReferenceError: __dirname is not defined` → the generic 500 page (681B,
    `Something went wrong`, `placeholder@example.com`, weak ETag, no x-powered-by).
    **Consequence: a real user clicking their activation email gets a 500** — the
    register→activate flow is broken end-to-end in the reference for the happy path.
- `POST /admin/user/create`, `POST /admin/user/:id/send-activation`, `GET /admin/user/
  :id/info`, … (9 `/admin/user/*` CRUD routes) = the **hub admin user-management**
  backend — tracked as its own wave in `tests/e2e/hub-parity-plan.md`
  (`#/site.general.users.*`, `parity/hub-admin-users`), **not** part of this leaf.

**Action taken:**
- **No Go port** of `/user/activate` or the admin user-creation routes
  (P3.4 `features/registrationpage` — commit `5ae88d595f` — is the registration *page*,
  a separate, already-ported leaf). The Go shadow simply does not own these routes;
  the live Node (which keeps powering the hub admin backend) continues to serve them
  unchanged, including the 500.
- **Node/admin-tools left untouched** (it is the hub's live admin-users backend).
- **Flagged for the owner:** the P3.4 registration-activation mail CTA points at
  `/user/activate` — a SaaS/broken route. In a clean-CE target this activation CTA
  is out of scope / non-functional; decide whether the register→activate flow is
  wanted in-product (would be a **deliberate Node+Go feature change**, not a
  drop-in) or dropped with the SaaS surface.
- **Follow-up (owner, optional):** fix the Node `__dirname` bug in admin-tools
  (`const __dirname = Path.dirname(fileURLToPath(import.meta.url))`) so
  `/user/activate` renders — only if/when the SaaS admin surface is retained.

#### P3.6 — SiteSettings (Manage/Site backend) — **✅ GATE 3/3 GREEN (2026-09-14)**

**Scope (ported): the *backend* of the Manage/Site settings leaf** — the admin
settings read/validate/merge/persist + secret-cipher + e-mail-test surface of
`app/src/Features/SiteSettings/SiteSettingsManager.mjs` + `SecretCipher.mjs`
+ `EnvHydrator.mjs`: 864 lines + ~500 lines behind 27 settings keys.
**Not ported:** the React *frontend* form (the admin-tools/hub UI keeps calling
these endpoints). The Go shadow is a strict 1:1 of the Node HTTP contract.

**Go package: `go/services/web/features/sitesettings/`** (10 files, ~2.9k LoC):
`orderedjson.go` (ordered JSON codec), `ordered.go` (ordered map helpers),
`cipher.go` (HKDF-SHA512 + AES-256-CTR secret cipher + cross-runtime bridge
`Open`/`EncryptText`/`DecryptText`), `seeds.go` (default section values),
`seeds`/`section.go` (clean-input + merge + `*Set` masking), `storageenv.go`
(managed-env fragment render/parse), `validators.go`, `manager.go`, `store.go`
(mongo read/write), `routes.go` (5 handlers + rate limiter + template-admins).
Feature is wired in `cmd/web/main.go`; flip conf `server-ce/nginx/flips/web-p3e.conf`
proxies `/admin/site`, `/admin/site-settings{,/}`, `/admin/site/template-admins` to
the Go shadow; gate `tests/e2e/specs/parity/web-go-p3e-flip.test.e2e.ts`.

**Routes + pins (Node oracle, live 2026-09-14):**
- `GET /admin/site` → **302 `/hub#/site`**; `GET /admin/site-settings` → **200
  `application/json`** with the fixed 23-section order, secrets masked to `""` +
  per-field `<name>Set` booleans, and `templates.counts` appended.
- `PUT /admin/site-settings/:section` → **200** `{ok,upserted,modified}`; invalid
  field → **422** `{message}`; unknown section → **422** `{message:"Unknown
  section: …"}`. `PUT storage` → additionally `{appliesOn:"restart",envLines:[…]}`
  (a real JSON **array**) and writes the managed env fragment.
- `POST /admin/site-settings/email/test` → invalid `to` → **422**; valid → **200
  `{ok:true}`** + one mail, subject `"[Overleaf] E-mail configuration test"`.
- `GET /admin/site/template-admins` → **200** `{users:[…]}`.
- AuthZ: anon JSON GET → **401**; anon html GET → **302 `/login`**; non-admin →
  **302 `/restricted?from=…`**.
- `PUT` is **CSRF-enforced**; `requireGlobalLogin` runs before `requireAdmin`;
  `email/test` is rate-limited 5/min.

**Key contracts / decisions:**
- **Ordered JSON is mandatory.** Node `JSON.stringify` preserves insertion order;
  map-keyed Go `json` marshals would alphabetise and break byte parity → a custom
  ordered encoder (`orderedjson.go`) is used for all section bodies and the merged
  response (`MergeOrdered(seed, stored)` = seed order, stored values win).
- **Secret cipher is cross-runtime.** HKDF-SHA512 + AES-256-CTR, wire format
  `ss::label:hex(salt):base64(CT):hex(iv)`. **Verified BOTH directions**: the Go
  shadow decrypts Node-encrypted `sso-saml` `idpCert`/`privateKey`, and Node
  decrypts a Go-encrypted secret; `encryptText`/`decryptText` round-trip on both
  runtimes (`cmd/sscipher-probe` + `go test` golden vectors).
- **Gate normalisation (documented divergences, not Go bugs):**
  - `templates.counts` key order — Node builds it from `Promise.all` completions,
    so order is nondeterministic; compared as a **sorted** object only.
  - `storage.envManaged` / `storage.envPath` — Node's managed-env detection is
    known to diverge from repo source (instrumented this cycle); dropped from the
    equality, all other fields order-sensitive byte-parity.
  - Volatile (`session_created`/`lastActive`/`updated_at`-family, nonces, dates,
    ETag) handled by the standard gate normaliser.
- **`GET /admin/site`** is a thin `requireAdmin` + redirect (parity with Node's
  admin-tools router); **template-admins** reads the `isAdmin` + template flag and
  `zotero` is intentionally excluded from the SSO secret wipe loop (Node
  `SecretCipher` behaviour) while `mendeley` is in it.

**Evidence (2026-09-14):** 13/13 A/B battery PASS (GET full-body, PUT
  toggle+restore, PUT invalid-422, PUT unknown-422, PUT storage fs, email/test
  422 + 200/mail, template-admins, anon 401/302, non-admin 302); cipher round-trip
  ALL PASS; flip gate **leg1 Node / leg2 Go(flip) / leg3 Node** all byte-identical
  (**3 passed, 32.9s**) including the mail side-effect. **P3 is now complete.**

**File-split refactor (owner instruction, same cycle):** every oversized web-feature
monolith was split into focused same-package files to keep Go files small for LLM
context + maintainability (pure moves, zero behaviour change; `goimports` added,
`go build` + `go vet` + `go test ./go/...` green, all of P3 re-verified green):
`userpages.go 1325→{userpages.go 93, _settings.go 832, _view.go 192,
_sessions.go 324}`, `instancestats.go 592→{instancestats.go 231, _series.go 198,
_alerts.go 224}`, `serveradmin.go 588→{serveradmin.go 135, _editor.go 74,
_messages.go 445}`, `authpages.go 546→{authpages.go 70, _login.go 469, _logout.go 54}`,
`passwordreset.go 514→{passwordreset.go 295, _token.go 245}`, `tokenaccess.go
471→{tokenaccess.go 148, _project.go 200, _grant.go 157}`, `registrationpage.go
469→{registrationpage.go 170, _register.go 216, _signup.go 110}`, plus the
service monoliths `docstore/routes.go 1094→{routes.go 105, routes_docs.go 597,
routes_comments.go 178, routes_archive.go 295}` and `chat/handlers.go 951→
{handlers.go 331, chat_sendedit.go 213, chat_threads.go 365, chat_clone.go 188}`.
Largest web Go file after all splits: **userpages_settings.go 832 LoC** (no web
file exceeds ~900).

**Latent bug found + fixed this cycle:** the e2e smtp sink exposes only
`DELETE /api/messages` (wipe) + `GET /api/messages` — but `web-go-p3c/p3d` (and
initially p3e) called a nonexistent `POST /api/flush` (silent no-op) relying on
before/after deltas to mask it; all three now use the real endpoint.

### P4 — project core (the heavy centre; flip in listed sub-order)

#### P4.1 — project list (`GET /user/projects`) — **✅ GATE 3/3 GREEN (2026-09-14)**

**Scope:** the project-list endpoint the hub/`projects` surface calls.
**Go package: `go/services/web/features/projectlist/`** (3 files):
`projectlist.go` (Feature + `buildList` + wire types), `format.go`
(`viewModel` = per-user archived/trashed), `queries.go` (the six `projects`
reads). Wired in `cmd/web/main.go`; flip `server-ce/nginx/flips/web-p4a.conf`;
gate `tests/e2e/specs/parity/web-go-p4a-flip.test.e2e.ts`.

**Route + pins (Node oracle, live 2026-09-14; A/B byte-identical):**
- `GET /user/projects` (logged-in) → **200 `application/json`**
  `{ "projects": [ {"_id", "name", "accessLevel"}, … ] }` — the ONLY three
  fields per entry, in EXACT bucket order (owned → invite-readWrite/review/
  readOnly → token-readAndWrite/readOnly, token buckets de-duplicated against
  any id already listed), **no sorting**, and with projects the user is a
  member of `archived[]` or `trashed[]` **omitted** (`.filter(!(archived||trashed))`).
- **accessLevel strings (exact):** `owner` / `readWrite` (invite) / `review`
  (invite) / `readOnly` (invite) / `readAndWrite` (token) / `readOnly` (token).
- anon GET accept json → **401**; anon GET accept html → **302** `/login`
  (the global `requireLogin` gate, shared with Node via the same redis session).

**Pitfall caught by the live oracle:** the route wires
`ProjectController.userProjectsJson` — the *simple* handler above. There is a
separate, richer `ProjectListController.getProjectsJson`
(`{totalSize, projects:[{id,…,lastUpdated,owner}]}`) that is **NOT** wired to
this route in this build. Pinning from the higher-level controller would have
produced the wrong shape; the **running Node response is authoritative** (as
with P3.6).

**Evidence:** A/B (Node:4000 vs Go:4010) byte-identical for both fixture users
(admin 13853B / user 12965B, matching etag + content-length) and the anon
401/302 contract; flip gate **leg1 Node / leg2 Go(flip) / leg3 Node** all
byte-identical (**3 passed, 31.1s**). `go build` + `go vet` + `go test ./go/…`
green (19 packages). Stack left node-active.

---

#### P4.2 — project entities (`GET /project/:Project_id/entities`) — **✅ GATE 3/3 GREEN (2026-09-14)**

**Scope:** the entity listing the file-tree/word-count surfaces call.
**Go package: `go/services/web/features/projectlist/`** (added 2 files to the
P4.1 package): `access.go` (read authorization — the Node
`ensureUserCanReadProject` mirror) + `entities.go` (the route handler + the
`rootFolder` recursive walk). Wired in `projectlist.go` (`pattern`,
`pattern`, `handler`).

**Route + pins (Node oracle, live 2026-09-14; A/B byte-identical 10/10):**
- `GET /project/:Project_id/entities` (logged-in, owned, valid id) →
  **200 `application/json`** `{ "project_id", "entities":[{ "path","type" }, …] }`
  where `type` ∈ `doc` | `file` and `path` is the slash-joined tree path
  (root = `""`, so `/name` for root children, `/sub/name` for a subfolder),
  **sorted ascending by path**. The walk reads `rootFolder[0]` and recurses
  `folder.folders`, emitting `folder.docs` → `doc` and `folder.fileRefs` →
  `file` (via `IterablePath` = `folder[field] || []`, so absent folders are
  empty, not an error).
- **read authorization** (`canRead`, mirrors Node `ensureUserCanReadProject`): the
  caller is the owner, or is named in `collaberator_refs` / `reviewer_refs` /
  `readOnly_refs`, or the project is `publicAccesLevel ∈ {readOnly, full}` (or
  `tokenBased` + named in `tokenAccess*Refs`), or the caller is an admin.
- **accept-sensitive 403 (exists but no read access):** `accept json` → **403
  `{ "message": "restricted" }`** (`application/json`); `accept html` → **403
  text/html** Restricted view. (Pinned: NOT a 404 — the *project* exists, the
  user just can't read it.)
- **valid id, project absent** → **404 text/html general/404** (NOT
  accept-dependent).
- **INVALID (non-hex) id** → **404 `application/json`** the exact Node
  validation body `{"error":"Validation error: Invalid Mongo ObjectId at
  \"params.Project_id\"","statusCode":404}` (NOT accept-dependent, NOT the
  general/404 view).
- **anon** accept json → **401**; accept html → **302 /login** (the
  `requireLogin` gate runs before the entities handler).

**Contract detail caught while mirroring Node:** the `general/404` page uses
the **request-relative path** in its `<link rel=alternate>`
(`subdomainDetails.url + currentUrl`, `currentUrl` = the route path *without*
a leading slash). The Go shared views kept `/restricted` hardcoded, so
`PageData.Path` (the request relative path) is now fed into BOTH the `404`
and `restricted` skeletons (`views/pages_data.go` — a single `PATH` slot each),
and the three existing callers are corrected: `authpages` (`/restricted` →
`Path: "restricted"`), the `tokenaccess` consent-403 (→ `Path`
`"project/<id>/sharing-updates"`), and this route (→ `Path`
`"project/<id>/entities"`). The `403`/`404` **bodies** are the only
`accept`-sensitive outputs; the status codes are not.

**Normalization in the gate:** the 403/404 **HTML** views embed a random
per-render CSP nonce and a *salted random* csrf token
(`8 base62 + "-" + base64(SHA1(salt+csrfSecret))` — the salts differ even
Node→Node, so byte-parity is impossible); the gate normalizes nonce +
`ol-csrfToken` + the `_csrf` value + the site origin before the body compare.
JSON bodies (200 / 403-json / malformed-404-json) and their ETags are compared
byte-for-byte. The `200` body is byte-identical (no random slots).

**Files:** `features/projectlist/access.go`, `features/projectlist/entities.go`;
`features/projectlist/projectlist.go` (route); `views/pages.go` (
`PageData.Path` doc + `Restricted403` note), `views/pages_data.go` (PATH slot in
both skeletons), `features/authpages/authpages.go` + `features/tokenaccess/
tokenaccess_grant.go` (Path callers); flip `server-ce/nginx/flips/web-p4b.conf`
(REGEX location `^/project/[^/]+/entities\z` — the id is a dynamic single
segment, mirroring Node's `:Project_id`); gate `tests/e2e/specs/parity/
web-go-p4b-flip.test.e2e.ts`.

**Evidence:** A/B (Node:4000 vs Go:4010, shared session) **10/10 byte-identical**
after nonce/csrf/sid normalization (owned-200, multi-entity, noaccess-403 json+html,
unknown-404 html, malformed-404 json, anon-401/302); the shared `GET /restricted`
route (authpages) re-verified **byte-identical** (no regression) after the
skeleton `Path` change. Flip gate **leg1 Node / leg2 Go(flip) / leg3 Node** all
match (**3 passed, 31.6s**). `go build` + `go vet` + `go test ./go/…` green
(19 packages). Stack left node-active.

**Known pre-existing edge (NOT introduced here, NOT part of P4.2 gate):** a
*logged-in* user who is a *token-read-only* member (in
`tokenAccessReadOnly_refs`) hitting `GET /project/<id>/sharing-updates` gets
**Node 302 → /project/<id>** vs **Go 403** in this build — the consent branch on
two sub-route (`tokenaccess`) differs; recorded for a future P2-tokenaccess
follow-up, left untouched here.

---

#### P4.3 — project members (`GET /project/:Project_id/members`) — **✅ GATE 3/3 GREEN (2026-09-14)**

The share-modal / "active members" list — the first, bounded read slice of the
Collaborators sub-unit. Wired exactly as Node (router.mjs:545 +
CollaboratorsController.getAllMembers +
CollaboratorsGetter.getAllInvitedMembers + ProjectAccess.loadInvitedMembers):
`requireLogin → (blockRestrictedUserFromProject) → ensureUserCanReadProject →
{ members: [ … ] }` — invited members only (the OWNER and TOKEN-sourced members
are **omitted**), order `collaborators → reviewers → readOnly` in array order
(no dedup, no sort), `privileges` ∈ `readAndWrite|review|readOnly`, per-member
key order `_id, first_name, last_name, email, privileges, signUpDate[, pendingEditor[, pendingReviewer]]`,
missing-user members dropped, `signUpDate` an ISO string with ms (Node
`JSON.stringify(Date)`).

- **Contract (live-oracle, A/B 8/8 byte-identical):** owned/collab 200; no-access
  403 `{"message":"restricted"}` (json) / Restricted (html); absent project 404
  general/404 (html, not accept-dep); malformed id 404
  `{"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}`
  (not accept-dep); anon 401 (json) / 302 `/login` (html). Member rows batch-loaded
  from `users` (projection `_id,email,first_name,last_name,signUpDate`).
- **Pitfall:** when decoding member user docs into `primitive.D`, the driver
  yielded the BSON `signUpDate` as a non-`time.Time` type — `signUpDateISO` must
  accept `time.Time` *or* `primitive.DateTime` *or* an int64 ms epoch, then format
  `2006-01-02T15:04:05.000Z`; handling only `time.Time` silently emitted `null`.
- **Files:** new `go/services/web/features/projectlist/members.go` (membersHandler +
  invitedMemberRows + loadUsers + signUpDateISO); `projectlist.go` registers
  `GET` `^/project/([^/]+)/members$` (reuses P4.2 access/loadProject/validOID/malformed404
  + views.NotFoundPage/Restricted403). Reuses the shared views restricted/404 pages
  (PATH slot = `project/<id>/members`).
- **Flip:** `server-ce/nginx/flips/web-p4c.conf` — regex location
  `~ ^/project/[^/]+/members\z` → `127.0.0.1:4010` (one dynamic segment, empty id
  falls through to Node 404).
- **Gate:** `tests/e2e/specs/parity/web-go-p4c-flip.test.e2e.ts` (3-leg, Node oracle →
  flip → Node reversal; 8-case battery; idempotent `webgo-p4c-{own,col,na}` fixtures;
  `signUpDate` ISO asserted; 403 html normalized on nonce + csrf + site origin) — **3 passed**;
  P4.1 + P4.2 gates re-run **3/3 + 3/3** (shared projectlist/views no regression);
  stack left node-active.

---

#### P4.4 — access-requests (`GET /project/:Project_id/access-requests`) — **✅ GATE 3/3 GREEN (2026-09-14)**

The owner/admin-facing list of pending access requests (the second bounded read
slice of the Collaborators sub-unit, same package + shared access infra as P4.3).
Wired exactly as Node (CollaboratorsRouter + CollaboratorsController.
getAccessRequests + CollaboratorsGetter.ProjectAccess.loadAccessRequestsView):
`requireLogin → ensureUserCanAdminProject → { editAccessRequests: [ … ] }`.

- **Contract (live-oracle, A/B 7/7 byte-identical):**
  `200 {editAccessRequests:[{_id, email, first_name, last_name, privilegeLevel,
  currentPrivilegeLevel, requestedAt}]}` — `privilegeLevel` = the level REQUESTED,
  `currentPrivilegeLevel` = their CURRENT level (owner|readAndWrite|review|readOnly)
  **or `false`** (`PrivilegeLevels.NONE`, a boolean) when not a member;
  `requestedAt` = ISO-ms string; order = `project.editAccessRequests` array order
  (no sort/dedup); rows whose user doc is gone are dropped; empty list → `[]`.
- **Guard (`canUserAdminProject`):** `OWNER` **or** site-admin-with-`modify-project-setting`
  (here `ADMIN_PRIVILEGE_AVAILABLE=true` → `owner || user.isAdmin`). Not admin →
  `403 {"message":"restricted"}` (json) / Restricted (html). (Not A/B-able in this e2e:
  the two fixture users are the owner and a site-admin, so both hit the 200/404 paths.)
- **Error contract** (same family as P4.2/P4.3): absent project 404 general/404
  (html, not accept-dep); malformed id 404 malformed JSON (not accept-dep); anon
  401 (json) / 302 `/login` (html).
- **Pitfall:** the shared `accessProj` (access.go) had to add `editAccessRequests: 1`
  — without it `loadProject` returned a doc with no request rows and the 200 body
  silently collapsed to `{"editAccessRequests":[]}` (members/entities still pass;
  they ignore the extra projected field). `currentPrivilegeLevel: false` is a bare
  JSON boolean (NOT the string "false") — `writeJSONVal` handles that.
- **Files:** new `go/services/web/features/projectlist/accessrequests.go` (handler +
  currentPrivLevel + loadUsersHex + writeJSONVal); `projectlist.go` registers
  `GET` `^/project/([^/]+)/access-requests$`; `members.go` `loadUsers` now delegates to
  the shared `loadUsersHex`; `access.go` `accessProj` gains `editAccessRequests`.
- **Flip:** `server-ce/nginx/flips/web-p4d.conf` — regex location
  `~ ^/project/[^/]+/access-requests\z` → `127.0.0.1:4010` (one dynamic segment +
  literal suffix; empty id falls through to Node 404).
- **Gate:** `tests/e2e/specs/parity/web-go-p4d-flip.test.e2e.ts` (3-leg Node → Go →
  Node; 7-case battery; idempotent `webgo-p4d-{req,empty}` fixtures; pins
  `currentPrivilegeLevel:false`, `requestedAt` ISO, row key order; 404 html normalized
  on nonce+csrf+origin) — **3 passed**; P4.1/P4.2/P4.3 re-run **3/3 + 3/3 + 3/3**;
  stack left node-active.

---

#### P4.5 — project rename (`POST /project/:Project_id/rename`) — **✅ GATE 3/3 GREEN (2026-09-14)**

First write (mutation) unit — opens the P4 write family. Wired exactly as Node
(router.mjs:798 + ProjectController.renameProject + EditorController.renameProject
+ ProjectDetailsHandler.renameProject): `requireLogin → ensureUserCanAdminProject →
renameProject`; `newName = newName.trim()`; `validateProjectName` (blank → 400);
`Project.updateOne({_id},{name:newName})`; **no audit entry**; `res.sendStatus(200)`.

- **Contract (live-oracle, A/B 6/6 byte-identical):**
  `200 text/plain body "OK"` (+ project `name` set to the trimmed value, verified in Mongo);
  blank name (trim→"") → `400 text/plain "Project name cannot be blank"`;
  missing `newProjectName` → `400 application/json` zod `received undefined`;
  non-string `newProjectName` (number) → `400 application/json` zod `received number`;
  invalid (non-hex) id → `404 application/json` malformed (not accept-dep);
  valid id, project absent → `404 HTML` general/404 (not accept-dep);
  **anonymous → `403 text/plain "Forbidden"`** (CSRF fires before requireLogin — the core
  enforces csrf globally for POST, so the handler's 401 branch is unreachable);
  not admin → `403 restricted` (json/html; not reachable w/ the two e2e users).
- **Validation order (faithful to Node):** 403-anon(CSRF) > 404 (absent/malformed) >
  403 (not admin) > 400 (body) > 200 (OK). rename emits **no** audit-log entry.
- **Files:** new `go/services/web/features/projectlist/rename.go` (renameHandler +
  zodReceived + writeRename); `projectlist.go` registers `POST` `^/project/([^/]+)/rename$`;
  `access.go` shares the new `canAdmin(uid, isAdmin, doc)` helper (also used by P4.4);
  `accessrequests.go` refactored onto `canAdmin`. Reuses loadProject/validOID/malformed404/
  views.NotFoundPage/Restricted403 + core.Res.SendStatus/PlainText/JSON/Redirect.
- **Flip:** `server-ce/nginx/flips/web-p4e.conf` — regex location
  `~ ^/project/[^/]+/rename\z` → `127.0.0.1:4010` (nginx location is method-agnostic;
  one dynamic segment + literal suffix).
- **Gate:** `tests/e2e/specs/parity/web-go-p4e-flip.test.e2e.ts` (3-leg Node → Go → Node;
  each leg renames the SAME fixture to a DISTINCT name and asserts (a) responses
  status/ct/body byte-match the baseline and (b) Mongo name == that leg's name;
  error cases assert the name is unchanged; idempotent `webgo-p4e-ren` fixture) — **3 passed**;
  P4.1/4.2/4.3/4.4 re-run **3/3 each**; full `go test ./go/...` green; stack left node-active.
- **Pitfall:** `res.sendStatus(200)` body is the 2-byte literal `OK` (text/plain), NOT a
  JSON `{}` — core.Res.SendStatus mirrors this. `UpdateOne` returns `(result, error)`
  (two values); `nameVal` must be `any` (type-asserted), not a pre-typed string.

---

#### P4.6 — project flag-writes (archive/unarchive/trash/untrash) — **✅ GATE 3/3 GREEN (2026-09-14)**

Second write unit — the per-user archived/trashed set mutations. Wired exactly as
Node (router.mjs + ProjectController.archive/unarchive/trash/untrash +
ProjectDeleter.mjs:156-188). **Key finding:** `archived` / `trashed` are **arrays of
user ObjectIDs** (per-user sets), NOT booleans — each user's view is archived/trashed
independently. All four ops are **no-audit** in this free build (`addEntryIfManaged`
→ `findManagedSubscriptions` returns `[]` → no `projectAuditLogEntries` write). Guard =
`requireLogin` + `ensureUserCanReadProject` (READ, like P4.2 `canRead`, not admin).

- **Contract (live oracle, A/B + gate 3/3; state-aware):**
  Each valid op → `200 text/plain` body `OK`, with the exact Mongo `$addToSet`/`$pull`:
  `archive` = `$addToSet{archived:uid}` + `$pull{trashed:uid}`; `unarchive` = `$pull{archived:uid}`;
  `trash` = `$addToSet{trashed:uid}` + `$pull{archived:uid}`; `untrash` = `$pull{trashed:uid}`.
  invalid (non-hex) id → `404 application/json` malformed — **param name is route-specific
  (`params.Project_id` for `/Project/…/archive`, `params.project_id` for `/project/…/trash`)**;
  valid id, project absent → `404 HTML` general/404 (byte-equal after nonce-norm);
  anonymous → `403 text/plain` `Forbidden` (CSRF before requireLogin; both stacks agree).
- **Files:** new `go/services/web/features/projectlist/projectflags.go` (flagHandler +
  applyFlagOp + flagOp/paramName + malformedMsg); `projectlist.go` registers POST/DELETE
  `^/Project/([^/]+)/archive$` and POST/DELETE `^/project/([^/]+)/trash$`; reuses
  loadProject/canRead/loadUserAdmin/validOID/views.NotFoundPage/Restricted403 +
  core.Res.SendStatus/JSON/Redirect.
- **Flip:** `server-ce/nginx/flips/web-p4f.conf` — two regex locations
  (`~ ^/Project/[^/]+/archive\z` and `~ ^/project/[^/]+/trash\z`, each covering its
  POST+DELETE pair; nginx location is method-agnostic) → `127.0.0.1:4010`.
- **Gate:** `tests/e2e/specs/parity/web-go-p4f-flip.test.e2e.ts` (3-leg Node → Go → Node;
  each leg: reset archived/trashed → run the 4 valid ops asserting 200 + per-op Mongo
  state → error battery; cross-leg diffs compare **responses AND state**; idempotent
  `webgo-p4f-flags` fixture) — **3 passed**; P4.1–P4.5 re-run **3/3 each**; full
  `go test ./go/...` green; stack left node-active.
- **Pitfalls:** (a) the malformed-404 message embeds the route param name, so trash must
  emit `params.project_id` (lowercase) not `params.Project_id`; (b) raw HTML 404 bodies
  differ only by the random CSP nonce — compare after normalization; (c) an in-container
  Node probe has no `docker` CLI, so Mongo state reads belong in the host-side gate
  (Playwright `execFileSync('docker','exec',mongoC,'mongosh…')`).

#### P4.7 — basic project creation (`POST /project/new`) — **✅ GATE 3/3 GREEN (2026-09-14)**

Third write unit — the primary "New Project" flow. Node sources
(`router.mjs` `webRouter.post('/project/new',…newProject)` →
`ProjectController.newProject` → `createBasicProject`/`createExampleProject`
→ `ProjectCreationHandler.createBasicProject` = `_createBlankProject` + `_createRootDoc`
→ `validateProjectName` + `_buildTemplate('mainbasic.tex')`
→ `DocstoreManager.updateDoc` (basic) + `HistoryManager.initializeProject` (basic)).

**Scope:** `template: "basic"` / absent (the common case) is ported here. The
`template: "example"` variant (main.tex + sample.bib + frog.jpg blob) is P4.7b below —
both templates are now served by Go.

#### P4.7b — example project creation (`POST /project/new` `{template:"example"}`) — **✅ GATE 3/3 GREEN (2026-09-14)**

The "Example project" option in New Project. Node source
(`ProjectCreationHandler.createExampleProject` = `_createBlankProject` (same project doc +
project_history init as basic) **+** `_addExampleProjectFiles`:
`_buildTemplate('main.tex')`→`_createRootDoc` (docstore rev0 + setRootDoc) +
`_buildTemplate('sample.bib')`→`addDoc` (docstore rev0) + `addFile` of `frog.jpg`
(`uploadFileFromDisk` → git-blob-SHA1 hash → `PUT {v1_history}/projects/{id}/blobs/{hash}`
basicAuth `staging:$V1_HISTORY_PASSWORD` → fileRef pushed to `rootFolder.fileRefs`)).
`populateClsiCacheForExampleProject` is a **fire-and-forget clsi cache warm** (a perf
optimisation, not a creation contract) — **deferred**.

**Contract (live oracle, A/B; id-free state capture):**
- `200 application/json` `{ project_id, owner_ref, owner:{first_name,last_name,email,_id} }`
- project doc: `rootFolder.docs=[main.tex, sample.bib]`;
  `rootFolder.fileRefs=[{name:'frog.jpg', rev:0, linkedFileData:null,
  hash:'5b889ef3cf71c83a4c027c4e4dc3d1a106b27809'}]` (git-blob-SHA1 of the frog);
  `folders=[]`; `rootDoc_id`=main.tex doc id.
- docstore: `main.tex` **118 lines**, `sample.bib` **10 lines** (rev 0).
- `frog.jpg` blob: retrievable from history-v1, **byte-identical** to the template
  (md5 `665777aa6c7c48794db02f5d69ccc24a`, 97080 bytes).
- name/template validation + anonymous 403 (shared parse path) still pin-exact.

- **Files:** new `go/services/web/features/projectlist/create_example.go`
  (`crCreateExampleProject` + `crTemplateLines` + `crGitBlobHash` =
  `sha1("blob <n>\x00"+bytes)` + `crUploadBlob` history-v1 PUT + env
  `WEB_V1_HISTORY_URL`/`V1_HISTORY_USER`/`V1_HISTORY_PASSWORD`/`WEB_EXAMPLE_PROJECT_DIR`);
  `create.go` refactored — `crInsertProject`/`crCreateDocRevision` parameterised so
  basic + example share one doc shape, handler branches on `template`.
- **Gate:** `tests/e2e/specs/parity/web-go-p4hb-flip.test.e2e.ts` (3-leg Node → Go → Node;
  each leg: delete prior `webgo-p4hb-*` → example create + id-free state capture
  (doc names, fileRef {name,rev,linkedFileData,hash}, docstore line-counts, frog-blob
  md5+size) → error battery + anon → cleanup; cross-leg diffs compare the 200 body +
  state + every error case) — **3 passed**; full P4.1–P4.7 re-run **3/3 each**;
  `go test ./go/...` green; stack left node-active.
- **Pitfalls:** (a) the fileRef `hash` is a **git blob SHA1**
  (`sha1("blob <len>\x00"+bytes)`), *not* the raw md5 (md5 `665777…` ≠ hash `5b889ef3…`);
  (b) the blob must be PUT to **history-v1** (`:3100/api`, basicAuth `staging`), and the
  project must be registered first via project_history `:3054` (the same
  `initializeProject` basic already calls);
  (c) the `mongodb` driver only resolves from inside the pnpm workspace, so the gate's
  state-capture script runs from `/overleaf/services/web`; (d) clsi cache warm is
  intentionally **not** replicated (perf-only, fire-and-forget in Node).

- **Contract (live oracle, A/B 13/13 + gate 3/3; id-normalized state):**
  `200 application/json` `{ project_id, owner_ref, owner:{first_name,last_name,email,_id} }`
  **+ a full Mongo project doc (Node's exact default field set) + docstore revision 0
  (`mainbasic.tex`, 15 lines) + project_history `initializeProject`.**
  Error battery (byte-pinned):
  * `projectName` absent OR whitespace → `400 text/plain` `Project name cannot be blank`
  * `projectName` has `/` → `400 text/plain` `Project name cannot contain / characters`
  * `projectName` > 150 UTF-16 units → `400 text/plain` `Project name is too long`
  * `projectName`/`template` non-string → `400 application/json`
    `{error:"Validation error: Invalid input: expected string, received <T> at body.<f>",statusCode:400}`
  * unrecognized body key → `400 application/json` `{error:"Validation error: Unrecognized key(s): "k" at "body"",statusCode:400}`
  * body not a JSON object (number/string/null/boolean/array-root) OR invalid JSON → `400 application/json` `{}`
  * anonymous → `403 text/plain` `Forbidden` (CSRF before requireLogin)
  zod accumulates **all** errors joined by `"; "` (value-errors projectName→template,
  then the unrecognized-key error; 13 cases total, Node-vs-Go A/B identical).
- **Files:** new `go/services/web/features/projectlist/create.go` (~640 LoC:
  `newProjectHandler` + `crParseCreateBody`/zod + `crNameError`/`crSanitizeControl`
  /`crUTF16Len` + `crInsertProject` (Node's exact default doc) + `crBasicDocLines`
  (`mainbasic.tex`) + docstore/project_history HTTP clients, env-overridable
  `WEB_DOCSTORE_URL` / `WEB_PROJECT_HISTORY_URL` / `WEB_BASIC_PROJECT_TEMPLATE`);
  `projectlist.go` registers `POST /project/new` (createNewPat `^/project/new$`).
- **Flip:** `server-ce/nginx/flips/web-p4g.conf` — exact `location = /project/new` →
  `127.0.0.1:4010`.
- **Gate:** `tests/e2e/specs/parity/web-go-p4g-flip.test.e2e.ts` (3-leg Node → Go → Node;
  each leg: delete prior `webgo-p4g-*` → success create asserting 200 + (id-normalized)
  Mongo doc + docstore line-count → 13-case error battery + anon → cleanup; cross-leg
  diffs compare the 200 body (ids normalized) + project doc + docstore + every error case)
  — **3 passed**; P4.1–P4.6 re-run **3/3 each**; full `go test ./go/...` green; stack left node-active.
- **Pitfalls:** (a) Node's project schema has **two differently-spelled** "collaborator"
  keys — `collaborator`+`_refs` (the refs array) and `collaboratec`+`Users` (a different
  spelling) — Go must emit each **exact byte sequence** (char-for-char), not the
  "correct English" spelling (a one-char `o`/`l` miss is a real schema divergence); (b)
  `spellCheckLanguage` inherits the **User model default `en`** (not the raw user-doc
  value, which may be absent) — Go defaults empty → `"en"`; (c) mainbasic.tex is **15
  lines after `split('\n')`** (trailing newline); the empty-body case is treated as `{}`
  (projectName undefined → blank error);
  (d) `overleaf.history.id` is a **hex string equal to the project `_id`** (not a new ObjectId).

#### P4.8 — project delete/restore — **✅ GATE 3/3 GREEN (2026-09-14)**

Node source oracle (all pinned live before implementation):

- `DELETE /Project/:Project_id` (router.mjs:778 → `ProjectController.deleteProject`
  → `ProjectDeleter.deleteProject`):
  1. `flushProjectToMongoAndDelete` = `DELETE {document-updater:3003}/project/{id}`
     (204); **fallback chain on failure**: POST document-updater `/flush` →
     POST project-history `/flush` → (on failure) POST project-history `/resync`
     `{force:true}` → retry DELETE. (Replicated verbatim; services are up in e2e,
     so the happy path is contract-visible.)
  2. `POST {docstore:3016}/project/{id}/archive` — best-effort; **no-op in this
     build** (mongo-based persistor: doc remains readable — pinned `200` from both).
  3. per-member tag pulls — no `tags` collection in this build (no-op).
  4. `deletedProjects.updateOne({deleterData.deletedProjectId}, {project: <FULL doc>,
     deleterData: {...}}, {upsert:true})` (+ Mongoose `__v:0`).
  5. `projects.deleteOne({_id})`; `hooks projectDeleted` — no listeners (no-op); audit
     writer — no-op (free build).
  6. → `200 text/plain "OK"`.
- `POST /Project/:Project_id/restore` → `Project.updateOne({_id}, {$unset:{archived:true}})`
  → `200 "OK"` (removes the `archived` field entirely).
- Auth contract (live A/B): missing project → `404 text/html` NotFound page (even with
  `accept: application/json`); malformed id → `404 application/json` validation error
  embedding `params.Project_id`; non-member → `403 {"message":"restricted"}`;
  anonymous → `403 text/plain Forbidden` (CSRF-first).

Go: `go/services/web/features/projectlist/delete.go` (+2 routes in `projectlist.go`),
flip conf `server-ce/nginx/flips/web-p4del.conf` (delete/restore/archive+new),
gate `tests/e2e/specs/parity/web-go-p4del-flip.test.e2e.ts` — 3-leg (Node → Go → Node),
per leg: 3 fixture creates + archive(B) + restore(B) + delete(C) + 8-case battery
(owner/other/anon) + pre/post state pins; cross-leg diffs compare every response
(nonce/csrf/`_csrf`-hidden/ids normalized) + normalized state — **3 passed**.
Full P4 regression (P4.1–P4.8, 27 legs) re-run **all green**; `go build/vet/test` clean;
stack left node-active.

- **Pitfalls:** (a) mongo-driver v1.17 decodes BSON dates to **`primitive.DateTime`**
  (not `time.Time`) when decoding into `primitive.D` — a `.(time.Time)` assertion on
  `lastUpdated` silently dropped `deletedProjectLastUpdatedAt` (caught by the
  deletedProjects state pin); (b) Node/Mongoose stores `deleterData` **`_id` first, then
  the remaining keys ASCII-sorted** (pinned: not source order, not schema order) — Go
  copies that order; (c) `deleterData` subdoc has its OWN fresh `_id` (≠ the
  `deletedProjects` `_id`); (d) Node **drops** the `deletedProjectOverleafId` /
  `*Token` keys when undefined (Go omits them too); (e) the 404 HTML page carries a
  per-request `_csrf` hidden input (Node rotates it per request) — gate-normalized;
  (f) login-heavy consecutive gate runs trip the per-IP login limiter → `clearRateLimits()`
  before each login now in the p4del/p4g/p4hb gates.

#### P4.9 — project clone/duplicate (`POST /Project/:Project_id/clone`) — **✅ GATE 3/3 GREEN (2026-09-15)**

`cloneProject` (ProjectController.mjs:398-461) + `ProjectDuplicator.mjs` — **read**
access suffices (ensureUserCanReadProject); non-admins get the plain copy
(isDebugCopy/cloneHistory/cloneRanges forced false).

- oracle (example-src → `webgo-p4cl-copy`):
  - response 200 application/json `{name,lastUpdated,project_id,owner_ref,owner}`;
    cloned project = the same 30-key shape as basic creation, **no `segmentation`
    persisted** (analytics-only), version **1** (single createNewFolderStructure $inc),
    rootDoc = copied main.tex, docs [main.tex(118), sample.bib(10)], fileRef
    frog.jpg same git-blob hash, fileRef shape `{name,created,rev,hash,_id}`
    **WITHOUT linkedFileData** (Node's clone `File` shape — unlike the
    example-create `linkedFileData:null`).
  - frog blob present on the NEW project (md5 `665777aa6c7c…`, 97080 B) via v1
    `copyBlob` (`?copyFrom=src`); source project/docstore/blob UNCHANGED.
  - **quirk pinned**: `{}` (no projectName) → Node's `newProjectName.trim()`
    TypeError → **500 generic HTML error page** — replicated; `"   "` → 400
    text/plain blank; `"a/b"` → 400 text/plain slash; unknown key / wrong types /
    array root → 400 JSON zod; missing → 404 HTML; malformed → 404 JSON validation;
    non-member → 403 `restricted`; anon → 403 `Forbidden` (CSRF).
- gate `web-go-p4clone-flip` (**3 passed 43s**): leg Node→Go→Node byte equality
  (create, clone, 11-case battery, anon, in-container state capture incl.
  linkedFileData-absent fileRef pin, docstore lines, frog md5/size, source pins).
- **side finding pinned by the state diff**: Node's `version` is `$inc`ed per
  structural edit (addDoc/addFile) — example project = **3** (2 docs + 1 file),
  not 1; `crInsertProject` now takes the version per call site (basic 1, example
  len(docs)+len(files), clone 1). p4hb gate now pins `version:3`.
- files: `features/projectlist/clone.go` (new; cloneHandler/clParseCloneBody/
  clZodReply/clDocLines/clCopyBlobs), `projectlist.go` (route), `create.go` /
  `create_example.go` (version param), flip `web-p4clone.conf`, gate
  `web-go-p4clone-flip.test.e2e.ts`, p4hb version pin.
- full P4.1–P4.9 suite re-run green after the version fix (30 passed 2.5m).

#### P4.10a — collaborator mutations (set-level / leave / remove / access-requests / transfer) — **✅ GATE 3/3 GREEN (2026-09-15)**
- Seven routes byte-pinned against Node: `PUT users/:uid` (3-way level +
  track_changes $set shape), `POST leave` (login-only, no project check —
  204 on ghost), `DELETE users/:uid` (admin; 10-key $pull, no match check),
  `DELETE access-requests/:uid` (decline; optional notify mail),
  `POST access-requests/:uid/grant` (admin; setLevel+hadRequest mail),
  `POST request-access` (requestable-map 403, rebuild editAccessRequests,
  owner mail), `POST transfer-ownership` (collab check, prev-owner→editor,
  contacts both ways, TPDS flush, 2 mails).
- Error contract: VA JSONs with QUOTED path (`at "body.privilegeLevel"`),
  `{"message":"restricted"}` 403, `not found` 404 JSON (set unmatched) vs
  HTML 404 page (grant unmatched / ghost grants), 204 = no body/CT (res.NoContent).
- Mail = 8 per battery (3 request, 1 declined, 1 granted, 2 ownership);
  Go sends synchronously (Node fires async — order unobservable through sink).
- Debug lessons (cost hours):
  1. **$addToSet field names**: readAndWrite→`collaberator_refs`,
     review→`reviewer_refs`, readOnly→`readOnly_refs` — NOT the raw level
     value (literal `review`/`readAndWrite`/`readOnly` fields wrote fine,
     invisible in HTTP diffs; only mongo profile + typeof dumps exposed it).
  2. **Mongo rejects $pull+$addToSet on the SAME field** ("would create a
     conflict") but mongoose rewrites Node's update — Go must omit the add
     target from the $pull list (RW branch: do not pull collab).
  3. **Collab gates must load the FULL doc** (loadProjectFull): `name` (mail
     subjects), `track_changes`, pending refs — the shared accessProj
     projection silently omitted them → blank mail subjects, 403s.
  4. `colVa` = `Validation error: <msg> at "<path>"` — path quoted, msg must
     NOT repeat the path.
  5. Transfer fixture MUST be a real `/project/new` project (TPDS flush
     needs `overleaf.history.id`; raw-mongo-seeded → Node 500).
- files: `features/projectlist/collab.go` (new ~750 lines), `core/response.go`
  (NoContent 204), `projectlist.go` (7 routes), flip `web-p4col.conf`, gate
  `web-go-p4col-flip.test.e2e.ts` (3-leg, 50+ cases, 8-mail assertion, ghost
  coverage).
- full `web-go P4` regression green after P4.10a (33 passed 3.2m).

#### P4.10b — project invites: create/list/revoke/resend, invite view, accept, sharing links — **✅ GATE 3/3 GREEN x3 runs (2026-09-15)**
- Ten invite routes byte-pinned against Node (CE, `sharing-updates=enabled`):
  `POST /invite` (admin; 200 `{invite}` / self-invite 200+error / bad email /
  bad privilege / unknown-key VA 400 / non-admin 403 / ghost 404),
  `GET /invites` (admin JSON list), `DELETE /invite/:id` (revokes; anon 403
  CSRF), `POST /invite/:id/resend` (201 + mail; missing → 404 sendStatus),
  `GET /invite/token/:tok` (React invite shell 200 / bad token 404 Invalid
  Invite page / member → 302 / ghost project 404 / ghost sender → Invalid
  page), `POST /invite/token/:tok/accept` (302 redirect, 204 XHR, bad token
  404, anon 403 CSRF), `GET /tokens` (link-sharing 500 when
  `tokenAccessReadOnly_refs` missing — Node Mongo-crash replicated),
  split-test-disabled `sharing-link` / `share` / `share/validate` → 403
  generic page with PATH slot.
- Invite mail byte-parity: Node text+HTML parts captured verbatim
  (`invite_mail_data.go`), raw-SMTP `SendExact` (CRLF framing + `\r\n.\r\n`
  DATA terminator for the sink), nodemailer-style Q-encoded subject
  reproduced by `invSubjectLines` (unit-tested in `invsubject_test.go`),
  6-mail battery pinned (recipient + subject).
- Gate hardening (the real cost of this unit): `tokenFromSink` is now
  **live-state driven** — it waits for a sink mail whose token HMAC matches a
  token still live in `projectInvites` (mongosh container resolved in the
  top-level scope — a `ReferenceError` had silently disabled the live check
  and made the gate consume stale mails). Invites are consumed single-use,
  so the battery keeps **exactly one live invite per email at each accept**
  (repeat-accept pile removed: Node's `addUserIdToProject`/`revokeInviteForUser`
  over a multi-invite pile flips member/non-member branches non-deterministically
  in this fork — pinned 2026-09-15).
- Go accept contract pinned: 302 (204 XHR) + single-use invite consumption +
  sender contacts; **no member-refs write** (Node's accept-path `$addToSet`
  applies or gets cast-stripped per worker — non-deterministic; refs excluded
  from state parity, owner/track_changes/invite-lifecycle are the anchors);
  member branch (upgrade + 302, no consumption) kept for P4.10a parity.
- files: `features/projectlist/invite.go` + `invite_mail_data.go` +
  `invsubject_test.go` (new), `core/mail.go` (`SendExact` raw SMTP),
  `views/pages_invite.go` + `pages_invite_data.go` (invite shell / Invalid
  404 / 403 restricted page), `projectlist.go` (10 routes), flip
  `web-p4inv.conf` (method mismatches proxy to Node upstream — nginx forbids
  `proxy_set_header` inside `if`), gate `web-go-p4inv-flip.test.e2e.ts`
  (3 legs, ~40 cases, 6-mail assertion, sink-token recovery).
- full `web-go P4` regression green after P4.10b (36 passed 3.6m).

#### P4.11a — editor entity creation: `POST /project/:id/doc` + `POST /project/:id/folder` — **✅ GATE 3/3 GREEN x3 runs (2026-09-15)**
- Node oracle (probe 4, live-pinned 2026-09-15): addDoc 200
  `{"name","_id"}` (trimmed name); doc store `POST
  {docstore}/project/:pid/doc/:did` fired on every doc path that passes
  `isCleanFilename` (blocked/dup 400s included); entity-count > 2000 →
  i18n JSON; path > 1024 → 400 `path too long`; top-level blocked JS
  property names (`toString`, `constructor`, … 14 words) → 400 `blocked
  element name` (docs/files only, folders + subfolders exempt); duplicate →
  400 `file already exists` (checked after the docstore call, matching
  Node's `_putElement` order); bad/ghost parent → 404 page; non-member →
  403 `{"message":"restricted"}`; anon + valid-CSRF → 401 `Unauthorized`;
  PUT/GET method mismatch → Node 404 (flip method-guard proxies to Node);
  strict body VA (`name:null` → `expected string, received null at
  "body.name"`, extra key → `Unrecognized key at "body"`, `name:42` →
  `expected string, received number`), raw name ≥ 150 chars → 400
  text/plain `Bad Request`.
- **Folder error shape diverges from doc** (pinned): invalid folder names
  (`'   '`, `'../escape'`, `'a*b'`) → 400 `application/json`
  `"Invalid File Name"` (i18n `invalid_file_name`); invalid **doc** names
  → 400 text/plain `invalid element name`.
- Go: `projectlist/entadd.go` (SafePath BADCHAR/BADFILE replica —
  `[/\\*\x00-\x1F\x7F\x80-\x9F]`, `^.$|^..$|edge-space` — RE2-safe,
  blocked-word table, full-tree parser tolerant of `primitive.M/A` driver
  shapes, parent-folder resolver, docstore client, `_putElement` check
  order, single `findOneAndUpdate` $push/$inc/$set write with
  `rootFolder.N` filter → MatchedCount 0 → Node's 500), routes registered
  in `projectlist.go`, flip `web-p411a.conf` (POST-only; other verbs proxy
  to Node — no in-`if` `proxy_set_header`), gate
  `web-go-p411a-flip.test.e2e.ts` (3 legs, ~35 cases, state anchor:
  rootFolder tree names + version + lastUpdatedBy).
- 404/500 page PATH slot: skel renders `origin + "/" + PATH` — pass the
  request path **without** the leading slash (`TrimPrefix`) or the
  alternate-`href` grows one byte vs Node (caught by the gate).
- Full `web-go P4` regression green after P4.11a (39 passed 3.8m).

#### P4.11b — editor entity DELETION: `DELETE /project/:id/{doc,file,folder}/:entity_id` — **✅ GATE 3/3 GREEN x3 runs (2026-09-15)**
- Node oracle (live-pinned 2026-09-15): auth (anon → 401 sendStatus; anon +
  **invalid** CSRF → core 403 `Forbidden`; non-member → 403
  `{"message":"restricted"}`) → zz.objectId(params) → 404 **JSON** VA
  `{"error":"Validation error: Invalid Mongo ObjectId at \"params.
  <Project_id|entity_id>\"","statusCode":404}` (params level = 404, not 400)
  → `flushProjectToMongo` (document-updater dead in this stack; failures
  tolerated — deletes still 204) → **root-folder guard** → 422 text/plain
  `cannot delete root folder` (requires `rootFolder[0]._id` present — a
  malformed project without the id 500s on Node's `.toString()`; real
  projects always have the id) → `findElement` (missing or wrong type →
  404 page) → `findOneAndUpdate({_id}: {$pull: {<parentArrayPath>: {_id}},
  $inc: {version:1}, $set: {lastUpdated, lastUpdatedBy}, [$unset
  rootDoc_id]})` (MatchedCount 0 → Node 500) → **_cleanUpEntity**: per doc
  in the deleted subtree → docstore `PATCH {docstore}/project/:pid/doc/:did
  {deleted:true, deletedAt, name}`; **docstore 404 (doc not there) →
  NotFoundError → HTTP 404 page AFTER the tree write already committed**
  (pinned via seeded doc without docstore entry: state mutated + 404)
  → 204 No Content (no body, no content-type).
- Subtle pins: root-doc deletion **unsets `rootDoc_id`** (same write via
  `$unset`); deleting a folder cleans every doc in its subtree (docstore
  PATCH each); `findElement` order = element arrays before recursive
  folders, folders in index order; doc→docs, file→fileRefs, folder→folders;
  PUT to a DELETE route → flip method-guard → Node express default 404 page
  (`Cannot PUT ...`).
- Go: `projectlist/delent.go` (handler mirrors the Node order exactly, reuses
  P4.11a tree parser + `entCanWrite` + `loadProjectFull` + `fireHTTP` +
  `cduBase`; new: `delFind` mirror of `findElement`, `delSubtreeDocs`,
  `entPatchDoc` docstore client, 404-VA `delParamVA`); routes registered in
  `projectlist.go` with `([^/]+)` segment patterns (the core router would
  404 non-hex paths before the handler ever ran — Node's Express passes
  them through for the VA response); flip `web-p411b.conf` (DELETE-only
  guards); gate `web-go-p411b-flip.test.e2e.ts` (3 legs, 20-case stateful
  battery + state anchor: flat tree names + version + lastUpdatedBy +
  rootDoc presence + docstore row count per project).
- Driver quirk (cost a gate failure): `UpdateOne(ctx, filter, bson.A{...})`
  is treated as an **aggregation pipeline** ("Unrecognized pipeline stage
  name: '$pull'") — pass a top-level `bson.D` document for operator
  updates; conditional append is `primitive.E`.
- Seeded docs need **real docstore entries** (POST the Go docstore API, as
  Node's addDoc does) — deleting a doc without an entry is a distinct
  Node 404-after-write case (covered by `delOrphan`).
- Full `web-go P4` regression green after P4.11b (42 passed 4.2m).

#### P4.12a — file proxy: `GET|HEAD /Project/:Project_id/file/:File_id` — **✅ GATE 3/3 GREEN x3 runs (2026-09-15)**
- Go: `features/projectlist/fileproxy.go` (`fileProxyHandler`, `fproxyFindFile`,
  `fproxyHistoryID`) + route in `projectlist.go` + flip
  `server-ce/nginx/flips/web-p412.conf` (GET|HEAD → Go; other verbs → Node;
  case-sensitive `/Project/…` vs lowercase editor `/project/…`).
- Chain: web → **history-v1 (Node, 127.0.0.1:3100)** `GET /api/projects/{hid}/blobs/{hash}`
  (basic auth `staging:$V1_HISTORY_PASSWORD`; hash = 40-hex sha1)
  → **Go filestore** read-only (local-FS backend
  `/var/lib/overleaf/data/history/overleaf-project-blobs/<fmt(hid)>/<h[:2]>/<h[2:]>`,
  `fseProjectKeyFormat` = pad-9 reverse 3/3/rest).
  `overleaf.history.id` has a UNIQUE index (one history id per project).
- Pinned Node oracle (gate battery, 12 cases): 200 member (CD
  `attachment; filename="<name>"`, `Cache-Control: private, max-age=3600`,
  **no Content-Type**, **chunked no-Content-Length**, body=blob); HEAD →
  **404 empty** (history-v1 registers NO HEAD blob route in this fork —
  quirk replicated); ghost file → 404 empty; ghost project → 404 app page;
  bad oid → 404 JSON VA (`params.Project_id` / `params.File_id`,
  statusCode 404); non-member → 403 (`{"message":"restricted"}` accept-json /
  restricted page accept-html — page now renders the REQUESTER's email + id,
  see fix below); anonymous → 401 `Unauthorized` (accept json) / 302 /login
  (else); non-GET verbs → Node CSRF 403 `Forbidden` (even anonymous — flip
  guard keeps them on Node, P4.11b-delPutMismatch precedent).
- **Views bug fixed here (pre-existing):** `restrictedHTML` in
  `go/services/web/views/pages_data.go` was captured with the capture user
  BAKED IN (literal `ol-usersEmail` + `ol-user_id` + account-menu email,
  plus 3 baked-in `_csrf` hidden values) — any requester other than the
  capture user saw the wrong identity on EVERY Go 403-restricted page.
  Now slotted (`\x01OLUSERS\x02` / `\x01OLUID\x02` / `\x01CSRF\x02`); the
  non-member-with-different-user battery case pins it.
- **Latent (deferred to a view-audit unit):** `pages_data_p2.go` (login /
  register / token pages) still bakes the capture user into `ol-user` JSON
  meta + `sessionUser` fragments — hidden in all prior gates because the
  capture user == the only test user.
- Gate tolerated deltas (documented in the spec): Node omits Content-Type
  vs Go's net/http text sniff (browser fallback identical) — CT_NORMALIZED
  set; Node forwards no Content-Length and Go now omits it too → both chunked.
- Full `web-go P4` regression green after P4.12a (42 tests; leg-1 flakies
  self-heal on retry as in prior runs).

#### P4.12b — document download: `GET|HEAD /Project/:Project_id/doc/:Doc_id/download` — **✅ GATE 3/3 GREEN x3 runs (2026-09-15)**
- Go: `features/projectlist/docdl.go` (`docDownloadHandler`, `docdlFindDoc`
  — findElement-ordered raw-tree walk for type doc) + route in
  `projectlist.go` + second `location` in `server-ce/nginx/flips/web-p412.conf`
  (same GET|HEAD→Go / other-verbs→Node guard).
- Chain: web → **document-updater (Node, 127.0.0.1:3003)**
  `GET /project/{pid}/doc/{did}?fromVersion=-1` → `{lines, …}` → web renders
  `lines.join('\n')` as `text/plain; charset=utf-8` with
  `Content-Disposition: attachment; filename="<doc.name>"`.
- Pinned Node oracle (13-case battery): 200 as above (weak ETag on both);
  **HEAD 200 same headers, no body** (express auto HEAD — contrast the file
  proxy's HEAD-404 history-v1 quirk); ghost doc (valid project) → **404
  text/plain 'Not Found'** (sendStatus); ghost project → 404 app page; bad oid
  → 404 JSON VA (`params.Doc_id`/`params.Project_id`; enforce-log enforces
  logOnly schemas); non-member → 403 JSON / restricted page (page renders the
  REQUESTER — exercises the P4.12a restrictedHTML fix); anonymous → 401/302;
  non-GET → Node CSRF 403 'Forbidden' (flip guard).
- **Unpinned rare edge (documented, not in battery):** doc in tree with no
  DU/docstore backing → DU 404 → Node 500 'Internal Server Error'
  (non-NotFoundError path); Go mirrors with 500.
- Gate seed note: a docstore POST right after a project re-seed can transiently
  miss once (stack settling) — the seed retries 4x/1s and logs code+body;
  without the retry the gate flakes.
- Full P4 regression green after P4.12b (45 flip tests; the same two leg-1
  flakies self-heal on retry as in prior runs).

#### P4.12c — private API doc trio: `GET|POST /project/:Project_id/doc/:doc_id` (+ `?plain=1`) + `POST /project/:Project_id/doc/:doc_id/changes/reject` — **✅ GATE 3/3 GREEN x3 runs (2026-09-15)**

`services/web/app/src/Features/Documents/DocumentController.mjs` +
`DocumentRouter.mjs` (privateApiRouter basic-auth surface, no session, no CSRF):

- **Go**: `features/projectlist/docapi.go`
  - basic auth gate first (Node order) → 401 `WWW-Authenticate: OverleafLogin`,
    `text/plain "Unauthorized"` + ETag, **no** `nosniff` (errorController 401
    path), XPB+CSP kept.
  - `GET` → docstore GET + chat resolved-thread-ids → pinned key-order JSON
    (`lines,version,ranges,pathname,projectHistoryType,historyRangesSupport,otMigrationStage,
    [projectHistoryId],resolvedCommentIds`) — `otMigrationStage:0` literal pinned
    (Node never omits); `?plain=1` → `text/plain` lines **+ `nosniff` (helmet on
    `res.send`)**; ghost project/doc → 404 `Not Found`; bad-oid → 404 zod-VA JSON.
  - `POST` (setDoc) → zod VA **params-first + exact messages** → docstore POST
    (Node's own path is docstore-direct via ProjectEntityUpdateHandler — no DU) →
    `{"rev":N[, "modified":true]}`.
  - `POST .../changes/reject` → 204 + `ETag: W/"a-bAsFyilMr4Ra1hIU5PyoyFRunpI"`
    (Node `res.status(204).send("No Content")` ETags the stripped body — replicated
    verbatim).
  - `NoSession` route option in `core/app.go` route struct (skips session+CSRF,
    mirrors Node's privateApiRouter); XPB per-response (Go core otherwise omits it).
- `runit/web-go-overleaf/run` exports `WEB_API_USER`/`WEB_API_PASSWORD` read from
  `/etc/overleaf/settings.js` (container node one-liner; NUL-free `:`-delimited
  parse).
- **flip**: `web-p413.conf` (reject POST → Go) + `web-p411b.conf` extended to
  `DELETE|GET|POST` for the doc pair (Node web profile has none of these routes →
  flip retargets 7420 to the API contract).
- **gate** `specs/parity/web-go-p413-flip.test.e2e.ts`: 19-case battery
  (reset/get-plain/no-auth×2/wrong-cred/bad×2/ghost×2/no-auth-POST/VA×5/setDoc/
  setDoc-noop/reject), Node :3000 oracle vs Go :4010 + Node baseline re-capture
  (3-leg), state-dependent `modified`/rev shape-compared + etag-exempted; plus a
  live-flip nginx acceptance leg (apply both confs → 7420 401/200/POST-200/204/
  unflipped-PUT-403 → strip, with reload race-retry and post-reload fetch retry).
  5/5 ×3 consecutive green; **47-test P4 regression green**; go build/vet/test green.
- Gate-plumbing lessons (reusable): flip vhost splice is node-driven (no sed/shell
  quoting), reload has a 3× retry, and nginx-assert fetches retry across the
  reload window (dropped sockets); `runLeg` fails loud when a leg's write-cases are
  unstable instead of returning a broken map.


#### P4.13a — project upload: `POST /Project/:Project_id/upload` — **✅ GATE 3/3 GREEN x3 runs (2026-09-15)**

`UploadsRouter.mjs` + `ProjectUploadController.uploadFile` + `FileSystemImportManager` (addDoc/addFile) +
`ProjectEntityUpdateHandler.upsertDoc/upsertFile` + `DocumentUpdaterHandler.updateProjectStructure`
— the file↔doc swap engine. Thin-client surface: docstore :3016 (doc create/set),
v1-history :3100 (blob PUT), DU :3003 (setDocument + structure ops) — all shared with Node.

- **Go**: `go/services/web/features/projectlist/upload.go` (self-contained unit:
  `upTarget` tree resolution, `upClassify` extension→content text/binary split
  mirroring FileTypeManager, `upDoDoc`/`upDoFile`, `upDocstorePut`, `upDUSetDoc`,
  `upUpdateStructure`, `upPutBlob`, `upSwap*`, `upPush*`, `upReplaceFile`).
  Request order pinned from Node: csrf(global) → Project_id VA (404 JSON) →
  folder_id PRESENT-validated (400 objectId; absent legal → root) → multer single
  `qqfile` (no file → 400 invalid_upload_request; wrong field name → 500 HTML
  page) → name length (422 invalid_filename) → project + `entCanWrite`
  (403 `restricted` / `{"message":"restricted"}` accept-dependent) → SafePath
  (422) → folder lookup (422 folder_not_found) → upsert.
  - doc new: docstore POST → `$push docs {name,_id}` → DU add-doc.
  - doc re-upload: DU setDocument only (DU flushes docstore + project).
  - file new/upsert: v1H blob PUT (git-blob-SHA1) before the mongo write;
    upsert = `$set fileRefs.idx.{_id,hash,linkedFileData:null,created}` +
    `$inc version` + `$inc fileRefs.idx.rev` + DU [rename-file, add-file].
  - file↔doc swaps: `$pull` one side, `$push` the other, DU [rename-*, add-*].
- **Bug fixes found by this unit** (both latent, pre-existing):
  (a) `entCanWrite` only read `collab_refs` + `owner_ref` — Node's real field is
    `collaberator_refs` (typo included); Go now accepts both + `owner` fallback;
  (b) `views.Error500Page` rendered an empty `mailto:` — now honors
    `OVERLEAF_ADMIN_EMAIL` with the CE default `placeholder@example.com`
    (Node `settings.adminEmail` parity).
- **Seed/gate environment pin** (cost a long debug arc): a mongo-direct seeded
  project MUST carry `owner_ref` + the `collab*_refs` family (Node
  `CollaboratorsGetter.getProjectAccess` 500s on missing `owner_ref`) and
  `main.tex` MUST be registered in the docstore (loopback :3016 — from inside
  the overleaf container) or every DU `setDoc`/structure call 404s → Node's
  catch-all `422 {"success":false}` / `500`.
- **Contract** (fresh-session oracles, 2026-09-15): 200 doc / file+hash;
  400 `invalid_upload_request`; 400 oid-VA for present-empty + malformed
  `folder_id` (absent legal); malformed `Project_id` → 404; ghost project →
  404 HTML Page-Not-Found; 403 `restricted` JSON (non-member, accept:json);
  403 `Forbidden` text (anon, CSRF-first); 422 `invalid_filename` /
  `folder_not_found`; 500 generic page with admin mail (wrong field name =
  multer `LIMIT_UNEXPECTED_FILE`).
- **flip**: `server-ce/nginx/flips/web-p413a.conf` (POST-only → Go; other
  verbs fall back to Node).
- **gate**: `specs/parity/web-go-p413a-flip.test.e2e.ts` — 17-case battery
  (6 write legs incl. both cross-type swaps + 8 error legs + 3 auth legs),
  3-leg Node→Go→Node with re-seed per leg + normalized state dump (tree flat
  + version + lastUpdatedBy); multipart via browser fetch FormData with the
  csrf token in the `x-csrf-token` header (both stacks accept it; avoids the
  Go `FormValue` body-consumption path). normBody must cover the logout-form
  `_csrf` hidden input (the Page Not Found page embeds it) — the one diff
  found in the first leg-2 run. **3× consecutive gate green**; full P4
  regression (p4a–p4g, p4hb, p411a/b, p412a/b, p412c trio, p4clone/col/del/
  inv, p3x, p0/p1/p2) green (p4f/p3c leg-baseline flakes are pre-existing
  wall-clock/session tails, green on solo re-runs); `go build/vet/test
  ./go/... ./cmd/...` green.
- **Deferred to P5**: socket emits on successful upload
  (`reciveNewDoc` / `removeEntity` / `convertFileToDoc` / `convertDocToFile`)
  and the `project-upload`/`file-upload` rate-limit counters (the gate stays
  under the 20pts/60s budget; Go enforcement is optional until the editor
  surface lands).


1. **Docstore** (436) + **FileStore** (422) + **Documents** (304) +
   **LinkedFiles** (1,537) + **Uploads** (1,637) — *thin clients of the Go
   docstore/filestore services already in production*: highest value/effort
   ratio in the whole plan.
2. **Chat** (517) + **Notifications** (576, web-side proxy of the Go chat +
   notifications services).
3. **Project** (7,947: list/CRUD/duplicate/delete/options/audit log writer) — **list (P4.1) +
   rename (P4.5) + flag-writes archive/trash (P4.6) + creation basic (P4.7) + example
   (P4.7b) + delete/restore (P4.8) + clone (P4.9) done**; options/settings stay.
4. **Collaborators** (3,791) — the read-side `/project/:id/members` (P4.3) and
   `/project/:id/access-requests` (P4.4) are done; **P4.10a set-level / leave /
   remove / request / grant / transfer mutations done** (2026-09-14/15);
   **P4.10b invites + sharing-links + token-acceptance DONE (2026-09-15)**.
5. **History** (2,556 — clients of history-v1/project-history services).
6. **ThirdPartyDataStore** (1,189), **Templates** (293) + template-gallery
   module (5.3k), **Launchpad** (1,774).

### P5 — editor & compile (highest coordination cost, deliberately last among core)

> **Status (2026-09-15): P5.1a editor page + P5.1b editor detach shell ✔ BOTH GATES 3/3 GREEN (Node baseline → Go parity → Node re-baseline) + P4 project-list regression 3/3 + go build/vet/test green. The whole editor PAGE surface (main + detacher + detached) is now Go.**
> Route `GET /editor/:id` + legacy `GET /Project/:id` (owner) byte-parity on the 35 kB editor
> bootstrap shell (68 `<meta name="ol-…">` slots + per-request CSP nonce + title +
> ol-navbar.currentUrl), **and `/editor|/Project/:id/detacher` + `/detached`** (the
> `ide-detached` chrome: `/stylesheets/ide-detached-*.css`, `<main id="pdf-preview-detached-root">`,
> `ol-detachRole` detacher|detached, full-path currentUrl). Go: `go/services/web/views` (slot
> renderer `EditorPage` selects `editorTemplate` vs the new generated `detachedTemplate`;
> `__PROJNAME__`/`__TITLE__`/`__NONCE__`/`__ORIGIN__`/`__CURRENTURL__` placeholders, all slot +
> title/name values html-escaped to match pug) + `go/services/web/features/editorpages` (one
> handler for all 6 routes via detachRole; user/project loads, `serializeUser`/`buildUserSettings`/
> `ol-navbar`/pinned config JSON, response envelope, 404/302/403 envelope parity) + unit tests
> `views/editor_test.go`; flips `web-p51a.conf` (main) + `web-p51b.conf` (all 6); gates
> `web-go-p51a-flip` + `web-go-p51b-flip`.
> Findings pinned: editor CSP adds `img-src 'self' data: blob:`; `<title>` =
> `<projectName> - OlliTeX, Online LaTeX Editor`; twitter/og:title + `ol-projectName` render the
> **real project name** (was a latent P5.1a bug — `WebGo-Ren-N` fixture was baked into the template;
> fixed with a `__PROJNAME__` placeholder in both templates); `ol-otMigrationStage` content=0; dates
> decode as `primitive.DateTime` (ms epoch) → ISO-8601 UTC; csrf rotates per render (gate normalizes);
> the detach pages do NOT add `id="ol-detach-role"` (that attr is only on the main shell — the P3e
> oracle was stale, Node truth = main-only, byte-verified in Node). **P5.1 done. Next: P5.2 (editor
> real-time / DocumentUpdater / OT / compile control plane).**

Editor (1,418: `/Editor/:id` open + OT plumbing to real-time),
DocumentUpdater (600 — client of the document-updater service),
Compile (4,380 — ClsiManager ↔ clsi service: compile triggers, logs, output
files; the compile *engine* remains clsi/Node; we replace the **control
plane**).
**P5 gate**: full open → edit (shared session with a Node-open second client) →
compile → PDF render journey, plus disconnect/storm soak.

**P5.2 — compile control plane (KICKOFF, scoped 2026-09-15; oracle captured):**
The compile *engine* stays the Node `clsi` / `clsi_typst` services (alive, `clsi` on
`:3013`). We port the **web control plane** (Go) that sits between the browser and
clsi. Routes (all owner-gated, per-`Project_id`):
- `POST /Project/:id/compile` → `CompileController.compile` (parse body/query;
  `CompileManager.compile`: recently-compiled redis check, auto-compile limits,
  `ensureRootDocumentIsValid`, project+limits, then `ClsiManager.sendRequest`).
  **clsi contract** (Node `ClsiManager._buildRequest` → `_finaliseRequest`): POST
  `{compile:{options:{buildId,editorId,compiler,timeout,imageName,draft,stopOn
  OnFirstError,check,compileGroup,populateClsiCache,enablePdfCaching,flags,metrics
  Method,metricsPath,…},baseHistoryVersion,rawSnapshot,rawChangeOperations,resources:
  [{path,content}]‖[{path,url:<filestore blob>,modified}]}}` to `Settings.apis.clsi.url`
  (= `http://clsi:3013`) at `/project/:pid/user/:uid/compile?compileBackendClass=…&
  compileGroup=…`; typst → `:3014`. `resources` docs = `doc.lines.join('\n')` from the
  **Go** docstore + files from **Go** filestore (both already ported). Web re-shapes clsi's
  response into `res.json`.
- `POST /Project/:id/compile/stop` → `CompileManager.stopCompile` → clsi `/compile/stop`; 200.
- `GET /Project/:id/output/cached/output.overleaf.json` → `ClsiCacheController.getLatest
  BuildFromCache` (clsi-cache; 404 when not populated — the e2e default).
- `GET /download/project/:id/build/:build_id/output/output.pdf` → `CompileController
  .downloadPdf`; `GET /download/project/:id/build/:editorBuildId/output/cached/:filename`
  → `ClsiCacheController.downloadFromCache` (path-allowed filter).

**Oracle (Node, success compile of a basic project):** `POST …/compile?check=validate&
draft=true` → **200** `application/json`: `{"status":"success","outputFiles":[ output.aux,
out.fdb_latexmk, output.fls, output.log, output.pdf(+size,createdAt,ranges), out.stdout,
out.synctex.gz ] (each `{path,url,type,build}`), "outputFilesArchive":{"path":"output
.zip",url,type:"zip"}, "compileGroup":"standard","compiler":"pdflatex","stats":{isInitial
Compile,latex-runs,pdf-size,…},"timings":{sync,compile,output,compileE2E,…},"outputUrl
Prefix":""}`. Absent (undefined → omitted): `buildId,clsiServerId,clsiCacheShard,
validationProblems,pdfDownloadDomain,pdfCachingMinChunkSize`. The `outputFiles[].url` +
`.build` carry clsi's **random per-compile buildId**; `createdAt` is a timestamp; `output
Files[].url` point at **clsi's** output nginx (`/project/:pid/user/:uid/build/:build/output/
:file`), not the web host.

**P5.2 parity gate design (≠ the P4/P5.1 byte-diff gate):** a raw body diff is NOT valid
because clsi's `buildId`, `createdAt`, `stats.*`, `timings.*` are non-deterministic per
compile. The gate must compare the **structured contract** — `status`, `compileGroup`,
`compiler`, `outputFiles` **set of `{path,type}`** + presence of `output.pdf`/`outputFiles
Archive`, header `content-type` — and **normalize** `build`(id), `createdAt`, `size`?,
`stats`, `timings` (assert shape, not values). Node baseline → Go parity → Node re-baseline,
flushing the `clsi_recently_compiled` redis check between legs (else Go leg gets `too-
recently-compiled`). **Go impl**: new `go/services/web/features/compile` (compile trigger
+ stop + cache/pdf read via the existing Go docstore/filestore + a small clsi HTTP client);
recently-compiled + compile-limits (user settings) + root-doc validation reuse Go core redis
+ sitesettings. Split likely: P5.2a trigger+stop, P5.2b output/PDF/cache read. (Larger
edge modes — history/incremental/png2pdf/clsi-cache — are separate, lower priority.)

**P5.2a — compile trigger + stop (DONE, GATE 3/3 GREEN, 2026-09-16):**
`go/services/web/features/compile/compile.go` ports the web control plane:
- `POST /project/:id/compile` (case-insensitive, like Express) → auth (404 invalid-id
  JSON / 404 absent-id HTML / 403 non-member JSON-or-HTML), read-access check,
  recently-compiled Redis guard (`SETNXEXReply`), clsi POST (free backend, project's
  clsiGroup), 30s timeout, and the **exact Node response reshape** (status, outputFiles
  with pathname-only URLs + build + pdf ranges/size, outputFilesArchive, compileGroup
  (default `standard`), compiler, clsiServerId/clsiCacheShard, validationProblems,
  stats, timings, outputUrlPrefix — present-empty-string passed through). 504 on
  timeout/failure with `validationProblems: [{type: "compilationFailed"}]`.
- `POST /project/:id/compile/stop` → clsi stop + `200 text/plain OK` (Node
  `res.sendStatus(200)`), never errors (clsi resolves even when nothing compiles).
- `core/redis.go` gained `SETNXEXReply` to distinguish "key already existed"
  (recently-compiled) from "set now" on the `SET key true EX 1 NX` guard.
- Flip: `server-ce/nginx/flips/web-p52a.conf` (POST-only, both routes → :4010).
- Gate: `web-go-p52a-flip.test.e2e.ts` (3-leg, deterministic offline). Not a byte-diff —
  it normalizes buildId/createdAt/size/nonce/csrf/hex-ids; compares status/CT/length/
  body-shape. Covers: success (with real clsi PDF, output.pdf + output.zip archive),
  PAIR (success + too-recently-compiled from a concurrent second compile), stop,
  non-member 403 (JSON + accept-dependent HTML), anonymous Forbidden, invalid-mongo-ObjectId
  404 JSON, absent-id 404 HTML. Node baseline ↔ Go ↔ Node re-baseline all pass.
- `go build`/`go vet` clean; unit tests `features/compile` + `core` pass.

**P5.2b — compile output read (DONE, GATE 3/3 GREEN, 2026-09-16):**
`go/services/web/features/compile/download.go` + routes in `compile.go Feature()`: `GET /download/project/:pid/build/:build_id/output/output.pdf` — authz (P4 preflight), param/query validation (404 JSON "Invalid buildId…" / 400 JSON editorId), sets `application/pdf` + `inline|attachment; filename="<safeProjectName>.pdf"` (Node `safeProjectName` = name minus `\p{L}\p{Nd}` → `_`), streams from clsi downloadHost (127.0.0.1:8080 here) forwarding ONLY Content-Length + Content-Type (+ `X-Accel-Buffering: no`), bare 404 (CT/CD kept) on upstream 404 — all pinned against the live Node oracle. `GET /project/:pid/output/cached/output.overleaf.json` → `SendStatus(404)` "Not Found". `GET /download/project/:pid/build/:editorBuildId/output/cached/:file` → editorId-buildId + filename allow-list validation, then bare 404 (Node `status(404).end()`, no CT) — the clsi-cache service is DISABLED in this stack (`CLSI_CACHE_INSTANCES=[]`), so the 404 shapes ARE the Node contract (enabled-clsi-cache behavior = documented out of scope). Unit: `safeProjectName` (CJK/Hebrew/digits parity), buildId regexes, cache allow-list. Flip `web-p52b.conf` (3 locations → :4010). Gate `web-go-p52b-flip.test.e2e.ts`: Node baseline → Go → Node re-baseline over pdf_inline/popup/badbid/missing, cached_json/file/badfile/badbid, auth403 (non-member fixture), anon, anon_json; PDF bodies compared after normalizing per-compile volatile fields (pdfTeX `D:…Z` + `/ID [...]` — two real compiles verified byte-identical after the two normalizations). Root-cause fixes were gate-side only (`x-csrf-token` body extraction; the two normalizations).

**Regression sweep 2026-09-16 (all green):** P5.2a 3/3, P5.1a 3/3, P5.1b 3/3, P4.13b 3/3, P4.1 27 passed; `go build/vet/test ./go/... ./cmd/...` green; gofmt clean on P5.2b files (flagged files pre-existing).

### P6 — modules (41 total; each a self-contained flip, roughly in this order)

**P6.1 — ollitex-hub (DONE, GATE 3/3 + SANITY GREEN, 2026-09-16):**
`go/services/web/features/hub/hub.go` (+ `hub_test.go`) registered in
`cmd/web/main.go`: the `/hub`, `/hub/admin`, `/hub/workspace` pages
(`views/hub_template.go` + `views/pageslots.go` shared slot renderer with the
P5.1a editor page; hub locals: per-user `ol-ExposedSettings`
(canManageTemplatesMenu flip for admin), hub `ol-navbar` (customLogo +
Library/Templates items, admin canDisplayAdminMenu/
canDisplayProjectUrlLookup flip), `ol-hub-theme` page slot (stored theme JSON
or `null`), `ol-user` (hydrated-ace parity — see below), `ol-userSettings`
(shared `editorpages.BuildUserSettings` with mongoose-default semantics), hub
`ol-footer` (`showThinFooter:false` override via `editorpages.HubFooterJSON`)).
Theme API: `GET/PUT/DELETE /api/hub-theme` (CSRF-gated, PUT validates light
+ dark against the Node `validateMode` contract — 8 hex colors,
fontFamily ≤300 chars, fontSize 12–22, radius 2–24 — errors
`400 {"error":"<where>: ..."}`; malformed JSON → `400 {}`;
`urlencoded` nested-body parity; upsert `hubthemes` doc `documentId:
"default"` stored as an ORDERED canonical-key-order doc so Node's lean GET
byte-match survives round-trips), `GET /api/hub/health` (admin; instance
+ featureGates pins from `server-ce/config/settings.js` + env, volatiles
generatedAt/uptimeSec/platform/pid/mongo normalized by the gate),
`GET /api/hub/notes` (RELEASE_NOTES.md bytes or `400/404`-free
`{"error":"release notes not available"}`).

**Parity bugs found by the gate and fixed** (each was a real divergence):
1. `saveTheme` was missing the upsert flag — PUT on a cleared doc no-oped;
   now `options.Update().SetUpsert(true)` (driver v1 API).
2. Mongo driver v1.17 decodes a struct field typed `any` to `bson.D`, not
   `map[string]any` — `modeFromStored` type-asserted the map and fell to
   null; `themeDoc` now declares `Light/Dark bson.D` and serializes it
   order-preserving (`bsonDJSON`, anyJSON extended with int32/bson.D).
3. Go map `$set` randomized the stored light/dark key order; `orderedMode`
   now stores canonical order (Node's validated-object order) so the
   lean GET bytes match Node exactly (492 B canonical put/get pin).
4. `ol-user.ace`: Node stringifies the hydrated mongoose doc. Rich stored
   ace (admin, p34-happy) = stored-order verbatim; sparse stored ace
   (e2e-user fixture: `{customKeybindings:{}}`) hydrates to the canonical
   defaults shape (ref providers first — zotero/mendeley/papers — then the
   field block, customKeybindings last). `serializeHubUser` implements both
   paths (rich = `bsonToJSON` stored order; sparse =
   `sparseAceJSON` + `aceProviderJSON` with the
   `{enabled:true,disablePersonalLibrary:false,groups:[]}` defaults).
5. `BuildUserSettings` (shared editor+hub): restored the mongoose schema-default
   fill-in (mode/theme/fontSize/autoComplete/… with `syntaxValidation`
   omitted when absent, `overallTheme` derived via the 2026-03-02
   signUpDate cutoff when absent, ref-provider blocks rebuilt in
   UserSettingsHelper order), fixing the P5.1a/P5.1b `ol-userSettings`
   drift while keeping both editor gates green.
6. Flip verification hardened: gate now asserts the exact include count
   (`grep -c` = 0/1) after flip/strip, nginx-t passes, and the leg-2
   routing leg is checked against a live-502 canary — a silent flip
   no-op (Node-vs-Node) can no longer false-green.

Gate `web-go-p61-hub-flip.test.e2e.ts` (3 legs + canonical put/get pin):
login flows, both CSRF 403s, both admin-gate 302
`/restricted?from=` bounces (member `Accept: application/json` → JSON 403;
HTML → 302), theme validation 400 battery (light/dark × 12 field shapes,
array-body, urlencoded, empty-body, malformed-JSON 400 {}), put/get
canonical, clear + null get, both health JSON (volatile-normalized) +
featureGates byte-pin, notes 1913 B byte-pin (md5
`7468d83a9a1a5ccc1b48c951f6a2fd04`), page bytes for user/admin ×
default/theme, workspace redirect. **All legs green.**
Regression sweep after P6.1: P5.1a 3/3, P5.1b 3/3, P5.2a 3/3, P5.2b 3/3,
P4.13b 3/3, P4.1 3/3, hub-endpoint-contract 2/2 — 23 e2e legs + contract
battery, zero failures.

**P6.2 — admin-tools project surface (DONE, GATE 4/4 GREEN, 2026-09-16):**
`go/services/web/features/projectlist/admin.go` + `flips/web-p62.conf`,
registered as `projectlist.AdminFeature` (distinct feature so the
`/project/*` cores stay untouched). Node oracle pins captured live from
the running Node (37 pins, `/tmp/p62_oracle.json` + log-verified BSONError
root cause). Surface: `GET /admin/user`/`/admin/project` (301 →
`/hub#/site.general.*`), `GET /admin/active-projects` (RT `/clients`
projection, Basic auth `WEB_API_USER`), `POST /admin/user/:userId/projects`
(`null` = all users; active + deletedProjects merge, Node
`_formatProjectInfo` field order, undefined-omitted / explicit-null
`lastUpdatedBy:null`, `_matchesFilters`, title `localeCompare`-style
ascending with `\uffff` null-name slot, lodash-orderBy "missing last"
for lastUpdated/deletedAt, 500 reproduction pins: bad `owner_ref` hex
(`new ObjectId` BSONError) + non-ObjectID `trashed` elements +
bad-sort OError), `POST trash|untrash` (`resolveProjectUserId` body-user
with owner fallback, 200 OK), `DELETE /admin/project/:id` (soft delete —
full P4 `deleteProjectExec` side-effect chain under the admin gate;
admin `deleterData` = Node admin options exactly: **no**
`deleterIpAddress`, **no** `deletedReason` — `deleteProjectExec` gained a
`reason` param, P4 caller still passes `"user"`, zip-import unchanged),
`POST undelete` (restore under the admin gate: same-`_id` re-insert,
owner from body, name = `generateUniqueName` ("p62-sacrifice (Restored)"
full-string pin), `deletedDocs` reset + docstore `deleteDoc` fires,
deprecated record delete, untrash), `DELETE purge`
(`expireDeletedProject` states: active → stale-record cleanup 200;
record-missing → 404; already-redacted → 200; otherwise docstore
`destroy` + history delete + (v1-history when `overleaf.history.id`
present) + chat destroy + audit-log purge + PII redaction),
`GET members` (`{owner, members}` admin wrapper shape over the P4
`invitedMemberRows` core), and the P4 collaborator cores re-mounted under
the admin gate (site-admin, no per-project ownership check — Node mounts
the **same** controllers under `ensureUserIsSiteAdmin` only):
invites list/create/revoke/resend, `PUT users/:uid`, `DELETE users/:uid`
(204). Gates split by Node behavior: non-parseReq controllers
(members/delete/undelete/trash/purge) → malformed `:id` = 500 HTML page;
parseReq controllers → malformed `:id` = 404 `Invalid Mongo ObjectId at
\"params.Project_id\"` (the P4 member-surface gate shape).

**Parity bugs found by the gate/audit and fixed:**
1. `deletedReason` was hard-coded `"user"` in the shared
   `deleteProjectExec`; Node admin delete omits it (admin options =
   `{deleterUser}` only). Parameterized `reason` (P4/zip unchanged,
   admin passes `""`).
2. Deleted-list rows read `deleterData` from the **project subdoc**;
   Node reads it from the `deletedProjects` record (and its projection
   selects exactly `deleterData.deletedAt/deleterId`). Moved the read to
   the record; non-Date `deletedAt` now 500s like Node's `toISOString`
   TypeError.
3. `lastUpdatedBy` explicit-`null` (61 stored docs) was dropped;
   Node emits `"lastUpdatedBy":null`. `lubNil` added; `name`/`nameNil`
   and `owner:null` distinctions match Node's JSON semantics.
4. The all-users list 500 (known bad fixture row "P52b NotMine",
   `owner_ref` = 22-char string) now reproduces: `isTrashed` semantics
   tightened (non-array `trashed`, non-ObjectID element, bad-owner hex
   all 500 exactly when Node throws), verified against the Node log's
   BSONError stack.
5. Gate harness: the sacrificial project id is re-minted per leg —
   `from=` redirect URLs (loc + body) and the create/delete rows are
   normalized; pin-sanity asserts the oracle's full 400-message strings
   via `JSON.parse` (escaped-quote `toContain` was byte-matching the
   wrong shape and flake-skip-masked itself).

Gate `web-go-p62-admin-flip.test.e2e.ts` (3 legs + oracle-anchor sanity):
legacy 301 + member 302 + anon 302 matrix, sacrifice create, members
before/after (184 B owner-only shape), admin-list 500 ×2 (pre/post
soft-delete), owner-scoped **120 KB full title-sorted listing byte-diff**,
bad-sort 500, trash/untrash 200s, invite 400 (267 B exact), put-user 400
(187 B exact), del-user 204, sharing-link 404 + 400 (261 B exact),
soft-delete 79 B `{deletedAt,deleterId}`, undelete 35 B name pin,
soft-delete #2, purge + purge-again 200s, member 302/403-CSRF/no-
token matrix, anon 302/403, badpid 500s, active-projects `[]` ×3 authz
rows. **All 4 tests green (no flaky).**
Regression sweep after P6.2: P6.1 4/4, P4.del 3/3, P4.col 3/3, P4.inv
3/3, `go build ./...` + `go vet` + full `go test ./go/...` green. The
sharing-link **create** success path (token AES-256-CTR/HKDF-SHA512,
`2026.3-v3` label, `OVERLEAF_INVITE_TOKEN_SECRET`) is implemented
(`atEncrypt`/`atDecrypt`) but is **not gate-pinned** (oracle pins only the
404/400 rows); flagged for a future invite-token e2e. Admin audit-log
entries (`sharing-link-created/updated`) are Node state side-effects not
visible to any gated contract — deferred with the pin set.

**P6.3a — admin-tools user surface, reads (DONE, GATE 4/4 GREEN, 2026-09-16):**
`go/services/web/features/adminusers/` (new feature, registered in
`cmd/web/main.go`) + `flips/web-p63a.conf`. Node oracle pins live-captured
(37 pins, `/tmp/p63a_node.json`). Surface: `POST /admin/users`
(admin user list) + `GET /admin/user/:userId/info`. The hub's live
`#/site.general.users.*` table backend (the P3.5 owner decision excluded
only the `/user/activate` page + the mutation surface — this is the read
half of the same module).

**Parity gotchas found by the oracle (each now pinned in the gate):**
1. `page.{size,no}` is a **Node no-op** — `_sortAndPaginate` never slices;
every pin returns the full filtered list (totalSize = filtered count).
2. `sort.by === 'name'` ignores `sort.order` **entirely** (pinned:
   `name_asc` and `name_desc` bodies byte-identical); comparator =
   `(lastName,firstName,email)` with missing → `\uffff`,
   case-insensitive (localeCompare sensitivity:'base' on this ASCII data),
   stable.
3. Non-name sorts = lodash `orderBy`: undefined values **first in desc /
   last in asc** (pinned sequences: no-lastActive users lead the
   lastActive-desc list in natural order; the two no-signUpDate users trail
   signUpDate-asc and lead signUpDate-desc), stable on ties; string keys
   lowercased; `bad by` / `bad order` → OError → 681 B 500 page.
4. **search short-circuit quirk**: Node's exclusion clause is
   `email -1 && first?.indexOf -1 && last?.indexOf -1`; a MISSING name
   makes that operand `undefined` (≠ -1) so the row CANNOT be excluded —
   pinned: the 28 no-lastName users pass ANY search (`f_search_none`
   = exactly those 28); a NULL name would TypeError → 500; email always
   required (`.email.toLowerCase()`).
5. `f_saml` (and other unavailable auth-method filters) are inert because
   the filter loop iterates `availableAuthMethods` = `['local']` only
   (EXTERNAL_AUTH unset) — pinned: `f_saml` returns all 50.
6. Row JSON = Node `JSON.stringify` presence semantics: missing
   `last_name`/`isAdmin`/`signUpDate`/`lastActive`/`lastLoggedIn`/`loginCount`
   **drop** the key (real data: 28 missing lastName, 1 missing isAdmin,
   2 missing signUpDate, 47 missing lastActive); `canManageTemplates`,
   `inactive`, `deleted`, `authMethods`, `allow*` always present;
   `suspended`/`deletedAt` conditional; dates ISO-ms UTC. Hand-built
   ordered emission (Go maps would alphabetise).
7. body-shape pins: non-matching content types (text/plain, no CT) leave
   `req.body` `{}` in Node → **200 full list** (not 4xx); JSON **array**
   body destructures to no filters → 200; malformed JSON → `400 {}`;
   urlencoded `filters[admin]=true` → qs nesting + string truthiness.
8. `info`: `activationLink` from the first live password token
   (use/password, data.user_id, expiresAt > now, no usedAt,
   peekCount < MAX_PEEKS=4) else `null` (pinned nulls); ghost + bad id
   both → `200 {…false}` (Node swallows the cast error).

Gate `web-go-p63a-flip.test.e2e.ts` (3 legs + oracle anchors, **4/4 green
FIRST RUN, no flaky**) over the full authz matrix (anon 403/'Forbidden'/
302, member 302/empty-CT html-vs-json, 403 no-token), 10 filter pins,
9 sort pins (incl. both 500s), 5 body-shape pins, 6 info pins, 15.6 KB
full-list byte-diff.
Regression sweep after P6.3a: P6.2 4/4, P3.6 (site-settings) 3/3, P6.1 4/4,
`go build ./...` + `go vet` + full `go test ./go/...` green.

**Next (P6.3b)**: the admin user mutation surface —
`POST /admin/user/create` (registerNewUser), `POST /admin/user/:id/update`,
`POST /admin/user/:id/delete` (soft delete), `POST /admin/user/:id/restore`,
`DELETE /admin/user/:id` (purge), `POST /admin/user/:id/send-activation`
(email side-channel; UserDeleter + OwnershipTransfer + activation-token
stack).

ollitex-hub (19.9k — the workspace/admin surfaces; mostly proxies of already
-flipped P3/P4 endpoints + its own HubController), admin-tools **project
surface DONE (P6.2)** (remaining: site/user surfaces), llm (14.4k — external-provider client + rate limits + BYO-key crypto),
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

1. ~~`go/services/web/contract/routes.csv`~~ **✔** — `go/services/web/contract/routes.csv`
   (159 routes: method, path, router chain, feature, source line; module
   routers included). Auth-requirement + queue/downstream attribution is
   derived per flip unit (the 4 P0 units are attributed in the gate spec).
2. `go/services/web/core/` — **✔** http kernel (two profiles, `ENABLED_SERVICES`),
   config (env parity incl. `OVERLEAF_REDIS_*`/`OVERLEAF_MONGO_URL`), session
   store (Redis interop), csrf (Node-exact), static, errors (`pbhttp` reuse),
   rolling/lazy session semantics. Validation/rates/views/i18n land with the
   first feature that needs them (P1/P2/P3 respectively) — not built blind.
   Pinned unit tests: `go/services/web/core/core_test.go`.
3. Shadow-port runner — **✔** `cmd/web` (+ `server-ce/runit/web-go-overleaf/run`,
   sv-managed, **off by default**; `web-go-flip` runit service gates the nginx
   flip on `FLIP_GO_WEB_P0=1`).
4. Session interop A/B harness — **✔** `tests/e2e/specs/parity/web-go-p0-flip.test.e2e.ts`
   (leg3a/3b: both cookie directions + both csrf-token directions, cross-checked
   against the shared redis doc with Node's own algorithm as third party).
5. nginx flip table + SOP — **✔** `server-ce/nginx/flips/web-p0.conf` (exact
   `location =` blocks; `^~`/exact semantics documented), apply/strip =
   `web-go-flip` service (self-healing; node-based vhost insertion; nginx -t
   gate; reload-race settled by gate-side polling). Rollback: strip the
   include + reload = 100% Node, no data migration.
   **Reload race note (pinned in gate): `nginx -s reload` is async — probes
   must poll; baked into the spec.**
6. **Go green, zero traffic, e2e smoke unchanged → DONE**: gate 5/5 green
   (24.5s, no flakes), 16/16 regression sample (smoke/auth/docstore-filestore/
   chat-notifications/linked-url incl. LIVE leg), Go tree 13 packages `ok`.

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
