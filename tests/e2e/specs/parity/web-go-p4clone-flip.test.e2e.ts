/**
 * WEB-GO P4.9 FLIP GATE (WEB_GO_PLAN.md P4.9 — project clone):
 *
 *   Route (Go shadow at 127.0.0.1:4010, flips/web-p4clone.conf):
 *     POST /Project/:Project_id/clone   (ensureUserCanReadProject)
 *       body { projectName?: string, isDebugCopy?/cloneHistory?/cloneRanges?: boolean,
 *              tags?: [{ id: objectId }] }   (zod strictObject)
 *     Node engine: flush(src) -> blank project (30 keys, NO segmentation —
 *       analytics-only) -> setCompiler -> copy docs (docstore rev 0) ->
 *       copy blobs (v1 copyBlob ?copyFrom=src) -> rootFolder structure +
 *       version=1 -> setRootDoc.
 *     Response: 200 application/json {name, lastUpdated, project_id, owner_ref,
 *              owner:{first_name,last_name,email,_id}}
 *
 *   leg 1  Node baseline      leg 2  FLIP ON — Go      leg 3  FLIP OFF — Node
 *
 *   Contract pins (live oracle, A/B-verified 2026-09-15):
 *     - success 200 body shape; cloned project: 30 keys, docs [main.tex,
 *       sample.bib] (118/10 lines), fileRef [frog.jpg same git-blob hash],
 *       fileRef {name,created,rev,hash,_id} WITHOUT linkedFileData (Node's
 *       clone File shape), rootDoc_id = copied main.tex, version=1.
 *     - frog blob retrievable from the NEW project (md5 665777aa6c7c..., 97080B).
 *     - source project/docstore/blob UNCHANGED.
 *     - {} (no projectName) -> **500 HTML "Something went wrong"**  (Node quirk:
 *       `newProjectName.trim()` TypeError — replicated)
 *       "   " -> 400 text/plain blank; "a/b" -> 400 text/plain slash;
 *       unknown key / wrong types -> 404→400 application/json zod errors;
 *       missing project -> 404 HTML page; malformed -> 404 JSON validation;
 *       non-member -> 403 restricted; anonymous -> 403 Forbidden (CSRF).
 *
 *   Run: npx playwright test -g "web-go P4.9 clone flip gate"
 */
import { execFileSync } from 'child_process'
import { test, expect } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'
import crypto from 'crypto'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const OTHER = { email: 'e2e-tpladmin@e2e.test', password: 'Ol-Fixture-7tW4' }
const PREFIX = 'webgo-p4cl-'
const NAME = PREFIX + 'copy'

const FROG_MD5 = '665777aa6c7c48794db02f5d69ccc24a'
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
  .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
  .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
  .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
  .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
  .replace(/\b[0-9a-f]{24}\b/g, '<HEX>')
  .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{1,9}Z/g, '<TS>')

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

async function login(user: { email: string; password: string }): Promise<{ ck: string; csrf: string }> {
  const page = await call('/login')
  const csrf = (page.body.match(/name=["']ol-csrfToken["']\s+content=["']([^"']+)"/) || [])[1] || ''
  let ck = ''
  {
    const m = (page.setcookie || '').match(/overleaf\.sid=[^;\n]+/)
    if (m) ck = m[0]
  }
  const logged = await call('/login', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body: JSON.stringify(user) })
  expect(logged.status, `login status ${user.email}`).toBe(200)
  const sid = ((logged.setcookie || '').split('\n').find((l) => l.includes('overleaf.sid')) || '').replace(/^overleaf\.sid=/, '').split(';')[0]
  if (sid) ck = `overleaf.sid=${sid}`
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body.trim() }
}

