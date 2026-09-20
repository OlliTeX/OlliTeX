/**
 * P6.18 flip gate — page-shells module surface (Node → OlliTeX Go web).
 *
 * 5-leg contract-parity gate (same harness family as P6.5…P6.17):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a … P6.18) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *   leg 4: pin sanity (oracle anchors)
 *
 * Pinned Node behaviors (oracle, live-captured 2026-09-19; WEB_GO_PLAN.md):
 *  - GET /user/mysettings (requireLogin; anonymous → 302 /login or 401
 *    for JSON accept — global chain, pinned P1):
 *      301 /hub#/mysettings.account — express Accept negotiation:
 *      accept html → "<p>Moved Permanently. Redirecting to /hub#/mysettings
 *      .account</p>" text/html; accept text (plain) or star-slash-star →
 *      plain body;
 *      accept json/form → EMPTY body, NO Content-Type header;
 *      Vary: Accept; NO X-Powered-By, NO ETag on the redirect itself.
 *  - GET /admin/panel (ensureUserIsSiteAdmin, pinned P3.1 chain):
 *      site admin → 301 /hub#/overview (same negotiation);
 *      non-admin  → 302 /restricted?from=%2Fadmin%2Fpanel (PATH only —
 *      query strings stripped from `from`, original path case kept);
 *      anonymous  → 302 /login / 401 json.
 *  - non-GET methods on both paths → 403 "Forbidden" (csrf gate, xpb
 *    Express, weak etag — Node sendStatus shape), regardless of session.
 *  - OPTIONS (any auth level that passes the gate; the gate itself still
 *    fires first — anon OPTIONS on these gated paths → 302 /login):
 *    → 200 Allow: GET,HEAD + body "GET,HEAD" (text/html, weak sha1 etag,
 *    no X-Powered-By) — express Router auto-OPTIONS, pinned 2026-09-19.
 *  - Path variants (case / trailing slash / subpaths): the flip conf uses
 *    nginx exact-match locations, so they fall through to Node on EVERY
 *    leg (both sides Node → parity by construction). The v_* rows assert
 *    leg-to-leg identity only (like the P6.17 method-guard fall-throughs).
 *
 * Declared routes covered (services/web/modules/page-shells, 2):
 *   GET /user/mysettings
 *   GET /admin/panel
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync, readFileSync, existsSync } from 'node:fs'

const LEG1_PATH = '/tmp/web-go-p618-leg1.json'

const overleafC = 'ol-e2e-overleaf-1'
// Cumulative through P6.18.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf', 'web-p69.conf', 'web-p610.conf', 'web-p611.conf', 'web-p612.conf', 'web-p613.conf', 'web-p614.conf', 'web-p615.conf', 'web-p616.conf', 'web-p617.conf', 'web-p618.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const UA = 'p618-gate'

type Leg = Record<string, { status: number; ct: string; loc: string; allow: string; body: string }>

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
    .replace(/\b(20\d{2}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\b/g, 'TS')
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
    if (a.allow !== b.allow) ds.push(`${label}:${k} allow A='${a.allow}' B='${b.allow}'`)
    const an = normBody(a.body)
    const bn = normBody(b.body)
    if (an !== bn) {
      ds.push(`${label}:${k} body A~${an.slice(0, 140)} || B~${bn.slice(0, 140)}`)
    }
  }
  return ds
}

async function login(email: string, pw: string): Promise<string> {
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
        body: JSON.stringify({ username: email, email, password: pw }),
        redirect: 'manual',
      })
      const body1 = await r1.text()
      if (r1.status === 502 || r1.status === 503 || r1.status === 504) {
        lastErr = new Error('drain window ' + r1.status)
        await sleep(1500)
        continue
      }
      if (r1.status !== 200) throw new Error('login failed ' + r1.status + ' ' + body1.slice(0, 200))
      return (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
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
 * Battery (fully stateless — no projects, no writes, no limiters —
 * determinism contract: every row byte-identical across legs).
 */
