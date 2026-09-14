/**
 * WEB-GO P3.2 FLIP GATE (WEB_GO_PLAN.md P3.2 — instance-stats web leaf):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p3b.conf):
 *     GET  /admin/instance-stats                 (retired page → 301 hub)
 *     GET  /admin/instance-stats/api/series
 *     GET  /admin/instance-stats/api/alert-config
 *     PUT  /admin/instance-stats/api/alert-config
 *     POST /admin/instance-stats/api/send-test-alert-email
 *
 *   leg 1  Node baseline — the full battery via nginx (flip OFF)
 *   leg 2  FLIP ON       — identical battery on Go, byte-for-byte match
 *   leg 3  FLIP OFF      — Node again (reversal)
 *
 * Contract pins (live 2026-09-14, p32pin.json vs p32pin_go.json, 0 diffs):
 *   - 301 matrix (status-independent Accept rules): html → <p>Moved
 *     Permanently. Redirecting to …</p>; textplain, textstar, starstar, or
 *     none → plain; application/json|xml → EMPTY body, NO Content-Type;
 *     application/json|xml → EMPTY body, NO Content-Type; Vary: Accept.
 *   - anonymous: json accept → 401 plain + WWW-Authenticate; else 302 /login.
 *   - non-admin: 302 /restricted?from=%2F<...> (encodeURIComponent).
 *   - series: {metric, window, points:[{day, values}]} day = epoch ms UTC
 *     midnight; window cutoffs day/week/month/6m/year/all; default month.
 *   - alert-config PUT: emails-first validation (split /[\s,;]+, dedupe,
 *     EMAIL_RE), then disk then ram (number 1..100, non-integers legal and
 *     round-tripped as doubles); 400 {"message": ...}; success {"ok":true}.
 *   - test alert: 400 {"message":"Invalid email address[: x]"}; success
 *     {"ok":true,"sentTo":[...]} — one real mail per recipient via the sink.
 *   - x-powered-by: ABSENT on all of the above (Node rule pinned P3.2 —
 *     only /status, csrf-403 and body-parser-400 carry it) — asserted here
 *     via the x-powered-by header in HDRS.
 *
 * State discipline:
 *   - alert config is restored to node defaults (90/90, no emails) in
 *     beforeAll and at the END of each leg (S7) — every leg starts from
 *     the same slate.
 *   - per-leg sink-mail deltas are asserted (3 alerts per leg: legacy 1 +
 *     priority 2).
 *
 * Afterwards the stack is left node-active (e2e convention).
 *
 * Run: npx playwright test -g "web-go P3.2 flip gate"
 */
import { execFileSync } from 'child_process'
import http from 'node:http'
import path from 'path'
import { fileURLToPath } from 'url'
import { test, expect } from '@playwright/test'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const SINK = process.env.E2E_SMTP_SINK || 'http://127.0.0.1:18025'
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')

const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ALERT_SUBJECT = '[Overleaf] Instance stats alert test'
const MAIL_LEGACY = 'p32-legacy@e2e.test'
const MAIL_A = 'p32-a@e2e.test'
const MAIL_B = 'p32-b@e2e.test'
const MAIL_C = 'p32-c@e2e.test'

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

function normBody(s: string): string {
  return s
    .replace(/overleaf\.sid=s%3A[^;,\s]+/g, 'overleaf.sid=SID.S')
    .replace(/Expires=[^;,\s]+/g, 'Expires=EX')
}

function normCookie(s: string): string {
  return (s || '')
    .split('\n')
    .map((l) =>
      l
        .trim()
        .replace(/overleaf\.sid=[^;\n]+/g, 'overleaf.sid=SID')
        .replace(/Expires=[^;\n]+/g, 'Expires=EX')
        .replace(/Path=[^;\n]+/g, 'Path=P')
    )
    .filter(Boolean)
    .sort()
    .join('|')
}

// headers compared for every response (lowercase). date/server/connection
// are stack volatile — out of scope. x-powered-by IS compared (P3.2 pin:
// absent on all these routes).
const HDRS = [
  'content-type',
  'location',
  'vary',
  'etag',
  'x-powered-by',
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
    } catch {
      /* socket reset during reload — retry */
    }
    if (Date.now() - t0 > timeoutMs) throw new Error('nginx never settled after reload')
    await sleep(300)
  }
}

