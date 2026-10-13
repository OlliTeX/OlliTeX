// Container-side probe helper for the InactiveData e2e spec (inactive-data).
// Runs INSIDE the web container (node is present; the image ships a Node
// toolchain). Busybox wget drops 4xx bodies, so this does the fetch instead.
// Reads everything from env (secrets never leave the container namespace):
//   U  = url, M = GET|POST, B = request body (POST), A = none|wrong|right
// Prints: PROBE_STATUS=<n>, PROBE_AUTH=<www-authenticate|empty>, then
// PROBE_BODYSTART, then up to 400 body bytes.
'use strict'
const u = process.env.U
const M = process.env.M || 'GET'
const B = process.env.B || ''
const A = process.env.A || 'none'
const h = {}
if (A === 'right') {
  h.authorization = 'Basic ' + Buffer.from(process.env.WEB_API_USER + ':' + process.env.WEB_API_PASSWORD).toString('base64')
} else if (A === 'wrong') {
  h.authorization = 'Basic ' + Buffer.from('nope:nope').toString('base64')
}
fetch(u, {
  method: M,
  body: M === 'POST' ? B : undefined,
  headers: M === 'POST' ? { 'content-type': 'application/json', ...h } : h,
})
  .then(async (r) => {
    console.log('PROBE_STATUS=' + r.status)
    console.log('PROBE_AUTH=' + (r.headers.get('www-authenticate') || ''))
    console.log('PROBE_BODYSTART')
    console.log((await r.text()).slice(0, 400))
  })
  .catch((e) => {
    console.log('PROBE_FETCHERR=' + (e && e.message))
    process.exit(3)
  })
