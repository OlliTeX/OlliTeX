const BASE = 'http://127.0.0.1:7420'
const PID = '6ab1af7a84388d6d70e6c98c'
const r0 = await fetch(BASE + '/login', { headers: { 'user-agent': 'p72' }, redirect: 'manual' })
const h = await r0.text()
const csrf = (h.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
const r1 = await fetch(BASE + '/login', {
  method: 'POST',
  headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json', cookie: ck0, 'user-agent': 'p72' },
  body: JSON.stringify({ email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }),
  redirect: 'manual',
})
console.log('login:', r1.status)
const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
const r = await fetch(BASE + '/editor/' + PID, { headers: { cookie: ck, accept: 'text/html', 'user-agent': 'p72' }, redirect: 'manual' })
const t = await r.text()
console.log('status:', r.status, 'ct:', (r.headers.get('content-type') || '').slice(0, 40))
console.log('etag:', r.headers.get('etag'))
console.log('CSP:', (r.headers.get('content-security-policy') || '').slice(0, 260))
console.log('body head:', t.slice(0, 400).replace(/\n/g, ' '))
