/**
 * P6.11 flip gate — github-sync module surface (Node → OlliTeX Go web).
 *
 * 4-leg contract-parity gate (same harness family as P6.5…P6.10):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a … P6.11) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *
 * Module state in the e2e environment (pinned, live-captured):
 * GITHUB_SYNC_ENABLED is unset and `db.githubSync*` collections hold 0
 * docs, so the github-sync module is NOT registered on the Node side —
 * every declared route returns the shared 404/403/401 chain. The Go core
 * reproduces that chain byte-for-byte (verified live before writing this
 * gate): member mutation w/o csrf token → 403 text/plain "Forbidden";
 * member w/ valid token → 404 express "Cannot <METHOD> <path>" page for
 * non-GET and the 404 OlliTeX "Page Not Found" view for GET/HEAD; anon
 * GET+accept-json → 401 "Unauthorized", anon GET bare → 302 /login,
 * anon non-GET → 403 "Forbidden". No Go package is required for this unit:
 * the flip moves the 16 declared routes' answering from Node to the Go
 * web (identical bodies), future-safe after P7 Node retirement.
 *
 * Declared routes covered (services/web/modules/github-sync):
 *   user:       GET  /user/github-sync/{status,orgs,repos,oauth2,oauth2/callback}
 *               POST /user/github-sync/unlink
 *               GET|POST /user/git-servers, DELETE /user/git-servers/:id,
 *                        POST /user/git-servers/test
 *               POST /user/git-pat/link
 *   project:    GET  /project/:id/github-sync/state
 *               POST /project/:id/github-sync/export
 *               GET  /project/:id/github-sync/merge/overview
 *               POST /project/:id/github-sync/merge
 *               DELETE /project/:id/github-sync
 *               POST /project/new/github-sync
 *
 * DB state anchors: githubSyncUserCredentials 0 before/after;
 * githubSyncProjectStates 0 before/after.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
// Cumulative: P7 ships the union — leg 2 exercises the full flipped so far.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf', 'web-p69.conf', 'web-p610.conf', 'web-p611.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const UA = 'p611-gate'
const GHOST = '0123456789abcdef01234567'

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
  return msh('print(db.githubSyncUserCredentials.countDocuments({}))')
}

function statesCount(): string {
  return msh('print(db.githubSyncProjectStates.countDocuments({}))')
}

// fixtures: the gate project and "other user" project already exist from
// P6.5…P6.10 (owned by e2e-user / p69-other respectively).
function ensureGateProject(): string {
  let pid = msh('const p=db.projects.findOne({name:"dropbox-p610-gate"});print(p?p._id.toString():"")')
  if (/^[0-9a-f]{24}$/.test(pid)) return pid
  // absent (fresh stack): create via the Node project API (unflipped)
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
  pid = (out || '').trim().split('\n').pop() || ''
  if (!/^[0-9a-f]{24}$/.test(pid)) throw new Error('gate project create failed: ' + out)
  return pid
}

function ensureOtherProject(): string {
  const pid = msh('const p=db.projects.findOne({name:"webdav-p69-other"});print(p?p._id.toString():"")')
  if (/^[0-9a-f]{24}$/.test(pid)) return pid
  throw new Error('webdav-p69-other fixture missing (run P6.9 gate first)')
}

function cleanState(): void {
  // camelCase = the module's explicit collections (0 docs in e2e)
  msh('db.githubSyncUserCredentials.deleteMany({});db.githubSyncProjectStates.deleteMany({});db.githubsyncusercredentials.deleteMany({});db.githubsyncprojectstates.deleteMany({});print("cleaned")')
}

async function runLeg(): Promise<Leg> {
  const MU = await login(USER.email, USER.password)
  const AH = { cookie: MU.ck, 'x-csrf-token': MU.tok, 'user-agent': UA, accept: 'application/json' }
  const AH_NOCSRF = { cookie: MU.ck, 'user-agent': UA, accept: 'application/json' }
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

  cleanState()
  const GPID = ensureGateProject()
  const OPID = ensureOtherProject()

  const pins: Leg = {}
  pins.creds_before = { status: 0, ct: '', loc: '', body: credsCount() }
  pins.states_before = { status: 0, ct: '', loc: '', body: statesCount() }

  // anon (requireLogin global chain — identical both stacks)
  pins.anon_status_json = await call({ path: '/user/github-sync/status', headers: ANJ })
  pins.anon_status_bare = await call({ path: '/user/github-sync/status', headers: AN })
  pins.anon_gitservers_json = await call({ path: '/user/git-servers', headers: ANJ })
  pins.anon_gitservers_bare = await call({ path: '/user/git-servers', headers: AN })
  pins.anon_oauth2_json = await call({ path: '/user/github-sync/oauth2', headers: ANJ })
  pins.anon_oauth2_bare = await call({ path: '/user/github-sync/oauth2', headers: AN })
  pins.anon_callback_bare = await call({ path: '/user/github-sync/oauth2/callback', headers: AN })
  pins.anon_orgs_json = await call({ path: '/user/github-sync/orgs', headers: ANJ })
  pins.anon_orgs_bare = await call({ path: '/user/github-sync/orgs', headers: AN })
  pins.anon_repos_json = await call({ path: '/user/github-sync/repos', headers: ANJ })
  pins.anon_unlink_post = await call({ path: '/user/github-sync/unlink', method: 'POST', headers: AN, json: {} })
  pins.anon_new_post = await call({ path: '/project/new/github-sync', method: 'POST', headers: AN, json: {} })
  pins.anon_state_json = await call({ path: `/project/${GPID}/github-sync/state`, headers: ANJ })
  pins.anon_state_bare = await call({ path: `/project/${GPID}/github-sync/state`, headers: AN })
  pins.anon_export_post = await call({ path: `/project/${GPID}/github-sync/export`, method: 'POST', headers: AN, json: {} })
  pins.anon_overview_get = await call({ path: `/project/${GPID}/github-sync/merge/overview`, headers: ANJ })
  pins.anon_merge_post = await call({ path: `/project/${GPID}/github-sync/merge`, method: 'POST', headers: AN, json: {} })
  pins.anon_unlinkrepo_del = await call({ path: `/project/${GPID}/github-sync`, method: 'DELETE', headers: AN })
  pins.anon_pat_link_post = await call({ path: '/user/git-pat/link', method: 'POST', headers: AN, json: {} })
  pins.anon_test_post = await call({ path: '/user/git-servers/test', method: 'POST', headers: AN, json: {} })

  // member w/ valid csrf token → the 404 chain (module unregistered)
  pins.m_status = await call({ path: '/user/github-sync/status', headers: AH })
  pins.m_oauth2 = await call({ path: '/user/github-sync/oauth2', headers: AH })
  pins.m_callback = await call({ path: '/user/github-sync/oauth2/callback', headers: AH })
  pins.m_orgs = await call({ path: '/user/github-sync/orgs', headers: AH })
  pins.m_repos = await call({ path: '/user/github-sync/repos', headers: AH })
  pins.m_gitservers = await call({ path: '/user/git-servers', headers: AH })
  pins.m_gitservers_post = await call({ path: '/user/git-servers', method: 'POST', headers: AH, json: { provider: 'github', url: 'https://github.com', username: 'x' } })
  pins.m_gitservers_test = await call({ path: '/user/git-servers/test', method: 'POST', headers: AH, json: { provider: 'github', url: 'https://github.com' } })
  pins.m_gitservers_del = await call({ path: '/user/git-servers/zzz', method: 'DELETE', headers: AH })
  pins.m_pat_link = await call({ path: '/user/git-pat/link', method: 'POST', headers: AH, json: { provider: 'github', url: 'https://github.com', username: 'x', pat: 'ghp_x' } })
  pins.m_unlink = await call({ path: '/user/github-sync/unlink', method: 'POST', headers: AH, json: {} })
  pins.m_new = await call({ path: '/project/new/github-sync', method: 'POST', headers: AH, json: { name: 'x', fullName: 'o/r', defaultBranchName: 'main' } })
  pins.m_state = await call({ path: `/project/${GPID}/github-sync/state`, headers: AH })
  pins.m_export = await call({ path: `/project/${GPID}/github-sync/export`, method: 'POST', headers: AH, json: { name: 'x' } })
  pins.m_overview = await call({ path: `/project/${GPID}/github-sync/merge/overview`, headers: AH })
  pins.m_merge = await call({ path: `/project/${GPID}/github-sync/merge`, method: 'POST', headers: AH, json: {} })
  pins.m_unlinkrepo = await call({ path: `/project/${GPID}/github-sync`, method: 'DELETE', headers: AH })

  // member w/o csrf token — mutation methods → 403 (csrf chain, pre-route)
  pins.mt_gitservers_post = await call({ path: '/user/git-servers', method: 'POST', headers: AH_NOCSRF, json: {} })
  pins.mt_merge_post = await call({ path: `/project/${GPID}/github-sync/merge`, method: 'POST', headers: AH_NOCSRF, json: {} })
  pins.mt_unlinkrepo_del = await call({ path: `/project/${GPID}/github-sync`, method: 'DELETE', headers: AH_NOCSRF })

  // project params (no authz fire — the routes never exist; GET → 404 page)
  pins.p_bad_state = await call({ path: '/project/zz/github-sync/state', headers: AH })
  pins.p_ghost_state = await call({ path: `/project/${GHOST}/github-sync/state`, headers: AH })
  pins.p_other_state = await call({ path: `/project/${OPID}/github-sync/state`, headers: AH })
  pins.p_other_merge = await call({ path: `/project/${OPID}/github-sync/merge`, method: 'POST', headers: AH, json: {} })

  // fall-through guards (wrong method at a flipped location → Node 404)
  pins.ft_post_status = await call({ path: '/user/github-sync/status', method: 'POST', headers: AH, json: {} })
  pins.ft_get_patlink = await call({ path: '/user/git-pat/link', headers: AH })
  pins.ft_put_new = await call({ path: '/project/new/github-sync', method: 'PUT', headers: AH, json: {} })

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
  expect(Object.keys(leg1!).length).toBe(51)
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
  // DB anchors (module unregistered → never touched)
  expect(L.creds_before.body).toBe('0')
  expect(L.states_before.body).toBe('0')
  expect(L.creds_after.body).toBe('0')
  expect(L.states_after.body).toBe('0')
  // anon (requireLogin global chain)
  expect(L.anon_status_json.status).toBe(401)
  expect(L.anon_status_json.ct).toBe('text/plain')
  expect(L.anon_status_json.body).toBe('Unauthorized')
  expect(L.anon_status_bare.status).toBe(302)
  expect(L.anon_status_bare.loc).toBe('/login')
  expect(L.anon_gitservers_json.status).toBe(401)
  expect(L.anon_gitservers_bare.status).toBe(302)
  expect(L.anon_gitservers_bare.loc).toBe('/login')
  expect(L.anon_oauth2_json.status).toBe(401)
  expect(L.anon_oauth2_bare.status).toBe(302)
  expect(L.anon_callback_bare.status).toBe(302)
  expect(L.anon_callback_bare.loc).toBe('/login')
  expect(L.anon_orgs_json.status).toBe(401)
  expect(L.anon_orgs_bare.status).toBe(302)
  expect(L.anon_repos_json.status).toBe(401)
  expect(L.anon_unlink_post.status).toBe(403)
  expect(L.anon_unlink_post.body).toBe('Forbidden')
  expect(L.anon_new_post.status).toBe(403)
  expect(L.anon_new_post.body).toBe('Forbidden')
  expect(L.anon_state_json.status).toBe(401)
  expect(L.anon_state_bare.status).toBe(302)
  expect(L.anon_state_bare.loc).toBe('/login')
  expect(L.anon_export_post.status).toBe(403)
  expect(L.anon_overview_get.status).toBe(401)
  expect(L.anon_merge_post.status).toBe(403)
  expect(L.anon_unlinkrepo_del.status).toBe(403)
  expect(L.anon_unlinkrepo_del.body).toBe('Forbidden')
  expect(L.anon_pat_link_post.status).toBe(403)
  expect(L.anon_test_post.status).toBe(403)
  // member w/ token → 404 OlliTeX page (GET)
  for (const k of ['m_status', 'm_oauth2', 'm_callback', 'm_orgs', 'm_repos', 'm_gitservers', 'm_state', 'm_overview']) {
    expect(L[k].status, `${k} status`).toBe(404)
    expect(L[k].ct, `${k} ct`).toBe('text/html')
    expect(L[k].body, `${k} body`).toContain('Page Not Found')
  }
  // member w/ token → express 404 pages (non-GET)
  expect(L.m_gitservers_post.status).toBe(404)
  expect(L.m_gitservers_post.ct).toBe('text/html')
  expect(L.m_gitservers_post.body).toBe(
    '<!DOCTYPE html>\n<html lang="en">\n<head>\n<meta charset="utf-8">\n<title>Error</title>\n</head>\n<body>\n<pre>Cannot POST /user/git-servers</pre>\n</body>\n</html>\n',
  )
  expect(L.m_gitservers_test.body).toContain('Cannot POST /user/git-servers/test')
  expect(L.m_gitservers_del.status).toBe(404)
  expect(L.m_gitservers_del.body).toContain('Cannot DELETE /user/git-servers/zzz')
  expect(L.m_pat_link.status).toBe(404)
  expect(L.m_pat_link.body).toContain('Cannot POST /user/git-pat/link')
  expect(L.m_unlink.status).toBe(404)
  expect(L.m_unlink.body).toContain('Cannot POST /user/github-sync/unlink')
  expect(L.m_new.status).toBe(404)
  expect(L.m_new.body).toContain('Cannot POST /project/new/github-sync')
  expect(L.m_export.status).toBe(404)
  expect(L.m_export.body).toContain('Cannot POST /project/')
  expect(L.m_export.body).toContain('/github-sync/export</pre>')
  expect(L.m_merge.status).toBe(404)
  expect(L.m_merge.body).toContain('github-sync/merge</pre>')
  expect(L.m_unlinkrepo.status).toBe(404)
  expect(L.m_unlinkrepo.body).toContain('Cannot DELETE /project/')
  expect(L.m_unlinkrepo.body).toContain('github-sync</pre>')
  // member w/o token → csrf 403 chain (mutation methods)
  expect(L.mt_gitservers_post.status).toBe(403)
  expect(L.mt_gitservers_post.ct).toBe('text/plain')
  expect(L.mt_gitservers_post.body).toBe('Forbidden')
  expect(L.mt_merge_post.status).toBe(403)
  expect(L.mt_merge_post.body).toBe('Forbidden')
  expect(L.mt_unlinkrepo_del.status).toBe(403)
  expect(L.mt_unlinkrepo_del.body).toBe('Forbidden')
  // project params (routes absent on Node → 404, not 403-restricted/404-json)
  expect(L.p_bad_state.status).toBe(404)
  expect(L.p_bad_state.body).toContain('Page Not Found')
  expect(L.p_ghost_state.status).toBe(404)
  expect(L.p_ghost_state.body).toContain('Page Not Found')
  expect(L.p_other_state.status).toBe(404)
  expect(L.p_other_state.body).toContain('Page Not Found')
  expect(L.p_other_merge.status).toBe(404)
  expect(L.p_other_merge.body).toContain('Cannot POST /project/')
  // fall-through guards (nginx method guard → Node answers)
  expect(L.ft_post_status.status).toBe(404)
  expect(L.ft_post_status.body).toContain('Cannot POST /user/github-sync/status')
  expect(L.ft_get_patlink.status).toBe(404)
  expect(L.ft_get_patlink.body).toContain('Page Not Found')
  expect(L.ft_put_new.status).toBe(404)
  expect(L.ft_put_new.body).toContain('Cannot PUT /project/new/github-sync')
})

test.afterAll(async () => {
  try {
    for (const conf of FLIPCONFS) if (flipCount(conf) > 0) await flip('strip')
  } catch {
    /* best effort */
  }
  cleanState()
})
