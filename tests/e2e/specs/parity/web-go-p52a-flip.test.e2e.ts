/**
 * WEB-GO P5.2a FLIP GATE (WEB_GO_PLAN.md P5.2a — COMPILE CONTROL PLANE):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p52a.conf):
 *     POST /project/:Project_id/compile        (CompileController.compile)
 *     POST /project/:Project_id/compile/stop   (CompileController.stopCompile)
 *
 *   The compile ENGINE stays the Node `clsi` on 127.0.0.1:3013 — Node and Go
 *   both POST /project/<pid>/user/<uid>/compile to it. So this gate compares
 *   the WEB RESPONSE CONTRACT, not raw bytes: build/buildId, createdAt,
 *   stats, timings and pdf size are normalized (they are legitimately
 *   per-compile values even between two Node runs on the same clsi).
 *
 *   leg 1  Node baseline       (flip OFF)
 *   leg 2  FLIP ON — Go        (flip ON , battery, flip OFF)
 *   leg 3  Node re-baseline    (flip OFF)
 *
 *   Battery (per leg; the recent-compile pair fires CONCURRENTLY so the
 *   1-second recently-compiled guard is deterministically hit — clsi takes
 *   several seconds, so the second in-flight request always sees the key):
 *     pair            compile × 2 concurrently
 *                      ⇒ exactly one "success" + one
 *                        "too-recently-compiled" (multiset compared)
 *     stop            POST …/compile/stop      ⇒  200 text/plain "OK"
 *     anon_compile    POST …/compile (no cookie) ⇒ 403 "Forbidden"
 *     anon_stop       POST …/compile/stop (no cookie) ⇒ 403 "Forbidden"
 *     bad_id          POST /project/not-an-oid/compile
 *                      ⇒ 404 JSON malformed (NOT accept-dep)
 *     absent          POST /project/deadbeefdeadbeefdeadbeef/compile
 *                      ⇒ 404 HTML general/404 (NOT accept-dep)
 *
 *   Node oracle pins (live 2026-09-15, P5.2 kickoff commit 8ad7943419):
 *     success 200 application/json, key ORDER:
 *       status, outputFiles, outputFilesArchive, compileGroup, compiler,
 *       (clsiServerId, clsiCacheShard), (validationProblems), stats,
 *       timings, (outputUrlPrefix), (pdfDownloadDomain),
 *       (pdfCachingMinChunkSize)   — undefined OMITTED
 *     outputFiles: {path, url(pathname-only), type, build}; output.pdf ALSO
 *       {contentId?, ranges: file.ranges||[], size, startXRefTable?,
 *       createdAt}; clsi's url host STRIPPED (new URL(url).pathname)
 *     outputFilesArchive = buildId ? {path:"output.zip",
 *       url:"/project/<pid>/user/<uid>/build/<buildId>/output/output.zip",
 *       type:"zip"} : null (EXPLICIT)
 *     recently: {status:"too-recently-compiled", outputFiles:[],
 *       outputFilesArchive:null}  — no limits keys (SET-NX happens before
 *       owner fetch)
 *     limits: compileGroup "standard" (CE), compiler "pdflatex" (project),
 *       clsi backend class "free"
 *
 * Run: npx playwright test -g "web-go P5.2a flip gate"
 */
import { execFileSync } from 'node:child_process'
import { test, expect } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const redisC = 'ol-e2e-redis-1'
const FLIPCONF = 'web-p52a.conf'
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const FIX_NAME = 'P52a Compile Fixture'
const ABSENT_PID = 'deadbeefdeadbeefdeadbeef'

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function dexe(c: string, cmd: string, capture = false): string {
  try {
    const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore', maxBuffer: 64 * 1024 * 1024 })
    return out || ''
  } catch (e) {
    if (capture) throw e
    return ''
  }
}
function dexeQ(c: string, cmd: string): string {
  return (execFileSync('docker', ['exec', c, 'mongosh', '--quiet', 'sharelatex', '--eval', cmd], { encoding: 'utf8' }) || '').trim()
}

/**
 * Node's compile route carries `rate-limit:compile-project-http:<pid>`
 * (200 pts/10 min). The gate runs a handful of compiles per leg, far below
 * the ceiling — but flush the counter anyway so a dirty pre-state (manual
 * probes) cannot 429 only the Node leg. Go registers NO limiter here (P4
 * parity decision): flushing equalises the state so the comparison is the
 * compile response, not rate-limit residue.
 */
function flushCompileLimiter(): void {
  for (let db = 0; db <= 7; db++) {
    dexe(redisC, `redis-cli -n ${db} --scan --pattern 'rate-limit:compile-project-http*' 2>/dev/null | while read -r k; do [ -n "$k" ] && redis-cli -n ${db} del "$k" >/dev/null 2>&1; done; true`)
  }
}

