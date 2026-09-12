# OlliTeX — What's new

> This file feeds the "What's new" card on the hub Overview (`docs/RELEASE_NOTES.md`
> is served at `/api/hub/notes`). Keep entries user-facing and concise; newest
> first.

## 2026-09 — OlliTeX 1.0 (ext-6.3.0-port)

### One console
- The whole instance lives in **/hub**: workspace (Projects, Templates, Reference library, My settings) and administration (Users, Projects, Site settings, LLM, Templates, Instance stats, Health) in a single accordion rail
- Site settings fully in the hub: sign-up, e-mail, SSO (SAML / OIDC / LDAP with editable timeouts), branding, sandboxed compilation
- Template management restored: "All users are template gallery admins" + "Non-admins can publish templates" switches, full per-category table with live template counts and edit
- Git integration tokens in My settings — add, one-time reveal, revoke
- Legacy admin pages now redirect to their hub leaves (`/admin/site` → `/hub#/site`, and the retired user/project/llm pages likewise)

### Editor
- Renovated Mantine editor shell with full feature parity (rail, toolbar, panes, modals)
- **Typst** support end-to-end: create, edit, compile, preview — alongside LaTeX
- Compiles run in **sandboxed sibling containers** (mandatory) — no TeX Live install on the app container, no local runner

### Accounts & integrations
- Reference library: paste / .bib / ORCID / Zotero imports, bulk actions, trash & restore
- Project synchronisation: WebDAV, GitHub, Dropbox in My settings
- LLM features: Ask AI, grammar checking, compliance review, BYO providers, instance token meter and admin controls

### Platform
- Free local quality gate (no hosted CI): `make selftest`, `make release`, git pre-push hook
- E-mail works with any SMTP relay (self-hosted, e.g. Mailpit) — no paid service
- E2E suite (Playwright) + full vitest unit suite; production deployments verified with a live 17-point probe
