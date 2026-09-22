// U10.2a parity battery — analytics pokes + university redirects + project
// tokens + 404/"Cannot POST" pins + editingSession 429. 3-leg Node==Go==Node.
// Run inside ol-e2e-overleaf-1. LEG 1+3 = 127.0.0.1:4000 (Node), LEG 2 = 4010 (Go).
const LEG = Number(process.env.LEG || 1)
const BASE = LEG === 2 ? 'http://127.0.0.1:4010' : 'http://127.0.0.1:4000'
const B = (p) => BASE + p
const P = '6aa4ba9c73ef0e5094f4ce33' // user-owned, tokens:{}
const NO_TOKENS = process.env.NO_TOKENS_PROJ || '6ab2e233db71f85bd1606679'
let SID = ''
let TOK = ''

async function login() {
  const r0 = await fetch(B('/login'), { headers: { accept: 'text/html' } })
  const h0 = await r0.text()
  const c0 = (h0.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1 = await fetch(B('/login'), {
    method: 'POST',
    headers: { 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': c0, cookie: ck0 },
    body: JSON.stringify({ email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }),
  })
  if (r1.status !== 200) throw new Error('login ' + r1.status + ' ' + (await r1.text()).slice(0, 120))
  const ck1 = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
  const pg = await fetch(B('/hub'), { headers: { accept: 'text/html', cookie: ck1 } })
  const pc = ((await pg.text()).match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  SID = ck1
  TOK = pc
}

async function call(tag, method, path, opts = {}) {
  const h = {}
  if (!opts.anon) {
    h.cookie = `${SID}; ol-csrfToken=${TOK}`
  }
  h.accept = opts.acc || (method === 'GET' ? 'text/html' : 'application/json, text/plain, */*')
  let body
  if (opts.json !== undefined) {
    h['content-type'] = 'application/json'
    body = JSON.stringify(opts.json)
  }
  if (method !== 'GET' && !opts.anon) h['x-csrf-token'] = TOK
  let r
  try {
    r = await fetch(B(path), { method, headers: h, body, redirect: 'manual' })
  } catch (e) {
    return { tag, err: String(e && e.message || e) }
  }
  const b = await r.text()
  const headers = {}
  for (const k of r.headers.keys()) headers[k.toLowerCase()] = r.headers.get(k)
  return {
    tag,
    status: r.status,
    headers,
    len: b.length,
    body: b.slice(0, opts.maxBody || 600),
  }
}

async function main() {
  await login()
  const recs = []
  const push = (m, p, o, tag) => recs.push(call(tag, m, p, o))

  // --- analytics event pokes ---
  push('POST', '/event/test', { json: {} }, 'ev1 POST /event/test {}')
  push('POST', '/event/test', { json: { segmentation: { a: 1 } } }, 'ev2 POST /event/test seg')
  push('POST', '/event/ABC-9.x', { json: { x: 1 } }, 'ev3 POST /event/ABC-9.x (dot = no match)')
  push('POST', '/event/ABC', { json: { x: 1 } }, 'ev3b POST /event/ABC (case-insensitive match)')
  push('POST', '/event/a+b', { json: {} }, 'ev3c POST /event/a+b (plus = no match)')
  push('POST', '/event/a b', { json: {} }, 'ev3d POST /event/"a b" (space = no match)')
  push('POST', '/event/a-1_x', { json: {} }, 'ev3e POST /event/a-1_x (class match)')
  push('POST', '/event/', { json: {} }, 'ev4 POST /event/ (trailing)')
  push('POST', '/event/a/b', { json: {} }, 'ev5 POST /event/a/b (2 seg)')
  push('GET', '/event/test', {}, 'ev6 GET /event/test (wrong method)')
  push('PUT', '/event/test', {}, 'ev7 PUT /event/test (wrong method)')
  push('POST', '/event/test', { anon: true, json: {} }, 'ev8 POST /event anon')
  push('PUT', `/editingSession/${P}`, {}, 'es1 PUT /editingSession P1')
  push('PUT', '/editingSession/deadbeef', { json: { segmentation: { x: 1 } } }, 'es2 PUT /editingSession BADID')
  push('GET', `/editingSession/${P}`, {}, 'es3 GET /editingSession P1')
  push('PUT', '/editingSession', {}, 'es4 PUT /editingSession (no seg)')
  push('PUT', '/editingSession/test', { anon: true }, 'es5 PUT /editingSession anon')
  // --- university redirects ---
  push('GET', '/university', {}, 'uni1 GET /university')
  push('GET', '/university', { acc: 'application/json' }, 'uni2 GET /university (json)')
  push('GET', '/university/Foo', {}, 'uni3 GET /university/Foo')
  push('GET', '/university/a.html', {}, 'uni4 GET /university/a.html')
  push('GET', '/university/A.HTML', {}, 'uni5 GET /university/A.HTML')
  push('GET', '/university', { anon: true }, 'uni6 GET /university anon')
  // --- project tokens ---
  push('GET', `/project/${P}/tokens`, { acc: 'application/json' }, 'tk1 GET tokens (tokens:{})')
  if (NO_TOKENS) push('GET', `/project/${NO_TOKENS}/tokens`, { acc: 'application/json' }, 'tk2 GET tokens (tokens ABSENT)')
  push('GET', '/project/deadbeefdeadbeefdeadbeef/tokens', { acc: 'application/json' }, 'tk3 GET tokens (bad oid)')
  push('GET', `/project/${P}/tokens`, { anon: true, acc: 'application/json' }, 'tk4 GET tokens anon (json)')
  push('GET', `/project/${P}/tokens`, { anon: true, acc: 'text/html' }, 'tk5 GET tokens anon (html)')
  // --- 404 / Cannot-POST pins ---
  push('GET', '/planned_maintenance', {}, 'pm1 GET /planned_maintenance (logged)')
  push('GET', '/planned_maintenance', { anon: true }, 'pm2 GET /planned_maintenance anon')
  push('POST', `/project/${P}/history/resync`, { json: {} }, 'hs1 POST history/resync')
  push('POST', '/project/new/import-document', { json: { name: 'x.docx' } }, 'id1 POST new/import-document')
  push('POST', '/project/new/import-docx', { json: { name: 'x.docx' } }, 'id2 POST new/import-docx')

  const settled = await Promise.all(recs)
  const out = settled.sort((a, b) => String(a.tag).localeCompare(String(b.tag)))

  // --- editingSession 429 (limiter 20/60): 21st call must 429 on both legs.
  const statuses = []
  let last = null
  for (let i = 0; i < 21; i++) {
    const r = await call('es429#' + i, 'PUT', `/editingSession/${P}`, { json: { n: i } })
    statuses.push(r.status)
    last = r
  }
  out.push({ tag: 'es429-serial', statuses: statuses.join(','), lastStatus: last.status, lastBody: (last.body || '').slice(0, 80) })

  // lg1 — the logged-in logout confirmation page (Node UserPagesController.logout).
  // Destroys the session → must stay LAST. The csrf meta value inside the
  // body is per-session (gate normalises), the rest is byte-pinned.
  {
    const r = await call('lg1', 'GET', '/logout', { maxBody: 9500 })
    r.body = (r.body || '').slice(0, 9500)
    out.push({ tag: 'lg1 GET /logout (logged-in page)', status: r.status, headers: r.headers, len: r.len, body: r.body })
  }

  console.log('BATTERY-JSON:' + JSON.stringify(out))
}
main().catch((e) => {
  console.error('BATTERY-ERROR', e && e.message)
  process.exit(1)
})
