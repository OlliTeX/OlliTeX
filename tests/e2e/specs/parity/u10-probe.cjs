// U10 probe battery — candidate residual routes, Node (:4000) vs Go (:4010).
// Usage: node u10-probe.cjs
// Emits table rows: DIFFER|SAME  METHOD PATH  N=status(ct)  G=status(ct)
// (probe-only: read routes + cheap stateless POSTs; no destructive
// mutations, no email sends.)
const BASE_N = 'http://127.0.0.1:4000'
const BASE_G = 'http://127.0.0.1:4010'
const UA = 'Mozilla/5.0 (X11; Linux x86_64) U10Probe/1.0'
const PID = '6aa4ba9c73ef0e5094f4ce33' // WebGo-Ren-N (e2e-user owned), 5 live docs
const DID = '6aafffd0abcdef000000aabb' // live doc
const ADMIN = ['e2e-admin@e2e.test', 'Ol-Fixture-9x7K']
const USER = ['e2e-user@e2e.test', 'Ol-Fixture-3m2Q']

async function sess(base, email, pass) {
  const h = { 'user-agent': UA }
  const r0 = await fetch(base + '/login', { headers: { ...h, accept: 'text/html' } })
  const html = await r0.text()
  const csrf0 = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1 = await fetch(base + '/login', {
    method: 'POST',
    headers: { ...h, 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': csrf0, cookie: ck0 },
    body: JSON.stringify({ email, password: pass }),
  })
  const b1 = await r1.text()
  if (r1.status !== 200) throw new Error('login ' + email + ' ' + base + ' -> ' + r1.status + ' ' + b1.slice(0, 120))
  return { ck: (r1.headers.get('set-cookie') || '').split(';')[0] || ck0, csrf: csrf0 }
}

async function probe(base, path, extra) {
  try {
    const r = await fetch(base + path, { redirect: 'manual', headers: Object.assign({ 'user-agent': UA }, extra || {}) })
    const body = await r.text()
    return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], body }
  } catch (e) {
    return { status: -1, ct: 'ERR:' + e.message.split(' ')[0], body: '' }
  }
}

// [path, who: 'a'|'u'|'n', extra?]
const CASES = [
  // history family
  ['/project/' + PID + '/updates', 'a'],
  ['/project/' + PID + '/updates', 'u'],
  ['/project/' + PID + '/latest/history', 'a'],
  ['/project/' + PID + '/latest/history', 'u'],
  ['/project/' + PID + '/version/1/zip', 'a'],
  ['/project/' + PID + '/filetree/diff?old=1', 'a'],
  ['/project/' + PID + '/filetree/diff?old=1', 'u'],
  ['/project/' + PID + '/doc/' + DID + '/diff', 'a'],
  ['/project/' + PID + '/doc/' + DID + '/diff?old=1', 'a'],
  ['/project/' + PID + '/changes', 'a'],
  ['/project/' + PID + '/changes?range=0-1', 'a'],
  ['/project/' + PID + '/blob/' + DID, 'a'],
  ['/project/' + PID + '/blob/' + DID + '?rev=1', 'a'],
  ['/project/' + PID + '/labels', 'a'],
  ['/project/' + PID + '/labels', 'u'],
  // project-json family
  ['/project/' + PID + '/wordcount', 'a'],
  ['/project/' + PID + '/wordcount', 'u'],
  ['/project/' + PID + '/metadata', 'a'],
  ['/project/' + PID + '/messages', 'a'],
  ['/project/' + PID + '/messages', 'u'],
  ['/project/' + PID + '/entities', 'a'],
  ['/project/' + PID + '/sync/code', 'a'],
  ['/project/' + PID + '/sync/code', 'u'],
  ['/project/' + PID + '/sync/pdf', 'a'],
  ['/project/' + PID + '/tokens', 'a'],
  ['/project/' + PID + '/members', 'a'],
  ['/project/' + PID + '/invites', 'a'],
  ['/project/' + PID + '/access-requests', 'a'],
  ['/project/' + PID + '/sharing-link', 'a'],
  ['/project/' + PID + '/share', 'a'],
  ['/project/' + PID + '/sharing-updates', 'a'],
  ['/project/' + PID + '/flush', 'a'],
  ['/project/' + PID + '/settings', 'a'],
  // user tail
  ['/user/personal_info', 'a'],
  ['/user/personal_info', 'u'],
  ['/user/email-preferences', 'a'],
  ['/user/tpds/queues', 'a'],
  ['/user/notification/6afff', 'a'],
  ['/user/reconfirm', 'u'],
  // misc pages + json
  ['/socket-diagnostics', 'a'],
  ['/socket-diagnostics', 'u'],
  ['/status/compiler/' + PID, 'a'],
  ['/account-suspended', 'a'],
  ['/compromised-password', 'a'],
  ['/beta/participate', 'n'],
  ['/university', 'n'],
  ['/planned_maintenance', 'a'],
  ['/unsupported-browser', 'n'],
  ['/login/legacy', 'n'],
  ['/chrome', 'n'],
  ['/coverage', 'a'],
  ['/read-only/one-time-login', 'a'],
  ['/tutorial/6afff/complete', 'a'],
  ['/notifications', 'a'],
  ['/notifications', 'u'],
  // admin extra
  ['/admin/disconnectAllUsers', 'a'],
  ['/admin/flushProjectToTpds', 'a'],
  ['/admin/pollDropboxForUser', 'a'],
  ['/project/' + PID + '/settings/admin', 'a'],
]

;(async () => {
  const na = await sess(BASE_N, ADMIN[0], ADMIN[1])
  const nu = await sess(BASE_N, USER[0], USER[1])
  const ga = await sess(BASE_G, ADMIN[0], ADMIN[1])
  const gu = await sess(BASE_G, USER[0], USER[1])
  let differ = 0
  for (const [path, who] of CASES) {
    const extraN = who === 'a' ? { cookie: na.ck } : who === 'u' ? { cookie: nu.ck } : { accept: 'text/html' }
    const extraG = who === 'a' ? { cookie: ga.ck } : who === 'u' ? { cookie: gu.ck } : { accept: 'text/html' }
    const n = await probe(BASE_N + '', path, extraN)
    const g = await probe(BASE_G + '', path, extraG)
    const same = n.status === g.status && n.ct === g.ct
    if (!same) differ++
    console.log((same ? 'SAME  ' : 'DIFFER') + ` ${who} ${path}  N=${n.status}(${n.ct}) G=${g.status}(${g.ct})`)
  }
  console.log('U10-PROBE-DONE differ=' + differ + ' of ' + CASES.length)
})().catch((e) => { console.error('U10-PROBE-FATAL', e && e.message); process.exit(1) })