async function runLeg(): Promise<Leg> {
  const U = await login(USER.email, USER.password)
  const D = await login(ADMIN.email, ADMIN.password)
  const UH = { cookie: U, 'user-agent': UA, accept: 'application/json' }
  const UB = { cookie: U, 'user-agent': UA } // bare (accept */*)
  const AN = { 'user-agent': UA, accept: 'application/json' }
  const ANB = { 'user-agent': UA }

  async function call(path: string, headers: Record<string,string> = {}, method = 'GET'): Promise<{ status: number; ct: string; loc: string; allow: string; body: string }> {
    const r: any = await fetch(BASE + path, { headers, method, redirect: 'manual' })
    const text = await r.text()
    return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location') || '', allow: r.headers.get('allow') || '', body: text }
  }

  const MS = '/user/mysettings'
  const PAN = '/admin/panel'
  const pins: Leg = {}

  // ---- anonymous (global chain) ------------------------------------------------
  pins.a_ms_bare = await call(MS, ANB)
  pins.a_ms_json = await call(MS, AN)
  pins.a_pan_bare = await call(PAN, ANB)
  pins.a_pan_json = await call(PAN, AN)
  pins.a_ms_opt = await call(MS, ANB, 'OPTIONS') // gate fires before the auto-OPTIONS
  pins.a_ms_post = await call(MS, ANB, 'POST')
  pins.a_pan_post = await call(PAN, ANB, 'POST')

  // ---- logged-in non-admin: /user/mysettings ------------------------------------
  pins.u_ms_bare = await call(MS, UB)
  pins.u_ms_html = await call(MS, { ...UB, accept: 'text/html' })
  pins.u_ms_json = await call(MS, UH)
  pins.u_ms_head = await call(MS, UB, 'HEAD')
  pins.u_ms_opt = await call(MS, UB, 'OPTIONS')
  pins.u_ms_q = await call(MS + '?utm=1', UB)
  pins.u_ms_post = await call(MS, UB, 'POST')
  pins.u_ms_put = await call(MS, UB, 'PUT')
  // ---- logged-in non-admin: /admin/panel (restricted bounce) ----------------------
  pins.u_pan_bare = await call(PAN, UB)
  pins.u_pan_json = await call(PAN, UH)
  pins.u_pan_opt = await call(PAN, UB, 'OPTIONS')
  pins.u_pan_del = await call(PAN, UB, 'DELETE')

  // ---- admin ----------------------------------------------------------------------
  pins.d_ms_bare = await call(MS, { cookie: D, 'user-agent': UA })
  pins.d_pan_bare = await call(PAN, { cookie: D, 'user-agent': UA })
  pins.d_pan_html = await call(PAN, { cookie: D, 'user-agent': UA, accept: 'text/html' })
  pins.d_pan_opt = await call(PAN, { cookie: D, 'user-agent': UA }, 'OPTIONS')

  // ---- nginx exact-match fall-through variants (Node answers on every leg) ----------
  pins.v_ms_upper = await call('/USER/MYSETTINGS', UB)
  pins.v_ms_trail = await call(MS + '/', UB)
  pins.v_ms_x = await call(MS + 'x', UB) // 404 page — csrf token normalized
  pins.v_pan_upper = await call('/ADMIN/PANEL', { cookie: D, 'user-agent': UA })

  return pins
}

test('leg 0: flip off before start (force-strip any leftovers)', async () => {
  try {
    await flip('strip')
  } catch {
    /* best effort */
  }
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
}, 120_000)

