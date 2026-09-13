/**
 * WEB-GO P1 AUTH FLIP GATE (WEB_GO_PLAN.md P1 — the auth/static/messages
 * wave behind the nginx flip):
 *
 *   leg 1  Node baseline  the P1 route battery answered by Node through
 *                         nginx :7420 (flip OFF): `/` anon redirect, the
 *                         XHR 401 challenge, `/system/messages` anon, the
 *                         login page, a full login (JSON), logged-in
 *                         `/restricted` + `/` → /hub, the logout page +
 *                         POST, and the logged-in 404 view — captured as
 *                         the canonical baseline (nonce/csrf-normalized).
 *   leg 2  FLIP ON        the identical battery now answered by the Go web
 *                         shadow via the flip table — every response must
 *                         match the Node baseline byte-for-byte after
 *                         nonce/CSRF normalization, plus the two
 *                         A/B interop legs (Node-issued login cookie read
 *                         by Go; Go-issued login cookie read by Node).
 *   leg 3  FLIP OFF       the battery re-runs on Node and matches the
 *                         baseline again (reversal is lossless).
 *
 * Afterwards the stack is left node-active (e2e convention): the flip is
 * stripped and the shadow service removed from /etc/service.
 *
 * Run: npx playwright test -g "web-go P1 auth flip gate"
 */
import { execFileSync } from 'child_process'
import path from 'path'
import { fileURLToPath } from 'url'
import { test, expect } from '@playwright/test'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')

const USER = {
  email: 'e2e-user@e2e.test',
  password: 'Ol-Fixture-3m2Q',
} as const

// ---- docker helpers (stack-scoped only) ---------------------------------

