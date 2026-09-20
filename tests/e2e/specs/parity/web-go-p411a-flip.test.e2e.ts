// P4.11a: editor entity creation parity — POST /project/:id/doc and /folder
// Three-leg: Node baseline -> Go parity -> Node re-baseline.
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
// fixture owner ids
const UID = '6aa4b8b573ef0e5094f4cbc0'
const OUID = '6aa4b8c0ee67ff98732d4947'
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
  const inc = `  include /etc/nginx/overleaf-flips/${conf};\n`
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
function seed(): { a: string; sub: string; x: string } {
  const out = dexeQ(mongoC, `
    db.projects.deleteMany({name:{$in:["pf11a-mine","pf11a-x"]}});
    db.projectInvites.deleteMany({});
    db.docs.deleteMany({name:{$in:["pf11a-mine","pf11a-x"]}});
    const uid = ObjectId("6aa4b8b573ef0e5094f4cbc0");
    const oidd = ObjectId("6aa4b8c0ee67ff98732d4947");
    const a = db.projects.insertOne({_id:new ObjectId(), name:"pf11a-mine", owner_ref:uid, publicAccesLevel:"private",
      track_changes:{enabled:false}, overleaf:{history:{id:new ObjectId(),rev:"r0",version:1}},
      rootFolder:[{name:"",docs:[{_id:new ObjectId(),name:"main.tex"}],
                   fileRefs:[{name:"pic.jpg",_id:new ObjectId(),hash:"deadbeefhash",linkedFileData:{},created:true,rev:0}],
                   folders:[{name:"sub",_id:new ObjectId(),docs:[],fileRefs:[],folders:[]}]}]});
    const x = db.projects.insertOne({_id:new ObjectId(), name:"pf11a-x", owner_ref:oidd, publicAccesLevel:"private",
      track_changes:{enabled:false}, overleaf:{history:{id:new ObjectId(),rev:"r0",version:1}},
      rootFolder:[{name:"",docs:[{_id:new ObjectId(),name:"main.tex"}],fileRefs:[],folders:[]}]});
    print(JSON.stringify({a:String(a.insertedId), x:String(x.insertedId)}));
  `)
  const ids = JSON.parse(out.split('\n').pop())
  const sub = dexeQ(mongoC, `print(db.projects.findOne({name:"pf11a-mine"}).rootFolder[0].folders[0]._id)`).match(/[0-9a-f]{24}/)?.[0] || ''
  return { a: ids.a, x: ids.x, sub }
}

function stateDump(projA: string, projX: string): Record<string, unknown> {
  const t = (name: string) => dexeQ(mongoC, `
    const p = db.projects.findOne({name:"${name}"});
    const walk = (f, d) => {
      const out = { docs: (f.docs||[]).map(x=>x.name).sort(), files: (f.fileRefs||[]).map(x=>x.name).sort(), folders: [] };
      for (const s of f.folders||[]) out.folders.push(walk(s, d+1));
      return out
    };
    const root = walk(p.rootFolder[0], 0).folders.length ? walk(p.rootFolder[0],0) : null;
    const flat = [];
    (function rec(f){ for (const d of f.docs||[]) flat.push("doc:"+d.name); for (const x of f.fileRefs||[]) flat.push("file:"+x.name); for (const s of f.folders||[]) { flat.push("folder:"+s.name); rec(s);} })(p.rootFolder[0]);
    print(JSON.stringify({flat: flat.sort(), ver: p.version, lub: String(p.lastUpdatedBy||"") }));
  `)
  return { [projA]: t(projA), [projX]: t(projX) }
}

