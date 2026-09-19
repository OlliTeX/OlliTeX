/**
 * P6.17 flip gate — tex-autoformatter module surface (Node → OlliTeX Go web).
 *
 * 5-leg contract-parity gate (same harness family as P6.5…P6.16):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a … P6.17) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *   leg 4: pin sanity (oracle anchors)
 *
 * Pinned Node behaviors (oracle, live-captured 2026-09-19; WEB_GO_PLAN.md):
 *  - global chain (P6.14 pin): anon POST → 403 "Forbidden"; anon GET
 *    +accept-json → 401 "Unauthorized"; anon GET bare → 302 /login.
 *    csrf missing (logged in) → 403 "Forbidden".
 *  - express.json full-parse: invalid JSON / top-level primitive → 400 {}.
 *  - body contract: content string required (missing/non-string → 400
 *    {"error":"content must be a string"}); UTF-16 length > 5 MiB → 400
 *    {"error":"content too large"}.
 *  - .bib (last dot-ext, case-insensitive; NO filename → tex path):
 *    bibtex-tidy@1.15.1 parity — 147-case corpus byte-verified in the Go
 *    unit test (go/services/web/features/texfmt) plus the rows below.
 *    formatter throw → 500 {"error":"Formatting failed"}.
 *  - non-bib: tex-fmt --stdin (same binary, same container).
 *  - 200 = {"formatted": <string>}; JSON.stringify (no HTML escaping).
 *
 * Declared routes covered (services/web/modules/tex-autoformatter, 1):
 *   POST /api/format-tex
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync, readFileSync, existsSync } from 'node:fs'

const LEG1_PATH = '/tmp/web-go-p617-leg1.json'

const overleafC = 'ol-e2e-overleaf-1'
// Cumulative through P6.17.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf', 'web-p69.conf', 'web-p610.conf', 'web-p611.conf', 'web-p612.conf', 'web-p613.conf', 'web-p614.conf', 'web-p615.conf', 'web-p616.conf', 'web-p617.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const UA = 'p617-gate'

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
 * Battery (fully stateless — no projects, no limiters — determinism
 * contract: every row must be byte-identical across legs).
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

  const P = '/api/format-tex'
  const pins: Leg = {}
  const post = (headers: Record<string, string>, rawBody: string) =>
    call({ path: P, method: 'POST', headers, rawBody })

  // ---- anon (global chain) ----------------------------------------------------
  pins.a_post = await post({ 'user-agent': UA }, '{}')
  pins.a_get_json = await call({ path: P, headers: AN })
  pins.a_get_bare = await call({ path: P, headers: ANB })

  // ---- 400 validation battery (stateless) --------------------------------------
  pins.v_empty = await post(UH, '{}')
  pins.v_content_num = await post(UH, '{"content":42}')
  pins.v_content_null = await post(UH, '{"content":null}')
  pins.v_arr = await post(UH, '[1,2]')
  pins.v_bady = await post(UH, '{bad')
  pins.v_str = await post(UH, '"hi"')
  pins.v_null = await post(UH, 'null')
  pins.v_huge = await post(UH, JSON.stringify({ content: 'x'.repeat(5 * 1024 * 1024 + 1), filename: 'a.bib' }))
  // > 12 MiB bodyParser limit (Node max_json_request_size) → 413 {} (pinned)
  pins.v_12m = await post(UH, JSON.stringify({ content: 'x'.repeat(12 * 1024 * 1024 + 1), filename: 'a.bib' }))

  // ---- csrf (logged in, no token) ----------------------------------------------
  pins.c_nocsrf = await post(UH_NOCSRF, JSON.stringify({ content: 'x', filename: 'x.bib' }))

  // ---- 200 bib battery (corpus representatives — byte-identical both legs) ----
  pins.u_bib_basic = await post(
    UH,
    JSON.stringify({
      content: '@article{knuth1974,\nauthor = {Knuth, Donald E.},\ntitle = {The $\\TeX$ Book},\njournal = {ACM},\nyear = {1974},\npages = {1--10},\n}\n',
      filename: 'refs.bib',
    }),
  )
  pins.u_bib_allord = await post(
    UH,
    JSON.stringify({
      content: '@misc{a, metadata = {m}, metadata2 = {zz}, shorttitle = {st}, year = {1999}, month = 2, volume = {9}, author = {au}, doi = {d}, pages = {p}, title = {t}, url = {u}, location = {l}, on = {on}, publisher = {pub}, urldate = {ud}, number = {4}, journal = {j}}\n',
      filename: 'refs.bib',
    }),
  )
  pins.u_bib_quotes = await post(
    UH,
    JSON.stringify({ content: '@article{a, title = {Say "hi"}, note = {\\& {\\%} \\$ \\#}, year = {1994}}\n', filename: 'refs.bib' }),
  )
  pins.u_bib_stray = await post(
    UH,
    JSON.stringify({ content: '% c1\nTEXT\n@article{a, title = {T}}\n@comment{c}\n% z\n@article{b, title = {U}}\n', filename: 'refs.bib' }),
  )
  pins.u_bib_prelim = await post(
    UH,
    JSON.stringify({ content: '@string{x = v}\n@preamble{\\begin{document}}\n@article(a, title = {T}, year = {1999})\n', filename: 'refs.BIB' }),
  )
  pins.u_bib_ws = await post(UH, JSON.stringify({ content: '   \n \t\n', filename: 'refs.bib' }))
  pins.u_bib_emptytok = await post(UH, JSON.stringify({ content: 'x', filename: 'refs.bib' }))

  // ---- 500 throw battery (formatter throws on both stacks) ----------------------
  pins.u_bib_throw = await post(UH, JSON.stringify({ content: '@misc{m, v = x and y, title = {T}}\n', filename: 'refs.bib' }))
  pins.u_bib_throw2 = await post(UH, JSON.stringify({ content: '@misc{m, v = , title = {T}}\n', filename: 'refs.bib' }))

  // ---- tex-fmt passthrough (non-bib / no filename) ------------------------------
  pins.u_tex_basic = await post(UH, JSON.stringify({ content: '% comment\n\\section{s}\n\\textbf{x}\n', filename: 'main.tex' }))
  pins.u_nofilename = await post(UH, JSON.stringify({ content: 'x\\section{y}' }))
  pins.u_noext = await post(UH, JSON.stringify({ content: 'plain text', filename: 'notes' }))
  pins.u_tex_empty = await post(UH, JSON.stringify({ content: '', filename: 'main.tex' }))

  // ---- nginx method-guard fall-through (→ Node answers on every leg) -------------
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
  leg1 = await runLeg()
  const n = Object.keys(leg1!).length
  expect(n).toBe(28)
  writeFileSync(LEG1_PATH, JSON.stringify(leg1))
}, 300_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  const B = await runLeg()
  const ds = diffLegs('p617', leg1Load(), B)
  expect(ds, ds.join('\n')).toEqual([])
}, 300_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  const C = await runLeg()
  const ds = diffLegs('p617', leg1Load(), C)
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
  // validation 400s
  expect(L.v_empty.status).toBe(400)
  expect(L.v_empty.body).toBe('{"error":"content must be a string"}')
  expect(L.v_content_num.status).toBe(400)
  expect(L.v_content_num.body).toBe('{"error":"content must be a string"}')
  expect(L.v_content_null.status).toBe(400)
  expect(L.v_content_null.body).toBe('{"error":"content must be a string"}')
  expect(L.v_arr.status).toBe(400)
  expect(L.v_arr.body).toBe('{"error":"content must be a string"}')
  expect(L.v_bady.status).toBe(400)
  expect(L.v_bady.body).toBe('{}')
  expect(L.v_str.status).toBe(400)
  expect(L.v_str.body).toBe('{}')
  expect(L.v_null.status).toBe(400)
  expect(L.v_null.body).toBe('{}')
  expect(L.v_huge.status).toBe(400)
  expect(L.v_huge.body).toBe('{"error":"content too large"}')
  expect(L.v_12m.status).toBe(413)
  expect(L.v_12m.body).toBe('{}')
  // csrf
  expect(L.c_nocsrf.status).toBe(403)
  expect(L.c_nocsrf.body).toBe('Forbidden')
  // bib 200 shape (formatted string, JSON.stringify — no HTML escaping)
  expect(L.u_bib_basic.status).toBe(200)
  {
    const j = JSON.parse(L.u_bib_basic.body)
    expect(Object.keys(j)).toEqual(['formatted'])
    expect(j.formatted).toContain('author')
  }
  expect(L.u_bib_quotes.status).toBe(200)
  {
    const j = JSON.parse(L.u_bib_quotes.body)
    // the & stays & (no \u0026 HTML escaping) in the raw wire bytes
    expect(L.u_bib_quotes.body).toContain('\\&')
    expect(j.formatted).toContain('Say "hi"')
  }
  expect(L.u_bib_stray.status).toBe(200)
  expect(L.u_bib_prelim.status).toBe(200)
  expect(L.u_bib_ws.status).toBe(200)
  expect(L.u_bib_ws.body).toBe('{"formatted":"\\n"}')
  expect(L.u_bib_emptytok.status).toBe(200)
  expect(L.u_bib_emptytok.body).toBe('{"formatted":"x\\n"}')
  // throws
  expect(L.u_bib_throw.status).toBe(500)
  expect(L.u_bib_throw.body).toBe('{"error":"Formatting failed"}')
  expect(L.u_bib_throw2.status).toBe(500)
  expect(L.u_bib_throw2.body).toBe('{"error":"Formatting failed"}')
  // tex-fmt passthrough
  expect(L.u_tex_basic.status).toBe(200)
  {
    const j = JSON.parse(L.u_tex_basic.body)
    expect(j.formatted).toContain('\\section{s}')
  }
  expect(L.u_nofilename.status).toBe(200)
  expect(L.u_noext.status).toBe(200)
  expect(L.u_tex_empty.status).toBe(200)
  // method-guard fall-through rows only need leg-to-leg equality (Node
  // answers them on every leg; no fixed status is pinned)
}, 120_000)
