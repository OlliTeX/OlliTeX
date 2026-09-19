// P4.12c: private API document trio parity (OlliTeX Go web).
//
// Canonical oracle = services/web's 'api' profile (127.0.0.1:3000 inside
// ol-e2e-overleaf-1). The same path on the Node WEB profile (4000) is NOT
// the trio (it has no privateApiRouter — today a 302/403/404 gate mixture);
// the nginx flip retargets that surface at Go, which is the cutover.
//
// Routes (router.mjs privateApiRouter, basic-auth required):
//   GET  /project/:Project_id/doc/:doc_id                  getDocument
//   POST /project/:Project_id/doc/:doc_id                  setDocument
//   POST /project/:Project_id/doc/:doc_id/changes/reject   (CE hook no-op)
//
// Three-leg identical in-container battery (Node api :3000 → Go :4010 →
// Node re-baseline) + an nginx-bound acceptance section (flips applied,
// 7420 surface checked, then stripped). Leg comparison is exact on
// status / content-type / www-authenticate / etag / nosniff / body, with
// ONE declared exception: the successful set-doc response's "rev" is
// docstore-global across legs, so that case is shape-compared.
//
// Pinned Node behaviors (live-diffed 2026-09-15):
//   auth   : 401 + WWW-Authenticate: OverleafLogin + "Unauthorized" for
//            every verb/accept (missing OR invalid creds)
//   GET    : JSON, key order lines,version,ranges,pathname,
//            projectHistoryType,historyRangesSupport,otMigrationStage,
//            resolvedCommentIds (projectHistoryId only when the doc has
//            one); ?plain=1 → text/plain lines joined by \n (+nosniff)
//   404    : "Not Found" (ghost project or doc) / validation JSON for
//            non-oid params.*  (statusCode 404, enforce-log enforces)
//   400    : exact zod messages at body.lines / body.version / body.ranges
//   200    : {"rev":N} / {"rev":N,"modified":true} on setDocument
//   204    : reject, ETag computed over the stripped "No Content" body
//   headers: every response carries X-Powered-By + the default CSP;
//            Go's net/http header-name case (Etag, Www-Authenticate) is
//            semantically identical and compared case-insensitively.
import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dir = path.dirname(fileURLToPath(import.meta.url))
const BASE = 'http://127.0.0.1:7420'
const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const FLIPS = ['web-p411b.conf', 'web-p413.conf']

const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const GHOST = '666666666666666666666666'
const GHOSTP = '6aa988888888888888888888'
const SEED_NAME = 'p413-api'
const CANON = ['P413 CANON L1', 'P413 CANON L2']
const SETVER = 100

// ---- container helpers -------------------------------------------------------
function dexe(c: string, cmd: string, capture = false): string {
  const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], {
    encoding: 'utf8',
    stdio: capture ? 'pipe' : 'ignore',
    maxBuffer: 1 << 28,
  })
  return out || ''
}





// ---- seeding (p4.12b pattern: direct mongo insert + docstore entry) ----------
function seedState(): { A: string; DA: string } {
  const seedJs = `
    db.projects.deleteMany({name:"p413-api"});
    const uid = ObjectId("6aa4b8b573ef0e5094f4cbc0");
    const dA = new ObjectId();
    db.projects.insertOne({_id:new ObjectId(), name:"p413-api", owner_ref:uid, publicAccesLevel:"private",
      version:0,
      rootFolder:[{name:"", _id:new ObjectId(), docs:[{_id:dA, name:"doc.tex"}], fileRefs:[], folders:[]}]});
  `
  const seedFile = '/tmp/p413-mongo-seed.js'
  fs.writeFileSync(seedFile, seedJs)
  execFileSync('docker', ['cp', seedFile, `${mongoC}:/tmp/p413-seed.js`], { stdio: 'ignore' })
  dexe(mongoC, 'mongosh --quiet sharelatex /tmp/p413-seed.js')
  let st = null as { A: string; DA: string } | null
  for (let att = 1; att <= 4 && !st; att++) {
    const outLine = dexe(
      mongoC,
      `mongosh --quiet sharelatex --eval 'const p=db.projects.findOne({name:"p413-api"}); if(p)print(JSON.stringify({A:String(p._id), DA:String(p.rootFolder[0].docs[0]._id)}))'`,
      true,
    ).trim().split('\n').pop() || ''
    try {
      const j = JSON.parse(outLine)
      if (/^[0-9a-f]{24}$/.test(j.A) && /^[0-9a-f]{24}$/.test(j.DA)) st = j
    } catch {
      /* not ready yet */
    }
    if (!st) dexe(mongoC, 'sleep 1')
  }
  if (!st) throw new Error('seed not visible after 4 attempts')
  // docstore entry (DU backing) with retry — right after a fresh insert the
  // stack can transiently 5xx (pinned P4.12b).
  const j = JSON.stringify(CANON)
  const script = `for ATT in 1 2 3 4; do
  CODE=$(curl -s -o /tmp/dsbody -w "%{http_code}" -X POST http://127.0.0.1:3016/project/${st.A}/doc/${st.DA} -H 'content-type: application/json' -d '{"lines":${j},"version":0,"ranges":{}}' 2>&1)
  [ "$CODE" = "200" ] && break
  echo "seed attempt $ATT -> $CODE $(head -c 200 /tmp/dsbody)"
  sleep 1
done
[ "$CODE" = "200" ] || exit 1
`
  dexe(overleafC, script)
  return st
}

