import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// i18n sanity for the python-runner module (same guard pattern as
// modules/webdav/test/unit/i18n-sanity.test.mjs): every key the UI renders
// must exist in en.json with a non-empty value and be known to
// extracted-translations.json (the extraction tooling's source of truth).
// The caveat string (owner requirement 2026-09-06) is covered explicitly.

const here = path.dirname(fileURLToPath(import.meta.url))
const webRoot = path.resolve(here, '../../../../../../../') // repo root (7 up from test/unit/src; locales moved to root in the reorg)
const enPath = path.join(webRoot, 'locales/en.json')
const extractedPath = path.join(webRoot, 'frontend/extracted-translations.json')

const en = JSON.parse(fs.readFileSync(enPath, 'utf8'))
const extracted = JSON.parse(fs.readFileSync(extractedPath, 'utf8'))

const MODULE_KEYS = [
  'run_python_code',
  'stop_python_execution',
  'loading_python_runtime',
  'run_current_script_to_see_output',
  'python_run_code_caveat',
]

describe('python-runner i18n sanity', () => {
  it('all module UI keys exist in en.json with non-empty values', () => {
    for (const k of MODULE_KEYS) {
      expect(typeof en[k], `en.json key missing/empty: ${k}`).toBe('string')
      expect(en[k].length, `en.json empty value: ${k}`).toBeGreaterThan(0)
    }
  })

  it('all module UI keys are present in extracted-translations.json', () => {
    for (const k of MODULE_KEYS) {
      expect(k in extracted, `missing extracted-translations key: ${k}`).toBe(true)
    }
  })

  it('no module key uses {{ ... }} interpolation syntax in en.json', () => {
    const offenders = MODULE_KEYS.filter(k => String(en[k] ?? '').includes('{{'))
    expect(offenders).toEqual([])
  })

  it('the run-code caveat explains the browser-local execution', () => {
    const v = String(en.python_run_code_caveat ?? '')
    expect(v.toLowerCase()).toContain('browser')
    expect(v.toLowerCase()).toContain('only run code you can read')
  })
})
