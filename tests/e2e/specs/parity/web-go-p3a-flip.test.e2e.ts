/**
 * WEB-GO P3.1 FLIP GATE (WEB_GO_PLAN.md P3.1 — ServerAdmin leaf):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p3a.conf):
 *     GET    /admin/editor-state
 *     POST   /admin/openEditor
 *     POST   /admin/closeEditor
 *     POST   /admin/messages
 *     POST   /admin/messages/clear
 *     PATCH  /admin/messages/:id
 *     DELETE /admin/messages/:id
 *   (plus the public GET /system/messages list, unflipped on both legs —
 *   it reads the same mongo collection the flipped routes mutate).
 *
 *   leg 1  Node baseline — the full battery via nginx (flip OFF)
 *   leg 2  FLIP ON       — identical battery on Go, byte-for-byte match
 *   leg 3  FLIP OFF      — Node again (reversal)
 *
 * Contract pins (live, 2026-09-13/14, p3pin*.json + redprobe matrix):
 *   - 302 matrix: Accept texthtml → <p>Found…</p> text/html CT;
 *     textplain, the text-star form, starstar, or none → plain Found…
 *     text/plain CT; application/json or xml → EMPTY body, NO Content-Type;
 *     Vary: Accept on every 302; no ETag on redirects.
 *   - tri-state closeEditor: {} → siteIsOpen=false but editorIsOpen=true
 *     (undefined !== false); {isOpen:false} → both closed.
 *   - zod shapes verbatim (content/placements/isOpen/unknown-keys).
 *   - 500s are the rendered general/500 page (nonce CSP).
 *
 * State discipline:
 *   - beforeAll + each leg STARTS by clearing system_messages, and the
 *     battery ends with a clear → every leg runs from the same slate.
 *   - editor state transitions (S2) are fully deterministic and END open
 *     (openEditor) — beforeAll also forces open via Node.
 *   - message ids are taken from the (normalized) list per leg, so the
 *     PATCH/DELETE sequences are identical across legs.
 *
 * Afterwards the stack is left node-active (e2e convention).
 *
 * Run: npx playwright test -g "web-go P3.1 flip gate"
 */
import { execFileSync } from 'child_process'
import http from 'node:http'
import path from 'path'
import { fileURLToPath } from 'url'
import { test, expect } from '@playwright/test'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')

const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const MSG_A = 'P3A leg message A'
const MSG_B = 'P3A leg message B'

// ---- docker helpers ------------------------------------------------------

function dexe(container: string, cmd: string, allowFail = false): string {
  try {
    return execFileSync('docker', ['exec', container, 'bash', '-c', cmd], {
      encoding: 'utf8',
      maxBuffer: 64 * 1024 * 1024,
      timeout: 120_000,
    })
  } catch (e: unknown) {
    if (allowFail) return ''
    throw e
  }
}

function runningContainer(match: string): string {
  const out = execFileSync('docker', ['ps', '--filter', `name=${match}`, '--format', '{{.Names}}'], {
    encoding: 'utf8',
  }).trim()
  const names = out.split('\n')
  if (!names.length) throw new Error(`no running container matching ${match}`)
  return names[0]
}

// ---- normalization -------------------------------------------------------

const NONCE_RE = '[A-Za-z0-9+/=_-]{16,}'

function normBody(s: string): string {
  return s
    .replace(/nonce[=-]"?${NONCE_RE}"?/g, 'nonce=N')
    .replace(/nonce="N"?/g, 'nonce=N')
    .replace(/name="ol-csrfToken" content="[^"]*"/g, 'name="ol-csrfToken" content="T"')
    .replace(/"6a[0-9a-f]{22}"/g, '"ID"')
    .replace(/overleaf\.sid=s%3A[^;,\s]+/g, 'overleaf.sid=SID')
    .replace(/overleaf\.sid=s:[^;,\s]+/g, 'overleaf.sid=SID')
    .replace(/Expires=[^;,\s]+/g, 'Expires=EX')
}

function normCsp(s: string): string {
  return (s || '').replace(new RegExp(`nonce-(?:${NONCE_RE})`, 'g'), 'nonce=N')
}

function normCookie(s: string): string {
  return normBody(s)
    .replace(/Path=[^;\s]+/g, 'Path=P')
    .replace(/Expires=[^;]+/g, 'Expires=EX')
    .replace(/Max-Age=\d+/g, 'Max-Age=MA')
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .sort()
    .join('|')
}

