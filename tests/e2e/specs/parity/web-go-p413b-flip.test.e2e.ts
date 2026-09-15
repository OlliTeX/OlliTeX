// P4.13b: new-project zip upload (POST /project/new/upload) parity —
// Node baseline -> Go parity -> Node re-baseline through the 7420 nginx
// surface with the web-p413b flip applied for leg 2 only.
//
// Oracle (Node pinned, fresh sessions 2026-09-15, in-container probes):
//   200 {"success":true,"project_id":"<hex24>"}           (zip import ok)
//   400 {"success":false,"error":"invalid_upload_request"}(no qqfile)
//   400 VA  "Invalid input: expected string, received undefined at \"body.name\""
//           "Too small: expected string to have >=1 characters at \"body.name\""
//           "Unrecognized key: \"foo\" at \"body\""
//           "Path is absolute at \"body.relativePath\""
//           "Path traversal detected at \"body.relativePath\""
//   403 text/plain "Forbidden" (anonymous — global CSRF before requireLogin)
//   422 {"success":false,"error":"Invalid zip file"}      (bad/zero/all-ignored)
//   422 {"success":false,"error":"Zip doesn’t contain any file"} (dir-only)
//   500 {"success":false,"error":"Upload failed"}         (blocked name after
//       creation → zip-import-failure cleanup, NO deleterId/ipAddress)
//   500 HTML generic error page (multer LIMIT_UNEXPECTED_FILE, field != qqfile)
//   413 nginx Request Entity Too Large (>50MiB via client_max_body_size)
//
// Side-effect parity (mongo + docstore + DU-ops dumps per leg):
//   single top-level dir stripped; rootDoc = depth/main.tex/size/path first
//   with \documentclass; name = detex(\title{…}) || body.name, unique per
//   owner (' (n)' suffix); docs import via docstore PUT rev0 (text-ext with
//   ANY decodable utf8/utf16le-BOM/latin1 content, no NUL/surrogates,
//   <max_doc_length) else v1H blob (git sha1); $set rootFolder + $inc
//   version→1; DU add-doc-then-add-file ops (source=null).
//
// Rate limiting: Node's project-upload limiter (20pts/60s) lives in redis
// (web settings do not map OVERLEAF_DISABLE_RATE_LIMITS). The gate flushes
// rate-limit:project-upload* before EVERY POST so both legs see a cold
// window (Go has no uploader limiter yet — P5 scope).
//
// ZIP fixtures are built in-test with a minimal store-method writer
// (CRC32 via node:zlib; format validated against python zipfile -t).

import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import zlib from 'node:zlib'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const redisC = 'ol-e2e-redis-1'
const FLIPCONF = 'web-p413b.conf'
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }

const T_TITLED = 'Zip LaTeX Import'
const T_VAGO = 'VA GO'
const RUN = 'p413b-' + String(Date.now()).slice(-5)

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function dexe(c: string, cmd: string, capture = false): string {
  const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore' })
  return out || ''
}
function dexeQ(c: string, cmd: string): string {
  return (execFileSync('docker', ['exec', c, 'mongosh', '--quiet', 'sharelatex', '--eval', cmd], { encoding: 'utf8' }) || '').trim()
}
function dexeRaw(c: string, args: string[]): string {
  return (execFileSync('docker', ['exec', c, ...args], { encoding: 'utf8' }) || '').trim()
}

function flushLimits(): void {
  try {
    const keys = dexeRaw(redisC, ['redis-cli', '--scan', '--pattern', 'rate-limit:project-upload*'])
    if (keys) for (const k of keys.split('\n')) if (k) dexe(redisC, `redis-cli DEL "${k}" >/dev/null`)
  } catch {}
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
inc = "  include /etc/nginx/overleaf-flips/${FLIPCONF};\n"
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
  void sleep(2000)
}

