/**
 * WEB-GO P6.3b FLIP GATE (WEB_GO_PLAN.md P6.3b — admin-tools user surface,
 * mutation side):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p63b.conf):
 *     POST   /admin/user/create            create user (+admin extras + mail)
 *     POST   /admin/user/:userId/send-activation
 *     POST   /admin/user/:userId/update    (name / email / canManageTemplates)
 *     POST   /admin/user/:userId/delete    (audit + deletedUsers + projects + mail)
 *     POST   /admin/user/:userId/restore   (re-insert + undelete owned projects)
 *     DELETE /admin/user/:userId           (purge/redact record)
 *
 *   leg 1  Node baseline       (p63b flip OFF)
 *   leg 2  FLIP ON — Go        (flips ON, battery, flip OFF)
 *   leg 3  Node re-baseline    (flip OFF)
 *
 *   Battery mirrors the Node oracle (/tmp/p63b_oracle.mjs -> /tmp/p63b_node.json,
 *   live 2026-09-16): authz matrix on the mutation surface, create battery
 *   (local/dup/invalid/empty/external+admin+templates), send-activation
 *   (ok/ghost/bad-hex), update battery (name trim, email swap w/ mail +
 *   audit, dup, invalid, canManageTemplates t/f, site-admin 409, no-op,
 *   ghost 500 both accepts), delete lifecycle (mail / again / restore /
 *   restore-again / skip-mail / purge / purge-again / purge-ghost) and
 *   list pins around the sacrificial users — plus side-effect parity:
 *   mail sink (count/to/subject), password token counts, userAuditLog
 *   entries, deletedUsers record lifecycle, user doc shape (key set +
 *   key count + emails[] swap).
 *
 *   Volatile normalization before diff: 64-hex tokens -> TOK, 24-hex
 *   object ids -> UID, ISO-millis dates -> TS.
 */
import { execFileSync } from 'node:child_process'
import { test, expect } from '@playwright/test'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const FLIPCONF = 'web-p63b.conf'
const BASE = 'http://127.0.0.1:7420'
const SINK = 'http://127.0.0.1:18025'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const SAC1 = 'p63b-sac1@e2e.test'
const SAC1M = 'p63b-sac1m@e2e.test'
const SAC2 = 'p63b-sac2@e2e.test'
const GHOST = 'aaaa1111bbbb2222cccc3333'
const BADHEX = 'not-a-hex-id'
const SACS = [SAC1, SAC1M, SAC2]

type Leg = Record<string, { status: number; ct: string; loc?: string | null; body: string; len: number }>
type FX = Record<string, any>

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
    return (e.stdout ? e.stdout.toString() : '') || (e.stderr ? e.stderr.toString() : '') || ''
  }
}
function dexe(c: string, cmd: string, capture = false): string {
  try {
    const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore', maxBuffer: 64 * 1024 * 1024 })
    return out || ''
  } catch (e: any) {
    if (capture) throw e
    return (e.stdout ? e.stdout.toString() : '') || ''
  }
}
function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 64 * 1024 * 1024 })
}

async function waitGo(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    if (dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim() === '200') return
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
      const body = buf.toString('latin1')
      if (process.env.P63B_DEBUG) console.log(`[P63B] ${init.method || 'GET'} ${p} -> ${r.status} ck=${(h.cookie || 'none').slice(0, 30)} sc=${(setcookie || '').slice(0, 30)} body=${body.slice(0, 60).replace(/\n/g, ' ')}`)
      return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location'), body, len: buf.length, setcookie }
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

// ---------- volatile normalization ----------
const norm = (s: string): string =>
  s
    .replace(/token=[0-9a-f]{64}/gi, 'token=TOK')
    .replace(/[0-9a-f]{64}/g, 'TOK')
    .replace(/[0-9a-f]{24}/g, 'UID')
    .replace(/20\d{2}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z/g, 'TS')
    .replace(/20\d{2}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z/g, 'TS')

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
    if ((a.loc || '') !== (b.loc || '')) ds.push(`${label}:${k} loc A='${a.loc}' B='${b.loc}'`)
    const na = norm(a.body)
    const nb = norm(b.body)
    if (na !== nb) {
      const i = na.findIndex((c, i2) => c !== nb[i2])
      ds.push(`${label}:${k} body@${i} | A~${na.slice(Math.max(0, i - 60), i + 140).replace(/\n/g, ' ')} || B~${nb.slice(Math.max(0, i - 60), i + 140).replace(/\n/g, ' ')}`)
    }
  }
  return ds
}

