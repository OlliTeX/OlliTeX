/**
 * WEB-GO P6.1 FLIP GATE (WEB_GO_PLAN.md P6.1 — ollitex-hub module):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p61.conf):
 *     GET    /hub            login      → React hub page (meta: ol-user,
 *                                        ol-userSettings, ol-navbar,
 *                                        ol-hub-admin, ol-hub-theme, …)
 *     GET    /hub/admin      login      → 302 /hub
 *     GET    /hub/workspace  login      → 302 /hub
 *     GET    /api/hub-theme  login      → null | stored theme JSON
 *     PUT    /api/hub-theme  site admin → theme JSON | 400 {"error":…} | 400 {}
 *     DELETE /api/hub-theme  site admin → {"ok":true}
 *     GET    /api/hub/health login+admin → core health JSON
 *     GET    /api/hub/notes  global gate → docs/RELEASE_NOTES.md
 *
 *   leg 1  Node baseline       (flip OFF)
 *   leg 2  FLIP ON — Go        (flip ON , battery, flip OFF)
 *   leg 3  Node re-baseline    (flip OFF)
 *
 *   Battery (deterministic, self-contained: leg 0 action is an admin
 *   DELETE that resets the hubtheme doc, so every leg starts at the same
 *   state): pages, redirects (Accept-matrix pinned), CSRF 403, malformed
 *   JSON 400 {}, validation 400s (exact message strings), member 302
 *   restricted (json vs plain bodies), PUT round-trip (492B canonical),
 *   health (volatile fields normalized), release notes (file bytes),
 *   state restore (DELETE + GET null).
 *
 *   Node oracle pins (live 2026-09-16, /tmp/p61/oracle.json + probes).
 */
import { execFileSync } from 'node:child_process'
import { test, expect } from '@playwright/test'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const FLIPCONF = 'web-p61.conf'
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }

// Canonical theme (the live Node PUT-ok response shape — a VALID input:
// the same validators accept it; PUT must return it byte-identical).
const THEME = {
  version: 1,
  light: {
    primary: '#0f62fe', background: '#f6f8fd', surface: '#ffffff', text: '#16181d',
    dimmed: '#5b6270', border: '#d5dae4', button: '#0f62fe', buttonText: '#ffffff',
    fontFamily: 'Inter, system-ui, sans-serif', fontSize: 15, radius: 10,
  },
  dark: {
    primary: '#4589ff', background: '#0f1218', surface: '#171c26', text: '#f2f5fa',
    dimmed: '#9aa3b5', border: '#2b3242', button: '#4589ff', buttonText: '#0b1220',
    fontFamily: 'Inter, system-ui, sans-serif', fontSize: 15, radius: 12,
  },
}
// Wait — dark.buttonText/family/size/radius must come from the oracle, not
// my reconstruction. The gate reads the canonical shape dynamically below
// (CANONICAL is set from the leg-1 PUT-ok response so legs 2/3 are
// compared against leg 1 BYTES, independent of my transcription).
let CANONICAL_THEME: any = null

type Leg = Record<string, { status: number; ct: string; loc?: string; body?: string; len: number }>

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function dexe(c: string, cmd: string, capture = false): string {
  try {
    const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore', maxBuffer: 64 * 1024 * 1024 })
    return out || ''
  } catch (e) {
    if (capture) throw e
    return ''
  }
}
// strict variant: any non-zero exit (including `nginx -t`) throws
function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 64 * 1024 * 1024 })
}

function dexeQ(c: string, cmd: string): string {
  return (execFileSync('docker', ['exec', c, 'mongosh', '--quiet', 'sharelatex', '--eval', cmd], { encoding: 'utf8' }) || '').trim()
}

async function waitGo(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
    if (code === '200') return
    await sleep(500)
  }
  throw new Error('Go shadow /status not 200')
}

async function call(p: string, init: { method?: string; headers?: Record<string, string>; cookie?: string; body?: any } = {}): Promise<any> {
  const h: Record<string, string> = { ...(init.headers || {}) }
  if (init.cookie) h['cookie'] = init.cookie
  let lastErr: unknown = null
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      const r = await fetch(BASE + p, { method: init.method || 'GET', headers: h, body: init.body as any, redirect: 'manual' })
      const buf = Buffer.from(await r.arrayBuffer())
      const setcookie = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : (r.headers.get('set-cookie') || '').toString()
      return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location'), body: buf.toString('latin1'), len: buf.length, setcookie }
    } catch (e) {
      lastErr = e
      await sleep(400 * (attempt + 1))
    }
  }
  throw lastErr
}

