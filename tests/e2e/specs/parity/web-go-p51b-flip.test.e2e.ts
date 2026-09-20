// P5.1b: editor DETACH shell (GET /editor|Project/:id/detacher|detached)
// parity — Node baseline -> Go parity -> Node re-baseline through the 7420
// nginx surface with the web-p51b flip applied for leg 2 only.
//
// Pinned from the Node oracle (2026-09-15, /tmp/p51/owner-{detacher,
// detached}.html) + ProjectController.loadEditor:
//
//   const template = detachRole === 'detached' ? 'project/ide-react-detached'
//                                               : 'project/ide-react'
//
//   * /detacher  → ide-react chrome (SAME as the main shell), ol-detachRole
//                  = "detacher", ol-navbar.currentUrl + <link alternate> =
//                  the /detacher path.
//   * /detached  → ide-react-detached chrome: /stylesheets/ide-detached-*.
//                  css, <div id="pdf-preview-detached-root">, 26 <script>
//                  (no socket.io, no defer), /js/ide-detached-*.js, ol-
//                  detachRole = "detached".
//
// Node reuses the SAME loadEditor locals for all six editor routes; the Go
// shadow (go/services/web/features/editorpages → views.EditorPage with
// EditorData.Detached) renders both chromes. Go + Node share Mongo + Redis,
// so ONE session drives both legs; the gate normalizes ONLY the per-render
// CSP nonce + the per-request csrf token (Node regenerates csrf each
// request) + hex24 ids / timestamps.

import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const redisC = 'ol-e2e-redis-1'
const FLIPCONF = 'web-p51b.conf'
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const FIX_NAME = 'P51b Editor Fixture'

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function dexe(c: string, cmd: string, capture = false): string {
  const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore' })
  return out || ''
}
function dexeQ(c: string, cmd: string): string {
  return (execFileSync('docker', ['exec', c, 'mongosh', '--quiet', 'sharelatex', '--eval', cmd], { encoding: 'utf8' }) || '').trim()
}

// P5.1b — the Node editor routes carry `openProjectRateLimiter` (a per-
// project 15pts/60s counter, key `rate-limit:open-project:<pid>:<uid>`).
// This gate opens the SAME fixture project for every route and every leg, so
// it necessarily consumes that counter and Node 429s (leg 3). The e2e stack
// is configured with rate limits OFF (OVERLEAF_DISABLE_RATE_LIMITS), and the
// Go shadow deliberately registers no limiter for the editor page (parity
// decision, P5.1a). To keep the gate a pure HTML-parity test, flush the
// open-project counter before each battery so Node serves 200. This does NOT
// mask a parity gap: it equalises the limiter state across the legs so the
// comparison is the page bytes, not the rate-limit counter.
function flushOpenProjectRateLimits(): void {
  for (let db = 0; db <= 7; db++) {
    try {
      execFileSync(
        'docker',
        ['exec', redisC, 'sh', '-c', `redis-cli -n ${db} --scan --pattern 'rate-limit:open-project*' 2>/dev/null | while read -r k; do [ -n "$k" ] && redis-cli -n ${db} del "$k" >/dev/null 2>&1; done; true`],
        { stdio: 'ignore', encoding: 'utf8' },
      )
    } catch {}
  }
}

async function waitGo(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
    if (code === '200') return
    await sleep(500)
  }
  throw new Error('Go shadow /status not 200')
}

function flip(mode: 'apply' | 'strip'): void {
  const vhost = '/etc/nginx/sites-enabled/overleaf.conf'
  if (mode === 'apply') {
    dexe(overleafC, `
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/${FLIPCONF} /etc/nginx/overleaf-flips/${FLIPCONF}
node -e "const fs=require('fs');const p='${vhost}';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"
if ! grep -q "overleaf-flips/${FLIPCONF}" ${vhost}; then
  python3 - ${vhost} <<'PYEOF'
import sys
p = sys.argv[1]
s = open(p).read()
inc = "  include /etc/nginx/overleaf-flips/${FLIPCONF};\\n"
if "location / {" in s:
    s = s.replace("location / {", inc + "location / {", 1)
else:
    raise SystemExit("anchor 'location / {' not found")
open(p, 'w').write(s)
PYEOF
fi
nginx -t 2>&1 | tail -1 && nginx -s reload
`)
  } else {
    dexe(overleafC, `sed -i "/overleaf-flips\\/${FLIPCONF}/d" ${vhost} && nginx -t 2>&1 | tail -1 && nginx -s reload`)
  }
  void sleep(2000)
}

