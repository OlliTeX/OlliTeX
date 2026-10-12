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

  test('library page: legacy /library retired — 404/login-wall pin (surface moved into the editor)', async () => {
    // 2026-11-16 (9b142051): the standalone /library (shared-citations) page is
    // RETIRED (verified: anonymous 302 /login, logged-in flat 404 "Not found").
    // In v2 the personal-library/citations surface lives INSIDE the editor
    // (file-tree menu → "personal library section (0 citations)"), not as a
    // top-level page. FLAG (owner): confirm /library retirement + the
    // editor-internal library as the canonical v2 surface; re-arm the render +
    // Button-leak assertions if the page returns. This pins the RETIRED shape.
    await page.goto(B + '/library', { waitUntil: 'load' }).catch(() => {})
    const url = page.url()
    const body = (await page.locator('body').innerText().catch(() => '')) || ''
    const retired = /\/login/i.test(url) || /not found|404/i.test(body)
    expect(retired, '/library must be RETIRED (url ' + url + ')').toBeTruthy()
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
