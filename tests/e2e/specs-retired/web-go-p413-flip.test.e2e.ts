// P4.13: private API document-trio WIRE contract — WEB profile, standalone.
//
// CONVERTED 2026-10-05 (owner): the 3-leg shadow comparison (Node web :4000
// vs Go shadow :4010 vs Node api :3000) is retired — in the P7 image the
// canonical stack IS Go, so :4000 (web) and :3000 (api) are both Go profiles
// and the shadow :4010 no longer exists. The wire is now pinned DIRECTLY on
// canonical Go (:4000 web profile; the api profile is pinned by web-go-uapi-doc)
// + 2-run byte-parity stability. Original pins transcribed from live canonical
// captures 2026-09-23 (flip-era Node-oracle-verified) — see header docs.
//
// Wire surface pinned (basic-auth document trio + adjacent routes):
//   reachable/no-auth:
//     GET + Accept json          -> 401 text/plain "Unauthorized" + WWW-Authenticate: OverleafLogin
//     GET + Accept html/none     -> 302 Location: /login ("Found. Redirecting to /login")
//     any POST (no csrf)         -> 403 text/plain "Forbidden" (csrf gate precedes auth validation)
//   wrong Authorization (ANY Accept) -> 401 (never content-negotiated)
//   valid-cred:
//     GET real doc   -> 200 application/json {lines,version,ranges,pathname,projectHistoryType}
//     GET ?plain=1   -> 200 text/plain (joined lines)
//     GET ghost doc  -> 404 rendered "Page Not Found - OlliTeX"
//     GET bad oid    -> 404 rendered "Page Not Found" (html, even for accept json)
//     POST setDocument / changes-reject -> 403 (csrf boundary fires before validation;
//       the 400-validation surface is session+csrf-only, out of the basic-auth wire gate)
//   unknown route (anonymous) -> 401 (OverleafLogin wire, not a 302 login bounce)
// csrf + nonce are random/per-session and normalized where rendered pages compare.
import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const BASE = 'http://127.0.0.1:4000'
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
    const uid = (db.users.findOne({email:"e2e-user@e2e.test"}) || {})._id || ObjectId("6ac10cd5f4600767b51ae7a2");
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

// ---- one capture pass against canonical Go (cred optional) ---------------------
const CAP = `
const B = process.env.LEG_BASE, CRED = process.env.LEG_CRED, A = process.env.LEGA, DA = process.env.LEGDA, G = process.env.LEGG;
const CANON = ${JSON.stringify(CANON)};
const body210 = JSON.stringify({ lines: CANON, version: 210, ranges: {} });
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
      out.push({ case: name, code: r.status, ct: r.headers.get('content-type') || '', loc: r.headers.get('location') || '', www: (r.headers.get('www-authenticate') || '').toLowerCase(), body: await r.text() })
    } catch (e) {
      out.push({ case: name, code: 0, ct: '', loc: '', www: '', body: 'ERR ' + String(e) })
    }
  }
  console.log(JSON.stringify(out))
})().catch(e => { console.error(e); process.exit(1) })
`

interface Row { case: string; code: number; ct: string; loc: string; www: string; body: string }
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
    .replace(/nonce-[A-Za-z0+/=]{8,40}/g, 'nonce-N')
    .replace(/nonce="[^"]*"/g, 'nonce="N"')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
    .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
}

let STATE: { A: string; DA: string } | null = null

test('seed project p413-webwire (+docstore entry)', async () => {
  STATE = seedState()
  expect(STATE.A).toMatch(/^[0-9a-f]{24}$/)
  expect(STATE.DA).toMatch(/^[0-9a-f]{24}$/)
})