function clearRateLimits(): void {
  try {
    const out = execFileSync('docker', ['ps', '--filter', 'name=ol-e2e-redis', '--format', '{{.Names}}'], { encoding: 'utf8' })
    const rc = out.split('\n').find(Boolean)
    if (!rc) return
    const keys = execFileSync('docker', ['exec', rc, 'sh', '-c', 'redis-cli --scan --pattern "rate-limit:*"'], { encoding: 'utf8' }).split('\n').filter(Boolean)
    for (const k of keys) execFileSync('docker', ['exec', rc, 'redis-cli', 'DEL', k], { encoding: 'utf8' })
  } catch {}
}

function cleanup(mongoC: string): void {
  dexe(mongoC, `mongosh --quiet sharelatex --eval '
    db.projects.deleteMany({name:{$regex:"^${PREFIX}"}});
    db.deletedProjects.deleteMany({"project.name":{$regex:"^${PREFIX}"}});
  '`, true)
}

// state capture (in-container; runs from /overleaf/services/web for the
// 'mongodb' driver). Id-free: hex-><HEX>, ISO ts-><TS>.
const EXCAP = `
import crypto from 'node:crypto';
import { MongoClient, ObjectId } from 'mongodb';
const [src, nw] = process.argv.slice(2, 4);
const M = process.env.OVERLEAF_MONGO_URL || 'mongodb://mongo:27017/sharelatex?replicaSet=overleaf';
const client = new MongoClient(M); await client.connect();
const db = client.db('sharelatex');
const doc = async (pid) => {
  const p = await db.collection('projects').findOne({ _id: new ObjectId(pid) });
  if (!p) return null;
  const rf = (p.rootFolder || [])[0] || {};
  return {
    keys: Object.keys(p).sort(),
    name: p.name, compiler: p.compiler, version: p.version,
    hasSeg: ('segmentation' in p),
    rootDocHex: /^[0-9a-f]{24}$/.test(String(p.rootDoc_id || '')),
    rf: {
      keys: Object.keys(rf).join(','),
      name: rf.name,
      docs: (rf.docs || []).map(d => ({ n: d.name, keys: Object.keys(d).join(',') })),
      fileRefs: (rf.fileRefs || []).map(f => ({
        n: f.name, rev: f.rev, hash: f.hash,
        lf: f.linkedFileData === undefined ? 'undef' : String(f.linkedFileData),
        keys: Object.keys(f).join(','),
      })),
    },
  };
};
const docLines = async (pid) => {
  const p = await db.collection('projects').findOne({ _id: new ObjectId(pid) });
  const docs = (((p.rootFolder || [])[0] || {}).docs || []).map(d => [d.name, String(d._id)]);
  const out = {};
  for (const [n, did] of docs) {
    try {
      const r = await fetch('http://127.0.0.1:3016/project/' + pid + '/doc/' + did);
      const j = JSON.parse(await r.text());
      out[n] = Array.isArray(j.lines) ? j.lines.length : 'NOLINES';
    } catch (e) { out[n] = 'ERR'; }
  }
  return out;
};
const blob = async (pid) => {
  const v1 = process.env.V1_HISTORY_URL || 'http://127.0.0.1:3100/api';
  const auth = 'Basic ' + Buffer.from(process.env.V1_HISTORY_USER || 'staging' + ':' + (process.env.V1_HISTORY_PASSWORD || '')).toString('base64');
  const p = await db.collection('projects').findOne({ _id: new ObjectId(pid) });
  const fr = ((((p.rootFolder || [])[0] || {}).fileRefs) || [])[0];
  if (!fr) return null;
  try {
    const r = await fetch(v1 + '/projects/' + pid + '/blobs/' + fr.hash, { headers: { authorization: auth } });
    if (r.status !== 200) return { status: r.status };
    const buf = Buffer.from(await r.arrayBuffer());
    return { md5: crypto.createHash('md5').update(buf).digest('hex'), size: buf.length };
  } catch (e) { return { err: String(e).slice(0, 40) }; }
};
const out = { src: { doc: await doc(src), lines: await docLines(src), blob: await blob(src) } };
out.new = { doc: await doc(nw), lines: await docLines(nw), blob: await blob(nw) };
console.log(JSON.stringify(out));
await client.close();
`

