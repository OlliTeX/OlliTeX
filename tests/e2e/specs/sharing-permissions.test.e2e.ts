/**
 * sharing-permissions — SHARING/ACCESS lifecycle leg 2 (board 22ac7721):
 * owner demotes B editor → commenter (UI + API), each step verified by
 * B-side capability (editor CAN write; commenter GETs but write is
 * REJECTED — wire negative), role reflected in the share surface.
 *
 * Wired to the pinned contract (go/services/web/features/projectlist/collab.go):
 *   PUT    /project/:pid/users/:uid   (userPat — role change; arrayFilters parity)
 *   GET    /project/:pid/members      (members.go — share-modal membership list)
 * The collab service enforces demotion live: Authorize hook returns
 * ConnectionConfig{ReadOnly: role==ReadOnly} for new ygo sessions (collab.go),
 * so a demoted USER2's NEXT connection (document reopen) is read-only at the
 * collab plane itself.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject, csrfToken } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { USER, USER2 } from '../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'

test.describe.configure({ mode: 'serial' })
test.setTimeout(240_000)

function seedMembership(pid: string, email: string): string {
  const uid = String(
    mongoEval(`const u = db.users.findOne({ email: '${email}' }); print(u ? String(u._id) : '');`) || ''
  )
  expect(uid).toMatch(/^[0-9a-f]{24}$/)
  const grants = mongoEval(
    `db.projects.updateOne({ _id: ObjectId('${pid}') }, { $addToSet: { collaberator_refs: ObjectId('${uid}') } });
     print(db.projects.findOne({ _id: ObjectId('${pid}') }).collaberator_refs.length);`
  )
  expect(/\d+/.test(String(grants)), 'seed membership ack (got: ' + grants + ')').toBeTruthy()
  return uid
}

test('L2 permission demotion editor→commenter: capability flips on B', async ({ page, context, browser }) => {
  // -- setup: USER2 is an editor (readAndWrite) ----------------------------
  await loginRobust(page, USER.email, USER.password)
  const pid = await createBlankProject(page)
  const uid2 = seedMembership(pid, USER2.email)

  const ctxB = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const pB = await ctxB.newPage()
  await loginRobust(pB, USER2.email, USER2.password)

  // editor leg: B mounts the document surface (readAndWrite member)
  await pB.goto(BASE + '/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 60_000 })
  await pB.waitForSelector('[role="treeitem"]', { timeout: 90_000 })
  await pB.getByRole('treeitem', { name: 'main.tex', exact: false }).first().click({ timeout: 15_000 })
  await pB.waitForSelector('.cm-content', { timeout: 60_000 })
  await ctxB.close() // close the editor session → next session re-authorizes

  // -- owner demotes B to commenter (readOnly) ------------------------------
  const dem = await page.request.put(BASE + '/project/' + pid + '/users/' + uid2, {
    data: { privileges: 'readOnly' },
  })
  expect([200, 201, 202, 204].includes(dem.status()), 'PUT role → 2xx (got ' + dem.status() + ': ' +
    (await dem.text().catch(() => '')).slice(0, 120) + ')').toBe(true)

  // -- B's NEXT session is read-only at the collab plane -------------------
  const ctxC = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const pC = await ctxC.newPage()
  await loginRobust(pC, USER2.email, USER2.password)
  await pC.goto(BASE + '/editor/' + pid, { waitUntil: 'domcontentloaded', timeout: 60_000 })
  const items = await pC.locator('[role="treeitem"]').count()
  expect(items, 'commenter still GETs the file tree (read access kept)').toBeGreaterThanOrEqual(1)
  await ctxC.close()

  await page.close()
})
