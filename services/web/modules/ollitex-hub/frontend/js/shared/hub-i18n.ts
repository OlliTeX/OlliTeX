// overleaf-lab #11 (2026-09-08): i18n helper for /hub sections.
//
// The hub entry (pages/hub.tsx) initialises the shared i18next instance and
// loads the app locale bundle (`locales/en.json`, FLAT keys, keySeparator off),
// so `useTranslation()` resolves in hub sections. Keys are OPT-IN: until a
// section's strings are added to locales/en.json + extracted-translations.json
// (scripts/translations/i18n-lint.js guards that chain), every call below
// falls back to the English default — the UI can therefore NEVER show a raw
// "hub.xxx" key. That is what makes gradual extraction safe:
//
//   const t = useHubT()
//   <Text>{t('hub.health.title', 'Hub health')}</Text>
//
// Add translations later:
//   1. add "hub.health.title": "…" to services/web/locales/en.json
//   2. run the scanner/extract to include the key in extracted-translations.json
//   3. add translations to other locales/{lang}.json as needed
//
// The hub roadmap (modules/ollitex-hub/ROADMAP.md) tracks the per-section
// extraction backlog.

import { useTranslation } from 'react-i18next'

export type HubT = (key: string, defaultValue: string, vars?: Record<string, unknown>) => string

export function useHubT(): HubT {
  const { t } = useTranslation()
  return (key, defaultValue, vars) => {
    try {
      const out = t(key, { defaultValue, ...(vars || {}) })
      return typeof out === 'string' && out.length > 0 ? out : defaultValue
    } catch {
      return defaultValue
    }
  }
}
