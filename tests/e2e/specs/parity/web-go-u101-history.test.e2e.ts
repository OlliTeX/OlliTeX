/**
 * P7 U10.1 parity gate — History web family (WEB_GO_PLAN.md U10.1):
 *   GET  /project/:id/updates                       → 200 (injected users) |
 *                                                  401 | 403 (restricted) |
 *                                                  404 JSON (bad Project_id)
 *   GET  /project/:id/latest-history, /changes      → 200 | 400 since bad |
 *   GET  /project/:id/labels + POST (csrf) +        → 201 | 400 unknown key |
 *        DELETE (404 bad id, 403 nonmember 401)     401 | csrf 403 Forbidden
 *   POST /project/:id/blob/:hash (idempotent)       → 201 then 200
 *   GET  /project/:id/blob/:hash (+HEAD/Range/304)  → 200 no-Content-Length |
 *                                                  404 escaped validation
 *   POST /project/:id/version/:n/zip                → 200 application/zip
 *                                                  attachment (body
 *                                                  mtime-normalized) |
 *                                                  404 JSON | 402 nohist
 *   POST /project/:id/flush                         → 200 | 401 | 403
 *   GET  /project/:id/diff(/doc/:did)               → 200 | 404 JSON (bad did)
 *   PUT  restore_file/revert_file/revert-project    → exact 400 | 401 | 403
 *
 * Node==Go==Node 3-leg gate — U1/U2/U8/U9 idiom. Leg batteries run in the
 * container against 127.0.0.1:4000 (Node) and 127.0.0.1:4010 (Go shadow).
 *
 * Honest scope pin: the VALID-body SUCCESS paths of the three restore routes
 * run Node's RestoreManager (local blobs + Mongo + V2) — ported in U10.5
 * with its own oracle pin. This gate covers everything else in the family.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const BATTERY = `${process.cwd()}/specs/parity/u101-history-matrix.cjs`
const NODE = 'http://127.0.0.1:4000'
const GO = 'http://127.0.0.1:4010'

function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 256 * 1024 * 1024 })
}

function probe(url: string): string {
  let out = ''
  try {
    out = execFileSync('docker', ['exec', overleafC, 'sh', '-c', `curl -s -m 3 -o /dev/null -w "%{http_code}" ${url}`], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    })
  } catch (e: any) {
    out = e && e.stdout ? e.stdout.toString() : ''
  }
  return (out || '').trim().split('\n')[0]
}

async function waitUp(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    if (probe(`${NODE}/status`) === '200' && probe(`${GO}/status`) === '200') return
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error(`services not up (node=${probe(NODE + '/status')} go=${probe(GO + '/status')})`)
}

function flushRateLimiters(): void {
  execFileSync(
    'docker',
    ['exec', 'ol-e2e-redis-1', "sh", "-c", `for k in $(redis-cli --scan --pattern "rate-limit:overleaf-login:*"); do redis-cli DEL \"$k\"; done`],
    { encoding: 'utf8' },
  )
}

const norm = (s: string): string =>
  s
    .replace(/nonce="[^"]*"/g, 'nonce="N"')
    .replace(/ol-csrfToken=[A-Za-z0-9_~-]+/g, 'ol-csrfToken=T')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="C"')
    .replace(/value="[A-Za-z0-9]+"/g, 'value="T"')
    .replace(/overleaf\.sid=[A-Za-z0-9._~\-+%/=]*/g, 'overleaf.sid=X')
    .replace(/Set-Cookie: overleaf\.sid=[^;]+/g, 'Set-Cookie: overleaf.sid=X')
    .replace(/expires=[^;]+/gi, 'expires=E')
    // per-creation identity (each leg creates its own label) — parity is on
    // shape/order/keys; the id+created_at are unique per row by design.
    .replace(/"id":"[0-9a-f]{24}"/g, '"id":"ID"')
    .replace(/"created_at":"[^"]*"/g, '"created_at":"T"')
    // ETag hashes are derived from the (nonce/id-bearing) bodies — normalize
    // the hash while keeping the length prefix (it is body-size parity).
    .replace(/W\/"([0-9a-f]+)-([A-Za-z0-9+\/=]+)/g, 'W/"$1-HASH"')

function runBattery(base: string, _leg: string): any[] {
  const cp = execFileSync('docker', ['cp', BATTERY, `${overleafC}:/tmp/u101-history-matrix.cjs`], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] })
  if (cp) throw new Error(`docker cp failed: ${cp}`)
  const run = dexeStrict(overleafC, `node /tmp/u101-history-matrix.cjs ${base} && cat /tmp/u101-battery-out.json`)
  const start = run.indexOf('BATTERY-JSON:')
  if (start < 0) throw new Error(`battery(${_leg}) produced no output: ${run.slice(0, 400)}`)
  const recs = JSON.parse(run.slice(start + 'BATTERY-JSON:'.length))
  for (const r of recs) {
    r.body = norm(r.body || '')
    r.headers = { ...r.headers }
    for (const k of Object.keys(r.headers)) {
      if (/set-cookie|location/i.test(k)) r.headers[k] = norm(r.headers[k])
      if (/^etag$/i.test(k)) r.headers[k] = (r.headers[k] || '').replace(/W\/"([0-9a-f]+)-([A-Za-z0-9+\/=]+)/, 'W/"$1-HASH"')
    }
  }
  return recs
}