// ---------- battery ----------
async function battery(ids: { a: string; sub: string; x: string }, U: { ck: string; csrf: string }, O: { ck: string; csrf: string }): Promise<{ cases: Record<string, R>; state: unknown }> {
  const cases: Record<string, R> = {}
  const h = (csrf: string, ck: string) => ({ 'content-type': 'application/json', 'x-csrf-token': csrf, cookie: ck, accept: 'application/json' }) as Record<string, string>
  const J = (o: unknown) => JSON.stringify(o)

  // ---- addDoc ----
  cases.addDocRoot = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'alpha.tex' }) })
  cases.addDocTrim = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: ' lead  ' }) })
  cases.addDocInSub = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'inner.tex', parent_folder_id: ids.sub }) })
  cases.addDocBlockedTop = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'toString' }) })
  cases.addDocBlockedSub = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'toString', parent_folder_id: ids.sub }) })
  cases.addDocDupDoc = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'main.tex' }) })
  cases.addDocDupFile = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'pic.jpg' }) })
  cases.addDocBadChar = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'a*b' }) })
  cases.addDocDot = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: '..' }) })
  cases.addDocSpaces = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: '   ' }) })
  cases.addDocLen = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'a'.repeat(150) + '.tex' }) })
  cases.addDocParentBad = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'x.tex', parent_folder_id: 'notanoid' }) })
  cases.addDocParentGhost = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'x.tex', parent_folder_id: '666666666666666666666666' }) })
  cases.addDocGhost = await call(`/project/${GHOST}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'g.tex' }) })
  cases.addDocNonmember = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(O.csrf, O.ck), body: J({ name: 'evil.tex' }) })
  cases.addDocAnon = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: { 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': 'faketoken' }, body: J({ name: 'anon.tex' }) })
  cases.addDocGetMismatch = await call(`/project/${ids.a}/doc`, { headers: { accept: 'application/json', cookie: U.ck } })
  cases.addDocX = await call(`/project/${ids.x}/doc`, { method: 'POST', headers: h(O.csrf, O.ck), body: J({ name: 'mine-over.txt' }) })
  // extra contract pins (probe 2026-09-15)
  cases.addDocNameNull = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: null }) })
  cases.addDocExtraKey = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: JSON.stringify({ name: 'ok9.tex', extra: 1 }) })
  cases.addDocNameNum = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: h(U.csrf, U.ck), body: JSON.stringify({ name: 42 }) })
  // anonymous with a VALID csrf (page token) -> 401 sendStatus
  {
    const an = await call('/login')
    const anCsrf = (an.body.match(/name=["']ol-csrfToken["']\s+content=["']([^"']+)"/) || [])[1] || ''
    const anSid = (an.setcookie || '').match(/overleaf\.sid=[^;\n]+/)?.[0] || ''
    const r = await call(`/project/${ids.a}/doc`, { method: 'POST', headers: { 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': anCsrf, cookie: anSid || undefined }, body: J({ name: 'anon.tex' }) })
    cases.addDocAnonCsrf = r
  }
  cases.addDocPutMismatch = await call(`/project/${ids.a}/doc`, { method: 'PUT', headers: { 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': U.csrf, cookie: U.ck }, body: J({ name: 'put.tex' }) })
  cases.addFoldPutMismatch = await call(`/project/${ids.a}/folder`, { method: 'PUT', headers: { 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': U.csrf, cookie: U.ck }, body: J({ name: 'putf' }) })

  // ---- addFolder ----
  cases.addFoldRoot = await call(`/project/${ids.a}/folder`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'myf' }) })
  cases.addFoldDup = await call(`/project/${ids.a}/folder`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'myf' }) })
  cases.addFoldDirty = await call(`/project/${ids.a}/folder`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: '../escape' }) })
  cases.addFoldTopWord = await call(`/project/${ids.a}/folder`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'toString' }) })
  cases.addFoldInSub = await call(`/project/${ids.a}/folder`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'innerf', parent_folder_id: ids.sub }) })
  cases.addFoldSpaces = await call(`/project/${ids.a}/folder`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: '   ' }) })
  cases.addFoldGhost = await call(`/project/${GHOST}/folder`, { method: 'POST', headers: h(U.csrf, U.ck), body: J({ name: 'gf' }) })
  cases.addFoldNonmember = await call(`/project/${ids.a}/folder`, { method: 'POST', headers: h(O.csrf, O.ck), body: J({ name: 'evilf' }) })

  if (process.env.P411A_DEBUG) {
    console.log('P411A IDS', JSON.stringify(ids))
    for (const k of ['addDocInSub','addDocBlockedTop','addDocBlockedSub']) {
      console.log('P411A', k, '=>', cases[k].status, cases[k].ct, cases[k].body.slice(0,150))
    }
  }
  const state = stateDump('pf11a-mine', 'pf11a-x')
  return { cases, state }
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
  const jsonA = JSON.stringify(a.state), jsonB = JSON.stringify(b.state)
  if (jsonA !== jsonB) ds.push(`${lg}:state
  N: ${jsonA.slice(0, 600)}
  G: ${jsonB.slice(0, 600)}`)
  return ds
}

test.describe('@local web-go P4.11 (entities add) parity', () => {
  let U: { ck: string; csrf: string }, O: { ck: string; csrf: string }
  let ids: { a: string; sub: string; x: string }

  test.beforeAll(async () => {
    test.setTimeout(240_000)
    dexe(overleafC, FLIP('web-p411a.conf', 'strip'), true); await nginxSettled()
    if (process.env.P4INV_BUILD) {
      execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'bin/web'), `${overleafC}:/usr/local/bin/go-services/web`])
      dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
      dexe(overleafC, 'sv restart web-go-overleaf', true)
    }
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p411a.conf'), `${overleafC}:/tmp/web-p411a.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p411a.conf /usr/local/share/overleaf-flips/web-p411a.conf')
    for (;;) { const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim(); if (code === '200') break; await sleep(500) }
    U = await login(USER)
    O = await login(OTHER)
    ids = seed()
  }, 240_000)

  test.afterAll(async () => {
    try { dexe(overleafC, FLIP('web-p411a.conf', 'strip'), true); await nginxSettled() } catch {}
  })

  test('leg 1: Node baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('web-p411a.conf', 'strip'), true); await nginxSettled()
    clearRateLimits()
    leg1 = await battery(ids, U, O)
    const c = (k: string) => leg1.cases[k]
    // pin the Node contract (oracle, 2026-09-15 probes)
    expect(c('addDocRoot').status).toBe(200)
    expect(c('addDocRoot').ct).toBe('application/json')
    expect(c('addDocRoot').body).toMatch(/"name":"alpha\.tex","_id":"[0-9a-f]{24}"/)
    expect(c('addDocTrim').body).toContain('"name":"lead"')
    expect(c('addDocBlockedTop').status).toBe(400)
    expect(c('addDocBlockedTop').ct).toBe('text/plain')
    expect(c('addDocBlockedTop').body).toBe('blocked element name')
    expect(c('addDocBlockedSub').status).toBe(200)
    expect(c('addDocDupDoc').body).toBe('file already exists')
    expect(c('addDocDupFile').body).toBe('file already exists')
    expect(c('addDocBadChar').body).toBe('invalid element name')
    expect(c('addDocDot').body).toBe('invalid element name')
    expect(c('addDocSpaces').body).toBe('invalid element name')
    expect(c('addDocLen').status).toBe(400)
    expect(c('addDocLen').ct).toBe('text/plain')
    expect(c('addDocParentBad').body).toContain('Invalid Mongo ObjectId')
    expect(c('addDocParentGhost').status).toBe(404)
    expect(c('addDocGhost').status).toBe(404)
    expect(c('addDocNonmember').body).toBe('{"message":"restricted"}')
    expect(c('addDocAnon').status).toBe(403)
    expect(c('addDocAnon').ct).toBe('text/plain')
    expect(c('addDocNameNull').status).toBe(400)
    expect(c('addDocNameNull').body).toContain('expected string, received null')
    expect(c('addDocNameNull').body).toContain('body.name')
    expect(c('addDocExtraKey').status).toBe(400)
    expect(c('addDocExtraKey').body).toContain(`Unrecognized key: \\\"extra\\\"`)
    expect(c('addDocNameNum').status).toBe(400)
    expect(c('addDocNameNum').body).toContain('expected string, received number')
    expect(c('addDocAnonCsrf').status).toBe(401)
    expect(c('addDocAnonCsrf').ct).toBe('text/plain')
    expect(c('addDocAnonCsrf').body).toBe('Unauthorized')
    expect(c('addFoldRoot').status).toBe(200)
    expect(c('addFoldRoot').body).toMatch(/"name":"myf","_id":"[0-9a-f]{24}","docs":\[\],"fileRefs":\[\],"folders":\[\]/)
    expect(c('addFoldDup').body).toBe('file already exists')
    expect(c('addFoldDirty').status).toBe(400)
    expect(c('addFoldDirty').ct).toBe('application/json')
    expect(c('addFoldDirty').body).toBe('"Invalid File Name"')
    expect(c('addFoldTopWord').status).toBe(200)
    expect(c('addFoldSpaces').body).toBe('"Invalid File Name"')
    expect(c('addFoldGhost').status).toBe(404)
    expect(c('addFoldNonmember').body).toBe('{"message":"restricted"}')
  })

  test('leg 2: Go parity', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('web-p411a.conf', 'apply'), true); await nginxSettled()
    clearRateLimits()
    ids = seed()
    const leg2 = await battery(ids, U, O)
    dexe(overleafC, FLIP('web-p411a.conf', 'strip'), true); await nginxSettled()
    const ds = diffLegs('p411a', leg1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('leg 3: Node re-baseline', async () => {
    test.setTimeout(180_000)
    dexe(overleafC, FLIP('web-p411a.conf', 'strip'), true); await nginxSettled()
    clearRateLimits()
    ids = seed()
    const leg3 = await battery(ids, U, O)
    const d1 = diffLegs('p411a-leg3', leg1, leg3)
    if (d1.length) console.log(d1.join('\n'))
    expect(d1, d1.join('\n')).toHaveLength(0)
  })
})
