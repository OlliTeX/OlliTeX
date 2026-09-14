/**
 * WEB-GO P3.4 FLIP GATE (WEB_GO_PLAN.md P3.4 — registration page):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p3d.conf):
 *     GET  /register  (registration shell, anon + logged-in)
 *     POST /register  (ensureRegistrationEnabled → rateLimit(5/60) → create)
 *
 *   leg 1  Node baseline — the full battery via nginx (flip OFF)
 *   leg 2  FLIP ON       — identical battery on Go, byte-for-byte match
 *   leg 3  FLIP OFF      — Node again (reversal)
 *
 * Contract pins (live 2026-09-14, Node oracle + A/B battery 10/10 PASS):
 *   - GET anon (enabled)      → 200 text/html (React shell);
 *   - POST anon no-csrf       → 403 text/plain "Forbidden" (core csrf gate);
 *   - POST disabled           → 403 text/plain "Registration is disabled on this site";
 *   - POST logged-in          → 302 → "/";
 *   - POST first/last >100 or non-string → 400 {"message":"Too long name."};
 *   - POST invalid email      → 400 {"message":"Invalid email address."};
 *   - POST disallowed domain  → 403 {"message":"Registration is not available for this email domain."};
 *   - POST email registered   → 409 {"message":{"key":"account_with_this_email_exists"}};
 *   - POST happy              → 200 {"message":"Registration successful. Please check your email to activate your account."}
 *                               + user doc (bcrypt hash, emails[], signUpDate),
 *                               + 'password' token (use=password, token 64 hex, expires ≈ 7d),
 *                               + mail subject "Activate your OlliTeX Account" to the new email;
 *   - POST 6th in 60s         → 429 "Rate limit reached, please try again later";
 *   - logged-in rate bucketed by user id (Node getUserId(req)||req.ip) — 302 NOT 429;
 *   - rate limit shared Redis bucket — the gate resets it between legs.
 *
 * State discipline (mongo ol-e2e-mongo-*, redis ol-e2e-redis-*):
 *   - the fixed happy-email user + its token are deleted and the
 *     `rate-limit:postRegister:*` redis keys are dropped between legs;
 *   - the smtp sink is flushed per leg; the happy leg's mail is the only
 *     registration traffic (asserted exactly-one "Activate your …" to the
 *     new address).
 *
 * Afterwards the stack is left node-active (e2e convention).
 *
 * Run: npx playwright test -g "web-go P3.4 flip gate"
 */
import { execFileSync } from 'child_process'
import { test, expect } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const SINK = process.env.E2E_SMTP_SINK || 'http://127.0.0.1:18025'

const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const HAPPY = 'p34-happy@e2e.test'
const TAKEN = 'e2e-user@e2e.test'

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

// ---- containers ----------------------------------------------------------

