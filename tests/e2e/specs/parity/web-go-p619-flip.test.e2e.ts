/**
 * P6.19 flip gate — git-bridge web module surface (Node → OlliTeX Go web).
 *
 * 5-leg contract-parity gate (same harness family as P6.5…P6.18):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery
 *   leg 2: CUMULATIVE flip (P6.4a … P6.19) → Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *   leg 4: pin sanity (oracle anchors)
 *
 * Flipped surface (nginx plane → Node `web` profile on the Node legs):
 *   GET    /oauth/token/info                        (public; PAT validation,
 *            rate limit 30/60s — `oauth-token-info`)
 *   GET    /git-bridge/personal-access-tokens       (requireLogin)
 *   POST   /git-bridge/personal-access-tokens       (requireLogin)
 *   DELETE /git-bridge/personal-access-tokens/:token_id (requireLogin)
 *
 * The docs-API half of the module lives on the Node `web-api` profile
 * (127.0.0.1:3000), which the e2e nginx plane does not route (nginx
 * `location /` → :4000; the git-bridge service talks to :3000 directly).
 * It is A/B-verified out-of-band (2026-09-20, pinned log in
 * server-ce/nginx/flips/web-p619.conf): 18-case battery zero-diff plus
 * deep-push parity (accepted / postback upToDate+latestVerId /
 * entity swap / hash-based history versions / atts snapshots). On the
 * nginx plane the docs paths fall through to Node on every leg (parity
 * by construction — not flipped).
 *
 * Pinned Node behaviors (oracle, live-captured 2026-09-20):
 *  - PAT create 200 body (key order pinned):
 *      {"_id":<24hex>,"accessToken":"olp_<36>","accessTokenPartial":"olp_<4>",
 *       "createdAt":"<ISO ms>","expiresAt":"<ISO ms = createdAt + 1y>"}
 *    mongo row stores accessToken = sha256hex(token), user_id as a string.
 *  - PAT list 200: array sorted createdAt ASC; entries
 *      {"_id","accessTokenPartial","createdAt","expiresAt","lastUsedAt?"}
 *  - PAT delete: own → 200 "OK"; again → 404 "Not Found";
 *    other user's → 404 (query includes user_id).
 *  - token/info: Bearer <valid PAT> → 200 "OK" (text/plain, no X-Powered-By
 *    for NoSession paths); Bearer <garbage> / Basic <x> / absent →
 *    401 "Unauthorized"; also sets lastUsedAt non-blocking.
 *  - PAT cap: 10 existing tokens → create 403 (MAX_PAT_COUNT=10).
 *  - anonymous PAT list → 302 /login (global chain, pinned P1);
 *    anonymous token/info → 401 (NoSession gate does not fire).
 *  - wrong method on flipped paths: nginx method guard sends to Node;
 *    Node answers (csrf 403 "Forbidden" on POST/PUT family for
 *    requireLogin paths; OPTIONS auto-response on GET paths) — both legs
 *    see the same answers (Node vs Node), pinned P6.17 pattern.
 *
 * State hygiene: the legs clean+recreate PAT rows for both fixture users;
 * the 403 cap cell seeds 10 rows and removes them inside the same leg.
 * Ephemeral values (ObjectIds, token strings, partials, timestamps) are
 * normalized before leg diffs.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync, readFileSync, existsSync } from 'node:fs'

const LEG1_PATH = '/tmp/web-go-p619-leg1.json'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
// Cumulative through P6.19.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf', 'web-p66.conf', 'web-p67.conf', 'web-p68.conf', 'web-p69.conf', 'web-p610.conf', 'web-p611.conf', 'web-p612.conf', 'web-p613.conf', 'web-p614.conf', 'web-p615.conf', 'web-p616.conf', 'web-p617.conf', 'web-p618.conf', 'web-p619.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = 'e2e-user@e2e.test'
const ADMIN = 'e2e-admin@e2e.test'
const UA = 'p619-gate'

type Leg = Record<string, { status: number; ct: string; body: string }>

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 64 * 1024 * 1024 })
}

function probe(url: string): string {
  let out = ''
  try {
    out = execFileSync('docker', ['exec', overleafC, 'sh', '-c', `curl -s -m 3 -o /dev/null -w "%{http_code}" ${url}`], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    })
  } catch (e: any) {
    out = e && e.stdout ? e.stdout.toString() : ''
  }
  return (out || '').trim().split('\n')[0]
}

async function waitGo(timeoutMs = 60000): Promise<void> {
  const t0 = Date.now()
  let last = ''
  while (Date.now() - t0 < timeoutMs) {
    last = probe('http://127.0.0.1:4010/status')
    if (last === '200') return
    await sleep(500)
  }
  throw new Error('Go :4010 never came up (last probe: ' + last + ')')
}

function flipCount(conf: string): number {
  return Number(dexeStrict(overleafC, `sh -c 'grep -c "overleaf-flips/${conf}" /etc/nginx/sites-enabled/overleaf.conf || true'`).trim()) || 0
}

async function flip(mode: 'apply' | 'strip'): Promise<void> {
  if (mode === 'strip') {
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"`)
    dexeStrict(overleafC, 'nginx -t && nginx -s reload')
    const r: any = await fetch(BASE + '/status', { headers: { 'user-agent': UA } })
    if (r.status !== 200) throw new Error('strip failed (status not 200)')
    await sleep(1200)
    for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
    return
  }
  dexeStrict(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && mkdir -p /etc/nginx/overleaf-flips')
  for (const conf of FLIPCONFS) {
    execFileSync('docker', ['cp', `${FLIPSRC}/${conf}`, `${overleafC}:/usr/local/share/overleaf-flips/${conf}`], { timeout: 30000 })
    dexeStrict(overleafC, `cp -f /usr/local/share/overleaf-flips/${conf} /etc/nginx/overleaf-flips/${conf}`)
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';let s=fs.readFileSync(p,'utf8');const inc='  include /etc/nginx/overleaf-flips/${conf};'+String.fromCharCode(10);if(!s.includes('overleaf-flips/${conf}')){if(s.includes('location / {')){s=s.replace('location / {',inc+'location / {',1)}else{throw new Error('anchor not found')}};fs.writeFileSync(p,s)"`)
  }
  dexeStrict(overleafC, 'nginx -t && nginx -s reload')
  await sleep(1200)
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(1)
}

function cleanPats(): void {
  // sh -c sees: mongosh ... --eval '<script>'  (single quotes protect the
  // regex backslashes from the container shell)
  dexeStrict(mongoC, 'mongosh --quiet sharelatex --eval \'print(db.oauthAccessTokens.deleteMany({scope: /\\bgit_bridge\\b/}).deletedCount)\'')
}

function seedPats(email: string, n: number, mode: 'node' | 'go'): void {
  // Seed n PAT rows (cap cell). BOTH stacks store user_id as a hex string
  // (Node session round-trips user._id through JSON; Go uses UserIDHex).
  void mode
  const hex = dexeStrict(mongoC, `mongosh --quiet sharelatex --eval 'const u = db.users.findOne({ email: "${email}" }, { _id: 1 }); print(u == null ? null : u._id.toString())'`).trim()
  const uidExpr = `"${hex}"`
  const lines = [
    `const now = new Date();`,
    `for (let i = 0; i < ${n}; i++) {`,
    `  db.oauthAccessTokens.insertOne({`,
    `    accessToken: "seed" + i,`,
    `    accessTokenPartial: "olp_SEED" + i,`,
    `    user_id: ${uidExpr},`,
    `    type: "personal_access_token",`,
    `    scope: "git_bridge",`,
    `    createdAt: new Date(now.getTime() - (${n} - i) * 1000),`,
    `    expiresAt: new Date(now.getTime() + 86400000 * 365),`,
    `  });`,
    `}`,
  ]
  dexeStrict(mongoC, `mkdir -p /tmp/p619 && cat > /tmp/p619/seedpats.js << 'SEED_EOF'
${lines.join('\n')}
SEED_EOF
`)
  dexeStrict(mongoC, 'mongosh --quiet sharelatex /tmp/p619/seedpats.js')
}

function seedCap(mode: 'node' | 'go'): number {
  seedPats('e2e-user@e2e.test', 10, mode)
  const n = Number(dexeStrict(mongoC, "mongosh --quiet sharelatex --eval 'print(db.oauthAccessTokens.countDocuments({accessToken: /^seed/, scope: \"git_bridge\"}))'"))
  if (n !== 10) throw new Error('seedCap expected 10 rows, got ' + n + ' (mode=' + mode + ')')
  return n
}

function normBody(b: string): string {
  return b
    .replace(/overleaf\.sid=s%3A[^;,\s"\\]+/g, 'overleaf.sid=SID.')
    .replace(/(olp|olp_[A-Za-z0-9]{0,4})[A-Za-z0-9]{4,40}/g, 'TOK')
    .replace(/\b[0-9a-f]{24}\b/g, 'ID24')
    .replace(/\b(20\d{2}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\b/g, 'TS')
    .replace(/<meta name="ol-csrfToken" content="[^"]*"/g, '<meta name="ol-csrfToken" content="CSRF"')
    .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
    .replace(/ol-csrfToken[=: \t]*[^ \t>]{8,}/g, 'ol-csrfToken=CSRF')
    .replace(/nonce="[^"]*"/g, 'nonce="NONCE"')
    .replace(/nonce='[^']*'/g, "nonce='NONCE'")
    .replace(/nonce-?"[^"]*"/g, 'nonce="NONCE"')
    .replace(/nonce-[A-Za-z0-9+/=~]{8,}/g, 'nonce-NONCE')
    .replace(/src="[^"?]+\?[^"]*"/g, 'src="ASSET"')
    .replace(/href="[^"?#]+\.[a-z]+\?[^"]*"/gi, 'href="ASSET"')
}

function normCreate(b: string): string {
  // create response: keep the key ORDER (parity requirement), mask values
  return normBody(b)
    .replace(/"accessToken":"[^"]*"/g, '"accessToken":"TOK"')
    .replace(/"accessTokenPartial":"[^"]*"/g, '"accessTokenPartial":"TOK"')
    .replace(/"createdAt":"[^"]*"/g, '"createdAt":"TS"')
    .replace(/"expiresAt":"[^"]*"/g, '"expiresAt":"TS"')
}

function normList(b: string): string {
  // list: entries sorted createdAt ASC; mask per-entry values, keep order
  const arr = b
    .replace(/"accessTokenPartial":"[^"]*"/g, '"accessTokenPartial":"TOK"')
    .replace(/"createdAt":"[^"]*"/g, '"createdAt":"TS"')
    .replace(/"expiresAt":"[^"]*"/g, '"expiresAt":"TS"')
    .replace(/"lastUsedAt":"[^"]*"/g, '"lastUsedAt":"TS"')
    .replace(/\b[0-9a-f]{24}\b/g, 'ID24')
  // seeded cap rows (identical on both legs already) — normalize anyway
  return arr.replace(/"olp_SEED\d+"/g, '"TOK"')
}

function diffLegs(label: string, A: Leg, B: Leg): string[] {
  const ds: string[] = []
  const ks = [...new Set([...Object.keys(A), ...Object.keys(B)])].sort()
  for (const k of ks) {
    const a = A[k]
    const b = B[k]
    if (!a && !b) continue
    if (!a || !b) {
      ds.push(`${label}:${k} present-on-one-side`)
      continue
    }
    if (a.status !== b.status) ds.push(`${label}:${k} status A=${a.status} B=${b.status}`)
    if (a.ct !== b.ct) ds.push(`${label}:${k} ct A='${a.ct}' B='${b.ct}'`)
    const an = normBody(a.body)
    const bn = normBody(b.body)
    if (an !== bn) {
      ds.push(`${label}:${k} body A~${an.slice(0, 160)} || B~${bn.slice(0, 160)}`)
    }
  }
  return ds
}

async function login(email: string, pw: string): Promise<string> {
  let lastErr: any = null
  for (let attempt = 0; attempt < 8; attempt++) {
    try {
      const r0: any = await fetch(BASE + '/login', { headers: { 'user-agent': UA }, redirect: 'manual' })
      const html = await r0.text()
      const csrf = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
      const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
      const r1: any = await fetch(BASE + '/login', {
        method: 'POST',
        headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json', cookie: ck0, 'user-agent': UA },
        body: JSON.stringify({ username: email, email, password: pw }),
        redirect: 'manual',
      })
      const body1 = await r1.text()
      if (r1.status === 502 || r1.status === 503 || r1.status === 504) {
        lastErr = new Error('drain window ' + r1.status)
        await sleep(1500)
        continue
      }
      if (r1.status !== 200) throw new Error('login failed ' + r1.status + ' ' + body1.slice(0, 200))
      return (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
    } catch (e: any) {
      lastErr = e
      await sleep(1500)
    }
  }
  if (lastErr instanceof Error) throw lastErr
  throw new Error('login failed (retries exhausted)')
}

let leg1: Leg | null = null

function leg1Load(): Leg {
  if (leg1) return leg1
  if (existsSync(LEG1_PATH)) {
    return JSON.parse(readFileSync(LEG1_PATH, 'utf8')) as Leg
  }
  throw new Error('leg1 baseline missing (leg 1 did not record it)')
}

/**
 * Battery. State-self-contained per leg: cleanPats at start; PAT
 * create/list/delete/cap all created+removed within the leg.
 */
