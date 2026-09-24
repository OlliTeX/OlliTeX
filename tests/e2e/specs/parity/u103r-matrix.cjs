/**
 * U10.3r parity matrix — the private-API (basic-auth) document trio.
 *
 *   GET  /project/:Project_id/doc/:doc_id           (requirePrivateApiAuth)
 *   POST /project/:Project_id/doc/:doc_id           (setDocument)
 *   POST /project/:Project_id/doc/:doc_id/changes/reject
 *
 * The `/user/:userId/tag` GET is deliberately NOT pinned here: it is a
 * SESSION-dependent route whose live oracle is the U1 gate (a valid
 * logged-in session renders the 404 HTML page). This gate is scoped to the
 * profile-neutral basic-auth doc trio, whose wire is identical between the
 * Node web (:4000) and the Go shadow.
 *
 * Node oracle surface (pinned live 2026-09-23, web profile :4000, full
 * header capture). The gate branches on Authorization PRESENCE (pin: a
 * no-Authorization request behaves identically with or without a logged-in
 * session cookie — json→401, html→302):
 *   - no Authorization + Accept→json      → 401 "Unauthorized"
 *                                           + WWW-Authenticate: OverleafLogin
 *                                           + FULL helmet + fresh sid,
 *                                           NO X-Powered-By
 *   - no Authorization + Accept→html/none → 302 Location /login + helmet
 *                                           + Vary: Accept + negotiated body
 *   - wrong Authorization (ANY Accept)    → 401 (requireBasic failure never
 *                                           content-negotiates)
 *   - any POST (no csrf token)            → 403 text "Forbidden" + XPB + CSP
 *                                           + fresh sid, NO helmet set
 *                                           (csrf fires before helmet)
 *
 * IMPORTANT — the valid-cred cases are NOT compared here: the sandbox
 * network guard drops ANY request (to Node OR Go) whose Authorization
 * header carries the WEB_API password (400 bare), so the app-level valid
 * path never runs in e2e. The reachable auth-fail/POST-block surface above
 * is byte/structurally identical between Node :4000 and Go :4010. Go's
 * valid-cred accept branch is proven in-process by
 * core.TestAPIBasicGateMatrix (no network).
 *
 * Output: one line per case  label|status|hdr§...|body
 */
const LEG = Number(process.argv[2] || 1)
const BASE = LEG === 2 ? 'http://127.0.0.1:4010' : 'http://127.0.0.1:4000'

const WRONG = 'Basic ' + Buffer.from('wrong:wrong').toString('base64')

const CASES = []
function add(label, run) { CASES.push({ label, run }) }

async function main() {
  const PJ = process.env.U103R_PJ || 'aaaa000000000000000000a1'
  const DOC = process.env.U103R_DOC || 'aaaa000000000000000000a2'
  const DOCGET = `/project/${PJ}/doc/${DOC}`

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
    out.push(`${label}|${r.status}|${hdr}|${r._body}`)
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

  // doc GET gate (no-auth → 401 json / 302 html|none; wrong → 401 any Accept)
  add('doc-unauth-json', async () => get(DOCGET, J))
  add('doc-unauth-html', async () => get(DOCGET, H))
  add('doc-unauth-plain', async () => get(DOCGET, {}))
  add('doc-wrong-json', async () => get(DOCGET, WJ))
  add('doc-wrong-html', async () => get(DOCGET, WH))

  // doc POST: no-csrf → 403 (csrf fires before basic auth), both routes
  add('post-doc-nocsrf', async () => post(DOCGET, {}))
  add('post-rej-nocsrf', async () => post(DOCGET + '/changes/reject', {}))

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

main().catch((e) => { console.error(e); process.exit(1) })
