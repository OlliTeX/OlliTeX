import { test, expect } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api } from '../../parity/harness'
import { USER, TPLADMIN, ADMIN } from '../../fixtures/credentials'

// Parity baseline (legacy /user/notification-preferences) — email
// notification preferences. Contract: GET/POST /notifications/preferences.
// 2026-09-10 (owner queue 3): the legacy PAGE is REMOVED — the URL now 301s
// to the hub surface (Me → My settings → Email preferences, /hub#/mysettings.email)
// which manages the same endpoints. Page-form tests now assert the redirect
// against the hub surface; preference state is exercised via the API, whose
// contract is unchanged.
const BASE = 'http://127.0.0.1:7420'
const PAGE = BASE + '/user/notification-preferences'
const HUB_EMAIL = BASE + '/hub#/mysettings.email'
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
const setPrefs = async (body: unknown) => api(p, 'POST', '/notifications/preferences', body)

test('redirects: /user/notification-preferences 301 → /hub#/mysettings.email (page removed 2026-09-10)', async () => {
  const res = await p.request.get(PAGE, { maxRedirects: 0 })
  expect(res.status(), '301 expected').toBe(301)
  expect(res.headers()['location']).toBe('/hub#/mysettings.email')
})

test('hub surface: the redirect target renders the email preferences section', async () => {
  await p.goto(HUB_EMAIL, { waitUntil: 'domcontentloaded' })
  const body = ((await p.locator('body').innerText().catch(() => '')) || '').toLowerCase()
  await expect(p.locator('input:visible, select:visible, textarea:visible, button:visible').first()).toBeVisible({ timeout: 15000 })
  expect(/email preferences/i.test(body), 'email preferences surface visible').toBeTruthy()
})

test('deny: guests cannot open the preference surface (redirect → login)', async ({ browser }) => {
  const ctx = await browser.newContext(); const q = await ctx.newPage()
  await q.goto(PAGE, { waitUntil: 'domcontentloaded' }).catch(() => {})
  const url = q.url()
  const body = (await q.locator('body').innerText().catch(() => '')) || ''
  const denied = /login|sign in/i.test(url) || /mute all/i.test(body) === false
  expect(denied, 'guest must be redirected/denied (at ' + url + ')').toBeTruthy()
  await ctx.close()
})

test('mute: POST /notifications/preferences persists the mute state and reads back', async () => {
  const start = (await prefs()) as any
  const curMute = Boolean(start?.muteAllNotifications)
  const delayPart = start?.notificationDelayMinutes == null ? {} : { notificationDelayMinutes: start.notificationDelayMinutes }
  try {
    await setPrefs({ muteAllNotifications: true, ...delayPart })
    expect(Boolean((await prefs())?.muteAllNotifications), 'muted=true read back').toBeTruthy()
    await setPrefs({ muteAllNotifications: false, ...delayPart })
    expect(Boolean((await prefs())?.muteAllNotifications), 'muted=false read back').toBeFalsy()
  } finally {
    await setPrefs({ muteAllNotifications: curMute, ...delayPart }).catch(() => {})
  }
})

test('delay: custom delay minutes persists and reads back (API)', async () => {
  const before = (await prefs()) as any
  const curMute = Boolean(before?.muteAllNotifications)
  const oldDelay = before?.notificationDelayMinutes ?? null
  const val = (oldDelay === 45 ? 60 : 45)
  try {
    await setPrefs({ muteAllNotifications: curMute, notificationDelayMinutes: val })
    const after = (await prefs()) as any
    expect(Number(after?.notificationDelayMinutes), 'delay read back').toBe(val)
  } finally {
    await setPrefs({ muteAllNotifications: curMute, ...(oldDelay === null ? {} : { notificationDelayMinutes: oldDelay }) }).catch(() => {})
  }
})

test('default-hint: the hub email section shows the server default delay hint', async () => {
  await p.goto(HUB_EMAIL, { waitUntil: 'domcontentloaded' })
  await expect(p.locator('body').first()).toBeVisible({ timeout: 15000 })
  const body = ((await p.locator('body').innerText().catch(() => '')) || '').toLowerCase()
  expect(/default \(2 minutes\)|2 minutes/i.test(body), 'server default delay hint visible').toBeTruthy()
})

// role coverage: template admin and admin reach the same surface via the redirect
test('role: tpladmin and admin reach the hub email surface through the redirect', async ({ browser }) => {
  for (const u of [TPLADMIN, ADMIN]) {
    const ctx = await browser.newContext(); const q = await ctx.newPage()
    await loginRobust(q, u.email, u.password)
    await q.goto(PAGE, { waitUntil: 'domcontentloaded' })
    expect(q.url(), u.email + ' lands on the hub').toContain('/hub')
    await expect(q.locator('input:visible, select:visible, textarea:visible, button:visible').first()).toBeVisible({ timeout: 15000 })
    await ctx.close()
  }
})