// headers compared for every response (lowercase). date/server/connection/
// x-powered-by are stack-volatility or known asymmetries — out of scope.
const HDRS = [
  'content-type',
  'location',
  'vary',
  'etag',
  'content-security-policy',
  'permissions-policy',
  'cross-origin-opener-policy',
  'cross-origin-resource-policy',
  'referrer-policy',
  'x-content-type-options',
  'x-download-options',
  'x-frame-options',
  'x-permitted-cross-domain-policies',
  'x-xss-protection',
  'cache-control',
  'expires',
  'pragma',
  'surrogate-control',
  'www-authenticate',
] as const

interface R {
  status: number
  h: Record<string, string>
  body: string
  setcookie: string
}

/** nginx -s reload is asynchronous — poll until the vhost answers again. */
async function nginxSettled(timeoutMs = 15_000): Promise<void> {
  const t0 = Date.now()
  for (;;) {
    try {
      const r = await fetch(BASE + '/status', { redirect: 'manual' })
      if (r.status >= 100) {
        await r.text().catch(() => {})
        return
      }
    } catch { /* socket reset during reload — retry */ }
    if (Date.now() - t0 > timeoutMs) throw new Error('nginx never settled after reload')
    await sleep(300)
  }
}

/** one transparent retry on reload-race socket resets. */
async function callT(path: string, init: RequestInit & { cookie?: string; rawHeaders?: Record<string, string> } = {}, attempt = 0): Promise<R> {
  try {
    return await call(path, init)
  } catch (e) {
    if (attempt < 3 && /socket|ECONNRESET|other side closed|fetch failed/i.test(String(e))) {
      await sleep(400 * (attempt + 1))
      return await callT(path, init, attempt + 1)
    }
    throw e
  }
}

async function call(path: string, init: RequestInit & { cookie?: string; rawHeaders?: Record<string, string> } = {}): Promise<R> {
  const headers: Record<string, string> = { ...(init.headers as Record<string, string>) }
  if (init.cookie) headers['cookie'] = init.cookie as string
  for (const [k, v] of Object.entries(init.rawHeaders || {})) headers[k] = v
  const r = await fetch(BASE + path, { ...init, headers, redirect: 'manual' })
  const h: Record<string, string> = {}
  for (const key of HDRS) h[key] = r.headers.get(key) || ''
  h['etag'] = h['etag'] || ''
  const sc = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : r.headers.get('set-cookie') || ''
  const body = await r.text()
  return { status: r.status, h, body, setcookie: sc }
}

/** raw HTTP with a FULLY custom header set (used for the no-Accept case,
 * which fetch cannot express). */
