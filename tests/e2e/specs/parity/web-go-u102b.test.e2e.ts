/**
 * U10.2b parity gate — entity rename / move / duplicate (WEB_GO_PLAN.md
 * U10.2 "residual web surface (entity ops)"):
 *
 *   POST /project/:pid/:type/:id/rename   (no limiter)
 *   POST /project/:pid/:type/:id/move     (no limiter)
 *   POST /project/:pid/:type/:id/duplicate
 *        (limiter add-folder-to-project 60/60, key <pid>:<uid>)
 *
 * Node oracle surface (pinned live 2026-09-22):
 *   - auth: ensureUserCanWriteProjectContent — anon 403 text "Forbidden";
 *     non-member 403 {"message":"restricted"} (JSON) / restricted page
 *     (HTML); ghost project → 404 page
 *   - rename VA: params → 404 JSON; strict body multi-issue "; " join;
 *     JS-utf16 name gate (null/''/>=150) → bare 400 "Bad Request";
 *     SafePath.clean → 400 "invalid element name"; top blocked → 400
 *     "blocked element name"; parent-name dup (incl. self) → 400 "file
 *     already exists"; success → 204 + DU rename-{doc|file} op
 *     (folder renames → ZERO ops → DU call skipped)
 *   - move VA: folder_id oid; findEntity dest missing → 404 page;
 *     folder self/descendant → 400 "destination folder is the same as
 *     me" / "…child folder of me"; _putElement: clean/2000/path>1024/
 *     blocked/dup → 400; success → 204 (push+pull, version +2, DU rename
 *     op for doc|file)
 *   - duplicate: type gate → bare 400; ghost project → 404 page (gate);
 *     ghost entity / kind mismatch → bare 404; name = a_copy → a_copy(1);
 *     doc → docstore POST {lines, version:0, ranges:{}} (BEFORE lock) +
 *     $push + 200 {"name","_id"}; file → new File (mongo order
 *     name,created,rev,linkedFileData,hash,_id) + DU add-file
 *     {historyRangesSupport,hash,createdBlob:true} + editor-events
 *     reciveNewFile + 200 {"name","rev","linkedFileData","hash","_id",
 *     "created"}
 *
 * State parity: project tree + version + lastUpdatedBy; DU ops redis list
 * (ProjectHistory:Ops:{pid}, P4.13b idiom); docstore GET of the new
 * duplicate doc; editor-events capture window.
 *
 * Node==Go==Node 3-leg gate; per-leg ids/timestamps normalized
 * (first-seen hex24 → H<n>, ISO ts → TS, nonce/csrf, ETag hash, event
 * counter id).
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const redisC = 'ol-e2e-redis-1'
const BATTERY = `${process.cwd()}/specs/parity/u102b-matrix.cjs`
const NODE = 'http://127.0.0.1:4000'
const GO = 'http://127.0.0.1:4010'

function dexe(c: string, cmd: string): string {
  return (execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 256 * 1024 * 1024 }) || '').trim()
}

function dexeQ(c: string, code: string): string {
  const out = dexe(c, `mongosh --quiet sharelatex --eval '${code.replace(/'/g, `'\\''`)}'`)
  return out
}

function redisRaw(args: string[]): string {
  return (execFileSync('docker', ['exec', redisC, 'redis-cli', ...args], { encoding: 'utf8', stdio: 'pipe' }) || '').trim()
}

function probe(url: string): string {
  let out = ''
  try {
    out = dexe(overleafC, `curl -s -m 3 -o /dev/null -w "%{http_code}" ${url}`)
  } catch (e: any) {
    out = e && e.stdout ? e.stdout.toString() : ''
  }
  return (out || '').trim().split('\n')[0]
}

async function waitUp(): Promise<void> {
  for (let i = 0; i < 60; i++) {
    if (probe(`${NODE}/status`) === '200' && probe(`${GO}/status`) === '200') return
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error(`services not up (node=${probe(NODE + '/status')} go=${probe(GO + '/status')})`)
}

function flushLimits(): void {
  for (const p of ['rate-limit:add-folder-to-project:*', 'rate-limit:overleaf-login:*']) {
    try {
      const keys = redisRaw(['--scan', '--pattern', p]).split('\n').filter(Boolean)
      if (keys.length) redisRaw(['DEL', ...keys])
    } catch {}
  }
}

function seed(leg: string): { ids: Record<string, string>; clean: string } {
  const name = `WebGo-U102b-${leg}`
  const out = dexe(mongoC, `cat > /tmp/seed-u102b.js <<'SEED'
const f = db.getSiblingDB('sharelatex');
f.projects.deleteMany({ name: "${name}" });
const uid = f.users.findOne({ email: 'e2e-user@e2e.test' })._id;
const now = new Date();
const rootId = new ObjectId();
const did = new ObjectId(); const did1 = new ObjectId(); const d2 = new ObjectId();
const hid = new ObjectId(); const fid = new ObjectId();
const sub = new ObjectId(); const outer = new ObjectId(); const osub = new ObjectId();
const proj = {
  _id: new ObjectId(), name: "${name}", owner: uid, isCollaborative: true, version: 1,
  lastUpdated: now, lastUpdatedBy: uid,
  owner_ref: uid, collab_refs: [uid], readOnly_refs: [],
  tokenAccessReadAndWrite_refs: [], tokenAccessReadOnly_refs: [],
  publicAccesLevel: 'private', pendingEditor_refs: [], reviewer_refs: [], pendingReviewer_refs: [],
  rootFolder: [{ _id: rootId, name: '',
    docs: [
      { name: 'main.tex', _id: did },
      { name: 'd1.txt', _id: did1 },
      { name: 'd2.txt', _id: d2 }
    ],
    fileRefs: [{ name: 'f1.bin', created: now, rev: 0, linkedFileData: null, hash: 'cafedeadcafedeadcafedeadcafedeadcafedead', _id: fid }],
    folders: [
      { _id: sub, name: 'sub', docs: [], fileRefs: [], folders: [] },
      { _id: outer, name: 'outer', docs: [], fileRefs: [], folders: [
        { _id: osub, name: 'osub', docs: [], fileRefs: [], folders: [] }
      ]}
    ] }],
  collaborators: [{ type: 'user', id: uid, access: 'owner', accepted: true }],
  overleaf: { history: { id: hid, rangeMap: {} } },
};
ins = f.projects.insertOne(proj);
print(JSON.stringify({ pj: String(proj._id), rf: String(rootId), main: String(did), d1: String(did1), d2: String(d2), f1: String(fid), sub: String(sub), outer: String(outer), osub: String(osub) }));
SEED
mongosh --quiet sharelatex /tmp/seed-u102b.js | tail -1`)
  const line = out.split('\n').filter((l) => l.startsWith('{')).pop() || ''
  let ids: Record<string, string>
  try {
    ids = JSON.parse(line)
  } catch {
    throw new Error(`seed parse failed: ${out.slice(0, 300)}`)
  }
  // docstore registration of the real docs (main.tex/d1.txt/d2.txt share the
  // same lines — Node getDoc on a fresh doc returns [] anyway, but the
  // duplicate-doc docstore POST must succeed on BOTH stacks: seed the
  // source lines once).
  for (const did of [ids.d1, ids.d2]) {
    const reg = dexe(
      overleafC,
      `PJID=${ids.pj} DID=${did} /usr/bin/node --input-type=module -e '
      (async () => {
        const r = await fetch("http://127.0.0.1:3016/project/" + process.env.PJID + "/doc/" + process.env.DID, {
          method: "POST", headers: { "content-type": "application/json" },
          body: JSON.stringify({ lines: ["L1", "L2"], version: 0, ranges: {} })
        });
        console.log("reg=" + r.status);
      })().catch((e) => console.log("ERR " + e));
    ' 2>&1`,
    )
    if (!/reg=200/.test(reg)) throw new Error(`docstore seed failed: ${reg.slice(0, 200)}`)
  }
  const clean = dexe(redisC, `for k in $(redis-cli --scan --pattern "ProjectHistory:Ops:{${ids.pj}}"); do redis-cli DEL "$k"; done; for k in $(redis-cli --scan --pattern "rate-limit:add-folder-to-project:${ids.pj}*"); do redis-cli DEL "$k"; done; echo CLEANED`)
  return { ids, clean }
}

// ---------- normalization ----------
const ERE = /W\/"([0-9a-f]+)-[A-Za-z0-9+/=_-]+["\\]*/g
// NOTE: applied TWICE — the ETag header value is JSON-escaped inside the
// serialized record ("W/\"9-BASE64\""), so one pass strips the visible
// hash but can leave a re-formed match at the boundary.
const normBase = (s: string): string =>
  s
    .replace(/nonce="[^"]*"/g, 'nonce="N"')
    .replace(/nonce-[A-Za-z0-9+/=]+/g, 'nonce-X')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="C"')
    .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="C"')
    .replace(/expires=[^;]+/gi, 'expires=E')
    .replace(ERE, 'W/"$1-HASH"').replace(ERE, 'W/"$1-HASH"')
    .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z/g, 'TS')
  // realtime event ids (web:<host>:<seq>) — host-specific, not a contract
    .replace(/web:[a-z]+:[0-9a-f]+-\d+/g, 'web:EVT')