test('leg 1: Node baseline battery', async () => {
  leg1 = await runLeg()
  const n = Object.keys(leg1!).length
  expect(n).toBe(27)
  writeFileSync(LEG1_PATH, JSON.stringify(leg1))
}, 300_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  const B = await runLeg()
  const ds = diffLegs('p618', leg1Load(), B)
  expect(ds, ds.join('\n')).toEqual([])
}, 300_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  const C = await runLeg()
  const ds = diffLegs('p618', leg1Load(), C)
  expect(ds, ds.join('\n')).toEqual([])
}, 300_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1Load()
  // anonymous: global chain (login gate + csrf)
  expect(L.a_ms_bare.status).toBe(302)
  expect(L.a_ms_bare.loc).toBe('/login')
  expect(L.a_ms_json.status).toBe(401)
  expect(L.a_ms_json.body).toBe('Unauthorized')
  expect(L.a_pan_bare.status).toBe(302)
  expect(L.a_pan_bare.loc).toBe('/login')
  expect(L.a_pan_json.status).toBe(401)
  // anon OPTIONS on gated paths: the gate fires FIRST (pinned)
  expect(L.a_ms_opt.status).toBe(302)
  // non-GET → csrf 403, all auth levels
  expect(L.a_ms_post.status).toBe(403)
  expect(L.a_ms_post.body).toBe('Forbidden')
  expect(L.a_pan_post.status).toBe(403)
  expect(L.u_ms_post.status).toBe(403)
  expect(L.u_ms_put.status).toBe(403)
  expect(L.u_pan_del.status).toBe(403)
  // /user/mysettings: 301 hub with the express Accept matrix
  expect(L.u_ms_bare.status).toBe(301)
  expect(L.u_ms_bare.loc).toBe('/hub#/mysettings.account')
  expect(L.u_ms_bare.body).toBe('Moved Permanently. Redirecting to /hub#/mysettings.account')
  expect(L.u_ms_bare.ct).toBe('text/plain')
  expect(L.u_ms_html.body).toBe('<p>Moved Permanently. Redirecting to /hub#/mysettings.account</p>')
  expect(L.u_ms_json.status).toBe(301)
  expect(L.u_ms_json.loc).toBe('/hub#/mysettings.account')
  expect(L.u_ms_json.body).toBe('')
  expect(L.u_ms_json.ct).toBe('')
  expect(L.u_ms_head.status).toBe(301)
  expect(L.u_ms_head.loc).toBe('/hub#/mysettings.account')
  // query strings are irrelevant to the fixed redirect target
  expect(L.u_ms_q.status).toBe(301)
  expect(L.u_ms_q.loc).toBe('/hub#/mysettings.account')
  // OPTIONS auto-response (passing the gate): 200 Allow GET,HEAD
  expect(L.u_ms_opt.status).toBe(200)
  expect(L.u_ms_opt.allow).toBe('GET,HEAD')
  expect(L.u_ms_opt.body).toBe('GET,HEAD')
  // /admin/panel non-admin: restricted bounce (path only, %-encoded)
  expect(L.u_pan_bare.status).toBe(302)
  expect(L.u_pan_bare.loc).toBe('/restricted?from=%2Fadmin%2Fpanel')
  expect(L.u_pan_bare.body).toBe('Found. Redirecting to /restricted?from=%2Fadmin%2Fpanel')
  expect(L.u_pan_json.status).toBe(302)
  expect(L.u_pan_json.loc).toBe('/restricted?from=%2Fadmin%2Fpanel')
  expect(L.u_pan_opt.status).toBe(200)
  expect(L.u_pan_opt.allow).toBe('GET,HEAD')
  // admin: both routes redirect to the hubs
  expect(L.d_ms_bare.status).toBe(301)
  expect(L.d_ms_bare.loc).toBe('/hub#/mysettings.account')
  expect(L.d_pan_bare.status).toBe(301)
  expect(L.d_pan_bare.loc).toBe('/hub#/overview')
  expect(L.d_pan_bare.body).toBe('Moved Permanently. Redirecting to /hub#/overview')
  expect(L.d_pan_html.body).toBe('<p>Moved Permanently. Redirecting to /hub#/overview</p>')
  expect(L.d_pan_opt.status).toBe(200)
  expect(L.d_pan_opt.body).toBe('GET,HEAD')
  // variants: Node answers on every leg — anchors for the fall-through design
  expect(L.v_ms_upper.status).toBe(301)
  expect(L.v_ms_trail.status).toBe(301)
  expect(L.v_ms_x.status).toBe(404)
  expect(L.v_pan_upper.status).toBe(301)
  // the 404 variant page carries the session csrf token (normalized in
  // leg diffs — verify it PRESENT in the Node baseline so Go is never
  // asked to match an un-normalized dynamic value)
  expect(L.v_ms_x.body).toContain('ol-csrfToken')
}, 120_000)