function rawCall(path: string, method: string, headers: Record<string, string>, body?: string): Promise<R> {
  return new Promise((res, rej) => {
    const u = new URL(BASE + path)
    const req = http.request(
      { host: u.hostname, port: u.port, path: u.pathname + u.search, method, headers },
      (resp) => {
        const hs: Record<string, string> = {}
        for (const key of HDRS) hs[key] = resp.headers[key] || ''
        hs['etag'] = resp.headers['etag'] || ''
        const scs: string[] = resp.headers['set-cookie'] || []
        let d = ''
        resp.on('data', (c) => (d += c))
        resp.on('end', () =>
          res({ status: resp.statusCode || 0, h: hs, body: d, setcookie: scs.join('\n') })
        )
      }
    )
    req.on('error', rej)
    if (body) req.write(body)
    req.end()
  })
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

// ---- flip plumbing (web-p3a.conf) ---------------------------------------

const FLIP_APPLY = `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/web-p3a.conf /etc/nginx/overleaf-flips/web-p3a.conf
if ! grep -q "overleaf-flips/web-p3a.conf" "$vhost"; then
  node -e '
    const fs = require("fs");
    const v = process.argv[1];
    const inc = "  include /etc/nginx/overleaf-flips/web-p3a.conf;\\n\\n";
    let s = fs.readFileSync(v, "utf8");
    if (!s.includes("overleaf-flips/web-p3a.conf")) {
      const lines = s.split("\\n");
      const idx = lines.findIndex((l) => l.trim() === "location / {");
      if (idx < 0) throw new Error("location / open line not found in vhost");
      lines.splice(idx, 0, inc);
      fs.writeFileSync(v, lines.join("\\n"));
    }
  ' "$vhost"
fi
nginx -t && nginx -s reload && sleep 2
`
const FLIP_STRIP = `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
if grep -q "overleaf-flips/web-p3a.conf" "$vhost"; then
  sed -i "/overleaf-flips\\/web-p3a.conf/d" "$vhost"
  nginx -t && nginx -s reload && sleep 2
fi
rm -rf /etc/nginx/overleaf-flips
`

// ---- sessions ------------------------------------------------------------

interface Sess {
  cookie: string
  csrf: string
}

async function anonSession(): Promise<Sess> {
  const page = await call('/login')
  const tok = (page.body.match(/name="ol-csrfToken" content="([^"]+)"/) || [])[1] || ''
  const ck = (page.setcookie.match(/overleaf\.sid=[^;,\s]+/) || [''])[0]
  if (!ck) throw new Error('anon session cookie not issued')
  return { cookie: ck, csrf: tok }
}

/** full login through the (never-flipped) login route for either account. */
async function login(who: { email: string; password: string }): Promise<Sess> {
  const a = await anonSession()
  const r = await call('/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': a.csrf, accept: 'application/json' },
    cookie: a.cookie,
    body: JSON.stringify({ email: who.email, password: who.password }),
  })
  expect(r.status, `login ${who.email}`).toBe(200)
  const lines = r.setcookie.split('\n').filter((l) => l.includes('overleaf.sid'))
  await sleep(500)
  const csrf = (await call('/dev/csrf', { cookie: lines[lines.length - 1] || a.cookie })).body.trim()
  return { cookie: lines[lines.length - 1] || a.cookie, csrf }
}

const JSONH = { 'content-type': 'application/json', accept: 'application/json' } as const

// ---- state restore (Node-only, never flipped) -----------------------------

async function restoreState(admin: Sess): Promise<void> {
  // clear all messages (idempotent), force editor+site open
  await call('/admin/messages/clear', {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': admin.csrf }, cookie: admin.cookie,
  })
  await call('/admin/openEditor', {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': admin.csrf }, cookie: admin.cookie,
  })
}

async function verifyOpen(admin: Sess): Promise<void> {
  const r = await call('/admin/editor-state', { cookie: admin.cookie })
  expect(r.body, 'editor-state after restore').toBe('{"editorIsOpen":true,"siteIsOpen":true}')
}

// ---- the battery -----------------------------------------------------------

const A_STAR = { accept: '*/*' } as const

/** list read after a mutation — small grace for the pubsub-driven cache
 * refresh on the stack that READS after the OTHER stack WROTE. */
async function listAfter(admin: Sess, anon: Sess): Promise<R> {
  for (let i = 0; i < 12; i++) {
    const r = await call('/system/messages', { cookie: anon.cookie, ...A_STAR })
    if (r.body.includes(MSG_A) || i === 11) return r
    await sleep(120)
  }
  throw new Error('unreachable')
}

