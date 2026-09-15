/**
 * WEB-GO P4.7b FLIP GATE (WEB_GO_PLAN.md P4.7b — 'example' project creation):
 *
 *   Route (Go shadow at 127.0.0.1:4010, flips/web-p4g.conf — same location as P4.7):
 *     POST /project/new   body { projectName?: string, template?: string }
 *       template === "example"  -> createExampleProject
 *         = basic project doc (default field set)
 *         + main.tex           (docstore rev 0, 118 lines)  + setRootDoc
 *         + sample.bib         (docstore rev 0, 10 lines)
 *         + frog.jpg           (history-v1 blob, git-blob-sha1 hash, rootFolder.fileRefs[0])
 *         + project_history initializeProject
 *         (+ clsi cache warm — fire-and-forget perf, NOT a contract; deferred)
 *
 *   leg 1  Node baseline      (template:'example')
 *   leg 2  FLIP ON  — Go      (template:'example')
 *   leg 3  FLIP OFF — Node    (template:'example')
 *
 *   Contract pins (live oracle, A/B-verified against the Node service):
 *     - 200 application/json { project_id, owner_ref, owner:{first_name,last_name,email,_id} }
 *     - project doc: rootFolder.docs=[main.tex, sample.bib];
 *                    rootFolder.fileRefs=[{name:'frog.jpg',rev:0,linkedFileData:null,
 *                                          hash:'5b889ef3cf71c83a4c027c4e4dc3d1a106b27809'}];
 *                    folders=[]; rootDoc_id set (= main.tex doc id).
 *     - docstore: main.tex 118 lines, sample.bib 10 lines (rev 0).
 *     - frog.jpg blob: retrievable from history-v1, BYTE-IDENTICAL to the template
 *                       (md5 665777aa6c7c48794db02f5d69ccc24a, 97080 bytes).
 *     - name/template validation battery + anonymous 403 (shared parse path) still match.
 *
 *   State discipline: each leg deletes prior `webgo-p4hb-*` projects, runs the example
 *   create + full state capture, runs the error battery, then deletes the leg's project.
 *   Cross-leg: 200 body (ids normalized) + normalized state (doc names, fileRef
 *   {name,rev,linkedFileData,hash}, doc line-counts, frog blob md5+size) all match;
 *   every error case matches exactly (status + content-type + body).
 *
 *   Run: npx playwright test -g "web-go P4.7b flip gate"
 */
import { execFileSync } from 'child_process'
import { test, expect } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const PREFIX = 'webgo-p4hb-'

// Live-oracle pins (deterministic: static template files; hash = f(content)).
const FROG_HASH = '5b889ef3cf71c83a4c027c4e4dc3d1a106b27809'   // git blob sha1
const FROG_MD5 = '665777aa6c7c48794db02f5d69ccc24a'             // raw content md5
const FROG_SIZE = 97080
const MAIN_LINES = 118
const BIB_LINES = 10

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
if grep -q "overleaf-flips\\\/${conf}" "$vhost"; then
  sed -i "/overleaf-flips\\\/${conf}/d" "$vhost"
  nginx -t && nginx -s reload && sleep 2
