import { describe, it, expect } from 'vitest'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// i18n chain guard (owner 2026-09-07, "Forgejo-style i18n linter for LibreLeaf").
// Runs the linter and requires a clean result: no key may be (a) used in code
// but missing from locales/en.json (raw key visible in UI), (b) used in code
// + present in en.json but absent from frontend/extracted-translations.json
// (the webpack translations-loader silently prunes it from the bundle — the
// exact bug class that shipped 2026-09-07: mendeley_not_configured and 400+
// other keys rendered as raw identifiers), or (c) left as a dead entry in
// extracted-translations.json without an en translation.
//
// See services/web/scripts/translations/i18n-lint.js for the checks and
// services/web test/unit/src/i18n-lint.test.mjs for this guard.

const here = path.dirname(fileURLToPath(import.meta.url))
const webRoot = path.resolve(here, '../../..') // services/web
const LINTER = path.join(webRoot, 'scripts/translations/i18n-lint.js')

describe('i18n chain (Forgejo-style linter)', () => {
  it(
    'every translation key in the code reaches the runtime bundle',
    () => {
      let out = ''
      try {
        out = execFileSync(
          process.execPath,
          [LINTER],
          { cwd: webRoot, encoding: 'utf8', timeout: 180_000 }
        )
      } catch (e) {
        const combined = `${e.stdout || ''}${e.stderr || ''}`
        fail(`i18n-lint reported findings:\n${combined.slice(-4000)}`)
      }
      expect(out).toContain('0 error(s)')
    },
    240_000
  )
})