function diffFx(label: string, A: FX, B: FX): string[] {
  const ds: string[] = []
  const ks = [...new Set([...Object.keys(A), ...Object.keys(B)])].sort()
  for (const k of ks) {
    const a = JSON.stringify(norm(JSON.stringify(A[k] ?? null)))
    const b = JSON.stringify(norm(JSON.stringify(B[k] ?? null)))
    if (a !== b) ds.push(`${label}:fx ${k}\n  A=${a.slice(0, 400)}\n  B=${b.slice(0, 400)}`)
  }
  return ds
}

// ---------- side-effect probes (server-agnostic, direct mongo + sink) ----------

function fxDocProfile(email: string): any {
  const src = msh(`const u = db.users.findOne({email: ${JSON.stringify(email)}});
  if (!u) print(JSON.stringify({absent: true})); else {
    const out = {
      keyCount: Object.keys(u).length,
      keySet: Object.keys(u).sort().join(','),
      email: u.email,
      firstName: u.first_name,
      lastName: u.last_name,
      holdingAccount: u.holdingAccount,
      hasHash: !!u.hashedPassword,
      thirdParty: Array.isArray(u.thirdPartyIdentifiers) ? u.thirdPartyIdentifiers.length : null,
      flags: u.flags ? (u.flags.canManageTemplates ? 'T' : 'F') : null,
      hasLPEC: 'lastPrimaryEmailCheck' in u ? 'Y' : 'N',
      emails: (u.emails || []).map(e => e.email + '|' + Object.keys(e).sort().join(',')),
      deleted: u.deleted === true,
      suspended: u.suspended === true,
    }
    print(JSON.stringify(out))
  }`, true).trim().split('\n').pop() || '{}'
  try {
    return JSON.parse(src)
  } catch {
    return { raw: src.slice(0, 200) }
  }
}

function fxAudit(email: string): any {
  const src = msh(`const u = db.users.findOne({email: ${JSON.stringify(email)}});
  const rec = db.deletedUsers.findOne({user: {email: ${JSON.stringify(email)}}});
  const ids = [];
  if (u) ids.push(u._id.toLocaleString());
  if (rec) ids.push(rec.user._id.toLocaleString());
  const rows = db.userAuditLogEntries.find({userId: {$in: ids.map(id => ObjectId(id))}}).toArray()
    .map(e => e.operation + ' ' + JSON.stringify(e.info))
    .sort()
  print(JSON.stringify(rows))`, true)
  const line = src.trim().split('\n').filter((l) => l.startsWith('[') || l.startsWith('{')).pop() || '[]'
  try {
    return JSON.parse(line)
  } catch {
    return 'PARSE_ERR:' + line.slice(0, 200)
  }
}