// ---------- zip writer (store method) -------------------------------------------
function zipStore(entries: Array<{ name: string; data: Buffer }>): Buffer {
  const d = new Date(2026, 8, 15, 12, 0, 0)
  const t = (d.getHours() << 11) | (d.getMinutes() << 5) | (d.getSeconds() >> 1)
  const dt = (((d.getFullYear() - 1980) & 0x7f) << 9) | ((d.getMonth() + 1) << 5) | d.getDate()
  const locals: Buffer[] = []
  const centrals: Buffer[] = []
  let offset = 0
  for (const e of entries) {
    const nameB = Buffer.from(e.name)
    const data = e.data
    const crc = zlib.crc32 ? (zlib.crc32(data) >>> 0) : 0
    const lh = Buffer.alloc(30)
    lh.writeUInt32LE(0x04034b50, 0)
    lh.writeUInt16LE(20, 4)
    lh.writeUInt16LE(0, 6)
    lh.writeUInt16LE(0, 8)
    lh.writeUInt16LE(t, 10)
    lh.writeUInt16LE(dt, 12)
    lh.writeUInt32LE(crc, 14)
    lh.writeUInt32LE(data.length, 18)
    lh.writeUInt32LE(data.length, 22)
    lh.writeUInt16LE(nameB.length, 26)
    locals.push(Buffer.concat([lh, nameB, data]))
    const ch = Buffer.alloc(46)
    ch.writeUInt32LE(0x02014b50, 0)
    ch.writeUInt16LE(20, 4)
    ch.writeUInt16LE(20, 6)
    ch.writeUInt16LE(0, 8)
    ch.writeUInt16LE(0, 10)
    ch.writeUInt16LE(t, 12)
    ch.writeUInt16LE(dt, 14)
    ch.writeUInt32LE(crc, 16)
    ch.writeUInt32LE(data.length, 20)
    ch.writeUInt32LE(data.length, 24)
    ch.writeUInt16LE(nameB.length, 28)
    ch.writeUInt16LE(0, 30)
    ch.writeUInt16LE(0, 32)
    ch.writeUInt16LE(0, 34)
    ch.writeUInt16LE(0, 36)
    ch.writeUInt32LE(0, 38)
    ch.writeUInt32LE(offset, 42)
    centrals.push(Buffer.concat([ch, nameB]))
    offset += lh.length + nameB.length + data.length
  }
  const central = Buffer.concat(centrals)
  const eocd = Buffer.alloc(22)
  eocd.writeUInt32LE(0x06054b50, 0)
  eocd.writeUInt16LE(0, 4)
  eocd.writeUInt16LE(0, 6)
  eocd.writeUInt16LE(entries.length, 8)
  eocd.writeUInt16LE(entries.length, 10)
  eocd.writeUInt32LE(central.length, 12)
  eocd.writeUInt32LE(offset, 16)
  return Buffer.concat([...locals, central, eocd])
}

// ---------- fixtures (bytes pinned from Node oracle runs) ------------------------
const B = (s: string) => Buffer.from(s, 'utf8')
const MAIN_DOCCLASS = B('\\documentclass{article}\n\\begin{document}\nHello main.\n\\end{document}\n')
const TEX_TITLED = B('\\documentclass{article}\n\\title{Zip \\LaTeX Import}\\begin{document}\nx\n\\end{document}\n')
const TX_VA = B('\\documentclass{article}\n\\title{VA GO}\\begin{document}\nx\n\\end{document}\n')
const SUB_TEX = B('\\section{sub}\n')
const PIC = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4, 5, 6, 7, 8])
const MD = B('# notes\nsome text\n')
const TXT = B('plain text file\n')
const LATIN1_DOC = Buffer.concat([B('\\documentclass{article}\nhello '), Buffer.from([0xe9]), B(' caf'), Buffer.from([0xe9]), B('\n')])
const U16_DOC = Buffer.concat([Buffer.from([0xff, 0xfe]), Buffer.from('\u00fctext doc\n', 'utf16le')])

const F: Record<string, Buffer> = {
  good1: zipStore([
    { name: 'proj/', data: Buffer.alloc(0) },
    { name: 'proj/main.tex', data: TEX_TITLED },
    { name: 'proj/notes.md', data: MD },
    { name: 'proj/sub/other.tex', data: SUB_TEX },
    { name: 'proj/sub/pic.png', data: PIC },
  ]),
  rootonly: zipStore([
    { name: 'main.tex', data: MAIN_DOCCLASS },
    { name: 'readme.txt', data: TXT },
  ]),
  notex: zipStore([
    { name: 'image.png', data: PIC },
    { name: 'readme.txt', data: TXT },
  ]),
  notitle: zipStore([{ name: 'main.tex', data: MAIN_DOCCLASS }]),
  va: zipStore([{ name: 'm/main.tex', data: TX_VA }]),
  bad: Buffer.from('not a zip file at all, just garbage bytes 1234567890'),
  emptyzero: zipStore([]),
  dirsonly: zipStore([{ name: 'inner/', data: Buffer.alloc(0) }]),
  ignoredonly: zipStore([
    { name: 'notes.aux', data: TXT },
    { name: '__MACOSX/._x', data: TXT },
    { name: '.hidden', data: TXT },
  ]),
  blocked: zipStore([{ name: 'toString', data: MAIN_DOCCLASS }]),
  latin1: zipStore([{ name: 'main.tex', data: LATIN1_DOC }]),
  u16: zipStore([{ name: 'main.tex', data: U16_DOC }]),
}

