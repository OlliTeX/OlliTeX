/* U2 in-container editor-route matrix — runs inside the overleaf container
 * against base (http://127.0.0.1:4000 Node or :4010 Go). Emits:
 *   BATTERY-JSON:[{tag,status,ct,loc,body,etag}, ...]
 *
 * Matrix (Node oracle, captured 2026-09-22): Express routing is
 * case-INsensitive, so /editor|/project in ANY case + hex id in EITHER
 * case all render the editor (200); a non-empty invalid id is 404 JSON
 * (exact body); the empty-id forms split: /editor/ → 404 HTML page,
 * /Project/ (any case) → 301 hub#/projects.all; anonymous → 302 /login
 * (auth first).
 *
 * args: <base> <pid>
 */
const BASE = process.argv[2]
const PID = process.argv[3]
const UPP = PID.toUpperCase()

async function login() {
  const r0 = await fetch(BASE + '/login', { headers: { 'user-agent': 'u2-gate' } })
  const html = await r0.text()
  if (r0.status !== 200) throw new Error('GET /login -> ' + r0.status)
  const csrf0 = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1 = await fetch(BASE + '/login', {
    method: 'POST',
    headers: {
      'user-agent': 'u2-gate',
      'content-type': 'application/json',
      accept: 'application/json',
      'x-csrf-token': csrf0,
      cookie: ck0,
    },
    body: JSON.stringify({ email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }),
  })
  const b1 = await r1.text()
  if (r1.status !== 200 && r1.status !== 302) {
    throw new Error('POST /login -> ' + r1.status + ' ' + b1.slice(0, 160))
  }
  const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
  const rl = await fetch(BASE + '/login', { headers: { 'user-agent': 'u2-gate', cookie: ck } })
  const h1 = await rl.text()
  const csrf = (h1.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  if (!csrf) throw new Error('no csrf post-login')
  return { ck, csrf }
}

async function main() {
  const sess = await login()
  const out = []
  async function req(method, path, opts = {}) {
    const h = { 'user-agent': 'u2-gate' }
    if (!opts.anon) h.cookie = sess.ck
    h.accept = opts.accept || 'text/html,application/xhtml+xml'
    if (!opts.anon && method !== 'GET') h['x-csrf-token'] = sess.csrf
    const r = await fetch(BASE + path, { method, headers: h, redirect: 'manual' })
    return {
      tag: opts.tag || method + ' ' + path,
      status: r.status,
      ct: (r.headers.get('content-type') || '').toLowerCase(),
      loc: r.headers.get('location') || '',
      body: await r.text(),
      etag: r.headers.get('etag') || '',
    }
  }

  const cases = [
    // 200 editor (any prefix case × id case, main + detach)
    ['V1 /Project lower', `/Project/${PID}`],
    ['V2 /project lower', `/project/${PID}`],
    ['V3 /Project UPP id', `/Project/${UPP}`],
    ['V4 /project UPP id', `/project/${UPP}`],
    ['V5 /editor lower', `/editor/${PID}`],
    ['V6 /Editor upper', `/Editor/${PID}`],
    ['V7 detach lower', `/project/${PID}/detached`],
    ['V8 detach editor', `/editor/${PID}/detacher`],
    // 404 JSON (invalid id, any case)
    ['B1 /Project/xyz', '/Project/xyz'],
    ['B2 /editor/abc', '/editor/abc'],
    ['B3 /Project/12', '/Project/12'],
    ['B4 /project/xyz/detached', `/project/xyz/detached`],
    // empty-id splits
    ['E1 /editor/ -> 404 page', '/editor/'],
    ['E2 /Project/ -> 301', '/Project/'],
    ['E3 /project/ -> 301', '/project/'],
    ['E4 /PROJECT/ -> 301', '/PROJECT/'],
  ]
  for (const [tag, path] of cases) {
    out.push(await req('GET', path, { tag }))
  }

  // anonymous (auth bounces first — incl. the bad-id form)
  out.push(await req('GET', `/Project/${PID}`, { tag: 'N1 anon /Project', anon: true }))
  out.push(await req('GET', `/project/${PID}`, { tag: 'N2 anon /project', anon: true }))
  out.push(await req('GET', '/Project/xyz', { tag: 'N3 anon badid', anon: true }))

  console.log('BATTERY-JSON:' + JSON.stringify(out))
}

main().catch((e) => {
  console.log('BATTERY-ERROR: ' + (e && e.message ? e.message : String(e)))
  process.exit(1)
})
