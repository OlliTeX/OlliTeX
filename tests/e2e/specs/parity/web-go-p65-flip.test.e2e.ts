/**
 * P6.5 flip gate — bib-editor library surface (Node → OlliTeX Go web).
 *
 * 3-leg contract-parity gate (same harness family as P6.4a/P6.4b):
 *   leg 0: flips stripped before start (clean slate)
 *   leg 1: Node baseline battery (57-pin oracle, /tmp/p65_oracle.mjs shape)
 *   leg 2: CUMULATIVE flip (P6.4a + P6.4b + P6.5) → same battery against Go
 *   leg 3: flips stripped → Node re-baseline (Node determinism anchor)
 *
 * Pins: status + content-type + body (volatile fields normalized: 24-hex
 * ObjectIds → OID, nextCursor → CURSOR, ISO timestamps → TS, nonce/CSRF).
 * Cleanup between/after legs: `libraryreferences` emptied (the battery
 * creates and deletes its own docs; the collection is asserted empty at
 * the end of every leg).
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
// Cumulative: the Go leg must carry the full flipped so far (P7 ships the
// union); the library battery never touches LLM state, so P6.4a/P6.4b
// flips are inert here — they only prove coexistence.
const FLIPCONFS = ['web-p64a.conf', 'web-p64b.conf', 'web-p65.conf']
const FLIPSRC = `${process.cwd()}/../../server-ce/nginx/flips`
const BASE = 'http://127.0.0.1:7420'
const USER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const UA = 'p65-gate'

type Leg = Record<string, { status: number; ct: string; loc: string; body: string }>

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function msh(cmd: string, capture = false): string {
  try {
    const out = execFileSync('docker', ['exec', mongoC, 'mongosh', 'mongodb://127.0.0.1:27017/sharelatex', '--quiet', '--eval', cmd], {
      encoding: 'utf8',
      stdio: capture ? 'pipe' : 'ignore',
      maxBuffer: 64 * 1024 * 1024,
    })
    return out || ''
  } catch (e: any) {
    if (capture) throw e
    return (e.stdout ? e.stdout.toString() : '') || ''
  }
}

function dexeStrict(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 64 * 1024 * 1024 })
}

function dexe(c: string, cmd: string, capture = false): string {
  try {
    const out = execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: capture ? 'pipe' : 'ignore', maxBuffer: 64 * 1024 * 1024 })
    return out || ''
  } catch (e: any) {
    if (capture) throw e
    return ''
  }
}

async function waitGo(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    if (dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim() === '200') return
    await sleep(500)
  }
  throw new Error('Go :4010 never came up')
}

function flipCount(conf: string): number {
  return Number(dexeStrict(overleafC, `sh -c 'grep -c "overleaf-flips/${conf}" /etc/nginx/sites-enabled/overleaf.conf || true'`).trim()) || 0
}

async function flip(mode: 'apply' | 'strip'): Promise<void> {
  if (mode === 'strip') {
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';const s=fs.readFileSync(p,'utf8');const L=s.split(String.fromCharCode(10)).filter(x=>!x.includes('overleaf-flips/'));fs.writeFileSync(p,L.join(String.fromCharCode(10)))"`)
    dexeStrict(overleafC, 'nginx -t && nginx -s reload')
    const r: any = await fetch(BASE + '/status', { headers: { 'user-agent': UA } })
    if (r.status !== 200) throw new Error('strip failed (status not 200)')
    await sleep(1200)
    for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
    return
  }
  dexeStrict(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && mkdir -p /etc/nginx/overleaf-flips')
  for (const conf of FLIPCONFS) {
    execFileSync('docker', ['cp', `${FLIPSRC}/${conf}`, `${overleafC}:/usr/local/share/overleaf-flips/${conf}`], { timeout: 30000 })
    dexeStrict(overleafC, `cp -f /usr/local/share/overleaf-flips/${conf} /etc/nginx/overleaf-flips/${conf}`)
    dexeStrict(overleafC, `node -e "const fs=require('fs');const p='/etc/nginx/sites-enabled/overleaf.conf';let s=fs.readFileSync(p,'utf8');const inc='  include /etc/nginx/overleaf-flips/${conf};'+String.fromCharCode(10);if(!s.includes('overleaf-flips/${conf}')){if(s.includes('location / {')){s=s.replace('location / {',inc+'location / {',1)}else{throw new Error('anchor not found')}};fs.writeFileSync(p,s)"`)
  }
  dexeStrict(overleafC, 'nginx -t && nginx -s reload')
  // nginx reload is async; early requests can hit a draining worker.
  await sleep(1200)
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(1)
}

// ---------- cleanup / state ----------
function wipeLibrary(): void {
  msh('db.libraryreferences.deleteMany({})', true)
}
function libraryCount(): number {
  return Number(msh('print(db.libraryreferences.countDocuments({}))', true).trim().split('\n').pop()) || 0
}

// Rate-limit counters are SHARED (identical redis key namespace + same client
// IP on both stacks) — flush the bib-library windows at the start of every
// leg so a 429 can never leak into the parity battery.
function flushRLim(): void {
  try {
    const out = execFileSync('docker', ['exec', 'ol-e2e-redis-1', 'redis-cli', 'keys', 'rate-limit:bib-library*'], {
      encoding: 'utf8',
      timeout: 15000,
    })
    const keys = out.split('\n').map((k) => k.trim()).filter((k) => k.length > 0)
    if (keys.length) {
      execFileSync('docker', ['exec', 'ol-e2e-redis-1', 'redis-cli', 'del', ...keys], { encoding: 'utf8', timeout: 15000 })
    }
  } catch {
    /* best effort — batteries fit within the windows when warm start is clean */
  }
}

