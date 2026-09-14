/**
 * WEB-GO P4.4 FLIP GATE (WEB_GO_PLAN.md P4.4 — project access-requests route):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p4d.conf — regex location
 *   matching one dynamic segment + the literal "access-requests"):
 *     GET /project/:Project_id/access-requests
 *       → requireLogin → (ensureUserCanAdminProject: OWNER or site-admin w/
 *         'modify-project-setting'; ADMIN_PRIVILEGE_AVAILABLE=true → owner||isAdmin)
 *       → { editAccessRequests: [ {_id, email, first_name, last_name,
 *                                  privilegeLevel, currentPrivilegeLevel,
 *                                  requestedAt} ] }
 *
 *   leg 1  Node baseline — the battery via nginx (flip OFF)
 *   leg 2  FLIP ON       — identical battery on Go
 *   leg 3  FLIP OFF      — Node again (reversal)
 *
 * Contract pins (live-oracle 2026-09-14; A/B byte-identical 7/7):
 *   - owner / site-admin, present project → 200 application/json
 *       { editAccessRequests: [ … ] }   (empty → []; key order per row:
 *         _id, email, first_name, last_name, privilegeLevel,
 *         currentPrivilegeLevel, requestedAt)
 *         privilegeLevel        = the level the user REQUESTED
 *         currentPrivilegeLevel = their CURRENT level, or FALSE (NONE) when
 *                                 they are not a member
 *         requestedAt           = ISO-8601 string (Node JSON.stringify(Date))
 *         order = project.editAccessRequests array order (no sort/dedup);
 *         rows whose user doc is gone are dropped.
 *   - valid id, project absent            → 404 general/404 (text/html, NOT accept-dep)
 *   - INVALID (non-hex) id                → 404 application/json  (NOT accept-dep)
 *       {"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}
 *   - anon accept json → 401 ; anon accept html → 302 Location /login
 *   - (not admin → 403 restricted; not A/B-able here: only two e2e users exist,
 *     one owner-ish & one site-admin, so both reach the 200/404 paths)
 *
 * Normalization: the 404 HTML view embeds a per-render CSP NONCE, a salted
 * RANDOM csrf token, and the site origin — normalized before the body compare.
 * JSON bodies and ETags are byte-compared.
 *
 * Run: npx playwright test -g "web-go P4.4 flip gate"
 */
import { execFileSync } from 'child_process'
import { test, expect } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

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

function FLIP(cmd: 'apply' | 'strip'): string {
  const conf = 'web-p4d.conf'
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
    method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' },
    cookie: ck, body: JSON.stringify(who),
  })
  expect(r.status, `login ${who.email}`).toBe(200)
  const lines = r.setcookie.split('\n').filter((l) => l.includes('overleaf.sid'))
  return lines[lines.length - 1] || ck
}

function normHtml(s: string): string {
  return s
    .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
    .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
    .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
    .replace(/https?:\/\/[0-9a-z.-]+:[0-9]+/gi, 'SITE')
}

