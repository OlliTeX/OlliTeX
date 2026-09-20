// P4.12b: document download parity — GET|HEAD /Project/:Project_id/doc/:Doc_id/download
// ("download" suffix distinguishes it from the private API doc route).
// Three-leg: Node baseline -> Go parity -> Node re-baseline.
// Read-only unit: identical seed each leg (docs + docstore entries are
// idempotently re-created; DU serves fromVersion=-1 = docstore lines).
//
// Pinned Node oracle (2026-09-15):
//   GET member:  200 text/plain; charset=utf-8 +
//     Content-Disposition: attachment; filename="<doc.name>" +
//     body = lines.join('\n') (weak ETag on both)
//   HEAD member: 200, same headers, NO body (express auto HEAD→GET)
//   ghost doc (valid project): 404 text/plain 'Not Found' (sendStatus)
//   ghost project: 404 app page
//   bad oid: 404 JSON VA params.Project_id / params.Doc_id (statusCode 404;
//     enforce-log mode enforces logOnly schemas too)
//   non-member: 403 {"message":"restricted"} (accept json) / restricted
//     page (accept html — renders the REQUESTER's email; requires the
//     P4.12a baked-in-capture-user fix in restrictedHTML)
//   anonymous: 401 'Unauthorized' (accept json) / 302 /login (else)
//   non-GET: flip guard keeps them on Node → CSRF 403 'Forbidden'
//   DU-missing edge (doc in tree, no docstore entry): unpinned; Go = 500
//     (Node's non-NotFoundError path) — deliberately not in the battery.
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
const L0 = '%\\documentclass{article}'
const LINES = [L0, 'begin/end', 'P412B DOC LINE1', 'P412B DOC LINE2']
const LINES_DEEP = ['deep doc one', 'deep doc two']

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

interface R { status: number; ct: string; body: string; setcookie?: string; hdr?: Record<string, string> }
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

async function call(p: string, init: RequestInit & { cookie?: string } = {}, wantHdrs?: string[]): Promise<R> {
  const h = { ...(init.headers || {}) as Record<string, string>, accept: ((init.headers as any)?.accept as string) || 'application/json' }
  if (init.cookie) h['cookie'] = init.cookie as string
  const r = await fetch(BASE + p, { ...init, headers: h, redirect: 'manual' })
  const setcookie = (r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : ((r.headers.get('set-cookie') as string) || '')) as string
  const body = Buffer.from(await r.arrayBuffer()).toString('utf8')
  const out: R = { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], body, setcookie }
  if (wantHdrs) {
    out.hdr = {}
    for (const k of wantHdrs) out.hdr[k.toLowerCase()] = (r.headers.get(k) || '')
  }
  return out
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
interface Ids { a: string; x: string; dA: string; dDeep: string; dX: string }
function seed(): Ids {
  const idsOut = dexeQ(mongoC, `
    db.projects.deleteMany({name:{$in:["p412-mine","p412-x"]}});
    const uid = ObjectId("6aa4b8b573ef0e5094f4cbc0");
    const oidd = ObjectId("6aa4b8c0ee67ff98732d4947");
    const dA = new ObjectId(), dDeep = new ObjectId(), dX = new ObjectId();
    const sub = new ObjectId();
    const a = db.projects.insertOne({_id:new ObjectId(), name:"p412-mine", owner_ref:uid, publicAccesLevel:"private",
      version:0,
      overleaf:{rootDoc_id:dA},
      rootFolder:[{name:"", _id:new ObjectId(),
        docs:[{_id:dA, name:"doc.tex"}],
        fileRefs:[],
        folders:[{name:"notes", _id:sub, docs:[{_id:dDeep, name:"deep.md"}], fileRefs:[], folders:[]}]}]});
    const x = db.projects.insertOne({_id:new ObjectId(), name:"p412-x", owner_ref:oidd, publicAccesLevel:"private",
      version:0,
      rootFolder:[{name:"", _id:new ObjectId(), docs:[{_id:dX, name:"x.tex"}], fileRefs:[], folders:[]}]});
    print(JSON.stringify({a:String(a.insertedId), x:String(x.insertedId), dA:String(dA), dDeep:String(dDeep), dX:String(dX)}));
  `)
  const ids = JSON.parse(idsOut.split('\n').pop()) as Ids
  // docstore entries (Node's addDoc equivalent — DU fromVersion=-1 reads
  // the docstore lines). Three explicit calls: the JSON bodies contain ':'.
  const j1 = JSON.stringify(LINES)
  const j2 = JSON.stringify(LINES_DEEP)
  const j3 = JSON.stringify(['x doc line'])
  // docstore entries (Node's addDoc equivalent — DU fromVersion=-1 reads
  // the docstore lines). Retried once: right after a project re-seed the
  // stack is still settling and POSTs can transiently 5xx.
  const mk = (pid: string, did: string, lines: string) =>
    `for ATT in 1 2 3 4; do
  CODE=$(curl -s -o /tmp/dsbody -w "%{http_code}" -X POST http://127.0.0.1:3016/project/${pid}/doc/${did} -H 'content-type: application/json' -d '{"lines":${lines},"version":0,"ranges":{}}' 2>&1)
  [ "$CODE" = "200" ] && break
  echo "seed attempt $ATT for $did -> $CODE $(head -c 200 /tmp/dsbody)"
  sleep 1
  done
  [ "$CODE" = "200" ] || exit 1\n`
  dexe(overleafC, mk(ids.a, ids.dA, j1) + mk(ids.a, ids.dDeep, j2) + mk(ids.x, ids.dX, j3), true)
  return ids
}

