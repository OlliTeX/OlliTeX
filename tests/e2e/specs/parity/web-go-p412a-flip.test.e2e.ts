// P4.12a: file proxy parity — GET|HEAD /Project/:Project_id/file/:File_id
// Three-leg: Node baseline -> Go parity -> Node re-baseline.
// Read-only unit: identical seed each leg (blobs are immutable local-FS
// files in the filestore data dir; fileRefs are re-seeded idempotently).
//
// Pinned Node oracle (2026-09-15):
//   200: no Content-Type (express omits it; Go net/http sniffs text/plain
//        — normalized), Content-Disposition attachment; filename="<name>",
//        Cache-Control private, max-age=3600, body = blob, chunked (no CL)
//   HEAD: 404 empty (history-v1 has no HEAD blob route in this fork)
//   ghost file: 404 empty; ghost project: 404 app page
//   bad oid: 404 JSON VA params.Project_id/params.File_id (statusCode 404)
//   non-member: 403 {"message":"restricted"} (accept json) / page (html)
//   anonymous GET: 302 /login; non-GET (any): 403 'Forbidden' (CSRF gate
//     fires even anonymously; POST+valid csrf → 404 app 404, 202 bytes)
//   state: read-only, project untouched
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
const HISTORY_ID = '6aa9055cabe0c87534b061aa'
const HISTORY_ID_X = '6aa9055cabe0c87534b061ab'
const HASH_A = '8d02edae8f11bfb21a0c6ba23344d285acfa6800'
const HASH_B = '0c7b604599d6e9d51b34e9e1d6e0b1c0aa112233'
const HASH_MISSING = 'd41d8cd98f00b204e9800998ecf8427e00000000'
const BODY_A = 'P412-BLOB-A-CONTENT'
const BODY_B = 'P412-BLOB-B-CONTENT'

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
interface Ids { a: string; x: string; fA: string; fB: string; fMiss: string; fX: string }
function seed(): Ids {
  // blobs in the filestore local-FS project-blob dir (immutable content,
  // idempotent write) — key layout = fseProjectKeyFormat(hid)/<h[:2]>/<h[2:]>
  dexe(overleafC, `
    for HID in '${HISTORY_ID}' '${HISTORY_ID_X}'; do
      KEYPATH=$(node -e "let id='$HID'; while(id.length<9)id='0'+id; let s=id.split('').reverse().join(''); console.log(s.slice(0,3)+'/'+s.slice(3,6)+'/'+s.slice(6))")
      B=/var/lib/overleaf/data/history/overleaf-project-blobs/\$KEYPATH
      mkdir -p \$B/${HASH_A.slice(0, 2)} \$B/${HASH_B.slice(0, 2)}
      printf '%s\\n' '${BODY_A}' > \$B/${HASH_A.slice(0, 2)}/${HASH_A.slice(2)}
      printf '%s\\n' '${BODY_B}' > \$B/${HASH_B.slice(0, 2)}/${HASH_B.slice(2)}
      chown -R www-data:www-data \$B
    done
  `, true)
  const out = dexeQ(mongoC, `
    db.projects.deleteMany({name:{$in:["pf12-mine","pf12-x"]}});
    db.docstore_docs.deleteMany({project_name:{$in:["pf12-mine","pf12-x"]}});
    const uid = ObjectId("6aa4b8b573ef0e5094f4cbc0");
    const oidd = ObjectId("6aa4b8c0ee67ff98732d4947");
    const fA = new ObjectId(), fB = new ObjectId(), fMiss = new ObjectId(), fX = new ObjectId();
    const sub = new ObjectId();
    const a = db.projects.insertOne({_id:new ObjectId(), name:"pf12-mine", owner_ref:uid, publicAccesLevel:"private",
      version:0,
      overleaf:{history:{id:ObjectId("${HISTORY_ID}"),rev:"r0",version:1}},
      rootFolder:[{name:"", _id:new ObjectId(), docs:[],
        fileRefs:[{_id:fA, name:"fake.png", hash:"${HASH_A}"}, {_id:fMiss, name:"missing.png", hash:"${HASH_MISSING}"}],
        folders:[{name:"sub", _id:sub, docs:[], fileRefs:[{_id:fB, name:"deep.png", hash:"${HASH_B}"}], folders:[]}]}]});
    const x = db.projects.insertOne({_id:new ObjectId(), name:"pf12-x", owner_ref:oidd, publicAccesLevel:"private",
      version:0,
      overleaf:{history:{id:ObjectId("${HISTORY_ID_X}"),rev:"r0",version:1}},
      rootFolder:[{name:"", _id:new ObjectId(), docs:[], fileRefs:[{_id:fX, name:"x.png", hash:"${HASH_A}"}], folders:[]}]});
    print(JSON.stringify({a:String(a.insertedId), x:String(x.insertedId), fA:String(fA), fB:String(fB), fMiss:String(fMiss), fX:String(fX)}));
  `)
  return JSON.parse(out.split('\n').pop()) as Ids
}

