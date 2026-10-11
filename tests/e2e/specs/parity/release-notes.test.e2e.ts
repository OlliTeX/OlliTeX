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

test('notes: the (admin) overview renders its KPI grid', async () => {
  // B wave 2026-10-11: the "What's new" card was retired from the overview by
  // the owner on 2026-10-09 (leaves.tsx: "the What's new / Instance
  // management / About cards were retired from this page — the KPI grid
  // remains"); the overview leaf is admin-settings only (/admin-settings/
  // overview — hub-root drops it from the /hub rail). Assert the surviving
  // KPI grid instead of the retired card.
  await p.goto(BASE + '/admin-settings/overview', { waitUntil: 'domcontentloaded' })
  await expect(p.locator('main').getByText('Projects', { exact: true }).first())
    .toBeVisible({ timeout: 20000 })
  expect(await p.locator('main').innerText()).toMatch(/Users|Projects/)
})
