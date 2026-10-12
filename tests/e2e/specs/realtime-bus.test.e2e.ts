/**
 * realtime-bus — the Go collab/presence battery (D28a, S4 flip final leg).
 *
 * RETARGETED 2026-10-12 (owner: "Socket.io was retired — now ygo / yjs"):
 * the socket.io 0.9 event bus (services/real-time on :3026) is RETIRED.
 * The live collaboration + presence plane is the Go collab service
 * (`cmd/collab`, ygo engine):
 *
 *   WS   /collab/{projectId}   (browser y-websocket clients)
 *   GET  /presence             (live (room, uid) pairs; internal port,
 *                               Go web does authz via /admin/active-projects)
 *   GET  /healthz
 *
 * The OLD spec probed `:3026/clients` and `/project/:pid/count-connected-clients`
 * — routes of the retired bus. That plane no longer exists in the image
 * (collab service listens on COLLAB_LISTEN, default :3450 — verified in the
 * running world: /healthz -> {"status":"ok","service":"collab","engine":"ygo"}).
 *
 * New live contract (same lifecycle intent, current architecture):
 *
 *   B1 tab A (USER, owner) boots the IDE — joins the ygo room
 *   B2 tab B (USER2, collaborator) boots the IDE — second live client
 *   B3 /presence lists the project room with BOTH users (2 distinct uids)
 *   B4 distinct live uids for this project == 2
 *   B5 close BOTH tabs back-to-back (entries fresh) -> the project leaves
 *       /presence within seconds: last peer leaves -> OnLastPeer -> DropRoom
 *       (deterministic unload path — at that instant the entries would still
 *       have been reporting if they had NOT been dropped, which is exactly
 *       what makes this a proof of the unload lifecycle)
 *   B6 USER re-joins (fresh authorize) and goes idle -> after the 90s TTL the
 *       pair stops reporting while the socket stays open (the registry TTL is
 *       the per-peer drop signal: last-seen is written only at authorize, no
 *       activity touch — documented read-side filter semantics)
 *
 * NOTE on the OLD B5 ("count drops to 1, same user, two tabs"): presence is
 * per (room, user), not per socket (the registry is map[room]map[uid]time,
 * presenceTTL = 90s in go/services/collab/presence.go), and last-seen is only
 * written at authorize time (no activity touch) — so two tabs of ONE user are
 * architecturally indistinguishable, and an entry STOPS REPORTING after 90s
 * idle even with the socket open. The honest two-client assertion is therefore
 * two USERS (the stronger test), and the lifecycle exits are the two paths
 * above: TTL drop per pair + immediate room unload on last peer leaving.
 *
 * Membership bootstrap is direct-DB: in this build the invite-accept path
 * is a pinned Node-parity no-op (see collab-two-user test's
 * grantCollaboratorDirect for the full rationale); access is read from
 * the project doc's collaberator_refs (access.go).
 *
 * Ops endpoints are read from inside the overleaf container (the collab
 * service listens on 127.0.0.1:3450 inside it), like helpers/host.ts.
 */
import { test, expect } from '@playwright/test'
import { execFileSync } from 'child_process'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval, runningContainers } from '../helpers/host'
import { USER, USER2 } from '../fixtures/credentials'

test.setTimeout(300_000)

const PRESENCE_PORT = 3450 // COLLAB_LISTEN default (cmd/collab/main.go)
const PRESENCE_TTL_MS = 95_000 // presenceTTL 90s (presence.go) + margin

function collabServiceCurl(path: string): string {
  const names = runningContainers()
  const ol = names.find((n) => /-e2e-overleaf-1$/.test(n))
  if (!ol) throw new Error('overleaf container not found (expected *-e2e-overleaf-1)')
  return execFileSync('docker', ['exec', '--', ol, 'curl', '-s', '-m', '5', `http://127.0.0.1:${PRESENCE_PORT}` + path], {
    encoding: 'utf8',
    timeout: 30_000,
  }).trim()
}

/** Distinct user ids (mongo _id hex) with a live room for this project. */
function liveUidsForProject(pid: string): string[] {
  const raw = collabServiceCurl('/presence')
  let entries: Array<{ room: string; userId: string }> = []
  try {
    const parsed = JSON.parse(raw)
    entries = Array.isArray(parsed) ? parsed : []
  } catch {
    entries = []
  }
  const uids = new Set<string>()
  for (const e of entries) {
    if (e && e.room && (e.room === pid || e.room.startsWith(pid)) && e.userId) uids.add(e.userId)
  }
  return Array.from(uids)
}

// small local wrapper to keep the mongo plumbing in ONE place per use
function mono(script: string): string {
  const out = (mongoEval(script) || '').trim()
  return out.split('\n').pop() || ''
}

function getUserIdHex(email: string): string {
  // mongosh prints ObjectId(...) unless coerced — String() it (collab-two-user pattern)
  return mono(`var u = db.users.findOne({ email: '${email}' }); u ? String(u._id) : '';`)
}

