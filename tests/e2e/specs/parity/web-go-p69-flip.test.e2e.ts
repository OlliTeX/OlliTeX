/**
 * P6.9 flip gate — webdav module surface (Node → OlliTeX Go web).
 *
 * 4-leg contract-parity gate (same harness family as P6.5…P6.8):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a + P6.4b + P6.5 + P6.6 + P6.7 + P6.8 + P6.9) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *
 * Single phase — the e2e instance is webdav-UNLINKED (WEBDAV_ENABLED=true
 * so the module loads on both stacks, but the user has no credentials,
 * no project sync states, and the sandbox has no live WebDAV server →
 * every gate path is offline-deterministic; the pinned Go/Node code
 * paths are all before the first remote WebDAV hop):
 *
 *   anon (requireLogin chain, identical both stacks):
 *     GET  /user/webdav/status (+accept-json)      → 401 text/plain "Unauthorized"
 *     GET  /user/webdav/status (bare)               → 302 → /login
 *     POST /user/webdav/connect                     → 403 text/plain "Forbidden"
 *     POST /project/:id/webdav/pull                 → 403 text/plain "Forbidden"
 *
 *   member (e2e-user, project webdav-p69-gate owned, unlinked):
 *     GET  /user/webdav/status                      → 200 {"connected":false}
 *     GET  /project/:id/webdav/state                → 200 {"connected":false}
 *     GET  /project/:id/webdav/files                → 404 {"message":"Project not linked to WebDAV"}
 *     POST /project/:id/webdav/link                 → 400 {"message":"WebDAV credentials are not configured"}
 *     POST /project/:id/webdav/pull                 → 409 {"message":"WebDAV is not connected"}
 *     POST /project/:id/webdav/push                 → 500 {"message":"WebDAV is not connected"}
 *     POST /project/:id/webdav/conflict/resolve {}  → 400 {"message":"Missing required parameters: path and choice are required"}
 *     POST …/conflict/resolve {choice:"bogus"}      → 400 {"message":"Invalid choice. Must be 'local' or 'remote'"}
 *     POST …/conflict/resolve {path,choice}         → 404 {"message":"No active conflict for this file","errorCode":"CONFLICT_NOT_FOUND"}
 *     DELETE /project/:id/webdav/state              → 404 {"message":"Project is not linked to WebDAV","errorCode":"PROJECT_NOT_LINKED"}
 *     GET  /project/:id/webdav/project-name         → 200 {"projectName":"webdav-p69-gate"}
 *     POST /project/new/webdav {}                   → 400 {"error":"projectName is required"}
 *     POST /project/new/webdav {projectName}        → 500 {"error":"WebDAV is not connected"}
 *
 *   connect cycle (state machine, same order both stacks):
 *     POST /user/webdav/connect {full}              → 200 {"success":true}
 *     GET  /user/webdav/status                      → 200 {"connected":true,"baseUrl":"https://dav.e2e.invalid","rootPath":"/Overleaf","lastSyncAt":null,"lastSyncError":null,"lastConflict":null}
 *     POST /user/webdav/connect {}                  → 200 {"success":true}
 *     GET  /user/webdav/status                      → 200 {"connected":true,"lastSyncAt":null,"lastSyncError":null,"lastConflict":null}
 *     POST /project/:id/webdav/link (baseUrl-only)  → 400 {"message":"WebDAV credentials are incomplete"}
 *     corrupt token (mongo) → GET status            → 200 {"connected":false} (Node live degrade)
 *     POST /user/webdav/disconnect                  → 200 {"success":true}
 *     GET  /user/webdav/status                      → 200 {"connected":false}
 *
 *   project params (both stacks):
 *     bad ObjectId  /project/zz/webdav/state        → 404 {"error":"Validation error: Invalid Mongo ObjectId at \"params.project_id\"","statusCode":404}
 *     ghost project /project/<24hex>/webdav/state   → 404 HTML (general/404 page)
 *     other user's  /project/<other>/webdav/state   → 403 {"message":"restricted"}
 *
 * DB state anchors (reset at leg start, restored by the leg's own
 * disconnect): webdavUserCredentials count 0 before/after;
 * webdavSyncProjectStates count 0 throughout.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
// Cumulative: P7 ships the union — leg 2 exercises the full flipped so far.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf', 'web-p69.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const OTHER = { email: 'p69-other@e2e.test', password: 'Ol-Fixture-3m2Q' }
const UA = 'p69-gate'

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
  // Per-request / per-session randomness on BOTH stacks (CSP nonce, CSRF
  // token, session sid/dates) — full normalization suite (proven in P3c).
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
  return msh('print(db.webdavusercredentials.countDocuments({}))')
}

function statesCount(): string {
  return msh('print(db.webdavsyncprojectstates.countDocuments({}))')
}

// ---- fixtures (idempotent, Node-handled on both legs) ----------------------

function ensureGateProject(): string {
  const existing = msh('const p=db.projects.findOne({name:"webdav-p69-gate"});print(p?p._id.toString():"")')
  if (existing) return existing
  // create via the Node API (not in the flipped set → Node on both legs)
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
  const r=await fetch(BASE+'/project/new',{method:'POST',headers:{cookie:ck,'x-csrf-token':tok,accept:'application/json','content-type':'application/json','user-agent':UA},body:JSON.stringify({projectName:'webdav-p69-gate'}),redirect:'manual'})
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
  // always enforce: project exists (as e2e-user) and ownership sits with p69-other
  let pid = msh('const p=db.projects.findOne({name:"webdav-p69-other"});print(p?p._id.toString():"")')
  if (!pid) {
    // make sure the other user exists (register is Node-handled in both legs)
    execFileSync('node', ['-e', `
(async()=>{
  const BASE='${BASE}', UA='${UA}';
  const r0=await fetch(BASE+'/register',{headers:{'user-agent':UA},redirect:'manual'})
  const h=await r0.text(); const csrf=(h.match(/ol-csrfToken" content="([^"]+)"/)||[])[1]
  const ck0=(r0.headers.get('set-cookie')||'').split(';')[0]
  const r1=await fetch(BASE+'/register',{method:'POST',headers:{'content-type':'application/json','x-csrf-token':csrf,accept:'application/json',cookie:ck0},body:JSON.stringify({first_name:'P69',last_name:'Other',email:'${OTHER.email}',password:'${OTHER.password}'}),redirect:'manual'})
  const t=await r1.text()
  if(r1.status>=400 && !/exists|registered|already/i.test(t)) throw new Error('register failed: '+r1.status+' '+t.slice(0,120))
  console.log('register: '+r1.status)
})().catch(e=>{console.error('ERR '+e.message);process.exit(1)})
  `], { encoding: 'utf8', stdio: 'pipe' })
    // create a project as e2e-user
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
  const r=await fetch(BASE+'/project/new',{method:'POST',headers:{cookie:ck,'x-csrf-token':tok,accept:'application/json','content-type':'application/json','user-agent':UA},body:JSON.stringify({projectName:'webdav-p69-other'}),redirect:'manual'})
  const j=await r.json().catch(()=>({}))
  if(!j.project_id) throw new Error('create failed: '+r.status)
  console.log(j.project_id)
})().catch(e=>{console.error('ERR '+e.message);process.exit(1)})
      `],
      { encoding: 'utf8', stdio: 'pipe' },
    )
    pid = (out || '').trim().split('\n').pop() || ''
    if (!/^[0-9a-f]{24}$/.test(pid)) throw new Error('other project create failed: ' + out)
  }
  msh(`
    const pid=ObjectId('${pid}');
    const u2=db.users.findOne({email:'${OTHER.email}'})._id;
    db.projects.updateOne({_id:pid},{$set:{owner:{user_id:u2,userId:u2},owner_ref:u2,collaberator_refs:[],reviewer_refs:[],readonly_refs:[],pendingEditor_refs:[],pendingReviewer_refs:[],tokenAccessReadOnly_refs:[],tokenAccessReadAndWrite_refs:[],editAccessRequests:[],lastUpdatedBy:u2}});
    print('moved')
  `)
  return pid
}

function cleanWebdavState(): void {
  // lowercase = live Node collections; camelCase = legacy Go-shape leftovers
  // from early P6.9 iterations — wipe both for a level start.
  msh('db.webdavusercredentials.deleteMany({});db.webdavsyncprojectstates.deleteMany({});db.webdavUserCredentials.deleteMany({});db.webdavSyncProjectStates.deleteMany({});print("cleaned")')
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
  cleanWebdavState()
  const GPID = ensureGateProject()
  const OPID = ensureOtherProject()
  const GHOST = '0123456789abcdef01234567'

  const pins: Leg = {}
  pins.creds_before = { status: 0, ct: '', loc: '', body: credsCount() }
  pins.states_before = { status: 0, ct: '', loc: '', body: statesCount() }

  // anon (requireLogin chain)
  pins.anon_status_json = await call({ path: '/user/webdav/status', headers: ANJ })
  pins.anon_status = await call({ path: '/user/webdav/status', headers: AN })
  pins.anon_connect_post = await call({ path: '/user/webdav/connect', method: 'POST', headers: AN, json: { baseUrl: 'x' } })
  pins.anon_pull_post = await call({ path: `/project/${GPID}/webdav/pull`, method: 'POST', headers: AN, json: {} })

  // member unlinked battery (fixed order — state machine)
  pins.m_status = await call({ path: '/user/webdav/status', headers: AH })
  pins.m_state = await call({ path: `/project/${GPID}/webdav/state`, headers: AH })
  pins.m_files = await call({ path: `/project/${GPID}/webdav/files`, headers: AH })
  pins.m_link = await call({ path: `/project/${GPID}/webdav/link`, method: 'POST', headers: AH, json: {} })
  pins.m_pull = await call({ path: `/project/${GPID}/webdav/pull`, method: 'POST', headers: AH, json: {} })
  pins.m_push = await call({ path: `/project/${GPID}/webdav/push`, method: 'POST', headers: AH, json: {} })
  pins.m_conflict_missing = await call({ path: `/project/${GPID}/webdav/conflict/resolve`, method: 'POST', headers: AH, json: {} })
  pins.m_conflict_bogus = await call({ path: `/project/${GPID}/webdav/conflict/resolve`, method: 'POST', headers: AH, json: { path: 'main.tex', choice: 'bogus' } })
  pins.m_conflict_valid = await call({ path: `/project/${GPID}/webdav/conflict/resolve`, method: 'POST', headers: AH, json: { path: 'main.tex', choice: 'local' } })
  pins.m_unlink = await call({ path: `/project/${GPID}/webdav/state`, method: 'DELETE', headers: AH })
  pins.m_name = await call({ path: `/project/${GPID}/webdav/project-name`, headers: AH })
  pins.m_new_bad = await call({ path: '/project/new/webdav', method: 'POST', headers: AH, json: {} })
  pins.m_new_unlinked = await call({ path: '/project/new/webdav', method: 'POST', headers: AH, json: { projectName: 'whatever' } })

  // project params
  pins.p_bad_oid = await call({ path: '/project/zz/webdav/state', headers: AH })
  pins.p_bad_oid_post = await call({ path: '/project/zz/webdav/pull', method: 'POST', headers: AH, json: {} })
  pins.p_ghost = await call({ path: `/project/${GHOST}/webdav/state`, headers: AH })
  pins.p_ghost_name = await call({ path: `/project/${GHOST}/webdav/project-name`, headers: AH })
  pins.p_other_state = await call({ path: `/project/${OPID}/webdav/state`, headers: AH })
  pins.p_other_files = await call({ path: `/project/${OPID}/webdav/files`, headers: AH })
  pins.p_other_pull = await call({ path: `/project/${OPID}/webdav/pull`, method: 'POST', headers: AH, json: {} })

  // connect cycle
  pins.c_connect_full = await call({
    path: '/user/webdav/connect',
    method: 'POST',
    headers: AH,
    json: { baseUrl: 'https://dav.e2e.invalid', username: 'alice', password: 's3cret', rootPath: '/Overleaf' },
  })
  pins.c_status_full = await call({ path: '/user/webdav/status', headers: AH })
  pins.c_state_after_full = await call({ path: `/project/${GPID}/webdav/state`, headers: AH })
  pins.c_connect_empty = await call({ path: '/user/webdav/connect', method: 'POST', headers: AH, json: {} })
  pins.c_status_empty = await call({ path: '/user/webdav/status', headers: AH })
  pins.c_link_incomplete = await call({ path: `/project/${GPID}/webdav/link`, method: 'POST', headers: AH, json: {} })
  pins.c_connect_base = await call({ path: '/user/webdav/connect', method: 'POST', headers: AH, json: { baseUrl: 'https://only.e2e.invalid' } })
  pins.c_link_incomplete2 = await call({ path: `/project/${GPID}/webdav/link`, method: 'POST', headers: AH, json: {} })
  // Node stores userId as a HEX STRING (live oracle) — corrupt in the same
  // shape, or the upsert would create a rogue second doc.
  msh(`(function(){const u=String(db.users.findOne({email:"${USER.email}"})._id);db.webdavusercredentials.updateOne({userId:u},{$set:{credentials:"AAAA-corrupted-2"}},{upsert:true})})()`)
  pins.c_status_corrupt = await call({ path: '/user/webdav/status', headers: AH })
  pins.c_disconnect = await call({ path: '/user/webdav/disconnect', method: 'POST', headers: AH, json: {} })
  pins.c_status_after = await call({ path: '/user/webdav/status', headers: AH })
  pins.c_disconnect_again = await call({ path: '/user/webdav/disconnect', method: 'POST', headers: AH, json: {} })

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
  // flush shared rate-limit keys before the long battery (Node+Go share Redis)
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
  expect(L.anon_connect_post.status).toBe(403)
  expect(L.anon_connect_post.body).toBe('Forbidden')
  expect(L.anon_pull_post.status).toBe(403)
  expect(L.anon_pull_post.body).toBe('Forbidden')
  // member unlinked battery
  expect(L.m_status.body).toBe('{"connected":false}')
  expect(L.m_state.body).toBe('{"connected":false}')
  expect(L.m_files.status).toBe(404)
  expect(L.m_files.body).toBe('{"message":"Project not linked to WebDAV"}')
  expect(L.m_link.status).toBe(400)
  expect(L.m_link.body).toBe('{"message":"WebDAV credentials are not configured"}')
  expect(L.m_pull.status).toBe(409)
  expect(L.m_pull.body).toBe('{"message":"WebDAV is not connected"}')
  expect(L.m_push.status).toBe(500)
  expect(L.m_push.body).toBe('{"message":"WebDAV is not connected"}')
  expect(L.m_conflict_missing.status).toBe(400)
  expect(L.m_conflict_missing.body).toBe('{"message":"Missing required parameters: path and choice are required"}')
  expect(L.m_conflict_bogus.status).toBe(400)
  expect(L.m_conflict_bogus.body).toBe(`{"message":"Invalid choice. Must be 'local' or 'remote'"}`)
  expect(L.m_conflict_valid.status).toBe(404)
  expect(L.m_conflict_valid.body).toBe('{"message":"No active conflict for this file","errorCode":"CONFLICT_NOT_FOUND"}')
  expect(L.m_unlink.status).toBe(404)
  expect(L.m_unlink.body).toBe('{"message":"Project is not linked to WebDAV","errorCode":"PROJECT_NOT_LINKED"}')
  expect(L.m_name.body).toBe('{"projectName":"webdav-p69-gate"}')
  expect(L.m_new_bad.status).toBe(400)
  expect(L.m_new_bad.body).toBe('{"error":"projectName is required"}')
  expect(L.m_new_unlinked.status).toBe(500)
  expect(L.m_new_unlinked.body).toBe('{"error":"WebDAV is not connected"}')
  // project params
  expect(L.p_bad_oid.status).toBe(404)
  expect(L.p_bad_oid.body).toBe('{"error":"Validation error: Invalid Mongo ObjectId at \\"params.project_id\\"","statusCode":404}')
  expect(L.p_ghost.status).toBe(404)
  expect(L.p_ghost.ct).toBe('text/html')
  expect(L.p_ghost.body).toContain('Page Not Found')
  expect(L.p_other_state.status).toBe(403)
  expect(L.p_other_state.body).toBe('{"message":"restricted"}')
  // connect cycle
  expect(L.c_connect_full.body).toBe('{"success":true}')
  expect(L.c_status_full.body).toBe(
    '{"connected":true,"baseUrl":"https://dav.e2e.invalid","rootPath":"/Overleaf","lastSyncAt":null,"lastSyncError":null,"lastConflict":null}',
  )
  expect(L.c_connect_empty.body).toBe('{"success":true}')
  expect(L.c_status_empty.body).toBe('{"connected":true,"lastSyncAt":null,"lastSyncError":null,"lastConflict":null}')
  expect(L.c_link_incomplete.status).toBe(400)
  expect(L.c_link_incomplete2.status).toBe(400)
  expect(L.c_link_incomplete2.body).toBe('{"message":"WebDAV credentials are incomplete"}')
  // Live Node: corrupted (undecryptable) stored token → not linked WITH error key
  expect(L.c_status_corrupt.body).toBe('{"connected":false,"error":"stored-credentials-invalid"}')
  // disconnect restores
  expect(L.c_disconnect.body).toBe('{"success":true}')
  expect(L.c_status_after.body).toBe('{"connected":false}')
  expect(L.c_disconnect_again.body).toBe('{"success":true}')
})

test.afterAll(async () => {
  try {
    for (const conf of FLIPCONFS) if (flipCount(conf) > 0) await flip('strip')
  } catch {
    /* best effort */
  }
  cleanWebdavState()
})
