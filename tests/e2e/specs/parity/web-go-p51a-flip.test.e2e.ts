// P5.1a: editor page (GET /editor/:id + legacy GET /Project/:id) parity —
// Node baseline -> Go parity -> Node re-baseline through the 7420 nginx
// surface with the web-p51a flip applied for leg 2 only.
//
// The editor page is ~35 HTML + ~68 ol-* <meta> bootstrap slots (the React
// app boots entirely from these). Byte parity of the page IS the editor
// bootstrap contract: a wrong/missing meta (ol-user, ol-ExposedSettings,
// ol-splitTestVariants, ol-capabilities, ol-grammarSettings, ol-navbar,
// ol-project_id, ol-otMigrationStage, …) breaks the client before the
// first byte of collaboration ever lands.
//
// Node pipeline (router.mjs ~590-640, pinned oracle 2026-09-15):
//   openProjectRateLimiter (NO-OP: e2e sets OVERLEAF_DISABLE_RATE_LIMITS)
//   -> useCapabilities -> ensureUserCanReadProject (requireLogin: anon 302
//      /login; no-read 401) -> ProjectController.loadEditor -> res.render(
//      'project/ide-react', locals)
//
// Go shadow: go/services/web/features/editorpages (P5.1a) -> views.EditorPage.
// Go and Node share Mongo (users/projects) + Redis (session): ONE session
// drives both legs, so ol-csrfToken is identical; the gate normalizes ONLY
// the per-request CSP nonce (renderer-generated) + per-pixel hex24 ids.
//
// Pinned Node oracle (owner, fresh session):
//   200 text/html ~35.3 kB, one nonce, 68 ol-* metas
//   ol-project_id=ol-user_id-derived, ol-projectName, ol-navbar.currentUrl
//   == the requested path, ol-otMigrationStage=0, ol-learnedWords=[],
//   ol-projectTags=[], ol-inactiveTutorials=[], ol-splitTestInfo={},
//   ol-ab={}, ol-grammarSettings={llmAdminEnabled:true,llmServerConfigured:
//   false,llmAvailableForUser:false,ltAvailable:true}

import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const FLIPCONF = 'web-p51a.conf'
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const FIX_NAME = 'P51a Editor Fixture'

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function dexe(c: string, cmd: string, capture = false): string {
  const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore' })
  return out || ''
}
function dexeQ(c: string, cmd: string): string {
  return (execFileSync('docker', ['exec', c, 'mongosh', '--quiet', 'sharelatex', '--eval', cmd], { encoding: 'utf8' }) || '').trim()
}

