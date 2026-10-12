/**
 * sharing-remove-impact — SHARING/ACCESS lifecycle leg 5 (board 22ac7721):
 * owner removes B → B's project VANISHES from B's list AND B's next project
 * read is REJECTED (negative wire). The "impact" legs are the new part over
 * the modals-p4 pin (which covers invite+remove at the modal level).
 *
 * Pinned surface (projectlist/collab.go):
 *   DELETE /project/:pid/users/:uid   (userPat — the remove leg, modals-p4)
 * Access semantics (access.go): non-member (access NONE) → 403 Forbidden on
 * the project surface / 404 general on ghost projects; the project list
 * projection only returns projects of members/owner.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { USER, USER2 } from '../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'

test.describe.configure({ mode: 'serial' })
test.setTimeout(180_000)

test('L5 remove impact: B list clears + next read rejected', async ({ page, context, browser }) => {
  // -- setup: USER2 is a member (the documented seed pattern) ----------------
  await loginRobust(page, USER.email, USER.password)
  const pid = await createBlankProject(page)
  const uid2 = String(
    mongoEval(`const u = db.users.findOne({ email: '${USER2.email}' }); print(u ? String(u._id) : '');`) || ''
  )
  expect(uid2).toMatch(/^[0-9a-f]{24}$/)
  expect(
    mongoEval(`db.projects.updateOne({ _id: ObjectId('${pid}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } });
              print(db.projects.findOne({ _id: ObjectId('${pid}') }).collaberator_refs.length);`)
  ).toMatch(/\d+/)

  // B currently sees the project in its list
  const ctxB = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const pB = await ctxB.newPage()
  await loginRobust(pB, USER2.email, USER2.password)
  await pB.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
  const projName = String(mongoEval(`print(db.projects.findOne({ _id: ObjectId('${pid}') }).name)`) || '').trim()
  const before = await pB.locator('body').innerText().catch(() => '')
  expect(before, 'B list shows the project BEFORE removal').toMatch(new RegExp(projName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'i'))

  // B's read currently succeeds (member)
  const readBefore = await pB.request.get(BASE + '/project/' + pid, { maxRedirects: 0 }) as any
  expect([200, 302, 303].includes(readBefore.status()), 'B read before removal OK').toBe(true)

  // -- owner removes B --------------------------------------------------------
  const rm = await page.request.delete(BASE + '/project/' + pid + '/users/' + uid2, { maxRedirects: 0 }) as any
  expect([200, 202, 204].includes(rm.status()),
    'DELETE member → 2xx (got ' + rm.status() + ')').toBe(true)

  // -- impact leg 1: B's list no longer shows the project ---------------------
  await pB.reload({ waitUntil: 'domcontentloaded' })
  const after = await pB.locator('body').innerText().catch(() => '')
  expect(after, 'B list no longer shows the project after removal')
    .not.toMatch(new RegExp(projName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'i'))

  // -- impact leg 2: B's next read is rejected (non-member) -------------------
  const readAfter = await pB.request.get(BASE + '/project/' + pid, { maxRedirects: 0 }) as any
  expect([401, 403, 404].includes(readAfter.status()),
    'B read after removal rejected (got ' + readAfter.status() + ')').toBe(true)

  await ctxB.close()
})
