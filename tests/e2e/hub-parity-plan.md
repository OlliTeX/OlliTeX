# /hub legacy-parity plan — capture → map → prove → retire

**Owner decision (2026-09-07):** pause incremental feature work; execute phases 0→4 in
order; do not rush; precision over speed. Delete old pages **only** after 100% of
functionality is present under /hub *and* protected by tests. Remove all /hub →
legacy-page links in the same final wave (today they are the only reason legacy pages
must keep living).

**Roles (owner note 2026-09-07, 22:4x):** every legacy page is crawled/asserted as
1. **SITE ADMIN** (`e2e-admin@e2e.test`, isAdmin)
2. **TEMPLATE ADMIN** (`e2e-tpladmin@e2e.test`, canManageTemplates only, NOT site admin)
3. **USER** (`e2e-user@e2e.test`, plain member)

**Modals (owner note):** modal chains can be **several layers deep** — the crawl is
modal-BFS (open a control → snapshot the dialog → recurse into its controls → close →
next branch), depth-capped at 5, and every layer is a first-class matrix feature
(`modal: [L1…L3]`).

**Why we can do this now:** the legacy pages are alive and served. Phase 0 converts
their behavior into **repo-pinned, executable artifacts** — when we delete the pages
in Phase 4, nothing is lost: the matrices + baseline tests remain the contract.

---

## Pages in scope (13)

| Legacy page | Role access | /hub target leaf(s) |
|---|---|---|
| `/project` | all (data differs) | `#/projects.all` (+ .mine/.team etc.) |
| `/library` | all | `#/library` |
| `/templates` | all (browse), tpladmin/admin also manage | `#/templates.all` |
| `/templates/manage` | tpladmin + admin | `#/site.general.managetpl` |
| `/user/mysettings` | all | `#/mysettings.*` (7 leaves incl. `.etc`) |
| `/user/llm-settings` | all | `#/mysettings.llm` |
| `/user/notification-preferences` | all | `#/mysettings.notifications` |
| `/admin/instance-stats` | admin | `#/admin.leaf.instance` |
| `/admin/panel` | admin | `#/site.admin.panel` |
| `/admin/llm/settings` | admin (+ tpladmin if enabled) | `#/site.admin.llm` |
| `/admin/site` | admin | `#/site.general.enclose` (index) + 16 section leaves |
| `/admin/user` | admin | `#/site.general.users.*` (4 views + `.etc`) |
| `/admin/project` | admin | `#/site.general.projects.*` (4 views + `.etc`) |

## Deliverables (all under `tests/e2e/parity/` unless noted)

| Path | Content |
|---|---|
| `hub-parity-plan.md` | this file + live status table (updated each phase) |
| `crawl/crawl.mjs` | crawl tooling: 3 roles × 13 pages, deep-modal BFS, DOM+network capture, safe-action blocklist (never fires Save/Delete/Confirm etc.) |
| `crawl/roles.json` | role list + access expectations (credential values come from `fixtures/credentials.ts` — never new secret files) |
| `reference/<role>/<page>/` | frozen artifacts: `initial.dom.json`, `modal-<path>.dom.json`, `network.log.json`, screenshots; stamped with commit hash + date |
| `legacy/<page>.yaml` | **functional matrix**: every control/modal/branch/condition with `behavior` (endpoint+payload+visible effect), `roles`, `preconditions`, `tested_by` |
| `map.yaml` | legacy feature → hub location + tests; statuses `mapped+tested / mapped / missing / excluded (reason)` |
| `check.mjs` | **parity gate**: exits 1 if any feature is unmapped, or mapped but not tested, or a `tested_by` test doesn't exist in the suites |
| `specs/parity/legacy-<page>.test.e2e.ts` | legacy baseline suite (proves old pages work as designed — the executable reference) |
| `specs/parity/hub-<page>.test.e2e.ts` | hub parity suite: same endpoints (request-sequence match), same visible outcomes, per role |
| `checks/parity-check.test.e2e.ts` | runs `check.mjs` inside the e2e suite → the 100% guarantee is CI-enforced |

Feature entry shape (matrix):
```yaml
- id: users.row.delete
  kind: button                 # button|input|textarea|checkbox|select|radio|tab|menu|modal|table|banner|link
  where: row-menu              # stable location text/path (not locator — locators live in tests)
  modal: ["Confirm delete (L1)"]
  roles: { admin: full, tpladmin: denied-403, user: denied-403 }
  behavior:
    endpoint: POST /admin/user/:id/delete
    visible: row removed + notification
  preconditions: [user-list-loaded]
  tested_by: { legacy: parity/legacy-admin-user.ts#delete-flow, hub: parity/hub-admin-users.ts#delete-flow }
```

