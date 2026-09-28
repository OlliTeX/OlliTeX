# go/libraries/i18n — extraction & convention notes (docs/go-i18n-evaluation.md §4)

## Catalogs

- `locales/<locale>.json` — flat `{"key": "text"}` OR
  `{"key": {"zero|one|two|few|many|other": "..."}}` (CLDR plural forms —
  nicksnyder core is CLDR, not ICU; form strings are literal).
- `en.json` defines the key space. Translators fill other locales for the
  keys they cover; missing key → en fallback (Localizer chain).
- **Do not** edit the frontend catalogs under repo-root `locales/` (separate
  pipeline, owner scope per the adoption decision).

## Placeholders ({{var}})

Renderer-owned. nicksnyder core treats `{{...}}` as Go-template functions and
would error on unknown names, so every message is loaded with sentinel
delims (`\x00`/`\x01`) — `{{var}}` passes through byte-identical and the
renderer (e.g. emailtemplates' interpolator) substitutes afterwards, exactly
as in the English pipeline. Do not rely on nicksnyder to substitute.

## Adding a key (canary discipline)

1. en.json gets the canonical English bytes (identity to the current
   default string — pin it in a test).
2. de.json (or the target locale) gets the translation.
3. i18n_test.go pins the matrix row (en + de + fallback + unknown-key).

## Seams (adopted order, §3.1)

- E-mail: `emailtemplates.RenderWithT(...)` + `core.App.I18n` (nil =
  English-only = today's bytes).
- Views: `Bundle.T(locale, key, vars)` at view/page-data builders — ~440
  strings, per the §3.1 step-2 list (future slices).
- API error strings: NEVER translated (§3.2 — byte-pinned contract surface).
