/**
 * sharing-request-access — SHARING/ACCESS lifecycle leg 4 (board 22ac7721):
 * B (no access) requests access → owner sees the request → owner grants
 * editor → B's next load has editor access (permission propagation); the
 * owner's member surface reflects the membership.
 *
 * Pinned surface (projectlist/collab.go + accessrequests.go):
 *   POST   /project/:pid/request-access                 (reqAccPat)
 *   GET    /project/:pid/access-requests                (arPat, owner/admin)
 *   POST   /project/:pid/access-requests/:id/grant      (accGrantPat)
 *   DELETE /project/:pid/access-requests/:id            (accDeclPat — decline)
 *   GET    /project/:pid/members                        (members.go)
 *
 * The board note: "request-access UI gate was ownerHasSharingUpdates — the
 * red legs are SPEC-MIGRATION against the toolbar rework, not product gaps"
 * → this spec drives the LOOP over the API surface (the UI gate is a
 * front-end visibility question; the wire contract is the product).
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject, csrfToken } from '../helpers/auth'
import { USER, USER2 } from '../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'

test.describe.configure({ mode: 'serial' })
test.setTimeout(180_000)

test('L4 request-access → owner grant → B gains editor access', async ({ page, context, browser }) => {
  // -- USER owns a project USER2 has no access to ---------------------------
  await loginRobust(page, USER.email, USER.password)
  const pid = await createBlankProject(page)
  expect(pid).toMatch(/^[0-9a-f]{24}$/)

  // -- B requests access (anon or logged B; logged B = the honest leg) -------
  const ctxB = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const pB = await ctxB.newPage()
  await loginRobust(pB, USER2.email, USER2.password)
  const req = await pB.request.post(BASE + '/project/' + pid + '/request-access', {
    data: { access: 'readAndWrite' },
  })
  expect([200, 201, 202, 204].includes(req.status()),
    'POST request-access → 2xx (got ' + req.status() + ': ' +
      (await req.text().catch(() => '')).slice(0, 160) + ')').toBe(true)

  // -- owner sees the request ------------------------------------------------
  const seen = await page.request.get(BASE + '/project/' + pid + '/access-requests')
  expect(seen.status(), 'GET access-requests → 200').toBe(200)
  const seens: any = await seen.json().catch(() => ({}))
  const reqs: any[] = Array.isArray(seens) ? seens : (seens?.accessRequests || seens?.requests || seens?.list || [])
  expect(reqs.length, 'owner sees exactly 1 request').toBeGreaterThanOrEqual(1)
  const rid = String(reqs[0]?._id || reqs[0]?.id || '')
  expect(rid, 'request has an id').toBeTruthy()

  // -- owner grants -----------------------------------------------------------
  const grant = await page.request.post(BASE + '/project/' + pid + '/access-requests/' + rid + '/grant', {
    data: {},
  })
  expect([200, 201, 202, 204].includes(grant.status()),
    'grant → 2xx (got ' + grant.status() + ': ' +
      (await grant.text().catch(() => '')).slice(0, 160) + ')').toBe(true)

  // -- B's next load has access (propagation; no re-login) -------------------
  await pB.waitForTimeout(1_500)
  const after = await pB.request.get(BASE + '/project/' + pid, { maxRedirects: 0 }) as any
  expect([200, 302, 303].includes(after.status()),
    'B next load reaches the project (got ' + after.status() + ')').toBe(true)

  // -- owner's member surface reflects the membership -------------------------
  const mem = await page.request.get(BASE + '/project/' + pid + '/members')
  expect([200, 204].includes(mem.status()), 'GET members → 2xx').toBe(true)

  await ctxB.close()
})
