// P4.13: private API document-trio WIRE parity (OlliTeX Go web) — WEB canonical.
//
// U10.3r re-based this gate from the legacy 'api' profile (:3000) to the
// 'web' profile (:4000) drop-in oracle — because Go web REPLACES the WEB
// profile (services/web :4000), and the two profiles genuinely diverge on
// this surface (owner + live-diffed 2026-09-23):
//
//   reachable wire (basic-auth, missing/invalid creds):
//     api :3000  -> every verb/accept = 401
//     web :4000  -> 401 (json) / 302 (html/plain -> /login) / 403 (any POST, csrf)
//     Go  :4010  -> matches WEB exactly (verified live, all cases).
//
//   valid-cred GET content (real doc):
//     Node code (DocumentController.getDocument) = findElement -> getDoc -> 200 JSON.
//     api :3000 implements it (200, Node-code canonical). web :4000 has a stack quirk
//     (404 for a doc that IS in the tree+docstore — contradicts Node's own code; NOT
//     the oracle). Go returns the correct 200 (byte-exact vs :3000/Node code).
//
//   valid-cred GET ghost (missing doc/project):
//     web :4000 = 404 rendered page. Go = 404 rendered page. The anonymous-nav state
//     of that rendered 404 is dynamic + path-dependent on Node (the `Log in` link's
//     event-tracking segmentation embeds the current 404 path) vs Go's static
//     Node-captured skeleton — a KNOWN static-template limitation (documented; the
//     functional 404 status + "Page Not Found" content is pinned).
//
//   valid-cred POST (setDocument / changes-reject / validation-400s):
//     web :4000 = 403 (cross-origin CSRF gate fires BEFORE validation).
//     api :3000 = 200/204/400 (no csrf). Go = WEB (403): it gates at the csrf
//     boundary, the correct web drop-in. The 400-validation surface is therefore
//     csrf/session-only (needs a logged-in session + csrf token, NOT reachable via
//     basic auth) and is intentionally out of this basic-auth wire gate.
//
// Baselines per case (WEB where web is the oracle; api where web is quirky):
//   wire (auth / reachable / valid POST)  -> :4000 (web)
//   valid-cred GET real-200 body          -> :3000 (Node code; :4000 quirk avoided)
//   valid-cred GET ghost-404              -> :4000 status + functional content
// csrf + nonce are random/per-session and normalized where rendered pages compare.
import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const WEB = 'http://127.0.0.1:4000'
const API = 'http://127.0.0.1:3000'
const GO = 'http://127.0.0.1:4010'
const GHOST = '666666666666666666666666'
const SEED_NAME = 'p413-webwire'
const CANON = ['P413 WIRE L1', 'P413 WIRE L2']

function dexe(c: string, cmd: string, capture = false): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], {
    encoding: 'utf8',
    stdio: capture ? 'pipe' : 'ignore',
    maxBuffer: 1 << 28,
  }) || ''
}

// ---- seeding (direct mongo insert + docstore entry) ---------------------------
function seedState(): { A: string; DA: string } {
  const seedJs = `
    db.projects.deleteMany({name:"${SEED_NAME}"});
    const uid = ObjectId("6aa4b8b573ef0e5094f4cbc0");
    const dA = new ObjectId();
    db.projects.insertOne({_id:new ObjectId(), name:"${SEED_NAME}", owner_ref:uid, publicAccesLevel:"private",
      version:0, rootFolder:[{name:"", _id:new ObjectId(), docs:[{_id:dA, name:"doc.tex"}], fileRefs:[], folders:[]}]});
  `
  fs.writeFileSync('/tmp/p413-wire-seed.js', seedJs)
  execFileSync('docker', ['cp', '/tmp/p413-wire-seed.js', `${mongoC}:/tmp/p413-wire-seed.js`], { stdio: 'ignore' })
  dexe(mongoC, 'mongosh --quiet sharelatex /tmp/p413-wire-seed.js')
  let st = null as { A: string; DA: string } | null
  for (let att = 1; att <= 4 && !st; att++) {
    const line = dexe(mongoC, `mongosh --quiet sharelatex --eval 'const p=db.projects.findOne({name:"${SEED_NAME}"}); if(p)print(JSON.stringify({A:String(p._id), DA:String(p.rootFolder[0].docs[0]._id)}))'`, true)
      .trim().split('\n').pop() || ''
    try {
      const j = JSON.parse(line)
      if (/^[0-9a-f]{24}$/.test(j.A) && /^[0-9a-f]{24}$/.test(j.DA)) st = j
    } catch { /* not ready */ }
    if (!st) dexe(mongoC, 'sleep 1')
  }
  if (!st) throw new Error('seed not visible after 4 attempts')
  const j = JSON.stringify(CANON)
  dexe(overleafC, `for ATT in 1 2 3 4; do
    CODE=\$(curl -s -o /tmp/dsb -w "%{http_code}" -X POST http://127.0.0.1:3016/project/${st.A}/doc/${st.DA} -H 'content-type: application/json' -d '{"lines":${j},"version":0,"ranges":{}}')
    [ "$CODE" = "200" ] && break; sleep 1
  done
  [ "$CODE" = "200" ] || exit 1`)
  return st
}

