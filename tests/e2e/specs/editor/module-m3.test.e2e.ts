/**
 * M3 module Mantine wave — dropbox + git-sync (github-sync) surfaces.
 *
 * Drop the geometry-dependent viewport click (use a DOM click) and assert
 * the REAL fixture states:
 *   dropbox (account not linked):  modal frame follows the route + the
 *                                  "link your account" affordance visible
 *   git-sync (check failing here): modal frame follows the route + the
 *                                  Unlink/Cancel actions visible as the
 *                                  route's button surface
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../../helpers/auth'

const B = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

async function openCard(page: any, re: RegExp) {
  const rail = page
    .locator("[data-rr-ui-event-key='integrations'], [aria-label^='Integrations']")
    .first()
  await rail.click({ timeout: 8000 }).catch(() => {})
  await page.waitForTimeout(2000)
  const btn = page
    .locator('button.integrations-panel-card-button')
    .filter({ hasText: re })
    .first()
  await btn.evaluate((el: any) => el.scrollIntoView({ block: 'center' })).catch(() => {})
  await page.waitForTimeout(600)
  // DOM-level click: immune to panel-resize race / off-viewport cards
  await btn.dispatchEvent('click')
  await page.waitForTimeout(3200)
}

test.describe('M3 dropbox + git-sync surfaces', () => {
  let c: any
  let page: any
  let pid: string
  let pageErrors: string[] = []

  test.beforeAll(async ({ browser }) => {
    c = await browser.newContext({ viewport: { width: 1500, height: 950 } })
    page = await c.newPage()
    page.on('pageerror', (e: Error) => pageErrors.push(String(e)))
    await loginRobust(page, 'e2e-admin@e2e.test', 'Ol-Fixture-9x7K')
    pid = await createBlankProject(page)
  })

  test.afterAll(async () => {
    await page.request.delete('/project/' + pid).catch(() => {})
    await c.close().catch(() => {})
  })

  test('editor route: dropbox + git-sync on the Mantine surface', async () => {
    await page.goto(B + '/editor/' + pid, { waitUntil: 'load' })
    await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
    await page.waitForTimeout(2200)

    // dropbox (not linked)
    await openCard(page, /dropbox/i)
    let frame = page
      .locator('.mantine-Modal-content')
      .filter({ hasText: /dropbox/i })
      .first()
    await expect(frame, 'the /editor dropbox modal must be the Mantine frame').toBeVisible({ timeout: 25_000 })
    await expect(page.getByText(/link your account/i).first(), 'dropbox connect affordance').toBeVisible({ timeout: 10_000 })
    await page.keyboard.press('Escape')
    await page.waitForTimeout(900)

    // git-sync
    await openCard(page, /git provider/i)
    frame = page
      .locator('.mantine-Modal-content')
      .filter({ hasText: /git/i })
      .first()
    await expect(frame, 'the /editor git-sync modal must be the Mantine frame').toBeVisible({ timeout: 25_000 })
    const cancel = page.locator('.mantine-Button-root').filter({ hasText: /cancel|unlink/i }).first()
    await expect(cancel, 'the git-sync Cancel/Unlink must be a Mantine Button on /editor').toBeVisible({ timeout: 20_000 })
    await page.keyboard.press('Escape')
    await page.waitForTimeout(700)

    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ')).toEqual([])
  })

  test('project route: dropbox + git-sync stay legacy', async () => {
    await page.goto(B + '/Project/' + pid, { waitUntil: 'load' })
    await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
    await page.waitForTimeout(2200)

    // dropbox (not linked)
    await openCard(page, /dropbox/i)
    let frame = page
      .locator('.modal-content')
      .filter({ hasText: /dropbox/i })
      .first()
    await expect(frame, 'the /Project dropbox modal must be the legacy RB frame').toBeVisible({ timeout: 25_000 })
    await expect(page.getByText(/link your account/i).first(), 'dropbox connect affordance').toBeVisible({ timeout: 10_000 })
    await page.keyboard.press('Escape')
    await page.waitForTimeout(900)

    // git-sync
    await openCard(page, /git provider/i)
    frame = page
      .locator('.modal-content')
      .filter({ hasText: /git/i })
      .first()
    await expect(frame, 'the /Project git-sync modal must be the legacy RB frame').toBeVisible({ timeout: 25_000 })
    const cancel = page.locator('.modal .btn').filter({ hasText: /cancel|unlink/i }).first()
    await expect(cancel, 'the git-sync Cancel/Unlink must be a legacy OLButton on /Project').toBeVisible({ timeout: 20_000 })
    const mantineBtns = await page.locator('.modal .mantine-Button-root').count()
    expect(mantineBtns, 'no Mantine Buttons inside the /Project modal').toBe(0)
    await page.keyboard.press('Escape')
    await page.waitForTimeout(700)

    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ')).toEqual([])
  })
})
