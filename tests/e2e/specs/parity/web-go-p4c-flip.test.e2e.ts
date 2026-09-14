/**
 * WEB-GO P4.3 FLIP GATE (WEB_GO_PLAN.md P4.3 — project members route):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p4c.conf — regex location
 *   matching one dynamic path segment, mirroring Node's `:Project_id`):
 *     GET /project/:Project_id/members
 *       → requireLogin → (blockRestrictedUserFromProject) → ensureUserCanReadProject
 *       → { members: [ {_id, first_name, last_name, email, privileges,
 *                       signUpDate[, pendingEditor[, pendingReviewer]]} ] }
 *
 *   leg 1  Node baseline — the battery via nginx (flip OFF)
 *   leg 2  FLIP ON       — identical battery on Go
 *   leg 3  FLIP OFF      — Node again (reversal)
 *
 * Contract pins (live-oracle 2026-09-14; A/B byte-identical 8/8):
 *   - logged-in, owned / member, valid id → 200 application/json
 *       { members:[…] }; member key order _id, first_name, last_name, email,
 *         privileges, signUpDate[, pendingEditor[, pendingReviewer]];
 *         privileges ∈ {readAndWrite, review, readOnly} (NEVER "owner");
 *         order = collaborators → reviewers → readOnly (array order, no sort);
 *         the OWNER and TOKEN members are OMITTED.
 *   - valid id, project absent            → 404 general/404 (text/html, NOT accept-dep)
 *   - INVALID (non-hex) id                → 404 application/json  (NOT accept-dep)
 *       {"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}
 *   - present, no read access             → 403  json {"message":"restricted"} | html Restricted
 *   - anon accept json → 401 ; anon accept html → 302 Location /login
 *
 * Normalization: the 403 HTML view embeds a per-render CSP NONCE and a salted
 * RANDOM csrf token (never byte-comparable across services) — normalized
 * before the body compare. JSON bodies and their ETags are byte-compared.
 *
 * State discipline: beforeAll creates three idempotent fixture projects and
 * captures their ids; the battery is otherwise read-only. Each leg
 * re-authenticates its own session. Afterwards the stack is left node-active.
 *
 * Run: npx playwright test -g "web-go P4.3 flip gate"
 */
import { execFileSync } from 'child_process'
import { test, expect } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

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

