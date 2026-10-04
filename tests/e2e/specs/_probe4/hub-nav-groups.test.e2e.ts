/* hub-nav-groups: verify the functionally grouped /hub rail (owner UX 2026-10-06).
 * The rail (not the account dropdown) is the long scrolling list the owner
 * asked to regroup: Workspace / Personal / Administration headings, and the
 * Site-settings General branch split into domain folders.
 */
import { test, expect } from '@playwright/test'
import { ADMIN } from '../../fixtures/credentials'
import { loginRobust } from '../../helpers/auth'

const BASE = 'http://127.0.0.1:7420'

test('hub rail: functional group headings + all destinations reachable', async ({ page }) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
  await page.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(1800)

  // 1. The three functional section headings exist (non-interactive labels).
  const rail = page.locator('nav, [role="navigation"]').last()
  for (const heading of ['Workspace', 'Personal', 'Administration']) {
    const h = page.locator(`text=${heading}`).first()
    await h.waitFor({ state: 'attached', timeout: 15_000 })
    await expect(h).toBeTruthy()
  }

  // 2. All top-level destinations are present under their sections.
  for (const label of ['Projects', 'Templates', 'Reference library', 'My settings', 'Overview & activity', 'Site settings']) {
    await expect(page.getByRole('button', { name: label }).first()).toBeVisible()
  }

  // 3. New domain folders inside Site settings → General.
  await page.getByRole('button', { name: 'Site settings' }).first().click()
  await page.waitForTimeout(400)
  await page.getByRole('button', { name: 'General' }).first().click()
  await page.waitForTimeout(300)
  for (const folder of ['Content & community', 'Diagnostics']) {
    const f = page.getByRole('button', { name: folder }).first()
    await f.waitFor({ state: 'attached', timeout: 10_000 })
    await f.click()
    await page.waitForTimeout(250)
  }
  // Spot-check leaves that survived the regrouping (deep-link ids unchanged).
  await expect(page.getByRole('button', { name: 'Manage templates' }).first()).toBeAttached()
  await expect(page.getByRole('button', { name: 'Instance statistics' }).first()).toBeAttached()
  await expect(page.getByRole('button', { name: 'System messages' }).first()).toBeAttached()

  // 4. Deep links still resolve (stable ids).
  await page.goto(BASE + '/hub#/site/general/users/all', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(1200)
  const main = page.locator('main').last()
  const bodyText = await main.textContent()
  expect(bodyText).toContain('All users')
})
