# Leak sweep — 2026-10-11 (E: no-credential / no-secret guard)

Scope: `README.md`, `docs/wiki/**` (all pages + assets), `docs/*.md`.
Directive: no credentials, API keys, tokens, secrets, personal data, or
instance-specific credentials in any doc text or image before it lands on
GitHub.

## Sweep results

| Check | Method | Result |
| --- | --- | --- |
| Key/secret patterns in doc text | regex: `api[_-]?key`=value, `sk-*` (16+), JWT `eyJ*` (20+), `password`=value, `Bearer *` (16+), `xox[bp]-*`, `ghp_*` over `README.md` + `docs/` | **0 hits** |
| Real e-mails / dev accounts | grep `@uni-bremen.de`, `psintern.neuro.uni-bremen.de`, `uiverif`, `nontest@dev*` over `README.md` + `docs/` | **0 wiki hits**; 1 hit in `docs/AA_WHOLE_PROJECT_AUDIT_2026-10-07.md:208` (INTERNAL audit receipt — the instance *URL* is referenced as deployment context, not a credential; kept, flagged) |
| Long hex / base64 blobs | `\b[0-9a-f]{32,}\b`, `\b[A-Za-z0-9+/]{48,}={0,2}\b` over `README.md` + `docs/wiki/` | **0 hits** (matches that fired were file paths, verified) |
| Screenshot (PNG asset) strings | `strings` over all 41 wiki asset PNGs, greps for e-mail regex, `sk-*`, `eyJ*`, `xox[bp]-*` | **0 hits** (assets are auto-generated from the disposable E2E fixture stack per WIKI_PLAN §1 — no manual captures, no real accounts) |
| Placeholder discipline | new/updated pages (users/11-wakatime, users/12-editor-markdown, users/09-settings, admins/01/07/10/11/12, installation/04/06, README) | examples use `user@example.org`, `changeme`, `my-client`, `••••••••` — no real material |

## What the new pages explicitly guard

- `users/11-wakatime.md` — personal API key = credential material; copy only
  when needed, treat like a password.
- `admins/10-wakatime.md` — relay target must stay instance-local unless
  deliberate; Wakapi admin pw = secret-store material, never in docs.
- `admins/11-python-runner.md` — exposes no credentials by design.
- `admins/12-federation.md` — client secrets documented by shape only
  (`client id: my-client`), never value; provider secrets redacted in
  screenshots.
- `installation/04-security-sandbox.md` — the sweep procedure itself is
  written into the credential-hygiene section (the standing guard).

## Verdict

**PASS** — README + wiki clear for publication under the owner's
no-credential/no-secret hard rule. 1 flagged internal doc (audit receipt,
instance URL only — no credential) kept in place as an internal receipt.

Swept by: AG on the dev instance (2026-10-11). Receipt filed alongside the
wiki; no assets regenerated in this pass (existing assets re-scanned only).
