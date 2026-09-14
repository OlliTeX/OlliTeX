/**
 * WEB-GO P4.7 FLIP GATE (WEB_GO_PLAN.md P4.7 — basic project creation):
 *
 *   Route (Go shadow at 127.0.0.1:4010, flips/web-p4g.conf):
 *     POST /project/new
 *       body { projectName?: string, template?: string }   (newProjectSchema, z.strictObject)
 *       template == "example" -> example project (DEFERRED to P4.7b: filestore + clsi)
 *
 *   leg 1  Node baseline
 *   leg 2  FLIP ON  — Go
 *   leg 3  FLIP OFF — Node
 *
 * contract pins (live oracle, A/B-verified against the Node service):
 *   - 200 application/json { project_id, owner_ref, owner:{first_name,last_name,email,_id} }
 *       + Mongo project doc (default field set) + docstore revision 0 (mainbasic.tex, 15 lines)
 *         + project_history initializeProject
 *   - 400 text/plain        "Project name cannot be blank"   (absent / whitespace name)
 *   - 400 text/plain        "Project name cannot contain / characters"  (name has '/')
 *   - 400 application/json  {error:"Validation error: ...",statusCode:400}  (zod)
 *         * projectName non-string  "Invalid input: expected string, received <T> at body.projectName"
 *         * template  non-string  ...  at body.template
 *         * unrecognized key        "Unrecognized key(s): "k" at body"
 *         * array root              "Invalid input: expected object, received array at body"
 *   - 400 application/json  `{}`  (JSON root not an object: number/string/null/boolean, or invalid JSON)
 *   - 403 text/plain        "Forbidden"  (anonymous — CSRF before requireLogin)
 *
 * State discipline: each leg (a) deletes prior `webgo-p4g-*` projects, (b) runs the
 * success create and captures the (id-normalized) project doc + docstore line-count +
 * 200 body, (c) runs the full error battery, then (d) deletes the leg's created project.
 * Cross-leg: 200 body (ids normalized) + project doc (ids+lastUpdated normalized) +
 * docstore line-count match the Node baseline; every error case matches exactly
 * (status + content-type + body).
 *
 * Run: npx playwright test -g "web-go P4.7 flip gate"
 */
import { execFileSync } from 'child_process'
import { test, expect } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const PREFIX = 'webgo-p4g-'

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
function dexe(c: string, cmd: string, allowFail = false): string {
  try { return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', maxBuffer: 64*1024*1024, timeout: 150_000 }) }
  catch (e) { if (allowFail) return ''; throw e }
}
function runningContainer(m: string): string {
  const out = execFileSync('docker', ['ps', '--filter', `name=${m}`, '--format', '{{.Names}}'], { encoding: 'utf8' })
  const n = out.split('\n').filter(Boolean)
  if (!n.length) throw new Error(`no container ${m}`)
  return n[0]
}

function FLIP(conf: string, cmd: 'apply' | 'strip'): string {
  if (cmd === 'apply') return `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/${conf} /etc/nginx/overleaf-flips/${conf}
if ! grep -q "overleaf-flips/${conf}" "$vhost"; then
  node -e '
    const fs=require("fs");const v=process.argv[1];
    const inc="  include /etc/nginx/overleaf-flips/${conf};\\n\\n";
    let s=fs.readFileSync(v,"utf8");const l=s.split("\\n");
    const i=l.findIndex(x=>x.trim()==="location / {");
    if(i<0)throw new Error("location / not found");
    l.splice(i,0,inc);fs.writeFileSync(v,l.join("\\n"));' "$vhost"
fi
nginx -t && nginx -s reload && sleep 2
`
  return `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
if grep -q "overleaf-flips/${conf}" "$vhost"; then
  sed -i "/overleaf-flips\\\/${conf}/d" "$vhost"
  nginx -t && nginx -s reload && sleep 2
fi
`
}

interface R { status: number; ct: string; body: string; setcookie?: string }

const normBody = (s: string) => s
  .replace(/\b[0-9a-f]{24}\b/g, '<HEX>')
  .replace(/"ol-csrfToken"\s*:\s*"[^"]*"/g, '"ol-csrfToken":"CSRF"')
  .replace(/"csrfToken"\s*:\s*"[^"]*"/g, '"csrfToken":"CSRF"')

async function nginxSettled(timeoutMs = 15_000): Promise<void> {
  const t0 = Date.now()
  for (;;) {
    try { const r = await fetch(BASE + '/status', { redirect: 'manual' }); if (r.status >= 100) { await r.text().catch(() => {}); return } } catch {}
    if (Date.now() - t0 > timeoutMs) throw new Error('nginx settle')
    await sleep(300)
  }
}

async function call(p: string, init: RequestInit & { cookie?: string } = {}): Promise<R> {
  const h = { ...(init.headers || {}) as Record<string, string>, accept: ((init.headers as any)?.accept as string) || 'application/json' }
  if (init.cookie) h['cookie'] = init.cookie as string
  const r = await fetch(BASE + p, { ...init, headers: h, redirect: 'manual' })
  const setcookie = (r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : (r.headers.get('set-cookie') || '')) as string
  const body = Buffer.from(await r.arrayBuffer()).toString('binary')
  return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], body, setcookie }
}