// ---------- http ---------------------------------------------------------------
type R = { status: number; ct: string; body: string; setcookie: string }
async function call(p: string, init: { method?: string; headers?: Record<string, string>; cookie?: string; body?: any } = {}): Promise<R> {
  const h: Record<string, string> = { ...(init.headers || {}) }
  if (init.cookie) h['cookie'] = init.cookie
  let lastErr: unknown = null
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      const r = await fetch(BASE + p, { method: init.method || 'GET', headers: h, body: init.body as any, redirect: 'manual' })
      const body = Buffer.from(await r.arrayBuffer()).toString('utf8')
      const setcookie = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : (r.headers.get('set-cookie') || '').toString()
      return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], body, setcookie }
    } catch (e) {
      lastErr = e
      await new Promise((res) => setTimeout(res, 400 * (attempt + 1)))
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
  const sid = (logged.setcookie.split('\n').find((l) => l.includes('overleaf.sid')) || '').replace(/^overleaf\.sid=/, '').split(';')[0]
  const ck = sid ? 'overleaf.sid=' + sid : ck0
  // CSRF rotates on use — grab a FRESH token for the next mutation (the
  // proven P4/P5.1a pattern via /dev/csrf, which returns res.locals.csrfToken).
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body.trim() || csrf0 }
}

const norm = (s: string): string => s
  .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
  .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
  .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
  .replace(/name="_csrf"\s+value="[^"]*"/g, 'name="_csrf" value="CSRF"')
  .replace(/\b[0-9a-f]{24}\b/g, '<HEX24>')
  .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z/g, '<TS>')

async function ensureFixtureProject(U: { ck: string; csrf: string }): Promise<string> {
  dexeQ(mongoC, `db.projects.deleteMany({ name: /^P51b Editor Fixture/ })`)
  const r = await call('/project/new', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': U.csrf, accept: 'application/json' },
    cookie: U.ck,
    body: JSON.stringify({ projectName: FIX_NAME }),
  })
  if (r.status !== 200) throw new Error('fixture project create failed ' + r.status + ' ' + r.body.slice(0, 160))
  const m = r.body.match(/"project_id":"([0-9a-f]{24})"/i)
  if (!m) throw new Error('fixture project pid not in response: ' + r.body.slice(0, 160))
  return m[1]
}

// The 4 detach routes (the P5.1b scope).
const ROUTES = ['/editor/<PID>/detacher', '/editor/<PID>/detached', '/Project/<PID>/detacher', '/Project/<PID>/detached'] as const
type Key = (typeof ROUTES)[number]
type Leg = Record<string, R>

async function battery(pid: string, U: { ck: string; csrf: string }): Promise<Leg> {
  flushOpenProjectRateLimits()
  const leg: Leg = {}
  for (const route of ROUTES) {
    leg[route] = await call(route.replace('<PID>', pid), { headers: { cookie: U.ck, accept: 'text/html' } })
  }
  return leg
}

function diffLegs(label: string, A: Leg, B: Leg): string[] {
  const ds: string[] = []
  for (const k of ROUTES) {
    const a = A[k], b = B[k]
    if (a.status !== b.status) { ds.push(`${label}:${k} status A=${a.status} B=${b.status}`); continue }
    if (a.ct !== b.ct) { ds.push(`${label}:${k} ct A=${a.ct} B=${b.ct}`); continue }
    const na = norm(a.body), nb = norm(b.body)
    if (na !== nb) {
      let i = 0
      while (i < na.length && i < nb.length && na[i] === nb[i]) i++
      ds.push(`${label}:${k} body len A=${na.length} B=${nb.length} firstDiff@${i}\nA: …${na.slice(Math.max(0, i - 80), i + 120)}\nB: …${nb.slice(Math.max(0, i - 80), i + 120)}`)
    }
  }
  return ds
}

