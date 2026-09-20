/**
 * WEB-GO P6.2 FLIP GATE (WEB_GO_PLAN.md P6.2 — admin-tools project surface):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p62.conf):
 *     GET    /admin/user                      login+admin → 301 /hub#/site.general.users.all
 *     GET    /admin/project                   login+admin → 301 /hub#/site.general.projects.all
 *     GET    /admin/active-projects           login+admin → RT /clients projection [] | rows
 *     POST   /admin/user/:userId/projects     login+admin → {totalSize,projects} | 500 page
 *     POST   /admin/project/:id/trash|untrash login+admin → 200 OK
 *     DELETE /admin/project/:id               login+admin → 200 {deletedAt,deleterId}
 *     POST   /admin/project/:id/undelete      login+admin → 200 {name}
 *     DELETE /admin/project/:id/purge         login+admin → 200 OK
 *     GET    /admin/project/:Project_id/members login+admin → {owner,members}
 *     GET    /admin/project/:Project_id/invites (P4 core, admin gate)
 *     POST   /admin/project/:Project_id/invite  (P4 core + admin aggregated-400 pin)
 *     PUT    /admin/project/:Project_id/users/:user_id (P4 core + admin aggregated-400 pin)
 *     DELETE /admin/project/:Project_id/users/:user_id (P4 core, admin gate)
 *     DELETE /admin/project/:Project_id/invite/:invite_id (P4 core, admin gate)
 *     POST   /admin/project/:Project_id/invite/:invite_id/resend (P4 core, admin gate)
 *     GET    /admin/project/:Project_id/sharing-link → 404 (no reusable invite)
 *     POST   /admin/project/:Project_id/sharing-link → 400 aggregated validation pin
 *
 *   leg 1  Node baseline       (flip OFF)
 *   leg 2  FLIP ON — Go        (flip ON , battery, flip OFF)
 *   leg 3  Node re-baseline    (flip OFF)
 *
 *   Battery (deterministic, self-contained each leg): legacy 301s, member/
 *   anon authz matrix, sacrificial project create → members, listing pins
 *   (admin list 500 on the known bad owner_ref row; owner-scope title-sorted
 *   full list), bad-sort 500, trash/untrash, invite/put-user 400 pins,
 *   del-user 204, sharing-link 404/400, soft-delete → undelete →
 *   soft-delete → purge → purge-again, member 302/403 matrix, anon 302/403,
 *   malformed-project-id 500s, active-projects matrix.
 *
 *   Node oracle pins (live 2026-09-16, /tmp/p62_oracle.json).
 */
import { execFileSync } from 'node:child_process'
import { test, expect } from '@playwright/test'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const FLIPCONF = 'web-p62.conf'
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }

type Leg = Record<string, { status: number; ct: string; loc?: string; body?: string; len: number }>

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
function dexeQ(c: string, cmd: string): string {
  return (execFileSync('docker', ['exec', c, 'mongosh', '--quiet', 'sharelatex', '--eval', cmd], { encoding: 'utf8' }) || '').trim()
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

// --- normalizations (volatile, implementation-stable fields only) --------
const ISO = '2006-01-02T15:04:05.000Z'
const isoRe = `\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}\\.\\d{3}Z`
const HEX24 = '[0-9a-f]{24}'

function normCreate(s: string): string {
  return s.replace(new RegExp(`"project_id":"${HEX24}"`), '"project_id":"PID"')
}
function normDel(s: string): string {
  return s.replace(new RegExp(`"deletedAt":"${isoRe}"`), '"deletedAt":"DT"')
}
// the sacrificial row (only row with this exact name at this point in the
// battery): id + lastUpdated are creation-time volatiles
function normSacrificeRow(s: string): string {
  const re = new RegExp(`\\{"id":"${HEX24}","name":"p62-sacrifice","owner":"${HEX24}","lastUpdated":"${isoRe}","lastUpdatedBy":"${HEX24}","inactive":[a-z]+,"trashed":[a-z]+,"deleted":[a-z]+\\}`)
  return s.replace(re, '{"id":"PID","name":"p62-sacrifice","owner":"OID","lastUpdated":"LU","lastUpdatedBy":"UID","inactive":false,"trashed":false,"deleted":false}')
}
function normPidUrl(s: string): string {
  // the sacrificial project id is re-minted each leg; neutralize it in the
  // 302 `from=` redirect URLs (both body text and Location header)
  return s.replace(/%2Fproject%2F[0-9a-f]{24}%2F/g, '%2Fproject%2FPID%2F')
}
function norm(k: string, s: string): string {
  if (k === 'sacrifice_create') return normCreate(s)
  if (k === 'del_soft' || k === 'del_soft2') return normDel(s)
  if (k === 'usr_list') return normSacrificeRow(s)
  if (k === 'member_members' || k === 'member_trash_tok') return normPidUrl(s)
  return s
}

// --- battery -------------------------------------------------------------
async function battery(U: { ck: string; csrf: string }, A: { ck: string; csrf: string }): Promise<Leg> {
  const out: Leg = {}
  const listBody = JSON.stringify({ filters: {}, sort: { by: 'lastUpdated', order: 'desc' }, page: { size: 20 } })
  const ownerHex = dexeQ(mongoC, 'String(db.users.findOne({email:"e2e-user@e2e.test"},{_id:1})._id)')

  // --- legacy redirects + authz matrix ---
  out.red_users = await call('/admin/user', { cookie: A.ck })
  out.red_projects = await call('/admin/project', { cookie: A.ck })
  out.red_users_member = await call('/admin/user', { cookie: U.ck })
  out.red_users_anon = await call('/admin/user')

  // --- sacrificial project (user's) ---
  out.sacrifice_create = await call('/project/new', {
    method: 'POST',
    cookie: U.ck,
    headers: { 'content-type': 'application/json', 'x-csrf-token': U.csrf },
    body: JSON.stringify({ projectName: 'p62-sacrifice' }),
  })
  if (out.sacrifice_create.status !== 200) throw new Error('sacrifice_create failed ' + out.sacrifice_create.status)
  const id = (JSON.parse(out.sacrifice_create.body) || {}).project_id || ''
  if (!/^[0-9a-f]{24}$/.test(id)) throw new Error('no sacrifice id')

  out.members_before = await call('/admin/project/' + id + '/members', { cookie: A.ck })
  out.admin_list = await call('/admin/user/null/projects', { method: 'POST', cookie: A.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': A.csrf }, body: listBody })
  out.usr_list = await call('/admin/user/' + ownerHex + '/projects', { method: 'POST', cookie: A.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': A.csrf }, body: JSON.stringify({ filters: {}, sort: { by: 'title', order: 'asc' }, page: { size: 20 } }) })
  out.bad_sort = await call('/admin/user/null/projects', { method: 'POST', cookie: A.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': A.csrf }, body: JSON.stringify({ filters: {}, sort: { by: 'bogus', order: 'desc' }, page: { size: 20 } }) })

  out.trash = await call('/admin/project/' + id + '/trash', { method: 'POST', cookie: A.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': A.csrf }, body: JSON.stringify({}) })
  out.trash_nobody = await call('/admin/project/' + id + '/trash', { method: 'POST', cookie: A.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': A.csrf }, body: JSON.stringify({ userId: ownerHex }) })
  out.untrash = await call('/admin/project/' + id + '/untrash', { method: 'POST', cookie: A.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': A.csrf }, body: JSON.stringify({ userId: ownerHex }) })

  out.invites_empty = await call('/admin/project/' + id + '/invites', { cookie: A.ck })
  out.invite_admin = await call('/admin/project/' + id + '/invite', { method: 'POST', cookie: A.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': A.csrf }, body: JSON.stringify({ userRef: 'e2e-admin@e2e.test', level: 'owner' }) })
  out.invites_after = await call('/admin/project/' + id + '/invites', { cookie: A.ck })
  out.members_after = await call('/admin/project/' + id + '/members', { cookie: A.ck })

  out.put_user = await call('/admin/project/' + id + '/users/' + ownerHex, { method: 'PUT', cookie: A.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': A.csrf }, body: JSON.stringify({ level: 'reader' }) })
  out.del_user = await call('/admin/project/' + id + '/users/' + ownerHex, { method: 'DELETE', cookie: A.ck, headers: { 'x-csrf-token': A.csrf } })

  out.sharing_link = await call('/admin/project/' + id + '/sharing-link', { cookie: A.ck })
  out.sharing_link_set = await call('/admin/project/' + id + '/sharing-link', { method: 'POST', cookie: A.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': A.csrf }, body: JSON.stringify({ readOnly: false, readAndWrite: false }) })

  out.del_soft = await call('/admin/project/' + id, { method: 'DELETE', cookie: A.ck, headers: { 'x-csrf-token': A.csrf } })
  out.admin_list_after_del = await call('/admin/user/null/projects', { method: 'POST', cookie: A.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': A.csrf }, body: JSON.stringify({ filters: { deleted: true }, sort: { by: 'deletedAt', order: 'desc' }, page: { size: 20 } }) })
  out.undelete = await call('/admin/project/' + id + '/undelete', { method: 'POST', cookie: A.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': A.csrf }, body: JSON.stringify({ userId: ownerHex }) })
  out.del_soft2 = await call('/admin/project/' + id, { method: 'DELETE', cookie: A.ck, headers: { 'x-csrf-token': A.csrf } })
  out.purge = await call('/admin/project/' + id + '/purge', { method: 'DELETE', cookie: A.ck, headers: { 'x-csrf-token': A.csrf } })
  out.purge_again = await call('/admin/project/' + id + '/purge', { method: 'DELETE', cookie: A.ck, headers: { 'x-csrf-token': A.csrf } })

  // --- member (non-admin) access ---
  out.member_members = await call('/admin/project/' + id + '/members', { cookie: U.ck })
  out.member_trash_notoken = await call('/admin/project/' + id + '/trash', { method: 'POST', cookie: U.ck, headers: { 'content-type': 'application/json' }, body: JSON.stringify({}) })
  out.member_trash_tok = await call('/admin/project/' + id + '/trash', { method: 'POST', cookie: U.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': U.csrf }, body: JSON.stringify({}) })
  out.member_list_json = await call('/admin/user/null/projects', { method: 'POST', cookie: U.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': U.csrf, accept: 'application/json' }, body: JSON.stringify({ filters: {}, sort: {}, page: {} }) })

  // --- anonymous ---
  out.anon_members = await call('/admin/project/' + id + '/members')
  out.anon_list = await call('/admin/user/null/projects', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ filters: {}, sort: {}, page: {} }) })

  // --- malformed project id ---
  out.badpid_members = await call('/admin/project/ZZZ/members', { cookie: A.ck })
  out.badpid_delete = await call('/admin/project/ZZZ', { method: 'DELETE', cookie: A.ck, headers: { 'x-csrf-token': A.csrf } })

  // --- active projects ---
  out.active = await call('/admin/active-projects', { cookie: A.ck })
  out.active_member = await call('/admin/active-projects', { cookie: U.ck })
  out.active_anon = await call('/admin/active-projects')

  return out
}

function firstDiff(a: string, b: string): number {
  const n = Math.min(a.length, b.length)
  for (let i = 0; i < n; i++) if (a[i] !== b[i]) return i
  return n
}
function ctx(a: string, b: string, i: number): string {
  const s0 = Math.max(0, i - 80)
  return `...A: ${a.slice(s0, i + 160).replace(/\n/g, '\\n')}\n...B: ${b.slice(s0, i + 160).replace(/\n/g, '\\n')}`
}
function diffLegs(name: string, a: Leg, b: Leg): string[] {
  const ds: string[] = []
  const keys = Object.keys(a).sort()
  for (const k of keys) {
    const A = a[k]
    const B = b[k]
    if (!B) {
      ds.push(`${name}:${k} missing in B`)
      continue
    }
    if (A.status !== B.status) {
      ds.push(`${name}:${k} status A=${A.status} B=${B.status}`)
      if (A.body || B.body) ds.push(ctx(A.body || '', B.body || '', 0))
      continue
    }
    if (A.ct !== B.ct) {
      ds.push(`${name}:${k} ct A='${A.ct}' B='${B.ct}' (status ${A.status})`)
      continue
    }
    let locA = A.loc || ''
    let locB = B.loc || ''
    if (k === 'member_members' || k === 'member_trash_tok') {
      // the sacrificial project id is re-minted each leg
      locA = locA.replace(/%2Fproject%2F[0-9a-f]{24}%2F/g, '%2Fproject%2FPID%2F')
      locB = locB.replace(/%2Fproject%2F[0-9a-f]{24}%2F/g, '%2Fproject%2FPID%2F')
    }
    if (locA !== locB) {
      ds.push(`${name}:${k} loc A='${locA}' B='${locB}' (status ${A.status})`)
      continue
    }
    let bodyA = norm(k, A.body || '')
    let bodyB = norm(k, B.body || '')
    if (bodyA !== bodyB) {
      ds.push(`${name}:${k} body len A=${A.len} B=${B.len}`)
      ds.push(ctx(bodyA, bodyB, firstDiff(bodyA, bodyB)))
    }
  }
  return ds
}

function prepSacrifice(): void {
  // deterministic start state: no lingering p62-sacrifice project (active or
  // deleted record) — purge removes both, so a simple name check suffices.
  const n = dexeQ(mongoC, 'db.projects.countDocuments({name:"p62-sacrifice"})')
  if (n !== '0') {
    // clean through the Node admin surface (flip is off in beforeAll)
    throw new Error('leftover p62-sacrifice project: ' + n + ' — run cleanup manually')
  }
}

test.describe('@local web-go P6.2 (admin-tools project surface) parity', () => {
  let U: { ck: string; csrf: string }
  let A: { ck: string; csrf: string }
  const LEG1: Leg = {}

  test.beforeAll(async () => {
    await waitGo()
    A = await login(ADMIN)
    U = await login(USER)
    prepSacrifice()
  })

  test('leg 1: Node baseline', async () => {
    flip('strip')
    await sleep(300)
    Object.assign(LEG1, await battery(U, A))
  })

  test('leg 2: Go parity', async () => {
    flip('apply')
    await sleep(800)
    const leg2 = await battery(U, A)
    flip('strip')
    await sleep(300)
    const ds = diffLegs('p62', LEG1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('leg 3: Node re-baseline', async () => {
    flip('strip')
    await sleep(300)
    const leg3 = await battery(U, A)
    const ds = diffLegs('p62', LEG1, leg3)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('pin sanity (oracle anchors)', async ({}, t) => {
    // hard anchors from the live Node oracle — fail fast with a clear label.
    if (!Object.keys(LEG1).length) t.skip('no leg-1 baseline captured (earlier leg failed)')
    expect(LEG1.red_users.status).toBe(301)
    expect(LEG1.red_users.loc).toBe('/hub#/site.general.users.all')
    expect(LEG1.red_users_member.loc).toBe('/restricted?from=%2Fadmin%2Fuser')
    expect(LEG1.admin_list.status).toBe(500)
    expect(LEG1.invite_admin.status).toBe(400)
    const inv = JSON.parse(LEG1.invite_admin.body)
    expect(inv.error).toBe('Validation error: Invalid input: expected string, received undefined at "body.email"; Invalid option: expected one of "readOnly"|"readAndWrite"|"review" at "body.privileges"; Unrecognized keys: "userRef", "level" at "body"')
    expect(inv.statusCode).toBe(400)
    const pu = JSON.parse(LEG1.put_user.body)
    expect(pu.error).toBe('Validation error: Invalid option: expected one of "readOnly"|"readAndWrite"|"review" at "body.privilegeLevel"; Unrecognized key: "level" at "body"')
    expect(JSON.parse(LEG1.undelete.body).name).toBe('p62-sacrifice (Restored)')
    expect(LEG1.sharing_link.status).toBe(404)
    expect(LEG1.sharing_link.body).toBe('Not Found')
    expect(LEG1.sharing_link_set.status).toBe(400)
    expect(LEG1.undelete.body).toContain('\"name\":\"p62-sacrifice (Restored)\"')
    expect(LEG1.purge.status).toBe(200)
    expect(LEG1.purge_again.status).toBe(200)
    expect(LEG1.badpid_members.status).toBe(500)
    expect(LEG1.active.status).toBe(200)
    expect(LEG1.active.body).toBe('[]')
    expect(LEG1.anon_list.status).toBe(403)
    expect(LEG1.member_list_json.status).toBe(302)
    expect(LEG1.member_list_json.ct).toBe('')
  })
})