async function battery(admin: Sess, nonadmin: Sess, anon: Sess): Promise<Record<string, R>> {
  const out: Record<string, R> = {}
  const J = (h: Record<string, string>) => ({ ...JSONH, ...h })

  // S0 — clean slate (deterministic per leg)
  out['S0.1 clear (plain accept)'] = await call('/admin/messages/clear', {
    method: 'POST', headers: J({ accept: 'text/plain', 'x-csrf-token': admin.csrf }), cookie: admin.cookie,
  })
  await sleep(600) // cache-refresh grace for the reader stack
  out['S0.2 list emptied (anon)'] = await call('/system/messages', { cookie: anon.cookie })
  out['S0.3 editor-state (admin)'] = await call('/admin/editor-state', { cookie: admin.cookie, ...A_STAR })

  // S1 — authorization + method-not-allowed
  out['S1.1 editor-state anon'] = await call('/admin/editor-state', { cookie: anon.cookie })
  out['S1.2 editor-state non-admin'] = await call('/admin/editor-state', { cookie: nonadmin.cookie })
  out['S1.3 closeEditor non-admin'] = await call('/admin/closeEditor', {
    method: 'POST', headers: J({ 'x-csrf-token': nonadmin.csrf }), cookie: nonadmin.cookie, body: '{}',
  })
  out['S1.4 POST editor-state (404 Cannot POST)'] = await call('/admin/editor-state', { method: 'POST' })
  out['S1.5 messages anon POST (no csrf token) → 403'] = await call('/admin/messages', { method: 'POST', cookie: anon.cookie, body: '{}' })

  // S2 — editor tri-state transitions (deterministic; ends OPEN)
  out['S2.1 closeEditor {} → 302 html'] = await call('/admin/closeEditor', {
    method: 'POST', headers: J({ accept: 'text/html', 'x-csrf-token': admin.csrf }), cookie: admin.cookie, body: '{}',
  })
  out['S2.2 state: site closed, editor OPEN'] = await call('/admin/editor-state', { cookie: admin.cookie })
  out['S2.3 closeEditor {isOpen:false} → 302 empty'] = await call('/admin/closeEditor', {
    method: 'POST', headers: J({ 'x-csrf-token': admin.csrf }), cookie: admin.cookie, body: '{"isOpen":false}',
  })
  out['S2.4 state: both closed'] = await call('/admin/editor-state', { cookie: admin.cookie })
  {
    const noh = { cookie: admin.cookie, 'content-type': 'application/json', 'x-csrf-token': admin.csrf }
    out['S2.5 openEditor no-Accept (plain)'] = await rawCall('/admin/openEditor', 'POST', noh, '{"junk":1}')
  }
  out['S2.6 state: both open'] = await call('/admin/editor-state', { cookie: admin.cookie })

  // S3 — create matrix
  out['S3.1 create A [editor,hub] → 302'] = await call('/admin/messages', {
    method: 'POST', headers: J({ 'x-csrf-token': admin.csrf }), cookie: admin.cookie,
    body: JSON.stringify({ content: MSG_A, placements: ['editor', 'hub'] }),
  })
  out['S3.2 create B (no placements) → 302'] = await call('/admin/messages', {
    method: 'POST', headers: J({ 'x-csrf-token': admin.csrf }), cookie: admin.cookie,
    body: JSON.stringify({ content: MSG_B }),
  })
  out['S3.3 list after creates (anon)'] = await listAfter(admin, anon)

  const badCreate = (label: string, body: string) =>
    call('/admin/messages', {
      method: 'POST', headers: J({ 'x-csrf-token': admin.csrf }), cookie: admin.cookie, body,
    }).then((r) => (out[label] = r))

  await badCreate('S3.4 create {} → 400', '{}')
  await badCreate('S3.5 create {content:5}', '{"content":5}')
  await badCreate('S3.6 create {content:"",placements:[5]}', '{"content":"","placements":[5]}')
  await badCreate('S3.7 create placements 5×valid', '{"content":"x","placements":["editor","hub","auth","editor","hub"]}')
  await badCreate('S3.8 create placements string>4', '{"content":"x","placements":"editorx"}')
  await badCreate('S3.9 create placements bad elem', '{"content":"x","placements":["editor","xxx"]}')
  await badCreate('S3.10 create unknown key', '{"content":"x","junk":1}')
  await badCreate('S3.11 create unknown keys (order)', '{"content":"x","zz":1,"aa":2}')
  await badCreate('S3.12 create body 5', '5')
  await badCreate('S3.13 create body [1]', '[1]')

  // S4 — PATCH/DELETE (ids from the list: marker A first, marker B after)
  const listNow = await call('/system/messages', { cookie: admin.cookie, ...A_STAR })
  let idA = ''
  let idB = ''
  {
    const docs = JSON.parse(listNow.body)
    const a = docs.find((d: { content: string }) => d.content === MSG_A)
    const b = docs.find((d: { content: string }) => d.content === MSG_B)
    idA = (a?._id as string) || ''
    idB = (b?._id as string) || ''
    if (!idA || !idB) throw new Error(`battery fixtures missing: ${listNow.body.slice(0, 200)}`)
  }
  const patch = (label: string, body: string, hdrs: Record<string, string>) =>
    call(`/admin/messages/${idA}`, { method: 'PATCH', headers: J(hdrs), cookie: admin.cookie, body })
      .then((r) => (out[label] = r))

  await patch('S4.1 PATCH [auth] xhr → 200 success', '{"placements":["auth"]}', { 'x-requested-with': 'XMLHttpRequest' })
  await patch('S4.2 PATCH [editor,hub] plain → 302', '{"placements":["editor","hub"]}', { accept: 'text/plain' })
  await patch('S4.3 PATCH {} → 400', '{}', {})
  await patch('S4.4 PATCH placements "hub" → 400', '{"placements":"hub"}', {})
  await patch('S4.5 PATCH bad element → 400', '{"placements":["hub","xxx"]}', {})
  await patch('S4.6 PATCH 5 items → 400', '{"placements":["editor","hub","auth","editor","hub"]}', {})
  await patch('S4.7 PATCH unknown key → 400', '{"placements":["editor"],"junk":1}', {})
  await sleep(700) // pubsub refresh grace (Go leg publishes)
  out['S4.8 list after PATCH (M1=[editor,hub])'] = await call('/system/messages', { cookie: admin.cookie, ...A_STAR })

  out['S4.9 DELETE A xhr → 200 success'] = await call(`/admin/messages/${idA}`, {
    method: 'DELETE', headers: J({ 'x-requested-with': 'XMLHttpRequest', 'x-csrf-token': admin.csrf }), cookie: admin.cookie,
  })
  out['S4.10 DELETE A again → 500 page'] = await call(`/admin/messages/${idA}`, {
    method: 'DELETE', headers: J({ 'x-requested-with': 'XMLHttpRequest', 'x-csrf-token': admin.csrf }), cookie: admin.cookie,
  })
  out['S4.11 DELETE abc → 500 (cast)'] = await call('/admin/messages/abc', {
    method: 'DELETE', headers: J({ 'x-requested-with': 'XMLHttpRequest', 'x-csrf-token': admin.csrf }), cookie: admin.cookie,
  })
  out['S4.12 DELETE B plain → 302'] = await call(`/admin/messages/${idB}`, {
    method: 'DELETE', headers: J({ accept: 'text/plain', 'x-csrf-token': admin.csrf }), cookie: admin.cookie,
  })

  // S5 — back to slate + final state reads
  out['S5.1 clear (html accept)'] = await call('/admin/messages/clear', {
    method: 'POST', headers: J({ accept: 'text/html', 'x-csrf-token': admin.csrf }), cookie: admin.cookie,
  })
  await sleep(700)
  out['S5.2 list final (anon)'] = await call('/system/messages', { cookie: anon.cookie })
  out['S5.3 list final (non-admin)'] = await call('/system/messages', { cookie: nonadmin.cookie })
  out['S5.4 editor-state final'] = await call('/admin/editor-state', { cookie: admin.cookie })
  return out
}

