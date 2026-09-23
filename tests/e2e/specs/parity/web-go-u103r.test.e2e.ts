// U10.3r — private-API (basic-auth) document trio parity: Node == Go.
//
// Routes (Node web profile = the drop-in Go replaces; the same path on the
// 'api' profile :3000 differs — 401 everywhere — and is NOT this gate's
// oracle; see WEB_GO_PLAN.md cutover section):
//   GET  /project/:Project_id/doc/:doc_id          → 401/302 (no auth)
//   POST /project/:Project_id/doc/:doc_id          → 403 (no-csrf POST)
//   POST /project/:Project_id/doc/:doc_id/changes/reject → 403 (no-csrf POST)
//
// Three-leg gate inside ol-e2e-overleaf-1:
//   Node (:4000, leg 1)  ==  Go (:4010, leg 2)  ==  Node (:4000, leg 3)
// The matrix pins the reachable auth-fail / no-csrf surface (full header
// set incl. the helmet bundle, ETag-normalized, set-cookie name-only).
//
// NOTE: the valid-credential surface (200 JSON / "Not Found") is NOT
// compared here — the sandbox network guard drops any request carrying the
// WEB_API password (→ bare 400) to Node AND Go alike, so the app-level
// valid path never runs in e2e in this stack. It is covered in-process by
// the Go unit test core.TestAPIBasicGateMatrix (valid-cred → 200 "Not
// Found"). The reachable surface above is byte/structurally identical.

import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dir = path.dirname(fileURLToPath(import.meta.url))
const overleafC = 'ol-e2e-overleaf-1'
const MATRIX = path.resolve(__dir, 'u103r-matrix.cjs')

function dexeStrict(c: string, cmd: string): string {
  const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], {
    encoding: 'utf8',
    stdio: 'pipe',
    maxBuffer: 64 * 1024 * 1024,
    timeout: 60000,
  })
  return out
}

function leg(legNo: number): string {
  const raw = dexeStrict(overleafC, `node /tmp/u103r-matrix.cjs ${legNo} 2>/tmp/u103r-err-${legNo} || { echo U103R-LEG-ERR; cat /tmp/u103r-err-${legNo}; exit 1; }`)
  const lines = raw.split('\n').map((l) => l.trimEnd()).filter((l) => l.length > 0)
  if (lines.some((l) => l.startsWith('U103R-LEG-ERR') || l.includes('ERR|'))) {
    throw new Error(`leg${legNo} produced an error row:\n${raw.split('\n').slice(0, 12).join('\n')}`)
  }
  return lines.join('\n')
}

test('u103r: private-API document trio Node==Go==Node (3 legs)', async () => {
  execFileSync('docker', ['cp', MATRIX, `${overleafC}:/tmp/u103r-matrix.cjs`], { timeout: 30000, stdio: 'ignore' })
  const l1 = leg(1)
  const l2 = leg(2)
  const l3 = leg(1)

  test.info().annotations.push({ type: 'u103r-leg', description: l1 })
  test.info().annotations.push({ type: 'u103r-go', description: l2 })

  const rows: string[] = ['u103r doc-trio parity (leg1==leg3 determinism, leg1==leg2 Node==Go):']
  const l1L = l1.split('\n'), l2L = l2.split('\n'), l3L = l3.split('\n')
  if (l1L.length !== l2L.length || l1L.length !== l3L.length) {
    rows.push(`  CASE COUNT MISMATCH: l1=${l1L.length} l2=${l2L.length} l3=${l3L.length}`)
  }
  const n = Math.max(l1L.length, l2L.length, l3L.length)
  const bad: string[] = []
  for (let i = 0; i < n; i++) {
    const a = l1L[i] ?? '<missing>', b = l2L[i] ?? '<missing>', c = l3L[i] ?? '<missing>'
    if (a !== b || a !== c) bad.push(`  case[${i}]:\n    node1: ${a.slice(0, 400)}\n    go   : ${b.slice(0, 400)}\n    node2: ${c.slice(0, 400)}`)
  }
  for (const line of l1L) rows.push('   ' + line.slice(0, 140))
  rows.push(`u103r summary: cases=${l1L.length} diffs=${bad.length}`)
  console.log(rows.join('\n'))
  expect(bad.join('\n'), bad.join('\n') || 'Node==Go==Node — u103r document trio parity GREEN').toBe('')
})
