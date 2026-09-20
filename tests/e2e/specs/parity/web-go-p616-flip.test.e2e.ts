/**
 * P6.16 flip gate — typst module surface (Node → OlliTeX Go web).
 *
 * 5-leg contract-parity gate (same harness family as P6.5…P6.15):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a … P6.16) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *   leg 4: pin sanity (oracle anchors)
 *
 * Pinned Node behaviors (oracle, live-captured 2026-09-18; WEB_GO_PLAN.md):
 *  - global chain: anon POST /project/new/typst → 403 "Forbidden" (anon
 *    non-GET); anon GET +accept-json → 401 "Unauthorized"; anon GET bare →
 *    302 /login. csrf missing (logged in) → 403 "Forbidden".
 *  - POST schema is z.object (NON-strict — unknown keys are STRIPPED,
 *    unlike the TeX /project/new strictObject battery):
 *      body.projectName string .trim().max(100).optional() (empty→undefined)
 *      body.template    string .max(50).optional()
 *    400 = {"error":"Validation error: <issue>[; <issue>]","statusCode":400}
 *      type: Invalid input: expected string, received <t> at "body.<f>"
 *      max:  Too big: expected string to have <=N characters at "body.<f>"
 *      array body: Invalid input: expected object, received array at "body"
 *      invalid JSON / top-level primitive → 400 {}
 *  - name check (400 text/plain, AFTER schema): blank (absent/whitespace)
 *    → "Project name cannot be blank"/; "/" → "Project name cannot contain
 *    / characters".
 *  - 200 = {project_id, owner_ref, owner:{first_name,last_name,email,_id}}
 *    (project_id is new per leg → normalized in the comparison).
 *  - template: basic (1 doc), article (2 docs), example (2 docs + frog.jpg),
 *    unrecognized/absent → basic. Content parity verified in Mongo during
 *    implementation (doc line sets byte-identical Node vs Go created).
 *
 * Declared routes covered (services/web/modules/typst, 1):
 *   POST /project/new/typst
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync, readFileSync, existsSync } from 'node:fs'

const LEG1_PATH = '/tmp/web-go-p616-leg1.json'

const overleafC = 'ol-e2e-overleaf-1'
// Cumulative through P6.16.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf', 'web-p69.conf', 'web-p610.conf', 'web-p611.conf', 'web-p612.conf', 'web-p613.conf', 'web-p614.conf', 'web-p615.conf', 'web-p616.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const UA = 'p616-gate'

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

function flushCreateTypstLimiter(): void {
  // P6.16: the create-typst-project limiter (20/60s, Node-enforced in the
  // shared redis) is LIVE in e2e — flush it between legs so the Node legs
  // (and any fall-through) never 429 inside the battery window.
  try {
    dexeStrict(
      'ol-e2e-redis-1',
      'sh -c "redis-cli --scan --pattern \\"rate-limit:create-typst-project:*\\\" | xargs -r redis-cli del >/dev/null 2>&1 || true"',
    )
  } catch {
    /* redis unavailable — best effort */
  }
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
    // creation responses mint a fresh project_id per leg — normalize
    // 24-hex ObjectIds (owner_ref/owner._id are the same user both legs).
    .replace(/\b[0-9a-f]{24}\b/g, 'OBJID')
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
 * Battery (determinism contract): anon/validation/csrf rows are
 * state-invariant; the 200-creation rows mint one project per leg (the
 * response ids are normalized in diffLegs).
 */
