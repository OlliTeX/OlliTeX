// U1 full-capture: complete api/project responses (no truncation), exact key order
const B = process.env.T || 'http://127.0.0.1:4000'
const fs = await import('fs')
const r0 = await fetch(B + '/login')
const html0 = await r0.text()
const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
const rc = html0.match(/ol-csrfToken" content="([^"]+)"/)
const r1 = await fetch(B + '/login', { method: 'POST', headers: { 'content-type': 'application/json', accept: 'application/json', cookie: ck0, 'x-csrf-token': rc[1] }, body: JSON.stringify({ email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }) })
if (r1.status !== 200) throw new Error('login ' + r1.status)
const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0

const calls = [
  ['POST', '/api/project', { filters: {} }],
  ['POST', '/api/project', { page: { size: 3 } }],
  ['POST', '/api/project', { sort: { by: 'lastUpdated', order: 'asc' } }],
  ['POST', '/api/project', { filters: { ownedByUser: true } }],
]
const out = []
for (const [m, p, b] of calls) {
  const r = await fetch(B + p, { method: m, headers: { 'content-type': 'application/json', accept: 'application/json', cookie: ck }, body: JSON.stringify(b) })
  const text = await r.text()
  out.push({ body: b, status: r.status, len: text.length, first: text.slice(0, 400), last: text.slice(-120), keys: JSON.parse(text).projects ? Object.keys(JSON.parse(text).projects[0]) : null, count: JSON.parse(text).projects?.length })
}
fs.writeFileSync('/tmp/u1-full.json', JSON.stringify(out))
console.log(out.map((o) => `body=${JSON.stringify(o.body)} status=${o.status} len=${o.len} projects=${o.count} keys=${o.keys}`).join('\n'))
