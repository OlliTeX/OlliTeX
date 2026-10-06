const B = process.env.T || 'http://127.0.0.1:4000'
const routes = ['/project','/project/owned','/project/shared','/project/archived','/project/trashed','/project/untagged','/project/tags/mytag']
const r0 = await fetch(B + '/login')
const h = await r0.text()
const csrf = (h.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
const r1 = await fetch(B + '/login', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json', cookie: ck0 }, body: JSON.stringify({ email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }) })
const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
console.log('login:', r1.status)
// anonymous
for (const u of [routes[0], routes[6]]) {
  const r = await fetch(B + u, { redirect: 'manual' })
  console.log('anon', u, '=>', r.status, 'loc:' + (r.headers.get('location') || '').slice(0, 28), JSON.stringify((await r.text()).slice(0, 45)))
}
// authed
let bad = 0
for (const u of routes) {
  const r = await fetch(B + u, { headers: { cookie: ck }, redirect: 'manual' })
  const loc = r.headers.get('location') || ''
  const body = await r.text()
  const exp = 'Moved Permanently. Redirecting to ' + loc
  const ok = r.status === 301 && body === exp
  if (!ok) bad++
  console.log(ok ? 'OK ' : 'BAD', u, '=>', r.status, loc, '| ct:' + (r.headers.get('content-type') || '').slice(0, 22))
}
console.log(bad === 0 ? 'ALL-7-ORACLE-MATCH' : bad + ' MISMATCH')
