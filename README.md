<h1 align="center">
  <br>
  <img src="tools/logo/logo.svg" alt="OlliTeX" width="190">
  <br><br>
  <img src="tools/logo/logo-horizontal.png" alt="OlliTeX — real-time collaborative authoring" width="340">
</h1>

<p align="center">
  <a href="#why-ollitex">Why OlliTeX?</a> •
  <a href="#features">Features</a> •
  <a href="#getting-started">Getting started</a> •
  <a href="#contributing">Contributing</a> •
  <a href="#license">License</a>
</p>

<img src="doc/screenshot.png" alt="A project being edited in OlliTeX" width="100%">
<p align="center">
  Figure 1: A project being edited in OlliTeX.
</p>

## OlliTeX

**OlliTeX is a free, self-hostable, real-time collaborative authoring platform
for LaTeX — and, since 2026, for Typst too.**

It is a fork of [Overleaf Community Edition](https://github.com/overleaf/overleaf)
(open source, GNU AGPL v3), extended and maintained as a complete product.
Overleaf runs a hosted commercial service at
[www.overleaf.com](https://www.overleaf.com); OlliTeX contains **no**
subscription, billing, or paid-plan components, and no advertising of premium
services — it is a fork, full stop.

## Why OlliTeX?

The name is a small piece of etymological wordplay that sits exactly on the
two meanings of *latex*.

**Olli** — from the **Olmec** ("Olmeco") civilization of Mesoamerica, the
"rubber culture": the people who, around 1500 BCE, first tapped the
*Hevea* rubber tree, vulcanized the sap by mixing it with the juice of
*Ipomoea alba*, and made the legendary bouncing rubber ball of the
Mesoamerican ballgame. In Nahuatl the word for rubber is **ātlātl** —
literally *"water of the water"* (ātl = water), because the milky white
sap that oozes from the cut bark looks like water. The Olmecs were, in
short, the first *latex* engineers.

**-TeX** — from TeX, Donald Knuth's typesetting system, and — by extension —
LaTeX (*La* + *TeX*), Leslie Lamport's macro layer on top of it. This is the
**second** meaning of "latex": the typesetting macro package, where a few
typed symbols render as a full equation.

So **OlliTeX = the Olmecs' rubber, meets TeX's precise ink.** A document
system that behaves like the Olmec ball itself — *resilient, stretchy, and
capable of springing back after a hard hit* — while delivering the accuracy
only a real typesetting engine can: **rubber typesetting, with a
Mesoamerican accent.**

> And, as a practical wink to every student who has ever watched a 40-page
> PDF compile: the OlliTeX compile queue does not bounce — it *flows*, like
> sap, like water, like ātlātl.

## Features

Everything in Overleaf CE (real-time collaborative editing, track changes and
comments, sandboxed compiles with TeX Live image selection, template gallery,
import/export, Git & GitHub sync, SAML/LDAP/OIDC authentication, advanced
administrator tools) **plus** the OlliTeX-specific stack:

- **Typst as a first-class format** — create, edit, and compile Typst
  documents alongside LaTeX: a dedicated `clsi_typst` compile service,
  Typst project templates (blank, article with bibliography, worked example),
  syntax highlighting and formatting in the source editor.
- **The integrated control hub at [`/hub`](#)** — one pane for everything:
  the user workspace (projects, tags, templates, library, sessions, settings)
  and the full admin surface (users, projects, active sessions, instance
  stats, site settings, LLM administration, templates) in a single
  Mantine-styled console with hash-addressable leaves.
- **Renovated editor** — a modern Mantine design-system layer
  (`/editor/:id`, dual-run identical to the classic `/Project/:id` URL),
  command palette, rail/toolbar redesign, theme tokens.
- **AI features, admin-gated and BYO** — LLM-powered chat, inline
  completion, and compliance review; bring-your-own provider (OpenAI-compatible
  endpoints, per-user) with per-feature rate limiting (instance rate, daily
  token budgets) on a single hub page; grammar checking via LanguageTool
  and/or LLM; an optional self-hosted LanguageTool service.
- **Extended collaboration tools** — Zotero integration, Mendeley, reference
  search & pick, equation editor, symbol palette, diagram canvas,
  document import (`.docx`, `.md`) and export (`.docx`, `.md`, `.html`),
  WebDAV/Nextcloud sync, Dropbox, sign-up page.
- **Operations toolkit** — environment-driven configuration
  (`server-ce/config/env.sh` + `tools/toolkit` seed), a central
  [Makefile](Makefile) entry point, an AGPL-compliant rebrand with provenance
  kept visible (see the local `BRANDING.md` handoff notes).

> [!CAUTION]
> Community Edition is intended for use in environments where **all** users
> are trusted. It is **not** appropriate for scenarios where isolation of
> users is required, since Sandboxed Compiles is not always active. When not
> using Sandboxed Compiles, users have full read and write access to the
> `sharelatex` container resources (filesystem, network, environment
> variables) when running compiles. Where not all users can be fully
> trusted, it is strongly recommended to use Sandboxed Compiles.

## Getting started

### Build & run

```
cd server-ce
make all          # builds sharelatex/sharelatex:ext-6.3.0-port (+ TeX Live base image)
```

The [`Dockerfile-base`](server-ce/Dockerfile-base) builds the
`sharelatex/sharelatex-base:*` image (dependencies + TeX Live) and
[`Dockerfile`](server-ce/Dockerfile) builds the application image on top.
Configuration lives in [`server-ce/config/env.sh`](server-ce/config/env.sh)
(plus toolkit seeds under `tools/toolkit`) — one place for site name, URL,
auth providers, LLM admin gates, compile images, and so on.

A ready-to-run local deployment (nginx + overleaf + mongo + redis) lives in
[`develop/docker-compose.yml`](develop/docker-compose.yml).

### Development

- Central entry point: `make help` (targets: `build`, `unit`, `hub`, `lint`,
  `format`, `ci`, `deploy-test`, `image`).
- The production web build is `cd services/web && yarn webpack:production`
  — this is the canonical gate; `yarn webpack` starts the *dev server*.
- In-repo unit/integration suites: `cd services/web && yarn test:unit`
  (Vitest, all projects) or the root `make unit`.
- `ext_explain.md` documents the extension surface (features, settings,
  module wiring) of the fork.
- `BRANDING.md` documents the OlliTeX rebrand and how to swap the logo
  set (final art belongs in `tools/logo/` and the referenced app paths).

### Testing

- End-to-end suite: [`tests/e2e`](tests/e2e) — a disposable, self-seeding
  stack (`scripts/stack-up.sh` → `playwright test` → `scripts/stack-down.sh`)
  covering smoke, admin site settings, LLM/BYO, grammar, keybindings,
  notifications, Zotero, Typst compiles, and a **legacy↔hub parity harness**
  (every removed legacy page is asserted to redirect and its capability to
  exist on the hub; see `tests/e2e/parity/check.mjs`).
- The suite's structure was inspired by the testing practices of
  [Forgejo](https://codeberg.org/forgejo/forgejo).

## Documentation

- **Wiki** — [`docs/wiki`](docs/wiki/README.md): user guide, admin guide,
  and installation docs, with auto-generated screenshots. A data-safety
  gate (link integrity + secret/private-e-mail-domain scan + PNG metadata
  scan) fails any run where something it should not be in the wiki is in
  the wiki; the audit receipt is at [`docs/wiki/AUDIT.md`](docs/wiki/AUDIT.md).
  - Regenerate screenshots (needs the e2e stack running):
    `make wiki-shots`.
  - Docs-only gate (CI):
    `make wiki-check`.
- Backlog: [`IMPROVEMENTS_2026-09-16.md`](IMPROVEMENTS_2026-09-16.md)
  (P0 first: admin API authorization pass, then SMTP unblock).
- Plans (untracked local docs): `BRANDING.md`, `ext_explain.md`,
  `TYPST_INTEGRATION_PLAN.md`, `docs/WIKI_PLAN.md`, and the round plans in
  the repo root (see `.gitignore` for the list).

## Contributing

OlliTeX is a community fork; contributions are welcome. Please read
[`CONTRIBUTING.md`](CONTRIBUTING.md), keep the AGPL notices intact, and run
`make ci` before opening a pull request.

## Authors

- [The Overleaf Team](https://www.overleaf.com/about) — Community Edition
- [yu-i-i](https://github.com/yu-i-i) — Extended CE features; adapted code
  listed in [`CREDITS`](CREDITS.md)
- [davrot](https://github.com/davrot) — the 6.3.0 port, the OlliTeX
  extension stack, and maintenance of this fork

## License

The code in this repository is released under the GNU AFFERO GENERAL PUBLIC
LICENSE, version 3. A copy can be found in the [`LICENSE`](LICENSE) file.

Copyright (c) Overleaf, 2014-2026.\
Copyright (c) @yu-i-i, 2024-2026, for the Extended CE features.\
Copyright (c) @davrot, 2025-2026, for the 6.3.0 port and extensions.

Portions of the code are derived from other open-source projects; see
[`CREDITS`](CREDITS.md).
