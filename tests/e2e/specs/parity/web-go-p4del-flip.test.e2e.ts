/**
 * WEB-GO P4.8 FLIP GATE (WEB_GO_PLAN.md P4.8 — project delete/restore):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p4del.conf):
 *     DELETE /Project/:Project_id            -> hard delete
 *         = document-updater flush+delete (204) [Node fallback chain replicated]
 *         + docstore POST /project/:id/archive (best-effort; no-op persistor here)
 *         + deletedProjects upsert { project: <full doc>, deleterData: {...}, __v:0 }
 *         + projects.deleteOne
 *     POST   /Project/:Project_id/restore    -> $unset { archived: true }
 *     POST   /Project/:Project_id/archive    -> (P4.6; flipped for fixture setup)
 *     POST   /project/new                    -> (P4.7; flipped for fixture creation)
 *
 *   All three mutations: 200 text/plain "OK" (sendStatus).
 *
 *   leg 1  Node baseline      leg 2  FLIP ON — Go      leg 3  FLIP OFF — Node
 *
 *   Contract pins (live oracle, A/B-verified against the Node service):
 *     - restore on an archived project removes the `archived` field entirely.
 *     - missing project -> 404 text/html NotFound page ("Page Not Found - OlliTeX")
 *     - malformed id    -> 404 application/json {"error":"Validation error: Invalid
 *                          Mongo ObjectId at \"params.Project_id\"","statusCode":404}
 *     - non-member      -> 403 application/json {"message":"restricted"}
 *     - anonymous       -> 403 text/plain "Forbidden" (CSRF before the route guards)
 *     - deleted project: gone from `projects`; `deletedProjects` record with the
 *       full project copy + 13-field deleterData (token/overleaf-id keys absent
 *       when undefined) + fresh subdoc _id; docstore doc still readable
 *       (archive is a no-op with the mongo/memory persistor backend).
 *
 *   Run: npx playwright test -g "web-go P4.8 delete/restore flip gate"
 */
import { execFileSync } from 'child_process'
import fs from 'node:fs'
import { test, expect } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'
import crypto from 'crypto'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const OTHER = { email: 'e2e-tpladmin@e2e.test', password: 'Ol-Fixture-7tW4' }
const PREFIX = 'webgo-p4del-'

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
// normalize: id-free, nonce-free, csrf-free — comparable across legs/services
const normBody = (s: string) => s
  .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
  .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
  .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
  .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
  .replace(/\b[0-9a-f]{24}\b/g, '<HEX>')

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
  let ck = (page.setcookie || '').split('\n').find((l) => l.includes('overleaf.sid')) || ''
  const logged = await call('/login', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body: JSON.stringify(user) })
  expect(logged.status, `login status ${user.email}`).toBe(200)
  const sid = ((logged.setcookie || '').split('\n').find((l) => l.includes('overleaf.sid')) || '').replace(/^overleaf\.sid=/, '').split(';')[0]
  if (sid) ck = `overleaf.sid=${sid}`
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body.trim() }
}

function cleanup(mongoC: string): void {
  dexe(mongoC, `mongosh --quiet sharelatex --eval '
    db.projects.deleteMany({name:{$regex:"^${PREFIX}"}});
    db.deletedProjects.deleteMany({"project.name":{$regex:"^${PREFIX}"}});
  '`, true)
}

// login-rate-limit hygiene (host-side): the 3 legs log in 2 users each;
// without a clear, bursts trip the per-IP limiter (403 Forbidden on login).
function clearRateLimits(): void {
  try {
    const out = execFileSync('docker', ['ps', '--filter', 'name=ol-e2e-redis', '--format', '{{.Names}}'], { encoding: 'utf8' })
    const rc = out.split('\n').find(Boolean)
    if (!rc) return
    const keys = execFileSync('docker', ['exec', rc, 'sh', '-c', 'redis-cli --scan --pattern "rate-limit:*"'], { encoding: 'utf8' }).split('\n').filter(Boolean)
    for (const k of keys) execFileSync('docker', ['exec', rc, 'redis-cli', 'DEL', k], { encoding: 'utf8' })
  } catch {}
}