async function waitGo(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim()
    if (code === '200') return
    await sleep(500)
  }
  throw new Error('Go shadow /status not 200')
}

function flip(mode: 'apply' | 'strip'): void {
  const vhost = '/etc/nginx/sites-enabled/overleaf.conf'
  if (mode === 'apply') {
    dexe(overleafC, `
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/${FLIPCONF} /etc/nginx/overleaf-flips/${FLIPCONF}
node -e "const fs=require('fs');const p='${vhost}';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"
if ! grep -q "overleaf-flips/${FLIPCONF}" ${vhost}; then
  python3 - ${vhost} <<'PYEOF'
import sys
p = sys.argv[1]
s = open(p).read()
inc = "  include /etc/nginx/overleaf-flips/${FLIPCONF};\\n"
if "location / {" in s:
    s = s.replace("location / {", inc + "location / {", 1)
else:
    raise SystemExit("anchor 'location / {' not found")
open(p, 'w').write(s)
PYEOF
fi
nginx -t 2>&1 | tail -1 && nginx -s reload
`)
  } else {
    dexe(overleafC, `sed -i "/overleaf-flips\\/${FLIPCONF}/d" ${vhost} && nginx -t 2>&1 | tail -1 && nginx -s reload`)
  }
  dexe(overleafC, 'sleep 1.5')
}

// ---------- http ---------------------------------------------------------------
type R = { status: number; ct: string; body: string; setcookie: string }
async function call(p: string, init: { method?: string; headers?: Record<string, string>; cookie?: string; body?: any } = {}): Promise<R> {
  const h: Record<string, string> = { ...(init.headers || {}) }
  if (init.cookie) h['cookie'] = init.cookie
  let lastErr: unknown = null
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      const r = await fetch(BASE + p, { method: init.method || 'GET', headers: h, body: init.body as any, redirect: 'manual' })
      const body = Buffer.from(await r.arrayBuffer()).toString('utf8')
      const setcookie = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : (r.headers.get('set-cookie') || '').toString()
      return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], body, setcookie }
    } catch (e) {
      lastErr = e
      await new Promise((res) => setTimeout(res, 400 * (attempt + 1)))
    }
  }
  throw lastErr
}

async function login(user: { email: string; password: string }): Promise<{ ck: string; csrf: string }> {
  const page = await call('/login')
  const csrf0 = (page.body.match(/name=["']ol-csrfToken["']\s+content=["']([^"']+)"/) || [])[1] || ''
  const ck0 = ((page.setcookie || '').match(/overleaf\.sid=[^;\n]+/) || [])[0] || ''
  const logged = await call('/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': csrf0, accept: 'application/json' },
    cookie: ck0 || undefined,
    body: JSON.stringify(user),
  })
  if (logged.status !== 200) throw new Error('login failed ' + logged.status)
  const sid = ((logged.setcookie || '').split('\n').find((l) => l.includes('overleaf.sid')) || '').replace(/^overleaf\.sid=/, '').split(';')[0]
  const ck = sid ? 'overleaf.sid=' + sid : ck0
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body.trim() || csrf0 }
}

// HTML norm (nonce/csrf/hex24/ts) — same as the P5.1 gates.
const normHTML = (s: string): string =>
  s
    .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
    .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
    .replace(/name="_csrf"[^>]*value="[^"]*"/g, 'name="_csrf" value="CSRF"')
    .replace(/\b[0-9a-f]{24}\b/g, '<H24>')
    .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z/g, '<TS>')

// JSON compile-response norm: buildId (ts-hex + '-' + 8hex) + build field,
// createdAt, pdf size, stats/timings objects. Kept EXACT otherwise.
const normJSON = (s: string): string =>
  s
    .replace(/(\\?\/build\/)[0-9a-f]+-[0-9a-f]+/g, '$1BID')
    .replace(/"build":"[0-9a-f]+-[0-9a-f]+"/g, '"build":"BID"')
    .replace(/"createdAt":"[^"]*"/g, '"createdAt":"TS"')
    .replace(/"size":\d+/g, '"size":0')
    .replace(/"stats":\{[^{}]*\}/g, '"stats":{}')
    .replace(/"timings":\{[^{}]*\}/g, '"timings":{}')
    .replace(/\b[0-9a-f]{24}\b/g, '<H24>')

function parseJSON(t: string): any {
  try {
    return JSON.parse(t)
  } catch {
    return null
  }
}

async function ensureFixtureProject(U: { ck: string; csrf: string }): Promise<string> {
  dexeQ(mongoC, `db.projects.deleteMany({ name: /^P52a Compile Fixture/ })`)
  const r = await call('/project/new', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': U.csrf, accept: 'application/json' },
    cookie: U.ck,
    body: JSON.stringify({ projectName: FIX_NAME }),
  })
  if (r.status !== 200) throw new Error('fixture project create failed ' + r.status + ' ' + r.body.slice(0, 200))
  const j = parseJSON(r.body)
  const pid = j && (j.project_id || j.projectId)
  if (!pid) throw new Error('fixture project pid not in response: ' + r.body.slice(0, 200))
  return pid
}

