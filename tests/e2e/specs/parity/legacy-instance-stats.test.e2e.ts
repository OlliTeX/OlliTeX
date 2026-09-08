import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /admin/instance-stats) — admin-only dashboard:
// series API (window-aware), alert-config get/put, test-alert email.
const BASE = 'http://127.0.0.1:7420'
const PAGE = BASE + '/admin/instance-stats'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, ADMIN.email, ADMIN.password)
  await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
})
test.afterAll(async () => {
  if (p) await p.context().close().catch(() => {})
})
const a = (method: string, path: string, body?: unknown) => api(p, method, path, body)
const cfg = async () => (await a('GET', '/admin/instance-stats/api/alert-config')).json().catch(() => null)

test('renders: /admin/instance-stats shows the dashboard (sections + window control)', async () => {
  expect(p.url(), 'on the page').toContain('/admin/instance-stats')
  const body = (await p.locator('body').innerText()) || ''
  expect(/users|project|storage|system/i.test(body), 'dashboard sections present').toBeTruthy()
  await expect(p.locator('select, [role="combobox"]').first()).toBeVisible({ timeout: 10000 })
})

test('deny: non-site-admins (tpladmin, user) and guests are denied', async ({ browser }) => {
  for (const who of [TPLADMIN, USER]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await q.goto(PAGE, { waitUntil: 'domcontentloaded' }).catch(() => {})
    const landed = q.url()
    const denied = /login|signin|denied|403|forbidden/i.test(landed) || !/instance statistics|dashboard/i.test((await q.locator('body').innerText().catch(() => '')) || '')
    expect(denied, who.email + ' denied (url ' + landed + ')').toBeTruthy()
    await ctx.close()
  }
  const ctx = await browser.newContext(); const g = await ctx.newPage()
  await g.goto(PAGE, { waitUntil: 'domcontentloaded' }).catch(() => {})
  const denied = /login|signin/i.test(g.url()) || !/send test|alert/i.test((await g.locator('body').innerText().catch(() => '')) || '')
  expect(denied, 'guest denied').toBeTruthy()
  await ctx.close()
})

test('series: GET /admin/instance-stats/api/series returns {metric, window, points[]}', async () => {
  const r = await a('GET', '/admin/instance-stats/api/series?metric=user_count&window=month')
  expect(r.status(), 'series status ' + r.status()).toBeLessThan(500)
  const j = await r.json()
  expect(j.metric).toBe('user_count')
  expect(j.window).toBe('month')
  expect(Array.isArray(j.points), 'points array').toBeTruthy()
})

test('alert-config: GET returns thresholds; PUT persists and reads back', async () => {
  const before = await cfg()
  expect(before, 'config object').toBeTruthy()
  expect(typeof before.diskWarningPercent, 'disk threshold').toBe('number')
  try {
    const r = await a('PUT', '/admin/instance-stats/api/alert-config', { ...before, diskWarningPercent: 42 })
    expect(r.status(), 'put status ' + r.status()).toBeLessThan(500)
    const after = await cfg()
    expect(after?.diskWarningPercent, 'persisted disk threshold').toBe(42)
  } finally {
    await a('PUT', '/admin/instance-stats/api/alert-config', { ...before }).catch(() => {})
    const back = await cfg()
    expect(back?.diskWarningPercent, 'restored').toBe(before.diskWarningPercent)
  }
})

test('test-email: POST send-test-alert-email responds for the site admin', async () => {
  const r = await a('POST', '/admin/instance-stats/api/send-test-alert-email', { reason: 'parity' })
  expect(r.status(), 'test-email status ' + r.status()).toBeLessThan(500)
})

test('windows: the selectors cover month/6m/year/all and each fetches its window', async () => {
  for (const w of ['month', '6m', 'year', 'all']) {
    const r = await a('GET', `/admin/instance-stats/api/series?metric=project_count&window=${w}`)
    expect(r.status(), `window ${w} status`).toBeLessThan(500)
    const j = await r.json().catch(() => ({}))
    expect(j.window, `window echoed (${w})`).toBe(w)
  }
})