function fxRecord(mode: 'email' | 'delid', val: string): any {
  const filt = mode === 'email'
    ? `{'user.email': ${JSON.stringify(val)}}`
    : `{'deleterData.deletedUserId': ObjectId(${JSON.stringify(val)})}`
  const src = msh(`const r = db.deletedUsers.findOne(${filt});
  if (!r) { print(JSON.stringify({record: false})); } else {
    print(JSON.stringify({
      record: true,
      hasUser: !!r.user,
      userEmail: r.user ? r.user.email : null,
      hasDD: !!r.deleterData,
      ddKeys: r.deleterData ? Object.keys(r.deleterData).sort().join(',') : '',
      hasIp: !!(r.deleterData && r.deleterData.deleterIpAddress),
      hasDeleterId: !!(r.deleterData && r.deleterData.deleterId),
      hasDeletedUserId: !!(r.deleterData && r.deleterData.deletedUserId),
      hasDeletedAt: !!(r.deleterData && r.deleterData.deletedAt),
    }))
  }`, true)
  const line = src.trim().split('\n').filter((l) => l.startsWith('{')).pop() || '{}'
  try {
    return JSON.parse(line)
  } catch {
    return { raw: line.slice(0, 200) }
  }
}

function fxTokens(email: string): string {
  const src = msh(`const n = db.tokens.countDocuments({use: 'password', 'data.email': ${JSON.stringify(email)}});
  print(JSON.stringify({n}))`, true)
  const line = src.trim().split('\n').filter((l) => l.startsWith('{')).pop() || '{}'
  try {
    return JSON.stringify(JSON.parse(line))
  } catch {
    return line.slice(0, 80)
  }
}

async function sinkReset(): Promise<void> {
  await fetch(SINK + '/api/messages', { method: 'DELETE' }).catch(() => undefined)
}
async function sinkMail(): Promise<any[]> {
  try {
    const r = await fetch(SINK + '/api/messages')
    const msgs = await r.json()
    return (Array.isArray(msgs) ? msgs : msgs.messages || [])
      .filter((m: any) => (m.to || []).some((t: string) => SACS.some((s) => String(t).includes(s.replace('@e2e.test', '')))))
      .map((m: any) => ({ to: (m.to || []).join(','), subject: m.subject }))
  } catch {
    return []
  }
}

function cleanSacs(): void {
  msh(`
  const EMS = ${JSON.stringify(SACS)}
  const ids = []
  db.users.find({email: {$in: EMS}}).forEach(u => ids.push(u._id.toLocaleString()))
  db.deletedUsers.find({user: {email: {$in: EMS}}}).forEach(r => ids.push(r.user._id.toLocaleString()))
  const oids = ids.map(id => ObjectId(id))
  if (oids.length) {
    db.users.deleteMany({email: {$in: EMS}})
    db.deletedUsers.deleteMany({user: {email: {$in: EMS}}})
    db.tokens.deleteMany({$or: [{'data.email': {$in: EMS}}, {'data.user_id': {$in: ids}}]})
    db.userAuditLogEntries.deleteMany({userId: {$in: oids}})
  }`, true)
}

// ---------- the battery (mirrors /tmp/p63b_oracle.mjs) ----------

