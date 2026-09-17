/**
 * P6.10 flip gate — dropbox module surface (Node → OlliTeX Go web).
 *
 * 4-leg contract-parity gate (same harness family as P6.5…P6.9):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a + P6.4b + P6.5 + P6.6 + P6.7 + P6.8 +
 *          P6.9 + P6.10) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *
 * Single phase — the e2e instance is dropbox-UNLINKED (DROPBOX_ENABLED=true
 * so the module loads on both stacks, but the user has no credentials, no
 * project sync states, no Dropbox app keys, and the sandbox has no network
 * → every gate path is offline-deterministic before the first remote hop):
 *
 *   anon (requireLogin chain, identical both stacks):
 *     GET  /user/dropbox/status (+accept-json)      → 401 text/plain "Unauthorized"
 *     GET  /user/dropbox/status (bare)               → 302 → /login
 *     GET  /user/dropbox/oauth2 (+accept-json)       → 401 text/plain
 *     GET  /user/dropbox/oauth2 (bare)               → 302 → /login
 *     GET  /user/dropbox/oauth/callback              → 302 → /login
 *     POST /user/dropbox/connect                     → 403 text/plain "Forbidden"
 *     POST /project/:id/dropbox/pull                 → 403 text/plain "Forbidden"
 *
 *   member (e2e-user, project dropbox-p610-gate owned, unlinked):
 *     GET  /user/dropbox/status                      → 200 {"connected":false}
 *     GET  /user/dropbox/oauth2                      → 503 text/html "Dropbox OAuth is not configured"
 *     GET  /user/dropbox/oauth/callback              → 400 text/html "Invalid Dropbox OAuth state"
 *     POST /user/dropbox/connect {}                  → 400 {"error":"Missing access_token"}
 *     POST /user/dropbox/connect {access_token}      → 500 {"error":"No encryption secret available for Dropbox credentials (set WEBDAV_TOKEN_CIPHER_PASSWORD or SECRET_TOKEN)"}
 *     DELETE /project/:id/dropbox/state              → 200 {"success":true}
 *     GET  /project/:id/dropbox/state                → 200 {"connected":false}
 *     POST /project/:id/dropbox/link                 → 409 {"error":"Not connected to Dropbox. Please connect your account first."}
 *     POST /project/:id/dropbox/pull                 → 409 {"error":"Project not linked to Dropbox"}
 *     POST /project/:id/dropbox/push                 → 409 {"error":"Project not linked to Dropbox"}
 *     GET  /project/:id/dropbox/files                → 409 {"error":"Project not linked to Dropbox"}
 *     POST /project/new/dropbox {}                   → 400 {"error":"projectName is required"}
 *     POST /project/new/dropbox {projectName}        → 409 {"error":"Dropbox credentials not found"}
 *     POST /user/dropbox/disconnect                  → 200 {"success":true,"unlinkedProjects":"/"}
 *
 *   project params (state GET is LOGIN-ONLY in Node — no authz/no zod):
 *     bad ObjectId  /project/zz/dropbox/state        → 200 {"connected":false}
 *     bad ObjectId  /project/zz/dropbox/link         → 404 {"error":"Validation error: Invalid Mongo ObjectId at \"params.project_id\"","statusCode":404}
 *     ghost project /project/<24hex>/dropbox/state   → 200 {"connected":false}
 *     ghost project /project/<24hex>/dropbox/link    → 404 HTML (general/404 page)
 *     other user's  /project/<other>/dropbox/state   → 200 {"connected":false}
 *     other user's  /project/<other>/dropbox/link    → 403 {"message":"restricted"}
 *     other user's  /project/<other>/dropbox/pull    → 403 {"message":"restricted"}
 *
 *   corrupted-credential cycle (state machine, same order both stacks):
 *     inject dropboxusercredentials {userId,accessToken,path:"/"} (mongo)
 *     GET  /user/dropbox/status                      → 200 {"connected":true,"path":"/","projects":[],"lastSyncAt":null,"lastSyncError":null}
 *     POST /project/:id/dropbox/link                 → 500 {"error":"Token decryption failed"}
 *     POST /project/new/dropbox {projectName}        → 500 {"error":"Decryption failed. Invalid token or encryption key."}
 *     POST /project/:id/dropbox/pull                 → 409 {"error":"Project not linked to Dropbox"}
 *     POST /user/dropbox/disconnect                  → 200 {"success":true,"unlinkedProjects":"/"}
 *     GET  /user/dropbox/status                      → 200 {"connected":false}
 *
 * DB state anchors (reset at leg start, restored by the leg's own
 * disconnect): dropboxusercredentials count 0 before/after;
 * dropboxsyncprojectstates count 0 throughout.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
// Cumulative: P7 ships the union — leg 2 exercises the full flipped so far.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf', 'web-p69.conf', 'web-p610.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const OTHER = { email: 'p69-other@e2e.test', password: 'Ol-Fixture-3m2Q' }
const UA = 'p610-gate'

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

function normBody(b: string): string {
  // Per-request / per-session randomness on BOTH stacks — the proven P3c
  // normalization suite. (No rendered views in this battery, but the login
  // flow for fixture creation touches them — keep full parity.)
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
    const an = normBody(a.body)
    const bn = normBody(b.body)
    if (an !== bn) {
      ds.push(`${label}:${k} body A~${an.slice(0, 140)} || B~${bn.slice(0, 140)}`)
    }
  }
  return ds
}

function credsCount(): string {
  return msh('print(db.dropboxusercredentials.countDocuments({}))')
}

function statesCount(): string {
  return msh('print(db.dropboxsyncprojectstates.countDocuments({}))')
}

// ---- fixtures (idempotent, Node-handled on both legs) ----------------------

function ensureGateProject(): string {
  const existing = msh('const p=db.projects.findOne({name:"dropbox-p610-gate"});print(p?p._id.toString():"")')
  if (existing) return existing
  const out = execFileSync(
    'node',
    ['-e', `
(async()=>{
  const BASE='${BASE}', UA='${UA}';
  const r0=await fetch(BASE+'/login',{headers:{'user-agent':UA},redirect:'manual'})
  const h=await r0.text(); const csrf=(h.match(/ol-csrfToken" content="([^"]+)"/)||[])[1]
  const ck0=(r0.headers.get('set-cookie')||'').split(';')[0]
  const r1=await fetch(BASE+'/login',{method:'POST',headers:{'content-type':'application/json','x-csrf-token':csrf,accept:'application/json',cookie:ck0},body:JSON.stringify({email:'${USER.email}',password:'${USER.password}'}),redirect:'manual'})
  if(r1.status!==200) throw new Error('login '+r1.status)
  const ck=(r1.headers.get('set-cookie')||'').split(';')[0]||ck0
  const tok=(await (await fetch(BASE+'/dev/csrf',{headers:{cookie:ck}})).text()).trim()
  const r=await fetch(BASE+'/project/new',{method:'POST',headers:{cookie:ck,'x-csrf-token':tok,accept:'application/json','content-type':'application/json','user-agent':UA},body:JSON.stringify({projectName:'dropbox-p610-gate'}),redirect:'manual'})
  const j=await r.json().catch(()=>({}))
  if(!j.project_id) throw new Error('create failed: '+r.status)
  console.log(j.project_id)
})().catch(e=>{console.error('ERR '+e.message);process.exit(1)})
    `],
    { encoding: 'utf8', stdio: 'pipe' },
  )
  const pid = (out || '').trim().split('\n').pop() || ''
  if (!/^[0-9a-f]{24}$/.test(pid)) throw new Error('gate project create failed: ' + out)
  return pid
}

function ensureOtherProject(): string {
  // Reuse the P6.9 "other" fixture (owned by p69-other, un-writable for
  // e2e-user) — the authz pin only needs ANY project we don't own.
  let pid = msh('const p=db.projects.findOne({name:"webdav-p69-other"});print(p?p._id.toString():"")')
  if (/^[0-9a-f]{24}$/.test(pid)) return pid
  // create a project as e2e-user, then move ownership to the other user
  const out = execFileSync(
    'node',
    ['-e', `
(async()=>{
  const BASE='${BASE}', UA='${UA}';
  const r0=await fetch(BASE+'/register',{headers:{'user-agent':UA},redirect:'manual'})
  const h=await r0.text(); const csrf=(h.match(/ol-csrfToken" content="([^"]+)"/)||[])[1]
  const ck0=(r0.headers.get('set-cookie')||'').split(';')[0]
  const r1=await fetch(BASE+'/register',{method:'POST',headers:{'content-type':'application/json','x-csrf-token':csrf,accept:'application/json',cookie:ck0},body:JSON.stringify({first_name:'P69',last_name:'Other',email:'${OTHER.email}',password:'${OTHER.password}'}),redirect:'manual'})
  const t=await r1.text()
  if(r1.status>=400 && !/exists|registered|already/i.test(t)) throw new Error('register failed: '+r1.status+' '+t.slice(0,120))
})().catch(e=>{console.error('ERR '+e.message);process.exit(1)})
    `],
    { encoding: 'utf8', stdio: 'pipe' },
  )
  const out2 = execFileSync(
    'node',
    ['-e', `
(async()=>{
  const BASE='${BASE}', UA='${UA}';
  const r0=await fetch(BASE+'/login',{headers:{'user-agent':UA},redirect:'manual'})
  const h=await r0.text(); const csrf=(h.match(/ol-csrfToken" content="([^"]+)"/)||[])[1]
  const ck0=(r0.headers.get('set-cookie')||'').split(';')[0]
  const r1=await fetch(BASE+'/login',{method:'POST',headers:{'content-type':'application/json','x-csrf-token':csrf,accept:'application/json',cookie:ck0},body:JSON.stringify({email:'${USER.email}',password:'${USER.password}'}),redirect:'manual'})
  if(r1.status!==200) throw new Error('login '+r1.status)
  const ck=(r1.headers.get('set-cookie')||'').split(';')[0]||ck0
  const tok=(await (await fetch(BASE+'/dev/csrf',{headers:{cookie:ck}})).text()).trim()
  const r=await fetch(BASE+'/project/new',{method:'POST',headers:{cookie:ck,'x-csrf-token':tok,accept:'application/json','content-type':'application/json','user-agent':UA},body:JSON.stringify({projectName:'webdav-p69-other'}),redirect:'manual'})
  const j=await r.json().catch(()=>({}))
  if(!j.project_id) throw new Error('create failed: '+r.status)
  console.log(j.project_id)
})().catch(e=>{console.error('ERR '+e.message);process.exit(1)})
      `],
    { encoding: 'utf8', stdio: 'pipe' },
  )
  pid = (out2 || '').trim().split('\n').pop() || ''
  if (!/^[0-9a-f]{24}$/.test(pid)) throw new Error('other project create failed: ' + (out2 || ''))
  msh(`
    const pid=ObjectId('${pid}');
    const u2=db.users.findOne({email:'${OTHER.email}'})._id;
    db.projects.updateOne({_id:pid},{$set:{owner:{user_id:u2,userId:u2},owner_ref:u2,collab_refs:[],collaborator_refs:[],reviewer_refs:[],readonly_refs:[],pendingEditor_refs:[],pendingReviewer_refs:[],tokenAccessReadOnly_refs:[],tokenAccessReadAndWrite_refs:[],editAccessRequests:[],lastUpdatedBy:u2}});
    print('moved')
  `)
  return pid
}

function cleanDropboxState(): void {
  // lowercase = live Node collections; camelCase = legacy leftovers —
  // wipe both for a level start.
  msh('db.dropboxusercredentials.deleteMany({});db.dropboxsyncprojectstates.deleteMany({});db.dropboxUserCredentials.deleteMany({});db.dropboxSyncProjectStates.deleteMany({});print("cleaned")')
}

// inject a stored-but-garbage credential (Node writes userId as a HEX STRING)
function injectGarbageCred(): void {
  msh(`(function(){const u=String(db.users.findOne({email:"${USER.email}"})._id);db.dropboxusercredentials.updateOne({userId:u},{$set:{accessToken:"${"A".repeat(40)}",path:"/"}},{upsert:true})})()`)
}

async function runLeg(): Promise<Leg> {
  const MU = await login(USER.email, USER.password)
  const AH = { cookie: MU.ck, 'x-csrf-token': MU.tok, 'user-agent': UA, accept: 'application/json' }
  const AN = { 'user-agent': UA }
  const ANJ = { 'user-agent': UA, accept: 'application/json' }

  async function call(init: { path: string; method?: string; headers?: Record<string, string>; json?: unknown }): Promise<{ status: number; ct: string; loc: string; body: string }> {
    let body: string | undefined
    const headers = { ...(init.headers || {}) }
    if (init.json !== undefined) {
      body = JSON.stringify(init.json)
      headers['content-type'] = headers['content-type'] || 'application/json'
    }
    const r: any = await fetch(BASE + init.path, { method: init.method || 'GET', headers, body, redirect: 'manual' })
    const text = await r.text()
    return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location') || '', body: text }
  }

  // fixtures + clean start (identical conditions both legs)
  cleanDropboxState()
  const GPID = ensureGateProject()
  const OPID = ensureOtherProject()
  const GHOST = '0123456789abcdef01234567'

  const pins: Leg = {}
  pins.creds_before = { status: 0, ct: '', loc: '', body: credsCount() }
  pins.states_before = { status: 0, ct: '', loc: '', body: statesCount() }

  // anon (requireLogin chain)
  pins.anon_status_json = await call({ path: '/user/dropbox/status', headers: ANJ })
  pins.anon_status = await call({ path: '/user/dropbox/status', headers: AN })
  pins.anon_oauth2_json = await call({ path: '/user/dropbox/oauth2', headers: ANJ })
  pins.anon_oauth2 = await call({ path: '/user/dropbox/oauth2', headers: AN })
  pins.anon_callback = await call({ path: '/user/dropbox/oauth/callback', headers: AN })
  pins.anon_connect_post = await call({ path: '/user/dropbox/connect', method: 'POST', headers: AN, json: { access_token: 'sl.x' } })
  pins.anon_pull_post = await call({ path: `/project/${GPID}/dropbox/pull`, method: 'POST', headers: AN, json: {} })

  // member unlinked battery (fixed order)
  pins.m_status = await call({ path: '/user/dropbox/status', headers: AH })
  pins.m_oauth2 = await call({ path: '/user/dropbox/oauth2', headers: AH })
  pins.m_callback = await call({ path: '/user/dropbox/oauth/callback', headers: AH })
  pins.m_connect_missing = await call({ path: '/user/dropbox/connect', method: 'POST', headers: AH, json: {} })
  pins.m_connect_sl = await call({ path: '/user/dropbox/connect', method: 'POST', headers: AH, json: { access_token: 'sl.oracletest123' } })
  pins.m_unlink = await call({ path: `/project/${GPID}/dropbox/state`, method: 'DELETE', headers: AH })
  pins.m_state = await call({ path: `/project/${GPID}/dropbox/state`, headers: AH })
  pins.m_link = await call({ path: `/project/${GPID}/dropbox/link`, method: 'POST', headers: AH, json: {} })
  pins.m_pull = await call({ path: `/project/${GPID}/dropbox/pull`, method: 'POST', headers: AH, json: {} })
  pins.m_push = await call({ path: `/project/${GPID}/dropbox/push`, method: 'POST', headers: AH, json: {} })
  pins.m_files = await call({ path: `/project/${GPID}/dropbox/files`, headers: AH })
  pins.m_new_bad = await call({ path: '/project/new/dropbox', method: 'POST', headers: AH, json: {} })
  pins.m_new_unlinked = await call({ path: '/project/new/dropbox', method: 'POST', headers: AH, json: { projectName: 'whatever' } })
  pins.m_disconnect = await call({ path: '/user/dropbox/disconnect', method: 'POST', headers: AH, json: {} })
  pins.m_status_after_disc = await call({ path: '/user/dropbox/status', headers: AH })
  pins.m_disconnect_again = await call({ path: '/user/dropbox/disconnect', method: 'POST', headers: AH, json: {} })

  // project params (state GET: login-only; link/pull: authz chain)
  pins.p_bad_oid_state = await call({ path: '/project/zz/dropbox/state', headers: AH })
  pins.p_bad_oid_link = await call({ path: '/project/zz/dropbox/link', method: 'POST', headers: AH, json: {} })
  pins.p_ghost_state = await call({ path: `/project/${GHOST}/dropbox/state`, headers: AH })
  pins.p_ghost_link = await call({ path: `/project/${GHOST}/dropbox/link`, method: 'POST', headers: AH, json: {} })
  pins.p_other_state = await call({ path: `/project/${OPID}/dropbox/state`, headers: AH })
  pins.p_other_link = await call({ path: `/project/${OPID}/dropbox/link`, method: 'POST', headers: AH, json: {} })
  pins.p_other_pull = await call({ path: `/project/${OPID}/dropbox/pull`, method: 'POST', headers: AH, json: {} })

  // corrupted-credential cycle (garbage token → decrypt failure pins)
  injectGarbageCred()
  pins.c_status_connected = await call({ path: '/user/dropbox/status', headers: AH })
  pins.c_link_decrypt = await call({ path: `/project/${GPID}/dropbox/link`, method: 'POST', headers: AH, json: {} })
  pins.c_new_decrypt = await call({ path: '/project/new/dropbox', method: 'POST', headers: AH, json: { projectName: 'x' } })
  pins.c_pull_unlinked = await call({ path: `/project/${GPID}/dropbox/pull`, method: 'POST', headers: AH, json: {} })
  pins.c_disconnect = await call({ path: '/user/dropbox/disconnect', method: 'POST', headers: AH, json: {} })
  pins.c_status_after = await call({ path: '/user/dropbox/status', headers: AH })

  pins.creds_after = { status: 0, ct: '', loc: '', body: credsCount() }
  pins.states_after = { status: 0, ct: '', loc: '', body: statesCount() }
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

test('leg 0: flip off before start (force-strip any leftovers)', async () => {
  try {
    await flip('strip')
  } catch {
    /* best effort */
  }
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
}, 60_000)