// ---------- volatile normalization ----------
const norm = (s: string): string =>
  s
    .replace(/"(_id)":"[0-9a-f]{24}"/g, '"_id":"OID"')
    .replace(/"nextCursor":"[0-9a-f]{24}"/g, '"nextCursor":"CURSOR"')
    .replace(/"createdAt":"[^"]*"/g, '"createdAt":"TS"')
    .replace(/"updatedAt":"[^"]*"/g, '"updatedAt":"TS"')
    .replace(/nonce="[^"]+"/g, 'nonce="NONCE"')
    .replace(/<meta name="ol-csrfToken" content="[^"]*"/g, '<meta name="ol-csrfToken" content="CSRF"')
    .replace(/<input name="_csrf" type="hidden" value="[^"]*"/g, '<input name="_csrf" type="hidden" value="CSRF"')
    .replace(/20\d{2}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z/g, 'TS')

function diffLegs(label: string, A: Leg, B: Leg): string[] {
  const ds: string[] = []
  const ks = [...new Set([...Object.keys(A), ...Object.keys(B)])].sort()
  for (const k of ks) {
    const a = A[k]
    const b = B[k]
    if (!a && !b) continue
    if (!a || !b) {
      ds.push(`${label}:${k} present-on-one-side`)
      continue
    }
    if (a.status !== b.status) ds.push(`${label}:${k} status A=${a.status} B=${b.status}`)
    if (a.ct !== b.ct) ds.push(`${label}:${k} ct A='${a.ct}' B='${b.ct}'`)
    if (a.loc !== b.loc) ds.push(`${label}:${k} loc A='${a.loc}' B='${b.loc}'`)
    if (norm(a.body) !== norm(b.body)) {
      const na = norm(a.body)
      const nb = norm(b.body)
      let i = 0
      while (i < na.length && i < nb.length && na[i] === nb[i]) i++
      if (i >= Math.min(na.length, nb.length)) i = Math.min(na.length, nb.length)
      ds.push(`${label}:${k} body@${i} | A~${na.slice(Math.max(0, i - 60), i + 140).replace(/\n/g, ' ')} || B~${nb.slice(Math.max(0, i - 60), i + 140).replace(/\n/g, ' ')}`)
    }
  }
  return ds
}

