// U-API — private-API (basic-auth) document trio + API-profile route
// selection parity, API profile.
//
// The API profile (ENABLED_SERVICES=api, the service web-api-overleaf serves
// on :3000) has a wire DISTINCT from the web profile (pinned by
// web-go-u103r): unauth/wrong → 401 for ANY Accept (no 302/login), POST → 401
// (no csrf 403), ghost → 404 plain "Not Found", X-Powered-By present, no
// session cookie, no helmet baseline.
//
// This gate ALSO pins API-profile route/profile selection (Node :3000 does not
// mount webRouter): web-only routes (/, /project/:id, /project/:id/members,
// /entities, unknown paths) → 404 with the Express finalhandler wire
// (x-powered-by: Express, x-content-type-options: nosniff,
// content-security-policy: default-src 'none'), NOT the web profile's 301/302
// login bounce.
//
// Three-leg gate inside ol-e2e-overleaf-1:
//   Node api (:3000, leg 1)  ==  Go api (:4011, leg 2)  ==  Node api (:3000, leg 3)
// The Go api leg is the sv-managed shadow service `web-go-api-overleaf`
// (ENABLED_SERVICES=api, 127.0.0.1:4011). The matrix pins the full
// unauth/wrong + valid ghost/real wire (header set + body, ETag-normalized,
// set-cookie name-only, helmet-bundle joined).
//
// The valid-cred POST (setDocument) is NOT compared — its status is
// state-dependent in this stack (Node :3000 → 500, an env/docstore condition,
// not the profile wire). See uapi-doc-matrix.cjs.

import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dir = path.dirname(fileURLToPath(import.meta.url))
const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const MATRIX = path.resolve(__dir, 'uapi-doc-matrix.cjs')
const SEED_NAME = 'uapi-wire'
const CANON = ['UAPI WIRE L1', 'UAPI WIRE L2']
const OWNER = '6aa4b8b573ef0e5094f4cbc0'

function dexe(c: string, cmd: string, capture = false): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], {
    encoding: 'utf8',
    stdio: capture ? 'pipe' : 'ignore',
    maxBuffer: 1 << 28,
    timeout: 60000,
  }) || ''
}
function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], {
    encoding: 'utf8',
    stdio: 'pipe',
    maxBuffer: 64 * 1024 * 1024,
    timeout: 60000,
  })
}

// ---- seeding (direct mongo insert + docstore entry) ---------------------------
function seedState(): { A: string; DA: string } {
  const seedJs = `
    db.projects.deleteMany({name:"${SEED_NAME}"});
    const uid = ObjectId("${OWNER}");
    const dA = new ObjectId();
    db.projects.insertOne({_id:new ObjectId(), name:"${SEED_NAME}", owner_ref:uid, publicAccesLevel:"private",
      version:0, rootFolder:[{name:"", _id:new ObjectId(), docs:[{_id:dA, name:"doc.tex"}], fileRefs:[], folders:[]}]});
  `
  fs.writeFileSync('/tmp/uapi-wire-seed.js', seedJs)
  execFileSync('docker', ['cp', '/tmp/uapi-wire-seed.js', `${mongoC}:/tmp/uapi-wire-seed.js`], { stdio: 'ignore' })
  dexe(mongoC, 'mongosh --quiet sharelatex /tmp/uapi-wire-seed.js')
  let st = null as { A: string; DA: string } | null
  for (let att = 1; att <= 4 && !st; att++) {
    const line = dexe(mongoC, `mongosh --quiet sharelatex --eval 'const p=db.projects.findOne({name:"${SEED_NAME}"}); if(p)print(JSON.stringify({A:String(p._id), DA:String(p.rootFolder[0].docs[0]._id)}))'`, true)
      .trim().split('\n').pop() || ''
    try {
      const j = JSON.parse(line)
      if (/^[0-9a-f]{24}$/.test(j.A) && /^[0-9a-f]{24}$/.test(j.DA)) st = j
    } catch { /* not ready */ }
    if (!st) dexe(mongoC, 'sleep 1')
  }
  if (!st) throw new Error('seed not visible after 4 attempts')
  const j = JSON.stringify(CANON)
  dexe(overleafC, `for ATT in 1 2 3 4; do
    CODE=$(curl -s -o /tmp/uapi-dsb -w "%{http_code}" -X POST http://127.0.0.1:3016/project/${st.A}/doc/${st.DA} -H 'content-type: application/json' -d '{"lines":${j},"version":0,"ranges":{}}')
    [ "$CODE" = "200" ] && break; sleep 1
  done
  [ "$CODE" = "200" ] || exit 1`)
  return st
}

// Ensure the sv-managed Go api shadow is up (idempotent; no-op if already running).
function ensureGoApi(): void {
  dexe(overleafC, 'sv up web-go-api-overleaf >/dev/null 2>&1 || true; sleep 1')
}

function leg(legNo: number, pj: string, doc: string): string {
  const raw = dexeStrict(overleafC, `UAPI_PJ=${pj} UAPI_DOC=${doc} node /tmp/uapi-doc-matrix.cjs ${legNo} 2>/tmp/uapi-err-${legNo} || { echo UAPI-LEG-ERR; cat /tmp/uapi-err-${legNo}; exit 1; }`)
  const lines = raw.split('\n').map((l) => l.trimEnd()).filter((l) => l.length > 0)
  if (lines.some((l) => l.startsWith('UAPI-LEG-ERR') || l.includes('ERR|'))) {
    throw new Error(`leg${legNo} produced an error row:\n${raw.split('\n').slice(0, 12).join('\n')}`)
  }
  return lines.join('\n')
}

let STATE = null as { A: string; DA: string } | null

test('seed project uapi-wire (+docstore entry)', async () => {
  STATE = seedState()
  expect(STATE.A).toMatch(/^[0-9a-f]{24}$/)
  expect(STATE.DA).toMatch(/^[0-9a-f]{24}$/)
})

test('uapi: private-API doc trio (API profile) Node api :3000 == Go api :4011 == Node', async () => {
  if (!STATE) STATE = seedState()
  ensureGoApi()
  execFileSync('docker', ['cp', MATRIX, `${overleafC}:/tmp/uapi-doc-matrix.cjs`], { timeout: 30000, stdio: 'ignore' })
  const l1 = leg(1, STATE.A, STATE.DA)
  const l2 = leg(2, STATE.A, STATE.DA)
  const l3 = leg(1, STATE.A, STATE.DA)

  test.info().annotations.push({ type: 'uapi-leg', description: l1 })
  test.info().annotations.push({ type: 'uapi-go', description: l2 })

  const rows: string[] = ['uapi doc-trio (API profile) parity (leg1==leg3 determinism, leg1==leg2 Node==Go):']
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
  rows.push(`uapi summary: cases=${l1L.length} diffs=${bad.length}`)
  console.log(rows.join('\n'))
  expect(bad.join('\n'), bad.join('\n') || 'Node==Go==Node — uapi document trio (API profile) parity GREEN').toBe('')
})