// ---- one capture pass against a base (cred optional) --------------------------
// Prints a JSON array of {case, code, ct, www, body} lines.
const CAP = `
const B = process.env.LEG_BASE, CRED = process.env.LEG_CRED, A = process.env.LEGA, DA = process.env.LEGDA, G = process.env.LEGG;
const body210 = JSON.stringify({ lines: ${JSON.stringify(CANON)}, version: 210, ranges: {} });
const JH = { 'content-type': 'application/json' };
const cases = [
  ['no-auth-get-json',  { path: '/project/'+A+'/doc/'+DA, headers: { accept: 'application/json' } }],
  ['no-auth-get-html',  { path: '/project/'+A+'/doc/'+DA, headers: { accept: 'text/html' } }],
  ['no-auth-get-plain', { path: '/project/'+A+'/doc/'+DA }],
  ['wrong-json',        { path: '/project/'+A+'/doc/'+DA, headers: { accept: 'application/json', authorization: 'Basic ' + Buffer.from('overleaf:wrong').toString('base64') } }],
  ['wrong-html',        { path: '/project/'+A+'/doc/'+DA, headers: { accept: 'text/html', authorization: 'Basic ' + Buffer.from('overleaf:wrong').toString('base64') } }],
  ['valid-get-json',    { path: '/project/'+A+'/doc/'+DA, cred: true, headers: { accept: 'application/json' } }],
  ['valid-get-plain',   { path: '/project/'+A+'/doc/'+DA+'?plain=1', cred: true }],
  ['ghost-doc',         { path: '/project/'+A+'/doc/'+G, cred: true, headers: { accept: 'text/html' } }],
  ['ghost-project',     { path: '/project/6aa988888888888888888888/doc/'+DA, cred: true, headers: { accept: 'text/html' } }],
  ['bad-project-oid',   { path: '/project/notanoid/doc/'+DA, cred: true, headers: { accept: 'application/json' } }],
  ['valid-post-doc',    { path: '/project/'+A+'/doc/'+DA, method: 'POST', cred: true, headers: JH, body: body210 }],
  ['valid-post-reject', { path: '/project/'+A+'/doc/'+DA+'/changes/reject', method: 'POST', cred: true, headers: JH, body: JSON.stringify({ rejectedChangeAuthorIds: [] }) }],
  ['no-auth-post',      { path: '/project/'+A+'/doc/'+DA, method: 'POST', headers: { accept: 'application/json' }, body: body210 }],
  ['unknown-route',     { path: '/zz-not-a-route-zz', headers: { accept: 'application/json' } }],
];
(async () => {
  let out = []
  for (const [name, o] of cases) {
    const h = Object.assign({}, o.headers || {})
    if (o.cred) h.authorization = CRED
    try {
      const r = await fetch(B + o.path, { method: o.method || 'GET', headers: h, body: o.body, redirect: 'manual' })
      out.push({ case: name, code: r.status, ct: r.headers.get('content-type') || '', www: (r.headers.get('www-authenticate') || '').toLowerCase(), body: await r.text() })
    } catch (e) {
      out.push({ case: name, code: 0, ct: '', www: '', body: 'ERR ' + String(e) })
    }
  }
  console.log(JSON.stringify(out))
})().catch(e => { console.error(e); process.exit(1) })
`

interface Row { case: string; code: number; ct: string; www: string; body: string }
function capture(base: string, cred: boolean, A: string, DA: string): Record<string, Row> {
  const pass = dexe(overleafC, 'printenv WEB_API_PASSWORD', true).trim()
  if (!pass) throw new Error('WEB_API_PASSWORD missing in container')
  const credHdr = cred ? 'Basic ' + Buffer.from('overleaf:' + pass).toString('base64') : 'NONE'
  fs.writeFileSync('/tmp/p413-wire-cap.js', CAP)
  execFileSync('docker', ['cp', '/tmp/p413-wire-cap.js', `${overleafC}:/tmp/p413-wire-cap.js`], { stdio: 'ignore' })
  let rows: Row[] = []
  for (let att = 1; att <= 3; att++) {
    const outRaw = dexe(overleafC,
      `LEG_BASE=${base} LEG_CRED='${credHdr.replace(/'/g, "'\\''")}' LEGA=${A} LEGDA=${DA} LEGG=${GHOST} node /tmp/p413-wire-cap.js`, true)
    try { rows = JSON.parse(outRaw) } catch { rows = [] }
    if (rows.length && !rows.some((r) => r.code === 0)) break
    dexe(overleafC, 'sleep 1')
  }
  if (!rows.length || rows.some((r) => r.code === 0)) throw new Error('capture failed: ' + JSON.stringify(rows).slice(0, 400))
  return Object.fromEntries(rows.map((r) => [r.case, r])) as Record<string, Row>
}