// ---------- http helpers ----------------------------------------------------------
type R = { status: number; ct: string; body: string; setcookie: string }
async function call(p: string, init: { method?: string; headers?: Record<string, string>; cookie?: string; body?: any } = {}): Promise<R> {
  const h: Record<string, string> = { ...(init.headers || {}) }
  if (init.cookie) h['cookie'] = init.cookie
  // retry transient socket resets (nginx worker swap right after a reload can
  // drop a reused keep-alive connection mid-request)
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
  const sid = (logged.setcookie.split('\n').find((l) => l.includes('overleaf.sid')) || '').replace(/^overleaf\.sid=/, '').split(';')[0]
  const ck = sid ? 'overleaf.sid=' + sid : ck0
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body.trim() || csrf0 }
}

const norm = (s: string): string => s
  .replace(/\b[0-9a-f]{24}\b/g, '<HEX24>')
  .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
  .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
  .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
  .replace(/name="_csrf"\s+type="hidden"\s+value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
  .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z/g, '<TS>')

// ---------- per-leg re-seed (fresh name space, same every leg) --------------------
async function reseedAwait(U: { ck: string; csrf: string }): Promise<void> {
  dexeQ(mongoC, `
    const re = { $regex: "^${RUN}|^${T_TITLED} |^${T_TITLED}$|^${T_VAGO} |^${T_VAGO}$" };
    db.projects.deleteMany({ name: re });
    db.deletedProjects.deleteMany({ 'project.name': re });
  `)
  flushLimits()
  const r = await call('/project/new', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': U.csrf, accept: 'application/json' },
    cookie: U.ck,
    body: JSON.stringify({ projectName: T_TITLED }),
  })
  if (r.status !== 200) throw new Error('pre-collision create failed: ' + r.status + ' ' + r.body.slice(0, 140))
}

// ---------- state dump (mongo projects + DU ops + docstore contents) --------------
function duOps(pid: string): string {
  try {
    const type = dexeRaw(redisC, ['redis-cli', 'TYPE', `ProjectHistory:Ops:{${pid}}`]).trim()
    let raw = ''
    if (type.toLowerCase() === 'list') raw = dexeRaw(redisC, ['redis-cli', 'LRANGE', `ProjectHistory:Ops:{${pid}}`, '0', '-1'])
    else raw = dexeRaw(redisC, ['redis-cli', 'GET', `ProjectHistory:Ops:{${pid}}`])
    if (raw.startsWith('"')) {
      try {
        raw = JSON.parse(raw)
      } catch {}
    }
    return raw
      .replace(/\b[0-9a-f]{24}\b/g, '<ID>')
      .replace(/"ts":"[^"]*"/g, '"ts":"<TS>"')
      .replace(/"created":"[^"]*"/g, '"created":"<TS>"')
  } catch {
    return ''
  }
}

function docstoreDoc(pid: string): unknown {
  try {
    const did = dexeQ(mongoC, `print(db.projects.findOne({_id: new ObjectId("${pid}")}).rootFolder[0].docs[0]._id.toString())`)
    const raw = dexeRaw(overleafC, ['curl', '-s', `http://127.0.0.1:3016/project/${pid}/doc/${did}`])
    try {
      const j = JSON.parse(raw)
      return { lines: j.lines, rev: j.rev, version: j.version }
    } catch {
      return { raw: raw.slice(0, 200) }
    }
  } catch {
    return null
  }
}

