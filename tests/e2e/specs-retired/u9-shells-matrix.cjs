// U9 parity battery — hub/admin/home shell pages (Node vs Go, dual-port).
// Usage: node u9-shells-matrix.cjs <base>
// Emits BATTERY-JSON:[{tag,method,status,ct,loc,etag,body}...] (order-stable).
//
// Pin set (Node oracle, live A/B on this stack 2026-09-22):
//   admin  /hub, /hub/, /HUB, /HUB/      → 200 (case + slash variants;
//                                            canonical currentUrl + alternate
//                                            link reflect the requested path)
//   admin  /admin, /admin/, /Admin,
//          /ADMIN                        → 200 admin shell (LLM tab on:
//                                            LLM_ENABLED=true here;
//                                            system-messages empty; open
//                                            sockets empty <ul></ul>)
//   admin  /hub/admin, /hub/admin/,
//          /HUB/ADMIN, /hub/workspace    → 302 /hub
//   admin|user /                        → 302 /hub (logged in)
//   admin|user /home                    → 302 /login (HomeController.home —
//                                            the home pug is absent in this
//                                            build)
//   user   /admin (any variant)         → 302 /restricted?from=%2Fadmin
//   user   /restricted?from=%2Fadmin    → 200 restricted page (ol-usersEmail
//                                            + ol-user_id filled from the
//                                            SESSION; canManageTemplatesMenu
//                                            false)
//   user   /login                       → 200 (navbar sessionUser filled)
//   anon   /login, /register            → 200 (anonymous-shape navbars:
//                                            showSignUpLink false — SAML IdP
//                                            enabled in this stack)
//   anon   /admin, /home, /hub, /       → accept-json 401 "Unauthorized";
//                                            accept-html 302 /login
//   admin  /user/settings, /logout      → 200 (exposed-settings
//                                            canManageTemplatesMenu true)
const BASE = process.argv[2] || 'http://127.0.0.1:4000'
const UA = 'Mozilla/5.0 (X11; Linux x86_64) ParityBattery/1.0'

const ADMIN = ['e2e-admin@e2e.test', 'Ol-Fixture-9x7K']
const USER = ['e2e-user@e2e.test', 'Ol-Fixture-3m2Q']

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

async function rec(tag, url, extra) {
  const r = await fetch(BASE + url, { redirect: 'manual', headers: Object.assign({ 'user-agent': UA }, extra || {}) })
  const body = await r.text()
  const etag = r.headers.get('etag') || ''
  return {
    tag,
    status: r.status,
    ct: r.headers.get('content-type') || '',
    loc: r.headers.get('location') || '',
    etaglen: /W\/"([0-9a-f]+)-/i.test(etag) ? Number('0x' + etag.match(/W\/"([0-9a-f]+)-/i)[1]) : -1,
    body,
  }
}

;(async () => {
  const out = []
  const adm = await sess(ADMIN[0], ADMIN[1])
  const use = await sess(USER[0], USER[1])
  const A = { cookie: adm.ck }
  const U = { cookie: use.ck }

  // ---- 200 page families (admin) ----
  for (const p of ['/hub', '/hub/', '/HUB', '/HUB/']) {
    out.push(await rec('admin GET ' + p, p, A))
  }
  for (const p of ['/admin', '/admin/', '/Admin', '/ADMIN']) {
    out.push(await rec('admin GET ' + p, p, A))
  }
  // ---- 302 alias family (admin) ----
  for (const p of ['/hub/admin', '/hub/admin/', '/HUB/ADMIN', '/hub/workspace', '/hub/workspace/']) {
    out.push(await rec('admin GET ' + p, p, A))
  }
  // ---- 302 /hub (root, both users) ----
  out.push(await rec('admin GET /', '/', A))
  out.push(await rec('user  GET /', '/', U))
  // ---- /home → 302 /login (both users + case/slash variants) ----
  for (const p of ['/home', '/Home', '/home/']) {
    out.push(await rec('admin GET ' + p, p, A))
    out.push(await rec('user  GET ' + p, p, U))
  }
  // ---- non-admin bounce (user) ----
  for (const p of ['/admin', '/Admin', '/admin/']) {
    out.push(await rec('user  GET ' + p, p, U))
  }
  out.push(await rec('user  GET /restricted?from=%2Fadmin', '/restricted?from=%2Fadmin', U))
  // ---- login + settings + logout (both states) ----
  out.push(await rec('user  GET /login (logged-in)', '/login', U))
  out.push(await rec('anon  GET /login', '/login', {}))
  out.push(await rec('anon  GET /register', '/register', {}))
  out.push(await rec('admin GET /user/settings', '/user/settings', A))
  out.push(await rec('admin GET /logout', '/logout', A))
  out.push(await rec('user  GET /logout', '/logout', U))
  // ---- anonymous gate chains ----
  for (const p of ['/admin', '/home', '/hub', '/', '/restricted']) {
    out.push(await rec('anon  GET ' + p + ' (accept-json)', p, { accept: 'application/json' }))
  }
  for (const p of ['/admin', '/home', '/', '/hub']) {
    out.push(await rec('anon  GET ' + p + ' (accept-html)', p, { accept: 'text/html' }))
  }
  console.log('BATTERY-JSON:' + JSON.stringify(out))
})().catch((e) => {
  console.error('BATTERY-FATAL', e && e.message)
  process.exit(1)
})
