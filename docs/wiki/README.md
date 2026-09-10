# OlliTeX wiki

The complete user and admin guide for the OlliTeX instance.

**OlliTeX** (the Olmecs' rubber, meets TeX's precise ink) is a free,
self-hostable, real-time collaborative authoring platform for **LaTeX and
Typst**, built on [Overleaf Community Edition](https://github.com/overleaf/overleaf)
(AGPL v3).

## Where do I start?

| I am… | Start here |
|---|---|
| A **new user** | [Getting started](users/01-getting-started.md) → [Projects](users/02-projects.md) → [The LaTeX editor](users/03-editor-latex.md) |
| Working in **Typst** | [The Typst editor](users/04-editor-typst.md) |
| Using **AI features** | [AI features & your own LLM provider](users/05-ai-features.md) |
| Managing **references** | [Reference managers & bibliography](users/06-references.md) |
| An **admin** | [Admin overview](admins/01-admin-overview.md) → [Users](admins/02-users.md) → [Site settings](admins/04-site-settings.md) |
| **Deploying** OlliTeX | [Docker deployment](installation/01-docker.md) → [Configuration](installation/02-configuration.md) |

## The hub

Nearly everything lives on **one page**: the hub at [`/hub`](users/01-getting-started.md#the-hub).
Every section has a stable deep link (e.g. `/hub#/mysettings.email`,
`/hub#/site.llm.instance`) — those URLs are used throughout this wiki and can
be bookmarked.

## Conventions used in this wiki

- **Hub → …** means: open `/hub`, then the rail section, then the leaf.
- *Bold* text marks buttons, menus, and field labels exactly as they appear.
- Screenshots are **auto-generated** from a disposable test stack with
  synthetic fixture data only — no real users, credentials, or keys ever
  appear. See the [audit receipt](AUDIT.md).
- This guide describes OlliTeX, a **fork of Overleaf Community Edition
  (open source, AGPL v3)**.

## Verified against

OlliTeX @ `f827b5ceb9` — built and verified 2026-09-16 (see
[AUDIT.md](AUDIT.md) for the exact shot list and data-safety scan).
