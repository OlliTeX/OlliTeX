/**
 * M4 module Mantine wave — llm module surfaces.
 *
 * Gate discipline proof:
 *  1. LLM user settings page (no editor context): fully legacy surface
 *     (zero Mantine Buttons — the gate must not leak outside /editor).
 *  2. LLM admin settings page (admin, no editor context): same legacy
 *     guarantee + the page keeps working.
 *  3. /editor: the LLM in-editor surfaces remain stable (no pageerrors)
 *     — the functional round-trips are covered by the byo-llm suite which
 *     runs against the same converted components.
 */
import { test, expect } from '@playwright/test'
import { loginRobust, createBlankProject } from '../../helpers/auth'

const B = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

test.describe('M4 llm surfaces', () => {
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

  test('user llm settings page: legacy surface, zero Mantine Button leak', async () => {
    await page.goto(B + '/user/settings#llm', { waitUntil: 'load' })
    await page.waitForTimeout(4000)
    const body = await page.locator('body').innerText()
    expect(body.toLowerCase(), 'the LLM settings section must render').toMatch(/api key|provider|llm|model/i)
    const legacyBtns = await page.locator('.btn').count()
    expect(legacyBtns, 'LLM page buttons must be legacy OLButtons').toBeGreaterThan(0)
    const mantineBtns = await page.locator('.mantine-Button-root').count()
    expect(mantineBtns, 'no Mantine Buttons on the editor-less LLM page').toBe(0)
    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ')).toEqual([])
  })

  test('admin llm settings page: legacy URL redirects to the hub (page removed 2026-09-10)', async () => {
    const res = await page.request.get(B + '/admin/llm/settings', { maxRedirects: 0 }).catch(() => null)
    expect(res, 'response available').toBeTruthy()
    expect(res!.status(), '301 expected').toBe(301)
    expect(res!.headers()['location']).toBe('/hub#/site.llm.instance')
    // follow the redirect: the hub admin instance surface must render (llm text, no page errors)
    await page.goto(B + '/admin/llm/settings', { waitUntil: 'load' }).catch(() => {})
    await page.waitForTimeout(3500)
    const body = await page.locator('body').innerText()
    expect(body.toLowerCase()).toMatch(/llm|instance/i)
    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ')).toEqual([])
  })

  test('editor: llm in-editor surfaces stable (no pageerrors)', async () => {
    await page.goto(B + '/editor/' + pid, { waitUntil: 'load' })
    await expect(page.locator('.cm-editor').first()).toBeVisible({ timeout: 90_000 })
    await page.waitForTimeout(2500)
    // open the LLM rail/pane affordance if present (best-effort)
    const llm = page.locator("[aria-label^='LLM' i], [aria-label*='Ask AI' i], .llm-panel").first()
    const present = await llm.isVisible().catch(() => false)
    if (present) {
      await llm.click({ timeout: 5_000 }).catch(() => {})
      await page.waitForTimeout(2500)
    }
    await page.keyboard.press('Escape').catch(() => {})
    await page.waitForTimeout(800)
    expect(pageErrors, 'no pageerrors in the editor with LLM module: ' + pageErrors.join(' | ')).toEqual([])
  })
})
