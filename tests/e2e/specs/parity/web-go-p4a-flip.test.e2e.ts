/**
 * WEB-GO P4.1 FLIP GATE (WEB_GO_PLAN.md P4.1 — project list):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p4a.conf):
 *     GET /user/projects  → requireLogin → { projects: [ {_id, name, accessLevel} ] }
 *
 *   leg 1  Node baseline — the battery via nginx (flip OFF)
 *   leg 2  FLIP ON       — identical battery on Go, byte-for-byte match
 *   leg 3  FLIP OFF      — Node again (reversal)
 *
 * Contract pins (lve-oracle 2026-09-14; A/B byte-identical):
 *   - GET /user/projects (logged-in) → 200 application/json, body
 *     `{ "projects": [ {"_id","name","accessLevel"}, … ] }` in EXACT bucket
 *     order (owned / readWrite / review / readOnly / token-readAndWrite /
 *     token-readOnly, token buckets de-duplicated), NO sorting, and with
 *     `archived[]`/`trashed[]` projects Omitted.
 *   - accessLevel strings: owner / readWrite / review / readOnly / readAndWrite.
 *   - anon GET accept json  → 401
 *   - anon GET accept html  → 302 Location /login
 *
 * State discipline (mongo ol-e2e-mongo-*, redis ol-e2e-redis-*):
 *   - read-only route: no mutation; the auth session is shared (Node/Go read
 *     the same redis session), so each leg re-authenticates its own session.
 *   - the login rate-limit window (10/120s per e-mail) is per-process and this
 *     gate stays well under it (2 logins/leg × 3 legs).
 *
 * Afterwards the stack is left node-active (e2e convention).
 *
 * Run: npx playwright test -g "web-go P4.1 flip gate"
 */
import { execFileSync } from 'child_process'
import { test, expect } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

// ---- containers ----------------------------------------------------------
function dexe(container: string, cmd: string, allowFail = false): string {
  try {
    return execFileSync('docker', ['exec', container, 'sh', '-c', cmd], {
      encoding: 'utf8', maxBuffer: 64 * 1024 * 1024, timeout: 150_000,
    })
  } catch (e: unknown) { if (allowFail) return ''; throw e }
}
function runningContainer(match: string): string {
  const out = execFileSync('docker', ['ps', '--filter', `name=${match}`, '--format', '{{.Names}}'], { encoding: 'utf8' })
  const names = out.split('\n').filter(Boolean)
  if (!names.length) throw new Error(`no running container matching ${match}`)
  return names[0]
}

// ---- flip plumbing (web-p4a.conf) ----------------------------------------
function FLIP(cmd: 'apply' | 'strip'): string {
  const conf = 'web-p4a.conf'
  if (cmd === 'apply') {
    return `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/${conf} /etc/nginx/overleaf-flips/${conf}
if ! grep -q "overleaf-flips/${conf}" "$vhost"; then
  node -e '
    const fs = require("fs");
    const v = process.argv[1];
    const inc = "  include /etc/nginx/overleaf-flips/${conf};\\n\\n";
    let s = fs.readFileSync(v, "utf8");
    const lines = s.split("\\n");
    const idx = lines.findIndex((l) => l.trim() === "location / {");
    if (idx < 0) throw new Error("location / open line not found in vhost");
    lines.splice(idx, 0, inc);
    fs.writeFileSync(v, lines.join("\\n"));
  ' "$vhost"
fi
nginx -t && nginx -s reload && sleep 2
`
  }
  return `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
if grep -q "overleaf-flips/${conf}" "$vhost"; then
  sed -i "/overleaf-flips\\\/${conf}/d" "$vhost"
  nginx -t && nginx -s reload && sleep 2
fi
`
}

// ---- http helpers --------------------------------------------------------
const HDRS = ['content-type', 'location', 'etag', 'content-length', 'x-powered-by', 'www-authenticate'] as const
interface R { status: number; h: Record<string, string>; body: string; setcookie: string }

async function nginxSettled(timeoutMs = 15_000): Promise<void> {
  const t0 = Date.now()
  for (;;) {
    try {
      const r = await fetch(BASE + '/status', { redirect: 'manual' })
      if (r.status >= 100) { await r.text().catch(() => {}); return }
    } catch { /* retry */ }
    if (Date.now() - t0 > timeoutMs) throw new Error('nginx never settled after reload')
    await sleep(300)
  }
}