fi
`
}

interface R { status: number; ct: string; body: string; setcookie?: string }
const normBody = (s: string) => s
  .replace(/\b[0-9a-f]{24}\b/g, '<HEX>')
  .replace(/"ol-csrfToken"\s*:\s*"[^"]*"/g, '"ol-csrfToken":"CSRF"')

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

// Self-contained state capture (runs in the overleaf container): project doc
// shape + docstore line-counts + frog blob md5/size. Output is id-free and
// timestamp-free, so it is directly comparable across legs and runs.
const EXCAP = `
import crypto from 'node:crypto';
import { MongoClient, ObjectId } from 'mongodb';
const pid = process.argv[2];
const M = process.env.OVERLEAF_MONGO_URL || 'mongodb://mongo:27017/sharelatex?replicaSet=overleaf';
const client = new MongoClient(M); await client.connect();
const db = client.db('sharelatex');
const p = await db.collection('projects').findOne({ _id: new ObjectId(pid) });
if (!p) { console.log(JSON.stringify({ present: false })); process.exit(0); }
const rf = p.rootFolder && p.rootFolder[0];
const docs = ((rf && rf.docs) || []).map(function(d){ return d.name; }).sort();
const fileRefs = ((rf && rf.fileRefs) || []).map(function(f){ return { n: f.name, rev: f.rev, lfd: f.linkedFileData === undefined ? 'undef' : f.linkedFileData, hash: f.hash }; });
const folders = ((rf && rf.folders) || []).length;
const docstore = process.env.WEB_DOCSTORE_URL || 'http://127.0.0.1:3016';
const docLines = {};
for (const d of ((rf && rf.docs) || [])) {
  try { const r = await fetch(docstore + '/project/' + pid + '/doc/' + d._id); const txt = await r.text();
    let n; try { const j = JSON.parse(txt); n = Array.isArray(j.lines) ? j.lines.length : 'NOLINES'; } catch (e) { n = 'PARSE'; }
    docLines[d.name] = n;
  } catch (e) { docLines[d.name] = 'ERR'; }
}
const v1 = process.env.V1_HISTORY_URL || 'http://127.0.0.1:3100/api';
const u = process.env.V1_HISTORY_USER || 'staging';
const pw = process.env.V1_HISTORY_PASSWORD || '';
const auth = 'Basic ' + Buffer.from(u + ':' + pw).toString('base64');
const blob = {};
for (const f of fileRefs) {
  try { const r = await fetch(v1 + '/projects/' + pid + '/blobs/' + f.hash, { headers: { authorization: auth } });
    if (r.status !== 200) { blob[f.n] = { status: r.status }; continue; }
    const buf = Buffer.from(await r.arrayBuffer());
    blob[f.n] = { md5: crypto.createHash('md5').update(buf).digest('hex'), size: buf.length };
  } catch (e) { blob[f.n] = { err: String(e) }; }
}
const out = {
  present: true,
  docs, fileRefs, folders,
  rootDocHex: /^[0-9a-f]{24}$/.test(String(p.rootDoc_id)),
  docLines, blob,
  name: p.name, compiler: p.compiler, spell: p.spellCheckLanguage,
  grammarPicky: p.grammarPicky, publicAccesLevel: p.publicAccesLevel,
  readOnly: p.readOnly, active: p.active,
  hasCollabratecUsers: ('collabratecUsers' in p), tokensEmpty: (p.tokens || []).length === 0,
};
console.log(JSON.stringify(out));
await client.close();
`

function exCapture(pid: string, overleafC: string): string {
  // The 'mongodb' driver resolves only from inside the pnpm workspace
  // (/overleaf/services/web), so the script lives there briefly and is removed
  // after. Runs from that cwd; output is deterministic (id/timestamp-free).
  return dexe(overleafC, `
    cat > /overleaf/services/web/.excap-tmp.mjs <<'EXO'