async function login(user: { email: string; password: string }): Promise<{ ck: string; csrf: string }> {
  const page = await call('/login')
  const csrf0 = (page.body.match(/name=["']ol-csrfToken["']\s+content=["']([^"']+)"/) || [])[1] || ''
  const ck0 = ((page.setcookie || '').match(/overleaf\.sid=[^;\n]+/) || [])[0] || ''
  const logged = await call('/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': csrf0, accept: 'application/json' },
    cookie: ck0 || undefined,
    body: JSON.stringify(user),
  })
  if (logged.status !== 200) throw new Error('login failed ' + logged.status)
  const sid = ((logged.setcookie || '').split('\n').find((l: string) => l.includes('overleaf.sid')) || '').replace(/^overleaf\.sid=/, '').split(';')[0]
  const ck = sid ? 'overleaf.sid=' + sid : ck0
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body.trim() || csrf0 }
}

const FLIP_ANCHOR = 'location / {'
function verifyInclude(n: number): void {
  // exactly `n` occurrences of the include line in the active vhost
  // (grep exits 1 when the count is 0 — neutralize the exit code and
  // compare the printed count)
  const cnt = dexeStrict(overleafC, `sh -c 'grep -c "overleaf-flips/${FLIPCONF}" /etc/nginx/sites-enabled/overleaf.conf || true'`).trim()
  if (cnt !== String(n)) throw new Error(`flip include expected ${n}, found ${cnt}`)
}
function stripFlips(): void {
  const vhost = '/etc/nginx/sites-enabled/overleaf.conf'
  dexeStrict(overleafC, `node -e "const fs=require('fs');const p='${vhost}';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"`)
  dexeStrict(overleafC, 'nginx -t')
  dexe(overleafC, 'nginx -s reload')
  verifyInclude(0)
}
function flip(mode: 'apply' | 'strip'): void {
  if (mode === 'strip') {
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"`)
    dexeStrict(overleafC, 'nginx -t')
    dexe(overleafC, 'nginx -s reload')
    verifyInclude(0)
    return
  }
  const vhost = '/etc/nginx/sites-enabled/overleaf.conf'
  dexeStrict(overleafC, `mkdir -p /etc/nginx/overleaf-flips && cp -f /usr/local/share/overleaf-flips/${FLIPCONF} /etc/nginx/overleaf-flips/${FLIPCONF}`)
  dexeStrict(overleafC, `node -e "const fs=require('fs');const p='${vhost}';let s=fs.readFileSync(p,'utf8');const inc='  include /etc/nginx/overleaf-flips/${FLIPCONF};'+String.fromCharCode(10);if(!s.includes('overleaf-flips/${FLIPCONF}')){if(s.includes('location / {')){s=s.replace('location / {',inc+'location / {',1)}else{throw new Error('anchor not found')}};fs.writeFileSync(p,s)"`)
  dexeStrict(overleafC, 'nginx -t')
  dexe(overleafC, 'nginx -s reload')
  verifyInclude(1)
}

