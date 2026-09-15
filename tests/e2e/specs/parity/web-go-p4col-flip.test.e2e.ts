/**
 * WEB-GO P4.10a FLIP GATE (WEB_GO_PLAN.md P4.10a — collaborator writes):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p4col.conf):
 *     POST   /project/:id/leave                              (requireLogin)
 *     PUT    /project/:id/users/:user_id   (admin; {privilegeLevel})
 *     DELETE /project/:id/users/:user_id   (admin)
 *     POST   /project/:id/request-access   (read; {privilegeLevel})
 *     DELETE /project/:id/access-requests/:user_id (admin; {notify?})
 *     POST   /project/:id/access-requests/:user_id/grant (admin; {privilegeLevel,notify?})
 *     POST   /project/:id/transfer-ownership (admin; {user_id,skipEmails?})
 *
 *   leg 1  Node baseline      leg 2  FLIP ON — Go      leg 3  FLIP OFF — Node
 *
 *   State pins: ref arrays (collaberator_refs / reviewer_refs / readOnly_refs),
 *   editAccessRequests (rebuild-in-place), track_changes (boolean→map
 *   conversion on REVIEW for ownerless projects), contacts (transfer
 *   touchContact), mail subjects per recipient (5 templates), 204-only
 *   responses, and Node's exact error shapes (zod 400s/404s, 403 restricted,
 *   404 "project or collaborator not found", CSRF 403 Forbidden anon).
 *
 *   Run: npx playwright test -g "web-go P4.10a collab-writes flip gate"
 */
import { execFileSync } from 'child_process'
import { test, expect } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'
import crypto from 'crypto'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..', '..', '..')
const BASE = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'
const SINK = process.env.E2E_MAIL_SINK || 'http://127.0.0.1:18025'
const OWNER = { email: 'e2e-user@e2e.test', password: 'Ol-Fixture-3m2Q' }
const OTHER = { email: 'e2e-tpladmin@e2e.test', password: 'Ol-Fixture-7tW4' }
const THIRD = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const PREFIX = 'webgo-p4col-'
const RAND = crypto.randomBytes(12).toString('hex')

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
function dexe(c: string, cmd: string, allowFail = false): string {
  try { return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', maxBuffer: 64*1024*1024, timeout: 150_000 }) }
  catch (e) { if (allowFail) return ''; throw e }
}
function runningContainer(m: string): string {
  const out = execFileSync('docker', ['ps', '--filter', `name=${m}`, '--format', '{{.Names}}'], { encoding: 'utf8' })
  const n = out.split('\n').filter(Boolean)
  if (!n.length) throw new Error(`no container ${m}`)
  return n[0]
}

function FLIP(conf: string, cmd: 'apply' | 'strip'): string {
  if (cmd === 'apply') return `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
mkdir -p /etc/nginx/overleaf-flips
cp -f /usr/local/share/overleaf-flips/${conf} /etc/nginx/overleaf-flips/${conf}
if ! grep -q "overleaf-flips/${conf}" "$vhost"; then
  node -e '
    const fs=require("fs");const v=process.argv[1];
    const inc="  include /etc/nginx/overleaf-flips/${conf};\\n\\n";
    let s=fs.readFileSync(v,"utf8");const l=s.split("\\n");
    const i=l.findIndex(x=>x.trim()==="location / {");
    if(i<0)throw new Error("location / not found");
    l.splice(i,0,inc);fs.writeFileSync(v,l.join("\\n"));' "$vhost"
fi
nginx -t && nginx -s reload && sleep 2
`
  return `
set -e
vhost=/etc/nginx/sites-enabled/overleaf.conf
if grep -q "overleaf-flips\\\/${conf}" "$vhost"; then
  sed -i "/overleaf-flips\\\/${conf}/d" "$vhost"
  nginx -t && nginx -s reload && sleep 2
fi
`
}

interface R { status: number; ct: string; body: string; setcookie?: string }
const normBody = (s: string) => s
  .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
  .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
  .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
  .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
  .replace(/[0-9a-f]{24}/g, '<HEX>')
  .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
  .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
  .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
  .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
  .replace(/\b[0-9a-f]{24}\b/g, '<HEX>')
  .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{1,9}Z/g, '<TS>')

