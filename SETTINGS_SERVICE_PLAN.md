# SETTINGS_SERVICE_PLAN — Go settings microservice on SQLite

Status: **DRAFT (owner direction, 2026-09-14)**. Supersedes ad-hoc env plumbing for
runtime config. Ordering vs WEB_GO_PLAN: decision pending (see "Open questions").

## Why
- ~10 Go services + the Node web each parse env for endpoints, toggles, limits and
  secrets; three sources of truth (image env, `server-ce/settings.js`,
  `tests/e2e/stack/settings.js`) already drift in subtle ways.
- WEB_GO parity work keeps hitting env-fragility (redis host indirection,
  siteUrl origin, secret derivation, per-module toggles). One store of truth
  removes a whole class of parity bugs.
- A typed settings service simplifies the Go conversion: consumers get typed
  getters + validation instead of `os.Getenv` + scattered parsing.
- The admin surfaces (hub admin/site + `manage-site`) already present a
  site-settings UI; the service gives it a durable backend.

## Shape (v1)
`cmd/settings` — single Go binary, stdlib net/http + SQLite (pure-Go driver,
e.g. modernc.org/sqlite; no CGO).

- Storage: one SQLite file (`SETTINGS_DB_PATH`, bootstrap env exception).
  Tables: `settings(key PK, value TEXT, updated_at, updated_by, note)`,
  `settings_audit(id, key, old, new, at, who)`.
- API (HTTP, JSON; authenticated — see Open Q3):
  - `GET  /v1/settings/resolve?consumer=<name>` → effective values
    (stored > env > built-in default), with source annotation per key.
  - `GET  /v1/settings` (admin) — all keys + sources + audit tail.
  - `POST /v1/settings` (admin) — upsert validated key(s), audit, prev value kept.
  - `POST /v1/settings/rollback` (admin) — restore previous N values.
  - `GET  /health_check` (no auth).
- Precedence: **stored value > env default > built-in default** — mirrors the
  Node SiteSettings/EnvHydrator contract ("stored settings win over env") that
  WEB_GO P3 SiteSettings work will port from Node.
- Consumers (all Go services): replace `os.Getenv` for migrated keys with
  `client.Resolve(consumer)` at boot + refresh on change (poll with backoff or
  SSE push — decide in S0). **Liveness rule: a settings-service outage after
  boot must not crash a consumer** — last-known-good values continue (same
  semantics as Node hydrating env at boot).

## What stays in env (bootstrap allowlist, explicit + documented)
- `SETTINGS_DB_PATH`, settings service listen addr, bootstrap service-token.
- Service URLs needed for the very first boot of the graph (e.g. which
  endpoints exist) — or seeded into the store on first boot.
- Secrets that must exist before the service can start: decision per-secret
  (see Open Q2).

## Key migration waves
- S0: skeleton + SQLite + auth + health + 5 probe keys; gate = 2 consumers
  (web shadow + one existing Go service) reading 1 key through it, values
  byte-identical to today's env reads.
- S1: web service env keys (endpoints, siteUrl, toggles) — full WEB_GO gate
  matrix must stay green (P0/P1/P2/P3.x all re-run).
- S2: remaining 8–9 services (chat, docstore, filestore, datamanipulator,
  github, linked-url-proxy, notifications, webdav, dropbox).
- S3: admin read/write from existing hub admin/site surfaces (durable backend
  for the settings UI; import current runtime values first).
- S4: fold in `server-ce/settings.js`-only keys (read-only or migrated);
  retire duplicated env plumbing; document the bootstrap allowlist.

## Contracts & safety
- Typed schema per key (Go port of the zod/Settings equivalents) — unknown or
  mistyped values rejected at `POST` (not at consumer read time).
- Audit + rollback are first-class (the parity story: any admin change is
  reversible without a restart).
- Backup: SQLite file is the single source → backup strategy mandatory before
  S3 (Open Q1).
- Secrets at rest: file perms + optional app-layer encryption decision
  (Open Q2).

## Risks
- Bootstrap chicken-egg → explicit allowlist + first-boot seed.
- Dual-writer drift → only the service writes; env is a read-only input;
  `resolve` annotates the source so drift is visible.
- Perf → resolve once at boot; pushes are best-effort.
- Parity risk → every wave gates against the existing Web parity battery.

## Open questions (owner)
1. SQLite location + backup policy (where does the file live on live + e2e).
2. Which secrets are allowed in the store (COOKIE/SESSION secrets,
   LLM_KEY_SECRET, CRYPTO_RANDOM lineage …) vs env-forever.
3. Auth model: shared service token in env (bootstrap exception) vs mTLS.
4. Hot-reload semantics per consumer (poll interval / event push).
5. Ordering: start this after WEB_GO P3 completes, or in parallel with P4+.