async function call(
  path: string,
  init: RequestInit & { cookie?: string } = {},
  attempt = 0
): Promise<R> {
  try {
    const headers: Record<string, string> = { ...(init.headers as Record<string, string>) }
    if (init.cookie) headers['cookie'] = init.cookie as string
    const r = await fetch(BASE + path, { ...init, headers, redirect: 'manual' })
    const h: Record<string, string> = {}
    for (const key of HDRS) h[key] = r.headers.get(key) || ''
    const sc = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : r.headers.get('set-cookie') || ''
    const body = await r.text()
    return { status: r.status, h, body, setcookie: sc }
  } catch (e) {
    if (attempt < 3 && /socket|ECONNRESET|other side closed|fetch failed/i.test(String(e))) {
      await sleep(400 * (attempt + 1))
      return call(path, init, attempt + 1)
    }
    throw e
  }
}

/** raw HTTP with a FULLY custom header set (the no-Accept 301 row: fetch
 * cannot omit its default accept header). */
function rawCall(path: string, method: string, headers: Record<string, string>, cookie?: string): Promise<R> {
  return new Promise((res, rej) => {
    const u = new URL(BASE + path)
    const hs: Record<string, string> = { ...headers }
    if (cookie) hs['cookie'] = cookie
    const req = http.request({ host: u.hostname, port: u.port, path: u.pathname + u.search, method, headers: hs }, (resp) => {
      const h: Record<string, string> = {}
      for (const key of HDRS) h[key] = (resp.headers as Record<string, string>)[key] || ''
      const scs: string[] = (resp.headers['set-cookie'] as string[]) || []
      let d = ''
      resp.on('data', (c) => (d += c))
      resp.on('end', () => res({ status: resp.statusCode || 0, h, body: d, setcookie: scs.join('\n') }))
    })
    req.on('error', rej)
    req.end()
  })
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

// ---- flip plumbing (web-p3b.conf) ---------------------------------------

const FLIP_APPLY = `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/web-p3b.conf /etc/nginx/overleaf-flips/web-p3b.conf
if ! grep -q "overleaf-flips/web-p3b.conf" "$vhost"; then
  node -e '
    const fs = require("fs");
    const v = process.argv[1];
    const inc = "  include /etc/nginx/overleaf-flips/web-p3b.conf;\\n\\n";
    let s = fs.readFileSync(v, "utf8");
    if (!s.includes("overleaf-flips/web-p3b.conf")) {
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
if grep -q "overleaf-flips/web-p3b.conf" "$vhost"; then
  sed -i "/overleaf-flips\\/web-p3b.conf/d" "$vhost"
  nginx -t && nginx -s reload && sleep 2
fi
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
const J = (h: Record<string, string> = {}) => ({ ...JSONH, ...h })

// ---- state restore (alert config defaults) --------------------------------

async function restoreConfig(admin: Sess): Promise<void> {
  await call('/admin/instance-stats/api/alert-config', {
    method: 'PUT',
    headers: J({ 'x-csrf-token': admin.csrf }),
    cookie: admin.cookie,
    body: JSON.stringify({ alertEmails: [], diskWarningPercent: 90, ramWarningPercent: 90 }),
  })
}

async function verifyDefaults(admin: Sess): Promise<void> {
  const r = await call('/admin/instance-stats/api/alert-config', { cookie: admin.cookie, ...J() })
  expect(r.body, 'config defaults').toBe(
    '{"alertEmails":[],"alertEmail":"","diskWarningPercent":90,"ramWarningPercent":90}'
  )
}

// ---- sink mail accounting --------------------------------------------------

async function sinkAlerts(): Promise<{ count: number; to: string[] }> {
  const r = await fetch(SINK + '/api/messages')
  const env = (await r.json()) as { count: number; messages: Array<{ subject?: string; to?: string | string[] }> }
  const hits = (env.messages || []).filter((m) => m.subject === ALERT_SUBJECT)
  const to: string[] = []
  for (const m of hits) {
    const t = Array.isArray(m.to) ? m.to : [m.to]
    to.push(...t.filter((x) => String(x).startsWith('p32-')))
  }
  return { count: env.count, to: to.sort() }
}

/** multiset difference of the p32-* recipient list (leg-added mails). */
function addedMails(before: string[], after: string[]): string[] {
  const ca = new Map<string, number>()
  for (const x of before) ca.set(x, (ca.get(x) || 0) + 1)
  const out: string[] = []
  for (const x of after) {
    const n = ca.get(x) || 0
    if (n > 0) {
      ca.set(x, n - 1)
      continue
    }
    out.push(x)
  }
  return out.sort()
}

// ---- the battery -----------------------------------------------------------

async function battery(admin: Sess, nonadmin: Sess, anon: Sess): Promise<Record<string, R>> {
  const out: Record<string, R> = {}

  // S0 — state read (defaults; restored before the leg)
  out['S0.1 config defaults (admin)'] = await call('/admin/instance-stats/api/alert-config', {
    cookie: admin.cookie,
    ...J(),
  })

  // S1 — retired page 301 + Accept matrix (admin, logged in)
  out['S1.1 page no-Accept (301 plain)'] = await rawCall('/admin/instance-stats', 'GET', {}, admin.cookie)
  out['S1.2 page text/html'] = await call('/admin/instance-stats', {
    cookie: admin.cookie,
    headers: { accept: 'text/html' },
  })
  out['S1.3 page text/plain'] = await call('/admin/instance-stats', {
    cookie: admin.cookie,
    headers: { accept: 'text/plain' },
  })
  out['S1.4 page application/json (empty body, no CT)'] = await call('/admin/instance-stats', {
    cookie: admin.cookie,
    headers: { accept: 'application/json' },
  })
  out['S1.5 page application/xml (empty body, no CT)'] = await call('/admin/instance-stats', {
    cookie: admin.cookie,
    headers: { accept: 'application/xml' },
  })
  out['S1.6 page text/*'] = await call('/admin/instance-stats', { cookie: admin.cookie, headers: { accept: 'text/*' } })
  out['S1.7 page star/*'] = await call('/admin/instance-stats', { cookie: admin.cookie, headers: { accept: '*/*' } })

  // S2 — anonymous (login gate): json → 401; others → 302 /login
  out['S2.1 config anon json → 401'] = await call('/admin/instance-stats/api/alert-config', {
    cookie: anon.cookie,
    headers: { accept: 'application/json' },
  })
  out['S2.2 config anon text/html → 302 /login'] = await call('/admin/instance-stats/api/alert-config', {
    cookie: anon.cookie,
    headers: { accept: 'text/html' },
  })
  out['S2.3 config anon star/* → 302 /login'] = await call('/admin/instance-stats/api/alert-config', {
    cookie: anon.cookie,
    headers: { accept: '*/*' },
  })
  out['S2.4 config anon xml → 302 (no CT)'] = await call('/admin/instance-stats/api/alert-config', {
    cookie: anon.cookie,
    headers: { accept: 'application/xml' },
  })
  out['S2.5 page anon json → 401'] = await call('/admin/instance-stats', {
    cookie: anon.cookie,
    headers: { accept: 'application/json' },
  })

  // S3 — non-admin (authz): 302 /restricted?from=%2F...
  out['S3.1 config non-admin json → 302 restricted'] = await call('/admin/instance-stats/api/alert-config', {
    cookie: nonadmin.cookie,
    headers: { accept: 'application/json' },
  })
  out['S3.2 page non-admin json → 302 restricted'] = await call('/admin/instance-stats', {
    cookie: nonadmin.cookie,
    headers: { accept: 'application/json' },
  })
  out['S3.3 PUT config non-admin plain → 302 restricted'] = await call('/admin/instance-stats/api/alert-config', {
    method: 'PUT',
    headers: J({ 'x-csrf-token': nonadmin.csrf, accept: 'text/plain' }),
    cookie: nonadmin.cookie,
    body: JSON.stringify({ diskWarningPercent: 10, ramWarningPercent: 20 }),
  })

  // S4 — series: validation + windows (deterministic seeded data)
  out['S4.1 series bad metric → 400'] = await call('/admin/instance-stats/api/series?metric=bogus', {
    cookie: admin.cookie,
    ...J(),
  })
  out['S4.2 series empty metric → 400'] = await call('/admin/instance-stats/api/series?window=month', {
    cookie: admin.cookie,
    ...J(),
  })
  out['S4.3 series bad window → 400'] = await call('/admin/instance-stats/api/series?metric=new_users&window=bogus', {
    cookie: admin.cookie,
    ...J(),
  })
  out['S4.4 series duplicate metric → 400'] = await call(
    '/admin/instance-stats/api/series?metric=new_users&metric=new_users',
    { cookie: admin.cookie, ...J() }
  )
  out['S4.5 series duplicate window → 400'] = await call(
    '/admin/instance-stats/api/series?metric=new_users&window=month&window=month',
    { cookie: admin.cookie, ...J() }
  )
  const ser = (window: string) =>
    call(`/admin/instance-stats/api/series?metric=active_projects&window=${window}`, {
      cookie: admin.cookie,
      ...J(),
    }).then((r) => (out[`S4.6 series active_projects ${window}`] = r))
  await ser('day')
  await ser('week')
  await ser('month')
  await ser('6m')
  await ser('year')
  await ser('all')
  out['S4.7 series new_users day (empty points)'] = await call(
    '/admin/instance-stats/api/series?metric=new_users&window=day',
    { cookie: admin.cookie, ...J() }
  )
  out['S4.8 series default window (month)'] = await call(
    '/admin/instance-stats/api/series?metric=active_projects',
    { cookie: admin.cookie, ...J() }
  )

  // S5 — alert-config PUT validation + round-trips (state ends restored)
  const put = (label: string, body: object) =>
    call('/admin/instance-stats/api/alert-config', {
      method: 'PUT',
      headers: J({ 'x-csrf-token': admin.csrf }),
      cookie: admin.cookie,
      body: JSON.stringify(body),
    }).then((r) => (out[label] = r))

  await put('S5.1 PUT bad email → 400', {
    alertEmails: ['bad'],
    diskWarningPercent: 10,
    ramWarningPercent: 20,
  })
  await put('S5.2 PUT disk string → 400', { diskWarningPercent: 'x', ramWarningPercent: 20 })
  await put('S5.3 PUT disk 0 → 400', { diskWarningPercent: 0, ramWarningPercent: 20 })
  await put('S5.4 PUT disk 101 → 400', { diskWarningPercent: 101, ramWarningPercent: 20 })
  await put('S5.5 PUT ram 101 → 400', { diskWarningPercent: 20, ramWarningPercent: 101 })
  await put('S5.6 PUT disk bool → 400', { diskWarningPercent: true, ramWarningPercent: 20 })
  await put('S5.7 PUT float + string emails → 200', {
    alertEmails: `${MAIL_A} ${MAIL_B}`,
    diskWarningPercent: 55.5,
    ramWarningPercent: 42,
  })
  out['S5.8 GET after float (55.5 round-trip)'] = await call('/admin/instance-stats/api/alert-config', {
    cookie: admin.cookie,
    ...J(),
  })
  await put('S5.9 PUT int → 200', { alertEmails: ['z@w.tv'], diskWarningPercent: 85, ramWarningPercent: 30 })
  out['S5.10 GET after int'] = await call('/admin/instance-stats/api/alert-config', {
    cookie: admin.cookie,
    ...J(),
  })

  // S6 — test alert email (3 mails per leg; recipients fixed per leg)
  out['S6.1 mail bad → 400 named'] = await call('/admin/instance-stats/api/send-test-alert-email', {
    method: 'POST',
    headers: J({ 'x-csrf-token': admin.csrf }),
    cookie: admin.cookie,
    body: JSON.stringify({ emails: 'not-an-email' }),
  })
  out['S6.2 mail none → 400 generic'] = await call('/admin/instance-stats/api/send-test-alert-email', {
    method: 'POST',
    headers: J({ 'x-csrf-token': admin.csrf }),
    cookie: admin.cookie,
    body: JSON.stringify({}),
  })
  out['S6.3 mail legacy → 200 (1 sent)'] = await call('/admin/instance-stats/api/send-test-alert-email', {
    method: 'POST',
    headers: J({ 'x-csrf-token': admin.csrf }),
    cookie: admin.cookie,
    body: JSON.stringify({ email: MAIL_LEGACY }),
  })
  out['S6.4 mail priority → 200 (a+b sent, c dropped)'] = await call(
    '/admin/instance-stats/api/send-test-alert-email',
    {
      method: 'POST',
      headers: J({ 'x-csrf-token': admin.csrf }),
      cookie: admin.cookie,
      body: JSON.stringify({ emails: `${MAIL_A}, ${MAIL_B}`, email: MAIL_C }),
    }
  )

  // S7 — restore defaults (state for the NEXT leg)
  // S6.5 — csrf rejection (no token): 403 "Forbidden" + xpb Express (both
  // stacks send this via res.sendStatus / the pinned 403 shape)
  out['S6.5 PUT config (no csrf token) → 403'] = await call('/admin/instance-stats/api/alert-config', {
    method: 'PUT',
    headers: { 'content-type': 'application/json', 'accept': 'application/json' },
    cookie: admin.cookie,
    body: JSON.stringify({ diskWarningPercent: 10, ramWarningPercent: 20 }),
  })

  // S7 — restore defaults (state for the NEXT leg)
  await restoreConfig(admin)
  out['S7.1 final config defaults'] = await call('/admin/instance-stats/api/alert-config', {
    cookie: admin.cookie,
    ...J(),
  })

  return out
}

// ---- leg comparison --------------------------------------------------------

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
      // These response bodies are fully deterministic (no fresh ids), so
      // the ETag (f(body)) must match when the bodies match — no skip rule.
      let bv = b.h[key]
      let ov = o.h[key]
      if (bv !== ov) problems.push(`${k}: header ${key} "${bv}" != "${ov}"`)
    }
    const nb = normBody(b.body)
    const ob = normBody(o.body)
    if (nb !== ob) {
      let at = 0
      for (let i = 0; i < Math.min(nb.length, ob.length); i++) {
        if (nb[i] !== ob[i]) {
          at = i
          break
        }
      }
      problems.push(
        `${k}: body@${at}\n  base ${JSON.stringify(nb.slice(Math.max(0, at - 80), at + 120))}\n  othr ${JSON.stringify(ob.slice(Math.max(0, at - 80), at + 120))}`
      )
    }
    const bs = normCookie(b.setcookie)
    const os = normCookie(o.setcookie)
    if (bs !== os) problems.push(`${k}: setcookie "${bs.slice(0, 140)}" != "${os.slice(0, 140)}"`)
  }
  return problems
}

// ---- the gate ---------------------------------------------------------------

test.describe.serial('web-go P3.2 flip gate (WEB_GO_PLAN P3.2 instance-stats)', () => {
  let overleafC = ''
  let leg1: Record<string, R> | null = null
  let admin: Sess
  let nonadmin: Sess
  let anon: Sess
  let sinkBase = 0
  let sinkAfterLeg1 = 0
  let sinkAfterLeg2 = 0

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, `chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web`)
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/runit/web-go-overleaf/run'), `${overleafC}:/tmp/webgo-run`])
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p3b.conf'), `${overleafC}:/tmp/web-p3b.conf`])
    dexe(overleafC, `mkdir -p /usr/local/share/overleaf-flips /etc/service/web-go-overleaf && cp /tmp/webgo-run /etc/service/web-go-overleaf/run && chmod 755 /etc/service/web-go-overleaf/run && cp /tmp/web-p3b.conf /usr/local/share/overleaf-flips/web-p3b.conf`)
    // (re)start the shadow with the fresh binary and wait for it
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) {
      const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
      if (code === '200') break
      await sleep(500)
    }
    dexe(overleafC, FLIP_STRIP, true)
    await nginxSettled()
    admin = await login(ADMIN)
    nonadmin = await login(USER)
    anon = await anonSession()
    await restoreConfig(admin)
    await verifyDefaults(admin)
  })

  test.afterAll(async () => {
    try {
      dexe(overleafC, FLIP_STRIP, true)
      await nginxSettled()
      const a = await login(ADMIN)
      await restoreConfig(a)
      await verifyDefaults(a)
    } catch {
      /* gate teardown is best-effort */
    }
  })

  test('leg 1: Node baseline battery', async () => {
    test.setTimeout(600_000)
    dexe(overleafC, FLIP_STRIP, true)
    await nginxSettled()
    admin = await login(ADMIN)
    nonadmin = await login(USER)
    anon = await anonSession()
    sinkBase = (await sinkAlerts()).count
    const mBefore = (await sinkAlerts()).to
    leg1 = await battery(admin, nonadmin, anon)
    expect(Object.keys(leg1).length).toBeGreaterThanOrEqual(30)
    const after = await sinkAlerts()
    expect(addedMails(mBefore, after.to), 'leg1 added mails').toEqual([MAIL_A, MAIL_B, MAIL_LEGACY])
    sinkAfterLeg1 = after.count
    expect(sinkAfterLeg1 - sinkBase).toBe(3)
  }, 600_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(600_000)
    dexe(overleafC, FLIP_APPLY)
    await nginxSettled()
    admin = await login(ADMIN)
    nonadmin = await login(USER)
    anon = await anonSession()
    const mBefore2 = (await sinkAlerts()).to
    const leg2 = await battery(admin, nonadmin, anon)
    const after = await sinkAlerts()
    expect(addedMails(mBefore2, after.to), 'leg2 added mails').toEqual([MAIL_A, MAIL_B, MAIL_LEGACY])
    expect(after.count - sinkAfterLeg1, 'leg2 mail delta').toBe(3)
    sinkAfterLeg2 = after.count
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
    const mBefore3 = (await sinkAlerts()).to
    const leg3 = await battery(admin, nonadmin, anon)
    const after = await sinkAlerts()
    expect(addedMails(mBefore3, after.to), 'leg3 added mails').toEqual([MAIL_A, MAIL_B, MAIL_LEGACY])
    expect(after.count - sinkAfterLeg2, 'leg3 mail delta').toBe(3)
    const problems = diffLegs(leg1!, leg3)
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 600_000)
})
