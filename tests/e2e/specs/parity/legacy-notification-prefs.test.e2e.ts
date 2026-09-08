import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { USER, TPLADMIN, ADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /user/notification-preferences) — email
// notification preferences. Contract: GET/POST /notifications/preferences.
const BASE = 'http://127.0.0.1:7420'
const PAGE = BASE + '/user/notification-preferences'
let p: any = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
  await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
})
test.afterAll(async () => {
  if (p) await p.context().close().catch(() => {})
})
const prefs = async () => (await (await api(p, 'GET', '/notifications/preferences')).json())
const setPrefs = async (body: unknown) => api(p, 'POST', '/notifications/preferences', body)
const restore = async (mute: boolean, delay: number | null) => {
  await setPrefs({ muteAllNotifications: mute, ...(delay === null ? {} : { notificationDelayMinutes: delay }) })
}

test('renders: "Email preferences" with mute checkbox + delay input', async () => {
  await expect(p.locator('h1, h2', { hasText: /email preferences/i })).toBeVisible({ timeout: 15000 })
  await expect(p.locator('input[name="muteAllNotifications"], input[value="1"]').first()).toBeAttached()
  await expect(p.locator('input[type="number"]').first()).toBeAttached()
})

test('deny: guests cannot open the page', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await q.goto(PAGE, { waitUntil: 'domcontentloaded' }).catch(() => {})
  const url = q.url()
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  const denied = /login|sign in/i.test(url) || /email preferences/i.test(body) === false
  expect(denied, 'guest must be redirected/denied (at ' + url + ')').toBeTruthy()
  await ctx.close()
})

test('mute: page form reflects and persists the mute state (POST /notifications/preferences)', async () => {
  // explicit contract: the page checkbox = "notifications ON"; muted == unchecked.
  // Set state via the API, verify the page renders it, submit, verify persistence.
  const start = (await prefs()) as any
  const curMute = Boolean(start?.muteAllNotifications)
  try {
    // round-trip 1: muted ON (box must render unchecked; submit keeps it muted)
    await setPrefs({ muteAllNotifications: true, ...(start?.notificationDelayMinutes === null || start?.notificationDelayMinutes === undefined ? {} : { notificationDelayMinutes: start.notificationDelayMinutes }) })
    await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
    const boxOff = p.locator('input[name="muteAllNotifications"]').first()
    await expect(boxOff).toBeVisible({ timeout: 15000 })
    await expect(boxOff).not.toBeChecked({ timeout: 5000 })
    await p.locator('button[type="submit"]').first().click()
    await p.waitForLoadState('domcontentloaded')
    await p.waitForTimeout(1200)
    expect(Boolean((await prefs())?.muteAllNotifications), 'muted=true read back').toBeTruthy()
    // round-trip 2: unmuted (box checked; submit keeps it unmuted)
    await setPrefs({ muteAllNotifications: false })
    await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
    const boxOn = p.locator('input[name="muteAllNotifications"]').first()
    await expect(boxOn).toBeChecked({ timeout: 5000 })
    await p.locator('button[type="submit"]').first().click()
    await p.waitForLoadState('domcontentloaded')
    await p.waitForTimeout(1200)
    expect(Boolean((await prefs())?.muteAllNotifications), 'muted=false read back').toBeFalsy()
  } finally {
    await setPrefs({ muteAllNotifications: curMute }).catch(() => {})
  }
})

test('delay: custom delay minutes persists and reads back', async () => {
  const before = await prefs()
  const beforeG = before?.global ?? before
  const oldDelay = beforeG?.notificationDelayMinutes ?? null
  const val = (oldDelay === 45 ? 60 : 45)
  await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
  const num = p.locator('input[type="number"]').first()
  await num.fill(String(val))
  const box = p.locator('input[name="muteAllNotifications"]').first()
  if (await box.isChecked()) await box.click()
  await p.locator('button[type="submit"]').first().click()
  await p.waitForLoadState('domcontentloaded')
  await p.waitForTimeout(1500)
  const after = (await prefs())?.global ?? (await prefs())
  expect(Number(after?.notificationDelayMinutes), 'delay read back').toBe(val)
  // restore the previous delay value (or clear)
  const delay = oldDelay === null ? {} : { notificationDelayMinutes: oldDelay }
  await api(p, 'POST', '/notifications/preferences', JSON.stringify({ muteAllNotifications: false, ...delay }))
})

test('default-label: page shows the server default delay hint', async () => {
  await p.goto(PAGE, { waitUntil: 'domcontentloaded' })
  const body = (await p.locator('body').innerText()) || ''
  expect(/2 minutes|default|leave empty/i.test(body), 'server default delay hint visible').toBeTruthy()
})

// role coverage: template admin and admin reach the same page
test('role: tpladmin and admin can open the page', async ({ browser }) => {
  for (const u of [TPLADMIN, ADMIN]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await loginRobust(q, u.email, u.password)
    const r = await q.goto(PAGE, { waitUntil: 'domcontentloaded' })
    expect(r?.status(), 'role page status for ' + u.email).toBe(200)
    await expect(q.locator('input[type="number"]').first()).toBeAttached({ timeout: 10000 })
    await ctx.close()
  }
})