// ---------- battery (oracle order) ----------
async function runLeg(): Promise<Leg> {
  flushRLim()
  wipeLibrary()
  const MU = await login(USER.email, USER.password)
  const AH = { cookie: MU.ck, 'x-csrf-token': MU.tok, 'user-agent': UA }
  const AJ = { accept: 'application/json', ...AH }

  async function call(init: { path: string; method?: string; headers?: Record<string, string> }, body?: any): Promise<{ status: number; ct: string; loc: string; body: string }> {
    const h: Record<string, string> = { ...(init.headers || {}) }
    let payload: string | undefined
    if (body !== undefined) {
      payload = typeof body === 'string' ? body : JSON.stringify(body)
      h['content-type'] = h['content-type'] || 'application/json'
    }
    const r: any = await fetch(BASE + init.path, { method: init.method || 'GET', headers: h, body: payload, redirect: 'manual' })
    const text = await r.text()
    return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], loc: r.headers.get('location') || '', body: text }
  }
  const pin = (p: { status: number; ct: string; loc: string; body: string }, html = false): Leg[string] => ({
    status: p.status,
    ct: p.ct,
    loc: p.loc,
    body: html ? norm(p.body) : p.body,
  })

  const pins: Leg = {}

  // A: authz + pages
  pins.a_anon_page = pin(await call({ path: '/library' }), true)
  pins.a_anon_list = pin(await call({ path: '/library/references' }))
  pins.a_anon_create = pin(await call({ path: '/library/references', method: 'POST' }, { entries: [] }))
  pins.a_member_page = pin(await call({ path: '/library', headers: AH }), true)
  pins.a_member_page_json = pin(await call({ path: '/library', headers: AJ }), true)
  pins.a_member_trash = pin(await call({ path: '/library/trashed', headers: AH }), true)

  // B: validation 400 battery
  const V = async (name: string, body: any) => {
    pins[name] = pin(await call({ path: '/library/references', method: 'POST', headers: AJ }, body))
  }
  await V('c_nobatch', { entries: null })
  await V('c_notarray', { entries: 'x' })
  await V('c_empty', { entries: [] })
  await V('c_notobj', { entries: [null] })
  await V('c_keymissing', { entries: [{ key: '  ', type: 'article' }] })
  await V('c_keylong', { entries: [{ key: 'a'.repeat(129), type: 'article' }] })
  await V('c_keyinvalid', { entries: [{ key: 'a b!c', type: 'article' }] })
  await V('c_typeempty', { entries: [{ key: 'ora1', type: '' }] })
  await V('c_typeunknown', { entries: [{ key: 'ora1', type: 'bogus' }] })
  await V('c_fieldsnotarr', { entries: [{ key: 'ora1', type: 'article', fields: 'x' }] })
  await V('c_fieldnotobj', { entries: [{ key: 'ora1', type: 'article', fields: [1] }] })
  await V('c_fieldname', { entries: [{ key: 'ora1', type: 'article', fields: [{ name: '9abc', value: 'v' }] }] })
  await V('c_fieldval', { entries: [{ key: 'ora1', type: 'article', fields: [{ name: 'title', value: 42 }] }] })
  await V('c_fieldlong', { entries: [{ key: 'ora1', type: 'article', fields: [{ name: 'title', value: 'x'.repeat(32769) }] }] })
  await V('c_toolong', { entries: [{ key: 'ora1', type: 'article', fields: [{ name: 'title', value: 'ok' }, { name: 'a'.repeat(65), value: 'v' }] }] })

  // C: create + list/search/count
  const e1 = {
    key: 'oracle2026a',
    type: 'Article',
    fields: [
      { name: 'title', value: '  Hello World  ' },
      { name: 'title', value: 'Last Wins Title' },
      { name: 'EMPTY', value: 'drop me' },
      { name: 'author', value: 'A and B' },
      { name: 'year', value: null },
    ],
  }
  const e2 = { key: 'oracle2026b', type: 'misc', fields: [] }
  const cr = await call({ path: '/library/references', method: 'POST', headers: AJ }, { entries: [e1, e2] })
  pins.c_ok = pin(cr)
  const citems = JSON.parse(cr.body).items
  const E1_ID = citems[0]._id
  const E2_ID = citems[1]._id

  pins.b_list = pin(await call({ path: '/library/references', headers: AJ }))
  pins.b_limit1 = pin(await call({ path: '/library/references?limit=1', headers: AJ }))
  const cursor1 = JSON.parse(pins.b_limit1.body).nextCursor
  pins.b_cursor = pin(await call({ path: `/library/references?limit=1&cursor=${cursor1}`, headers: AJ }))
  pins.b_search = pin(await call({ path: '/library/references?search=lastwins', headers: AJ }))
  pins.b_search_multi = pin(await call({ path: '/library/references?search=a and b author', headers: AJ }))
  pins.b_search_trashed = pin(await call({ path: '/library/references?search=lastwins&trashed=true', headers: AJ }))
  pins.b_count = pin(await call({ path: '/library/references/count', headers: AJ }))
  pins.b_count_search = pin(await call({ path: '/library/references/count?search=lastwins', headers: AJ }))
  pins.b_count_trashed = pin(await call({ path: '/library/references/count?trashed=true', headers: AJ }))
  pins.b_match = pin(
    await call(
      { path: '/library/references/match', method: 'POST', headers: AJ },
      { entries: [{ key: 'oracle2026a' }, { key: '  oracle2026b  ' }, { key: 'zz-none' }, { key: 123 }, null, { key: '' }] }
    )
  )
  pins.b_sugg_empty = pin(await call({ path: '/library/references/citation-key-suggestions?base=', headers: AJ }))
  pins.b_sugg_base = pin(await call({ path: '/library/references/citation-key-suggestions?base=ORACLE2026A', headers: AJ }))
  pins.b_sugg_extra = pin(await call({ path: '/library/references/citation-key-suggestions?base=oracle2026a&keys=oracle2026ab,oracle2026ac', headers: AJ }))
  pins.b_sugg_long = pin(await call({ path: '/library/references/citation-key-suggestions?base=' + encodeURIComponent('a'.repeat(100)), headers: AJ }))

  // D: download .bib (exact bytes)
  pins.b_dl_default = pin(await call({ path: '/library/references/download', headers: AJ }))
  pins.b_dl_ids = pin(await call({ path: `/library/references/download?ids=${E2_ID}`, headers: AJ }))
  pins.b_dl_ids_bad = pin(await call({ path: '/library/references/download?ids=zzz', headers: AJ }))
  pins.b_dl_search = pin(await call({ path: '/library/references/download?search=lastwins', headers: AJ }))
  pins.b_dl_excl = pin(await call({ path: '/library/references/download?mode=exclusion&search=lastwins', headers: AJ }))
  pins.b_dl_excl_nosearch = pin(await call({ path: '/library/references/download?mode=exclusion', headers: AJ }))
  const esc = await call(
    { path: '/library/references', method: 'POST', headers: AJ },
    { entries: [{ key: 'esc-test', type: 'misc', fields: [{ name: 'title', value: 'a}b {ok {unbalanced \\}end' }] }] }
  )
  const escId = JSON.parse(esc.body).items[0]._id
  pins.b_dl_escape = pin(await call({ path: `/library/references/download?ids=${escId}`, headers: AJ }))

  // E: update / rename / conflicts
  const updOkBody = { key: 'oracle2026a', type: 'book', fields: [{ name: 'title', value: 'Updated Title' }, { name: 'note', value: 'keep' }] }
  pins.u_updtype = pin(await call({ path: '/library/references/oracle2026a', method: 'PATCH', headers: AJ }, { key: 'oracle2026a', fields: updOkBody.fields }))
  pins.u_ok = pin(await call({ path: '/library/references/oracle2026a', method: 'PATCH', headers: AJ }, updOkBody))
  pins.u_renconflict = pin(await call({ path: '/library/references/oracle2026a', method: 'PATCH', headers: AJ }, { key: 'oracle2026b', type: 'book', fields: [{ name: 'title', value: 'x' }] }))
  pins.u_rename = pin(await call({ path: '/library/references/oracle2026a', method: 'PATCH', headers: AJ }, { key: 'renamed2026a', type: 'book', fields: updOkBody.fields }))
  pins.u_notfound = pin(await call({ path: '/library/references/no-such-key', method: 'PATCH', headers: AJ }, { key: 'no-such-key', type: 'book', fields: [] }))
  pins.u_invalid = pin(await call({ path: '/library/references/renamed2026a', method: 'PATCH', headers: AJ }, { key: 'bad key!', type: 'book', fields: [] }))

  // F: delete / restore / trash lifecycle
  pins.d_nothing = pin(await call({ path: '/library/references/delete', method: 'POST', headers: AJ }, {}))
  pins.d_ids = pin(await call({ path: '/library/references/delete', method: 'POST', headers: AJ }, { ids: [E2_ID] }))
  pins.d_ids_bad = pin(await call({ path: '/library/references/delete', method: 'POST', headers: AJ }, { ids: ['zzz'] }))
  pins.d_trash_list = pin(await call({ path: '/library/references?trashed=true', headers: AJ }))
  pins.d_search = pin(await call({ path: '/library/references/delete', method: 'POST', headers: AJ }, { search: 'renamed2026a updated' }))
  const t1 = await call({ path: '/library/references', method: 'POST', headers: AJ }, { entries: [{ key: 'perma-x', type: 'misc', fields: [] }] })
  const tid1 = JSON.parse(t1.body).items[0]._id
  pins.d_perma = pin(await call({ path: '/library/references/delete', method: 'POST', headers: AJ }, { ids: [tid1], permanent: true }))
  pins.d_perma_gone = pin(await call({ path: '/library/references?search=perma', headers: AJ }))
  pins.d_perma_goneT = pin(await call({ path: '/library/references?search=perma&trashed=true', headers: AJ }))

  const t2 = await call({ path: '/library/references', method: 'POST', headers: AJ }, { entries: [{ key: 'rest-x', type: 'misc', fields: [] }] })
  const tid2 = JSON.parse(t2.body).items[0]._id
  await call({ path: '/library/references/delete', method: 'POST', headers: AJ }, { ids: [tid2] })
  pins.r_ids = pin(await call({ path: '/library/references/restore', method: 'POST', headers: AJ }, { ids: [tid2] }))
  pins.r_ids_now = pin(await call({ path: '/library/references?search=rest', headers: AJ }))
  pins.r_bad = pin(await call({ path: '/library/references/restore', method: 'POST', headers: AJ }, { ids: ['zzz', '123'] }))

  // Deterministic end-state (rest-x active, renamed2026a trashed, the rest
  // gone): compare as pins so state drift is caught on both legs.
  pins.state_active = pin(await call({ path: '/library/references?limit=100', headers: AJ }))
  pins.state_trash = pin(await call({ path: '/library/references?trashed=true&limit=100', headers: AJ }))

  // cleanup: wipe the battery's remaining docs (stack-agnostic).
  wipeLibrary()
  return pins
}

