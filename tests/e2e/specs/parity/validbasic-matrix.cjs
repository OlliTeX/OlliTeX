/* WEB-profile VALID-BASIC parity matrix. Oracle = Node web :4000 (legs 1/3),
 * shadow = Go web :4010 (leg 2).
 *
 * Node requireGlobalLogin (AuthenticationController). When an Authorization
 * header is PRESENT the basic-credential decision is authoritative (valid →
 * authenticated & dispatched to the route/404-view, invalid → 401); only when
 * absent does the session decide (none → 302/401). This gate pins that wire:
 *
 *   valid basic + non-web route (members, /foo, personal_info, tag) → 404
 *       (rendered page — FUNCTIONAL pin: status + content-type + nosniff)
 *   valid basic + /project/:id → 403 restricted:
 *       Accept: application/json → 24B {"message":"restricted"}  (BYTE-EXACT)
 *       else                       → rendered "Restricted" page (FUNCTIONAL)
 *   wrong basic (any Accept) → 401 "Unauthorized" + WWW-Authenticate  (BYTE-EXACT)
 *   no-auth non-json         → 302 /login                              (BYTE-EXACT)
 *   no-auth json             → 401 "Unauthorized"                      (BYTE-EXACT)
 *
 * Deterministic wires are byte-exact compared (header set + body, ETag
 * weak-normalized, set-cookie name-only). Rendered 404/403 pages are compared
 * functionally (status + content-type + nosniff only) — the page body/nonce-CSP/
 * etag differ across Go's static template vs Node's rendered tree (the same
 * "known static-template" limit the u103r/p413 gates accept for 404 pages).
 */
'use strict'
const LEG = Number(process.argv[2] || 1)
// leg 1/3 → Node web :4000 (oracle); leg 2 → Go web :4010 (shadow).
const BASE = LEG === 2 ? 'http://127.0.0.1:4010' : 'http://127.0.0.1:4000'
const PJ = '6ab3b8d5156c46efd24be99c' // P4.13 fixture (stable project id)
const OWNER = '6aa4b8b573ef0e5094f4cbc0'
const WRONG = 'Basic ' + Buffer.from('wrong:wrong').toString('base64')
const VALID = (() => {
  try {
    const s = require('/etc/overleaf/settings.js')
    const u = Object.keys(s.httpAuthUsers || {})[0]
    if (u) return 'Basic ' + Buffer.from(u + ':' + s.httpAuthUsers[u]).toString('base64')
  } catch { /* not available */ }
  return null
})()
if (!VALID) {
  console.error('ERR|no-valid-creds: /etc/overleaf/settings.js httpAuthUsers unreadable')
  process.exit(1)
}

const OUT = []
let COUNT = 0

async function get(path, headers) {
  const r = await fetch(BASE + path, { headers: headers || {}, redirect: 'manual' })
  r._body = await r.text()
  return r
}

// BYTE-EXACT: full header set + body (ETag weak-normalized, set-cookie name-only).
async function recFull(label, r) {
  const etagN = (v) => (v || '').replace(/W\/"([0-9a-f]+)-[^"]*"/g, 'W/"$1-H"')
  const sc = r.headers.getSetCookie ? r.headers.getSetCookie() : []
  const scN = sc.map((c) => c.split(';').map((p) => { const t = p.trim(); const i = t.indexOf('='); return i === -1 ? t : t.slice(0, i + 1) + 'X' }).join(';')).join(' | ')
  const helmet = [
    r.headers.get('cross-origin-opener-policy') || '',
    r.headers.get('cross-origin-resource-policy') || '',
    r.headers.get('referrer-policy') || '',
    r.headers.get('x-content-type-options') || '',
    r.headers.get('x-download-options') || '',
    r.headers.get('x-frame-options') || '',
    r.headers.get('x-permitted-cross-domain-policies') || '',
    r.headers.get('x-xss-protection') || '',
    r.headers.get('content-security-policy') || '',
  ].join('§')
  const hdr = [
    r.headers.get('content-type') || '',
    r.headers.get('x-powered-by') || '',
    etagN(r.headers.get('etag')),
    scN,
    r.headers.get('location') || '',
    r.headers.get('www-authenticate') || '',
    helmet,
  ].join('§')
  const b = String(r._body || '').replace(/\r?\n/g, '\\n').replace(/\t/g, '\\t')
  OUT.push(`${label}|${r.status}|${hdr}|${b}`)
  COUNT++
}

// FUNCTIONAL: rendered page — status + content-type + nosniff only.
async function recPage(label, r) {
  OUT.push(`${label}|${r.status}|ct=${r.headers.get('content-type') || ''}|nosniff=${r.headers.get('x-content-type-options') || ''}|«page»`)
  COUNT++
}

const J = { accept: 'application/json' }
const NJ = { accept: '*/*' }
const V = (h) => ({ ...h, authorization: VALID })
const W = (h) => ({ ...h, authorization: WRONG })

async function main() {
  // wrong basic → 401 (any Accept, byte-exact) — two accepts pinned.
  await recFull('vb-wrong-nj', await get('/members', W(NJ)))
  await recFull('vb-wrong-j', await get('/members', W(J)))

  // no-auth → 302 /login (non-json) / 401 (json) — byte-exact.
  await recFull('vb-noauth-302', await get('/members', NJ))
  await recFull('vb-noauth-401', await get('/members', J))

  // valid basic + non-web routes → 404 rendered page (functional).
  await recPage('vb-404-members', await get('/members', V(NJ)))
  await recPage('vb-404-unknown', await get('/zzz-validbasic-nope', V(NJ)))
  await recPage('vb-404-pi', await get(`/user/${OWNER}/personal_info`, V(NJ)))
  await recPage('vb-404-tag', await get(`/user/${OWNER}/tag`, V(NJ)))

  // valid basic + /project/:id → 403 restricted (JSON byte-exact, page functional).
  await recFull('vb-pj-403-json', await get(`/project/${PJ}`, V(J)))
  await recPage('vb-pj-403-page', await get(`/project/${PJ}`, V(NJ)))

  OUT.push(`validbasic summary: cases=${COUNT} (leg=${LEG})`)
  console.log(OUT.join('\n'))
}

main().catch((e) => { console.error((e && e.message) || e); process.exit(1) })
