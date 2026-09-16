/**
 * WEB-GO P5.2b FLIP GATE (WEB_GO_PLAN.md P5.2b — COMPILE OUTPUT READ):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p52b.conf):
 *     GET /download/project/:pid/build/:build_id/output/output.pdf
 *     GET /download/project/:pid/build/:editorBuildId/output/cached/:file
 *     GET /project/:pid/output/cached/output.overleaf.json
 *
 *   The PDF PREVIEW pane is not in this surface — overleaf.conf proxies
 *   the anonymous/user `/project/…/build/…/output.output.*` fast path
 *   straight to clsi-nginx (:8080) and that stays after the cutover.
 *   The download-PDF button and the clsi-cache shapes ARE web routes.
 *
 *   leg 1  Node baseline       (flip OFF)
 *   leg 2  FLIP ON — Go        (flip ON , battery, flip OFF)
 *   leg 3  Node re-baseline    (flip OFF)
 *
 *   Battery (deterministic; compiles fresh per leg so each leg is
 *   self-contained; clsi artifact is downloaded, not recompiled):
 *     pdf_inline     ⇒ 200 application/pdf, inline; filename="P52b_Oracle.pdf",
 *                      pdf sha256 + length
 *     pdf_popup      ⇒ 200, attachment; filename="P52b_Oracle.pdf", same sha
 *     pdf_badbid     ⇒ 404 JSON "Invalid buildId at \"params.build_id\""
 *     pdf_missing    ⇒ 404 application/pdf + inline CD, EMPTY body
 *     cached_json    ⇒ 404 text/plain "Not Found" (clsi-cache disabled)
 *     cached_file    ⇒ 404 NO content-type, empty body
 *     cached_badfile ⇒ 404 JSON "Path is not allowed at \"params.filename\""
 *     cached_badbid  ⇒ 404 JSON "Invalid editorId-buildId at ..."
 *     auth403        ⇒ 403 user/restricted HTML (non-member fixture)
 *     anon           ⇒ 302 Location /login "Found. Redirecting to /login"
 *     anon_json      ⇒ 401 "Unauthorized" (Accept: application/json)
 *
 *   Node oracle pins (live 2026-09-16, /tmp/p52b/oracle.json + probes).
 */
import { execFileSync } from 'node:child_process'
import { test, expect } from '@playwright/test'
import crypto from 'node:crypto'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const redisC = 'ol-e2e-redis-1'
const FLIPCONF = 'web-p52b.conf'
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const FIX_NAME = 'P52b Oracle'
const BAD_BID = 'deadbeefdeadbeefdeadbeef'
const MISSING_BID = '6aa9c6340000000000000000-1'
const FIXED_EBID = '00000000' + '-1111-2222-3333-444455556666' + '-' + '1a0a7b4d47a' + '-' + '32ed7a555de306bd'
const BAD_EBID = 'nope'

type Leg = Record<string, { status: number; ct: string; cd?: string; loc?: string; body?: string; sha?: string; len: number }>

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
const sha256 = (s: string): string => crypto.createHash('sha256').update(s).digest('hex')

// Node's downloadPdf route carries a per-IP limiter (full-pdf-download).
// Flush anyway so a dirty pre-state cannot 429 only the Node leg.
function flushLimiter(): void {
  for (let db = 0; db <= 7; db++) {
    dexe(redisC, `redis-cli -n ${db} --scan --pattern 'rate-limit:full-pdf-download*' 2>/dev/null | while read -r k; do [ -n "$k" ] && redis-cli -n ${db} del "$k" >/dev/null 2>&1; done; true`)
    dexe(redisC, `redis-cli -n ${db} --scan --pattern 'compile:*' 2>/dev/null | while read -r k; do [ -n "$k" ] && redis-cli -n ${db} del "$k" >/dev/null 2>&1; done; true`)
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

async function call(p: string, init: { method?: string; headers?: Record<string, string>; cookie?: string; body?: any } = {}): Promise<any> {
  const h: Record<string, string> = { ...(init.headers || {}) }
  if (init.cookie) h['cookie'] = init.cookie
  let lastErr: unknown = null
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      const r = await fetch(BASE + p, { method: init.method || 'GET', headers: h, body: init.body as any, redirect: 'manual' })
      const buf = Buffer.from(await r.arrayBuffer())
      const setcookie = r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : (r.headers.get('set-cookie') || '').toString()
      return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], cd: r.headers.get('content-disposition'), loc: r.headers.get('location'), body: buf.toString('latin1'), len: buf.length, setcookie }
    } catch (e) {
      lastErr = e
      await sleep(400 * (attempt + 1))
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
  const sid = ((logged.setcookie || '').split('\n').find((l: string) => l.includes('overleaf.sid')) || '').replace(/^overleaf\.sid=/, '').split(';')[0]
  const ck = sid ? 'overleaf.sid=' + sid : ck0
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body.trim() || csrf0 }
}

