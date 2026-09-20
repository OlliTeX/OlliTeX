/**
 * P6.14 flip gate — notifications preferences module surface
 * (Node → OlliTeX Go web).
 *
 * 4-leg contract-parity gate (same harness family as P6.5…P6.13):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a … P6.14) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *
 * Pinned Node behaviors (oracle, live-captured 2026-09-18):
 *  - global chain: anon GET+accept-json → 401 "Unauthorized"; anon GET bare
 *    → 302 /login; anon non-GET → 403 "Forbidden".
 *  - GET /notifications/preferences → 200 normalized globals
 *    (muteAll, delay (int|null), 12 preference keys).
 *  - POST /notifications/preferences: zod battery — mute required bool
 *    (missing/undefined, string, null, number, array words pinned), delay
 *    optional int 1..10080 nullable (string/int/number words + too small /
 *    too big pinned); the 400 error message's field path is DOUBLE-escaped
 *    in the raw body (`at \"body.x\"`) — a Node escape artifact, mirrored.
 *    invalid-JSON / JSON-string / JSON-null / JSON-array / no-body → 400
 *    ({} / received-array) — body parsing precedes the csrf 403 (Express
 *    chain order; core app.go now parses object/array roots fully).
 *    200 echo = {muteAllNotifications[, notificationDelayMinutes]} (delay
 *    key only present when the input carried it).
 *  - Project: GET → 200 {12 keys resolved project>>global>>default} +
 *    muteAll appended LAST; POST → 200 body `null`; non-member → 403
 *    restricted page with the LAYOUT-DEFAULT title (Node
 *    ErrorController.forbidden renders user/restricted with NO title local —
 *    views.Restricted403AppTitle), ghost → 404 page, bad hex → 500 page
 *    (new ObjectId CastError), all BOTH accepts.
 *  - /user/notification-preferences GET|POST → 301 /hub#/mysettings.email
 *    (express redirect Accept matrix).
 *  - /user/send-test-email POST → 200 {"message":"Email Sent"} (+ mail to
 *    the user's email; delivery verified in the A/B battery — no spec pins
 *    the mail body).
 *
 * Declared routes covered (services/web/modules/notifications, 7):
 *   GET    /notifications/preferences
 *   POST   /notifications/preferences
 *   GET    /notifications/preferences/project/:projectId
 *   POST   /notifications/preferences/project/:projectId
 *   GET    /user/notification-preferences
 *   POST   /user/notification-preferences
 *   POST   /user/send-test-email
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync, readFileSync, existsSync } from 'node:fs'

const LEG1_PATH = '/tmp/web-go-p614-leg1.json'

const overleafC = 'ol-e2e-overleaf-1'
// Cumulative through P6.14.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf', 'web-p69.conf', 'web-p610.conf', 'web-p611.conf', 'web-p612.conf', 'web-p613.conf', 'web-p614.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const UA = 'p614-gate'
const PROJ = '6aa4ba9c73ef0e5094f4ce33' // e2e-user owned (WebGo-Ren-N)
const GHOST = '6aa4b8d673ef0e5094f4ccee' // valid 24-hex, no project

type Leg = Record<string, { status: number; ct: string; loc: string; body: string }>

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

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

function leg1Load(): Leg {
  if (leg1) return leg1
  if (existsSync(LEG1_PATH)) {
    return JSON.parse(readFileSync(LEG1_PATH, 'utf8')) as Leg
  }
  throw new Error('leg1 baseline missing (leg 1 did not record it)')
}

/**
 * Battery order (determinism contract): state-invariant rows first (anon,
 * 301s, error pages, validation 400s, mail), then the WRITE rows (deterministic
 * constants — the final state is fixed after both stacks run the same
 * sequence), then the READ rows (both stacks observe the same settled state).
 */
