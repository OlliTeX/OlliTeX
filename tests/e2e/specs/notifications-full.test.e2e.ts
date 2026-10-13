/**
 * INTENSIVE notifications feature suite (TODO-0f553d2d) — the RECEIVER side.
 *
 * Prior coverage (honest gap this closes):
 *  - parity/service-chat-notifications.test.e2e.ts (B3): chat round-trip + the
 *    notifications service POST /user/:uid + GET list with a SYNTHETIC key,
 *    single user — i.e. the API shape, not a real delivery.
 *  - notifications.test.e2e.ts: preferences surface only (legacy 404 pin +
 *    hub email-leaf mute switch).
 *
 * This spec drives the REAL emitter (Go chat notify fan-out) and asserts the
 * whole recipient pipeline in the live world:
 *
 *   A(owner) ──POST /project/:pid/thread/:tid/messages──▶ 201
 *       Go chat notify.go (Node 6.3 parity):
 *         recipients = [owner, collab..., readOnly..., tokenAccess...] − sender,
 *         gated by muted set + per-project prefs (absent key ⇒ notify TRUE,
 *         pinned in go/services/chat/notify.go prefBool)
 *         ├─ UpsertNotification  → GET /user/:uid list entry
 *         └─ UpsertEmailNotification → emailNotifications doc (recipient_id;
 *            world note: the MAIL SENDER (cronmail service) is NOT running in
 *            the e2e world — the scheduled doc is the pinnable mail-side state)
 *
 * VERIFIED LIVE 2026-10-13 (ol-e2e-overleaf-1, ag-v57 world) before pinning:
 *  1. comment (first msg of thread)  → B list gains
 *       key=project-comment-<pid>-<tid>-<mid>, templateKey=notification_comment_on_project,
 *       messageOpts={projectId, projectName, userName, threadId}
 *  2. reply (same thread, as B)      → A (thread author) gains
 *       key=project-reply-<pid>-<tid>-<mid>, templateKey=notification_reply_on_project;
 *       B itself gets NOTHING (sender-excluded)
 *  3. mail leg                       → emailNotifications doc {recipient_id,
 *       project_id, emailType:"projectNotification", opts{isComment,projectName,userName,threadId}}
 *  4. read-state                     → DELETE /user/:uid/notification/:nid 200 ⇒ entry gone
 *  5. mute negative                  → prefs commentOnInvitedProject:false ⇒ new comment,
 *       B list UNCHANGED (send still 201)
 *  6. shape pin                      → DELETE /user/:uid without body ⇒ 400 "invalid JSON: EOF"
 *
 * World-fidelity notes (pinned, NOT masked):
 *  - the notifications service API has NO auth gate in this port (internal
 *    trust boundary; probes run unauthenticated by design — Node parity).
 *  - collab membership must be the Node-typo field `collaberator_refs: [ObjectId]`
 *    (flat array of ObjectIds — go/services/chat/chat.go ProjectRefs; objects
 *    { _id } silently decode away, D2-class drift).
 *  - mongosh in this world has NO `oid()` helper — use `new ObjectId()`.
 *
 * Exit criteria (TODO): green suite + the two prior notification specs stay green.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'

const WEB = process.env.OL_E2E_CONTAINER || 'ol-e2e-overleaf-1'
const MONGO = process.env.OL_E2E_MONGO_CONTAINER || 'ol-e2e-mongo-1'

const CHAT_PORT = 3010 // in-container chat service
const NOTIF_PORT = 3042 // in-container notifications service

// ---------------------------------------------------------------- helpers ---

function sh(args: string[], timeoutMs = 20_000): string {
  try {
    return execFileSync('docker', args, { encoding: 'utf8', timeout: timeoutMs })
  } catch (e: any) {
    return (e.stdout || '') + '\n' + (e.stderr || '').toString().slice(0, 400)
  }
}

/** in-container HTTP probe (the world's service ports are loopback-bound inside web). */
function probe(
  method: string,
  url: string,
  body?: unknown,
): { status: number; body: string } {
  const args = [
    'exec', WEB, 'sh', '-c',
    `curl -s -m 6 -o /tmp/nf_full_body -w '%{http_code}' -X ${method} '${url}'` +
      (body !== undefined ? ` -H 'Content-Type: application/json' -d '${JSON.stringify(body).replace(/'/g, "'\\''")}'` : '') +
      `; echo; cat /tmp/nf_full_body 2>/dev/null`,
  ]
  const out = sh(args).split('\n')
  const status = Number(out.shift() || 0)
  const rawBody = out.join('\n')
  let parsed: unknown = null
  try { parsed = JSON.parse(rawBody) } catch { parsed = null }
  return { status, body: rawBody, parsed }
}

