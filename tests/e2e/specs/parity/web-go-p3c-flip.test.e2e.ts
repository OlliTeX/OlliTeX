/**
 * WEB-GO P3.3 FLIP GATE (WEB_GO_PLAN.md P3.3 — user pages / user settings):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p3c.conf):
 *     GET  /user/settings            (My settings page  + logged-in guard)
 *     POST /user/settings            (updateUserSettings — zod 30-field schema)
 *     GET  /user/sessions            (sessions page)
 *     GET /user/sessions/list        (JSON list)
 *     POST /user/sessions/clear      (logout all sessions + mail + audit)
 *
 *   leg 1  Node baseline — the full battery via nginx (flip OFF)
 *   leg 2  FLIP ON       — identical battery on Go, byte-for-byte match
 *   leg 3  FLIP OFF      — Node again (reversal)
 *
 * Contract pins (live 2026-09-14, /tmp/p3c-diff.mjs 56/56 PASS, Node
 * oracle battery + Node corner probes):
 *   - anon: settings/sessions pages → 302 /login?from=/user/<route>;
 *     list json → 401 plain + WWW-Authenticate; list plain/none → 302;
 *     clear json → 401; clear plain → 302; POST /user/settings → 403
 *     "Forbidden" (csrf BEFORE login gate).
 *   - core body gate: non-object JSON roots (42, null) → 400 BARE "{}"
 *     (body-parser strict, no web headers); array root → zod 400
 *     "Invalid input: expected object, received array at \"body\"".
 *   - validation: MULTI-issue reports joined by "; " in SCHEMA definition
 *     order, then unrecognized keys in body order. Messages pinned live
 *     (e.g. first_name 256 chars → "Too big: expected string to have
 *     <=255 characters at \"body.first_name\"" — literal '<', NOT
 *     \u003c).
 *   - customKeybindings: NO length/count VALIDATION limits (65 and 66
 *     valid-string entries → 200); save filter = slice(0,64) BEFORE the
 *     (non-empty key) AND (null | 1..24 char string) value filter, null →
 *     '' — the saved value is a Mongo MAP (the Node mongoose Map shape).
 *   - ref providers: z.object items (unknowns stripped), strict parent;
 *     groups[i] id missing → "received undefined", null → "received null".
 *   - emails: own → 200 no-op; invalid/empty → 400 {"error":"Invalid
 *     email format"}; taken → 409 {"error":"Email already exists"}.
 *   - first_name/last_name/role: sanitizeControlCharacters + trim on save.
 *   - sessions list: sorted desc, {sid, session_created, ip, device_info};
 *     ETag f(body) — the list carries per-session created-ms (genuinely
 *     different across legs) so its ETag is excluded from comparison.
 *   - clear: 201 "OK", sid cookie cleared, ALL user session docs purged
 *     (redis + UserSessions:{uid} index), mail "Overleaf security note:
 *     active sessions cleared" to the user's email, one audit entry per
 *     clear (userAuditLogEntries, type "session.clear").
 *
 * State discipline (mongo container ol-e2e-mongo-*):
 *   - fixture user doc restored (first_name E2e / last_name User / role
 *     user / ace.customKeybindings {}) in beforeAll and at the END of
 *     every battery (final restore case S7.5) — every leg starts from
 *     the same slate;
 *   - user sessions cleared + smtp sink counted per leg;
 *   - audit delta = 1 per leg (asserted afterAll: 3 total).
 *
 * Afterwards the stack is left node-active (e2e convention).
 *
 * Run: npx playwright test -g "web-go P3.3 flip gate"
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
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const SUBJECT = 'Overleaf security note: active sessions cleared'

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
    .replace(/overleaf\.sid=s%3A[^;,\s"\\]+/g, 'overleaf.sid=SID.')
    .replace(/"sid":"[^"]*"/g, '"sid":"SID"')
    .replace(/Expires=[^;,\s]+/g, 'Expires=EX')
    .replace(
      /\b(20\d{2}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\b/g,
      'TS'
    )
    .replace(/nonce-[A-Za-z0-9+/=~]{16,}/g, 'nonce-NONCE')
    .replace(/nonce="[^"]+"/g, 'nonce="N"')
    .replace(/<meta name="ol-csrfToken" content="[^"]*"/g, '<meta name="ol-csrfToken" content="CSRF"')
    .replace(/"ol-csrfToken":"[^"]*"/g, '"ol-csrfToken":"CSRF"')
    .replace(/<input name="_csrf" type="hidden" value="[^"]*"/g, '<input name="_csrf" type="hidden" value="CSRF"')
    .replace(/"(first_name|last_name|role|institution|email|customKeybindings|session_created|created_at|updated_at|lastActive|lastLoginIp|lastLoggedIn|session_ip)":\s*("[^"]*"|\d+|\[\s*\]|null)/g, '"$1":"X"')
    .replace(/\[\s*\{\s*"(device|name|version|ip|sid|created_at|session_created)"[^\]]*(\{[^}]*\}?)?\s*\?\s*\]/g, '[]')
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
  'content-type',
  'location',
  'vary',
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
  'x-powered-by',
] as const

// cases whose body carries genuinely-volatile session created-ms
const SKIP_ETAG = new Set(['S7.2 list json (200, sorted desc)'])

interface R {
  status: number
  h: Record<string, string>
  body: string
  setcookie: string
}

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
    h['etag'] = r.headers.get('etag') || ''
    h['content-length'] = r.headers.get('content-length') || ''
    const sc = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : r.headers.get('set-cookie') || ''
    const body = Buffer.from(await r.arrayBuffer()).toString('binary')
    return { status: r.status, h, body, setcookie: sc }
  } catch (e) {
    if (attempt < 3 && /socket|ECONNRESET|other side closed|fetch failed/i.test(String(e))) {
      await sleep(400 * (attempt + 1))
      return call(path, init, attempt + 1)
    }
    throw e
  }
}

// ---- flip plumbing (web-p3c.conf) ----------------------------------------

const FLIP_APPLY = `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/web-p3c.conf /etc/nginx/overleaf-flips/web-p3c.conf
if ! grep -q "overleaf-flips/web-p3c.conf" "$vhost"; then
  node -e '
    const fs = require("fs");
    const v = process.argv[1];
    const inc = "  include /etc/nginx/overleaf-flips/web-p3c.conf;\\n\\n";
    let s = fs.readFileSync(v, "utf8");
    if (!s.includes("overleaf-flips/web-p3c.conf")) {
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
if grep -q "overleaf-flips/web-p3c.conf" "$vhost"; then
  sed -i "/overleaf-flips\\/web-p3c.conf/d" "$vhost"
  nginx -t && nginx -s reload && sleep 2
fi
`

// ---- sessions --------------------------------------------------------------

interface Sess {
  cookie: string
  csrf: string
}

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

function mongoshScript(mongoC: string, script: string, tag: string): string {
  const f = `/tmp/p3c-${tag}.js`
  execFileSync('docker', ['exec', '-i', mongoC, 'sh', '-c', `cat > ${f}`], {
    input: script,
  })
  return f
}

function restoreFixture(mongoC: string): void {
  const f = mongoshScript(
    mongoC,
    `const u = db.users.findOne({ email: 'e2e-user@e2e.test' });
db.users.updateOne({ _id: u._id }, { $set: { first_name: 'E2e', last_name: 'User', role: 'user', institution: '', ace: { customKeybindings: {} } } });
print('restored');`,
    'restore'
  )
  dexe(mongoC, `mongosh --quiet mongodb://localhost/sharelatex ${f}`)
}

function userUids(mongoC: string): string {
  const f = mongoshScript(mongoC, `const u = db.users.findOne({ email: 'e2e-user@e2e.test' }); print(String(u._id))`, 'uids')
  return dexe(mongoC, `mongosh --quiet mongodb://localhost/sharelatex ${f}`).trim()
}

function auditCount(mongoC: string, uid: string): number {
  const f = mongoshScript(
    mongoC,
    `print(db.userAuditLogEntries.countDocuments({ userId: ObjectId('${uid}'), operation: 'clear-sessions' }))`,
    'audit'
  )
  const out = dexe(mongoC, `mongosh --quiet mongodb://localhost/sharelatex ${f}`, true)
  return parseInt(out.trim(), 10) || 0
}

async function flushSink(): Promise<number> {
  await fetch(SINK + '/api/messages', { method: 'DELETE' }).catch(() => {})
  const r = await fetch(SINK + '/api/messages')
  const env = (await r.json()) as { count: number }
  return env.count
}

async function sinkHits(): Promise<{ count: number; to: string[] }> {
  const r = await fetch(SINK + '/api/messages')
  const env = (await r.json()) as { count: number; messages: Array<{ subject?: string; to?: string | string[] }> }
  const hits = (env.messages || []).filter((m) => m.subject === SUBJECT)
  const to: string[] = []
  for (const m of hits) {
    const t = Array.isArray(m.to) ? m.to : [m.to]
    to.push(...t.filter((x) => String(x).endsWith('@e2e.test')))
  }
  return { count: env.count, to: to.sort() }
}

// ---- the battery (56 pinned cases) ----------------------------------------

const J = { 'content-type': 'application/json', accept: 'application/json' } as const
const POST = (csrf: string) => ({ ...J, 'x-csrf-token': csrf })

async function battery(me: Sess, anon: Sess): Promise<Record<string, R>> {
  const out: Record<string, R> = {}
  const put = (label: string, body: unknown) =>
    call('/user/settings', {
      method: 'POST',
      headers: POST(me.csrf),
      cookie: me.cookie,
      body: typeof body === 'string' ? body : JSON.stringify(body),
    }).then((r) => (out[label] = r))

  // S1 — anonymous authorization
  out['S1.1 settings anon html → 302'] = await call('/user/settings', {
    cookie: anon.cookie,
    headers: { accept: 'text/html' },
  })
  out['S1.1b settings anon text/plain → 302'] = await call('/user/settings', {
    cookie: anon.cookie,
    headers: { accept: 'text/plain' },
  })
  out['S1.2 list anon json → 401'] = await call('/user/sessions/list', { cookie: anon.cookie, ...J })
  out['S1.2b list anon text/plain → 302'] = await call('/user/sessions/list', {
    cookie: anon.cookie,
    headers: { accept: 'text/plain' },
  })
  out['S1.2c list anon none → 302'] = await call('/user/sessions/list', {
    cookie: anon.cookie,
    headers: { accept: '*/*' },
  })
  out['S1.3 clear anon json → 401'] = await call('/user/sessions/clear', {
    method: 'POST',
    headers: J,
    cookie: anon.cookie,
  })
  out['S1.3b clear anon text/plain → 302'] = await call('/user/sessions/clear', {
    method: 'POST',
    headers: { accept: 'text/plain', 'content-type': 'application/json' },
    cookie: anon.cookie,
  })
  out['S1.4 POST settings anon → 403 csrf'] = await call('/user/settings', {
    method: 'POST',
    headers: { ...J },
    cookie: anon.cookie,
    body: '{}',
  })
  out['S1.5 POST settings body 42 → 400 bare'] = await call('/user/settings', {
    method: 'POST',
    headers: J,
    cookie: me.cookie,
    body: '42',
  })
  out['S1.6 POST settings body null → 400 bare'] = await call('/user/settings', {
    method: 'POST',
    headers: J,
    cookie: me.cookie,
    body: 'null',
  })
  out['S1.7 POST settings body [] → 400 zod'] = await call('/user/settings', {
    method: 'POST',
    headers: J,
    cookie: me.cookie,
    body: '[]',
  })

  // S2 — pages
  out['S2.1 GET settings (200 html)'] = await call('/user/settings', {
    cookie: me.cookie,
    headers: { accept: 'text/html' },
  })
  out['S2.2 GET sessions page (200 html)'] = await call('/user/sessions', {
    cookie: me.cookie,
    headers: { accept: 'text/html' },
  })

  // S3 — no-op save
  out['S3.1 POST {} → 200 OK'] = await put('S3.1 POST {} → 200 OK', {})

  // S4 — validation matrix (multi-issue + order pins)
  await put('S4.1 unknown key', { bogus: 1 })
  await put('S4.2 two unknowns', { a: 1, b: 2 })
  await put('S4.3 mode 42', { mode: 42 })
  await put('S4.4 mode null', { mode: null })
  await put('S4.5 first_name null ok', { first_name: null })
  await put('S4.6 fontSize str', { fontSize: 'x' })
  await put('S4.7 first_name 256 (<=255 literal)', { first_name: 'x'.repeat(256) })
  await put('S4.8 autoComplete 0', { autoComplete: 0 })
  await put('S4.9 ref provider null', { zotero: null })
  await put('S4.10 zotero non-object', { zotero: 'x' })
  await put('S4.11 zotero unknown key', { zotero: { foo: true } })
  await put('S4.12 groups non-array', { zotero: { groups: 5 } })
  await put('S4.13 groups item null', { zotero: { groups: [null] } })
  await put('S4.14 groups item missing id', { zotero: { groups: [{}] } })
  await put('S4.15 groups item null id', { zotero: { groups: [{ id: null }] } })
  await put('S4.16 groups bad id', { zotero: { groups: [{ id: 5 }] } })
  await put('S4.17 kb non-object', { customKeybindings: [1] })
  await put('S4.18 kb value 42', { customKeybindings: { k: 42 } })
  await put('S4.19 kb value bool', { customKeybindings: { k: true } })
  await put('S4.20 two errors fn+email', { first_name: 'x'.repeat(300), fontSize: 'zz' })
  await put('S4.21 multi zotero (enabled+id+groups)', {
    zotero: { enabled: 'x', groups: [{ id: 5 }], m: 1 },
  })
  await put('S4.22 six coerces never reject', {
    previewTabs: 1,
    breadcrumbs: 0,
    editorTabs: 'a',
    nonBlinkingCursor: 2,
    darkModePdf: {},
    floatingMenu: 'x',
  })
  await put('S4.23 refsSearchMode weird → 200', { referencesSearchMode: 'weird' })
  await put('S4.24 groups mixed 3 + mendeley id null', {
    zotero: { groups: [{ id: 5 }, {}, { id: 'ok' }] },
    mendeley: { groups: [{ id: null }] },
  })
  await put('S4.25 zotero+kb multi-issue', { zotero: { groups: [{ id: 5 }] }, customKeybindings: { k: false } })

  // S5 — keybindings save semantics (valid inputs)
  await put('S5.1 kb 65 valid (200)', Object.fromEntries(Array.from({ length: 65 }, (_, i) => [String(i), String(i)])))
  await put('S5.2 kb null value → 200', { customKeybindings: { a: null } })
  await put(
    'S5.3 kb value >24 (200, dropped)',
    { customKeybindings: { a: 'x'.repeat(30) } }
  )

  // S6 — emails + trim
  await put('S6.1 email own (no-op 200)', { email: USER.email })
  await put('S6.2 email invalid (400)', { email: 'not-an-email' })
  await put('S6.3 email empty (400)', { email: '' })
  await put('S6.4 email taken (409)', { email: ADMIN.email })
  await put('S6.5 first_name trimmed (200)', { first_name: '  T33  ' })

  // S7 — sessions
  out['S7.1 GET sessions/list (200 array)'] = await call('/user/sessions/list', { cookie: me.cookie, ...J })
  out['S7.2 list json (200, sorted desc)'] = await call('/user/sessions/list', { cookie: me.cookie, ...J })
  out['S7.3 POST clear (201 OK)'] = await call('/user/sessions/clear', {
    method: 'POST',
    headers: POST(me.csrf),
    cookie: me.cookie,
  })
  // re-login: S7.3 cleared every session cookie for this user
  me = await login(USER)

  // S7.5 — final state restore (deterministic slate for the next leg)
  out['S7.5 final restore (200)'] = await call('/user/settings', {
    method: 'POST',
    headers: POST(me.csrf),
    cookie: me.cookie,
    body: JSON.stringify({ first_name: 'E2e', last_name: 'User', role: 'user', customKeybindings: {} }),
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
    for (const key of [...HDRS, 'content-length'] as string[]) {
      if (key === 'etag' && SKIP_ETAG.has(k)) continue
      let bv = b.h[key]
      let ov = o.h[key]
      if (key === 'etag' && bv && ov && bv !== ov) {
        const bl = (bv.match(/^W\/"([0-9a-f]+)/) || [])[1]
        const ol = (ov.match(/^W\/"([0-9a-f]+)/) || [])[1]
        if (bl && ol) {
          // compare length + content-length (hash covers volatile ms)
          bv = `len=${bl} cl=${b.h['content-length'] || ''}`
          ov = `len=${ol} cl=${o.h['content-length'] || ''}`
        }
      }
      // nonces are per-request random on both stacks — normalize before
      // comparing (CSP + any header carrying one)
      bv = (bv || '').replace(/nonce-[A-Za-z0-9+/=~]{16,}/g, 'nonce-NONCE')
      ov = (ov || '').replace(/nonce-[A-Za-z0-9+/=~]{16,}/g, 'nonce-NONCE')
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
        `${k}: body@${at}\n  base ${JSON.stringify(nb.slice(Math.max(0, at - 80), at + 160))}\n  othr ${JSON.stringify(ob.slice(Math.max(0, at - 80), at + 160))}`
      )
    }
    if (normCookie(b.setcookie) !== normCookie(o.setcookie))
      problems.push(`${k}: setcookie "${normCookie(b.setcookie).slice(0, 160)}" != "${normCookie(o.setcookie).slice(0, 160)}"`)
  }
  return problems
}

// ---- the gate ---------------------------------------------------------------

test.describe.serial('web-go P3.3 flip gate (WEB_GO_PLAN P3.3 user pages/settings)', () => {
  let overleafC = ''
  let mongoC = ''
  let uid = ''
  let leg1: Record<string, R> | null = null
  let me: Sess
  let anon: Sess
  let auditBase = 0

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    mongoC = runningContainer('ol-e2e-mongo')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, `chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web`)
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/runit/web-go-overleaf/run'), `${overleafC}:/tmp/webgo-run`])
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p3c.conf'), `${overleafC}:/tmp/web-p3c.conf`])
    dexe(
      overleafC,
      `mkdir -p /usr/local/share/overleaf-flips /etc/service/web-go-overleaf && cp /tmp/webgo-run /etc/service/web-go-overleaf/run && chmod 755 /etc/service/web-go-overleaf/run && cp /tmp/web-p3c.conf /usr/local/share/overleaf-flips/web-p3c.conf`
    )
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) {
      const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
      if (code === '200') break
      await sleep(500)
    }
    dexe(overleafC, FLIP_STRIP, true)
    await nginxSettled()
    uid = userUids(mongoC)
    expect(uid).toMatch(/^[0-9a-f]{24}$/)
    restoreFixture(mongoC)
    me = await login(USER)
    await call('/user/sessions/clear', { method: 'POST', headers: POST(me.csrf), cookie: me.cookie })
    await flushSink()
    auditBase = auditCount(mongoC, uid)
  })

  test.afterAll(async () => {
    try {
      dexe(overleafC, FLIP_STRIP, true)
      await nginxSettled()
      restoreFixture(mongoC)
    } catch {
      /* best effort */
    }
  })

  async function freshLegSess(): Promise<void> {
    const s = await login(USER)
    // deterministic slate: exactly ONE live session (the current) when
    // the battery snapshots /user/sessions/list
    await call('/user/sessions/clear', { method: 'POST', headers: POST(s.csrf), cookie: s.cookie })
    me = await login(USER)
    anon = await anonSession()
  }

  test('leg 1: Node baseline battery', async () => {
    test.setTimeout(600_000)
    dexe(overleafC, FLIP_STRIP, true)
    await nginxSettled()
    const auditBefore = auditCount(mongoC, uid)
    await freshLegSess()
    await flushSink()
    const before = await sinkHits()
    leg1 = await battery(me, anon)
    expect(Object.keys(leg1).length).toBeGreaterThanOrEqual(50)
    restoreFixture(mongoC)
    const after = await sinkHits()
    expect(after.to.length - before.to.length, 'leg1 clear-mails').toBe(1)
    expect(auditCount(mongoC, uid) - auditBefore, 'leg1 clear-audits').toBe(2)
  }, 600_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(600_000)
    dexe(overleafC, FLIP_APPLY)
    await nginxSettled()
    const auditBefore2 = auditCount(mongoC, uid)
    await freshLegSess()
    await flushSink()
    const before = await sinkHits()
    const leg2 = await battery(me, anon)
    restoreFixture(mongoC)
    const after = await sinkHits()
    expect(after.to.length - before.to.length, 'leg2 clear-mails').toBe(1)
    expect(auditCount(mongoC, uid) - auditBefore2, 'leg2 clear-audits').toBe(2)
    const problems = diffLegs(leg1!, leg2)
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 600_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(600_000)
    dexe(overleafC, FLIP_STRIP)
    await nginxSettled()
    const auditBefore3 = auditCount(mongoC, uid)
    await freshLegSess()
    await flushSink()
    const before = await sinkHits()
    const leg3 = await battery(me, anon)
    restoreFixture(mongoC)
    const after = await sinkHits()
    expect(after.to.length - before.to.length, 'leg3 clear-mails').toBe(1)
    expect(auditCount(mongoC, uid) - auditBefore3, 'leg3 clear-audits').toBe(2)
    const problems = diffLegs(leg1!, leg3)
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 600_000)

})