function stateDump(): string {
  return dexeQ(mongoC, `
    const out = {};
    for (const name of ["pf12-mine","pf12-x"]) {
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
  const H200 = ['content-disposition', 'cache-control', 'location']

  // ---- auth / guards ----
  cases.anonGet = await call(`/Project/${ids.a}/file/${ids.fA}`, {}, ['location', 'content-type'])
  cases.anonHtml = await call(`/Project/${ids.a}/file/${ids.fA}`, { headers: { accept: 'text/html' } }, ['location'])
  cases.anonHead = await call(`/Project/${ids.a}/file/${ids.fA}`, { method: 'HEAD' })
  cases.postAnon = await call(`/Project/${ids.a}/file/${ids.fA}`, { method: 'POST' })
  cases.postNoCsrf = await call(`/Project/${ids.a}/file/${ids.fA}`, { method: 'POST', headers: { cookie: U.ck, accept: 'application/json' } })

  // ---- validation / authz ----
  cases.badProj = await call(`/Project/notanoid/file/${ids.fA}`, ck(U.ck))
  cases.badFile = await call(`/Project/${ids.a}/file/notanoid`, ck(U.ck))
  cases.ghostProj = await call(`/Project/${GHOST}/file/${ids.fA}`, ck(U.ck))
  cases.nonmember = await call(`/Project/${ids.a}/file/${ids.fA}`, ck(O.ck))
  cases.nonmemberHtml = await call(`/Project/${ids.a}/file/${ids.fA}`, { cookie: O.ck, headers: { accept: 'text/html' } as Record<string, string> })

  // ---- data surface ----
  cases.get200 = await call(`/Project/${ids.a}/file/${ids.fA}`, ck(U.ck), H200)
  cases.u403 = await call(`/Project/${ids.x}/file/${ids.fX}`, { cookie: U.ck, headers: { accept: 'text/html' } as Record<string, string> })
  cases.head = await call(`/Project/${ids.a}/file/${ids.fA}`, { method: 'HEAD', cookie: U.ck, accept: 'application/json' } as Record<string, string>)
  cases.ghostFile = await call(`/Project/${ids.a}/file/${GHOST}`, ck(U.ck))
  cases.noBlob = await call(`/Project/${ids.a}/file/${ids.fMiss}`, ck(U.ck))
  cases.deepFile = await call(`/Project/${ids.a}/file/${ids.fB}`, ck(U.ck), H200)

  const state = stateDump()
  return { cases, state }
}

type Leg = Awaited<ReturnType<typeof battery>>
let leg1: Leg

const CT_NORMALIZED = new Set(['get200', 'deepFile']) // Node: none, Go: text/plain sniff

function diffLegs(lg: string, a: Leg, b: Leg): string[] {
  const ds: string[] = []
  const keys = Object.keys(a.cases)
  for (const k of keys) {
    if (!b.cases[k]) { ds.push(`${lg}:${k} missing in B`); continue }
    const A = a.cases[k], B = b.cases[k]
    if (A.status !== B.status) { ds.push(`${lg}:${k} status N=${A.status} G=${B.status}`); continue }
    const ctEq = A.ct === B.ct || (CT_NORMALIZED.has(k) && (A.ct === '' || A.ct === 'text/plain') && (B.ct === '' || B.ct === 'text/plain'))
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

test.describe('@local web-go P4.12a (file proxy) parity', () => {
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
      for (const k of Object.keys(leg1.cases)) console.log('P412', k, '=>', leg1.cases[k].status, '|', leg1.cases[k].ct, '|', JSON.stringify(leg1.cases[k].hdr || ''), '|', leg1.cases[k].body.slice(0, 120).replace(/\n/g, ' '))
      console.log('P412 state', leg1.state)
    }
    // pinned Node oracle (2026-09-15)
    expect(c('anonGet').status).toBe(401)
    expect(c('anonGet').body).toBe('Unauthorized')
    expect(c('anonHtml').status).toBe(302)
    expect(c('anonHtml').hdr?.['location']).toBeTruthy()
    expect(c('anonHead').status).toBe(401)
    expect(c('badProj').status).toBe(404)
    expect(c('badProj').ct).toBe('application/json')
    expect(c('badProj').body).toContain('params.Project_id')
    expect(c('badFile').body).toContain('params.File_id')
    expect(c('ghostProj').status).toBe(404)
    expect(c('nonmember').body).toBe('{"message":"restricted"}')
    expect(c('nonmemberHtml').status).toBe(403)
    expect(c('nonmemberHtml').ct).toBe('text/html')
    expect(c('get200').status).toBe(200)
    expect(c('get200').hdr?.['content-disposition']).toBe('attachment; filename="fake.png"')
    expect(c('get200').hdr?.['cache-control']).toBe('private, max-age=3600')
    expect(c('get200').body).toContain(BODY_A)
    expect(c('head').status).toBe(404)
    expect(c('ghostFile').status).toBe(404)
    expect(c('ghostFile').body).toBe('')
    expect(c('noBlob').status).toBe(404)
    expect(c('noBlob').body).toBe('')
    expect(c('deepFile').status).toBe(200)
    expect(c('deepFile').hdr?.['content-disposition']).toBe('attachment; filename="deep.png"')
    expect(c('postAnon').status).toBe(403)
    expect(c('postNoCsrf').status).toBe(403)
  })

  test('leg 2: Go parity', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('web-p412.conf', 'apply'), true); await nginxSettled()
    clearRateLimits()
    ids = seed()
    const leg2 = await battery(ids, U, O)
    if (process.env.P412_DEBUG) {
      console.log('DBG U.ck=', U.ck)
      console.log('DBG O.ck=', O.ck)
      const nb = leg2.cases['nonmemberHtml']
      const m = nb.body.match(/ol-usersEmail\" content=\"([^\"]+)/)
      console.log('DBG go nonmemberHtml email=', m && m[1])
      const mu = leg2.cases['u403'].body.match(/ol-usersEmail\" content=\"([^\"]+)/)
      console.log('DBG go u403 (U on x) email=', mu && mu[1], 'status', leg2.cases['u403'].status)
      const nb1 = leg1?.cases['nonmemberHtml']
      const m1 = nb1 && nb1.body.match(/ol-usersEmail\" content=\"([^\"]+)/)
      console.log('DBG node nonmemberHtml email=', m1 && m1[1])
    }
    dexe(overleafC, FLIP('web-p412.conf', 'strip'), true); await nginxSettled()
    const ds = diffLegs('p412', leg1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('leg 3: Node re-baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('web-p412.conf', 'strip'), true); await nginxSettled()
    clearRateLimits()
    ids = seed()
    const leg3 = await battery(ids, U, O)
    const d1 = diffLegs('p412-leg3', leg1, leg3)
    if (d1.length) console.log(d1.join('\n'))
    expect(d1, d1.join('\n')).toHaveLength(0)
  })
})