const TRANSPORT = new Set(['connection', 'keep-alive', 'date'])

type LegData = { recs: any[]; state: any; events: string }

// hex24 → first-seen token (per leg), applied across recs+state+events.
// Shared across legs: assign H tokens by SORTED hex so the same logical
// id gets the same token on every leg (first-seen order legitimately
// differs between Node and Go renderings and is not a wire contract —
// the *set* of ids is what parity pins).
const HEX_MAP: Record<string, string> = {}
let HEX_N = 0
// slot tokens for per-leg fixture ids (project, root folder, d1, d2,
// f1, sub, outer, osub — new ObjectIDs per leg by construction): they
// are the same *logical* slot on every leg, so they pin to fixed labels.
const SLOT_TOK: Record<string, string> = {}
function slotIds(ids: Record<string, string>): void {
  if (ids.pj) SLOT_TOK[ids.pj] = 'PJ'
  if (ids.rf) SLOT_TOK[ids.rf] = 'RF'
  if (ids.d1) SLOT_TOK[ids.d1] = 'D1'
  if (ids.d2) SLOT_TOK[ids.d2] = 'D2'
  if (ids.f1) SLOT_TOK[ids.f1] = 'F1'
  if (ids.sub) SLOT_TOK[ids.sub] = 'SUB'
  if (ids.outer) SLOT_TOK[ids.outer] = 'OUTER'
  if (ids.osub) SLOT_TOK[ids.osub] = 'OSUB'
}