async function login(email: string, pw: string): Promise<{ ck: string; tok: string }> {
  const r0: any = await fetch(BASE + '/login', { headers: { 'user-agent': UA }, redirect: 'manual' })
  const html = await r0.text()
  const csrf = (html.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1: any = await fetch(BASE + '/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json', cookie: ck0, 'user-agent': UA },
    body: JSON.stringify({ email, password: pw }),
    redirect: 'manual',
  })
  const body1 = await r1.text()
  if (r1.status !== 200) throw new Error('login failed ' + r1.status + ' ' + body1.slice(0, 200))
  const ck = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
  const cs: any = await fetch(BASE + '/dev/csrf', { headers: { cookie: ck, 'user-agent': UA } })
  const tok = (await cs.text()).trim()
  return { ck, tok }
}

// ---------- gate legs ----------
let leg1: Leg | null = null
let leg2: Leg | null = null
let leg3: Leg | null = null

test('leg 0: flip off before start', async () => {
  for (const conf of FLIPCONFS) expect(flipCount(conf)).toBe(0)
}, 60_000)

test('leg 1: Node baseline', async () => {
  leg1 = await runLeg()
  expect(Object.keys(leg1).length).toBeGreaterThanOrEqual(57)
  expect(libraryCount()).toBe(0)
}, 300_000)

