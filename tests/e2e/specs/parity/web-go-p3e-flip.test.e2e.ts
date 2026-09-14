/**
 * WEB-GO P3.6 FLIP GATE (WEB_GO_PLAN.md P3.6 — Manage/Site SiteSettings leaf):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p3e.conf):
 *     GET    /admin/site                              → 302 /hub#/site
 *     GET    /admin/site-settings                     → all sections (JSON)
 *     PUT    /admin/site-settings/:section            → upsert a section
 *     POST   /admin/site-settings/email/test          → test e-mail
 *     GET    /admin/site/template-admins              → template-admin list
 *
 *   leg 1  Node baseline — the full battery via nginx (flip OFF)
 *   leg 2  FLIP ON       — identical battery on Go, byte-for-byte match
 *   leg 3  FLIP OFF      — Node again (reversal)
 *
 * Contract pins (live 2026-09-14, Node oracle + A/B battery 13/13 PASS +
 * cipher round-trip ALL PASS):
 *   - GET /admin/site (admin)                 → 302 Location /hub#/site
 *   - GET /admin/site-settings (admin)        → 200 application/json, fixed
 *                                               23-section order, secrets
 *                                               masked, templates.counts added
 *   - PUT signup (valid)                      → 200 {ok,upserted,modified}
 *   - PUT signup (enabled:"no")               → 422 {message:"enabled must be a boolean"}
 *   - PUT notreal                             → 422 {message:"Unknown section: notreal"}
 *   - PUT storage (fs)                        → 200 {ok,upserted,modified,appliesOn,
 *                                               envLines:[...]}  (envLines is a JSON ARRAY)
 *   - POST email/test (invalid)               → 422 {message:"Enter a valid e-mail address"}
 *   - POST email/test (valid)                 → 200 {ok:true} + 1 mail, subject
 *                                               "[Overleaf] E-mail configuration test"
 *   - GET template-admins (admin)             → 200 {users:[{id,email,firstName,
 *                                               lastName,isAdmin,hasTemplateFlag}]}
 *   - anon GET  accept json                   → 401 (requireGlobalLogin)
 *   - anon GET  accept html                   → 302 /login
 *   - non-admin GET                           → 302 /restricted?from=…
 *
 * Normalization (Node volatile fields):
 *   - templates.counts key order: Node assigns counts from Promise.all
 *     completions → order is NOT guaranteed; compare counts as a sorted-key
 *     object (all other JSON is order-sensitive byte parity).
 *   - storage.envManaged / storage.envPath: Node's managed-env detection is
 *     known to diverge from repo source (instrumented 2026-09-14); both are
 *     dropped from the comparison.
 *
 * State discipline (mongo ol-e2e-mongo-*, redis ol-e2e-redis-*):
 *   - the battery reads the live signup/storage sections and restores them;
 *     the storage PUT writes the managed env fragment (shared FS) — the gate
 *     ends with storage backend = fs (the e2e default) and signup restored;
 *   - the smtp sink is flushed per leg; each leg's valid email/test mail is
 *     attributed to a leg-unique recipient (asserted exactly-one).
 *   - the email/test in-memory 5/min window is per-process; the gate keeps
 *     well under it (≤1 valid per leg).
 *
 * Afterwards the stack is left node-active (e2e convention).
 *
 * Run: npx playwright test -g "web-go P3.6 flip gate"
 */
import { execFileSync } from 'child_process'
import { test, expect } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const SINK = process.env.E2E_SMTP_SINK || 'http://127.0.0.1:18025'

const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const GUEST = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }

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

// ---- normalization -------------------------------------------------------
function canon(o: unknown): unknown {
  if (Array.isArray(o)) return o.map(canon)
  if (o && typeof o === 'object') {
    const out: Record<string, unknown> = {}
    for (const k of Object.keys(o as Record<string, unknown>)) {
      if (k === 'envManaged' || k === 'envPath') continue
      const v = (o as Record<string, unknown>)[k]
      if (k === 'counts' && v && typeof v === 'object' && !Array.isArray(v)) {
        const so: Record<string, unknown> = {}
        for (const k2 of Object.keys(v as Record<string, unknown>).sort())
          so[k2] = canon((v as Record<string, unknown>)[k2])
        out[k] = so
      } else out[k] = canon(v)
    }
    return out
  }
  return o
}
function canonForCompare(body: string): string {
  const t = body.trim()
  if ((t[0] === '{' || t[0] === '[')) {
    try { return JSON.stringify(canon(JSON.parse(body))) } catch { return body }
  }
  return body
}
const HDRS = [
  'content-type', 'location', 'vary', 'content-length', 'www-authenticate',
  'x-powered-by', 'x-content-type-options',
] as const
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
    const headers: Record<string, string> = { ...(init.headers as Record<string, string>) }
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

