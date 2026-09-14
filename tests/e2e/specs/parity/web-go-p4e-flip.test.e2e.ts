/**
 * WEB-GO P4.5 FLIP GATE (WEB_GO_PLAN.md P4.5 — project rename route):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p4e.conf):
 *     POST /project/:Project_id/rename
 *       → requireLogin → ensureUserCanAdminProject (owner || site-admin)
 *       → body { newProjectName: "<string>" }  (trim; blank -> 400)
 *       → sets project.name  (NO audit entry)  → 200 text/plain "OK"
 *
 *   leg 1  Node baseline       (legName "…-L1")
 *   leg 2  FLIP ON — Go        (legName "…-L2")
 *   leg 3  FLIP OFF — Node     (legName "…-L3")
 *
 * Contract pins (live-oracle 2026-09-14; A/B 6/6 byte-identical):
 *   - 200 text/plain body "OK"  + project.name set to the trimmed value
 *   - blank name (trim -> "")  -> 400 text/plain "Project name cannot be blank"
 *   - newProjectName missing   -> 400 application/json (zod "received undefined")
 *   - newProjectName number    -> 400 application/json (zod "received number")
 *   - INVALID (non-hex) id     -> 404 application/json malformed (not accept-dep)
 *   - valid id, absent project -> 404 HTML general/404 (not accept-dep)
 *   - anonymous POST           -> 403 text/plain "Forbidden"  (CSRF before requireLogin)
 *   - not admin                -> 403 restricted json/html     (not reachable w/ the 2 e2e users)
 *
 * Write-discipline: each leg renames the SAME fixture to a DISTINCT name, then
 * asserts (a) that leg's responses (status/ct/body) match the Node baseline
 * byte-for-byte and (b) Mongo name == that leg's name. The invalid/error cases
 * must leave the name unchanged.
 *
 * Run: npx playwright test -g "web-go P4.5 flip gate"
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
  const conf = 'web-p4e.conf'
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

const HDRS = ['content-type', 'location', 'etag', 'content-length'] as const
interface R { status: number; ct: string; loc: string; body: string; setcookie: string }

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
  let ck = (page.setcookie.match(/overleaf\.sid=[^;\n]+/) || [])[0] || ''
  const r = await call('/login', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body: JSON.stringify(USER) })
  expect(r.status, 'login').toBe(200)
  const lines = r.setcookie.split('\n').filter((l) => l.includes('overleaf.sid'))
  ck = lines[lines.length - 1] || ck
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body }
}

async function postRename(p: string, body: any, ck: string, csrf: string): Promise<R> {
  return call(p, { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body: JSON.stringify(body) })
}

function normHtml(s: string): string {
  return s.replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N').replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
    .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
    .replace(/https?:\/\/[0-9a-z.-]+:[0-9]+/gi, 'SITE')
}
function bodyCompare(a: R, b: R): void {
  expect(a.status, 'status').toBe(b.status)
  expect(a.ct, 'content-type').toBe(b.ct)
  expect(a.loc, 'location').toBe(b.loc)
  const isHtml = /text\/html/.test(a.ct)
  const ba = isHtml ? normHtml(a.body) : a.body
  const bb = isHtml ? normHtml(b.body) : b.body
  expect(ba, 'body').toBe(bb)
}

interface Leg { valid: R; blank: R; missing: R; num: R; mal: R; absent: R; anon: R }
async function battery(p: string, legName: string): Promise<Leg> {
  const { ck, csrf } = await login()
  const base = `/project/${p}`
  const valid = await postRename(base + '/rename', { newProjectName: legName }, ck, csrf)
  const blank = await postRename(base + '/rename', { newProjectName: '   ' }, ck, csrf)
  const missing = await postRename(base + '/rename', {}, ck, csrf)
  const num = await postRename(base + '/rename', { newProjectName: 123 }, ck, csrf)
  const mal = await postRename('/project/not-a-valid-oid/rename', { newProjectName: 'X' }, ck, csrf)
  const absent = await postRename('/project/ffffffffffffffffffffffff/rename', { newProjectName: 'X' }, ck, csrf)
  const anon = await postRename(base + '/rename', { newProjectName: 'X' }, '', '')
  return { valid, blank, missing, num, mal, absent, anon }
}

function diffLegs(a: Leg, b: Leg, tag: string): string[] {
  const p: string[] = []
  for (const k of ['valid', 'blank', 'missing', 'num', 'mal', 'absent'] as const) {
    try { bodyCompare(a[k], b[k]) } catch (e: any) { p.push(`${tag}:${k}: ${String(e.message).split('\n')[0]}`) }
  }
  return p
}

function mongoName(oid: string, mongoC: string): string {
  return dexe(mongoC, `mongosh --quiet sharelatex --eval 'print(db.projects.findOne({_id:ObjectId("${oid}")})?.name ?? "NULL")'`).trim()
}

test.describe.serial('web-go P4.5 flip gate (WEB_GO_PLAN P4.5 rename)', () => {
  let overleafC = '', mongoC = '', pid = '', leg1: Leg | null = null
  const L1 = 'P4e-Rename-L1', L2 = 'P4e-Rename-L2', L3 = 'P4e-Rename-L3'

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf'); mongoC = runningContainer('ol-e2e-mongo')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p4e.conf'), `${overleafC}:/tmp/web-p4e.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p4e.conf /usr/local/share/overleaf-flips/web-p4e.conf')
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) { const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim(); if (code === '200') break; await sleep(500) }
    const out = dexe(mongoC, `mongosh --quiet sharelatex --eval '
       const user=new ObjectId("${USER_OID}");
       db.projects.deleteMany({name:"webgo-p4e-ren"});
       const r=db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4e-ren",owner_ref:user,collaberator_refs:[],readOnly_refs:[],publicAccesLevel:"private",rootFolder:[{name:"",docs:[{_id:new ObjectId(),name:"main.tex"}],fileRefs:[],folders:[]}]});
       print(r.insertedId.toString())'`)
    pid = (out.match(/[0-9a-f]{24}/) || [])[0]
    if (!pid) throw new Error('fixture create failed: ' + out)
    dexe(overleafC, FLIP('strip'), true); await nginxSettled()
  })

  test.afterAll(async () => { try { dexe(overleafC, FLIP('strip'), true); await nginxSettled() } catch {} })

  test('leg 1: Node baseline (rename + error battery)', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('strip'), true); await nginxSettled()
    leg1 = await battery(pid, L1)
    expect(leg1.valid.status).toBe(200)
    expect(leg1.valid.body).toBe('OK')
    expect(leg1.valid.ct).toBe('text/plain; charset=utf-8')
    expect(mongoName(pid, mongoC)).toBe(L1)
    expect(leg1.blank.status).toBe(400)
    expect(leg1.blank.body).toBe('Project name cannot be blank')
    expect(leg1.blank.ct).toBe('text/plain; charset=utf-8')
    expect(leg1.missing.status).toBe(400)
    expect(leg1.num.status).toBe(400)
    expect(leg1.mal.status).toBe(404)
    expect(leg1.absent.status).toBe(404)
    expect(leg1.absent.ct).toContain('text/html')
    expect(leg1.anon.status).toBe(403)
    expect(leg1.anon.body).toBe('Forbidden')
    expect(mongoName(pid, mongoC), 'error cases must not change the name').toBe(L1)
  }, 180_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('apply')); await nginxSettled()
    const leg2 = await battery(pid, L2)
    const p = diffLegs(leg1!, leg2, 'GO')
    expect(mongoName(pid, mongoC), 'Go sets its own leg name').toBe(L2)
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 180_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('strip')); await nginxSettled()
    const leg3 = await battery(pid, L3)
    const p = diffLegs(leg1!, leg3, 'NODE-rev')
    expect(mongoName(pid, mongoC)).toBe(L3)
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 180_000)
})
