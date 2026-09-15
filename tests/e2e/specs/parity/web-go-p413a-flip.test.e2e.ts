// P4.13a: project upload (POST /Project/:Project_id/upload) parity —
// Node baseline -> Go parity -> Node re-baseline through the 7420 nginx
// surface with the web-p413a flip applied for leg 2 only.
//
// Oracle (Node pinned, fresh sessions 2026-09-15, in-container probes):
//   200 {"success":true,"entity_id":"<hex24>","entity_type":"doc"}
//   200 {"success":true,"entity_id":"<hex24>","entity_type":"file","hash":"<sha40>"}
//   400 {"success":false,"error":"invalid_upload_request"}  (no qqfile part)
//   400 {"error":"Validation error: Invalid Mongo ObjectId at \"query.folder_id\"","statusCode":400}
//       (present-but-empty or malformed folder_id)
//   404 {"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}
//   404 HTML "Page Not Found" page (valid-but-ghost Project_id)
//   403 {"message":"restricted"} (accept: application/json, non-member)
//   403 text/plain "Forbidden" (anonymous — global CSRF runs first)
//   422 {"success":false,"error":"invalid_filename"}  (name empty/len>150, '../' dirty)
//   422 {"success":false,"error":"folder_not_found"}  (absent/ghost folder_id)
//   500 HTML generic error page (mail to adminEmail placeholder@example.com)
//       — multer LIMIT_UNEXPECTED_FILE for a file part whose field is not 'qqfile'
//
// Side-effect parity (mongo state dump per leg):
//   doc upload    → docstore POST (rev) + $push docs {name,_id} + DU add-doc
//   doc re-upload → DU setDocument only
//   file upload   → v1H blob PUT + $push fileRefs + DU add-file
//   file re-upload→ v1H blob PUT + $set fileRefs.idx + $inc rev + DU rename+add
//   file↔doc swaps→ $pull one side, $push the other + DU rename-*+add-*
//   project version increments and lastUpdatedBy is stable (e2e-user).
//
// The multipart upload must reach Go WITHOUT csrf consuming the body: the
// gate passes the token via the x-csrf-token header (Node csurf + Go core
// both accept it; Go checks the header before any FormValue body parse).

import { expect, test, beforeAll, afterAll } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dir = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(__dir, '..', '..', '..', '..')
const BASE = 'http://127.0.0.1:7420'
const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const FLIPCONF = 'web-p413a.conf'

const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const OTHER = { email: 'e2e-tpladmin@e2e.test', password: 'Ol-Fixture-7tW4' }
const GHOST = '666666666666666666666666'
const GHOSTRF = '6b000000000000000000bbbb'
const SEED = 'p413a-parity'

const TEXT = 'hello\nworld\n'
const BIG = Buffer.concat([Buffer.alloc(160, 0x41), Buffer.from('0123456789abcdef', 'hex'), Buffer.from('END\n')])
const NLBIN = Buffer.concat([Buffer.from([0x00, 0x41, 0x42]), Buffer.from('\r\n')])

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function dexe(c: string, cmd: string, capture = false): string {
  const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore' })
  return out || ''
}
function dexeQ(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'mongosh', '--quiet', 'sharelatex', '--eval', cmd], { encoding: 'utf8' }).trim()
}

function FLIP(conf: string, mode: 'apply' | 'strip'): string {
  const inc = `  include /etc/nginx/overleaf-flips/${conf};\n`
  if (mode === 'apply') return `
set -e
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/${conf} /etc/nginx/overleaf-flips/${conf}
# Defensive: drop stale flip includes from previously failed runs (orphaned
# includes break nginx -t and cascade-fail later gates in the batch).
node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"
grep -q "overleaf-flips/${conf}" "/etc/nginx/sites-enabled/overleaf.conf" && exit 0
node -e '
    const fs=require("fs");const v="/etc/nginx/sites-enabled/overleaf.conf";
    const inc="  include /etc/nginx/overleaf-flips/${conf};\\n\\n";
    let s=fs.readFileSync(v,"utf8");const l=s.split("\\n");
    const i=l.findIndex(x=>x.trim()==="location / {");
    if(i<0)throw new Error("location / not found");
    l.splice(i,0,inc);fs.writeFileSync(v,l.join("\\n"));'
nginx -t && nginx -s reload && sleep 2
`
  return `
set -e
if grep -q "overleaf-flips\\/${conf}" "/etc/nginx/sites-enabled/overleaf.conf"; then
  sed -i "/overleaf-flips\\/${conf}/d" "/etc/nginx/sites-enabled/overleaf.conf"
  nginx -t && nginx -s reload && sleep 2
fi
`
}