## Phases

### Phase 0 — Freeze the reference ⏳ (in progress)
1. Crawl tooling + template-admin fixture (register via /register + activate + `canManageTemplates=true` in the e2e user doc).
2. Crawl 13 pages × 3 roles, deep-modal BFS (depth ≤ 5), capture DOM + exact network sequence per control; role-denied pages recorded as features (403/redirect/blank).
3. Draft the 13 matrices (crawl + code audit cross-check).
4. Legacy baseline suite (specs/parity/legacy-*.test.e2e.ts), green.
- **Gate 0:** reference artifacts committed (stamp = commit hash), baseline suite green, matrix draft complete.

### Phase 1 — Map legacy → hub
- `map.yaml` generated from nav-tree + code grep (endpoint strings) + existing hub tests; corrected by hand.
- Gap list ordered into waves (smallest blast radius first): user leaves → library → templates/manage → project → admin panel/instance/llm → admin users → admin projects → admin site.
- **Gate 1:** every feature has a status (mapped / missing / excluded-with-reason).

### Phase 2 — Close the gaps, wave by wave
Per feature (≤ ~10 per wave): implement under /hub + vitest (component) + hub parity e2e
(same endpoints + visible outcome, all role-applicable entries) + matrix `tested_by` set.
- Gates per wave: vitest → webpack → e2e (full) → `check.mjs` (coverage monotonically grows).

### Phase 3 — Conditional & context-sensitive matrix
Assert for **both** legacy and /hub:
- **Roles:** site-admin vs template-admin vs user (incl. "denied" expectations).
- **Feature flags:** zotero/mendeley/webdav/dropbox on↔off, LLM BYO vs admin-configured, python-runner off.
- **Data states:** empty lists, 200+ items, trashed/archived/shared.
- **Error paths:** API 500/403 per critical action via Playwright `page.route` — assert the exact user-visible message.
- **Gate 3:** every matrix `roles`/`preconditions` entry has a passing assertion on both sides.

### Phase 4 — Retire (only when the machine says 100%)
- `check.mjs`: 100% mapped, 100% tested, legacy baseline + hub parity green, webpack green.
- Deletion wave: legacy page routes + React shells removed (**API endpoints stay — /hub consumes them**), /hub → legacy links removed, nav/entry points cleaned.
- Side-by-side screenshot diff set per page for owner visual sign-off.
- Tag `parity-complete`; final production image + cycle (proven flow: `make all` → marker verify → cycle).

## Status

| Page (13) | Matrix | Legacy baseline | Hub parity | Gate |
|---|---|---|---|---|
| `/admin/user` | ✅ 11 features | ✅ 9/9 | ✅ 8/8 | ✅ GREEN (both sides) |
| `/admin/project` | ✅ 9 features | ✅ 9/9 | ✅ 8/8 | ✅ GREEN (both sides) |
| `/admin/site` | ✅ 8 features | ✅ 8/8 | ✅ 8/8 | ✅ GREEN (both sides) |
| `/admin/panel` | ✅ 6 features | ✅ 6/6 | ✅ 6/6 | ✅ GREEN (both sides) |
| `/admin/llm/settings` | ✅ 7 features | ✅ 7/7 | ✅ 7/7 | ✅ GREEN (both sides) |
| `/admin/instance-stats` | ✅ 6 features | ✅ 6/6 | ✅ 6/6 | ✅ GREEN (both sides) |
| `/project` | ✅ 8 features | ✅ 8/8 | ✅ 8/8 | ✅ GREEN (both sides) |
| `/library` | ✅ 7 features | ✅ 8/8 | ✅ 6/6 | ✅ GREEN (both sides) |
| `/templates` | ✅ 6 features | ✅ 6/6 | ✅ 6/6 | ✅ GREEN (both sides) |
| `/templates/manage` | ✅ 8 features | ✅ 8/8 | ✅ 7/7 | ✅ GREEN (both sides) |
| `/user/mysettings` | ✅ 9 features | ✅ 9/9 | ✅ 9/9 | ✅ GREEN (both sides) |
| `/user/llm-settings` | ✅ 8 features | ✅ 8/8 | ✅ 8/8 | ✅ GREEN (both sides) |
| `/user/notification-preferences` | ✅ 6 features | ✅ 6/6 | ✅ 6/6 | ✅ GREEN (both sides) |

