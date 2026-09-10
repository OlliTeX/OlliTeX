# OlliTeX user & admin wiki — plan (2026-09-16)

Status: **plan approved-for-execution (owner TODO-0d72dacf).** This document
is the plan; the pages it describes are built in `docs/wiki/` in the waves
under §6.

## 1. Purpose & hard constraints

One canonical, self-contained wiki that a new **user** and a new **admin**
can follow end-to-end on a fresh OlliTeX instance.

**Non-negotiable (owner requirement):** the wiki and every asset inside it
contain **no real user data, no credentials, no API keys, no secrets, and no
personal information.** Only fixture data created for the wiki itself on the
disposable E2E stack.

Other constraints:

- **Screenshots are always auto-generated** — no manual captures. Re-running
  the pipeline (§5) is the only way an image in `docs/wiki/assets/` changes.
- Pages are plain **markdown + nested folders** (works in any viewer, no
  build step; an optional static-site render is a later, separate wave §7).
- Provenance: every page ends with a `Verified against:` line (git tag/short
  SHA + date) so docs never silently drift from the product.

## 2. Information architecture

```
docs/wiki/
├── README.md                     # wiki home: readers map (user vs admin), links
├── assets/                       # screenshots only, auto-generated (naming per §5.5)
│   ├── users/…
│   └── admins/…
├── users/
│   ├── 01-getting-started.md     # first login, hub tour, first project
│   ├── 02-projects.md            # create (blank / LaTeX template / Typst),
│   │                             #   tags, search, trash/restore, download
│   ├── 03-editor-latex.md        # editing, compile menu + images, PDF pane,
│   │                             #   comments & track changes, history
│   ├── 04-editor-typst.md        # Typst projects: create, compile, templates,
│   │                             #   syntax/formatting
│   ├── 05-ai-features.md         # chat (ask AI), inline completion, grammar
│   │                             #   (LanguageTool / LLM), BYO provider setup
│   ├── 06-references.md          # bibliography tools: bibtex, Zotero, Mendeley,
│   │                             #   reference search & pick
│   ├── 07-file-tools.md          # import (docx/md/URL), export, linked files
│   ├── 08-sync-integrations.md   # GitHub sync, WebDAV/Nextcloud, Dropbox
│   ├── 09-settings.md            # My settings hub leaves: appearance,
│   │                             #   notifications, LLM, WebDAV, sessions
│   └── 10-accounts.md            # register, login (incl SSO), password
│                                 #   reset, secondary email
├── admins/
│   ├── 01-admin-overview.md      # who is admin, the /hub admin rail
│   ├── 02-users.md               # list/suspend/delete, bulk select,
│   │                             #   roles, audit log
│   ├── 03-projects.md            # all projects, ownership, trash,
│   │                             #   active project sessions
│   ├── 04-site-settings.md       # site name/URL, email/SMTP, registrations,
│   │                             #   storage, sandbox compiles, integrations
│   ├── 05-llm-rate-limiter.md    # the "Rate Limiter" leaf: instance LLM on/off,
│   │                             #   BYO flag, user/admin rates, token budgets
│   ├── 06-templates.md           # gallery management, publishing, categories
│   ├── 07-instance-stats.md      # reading the stats page, retention
│   ├── 08-sso-saml-oidc.md       # identity providers, group mapping
│   └── 09-operations.md          # system messages, sessions, diagnostics
└── installation/
    ├── 01-docker.md              # server-ce make all + compose example
    ├── 02-configuration.md       # env.sh + tools/toolkit seed map
    ├── 03-upgrade.md             # image swap + data notes
    └── 04-security-sandbox.md    # sandbox compiles, trusted-user warning
```

Numbering is stable (new pages get the next number). Every page follows the
template in §4.

## 3. What a page looks like (template)

```markdown
# [Feature] (users)

Goal: one sentence — what the reader can do after this page.

## Steps
1. Open **Hub → …** (`/hub#/projects.all`).
2. Click **New project** → **Blank project**.
   ![blank project modal](../assets/users/02-projects-new.png)
3. …

Notes / edge cases
- …

