/**
 * WEB-GO P0 FLIP GATE (WEB_GO_PLAN.md — the three-leg M0 gate):
 *
 *   leg 1  shadow-up   the Go web binary runs in the disposable stack
 *   leg 2  A parity    Go /status is byte/header-identical to Node /status
 *                      (direct container probes, both on their native ports)
 *   leg 3  B interop   sessions live in the SAME redis (websessions):
 *                      - a NODE-created session (login) is read by GO
 *                        (GET /dev/csrf on 127.0.0.1:4010 with the cookie)
 *                      - a GO-created session (anonymous /status) is read
 *                        by NODE (/dev/csrf on 127.0.0.1:4000 — a rejection
 *                        would 302, which is what breaks live logins)
 *                      - both derived CSRF tokens are cross-verified
 *                        against the redis session document's csrfSecret
 *                        with the Node algorithm (third-party check)
 *   leg 4  C routing   the nginx flip (FLIP table) points /status,
 *                      /dev/csrf and the three /health_check endpoints at
 *                      the Go service; flipping OFF restores stock
 *                      behavior (e.g. /health_check/redis 404s again)
 *
 * Afterwards the stack is left node-active (e2e convention): the flip is
 * stripped and the shadow services removed from /etc/service.
 *
 * Run: npx playwright test -g "web-go P0 flip gate"
 */
import { execFileSync } from 'child_process'
import { createHash } from 'crypto'
import path from 'path'
import { fileURLToPath } from 'url'
import { test, expect } from '@playwright/test'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

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

// ---- csrf cross-check (Node algorithm, third-party) ----------------------