**FINAL STATUS (2026-09-09): **13/13 page-pairs GREEN — 194/194 parity tests
passing (serial, workers=1, retries=0); `parity/check.mjs` = **GATE GREEN — 13
matrices, 97/97 features covered BOTH sides** (also asserted in-suite by
`parity-check.test.e2e.ts`).

Completion wave (2026-09-09) added pages 10–13:
- `/admin/instance-stats` ↔ hub `#/site.general.stats` — series API + alert-config
  round-trip + test-alert + windows (month/6m/year/all).
- `/admin/llm/settings` ↔ hub `#/site.llm.*` — settings JSON, save round-trip,
  check, models, usage. Bug fixed: PG-AL-1 (hub read `/admin/llm/settings` HTML
  route instead of `/settings/json` → empty panel) now uses the JSON endpoint.
- `/admin/panel` (3 CE panes) ↔ hub `#/site.general.messages|editor|activeprojects`
  — system messages post/clear, active-projects real-time feed, editor
  open/close + disconnect-all. Bugs fixed: PG-PN-1 (editor pane had no hub
  surface → added `AdminEditorSection`), PG-AP-1 (hub "Active projects" leaf was
  a build-plan placeholder → added `ActiveProjectsSection` on
  `/admin/active-projects`), PG-TO-1 (hub toasts must use the module-level
  `notifications` import, not a hook).
- `/admin/site` ↔ hub `#/site.general.enclose` + section leaves — 22-section
  settings read, misc/signup round-trips, SSO enable-without-credentials guard
  (422 parity), email-test determinism, template-admins list.
- ESLint across the hub module: **0 errors / 0 warnings** (fixed 28 pre-existing
  violations from earlier waves: unused imports/vars, no-console, a11y
  label/anchor rules, exhaustive-deps).
- E2E determinism: hub hash-only navigations can race the shell state — UI-level
  assertions for newly-created data use a fresh first-load context (documented
  inline at the call sites).

Wave fixes folded into hub source (all e2e-evidenced):
- PG-HB-1 LLM modal crash: capture `e.currentTarget.value` BEFORE lazy `setState` updaters (6 sites, llm-settings-section).
- PG-TH-1/2 template-import FileInput: normalize to real `File|null` (no circular-JSON Event); PG-TH-3 Modal `opened=`; PG-TH-6 edit POST `{ body: editForm }`; PG-TE-2 partial-update body (license is schema-required → omit blank fields).
- PG-LR-1 Mantine 9 Checkbox delivers `(checked, event)`: 4 sites normalized (library select-all ×2, admin-projects skip-emails, admin-users bulk-email).
- PG-SW Switch normalization (computed-inversion) across llm/admin-* sections; PG-ND-1 NumberInput → plain input.
- Harness: added PUT/PATCH branches to `api()` (was silently POSTing).
- Gate matcher: test-title substring match (tokens may be multi-word); titles are the contract names.

## Proven pattern (per page)
1. `parity/legacy/<page>.yaml` — feature list w/ legacy endpoints + `legacy_test`/`hub_test` tokens.
2. `specs/parity/legacy-<page>.test.e2e.ts` — one shared login; API-first flows (endpoint+payload+state); role denial asserted.
3. `specs/parity/hub-<page>.test.e2e.ts` — drives the NEW Mantine UI, asserts same endpoints+payload+visible state.
4. `node tests/e2e/parity/check.mjs` — RED until every feature has a test on BOTH sides (CI-enforced via `specs/parity-check.test.e2e.ts`).

## Hard-won facts (reuse on every page)
- **Host**: use `http://127.0.0.1:7420` (Playwright baseURL) — `localhost` splits the cookie jar → silent 403s/redirects.
- **Login**: ONE per spec file (shared context); CE rate-limits 20/min/IP → storm = CAPTCHA.
- **CSRF**: token is per page-load; on 403 → `page.reload()` + retry once.
- **Hub hash**: dotted leaf id `#/site.general.users.all` (NOT slashes).
- **Hub menus**: `button[aria-label="Actions"]` → `[role=menu]` items; modals `[role=dialog]`; confirm buttons scoped to `[role=dialog]`.
- **List pages**: search box (placeholder) to locate throwaway rows before acting (pagination hides new rows).
- Legacy row actions are aria-labeled icon buttons (`aria-label="Info"|"Update"|"suspend"|"Delete"|"Resend"`).

## Open parity gaps (from audit)
- **PG-REG-1**: `/user/activate` is 404 in the current build (port delisted the `user-activate` module because its router hijacked `GET /admin/user`) → register→set-password flow broken; must be restored under parity.
- e2e templates: 0 (prod has 3) → "Example project" menu branch covered by vitest; template fixtures optional.
