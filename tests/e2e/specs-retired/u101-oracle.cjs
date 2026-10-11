// U10.1 oracle capture — history-family reads from Node (authoritative).
// Usage: node u101-oracle.cjs > /tmp/u101-oracle.jsonl
const BASE = 'http://127.0.0.1:4000'
const UA = 'Mozilla/5.0 (X11; Linux x86_64) U101Oracle/1.0'
const PID = '6aa4ba9c73ef0e5094f4ce33' // WebGo-Ren-N (e2e-user owned)
const USER = ['e2e-user@e2e.test', 'Ol-Fixture-3m2Q']

async function sess() {
  const h = { 'user-agent': UA }
  const r0 = await fetch(BASE + '/login', { headers: { ...h, accept: 'text/html' } })
  const html = await r0.text()
  const csrf0 = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1 = await fetch(BASE + '/login', {
    method: 'POST',
    headers: { ...h, 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': csrf0, cookie: ck0 },
    body: JSON.stringify({ email: USER[0], password: USER[1] }),
  })
  if (r1.status !== 200) throw new Error('login -> ' + r1.status)
  return { ck: (r1.headers.get('set-cookie') || '').split(';')[0] || ck0 }
}

async function rec(tag, path) {
  const r = await fetch(BASE + path, { redirect: 'manual', headers: { 'user-agent': UA, cookie: S.ck, accept: '' } })
  const body = await r.text()
  return { tag, status: r.status, ct: r.headers.get('content-type') || '', etag: r.headers.get('etag') || '', body }
}

;(async () => {
  S = await sess()
  const recs = []
  for (const [tag, path] of [
    ['updates', '/project/' + PID + '/updates'],
    ['diff', '/project/' + PID + '/diff'],
    ['diff-old1', '/project/' + PID + '/diff?old=1'],
    ['filetree-diff-old1', '/project/' + PID + '/filetree/diff?old=1'],
    ['latest-history', '/project/' + PID + '/latest/history'],
    ['changes', '/project/' + PID + '/changes'],
    ['changes-since0', '/project/' + PID + '/changes?since=0'],
    ['changes-paginated', '/project/' + PID + '/changes?paginated=true'],
    ['labels', '/project/' + PID + '/labels'],
    ['version1-zip-HEAD', '/project/' + PID + '/version/1/zip'],
    ['doc-diff', '/project/' + PID + '/doc/6aafffd0abcdef000000aabb/diff'],
    ['blob-404', '/project/' + PID + '/blob/1111111111111111111111111111111111111111'],
  ]) {
    const r = await rec(tag, path)
    recs.push(r)
  }
  console.log('ORACLE-JSON:' + JSON.stringify(recs))
})().catch((e) => { console.error('ORACLE-FATAL', e && e.message); process.exit(1) })
