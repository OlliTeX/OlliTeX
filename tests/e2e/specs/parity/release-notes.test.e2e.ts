/**
 * 2026-09-16 (owner task T10/#8, P1): release-notes surfacing.
 * docs/RELEASE_NOTES.md is served at GET /api/hub/notes and rendered as the
 * "What's new" card on the hub Overview.
 */
import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { ADMIN } from '../../fixtures/credentials'

const BASE = 'http://127.0.0.1:7420'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1440, height: 940 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
})
test.afterAll(async () => {
  if (p) await p.context().close().catch(() => {})
})

test('notes: /api/hub/notes serves the release notes markdown', async () => {
  const r: any = await p.request.get(BASE + '/api/hub/notes')
  expect(r.status()).toBe(200)
  expect((r.headers()['content-type'] || '')).toMatch(/markdown/)
  const txt = await r.text()
  expect(txt).toMatch(/OlliTeX — What's new/)
  expect(txt).toMatch(/One console/)
})

test('notes: hub Overview shows the What’s new card', async () => {
  await p.goto(BASE + '/hub#/overview', { waitUntil: 'domcontentloaded' })
  await expect(p.locator('main').getByText('What’s new', { exact: false }).first())
    .toBeVisible({ timeout: 20000 })
  expect(await p.locator('main').innerText()).toMatch(/One console/)
})