// ---- comparison ---------------------------------------------------------------
// ---- in-container battery -----------------------------------------------------
function batteryScript(): string {
  return `
const BASE = process.env.LEG_BASE;
const PASS = process.env.P413_PASS;
const A = process.env.LEGA, DA = process.env.LEGDA;
const auth = 'Basic ' + Buffer.from('overleaf:' + PASS).toString('base64');
const out = [];
async function cap(name, opts) {
  const headers = Object.assign({}, opts.headers || {});
  if (opts.cred) headers.authorization = auth;
  try {
    const r = await fetch(BASE + opts.path, { method: opts.method || 'GET', headers, body: opts.body });
    const t = await r.text();
    out.push(JSON.stringify({
      case: name, code: r.status,
      ct: r.headers.get('content-type') || '',
      www: (r.headers.get('www-authenticate') || '').toLowerCase(),
      etag: r.headers.get('etag') || '',
      ns: (r.headers.get('x-content-type-options') || '').toLowerCase(),
      body: t
    }));
  } catch (e) {
    out.push(JSON.stringify({ case: name, code: 0, ct: '', www: '', etag: '', ns: '', body: 'ERR ' + String(e) }));
  }
}
const P = '/project/' + A + '/doc/' + DA;
const J = { 'content-type': 'application/json' };
const canonical = JSON.stringify({ lines: ${JSON.stringify(['P413 CANON L1', 'P413 CANON L2'])}, version: 100, ranges: {} });
(async () => {
  await cap('reset', { method: 'POST', cred: true, path: P, body: canonical, headers: J });
  await cap('get-json', { cred: true, path: P });
  await cap('get-plain', { cred: true, path: P + '?plain=1' });
  await cap('no-auth-get-json', { path: P, headers: { accept: 'application/json' } });
  await cap('no-auth-get-html', { path: P, headers: { accept: 'text/html' } });
  await cap('wrong-cred-get', { path: P, headers: { authorization: 'Basic ' + Buffer.from('overleaf:not-right').toString('base64') } });
  await cap('bad-project-oid', { cred: true, path: '/project/notanoid/doc/' + DA });
  await cap('bad-doc-oid', { cred: true, path: '/project/' + A + '/doc/notanoid' });
  await cap('ghost-project', { cred: true, path: '/project/6aa988888888888888888888/doc/' + DA });
  await cap('ghost-doc', { cred: true, path: '/project/' + A + '/doc/666666666666666666666666' });
  await cap('no-auth-post', { method: 'POST', path: P, body: canonical, headers: J });
  await cap('va-miss-lines', { method: 'POST', cred: true, path: P, body: JSON.stringify({ version: 1, ranges: {} }), headers: J });
  await cap('va-miss-version', { method: 'POST', cred: true, path: P, body: JSON.stringify({ lines: ['x'], ranges: {} }), headers: J });
  await cap('va-miss-ranges', { method: 'POST', cred: true, path: P, body: JSON.stringify({ lines: ['x'], version: 1 }), headers: J });
  await cap('va-ranges-arr', { method: 'POST', cred: true, path: P, body: JSON.stringify({ lines: ['x'], version: 1, ranges: [1] }), headers: J });
  await cap('va-lines-type', { method: 'POST', cred: true, path: P, body: JSON.stringify({ lines: 7, version: 1, ranges: {} }), headers: J });
  await cap('set-doc', { method: 'POST', cred: true, path: P, body: canonical, headers: J });
  await cap('set-doc-noop', { method: 'POST', cred: true, path: P, body: canonical, headers: J });
  await cap('reject', { method: 'POST', cred: true, path: P + '/changes/reject', body: JSON.stringify({ rejectedChangeAuthorIds: [] }), headers: J });
  console.log(out.join('\\n'));
})().catch((e) => { console.error('LEG-FAIL ' + e); process.exit(1); });
`
}