async function login(): Promise<{ ck: string; csrf: string }> {
  const page = await call('/login')
  const csrf = (page.body.match(/name=["']ol-csrfToken["']\s+content=["']([^"']+)"/) || [])[1] || ''
  let ck = (page.setcookie || '').match(/overleaf\.sid=[^;\n]+/) ? (page.setcookie || '').split('\n').find((l) => l.includes('overleaf.sid')) || '' : ''
  const logged = await call('/login', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body: JSON.stringify(USER) })
  expect(logged.status, 'login status (leg)').toBe(200)
  ck = (logged.setcookie || '').split('\n').find((l) => l.includes('overleaf.sid')) || ck
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body }
}

function mongoDeletePrefix(prefix: string, mongoC: string): void {
  dexe(mongoC, `mongosh --quiet sharelatex --eval 'db.projects.deleteMany({name:{$regex:"^${prefix}"}})'`, true)
}
function mongoDoc(pid: string, mongoC: string): string {
  return dexe(mongoC, `mongosh --quiet sharelatex --eval '
    const p=db.projects.findOne({_id:ObjectId("${pid}")});
    if(!p){print("NONE");}else{
      const o={}; for(const k in p){ if(k==="_id")o._id="<HEX>"; else o[k]=p[k]; }
      o.rootFolder=(o.rootFolder||[]).map(f=>({name:f.name,docs:(f.docs||[]).map(d=>({name:d.name})),fileRefs:(f.fileRefs||[]).length,folders:(f.folders||[]).length}));
      if(o.overleaf&&o.overleaf.history)o.overleaf.history.id="<HEX>";
      o.rootDoc_id="<HEX>"; delete o.lastUpdated; delete o.__v;
      print(JSON.stringify(o));
    }'`).trim()
}
function docstoreLines(pid: string, overleafC: string): string {
  return dexe(overleafC, `
    set -e
    pid=${pid}
    didout=$(curl -s http://127.0.0.1:3016/project/$pid/doc)
    docid=$(printf '%s' "$didout" | node -e 'let d="";process.stdin.on("data",c=>d+=c).on("end",()=>{const j=JSON.parse(d);const a=j.docs||j;console.log((a[0]&&(a[0]._id||a[0].id))||"")})')
    linesout=$(curl -s http://127.0.0.1:3016/project/$pid/doc/$docid)
    printf '%s' "$linesout" | node -e 'let d="";process.stdin.on("data",c=>d+=c).on("end",()=>{const j=JSON.parse(d);console.log(j.lines?j.lines.length:"NOLINES")})'
  `, true).trim() || 'ERR'
}

interface Leg {
  create: R & { pid: string }
  createState: string
  createDocLines: string
  errors: Record<string, R>
  anon: R
}

async function battery(mongoC: string, overleafC: string): Promise<Leg> {
  const { ck, csrf } = await login()
  const cr = await call('/project/new', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body: JSON.stringify({ projectName: PREFIX + 'gate' }) })
  const pid = (cr.body.match(/"project_id"\s*:\s*"([0-9a-f]{24})"/) || [])[1] || ''
  const createState = pid ? mongoDoc(pid, mongoC) : 'NO_PID'
  const docLines = docstoreLines(pid, overleafC)
  const err = (body: string) => call('/project/new', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body })
  const errors: Record<string, R> = {}
  errors.blank     = await err(JSON.stringify({ projectName: '   ' }))
  errors.whitespace= await err(JSON.stringify({ projectName: '\t \n' }))
  errors.slash     = await err(JSON.stringify({ projectName: 'a/b' }))
  errors.extra     = await err(JSON.stringify({ projectName: 'x', extraKey: 1 }))
  errors.numName   = await err(JSON.stringify({ projectName: 123 }))
  errors.boolName  = await err(JSON.stringify({ projectName: true }))
  errors.tmplOnly  = await err(JSON.stringify({ template: 'basic' }))
  errors.emptyObj  = await err(JSON.stringify({}))
  errors.badJson   = await err('{not json')
  errors.numRoot   = await err('123')
  errors.strRoot   = await err('"foo"')
  errors.nullRoot  = await err('null')
  errors.arrRoot   = await err('[1,2]')
  const anon = await call('/project/new', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': 'faketoken', accept: 'application/json' }, body: '{"projectName":"x"}' })
  mongoDeletePrefix(PREFIX, mongoC)
  return { create: { ...cr, pid }, createState, createDocLines: docLines, errors, anon }
}

