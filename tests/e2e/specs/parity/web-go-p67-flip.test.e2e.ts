/**
 * P6.7 flip gate — orcid-picker module surface (Node → OlliTeX Go web).
 *
 * 4-leg contract-parity gate (same harness family as P6.4a…P6.6):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a + P6.4b + P6.5 + P6.6 + P6.7) → same battery vs Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *
 * Pinned battery (all requireLogin; the e2e user is the caller on member
 * pins; no per-user state — orcid is purely upstream-ORCID-driven):
 *
 *   anon (core chain, identical both stacks):
 *     GET  /orcid-picker/search  +accept-json → 401 text/plain "Unauthorized"
 *     GET  /orcid-picker/works   (bare)       → 302 → /login
 *     POST /orcid-picker/fetch-bib            → 403 text/plain "Forbidden"
 *
 *   400s (validation happens before any network I/O in both stacks):
 *     search  no q            → {"error":"q query parameter required"}
 *     search  blank q         → {"error":"q query parameter required"}
 *     works   no orcid        → {"error":"orcid query parameter required"}
 *     works   bad orcid       → {"error":"Invalid ORCID identifier"}
 *     fetch-bib no orcid      → {"error":"orcid query parameter required"}
 *     fetch-bib bad orcid     → {"error":"Invalid ORCID identifier"}
 *     fetch-bib no putCode    → {"error":"putCode query parameter required"}
 *     fetch-bib putCode=abc   → {"error":"putCode query parameter required"}
 *     fetch-bib putCode=Infinity (Number.isFinite(Infinity)===false)
 *                              → {"error":"putCode query parameter required"}
 *
 *   deterministic upstream (both stacks hit the same pub.orcid.org edge):
 *     search  "Alan Turing" (fielded, zero registry match) → 200 {"results":[]}
 *     works   9999-9999-9999-9999 (never registered)       → 502
 *             {"error":"Upstream API responded with 404"}
 *     fetch-bib 9999-9999-9999-9999&putCode=123            → 502 (same body)
 *
 *   live result-set pins (byte-identical in the 2026-09-17 dual-stack
 *   capture; the gate compares leg-vs-leg, so ORCID registry data must only
 *   be stable across the ~1 minute the gate runs):
 *     search  "Turing" (free-text, non-empty)               → 200 {results:[…]}
 *     works   0000-0002-0185-5110 (15 works, year-desc)     → 200 {works:[…]}
 *     fetch-bib 0000-0002-0185-5110 putCode 17643012        → 200 {bibtex:"@…"}
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
// Cumulative: P7 ships the union — leg 2 exercises the full flipped so far.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const UA = 'p67-gate'

type Leg = Record<string, { status: number; ct: string; loc: string; body: string }>

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 64 * 1024 * 1024 })
}

// curl exit≠0 (connection refused while a service is draining) makes
// execFileSync throw — the captured status rides on e.stdout.
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

// ---------- diff (bodies are exact — no volatiles in the pinned set) ----------

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
    if (a.body !== b.body) {
      ds.push(`${label}:${k} body A~${a.body.slice(0, 140)} || B~${b.body.slice(0, 140)}`)
    }
  }
  return ds
}

// ---------- battery ----------

async function runLeg(): Promise<Leg> {
  const MU = await login(USER.email, USER.password)
  const AH = { cookie: MU.ck, 'x-csrf-token': MU.tok, 'user-agent': UA, accept: 'application/json' }
  const AN = { 'user-agent': UA }
  const ANJ = { 'user-agent': UA, accept: 'application/json' }

  async function call(init: { path: string; method?: string; headers?: Record<string, string> }): Promise<{ status: number; ct: string; loc: string; body: string }> {
    const r: any = await fetch(BASE + init.path, { method: init.method || 'GET', headers: { ...(init.headers || {}) }, redirect: 'manual' })
    const text = await r.text()
    return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location') || '', body: text }
  }

  const pins: Leg = {}

  // anon (requireLogin chain)
  pins.anon_search_json = await call({ path: '/orcid-picker/search?q=x', headers: ANJ })
  pins.anon_works = await call({ path: '/orcid-picker/works?orcid=9999-9999-9999-9999', headers: AN })
  pins.anon_bib_post = await call({ path: '/orcid-picker/fetch-bib?orcid=9999-9999-9999-9999&putCode=1', method: 'POST', headers: AN })

  // 400 battery (validation before network)
  pins.m_noq = await call({ path: '/orcid-picker/search', headers: AH })
  pins.m_blankq = await call({ path: '/orcid-picker/search?q=%20%20', headers: AH })
  pins.m_noorcid = await call({ path: '/orcid-picker/works', headers: AH })
  pins.m_badorcid = await call({ path: '/orcid-picker/works?orcid=12345', headers: AH })
  pins.m_bib_noorcid = await call({ path: '/orcid-picker/fetch-bib', headers: AH })
  pins.m_bib_badorcid = await call({ path: '/orcid-picker/fetch-bib?orcid=12345', headers: AH })
  pins.m_bib_noput = await call({ path: '/orcid-picker/fetch-bib?orcid=9999-9999-9999-9999', headers: AH })
  pins.m_bib_putbad = await call({ path: '/orcid-picker/fetch-bib?orcid=9999-9999-9999-9999&putCode=abc', headers: AH })
  pins.m_bib_putinf = await call({ path: '/orcid-picker/fetch-bib?orcid=9999-9999-9999-9999&putCode=Infinity', headers: AH })

  // deterministic upstream
  pins.m_search_alan = await call({ path: '/orcid-picker/search?q=Alan%20Turing', headers: AH })
  pins.m_works_404 = await call({ path: '/orcid-picker/works?orcid=9999-9999-9999-9999', headers: AH })
  pins.m_bib_404 = await call({ path: '/orcid-picker/fetch-bib?orcid=9999-9999-9999-9999&putCode=123', headers: AH })

  // live result sets (parity-anchored; both legs hit the same registry)
  pins.m_search_turing = await call({ path: '/orcid-picker/search?q=Turing', headers: AH })
  pins.m_works_live = await call({ path: '/orcid-picker/works?orcid=0000-0002-0185-5110', headers: AH })
  pins.m_bib_live = await call({ path: '/orcid-picker/fetch-bib?orcid=0000-0002-0185-5110&putCode=17643012', headers: AH })

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

// ---------- gate legs ----------
let leg1: Leg | null = null

test('leg 0: flip off before start', async () => {
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
}, 60_000)

test('leg 1: Node baseline battery', async () => {
  leg1 = await runLeg()
  expect(Object.keys(leg1!).length).toBe(18)
}, 240_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  const leg2 = await runLeg()
  const ds = diffLegs('go', leg1!, leg2)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 4000))
}, 240_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  const leg3 = await runLeg()
  const ds = diffLegs('node-determinism', leg1!, leg3)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 4000))
}, 240_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1!
  // anon
  expect(L.anon_search_json.status).toBe(401)
  expect(L.anon_search_json.ct).toBe('text/plain')
  expect(L.anon_search_json.body).toBe('Unauthorized')
  expect(L.anon_works.status).toBe(302)
  expect(L.anon_works.loc).toBe('/login')
  expect(L.anon_bib_post.status).toBe(403)
  expect(L.anon_bib_post.body).toBe('Forbidden')
  // 400 battery
  const E = 'application/json'
  expect(L.m_noq.status).toBe(400)
  expect(L.m_noq.body).toBe('{"error":"q query parameter required"}')
  expect(L.m_noq.ct).toBe(E)
  expect(L.m_blankq.status).toBe(400)
  expect(L.m_blankq.body).toBe('{"error":"q query parameter required"}')
  expect(L.m_noorcid.status).toBe(400)
  expect(L.m_noorcid.body).toBe('{"error":"orcid query parameter required"}')
  expect(L.m_badorcid.status).toBe(400)
  expect(L.m_badorcid.body).toBe('{"error":"Invalid ORCID identifier"}')
  expect(L.m_bib_noorcid.status).toBe(400)
  expect(L.m_bib_noorcid.body).toBe('{"error":"orcid query parameter required"}')
  expect(L.m_bib_badorcid.status).toBe(400)
  expect(L.m_bib_badorcid.body).toBe('{"error":"Invalid ORCID identifier"}')
  expect(L.m_bib_noput.status).toBe(400)
  expect(L.m_bib_noput.body).toBe('{"error":"putCode query parameter required"}')
  expect(L.m_bib_putbad.status).toBe(400)
  expect(L.m_bib_putbad.body).toBe('{"error":"putCode query parameter required"}')
  expect(L.m_bib_putinf.status).toBe(400)
  expect(L.m_bib_putinf.body).toBe('{"error":"putCode query parameter required"}')
  // deterministic upstream
  expect(L.m_search_alan.status).toBe(200)
  expect(L.m_search_alan.body).toBe('{"results":[]}')
  expect(L.m_works_404.status).toBe(502)
  expect(L.m_works_404.body).toBe('{"error":"Upstream API responded with 404"}')
  expect(L.m_bib_404.status).toBe(502)
  expect(L.m_bib_404.body).toBe('{"error":"Upstream API responded with 404"}')
  // live pins — shape checks (parity vs leg2/leg3 carries the bytes)
  expect(L.m_search_turing.status).toBe(200)
  const sr = JSON.parse(L.m_search_turing.body)
  expect(Array.isArray(sr.results)).toBe(true)
  for (const r of sr.results) {
    expect(typeof r.orcid).toBe('string')
    expect(typeof r.givenNames).toBe('string')
    expect(typeof r.familyNames).toBe('string')
    expect(Array.isArray(r.institutionNames)).toBe(true)
  }
  expect(L.m_works_live.status).toBe(200)
  const wr = JSON.parse(L.m_works_live.body)
  expect(Array.isArray(wr.works)).toBe(true)
  expect(wr.works.length).toBeGreaterThan(0)
  const yrs = wr.works.map((w: any) => parseInt(w.year, 10) || 0)
  for (let i = 1; i < yrs.length; i++) expect(yrs[i - 1]).toBeGreaterThanOrEqual(yrs[i]) // year-desc
  for (const w of wr.works) {
    expect(typeof w.title).toBe('string')
    expect(typeof w.putCode).toBe('number')
    expect(w.doi === null || typeof w.doi === 'string').toBe(true)
  }
  expect(L.m_bib_live.status).toBe(200)
  const br = JSON.parse(L.m_bib_live.body)
  expect(typeof br.bibtex).toBe('string')
  expect(br.bibtex.startsWith('@')).toBe(true)
})

test.afterAll(async () => {
  try {
    for (const conf of FLIPCONFS) if (flipCount(conf) > 0) await flip('strip')
  } catch {
    /* best effort */
  }
})
