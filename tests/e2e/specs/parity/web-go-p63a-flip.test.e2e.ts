/**
 * WEB-GO P6.3a FLIP GATE (WEB_GO_PLAN.md P6.3a — admin-tools user surface,
 * read side):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p63a.conf):
 *     POST /admin/users              login+admin -> {totalSize,users} (filters+sort)
 *     GET  /admin/user/:userId/info  login+admin -> {activationLink,canManageTemplates}
 *
 *   leg 1  Node baseline       (flip OFF)
 *   leg 2  FLIP ON — Go        (flip ON , battery, flip OFF)
 *   leg 3  Node re-baseline    (flip OFF)
 *
 *   Battery: anon/member authz matrix (403 'Forbidden', 302 restricted,
 *   403 token-less), full-list pins, filter battery (all/admin/inactive/
 *   suspended/local/saml/deleted/search×3 — incl. the no-lastName search
 *   short-circuit quirk), sort battery (name asc/desc identical, email,
 *   lastActive, signUpDate, deletedAt, bad-by/bad-order 500s), urlencoded
 *   body, bad-CT body handling (200 full list per Node req.body={}) and
 *   malformed-JSON 400 {}, info pins (token-less nulls, ghost, bad id).
 *
 *   Node oracle pins (live 2026-09-16, /tmp/p63a_node.json).
 */
import { execFileSync } from 'node:child_process'
import { test, expect } from '@playwright/test'

const overleafC = 'ol-e2e-overleaf-1'
const FLIPCONF = 'web-p63a.conf'
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const ADMIN_ID = '6aa4b8a873ef0e5094f4cba3'
const USER_ID = '6aa4b8b573ef0e5094f4cbc0'
const TPL_ID = '6aa4b8c0ee67ff98732d4947'

type Leg = Record<string, { status: number; ct: string; loc?: string | null; body: string; len: number }>

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function dexe(c: string, cmd: string, capture = false): string {
  try {
    const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore', maxBuffer: 64 * 1024 * 1024 })
    return out || ''
  } catch (e) {
    if (capture) throw e
    return ''
  }
}
function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 64 * 1024 * 1024 })
}

async function waitGo(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
    if (code === '200') return
    await sleep(500)
  }
  throw new Error('Go shadow /status not 200')
}

async function call(p: string, init: { method?: string; headers?: Record<string, string>; cookie?: string; body?: any } = {}): Promise<any> {
  const h: Record<string, string> = { ...(init.headers || {}) }
  if (init.cookie) h['cookie'] = init.cookie
  let lastErr: unknown = null
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      const r = await fetch(BASE + p, { method: init.method || 'GET', headers: h, body: init.body as any, redirect: 'manual' })
      const buf = Buffer.from(await r.arrayBuffer())
      const setcookie = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : (r.headers.get('set-cookie') || '').toString()
      return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location'), body: buf.toString('latin1'), len: buf.length, setcookie }
    } catch (e) {
      lastErr = e
      await sleep(400 * (attempt + 1))
    }
  }
  throw lastErr
}

async function login(user: { email: string; password: string }): Promise<{ ck: string; csrf: string }> {
  const page = await call('/login')
  const csrf0 = (page.body.match(/name=["']ol-csrfToken["']\s+content=["']([^"']+)"/) || [])[1] || ''
  const ck0 = ((page.setcookie || '').match(/overleaf\.sid=[^;\n]+/) || [])[0] || ''
  const logged = await call('/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': csrf0, accept: 'application/json' },
    cookie: ck0 || undefined,
    body: JSON.stringify(user),
  })
  if (logged.status !== 200) throw new Error('login failed ' + logged.status)
  const sid = ((logged.setcookie || '').split('\n').find((l: string) => l.includes('overleaf.sid')) || '').replace(/^overleaf\.sid=/, '').split(';')[0]
  const ck = sid ? 'overleaf.sid=' + sid : ck0
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body.trim() || csrf0 }
}