/** Open the root document — a plain IDE-chrome boot creates NO ygo session yet; the
 *  (room, uid) presence entry is registered in the Authorize hook, which only runs
 *  when a y-websocket client connects for the room (collab.go), and the frontend
 *  opens that connection when a document tab is mounted (024: document-container
 *  ensureEngine → createEngine(projectId, sink)). */
async function openRootDoc(p: any): Promise<void> {
  const item = p.getByRole('treeitem', { name: 'main.tex', exact: false }).first()
  await item.click({ timeout: 20_000 })
  await p.waitForTimeout(2_500)
}

test('collab/presence (Go, ygo engine) — 2-user lifecycle', async ({ page, context, browser }) => {
  // ---- USER (owner) session --------------------------------------------
  await loginRobust(page, USER.email, USER.password)
  const pid = await createBlankProject(page)
  expect(pid).toMatch(/^[0-9a-f]{24}$/)

  // ---- USER2 (collaborator) session — must exist before membership -----
  const ctx2 = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const tab2 = await ctx2.newPage()
  await loginRobust(tab2, USER2.email, USER2.password)

  const uid2 = getUserIdHex(USER2.email)
  expect(uid2).toMatch(/^[0-9a-f]{24}$/)
  const grant = mono(
    `db.projects.updateOne({ _id: ObjectId('${pid}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } });
     print(db.projects.findOne({ _id: ObjectId('${pid}') }).collaberator_refs.length);`
  )
  expect(parseInt(grant, 10) >= 1, `USER2 present in collaberator_refs (got '${grant}')`).toBeTruthy()

  // B1: tab A boots the IDE (ygo room join must resolve for the IDE to render)
  await page.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await page.waitForSelector('[role="treeitem"]', { timeout: 90_000 })
  await openRootDoc(page) // mount document tab → ygo connection → presence Add(pid, USER)

  // B2: tab B boots the IDE as USER2 (second live client in the same room)
  await tab2.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await tab2.waitForSelector('[role="treeitem"]', { timeout: 90_000 })
  await openRootDoc(tab2) // mount document tab → presence Add(pid, USER2)

  // B3+B4: /presence lists the project room with BOTH users (2 distinct uids).
  // Give the (room, uid) registration a beat — connections are admitted
  // through the Authorize hook (presence.go).
  let live = liveUidsForProject(pid)
  const deadline = Date.now() + 15_000
  while (live.length < 2 && Date.now() < deadline) {
    await page.waitForTimeout(500)
    live = liveUidsForProject(pid)
  }
  expect(live.length, () => `live uids for ${pid} = ${JSON.stringify(live)} (uid2=${uid2}; raw /presence=${collabServiceCurl('/presence').slice(0, 300)})`).toBeGreaterThanOrEqual(2)
  expect(live).toContain(uid2)
  const count2 = live.length
  expect(count2).toBe(2)

  // B5: unload path (deterministic). Both entries are fresh (joined moments
  // ago) — close both tabs back-to-back: USER2's close leaves USER as last
  // peer, USER's close leaves the room -> OnLastPeer -> DropRoom(pid). If the
  // drop had NOT happened, both entries would still report for their remaining
  // TTL window — so absence within seconds is the proof.
  await tab2.close()
  await ctx2.close()
  await page.close()
  const b5By = Date.now() + 15_000
  let afterUnload = liveUidsForProject(pid)
  while (afterUnload.length > 0 && Date.now() < b5By) {
    await page.waitForTimeout(500)
    afterUnload = liveUidsForProject(pid)
  }
  expect(afterUnload, `project ${pid} dropped after last peer left (DropRoom)`).toEqual([])

  // B6: TTL path (documented read-side filter). Fresh authorize (reopen the
  // document → new ygo session → Add(pid, USER)), then idle: the pair stops
  // reporting after presenceTTL (90s) while the socket stays open.
  const ctxA = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const tabA = await ctxA.newPage()
  await loginRobust(tabA, USER.email, USER.password)
  await tabA.goto('/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await tabA.waitForSelector('[role="treeitem"]', { timeout: 90_000 })
  await openRootDoc(tabA)

  const uid1 = getUserIdHex(USER.email)
  expect(uid1).toMatch(/^[0-9a-f]{24}$/)
  let fresh = liveUidsForProject(pid)
  const freshBy = Date.now() + 15_000
  while (!fresh.includes(uid1) && Date.now() < freshBy) {
    await tabA.waitForTimeout(500)
    fresh = liveUidsForProject(pid)
  }
  expect(fresh, 'fresh authorize registered (room, USER)').toContain(uid1)

  const staleBy = Date.now() + PRESENCE_TTL_MS + 40_000
  let stale = liveUidsForProject(pid)
  while (stale.includes(uid1) && Date.now() < staleBy) {
    await tabA.waitForTimeout(2_000)
    stale = liveUidsForProject(pid)
  }
  expect(stale, 'idle pair stops reporting after presenceTTL (socket still open)').not.toContain(uid1)

  await tabA.close()
  await ctxA.close()
})