// ---- leg comparison --------------------------------------------------------

const normHas = (r: R, needle: string): boolean => r.body.includes(needle)

function diffLegs(base: Record<string, R>, other: Record<string, R>): string[] {
  const problems: string[] = []
  const keys = Array.from(new Set([...Object.keys(base), ...Object.keys(other)])).sort()
  for (const k of keys) {
    const b = base[k]
    const o = other[k]
    if (!b || !o) {
      problems.push(`${k}: missing in ${b ? 'other' : 'base'} leg`)
      continue
    }
    if (b.status !== o.status) problems.push(`${k}: status ${b.status} != ${o.status}`)
    for (const key of HDRS) {
      // Fresh-content rule: bodies containing newly created `_id` values
      // legitimately differ per leg (each leg creates its own docs), and
      // the ETag is a pure function of the raw body — so it cannot match
      // across legs. Compare everything else byte-exact.
      if (key === 'etag' && normHas(b, '"_id"') && normHas(o, '"_id"')) continue
      let bv = b.h[key]
      let ov = o.h[key]
      if (key === 'content-security-policy') {
        bv = normCsp(bv)
        ov = normCsp(ov)
      }
      if (bv !== ov) problems.push(`${k}: header ${key} "${bv}" != "${ov}"`)
    }
    const nb = normBody(b.body)
    const ob = normBody(o.body)
    if (nb !== ob) {
      let at = Math.min(nb.length, ob.length)
      for (let i = 0; i < Math.min(nb.length, ob.length); i++) if (nb[i] !== ob[i]) { at = i; break }
      problems.push(
        `${k}: body@${at}\n  base ${JSON.stringify(nb.slice(Math.max(0, at - 80), at + 80))}\n  othr ${JSON.stringify(ob.slice(Math.max(0, at - 80), at + 80))}`
      )
    }
    const bs = normCookie(b.setcookie)
    const os = normCookie(o.setcookie)
    if (bs !== os) problems.push(`${k}: setcookie "${bs.slice(0, 140)}" != "${os.slice(0, 140)}"`)
  }
  return problems
}

