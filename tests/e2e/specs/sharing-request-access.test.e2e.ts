/**
 * sharing-request-access — SHARING/ACCESS lifecycle leg 4 (board 22ac7721).
 *
 * LIVE-PINNED 2026-10-12 (ag-v57 world, CSRF-auth probes):
 *   POST /project/:pid/request-access     → 403 HTML "Restricted" (logged-in B)
 *   GET  /project/:pid/access-requests    → 200 {"editAccessRequests":[]}
 * Board note (2026-10-11): "request-access UI gate was ownerHasSharingUpdates
 * — the red legs are SPEC-MIGRATION against the toolbar rework, not product
 * gaps". The wire pin in this CE build is exactly that: the request-access
 * loop sits BEHIND the (disabled for CE) sharing-updates gate → 403
 * Restricted, while the owner's request-list surface is live (200, shape
 * {"editAccessRequests": [...] }).
 *
 * Assertable contract in this build:
 *   R1 B (no access, logged-in) POST request-access → 403 Restricted (pinned)
 *   R2 owner GET access-requests → 200 {"editAccessRequests": []} (list surface)
 *   R3 a NON-member's project read is still denied (the denial the request
 *      access loop would otherwise satisfy — the security invariant holds
 *      even with the loop gated off)
 * (If the owner enables the sharing-updates gate, this spec body is the
 * template to flip: R1 → 2xx, then grant/decline via /access-requests/:id/{grant}
 * and the B-access-propagation legs.)
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { USER, USER2 } from '../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'

test.describe.configure({ mode: 'serial' })
test.setTimeout(120_000)

test('L4 pinned state: request-access 403 (CE gate) + owner list surface + denial invariant', async ({ page, context, browser }) => {
  // -- USER owns a project USER2 has no access to ----------------------------
  await loginRobust(page, USER.email, USER.password)
  const pid = String(await createBlankProject(page)).trim()
  expect(pid).toMatch(/^[0-9a-f]{24}$/)

  // -- R1: B requests access → pinned 403 Restricted (CE gate off) ----------
  const ctxB = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const pB = await ctxB.newPage()
  await loginRobust(pB, USER2.email, USER2.password)
  const csrfB = (await pB.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const req = await pB.request.post(BASE + '/project/' + pid + '/request-access', {
    data: { access: 'readAndWrite' },
    headers: { 'X-CSRF-TOKEN': csrfB },
  })
  expect(req.status(), 'R1: request-access pinned 403 in this CE build (got ' + req.status() + ')').toBe(403)
  expect((await req.text().catch(() => '')).toLocaleLowerCase(), '403 body = Restricted page').toContain('restricted')

  // -- R2: owner's request-list surface is live ------------------------------
  const lst = await page.request.get(BASE + '/project/' + pid + '/access-requests')
  expect(lst.status(), 'R2: owner GET access-requests → 200').toBe(200)
  const lstj: any = await lst.json().catch(() => null)
  expect(Array.isArray(lstj?.editAccessRequests), 'R2: shape {"editAccessRequests":[...]} (got ' + JSON.stringify(lstj).slice(0, 80) + ')').toBe(true)

  // -- R3: the denial invariant (no access ⇒ no project surface) ------------
  const deny = await pB.request.get(BASE + '/project/' + pid, { maxRedirects: 0 }) as any
  expect([401, 403, 404].includes(deny.status()),
    'R3: non-member project read denied (got ' + deny.status() + ')').toBe(true)

  await ctxB.close()
})