// ---- flip plumbing (web-p4c.conf) ----------------------------------------
function FLIP(cmd: 'apply' | 'strip'): string {
  const conf = 'web-p4c.conf'
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

// ---- normalization -------------------------------------------------------
function normHtml(s: string): string {
  return s
    .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
    .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
    .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
    .replace(/https?:\/\/[0-9a-z.-]+:[0-9]+/gi, 'SITE')
}

// ---- battery -------------------------------------------------------------
interface Leg {
  owned: R       // 200 (USER is owner; member = ADMIN readAndWrite)
  collab: R      // 200 (USER is a readOnly member of ADMIN's project)
  naJson: R      // 403 application/json
  naHtml: R      // 403 text/html
  unkHtml: R     // 404 general/404 (text/html)
  malJson: R     // 404 application/json (malformed id)
  anonJson: R    // 401
  anonHtml: R    // 302 /login
}
async function battery(own: string, col: string, na: string): Promise<Leg> {
  const user = await login(USER)
  const UNKNOWN = 'ffffffffffffffffffffffff'
  return {
    owned: await call(`/project/${own}/members`, { cookie: user, headers: { accept: 'application/json' } }),
    collab: await call(`/project/${col}/members`, { cookie: user, headers: { accept: 'application/json' } }),
    naJson: await call(`/project/${na}/members`, { cookie: user, headers: { accept: 'application/json' } }),
    naHtml: await call(`/project/${na}/members`, { cookie: user, headers: { accept: 'text/html' } }),
    unkHtml: await call(`/project/${UNKNOWN}/members`, { cookie: user, headers: { accept: 'text/html' } }),
    malJson: await call(`/project/not-a-valid-objectid/members`, { cookie: user, headers: { accept: 'application/json' } }),
    anonJson: await call(`/project/${own}/members`, { headers: { accept: 'application/json' } }),
    anonHtml: await call(`/project/${own}/members`, { headers: { accept: 'text/html' } }),
  }
}

function cmpCase(a: R, b: R, key: string, html: boolean, problems: string[]): void {
  if (a.status !== b.status) { problems.push(`${key}: status ${a.status} vs ${b.status}`); return }
  if ((a.h['content-type'] || '') !== (b.h['content-type'] || ''))
    problems.push(`${key}: content-type ${a.h['content-type']} vs ${b.h['content-type']}`)
  if ((a.h.location || '') !== (b.h.location || ''))
    problems.push(`${key}: location ${a.h.location} vs ${b.h.location}`)
  const ba = html ? normHtml(a.body) : a.body
  const bb = html ? normHtml(b.body) : b.body
  if (ba !== bb) problems.push(`${key}: body differs\n  A=${ba.slice(0, 200)}\n  B=${bb.slice(0, 200)}`)
  if (!html && (a.h.etag || '') !== (b.h.etag || ''))
    problems.push(`${key}: etag ${a.h.etag} vs ${b.h.etag}`)
}

function diffLegs(a: Leg, b: Leg, tag: string): string[] {
  const problems: string[] = []
  cmpCase(a.owned, b.owned, 'owned(200)', false, problems)
  cmpCase(a.collab, b.collab, 'collab(200)', false, problems)
  cmpCase(a.naJson, b.naJson, 'noaccess(403-json)', false, problems)
  cmpCase(a.naHtml, b.naHtml, 'noaccess(403-html)', true, problems)
  cmpCase(a.unkHtml, b.unkHtml, 'unknown(404-html)', true, problems)
  cmpCase(a.malJson, b.malJson, 'malformed(404-json)', false, problems)
  cmpCase(a.anonJson, b.anonJson, 'anon(401)', false, problems)
  cmpCase(a.anonHtml, b.anonHtml, 'anon(302)', false, problems)
  return problems.map((p) => `${tag}: ${p}`)
}

// ---- fixtures -----------------------------------------------------------
const USER_OID = '6aa4b8b573ef0e5094f4cbc0'
const ADMIN_OID = '6aa4b8a873ef0e5094f4cba3'
function createFixtures(mongoC: string): { own: string; col: string; na: string } {
  const script = `
const user=new ObjectId("${USER_OID}"), admin=new ObjectId("${ADMIN_OID}");
db.projects.deleteMany({name:{$in:["webgo-p4c-own","webgo-p4c-col","webgo-p4c-na"]}});
const own=db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4c-own",owner_ref:user,collaberator_refs:[admin],publicAccesLevel:"private",
  rootFolder:[{name:"",docs:[{_id:new ObjectId(),name:"main.tex"}],fileRefs:[],folders:[]}]});
const col=db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4c-col",owner_ref:admin,readOnly_refs:[user],publicAccesLevel:"private",
  rootFolder:[{name:"",docs:[{_id:new ObjectId(),name:"main.tex"}],fileRefs:[],folders:[]}]});
const na=db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4c-na",owner_ref:admin,publicAccesLevel:"private",
  rootFolder:[{name:"",docs:[{_id:new ObjectId(),name:"main.tex"}],fileRefs:[],folders:[]}]});
print("OWN:"+own.insertedId.toString());
print("COL:"+col.insertedId.toString());
print("NA:"+na.insertedId.toString());
`
  const out = dexe(mongoC, `mongosh --quiet sharelatex --eval '${script.replace(/'/g, `'\''`)}'`)
  const own = (out.match(/OWN:([0-9a-f]{24})/) || [])[1]
  const col = (out.match(/COL:([0-9a-f]{24})/) || [])[1]
  const na = (out.match(/NA:([0-9a-f]{24})/) || [])[1]
  if (!own || !col || !na) throw new Error('fixture creation failed: ' + out)
  return { own, col, na }
}

// ---- the gate -----------------------------------------------------------
test.describe.serial('web-go P4.3 flip gate (WEB_GO_PLAN P4.3 project members)', () => {
  let overleafC = ''
  let mongoC = ''
  let own = ''
  let col = ''
  let na = ''
  let leg1: Leg | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    mongoC = runningContainer('ol-e2e-mongo')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p4c.conf'), `${overleafC}:/tmp/web-p4c.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p4c.conf /usr/local/share/overleaf-flips/web-p4c.conf')
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) {
      const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
      if (code === '200') break
      await sleep(500)
    }
    const fx = createFixtures(mongoC)
    own = fx.own; col = fx.col; na = fx.na
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
    leg1 = await battery(own, col, na)
    // pins (Node is the authoritative oracle)
    expect(leg1.owned.status).toBe(200)
    const jo = JSON.parse(leg1.owned.body)
    expect(Object.keys(jo)).toEqual(['members'])
    expect(Array.isArray(jo.members), 'members array').toBe(true)
    expect(jo.members.length, 'owned member list (owner excluded)').toBe(1)
    expect(Object.keys(jo.members[0])).toEqual(['_id', 'first_name', 'last_name', 'email', 'privileges', 'signUpDate'])
    expect(jo.members[0]._id).toBe(ADMIN_OID)
    expect(jo.members[0].privileges).toBe('readAndWrite')
    expect(new Date(jo.members[0].signUpDate).toISOString(), 'signUpDate ISO').toContain('T')

    const jc = JSON.parse(leg1.collab.body)
    expect(jc.members.length, 'collab member list').toBe(1)
    expect(jc.members[0].privileges).toBe('readOnly')

    expect(leg1.naJson.status).toBe(403)
    expect(leg1.naJson.body).toBe('{"message":"restricted"}')
    expect(leg1.naHtml.status).toBe(403)
    expect(leg1.unkHtml.status).toBe(404)
    expect(leg1.unkHtml.h['content-type']).toContain('text/html')
    expect(leg1.malJson.status).toBe(404)
    expect(leg1.malJson.body).toBe('{"error":"Validation error: Invalid Mongo ObjectId at \\"params.Project_id\\"","statusCode":404}')
    expect(leg1.anonJson.status).toBe(401)
    expect(leg1.anonHtml.status).toBe(302)
    expect(leg1.anonHtml.h.location).toBe('/login')
  }, 180_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('apply'))
    await nginxSettled()
    const leg2 = await battery(own, col, na)
    const problems = diffLegs(leg1!, leg2, 'GO')
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 180_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('strip'))
    await nginxSettled()
    const leg3 = await battery(own, col, na)
    const problems = diffLegs(leg1!, leg3, 'NODE-rev')
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 180_000)
})
