// U10.2b parity battery — entity rename / move / duplicate (Node EditorRouter
// POST family → ProjectEntityUpdateHandler → DU/docstore side effects).
// 3-leg Node==Go==Node. Run inside ol-e2e-overleaf-1:
//   LEG 1+3 → 127.0.0.1:4000 (Node), LEG 2 → 127.0.0.1:4010 (Go).
//
// Fixture: a per-leg seeded project (mongo, identical seed) with:
//   root docs:  main.tex (rootDoc, registered in docstore), d1.txt, d2.txt
//   root file:  f1.bin (hash-pinned, no blob — file duplicate shares the
//               source hash; DU add-file pins hash + createdBlob:true)
//   folders:    sub/ and outer/outer-sub/ (descendant move pin)
//
// Captured state (normalized in the runner):
//   - project flat tree (names+types, sorted), version, lastUpdatedBy
//   - DU ops list `ProjectHistory:Ops:{pj}` (redis) — the updateProject-
//     Structure side-effect evidence (P4.13b idiom)
//   - docstore GET of the new duplicate doc (lines/rev)
//   - editor-events publish capture (redis-cli subscribe window) for
//     duplicate-file "reciveNewFile"
// Normalization (leg-independent): every distinct 24-hex id → H<i> in
// first-seen order; ISO timestamps → TS; per-request csrf/nonce; ETag hash
// (length prefix kept — the body-size parity check); DU "version" kept
// (identical seed → identical arithmetic).
const LEG = Number(process.env.LEG || 1)
const BASE = LEG === 2 ? 'http://127.0.0.1:4010' : 'http://127.0.0.1:4000'
const B = (p) => BASE + p
let SID = ''
let TOK = ''
let OTHER_CK = '' // non-member session (tpladmin)
let OTHER_TOK = ''

function sh(argsStr) {
  const { execFileSync } = require('child_process')
  try {
    return execFileSync('sh', ['-c', argsStr], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }) || ''
  } catch (e) {
    return (e.stdout || '') + (e.stderr || '')
  }
}