function dexe(container: string, cmd: string, allowFail = false): string {
  try {
    return execFileSync('docker', ['exec', container, 'sh', '-c', cmd], {
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
  const out = execFileSync('docker', ['ps', `--filter`, `name=${match}`, `--format`, `{{.Names}}`], {
    encoding: 'utf8',
  }).trim()
  const names = out.split('\n')
  if (!names.length) throw new Error(`no running container matching ${match}`)
  return names[0]
}

// ---- normalization -------------------------------------------------------

function normBody(s: string): string {
  return s
    .replace(/overleaf\.sid=s%3A[^;,\s"\\]+/g, 'SID')
    .replace(/nonce="[^"]+"/g, 'nonce="N"')
    .replace(/<meta name="ol-csrfToken" content="[^"]*"/g, '<meta name="ol-csrfToken" content="CSRF"')
    .replace(/<input name="_csrf" type="hidden" value="[^"]*"/g, '<input name="_csrf" type="hidden" value="CSRF"')
    .replace(/"ol-csrfToken":"[^"]*"/g, '"ol-csrfToken":"CSRF"')
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

const HDRS = [
  'content-type', 'location', 'vary', 'content-security-policy', 'permissions-policy',
  'cross-origin-opener-policy', 'cross-origin-resource-policy', 'referrer-policy',
  'x-content-type-options', 'x-download-options', 'x-frame-options',
  'x-permitted-cross-domain-policies', 'x-xss-protection', 'cache-control', 'expires',
  'pragma', 'surrogate-control', 'www-authenticate', 'x-powered-by',
] as const

interface R { status: number; h: Record<string, string>; body: string; setcookie: string }

async function nginxSettled(timeoutMs = 15_000): Promise<void> {
  const t0 = Date.now()
  for (;;) {
    try {
      const r = await fetch(BASE + '/status', { redirect: 'manual' })
      if (r.status >= 100) {
        await r.text().catch(() => {})
        return
      }
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
    h['etag'] = r.headers.get('etag') || ''
    h['content-length'] = r.headers.get('content-length') || ''
    const sc = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : r.headers.get('set-cookie') || ''
    const body = Buffer.from(await r.arrayBuffer()).toString('binary')
    return { status: r.status, h, body, setcookie: sc }
  } catch (e) {
    if (attempt < 3 && /socket|ECONNRESET|other side closed|fetch failed/i.test(String(e))) {
      await sleep(400 * (attempt + 1))
      return call(p, init, attempt + 1)
    }
    throw e
  }
}

// ---- flip plumbing (web-p3d.conf) ----------------------------------------

function FLIP(cmd: 'apply' | 'strip'): string {
  const conf = 'web-p3d.conf'
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
  const cookie = lines[lines.length - 1] || a.cookie
  const csrf = (await call('/dev/csrf', { cookie })).body.trim()
  return { cookie, csrf }
}

// ---- state helpers ---------------------------------------------------------

function cleanRegistrationState(mongoName: string, redisName: string): void {
  const f = `/tmp/p34-clean.js`
  execFileSync('docker', ['exec', '-i', mongoName, 'sh', '-c', 'cat > /tmp/p34-clean.js'], {
    input: `
db.users.deleteMany({ email: { $regex: /^p34-/ } });
db.tokens.deleteMany({ "data.email": { $regex: /^p34-/ } });
print('cleaned');
`,
  })
  dexe(mongoName, `mongosh --quiet mongodb://localhost/sharelatex ${f}`)
  // reset the shared postRegister rate-limit bucket (Go key + a Node fallback)
  dexe(redisName, `redis-cli --scan --pattern "rate-limit:postRegister:*" | xargs -r redis-cli DEL`, true)
  dexe(redisName, `redis-cli --scan --pattern "rl:postRegister:*" | xargs -r redis-cli DEL`, true)
  dexe(redisName, `redis-cli --scan --pattern "*postRegister*" | xargs -r redis-cli DEL`, true)
}

function happyUserDoc(mongoName: string, email: string): string {
  dexe(mongoName, `cat > /tmp/p34-user.js <<'EOF'
const u = db.users.findOne({ email: "${email}" });
if (!u) { print("NOUSER"); quit(); }
print(JSON.stringify({
  hasHash: String(u.hashedPassword || "").startsWith("$2"),
  fn: u.first_name, ln: u.last_name,
  emails0: (u.emails && u.emails[0] && u.emails[0].email) || "",
  hasSignUp: !!u.signUpDate,
}))
EOF`, true)
  return dexe(mongoName, `mongosh --quiet mongodb://localhost/sharelatex /tmp/p34-user.js`, true).trim()
}

function happyToken(mongoName: string, email: string): string {
  const f = `/tmp/p34-tok.js`
  dexe(mongoName, `cat > /tmp/p34-tok.js <<'EOF'
const t = db.tokens.findOne({ "data.email": "${email}" });
if (!t) { print("NOTOKEN"); quit(); }
const days = Math.round((new Date(t.expiresAt) - new Date(t.createdAt)) / 86400000);
print(JSON.stringify({ use: t.use, tokenLen: (t.token||"").length, days }));
EOF`, true)
  return dexe(mongoName, `mongosh --quiet mongodb://localhost/sharelatex /tmp/p34-tok.js`, true).trim()
}

async function flushSink(): Promise<void> {
  await fetch(SINK + '/api/flush', { method: 'POST' }).catch(() => {})
  await fetch(SINK + '/api/messages').catch(() => {})
}

async function sinkReg(email: string): Promise<{ n: number; subjects: string[] }> {
  const r = await fetch(SINK + '/api/messages')
  const env = (await r.json()) as { count: number; messages: Array<{ subject?: string; to?: string | string[] }> }
  const to = (m: { to?: string | string[] }): boolean => {
    const t = Array.isArray(m.to) ? m.to : [m.to || '']
    return t.some((x) => String(x).toLowerCase() === email.toLowerCase())
  }
  const hits = (env.messages || []).filter(to)
  return { n: hits.length, subjects: hits.map((m) => String(m.subject || '')).sort() }
}

// ---- the battery -----------------------------------------------------------

const J = { 'content-type': 'application/json', accept: 'application/json' } as const
const POST = (csrf: string) => ({ ...J, 'x-csrf-token': csrf })

interface LegResult { cases: Record<string, R>; happySubject: string; happyDelta: number }

async function battery(me: Sess, anon: Sess): Promise<LegResult> {
  const out: Record<string, R> = {}
  const put = (csrf: string, ck: string, body: object) =>
    call('/register', { method: 'POST', headers: POST(csrf), cookie: ck, body: JSON.stringify(body) })
  const before = await sinkReg(HAPPY) // 0 after prep's flush

  // R1 — anonymous page (enabled)
  out['R1 GET /register anon → 200 html'] = await call('/register', { cookie: anon.cookie, headers: { accept: 'text/html' } })

  // R2 — POST no csrf → 403
  out['R2 POST no-csrf → 403 Forbidden'] = await call('/register', {
    method: 'POST', headers: { accept: 'application/json', 'content-type': 'application/json' },
    cookie: anon.cookie, body: JSON.stringify({ email: 'p34-x@e2e.test', first_name: 'A', last_name: 'B' }),
  })

  // R3 — invalid email
  out['R3 POST invalid-email → 400'] = await put(anon.csrf, anon.cookie, { email: 'not-an-email', first_name: 'A', last_name: 'B' })
  // R4 — name too long
  out['R4 POST name-too-long → 400'] = await put(anon.csrf, anon.cookie, { email: 'p34-ok@e2e.test', first_name: 'x'.repeat(101), last_name: 'B' })
  // R5 — disallowed domain
  out['R5 POST disallowed-domain → 403'] = await put(anon.csrf, anon.cookie, { email: 'p34-dom@other.com', first_name: 'A', last_name: 'B' })
  // R6 — email taken
  out['R6 POST email-taken → 409'] = await put(anon.csrf, anon.cookie, { email: TAKEN, first_name: 'A', last_name: 'B' })
  // R7 — happy
  out['R7 POST happy → 200'] = await put(anon.csrf, anon.cookie, { email: HAPPY, first_name: 'Reg', last_name: 'One' })

  // R8 — logged-in → 302 /
  out['R8 POST logged-in → 302 /'] = await call('/register', {
    method: 'POST', headers: POST(me.csrf), cookie: me.cookie,
    body: JSON.stringify({ email: 'p34-li@e2e.test', first_name: 'L', last_name: 'I' }),
  })

  // R9/R10 — rate limit (2 more consuming POSTs, anonymous bucket)
  out['R9 POST rate-limit-1 → 429'] = await put(anon.csrf, anon.cookie, { email: 'p34-rl1@e2e.test', first_name: 'A', last_name: 'B' })
  out['R10 POST rate-limit-2 → 429'] = await put(anon.csrf, anon.cookie, { email: 'p34-rl2@e2e.test', first_name: 'A', last_name: 'B' })

  const after = await sinkReg(HAPPY)
  return { cases: out, happySubject: after.subjects[0] || '', happyDelta: after.n - before.n }
}

// ---- leg diff --------------------------------------------------------------

function diffLegs(a: Record<string, R>, b: Record<string, R>): string[] {
  const keys = new Set([...Object.keys(a), ...Object.keys(b)])
  const problems: string[] = []
  for (const k of keys) {
    const x = a[k], y = b[k]
    if (!x || !y) { problems.push(`${k}: presence ${x ? 'only-baseline' : 'only-flip'}`); continue }
    if (x.status !== y.status) problems.push(`${k}: status ${x.status} vs ${y.status}`)
    const normH = (v: string) => (v || '').replace(/nonce-[A-Za-z0-9+/,=~]{16,}/g, 'nonce-NONCE')
    for (const h of HDRS) if (normH(x.h[h] || '') !== normH(y.h[h] || '') && h !== 'etag' && h !== 'content-length') {
      problems.push(`${k}: header ${h}: ${x.h[h]||''} vs ${y.h[h]||''}`)
    }
    if ((x.h['content-length'] || '') !== (y.h['content-length'] || '') ) {
      problems.push(`${k}: content-length ${x.h['content-length']} vs ${y.h['content-length']}`)
    }
    const ax = normBody(x.body), by = normBody(y.body)
    if (ax !== by) problems.push(`${k}: body ${ax.slice(0, 200)}... vs ${by.slice(0, 200)}...`)
    if (normCookie(x.setcookie) !== normCookie(y.setcookie)) problems.push(`${k}: setcookie ${x.setcookie} vs ${y.setcookie}`)
  }
  return problems
}

// ---- the gate -----------------------------------------------------------------

test.describe.serial('web-go P3.4 flip gate (WEB_GO_PLAN P3.4 registration page)', () => {
  let overleafC = ''
  let mongoName = ''
  let redisName = ''
  let leg1: LegResult | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    mongoName = runningContainer('ol-e2e-mongo')
    redisName = runningContainer('ol-e2e-redis')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, `chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web`)
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/runit/web-go-overleaf/run'), `${overleafC}:/tmp/webgo-run`])
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p3d.conf'), `${overleafC}:/tmp/web-p3d.conf`])
    dexe(
      overleafC,
      `mkdir -p /usr/local/share/overleaf-flips /etc/service/web-go-overleaf && cp /tmp/webgo-run /etc/service/web-go-overleaf/run && chmod 755 /etc/service/web-go-overleaf/run && cp /tmp/web-p3d.conf /usr/local/share/overleaf-flips/web-p3d.conf`
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
    try {
      dexe(overleafC, FLIP('strip'), true)
      await nginxSettled()
    } catch { /* best effort */ }
  })

  async function prep(): Promise<{ me: Sess; anon: Sess }> {
    const me = await login(USER)
    const anon = await anonSession()
    cleanRegistrationState(mongoName, redisName)
    await flushSink() // start each leg with an empty sink
    return { me, anon }
  }

  test('leg 1: Node baseline battery', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('strip'), true)
    await nginxSettled()
    const { me, anon } = await prep()
    leg1 = await battery(me, anon)
    const c = leg1.cases
    expect(c['R1 GET /register anon → 200 html'].status).toBe(200)
    expect(c['R2 POST no-csrf → 403 Forbidden'].status).toBe(403)
    expect(c['R3 POST invalid-email → 400'].status).toBe(400)
    expect(c['R4 POST name-too-long → 400'].status).toBe(400)
    expect(c['R5 POST disallowed-domain → 403'].status).toBe(403)
    expect(c['R6 POST email-taken → 409'].status).toBe(409)
    expect(c['R7 POST happy → 200'].status).toBe(200)
    expect(c['R8 POST logged-in → 302 /'].status).toBe(302)
    expect(c['R8 POST logged-in → 302 /'].h.location).toBe('/')
    expect(c['R9 POST rate-limit-1 → 429'].status).toBe(429)
    expect(c['R10 POST rate-limit-2 → 429'].status).toBe(429)
    // side effects
    expect(leg1.happySubject, 'leg1 mail subject').toContain('Activate your')
    expect(leg1.happyDelta, 'leg1 happy mail delta').toBe(1)
    const du = happyUserDoc(mongoName, HAPPY)
    expect(du, 'leg1 user doc').toContain('hasHash":true')
    const dt = happyToken(mongoName, HAPPY)
    expect(dt, 'leg1 token').toContain('"use":"password"')
    expect(dt, 'leg1 token days').toContain('"days":7')
  }, 300_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('apply'))
    await nginxSettled()
    const { me, anon } = await prep()
    const leg2 = await battery(me, anon)
    expect(leg2.happySubject, 'leg2 mail subject parity').toBe(leg1!.happySubject)
    expect(leg2.happyDelta, 'leg2 happy mail delta').toBe(1)
    const du = happyUserDoc(mongoName, HAPPY)
    expect(du, 'leg2 user doc').toContain('hasHash":true')
    const dt = happyToken(mongoName, HAPPY)
    expect(dt, 'leg2 token').toContain('"use":"password"')
    expect(dt, 'leg2 token days').toContain('"days":7')
    const problems = diffLegs(leg1!.cases, leg2.cases)
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 300_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('strip'))
    await nginxSettled()
    const { me, anon } = await prep()
    const leg3 = await battery(me, anon)
    expect(leg3.happySubject, 'leg3 mail subject parity').toBe(leg1!.happySubject)
    expect(leg3.happyDelta, 'leg3 happy mail delta').toBe(1)
    const problems = diffLegs(leg1!.cases, leg3.cases)
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 300_000)
})