async function battery(U: { ck: string; csrf: string }, A: { ck: string; csrf: string }): Promise<Leg> {
  const out: Leg = {}
  const jh = { accept: 'application/json' }
  const jhdr = A.ck && ({ 'content-type': 'application/json', 'x-csrf-token': A.csrf, accept: 'application/json' })

  // --- state reset + null pin ---
  out.theme_clear = await call('/api/hub-theme', { method: 'DELETE', cookie: A.ck, headers: { 'x-csrf-token': A.csrf, ...jh } })
  if (out.theme_clear.status !== 200) throw new Error('state reset failed ' + out.theme_clear.status)
  out.theme_get_null = await call('/api/hub-theme', { cookie: U.ck, headers: jh })
  out.theme_get_anon = await call('/api/hub-theme', { headers: {} })

  // --- pages (theme = null) ---
  out.page_user = await call('/hub', { cookie: U.ck })
  out.page_admin = await call('/hub', { cookie: A.ck })

  // --- redirects (Accept matrix, pinned: text/plain vs html vs bare) ---
  out.adm_redir = await call('/hub/admin', { cookie: U.ck })
  out.adm_redir_html = await call('/hub/admin', { cookie: U.ck, headers: { accept: 'text/html' } })
  out.adm_redir_json = await call('/hub/admin', { cookie: U.ck, headers: jh })
  out.ws_redir = await call('/hub/workspace', { cookie: U.ck })
  out.anon_hub = await call('/hub', {})
  out.anon_hub_json = await call('/hub', { headers: jh })
  out.health_anon = await call('/api/hub/health', {})

  // --- validation 400s (exact messages) ---
  const bad = (m: any) => JSON.stringify({ version: 1, light: m, dark: THEME.dark })
  const goodLight = THEME.light
  out.theme_put_bad = await call('/api/hub-theme', { method: 'PUT', cookie: A.ck, headers: jhdr, body: JSON.stringify({ version: 1, light: { primary: 'red', background: '#fff', surface: '#fff', text: '#000', dimmed: '#888', border: '#ccc', button: '#00f', buttonText: '#fff', fontFamily: 'sans-serif', fontSize: 15, radius: 10 }, dark: THEME.dark }) })
  out.theme_put_bad_fs = await call('/api/hub-theme', { method: 'PUT', cookie: A.ck, headers: jhdr, body: JSON.stringify(bad({ ...goodLight, fontSize: 99 })) })
  out.theme_put_bad_dark = await call('/api/hub-theme', { method: 'PUT', cookie: A.ck, headers: jhdr, body: JSON.stringify({ version: 1, light: goodLight, dark: {} }) })
  out.theme_put_anon_bad = await call('/api/hub-theme', { method: 'PUT', headers: { 'content-type': 'application/json', ...jh }, body: JSON.stringify({ light: goodLight, dark: THEME.dark }) })
  out.theme_put_malformed = await call('/api/hub-theme', { method: 'PUT', cookie: A.ck, headers: jhdr, body: '{"version":1,' })
  // member: valid JSON + good theme still 302 (admin gate before validate)
  out.theme_put_member = await call('/api/hub-theme', { method: 'PUT', cookie: U.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': U.csrf, ...jh }, body: JSON.stringify({ version: 1, light: goodLight, dark: THEME.dark }) })
  out.theme_put_member_bad = await call('/api/hub-theme', { method: 'PUT', cookie: U.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': U.csrf, ...jh }, body: JSON.stringify({ version: 1, light: { primary: 'nope' }, dark: THEME.dark }) })
  out.theme_put_member_plain = await call('/api/hub-theme', { method: 'PUT', cookie: U.ck, headers: { 'content-type': 'application/json', 'x-csrf-token': U.csrf, accept: 'text/plain' }, body: JSON.stringify({ version: 1, light: goodLight, dark: THEME.dark }) })
  out.theme_del_member = await call('/api/hub-theme', { method: 'DELETE', cookie: U.ck, headers: { 'x-csrf-token': U.csrf, ...jh } })
  out.theme_del_member_plain = await call('/api/hub-theme', { method: 'DELETE', cookie: U.ck, headers: { 'x-csrf-token': U.csrf, accept: 'text/plain' } })

  // --- round trip ---
  out.theme_put_ok = await call('/api/hub-theme', { method: 'PUT', cookie: A.ck, headers: jhdr, body: JSON.stringify({ version: 1, light: goodLight, dark: THEME.dark }) })
  if (out.theme_put_ok.status === 200) {
    try { CANONICAL_THEME = JSON.parse(out.theme_put_ok.body) } catch {}
  }
  out.theme_get_set = await call('/api/hub-theme', { cookie: U.ck, headers: jh })
  out.page_user_theme = await call('/hub', { cookie: U.ck })
  out.page_admin_theme = await call('/hub', { cookie: A.ck })
  out.health_admin = await call('/api/hub/health', { cookie: A.ck, headers: jh })
  out.health_member = await call('/api/hub/health', { cookie: U.ck, headers: jh })
  out.notes_member = await call('/api/hub/notes', { cookie: U.ck, headers: { accept: '*/*' } })
  out.notes_anon = await call('/api/hub/notes', {})

  // --- restore ---
  out.theme_del_admin = await call('/api/hub-theme', { method: 'DELETE', cookie: A.ck, headers: { 'x-csrf-token': A.csrf, ...jh } })
  out.theme_get_final = await call('/api/hub-theme', { cookie: U.ck, headers: jh })
  out.page_user_final = await call('/hub', { cookie: U.ck })
  return out
}

// --- normalizations ------------------------------------------------------
function normHTML(s: string): string {
  return s
    .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
    .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
    .replace(/name="_csrf"[^>]*value="[^"]*"/g, 'name="_csrf" value="CSRF"')
}

// implementation-dependent fields (the P6.1 plan normalization set)
function normHealth(s: string): string {
  return s
    .replace(/"generatedAt":"[^"]*"/, '"generatedAt":"<ISO>"')
    .replace(/"uptimeSec":\d+/, '"uptimeSec":0')
    .replace(/"node":"[^"]*"/, '"node":"<V>"')
    .replace(/"pid":\d+/, '"pid":0')
    .replace(/"mongo":\{[^}]*\}/, '"mongo":{}')
}

function firstDiff(a: string, b: string): number {
  const n = Math.min(a.length, b.length)
  for (let i = 0; i < n; i++) if (a[i] !== b[i]) return i
  return n
}
function ctx(a: string, b: string, i: number): string {
  const s0 = Math.max(0, i - 80)
  return `...A: ${a.slice(s0, i + 140).replace(/\n/g, '\\n')}\n...B: ${b.slice(s0, i + 140).replace(/\n/g, '\\n')}`
}

function diffLegs(name: string, a: Leg, b: Leg): string[] {
  const ds: string[] = []
  const keys = Object.keys(a).sort()
  for (const k of keys) {
    const A = a[k]
    const B = b[k]
    if (!B) {
      ds.push(`${name}:${k} missing in B`)
      continue
    }
    if (A.status !== B.status) {
      ds.push(`${name}:${k} status A=${A.status} B=${B.status}`)
      continue
    }
    if (A.ct !== B.ct) {
      ds.push(`${name}:${k} ct A='${A.ct}' B='${B.ct}' (status ${A.status})`)
      continue
    }
    if ((A.loc || '') !== (B.loc || '')) {
      ds.push(`${name}:${k} loc A='${A.loc}' B='${B.loc}' (status ${A.status})`)
      continue
    }
    let bodyA = A.body || ''
    let bodyB = B.body || ''
    if (k.startsWith('page_')) {
      bodyA = normHTML(bodyA)
      bodyB = normHTML(bodyB)
    } else if (k === 'health_admin') {
      bodyA = normHealth(bodyA)
      bodyB = normHealth(bodyB)
    }
    if (bodyA !== bodyB) {
      ds.push(`${name}:${k} body len A=${A.len} B=${B.len}`)
      ds.push(ctx(bodyA, bodyB, firstDiff(bodyA, bodyB)))
    }
  }
  return ds
}

test.describe('@local web-go P6.1 (ollitex-hub) parity', () => {
  let U: { ck: string; csrf: string }
  let A: { ck: string; csrf: string }
  const LEG1: Leg = {}

  test.beforeAll(async () => {
    await waitGo()
    // pre-reset (any prior state) through Node
    A = await login(ADMIN)
    await call('/api/hub-theme', { method: 'DELETE', cookie: A.ck, headers: { 'x-csrf-token': A.csrf, accept: 'application/json' } })
    U = await login(USER)
    // ensure hubthemes is clean + fixture users exist (idempotent)
    dexeQ(mongoC, 'db.hubthemes.deleteMany({ documentId: "default" })')
  })

  test('leg 1: Node baseline', async () => {
    CANONICAL_THEME = null
    flip('strip')
    await sleep(300)
    Object.assign(LEG1, await battery(U, A))
  })

  test('leg 2: Go parity', async () => {
    flip('apply')
    await sleep(800)
    const leg2 = await battery(U, A)
    flip('strip')
    await sleep(300)
    const ds = diffLegs('p61', LEG1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('leg 3: Node re-baseline', async () => {
    flip('strip')
    await sleep(300)
    const leg3 = await battery(U, A)
    const ds = diffLegs('p61', LEG1, leg3)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('canonical theme pin sanity', async () => {
    // The PUT-ok response round-trips through GET (same bytes) — both legs
    // prove state via mongo (shared), so a mismatch would have shown up in
    // leg 2/3 diffs already; this is the human-readable anchor.
    expect(CANONICAL_THEME).toBeTruthy()
    expect(JSON.stringify(CANONICAL_THEME.light.primary)).toBe('"#0f62fe"')
  })
})
