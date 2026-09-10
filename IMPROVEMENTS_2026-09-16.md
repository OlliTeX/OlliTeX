# OlliTeX — prioritized improvement suggestions (2026-09-16)

Compiled from ~3 weeks of waves (port → hub → editor → modules → audits →
removals). Evidence references are commit SHAs / spec names / files in this
repo. Ordered by **owner-visible value ÷ effort**, split into three tiers.

## P0 — ship with the current release (small, high value)

1. **API-level authorization audit on admin endpoints** *(security)*
   Observed: as a non-admin ("tpladmin"-level) user, `POST /admin/llm/settings
   {}` returns **200** — the hub UI gates the card, but the JSON API has no
   4xx gate (inherited stack behavior). Several removed pages relied on the
   same UI-gating. Recommendation: add an `assertAdmin` guard pass over
   `AppRouter` `/admin/*` + `/admin/site-settings*` + LLM admin routes with unit
   tests proving non-admin → 403. Cheap (pattern: `isAdmin` check already
   exists in controllers), removes a real data-leak surface.
2. **Unblock SMTP (owner action) → then enable: password-reset e-mails,
   Hub #18 "test e-mail" button, and registration confirmation**
   The UI for all three exists (hub `site.email.test`, `/login` reset flow,
   registration page); they are currently inert without real SMTP. One config
   entry (`server-ce/config/env.sh`) + one e2e SMTP-capture test (mock
   relay).
3. **Fix the two known test flakes so `make ci` is noise-free**
   (a) `TpdsProjectFlusher "should flush the project from the doc updater"`
   times out under parallel load (passes standalone — 19/19 ×3). Add an explicit
   socket mock / raised timeout in that spec.
   (b) Login-spec console 401 noise from `GET /system/messages`
   (pre-existing since `36a3331ea2`) — assert *expected* 401s are filtered in
   the canary, not asserted zero-error.
4. **Rename the dev-server npm script** — `yarn webpack` in
   `services/web/package.json` starts the **dev server**; the production gate
   is `yarn webpack:production`. Two sessions in this project hit the orphan
   `webpack serve` on port 3808 (then `EADDRINUSE`). Rename to `webpack:dev`
   (keep `webpack` as an alias that *warns* and runs production), or at least
   add `"start": "webpack:dev"` and document in README §Development.
5. **Kill the 5 pre-existing webpack warnings** (`findDOMNode`,
   `componentWillUpdate`) — React 19.2 is already the runtime; the legacy
   lifecycles are the last `legacy: true`-style debt and block a future
   clean-mode build.

## P1 — next release cycle (medium effort, clear payoff)

6. **Hub-ify the remaining legacy surfaces** — with the 7 legacy pages now
   removed (`b8d91f5322`, `b01af43ac3`), the same pattern applies to
   `/user/mysettings` shell, `/library`, and the legacy `admin/index` landing.
   The parity harness (`tests/e2e/parity/check.mjs`) is already built for
   exactly this: add the matrix, remove the page, keep the APIs. This is the
   strategic direction — one console, zero dead ends.
7. **Full-text project search** — the hub search box filters project names
   today. Adding content search (compile-output + source, simple inverted
   index in the existing Mongo) is the single most-requested class of feature
   in the issues files and the only way the hub scales past ~100 projects.
8. **In-app release notes / "What's new"** — the owner has been accumulating
   change notes in `next_steps.md` + issue files. A small `/hub#/overview`
   card fed by a `docs/RELEASE_NOTES.md` (same source as the wiki) would make
   each wave visible to end users instead of the owner's log.
9. **i18n coverage gate + a second locale** — user-facing leaves are mostly
   translated via `t()` (admin leaves intentionally English — documented).
   Add a vitest gate: every user-facing section must resolve through
   `extracted-translations.json`; then ship `es` (Bremen/Norderney readers)
   from the existing `locales/*.json` scaffolding.
10. **Typst ergonomics wave** — the pinned-image-safe subset works
    (`TYPST_INTEGRATION_PLAN.md`, `1cf1f62f53`); next: full syntax coverage,
    user-selectable figure/palette presets in the menu, and a compile-time
    comparison (typst vs latex p95) in instance stats.
11. **Bundle diet + code-split the hub** — `public/` ships 762 MB (all
    module bundles in one manifest; the e2e sync copies the whole thing).
    Split hub/admin/typst/llm modules into lazy chunks; expect a materially
    faster first load and a smaller `docker cp` surface.

## P2 — backlog (strategic, larger)

12. **CI for the whole story** — `.github/workflows/e2e.yml` exists; extend to:
    `make ci` → `make image` → push image with tag policy → healthcheck →
    archive the `AUDIT`/parity artifacts. Then deployments become a merge,
    not a machine session.
13. **Metrics for admins** — the `libraries/metrics` plumbing exists but is
    unwired; surface per-feature usage (compiles, LLM tokens, compiles by
    TeX image) on `site.instance` with the existing `stats-chart.tsx`.
14. **Template sharing** — cross-instance template publish/share is a privacy
    decision; even a "share project as template link within this instance"
    would close the biggest workflow gap in template workflows.
15. **Editor: error-log UX + offline note** — compilation error logs could
    offer "explanation" (reuse the LLM grammar surface, admin-gated) and the
    editor should show a clear offline/compile-queue banner (currently the
    queue state is only in the compile drop-down).
16. **Migrate the remaining `@overleaf/*` package renames +
    `sharelatex` DB name** — only with a data-migration script; high risk /
    low user value, keep as a conscious deferral (recorded in BRANDING.md).

## Explicit non-goals (to keep the scope honest)

- No billing / subscriptions / team features (SaaS boundary already purged —
  `TODO-2e4a414c`).
- No server-side render framework swap; the pug + React module model works.
- No multi-tenant SaaS mode; single trusted institution per instance.