interface R { status: number; ct: string; body: string; setcookie?: string }
const normBody = (s: string) => s
  .replace(/\b[0-9a-f]{24}\b/g, '<HEX24>')
  .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
  .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
  .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
  .replace(/name="_csrf"\s+type="hidden"\s+value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
  .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z/g, '<TS>')

async function nginxSettled(timeoutMs = 15_000): Promise<void> {
  const t0 = Date.now()
  for (;;) {
    try { const r = await fetch(BASE + '/status', { redirect: 'manual' }); if (r.status >= 100) { await r.text().catch(() => {}); return } } catch {}
    if (Date.now() - t0 > timeoutMs) throw new Error('nginx settle')
    await sleep(300)
  }
}

async function call(p: string, init: RequestInit & { cookie?: string } = {}): Promise<R> {
  const h = { ...(init.headers || {}) as Record<string, string> }
  if (init.cookie) h['cookie'] = init.cookie as string
  const r = await fetch(BASE + p, { ...init, headers: h, redirect: 'manual' })
  const setcookie = (r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : ((r.headers.get('set-cookie') as string) || '')) as string
  const body = Buffer.from(await r.arrayBuffer()).toString('utf8')
  return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], body, setcookie }
}

async function login(user: { email: string; password: string }): Promise<{ ck: string; csrf: string }> {
  const page = await call('/login')
  const csrf = (page.body.match(/name=["']ol-csrfToken["']\s+content=["']([^"']+)"/) || [])[1] || ''
  const ck0 = ((page.setcookie || '').match(/overleaf\.sid=[^;\n]+/) || [])[0] || ''
  const logged = await call('/login', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck0 || undefined, body: JSON.stringify(user) })
  if (logged.status !== 200) throw new Error('login failed ' + user.email + ': ' + logged.status + ' ' + logged.body.slice(0, 200))
  const sid = ((logged.setcookie || '').split('\n').find((l) => l.includes('overleaf.sid')) || '').replace(/^overleaf\.sid=/, '').split(';')[0]
  const ck = sid ? ('overleaf.sid=' + sid) : ck0
  const cs = await call('/dev/csrf', { cookie: ck || undefined, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body.trim() || csrf }
}

// ---------- seed --------------------------------------------------------------
// Seed shape mirrors a real Node-created project (owner_ref / collab_refs /
// publicAccesLevel — CollaboratorsGetter.getProjectAccess reads the *_ref
// fields and 500s if owner_ref is missing). main.tex MUST also exist in the
// docstore or DU setDocument/structure calls 404 (loopback API, so register
// from inside the overleaf container).
function seed(): { pj: string; rf: string } {
  const out = dexeQ(mongoC, `
    const f = db.getSiblingDB('sharelatex');
    f.projects.deleteMany({ name: '${SEED}' });
    const uid = f.users.findOne({ email: 'e2e-user@e2e.test' })._id;
    const now = new Date();
    const rootId = new ObjectId(); const did = new ObjectId(); const hid = new ObjectId(); const fid = new ObjectId();
    const proj = {
      _id: new ObjectId(), name: '${SEED}', owner: uid, isCollaborative: true, version: 1,
      lastUpdated: now, lastUpdatedBy: uid,
      owner_ref: uid, collab_refs: [uid], readOnly_refs: [],
      tokenAccessReadAndWrite_refs: [], tokenAccessReadOnly_refs: [],
      publicAccesLevel: 'private', pendingEditor_refs: [], reviewer_refs: [], pendingReviewer_refs: [],
      rootFolder: [{ _id: rootId, name: '', docs: [{ name: 'main.tex', _id: did }],
        fileRefs: [{ name: 'notes.md', created: now, rev: 0, linkedFileData: null, hash: 'cafedeadcafedeadcafedeadcafedeadcafedead', _id: fid }],
        subFolders: [] }],
      collaborators: [{ type: 'user', id: uid, access: 'owner', accepted: true }],
      overleaf: { history: { id: hid, rangeMap: {} } },
    };
    f.projects.insertOne(proj);
    print(JSON.stringify({ pj: String(proj._id), rf: String(rootId), did: String(did) }));
  `)
  const ids = JSON.parse(out.split('\n').pop())
  // docstore registration of main.tex (DU setDoc + structure calls require it).
  const reg = dexe(overleafC, `PJID=${ids.pj} DID=${ids.did} /usr/bin/node --input-type=module -e '
    (async () => {
      const r = await fetch("http://127.0.0.1:3016/project/" + process.env.PJID + "/doc/" + process.env.DID, {
        method: "POST", headers: { "content-type": "application/json" },
        body: JSON.stringify({ lines: ["% !TEX", "main"], version: 0, ranges: {} }),
      });
      console.log("reg=" + r.status);
    })().catch((e) => console.log("ERR " + e));
  ' 2>&1`, true)
  if (!/reg=200/.test(reg)) throw new Error('docstore seed registration failed: ' + reg)
  return { pj: ids.pj, rf: ids.rf }
}

function stateDump(): Record<string, unknown> {
  const t = dexeQ(mongoC, `
    const p = db.projects.findOne({ name: '${SEED}' });
    const flat = [];
    (function rec(f) {
      for (const d of f.docs || []) flat.push('doc:' + d.name);
      for (const x of f.fileRefs || []) flat.push('file:' + x.name + ':rev' + x.rev + ':h' + String(x.hash || '').slice(0, 8));
      for (const s of f.subFolders || []) { flat.push('folder:' + s.name); rec(s); }
    })(p.rootFolder[0]);
    print(JSON.stringify({ flat: flat.sort(), ver: p.version, lub: String(p.lastUpdatedBy || '') }));
  `)
  try { return JSON.parse(t.split('\n').pop()) } catch { return { raw: t.slice(0, 300) } }
}

// ---------- battery -----------------------------------------------------------
async function battery(ids: { pj: string; rf: string }, U: { ck: string; csrf: string }, O: { ck: string; csrf: string }): Promise<{ cases: Record<string, R>; state: unknown }> {
  const cases: Record<string, R> = {}
  const hdr = (csrf: string, ck: string) => ({ 'x-csrf-token': csrf, cookie: ck, accept: 'application/json' }) as Record<string, string>
  const fd = (name: string, content: string | Uint8Array, field = 'qqfile', filename = content.length ? 'up.bin' : 'up.bin'): FormData => {
    const f = new FormData()
    f.append('name', name)
    if (field) f.append(field, new Blob([content as any], { type: 'application/octet-stream' }), filename)
    return f
  }
  const url = (q: string) => `/Project/${ids.pj}/upload${q}`
  const qrf = `?folder_id=${ids.rf}`

  // ---- write cases (each seeded project starts identical) ----
  cases.docNew = await call(url(qrf), { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('up.txt', TEXT) })
  cases.docUpsert = await call(url(qrf), { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('main.tex', TEXT) })
  cases.fileNew = await call(url(qrf), { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('pic2.png', new Uint8Array(BIG)) })
  cases.fileUpsert = await call(url(qrf), { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('pic.jpg', new Uint8Array(BIG)) })
  cases.crossDocToFile = await call(url(qrf), { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('main.tex', new Uint8Array(NLBIN)) })
  cases.crossFileToDoc = await call(url(qrf), { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('notes.md', TEXT) })

  // ---- error contract ----
  cases.badslash = await call(url(qrf), { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('../evil.ts', TEXT) })
  cases.badlong = await call(url(qrf), { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('a'.repeat(151) + '.tex', TEXT) })
  cases.badfolder = await call('/Project/' + ids.pj + '/upload?folder_id=notanoid', { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('z.txt', TEXT) })
  cases.ghostfolder = await call('/Project/' + ids.pj + '/upload?folder_id=' + GHOSTRF, { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('u.txt', TEXT) })
  cases.nofolder = await call(url(''), { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('s.txt', TEXT) })
  cases.emptyfolder = await call(url('?folder_id='), { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('v.txt', TEXT) })
  cases.nofile = await call(url(qrf), { method: 'POST', headers: hdr(U.csrf, U.ck), body: (() => { const f = new FormData(); f.append('name', 'n.txt'); return f })() })
  cases.wrongfield = await call(url(qrf), { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('r.txt', TEXT, 'other') })

  // ---- auth surface ----
  cases.ghostProject = await call(`/Project/${GHOST}/upload?folder_id=${ids.rf}`, { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('g.txt', TEXT) })
  cases.badProject = await call('/Project/nonhex/upload?folder_id=' + ids.rf, { method: 'POST', headers: hdr(U.csrf, U.ck), body: fd('b.txt', TEXT) })
  cases.nonmember = await call(url(qrf), { method: 'POST', headers: hdr(O.csrf, O.ck), body: fd('evil.txt', TEXT) })
  cases.anon = await call(url(qrf), { method: 'POST', headers: { 'x-csrf-token': 'faketoken', accept: 'application/json' }, body: fd('anon.txt', TEXT) })

  const state = stateDump()
  return { cases, state }
}

type Leg = Awaited<ReturnType<typeof battery>>
let leg1: Leg

function diffLegs(lg: string, a: Leg, b: Leg): string[] {
  const ds: string[] = []
  for (const k of Object.keys(a.cases)) {
    const A = a.cases[k], B = b.cases[k]
    if (A.status !== B.status) { ds.push(`${lg}:${k} status N=${A.status} G=${B.status}`); continue }
    if (A.ct !== B.ct) { ds.push(`${lg}:${k} ct N=${A.ct} G=${B.ct}`); continue }
    const na = normBody(A.body), nb = normBody(B.body)
    if (na !== nb) ds.push(`${lg}:${k} body len N=${na.length} G=${nb.length}
  N: ${na.slice(0, 300)}
  G: ${nb.slice(0, 300)}`)
  }
  const ja = JSON.stringify(a.state), jb = JSON.stringify(b.state)
  if (ja !== jb) ds.push(`${lg}:state
  N: ${ja.slice(0, 500)}
  G: ${jb.slice(0, 500)}`)
  return ds
}

test.describe('@local web-go P4.13a (upload) parity', () => {
  let U: { ck: string; csrf: string }, O: { ck: string; csrf: string }
  let ids: { pj: string; rf: string }

  test.beforeAll(async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP(FLIPCONF, 'strip'), true); await nginxSettled()
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/' + FLIPCONF), `${overleafC}:/tmp/${FLIPCONF}`])
    dexe(overleafC, `mkdir -p /usr/local/share/overleaf-flips && cp /tmp/${FLIPCONF} /usr/local/share/overleaf-flips/${FLIPCONF}`)
    for (;;) { const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim(); if (code === '200') break; await sleep(500) }
    U = await login(USER)
    O = await login(OTHER)
    ids = seed()
  }, 300_000)

  test.afterAll(async () => {
    try { dexe(overleafC, FLIP(FLIPCONF, 'strip'), true); await nginxSettled() } catch {}
  })

  test('leg 1: Node baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP(FLIPCONF, 'strip'), true); await nginxSettled()
    ids = seed()
    leg1 = await battery(ids, U, O)
    const c = (k: string) => leg1.cases[k]
    // Node oracle pins (fresh sessions, 2026-09-15)
    expect(c('docNew').status).toBe(200)
    expect(c('docNew').ct).toBe('application/json')
    expect(c('docNew').body).toMatch(/"success":true,"entity_id":"[0-9a-f]{24}","entity_type":"doc"/)
    expect(c('fileNew').body).toMatch(/"entity_type":"file","hash":"[0-9a-f]{40}"/)
    expect(c('docUpsert').status).toBe(200)
    expect(c('crossDocToFile').status).toBe(200)
    expect(c('crossFileToDoc').status).toBe(200)
    expect(c('crossFileToDoc').body).toContain('"entity_type":"doc"')
    expect(c('badslash').status).toBe(422)
    expect(c('badslash').body).toBe('{"success":false,"error":"invalid_filename"}')
    expect(c('badlong').body).toBe('{"success":false,"error":"invalid_filename"}')
    expect(c('badfolder').status).toBe(400)
    expect(c('badfolder').body).toContain('Invalid Mongo ObjectId at \\\"query.folder_id\\\"')
    expect(c('emptyfolder').status).toBe(400)
    expect(c('emptyfolder').body).toContain('Invalid Mongo ObjectId at \\\"query.folder_id\\\"')
    expect(c('ghostfolder').body).toBe('{"success":false,"error":"folder_not_found"}')
    expect(c('nofolder').body).toBe('{"success":false,"error":"folder_not_found"}')
    expect(c('nofile').status).toBe(400)
    expect(c('nofile').body).toBe('{"success":false,"error":"invalid_upload_request"}')
    expect(c('wrongfield').status).toBe(500)
    expect(c('wrongfield').ct).toBe('text/html')
    expect(c('wrongfield').body).toContain('placeholder@example.com')
    expect(c('badProject').status).toBe(404)
    expect(c('badProject').body).toContain('Invalid Mongo ObjectId at \\\"params.Project_id\\\"')
    expect(c('ghostProject').status).toBe(404)
    expect(c('ghostProject').body).toContain('Page Not Found')
    expect(c('nonmember').status).toBe(403)
    expect(c('nonmember').body).toBe('{"message":"restricted"}')
    expect(c('anon').status).toBe(403)
    expect(c('anon').ct).toBe('text/plain')
    expect(c('anon').body).toBe('Forbidden')
  })

  test('leg 2: Go parity', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP(FLIPCONF, 'apply'), true); await nginxSettled()
    ids = seed()
    const leg2 = await battery(ids, U, O)
    dexe(overleafC, FLIP(FLIPCONF, 'strip'), true); await nginxSettled()
    const ds = diffLegs('p413a', leg1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('leg 3: Node re-baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP(FLIPCONF, 'strip'), true); await nginxSettled()
    ids = seed()
    const leg3 = await battery(ids, U, O)
    const d1 = diffLegs('p413a-leg3', leg1, leg3)
    if (d1.length) console.log(d1.join('\n'))
    expect(d1, d1.join('\n')).toHaveLength(0)
  })
})