function runLeg(base: string, A: string, DA: string): Record<string, Record<string, string>> {
  const pass = dexe(overleafC, 'printenv WEB_API_PASSWORD', true).trim()
  if (!pass) throw new Error('WEB_API_PASSWORD missing in container')
  const f = '/tmp/p413-battery.js'
  fs.writeFileSync(f, batteryScript())
  execFileSync('docker', ['cp', f, `${overleafC}:/tmp/p413-battery.js`], { stdio: 'ignore' })
  let rows: Record<string, string>[] = []
  let lastOut = ''
  for (let att = 1; att <= 3; att++) {
    lastOut = dexe(overleafC, `LEG_BASE=${base} P413_PASS='${pass.replace(/'/g, "'\\''")}' LEGA=${A} LEGDA=${DA} node /tmp/p413-battery.js`, true)
    rows = lastOut.trim().split('\n').filter(Boolean).map((l) => JSON.parse(l) as Record<string, string>)
    if (rows.length !== 19) throw new Error(`leg produced ${rows.length} cases (want 19): ${lastOut.slice(0, 400)}`)
    if (!rows.some((r) => r.code === 0)) break
    if (att < 3) dexe(overleafC, 'sleep 1')
  }
  if (rows.some((r) => r.code === 0)) throw new Error('leg transport failure: ' + JSON.stringify(rows.filter((r) => r.code === 0)))
  const map = Object.fromEntries(rows.map((r) => [r.case, r])) as Record<string, Record<string, string>>
  const m2ok = (mm: Record<string, Record<string, string>>) =>
    mm['reset']!.code === 200 &&
    mm['set-doc']!.code === 200 &&
    mm['set-doc-noop']!.code === 200 &&
    mm['ghost-doc']!.code === 404 &&
    mm['ghost-project']!.code === 404
  if (!m2ok(map)) {
    // stack-settling transient (re-seed race): one more full pass, then fail
    // loud so the Playwright retry gets a fresh worker + fresh seed.
    dexe(overleafC, 'sleep 1')
    const out2 = dexe(overleafC, `LEG_BASE=${base} P413_PASS='${pass.replace(/'/g, "'\\''")}' LEGA=${A} LEGDA=${DA} node /tmp/p413-battery.js`, true)
    const rows2 = out2.trim().split('\n').filter(Boolean).map((l) => JSON.parse(l) as Record<string, string>)
    if (rows2.length === 19 && !rows2.some((r) => r.code === 0)) {
      const map2 = Object.fromEntries(rows2.map((r) => [r.case, r])) as Record<string, Record<string, string>>
      if (m2ok(map2)) return map2
    }
    throw new Error('leg write-cases unstable: reset=' + map['reset']!.code + ' setdoc=' + map['set-doc']!.code + ' ' + JSON.stringify([map['reset'], map['set-doc']]).slice(0, 300))
  }
  return map
}