${EXCAP}
EXO
    cd /overleaf/services/web && node .excap-tmp.mjs ${pid}; rc=$?; rm -f /overleaf/services/web/.excap-tmp.mjs; exit $rc
  `, true).trim().split('\n').slice(-1)[0] || 'NO_CAPTURE'
}

interface Leg {
  create: R & { pid: string }
  state: string
  errors: Record<string, R>
  anon: R
}


// login-rate-limit hygiene: consecutive gate legs/batches trip the per-IP
// login limiter (403 on login). Clear the rate-limit keys before each login.
function clearRateLimits(): void {
  try {
    const out = execFileSync('docker', ['ps', '--filter', 'name=ol-e2e-redis', '--format', '{{.Names}}'], { encoding: 'utf8' })
    const rc = out.split('\n').find(Boolean)
    if (!rc) return
    const keys = execFileSync('docker', ['exec', rc, 'sh', '-c', 'redis-cli --scan --pattern "rate-limit:*"'], { encoding: 'utf8' }).split('\n').filter(Boolean)
    for (const k of keys) execFileSync('docker', ['exec', rc, 'redis-cli', 'DEL', k], { encoding: 'utf8' })
  } catch {}
}

async function battery(mongoC: string, overleafC: string): Promise<Leg> {
  clearRateLimits()
  const { ck, csrf } = await login()
  const cr = await call('/project/new', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body: JSON.stringify({ projectName: PREFIX + 'gate', template: 'example' }) })
  const pid = (cr.body.match(/"project_id"\s*:\s*"([0-9a-f]{24})"/) || [])[1] || ''
  await sleep(300)
  const state = pid ? exCapture(pid, overleafC) : 'NO_PID'
  const err = (body: string) => call('/project/new', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body })
  const errors: Record<string, R> = {}
  errors.blankEx     = await err(JSON.stringify({ projectName: '   ', template: 'example' }))
  errors.slashEx     = await err(JSON.stringify({ projectName: 'a/b', template: 'example' }))
  errors.extraEx     = await err(JSON.stringify({ projectName: 'x', template: 'example', extraKey: 1 }))
  errors.tmplNum     = await err(JSON.stringify({ projectName: 'x', template: 5 }))
  errors.badJsonEx   = await err('{not json')
  errors.numRoot     = await err('123')
  const anon = await call('/project/new', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': 'faketoken', accept: 'application/json' }, body: '{"projectName":"x","template":"example"}' })
  mongoDeletePrefix(PREFIX, mongoC)
  return { create: { ...cr, pid }, state, errors, anon }
}

function errCompare(a: Record<string, R>, b: Record<string, R>, tag: string): string[] {
  const p: string[] = []
  for (const k of Object.keys(a).sort()) {
    const x = a[k], y = b[k]
    if (!y || x.status !== y.status || x.ct !== y.ct || x.body !== y.body) {
      p.push(`${tag}:${k}: N[s=${x.status} ct=${x.ct} b=${x.body.slice(0, 70)}] G[s=${y?.status ?? 'N/A'} ct=${y?.ct ?? ''} b=${(y?.body || '').slice(0, 70)}]`)
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
  if (na !== nb) p.push(`${tag}:create-body N[${na.slice(0, 90)}] G[${nb.slice(0, 90)}]`)
  if (a.state.trim() !== b.state.trim()) p.push(`${tag}:state N=${a.state.slice(0, 200)} ... G=${b.state.slice(0, 200)} ...`)
  return p
}

test.describe.serial('web-go P4.7b flip gate (WEB_GO_PLAN P4.7b example create)', () => {
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

  test('leg 1: Node baseline (example create + state + battery)', async () => {
    test.setTimeout(200_000)
    dexe(overleafC, FLIP('web-p4g.conf', 'strip'), true); await nginxSettled()
    mongoDeletePrefix(PREFIX, mongoC)
    leg1 = await battery(mongoC, overleafC)
    expect(leg1.create.status, 'create status').toBe(200)
    expect(leg1.create.ct).toBe('application/json')
    expect(leg1.create.body).toContain('project_id')
    const st = JSON.parse(leg1.state)
    expect(st.present, 'project present').toBe(true)
    expect(st.docs, 'docs set').toEqual(['main.tex', 'sample.bib'])
    expect(st.fileRefs.length, 'fileRef count').toBe(1)
    expect(st.fileRefs[0].n, 'fileRef name').toBe('frog.jpg')
    expect(st.fileRefs[0].rev, 'fileRef rev').toBe(0)
    expect(st.fileRefs[0].lfd, 'fileRef linkedFileData').toBe(null)
    expect(st.fileRefs[0].hash, 'fileRef hash').toBe(FROG_HASH)
    expect(st.docLines['main.tex'], 'main.tex lines').toBe(MAIN_LINES)
    expect(st.docLines['sample.bib'], 'sample.bib lines').toBe(BIB_LINES)
    expect(st.blob['frog.jpg'] && st.blob['frog.jpg'].md5, 'frog blob md5').toBe(FROG_MD5)
    expect(st.blob['frog.jpg'] && st.blob['frog.jpg'].size, 'frog blob size').toBe(FROG_SIZE)
    expect(st.folders, 'folders').toBe(0)
    expect(st.rootDocHex, 'rootDoc set').toBe(true)
    expect(leg1.errors.blankEx.status).toBe(400)
    expect(leg1.errors.blankEx.body).toBe('Project name cannot be blank')
    expect(leg1.errors.slashEx.body).toBe('Project name cannot contain / characters')
    expect(leg1.errors.extraEx.body).toContain('Unrecognized key')
    expect(leg1.errors.tmplNum.body).toContain('expected string, received')
    expect(leg1.errors.numRoot.body).toBe('{}')
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
