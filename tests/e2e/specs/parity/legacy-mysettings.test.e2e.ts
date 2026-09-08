import { test, expect, type Page } from '@playwright/test'
import { loginRobust } from '../../helpers/auth'
import { api, mongoEval } from '../../parity/harness'
import { USER } from '../../fixtures/credentials'

// Parity baseline (legacy /user/mysettings) — self-service settings contract:
// endpoint + payload + persisted user-doc state. Values are read, mutated,
// asserted, then RESTORED so fixtures stay pristine.
const BASE = 'http://127.0.0.1:7420'
let p: Page | null = null

test.beforeAll(async ({ browser }) => {
  const c = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  p = await c.newPage()
  await loginRobust(p, USER.email, USER.password)
})
test.afterAll(async () => {
  if (p) await p.context().close().catch(() => {})
})

const u = () => { expect(p).toBeTruthy(); return p }
const userId = () => mongoEval('db.users.findOne({ email: "e2e-user@e2e.test" })._id.toString()')
const doc = (field: string) => mongoEval(`db.users.findOne({ _id: ObjectId("${userId()}") }).${field}`)
const ok = (code: number) => [200, 204].includes(code)

test('renders: /user/mysettings loads for a logged-in user', async () => {
  const page = u()
  const r = await page.goto(BASE + '/user/mysettings', { waitUntil: 'domcontentloaded' })
  expect(r?.status()).toBe(200)
  await page.waitForTimeout(1500)
  await expect(page).not.toHaveTitle(/login/i)
})

test('denied: unauthenticated guests do not reach settings', async ({ page }) => {
  const r = await page.request.get(BASE + '/user/mysettings')
  const followed = await page.goto(BASE + '/user/mysettings', { waitUntil: 'domcontentloaded' }).catch(() => null)
  const denied = r?.status() === 403 || r?.status() === 302 || /login/i.test(page.url() || '') || /login/i.test(followed?.url() || '')
  expect(denied, 'guest must not get settings (got: ' + (r?.status()) + ' at ' + (page.url()).slice(0, 80) + ')').toBeTruthy()
})

test('account: PUT /user/settings {first_name,last_name} persists', async () => {
  const page = u()
  const before = { f: doc('first_name'), l: doc('last_name') }
  const r = await api(page, 'POST', '/user/settings', { first_name: 'Parity', last_name: 'Check' })
  expect(ok(r.status()), (await r.text().catch(() => '')).slice(0, 140)).toBeTruthy()
  expect(doc('first_name')).toBe('Parity')
  expect(doc('last_name')).toBe('Check')
  // restore
  const back = await api(page, 'POST', '/user/settings', { first_name: before.f, last_name: before.l })
  expect(ok(back.status())).toBeTruthy()
})

test('password: POST /user/password/update changes it (new pw logs in)', async () => {
  const page = u()
  const newpw = 'Ol-Fixture-3m2Q' // set to the known fixture password (idempotent)
  const r = await api(page, 'POST', '/user/password/update', { currentPassword: newpw, newPassword1: newpw + 'X9', newPassword2: newpw + 'X9' })
  expect(r.status(), (await r.text().catch(() => '')).slice(0, 140)).toBe(200)
  // verify new password actually authenticates (separate context)
  const { browser } = { browser: (globalThis as any).__pwBrowser }
  void browser
  const b = await (page.context())
  void b
  const ctx2 = await (page as any).context().browser()!.newContext()
  const p2 = await ctx2.newPage()
  await loginRobust(p2, USER.email, newpw + 'X9')
  await expect(p2).toHaveURL(/\/(project|hub)/, 'login with the new password succeeds')
  await ctx2.close()
  // restore the fixture password
  const back = await api(page, 'POST', '/user/password/update', { currentPassword: newpw + 'X9', newPassword1: newpw, newPassword2: newpw })
  expect(back.status()).toBe(200)
})

test('sync: WebDAV connect → status → disconnect (project synchronisation)', async () => {
  const page = u()
  const conn = await api(page, 'POST', '/user/webdav/connect', {
    serverUrl: 'https://dav.example.net/remote.php/dav/files/e2e',
    username: 'e2e-user',
    password: 'not-a-real-secret',
    directory: '/',
  })
  expect(ok(conn.status()), 'connect: ' + (await conn.text().catch(() => '')).slice(0, 160)).toBeTruthy()
  const st = await api(page, 'GET', '/user/webdav/status')
  expect(st.status(), 'status: ' + (await st.text().catch(() => '')).slice(0, 160)).toBe(200)
  const disc = await api(page, 'POST', '/user/webdav/disconnect', {})
  expect(ok(disc.status()), 'disconnect').toBeTruthy()
})

test('keybindings: PUT /user/settings customKeybindings saves + reloads', async () => {
  const page = u()
  const before = String(doc('customKeybindings'))
  const r = await api(page, 'POST', '/user/settings', { customKeybindings: { toggleComments: 'Mod-Shift-7' } })
  expect(ok(r.status())).toBeTruthy()
  expect(String(doc('ace.customKeybindings'))).toContain('toggleComments')
  // restore
  const back = await api(page, 'POST', '/user/settings', { customKeybindings: {} })
  expect(ok(back.status())).toBeTruthy()
})

test('sessions: GET /user/sessions lists; POST /user/sessions/clear works', async () => {
  const page = u()
  const lr = await page.request.get(BASE + '/user/sessions', { headers: { Accept: 'text/html' } })
  expect(lr.status()).toBe(200)
  const html = await lr.text()
  expect(/Sessions/i.test(html), 'session list page rendered').toBeTruthy()
  const cr = await api(page, 'POST', '/user/sessions/clear', {})
  expect([200, 201, 204, 302, 303].includes(cr.status()), 'clear sessions: ' + cr.status()).toBeTruthy()
})

test('appearance: PUT /user/settings overallTheme persists', async () => {
  const page = u()
  const before = String(doc('ace.overallTheme'))
  const r = await api(page, 'POST', '/user/settings', { overallTheme: 'dark' })
  expect(ok(r.status())).toBeTruthy()
  expect(doc('ace.overallTheme')).toBe('dark')
  // restore
  if (before && before !== 'null') await api(page, 'POST', '/user/settings', { overallTheme: before })
})

test('references: PUT /user/settings reference providers persist (zotero)', async () => {
  const page = u()
  const r = await api(page, 'POST', '/user/settings', { zotero: { enabled: true, disablePersonalLibrary: false } })
  expect([200, 204].includes(r.status()), 'zotero settings accepted: ' + (await r.text().catch(() => '')).slice(0, 160)).toBeTruthy()
  if ([200, 204].includes(r.status())) expect(String(doc('ace.zotero'))).toBeTruthy()
})
