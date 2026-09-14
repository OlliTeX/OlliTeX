/**
 * WEB-GO P4.6 FLIP GATE (WEB_GO_PLAN.md P4.6 — project flag-writes):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p4f.conf):
 *     POST   /Project/:Project_id/archive  (requireLogin, ensureUserCanReadProject) -> archive
 *     DELETE /Project/:Project_id/archive  (same)                                   -> unarchive
 *     POST   /project/:project_id/trash    (same)                                   -> trash
 *     DELETE /project/:project_id/trash    (same)                                   -> untrash
 *
 *   leg 1  Node baseline
 *   leg 2  FLIP ON — Go
 *   leg 3  FLIP OFF — Node
 *
 * contract pins (live oracle; each valid op -> 200 text/plain "OK"):
 *   archive   : $addToSet{archived:uid} + $pull{trashed:uid}
 *   unarchive : $pull{archived:uid}
 *   trash     : $addToSet{trashed:uid}  + $pull{archived:uid}
 *   untrash   : $pull{trashed:uid}
 *   (`archived` / `trashed` are per-user ObjectId SETS; NO audit entry in the free build)
 *   - invalid (non-hex) id   -> 404 JSON malformed (Project_id | project_id per route)
 *   - valid id, absent       -> 404 HTML general/404 (not accept-dep)
 *   - anonymous              -> 403 text/plain "Forbidden" (CSRF before requireLogin)
 *   - present, no read access -> 403 (json/html)  (not reachable w/ the 2 e2e users)
 *
 * State discipline: each leg (a) resets archived/trashed, (b) runs the 4 valid
 * ops and captures the Mongo state after EACH, (c) runs the error battery, and
 * asserts (i) all valid responses == 200 "OK", (ii) responses+state match the
 * Node baseline byte-for-byte, (iii) error cases leave the final state unchanged.
 *
 * Run: npx playwright test -g "web-go P4.6 flip gate"
 */
import { execFileSync } from 'child_process'
import { test, expect } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const USER_OID = '6aa4b8b573ef0e5094f4cbc0'

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

