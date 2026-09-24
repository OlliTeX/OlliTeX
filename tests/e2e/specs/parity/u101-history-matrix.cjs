// U10.1 — History web-family per-leg battery (U1/U2/U8/U9 gate idiom).
// Usage: node u101-history-matrix.cjs http://127.0.0.1:4000   (Node leg)
//        node u101-history-matrix.cjs http://127.0.0.1:4010   (Go leg)
// Emits: BATTERY-JSON:[{tag,status,ct,body,len,headers,b64?...}, ...]
//
// Honest scope pin: the valid-body SUCCESS paths of
// PUT restore_file / revert_file / revert-project execute Node's
// RestoreManager (local blob fs + Mongo + V2) — ported in U10.5 with its own
// oracle gate. This battery pins the shared wire: access gates (401/403),
// exact 400 validation bodies (escaped "at \"...\"" paths), 404 param
// validation, read/zip/blob/label proxies, and idempotent blob upsert.
const P = '6aa4ba9c73ef0e5094f4ce33'        // WebGo-Ren-N (v1 history present)
const NOP = '6ab1c3bd8e06b0422ceac4a1'      // webgo-p4inv-d (NO v1 history id)
const BLOB = '5075e7f8058697019905149437412c45900faf8c'
const ZZZ = 'zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz'
const BASE = process.argv[2]
if (!BASE) { console.error('usage: u101-history-matrix.cjs <base-url>'); process.exit(2) }