""
// per-leg dup sequences: the Nth successful duplicate of a source element
// (slot|kind) occurs at a fixed battery position, so pin (leg, slot, kind,
// nth). Each leg's own numbering must be stable: leg 1 uses its own list,
// etc. — all legs run the same battery, so positions match.
const LEG_DUPS: { [k: string]: { [k2: string]: string[] } } = {}
function dupSeqFor(leg: string): { [k: string]: string[] } {
  if (!LEG_DUPS[leg]) LEG_DUPS[leg] = {}
  return LEG_DUPS[leg]
}
function registerDup(leg: string, slot: string, kind: string, hex: string): void {
  const m = dupSeqFor(leg)
  const k = slot + '|' + kind
  if (!m[k]) m[k] = []
  const arr = m[k]
  if (arr.indexOf(hex) < 0) arr.push(hex)
}
let CUR_LEG = 'L1'
function hexTok(h: string): string {
  if (SLOT_TOK[h]) return SLOT_TOK[h]
  for (const leg of Object.keys(LEG_DUPS)) {
    const m = LEG_DUPS[leg]
    for (const k of Object.keys(m)) {
      const i = m[k].indexOf(h)
      if (i >= 0) {
        const tok = 'DUP' + k + '#' + i
        if (!(h in HEX_MAP)) HEX_MAP[h] = tok
        return HEX_MAP[h]
      }
    }
  }
  if (!(h in HEX_MAP)) HEX_MAP[h] = 'H' + HEX_N++
  return HEX_MAP[h]
}
function dupIdsFromRecs(legName: string, leg: LegData): void {
  // response bodies for successful dups are of the shape
  //   doc:  {"name":"<n>","_id":"<hex>"}
  //   file: {"name":"<n>","rev":0,...,"_id":"<hex>","created":"..."}
  for (const r of leg.recs as any[]) {
    if (r.status !== 200 || typeof r.body !== 'string') continue
    const m = r.body.match(/\{"name":"([^"{}]+)_copy(\(\d+\))?\.[^"]*","_id":"([0-9a-f]{24})"/)
      || r.body.match(/\{"name":"([^"{}]+)_copy(\(\d+\))?\.bin","rev":0,"linkedFileData":[^,]*,"hash":"[0-9a-f]+","_id":"([0-9a-f]{24})"/)
    if (!m) continue
    const name = m[1]
    const hex = m[3]
    const lower = name.toLowerCase()
    let slot = 'X'
    if (lower.indexOf('d1') >= 0) slot = 'D1'
    else if (lower.indexOf('d2') >= 0) slot = 'D2'
    else if (lower.indexOf('f1') >= 0) slot = 'F1'
    registerDup(legName, slot, r.body.indexOf('"rev":0') >= 0 ? 'file' : 'doc', hex)
  }
}