Verified against: OlliTeX @ `<short-sha>` (2026-09-16)
```

- Always link the **canonical URL** (the hash-addressable hub leaf where it
  exists) so readers can jump straight to the surface.
- One screenshot per step, max 2 per step; images referenced by **relative**
  paths only.

## 4. Screenshot pipeline (auto-generated, data-safe)

### 4.1 Source environment
- The disposable E2E stack (`tests/e2e/scripts/stack-up.sh`,
  `docker-compose.test.yml`) — throwaway mongo/redis; no production data.
- Fixture accounts only (`e2e-user` / `e2e-admin`, `Ol-Fixture-*` passwords)
  from `tests/e2e/fixtures/credentials.ts`. No real identities anywhere.

### 4.2 Generator (one script, new)
`tests/e2e/scripts/wiki-screenshots.mjs` — a deterministic Playwright driver:

1. Reset the stack (fresh mongo) and seed **wiki demo data**: 2–3 projects with
   lorem-ipsum content, one Typst project from the example template, one
   BYO provider row with a **dummy** key `sk-ollitex-dummy-do-not-use`
   (unreachable endpoint), one tag, one comment.
2. Walk a fixed **shot list** (each entry: `name`, `context (user|admin)`,
   `route or click-path`, `expected visible text`): e.g.
   `users/01-getting-started-hub.png` = admin rail at `/hub`;
   `users/05-ai-features-byo.png` = the BYO leaf with the dummy provider row.
3. For each shot: fixed viewport **1440×900**, locale `en`,
   `prefers-reduced-motion: reduce`, wait `networkidle` + the expected-text
   guard, full-viewport screenshot to
   `docs/wiki/assets/<context>/<nn-page>-<slab>.png`.
4. Cleanup: delete every wiki demo project/template (admin purge), leaving an
   empty stack.

The expected-text guard makes a shot **fail the run** rather than capture a
broken page — no "screenshot of an error state" can ship silently.

### 4.3 Redaction & data-safety gates (run after every shot)
A final `verify` step inside the same script:

- **Pattern scan** over all PNG metadata *and* every `docs/wiki/**/*.md` +
  `assets/` filename: `sk-[A-Za-z0-9]{8,}`, `AKIA[0-9A-Z]{16}`, `Bearer `,
  `xox[baprs]-`, the two real admin credential strings from
  `/data_1/image_mining/testuser.txt` (read at runtime, never in docs),
  the real domain `rotermund.at`, and any `@`-address outside
  `{e2e-user,e2e-admin,e2e-tpladmin}@e2e.test` → **hard fail** with the
  offending file + pattern.
- **Secret inventory diff**: the only provider key permitted to appear in
  prose is the literal dummy `sk-ollitex-dummy-do-not-use`.
- **Network hygiene**: during the run, Playwright `route('**')` denies every
  request not matching the E2E origin or the unreachable
  `http://ollitex-wiki-demo.invalid` BYO endpoint — nothing can phone home.
- Output: `docs/wiki/AUDIT.md` (auto-overwritten per run): shot list, SHAs of
  images, verification timestamp, stack version. This file is the data-safety
  receipt.

### 4.4 Regeneration contract
- Any product change that alters a shot's expected text → the run fails on
  the old shot → update the shot list → re-run (images and AUDIT.md both
  change in the same commit).
- `make wiki-shots` (root Makefile target) = stack-up → generator → stack-down
  + the §4.3 gate. No manual screenshots, ever.

### 4.5 Naming
`<nn>-<page-slug>-<slab>.png` where `nn`/slug match the page file
(`05-ai-features-byo.png` lives with `users/05-ai-features.md`). A shot name
is unique across the wiki; the AUDIT.md maps name → source step.

## 5. Writing rules

- Short imperative steps; hub leaf links first, click-path second.
- One feature per page; cross-links, no copy-paste between pages.
- English only.
- Every admin page carries the AGPL note: "This describes OlliTeX, a fork of
  Overleaf CE (AGPL v3)."
- Admin secrets (SMTP host/token values) are **never** shown — describe the
  *field* and its *purpose*, use `<your-smtp-host>` style placeholders.

## 6. Execution waves (estimates, owner-orderable)

| # | Scope | Output | ~Effort |
|---|-------|--------|---------|
| W1 | Tree + `users/01–03` + hub tour | 5 pages, 8–10 shots | 0.5 d |
| W2 | `users/04–10` (Typst, AI, references, sync, settings, accounts) | 7 pages, ~15 shots | 1 d |
| W3 | `admins/01–09` (incl. Rate Limiter) | 9 pages, ~18 shots | 1 d |
| W4 | `installation/01–04` | 4 pages, ~4 shots | 0.5 d |
| W5 | Pipeline hardening: `make wiki-shots`, AUDIT.md, secret-scan gate, CI hook (run generator in dry-run mode on doc PRs) | gates green | 0.5 d |
| W6 | Polish pass: consistent terminology (audit vs glossary), link check, `Verified against:` stamps | final | 0.5 d |

Acceptance per wave: pages render standalone (relative links only), every
`![](` resolves, AUDIT.md covers 100% of images, pattern scan clean, and a
fresh-reader test (one page per audience, cold start, task completed).

## 7. Optional later (not in scope now)

- Static-site render (MkDocs Material or Docusaurus) of `docs/wiki/` for a
  pretty public site — pure presentation, zero content change.
- German translations of the user pages (Norderney/Bremen readers).
- Short "5-minute tour" GIFs from the same pipeline (Playwright video).

## 8. Open items for the owner

- Confirm the audience split above (any extra reader class, e.g.
  institution admins with SSO only?).
- Confirm the dummy-provider prose (`sk-ollitex-dummy-do-not-use`) is the
  acceptable stand-in for real keys in docs.
- Wave order / priority — the table is the suggested order.
