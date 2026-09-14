/**
 * WEB-GO P4.2 FLIP GATE (WEB_GO_PLAN.md P4.2 — project entities route):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p4b.conf — regex location
 *   matching one dynamic path segment, mirroring Node's `:Project_id`):
 *     GET /project/:Project_id/entities
 *       → requireLogin → ensureUserCanReadProject →
 *         { project_id, entities:[{path,type}] }   (path asc)
 *
 *   leg 1  Node baseline — the battery via nginx (flip OFF)
 *   leg 2  FLIP ON       — identical battery on Go
 *   leg 3  FLIP OFF      — Node again (reversal)
 *
 * Contract pins (live-oracle 2026-09-14; A/B byte-identical 10/10):
 *   - logged-in, owned, valid id       → 200 application/json
 *       { project_id, entities:[{path,type:"doc"|"file"}] } sorted by path asc.
 *   - logged-in, valid id, NO access   → 403
 *       accept json → {"message":"restricted"}   (application/json)
 *       accept html → the Restricted view        (text/html)
 *   - logged-in, valid id, project absent → 404 general/404 (text/html)
 *       (NOT accept-dependent)
 *   - logged-in, INVALID object id     → 404 application/json
 *       {"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}
 *       (NOT accept-dependent)
 *   - anon accept json → 401 ; anon accept html → 302 Location /login
 *
 * Normalization: the 403/404 HTML views embed a per-render CSP NONCE and a
 * salted RANDOM csrf token (node and go both derive `salt + SHA1(salt+
 * csrfSecret)`, so they can never match byte-for-byte across services). Both
 * are normalized before the body compare. JSON bodies (200/403-json/404-json)
 * and the ETag (deterministic for the JSON bodies) are compared byte-for-byte.
 *
 * State discipline: beforeAll creates two idempotent fixture projects (an
 * owned multi-entity project for USER, and a no-access project for ADMIN)
 * and captures their ids; the battery is otherwise read-only. Each leg
 * re-authenticates its own session (login rate-limit 10/120s × 1 user × 3
 * legs is comfortably within budget). Afterwards the stack is left
 * node-active (e2e convention).
 *
 * Run: npx playwright test -g "web-go P4.2 flip gate"
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

// ---- flip plumbing (web-p4b.conf) ----------------------------------------
function FLIP(cmd: 'apply' | 'strip'): string {
  const conf = 'web-p4b.conf'
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
// The 403/404 HTML views carry a random per-render CSP nonce + a salted
// random csrf token (ol-csrfToken meta + the _csrf form value) — normalize
// both (and the site origin) so the remaining bytes are compared.
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
  owned: R       // 200 owned multi-entity (USER)
  naJson: R      // 403 application/json
  naHtml: R      // 403 text/html
  unkHtml: R     // 404 general/404 (text/html)
  malJson: R     // 404 application/json (malformed id)
  anonJson: R    // 401
  anonHtml: R    // 302 /login
}
async function battery(ownedId: string, noaccessId: string): Promise<Leg> {
  const user = await login(USER)
  const UNKNOWN = 'ffffffffffffffffffffffff' // valid ObjectId, absent in db
  return {
    owned: await call(`/project/${ownedId}/entities`, { cookie: user, headers: { accept: 'application/json' } }),
    naJson: await call(`/project/${noaccessId}/entities`, { cookie: user, headers: { accept: 'application/json' } }),
    naHtml: await call(`/project/${noaccessId}/entities`, { cookie: user, headers: { accept: 'text/html' } }),
    unkHtml: await call(`/project/${UNKNOWN}/entities`, { cookie: user, headers: { accept: 'text/html' } }),
    malJson: await call(`/project/not-a-valid-objectid/entities`, { cookie: user, headers: { accept: 'application/json' } }),
    anonJson: await call(`/project/${ownedId}/entities`, { headers: { accept: 'application/json' } }),
    anonHtml: await call(`/project/${ownedId}/entities`, { headers: { accept: 'text/html' } }),
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
  if (ba !== bb) problems.push(`${key}: body differs\n  A=${ba.slice(0, 240)}\n  B=${bb.slice(0, 240)}`)
  if (!html && (a.h.etag || '') !== (b.h.etag || ''))
    problems.push(`${key}: etag ${a.h.etag} vs ${b.h.etag}`)
}

function diffLegs(a: Leg, b: Leg, tag: string): string[] {
  const problems: string[] = []
  cmpCase(a.owned, b.owned, 'owned(200)', false, problems)
  cmpCase(a.naJson, b.naJson, 'noaccess(403-json)', false, problems)
  cmpCase(a.naHtml, b.naHtml, 'noaccess(403-html)', true, problems)
  cmpCase(a.unkHtml, b.unkHtml, 'unknown(404-html)', true, problems)
  cmpCase(a.malJson, b.malJson, 'malformed(404-json)', false, problems)
  cmpCase(a.anonJson, b.anonJson, 'anon(401)', false, problems)
  cmpCase(a.anonHtml, b.anonHtml, 'anon(302)', false, problems)
  for (const p of problems) problems // eslint-disable-line no-empty
  return problems.map((p) => `${tag}: ${p}`)
}

// ---- fixtures ------------------------------------------------------------
// Create two idempotent fixture projects and return {ownedId, noaccessId}.
const USER_OID = '6aa4b8b573ef0e5094f4cbc0'
const ADMIN_OID = '6aa4b8a873ef0e5094f4cba3'
function createFixtures(mongoC: string): { ownedId: string; noaccessId: string } {
  const script = `
const user=new ObjectId("${USER_OID}"), admin=new ObjectId("${ADMIN_OID}");
db.projects.deleteMany({name:"webgo-p4b-om"});
db.projects.deleteMany({name:"webgo-p4b-na"});
const om=db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4b-om",owner_ref:user,publicAccesLevel:"private",
  rootFolder:[{name:"",
    docs:[{name:"b.tex",_id:new ObjectId()},{name:"a.tex",_id:new ObjectId()}],
    fileRefs:[{name:"z.png",_id:new ObjectId()}],
    folders:[{name:"sub",docs:[{name:"c.tex",_id:new ObjectId()}],
             fileRefs:[{name:"y.png",_id:new ObjectId()},{name:"x.png",_id:new ObjectId()}],folders:[]}]}]});
const na=db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4b-na",owner_ref:admin,publicAccesLevel:"private",
  rootFolder:[{name:"",docs:[{name:"main.tex",_id:new ObjectId()}],fileRefs:[],folders:[]}]});
print("OWNED:"+om.insertedId.toString());
print("NOACCESS:"+na.insertedId.toString());
`
  const out = dexe(mongoC, `mongosh --quiet sharelatex --eval '${script.replace(/'/g, `'\''`)}'`)
  const owned = (out.match(/OWNED:([0-9a-f]{24})/) || [])[1]
  const noaccess = (out.match(/NOACCESS:([0-9a-f]{24})/) || [])[1]
  if (!owned || !noaccess) throw new Error('fixture creation failed: ' + out)
  return { ownedId: owned, noaccessId: noaccess }
}

// ---- the gate ------------------------------------------------------------
test.describe.serial('web-go P4.2 flip gate (WEB_GO_PLAN P4.2 project entities)', () => {
  let overleafC = ''
  let mongoC = ''
  let ownedId = ''
  let noaccessId = ''
  let leg1: Leg | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    mongoC = runningContainer('ol-e2e-mongo')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p4b.conf'), `${overleafC}:/tmp/web-p4b.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p4b.conf /usr/local/share/overleaf-flips/web-p4b.conf')
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) {
      const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
      if (code === '200') break
      await sleep(500)
    }
    const fx = createFixtures(mongoC)
    ownedId = fx.ownedId
    noaccessId = fx.noaccessId
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
    leg1 = await battery(ownedId, noaccessId)
    // pins (Node is the authoritative oracle)
    expect(leg1.owned.status).toBe(200)
    const j = JSON.parse(leg1.owned.body)
    expect(Object.keys(j)).toEqual(['project_id', 'entities'])
    expect(j.project_id).toBe(ownedId)
    expect(Array.isArray(j.entities), 'entities array').toBe(true)
    expect(j.entities.length, 'multi-entity fixture').toBe(6)
    for (const e of j.entities) expect(Object.keys(e)).toEqual(['path', 'type'])
    const paths = j.entities.map((e: { path: string }) => e.path)
    expect(paths, 'sorted by path asc').toEqual([...paths].sort())
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
    const leg2 = await battery(ownedId, noaccessId)
    const problems = diffLegs(leg1!, leg2, 'GO')
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 180_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('strip'))
    await nginxSettled()
    const leg3 = await battery(ownedId, noaccessId)
    const problems = diffLegs(leg1!, leg3, 'NODE-rev')
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 180_000)
})
