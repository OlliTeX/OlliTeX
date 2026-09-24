/**
 * P6.20 flip gate — launchpad module (first-admin bootstrap), the LAST P6 flip
 * (WEB_GO_PLAN.md P6.20). 4-leg contract-parity gate, same harness family as
 * P6.5…P6.9:
 *   leg 0: flips stripped (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a…P6.9 + P6.20) → Go
 *   leg 3: flips stripped → Node re-baseline (determinism anchor)
 *
 * Battery (identical order on both legs). Launchpad behavior is state-bound
 * ("does an admin exist"), so cells reset the shared state before them:
 *
 *   FRESH cell (e2e-admin demoted to isAdmin:false — the "first boot" world):
 *     F1  anon GET /launchpad                        → 200 fresh page (byte-parity bake)
 *     F2  POST /launchpad/register_admin {}          → 400 Bad Request
 *         POST …/register_admin {email:"bad-email"}  → 400 (password missing)
 *         POST …/register_admin {email, password:"abc"}   → 400 {"message":{"type":"error","text":"password is too short"}}
 *         POST …/register_admin {email,"pass|word9"}      → 400 {"message":{"type":"error","text":"password contains an invalid character"}}
 *     F3  POST …/register_admin happy                → 200 {"redir":"/launchpad"}
 *         mongo: fresh LOCAL doc shape pinned
 *              (isAdmin, holdingAccount:false, first_name=local part, NO last_name,
 *               hashedPassword set, analyticsId set, emails[0] {email, reversedHostname, createdAt})
 *         POST …/register_admin (again)              → 403 {"message":{"type":"error","text":"admin user already exists"}}
 *         anon GET /launchpad                        → 302 /login
 *     F4  (fresh again) POST …/register_saml_admin {email} → 403 Forbidden (authMethod≠'saml')
 *         POST …/register_ldap_admin {}              → 400 Bad Request
 *         POST …/register_ldap_admin happy           → 200 {"redir":"/launchpad","email":"<email>"}
 *         mongo: fresh EXTERNAL doc shape pinned
 *              (first_name=FULL email, last_name "", NO hashedPassword, confirmedAt number,
 *               reversedHostname, emails[0])
 *         POST …/register_ldap_admin (again)         → 403 Forbidden (admin exists)
 *
 *   DEFAULT cell (canonical: e2e-admin admin again):
 *     D1  anon GET /launchpad                        → 302 /login
 *     D2  POST …/register_admin happy                → 403 {"message":{"type":"error","text":"admin user already exists"}}
 *     D3  POST …/register_ldap_admin {email}         → 403 Forbidden (admin exists; method gate passes on 'ldap')
 *         POST …/register_saml_admin {email}         → 403 Forbidden (method gate first)
 *
 *   LOGGED cells:
 *     L0  admin (e2e-admin) GET /launchpad           → 200 admin page (byte-parity bake; ETag shape)
 *     L1  admin POST …/send_test_email {}            → 400 {"message":"no email address supplied"}
 *     L2  admin POST …/send_test_email {email}       → 200 {"message":"Email Sent"}
 *         smtp sink: 1 message, from noreply@e2e.test, to the target,
 *         subject "A Test Email from OlliTeX", body "This is a test Email from OlliTeX"
 *     U0  non-admin (e2e-user) GET /launchpad        → 302 /restricted
 *     U1  non-admin POST …/send_test_email {email}   → 302 /restricted?from=%2Flaunchpad%2Fsend_test_email
 *     A2  anon POST …/send_test_email {email}        → 302 /login (requireGlobalLogin)
 *
 * Fixture integrity (finally, every leg): e2e-admin isAdmin:true restored,
 * e2e-tpladmin not-admin + canManageTemplates, e2e-user not-admin, gate users
 * (p620-local/p620-ldap) absent.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const FLIPCONFS = [
  'web-p64a.conf',
  'web-p64b.conf',
  'web-p65.conf',
  'web-p66.conf',
  'web-p67.conf',
  'web-p68.conf',
  'web-p69.conf',
  'web-p620.conf',
]
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const SINK = process.env.E2E_SMTP_SINK || 'http://127.0.0.1:18025'
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const LOCAL_NEW = 'p620-local@e2e.test'
const LDAP_NEW = 'p620-ldap@e2e.test'
const MAIL_TO = 'p620-mail@e2e.test'
const UA = 'p620-gate'

type Leg = Record<string, { status: number; ct: string; loc: string; body: string; etag?: string }>

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
    dexeStrict(
      overleafC,
      `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"`,
    )
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
    dexeStrict(
      overleafC,
      `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';let s=fs.readFileSync(p,'utf8');const inc='  include /etc/nginx/overleaf-flips/${conf};'+String.fromCharCode(10);if(!s.includes('overleaf-flips/${conf}')){if(s.includes('location / {')){s=s.replace('location / {',inc+'location / {',1)}else{throw new Error('anchor not found')}};fs.writeFileSync(p,s)"`,
    )
  }
  dexeStrict(overleafC, 'nginx -t && nginx -s reload')
  await sleep(1200)
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(1)
}

function normBody(b: string): string {
  // Per-request / per-session randomness on BOTH stacks (P3c proven suite) —
  // launchpad adds no new classes (same nonce/csrf/sid/dates vocabulary).
  return b
    .replace(/overleaf\.sid=s%3A[^;,\s"\\]+/g, 'overleaf.sid=SID.')
    .replace(/"sid":"[^"]*"/g, '"sid":"SID"')
    .replace(/Expires=[^;,\s]+/g, 'Expires=EX')
    .replace(
      /\b(20\d{2}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\b/g,
      'TS',
    )
    .replace(/nonce-[A-Za-z0-9+/=~]{16,}/g, 'nonce-NONCE')
    .replace(/nonce="[^"]+"/g, 'nonce="N"')
    .replace(/<meta name="ol-csrfToken" content="[^"]*"/g, '<meta name="ol-csrfToken" content="CSRF"')
    .replace(/"ol-csrfToken":"[^"]*"/g, '"ol-csrfToken":"CSRF"')
    .replace(/<input name="_csrf" type="hidden" value="[^"]*"/g, '<input name="_csrf" type="hidden" value="CSRF"')
    .replace(/"(first_name|last_name|role|institution|email|customKeybindings|session_created|created_at|updated_at|lastActive|lastLoginIp|lastLoggedIn|session_ip)":\s*("[^"]*"|\d+|\[\s*\]|null)/g, '"$1":"X"')
    .replace(/\[\s*\{\s*"(device|name|version|ip|sid|created_at|session_created)"[^\]]*(\{[^}]*\}?)?\s*\?\s*\]/g, '[]')
}

function normEtag(e: string | undefined): string {
  // Weak ETag, W/"<bodylen-hex>-<digest>" — pin shape + body length (byte parity).
  const m = /^W\/"([0-9a-f]+)-([A-Za-z0-9+/=]+)"$/.exec(e || '')
  return m ? `LEN_${parseInt(m[1], 16)}_DIGEST${m[2].length}` : (e || '').slice(0, 40)
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
    if (a.etag !== undefined || b.etag !== undefined) {
      if (normEtag(a.etag) !== normEtag(b.etag)) ds.push(`${label}:${k} etag A='${a.etag}' B='${b.etag}'`)
    }
    const an = normBody(a.body)
    const bn = normBody(b.body)
    if (an !== bn) {
      // first divergence window for a fast pointer
      let i = 0
      while (i < an.length && i < bn.length && an[i] === bn[i]) i++
      ds.push(`${label}:${k} body @${i} A~'${an.slice(Math.max(0, i - 60), i + 100)}' || B~'${bn.slice(Math.max(0, i - 60), i + 100)}'`)
    }
  }
  return ds
}

// ---- state cells -----------------------------------------------------------

function resetLocalAdmin(fresh: boolean): string {
  // fresh=true → the "boot" world (no admin exists); false → canonical.
  // Only e2e-admin is touched (tpladmin/user carry no isAdmin:true). Gate users
  // are always wiped for a level start.
  return msh('db.users.deleteMany({email:{$in:["p620-local@e2e.test","p620-ldap@e2e.test"]}});const a=db.users.findOne({email:"e2e-admin@e2e.test"});db.users.updateOne({_id:a._id},{$set:{isAdmin:' + (fresh ? 'false' : 'true') + '}});print(db.users.countDocuments({isAdmin:true}))')
}

function fixturesIntact(): void {
  const admin = msh('const u=db.users.findOne({email:"e2e-admin@e2e.test"});print([u.isAdmin, (!!u.canManageTemplates)]+"")')
  const tpl = msh('const u=db.users.findOne({email:"e2e-tpladmin@e2e.test"});print([u.isAdmin===true, u.canManageTemplates===true]+"")')
  const usr = msh('const u=db.users.findOne({email:"e2e-user@e2e.test"});print([u.isAdmin===true]+"")')
  const gone = msh('print(db.users.countDocuments({email:{$in:["p620-local@e2e.test","p620-ldap@e2e.test"]}}))')
  expect(admin).toBe('true,false')
  expect(tpl).toBe('false,true')
  expect(usr).toBe('false')
  expect(gone).toBe('0')
}

function userShape(email: string): string {
  return msh(
    `const u=db.users.findOne({email:'${email}'});
   const e0=(u.emails||[])[0]||{};
   print([u.isAdmin, u.holdingAccount, u.first_name, 'ln='+(u.last_name===undefined?'∅':u.last_name), !!u.hashedPassword, !!u.analyticsId,
   (u.emails||[]).length, e0.email, e0.reversedHostname, !!e0.createdAt, (typeof e0.confirmedAt)+'')+'']`,
  )
}

// ---- sink ------------------------------------------------------------------

async function flushSink(): Promise<void> {
  await fetch(SINK + '/api/messages', { method: 'DELETE' }).catch(() => {})
}

async function sinkWait(subject: string, ms: number): Promise<any> {
  const t0 = Date.now()
  for (;;) {
    const r = await fetch(SINK + '/api/messages')
    const env = (await r.json()) as { count: number; messages: Array<any> }
    const hit = (env.messages || []).filter((m) => m.subject === subject)
    if (hit.length) return hit[0]
    if (Date.now() - t0 > ms) throw new Error(`sink: no "${subject}" in ${ms}ms (count=${env.count})`)
    await sleep(250)
  }
}

// ---- login (proven fetch path from the harness family) ----------------------

async function login(cred: { email: string; password: string }): Promise<{ ck: string; csrf: string }> {
  const r0: any = await fetch(BASE + '/login', { headers: { 'user-agent': UA }, redirect: 'manual' })
  const h = await r0.text()
  const csrf = (h.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1: any = await fetch(BASE + '/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json', cookie: ck0, 'user-agent': UA },
    body: JSON.stringify({ email: cred.email, password: cred.password }),
    redirect: 'manual',
  })
  if (r1.status !== 200) {
    const t = await r1.text().catch(() => '')
    throw new Error(`login ${r1.status} ${t.slice(0, 120)}`)
  }
  const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
  return { ck, csrf }
}

// ---- the battery -------------------------------------------------------------

async function runLeg(): Promise<Leg> {
  const pins: Leg = {}
  await flushSink()

  // shared call helper
  const call = async (
    init: { path: string; method?: string; headers?: Record<string, string>; json?: unknown },
  ): Promise<{ status: number; ct: string; loc: string; body: string; etag?: string }> => {
    let body: string | undefined
    const headers = { ...(init.headers || {}) }
    if (init.json !== undefined) {
      body = JSON.stringify(init.json)
      headers['content-type'] = headers['content-type'] || 'application/json'
    }
    const r: any = await fetch(BASE + init.path, { method: init.method || 'GET', headers, body, redirect: 'manual' })
    const text = await r.text()
    return {
      status: r.status,
      ct: (r.headers.get('content-type') || '').split(';')[0],
      loc: r.headers.get('location') || '',
      body: text,
      etag: r.headers.get('etag') || undefined,
    }
  }

  // ============================ FRESH cell ==============================
  const adminCountFresh = resetLocalAdmin(true)
  expect(adminCountFresh).toBe('0')
  await sleep(400)

  // F1 — the first-boot page (byte-parity bake; the 14.6KB world)
  const freshPage: any = await call({ path: '/launchpad' })
  pins['F1 anon fresh page'] = freshPage
  // separate anon session for the POST battery (cookie + session csrf)
  const a1: any = await fetch(BASE + '/launchpad', { headers: { 'user-agent': UA }, redirect: 'manual' })
  const anonHtml = await a1.text()
  const anonCk = (a1.headers.get('set-cookie') || '').split(';')[0]
  const anonCsrf = (anonHtml.match(/ol-csrfToken" content="([^"]+)"/) || [])[1] as string
  const ANON = { cookie: anonCk, 'user-agent': UA }
  const ANONJ = { ...ANON, accept: 'application/json', 'x-csrf-token': anonCsrf }
  expect(!!anonCk && !!anonCsrf).toBe(true)

  // F2 — validation battery (fresh ⇒ passes the admin-exists gate)
  pins['F2a empty'] = await call({ path: '/launchpad/register_admin', method: 'POST', headers: ANONJ, json: {} })
  pins['F2b missing password'] = await call({ path: '/launchpad/register_admin', method: 'POST', headers: ANONJ, json: { email: 'p620-x@e2e.test' } })
  pins['F2c short password'] = await call({
    path: '/launchpad/register_admin',
    method: 'POST',
    headers: ANONJ,
    json: { email: 'p620-x@e2e.test', password: 'abc' },
  })
  pins['F2d invalid char'] = await call({
    path: '/launchpad/register_admin',
    method: 'POST',
    headers: ANONJ,
    json: { email: 'p620-x@e2e.test', password: 'pass|word9' },
  })

  // F3 — happy local admin creation
  pins['F3 happy local'] = await call({
    path: '/launchpad/register_admin',
    method: 'POST',
    headers: ANONJ,
    json: { email: LOCAL_NEW, password: 'Ol-Fixture-9x7K' },
  })
  pins['F3b mongo local doc'] = {
    status: 0,
    ct: '',
    loc: '',
    body: userShape(LOCAL_NEW),
  }
  pins['F3c re-register 403'] = await call({
    path: '/launchpad/register_admin',
    method: 'POST',
    headers: ANONJ,
    json: { email: LOCAL_NEW, password: 'Ol-Fixture-9x7K' },
  })
  pins['F3d anon now 302'] = await call({ path: '/launchpad', headers: { 'user-agent': UA } })

  // F4 — external (ldap-method) bootstrap
  resetLocalAdmin(true)
  await sleep(300)
  pins['F4a saml method gate'] = await call({
    path: '/launchpad/register_saml_admin',
    method: 'POST',
    headers: ANONJ,
    json: { email: 'p620-s@e2e.test' },
  })
  pins['F4b ldap empty'] = await call({ path: '/launchpad/register_ldap_admin', method: 'POST', headers: ANONJ, json: {} })
  pins['F4c ldap happy'] = await call({
    path: '/launchpad/register_ldap_admin',
    method: 'POST',
    headers: ANONJ,
    json: { email: LDAP_NEW },
  })
  pins['F4d mongo external doc'] = {
    status: 0,
    ct: '',
    loc: '',
    body: userShape(LDAP_NEW),
  }
  pins['F4e ldap re-register 403'] = await call({
    path: '/launchpad/register_ldap_admin',
    method: 'POST',
    headers: ANONJ,
    json: { email: 'p620-ldap2@e2e.test' },
  })

  // ========================== DEFAULT cell ==========================
  const adminCountDef = resetLocalAdmin(false)
  expect(adminCountDef).toBe('1')
  await sleep(400)

  pins['D1 anon 302 login'] = await call({ path: '/launchpad', headers: { 'user-agent': UA } })
  pins['D2 local admin-exists 403'] = await call({
    path: '/launchpad/register_admin',
    method: 'POST',
    headers: ANONJ,
    json: { email: LOCAL_NEW, password: 'Ol-Fixture-9x7K' },
  })
  pins['D3a ldap admin-exists 403'] = await call({
    path: '/launchpad/register_ldap_admin',
    method: 'POST',
    headers: ANONJ,
    json: { email: 'p620-ldap3@e2e.test' },
  })
  pins['D3b saml 403'] = await call({
    path: '/launchpad/register_saml_admin',
    method: 'POST',
    headers: ANONJ,
    json: { email: 'p620-s3@e2e.test' },
  })

  // ============================ LOGGED cells ===========================
  const MU = await login(ADMIN)
  const ADMH = { cookie: MU.ck, 'x-csrf-token': MU.csrf, 'user-agent': UA, accept: 'application/json' }
  const adminPage: any = await call({ path: '/launchpad', headers: { cookie: MU.ck, 'user-agent': UA } })
  pins['L0 admin page'] = adminPage

  pins['L1 email missing 400'] = await call({ path: '/launchpad/send_test_email', method: 'POST', headers: ADMH, json: {} })

  await flushSink()
  pins['L2 email sent 200'] = await call({ path: '/launchpad/send_test_email', method: 'POST', headers: ADMH, json: { email: MAIL_TO } })
  const sinkHit = await sinkWait('A Test Email from OlliTeX', 8000).catch((e) => ({ err: String(e.message || e) }))
  pins['L2b sink'] = {
    status: 0,
    ct: '',
    loc: '',
    body: sinkHit.err
      ? String(sinkHit.err)
      : `from=${(sinkHit.from || '').replace(/[<>()]/g, '')}|to=${(Array.isArray(sinkHit.to) ? sinkHit.to.join(',') : sinkHit.to)}|subject=${sinkHit.subject}|body1=${/This is a test Email from OlliTeX/.test(sinkHit.raw || '')}|open=${/Open OlliTeX:/.test(sinkHit.raw || '')}|team=${/The OlliTeX Team -/.test(sinkHit.raw || '')}`,
  }

  const MU2 = await login(USER)
  const USEH = { cookie: MU2.ck, 'x-csrf-token': MU2.csrf, 'user-agent': UA, accept: 'application/json' }
  pins['U0 non-admin 302 restricted'] = await call({ path: '/launchpad', headers: { cookie: MU2.ck, 'user-agent': UA } })
  pins['U1 non-admin email 302'] = await call({
    path: '/launchpad/send_test_email',
    method: 'POST',
    headers: USEH,
    json: { email: MAIL_TO },
  })
  pins['A2 anon email 302 login'] = await call({
    path: '/launchpad/send_test_email',
    method: 'POST',
    headers: { ...ANON, accept: 'application/json', 'x-csrf-token': anonCsrf },
    json: { email: MAIL_TO },
  })

  // ============================ restore ===============================
  const adminCountEnd = resetLocalAdmin(false)
  expect(adminCountEnd).toBe('1')
  fixturesIntact()

  return pins
}

// ---- the 4-leg gate ---------------------------------------------------------

test(
  'P6.20 launchpad flip parity',
  async () => {

  // leg 0 — clean slate
  await flip('strip')

  const node1 = await runLeg()
  // leg 2 — cumulative flip → Go
  await flip('apply')
  await waitGo()
  const go = await runLeg()
  // leg 3 — strip → Node determinism anchor
  await flip('strip')
  const node2 = await runLeg()

  const ds1 = diffLegs('node-vs-go', node1, go)
  const ds2 = diffLegs('node-determinism', node1, node2)
  const problems = [...ds1, ...ds2]
  if (problems.length) {
    throw new Error('PIN DIFFS (' + problems.length + '):\n  ' + problems.join('\n  '))
  }
}, 240_000)
