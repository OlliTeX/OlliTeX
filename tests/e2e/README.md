# Overleaf CE — e2e suite (Playwright, forgejo-style)

Repo-native end-to-end suite with a **disposable, self-contained test
stack** — the pattern forgejo uses for its e2e (fixed fixtures, idempotent
seeding, isolated infra, CI-ready). **It never touches the production box.**

```
tests/e2e/
├── docker-compose.test.yml      # disposable stack (project `ol-e2e`, port 7420)
├── playwright.config.ts
├── global-setup.ts              # fixture seeding (idempotent, contract-strict)
├── fixtures/credentials.ts      # the ONLY identities: e2e-admin / e2e-user @e2e.test
├── helpers/ (auth, ctx, host)
├── specs/*.test.e2e.ts          # 8 journeys (P0 set)
└── scripts/ stack-up|down|reset # stack lifecycle (idempotent)
```

## Fixture contract
| identity | password | role |
|---|---|---|
| `e2e-admin@e2e.test` | `E2e-Admin!1234` | admin (promoted via mongo `permissions:["admin"]` after registration) |
| `e2e-user@e2e.test` | `E2e-User!1234` | regular |

Seeding uses the **real app flows** (`POST /register` → one-time token →
`POST /user/password/set` — email is disabled in the test stack, the
account is not rolled back) and THROWS on any contract violation instead
of degrading. A seed manifest is written to `test-results/seed-manifest.json`.

## Stack notes (2026-09, this host)
- **mongo: 6.0** — fresh mongo 8.x builds are kernel-incompatible on this
  host's 7.0.0 kernel (SERVER-121912 gate on 8.3-late/8.4; hard segfaults
  under load on 8.0/8.2). 6.0 was probed to survive the full migration
  load. The image's "Mongo ≥ 8.0" boot check is overridden via
  `OL_MIN_MONGO_VERSION=6.0` / `OL_MIN_MONGO_FCV=6.0` **test-stack only**
  (production defaults unchanged; `build-images-scripts/check-mongodb.mjs`).
- **Registration domain gate**: CE treats an empty allowed-domain list as
  "block all" (`[] && ![].some(...)`), so the stack sets
  `OVERLEAF_ALLOWED_REGISTRATION_EMAIL_DOMAINS=e2e.test`.
- **Registration `analyticsId` bug fixed** (2026-09-05): the 6.2-era
  `registration-page` module dropped `analyticsId`, which CE 6.3.0's
  `UserCreator` requires → every `/register` 500'd. Fixed in
  `modules/registration-page/.../UserRegistrationHandler.mjs`
  (mirrors the app-side handler).
- Compile goes through the host docker socket (same path as production);
  git-bridge is live; all ext module gates are ON; plain HTTP on
  `127.0.0.1:7420`.

## Running
```bash
# one-time
npm install --no-audit --no-fund

# local (this host)
bash scripts/stack-up.sh          # idempotent; boots the disposable stack
npx playwright test               # seeds fixtures, runs the suite
bash scripts/stack-down.sh        # stop (data kept for quick re-runs)
bash scripts/stack-reset.sh       # full wipe (volumes + env) — clean slate

# CI
.github/workflows/e2e.yml — builds the image, same flow, artifacts out.
```

## Scope (P0 set — 8 journeys)
| spec | what it pins |
|---|---|
| `smoke` | login → new project → editor → **compile (host docker) → PDF** |
| `byo-llm` | provider save/list **key-not-echoed** / graceful check / delete |
| `keybindings` | preset row = Overleaf/Vim/Emacs (R11) · 69-row registry · rebind persists · **Reset-now KEEPS modal** |
| `admin-site` | golden /admin/site tabs (no LLM tab, R9-9) · pandoc toggle gates Word import · admin LLM card (R11-6) |
| `zotero` | graceful with no credential (no 5xx) · `/library` renders |
| `sync-graceful` | webdav + dropbox status **never 5xx** (R11 regression class) · settings render |
| `notifications` | prefs page renders · mute-all round-trips |
| `grammar` | stored grammar mode round-trips · mysettings section |

Out of scope by design: real LLM/SaaS calls (webdav/dropbox/zotero go the
graceful-degradation path only), template admin, SSO, i18n matrix.
Add follow-on specs to `specs/`; the stack needs no changes.

## Lessons pinned by this suite (2026-09-05, all verified live)
- **New-project flow (6.3)**: trigger button → dropdown item only sets the
  active modal → `#blank-project-modal` form → **Create is DISABLED until the
  name input has a value** → Create POSTs `/project/new`. Substring traps:
  `has-text("Create")` also matches the "Create a new project" toggle.
- **Compile contract (6.3)**: `POST /project/:id/compile` is **synchronous**
  and returns the terminal state (`{status, outputFiles}`); there is no
  GET-status endpoint (404). CSRF token required.
- **PDF preview panel**: `.ide-redesign-pdf-container` (IDE redesign).
- **Rate limits**: `overleaf-login` = 20/min/IP + 200/min /8 subnet,
  `per-email` 10/120s (settings `rateLimit.*`, honored after container
  restart). The disposable stack stores generous values in the
  `sharelatex.settings` doc (`_id: "site"`).
- **Admin elevation**: `users.isAdmin = true` (the `permissions` array alone
  does NOT grant /admin).
- **BYO-LLM routes**: delete is `POST /user/llm-providers/:id/delete`;
  loopback base URLs are refused at save time by the SSRF gate (by design).
- **Pandoc section** persists to the `site_settings` collection (id `global`,
  key `pandoc`) — a different store from `settings` (id `site`); the flag
  takes effect on the NEW-PROJECT Word import after restart (env hydration).
- **Two UIs for the same menu** (welcome inline dropdown vs list modal) with
  identical item markup — always scope clicks to the open menu/modal.

## External services (credentials live on YOUR machine only)

Real credentials for external services (git hosts, WebDAV, Dropbox, Zotero, LLM
providers) live in **`credentials/config.json`** — local only, git-ignored.

```sh
cp credentials/config.json.example credentials/config.json   # then fill in + enable
```

- Template + policy + "ephemeral-token" handling: **`credentials/README.md`**
- One-time-login services (OAuth tokens valid "for some hours"): capture once with
  `node scripts/auth-capture.mjs --service <name> --url <login-url> ...`
  → cached in `credentials/.auth/` (0600) with expiry; `getServiceToken()`
  rejects stale caches with the exact re-capture command.
- `specs/external-probe.test.e2e.ts` live-checks only the services you enabled;
  skips cleanly when `config.json` is absent.
