// U1 supplement: DELETEs + single add/remove (csrf from POST-login session) + anon bounces
const B = process.env.T || 'http://127.0.0.1:4000'
const fs = await import('fs')
const r0 = await fetch(B + '/login')
const html0 = await r0.text()
const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
const r1 = await fetch(B + '/login', {
  method: 'POST',
  headers: { 'content-type': 'application/json', accept: 'application/json', cookie: ck0, 'x-csrf-token': (html0.match(/ol-csrfToken" content="([^"]+)"/) || [])[1] },
  body: JSON.stringify({ email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }),
})
const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
if (r1.status !== 200) throw new Error('login ' + r1.status)
const rl = await fetch(B + '/login', { headers: { cookie: ck } })
const html1 = await rl.text()
const csrf = (html1.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
const H = { cookie: ck, accept: 'application/json', 'x-csrf-token': csrf }
const out = []
async function rec(method, path, body) {
  const h = { ...H }
  let data
  if (body) {
    h['content-type'] = 'application/json'
    data = JSON.stringify(body)
  }
  const r = await fetch(B + path, { method, headers: h, body: data })
  const text = await r.text()
  const hdrs = {}
  for (const k of ['content-type', 'etag', 'x-powered-by', 'vary']) {
    const v = r.headers.get(k)
    if (v) hdrs[k] = v
  }
  out.push({ method, path, body, status: r.status, headers: hdrs, body: text.slice(0, 1200) })
  return out[out.length - 1]
}

// fresh tag + project for mutation tests
const t = await rec('POST', '/tag', { name: 'p7u1-mut' + Date.now() })
const tid = JSON.parse(t.body)._id
const pj = await rec('POST', '/project/new', { projectName: 'p7u1-mut' + Date.now() })
const pid = JSON.parse(pj.body).project_id

await rec('POST', `/tag/${tid}/project/${pid}`, undefined)
await rec('DELETE', `/tag/${tid}/project/${pid}`, undefined)
await rec('DELETE', `/tag/${tid}/project/${pid}`, undefined) // idempotent
await rec('POST', `/tag/${tid}/projects`, { projectIds: [pid, pid] }) // dup ids
await rec('POST', `/tag/${tid}/projects/remove`, { projectIds: [pid] })
await rec('DELETE', `/tag/${tid}`, undefined)
await rec('DELETE', `/tag/${tid}`, undefined) // idempotent
await rec('DELETE', '/tag/not-an-oid', undefined) // VA
await rec('POST', `/tag/not-an-oid/rename`, { name: 'x' }) // VA
await rec('POST', `/tag/${tid}/project/badoid`, undefined) // VA projectId
await rec('POST', `/tag/${tid}/projects`, { projectIds: ['badoid'] }) // VA array
await rec('POST', `/tag/${tid}/projects/remove`, {}) // VA missing

// anon bounces (fresh context, no cookie)
for (const [m, p] of [['GET', '/tag'], ['POST', '/api/project'], ['POST', '/tag']]) {
  const h = { accept: 'application/json' }
  let data
  if (m === 'POST') {
    h['content-type'] = 'application/json'
    data = JSON.stringify(p === '/tag' ? { name: 'anon' } : {})
  }
  const r = await fetch(B + p, { method: m, headers: h, body: data, redirect: 'manual' })
  const hdrs = {}
  for (const k of ['content-type', 'location', 'set-cookie', 'x-powered-by']) {
    const v = r.headers.get(k)
    if (v) hdrs[k] = v
  }
  out.push({ method: m + '(anon)', path: p, status: r.status, headers: hdrs, body: (await r.text()).slice(0, 200) })
}

fs.writeFileSync('/tmp/u1-oracle2.jsonl', out.map((o) => JSON.stringify(o)).join('\n'))
console.log('supplement captured:', out.length)
