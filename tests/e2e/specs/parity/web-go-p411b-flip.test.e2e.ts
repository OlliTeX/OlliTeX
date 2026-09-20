// P4.11b: editor entity DELETION parity — DELETE /project/:id/{doc,file,folder}/:entity_id
// Three-leg: Node baseline -> Go parity -> Node re-baseline.
// Stateful battery: the deletion sequence mutates the seeded project; both
// legs re-seed fresh and run the identical order (pinned 2026-09-15).
import { expect, test, beforeAll, afterAll } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dir = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(__dir, '..', '..', '..', '..')
const BASE = 'http://127.0.0.1:7420'
const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'

const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const OTHER = { email: 'e2e-tpladmin@e2e.test', password: 'Ol-Fixture-7tW4' }
const GHOST = '666666666666666666666666'

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function dexe(c: string, cmd: string, capture = false): string {
  const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore' })
  return out || ''
}
function dexeQ(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'mongosh', '--quiet', 'sharelatex', '--eval', cmd], { encoding: 'utf8' }).trim()
}

function FLIP(conf: string, mode: 'apply' | 'strip'): string {
  if (mode === 'apply') return `
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
if grep -q "overleaf-flips\\/${conf}" "$vhost"; then
  sed -i "/overleaf-flips\\/${conf}/d" "$vhost"
  nginx -t && nginx -s reload && sleep 2
fi
`
}

interface R { status: number; ct: string; body: string; setcookie?: string }
const normVolatile = (s: string) =>
  s
    .replace(/\b[0-9a-f]{48}\b/g, '<TK>')
    .replace(/\b[0-9a-f]{24}\b/g, '<HEX24>')
const normBody = (s: string) => normVolatile(s)
  .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
  .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
  .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
  .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
  .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z/g, '<TS>')
  .replace(/\b1[5-9]\d{5,7}\b|\b2[0-9]{9}\b/g, '<EPOCH>')

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

function clearRateLimits(): void {
  try {
    const rc = execFileSync('docker', ['ps', '--filter', 'name=ol-e2e-redis', '--format', '{{.Names}}'], { encoding: 'utf8' }).split('\n').find(Boolean)
    if (!rc) return
    const keys = execFileSync('docker', ['exec', rc, 'sh', '-c', 'redis-cli --scan --pattern "rate-limit:*"'], { encoding: 'utf8' }).split('\n').filter(Boolean)
    for (const k of keys) execFileSync('docker', ['exec', rc, 'redis-cli', 'DEL', k])
  } catch {}
}