function stateDump(): unknown {
  try {
    const t = dexeQ(mongoC, `
      const out = {};
      const re = { $regex: "^${RUN}|^${T_TITLED} |^${T_TITLED}$|^${T_VAGO} |^${T_VAGO}$" };
      function flat(f) {
        const o = [];
        (function rec(x) {
          (x.docs||[]).forEach(d => o.push('doc:' + d.name));
          (x.fileRefs||[]).forEach(d => o.push('file:' + d.name + ':rev' + d.rev + ':h' + String(d.hash||'').slice(0,8)));
          (x.folders||[]).forEach(s => { o.push('folder:' + s.name); rec(s); });
        })(f);
        return o.sort();
      }
      db.projects.find({ name: re }).forEach(p => {
        const rf = p.rootFolder[0];
        let rootPath = null;
        if (p.rootDoc_id) {
          (function rec(x, pre) {
            (x.docs||[]).forEach(d => { if (String(d._id) === String(p.rootDoc_id)) rootPath = pre + '/' + d.name; });
            (x.folders||[]).forEach(s => rec(s, pre + '/' + s.name));
          })(rf, '');
        }
        out[p.name] = {
          ver: p.version, lub: String(p.lastUpdatedBy||''), rootPath,
          flat: flat(rf),
          rfKeys: Object.keys(rf).sort().join(','),
          owner: p.owner_ref, spell: p.spellCheckLanguage, compiler: p.compiler,
          trashed: !!p.trashed, pub: p.publicAccesLevel,
          docKeys: (rf.docs||[]).length ? Object.keys(rf.docs[0]).sort().join(',') : null,
          fileKeys: (rf.fileRefs||[]).length ? Object.keys(rf.fileRefs[0]).sort().join(',') : null,
        };
      });
      const del = db.deletedProjects.findOne({ 'project.name': { $regex: "^${RUN}" } });
      out.__deleted = del ? {
        found: true,
        reason: del.deleterData.deletedReason,
        keys: Object.keys(del.deleterData).sort().join(','),
        hasDeleterId: 'deleterId' in del.deleterData,
        hasIp: 'deleterIpAddress' in del.deleterData,
      } : { found: false };
      print(JSON.stringify(out));
    `)
    const parsed: Record<string, any> = JSON.parse(t.split('\n').pop() || '{}')
    const pidsTxt = dexeQ(mongoC, `
      db.projects.find({ name: { $regex: "^${RUN}|^${T_TITLED} |^${T_TITLED}$|^${T_VAGO} |^${T_VAGO}$" } }).forEach(p => print(p.name + '|' + p._id));
    `)
    for (const line of pidsTxt.split('\n')) {
      const [name, pid] = line.split('|')
      if (!name || !pid || !parsed[name]) continue
      parsed[name].duOps = duOps(pid)
      if (name.includes('Latin1') || name.includes('U16') || name.includes('No Title') || name.includes('Root Only')) {
        parsed[name].docstore = docstoreDoc(pid)
      }
    }
    return parsed
  } catch (e) {
    return { err: String(e) }
  }
}

// ---------- battery (fixed case order; creates happen in this order) --------------
type Leg = { cases: Record<string, R>; state: unknown }

