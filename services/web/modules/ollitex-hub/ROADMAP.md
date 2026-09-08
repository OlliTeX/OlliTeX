# OlliTeX Hub — Roadmap & Status (2026-09-08)

Owner surface: one unified page for the whole instance — workspace +
administration in a single nested-accordion rail (`/hub`). Canonical design:
`nav_structure.md` (repo root). Parity contract + machine gate:
`tests/e2e/parity/` (gate: `node tests/e2e/parity/check.mjs`, wired into CI
as the `parity-gate` job and as `specs/parity-check.test.e2e.ts`).

## Done (owner parity, 13/13 legacy pages under /hub)

| # | Legacy page | /hub leaf(s) | Gate |
|---|-------------|--------------|------|
| 1 | project list `/projects` | `#/projects.all` (+owned/shared/archived/trashed/tags) | ✅ |
| 2 | templates `/templates/*` | `#/templates.all` + dynamic categories | ✅ |
| 3 | library `/library` | `#/library` | ✅ |
| 4 | my settings `/user/mysettings` | `#/mysettings.*` (9 leaves) | ✅ |
| 5 | notification prefs | `#/mysettings.email` | ✅ |
| 6 | LLM user settings | `#/mysettings.llm.*` (4 leaves) | ✅ |
| 7 | admin users `/admin/user` | `#/site.general.users.*` (5 views) | ✅ |
| 8 | admin projects `/admin/project` | `#/site.general.projects.*` (4 views) | ✅ |
| 9 | manage templates `/admin/panel#templates`-style | `#/site.general.managetpl` | ✅ |
| 10 | instance stats `/admin/instance-stats` | `#/site.general.stats` | ✅ |
| 11 | LLM admin `/admin/llm/settings` | `#/site.llm.*` (6 leaves) | ✅ |
| 12 | CE admin panel `/admin/panel` | `#/site.general.messages` / `.editor` / `.activeprojects` | ✅ |
| 13 | manage site `/admin/site` | `#/site.general.*` + `site.integrations.*` + `site.services.*` + `site.compilation.*` | ✅ |

Status: **194/194 parity e2e tests green, gate 97/97**, committed
`5e07a23424`, production container cycled to the same revision.

## 2026-09-08 improvement wave (owner "do it 1–14" — all delivered)

| # | Item | Where |
|---|------|-------|
| 1 | Deterministic routing: hash = source of truth (guarded pushState, popstate, bounded reconciler) | `hub/hub-root.tsx` |
| 2 | Leaf-completeness audit: no silent stubs | `test/frontend/hub-leaf-audit.test.ts` + `KNOWN_STUBS` (= ∅) |
| 3 | API contract: shared `useSectionData` (labels = endpoint identity, TTL dedupe) + endpoint manifest spec | `shared/use-section-data.ts`, `tests/e2e/specs/parity/hub-endpoint-contract.test.e2e.ts` |
| 4 | Destructive-action guards: live editor-gate chip (`GET /admin/editor-state`), confirms on close/open/disconnect, DISCONNECT typed-confirm, single-user delete confirm | `sections/admin/admin-editor-section.tsx`, `admin-users-section.tsx`, `shared/confirm-modal.tsx` (+ `disabled`), `AdminController.editorState` + route |
| 5 | One safe toast API `notify/ok/fail` (never throws) + eslint `no-restricted-imports` in the hub module (wrapper is the only importer) | `shared/notify.ts`, `services/web/eslint.config.mjs`, 17 sections migrated (71 call sites) |
| 6 | Live data: Active Projects + System Messages refetch on tab activation & 30 s heartbeat + "last updated" + refresh button | `active-projects-section.tsx`, `system-messages-section.tsx` |
| 7 | Instance stats: `day`/`week` windows (server `WINDOWS` + types + options), empty state, alert-config card (recipients + disk/RAM % + **send test**) | `instanceStatsConstants.mjs`, `types.ts`, `config.ts`, `instance-stats-section.tsx` |
| 8 | URL-persisted leaf state: search/tag/page in the hash (`#/projects.all?q=…&page=2`), leaf-ownership marker prevents cross-leaf leakage | `shared/hash-params.ts`, `projects-section.tsx` |
| 9 | Refetch storm: shared 30 s `getSiteSettings()` cache + invalidate-on-save (never serves failures/stale after writes) | `shared/settings-cache.ts`, `site/site-core.tsx` |
| 10 | a11y: axe-core scan of a representative leaf sample per role; critical/serious fail, moderate reported for backlog | `tests/e2e/specs/parity/hub-a11y.test.e2e.ts` (`@axe-core/playwright`) |
| 11 | i18n: safe `useHubT(key, defaultValue)` pattern (UI can never show raw keys) applied to the Hub health leaf; per-section backlog below | `shared/hub-i18n.ts`, `hub-health-section.tsx` |
| 12 | CI: explicit docker-free `parity-gate` job + `hub-unit` job ahead of the full e2e job | `.github/workflows/e2e.yml` |
| 13 | This sheet | `modules/ollitex-hub/ROADMAP.md` |
| 14 | Hub health diagnostics leaf: server core (uptime/node/mongo ping/feature gates), 13 endpoint probes, captured client errors (ring buffer started at hub root), copy-report | `sections/admin/hub-health-section.tsx`, `shared/error-collector.ts`, `GET /api/hub/health` (HubController + router), nav `site.general.health` |

