/* U1 in-container battery — runs inside the overleaf container against
 * base (http://127.0.0.1:4000 Node or :4010 Go). Emits:
 *   <diagnostic lines...>
 *   BATTERY-JSON:[{tag,status,ct,loc,body,etag}, ...]
 * The gate compares the arrays across legs.
 */
// args: <mode: battery|setup> <base> <ctx-json-or-path>
const fs = require('fs')
const MODE = process.argv[2]
const BASE = process.argv[3]
const CTX = process.argv[4]
const C = CTX && CTX.startsWith('/') ? JSON.parse(fs.readFileSync(CTX, 'utf8')) : JSON.parse(CTX)
const LONG_NAME = C.LONG

async function login() {
  const h = { 'user-agent': C.UA }
  const r0 = await fetch(BASE + '/login', { headers: { ...h, accept: 'text/html' } })
  const html = await r0.text()
  if (r0.status !== 200) throw new Error('GET /login -> ' + r0.status)
  const csrf0 = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1 = await fetch(BASE + '/login', {
    method: 'POST',
    headers: {
      ...h,
      'content-type': 'application/json',
      accept: 'application/json',
      'x-csrf-token': csrf0,
      cookie: ck0,
    },
    body: JSON.stringify(C.ADMIN),
  })
  const b1 = await r1.text()
  if (r1.status !== 200) throw new Error('POST /login -> ' + r1.status + ' ' + b1.slice(0, 200))
  const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
  const rl = await fetch(BASE + '/login', { headers: { ...h, cookie: ck } })
  const h1 = await rl.text()
  const csrf = (h1.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  if (!csrf) throw new Error('no csrf token post-login')
  return { ck, csrf }
}

function mk(sess) {
  async function req(method, path, body, opts = {}) {
    const h = { 'user-agent': C.UA }
    if (!opts.anon) h.cookie = sess.ck
    h.accept = opts.accept || 'application/json'
    let data
    if (body !== undefined) {
      h['content-type'] = 'application/json'
      data = opts.raw ? body : JSON.stringify(body)
    }
    // Node requires the csrf token on every mutation — including bodyless
    // DELETEs (pinned: 403 "invalid csrf token" otherwise on both stacks).
    if (!opts.anon && method !== 'GET' && h.cookie) {
      h['x-csrf-token'] = sess.csrf
    }
    const r = await fetch(BASE + path, { method, headers: h, body: data, redirect: 'manual' })
    const text = await r.text()
    return {
      tag: opts.tag || method + ' ' + path,
      status: r.status,
      ct: r.headers.get('content-type') || '',
      loc: r.headers.get('location') || '',
      body: text,
      etag: r.headers.get('etag') || '',
    }
  }
  return req
}

async function main() {
  const sess = await login()
  const req = mk(sess)
  const out = []
  const rec = (r) => out.push(r)

  // --- setup: fresh tag per leg; T1/T2/v-tags created by the previous leg's
  // deletion of T3? No — T1/T2/vx/vy/vz persist; T3 is (re)created below.
  rec(await req('POST', '/tag', { name: C.T1 }, { tag: 'U1 create T1' }))
  rec(await req('POST', '/tag', { name: C.T1, color: '#ffff00' }, { tag: 'U2 dup T1' }))
  rec(await req('POST', '/tag', { name: C.T2, color: '#123abc' }, { tag: 'U3 create T2' }))
  rec(await req('POST', '/tag', { name: C.T3 }, { tag: 'U4 create T3' }))
  // T3 id from the last create response body
  const t3body = out[out.length - 1].body
  let idT3 = null
  try {
    idT3 = JSON.parse(t3body)._id
  } catch (e) {
    /* handled below */
  }
  if (!idT3) {
    // dup case: fetch the list and find by name
    const lr = await req('GET', '/tag', undefined, { tag: 'tmp' })
    out.pop()
    const tags = JSON.parse(lr.body)
    const hit = tags.find((t) => t.name === C.T3)
    if (!hit) throw new Error('T3 not found in list')
    idT3 = hit._id
  }

  rec(await req('POST', `/tag/${idT3}/rename`, { name: '' }, { tag: 'U5 rename VA' }))
  rec(await req('POST', `/tag/${idT3}/rename`, { name: C.T3 + 'r' }, { tag: 'U6 rename' }))
  rec(await req('POST', `/tag/${idT3}/edit`, { name: C.T3 + 'e', color: '#010203' }, { tag: 'U7 edit' }))
  rec(await req('POST', '/tag/not-a-oid/rename', { name: 'x' }, { tag: 'U8 rename badoid' }))
  rec(await req('DELETE', '/tag/not-a-oid', undefined, { tag: 'U9 delete badoid' }))
  rec(await req('DELETE', `/tag/${idT3}`, undefined, { tag: 'U10 delete T3' }))
  rec(await req('DELETE', `/tag/${idT3}`, undefined, { tag: 'U11 delete T3 again' }))

  // idT1 from the U1 create
  let idT1 = null
  try {
    idT1 = JSON.parse(out.find((r) => r.tag === 'U1 create T1').body)._id
  } catch (e) {
    /* dup case */
  }
  if (!idT1) {
    const lr = await req('GET', '/tag', undefined, { tag: 'tmp' })
    out.pop()
    const tags = JSON.parse(lr.body)
    idT1 = tags.find((t) => t.name === C.T1)._id
  }

  rec(await req('POST', `/tag/${idT1}/project/notapj`, undefined, { tag: 'G1 add badpid' }))
  rec(await req('POST', `/tag/${idT1}/project/${C.pid}`, undefined, { tag: 'G2 add pid' }))
  rec(await req('POST', `/tag/${idT1}/projects`, { projectIds: [C.pid] }, { tag: 'G3 add many dup' }))
  rec(await req('POST', `/tag/${idT1}/projects`, { projectIds: ['bad'] }, { tag: 'G4 add many bad' }))
  rec(await req('POST', `/tag/${idT1}/projects/remove`, { projectIds: [C.pid] }, { tag: 'G5 remove many' }))
  rec(await req('DELETE', `/tag/${idT1}/project/${C.pid}`, undefined, { tag: 'G6 remove single' }))
  rec(await req('DELETE', `/tag/${idT1}/project/${C.pid}`, undefined, { tag: 'G7 remove single again' }))
  rec(await req('GET', '/tag', undefined, { tag: 'L1 list' }))

  // --- create VA matrix (stateless 400s + one 500) ---
  rec(await req('POST', '/tag', {}, { tag: 'V1 empty' }))
  rec(await req('POST', '/tag', { color: '#aabbcc' }, { tag: 'V2 color-only' }))
  rec(await req('POST', '/tag', { name: '' }, { tag: 'V3 empty-name' }))
  rec(await req('POST', '/tag', { name: 5 }, { tag: 'V4 name-type' }))
  rec(await req('POST', '/tag', { name: C.TMP, color: 'zz' }, { tag: 'V5 color-pattern' }))
  rec(await req('POST', '/tag', { name: C.TMP, color: 7 }, { tag: 'V6 color-type' }))
  rec(await req('POST', '/tag', { name: C.TMP, zzz: 1, aaa: 2 }, { tag: 'V7 unknown-order' }))
  rec(await req('POST', '/tag', { name: LONG_NAME }, { tag: 'V8 500 long' }))

  // --- api/project ---
  rec(await req('POST', '/api/project', {}, { tag: 'A1 bare' }))
  rec(await req('POST', '/api/project', { filters: { ownedByUser: true } }, { tag: 'A2 owned' }))
  rec(await req('POST', '/api/project', { filters: { sharedWithUser: true } }, { tag: 'A3 shared' }))
  rec(await req('POST', '/api/project', { filters: { archived: true } }, { tag: 'A4 archived' }))
  rec(await req('POST', '/api/project', { filters: { trashed: true } }, { tag: 'A5 trashed' }))
  rec(await req('POST', '/api/project', { filters: { tag: C.T1 } }, { tag: 'A6 tag' }))
  rec(await req('POST', '/api/project', { filters: { tag: 'u1-no-such' } }, { tag: 'A7 tag none' }))
  rec(await req('POST', '/api/project', { filters: { tag: null } }, { tag: 'A8 tag null' }))
  rec(await req('POST', '/api/project', { filters: { tag: '' } }, { tag: 'A9 tag empty' }))
  rec(await req('POST', '/api/project', { filters: { search: C.P1.slice(0, 8) } }, { tag: 'A10 search' }))
  rec(await req('POST', '/api/project', { sort: { by: 'title' } }, { tag: 'A11 sort title' }))
  rec(await req('POST', '/api/project', { sort: { by: 'owner', order: 'asc' } }, { tag: 'A12 sort owner' }))
  rec(await req('POST', '/api/project', { sort: { by: 'lastUpdated', order: 'asc' } }, { tag: 'A13 lu asc' }))
  rec(await req('POST', '/api/project', { sort: { by: 'lastUpdated', order: 'desc' } }, { tag: 'A14 lu desc' }))
  rec(await req('POST', '/api/project', { page: { size: 5 } }, { tag: 'A15 page' }))
  rec(await req('POST', '/api/project', { zzz: 1 }, { tag: 'B1 body zzz' }))
  rec(await req('POST', '/api/project', { filters: { zzz: 1 } }, { tag: 'B2 filters zzz' }))
  rec(await req('POST', '/api/project', { sort: { by: 'nope' } }, { tag: 'B3 sort enum' }))
  rec(await req('POST', '/api/project', { sort: { by: 'b1', order: 'o1' } }, { tag: 'B4 sort two' }))
  rec(await req('POST', '/api/project', { page: { size: 0 } }, { tag: 'B5 size0' }))
  rec(await req('POST', '/api/project', { page: { lastId: 'zzz' } }, { tag: 'B6 lastId' }))
  rec(await req('POST', '/api/project', { filters: { ownedByUser: 'x' } }, { tag: 'B7 bool type' }))
  rec(await req('POST', '/api/project', { filters: { tag: 5 } }, { tag: 'B8 tag type' }))
  rec(await req('POST', '/api/project', { sort: null }, { tag: 'B9 sort null' }))
  rec(await req('POST', '/api/project', '[1,2]', { tag: 'B10 body array', raw: true }))
  rec(await req('POST', '/api/project', '5', { tag: 'B11 body scalar', raw: true }))

  // --- anon (fresh, no session) ---
  const anreq = mk({ ck: '', csrf: '' })
  rec(
    await anreq('GET', '/tag', undefined, { tag: 'N1 anon GET json', anon: true }),
  )
  rec(
    await anreq('GET', '/tag', undefined, { tag: 'N2 anon GET browser', anon: true, accept: 'text/html,application/xhtml+xml' }),
  )
  rec(await anreq('POST', '/tag', { name: C.ANON }, { tag: 'N3 anon POST', anon: true }))
  rec(await anreq('POST', '/api/project', {}, { tag: 'N4 anon api', anon: true }))
  rec(await anreq('DELETE', `/tag/${idT1}`, undefined, { tag: 'N5 anon delete', anon: true }))

  // --- private 404 page ---
  rec(await req('GET', `/user/${C.ADMIN_UID}/tag`, undefined, { tag: 'P1 user tag' }))
  rec(await req('GET', '/user/notanuid/tag', undefined, { tag: 'P2 user tag bad' }))

  console.log('BATTERY-JSON:' + JSON.stringify(out))
}

async function setupMode() {
  // Ensure the shared fixture project exists; print its id.
  const sess = await login()
  const req = mk(sess)
  const r = await req('POST', '/api/project', { filters: { search: C.P1 } }, { tag: 'setup lookup' })
  let pid = null
  try {
    const j = JSON.parse(r.body)
    if (j.projects) pid = (j.projects.find((x) => x.name === C.P1) || {}).id || null
  } catch (e) {
    /* not json */
  }
  if (!pid) {
    const cr = await req('POST', '/project/new', { projectName: C.P1 }, { tag: 'setup create' })
    if (cr.status !== 200) throw new Error('setup create project -> ' + cr.status + ' ' + cr.body.slice(0, 200))
    pid = JSON.parse(cr.body).project_id
  }
  if (!/^[0-9a-f]{24}$/.test(pid || '')) throw new Error('setup: bad pid ' + pid)
  console.log('SETUP-JSON:' + JSON.stringify({ pid }))
}

;(MODE === 'setup' ? setupMode() : main()).catch((e) => {
  console.log((MODE === 'setup' ? 'SETUP-ERROR: ' : 'BATTERY-ERROR: ') + (e && e.message ? e.message : String(e)))
  process.exit(1)
})
