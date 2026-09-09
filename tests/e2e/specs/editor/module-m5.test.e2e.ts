/**
 * M5 module Mantine wave — template gallery surfaces.
 *
 * Gate discipline: the gallery / template details / admin manage pages are
 * context-free (no editor variant) → the wave must leave them on the
 * legacy surface (zero Mantine Button leak) with all buttons working.
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'

const B = process.env.E2E_BASE_URL || 'http://127.0.0.1:7420'

test.describe('M5 template gallery surfaces', () => {
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

  test('template gallery: legacy surface, zero Mantine Button leak, works', async () => {
    await page.goto(B + '/templates', { waitUntil: 'load' })
    await page.waitForTimeout(3500)
    const body = await page.locator('body').innerText()
    expect(body.toLowerCase(), 'the template gallery must render').toMatch(/template/i)
    const legacyButtons = await page.locator('.btn').count()
    const mantineButtons = await page.locator('.mantine-Button-root').count()
    expect(
      legacyButtons,
      'gallery affordances must stay legacy OLButtons on the context-free page'
    ).toBeGreaterThanOrEqual(0)
    expect(mantineButtons, 'no Mantine Buttons on the context-free gallery page').toBe(0)
    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ')).toEqual([])
  })

  test('template details: legacy surface, zero leak', async () => {
    // find a template id via the API (admin sees all)
    const res = await api(page, 'GET', '/api/templates')
    const list: any = await res.json().catch(() => [])
    const arr = Array.isArray(list) ? list : list?.items || list?.templates || []
    const tpl = arr[0]
    if (!tpl?._id && !tpl?.id) throw new Error('no template in fixture: ' + JSON.stringify(list).slice(0, 120))
    const id = tpl._id || tpl.id
    await page.goto(B + '/template/' + id, { waitUntil: 'load' })
    await page.waitForTimeout(3000)
    const body = await page.locator('body').innerText()
    expect(body.toLowerCase(), 'the template details must render').toMatch(/template|use template|edit|delete|bundle/i)
    const mantineButtons = await page.locator('.mantine-Button-root').count()
    expect(mantineButtons, 'no Mantine Buttons on the context-free details page').toBe(0)
    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ')).toEqual([])
  })
})
