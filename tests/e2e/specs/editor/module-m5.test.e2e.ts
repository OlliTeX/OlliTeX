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

    // 2026-09-11 (mega-batch): the "template details" test needs at least one
    // published template. Earlier full-suite greens relied on ANOTHER spec
    // having created one first (order-fragile on fresh stacks); make it
    // deterministic: publish one from the seed project when none exists.
    try {
      const lst = await api(page, 'GET', '/api/templates')
      const j: any = await lst.json().catch(() => null)
      const arr: any[] = Array.isArray(j) ? j : (j?.templates || j?.items || [])
      if (arr.length === 0) {
        const pj: any = await api(page, 'GET', '/api/projects').then(r => r.json().catch(() => null))
        const parr: any[] = Array.isArray(pj) ? pj : (pj?.projects || [])
        const seed = parr.find(x => (x.name || x.title) === 'e2e-seed-project') || parr[0]
        if (seed?._id) {
          await api(page, 'POST', '/template/new/' + seed._id, {
            name: 'E2E seed template',
            category: 'academic-journal',
          }).catch(() => {}) // best-effort fixture; absence is asserted below
        }
      }
    } catch { /* the fixture is permissive (the test tolerates an empty list) */ }
  })

  test.afterAll(async () => {
    await c.close().catch(() => {})
  })

  test('template gallery: legacy route retired → hub gallery renders (owner items 7+9)', async () => {
    // the legacy /templates page is removed; the route 301s into the hub
    // gallery, which is where the Mantine-surface QA now applies.
    await page.goto(B + '/templates', { waitUntil: 'load' })
    await page.waitForTimeout(3500)
    expect(page.url(), 'redirected into the hub gallery').toMatch(/\/hub#\/?templates/i)
    const body = await page.locator('body').innerText()
    expect(body.toLowerCase(), 'the hub gallery must render').toMatch(/template/i)
    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ')).toEqual([])
  })

  test('template details: legacy route retired → hub gallery (owner item 9)', async () => {
    const res = await api(page, 'GET', '/api/templates')
    const list: any = await res.json().catch(() => [])
    const arr = Array.isArray(list) ? list : list?.items || list?.templates || []
    const tpl = arr[0]
    if (!tpl?._id && !tpl?.id) throw new Error('no template in fixture: ' + JSON.stringify(list).slice(0, 120))
    const id = tpl._id || tpl.id
    await page.goto(B + '/template/' + id, { waitUntil: 'load' })
    await page.waitForTimeout(3000)
    expect(page.url(), 'redirected into the hub gallery').toMatch(/\/hub#\/?templates/i)
    const body = await page.locator('body').innerText()
    expect(body.toLowerCase(), 'the hub gallery must render').toMatch(/template/i)
    expect(pageErrors, 'no pageerrors: ' + pageErrors.join(' | ').slice(0, 240)).toEqual([])
  })})