function errCompare(a: Record<string, R>, b: Record<string, R>, tag: string): string[] {
  const p: string[] = []
  for (const k of Object.keys(a).sort()) {
    const x = a[k], y = b[k]
    if (!y || x.status !== y.status || x.ct !== y.ct || x.body !== y.body) {
      p.push(`${tag}:${k}: N[s=${x.status} ct=${x.ct} b=${x.body.slice(0,60)}] G[s=${y?.status ?? 'N/A'} ct=${y?.ct ?? ''} b=${(y?.body || '').slice(0,60)}]`)
    }
  }
  return p
}

function diffLegs(a: Leg, b: Leg, tag: string): string[] {
  const p = errCompare(a.errors, b.errors, tag)
  if (a.anon.status !== b.anon.status || a.anon.body !== b.anon.body || a.anon.ct !== b.anon.ct)
    p.push(`${tag}:anon N[s=${a.anon.status} ct=${a.anon.ct} b=${a.anon.body}] G[s=${b.anon.status} ct=${b.anon.ct} b=${b.anon.body}]`)
  if (a.create.status !== b.create.status) p.push(`${tag}:create-status N=${a.create.status} G=${b.create.status}`)
  if (a.create.ct !== b.create.ct) p.push(`${tag}:create-ct N=${a.create.ct} G=${b.create.ct}`)
  const na = normBody(a.create.body), nb = normBody(b.create.body)
  if (na !== nb) p.push(`${tag}:create-body N[${na.slice(0,90)}] G[${nb.slice(0,90)}]`)
  const sa = a.createState.trim(), sb = b.createState.trim()
  if (sa !== sb) p.push(`${tag}:state-diff len N=${sa.length} G=${sb.length}\nN=${sa.slice(0,200)}\nG=${sb.slice(0,200)}`)
  if (a.createDocLines !== b.createDocLines) p.push(`${tag}:doclines N=${a.createDocLines} G=${b.createDocLines}`)
  return p
}

test.describe.serial('web-go P4.7 flip gate (WEB_GO_PLAN P4.7 basic create)', () => {
  let overleafC = '', mongoC = '', leg1: Leg | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf'); mongoC = runningContainer('ol-e2e-mongo')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p4g.conf'), `${overleafC}:/tmp/web-p4g.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p4g.conf /usr/local/share/overleaf-flips/web-p4g.conf')
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) { const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim(); if (code === '200') break; await sleep(500) }
    dexe(overleafC, FLIP('web-p4g.conf', 'strip'), true); await nginxSettled()
  }, 240_000)

  test.afterAll(async () => { try { dexe(overleafC, FLIP('web-p4g.conf', 'strip'), true); await nginxSettled(); mongoDeletePrefix(PREFIX, mongoC) } catch {} })

  test('leg 1: Node baseline (create + state + error battery)', async () => {
    test.setTimeout(200_000)
    dexe(overleafC, FLIP('web-p4g.conf', 'strip'), true); await nginxSettled()
    mongoDeletePrefix(PREFIX, mongoC)
    leg1 = await battery(mongoC, overleafC)
    expect(leg1.create.status, 'create status').toBe(200)
    expect(leg1.create.ct).toBe('application/json')
    expect(leg1.create.body).toContain('project_id')
    expect(leg1.create.body).toContain('owner_ref')
    expect(leg1.createState, 'project doc present').toContain('"collabratecUsers"')
    expect(leg1.createDocLines, 'docstore lines').toBe('15')
    expect(leg1.errors.blank.status).toBe(400)
    expect(leg1.errors.blank.ct).toBe('text/plain')
    expect(leg1.errors.blank.body).toBe('Project name cannot be blank')
    expect(leg1.errors.slash.body).toBe('Project name cannot contain / characters')
    expect(leg1.errors.extra.status).toBe(400)
    expect(leg1.errors.extra.ct).toBe('application/json')
    expect(leg1.errors.extra.body).toContain('Unrecognized key')
    expect(leg1.errors.numName.body).toContain('expected string, received number')
    expect(leg1.errors.badJson.status).toBe(400)
    expect(leg1.errors.badJson.body).toBe('{}')
    expect(leg1.errors.arrRoot.status).toBe(400)
    expect(leg1.errors.arrRoot.ct).toBe('application/json')
    expect(leg1.anon.status).toBe(403)
    expect(leg1.anon.body).toBe('Forbidden')
  }, 200_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(200_000)
    dexe(overleafC, FLIP('web-p4g.conf', 'apply')); await nginxSettled()
    const leg2 = await battery(mongoC, overleafC)
    const p = diffLegs(leg1!, leg2, 'GO')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 200_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(200_000)
    dexe(overleafC, FLIP('web-p4g.conf', 'strip')); await nginxSettled()
    const leg3 = await battery(mongoC, overleafC)
    const p = diffLegs(leg1!, leg3, 'NODE-rev')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 200_000)
})