function stateDump(): string {
  return dexeQ(mongoC, `
    const out = {};
    for (const name of ["p412-mine","p412-x"]) {
      const p = db.projects.findOne({name});
      const flat = [];
      (function rec(f){ for (const d of f.docs||[]) flat.push("doc:"+d.name); for (const x of f.fileRefs||[]) flat.push("file:"+x.name); for (const s of f.folders||[]) { flat.push("folder:"+s.name); rec(s);} })(p.rootFolder[0]);
      out[name] = { flat: flat.sort(), ver: p.version };
    }
    print(JSON.stringify(out));
  `)
}

// ---------- battery (read-only — identical both legs) ----------
async function battery(ids: Ids, U: { ck: string; csrf: string }, O: { ck: string; csrf: string }): Promise<{ cases: Record<string, R>; state: string }> {
  const cases: Record<string, R> = {}
  const ck = (c: string) => ({ cookie: c, accept: 'application/json' }) as Record<string, string>
  const H200 = ['content-disposition', 'content-type']
  const dl = (did: string, proj = ids.a) => `/Project/${proj}/doc/${did}/download`

  // ---- auth / guards ----
  cases.anonJson = call(dl(ids.dA), { headers: { accept: 'application/json' } } as Record<string, string>)
  cases.anonHtml = call(dl(ids.dA), { headers: { accept: 'text/html' } } as Record<string, string>)
  cases.postAnon = call(dl(ids.dA), { method: 'POST' } as Record<string, string>)
  cases.postNoCsrf = call(dl(ids.dA), { method: 'POST', headers: ck(U.ck) } as Record<string, string>)

  // ---- validation / authz ----
  cases.badProj = call(`/Project/notanoid/doc/${ids.dA}/download`, ck(U.ck))
  cases.badDoc = call(dl('notanoid'), ck(U.ck))
  cases.ghostProj = call(`/Project/${GHOST}/doc/${ids.dA}/download`, ck(U.ck))
  cases.nonmember = call(dl(ids.dA), ck(O.ck))
  cases.nonmemberHtml = call(dl(ids.dA), { cookie: O.ck, headers: { accept: 'text/html' } } as Record<string, string>)
  cases.u403 = call(dl(ids.dX, ids.x), { cookie: U.ck, headers: { accept: 'text/html' } } as Record<string, string>)

  // ---- data surface ----
  cases.get200 = call(dl(ids.dA), ck(U.ck), H200)
  cases.head200 = call(dl(ids.dA), { method: 'HEAD', cookie: U.ck } as Record<string, string>, H200)
  cases.deep200 = call(dl(ids.dDeep), ck(U.ck), H200)
  cases.ghostDoc = call(dl(GHOST), ck(U.ck))

  const state = stateDump()
  return Promise.all(Object.values(cases)).then((rs) => {
    const rec: Record<string, R> = {}
    for (const [k, v] of Object.entries(cases)) rec[k] = rs[Object.keys(cases).indexOf(k)]
    return { cases: rec, state }
  })
}

type Leg = Awaited<ReturnType<typeof battery>>
let leg1: Leg

function diffLegs(lg: string, a: Leg, b: Leg): string[] {
  const ds: string[] = []
  const keys = Object.keys(a.cases)
  for (const k of keys) {
    if (!b.cases[k]) { ds.push(`${lg}:${k} missing in B`); continue }
    const A = a.cases[k], B = b.cases[k]
    if (A.status !== B.status) { ds.push(`${lg}:${k} status N=${A.status} G=${B.status}`); continue }
    const ctEq = A.ct === B.ct
    if (!ctEq) { ds.push(`${lg}:${k} ct N=${A.ct} G=${B.ct}`); continue }
    const na = normBody(A.body), nb = normBody(B.body)
    if (na !== nb) {
      let i = 0
      while (i < Math.min(na.length, nb.length) && na[i] === nb[i]) i++
      ds.push(`${lg}:${k} body` +
        (na.length !== nb.length ? ` len N=${na.length} G=${nb.length}` : '') + ` firstdiff=${i}
  N: ${JSON.stringify(na.slice(Math.max(0, i - 120), i + 200))}
  G: ${JSON.stringify(nb.slice(Math.max(0, i - 120), i + 200))}`)
    }
    if (A.hdr || B.hdr) {
      const ah = JSON.stringify(A.hdr || {}), bh = JSON.stringify(B.hdr || {})
      if (ah !== bh) ds.push(`${lg}:${k} hdr
  N: ${ah}
  G: ${bh}`)
    }
  }
  if (a.state !== b.state) ds.push(`${lg}:state
  N: ${a.state.slice(0, 500)}
  G: ${b.state.slice(0, 500)}`)
  return ds
}