function tokenizeShared(legName: string, leg: LegData): void {
  dupIdsFromRecs(legName, leg)
  const collect = (val: any): void => {
    if (typeof val === 'string') {
      for (const m of val.matchAll(/[0-9a-f]{24}/g)) hexTok(m[0])
    } else if (val && typeof val === 'object') for (const k of Object.keys(val)) collect((val as any)[k])
  }
  for (const r of leg.recs) collect(r)
  collect(leg.state)
  collect(leg.events)
}

function tokenize(leg: LegData): void {
  try {
  const replace = (s: string): string => {
    let out = s.replace(/[0-9a-f]{24}/g, (h) => hexTok(h))
    // the hex->token swap changes string length, so a previously
    // non-matching boundary can form a new 24-hex run; sweep once more
    out = out.replace(/[0-9a-f]{24}/g, (h) => hexTok(h))
    return out
  }
  const apply = (val: any): any => {
    if (typeof val === 'string') return replace(normBase(val))
    if (Array.isArray(val)) return val.map(apply)
    if (val && typeof val === 'object') {
      const o: Record<string, any> = {}
      for (const k of Object.keys(val)) o[k] = apply((val as any)[k])
      return o
    }
    return val
  }
  const normCookie = (v: any): any =>
    typeof v === 'string' ? v.replace(/=s%3A[A-Za-z0-9._~-]+/g, '=s%3A<SESS>') : v
  const normH = (v: any): any => {
    if (typeof v === 'string') return replace(normBase(v))
    if (Array.isArray(v)) return v.map(normH)
    if (v && typeof v === 'object') {
      const o: Record<string, any> = {}
      for (const k of Object.keys(v)) o[k] = normH((v as any)[k])
      return o
    }
    return v
  }
  const sorted = (o: any): any => {
    if (Array.isArray(o)) return o.map(sorted)
    if (o && typeof o === 'object') {
      const out: Record<string, any> = {}
      for (const k of Object.keys(o).sort()) out[k] = sorted((o as any)[k])
      return out
    }
    return o
  }
  for (const r of leg.recs) {
    for (const k of Object.keys(r)) {
      if (k === 'headers') continue
      r[k] = apply((r as any)[k])
    }
    const h: Record<string, string> = {}
    for (const k of Object.keys((r.headers || {}) as Record<string, string>)) {
      const lk = String(k).toLowerCase()
      if (TRANSPORT.has(lk)) continue
      h[lk] = normH((r.headers as any)[k])
    }
    // set-cookie session value differs per leg by construction; pin the
    // shape and normalize the value (an honest oracle: the session token
    // is a per-login random, not a wire contract).
    if (h['set-cookie']) h['set-cookie'] = String(h['set-cookie']).replace(/s%3A[A-Za-z0-9._~+%;-]+/g, 's%3A<SESS>')
    // key order is not a wire contract (header maps are unordered): sort
    r.headers = sorted(h)
  }
  leg.state = apply(leg.state)
  leg.state = sorted(leg.state) as LegData['state']
  leg.events = replace(normBase(leg.events))
  process.stdout.write('TOKENIZE-OK setcookie=' + JSON.stringify((leg.recs[0] as any).headers && (leg.recs[0] as any).headers['set-cookie']).slice(0, 120) + ' | etag=' + JSON.stringify((leg.recs[0] as any).headers && (leg.recs[0] as any).headers['etag']) + ' | keys=' + Object.keys((leg.recs[0] as any).headers || {}).join(',') + '\n')
  } catch (e) {
    process.stdout.write('TOKENIZE-FAIL ' + String(e) + '\n')
    throw e
  }
}

