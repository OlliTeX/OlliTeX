/**
 * P6.15 flip gate — languagetool module surface (Node → OlliTeX Go web).
 *
 * 4-leg contract-parity gate (same harness family as P6.5…P6.14):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a … P6.15) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *
 * Pinned Node behaviors (oracle, live-captured 2026-09-18; WEB_GO_PLAN.md):
 *  - global chain: anon GET /languagetool/languages +accept-json → 401
 *    "Unauthorized"; bare accept → 302 /login; anon non-GET → 403
 *    "Forbidden".
 *  - GET /languagetool/languages → 200 the LT /v2/languages array verbatim
 *    (the live e2e stack ships the LanguageTool server; both stacks proxy
 *    the same LT instance).
 *  - POST /languagetool/check: body {language='auto', text?, data?, picky?}.
 *    {} → 400 {"error":"text or data is required"}; invalid JSON ({bad) →
 *    400 {} (express.json precedes csrf); csrf missing → 403 "Forbidden".
 *    Valid text/data → 200 the LT /v2/check result verbatim (level picky by
 *    default, picky:false → default level; the 5 LaTeX false-positive rules
 *    disabled; bad language → LT error → 200 {"matches":[]}).
 *  - POST /admin/languagetool/check (site admin): {} → 200
 *    {"success":true,"message":"LanguageTool reachable","languageCount":N};
 *    unreachable url → 500 {"success":false,"error":"Connection attempt failed"};
 *    non-admin member → 302 /restricted?from=%2Fadmin%2Flanguagetool%2Fcheck
 *    (both html and json accepts); anon → 403 "Forbidden".
 *
 * Declared routes covered (services/web/modules/languagetool, 3):
 *   GET  /languagetool/languages
 *   POST /languagetool/check
 *   POST /admin/languagetool/check
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync, readFileSync, existsSync } from 'node:fs'

const LEG1_PATH = '/tmp/web-go-p615-leg1.json'

const overleafC = 'ol-e2e-overleaf-1'
// Cumulative through P6.15.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf', 'web-p69.conf', 'web-p610.conf', 'web-p611.conf', 'web-p612.conf', 'web-p613.conf', 'web-p614.conf', 'web-p615.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const UA = 'p615-gate'

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
 * Battery order (determinism contract): state-invariant rows only (anon,
 * validation 400s, csrf, admin authz, LT read-only proxies — the LT /v2
 * endpoints are stateless w.r.t. these inputs).
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
  pins.a_lang_json = await call({ path: '/languagetool/languages', headers: AN })
  pins.a_lang_bare = await call({ path: '/languagetool/languages', headers: ANB })
  pins.a_check_post = await call({ path: '/languagetool/check', method: 'POST', headers: { 'user-agent': UA }, json: {} })
  pins.a_admin_post = await call({ path: '/admin/languagetool/check', method: 'POST', headers: { 'user-agent': UA }, json: {} })
  pins.a_admin_get = await call({ path: '/admin/languagetool/check', headers: ANB })

  // ---- authz: member non-admin vs admin ---------------------------------------
  pins.u_admin_json = await call({ path: '/admin/languagetool/check', method: 'POST', headers: UH, rawBody: '{}' })
  pins.u_admin_html = await call({ path: '/admin/languagetool/check', method: 'POST', headers: { cookie: U.ck, 'x-csrf-token': U.tok, 'user-agent': UA, accept: 'text/html' }, rawBody: '{}' })

  // ---- GET languages (LT proxy — stateless read) -------------------------------
  pins.u_lang = await call({ path: '/languagetool/languages', headers: UH })
  pins.a_lang_get = await call({ path: '/languagetool/languages', headers: AH })

  // ---- check validation battery (no LT round-trip — 400s / csrf) ----------------
  pins.v_empty_obj = await call({ path: '/languagetool/check', method: 'POST', headers: UH, rawBody: '{}' })
  pins.v_nobody = await call({ path: '/languagetool/check', method: 'POST', headers: { cookie: U.ck, 'x-csrf-token': U.tok, 'user-agent': UA, accept: 'application/json', 'content-type': 'application/json' } })
  pins.v_bady = await call({ path: '/languagetool/check', method: 'POST', headers: UH, rawBody: '{bad' })
  pins.v_check_strbody = await call({ path: '/languagetool/check', method: 'POST', headers: UH, rawBody: '"hello"' })
  pins.c_post_nocsrf = await call({ path: '/languagetool/check', method: 'POST', headers: UH_NOCSRF, rawBody: '{"text":"hi"}' })
  pins.c_bady_nocsrf = await call({ path: '/languagetool/check', method: 'POST', headers: { cookie: U.ck, 'user-agent': UA, accept: 'application/json', 'content-type': 'application/json' }, rawBody: '{bad' })

  // ---- check LT proxies (stateless — same LT instance both stacks) -----------------
  pins.u_check_text = await call({ path: '/languagetool/check', method: 'POST', headers: UH, rawBody: '{"text":"This is a test. This is a test, again  again"}' })
  pins.u_check_picky_off = await call({ path: '/languagetool/check', method: 'POST', headers: UH, rawBody: '{"text":"Write this sentence using the passive voice, which we are using right here, in fact.","picky":false}' })
  pins.u_check_data_obj = await call({ path: '/languagetool/check', method: 'POST', headers: UH, rawBody: '{"data":{"text":"Hello, world."},"language":"en-US"}' })
  pins.u_check_badlang = await call({ path: '/languagetool/check', method: 'POST', headers: UH, rawBody: '{"text":"hi","language":"xx-NOPE"}' })

  // ---- admin connection check -------------------------------------------------
  pins.a_conn_ok = await call({ path: '/admin/languagetool/check', method: 'POST', headers: AH, rawBody: '{}' })
  pins.a_conn_refused = await call({ path: '/admin/languagetool/check', method: 'POST', headers: AH, rawBody: '{"url":"http://127.0.0.1:9"}' })

  // ---- nginx method-guard fall-through (→ Node answers on both legs) --------------
  pins.ft_check_get = await call({ path: '/languagetool/check', headers: UH })
  pins.ft_lang_post = await call({ path: '/languagetool/languages', method: 'POST', headers: UH, rawBody: '{}' })

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
  expect(n).toBe(23)
  writeFileSync(LEG1_PATH, JSON.stringify(leg1))
}, 300_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  const B = await runLeg()
  const ds = diffLegs('p615', leg1Load(), B)
  expect(ds, ds.join('\n')).toEqual([])
}, 300_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  const C = await runLeg()
  const ds = diffLegs('p615', leg1Load(), C)
  expect(ds, ds.join('\n')).toEqual([])
}, 300_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1Load()
  expect(L.a_lang_json.status).toBe(401)
  expect(L.a_lang_json.body).toBe('Unauthorized')
  expect(L.a_lang_bare.status).toBe(302)
  expect(L.a_lang_bare.loc).toBe('/login')
  expect(L.a_check_post.status).toBe(403)
  expect(L.a_admin_post.status).toBe(403)
  expect(L.a_admin_get.status).toBe(302)
  // member non-admin → the restricted bounce (both accepts)
  expect(L.u_admin_json.status).toBe(302)
  expect(L.u_admin_json.loc).toBe('/restricted?from=%2Fadmin%2Flanguagetool%2Fcheck')
  expect(L.u_admin_html.status).toBe(302)
  // GET languages — the LT proxy, stateless array
  expect(L.u_lang.status).toBe(200)
  expect(L.u_lang.body).toContain('"longCode":"en-US"')
  expect(L.a_lang_get.status).toBe(200)
  // validation battery
  expect(L.v_empty_obj.status).toBe(400)
  expect(L.v_empty_obj.body).toBe('{"error":"text or data is required"}')
  expect(L.v_nobody.status).toBe(400)
  expect(L.v_nobody.body).toBe('{"error":"text or data is required"}')
  expect(L.v_bady.status).toBe(400)
  expect(L.v_bady.body).toBe('{}')
  expect(L.v_check_strbody.status).toBe(400)
  expect(L.c_post_nocsrf.status).toBe(403)
  expect(L.c_bady_nocsrf.status).toBe(400)
  expect(L.c_bady_nocsrf.body).toBe('{}')
  // LT proxies (stateless)
  expect(L.u_check_text.status).toBe(200)
  expect(L.u_check_text.body).toContain('"name":"LanguageTool"')
  expect(L.u_check_picky_off.status).toBe(200)
  expect(L.u_check_data_obj.status).toBe(200)
  expect(L.u_check_data_obj.body).toContain('"software"')
  expect(L.u_check_badlang.status).toBe(200)
  expect(L.u_check_badlang.body).toBe('{"matches":[]}')
  // admin connection check
  expect(L.a_conn_ok.status).toBe(200)
  expect(L.a_conn_ok.body).toContain('"success":true')
  expect(L.a_conn_ok.body).toContain('"message":"LanguageTool reachable"')
  expect(L.a_conn_refused.status).toBe(500)
  expect(L.a_conn_refused.body).toBe('{"success":false,"error":"Connection attempt failed"}')
  // method-guard fall-through (Node answers — GET /languagetool/check is a
  // 404 route on Node; POST /languagetool/languages likewise)
  expect(L.ft_check_get.status).toBe(404)
  expect(L.ft_lang_post.status).toBe(404)
}, 60_000)
