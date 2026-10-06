// U1 Node oracle (pure fetch) — Node primary 4000. Captures status+headers+body.
const BASE = process.env.T || 'http://127.0.0.1:4000'
const fs = await import('fs')

async function getCookieJar() {
  const r0 = await fetch(BASE + '/login', { redirect: 'manual' })
  const html = await r0.text()
  const csrf = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1 = await fetch(BASE + '/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json', cookie: ck0 },
    body: JSON.stringify({ email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }),
    redirect: 'manual',
  })
  const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
  if (r1.status !== 200) throw new Error('login failed: ' + r1.status)
  return { ck }
}
const { ck } = await getCookieJar()

async function csrfFrom(base) {
  const r = await fetch(base, { headers: { cookie: ck }, redirect: 'manual' })
  const t = await r.text()
  return (t.match(/ol-csrfToken" content="([^"]+)"/) || [])[1] || ''
}

const out = []
async function rec(method, path, body) {
  const headers = { cookie: ck, accept: 'application/json' }
  let data
  if (body !== undefined) {
    headers['content-type'] = 'application/json'
    if (method !== 'GET') headers['x-csrf-token'] = await csrfFrom(BASE + '/login')
    data = JSON.stringify(body)
  }
  let r
  try {
    r = await fetch(BASE + path, { method, headers, body: data, redirect: 'manual' })
  } catch (e) {
    out.push({ method, path, body, err: String(e) })
    return
  }
  const text = await r.text()
  const hdrs = {}
  for (const k of ['content-type', 'etag', 'x-powered-by', 'vary']) {
    const v = r.headers.get(k)
    if (v) hdrs[k] = v
  }
  const entry = { method, path, body, status: r.status, headers: hdrs, body: text.slice(0, 1500) }
  out.push(entry)
  return entry
}
// let rec() callers: previous `out.push(...)` lines unchanged

// setup: 2 tags + a project, tag it
await rec('POST', '/tag', { name: 'p7u1-alpha', color: '#a1b2c3' })
await rec('POST', '/tag', { name: 'p7u1-beta' })
await rec('POST', '/tag', { name: 'p7u1-alpha', color: '#fffff0' }) // dup -> existing
const projBody = await (await rec('POST', '/project/new', { projectName: 'p7u1-list' + Date.now() })).body
const pid = (JSON.parse(projBody) || {}).project_id
// need tag ids: read the list
await rec('GET', '/tag')
// parse tag ids from the last GET /tag is tricky; re-fetch raw
const tagListResp = await fetch(BASE + '/tag', { headers: { cookie: ck, accept: 'application/json' } })
const tagList = await tagListResp.json()
const t1 = tagList.find((t) => t.name === 'p7u1-alpha')
const t2 = tagList.find((t) => t.name === 'p7u1-beta')
await rec('POST', `/tag/${t1._id}/project/${pid}`, undefined)

// ---- battery ----
await rec('GET', '/tag')
await rec('POST', '/api/project', {})
await rec('POST', '/api/project', { filters: {} })
await rec('POST', '/api/project', { filters: { ownedByUser: true } })
await rec('POST', '/api/project', { filters: { sharedWithUser: true } })
await rec('POST', '/api/project', { filters: { archived: true } })
await rec('POST', '/api/project', { filters: { trashed: true } })
await rec('POST', '/api/project', { filters: { tag: 'p7u1-alpha' } })
await rec('POST', '/api/project', { filters: { tag: 'no-such-tag-xyz' } })
await rec('POST', '/api/project', { filters: { tag: null } })
await rec('POST', '/api/project', { filters: { search: 'p7u1' } })
await rec('POST', '/api/project', { filters: { search: '' } })
await rec('POST', '/api/project', { sort: { by: 'lastUpdated', order: 'asc' } })
await rec('POST', '/api/project', { sort: { by: 'title', order: 'asc' } })
await rec('POST', '/api/project', { sort: { by: 'owner', order: 'desc' } })
await rec('POST', '/api/project', { page: { size: 3 } })
await rec('POST', '/api/project', { filters: { ownedByUser: true, search: 'zzz-none' } })
await rec('POST', '/api/project', { filters: { zzz: true } }) // VA strict
await rec('POST', '/api/project', { sort: { by: 'bad' } }) // VA enum
await rec('POST', '/api/project', { page: { size: -1 } }) // VA size
await rec('POST', '/api/project', { zunknown: 1 }) // top strict
await rec('POST', '/tag', { name: '', color: '#aabbcc' }) // VA name empty
await rec('POST', '/tag', { name: 'x', color: 'notacolor' }) // VA color
await rec('POST', '/tag', { name: 'x', color: 'hsl(120, 70%, 45%)' }) // hsl ok
await rec('POST', '/tag', { name: 'x', color: 'hsl(120,70%,45%)' }) // hsl no-space
await rec('POST', '/tag', { name: 'fallback-ok', zzz: 1 }) // fallback extra
await rec('POST', `/tag/${t1._id}/rename`, { name: 'p7u1-alpha-renamed' })
await rec('POST', `/tag/${t1._id}/rename`, { name: '' }) // fallback empty 400
await rec('POST', `/tag/${t1._id}/edit`, { name: 'p7u1-alpha-fin', color: '#010203' })
await rec('POST', `/tag/${t1._id}/project/${pid}`, undefined)
await rec('POST', `/tag/${t2._id}/projects`, { projectIds: [pid] })
await rec('POST', `/tag/${t2._id}/projects/remove`, { projectIds: [pid] })
await rec('DELETE', `/tag/${t1._id}/project/${pid}`, undefined)
await rec('DELETE', `/tag/${t1._id}/project/${pid}`, undefined)
await rec('DELETE', `/tag/${t1._id}`, undefined)
await rec('DELETE', `/tag/${t1._id}`, undefined)
await rec('DELETE', '/tag/zzz-not-an-oid', undefined)

fs.writeFileSync('/tmp/u1-oracle.jsonl', out.map((o) => JSON.stringify(o)).join('\n'))
console.log('captured', out.length, 'cases -> /tmp/u1-oracle.jsonl')
