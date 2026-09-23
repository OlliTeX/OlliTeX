/**
 * U-API doc-trio parity matrix — the private-API (basic-auth) document trio,
 * API profile (ENABLED_SERVICES=api).
 *
 * This is the API-profile oracle (Node api :3000), distinct from the WEB
 * profile oracle (Node web :4000, pinned by u103r-matrix.cjs). The two wires
 * differ (WEB: unauth json→401 / html→302, POST→403, ghost→404 HTML page;
 * API: unauth→401 for ANY Accept, POST→401, ghost→404 plain "Not Found",
 * X-Powered-By present, no session cookie, no helmet baseline).
 *
 * Node api :3000 surface (pinned live 2026-09-23, full header capture):
 *   - unauth (json/html/plain, GET or POST) + wrong basic → 401 "Unauthorized"
 *     + WWW-Authenticate: OverleafLogin + X-Powered-By + fixed CSP,
 *     NO helmet baseline, NO session cookie.
 *   - right basic + GET ghost/missing → 404 "Not Found" (text/plain, weak ETag,
 *     X-Powered-By, fixed CSP, no helmet/cookie).
 *   - right basic + GET real doc → 200 application/json (Node key order,
 *     X-Powered-By, fixed CSP, no helmet/cookie).
 *
 * The valid-cred POST (setDocument) status is STATE-dependent in this stack
 * (Node :3000 returns 500 — an env/docstore condition, not the gate) and is
 * deliberately NOT in this wire gate; the unauth/wrong 401 surface above is
 * the profile-wire contract.
 *
 * Output: one line per case  label|status|hdr§...|body   (ETag weak-normalized,
 * set-cookie name-only, helmet-bundle joined so any presence is seen).
 */
const LEG = Number(process.argv[2] || 1)
// leg 1 and 3 → Node api :3000 (the oracle); leg 2 → Go api :4011 (the shadow).
const BASE = LEG === 2 ? 'http://127.0.0.1:4011' : 'http://127.0.0.1:3000'

const WRONG = 'Basic ' + Buffer.from('wrong:wrong').toString('base64')
// Valid service credentials — same source the runit services + Node read.
const VALID = (() => {
  try {
    const s = require('/etc/overleaf/settings.js')
    const u = Object.keys(s.httpAuthUsers || {})[0]
    if (u) return 'Basic ' + Buffer.from(u + ':' + s.httpAuthUsers[u]).toString('base64')
  } catch { /* not available */ }
  return null
})()
if (!VALID) {
  console.error('ERR|no-valid-creds: /etc/overleaf/settings.js httpAuthUsers unreadable')
  process.exit(1)
}

const PJ = process.env.UAPI_PJ
const DOC = process.env.UAPI_DOC
const GHOST = process.env.UAPI_GHOST || '666666666666666666666666'
const PI_UID = '6aa4b8b573ef0e5094f4cbc0' // e2e admin (fixed fixture)
if (!PJ || !DOC) {
  console.error('ERR|missing-fixture: UAPI_PJ / UAPI_DOC not set')
  process.exit(1)
}
const DOCGET = `/project/${PJ}/doc/${DOC}`
const GHOSTGET = `/project/${PJ}/doc/${GHOST}`
const REJ = DOCGET + '/changes/reject'

const CASES = []
function add(label, run) { CASES.push({ label, run }) }