/** mongosh via script file (avoids shell-quoting traps; mongosh here lacks oid()). */
function mongoEval(js: string, label: string): string {
  const hostFile = `/tmp/nf_${label}.js`
  fs.writeFileSync(hostFile, js + '\n')
  const f = `/tmp/nf_${label}.js`
  sh(['cp', hostFile, `${MONGO}:${f}`])
  return sh(['exec', MONGO, 'mongosh', 'sharelatex', '--quiet', f], 30_000).trim()
}

// The legacy suite (notifications.test.e2e.ts) logs in as B (fixtures USER) and
// TOGGLES B's global `muteAllNotifications` leaf without restoring it — that state
// suppresses the whole fan-out (chat notify.go MutedUserIDs gate) and leaks across
// runs. This suite snapshots it, normalizes to unmuted for delivery legs, and
// restores at teardown. (Pinned 2026-10-13: the pollution pattern + the gate.)
function bMuteSaved(): void {
  if (savedBMute !== '') return
  const v = mongoEval(
    'var d = db.notificationsPreferences.findOne({user_id: new ObjectId("' + UB + '"), project_id: null}); print(d ? String(d.muteAllNotifications === true) : "absent")',
    'bmutesave',
  ).trim()
  savedBMute = v
  if (v === 'true') {
    mongoEval(
      'db.notificationsPreferences.updateOne({user_id: new ObjectId("' + UB + '"), project_id: null}, {$set: {muteAllNotifications: false}})',
      'bmuteclear',
    )
  }
}
function restoreBMute(): void {
  if (savedBMute === 'true') {
    mongoEval(
      'db.notificationsPreferences.updateOne({user_id: new ObjectId("' + UB + '"), project_id: null}, {$set: {muteAllNotifications: true}}, {upsert: true})',
      'bmuterestore',
    )
  }
  savedBMute = ''
}
let savedBMute = ''

test.afterAll(() => restoreBMute())

// ----------------------------------------------------------------- world ---

const RUN = Date.now().toString(36)

function userHex(email: string): string {
  const v = mongoEval(
    `var u = db.users.findOne({email: "${email}"}); print(u ? u._id.toHexString() : "NONE")`,
    'user_' + email.replace(/[^a-z]/g, ''),
  ).trim()
  expect(v, 'world user missing: ' + email).not.toBe('NONE')
  return v
}

const UA = userHex('e2e-admin@e2e.test')
const UB = userHex('e2e-user@e2e.test')

let PID = ''
function ensureProjectWithCollaboratorB(): void {
  if (PID) return
  const v = mongoEval(
    [
      `var p = db.projects.findOne({owner_ref: new ObjectId("${UA}"), active: true})`,
      `print(p ? p._id.toHexString() : "NONE")`,
    ].join('\n'),
    'pid',
  ).trim()
  if (v === 'NONE') throw new Error('no active owner project for the admin fixture user')
  PID = v
  // ensure B is a collaborator in the shape the Go chat emitter decodes
  mongoEval(
    [
      `var pid = new ObjectId("${PID}")`,
      `var b = new ObjectId("${UB}")`,
      `db.projects.updateOne({_id: pid}, {$addToSet: {collaberator_refs: b}})`,
      `db.projects.updateOne({_id: pid}, {$set: {active: true}})`,
      `print(JSON.stringify(db.projects.findOne({_id: pid}).collaberator_refs))`,
    ].join('\n'),
    'setup',
  )
}

function freshThreadId(): string {
  const v = mongoEval('print(new ObjectId().toHexString())', 'tid_' + RUN + Math.random().toString(36).slice(2, 8))
  expect(v, 'fresh thread id').toMatch(/^[0-9a-f]{24}$/)
  return v
}