function exCapture(src: string, nw: string, overleafC: string): string {
  return dexe(overleafC, `
    cat > /overleaf/services/web/.excap-p4cl.mjs <<'EXO'
${EXCAP}
EXO
    cd /overleaf/services/web && node .excap-p4cl.mjs ${src} ${nw}; rc=$?; rm -f /overleaf/services/web/.excap-p4cl.mjs; exit $rc
  `, true).trim().split('\n').slice(-1)[0] || 'NO_CAPTURE'
}

interface Leg {
  createSrc: R
  clone: R & { newPid: string }
  battery: Record<string, R>
  anon: Record<string, R>
  state: string
}

async function battery(mongoC: string, overleafC: string): Promise<Leg> {
  cleanup(mongoC)
  clearRateLimits()
  const owner = await login(USER)
  const other = await login(OTHER)
  const H = (csrf: string, ck: string) => ({ headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck })

  const cr = await call('/project/new', { method: 'POST', ...H(owner.csrf, owner.ck), body: JSON.stringify({ projectName: 'webgo-p4cl-src', template: 'example' }) })
  expect(cr.status, 'src create').toBe(200)
  const src = (cr.body.match(/"project_id"\s*:\s*"([0-9a-f]{24})"/) || [])[1] || ''
  await sleep(500)

  const clone = await call(`/Project/${src}/clone`, { method: 'POST', ...H(owner.csrf, owner.ck), body: JSON.stringify({ projectName: NAME }) })
  expect(clone.status, 'clone status').toBe(200)
  const newPid = (clone.body.match(/"project_id"\s*:\s*"([0-9a-f]{24})"/) || [])[1] || ''
  await sleep(500)

  const battery: Record<string, R> = {
    empty: await call(`/Project/${src}/clone`, { method: 'POST', ...H(owner.csrf, owner.ck), body: '{}' }),
    blank: await call(`/Project/${src}/clone`, { method: 'POST', ...H(owner.csrf, owner.ck), body: JSON.stringify({ projectName: '   ' }) }),
    slash: await call(`/Project/${src}/clone`, { method: 'POST', ...H(owner.csrf, owner.ck), body: JSON.stringify({ projectName: 'a/b' }) }),
    unknown: await call(`/Project/${src}/clone`, { method: 'POST', ...H(owner.csrf, owner.ck), body: JSON.stringify({ unknownKey: 1 }) }),
    nameNum: await call(`/Project/${src}/clone`, { method: 'POST', ...H(owner.csrf, owner.ck), body: JSON.stringify({ projectName: 5 }) }),
    dbgNum: await call(`/Project/${src}/clone`, { method: 'POST', ...H(owner.csrf, owner.ck), body: JSON.stringify({ projectName: 'webgo-p4cl-z', isDebugCopy: 3 }) }),
    tagsStr: await call(`/Project/${src}/clone`, { method: 'POST', ...H(owner.csrf, owner.ck), body: JSON.stringify({ projectName: 'webgo-p4cl-z', tags: 'x' }) }),
    arrayRoot: await call(`/Project/${src}/clone`, { method: 'POST', ...H(owner.csrf, owner.ck), headers: { 'content-type': 'application/json', 'x-csrf-token': owner.csrf, accept: 'application/json' }, cookie: owner.ck, body: '[1]' }),
    malformed: await call('/Project/not-an-oid/clone', { method: 'POST', ...H(owner.csrf, owner.ck), body: JSON.stringify({ projectName: 'x' }) }),
    missing: await call(`/Project/${randomHex24()}/clone`, { method: 'POST', ...H(owner.csrf, owner.ck), body: JSON.stringify({ projectName: 'x' }) }),
    nonmember: await call(`/Project/${src}/clone`, { method: 'POST', ...H(other.csrf, other.ck), body: JSON.stringify({ projectName: 'webgo-p4cl-nm' }) }),
  }
  const anon: Record<string, R> = {
    clone: await call(`/Project/${src}/clone`, { method: 'POST', headers: { accept: 'application/json', 'x-csrf-token': 'faketoken' }, body: '{"projectName":"x"}' }),
  }

  const state = exCapture(src, newPid, overleafC)
  cleanup(mongoC)
  return { createSrc: cr, clone: { ...clone, newPid }, battery, anon, state }
}

