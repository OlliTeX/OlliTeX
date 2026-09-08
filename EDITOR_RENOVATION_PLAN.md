# Editor renovation plan — Mantine under `/editor`

Owner mandate (2026-09-08): renovate the production editor in **CodeMirror 6 (already
in use — keep it) + Mantine, including all modals**, served under the **`/editor`**
route. **No functionality may be lost.** **Everything gets test coverage while we
renovate.**

Status: **PLAN** — Phase 0 in flight (2026-09-09). Method: the proven `/hub`
playbook — parity matrix as contract, per-surface swap-with-gate, legacy route
untouched until GREEN, owner checkpoint per phase.

---

## P0 progress log (live facts, 2026-09-09)

- **Route live**: `GET /editor/:Project_id` (+ detached variant) registered in
  `app/src/router.mjs` with the identical middleware chain as
  `/Project/:Project_id` (contract test:
  `test/unit/src/Project/EditorRouteContract.test.mjs`). Dual-run e2e GREEN
  (3/3): render → type (OT) → compile → PDF on the NEW url; guest +
  non-member + detached answers identical to the legacy url.
- **Gate live**: `tests/e2e/parity/editor/*.yaml` (9 docs, 68 rows, phase-dated
  P0…P8) + `tests/e2e/editor/check.mjs` (phase ratchet via
  `tests/e2e/editor/PHASE`, currently **P0**: 6/6 due rows covered, 62 pending)
  + CI job `editor-gate` + spec driver `specs/editor/parity-gate.test.e2e.ts`
  (GREEN).
- **a11y baseline captured** (`tests/e2e/editor/a11y-baseline.json`): the LEGACY
  editor already shows **2 critical + 2 serious** axe violations
  (aria-required-children, button-name, aria-prohibited-attr, nested-interactive)
  → the P8 target (zero) is a strict improvement path.
- **Test-debt findings (corrected)**: the fork's live frontend runner is
  **vitest** (projects in `vitest.config.js`); the old mocha `test:frontend`
  suite is broken in this fork (mocha-11 `.tsx` load failures + Cypress `cy`
  references) — specs counted there are NOT executing. Real debt therefore:
  **command-palette: 0 live specs (fixed — 10 baseline tests)** and
  **share-project: API contract now frozen in the live runner** (5 tests).
  New vitest project: `EditorRenovation`
  (`test/frontend/editor-renovation/**`, 15/15 green), added to CI.
- **Exports touched by the baselines** (behavior-neutral):
  `getSourcesMatchingQuery` exported from
  `command-palette/hooks/use-command-palette-results.ts`.
- **Not changed by P0**: any visual surface, the CM6 core, APIs, CSS. /editor
  and /Project are pixel-identical today — exactly as the mandate requires
  before renovation starts.

---

## 1. What we have today (inventory, verified against source)

### Already React + CM6 (this is the base — the core is NOT rewritten)
- **IDE shell** `features/ide-react/` (141 TS files): rail (tabs, panels, account
  menu, help, overflow, keyboard-shortcuts), toolbar (title, share, compile, export,
  download, duplicate, review-mode, history, online-users, change-layout,
  offline-indicator, request-access), layout/main-layout, resize handles, unsaved
  docs, alerts, editor-manager context + connection (socket/OT), scope-value-store,
  references store.
- **Source editor** `features/source-editor/` (306 TS files): **CodeMirror 6** with
  extensions (keymaps, math preview, autocompletion, auto-close brackets, comments,
  LLM inline actions, grammar highlights, review decorations…), CodeMirror toolbar,
  keybindings system, math-preview-tooltip, search, floating menus.
- **Panes**: file-tree (59 files), review-panel (52), history (72), pdf-preview
  (66), chat (12), outline (9), command-palette (10), integrations-panel,
  editor-floating-menu, editor-navigation-toolbar, bookmarkable-tab, tooltip, bibtex
  (references pane), mathjax.
- **Modals in the IDE today**: `ide-react/components/modals/` — generic-confirm,
  generic-message, diff-viewer, force-disconnected, out-of-sync, unable-to-sync,
  project-converted-from-document; plus share-project-modal (32 files),
  clone-project-modal, word-count-modal, hotkeys-modal, request-access-modal,
  settings (69 files), publish (SaaS — **not in this build**), table-generator,
  figure-modal, equation-ai, editor-survey, new-editor-promo.
