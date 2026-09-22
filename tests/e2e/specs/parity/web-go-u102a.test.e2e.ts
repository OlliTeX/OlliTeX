/**
 * P7 U10.2a parity gate — residual web surface slice A (WEB_GO_PLAN.md
 * U10.2 "residual web surface (analytics/event, editingSession, university
 * redirects, project tokens, 404 pins)"):
 *
 *   POST /event/:event              → 202 Accepted (analytics off —
 *                                     apis.v1 unset) | 404 "Cannot POST"
 *                                     for class-mismatched segments (dot,
 *                                     plus, space, slash, empty) | 403
 *                                     anonymous (CSRF wall)
 *   PUT  /editingSession/:projectId → 202 | 404 (GET/no-seg) |
 *                                     429 serial (limiter 20/min)
 *   GET  /university, /university/* → 302 /i/university… (lowercased,
 *                                     first .html stripped)
 *   GET  /project/:id/tokens        → 200 {} (owner tokens:{}) | 403
 *                                     (owner tokens ABSENT) | 404 ghost |
 *                                     401/302 anonymous
 *   404 pins: planned_maintenance, history/resync method 404,
 *             import-document/docx 404
 *   GET  /logout (logged)           → 200 confirmation page (nonce CSP,
 *                                     csrf-normalized body)
 *
 * Node==Go==Node 3-leg gate. Compared per case: status, body (nonce/csrf/
 * sid-expires normalized), and the FULL application header set — the
 * transport-only connection/keep-alive date headers are the one documented
 * exclusion (net/http vs Node http transport).
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const redisC = 'ol-e2e-redis-1'
const BATTERY = `${process.cwd()}/specs/parity/u102a-matrix.cjs`
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
    ['exec', redisC, 'sh', '-c', 'for k in $(redis-cli --scan --pattern "rate-limit:overleaf-login:*"); do redis-cli DEL "$k"; done'],
    { encoding: 'utf8' },
  )
  execFileSync(
    'docker',
    ['exec', redisC, 'sh', '-c', 'for k in $(redis-cli --scan --pattern "rate-limit:analytics*"); do redis-cli DEL "$k"; done'],
    { encoding: 'utf8' },
  )
  execFileSync(
    'docker',
    ['exec', redisC, 'sh', '-c', 'for k in $(redis-cli --scan --pattern "rate-limit:get-project-tokens*"); do redis-cli DEL "$k"; done'],
    { encoding: 'utf8' },
  )
}

const norm = (s: string): string =>
  s
    .replace(/nonce="[^"]*"/g, 'nonce="N"')
    .replace(/nonce-[A-Za-z0-9+/=]+/g, 'nonce-X')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="C"')
    .replace(/ol-csrfToken="[^"]*"/g, 'ol-csrfToken="C"')
    .replace(/ol-csrfToken=[A-Za-z0-9_~-]+/g, 'ol-csrfToken=T')
    .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="C"')
    .replace(/s%3A[A-Za-z0-9._~+\/%-]+/g, 's%3AX')
    .replace(/expires=[^;]+/gi, 'expires=E')
    .replace(/Expires: [^,\n]+/i, 'Expires: E')
    // ETag hashes derive from nonce/csrf-bearing bodies — normalize the
    // hash, keep the length prefix (it IS the body-size parity check).
    .replace(/W\/"([0-9a-f]+)-([A-Za-z0-9+/=]+)/g, 'W/"$1-HASH"')

// Transport-only headers (net/http vs Node http) — documented exclusion.
const TRANSPORT = new Set(['connection', 'keep-alive', 'date'])

function runBattery(base: string, leg: string): any[] {
  execFileSync('docker', ['cp', BATTERY, `${overleafC}:/tmp/u102a-matrix.cjs`], { stdio: ['ignore', 'pipe', 'pipe'] })
  const run = dexeStrict(overleafC, `LEG=${leg} node /tmp/u102a-matrix.cjs 2>/dev/null | sed -n 's/^BATTERY-JSON://p'`)
  const start = run.indexOf('BATTERY-JSON:')
  const raw = start >= 0 ? run.slice(start + 'BATTERY-JSON:'.length).trim() : run.trim()
  let recs: any[]
  try {
    recs = JSON.parse(raw)
  } catch (e: any) {
    throw new Error(`battery(${leg}) produced no JSON: ${String(e).slice(0, 200)} | out=${run.slice(0, 400)}`)
  }
  for (const r of recs) {
    if (r.err) throw new Error(`battery(${leg}) case ${r.tag}: ${r.err}`)
    r.body = norm(r.body || '')
    const h: Record<string, string> = {}
    for (const k of Object.keys(r.headers || {})) {
      const lk = String(k).toLowerCase()
      if (TRANSPORT.has(lk)) continue
      h[lk] = norm(r.headers[k])
    }
    r.headers = h
  }
  return recs
}

test.describe.configure({ timeout: 900_000 })

test('10.2a: analytics/university/tokens/404-pins Node==Go==Node (3 legs)', async () => {
  await waitUp()
  flushRateLimiters()
  const leg1 = runBattery(NODE, '1')
  // The analytics limiters are SHARED Redis state keyed
  // `rate-limit:analytics-update-editing-session:<projectId>:<uid>` (Node
  // and Go consume the SAME key shape — pinned), so each leg must start
  // from a clean window or the es429-serial case drifts between legs.
  flushRateLimiters()
  const legGo = runBattery(GO, '2')
  flushRateLimiters()
  const leg2 = runBattery(NODE, '3')

  const t1 = new Map(leg1.map((r: any) => [r.tag, r]))
  const tG = new Map(legGo.map((r: any) => [r.tag, r]))

  expect(
    new Set(leg1.map((r: any) => r.tag)),
    'tag sets (node1)',
  ).toEqual(new Set(legGo.map((r: any) => r.tag)))
  expect(new Set(leg2.map((r: any) => r.tag))).toEqual(new Set(leg1.map((r: any) => r.tag)))

  const diffs: string[] = []
  const rows: string[] = []
  for (const tag of [...t1.keys()].sort()) {
    const a = t1.get(tag)
    const b = tG.get(tag)
    const c = leg2.find((r: any) => r.tag === tag)
    if (!a || !b || !c) {
      diffs.push(`${tag}: MISSING in one leg`)
      continue
    }
    const sig = (r: any) => JSON.stringify({ status: r.status, len: r.len, headers: r.headers, body: r.body, statuses: r.statuses, lastStatus: r.lastStatus, lastBody: r.lastBody })
    const ok = sig(a) === sig(b) && sig(a) === sig(c)
    rows.push(`${ok ? 'PASS' : 'DIFF'}  ${tag}`)
    if (!ok) {
      diffs.push(tag)
      rows.push(`   node1: ${JSON.stringify(a).slice(0, 800)}`)
      rows.push(`   go   : ${JSON.stringify(b).slice(0, 800)}`)
      rows.push(`   node2: ${JSON.stringify(c).slice(0, 800)}`)
    }
  }
  console.log(rows.join('\n'))
  console.log(`U10.2a: legs=${leg1.length} cases=${t1.size} diffs=${diffs.length}`)
  expect(
    diffs,
    `wire diffs (Node==Go==Node):\n${diffs.join('\n')}`,
  ).toEqual([])

  // Spot pins (independent of the leg-equality above).
  const ev1 = t1.get('ev1 POST /event/test {}')
  expect(ev1).toBeTruthy()
  expect(ev1.status).toBe(202)
  expect(ev1.body).toBe('Accepted')
  expect(ev1.headers['content-type']).toBe('text/plain; charset=utf-8')
  expect(ev1.headers['content-length']).toBe('8')
}, 900_000)
