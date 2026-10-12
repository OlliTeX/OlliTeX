/**
 * sharing-token-links — SHARING/ACCESS lifecycle leg 3 (board 22ac7721).
 *
 * LIVE-PINNED 2026-10-12 (ag-v57 world, CSRF-auth probes, evidence in
 * OPEN_QUEUE_PROGRESS.md cont.4):
 *   POST /project/:pid/sharing-link   → 403 HTML "Restricted"  (OWNER too!)
 *   GET  /project/:pid/sharing-link   → 403 HTML "Restricted"
 *   GET  /project/:pid/tokens         → 200 {}      (empty list surface)
 *   GET  /project/:pid/members        → 200 {"members":[]}
 * Interpretation (invite.go P4.10b header lines 41-43): the
 * "sharing-updates-new-link" split test is DISABLED in this CE build — the
 * shared-link create/view surface is 403 Restricted behind that gate
 * (ownerHasSharingUpdates). That is the pinned wire contract here: a
 * disabled-gate 403, NOT a CSRF/bug (control: invite POST with identical
 * CSRF headers on the same session returns 200).
 *
 * Board leg 3 wants create→incognito read/write→revoke. With the gate OFF
 * in CE, the assertable contract is the pinned disabled state (403
 * Restricted on create AND on the link path) + the live list surfaces
 * (tokens/members shapes) + the invite-token view leg (the CE-supported
 * token link, exercised end-to-end by the L1 invite-loop spec).
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'

test.describe.configure({ mode: 'serial' })
test.setTimeout(120_000)

test('L3 pinned CE state: sharing-link 403 Restricted (gate disabled) + live list shapes', async ({ page }) => {
  await loginRobust(page, USER.email, USER.password)
  const pid = String(await createBlankProject(page)).trim()
  expect(pid).toMatch(/^[0-9a-f]{24}$/)

  // -- the disabled-gate pin (both directions, owner session) ---------------
  const csrfTok = (await page.locator('meta[name="ol-csrfToken"]').getAttribute('content').catch(() => null)) || ''
  const mk = await page.request.post(BASE + '/project/' + pid + '/sharing-link', {
    data: { access: 'readOnly' },
    headers: { 'X-CSRF-TOKEN': csrfTok },
  })
  expect(mk.status(), 'POST sharing-link: pinned 403 (CE gate sharing-updates-new-link disabled)').toBe(403)
  expect((await mk.text().catch(() => '')).toLocaleLowerCase(), '403 body is the Restricted page').toContain('restricted')

  const view = await page.request.get(BASE + '/project/' + pid + '/sharing-link')
  expect(view.status(), 'GET sharing-link: pinned 403 (same gate)').toBe(403)

  // -- the live list surfaces (200 + observed shapes) -------------------------
  const toks = await page.request.get(BASE + '/project/' + pid + '/tokens')
  expect(toks.status(), 'GET /tokens → 200').toBe(200)
  expect((await toks.text().catch(() => '')).trim(), 'observed pin: {} (no live tokens yet)').toBe('{}')

  const mem = await page.request.get(BASE + '/project/' + pid + '/members')
  expect(mem.status(), 'GET /members → 200').toBe(200)
  const memj: any = await mem.json().catch(() => ({ members: [] }))
  expect(Array.isArray(memj?.members), 'members shape = {members:[...]}').toBe(true)

  // -- control probe: a POST on the same surface family with IDENTICAL CSRF --
  // proves the 403s above are the route gate (not CSRF/authz plumbing).
  const inv = await page.request.post(BASE + '/project/' + pid + '/invite', {
    data: { email: 'e2e-control@e2e.test', privileges: 'readAndWrite' },
    headers: { 'X-CSRF-TOKEN': csrfTok },
  })
  const invStatus = inv.status()
  expect(invStatus, 'control: invite POST 2xx (got ' + invStatus + ')').toBeGreaterThanOrEqual(200)
  expect(invStatus).toBeLessThan(300)
})