let LEG1: Leg | null = null

test.describe('@local web-go P5.1b (editor detach shell) parity', () => {
  let U: { ck: string; csrf: string }
  let PID: string

  test.beforeAll(async () => {
    test.setTimeout(600_000)
    const here = path.dirname(fileURLToPath(import.meta.url))
    const cp = path.resolve(here, '..', '..', '..', '..', 'server-ce', 'nginx', 'flips', FLIPCONF)
    execFileSync('docker', ['cp', cp, `${overleafC}:/tmp/${FLIPCONF}`])
    dexe(overleafC, `mkdir -p /usr/local/share/overleaf-flips && cp -f /tmp/${FLIPCONF} /usr/local/share/overleaf-flips/${FLIPCONF}`)
    flip('strip')
    await waitGo()
    U = await login(USER)
    PID = await ensureFixtureProject(U)
    flushOpenProjectRateLimits()
    // Pin the Node oracle BEFORE any flip: all four detach routes must 200.
    for (const route of ROUTES) {
      const p = route.replace('<PID>', PID)
      const probe = await call(p, { headers: { cookie: U.ck, accept: 'text/html' } })
      if (probe.status !== 200) throw new Error(`Node oracle ${p} not 200: ${probe.status} ${probe.body.slice(0, 120)}`)
      if (!probe.ct.includes('text/html')) throw new Error(`Node oracle ${p} CT not html: ${probe.ct}`)
      if (!probe.body.includes('name="ol-project_id"')) throw new Error(`Node oracle ${p} missing ol-project_id meta`)
    }
  }, 600_000)

  test.afterAll(async () => {
    try {
      flip('strip')
      await waitGo()
    } catch {}
  })

  test('leg 1: Node baseline', async () => {
    test.setTimeout(300_000)
    flip('strip')
    const leg1 = await battery(PID, U)
    LEG1 = leg1
    for (const route of ROUTES) {
      const c = leg1[route]
      expect(c.status).toBe(200)
      expect(c.ct).toBe('text/html')
      expect(c.body).toContain('<!DOCTYPE html')
      expect(c.body).toMatch(/<meta name="ol-csrfToken" content="[A-Za-z0-9_-]+"/)
      const isDetached = route.endsWith('/detached')
      const isDetacher = route.endsWith('/detacher')
      // ol-detachRole: detached/detacher shells carry their value; both pins.
      expect(c.body).toMatch(/<meta name="ol-detachRole" data-type="string" content="(detached|detacher)"/)
      if (isDetached) {
        expect(c.body).toContain('content="detached"')
        expect(c.body).toContain('/stylesheets/ide-detached-')
        expect(c.body).toContain('id="pdf-preview-detached-root"')
        expect(c.body).toContain('/js/ide-detached-')
        expect(c.body).not.toContain('/stylesheets/pages/ide-')
      } else if (isDetacher) {
        expect(c.body).toContain('content="detacher"')
        expect(c.body).toContain('/stylesheets/pages/ide-')
        expect(c.body).not.toContain('/stylesheets/ide-detached-')
        expect(c.body).not.toContain('id="pdf-preview-detached-root"')
      }
      // currentUrl + alternate track the requested route (Node HTML-escapes
      // the JSON inside the ol-navbar meta content → &quot; entities; the value
      // is the absolute path, e.g. /editor/<PID>/detacher).
      const rel = route.replace('<PID>', PID) // /editor/PID/detacher
      expect(c.body).toContain('currentUrl&quot;:&quot;' + rel + '&quot;')
      expect(c.body).toContain('<link rel="alternate" href="http://127.0.0.1:7420' + rel + '"')
    }
  })

  test('leg 2: Go parity', async () => {
    test.setTimeout(300_000)
    flip('apply')
    const leg2 = await battery(PID, U)
    flip('strip')
    const ds = diffLegs('p51b', LEG1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('leg 3: Node re-baseline', async () => {
    test.setTimeout(300_000)
    flip('strip')
    const leg3 = await battery(PID, U)
    const d1 = diffLegs('p51b-leg3', LEG1, leg3)
    if (d1.length) console.log(d1.join('\n'))
    expect(d1, d1.join('\n')).toHaveLength(0)
  })
})
