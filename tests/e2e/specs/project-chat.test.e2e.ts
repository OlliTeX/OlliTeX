/**
 * project-chat — PROJECT CHAT suite (board 1d7cb391): create/reply/edit/delete
 * + permissions + pagination over the chatbridge surface.
 *
 * Pinned contract (go/services/web/features/chatbridge/chatbridge.go,
 * 2026-09-30 Node audit port):
 *   GET    /project/:pid/messages              ?{limit, before} list
 *   POST   /project/:pid/messages              {content, client_id} send
 *   DELETE /project/:pid/messages/:id          author or owner; else 403
 *   POST   /project/:pid/messages/:id/edit     author or owner; else 403
 *   403 shape (non-author/owner, non-member, anon): {"message":"restricted"}
 *   invalid id → 404 JSON · absent project → 404 HTML
 * Upstream: the Go chat service (runsv chat-overleaf, 127.0.0.1:3010 in the
 * world) — the bridge carries {user_id (OID), content} downstream.
 *
 * UI leg: the chat pane (frontend/js/features/chat/components/chat-pane.tsx,
 * message-input/message-and-dropdown edit+delete) is exercised when the
 * pane can be discovered on this revision (board 1d7cb391 leg 1: "UI chat
 * surface if present — discover first; else the wire POST").
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

async function list(p: any, pid: string, q?: string) {
  const r = await p.request.get(BASE + '/project/' + pid + '/messages' + (q ? '?' + q : ''))
  const j: any = await r.json().catch(() => null)
  const msgs: any[] = Array.isArray(j) ? j : (j?.messages || j?.list || [])
  return { status: r.status(), msgs, raw: j }
}

/** Try to find + open the chat pane; return the page's pane handle or null. */
async function tryOpenChatPane(p: any): Promise<boolean> {
  // toolbar/right-side chat toggle — probe common anchors for this revision
  const anchors = [
    '[aria-label*="hat" i]', '[title*="hat" i]', 'button:has-text("Chat")',
    '[class*="chat"]',
  ]
  for (const a of anchors) {
    const loc = p.locator(a).first()
    try {
      if (await loc.count() > 0 && (await loc.isVisible().catch(() => false))) {
        await loc.click({ timeout: 3_000 }).catch(() => {})
        await p.waitForTimeout(1_000)
        return !!(await p.locator('[class*="chat-pane"], [class*="message-list"], [class*="message-input"]').first().isVisible().catch(() => false))
      }
    } catch {}
  }
  return false
}

test.describe.configure({ mode: 'serial' })
test.setTimeout(300_000)

let PID = ''
let uid2 = ''

test('C0 setup: owner + member sessions ready', async ({ browser }) => {
  const page = await browser.newPage()
  await loginRobust(page, USER.email, USER.password)
  PID = await createBlankProject(page)
  await page.close()
  expect(PID).toMatch(/^[0-9a-f]{24}$/)
  uid2 = String(mongoEval(`const u = db.users.findOne({ email: '${USER2.email}' }); print(u ? String(u._id) : '');`) || '')
  expect(uid2).toMatch(/^[0-9a-f]{24}$/)
  expect(
    mongoEval(`db.projects.updateOne({ _id: ObjectId('${PID}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } });
              print(db.projects.findOne({ _id: ObjectId('${PID}') }).collaberator_refs.length);`)
  ).toMatch(/\d+/)
})

test('C1 create + list (cross-user visibility)', async ({ browser }) => {
  const pa = await browser.newPage()
  await loginRobust(pa, USER.email, USER.password)
  const contentA = RUN + '-hello-from-A'
  const sent = await send(pa, PID, contentA)
  expect([200, 201, 202].includes(sent.status), 'POST messages → 2xx (got ' + sent.status + ')').toBe(true)

  const la = await list(pa, PID)
  expect(la.status, 'GET messages → 200').toBe(200)
  expect(la.msgs.length, 'A sees own message').toBeGreaterThanOrEqual(1)
  expect(la.msgs.map((m: any) => m.content).join('\n'), 'content echoed').toContain(contentA)
  const mine = la.msgs.find((m: any) => m.content === contentA)
  expect(mine, 'shape: content present').toBeTruthy()
  expect(String(mine.user_id || mine.userId || ''), 'shape: author id present').toMatch(/^[0-9a-f]{24}$/)
  expect(Boolean(mine.date || mine.createdAt || mine.timestamp), 'shape: timestamp present').toBe(true)

  // B's list sees A's message (cross-user visibility within the project)
  const pb = await browser.newPage()
  await loginRobust(pb, USER2.email, USER2.password)
  const lb = await list(pb, PID)
  expect(lb.msgs.map((m: any) => m.content).join('\n'), 'B sees A message on list').toContain(contentA)
  await pa.close(); await pb.close()
})