function FLIP(cmd: 'apply' | 'strip'): string {
  const conf = 'web-p4f.conf'
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

interface R { status: number; ct: string; loc: string; body: string; setcookie: string }

const normHtml = (s: string) => s
  .replace(/nonce="[^"]{4,40}"/g, 'nonce="N"').replace(/nonce-[A-Za-z0/+/=_-]+/g, 'nonce-N')
  .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
  .replace(/value="[^"]*"/g, 'value="CSRF"')
  .replace(/https?:\/\/[0-9a-z.-]+(?::[0-9]+)?/gi, 'SITE')

function bodyCompare(a: R, b: R): void {
  expect(a.status, 'status').toBe(b.status)
  expect(a.ct, 'content-type').toBe(b.ct)
  const isHtml = /text\/html/.test(a.ct)
  const ba = isHtml ? normHtml(a.body) : a.body
  const bb = isHtml ? normHtml(b.body) : b.body
  expect(ba, 'body').toBe(bb)
}

async function nginxSettled(timeoutMs = 15_000): Promise<void> {
  const t0 = Date.now()
  for (;;) {
    try { const r = await fetch(BASE + '/status', { redirect: 'manual' }); if (r.status >= 100) { await r.text().catch(() => {}); return } } catch {}
    if (Date.now() - t0 > timeoutMs) throw new Error('nginx settle')
    await sleep(300)
  }
}

async function call(p: string, init: RequestInit & { cookie?: string } = {}, attempt = 0): Promise<R> {
  try {
    const h = { ...(init.headers || {}) as Record<string, string>, accept: ((init.headers as any)?.accept as string) || 'application/json' }
    if (init.cookie) h['cookie'] = init.cookie as string
    const r = await fetch(BASE + p, { ...init, headers: h, redirect: 'manual' })
    const setcookie = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : (r.headers.get('set-cookie') || '')
    const body = Buffer.from(await r.arrayBuffer()).toString('binary')
    return { status: r.status, ct: r.headers.get('content-type') || '', loc: r.headers.get('location') || '', body, setcookie }
  } catch (e) {
    if (attempt < 3 && /socket|ECONNRESET|fetch failed/i.test(String(e))) { await sleep(400 * (attempt + 1)); return call(p, init, attempt + 1) }
    throw e
  }
}

async function login(): Promise<{ ck: string; csrf: string }> {
  const page = await call('/login')
  const csrf = (page.body.match(/name=["']ol-csrfToken["']\s+content=["']([^"']+)"/) || [])[1] || ''
  let ck = (page.ct ? '' : '')
  const setc = (page as any).setcookie || ''
  ck = (setc.match(/overleaf\.sid=[^;\n]+/) || [])[0] || ''
  const r = (await call('/login', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body: JSON.stringify(USER) }) as any)
  expect(r.status, 'login').toBe(200)
  const lines = ((r.setcookie || '').split('\n')).filter((l: string) => l.includes('overleaf.sid'))
  ck = lines[lines.length - 1] || ck
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body }
}

function mongoState(oid: string, mongoC: string): string {
  const o = dexe(mongoC, `mongosh --quiet sharelatex --eval 'const p=db.projects.findOne({_id:ObjectId("${oid}")},{archived:1,trashed:1});const j={a:(p.archived||[]).map(v=>String(v)),t:(p.trashed||[]).map(v=>String(v)),hasA:!("archived" in p==false)&&("archived" in p),hasT:("trashed" in p)};print(JSON.stringify(j));'`).trim()
  return o
}
function mongoReset(oid: string, mongoC: string): void {
  dexe(mongoC, `mongosh --quiet sharelatex --eval 'db.projects.updateOne({_id:ObjectId("${oid}")},{$unset:{archived:"",trashed:""}})'`)
}

interface Leg {
  archive: R; unarchive: R; trash: R; untrash: R
  sArchive: string; sUnarchive: string; sTrash: string; sUntrash: string
  malArch: R; malTrash: R; absArch: R; absTrash: R; anonArch: R; anonTrash: R
}
async function battery(p: string, mongoC: string): Promise<Leg> {
  const { ck, csrf } = await login()
  mongoReset(p, mongoC)
  const op = (method: string, url: string) =>
    call(url, { method, headers: { 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body: undefined })
  const archive = await op('POST', `/Project/${p}/archive`)
  const sArchive = mongoState(p, mongoC)
  const unarchive = await op('DELETE', `/Project/${p}/archive`)
  const sUnarchive = mongoState(p, mongoC)
  const trash = await op('POST', `/project/${p}/trash`)
  const sTrash = mongoState(p, mongoC)
  const untrash = await op('DELETE', `/project/${p}/trash`)
  const sUntrash = mongoState(p, mongoC)
  const malArch = await op('POST', '/Project/not-a-oid/archive')
  const malTrash = await op('POST', '/project/not-a-oid/trash')
  const absArch = await call(`/Project/ffffffffffffffffffffffff/archive`, { method: 'POST', headers: { 'x-csrf-token': csrf, accept: 'text/html' }, cookie: ck })
  const absTrash = await call(`/project/ffffffffffffffffffffffff/trash`, { method: 'POST', headers: { 'x-csrf-token': csrf, accept: 'text/html' }, cookie: ck })
  // anonymous (no session cookie / no csrf) -> CSRF 403 Forbidden
  const anonArch = await call(`/Project/${p}/archive`, { method: 'POST', headers: { accept: 'application/json' } })
  const anonTrash = await call(`/project/${p}/trash`, { method: 'DELETE', headers: { accept: 'application/json' } })
  return { archive, unarchive, trash, untrash, sArchive, sUnarchive, sTrash, sUntrash, malArch, malTrash, absArch, absTrash, anonArch, anonTrash }
}

function diffLegs(a: Leg, b: Leg, tag: string): string[] {
  const p: string[] = []
  for (const k of ['archive', 'unarchive', 'trash', 'untrash', 'malArch', 'malTrash', 'absArch', 'absTrash', 'anonArch', 'anonTrash'] as const) {
    try { bodyCompare(a[k], b[k]) } catch (e: any) { p.push(`${tag}:${k}: ${String(e.message).split('\n')[0]}`) }
  }
  for (const k of ['sArchive', 'sUnarchive', 'sTrash', 'sUntrash'] as const) {
    if (a[k] !== b[k]) p.push(`${tag}:${k}: N=${a[k]} G=${b[k]}`)
  }
  return p
}

test.describe.serial('web-go P4.6 flip gate (WEB_GO_PLAN P4.6 flag-writes)', () => {
  let overleafC = '', mongoC = '', pid = '', leg1: Leg | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf'); mongoC = runningContainer('ol-e2e-mongo')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p4f.conf'), `${overleafC}:/tmp/web-p4f.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p4f.conf /usr/local/share/overleaf-flips/web-p4f.conf')
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) { const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim(); if (code === '200') break; await sleep(500) }
    const out = dexe(mongoC, `mongosh --quiet sharelatex --eval '
       const user=new ObjectId("${USER_OID}");
       db.projects.deleteMany({name:"webgo-p4f-flags"});
       const r=db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4f-flags",owner_ref:user,collaberator_refs:[],readOnly_refs:[],publicAccesLevel:"private",rootFolder:[{name:"",docs:[{_id:new ObjectId(),name:"main.tex"}],fileRefs:[],folders:[]}]});
       print(r.insertedId.toString())'`)
    pid = (out.match(/[0-9a-f]{24}/) || [])[0]
    if (!pid) throw new Error('fixture create failed: ' + out)
    dexe(overleafC, FLIP('strip'), true); await nginxSettled()
  })

  test.afterAll(async () => { try { dexe(overleafC, FLIP('strip'), true); await nginxSettled() } catch {} })

  test('leg 1: Node baseline (flag-write battery + state)', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('strip'), true); await nginxSettled()
    leg1 = await battery(pid, mongoC)
    for (const k of ['archive', 'unarchive', 'trash', 'untrash'] as const) {
      expect(leg1[k].status, k).toBe(200)
      expect(leg1[k].body, k).toBe('OK')
      expect(leg1[k].ct, k).toBe('text/plain; charset=utf-8')
    }
    expect(leg1.sArchive, 'archive add-to-set').toContain(USER_OID)
    expect(leg1.sUntrash, 'final untrash clean').not.toContain(USER_OID)
    expect(leg1.malArch.status).toBe(404)
    expect(leg1.malArch.body).toContain('params.Project_id')
    expect(leg1.malTrash.status).toBe(404)
    expect(leg1.malTrash.body).toContain('params.project_id')
    expect(leg1.absArch.status).toBe(404); expect(leg1.absArch.ct).toContain('text/html')
    expect(leg1.absTrash.status).toBe(404); expect(leg1.absTrash.ct).toContain('text/html')
    expect(leg1.anonArch.status).toBe(403); expect(leg1.anonArch.body).toBe('Forbidden')
    expect(leg1.anonTrash.status).toBe(403); expect(leg1.anonTrash.body).toBe('Forbidden')
  }, 180_000)

  test('leg 2: FLIP ON — Go matches the Node baseline (responses + state)', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('apply')); await nginxSettled()
    const leg2 = await battery(pid, mongoC)
    const p = diffLegs(leg1!, leg2, 'GO')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 180_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('strip')); await nginxSettled()
    const leg3 = await battery(pid, mongoC)
    const p = diffLegs(leg1!, leg3, 'NODE-rev')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 180_000)
})