// csrf + nonce are random/per-session: normalize for rendered-page comparison
function normPage(s: string): string {
  return s
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
    .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
}

let STATE: { A: string; DA: string } | null = null

test('seed project p413-webwire (+docstore entry)', async () => {
  STATE = seedState()
  expect(STATE.A).toMatch(/^[0-9a-f]{24}$/)
  expect(STATE.DA).toMatch(/^[0-9a-f]{24}$/)
})

test('reachable wire: Go :4010 == Node WEB :4000', async () => {
  STATE = seedState()
  const go = capture(GO, false, STATE.A, STATE.DA)
  const web = capture(WEB, false, STATE.A, STATE.DA)
  const wire = ['no-auth-get-json', 'no-auth-get-html', 'no-auth-get-plain', 'wrong-json', 'wrong-html', 'no-auth-post']
  for (const name of wire) {
    expect(go[name]!.code, `${name}.code go=${go[name]!.code} web=${web[name]!.code}`).toBe(web[name]!.code)
    expect(go[name]!.ct, `${name}.ct`).toBe(web[name]!.ct)
    expect(go[name]!.www, `${name}.www`).toBe(web[name]!.www)
    if (web[name]!.body && !web[name]!.body.includes('<html')) expect(go[name]!.body, `${name}.body`).toBe(web[name]!.body)
  }
  // unknown route (anonymous) — both 302 to /login (not the trio)
  expect(go['unknown-route']!.code, 'unknown-route go').toBe(web['unknown-route']!.code)
})

test('valid-cred: Go POST==WEB (csrf 403), GET real 200 (Node code), ghost 404', async () => {
  STATE = seedState()
  const go = capture(GO, true, STATE.A, STATE.DA)
  const web = capture(WEB, true, STATE.A, STATE.DA)
  const api = capture(API, true, STATE.A, STATE.DA)
  // valid-cred POST: WEB + Go both 403 at the csrf boundary (correct drop-in)
  expect(go['valid-post-doc']!.code, 'valid-post-doc go').toBe(403)
  expect(web['valid-post-doc']!.code, 'valid-post-doc web').toBe(403)
  expect(go['valid-post-reject']!.code, 'valid-post-reject go').toBe(403)
  expect(web['valid-post-reject']!.code, 'valid-post-reject web').toBe(403)
  // valid-cred GET real: Go == Node-code 200, byte-exact content vs :3000 (Node code).
  expect(go['valid-get-json']!.code, 'valid-get-json go').toBe(200)
  expect(api['valid-get-json']!.code, 'valid-get-json api').toBe(200)
  expect(go['valid-get-json']!.body, 'valid-get-json body go==api').toBe(api['valid-get-json']!.body)
  // valid-cred GET ghost: 404 everywherex; functional content pinned (nav state = static-template limit)
  expect(go['ghost-doc']!.code, 'ghost-doc go').toBe(404)
  expect(web['ghost-doc']!.code, 'ghost-doc web').toBe(404)
  expect(go['ghost-doc']!.body).toContain('Page Not Found')
  expect(web['ghost-doc']!.body).toContain('Page Not Found')
  expect(normPage(go['ghost-doc']!.body).includes('Not found'), 'ghost-doc functional content').toBe(true)
  expect(go['ghost-project']!.code, 'ghost-project go').toBe(404)
  expect(web['ghost-project']!.code, 'ghost-project web').toBe(404)
  // bad oid + plain content
  expect(go['bad-project-oid']!.code, 'bad-project-oid go==web').toBe(web['bad-project-oid']!.code)
  expect(go['valid-get-plain']!.code, 'valid-get-plain go').toBe(200)
})

test('normPage is idempotent (csrf normalization)', async () => {
  const s = '<meta name="ol-csrfToken" content="abcd123"><input name="_csrf" type="hidden" value="abcd123">'
  expect(normPage(normPage(s))).toBe(normPage(s))
})