// ---- the gate ---------------------------------------------------------------

test.describe.serial('web-go P3.1 flip gate (WEB_GO_PLAN P3.1 serveradmin)', () => {
  let overleafC = ''
  let leg1: Record<string, R> | null = null
  let admin: Sess
  let nonadmin: Sess
  let anon: Sess

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    // Cross-stack list freshness: the Node pod caches /system/messages and
    // only refreshes on pubsub (NOTIFY_ON_SYSTEM_MESSAGE_CHANGES) or a
    // 20-30s background tick. Upstream CE ships this flag on; the gate
    // needs the pubsub path (the Go mutations publish on the same channel),
    // so ensure it and restart web if missing.
    {
      const envline = 'export NOTIFY_ON_SYSTEM_MESSAGE_CHANGES=true'
      const dexe = (cmd: string) => execFileSync('docker', ['exec', overleafC, 'bash', '-c', cmd], { encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, timeout: 60_000 })
      let cur = ''
      try { cur = dexe('cat /etc/overleaf/env.sh 2>/dev/null') || '' } catch { cur = '' }
      if (!cur.includes('NOTIFY_ON_SYSTEM_MESSAGE_CHANGES')) {
        dexe(`printf '\\n${envline}\\n' >> /etc/overleaf/env.sh`)
        dexe('sv restart /etc/service/web-overleaf')
        const t0 = Date.now()
        for (;;) {
          const code = dexe('curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4000/status').trim()
          if (code === '200') break
          if (Date.now() - t0 > 25_000) throw new Error('node web did not come back after env restart')
          await sleep(400)
        }
      }
    }
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'bin/web'), `${overleafC}:/usr/local/bin/go-services/web`])
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/runit/web-go-overleaf/run'), `${overleafC}:/tmp/webgo-run`])
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p3a.conf'), `${overleafC}:/tmp/web-p3a.conf`])
    dexe(overleafC, `mkdir -p /usr/local/share/overleaf-flips /etc/service/web-go-overleaf && cp /tmp/webgo-run /etc/service/web-go-overleaf/run && chmod 755 /etc/service/web-go-overleaf/run && cp /tmp/web-p3a.conf /usr/local/share/overleaf-flips/web-p3a.conf`)
    const t0 = Date.now()
    for (;;) {
      const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
      if (code === '200') break
      if (Date.now() - t0 > 20_000) throw new Error(`go shadow never came up (last ${code})`)
      await sleep(500)
    }
    dexe(overleafC, FLIP_STRIP, true) // ensure flip OFF for baseline
    await nginxSettled()
    admin = await login(ADMIN)
    nonadmin = await login(USER)
    anon = await anonSession()
    await restoreState(admin)
    await sleep(500)
    await verifyOpen(admin)
  })

  test.afterAll(async () => {
    try {
      dexe(overleafC, FLIP_STRIP, true)
      if (admin) await restoreState(admin)
      if (admin) await verifyOpen(admin)
    } catch {
      /* gate teardown is best-effort */
    }
  })

  test('leg 1: Node baseline battery', async () => {
    test.setTimeout(600_000)
    dexe(overleafC, FLIP_STRIP, true)
    await nginxSettled()
    leg1 = await battery(admin, nonadmin, anon)
    expect(Object.keys(leg1).length).toBeGreaterThanOrEqual(30)
  }, 600_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(600_000)
    dexe(overleafC, FLIP_APPLY)
    await nginxSettled()
    admin = await login(ADMIN)
    nonadmin = await login(USER)
    anon = await anonSession()
    const leg2 = await battery(admin, nonadmin, anon)
    const problems = diffLegs(leg1!, leg2)
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 600_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(600_000)
    dexe(overleafC, FLIP_STRIP)
    await nginxSettled()
    admin = await login(ADMIN)
    nonadmin = await login(USER)
    anon = await anonSession()
    const leg3 = await battery(admin, nonadmin, anon)
    const problems = diffLegs(leg1!, leg3)
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 600_000)
})