// ---------- battery -------------------------------------------------------------
type Leg = {
  pair: string[] // sorted [status|normBody] strings of the 2 concurrent compiles
  compileCTs: string[]
  stop: R
  anon_compile: R
  anon_stop: R
  bad_id: R
  absent: R
}

async function compilePost(pid: string, U: { ck: string; csrf: string }): Promise<R> {
  // fresh csrf per mutation (token rotates per render/use — P4/P5.1 pattern)
  const cs = await call('/dev/csrf', { cookie: U.ck, headers: { accept: 'text/plain' } })
  return call(`/project/${pid}/compile`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': cs.body.trim(), accept: 'application/json' },
    cookie: U.ck,
    body: JSON.stringify({ compiler: 'pdflatex' }),
  })
}

async function battery(pid: string, U: { ck: string; csrf: string }): Promise<Leg> {
  flushCompileLimiter()
  await sleep(1300) // let the 1s recently-compiled key from pre-work expire

  // CONCURRENT pair: one wins the SET-NX guard and compiles (clsi takes
  // several seconds), the other deterministically gets the recent reply.
  const [a, b] = await Promise.all([compilePost(pid, U), compilePost(pid, U)])
  const mk = (r: R): string => {
    const j = parseJSON(r.body)
    return r.status + '|' + (r.ct || '') + '|' + normJSON(r.body) + '|' + (j && j.status)
  }
  const pair = [mk(a), mk(b)].sort()

  await sleep(1300) // recent key expiry before the next leg's compile
  const csStop = await call('/dev/csrf', { cookie: U.ck, headers: { accept: 'text/plain' } })
  const stop = await call(`/project/${pid}/compile/stop`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': csStop.body.trim(), accept: 'application/json' },
    cookie: U.ck,
  })

  const anon_compile = await call(`/project/${pid}/compile`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', accept: 'application/json' },
    body: JSON.stringify({ compiler: 'pdflatex' }),
  })
  const anon_stop = await call(`/project/${pid}/compile/stop`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', accept: 'application/json' },
  })
  const bad_id = await call('/project/not-an-oid/compile', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': (await call('/dev/csrf', { cookie: U.ck, headers: { accept: 'text/plain' } })).body.trim(), accept: 'application/json' },
    cookie: U.ck,
    body: JSON.stringify({ compiler: 'pdflatex' }),
  })
  const absent = await call(`/project/${ABSENT_PID}/compile`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': (await call('/dev/csrf', { cookie: U.ck, headers: { accept: 'text/plain' } })).body.trim(), accept: 'application/json' },
    cookie: U.ck,
    body: JSON.stringify({ compiler: 'pdflatex' }),
  })

  return { pair, compileCTs: [a.ct, b.ct].sort(), stop, anon_compile, anon_stop, bad_id, absent }
}

function diffLegs(label: string, A: Leg, B: Leg): string[] {
  const ds: string[] = []
  if (A.pair.join('\n') !== B.pair.join('\n')) {
    ds.push(`${label}:PAIR\nA: ${A.pair.join('\n')}\nB: ${B.pair.join('\n')}`)
  }
  if (A.compileCTs.join() !== B.compileCTs.join()) ds.push(`${label}:compile CTs A=${A.compileCTs} B=${B.compileCTs}`)
  for (const k of ['stop', 'anon_compile', 'anon_stop', 'bad_id', 'absent'] as const) {
    const a = A[k], b = B[k]
    if (a.status !== b.status) ds.push(`${label}:${k} status A=${a.status} B=${b.status}`);
    if (a.ct !== b.ct) ds.push(`${label}:${k} ct A=${a.ct} B=${b.ct}`)
    // byte-exact for error routes (no volatile fields), modulo HTML norm
    const na = normHTML(a.body), nb = normHTML(b.body)
    if (na !== nb) {
      let i = 0
      while (i < na.length && i < nb.length && na[i] === nb[i]) i++
      ds.push(`${label}:${k} body len A=${na.length} B=${nb.length} firstDiff@${i}\nA: …${na.slice(Math.max(0, i - 80), i + 120)}\nB: …${nb.slice(Math.max(0, i - 80), i + 120)}`)
    }
  }
  return ds
}

let LEG1: Leg | null = null