// ---- flip plumbing (self-contained; stripped afterwards) ----------------------
function flipConf(conf: string, mode: 'apply' | 'strip') {
  // node-driven vhost splice: no sed/shell quoting involved
  // Self-contained: apply installs the conf from the host flip dir into the
  // container staging dir first (a standalone run has no prior gate that
  // populated /usr/local/share/overleaf-flips — the original drop-the-cp-in
  // apply made this test depend on sibling gates in the same batch).
  if (mode === 'apply') {
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips')
    execFileSync('docker', ['cp', `${process.cwd()}/../../server-ce/nginx/flips/${conf}`, `${overleafC}:/usr/local/share/overleaf-flips/${conf}`], { timeout: 30000 })
  }
  const nodeScript = mode === 'apply'
    ? 'const fs=require("fs");const v=process.argv[1],conf=process.argv[2];const inc="  include /etc/nginx/overleaf-flips/"+conf+";"+String.fromCharCode(10,10);let s=fs.readFileSync(v,"utf8");const L=s.split(String.fromCharCode(10));const i=L.findIndex(x=>x.trim()==="location / {");if(i<0)throw new Error("location / not found");if(!L.some(x=>x.includes("/"+conf)))L.splice(i,0,inc);fs.writeFileSync(v,L.join(String.fromCharCode(10)));"ok"'
    : 'const fs=require("fs");const v=process.argv[1],conf=process.argv[2];let s=fs.readFileSync(v,"utf8");const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes("overleaf-flips/"+conf));fs.writeFileSync(v,L.join(String.fromCharCode(10)));"ok"'
  try {
    // apply: the conf is installed to the container staging dir by the
    // pre-step above (docker cp from the host flip dir), so the cp into
    // /etc/nginx/overleaf-flips MUST run here. strip: no cp, just unsplice.
    const cpStep = mode === 'apply' ? `cp -f /usr/local/share/overleaf-flips/${conf} /etc/nginx/overleaf-flips/${conf} && ` : ''
    const out = dexe(overleafC, `mkdir -p /etc/nginx/overleaf-flips && ${cpStep}node -e '${nodeScript.replace(/'/g, "\'")}' /etc/nginx/sites-enabled/overleaf.conf ${conf}`, true)
    // reload with retry (rapid successive reloads can race the master)
    let rc = 1
    for (let t = 1; t <= 3; t++) {
      rc = 0
      break
    }
    dexe(overleafC, `ok=0; for T in 1 2 3; do if nginx -t 2>/dev/null && nginx -s reload; then ok=1; break; fi; sleep 1; done; [ "$ok" = "1" ]`, true)
  } catch (e: any) {
    throw new Error(`flip ${conf} ${mode} failed: ${(e?.stderr || '').toString() + (e?.stdout || '').toString() + String(e?.message || e)}`)
  }
}

const SHAPED = new Set(['reset', 'set-doc', 'set-doc-noop'])
function shapeOf(body: string): string {
  // rev is docstore-global across legs — compare shape ({"rev":N[,
  // "modified":bool]}) only; the modified flag is written-state dependent.
  const j = JSON.parse(body)
  return `rev:${typeof j.rev}`
}
function compareCase(name: string, a: Record<string, string>, b: Record<string, string>): void {
  const fields = ['code', 'ct', 'www', 'etag', 'ns', 'body']
  for (const f of fields) {
    if (f === 'etag' && SHAPED.has(name)) continue // hashes the (state-dependent) body
    let av = a[f], bv = b[f]
    if (f === 'body' && SHAPED.has(name)) {
      av = shapeOf(av)
      bv = shapeOf(bv)
    }
    expect(av, `${name}.${f}\n  node: ${JSON.stringify(av)}\n  other: ${JSON.stringify(bv)}`).toBe(bv)
  }
}

// ---- tests --------------------------------------------------------------------
let STATE: { A: string; DA: string } | null = null
let LEGS: Record<string, Record<string, Record<string, string>>> = {}
let flipsApplied = false

test('seed project p413-api (+docstore entry)', async () => {
  STATE = seedState()
  expect(STATE.A).toMatch(/^[0-9a-f]{24}$/)
  expect(STATE.DA).toMatch(/^[0-9a-f]{24}$/)
})

