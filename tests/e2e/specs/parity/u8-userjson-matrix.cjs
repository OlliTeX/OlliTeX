// U8 parity battery — user-family JSON reads (Node vs Go, dual-port).
// Usage: node u8-userjson-matrix.cjs <base>
// Emits BATTERY-JSON:[{tag,status,ct,loc,body}...] (order-stable).
//
// Pin set (Node oracle 2026-09-22, e2e stack):
//   contacts:  {"contacts":[...]} — n DESC / ts DESC order, holdingAccount
//              dropped (after 50-slice), {"id","email","first_name",
//              "last_name","type":"user"} rows.
//   emails:    array of {email, reversedHostname, [createdAt], [_id],
//              default, emailHasInstitutionLicence, lastConfirmedAt} —
//              ABSENT keys omitted entirely (tpladmin fixture record).
//   features:  user.features, stored key order, single-source merge
//              (compileGroup: non-'priority' → 'standard').
//   matrix:    POST/PUT/DELETE with valid CSRF → 403 "Forbidden" chain;
//              anon GET → 302 /login.
const BASE = process.argv[2] || 'http://127.0.0.1:4000'
const UA = 'Mozilla/5.0 (X11; Linux x86_64) ParityBattery/1.0'

const USERS = [
  ['admin', 'e2e-admin@e2e.test', 'Ol-Fixture-9x7K'],
  ['user', 'e2e-user@e2e.test', 'Ol-Fixture-3m2Q'],
  ['tpladmin', 'e2e-tpladmin@e2e.test', 'Ol-Fixture-7tW4'],
]
const GETS = ['/user/contacts', '/user/emails', '/user/features']

async function sess(email, pass) {
  const h = { 'user-agent': UA }
  const r0 = await fetch(BASE + '/login', { headers: { ...h, accept: 'text/html' } })
  if (r0.status !== 200) throw new Error('GET /login -> ' + r0.status)
  const html = await r0.text()
  const csrf0 = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1 = await fetch(BASE + '/login', {
    method: 'POST',
    headers: { ...h, 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': csrf0, cookie: ck0 },
    body: JSON.stringify({ email, password: pass }),
  })
  const b1 = await r1.text()
  if (r1.status !== 200) throw new Error('login ' + email + ' -> ' + r1.status + ' ' + b1.slice(0, 160))
  const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
  return { ck, csrf: csrf0 }
}

async function rec(tag, method, url, extra) {
  const r = await fetch(BASE + url, { method, redirect: 'manual', headers: Object.assign({ 'user-agent': UA }, extra || {}) })
  const body = await r.text()
  return {
    tag,
    method,
    status: r.status,
    ct: r.headers.get('content-type') || '',
    loc: r.headers.get('location') || '',
    body,
  }
}

;(async () => {
  const out = []
  for (const [who, email, pass] of USERS) {
    const s = await sess(email, pass)
    for (const p of GETS) {
      out.push(await rec(who + ' GET ' + p, 'GET', p, { cookie: s.ck, accept: 'application/json' }))
    }
    for (const m of ['POST', 'PUT', 'DELETE']) {
      out.push(await rec(who + ' ' + m + ' /user/contacts (csrf)', m, '/user/contacts', {
        cookie: s.ck,
        'x-csrf-token': s.csrf,
        'content-type': 'application/json',
        body: '{}',
      }))
    }
  }
  out.push(await rec('anon GET /user/emails (accept json) -> 401', 'GET', '/user/emails', { accept: 'application/json' }))
  out.push(await rec('anon GET /user/emails (bare) -> 302', 'GET', '/user/emails', {}))
  console.log('BATTERY-JSON:' + JSON.stringify(out))
})().catch((e) => {
  console.error('BATTERY-FATAL', e && e.message)
  process.exit(1)
})