// ---------- seed / state ----------
interface Ids { a: string; x: string; sub: string; sub2: string; main: string; notes: string; deep: string; img: string; xm: string }
function seed(): Ids {
  const out = dexeQ(mongoC, `
    db.projects.deleteMany({name:{$in:["pf11b-mine","pf11b-x"]}});
    db.docstore_docs.deleteMany({project_name:{$in:["pf11b-mine","pf11b-x"]}});
    const uid = ObjectId("6aa4b8b573ef0e5094f4cbc0");
    const oidd = ObjectId("6aa4b8c0ee67ff98732d4947");
    const main = new ObjectId(), notes = new ObjectId(), deep = new ObjectId(), img = new ObjectId(), xm = new ObjectId();
    const sub = new ObjectId(), sub2 = new ObjectId();
    const a = db.projects.insertOne({_id:new ObjectId(), name:"pf11b-mine", owner_ref:uid, publicAccesLevel:"private",
      version:0, rootDoc_id: main,
      rootFolder:[{name:"", _id:new ObjectId(), docs:[{_id:main, name:"main.tex"}], fileRefs:[{_id:img, name:"img.png"}],
        folders:[{name:"sub", _id:sub, docs:[{_id:notes, name:"notes.tex"}], fileRefs:[],
          folders:[{name:"sub2", _id:sub2, docs:[{_id:deep, name:"deep.txt"}], fileRefs:[], folders:[]}]}]}]});
    const x = db.projects.insertOne({_id:new ObjectId(), name:"pf11b-x", owner_ref:oidd, publicAccesLevel:"private",
      version:0, rootDoc_id: xm,
      rootFolder:[{name:"", _id:new ObjectId(), docs:[{_id:xm, name:"xm.tex"}], fileRefs:[], folders:[]}]});
    print(JSON.stringify({a:String(a.insertedId), x:String(x.insertedId), main:String(main), notes:String(notes), deep:String(deep), img:String(img), xm:String(xm), sub:String(sub), sub2:String(sub2)}));
  `)
  const ids: Ids = JSON.parse(out.split('\n').pop())
  // docstore entries for every seeded doc (Node addDoc does this too; the
  // deletion cleanup PATCHes them — a missing entry is a distinct 404 case
  // covered separately by delOrphan).
  dexe(overleafC, `
    for spec in "${ids.a}:${ids.main}" "${ids.a}:${ids.notes}" "${ids.a}:${ids.deep}" "${ids.x}:${ids.xm}"; do
      pid=\${spec%%:*}; did=\${spec##*:};
      curl -s -o /dev/null -w "docstore-create %{http_code}\\n" -X POST http://127.0.0.1:3016/project/\$pid/doc/\$did \\
        -H 'content-type: application/json' -d '{"lines":["L"],"version":0,"ranges":{}}'
    done
  `, true)
  return ids
}

function stateDump(): string {
  return dexeQ(mongoC, `
    const out = {};
    for (const name of ["pf11b-mine","pf11b-x"]) {
      const p = db.projects.findOne({name});
      const flat = [];
      (function rec(f){ for (const d of f.docs||[]) flat.push("doc:"+d.name); for (const x of f.fileRefs||[]) flat.push("file:"+x.name); for (const s of f.folders||[]) { flat.push("folder:"+s.name); rec(s);} })(p.rootFolder[0]);
      const n = db.docs.countDocuments({project_id:String(p._id)});
      out[name] = { flat: flat.sort(), ver: p.version, lub: String(p.lastUpdatedBy||""), rootDoc: p.rootDoc_id ? "present" : "absent", docstore: n };
    }
    print(JSON.stringify(out));
  `)
}

