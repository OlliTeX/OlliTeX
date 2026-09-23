/**
 * U-API doc-trio parity matrix — the private-API (basic-auth) document trio,
 * API profile (ENABLED_SERVICES=api).
 *
 * This is the API-profile oracle (Node api :3000), distinct from the WEB
 * profile oracle (Node web :4000, pinned by u103r-matrix.cjs). The two wires
 * differ (WEB: unauth json→401 / html→302, POST→403, ghost→404 HTML page;
 * API: unauth→401 for ANY Accept, POST→401, ghost→404 plain "Not Found",
 * X-Powered-By present, no session cookie, no helmet baseline).
 *
 * Node api :3000 surface (pinned live 2026-09-23, full header capture):
 *   - unauth (json/html/plain, GET or POST) + wrong basic → 401 "Unauthorized"
 *     + WWW-Authenticate: OverleafLogin + X-Powered-By + fixed CSP,
 *     NO helmet baseline, NO session cookie.
 *   - right basic + GET ghost/missing → 404 "Not Found" (text/plain, weak ETag,
 *     X-Powered-By, fixed CSP, no helmet/cookie).
 *   - right basic + GET real doc → 200 application/json (Node key order,
 *     X-Powered-By, fixed CSP, no helmet/cookie).
 *
 * The valid-cred POST (setDocument) status is STATE-dependent in this stack
 * (Node :3000 returns 500 — an env/docstore condition, not the gate) and is
 * deliberately NOT in this wire gate; the unauth/wrong 401 surface above is
 * the profile-wire contract.
 *
 * Output: one line per case  label|status|hdr§...|body   (ETag weak-normalized,
 * set-cookie name-only, helmet-bundle joined so any presence is seen).
 */
const LEG = Number(process.argv[2] || 1)
// leg 1 and 3 → Node api :3000 (the oracle); leg 2 → Go api :4011 (the shadow).
const BASE = LEG === 2 ? 'http://127.0.0.1:4011' : 'http://127.0.0.1:3000'

const WRONG = 'Basic ' + Buffer.from('wrong:wrong').toString('base64')
// Valid service credentials — same source the runit services + Node read.
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

const PJ = process.env.UAPI_PJ
const DOC = process.env.UAPI_DOC
const GHOST = process.env.UAPI_GHOST || '666666666666666666666666'
if (!PJ || !DOC) {
  console.error('ERR|missing-fixture: UAPI_PJ / UAPI_DOC not set')
  process.exit(1)
}
const DOCGET = `/project/${PJ}/doc/${DOC}`
const GHOSTGET = `/project/${PJ}/doc/${GHOST}`
const REJ = DOCGET + '/changes/reject'

const CASES = []
function add(label, run) { CASES.push({ label, run }) }

async function main() {
  const out = []
  const etagN = (v) => (v || '').replace(/W\/"([0-9a-f]+)-[^"]*"/g, 'W/"$1-H"')
  const rec = (label, r) => {
    const sc = r.headers.getSetCookie ? r.headers.getSetCookie() : []
    const scN = sc
      .map((c) => c.split(';').map((p) => { const t = p.trim(); const i = t.indexOf('='); return i === -1 ? t : t.slice(0, i + 1) + 'X' }).join(';'))
      .join(' | ')
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
      r.headers.get('vary') || '',
      r.headers.get('www-authenticate') || '',
      helmet,
    ].join('§')
    // One line per case: escape newlines/tabs in the body so every case is
    // exactly one output line (robust to multi-line bodies like the 404 page,
    // and the "cases=N" summary counts cases, not lines).
    const body1 = String(r._body || '').replace(/\r?\n/g, '\\n').replace(/\t/g, '\\t')
    out.push(`${label}|${r.status}|${hdr}|${body1}`)
  }
  const get = async (path, headers) => {
    const r = await fetch(BASE + path, { headers: headers || {}, redirect: 'manual' })
    r._body = await r.text()
    return r
  }
  const post = async (path, body, headers) => {
    const h = { ...(headers || {}), 'content-type': 'application/json' }
    const r = await fetch(BASE + path, { method: 'POST', headers: h, body: typeof body === 'string' ? body : JSON.stringify(body), redirect: 'manual' })
    r._body = await r.text()
    return r
  }

  const J = { accept: 'application/json' }
  const H = { accept: 'text/html' }
  const WJ = { accept: 'application/json', authorization: WRONG }
  const WH = { accept: 'text/html', authorization: WRONG }
  const VJ = { accept: 'application/json', authorization: VALID }

  // unauth surface (any Accept → 401, GET + POST)
  add('doc-unauth-json', async () => get(DOCGET, J))
  add('doc-unauth-html', async () => get(DOCGET, H))
  add('doc-unauth-plain', async () => get(DOCGET, {}))
  add('post-doc-unauth', async () => post(DOCGET, {}))
  add('post-rej-unauth', async () => post(REJ, {}))

  // wrong basic (any Accept → 401, never content-negotiates)
  add('doc-wrong-json', async () => get(DOCGET, WJ))
  add('doc-wrong-html', async () => get(DOCGET, WH))
  add('post-doc-wrong', async () => post(DOCGET, {}, { authorization: WRONG }))

  // valid cred: ghost → 404 plain, real → 200 JSON (both API-profile wire)
  add('doc-ghost-valid', async () => get(GHOSTGET, VJ))
  add('doc-valid-real', async () => get(DOCGET, VJ))

  // API-profile web-route exclusion (Node :3000 does NOT mount webRouter —
  // pinned live 2026-09-23): web-only routes 404 with the Express
  // finalhandler wire (Cannot <METHOD> <path>, content-security-policy:
  // "default-src 'none'", x-content-type-options: nosniff, x-powered-by:
  // Express), NOT the web profile's 301/302 login bounce. These pin the
  // route/profile-selection fix (previously Go api 301/302'd them).
  const P = '/project/' + PJ
  add('webroot-slash', async () => get('/', J))
  add('webroot-project', async () => get(P, J))
  add('webroot-members', async () => get(P + '/members', J))
  add('webroot-entities', async () => get('/entities', J))
  add('webroot-unknown', async () => get('/foo-404', J))

  for (const c of CASES) {
    try {
      const r = await c.run()
      rec(c.label, r)
    } catch (e) {
      out.push(`${c.label}|ERR|${(e && e.message) || e}||`)
    }
  }
  console.log(out.join('\n'))
}

main().catch((e) => { console.error((e && e.message) || e); process.exit(1) })
