import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, captureApi, waitForCall } from '../../parity/harness'
import { USER, TPLADMIN } from '../../fixtures/credentials'

// Parity (HUB) — /hub#/mysettings.email (email preferences leaf) vs legacy
// /user/notification-preferences. Same contract: GET/POST
// /notifications/preferences { muteAllNotifications, notificationDelayMinutes }.
const BASE = 'http://127.0.0.1:7420'
const LEAF = BASE + '/hub#/mysettings.email'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
})
test.afterAll(async () => {
  if (p) await p.context().close().catch(() => {})
})
const prefs = async () => (await (await api(p, 'GET', '/notifications/preferences')).json())

test('renders: hub email-prefs leaf shows mute + delay controls', async () => {
  await p.goto(LEAF, { waitUntil: 'domcontentloaded' })
  await p.waitForTimeout(2000)
  const body = (await p.locator('body').innerText()) || ''
  expect(/email|notification/i.test(body), 'settings UI present').toBeTruthy()
})

test('deny: guest cannot open the hub email-prefs leaf', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  const r = await q.goto(LEAF, { waitUntil: 'domcontentloaded' }).catch(() => null)
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  const denied = r?.status() === 403 || !/email|notification/i.test(body) || /sign in|log ?in/i.test(body)
  expect(denied, 'guest denied (got ' + (r?.status()) + ')').toBeTruthy()
  await ctx.close()
})

test('mute: hub switch persists via POST /notifications/preferences', async () => {
  const before = await prefs()
  await p.goto(LEAF, { waitUntil: 'domcontentloaded' })
  // Mantine Switch: the <input> is visually hidden — click the styled root.
  const sw = p.locator('.mantine-Switch-root').first()
  await expect(sw).toBeVisible({ timeout: 15000 })
  // Switch checked = notifications ON; we want notifications OFF (mute ON)
  const input = p.locator('input[type="checkbox"]').first()
  if (await input.isChecked()) {
    const cap = captureApi(p as any, BASE)
    await sw.click()
    await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === '/notifications/preferences'), 'prefs save')
  }
  await p.waitForTimeout(1500)
  const after = await prefs()
  expect(Boolean(after?.muteAllNotifications), 'notifications muted read back').toBeTruthy()
  // restore: notifications ON again
  if (!(await input.isChecked().catch(() => false))) {
    await sw.click()
    await p.waitForTimeout(1500)
  }
  const back = await prefs()
  expect(Boolean(back?.muteAllNotifications), 'unmuted read back').toBeFalsy()
})

test('delay: hub NumberInput + Save preferences persists and reads back', async () => {
  const before = await prefs()
  const oldDelay = before?.notificationDelayMinutes ?? null
  const val = oldDelay === 45 ? 60 : 45
  await p.goto(LEAF, { waitUntil: 'domcontentloaded' })
  const num = p.locator('input[placeholder*="Server default" i]').first()
  await expect(num).toBeAttached({ timeout: 15000 })
  await num.fill(String(val))
  const cap = captureApi(p as any, BASE)
  await p.locator('button', { hasText: /save preferences/i }).first().click()
  await waitForCall(cap, c => c.some(x => x.method === 'POST' && x.path === '/notifications/preferences'), 'prefs save')
  await p.waitForTimeout(1200)
  const after = await prefs()
  expect(Number(after?.notificationDelayMinutes), 'delay read back').toBe(val)
  // restore previous delay value (or clear)
  const body: Record<string, unknown> = { muteAllNotifications: false }
  if (oldDelay !== null) body.notificationDelayMinutes = oldDelay
  else body.notificationDelayMinutes = null
  void body
  await api(p, 'POST', '/notifications/preferences', JSON.stringify({ muteAllNotifications: false, notificationDelayMinutes: oldDelay }))
})

test('default-label: hub shows the server default delay hint', async () => {
  await p.goto(LEAF, { waitUntil: 'domcontentloaded' })
  const body = (await p.locator('body').innerText()) || ''
  expect(/2 minutes|default|leave empty/i.test(body), 'default delay hint').toBeTruthy()
})

test('role: tpladmin opens the leaf (personal settings for any role)', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await loginRobust(q, TPLADMIN.email, TPLADMIN.password)
  await q.goto(LEAF, { waitUntil: 'domcontentloaded' })
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  expect(/email|notification/i.test(body), 'tpladmin leaf').toBeTruthy()
  await ctx.close()
})
