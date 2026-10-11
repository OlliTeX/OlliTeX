// uapi-doc: private-API (API profile, :3000) document-trio + admin/private
// route wire contract — standalone.
//
// CONVERTED 2026-10-05 (owner): the 3-leg shadow comparison (Node api :3000
// oracle vs Go shadow :4011) is retired — in the P7 image :3000 IS the
// canonical Go api profile and the shadow no longer exists. The full wire is
// now pinned DIRECTLY on canonical Go (:3000) + 2-run byte-parity stability
// (re-seeded between runs; stateful cases create projects each run).
//
// Surface: ~125 cases from uapi-doc-matrix.cjs, grouped:
//   document-trio (doc GET/POST unauth|wrong|valid|ghost)
//   changes-reject validation family
//   route-selection (web profile-only routes 404 on api)
//   details / personal_info / tag (unauth|invalid|ghost|valid)
//   TPDS cp-gate + resolve + folder-upload + dropbox/github/githubdelete
//     sync-mu + delete + deact + join + rsync + export + zip + bulk + deactold
// Node-oracle notes: the TPDS path maps invalid project names to the Express
//   500 (tpdsapi.go pins this — NOT a 400 regression); valid-cred surface
//   pins current Go behavior (flip-era legs were identical).
// csrf + nonce + random ids are normalized (sid/etag via the matrix; 24-hex
// ids + unique-name counts in the spec normalizer).
import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const OWNER = '6aa4b8b573ef0e5094f4cbc0' // Node-era fixture identity: uapi-wire seeded with owner_ref = matrix PI_UID (a GHOST user — row need not exist; node-era resolve pins — pi-valid 404 + res-existing-name 50B no-historyId — were captured with this exact owner/requester coupling; 2026-10-05 round-5 diagnosis)
const SEED_NAME = 'uapi-wire'
const CANON = ['UAPI WIRE L1', 'UAPI WIRE L2']

const MATRIX = ((): string => {
  const cands = [
    path.join(process.cwd(), 'specs/parity', 'uapi-doc-matrix.cjs'),
    path.join(process.cwd(), 'uapi-doc-matrix.cjs'),
    path.join('/data_1/image_mining/the_diff/overleaf/tests/e2e/specs/parity', 'uapi-doc-matrix.cjs'),
  ]
  return cands.find((p) => fs.existsSync(p)) || cands[0]
})()

function dexe(c: string, cmd: string): string {
  return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], {
    encoding: 'utf8', stdio: 'pipe', maxBuffer: 1 << 28,
  }) || ''
}

function dexeQ(c: string, js: string): string {
  return execFileSync('docker', ['exec', c, 'mongosh', '--quiet', 'sharelatex', '--eval', js], { encoding: 'utf8' }).trim()
}

function seedState(): { A: string; DA: string } {
  pinOwnerFeaturesEmpty()
  const CANONJS = JSON.stringify(CANON)
  const seedJs = `
    db.projects.deleteMany({name:"${SEED_NAME}"});
    const uid = ObjectId("${OWNER}");
    const dA = new ObjectId();
    db.projects.insertOne({_id:new ObjectId(), name:"${SEED_NAME}", owner_ref:uid, publicAccesLevel:"private",
      version:0, rootFolder:[{name:"", _id:new ObjectId(), docs:[{_id:dA, name:"doc.tex"}], fileRefs:[], folders:[]}]});
  `
  fs.writeFileSync('/tmp/uapi-wire-seed.js', seedJs)
  execFileSync('docker', ['cp', '/tmp/uapi-wire-seed.js', mongoC + ':/tmp/uapi-wire-seed.js'], { stdio: 'ignore' })
  dexe(mongoC, 'mongosh --quiet sharelatex /tmp/uapi-wire-seed.js')
  let st = null as { A: string; DA: string } | null
  for (let att = 1; att <= 4 && !st; att++) {
    const line = dexe(mongoC, `mongosh --quiet sharelatex --eval 'const p=db.projects.findOne({name:"${SEED_NAME}"}); if(p)print(JSON.stringify({A:String(p._id), DA:String(p.rootFolder[0].docs[0]._id)}))'`)
      .trim().split('\n').pop() || ''
    try {
      const j = JSON.parse(line)
      if (/^[0-9a-f]{24}$/.test(j.A) && /^[0-9a-f]{24}$/.test(j.DA)) st = j
    } catch { /* not ready */ }
    if (!st) dexe(mongoC, 'sleep 1')
  }
  if (!st) throw new Error('seed not visible after 4 attempts')
  const body = '{"lines":' + JSON.stringify(CANON) + ',"version":0,"ranges":{}}'
  dexe(overleafC, `for ATT in 1 2 3 4; do
    CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST http://127.0.0.1:3016/project/${st.A}/doc/${st.DA} -H 'content-type: application/json' -d '${body}')
    [ "$CODE" = "200" ] && break; sleep 1
  done
  [ "$CODE" = "200" ] || exit 1`)
  return st
}