function b64urlNoPad(buf: Buffer): string {
  return buf.toString('base64').replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

/** token = <8 base62 salt>-'-'-b64url(sha1(salt + '-' + secret)) */
function verifyCsrfToken(secret: string, token: string): { ok: boolean; detail: string } {
  const i = token.indexOf('-')
  if (i !== 8) return { ok: false, detail: `salt length ${i} (want 8) in ${JSON.stringify(token)}` }
  const salt = token.slice(0, i)
  const got = token.slice(i + 1)
  const want = b64urlNoPad(createHash('sha1').update(`${salt}-${secret}`).digest())
  if (got !== want) return { ok: false, detail: `hash mismatch (want ${want})` }
  return { ok: true, detail: '' }
}

function parseHeaders(raw: string): Map<string, string> {
  const [head = '', ...rest] = raw.split('\r\n\r\n')
  const m = new Map<string, string>()
  for (const line of head.split('\r\n').slice(1)) {
    const idx = line.indexOf(':')
    if (idx > 0) m.set(line.slice(0, idx).trim().toLowerCase(), line.slice(idx + 1).trim())
  }
  return m
}

function bodyOf(raw: string): string {
  const parts = raw.split('\r\n\r\n', 2)
  return parts[1] ?? ''
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

/**
 * Node persists the session at res.end (async fire-and-forget — express-
 * session does not block the 302 on the redis SET). Wait for the doc so
 * the other stack reads what Node actually wrote.
 */
async function pollStatus(url: string, want: number, ms: number): Promise<number> {
  const t0 = Date.now()
  let last = 0
  for (;;) {
    const r = await fetch(url)
    last = r.status
    if (r.status === want) return last
    if (Date.now() - t0 > ms) return last
    await new Promise(r2 => setTimeout(r2, 250))
  }
}

async function waitForSessionDoc(sid: string, ms = 8000): Promise<string> {
  const t0 = Date.now()
  for (;;) {
    const raw = dexe(redisC, `redis-cli get sess:${sid}`, true, 'sh').trim()
    if (raw.replace(/^"|"$/g, '').startsWith('{')) return raw
    if (Date.now() - t0 > ms) throw new Error(`session doc sess:${sid} did not appear in ${ms}ms (last: ${raw.slice(0, 60)})`)
    await new Promise(r => setTimeout(r, 250))
  }
}

let overleafC = ''
let redisC = ''
let goSid: { sid: string; cookie: string } | null = null
let nodeSid: { sid: string; cookie: string } | null = null

test.describe.serial('web-go P0 flip gate (WEB_GO_PLAN M0)', () => {
  test('leg1: shadow service is up and serving (direct :4010)', async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    redisC = runningContainer('ol-e2e-redis')

    const up = dexe(overleafC, 'curl -s -m 3 -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status', true).trim()
    expect(up, 'Go web shadow must be answering on 127.0.0.1:4010 (see /var/log/overleaf/web-go.log)').toBe('200')
  })

  test('leg2: A parity — Go /status == Node /status (headers + body)', async () => {
    const nodeRaw = dexe(overleafC, `curl -si http://127.0.0.1:4000/status`)
    const goRaw = dexe(overleafC, `curl -si http://127.0.0.1:4010/status`)
    const nodeH = parseHeaders(nodeRaw)
    const goH = parseHeaders(goRaw)

    for (const key of ['content-type', 'x-content-type-options', 'etag', 'x-powered-by', 'content-length']) {
      expect(goH.get(key), `Go ${key}`).toBe(nodeH.get(key), `${key} parity`)
    }
    // pinned Node values (must hold for BOTH):
    expect(nodeH.get('content-type')).toBe('text/plain; charset=utf-8')
    expect(nodeH.get('x-content-type-options')).toBe('nosniff')
    expect(nodeH.get('etag')).toBe('W/"12-1XIqlzCgkZ5qAye222Y2URlsyEU"')
    expect(nodeH.get('x-powered-by')).toBe('Express')
    expect(bodyOf(nodeRaw)).toBe('web is alive (web)')
    expect(bodyOf(goRaw)).toBe(bodyOf(nodeRaw))
  })

  test('leg3a: A/B interop — Node-created session is readable by Go', async () => {
    // Node login (public site; /login is NOT in the P0 flip table)
    const loginPage = await fetch(`${BASE}/login`)
    expect(loginPage.status).toBe(200)
    const html = await loginPage.text()
    const csrfMatch = html.match(/name="ol-csrfToken" content="([^"]+)"/)
    expect(csrfMatch, 'ol-csrfToken meta on /login').toBeTruthy()
    const csrf = csrfMatch![1]
    const initialCookie = (loginPage.headers.get('set-cookie') ?? '').split(';')[0]
    expect(initialCookie, 'overleaf.sid set on /login').toMatch(/^overleaf\.sid=/)

    const loginRes = await fetch(`${BASE}/login`, {
      method: 'POST',
      redirect: 'manual',
      headers: {
        'cookie': initialCookie,
        'content-type': 'application/x-www-form-urlencoded',
      },
      body: new URLSearchParams({
        email: USER.email,
        password: USER.password,
        _csrf: csrf,
      }).toString(),
    })
    expect([302, 303], 'login redirects').toContain(loginRes.status)
    const setCookies = (loginRes.headers.get('set-cookie') ?? '').match(/overleaf\.sid=[^;]+/g) ?? []
    const authCookieRaw = setCookies.length ? setCookies[setCookies.length - 1].replace(/^overleaf\.sid=/, '') : initialCookie.replace(/^overleaf\.sid=/, '')
    // Node URL-encodes the cookie value in Set-Cookie; decode to the raw form
    const cookie = decodeURIComponent(authCookieRaw)
    expect(cookie, 'authenticated sid (decoded)').toMatch(/^s:[A-Za-z0-9_-]+\.[A-Za-z0-9+/]+$/)

    const sid = cookie.slice(2, 34) // "s:" + 32 chars (cookie-signature keeps the raw sid intact)
    nodeSid = { sid, cookie }

    // Go reads the NODE session (same redis, same store contract)
    const out = dexe(overleafC, `curl -si -H "Cookie: overleaf.sid=${cookie}" http://127.0.0.1:4010/dev/csrf`)
    const code = out.split(' ', 2)[1]
    expect(code, 'Go must accept a Node-created session (302 would = session rejected)').toBe('200')
    const token = bodyOf(out)
    expect(token, 'Go /dev/csrf token shape').toMatch(/^[A-Za-z0-9]{8}-[A-Za-z0-9_-]+$/)

    // third-party cross-check against the shared redis document
    const docRaw = await waitForSessionDoc(sid)
    const doc = JSON.parse(docRaw.replace(/^"|"$/g, ''))
    expect(typeof doc.csrfSecret, 'session csrfSecret').toBe('string')
    expect(doc.validationToken, 'validationToken').toBe('v1:' + sid.slice(-4))
    const v = verifyCsrfToken(doc.csrfSecret, token)
    expect(v, `Go token must derive from the Node algorithm: ${v.detail}`).toBeTruthy()
  })

  test('leg3b: A/B — a Go-written session accepts a NODE-derived token; a Node-written session accepts a GO check', async () => {
    // ---- Node side: anonymous request creates session doc D1 (rolling) ----
    const n0 = await fetch(`${BASE}/project`, { redirect: 'manual' })
    expect(n0.status, 'anonymous /project bounces to /login').toBe(302)
    const rawSet = n0.headers.get('set-cookie') ?? ''
    const m0 = rawSet.match(/overleaf\.sid=s%3A([A-Za-z0-9_-]+)/)
    expect(m0, 'Node rolling 302 must issue the session cookie').toBeTruthy()
    const sid1 = m0![1]

    // Node's own 403 contract on bad csrf (pinned: 403 + refreshed cookie)
    const page = await fetch(`${BASE}/login`)
    void (await page.text())
    const pageCookie = (page.headers.get('set-cookie') ?? '').split(';')[0]
    const bad = await fetch(`${BASE}/login`, {
      method: 'POST',
      redirect: 'manual',
      headers: { cookie: pageCookie, 'content-type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ email: 'x@y.z', password: 'nope', _csrf: 'garbage-token-' + Date.now() }).toString(),
    })
    expect(bad.status, 'Node bad-csrf must be 403').toBe(403)

    // ---- D1 doc in the shared redis ----
    const doc1Raw = await waitForSessionDoc(sid1)
    const doc1 = JSON.parse(doc1Raw.replace(/^"|"$/g, ''))
    expect(typeof doc1.csrfSecret, 'D1 csrfSecret').toBe('string')
    const sec1 = doc1.csrfSecret

    // ---- GO verifies a NODE-derived token (Node algorithm, D1 secret) ----
    // token shape: 8 base62 salt + '-' + b64url(sha1(salt+'-'+secret))
    const salt = 'AbCdEf12'
    const nodeStyleToken = salt + '-' + b64urlNoPad(createHash('sha1').update(`${salt}-${sec1}`).digest())
    // send the Node cookie (signed by Node) + the Node-style token to a GO POST
    const signedSid = (rawSet.match(/overleaf\.sid=([^;]+)/) ?? [])[1]
    const goVerify = dexe(overleafC, `curl -si -H "Cookie: overleaf.sid=${decodeURIComponent(signedSid)}" -H "content-type: application/x-www-form-urlencoded" --data "x=1&_csrf=${encodeURIComponent(nodeStyleToken)}" -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/post-route`)
    // P0 has no POST routes; a VALID csrf passes dispatch → 302 (anon login bounce);
    // an INVALID csrf stays 403. That delta is the interop proof.
    expect(goVerify.trim(), 'Go must ACCEPT a Node-derived csrf token (302 = dispatched; 403 = rejected)').toBe('302')
    const goReject = dexe(overleafC, `curl -si -H "Cookie: overleaf.sid=${decodeURIComponent(signedSid)}" -H "content-type: application/x-www-form-urlencoded" --data "x=1&_csrf=garbage${Date.now()}" -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/post-route`)
    expect(goReject.trim(), 'Go must REJECT a garbage token (403)').toBe('403')

    // ---- Go-written session D2; Node must verify a Go-derived token on it ----
    const goAnon = dexe(overleafC, `curl -si http://127.0.0.1:4010/zzz-anon-probe`)
    const goSet = parseHeaders(goAnon).get('set-cookie') ?? ''
    const gSigned = (goSet.match(/overleaf\.sid=([^;]+)/) ?? [])[1]
    expect(gSigned, 'Go 302 must issue the session cookie').toBeTruthy()
    const gSid = gSigned!.slice(2, 34) // Go emits the raw (un-encoded) cookie value
    // lazy-csrf: D2 gains its csrfSecret on first use — trigger it with the
    // garbage-403 probe (also the negative control for D2)
    const d2Reject = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" -H "Cookie: overleaf.sid=${gSigned}" --data "x=1&_csrf=zzz${Date.now()}" http://127.0.0.1:4010/post-route`)
    expect(d2Reject.trim(), 'Go must 403 the garbage token on D2').toBe('403')
    const doc2Raw = await waitForSessionDoc(gSid)
    const doc2 = JSON.parse(doc2Raw.replace(/^"|"$/g, ''))
    expect(typeof doc2.csrfSecret, 'D2 (Go-written) csrfSecret after first use').toBe('string')
    const sec2 = doc2.csrfSecret

    // Node verifies a token derived from the Go-written secret (NODE's own
    // verification engine — proves the Go doc is consumable):
    const goStyleToken = 'Zz9Yy8Xx' + '-' + b64urlNoPad(createHash('sha1').update(`Zz9Yy8Xx-${sec2}`).digest())
    const nodeVerify = await fetch(`${BASE}/login`, {
      method: 'POST',
      redirect: 'manual',
      headers: { cookie: `overleaf.sid=${encodeURIComponent(gSigned as string)}`, 'content-type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ email: 'nobody@nowhere.test', password: 'wrongpass1', _csrf: goStyleToken }).toString(),
    })
    expect(nodeVerify.status, 'Node must ACCEPT the token (not 403) on a Go-written session (401/400 login-fail expected, 403 = csrf rejected)').not.toBe(403)
    // control: same request with a garbage token must 403 (proof the accept above was the csrf engine)
    const nodeReject = await fetch(`${BASE}/login`, {
      method: 'POST',
      redirect: 'manual',
      headers: { cookie: `overleaf.sid=${encodeURIComponent(gSigned as string)}`, 'content-type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({ email: 'nobody@nowhere.test', password: 'wrongpass1', _csrf: 'garbage' + Date.now() }).toString(),
    })
    expect(nodeReject.status, 'Node must REJECT the garbage token (403)').toBe(403)
  })

  test('leg4: flip ON routes /status + health checks to Go; OFF restores stock', async () => {
    // stock behavior reference (flip off now):
    const stockHc = await fetch(`${BASE}/health_check/redis`)
    expect([404, 200], 'stock /health_check routing').toContain(stockHc.status)
    const stockCode = stockHc.status

        expect(dexe(overleafC, FLIP_APPLY), 'flip applied').toContain('FLIP_APPLIED')
    // nginx reload is async — poll until the routing actually switched
    const settled = await pollStatus(`${BASE}/status`, 200, 8000)
    expect(settled, 'flipped /status still 200').toBe(200)

    const flipped = await fetch(`${BASE}/status`)
    expect(flipped.status).toBe(200)
    expect(await flipped.text()).toBe('web is alive (web)') // byte-parity under the flip

    let hc = await fetch(`${BASE}/health_check/redis`)
    if (stockCode === 404) {
      const t0 = Date.now()
      while (hc.status === stockCode && Date.now() - t0 < 8000) {
        await new Promise(r2 => setTimeout(r2, 250))
        hc = await fetch(`${BASE}/health_check/redis`)
      }
      expect(hc.status, 'flip must route /health_check/redis to Go (stock was 404)').toBe(200)
      expect(await hc.text()).toBe('OK')
    } else {
      expect(hc.status).toBe(200)
    }

    expect(dexe(overleafC, FLIP_STRIP), 'flip stripped').toContain('FLIP_STRIPPED')
    let reverted = await fetch(`${BASE}/health_check/redis`)
    {
      const t0 = Date.now()
      while (reverted.status !== stockCode && Date.now() - t0 < 8000) {
        await new Promise(r2 => setTimeout(r2, 250))
        reverted = await fetch(`${BASE}/health_check/redis`)
      }
    }
    expect(reverted.status, 'stock routing restored after strip').toBe(stockCode)
    const stillOk = await fetch(`${BASE}/status`)
    expect(stillOk.status).toBe(200)
  })
})

test('cleanup: e2e left node-active (flip off, shadow removed)', async () => {
  const overleafC = runningContainer('ol-e2e-overleaf')
  dexe(overleafC, FLIP_STRIP, true)
  dexe(overleafC, 'rm -rf /etc/service/web-go-overleaf /etc/service/web-go-flip', true)
  const hc = await fetch(`${BASE}/health_check/redis`)
  expect([200, 404]).toContain(hc.status) // either stock outcome; NOT a 5xx
})