// state capture (mongo + redis) — post-battery.
function captureState(leg: LegData, ids: Record<string, string>): void {
  const tree = dexe(mongoC, `mongosh --quiet sharelatex --eval '
    const f = db.getSiblingDB("sharelatex");
    const d = f.projects.findOne({_id: ObjectId("${ids.pj}")});
    const out = [];
    (function rec(fl, path){
      for (const x of (fl.docs||[])) out.push("doc:"+x.name);
      for (const x of (fl.fileRefs||[])) out.push("file:"+x.name);
      for (const s of (fl.folders||fl.folders||[])) { out.push("folder:"+s.name); rec(s, path+"/"+s.name); }
    })(d.rootFolder[0], "/");
    // duplicate doc: the newest root doc that is NOT one of the seed names.
    const seed = new Set(["main.tex","d1.txt","d2.txt"]);
    const extra = (d.rootFolder[0].docs||[]).filter(x=>!seed.has(x.name)).map(x=>x.name).sort();
    print(JSON.stringify({ tree: out.sort(), version: d.version, lub: String(d.lastUpdatedBy), extraDocs: extra, extraFiles: (d.rootFolder[0].fileRefs||[]).filter(x=>x.name!=="f1.bin").map(x=>x.name).sort() }));
  ' | tail -1`)
  let state: any = {}
  try {
    const l2 = tree.split('\n').filter((l) => l.startsWith('{')).pop() || '{}'
    state = JSON.parse(l2)
  } catch {
    state = { raw: tree.slice(0, 300) }
  }
  // DU ops (P4.13b idiom)
  let ops: string[] = []
  try {
    const type = redisRaw(['TYPE', `ProjectHistory:Ops:${ids.pj}`])
    if (type.toLowerCase() === 'list') {
      ops = redisRaw(['LRANGE', `ProjectHistory:Ops:${ids.pj}`, '0', '-1']).split('\n').filter((l) => l.trim().startsWith('{'))
    } else {
      const v = redisRaw(['GET', `ProjectHistory:Ops:${ids.pj}`])
      if (v) ops = [v]
    }
  } catch {
    ops = []
  }
  state.duOps = ops.map((o) =>
    o
      .replace(/"ts"\s*:\s*"[^"]*"/, '"ts":"TS"')
      .replace(/"ts"\s*:\s*\d+(\.\d+)?/, '"ts":"TS"')
      .replace(/"importedAt"\s*:\s*"[^"]*"/, '"importedAt":"TS"'),
  )
  // duplicate doc lines (docstore) — the first extra doc's id via a second
  // normalized GET against the docstore for each seeded project
  let dupDoc = 'n/a'
  try {
    // find the extra doc id from mongo
    const didOut = dexe(mongoC, `mongosh --quiet sharelatex --eval '
      const f = db.getSiblingDB("sharelatex");
      const d = f.projects.findOne({_id: ObjectId("${ids.pj}")});
      const seed = new Set(["main.tex","d1.txt","d2.txt"]);
      const ex = (d.rootFolder[0].docs||[]).filter(x=>!seed.has(x.name)).map(x=>x.name).sort();
      if (ex.length) print(String((d.rootFolder[0].docs||[]).find(x=>x.name===ex[0])._id));
    ' | tail -1`)
    const did = didOut.trim().split('\n').pop()
    if (/^[0-9a-f]{24}$/.test(did)) {
      const got = dexe(
        overleafC,
        `curl -s http://127.0.0.1:3016/project/${ids.pj}/doc/${did}`,
      )
      dupDoc = got
        .replace(/"rev"\s*:\s*\d+/, '"rev":R')
        .replace(/"version"\s*:\s*\d+/, '"version":V')
    }
  } catch {
    dupDoc = 'n/a'
  }
  state.dupDoc = dupDoc
  leg.state = state
}