async function battery(U: { ck: string; csrf: string }): Promise<Leg> {
  const cases: Record<string, R> = {}
  const hdr = () => ({ 'x-csrf-token': U.csrf, cookie: U.ck, accept: 'application/json' })
  const mk = (zip: Buffer, name: string | null, extra: Record<string, string> = {}, field: string | null = 'qqfile') => {
    const f = new FormData()
    if (name != null) f.append('name', name)
    for (const [k, v] of Object.entries(extra)) f.append(k, v)
    if (field) f.append(field, new Blob([zip as any], { type: 'application/zip' }), 'upload.zip')
    return f
  }
  const post = async (k: string, body: FormData, h?: Record<string, string>) => {
    flushLimits()
    cases[k] = await call('/project/new/upload', { method: 'POST', headers: h || hdr(), body })
    return cases[k]
  }

  // ---- import successes (title wins over body.name for naming) ----
  cases.good1 = await post('good1', mk(F.good1, `${RUN} ${T_TITLED}`))
  cases.rootonly = await post('rootonly', mk(F.rootonly, `${RUN} Root Only Zip`))
  cases.notex = await post('notex', mk(F.notex, `${RUN} NoTex Zip`))
  cases.notitle = await post('notitle', mk(F.notitle, `${RUN} No Title Zip`))
  // ---- VA edges (enforce mode; none of these create projects) ----
  cases.noname = await post('noname', (
    (() => {
      const f = new FormData()
      f.append('qqfile', new Blob([F.good1 as any]), 'u.zip')
      return f
    })()
  ))
  cases.nameempty = await post('nameempty', mk(F.notitle, ''))
  cases.unknownkey = await post('unknownkey', (
    (() => {
      const f = new FormData()
      f.append('name', `${RUN} UK`)
      f.append('foo', 'bar')
      f.append('qqfile', new Blob([F.notitle as any]), 'u.zip')
      return f
    })()
  ))
  cases.relpathabs = await post('relpathabs', mk(F.notitle, `${RUN} RP1`, { relativePath: '/abs/x' }))
  cases.relpathdot = await post('relpathdot', mk(F.notitle, `${RUN} RP2`, { relativePath: '..' }))
  // ---- VA-legal extras (create projects named from the VA GO title) ----
  cases.relpathok = await post('relpathok', mk(F.va, `${RUN} RPOK`, { relativePath: 'a/b' }))
  cases.typeweird = await post('typeweird', mk(F.va, `${RUN} TW`, { type: '123' }))
  // ---- zip guard errors ----
  cases.bad = await post('bad', mk(F.bad, `${RUN} Bad Zip`))
  cases.emptyzero = await post('emptyzero', mk(F.emptyzero, `${RUN} Empty Zero`))
  cases.dirsonly = await post('dirsonly', mk(F.dirsonly, `${RUN} Dirs Only`))
  cases.ignoredonly = await post('ignoredonly', mk(F.ignoredonly, `${RUN} Ignored Only`))
  // ---- blocked name after creation → 500 + zip-import-failure cleanup ----
  cases.blocked = await post('blocked', mk(F.blocked, `${RUN} Blocked Zip`))
  // ---- encoding edges (docs with non-utf8 content) ----
  cases.latin1 = await post('latin1', mk(F.latin1, `${RUN} Latin1 Zip`))
  cases.u16 = await post('u16', mk(F.u16, `${RUN} U16 Zip`))
  // ---- multipart / auth surface ----
  cases.noqqfile = await post('noqqfile', (
    (() => {
      const f = new FormData()
      f.append('name', `${RUN} NoFile`)
      return f
    })()
  ))
  cases.wrongfield = await post('wrongfield', mk(F.good1, `${RUN} WF`, {}, 'otherfield'))
  cases.anon = await post(
    'anon',
    (() => {
      const f = new FormData()
      f.append('name', 'anon')
      f.append('qqfile', new Blob([F.good1 as any]), 'u.zip')
      return f
    })(),
    { 'x-csrf-token': 'faketoken', accept: 'application/json' }
  )
  cases.hugefile = await post('hugefile', (
    (() => {
      const f = new FormData()
      f.append('name', `${RUN} Huge`)
      f.append('qqfile', new Blob([Buffer.alloc(51 * 1024 * 1024, 1)], { type: 'application/zip' }), 'u.zip')
      return f
    })()
  ))

  const state = stateDump()
  return { cases, state }
}

function diffLegs(label: string, a: Leg, b: Leg): string[] {
  const ds: string[] = []
  for (const k of Object.keys(a.cases)) {
    if (!b.cases[k]) {
      ds.push(`${label}:${k} missing in leg B`)
      continue
    }
    const A = a.cases[k], B = b.cases[k]
    if (A.status !== B.status) {
      ds.push(`${label}:${k} status A=${A.status} B=${B.status}`)
      continue
    }
    if (A.ct !== B.ct) {
      ds.push(`${label}:${k} ct A=${A.ct} B=${B.ct}`)
      continue
    }
    const na = norm(A.body), nb = norm(B.body)
    if (na !== nb) ds.push(`${label}:${k} body len A=${na.length} B=${nb.length}\nA: ${na.slice(0, 260)}\nB: ${nb.slice(0, 260)}`)
  }
  const ja = JSON.stringify(a.state), jb = JSON.stringify(b.state)
  if (ja !== jb) ds.push(`${label}:state\nA: ${ja.slice(0, 450)}\nB: ${jb.slice(0, 450)}`)
  return ds
}

let LEG1: Leg | null = null

