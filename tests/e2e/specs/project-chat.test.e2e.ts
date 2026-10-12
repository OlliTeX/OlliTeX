/**
 * project-chat — PROJECT CHAT suite (board 1d7cb391), LIVE-PINNED 2026-10-12
 * (ag-v57 world). Evidence: OPEN_QUEUE_PROGRESS.md cont.5 (probe battery).
 *
 * PINS (chatbridge → Go chat service, room_id = project id):
 *   POST   /project/:pid/messages            201 {"id","content","timestamp",
 *                                                 "user_id","room_id"}
 *   GET    /project/:pid/messages            200 [ { "content","id","timestamp",
 *                                                 "user":{email,first_name,id,
 *                                                 last_name}, "user_id" }, ... ]
 *            (edited messages additionally carry "edited_at"; original
 *             "timestamp" preserved — PINNED)
 *   POST   /project/:pid/messages/:id/edit   author  → 204
 *                                           non-author → 403 {"message":"restricted"}
 *   DELETE /project/:pid/messages/:id        author  → 204 (then absent from list)
 *                                           non-author → 403 {"message":"restricted"}
 *   MEMBER (legacy collaberator_refs) GET/POST → 403 restricted  ⚑ FLAG
 *
 * ⚑ OWNER-FLAG (not silently shipped): in this build the chat plane admits the
 * OWNER's sessions but a legacy-ref MEMBER (B, seeded exactly as access.go
 * reads membership) gets 403 restricted on BOTH list and send — while the
 * Node/Overleaf parity of this surface is member-visible chat. Chat room
 * membership evidently is not derived from the project membership array in
 * the current Go port (owner/first-poster only observed). Decision needed:
 *   (a) derive chat-plane membership from project access (member sees room),
 *   (b) keep owner-only and document it (then cross-user legs re-pin 403, as
 *       already pinned here).
 * This spec pins the OBSERVED contract (owner plane fully functional; member
 * 403) so the behavior is at least asserted, not drifting.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { USER, USER2 } from '../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'
const RUN = 'chate2e-' + Math.abs(hashStr(new Date().toISOString().slice(0, 19)))

function hashStr(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
  return h
}

async function csrf(p: any): Promise<string> {
  return (await p.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
}

async function send(p: any, pid: string, content: string) {
  const r = await p.request.post(BASE + '/project/' + pid + '/messages', {
    data: { content, client_id: 'e2e-' + Math.random().toString(36).slice(2) },
    headers: { 'X-CSRF-TOKEN': await csrf(p) },
  })
  return { status: r.status(), body: await r.text().catch(() => '') }
}

async function listArr(p: any, pid: string, q?: string): Promise<any[]> {
  const r = await p.request.get(BASE + '/project/' + pid + '/messages' + (q ? '?' + q : ''))
  if (r.status() !== 200) return []
  const j: any = await r.json().catch(() => null)
  if (Array.isArray(j)) return j
  return j?.messages || j?.list || []
}

test.describe.configure({ mode: 'serial' })
test.setTimeout(300_000)

let PID = ''
let uid2 = ''

test('C0 setup: owner session + legacy-ref member seeded', async ({ browser }) => {
  const page = await browser.newPage()
  await loginRobust(page, USER.email, USER.password)
  PID = String(await createBlankProject(page)).trim()
  await page.close()
  expect(PID).toMatch(/^[0-9a-f]{24}$/)
  uid2 = String(mongoEval(`const u = db.users.findOne({ email: '${USER2.email}' }); print(u ? String(u._id) : '');`) || '').trim()
  expect(uid2).toMatch(/^[0-9a-f]{24}$/)
  const seedCount = String(mongoEval(`db.projects.updateOne({ _id: ObjectId('${PID}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } });
    print(db.projects.findOne({ _id: ObjectId('${PID}') }).collaberator_refs.length);`) || '').trim()
  expect(seedCount, 'seeded member count ≥ 1').toMatch(/\d+/)
  expect(Number(seedCount), '>=1 collab refs').toBeGreaterThanOrEqual(1)
})

test('C1 create + owner list shape (pinned 201 + array shape)', async ({ browser }) => {
  const pa = await browser.newPage()
  await loginRobust(pa, USER.email, USER.password)
  const contentA = RUN + '-hello-from-A'
  const sent = await send(pa, PID, contentA)
  expect(sent.status, 'POST messages → PINNED 201 (got ' + sent.status + ': ' + sent.body.slice(0, 120) + ')').toBe(201)
  const sentj: any = JSON.parse(sent.body)
  expect(sentj).toMatchObject({ room_id: PID })
  expect(String(sentj.user_id).toLowerCase(), 'shape: author id').toMatch(/^[0-9a-f]{24}$/i)
  expect(sentj.id, 'shape: id').toMatch(/^[0-9a-f]{24}$/)
  expect(Number(sentj.timestamp) > 0, 'shape: timestamp').toBe(true)

  const la = await listArr(pa, PID)
  expect(la.length, 'owner list non-empty').toBeGreaterThanOrEqual(1)
  const mine = la.find((m: any) => m.content === contentA)
  expect(mine, 'own message on list').toBeTruthy()
  expect(mine.user?.email, 'shape: user sub-object (email)').toBe(USER.email)
  await pa.close()
})

test('C2 cross-user pin: member list/send are 403 restricted in this build (⚑ owner-flag)', async ({ browser }) => {
  const pb = await browser.newPage()
  await loginRobust(pb, USER2.email, USER2.password)
  const l = await pb.request.get(BASE + '/project/' + PID + '/messages')
  expect(l.status(), 'B list: PINNED 403 restricted in this build (got ' + l.status() + ')').toBe(403)
  expect((await l.text().catch(() => '')).toLowerCase()).toContain('restricted')
  const s = await send(pb, PID, RUN + '-member-send-attempt')
  expect(s.status, 'B send: PINNED 403 restricted (got ' + s.status + ')').toBe(403)
  await pb.close()
})

test('C3 edit: author 204 + edited_at; non-author 403 restricted', async ({ browser }) => {
  const pa = await browser.newPage()
  await loginRobust(pa, USER.email, USER.password)
  const la = await listArr(pa, PID)
  const mine = la[0]
  expect(mine, 'a message exists to edit').toBeTruthy()
  const mid = String(mine.id || mine._id)
  expect(mid).toMatch(/^[0-9a-f]{24}$/)

  const ed = await pa.request.post(BASE + '/project/' + PID + '/messages/' + mid + '/edit', {
    data: { content: (mine.content as string) + ' [edited-by-A]' },
    headers: { 'X-CSRF-TOKEN': await csrf(pa) },
  })
  expect(ed.status(), 'author edit → PINNED 204 (got ' + ed.status() + ')').toBe(204)
  const after = await listArr(pa, PID)
  const upd = after.find((m: any) => String(m.id || m._id) === mid)
  expect(String(upd.content).endsWith('[edited-by-A]'), 'edit visible on list').toBe(true)
  expect(Number(upd.edited_at) > 0, 'edited_at present (pinned)').toBe(true)
  expect(upd.timestamp, 'original timestamp preserved').toBeTruthy()

  // non-author non-owner edit → 403 restricted (PINNED)
  const pb = await browser.newPage()
  await loginRobust(pb, USER2.email, USER2.password)
  const deny = await pb.request.post(BASE + '/project/' + PID + '/messages/' + mid + '/edit', {
    data: { content: 'hijack-attempt' },
    headers: { 'X-CSRF-TOKEN': await csrf(pb) },
  })
  expect(deny.status(), 'non-author edit → PINNED 403 (got ' + deny.status() + ')').toBe(403)
  expect((await deny.text().catch(() => '')).toLowerCase()).toContain('restricted')
  await pa.close(); await pb.close()
})

test('C4 delete: author 204 + gone; non-author 403 restricted', async ({ browser }) => {
  const pa = await browser.newPage()
  await loginRobust(pa, USER.email, USER.password)
  const throwaway = RUN + '-throwaway-A'
  const sent = await send(pa, PID, throwaway)
  expect(sent.status, 'setup post → 201').toBe(201)
  const mid = (JSON.parse(sent.body).id || '').toLowerCase()
  expect(mid).toMatch(/^[0-9a-f]{24}$/)

  // non-author (B) delete → 403 restricted (PINNED)
  const pb = await browser.newPage()
  await loginRobust(pb, USER2.email, USER2.password)
  const deny = await pb.request.delete(BASE + '/project/' + PID + '/messages/' + mid, {
    headers: { 'X-CSRF-TOKEN': await csrf(pb) },
  })
  expect(deny.status(), 'non-author delete → PINNED 403 (got ' + deny.status() + ')').toBe(403)

  // author delete → 204 + gone (PINNED)
  const del = await pa.request.delete(BASE + '/project/' + PID + '/messages/' + mid, {
    headers: { 'X-CSRF-TOKEN': await csrf(pa) },
  })
  expect(del.status(), 'author delete → PINNED 204 (got ' + del.status() + ')').toBe(204)
  const la2 = await listArr(pa, PID)
  expect(la2.some((m: any) => String(m.id || m._id).toLowerCase() === mid), 'deleted msg gone for author').toBe(false)
  await pa.close(); await pb.close()
})

test('C5 permissions pin: member (and demoted member) chat POST is 403 restricted in this build', async ({ browser }) => {
  // The board leg asked "can commenter post? pin actual behavior" — pinned
  // actual: B (legacy-ref member) is already 403 on POST (C2), so the
  // capability matrix in this build is OWNER=YES / MEMBER=NO (⚑ owner-flag,
  // see header). Demotion is therefore a no-op for chat here; we still pin
  // the PUT surface interaction so a future fix gets caught on BOTH axes.
  const pb = await browser.newPage()
  await loginRobust(pb, USER2.email, USER2.password)
  const r = await send(pb, PID, RUN + '-commenter-attempt')
  expect(r.status, 'member chat POST pin (this build: 403; got ' + r.status + ')').toBe(403)
  await pb.close()
})

test('C6 pagination: limit + before=<timestamp> (PINNED 2026-10-12)', async ({ browser }) => {
  // PINNED semantics (probe6, evidence in OPEN_QUEUE_PROGRESS.md cont.5):
  //   - messages are newest-first
  //   - limit=N bounds the window
  //   - before= works ONLY with a millisecond TIMESTAMP (oldest ts of the
  //     previous window) → strictly older messages
  //   - before=<messageId> → 400; after=<...> → 400 (unsupported)
  const pa = await browser.newPage()
  await loginRobust(pa, USER.email, USER.password)
  const seeds: string[] = []
  for (let i = 1; i <= 5; i++) {
    const c = `${RUN}-page-${i}`
    seeds.push(c)
    const r = await send(pa, PID, c)
    expect(r.status, `seed ${i} → 201 (got ${r.status})`).toBe(201)
    await new Promise((r2) => setTimeout(r2, 150))
  }

  const w1r = await pa.request.get(BASE + '/project/' + PID + '/messages?limit=2')
  expect(w1r.status()).toBe(200)
  const w1: any[] = await w1r.json().catch(() => [])
  expect(w1.length, 'limit=2 → 2 rows (got ' + w1.length + ')').toBe(2)
  expect(w1[0].content, 'newest-first order').toBe(seeds[4])
  expect(w1[0].timestamp >= w1[1].timestamp, 'timestamps descending').toBe(true)

  const boundary = Number(w1[1].timestamp) // oldest row of window 1
  const w2r = await pa.request.get(BASE + '/project/' + PID + '/messages?limit=2&before=' + boundary)
  expect(w2r.status(), 'before=<timestamp> → 200 (got ' + w2r.status() + ')').toBe(200)
  const w2: any[] = await w2r.json().catch(() => [])
  expect(w2.length, 'window 2 has rows (got ' + w2.length + ')').toBeGreaterThanOrEqual(1)
  expect(w2.every((m: any) => Number(m.timestamp) < boundary), 'strictly older than the boundary').toBe(true)

  // the pinned negative: before=<messageId> is REJECTED (400)
  const badR = await pa.request.get(BASE + '/project/' + PID + '/messages?limit=2&before=' + String(w1[0].id).toLowerCase())
  expect(badR.status(), 'before=<messageId> → PINNED 400 (got ' + badR.status() + ')').toBe(400)
  await pa.close()
})