// ---- flip plumbing (web-p3e.conf) ----------------------------------------
function FLIP(cmd: 'apply' | 'strip'): string {
  const conf = 'web-p3e.conf'
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
    if (!s.includes("overleaf-flips/${conf}")) {
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

// ---- sessions -------------------------------------------------------------
interface Sess { cookie: string; csrf: string }
async function anonSession(): Promise<Sess> {
  const page = await call('/login')
  const tok = (page.body.match(/name="ol-csrfToken" content="([^"]+)"/) || [])[1] || ''
  const ck = (page.setcookie.match(/overleaf\.sid=[^;\n]+/) || [])[0]
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
  const cookie = lines[lines.length - 1] || a.cookie
  const csrf = (await call('/dev/csrf', { cookie })).body.trim()
  return { cookie, csrf }
}

// ---- sink helpers ---------------------------------------------------------
async function flushSink(): Promise<void> {
  await fetch(SINK + '/api/messages', { method: 'DELETE' }).catch(() => {})
  await fetch(SINK + '/api/messages').catch(() => {})
}
async function sinkTo(email: string): Promise<{ n: number; subjects: string[] }> {
  const r = await fetch(SINK + '/api/messages')
  const env = (await r.json()) as { messages: Array<{ subject?: string; to?: string | string[] }> }
  const to = (m: { to?: string | string[] }) => {
    const t = Array.isArray(m.to) ? m.to : [m.to || '']
    return t.some((x) => String(x).toLowerCase() === email.toLowerCase())
  }
  const hits = (env.messages || []).filter(to)
  return { n: hits.length, subjects: hits.map((m) => String(m.subject || '')).sort() }
}

// ---- the battery -----------------------------------------------------------
const J = { 'content-type': 'application/json', accept: 'application/json' } as const
const POST = (csrf: string) => ({ ...J, 'x-csrf-token': csrf })

interface LegResult {
  cases: Record<string, R>
  mailSubject: string
  mailDelta: number
  signupEnabled: boolean
}

async function battery(me: Sess, guest: Sess, recv: string): Promise<LegResult> {
  const out: Record<string, R> = {}

  // A — admin GET /admin/site → 302 /hub#/site
  out['A GET /admin/site → 302'] = await call('/admin/site', { cookie: me.cookie, headers: { accept: 'application/json' } })

  // B — admin GET /admin/site-settings → 200 (normalised)
  out['B GET /admin/site-settings → 200'] = await call('/admin/site-settings', { cookie: me.cookie, headers: J })
  const settings = (JSON.parse(out['B GET /admin/site-settings → 200'].body) || {}) as Record<string, any>
  const signup = (settings['signup'] || {})
  const signupEnabled = !!signup.enabled

  // C — PUT signup (enabled=false) then restore
  out['C PUT signup(enabled=false) → 200'] = await call('/admin/site-settings/signup', {
    method: 'PUT', headers: POST(me.csrf), cookie: me.cookie, body: JSON.stringify({ ...signup, enabled: false }),
  })
  out['C2 PUT signup(restore) → 200'] = await call('/admin/site-settings/signup', {
    method: 'PUT', headers: POST(me.csrf), cookie: me.cookie, body: JSON.stringify(signup),
  })

  // C3 — PUT signup invalid → 422
  out['C3 PUT signup(enabled:"no") → 422'] = await call('/admin/site-settings/signup', {
    method: 'PUT', headers: POST(me.csrf), cookie: me.cookie, body: JSON.stringify({ enabled: 'no' }),
  })

  // D — PUT unknown section → 422
  out['D PUT unknown-section → 422'] = await call('/admin/site-settings/notreal', {
    method: 'PUT', headers: POST(me.csrf), cookie: me.cookie, body: JSON.stringify({ enabled: true }),
  })

  // E — PUT storage (fs) → 200 (envLines array)
  out['E PUT storage(fs) → 200'] = await call('/admin/site-settings/storage', {
    method: 'PUT', headers: POST(me.csrf), cookie: me.cookie,
    body: JSON.stringify({ backend: 'fs', s3Endpoint: '', s3AccessKeyId: '', s3Secret: '', templateFilesBucket: '', projectBlobsBucket: '', globalBlobsBucket: '', docstoreArchiveBucket: '' }),
  })

  // F — email/test invalid → 422
  out['F POST email/test invalid → 422'] = await call('/admin/site-settings/email/test', {
    method: 'POST', headers: POST(me.csrf), cookie: me.cookie, body: JSON.stringify({ to: 'not-an-email' }),
  })

  // G — email/test valid → 200 + mail
  await flushSink()
  out['G POST email/test valid → 200'] = await call('/admin/site-settings/email/test', {
    method: 'POST', headers: POST(me.csrf), cookie: me.cookie, body: JSON.stringify({ to: recv }),
  })
  const afterMail = await sinkTo(recv)

  // H — template-admins
  out['H GET template-admins → 200'] = await call('/admin/site/template-admins', { cookie: me.cookie, headers: J })

  // I — authz
  out['I1 anon JSON GET → 401'] = await call('/admin/site-settings', { headers: { accept: 'application/json' } })
  out['I2 anon html GET → 302'] = await call('/admin/site-settings', { headers: { accept: 'text/html' } })
  out['I3 non-admin GET → 302'] = await call('/admin/site-settings', { cookie: guest.cookie, headers: J })

  return { cases: out, mailSubject: afterMail.subjects[0] || '', mailDelta: afterMail.n, signupEnabled }
}

// ---- leg diff --------------------------------------------------------------
function diffLegs(a: Record<string, R>, b: Record<string, R>): string[] {
  const keys = new Set([...Object.keys(a), ...Object.keys(b)])
  const problems: string[] = []
  for (const k of keys) {
    const x = a[k], y = b[k]
    if (!x || !y) { problems.push(`${k}: presence ${x ? 'only-baseline' : 'only-flip'}`); continue }
    if (x.status !== y.status) problems.push(`${k}: status ${x.status} vs ${y.status}`)
    for (const h of HDRS) {
      if (h === 'content-length') continue
      if ((x.h[h] || '') !== (y.h[h] || '')) problems.push(`${k}: header ${h}: "${x.h[h]}" vs "${y.h[h]}"`)
    }
    const ax = canonForCompare(x.body), by = canonForCompare(y.body)
    if (ax !== by) problems.push(`${k}: body ${JSON.stringify(ax).slice(0, 220)} vs ${JSON.stringify(by).slice(0, 220)}`)
  }
  return problems
}

// ---- the gate -----------------------------------------------------------------
test.describe.serial('web-go P3.6 flip gate (WEB_GO_PLAN P3.6 SiteSettings)', () => {
  let overleafC = ''
  let mongoName = ''
  let leg1: LegResult | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    mongoName = runningContainer('ol-e2e-mongo')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/runit/web-go-overleaf/run'), `${overleafC}:/tmp/webgo-run`])
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p3e.conf'), `${overleafC}:/tmp/web-p3e.conf`])
    dexe(
      overleafC,
      'mkdir -p /usr/local/share/overleaf-flips /etc/service/web-go-overleaf && cp /tmp/webgo-run /etc/service/web-go-overleaf/run && chmod 755 /etc/service/web-go-overleaf/run && cp /tmp/web-p3e.conf /usr/local/share/overleaf-flips/web-p3e.conf'
    )
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

  async function prep() {
    const me = await login(ADMIN)
    const guest = await login(GUEST)
    await flushSink()
    return { me, guest }
  }

  test('leg 1: Node baseline battery', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('strip'), true)
    await nginxSettled()
    const { me, guest } = await prep()
    leg1 = await battery(me, guest, 'p3e-leg1@e2e.test')
    const c = leg1.cases
    expect(c['A GET /admin/site → 302'].status).toBe(302)
    expect(c['A GET /admin/site → 302'].h.location).toBe('/hub#/site')
    expect(c['B GET /admin/site-settings → 200'].status).toBe(200)
    expect(c['C PUT signup(enabled=false) → 200'].status).toBe(200)
    expect(c['C2 PUT signup(restore) → 200'].status).toBe(200)
    expect(c['C3 PUT signup(enabled:"no") → 422'].status).toBe(422)
    expect(c['C3 PUT signup(enabled:"no") → 422'].body).toContain('enabled must be a boolean')
    expect(c['D PUT unknown-section → 422'].status).toBe(422)
    expect(c['D PUT unknown-section → 422'].body).toContain('Unknown section: notreal')
    expect(c['E PUT storage(fs) → 200'].status).toBe(200)
    expect(c['E PUT storage(fs) → 200'].body).toContain('"envLines":["export OVERLEAF_FILESTORE_BACKEND=')
    expect(c['F POST email/test invalid → 422'].status).toBe(422)
    expect(c['F POST email/test invalid → 422'].body).toContain('Enter a valid e-mail address')
    expect(c['G POST email/test valid → 200'].status).toBe(200)
    expect(c['G POST email/test valid → 200'].body).toBe('{"ok":true}')
    expect(c['H GET template-admins → 200'].status).toBe(200)
    expect(c['I1 anon JSON GET → 401'].status).toBe(401)
    expect(c['I2 anon html GET → 302'].status).toBe(302)
    expect(c['I2 anon html GET → 302'].h.location).toBe('/login')
    expect(c['I3 non-admin GET → 302'].status).toBe(302)
    expect(c['I3 non-admin GET → 302'].h.location).toBe('/restricted?from=%2Fadmin%2Fsite-settings')
    // side effects
    expect(leg1.mailDelta, 'leg1 mail delta').toBe(1)
    expect(leg1.mailSubject).toBe('[Overleaf] E-mail configuration test')
  }, 300_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('apply'))
    await nginxSettled()
    const { me, guest } = await prep()
    const leg2 = await battery(me, guest, 'p3e-leg2@e2e.test')
    expect(leg2.mailDelta, 'leg2 mail delta').toBe(1)
    expect(leg2.mailSubject, 'leg2 mail subject').toBe(leg1!.mailSubject)
    const problems = diffLegs(leg1!.cases, leg2.cases)
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 300_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('strip'))
    await nginxSettled()
    const { me, guest } = await prep()
    const leg3 = await battery(me, guest, 'p3e-leg3@e2e.test')
    expect(leg3.mailDelta, 'leg3 mail delta').toBe(1)
    expect(leg3.mailSubject).toBe(leg1!.mailSubject)
    const problems = diffLegs(leg1!.cases, leg3.cases)
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 300_000)
})