// ---------- editor-events capture ----------
// The battery runs sequentially inside one container exec; start a
// redis-cli SUBSCRIBE recorder just before, stop after (per leg).
function runEventsRecorder(start: boolean): string {
  const cmd = start
    ? 'nohup sh -c "redis-cli SUBSCRIBE editor-events > /tmp/u102b-events.raw 2>&1 &" >/dev/null 2>&1; echo STARTED'
    : 'pkill -f "redis-cli SUBSCRIBE editor-events" >/dev/null 2>&1; sleep 0.3; echo STOPPED'
  try {
    return execFileSync('docker', ['exec', redisC, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe' }) || ''
  } catch {
    return 'OK'
  }
}

function runLeg(base: string, leg: string): LegData {
  const { ids, clean } = seed(leg)
  slotIds(ids)
  flushLimits()
  runEventsRecorder(true)
  const data: LegData = { recs: [], state: {}, events: '' }
  execFileSync('docker', ['cp', BATTERY, `${overleafC}:/tmp/u102b-matrix.cjs`], { stdio: ['ignore', 'pipe', 'pipe'] })
  const out = execFileSync(
    'docker',
    ['exec', '-e', 'LEG=' + (base.endsWith('4010') ? '2' : '1'), '-e', 'U2B_IDS=' + JSON.stringify(ids), overleafC, 'sh', '-c', 'node /tmp/u102b-matrix.cjs 2>&1'],
    { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 256 * 1024 * 1024 },
  )
  runEventsRecorder(false)
  const lines = (out || '').trim().split('\n')
  for (const ln of lines) if (ln.startsWith('CASE ')) console.log(`  [${leg}] ${ln.slice(5)}`)
  const jline = lines.slice().reverse().find((l: string) => l.startsWith('BATTERY-JSON:'))
  if (!jline) throw new Error(`battery(leg=${leg}) no JSON in output (last lines: ${(lines.slice(-6)).join(' | ')})`)
  let recs: any[]
  try {
    recs = JSON.parse(jline.slice('BATTERY-JSON:'.length))
  } catch (e: any) {
    throw new Error(`battery(leg=${leg}) parse: ${String(e).slice(0, 160)} | line=${jline.slice(0, 200)}`)
  }
  const errs = recs.filter(r => r.err)
  if (errs.length > 0) {
    // keep going: capture partial state + print all case statuses
    const brief = recs.map(r => `${r.tag}=${r.status}${r.err ? '!' : ''}`).join(' ')
    throw new Error(`battery(leg=${leg}) ${errs.length} case err(s): ${errs.map(e => `${e.tag}:${e.err}`).join(' | ')}\nALL: ${brief}`)
  }
  data.recs = recs
  captureState(data, ids)
  try {
    data.events = dexe(overleafC, 'cat /dev/null') // events are in the redis container
    const rawEvents = execFileSync('docker', ['exec', redisC, 'tail', '-200', '/tmp/u102b-events.raw'], { encoding: 'utf8', stdio: 'pipe' }) || ''
    data.events = rawEvents
  } catch {
    data.events = ''
  }
  void clean
  return data
}

test.describe.configure({ timeout: 1200_000 })

test('u102b: entity rename/move/duplicate Node==Go==Node (3 legs)', async () => {
  await waitUp()
  flushLimits()
  const leg1 = runLeg(NODE, 'L1')
  const leg2 = runLeg(GO, 'L2')
  const leg3 = runLeg(NODE, 'L3')
  expect(leg1.recs.length).toBe(leg2.recs.length)
  expect(leg1.recs.length).toBeGreaterThanOrEqual(60)
  // shared hex→token numbering (must be complete before any replacement)
  tokenizeShared('L1', leg1)
  tokenizeShared('L2', leg2)
  tokenizeShared('L3', leg3)
  tokenize(leg1)
  tokenize(leg2)
  tokenize(leg3)
  const sameA = JSON.stringify(leg1) === JSON.stringify(leg2)
  const sameB = JSON.stringify(leg2) === JSON.stringify(leg3)
  if (!sameA || !sameB) {
    const findDiff = (a: any, b: any): string[] => {
      const diffs: string[] = []
      const ra = a.recs || []
      const rb = b.recs || []
      for (let i = 0; i < Math.max(ra.length, rb.length); i++) {
        const x = ra[i]
        const y = rb[i]
        if (!x || !y) {
          diffs.push(`idx ${i}: missing on one side (${x ? x.tag : '?'}/${y ? y.tag : '?'})`)
          continue
        }
        if (JSON.stringify(x) !== JSON.stringify(y)) {
        diffs.push(`idx ${i} (${x.tag})`)
        if (process.env.U2B_DBG) {
          const dx = JSON.stringify(x)
          const dy = JSON.stringify(y)
          let at = 0
          while (at < Math.min(dx.length, dy.length) && dx[at] === dy[at]) at++
          console.log(`DBG ${x.tag}: first diff at ${at}`)
          console.log(`  N: ${JSON.stringify(dx.slice(Math.max(0, at - 80), at + 160))}`)
          console.log(`  G: ${JSON.stringify(dy.slice(Math.max(0, at - 80), at + 160))}`)
        }
      }
      }
      if (JSON.stringify(a.state) !== JSON.stringify(b.state)) diffs.push('state')
      if (JSON.stringify(a.events) !== JSON.stringify(b.events)) diffs.push('events')
      return diffs
    }
    const da = findDiff(leg1, leg2)
    const db = findDiff(leg2, leg3)
    const dump = (recs: any[]): void => {
      for (const i of da.concat(db)) {
        if (!i.includes('idx')) continue
        const m = /^idx (\d+).*\((.*)\)/.exec(i)
        if (!m) continue
        const idx = Number(m[1])
        const tag = m[2]
        expect.soft(false, `case ${tag} differs`).toBe(true)
        console.log(`NODE[${idx}] =`, JSON.stringify(leg1.recs[idx] || leg3.recs[idx]).slice(0, 500))
        console.log(`GO  [${idx}] =`, JSON.stringify(leg2.recs[idx]).slice(0, 500))
      }
      if (da.includes('state') || db.includes('state')) {
        console.log('STATE N =', JSON.stringify(leg1.state).slice(0, 1500))
        console.log('STATE G =', JSON.stringify(leg2.state).slice(0, 1500))
      }
      if (da.includes('events') || db.includes('events')) {
        console.log('EVENTS N =', String(leg1.events).slice(0, 1000))
        console.log('EVENTS G =', String(leg2.events).slice(0, 1000))
      }
    }
    dump(leg1.recs)
  }
  expect(sameA, 'Node(leg1) == Go(leg2)').toBe(true)
  expect(sameB, 'Go(leg2) == Node(leg3)').toBe(true)
  console.log(`U10.2b: legs=3 cases=${leg1.recs.length} duOps(L2)=${leg2.state.duOps ? leg2.state.duOps.length : 'n/a'} — parity GREEN`)
})
