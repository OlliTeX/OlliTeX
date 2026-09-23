/**
 * U10.3 LinkedFiles wire matrix (WEB_GO_PLAN.md U10.3) — dual-port
 * in-container battery against one seeded fixture project:
 *
 *   POST /project/:pid/linked_file
 *   POST /project/:pid/linked_file/:fid/refresh
 *
 * CE stack: all linked-file agents are disabled (hasLinkUrlFeature etc.)
 * so EVERY branch is read-only: validation (400/404), authorization
 * (401/403), entity lookup (404 page / 409), agent-null (400). No Mongo
 * / DU / docstore / realtime effects — the gate additionally asserts the
 * project doc is byte-identical before/after all legs.
 *
 * Node oracle (pinned live 2026-09-22):
 *   - parseReq FIRST: params.project_id invalid -> 404 {error:"Validation
 *     error: Invalid Mongo ObjectId at \"params.project_id\""} AND
 *     short-circuits (file_id + body not inspected: bothbad -> pid only)
 *   - then authorization: ghost/valid pid w/o membership -> 403
 *     {"message":"restricted"}; anon no-csrf -> 403 text "Forbidden";
 *     anon w/ csrf + json accept -> 401 "Unauthorized"
 *   - body (zod strict, ALL issues joined "; ", any params issue -> 404):
 *       create:  discriminator first (invalid/missing provider -> bare
 *                "Validation error"), then name, parent_folder_id, data,
 *                data required (schema order), data unknown (combined
 *                "Unrecognized key(s): a, b at \"body.data\"" client
 *                order), body unknown (combined, last)
 *       refresh: file_id issue first, then clientId,
 *                shouldReindexReferences, body unknown (combined)
 *   - refresh lookup: file (type:'file') missing incl. doc-ids -> 404
 *     PAGE; linkedFileData null or provider missing -> 409 bare;
 *     provider present -> _getAgent null (CE) -> 400 bare
 *   - create valid shape -> agent null (CE) -> 400 bare
 *   - 429: shared redis limiters create-linked-file /
 *     refresh-linked-file 100/60s — battery stays far under the cap
 *     (deterministic; the 429 branch is pinned per-route in the Go unit
 *     surface instead).
 *
 * Output: one line per case  label|status|hdr§...|body
 * (full body — including 404 pages — so the gate can byte-compare; hdr =
 * content-type, x-powered-by, etag(hash-normalized), set-cookie(value-
 * normalized), §-joined).
 */
const LEG = Number(process.argv[2] || 1)
const BASE = LEG === 2 ? 'http://127.0.0.1:4010' : 'http://127.0.0.1:4000'