const randomHex24 = () => crypto.randomBytes(12).toString('hex')

function rCompare(name: string, a: R, b: R, tag: string, p: string[]): void {
  if (a.status !== b.status) p.push(`${tag}:${name} status N=${a.status} G=${b.status}`)
  if (a.ct !== b.ct) p.push(`${tag}:${name} ct N=${a.ct} G=${b.ct}`)
  const na = normBody(a.body), nb = normBody(b.body)
  if (na !== nb) {
    let i = 0
    while (i < Math.min(na.length, nb.length) && na[i] === nb[i]) i++
    p.push(`${tag}:${name} body len N=${na.length} G=${nb.length} @${i} N[...${na.slice(Math.max(0, i - 60), i + 60)}...] G[...${nb.slice(Math.max(0, i - 60), i + 60)}...]`)
  }
}

async function diffLegs(a: Leg, b: Leg, tag: string): Promise<string[]> {
  const p: string[] = []
  rCompare('createSrc', a.createSrc, b.createSrc, tag, p)
  rCompare('clone', a.clone, b.clone, tag, p)
  for (const k of Object.keys(a.battery).sort()) rCompare(`bat:${k}`, a.battery[k], b.battery[k] || { status: -1, ct: '', body: '' }, tag, p)
  for (const k of Object.keys(a.anon).sort()) rCompare(`anon:${k}`, a.anon[k], b.anon[k] || { status: -1, ct: '', body: '' }, tag, p)
  if (a.state !== b.state) {
    p.push(`${tag}:state N=${a.state.slice(0, 200)} G=${b.state.slice(0, 200)}`)
    try {
      const fs = await import('node:fs')
      fs.default.appendFileSync('/tmp/p4cl-debug.txt', `\n### ${tag}:state\nN=${a.state}\nG=${b.state}\n`)
    } catch {}
  }
  return p
}

