/* U2 in-container editor-route matrix — runs inside the overleaf container
 * against base (http://127.0.0.1:4000 Node or :4010 Go). Emits:
 *   BATTERY-JSON:[{tag,status,ct,loc,body,etag}, ...]
 *
 * Matrix (POST-retraction 2026-10-05, owner decision): /editor is the ONLY
 * editor page prefix (case-INsensitive Node truth is kept); the legacy
 * /Project/:id + /project/:id PAGE routes are RETIRED (404 now). A
 * non-empty invalid id is 404 JSON (exact body); /editor/ (empty id) →
 * 404 HTML page; anonymous → 302 /login (auth first). /Project/:id/<action>
 * APIs stay (registered elsewhere).
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

  // RETIRED 2026-10-05 (owner decision): the /Project|/project PAGE routes
  // are gone — they now 404 like any unknown path (V1-V4, B1, B3, B4, E2-E4
  // were the retired rows; the /editor family keeps its full matrix).
  const cases = [
    // 200 editor (/editor case-INsensitive — Node/Express truth, kept)
    ['V5 /editor lower', `/editor/${PID}`],
    ['V6 /Editor upper', `/Editor/${PID}`],
    ['V6b /EDITOR UPP id', `/EDITOR/${UPP}`],
    ['V8 detach editor', `/editor/${PID}/detacher`],
    ['V8b detach Editor', `/Editor/${PID}/detached`],
    // 404 JSON (invalid id)
    ['B2 /editor/abc', '/editor/abc'],
    ['B4 /editor/xyz/detached', `/editor/xyz/detached`],
    // empty-id split
    ['E1 /editor/ -> 404 page', '/editor/'],
    // retired prefix -> generic 404 (was 200 / 301 in the Node-era rows)
    ['R1 /Project/ID -> 404 (RETIRED route)', `/Project/${PID}`],
    ['R2 /project/ID -> 404 (RETIRED route)', `/project/${PID}`],
    ['R3 /Project/xyz -> 404 (RETIRED route)', '/Project/xyz'],
    ['R4 /Project/ -> 404 (RETIRED route)', '/Project/'],
    ['R5 /project/ -> 404 (RETIRED route)', '/project/'],
  ]
  for (const [tag, path] of cases) {
    out.push(await req('GET', path, { tag }))
  }

  // anonymous (auth bounces first — incl. the bad-id form; retired prefix
  // now 404s regardless of auth)
  out.push(await req('GET', `/editor/${PID}`, { tag: 'N1 anon /editor', anon: true }))
  out.push(await req('GET', `/Project/${PID}`, { tag: 'N2 anon /Project (retired)', anon: true }))
  out.push(await req('GET', '/editor/xyz', { tag: 'N3 anon badid', anon: true }))

  console.log('BATTERY-JSON:' + JSON.stringify(out))
}

main().catch((e) => {
  console.log('BATTERY-ERROR: ' + (e && e.message ? e.message : String(e)))
  process.exit(1)
})
