/**
 * P6.8 flip gate — mendeley reference-provider module surface (Node → OlliTeX Go web).
 *
 * 4-leg contract-parity gate (same harness family as P6.5…P6.7):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a + P6.4b + P6.5 + P6.6 + P6.7 + P6.8) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *
 * Single phase — the e2e instance is mendeley-UNCONFIGURED (no MENDELEY_*
 * env, site_settings.mendeley unset) and the e2e user is mendeley-UNLINKED,
 * so NO api.mendeley.com traffic occurs on any gate path (the Go/Node
 * code paths before the first network hop are byte-pinned):
 *
 *   anon (requireLogin chain, identical both stacks):
 *     GET  /user/mendeley/status  +accept-json → 401 text/plain "Unauthorized"
 *     GET  /mendeley/groups       (bare)       → 302 → /login
 *     POST /mendeley/unlink                      → 403 text/plain "Forbidden"
 *
 *   member:
 *     GET  /user/mendeley/status → 200 application/json
 *          {"configured":false,"connected":false}
 *          (status is the only route that answers when unconfigured)
 *     GET  /mendeley/groups      → 403 application/json
 *          {"error":"not_configured","message":"mendeley_groups_relink"}
 *     GET  /user/mendeley/oauth  → 302 Location /hub#mysettings.references
 *          (_ensureConfigured throws before the first network hop)
 *     GET  /user/mendeley/oauth/callback?code=x&state=y
 *          → 302 /hub#mysettings.references (state mismatch → same redirect)
 *     POST /mendeley/unlink      → 200 text/plain "OK"
 *          ($unset no-op — the user has no stored credentials)
 *
 * DB state anchors (unchanged across legs): site_settings.mendeley == null;
 * users.refProviders == {} (unlinked).
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
// Cumulative: P7 ships the union — leg 2 exercises the full flipped so far.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const UA = 'p68-gate'

type Leg = Record<string, { status: number; ct: string; loc: string; body: string }>

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function msh(cmd: string): string {
  try {
    const out = execFileSync('docker', ['exec', mongoC, 'mongosh', 'mongodb://127.0.0.1:27017/sharelatex', '--quiet', '--eval', cmd], {
      encoding: 'utf8',
      stdio: 'pipe',
      maxBuffer: 64 * 1024 * 1024,
    })
    return (out || '').trim().split('\n').pop() || ''
  } catch (e: any) {
    return ((e && e.stdout ? e.stdout.toString() : '') || '').trim().split('\n').pop() || ''
  }
}

function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 64 * 1024 * 1024 })
}

// curl exit≠0 (drain window) → the status rides on e.stdout.
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
    if (a.body !== b.body) {
      ds.push(`${label}:${k} body A~${a.body.slice(0, 140)} || B~${b.body.slice(0, 140)}`)
    }
  }
  return ds
}

function mendeleyState(): string {
  return msh('print(JSON.stringify(db.site_settings.findOne({_id:"global"}).mendeley ?? null))')
}

function userLinked(): string {
  return msh('const u=db.users.findOne({email:"e2e-user@e2e.test"}); print(u && u.refProviders && u.refProviders.mendeley ? "linked" : "not-linked")')
}

async function runLeg(): Promise<Leg> {
  const MU = await login(USER.email, USER.password)
  const AH = { cookie: MU.ck, 'x-csrf-token': MU.tok, 'user-agent': UA, accept: 'application/json' }
  const AN = { 'user-agent': UA }
  const ANJ = { 'user-agent': UA, accept: 'application/json' }

  async function call(init: { path: string; method?: string; headers?: Record<string, string> }): Promise<{ status: number; ct: string; loc: string; body: string }> {
    const r: any = await fetch(BASE + init.path, { method: init.method || 'GET', headers: { ...(init.headers || {}) }, redirect: 'manual' })
    const text = await r.text()
    return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location') || '', body: text }
  }

  const pins: Leg = {}
  pins.state = { status: 0, ct: '', loc: '', body: mendeleyState() }
  pins.ulink = { status: 0, ct: '', loc: '', body: userLinked() }

  // anon (requireLogin chain)
  pins.anon_status_json = await call({ path: '/user/mendeley/status', headers: ANJ })
  pins.anon_status = await call({ path: '/mendeley/groups', headers: AN })
  pins.anon_oauth_json = await call({ path: '/user/mendeley/oauth', headers: ANJ })
  pins.anon_cb_json = await call({ path: '/user/mendeley/oauth/callback?code=x&state=y', headers: ANJ })
  pins.anon_unlink_post = await call({ path: '/mendeley/unlink', method: 'POST', headers: AN })

  // member battery
  pins.m_status = await call({ path: '/user/mendeley/status', headers: AH })
  pins.m_groups = await call({ path: '/mendeley/groups', headers: AH })
  pins.m_oauth = await call({ path: '/user/mendeley/oauth', headers: AH })
  pins.m_cb = await call({ path: '/user/mendeley/oauth/callback?code=x&state=y', headers: AH })
  pins.m_unlink = await call({ path: '/mendeley/unlink', method: 'POST', headers: AH })

  pins.state_after = { status: 0, ct: '', loc: '', body: mendeleyState() }
  pins.ulink_after = { status: 0, ct: '', loc: '', body: userLinked() }
  return pins
}

async function login(email: string, pw: string): Promise<{ ck: string; tok: string }> {
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

let leg1: Leg | null = null

test('leg 0: flip off before start', async () => {
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
}, 60_000)

test('leg 1: Node baseline battery', async () => {
  leg1 = await runLeg()
  expect(Object.keys(leg1!).length).toBe(14)
}, 240_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  const leg2 = await runLeg()
  const ds = diffLegs('go', leg1!, leg2)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 4000))
}, 240_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  const leg3 = await runLeg()
  const ds = diffLegs('node-determinism', leg1!, leg3)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 4000))
}, 240_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1!
  expect(L.state.body).toBe('null')
  expect(L.ulink.body).toBe('not-linked')
  expect(L.state_after.body).toBe('null')
  expect(L.ulink_after.body).toBe('not-linked')
  // anon
  expect(L.anon_status_json.status).toBe(401)
  expect(L.anon_status_json.ct).toBe('text/plain')
  expect(L.anon_status_json.body).toBe('Unauthorized')
  expect(L.anon_status.status).toBe(302)
  expect(L.anon_status.loc).toBe('/login')
  expect(L.anon_oauth_json.status).toBe(401)
  expect(L.anon_cb_json.status).toBe(401)
  expect(L.anon_unlink_post.status).toBe(403)
  expect(L.anon_unlink_post.body).toBe('Forbidden')
  // member
  expect(L.m_status.status).toBe(200)
  expect(L.m_status.ct).toBe('application/json')
  expect(L.m_status.body).toBe('{"configured":false,"connected":false}')
  expect(L.m_groups.status).toBe(403)
  expect(L.m_groups.ct).toBe('application/json')
  expect(L.m_groups.body).toBe('{"error":"not_configured","message":"mendeley_groups_relink"}')
  expect(L.m_oauth.status).toBe(302)
  expect(L.m_oauth.loc).toBe('/hub#mysettings.references')
  expect(L.m_cb.status).toBe(302)
  expect(L.m_cb.loc).toBe('/hub#mysettings.references')
  expect(L.m_unlink.status).toBe(200)
  expect(L.m_unlink.ct).toBe('text/plain')
  expect(L.m_unlink.body).toBe('OK')
})

test.afterAll(async () => {
  try {
    for (const conf of FLIPCONFS) if (flipCount(conf) > 0) await flip('strip')
  } catch {
    /* best effort */
  }
})
