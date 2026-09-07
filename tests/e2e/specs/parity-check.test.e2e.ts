import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

/**
 * PARITY GATE (phase 4 enforcer; available from phase 0 onward).
 * Runs tests/e2e/parity/check.mjs against the matrices in tests/e2e/parity/legacy/
 * and fails this test if any legacy feature is unmapped or tested on only one side.
 *
 * Exit code 3 = matrices not ready yet (phase 0 in progress) → skipped, not failed.
 */
const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..', '..')
const CHECK = path.join(ROOT, 'tests/e2e/parity/check.mjs')

test('parity gate: 100% of legacy functionality mapped + tested (legacy AND hub)', async () => {
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
    test.skip(true, 'matrices not complete yet (phase 0 in progress)')
    return
  }
  expect(code, `parity gate gaps:\n${out}`).toBe(0)
})