- **Module UIs inside the editor**: python-runner (tab + output), LLM (ask-ai,
  compile-fix, BYO settings, grammar via LanguageTool module), zotero import,
  mendeley import, webdav, dropbox, github sync, linked files, diagram visual editor.
- **Route**: `GET /project/:ProjectId` → `ProjectController.editorPage` →
  `app/views/project/ide-react.pug` (entry `pages/ide`, DOM root `#ide-root`).
  `ide-react-detached.pug` variant for the detached/sharejs flow.

### The actual gap
- **Zero Mantine anywhere in the editor** (verified: 0 `@mantine` imports across
  all editor surfaces). All styling = the old SCSS system (`stylesheets/pages/
  editor/*.scss` ~40 files: ide.scss, ide-redesign.scss, rail.scss, chat.scss,
  history.scss, review-panel.scss, file-tree.scss, math-preview.scss, logs.scss,
  …). Mantine exists only in `/hub`, the page shells, and a few module widgets.
- Test coverage is uneven (existing vitest spec files per surface):

| surface              | specs | gap?                          |
|----------------------|-------|-------------------------------|
| source-editor        | 21    | good                          |
| file-tree            | 13    | good                          |
| ide-react            | 11    | okay                          |
| pdf-preview          | 7     | okay                          |
| history              | 3     | thin → fill in its phase      |
| review-panel         | 2     | thin → fill in its phase      |
| chat                 | 1     | thin → fill in its phase      |
| word-count-modal     | 1     | thin → fill in its phase      |
| **share-project**    | **0** | **debt — must fix**           |
| **clone-project**    | **0** | **debt — must fix**           |
| **settings (modal)** | **0** | **debt — must fix (largest)** |
| **command-palette**  | **0** | **debt — must fix**           |

- E2E that must stay green throughout (existing): smoke (open/compile/PDF),
  editor-console-canary (zero client errors), keybindings, grammar, byo-llm,
  zotero, python-runner, notifications, sync-graceful, external-probe.

### Invariants — things we NEVER touch (the "no functionality lost" guarantee)
1. **OT/socket sync** (document-updater, sharejs, presence, unsaved-docs, sync
   modals semantics) — renovated only where a *modal banner* re-skins them.
2. **Compile pipeline + PDF preview engine** (compilers, pdf.js, log parsing).
3. **CodeMirror 6 extension graph** — behavior identical; only *chrome around it*
   (menus, tooltips, popovers, search UI) is re-skinned, with behavior tests
   proving equivalence.
4. **API surface** — every `/project/:id/...` route, socket events, cookie/session.
5. **DOM contract** `#ide-root` (browser extensions rely on it — the pug has an
   explicit TODO to keep `main` for this reason) + `.loading-screen`, CSP nonces,
   `socket.io` script, theme init.
6. **i18n keys** (same translated strings), **feature flags** (saas/CE split,
   pythonRunner, LLM, webdav… all stay), **dark/light + loading-screen theme**.

---

## 2. Architecture

```
GET /editor/:ProjectId     → new route (same auth: requireLogin + role guard,
                             same editorPage controller + template; variant flag
                             `ui.mantine === true` set for this page)
GET /project/:ProjectId    → EXACTLY today's build, byte-identical behavior,
                             stays live as the fallback for the whole program
```

- **New entry** `pages/ide-mantine.tsx` (webpack auto-discovers `pages/*`) → same
  feature modules, but a renovating component layer: every surface ships a
  Mantine-backed variant; the variant is selected by the UI flag, so **each
  surface can be rolled back individually** (flag off → legacy component).
- **CSS isolation**: new styles live in `stylesheets/pages/editor-v2/*.scss` under
  a root class (`#ide-root.ol-editor-v2`). Legacy scss is never edited until a
  surface is confirmed done — `/project` therefore stays pixel-perfect.
- **Theme**: reuse the OlliTeX Mantine theme (OL palette, light + dark) via the
  existing `shared/provider` machinery; editor-scoped dark scheme follows the same
  `overallTheme` rules as `/hub` (owner-settable site-wide, user-overridable).
- **CM6 interop rule**: Mantine renders *outside* the CM6 editor root (toolbars,
  popovers, modals). Anything that renders *inside* CM6 (inline decorations,
  autocompletion) keeps its CM6 DOM; we style it via CSS in `editor-v2`, and only
  lift it into a Mantine popover where behavior is fully test-proven (search,
  command menu) — always later phases, never P1-P4.