function listEntries(uid: string): any[] {
  const r = probe('GET', `http://127.0.0.1:${NOTIF_PORT}/user/${uid}`)
  expect(r.status, 'GET /user/:uid → 200, got ' + r.status + ' ' + r.body).toBe(200)
  const d = JSON.parse(r.body)
  expect(Array.isArray(d)).toBe(true)
  return d
}

function entriesForThread(uid: string, tid: string): any[] {
  return listEntries(uid).filter((e) => (e.key || '').includes('-' + tid + '-'))
}

function pollForThreadEntry(uid: string, tid: string, expectPresent: boolean, ms = 9_000): any[] {
  const t0 = Date.now()
  let cur: any[] = []
  for (;;) {
    cur = entriesForThread(uid, tid)
    if (expectPresent ? cur.length > 0 : cur.length === 0) return cur
    if (Date.now() - t0 > ms) return (expectPresent ? [] : cur)
    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 500)
  }
}

function sendAs(uid: string, tid: string, content: string): { status: number; body: any } {
  const r = probe('POST', `http://127.0.0.1:${CHAT_PORT}/project/${PID}/thread/${tid}/messages`, {
    user_id: ua(),
    content,
  })
  ;(r as any).uid = uid
  return r as any
}
function ua(): string {
  // resolved by the caller-closed-over sender id (kept here for the probe body)
  return SENDER
}
let SENDER = ''

// ------------------------------------------------------------------- legs ---