interface Leg {
  withReq: R       // 200 (owner USER; ADMIN requested readAndWrite, not a member)
  adminReq: R      // 200 (site-admin ADMIN; same body)
  empty: R         // 200 {editAccessRequests:[]}'
  unkHtml: R       // 404 general/404 (text/html)
  malJson: R       // 404 application/json (malformed id)
  anonJson: R      // 401
  anonHtml: R      // 302 /login
}
async function battery(req: string, empty: string): Promise<Leg> {
  const user = await login(USER)
  const admin = await login(ADMIN)
  const UNKNOWN = 'ffffffffffffffffffffffff'
  return {
    withReq: await call(`/project/${req}/access-requests`, { cookie: user, headers: { accept: 'application/json' } }),
    adminReq: await call(`/project/${req}/access-requests`, { cookie: admin, headers: { accept: 'application/json' } }),
    empty: await call(`/project/${empty}/access-requests`, { cookie: user, headers: { accept: 'application/json' } }),
    unkHtml: await call(`/project/${UNKNOWN}/access-requests`, { cookie: user, headers: { accept: 'text/html' } }),
    malJson: await call(`/project/not-a-valid-objectid/access-requests`, { cookie: user, headers: { accept: 'application/json' } }),
    anonJson: await call(`/project/${req}/access-requests`, { headers: { accept: 'application/json' } }),
    anonHtml: await call(`/project/${req}/access-requests`, { headers: { accept: 'text/html' } }),
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
  cmpCase(a.withReq, b.withReq, 'withReq(200)', false, problems)
  cmpCase(a.adminReq, b.adminReq, 'adminReq(200)', false, problems)
  cmpCase(a.empty, b.empty, 'empty(200)', false, problems)
  cmpCase(a.unkHtml, b.unkHtml, 'unknown(404-html)', true, problems)
  cmpCase(a.malJson, b.malJson, 'malformed(404-json)', false, problems)
  cmpCase(a.anonJson, b.anonJson, 'anon(401)', false, problems)
  cmpCase(a.anonHtml, b.anonHtml, 'anon(302)', false, problems)
  return problems.map((p) => `${tag}: ${p}`)
}

const USER_OID = '6aa4b8b573ef0e5094f4cbc0'
const ADMIN_OID = '6aa4b8a873ef0e5094f4cba3'
function createFixtures(mongoC: string): { req: string; empty: string } {
  const script = `
const user=new ObjectId("${USER_OID}"), admin=new ObjectId("${ADMIN_OID}");
db.projects.deleteMany({name:{$in:["webgo-p4d-req","webgo-p4d-empty"]}});
const req=db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4d-req",owner_ref:user,collaberator_refs:[],readOnly_refs:[],publicAccesLevel:"private",
  editAccessRequests:[{userId:admin,privilegeLevel:"readAndWrite",requestedAt:new Date("2026-09-14T00:00:00.000Z")}],
  rootFolder:[{name:"",docs:[{_id:new ObjectId(),name:"main.tex"}],fileRefs:[],folders:[]}]});
const empty=db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4d-empty",owner_ref:user,collaberator_refs:[],readOnly_refs:[],publicAccesLevel:"private",
  rootFolder:[{name:"",docs:[{_id:new ObjectId(),name:"main.tex"}],fileRefs:[],folders:[]}]});
print("REQ:"+req.insertedId.toString());
print("EMPTY:"+empty.insertedId.toString());
`
  const out = dexe(mongoC, `mongosh --quiet sharelatex --eval '${script.replace(/'/g, `'\''`)}'`)
  const req = (out.match(/REQ:([0-9a-f]{24})/) || [])[1]
  const empty = (out.match(/EMPTY:([0-9a-f]{24})/) || [])[1]
  if (!req || !empty) throw new Error('fixture creation failed: ' + out)
  return { req, empty }
}

test.describe.serial('web-go P4.4 flip gate (WEB_GO_PLAN P4.4 access-requests)', () => {
  let overleafC = ''
  let mongoC = ''
  let req = ''
  let empty = ''
  let leg1: Leg | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf')
    mongoC = runningContainer('ol-e2e-mongo')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p4d.conf'), `${overleafC}:/tmp/web-p4d.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p4d.conf /usr/local/share/overleaf-flips/web-p4d.conf')
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) {
      const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
      if (code === '200') break
      await sleep(500)
    }
    const fx = createFixtures(mongoC)
    req = fx.req; empty = fx.empty
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
    leg1 = await battery(req, empty)
    // pins (Node is the authoritative oracle)
    expect(leg1.withReq.status).toBe(200)
    const jr = JSON.parse(leg1.withReq.body)
    expect(Object.keys(jr)).toEqual(['editAccessRequests'])
    expect(jr.editAccessRequests.length).toBe(1)
    expect(Object.keys(jr.editAccessRequests[0])).toEqual(['_id', 'email', 'first_name', 'last_name', 'privilegeLevel', 'currentPrivilegeLevel', 'requestedAt'])
    expect(jr.editAccessRequests[0]._id).toBe(ADMIN_OID)
    expect(jr.editAccessRequests[0].privilegeLevel).toBe('readAndWrite')
    expect(jr.editAccessRequests[0].currentPrivilegeLevel).toBe(false)
    expect(jr.editAccessRequests[0].requestedAt).toBe('2026-09-14T00:00:00.000Z')

    expect(leg1.adminReq.status).toBe(200)
    expect(leg1.adminReq.body, 'site-admin sees the same request list').toBe(leg1.withReq.body)

    const je = JSON.parse(leg1.empty.body)
    expect(je.editAccessRequests).toEqual([])

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
    const leg2 = await battery(req, empty)
    const problems = diffLegs(leg1!, leg2, 'GO')
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 180_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('strip'))
    await nginxSettled()
    const leg3 = await battery(req, empty)
    const problems = diffLegs(leg1!, leg3, 'NODE-rev')
    expect(problems, problems.join('\n---\n')).toHaveLength(0)
  }, 180_000)
})
