import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { ADMIN, USER, TPLADMIN } from '../../fixtures/credentials'

// Parity HUB side (hub Instance statistics leaf, /admin-settings/site.general.stats) —
// same admin-only instance-stats surface as legacy /admin/instance-stats:
// the shared fetchSeries API + window switcher + admin-only access.
const BASE = 'http://127.0.0.1:7420'
const PAGE = BASE + '/admin-settings/site.general.stats'
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

test('renders: canonical statistics page shows the dashboard + alert area', async () => {
  // AJ-1 (owner 2026-10-08): the settings sections left /hub (RETIRED_FROM_
  // HUB in hub-root) and live on the dedicated page
  // /admin-settings/site.general.stats — the canonical surface (the
  // /hub#/site.general.stats deep link no longer renders the section).
  // Re-pinned 2026-10-11 (ag-v44, battery cleanup) from the retired /hub
  // form. The window switch is API-pinned below ("windows:" leg); the UI
  // window control is Grafana-pane-only in the current design (see board
  // item TODO-9b142051, Class C note).
  await p.goto(BASE + '/admin-settings/site.general.stats', { waitUntil: 'domcontentloaded' })
  await expect(p.locator('body', { hasText: /instance statistics/i }).first()).toBeVisible({ timeout: 15000 })
  const body = (await p.locator('body').innerText()) || ''
  expect(/alert settings/i.test(body), 'alert area present').toBeTruthy()
})

test('deny: non-site-admins cannot open the hub statistics leaf', async ({ browser }) => {
  for (const who of [TPLADMIN, USER]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await loginRobust(q, who.email, who.password)
    await q.goto(PAGE, { waitUntil: 'domcontentloaded' })
    await q.waitForTimeout(1200)
    const body = (await q.locator('body').innerText().catch(() => '')) || ''
    // denial parity: the admin-only metrics surface (window control + test controls) must not render
    const denied = !/alert|send test/i.test(body)
    expect(denied, who.email + ' denied (body: ' + body.replace(/\s+/g, ' ').slice(0, 80) + ')').toBeTruthy()
    await ctx.close()
  }
})

test('series: the leaf is backed by the same /admin/instance-stats/api/series contract', async () => {
  const r = await a('GET', '/admin/instance-stats/api/series?metric=user_count&window=month')
  expect(r.status(), 'series status ' + r.status()).toBeLessThan(500)
  const j = await r.json()
  expect(j.metric).toBe('user_count')
  expect(j.window).toBe('month')
  expect(Array.isArray(j.points), 'points array (shared API)').toBeTruthy()
})

test('alert-config: the same GET/PUT alert-config contract serves the hub', async () => {
  const before = await cfg()
  expect(before, 'config object').toBeTruthy()
  expect(typeof before.diskWarningPercent, 'disk threshold').toBe('number')
  try {
    const r = await a('PUT', '/admin/instance-stats/api/alert-config', { ...before, ramWarningPercent: 55 })
    expect(r.status(), 'put status ' + r.status()).toBeLessThan(500)
    const after = await cfg()
    expect(after?.ramWarningPercent, 'persisted ram threshold').toBe(55)
  } finally {
    await a('PUT', '/admin/instance-stats/api/alert-config', { ...before }).catch(() => {})
    const back = await cfg()
    expect(back?.ramWarningPercent, 'restored').toBe(before.ramWarningPercent)
  }
})

test('test-email: the same send-test-alert endpoint answers for the hub admin', async () => {
  const r = await a('POST', '/admin/instance-stats/api/send-test-alert-email', { reason: 'parity-hub' })
  expect(r.status(), 'test-email status ' + r.status()).toBeLessThan(500)
})

test('windows: month/6m/year/all are all valid windows for the hub charts', async () => {
  for (const w of ['month', '6m', 'year', 'all']) {
    const r = await a('GET', `/admin/instance-stats/api/series?metric=ram_usage&window=${w}`)
    expect(r.status(), `window ${w} status`).toBeLessThan(500)
    const j = await r.json().catch(() => ({}))
    expect(j.window, `window echoed (${w})`).toBe(w)
  }
})