function cleanupResiduals() {
  // prefix match (covers ' (N)' unique-name suffixes); file-based mongosh — no shell quoting
  const js = [
    "const ids = db.projects.find({ name: { $regex: '^(uapi-cp-gate|uapi-wire|resgate-gate)' } }).toArray().map(p => p._id);",
    "if (ids.length) { db.projects.deleteMany({ _id: { $in: ids } }); db.project_history.deleteMany({ project_id: { $in: ids } }); }",
    "true;",
  ].join('\n')
  fs.writeFileSync('/tmp/uapi-cleanup.js', js)
  execFileSync('docker', ['cp', '/tmp/uapi-cleanup.js', mongoC + ':/tmp/uapi-cleanup.js'], { stdio: 'ignore' })
  dexe(mongoC, 'mongosh --quiet sharelatex /tmp/uapi-cleanup.js')
}

function runMatrix(): string[] {
  const st = seedState()
  try {
    execFileSync('docker', ['cp', MATRIX, overleafC + ':/tmp/uapi-doc-matrix.cjs'], { stdio: 'ignore' })
    let out = ''
    for (let att = 1; att <= 2 && out.split('\n').filter(Boolean).length === 0; att++) {
      out = dexe(overleafC, `UAPI_PJ=${st.A} UAPI_DOC=${st.DA} node /tmp/uapi-doc-matrix.cjs 1`)
      if (!out) dexe(overleafC, 'sleep 2')
    }
    return out.split('\n').filter(Boolean)
  } finally {
    cleanupResiduals()
    restoreOwnerFeatures()
  }
}