test('leg 2: Go parity (flip on)', async () => {
  await flip('apply')
  await waitGo()
  leg2 = await runLeg()
  const ds = diffLegs('go', leg1!, leg2!)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 4000))
  expect(libraryCount()).toBe(0)
}, 300_000)

test('leg 3: Node re-baseline', async () => {
  await flip('strip')
  leg3 = await runLeg()
  const ds = diffLegs('node-determinism', leg1!, leg3!)
  if (ds.length) throw new Error(ds.join('\n').slice(0, 4000))
  expect(libraryCount()).toBe(0)
}, 300_000)

test('pin sanity: anchors hold (Node baseline)', async () => {
  const L = leg1!
  expect(L.a_anon_page.loc).toBe('/login')
  expect(L.a_anon_create.status).toBe(403)
  expect(L.a_anon_create.body).toBe('Forbidden')
  expect(L.a_member_page.status).toBe(200)
  expect(L.a_member_page.body).toContain('ol-libraryView" content="library"')
  expect(L.a_member_trash.body).toContain('ol-libraryView" content="trash"')
  expect(L.a_member_page.body).toContain('/js/modules/bib-editor/pages/library-')
  expect(JSON.parse(L.c_keymissing.body).message).toBe('A citation key is required.')
  expect(JSON.parse(L.c_typeunknown.body).message).toBe('Unknown entry type.')
  expect(L.c_ok.status).toBe(201)
  expect(JSON.parse(L.c_ok.body).items[0].fields).toEqual([
    { name: 'title', value: 'Last Wins Title' },
    { name: 'empty', value: 'drop me' },
    { name: 'author', value: 'A and B' },
  ])
  expect(JSON.parse(L.b_count.body).count).toBe(2)
  expect(JSON.parse(L.b_limit1.body).items.length).toBe(1)
  expect(L.b_limit1.body).toMatch(/"nextCursor":"[0-9a-f]{24}"/)
  expect(JSON.parse(L.b_cursor.body).items.length).toBe(1)
  expect(JSON.parse(L.b_cursor.body).items[0].key).toBe('oracle2026b')
  expect(JSON.parse(L.b_sugg_base.body).keys.length).toBe(10)
  expect(JSON.parse(L.u_renconflict.body).duplicateKey).toBe('oracle2026b')
  expect(L.u_renconflict.status).toBe(409)
  expect(JSON.parse(L.d_nothing.body).message).toBe('Provide ids or a search term to delete.')
  expect(JSON.parse(L.d_ids_bad.body).deletedCount).toBe(0)
  expect(JSON.parse(L.r_bad.body).restoredCount).toBe(0)
  expect(L.b_dl_default.ct).toBe('text/plain')
  expect(L.b_dl_default.body).toContain('@article{oracle2026a,')
  expect(L.b_dl_escape.body).toContain('a\\}b {ok {unbalanced \\}end}')
  expect(L.b_dl_search.body).toBe('')
  // exclusion + non-matching search → nothing excluded → everything downloaded
  expect(L.b_dl_excl.body).toContain('oracle2026a')
  expect(L.b_dl_excl.body).toContain('oracle2026b')
  expect(L.d_perma_gone.body).toContain('"items":[]')
  expect(L.d_perma_goneT.body).toContain('"items":[]')
  expect(L.state_active.body).toContain('"key":"rest-x"')
  expect(L.state_active.body).not.toContain('"key":"perma-x"')
  expect(L.state_trash.body).toContain('"key":"renamed2026a"')
})

test.afterAll(async () => {
  try {
    for (const conf of FLIPCONFS) if (flipCount(conf) > 0) await flip('strip')
  } catch {
    /* best effort */
  }
  try {
    wipeLibrary()
  } catch {
    /* best effort */
  }
})