const REC = []
function rec(tag, r, text, bin) {
  const o = { tag, status: r.status, ct: r.headers.get('content-type') || '', body: text, len: (text || '').length }
  if (bin) { o.b64 = (text || '').toString('base64'); o.body = '' }
  const hd = {}
  for (const k of ['content-length', 'content-disposition', 'content-range', 'etag', 'cache-control', 'x-accel-buffering', 'vary', 'x-frame-options', 'location', 'set-cookie']) {
    const v = r.headers.get(k)
    if (v != null) hd[k] = v
  }
  o.headers = hd
  REC.push(o)
}
function normBody(b) {
  return b
    .replace(/nonce="[^"]*"/g, 'nonce="N"')
    .replace(/(ol-csrfToken" content=")[^"]*"/g, '$1C"')
    .replace(/ol-csrfToken=(\w+)/g, 'ol-csrfToken=T')
    .replace(/"sid":"[^"]*"/g, '"sid":"S"')
    .replace(/overleaf\.sid=[a-z0-9.%=]+/g, 'overleaf.sid=X')
    .replace(/expires=[^;"]+;?/gi, 'expires=E;')
    .replace(/content="application\/json/.replace(/(.*)$/, '') || '', '')
    .replace(new RegExp('overleaf\\.sid=[A-Za-z0-9._\\-]+', 'g'), 'overleaf.sid=X')
    // zip mtime: zero the 2-byte time + 2-byte date fields after each PK\x03\x04
    // local header and each PK\x01\x02 central header.
}
function zipNorm(buf) {
  const b = Buffer.from(buf)
  const out = Buffer.from(b)
  const magicLocal = Buffer.from([0x50, 0x4b, 0x03, 0x04])
  const magicCent = Buffer.from([0x50, 0x4b, 0x01, 0x02])
  for (let i = 0; i + 4 < b.length; i++) {
    if (b.subarray(i, i + 4).equals(magicLocal) || b.subarray(i, i + 4).equals(magicCent)) {
      out[i + 10] = 0; out[i + 11] = 0; out[i + 12] = 0; out[i + 13] = 0
    }
  }
  return out
}
const U = (p) => BASE + p

let S = ''
let C = {}
async function login(email, password, into) {
  // A/B-verified Node login flow: GET /login -> csrf+cookie; POST /login
  // (accept: application/json -> 200 + NEW session cookie); GET /hub for the
  // post-login csrf token bound to that session.
  const r0 = await fetch(U('/login'), { headers: { 'user-agent': 'u101-gate', accept: 'text/html' } })
  const h0 = await r0.text()
  const csrf0 = (h0.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1 = await fetch(U('/login'), {
    method: 'POST',
    headers: { 'user-agent': 'u101-gate', 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': csrf0, cookie: ck0 },
    body: JSON.stringify({ email, password }),
  })
  if (r1.status !== 200) throw new Error('login POST /login -> ' + r1.status)
  const ck1 = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
  const pg = await fetch(U('/hub'), { headers: { 'user-agent': 'u101-gate', accept: 'text/html', cookie: ck1 } })
  const pc = ((await pg.text()).match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  if (!pc) throw new Error('no post-login csrf token for ' + email)
  if (into) { into.ck = ck1; into.tok = pc }
  return { ck: ck1, tok: pc }
}

async function leg() {
  const me = {}
  await login('e2e-user@e2e.test', 'Ol-Fixture-3m2Q', me)
  S = me.ck
  C = { 'x-csrf-token': me.tok }
  const J = { 'content-type': 'application/json', accept: 'application/json', cookie: S }
  const H = { 'content-type': 'application/json', accept: 'text/html', cookie: S }
  const ANJ = { accept: 'application/json' }
  const ANH = { accept: 'text/html' }
  const them = {}
  await login('e2e-admin@e2e.test', 'Ol-Fixture-9x7K', them)
  const NJ = { 'content-type': 'application/json', accept: 'application/json', cookie: them.ck, 'x-csrf-token': them.tok }

  // ---- updates
  {
    const r = await fetch(U(`/project/${P}/updates`), { headers: J })
    rec('updates member', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/updates`), { headers: ANJ })
    rec('updates anon json', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/updates`), { headers: NJ })
    rec('updates nonmember json', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/updates`), { headers: { ...NJ, accept: 'text/html' } })
    rec('updates nonmember html', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${NOP}/updates`), { headers: J })
    rec('updates nohistory', r, await r.text())
  }

  // ---- latest-history
  {
    const r = await fetch(U(`/project/${P}/latest/history`), { headers: J })
    rec('latestHistory member', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/latest/history`), { headers: ANJ })
    rec('latestHistory anon', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${NOP}/latest/history`), { headers: J })
    rec('latestHistory nohistory', r, await r.text())
  }

  // ---- changes
  {
    const r = await fetch(U(`/project/${P}/changes?since=0`), { headers: J })
    rec('changes since0 member', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/changes?since=1`), { headers: J })
    rec('changes since1 member', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/changes?since=abc`), { headers: J })
    rec('changes since-abc', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/changes`), { headers: ANJ })
    rec('changes anon', r, await r.text())
  }

  // ---- labels read / create / delete
  {
    const r = await fetch(U(`/project/${P}/labels`), { headers: J })
    rec('labels get member', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/labels`), { headers: ANJ })
    rec('labels get anon', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/labels`), { headers: { ...J, ...C }, method: 'POST', body: JSON.stringify({ comment: 'u101-gate', version: 1 }) })
    const b = await r.text()
    rec('labels create', r, b)
    const m = /"id":"([0-9a-f]{24})"/.exec(b)
    global.__LABEL = m ? m[1] : ''
    if (!global.__LABEL) console.error('LABEL-CREATE-FAILED status=' + r.status + ' tok-present=' + (C['x-csrf-token'] ? 'yes' : 'no') + ' body=' + b.slice(0, 300))
  }
  {
    const r = await fetch(U(`/project/${P}/labels`), { headers: { ...J, ...C }, method: 'POST', body: JSON.stringify({ bogus: 1 }) })
    rec('labels create-unknown-key', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/labels`), { headers: { ...ANJ, ...C }, method: 'POST', body: JSON.stringify({ comment: 'x' }) })
    rec('labels create anon', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/labels`), { headers: J, method: 'POST', body: JSON.stringify({ comment: 'x' }) })
    rec('labels create-nocsrf', r, await r.text())
  }
  {
    const L = global.__LABEL
    if (!L) { REC.push({ tag: 'labels delete', status: -1, ct: '', body: 'NO LABEL CREATED', len: 0, headers: {} }); return }
    const r = await fetch(U(`/project/${P}/labels/${L}`), { headers: { ...J, ...C }, method: 'DELETE' })
    rec('labels delete', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/labels/badlabelid`), { headers: { ...J, ...C }, method: 'DELETE' })
    rec('labels delete-badid', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/labels/6ab2bbd9c9f8a4f1a9025af6`), { headers: { ...NJ, ...C }, method: 'DELETE' })
    rec('labels delete nonmember', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/labels/6ab2bbd9c9f8a4f1a9025af6`), { headers: { ...NJ, ...C, accept: 'text/html' }, method: 'DELETE' })
    rec('labels delete nonmember html', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/labels/6ab2bbd9c9f8a4f1a9025af6`), { headers: { ...ANJ, ...C }, method: 'DELETE' })
    rec('labels delete anon', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/labels`), { headers: { accept: 'application/json', ...C }, method: 'POST', body: JSON.stringify({ comment: 'x' }) })
    rec('labels create anon nojsonct', r, await r.text())
  }

  // ---- blob (upsert idempotent, get, head, range, 304, validation)
  {
    const r = await fetch(U(`/project/${P}/blob/${BLOB}`), { headers: J, method: 'POST' })
    rec('blob upsert first', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/blob/${BLOB}`), { headers: J, method: 'POST' })
    rec('blob upsert second', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/blob/${BLOB}`), { headers: { ...J, accept: '*/*' } })
    rec('blob get member', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/blob/${BLOB}`), { headers: { ...J, accept: '*/*' }, method: 'HEAD' })
    rec('blob head member', r, '')
  }
  {
    const r = await fetch(U(`/project/${P}/blob/${BLOB}`), { headers: { accept: '*/*', cookie: S, range: 'bytes=10-39' } })
    rec('blob range', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/blob/${BLOB}`), { headers: { accept: '*/*', cookie: S, 'if-none-match': BLOB } })
    rec('blob 304', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/blob/${ZZZ}`), { headers: J })
    rec('blob badhash', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/blob/${BLOB}`), { headers: ANJ })
    rec('blob get anon', r, await r.text())
  }

  // ---- version zip
  {
    const r = await fetch(U(`/project/${P}/version/1/zip`), { headers: { accept: 'text/html', cookie: S } })
    const buf = Buffer.from(await r.arrayBuffer())
    rec('zip v1 member', r, zipNorm(buf).toString('base64'), true)
  }
  {
    const r = await fetch(U(`/project/${P}/version/1/zip`), { headers: { accept: 'text/html', cookie: S }, method: 'HEAD' })
    rec('zip v1 head', r, '')
  }
  {
    const r = await fetch(U(`/project/${NOP}/version/1/zip`), { headers: { accept: 'text/plain', cookie: S } })
    rec('zip nohistory', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/version/-1/zip`), { headers: J })
    rec('zip version neg', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/version/9999/zip`), { headers: J })
    rec('zip version 9999', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/version/1/zip`), { headers: ANH })
    rec('zip anon', r, await r.text())
  }

  // ---- flush
  {
    const r = await fetch(U(`/project/${P}/flush`), { headers: J, method: 'POST', body: '' })
    rec('flush member', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/flush`), { headers: ANJ, method: 'POST', body: '' })
    rec('flush anon', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/flush`), { headers: NJ, method: 'POST', body: '' })
    rec('flush nonmember', r, await r.text())
  }

  // ---- bad id / doc-did validation
  {
    const r = await fetch(U('/project/notanid/updates'), { headers: J })
    rec('badid updates member', r, await r.text())
  }
  {
    const r = await fetch(U('/project/notanid/updates'), { headers: ANJ })
    rec('badid updates anon', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/doc/badlabelid/diff?from=0&version=1`), { headers: J })
    rec('docdiff baddid member', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/doc/badlabelid/diff?from=0&version=1`), { headers: ANJ })
    rec('docdiff baddid anon', r, await r.text())
  }

  // ---- diff reads
  {
    const r = await fetch(U(`/project/${P}/diff?from=0&version=1`), { headers: J })
    rec('diff 0-1 member', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/filetree/diff?version=1`), { headers: J })
    rec('filetree diff member', r, await r.text())
  }

  // ---- restore gates (success paths deferred to U10.5)
  {
    const r = await fetch(U(`/project/${P}/restore_file`), { headers: { ...J, ...C }, method: 'POST', body: JSON.stringify({ version: 'notanumber' }) })
    rec('restore_file badversion', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/revert_file`), { headers: { ...J, ...C }, method: 'POST', body: JSON.stringify({ version: 'notanumber' }) })
    rec('revert_file badversion', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/revert-project`), { headers: { ...J, ...C }, method: 'POST', body: JSON.stringify({ version: 'notanumber' }) })
    rec('revert_project badversion', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/restore_file`), { headers: { ...ANJ, ...C }, method: 'POST', body: JSON.stringify({}) })
    rec('restore_file anon', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/restore_file`), { headers: { ...NJ, ...C }, method: 'POST', body: JSON.stringify({ version: 1 }) })
    rec('restore_file nonmember', r, await r.text())
  }
  {
    const r = await fetch(U(`/project/${P}/restore_file`), { headers: { ...J, ...C }, method: 'POST', body: JSON.stringify({ version: 1, bogus: true }) })
    rec('restore_file unknownkey', r, await r.text())
  }

  const fs = require('fs')
  const out = 'BATTERY-JSON:' + JSON.stringify(REC)
  fs.writeFileSync('/tmp/u101-battery-out.json', out)
  console.log('LEG-DONE cases=' + REC.length + ' bytes=' + out.length)
}

process.on('unhandledRejection', (e) => { console.error('UNHANDLED', e && e.stack || e); process.exit(2) })
leg()
  .then(() => { console.error('LEG-RESOLVED') })
  .catch((e) => { console.error('LEG-FAIL', e && e.stack || e); process.exit(1) })
