/**
 * sync-graceful — WebDAV + Dropbox (one journey): the status endpoints must
 * answer 2xx/4xx JSON with no linked credential — NEVER 5xx (the R11
 * webdav-500 regression class, pinned here for both providers); the user
 * settings surfaces render the sync sections. (5xx gets a warmup retry —
 * the test stack's services need a moment after container start.)
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../helpers/auth'
import { USER } from '../fixtures/credentials'

async function expectNot5xx(res: import('playwright').APIResponse): Promise<number> {
  let status = res.status()
  for (let i = 0; i < 2 && status >= 500; i++) {
    await new Promise((r) => setTimeout(r, 4_000))
    const r2 = await res.request().context.get(res.url())
    status = r2.status()
  }
  return status
}

test('webdav + dropbox status graceful (no 5xx) + settings render', async ({ page, context }) => {
  await loginRobust(page, USER.email, USER.password)
  await page.waitForTimeout(500)

  const wd = await context.request.get('/user/webdav/status')
  expect(
    await expectNot5xx(wd),
    `webdav status must not 5xx (R11 regression): ${(await wd.text().catch(() => '')).slice(0, 200)}`
  ).toBeLessThan(500)
  if (wd.status < 500) {
    const wdj = await wd.json().catch(() => ({}))
    expect(wdj.connected === false || wdj.error !== undefined).toBe(true)
  }

  const dbx = await context.request.get('/user/dropbox/status')
  expect(
    await expectNot5xx(dbx),
    `dropbox status must not 5xx: ${(await dbx.text().catch(() => '')).slice(0, 200)}`
  ).toBeLessThan(500)

  // 2026-09-12: the legacy /user/mysettings page was REMOVED (301 → hub), but
  // the user-level WebDAV/Dropbox "Project synchronisation" card still lives on
  // the legacy /user/settings page (settings/components/linking-section). That
  // is the surface this render check asserts (git-bridge + notifications both
  // deep-link to /user/settings#project-sync). The R11 no-5xx guard above is
  // the meaningful regression pin; this only pins that the card still renders.
  const res = await page.goto('/user/settings')
  expect(res?.status() ?? 200).toBe(200)
  await page.waitForTimeout(1_500)
  const body = await page.locator('body').innerText()
  // sync surfaces live under the "Project synchronisation" card (WebDAV / Dropbox rows)
  expect(body).toMatch(/Project synchronisation/i)
  expect(body).toMatch(/WebDAV/i)
})