// state capture (in-container ESM; runs from /overleaf/services/web for the
// 'mongodb' driver resolution). Output: id-free + timestamp-free + presence-
// based, directly comparable across legs.
function exCapture(args: string, which: 'pre' | 'post', overleafC: string): string {
  return dexe(overleafC, `
    cat > /overleaf/services/web/.excap-p4del.mjs <<'EXO'
import { MongoClient, ObjectId } from 'mongodb';
const [A, B, C] = process.argv.slice(2, 5);
const which = process.argv[5];
const M = process.env.OVERLEAF_MONGO_URL || 'mongodb://mongo:27017/sharelatex?replicaSet=overleaf';
const client = new MongoClient(M); await client.connect();
const db = client.db('sharelatex');
const proj = async (pid) => {
  const p = await db.collection('projects').findOne({ _id: new ObjectId(pid) });
  if (!p) return { present: false };
  return {
    present: true, name: p.name,
    archivedAbsent: !('archived' in p),
    archivedHasOwner: !!(p.archived && p.archived.some(x => String(x) === String(p.owner_ref))),
    collabEmpty: ((p.collablator_refs) || []).length === 0,
    rootDocs: (((( p.rootFolder || [])[0]) || {}).docs || []).map(d => d.name).sort(),
  };
};
const out = { A: await proj(A), B: await proj(B) };
if (which === 'post') {
  const c = await proj(C);
  out.C_present = !!c.present;
  const rec = await db.collection('deletedProjects').findOne({ 'deleterData.deletedProjectId': new ObjectId(C) });
  if (rec) {
    const dd = rec.deleterData || {};
    const arr = (v) => (Array.isArray(v) ? v.length : -1);
    const p = rec.project || {};
    out.C_rec = {
      found: true,
      topKeys: Object.keys(rec).sort(),
      __v: (rec.__v === undefined ? null : rec.__v),
      dd: {
        fieldKeys: Object.keys(dd).sort(),
        deletedReason: dd.deletedReason,
        has_deleterId: !!dd.deleterId,
        has_deleterIpAddress: typeof dd.deleterIpAddress === 'string' && dd.deleterIpAddress.length > 0,
        has_deletedAt: !!dd.deletedAt,
        has_deletedProjectId: !!dd.deletedProjectId,
        ownerSet: !!dd.deletedProjectOwnerId,
        arrCols: arr(dd.deletedProjectCollaboratorIds),
        arrRO: arr(dd.deletedProjectReadOnlyIds),
        arrRev: arr(dd.deletedProjectReviewerIds),
        arrRWTokAcc: arr(dd.deletedProjectReadWriteTokenAccessIds),
        arrROTokAcc: arr(dd.deletedProjectReadOnlyTokenAccessIds),
        overleafHistSet: !!dd.deletedProjectOverleafHistoryId,
        hasOverleafId: 'deletedProjectOverleafId' in dd,
        hasRWTok: 'deletedProjectReadWriteToken' in dd,
        hasROTok: 'deletedProjectReadOnlyToken' in dd,
        lastUpdSet: !!dd.deletedProjectLastUpdatedAt,
        subHasId: !!dd._id,
      },
      project: {
        keyCount: Object.keys(p).length,
        name: p.name,
        ownerSet: !!p.owner_ref,
        rootDocHex: /^[0-9a-f]{24}$/.test(String(p.rootDoc_id || '')),
        collabEmpty: ((p.collablator_refs) || []).length === 0,
        tokensReadAndWriteAbsent: !(p.tokens && 'readAndWrite' in (p.tokens || {})),
        overleafHistorySet: !!(p.overleaf && p.overleaf.history && p.overleaf.history.id),
        rootDocs: (((( p.rootFolder || [])[0]) || {}).docs || []).map(d => d.name).sort(),
      },
    };
    const doc = (((( rec.project || {}).rootFolder || [])[0] || {}).docs || [])[0];
    if (doc && doc._id) {
      try {
        const r = await fetch('http://127.0.0.1:3016/project/' + C + '/doc/' + String(doc._id));
        out.docvisible = { status: r.status };
        await r.text();
      } catch (e) { out.docvisible = { err: String(e).slice(0, 40) }; }
    } else { out.docvisible = { nodoc: true }; }
  } else { out.C_rec = { found: false }; }
}
console.log(JSON.stringify(out));
await client.close();
EXO
    cd /overleaf/services/web && node .excap-p4del.mjs ${args} ${which}; rc=$?; rm -f /overleaf/services/web/.excap-p4del.mjs; exit $rc
  `, true).trim().split('\n').slice(-1)[0] || 'NO_CAPTURE'
}