test.describe.configure({ timeout: 900_000 })

test('10.1: history family Node==Go==Node (3 legs)', async () => {
  await waitUp()
  flushRateLimiters()
  const leg1 = runBattery(NODE, 'node1')
  const legGo = runBattery(GO, 'go')
  const leg2 = runBattery(NODE, 'node2')

  const t1 = new Map(leg1.map((r: any) => [r.tag, r]))
  const tG = new Map(legGo.map((r: any) => [r.tag, r]))

  expect(new Set(leg1.map((r: any) => r.tag))).toEqual(new Set(legGo.map((r: any) => r.tag)))
  expect(new Set(leg2.map((r: any) => r.tag))).toEqual(new Set(leg1.map((r: any) => r.tag)))

  const diffs: string[] = []
  const rows: string[] = []
  for (const tag of [...t1.values()].map((r: any) => r.tag).sort()) {
    const a = t1.get(tag)
    const b = tG.get(tag)
    const c = leg2.find((r: any) => r.tag === tag)
    if (!a || !b || !c) { diffs.push(`${tag}: MISSING in one leg`); continue }
    const sig = (r: any) => JSON.stringify([r.status, r.ct, r.b64 || r.body, r.headers, r.len])
    const ok = sig(a) === sig(b) && sig(a) === sig(c)
    const blen = (r: any) => (r.b64 ? r.b64.length : (r.body ? r.body.length : 0))
    rows.push(`${ok ? 'PASS' : 'DIFF'}  ${tag}  node1=${a.status}/${blen(a)} go=${b.status}/${blen(b)}`)
    if (!ok) {
      diffs.push(tag)
      rows.push(`   node1 JSON: ${JSON.stringify([a.status, a.ct, (a.b64 || a.body).slice(0, 400), a.headers])}`)
      rows.push(`   go    JSON: ${JSON.stringify([b.status, b.ct, (b.b64 || b.body).slice(0, 400), b.headers])}`)
      rows.push(`   node2 JSON: ${JSON.stringify([c.status, c.ct, (c.b64 || c.body).slice(0, 400), c.headers])}`)
    }
  }
  console.log(rows.join('\n'))
  console.log(`U10.1 history: legs=${leg1.length} cases=${t1.size} diffs=${diffs.length}`)
  expect(diffs, `wire diffs (Node==Go==Node):\n${diffs.join('\n')}`).toEqual([])

  // Leg stability (node1 == node2) already asserted per-case above.
  const blob = t1.get('blob get member')
  expect(blob).toBeTruthy()
}, 900_000)