test.describe('@local web-go P4.13b (new-upload) parity', () => {
  let U: { ck: string; csrf: string }

  test.beforeAll(async () => {
    test.setTimeout(600_000)
    const here = path.dirname(fileURLToPath(import.meta.url))
    const cp = path.resolve(here, '..', '..', '..', '..', 'server-ce', 'nginx', 'flips', FLIPCONF)
    execFileSync('docker', ['cp', cp, `${overleafC}:/tmp/${FLIPCONF}`])
    dexe(overleafC, `mkdir -p /usr/local/share/overleaf-flips && cp -f /tmp/${FLIPCONF} /usr/local/share/overleaf-flips/${FLIPCONF}`)
    flip('strip')
    await waitGo()
    U = await login(USER)
  }, 600_000)

  test.afterAll(async () => {
    try {
      flip('strip')
      await waitGo()
    } catch {}
  })

  test('leg 1: Node baseline', async () => {
    test.setTimeout(300_000)
    flip('strip')
    await reseedAwait(U)
    const leg1 = await battery(U)
    LEG1 = leg1
    const c = (k: string) => leg1.cases[k]
    // Node oracle pins (fresh sessions, 2026-09-15)
    expect(c('good1').status).toBe(200)
    expect(c('good1').ct).toBe('application/json')
    expect(c('good1').body).toMatch(/"success":true,"project_id":"[0-9a-f]{24}"/)
    expect(c('rootonly').status).toBe(200)
    expect(c('notex').status).toBe(200)
    expect(c('notitle').status).toBe(200)
    expect(c('noname').body).toBe('{"error":"Validation error: Invalid input: expected string, received undefined at \\"body.name\\"","statusCode":400}')
    expect(c('nameempty').body).toBe('{"error":"Validation error: Too small: expected string to have >=1 characters at \\"body.name\\"","statusCode":400}')
    expect(c('unknownkey').body).toBe('{"error":"Validation error: Unrecognized key: \\"foo\\" at \\"body\\"","statusCode":400}')
    expect(c('relpathabs').body).toBe('{"error":"Validation error: Path is absolute at \\"body.relativePath\\"","statusCode":400}')
    expect(c('relpathdot').body).toBe('{"error":"Validation error: Path traversal detected at \\"body.relativePath\\"","statusCode":400}')
    expect(c('relpathok').status).toBe(200)
    expect(c('typeweird').status).toBe(200)
    expect(c('bad').body).toBe('{"success":false,"error":"Invalid zip file"}')
    expect(c('emptyzero').body).toBe('{"success":false,"error":"Invalid zip file"}')
    expect(c('dirsonly').body).toBe('{"success":false,"error":"Zip doesn\u2019t contain any file"}')
    expect(c('ignoredonly').body).toBe('{"success":false,"error":"Invalid zip file"}')
    expect(c('blocked').status).toBe(500)
    expect(c('blocked').body).toBe('{"success":false,"error":"Upload failed"}')
    expect(c('latin1').status).toBe(200)
    expect(c('u16').status).toBe(200)
    expect(c('noqqfile').body).toBe('{"success":false,"error":"invalid_upload_request"}')
    expect(c('wrongfield').status).toBe(500)
    expect(c('wrongfield').ct).toBe('text/html')
    expect(c('wrongfield').body).toContain('placeholder@example.com')
    expect(c('anon').status).toBe(403)
    expect(c('anon').ct).toBe('text/plain')
    expect(c('anon').body).toBe('Forbidden')
    expect(c('hugefile').status).toBe(413)
    expect(c('hugefile').ct).toBe('text/html')
    // state pins
    const st = leg1.state as Record<string, any>
    expect(st.__deleted).toEqual({
      found: true,
      reason: 'zip-import-failure',
      keys: expect.stringContaining('deletedReason'),
      hasDeleterId: false,
      hasIp: false,
    })
    const g1 = st[T_TITLED + ' (1)']
    expect(g1).toBeTruthy()
    expect(g1.ver).toBe(1)
    expect(g1.rootPath).toBe('/main.tex')
    expect(g1.flat).toEqual(['doc:main.tex', 'doc:notes.md', 'doc:other.tex', 'file:pic.png:rev0:h6b8f2521', 'folder:sub'])
    expect(st[T_VAGO]).toBeTruthy()
    expect(st[T_VAGO].rootPath).toBe('/main.tex')
    expect(st[`${RUN} NoTex Zip`].rootPath).toBeNull()
    expect(st[`${RUN} Latin1 Zip`].flat).toEqual(['doc:main.tex'])
    expect(st[`${RUN} Latin1 Zip`].docstore).toBeTruthy()
    expect(st[`${RUN} U16 Zip`].flat).toEqual(['doc:main.tex'])
  })

  test('leg 2: Go parity', async () => {
    test.setTimeout(300_000)
    flip('apply')
    await reseedAwait(U)
    const leg2 = await battery(U)
    flip('strip')
    const ds = diffLegs('p413b', LEG1, leg2)
    if (ds.length) console.log(ds.join('\n'))
    expect(ds, ds.join('\n')).toHaveLength(0)
  })

  test('leg 3: Node re-baseline', async () => {
    test.setTimeout(300_000)
    flip('strip')
    await reseedAwait(U)
    const leg3 = await battery(U)
    const d1 = diffLegs('p413b-leg3', LEG1, leg3)
    if (d1.length) console.log(d1.join('\n'))
    expect(d1, d1.join('\n')).toHaveLength(0)
  })
})
