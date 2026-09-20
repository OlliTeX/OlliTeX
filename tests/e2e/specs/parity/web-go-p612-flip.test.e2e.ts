/**
 * P6.12 flip gate — track-changes module surface (Node → OlliTeX Go web).
 *
 * 4-leg contract-parity gate (same harness family as P6.5…P6.11):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a … P6.12) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *
 * Pinned Node behaviors (oracle, live-captured 2026-09-18):
 *  - requireLogin chain: GET+accept-json → 401 text/plain "Unauthorized";
 *    GET bare → 302 /login; non-GET → 403 "Forbidden".
 *  - /track_changes: exact validation 400s (message parity incl. escaped
 *    quotes — Go uses SetEscapeHTML(false)); accepted states → 204 empty;
 *    stored true | false | {uid|__guests__:bool}.
 *  - empty reads: ranges `[]`, changes/users `[]`, threads `{}`.
 *  - comment cycle with a 24-hex thread id: send/edit/delete → 204; the
 *    threads pin gains the injected `user` object (user id, first/last
 *    name, email) appended per message; edited messages gain `edited_at`;
 *    missing user → key ABSENT (never null).
 *  - ANY downstream failure (ghost thread/message in chat, doc accept/
 *    resolve/reopen/delete in document-updater) → the rendered 500 page
 *    (Node: next(err) without status → general/500 view) — identical.
 *  - authz: bad oid → 404 JSON validation error; ghost oid → 404 OlliTeX
 *    page; other project → 403 {"message":"restricted"} (read AND write
 *    chain — accept-changes + delete-thread use the write chain).
 *  - Shared redis rate-limit keys/limits (track-changes-reads 60/min,
 *    track-changes-writes 20/min) — flushed before each leg so the 429
 *    shape is not pinned here (identical by construction, both stacks
 *    target the same limiter keys).
 *
 * Declared routes covered (services/web/modules/track-changes, 11):
 *   POST   /project/:id/track_changes
 *   POST   /project/:id/doc/:doc_id/changes/accept
 *   GET    /project/:id/ranges
 *   GET    /project/:id/changes/users
 *   GET    /project/:id/threads
 *   POST   /project/:id/thread/:thread_id/messages
 *   POST   /project/:id/thread/:thread_id/messages/:message_id/edit
 *   DELETE /project/:id/thread/:thread_id/messages/:message_id
 *   POST   /project/:id/doc/:doc_id/thread/:thread_id/resolve
 *   POST   /project/:id/doc/:doc_id/thread/:thread_id/reopen
 *   DELETE /project/:id/doc/:doc_id/thread/:thread_id
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync, readFileSync, existsSync } from 'node:fs'

const LEG1_PATH = '/tmp/web-go-p612-leg1.json'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
// Cumulative through P6.12.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf', 'web-p69.conf', 'web-p610.conf', 'web-p611.conf', 'web-p612.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const UA = 'p612-gate'
const GHOST = '0123456789abcdef01234567' // unknown 24-hex oid (authz 404 page)
const T24 = '6aac665553b2cdd8a092b3aa' // ghost thread id (valid hex, no room) → chat 404 → 500 page
const D24 = '6aac665553b2cdd8a092b3ad' // ghost doc id (valid hex, no ranges) → DU 404 → 500 page

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
    // P6.12: chat ids + timestamps (random per leg; both stacks normalized)
    .replace(/"id":"[0-9a-f]{24}"/g, '"id":"MSGID"')
    .replace(/"room_id":"[0-9a-f]{24}"/g, '"room_id":"ROOMID"')
    .replace(/"timestamp":\d+/g, '"timestamp":TS')
    .replace(/"edited_at":\d+/g, '"edited_at":TS')
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

// ---- fixtures -----------------------------------------------------------------

function ensureGateProject(): string {
  let pid = msh('const p=db.projects.findOne({name:"tc-p612-gate"});print(p?p._id.toString():"")')
  if (/^[0-9a-f]{24}$/.test(pid)) return pid
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
  const r=await fetch(BASE+'/project/new',{method:'POST',headers:{cookie:ck,'x-csrf-token':tok,accept:'application/json','content-type':'application/json','user-agent':UA},body:JSON.stringify({projectName:'tc-p612-gate'}),redirect:'manual'})
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

// Deterministic chat state for the gate project: drop thread rooms + their
// messages; reset the project track_changes so each leg starts identical.
function cleanChat(pid: string): string {
  return msh(
    'const pid=new ObjectId("'+pid+'");const rooms=db.rooms.find({project_id:pid},{_id:1}).toArray().map(r=>r._id);const msgs=db.messages.deleteMany({room_id:{$in:rooms}}).deletedCount;const rm=db.rooms.deleteMany({project_id:pid}).deletedCount;db.projects.updateOne({\'_id\':pid},{$set:{track_changes:false}});print(msgs+" "+rm)',
  )
}

async function runLeg(): Promise<Leg> {
  const MU = await login(USER.email, USER.password)
  const AH = { cookie: MU.ck, 'x-csrf-token': MU.tok, 'user-agent': UA, accept: 'application/json' }
  const AH_NOCSRF = { cookie: MU.ck, 'user-agent': UA, accept: 'application/json' }
  const AN = { 'user-agent': UA, accept: 'application/json' } // anon GET json
  const ANB = { 'user-agent': UA } // anon GET bare

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

  const GPID = ensureGateProject()
  const OPID = ensureOtherProject()
  cleanChat(GPID)

  const pins: Leg = {}
  pins.chat_before = { status: 0, ct: '', loc: '', body: cleanChat(GPID) }

  // ---- anon (requireLogin chain) --------------------------------------------
  pins.a_threads_json = await call({ path: `/project/${GPID}/threads`, headers: AN })
  pins.a_threads_bare = await call({ path: `/project/${GPID}/threads`, headers: ANB })
  pins.a_ranges_json = await call({ path: `/project/${GPID}/ranges`, headers: AN })
  pins.a_users_json = await call({ path: `/project/${GPID}/changes/users`, headers: AN })
  pins.a_trackchanges_post = await call({ path: `/project/${GPID}/track_changes`, method: 'POST', headers: { 'user-agent': UA }, json: {} })
  pins.a_accept_post = await call({ path: `/project/${GPID}/doc/${D24}/changes/accept`, method: 'POST', headers: { 'user-agent': UA }, json: {} })
  pins.a_send_post = await call({ path: `/project/${GPID}/thread/${T24}/messages`, method: 'POST', headers: { 'user-agent': UA }, json: { content: 'x' } })
  pins.a_resolve_post = await call({ path: `/project/${GPID}/doc/${D24}/thread/${T24}/resolve`, method: 'POST', headers: { 'user-agent': UA }, json: {} })
  pins.a_bad_threads_json = await call({ path: '/project/zz/threads', headers: AN })
  pins.a_ghost_threads_bare = await call({ path: `/project/${GHOST}/threads`, headers: ANB })

  // ---- member: track_changes validation -------------------------------------
  pins.m_tc_empty = await call({ path: `/project/${GPID}/track_changes`, method: 'POST', headers: AH, json: {} })
  pins.m_tc_on_x = await call({ path: `/project/${GPID}/track_changes`, method: 'POST', headers: AH, json: { on: 'x' } })
  pins.m_tc_for_x = await call({ path: `/project/${GPID}/track_changes`, method: 'POST', headers: AH, json: { on_for: 'x' } })
  pins.m_tc_for_badkey = await call({ path: `/project/${GPID}/track_changes`, method: 'POST', headers: AH, json: { on_for: { badkey: true } } })
  pins.m_tc_for_badval = await call({ path: `/project/${GPID}/track_changes`, method: 'POST', headers: AH, json: { on_for: { [USER_UID_HINT]: 'true' } } })
  pins.m_tc_guests_x = await call({ path: `/project/${GPID}/track_changes`, method: 'POST', headers: AH, json: { on_for_guests: 'x' } })
  pins.m_tc_on_true = await call({ path: `/project/${GPID}/track_changes`, method: 'POST', headers: AH, json: { on: true } })
  pins.m_tc_on_false = await call({ path: `/project/${GPID}/track_changes`, method: 'POST', headers: AH, json: { on: false } })

  // ---- member: reads (empty project) -----------------------------------------
  pins.m_read_ranges = await call({ path: `/project/${GPID}/ranges`, headers: AH })
  pins.m_read_users = await call({ path: `/project/${GPID}/changes/users`, headers: AH })
  pins.m_read_threads0 = await call({ path: `/project/${GPID}/threads`, headers: AH })

  // ---- member: comment cycle --------------------------------------------------
  pins.m_send = await call({ path: `/project/${GPID}/thread/${T24}/messages`, method: 'POST', headers: AH, json: { content: 'hello p612 gate' } })
  const tid = (pins.m_read_threads1 = await call({ path: `/project/${GPID}/threads`, headers: AH }))
  // extract the fresh message id from the threads response (Node and Go each
  // generate their own — the battery replays the SAME id on both legs).
  const midMatch = tid.body.match(/"id":"([0-9a-f]{24})"/)
  pins.m_edit = await call({ path: `/project/${GPID}/thread/${T24}/messages/${midMatch ? midMatch[1] : '6aac665553b2cdd8a092b3ac'}/edit`, method: 'POST', headers: AH, json: { content: 'hello p612 gate — edited' } })
  pins.m_read_threads2 = await call({ path: `/project/${GPID}/threads`, headers: AH })
  const midMatch2 = pins.m_read_threads2.body.match(/"id":"([0-9a-f]{24})"/)
  pins.m_delmsg = await call({ path: `/project/${GPID}/thread/${T24}/messages/${midMatch2 ? midMatch2[1] : '6aac665553b2cdd8a092b3ac'}`, method: 'DELETE', headers: AH })
  pins.m_read_threads3 = await call({ path: `/project/${GPID}/threads`, headers: AH })

  // ---- member: downstream 500-page pins (ghost thread/doc) ---------------------
  pins.m_accept = await call({ path: `/project/${GPID}/doc/${D24}/changes/accept`, method: 'POST', headers: AH, json: { change_ids: ['chg1'] } })
  pins.m_send_ghost = await call({ path: `/project/${GPID}/thread/0123456789abcdef01234568/messages`, method: 'POST', headers: AH, json: { content: 'ghost' } })
  pins.m_edit_ghost = await call({ path: `/project/${GPID}/thread/0123456789abcdef01234568/messages/0123456789abcdef01234568/edit`, method: 'POST', headers: AH, json: { content: 'ghost' } })
  pins.m_delmsg_ghost = await call({ path: `/project/${GPID}/thread/0123456789abcdef01234568/messages/0123456789abcdef01234568`, method: 'DELETE', headers: AH })
  pins.m_resolve_ghost = await call({ path: `/project/${GPID}/doc/${D24}/thread/0123456789abcdef01234568/resolve`, method: 'POST', headers: AH, json: {} })
  pins.m_reopen_ghost = await call({ path: `/project/${GPID}/doc/${D24}/thread/0123456789abcdef01234568/reopen`, method: 'POST', headers: AH, json: {} })
  pins.m_deltask_ghost = await call({ path: `/project/${GPID}/doc/${D24}/thread/0123456789abcdef01234568`, method: 'DELETE', headers: AH })

  // ---- authz: bad oid / ghost / other ------------------------------------------
  pins.p_bad_threads = await call({ path: '/project/zz/threads', headers: AH })
  pins.p_bad_trackchanges = await call({ path: '/project/zz/track_changes', method: 'POST', headers: AH, json: { on: true } })
  pins.p_bad_accept = await call({ path: '/project/zz/doc/' + D24 + '/changes/accept', method: 'POST', headers: AH, json: {} })
  pins.p_ghost_threads = await call({ path: `/project/${GHOST}/threads`, headers: AH })
  pins.p_ghost_trackchanges = await call({ path: `/project/${GHOST}/track_changes`, method: 'POST', headers: AH, json: { on: true } })
  pins.p_other_threads = await call({ path: `/project/${OPID}/threads`, headers: AH })
  pins.p_other_trackchanges = await call({ path: `/project/${OPID}/track_changes`, method: 'POST', headers: AH, json: { on: true } })
  pins.p_other_accept = await call({ path: `/project/${OPID}/doc/${D24}/changes/accept`, method: 'POST', headers: AH, json: {} })
  pins.p_other_deltask = await call({ path: `/project/${OPID}/doc/${D24}/thread/${T24}`, method: 'DELETE', headers: AH })

  // ---- member w/o csrf token (mutation → csrf 403 pre-route) ----------------------
  pins.mt_trackchanges = await call({ path: `/project/${GPID}/track_changes`, method: 'POST', headers: AH_NOCSRF, json: { on: true } })
  pins.mt_send = await call({ path: `/project/${GPID}/thread/${T24}/messages`, method: 'POST', headers: AH_NOCSRF, json: { content: 'x' } })
  pins.mt_deltask = await call({ path: `/project/${GPID}/doc/${D24}/thread/${T24}`, method: 'DELETE', headers: AH_NOCSRF })

  // ---- nginx method-guard fall-through (→ Node answers) ----------------------------
  pins.ft_get_trackchanges = await call({ path: `/project/${GPID}/track_changes`, headers: AH })
  pins.ft_post_threads = await call({ path: `/project/${GPID}/threads`, method: 'POST', headers: AH, json: {} })
  pins.ft_put_ranges = await call({ path: `/project/${GPID}/ranges`, method: 'PUT', headers: AH, json: {} })
  pins.ft_get_messages = await call({ path: `/project/${GPID}/thread/${T24}/messages`, headers: AH })

  pins.chat_after = { status: 0, ct: '', loc: '', body: cleanChat(GPID) }
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

// 24-hex user id placeholder (a VALID oid that is not the gate user — the
// "on_for values must be booleans" pin only needs oid-shaped keys to pass the
// key check).
const USER_UID_HINT = '6aac665553b2cdd8a092b3ac'

function leg1Load(): Leg {
  if (leg1) return leg1
  if (existsSync(LEG1_PATH)) {
    return JSON.parse(readFileSync(LEG1_PATH, 'utf8')) as Leg
  }
  throw new Error('leg1 baseline missing (leg 1 did not record it)')
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
    'sh -c "redis-cli --scan --pattern \\"rate-limit:*\\" | xargs -r redis-cli del >/dev/null 2>&1 || true"',
  )
  leg1 = await runLeg()
  expect(Object.keys(leg1!).length).toBe(52)
  writeFileSync(LEG1_PATH, JSON.stringify(leg1)) // disk anchor (module state can be isolated per test)
}, 300_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  dexeStrict(
    'ol-e2e-redis-1',
    'sh -c "redis-cli --scan --pattern \\"rate-limit:*\\" | xargs -r redis-cli del >/dev/null 2>&1 || true"',
  )
  const leg2 = await runLeg()
  const ds = diffLegs('go', leg1Load(), leg2)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 6000))
}, 300_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  dexeStrict(
    'ol-e2e-redis-1',
    'sh -c "redis-cli --scan --pattern \\"rate-limit:*\\" | xargs -r redis-cli del >/dev/null 2>&1 || true"',
  )
  const leg3 = await runLeg()
  const ds = diffLegs('node-determinism', leg1Load(), leg3)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 6000))
}, 300_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1Load()
  // chat state anchors: clean before each leg; after = battery residue
  // (T24 room + m_send_ghost room with its message — the SHARED chat
  // service auto-creates on ghost sends; identical on both stacks, and the
  // leg diff enforces the exact parity of this line anyway)
  expect(L.chat_before.body).toBe('0 0')
  // anon (requireLogin chain)
  expect(L.a_threads_json.status).toBe(401)
  expect(L.a_threads_json.ct).toBe('text/plain')
  expect(L.a_threads_json.body).toBe('Unauthorized')
  expect(L.a_threads_bare.status).toBe(302)
  expect(L.a_threads_bare.loc).toBe('/login')
  expect(L.a_ranges_json.status).toBe(401)
  expect(L.a_users_json.status).toBe(401)
  for (const k of ['a_trackchanges_post', 'a_accept_post', 'a_send_post', 'a_resolve_post']) {
    expect(L[k].status, `${k} status`).toBe(403)
    expect(L[k].ct, `${k} ct`).toBe('text/plain')
    expect(L[k].body, `${k} body`).toBe('Forbidden')
  }
  expect(L.a_bad_threads_json.status).toBe(401)
  expect(L.a_ghost_threads_bare.status).toBe(302)
  // track_changes validation (exact messages, escaped quotes)
  expect(L.m_tc_empty.status).toBe(400)
  expect(L.m_tc_empty.body).toBe('{"message":"No track-changes fields provided"}')
  expect(L.m_tc_on_x.body).toBe('{"message":"\\"on\\" must be a boolean"}')
  expect(L.m_tc_for_x.body).toBe('{"message":"\\"on_for\\" must be an object"}')
  expect(L.m_tc_for_badkey.body).toBe('{"message":"\\"on_for\\" keys must be user ids"}')
  expect(L.m_tc_for_badval.body).toBe('{"message":"\\"on_for\\" values must be booleans"}')
  expect(L.m_tc_guests_x.body).toBe('{"message":"\\"on_for_guests\\" must be a boolean"}')
  // accepted states → 204 empty
  for (const k of ['m_tc_on_true', 'm_tc_on_false']) {
    expect(L[k].status, `${k} status`).toBe(204)
    expect(L[k].body, `${k} body`).toBe('')
  }
  // empty reads
  expect(L.m_read_ranges.status).toBe(200)
  expect(L.m_read_ranges.body).toBe('[]')
  expect(L.m_read_users.status).toBe(200)
  expect(L.m_read_users.body).toBe('[]')
  expect(L.m_read_threads0.status).toBe(200)
  expect(L.m_read_threads0.body).toBe('{}')
  // comment cycle
  expect(L.m_send.status).toBe(204)
  expect(L.m_read_threads1.status).toBe(200)
  expect(L.m_read_threads1.body).toContain('"first_name":"E2e"')
  expect(L.m_read_threads1.body).toContain('"last_name":"User"')
  expect(L.m_read_threads1.body).toContain('"email":"e2e-user@e2e.test"')
  expect(L.m_edit.status).toBe(204)
  expect(L.m_read_threads2.body).toContain('hello p612 gate — edited')
  expect(L.m_read_threads2.body).toMatch(/"edited_at":\d+/)
  expect(L.m_delmsg.status).toBe(204)
  expect(L.m_read_threads3.status).toBe(200)
  // downstrean…les → rendered 500 page; m_send_ghost → chat auto-creates
  // the room → 204 on BOTH stacks (shared chat service)
  expect(L.m_send_ghost.status).toBe(204)
  const err500s = ['m_accept', 'm_edit_ghost', 'm_delmsg_ghost', 'm_resolve_ghost', 'm_reopen_ghost', 'm_deltask_ghost']
  for (const k of err500s) {
    expect(L[k].status, `${k} status`).toBe(500)
    expect(L[k].ct, `${k} ct`).toBe('text/html')
  }
  // authz: bad oid → 404 JSON validation
  for (const k of ['p_bad_threads', 'p_bad_trackchanges', 'p_bad_accept']) {
    expect(L[k].status, `${k} status`).toBe(404)
    expect(L[k].body, `${k} body`).toContain('Validation error: Invalid Mongo ObjectId')
    expect(L[k].body, `${k} body`).toContain('params.project_id')
  }
  // ghost → 404 OlliTeX page
  for (const k of ['p_ghost_threads', 'p_ghost_trackchanges']) {
    expect(L[k].status, `${k} status`).toBe(404)
    expect(L[k].ct, `${k} ct`).toBe('text/html')
  }
  // other project → 403 restricted (read + write chain)
  for (const k of ['p_other_threads', 'p_other_trackchanges', 'p_other_accept', 'p_other_deltask']) {
    expect(L[k].status, `${k} status`).toBe(403)
    expect(L[k].body, `${k} body`).toBe('{"message":"restricted"}')
  }
  // csrf 403 pre-route
  for (const k of ['mt_trackchanges', 'mt_send', 'mt_deltask']) {
    expect(L[k].status, `${k} status`).toBe(403)
    expect(L[k].ct, `${k} ct`).toBe('text/plain')
    expect(L[k].body, `${k} body`).toBe('Forbidden')
  }
  // method-guard fall-through → Node express 404s
  // method-guard fall-through — Node oracle (pinned live): GET fallthroughs
  // land on the rendered OlliTeX 404 page; other methods get express's
  // "Cannot <METHOD>" 404. Go matches both shapes (leg parity enforces).
  expect(L.ft_get_trackchanges.status).toBe(404)
  expect(L.ft_get_trackchanges.body).toContain('Page Not Found')
  expect(L.ft_post_threads.status).toBe(404)
  expect(L.ft_post_threads.body).toContain('Cannot POST')
  expect(L.ft_put_ranges.status).toBe(404)
  expect(L.ft_put_ranges.body).toContain('Cannot PUT')
  expect(L.ft_get_messages.status).toBe(404)
  expect(L.ft_get_messages.body).toContain('Page Not Found')
})

test.afterAll(async () => {
  try {
    const GPID = msh('const p=db.projects.findOne({name:"tc-p612-gate"});print(p?p._id.toString():"")')
    if (/^[0-9a-f]{24}$/.test(GPID)) cleanChat(GPID)
    for (const conf of FLIPCONFS) if (flipCount(conf) > 0) await flip('strip')
  } catch {
    /* best effort */
  }
})