test.describe('notifications-full (receiver side, Go chat fan-out)', () => {
  test('L1: first comment in a thread → collaborator gets list entry (comment template)', () => {
    ensureProjectWithCollaboratorB()
    bMuteSaved()
    const tid = freshThreadId()
    SENDER = UA
    const r = sendAs(UA, tid, `nf-full-L1-${RUN}`)
    expect(r.status, 'chat send → 201, got ' + r.status + ' ' + JSON.stringify(r.body)).toBe(201)
    const entries = pollForThreadEntry(UB, tid, true)
    expect(entries.length, 'B must receive exactly one entry for the comment').toBe(1)
    const e = entries[0]
    expect(e.templateKey).toBe('notification_comment_on_project')
    expect(e.key).toBe(`project-comment-${PID}-${tid}-${(r.parsed as any).id}`)
    // messageOpts contract (notify.go messageOpts map)
    expect(e.messageOpts.projectId).toBe(PID)
    expect(e.messageOpts.threadId).toBe(tid)
    expect(typeof e.messageOpts.projectName).toBe('string')
    expect(typeof e.messageOpts.userName).toBe('string')
    expect(e.user_id).toBe(UB)
  })

  test('L2: reply in the same thread → author notified (reply template); sender not self-notified', () => {
    ensureProjectWithCollaboratorB()
    bMuteSaved()
    const tid = freshThreadId()
    // seed the thread as A (author), then B replies (participants = A, B)
    SENDER = UA
    const seed = sendAs(UA, tid, `nf-full-L2seed-${RUN}`)
    expect(seed.status).toBe(201)
    SENDER = UB
    const reply = sendAs(UB, tid, `nf-full-L2reply-${RUN}`)
    expect(reply.status).toBe(201)
    const forA = pollForThreadEntry(UA, tid, true)
    const replyA = forA.filter((e) => (e.key || '').startsWith('project-reply-'))
    expect(replyA.length, 'thread author must get the reply entry').toBe(1)
    expect(replyA[0].templateKey).toBe('notification_reply_on_project')
    expect(replyA[0].key).toBe(`project-reply-${PID}-${tid}-${(reply.parsed as any).id}`)
    // sender (B) is NEVER notified about its own message (sender-excluded in notify.go)
    const forB = entriesForThread(UB, tid)
    const selfReplies = forB.filter((e) => (e.key || '').startsWith('project-reply-'))
    expect(selfReplies.length, 'B must not be self-notified for its own reply').toBe(0)
    // B still holds the earlier comment entry (not retroactively removed)
    expect(forB.some((e) => (e.key || '').startsWith('project-comment-'))).toBe(true)
  })

  test('L3: mail leg — emailNotifications scheduled doc per recipient (recipient_id form)', () => {
    ensureProjectWithCollaboratorB()
    bMuteSaved()
    const tid = freshThreadId()
    SENDER = UA
    const r = sendAs(UA, tid, `nf-full-L3-${RUN}`)
    expect(r.status).toBe(201)
    // Node-parity upsert is PER (recipient, project) — a second comment on the same
    // project REFRESHES the same doc (scheduledAt/updatedAt) rather than adding one.
    // Assert the refresh: the doc gains OUR fresh threadId in opts.
    const docq = (
      'var d = db.emailNotifications.findOne({recipient_id: new ObjectId("' + UB + '"), project_id: new ObjectId("' + PID + '")})'
    )
    let doc = 'null'
    for (let i = 0; i < 12; i++) {
      doc = mongoEval(docq + '\nprint(d ? JSON.stringify(d) : "null")', 'maildoc' + RUN + i).trim()
      if (doc !== 'null' && doc.includes(tid)) break
      Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 500)
    }
    expect(doc, 'mail-side scheduled doc must exist for B on this project').not.toBe('null')
    expect(doc).toContain('projectNotification')
    expect(doc).toContain(`"${tid}"`)
    expect(doc).toContain('"isComment":true')
    // world note (pinned): the sending service (cronmail) is not running in the
    // e2e world — the scheduled doc IS the mail-side pinnable state, not a sent mail.
  })

  test('L4: read-state — DELETE /user/:uid/notification/:nid removes the entry', () => {
    ensureProjectWithCollaboratorB()
    bMuteSaved()
    const tid = freshThreadId()
    SENDER = UA
    const r = sendAs(UA, tid, `nf-full-L4-${RUN}`)
    expect(r.status).toBe(201)
    const mine = pollForThreadEntry(UB, tid, true)
    const target = mine[0]._id
    const del = probe('DELETE', `http://127.0.0.1:${NOTIF_PORT}/user/${UB}/notification/${target}`)
    expect(del.status, 'entry delete → 200, got ' + del.status + ' ' + del.body).toBe(200)
    const now = entriesForThread(UB, tid)
    expect(now.some((e) => e._id === target), 'entry must be gone after delete').toBe(false)
  })

  test('L5: mute negative — prefs commentOnInvitedProject:false suppresses the fan-out', () => {
    ensureProjectWithCollaboratorB()
    bMuteSaved()
    mongoEval(
      [
        `db.notificationsPreferences.updateOne(`,
        `  {user_id: new ObjectId("${UB}"), project_id: new ObjectId("${PID}")},`,
        `  {$set: {commentOnInvitedProject: false}}, {upsert: true})`,
      ].join('\n'),
      'muteon' + RUN,
    )
    try {
      const tid = freshThreadId()
      SENDER = UA
      const r = sendAs(UA, tid, `nf-full-L5-${RUN}`)
      expect(r.status, 'send itself still 201').toBe(201)
      const entries = pollForThreadEntry(UB, tid, false, 3_500)
      expect(entries.length, 'muted B must NOT receive the notification').toBe(0)
    } finally {
      mongoEval(
        `db.notificationsPreferences.deleteMany({user_id: new ObjectId("${UB}"), project_id: new ObjectId("${PID}")})`,
        'muteoff' + RUN,
      )
    }
    // control: with the mute cleared, a fresh comment delivers again (re-arm)
    const tid2 = freshThreadId()
    SENDER = UA
    expect(sendAs(UA, tid2, `nf-full-L5ctrl-${RUN}`).status).toBe(201)
    const entries = pollForThreadEntry(UB, tid2, true)
    expect(entries.length, 'after un-mute delivery resumes').toBe(1)
  })

  test('L6: shape pin — DELETE /user/:uid (key-clear) without body → 400 invalid JSON', () => {
    const r = probe('DELETE', `http://127.0.0.1:${NOTIF_PORT}/user/${UB}`)
    expect(r.status, 'bodyless key-clear must be 400 (Node parity), got ' + r.status).toBe(400)
    expect(r.body).toContain('invalid JSON')
  })
})
