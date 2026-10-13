# E2E INTENSIVE-COVERAGE AUDIT — 2026-10-13

Owner question (work order): **"can you check if everything is covered by intensive (e2e) tests?"**

Inventory basis: 124 spec files under `tests/e2e/specs/` (+ `tests/e2e/parity/check.mjs`
gate run in CI `e2e.yml`), 47 Go web features, frontend feature modules, and the
board/progress history of this engagement.

## Classification
- **INTENSIVE** — dedicated multi-leg arc, behavior + wire contract, 2× green stable
- **CONTRACT** — API/wire contract pinned (status/shapes), fewer behavior legs
- **PIN**   — pins the CURRENT (incomplete) behavior; self-activates when the feature lands
- **GAP**   — no dedicated spec (or audit-only)

## Matrix (per product surface)

| Surface | Spec(s) | Class |
|---|---|---|
| Auth (login/register/activate/reset/mail flow) | auth-lifecycle, auth-pages, a5smoke | INTENSIVE |
| Sharing: invite/perm/remove/request-access/token-links | sharing-* (5 suites) | INTENSIVE |
| Comments (wire + UI + two-user + multi-file) | comments-ui-lifecycle, comments-two-user, comments-multifile | INTENSIVE |
| Track changes (toggle/capture/accept/two-user) | trackchanges-intensive | INTENSIVE |
| Notifications (mail fan-out, read-state, mute) | notifications, notifications-full | INTENSIVE |
| History/versions (list/restore/diff surface) | history-versioning, review-edited-at | INTENSIVE |
| Collab (per-doc rooms 024-Option-B, two-user, sync) | collab-two-user, multifile-collab, realtime-bus | INTENSIVE |
| File tree CRUD (folders/files/rename/upload) | editor/file-tree, file-view-svg | INTENSIVE |
| Complex editing TeX (rich/bib/ORCID/compile) | complex-edit-tex (6/6) | INTENSIVE |
| Complex editing Typst (CM6 dispatch/refs/compile) | complex-edit-typst (5/5), typst-t2 | INTENSIVE |
| Markdown/Milkdown (empty new, theme, round-trip) | markdown-visual (4/4) | INTENSIVE |
| WakaTime emission (dual emitter + gate chain) | wakatime-emission (4/4) + owner flags AF-1/2/3 pinned | INTENSIVE |
| Grafana/Prometheus bridge | admin-grafana-bridge (4/4), parity hub-instance-stats | INTENSIVE |
| File proxy `/Project/:id/file/:fid` (oracle legs) | fileproxy-route (6/6) | INTENSIVE |
| Federation git_url edge (allowlist + dormant DELETE) | federation-git-edge (4/4), federation-gate | INTENSIVE |
| SSO admin + login slots | sso-admin-live (a/c legs) | INTENSIVE (leg b external — pinned) |
| Reference managers: Mendeley/Zotero | mendeley-link-cycle, zotero, zotero-link-cycle | INTENSIVE |
| Sync conflicts: Dropbox/WebDAV/git-hosts | sync-conflict-dropbox, -webdav, -git-hosts, sync-graceful | INTENSIVE (Dropbox tokens pin) |
| Git bridge (clone/sync/token lifecycle) | git-clone-bridge, git-bridge-edge-probe, git-bridge-suspended-token | INTENSIVE |
| Chat (project + service) | project-chat, parity/service-chat-notifications | INTENSIVE |
| LLM (BYO + admin + usage) | byo-llm, parity hub-llm-admin, hub-llm-settings, legacy-llm-* | INTENSIVE |
| Python runner | python-runner | INTENSIVE |
| Drawio / SVG file view | drawio-roundtrip, file-view-svg | INTENSIVE |
| Equation editor | equation-editor | INTENSIVE |
| LanguageTool grammar checks | grammar | INTENSIVE |
| Templates (publish/manage/gallery) | parity hub-templates-manage/gallery, legacy-templates-manage/gallery | INTENSIVE |
| Hub 13-leaf parity matrix | parity/check.mjs + 13 parity hub-* specs + hub-owner-batch2 | INTENSIVE (CI-gated) |
| Admin (users/site/stats/panel) | admin-user-lifecycle, admin-site, parity legacy/hub-admin-* | INTENSIVE |
| a11y contrast (hub + NON-hub dual-scheme) | parity/hub-a11y, a11y-dual-scheme-nonhub (9/9), editor/a11y-baseline | INTENSIVE (gates) |
| Review panel surface | review-panel, review-overview-completeness, review-fileview-500 | INTENSIVE |
| ZIP import/export + tracked-changes/comments | zip-tracked-comments (13 legs: A–M) | CONTRACT+PIN |
| Compile + build output | compile-errors-bibtex, complex arcs (clsi) | INTENSIVE |
| Consent | consent | CONTRACT |
| Email templates | email-templates | CONTRACT |
| Project tags/inspection | project-tags-inspection | CONTRACT |
| Project metadata | project-metadata | CONTRACT |
| Inactive data rules | inactive-data | CONTRACT |
| Word count / selection | word-count-selection | CONTRACT |
| Keyboard shortcuts surface | keybindings | CONTRACT |
| Settings canonical + mysettings/admin | settings-canonical, parity hub/legacy mysettings/admin-site | INTENSIVE |
| List continuation (hub project list) | list-continuation | CONTRACT |
| Release notes | parity/release-notes | CONTRACT |
| Dropbox live token cycle | (pinned in dropbox-link-cycle) | PIN (owner token) |
| System messages | (no dedicated spec — audit-level only) | GAP (thin) |
| Launchpad/onboarding pages | (a5smoke reach) | GAP (thin) |
| Static pages / status / health | (smoke + parity endpoint-contract) | CONTRACT |
| Analytics | (backbone audit only) | GAP (audit-only) |
| Per-chunk history attribution (65b0d668) | audited in a11y-dual-scheme (leg 1) → "Anonymous" | PIN (product gap) |
| PDF visual fidelity | pdftotext assertions only (no pixel) | GAP (method) |

## Verdict
- **Core product surfaces are INTENSIVELY covered** — 30+ surfaces have dedicated
  multi-leg arcs, most 2× green and self-cleaning in this engagement (487/1/4
  full battery + per-arc batteries).
- **Contract/PIN level (honest residuals):**
  1. ZIP import/export of tracked-changes + comments — **feature ABSENT in this
     checkout** (route 404s; admin multi-zip is a placeholder; import is files-only).
     `zip-tracked-comments` (13 legs) pins the current state and SELF-ACTIVATES the
     full owner contract (export JSON payloads, import carry, round-trip, corrupt-JSON
     determinism, permission leak-gates, template exclusion) the moment the
     implementation lands.
  2. SSO leg (b) real LDAP transport — externally blocked (pinned).
  3. Dropbox live credentials — owner token expired (pinned; cycle contract pinned).
  4. 65b0d668 per-update attribution — product gap CONFIRMED live ("Anonymous"),
     kept open for owner design.
- **Thin/GAP (low-risk, contract-level or audit-only):** systemmessages,
  launchpad, staticpages, analytics; PDF pixel-level fidelity (method gap —
  assertions are pdftotext-based).