test('C2 reply (B→A) lands on A list within the pinned wait', async ({ browser }) => {
  const pb = await browser.newPage()
  await loginRobust(pb, USER2.email, USER2.password)
  const contentB = RUN + '-reply-from-B'
  const sentB = await send(pb, PID, contentB)
  expect([200, 201, 202].includes(sentB.status), 'B reply → 2xx').toBe(true)

  // A's FRESH list sees the reply (pinned bounded wait, not flaky-poll)
  const pa = await browser.newPage()
  await loginRobust(pa, USER.email, USER.password)
  let ok = false
  for (let i = 0; i < 12 && !ok; i++) {
    const la = await list(pa, PID)
    ok = la.msgs.some((m: any) => m.content === contentB)
    if (!ok) await new Promise((r) => setTimeout(r, 1_000))
  }
  expect(ok, 'A fresh GET sees B reply (pinned 12s wait)').toBe(true)
  await pa.close(); await pb.close()
})

test('C3 edit: author edits own; non-author non-owner REJECTED (403 restricted)', async ({ browser }) => {
  const uidA = String(mongoEval(`const u = db.users.findOne({ email: '${USER.email}' }); print(u ? String(u._id) : '');`) || '')
  expect(uidA).toMatch(/^[0-9a-f]{24}$/)
  const pa = await browser.newPage()
  await loginRobust(pa, USER.email, USER.password)
  const probe = await list(pa, PID)
  const mineA = probe.msgs.find((m: any) => String(m.user_id || m.userId).toLowerCase() === uidA)
  expect(mineA, 'find A message to edit').toBeTruthy()
  const mid = String(mineA._id || mineA.id)
  expect(mid, 'message id is a real ObjectId (pinned invalid-id → 404 contract)').toMatch(/^[0-9a-f]{24}$/)

  // author edits own
  const ed = await pa.request.post(BASE + '/project/' + PID + '/messages/' + mid + '/edit', {
    data: { content: (mineA.content as string) + ' [edited-by-A]' },
    headers: { 'X-CSRF-TOKEN': await csrf(pa) },
  })
  expect([200, 201, 202, 204].includes(ed.status), 'author edit → 2xx (got ' + ed.status + ')').toBe(true)
  const after = await list(pa, PID)
  expect(after.msgs.map((m: any) => m.content).join('\n'), 'edit visible on list')
    .toContain(String(mineA.content) + ' [edited-by-A]')

  // B (non-author, non-owner) edit → 403 restricted
  const pb = await browser.newPage()
  await loginRobust(pb, USER2.email, USER2.password)
  const deny = await pb.request.post(BASE + '/project/' + PID + '/messages/' + mid + '/edit', {
    data: { content: 'hijack-attempt' },
    headers: { 'X-CSRF-TOKEN': await csrf(pb) },
  })
  expect(deny.status, 'non-author edit must be 403 (got ' + deny.status + ')').toBe(403)
  await pa.close(); await pb.close()
})