function verifyInclude(n: number): void {
  const cnt = dexeStrict(overleafC, `sh -c 'grep -c "overleaf-flips/${FLIPCONF}" /etc/nginx/sites-enabled/overleaf.conf || true'`).trim()
  if (cnt !== String(n)) throw new Error(`flip include expected ${n}, found ${cnt}`)
}
function flip(mode: 'apply' | 'strip'): void {
  if (mode === 'strip') {
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"`)
    dexeStrict(overleafC, 'nginx -t')
    dexe(overleafC, 'nginx -s reload')
    verifyInclude(0)
    return
  }
  dexeStrict(overleafC, `mkdir -p /etc/nginx/overleaf-flips && cp -f /usr/local/share/overleaf-flips/${FLIPCONF} /etc/nginx/overleaf-flips/${FLIPCONF}`)
  dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';let s=fs.readFileSync(p,'utf8');const inc='  include /etc/nginx/overleaf-flips/${FLIPCONF};'+String.fromCharCode(10);if(!s.includes('overleaf-flips/${FLIPCONF}')){if(s.includes('location / {')){s=s.replace('location / {',inc+'location / {',1)}else{throw new Error('anchor not found')}};fs.writeFileSync(p,s)"`)
  dexeStrict(overleafC, 'nginx -t')
  dexe(overleafC, 'nginx -s reload')
  verifyInclude(1)
}

const JS = { 'content-type': 'application/json', accept: 'application/json' }

function diffLegs(label: string, A: Leg, B: Leg): string[] {
  const ds: string[] = []
  const ks = [...new Set([...Object.keys(A), ...Object.keys(B)])].sort()
  for (const k of ks) {
    const a = A[k]
    const b = B[k]
    if (!a && !b) continue
    if (!a || !b) { ds.push(`${label}:${k} present-on-one-side`); continue }
    if (a.status !== b.status) ds.push(`${label}:${k} status A=${a.status} B=${b.status}`)
    if (a.ct !== b.ct) ds.push(`${label}:${k} ct A='${a.ct}' B='${b.ct}'`)
    if ((a.loc || '') !== (b.loc || '')) ds.push(`${label}:${k} loc A='${a.loc}' B='${b.loc}'`)
    if (a.len !== b.len) ds.push(`${label}:${k} len A=${a.len} B=${b.len} | A~${a.body.slice(0, 80).replace(/\n/g, ' ')} | B~${b.body.slice(0, 80).replace(/\n/g, ' ')}`)
    else if (a.body !== b.body) {
      const i = a.body.indexOf(b.body) >= 0 || b.body.indexOf(a.body) >= 0 ? -1 : a.body.findIndex((c: string, i2: number) => c !== b.body[i2])
      ds.push(`${label}:${k} body@${i} | A~${a.body.slice(Math.max(0, i - 40), i + 100).replace(/\n/g, ' ')} || B~${b.body.slice(Math.max(0, i - 40), i + 100).replace(/\n/g, ' ')}`)
    }
  }
  return ds
}

async function battery(U: { ck: string; csrf: string }, A: { ck: string; csrf: string }): Promise<Leg> {
  const out: Leg = {}
  const P = (o: any) => JSON.stringify(o)
  const post = (path: string, body?: any, extra?: Record<string, string>, ck?: string, csrf?: string) =>
    call(path, { method: 'POST', cookie: ck || A.ck, headers: { ...JS, 'csrf-token': csrf || A.csrf, ...(extra || {}) }, body: body !== undefined ? (typeof body === 'string' ? body : P(body)) : undefined })

  // --- authz matrix ---
  out.anon_post_users = await call('/admin/users', { method: 'POST', headers: JS, body: P({}) })
  out.anon_post_users_html = await call('/admin/users', { method: 'POST', headers: { 'content-type': 'application/x-www-form-urlencoded', accept: 'text/html' }, body: '' })
  out.anon_info = await call(`/admin/user/${USER_ID}/info`)

  out.user_post_users = await post('/admin/users', {}, undefined, U.ck, U.csrf)
  out.user_post_users_html = await post('/admin/users', '', { 'content-type': 'application/x-www-form-urlencoded', accept: 'text/html' }, U.ck, U.csrf)
  out.user_info = await call(`/admin/user/${USER_ID}/info`, { cookie: U.ck })
  out.user_nocsrf = await call('/admin/users', { method: 'POST', cookie: U.ck, headers: JS, body: P({}) })

  // --- admin: base lists ---
  out.admin_users_empty = await post('/admin/users', {})
  out.admin_users_nobody = await call('/admin/users', { method: 'POST', cookie: A.ck, headers: { ...JS, 'csrf-token': A.csrf } })

  // --- filters ---
  out.f_all = await post('/admin/users', { filters: { all: true } })
  out.f_admin = await post('/admin/users', { filters: { admin: true } })
  out.f_inactive = await post('/admin/users', { filters: { inactive: true } })
  out.f_suspended = await post('/admin/users', { filters: { suspended: true } })
  out.f_local = await post('/admin/users', { filters: { local: true } })
  out.f_saml = await post('/admin/users', { filters: { saml: true } })
  out.f_deleted = await post('/admin/users', { filters: { deleted: true } })
  out.f_search_e2e = await post('/admin/users', { filters: { search: 'e2e' } })
  out.f_search_none = await post('/admin/users', { filters: { search: 'zzz-no-such-thing' } })
  out.f_search_case = await post('/admin/users', { filters: { search: 'E2E-ADMIN' } })

  // --- sort + page (page is a Node no-op) ---
  out.s_name_asc = await post('/admin/users', { sort: { by: 'name', order: 'asc' } })
  out.s_name_desc = await post('/admin/users', { sort: { by: 'name', order: 'desc' }, page: { size: 3 } })
  out.s_email_asc = await post('/admin/users', { sort: { by: 'email', order: 'asc' } })
  out.s_lastActive_desc = await post('/admin/users', { sort: { by: 'lastActive', order: 'desc' }, page: { size: 5 } })
  out.s_signUpDate_asc = await post('/admin/users', { sort: { by: 'signUpDate', order: 'asc' }, page: { size: 5 } })
  out.s_deletedAt_desc = await post('/admin/users', { sort: { by: 'deletedAt', order: 'desc' }, page: { size: 5 } })
  out.s_default_page2 = await post('/admin/users', { page: { size: 5, no: 2 } })
  out.s_badby = await post('/admin/users', { sort: { by: 'lastName', order: 'asc' } })
  out.s_badorder = await post('/admin/users', { sort: { by: 'name', order: 'sideways' } })

  // --- body-shape pins ---
  out.s_urlenc = await call('/admin/users', { method: 'POST', cookie: A.ck, headers: { 'csrf-token': A.csrf, 'content-type': 'application/x-www-form-urlencoded', accept: 'text/html' }, body: 'filters[admin]=true' })
  out.badct_textplain = await post('/admin/users', 'filters[admin]=true', { 'content-type': 'text/plain' })
  out.badct_nobody = await call('/admin/users', { method: 'POST', cookie: A.ck, headers: { 'content-type': 'text/plain', 'csrf-token': A.csrf, accept: 'application/json' } })
  out.badjson = await post('/admin/users', 'notjson', { 'content-type': 'application/json' })
  out.json_arr_body = await post('/admin/users', '[1,2]', { 'content-type': 'application/json' })
  out.form_no_ct = await call('/admin/users', { method: 'POST', cookie: A.ck, headers: { 'csrf-token': A.csrf, accept: 'application/json' }, body: 'filters[admin]=true' })

  // --- info ---
  out.info_e2euser = await call(`/admin/user/${USER_ID}/info`, { cookie: A.ck })
  out.info_admin = await call(`/admin/user/${ADMIN_ID}/info`, { cookie: A.ck })
  out.info_tpl = await call(`/admin/user/${TPL_ID}/info`, { cookie: A.ck })
  out.info_ghost = await call('/admin/user/6aaa00000000000000000001/info', { cookie: A.ck })
  out.info_badid = await call('/admin/user/notanid/info', { cookie: A.ck })
  out.info_member_other = await call(`/admin/user/${ADMIN_ID}/info`, { cookie: U.ck })

  return out
}

test.describe(`@local web-go P6.3a (admin-tools user surface reads) parity`, () => {
  let U: { ck: string; csrf: string }
  let A: { ck: string; csrf: string }
  const LEG1: Leg = {}
  let FLIPPED = false

  test.beforeAll(async () => {
    U = await login(USER)
    A = await login(ADMIN)
  })

  test.afterAll(() => {
    if (FLIPPED) {
      try {
        flip('strip')
      } catch {
        // best-effort restore (failures are visible in the run log)
      }
    }
  })

  test('leg 1: Node baseline', async () => {
    flip('strip')
    Object.assign(LEG1, await battery(U, A))
  })

  test('leg 2: Go parity', async ({}, t) => {
    await waitGo()
    flip('apply')
    FLIPPED = true
    const leg2 = await battery(U, A)
    flip('strip')
    FLIPPED = false
    const ds = diffLegs('p63a', LEG1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
    void t
  })

  test('leg 3: Node re-baseline', async () => {
    flip('strip')
    const leg3 = await battery(U, A)
    const ds = diffLegs('p63a', LEG1, leg3)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('pin sanity (oracle anchors)', async ({}, t) => {
    if (!Object.keys(LEG1).length) t.skip('no leg-1 baseline captured (earlier leg failed)')
    expect(LEG1.anon_post_users.status).toBe(403)
    expect(LEG1.anon_post_users.body).toBe('Forbidden')
    expect(LEG1.anon_info.status).toBe(302)
    expect(LEG1.anon_info.loc).toBe('/login')
    expect(LEG1.user_post_users.status).toBe(302)
    expect((LEG1.user_post_users.loc || '')).toBe('/restricted?from=%2Fadmin%2Fusers')
    expect(LEG1.user_nocsrf.status).toBe(403)
    expect(LEG1.user_nocsrf.body).toBe('Forbidden')
    const base = JSON.parse(LEG1.admin_users_empty.body)
    expect(base.totalSize).toBeGreaterThan(0)
    expect(LEG1.f_admin.body).toContain('"totalSize":1')
    expect(JSON.parse(LEG1.f_admin.body).users[0].id).toBe(ADMIN_ID)
    // Node truth (pinned live 2026-09-16): search is an in-memory
    // email/firstName/lastName substring filter — a nonsense query matches
    // nothing, while the case-folded probe 'E2E-ADMIN' still matches admin.
    const searchNone = JSON.parse(LEG1.f_search_none.body)
    expect(searchNone.totalSize).toBe(0)
    expect(JSON.parse(LEG1.f_search_case.body).totalSize).toBeGreaterThanOrEqual(1)
    expect(LEG1.s_badby.status).toBe(500)
    expect(LEG1.s_badorder.status).toBe(500)
    // name sort ignores the order flag (pinned: asc == desc)
    expect(LEG1.s_name_asc.body).toBe(LEG1.s_name_desc.body)
    expect(LEG1.badjson.status).toBe(400)
    expect(LEG1.badjson.body).toBe('{}')
    expect(LEG1.badct_textplain.status).toBe(200)
    expect(JSON.parse(LEG1.info_e2euser.body).activationLink).toBe(null)
    expect(LEG1.info_ghost.status).toBe(200)
    expect(JSON.parse(LEG1.info_ghost.body).canManageTemplates).toBe(false)
    expect(LEG1.info_badid.status).toBe(200)
    expect(LEG1.form_no_ct.status).toBe(200)
  })
})