async function runLeg(): Promise<Leg> {
  const U = await login(USER.email, USER.password)
  const AD = await login(ADMIN.email, ADMIN.password)
  const UH = { cookie: U.ck, 'x-csrf-token': U.tok, 'user-agent': UA, accept: 'application/json' }
  const UH_NOCSRF = { cookie: U.ck, 'user-agent': UA, accept: 'application/json' }
  const AH = { cookie: AD.ck, 'x-csrf-token': AD.tok, 'user-agent': UA, accept: 'application/json' }
  const AN = { 'user-agent': UA, accept: 'application/json' } // anon GET json
  const ANB = { 'user-agent': UA } // anon GET bare

  async function call(init: {
    path: string
    method?: string
    headers?: Record<string, string>
    json?: unknown
    rawBody?: string
  }): Promise<{ status: number; ct: string; loc: string; body: string }> {
    let body: string | undefined
    const headers = { ...(init.headers || {}) }
    if (init.json !== undefined) {
      body = JSON.stringify(init.json)
      headers['content-type'] = headers['content-type'] || 'application/json'
    }
    if (init.rawBody !== undefined) {
      body = init.rawBody
      headers['content-type'] = headers['content-type'] || 'application/json'
    }
    const r: any = await fetch(BASE + init.path, { method: init.method || 'GET', headers, body, redirect: 'manual' })
    const text = await r.text()
    return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location') || '', body: text }
  }

  const pins: Leg = {}

  // ---- anon (global chain) ---------------------------------------------------
  pins.a_global_json = await call({ path: '/notifications/preferences', headers: AN })
  pins.a_global_bare = await call({ path: '/notifications/preferences', headers: ANB })
  pins.a_global_post = await call({ path: '/notifications/preferences', method: 'POST', headers: { 'user-agent': UA }, json: {} })
  pins.a_proj_get = await call({ path: `/notifications/preferences/project/${PROJ}`, headers: AN })
  pins.a_proj_post = await call({ path: `/notifications/preferences/project/${PROJ}`, method: 'POST', headers: { 'user-agent': UA }, json: {} })
  pins.a_ntfp_bare = await call({ path: '/user/notification-preferences', headers: ANB })
  pins.a_ntfp_json = await call({ path: '/user/notification-preferences', headers: AN })
  pins.a_test_post = await call({ path: '/user/send-test-email', method: 'POST', headers: { 'user-agent': UA }, json: {} })

  // ---- 301 family (state-invariant) ------------------------------------------
  pins.n301_u_html = await call({ path: '/user/notification-preferences', headers: { cookie: U.ck, 'user-agent': UA, accept: 'text/html' } })
  pins.n301_u_json = await call({ path: '/user/notification-preferences', headers: UH })
  pins.n301_u_text = await call({ path: '/user/notification-preferences', headers: { cookie: U.ck, 'user-agent': UA, accept: 'text/plain' } })
  pins.n301_u_post_html = await call({ path: '/user/notification-preferences', method: 'POST', headers: { cookie: U.ck, 'x-csrf-token': U.tok, 'user-agent': UA, accept: 'text/html', 'content-type': 'application/json' }, rawBody: '{}' })
  pins.n301_u_post_json = await call({ path: '/user/notification-preferences', method: 'POST', headers: UH, rawBody: '{}' })
  pins.n301_a_json = await call({ path: '/user/notification-preferences', headers: AH })

  // ---- error pages (state-invariant) -------------------------------------------
  pins.p_admin_403_json = await call({ path: `/notifications/preferences/project/${PROJ}`, headers: AH })
  pins.p_admin_403_html = await call({ path: `/notifications/preferences/project/${PROJ}`, headers: { cookie: AD.ck, 'user-agent': UA, accept: 'text/html' } })
  pins.p_admin_post_403 = await call({ path: `/notifications/preferences/project/${PROJ}`, method: 'POST', headers: AH, rawBody: '{}' })
  pins.p_ghost_404_json = await call({ path: `/notifications/preferences/project/${GHOST}`, headers: UH })
  pins.p_ghost_404_html = await call({ path: `/notifications/preferences/project/${GHOST}`, headers: { cookie: U.ck, 'user-agent': UA, accept: 'text/html' } })
  pins.p_ghost_post = await call({ path: `/notifications/preferences/project/${GHOST}`, method: 'POST', headers: UH, rawBody: '{}' })
  pins.p_badhex_500 = await call({ path: '/notifications/preferences/project/zz', headers: UH })
  pins.p_badhex_post = await call({ path: '/notifications/preferences/project/zz', method: 'POST', headers: UH, rawBody: '{}' })

  // ---- validation battery (400s — no state change) ----------------------------
  pins.v_empty_obj = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{}' })
  pins.v_nobody = await call({ path: '/notifications/preferences', method: 'POST', headers: { cookie: U.ck, 'x-csrf-token': U.tok, 'user-agent': UA, accept: 'application/json', 'content-type': 'application/json' } })
  pins.v_mutestr = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":"x"}' })
  pins.v_mutenull = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":null}' })
  pins.v_mutenum = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":1}' })
  pins.v_mutearr = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":[1]}' })
  pins.v_delstr = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":false,"notificationDelayMinutes":"7"}' })
  pins.v_delfrac = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":false,"notificationDelayMinutes":2.5}' })
  pins.v_delsmall = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":false,"notificationDelayMinutes":0}' })
  pins.v_delbig = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":false,"notificationDelayMinutes":10081}' })
  pins.v_delobj = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":false,"notificationDelayMinutes":{"1":2}}' })
  pins.v_delbool = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":false,"notificationDelayMinutes":true}' })
  pins.v_bady = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{bad' })
  pins.v_strbody = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '"hello"' })
  pins.v_arrbody = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '[1,2]' })
  pins.v_nullbody = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: 'null' })

  // ---- csrf chain (state-invariant) ---------------------------------------------
  pins.c_test_nocsrf = await call({ path: '/user/send-test-email', method: 'POST', headers: { cookie: U.ck, 'user-agent': UA, accept: 'application/json', 'content-type': 'application/json' }, rawBody: '{}' })
  pins.c_post_nocsrf = await call({ path: '/notifications/preferences', method: 'POST', headers: UH_NOCSRF, rawBody: '{"muteAllNotifications":false}' })
  pins.c_bady_nocsrf = await call({ path: '/notifications/preferences', method: 'POST', headers: { cookie: U.ck, 'user-agent': UA, accept: 'application/json', 'content-type': 'application/json' }, rawBody: '{bad' })

  // ---- mail (deliveries land in the local sink; response pinned) ----------------
  pins.t_user_test = await call({ path: '/user/send-test-email', method: 'POST', headers: UH, rawBody: '{}' })
  pins.t_admin_test = await call({ path: '/user/send-test-email', method: 'POST', headers: AH, rawBody: '{}' })

  // ---- WRITE battery (deterministic constants; order is part of the contract) ----
  pins.w_mute_true = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":true}' })
  pins.w_delay5 = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":false,"notificationDelayMinutes":5}' })
  pins.w_delaynull = await call({ path: '/notifications/preferences', method: 'POST', headers: UH, rawBody: '{"muteAllNotifications":false,"notificationDelayMinutes":null}' })
  pins.w_proj_empty = await call({ path: `/notifications/preferences/project/${PROJ}`, method: 'POST', headers: UH, rawBody: '{}' })
  pins.w_proj_flag = await call({ path: `/notifications/preferences/project/${PROJ}`, method: 'POST', headers: UH, rawBody: '{"commentOnOwnProject":false,"trackChangesAcceptedOnAuthoredChange":false}' })

  // ---- READ battery (post-write settled state — both legs replay the same) --------
  pins.r_global = await call({ path: '/notifications/preferences', headers: UH })
  pins.r_proj = await call({ path: `/notifications/preferences/project/${PROJ}`, headers: UH })

  // ---- nginx method-guard fall-through (→ Node answers on both legs) --------------
  pins.ft_get_sendtest = await call({ path: '/user/send-test-email', headers: AH })
  pins.ft_del_global = await call({ path: '/notifications/preferences', method: 'DELETE', headers: AH })

  return pins
}

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
  const n = Object.keys(leg1!).length
  expect(n).toBe(52)
  writeFileSync(LEG1_PATH, JSON.stringify(leg1))
}, 300_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  const B = await runLeg()
  const ds = diffLegs('p614', leg1Load(), B)
  expect(ds, ds.join('\n')).toEqual([])
}, 300_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  const C = await runLeg()
  const ds = diffLegs('p614', leg1Load(), C)
  expect(ds, ds.join('\n')).toEqual([])
}, 300_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1Load()
  expect(L.a_global_json.status).toBe(401)
  expect(L.a_global_bare.status).toBe(302)
  expect(L.a_global_post.status).toBe(403)
  expect(L.a_ntfp_bare.status).toBe(302)
  expect(L.a_test_post.status).toBe(403)
  expect(L.n301_u_html.status).toBe(301)
  expect(L.n301_u_html.loc).toBe('/hub#/mysettings.email')
  expect(L.n301_u_post_json.status).toBe(301)
  expect(L.n301_u_json.body).toBe('')
  expect(L.p_admin_403_json.status).toBe(403)
  expect(L.p_admin_403_json.body).toContain('don\u2019t have permission to load this page')
  expect(L.p_admin_403_json.body).toContain('<title translate="no">OlliTeX, Online LaTeX Editor</title>')
  expect(L.p_ghost_404_json.status).toBe(404)
  expect(L.p_badhex_500.status).toBe(500)
  expect(L.v_empty_obj.status).toBe(400)
  expect(L.v_empty_obj.body).toContain('expected boolean, received undefined at \\\"body.muteAllNotifications\\\"')
  expect(L.v_delsmall.body).toContain('Too small: expected number to be >=1')
  expect(L.v_delbig.body).toContain('Too big: expected number to be <=10080')
  expect(L.v_delfrac.body).toContain('expected int, received number')
  expect(L.v_mutearr.body).toContain('received array')
  expect(L.v_delobj.body).toContain('received object')
  expect(L.v_bady.body).toBe('{}')
  expect(L.v_arrbody.body).toContain('expected object, received array at \\\"body\\\"')
  expect(L.w_mute_true.body).toBe('{"muteAllNotifications":true}')
  expect(L.w_delay5.body).toBe('{"muteAllNotifications":false,"notificationDelayMinutes":5}')
  expect(L.w_delaynull.body).toBe('{"muteAllNotifications":false,"notificationDelayMinutes":null}')
  expect(L.w_proj_empty.body).toBe('null')
  expect(L.w_proj_flag.body).toBe('null')
  expect(L.r_global.status).toBe(200)
  expect(L.r_global.body).toContain('"muteAllNotifications":false')
  expect(L.r_global.body).toContain('"notificationDelayMinutes":null')
  expect(L.r_proj.status).toBe(200)
  expect(L.r_proj.body).toContain('"commentOnOwnProject":false')
  expect(L.r_proj.body).toContain('"trackChangesAcceptedOnAuthoredChange":false')
  // pinned: muteAll is the LAST key of the project response
  expect(L.r_proj.body.indexOf('"muteAllNotifications":')).toBeGreaterThan(L.r_proj.body.lastIndexOf('"trackChangesRejectedOnAuthoredChange"'))
  expect(L.t_user_test.status).toBe(200)
  expect(L.t_user_test.body).toBe('{"message":"Email Sent"}')
  expect(L.t_admin_test.status).toBe(200)
  expect(L.c_test_nocsrf.status).toBe(403)
  expect(L.a_ntfp_json.status).toBe(401)
  expect(L.a_ntfp_json.body).toBe('Unauthorized')
}, 60_000)