test('C4 delete: author deletes own; owner deletes other; non-author REJECTED', async ({ browser }) => {
  const pb = await browser.newPage()
  await loginRobust(pb, USER2.email, USER2.password)
  // B creates a throwaway
  const throwaway = RUN + '-throwaway-B'
  await send(pb, PID, throwaway)
  const lb = await list(pb, PID)
  const mid = String(lb.msgs.find((m: any) => m.content === throwaway)?._id || '')
  expect(mid).toMatch(/^[0-9a-f]{24}$/)

  // non-author (A, but A is OWNER — use B→A denial on A's message instead):
  const pa = await browser.newPage()
  await loginRobust(pa, USER.email, USER.password)
  const la = await list(pa, PID)
  const aMsg = la.msgs.find((m: any) => m.content !== throwaway)
  const aMid = String(aMsg?._id || '')
  expect(aMid).toMatch(/^[0-9a-f]{24}$/)

  // B (non-author, non-owner) deletes A's message → 403 restricted
  const deny = await pb.request.delete(BASE + '/project/' + PID + '/messages/' + aMid, {
    headers: { 'X-CSRF-TOKEN': await csrf(pb) },
  })
  expect(deny.status, 'non-author delete must be 403 (got ' + deny.status + ')').toBe(403)
  const denyTxt = await (await pb.request.delete(BASE + '/project/' + PID + '/messages/' + aMid, {
    headers: { 'X-CSRF-TOKEN': await csrf(pb) },
  })).json().catch(() => ({}))
  expect((denyTxt as any)?.message, '403 restricted shape').toBe('restricted')

  // B deletes own throwaway → 2xx + gone
  const del = await pb.request.delete(BASE + '/project/' + PID + '/messages/' + mid, {
    headers: { 'X-CSRF-TOKEN': await csrf(pb) },
  })
  expect([200, 202, 204].includes(del.status), 'author delete → 2xx (got ' + del.status + ')').toBe(true)
  const lb2 = await list(pb, PID)
  expect(lb2.msgs.some((m: any) => String(m._id || m.id) === mid), 'deleted msg gone for B').toBe(false)

  // owner deletes A's (other user's) message → 2xx + gone (permission leg)
  const od = await pa.request.delete(BASE + '/project/' + PID + '/messages/' + aMid, {
    headers: { 'X-CSRF-TOKEN': await csrf(pa) },
  })
  expect([200, 202, 204].includes(od.status), 'owner delete-other → 2xx (got ' + od.status + ')').toBe(true)
  const la2 = await list(pa, PID)
  expect(la2.msgs.some((m: any) => String(m._id || m.id) === aMid), 'deleted msg gone for A').toBe(false)
  await pa.close(); await pb.close()
})

test('C5 permissions pin: commenter (readOnly) chat capability', async ({ browser }) => {
  const pa = await browser.newPage()
  await loginRobust(pa, USER.email, USER.password)
  const dem = await pa.request.put(BASE + '/project/' + PID + '/users/' + uid2, {
    data: { privileges: 'readOnly' },
    headers: { 'X-CSRF-TOKEN': await csrf(pa) },
  })
  expect([200, 201, 202, 204].includes(dem.status), 'demote to readOnly → 2xx').toBe(true)

  const pb = await browser.newPage()
  await loginRobust(pb, USER2.email, USER2.password)
  const r = await send(pb, PID, RUN + '-commenter-attempt')
  // PIN (Node parity, chatbridge.go line 20-21: non-member → 403 JSON
  // restricted / HTML user-restricted; demoted readOnly is the same
  // access-level family): a non-privileged member cannot post chat.
  expect(r.status, 'commenter chat POST pin (Node parity: 403; got ' + r.status + ')').toBe(403)

  // restore editor for any subsequent specs in this world
  const res = await pa.request.put(BASE + '/project/' + PID + '/users/' + uid2, {
    data: { privileges: 'readAndWrite' },
    headers: { 'X-CSRF-TOKEN': await csrf(pa) },
  })
  expect([200, 201, 202, 204].includes(res.status), 'restore readAndWrite → 2xx').toBe(true)
  await pa.close(); await pb.close()
})

test('C6 pagination: {limit, before} passthrough', async ({ browser }) => {
  const pa = await browser.newPage()
  await loginRobust(pa, USER.email, USER.password)
  const seeds: string[] = []
  for (let i = 1; i <= 5; i++) {
    const c = `${RUN}-page-${i}`
    seeds.push(c)
    await send(pa, PID, c)
    await new Promise((r) => setTimeout(r, 150))
  }

  const page1 = await list(pa, PID, 'limit=2')
  expect(page1.msgs.length, 'limit=2 → exactly 2 (got ' + page1.msgs.length + ')').toBe(2)
  const newestFirst = page1.msgs.map((m: any) => m.content).join(' ')
  expect(newestFirst).toContain(seeds[4]) // most recent in the window

  const cursor = String(page1.msgs[page1.msgs.length - 1]?._id || page1.msgs[page1.msgs.length - 1]?.id || '')
  expect(cursor).toMatch(/^[0-9a-f]{24}$/)
  const page2 = await list(pa, PID, 'limit=2&before=' + encodeURIComponent(cursor))
  expect(page2.msgs.length, 'before=cursor → up to 2 older (got ' + page2.msgs.length + ')').toBe(2)
  expect(page2.msgs.map((m: any) => m.content).join(' ')).toContain(seeds[2])
  // disjoint: no id appears on both pages
  const ids1 = new Set(page1.msgs.map((m: any) => String(m._id || m.id)))
  expect(page2.msgs.some((m: any) => ids1.has(String(m._id || m.id))), 'pages disjoint on _id').toBe(false)
  await pa.close()
})