async function main() {
  const out = []
  const etagN = (v) => (v || '').replace(/W\/"([0-9a-f]+)-[^"]*"/g, 'W/"$1-H"')
  const rec = (label, r) => {
    const sc = r.headers.getSetCookie ? r.headers.getSetCookie() : []
    const scN = sc
      .map((c) => c.split(';').map((p) => { const t = p.trim(); const i = t.indexOf('='); return i === -1 ? t : t.slice(0, i + 1) + 'X' }).join(';'))
      .join(' | ')
    const helmet = [
      r.headers.get('cross-origin-opener-policy') || '',
      r.headers.get('cross-origin-resource-policy') || '',
      r.headers.get('referrer-policy') || '',
      r.headers.get('x-content-type-options') || '',
      r.headers.get('x-download-options') || '',
      r.headers.get('x-frame-options') || '',
      r.headers.get('x-permitted-cross-domain-policies') || '',
      r.headers.get('x-xss-protection') || '',
      r.headers.get('content-security-policy') || '',
    ].join('§')
    const hdr = [
      r.headers.get('content-type') || '',
      r.headers.get('x-powered-by') || '',
      etagN(r.headers.get('etag')),
      scN,
      r.headers.get('location') || '',
      r.headers.get('vary') || '',
      r.headers.get('www-authenticate') || '',
      helmet,
    ].join('§')
    // One line per case: escape newlines/tabs in the body so every case is
    // exactly one output line (robust to multi-line bodies like the 404 page,
    // and the "cases=N" summary counts cases, not lines). createProject's
    // valid-200 body carries a freshly-generated random projectId — normalize
    // it to "X" so Node==Go compare on the wire (each leg creates its own id).
    const body1 = String(r._body || '')
      .replace(/\r?\n/g, '\\n')
      .replace(/\t/g, '\\t')
      .replace(/"projectId":"[0-9a-f]{24}"/gi, '"projectId":"X"')
    out.push(`${label}|${r.status}|${hdr}|${body1}`)
  }
  const get = async (path, headers) => {
    const r = await fetch(BASE + path, { headers: headers || {}, redirect: 'manual' })
    r._body = await r.text()
    return r
  }
  const post = async (path, body, headers) => {
    const h = { ...(headers || {}), 'content-type': 'application/json' }
    const r = await fetch(BASE + path, { method: 'POST', headers: h, body: typeof body === 'string' ? body : JSON.stringify(body), redirect: 'manual' })
    r._body = await r.text()
    return r
  }
  const del = async (path, headers) => {
    const r = await fetch(BASE + path, { method: 'DELETE', headers: headers || {}, redirect: 'manual' })
    r._body = await r.text()
    return r
  }
  // postRaw: POST a RAW (non-JSON) body with only the given headers (no forced
  // content-type). The TPDS third-party-sync update endpoints accept the raw
  // webhook body (Dropbox/GitHub send file contents, not JSON); forcing
  // application/json makes Node's body-parser 400 the raw body — not the real
  // wire.
  const postRaw = async (path, body, headers) => {
    const r = await fetch(BASE + path, { method: 'POST', headers: headers || {}, body, redirect: 'manual' })
    r._body = await r.text()
    return r
  }

  const J = { accept: 'application/json' }
  const H = { accept: 'text/html' }
  const WJ = { accept: 'application/json', authorization: WRONG }
  const WH = { accept: 'text/html', authorization: WRONG }
  const VJ = { accept: 'application/json', authorization: VALID }

  // unauth surface (any Accept → 401, GET + POST)
  add('doc-unauth-json', async () => get(DOCGET, J))
  add('doc-unauth-html', async () => get(DOCGET, H))
  add('doc-unauth-plain', async () => get(DOCGET, {}))
  add('post-doc-unauth', async () => post(DOCGET, {}))
  add('post-rej-unauth', async () => post(REJ, {}))

  // U-API — POST /project/:id/doc/:doc_id/changes/reject (privateApiRouter, API-only).
  // Node DocumentController.trackChangesRejected: strict zod body
  //   rejectedChangeAuthorIds: z.array(zz.objectId())  (required)
  //   userId: zz.objectId().nullish()                    (optional, may be null)
  //   previews: z.array(changePreview).optional()        (optional)
  // valid→204; any violation→400 JSON (zod, all issues, schema order, then
  // unknown-keys, joined "; "); array body→400 JSON; scalar/null/bad-JSON body
  // →400 HTML (705B express error page). All Node==Go verified (37-case compare,
  // 2026-09-24). PI_UID is the e2e admin (valid 24-hex oid).
  const PV = { sectionPath: [], startLine: 1, changes: [], slice: 's', sliceStart: 0, userIds: [PI_UID] }
  add('rej-valid-nullU', async () => post(REJ, { rejectedChangeAuthorIds: [PI_UID], userId: null }, VJ))
  add('rej-valid-uid', async () => post(REJ, { rejectedChangeAuthorIds: [PI_UID], userId: PI_UID }, VJ))
  add('rej-valid-prev', async () => post(REJ, { rejectedChangeAuthorIds: [PI_UID], previews: [PV] }, VJ))
  add('rej-nobody', async () => post(REJ, undefined, VJ))
  add('rej-rca-bad', async () => post(REJ, { rejectedChangeAuthorIds: ['no'] }, VJ))
  add('rej-uid-num', async () => post(REJ, { rejectedChangeAuthorIds: [PI_UID], userId: 5 }, VJ))
  add('rej-uid-nostr', async () => post(REJ, { rejectedChangeAuthorIds: [PI_UID], userId: 'no' }, VJ))
  add('rej-unk-key', async () => post(REJ, { rejectedChangeAuthorIds: [PI_UID], userId: PI_UID, bogus: 1 }, VJ))
  add('rej-prev-{}', async () => post(REJ, { rejectedChangeAuthorIds: [PI_UID], previews: [{}] }, VJ))
  add('rej-prev-extra', async () => post(REJ, { rejectedChangeAuthorIds: [PI_UID], previews: [{ ...PV, bogus: 1 }] }, VJ))
  add('rej-changes-nop', async () => post(REJ, { rejectedChangeAuthorIds: [PI_UID], previews: [{ ...PV, changes: [{ i: 'a' }] }] }, VJ))
  add('rej-rca-unk', async () => post(REJ, { rejectedChangeAuthorIds: 'x', other: 1 }, VJ))
  add('rej-arr-body', async () => post(REJ, '[1]', VJ))
  add('rej-raw-num', async () => post(REJ, '123', VJ))
  add('rej-raw-null', async () => post(REJ, 'null', VJ))
  add('rej-badjson', async () => post(REJ, '{bad', VJ))

  // wrong basic (any Accept → 401, never content-negotiates)
  add('doc-wrong-json', async () => get(DOCGET, WJ))
  add('doc-wrong-html', async () => get(DOCGET, WH))
  add('post-doc-wrong', async () => post(DOCGET, {}, { authorization: WRONG }))

  // valid cred: ghost → 404 plain, real → 200 JSON (both API-profile wire)
  add('doc-ghost-valid', async () => get(GHOSTGET, VJ))
  add('doc-valid-real', async () => get(DOCGET, VJ))

  // API-profile web-route exclusion (Node :3000 does NOT mount webRouter —
  // pinned live 2026-09-23): web-only routes 404 with the Express
  // finalhandler wire (Cannot <METHOD> <path>, content-security-policy:
  // "default-src 'none'", x-content-type-options: nosniff, x-powered-by:
  // Express), NOT the web profile's 301/302 login bounce. These pin the
  // route/profile-selection fix (previously Go api 301/302'd them).
  const P = '/project/' + PJ
  add('webroot-slash', async () => get('/', J))
  add('webroot-project', async () => get(P, J))
  add('webroot-members', async () => get(P + '/members', J))
  add('webroot-entities', async () => get('/entities', J))
  add('webroot-unknown', async () => get('/foo-404', J))

  // U-API — GET /project/:id/details (privateApiRouter, API-ONLY route). Node
  // ProjectDetailsHandler.getDetails wire: unauth→401 (challenge),
  // invalid-oid→404 JSON {error:"Validation error: Invalid Mongo ObjectId at
  // \"params.project_id\"",statusCode:404}, ghost→404 text/plain "Not Found",
  // valid→200 JSON {name, features:<owner's user.features, document order>}
  // (undefined desc/compiler/overleaf omitted). Node api :3000 == Go api :4011.
  add('details-unauth', async () => get(P + '/details', J))
  add('details-invalid', async () => get('/project/notahex/details', VJ))
  add('details-ghost', async () => get('/project/111111111111111111111111/details', VJ))
  add('details-valid', async () => get(P + '/details', VJ))

  // U-API — GET /user/:user_id/personal_info (privateApiRouter, basic-auth;
  // the webRouter variant is the distinct path /user/personal_info — no
  // collision). Node UserInfoController.getPersonalInfo wire: unauth→401
  // (challenge), bad-uid (not hex24/numeric)→404 JSON {error:"Validation
  // error: Invalid Mongo ObjectId at \"params.user_id\"",statusCode:404},
  // ghost (hex24, absent)→404 text/plain "Not Found", valid→200 JSON
  // {id, first_name, last_name, email} (id-first, truthy-only keys).
  // Node api :3000 == Go api :4011.
  add('pi-unauth', async () => get('/user/' + PI_UID + '/personal_info', J))
  add('pi-invalid', async () => get('/user/notahex/personal_info', VJ))
  add('pi-ghost', async () => get('/user/666666666666666666666666/personal_info', VJ))
  add('pi-valid', async () => get('/user/' + PI_UID + '/personal_info', VJ))

  // U-API — GET /user/:userId/tag (privateApiRouter, basic-auth; Node
  // TagsController.apiGetAllTags → Tag.find({user_id}) → res.json). Wire
  // (Node api :3000, live-pinned 2026-09-23): unauth → 401 (challenge);
  // userId not a 24-hex oid (notahex / all-digit) → 404 JSON VA (params.userId);
  // valid oid (ghost OR real) → 200 [tags] (admin fixture has none → []). The
  // web profile serves the U1 session 404-page oracle instead (APIOnly); u1
  // stays green (this is an api-profile :3000 oracle).
  add('tag-unauth', async () => get('/user/' + PI_UID + '/tag', J))
  add('tag-invalid', async () => get('/user/notahex/tag', VJ))
  add('tag-ghost', async () => get('/user/666666666666666666666666/tag', VJ))
  add('tag-digit', async () => get('/user/123456789/tag', VJ))
  add('tag-valid', async () => get('/user/' + PI_UID + '/tag', VJ))

  // U-API — POST /user/:user_id/project/new (createProject, privateApiRouter,
  // basic-auth; APIOnly). Node TpdsController.createProject →
  // generateUniqueName → createBlankProject (BLANK: rootFolder, no main.tex)
  // → res.json({projectId}). Wire (Node api :3000, pinned 2026-09-23):
  // unauth / wrong → 401 (challenge); user_id !24-hex → 404 JSON VA
  // (params.user_id); valid-uid + valid-name → 200 {"projectId":"<24hex>"}
  // (CREATES a BLANK project; projectId normalized to "X" above); valid-uid +
  // invalid-name (empty / slash) → 500 "Internal Server Error" (Node's TPDS
  // path leaves the name-validation error unmapped → Express 500; NO project
  // created). Only cp-valid-name writes state; the test cleans it up.
  add('cp-unauth', async () => post('/user/' + PI_UID + '/project/new', { projectName: 'uapi-cp-gate' }, J))
  add('cp-wrong', async () => post('/user/' + PI_UID + '/project/new', { projectName: 'uapi-cp-gate' }, WJ))
  add('cp-invalid-oid', async () => post('/user/notahex/project/new', { projectName: 'x' }, VJ))
  add('cp-invalid-digit', async () => post('/user/123456789/project/new', {}, VJ))
  add('cp-empty-name', async () => post('/user/' + PI_UID + '/project/new', { projectName: '' }, VJ))
  add('cp-slash-name', async () => post('/user/' + PI_UID + '/project/new', { projectName: 'a/b' }, VJ))
  add('cp-valid-name', async () => post('/user/' + PI_UID + '/project/new', { projectName: 'uapi-cp-gate' }, VJ))

  // U-API — POST /user/:user_id/project/resolve (resolveProject, privateApiRouter,
  // basic-auth; APIOnly). Node getOrCreateProject (get-by-id RW+active OR
  // get-or-create-by-name). Wire (Node api :3000, pinned 2026-09-23):
  // unauth/wrong → 401; user_id !24hex → 404 VA (params.user_id);
  // {projectId:!24hex} → 400 VA (body.projectId); {} → 400 (197B, undefined or); {projectName:''}
  // → 400 (120B, Too small); both keys → 400 (139B, Unrecognized key);
  // {projectId:ghost} → 200 {"status":"rejected"} (21B);
  // {projectName:existing} → 200 {"status":"success","projectId","historyId?,"otMigrationStage":0}
  //   (historyId OMITTED when the project doc has no overleaf.history.id — the
  //    uapi-wire seed has none; a freshly created blank has it = projectId);
  // {projectName:new-name} → 200 success (CREATES a blank project named exactly that).
  // SEED = the seeded uapi-wire fixture (owned by PI_UID, active).
  const SEED = process.env.UAPI_SEEDNAME || 'uapi-wire'
  add('res-unauth', async () => post('/user/' + PI_UID + '/project/resolve', { projectName: SEED }, J))
  add('res-wrong', async () => post('/user/' + PI_UID + '/project/resolve', { projectName: SEED }, WJ))
  add('res-invalid-oid', async () => post('/user/notahex/project/resolve', { projectName: 'x' }, VJ))
  add('res-invalid-digit', async () => post('/user/123456789/project/resolve', { projectName: 'x' }, VJ))
  add('res-bad-pid', async () => post('/user/' + PI_UID + '/project/resolve', { projectId: 'notahex' }, VJ))
  add('res-empty-body', async () => post('/user/' + PI_UID + '/project/resolve', {}, VJ))
  add('res-empty-name', async () => post('/user/' + PI_UID + '/project/resolve', { projectName: '' }, VJ))
  add('res-both-keys', async () => post('/user/' + PI_UID + '/project/resolve', { projectId: 'notahex', projectName: 'x' }, VJ))
  add('res-ghost-pid', async () => post('/user/' + PI_UID + '/project/resolve', { projectId: GHOST }, VJ))
  add('res-existing-name', async () => post('/user/' + PI_UID + '/project/resolve', { projectName: SEED }, VJ))
  add('res-new-name', async () => post('/user/' + PI_UID + '/project/resolve', { projectName: 'resgate-gate' }, VJ))

  // U-API — POST /tpds/folder-update (updateFolder, privateApiRouter, basic-
  // auth; APIOnly). Node TpdsUpdateHandler.createFolder -> getOrCreateProject
  // -> FileTypeManager.shouldIgnore -> UpdateMerger.createFolder ->
  // EditorController.promises.mkdirp -> ProjectEntityUpdateHandler.mkdirp
  // (find-or-create each segment). Wire (Node api :3000, live-pinned 2026-09-23):
  // unauth / wrong -> 401 (challenge); {} -> 400 (185B, userId+path undefined);
  // {userId} -> 400 (114B, path undefined); {path} -> 400 (116B, userId
  // undefined); {userId:not-an-oid} -> 400 (88B, Invalid Mongo ObjectId at
  // body.userId); {userId:ghost} -> 500 (21B, no project -> Express 500);
  // valid uid + path -> 200 JSON {entityId, projectId, path, folderId} (CREATES
  // the folder; entityId=deepest, folderId=its parent, null at root). Node's
  // mkdirp uses the PROJECT _id for the mongo filter; the Go port (tpdsMkdirp)
  // matches. All 3 legs hit the SEEDed project A (re-seeded each run, so the
  // created folders converge to the same OIDs and are wiped on the next run).
  add('fu-unauth', async () => post('/tpds/folder-update', { userId: PI_UID, path: '/gu-g' }, J))
  add('fu-wrong', async () => post('/tpds/folder-update', { userId: PI_UID, path: '/gu-g' }, WJ))
  add('fu-no-body', async () => post('/tpds/folder-update', {}, VJ))
  add('fu-uid-only', async () => post('/tpds/folder-update', { userId: PI_UID }, VJ))
  add('fu-path-only', async () => post('/tpds/folder-update', { path: '/gu-g' }, VJ))
  add('fu-bad-uid', async () => post('/tpds/folder-update', { userId: 'notanoid', path: '/gu-g' }, VJ))
  // NB: {userId:ghost, path} (NO projectId/projectName) is a STATEFUL/undefined
  // Node region — getOrCreateProject with no id/name falls through and Node
  // itself is non-deterministic (500 "Internal Server Error" OR 200 resolving
  // path:"/"). Not a stable wire contract, so it is deliberately EXCLUDED (the
  // deterministic {projectId:ghost}-absent 500 and the valid {projectId} wire
  // above are the pinned contract).
  add('fu-root', async () => post('/tpds/folder-update', { userId: PI_UID, projectId: PJ, path: '/' }, VJ))
  add('fu-top', async () => post('/tpds/folder-update', { userId: PI_UID, projectId: PJ, path: '/gu-g' }, VJ))
  add('fu-nested', async () => post('/tpds/folder-update', { userId: PI_UID, projectId: PJ, path: '/gu-g/gu-n' }, VJ))

  // U-API — TPDS third-party-sync update endpoints (Dropbox mergeUpdate/
  // deleteUpdate + GitHub updateProjectContents/deleteProjectContents),
  // privateApiRouter, basic-auth (APIOnly). Deterministic 3-leg-stable states
  // (401 / 404-VA single+joined / mergeUpdate rejected / GitHub 404-Not-Found /
  // Dropbox-DELETE 200 "OK" / GitHub-DELETE 200 {}). The 200-applied doc/file
  // upsert was verified via a direct Node==Go comparison on separate per-leg
  // projects (entityId/rev are stateful per leg on a shared project, so it is
  // not 3-leg gate-able). All of the cases above leave the seeded project PJ
  // untouched, so the 3 legs converge.
  add('sync-mu-dbox-unauth', async () => postRaw('/user/' + PI_UID + '/update/mu-g/x.tex', 'hello', J))
  add('sync-mu-dbox-wrong', async () => postRaw('/user/' + PI_UID + '/update/mu-g/x.tex', 'hello', WJ))
  add('sync-mu-dbox-baduid', async () => postRaw('/user/notahex/update/mu-g/x.tex', 'hello', VJ))
  add('sync-mu-pid-unauth', async () => postRaw('/project/' + PJ + '/user/' + PI_UID + '/update/x.tex', 'hello', J))
  add('sync-mu-pid-badpid', async () => postRaw('/project/badpid/user/' + PI_UID + '/update/x.tex', 'hello', VJ))
  add('sync-mu-pid-baduid', async () => postRaw('/project/' + PJ + '/user/notahex/update/x.tex', 'hello', VJ))
  add('sync-mu-pid-bothbad', async () => postRaw('/project/badpid/user/notahex/update/x.tex', 'hello', VJ))
  add('sync-mu-pid-ghostpid', async () => postRaw('/project/' + GHOST + '/user/' + PI_UID + '/update/x.tex', 'hello', VJ))
  add('sync-mu-pid-ghostuid', async () => postRaw('/project/' + PJ + '/user/' + GHOST + '/update/x.tex', 'hello', VJ))
  add('sync-gh-unauth', async () => postRaw('/project/' + PJ + '/contents/x.tex', 'hello', J))
  add('sync-gh-badpid', async () => postRaw('/project/badpid/contents/x.tex', 'hello', VJ))
  add('sync-gh-ghostpid', async () => postRaw('/project/' + GHOST + '/contents/x.tex', 'hello', VJ))
  add('sync-du-dbox-unauth', async () => del('/user/' + PI_UID + '/update/mu-g/x.tex', J))
  add('sync-du-dbox-ghostuid', async () => del('/user/' + GHOST + '/update/mu-g/x.tex', VJ))
  add('sync-du-dbox-baduid', async () => del('/user/notahex/update/mu-g/x.tex', VJ))
  add('sync-du-pid-unauth', async () => del('/project/' + PJ + '/user/' + PI_UID + '/update/x.tex', J))
  add('sync-du-pid-ghostpid', async () => del('/project/' + GHOST + '/user/' + PI_UID + '/update/x.tex', VJ))
  add('sync-du-pid-ghostuid', async () => del('/project/' + PJ + '/user/' + GHOST + '/update/x.tex', VJ))
  add('sync-du-pid-baduid', async () => del('/project/' + PJ + '/user/notahex/update/x.tex', VJ))
  add('sync-du-pid-bothbad', async () => del('/project/badpid/user/notahex/update/x.tex', VJ))
  add('sync-ghdel-ghostpid', async () => del('/project/' + GHOST + '/contents/x.tex', VJ))
  add('sync-ghdel-unauth', async () => del('/project/' + PJ + '/contents/x.tex', J))
  add('sync-ghdel-badpid', async () => del('/project/badpid/contents/x.tex', VJ))

  // U-API — GET /perfTest (privateApiRouter, public, no auth): plainText 200
  // "hello" (nosniff + XPB + global CSP, ETag W/"5-..."). APIOnly (Node web
  // :4000 does not mount it). Pinned Node :3000 (2026-09-24); Node==Go.
  add('api-perftest', async () => get('/perfTest', {}))

  // U-API — GET /internal/project/:project_id (privateApiRouter, API-ONLY).
  // Same Node handler (ProjectDetailsHandler.getDetails) as /project/:id/details
  // → identical 200 body; only the path + param name (project_id) differ.
  // Wire pinned Node :3000 (2026-09-24): unauth→401 (challenge); bad-oid→404
  // JSON VA params.project_id; ghost→404 text/plain "Not Found"; valid→200 JSON
  // {name,description?,compiler?,features,overleaf?} (PJ is the seeded fixture,
  // body has no random ids so the 3 legs converge).
  add('idp-unauth', async () => get('/internal/project/' + PJ, J))
  add('idp-badoid', async () => get('/internal/project/notahex', VJ))
  add('idp-ghost', async () => get('/internal/project/' + GHOST, VJ))
  add('idp-valid', async () => get('/internal/project/' + PJ, VJ))

  // POST /internal/project/:project_id/deactivate (Node
  // InactiveProjectController.deactivateProject; basic-auth, api-only).
  // Pinned Node :3000 (2026-09-24): unauth/wrong→401 "Unauthorized" (text/plain,
  // +WWW-Authenticate+XPB+CSP); bad-oid→404 JSON VA params.project_id;
  // valid-oid (ghost→no-op, real→active:false)→200 text/plain "OK" (2B, XPB,
  // weak ETag, no nosniff, +CSP). No body. Only the ghost leg reaches the 200
  // (a no-op mutation), so this gate is non-destructive.
  add('deact-unauth', async () => postRaw('/internal/project/' + PJ + '/deactivate', undefined, { accept: 'application/json' }))
  add('deact-wrong', async () => postRaw('/internal/project/' + PJ + '/deactivate', undefined, WJ))
  add('deact-badoid', async () => postRaw('/internal/project/notahex/deactivate', undefined, VJ))
  add('deact-ghost', async () => postRaw('/internal/project/' + GHOST + '/deactivate', undefined, VJ))

  // POST /project/:Project_id/join (Node EditorRouter, privateApiRouter only,
  // basic-auth; "called by the real-time API"). Pinned Node :3000 (2026-09-24):
  // unauth/wrong→401 (challenge); bad Project_id→404 JSON VA params.Project_id;
  // body invalid→400 JSON VA (userId union/unknown key); bothbad→404 joined;
  // ghost→404 text "Not Found"; anon (private)→403 "Forbidden"; owner→200 JSON
  // {project:<model>,privilegeLevel,isRestrictedUser,isTokenMember,isInvitedMember}
  // (Node EditorHttpController.joinProject + ProjectEditorHandler.buildProjectModelView;
  // owner is the fixed-owner seeded uapi-wire project → deterministic 3-leg wire).
  const JOIN = `/project/${PJ}/join`
  add('join-unauth', async () => post(JOIN, { userId: PI_UID }, J))
  add('join-wrong', async () => post(JOIN, { userId: PI_UID }, WJ))
  add('join-badparam', async () => post('/project/notahex/join', { userId: PI_UID }, VJ))
  add('join-nobody', async () => post(JOIN, {}, VJ))
  add('join-baduid', async () => post(JOIN, { userId: 'notahex' }, VJ))
  add('join-nonstring', async () => post(JOIN, { userId: 123 }, VJ))
  add('join-unknownkey', async () => post(JOIN, { userId: PI_UID, extra: 1 }, VJ))
  add('join-bothbad', async () => post('/project/notahex/join', { userId: 'notahex' }, VJ))
  add('join-ghost', async () => post('/project/' + GHOST + '/join', { userId: PI_UID }, VJ))
  add('join-anon', async () => post(JOIN, { userId: 'anonymous-user' }, VJ))
  add('join-owner200', async () => post(JOIN, { userId: PI_UID }, VJ))

  for (const c of CASES) {
    try {
      const r = await c.run()
      rec(c.label, r)
    } catch (e) {
      out.push(`${c.label}|ERR|${(e && e.message) || e}||`)
    }
  }
  console.log(out.join('\n'))
}

main().catch((e) => { console.error((e && e.message) || e); process.exit(1) })
