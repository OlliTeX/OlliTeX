// U1 edge capture: 500s, bad-OID VA bodies, anon bounces
const B = process.env.T || 'http://127.0.0.1:4000'
const fs = await import('fs')
const r0 = await fetch(B + '/login')
const html0 = await r0.text()
const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
const rc = html0.match(/ol-csrfToken" content="([^"]+)"/)
const r1 = await fetch(B + '/login', { method: 'POST', headers: { 'content-type': 'application/json', accept: 'application/json', cookie: ck0, 'x-csrf-token': rc[1] }, body: JSON.stringify({ email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }) })
if (r1.status !== 200) throw new Error('login ' + r1.status)
const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
const rl = await fetch(B + '/login', { headers: { cookie: ck } })
const h1 = await rl.text()
const csrf = (h1.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]

const out = []
async function rec(m, p, body) {
  const h = { cookie: ck, accept: 'application/json', 'x-csrf-token': csrf }
  let data
  if (body !== undefined) {
    h['content-type'] = 'application/json'
    data = JSON.stringify(body)
  }
  const r = await fetch(B + p, { method: m, headers: h, body: data })
  const text = await r.text()
  const hdrs = {}
  for (const k of ['content-type', 'etag', 'x-powered-by', 'vary', 'transfer-encoding', 'content-length']) {
    const v = r.headers.get(k)
    if (v) hdrs[k] = v
  }
  out.push({ m, p, body, status: r.status, headers: hdrs, body: text.slice(0, 600) })
  return out[out.length - 1]
}

const t = (await rec('POST', '/tag', { name: 'p7u1-edge' + Date.now() })).body
const tid = JSON.parse(t)._id

await rec('POST', '/tag', { name: 'x'.repeat(60) }) // 500 path (create)
await rec('POST', `/tag/${tid}/rename`, { name: 'y'.repeat(60) }) // 500 path (rename)
await rec('DELETE', '/tag/not-an-oid', undefined)
await rec('DELETE', '/tag/6ab225a7db71f85bd15ffaae', undefined) // real oid, idempotent
await rec('POST', `/tag/${tid}/project/notaprojid`, undefined)
await rec('POST', `/tag/${tid}/projects`, { projectIds: ['notaprojid'] })
await rec('POST', `/tag/${tid}/projects/remove`, { projectIds: [] })
await rec('POST', `/tag/${tid}/projects`, { projectIds: [] })
await rec('GET', '/user/6aa4b8a873ef0e5094f4cba3/tag')
await rec('GET', '/user/notanuid/tag')

// anon bounces
for (const [m, p, body] of [['GET', '/tag'], ['POST', '/api/project', {}], ['POST', '/tag', { name: 'anon' }], ['DELETE', '/tag/6ab225a7db71f85bd15ffaae']]) {
  const h = { accept: 'application/json' }
  let data
  if (body !== undefined) {
    h['content-type'] = 'application/json'
    data = JSON.stringify(body)
  }
  const r = await fetch(B + p, { method: m, headers: h, body: data, redirect: 'manual' })
  const text = await r.text()
  const hdrs = {}
  for (const k of ['content-type', 'location', 'x-powered-by', 'transfer-encoding']) {
    const v = r.headers.get(k)
    if (v) hdrs[k] = v
  }
  out.push({ m: m + '(anon)', p, status: r.status, headers: hdrs, body: text.slice(0, 300) })
}

fs.writeFileSync('/tmp/u1-edge.jsonl', out.map((o) => JSON.stringify(o)).join('\n'))
console.log('edge captured:', out.length)
