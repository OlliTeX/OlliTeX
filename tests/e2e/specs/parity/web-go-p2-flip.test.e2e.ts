/**
 * WEB-GO P2 FLIP GATE (WEB_GO_PLAN.md P2 — password reset + token access):
 *
 *   leg 1  Node baseline  the P2 battery answered by Node through nginx
 *                         :7420 (flip OFF) — pages, the reset/request
 *                         matrix, the set-password validation matrix, the
 *                         reset SUCCESS round-trip (with restore), the
 *                         token access pages, the grant matrix (anon +
 *                         logged-in + 404), the consent page/moves, and a
 *                         rate-limit 429 burst — captured as canonical.
 *   leg 2  FLIP ON        the identical battery answered by the Go shadow
 *                         via server-ce/nginx/flips/web-p2.conf — every
 *                         response must match the Node baseline
 *                         byte-for-byte after nonce/csrf normalization.
 *   leg 3  FLIP OFF       re-runs on Node and matches again (reversal).
 *
 * Shared-state discipline (Node and Go share redis + mongo):
 *   - the password_reset_rate_limit window (6/60s, key per IP) is rest 66s
 *     before every budget-sensitive section, and after the 429 burst;
 *   - the reset SUCCESS probe runs in its own window and ALWAYS restores
 *     the e2e password (the battery ends with the fixture password);
 *   - consent fixture (PID) token refs are restored in teardown.
 *
 * Afterwards the stack is left node-active (e2e convention).
 *
 * Run: npx playwright test -g "web-go P2 flip gate"
 */
import { execFileSync } from 'child_process'
import path from 'path'
import { fileURLToPath } from 'url'
import { test, expect } from '@playwright/test'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const SINK = 'http://127.0.0.1:18025'

const USER = {
  email: 'e2e-user@e2e.test',
  password: 'Ol-Fixture-3m2Q',
} as const
const PID = '6aa4b8c973ef0e5094f4cc02'
const RW = '1234567890abcdefgh'
const RO = 'abcdefghijkl'
const NEWPW = 'P2gate-P@ss-9x'

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

function mongoshPrint(expr: string): string {
  const b64 = Buffer.from(`print(${expr})`).toString('base64')
  return dexe(runningContainer('ol-e2e-mongo'), `printf %s '${b64}' | base64 -d > /tmp/mq.js && mongosh --quiet sharelatex /tmp/mq.js 2>/dev/null | tail -1`).trim()
}

function mongoshRun(stmts: string): string {
  const b64 = Buffer.from(stmts).toString('base64')
  return dexe(runningContainer('ol-e2e-mongo'), `printf %s '${b64}' | base64 -d > /tmp/mq.js && mongosh --quiet sharelatex /tmp/mq.js 2>/dev/null | tail -1`).trim()
}

// ---- normalization -------------------------------------------------------