async function runLeg(): Promise<Leg> {
  const U = await login(USER.email, USER.password)
  const UH = { cookie: U.ck, 'x-csrf-token': U.tok, 'user-agent': UA, accept: 'application/json' }
  const UH_NOCSRF = { cookie: U.ck, 'user-agent': UA, accept: 'application/json' }
  const AN = { 'user-agent': UA, accept: 'application/json' } // anon GET json
  const ANB = { 'user-agent': UA } // anon GET bare

  async function call(init: {
    path: string
    method?: string
    headers?: Record<string, string>
    rawBody?: string
  }): Promise<{ status: number; ct: string; loc: string; body: string }> {
    let body: string | undefined
    const headers = { ...(init.headers || {}) }
    if (init.rawBody !== undefined) {
      body = init.rawBody
      headers['content-type'] = headers['content-type'] || 'application/json'
    }
    const r: any = await fetch(BASE + init.path, { method: init.method || 'GET', headers, body, redirect: 'manual' })
    const text = await r.text()
    return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location') || '', body: text }
  }

  const P = '/project/new/typst'
  const pins: Leg = {}

  // ---- anon (global chain) ----------------------------------------------------
  pins.a_post = await call({ path: P, method: 'POST', headers: { 'user-agent': UA }, rawBody: '{}' })
  pins.a_get_json = await call({ path: P, headers: AN })
  pins.a_get_bare = await call({ path: P, headers: ANB })

  // ---- 400 validation battery (no project created) -----------------------------
  pins.v_nobody = await call({ path: P, method: 'POST', headers: UH, rawBody: '' })
  pins.v_blank = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":"   "}' })
  pins.v_name_num = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":7}' })
  pins.v_tmpl_num = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":"X","template":9}' })
  pins.v_both_bad = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":3,"template":"' + 'x'.repeat(51) + '"}' })
  pins.v_tmpl_null = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":"X","template":null}' })
  pins.v_tmpl_obj = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":"X","template":{"x":1}}' })
  pins.v_arr = await call({ path: P, method: 'POST', headers: UH, rawBody: '[1,2]' })
  pins.v_bady = await call({ path: P, method: 'POST', headers: UH, rawBody: '{bad' })
  pins.v_str = await call({ path: P, method: 'POST', headers: UH, rawBody: '"hi"' })
  pins.v_null = await call({ path: P, method: 'POST', headers: UH, rawBody: 'null' })
  pins.v_slash = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":"a/b"}' })

  // ---- csrf (logged in, no token) ----------------------------------------------
  pins.c_nocsrf = await call({ path: P, method: 'POST', headers: UH_NOCSRF, rawBody: '{"projectName":"CSRF"}' })

  // ---- 200 creations (one project per leg per row) ------------------------------
  pins.u_basic = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":"P616 Basic"}' })
  pins.u_article = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":"P616 Article","template":"article"}' })
  pins.u_example = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":"P616 Example","template":"example"}' })
  pins.u_bogus = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":"P616 Bogus","template":"nope"}' })
  pins.u_extra = await call({ path: P, method: 'POST', headers: UH, rawBody: '{"projectName":"P616 Extra","foo":1}' })
  pins.u_empty = await call({ path: P, method: 'POST', headers: UH, rawBody: '{}' })

  // ---- nginx method-guard fall-through (→ Node answers on both legs) --------------
  pins.ft_get = await call({ path: P, headers: UH })
  pins.ft_delete = await call({ path: P, method: 'DELETE', headers: UH })

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
  dexeStrict(
    'ol-e2e-redis-1',
    'sh -c "redis-cli --scan --pattern \\"rate-limit:*\\" | xargs -r redis-cli del >/dev/null 2>&1 || true"',
  )
  leg1 = await runLeg()
  const n = Object.keys(leg1!).length
  expect(n).toBe(24)
  writeFileSync(LEG1_PATH, JSON.stringify(leg1))
}, 300_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  flushCreateTypstLimiter()
  const B = await runLeg()
  const ds = diffLegs('p616', leg1Load(), B)
  expect(ds, ds.join('\n')).toEqual([])
}, 300_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  flushCreateTypstLimiter()
  const C = await runLeg()
  const ds = diffLegs('p616', leg1Load(), C)
  expect(ds, ds.join('\n')).toEqual([])
}, 300_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1Load()
  // global chain
  expect(L.a_post.status).toBe(403)
  expect(L.a_post.body).toBe('Forbidden')
  expect(L.a_get_json.status).toBe(401)
  expect(L.a_get_json.body).toBe('Unauthorized')
  expect(L.a_get_bare.status).toBe(302)
  expect(L.a_get_bare.loc).toBe('/login')
  // name + schema validation (400s)
  expect(L.v_blank.status).toBe(400)
  expect(L.v_blank.body).toBe('Project name cannot be blank')
  expect(L.v_blank.ct.startsWith('text/plain')).toBeTruthy()
  expect(L.v_nobody.status).toBe(400)
  expect(L.v_nobody.body).toBe('Project name cannot be blank')
  expect(L.v_slash.status).toBe(400)
  expect(L.v_slash.body).toBe('Project name cannot contain / characters')
  expect(L.v_name_num.status).toBe(400)
  expect(L.v_name_num.body).toBe('{"error":"Validation error: Invalid input: expected string, received number at \\"body.projectName\\"","statusCode":400}')
  expect(L.v_tmpl_num.status).toBe(400)
  expect(L.v_tmpl_num.body).toContain('received number at \\"body.template\\"')
  expect(L.v_both_bad.status).toBe(400)
  expect(L.v_both_bad.body).toContain('expected string, received number at \\"body.projectName\\"')
  expect(L.v_both_bad.body).toContain('<=50 characters at \\"body.template\\"')
  expect(L.v_tmpl_null.status).toBe(400)
  expect(L.v_tmpl_null.body).toContain('received null at \\"body.template\\"')
  expect(L.v_tmpl_obj.status).toBe(400)
  expect(L.v_tmpl_obj.body).toContain('received object at \\"body.template\\"')
  expect(L.v_arr.status).toBe(400)
  expect(L.v_arr.body).toBe('{"error":"Validation error: Invalid input: expected object, received array at \\"body\\"","statusCode":400}')
  expect(L.v_bady.status).toBe(400)
  expect(L.v_bady.body).toBe('{}')
  expect(L.v_str.status).toBe(400)
  expect(L.v_str.body).toBe('{}')
  expect(L.v_null.status).toBe(400)
  expect(L.v_null.body).toBe('{}')
  // csrf
  expect(L.c_nocsrf.status).toBe(403)
  // creations (200, the TeX /project/new shape)
  expect(L.u_basic.status).toBe(200)
  {
    const j = JSON.parse(L.u_basic.body)
    expect(Object.keys(j)).toEqual(['project_id', 'owner_ref', 'owner'])
    expect(Object.keys(j.owner)).toEqual(['first_name', 'last_name', 'email', '_id'])
  }
  expect(L.u_article.status).toBe(200)
  expect(L.u_example.status).toBe(200)
  expect(L.u_bogus.status).toBe(200) // unrecognized template → basic fallback
  expect(L.u_extra.status).toBe(200) // non-strict schema: unknown key stripped
  expect(L.u_empty.status).toBe(400) // {} → name blank
  // (method-guard fall-through rows only need leg-to-leg equality — Node
  // answers them on every leg; no fixed status is pinned)
}, 60_000)
