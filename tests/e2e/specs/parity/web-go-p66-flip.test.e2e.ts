/**
 * P6.6 flip gate — zotero module surface (Node → OlliTeX Go web).
 *
 * 3-leg contract-parity gate (same harness family as P6.4a…P6.5):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline (two-phase battery: disabled → enabled)
 *   leg 2: CUMULATIVE flip (P6.4a + P6.4b + P6.5 + P6.6) → same battery vs Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *
 * Two phases per leg (the e2e zotero section default is DISABLED):
 *   A (site_settings.zotero.enabled unset → seed false):
 *       groups/oauth/picker/* → 403 text/html "Zotero is disabled on this site"
 *       status 200 false; unlink 200 OK; callback 403 {"message":"Invalid OAuth token"}
 *   B (site_settings.zotero.enabled=true, user NOT zotero-linked):
 *       groups 200 null; picker/libraries|collections|items 409 zotero_not_linked
 *       picker/bibtex no-keys 400 "no items selected", with-keys 409
 *       oauth 400 "Failed to start Zotero authorization" (live fast-fail in sandbox)
 *       status 200 false; unlink 200 OK; callback 403 {"message":"Invalid OAuth token"}
 *
 * The phase transition writes `site_settings.global.zotero.enabled` +
 * restarts the Node web (its getSection has a 5s doc cache; Go reads live).
 * Determinism: the e2e user is unlinked → no zotero.org traffic on pinned
 * paths; no collection is written (unlink is a no-op when unlinked).
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
// Cumulative: P7 ships the union — leg 2 exercises the full flipped so far.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const UA = 'p66-gate'

type Leg = Record<string, { status: number; ct: string; loc: string; body: string }>

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function msh(cmd: string, capture = false): string {
  try {
    const out = execFileSync('docker', ['exec', mongoC, 'mongosh', 'mongodb://127.0.0.1:27017/sharelatex', '--quiet', '--eval', cmd], {
      encoding: 'utf8',
      stdio: capture ? 'pipe' : 'ignore',
      maxBuffer: 64 * 1024 * 1024,
    })
    return out || ''
  } catch (e: any) {
    if (capture) throw e
    return (e.stdout ? e.stdout.toString() : '') || ''
  }
}

function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 64 * 1024 * 1024 })
}

function dexe(c: string, cmd: string, capture = false): string {
  try {
    const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore', maxBuffer: 64 * 1024 * 1024 })
    return out || ''
  } catch (e: any) {
    if (capture) throw e
    return ''
  }
}

// curl exit≠0 (connection refused while a service is draining) makes
// execFileSync throw — the captured '000'/'200' stdout rides on e.stdout.
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

async function waitGo(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    if (probe('http://127.0.0.1:4010/status') === '200') return
    await sleep(500)
  }
  throw new Error('Go :4010 never came up')
}

async function waitNode(): Promise<void> {
  // inside the container the node web app listens on 127.0.0.1:4000 (7420 is
  // the host-side mapped port only — unreachable from in-container).
  for (let i = 0; i < 90; i++) {
    if (probe('http://127.0.0.1:4000/status') === '200') return
    await sleep(500)
  }
  throw new Error('Node :4000 never came up after restart')
}

function flipCount(conf: string): number {
  return Number(dexeStrict(overleafC, `sh -c 'grep -c "overleaf-flips/${conf}" /etc/nginx/sites-enabled/overleaf.conf || true'`).trim()) || 0
}

async function flip(mode: 'apply' | 'strip'): Promise<void> {
  if (mode === 'strip') {
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"`)
    dexeStrict(overleafC, 'nginx -t && nginx -s reload')
    const r: any = await fetch(BASE + '/status', { headers: { 'user-agent': UA } })
    if (r.status !== 200) throw new Error('strip failed (status not 200)')
    await sleep(1200)
    for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
    return
  }
  dexeStrict(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && mkdir -p /etc/nginx/overleaf-flips')
  for (const conf of FLIPCONFS) {
    execFileSync('docker', ['cp', `${FLIPSRC}/${conf}`, `${overleafC}:/usr/local/share/overleaf-flips/${conf}`], { timeout: 30000 })
    dexeStrict(overleafC, `cp -f /usr/local/share/overleaf-flips/${conf} /etc/nginx/overleaf-flips/${conf}`)
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';let s=fs.readFileSync(p,'utf8');const inc='  include /etc/nginx/overleaf-flips/${conf};'+String.fromCharCode(10);if(!s.includes('overleaf-flips/${conf}')){if(s.includes('location / {')){s=s.replace('location / {',inc+'location / {',1)}else{throw new Error('anchor not found')}};fs.writeFileSync(p,s)"`)
  }
  dexeStrict(overleafC, 'nginx -t && nginx -s reload')
  await sleep(1200)
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(1)
}

// ---------- phase state ----------

// site_settings zotero.enabled: null = unset (disabled seed), true = enabled.
// Writes the REAL collection (site_settings — snake_case) + restarts the
// Node web (5s getSection doc cache; Go reads per request).
function setZoteroEnabled(v: boolean | null): void {
  if (v === null) {
    msh('db.site_settings.updateOne({_id:"global"},{$unset:{"zotero":1}})', true)
  } else {
    msh(`db.site_settings.updateOne({_id:"global"},{$set:{"zotero.enabled":${v}}})`, true)
  }
}

async function setPhase(v: boolean | null): Promise<void> {
  setZoteroEnabled(v)
  dexeStrict(overleafC, 'sv restart web-overleaf')
  await waitNode()
  await sleep(800)
}

function zoteroState(): string {
  // mongosh: JSON.stringify(undefined) === undefined → print() writes nothing;
  // `?? null` pins unset as 'null'.
  return msh('print(JSON.stringify(db.site_settings.findOne({_id:"global"}).zotero ?? null))', true).trim().split('\n').pop() || ''
}

function userLinked(): string {
  return msh(
    'const u=db.users.findOne({email:"e2e-user@e2e.test"}); print(u && u.refProviders && u.refProviders.zotero ? "linked" : "not-linked")',
    true
  ).trim().split('\n').pop() || ''
}

// ---------- diff (zotero bodies are exact constants — no volatiles) ----------
const norm = (s: string): string => s

function diffLegs(label: string, A: Leg, B: Leg): string[] {
  const ds: string[] = []
  const ks = [...new Set([...Object.keys(A), ...Object.keys(B)])].sort()
  for (const k of ks) {
    const a = A[k]
    const b = B[k]
    if (!a && !b) continue
    if (!a || !b) {
      ds.push(`${label}:${k} present-on-one-side`)
      continue
    }
    if (a.status !== b.status) ds.push(`${label}:${k} status A=${a.status} B=${b.status}`)
    if (a.ct !== b.ct) ds.push(`${label}:${k} ct A='${a.ct}' B='${b.ct}'`)
    if (a.loc !== b.loc) ds.push(`${label}:${k} loc A='${a.loc}' B='${b.loc}'`)
    if (norm(a.body) !== norm(b.body)) {
      ds.push(`${label}:${k} body A~${norm(a.body).slice(0, 140)} || B~${norm(b.body).slice(0, 140)}`)
    }
  }
  return ds
}

// ---------- battery ----------

async function runLeg(): Promise<Leg> {
  const MU = await login(USER.email, USER.password)
  const AH = { cookie: MU.ck, 'x-csrf-token': MU.tok, 'user-agent': UA, accept: 'application/json' }
  const AN = { 'user-agent': UA }
  const ANJ = { 'user-agent': UA, accept: 'application/json' }

  async function call(init: { path: string; method?: string; headers?: Record<string, string> }, body?: any): Promise<{ status: number; ct: string; loc: string; body: string }> {
    const h: Record<string, string> = { ...(init.headers || {}) }
    let payload: string | undefined
    if (body !== undefined) {
      payload = typeof body === 'string' ? body : JSON.stringify(body)
      h['content-type'] = h['content-type'] || 'application/json'
    }
    const r: any = await fetch(BASE + init.path, { method: init.method || 'GET', headers: h, body: payload, redirect: 'manual' })
    const text = await r.text()
    return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location') || '', body: text }
  }

  const pins: Leg = {}

  // ---- Phase A: zotero DISABLED (default) ----
  pins.A_zstate = { status: 0, ct: '', loc: '', body: zoteroState() }
  pins.A_ulink = { status: 0, ct: '', loc: '', body: userLinked() }

  // anon (accept negotiation + csrf 403 — requireGlobalLogin chain)
  pins.A_anon_status_json = await call({ path: '/user/zotero/status', headers: ANJ })
  pins.A_anon_status = await call({ path: '/user/zotero/status', headers: AN })
  pins.A_anon_groups_json = await call({ path: '/user/zotero/groups', headers: ANJ })
  pins.A_anon_pickl_json = await call({ path: '/user/zotero/picker/libraries', headers: ANJ })
  pins.A_anon_oauth_json = await call({ path: '/user/zotero/oauth', headers: ANJ })
  pins.A_anon_cb_json = await call({ path: '/user/zotero/oauth/callback?oauth_token=x&oauth_verifier=y', headers: ANJ })
  pins.A_anon_del = await call({ path: '/user/zotero', method: 'DELETE', headers: AN })
  pins.A_anon_patch = await call({ path: '/user/zotero/picker/libraries', method: 'PATCH', headers: AN })

  // member — gated routes 403 disabled; un-gated routes pass through
  pins.A_m_status = await call({ path: '/user/zotero/status', headers: AH })
  pins.A_m_groups = await call({ path: '/user/zotero/groups', headers: AH })
  pins.A_m_oauth = await call({ path: '/user/zotero/oauth', headers: AH })
  pins.A_m_pick_l = await call({ path: '/user/zotero/picker/libraries', headers: AH })
  pins.A_m_pick_c = await call({ path: '/user/zotero/picker/collections?library=GL1&libraryKind=group', headers: AH })
  pins.A_m_pick_i = await call({ path: '/user/zotero/picker/items?library=&libraryKind=user', headers: AH })
  pins.A_m_pick_b1 = await call({ path: '/user/zotero/picker/bibtex', headers: AH })
  pins.A_m_pick_b2 = await call({ path: '/user/zotero/picker/bibtex?keys=AB1%2CCD2', headers: AH })
  pins.A_m_cb = await call({ path: '/user/zotero/oauth/callback?oauth_token=x&oauth_verifier=y', headers: AH })
  pins.A_m_unlink = await call({ path: '/user/zotero', method: 'DELETE', headers: AH })

  // ---- Phase B: zotero ENABLED (admin-managed), still unlinked ----
  await setPhase(true)
  pins.B_zstate = { status: 0, ct: '', loc: '', body: zoteroState() }
  pins.B_ulink = { status: 0, ct: '', loc: '', body: userLinked() }

  pins.B_anon_status_json = await call({ path: '/user/zotero/status', headers: ANJ })
  pins.B_anon_del = await call({ path: '/user/zotero', method: 'DELETE', headers: AN })

  pins.B_m_status = await call({ path: '/user/zotero/status', headers: AH })
  pins.B_m_groups = await call({ path: '/user/zotero/groups', headers: AH })
  pins.B_m_oauth = await call({ path: '/user/zotero/oauth', headers: AH })
  pins.B_m_pick_l = await call({ path: '/user/zotero/picker/libraries', headers: AH })
  pins.B_m_pick_c = await call({ path: '/user/zotero/picker/collections?library=GL1&libraryKind=group', headers: AH })
  pins.B_m_pick_i = await call({ path: '/user/zotero/picker/items?library=&libraryKind=user', headers: AH })
  pins.B_m_pick_b1 = await call({ path: '/user/zotero/picker/bibtex', headers: AH })
  pins.B_m_pick_b2 = await call({ path: '/user/zotero/picker/bibtex?keys=AB1%2CCD2', headers: AH })
  pins.B_m_cb = await call({ path: '/user/zotero/oauth/callback?oauth_token=x&oauth_verifier=y', headers: AH })
  pins.B_m_unlink = await call({ path: '/user/zotero', method: 'DELETE', headers: AH })

  // restore deterministic default (disabled) for the next leg / afterAll
  await setPhase(null)
  return pins
}

async function login(email: string, pw: string): Promise<{ ck: string; tok: string }> {
  // the battery may start right after a web-overleaf restart — retry the
  // whole login across the 502 drain window.
  let lastErr: any = null
  for (let attempt = 0; attempt < 8; attempt++) {
    try {
      const r0: any = await fetch(BASE + '/login', { headers: { 'user-agent': UA }, redirect: 'manual' })
      const html = await r0.text()
      const csrf = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
      const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
      const r1: any = await fetch(BASE + '/login', {
        method: 'POST',
        headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json', cookie: ck0, 'user-agent': UA },
        body: JSON.stringify({ email, password: pw }),
        redirect: 'manual',
      })
      const body1 = await r1.text()
      if (r1.status === 502 || r1.status === 503 || r1.status === 504) {
        lastErr = new Error('drain window ' + r1.status)
        await sleep(1500)
        continue
      }
      if (r1.status !== 200) throw new Error('login failed ' + r1.status + ' ' + body1.slice(0, 200))
      const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
      const cs: any = await fetch(BASE + '/dev/csrf', { headers: { cookie: ck, 'user-agent': UA } })
      const tok = (await cs.text()).trim()
      return { ck, tok }
    } catch (e: any) {
      lastErr = e
      await sleep(1500)
    }
  }
  if (lastErr instanceof Error) throw lastErr
  throw new Error('login failed (retries exhausted)')
}

// ---------- gate legs ----------
let leg1: Leg | null = null
let leg2: Leg | null = null
let leg3: Leg | null = null

test('leg 0: flip off before start', async () => {
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
}, 60_000)

test('leg 1: Node baseline (A disabled → B enabled)', async () => {
  leg1 = await runLeg()
  expect(Object.keys(leg1).length).toBeGreaterThanOrEqual(26)
  expect(zoteroState()).toBe('null')
  expect(userLinked()).toBe('not-linked')
}, 300_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  leg2 = await runLeg()
  const ds = diffLegs('go', leg1!, leg2!)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 4000))
  expect(zoteroState()).toBe('null')
  expect(userLinked()).toBe('not-linked')
}, 300_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  leg3 = await runLeg()
  const ds = diffLegs('node-determinism', leg1!, leg3!)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 4000))
  expect(zoteroState()).toBe('null')
  expect(userLinked()).toBe('not-linked')
}, 300_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1!
  // disabled phase
  expect(L.A_zstate.body).toBe('null')
  expect(L.A_m_status.status).toBe(200)
  expect(L.A_m_status.body).toBe('false')
  for (const k of ['A_m_groups', 'A_m_oauth', 'A_m_pick_l', 'A_m_pick_c', 'A_m_pick_i', 'A_m_pick_b1', 'A_m_pick_b2']) {
    expect(L[k].status).toBe(403)
    expect(L[k].ct).toBe('text/html')
    expect(L[k].body).toBe('Zotero is disabled on this site')
  }
  expect(L.A_m_cb.status).toBe(403)
  expect(L.A_m_cb.body).toBe('{"message":"Invalid OAuth token"}')
  expect(L.A_m_unlink.status).toBe(200)
  expect(L.A_m_unlink.ct).toBe('text/plain')
  expect(L.A_m_unlink.body).toBe('OK')
  // anon
  expect(L.A_anon_status_json.status).toBe(401)
  expect(L.A_anon_status.loc).toBe('/login')
  expect(L.A_anon_del.status).toBe(403)
  expect(L.A_anon_del.body).toBe('Forbidden')
  // enabled phase
  expect(L.B_zstate.body).toBe('{"enabled":true}')
  expect(L.B_m_status.status).toBe(200)
  expect(L.B_m_status.body).toBe('false')
  expect(L.B_m_groups.status).toBe(200)
  expect(L.B_m_groups.body).toBe('null')
  for (const k of ['B_m_pick_l', 'B_m_pick_c', 'B_m_pick_i', 'B_m_pick_b2']) {
    expect(L[k].status).toBe(409)
    expect(L[k].body).toBe('{"message":"zotero_not_linked"}')
  }
  expect(L.B_m_pick_b1.status).toBe(400)
  expect(L.B_m_pick_b1.body).toBe('{"message":"no items selected"}')
  expect(L.B_m_oauth.status).toBe(400)
  expect(JSON.parse(L.B_m_oauth.body).message).toBe('Failed to start Zotero authorization')
  expect(L.B_m_cb.status).toBe(403)
  expect(L.B_m_cb.body).toBe('{"message":"Invalid OAuth token"}')
  expect(L.B_m_unlink.status).toBe(200)
  expect(L.B_m_unlink.body).toBe('OK')
  expect(L.B_anon_status_json.status).toBe(401)
  expect(L.B_anon_del.status).toBe(403)
})

test.afterAll(async () => {
  try {
    for (const conf of FLIPCONFS) if (flipCount(conf) > 0) await flip('strip')
  } catch {
    /* best effort */
  }
  try {
    setZoteroEnabled(null)
    dexeStrict(overleafC, 'sv restart web-overleaf')
    await waitNode()
  } catch {
    /* best effort */
  }
})
