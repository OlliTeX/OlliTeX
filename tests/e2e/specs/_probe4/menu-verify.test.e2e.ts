/* menu-verify: the grouped /hub account menu (owner UX 2026-10-06). */
import { test, expect } from '@playwright/test'
import { ADMIN } from '../../fixtures/credentials'
import { loginRobust } from '../../helpers/auth'

const BASE = 'http://127.0.0.1:7420'

test('account menu: functional groups + all links', async ({ page }) => {
  await loginRobust(page, ADMIN.email, ADMIN.password)
  await page.goto(BASE + '/hub', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(1600)
  const toggle = page.locator('button[aria-label="Account"]').first()
  await toggle.click()
  const menu = page.locator('.dropdown-menu').filter({ hasText: 'Personal' }).last()
  await menu.waitFor({ state: 'visible', timeout: 10_000 })
  const labels = await menu.locator('.nav-section-label').allTextContents()
  console.log('GROUPS:', JSON.stringify(labels))
  expect(labels).toContain('Workspace')
  expect(labels).toContain('Personal')
  expect(labels.some(l => /manage instance/i.test(l))).toBe(true)
  const btn = menu.locator('button.dropdown-item:has-text("Admin")').first()
  if (await btn.count()) {
    await btn.click()
    await page.waitForTimeout(300)
    const links = await menu.locator('.dropdown-item').allTextContents()
    console.log('LINKS:', JSON.stringify(links))
    const all = links.join(' | ')
    expect(all).toContain('Manage Site')
    expect(all).toContain('Manage Extensions')
    expect(all).toContain('Manage Users')
    expect(all).toContain('Manage template gallery')
  }
  const linksTop = await menu.locator('a.dropdown-item').allTextContents()
  const topAll = linksTop.join(' | ')
  expect(topAll).toContain('Projects')
  expect(topAll).toContain('Account settings')
})
