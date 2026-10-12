/**
 * sharing-remove-impact — SHARING/ACCESS lifecycle leg 5 (board 22ac7721).
 *
 * LIVE-PINNED 2026-10-12 (ag-v57 world, evidence in OPEN_QUEUE_PROGRESS.md):
 *   B member (legacy collaberator_refs) GET /editor/:pid  → 200   (member)
 *   owner   DELETE /project/:pid/users/:uid               → 204   (idempotent)
 *   B after  GET /project/:pid                            → 404   (retired route)
 *   B after  GET /editor/:pid                             → 403   (PINNED denial)
 *   B hub list: project name ABSENT after removal (the L1-proven positive:
 *   B hub list SHOWS the project name while member — invite-loop spec)
 *
 * The "impact" legs (new over modals-p4): list-vanish + read-denial after
 * the owner's remove — pinned to the observed 403 on the editor surface.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { USER, USER2 } from '../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'

test.describe.configure({ mode: 'serial' })
test.setTimeout(180_000)

test('L5 remove impact: B editor-read 200 → owner remove 204 → B editor-read 403 + list vanishes', async ({ page, context, browser }) => {
  // -- setup: USER2 is a legacy-ref member -----------------------------------
  await loginRobust(page, USER.email, USER.password)
  // UNIQUE name: the default blank-project name is shared across accounts,
  // so a name match on B's hub list would false-positive on B's OWN projects.
  const run = 'rmimp-' + Date.now()
  const csrf0 = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const mk = await page.request.post(BASE + '/project/new', { data: { projectName: run }, headers: { 'X-CSRF-TOKEN': csrf0 } })
  expect(mk.status(), 'create named project → 2xx (got ' + mk.status() + ')').toBeGreaterThanOrEqual(200)
  const pid = ((await mk.text()).match(/"[0-9a-f]{24}"/) || [''])[0].replace(/"/g, '')
  expect(pid, 'pid extracted').toMatch(/^[0-9a-f]{24}$/)
  const projName = run
  const csrf = csrf0
  const H = { 'X-CSRF-TOKEN': csrf }

  const uid2 = String(mongoEval(`const u = db.users.findOne({ email: '${USER2.email}' }); print(u ? String(u._id) : '');`) || '').trim()
  expect(uid2).toMatch(/^[0-9a-f]{24}$/)
  const grants = String(mongoEval(`db.projects.updateOne({ _id: ObjectId('${pid}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } });
    print(db.projects.findOne({ _id: ObjectId('${pid}') }).collaberator_refs.length);`) || '')
  expect(/\d+/.test(grants), 'seed membership ack').toBeTruthy()

  // B sees the project in its hub list (positive leg; L1-proven pattern)
  expect(projName, 'project has a name').toBeTruthy()
  const nameRe = new RegExp(projName.replace(/[.*+?^${}()|\[\]\\]/g, '\\$&'), 'i')

  const ctxB = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const pB = await ctxB.newPage()
  await loginRobust(pB, USER2.email, USER2.password)
  await pB.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
  const before = await pB.locator('body').innerText().catch(() => '')
  expect(before, 'B hub list shows the project BEFORE removal').toMatch(nameRe)

  // B's editor read works while member (PINNED positive)
  const readBefore = await pB.request.get(BASE + '/editor/' + pid, { maxRedirects: 0 }) as any
  expect([200, 302, 303].includes(readBefore.status()), 'B editor read before removal OK (got ' + readBefore.status() + ')').toBe(true)

  // -- owner removes B ---------------------------------------------------------
  const rm = await page.request.delete(BASE + '/project/' + pid + '/users/' + uid2, { headers: H }) as any
  expect([200, 202, 204].includes(rm.status()), 'DELETE member → 2xx (got ' + rm.status() + ')').toBe(true)

  // -- impact leg 1: B's hub list no longer shows the project ------------------
  await pB.reload({ waitUntil: 'domcontentloaded' })
  const after = await pB.locator('body').innerText().catch(() => '')
  expect(after, 'B hub list no longer shows the project after removal').not.toMatch(nameRe)

  // -- impact leg 2: B's editor read is REJECTED (PINNED 403) ------------------
  const readAfter = await pB.request.get(BASE + '/editor/' + pid, { maxRedirects: 0 }) as any
  expect(readAfter.status(), 'B editor read after removal → PINNED 403 (got ' + readAfter.status() + ')').toBe(403)

  await ctxB.close()
})