// ---------- battery (stateful, identical order both legs) ----------
async function battery(ids: Ids, U: { ck: string; csrf: string }, O: { ck: string; csrf: string }): Promise<{ cases: Record<string, R>; state: string }> {
  const cases: Record<string, R> = {}
  const h = (csrf: string, ck: string) => ({ 'content-type': 'application/json', 'x-csrf-token': csrf, cookie: ck, accept: 'application/json' }) as Record<string, string>

  // ---- guards / validation (no tree mutation) ----
  cases.delRootFolder = await call(`/project/${ids.a}/folder/${rootFolderId()}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })
  cases.delGhostDoc = await call(`/project/${ids.a}/doc/${GHOST}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })
  cases.delBadProj = await call(`/project/notanoid/doc/${ids.main}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })
  cases.delBadEntity = await call(`/project/${ids.a}/doc/notanoid`, { method: 'DELETE', headers: h(U.csrf, U.ck) })
  cases.delWrongTypeDoc = await call(`/project/${ids.a}/doc/${ids.sub}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })
  cases.delWrongTypeFile = await call(`/project/${ids.a}/file/${ids.sub}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })

  // ---- subtree deletions (each 204, ordered deep-first) ----
  cases.delDeepDoc = await call(`/project/${ids.a}/doc/${ids.deep}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })
  cases.delSub2 = await call(`/project/${ids.a}/folder/${ids.sub2}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })
  cases.delSub = await call(`/project/${ids.a}/folder/${ids.sub}`, { method: 'DELETE', headers: h(U.csrf, U.ck) }) // subtree: notes.tex cleaned
  cases.delSubAgain = await call(`/project/${ids.a}/folder/${ids.sub}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })

  // ---- orphan doc (tree entry, NO docstore entry) → Node 404 AFTER write ----
  dexeQ(mongoC, `
    const p = db.projects.findOne({name:"pf11b-mine"});
    db.projects.updateOne({_id:p._id}, { \$push: { "rootFolder.0.docs": { _id: new ObjectId(), name: "orphan.tex" } } });
    print("orphan-inserted");
  `)
  const orphan = dexeQ(mongoC, `print(db.projects.findOne({name:"pf11b-mine"}).rootFolder[0].docs[1]._id)`).match(/[0-9a-f]{24}/)?.[0] || GHOST
  cases.delOrphan = await call(`/project/${ids.a}/doc/${orphan}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })
  cases.delOrphanAgain = await call(`/project/${ids.a}/doc/${orphan}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })

  // ---- root doc + remaining files ----
  cases.delRootDoc = await call(`/project/${ids.a}/doc/${ids.main}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })
  cases.delImg = await call(`/project/${ids.a}/file/${ids.img}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })
  cases.delMainAgain = await call(`/project/${ids.a}/doc/${ids.main}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })

  // ---- authz / auth ----
  // ---- authz (before x's doc disappears) then owner success ----
  cases.delNonmemberU = await call(`/project/${ids.x}/doc/${ids.xm}`, { method: 'DELETE', headers: h(U.csrf, U.ck) })
  cases.delXOwner = await call(`/project/${ids.x}/doc/${ids.xm}`, { method: 'DELETE', headers: h(O.csrf, O.ck) })
  cases.delAnon = await call(`/project/${ids.a}/doc/${GHOST}`, { method: 'DELETE', headers: { 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': 'faketoken' } })
  const an = await call('/login')
  const anCsrf = (an.body.match(/name=["']ol-csrfToken["']\s+content=["']([^"']+)"/) || [])[1] || ''
  const anSid = (an.setcookie || '').match(/overleaf\.sid=[^;\n]+/)?.[0] || ''
  cases.delAnonCsrf = await call(`/project/${ids.a}/doc/${GHOST}`, { method: 'DELETE', headers: { 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': anCsrf, cookie: anSid || undefined } })

  // ---- verb mismatch (Node-only routes handle it) ----
  cases.delPutMismatch = await call(`/project/${ids.a}/doc/${GHOST}`, { method: 'PUT', headers: { 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': U.csrf, cookie: U.ck } })

  const state = stateDump()
  return { cases, state }
}

function rootFolderId(): string {
  return dexeQ(mongoC, `print(db.projects.findOne({name:"pf11b-mine"}).rootFolder[0]._id)`).match(/[0-9a-f]{24}/)?.[0] || GHOST
}

type Leg = Awaited<ReturnType<typeof battery>>
let leg1: Leg

function diffLegs(lg: string, a: Leg, b: Leg): string[] {
  const ds: string[] = []
  const keys = Object.keys(a.cases)
  for (const k of keys) {
    const A = a.cases[k], B = b.cases[k]
    if (A.status !== B.status) { ds.push(`${lg}:${k} status N=${A.status} G=${B.status}`); continue }
    if (A.ct !== B.ct) { ds.push(`${lg}:${k} ct N=${A.ct} G=${B.ct}`); continue }
    const na = normBody(A.body), nb = normBody(B.body)
    if (na !== nb) {
      ds.push(`${lg}:${k} body` +
        (na.length !== nb.length ? ` len N=${na.length} G=${nb.length}` : '') + `
  N: ${na.slice(0, 400)}
  G: ${nb.slice(0, 400)}`)
    }
  }
  if (a.state !== b.state) ds.push(`${lg}:state
  N: ${a.state.slice(0, 700)}
  G: ${b.state.slice(0, 700)}`)
  return ds
}

test.describe('@local web-go P4.11b (entities delete) parity', () => {
  let U: { ck: string; csrf: string }, O: { ck: string; csrf: string }
  let ids: Ids

  test.beforeAll(async () => {
    test.setTimeout(240_000)
    dexe(overleafC, FLIP('web-p411b.conf', 'strip'), true); await nginxSettled()
    if (process.env.P411B_BUILD) {
      execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'bin/web'), `${overleafC}:/usr/local/bin/go-services/web`])
      dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
      dexe(overleafC, 'sv restart web-go-overleaf', true)
    }
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p411b.conf'), `${overleafC}:/tmp/web-p411b.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p411b.conf /usr/local/share/overleaf-flips/web-p411b.conf')
    for (;;) { const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim(); if (code === '200') break; await sleep(500) }
    U = await login(USER)
    O = await login(OTHER)
    ids = seed()
  }, 240_000)

  test.afterAll(async () => {
    try { dexe(overleafC, FLIP('web-p411b.conf', 'strip'), true); await nginxSettled() } catch {}
  })

  test('leg 1: Node baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('web-p411b.conf', 'strip'), true); await nginxSettled()
    clearRateLimits()
    ids = seed()
    leg1 = await battery(ids, U, O)
    const c = (k: string) => leg1.cases[k]
    if (process.env.P411B_DEBUG) {
      for (const k of Object.keys(leg1.cases)) console.log('P411B', k, '=>', leg1.cases[k].status, '|', leg1.cases[k].ct, '|', leg1.cases[k].body.slice(0, 160).replace(/\n/g, ' '))
      console.log('P411B state', leg1.state)
    }
    // pinned Node oracle (2026-09-15)
    expect(c('delRootFolder').status).toBe(422)
    expect(c('delRootFolder').ct).toBe('text/plain')
    expect(c('delRootFolder').body).toBe('cannot delete root folder')
    expect(c('delGhostDoc').status).toBe(404)
    expect(c('delGhostDoc').ct).toBe('text/html')
    expect(c('delBadProj').status).toBe(404)
    expect(c('delBadProj').ct).toBe('application/json')
    expect(c('delBadProj').body).toContain('params.Project_id')
    expect(c('delBadProj').body).toContain('statusCode":404')
    expect(c('delBadEntity').body).toContain('params.entity_id')
    expect(c('delWrongTypeDoc').status).toBe(404)
    expect(c('delWrongTypeFile').status).toBe(404)
    expect(c('delDeepDoc').status).toBe(204)
    expect(c('delDeepDoc').body).toBe('')
    expect(c('delSub2').status).toBe(204)
    expect(c('delSub').status).toBe(204)
    expect(c('delSubAgain').status).toBe(404)
    expect(c('delOrphan').status).toBe(404)
    expect(c('delRootDoc').status).toBe(204)
    expect(c('delImg').status).toBe(204)
    expect(c('delMainAgain').status).toBe(404)
    expect(c('delXOwner').status).toBe(204)
    expect(c('delNonmemberU').body).toBe('{"message":"restricted"}')
    expect(c('delAnon').status).toBe(403)
    expect(c('delAnonCsrf').status).toBe(401)
    expect(c('delAnonCsrf').body).toBe('Unauthorized')
  })

  test('leg 2: Go parity', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('web-p411b.conf', 'apply'), true); await nginxSettled()
    clearRateLimits()
    ids = seed()
    const leg2 = await battery(ids, U, O)
    dexe(overleafC, FLIP('web-p411b.conf', 'strip'), true); await nginxSettled()
    const ds = diffLegs('p411b', leg1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('leg 3: Node re-baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('web-p411b.conf', 'strip'), true); await nginxSettled()
    clearRateLimits()
    ids = seed()
    const leg3 = await battery(ids, U, O)
    const d1 = diffLegs('p411b-leg3', leg1, leg3)
    if (d1.length) console.log(d1.join('\n'))
    expect(d1, d1.join('\n')).toHaveLength(0)
  })
})