async function call(p: string, init: RequestInit & { cookie?: string } = {}, attempt = 0): Promise<R> {
  try {
    const initH = (init.headers || {}) as Record<string, string>
    const headers: Record<string, string> = { ...initH, accept: initH.accept || 'application/json' }
    if (init.cookie) headers['cookie'] = init.cookie as string
    const r = await fetch(BASE + p, { ...init, headers, redirect: 'manual' })
    const h: Record<string, string> = {}
    for (const key of HDRS) h[key] = r.headers.get(key) || ''
    const sc = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : (r.headers.get('set-cookie') || '')
    const body = Buffer.from(await r.arrayBuffer()).toString('binary')
    return { status: r.status, h, body, setcookie: sc }
  } catch (e) {
    if (attempt < 3 && /socket|ECONNRESET|other side closed|fetch failed/i.test(String(e))) {
      await sleep(400 * (attempt + 1)); return call(p, init, attempt + 1)
    }
    throw e
  }
}

async function login(who: { email: string; password: string }): Promise<string> {
  const page = await call('/login')
  const csrf = (page.body.match(/name=["']ol-csrfToken["']\s+content=["']([^"']+)"/) || [])[1] || ''
  let ck = (page.setcookie.match(/overleaf\.sid=[^;\n]+/) || [])[0] || ''
  const r = await call('/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' },
    cookie: ck,
    body: JSON.stringify(who),
  })
  expect(r.status, `login ${who.email}`).toBe(200)
  const lines = r.setcookie.split('\n').filter((l) => l.includes('overleaf.sid'))
  return lines[lines.length - 1] || ck
}

// ---- battery -------------------------------------------------------------
interface Leg {
  admin: R
  user: R
  anonJson: R
  anonHtml: R
}
async function battery(): Promise<Leg> {
  const admin = await login(ADMIN)
  const user = await login(USER)
  return {
    admin: await call('/user/projects', { cookie: admin, headers: { accept: 'application/json' } }),
    user: await call('/user/projects', { cookie: user, headers: { accept: 'application/json' } }),
    anonJson: await call('/user/projects', { headers: { accept: 'application/json' } }),
    anonHtml: await call('/user/projects', { headers: { accept: 'text/html' } }),
  }
}

function diffLegs(a: Leg, b: Leg, tag: string): string[] {
  const problems: string[] = []
  for (const key of ['admin', 'user'] as const) {
    const x = a[key], y = b[key]
    if (x.status !== y.status) problems.push(`${tag} ${key}: status ${x.status} vs ${y.status}`)
    if ((x.h.etag || '') !== (y.h.etag || '')) problems.push(`${tag} ${key}: etag ${x.h.etag} vs ${y.h.etag}`)
    if (x.body !== y.body) problems.push(`${tag} ${key}: body ${x.body.slice(0, 200)} vs ${y.body.slice(0, 200)}`)
  }
  for (const key of ['anonJson', 'anonHtml'] as const) {
    const x = a[key], y = b[key]
    if (x.status !== y.status) problems.push(`${tag} ${key}: status ${x.status} vs ${y.status}`)
    if ((x.h.location || '') !== (y.h.location || '')) problems.push(`${tag} ${key}: location ${x.h.location} vs ${y.h.location}`)
  }
  return problems
}

// ---- the gate                                                              ----
test.describe.serial('web-go P4.1 flip gate (WEB_GO_PLAN P4.1 project list)', () => {
  let overleafC = ''
  let leg1: Leg | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p4a.conf'), `${overleafC}:/tmp/web-p4a.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p4a.conf /usr/local/share/overleaf-flips/web-p4a.conf')
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) {
      const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
      if (code === '200') break
      await sleep(500)
    }
    dexe(overleafC, FLIP('strip'), true)
    await nginxSettled()
  })

  test.afterAll(async () => {
    try { dexe(overleafC, FLIP('strip'), true); await nginxSettled() } catch { /* best effort */ }
  })

  test('leg 1: Node baseline battery', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('strip'), true)
    await nginxSettled()
    leg1 = await battery()
    // pins
    expect(leg1.admin.status).toBe(200)
    expect(leg1.user.status).toBe(200)
    const jA = JSON.parse(leg1.admin.body)
    const jU = JSON.parse(leg1.user.body)
    expect(Array.isArray(jA.projects), 'admin projects array').toBe(true)
    expect(jA.projects.length, 'admin has projects').toBeGreaterThan(0)
    expect(jU.projects.length, 'user has projects').toBeGreaterThan(0)
    for (const p of jA.projects) {
      expect(Object.keys(p)).toEqual(['_id', 'name', 'accessLevel'])
      expect(['owner', 'readWrite', 'review', 'readOnly', 'readAndWrite']).toContain(p.accessLevel)
    }
    expect(leg1.anonJson.status).toBe(401)
    expect(leg1.anonHtml.status).toBe(302)
    expect(leg1.anonHtml.h.location).toBe('/login')
  }, 180_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('apply'))
    await nginxSettled()
    const leg2 = await battery()
    const problems = diffLegs(leg1!, leg2, 'GO')
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 180_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('strip'))
    await nginxSettled()
    const leg3 = await battery()
    const problems = diffLegs(leg1!, leg3, 'NODE-rev')
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 180_000)
})
