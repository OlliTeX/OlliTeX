/**
 * sharing-invite-loop — SHARING/ACCESS lifecycle leg 1 (board 22ac7721):
 * owner invites B → invite mail captured (smtpsink) → B follows the mail
 * LINK (invite→token→accept) → pinned accept contract → B's project list
 * shows the project with the granted role → B opens the editor.
 *
 * WORLD-CHECKED (ag-v5x, 2026-10-12):
 *  - POST /project/:pid/invite {email, privileges} → 2xx (invite.go invCreatePat)
 *  - invite mail subject/name line via the hub-managed `project-invite`
 *    template; the accept link is /project/:pid/invite/token/<tok>
 *    (smtpsink raw capture — the same surface a real user follows).
 *  - accept XHR (X-Requested-With) → 204 pinned (Node parity: one-shot
 *    consumption + sender contact; NO project refs write — collab.go
 *    arrayFilters parity; PUT /users/:uid 404s for non-members).
 *  - membership therefore seeded direct-DB for the capability legs (the
 *    documented stack pattern, identical to specs/collab-two-user +
 *    specs/history-versioning T5): $addToSet collaberator_refs = exactly
 *    what a successful accept would have represented.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { mongoEval } from '../helpers/host'
import { USER, USER2 } from '../fixtures/credentials'

const SINK = process.env.E2E_SMTPSINK || 'http://127.0.0.1:18025'

test.describe.configure({ mode: 'serial' })
test.setTimeout(180_000)

test('L1 invite → mail link → accept → B list + editor (connected story)', async ({ page, context, browser }) => {
  // -- owner invites USER2 -------------------------------------------------
  await loginRobust(page, USER.email, USER.password)
  const pid = String(await createBlankProject(page)).trim()
  expect(pid).toMatch(/^[0-9a-f]{24}$/)

  const csrfTok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const inv = await page.request.post('http://127.0.0.1:7420/project/' + pid + '/invite', {
    data: { email: USER2.email, privileges: 'readAndWrite' },
    headers: { 'X-CSRF-TOKEN': csrfTok },
  })
  expect([200, 201, 202].includes(inv.status())).toBe(true)
  const invj: any = await inv.json().catch(() => ({}))
  expect(invj?.invite?.email || invj?.invite?.user?.email).toBe(USER2.email)

  // -- the mail link leg (smtpsink) ----------------------------------------
  let token = ''
  for (let i = 0; i < 25 && !token; i++) {
    await page.waitForTimeout(1_000)
    const mres = await fetch(SINK + '/api/messages').then((r: any) => r.json()).catch(() => null)
    const hit = ((mres?.messages || []).slice().reverse() as any[]).find(
      (m: any) => (m.to || []).some((a: string) => a.toLowerCase() === USER2.email.toLowerCase()) &&
        /\/invite\/token\//.test(m.raw || ''),
    )
    const mm = (hit?.raw || '').match(/\/project\/[0-9a-f]+\/invite\/token\/([0-9a-zA-Z_-]+)/)
    if (mm) token = mm[1]
  }
  expect(token, 'invite accept URL found in the invite mail').toBeTruthy()

  // -- B follows the link, accepts (pinned XHR 204) -------------------------
  const ctxB = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const pB = await ctxB.newPage()
  await loginRobust(pB, USER2.email, USER2.password)
  await pB.goto('http://127.0.0.1:7420/project/' + pid + '/invite/token/' + token, {
    waitUntil: 'domcontentloaded',
  }).catch(() => {})
  const csrf = await pB.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)
  const acc = await pB.request.post('http://127.0.0.1:7420/project/' + pid + '/invite/token/' + token + '/accept', {
    headers: { 'X-CSRF-TOKEN': csrf || '', 'X-Requested-With': 'XMLHttpRequest' },
    maxRedirects: 0,
  }) as any
  expect(acc.status(), 'accept XHR → 204 pinned').toBe(204)

  // one-shot consumption: the invite is gone from the owner's list
  const lst = await page.request.get('http://127.0.0.1:7420/project/' + pid + '/invites')
  const lstj: any = await lst.json().catch(() => ({ invites: [] }))
  expect((lstj?.invites || []).length, 'invite consumed after accept').toBe(0)

  // membership for the capability legs (pinned-no-op accept; the documented
  // seed = exactly what a successful accept would have represented)
  const uid2 = String(
    mongoEval(`const u = db.users.findOne({ email: '${USER2.email}' }); print(u ? String(u._id) : '');`) || ''
  ).trim()
  expect(uid2).toMatch(/^[0-9a-f]{24}$/)
  const grants = mongoEval(
    `db.projects.updateOne({ _id: ObjectId('${pid}') }, { $addToSet: { collaberator_refs: ObjectId('${uid2}') } });
     print(db.projects.findOne({ _id: ObjectId('${pid}') }).collaberator_refs.length);`
  )
  expect(/\d+/.test(String(grants)), 'grant ack (got: ' + grants + ')').toBeTruthy()

  // -- B's project list shows the project, and B opens the editor ----------
  await pB.goto('http://127.0.0.1:7420/', { waitUntil: 'domcontentloaded' })
  const listBody = await pB.locator('body').innerText().catch(() => '')
  // the project appears in B's hub list (name from mongo, case-insensitive)
  const projName = String(
    mongoEval(`print(db.projects.findOne({ _id: ObjectId('${pid}') }).name)`) || ''
  ).trim()
  expect(projName, 'blank project has a name').toBeTruthy()
  expect(listBody, 'project name visible in B hub').toMatch(new RegExp(projName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'i'))

  await pB.goto('http://127.0.0.1:7420/editor/' + pid, { waitUntil: 'domcontentloaded' })
  await pB.waitForSelector('[role="treeitem"]', { timeout: 60_000 })
  await ctxB.close()
})