async function nginxSettled(timeoutMs = 15_000): Promise<void> {
  const t0 = Date.now()
  for (;;) {
    try { const r = await fetch(BASE + '/status', { redirect: 'manual' }); if (r.status >= 100) { await r.text().catch(() => {}); return } } catch {}
    if (Date.now() - t0 > timeoutMs) throw new Error('nginx settle')
    await sleep(300)
  }
}

async function call(p: string, init: RequestInit & { cookie?: string } = {}): Promise<R> {
  const h = { ...(init.headers || {}) as Record<string, string>, accept: ((init.headers as any)?.accept as string) || 'application/json' }
  if (init.cookie) h['cookie'] = init.cookie as string
  const r = await fetch(BASE + p, { ...init, headers: h, redirect: 'manual' })
  const setcookie = (r.headers.getSetCookie ? r.headers.getSetCookie().join('\n') : ((r.headers.get('set-cookie') as string) || '')) as string
  const body = Buffer.from(await r.arrayBuffer()).toString('binary')
  return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], body, setcookie }
}

async function login(user: { email: string; password: string }): Promise<{ ck: string; csrf: string }> {
  const page = await call('/login')
  const csrf = (page.body.match(/name=["']ol-csrfToken["']\s+content=["']([^"']+)"/) || [])[1] || ''
  let ck = ''
  {
    const m = (page.setcookie || '').match(/overleaf\.sid=[^;\n]+/)
    if (m) ck = m[0]
  }
  const logged = await call('/login', { method: 'POST', headers: { 'content-type': 'application/json', 'x-csrf-token': csrf, accept: 'application/json' }, cookie: ck, body: JSON.stringify(user) })
  expect(logged.status, `login status ${user.email}`).toBe(200)
  const sid = ((logged.setcookie || '').split('\n').find((l) => l.includes('overleaf.sid')) || '').replace(/^overleaf\.sid=/, '').split(';')[0]
  if (sid) ck = `overleaf.sid=${sid}`
  const cs = await call('/dev/csrf', { cookie: ck, headers: { accept: 'text/plain' } })
  return { ck, csrf: cs.body.trim() }
}

function clearRateLimits(): void {
  try {
    const out = execFileSync('docker', ['ps', '--filter', 'name=ol-e2e-redis', '--format', '{{.Names}}'], { encoding: 'utf8' })
    const rc = out.split('\n').find(Boolean)
    if (!rc) return
    const keys = execFileSync('docker', ['exec', rc, 'sh', '-c', 'redis-cli --scan --pattern "rate-limit:*"'], { encoding: 'utf8' }).split('\n').filter(Boolean)
    for (const k of keys) execFileSync('docker', ['exec', rc, 'redis-cli', 'DEL', k], { encoding: 'utf8' })
  } catch {}
}

async function sinkClear(): Promise<void> {
  await fetch(SINK + '/api/messages', { method: 'DELETE' }).catch(() => {})
  await fetch(SINK + '/api/messages').catch(() => {})
}
async function sinkList(): Promise<string> {
  for (let i = 0; i < 4; i++) {
    const r = await fetch(SINK + '/api/messages').catch(() => null)
    if (!r) { await sleep(500); continue }
    const j: any = await r.json().catch(() => null)
    if (!j) { await sleep(500); continue }
    // unfold the raw MIME Subject header (.subject first line loses the
    // continuation; Message-ID/boundary/Date random => excluded from compare)
    const rows = (j.messages || []).map((m: any) => {
      const to = Array.isArray(m.to) ? m.to.map((x: any) => String(x)).sort().join(',') : String(m.to || '')
      let subject = ''
      const raw: string = m.raw || ''
      const lines = raw.split('\n')
      for (let k = 0; k < lines.length; k++) {
        const ln = lines[k].replace(/\r$/, '')
        if (/^Subject:/.test(ln)) {
          subject = ln.slice('Subject:'.length)
          while (k + 1 < lines.length && /^[ \t]/.test(lines[k + 1])) {
            k++
            subject += ' ' + lines[k].replace(/\r$/, '').trim()
          }
          break
        }
      }
      if (!subject) subject = String(m.subject || '')
      return to + '|' + subject.replace(/[\r\n]+/g, ' ').trim()
    }).sort()
    return rows.join('\n')
  }
  return 'NOSINK'
}

// seed/teardown of the determinate projects (Node P4.4 precedent: direct
// document seeding is legitimate fixture state for the write routes).
function seed(mongoC: string): Record<string, string> {
  const out = dexe(mongoC, `mongosh --quiet sharelatex --eval '
    const U = db.users.findOne({email:"e2e-user@e2e.test"})._id;
    const O = db.users.findOne({email:"e2e-tpladmin@e2e.test"})._id;
    const T = db.users.findOne({email:"e2e-admin@e2e.test"})._id;
    db.projects.deleteMany({name:{$regex:"^webgo-p4col-"}});
    db.deletedProjects.deleteMany({"project.name":{$regex:"^webgo-p4col-"}});
    db.contacts.deleteMany({user_id:{$in:[U,O,T]}});
    const a = db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4col-a",owner_ref:U,readOnly_refs:[O],publicAccesLevel:"private"}).insertedId;
    const b = db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4col-b",owner_ref:U,collaberator_refs:[O],publicAccesLevel:"private"}).insertedId;
    const c = db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4col-c",owner_ref:U,reviewer_refs:[O],track_changes:{[String(U)]:true,[String(O)]:false},publicAccesLevel:"private"}).insertedId;
    const t = db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4col-t",owner_ref:U,collaberator_refs:[O],publicAccesLevel:"private"}).insertedId;
    const d = db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4col-d",owner_ref:O,publicAccesLevel:"private"}).insertedId;
    const e = db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4col-e",owner_ref:U,publicAccesLevel:"private"}).insertedId;
    const su = db.users.findOne({email:"e2e-user@e2e.test"});
    db.users.deleteOne({email:"webgo-p4col-x@e2e.test"});
    const suClone = Object.assign({}, su, {_id:new ObjectId(),name:"Fourth Nonmember",first_name:"Fourth",last_name:"Nonmember",email:"webgo-p4col-x@e2e.test",emails:[{email:"webgo-p4col-x@e2e.test",verifiedAt:new Date(0)}]});
    delete suClone.samlIdentifiers; delete suClone.thirdPartyIdentifiers;
    const x = db.users.insertOne(suClone).insertedId;
    print(JSON.stringify({a:String(a),b:String(b),c:String(c),t:String(t),d:String(d),e:String(e),u:String(U),o:String(O),m:String(T),x:String(x)}));
  '`, true).trim().split('\n').filter((l) => l.trim().startsWith('{')).pop() || '{}'
  return JSON.parse(out)
}

// state capture: the six project docs (normalized) + contacts.
function exState(ids: Record<string, string>, mongoC: string): string {
  const dump = (id: string) => dexe(mongoC, `mongosh --quiet sharelatex --eval '
    const p = db.projects.findOne({_id:ObjectId("${id}")});
    if (!p) { print("NONE"); } else {
      const normMap = (v) => {
        if (v === undefined) return "undef";
        if (v === null) return "null";
        if (typeof v !== "object") return String(v);
        if (Array.isArray(v)) return "[" + v.map((x) => /^[0-9a-f]{24}$/.test(String(x)) ? "<HEX>" : JSON.stringify(x)).join(",") + "]";
        const ks = Object.keys(v).sort();
        return "{" + ks.map((k) => (/^[0-9a-f]{24}$/.test(k) ? "<HEX>" : k) + ":" + normMap(v[k])).join(",") + "}";
      };
      const req = (p.editAccessRequests || []).map((r) => ({ u: /^[0-9a-f]{24}$/.test(String(r.userId)) ? "<HEX>" : String(r.userId), lvl: r.privilegeLevel, at: r.requestedAt ? "<TS>" : "undef" }));
      const keys = Object.keys(p).filter((k) => k !== "_id").sort();
      print(JSON.stringify({
        keys,
        owner: "<HEX>",
        collab: normMap(p.collaberator_refs),
        reviewer: normMap(p.reviewer_refs),
        readOnly: normMap(p.readOnly_refs),
        pendEd: normMap(p.pendingEditor_refs),
        pendRev: normMap(p.pendingReviewer_refs),
        tacRO: normMap(p.tokenAccessReadOnly_refs),
        tacRW: normMap(p.tokenAccessReadAndWrite_refs),
        req,
        tc: p.track_changes === undefined ? "undef" : (typeof p.track_changes === "object" ? normMap(p.track_changes) : String(p.track_changes)),
        last: p.lastUpdated ? "<TS>" : "undef",
        __v: p.__v === undefined ? "undef" : p.__v,
      }));
    }'`, true).trim().split('\n').filter((l) => l.trim().startsWith('{') || l === 'NONE').pop() || 'NO_DUMP'
  const names = ['a', 'b', 'c', 't', 'd', 'e']
  const out: Record<string, any> = {}
  for (const n of names) out[n] = JSON.parse(dump(ids[n]))
  const contacts = dexe(mongoC, `mongosh --quiet sharelatex --eval '
    const rows = db.contacts.find({}).toArray().sort((x, y) => String(x._id).localeCompare(String(y._id))).map((c) => {
      const cs = c.contacts || {};
      const ks = Object.keys(cs).sort();
      return { id: "<HEX>", contacts: ks.map((k) => "<HEX>:" + cs[k].n + ":<TS>").join(";") };
    });
    print(JSON.stringify(rows));
  '`, true).trim().split('\n').filter((l) => l.trim().startsWith('[')).pop() || '[]'
  out.contacts = JSON.parse(contacts)
  return JSON.stringify(out)
}

interface Leg {
  cases: Record<string, R>
  state: string
  mail: string
}

async function battery(mongoC: string): Promise<Leg> {
  const ids = seed(mongoC)
  clearRateLimits()
  await sinkClear()
  const U = await login(OWNER)
  const O = await login(OTHER)
  const X = await login({ email: 'webgo-p4col-x@e2e.test', password: 'Ol-Fixture-3m2Q' })
  const M = await login({ email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' })
  const H = (u: { ck: string; csrf: string }) => ({ headers: { 'content-type': 'application/json', 'x-csrf-token': u.csrf, accept: 'application/json' }, cookie: u.ck })
  const C = (u: { ck: string; csrf: string }, p: string, method: string, body?: any) => call(p, { method, ...H(u), body: body === undefined ? undefined : JSON.stringify(body) })
  const cases: Record<string, R> = {}
  let r: R

  // ---- members baseline (read, P4.3 route — unchanged surface) ----
  r = await C(O, `/project/${ids.a}/members`, 'GET')
  cases.membersA = r

  // ---- real-project fixture for the transfer section ----
  // Node's transfer flushes to TPDS using overleaf.history.id, which only
  // real (created-via-API) projects have; a bare mongo insert 500s.
  r = await C(U, '/project/new', 'POST', { projectName: 'webgo-p4col-t-real' })
  cases.createT = r
  {
    const tReal = dexe(mongoC, `mongosh --quiet sharelatex --eval '
      const t = db.projects.findOne({name:"webgo-p4col-t-real"});
      const O = db.users.findOne({email:"e2e-tpladmin@e2e.test"})._id;
      db.projects.updateOne({_id:t._id},{$push:{collaberator_refs:O}});
      db.projects.deleteOne({name:"webgo-p4col-t"});
      print(String(t._id));'`, true).trim().split('\n').pop().trim()
    ids.t = tReal
  }

  // ---- setCollaboratorInfo (PUT users) ----
  r = await C(U, `/project/${ids.a}/users/${ids.o}`, 'PUT', { privilegeLevel: 'review' }); cases.view2review = r
  r = await C(U, `/project/${ids.a}/users/${ids.o}`, 'PUT', { privilegeLevel: 'readAndWrite' }); cases.review2editor = r
  r = await C(U, `/project/${ids.a}/users/${ids.o}`, 'PUT', { privilegeLevel: 'readOnly' }); cases.editor2viewer = r
  r = await C(U, `/project/${ids.c}/users/${ids.o}`, 'PUT', { privilegeLevel: 'review' }); cases.setCReview = r
  r = await C(O, `/project/${ids.c}/request-access`, 'POST', { privilegeLevel: 'review' }); cases.reqSameLevel = r
  r = await C(U, `/project/${ids.c}/users/${ids.o}`, 'PUT', { privilegeLevel: 'readOnly' }); cases.setCDemote = r
  r = await C(O, `/project/${ids.a}/users/${ids.u}`, 'PUT', { privilegeLevel: 'readOnly' }); cases.setNonadmin = r
  r = await C(U, `/project/${ids.a}/users/${RAND}`, 'PUT', { privilegeLevel: 'readOnly' }); cases.setNonmember = r
  r = await C(U, `/project/${ids.a}/users/${ids.u}`, 'PUT', { privilegeLevel: 'readOnly' }); cases.setOwnerTarget = r
  r = await C(U, `/project/${ids.a}/users/${ids.o}`, 'PUT', { privilegeLevel: 'owner' }); cases.setBadLevel = r
  r = await C(U, `/project/${ids.a}/users/${ids.o}`, 'PUT', { privilegeLevel: 'readOnly', zz: 1 }); cases.setUnknown = r
  r = await C(U, `/project/${ids.a}/users/${ids.o}`, 'PUT', {}); cases.setNoLevel = r
  r = await C(U, `/project/${ids.a}/users/not-an-oid`, 'PUT', { privilegeLevel: 'readOnly' }); cases.setBadUser = r
  r = await call(`/project/${ids.a}/users/${ids.o}`, { method: 'PUT', headers: { accept: 'application/json', 'x-csrf-token': 'faketoken' }, body: '{"privilegeLevel":"readOnly"}' }); cases.setAnon = r

  // ---- requestAccess ----
  r = await C(O, `/project/${ids.a}/request-access`, 'POST', { privilegeLevel: 'readAndWrite' }); cases.reqRW = r
  r = await C(O, `/project/${ids.a}/request-access`, 'POST', { privilegeLevel: 'review' }); cases.reqAgain = r
  r = await C(O, `/project/${ids.a}/request-access`, 'POST', { privilegeLevel: 'readOnly' }); cases.reqBadLevel = r
  r = await C(U, `/project/${ids.a}/request-access`, 'POST', { privilegeLevel: 'readAndWrite' }); cases.reqOwner = r
  r = await C(X, `/project/${ids.a}/request-access`, 'POST', { privilegeLevel: 'readAndWrite' }); cases.reqNonmember = r
  r = await C(O, `/project/${ids.a}/request-access`, 'POST', { privilegeLevel: 'readAndWrite', zz: 1 }); cases.reqUnknown = r
  r = await C(O, `/project/not-an-oid/request-access`, 'POST', { privilegeLevel: 'review' }); cases.reqBadProject = r

  // ---- decline ----
  r = await C(U, `/project/${ids.a}/access-requests/${ids.o}`, 'DELETE'); cases.decline = r
  r = await C(O, `/project/${ids.a}/request-access`, 'POST', { privilegeLevel: 'readAndWrite' }); cases.req2Before = r
  r = await C(U, `/project/${ids.a}/access-requests/${ids.o}`, 'DELETE', { notify: true }); cases.declineNotify = r
  r = await C(U, `/project/${ids.a}/access-requests/${ids.o}`, 'DELETE'); cases.declineGone = r
  r = await C(U, `/project/${ids.a}/access-requests/not-an-oid`, 'DELETE'); cases.declineBadId = r
  r = await C(O, `/project/${ids.a}/access-requests/${ids.o}`, 'DELETE'); cases.declineNonadmin = r
  r = await C(U, `/project/${ids.a}/access-requests/${ids.o}`, 'DELETE', { notify: 'yes' }); cases.declineBadNotify = r

  // ---- grant ----
  r = await C(O, `/project/${ids.a}/request-access`, 'POST', { privilegeLevel: 'readAndWrite' }); cases.req3Before = r
  r = await C(U, `/project/${ids.a}/access-requests/${ids.o}/grant`, 'POST', { privilegeLevel: 'readAndWrite' }); cases.grant = r
  r = await C(U, `/project/${ids.a}/users/${ids.o}`, 'PUT', { privilegeLevel: 'readOnly' }); cases.grantBackViewer = r
  r = await C(O, `/project/${ids.a}/request-access`, 'POST', { privilegeLevel: 'review' }); cases.req4Before = r
  r = await C(U, `/project/${ids.a}/access-requests/${ids.o}/grant`, 'POST', { privilegeLevel: 'review', notify: true }); cases.grantNotify = r
  r = await C(U, `/project/${ids.a}/access-requests/${RAND}/grant`, 'POST', { privilegeLevel: 'readAndWrite' }); cases.grantBadUser = r
  r = await C(U, `/project/${ids.a}/access-requests/${ids.o}/grant`, 'POST', { privilegeLevel: 'readOnly' }); cases.grantBadLevel = r
  r = await C(U, `/project/${ids.a}/access-requests/${ids.o}/grant`, 'POST', {}); cases.grantNoLevel = r
  r = await C(O, `/project/${ids.a}/access-requests/${ids.o}/grant`, 'POST', { privilegeLevel: 'review' }); cases.grantNonadmin = r
  r = await C(U, `/project/not-an-oid/access-requests/${ids.o}/grant`, 'POST', { privilegeLevel: 'review' }); cases.grantBadProject = r

  // ---- removeUser / leave ----
  r = await C(M, `/project/${ids.b}/users/${ids.o}`, 'DELETE'); cases.removeByAdmin = r
  r = await C(U, `/project/${ids.b}/users/${ids.o}`, 'DELETE'); cases.removeB = r
  r = await C(U, `/project/${ids.b}/users/${RAND}`, 'DELETE'); cases.removeNonmember = r
  r = await C(U, `/project/${RAND}/users/${ids.o}`, 'DELETE'); cases.removeGhost = r
  r = await C(U, `/project/${ids.b}/users/not-an-oid`, 'DELETE'); cases.removeBadId = r
  r = await C(O, `/project/${ids.b}/users/${ids.o}`, 'DELETE'); cases.removeNonadmin = r
  r = await C(O, `/project/${ids.d}/leave`, 'POST'); cases.leaveD = r
  r = await C(U, `/project/${ids.d}/leave`, 'POST'); cases.leaveNotMember = r
  r = await call(`/project/${ids.d}/leave`, { method: 'POST', headers: { accept: 'application/json', 'x-csrf-token': 'faketoken' } }); cases.leaveAnon = r
  r = await C(U, `/project/not-an-oid/leave`, 'POST'); cases.leaveBadId = r

  // ---- ghost project (valid hex, no doc) — 404 coverage ----
  r = await C(O, `/project/${RAND}/request-access`, 'POST', { privilegeLevel: 'readAndWrite' }); cases.reqGhost = r
  r = await C(U, `/project/${RAND}/access-requests/${ids.o}/grant`, 'POST', { privilegeLevel: 'readAndWrite' }); cases.grantGhost = r
  r = await C(U, `/project/${RAND}/users/${ids.o}`, 'PUT', { privilegeLevel: 'readOnly' }); cases.setGhost = r
  r = await C(U, `/project/${RAND}/leave`, 'POST'); cases.leaveGhost = r
  r = await C(U, `/project/${RAND}/transfer-ownership`, 'POST', { user_id: ids.o }); cases.transferGhost = r

  // ---- transfer ownership (T was re-created as a REAL project above) ----
  r = await C(U, `/project/${ids.t}/transfer-ownership`, 'POST', { user_id: ids.o }); cases.transfer = r
  r = await C(O, `/project/${ids.t}/transfer-ownership`, 'POST', { user_id: ids.u, skipEmails: true }); cases.transferBackNoMail = r
  r = await C(U, `/project/${ids.e}/transfer-ownership`, 'POST', { user_id: ids.o }); cases.transferNoncollab = r
  r = await C(U, `/project/${ids.e}/transfer-ownership`, 'POST', { user_id: ids.u }); cases.transferSelf = r
  r = await C(U, `/project/${ids.e}/transfer-ownership`, 'POST', { user_id: 'not-an-oid' }); cases.transferBadId = r
  r = await C(U, `/project/${ids.e}/transfer-ownership`, 'POST', { user_id: RAND }); cases.transferUnknown = r
  r = await C(U, `/project/${ids.e}/transfer-ownership`, 'POST', { user_id: ids.o, zz: 1 }); cases.transferUnknownKey = r
  r = await call(`/project/${ids.e}/transfer-ownership`, { method: 'POST', headers: { accept: 'application/json', 'x-csrf-token': 'faketoken' }, body: `{"user_id":"${ids.o}"}` }); cases.transferAnon = r

  const state = exState(ids, mongoC)
  const mail = await sinkList()
  // teardown (safe: only the seeded fixture user's contacts/projects)
  dexe(mongoC, `mongosh --quiet sharelatex --eval '
    const U = db.users.findOne({email:"e2e-user@e2e.test"})._id;
    const O = db.users.findOne({email:"e2e-tpladmin@e2e.test"})._id;
    const T = db.users.findOne({email:"e2e-admin@e2e.test"})._id;
    const X = db.users.findOne({email:"webgo-p4col-x@e2e.test"})?._id;
    db.projects.deleteMany({name:{$regex:"^webgo-p4col-"}});
    db.contacts.deleteMany({user_id:{$in:[U,O,T].concat(X?[X]:[]).map(x=>x)}});
    db.users.deleteOne({email:"webgo-p4col-x@e2e.test"});'`, true)
  return { cases, state, mail }
}

function rCompare(name: string, a: R, b: R, tag: string, p: string[]): void {
  if (a.status !== b.status) p.push(`${tag}:${name} status N=${a.status} G=${b.status}`)
  if (a.ct !== b.ct) p.push(`${tag}:${name} ct N=${a.ct} G=${b.ct}`)
  const na = normBody(a.body), nb = normBody(b.body)
  if (na !== nb) {
    let i = 0
    while (i < Math.min(na.length, nb.length) && na[i] === nb[i]) i++
    p.push(`${tag}:${name} body N[...${na.slice(Math.max(0, i - 60), i + 60)}...] G[...${nb.slice(Math.max(0, i - 60), i + 60)}...]`)
  }
}

function diffLegs(a: Leg, b: Leg, tag: string): string[] {
  const p: string[] = []
  const an = Object.keys(a.cases).sort(), bn = Object.keys(b.cases).sort()
  if (an.join(',') !== bn.join(',')) p.push(`${tag}:cases N=${an.join(',')} G=${bn.join(',')}`)
  for (const k of an) {
    const bb = b.cases[k]
    if (!bb) continue
    if (!(k === 'mailTransfer' || k === 'mailList')) rCompare(k, a.cases[k], bb, tag, p)
  }
  const as = JSON.parse(a.state), bs = JSON.parse(b.state)
  const aJson = JSON.stringify(as), bJson = JSON.stringify(bs)
  if (aJson !== bJson) {
    for (const k of (Object.keys(as).concat(Object.keys(bs)) as any[])) {
      if (JSON.stringify(as[k]) !== JSON.stringify(bs[k])) {
        p.push(`${tag}:state.${k} N=${JSON.stringify(as[k]).slice(0, 220)} G=${JSON.stringify(bs[k]).slice(0, 220)}`)
      }
    }
  }
  if (a.mail !== b.mail) {
    p.push(`${tag}:mail N="${a.mail.replace(/\n/g, ' | ').slice(0, 400)}" G="${b.mail.replace(/\n/g, ' | ').slice(0, 400)}"`)
  }
  return p
}

test.describe.serial('web-go P4.10a collab-writes flip gate', () => {
  let overleafC = '', mongoC = '', leg1: Leg | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf'); mongoC = runningContainer('ol-e2e-mongo')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'bin/web'), `${overleafC}:/usr/local/bin/go-services/web`])
    dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p4col.conf'), `${overleafC}:/tmp/web-p4col.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p4col.conf /usr/local/share/overleaf-flips/web-p4col.conf')
    dexe(overleafC, 'sv restart web-go-overleaf', true)
    for (;;) { const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim(); if (code === '200') break; await sleep(500) }
    dexe(overleafC, FLIP('web-p4col.conf', 'strip'), true); await nginxSettled()
  }, 240_000)

  test.afterAll(async () => { try { dexe(overleafC, FLIP('web-p4col.conf', 'strip'), true); await nginxSettled() } catch {} })

  test('leg 1: Node baseline', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('web-p4col.conf', 'strip'), true); await nginxSettled()
    leg1 = await battery(mongoC)

    try {
      const fs = await import('node:fs')
      fs.default.writeFileSync('/tmp/p4col-leg1.json', JSON.stringify({ cases: leg1.cases, mail: leg1.mail, state: leg1.state }, null, 1))
    } catch {}
    const c = (k: string) => leg1.cases[k]
    expect(leg1.cases.membersA.status).toBe(200)
    expect(c('view2review').status, 'view2review').toBe(204)
    expect(c('review2editor').status).toBe(204)
    expect(c('editor2viewer').status).toBe(204)
    expect(c('setCReview').status).toBe(204)
    expect(c('setNonadmin').status, 'setNonadmin').toBe(403)
    expect(c('setNonmember').status, 'setNonmember').toBe(404)
    expect(c('setBadLevel').status, 'setBadLevel').toBe(400)
    expect(c('setUnknown').status).toBe(400)
    expect(c('setNoLevel').status).toBe(400)
    expect(c('setBadUser').status).toBe(404)
    expect(c('setAnon').status, 'setAnon').toBe(403)
    expect(c('reqRW').status).toBe(204)
    expect(c('reqAgain').status).toBe(204)
    expect(c('reqBadLevel').status, 'reqBadLevel (zod enum 400)').toBe(400)
    expect(c('reqSameLevel').status, 'reqSameLevel 403').toBe(403)
    expect(c('reqOwner').status, 'reqOwner').toBe(403)
    expect(c('reqNonmember').status).toBe(403)
    expect(c('reqUnknown').status).toBe(400)
    expect(c('reqBadProject').status).toBe(404)
    expect(c('decline').status).toBe(204)
    expect(c('declineNotify').status).toBe(204)
    expect(c('declineGone').status).toBe(204)
    expect(c('declineBadId').status).toBe(404)
    expect(c('declineNonadmin').status).toBe(403)
    expect(c('declineBadNotify').status).toBe(400)
    expect(c('grant').status).toBe(204)
    expect(c('grantBackViewer').status).toBe(204)
    expect(c('grantNotify').status).toBe(204)
    expect(c('grantBadUser').status).toBe(404)
    expect(c('grantBadUser').ct).toBe('text/html')
    expect(c('grantBadUser').body).toContain('Page Not Found')
    expect(c('grantBadLevel').status).toBe(400)
    expect(c('grantNoLevel').status).toBe(400)
    expect(c('grantNonadmin').status).toBe(403)
    expect(c('grantBadProject').status).toBe(404)
    expect(c('removeB').status).toBe(204)
    expect(c('removeNonmember').status, 'removeNonmember 204 (no match check)').toBe(204)
    expect(c('removeBadId').status).toBe(404)
    expect(c('removeNonadmin').status).toBe(403)
    expect(c('leaveD').status).toBe(204)
    expect(c('leaveNotMember').status, 'leaveNotMember 204 no-op').toBe(204)
    expect(c('leaveAnon').status).toBe(403)
    expect(c('transfer').status).toBe(204)
    expect(c('transferBackNoMail').status).toBe(204)
    expect(c('transferNoncollab').status, 'transferNoncollab').toBe(403)
    expect(c('transferSelf').status, 'transferSelf no-op').toBe(204)
    expect(c('transferBadId').status, 'transferBadId (body.key => 400)').toBe(400)
    expect(c('transferBadId').body).toContain('Invalid Mongo ObjectId at \\"body.user_id\\"')
    expect(c('transferUnknown').status).toBe(404)
    expect(c('transferAnon').status).toBe(403)

    // mail list (transfer: 1 confirmation pair — the skipEmails follow-up
    // sends none)
    expect(leg1.mail, 'mail list').toContain('requested editor access to webgo-p4col-a - OlliTeX')
    expect(leg1.mail).toContain('was declined - ')
    expect(leg1.mail).toContain('was granted - ')
    expect(leg1.mail.split('\n').filter((l) => l.includes('ownership transfer')).length, '2 ownership mails').toBe(2)
    expect(leg1.mail.split('\n').filter((l) => l.includes('was granted')).length, '1 granted mail').toBe(1)
    expect(leg1.mail.split('\n').filter((l) => l.includes('requested editor access')).length, '3 editor request mails').toBe(3)
    expect(leg1.mail.split('\n').filter((l) => l.includes('requested reviewer access')).length, '1 reviewer request mail').toBe(1)
    expect(leg1.mail.split('\n').length, '8 mails total').toBe(8)
    expect(c('transferUnknownKey').status).toBe(400)
  }, 300_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('web-p4col.conf', 'apply')); await nginxSettled()
    const leg2 = await battery(mongoC)
    const p = diffLegs(leg1!, leg2, 'GO')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 300_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('web-p4col.conf', 'strip'), true); await nginxSettled()
    const leg3 = await battery(mongoC)
    const p = diffLegs(leg1!, leg3, 'NODE-rev')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 300_000)
})
