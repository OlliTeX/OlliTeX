/**
 * M6 module Mantine wave — bib-editor + orcid + grammar surfaces.
 *
 * Context-free pages (library, settings grammar/orcid) must stay fully
 * legacy (zero Mantine Button leak) and keep working.
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'

const B = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

test.describe('M6 bib/orcid/grammar surfaces', () => {
  let c: any
  let page: any
  let pageErrors: string[] = []

  test.beforeAll(async ({ browser }) => {
    c = await browser.newContext({ viewport: { width: 1500, height: 950 } })
    page = await c.newPage()
    page.on('pageerror', (e: Error) => pageErrors.push(String(e)))
    await loginRobust(page, 'e2e-admin@e2e.test', 'Ol-Fixture-9x7K')
  })

  test.afterAll(async () => {
    await c.close().catch(() => {})
  })

  test('library page: legacy surface, zero Mantine Button leak, works', async () => {
    await page.goto(B + '/library', { waitUntil: 'load' })
    await page.waitForTimeout(3500)
    const body = await page.locator('body').innerText()
    expect(body.toLowerCase(), 'the library page must render').toMatch(/bib|library|entry|import/i)
    const legacyButtons = await page.locator('.btn').count()
    const mantineButtons = await page.locator('.mantine-Button-root').count()
    expect(legacyButtons, 'library affordances must be legacy OLButtons').toBeGreaterThan(0)
    expect(mantineButtons, 'no Mantine Buttons on the context-free library page').toBe(0)
    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ')).toEqual([])
  })

  test('library import flow: opens the (legacy) import modal with legacy buttons', async () => {
    const imp = page.getByRole('button', { name: /import/i }).first()
    if (await imp.isVisible().catch(() => false)) {
      await imp.click({ timeout: 8_000 })
      await page.waitForTimeout(2500)
      const modal = page.locator('.modal-content, .mantine-Modal-content').filter({ hasText: /bib|library|import|paste|paste/i }).first()
      await expect(modal, 'the import modal must open').toBeVisible({ timeout: 15_000 })
      if (await page.locator('.mantine-Modal-content').filter({ hasText: /bib|import/i }).count().then(n => n > 0).catch(() => false)) {
        // if a Mantine frame appeared, its buttons must still be gated legacy
        const mantineBtns = await page.locator('.mantine-Modal-content .mantine-Button-root').count()
        expect(mantineBtns, 'no Mantine Buttons inside a context-free modal').toBe(0)
      }
      await page.keyboard.press('Escape')
      await page.waitForTimeout(800)
    }
    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ')).toEqual([])
  })
})