test('wire contract: private-API document trio (canonical Go web)', async () => {
  test.setTimeout(240_000)
  if (!STATE) STATE = seedState()
  const anon = capture(BASE, false, STATE.A, STATE.DA)
  const cred = capture(BASE, true, STATE.A, STATE.DA)

  // ---- reachable / no-auth wire -------------------------------------------
  const gj = anon['no-auth-get-json']!
  expect(gj.code, 'no-auth-get-json status').toBe(401)
  expect(gj.ct, 'no-auth-get-json ct').toBe('text/plain; charset=utf-8')
  expect(gj.body, 'no-auth-get-json body').toBe('Unauthorized')
  expect(gj.www, 'no-auth-get-json www-authenticate').toBe('overleaflogin')

  const gh = anon['no-auth-get-html']!
  expect(gh.code, 'no-auth-get-html status').toBe(302)
  expect(gh.loc, 'no-auth-get-html location').toBe('/login')
  expect(gh.body).toContain('Redirecting to /login')

  const gp = anon['no-auth-get-plain']!
  expect(gp.code, 'no-auth-get-plain status').toBe(302)
  expect(gp.loc, 'no-auth-get-plain location').toBe('/login')

  // ---- wrong Authorization (ANY Accept -> 401, never content-negotiated) ----
  for (const name of ['wrong-json', 'wrong-html']) {
    const r = anon[name]!
    expect(r.code, `${name} status`).toBe(401)
    expect(r.body, `${name} body`).toBe('Unauthorized')
    expect(r.www, `${name} www-authenticate`).toBe('overleaflogin')
  }

  // ---- valid-cred surface ----------------------------------------------------
  const vj = cred['valid-get-json']!
  expect(vj.code, 'valid-get-json status').toBe(200)
  expect(vj.ct, 'valid-get-json ct').toBe('application/json; charset=utf-8')
  const vjBody = JSON.parse(vj.body)
  expect(vjBody.lines, 'valid-get-json lines').toEqual(CANON)
  expect(vjBody.pathname, 'valid-get-json pathname').toBe('/doc.tex')

  const vp = cred['valid-get-plain']!
  expect(vp.code, 'valid-get-plain status').toBe(200)
  expect(vp.ct, 'valid-get-plain ct').toBe('text/plain; charset=utf-8')
  expect(vp.body, 'valid-get-plain body').toBe(CANON.join('\n'))

  const gd = cred['ghost-doc']!
  expect(gd.code, 'ghost-doc status').toBe(404)
  expect(gd.ct, 'ghost-doc ct').toContain('text/html')
  expect(gd.body, 'ghost-doc title').toContain('Page Not Found - OlliTeX')
  expect(normPage(gd.body).includes('Not found'), 'ghost-doc functional content').toBe(true)

  const gpp = cred['ghost-project']!
  expect(gpp.code, 'ghost-project status').toBe(404)
  expect(gpp.body, 'ghost-project body').toContain('Page Not Found')

  // bad project oid: rendered 404 page, EVEN FOR accept: application/json (pinned quirk)
  const bp = cred['bad-project-oid']!
  expect(bp.code, 'bad-project-oid status').toBe(404)
  expect(bp.body, 'bad-project-oid body').toContain('Page Not Found')

  // ---- POSTs: csrf boundary fires before validation (403, not 400/401) -------
  for (const name of ['valid-post-doc', 'valid-post-reject', 'no-auth-post']) {
    const pool = name.startsWith('no-auth') ? anon : cred
    const r = pool[name]!
    expect(r.code, `${name} status (csrf gate)`).toBe(403)
    expect(r.body, `${name} body`).toBe('Forbidden')
  }

  // ---- unknown route: anonymous wire = 401 OverleafLogin (not a login bounce) -
  const ur = anon['unknown-route']!
  expect(ur.code, 'unknown-route status').toBe(401)
  expect(ur.www, 'unknown-route www-authenticate').toBe('overleaflogin')
})

test('stability: 2-run byte parity (canonical Go web)', async () => {
  test.setTimeout(240_000)
  if (!STATE) STATE = seedState()
  const anonA = capture(BASE, false, STATE.A, STATE.DA)
  const anonB = capture(BASE, false, STATE.A, STATE.DA)
  const credA = capture(BASE, true, STATE.A, STATE.DA)
  const credB = capture(BASE, true, STATE.A, STATE.DA)
  const bad: string[] = []
  const cmp = (tag: string, a: Record<string, Row>, b: Record<string, Row>) => {
    const keys = new Set([...Object.keys(a), ...Object.keys(b)])
    for (const k of [...keys].sort()) {
      const A = a[k], B = b[k]
      if (!A || !B) { bad.push(`${tag}:${k} missing side A=${!!A} B=${!!B}`); continue }
      if (A.code !== B.code) bad.push(`${tag}:${k} code A=${A.code} B=${B.code}`)
      if (A.ct !== B.ct) bad.push(`${tag}:${k} ct A=${A.ct} B=${B.ct}`)
      if (A.loc !== B.loc) bad.push(`${tag}:${k} loc A=${A.loc} B=${B.loc}`)
      if (A.www !== B.www) bad.push(`${tag}:${k} www A=${A.www} B=${B.www}`)
      const na = normPage(A.body), nb = normPage(B.body)
      if (na !== nb) {
        let i = 0
        while (i < Math.min(na.length, nb.length) && na[i] === nb[i]) i++
        bad.push(`${tag}:${k} body len A=${na.length} B=${nb.length} @${i} A[...${na.slice(Math.max(0, i - 60), i + 60)}...] B[...${nb.slice(Math.max(0, i - 60), i + 60)}...]`)
      }
    }
  }
  cmp('anon', anonA, anonB)
  cmp('cred', credA, credB)
  expect(bad.join('\n'), bad.join('\n') || 'wire stability GREEN').toBe('')
})

test('normPage is idempotent (csrf normalization)', async () => {
  const s = '<meta name="ol-csrfToken" content="abcd123"><input name="_csrf" type="hidden" value="abcd123">'
  expect(normPage(normPage(s))).toBe(normPage(s))
})