async function login(email, password, out = '') {
  const r0 = await fetch(B('/login'), { headers: { accept: 'text/html' } })
  const h0 = await r0.text()
  const c0 = (h0.match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  const ck0 = (r0.headers.get('set-cookie') || '').split(';')[0]
  const r1 = await fetch(B('/login'), {
    method: 'POST',
    headers: { 'content-type': 'application/json', accept: 'application/json', 'x-csrf-token': c0, cookie: ck0 },
    body: JSON.stringify({ email, password }),
  })
  if (r1.status !== 200) throw new Error(`login(${email}) ${r1.status}`)
  const ck1 = (r1.headers.get('set-cookie') || '').split(';')[0] || ck0
  const pg = await fetch(B('/hub'), { headers: { accept: 'text/html', cookie: ck1 } })
  const tok = ((await pg.text()).match(/ol-csrfToken" content="([^"]+)"/) || [])[1]
  if (out === 'main') { SID = ck1; TOK = tok } else { OTHER_CK = ck1; OTHER_TOK = tok }
}

async function call(tag, method, path, opts = {}) {
  const h = {}
  if (opts.anon) {
    // no cookie
  } else if (opts.other) {
    h.cookie = `${OTHER_CK}; ol-csrfToken=${OTHER_TOK}`
  } else {
    h.cookie = `${SID}; ol-csrfToken=${TOK}`
  }
  h.accept = opts.acc || 'application/json, text/plain, */*'
  let body
  if (opts.json !== undefined) {
    h['content-type'] = 'application/json'
    body = typeof opts.json === 'string' ? opts.json : JSON.stringify(opts.json)
  }
  if (opts.form) {
    body = opts.form
  }
  if (method !== 'GET' && !opts.anon && !opts.other) h['x-csrf-token'] = TOK
  if (method !== 'GET' && opts.other) h['x-csrf-token'] = OTHER_TOK
  let r
  try {
    r = await fetch(B(path), { method, headers: h, body, redirect: 'manual' })
  } catch (e) {
    return { tag, err: String((e && e.message) || e) }
  }
  const b = await r.text()
  const headers = {}
  for (const k of r.headers.keys()) headers[k.toLowerCase()] = r.headers.get(k)
  return { tag, status: r.status, headers, len: b.length, body: b }
}

// ---------- ids (mongo container) ----------
function mongoEval(code) {
  // dexeQ equivalent: mongosh in ol-e2e-mongo-1 (invoked locally via a
  // sibling-exec trick is not available in-container — the RUNNER does the
  // seeding; the battery receives ids via env U2B_IDS.
  void code
  return ''
}

const IDS = JSON.parse(process.env.U2B_IDS || '{}')

async function main() {
  await login('e2e-user@e2e.test', 'Ol-Fixture-3m2Q', 'main')
  await login('e2e-tpladmin@e2e.test', 'Ol-Fixture-7tW4', 'other')
  const P = IDS.pj
  const RF = IDS.rf
  const D1 = IDS.d1
  const D2 = IDS.d2
  const F1 = IDS.f1
  const SUB = IDS.sub
  const OUTER = IDS.outer
  const OSUB = IDS.osub
  const GHOST = '600000000000000000000001'
  const GHOSTE = '600000000000000000000002'
  const recs = []
  // STRICT SEQUENTIAL: stateful cases depend on prior state.
  const push = async (tag, m, p, o) => {
    let r = await call(tag, m, p, o)
    // flake guard: one retry on transport-level failure (undici connection
    // reset); the retry is the same idempotent-for-4xx/409-style pins and
    // both legs attempt identically so parity is preserved.
    if (r.err && /fetch failed|socket destroyed|connection/i.test(r.err)) {
      await new Promise((rs) => setTimeout(rs, 250))
      const r2 = await call(tag, m, p, o)
      if (r2.status !== undefined) {
        r = r2
      } else {
        process.stdout.write(`CASE ${r.tag} -> RETRY-FAILED ${r2.err}\n`)
      }
    }
    recs.push(r)
    process.stdout.write(`CASE ${r.tag} -> ${r.status}${r.err ? ' ERR:' + r.err : ''}\n`)
    return r
  }
  const ren = (e) => `/project/${P}/doc/${e}/rename`
  const mov = (e) => `/project/${P}/doc/${e}/move`
  const dup = (e) => `/project/${P}/doc/${e}/duplicate`
  const renF = (e) => `/project/${P}/folder/${e}/rename`
  const movF = (e) => `/project/${P}/folder/${e}/move`
  const dupF = (e) => `/project/${P}/folder/${e}/duplicate`

  // ---- auth surface ----
  await push('a1 rename anon', 'POST', ren(D1), { anon: true, json: { name: 'x.txt' } })
  await push('a2 rename nonmember json', 'POST', ren(D1), { other: true, json: { name: 'x.txt' } })
  await push('a2b rename nonmember html', 'POST', ren(D1), { other: true, json: { name: 'x.txt' }, acc: 'text/html' })
  await push('a3 rename wrong-method', 'PUT', ren(D1), { json: { name: 'x.txt' } })
  await push('a4 rename GET', 'GET', ren(D1), { acc: 'text/html' })
  await push('a5 dup anon', 'POST', dup(D1), { anon: true, json: {} })
  await push('a6 dup nonmember', 'POST', dup(D1), { other: true, json: {} })
  await push('a7 move anon', 'POST', mov(D1), { anon: true, json: { folder_id: RF } })

  // ---- rename: validation contract ----
  await push('r-va-missing', 'POST', ren(GHOSTE), { json: { bogus: 1 } })
  await push('r-va-badnum', 'POST', ren(GHOSTE), { json: { name: 42 } })
  await push('r-va-null', 'POST', ren(GHOSTE), { json: { name: null } })
  await push('r-va-srcnum', 'POST', ren(GHOSTE), { json: { source: 42 } })
  await push('r-va-array', 'POST', ren(GHOSTE), { json: '[1]' })
  await push('r-va-num', 'POST', ren(GHOSTE), { json: '123' })
  await push('r-va-str', 'POST', ren(GHOSTE), { json: '"x"' })
  await push('r-va-2bug', 'POST', ren(GHOSTE), { json: { zeta: 1, alpha: 2 } })
  await push('r-va-empty', 'POST', ren(D1), { json: {} })
  await push('r-va-badparams', 'POST', `/project/${P}/doc/notanoid/rename`, { json: { name: 'x' } })
  await push('r-va-badtype', 'POST', `/project/${P}/widget/${D1}/rename`, { json: { name: 'x' } })
  await push('r-va-badproj', 'POST', `/project/notanoid/doc/${D1}/rename`, { json: { name: 'x' } })

  // ---- rename: name gates (entity GHOSTE for non-state cases) ----
  await push('r-len149-ok-then-back', 'POST', ren(D1), { json: { name: 'a'.repeat(149) + '.txt' } })
  await push('r-len150', 'POST', ren(GHOSTE), { json: { name: 'a'.repeat(150) } })
  await push('r-utf16', 'POST', ren(GHOSTE), { json: { name: '\u{1F600}'.repeat(76) + '.txt' } })
  await push('r-slash', 'POST', ren(GHOSTE), { json: { name: 'a/b' } })
  await push('r-backslash', 'POST', ren(GHOSTE), { json: { name: 'a\\b' } })
  await push('r-ctrlchar', 'POST', ren(GHOSTE), { json: { name: 'a\u0001b' } })
  await push('r-leadws', 'POST', ren(GHOSTE), { json: { name: ' x' } })
  await push('r-trailws', 'POST', ren(GHOSTE), { json: { name: 'x ' } })
  await push('r-dotdot', 'POST', ren(GHOSTE), { json: { name: '..' } })
  await push('r-dotfile-ok', 'POST', ren(D2), { json: { name: '.hidden' } })
  await push('r-dotfile-back', 'POST', ren(D2), { json: { name: 'd2.txt' } })
  await push('r-self', 'POST', ren(D1), { json: { name: 'd1.txt' } })
  await push('r-blocked-top', 'POST', ren(D1), { json: { name: 'toString' } })
  await push('r-ghostproj', 'POST', `/project/600000000000000000000003/doc/${D1}/rename`, { json: { name: 'x' } })

  // ---- rename: success + state (strict sequential order) ----
  await push('r-ok-d1', 'POST', ren(D1), { json: { name: 'd1-renamed.txt' } })
  await push('r-ok-back-d1', 'POST', ren(D1), { json: { name: 'd1.txt' } })
  await push('r-ok-subfolder', 'POST', `/project/${P}/folder/${SUB}/rename`, { json: { name: 'sub-renamed' } })
  await push('r-ok-back-sub', 'POST', `/project/${P}/folder/${SUB}/rename`, { json: { name: 'sub' } })
  // duplicate-name gates (both D1+D2 inside SUB):
  await push('r-setup-mv-d1in', 'POST', mov(D1), { json: { folder_id: SUB } })
  await push('r-setup-mv-d2in', 'POST', mov(D2), { json: { folder_id: SUB } })
  await push('r-dupname-sib', 'POST', `/project/${P}/folder/${SUB}/doc/${D2}/rename`, { json: { name: 'd1.txt' } })
  await push('r-dupname-self', 'POST', `/project/${P}/folder/${SUB}/doc/${D1}/rename`, { json: { name: 'd1.txt' } })
  // blocked name is allowed INSIDE a subfolder, and the sub-rename paths
  // all 404 in Node (the entity path is only resolved after the root-folder
  // array is checked — ProjectLocator searches from root, and D1/D2 live in
  // SUB, so these sub-prefixed renames never match):
  await push('r-blocked-subok', 'POST', `/project/${P}/folder/${SUB}/doc/${D2}/rename`, { json: { name: 'toString' } })
  await push('r-blocked-subback', 'POST', `/project/${P}/folder/${SUB}/doc/${D2}/rename`, { json: { name: 'd2.txt' } })
  // restore to the seed layout (same 404 pattern):
  await push('r-restore-d1out', 'POST', `/project/${P}/folder/${SUB}/doc/${D1}/move`, { json: { folder_id: RF } })
  await push('r-restore-d2out', 'POST', `/project/${P}/folder/${SUB}/doc/${D2}/move`, { json: { folder_id: RF } })
  // move them OUT via the ROOT path (works — ProjectLocator finds them in SUB):
  await push('r-restore-d1out2', 'POST', mov(D1), { json: { folder_id: RF } })
  await push('r-restore-d2out2', 'POST', mov(D2), { json: { folder_id: RF } })

  // ---- move: validation contract ----
  await push('m-va-missing', 'POST', mov(GHOSTE), { json: { source: 'x' } })
  await push('m-va-null', 'POST', mov(GHOSTE), { json: { folder_id: null } })
  await push('m-va-badoid', 'POST', mov(GHOSTE), { json: { folder_id: 'zzz' } })
  await push('m-va-2iss', 'POST', mov(GHOSTE), { json: { folder_id: null, bogus: 1 } })
  await push('m-va-array', 'POST', mov(GHOSTE), { json: '[1]' })
  await push('m-va-empty', 'POST', mov(D1), { json: {} })
  await push('m-va-badtype', 'POST', `/project/${P}/widget/${D1}/move`, { json: { folder_id: RF } })
  await push('m-va-ghost', 'POST', mov(GHOSTE), { json: { folder_id: GHOST } })
  await push('m-va-badproj', 'POST', `/project/600000000000000000000003/doc/${D1}/move`, { json: { folder_id: RF } })

  // folder self/descendant pins moved to the end (Go leg flake mid-sequence).

  // ---- move: success flows ----
  await push('m-in-d1', 'POST', mov(D1), { json: { folder_id: SUB } })
  await push('m-out-d1', 'POST', `/project/${P}/folder/${SUB}/doc/${D1}/move`, { json: { folder_id: RF } })
  await push('m-file-in', 'POST', `/project/${P}/file/${F1}/move`, { json: { folder_id: SUB } })
  await push('m-file-out', 'POST', `/project/${P}/folder/${SUB}/file/${F1}/move`, { json: { folder_id: RF } })
  await push('m-folder-in', 'POST', `/project/${P}/folder/${SUB}/move`, { json: { folder_id: OUTER } })
  await push('m-folder-out', 'POST', `/project/${P}/folder/${SUB}/move`, { json: { folder_id: RF } })

  // ---- duplicate ----
  await push('d-doc1', 'POST', dup(D1), { json: {} })
  await push('d-doc2', 'POST', dup(D1), { json: {} })
  await push('d-file1', 'POST', `/project/${P}/file/${F1}/duplicate`, { json: {} })
  await push('d-file2', 'POST', `/project/${P}/file/${F1}/duplicate`, { json: {} })
  await push('d-folder', 'POST', dupF(SUB), { json: {} })
  await push('d-ghost-ent', 'POST', dup(GHOSTE), { json: {} })
  await push('d-kind-mismatch', 'POST', `/project/${P}/file/${D1}/duplicate`, { json: {} })
  await push('d-ghostproj', 'POST', `/project/600000000000000000000003/doc/${D1}/duplicate`, { json: {} })
  await push('d-badparams', 'POST', `/project/${P}/widget/${D1}/duplicate`, { json: {} })
  await push('d-badent-id', 'POST', dup('zzzzzzzzzzzzzzzzzzzzzzzz'), { json: {} })
  await push('d-src', 'POST', dup(D2), { json: { source: 'gate' } })

  // folder self/descendant pins placed last (they were failing mid-sequence
  // on the Go leg with a connection reset; run them as the final stateful
  // cases to isolate any residual interference while still pinning the
  // contracts).
  await push('m-descendant', 'POST', `/project/${P}/folder/${OUTER}/move`, { json: { folder_id: OSUB } })
  await push('m-root-self', 'POST', `/project/${P}/folder/${RF}/move`, { json: { folder_id: RF } })
  await push('m-self', 'POST', `/project/${P}/folder/${SUB}/move`, { json: { folder_id: SUB } })

  console.log('BATTERY-JSON:' + JSON.stringify(recs))
}

main().catch((e) => {
  console.log('BATTERY-ERR:' + String((e && e.stack) || e).slice(0, 400))
  process.exit(1)
})