function dexe(container: string, cmd: string, allowFail = false, shell = 'bash'): string {
  try {
    return execFileSync('docker', ['exec', container, shell, '-c', cmd], {
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

// ---- normalization: what is legitimately per-request/per-stack ----------

/** nonce + csrf (meta and hidden inputs) + session cookie values differ
 * per request/session; everything else is pinned byte-exact. */
function norm(html: string): string {
  return html
    .replace(/nonce="[A-Za-z0-9+/=_-]{16,}"/g, 'nonce="N"')
    .replace(/name="ol-csrfToken" content="[^"]*"/, 'name="ol-csrfToken" content="T"')
    .replace(/value="[A-Za-z0-9+/_=-]{12,}"/g, 'value="CSRF"')
    .replace(/overleaf\.sid=s%3A[^;"]+/g, 'overleaf.sid=SID')
    .replace(/overleaf\.sid=s:[^;"]+/g, 'overleaf.sid=SID')
}

interface R {
  status: number
  ct: string
  csp: string
  location: string
  setcookie: string
  body: string
}

async function call(path: string, init: RequestInit & { cookie?: string } = {}): Promise<R> {
  const headers: Record<string, string> = { ...(init.headers as Record<string, string>) }
  if (init.cookie) headers['cookie'] = init.cookie as string
  const r = await fetch(BASE + path, { ...init, headers, redirect: 'manual' })
  const sc = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : (r.headers.get('set-cookie') || '')
  let body = ''
  const isHtml = (r.headers.get('content-type') || '').includes('text/html')
  if (isHtml) body = await r.text()
  else body = await r.text()
  return {
    status: r.status,
    ct: r.headers.get('content-type') || '',
    csp: r.headers.get('content-security-policy') || '',
    location: r.headers.get('location') || '',
    setcookie: sc,
    body,
  }
}

async function loginFlow(): Promise<{ cookie: string; status: number; body: string }> {
  const page = await call('/login')
  const tokM = page.body.match(/name="ol-csrfToken" content="([^"]+)"/)
  expect(tokM, 'login page must carry the csrf meta').toBeTruthy()
  const ck: string = (page.setcookie.match(/overleaf\.sid=([^;]+)/g) || [''])[0]
  const r = await call('/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': tokM![1], accept: 'application/json' },
    cookie: ck,
    body: JSON.stringify({ email: USER.email, password: USER.password }),
  })
  // regenerating responses carry (old, new) Set-Cookie lines — the browser
  // keeps the LAST one.
  const lines = r.setcookie.split('\n').filter((l) => l.includes('overleaf.sid'))
  return { cookie: lines[lines.length - 1] || '', status: r.status, body: r.body }
}

const FLIP_APPLY = `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/web-p0.conf /etc/nginx/overleaf-flips/web-p0.conf
if ! grep -q "overleaf-flips/web-p0.conf" "$vhost"; then
  node -e '
    const fs = require("fs");
    const v = process.argv[1];
    const inc = "  include /etc/nginx/overleaf-flips/web-p0.conf;\\n\\n";
    let s = fs.readFileSync(v, "utf8");
    if (!s.includes("overleaf-flips/web-p0.conf")) {
      const lines = s.split("\\n");
      const idx = lines.findIndex((l) => l.trim() === "location / {");
      if (idx < 0) throw new Error("location / open line not found in vhost");
      lines.splice(idx, 0, inc);
      fs.writeFileSync(v, lines.join("\\n"));
    }
  ' "$vhost"
fi
nginx -t && nginx -s reload && echo FLIP_APPLIED
`
const FLIP_STRIP = `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
if grep -q "overleaf-flips/web-p0.conf" "$vhost"; then
  sed -i "/overleaf-flips\\/web-p0.conf/d" "$vhost"
  nginx -t && nginx -s reload
fi
rm -rf /etc/nginx/overleaf-flips
echo FLIP_STRIPPED
`

// ---- the battery (runs against whatever the vhost points at) -----------

interface Baseline {
  home: R
  xhr401: R
  sysmsgs: R
  loginPage: R
  restricted: (cookie: string) => Promise<R>
  hub: (cookie: string) => Promise<R>
  logoutPage: (cookie: string) => Promise<R>
  logoutPost: (cookie: string, csrf: string) => Promise<R>
  notFound: (cookie: string) => Promise<R>
}

async function battery(): Promise<{ b: Baseline; session: { cookie: string; csrf: string } }> {
  const home = await call('/')
  const xhr401 = await call('/restricted', { headers: { accept: 'application/json' } })
  const sysmsgs = await call('/system/messages')
  const loginPage = await call('/login')
  const login = await loginFlow()
  // Node persists the login session doc asynchronously; give it a beat
  await new Promise((r) => setTimeout(r, 700))
  const csrf = (loginPage.body.match(/name="ol-csrfToken" content="([^"]+)"/) || [])[1] || ''
  const b: Baseline = {
    home,
    xhr401,
    sysmsgs,
    loginPage,
    restricted: async (cookie) => await call('/restricted', { cookie }),
    hub: async (cookie) => await call('/', { cookie }),
    logoutPage: async (cookie) => await call('/logout', { cookie }),
    logoutPost: async (cookie, cs) =>
      await call('/logout', {
        method: 'POST',
        cookie,
        headers: { 'content-type': 'application/x-www-form-urlencoded', 'x-csrf-token': cs || 'n/a' },
      }),
    notFound: async (cookie) => await call('/zzz-p1-404', { cookie }),
  }
  return { b, session: { cookie: login.cookie, csrf: csrfOf(b, login.cookie) } }
}

/** the session's OWN csrf token (from the /logout page it renders). */
async function csrfOf(b: Baseline, cookie: string): Promise<string> {
  const lg = await b.logoutPage(cookie)
  return (lg.body.match(/name="ol-csrfToken" content="([^"]+)"/) || [])[1] || ''
}

let overleafC = ''
let redisC = ''

function cmp(label: string, g: R, n: R, html = true) {
  expect(g.status, `${label}: status`).toBe(n.status)
  expect(g.ct, `${label}: content-type`).toBe(n.ct)
  expect(g.location, `${label}: location`).toBe(n.location)
  if (html) {
    expect(norm(g.body), `${label}: body (norm)`).toBe(norm(n.body))
  } else {
    expect(g.body, `${label}: body`).toBe(n.body)
  }
  // Set-Cookie: same cookie name/path/flags; the VALUE (sid+signature,
  // or its percent-encoding) is server-specific and already proven
  // interchangeable by the A/B interop legs.
  const shape = (s: string): string =>
    s
      .split('\n')
      .filter((l) => l.includes('overleaf.sid'))
      .map((l) => {
        const parts = l
          .replace(/overleaf\.sid=s%3A[^;]*/g, 'overleaf.sid=s%3AX')
          .replace(/overleaf\.sid=s:[^;]*/g, 'overleaf.sid=s:X')
          .split(';')
          .map((p) => p.split(':')[0].trim())
        return parts.join('|')
      })
      .join('¦')
  expect(shape(g.setcookie), `${label}: set-cookie shape`).toBe(shape(n.setcookie))
}

test.describe.serial('web-go P1 auth flip gate (WEB_GO_PLAN P1)', () => {
  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    redisC = runningContainer('ol-e2e-redis')
    // Re-arm the shadow runit entry (e2e cleanup removes it after the
    // P0 gate). The committed run script is docker-copied from the repo
    // — no inline heredocs. The web-go-flip service is intentionally NOT
    // started: this spec manipulates the vhost directly (P0 pattern),
    // and an env-off flip service would strip it within its 5s cycle.
    try {
      execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/runit/web-go-overleaf/run'), `${overleafC}:/tmp/webgo-run`])
      dexe(overleafC, `mkdir -p /etc/service/web-go-overleaf && cp /tmp/webgo-run /etc/service/web-go-overleaf/run && chmod 755 /etc/service/web-go-overleaf/run`)
    } catch (e) {
      // already present — fine (runsvdir keeps the live one)
      if (!String(e).includes('runsv') && !dexe(overleafC, 'test -f /etc/service/web-go-overleaf/run && echo yes', true).includes('yes')) {
        throw e
      }
    }
    // poll until the shadow answers
    const t0 = Date.now()
    for (;;) {
      const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
      if (code === '200') break
      if (Date.now() - t0 > 20_000) throw new Error(`go shadow /status never came up (last ${code})`)
      await new Promise((r) => setTimeout(r, 500))
    }
  })

  let baseline: { b: Baseline; session: { cookie: string; csrf: string }; csrf?: string; login?: { cookie: string } } | null = null

  test('leg1: Node baseline (flip OFF) — the P1 route battery', async () => {
    dexe(overleafC, FLIP_STRIP, true)
    const { b, session } = await battery()
    expect(b.home.status, 'anon / → 302').toBe(302)
    expect(b.home.location).toBe('/login')
    expect(b.xhr401.status, 'anon XHR /restricted → 401').toBe(401)
    expect(b.xhr401.status, 'anon XHR /restricted → 401').toBe(401)
    expect(b.sysmsgs.status, 'anon /system/messages → 200').toBe(200)
    expect(b.sysmsgs.body.trim(), 'anon /system/messages → []').toBe('[]')
    expect(b.loginPage.status, 'GET /login → 200').toBe(200)
    const res = await b.restricted(session.cookie)
    expect(res.status, 'logged-in /restricted → 200').toBe(200)
    const hub = await b.hub(session.cookie)
    expect(hub.status, 'logged-in / → 302').toBe(302)
    expect(hub.location, 'logged-in / → /hub').toBe('/hub')
    const lg = await b.logoutPage(session.cookie)
    expect(lg.status, 'GET /logout (logged-in) → 200').toBe(200)
    const n404 = await b.notFound(session.cookie)
    expect(n404.status, 'logged-in 404 view → 404').toBe(404)
    // store the baseline for the flip legs (describe scope)
    baseline = { b, session, csrf: await csrfOf(b, session.cookie), login: { cookie: '' } }
  })


  test('leg2: FLIP ON — identical battery on Go via nginx + A/B interop', async () => {
    const applied = dexe(overleafC, FLIP_APPLY)
    expect(applied).toContain('FLIP_APPLIED')
    await new Promise((r) => setTimeout(r, 800)) // nginx -s reload is async

    const { b, session } = await battery()
    const nb = baseline!.b
    const ns = baseline!.session
    cmp('anon /', b.home, nb.home)
    cmp('anon XHR /restricted', b.xhr401, nb.xhr401, false)
    expect(b.xhr401.body, '401 body parity').toBe(nb.xhr401.body)
    cmp('anon /system/messages', b.sysmsgs, nb.sysmsgs, false)
    expect(b.sysmsgs.body, 'sysmsgs anon body parity').toBe(nb.sysmsgs.body)
    cmp('GET /login page', b.loginPage, nb.loginPage)
    const rA = await b.restricted(session.cookie)
    const rB = await nb.restricted(ns.cookie)
    cmp('logged-in /restricted', rA, rB)
    const hA = await b.hub(session.cookie)
    const hB = await nb.hub(ns.cookie)
    cmp('logged-in / → hub', hA, hB)
    const lA = await b.logoutPage(session.cookie)
    const lB = await nb.logoutPage(ns.cookie)
    cmp('GET /logout page', lA, lB)
    const nA = await b.notFound(session.cookie)
    const nB = await nb.notFound(ns.cookie)
    cmp('404 view', nA, nB)

    // A/B interop 1: the Node-issued login cookie (baseline session) must
    // authenticate on the Go-flipped routes.
    const crossGo = await call('/restricted', { cookie: ns.cookie })
    expect(crossGo.status, 'Node cookie → Go route').toBe(200)
    // A/B interop 2: the Go-issued login cookie (the battery's loginFlow
    // session) must authenticate on the still-Node route (direct :4000)
    // and the doc must be in the shared redis store.
    const rawSid = (session.cookie.match(/overleaf\.sid=(s%3A[^;]+|s:[^;]+)/) || [])[1] || ''
    const decSid = decodeURIComponent(rawSid).replace(/^s:/, '').split('.')[0]
    const nodeSees = dexe(
      overleafC,
      `curl -s -o /dev/null -w "%{http_code}" -H "Cookie: overleaf.sid=${rawSid}" http://127.0.0.1:4000/restricted`,
      true,
    ).trim()
    expect(nodeSees, 'Go cookie → Node /restricted').toBe('200')
    const doc = dexe(redisC, `redis-cli get sess:${decSid}`, true, 'sh').trim()
    expect(doc.includes('passport'), 'Go login doc carries passport').toBeTruthy()

    // logout flow on the flipped routes (csrf from the flipped page)
    const csrfNow = await csrfOf(b, session.cookie)
    const outA = await b.logoutPost(session.cookie, csrfNow)
    expect(outA.status, 'POST /logout → 302').toBe(302)
    expect(outA.location, 'POST /logout → /login').toBe('/login')
  })

  test('leg3: FLIP OFF — Node answers identically again (reversal)', async () => {
    const stripped = dexe(overleafC, FLIP_STRIP)
    expect(stripped).toContain('FLIP_STRIPPED')
    await new Promise((r) => setTimeout(r, 800))
    const { b, session } = await battery()
    const nb = baseline!.b
    const ns = baseline!.session
    cmp('anon /', b.home, nb.home)
    cmp('anon XHR /restricted', b.xhr401, nb.xhr401, false)
    cmp('GET /login page', b.loginPage, nb.loginPage)
    const rA = await b.restricted(session.cookie)
    const rB = await nb.restricted(ns.cookie)
    cmp('logged-in /restricted', rA, rB)
    const nA = await b.notFound(session.cookie)
    const nB = await nb.notFound(ns.cookie)
    cmp('404 view', nA, nB)
  })

  test.afterAll(() => {
    if (!overleafC) return
    dexe(overleafC, FLIP_STRIP, true)
    // e2e convention: leave the stack node-active — drop the shadow entry
    dexe(overleafC, `rm -rf /etc/service/web-go-overleaf; sleep 1; true`, true)
  })
})