const FLIP_ANCHOR = 'location / {'

function stripFlips(): void {
  const vhost = '/etc/nginx/sites-enabled/overleaf.conf'
  dexe(
    overleafC,
    `node -e "const fs=require('fs');const p='${vhost}';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"`
  )
  dexe(overleafC, 'nginx -t && nginx -s reload')
}

function flip(mode: 'apply' | 'strip'): void {
  stripFlips()
  if (mode === 'strip') return
  const vhost = '/etc/nginx/sites-enabled/overleaf.conf'
  dexe(overleafC, `
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/${FLIPCONF} /etc/nginx/overleaf-flips/${FLIPCONF}
node -e "const fs=require('fs');const p='${vhost}';let s=fs.readFileSync(p,'utf8');const inc='  include /etc/nginx/overleaf-flips/${FLIPCONF};'+String.fromCharCode(10);if(s.includes('location / {')){s=s.replace('location / {',inc+'location / {',1)};else{throw new Error('anchor not found')};fs.writeFileSync(p,s)"
nginx -t && nginx -s reload
`)
}

async function battery(mePid: string, othPid: string, U: { ck: string; csrf: string }): Promise<Leg> {
  const out: Leg = {}
  flushLimiter()
  // fresh compile (Node leg: Node compile; Go leg: Node compile is the
  // engine either way — the artifact is what download reads).
  const csrfR = await call('/dev/csrf', { cookie: U.ck, headers: { accept: 'text/plain' } })
  const csrf1 = (csrfR.body || '').trim()
  const comp = await call(`/project/${mePid}/compile`, { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf1, accept: 'application/json' }, cookie: U.ck, body: '{}' })
  let bid = ''
  try {
    const j = JSON.parse(comp.body)
    bid = (j.outputFiles && j.outputFiles[0] && j.outputFiles[0].build) || ''
  } catch {}
  expect(bid, 'compile must yield a buildId: ' + comp.status + ' ' + comp.body.slice(0, 200)).toBeTruthy()

  const dl = () => `/download/project/${mePid}/build/`
  out.pdf_inline = await call(dl() + bid + '/output/output.pdf', { cookie: U.ck })
  out.pdf_popup = await call(dl() + bid + '/output/output.pdf?popupDownload=1', { cookie: U.ck })
  out.pdf_badbid = await call(dl() + BAD_BID + '/output/output.pdf', { cookie: U.ck })
  out.pdf_missing = await call(dl() + MISSING_BID + '/output/output.pdf', { cookie: U.ck })
  out.cached_json = await call(`/project/${mePid}/output/cached/output.overleaf.json`, { cookie: U.ck })
  out.cached_file = await call(`/download/project/${mePid}/build/${FIXED_EBID}/output/cached/output.pdf`, { cookie: U.ck })
  out.cached_badfile = await call(`/download/project/${mePid}/build/${FIXED_EBID}/output/cached/evil.txt`, { cookie: U.ck })
  out.cached_badbid = await call(`/download/project/${mePid}/build/${BAD_EBID}/output/cached/output.pdf`, { cookie: U.ck })
  out.auth403 = await call(`/download/project/${othPid}/build/${bid}/output/output.pdf`, { cookie: U.ck })
  out.anon = await call(dl() + bid + '/output/output.pdf')
  out.anon_json = await call(dl() + bid + '/output/output.pdf', { headers: { accept: 'application/json' } })
  return out
}

function normHTML(s: string): string {
  return s
    .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
    .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
    .replace(/name="_csrf"[^>]*value="[^"]*"/g, 'name="_csrf" value="CSRF"')
    .replace(/build\/[0-9a-f][0-9a-f-]+/g, 'build/<BID>') // requested-path echo (leg-specific)
    .replace(/\b[0-9a-f]{24}\b/g, '<H24>')
    .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z/g, '<TS>')
}

// pdf bytes are identical except per-compile volatile fields (pdfTeX
// D:YYYYMMDDHHMMSSZ CreationDate/ModDate + derived /ID trailer) — P5.2
// plan: normalize volatile fields, do NOT byte-diff raw.
const pdfNorm = (b: string): string =>
  b.replace(/D:\d{14}Z/g, 'D:<TS>Z').replace(/\/ID \[<[0-9A-Fa-f]+> <[0-9A-Fa-f]+>\]/g, '/ID [<ID> <ID>]')