test('leg 1: Node baseline battery', async () => {
  dexeStrict(
    'ol-e2e-redis-1',
    'sh -c "redis-cli --scan --pattern \"rate-limit:*\" | xargs -r redis-cli del >/dev/null 2>&1 || true"',
  )
  leg1 = await runLeg()
  expect(Object.keys(leg1!).length).toBe(40)
}, 300_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  dexeStrict(
    'ol-e2e-redis-1',
    'sh -c "redis-cli --scan --pattern \"rate-limit:*\" | xargs -r redis-cli del >/dev/null 2>&1 || true"',
  )
  const leg2 = await runLeg()
  const ds = diffLegs('go', leg1!, leg2)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 4000))
}, 300_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  dexeStrict(
    'ol-e2e-redis-1',
    'sh -c "redis-cli --scan --pattern \"rate-limit:*\" | xargs -r redis-cli del >/dev/null 2>&1 || true"',
  )
  const leg3 = await runLeg()
  const ds = diffLegs('node-determinism', leg1!, leg3)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 4000))
}, 300_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1!
  // DB state anchors
  expect(L.creds_before.body).toBe('0')
  expect(L.states_before.body).toBe('0')
  expect(L.creds_after.body).toBe('0')
  expect(L.states_after.body).toBe('0')
  // anon (requireLogin chain)
  expect(L.anon_status_json.status).toBe(401)
  expect(L.anon_status_json.ct).toBe('text/plain')
  expect(L.anon_status_json.body).toBe('Unauthorized')
  expect(L.anon_status.status).toBe(302)
  expect(L.anon_status.loc).toBe('/login')
  expect(L.anon_oauth2_json.status).toBe(401)
  expect(L.anon_oauth2.status).toBe(302)
  expect(L.anon_callback.status).toBe(302)
  expect(L.anon_callback.loc).toBe('/login')
  expect(L.anon_connect_post.status).toBe(403)
  expect(L.anon_connect_post.body).toBe('Forbidden')
  expect(L.anon_pull_post.status).toBe(403)
  expect(L.anon_pull_post.body).toBe('Forbidden')
  // member unlinked battery
  expect(L.m_status.body).toBe('{"connected":false}')
  expect(L.m_oauth2.status).toBe(503)
  expect(L.m_oauth2.ct).toBe('text/html')
  expect(L.m_oauth2.body).toBe('Dropbox OAuth is not configured')
  expect(L.m_callback.status).toBe(400)
  expect(L.m_callback.ct).toBe('text/html')
  expect(L.m_callback.body).toBe('Invalid Dropbox OAuth state')
  expect(L.m_connect_missing.status).toBe(400)
  expect(L.m_connect_missing.body).toBe('{"error":"Missing access_token"}')
  expect(L.m_connect_sl.status).toBe(500)
  expect(L.m_connect_sl.body).toBe(
    '{"error":"No encryption secret available for Dropbox credentials (set WEBDAV_TOKEN_CIPHER_PASSWORD or SECRET_TOKEN)"}',
  )
  expect(L.m_unlink.body).toBe('{"success":true}')
  expect(L.m_state.body).toBe('{"connected":false}')
  expect(L.m_link.status).toBe(409)
  expect(L.m_link.body).toBe('{"error":"Not connected to Dropbox. Please connect your account first."}')
  expect(L.m_pull.status).toBe(409)
  expect(L.m_pull.body).toBe('{"error":"Project not linked to Dropbox"}')
  expect(L.m_push.status).toBe(409)
  expect(L.m_push.body).toBe('{"error":"Project not linked to Dropbox"}')
  expect(L.m_files.status).toBe(409)
  expect(L.m_files.body).toBe('{"error":"Project not linked to Dropbox"}')
  expect(L.m_new_bad.status).toBe(400)
  expect(L.m_new_bad.body).toBe('{"error":"projectName is required"}')
  expect(L.m_new_unlinked.status).toBe(409)
  expect(L.m_new_unlinked.body).toBe('{"error":"Dropbox credentials not found"}')
  expect(L.m_disconnect.body).toBe('{"success":true,"unlinkedProjects":"/"}')
  expect(L.m_status_after_disc.body).toBe('{"connected":false}')
  expect(L.m_disconnect_again.body).toBe('{"success":true,"unlinkedProjects":"/"}')
  // project params (state: login-only)
  expect(L.p_bad_oid_state.status).toBe(200)
  expect(L.p_bad_oid_state.body).toBe('{"connected":false}')
  expect(L.p_bad_oid_link.status).toBe(404)
  expect(L.p_bad_oid_link.body).toBe('{"error":"Validation error: Invalid Mongo ObjectId at \\"params.project_id\\"","statusCode":404}')
  expect(L.p_ghost_state.status).toBe(200)
  expect(L.p_ghost_state.body).toBe('{"connected":false}')
  expect(L.p_ghost_link.status).toBe(404)
  expect(L.p_ghost_link.ct).toBe('text/html')
  expect(L.p_ghost_link.body).toContain('Page Not Found')
  expect(L.p_other_state.status).toBe(200)
  expect(L.p_other_state.body).toBe('{"connected":false}')
  expect(L.p_other_link.status).toBe(403)
  expect(L.p_other_link.body).toBe('{"message":"restricted"}')
  expect(L.p_other_pull.status).toBe(403)
  expect(L.p_other_pull.body).toBe('{"message":"restricted"}')
  // corrupted-credential cycle
  expect(L.c_status_connected.body).toBe('{"connected":true,"path":"/","projects":[],"lastSyncAt":null,"lastSyncError":null}')
  expect(L.c_link_decrypt.status).toBe(500)
  expect(L.c_link_decrypt.body).toBe('{"error":"Token decryption failed"}')
  expect(L.c_new_decrypt.status).toBe(500)
  expect(L.c_new_decrypt.body).toBe('{"error":"Decryption failed. Invalid token or encryption key."}')
  expect(L.c_pull_unlinked.status).toBe(409)
  expect(L.c_pull_unlinked.body).toBe('{"error":"Project not linked to Dropbox"}')
  expect(L.c_disconnect.body).toBe('{"success":true,"unlinkedProjects":"/"}')
  expect(L.c_status_after.body).toBe('{"connected":false}')
})

test.afterAll(async () => {
  try {
    for (const conf of FLIPCONFS) if (flipCount(conf) > 0) await flip('strip')
  } catch {
    /* best effort */
  }
  cleanDropboxState()
})
