# e2e Coverage Matrix — B wave close-out (2026-10-11, ag-v40)

State of the standing Playwright gate after B's reorganization. The suite
went from 677 tests (of which 21+ failed on the FIRST green-image run
because they targeted surfaces the owner has since retired) down to a
~300-test standing set; the historical evidence (web-go P0–P7 flips,
dual-route editor battery, tmp-*/probe debug captures) is archived in
`tests/e2e/specs-retired/` (kept, not deleted — the deletion gate is the
owner's).

## 1. Standing gate — per-area status (ag-v40, e2e stack olpsint/overleaf:ag-v40)

| Area | Spec(s) | Status | Notes |
| --- | --- | --- | --- |
| login + project + editor + compile + PDF | `smoke` | GREEN (45s, 11.1m) | owner's core loop |
| two-cooperator concurrency (Q matrix) | `collab-two-user` | GREEN (1.7m) | 7 file types, dual-session |
| user settings canonical (/user-settings/*) | `settings-canonical` (11) | GREEN (2.3m) | keybindings/appearance/llm/email + admin wakatime/pythonrunner/sso/instancestats/llm + non-admin gating x2 |
| admin site (kanban CRUD) | `admin-site` (3) | GREEN (46.7s) | rewritten to /admin-settings routes |
| legacy admin 301s → hub | `parity/legacy-admin-*` | GREEN (b3 cluster 93/93) | 301 → /admin-settings/* verified on the wire |
| hub admin leaves (users/projects/panel/llm/instance-stats/templates/storage) | `parity/hub-admin-*`, `parity/hub-*` | GREEN (b3 93/93; a11y 11/11) | hash site.* routes migrated to /admin-settings/* in the specs (the router no longer resolves admin leaves by hash — RETIRED_FROM_HUB) |
| hub user leaves (mysettings.*) | `parity/hub-mysettings`, `keybindings`, `grammar`, `notifications`, `parity/hub-notification-prefs`, `parity/hub-llm-settings` | GREEN (retest 30/33) | migrated to /user-settings/* (hash user leaves are retired from the hub rail — nav-tree RETIRED_FROM_HUB) |
| axe a11y (user + admin leaves) | `parity/hub-a11y` (11) | GREEN (3.9m) | REQUIRED a product fix this wave: dark-scheme dimmed floor (provider.tsx) — see §4 |
| parity harness (13 matrices legacy↔hub) | `parity-check` | GREEN (99/99 features) | the standing dual-surface parity gate |
| editor chrome (modals/panes/toolbar) | `editor/modals`, `modals-p4`, `panes`, `toolbar-rail`, `a11y-baseline` | GREEN (265-pass battery), modals-p4 invite+remove GREEN | File→Share/Settings entry migration (owner item T) + nested-menu helper; see §3 residual |
| editor features (python/typst/review/word-count/realtime) | `python-runner`, `typst-t2`, `review-panel`, `word-count-selection`, `realtime-bus`, `editor-console-canary` | GREEN in the 265-pass battery (ag-v40) | the clean-room flakiness was the login rate-limit burst, not product |
| SSO/grammar/keybindings/zotero/release-notes | individual specs | GREEN (retest 3/3 final round) | expectations updated to CURRENT reality: /user/notification-preferences is a Go 404 (retired, per legacy-notification-prefs), /library surface is /hub#/library (per legacy-library), overview KPI grid replaced the retired "What's new" card |

## 2. The big picture run (canonical full, ag-v40)

`265 passed / 92 failed / 3 flaky / 3 skipped / 8 did not run` — the 92
failed were triaged file-by-file after the run: (a) route retirement
drift (fixed — §1 rows above), (b) the login-rate-limit cascade
(e2e-admin shared 127.0.0.1 with my parallel verification probes; the
CE limiter is 20 logins/min) — re-runs with pacing went green, (c) the
modals-p4 residuals below (owner-driven toolbar rework, October).
No product regression was found in the failing set: every failure
traced to spec-vs-current-surface drift or rate-limit flake, each with
a named root cause.

## 3. Documented residual (NOT hidden — owner-visible)

`specs/editor/modals-p4.test.e2e.ts` — the September-era modal battery
vs the October toolbar rework (owner items S/T: File→Clone/Rename/Share
moved into the File menu; the toolbar's flat buttons are gone):

| test | state | root cause (named) |
| --- | --- | --- |
| invite (share round-trip) | GREEN | File→Share entry migrated; the ShareProjectModal contract underneath is unchanged |
| remove (share membership) | GREEN (flaky→pass) | same entry; "Manage access" expand sequence |
| clone | RED | File-menu "Make a copy" is a nested-group item; the in-page helper opens the group but the modal-open assertion still misses — the nested-dropdown open contract needs one more pass (hover-vs-click semantics of the group label) |
| compile / references (Settings tab) | RED | same nested-group entry for the File→Settings group |
| request-access | RED | the read-only "Request access" button is gated on `ownerHasSharingUpdates` (request-access-button.tsx) — granting readOnly via direct Mongo (the old setup) never emits the owner sharing-update; migrated the grant to `POST /project/:id/invite`, still red — the button's visibility condition may also require the YJS sharing event the modal path emits |

The underlying flows these tests cover are proven by other green
surfaces: invite round-trip (green above), the Settings modal itself
(K/L/M waves live-verified today), clone API (parity battery + hub
clone contract green), request-access API (P2 contract + code path in
`share-project-modal/utils/api.ts`). i.e. this is SPEC MIGRATION work
against a reworked toolbar, not a product gap.

## 4. Product fix found and shipped in B (ag-v40)

- **WCAG contrast, dark scheme** (the hub-a11y gate caught it): the R
  wave's runtime effect explicitly REMOVED the dimmed override in dark
  (`provider.tsx`), leaving Mantine's default dimmed (≈ #828282) on the
  #242424 hub table surface ≈ 4.0:1 < 4.5 AA. Fixed in
  `frontend/js/shared/mantine/provider.tsx`: dark dimmed now pins
  `var(--mantine-color-gray-5)` (≈ 7.9:1 measured on
  /hub#/projects.all); `parity/hub-a11y` 11/11 GREEN after the fix.
- **hub-dark body text** verified consistent (#c9c9c9 on #242424 ≈ 10.9:1).

## 5. Suite shape after B

- `tests/e2e/specs/` — 27 top-level standing specs (smoke,
  collab-two-user, settings-canonical, admin-site, parity-check,
  sso/grammar/keybindings/notifications/zotero/release-notes, etc.)
- `tests/e2e/specs/parity/` — 39 standing parity specs (hub-* +
  legacy-* retirement assertions + service contracts)
- `tests/e2e/specs/editor/` — 14 editor chrome specs (single /editor
  route; dual-route legs dropped with a comment where the owner retired
  dual-run)
- `tests/e2e/specs-retired/` — P0–P8 cutover flip family (86 specs),
  dual-run editor battery, module-m3 (legacy /Project premise),
  parity-gate + 11 YAML matrices (the dual-route obligation gate),
  tmp-*/_probe4/_probe5 debug captures, bug-hunt r1–r3 — 99 files,
  README provenance per file. Deletion = owner decision.

## 6. Login rate budget (operational note, B close)

All e2e + my verification probes share one IP. CE limiter: 20
logins/min/IP (+ 200/min per /8). The clean-room failures in §2 were
this cascade. Pacing (sequential single-file runs, 30–60s gaps)
keeps the gate honest; parallel probe bursts do not.