// 24-hex ids + unique-name "(N)" counts are per-run; normalize both pins and runs.
function normalizeLines(lines: string[]): string[] {
  return lines.map((l) => l
    .replace(/\b[0-9a-f]{24}\b/g, 'HEX')
    .replace(/ \(\d+\)/g, '')
    // per-request CSP nonces (canonical Go mints a fresh one every render;
    // the captured rows carry baked nonces) + per-request csurf tokens:
    // normalize on BOTH sides so live vs pinned and run-1 vs run-2 compare
    // the stable wire shape.
    .replace(/nonce-[A-Za-z0-9+/=._~-]{8,64}/g, 'nonce-N')
    .replace(/nonce="[^"]*"/g, 'nonce="N"')
    .replace(/value="[A-Za-z0-9_-]{16,64}"/g, 'value="T"')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="T"'))
}

// owner user's features is frozen to {} during each battery — the flip-era
// fixture had an empty-features owner and Node's ProjectDetailsHandler
// (ProjectDetailsHandler.mjs:65-69) returns user.features VERBATIM; later
// fixture runs wrote full feature flags into e2e-user (fixture drift), which
// would drift every details/idp row. Snapshot + set, restore after the run.
let ownerFeaturesBackup: string | null = null
function pinOwnerFeaturesEmpty(): void {
  if (ownerFeaturesBackup !== null) return
  const prev = dexeQ(mongoC, `const f = db.users.findOne({_id:ObjectId("${OWNER}")}, {features:1}); print(f ? (f.features === undefined ? '__ABSENT__' : JSON.stringify(f.features)) : '__GHOST__')`).trim().split('\n').pop() || '__GHOST__'
  ownerFeaturesBackup = prev === '' ? '__ABSENT__' : prev
  dexeQ(mongoC, `db.users.updateOne({_id:ObjectId("${OWNER}")}, {$set:{features:{}}})`)
}
function restoreOwnerFeatures(): void {
  if (ownerFeaturesBackup === null) return
  if (ownerFeaturesBackup === '__GHOST__') { ownerFeaturesBackup = null; return }
  if (ownerFeaturesBackup === '__ABSENT__') {
    dexeQ(mongoC, `db.users.updateOne({_id:ObjectId("${OWNER}")}, {$unset:{features:""}})`)
  } else {
    fs.writeFileSync('/tmp/uapi-feat-restore.js', `db.users.updateOne({_id:ObjectId("${OWNER}")}, {$set:{features:${ownerFeaturesBackup}}})`)
    execFileSync('docker', ['cp', '/tmp/uapi-feat-restore.js', mongoC + ':/tmp/uapi-feat-restore.js'], { stdio: 'ignore' })
    dexe(mongoC, 'mongosh --quiet sharelatex /tmp/uapi-feat-restore.js')
  }
  ownerFeaturesBackup = null
}

const CONTRACT: string[] = [
"doc-unauth-json|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "doc-unauth-html|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "doc-unauth-plain|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "post-doc-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "post-rej-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "rej-valid-nullU|204|§Express§W/\"a-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|",
  "rej-valid-uid|204|§Express§W/\"a-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|",
  "rej-valid-prev|204|§Express§W/\"a-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|",
  "rej-nobody|400|application/json; charset=utf-8§Express§W/\"84-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected array, received undefined at \\\"body.rejectedChangeAuthorIds\\\"\",\"statusCode\":400}",
  "rej-rca-bad|400|application/json; charset=utf-8§Express§W/\"6c-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"body.rejectedChangeAuthorIds[0]\\\"\",\"statusCode\":400}",
  "rej-uid-num|400|application/json; charset=utf-8§Express§W/\"71-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected string, received number at \\\"body.userId\\\"\",\"statusCode\":400}",
  "rej-uid-nostr|400|application/json; charset=utf-8§Express§W/\"58-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"body.userId\\\"\",\"statusCode\":400}",
  "rej-unk-key|400|application/json; charset=utf-8§Express§W/\"56-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Unrecognized key: \\\"bogus\\\" at \\\"body\\\"\",\"statusCode\":400}",
  "rej-prev-{}|400|application/json; charset=utf-8§Express§W/\"229-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected array, received undefined at \\\"body.previews[0].sectionPath\\\"; Invalid input: expected number, received undefined at \\\"body.previews[0].startLine\\\"; Invalid input: expected array, received undefined at \\\"body.previews[0].changes\\\"; Invalid input: expected string, received undefined at \\\"body.previews[0].slice\\\"; Invalid input: expected number, received undefined at \\\"body.previews[0].sliceStart\\\"; Invalid input: expected array, received undefined at \\\"body.previews[0].userIds\\\"\",\"statusCode\":400}",
  "rej-prev-extra|400|application/json; charset=utf-8§Express§W/\"62-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Unrecognized key: \\\"bogus\\\" at \\\"body.previews[0]\\\"\",\"statusCode\":400}",
  "rej-changes-nop|400|application/json; charset=utf-8§Express§W/\"86-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected number, received undefined at \\\"body.previews[0].changes[0].p\\\"\",\"statusCode\":400}",
  "rej-rca-unk|400|application/json; charset=utf-8§Express§W/\"aa-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected array, received string at \\\"body.rejectedChangeAuthorIds\\\"; Unrecognized key: \\\"other\\\" at \\\"body\\\"\",\"statusCode\":400}",
  "rej-arr-body|400|application/json; charset=utf-8§Express§W/\"69-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected object, received array at \\\"body\\\"\",\"statusCode\":400}",
  "rej-raw-num|400|application/json; charset=utf-8§Express§W/\"2-H\"§§§§§§§§§§§§§|{}",
  "rej-raw-null|400|application/json; charset=utf-8§Express§W/\"2-H\"§§§§§§§§§§§§§|{}",
  "rej-badjson|400|application/json; charset=utf-8§Express§W/\"2-H\"§§§§§§§§§§§§§|{}",
  "doc-wrong-json|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "doc-wrong-html|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "post-doc-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "doc-ghost-valid|404|text/plain; charset=utf-8§Express§W/\"9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Not Found",
  "doc-valid-real|200|application/json; charset=utf-8§Express§W/\"c8-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"lines\":[\"UAPI WIRE L1\",\"UAPI WIRE L2\"],\"version\":0,\"ranges\":{},\"pathname\":\"/doc.tex\",\"projectHistoryType\":\"project-history\",\"historyRangesSupport\":false,\"otMigrationStage\":0,\"resolvedCommentIds\":[]}",
  "webroot-slash|404|text/html; charset=utf-8§Express§§§§§§§§§nosniff§§§§§default-src 'none'|<!DOCTYPE html>\\n<html lang=\"en\">\\n<head>\\n<meta charset=\"utf-8\">\\n<title>Error</title>\\n</head>\\n<body>\\n<pre>Cannot GET /</pre>\\n</body>\\n</html>\\n",
  "webroot-project|404|text/html; charset=utf-8§Express§§§§§§§§§nosniff§§§§§default-src 'none'|<!DOCTYPE html>\\n<html lang=\"en\">\\n<head>\\n<meta charset=\"utf-8\">\\n<title>Error</title>\\n</head>\\n<body>\\n<pre>Cannot GET /project/HEX</pre>\\n</body>\\n</html>\\n",
  "webroot-members|404|text/html; charset=utf-8§Express§§§§§§§§§nosniff§§§§§default-src 'none'|<!DOCTYPE html>\\n<html lang=\"en\">\\n<head>\\n<meta charset=\"utf-8\">\\n<title>Error</title>\\n</head>\\n<body>\\n<pre>Cannot GET /project/HEX/members</pre>\\n</body>\\n</html>\\n",
  "webroot-entities|404|text/html; charset=utf-8§Express§§§§§§§§§nosniff§§§§§default-src 'none'|<!DOCTYPE html>\\n<html lang=\"en\">\\n<head>\\n<meta charset=\"utf-8\">\\n<title>Error</title>\\n</head>\\n<body>\\n<pre>Cannot GET /entities</pre>\\n</body>\\n</html>\\n",
  "webroot-unknown|404|text/html; charset=utf-8§Express§§§§§§§§§nosniff§§§§§default-src 'none'|<!DOCTYPE html>\\n<html lang=\"en\">\\n<head>\\n<meta charset=\"utf-8\">\\n<title>Error</title>\\n</head>\\n<body>\\n<pre>Cannot GET /foo-404</pre>\\n</body>\\n</html>\\n",
  "details-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "details-invalid|404|application/json; charset=utf-8§Express§W/\"5e-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.project_id\\\"\",\"statusCode\":404}",
  "details-ghost|404|text/plain; charset=utf-8§Express§W/\"9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Not Found",
  "details-valid|200|application/json; charset=utf-8§Express§W/\"22-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"name\":\"uapi-wire\",\"features\":{}}",
  "pi-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "pi-invalid|404|application/json; charset=utf-8§Express§W/\"5b-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.user_id\\\"\",\"statusCode\":404}",
  "pi-ghost|404|text/plain; charset=utf-8§Express§W/\"9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Not Found",
  "pi-valid|404|text/plain; charset=utf-8§Express§W/\"9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Not Found",
  "tag-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "tag-invalid|404|application/json; charset=utf-8§Express§W/\"5a-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.userId\\\"\",\"statusCode\":404}",
  "tag-ghost|200|application/json; charset=utf-8§Express§W/\"2-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|[]",
  "tag-digit|404|application/json; charset=utf-8§Express§W/\"5a-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.userId\\\"\",\"statusCode\":404}",
  "tag-valid|200|application/json; charset=utf-8§Express§W/\"2-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|[]",
  "cp-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "cp-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "cp-invalid-oid|404|application/json; charset=utf-8§Express§W/\"5b-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.user_id\\\"\",\"statusCode\":404}",
  "cp-invalid-digit|404|application/json; charset=utf-8§Express§W/\"5b-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.user_id\\\"\",\"statusCode\":404}",
  "cp-empty-name|500|text/plain; charset=utf-8§Express§W/\"15-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Internal Server Error",
  "cp-slash-name|500|text/plain; charset=utf-8§Express§W/\"15-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Internal Server Error",
  "cp-valid-name|200|application/json; charset=utf-8§Express§W/\"28-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"projectId\":\"X\"}",
  "res-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "res-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "res-invalid-oid|404|application/json; charset=utf-8§Express§W/\"5b-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.user_id\\\"\",\"statusCode\":404}",
  "res-invalid-digit|404|application/json; charset=utf-8§Express§W/\"5b-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.user_id\\\"\",\"statusCode\":404}",
  "res-bad-pid|400|application/json; charset=utf-8§Express§W/\"5b-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"body.projectId\\\"\",\"statusCode\":400}",
  "res-empty-body|400|application/json; charset=utf-8§Express§W/\"c5-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected string, received undefined at \\\"body.projectId\\\" or Invalid input: expected string, received undefined at \\\"body.projectName\\\"\",\"statusCode\":400}",
  "res-empty-name|400|application/json; charset=utf-8§Express§W/\"78-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Too small: expected string to have >=1 characters at \\\"body.projectName\\\"\",\"statusCode\":400}",
  "res-both-keys|400|application/json; charset=utf-8§Express§W/\"b9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"body.projectId\\\"; Unrecognized key: \\\"projectName\\\" at \\\"body\\\" or Unrecognized key: \\\"projectId\\\" at \\\"body\\\"\",\"statusCode\":400}",
  "res-ghost-pid|200|application/json; charset=utf-8§Express§W/\"15-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"status\":\"rejected\"}",
  "res-existing-name|200|application/json; charset=utf-8§Express§W/\"50-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"status\":\"success\",\"projectId\":\"X\",\"otMigrationStage\":0}",
  "res-new-name|200|application/json; charset=utf-8§Express§W/\"77-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"status\":\"success\",\"projectId\":\"X\",\"historyId\":\"HEX\",\"otMigrationStage\":0}",
  "fu-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "fu-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "fu-no-body|400|application/json; charset=utf-8§Express§W/\"b9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected string, received undefined at \\\"body.userId\\\"; Invalid input: expected string, received undefined at \\\"body.path\\\"\",\"statusCode\":400}",
  "fu-uid-only|400|application/json; charset=utf-8§Express§W/\"72-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected string, received undefined at \\\"body.path\\\"\",\"statusCode\":400}",
  "fu-path-only|400|application/json; charset=utf-8§Express§W/\"74-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected string, received undefined at \\\"body.userId\\\"\",\"statusCode\":400}",
  "fu-bad-uid|400|application/json; charset=utf-8§Express§W/\"58-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"body.userId\\\"\",\"statusCode\":400}",
  "fu-root|200|application/json; charset=utf-8§Express§W/\"69-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"entityId\":\"HEX\",\"projectId\":\"X\",\"path\":\"/\",\"folderId\":null}",
  "fu-top|200|application/json; charset=utf-8§Express§W/\"83-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"entityId\":\"HEX\",\"projectId\":\"X\",\"path\":\"/gu-g\",\"folderId\":\"HEX\"}",
  "fu-nested|200|application/json; charset=utf-8§Express§W/\"88-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"entityId\":\"HEX\",\"projectId\":\"X\",\"path\":\"/gu-g/gu-n\",\"folderId\":\"HEX\"}",
  "sync-mu-dbox-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "sync-mu-dbox-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "sync-mu-dbox-baduid|404|application/json; charset=utf-8§Express§W/\"5b-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.user_id\\\"\",\"statusCode\":404}",
  "sync-mu-pid-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "sync-mu-pid-badpid|404|application/json; charset=utf-8§Express§W/\"5e-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.project_id\\\"\",\"statusCode\":404}",
  "sync-mu-pid-baduid|404|application/json; charset=utf-8§Express§W/\"5b-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.user_id\\\"\",\"statusCode\":404}",
  "sync-mu-pid-bothbad|404|application/json; charset=utf-8§Express§W/\"8c-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.user_id\\\"; Invalid Mongo ObjectId at \\\"params.project_id\\\"\",\"statusCode\":404}",
  "sync-mu-pid-ghostpid|200|application/json; charset=utf-8§Express§W/\"15-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"status\":\"rejected\"}",
  "sync-mu-pid-ghostuid|200|application/json; charset=utf-8§Express§W/\"15-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"status\":\"rejected\"}",
  "sync-gh-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "sync-gh-badpid|404|application/json; charset=utf-8§Express§W/\"5e-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.project_id\\\"\",\"statusCode\":404}",
  "sync-gh-ghostpid|404|text/plain; charset=utf-8§Express§W/\"9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Not Found",
  "sync-du-dbox-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "sync-du-dbox-ghostuid|200|text/plain; charset=utf-8§Express§W/\"2-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|OK",
  "sync-du-dbox-baduid|404|application/json; charset=utf-8§Express§W/\"5b-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.user_id\\\"\",\"statusCode\":404}",
  "sync-du-pid-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "sync-du-pid-ghostpid|200|text/plain; charset=utf-8§Express§W/\"2-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|OK",
  "sync-du-pid-ghostuid|200|text/plain; charset=utf-8§Express§W/\"2-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|OK",
  "sync-du-pid-baduid|404|application/json; charset=utf-8§Express§W/\"5b-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.user_id\\\"\",\"statusCode\":404}",
  "sync-du-pid-bothbad|404|application/json; charset=utf-8§Express§W/\"8c-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.user_id\\\"; Invalid Mongo ObjectId at \\\"params.project_id\\\"\",\"statusCode\":404}",
  "sync-ghdel-ghostpid|200|application/json; charset=utf-8§Express§W/\"2-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{}",
  "sync-ghdel-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "sync-ghdel-badpid|404|application/json; charset=utf-8§Express§W/\"5e-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.project_id\\\"\",\"statusCode\":404}",
  "api-perftest|200|text/plain; charset=utf-8§Express§W/\"5-H\"§§§§§§§§nosniff§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|hello",
  "idp-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "idp-badoid|404|application/json; charset=utf-8§Express§W/\"5e-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.project_id\\\"\",\"statusCode\":404}",
  "idp-ghost|404|text/plain; charset=utf-8§Express§W/\"9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Not Found",
  "idp-valid|200|application/json; charset=utf-8§Express§W/\"22-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"name\":\"uapi-wire\",\"features\":{}}",
  "deact-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "deact-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "deact-badoid|404|application/json; charset=utf-8§Express§W/\"5e-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.project_id\\\"\",\"statusCode\":404}",
  "deact-ghost|200|text/plain; charset=utf-8§Express§W/\"2-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|OK",
  "join-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "join-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "join-badparam|404|application/json; charset=utf-8§Express§W/\"5e-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.Project_id\\\"\",\"statusCode\":404}",
  "join-nobody|400|application/json; charset=utf-8§Express§W/\"b5-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected string, received undefined at \\\"body.userId\\\" or Invalid input: expected \\\"anonymous-user\\\" at \\\"body.userId\\\"\",\"statusCode\":400}",
  "join-baduid|400|application/json; charset=utf-8§Express§W/\"58-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"body.userId\\\"\",\"statusCode\":400}",
  "join-nonstring|400|application/json; charset=utf-8§Express§W/\"b2-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected string, received number at \\\"body.userId\\\" or Invalid input: expected \\\"anonymous-user\\\" at \\\"body.userId\\\"\",\"statusCode\":400}",
  "join-unknownkey|400|application/json; charset=utf-8§Express§W/\"56-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Unrecognized key: \\\"extra\\\" at \\\"body\\\"\",\"statusCode\":400}",
  "join-bothbad|404|application/json; charset=utf-8§Express§W/\"89-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.Project_id\\\"; Invalid Mongo ObjectId at \\\"body.userId\\\"\",\"statusCode\":404}",
  "join-ghost|404|text/plain; charset=utf-8§Express§W/\"9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Not Found",
  "join-anon|403|text/plain; charset=utf-8§Express§W/\"9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Forbidden",
  "join-owner200|200|application/json; charset=utf-8§Express§W/\"3ad-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"project\":{\"_id\":\"HEX\",\"name\":\"uapi-wire\",\"rootFolder\":[{\"_id\":\"HEX\",\"name\":\"\",\"folders\":[{\"_id\":\"HEX\",\"name\":\"gu-g\",\"folders\":[{\"_id\":\"HEX\",\"name\":\"gu-n\",\"folders\":[],\"fileRefs\":[],\"docs\":[]}],\"fileRefs\":[],\"docs\":[]}],\"fileRefs\":[],\"docs\":[{\"_id\":\"HEX\",\"name\":\"doc.tex\"}]}],\"publicAccesLevel\":\"private\",\"dropboxEnabled\":false,\"grammarPicky\":true,\"deletedByExternalDataSource\":false,\"owner\":{\"_id\":\"HEX\"},\"members\":[],\"invites\":[],\"editAccessRequests\":[],\"features\":{\"collaborators\":-1,\"versioning\":false,\"dropbox\":false,\"compileTimeout\":60,\"compileGroup\":\"standard\",\"templates\":false,\"references\":false,\"referencesSearch\":false,\"mendeley\":false,\"trackChanges\":false,\"trackChangesVisible\":true,\"symbolPalette\":false}},\"privilegeLevel\":\"owner\",\"isRestrictedUser\":false,\"isTokenMember\":false,\"isInvitedMember\":true}",
  "rsync-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "rsync-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "rsync-badparam|404|application/json; charset=utf-8§Express§W/\"5e-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.Project_id\\\"\",\"statusCode\":404}",
  "rsync-badenum|400|application/json; charset=utf-8§Express§W/\"8c-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid option: expected one of \\\"forwards\\\"|\\\"backwards\\\" at \\\"body.historyRangesMigration\\\"\",\"statusCode\":400}",
  "rsync-badbool|400|application/json; charset=utf-8§Express§W/\"86-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid input: expected boolean, received number at \\\"body.resyncProjectStructureOnly\\\"\",\"statusCode\":400}",
  "rsync-unknown|400|application/json; charset=utf-8§Express§W/\"54-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Unrecognized key: \\\"foo\\\" at \\\"body\\\"\",\"statusCode\":400}",
  "rsync-bothbad|404|application/json; charset=utf-8§Express§W/\"bd-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.Project_id\\\"; Invalid option: expected one of \\\"forwards\\\"|\\\"backwards\\\" at \\\"body.historyRangesMigration\\\"\",\"statusCode\":404}",
  "rsync-enumbool|400|application/json; charset=utf-8§Express§W/\"e5-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid option: expected one of \\\"forwards\\\"|\\\"backwards\\\" at \\\"body.historyRangesMigration\\\"; Invalid input: expected boolean, received number at \\\"body.resyncProjectStructureOnly\\\"\",\"statusCode\":400}",
  "rsync-nullenum|400|application/json; charset=utf-8§Express§W/\"8c-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid option: expected one of \\\"forwards\\\"|\\\"backwards\\\" at \\\"body.historyRangesMigration\\\"\",\"statusCode\":400}",
  "rsync-disabled|404|text/plain; charset=utf-8§Express§W/\"9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Not Found",
  "rsync-ghost|500|text/plain; charset=utf-8§Express§W/\"15-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Internal Server Error",
  "exp-proj-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "exp-proj-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "exp-proj-badoid|404|application/json; charset=utf-8§Express§W/\"5d-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.projectId\\\"\",\"statusCode\":404}",
  "exp-proj-active|200|text/plain; charset=utf-8§Express§W/\"2-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|OK",
  "exp-proj-ghost|404|text/plain; charset=utf-8§Express§W/\"9-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Not Found",
  "exp-user-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "exp-user-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "exp-user-badoid|404|application/json; charset=utf-8§Express§W/\"5a-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.userId\\\"\",\"statusCode\":404}",
  "zip-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "zip-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "zip-badoid|404|application/json; charset=utf-8§Express§W/\"5e-H\"§§§§§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|{\"error\":\"Validation error: Invalid Mongo ObjectId at \\\"params.Project_id\\\"\",\"statusCode\":404}",
  "bulk-proj-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "bulk-proj-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "bulk-user-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "bulk-user-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "deactold-unauth|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized",
  "deactold-wrong|401|text/plain; charset=utf-8§Express§W/\"c-H\"§§§§OverleafLogin§§§§§§§§§base-uri 'none'; default-src 'none'; form-action 'none'; frame-ancestors 'none'; img-src 'self'|Unauthorized"]

test('uapi-doc: api-profile wire contract (canonical Go api :3000)', async () => {
  test.setTimeout(600_000)
  const lines = normalizeLines(runMatrix())
  const pinLines = normalizeLines(CONTRACT)
  expect(lines.length, 'line count').toBe(CONTRACT.length)
  for (let i = 0; i < CONTRACT.length; i++) {
    if (lines[i] !== pinLines[i]) {
      const a = lines[i] || '<missing>'
      const b = pinLines[i]
      const m = a.match(/^[a-z0-9-]+/)
      throw new Error(`row ${i} [${m ? m[0] : '?'}]:\n  live    : ${a.slice(0, 300)}\n  expected: ${b.slice(0, 300)}`)
    }
  }
})

test('uapi-doc: 2-run stability (byte parity, canonical Go api :3000)', async () => {
  test.setTimeout(900_000)
  const a = normalizeLines(runMatrix()).join('\n')
  const b = normalizeLines(runMatrix()).join('\n')
  if (a !== b) {
    const A = a.split('\n'), B = b.split('\n')
    let i = 0
    while (i < Math.min(A.length, B.length) && A[i] === B[i]) i++
    throw new Error(`run divergence @line ${i}/${A.length}:${B.length}\n  A: ${A[i] ? A[i].slice(0, 300) : ''}\n  B: ${B[i] ? B[i].slice(0, 300) : ''}`)
  }
})