- **Flags**: `editorUI.mantine.<surface>` per surface (toolbar, rail, modals,
  fileTree, review, history, chat, pdf, references, palette, search, moduleUIs),
  site-admin toggleable + env default. No `git revert` needed for a rollback.

---

## 3. Phases (each phase ships green; owner checkpoints between phases)

Gate rule (every phase, non-negotiable): **eslint 0 · vitest green (incl. new
specs) · e2e parity matrix row GREEN on BOTH routes · console-canary green ·
axe-core zero critical/serious on the touched surface · production build +
deploy + verify label === HEAD.**

### P0 — Foundations & contract (no visual change)
- `/editor/:ProjectId` route (render today's shell at the new URL; both routes
  identical — proves route/auth/flags wiring with zero risk).
- Editor **parity matrix** (`tests/e2e/parity/legacy/editor-*.yaml`) extracted
  from source + the 73 editor spec files: panes, toolbar actions, every modal,
  editor-core behaviors (keybindings, search, math, autocomplete, review
  decorations, LLM actions, module UIs), plus *link contracts* (`/s/:id`, deep
  links `/project/:id/...`). → this matrix IS the "no functionality lost"
  contract; same mechanism as the hub gate (`parity/check.mjs` equivalent for
  the editor: `tests/e2e/editor/check.mjs`, CI job `editor-gate`).
- Test-debt wave #1: **share-project-modal, clone-project-modal, command-palette,
  settings modal** get baseline component specs **against the legacy code first**
  (freeze behavior before touching it).
- a11y baseline: axe-core scan of `/project` editor surfaces (before-numbers to
  judge after-numbers).
- Exit: `/editor/:ProjectId` opens the working editor (e2e: open/compile/PDF/keybindings
  pass on the NEW route), matrix + gate wired in CI.

### P1 — Design tokens & theme
- Editor-v2 theme wiring (light + dark), CSS variable bridge, typography scale,
  focus rings, loading-screen parity, font/icon unification (Material Symbols
  slice for the editor like the hub allowlist).
- Exit: `/editor` visually ≈ today (tokens ready), theme toggle works both ways,
  canary green.

### P2 — Toolbar + rail (the "frame")
- Mantine: project title (inline edit + rename flow), share button, compile
  button + dropdown, export (incl. conversion flow), download, duplicate,
  review-mode, history button, online users, change-layout, offline indicator,
  tags; rail tabs/panels/resize, account menu, help, keyboard-shortcuts,
  contact-us. Buttons/Menus/Tooltips via Mantine; behaviors unchanged.
- Exit: toolbar + rail matrix rows green; all existing toolbar e2e pass on `/editor`.

### P3 — Core modals
- generic-confirm / generic-message / request-access / out-of-sync /
  unable-to-sync / force-disconnected / unsaved-docs / project-converted /
  diff-viewer → Mantine modals (one-by-one with parity rows).
- Exit: every modal row green on both routes; modal a11y (dialog role, focus
  trap, ESC) verified in specs.

### P4 — The big three: Share, Clone, Settings ⚠ largest phase, splits if needed
- Share-project modal (invite flow, roles, email, SaaS-gated parts hidden) +
  Clone-project + **project-settings** (compile form, TeX Live format, fonts,
  references, project tags, project options — 69 files) + word-count +
  hotkeys + publish (kept out: saas-only) + export modal.
- Test debt repaid in-phase (baseline specs from P0 get Mantine-variant specs
  added, asserting identical API calls: `/project/:id/share`, clone POST,
  settings PUTs…).
- Exit: share/clone/settings matrix rows green; invite round-trip e2e green.

### P5 — Panes
- file tree (context menus, bulk-select, move/rename/delete, search), review
  panel (add/edit/resolve, mentions), history (diff view, revert), chat
  (messages, mentions, markdown), outline, references/bibtex pane (import/export
  bib), logs (compile log pane), online panel.
- CM6 decorations stay untouched; only panes around them.
- Exit: pane matrix rows green; review round-trip e2e (legacy ↔ hub parity style).

### P6 — Editor-core chrome (highest risk, done early-tests)
- command menu (toolbar dropdown), symbol palette, floating menu (selection
  actions), math-preview tooltip, autocompletion menus, search/replace UI
  (lifted to Mantine popover driven by CM6 search state), table generator,
  equation-AI modal, editor-survey slot.