async function waitGo(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    // 2026-10-05: the single-Go P7 stack has no :4010 shadow — probe :4000 (the live Go web).
    const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4000/login`, true).trim()
    if (code === '200' || code === '302') return
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
  // proven P4.13b pattern via /dev/csrf, which returns res.locals.csrfToken).
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body.trim() || csrf0 }
}

// Normalize the only per-request values that BOTH legs legitimately differ
// on:
//   - the CSP nonce (unique per render — appears as nonce-… in the CSP
//     header/script-src AND as nonce="…" on <script>/<link> elements);
//   - ol-csrfToken (Node regenerates the csrf token per REQUEST — pinned
//     2026-09-15: two consecutive Node editor GETs on one session return
//     different tokens — so it cannot be compared across the two legs);
//   - hex24 ids + timestamps (defensive, though same project/session).
const norm = (s: string): string => s
  .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
  .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
  .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
  .replace(/name="_csrf"\s+value="[^"]*"/g, 'name="_csrf" value="CSRF"')
  .replace(/\b[0-9a-f]{24}\b/g, '<HEX24>')
  .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z/g, '<TS>')

// Create the fixture project idempotently; return its pid + name.
async function ensureFixtureProject(U: { ck: string; csrf: string }): Promise<string> {
  dexeQ(mongoC, `db.projects.deleteMany({ name: /^P51a Editor Fixture/ })`)
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

type Leg = { editor: R; legacy: R }

async function battery(pid: string, U: { ck: string; csrf: string }): Promise<Leg> {
  const h = { cookie: U.ck, accept: 'text/html' }
  const editor = await call('/editor/' + pid, { headers: h })
  const legacy = await call('/Project/' + pid, { headers: h })
  return { editor, legacy }
}

function diffLegs(label: string, a: Leg, b: Leg): string[] {
  const ds: string[] = []
  for (const k of ['editor', 'legacy'] as const) {
    const A = a[k], B = b[k]
    if (A.status !== B.status) { ds.push(`${label}:${k} status A=${A.status} B=${B.status}`); continue }
    if (A.ct !== B.ct) { ds.push(`${label}:${k} ct A=${A.ct} B=${B.ct}`); continue }
    const na = norm(A.body), nb = norm(B.body)
    if (na !== nb) {
      // find first divergence to make the failure actionable
      let i = 0
      while (i < na.length && i < nb.length && na[i] === nb[i]) i++
      ds.push(`${label}:${k} body len A=${na.length} B=${nb.length} firstDiff@${i}\nA: …${na.slice(Math.max(0, i - 80), i + 120)}\nB: …${nb.slice(Math.max(0, i - 80), i + 120)}`)
    }
  }
  return ds
}

let LEG1: Leg | null = null

test.describe('@local web-go P5.1a (editor page) parity', () => {
  let U: { ck: string; csrf: string }
  let PID: string

  test.beforeAll(async () => {
    test.setTimeout(600_000)
    const here = path.dirname(fileURLToPath(import.meta.url))
    const cp = path.resolve(here, '..', '..', '..', '..', 'images', 'main-amd64', 'nginx', 'flips', FLIPCONF)
    execFileSync('docker', ['cp', cp, `${overleafC}:/tmp/${FLIPCONF}`])
    dexe(overleafC, `mkdir -p /usr/local/share/overleaf-flips && cp -f /tmp/${FLIPCONF} /usr/local/share/overleaf-flips/${FLIPCONF}`)
    // 2026-10-05 (route-retirement wave) single-stack rework: the P7 e2e stack
    // serves the Go web service directly, so the gate runs against the live
    // stack (the flip confs are a no-op legacy from the dual-stack era). Leave
    // the nginx upstream on the live Go service.
    flip('apply')
    await waitGo()
    U = await login(USER)
    PID = await ensureFixtureProject(U)
    // Pin the Go oracle: a 200 text/html owner page with the ol-* bootstrap metas.
    const probe = await call('/editor/' + PID, { headers: { cookie: U.ck, accept: 'text/html' } })
    if (probe.status !== 200) throw new Error('Go /editor/:id not 200: ' + probe.status)
    if (!probe.ct.includes('text/html')) throw new Error('Go CT not html: ' + probe.ct)
    if (!probe.body.includes('name="ol-project_id"')) throw new Error('Go missing ol-project_id meta')
  }, 600_000)

  test.afterAll(async () => {
    try {
      // 2026-10-05: leave the single-Go stack on its live upstream.
      flip('apply')
      await waitGo()
    } catch {}
  })

  test.skip('leg 1: Node baseline', async () => {
    test.setTimeout(300_000)
    flip('strip')
    const leg1 = await battery(PID, U)
    LEG1 = leg1
    // Node oracle pins (the editor bootstrap contract)
    for (const k of ['editor', 'legacy'] as const) {
      const c = leg1[k]
      expect(c.status).toBe(200)
      expect(c.ct).toBe('text/html')
      expect(c.body).toContain('<!DOCTYPE html')
      expect(c.body).toMatch(/<meta name="ol-csrfToken" content="[A-Za-z0-9_-]+"/)
      expect(c.body).toMatch(/<meta name="ol-user" data-type="json" content="\{[^"]*\}"/)
      expect(c.body).toMatch(/<meta name="ol-project_id" content="[0-9a-f]{24}"/)
      expect(c.body).toContain('name="ol-ExposedSettings" data-type="json"')
      expect(c.body).toContain('name="ol-splitTestVariants" data-type="json"')
      expect(c.body).toContain('name="ol-navbar" data-type="json"')
      expect(c.body).toContain('name="ol-capabilities" data-type="json"')
      expect(c.body).toContain('name="ol-grammarSettings" data-type="json"')
    }
    // route-specific bootstrap
    expect(leg1.editor.body).toContain(`currentUrl`); expect(leg1.editor.body).toContain(`/editor/`)
    expect(leg1.legacy.body).toContain(`/Project/`)
  })

  test('leg 2: Go (single-stack; /Project retired 2026-10-05)', async () => {
    test.setTimeout(300_000)
    flip('apply')
    try {
      const leg2 = await battery(PID, U)
      // /editor/:id — the live editor page (owner route decision: /editor stays)
      expect(leg2.editor.status, 'editor status').toBe(200)
      expect(leg2.editor.ct, 'editor CT').toBe('text/html')
      expect(leg2.editor.body).toContain('<!DOCTYPE html')
      expect(leg2.editor.body).toMatch(/<meta name="ol-csrfToken" content="[A-Za-z0-9_-]+"/)
      expect(leg2.editor.body).toMatch(/<meta name="ol-project_id" content="[0-9a-f]{24}"/)
      expect(leg2.editor.body).toContain('name="ol-navbar" data-type="json"')
      expect(leg2.editor.body).toContain('name="ol-capabilities" data-type="json"')
      // /Project/:id — RETIRED by the 2026-10-05 route-retirement wave (owner
      // decision: /Project/:id editor page went, /editor/:id stays)
      expect(leg2.legacy.status, 'legacy /Project/:id retired \u2192 404').toBe(404)
    } finally {
      // leave the stack on the live Go upstream (P7 single-Go baseline)
      flip('apply')
    }
  })

  test.skip('leg 3: Node re-baseline', async () => {
    test.setTimeout(300_000)
    flip('strip')
    const leg3 = await battery(PID, U)
    const d1 = diffLegs('p51a-leg3', LEG1, leg3)
    if (d1.length) console.log(d1.join('\n'))
    expect(d1, d1.join('\n')).toHaveLength(0)
  })
})
