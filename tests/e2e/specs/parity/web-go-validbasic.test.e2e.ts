// WEB-profile VALID-BASIC parity — Node web :4000 == Go web :4010 == Node web :4000.
//
// Node requireGlobalLogin (AuthenticationController): an Authorization header
// present → basic decision (valid → authenticated & dispatched, invalid → 401);
// absent → session (none → 302/401). The gate pins the valid-basic web wire:
// valid→404-page (non-web routes) / 403-restricted (/project/:id, JSON
// {"message":"restricted"} byte-exact), wrong→401, no-auth→302/401.
//
// Three legs inside ol-e2e-overleaf-1: Node web (:4000, leg 1) == Go web
// (:4010, leg 2) == Node web (:4000, leg 3). The Go shadow is the sv-managed
// `web-go-overleaf` (ENABLED_SERVICES=web, 127.0.0.1:4010). Only the P4.13
// fixture project id (stable) is used — no seeding (all legs are auth-based).
import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dir = path.dirname(fileURLToPath(import.meta.url))
const overleafC = 'ol-e2e-overleaf-1'
const MATRIX = path.resolve(__dir, 'validbasic-matrix.cjs')

function dexe(c: string, cmd: string, capture = false): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], {
    encoding: 'utf8',
    stdio: capture ? 'pipe' : 'ignore',
    maxBuffer: 1 << 28,
    timeout: 60000,
  }) || ''
}

function ensureGoWeb(): void {
  dexe(overleafC, 'sv up web-go-overleaf >/dev/null 2>&1 || true; sleep 1')
}

function leg(legNo: number): string {
  const raw = dexe(overleafC, `node /tmp/validbasic-matrix.cjs ${legNo} 2>/tmp/vb-err-${legNo} || { echo VB-LEG-ERR; cat /tmp/vb-err-${legNo}; exit 1; }`)
  const lines = raw.split('\n').map((l) => l.trimEnd()).filter((l) => l.length > 0)
  if (lines.some((l) => l.startsWith('VB-LEG-ERR') || l.includes('ERR|'))) {
    throw new Error(`leg${legNo} produced an error row:\n${lines.slice(0, 12).join('\n')}`)
  }
  return lines.join('\n')
}

test('web valid-basic wire: Node web :4000 == Go web :4010 == Node web :4000', async () => {
  ensureGoWeb()
  fs.copyFileSync(MATRIX, '/tmp/validbasic-matrix.local')
  execFileSync('docker', ['cp', '/tmp/validbasic-matrix.local', `${overleafC}:/tmp/validbasic-matrix.cjs`], { timeout: 30000, stdio: 'ignore' })

  const l1 = leg(1)
  const l2 = leg(2)
  const l3 = leg(1)
  const sum = (s: string) => s.split('\n').find((l) => l.startsWith('validbasic summary:')) || 'n/a'
  const r1 = l1.split('\n').filter((l) => !l.startsWith('validbasic summary:'))
  const r2 = l2.split('\n').filter((l) => !l.startsWith('validbasic summary:'))
  const r3 = l3.split('\n').filter((l) => !l.startsWith('validbasic summary:'))
  expect(r1.length).toBeGreaterThan(0)
  expect(r1.length, 'case count L1==L3').toBe(r3.length)
  expect(r2.length, 'case count L1==L2').toBe(r1.length)
  const diffs: string[] = []
  for (let i = 0; i < r1.length; i++) {
    if (r1[i] !== r2[i]) diffs.push(`L1(${i}): ${r1[i]}\nL2(${i}): ${r2[i]}`)
    if (r1[i] !== r3[i]) diffs.push(`L1(${i}): ${r1[i]}\nL3(${i}): ${r3[i]}`)
  }
  if (diffs.length) {
    console.error('validbasic diffs (' + diffs.length + '):\n' + diffs.join('\n---\n').slice(0, 4000))
  }
  expect(diffs.length, 'Node web == Go web == Node web (valid-basic matrix)\n' + l1).toBe(0)
  console.log(sum(l1) + ' | diffs=' + diffs.length)
})
