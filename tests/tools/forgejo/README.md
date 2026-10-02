# Forgejo v15 — the "test representative" git-provider

Local test instance for the **git-provider sync integration**
(`go/services/web/features/ghsync` + `go/services/githubinterface` bridge,
UI module `frontend/modules/github-sync`).

**Why Forgejo:** we cannot host a live test instance of every git provider
(GitHub, GitLab, Gitea, …). Forgejo is the representative for the
*non-GitHub* provider family (Gitea-compatible API + git-over-http), and its
behavior pins the exact contract the integration must honor:

| Capability (git-provider sync) | GitHub | Forgejo (this instance) |
| --- | --- | --- |
| Link / unlink PAT | ✅ | ✅ — plain REST + git protocol |
| Import repo → project (clone) | ✅ | ✅ |
| Export project → repo (push) | ✅ | ✅ |
| Merge overview (commits since last sync) | ✅ | ✅ — commits listing API |
| **Merge remote-only advance** | ✅ | ✅ — contents-style REST tree fetch (GitHub-compatible `/api/v1` `contents`; the git-data API is NOT required — see A5 note) |

Provider fact (A5 pin): Forgejo (like Gitea) serves a GitHub-compatible
`/api/v1` but **lacks the git-data endpoints** (`git/ref`, `git/blobs`,
`git/trees`, `POST /merges` — verified 2026-08 on gitea.com and
v15.next.forgejo.org, re-pinned here by test A5). The Go merge engine does
NOT use the git-data API: it fetches the remote tree through the provider's
contents-style REST API in-process (`go/services/web/features/ghsync` →
go/services/githubinterface), so a **remote-only** advance merges cleanly
(`200 {"status":"merged"}`); local+remote divergence records
`{"status":"conflict"}` for the merge UI. (The module README's older
"501 on non-GitHub" claim predates this engine and is stale — corrected
2026-10-02.)

**SQLite by default** — *"If no database is configured, it will default to
using SQLite."* (forgejo.org/docs/v15.0/admin/installation/docker/). Single
service, no DB container. Good enough for testing.

## Run

```sh
cd overleaf
docker compose -f tests/tools/forgejo/docker-compose.yml up -d     # http://127.0.0.1:3000
python3 tests/tools/forgejo/run_forgejo_test.py                    # provider-contract checks (Part A)
docker compose -f tests/tools/forgejo/docker-compose.yml down -v   # reset to blank slate
```

- host port **3000** is Forgejo's canonical port — verify it is free on a
  shared box before `up` (same hygiene as the `sso-test` note about :4000).
- first boot is blank (no admin yet): the harness performs the install
  (`POST /` install form — v15 has no `POST /first-install`), then mints a PAT
  and a seed repo — no UI clicking.
- all state is in the named volume `forgejo-data`; `down -v` wipes it
  (a fresh run then re-bootstraps; the cached `.pat` becomes invalid and a
  new token is minted + re-cached).

## Harness: `run_forgejo_test.py` (stdlib-only python3, like the other tools)

**Part A — provider contract (always runs, no Overleaf needed)**