test.describe.serial('web-go P4.9 clone flip gate', () => {
  let overleafC = '', mongoC = '', leg1: Leg | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf'); mongoC = runningContainer('ol-e2e-mongo')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'bin/web'), `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p4clone.conf'), `${overleafC}:/tmp/web-p4clone.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p4clone.conf /usr/local/share/overleaf-flips/web-p4clone.conf')
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) { const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim(); if (code === '200') break; await sleep(500) }
    dexe(overleafC, FLIP('web-p4clone.conf', 'strip'), true); await nginxSettled()
  }, 240_000)

  test.afterAll(async () => { try { dexe(overleafC, FLIP('web-p4clone.conf', 'strip'), true); await nginxSettled(); cleanup(mongoC) } catch {} })

  test('leg 1: Node baseline (clone + state + battery)', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('web-p4clone.conf', 'strip'), true); await nginxSettled()
    leg1 = await battery(mongoC, overleafC)

    // success contract
    expect(leg1.clone.ct, 'clone ct').toBe('application/json')
    expect(leg1.clone.body).toContain('"project_id"')
    expect(leg1.clone.body).toContain('"owner_ref"')
    expect(leg1.clone.body).toContain('"lastUpdated"')

    // state contract
    const st = JSON.parse(leg1.state)
    const nd = st.new.doc
    expect(nd.name, 'new name').toBe(NAME)
    expect(nd.keys.length, 'new doc key count').toBe(30)
    expect(nd.hasSeg, 'segmentation absent').toBe(false)
    expect(nd.compiler, 'compiler').toBe('pdflatex')
    expect(nd.version, 'version').toBe(1)
    expect(nd.rootDocHex, 'rootDoc set').toBe(true)
    expect(nd.rf.docs.map((d: any) => d.n), 'doc names').toEqual(['main.tex', 'sample.bib'])
    expect(nd.rf.fileRefs.length, 'fileRef count').toBe(1)
    expect(nd.rf.fileRefs[0].n, 'fileRef name').toBe('frog.jpg')
    expect(nd.rf.fileRefs[0].lf, 'clone fileRef linkedFileData absent').toBe('undef')
    expect(st.new.lines['main.tex'], 'new main.tex lines').toBe(MAIN_LINES)
    expect(st.new.lines['sample.bib'], 'new sample.bib lines').toBe(BIB_LINES)
    expect(st.new.blob.md5, 'new frog blob md5').toBe(FROG_MD5)
    expect(st.new.blob.size, 'new frog blob size').toBe(FROG_SIZE)
    // source untouched
    expect(st.src.doc.name, 'src name').toBe('webgo-p4cl-src')
    expect(st.src.lines['main.tex'], 'src main.tex lines').toBe(MAIN_LINES)
    expect(st.src.blob.md5, 'src frog blob md5').toBe(FROG_MD5)

    // battery contract
    expect(leg1.battery.empty.status, 'empty body 500 (Node quirk)').toBe(500)
    expect(leg1.battery.empty.ct).toBe('text/html')
    expect(leg1.battery.empty.body).toContain('Something went wrong')
    expect(leg1.battery.blank.status).toBe(400)
    expect(leg1.battery.blank.ct, 'blank ct').toBe('text/plain')
    expect(leg1.battery.blank.body).toBe('Project name cannot be blank')
    expect(leg1.battery.slash.status).toBe(400)
    expect(leg1.battery.slash.body).toBe('Project name cannot contain / characters')
    expect(leg1.battery.unknown.status).toBe(400)
    expect(leg1.battery.unknown.body).toBe('{"error":"Validation error: Unrecognized key: \\"unknownKey\\" at \\"body\\"","statusCode":400}')
    expect(leg1.battery.nameNum.status).toBe(400)
    expect(leg1.battery.nameNum.body).toBe('{"error":"Validation error: Invalid input: expected string, received number at \\"body.projectName\\"","statusCode":400}')
    expect(leg1.battery.dbgNum.status).toBe(400)
    expect(leg1.battery.dbgNum.body).toContain('expected boolean, received number')
    expect(leg1.battery.tagsStr.status).toBe(400)
    expect(leg1.battery.tagsStr.body).toContain('expected array, received string')
    expect(leg1.battery.arrayRoot.body).toBe('{"error":"Validation error: Invalid input: expected object, received array at \\"body\\"","statusCode":400}')
    expect(leg1.battery.malformed.status).toBe(404)
    expect(leg1.battery.malformed.body).toBe('{"error":"Validation error: Invalid Mongo ObjectId at \\"params.Project_id\\"","statusCode":404}')
    expect(leg1.battery.missing.status).toBe(404)
    expect(leg1.battery.missing.ct, 'missing ct').toBe('text/html')
    expect(leg1.battery.missing.body).toContain('Page Not Found - OlliTeX')
    expect(leg1.battery.nonmember.status).toBe(403)
    expect(leg1.battery.nonmember.body).toBe('{"message":"restricted"}')
    expect(leg1.anon.clone.status).toBe(403)
    expect(leg1.anon.clone.body).toBe('Forbidden')
    expect(leg1.anon.clone.ct).toBe('text/plain')
  }, 300_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('web-p4clone.conf', 'apply')); await nginxSettled()
    const leg2 = await battery(mongoC, overleafC)
    const p = await diffLegs(leg1!, leg2, 'GO')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 300_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('web-p4clone.conf', 'strip'), true); await nginxSettled()
    const leg3 = await battery(mongoC, overleafC)
    const p = await diffLegs(leg1!, leg3, 'NODE-rev')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 300_000)
})