const randomHex24 = () => crypto.randomBytes(12).toString('hex')

interface Leg {
  creates: [R, R, R]
  archive: R
  restore: R
  del: R
  battery: Record<string, R>
  anon: Record<string, R>
  pre: string
  post: string
}

async function battery(mongoC: string, overleafC: string): Promise<Leg> {
  cleanup(mongoC)
  clearRateLimits()
  const owner = await login(USER)
  const other = await login(OTHER)
  const H = (csrf: string) => ({ 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' })

  const mk = (name: string) => call('/project/new', { method: 'POST', headers: H(owner.csrf), cookie: owner.ck, body: JSON.stringify({ projectName: name }) })
  const crA = await mk(PREFIX + 'a'); const crB = await mk(PREFIX + 'b'); const crC = await mk(PREFIX + 'c')
  expect([crA, crB, crC].map((r) => r.status).join(','), 'create statuses').toBe('200,200,200')
  const pid = (r: R) => (r.body.match(/"project_id"\s*:\s*"([0-9a-f]{24})"/) || [])[1] || ''
  const pidA = pid(crA), pidB = pid(crB), pidC = pid(crC)
  expect(pidA.length >= 24 && pidB.length >= 24 && pidC.length >= 24, 'pids captured').toBe(true)
  await sleep(200)

  const arch = await call(`/Project/${pidB}/archive`, { method: 'POST', headers: H(owner.csrf), cookie: owner.ck })
  const pre = exCapture(`${pidA} ${pidB} ${pidC}`, 'pre', overleafC)
  const restore = await call(`/Project/${pidB}/restore`, { method: 'POST', headers: H(owner.csrf), cookie: owner.ck })
  const miss = randomHex24()

  const battery: Record<string, R> = {
    delMalformed: await call('/Project/not-an-oid', { method: 'DELETE', headers: H(owner.csrf), cookie: owner.ck }),
    restoreMalformed: await call('/Project/not-an-oid/restore', { method: 'POST', headers: H(owner.csrf), cookie: owner.ck }),
    delMissing: await call(`/Project/${miss}`, { method: 'DELETE', headers: H(owner.csrf), cookie: owner.ck }),
    restoreMissing: await call(`/Project/${miss}/restore`, { method: 'POST', headers: H(owner.csrf), cookie: owner.ck }),
    delNonmember: await call(`/Project/${pidA}`, { method: 'DELETE', headers: H(other.csrf), cookie: other.ck }),
    restoreNonmember: await call(`/Project/${pidB}/restore`, { method: 'POST', headers: H(other.csrf), cookie: other.ck }),
  }
  const anon: Record<string, R> = {
    del: await call(`/Project/${pidA}`, { method: 'DELETE', headers: { accept: 'application/json', 'x-csrf-token': 'faketoken' }, body: '' }),
    restore: await call(`/Project/${pidB}/restore`, { method: 'POST', headers: { accept: 'application/json', 'x-csrf-token': 'faketoken' }, body: '' }),
  }

  const del = await call(`/Project/${pidC}`, { method: 'DELETE', headers: H(owner.csrf), cookie: owner.ck })
  await sleep(400)
  const post = exCapture(`${pidA} ${pidB} ${pidC}`, 'post', overleafC)
  return { creates: [crA, crB, crC], archive: arch, restore, del, battery, anon, pre, post }
}

function rCompare(name: string, a: R, b: R, tag: string, p: string[]): void {
  if (a.status !== b.status) p.push(`${tag}:${name} status N=${a.status} G=${b.status}`)
  if (a.ct !== b.ct) p.push(`${tag}:${name} ct N=${a.ct} G=${b.ct}`)
  const na = normBody(a.body), nb = normBody(b.body)
  if (na !== nb) {
    let i = 0
    while (i < Math.min(na.length, nb.length) && na[i] === nb[i]) i++
    p.push(`${tag}:${name} body len N=${na.length} G=${nb.length} firstDiff@${i} N[...${na.slice(Math.max(0, i - 60), i + 60)}...] G[...${nb.slice(Math.max(0, i - 60), i + 60)}...]`)
    try { fs.appendFileSync('/tmp/p4del-debug.txt', `${tag}:${name}\n===N===\n${na}\n===G===\n${nb}\n`) } catch {}
  }
}

function diffLegs(a: Leg, b: Leg, tag: string): string[] {
  const p: string[] = []
  for (let i = 0; i < 3; i++) rCompare(`create[${i}]`, a.creates[i], b.creates[i], tag, p)
  rCompare('archive', a.archive, b.archive, tag, p)
  rCompare('restore', a.restore, b.restore, tag, p)
  rCompare('del', a.del, b.del, tag, p)
  for (const k of Object.keys(a.battery).sort()) rCompare(`bat:${k}`, a.battery[k], b.battery[k] || { status: -1, ct: '', body: '' }, tag, p)
  for (const k of Object.keys(a.anon).sort()) rCompare(`anon:${k}`, a.anon[k], b.anon[k] || { status: -1, ct: '', body: '' }, tag, p)
  if (a.pre !== b.pre) {
    const an = JSON.parse(a.pre), bn = JSON.parse(b.pre)
    const d = JSON.stringify(diffObj(an, bn))
    p.push(`${tag}:pre diff ${d.slice(0, 300)}`)
    try { fs.appendFileSync('/tmp/p4del-debug.txt', `\n### ${tag}:pre\nN=${a.pre}\nG=${b.pre}\nDIFF=${d}\n`) } catch {}
  }
  if (a.post !== b.post) {
    const an = JSON.parse(a.post), bn = JSON.parse(b.post)
    const d = JSON.stringify(diffObj(an, bn))
    p.push(`${tag}:post diff ${d.slice(0, 300)}`)
    try { fs.appendFileSync('/tmp/p4del-debug.txt', `\n### ${tag}:post\nN=${a.post}\nG=${b.post}\nDIFF=${d}\n`) } catch {}
  }
  return p
}

function diffObj(a: any, b: any): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  const keys = new Set([...Object.keys(a), ...Object.keys(b)])
  for (const k of keys) {
    const av = JSON.stringify(a[k]), bv = JSON.stringify(b[k])
    if (av !== bv) {
      if (a[k] && b[k] && typeof a[k] === 'object' && typeof b[k] === 'object' && !Array.isArray(a[k])) out[k] = diffObj(a[k], b[k])
      else out[k] = { N: a[k], G: b[k] }
    }
  }
  return out
}

test.describe.serial('web-go P4.8 delete/restore flip gate', () => {
  let overleafC = '', mongoC = '', leg1: Leg | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf'); mongoC = runningContainer('ol-e2e-mongo')
    const repoBin = path.resolve(REPO_ROOT, 'bin/web')
    execFileSync('docker', ['cp', repoBin, `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p4del.conf'), `${overleafC}:/tmp/web-p4del.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p4del.conf /usr/local/share/overleaf-flips/web-p4del.conf')
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) { const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim(); if (code === '200') break; await sleep(500) }
    dexe(overleafC, FLIP('web-p4del.conf', 'strip'), true); await nginxSettled()
  }, 240_000)

  test.afterAll(async () => { try { dexe(overleafC, FLIP('web-p4del.conf', 'strip'), true); await nginxSettled(); cleanup(mongoC) } catch {} })

  test('leg 1: Node baseline (delete/restore + state + battery)', async () => {
    test.setTimeout(240_000)
    dexe(overleafC, FLIP('web-p4del.conf', 'strip'), true); await nginxSettled()
    leg1 = await battery(mongoC, overleafC)

    // success contract
    expect(leg1.archive.status, 'archive status').toBe(200)
    expect(leg1.archive.ct, 'archive ct').toBe('text/plain')
    expect(leg1.archive.body, 'archive body').toBe('OK')
    expect(leg1.restore.status, 'restore status').toBe(200)
    expect(leg1.restore.ct, 'restore ct').toBe('text/plain')
    expect(leg1.restore.body, 'restore body').toBe('OK')
    expect(leg1.del.status, 'delete status').toBe(200)
    expect(leg1.del.ct, 'delete ct').toBe('text/plain')
    expect(leg1.del.body, 'delete body').toBe('OK')

    // state contract
    const pre = JSON.parse(leg1.pre), post = JSON.parse(leg1.post)
    expect(pre.B.present, 'B present pre').toBe(true)
    expect(pre.B.archivedHasOwner, 'B archived (owner) after archive op').toBe(true)
    expect(post.B.present, 'B present post').toBe(true)
    expect(post.B.archivedAbsent, 'B.archived removed by restore').toBe(true)
    expect(post.A.present, 'A survives C delete').toBe(true)
    expect(post.C_present, 'C removed from projects').toBe(false)
    expect(post.C_rec.found, 'deletedProjects record exists').toBe(true)
    expect(post.C_rec.topKeys, 'record top keys (sorted)').toEqual(['__v', '_id', 'deleterData', 'project'])
    expect(post.C_rec.__v, '__v').toBe(0)
    expect(post.C_rec.dd.deletedReason, 'deletedReason').toBe('user')
    expect(post.C_rec.dd.has_deleterId, 'deleterId set').toBe(true)
    expect(post.C_rec.dd.has_deleterIpAddress, 'deleterIpAddress set').toBe(true)
    expect(post.C_rec.dd.has_deletedAt, 'deletedAt set').toBe(true)
    expect(post.C_rec.dd.ownerSet, 'deletedProjectOwnerId set').toBe(true)
    expect(post.C_rec.dd.arrCols, 'collab ids array').toBe(0)
    expect(post.C_rec.dd.arrRO, 'ro ids array').toBe(0)
    expect(post.C_rec.dd.arrRev, 'reviewer ids array').toBe(0)
    expect(post.C_rec.dd.arrRWTokAcc, 'rw token access array').toBe(0)
    expect(post.C_rec.dd.arrROTokAcc, 'ro token access array').toBe(0)
    expect(post.C_rec.dd.overleafHistSet, 'overleaf history id set').toBe(true)
    expect(post.C_rec.dd.hasOverleafId, 'overleaf id (absent)').toBe(false)
    expect(post.C_rec.dd.hasRWTok, 'rw token (absent)').toBe(false)
    expect(post.C_rec.dd.hasROTok, 'ro token (absent)').toBe(false)
    expect(post.C_rec.dd.lastUpdSet, 'lastUpdatedAt set').toBe(true)
    expect(post.C_rec.dd.subHasId, 'deleterData subdoc _id').toBe(true)
    expect(post.C_rec.project.name, 'record project name').toBe(PREFIX + 'c')
    expect(post.C_rec.project.ownerSet, 'record project owner').toBe(true)
    expect(post.C_rec.project.overleafHistorySet, 'record overleaf history').toBe(true)
    expect(post.docvisible.status, 'docstore doc still visible (archive no-op)').toBe(200)

    // battery contract
    expect(leg1.battery.delMalformed.status, 'malformed 404').toBe(404)
    expect(leg1.battery.delMalformed.ct).toBe('application/json')
    expect(leg1.battery.delMalformed.body).toBe('{"error":"Validation error: Invalid Mongo ObjectId at \\"params.Project_id\\"","statusCode":404}')
    expect(leg1.battery.restoreMalformed.status).toBe(404)
    expect(leg1.battery.restoreMalformed.body).toBe(leg1.battery.delMalformed.body)
    expect(leg1.battery.delMissing.status, 'missing 404').toBe(404)
    expect(leg1.battery.delMissing.ct, 'missing ct html').toBe('text/html')
    expect(leg1.battery.delMissing.body).toContain('Page Not Found - OlliTeX')
    expect(leg1.battery.restoreMissing.status).toBe(404)
    expect(leg1.battery.delNonmember.status, 'nonmember 403').toBe(403)
    expect(leg1.battery.delNonmember.body).toBe('{"message":"restricted"}')
    expect(leg1.battery.restoreNonmember.status).toBe(403)
    expect(leg1.battery.restoreNonmember.body).toBe('{"message":"restricted"}')
    expect(leg1.anon.del.status, 'anon 403').toBe(403)
    expect(leg1.anon.del.ct).toBe('text/plain')
    expect(leg1.anon.del.body).toBe('Forbidden')
    expect(leg1.anon.restore.status).toBe(403)
    expect(leg1.anon.restore.body).toBe('Forbidden')
  }, 240_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(240_000)
    dexe(overleafC, FLIP('web-p4del.conf', 'apply')); await nginxSettled()
    const leg2 = await battery(mongoC, overleafC)
    const p = diffLegs(leg1!, leg2, 'GO')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 240_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(240_000)
    dexe(overleafC, FLIP('web-p4del.conf', 'strip'), true); await nginxSettled()
    const leg3 = await battery(mongoC, overleafC)
    const p = diffLegs(leg1!, leg3, 'NODE-rev')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 240_000)
})