| # | Check | Pins |
| --- | --- | --- |
| A1 | bootstrap if blank (`POST /` install form → fixture admin, install-lock implicit) | repeatable, no UI |
| A2 | `GET /api/v1/version` → 15.x | representative is really Forgejo v15 |
| A3 | PAT minted via the v15 web token flow (flash-cookie; cached in git-ignored `.pat`) + seed repo `oltest-repo` | stable fixture identity |
| A4 | git wire round-trip: `git clone` → commit → `git push` over http+PAT | the exact surface Overleaf import (clone) / export (push) use |
| A5 | `GET /repos/…/git/ref/heads/main` → **404** | git-data API ABSENT on this family (provider fact — the Go merge engine does not need it; contents-style REST is used instead) |
| A6 | PR merge process: feature branch → `POST /pulls` → `POST /pulls/{i}/merge` → `merged=true` (+ merged file on main) | Forgejo-side merge pipeline actually works (test target of the integration's provider side) |

**Part B — Overleaf integration (skips unless `OLI_BASE` + `OLI_EMAIL` + `OLI_PASS` are set)**

| # | Call | Expect |
| --- | --- | --- |
| B1 | `POST /user/git-pat/link` `{provider:"forgejo", url, username, pat}` | 2xx |
| B2 | `POST /user/git-servers/test` | 2xx (PAT accepted) |
| B3 | `GET /user/github-sync/repos?provider=forgejo&serverUrl=…&username=…` | seed repo listed (resolution keys on provider+server+username — bare call resolves the GitHub default and answers `null`) |
| B4 | `POST /project/new/github-sync` (import) | `{projectId}` |
| B5 | push a remote commit → `GET /project/:id/github-sync/merge/overview` | commit visible since `lastSyncCommit` |
| B6 | `POST /project/:id/github-sync/merge` | **`200 {"status":"merged"}`** (remote-only fast path via contents REST — provider-agnostic) + B6b second call → `"clean"` idempotent |
| B7 | `DELETE /project/:id/github-sync` | 2xx (cleanup) |
| B8 | (opt, `OLI_CLEANUP=1`) `DELETE /Project/:id` (capital P — Node route quirk) | 2xx (project cleanup) |

```sh
# Part B against the disposable e2e Overleaf stack (ol-e2e; fixture user from
# tests/e2e/fixtures/credentials.ts — test-only dummies, owner rule 2026-09-05):
OLI_BASE=http://127.0.0.1:7420 \
OLI_EMAIL=e2e-user@e2e.test OLI_PASS='Ol-Fixture-3m2Q' \
  python3 tests/tools/forgejo/run_forgejo_test.py

# or any other Overleaf instance / account:
OLI_BASE=http://127.0.0.1:4000 OLI_EMAIL=… OLI_PASS=… python3 tests/tools/forgejo/run_forgejo_test.py
```

## Env

| Var | Default | Meaning |
| --- | --- | --- |
| `FORGEJO_BASE` | `http://127.0.0.1:3000` | Forgejo instance under test |
| `FORGEJO_ADMIN_USER` | `oltest` | fixture admin (bootstrap identity, test-only dummy) |
| `FORGEJO_ADMIN_PASS` | `Ol-Fixture-8mK2` | fixture admin password (test-only dummy) |
| `OLI_BASE` | *(empty → Part B skipped)* | Overleaf instance (git-provider sync API) |
| `OLI_EMAIL` / `OLI_PASS` | *(empty → Part B skipped)* | Overleaf account for Part B |
| `SKIP_PART_B` | `0` | `1` forces Part B skip (e.g. bridge service not deployed) |

## Verified quirks of this build (forgejo 15.0.9 + gitea 1.22 core, 2026-10-02)

Pinned empirically against the live instance so the harness (and anyone
wiring the integration) stays honest:

- **PAT plaintext surface**: v15 has no self-service token API
  (`POST /api/v1/user/token` → 404) and admin-created tokens are masked
  (sha + last-8 only). The web flow is the only plaintext path:
  web login → `POST /user/settings/applications/tokens/new`
  (`name`, `resource=all`, `scope=<comma list>`) → 303 response sets a
  `flash` cookie `info=<40-hex token>&success=…` (server side:
  `ctx.Flash.Info(t.Token)` in the v15 `AccessTokenCreatePost`). Shown once.
  → the harness caches it in the git-ignored `.pat` so re-runs are stable.
- **Auth headers accepted**: `Authorization: token <PAT>` and basic
  `user:token` (git-over-http). **Rejected**: `Token: <PAT>` and
  `PRIVATE-TOKEN: <PAT>` ("token is required") — those are GitLab-family
  conventions (gitlab-only in `githubinterface/rest.go`).
- **Scopes are fine-grained** (`write:<domain>` per domain — repository,
  user, issue, misc, notification, package, organization, …). 403s pin the
  minimums: `write:repository` (git + repo APIs), `write:user`
  (`POST /user/repos`), `write:issue` (PR create/merge).
- **API routes reject web-session cookies** ("token is required"); the
  admin pair works API-basic during bootstrap only.
- **git-data surface**: `/repos/{o}/{r}/git/ref/heads/<branch>` → **404**
  (absent — the A5 pin), while `/git/blobs/<sha>` exists → 400 on a bogus
  sha. The Go merge engine does not use git-data (contents-style REST), so
  the absence does NOT block remote-only merges (B6) — it only rules out the
  Node-era git-data merge path.
- **Merged PR representation**: `GET /repos/…/pulls/{i}` on a merged PR
  returns `state: "closed"` + `merged: true` + `merged_at` — NOT the
  GitHub-style `state: "merged"` (A6 accepts the `merged` flag).
- install quirks: form action is `/` (not `/first-install`), sqlite needs an
  explicit `db_path` (`/data/gitea/gitea.db`), log path must sit under the
  git-owned tree (`/data/gitea/log` — a fresh volume's `data/` itself is
  root:root), no `_csrf` field, registration closed on bootstrap, and the web
  process restarts after a successful install (harness polls admin login
  through that window).

## Fixture identities

`oltest` / `Ol-Fixture-8mK2` are **test-only dummies** — same policy as
`tests/e2e/fixtures/credentials.ts` (controlled identities, never random, no
real credential in this tree). The PAT is minted at runtime and cached in
`.pat` (git-ignored, mode 600) for stable re-runs and the e2e suite;
it only works against this disposable container's sqlite, never against a
real forgejo.

## Related

- module contract + endpoint list: `frontend/modules/github-sync/README.md`
- merge engine (contents-style REST, in-process) + state/merge handlers:
  `go/services/web/features/ghsync/controllers_project.go` +
  `go/services/web/features/ghsync/bridge.go` (bridge →
  `go/services/githubinterface`)
- git providers in the e2e suite: `tests/e2e/credentials/config.json.example`
  (github / codeberg / gitlab entries, owner rule 2026-09-05)
