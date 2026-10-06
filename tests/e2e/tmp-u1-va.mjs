// U1 VA-message batch
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
const csrf = (await rl.text()).match(/ol-csrfToken" content="([^"]+)"/)[1]
const calls = [
  ['POST', '/tag', {}],
  ['POST', '/tag', { color: '#aabbcc' }],
  ['POST', '/tag', { name: 5 }],
  ['POST', '/tag', { name: 'ok-99', color: 5 }],
  ['POST', '/api/project', { sort: { by: 'b1', order: 'o1' } }],
  ['POST', '/api/project', { page: { lastId: 'zzz' } }],
  ['POST', '/api/project', { filters: { tag: true } }],
  ['POST', '/api/project', { filters: { search: 5 } }],
  ['POST', '/api/project', { filters: { ownedByUser: 'yes' } }],
  ['POST', '/api/project', { sort: null }],
  ['POST', '/api/project', { filters: { tag: '' } }],
  ['POST', '/api/project', { filters: { tag: '' }, sort: { by: 'title' } }],
]
const out = []
for (const [m, p, b] of calls) {
  const r = await fetch(B + p, { method: m, headers: { 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': csrf, cookie: ck }, body: JSON.stringify(b) })
  out.push({ m, p, sent: b, status: r.status, body: (await r.text()).slice(0, 400) })
}
fs.writeFileSync('/tmp/u1-va.jsonl', out.map((o) => JSON.stringify(o)).join('\n'))
console.log('va batch:', out.length)