async function battery(A: { ck: string; csrf: string }, M: { ck: string; csrf: string }): Promise<{ pins: Leg; fx: FX; ids: any }> {
  cleanSacs()
  await sinkReset()
  await sleep(400)
  const AH = { cookie: A.ck, headers: { ...JS, 'x-csrf-token': A.csrf } }
  const out: Leg = {}
  const fx: FX = {}
  const P = JSON.stringify
  const post = (path: string, body?: any) =>
    call(path, { method: 'POST', cookie: A.ck, headers: { ...JS, 'x-csrf-token': A.csrf }, body: body !== undefined ? (typeof body === 'string' ? body : P(body)) : undefined })

  // --- authz matrix (mutation surface) ---
  out.anon_create = await call('/admin/user/create', { method: 'POST', headers: JS, body: P({ email: 'anon-probe@e2e.test' }) })
  out.anon_create_html = await call('/admin/user/create', { method: 'POST', headers: { 'content-type': 'application/x-www-form-urlencoded', accept: 'text/html' }, body: '' })
  out.anon_purge = await call(`/admin/user/${GHOST}`, { method: 'DELETE', headers: JS })
  out.member_create = await call('/admin/user/create', { method: 'POST', cookie: M.ck, headers: { ...JS, 'x-csrf-token': M.csrf }, body: P({ email: 'member-probe@e2e.test' }) })
  out.member_purge = await call(`/admin/user/${GHOST}`, { method: 'DELETE', cookie: M.ck, headers: JS })

  // --- phase A ---
  await pin2('a_create_local', { path: '/admin/user/create', method: 'POST', ...AH, body: P({ email: SAC1 }) })
  const sac1Id = out.a_create_local.status === 200 ? JSON.parse(out.a_create_local.body).user.id : null
  fx.doc_after_create_sac1 = fxDocProfile(SAC1)
  await pin2('a_create_dup', { path: '/admin/user/create', method: 'POST', ...AH, body: P({ email: SAC1 }) })
  await pin2('a_create_inval', { path: '/admin/user/create', method: 'POST', ...AH, body: P({ email: 'nope' }) })
  await pin2('a_create_empty', { path: '/admin/user/create', method: 'POST', ...AH, body: P({}) })
  await pin2('a_create_ext', { path: '/admin/user/create', method: 'POST', ...AH, body: P({ email: SAC2, isExternal: true, isAdmin: true, canManageTemplates: true }) })
  const sac2Id = out.a_create_ext.status === 200 ? JSON.parse(out.a_create_ext.body).user.id : null
  fx.doc_after_create_sac2 = fxDocProfile(SAC2)

  await pin2('a_info_link', { path: `/admin/user/${sac1Id}/info`, method: 'GET', ...AH })
  await pin2('a_info_ext', { path: `/admin/user/${sac2Id}/info`, method: 'GET', ...AH })
  await pin2('a_sendact_ok', { path: `/admin/user/${sac1Id}/send-activation`, method: 'POST', ...AH, body: P({}) })
  await pin2('a_sendact_ghost', { path: `/admin/user/${GHOST}/send-activation`, method: 'POST', ...AH, body: P({}) })
  await pin2('a_sendact_badhex', { path: `/admin/user/${BADHEX}/send-activation`, method: 'POST', ...AH, body: P({}) })

  fx.tokens_after_sendact_sac1 = fxTokens(SAC1)
  fx.tokens_sac2 = fxTokens(SAC2)

  await pin2('a_upd_name', { path: `/admin/user/${sac1Id}/update`, method: 'POST', ...AH, body: P({ firstName: 'Sac ', lastName: 'Rifice' }) })
  await pin2('a_upd_email', { path: `/admin/user/${sac1Id}/update`, method: 'POST', ...AH, body: P({ email: SAC1M }) })
  fx.doc_after_email_sac1m = fxDocProfile(SAC1M)
  fx.audit_after_updates_sac1m = fxAudit(SAC1M)
  await pin2('a_upd_email_dup', { path: `/admin/user/${sac1Id}/update`, method: 'POST', ...AH, body: P({ email: 'e2e-user@e2e.test' }) })
  await pin2('a_upd_email_bad', { path: `/admin/user/${sac1Id}/update`, method: 'POST', ...AH, body: P({ email: 'not-an-email' }) })
  await pin2('a_upd_tm_admin', { path: `/admin/user/6aa4b8a873ef0e5094f4cba3/update`, method: 'POST', ...AH, body: P({ canManageTemplates: false }) })
  await pin2('a_upd_tm_sac_t', { path: `/admin/user/${sac1Id}/update`, method: 'POST', ...AH, body: P({ canManageTemplates: true }) })
  await pin2('a_upd_tm_sac_f', { path: `/admin/user/${sac1Id}/update`, method: 'POST', ...AH, body: P({ canManageTemplates: false }) })
  await pin2('a_upd_none', { path: `/admin/user/${sac1Id}/update`, method: 'POST', ...AH, body: P({ lastName: 'Rifice' }) })
  await pin2('a_upd_ghost', { path: `/admin/user/${GHOST}/update`, method: 'POST', ...AH, body: P({ firstName: 'X' }) })
  await pin2('a_upd_ghost_htm', { path: `/admin/user/${GHOST}/update`, method: 'POST', cookie: A.ck, headers: { 'content-type': 'application/json', accept: 'text/html', 'x-csrf-token': A.csrf }, body: P({ firstName: 'X' }) })
  await pin2('a_list_target', { path: '/admin/users', method: 'POST', ...AH, body: P({ filters: { search: 'p63b-sac' } }) })
  fx.mailA = await sinkMail()
  fx.tokens_after_phaseA = { sac1: fxTokens(SAC1), sac1m: fxTokens(SAC1M), sac2: fxTokens(SAC2) }

  // --- phase B ---
  await pin2('b_del_mail', { path: `/admin/user/${sac1Id}/delete`, method: 'POST', ...AH, body: P({ sendEmail: true }) })
  fx.record_after_delete = fxRecord('email', SAC1M)
  fx.audit_after_del1 = fxAudit(SAC1M)
  await pin2('b_del_again', { path: `/admin/user/${sac1Id}/delete`, method: 'POST', ...AH, body: P({}) })
  await pin2('b_restore', { path: `/admin/user/${sac1Id}/restore`, method: 'POST', ...AH, body: P({}) })
  fx.doc_after_restore = fxDocProfile(SAC1M)
  await pin2('b_restore_again', { path: `/admin/user/${sac1Id}/restore`, method: 'POST', ...AH, body: P({}) })
  await pin2('b_del_skip', { path: `/admin/user/${sac1Id}/delete`, method: 'POST', ...AH, body: P({}) })
  fx.audit_after_del2 = fxAudit(SAC1M)
  await pin2('b_purge', { path: `/admin/user/${sac1Id}`, method: 'DELETE', ...AH })
  fx.record_after_purge = fxRecord('delid', sac1Id)
  await pin2('b_purge_again', { path: `/admin/user/${sac1Id}`, method: 'DELETE', ...AH })
  fx.record_after_purge2 = fxRecord('delid', sac1Id)
  await pin2('b_purge_ghost', { path: `/admin/user/${GHOST}`, method: 'DELETE', ...AH })
  await pin2('b_list_target', { path: '/admin/users', method: 'POST', ...AH, body: P({ filters: { search: 'p63b-sac' } }) })
  await pin2('b_ext_info', { path: `/admin/user/${sac2Id}/info`, method: 'GET', ...AH })
  fx.mailB = await sinkMail()

  return { pins: out, fx, ids: { sac1Id, sac2Id } }

  async function pin2(name: string, init: any) {
    const r = await call(init.path, init)
    out[name] = { status: r.status, ct: r.ct, loc: r.loc || null, body: r.body, len: r.len }
  }
}

