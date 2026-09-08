import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

/**
 * EDITOR RENOVATION GATE (phase ratchet enforcer — EDITOR_RENOVATION_PLAN.md).
 *
 * Runs tests/e2e/editor/check.mjs against the matrices in
 * tests/e2e/parity/editor/*.yaml with the current phase from
 * tests/e2e/editor/PHASE. A row is due when its `due` phase <= current phase;
 * every DUE row must be covered by live tests on BOTH the legacy route and
 * the /editor route. Pending (later-phase) rows are reported, not failing.
 *
 * Ratchet rule: PHASE may only move forward, and only AFTER the tests for the
 * new phase exist and pass (gate + e2e). Exit 3 = not ready → skipped.
 */
const HERE = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(HERE, '..', '..', '..', '..')
const CHECK = path.join(ROOT, 'tests/e2e/editor/check.mjs')

test('editor gate: 100% of the due functionality tested on BOTH /Project and /editor', async () => {
  let code = 1
  let out = ''
  try {
    out = execFileSync('node', [CHECK], { encoding: 'utf8', cwd: ROOT, timeout: 120000 })
    code = 0
  } catch (e) {
    code = e.status ?? 1
    out = `${e.stdout || ''}${e.stderr || ''}`
  }
  console.log(out.trimEnd())
  if (code === 3) {
    test.skip(true, 'editor matrices not ready yet (P0 in progress)')
    return
  }
  expect(code, `editor gate gaps:\n${out}`).toBe(0)
})
