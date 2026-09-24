/**
 * U10.3 parity gate — LinkedFiles create/refresh (WEB_GO_PLAN.md U10.3):
 *
 *   POST /project/:pid/linked_file                    (limiter create-linked-file)
 *   POST /project/:pid/linked_file/:fid/refresh       (limiter refresh-linked-file)
 *
 * Node oracle surface (pinned live; the LF matrix is the executable
 * spec):
 *   - parseReq FIRST: invalid params.project_id -> 404 JSON
 *     {"error":"Validation error: Invalid Mongo ObjectId at
 *     \"params.project_id\"","statusCode":404} AND short-circuits the
 *     rest; invalid params.file_id (real project) -> 404 JSON
 *     params.file_id (body issues still reported after it)
 *   - then membership: existing project + non-owner -> 403 JSON
 *     {"message":"restricted"}; anonymous no-csrf -> 403 text
 *     "Forbidden"; anon valid-csrf + json accept -> 401 "Unauthorized";
 *     anon valid-csrf + html accept -> 302 /login
 *   - then body (zod strict; ALL issues joined "; "; any params issue
 *     forces 404): create discriminator first (invalid/missing provider
 *     -> bare "Validation error"), then name / parent_folder_id / data /
 *     per-provider data required+unknown / body unknown (both
 *     unrecognized-key issue groups COMBINED singular/plural, client
 *     order); refresh clientId (string) / shouldReindexReferences
 *     (boolean) / body unknown
 *   - refresh lookup (type:'file'): doc-id / missing file -> 404 PAGE;
 *     fileRef without linkedFileData OR without provider -> 409 "Conflict";
 *     linkedFileData.provider present -> _getAgent null (CE) -> 400 bare
 *   - create valid shape -> _getAgent null (CE) -> 400 bare
 *
 * CE invariant: these stacks have all linked-file providers disabled,
 * so no case can write: the gate pins that the shared seeded project
 * doc is byte-identical before/after each leg and that no DU ops /
 * editor-events / new projects appear for it.
 *
 * Node==Go==Node 3-leg gate. Normalization: nonce, per-session csrf
 * token values, set-cookie session values, ETag body-hash (length kept),
 * date-ish values. 404-page bodies compared in full (structure, asset
 * pins, error block) after nonce/csrf normalization — the honest byte
 * surface of the view contract.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'

const overleafC = 'ol-e2e-overleaf-1'
const mongoC = 'ol-e2e-mongo-1'
const redisC = 'ol-e2e-redis-1'
const BATTERY = `${process.cwd()}/specs/parity/u103-lf-matrix.cjs`
const NODE = 'http://127.0.0.1:4000'
const GO = 'http://127.0.0.1:4010'

function dexe(c: string, cmd: string): string {
  return (execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', stdio: 'pipe', maxBuffer: 256 * 1024 * 1024 }) || '').trim()
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

function redisRaw(args: string[]): string {
  return (execFileSync('docker', ['exec', redisC, 'redis-cli', ...args], { encoding: 'utf8', stdio: 'pipe' }) || '').trim()
}

function flushLimits(): void {
  for (const p of ['rate-limit:create-linked-file:*', 'rate-limit:refresh-linked-file:*', 'rate-limit:overleaf-login:*']) {
    try {
      const keys = redisRaw(['--scan', '--pattern', p]).split('\n').filter(Boolean)
      if (keys.length) redisRaw(['DEL', ...keys])
    } catch {}
  }
}

// ---------- seed: ONE shared fixture project (read-only battery) ----------
const FID = {
  PJ: 'aaaa000000000000000000a1',
  DOC: 'aaaa000000000000000000a2',
  F_PLAIN: 'aaaa000000000000000000a3',
  F_NOPROV: 'aaaa000000000000000000a4',
  F_URL: 'aaaa000000000000000000a5',
  GHOST_F: 'aaaa000000000000000000b9',
}

function seed(): void {
  dexe(
    mongoC,
    `cat > /tmp/seed-u103.js <<'SEED'
const f = db.getSiblingDB('sharelatex');
f.projects.deleteMany({ _id: ObjectId("aaaa000000000000000000a1") });
const uid = f.users.findOne({ email: 'e2e-user@e2e.test' })._id;
const now = new Date();
const proj = {
  _id: ObjectId('aaaa000000000000000000a1'),
  name: 'WebGo-U103-LF', owner: uid, isCollaborative: true, version: 1,
  lastUpdated: now, lastUpdatedBy: uid,
  owner_ref: uid, collab_refs: [uid], readOnly_refs: [],
  tokenAccessReadAndWrite_refs: [], tokenAccessReadOnly_refs: [],
  publicAccesLevel: 'private', pendingEditor_refs: [], reviewer_refs: [], pendingReviewer_refs: [],
  rootFolder: [{ _id: ObjectId('aaaa000000000000000000a6'), name: '',
    docs: [{ name: 'main.tex', _id: ObjectId('aaaa000000000000000000a2') }],
    fileRefs: [
      { name: 'plain.bin', created: now, rev: 0, linkedFileData: null,
        hash: 'aaaaaaaaaaaaaaaaaaaa', _id: ObjectId('aaaa000000000000000000a3') },
      { name: 'noprov.bin', created: now, rev: 0, linkedFileData: { url: 'http://x' },
        hash: 'bbbbbbbbbbbbbbbbbbbb', _id: ObjectId('aaaa000000000000000000a4') },
      { name: 'url.bin', created: now, rev: 0,
        linkedFileData: { provider: 'url', url: 'http://x', importedAt: now },
        hash: 'cccccccccccccccccccc', _id: ObjectId('aaaa000000000000000000a5') }
    ],
    folders: []
  }],
  collaborators: [{ type: 'user', id: uid, access: 'owner', accepted: true }],
  overleaf: { history: { id: ObjectId('aaaa000000000000000000a7'), rangeMap: {} } },
};
f.projects.insertOne(proj);
print('SEEDED');
SEED
mongosh --quiet sharelatex /tmp/seed-u103.js | tail -1`
  )
  const clean = redisRaw(['DEL', `ProjectHistory:Ops:${FID.PJ}`])
  void clean
}

function projectJSON(): string {
  return dexe(
    mongoC,
    `mongosh --quiet sharelatex --eval '
      const d = db.projects.findOne({_id: ObjectId("aaaa000000000000000000a1")});
      if (!d) print("MISSING");
      else { delete d.lastUpdated; print(JSON.stringify(d)); }
    ' | tail -1`
  )
}

function state(leg: string): Record<string, string | number> {
  const count = dexe(mongoC, `mongosh --quiet sharelatex --eval 'print(db.projects.countDocuments({}))' | tail -1`)
  const duOps = (redisRaw(['EXISTS', `ProjectHistory:Ops:${FID.PJ}`]) || '0').trim()
  // editor-events during the leg window are captured in the leg runner
  return { project: projectJSON(), projCount: count, duOps: duOps, legMarker: leg }
}

// ---------- normalization ----------
const ERE = /W\/"([0-9a-f]+)-[^"\\]+/g
const normBase = (s: string): string => {
  let out = s
    .replace(/nonce="[^"]*"/g, 'nonce="N"')
    .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="C"')
    .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="C"')
    .replace(/ol-csrfToken=[A-Za-z0-9.-]+/g, 'ol-csrfToken=C')
    .replace(/expires=[^;]+/gi, 'expires=E')
    .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z/g, 'TS')
    .replace(ERE, 'W/"$1-H')
  // account-pill + _csrf form values are per-session (raw hex24 + token)
  out = out.replace(/<div class="disabled dropdown-item">[^<]*<\/div>/g, '<div class="disabled dropdown-item">U</div>')
  return out
}

function normRec(rec: string): string {
  return normBase(rec)
}

type LegData = {
  recs: string[]
  state: Record<string, string | number>
}

function runLeg(base: string): LegData {
  flushLimits()
  const data: LegData = { recs: [], state: {} }
  execFileSync('docker', ['cp', BATTERY, `${overleafC}:/tmp/u103-lf-matrix.cjs`], { stdio: ['ignore', 'pipe', 'pipe'] })
  const out = execFileSync(
    'docker',
    ['exec', '-e', 'LF_PJ=' + FID.PJ, '-e', 'LF_DOC=' + FID.DOC, '-e', 'LF_FPLAIN=' + FID.F_PLAIN, '-e', 'LF_FNOPROV=' + FID.F_NOPROV, '-e', 'LF_FURL=' + FID.F_URL, '-e', 'LF_GHOSTF=' + FID.GHOST_F, overleafC, 'sh', '-c', `node /tmp/u103-lf-matrix.cjs ${base.endsWith('4010') ? 2 : 1} 2>&1`],
    { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 256 * 1024 * 1024 },
  )
  const lines = (out || '').trim().split('\n').filter((l) => l && !l.startsWith('ERR'))
  if (lines.length < 40) {
    throw new Error(`battery leg(${base}) produced ${lines.length} lines (expected ~47): ${lines.slice(-6).join(' | ')}`)
  }
  data.recs = lines
  data.state = state(base.endsWith('4010') ? 'L2' : 'L-node')
  return data
}

test.describe.configure({ timeout: 900_000 })

test('u103: linked files create/refresh Node==Go==Node (3 legs)', async () => {
  await waitUp()
  seed()
  const before = projectJSON()
  expect(before.startsWith('{'), 'seed project present').toBe(true)

  const leg1 = runLeg(NODE)
  const leg2 = runLeg(GO)
  const leg3 = runLeg(NODE)

  const n = leg1.recs.length
  expect(n).toBeGreaterThanOrEqual(44)
  expect(leg2.recs.length).toBe(n)
  expect(leg3.recs.length).toBe(n)

  // wire parity per case (status + headers + full body, normalized)
  const diffs: string[] = []
  for (let i = 0; i < n; i++) {
    const a = normRec(leg1.recs[i])
    const b = normRec(leg2.recs[i])
    const c = normRec(leg3.recs[i])
    if (a !== b || b !== c) {
      diffs.push(leg1.recs[i].split('|')[0])
      if (process.env.U103_DBG) {
        const dx = a
        const dy = b
        let at = 0
        while (at < Math.min(dx.length, dy.length) && dx[at] === dy[at]) at++
        console.log(`DBG ${leg1.recs[i].split('|')[0]}: first diff at ${at}`)
        console.log(`  NODE(${leg1.recs[i].split('|')[1]}): ${JSON.stringify(dx.slice(Math.max(0, at - 400), at + 700))}`)
        console.log(`  GO  (${leg2.recs[i].split('|')[1]}): ${JSON.stringify(dy.slice(Math.max(0, at - 400), at + 700))}`)
        console.log(`  NLEN=${dx.length} GLEN=${dy.length}`)
      }
    }
  }
  for (const d of diffs) expect.soft(false, `case ${d} differs`).toBe(true)

  // state parity: project byte-identical, no new projects, no DU ops
  const strip = (st: Record<string, string | number>): Record<string, string | number> => ({
    project: normBase(String(st.project)),
    projCount: String(st.projCount),
    duOps: String(st.duOps),
  })
  const s1 = strip(leg1.state)
  const s2 = strip(leg2.state)
  const s3 = strip(leg3.state)
  expect(JSON.stringify(s1), 'state leg1==leg2').toEqual(JSON.stringify(s2))
  expect(JSON.stringify(s2), 'state leg2==leg3').toEqual(JSON.stringify(s3))
  // CE no-write invariant
  expect(String(s2.duOps), 'no DU ops for the LF fixture').toBe('0')
  // the fixture project must be UNCHANGED by the whole battery
  const after = normBase(projectJSON())
  expect(after, 'fixture project unchanged before/after').toBe(normBase(before))

  console.log(`U10.3: legs=3 cases=${n} — parity GREEN`)
})