// Seed id contract (spec writes this project + fileRefs):
const PJ = process.env.LF_PJ || 'aaaa000000000000000000a1'
const DOC = process.env.LF_DOC || 'aaaa000000000000000000a2'
const F_PLAIN = process.env.LF_FPLAIN || 'aaaa000000000000000000a3'
const F_NOPROV = process.env.LF_FNOPROV || 'aaaa000000000000000000a4'
const F_URL = process.env.LF_FURL || 'aaaa000000000000000000a5'
const GHOST_F = process.env.LF_GHOSTF || 'aaaa000000000000000000b9'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function anonCookie() {
  const r = await fetch(BASE + '/login', { headers: { accept: 'text/html' }, redirect: 'manual' })
  const t = await r.text()
  const csrf = (t.match(/ol-csrfToken" content="([^"]+)"/) || [])[1] || ''
  const sid = ((r.headers.getSetCookie && r.headers.getSetCookie() || [])[0] || '').split(';')[0]
  return { sid, csrf }
}

async function login(email, password, sid, csrf) {
  const l1 = await fetch(BASE + '/login', {
    method: 'POST',
    headers: { 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': csrf, cookie: sid },
    body: JSON.stringify({ email, password }),
    redirect: 'manual',
  })
  const j = await l1.json().catch(() => ({}))
  const nsid = ((l1.headers.getSetCookie && l1.headers.getSetCookie() || [])[0] || '').split(';')[0]
  const session = nsid || sid
  const hub = await fetch(BASE + '/hub', { headers: { accept: 'text/html', cookie: session } })
  const token = ((await hub.text()).match(/ol-csrfToken" content="([^"]+)"/) || [])[1] || csrf
  return { session, token }
}

const CASES = []
async function add(label, run) {
  CASES.push({ label, run })
}

async function main() {
  const out = []
  // ETag W/"<hexlen>-<bodyhash>": the hash is body-derived. JSON/plain
  // bodies are deterministic (must match), 404-page bodies carry a fresh
  // per-request nonce (hash legitimately differs) — normalize the hash
  // part, keep the length (a real parity signal).
  const etagN = (v) => (v || '').replace(/W\/"([0-9a-f]+)-[^"]*"/g, 'W/"$1-H"')
  const rec = (label, r) => {
    // headers compared: content-type, x-powered-by, etag (hash-normalized),
    // set-cookie (value-normalized). ETag is body-derived; Content-Length
    // follows the body — both are implied by the body compare.
    const sc = r.headers.getSetCookie ? r.headers.getSetCookie() : []
    const scN = sc.map((c) => c.split(';').map((p) => { const t = p.trim(); const i = t.indexOf('='); return i === -1 ? t : t.slice(0, i + 1) + 'X' }).join(';')).join(' | ')
    const hdr = [
      r.headers.get('content-type') || '',
      r.headers.get('x-powered-by') || '',
      etagN(r.headers.get('etag')),             // etag
      scN,                                      // set-cookie
      r.headers.get('location') || '',          // 302 target
    ].join('§')
    out.push(`${label}|${r.status}|${hdr}|${r._body}`)
  }
  const post = async (session, token, path, body, extraHeaders) => {
    const h = { ...(extraHeaders || {}) }
    if (session) h.cookie = session
    if (token) h['x-csrf-token'] = token
    if (body !== undefined) h['content-type'] = 'application/json'
    h.accept = 'application/json, text/plain, */*'
    const r = await fetch(BASE + path, {
      method: 'POST',
      headers: h,
      body: body === undefined ? undefined : typeof body === 'string' ? body : JSON.stringify(body),
      redirect: 'manual',
    })
    r._body = await r.text()
    return r
  }
  const get = async (session, path, extraHeaders) => {
    const h = { ...(extraHeaders || {}) }
    if (session) h.cookie = session
    h.accept = 'text/html'
    const r = await fetch(BASE + path, { headers: h, redirect: 'manual' })
    r._body = await r.text()
    return r
  }

  const { sid: anonSid, csrf } = await anonCookie()
  const owner = await login('e2e-user@e2e.test', 'Ol-Fixture-3m2Q', anonSid, csrf)
  await sleep(150)
  const nm = await login('e2e-tpladmin@e2e.test', 'Ol-Fixture-7tW4', anonSid, csrf)
  await sleep(150)

  const CR = `/project/${PJ}/linked_file`
  const RF = (f) => `/project/${PJ}/linked_file/${f}/refresh`
  const validCreate = {
    provider: 'url', name: 'f.pdf', parent_folder_id: PJ, data: { url: 'http://x' },
  }

  // ---------------- auth surface (both routes) ----------------
  // Node/Go oracle: no valid csrf -> 403 text "Forbidden" (regardless of
  // cookie presence); valid session csrf + json accept -> 401 text
  // "Unauthorized" (requireLogin); html accept -> 302 /login.
  add('anon-create-nocsrf', async () =>
    post(null, null, CR, validCreate, { accept: 'application/json, text/plain, */*' }))
  add('anon-create-csrf-401', async () => post(anonSid, csrf, CR, validCreate))
  add('anon-create-csrf-html', async () =>
    post(anonSid, csrf, CR, validCreate, { accept: 'text/html' }))
  add('anon-refresh-nocsrf', async () =>
    post(null, null, RF(F_PLAIN), {}, { accept: 'application/json, text/plain, */*' }))
  add('anon-refresh-csrf-401', async () => post(anonSid, csrf, RF(F_PLAIN), {}))

  // ---------------- create: shape + validation ----------------
  add('create-valid', async () => post(owner.session, owner.token, CR, validCreate))
  add('create-badprov', async () => post(owner.session, owner.token, CR, { ...validCreate, provider: 'ghost' }))
  add('create-noprov-badname', async () =>
    post(owner.session, owner.token, CR, { name: 5, parent_folder_id: PJ, data: { url: 'u' } }))
  add('create-missname', async () =>
    post(owner.session, owner.token, CR, { provider: 'url', parent_folder_id: PJ, data: { url: 'u' } }))
  add('create-badname', async () =>
    post(owner.session, owner.token, CR, { provider: 'url', name: 5, parent_folder_id: PJ, data: { url: 'u' } }))
  add('create-badpf', async () => post(owner.session, owner.token, CR, { ...validCreate, parent_folder_id: 'bad' }))
  add('create-nodata', async () =>
    post(owner.session, owner.token, CR, { provider: 'url', name: 'x', parent_folder_id: PJ }))
  add('create-data-unk1', async () =>
    post(owner.session, owner.token, CR, { provider: 'url', name: 'x', parent_folder_id: PJ, data: { url: 'u', zz: 1 } }))
  add('create-data-unk2', async () =>
    post(owner.session, owner.token, CR, { provider: 'url', name: 'x', parent_folder_id: PJ, data: { url: 'u', zz: 1, aa: 2 } }))
  add('create-body-unk1', async () => post(owner.session, owner.token, CR, { ...validCreate, zz: 1 }))
  add('create-body-unk2', async () => post(owner.session, owner.token, CR, { ...validCreate, zz: 1, aa: 2 }))
  add('create-multi', async () =>
    post(owner.session, owner.token, CR, { provider: 'url', name: 5, parent_folder_id: 'bad', data: { zz: 1 }, zz: 9 }))
  add('create-bodydata-unk', async () =>
    post(owner.session, owner.token, CR, { provider: 'url', name: 'x', parent_folder_id: PJ, data: { url: 'u', dd: 1 }, zz: 1 }))
  add('create-zot-bad', async () =>
    post(owner.session, owner.token, CR, { provider: 'zotero', name: 5, parent_folder_id: 'bad', data: { zz: 1 } }))
  add('create-pof-missing', async () =>
    post(owner.session, owner.token, CR, { provider: 'project_output_file', name: 'x', parent_folder_id: PJ, data: { zz: 1 } }))
  add('create-data-type', async () =>
    post(owner.session, owner.token, CR, { provider: 'url', name: 'x', parent_folder_id: PJ, data: [1, 2] }))

  // ---------------- create: params + ghost project ----------------
  add('create-badpid', async () =>
    post(owner.session, owner.token, '/project/not-oid/linked_file', validCreate))
  add('create-ghostpid', async () =>
    post(owner.session, owner.token, '/project/507070707070707070707070/linked_file', validCreate))

  // ---------------- refresh: params ----------------
  add('refresh-badpid', async () =>
    post(owner.session, owner.token, `/project/not-oid/linked_file/${F_PLAIN}/refresh`, {}))
  add('refresh-badfid', async () =>
    post(owner.session, owner.token, RF('not-oid'), {}))
  add('refresh-bothbad', async () =>
    post(owner.session, owner.token, '/project/bad/linked_file/not-oid/refresh', {}))
  add('refresh-badpid-extrak', async () =>
    post(owner.session, owner.token, '/project/bad/linked_file/507070707070707070707070/refresh', { zz: 1, aa: 2 }))
  add('refresh-ghostpid', async () =>
    post(owner.session, owner.token, '/project/507070707070707070707070/linked_file/507070707070707070707070/refresh', { clientId: 'x' }))

  // ---------------- refresh: body validation (combined) ----------------
  add('refresh-badclient', async () => post(owner.session, owner.token, RF(F_PLAIN), { clientId: 42 }))
  add('refresh-badbool', async () =>
    post(owner.session, owner.token, RF(F_PLAIN), { shouldReindexReferences: 'x' }))
  add('refresh-unk1', async () => post(owner.session, owner.token, RF(F_PLAIN), { zz: 1 }))
  add('refresh-unk3-order', async () =>
    post(owner.session, owner.token, RF(F_PLAIN), { zebra: 1, alpha: 2, mmm: 3 }))
  add('refresh-all3', async () =>
    post(owner.session, owner.token, RF('not-oid'), { clientId: 42, shouldReindexReferences: 'x', zz: 1 }))
  add('refresh-badbool+unk', async () =>
    post(owner.session, owner.token, RF(F_PLAIN), { shouldReindexReferences: 1, zz: 1 }))

  // ---------------- refresh: lookup + CE terminal branches ----------------
  add('refresh-plain-409', async () => post(owner.session, owner.token, RF(F_PLAIN), {}))
  add('refresh-noprov-409', async () => post(owner.session, owner.token, RF(F_NOPROV), {}))
  add('refresh-urlprov-400', async () => post(owner.session, owner.token, RF(F_URL), {}))
  add('refresh-urlprov-body-ok', async () =>
    post(owner.session, owner.token, RF(F_URL), { clientId: 'c1', shouldReindexReferences: true }))
  add('refresh-docid-404', async () => post(owner.session, owner.token, RF(DOC), {}))
  add('refresh-ghostfile-404', async () => post(owner.session, owner.token, RF(GHOST_F), {}))

  // ---------------- non-member ----------------
  add('nm-create-valid', async () => post(nm.session, nm.token, CR, validCreate))
  add('nm-refresh-plain', async () => post(nm.session, nm.token, RF(F_PLAIN), {}))
  add('nm-refresh-badfid', async () => post(nm.session, nm.token, RF('not-oid'), { zz: 1 }))
  add('nm-create-badpid', async () =>
    post(nm.session, nm.token, '/project/not-oid/linked_file', validCreate))

  // ---------------- U10.3r: /read-only/one-time-login ----------------
  // Node: NO auth middleware (login-whitelist route) — anonymous AND
  // logged-in both get the 200 page (navbar/email/uid branch on session).
  add('otl-anon', async () => get(null, '/read-only/one-time-login'))
  add('otl-owner', async () => get(owner.session, '/read-only/one-time-login'))
  add('otl-nm', async () => get(nm.session, '/read-only/one-time-login'))

  for (const c of CASES) {
    const r = await c.run()
    rec(c.label, r)
    await sleep(120)
  }
  console.log(out.join('\n'))
}

main().catch((e) => {
  console.error('ERR', e.message)
  process.exit(1)
})