## Intentional stubs (the "no silent stubs" contract)

`KNOWN_STUBS = ∅` — currently **zero** nav leaves render the "…is being
built" placeholder. Any new nav leaf without a real renderer MUST appear in
`KNOWN_STUBS` (with a reason comment) to pass `hub-leaf-audit.test.ts`.

## i18n extraction backlog (#11 continued)

Pattern (safe, gradual — UI never shows raw keys):

```tsx
const t = useHubT()
<Text>{t('hub.projects.title', 'Projects')}</Text>
```

Per-section status:

| Section | Status |
|---------|--------|
| hub-health | ✅ pattern applied (model) |
| editor, active-projects, system-messages, instance-stats | next wave strings |
| projects, library, templates, mysettings.* | backlog (high user visibility first) |
| site.* (20 sections) | backlog (largest surface; consider section-by-section) |

When extracting a section: (1) add `hub.<area>.<key>` entries to
`services/web/locales/en.json`, (2) regenerate `extracted-translations.json`
(scanner chain guarded by `scripts/translations/i18n-lint.js` +
`test/unit/src/i18n-lint.test.mjs`), (3) optional other-locale values in
`locales/{lang}.json`.

## Accessibility baseline (#10)

- Gate today: axe `wcag2a/wcag2aa/wcag21a/wcag21aa`, fail on
  critical+serious, moderate → console backlog.
- Known Mantine 9 gotchas to re-check each release: Switch
  `.mantine-Switch-root` click target, `role="dialog"` is a `<section>`,
  modal focus traps. The a11y spec re-runs on the live stack in CI.

## Next candidates (unscheduled, owner decides)

1. **Socket-driven live** for Active Projects / System Messages (today: 30 s
   heartbeat — correct, but polling).
2. **Visual regression** (Playwright screenshot diff of the 13 leaves) as a
   follow-up to the a11y gate.
3. **Phase 4 — legacy retirement**: remove legacy page routes + React shells
   *after* owner sign-off (API endpoints stay — /hub consumes them); clean
   nav/entry points; tag `parity-complete`. Machine gate already blocks any
   legacy feature removal from the matrix (`parity/check.mjs`).
4. **i18n completion** per the backlog table above.
5. **Hub telemetry** (optional, off by default): section render timings +
   probe failures surfaced in Hub health over time.

## How to run the guardrails

```bash
# hub unit tests (incl. leaf audit + icon guard)
cd services/web && yarn vitest run modules/ollitex-hub

# eslint guard (no raw toast imports in the hub)
cd services/web && node ../node_modules/eslint/bin/eslint.js --max-warnings 0 \
  modules/ollitex-hub/frontend/js

# parity gate (machine matrix)
node tests/e2e/parity/check.mjs

# e2e parity + contract + a11y (stack up first)
cd tests/e2e && npx playwright test specs/parity --workers=1 --retries=0
```
