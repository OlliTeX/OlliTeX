{
  "id": "89b7ceba",
  "title": "ADOPT go-i18n — COMPLETE: phase 1 + waves A/B/C (admin + one-time-login are the final wave). 95 keys en+de; gates green; audit=correct.",
  "tags": [
    "owner",
    "i18n",
    "go-i18n",
    "german-canary"
  ],
  "status": "done",
  "created_at": 1790557682705,
  "last_change": 1790665067681,
  "restore": "2026-10-11 from pi-transcripts (board dir lost to rm-rf incident)"
}

**COMPLETE (2026-09-28/29)** — all waves landed + gated:
- Phase 1: go/libraries/i18n (go-i18n v2.6.1) + email seam + `{{var}}` canary `9d586e7929`
- Wave A: navbar + /launchpad (38 strings, German render + en-byte-identity gates) `c461b0ff7f`
- Wave B: shell pages (login/register/logout/404/500/restricted/set-password/sessions) `b6c796f836` — via core.App.PageLocale + views.ShellI18n
- Wave C (FINAL): /admin shell (13 strings: h1/tabs/panels/LLM ON+OFF) + /read-only/one-time-login (5 fragments) `038ae9d577` — full shell union (A+B+C) in one lane; AdminShellParams.I18n seam; technical tokens verified untouched in de.
- Catalog: 95 keys × {en, de}; catalog-integrity gates in views (Go set == en catalog; de resolves all).
Audit against nicksnyder/go-i18n v2.6.1 (owner question): CORRECT (2 deliberate documented deviations: no DefaultMessage — caller-default seam; CE locale policy ignores q-values).
Frontend i18n (React-side locales/) = OUT OF SCOPE (frontend-owned). Owner may add more languages by dropping de-XX.json siblings.