function norm(s: string): string {
  return s
    .replace(/nonce="[A-Za-z0-9+/=_-]{16,}"/g, 'nonce="N"')
    .replace(/name="ol-csrfToken" content="[^"]*"/g, 'name="ol-csrfToken" content="T"')
    .replace(/value="[A-Za-z0-9+/_=-]{12,}"/g, 'value="CSRF"')
    .replace(/overleaf\.sid=s%3A[^;"]+/g, 'overleaf.sid=SID')
    .replace(/overleaf\.sid=s:[^;"]+/g, 'overleaf.sid=SID')
    .replace(/Expires=[^;]+/g, 'Expires=EXPIRE')
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
  const sc = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : r.headers.get('set-cookie') || ''
  const body = await r.text()
  return {
    status: r.status,
    ct: r.headers.get('content-type') || '',
    csp: r.headers.get('content-security-policy') || '',
    location: r.headers.get('location') || '',
    setcookie: sc,
    body,
  }
}

const sleep = (ms: number) => new Promise((res) => setTimeout(res, ms))

// ---- flip plumbing (web-p2.conf) ----------------------------------------

const FLIP_APPLY = `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/web-p2.conf /etc/nginx/overleaf-flips/web-p2.conf
if ! grep -q "overleaf-flips/web-p2.conf" "$vhost"; then
  node -e '
    const fs = require("fs");
    const v = process.argv[1];
    const inc = "  include /etc/nginx/overleaf-flips/web-p2.conf;\\n\\n";
    let s = fs.readFileSync(v, "utf8");
    if (!s.includes("overleaf-flips/web-p2.conf")) {
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
if grep -q "overleaf-flips/web-p2.conf" "$vhost"; then
  sed -i "/overleaf-flips\\/web-p2.conf/d" "$vhost"
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

/** full login through the (unflipped) login route; returns the logged-in
 * session cookie + the session-stable csrf token. */
async function loginUser(password: string): Promise<Sess & { status: number; body: string }> {
  const a = await anonSession()
  const r = await call('/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': a.csrf, accept: 'application/json' },
    cookie: a.cookie,
    body: JSON.stringify({ email: USER.email, password }),
  })
  const lines = r.setcookie.split('\n').filter((l) => l.includes('overleaf.sid'))
  await sleep(800)
  return { cookie: lines[lines.length - 1] || a.cookie, csrf: a.csrf, status: r.status, body: r.body }
}

/** mint a reset token for USER within session s. */
async function mint(s: Sess): Promise<string> {
  const r = await call('/user/password/reset', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': s.csrf, accept: 'application/json' },
    cookie: s.cookie,
    body: JSON.stringify({ email: USER.email }),
  })
  if (r.status !== 200) throw new Error(`mint failed: ${r.status} ${r.body.slice(0, 120)}`)
  await sleep(400)
  const tok = mongoshPrint(`db.tokens.find().sort({createdAt:-1}).limit(1).toArray()[0].token`)
  expect(tok.length, 'mint must yield a 64-hex token').toBe(64)
  return tok
}

const JSONH = { 'content-type': 'application/json', accept: 'application/json' } as const

// ---- the battery ----------------------------------------------------------

interface Leg {
  out: Record<string, R>
}

async function battery(): Promise<Leg> {
  const out: Record<string, R> = {}
  const A = await anonSession()

  // S1 — pages
  out['reset GET anon'] = await call('/user/password/reset', { cookie: A.cookie })
  out['reset GET token_expired'] = await call('/user/password/reset?error=token_expired', { cookie: A.cookie })

  // S2 — reset matrix (budget: 2 reset POSTs + 1 mint = 3)
  out['reset POST invalid email'] = await call('/user/password/reset', {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': A.csrf }, cookie: A.cookie, body: JSON.stringify({ email: 'not-an-email' }),
  })
  out['reset POST unknown email'] = await call('/user/password/reset', {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': A.csrf }, cookie: A.cookie, body: JSON.stringify({ email: 'nobody-xyz-p2b@e2e.test' }),
  })
  const t2 = await mint(A) // 3
  out['set GET token redirect'] = await call(`/user/password/set?passwordResetToken=${t2}&email=${USER.email}`, { cookie: A.cookie })
  out['set GET rendered'] = await call(`/user/password/set?email=${USER.email}`, { cookie: A.cookie })

  await sleep(66_000) // password_reset budget clean

  // S3 — set POST validation matrix (own window, 3 POSTs)
  const t3 = await mint(A) // 1
  out['set POST missing token'] = await call('/user/password/set', {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': A.csrf }, cookie: A.cookie, body: JSON.stringify({ password: 'longenough1' }),
  }) // 2
  out['set POST too short'] = await call('/user/password/set', {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': A.csrf }, cookie: A.cookie, body: JSON.stringify({ passwordResetToken: t3, password: 'abc', email: USER.email }),
  }) // 3 (peek 1)
  out['set POST invalid char'] = await call('/user/password/set', {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': A.csrf }, cookie: A.cookie, body: JSON.stringify({ passwordResetToken: t3, password: 'abcdefg ', email: USER.email }),
  }) // 4 (peek 2) — budget 4/6
  out['set POST contains email'] = await call('/user/password/set', {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': A.csrf }, cookie: A.cookie, body: JSON.stringify({ passwordResetToken: t3, password: 'e2e-user123', email: USER.email }),
  }) // 5 (peek 3)

  await sleep(66_000)

  // S4 — reset SUCCESS round-trip + restore + session kill (4 POSTs + mints)
  const U0 = await loginUser(USER.password) // kill-check target
  const t4 = await mint(A) // 1
  out['set POST success'] = await call('/user/password/set', {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': A.csrf }, cookie: A.cookie, body: JSON.stringify({ passwordResetToken: t4, password: NEWPW, email: USER.email }),
  }) // 2
  out['old cookie dead at gate'] = await call('/hub', { cookie: U0.cookie })
  out['login with new password'] = { status: (await loginUser(NEWPW)).status, ct: '', csp: '', location: '', setcookie: '', body: '' }
  const t5 = await mint(A) // 3
  out['set POST restore'] = await call('/user/password/set', {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': A.csrf }, cookie: A.cookie, body: JSON.stringify({ passwordResetToken: t5, password: USER.password, email: USER.email }),
  }) // 4
  out['login restored'] = { status: (await loginUser(USER.password)).status, ct: '', csp: '', location: '', setcookie: '', body: '' }

  await sleep(66_000)

  // S5 — token pages + grants + consent (grant limiters: 10/60s)
  const U = await loginUser(USER.password)
  const B = await anonSession()
  out['rw page anon gate'] = await call(`/${RW}`, { cookie: B.cookie })
  out['rw page user'] = await call(`/${RW}`, { cookie: U.cookie })
  out['ro page user'] = await call(`/read/${RO}`, { cookie: U.cookie })
  out['rw page unknown token'] = await call('/9876543210zzyyyy', { cookie: U.cookie })
  out['grant rw anon'] = await call(`/${RW}/grant`, {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': B.csrf }, cookie: B.cookie, body: JSON.stringify({ confirmedByUser: true }),
  })
  out['grant ro anon'] = await call(`/read/${RO}/grant`, {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': B.csrf }, cookie: B.cookie, body: JSON.stringify({ confirmedByUser: true }),
  })
  out['grant rw unconfirmed'] = await call(`/${RW}/grant`, {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': U.csrf }, cookie: U.cookie, body: JSON.stringify({ confirmedByUser: false }),
  })
  out['grant rw confirmed'] = await call(`/${RW}/grant`, {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': U.csrf }, cookie: U.cookie, body: JSON.stringify({ confirmedByUser: true }),
  })
  out['grant ro confirmed'] = await call(`/read/${RO}/grant`, {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': U.csrf }, cookie: U.cookie, body: JSON.stringify({ confirmedByUser: true }),
  })
  out['grant rw unknown 404'] = await call('/9876543210zzyyyy/grant', {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': U.csrf }, cookie: U.cookie, body: JSON.stringify({ confirmedByUser: true }),
  })
  out['consent page'] = await call(`/project/${PID}/sharing-updates`, { cookie: U.cookie })
  out['consent page 404'] = await call('/project/6aa4b8c973ef0e5094f4cdee/sharing-updates', { cookie: U.cookie })
  out['consent view move 204'] = await call(`/project/${PID}/sharing-updates/view`, {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': U.csrf }, cookie: U.cookie,
  })
  out['consent join move 204'] = await call(`/project/${PID}/sharing-updates/join`, {
    method: 'POST', headers: { ...JSONH, 'x-csrf-token': U.csrf }, cookie: U.cookie,
  })

  await sleep(66_000)

  // S6 — the 429 probe (fresh budget): first 6 pass (200), 7th = 429
  let first: R | null = null
  let last: R | null = null
  for (let i = 1; i <= 7; i++) {
    const r = await call('/user/password/reset', {
      method: 'POST', headers: { ...JSONH, 'x-csrf-token': A.csrf }, cookie: A.cookie, body: JSON.stringify({ email: USER.email }),
    })
    if (i === 1) first = r
    last = r
  }
  out['ratelimit first ok (200)'] = first!
  out['ratelimit 7th = 429'] = last!

  await sleep(68_000) // leave the window clean for the next leg
  return { out }
}

// ---- leg comparison ------------------------------------------------------

function diffLegs(base: Leg, other: Leg): string[] {
  const normCsp = (s: string) => s.replace(/nonce-[A-Za-z0-9+/=_-]+/g, 'nonce-N')
  const problems: string[] = []
  for (const k of Object.keys(base.out).sort()) {
    const b = base.out[k]
    const o = other.out[k]
    if (!o) { problems.push(`${k}: missing in other leg`); continue }
    for (const f of ['status', 'ct', 'csp', 'location', 'setcookie', 'body'] as const) {
      if (b[f] === undefined || o[f] === undefined) {
        problems.push(`${k}: field "${f}" undefined — base=${String(b[f]).slice(0, 60)} other=${String(o[f]).slice(0, 60)}`)
      }
    }
    if (b.status !== o.status) problems.push(`${k}: status ${b.status} != ${o.status}`)
    if (b.ct !== o.ct) problems.push(`${k}: content-type "${b.ct}" != "${o.ct}"`)
    if (normCsp(String(b.csp)) !== normCsp(String(o.csp))) problems.push(`${k}: csp differs: "${b.csp}" vs "${o.csp}"`)
    if (b.location !== o.location) problems.push(`${k}: location "${b.location}" != "${o.location}"`)
    const nb = norm(String(b.body)), ob = norm(String(o.body))
    if (nb !== ob) {
      let at = Math.min(nb.length, ob.length)
      for (let i = 0; i < Math.min(nb.length, ob.length); i++) {
        if (nb[i] !== ob[i]) { at = i; break }
      }
      problems.push(`${k}: body@${at}\n  base ${JSON.stringify(nb.slice(Math.max(0, at - 80), at + 80))}\n  othr ${JSON.stringify(ob.slice(Math.max(0, at - 80), at + 80))}`)
    }
    const bs = norm(String(b.setcookie)).split('\n').sort().join('|')
    const os = norm(String(o.setcookie)).split('\n').sort().join('|')
    if (bs !== os) problems.push(`${k}: setcookie "${bs.slice(0, 140)}" != "${os.slice(0, 140)}"`)
  }
  return problems
}

// ---- fixture restore ------------------------------------------------------

function restoreFixture(): void {
  const uid = mongoshPrint(`db.users.findOne({email:'${USER.email}'}, {_id:1})._id.toString()`)
  if (!uid) return
  mongoshRun(`db.projects.updateOne({_id: ObjectId('${PID}')}, {
    $pull: {collaberator_refs: ObjectId('${uid}')},
    $addToSet: {tokenAccessReadAndWrite_refs: ObjectId('${uid}'), tokenAccessReadOnly_refs: ObjectId('${uid}')},
    $set: {publicAccesLevel: 'tokenBased', tokens: {readOnly: '${RO}', readAndWrite: '${RW}', readAndWritePrefix: '1234567890'}}
  })`)
}

// ---- the gate ------------------------------------------------------------

test.describe.serial('web-go P2 flip gate (WEB_GO_PLAN P2)', () => {
  let overleafC = ''
  let leg1: Leg | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'bin/web'), `${overleafC}:/usr/local/bin/go-services/web`])
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/runit/web-go-overleaf/run'), `${overleafC}:/tmp/webgo-run`])
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p2.conf'), `${overleafC}:/tmp/web-p2.conf`])
    dexe(overleafC, `mkdir -p /usr/local/share/overleaf-flips /etc/service/web-go-overleaf && cp /tmp/webgo-run /etc/service/web-go-overleaf/run && chmod 755 /etc/service/web-go-overleaf/run && cp /tmp/web-p2.conf /usr/local/share/overleaf-flips/web-p2.conf`)
    const t0 = Date.now()
    for (;;) {
      const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
      if (code === '200') break
      if (Date.now() - t0 > 20_000) throw new Error(`go shadow never came up (last ${code})`)
      await sleep(500)
    }
    restoreFixture()
  })

  test.afterAll(() => {
    try { dexe(overleafC, FLIP_STRIP, true) } catch { /* gate */ }
    restoreFixture()
  })

  test('leg 1: Node baseline battery', async () => {
    test.setTimeout(1800_000)
    dexe(overleafC, FLIP_STRIP, true)
    leg1 = await battery()
    expect(Object.keys(leg1.out).length).toBeGreaterThan(20)
  }, 1800_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(1800_000)
    dexe(overleafC, FLIP_APPLY)
    const leg2 = await battery()
    const problems = diffLegs(leg1!, leg2)
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 1800_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(1800_000)
    dexe(overleafC, FLIP_STRIP)
    const leg3 = await battery()
    const problems = diffLegs(leg1!, leg3)
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 1800_000)

  test('smtp sink saw the password-reset mail (both stacks sent)', async () => {
    const raw = await fetch(SINK + '/api/messages')
    const env = (await raw.json()) as {
      count?: number
      messages?: Array<{ subject?: string }>
    }
    const msgs = env.messages ?? []
    const subs = msgs.map((m) => m.subject || '').filter((s) => String(s).includes('Password Reset'))
    expect(subs.length, `expected ≥5 reset mails, saw: ${subs.join(' | ')}`).toBeGreaterThanOrEqual(5)
  })
})