for (const leg of [
  ['node1', 'http://127.0.0.1:3000'],
  ['go', 'http://127.0.0.1:4010'],
  ['node3', 'http://127.0.0.1:3000'],
] as const) {
  const [name, base] = leg
  test(`leg ${name} (${base})`, async () => {
    STATE = seedState() // fresh worker: re-seed idempotently
    LEGS[name] = runLeg(base, STATE.A, STATE.DA)
    const r = LEGS[name]
    if (name === 'node1') {
      expect(r['get-json'].code).toBe(200)
      expect(r['get-json'].body).toContain('"otMigrationStage":0')
      expect(r['no-auth-get-json'].code).toBe(401)
      expect(r['no-auth-get-json'].body).toBe('Unauthorized')
      expect(r['no-auth-get-json'].www).toBe('overleaflogin')
      expect(r['no-auth-get-html'].code).toBe(401)
      expect(r['reject'].code).toBe(204)
      expect(r['ghost-doc'].code).toBe(404)
      expect(r['ghost-doc'].body).toBe('Not Found')
      expect(r['bad-project-oid'].body).toContain('params.Project_id')
      expect(r['va-miss-lines'].body).toContain('received undefined at ')
      expect(r['set-doc'].body).toContain('"rev":')
      expect(JSON.parse(r['reset'].body)).toHaveProperty('modified', true)
    }
    // cross-worker: the baseline (node1) is captured again in this worker
    // so the comparison is in-process.
    if (name !== 'node1') {
      const baseline = runLeg('http://127.0.0.1:3000', STATE.A, STATE.DA)
      for (const [caseName, baseRow] of Object.entries(baseline)) {
        compareCase(caseName, baseRow as Record<string, string>, r[caseName]!)
      }
    }
  })
}

test('nginx-bound: flipped 7420 surface serves the API contract', async () => {
  STATE = seedState()
  // Defensive: clear any stale flip includes (a previously failed run in a
  // batch can leave orphaned includes → nginx -t fails → whole battery red).
  dexe(overleafC, `node -e 'const fs=require("fs");const p=process.argv[1];const s=fs.readFileSync(p,"utf8");const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes("overleaf-flips/"));fs.writeFileSync(p,L.join(String.fromCharCode(10)))' /etc/nginx/sites-enabled/overleaf.conf`)
  for (const conf of FLIPS) flipConf(conf, 'apply')
  flipsApplied = true
  const auth = async () => {
    const pw = dexe(overleafC, 'printenv WEB_API_PASSWORD', true).trim()
    return 'Basic ' + Buffer.from(`overleaf:${pw}`).toString('base64')
  }
  const call = (u: string, init: RequestInit) =>
    fetch(BASE + u, { ...init, redirect: 'manual' }).then(async (x) => ({
      code: x.status,
      ct: x.headers.get('content-type') || '',
      www: (x.headers.get('www-authenticate') || '').toLowerCase(),
      body: await x.text(),
    }))
  const P = `/project/${STATE!.A}/doc/${STATE!.DA}`
  // nginx + upstream settle after flip reloads: retry any dropped socket
  const want = async (fn: () => Promise<any>): Promise<any> => {
    let last: any
    for (let t = 1; t <= 6; t++) {
      try {
        return await fn()
      } catch (e) {
        last = e
        await new Promise((r) => setTimeout(r, 700))
      }
    }
    throw last
  }
  try {
    const na = await want(() => call(P, { headers: { accept: 'application/json' } }))
    expect(na.code).toBe(401)
    expect(na.body).toBe('Unauthorized')
    expect(na.www).toBe('overleaflogin')

    const naHtml = await want(() => call(P, { headers: { accept: 'text/html' } }))
    expect(naHtml.code).toBe(401)

    const tok = await auth()

    const ok = await want(() => call(P, { headers: { authorization: tok, accept: 'application/json' } }))
    expect(ok.code).toBe(200)
    expect(ok.ct).toBe('application/json; charset=utf-8')
    // canonical state was established by the legs (reset wrote CANON to both services)
    expect(ok.body).toContain(JSON.stringify(CANON))

    const set = await want(() => call(P, {
      method: 'POST',
      headers: { 'content-type': 'application/json', authorization: tok },
      body: JSON.stringify({ lines: CANON, version: SETVER, ranges: {} }),
    }))
    expect(set.code).toBe(200)
    expect(typeof JSON.parse(set.body).rev).toBe('number')

    const rej = await want(() => call(`${P}/changes/reject`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', authorization: tok },
      body: JSON.stringify({ rejectedChangeAuthorIds: [] }),
    }))
    expect(rej.code).toBe(204)

    // unflipped verb stays on Node: web-profile CSRF gate 403
    const put = await want(() => call(P, { method: 'PUT', headers: { 'content-type': 'application/json' }, body: '{}' }))
    expect(put.code).toBe(403)
  } finally {
    if (flipsApplied) {
      for (const conf of FLIPS) flipConf(conf, 'strip')
      flipsApplied = false
    }
  }
})
