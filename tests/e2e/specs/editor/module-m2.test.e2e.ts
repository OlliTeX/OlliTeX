/**
 * M2 module Mantine wave — zotero/mendeley import surfaces (reachability +
 * route parity + no regressions).
 *
 * The zotero/mendeley module buttons (OAuth/confirm/cancel inside their
 * modals) are converted via the shared surface helper on this branch; the
 * live states reachable with fixture data are: file tree → Add files →
 * "From Zotero" / "From Mendeley". Both routes must reach the same state,
 * the modal frame follows the route (Mantine on /editor — proven by the
 * file-tree create modal — legacy on /Project), and neither route may crash
 * or leak Mantine buttons into the legacy route.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../../helpers/auth'

const B = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

async function openImportState(page: any, provider: string) {
  const plus = page.locator('.file-tree-toolbar-action-button').first()
  await plus.scrollIntoViewIfNeeded().catch(() => {})
  await plus.click({ timeout: 10_000 })
  await page.waitForTimeout(1200)
  const item = page
    .locator('li, [role="option"], .menu-item, button, a')
    .filter({ hasText: new RegExp('from ' + provider, 'i') })
    .first()
  await item.click({ timeout: 10_000 })
  await page.waitForTimeout(3500)
  const frame = page
    .locator('.mantine-Modal-content, .modal-content')
    .filter({ hasText: new RegExp(provider, 'i') })
    .first()
  await expect(frame, provider + ' import state must be reachable').toBeVisible({ timeout: 20_000 })
}

test.describe('M2 zotero/mendeley import surfaces', () => {
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

  async function check(route: string, isMantine: boolean) {
    await page.goto(B + route + '/' + pid, { waitUntil: 'load' })
    await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
    await page.waitForTimeout(2200)

    // zotero state
    const frameZ = page
      .locator('.mantine-Modal-content, .modal-content')
      .filter({ hasText: /zotero/i })
      .first()
    await openImportState(page, 'zotero')
    expect(await frameZ.evaluate(el => /mantine-Modal-content/.test((el as HTMLElement).className)))
      .toBe(isMantine)
    await page.keyboard.press('Escape')
    await page.waitForTimeout(900)

    // mendeley state
    const frameM = page
      .locator('.mantine-Modal-content, .modal-content')
      .filter({ hasText: /mendeley/i })
      .first()
    await openImportState(page, 'mendeley')
    expect(await frameM.evaluate(el => /mantine-Modal-content/.test((el as HTMLElement).className))).toBe(isMantine)
    await page.keyboard.press('Escape')
    await page.waitForTimeout(700)

    // the legacy route must not contain Mantine buttons at all
    if (!isMantine) {
      const mantineButtons: number = await page.locator('.mantine-Button-root').count()
      expect(mantineButtons, 'no Mantine Buttons may render on /Project').toBe(0)
    }

    expect(pageErrors, 'no pageerrors across both module states: ' + pageErrors.join(' | ')).toEqual([])
  }

  test('editor route: module import states on the Mantine surface', async () => {
    await check('/editor', true)
  })

  test('project route: module import states on the legacy surface', async () => {
    await check('/Project', false)
  })
})