// ---------- gate ----------

test.describe(`@local web-go P6.3b (admin-tools user surface mutations) parity`, () => {
  let A: { ck: string; csrf: string }
  let M: { ck: string; csrf: string }
  const L1: { pins: Leg; fx: FX } = { pins: {}, fx: {} }
  let FLIPPED = false

  test.beforeAll(async () => {
    A = await login(ADMIN)
    M = await login(USER)
  })

  test.afterAll(() => {
    if (FLIPPED) {
      try {
        flip('strip')
      } catch {
        // best-effort restore
      }
    }
    cleanSacs()
  })

  test('leg 1: Node baseline', async () => {
    flip('strip')
    const res1 = await battery(A, M) as any
    Object.assign(L1, res1)
    if (process.env.P63B_DEBUG) {
      console.log(`[P63B] A.ck=${(A.ck || '').slice(0, 30)} A.csrf?=${!!A.csrf} M.ck?=${!!M.ck}`)
      const c = res1.pins.a_create_local
      console.log(`[P63B] leg1 a_create_local status=${c && c.status} body=${(c && c.body || '').slice(0, 100)}`)
    }
  })

  test('leg 2: Go parity', async ({}, t) => {
    await waitGo()
    // fail-fast: a reaped admin account would make both legs identically
    // 403 and render the diff vacuous — refuse to compare on that basis.
    const g1 = L1.pins.a_create_local
    if (!g1 || g1.status !== 200) throw new Error(`leg-1 baseline not valid (a_create_local status=${g1 ? g1.status : 'missing'}) — sacrificial/admin state interfered, re-run`)
    flip('apply')
    FLIPPED = true
    let leg2: { pins: Leg; fx: FX } = { pins: {}, fx: {} }
    try {
      leg2 = await battery(A, M)
    } finally {
      flip('strip')
      FLIPPED = false
    }
    const ds = [...diffLegs('p63b', L1.pins, leg2.pins), ...diffFx('p63b', L1.fx, leg2.fx)]
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
    void t
  })

  test('leg 3: Node re-baseline', async () => {
    flip('strip')
    if (!L1.pins.a_create_local || L1.pins.a_create_local.status !== 200) throw new Error('leg-1 baseline invalid — re-run')
    const leg3 = await battery(A, M)
    const ds = [...diffLegs('p63b', L1.pins, leg3.pins), ...diffFx('p63b', L1.fx, leg3.fx)]
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('pin sanity (oracle anchors from /tmp/p63b_node.json)', async ({}, t) => {
    if (!Object.keys(L1.pins).length) t.skip('no leg-1 baseline captured (earlier leg failed)')
    const P = L1.pins
    // authz
    expect(P.anon_create.status).toBe(403)
    expect(P.member_create.status).toBe(302)
    expect((P.member_create.loc || '')).toBe('/restricted?from=%2Fadmin%2Fuser%2Fcreate')
    // create
    expect(P.a_create_local.status).toBe(200)
    const c = JSON.parse(P.a_create_local.body)
    expect(c.user.email).toBe(SAC1)
    expect(c.user.firstName).toBe('p63b-sac1')
    expect(c.user.inactive).toBe(true)
    expect(c.user.deleted).toBe(false)
    expect(c.user.authMethods).toEqual(['local'])
    expect(c.emailIsNotSent).toBe(false)
    expect(P.a_create_dup.status).toBe(409)
    expect(P.a_create_dup.body).toBe(JSON.stringify({ message: 'email_already_registered' }))
    expect(P.a_create_inval.status).toBe(422)
    expect(P.a_create_inval.body).toBe(JSON.stringify({ message: 'email_address_is_invalid' }))
    expect(P.a_create_empty.status).toBe(422)
    expect(P.a_create_empty.body).toBe(JSON.stringify({ message: 'Email address is empty' }))
    const ce = JSON.parse(P.a_create_ext.body)
    expect(ce.user.isAdmin).toBe(true)
    expect(ce.user.authMethods).toEqual([])
    // send-activation
    expect(P.a_sendact_ok.status).toBe(200)
    expect(P.a_sendact_ok.body).toBe('OK')
    expect(P.a_sendact_ghost.status).toBe(422)
    expect(P.a_sendact_ghost.body).toBe(
      JSON.stringify({ message: 'Error sending activation email. Please check your SMTP configuration.' }),
    )
    // update
    expect(P.a_upd_name.body).toBe(JSON.stringify({ firstName: 'Sac', lastName: 'Rifice' }))
    expect(P.a_upd_email.body).toBe(JSON.stringify({ email: SAC1M }))
    expect(P.a_upd_email_dup.body).toBe(
      JSON.stringify({ message: 'This email address is already associated with a different Overleaf account.' }),
    )
    expect(P.a_upd_email_bad.body).toBe(JSON.stringify({ message: 'Email address is invalid' }))
    expect(P.a_upd_tm_admin.status).toBe(409)
    expect(P.a_upd_tm_admin.body).toBe(
      JSON.stringify({
        userId: '6aa4b8a873ef0e5094f4cba3',
        message: 'Site admins are template gallery admins implicitly and cannot be removed from this role here.',
      }),
    )
    expect(P.a_upd_tm_sac_t.body).toBe(JSON.stringify({ flags: { canManageTemplates: true } }))
    expect(P.a_upd_tm_sac_f.body).toBe(JSON.stringify({ flags: { canManageTemplates: false } }))
    expect(P.a_upd_none.body).toBe('{}')
    expect(P.a_upd_ghost.status).toBe(500)
    expect(P.a_upd_ghost.len).toBe(681)
    expect(P.a_upd_ghost_htm.status).toBe(500)
    expect(P.a_upd_ghost_htm.len).toBe(681)
    // lifecycle
    expect(P.b_del_mail.status).toBe(200)
    expect(JSON.parse(P.b_del_mail.body).deletedAt).toMatch(/^20\d{2}-/)
    expect(P.b_del_again.body).toBe(JSON.stringify({ message: 'Something went wrong. Does the account still exist?' }))
    const br = JSON.parse(P.b_restore.body)
    expect(br.restoredId).toMatch(/^[0-9a-f]{24}$/)
    expect(br.email).toBe(SAC1M)
    expect(P.b_restore_again.body).toBe(JSON.stringify({ message: 'Something went wrong. The user is purged?' }))
    expect(P.b_del_skip.status).toBe(200)
    expect(P.b_purge.status).toBe(200)
    expect(P.b_purge.body).toBe('OK')
    expect(P.b_purge_again.body).toBe('OK')
    expect(P.b_purge_ghost.body).toBe(JSON.stringify({ message: 'Something went wrong. The user is already deleted?' }))
    // side effects
    expect(L1.fx.mailA).toEqual([
      { to: SAC1, subject: 'Activate your OlliTeX Account' },
      { to: SAC1, subject: 'Activate your OlliTeX Account' },
      { to: SAC1, subject: 'Overleaf security note: change of primary email address' },
      { to: SAC1M, subject: 'Overleaf security note: change of primary email address' },
    ])
    expect(L1.fx.mailB).toEqual([
      { to: SAC1, subject: 'Activate your OlliTeX Account' },
      { to: SAC1, subject: 'Activate your OlliTeX Account' },
      { to: SAC1, subject: 'Overleaf security note: change of primary email address' },
      { to: SAC1M, subject: 'Overleaf security note: change of primary email address' },
      { to: SAC1M, subject: 'Overleaf security note: account deleted' },
    ])
    expect(JSON.parse(L1.fx.tokens_after_sendact_sac1).n).toBe(2)
    expect(JSON.parse(L1.fx.tokens_sac2).n).toBe(0)
    expect(L1.fx.audit_after_updates_sac1m).toEqual([
      'add-email {"newSecondaryEmail":"' + SAC1M + '"}',
      'change-primary-email {"newPrimaryEmail":"' + SAC1M + '","oldPrimaryEmail":"' + SAC1 + '"}',
      'remove-email {"removedEmail":"' + SAC1 + '"}',
    ])
    expect(L1.fx.record_after_delete.record).toBe(true)
    expect(L1.fx.record_after_delete.hasUser).toBe(true)
    expect(L1.fx.record_after_delete.userEmail).toBe(SAC1M)
    expect(L1.fx.record_after_purge.record).toBe(true)
    expect(L1.fx.record_after_purge.hasUser).toBe(false)
    expect(L1.fx.record_after_purge.hasIp).toBe(false)
    expect(L1.fx.record_after_purge.hasDeleterId).toBe(true)
    const d2 = L1.fx.doc_after_create_sac2
    expect(d2.hasHash).toBe(false)
    expect(d2.flags).toBe('T')
    const d1 = L1.fx.doc_after_create_sac1
    expect(d1.hasHash).toBe(true)
    expect(d1.flags).toBe('F')
    expect(d1.emails[0]).toContain(SAC1)
  })
})