async function runLeg(mode: 'node' | 'go'): Promise<Leg> {
  cleanPats()
  const U = await login('e2e-user@e2e.test', 'Ol-Fixture-3m2Q')
  const D = await login('e2e-admin@e2e.test', 'Ol-Fixture-9x7K')
  const AN = { 'user-agent': UA }

  async function call(path: string, headers: Record<string, string> = {}, method = 'GET'): Promise<{ status: number; ct: string; body: string }> {
    const r: any = await fetch(BASE + path, { headers, method, redirect: 'manual' })
    const text = await r.text()
    return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], body: text }
  }

  const TI = '/oauth/token/info'
  const PL = '/git-bridge/personal-access-tokens'
  const pins: Leg = {}

  // ---- anonymous (global chain) ----------
  pins.a_pl = await call(PL, AN)          // 302 /login
  pins.a_ti = await call(TI, AN)          // 401 Unauthorized
  pins.a_ti_basic = await call(TI, { ...AN, authorization: 'Basic abcdef' }) // 401
  pins.a_pl_post = await call(PL, AN, 'POST') // login gate fires first (anon has no csrf)

  // ---- user: empty list, create, list, token/info, delete --------------
  const UH = { cookie: U, 'user-agent': UA, 'content-type': 'application/json' }
  pins.u_pl_empty = await call(PL, { cookie: U, 'user-agent': UA }) // []
  let create: { status: number; ct: string; body: string }
  let csrfU = ''
  {
    const page = await (await fetch(BASE + '/user/list', { headers: { cookie: U } })).text()
    csrfU = (page.match(/ol-csrfToken" content="([^"]+)"/) || [])[1] || ''
  }
  create = await call(PL, { ...UH, 'x-csrf-token': csrfU }, 'POST')
  pins.u_create = create
  const created = JSON.parse(create.body || '{}') || {}
  const tok = created.accessToken || ''
  const tid = created._id || ''

  pins.u_pl_one = await call(PL, { cookie: U, 'user-agent': UA })
  pins.u_ti_ok = await call(TI, { ...AN, authorization: 'Bearer ' + tok })
  pins.u_ti_bad = await call(TI, { ...AN, authorization: 'Bearer olp_wrongwrongwrongwrongwrongwrongwrong' })
  pins.u_ti_nobearer = await call(TI, { ...AN, authorization: 'Token abc' })

  // cross-user delete: admin tries to delete the user's token (admin's own csrf)
  let csrfD = ''
  {
    const page = await (await fetch(BASE + '/user/list', { headers: { cookie: D } })).text()
    csrfD = (page.match(/ol-csrfToken" content="([^"]+)"/) || [])[1] || ''
  }
  pins.u_cross_del = await call(PL + '/' + tid, { cookie: D, 'user-agent': UA, 'x-csrf-token': csrfD }, 'DELETE')

  // own delete → 200; again → 404
  pins.u_del = await call(PL + '/' + tid, { cookie: U, 'user-agent': UA, 'x-csrf-token': csrfU }, 'DELETE')
  pins.u_del_again = await call(PL + '/' + tid, { cookie: U, 'user-agent': UA, 'x-csrf-token': csrfU }, 'DELETE')
  pins.u_pl_empty2 = await call(PL, { cookie: U, 'user-agent': UA })

  // ---- cap cell: seed 10 (stack-typed) → create 403 → unseed ----------
  seedCap(mode)
  pins.u_cap_pl = await call(PL, { cookie: U, 'user-agent': UA })      // 10 seeded entries
  pins.u_cap_create = await call(PL, { ...UH, 'x-csrf-token': csrfU }, 'POST') // 403
  cleanPats()

  // ---- method-guard fall-throughs (Node answers on every leg) ------------
  pins.v_ti_post = await call(TI, AN, 'POST')
  pins.v_pl_put = await call(PL, AN, 'PUT')
  pins.v_del_get = await call(PL + 'deadbeefdeadbeefdeadbeef', { cookie: U, 'user-agent': UA, 'x-csrf-token': csrfU }, 'GET')
  pins.v_del_badid = await call(PL + '/not-an-id', { cookie: U, 'user-agent': UA, 'x-csrf-token': csrfU }, 'DELETE') // Node answers (route regex miss → location /)

  return pins
}

test('leg 0: flip off before start (force-strip any leftovers)', async () => {
  try {
    await flip('strip')
  } catch {
    /* best effort */
  }
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
}, 120_000)

test('leg 1: Node baseline battery', async () => {
  leg1 = await runLeg('node')
  const n = Object.keys(leg1!).length
  expect(n).toBe(20)
  writeFileSync(LEG1_PATH, JSON.stringify(leg1))
}, 300_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  const B = await runLeg('go')
  const ds = diffLegs('p619', leg1Load(), B)
  expect(ds, ds.join('\n')).toEqual([])
}, 300_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  const C = await runLeg('node')
  const ds = diffLegs('p619', leg1Load(), C)
  expect(ds, ds.join('\n')).toEqual([])
}, 300_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1Load()
  // anonymous
  expect(L.a_pl.status).toBe(302)
  expect(L.a_pl.body).toBe('Found. Redirecting to /login')
  expect(L.a_ti.status).toBe(401)
  expect(L.a_ti.body).toBe('Unauthorized')
  expect(L.a_ti_basic.status).toBe(401)
  expect(L.a_ti_basic.body).toBe('Unauthorized')
  // user: empty list shape
  expect(L.u_pl_empty.status).toBe(200)
  expect(L.u_pl_empty.ct).toBe('application/json')
  expect(L.u_pl_empty.body).toBe('[]')
  // create: key order pinned (values masked by norm)
  expect(L.u_create.status).toBe(200)
  expect(L.u_create.ct).toBe('application/json')
  expect(normCreate(L.u_create.body)).toBe('{"_id":"ID24","accessToken":"TOK","accessTokenPartial":"TOK","createdAt":"TS","expiresAt":"TS"}')
  // list with one entry: entry key order pinned
  expect(L.u_pl_one.status).toBe(200)
  expect(normList(L.u_pl_one.body).startsWith('[{"_id":"ID24","accessTokenPartial":"TOK","createdAt":"TS","expiresAt":"TS"')).toBe(true)
  // token/info happy + unhappy
  expect(L.u_ti_ok.status).toBe(200)
  expect(L.u_ti_ok.body).toBe('OK')
  expect(L.u_ti_bad.status).toBe(401)
  expect(L.u_ti_bad.body).toBe('Unauthorized')
  expect(L.u_ti_nobearer.status).toBe(401)
  // cross-user delete blocked (user_id in the query)
  expect(L.u_cross_del.status).toBe(404)
  expect(L.u_cross_del.body).toBe('Not Found')
  // own delete lifecycle
  expect(L.u_del.status).toBe(200)
  expect(L.u_del.body).toBe('OK')
  expect(L.u_del_again.status).toBe(404)
  expect(L.u_del_again.body).toBe('Not Found')
  expect(L.u_pl_empty2.body).toBe('[]')
  // cap
  expect(L.u_cap_pl.status).toBe(200)
  expect(JSON.parse(L.u_cap_pl.body).length).toBe(10)
  expect(L.u_cap_create.status).toBe(403)
  // method-guard fall-throughs (Node answers on every leg)
  expect(L.v_ti_post.status).toBe(403)
  expect(L.v_ti_post.body).toBe('Forbidden')
  expect(L.v_pl_put.status).toBe(403)
  expect(L.v_pl_put.body).toBe('Forbidden')
  expect(L.v_del_get.status).toBe(404) // GET with token_id param → Node falls to app 404 (route is DELETE only)
}, 120_000)
