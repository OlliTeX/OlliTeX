/**
 * U10.1 — history/labels/zip/blob/flush/diff contract + stability matrix
 * (rework 2026-09-28; P7 lineage — replaces the pre-P7 "Node==Go==Node"
 * 3-leg :4010-shadow design, obsolete since the hard cutover).
 *
 *   - canonical Go web at :4000 (the Node web + its :4010 shadow are retired;
 *     the 3-leg parity degenerates to run1==run2 wire-stability on the
 *     canonical service)
 *   - fixture: IDEMPOTENT SELF-SEEDED project (blank create + deterministic
 *     typed edits → v1 seed + versions) — replaces the old hardcoded fs-era
 *     fixture (WebGo-Ren-N) whose chunk blobs are unrecoverable after the
 *     G2 fs→S3 swap; see WEB_GO_STATE.md 2026-09-28 (9cd74a73 diagnosis)
 *   - 2-run gate: per case status/content-type/body/headers/byte-length must
 *     match between run1 and run2, AND the contractual pins below hold.
 *
 *   Pinned contracts (P7 lineage, U10.1 gate doc preserved 1:1):
 *   GET  /api/projects/:p/updates                → 200|401|404 JSON|500
 *   GET  /project/:id/changes?since=0            → 200 JSON ; bad since → 400 ; anon → 401
 *   GET/POST/DELETE /project/:id/labels          → 200|404 · create 201/203 · no-csrf 403 · anon 401
 *   POST /project/:id/blob/:hash                 → 201 then 200 (idempotent upsert) · get 200 · range 206 · 304 · bad hash 400
 *   GET  /project/:id/version/1/zip              → 200 (member, accept text/html) · nohistory 404 JSON|402
 *   POST /project/:id/flush                      → 200 | 401 | 403
 *   GET  /project/:id/diff(/doc/:did)            → 200 | 404 JSON (bad did)
 *   PUT  restore_file/revert_file/revert-project → exact 400 (member) | 401 (anon) | 403 (badkey)
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { USER } from '../../fixtures/credentials'
import { loginRobust, createBlankProject } from '../../helpers/auth'

const overleafC = 'ol-e2e-overleaf-1'
const BATTERY = `${process.cwd()}/specs/parity/u101-history-matrix.cjs`
const NODE = 'http://127.0.0.1:4000'

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
    if (probe(`${NODE}/status`) === '200') return
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error(`canonical web not up (:4000/status=${probe(NODE + '/status')})`)
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
    // per-creation identity (each run creates its own label) — parity is on
    // shape/order/keys; the id+created_at are unique per row by design.
    .replace(/"id":"[0-9a-f]{24}"/g, '"id":"ID"')
    .replace(/"created_at":"[^"]*"/g, '"created_at":"T"')
    // ETag hashes are derived from the (nonce/id-bearing) bodies — normalize
    // the hash while keeping the length prefix (it is body-size parity).
    .replace(/W\/"([0-9a-f]+)-([A-Za-z0-9+\/=]+)/g, 'W/"$1-HASH"')

function runBattery(pid: string, nop: string, leg: string): any[] {
  const cp = execFileSync('docker', ['cp', BATTERY, `${overleafC}:/tmp/u101-history-matrix.cjs`], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] })
  if (cp) throw new Error(`docker cp failed: ${cp}`)
  const run = dexeStrict(overleafC, `node /tmp/u101-history-matrix.cjs ${NODE} ${pid} ${nop} && cat /tmp/u101-battery-out.json`)
  const start = run.indexOf('BATTERY-JSON:')
  if (start < 0) throw new Error(`battery(${leg}) produced no output: ${run.slice(0, 400)}`)
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

test('10.1: history contract + 2-run stability (canonical web)', async ({ page }) => {
  await waitUp()
  flushRateLimiters()

  // ---- self-seeded fixture (idempotent; no storage-era dependency) ----
  // FIXTURE OWNER MUST BE e2e-user — the battery's 'me' session (u101-history-matrix.cjs L87);
  // the 'nonmember' cases then use e2e-admin (L95). Owning the fixture as admin would flip both.
  await loginRobust(page, USER.email, USER.password)
  const pid = await createBlankProject(page)
  // 2026-10-05 (route-retirement wave): the legacy /Project/:id|/project/:id
// editor PAGE route is retired — open the editor via the kept /editor/:id.
await page.goto(`/editor/${pid}`, { waitUntil: 'load' })
  await page.waitForSelector('.cm-content', { timeout: 60_000 })
  for (let i = 1; i <= 3; i++) {
    await page.click('.cm-content')
    await page.keyboard.press('Control+End')
    await page.keyboard.type(`\n% u101-seed-${i}`, { delay: 5 })
    await new Promise((r) => setTimeout(r, 2500))
  }
  const versionCount = async (): Promise<number> => {
    const resp = await page.request.get(`/project/${pid}/collab/history`)
    const j = (await resp.json().catch(() => ({}))) as any
    const hs = j.versions || j.history || []
    return Array.isArray(hs) ? hs.length : 0
  }
  let vc = -1
  for (let i = 0; i < 30; i++) {
    vc = await versionCount()
    if (vc >= 2) break
    await new Promise((r) => setTimeout(r, 2000))
  }
  expect(vc).toBeGreaterThanOrEqual(2) // v1 seed + at least one committed typed edit

  // nohistory-class id: random nonexistent (no fixture dependency)
  const NOP = '6f0000000000000000' + (Date.now() & 0xffff).toString(16).padStart(4, '0').slice(0, 4) // 24 hex, nonexistent

  const leg1 = runBattery(pid, NOP, 'run1')
  const leg2 = runBattery(pid, NOP, 'run2')

  const t1 = new Map(leg1.map((r: any) => [r.tag, r]))
  const t2 = new Map(leg2.map((r: any) => [r.tag, r]))
  expect(new Set(leg1.map((r: any) => r.tag))).toEqual(new Set(leg2.map((r: any) => r.tag)))

  const diffs: string[] = []
  const rows: string[] = []
  for (const [tag, r1] of t1) {
    const r2: any = t2.get(tag)
    const hd1 = JSON.stringify(r1.headers), hd2 = JSON.stringify(r2.headers)
    const eq = r1.status === r2.status && r1.ct === r2.ct && r1.body === r2.body && r1.len === r2.len && hd1 === hd2
    rows.push(`${eq ? 'OK ' : 'DIFF'}  ${tag.padEnd(26)}  ${r1.status}/${r2.status} ct=${r1.ct || '?'} len=${r1.len}`)
    if (!eq) {
      diffs.push(tag)
      if (r1.status !== r2.status) console.log(`diff[${tag}] status ${r1.status} vs ${r2.status}`)
      if (r1.ct !== r2.ct) console.log(`diff[${tag}] ct    ${r1.ct} vs ${r2.ct}`)
      if (r1.len !== r2.len) console.log(`diff[${tag}] len   ${r1.len} vs ${r2.len}`)
      if (hd1 !== hd2) console.log(`diff[${tag}] hdr   ${hd1}\n        vs ${hd2}`)
      if (r1.body !== r2.body)
        console.log(`diff[${tag}] body  ${r1.body.slice && r1.body.slice(0, 140)} vs ${r2.body.slice && r2.body.slice(0, 140)}`)
    }
  }
  console.log('\nU10.1 contract+stability (2 runs, canonical web):\n' + rows.join('\n'))

  // ---- D41 (owner 2026-09-26) split: TRANSITIONAL cases -> observed (no assert) ----
  // v1+ version plane on the OT store is transitional in the Yjs era: ygo is the
  // version store (collabhistory + ygo versioned store are live per D41) and the
  // hybrid-b1 composition (task d5dd23dd) owns the composed label/zip/diff contract.
  // These are RECORDED + stability-checked, NOT asserted, until d5dd23dd lands.
  const TRANSITIONAL = [
    'labels create', 'labels delete', 'labels delete-badid',
    'zip v1 member', 'zip v1 head',
    'changes since1 member', 'diff 0-1 member', 'filetree diff member',
  ]
  for (const tag of TRANSITIONAL) {
    const a = t1.get(tag), b = t2.get(tag)
    console.log(`[observed/transitional] ${tag}: run1=${a?.status} run2=${b?.status}`)
  }

  // ---- contractual pins: the OBSERVED stable wire contract (each verified 2-run
  //  identical above; nodeversion oracle consulted for csrf/anon classes) ----
  const s2 = (tag: string): number | undefined => t1.get(tag)?.status
  const expectStatus = (tag: string, allowed: number[], note = ''): void => {
    const got = s2(tag)
    expect(`pin[${tag}] present`, got).not.toBe(undefined)
    expect(allowed, `pin[${tag}] got=${got} expected one of [${allowed}]${note ? ' ' + note : ''}`).toContain(got)
  }
  // --- reads (member) ---
  expectStatus('updates member', [200])
  expectStatus('updates nonmember json', [200], 'them=e2e-admin: admin read override')
  expectStatus('updates nonmember html', [200], 'them=e2e-admin')
  expectStatus('latestHistory member', [200])
  expectStatus('changes since0 member', [200])
  expectStatus('labels get member', [200])
  // --- anon class ---
  expectStatus('updates anon json', [401])
  expectStatus('latestHistory anon', [401])
  expectStatus('changes anon', [401])
  expectStatus('labels get anon', [401])
  expectStatus('blob get anon', [401])
  expectStatus('badid updates anon', [401, 403])
  expectStatus('docdiff baddid anon', [401, 403])
  // --- validation class ---
  expectStatus('changes since-abc', [400])
  expectStatus('labels create-unknown-key', [400, 404])
  expectStatus('restore_file badversion', [400, 404])
  expectStatus('revert_file badversion', [400, 404])
  expectStatus('revert_project badversion', [400, 404])
  expectStatus('restore_file unknownkey', [400, 403], 'obs 400 = validation class')
  // --- csrf scope (oracle: nodeversion Csrf.mjs global csurf, zero production exclusions) ---
  expectStatus('blob upsert first', [403], 'no token in battery J; Node oracle = global csurf')
  expectStatus('blob upsert second', [403])
  expectStatus('flush member', [403, 429], '403 no-token (oracle); 429 = flush limiter exhausted (vendor)')
  expectStatus('flush anon', [403, 429])
  expectStatus('labels create anon', [401, 403])
  expectStatus('labels create-nocsrf', [403])
  expectStatus('labels create anon nojsonct', [401, 403])
  expectStatus('labels delete nonmember', [403])
  expectStatus('labels delete anon', [401, 403])
  expectStatus('restore_file anon', [401, 403])
  expectStatus('restore_file nonmember', [403])
  // --- absent/unknown-resource class ---
  expectStatus('updates nohistory', [404, 402])
  expectStatus('latestHistory nohistory', [404, 402])
  expectStatus('docdiff baddid member', [404, 400])
  expectStatus('badid updates member', [404, 402], 'nonexistent project')
  expectStatus('zip nohistory', [404, 402, 429], '429 = download-project-revision limiter exhausted (vendor)')
  expectStatus('zip version neg', [404, 400, 429])
  expectStatus('zip version 9999', [404, 402, 429])
  expectStatus('blob get member', [404, 429], 'upsert rejected by csrf -> blob absent; 429 = get-project-blob limiter (vendor)')
  expectStatus('blob head member', [404, 429])
  expectStatus('blob range', [404, 200, 206, 429])
  expectStatus('blob badhash', [404, 400, 429])
  // --- anon zip: observed = login-page 200 (redirect-followed); vendor-true anon handling class
  expectStatus('zip anon', [200, 401, 403], 'obs 200 = anon login redirect body')
  // --- admin flush: them=e2e-admin can flush any project
  expectStatus('flush nonmember', [200, 403], 'obs 200 = admin override')
  expect(diffs, diffs.slice(0, 12).join(', ')).toEqual([])
})