test.describe('@local web-go P5.2a (compile control plane) parity', () => {
  let U: { ck: string; csrf: string }
  let PID: string

  test.beforeAll(async () => {
    test.setTimeout(900_000)
    const here = path.dirname(fileURLToPath(import.meta.url))
    const cp = path.resolve(here, '..', '..', '..', '..', 'server-ce', 'nginx', 'flips', FLIPCONF)
    execFileSync('docker', ['cp', cp, `${overleafC}:/tmp/${FLIPCONF}`])
    dexe(overleafC, `mkdir -p /usr/local/share/overleaf-flips && cp -f /tmp/${FLIPCONF} /usr/local/share/overleaf-flips/${FLIPCONF}`)
    flip('strip')
    await waitGo()
    U = await login(USER)
    PID = await ensureFixtureProject(U)

    // clsi must be alive for both engines
    const cs = dexe(overleafC, 'curl -s -m 5 http://127.0.0.1:3013/status', true).trim()
    if (!cs.startsWith('CLSI is alive')) throw new Error('clsi not alive: ' + cs)

    // Pin the Node oracle BEFORE any flip: compile must be a clean success
    // with the pinned top-level contract.
    const probe = await compilePost(PID, U)
    expect(probe.status).toBe(200)
    expect(probe.ct).toBe('application/json')
    const j = parseJSON(probe.body)
    expect(j).not.toBeNull()
    expect(j.status).toBe('success')
    expect(Array.isArray(j.outputFiles)).toBe(true)
    const pdf = j.outputFiles.find((f: any) => f.path === 'output.pdf')
    expect(pdf).toBeTruthy()
    expect(pdf.url).toMatch(/^\/project\/[0-9a-f]{24}\/user\/[0-9a-f]{24}\/build\/[0-9a-f]+-[0-9a-f]+\//)
    expect(typeof pdf.createdAt).toBe('string')
    expect(Array.isArray(pdf.ranges)).toBe(true)
    expect(j.outputFilesArchive && j.outputFilesArchive.type).toBe('zip')
    expect(j.outputFilesArchive.url).toMatch(/\/build\/[0-9a-f]+-[0-9a-f]+\/output\/output\.zip$/)
    expect(j.compileGroup).toBe('standard')
    expect(j.compiler).toBe('pdflatex')
    expect(j.stats && typeof j.stats).toBe('object')
    expect(j.timings && typeof j.timings).toBe('object')
    // undefined keys must be OMITTED (res.json semantics)
    const raw = probe.body
    for (const key of ['clsiServerId', 'clsiCacheShard', 'validationProblems', 'pdfDownloadDomain', 'pdfCachingMinChunkSize']) {
      expect(raw.includes(`"${key}"`)).toBe(false)
    }
  }, 900_000)

  test.afterAll(async () => {
    try {
      flip('strip')
      await waitGo()
    } catch {}
  })

  test('leg 1: Node baseline', async () => {
    test.setTimeout(300_000)
    flip('strip')
    const leg1 = await battery(PID, U)
    LEG1 = leg1
    // pair = exactly one success + one too-recently-compiled
    const statuses = leg1.pair.map((s) => s.split('|').pop())
    expect(statuses.sort()).toEqual(['success', 'too-recently-compiled'])
    const success = leg1.pair.find((s) => s.endsWith('|success'))!
    const recent = leg1.pair.find((s) => s.endsWith('|too-recently-compiled'))!
    expect(recent.startsWith('200|application/json|')).toBe(true)
    expect(recent).toBe('200|application/json|{"status":"too-recently-compiled","outputFiles":[],"outputFilesArchive":null}|too-recently-compiled')
    expect(success.startsWith('200|application/json|')).toBe(true)
    expect(leg1.stop.status).toBe(200)
    expect(leg1.stop.ct).toBe('text/plain')
    expect(leg1.stop.body).toBe('OK')
    expect(leg1.anon_compile.status).toBe(403)
    expect(leg1.anon_stop.status).toBe(403)
    expect(leg1.bad_id.status).toBe(404)
    expect(leg1.bad_id.body).toContain('Invalid Mongo ObjectId at \\"params.Project_id\\"')
    expect(leg1.absent.status).toBe(404)
    expect(leg1.absent.ct).toBe('text/html')
    expect(leg1.absent.body).toContain('Page Not Found')
  })

  test('leg 2: Go parity', async () => {
    test.setTimeout(300_000)
    flip('apply')
    const leg2 = await battery(PID, U)
    flip('strip')
    if (!LEG1) throw new Error('leg1 missing')
    const ds = diffLegs('p52a', LEG1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('leg 3: Node re-baseline', async () => {
    test.setTimeout(300_000)
    flip('strip')
    const leg3 = await battery(PID, U)
    if (!LEG1) throw new Error('leg1 missing')
    const d1 = diffLegs('p52a-leg3', LEG1, leg3)
    if (d1.length) console.log(d1.join('\n'))
    expect(d1, d1.join('\n')).toHaveLength(0)
  })
})