function firstDiff(a: string, b: string): number {
  const n = Math.min(a.length, b.length)
  for (let i = 0; i < n; i++) if (a[i] !== b[i]) return i
  return n
}
function ctx(a: string, b: string, i: number): string {
  const s0 = Math.max(0, i - 80)
  return `...A: ${a.slice(s0, i + 140).replace(/\n/g, '\\n')}\n...B: ${b.slice(s0, i + 140).replace(/\n/g, '\\n')}`
}

function diffLegs(name: string, a: Leg, b: Leg): string[] {
  const ds: string[] = []
  const keys = Object.keys(a).sort()
  for (const k of keys) {
    const A = a[k]
    const B = b[k]
    if (!B) {
      ds.push(`${name}:${k} missing in B`)
      continue
    }
    if (A.status !== B.status) {
      ds.push(`${name}:${k} status A=${A.status} B=${B.status}`)
      continue
    }
    if (A.ct !== B.ct) {
      ds.push(`${name}:${k} ct A='${A.ct}' B='${B.ct}' (status ${A.status})`)
      continue
    }
    if ((A.cd || '') !== (B.cd || '')) {
      ds.push(`${name}:${k} cd A='${A.cd}' B='${B.cd}' (status ${A.status})`)
    }
    if ((A.loc || '') !== (B.loc || '')) {
      ds.push(`${name}:${k} loc A='${A.loc}' B='${B.loc}' (status ${A.status})`)
    }
    let bodyA = A.body || ''
    let bodyB = B.body || ''
    if (k === 'auth403') {
      bodyA = normHTML(bodyA)
      bodyB = normHTML(bodyB)
    } else if (k === 'pdf_inline' || k === 'pdf_popup') {
      bodyA = pdfNorm(bodyA)
      bodyB = pdfNorm(bodyB)
    }
    if (bodyA !== bodyB) {
      ds.push(`${name}:${k} body len A=${A.len} B=${B.len}`)
      ds.push(ctx(bodyA, bodyB, firstDiff(bodyA, bodyB)))
    }
  }
  return ds
}

function mongoProjectByName(name: string): string {
  return (dexeQ(mongoC, `const p = db.projects.findOne({name:"${name}"}); p ? p._id.toHexString() : ""`).split('\n').pop() || '').trim()
}

test.describe('@local web-go P5.2b (output read) parity', () => {
  let U: { ck: string; csrf: string }
  let ME_PID = ''
  let OTH_PID = ''
  const LEG1: Leg = {}

  test.beforeAll(async () => {
    await waitGo()
    U = await login(USER)
    ME_PID = mongoProjectByName(FIX_NAME)
    if (!ME_PID) {
      const csrfR = await call('/dev/csrf', { cookie: U.ck, headers: { accept: 'text/plain' } })
      const r = await call('/project/new', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': (csrfR.body || '').trim(), accept: 'application/json' }, cookie: U.ck, body: JSON.stringify({ projectName: FIX_NAME }) })
      ME_PID = ((JSON.parse(r.body) || {}).project_id as string) || ''
      await sleep(1500)
    }
    OTH_PID = mongoProjectByName('P52b NotMine')
    if (!OTH_PID) {
      dexeQ(
        mongoC,
        'const pid = new ObjectId(); db.projects.insertOne({_id: pid, name: "P52b NotMine", compiler: "pdflatex", owner_ref: "ffffffffff000000000001", collaberator_refs: [], reviewer_refs: [], readOnly_refs: [], rootDoc_id: new ObjectId().toString(), fileRefs: [], created: new Date(), lastUpdated: new Date()});'
      )
      OTH_PID = mongoProjectByName('P52b NotMine')
    }
    expect(ME_PID, 'member fixture project').toHaveLength(24)
    expect(OTH_PID, 'non-member fixture project').toHaveLength(24)
  })

  test('leg 1: Node baseline', async () => {
    flip('strip')
    await sleep(300)
    Object.assign(LEG1, await battery(ME_PID, OTH_PID, U))
  })

  test('leg 2: Go parity', async () => {
    flip('apply')
    await sleep(800)
    const leg2 = await battery(ME_PID, OTH_PID, U)
    flip('strip')
    await sleep(300)
    const ds = diffLegs('p52b', LEG1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('leg 3: Node re-baseline', async () => {
    flip('strip')
    await sleep(300)
    const leg3 = await battery(ME_PID, OTH_PID, U)
    const ds = diffLegs('p52b', LEG1, leg3)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })
})
