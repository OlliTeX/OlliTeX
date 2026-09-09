# Module Mantine wave — plan (2026-09-10, owner mandate: "the full conversion")

Owner notes in force:
- `/Project` gets retired by the owner **after** the conversion → the
  Mantine variants are the durable path; the legacy variants remain as
  the fallback until retirement (both keep passing until then).
- Invariants from the goal (never break): OT/socket sync, compile/PDF
  pipeline, CM6 extension graph, APIs, `#ide-root` identity, i18n,
  feature flags, dark/light, React #130 (IDE tree never remounts).

## Architecture (already built, reused as-is)

- **Gate**: `useEditorUiVariant()` → `{variant, shellReady, Provider}`;
  `canUseMantineSurface(ctx)` = /editor + shell chunk resolved
  (`features/editor-v2/variant.tsx`, unit-tested pure path rule).
- **Provider**: `OlliTProvider` (shared/mantine/provider.tsx — brand
  theme, live light/dark bridge to `ace.overallTheme`, notifications,
  `@mantine/core/styles.css`) inside the editor Mantine shell; modules
  wrap their Mantine variants in `ctx.Provider` (same pattern as
  `OLModal`).
- **Style bridge**: `editor-v2-tokens.css` maps legacy `--*` tokens onto
  Mantine inside `.ol-editor-mantine`.
- **Proof discipline (per wave)**: webpack + eslint + vitest (incl. new
  unit tests for the variant) + module e2e green on BOTH routes +
  axe zero critical/serious on the touched surfaces + commit → push →
  prod deploy → verify.

## Waves (size-ordered; each wave = one commit + deploy + proven)

| W | Module(s) | What converts | Notes |
|---|-----------|---------------|-------|
| **M1** | webdav | integration card: buttons, form inputs, error alerts → `Button`, `TextInput`, `Alert` | pilot; smallest editor-embedded card; a11y form wrapper kept |
| **M2** | zotero + mendeley | integration cards + import menus the same way | parity-state assertions already exist (P7) |
| **M3** | python-runner | output pane chrome (tabs/alerts/buttons) inside the split view | CM6 interop: Mantine stays OUT of CM root |
| **M4** | languagetool + orcid-picker | grammar settings section; ORCID pick modal | modal branch = OLModal Mantine (done) + Mantine form fields |
| **M5** | symbol-palette + reference-picker | palette rail chrome; pick dialogs | rail identity (`data-rr-ui-event-key`) must be preserved |
| **M6** | template-gallery | gallery cards, pagination, search → Mantine (standalone page) | biggest non-admin user surface (40 files) |
| **M7** | admin-tools | users/projects/site/LLM/templates sections | hub /admin-hub already renders these as Mantine sections — decide embed-vs-rebuild per section (parity-wave pattern) |
| **M8** | remaining standalone pages | notifications prefs, registration-page, library (public), user-activate, bib-import dialogs | Mantine page shells with the shared kit (pagination, confirm-modal, stats chart) |
| **M9** | cleanup after owner retires `/Project` | remove legacy branches behind the gate, drop `editor-v2` variant plumbing, simplify OLModal/OLDropdown dual paths | only after owner deletes `/Project`; deep-link audit first |

## Rules per wave

1. Only swap the **surface** inside the module's existing component —
   keep the component identity, props, and stable classes/selectors the
   e2e matrix relies on (`data-rr-ui-event-key`, `#webdav`,
   `.settings-widget-container`, …).
2. Legacy branch = current code verbatim (zero behavior change on
   /Project until retirement).
3. a11y gate: axe zero critical/serious on both routes (P8 discipline).
4. i18n: no new keys unless required; no raw keys.
5. Gate evidence recorded in the wave commit message + suite names.