test.describe('@local web-go P4.12b (doc download) parity', () => {
  let U: { ck: string; csrf: string }, O: { ck: string; csrf: string }
  let ids: Ids

  test.beforeAll(async () => {
    test.setTimeout(240_000)
    dexe(overleafC, FLIP('web-p412.conf', 'strip'), true); await nginxSettled()
    if (process.env.P412_BUILD) {
      execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'bin/web'), `${overleafC}:/usr/local/bin/go-services/web`])
      dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
      dexe(overleafC, 'sv restart web-go-overleaf', true)
    }
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p412.conf'), `${overleafC}:/tmp/web-p412.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p412.conf /usr/local/share/overleaf-flips/web-p412.conf')
    for (;;) { const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim(); if (code === '200') break; await sleep(500) }
    U = await login(USER)
    O = await login(OTHER)
    ids = seed()
  }, 240_000)

  test.afterAll(async () => {
    try { dexe(overleafC, FLIP('web-p412.conf', 'strip'), true); await nginxSettled() } catch {}
  })

  test('leg 1: Node baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('web-p412.conf', 'strip'), true); await nginxSettled()
    clearRateLimits()
    ids = seed()
    leg1 = await battery(ids, U, O)
    const c = (k: string) => leg1.cases[k]
    if (process.env.P412_DEBUG) {
      for (const k of Object.keys(leg1.cases)) console.log('P412B', k, '=>', leg1.cases[k].status, '|', leg1.cases[k].ct, '|', JSON.stringify(leg1.cases[k].hdr || ''), '|', leg1.cases[k].body.slice(0, 120).replace(/\n/g, '\\n'))
      console.log('P412B state', leg1.state)
    }
    // pinned Node oracle (2026-09-15)
    expect(c('anonJson').status).toBe(401)
    expect(c('anonJson').body).toBe('Unauthorized')
    expect(c('anonHtml').status).toBe(302)
    expect(c('postAnon').status).toBe(403)
    expect(c('postNoCsrf').status).toBe(403)
    expect(c('badProj').status).toBe(404)
    expect(c('badProj').body).toContain('params.Project_id')
    expect(c('badDoc').body).toContain('params.Doc_id')
    expect(c('ghostProj').status).toBe(404)
    expect(c('ghostProj').ct).toBe('text/html')
    expect(c('nonmember').body).toBe('{"message":"restricted"}')
    expect(c('nonmemberHtml').status).toBe(403)
    expect(c('nonmemberHtml').ct).toBe('text/html')
    expect(c('u403').status).toBe(403)
    expect(c('u403').body).toContain('e2e-user@e2e.test')
    expect(c('get200').status).toBe(200)
    expect(c('get200').ct).toBe('text/plain')
    expect(c('get200').hdr?.['content-disposition']).toBe('attachment; filename="doc.tex"')
    expect(c('get200').body).toBe(LINES.join('\n'))
    expect(c('head200').status).toBe(200)
    expect(c('head200').body).toBe('')
    expect(c('head200').hdr?.['content-disposition']).toBe('attachment; filename="doc.tex"')
    expect(c('deep200').status).toBe(200)
    expect(c('deep200').hdr?.['content-disposition']).toBe('attachment; filename="deep.md"')
    expect(c('deep200').body).toBe(LINES_DEEP.join('\n'))
    expect(c('ghostDoc').status).toBe(404)
    expect(c('ghostDoc').ct).toBe('text/plain')
    expect(c('ghostDoc').body).toBe('Not Found')
  })

  test('leg 2: Go parity', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('web-p412.conf', 'apply'), true); await nginxSettled()
    clearRateLimits()
    ids = seed()
    const leg2 = await battery(ids, U, O)
    if (process.env.P412_DEBUG) {
      const mu = leg2.cases['u403'].body.match(/ol-usersEmail" content="([^"]+)/)
      console.log('DBG go u403 (U on x) email=', mu && mu[1], 'status', leg2.cases['u403'].status)
      const m1 = leg1.cases['u403'].body.match(/ol-usersEmail" content="([^"]+)/)
      console.log('DBG node u403 (U on x) email=', m1 && m1[1])
    }
    dexe(overleafC, FLIP('web-p412.conf', 'strip'), true); await nginxSettled()
    const ds = diffLegs('p412b', leg1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('leg 3: Node re-baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('web-p412.conf', 'strip'), true); await nginxSettled()
    clearRateLimits()
    ids = seed()
    const leg3 = await battery(ids, U, O)
    const d1 = diffLegs('p412b-leg3', leg1, leg3)
    if (d1.length) console.log(d1.join('\n'))
    expect(d1, d1.join('\n')).toHaveLength(0)
  })
})
