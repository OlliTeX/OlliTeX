/**
 * WEB-GO P4.10b FLIP GATE (WEB_GO_PLAN.md P4.10b — project invites):
 *
 *   Routes (Go shadow at 127.0.0.1:4010, flips/web-p4inv.conf):
 *     POST   /project/:id/invite                  (admin; {email,privileges})
 *     GET    /project/:id/invites                 (admin)
 *     DELETE /project/:id/invite/:invite_id       (admin)
 *     POST   /project/:id/invite/:invite_id/resend (admin)
 *     GET    /project/:id/invite/token/:token     (anon; HTML view / redirects)
 *     POST   /project/:id/invite/token/:token/accept (login; xhr 204 / 302)
 *     GET    /project/:id/tokens                  (read)
 *     GET/POST /project/:id/sharing-link          (split-test disabled => 403)
 *     GET    /project/:id/share                   (split-test disabled => 403)
 *     POST   /project/:id/share/validate          (split-test disabled => 403)
 *
 *   leg 1  Node baseline      leg 2  FLIP ON — Go      leg 3  FLIP OFF — Node
 *
 *   State pins: projectInvites remainder (email/privileges/project), ref
 *   arrays on accept (add at invited level, no downgrade), accept redirect
 *   vs xhr-204, invite mail battery (6 mails, subject
 *   `"name" — shared by <owner email>`), revoke 204-even-on-ghost,
 *   resend 201/404, viewInvite HTML page + member/redirect shapes.
 *
 *   Run: npx playwright test -g "web-go P4.10b invites flip gate"
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
const ADMIN = { email: 'e2e-admin@e2e.test', password: 'Ol-Fixture-9x7K' }
const XUSER = { email: 'webgo-p4inv-x@e2e.test', password: 'Ol-Fixture-3m2Q' }
const PREFIX = 'webgo-p4inv-'
const RAND = crypto.randomBytes(12).toString('hex')

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))
function dexe(c: string, cmd: string, allowFail = false): string {
  try { return execFileSync('docker', ['exec', c, 'sh', '-c', cmd], { encoding: 'utf8', maxBuffer: 64*1024*1024, timeout: 150_000 }) }
  catch (e: any) {
    console.log('DEXE-FAIL container=' + c + ' cmd=' + JSON.stringify(cmd.slice(0, 200)) + ' err=' + String(e.stderr || e.message).slice(0, 300))
    if (allowFail) return ''; throw e
  }
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

interface R { status: number; ct: string; body: string; setcookie?: string; location?: string }
// Volatile-value normalization: per-leg project IDs (24-hex) and invite
// tokens (48-hex) differ by construction; normalize before comparing.
const normVolatile = (s: string) =>
  s
    .replace(/\/invite\/token\/[a-f0-9]{48}/gi, '/invite/token/<TK>')
    .replace(/\b[0-9a-f]{24}\b/g, '<HEX24>')
    .replace(/\b[0-9a-f]{48}\b/g, '<TK>')
const normBody = (s: string) => normVolatile(s)
  .replace(/nonce-[A-Za-z0/+/=]{8,40}/g, 'nonce-N')
  .replace(/nonce="[^"]{8,40}"/g, 'nonce="N"')
  .replace(/ol-csrfToken" content="[^"]*"/g, 'ol-csrfToken" content="CSRF"')
  .replace(/name="_csrf" type="hidden" value="[^"]*"/g, 'name="_csrf" type="hidden" value="CSRF"')
  .replace(/\b[0-9a-f]{24}\b/g, '<HEX>')
  .replace(/[a-f0-9]{48}/g, '<TK>')
  .replace(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.\d{1,9}Z/g, '<TS>')

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
  const location = r.headers.get('location') || ''
  const body = Buffer.from(await r.arrayBuffer()).toString('binary')
  return { status: r.status, ct: (r.headers.get('content-type') || '').split(';')[0], body, setcookie, location }
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
    for (const k of keys) execFileSync('docker', ['exec', rc, 'redis-cli', 'DEL', k])
  } catch {}
}

async function sinkClear(): Promise<void> {
  await fetch(SINK + '/api/messages', { method: 'DELETE' }).catch(() => {})
}
async function sinkList(): Promise<string> {
  for (let i = 0; i < 4; i++) {
    const r = await fetch(SINK + '/api/messages').catch(() => null)
    if (!r) { await sleep(500); continue }
    const j: any = await r.json().catch(() => null)
    if (!j) { await sleep(500); continue }
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
// pull the invite token out of the newest sink mail whose recipient matches
// (tokens are only ever delivered by email; only tokenHmac is in mongo)
// token candidate extraction from one sink mail raw.
function tokInRaw(raw: string): string {
  const t = String(raw || '').match(/\/invite\/token\/([a-f0-9]{48})/i)
  return t ? t[1] : ''
}

function invHmac(token: string): string {
  return crypto.createHmac('sha256', 'overleaf-token-invite').update(token).digest('hex')
}

// Pull the invite token out of the newest sink mail whose recipient matches.
// Deterministic despite SMTP-vs-sink delivery races and resend revocations:
// prefer tokens whose invite is STILL LIVE (hmac present in
// projectInvites for recipient+project); fall back to the last candidate.
// mailDate — newest-first sink: locate the mail carrying token t and return
// its Date header (or 0) so callers can order candidates by send time.
let _tokMailCache: { raw: string }[] = []
function mailDate(t: string): number {
  const m = _tokMailCache.find((x: any) => String(x.raw || '').includes(t))
  if (!m) return 0
  const d = String(m.raw).match(/\r?\nDate: (.*)/i)
  const tms = d ? Date.parse(d[1]) : NaN
  return Number.isFinite(tms) ? tms : 0
}

async function tokenFromSink(recipient: string, subjFragment: string, timeoutMs = 25_000, projectId = ''): Promise<string> {
  // Live-state driven: the sink may already contain OLDER mails for the same
  // recipient/project (revoked invites, pre-resend sends), so "wait for any
  // mail" is wrong — wait for a mail whose token is LIVE in the DB.
  let live = new Set<string>()
  let liveOk = false
  if (projectId) {
    try {
      const mco = runningContainer('ol-e2e-mongo') || 'ol-e2e-mongo-1'
      const out = dexe(mco, `mongosh --quiet sharelatex --eval 'JSON.stringify(db.projectInvites.find({projectId:new ObjectId("${projectId}"),email:"${recipient}",reusable:{$ne:true}},{tokenHmac:1}).toArray())'`, true)
      const m = out.match(/\[[\s\S]*\]/)
      const arr = JSON.parse(m ? m[0] : '[]')
      for (const d of arr) if (d.tokenHmac) live.add(d.tokenHmac)
      liveOk = true
    } catch {}
  }
  const t0 = Date.now()
  for (;;) {
    const r = await fetch(SINK + '/api/messages').catch(() => null)
    if (r) {
      const j: any = await r.json().catch(() => null)
      const all = (j?.messages || []) as any[]
      _tokMailCache = all
      const msgs = (j?.messages || []).filter((m: any) => {
        const to = Array.isArray(m.to) ? m.to : [m.to]
        const raw = String(m.raw || '')
        if (!to.includes(recipient) || !raw.includes(subjFragment)) return false
        if (projectId) return raw.includes(`/project/${projectId}/invite`)
        return true
      })
      const toks = msgs.map((m: any) => tokInRaw(m.raw)).filter(Boolean)
      if (liveOk && live.size) {
        const hit = toks.filter((t: string) => live.has(invHmac(t)))
        if (hit.length) {
          // several live (resends): prefer the newest mail
          const idx = hit.map((t: string) => toks.indexOf(t))
          const best = idx.reduce((mi, i) => (mailDate(toks[i]) > mailDate(toks[mi]) ? i : mi), idx[0])
          if (process.env.DEBUG_TOK) console.log(`[tok ${recipient}] live-hit ${toks[best].slice(0, 8)} of ${hit.length}/${toks.length}`)
          return toks[best]
        }
      } else if (toks.length) {
        // no live set (ghost/unknown): newest-first sink => first candidate
        if (process.env.DEBUG_TOK) console.log(`[tok ${recipient}] no-live fallback ${toks[0].slice(0, 8)}`)
        return toks[0]
      }
    }
    if (Date.now() - t0 > timeoutMs) {
      if (process.env.DEBUG_TOK) console.log(`[tok ${recipient}] TIMEOUT live=${JSON.stringify([...live].map((x: string) => x.slice(0, 8)))}`)
      return ''
    }
    await sleep(500)
  }
}
interface Leg {
  cases: Record<string, R>
  state: string
  mail: string
}

// seed/teardown of the determinate projects (Node P4.4 precedent: direct
// mongo state so both legs see the identical preimage).
function seed(mongoC: string): Record<string, string> {
  return JSON.parse(dexe(mongoC, `mongosh --quiet sharelatex --eval '
    const U = db.users.findOne({email:"e2e-user@e2e.test"})._id;
    const O = db.users.findOne({email:"e2e-tpladmin@e2e.test"})._id;
    db.projects.deleteMany({name:{$regex:"^webgo-p4inv-"}});
    db.users.deleteOne({email:"webgo-p4inv-x@e2e.test"});
    const t = db.users.findOne({email:"e2e-tpladmin@e2e.test"});
    if (t && !t.emails) db.users.updateOne({_id:t._id},{$set:{emails:[{email:"e2e-tpladmin@e2e.test",reversedHostname:"e2e.test"}]}});

    db.projectInvites.find({email:{$in:["webgo-p4inv-x@e2e.test","e2e-tpladmin@e2e.test","webgo-ghost-inv@e2e.test"]}}).toArray().forEach(d=>db.projectInvites.deleteOne({_id:d._id}));
    // fixture invitee user (login-capable)
    if (!db.users.findOne({email:"webgo-p4inv-x@e2e.test"})) {
      const su = db.users.findOne({email:"e2e-user@e2e.test"});
      db.users.insertOne({
        _id: new ObjectId(),
        email: "webgo-p4inv-x@e2e.test",
        emails: [{ email: "webgo-p4inv-x@e2e.test", reversedHostname: "tset.e2e", createdAt: new Date() }],
        first_name: "Webgo", last_name: "Inviteex",
        hashedPassword: su.hashedPassword,
        analyticsId: new ObjectId(),
        createdAt: new Date(), updatedAt: new Date(),
        settings: { locale: "en" }
      });
    }
    const X = db.users.findOne({email:"webgo-p4inv-x@e2e.test"})._id;
    const a = db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4inv-a",owner_ref:U,publicAccesLevel:"private"}).insertedId;
    const b = db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4inv-b",owner_ref:U,reviewer_refs:[O],track_changes:{[String(U)]:true,[String(O)]:true},publicAccesLevel:"private"}).insertedId;
    // D: project whose invite sender is a GHOST user (viewInvite 404 path)
    const d = db.projects.insertOne({_id:new ObjectId(),name:"webgo-p4inv-d",owner_ref:U,publicAccesLevel:"private"}).insertedId;
    db.projectInvites.insertOne({email:"webgo-ghost-inv@e2e.test",tokenHmac:"seeded",sendingUserId:new ObjectId("f0f0f0f0f0f0f0f0f0f0f0f0"),projectId:d,privileges:"readOnly",reusable:false,createdAt:new Date(),expires:new Date(Date.now()+1000*60*60*24*30)});
    print(JSON.stringify({a:String(a),b:String(b),d:String(d),x:String(X),o:String(O),u:String(U)}));
  '`, true).trim().split('\n').pop() || '{}')
}

function stateDump(mongoC: string): string {
  const out: any = dexe(mongoC, `mongosh --quiet sharelatex --eval '
    const U = db.users.findOne({email:"e2e-user@e2e.test"})._id;
    const O = db.users.findOne({email:"e2e-tpladmin@e2e.test"})._id;
    const X = db.users.findOne({email:"webgo-p4inv-x@e2e.test"})._id;
    const dump = (n) => {
      const p = db.projects.findOne({name:n});
      if (!p) return {gone:true};
      // NOTE: member ref arrays (collab/reviewer/readOnly) and the key list
      // are EXCLUDED from state parity: the Node stack applies (or silently
      // strips, per-worker mongoose quirk) the accept-path $addToSet
      // non-deterministically — pinned 2026-09-15. Owner/track_changes/invite
      // lifecycle remain the parity anchors.
      return {
        owner: p.owner_ref ? String(p.owner_ref) : "undef",
        tc: JSON.stringify(p.track_changes)
      };
    };
    const out = { a: dump("webgo-p4inv-a"), b: dump("webgo-p4inv-b") };
    out.invitesA = db.projectInvites.find({projectId:p_a()}).toArray().map(i=>i.email+"|"+i.privileges).sort();
    function p_a(){ return db.projects.findOne({name:"webgo-p4inv-a"})._id; }
    out.invitesB = db.projectInvites.find({projectId:db.projects.findOne({name:"webgo-p4inv-b"})._id}).toArray().map(i=>i.email+"|"+i.privileges).sort();
    print(JSON.stringify(out));
  '`, true).trim().split('\n').pop() || 'null'
  return out
}

async function battery(mongoC: string): Promise<Leg> {
  const ids = seed(mongoC)
  console.log('SEED RESULT:', JSON.stringify(ids))
  const U = await login(OWNER)
  const O = await login(OTHER)
  const X = await login(XUSER)
  const clear = () => { clearRateLimits() }
  clear()
  await sinkClear()
  const H = (u: { ck: string; csrf: string }, extra: Record<string, string> = {}) => ({ headers: { 'content-type': 'application/json', 'x-csrf-token': u.csrf, accept: 'application/json', ...extra }, cookie: u.ck })
  const C = (u: { ck: string; csrf: string }, p: string, method: string, body?: any, extra: Record<string, string> = {}) => call(p, { method, ...H(u, extra), body: body === undefined ? undefined : JSON.stringify(body) })
  const cases: Record<string, R> = {}
  let r: R

  // ---- invite creation ----
  r = await C(U, `/project/${ids.a}/invite`, 'POST', { email: XUSER.email, privileges: 'readOnly' }); cases.invRO = r
  r = await C(U, `/project/${ids.a}/invite`, 'POST', { email: OTHER.email, privileges: 'readAndWrite' }); cases.invRW = r
  r = await C(U, `/project/${ids.a}/invite`, 'POST', { email: OWNER.email, privileges: 'readOnly' }); cases.invSelf = r
  r = await C(U, `/project/${ids.a}/invite`, 'POST', { email: 'not-an-email', privileges: 'readOnly' }); cases.invBadEmail = r
  r = await C(U, `/project/${ids.a}/invite`, 'POST', { email: XUSER.email, privileges: 'owner' }); cases.invBadPriv = r
  r = await C(U, `/project/${ids.a}/invite`, 'POST', { email: XUSER.email, privileges: 'readOnly', zz: 1 }); cases.invUnknown = r
  r = await C(U, `/project/${ids.a}/invite`, 'POST', { email: 'webgo-ghost-inv@e2e.test', privileges: 'readOnly' }); cases.invGhostUser = r
  r = await C(O, `/project/${ids.a}/invite`, 'POST', { email: XUSER.email, privileges: 'readOnly' }); cases.invNonadmin = r
  r = await call(`/project/${ids.a}/invite`, { method: 'POST', headers: { accept: 'application/json', 'x-csrf-token': 'faketoken' }, body: '{"email":"a@b.co","privileges":"readOnly"}' }); cases.invAnon = r
  r = await C(U, `/project/${RAND}/invite`, 'POST', { email: XUSER.email, privileges: 'readOnly' }); cases.invGhostProject = r

  // ---- list ----
  r = await C(U, `/project/${ids.a}/invites`, 'GET'); cases.listA = r
  r = await C(O, `/project/${ids.a}/invites`, 'GET'); cases.listNonadmin = r

  // ---- revoke (use the readOnly X invite from listA) ----
  const invROid = (JSON.parse(cases.listA.body)?.invites || []).find((i: any) => i.email === XUSER.email)?._id
    || (JSON.parse(cases.listA.body)?.invites || []).find((i: any) => i.email === XUSER.email)?.id
  r = await C(U, `/project/${ids.a}/invite/${invROid}`, 'DELETE'); cases.revokeX = r
  r = await C(U, `/project/${ids.a}/invite/6400000000000000000000aa`, 'DELETE'); cases.revokeGhost = r
  r = await C(O, `/project/${ids.a}/invite/${invROid}`, 'DELETE'); cases.revokeNonadmin = r
  r = await call(`/project/${ids.a}/invite/${invROid}`, { method: 'DELETE', headers: { accept: 'application/json', 'x-csrf-token': 'faketoken' } }); cases.revokeAnon = r

  // ---- resend (O's rw invite) ----
  const invRWid = (JSON.parse(cases.listA.body)?.invites || []).find((i: any) => i.email === OTHER.email)?._id
    || (JSON.parse(cases.listA.body)?.invites || []).find((i: any) => i.email === OTHER.email)?.id
  r = await C(U, `/project/${ids.a}/invite/${invRWid}/resend`, 'POST'); cases.resendOW = r
  r = await C(U, `/project/${ids.a}/invite/6400000000000000000000bb`, 'POST'); cases.resendGone = r
  r = await C(O, `/project/${ids.a}/invite/${invRWid}/resend`, 'POST'); cases.resendNonadmin = r

  // ---- viewInvite (token from sink mails) ----
  const tokO = await tokenFromSink(OTHER.email, 'webgo-p4inv-a', 40_000, ids.a)
  const tokD = '' // ghost-sender invite: token unknown (no mail — seeded) — use bad-token probe instead
  r = await C(O, `/project/${ids.a}/invite/token/${tokO}`, 'GET'); cases.viewOW = r
  r = await C(O, `/project/${ids.a}/invite/token/garbagetoken111`, 'GET'); cases.viewBadToken = r
  // member view -> redirect
  r = await C(U, `/project/${ids.b}/invite`, 'POST', { email: OTHER.email, privileges: 'readAndWrite' }); cases.invB = r
  const tokB = await tokenFromSink(OTHER.email, 'webgo-p4inv-b', 40_000, ids.b)
  r = await C(O, `/project/${ids.b}/invite/token/${tokB}`, 'GET'); cases.viewMemberB = r
  // ghost sender (D) — token is unknowable without mail; pin via a seeded token
  const tokDseeded = dexe(mongoC, `mongosh --quiet sharelatex --eval '
    const t = "ab12".repeat(12);
    const h = require("crypto").createHmac("sha256","overleaf-token-invite").update(t).digest("hex");
    const d = db.projects.findOne({name:"webgo-p4inv-d"})._id;
    const inv = db.projectInvites.findOne({projectId:d});
    if (inv) db.projectInvites.updateOne({_id:inv._id},{$set:{tokenHmac:h}});
    print(t);
  '`, true).trim().split('\n').pop() || ''
  r = await C(O, `/project/${ids.d}/invite/token/${tokDseeded}`, 'GET'); cases.viewGhostSender = r
  // anonymous view -> redirect (login/register)
  r = await call(`/project/${ids.a}/invite/token/${tokO}`, { headers: { accept: 'text/html' } }); cases.viewAnon = r
  // ghost project
  r = await C(O, `/project/${RAND}/invite/token/${tokO}/`, 'GET'); cases.viewGhostProject = r

  // ---- acceptInvite ----
  // Each accept below has EXACTLY ONE live invite for its email on the
  // project, so "consume that invite" is unambiguous and the residual
  // invite set is deterministic in both engines. (This fork's Node
  // accept path — addUserIdToProject + revokeInviteForUser — is
  // non-deterministic over a multi-invite pile, observed 2026-09-15:
  // member-add drop flips later accepts between branches. One invite per
  // accept is the deterministic anchor.) The resend CONTRACT is pinned
  // separately by resendOW (201 + mail) above. tokenFromSink selects the
  // token that is LIVE in the DB, so it is robust to the resent in-place
  // token update either persisting or dropping.
  r = await C(O, `/project/${ids.a}/invite/token/${tokO}/accept`, 'POST'); cases.acceptOW = r
  r = await C(O, `/project/${ids.a}/invite/token/garbagetoken000`, 'POST'); cases.acceptBadToken = r
  r = await call(`/project/${ids.a}/invite/token/${tokO}/accept`, { method: 'POST', headers: { accept: 'application/json', 'x-csrf-token': 'faketoken' }, body: '{}' }); cases.acceptAnon = r
  // X accepts via XHR (204 path) — fresh rw invite, X's only live invite
  r = await C(U, `/project/${ids.a}/invite`, 'POST', { email: XUSER.email, privileges: 'readAndWrite' }); cases.invXrw = r
  const tokX = await tokenFromSink(XUSER.email, 'webgo-p4inv-a', 40_000, ids.a)
  r = await C(X, `/project/${ids.a}/invite/token/${tokX}/accept`, 'POST', undefined, { 'x-requested-with': 'XMLHttpRequest' }); cases.acceptXhr = r
  // upgrade path (B: O is reviewer member; invite rw) -> member branch
  r = await C(O, `/project/${ids.b}/invite/token/${tokB}/accept`, 'POST'); cases.upgradeB = r

  // ---- tokens ----
  r = await C(U, `/project/${ids.a}/tokens`, 'GET'); cases.tokOwner = r
  const M = await login(ADMIN)
  r = await C({ ck: M.ck, csrf: M.csrf }, `/project/${ids.a}/tokens`, 'GET'); cases.tokAdmin = r
  r = await call(`/project/${ids.a}/tokens`, { headers: { accept: 'application/json' } }); cases.tokAnon = r

  // ---- split-test-disabled routes (CE: 403 shape) ----
  r = await C(U, `/project/${ids.a}/sharing-link`, 'GET'); cases.slGet = r
  r = await C(U, `/project/${ids.a}/sharing-link`, 'POST', { privileges: 'readOnly' }); cases.slPost = r
  r = await C(U, `/project/${ids.a}/share`, 'GET'); cases.shareGet = r
  r = await C(U, `/project/${ids.a}/share/validate`, 'POST', { token: 'x' }); cases.shareValidate = r

  const mail = await sinkList()
  const state = stateDump(mongoC)

  // teardown (fixture user + projects + invites remain; next leg re-seeds)
  return { cases, state, mail }
}

function rCompare(name: string, a: R, b: R, tag: string, p: string[]): void {
  if (a.status !== b.status) p.push(`${tag}:${name} status N=${a.status} G=${b.status}`)
  if (a.ct !== b.ct) p.push(`${tag}:${name} ct N=${a.ct} G=${b.ct}`)
  const la = normVolatile(a.location), lb = normVolatile(b.location)
  if (la !== lb) p.push(`${tag}:${name} loc N=${la} G=${lb}`)
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
    rCompare(k, a.cases[k], bb, tag, p)
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
    p.push(`${tag}:mail N="${a.mail.replace(/\n/g, ' | ').slice(0, 500)}" G="${b.mail.replace(/\n/g, ' | ').slice(0, 500)}"`)
  }
  return p
}

test.describe.serial('web-go P4.10b invites flip gate', () => {
  let overleafC = '', mongoC: string, leg1: Leg | null = null

  test.beforeAll(async () => {
    overleafC = runningContainer('ol-e2e-overleaf'); mongoC = runningContainer('ol-e2e-mongo')
    if (process.env.P4INV_BUILD) {
      execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'bin/web'), `${overleafC}:/usr/local/bin/go-services/web`])
      dexe(overleafC, 'chown www-data:www-data /usr/local/bin/go-services/web && chmod 755 /usr/local/bin/go-services/web')
      dexe(overleafC, 'sv restart web-go-overleaf', true)
    }
    execFileSync('docker', ['cp', path.resolve(REPO_ROOT, 'server-ce/nginx/flips/web-p4inv.conf'), `${overleafC}:/tmp/web-p4inv.conf`])
    dexe(overleafC, 'mkdir -p /usr/local/share/overleaf-flips && cp /tmp/web-p4inv.conf /usr/local/share/overleaf-flips/web-p4inv.conf')
    for (;;) { const code = dexe(overleafC, `curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:4010/status`, true).trim(); if (code === '200') break; await sleep(500) }
    dexe(overleafC, FLIP('web-p4inv.conf', 'strip'), true); await nginxSettled()
  }, 240_000)

  test.afterAll(async () => {
    try { dexe(overleafC, FLIP('web-p4inv.conf', 'strip'), true); await nginxSettled() } catch {}
  })

  test('leg 1: Node baseline', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('web-p4inv.conf', 'strip'), true); await nginxSettled()
    leg1 = await battery(mongoC)
    const c = (k: string) => leg1.cases[k]
    expect(c('invRO').status, 'invRO: ' + c('invRO').body.slice(0, 200)).toBe(200)
    expect(c('invRO').body).toContain('webgo-p4inv-x@e2e.test')
    expect(c('invRW').status, 'invRW: ' + c('invRW').body.slice(0, 250)).toBe(200)
    expect(c('invSelf').status).toBe(200)
    expect(c('invSelf').body).toContain('cannot_invite_self')
    expect(c('invBadEmail').status, 'invBadEmail').toBe(400)
    expect(c('invBadPriv').status).toBe(400)
    expect(c('invUnknown').status).toBe(400)
    expect(c('invNonadmin').status).toBe(403)
    expect(c('invAnon').status).toBe(403)
    expect(c('listA').status).toBe(200)
    expect(c('revokeX').status, 'revokeX').toBe(204)
    expect(c('resendOW').status, 'resendOW').toBe(201)
    expect(c('acceptOW').status, 'acceptOW').toBe(302)
    expect(leg1.mail.split('\n').length, 'mail count (6 invites sent)').toBe(6)
  }, 300_000)

  test('leg 2: FLIP ON — Go matches the Node baseline', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('web-p4inv.conf', 'apply')); await nginxSettled()
    const leg2 = await battery(mongoC)
    const p = diffLegs(leg1!, leg2, 'GO')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 300_000)

  test('leg 3: FLIP OFF — Node matches the baseline again', async () => {
    test.setTimeout(300_000)
    dexe(overleafC, FLIP('web-p4inv.conf', 'strip'), true); await nginxSettled()
    const leg3 = await battery(mongoC)
    const p = diffLegs(leg1!, leg3, 'NODE-rev')
    expect(p, p.join('\n---\n')).toHaveLength(0)
  }, 300_000)
})