- Rule: **each item ships only with its behavior test passing on both routes**
  (keybindings spec is the guardrail; every CM6 action gets a click-through e2e
  row in the matrix).
- Exit: keybindings + math + search + LLM-inline e2e green on `/editor`.

### P7 — Module UIs inside the editor
- python-runner (tab + output + toasts), LLM (ask-ai, compile-fix, BYO
  settings, grammar panel), zotero/mendeley import modals, webdav/dropbox cards,
  github sync widget, linked files, diagram visual editor entry.
- Exit: module e2e suites (byo-llm, zotero, python-runner, grammar) green on
  `/editor`.

### P8 — Polish & hardening
- Full a11y pass (axe zero critical/serious, all editor surfaces, light + dark),
  i18n pass (no raw keys, screen-reader labels), keyboard-only walkthrough
  (full editor usable by keyboard — recorded as e2e), bundle/initial-load
  budget (measure P0 baseline; must not regress), dark-mode audit,
  **full matrix: 100% rows green on both routes**.
- Exit: owner go/no-go on retirement.

### P9 — (only after owner decision) retirement/redirect
- `/project/:ProjectId` → `/editor/:ProjectId` redirect (or keep both alive —
  owner's call), internal link generators updated (project list, hub links,
  invite emails, `external-probe`), deep-link audit (`/s/:id`, `/project/:id/
  folder/...`) — deep links keep working in ALL cases (redirect, not rewrite).

---

## 4. Testing — "cover everything while we are at it"

Layered, and **every renovation commit touches tests before/with code**:
1. **Component (vitest)**: every Mantine variant comes with specs asserting the
   *same API calls, same DOM contracts (aria), same keyboard behavior* as the
   legacy baseline frozen in P0. Test-debt table above is repaid in-phase —
   nothing renovated without a spec.
2. **Editor core (vitest, existing + expanded)**: CM6 extension behavior
   (keymaps, math, autocomplete, review decorations) — existing 21 specs stay the
   oracle; new chrome specs assert they still fire.
3. **Backend (existing unit suites)**: unchanged API ⇒ existing suites remain
   the contract; add suites for any new flag routes (`/editor` controller unit:
   auth, role, flag wiring).
4. **E2E parity matrix**: `tests/e2e/parity/legacy/editor-*.yaml` + new
   `specs/editor/` suite mirroring the hub pattern — each row tested **on both**
   `/project` and `/editor`; `editor/check.mjs` gate = **100% covered rows green
   on both routes** (CI `editor-gate` job).
5. **E2E behavior suites (must stay green each phase)**: smoke,
   editor-console-canary, keybindings, grammar, byo-llm, zotero, python-runner,
   notifications, sync-graceful, external-probe — parameterized to run against
   `/editor` too from P0.
6. **A11y gate**: axe-core spec over editor surfaces (`/editor`: toolbar, rail,
   each modal open, each pane) — zero critical/serious, light + dark.
7. **Perf regression**: P0 captures initial-load + TTI numbers; P8 asserts no
   regression beyond budget (editor is the most perf-sensitive surface we have).

## 5. Risks & mitigations
| risk | mitigation |
|---|---|
| OT/sync regression from re-rendering shell | core modules (connection, editor-manager) untouched; surface flags; sync-graceful e2e gate |
| CM6 ↔ Mantine DOM interop | interop rule (§2): Mantine outside CM root; inside-CM chrome only in P6, behavior-test first |
| modal focus-trap / ESC regressions | per-modal a11y specs in P3/P4 before swap |
| deep links (emails, `/s/:id`) | P9 redirect-only, never rewrite; external-probe e2e |
| bundle / first load bloat | perf budget in P0 + P8 gate; shared theme, no duplicate icon set (reuse Material Symbols slice) |
| settings modal (69 files) complexity | split into sub-waves (compile | references | options | tags) with per-subwave gates |
| owner design intent drift during build | owner checkpoint per phase; P2 "frame" is the first visible checkpoint — sign-off before panes |

## 6. Sequencing note
Phase 0 is **zero-visual** and safe to run while you review the `/hub` design —
it only adds a route, a test skeleton, and baseline specs. Suggested order:
**P0 → (your design review) → P1 → P2 (first visual checkpoint) → …**.
Honest size estimate: the editor is ~3-4× the surface area of the `/hub`
program (12 panes/modals-groups vs 13 leaves), expect **P0-P9 ≈ 10-14 waves**
at the current wave cadence, each independently shippable and rollback-safe.
